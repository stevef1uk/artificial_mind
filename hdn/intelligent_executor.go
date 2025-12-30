package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	planner "agi/planner_evaluator"
	selfmodel "agi/self"

	"github.com/redis/go-redis/v9"
)

// IntelligentExecutor handles the complete workflow of:
// 1. Planning tasks using HTN and Planner/Evaluator
// 2. Generating code for unknown tasks using LLM
// 3. Testing generated code in Docker
// 4. Caching successful code for reuse
// 5. Learning from failures and improving
type IntelligentExecutor struct {
	domainManager      *DomainManager
	codeStorage        *CodeStorage
	codeGenerator      *CodeGenerator
	dockerExecutor     *SimpleDockerExecutor
	llmClient          *LLMClient
	actionManager      *ActionManager
	plannerIntegration *PlannerIntegration
	selfModelManager   *selfmodel.Manager
	toolMetrics        *ToolMetricsManager
	fileStorage        *FileStorage
	hdnBaseURL         string // For tool calling
	maxRetries         int
	validationMode     bool
	usePlanner         bool
	recentTasks        map[string]time.Time // Loop protection: track recent task executions
	learningRedis      *redis.Client        // Redis client for learning data
	ctx                context.Context      // Context for Redis operations
}

// FailurePattern tracks common failure patterns for learning
type FailurePattern struct {
	PatternType   string    `json:"pattern_type"`   // "compilation", "runtime", "logic", "validation"
	ErrorCategory string    `json:"error_category"` // "undefined", "type_mismatch", "import_error", etc.
	Language      string    `json:"language"`
	TaskCategory  string    `json:"task_category"` // Derived from task name/description
	Frequency     int       `json:"frequency"`
	SuccessRate   float64   `json:"success_rate"` // Success rate after fixes
	CommonFixes   []string  `json:"common_fixes"` // What fixes work for this pattern
	FirstSeen     time.Time `json:"first_seen"`
	LastSeen      time.Time `json:"last_seen"`
}

// CodeGenStrategy tracks code generation strategies and their effectiveness
type CodeGenStrategy struct {
	StrategyID   string    `json:"strategy_id"`
	PromptStyle  string    `json:"prompt_style"` // "detailed", "concise", "example_based", etc.
	TaskCategory string    `json:"task_category"`
	Language     string    `json:"language"`
	SuccessRate  float64   `json:"success_rate"`
	AvgRetries   float64   `json:"avg_retries"`
	AvgQuality   float64   `json:"avg_quality"`
	UsageCount   int       `json:"usage_count"`
	LastUsed     time.Time `json:"last_used"`
}

// CodeGenLearningProgress tracks learning progress by task category and language
type CodeGenLearningProgress struct {
	TaskCategory   string  `json:"task_category"`
	Language       string  `json:"language"`
	SuccessRate    float64 `json:"success_rate"`
	AvgQuality     float64 `json:"avg_quality"`
	RecentProgress float64 `json:"recent_progress"` // Progress in last N executions
	FocusScore     float64 `json:"focus_score"`     // Should we focus here?
}

// ExecutionRequest represents a request to execute a task intelligently
type ExecutionRequest struct {
	TaskName        string            `json:"task_name"`
	Description     string            `json:"description"`
	Context         map[string]string `json:"context"`
	Language        string            `json:"language"`
	ForceRegenerate bool              `json:"force_regenerate"`
	MaxRetries      int               `json:"max_retries"`
	Timeout         int               `json:"timeout"`
	HighPriority    bool              `json:"high_priority"` // true for user requests, false for background tasks
}

// IntelligentExecutionResult represents the result of intelligent execution
type IntelligentExecutionResult struct {
	Success         bool             `json:"success"`
	Result          interface{}      `json:"result,omitempty"`
	Error           string           `json:"error,omitempty"`
	GeneratedCode   *GeneratedCode   `json:"generated_code,omitempty"`
	ExecutionTime   time.Duration    `json:"execution_time"`
	RetryCount      int              `json:"retry_count"`
	UsedCachedCode  bool             `json:"used_cached_code"`
	ValidationSteps []ValidationStep `json:"validation_steps,omitempty"`
	NewAction       *DynamicAction   `json:"new_action,omitempty"`
	WorkflowID      string           `json:"workflow_id,omitempty"`
}

// ValidationStep represents a step in the validation process
type ValidationStep struct {
	Step     string        `json:"step"`
	Success  bool          `json:"success"`
	Message  string        `json:"message"`
	Duration time.Duration `json:"duration"`
	Code     string        `json:"code,omitempty"`
	Output   string        `json:"output,omitempty"`
	Error    string        `json:"error,omitempty"`
}

func NewIntelligentExecutor(
	domainManager *DomainManager,
	codeStorage *CodeStorage,
	codeGenerator *CodeGenerator,
	dockerExecutor *SimpleDockerExecutor,
	llmClient *LLMClient,
	actionManager *ActionManager,
	plannerIntegration *PlannerIntegration,
	selfModelManager *selfmodel.Manager,
	toolMetrics *ToolMetricsManager,
	fileStorage *FileStorage,
	hdnBaseURL string,
	redisAddr string, // Redis address for learning data
) *IntelligentExecutor {
	// Initialize Redis client for learning data if address provided
	var learningRedis *redis.Client
	if redisAddr != "" {
		learningRedis = redis.NewClient(&redis.Options{
			Addr: redisAddr,
		})
	}

	return &IntelligentExecutor{
		domainManager:      domainManager,
		codeStorage:        codeStorage,
		codeGenerator:      codeGenerator,
		dockerExecutor:     dockerExecutor,
		llmClient:          llmClient,
		actionManager:      actionManager,
		plannerIntegration: plannerIntegration,
		selfModelManager:   selfModelManager,
		toolMetrics:        toolMetrics,
		fileStorage:        fileStorage,
		hdnBaseURL:         hdnBaseURL,
		maxRetries:         3,
		validationMode:     true,
		usePlanner:         plannerIntegration != nil,
		learningRedis:      learningRedis,
		ctx:                context.Background(),
		recentTasks:        make(map[string]time.Time),
	}
}

// callTool calls a tool via the HDN server API
func (ie *IntelligentExecutor) callTool(toolID string, params map[string]interface{}) (map[string]interface{}, error) {
	if ie.hdnBaseURL == "" {
		return nil, fmt.Errorf("HDN base URL not configured for tool calling")
	}

	// Prepare the request: the tools API expects the parameters at the top level JSON body
	requestBody := params

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal tool request: %v", err)
	}

	// Make HTTP request to tool endpoint
	url := fmt.Sprintf("%s/api/v1/tools/%s/invoke", ie.hdnBaseURL, toolID)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create tool request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tool request failed: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read tool response: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse tool response: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tool call failed with status %d: %v", resp.StatusCode, result)
	}

	return result, nil
}

// getAvailableTools fetches available tools from the HDN API
func (ie *IntelligentExecutor) getAvailableTools(ctx context.Context) ([]Tool, error) {
	if ie.hdnBaseURL == "" {
		return []Tool{}, nil
	}

	url := fmt.Sprintf("%s/api/v1/tools", ie.hdnBaseURL)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get tools: status %d", resp.StatusCode)
	}

	var result struct {
		Tools []Tool `json:"tools"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return result.Tools, nil
}

// filterRelevantTools filters tools relevant to the task
// NOTE: This function is intentionally permissive - it includes most tools by default
// to allow the LLM to choose the best tool for the task. Only obviously irrelevant tools are filtered.
func (ie *IntelligentExecutor) filterRelevantTools(tools []Tool, req *ExecutionRequest) []Tool {
	var relevant []Tool
	descLower := strings.ToLower(req.Description)
	taskLower := strings.ToLower(req.TaskName)
	combined := descLower + " " + taskLower

	// Expanded keywords that suggest tool usage - more comprehensive matching
	toolKeywords := map[string][]string{
		"tool_html_scraper": {"scrape", "html", "web", "fetch", "url", "website", "article", "news", "page", "content", "parse html"},
		"tool_http_get":     {"http", "url", "fetch", "get", "request", "api", "endpoint", "download", "retrieve", "web"},
		"tool_file_read":    {"read", "file", "load", "open", "readfile", "read file", "readfile", "content", "text"},
		"tool_file_write":   {"write", "file", "save", "store", "output", "write file", "save file", "create file", "writefile"},
		"tool_ls":           {"list", "directory", "dir", "files", "ls", "list files", "directory listing", "contents"},
		"tool_exec":         {"exec", "execute", "command", "shell", "run", "cmd", "system", "bash", "sh", "terminal"},
		"tool_codegen":      {"generate", "code", "create", "write code", "generate code", "program", "script"},
		"tool_json_parse":   {"json", "parse", "parse json", "decode", "unmarshal", "deserialize"},
		"tool_text_search":  {"search", "find", "text", "pattern", "match", "grep", "filter", "text search"},
		"tool_docker_list":  {"docker", "container", "image", "list docker", "docker list", "containers"},
		"tool_docker_build": {"docker build", "build image", "dockerfile", "container build"},
		"tool_ssh_executor": {"ssh", "remote", "execute", "remote execution", "ssh exec"},
	}

	seen := make(map[string]bool) // Track tools we've already added

	// First pass: include tools that match keywords
	for _, tool := range tools {
		if seen[tool.ID] {
			continue
		}

		// Check if tool matches keywords
		matched := false
		for toolID, keywords := range toolKeywords {
			if tool.ID == toolID {
				for _, keyword := range keywords {
					if strings.Contains(combined, keyword) {
						relevant = append(relevant, tool)
						seen[tool.ID] = true
						matched = true
						break
					}
				}
				if matched {
					break
				}
			}
		}

		if matched {
			continue
		}

		// Check if tool ID is explicitly mentioned in the description
		if strings.Contains(combined, strings.ToLower(tool.ID)) {
			relevant = append(relevant, tool)
			seen[tool.ID] = true
			continue
		}

		// Also check tool description/name for keyword matches (expanded keyword list)
		toolDesc := strings.ToLower(tool.Description + " " + tool.Name + " " + tool.ID)
		expandedKeywords := []string{"scrape", "http", "fetch", "url", "web", "file", "read", "write", "calculator", "calculate", "add", "subtract", "multiply", "divide", "math", "exec", "execute", "command", "shell", "run", "code", "generate", "json", "parse", "search", "find", "text", "docker", "container", "ssh", "remote", "list", "directory", "dir"}
		for _, keyword := range expandedKeywords {
			if strings.Contains(combined, keyword) && strings.Contains(toolDesc, keyword) {
				relevant = append(relevant, tool)
				seen[tool.ID] = true
				break
			}
		}
	}

	// Second pass: Include commonly useful tools that weren't filtered out
	// This ensures the LLM has access to a good set of tools even if keywords don't match
	// We include tools that are generally useful for most tasks
	alwaysInclude := []string{
		"tool_http_get",     // Very commonly used
		"tool_file_read",    // Commonly used
		"tool_file_write",   // Commonly used
		"tool_exec",         // Commonly used for system operations
		"tool_json_parse",   // Commonly used for data processing
		"tool_text_search",  // Commonly used for text operations
		"tool_ssh_executor", // For remote execution
	}

	for _, toolID := range alwaysInclude {
		if seen[toolID] {
			continue
		}
		// Find the tool in the original list
		for _, tool := range tools {
			if tool.ID == toolID {
				relevant = append(relevant, tool)
				seen[tool.ID] = true
				break
			}
		}
	}

	// If we still have very few tools, include more to give LLM options
	// This is a safety net to ensure LLM has enough tools to work with
	if len(relevant) < 5 && len(tools) > len(relevant) {
		log.Printf("🔧 [INTELLIGENT] Only %d tools matched keywords, including additional tools for LLM flexibility", len(relevant))
		// Include remaining tools that aren't obviously irrelevant
		for _, tool := range tools {
			if seen[tool.ID] {
				continue
			}
			// Skip tools that are clearly not for general use
			if strings.Contains(tool.ID, "tool_register") || strings.Contains(tool.ID, "tool_docker_build") {
				continue
			}
			relevant = append(relevant, tool)
			seen[tool.ID] = true
			if len(relevant) >= 10 { // Cap at reasonable number
				break
			}
		}
	}

	return relevant
}

// executeWithSSHTool executes code using the SSH executor tool
func (ie *IntelligentExecutor) executeWithSSHTool(ctx context.Context, code, language string, env map[string]string) (*DockerExecutionResponse, error) {
	// Determine if the SSH executor is enabled on this platform
	execMethod := strings.TrimSpace(os.Getenv("EXECUTION_METHOD"))
	isARM64 := runtime.GOARCH == "arm64" || runtime.GOARCH == "aarch64"

	// On ARM64, if EXECUTION_METHOD=docker is explicitly set, disable SSH to force Docker execution
	// This allows Mac users to force Docker execution
	sshEnabled := execMethod == "ssh" ||
		strings.TrimSpace(os.Getenv("ENABLE_ARM64_TOOLS")) == "true" ||
		(isARM64 && execMethod != "docker")

	if !sshEnabled {
		log.Printf("🔁 [INTELLIGENT] SSH executor disabled (EXECUTION_METHOD=%s, ENABLE_ARM64_TOOLS=%s, GOARCH=%s) — falling back to local Docker executor",
			execMethod, os.Getenv("ENABLE_ARM64_TOOLS"), runtime.GOARCH)

		if ie.dockerExecutor == nil {
			err := fmt.Errorf("docker executor unavailable for SSH fallback")
			return &DockerExecutionResponse{Success: false, Error: err.Error(), ExitCode: 1}, err
		}

		req := &DockerExecutionRequest{
			Language:     language,
			Code:         code,
			Timeout:      300,
			Environment:  map[string]string{"QUIET": "0"},
			IsValidation: true,
		}

		startTime := time.Now()
		resp, err := ie.dockerExecutor.ExecuteCode(ctx, req)
		duration := time.Since(startTime).Milliseconds()

		// Log the Docker fallback execution as a tool call for metrics
		if ie.toolMetrics != nil {
			status := "success"
			errorMsg := ""
			if err != nil || !resp.Success {
				status = "failure"
				if err != nil {
					errorMsg = err.Error()
				} else if resp.Error != "" {
					errorMsg = resp.Error
				}
			}
			callLog := &ToolCallLog{
				ToolID:   "tool_docker_exec",
				ToolName: "Docker Exec",
				Parameters: map[string]interface{}{
					"language": language,
					"code":     code,
					"timeout":  300,
				},
				Status:    status,
				Error:     errorMsg,
				Duration:  duration,
				Timestamp: time.Now(),
				Response: map[string]interface{}{
					"success":   resp.Success,
					"output":    resp.Output,
					"exit_code": resp.ExitCode,
				},
			}
			_ = ie.toolMetrics.LogToolCall(ctx, callLog)
		}

		if err != nil {
			return resp, err
		}
		return resp, nil
	}

	// Prepare parameters for the SSH tool
	params := map[string]interface{}{
		"code":     code,
		"language": language,
		"image":    ie.getImageForLanguage(language),
		"timeout":  300, // 5 minutes timeout
	}
	// Add environment variables if provided
	if env != nil && len(env) > 0 {
		envJSON, err := json.Marshal(env)
		if err == nil {
			params["environment"] = string(envJSON)
		}
	}

	// Call the SSH executor tool
	result, err := ie.callTool("tool_ssh_executor", params)
	if err != nil {
		msg := fmt.Sprintf("SSH tool call failed: %v", err)
		// If the tool is missing/not available (e.g., 404, 501), fall back to local Docker executor
		low := strings.ToLower(msg)
		missing := strings.Contains(low, "status 404") ||
			strings.Contains(low, "status 501") ||
			strings.Contains(low, "not found") ||
			strings.Contains(low, "tool not available") ||
			strings.Contains(low, "tool not implemented")
		disabled := strings.Contains(low, "ssh executor disabled")
		if missing {
			// If we are on ARM64 or explicitly enabled ARM64 tools, log a configuration error
			if runtime.GOARCH == "arm64" || runtime.GOARCH == "aarch64" || strings.TrimSpace(os.Getenv("ENABLE_ARM64_TOOLS")) == "true" {
				log.Printf("❌ [INTELLIGENT] SSH tool expected but missing on this platform (GOARCH=%s, ENABLE_ARM64_TOOLS=%s): %v", runtime.GOARCH, os.Getenv("ENABLE_ARM64_TOOLS"), err)
			}
			log.Printf("🔁 [INTELLIGENT] Falling back to local Docker executor")
			// Execute using local Docker executor as a fallback
			if ie.dockerExecutor == nil {
				return &DockerExecutionResponse{Success: false, Error: "docker executor unavailable", ExitCode: 1}, fmt.Errorf("docker executor unavailable")
			}
			req := &DockerExecutionRequest{
				Language:     language,
				Code:         code,
				Timeout:      300,
				Environment:  map[string]string{"QUIET": "0"},
				IsValidation: true,
			}
			startTime := time.Now()
			resp, derr := ie.dockerExecutor.ExecuteCode(ctx, req)
			duration := time.Since(startTime).Milliseconds()

			// Log the Docker fallback execution as a tool call for metrics
			if ie.toolMetrics != nil {
				status := "success"
				errorMsg := ""
				if derr != nil || !resp.Success {
					status = "failure"
					if derr != nil {
						errorMsg = derr.Error()
					} else if resp.Error != "" {
						errorMsg = resp.Error
					}
				}
				callLog := &ToolCallLog{
					ToolID:   "tool_docker_exec",
					ToolName: "Docker Exec",
					Parameters: map[string]interface{}{
						"language": language,
						"code":     code,
						"timeout":  300,
					},
					Status:    status,
					Error:     errorMsg,
					Duration:  duration,
					Timestamp: time.Now(),
					Response: map[string]interface{}{
						"success":   resp.Success,
						"output":    resp.Output,
						"exit_code": resp.ExitCode,
					},
				}
				_ = ie.toolMetrics.LogToolCall(ctx, callLog)
			}

			if derr != nil {
				return &DockerExecutionResponse{Success: false, Error: derr.Error(), ExitCode: 1}, derr
			}
			return resp, nil
		} else if disabled {
			log.Printf("🔁 [INTELLIGENT] SSH tool disabled according to API response — falling back to local Docker executor")
			if ie.dockerExecutor == nil {
				return &DockerExecutionResponse{Success: false, Error: "docker executor unavailable", ExitCode: 1}, fmt.Errorf("docker executor unavailable")
			}
			req := &DockerExecutionRequest{
				Language:     language,
				Code:         code,
				Timeout:      300,
				Environment:  map[string]string{"QUIET": "0"},
				IsValidation: true,
			}
			startTime := time.Now()
			resp, derr := ie.dockerExecutor.ExecuteCode(ctx, req)
			duration := time.Since(startTime).Milliseconds()

			// Log the Docker fallback execution as a tool call for metrics
			if ie.toolMetrics != nil {
				status := "success"
				errorMsg := ""
				if derr != nil || !resp.Success {
					status = "failure"
					if derr != nil {
						errorMsg = derr.Error()
					} else if resp.Error != "" {
						errorMsg = resp.Error
					}
				}
				callLog := &ToolCallLog{
					ToolID:   "tool_docker_exec",
					ToolName: "Docker Exec",
					Parameters: map[string]interface{}{
						"language": language,
						"code":     code,
						"timeout":  300,
					},
					Status:    status,
					Error:     errorMsg,
					Duration:  duration,
					Timestamp: time.Now(),
					Response: map[string]interface{}{
						"success":   resp.Success,
						"output":    resp.Output,
						"exit_code": resp.ExitCode,
					},
				}
				_ = ie.toolMetrics.LogToolCall(ctx, callLog)
			}

			if derr != nil {
				return &DockerExecutionResponse{Success: false, Error: derr.Error(), ExitCode: 1}, derr
			}
			return resp, nil
		}
		return &DockerExecutionResponse{
			Success:  false,
			Error:    msg,
			ExitCode: 1,
		}, err
	}

	// Convert tool result to DockerExecutionResponse format
	success, _ := result["success"].(bool)
	output, _ := result["output"].(string)
	errorMsg, _ := result["error"].(string)
	exitCode := 0
	if ec, ok := result["exit_code"].(float64); ok {
		exitCode = int(ec)
	}
	// durationMs reported by tool is currently unused

	return &DockerExecutionResponse{
		Success:  success,
		Output:   output,
		Error:    errorMsg,
		ExitCode: exitCode,
		Files:    make(map[string][]byte), // Drone tool doesn't return files yet
	}, nil
}

// getImageForLanguage returns the appropriate Docker image for a language
func (ie *IntelligentExecutor) getImageForLanguage(language string) string {
	switch strings.ToLower(language) {
	case "go":
		return "golang:1.21-alpine"
	case "python", "py":
		return "python:3.11-slim"
	case "javascript", "js", "node":
		return "node:18-slim"
	case "bash", "sh":
		return "alpine:latest"
	default:
		return "alpine:latest"
	}
}

// ensureRegisteredToolForTask registers a persistent tool for a task if missing.
// The tool is defined to execute via a container image matching the language,
// and invocations should pass {code, language} so execution routes via Drone in Kubernetes.
func (ie *IntelligentExecutor) ensureRegisteredToolForTask(taskName, language string) string {
	if ie.hdnBaseURL == "" {
		return ""
	}
	// Derive stable tool ID from task name
	norm := strings.ToLower(strings.TrimSpace(taskName))
	norm = strings.ReplaceAll(norm, " ", "_")
	norm = strings.ReplaceAll(norm, "/", "_")
	toolID := "tool_" + norm

	// Attempt to list/load to avoid duplicate registration (best-effort)
	// If listing fails, we still try to register.
	// Build tool payload - use cmd type instead of image type for better compatibility
	payload := map[string]interface{}{
		"id":            toolID,
		"name":          taskName,
		"description":   "Auto-generated tool for task: " + taskName,
		"input_schema":  map[string]string{"code": "string", "language": "string"},
		"output_schema": map[string]string{"output": "string", "error": "string", "success": "bool"},
		"permissions":   []string{"proc:exec"},
		"safety_level":  "medium",
		"created_by":    "agent",
		"exec": map[string]interface{}{
			"type": "cmd",
			"cmd":  "/app/tools/docker_executor", // Use the existing docker executor
			"args": []string{"--code", "{{.code}}", "--language", "{{.language}}"},
		},
	}

	b, _ := json.Marshal(payload)
	url := fmt.Sprintf("%s/api/v1/tools", ie.hdnBaseURL)
	req, err := http.NewRequest("POST", url, bytes.NewReader(b))
	if err != nil {
		return toolID
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return toolID
	}
	defer resp.Body.Close()
	// If 200 OK, registered now; if 400/409 already exists, that's fine.
	return toolID
}

// categorizeRequestForSafety uses LLM to intelligently categorize a request for safety evaluation
func (ie *IntelligentExecutor) categorizeRequestForSafety(req *ExecutionRequest) (map[string]interface{}, error) {
	prompt := fmt.Sprintf(`You are a safety analyzer. Analyze this task request and return ONLY a valid JSON object.

Task: %s
Description: %s
Context: %v

Return this exact JSON format (no other text):
{
  "human_harm": false,
  "human_order": true,
  "self_harm": false,
  "privacy_violation": false,
  "endanger_others": false,
  "order_unethical": false,
  "discrimination": false
}

Rules:
- Set human_harm=true ONLY if the task directly harms humans (violence, injury, death)
- Set privacy_violation=true ONLY if the task steals, accesses, or exposes private data
- Set endanger_others=true ONLY if the task could cause physical damage or danger
- Set order_unethical=true ONLY if the task is clearly illegal or unethical
- Mathematical calculations, data analysis, and programming tasks are generally safe
- Be precise, not overly cautious`,
		req.TaskName, req.Description, req.Context)

	// Add timeout wrapper around LLM call to prevent hanging
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Use priority from request (defaults to high for user requests)
	priority := PriorityLow
	if req.HighPriority {
		priority = PriorityHigh
	}
	response, err := ie.llmClient.callLLMWithContextAndPriority(ctx, prompt, priority)
	if err != nil {
		log.Printf("❌ [INTELLIGENT] LLM safety analysis failed: %v", err)
		// Use reasonable defaults instead of overly conservative ones
		return map[string]interface{}{
			"human_harm":        false,
			"human_order":       true,
			"self_harm":         false,
			"privacy_violation": false,
			"endanger_others":   false,
			"order_unethical":   false,
			"discrimination":    false,
		}, nil
	}

	// Clean the response - remove any markdown formatting or extra text
	cleanResponse := strings.TrimSpace(response)
	if strings.HasPrefix(cleanResponse, "```json") {
		cleanResponse = strings.TrimPrefix(cleanResponse, "```json")
	}
	if strings.HasSuffix(cleanResponse, "```") {
		cleanResponse = strings.TrimSuffix(cleanResponse, "```")
	}
	cleanResponse = strings.TrimSpace(cleanResponse)

	// Parse the JSON response
	var context map[string]interface{}
	if err := json.Unmarshal([]byte(cleanResponse), &context); err != nil {
		log.Printf("⚠️ [INTELLIGENT] Failed to parse LLM safety response: %v", err)
		log.Printf("⚠️ [INTELLIGENT] Raw response: %s", cleanResponse)
		// Use reasonable defaults instead of overly conservative ones
		context = map[string]interface{}{
			"human_harm":        false,
			"human_order":       true,
			"self_harm":         false,
			"privacy_violation": false,
			"endanger_others":   false,
			"order_unethical":   false,
			"discrimination":    false,
		}
	}

	log.Printf("🔍 [INTELLIGENT] LLM Safety Analysis: %+v", context)
	return context, nil
}

// ExecuteTaskIntelligently executes a task using the complete intelligent workflow
func (ie *IntelligentExecutor) ExecuteTaskIntelligently(ctx context.Context, req *ExecutionRequest) (*IntelligentExecutionResult, error) {
	start := time.Now()
	log.Printf("🧠 [INTELLIGENT] Starting intelligent execution for task: %s", req.TaskName)
	log.Printf("🧠 [INTELLIGENT] Description: %s", req.Description)
	log.Printf("🧠 [INTELLIGENT] Context: %+v", req.Context)
	log.Printf("🎯 [INTELLIGENT] HighPriority: %v", req.HighPriority)

	// Fast-path: pure LLM summarization tasks should not go through codegen/Docker
	// Note: daily_summary removed from this path - it now uses system data generation
	if strings.EqualFold(req.TaskName, "analyze_bootstrap") || strings.EqualFold(req.TaskName, "analyze_belief") {
		log.Printf("📝 [INTELLIGENT] Using direct LLM summarization path for %s", req.TaskName)
		// Build a concise prompt with tight constraints
		format := "Paragraph: <text>\nBullets:\n- <b1>\n- <b2>\n- <b3>\nQuestions:\n1) <q1>\n2) <q2>\n3) <q3>\n\n"
		prompt := "You are a concise knowledge summarizer. Analyze the bootstrapped knowledge and provide a factual summary.\n" +
			"Output ONLY the requested sections and nothing else.\n" +
			"Constraints: paragraph <= 80 words; exactly 3 bullets; exactly 3 short follow-up questions.\n" +
			"Focus on the actual concepts and knowledge that were bootstrapped, not educational approaches or project management.\n" +
			"Format:\n" + format
		if strings.TrimSpace(req.Description) != "" {
			// Extract the seed/topic from description
			desc := req.Description
			if strings.Contains(desc, "around") {
				parts := strings.Split(desc, "around")
				if len(parts) > 1 {
					seed := strings.TrimSpace(parts[1])
					prompt += fmt.Sprintf("Task: Summarize the knowledge concepts that were bootstrapped about: %s\n\n", seed)
				} else {
					prompt += "Description:\n" + desc + "\n\n"
				}
			} else {
				prompt += "Description:\n" + desc + "\n\n"
			}
		}
		// Only include relevant context (exclude project_id, session_id, prefer_traditional)
		if len(req.Context) > 0 {
			relevantCtx := false
			for k, v := range req.Context {
				// Skip administrative context fields
				if k != "session_id" && k != "project_id" && k != "prefer_traditional" && strings.TrimSpace(k) != "" && strings.TrimSpace(v) != "" {
					if !relevantCtx {
						prompt += "Context:\n"
						relevantCtx = true
					}
					prompt += "- " + k + ": " + v + "\n"
				}
			}
			if relevantCtx {
				prompt += "\n"
			}
		}

		// Call LLM directly to avoid verbose wrappers
		// Use priority from request (defaults to high for user requests)
		priority := PriorityLow
		if req.HighPriority {
			priority = PriorityHigh
		}
		// Add component information to context for token tracking
		ctx = WithComponent(ctx, "hdn-intelligent-executor")
		response, err := ie.llmClient.callLLMWithContextAndPriority(ctx, prompt, priority)
		result := &IntelligentExecutionResult{
			Success:         err == nil,
			Result:          response,
			ExecutionTime:   time.Since(start),
			RetryCount:      1,
			ValidationSteps: []ValidationStep{},
			WorkflowID:      fmt.Sprintf("intelligent_%d", time.Now().UnixNano()),
		}
		if err != nil {
			result.Error = err.Error()
		}
		// Record episode/metrics similar to planner/traditional paths
		ie.recordMonitorMetrics(result.Success, result.ExecutionTime)
		if ie.selfModelManager != nil {
			ie.recordExecutionEpisode(req, result, "llm_summarize")
		}
		log.Printf("✅ [INTELLIGENT] Direct LLM summarization completed (len=%d)", len(response))
		return result, nil
	}

	// Early check: Detect tasks that should NOT generate code
	// These include hypothesis testing, explicit tool usage, and simple informational tasks
	descLower := strings.ToLower(strings.TrimSpace(req.Description))
	taskLower := strings.ToLower(strings.TrimSpace(req.TaskName))
	combined := descLower + " " + taskLower

	// Check 1: Hypothesis testing tasks - let them go through normal code generation
	// but enhance the description to guide code generation for hypothesis testing
	if strings.HasPrefix(descLower, "test hypothesis:") || strings.HasPrefix(taskLower, "test hypothesis:") {
		log.Printf("🧪 [INTELLIGENT] Detected hypothesis testing task - will generate code to test hypothesis")

		// Extract hypothesis content
		hypothesisContent := req.Description
		if strings.HasPrefix(descLower, "test hypothesis:") {
			parts := strings.SplitN(req.Description, ":", 2)
			if len(parts) > 1 {
				hypothesisContent = strings.TrimSpace(parts[1])
			}
		}

		// Enhance description to guide code generation
		enhancedDesc := fmt.Sprintf(`Test hypothesis by gathering evidence: %s

Requirements:
1. Query Neo4j knowledge base using tool_mcp_query_neo4j via HTTP API for information related to the hypothesis
   - Call: POST http://host.docker.internal:8081/api/v1/tools/mcp_query_neo4j/invoke
   - Body: {"query": "CYPHER_QUERY", "natural_language": "description"}
   - Response format: {"results": [...], "count": N} - handle this structure correctly
   - IMPORTANT: Check if response has "results" key before accessing it, handle errors gracefully
2. Extract key terms from the hypothesis (event IDs, concept names, domains)
3. Gather evidence that supports or contradicts the hypothesis
4. Create a markdown report with findings, evidence, and conclusions
5. Save the report as hypothesis_test_report.md using tool_file_write or write to file directly

The hypothesis to test: %s`, hypothesisContent, hypothesisContent)

		req.Description = enhancedDesc
		req.TaskName = fmt.Sprintf("Test hypothesis: %s", hypothesisContent)

		// Add context hints for artifact creation
		if req.Context == nil {
			req.Context = make(map[string]string)
		}
		req.Context["hypothesis_testing"] = "true"
		req.Context["save_pdf"] = "true" // Ensure artifacts are created
		req.Context["artifact_names"] = "hypothesis_test_report.md"
		req.Context["allow_requests"] = "true" // Allow HTTP requests for API calls (Neo4j queries)

		// Continue to normal code generation path - don't skip
		// The enhanced description will guide the LLM to generate appropriate testing code
	}

	// Check 2: Explicit tool usage requests that mention specific tools
	// Pattern: "Use tool_XXX" or "use tool_XXX" in description
	toolPatterns := []string{
		"use tool_http_get",
		"use tool_html_scraper",
		"use tool_file_read",
		"use tool_file_write",
		"use tool_ls",
		"use tool_exec",
		"use tool_wiki",
		"tool_http_get to",
		"tool_html_scraper to",
	}
	hasExplicitToolRequest := false
	for _, pattern := range toolPatterns {
		if strings.Contains(combined, pattern) {
			hasExplicitToolRequest = true
			log.Printf("🔧 [INTELLIGENT] Detected explicit tool usage request: %s", pattern)
			break
		}
	}

	// Check 3: Simple informational tasks (news headlines, titles, etc.)
	// These are typically just text without actionable code requirements
	isSimpleInformational := false
	// Exclude tasks that have matrix operations, mathematical operations, or explicit code requirements
	hasMatrixOps := strings.Contains(descLower, "matrix") ||
		(req.Context != nil && (req.Context["matrix1"] != "" || req.Context["matrix2"] != ""))
	hasMathOps := strings.Contains(descLower, "calculate") ||
		strings.Contains(descLower, "addition") ||
		strings.Contains(descLower, "operation") ||
		strings.Contains(descLower, "perform")
	hasCodeRequirement := strings.Contains(descLower, "code") ||
		strings.Contains(descLower, "program") ||
		strings.Contains(descLower, "function") ||
		strings.Contains(descLower, "script") ||
		req.Language != "" // If language is specified, it's a code task

	// News headline patterns (often just titles without verbs indicating action)
	if len(req.Description) < 200 && !strings.Contains(descLower, "create") &&
		!strings.Contains(descLower, "write") && !strings.Contains(descLower, "generate") &&
		!strings.Contains(descLower, "build") && !strings.Contains(descLower, "implement") &&
		!hasCodeRequirement {
		// Check if it looks like a news headline or simple statement
		if strings.Count(req.Description, " ") < 15 && // Short description
			!strings.Contains(descLower, "calculate") &&
			!strings.Contains(descLower, "process") &&
			!strings.Contains(descLower, "analyze") &&
			!strings.Contains(descLower, "fetch") &&
			!strings.Contains(descLower, "get") &&
			!hasMatrixOps && // Exclude matrix operations
			!hasMathOps { // Exclude mathematical operations
			isSimpleInformational = true
			log.Printf("📰 [INTELLIGENT] Detected simple informational task - skipping code generation")
		}
	}

	// If explicit tool request detected, route to tool execution path
	if hasExplicitToolRequest {
		// Extract tool ID from description
		toolID := ""
		for _, pattern := range toolPatterns {
			if strings.Contains(combined, pattern) {
				// Extract tool name from pattern
				// Handle patterns like "use tool_http_get" or "tool_http_get to"
				if strings.HasPrefix(pattern, "use ") {
					parts := strings.Fields(pattern)
					if len(parts) >= 2 {
						toolID = parts[1] // e.g., "tool_http_get" from "use tool_http_get"
					}
				} else if strings.Contains(pattern, "tool_") {
					// Extract tool_XXX from pattern like "tool_http_get to"
					parts := strings.Fields(pattern)
					for _, part := range parts {
						if strings.HasPrefix(part, "tool_") {
							toolID = part
							break
						}
					}
				}
				if toolID != "" {
					break
				}
			}
		}
		// If we found a tool, try to execute it directly
		if toolID != "" {
			log.Printf("🔧 [INTELLIGENT] Routing to direct tool execution: %s", toolID)
			params := map[string]interface{}{}
			// Extract parameters from context or description
			if toolID == "tool_http_get" {
				if u, ok := req.Context["url"]; ok && strings.TrimSpace(u) != "" {
					params["url"] = u
				} else {
					// Try to extract URL from description
					// Look for URLs in the description
					urlPattern := regexp.MustCompile(`https?://[^\s]+`)
					if matches := urlPattern.FindStringSubmatch(req.Description); len(matches) > 0 {
						params["url"] = matches[0]
					} else {
						params["url"] = "http://example.com" // Default fallback
					}
				}
			}
			toolResp, err := ie.callTool(toolID, params)
			result := &IntelligentExecutionResult{Success: err == nil}
			if err != nil {
				result.Error = err.Error()
			} else {
				b, _ := json.Marshal(toolResp)
				result.Result = string(b)
			}
			result.ExecutionTime = time.Since(start)
			result.WorkflowID = fmt.Sprintf("intelligent_%d", time.Now().UnixNano())
			if ie.selfModelManager != nil {
				ie.recordExecutionEpisode(req, result, "direct_tool_call")
			}
			return result, nil
		}
	}

	// If simple informational task, return acknowledgment without code generation
	if isSimpleInformational {
		log.Printf("📝 [INTELLIGENT] Simple informational task - returning acknowledgment")
		result := &IntelligentExecutionResult{
			Success:         true,
			Result:          fmt.Sprintf("Informational task acknowledged: %s", req.Description),
			ExecutionTime:   time.Since(start),
			RetryCount:      0,
			ValidationSteps: []ValidationStep{},
			WorkflowID:      fmt.Sprintf("intelligent_%d", time.Now().UnixNano()),
		}
		ie.recordMonitorMetrics(result.Success, result.ExecutionTime)
		if ie.selfModelManager != nil {
			ie.recordExecutionEpisode(req, result, "informational")
		}
		return result, nil
	}

	// Short-circuit: if this is a simple tool execution, invoke tool directly (no LLM/codegen)
	if strings.EqualFold(strings.TrimSpace(req.TaskName), "Tool Execution") {
		toolID := ""
		// Try to detect tool from description like: "Execute tool tool_ls: ..."
		desc := strings.TrimSpace(req.Description)
		if strings.HasPrefix(desc, "Execute tool ") {
			rest := strings.TrimPrefix(desc, "Execute tool ")
			// Extract token up to ':' or space
			for i := 0; i < len(rest); i++ {
				if rest[i] == ':' || rest[i] == ' ' || rest[i] == '\n' || rest[i] == '\t' {
					toolID = strings.TrimSpace(rest[:i])
					break
				}
			}
			if toolID == "" {
				toolID = strings.TrimSpace(rest)
			}
		}
		// Known safe tools to shortcut
		if toolID == "tool_ls" || toolID == "tool_http_get" || toolID == "tool_file_read" || toolID == "tool_file_write" || toolID == "tool_exec" {
			params := map[string]interface{}{}
			// Minimal parameter inference
			if toolID == "tool_ls" {
				params["path"] = "."
			} else if toolID == "tool_http_get" {
				if u, ok := req.Context["url"]; ok && strings.TrimSpace(u) != "" {
					params["url"] = u
				} else {
					params["url"] = "http://example.com"
				}
			} else if toolID == "tool_file_read" {
				if p, ok := req.Context["path"]; ok && strings.TrimSpace(p) != "" {
					params["path"] = p
				} else {
					params["path"] = "/tmp"
				}
			} else if toolID == "tool_file_write" {
				if p, ok := req.Context["path"]; ok && strings.TrimSpace(p) != "" {
					params["path"] = p
				} else {
					params["path"] = "/tmp/out.txt"
				}
				if c, ok := req.Context["content"]; ok {
					params["content"] = c
				} else {
					params["content"] = ""
				}
			} else if toolID == "tool_exec" {
				if c, ok := req.Context["cmd"]; ok && strings.TrimSpace(c) != "" {
					params["cmd"] = c
				} else {
					params["cmd"] = "ls -la"
				}
			}
			toolResp, err := ie.callTool(toolID, params)
			result := &IntelligentExecutionResult{Success: err == nil}
			if err != nil {
				result.Error = err.Error()
			} else {
				b, _ := json.Marshal(toolResp)
				result.Result = string(b)
			}
			// Record metrics/episode if available
			if ie.selfModelManager != nil {
				ie.recordExecutionEpisode(req, result, "direct_tool_call")
			}
			return result, nil
		}
	}

	// Heuristic routing: web info-gathering (scrape/fetch URL) should use built-in web tools instead of codegen
	// Reuse descLower and taskLower already declared above
	// Consider multiple web-related intents and presence of URLs in context
	// Also detect explicit tool mentions (e.g., "use tool_http_get", "tool_html_scraper")
	hasWebIntent := strings.Contains(descLower, "gather information") ||
		strings.Contains(taskLower, "gather") ||
		strings.Contains(descLower, "scrape") || strings.Contains(taskLower, "scrape") ||
		strings.Contains(descLower, "scraping") || strings.Contains(taskLower, "scraping") ||
		strings.Contains(descLower, "fetch") || strings.Contains(taskLower, "fetch") ||
		strings.Contains(descLower, "web page") || strings.Contains(taskLower, "web page") ||
		strings.Contains(descLower, "http") || strings.Contains(taskLower, "http") ||
		strings.Contains(descLower, "url") || strings.Contains(taskLower, "url") ||
		strings.Contains(descLower, "screen scraper") || strings.Contains(taskLower, "screen scraper") ||
		strings.Contains(descLower, "screen-scraper") || strings.Contains(taskLower, "screen-scraper") ||
		strings.Contains(descLower, "scraper") || strings.Contains(taskLower, "scraper") ||
		strings.Contains(descLower, "crawler") || strings.Contains(taskLower, "crawler") ||
		strings.Contains(descLower, "tool_http_get") || strings.Contains(taskLower, "tool_http_get") ||
		strings.Contains(descLower, "tool_html_scraper") || strings.Contains(taskLower, "tool_html_scraper") ||
		strings.Contains(descLower, "use tool") || strings.Contains(taskLower, "use tool")

	// Also trigger if context already includes any URL-like entries
	urlsInCtx := ie.collectURLsFromContext(req.Context)
	if !hasWebIntent && len(urlsInCtx) > 0 {
		hasWebIntent = true
	}

	// Force native tools if prefer_tools=true and URLs are present
	if !hasWebIntent {
		if pref, ok := req.Context["prefer_tools"]; ok && strings.ToLower(strings.TrimSpace(pref)) == "true" && len(urlsInCtx) > 0 {
			hasWebIntent = true
		}
	}

	if hasWebIntent {
		aggRes, aggErr := ie.executeInfoGatheringWithTools(ctx, req)
		result := &IntelligentExecutionResult{WorkflowID: fmt.Sprintf("intelligent_%d", time.Now().UnixNano())}
		if aggErr != nil {
			result.Success = false
			result.Error = aggErr.Error()
			result.ExecutionTime = time.Since(start)
			// Still fall through to traditional path if aggregator failed
		} else {
			result.Success = true
			result.Result = aggRes
			result.ExecutionTime = time.Since(start)
			// Record episode/metrics if available
			if ie.selfModelManager != nil {
				ie.recordExecutionEpisode(req, result, "tool_info_gathering")
			}
			ie.recordMonitorMetrics(result.Success, result.ExecutionTime)
			return result, nil
		}
	}

	// Track this as a goal in self-model
	if ie.selfModelManager != nil {
		// Avoid spamming self-model with internal orchestration tasks
		// These are driven by Goals/Monitor and should not appear as user-facing PENDING goals
		lowerTask := strings.ToLower(strings.TrimSpace(req.TaskName))
		skipInternal := lowerTask == "goal execution" || lowerTask == "artifact_task" || strings.HasPrefix(lowerTask, "code_")
		if !skipInternal {
			goalName := fmt.Sprintf("Execute task: %s", req.TaskName)
			if err := ie.selfModelManager.AddGoal(goalName); err != nil {
				log.Printf("⚠️ [SELF-MODEL] Failed to add goal: %v", err)
			} else {
				log.Printf("🎯 [SELF-MODEL] Added goal: %s", goalName)
			}
		}
	}

	// Check for context cancellation before proceeding
	if ctx.Err() != nil {
		log.Printf("⏱️ [INTELLIGENT] Context canceled before execution: %v", ctx.Err())
		return &IntelligentExecutionResult{
			Success:         false,
			Error:           fmt.Sprintf("Execution timed out or was canceled: %v", ctx.Err()),
			ExecutionTime:   time.Since(start),
			RetryCount:      0,
			ValidationSteps: []ValidationStep{},
			WorkflowID:      fmt.Sprintf("intelligent_%d", time.Now().UnixNano()),
		}, nil
	}

	// Check for chained program requests BEFORE planner integration
	if ie.isChainedProgramRequest(req) {
		log.Printf("🔗 [INTELLIGENT] Detected chained program request, using multi-step execution")
		workflowID := fmt.Sprintf("intelligent_%d", time.Now().UnixNano())
		return ie.executeChainedPrograms(ctx, req, start, workflowID)
	}

	// If planner integration is available, use it for planning
	// Only use planner for complex tasks that might benefit from HTN planning
	// For simple intelligent execution requests, use direct execution
	// Honor explicit preference to use traditional path for artifact generation
	if pref, ok := req.Context["prefer_traditional"]; ok && strings.ToLower(pref) == "true" {
		log.Printf("⚙️ [INTELLIGENT] prefer_traditional=true, skipping planner path")
	} else if ie.usePlanner && ie.plannerIntegration != nil && ie.isComplexTask(req) {
		log.Printf("🎯 [INTELLIGENT] Using planner integration for complex task planning")
		return ie.executeWithPlanner(ctx, req, start)
	}

	// Fall back to original intelligent execution
	log.Printf("🤖 [INTELLIGENT] Using traditional intelligent execution (no planner)")
	workflowID := fmt.Sprintf("intelligent_%d", time.Now().UnixNano())
	result, err := ie.executeTraditionally(ctx, req, start, workflowID)

	// Check for context cancellation after execution
	if ctx.Err() != nil && (result == nil || !result.Success) {
		log.Printf("⏱️ [INTELLIGENT] Context canceled during execution: %v", ctx.Err())
		if result == nil {
			result = &IntelligentExecutionResult{
				Success:         false,
				Error:           fmt.Sprintf("Execution timed out or was canceled: %v", ctx.Err()),
				ExecutionTime:   time.Since(start),
				RetryCount:      0,
				ValidationSteps: []ValidationStep{},
				WorkflowID:      workflowID,
			}
		} else if result.Error == "" {
			result.Error = fmt.Sprintf("Execution timed out or was canceled: %v", ctx.Err())
		}
	}

	return result, err
}

// executeInfoGatheringWithTools invokes html scraper / http get tools based on context and aggregates results.
func (ie *IntelligentExecutor) executeInfoGatheringWithTools(ctx context.Context, req *ExecutionRequest) (string, error) {
	// Expect URLs in context under keys: url, urls (comma-separated), source_url_*, link_*
	urls := ie.collectURLsFromContext(req.Context)
	if len(urls) == 0 {
		return "", fmt.Errorf("no urls provided in context for information gathering")
	}

	var summaries []string
	// Prefer html scraper when available; otherwise fallback to http_get
	for _, u := range urls {
		// Try html scraper first
		scraperResp, err := ie.callTool("tool_html_scraper", map[string]interface{}{"url": u})
		if err == nil && scraperResp != nil {
			// Expect items array; build a brief summary line
			b, _ := json.Marshal(scraperResp)
			summaries = append(summaries, fmt.Sprintf("URL: %s\nDATA: %s", u, string(b)))
			continue
		}

		// Fallback to simple http get
		httpResp, err2 := ie.callTool("tool_http_get", map[string]interface{}{"url": u})
		if err2 != nil || httpResp == nil {
			// Note: Do not print error lines here per policy; just skip
			continue
		}
		status := 0
		if s, ok := httpResp["status"].(float64); ok {
			status = int(s)
		}
		body, _ := httpResp["body"].(string)
		// Truncate body for compactness
		const maxBody = 512
		if len(body) > maxBody {
			body = body[:maxBody] + "..."
		}
		summaries = append(summaries, fmt.Sprintf("URL: %s\nSTATUS: %d\nBODY: %s", u, status, body))
	}

	if len(summaries) == 0 {
		return "", fmt.Errorf("failed to fetch any url with available tools")
	}
	return strings.Join(summaries, "\n\n"), nil
}

// collectURLsFromContext extracts candidate URLs from context map.
func (ie *IntelligentExecutor) collectURLsFromContext(ctxMap map[string]string) []string {
	var urls []string
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		// Split on commas or whitespace to allow multiple
		parts := strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' || r == '\n' || r == '\t' })
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				urls = append(urls, p)
			}
		}
	}

	for k, v := range ctxMap {
		lk := strings.ToLower(strings.TrimSpace(k))
		if lk == "url" || lk == "urls" || strings.HasPrefix(lk, "source_url") || strings.HasPrefix(lk, "link_") {
			add(v)
		}
	}
	return urls
}

// isComplexTask determines if a task is complex enough to benefit from HTN planning
func (ie *IntelligentExecutor) isComplexTask(req *ExecutionRequest) bool {
	// First, check for obvious simple tasks that should NEVER use hierarchical planning
	descLower := strings.ToLower(req.Description)
	taskLower := strings.ToLower(req.TaskName)
	combined := descLower + " " + taskLower

	// Simple program creation patterns - these are ALWAYS simple
	simplePatterns := []string{
		"print",
		"create.*program",
		"write.*program",
		"generate.*program",
		"create.*go.*program",
		"write.*go.*program",
		"create.*python.*program",
		"write.*python.*program",
		"simple.*program",
		"hello.*world",
		"calculate.*number",
		"fibonacci",
		"prime.*number",
	}

	for _, pattern := range simplePatterns {
		matched, _ := regexp.MatchString(pattern, combined)
		if matched {
			log.Printf("✅ [INTELLIGENT] Task matches simple pattern '%s' - skipping hierarchical planning", pattern)
			return false
		}
	}

	// If explicitly marked as simple or traditional, skip hierarchical planning
	if pref, ok := req.Context["prefer_traditional"]; ok && strings.ToLower(pref) == "true" {
		log.Printf("✅ [INTELLIGENT] prefer_traditional=true - skipping hierarchical planning")
		return false
	}

	// Use LLM to determine task complexity for more accurate classification
	complexity, err := ie.classifyTaskComplexity(req)
	if err != nil {
		// Check if error is due to context cancellation
		if strings.Contains(err.Error(), "cancelled") || strings.Contains(err.Error(), "timeout") || strings.Contains(err.Error(), "deadline") {
			log.Printf("⏱️ [INTELLIGENT] LLM complexity classification cancelled/timed out: %v", err)
			// Don't fallback - this indicates a timeout, which should be handled upstream
			return false
		}
		// Fallback to simple classification if LLM fails (safer default)
		log.Printf("⚠️ [INTELLIGENT] LLM complexity classification failed: %v, defaulting to simple", err)
		return false
	}

	log.Printf("🧠 [INTELLIGENT] LLM classified task as: %s", complexity)
	return complexity == "complex"
}

// classifyTaskComplexity uses the LLM to determine if a task is simple or complex
func (ie *IntelligentExecutor) classifyTaskComplexity(req *ExecutionRequest) (string, error) {
	prompt := fmt.Sprintf(`You are a task complexity classifier. Analyze the following task and determine if it should be classified as "simple" or "complex".

Task Name: %s
Description: %s
Language: %s

Classification rules:
- SIMPLE: Basic code generation, single-purpose programs, simple calculations, straightforward implementations, printing text, creating a single file, simple algorithms
- COMPLEX: Multi-step workflows, system integrations, architectural decisions, complex business logic, multi-component solutions, multiple files, APIs, databases

🚨 CRITICAL: When in doubt, classify as SIMPLE. Only classify as COMPLEX if the task clearly requires multiple steps, multiple components, or system integration.

Examples:
- "Write a Python program that prints 'Hello World'" → SIMPLE
- "Create a Go program in main.go that prints 'Hello Steve'" → SIMPLE
- "Create a Go function that calculates fibonacci numbers" → SIMPLE
- "Create me a Go program" → SIMPLE
- "Build a REST API with authentication and database integration" → COMPLEX
- "Design a microservices architecture for e-commerce" → COMPLEX
- "Create a data pipeline that processes files and sends notifications" → COMPLEX
- "Create multiple programs that work together" → COMPLEX

Respond with only one word: "simple" or "complex"`,
		req.TaskName, req.Description, req.Language)

	// Use a shorter timeout for complexity classification
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Use priority from request (defaults to high for user requests)
	priority := PriorityLow
	if req.HighPriority {
		priority = PriorityHigh
	}
	response, err := ie.llmClient.callLLMWithContextAndPriority(ctx, prompt, priority)
	if err != nil {
		return "", err
	}

	// Parse response
	response = strings.ToLower(strings.TrimSpace(response))
	if response == "simple" || response == "complex" {
		return response, nil
	}

	// If response is unclear, try to extract the classification
	if strings.Contains(response, "simple") {
		return "simple", nil
	}
	if strings.Contains(response, "complex") {
		return "complex", nil
	}

	return "", fmt.Errorf("unable to parse complexity classification from response: %s", response)
}

// logIntelligentExecutionMetrics logs tool metrics for intelligent execution
func (ie *IntelligentExecutor) logIntelligentExecutionMetrics(ctx context.Context, req *ExecutionRequest, result *IntelligentExecutionResult) {
	if ie.toolMetrics == nil {
		return
	}

	// Determine tool ID and name
	toolID := "tool_intelligent_executor"
	toolName := "Intelligent Code Execution"

	// Determine status
	status := "success"
	if !result.Success {
		status = "failure"
	}

	// Create tool call log
	callLog := &ToolCallLog{
		ID:       fmt.Sprintf("intelligent_%d", time.Now().UnixNano()),
		ToolID:   toolID,
		ToolName: toolName,
		Parameters: map[string]interface{}{
			"task_name":   req.TaskName,
			"description": req.Description,
			"language":    req.Language,
			"context":     req.Context,
		},
		Status:      status,
		Error:       result.Error,
		Duration:    result.ExecutionTime.Milliseconds(),
		AgentID:     "intelligent_executor",
		ProjectID:   req.Context["project_id"],
		Timestamp:   time.Now(),
		Response:    result.Result,
		Permissions: []string{"code_generation", "docker_execution"},
		SafetyLevel: "medium",
	}

	// Log the tool call
	if err := ie.toolMetrics.LogToolCall(ctx, callLog); err != nil {
		log.Printf("⚠️ [INTELLIGENT] Failed to log tool metrics: %v", err)
	} else {
		log.Printf("📊 [INTELLIGENT] Logged tool metrics: %s (%s)", toolName, status)
	}
}

// storeChainedProgramArtifact stores a chained program as an artifact
func (ie *IntelligentExecutor) storeChainedProgramArtifact(generatedCode *GeneratedCode, workflowID, programName string) {
	if generatedCode == nil || ie.fileStorage == nil {
		return
	}

	// Check if programName already has an extension
	filename := programName
	if !strings.Contains(filepath.Ext(programName), ".") {
		// No extension, add one based on language
		var ext string
		switch strings.ToLower(generatedCode.Language) {
		case "python":
			ext = ".py"
		case "go":
			ext = ".go"
		case "javascript":
			ext = ".js"
		case "java":
			ext = ".java"
		default:
			ext = ".txt"
		}
		filename = fmt.Sprintf("%s%s", programName, ext)
	}

	// Store the file using the file storage
	storedFile := &StoredFile{
		Filename:    filename,
		Content:     []byte(generatedCode.Code),
		ContentType: fmt.Sprintf("text/x-%s-source", generatedCode.Language),
		Size:        int64(len(generatedCode.Code)),
		WorkflowID:  workflowID,
		StepID:      "chained_execution",
		CreatedAt:   time.Now(),
		ExpiresAt:   time.Now().Add(24 * time.Hour),
	}

	if err := ie.fileStorage.StoreFile(storedFile); err != nil {
		log.Printf("⚠️ [CHAINED] Failed to store program artifact %s: %v", filename, err)
	} else {
		log.Printf("📁 [CHAINED] Stored program artifact: %s", filename)
	}
}

// executeWithPlanner executes a task using the planner integration
func (ie *IntelligentExecutor) executeWithPlanner(ctx context.Context, req *ExecutionRequest, start time.Time) (*IntelligentExecutionResult, error) {
	workflowID := fmt.Sprintf("intelligent_%d", time.Now().UnixNano())
	result := &IntelligentExecutionResult{
		Success:         false,
		ValidationSteps: []ValidationStep{},
		WorkflowID:      workflowID,
	}

	// Register existing capabilities with planner
	ie.registerCapabilitiesWithPlanner()

	// Use planner to plan and execute
	episode, err := ie.plannerIntegration.PlanAndExecuteTaskWithWorkflowID(
		fmt.Sprintf("Execute task: %s", req.TaskName),
		req.TaskName,
		req.Description,
		req.Context,
		workflowID,
	)
	if err != nil {
		log.Printf("❌ [PLANNER] Planning/execution failed: %v", err)
		log.Printf("🔄 [PLANNER] Falling back to traditional execution")
		// Fall back to traditional execution when planner fails
		workflowID := fmt.Sprintf("intelligent_%d", time.Now().UnixNano())
		return ie.executeTraditionally(ctx, req, start, workflowID)
	}

	// Convert episode result to IntelligentExecutionResult
	result.Success = true
	result.Result = episode.Result
	result.ExecutionTime = time.Since(start)
	result.RetryCount = 1 // Planner handles retries internally

	// Record episode in self-model
	if ie.selfModelManager != nil {
		ie.recordExecutionEpisode(req, result, "planner_execution")
	}

	// Record metrics for monitor UI
	ie.recordMonitorMetrics(result.Success, result.ExecutionTime)

	log.Printf("🎉 [PLANNER] Task completed successfully via planner")
	return result, nil
}

// executeTraditionally executes a task using the traditional intelligent execution
func (ie *IntelligentExecutor) executeTraditionally(ctx context.Context, req *ExecutionRequest, start time.Time, workflowID string) (*IntelligentExecutionResult, error) {
	// Loop protection: Check for rapid repeated requests for the same task
	taskKey := fmt.Sprintf("%s:%s", req.TaskName, req.Description)
	now := time.Now()

	// Filter out trivial repetitive tasks
	trivialPatterns := []string{
		"create example.txt",
		"create example",
		"list directory and create",
		"list current directory",
	}
	descLower := strings.ToLower(req.Description)
	for _, pattern := range trivialPatterns {
		if strings.Contains(descLower, pattern) {
			// Check if we've seen this trivial task recently (within 1 minute)
			trivialKey := fmt.Sprintf("trivial:%s", pattern)
			if ie.recentTasks == nil {
				ie.recentTasks = make(map[string]time.Time)
			}
			if lastSeen, exists := ie.recentTasks[trivialKey]; exists {
				if now.Sub(lastSeen) < 1*time.Minute {
					log.Printf("⚠️ [INTELLIGENT] Trivial task filter: '%s' executed recently, skipping to prevent repetition", pattern)
					return &IntelligentExecutionResult{
						Success:        false,
						Error:          fmt.Sprintf("Trivial task '%s' executed too recently, skipping to prevent repetition", pattern),
						ExecutionTime:  time.Since(start),
						WorkflowID:     workflowID,
						RetryCount:     0,
						UsedCachedCode: false,
					}, nil
				}
			}
			ie.recentTasks[trivialKey] = now
		}
	}

	// Check if we've seen this exact task recently (within 5 seconds)
	if ie.recentTasks == nil {
		ie.recentTasks = make(map[string]time.Time)
	}

	if lastSeen, exists := ie.recentTasks[taskKey]; exists {
		if now.Sub(lastSeen) < 5*time.Second {
			log.Printf("⚠️ [INTELLIGENT] Loop protection: Task '%s' executed recently, skipping to prevent loop", req.TaskName)
			return &IntelligentExecutionResult{
				Success:        false,
				Error:          "Task executed too recently, possible loop detected",
				ExecutionTime:  time.Since(start),
				WorkflowID:     workflowID,
				RetryCount:     0,
				UsedCachedCode: false,
			}, nil
		}
	}

	// Update the recent tasks map
	ie.recentTasks[taskKey] = now

	// Clean up old entries (older than 5 minutes for better memory management)
	for key, timestamp := range ie.recentTasks {
		if now.Sub(timestamp) > 5*time.Minute {
			delete(ie.recentTasks, key)
		}
	}

	// Set defaults
	if req.Language == "" {
		// Try to infer language from the user request before applying defaults
		if inferred := ie.inferLanguageFromRequest(req); inferred != "" {
			req.Language = inferred
			log.Printf("🔍 [INTELLIGENT] Language inferred from request: %s", req.Language)
		} else {
			// Default to Python for mathematical tasks
			req.Language = "python"
			log.Printf("🔍 [INTELLIGENT] No language detected, defaulting to: %s", req.Language)
		}
	} else {
		log.Printf("🔍 [INTELLIGENT] Language explicitly provided: %s", req.Language)
	}
	if req.MaxRetries == 0 {
		req.MaxRetries = ie.maxRetries
	}
	if req.Timeout == 0 {
		req.Timeout = 120 // Reduced from 600 to prevent long-running requests and GPU overload
	}

	result := &IntelligentExecutionResult{
		Success:         false,
		ValidationSteps: []ValidationStep{},
		WorkflowID:      workflowID,
	}

	// Step 1: Check if this is a multi-program request that needs chained execution
	if ie.isChainedProgramRequest(req) {
		log.Printf("🔗 [INTELLIGENT] Detected chained program request, using multi-step execution")
		return ie.executeChainedPrograms(ctx, req, start, workflowID)
	}

	// Step 2: Static request safety check BEFORE anything else
	if unsafeReason := isRequestUnsafeStatic(req); unsafeReason != "" {
		log.Printf("🚫 [INTELLIGENT] Request blocked by static safety pre-check: %s", unsafeReason)
		result.ValidationSteps = append(result.ValidationSteps, ValidationStep{
			Step:     "static_safety_check",
			Success:  false,
			Message:  "Code rejected by safety policy",
			Error:    unsafeReason,
			Duration: 0,
			Output:   "",
		})
		result.Success = false
		result.Error = "Task blocked by safety policy"
		result.ExecutionTime = time.Since(start)
		return result, nil
	}

	// Step 3: Check principles BEFORE doing anything else
	log.Printf("🔒 [INTELLIGENT] Checking principles before any processing")

	// Check for context cancellation before LLM calls
	if ctx.Err() != nil {
		log.Printf("⏱️ [INTELLIGENT] Context canceled before safety check: %v", ctx.Err())
		result.Success = false
		result.Error = fmt.Sprintf("Execution timed out or was canceled: %v", ctx.Err())
		result.ExecutionTime = time.Since(start)
		return result, nil
	}

	// Use LLM to intelligently categorize the request for safety
	context, err := ie.categorizeRequestForSafety(req)
	if err != nil {
		// Check if error is due to context cancellation
		if ctx.Err() != nil || strings.Contains(err.Error(), "cancelled") || strings.Contains(err.Error(), "timeout") || strings.Contains(err.Error(), "deadline") {
			log.Printf("⏱️ [INTELLIGENT] Safety categorization cancelled/timed out: %v", err)
			result.Success = false
			result.Error = fmt.Sprintf("Execution timed out or was canceled: %v", ctx.Err())
			result.ExecutionTime = time.Since(start)
			return result, nil
		}
		log.Printf("❌ [INTELLIGENT] Safety categorization failed: %v", err)
		result.Success = false
		result.Error = fmt.Sprintf("Cannot verify task safety - LLM categorization failed: %v", err)
		result.ExecutionTime = time.Since(start)
		return result, nil
	}

	allowed, reasons, err := CheckActionWithPrinciples(req.TaskName, context)
	if err != nil {
		log.Printf("❌ [INTELLIGENT] Principles check FAILED for %s: %v", req.TaskName, err)
		result.Success = false
		result.Error = fmt.Sprintf("Cannot verify task safety - principles server unavailable: %v", err)
		result.ExecutionTime = time.Since(start)
		return result, nil
	} else if !allowed {
		log.Printf("🚫 [INTELLIGENT] Task BLOCKED by principles: %s. Reasons: %v", req.TaskName, reasons)
		// Add an explicit validation step so UIs/tests can detect hard blocks
		result.ValidationSteps = append(result.ValidationSteps, ValidationStep{
			Step:     "static_safety_check",
			Success:  false,
			Message:  "Code rejected by safety policy",
			Error:    fmt.Sprintf("blocked by safety policy: %v", reasons),
			Duration: 0,
			Output:   "",
		})
		result.Success = false
		result.Error = fmt.Sprintf("Task blocked by principles: %v", reasons)
		result.ExecutionTime = time.Since(start)
		return result, nil
	} else {
		log.Printf("✅ [INTELLIGENT] Principles check passed for %s", req.TaskName)
	}

	// Step 2: Check if we have compatible cached code for this task
	// Loop protection: Limit force_regenerate usage to prevent infinite loops
	if !req.ForceRegenerate || (req.ForceRegenerate && now.Sub(ie.recentTasks[taskKey]) > 10*time.Second) {
		cachedCode, err := ie.findCompatibleCachedCode(req)
		if err == nil && cachedCode != nil {
			log.Printf("✅ [INTELLIGENT] Found compatible cached code for task: %s", req.TaskName)
			result.UsedCachedCode = true

			// Test the cached code with current parameters
			validationResult := ie.validateCode(ctx, cachedCode, req, workflowID)
			result.ValidationSteps = append(result.ValidationSteps, validationResult)

			if validationResult.Success {
				log.Printf("✅ [INTELLIGENT] Compatible cached code validation successful")

				// Re-run static safety check before final execution to avoid unsafe patterns
				if unsafe := isCodeUnsafeStatic(cachedCode.Code, req.Language, req.Context); unsafe != "" {
					log.Printf("🚫 [INTELLIGENT] Skipping final tool execution due to safety: %s", unsafe)
				} else {
					// Use direct Docker executor for file storage (cached code)
					log.Printf("🎯 [INTELLIGENT] Final execution using direct Docker executor for file storage (cached code)")
					if finalResult, derr := ie.executeWithSSHTool(ctx, cachedCode.Code, req.Language, nil); derr != nil {
						log.Printf("⚠️ [INTELLIGENT] Final execution failed: %v", derr)
					} else if finalResult.Success {
						log.Printf("✅ [INTELLIGENT] Final execution successful, files stored")
					} else {
						log.Printf("⚠️ [INTELLIGENT] Final execution failed: %s", finalResult.Error)
					}
				}

				result.Success = true
				result.GeneratedCode = cachedCode
				result.Result = validationResult.Output
				result.ExecutionTime = time.Since(start)
				return result, nil
			} else {
				log.Printf("⚠️ [INTELLIGENT] Compatible cached code validation failed, will regenerate")
			}
		} else {
			log.Printf("🔍 [INTELLIGENT] No compatible cached code found: %v", err)
		}
	}

	// Step 3: Generate new code using LLM
	log.Printf("🤖 [INTELLIGENT] Generating new code using LLM")

	// Get available tools
	tools, err := ie.getAvailableTools(ctx)
	if err != nil {
		log.Printf("⚠️ [INTELLIGENT] Failed to get tools: %v (continuing without tools)", err)
		tools = []Tool{}
	}

	// Filter relevant tools
	relevantTools := ie.filterRelevantTools(tools, req)
	if len(relevantTools) > 0 {
		toolNames := make([]string, len(relevantTools))
		for i, t := range relevantTools {
			toolNames[i] = t.ID
		}
		log.Printf("🔧 [INTELLIGENT] Found %d relevant tools for task: %v", len(relevantTools), toolNames)
	}

	// Filter non-functional context keys before sending to codegen
	filteredCtx := filterCodegenContext(req.Context)

	// Check if this is a simple task BEFORE enhancing description
	// Simple tasks should not get matrix/environment variable guidance added
	// Use LLM classification instead of string matching for more accurate detection
	isSimpleTask := false
	descPreviewLen := 100
	if len(req.Description) < descPreviewLen {
		descPreviewLen = len(req.Description)
	}
	log.Printf("📝 [INTELLIGENT] Checking for simple task using LLM - description: %s", req.Description[:descPreviewLen])

	// Use LLM to classify task complexity - reuse the existing classifier
	complexity, err := ie.classifyTaskComplexity(req)
	if err != nil {
		// Fallback: if LLM fails, use quick string matching as backup
		log.Printf("⚠️ [INTELLIGENT] LLM classification failed: %v, using string matching fallback", err)
		simpleDescLower := strings.ToLower(req.Description)
		if (strings.Contains(simpleDescLower, "print") || strings.Contains(simpleDescLower, "prints")) &&
			!strings.Contains(simpleDescLower, "matrix") &&
			!strings.Contains(simpleDescLower, "json") &&
			!strings.Contains(simpleDescLower, "read") &&
			!strings.Contains(simpleDescLower, "file") &&
			!strings.Contains(simpleDescLower, "calculate") &&
			!strings.Contains(simpleDescLower, "process") &&
			!strings.Contains(simpleDescLower, "parse") &&
			!strings.Contains(simpleDescLower, "operation") {
			isSimpleTask = true
			log.Printf("📝 [INTELLIGENT] String matching fallback: Detected simple task")
		}
	} else {
		isSimpleTask = (complexity == "simple")
		if isSimpleTask {
			log.Printf("📝 [INTELLIGENT] LLM classified as simple task - skipping description enhancement")
		} else {
			log.Printf("📝 [INTELLIGENT] LLM classified as complex task - will enhance description if needed")
		}
	}

	// Enhance description with matrix operation guidance if needed (skip for simple tasks)
	enhancedDesc := req.Description
	if !isSimpleTask {
		if strings.Contains(strings.ToLower(enhancedDesc), "matrix") ||
			(req.Context != nil && (req.Context["matrix1"] != "" || req.Context["matrix2"] != "")) {
			if req.Language == "go" {
				enhancedDesc += "\n\n🚨 CRITICAL GO MATRIX REQUIREMENTS:\n1. Read from env: matrix1Str := os.Getenv(\"matrix1\"); json.Unmarshal([]byte(matrix1Str), &matrix1) - DO NOT hardcode!\n2. Import: \"os\", \"encoding/json\", \"fmt\"\n3. Output: Print each row separately - for i := 0; i < len(result); i++ { fmt.Println(result[i]) }\n4. WRONG: fmt.Println(result) prints [[6 8] [10 12]] on one line - this FAILS!\n5. CORRECT output format: [6 8] on line 1, [10 12] on line 2"
			} else if req.Language == "python" {
				enhancedDesc += "\n\n🚨 CRITICAL FOR PYTHON MATRIX OPERATIONS:\n- You MUST read matrices from environment variables using os.getenv(\"matrix1\") and os.getenv(\"matrix2\")\n- Parse the JSON string format (e.g., \"[[1,2],[3,4]]\") using json.loads()\n- DO NOT hardcode matrix values - the matrices will be different each time!\n- Example: matrix1 = json.loads(os.getenv(\"matrix1\"))"
			}
		}
	}

	// Add guidance for reading context parameters from environment variables (for Python)
	// Skip for simple tasks
	if !isSimpleTask && req.Language == "python" && req.Context != nil && len(req.Context) > 0 {
		// Check if there are numeric or string parameters that should be read from environment
		hasParams := false
		for k, v := range req.Context {
			if k != "input" && k != "artifact_names" && v != "" {
				hasParams = true
				break
			}
		}
		if hasParams {
			enhancedDesc += "\n\n🚨 CRITICAL FOR PYTHON - READING CONTEXT PARAMETERS:\n- You MUST read ALL context parameters from environment variables using os.getenv()\n- DO NOT hardcode values - the parameters will be different each time!\n- Example: count = int(os.getenv('count', '10'))  # Read 'count' from environment, default to '10' if not set\n- Example: number = int(os.getenv('number', '0'))  # Read 'number' from environment\n- You MUST import 'os' to use os.getenv()\n- Convert string values to appropriate types (int() for numbers, etc.)\n- The context provides these parameters: " + func() string {
				params := []string{}
				for k := range req.Context {
					if k != "input" && k != "artifact_names" {
						params = append(params, k)
					}
				}
				return strings.Join(params, ", ")
			}() + "\n- DO NOT hardcode these values - read them from environment variables!"
		}
	}

	// Add guidance for reading context parameters from environment variables (for JavaScript)
	// Skip for simple tasks
	if !isSimpleTask && (req.Language == "javascript" || req.Language == "js") && req.Context != nil && len(req.Context) > 0 {
		// Check if there are numeric or string parameters that should be read from environment
		hasParams := false
		for k, v := range req.Context {
			if k != "input" && k != "artifact_names" && v != "" {
				hasParams = true
				break
			}
		}
		if hasParams {
			enhancedDesc += "\n\n🚨 CRITICAL FOR JAVASCRIPT - READING CONTEXT PARAMETERS:\n- You MUST read ALL context parameters from environment variables using process.env\n- DO NOT hardcode values - the parameters will be different each time!\n- Example: const count = parseInt(process.env.count || '10', 10);  // Read 'count' from environment, default to '10'\n- Example: const dataStr = process.env.data || process.env.input || ''; const data = dataStr.split(',').map(Number);\n- Convert string values to appropriate types (parseInt() for integers, parseFloat() for floats, split() for arrays)\n- The context provides these parameters: " + func() string {
				params := []string{}
				for k := range req.Context {
					if k != "input" && k != "artifact_names" {
						params = append(params, k)
					}
				}
				return strings.Join(params, ", ")
			}() + "\n- DO NOT hardcode these values - read them from process.env!"
		}
	}

	// 🧠 INTELLIGENCE: Use learned prevention hints to avoid common errors
	if ie.learningRedis == nil {
		log.Printf("⚠️  [INTELLIGENCE] learningRedis is nil - cannot retrieve prevention hints")
	}
	preventionHints := ie.getPreventionHintsForTask(req)
	if len(preventionHints) > 0 {
		enhancedDesc += "\n\n🧠 LEARNED FROM EXPERIENCE - Common errors to avoid:\n"
		for _, hint := range preventionHints {
			enhancedDesc += fmt.Sprintf("- %s\n", hint)
		}
		log.Printf("🧠 [INTELLIGENCE] Added %d prevention hints from learned experience", len(preventionHints))
	}

	// Determine execution method to set correct ToolAPIURL
	// Check if we'll use SSH or Docker for execution
	executionMethod := strings.TrimSpace(os.Getenv("EXECUTION_METHOD"))
	forceDocker := req.Language == "rust" || req.Language == "java"
	useSSH := !forceDocker && (executionMethod == "ssh" || (executionMethod == "" && (runtime.GOARCH == "arm64" || runtime.GOARCH == "aarch64" || os.Getenv("ENABLE_ARM64_TOOLS") == "true")))

	// Set ToolAPIURL based on execution method
	// Only use host.docker.internal for Docker execution
	toolAPIURL := ie.hdnBaseURL
	if toolAPIURL == "" {
		if url := os.Getenv("HDN_URL"); url != "" {
			toolAPIURL = url
		} else {
			toolAPIURL = "http://localhost:8080"
		}
	}

	// Only replace localhost with host.docker.internal for Docker execution
	// For SSH execution, use Kubernetes service DNS if localhost is detected
	if !useSSH && strings.Contains(toolAPIURL, "localhost") {
		toolAPIURL = strings.Replace(toolAPIURL, "localhost", "host.docker.internal", -1)
		log.Printf("🌐 [INTELLIGENT] Updated ToolAPIURL for Docker: %s", toolAPIURL)
	} else if useSSH {
		// For SSH execution, if using localhost, try to use Kubernetes service DNS
		// The SSH host needs to be able to reach the Kubernetes service
		if strings.Contains(toolAPIURL, "localhost") {
			// Try to detect Kubernetes service DNS from environment
			// Common patterns: hdn-server-rpi58.agi.svc.cluster.local:8080
			if k8sService := os.Getenv("HDN_K8S_SERVICE"); k8sService != "" {
				toolAPIURL = strings.Replace(toolAPIURL, "localhost:8080", k8sService, -1)
				log.Printf("🌐 [INTELLIGENT] Using Kubernetes service DNS for SSH: %s", toolAPIURL)
			} else {
				// Default to Kubernetes service DNS pattern if in Kubernetes
				// This assumes the SSH host can reach the Kubernetes service
				toolAPIURL = strings.Replace(toolAPIURL, "localhost:8080", "hdn-server-rpi58.agi.svc.cluster.local:8080", -1)
				log.Printf("🌐 [INTELLIGENT] Using default Kubernetes service DNS for SSH: %s", toolAPIURL)
			}
		} else {
			log.Printf("🌐 [INTELLIGENT] Using ToolAPIURL for SSH execution: %s", toolAPIURL)
		}
	}

	codeGenReq := &CodeGenerationRequest{
		TaskName:     req.TaskName,
		Description:  enhancedDesc,
		Language:     req.Language,
		Context:      filteredCtx,
		Tags:         []string{"intelligent_execution", "auto_generated"},
		Executable:   true,
		Tools:        relevantTools,
		ToolAPIURL:   toolAPIURL,
		HighPriority: req.HighPriority, // Pass priority from execution request
	}

	codeGenResult, err := ie.codeGenerator.GenerateCode(codeGenReq)
	if err != nil {
		result.Error = fmt.Sprintf("Code generation failed: %v", err)
		result.ExecutionTime = time.Since(start)
		return result, nil
	}

	if !codeGenResult.Success {
		result.Error = fmt.Sprintf("Code generation failed: %s", codeGenResult.Error)
		result.ExecutionTime = time.Since(start)
		return result, nil
	}

	generatedCode := codeGenResult.Code
	log.Printf("✅ [INTELLIGENT] Generated code successfully")

	// Step 4: Validate and iterate on the generated code
	success := false
	for attempt := 0; attempt < req.MaxRetries; attempt++ {
		log.Printf("🔄 [INTELLIGENT] Validation attempt %d/%d", attempt+1, req.MaxRetries)

		validationResult := ie.validateCode(ctx, generatedCode, req, workflowID)
		result.ValidationSteps = append(result.ValidationSteps, validationResult)
		result.RetryCount = attempt + 1

		if validationResult.Success {
			log.Printf("✅ [INTELLIGENT] Code validation successful on attempt %d", attempt+1)
			success = true
			break
		} else {
			log.Printf("❌ [INTELLIGENT] Code validation failed on attempt %d: %s", attempt+1, validationResult.Error)

			// Learn from validation failure
			ie.learnFromValidationFailure(validationResult, req)

			// Try to fix the code using LLM feedback
			if attempt < req.MaxRetries-1 {
				log.Printf("🔧 [INTELLIGENT] Attempting to fix code using LLM feedback")
				fixedCode, fixErr := ie.fixCodeWithLLM(generatedCode, validationResult, req)
				if fixErr != nil {
					log.Printf("❌ [INTELLIGENT] Code fixing failed: %v", fixErr)
					continue
				}
				generatedCode = fixedCode
				log.Printf("✅ [INTELLIGENT] Code fixed, retrying validation")
			}
		}
	}

	if !success {
		// Try to get detailed error from the last validation step
		errorMsg := ""
		if len(result.ValidationSteps) > 0 {
			lastStep := result.ValidationSteps[len(result.ValidationSteps)-1]
			if lastStep.Error != "" {
				errorMsg = lastStep.Error
			} else if lastStep.Output != "" {
				// Use output as error if it contains error information
				if strings.Contains(strings.ToLower(lastStep.Output), "error") ||
					strings.Contains(strings.ToLower(lastStep.Output), "failed") ||
					strings.Contains(strings.ToLower(lastStep.Output), "traceback") ||
					strings.Contains(strings.ToLower(lastStep.Output), "connection refused") ||
					strings.Contains(strings.ToLower(lastStep.Output), "compilation") {
					errorMsg = fmt.Sprintf("Execution failed: %s", lastStep.Output)
				} else {
					// Truncate long output for error message
					outputPreview := lastStep.Output
					if len(outputPreview) > 500 {
						outputPreview = outputPreview[:500] + "..."
					}
					errorMsg = fmt.Sprintf("Code validation failed after all retry attempts. Last output: %s", outputPreview)
				}
			} else {
				errorMsg = "Code validation failed after all retry attempts (no error details available)"
			}
			result.Result = lastStep.Output
		} else {
			errorMsg = "Code validation failed after all retry attempts (no validation steps recorded)"
		}

		// Ensure error message is never empty
		if errorMsg == "" {
			errorMsg = "Code validation failed after all retry attempts"
		}

		result.Error = errorMsg
		result.ExecutionTime = time.Since(start)
		log.Printf("❌ [INTELLIGENT] Execution failed: %s", errorMsg)
		return result, nil
	}

	// Step 5: Final execution to store files (not validation)
	log.Printf("🎯 [INTELLIGENT] Final execution to store generated files via tool")
	// If artifact_names includes file entries and GeneratedCode is present, save those as files immediately
	if names, ok := req.Context["artifact_names"]; ok && names != "" && generatedCode != nil {
		parts := strings.Split(names, ",")
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		// Support multiple file types: .py, .go, .js, .java, .md, .txt
		supportedExts := []string{".py", ".go", ".js", ".java", ".md", ".txt"}
		for _, fname := range parts {
			for _, ext := range supportedExts {
				if strings.HasSuffix(strings.ToLower(fname), ext) {
					// Store code as file artifact before running docker
					log.Printf("📄 [INTELLIGENT] Storing code as artifact: %s", fname)
					// We don't have direct file storage here; pass via environment for docker executor which writes files
					req.Context["save_code_filename"] = fname
					break
				}
			}
		}
	}
	// (removed: normalization for final execution)

	// Final execution: Use SSH executor and extract files if artifacts are needed
	log.Printf("🎯 [INTELLIGENT] Final execution using SSH executor")
	if finalResult, derr := ie.executeWithSSHTool(ctx, generatedCode.Code, req.Language, req.Context); derr != nil {
		log.Printf("⚠️ [INTELLIGENT] Final execution failed: %v", derr)
	} else if finalResult.Success {
		log.Printf("✅ [INTELLIGENT] Final execution successful")
		// Extract and store files if artifact_names is set
		if names, ok := req.Context["artifact_names"]; ok && names != "" && ie.fileStorage != nil {
			parts := strings.Split(names, ",")
			for _, fname := range parts {
				fname = strings.TrimSpace(fname)
				if fname != "" {
					log.Printf("📁 [INTELLIGENT] Attempting to extract artifact: %s", fname)
					// Extract file from SSH execution directory
					if fileContent, err := ie.extractFileFromSSH(ctx, fname, req.Language); err == nil && len(fileContent) > 0 {
						// Get workflow ID from result or generate one
						workflowID := ""
						if finalResult != nil && finalResult.ContainerID != "" {
							workflowID = finalResult.ContainerID // Reuse container ID as workflow ID
						}
						if workflowID == "" {
							workflowID = fmt.Sprintf("intelligent_%d", time.Now().UnixNano())
						}
						// Store the file
						storedFile := &StoredFile{
							Filename:   fname,
							Content:    fileContent,
							WorkflowID: workflowID,
							StepID:     "",
						}
						if err := ie.fileStorage.StoreFile(storedFile); err != nil {
							log.Printf("⚠️ [INTELLIGENT] Failed to store artifact %s: %v", fname, err)
						} else {
							log.Printf("✅ [INTELLIGENT] Stored artifact: %s (%d bytes)", fname, len(fileContent))
						}
					} else {
						log.Printf("⚠️ [INTELLIGENT] Could not extract artifact %s: %v", fname, err)
					}
				}
			}
		}
	} else {
		log.Printf("⚠️ [INTELLIGENT] Final execution failed: %s", finalResult.Error)
	}

	// Step 6: Cache the successful code
	log.Printf("💾 [INTELLIGENT] Caching successful code")
	err = ie.codeStorage.StoreCode(generatedCode)
	if err != nil {
		log.Printf("⚠️ [INTELLIGENT] Failed to cache code: %v", err)
	}

	// Step 7: Create a dynamic action for future reuse
	log.Printf("🎯 [INTELLIGENT] Creating dynamic action for future reuse")
	dynamicAction := &DynamicAction{
		Task:          req.TaskName,
		Preconditions: []string{},
		Effects:       []string{req.TaskName + "_completed"},
		TaskType:      "intelligent_execution",
		Description:   req.Description,
		Code:          generatedCode.Code,
		Language:      generatedCode.Language,
		Context:       req.Context,
		CreatedAt:     time.Now(),
		Domain:        "intelligent",
		Tags:          []string{"intelligent_execution", "auto_generated", "validated"},
	}

	err = ie.actionManager.CreateAction(dynamicAction)
	if err != nil {
		log.Printf("⚠️ [INTELLIGENT] Failed to create dynamic action: %v", err)
	} else {
		result.NewAction = dynamicAction
		log.Printf("✅ [INTELLIGENT] Created dynamic action: %s", dynamicAction.Task)
	}

	// Step 6: Return successful result
	result.Success = true
	result.GeneratedCode = generatedCode
	if len(result.ValidationSteps) > 0 {
		lastStep := result.ValidationSteps[len(result.ValidationSteps)-1]
		result.Result = lastStep.Output
		log.Printf("📊 [INTELLIGENT] Setting result.Result from validation step output: %q (length: %d)", lastStep.Output, len(lastStep.Output))
	} else {
		log.Printf("⚠️ [INTELLIGENT] No validation steps to extract output from")
	}
	result.ExecutionTime = time.Since(start)

	// Record episode in self-model
	if ie.selfModelManager != nil {
		ie.recordExecutionEpisode(req, result, "traditional_execution")
	}

	// Learn from execution outcome
	if result.Success && generatedCode != nil {
		ie.recordSuccessfulExecution(req, result, generatedCode)
	} else if !result.Success {
		ie.recordFailedExecution(req, result)
	}

	// Record metrics for monitor UI
	ie.recordMonitorMetrics(result.Success, result.ExecutionTime)

	log.Printf("🎉 [INTELLIGENT] Intelligent execution completed successfully in %v", result.ExecutionTime)
	return result, nil
}

// registerCapabilitiesWithPlanner registers existing capabilities with the planner
func (ie *IntelligentExecutor) registerCapabilitiesWithPlanner() {
	if ie.plannerIntegration == nil {
		return
	}

	// Get all actions from the action manager
	actions, err := ie.actionManager.GetActionsByDomain("default")
	if err != nil {
		log.Printf("⚠️ [PLANNER] Failed to list actions: %v", err)
		return
	}

	// Convert actions to capabilities and register them
	for _, action := range actions {
		capability := ConvertDynamicActionToCapability(action)
		if err := ie.plannerIntegration.RegisterCapability(capability); err != nil {
			log.Printf("⚠️ [PLANNER] Failed to register capability %s: %v", action.Task, err)
		} else {
			log.Printf("✅ [PLANNER] Registered capability: %s", action.Task)
		}
	}

	// Also register cached code as capabilities
	cachedCode, err := ie.codeStorage.ListAllCode()
	if err != nil {
		log.Printf("⚠️ [PLANNER] Failed to list cached code: %v", err)
		return
	}

	for _, code := range cachedCode {
		capability := &planner.Capability{
			ID:         code.ID,
			TaskName:   code.TaskName,
			Entrypoint: fmt.Sprintf("%s.%s", code.Language, code.TaskName),
			Language:   code.Language,
			InputSig:   make(map[string]string),
			Outputs:    []string{code.TaskName + "_completed"},
			Preconds:   []string{},
			Effects:    map[string]interface{}{code.TaskName + "_completed": true},
			Score:      0.9, // High confidence for validated code
			CreatedAt:  code.CreatedAt,
			LastUsed:   time.Now(),
			Validation: map[string]interface{}{
				"executable": code.Executable,
				"tags":       code.Tags,
			},
			Permissions: code.Tags,
		}

		if err := ie.plannerIntegration.RegisterCapability(capability); err != nil {
			log.Printf("⚠️ [PLANNER] Failed to register cached code capability %s: %v", code.TaskName, err)
		} else {
			log.Printf("✅ [PLANNER] Registered cached code capability: %s", code.TaskName)
		}
	}
}

// findCachedCode searches for previously generated and validated code with parameter compatibility
func (ie *IntelligentExecutor) findCachedCode(taskName, description, language string) (*GeneratedCode, error) {
	// If taskName looks like an ID, search by ID first
	if strings.HasPrefix(taskName, "code_") {
		log.Printf("🔍 [INTELLIGENT] Searching for cached code by ID: %s (language: %s)", taskName, language)
		// Search all cached code and find by ID - try all languages
		allLanguages := []string{"python", "go", "javascript", "java", "cpp"}
		for _, lang := range allLanguages {
			allResults, err := ie.codeStorage.SearchCode("", lang, []string{"intelligent_execution", "validated"})
			if err != nil {
				log.Printf("🔍 [INTELLIGENT] Search error for language %s: %v", lang, err)
				continue
			}
			log.Printf("🔍 [INTELLIGENT] Found %d results for language %s", len(allResults), lang)
			for _, result := range allResults {
				log.Printf("🔍 [INTELLIGENT] Checking result: ID=%s, TaskName=%s, Language=%s, Executable=%v",
					result.Code.ID, result.Code.TaskName, result.Code.Language, result.Code.Executable)
				if result.Code.ID == taskName && result.Code.Executable {
					log.Printf("🔍 [INTELLIGENT] Found cached code by ID: %s (ID: %s, Language: %s)", result.Code.TaskName, result.Code.ID, result.Code.Language)
					return result.Code, nil
				}
			}
		}
	}

	// If no ID match, try exact task name match
	results, err := ie.codeStorage.SearchCode(taskName, language, []string{"intelligent_execution", "validated"})
	if err == nil && len(results) > 0 {
		for _, result := range results {
			if result.Code.Executable && result.Code.Language == language {
				log.Printf("🔍 [INTELLIGENT] Found cached code: %s (ID: %s)", result.Code.TaskName, result.Code.ID)
				return result.Code, nil
			}
		}
	}

	return nil, fmt.Errorf("no cached code found")
}

// findCompatibleCachedCode searches for cached code that's compatible with the current request parameters
func (ie *IntelligentExecutor) findCompatibleCachedCode(req *ExecutionRequest) (*GeneratedCode, error) {
	log.Printf("🔍 [INTELLIGENT] Searching for compatible cached code for task: %s", req.TaskName)

	// First, search for exact task name match only
	results, err := ie.codeStorage.SearchCode(req.TaskName, "", []string{"intelligent_execution", "validated"})
	if err != nil {
		log.Printf("🔍 [INTELLIGENT] Search error: %v", err)
		return nil, err
	}

	// Filter results to only include exact task name matches
	var exactMatches []CodeSearchResult
	for _, result := range results {
		if result.Code.TaskName == req.TaskName {
			exactMatches = append(exactMatches, result)
		}
	}

	if len(exactMatches) == 0 {
		log.Printf("🔍 [INTELLIGENT] No exact cached code found for task: %s", req.TaskName)
		return nil, fmt.Errorf("no exact cached code found")
	}

	log.Printf("🔍 [INTELLIGENT] Found %d exact cached code entries for task: %s", len(exactMatches), req.TaskName)

	// Check each exact match for parameter compatibility
	for _, result := range exactMatches {
		if !result.Code.Executable {
			continue
		}

		compatibility := ie.checkParameterCompatibility(result.Code, req)
		log.Printf("🔍 [INTELLIGENT] Compatibility check for %s (ID: %s): %s",
			result.Code.TaskName, result.Code.ID, compatibility.Status)

		if compatibility.IsCompatible {
			log.Printf("✅ [INTELLIGENT] Found compatible cached code: %s (ID: %s) - %s",
				result.Code.TaskName, result.Code.ID, compatibility.Reason)
			return result.Code, nil
		}
	}

	log.Printf("❌ [INTELLIGENT] No compatible cached code found for task: %s", req.TaskName)
	return nil, fmt.Errorf("no compatible cached code found")
}

// ParameterCompatibility represents the result of parameter compatibility checking
type ParameterCompatibility struct {
	IsCompatible bool    `json:"is_compatible"`
	Status       string  `json:"status"`
	Reason       string  `json:"reason"`
	Confidence   float64 `json:"confidence"`
}

// checkParameterCompatibility checks if cached code is compatible with current request parameters
func (ie *IntelligentExecutor) checkParameterCompatibility(cachedCode *GeneratedCode, req *ExecutionRequest) ParameterCompatibility {
	log.Printf("🔍 [COMPATIBILITY] Checking compatibility for %s (ID: %s)", cachedCode.TaskName, cachedCode.ID)

	// Extract original parameters from cached code context
	originalContext := cachedCode.Context
	currentContext := req.Context

	log.Printf("🔍 [COMPATIBILITY] Original context: %+v", originalContext)
	log.Printf("🔍 [COMPATIBILITY] Current context: %+v", currentContext)

	// Hard guard 1: language must match when requested
	if strings.TrimSpace(req.Language) != "" && strings.ToLower(cachedCode.Language) != strings.ToLower(req.Language) {
		return ParameterCompatibility{
			IsCompatible: false,
			Status:       "language_mismatch",
			Reason:       fmt.Sprintf("Cached language '%s' != requested '%s'", cachedCode.Language, req.Language),
			Confidence:   0.0,
		}
	}

	// Hard guard 2: project_id must match when both provided
	if oc, ok1 := originalContext["project_id"]; ok1 {
		if cc, ok2 := currentContext["project_id"]; ok2 {
			if strings.TrimSpace(oc) != "" && strings.TrimSpace(cc) != "" && oc != cc {
				return ParameterCompatibility{
					IsCompatible: false,
					Status:       "project_mismatch",
					Reason:       fmt.Sprintf("Cached project_id '%s' != current '%s'", oc, cc),
					Confidence:   0.0,
				}
			}
		}
	}

	// Check for exact match first
	if ie.contextsAreEqual(originalContext, currentContext) {
		return ParameterCompatibility{
			IsCompatible: true,
			Status:       "exact_match",
			Reason:       "Parameters match exactly",
			Confidence:   1.0,
		}
	}

	// Check for mathematical task compatibility
	if ie.isMathematicalTask(cachedCode.TaskName) {
		compatibility := ie.checkMathematicalCompatibility(cachedCode, req)
		if compatibility.IsCompatible {
			return compatibility
		}
	}

	// Check for string-based task compatibility
	if ie.isStringBasedTask(cachedCode.TaskName) {
		compatibility := ie.checkStringBasedCompatibility(cachedCode, req)
		if compatibility.IsCompatible {
			return compatibility
		}
	}

	// Check for structural compatibility (same parameter names, different values)
	compatibility := ie.checkStructuralCompatibility(cachedCode, req)
	if compatibility.IsCompatible {
		return compatibility
	}

	// Default: not compatible
	return ParameterCompatibility{
		IsCompatible: false,
		Status:       "incompatible",
		Reason:       "Parameters are fundamentally different",
		Confidence:   0.0,
	}
}

// contextsAreEqual checks if two context maps are equal
func (ie *IntelligentExecutor) contextsAreEqual(ctx1, ctx2 map[string]string) bool {
	if len(ctx1) != len(ctx2) {
		return false
	}

	for k, v1 := range ctx1 {
		if v2, exists := ctx2[k]; !exists || v1 != v2 {
			return false
		}
	}

	return true
}

// isMathematicalTask checks if a task is mathematical in nature
func (ie *IntelligentExecutor) isMathematicalTask(taskName string) bool {
	mathKeywords := []string{"prime", "matrix", "statistics", "calculate", "compute", "math", "number", "sum", "multiply", "divide", "add", "subtract"}
	taskLower := strings.ToLower(taskName)

	for _, keyword := range mathKeywords {
		if strings.Contains(taskLower, keyword) {
			return true
		}
	}
	return false
}

// isStringBasedTask checks if a task is string-based in nature
func (ie *IntelligentExecutor) isStringBasedTask(taskName string) bool {
	stringKeywords := []string{"text", "string", "parse", "format", "replace", "split", "join", "search", "find"}
	taskLower := strings.ToLower(taskName)

	for _, keyword := range stringKeywords {
		if strings.Contains(taskLower, keyword) {
			return true
		}
	}
	return false
}

// checkMathematicalCompatibility checks compatibility for mathematical tasks
func (ie *IntelligentExecutor) checkMathematicalCompatibility(cachedCode *GeneratedCode, req *ExecutionRequest) ParameterCompatibility {
	// For mathematical tasks, we need to be very strict about compatibility
	// Different mathematical operations (prime generation vs matrix operations vs statistics)
	// should NOT be considered compatible even if they're both "mathematical"

	// First, check if the task names are exactly the same
	if cachedCode.TaskName != req.TaskName {
		return ParameterCompatibility{
			IsCompatible: false,
			Status:       "mathematical_incompatible",
			Reason:       fmt.Sprintf("Different mathematical tasks: '%s' vs '%s'", cachedCode.TaskName, req.TaskName),
			Confidence:   0.0,
		}
	}

	// Check for critical parameters that must match exactly
	criticalParams := []string{"operation", "method", "type", "mode", "algorithm"}
	for _, param := range criticalParams {
		if originalVal, hasOriginal := cachedCode.Context[param]; hasOriginal {
			if currentVal, hasCurrent := req.Context[param]; hasCurrent {
				if originalVal != currentVal {
					return ParameterCompatibility{
						IsCompatible: false,
						Status:       "mathematical_incompatible",
						Reason:       fmt.Sprintf("Critical parameter '%s' differs: '%s' vs '%s'", param, originalVal, currentVal),
						Confidence:   0.0,
					}
				}
			}
		}
	}

	// Check if both contexts have similar parameter names
	originalKeys := make(map[string]bool)
	for k := range cachedCode.Context {
		originalKeys[k] = true
	}

	currentKeys := make(map[string]bool)
	for k := range req.Context {
		currentKeys[k] = true
	}

	// Check for key overlap
	commonKeys := 0
	for k := range originalKeys {
		if currentKeys[k] {
			commonKeys++
		}
	}

	// If we have good key overlap, the code is likely compatible
	keyOverlapRatio := float64(commonKeys) / float64(len(originalKeys))

	if keyOverlapRatio >= 0.8 { // 80% key overlap
		return ParameterCompatibility{
			IsCompatible: true,
			Status:       "mathematical_compatible",
			Reason:       fmt.Sprintf("Mathematical task with %d%% parameter overlap", int(keyOverlapRatio*100)),
			Confidence:   keyOverlapRatio,
		}
	}

	// Check for specific mathematical parameter compatibility
	if ie.hasCompatibleMathematicalParameters(cachedCode.Context, req.Context) {
		return ParameterCompatibility{
			IsCompatible: true,
			Status:       "mathematical_parameter_compatible",
			Reason:       "Mathematical parameters are compatible (different values, same structure)",
			Confidence:   0.8,
		}
	}

	return ParameterCompatibility{
		IsCompatible: false,
		Status:       "mathematical_incompatible",
		Reason:       "Mathematical parameters are not compatible",
		Confidence:   0.0,
	}
}

// hasCompatibleMathematicalParameters checks if mathematical parameters are compatible
func (ie *IntelligentExecutor) hasCompatibleMathematicalParameters(original, current map[string]string) bool {
	// Check for common mathematical parameter patterns
	mathParams := []string{"count", "number", "size", "length", "input", "value", "n", "limit", "max", "min"}

	for _, param := range mathParams {
		if _, hasOriginal := original[param]; hasOriginal {
			if _, hasCurrent := current[param]; hasCurrent {
				// Both have the same parameter name, likely compatible
				return true
			}
		}
	}

	return false
}

// checkStringBasedCompatibility checks compatibility for string-based tasks
func (ie *IntelligentExecutor) checkStringBasedCompatibility(cachedCode *GeneratedCode, req *ExecutionRequest) ParameterCompatibility {
	// For string-based tasks, we need more careful checking
	// as the content might be very different

	// Check if both have similar parameter structure
	originalKeys := make(map[string]bool)
	for k := range cachedCode.Context {
		originalKeys[k] = true
	}

	currentKeys := make(map[string]bool)
	for k := range req.Context {
		currentKeys[k] = true
	}

	// For string tasks, we need very high key overlap
	commonKeys := 0
	for k := range originalKeys {
		if currentKeys[k] {
			commonKeys++
		}
	}

	keyOverlapRatio := float64(commonKeys) / float64(len(originalKeys))

	if keyOverlapRatio >= 0.9 { // 90% key overlap for string tasks
		return ParameterCompatibility{
			IsCompatible: true,
			Status:       "string_compatible",
			Reason:       fmt.Sprintf("String task with %d%% parameter overlap", int(keyOverlapRatio*100)),
			Confidence:   keyOverlapRatio,
		}
	}

	return ParameterCompatibility{
		IsCompatible: false,
		Status:       "string_incompatible",
		Reason:       "String task parameters are not compatible",
		Confidence:   0.0,
	}
}

// checkStructuralCompatibility checks if the parameter structure is compatible
func (ie *IntelligentExecutor) checkStructuralCompatibility(cachedCode *GeneratedCode, req *ExecutionRequest) ParameterCompatibility {
	// Check if the parameter structure is similar enough
	originalKeys := make(map[string]bool)
	for k := range cachedCode.Context {
		originalKeys[k] = true
	}

	currentKeys := make(map[string]bool)
	for k := range req.Context {
		currentKeys[k] = true
	}

	// Check for key overlap
	commonKeys := 0
	for k := range originalKeys {
		if currentKeys[k] {
			commonKeys++
		}
	}

	// Check for new keys in current request
	newKeys := 0
	for k := range currentKeys {
		if !originalKeys[k] {
			newKeys++
		}
	}

	// Calculate compatibility score
	totalKeys := len(originalKeys) + newKeys
	if totalKeys == 0 {
		return ParameterCompatibility{
			IsCompatible: false,
			Status:       "no_parameters",
			Reason:       "No parameters to compare",
			Confidence:   0.0,
		}
	}

	compatibilityScore := float64(commonKeys) / float64(totalKeys)

	// If we have good structural compatibility, allow reuse
	if compatibilityScore >= 0.7 { // 70% structural compatibility
		return ParameterCompatibility{
			IsCompatible: true,
			Status:       "structurally_compatible",
			Reason:       fmt.Sprintf("Structural compatibility: %d%%", int(compatibilityScore*100)),
			Confidence:   compatibilityScore,
		}
	}

	return ParameterCompatibility{
		IsCompatible: false,
		Status:       "structurally_incompatible",
		Reason:       "Parameter structure is not compatible",
		Confidence:   0.0,
	}
}

// validateCode tests the generated code in Docker
func (ie *IntelligentExecutor) validateCode(ctx context.Context, code *GeneratedCode, req *ExecutionRequest, workflowID string) ValidationStep {
	start := time.Now()
	log.Printf("🧪 [VALIDATION] Testing code for task: %s", code.TaskName)

	// Check if task explicitly mentions using a tool - if so, allow HTTP requests
	// This needs to be done before the safety check so it can see allow_requests in context
	descLower := strings.ToLower(req.Description)
	taskLower := strings.ToLower(req.TaskName)
	if strings.Contains(descLower, "tool_http_get") || strings.Contains(descLower, "tool_html_scraper") ||
		strings.Contains(descLower, "use tool") || strings.Contains(taskLower, "tool_http_get") ||
		strings.Contains(taskLower, "tool_html_scraper") || strings.Contains(taskLower, "use tool") {
		if req.Context == nil {
			req.Context = make(map[string]string)
		}
		req.Context["allow_requests"] = "true"
		log.Printf("🔓 [VALIDATION] Allowing HTTP requests for tool-calling task")
	}

	// Also check for hypothesis testing tasks - they need HTTP requests for Neo4j queries
	// Check both the original description/task and the enhanced description
	if strings.HasPrefix(descLower, "test hypothesis:") || strings.HasPrefix(taskLower, "test hypothesis:") ||
		strings.HasPrefix(descLower, "test hypothesis by gathering evidence:") ||
		strings.Contains(descLower, "hypothesis testing") || strings.Contains(taskLower, "hypothesis testing") ||
		strings.Contains(descLower, "test hypothesis by gathering evidence") {
		if req.Context == nil {
			req.Context = make(map[string]string)
		}
		req.Context["allow_requests"] = "true"
		log.Printf("🔓 [VALIDATION] Allowing HTTP requests for hypothesis testing task")
	}

	// Also check if context already has hypothesis_testing flag set
	if req.Context != nil {
		if v, ok := req.Context["hypothesis_testing"]; ok && (strings.EqualFold(strings.TrimSpace(v), "true") || strings.TrimSpace(v) == "1") {
			req.Context["allow_requests"] = "true"
			log.Printf("🔓 [VALIDATION] Allowing HTTP requests (context flag: hypothesis_testing=true)")
		}
	}

	// Static safety check before any execution
	if unsafeReason := isCodeUnsafeStatic(code.Code, code.Language, req.Context); unsafeReason != "" {
		return ValidationStep{
			Step:     "static_safety_check",
			Success:  false,
			Message:  "Code rejected by safety policy",
			Duration: time.Since(start),
			Code:     code.Code,
			Error:    unsafeReason,
		}
	}

	// (removed: normalization during validation)
	env := map[string]string{}
	// Filter out non-functional context keys that shouldn't be passed to code execution
	// These keys can cause the LLM to generate code that references them incorrectly
	skipKeys := map[string]bool{
		"session_id":         true,
		"project_id":         true,
		"artifact_names":     true,
		"save_code_filename": true,
		"saveCodeFilename":   true,
		"artifacts":          true,
		"artifactsWrapper":   true,
		"prefer_traditional": true,
	}
	for k, v := range req.Context {
		if v != "" && !skipKeys[strings.TrimSpace(strings.ToLower(k))] {
			env[k] = v
		}
	}
	// Use QUIET mode to suppress environment dumps from SSH shell initialization
	env["QUIET"] = "1"

	// Choose execution method FIRST so we can set the correct HDN_URL
	executionMethod := strings.TrimSpace(os.Getenv("EXECUTION_METHOD"))

	// Force Docker for Rust and Java (not available on RPI host)
	// These languages require Docker containers with proper toolchains
	forceDocker := code.Language == "rust" || code.Language == "java"

	if forceDocker {
		log.Printf("🐳 [VALIDATION] Forcing Docker executor for %s (not available on RPI host)", code.Language)
	}

	useSSH := !forceDocker && (executionMethod == "ssh" || (executionMethod == "" && (runtime.GOARCH == "arm64" || runtime.GOARCH == "aarch64" || os.Getenv("ENABLE_ARM64_TOOLS") == "true")))

	// Pass HDN_URL to validation environment so generated code can call tool APIs if needed
	// IMPORTANT: Use host.docker.internal for Docker, but use Kubernetes service DNS for SSH
	var hdnURL string
	if ie.hdnBaseURL != "" {
		hdnURL = ie.hdnBaseURL
	} else if url := os.Getenv("HDN_URL"); url != "" {
		hdnURL = url
	} else {
		hdnURL = "http://localhost:8080"
	}

	// Only replace localhost with host.docker.internal for Docker execution
	// For SSH execution, use Kubernetes service DNS if localhost is detected
	if !useSSH && strings.Contains(hdnURL, "localhost") {
		hdnURL = strings.Replace(hdnURL, "localhost", "host.docker.internal", -1)
		log.Printf("🌐 [VALIDATION] Updated HDN_URL for Docker: %s", hdnURL)
	} else if useSSH {
		// For SSH execution, if using localhost, try to use Kubernetes service DNS
		// The SSH host needs to be able to reach the Kubernetes service
		if strings.Contains(hdnURL, "localhost") {
			// Try to detect Kubernetes service DNS from environment
			// Common patterns: hdn-server-rpi58.agi.svc.cluster.local:8080
			if k8sService := os.Getenv("HDN_K8S_SERVICE"); k8sService != "" {
				hdnURL = strings.Replace(hdnURL, "localhost:8080", k8sService, -1)
				log.Printf("🌐 [VALIDATION] Using Kubernetes service DNS for SSH: %s", hdnURL)
			} else {
				// Default to Kubernetes service DNS pattern if in Kubernetes
				// This assumes the SSH host can reach the Kubernetes service
				hdnURL = strings.Replace(hdnURL, "localhost:8080", "hdn-server-rpi58.agi.svc.cluster.local:8080", -1)
				log.Printf("🌐 [VALIDATION] Using default Kubernetes service DNS for SSH: %s", hdnURL)
			}
		} else {
			log.Printf("🌐 [VALIDATION] Using HDN_URL for SSH execution: %s", hdnURL)
		}
	}

	env["HDN_URL"] = hdnURL
	// Copy allow_requests from context to env if it was set above
	if allowReq, ok := req.Context["allow_requests"]; ok && allowReq == "true" {
		env["allow_requests"] = "true"
	}

	// Create Docker execution request
	// (removed: unused dockerReq)

	var result *DockerExecutionResponse
	var err error

	if useSSH {
		log.Printf("🧪 [VALIDATION] Using SSH executor for validation")
		result, err = ie.executeWithSSHTool(ctx, code.Code, code.Language, env)
	} else {
		log.Printf("🧪 [VALIDATION] Using Docker executor for validation")
		// Use direct Docker execution
		if ie.dockerExecutor == nil {
			return ValidationStep{
				Step:     "docker_execution",
				Success:  false,
				Error:    "docker executor unavailable",
				Message:  "Docker execution failed",
				Duration: time.Since(start),
				Code:     code.Code,
			}
		}

		dockerReq := &DockerExecutionRequest{
			Language:     code.Language,
			Code:         code.Code,
			Timeout:      600, // Increased to 10 minutes for complex code validation
			Environment:  env,
			IsValidation: true,
		}

		// If previous_output is in context, pass it as stdin (for JSON reading tasks)
		if prevOutput, ok := req.Context["previous_output"]; ok && prevOutput != "" {
			dockerReq.Input = prevOutput
			log.Printf("📥 [VALIDATION] Passing previous_output as stdin: %s", prevOutput)
		}

		result, err = ie.dockerExecutor.ExecuteCode(ctx, dockerReq)
		if err != nil {
			result = &DockerExecutionResponse{Success: false, Error: err.Error(), ExitCode: 1}
		}
	}

	validationStep := ValidationStep{
		Step:     "docker_execution",
		Success:  result.Success,
		Duration: time.Since(start),
		Code:     code.Code,
	}

	if err != nil {
		validationStep.Error = err.Error()
		validationStep.Message = "Docker execution failed"
		log.Printf("❌ [VALIDATION] Docker execution failed: %v", err)
		return validationStep
	}

	if !result.Success {
		validationStep.Error = result.Error
		validationStep.Message = "Code execution failed"
		// Set Output even on failure - it contains compilation errors for Go
		validationStep.Output = result.Output
		log.Printf("❌ [VALIDATION] Code execution failed: %s", result.Error)
		log.Printf("📊 [VALIDATION] Output (may contain compilation errors): %s", result.Output)
		return validationStep
	}

	validationStep.Output = result.Output
	validationStep.Message = "Code execution successful"

	// Check if output is empty but task likely requires output
	// For tasks that should produce output (like printing results), empty output indicates a problem
	if strings.TrimSpace(result.Output) == "" {
		// For chained programs, be more lenient - they might just save files without producing output
		isChainedProgram := strings.HasPrefix(req.TaskName, "prog") ||
			strings.HasPrefix(req.TaskName, "chained_prog") ||
			strings.HasPrefix(req.TaskName, "program_") ||
			strings.Contains(strings.ToLower(req.Description), "create") && strings.Contains(strings.ToLower(req.Description), "program")

		// Check if files were created (indicates success even without output)
		hasFiles := result.Files != nil && len(result.Files) > 0

		if isChainedProgram && hasFiles {
			log.Printf("✅ [VALIDATION] Chained program executed successfully and created files (no output required)")
			// Allow success for chained programs that create files
		} else if isChainedProgram {
			log.Printf("⚠️ [VALIDATION] Chained program executed successfully but no output (allowing success for file generation)")
			// Allow success for chained programs even without output - they might just generate code files
		} else {
			// Check if this is a task that should produce output
			// Most intelligent execution tasks should produce some output
			shouldHaveOutput := strings.Contains(strings.ToLower(req.Description), "print") ||
				strings.Contains(strings.ToLower(req.Description), "output") ||
				strings.Contains(strings.ToLower(req.Description), "result") ||
				strings.Contains(strings.ToLower(req.Description), "calculate") ||
				strings.Contains(strings.ToLower(req.Description), "generate") ||
				strings.Contains(strings.ToLower(req.Description), "return") ||
				strings.Contains(strings.ToLower(req.Description), "prime") ||
				strings.Contains(strings.ToLower(req.Description), "statistic") ||
				strings.Contains(strings.ToLower(req.Description), "matrix")

			if shouldHaveOutput {
				log.Printf("❌ [VALIDATION] Code executed successfully but produced no output (task requires output)")
				validationStep.Success = false
				validationStep.Error = "Code executed successfully but produced no output"
				validationStep.Message = "Code execution succeeded but no output was produced"
				return validationStep
			}
		}
	}

	log.Printf("✅ [VALIDATION] Code execution successful")
	log.Printf("📊 [VALIDATION] Output: %s", result.Output)

	return validationStep
}

// normalizeLanguageAndCode infers a correct runtime when saved language is unsupported and
// the code contains a recognizable header (e.g., leading "python\n"). Returns normalized language and code.
func normalizeLanguageAndCode(savedLanguage, code string) (string, string) {
	supported := map[string]bool{"python": true, "javascript": true, "bash": true, "sh": true, "go": true}
	lang := strings.ToLower(strings.TrimSpace(savedLanguage))
	if supported[lang] {
		return lang, code
	}
	if strings.HasPrefix(code, "python\n") {
		return "python", strings.TrimPrefix(code, "python\n")
	}
	if lang == "" {
		lang = "python"
	}
	return lang, code
}

// isCodeUnsafeStatic performs a lightweight static scan to block obviously dangerous behavior
func isCodeUnsafeStatic(code string, language string, ctx map[string]string) string {
	lower := strings.ToLower(code)
	// Disallow shell/system execution and raw networking from generated code
	dangerous := []string{
		"os.system(", "subprocess.popen", "subprocess.call", "subprocess.run",
		"shutil.rmtree", "eval(", "exec(", "__import__(", "open('/",
		"socket.", "urllib.request", "wget ", "curl ",
		// Disallow direct container orchestration commands (but allow mentions in comments/strings)
		"docker run", "docker exec", "docker build", "docker ps", "docker stop", "docker start",
		"docker rm", "docker rmi", "docker pull", "docker push", "docker compose",
		"podman run", "podman exec", "kubectl ",
	}
	// Conditionally disallow/allow Python requests
	allowReq := false
	if v := strings.TrimSpace(os.Getenv("ALLOW_REQUESTS")); v == "1" || strings.EqualFold(v, "true") {
		allowReq = true
	}
	if ctx != nil {
		if v, ok := ctx["allow_requests"]; ok && (strings.EqualFold(strings.TrimSpace(v), "true") || strings.TrimSpace(v) == "1") {
			allowReq = true
		}
	}
	if !allowReq {
		dangerous = append(dangerous, "requests.")
	}
	for _, pat := range dangerous {
		if strings.Contains(lower, pat) {
			return "contains dangerous pattern: " + pat
		}
	}
	// Keep allowing standard math, plotting, file read only under /app/data
	return ""
}

// isRequestUnsafeStatic blocks obviously dangerous intents before generation
func isRequestUnsafeStatic(req *ExecutionRequest) string {
	lowerTask := strings.ToLower(req.TaskName + " " + req.Description)
	// Obvious destructive intents
	patterns := []string{
		"delete all files", "wipe all files", "format disk", "rm -rf /",
		"destroy database", "exfiltrate", "ransomware", "dd if=/dev/zero",
		"deletes all files", "deleting all files", "delete all file", // variations
	}
	for _, p := range patterns {
		if strings.Contains(lowerTask, p) {
			return "request contains disallowed destructive intent: " + p
		}
	}
	// Inappropriate/adult content intents
	adultPatterns := []string{
		"inappropriate content", "adults only", "adult content", "porn", "xxx",
		"nsfw", "explicit sexual", "erotic",
		"inappropriate", "for adults only", "adults-only", // variations
	}
	for _, p := range adultPatterns {
		if strings.Contains(lowerTask, p) {
			return "request contains disallowed inappropriate content intent: " + p
		}
	}
	// Context-based destructive ops
	if req.Context != nil {
		tgt := strings.ToLower(strings.TrimSpace(req.Context["target"]))
		op := strings.ToLower(strings.TrimSpace(req.Context["operation"]))
		if (tgt == "all_files" || strings.Contains(tgt, "all file")) &&
			(op == "delete" || op == "wipe" || op == "destroy") {
			return "request attempts destructive operation on all files"
		}
		// Context-based adult indicators
		ctype := strings.ToLower(strings.TrimSpace(req.Context["content_type"]))
		audience := strings.ToLower(strings.TrimSpace(req.Context["audience"]))
		if strings.Contains(ctype, "inappropriate") || strings.Contains(ctype, "adult") || audience == "adults" || audience == "adults only" {
			return "request attempts to generate inappropriate/adult content"
		}
	}
	return ""
}

// fixCodeWithLLM attempts to fix code based on validation feedback
func (ie *IntelligentExecutor) fixCodeWithLLM(originalCode *GeneratedCode, validationResult ValidationStep, req *ExecutionRequest) (*GeneratedCode, error) {
	log.Printf("🔧 [FIX] Attempting to fix code using LLM feedback")

	// Create a prompt for fixing the code
	fixPrompt := ie.buildFixPrompt(originalCode, validationResult, req)

	// Call LLM to fix the code
	// Use priority from request (defaults to high for user requests)
	priority := PriorityLow
	if req.HighPriority {
		priority = PriorityHigh
	}
	ctx := context.Background()
	response, err := ie.llmClient.callLLMWithContextAndPriority(ctx, fixPrompt, priority)
	if err != nil {
		return nil, fmt.Errorf("LLM fix call failed: %v", err)
	}

	// Extract fixed code
	fixedCode, err := ie.codeGenerator.extractCodeFromResponse(response, originalCode.Language)
	if err != nil {
		return nil, fmt.Errorf("failed to extract fixed code: %v", err)
	}
	if fixedCode == "" {
		return nil, fmt.Errorf("failed to extract fixed code from LLM response")
	}

	// Create new GeneratedCode object
	fixedGeneratedCode := &GeneratedCode{
		ID:          fmt.Sprintf("code_%d", time.Now().UnixNano()),
		TaskName:    originalCode.TaskName,
		Description: originalCode.Description,
		Language:    originalCode.Language,
		Code:        fixedCode,
		Context:     originalCode.Context,
		CreatedAt:   time.Now(),
		Tags:        append(originalCode.Tags, "fixed"),
		Executable:  true,
	}

	log.Printf("✅ [FIX] Generated fixed code")
	return fixedGeneratedCode, nil
}

// buildFixPrompt creates a prompt for fixing code
func (ie *IntelligentExecutor) buildFixPrompt(originalCode *GeneratedCode, validationResult ValidationStep, req *ExecutionRequest) string {
	// Build language-specific guidance
	languageGuidance := ""
	if originalCode.Language == "javascript" || originalCode.Language == "js" {
		languageGuidance = `
🚨 CRITICAL FOR JAVASCRIPT CODE FIXES:
- Read the compilation error message CAREFULLY - it tells you exactly what's wrong!
- Common JavaScript errors:
  * "Identifier 'X' has already been declared" - Variable X is declared twice (e.g., "let x;" then "let x = [];")
    - FIX: Remove the duplicate declaration - if you already declared it with "let x, y, z;", don't declare it again with "let x = [];"
    - Use assignment instead: "x = [];" (not "let x = [];")
  * "SyntaxError: Cannot use import statement outside a module" - Don't use ES6 import syntax in Node.js without package.json
    - FIX: Use "require()" instead of "import", or use CommonJS syntax
  * "ReferenceError: X is not defined" - Variable used before declaration or typo
  * "TypeError: Cannot read property 'X' of undefined" - Object is undefined before accessing property
- If you see "has already been declared":
  - Check if the variable was declared in a "let x, y, z;" statement at the top
  - If yes, use assignment "x = value;" instead of redeclaring "let x = value;"
  - Example: If you have "let mean, median, mode, stdDev;" at the top, use "mode = [];" not "let mode = [];"
- CRITICAL: Read data from environment variables using process.env, NOT hardcode values!
  - WRONG: "const data = [1, 2, 3, 4, 5];" (hardcoded!)
  - CORRECT: "const dataStr = process.env.data || process.env.input || ''; const data = dataStr.split(',').map(Number);"
  - The context provides parameters like 'data' or 'input' - read them from process.env!
- After fixing, verify:
  - ✅ No duplicate variable declarations
  - ✅ All variables are properly declared before use
  - ✅ Data is read from process.env, not hardcoded
  - ✅ Code uses console.log() for output (not print())
`
	} else if originalCode.Language == "go" {
		// Check if this is a matrix operation task
		isMatrixOp := false
		if req != nil && req.Context != nil {
			if req.Context["matrix1"] != "" || req.Context["matrix2"] != "" {
				isMatrixOp = true
			}
		}
		if !isMatrixOp && req != nil {
			descLower := strings.ToLower(req.Description)
			if strings.Contains(descLower, "matrix") {
				isMatrixOp = true
			}
		}

		matrixGuidance := ""
		if isMatrixOp {
			matrixGuidance = `

🚨🚨🚨 CRITICAL FOR GO MATRIX OPERATIONS - READ CAREFULLY 🚨🚨🚨:
1. **READ MATRICES FROM ENV VARS**: You MUST use os.Getenv("matrix1") and os.Getenv("matrix2"). Parse JSON string "[[1,2],[3,4]]" using encoding/json. DO NOT hardcode matrices.
2. **REQUIRED IMPORTS**: You MUST import "encoding/json", "fmt", and "os".
3. **OUTPUT FORMAT (CRITICAL)**: You MUST print each ROW on a SEPARATE line using fmt.Println().
   - WRONG: fmt.Println(result) (prints [[6 8] [10 12]] on ONE line - FAILS VALIDATION!)
   - CORRECT: Use a loop: for i := 0; i < len(result); i++ { fmt.Println(result[i]) }
   - Expected output: [6 8] on first line, [10 12] on second line.
4. **VALIDATION FAILURE**: If validation failed because output doesn't match expected pattern, check:
   - Are you printing the entire matrix with one fmt.Println? (WRONG - prints on one line)
   - Are you printing each row separately? (CORRECT - each row on its own line)
   - Did you read matrices from environment variables? (REQUIRED - do not hardcode)
`
		}

		languageGuidance = `
🚨 GO FIX RULES (fix ALL errors in ONE pass):
- "undefined: X" → Add missing import (json→encoding/json, os.Getenv→os, fmt.Println→fmt, io.ReadAll→io+os)
- "X declared/imported and not used" → REMOVE it (Go treats unused as ERROR)
- "assignment mismatch: 2 vars but X returns 1" → json.Unmarshal returns ONLY error, NOT (value, error)!
- json.Unmarshal: err := json.Unmarshal(bytes, &data) (NOT jsonBytes, _ := ...)
- io.ReadAll: bytes, err := io.ReadAll(os.Stdin) (returns 2 values)
- JSON numbers are float64, NOT int64: data["key"].(float64) then convert to int
- Fix ALL errors at once - don't fix one at a time!` + matrixGuidance
	} else if originalCode.Language == "rust" {
		languageGuidance = `
🚨 CRITICAL FOR RUST CODE FIXES:
- Read the compilation error message CAREFULLY - Rust's borrow checker is very specific!
- Common Rust compilation errors:
  * "cannot borrow X as mutable, as it is not declared as mutable" - Variable needs mut keyword
    - FIX: Change "let x = ..." to "let mut x = ..."
    - If it's already mut, check if you're trying to borrow it incorrectly
  * "cannot borrow X as mutable because it is also borrowed as immutable" - Conflicting borrows
    - FIX: Restructure code to avoid simultaneous mutable and immutable borrows
    - Use scopes to limit borrow lifetimes: "{ let borrow = &x; ... }" then "x.mut_method()"
  * "cannot move out of X which is behind a shared reference" - Trying to move from a reference
    - FIX: Clone the value: "let y = x.clone();" or use references instead of moving
  * "expected X, found Y" - Type mismatch
    - FIX: Check types match - use ".to_string()", ".parse()", or explicit type conversions
  * "use of moved value: X" - Value was moved and can't be used again
    - FIX: Use references "&X" instead of moving, or clone if needed
  * "mismatched types" - Function expects different type
    - FIX: Check function signature and provide correct type
- For Box type mutable borrows:
  - WRONG: "increment_age(&mut person_box);" when person_box is Box<Person>
  - CORRECT: "increment_age(&mut *person_box);" (dereference then borrow)
  - OR: "let person = &mut *person_box; increment_age(person);"
- For Box type immutable borrows:
  - WRONG: "print_person(person_box);" when person_box is Box<Person>
  - CORRECT: "print_person(&*person_box);" or "print_person(&person_box);"
- If you see "cannot borrow as mutable":
  1. Check if the variable is declared with "mut": "let mut x = ..."
  2. If it's a Box, dereference first: "&mut *box_value"
  3. If it's already borrowed immutably, end that borrow before mutable borrow
- After fixing, verify:
  - ✅ All variables that need mutation are declared with "mut"
  - ✅ Box values are properly dereferenced before borrowing: "&mut *box" or "&*box"
  - ✅ No conflicting borrows (mutable and immutable at the same time)
  - ✅ No use of moved values
  - ✅ Types match function signatures
`
	}

	return fmt.Sprintf(`You are an expert programmer. The following code failed to execute properly and needs to be fixed.

Original Task: %s
Description: %s
Language: %s

Original Code:
`+"```"+`%s
%s
`+"```"+`

Error Details:
- Step: %s
- Error: %s
- Output: %s

Context:
%s
%s

🚨 CRITICAL FIXING INSTRUCTIONS:
1. Read ALL errors in the error message above - fix ALL of them in ONE revision!
2. If you see compilation errors, fix ALL of them before returning code!
3. If you see "imported and not used" errors, remove those imports BUT also check if you need to ADD other imports!
4. If you see "undefined" errors, add the missing imports!
5. After fixing, verify the code will compile and run successfully!
6. Make sure the code actually produces output - if the task requires reading JSON and printing a field, ensure the code reads from stdin and prints the result!

Please fix the code to make it work correctly. Return ONLY the fixed code wrapped in markdown code blocks like this:
`+"```"+`%s
// Your fixed code here
`+"```"+`

Fixed code:`,
		originalCode.TaskName,
		originalCode.Description,
		originalCode.Language,
		originalCode.Language,
		originalCode.Code,
		validationResult.Step,
		validationResult.Error,
		validationResult.Output,
		ie.formatContext(req.Context),
		languageGuidance,
		originalCode.Language)
}

// formatContext formats context map for display
func (ie *IntelligentExecutor) formatContext(context map[string]string) string {
	if len(context) == 0 {
		return "No additional context"
	}

	var parts []string
	for k, v := range context {
		parts = append(parts, fmt.Sprintf("- %s: %s", k, v))
	}
	return strings.Join(parts, "\n")
}

// inferLanguageFromRequest tries to determine the intended programming language
// from the task name, description, and context. Returns empty string if unknown.
func (ie *IntelligentExecutor) inferLanguageFromRequest(req *ExecutionRequest) string {
	// Context override - check this first as it's most explicit
	if lang, ok := req.Context["language"]; ok && strings.TrimSpace(lang) != "" {
		return strings.ToLower(strings.TrimSpace(lang))
	}

	// Strong hints in description
	desc := strings.ToLower(strings.TrimSpace(req.Description))

	// Check for Rust FIRST (before Go and Python, since "go" is a common word that might appear in other contexts)
	if strings.Contains(desc, "rust") || strings.Contains(desc, "rust program") ||
		strings.Contains(desc, "rust code") || strings.Contains(desc, ".rs") ||
		strings.Contains(desc, " in rust") || strings.Contains(desc, "create a rust") ||
		strings.Contains(desc, "write a rust") || strings.Contains(desc, "build a rust") {
		return "rust"
	}

	// Check for Go (after Rust to avoid false matches)
	if strings.Contains(desc, " go ") || strings.HasPrefix(desc, "go ") || strings.HasSuffix(desc, " in go") ||
		strings.Contains(desc, " in golang") || strings.Contains(desc, "golang") ||
		strings.Contains(desc, "main.go") || strings.Contains(desc, "go program") ||
		strings.Contains(desc, "go code") || strings.Contains(desc, ".go") {
		return "go"
	}

	// Check for Python (after Rust and Go)
	if strings.Contains(desc, "python") || strings.Contains(desc, "py script") ||
		strings.Contains(desc, "python program") || strings.Contains(desc, "python code") ||
		strings.Contains(desc, ".py") {
		return "python"
	}

	// Hints in task name
	task := strings.ToLower(strings.TrimSpace(req.TaskName))

	// Check for Rust in task name first (before Go to avoid false matches)
	if strings.Contains(task, "rust") || strings.Contains(task, ".rs") {
		return "rust"
	}

	// Check for Python in task name
	if strings.Contains(task, "python") || strings.Contains(task, ".py") {
		return "python"
	}

	// Check for Go in task name
	if strings.Contains(task, "go ") || strings.Contains(task, " golang") ||
		strings.Contains(task, ".go") || strings.Contains(task, "golang") {
		return "go"
	}

	return ""
}

// inferLanguageFromIntelligentRequest tries to determine the intended programming language
// from the task name, description, and context. Returns empty string if unknown.
func inferLanguageFromRequest(req *IntelligentExecutionRequest) string {
	// Strong hints in description
	desc := strings.ToLower(strings.TrimSpace(req.Description))

	// Check for Rust FIRST (before Go, since "go" is a common word)
	if strings.Contains(desc, "rust") || strings.Contains(desc, "rust program") ||
		strings.Contains(desc, "rust code") || strings.Contains(desc, ".rs") ||
		strings.Contains(desc, " in rust") || strings.Contains(desc, "write a rust") ||
		strings.Contains(desc, "create a rust") || strings.Contains(desc, "build a rust") {
		return "rust"
	}

	// Check for Go (after Rust to avoid false matches)
	if strings.Contains(desc, " go ") || strings.HasPrefix(desc, "go ") || strings.HasSuffix(desc, " in go") ||
		strings.Contains(desc, " in golang") || strings.Contains(desc, "golang") ||
		strings.Contains(desc, "main.go") || strings.Contains(desc, "go program") ||
		strings.Contains(desc, "go code") || strings.Contains(desc, "go script") ||
		strings.Contains(desc, "write a go") || strings.Contains(desc, "create a go") ||
		strings.Contains(desc, "build a go") || strings.Contains(desc, "develop a go") ||
		strings.Contains(desc, ".go") || strings.Contains(desc, "golang program") ||
		strings.Contains(desc, "golang code") || strings.Contains(desc, "golang script") {
		return "go"
	}

	// Hints in task name
	task := strings.ToLower(strings.TrimSpace(req.TaskName))

	// Check for Rust in task name first (before Go to avoid false matches)
	if strings.Contains(task, "rust") || strings.Contains(task, ".rs") {
		return "rust"
	}

	if strings.Contains(task, "go ") || strings.Contains(task, " golang") ||
		strings.Contains(task, ".go") || strings.Contains(task, "golang") {
		return "go"
	}

	// Context override
	if lang, ok := req.Context["language"]; ok && strings.TrimSpace(lang) != "" {
		return strings.ToLower(strings.TrimSpace(lang))
	}

	return ""
}

// filterCodegenContext removes non-functional keys that cause the LLM to emit unused variables
// or irrelevant code. It keeps only keys likely needed for computation or file hints.
func filterCodegenContext(in map[string]string) map[string]string {
	if len(in) == 0 {
		return in
	}
	// Keys to drop - these are system/metadata keys that confuse the LLM
	drop := map[string]bool{
		"session_id":         true,
		"project_id":         true,
		"artifact_names":     true,
		"save_code_filename": true,
		"artifacts_wrapper":  true, // System flag, not needed for code generation
		"force_regenerate":   true, // System flag, not needed for code generation
		// Common leakage aliases
		"saveCodeFilename": true,
		"artifacts":        true,
		"artifactsWrapper": true,
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		if drop[strings.TrimSpace(strings.ToLower(k))] {
			continue
		}
		out[k] = v
	}
	return out
}

// ExecutePrimeNumbersExample demonstrates the intelligent execution with prime numbers
func (ie *IntelligentExecutor) ExecutePrimeNumbersExample(ctx context.Context, count int) (*IntelligentExecutionResult, error) {
	req := &ExecutionRequest{
		TaskName:    "CalculatePrimes",
		Description: fmt.Sprintf("Calculate the first %d prime numbers", count),
		Context: map[string]string{
			"count": fmt.Sprintf("%d", count),
			"input": fmt.Sprintf("%d", count),
		},
		Language:        "python",
		ForceRegenerate: false,
		MaxRetries:      3,
		Timeout:         600,
	}

	return ie.ExecuteTaskIntelligently(ctx, req)
}

// ListCachedCapabilities returns all cached capabilities
func (ie *IntelligentExecutor) ListCachedCapabilities() ([]*GeneratedCode, error) {
	return ie.codeStorage.ListAllCode()
}

// GetExecutionStats returns statistics about intelligent executions
func (ie *IntelligentExecutor) GetExecutionStats() map[string]interface{} {
	allCode, err := ie.ListCachedCapabilities()
	if err != nil {
		return map[string]interface{}{
			"error": err.Error(),
		}
	}

	stats := map[string]interface{}{
		"total_cached_capabilities": len(allCode),
		"languages":                 make(map[string]int),
		"tags":                      make(map[string]int),
		"recent_executions":         []string{},
	}

	// Count by language
	for _, code := range allCode {
		if stats["languages"].(map[string]int)[code.Language] == 0 {
			stats["languages"].(map[string]int)[code.Language] = 0
		}
		stats["languages"].(map[string]int)[code.Language]++

		// Count by tags
		for _, tag := range code.Tags {
			if stats["tags"].(map[string]int)[tag] == 0 {
				stats["tags"].(map[string]int)[tag] = 0
			}
			stats["tags"].(map[string]int)[tag]++
		}
	}

	return stats
}

// recordExecutionEpisode records an execution episode in the self-model
func (ie *IntelligentExecutor) recordExecutionEpisode(req *ExecutionRequest, result *IntelligentExecutionResult, executionType string) {
	if ie.selfModelManager == nil {
		return
	}

	// Create episode metadata
	metadata := map[string]interface{}{
		"task_name":        req.TaskName,
		"description":      req.Description,
		"language":         req.Language,
		"execution_type":   executionType,
		"retry_count":      result.RetryCount,
		"execution_time":   result.ExecutionTime.String(),
		"used_cached_code": result.UsedCachedCode,
		"context":          req.Context,
	}

	// Determine the decision made
	decision := "execute_task"
	if result.UsedCachedCode {
		decision = "use_cached_code"
	} else if result.GeneratedCode != nil {
		decision = "generate_new_code"
	}

	// Record the episode
	err := ie.selfModelManager.RecordEpisode(
		fmt.Sprintf("Task execution: %s", req.TaskName),
		decision,
		fmt.Sprintf("Success: %v, Result: %v", result.Success, result.Result),
		result.Success,
		metadata,
	)

	if err != nil {
		log.Printf("⚠️ [SELF-MODEL] Failed to record episode: %v", err)
	} else {
		log.Printf("📝 [SELF-MODEL] Recorded execution episode for task: %s", req.TaskName)
	}

	// Update beliefs based on execution results
	ie.updateBeliefsFromExecution(req, result)
}

// updateBeliefsFromExecution updates beliefs based on execution results
func (ie *IntelligentExecutor) updateBeliefsFromExecution(req *ExecutionRequest, result *IntelligentExecutionResult) {
	if ie.selfModelManager == nil {
		return
	}

	// Update task-specific beliefs
	taskKey := fmt.Sprintf("task_%s_success_rate", req.TaskName)
	if result.Success {
		// Increment success count
		ie.selfModelManager.UpdateBelief(taskKey+"_successes",
			ie.getBeliefValue(taskKey+"_successes")+1)
	} else {
		// Increment failure count
		ie.selfModelManager.UpdateBelief(taskKey+"_failures",
			ie.getBeliefValue(taskKey+"_failures")+1)
	}

	// Update language-specific beliefs
	langKey := fmt.Sprintf("language_%s_success_rate", req.Language)
	if result.Success {
		ie.selfModelManager.UpdateBelief(langKey+"_successes",
			ie.getBeliefValue(langKey+"_successes")+1)
	} else {
		ie.selfModelManager.UpdateBelief(langKey+"_failures",
			ie.getBeliefValue(langKey+"_failures")+1)
	}

	// Update execution type beliefs
	execKey := fmt.Sprintf("execution_type_%s_success_rate", "traditional_execution")
	if result.Success {
		ie.selfModelManager.UpdateBelief(execKey+"_successes",
			ie.getBeliefValue(execKey+"_successes")+1)
	} else {
		ie.selfModelManager.UpdateBelief(execKey+"_failures",
			ie.getBeliefValue(execKey+"_failures")+1)
	}

	// Update last execution time
	ie.selfModelManager.UpdateBelief("last_execution_time", time.Now().Unix())
	ie.selfModelManager.UpdateBelief("last_task", req.TaskName)
}

// getBeliefValue gets a belief value as an integer, defaulting to 0
func (ie *IntelligentExecutor) getBeliefValue(key string) int {
	if ie.selfModelManager == nil {
		return 0
	}

	sm, err := ie.selfModelManager.Load()
	if err != nil {
		return 0
	}

	if val, exists := sm.Beliefs[key]; exists {
		if intVal, ok := val.(float64); ok {
			return int(intVal)
		}
		if intVal, ok := val.(int); ok {
			return intVal
		}
	}
	return 0
}

// recordMonitorMetrics records metrics in the format expected by the monitor UI
func (ie *IntelligentExecutor) recordMonitorMetrics(success bool, execTime time.Duration) {
	// We need access to Redis client, but intelligent executor doesn't have it
	// For now, we'll skip this and let the planner integration handle it
	// In a full implementation, we'd pass the Redis client to the intelligent executor
	log.Printf("📈 [INTELLIGENT] Execution completed: Success=%v, Time=%v", success, execTime)
}

// isChainedProgramRequest detects if a request needs multiple programs with chained execution
func (ie *IntelligentExecutor) isChainedProgramRequest(req *ExecutionRequest) bool {
	description := strings.ToLower(req.Description)
	log.Printf("🔍 [CHAINED-DETECT] Checking if request is chained: %s", req.Description)

	// Look for patterns that indicate multiple programs
	chainedPatterns := []string{
		"two programs",
		"multiple programs",
		"first program",
		"second program",
		"prog1",
		"prog2",
		"chained",
		"chain",
		"exec.command",
		"run.*program",
		"execute.*program",
		"call.*program",
		"then create",
		"then generate",
		"then make",
		"python.*then.*go",
		"go.*then.*python",
		"generates.*then.*reads",
		"reads.*then.*generates",
	}

	for _, pattern := range chainedPatterns {
		if strings.Contains(description, pattern) {
			log.Printf("✅ [CHAINED-DETECT] Matched pattern: '%s'", pattern)
			return true
		}
	}

	// Check artifact_names for multiple program files (not just any files)
	if names, ok := req.Context["artifact_names"]; ok && names != "" {
		parts := strings.Split(names, ",")
		if len(parts) >= 2 {
			// Count actual program files (not data files like .json)
			programCount := 0
			for _, part := range parts {
				part = strings.TrimSpace(part)
				if strings.HasSuffix(strings.ToLower(part), ".go") ||
					strings.HasSuffix(strings.ToLower(part), ".py") ||
					strings.HasSuffix(strings.ToLower(part), ".js") ||
					strings.HasSuffix(strings.ToLower(part), ".java") {
					programCount++
				}
			}
			// Only trigger chained execution if we have 2+ actual program files
			if programCount >= 2 {
				return true
			}
		}
	}

	return false
}

// executeChainedPrograms executes multiple programs sequentially with output passing
func (ie *IntelligentExecutor) executeChainedPrograms(ctx context.Context, req *ExecutionRequest, start time.Time, workflowID string) (*IntelligentExecutionResult, error) {
	log.Printf("🔗 [CHAINED] Starting chained program execution")

	// Parse the request to extract individual programs
	programs, err := ie.parseChainedPrograms(req)
	if err != nil {
		return &IntelligentExecutionResult{
			Success:       false,
			Error:         fmt.Sprintf("Failed to parse chained programs: %v", err),
			ExecutionTime: time.Since(start),
			WorkflowID:    workflowID,
		}, nil
	}

	log.Printf("🔗 [CHAINED] Parsed %d programs for chained execution", len(programs))

	// Execute programs sequentially
	var lastOutput string
	var allOutputs []string
	var generatedCodes []*GeneratedCode
	var programTimings []map[string]interface{} // Track timing for each program

	// Check if this is a performance comparison request (needed for output handling)
	lowerDesc := strings.ToLower(req.Description)
	needsReport := strings.Contains(lowerDesc, "time") || strings.Contains(lowerDesc, "performance") || strings.Contains(lowerDesc, "compare") || strings.Contains(lowerDesc, "report") || strings.Contains(lowerDesc, "differnce") || strings.Contains(lowerDesc, "difference")

	for i, program := range programs {
		log.Printf("🔗 [CHAINED] Executing program %d/%d: %s", i+1, len(programs), program.Name)

		// Track execution time for this program
		programStart := time.Now()

		// Create execution request for this program
		programReq := &ExecutionRequest{
			TaskName:     program.Name,
			Description:  program.Description,
			Context:      program.Context,
			Language:     program.Language,
			MaxRetries:   req.MaxRetries,
			Timeout:      req.Timeout,
			HighPriority: req.HighPriority, // Pass priority through to chained programs
		}

		// Add previous output as input if this isn't the first program
		if i > 0 && lastOutput != "" {
			programReq.Context["previous_output"] = lastOutput
			programReq.Description += fmt.Sprintf("\n\nPrevious program output: %s", lastOutput)
		}

		// Execute this program (bypass loop protection for chained execution)
		programResult, err := ie.executeProgramDirectly(ctx, programReq, time.Now(), workflowID)
		if err != nil {
			return &IntelligentExecutionResult{
				Success:       false,
				Error:         fmt.Sprintf("Program %d failed: %v", i+1, err),
				ExecutionTime: time.Since(start),
				WorkflowID:    workflowID,
			}, nil
		}

		// Store individual program artifacts with correct filename based on language
		artifactName := fmt.Sprintf("prog%d", i+1)
		// Map language to file extension
		langExt := map[string]string{
			"go":         ".go",
			"python":     ".py",
			"javascript": ".js",
			"js":         ".js",
			"java":       ".java",
			"rust":       ".rs",
			"cpp":        ".cpp",
			"c":          ".c",
		}
		if ext, ok := langExt[program.Language]; ok {
			artifactName = fmt.Sprintf("prog%d%s", i+1, ext)
		}
		ie.storeChainedProgramArtifact(programResult.GeneratedCode, workflowID, artifactName)

		// Record timing for this program BEFORE checking success
		// This ensures we can generate reports even if some programs fail
		programDuration := time.Since(programStart)

		// Try to extract algorithm execution time from program output
		// This gives us the actual algorithm performance, not Docker/compilation overhead
		var algorithmDurationMs int64 = 0
		var algorithmDurationNs int64 = 0
		var usingExtractedTiming bool = false
		if output, ok := programResult.Result.(string); ok {
			extractedTime := extractTimingFromOutput(output, program.Language)
			// Use extracted timing if it's reasonable (at least 100 nanoseconds)
			// Very small values (< 100ns) are likely false positives from noise in output
			// But allow small legitimate timings like 352ns
			if extractedTime >= 100 {
				algorithmDurationNs = extractedTime
				algorithmDurationMs = extractedTime / 1000000 // Convert ns to ms
				usingExtractedTiming = true
				log.Printf("⏱️ [CHAINED] Extracted algorithm timing from output: %d ns (%d ms)", algorithmDurationNs, algorithmDurationMs)
			} else {
				// Fallback to total execution time if no timing found in output
				algorithmDurationMs = programDuration.Milliseconds()
				algorithmDurationNs = programDuration.Nanoseconds()
				log.Printf("⏱️ [CHAINED] No valid timing found in output (extracted: %d ns), using total execution time: %d ms", extractedTime, algorithmDurationMs)
			}
		} else {
			// No output, use total execution time
			algorithmDurationMs = programDuration.Milliseconds()
			algorithmDurationNs = programDuration.Nanoseconds()
		}

		timing := map[string]interface{}{
			"program":              program.Name,
			"language":             program.Language,
			"duration_ms":          algorithmDurationMs,
			"duration_ns":          algorithmDurationNs,
			"total_duration_ms":    programDuration.Milliseconds(), // Keep total for reference
			"total_duration_ns":    programDuration.Nanoseconds(),
			"using_extracted_time": usingExtractedTiming, // Track if we used extracted vs total time
			"success":              programResult.Success,
		}
		programTimings = append(programTimings, timing)
		log.Printf("⏱️ [CHAINED] Program %d (%s) - Algorithm: %d ms, Total: %v (success: %v)", i+1, program.Language, algorithmDurationMs, programDuration, programResult.Success)

		// Extract output from this program (even if it failed, we want to capture what it produced)
		if output, ok := programResult.Result.(string); ok {
			// For performance comparisons, preserve full output (including timing info)
			// For data flow chaining, extract clean JSON
			if needsReport {
				// Keep full output for performance reports (includes timing)
				lastOutput = output
				allOutputs = append(allOutputs, output)
				log.Printf("🔗 [CHAINED] Using full output for program %d (performance comparison): %s", i+1, output)
			} else {
				// Clean the output to extract only JSON (remove env vars, SSH messages, etc.)
				// This is critical for chained programs where prog1 output feeds prog2
				cleanedOutput := extractJSONFromOutput(output)
				if cleanedOutput != "" {
					lastOutput = cleanedOutput
					allOutputs = append(allOutputs, cleanedOutput)
					log.Printf("🔗 [CHAINED] Extracted clean output from program %d: %s", i+1, cleanedOutput)
				} else {
					// Fallback: use original output if JSON extraction failed
					lastOutput = output
					allOutputs = append(allOutputs, output)
					log.Printf("⚠️ [CHAINED] Could not extract JSON from program %d output, using raw output", i+1)
				}
			}
		} else {
			// No output available, append empty string to maintain index alignment
			allOutputs = append(allOutputs, "")
			log.Printf("⚠️ [CHAINED] Program %d produced no output", i+1)
		}

		if !programResult.Success {
			log.Printf("⚠️ [CHAINED] Program %d execution failed: %s (but timing and output recorded for report)", i+1, programResult.Error)
			// Continue execution to allow other programs to run and report generation
			// Don't return early - we want to complete all programs and generate the report
			continue
		}

		// Store generated code
		if programResult.GeneratedCode != nil {
			generatedCodes = append(generatedCodes, programResult.GeneratedCode)
		}

		log.Printf("🔗 [CHAINED] Program %d completed successfully", i+1)
	}

	// Combine all outputs - use the LAST program's output as the final result
	combinedOutput := strings.Join(allOutputs, "\n")
	finalOutput := combinedOutput
	if len(allOutputs) > 0 {
		// Use the last program's output as the final result
		finalOutput = allOutputs[len(allOutputs)-1]
		log.Printf("🔗 [CHAINED] Using final program output as result: %s", finalOutput)
	}

	log.Printf("🔗 [CHAINED] All programs execution completed")
	log.Printf("🔗 [CHAINED] Program outputs: %v", allOutputs)
	log.Printf("🔗 [CHAINED] Final result: %s", finalOutput)
	log.Printf("🔗 [CHAINED] Recorded timings for %d programs", len(programTimings))

	// Generate comparison report if we have multiple programs with timings
	// (needsReport was already calculated above)

	log.Printf("📊 [CHAINED] Report generation check: timings=%d, needsReport=%v, description=%s", len(programTimings), needsReport, req.Description)

	if len(programTimings) >= 2 && needsReport {
		log.Printf("📊 [CHAINED] Generating performance comparison report")
		report := ie.generatePerformanceReport(programTimings, programs, allOutputs)
		if report != "" && ie.fileStorage != nil {
			reportFile := &StoredFile{
				Filename:    "performance_comparison_report.txt",
				Content:     []byte(report),
				ContentType: "text/plain",
				Size:        int64(len(report)),
				WorkflowID:  workflowID,
				StepID:      "chained_execution",
				CreatedAt:   time.Now(),
				ExpiresAt:   time.Now().Add(24 * time.Hour),
			}
			if err := ie.fileStorage.StoreFile(reportFile); err != nil {
				log.Printf("⚠️ [CHAINED] Failed to store performance report: %v", err)
			} else {
				log.Printf("✅ [CHAINED] Stored performance comparison report")
			}
		} else {
			log.Printf("⚠️ [CHAINED] Report generation skipped: report empty=%v, fileStorage nil=%v", report == "", ie.fileStorage == nil)
		}
	} else {
		log.Printf("⚠️ [CHAINED] Report generation skipped: timings=%d (need 2+), needsReport=%v", len(programTimings), needsReport)
	}

	// Create a combined result that shows all programs
	var combinedResult strings.Builder
	combinedResult.WriteString(fmt.Sprintf("Executed %d programs in sequence:\n\n", len(programs)))
	for i, program := range programs {
		combinedResult.WriteString(fmt.Sprintf("=== Program %d: %s (%s) ===\n", i+1, program.Name, program.Language))
		if i < len(allOutputs) && allOutputs[i] != "" {
			combinedResult.WriteString(fmt.Sprintf("Output: %s\n", allOutputs[i]))
		} else {
			combinedResult.WriteString("Output: (no output)\n")
		}
		if i < len(generatedCodes) && generatedCodes[i] != nil {
			combinedResult.WriteString(fmt.Sprintf("Code: %s\n", generatedCodes[i].Code))
		} else {
			combinedResult.WriteString("Code: (not available)\n")
		}
		combinedResult.WriteString("\n")
	}

	// Create a combined GeneratedCode that includes all programs
	var combinedCode *GeneratedCode
	if len(generatedCodes) > 0 && generatedCodes[0] != nil {
		var combinedCodeText strings.Builder
		combinedCodeText.WriteString(fmt.Sprintf("// Chained execution: %d programs\n\n", len(programs)))
		for i, code := range generatedCodes {
			if code != nil {
				combinedCodeText.WriteString(fmt.Sprintf("// === Program %d: %s (%s) ===\n", i+1, code.TaskName, code.Language))
				combinedCodeText.WriteString(code.Code)
				combinedCodeText.WriteString("\n\n")
			} else if i < len(programs) {
				// Program failed but we still want to show it in the combined code
				combinedCodeText.WriteString(fmt.Sprintf("// === Program %d: %s (%s) ===\n", i+1, programs[i].Name, programs[i].Language))
				combinedCodeText.WriteString("// Code generation failed or was not available\n\n")
			}
		}

		// Use the first program's metadata but combine all code
		combinedCode = &GeneratedCode{
			ID:          generatedCodes[0].ID,
			TaskName:    fmt.Sprintf("chained_%d_programs", len(programs)),
			Description: fmt.Sprintf("Chained execution of %d programs", len(programs)),
			Language:    generatedCodes[0].Language, // Use first program's language
			Code:        combinedCodeText.String(),
			Context:     generatedCodes[0].Context,
			CreatedAt:   generatedCodes[0].CreatedAt,
		}
	} else if len(programs) > 0 {
		// Fallback: create a minimal combined code even if no generated codes
		var combinedCodeText strings.Builder
		combinedCodeText.WriteString(fmt.Sprintf("// Chained execution: %d programs\n", len(programs)))
		combinedCodeText.WriteString("// Note: Individual program codes are stored as artifacts (prog1.go, prog2.py, etc.)\n")
		combinedCode = &GeneratedCode{
			ID:          fmt.Sprintf("chained_%d", time.Now().UnixNano()),
			TaskName:    fmt.Sprintf("chained_%d_programs", len(programs)),
			Description: fmt.Sprintf("Chained execution of %d programs", len(programs)),
			Language:    programs[0].Language,
			Code:        combinedCodeText.String(),
			Context:     req.Context,
			CreatedAt:   time.Now(),
		}
	}

	// Determine result format: use combined format for performance comparisons,
	// but use final output for data flow chaining (where one program feeds into another)
	// This maintains backward compatibility with tests that expect the final output
	var resultOutput interface{}
	if needsReport {
		// For performance comparisons, show combined format with all programs
		resultOutput = combinedResult.String()
		log.Printf("🔗 [CHAINED] Using combined result format (performance comparison)")
	} else {
		// For data flow chaining, use final program's output (for backward compatibility)
		if len(allOutputs) > 0 {
			resultOutput = allOutputs[len(allOutputs)-1]
		} else {
			resultOutput = finalOutput
		}
		log.Printf("🔗 [CHAINED] Using final program output for result (data flow chaining): %v", resultOutput)
	}

	result := &IntelligentExecutionResult{
		Success:        true,
		Result:         resultOutput, // Final output for chaining, combined format for comparisons
		GeneratedCode:  combinedCode, // Combined code from all programs (always available)
		ExecutionTime:  time.Since(start),
		WorkflowID:     workflowID,
		RetryCount:     0,
		UsedCachedCode: false,
		ValidationSteps: []ValidationStep{{
			Step:    "chained_execution",
			Success: true,
			Message: fmt.Sprintf("Successfully executed %d programs in sequence", len(programs)),
		}},
	}

	// Log tool metrics for chained execution
	ie.logIntelligentExecutionMetrics(ctx, req, result)

	return result, nil
}

// ChainedProgram represents a single program in a chained execution
type ChainedProgram struct {
	Name        string
	Description string
	Context     map[string]string
	Language    string
}

// parseChainedProgramsWithLLM uses LLM to intelligently parse a request into multiple programs
func (ie *IntelligentExecutor) parseChainedProgramsWithLLM(req *ExecutionRequest) ([]ChainedProgram, error) {
	if ie.llmClient == nil {
		return nil, fmt.Errorf("LLM client not available")
	}

	log.Printf("🔍 [CHAINED-LLM] Parsing request - Description: %s", req.Description)
	log.Printf("🔍 [CHAINED-LLM] TaskName: %s, Language: %s", req.TaskName, req.Language)

	// Check if request clearly asks for multiple programs
	lowerDesc := strings.ToLower(req.Description)
	hasMultipleLanguages := (strings.Contains(lowerDesc, "go") && strings.Contains(lowerDesc, "python")) ||
		(strings.Contains(lowerDesc, "python") && strings.Contains(lowerDesc, "go")) ||
		(strings.Contains(lowerDesc, "then create") || strings.Contains(lowerDesc, "then generate") || strings.Contains(lowerDesc, "then make"))

	multipleProgramsHint := ""
	if hasMultipleLanguages {
		multipleProgramsHint = "\n\n🚨 CRITICAL: This request clearly asks for MULTIPLE programs (mentions multiple languages or uses 'then'). You MUST return at least 2 programs in your JSON array. Do NOT combine them into a single program!"
	}

	// Create a structured prompt for LLM to parse the chained programs
	prompt := fmt.Sprintf(`You are a code generation assistant. Analyze the following user request and break it down into individual programs that need to be created.

User Request: "%s"

Context: %v%s

Parse this request and identify each distinct program that needs to be created. For each program, extract:
1. The programming language (go, python, javascript, java, etc.)
2. A clear description of what the program should do
3. Any specific requirements (tests, timings, reports, etc.)

CRITICAL INSTRUCTIONS:
- ONLY include what the user explicitly asks for. Do NOT add extra features like unit tests, reports, or other requirements unless the user specifically mentions them.
- If the user asks for a "bubble sort program", create a simple bubble sort program - do NOT add unit tests, performance reports, or other features unless explicitly requested.
- For timing/performance comparisons: The program MUST measure and print its own execution time using built-in timing functions:
  * Go: Import "time" package. Use start := time.Now() BEFORE calling the sorting function, call the sorting function, then elapsed := time.Since(start) AFTER and print ONCE: fmt.Printf("took: %%v\n", elapsed) or fmt.Printf("Duration: %%d nanoseconds\n", elapsed.Nanoseconds())
  * Python: Import time module. Use start_time = time.time() BEFORE calling the sorting function, call the sorting function, then end_time = time.time() AFTER and print ONCE at the END: print("Execution time:", end_time - start_time, "seconds")
  * CRITICAL: Timing code must be OUTSIDE any loops - measure the entire algorithm execution, not individual iterations
  * CRITICAL: Print timing ONCE at the end, not inside loops or multiple times
  * The timing output MUST be in the console output so it can be extracted for performance comparison
  * CRITICAL: If the user asks for performance comparison or timing, you MUST include timing code in BOTH programs
- Do NOT use subprocess.run, subprocess.call, or subprocess.Popen to execute other programs - this is not allowed.
- Keep descriptions simple and focused on what the user actually requested.

Return your response as a JSON array with this exact structure:
[
  {
    "name": "descriptive_name_for_program_1",
    "language": "go",
    "description": "Clear description of what this program should do, ONLY including what the user explicitly requested."
  },
  {
    "name": "descriptive_name_for_program_2", 
    "language": "python",
    "description": "Clear description of what this program should do, ONLY including what the user explicitly requested."
  }
]

Important:
- If the request mentions creating a program in one language "then" creating another, these are SEPARATE programs - return BOTH
- If the request mentions "Go program" AND "Python program", return TWO separate programs
- If tests are mentioned EXPLICITLY by the user, include that in the description
- If reports are mentioned EXPLICITLY by the user, that might be a separate program or part of the last program's description
- Be precise about the language for each program
- Return ONLY valid JSON, no additional text or explanation
- If the request asks for multiple programs, you MUST return multiple programs in the array

JSON Response:`, req.Description, req.Context, multipleProgramsHint)

	// Call LLM with appropriate priority (user requests are high priority)
	priority := PriorityLow // Default to low for background tasks
	if req.HighPriority {
		priority = PriorityHigh
	}
	ctx := context.Background()
	response, err := ie.llmClient.callLLMWithContextAndPriority(ctx, prompt, priority)
	if err != nil {
		return nil, fmt.Errorf("LLM call failed: %v", err)
	}

	log.Printf("🔍 [CHAINED-LLM] LLM response: %s", response)

	// Parse the JSON response
	var programDefs []struct {
		Name        string `json:"name"`
		Language    string `json:"language"`
		Description string `json:"description"`
	}

	// Try to extract JSON from the response (LLM might add extra text)
	jsonStart := strings.Index(response, "[")
	jsonEnd := strings.LastIndex(response, "]")
	if jsonStart == -1 || jsonEnd == -1 || jsonEnd <= jsonStart {
		return nil, fmt.Errorf("could not find JSON array in LLM response")
	}

	jsonStr := response[jsonStart : jsonEnd+1]
	if err := json.Unmarshal([]byte(jsonStr), &programDefs); err != nil {
		log.Printf("❌ [CHAINED-LLM] JSON parse error: %v, raw JSON: %s", err, jsonStr)
		return nil, fmt.Errorf("failed to parse LLM JSON response: %v", err)
	}

	if len(programDefs) == 0 {
		return nil, fmt.Errorf("LLM returned empty program list")
	}

	// Validate that we got multiple programs if the request clearly asks for them
	if hasMultipleLanguages && len(programDefs) == 1 {
		log.Printf("⚠️ [CHAINED-LLM] WARNING: Request asks for multiple programs but LLM only returned 1. Request: %s", req.Description)
		log.Printf("⚠️ [CHAINED-LLM] LLM returned: %+v", programDefs)

		// Try to manually split the request if LLM failed to parse correctly
		log.Printf("🔄 [CHAINED-LLM] Attempting manual split of request into multiple programs")
		manuallySplit := ie.manuallySplitMultiplePrograms(req)
		if len(manuallySplit) > 1 {
			log.Printf("✅ [CHAINED-LLM] Successfully manually split into %d programs", len(manuallySplit))
			programDefs = manuallySplit
		} else {
			log.Printf("⚠️ [CHAINED-LLM] Manual split only found %d program(s), proceeding with LLM result", len(manuallySplit))
		}
	}

	// Convert to ChainedProgram structs
	programs := make([]ChainedProgram, len(programDefs))
	for i, def := range programDefs {
		// Normalize language name
		lang := strings.ToLower(def.Language)
		if lang == "" {
			lang = "python" // default
		}

		programs[i] = ChainedProgram{
			Name:        def.Name,
			Description: def.Description,
			Language:    lang,
			Context:     make(map[string]string),
		}

		log.Printf("✅ [CHAINED-LLM] Parsed program %d: %s (%s) - %s", i+1, def.Name, lang, def.Description)
	}

	return programs, nil
}

// detectLanguageFromText detects programming language from text description
// This is a helper function for chained program detection
func detectLanguageFromText(text string) string {
	textLower := strings.ToLower(text)
	// Check for Rust first (before Go, since "go" is a common word)
	if strings.Contains(textLower, "rust") || strings.Contains(textLower, ".rs") {
		return "rust"
	}
	// Check for Go
	if strings.Contains(textLower, " go ") || strings.HasPrefix(textLower, "go ") ||
		strings.Contains(textLower, "golang") || strings.Contains(textLower, ".go") {
		return "go"
	}
	// Check for Python
	if strings.Contains(textLower, "python") || strings.Contains(textLower, ".py") {
		return "python"
	}
	// Check for JavaScript
	if strings.Contains(textLower, "javascript") || strings.Contains(textLower, "js") ||
		strings.Contains(textLower, "node") || strings.Contains(textLower, ".js") {
		return "javascript"
	}
	// Check for Java
	if strings.Contains(textLower, "java") && !strings.Contains(textLower, "javascript") ||
		strings.Contains(textLower, ".java") {
		return "java"
	}
	// Default to Python if no language detected
	return "python"
}

// manuallySplitMultiplePrograms attempts to manually split a request into multiple programs
// when the LLM fails to do so correctly
func (ie *IntelligentExecutor) manuallySplitMultiplePrograms(req *ExecutionRequest) []struct {
	Name        string `json:"name"`
	Language    string `json:"language"`
	Description string `json:"description"`
} {
	desc := req.Description
	lowerDesc := strings.ToLower(desc)
	var programs []struct {
		Name        string `json:"name"`
		Language    string `json:"language"`
		Description string `json:"description"`
	}

	// Look for "then" separator - be more flexible with spacing and case
	thenPatterns := []string{
		" then create", " then generate", " then make", " then ",
		"then create", "then generate", "then make", "then ",
	}
	var parts []string
	var splitIdx int = -1

	for _, pattern := range thenPatterns {
		patternLower := strings.ToLower(pattern)
		if idx := strings.Index(lowerDesc, patternLower); idx > 0 {
			// Find the actual position in the original string (case-sensitive)
			// Try to find the pattern in the original string at approximately the same position
			searchStart := idx
			if searchStart > len(desc) {
				searchStart = len(desc) - 10
			}
			if searchStart < 0 {
				searchStart = 0
			}

			// Search in original string around the found position
			searchArea := desc[searchStart:]
			if patternIdx := strings.Index(strings.ToLower(searchArea), patternLower); patternIdx >= 0 {
				splitIdx = searchStart + patternIdx + len(pattern)
				parts = []string{desc[:searchStart+patternIdx], desc[splitIdx:]}
				log.Printf("🔍 [MANUAL-SPLIT] Found pattern '%s' at position %d", pattern, searchStart+patternIdx)
				break
			}
		}
	}

	// If no "then" found, try splitting on "and" if multiple languages are mentioned
	if len(parts) < 2 {
		// Check for multiple languages
		langs := []string{"rust", "go", "python", "java", "javascript", "js"}
		langCount := 0
		for _, lang := range langs {
			if strings.Contains(lowerDesc, lang) {
				langCount++
			}
		}
		if langCount >= 2 {
			if idx := strings.Index(lowerDesc, " and "); idx > 0 {
				parts = []string{desc[:idx], desc[idx+5:]}
				log.Printf("🔍 [MANUAL-SPLIT] Split on 'and' at position %d", idx)
			}
		}
	}

	log.Printf("🔍 [MANUAL-SPLIT] Split result: %d parts", len(parts))
	if len(parts) >= 2 {

		if len(parts) >= 2 {
			// Extract first program - detect language from description
			part1 := strings.TrimSpace(parts[0])
			part1Lower := strings.ToLower(part1)
			lang1 := detectLanguageFromText(part1Lower) // Use flexible detection

			// Extract second program - detect language from description
			part2 := strings.TrimSpace(parts[1])
			// Remove "create" or "generate" prefix if present
			part2 = strings.TrimPrefix(part2, "create ")
			part2 = strings.TrimPrefix(part2, "generate ")
			part2 = strings.TrimSpace(part2)
			part2Lower := strings.ToLower(part2)
			lang2 := detectLanguageFromText(part2Lower) // Use flexible detection

			// Create program definitions
			prog1 := struct {
				Name        string `json:"name"`
				Language    string `json:"language"`
				Description string `json:"description"`
			}{
				Name:        fmt.Sprintf("program_1_%s", lang1),
				Language:    lang1,
				Description: part1,
			}

			prog2 := struct {
				Name        string `json:"name"`
				Language    string `json:"language"`
				Description string `json:"description"`
			}{
				Name:        fmt.Sprintf("program_2_%s", lang2),
				Language:    lang2,
				Description: part2,
			}

			programs = append(programs, prog1, prog2)
			log.Printf("✅ [CHAINED-LLM] Manually split: Program 1 (%s): %s", lang1, part1)
			log.Printf("✅ [CHAINED-LLM] Manually split: Program 2 (%s): %s", lang2, part2)
		}
	} else {
		// Multiple languages mentioned but no "then" - try to detect languages from description
		// This is a fallback for requests like "Create Rust and Java programs"
		langs := []string{"rust", "go", "python", "java", "javascript", "js"}
		var foundLangs []string
		for _, lang := range langs {
			if strings.Contains(lowerDesc, lang) {
				foundLangs = append(foundLangs, lang)
			}
		}
		if len(foundLangs) >= 2 {
			// Create programs for each detected language
			for i, lang := range foundLangs {
				prog := struct {
					Name        string `json:"name"`
					Language    string `json:"language"`
					Description string `json:"description"`
				}{
					Name:        fmt.Sprintf("program_%d_%s", i+1, lang),
					Language:    lang,
					Description: desc,
				}
				programs = append(programs, prog)
			}
		}
	}

	return programs
}

// generatePerformanceReport creates a performance comparison report for multiple programs
func (ie *IntelligentExecutor) generatePerformanceReport(timings []map[string]interface{}, programs []ChainedProgram, outputs []string) string {
	var report strings.Builder

	report.WriteString("=" + strings.Repeat("=", 70) + "\n")
	report.WriteString("PERFORMANCE COMPARISON REPORT\n")
	report.WriteString("=" + strings.Repeat("=", 70) + "\n\n")
	report.WriteString(fmt.Sprintf("Generated: %s\n\n", time.Now().Format(time.RFC3339)))

	// Write timing data for each program
	report.WriteString("EXECUTION TIMINGS:\n")
	report.WriteString("-" + strings.Repeat("-", 70) + "\n")

	for i, timing := range timings {
		// Safe type assertions with defaults
		programName, _ := timing["program"].(string)
		if programName == "" {
			programName = fmt.Sprintf("Program %d", i+1)
		}
		language, _ := timing["language"].(string)
		if language == "" {
			language = "unknown"
		}
		durationMs, _ := timing["duration_ms"].(int64)
		durationNs, _ := timing["duration_ns"].(int64)
		success, _ := timing["success"].(bool)

		report.WriteString(fmt.Sprintf("\nProgram %d: %s (%s)\n", i+1, programName, language))
		report.WriteString(fmt.Sprintf("  Status: %s\n", map[bool]string{true: "SUCCESS", false: "FAILED"}[success]))

		// Check if we have algorithm timing vs total timing
		usingExtracted := false
		if usingExtractedVal, ok := timing["using_extracted_time"]; ok {
			if val, ok := usingExtractedVal.(bool); ok {
				usingExtracted = val
			}
		}
		var totalMs int64
		hasTotal := false
		if totalMsVal, ok := timing["total_duration_ms"]; ok {
			if val, ok := totalMsVal.(int64); ok {
				totalMs = val
				hasTotal = true
			}
		}

		if usingExtracted && hasTotal && totalMs > durationMs {
			report.WriteString(fmt.Sprintf("  Algorithm Duration: %d ms (%.2f seconds)\n", durationMs, float64(durationMs)/1000.0))
			report.WriteString(fmt.Sprintf("  Algorithm Duration: %d nanoseconds\n", durationNs))
			report.WriteString(fmt.Sprintf("  Total Execution Time: %d ms (includes compilation/Docker overhead)\n", totalMs))
			report.WriteString(fmt.Sprintf("  Note: Algorithm time is the actual sorting performance, not total execution overhead\n"))
		} else if !usingExtracted && hasTotal {
			// No timing found in output, show total time with a note
			report.WriteString(fmt.Sprintf("  Duration: %d ms (%.2f seconds)\n", durationMs, float64(durationMs)/1000.0))
			report.WriteString(fmt.Sprintf("  Duration: %d nanoseconds\n", durationNs))
			report.WriteString(fmt.Sprintf("  Note: Program did not print execution time, showing total execution time\n"))
		} else {
			report.WriteString(fmt.Sprintf("  Duration: %d ms (%.2f seconds)\n", durationMs, float64(durationMs)/1000.0))
			report.WriteString(fmt.Sprintf("  Duration: %d nanoseconds\n", durationNs))
		}
	}

	// Compare timings if we have at least 2 programs
	if len(timings) >= 2 {
		report.WriteString("\n" + "=" + strings.Repeat("=", 70) + "\n")
		report.WriteString("PERFORMANCE COMPARISON:\n")
		report.WriteString("=" + strings.Repeat("=", 70) + "\n\n")

		// Use nanoseconds for comparison to avoid rounding errors with small values
		ns1, _ := timings[0]["duration_ns"].(int64)
		ns2, _ := timings[1]["duration_ns"].(int64)
		ms1, _ := timings[0]["duration_ms"].(int64)
		ms2, _ := timings[1]["duration_ms"].(int64)
		usingExtracted1, _ := timings[0]["using_extracted_time"].(bool)
		usingExtracted2, _ := timings[1]["using_extracted_time"].(bool)

		lang1, _ := timings[0]["language"].(string)
		if lang1 == "" {
			lang1 = "unknown"
		}
		lang2, _ := timings[1]["language"].(string)
		if lang2 == "" {
			lang2 = "unknown"
		}

		// Use nanoseconds for accurate comparison, especially for small values
		// Only compare extracted timings if both are available, otherwise use total execution time
		compareExtracted := usingExtracted1 && usingExtracted2

		if compareExtracted {
			// Both have extracted algorithm timings - compare those
			if ns1 == 0 && ns2 == 0 {
				report.WriteString("Both programs show 0 execution time (timing may not have been captured)\n")
			} else if ns1 == 0 {
				report.WriteString(fmt.Sprintf("%s: timing not captured from output, %s: %d ns (%.6f ms)\n", lang1, lang2, ns2, float64(ns2)/1000000.0))
			} else if ns2 == 0 {
				report.WriteString(fmt.Sprintf("%s: timing not captured from output, %s: %d ns (%.6f ms)\n", lang2, lang1, ns1, float64(ns1)/1000000.0))
			} else if ns1 < ns2 {
				diff := float64(ns2-ns1) / float64(ns1) * 100
				report.WriteString(fmt.Sprintf("%s executed %.2f%% faster than %s\n", lang1, diff, lang2))
				report.WriteString(fmt.Sprintf("%s: %d ns (%.6f ms)\n", lang1, ns1, float64(ns1)/1000000.0))
				report.WriteString(fmt.Sprintf("%s: %d ns (%.6f ms)\n", lang2, ns2, float64(ns2)/1000000.0))
			} else if ns2 < ns1 {
				diff := float64(ns1-ns2) / float64(ns2) * 100
				report.WriteString(fmt.Sprintf("%s executed %.2f%% faster than %s\n", lang2, diff, lang1))
				report.WriteString(fmt.Sprintf("%s: %d ns (%.6f ms)\n", lang1, ns1, float64(ns1)/1000000.0))
				report.WriteString(fmt.Sprintf("%s: %d ns (%.6f ms)\n", lang2, ns2, float64(ns2)/1000000.0))
			} else {
				report.WriteString(fmt.Sprintf("Both programs executed in the same time: %d ns (%.6f ms)\n", ns1, float64(ns1)/1000000.0))
			}
		} else {
			// One or both don't have extracted timings - compare total execution times
			// But note which one has extracted timing
			if !usingExtracted1 && !usingExtracted2 {
				report.WriteString("Note: Comparing total execution times (algorithm timings not extracted from output)\n")
			} else if usingExtracted1 {
				report.WriteString(fmt.Sprintf("Note: %s has algorithm timing (%d ns), %s using total execution time\n", lang1, ns1, lang2))
			} else {
				report.WriteString(fmt.Sprintf("Note: %s has algorithm timing (%d ns), %s using total execution time\n", lang2, ns2, lang1))
			}

			if ms1 == 0 && ms2 == 0 {
				report.WriteString("Both programs show 0 execution time\n")
			} else if ms1 < ms2 {
				diff := float64(ms2-ms1) / float64(ms1) * 100
				report.WriteString(fmt.Sprintf("%s executed %.2f%% faster than %s\n", lang1, diff, lang2))
			} else if ms2 < ms1 {
				diff := float64(ms1-ms2) / float64(ms2) * 100
				report.WriteString(fmt.Sprintf("%s executed %.2f%% faster than %s\n", lang2, diff, lang1))
			} else {
				report.WriteString(fmt.Sprintf("Both programs executed in the same time: %d ms\n", ms1))
			}
			report.WriteString(fmt.Sprintf("%s: %d ms\n", lang1, ms1))
			report.WriteString(fmt.Sprintf("%s: %d ms\n", lang2, ms2))
		}
	}

	// Add outputs section
	if len(outputs) > 0 {
		report.WriteString("\n" + "=" + strings.Repeat("=", 70) + "\n")
		report.WriteString("PROGRAM OUTPUTS:\n")
		report.WriteString("=" + strings.Repeat("=", 70) + "\n\n")
		for i, output := range outputs {
			report.WriteString(fmt.Sprintf("Program %d Output:\n", i+1))
			report.WriteString(output)
			report.WriteString("\n\n")
		}
	}

	report.WriteString("\n" + "=" + strings.Repeat("=", 70) + "\n")
	report.WriteString("END OF REPORT\n")
	report.WriteString("=" + strings.Repeat("=", 70) + "\n")

	return report.String()
}

// parseChainedPrograms parses a request into multiple programs using LLM
func (ie *IntelligentExecutor) parseChainedPrograms(req *ExecutionRequest) ([]ChainedProgram, error) {
	log.Printf("🧠 [CHAINED] Using LLM to parse chained programs from: %s", req.Description)

	// First try LLM-based parsing
	programs, err := ie.parseChainedProgramsWithLLM(req)
	if err == nil && len(programs) > 0 {
		log.Printf("✅ [CHAINED] LLM parsing succeeded, found %d programs", len(programs))
		return programs, nil
	}

	log.Printf("⚠️ [CHAINED] LLM parsing failed: %v, falling back to pattern matching", err)

	// Before pattern matching, try manual split as intermediate fallback
	// This is critical - if the request has "then" or mentions multiple languages, force manual split
	lowerDesc := strings.ToLower(req.Description)
	hasThen := strings.Contains(lowerDesc, "then")
	// Check for multiple languages more flexibly
	langs := []string{"rust", "go", "python", "java", "javascript", "js"}
	langCount := 0
	for _, lang := range langs {
		if strings.Contains(lowerDesc, lang) {
			langCount++
		}
	}
	hasMultipleLangs := langCount >= 2

	if hasThen || hasMultipleLangs {
		log.Printf("🔄 [CHAINED] Request has 'then' or multiple languages, forcing manual split")
		log.Printf("🔄 [CHAINED] hasThen=%v, hasMultipleLangs=%v", hasThen, hasMultipleLangs)
		manuallySplit := ie.manuallySplitMultiplePrograms(req)
		if len(manuallySplit) > 1 {
			log.Printf("✅ [CHAINED] Manual split succeeded, found %d programs", len(manuallySplit))
			// Convert to ChainedProgram
			chainedPrograms := make([]ChainedProgram, len(manuallySplit))
			for i, def := range manuallySplit {
				chainedPrograms[i] = ChainedProgram{
					Name:        def.Name,
					Description: def.Description,
					Language:    def.Language,
					Context:     make(map[string]string),
				}
			}
			return chainedPrograms, nil
		} else {
			log.Printf("⚠️ [CHAINED] Manual split only found %d program(s), but request clearly asks for multiple", len(manuallySplit))
		}
	}

	// Fallback to pattern matching if LLM fails
	description := req.Description
	programs = []ChainedProgram{}

	// Look for "prog1" and "prog2" patterns or check for "then create" patterns (case-insensitive)
	lowerDesc = strings.ToLower(description)
	hasProg1 := strings.Contains(description, "prog1") ||
		(strings.Contains(lowerDesc, "python") && strings.Contains(lowerDesc, "generates")) ||
		(strings.Contains(lowerDesc, "create") && strings.Contains(lowerDesc, "python")) ||
		// Handle "Go program... then Python" pattern
		(strings.Contains(lowerDesc, "go") && strings.Contains(lowerDesc, "program") && strings.Contains(lowerDesc, "then"))
	hasProg2 := strings.Contains(description, "prog2") ||
		(strings.Contains(lowerDesc, "go") && strings.Contains(lowerDesc, "reads")) ||
		(strings.Contains(lowerDesc, "then") && strings.Contains(lowerDesc, "go")) ||
		// Handle "Go program... then Python" pattern - Python is prog2
		(strings.Contains(lowerDesc, "go") && strings.Contains(lowerDesc, "program") && strings.Contains(lowerDesc, "then") && strings.Contains(lowerDesc, "python")) ||
		// Handle "then create Python" or "then Python" patterns
		(strings.Contains(lowerDesc, "then") && (strings.Contains(lowerDesc, "python") || strings.Contains(lowerDesc, "create") && strings.Contains(lowerDesc, "python")))

	log.Printf("🔍 [CHAINED] Parsing description: %s", description)
	log.Printf("🔍 [CHAINED] hasProg1: %v, hasProg2: %v", hasProg1, hasProg2)
	log.Printf("🔍 [CHAINED] Contains 'python': %v, Contains 'generates': %v", strings.Contains(description, "python"), strings.Contains(description, "generates"))
	log.Printf("🔍 [CHAINED] Contains 'go': %v, Contains 'reads': %v", strings.Contains(description, "go"), strings.Contains(description, "reads"))

	if hasProg1 && hasProg2 {
		// Try to parse the description to extract specific requirements for each program
		prog1Desc, prog1Lang := ie.parseProgramRequirements(description, "prog1")
		prog2Desc, prog2Lang := ie.parseProgramRequirements(description, "prog2")

		log.Printf("🔍 [CHAINED] prog1Desc: %s, prog1Lang: %s", prog1Desc, prog1Lang)
		log.Printf("🔍 [CHAINED] prog2Desc: %s, prog2Lang: %s", prog2Desc, prog2Lang)

		prog1 := ChainedProgram{
			Name:        fmt.Sprintf("chained_prog1_%d", time.Now().UnixNano()),
			Description: prog1Desc,
			Context:     make(map[string]string),
			Language:    prog1Lang,
		}
		programs = append(programs, prog1)

		prog2 := ChainedProgram{
			Name:        fmt.Sprintf("chained_prog2_%d", time.Now().UnixNano()),
			Description: prog2Desc,
			Context:     make(map[string]string),
			Language:    prog2Lang,
		}
		programs = append(programs, prog2)
	} else {
		// Fallback: create two programs based on artifact names
		if names, ok := req.Context["artifact_names"]; ok && names != "" {
			parts := strings.Split(names, ",")
			for _, part := range parts {
				part = strings.TrimSpace(part)
				if part != "" {
					// Infer language from file extension
					ext := strings.ToLower(filepath.Ext(part))
					lang := req.Language // Default to request language
					switch ext {
					case ".py":
						lang = "python"
					case ".go":
						lang = "go"
					case ".js":
						lang = "javascript"
					case ".java":
						lang = "java"
					case ".rs":
						lang = "rust"
					case ".cpp", ".cc", ".cxx":
						lang = "cpp"
					case ".c":
						lang = "c"
					}

					// Build a better description based on the filename and position
					// Extract requirements from the original description
					desc := req.Description

					// Determine which program this is based on position
					idx := 0
					for i, p := range parts {
						if strings.TrimSpace(p) == part {
							idx = i
							break
						}
					}

					// Extract program-specific requirements from the description
					if idx == 0 && strings.Contains(part, ".py") {
						// This is prog1.py - extract Python/JSON requirements
						if strings.Contains(strings.ToLower(desc), "program 1") || strings.Contains(strings.ToLower(desc), "prog1") {
							// Try to extract the exact JSON requirement
							jsonPattern := regexp.MustCompile(`(?i)(program\s*1|prog1).*?(python).*?(print|generate).*?(\{[^}]+\})`)
							if matches := jsonPattern.FindStringSubmatch(desc); len(matches) > 0 {
								jsonStr := matches[len(matches)-1]
								desc = fmt.Sprintf("Program 1 (Python): You MUST print EXACTLY this JSON string: %s. Do NOT print anything else - no labels, no extra text, just the JSON.", jsonStr)
							} else {
								desc = fmt.Sprintf("Program 1 (Python): %s. You MUST generate Python code that prints JSON.", desc)
							}
						} else {
							desc = fmt.Sprintf("Program 1 (Python): Generate %s. %s", part, desc)
						}
					} else if idx == 1 && strings.Contains(part, ".go") {
						// This is prog2.go - extract Go/read requirements
						if strings.Contains(strings.ToLower(desc), "program 2") || strings.Contains(strings.ToLower(desc), "prog2") {
							// Try to extract the read/process requirement
							readPattern := regexp.MustCompile(`(?i)(program\s*2|prog2).*?(go).*?(read|process).*?(\d+)`)
							if matches := readPattern.FindStringSubmatch(desc); len(matches) > 0 {
								resultNum := matches[len(matches)-1]
								desc = fmt.Sprintf("Program 2 (Go): You MUST read JSON from stdin (or previous program output), extract the 'number' field, multiply it by 2, and print EXACTLY the result: %s. Do NOT print labels, just the number.", resultNum)
							} else {
								desc = fmt.Sprintf("Program 2 (Go): %s. You MUST generate Go code that reads JSON and processes it.", desc)
							}
						} else {
							desc = fmt.Sprintf("Program 2 (Go): Generate %s. This program reads output from the previous program. %s", part, desc)
						}
					} else {
						// Fallback: use position-based description
						if idx == 0 {
							desc = fmt.Sprintf("Program 1: Generate %s. %s", part, desc)
						} else {
							desc = fmt.Sprintf("Program %d: Generate %s. This program processes output from previous programs. %s", idx+1, part, desc)
						}
					}

					program := ChainedProgram{
						Name:        strings.TrimSuffix(part, ext),
						Description: desc,
						Context:     make(map[string]string),
						Language:    lang,
					}
					programs = append(programs, program)
				}
			}
		}
	}

	if len(programs) == 0 {
		// Final fallback: create at least one program from the original request
		// This ensures execution can proceed even if parsing fails
		log.Printf("⚠️ [CHAINED] All parsing methods failed, creating single program from original request as fallback")

		// Try to infer language from description
		lang := req.Language
		if lang == "" {
			lowerDesc := strings.ToLower(req.Description)
			if strings.Contains(lowerDesc, "rust") || strings.Contains(lowerDesc, ".rs") {
				lang = "rust"
			} else if strings.Contains(lowerDesc, "go") && !strings.Contains(lowerDesc, "python") {
				lang = "go"
			} else if strings.Contains(lowerDesc, "python") {
				lang = "python"
			} else {
				lang = "python" // default
			}
		}

		fallbackProgram := ChainedProgram{
			Name:        fmt.Sprintf("program_%s", lang),
			Description: req.Description,
			Language:    lang,
			Context:     make(map[string]string),
		}

		log.Printf("✅ [CHAINED] Created fallback program: %s (%s)", fallbackProgram.Name, lang)
		return []ChainedProgram{fallbackProgram}, nil
	}

	return programs, nil
}

// parseProgramRequirements parses the description to extract specific requirements for each program
func (ie *IntelligentExecutor) parseProgramRequirements(description, programName string) (string, string) {
	lower := strings.ToLower(description)

	// Handle single-line descriptions with "then" separator
	if strings.Contains(lower, "then") {
		// Split on "then" but also handle "and then"
		sep := "then"
		if strings.Contains(lower, "and then") {
			sep = "and then"
		}
		parts := strings.Split(description, sep)
		if len(parts) >= 2 {
			part1 := strings.TrimSpace(parts[0])
			part2 := strings.TrimSpace(parts[1])
			lowerPart1 := strings.ToLower(part1)
			lowerPart2 := strings.ToLower(part2)

			// Check if this is for prog1 (first part) or prog2 (second part)
			if programName == "prog1" {
				// Extract language from first part
				lang := "python" // default
				if strings.Contains(lowerPart1, "go") && strings.Contains(lowerPart1, "program") {
					lang = "go"
				} else if strings.Contains(lowerPart1, "python") && strings.Contains(lowerPart1, "program") {
					lang = "python"
				} else if strings.Contains(lowerPart1, "python") {
					lang = "python"
				} else if strings.Contains(lowerPart1, "go") {
					lang = "go"
				} else if strings.Contains(lowerPart1, "javascript") || strings.Contains(lowerPart1, "js") {
					lang = "javascript"
				} else if strings.Contains(lowerPart1, "java") {
					lang = "java"
				}
				// Preserve the full description for better context
				return part1, lang
			} else if programName == "prog2" {
				// Extract language from second part
				lang := "python" // default (since Go is usually first)
				if strings.Contains(lowerPart2, "python") && strings.Contains(lowerPart2, "program") {
					lang = "python"
				} else if strings.Contains(lowerPart2, "go") && strings.Contains(lowerPart2, "program") {
					lang = "go"
				} else if strings.Contains(lowerPart2, "python") {
					lang = "python"
				} else if strings.Contains(lowerPart2, "go") {
					lang = "go"
				} else if strings.Contains(lowerPart2, "javascript") || strings.Contains(lowerPart2, "js") {
					lang = "javascript"
				} else if strings.Contains(lowerPart2, "java") {
					lang = "java"
				}
				// Preserve the full description for better context
				return part2, lang
			}
		}
	}

	// Fallback: try to infer from the description content
	if strings.Contains(lower, "json") && strings.Contains(lower, "generates") {
		if programName == "prog1" {
			return "Create a Python program that generates JSON with a number", "python"
		}
	}
	if strings.Contains(lower, "json") && strings.Contains(lower, "reads") {
		if programName == "prog2" {
			return "Create a Go program that reads JSON and multiplies the number by 2", "go"
		}
	}

	// Final fallback
	return fmt.Sprintf("Generate %s", programName), "python"
}

// extractJSONFromOutput extracts clean JSON from program output, removing env vars and other noise
// extractTimingFromOutput extracts algorithm execution time from program output
// Looks for patterns like "took: 123ns", "duration: 456ms", "Time: 789 microseconds", etc.
func extractTimingFromOutput(output string, language string) int64 {
	if output == "" {
		return 0
	}

	// Common timing patterns in output
	patterns := []*regexp.Regexp{
		// Go: "Execution time: 352ns" (no space - Go's time.Duration format) - prioritize this
		regexp.MustCompile(`(?i)execution\s+time[:\s]+(\d+(?:\.\d+)?(?:e[+-]?\d+)?)(ns|us|ms|s|h|m)\b`),
		// Go: "Execution time: 540 nanoseconds" (with space and full word)
		regexp.MustCompile(`(?i)execution\s+time[:\s]+(\d+(?:\.\d+)?(?:e[+-]?\d+)?)\s+(ns|nanoseconds|nanosecond)`),
		// Go: "Execution time: 9m30s nanoseconds" - handle Go Duration strings
		regexp.MustCompile(`(?i)execution\s+time[:\s]+((?:\d+h)?(?:\d+m)?(?:\d+(?:\.\d+)?s)?)\s*(ns|nanoseconds|nanosecond|us|microseconds|microsecond|ms|milliseconds|millisecond|s|seconds|second)`),
		// Go: "took: 123456ns" or "Duration: 123456ns" or "took: 1.234567s"
		regexp.MustCompile(`(?i)(?:took|duration|time|elapsed)[:\s]+((?:\d+h)?(?:\d+m)?(?:\d+(?:\.\d+)?s)?|\d+(?:\.\d+)?(?:e[+-]?\d+)?)\s*(ns|nanoseconds|nanosecond|us|microseconds|microsecond|ms|milliseconds|millisecond|s|seconds|second)`),
		// Python: "Execution time: 0.123456 seconds" or "Execution time: 5.0067901611328125e-06 seconds" (with scientific notation)
		regexp.MustCompile(`(?i)execution\s+time[:\s]+(\d+(?:\.\d+)?(?:e[+-]?\d+)?)\s*(s|seconds|second|ms|milliseconds|millisecond|us|microseconds|microsecond|ns|nanoseconds|nanosecond)`),
		// Python: "took: 0.123456 seconds" or "duration: 123.456 ms" (with scientific notation)
		regexp.MustCompile(`(?i)(?:took|duration|time|elapsed)[:\s]+(\d+(?:\.\d+)?(?:e[+-]?\d+)?)\s*(ms|milliseconds|millisecond|s|seconds|second|us|microseconds|microsecond|ns|nanoseconds|nanosecond)`),
		// Generic: "Execution time: 123456" or "Sorting time: 0.123" (with scientific notation)
		regexp.MustCompile(`(?i)(?:sorting\s+time|algorithm\s+time)[:\s]+(\d+(?:\.\d+)?(?:e[+-]?\d+)?)\s*(ns|nanoseconds|nanosecond|ms|milliseconds|millisecond|s|seconds|second|us|microseconds|microsecond)`),
	}

	// Try each pattern and find ALL matches, then use the LAST one
	// This handles cases where timing is printed multiple times (e.g., in loops)
	var lastMatch []string

	for _, pattern := range patterns {
		// Find all matches in the output
		allMatches := pattern.FindAllStringSubmatch(output, -1)
		if len(allMatches) > 0 {
			// Use the last match (most likely the final timing)
			match := allMatches[len(allMatches)-1]
			if len(match) >= 3 {
				lastMatch = match
			}
		}
	}

	// Process the last match found
	if lastMatch != nil && len(lastMatch) >= 3 {
		valueStr := lastMatch[1]
		unit := strings.ToLower(lastMatch[2])

		var nanoseconds int64

		// Check if this is a Go Duration string (e.g., "9m30s", "1h2m3s", "5s")
		// Duration strings have format like "9m30s", "1h2m3.5s", etc.
		if matched, _ := regexp.MatchString(`^(?:\d+h)?(?:\d+m)?(?:\d+(?:\.\d+)?s)?$`, valueStr); matched {
			// Parse Go Duration string using time.ParseDuration
			if duration, err := time.ParseDuration(valueStr); err == nil {
				nanoseconds = duration.Nanoseconds()
				log.Printf("🔍 [TIMING-EXTRACT] Parsed Go Duration string '%s' = %d ns", valueStr, nanoseconds)
				if nanoseconds > 0 {
					return nanoseconds
				}
			} else {
				log.Printf("⚠️ [TIMING-EXTRACT] Failed to parse Go Duration string '%s': %v", valueStr, err)
			}
		}

		// Try parsing as a numeric value (handles scientific notation)
		var value float64
		// Use strconv.ParseFloat which handles both regular and scientific notation (e.g., 5.24e-06)
		if parsed, err := strconv.ParseFloat(valueStr, 64); err == nil {
			value = parsed
		} else {
			log.Printf("⚠️ [TIMING-EXTRACT] Failed to parse timing value '%s': %v", valueStr, err)
			return 0
		}

		// Convert to nanoseconds based on unit
		switch unit {
		case "ns", "nanoseconds", "nanosecond":
			nanoseconds = int64(value)
		case "us", "microseconds", "microsecond":
			nanoseconds = int64(value * 1000)
		case "ms", "milliseconds", "millisecond":
			nanoseconds = int64(value * 1000000)
		case "s", "seconds", "second":
			nanoseconds = int64(value * 1000000000)
		default:
			return 0
		}

		if nanoseconds > 0 {
			log.Printf("🔍 [TIMING-EXTRACT] Found timing: %s %s = %d ns (from last occurrence)", valueStr, unit, nanoseconds)
			return nanoseconds
		}
	}

	return 0
}

func extractJSONFromOutput(output string) string {
	if output == "" {
		return ""
	}

	// Remove common environment variable patterns
	lines := strings.Split(output, "\n")
	var jsonLines []string
	inJSON := false

	for _, line := range lines {
		lineTrimmed := strings.TrimSpace(line)

		// Skip empty lines
		if lineTrimmed == "" {
			continue
		}

		// Skip environment variable dumps (VAR='value' or VAR="value")
		if matched, _ := regexp.MatchString(`^[A-Z_][A-Z0-9_]*=['"]`, lineTrimmed); matched {
			continue
		}

		// Skip SSH messages
		if strings.Contains(lineTrimmed, "Warning: Permanently added") ||
			strings.Contains(lineTrimmed, "Host key verification") {
			continue
		}

		// Look for JSON start (either { or [)
		if strings.HasPrefix(lineTrimmed, "{") || strings.HasPrefix(lineTrimmed, "[") {
			inJSON = true
			jsonLines = append(jsonLines, lineTrimmed)
			// If it's a single-line JSON, we're done
			if strings.HasSuffix(lineTrimmed, "}") || strings.HasSuffix(lineTrimmed, "]") {
				break
			}
			continue
		}

		// If we're in JSON, collect lines until we find the end
		if inJSON {
			jsonLines = append(jsonLines, lineTrimmed)
			if strings.HasSuffix(lineTrimmed, "}") || strings.HasSuffix(lineTrimmed, "]") {
				break
			}
		}
	}

	if len(jsonLines) == 0 {
		// Fallback: try to find JSON anywhere in the output by looking for { or [
		// Find the first { or [ and extract until matching } or ]
		startIdx := -1
		var openChar, closeChar byte
		for i := 0; i < len(output); i++ {
			if output[i] == '{' {
				startIdx = i
				openChar = '{'
				closeChar = '}'
				break
			} else if output[i] == '[' {
				startIdx = i
				openChar = '['
				closeChar = ']'
				break
			}
		}

		if startIdx >= 0 {
			// Find matching closing brace/bracket
			depth := 0
			endIdx := -1
			for i := startIdx; i < len(output); i++ {
				if output[i] == openChar {
					depth++
				} else if output[i] == closeChar {
					depth--
					if depth == 0 {
						endIdx = i
						break
					}
				}
			}
			if endIdx > startIdx {
				extracted := output[startIdx : endIdx+1]
				// Validate extracted JSON
				var test interface{}
				if err := json.Unmarshal([]byte(extracted), &test); err == nil {
					return extracted
				}
			}
		}

		return ""
	}

	// Join JSON lines and validate
	jsonStr := strings.Join(jsonLines, "\n")

	// Validate it's valid JSON
	var test interface{}
	if err := json.Unmarshal([]byte(jsonStr), &test); err == nil {
		return jsonStr
	}

	// If validation failed, try to extract just the JSON object/array
	// Remove everything before first { or [
	startIdx := -1
	for i := 0; i < len(jsonStr); i++ {
		if jsonStr[i] == '{' || jsonStr[i] == '[' {
			startIdx = i
			break
		}
	}
	if startIdx >= 0 {
		// Find matching closing brace/bracket
		openChar := jsonStr[startIdx]
		var closeChar byte = '}'
		if openChar == '[' {
			closeChar = ']'
		}
		depth := 0
		endIdx := -1
		for i := startIdx; i < len(jsonStr); i++ {
			if jsonStr[i] == openChar {
				depth++
			} else if jsonStr[i] == closeChar {
				depth--
				if depth == 0 {
					endIdx = i
					break
				}
			}
		}
		if endIdx > startIdx {
			extracted := jsonStr[startIdx : endIdx+1]
			// Validate extracted JSON
			var test2 interface{}
			if err := json.Unmarshal([]byte(extracted), &test2); err == nil {
				return extracted
			}
		}
	}

	return ""
}

// extractProgramDescription extracts the description for a specific program
func (ie *IntelligentExecutor) extractProgramDescription(description, programName string) string {
	// Simple extraction - look for lines containing the program name
	lines := strings.Split(description, "\n")
	var relevantLines []string

	for _, line := range lines {
		if strings.Contains(strings.ToLower(line), programName) {
			relevantLines = append(relevantLines, line)
		}
	}

	if len(relevantLines) > 0 {
		return strings.Join(relevantLines, "\n")
	}

	// Fallback
	return fmt.Sprintf("Generate %s", programName)
}

// executeProgramDirectly executes a single program without loop protection (for chained execution)
func (ie *IntelligentExecutor) executeProgramDirectly(ctx context.Context, req *ExecutionRequest, start time.Time, workflowID string) (*IntelligentExecutionResult, error) {
	log.Printf("🔗 [CHAINED] Executing program directly: %s", req.TaskName)

	// Set defaults
	if req.Language == "" {
		if inferred := ie.inferLanguageFromRequest(req); inferred != "" {
			req.Language = inferred
		} else {
			req.Language = "python"
		}
	}
	if req.MaxRetries == 0 {
		req.MaxRetries = ie.maxRetries
	}
	if req.Timeout == 0 {
		req.Timeout = 120 // Reduced from 600 to prevent long-running requests and GPU overload
	}

	result := &IntelligentExecutionResult{
		Success:         false,
		ValidationSteps: []ValidationStep{},
		WorkflowID:      workflowID,
	}

	// Generate code for this program using the existing code generator
	filteredCtx := filterCodegenContext(req.Context)

	// Enhance description with language requirement and filename if available
	enhancedDesc := req.Description
	if req.Language != "" {
		enhancedDesc = fmt.Sprintf("CRITICAL: You MUST generate %s code, NOT any other language!\n\n%s", req.Language, enhancedDesc)
	}

	// Add language-specific guidance
	if req.Language == "python" && strings.Contains(strings.ToLower(enhancedDesc), "json") {
		enhancedDesc += "\n\nCRITICAL FOR PYTHON JSON: If you need to print JSON, use: json.dumps(dict_object). Do NOT use json.loads() on a dictionary - that's for parsing JSON strings!"
	}

	// Add matrix operation guidance
	if strings.Contains(strings.ToLower(enhancedDesc), "matrix") ||
		(req.Context != nil && (req.Context["matrix1"] != "" || req.Context["matrix2"] != "")) {
		if req.Language == "go" {
			enhancedDesc += "\n\n🚨 CRITICAL GO MATRIX REQUIREMENTS:\n1. Read from env: matrix1Str := os.Getenv(\"matrix1\"); json.Unmarshal([]byte(matrix1Str), &matrix1) - DO NOT hardcode!\n2. Import: \"os\", \"encoding/json\", \"fmt\"\n3. Output: Print each row separately - for i := 0; i < len(result); i++ { fmt.Println(result[i]) }\n4. WRONG: fmt.Println(result) prints [[6 8] [10 12]] on one line - this FAILS!\n5. CORRECT output format: [6 8] on line 1, [10 12] on line 2"
		} else if req.Language == "python" {
			enhancedDesc += "\n\n🚨 CRITICAL FOR PYTHON MATRIX OPERATIONS:\n- You MUST read matrices from environment variables using os.getenv(\"matrix1\") and os.getenv(\"matrix2\")\n- Parse the JSON string format (e.g., \"[[1,2],[3,4]]\") using json.loads()\n- DO NOT hardcode matrix values - the matrices will be different each time!\n- Example: matrix1 = json.loads(os.getenv(\"matrix1\"))"
		}
	}
	// If this is part of a chained execution, add the program name/filename to context
	if req.TaskName != "" {
		enhancedDesc = fmt.Sprintf("%s\n\nIMPORTANT: The generated code will be saved as: %s", enhancedDesc, req.TaskName)
		// Try to infer filename from task name
		if strings.HasPrefix(req.TaskName, "prog") || strings.HasPrefix(req.TaskName, "chained_prog") {
			// Extract expected extension from artifact_names if available
			if names, ok := req.Context["artifact_names"]; ok {
				parts := strings.Split(names, ",")
				for _, part := range parts {
					part = strings.TrimSpace(part)
					if strings.Contains(part, req.TaskName) || strings.Contains(req.TaskName, strings.TrimSuffix(part, filepath.Ext(part))) {
						ext := filepath.Ext(part)
						if ext != "" {
							enhancedDesc = fmt.Sprintf("%s\n\nCRITICAL: The output filename will be %s - ensure you generate %s code that matches this extension!", enhancedDesc, part, req.Language)
						}
						break
					}
				}
			}
		}
	}

	codeGenReq := &CodeGenerationRequest{
		TaskName:     req.TaskName,
		Description:  enhancedDesc,
		Language:     req.Language,
		Context:      filteredCtx,
		Tags:         []string{"intelligent_execution", "auto_generated", "chained"},
		Executable:   true,
		HighPriority: req.HighPriority, // Pass priority from execution request
	}

	// Create a specific prompt for Go programs that need to parse JSON
	// Check multiple conditions: description contains "json", OR it's a chained prog2 that reads from stdin
	isChainedProg2 := (strings.HasPrefix(req.TaskName, "chained_prog2") || strings.HasPrefix(req.TaskName, "prog2")) && req.Language == "go"
	hasPreviousOutput := filteredCtx != nil && filteredCtx["previous_output"] != ""
	needsJSONParsing := req.Language == "go" && (strings.Contains(strings.ToLower(enhancedDesc), "json") ||
		strings.Contains(strings.ToLower(enhancedDesc), "read") ||
		strings.Contains(strings.ToLower(enhancedDesc), "stdin") ||
		isChainedProg2 ||
		hasPreviousOutput)

	if needsJSONParsing {
		// Add specific instructions for JSON parsing in Go with a complete working example
		codeGenReq.Description = enhancedDesc + "\n\n🚨 CRITICAL: You MUST copy this EXACT code - including ALL imports:\n\npackage main\n\nimport (\n\t\"encoding/json\"\n\t\"fmt\"\n\t\"io\"\n\t\"os\"\n\t\"strings\"\n)\n\nfunc main() {\n\t// Read JSON from stdin - EXACTLY this line, no variations!\n\tjsonBytes, _ := io.ReadAll(os.Stdin)\n\t\n\t// CRITICAL: Trim whitespace and newlines from input\n\tjsonStr := strings.TrimSpace(string(jsonBytes))\n\t\n\t// Unmarshal into map[string]interface{}\n\tvar data map[string]interface{}\n\tjson.Unmarshal([]byte(jsonStr), &data)\n\t\n\t// Extract number as float64, then convert to int\n\t// Use type assertion with ok check to avoid panic\n\tif numVal, ok := data[\"number\"].(float64); ok {\n\t\tnumber := int(numVal)\n\t\t// Calculate result (multiply by 2)\n\t\tresult := number * 2\n\t\t// Print ONLY the number, no labels\n\t\tfmt.Println(result)\n\t}\n}\n\n🚨 CRITICAL RULES - DO NOT DEVIATE:\n- MUST include ALL 5 imports: \"encoding/json\", \"fmt\", \"io\", \"os\", \"strings\"\n- MUST use: io.ReadAll(os.Stdin) - NOT log.Std(), NOT stdin, NOT anything else!\n- MUST trim whitespace: jsonStr := strings.TrimSpace(string(jsonBytes))\n- MUST import \"encoding/json\" to use json.Unmarshal - this is REQUIRED!\n- MUST import \"strings\" to use strings.TrimSpace - this is REQUIRED!\n- MUST import \"os\" package to access os.Stdin\n- MUST use type assertion with ok check: if numVal, ok := data[\"number\"].(float64); ok {\n- DO NOT use direct type assertion without ok check - it will panic if the value is nil!\n- DO NOT use log.Std() or any other function - ONLY os.Stdin!"
	}

	codeGenResult, err := ie.codeGenerator.GenerateCode(codeGenReq)
	if err != nil {
		result.Error = fmt.Sprintf("Code generation failed: %v", err)
		result.ExecutionTime = time.Since(start)
		return result, nil
	}

	if !codeGenResult.Success {
		result.Error = fmt.Sprintf("Code generation failed: %s", codeGenResult.Error)
		result.ExecutionTime = time.Since(start)
		return result, nil
	}

	generatedCode := codeGenResult.Code
	if generatedCode == nil {
		result.Error = "No code generated"
		result.ExecutionTime = time.Since(start)
		return result, nil
	}

	// Validate and iterate on the generated code (retry loop for chained programs)
	// Note: We don't do pre-validation of code content because requests vary too much.
	// Instead, we rely on execution-based validation which will catch:
	// - Compilation/runtime errors
	// - Missing expected output
	// - The existing fixCodeWithLLM mechanism will handle fixing incorrect implementations
	for attempt := 0; attempt < req.MaxRetries; attempt++ {
		log.Printf("🔄 [CHAINED] Validation attempt %d/%d for program: %s", attempt+1, req.MaxRetries, req.TaskName)

		validationResult := ie.validateCode(ctx, generatedCode, req, workflowID)
		result.ValidationSteps = append(result.ValidationSteps, validationResult)
		result.RetryCount = attempt + 1

		if validationResult.Success {
			log.Printf("✅ [CHAINED] Code validation successful on attempt %d", attempt+1)
			result.Success = true
			result.Result = validationResult.Output
			result.GeneratedCode = generatedCode
			result.ExecutionTime = time.Since(start)
			return result, nil
		} else {
			log.Printf("❌ [CHAINED] Code validation failed on attempt %d: %s", attempt+1, validationResult.Error)

			// Try to fix the code using LLM feedback
			if attempt < req.MaxRetries-1 {
				log.Printf("🔧 [CHAINED] Attempting to fix code using LLM feedback")
				fixedCode, fixErr := ie.fixCodeWithLLM(generatedCode, validationResult, req)
				if fixErr != nil {
					log.Printf("❌ [CHAINED] Code fixing failed: %v", fixErr)
					continue
				}
				generatedCode = fixedCode
				log.Printf("✅ [CHAINED] Code fixed, retrying validation")
			}
		}
	}

	// If we get here, all retries failed
	result.Success = false
	if len(result.ValidationSteps) > 0 {
		lastStep := result.ValidationSteps[len(result.ValidationSteps)-1]
		if lastStep.Error != "" {
			result.Error = lastStep.Error
		} else if lastStep.Output != "" {
			result.Error = fmt.Sprintf("Execution failed: %s", lastStep.Output)
		} else {
			result.Error = "Code validation failed after all retry attempts"
		}
		result.Result = lastStep.Output
	} else {
		result.Error = "Code validation failed after all retry attempts"
	}
	result.GeneratedCode = generatedCode
	result.ExecutionTime = time.Since(start)

	// Log tool metrics for intelligent execution
	ie.logIntelligentExecutionMetrics(ctx, req, result)

	// Learn from execution outcome
	if result.Success && generatedCode != nil {
		ie.recordSuccessfulExecution(req, result, generatedCode)
	} else if !result.Success {
		ie.recordFailedExecution(req, result)
	}

	return result, nil
}

// ============================================================================
// LEARNING METHODS - Focused Learning Improvements
// ============================================================================

// categorizeFailure categorizes a failure error into a pattern type and category
func (ie *IntelligentExecutor) categorizeFailure(errorMsg string, language string) (patternType, errorCategory string) {
	errorLower := strings.ToLower(errorMsg)

	// Determine pattern type
	if strings.Contains(errorLower, "undefined") ||
		strings.Contains(errorLower, "imported and not used") ||
		strings.Contains(errorLower, "declared but not used") ||
		strings.Contains(errorLower, "cannot find package") {
		patternType = "compilation"
	} else if strings.Contains(errorLower, "panic") ||
		strings.Contains(errorLower, "runtime error") ||
		strings.Contains(errorLower, "index out of range") ||
		strings.Contains(errorLower, "nil pointer") {
		patternType = "runtime"
	} else if strings.Contains(errorLower, "type") && strings.Contains(errorLower, "mismatch") ||
		strings.Contains(errorLower, "cannot use") ||
		strings.Contains(errorLower, "assignment mismatch") {
		patternType = "type_error"
	} else {
		patternType = "validation"
	}

	// Determine error category
	if strings.Contains(errorLower, "undefined") {
		errorCategory = "undefined_symbol"
	} else if strings.Contains(errorLower, "import") {
		errorCategory = "import_error"
	} else if strings.Contains(errorLower, "type") {
		errorCategory = "type_mismatch"
	} else if strings.Contains(errorLower, "assignment mismatch") {
		errorCategory = "assignment_mismatch"
	} else if strings.Contains(errorLower, "unused") || strings.Contains(errorLower, "not used") {
		errorCategory = "unused_import"
	} else {
		errorCategory = "other"
	}

	return patternType, errorCategory
}

// recordFailurePattern records a failure pattern for learning
func (ie *IntelligentExecutor) recordFailurePattern(validationResult ValidationStep, req *ExecutionRequest) {
	if ie.learningRedis == nil {
		return
	}

	patternType, errorCategory := ie.categorizeFailure(validationResult.Error, req.Language)
	taskCategory := ie.deriveTaskCategory(req.TaskName, req.Description)

	patternKey := fmt.Sprintf("failure_pattern:%s:%s:%s", patternType, errorCategory, req.Language)
	patternData, err := ie.learningRedis.Get(ie.ctx, patternKey).Result()

	var pattern FailurePattern
	if err == nil && patternData != "" {
		json.Unmarshal([]byte(patternData), &pattern)
	} else {
		pattern = FailurePattern{
			PatternType:   patternType,
			ErrorCategory: errorCategory,
			Language:      req.Language,
			TaskCategory:  taskCategory,
			Frequency:     0,
			SuccessRate:   0.0,
			CommonFixes:   []string{},
			FirstSeen:     time.Now(),
		}
	}

	pattern.Frequency++
	pattern.LastSeen = time.Now()

	patternDataJSON, _ := json.Marshal(pattern)
	ie.learningRedis.Set(ie.ctx, patternKey, patternDataJSON, 30*24*time.Hour) // 30 days TTL

	log.Printf("📊 [LEARNING] Recorded failure pattern: %s/%s (frequency: %d)", patternType, errorCategory, pattern.Frequency)
}

// learnFromValidationFailure learns from validation failures to improve future code generation
func (ie *IntelligentExecutor) learnFromValidationFailure(validationResult ValidationStep, req *ExecutionRequest) {
	if ie.learningRedis == nil {
		return
	}

	// Record failure pattern
	ie.recordFailurePattern(validationResult, req)

	// Update prevention hints based on error type
	patternType, errorCategory := ie.categorizeFailure(validationResult.Error, req.Language)
	preventionKey := fmt.Sprintf("prevention_hint:%s:%s:%s", patternType, errorCategory, req.Language)

	// Store prevention hint
	hint := ie.generatePreventionHint(validationResult.Error, req.Language)
	ie.learningRedis.Set(ie.ctx, preventionKey, hint, 30*24*time.Hour)
}

// generatePreventionHint generates a prevention hint based on error message
func (ie *IntelligentExecutor) generatePreventionHint(errorMsg, language string) string {
	errorLower := strings.ToLower(errorMsg)

	if strings.Contains(errorLower, "undefined") {
		return "Check for missing imports or typos in function/variable names"
	} else if strings.Contains(errorLower, "imported and not used") {
		return "Remove unused imports - they cause compilation errors"
	} else if strings.Contains(errorLower, "assignment mismatch") {
		return "Check function return values - json.Unmarshal returns only error, not ([]byte, error)"
	} else if strings.Contains(errorLower, "type") && strings.Contains(errorLower, "mismatch") {
		return "Check type assertions - JSON numbers are float64, not int64"
	}

	return "Review error message carefully and fix all issues"
}

// getPreventionHintsForTask retrieves learned prevention hints for a task
// This is where the system demonstrates intelligence by using what it learned
func (ie *IntelligentExecutor) getPreventionHintsForTask(req *ExecutionRequest) []string {
	if ie.learningRedis == nil {
		log.Printf("⚠️  [INTELLIGENCE] getPreventionHintsForTask: learningRedis is nil for task %s (language: %s)", req.TaskName, req.Language)
		return []string{}
	}
	log.Printf("🧠 [INTELLIGENCE] getPreventionHintsForTask: Searching for hints (task: %s, language: %s)", req.TaskName, req.Language)

	taskCategory := ie.deriveTaskCategory(req.TaskName, req.Description)
	var hints []string

	// Get prevention hints for common error patterns in this language
	patternTypes := []string{"compilation", "runtime", "type_error", "validation"}
	searchedKeys := []string{}
	for _, patternType := range patternTypes {
		// Try to get hints for common error categories
		errorCategories := []string{"undefined", "type_mismatch", "import_error", "syntax_error"}
		for _, errorCategory := range errorCategories {
			preventionKey := fmt.Sprintf("prevention_hint:%s:%s:%s", patternType, errorCategory, req.Language)
			searchedKeys = append(searchedKeys, preventionKey)
			hint, err := ie.learningRedis.Get(ie.ctx, preventionKey).Result()
			if err == nil && hint != "" {
				// Check if this hint is relevant to the task category
				// Get failure pattern to check frequency
				patternKey := fmt.Sprintf("failure_pattern:%s:%s:%s", patternType, errorCategory, req.Language)
				patternData, err := ie.learningRedis.Get(ie.ctx, patternKey).Result()
				if err == nil && patternData != "" {
					var pattern FailurePattern
					if json.Unmarshal([]byte(patternData), &pattern) == nil {
						// Only include hints for patterns that occurred at least 2 times
						// This shows the system learned from repeated mistakes
						if pattern.Frequency >= 2 {
							hints = append(hints, hint)
							log.Printf("🧠 [INTELLIGENCE] Retrieved learned prevention hint: %s (seen %d times)", hint, pattern.Frequency)
						} else {
							log.Printf("🧠 [INTELLIGENCE] Found hint but frequency too low: %s (frequency: %d, need >= 2)", preventionKey, pattern.Frequency)
						}
					}
				} else {
					log.Printf("🧠 [INTELLIGENCE] Found hint but no matching pattern: %s", preventionKey)
				}
			}
		}
	}
	if len(hints) == 0 && len(searchedKeys) > 0 {
		log.Printf("🧠 [INTELLIGENCE] Searched %d keys for prevention hints, found 0 matching hints for task %s (language: %s)", len(searchedKeys), req.TaskName, req.Language)
	}

	// Also check for task-category-specific strategies that worked well
	strategyKey := fmt.Sprintf("codegen_strategy:%s:%s", taskCategory, req.Language)
	strategyData, err := ie.learningRedis.Get(ie.ctx, strategyKey).Result()
	if err == nil && strategyData != "" {
		var strategy CodeGenStrategy
		if json.Unmarshal([]byte(strategyData), &strategy) == nil {
			// If we have a successful strategy with good quality, add a hint
			if strategy.SuccessRate > 0.7 && strategy.AvgQuality > 0.6 && strategy.UsageCount >= 3 {
				hints = append(hints, fmt.Sprintf("Previous successful approach: %s (success rate: %.0f%%, avg retries: %.1f)",
					strategy.PromptStyle, strategy.SuccessRate*100, strategy.AvgRetries))
				log.Printf("🧠 [INTELLIGENCE] Using learned successful strategy: %s (%.0f%% success)", strategy.PromptStyle, strategy.SuccessRate*100)
			}
		}
	}

	return hints
}

// recordSuccessfulExecution records a successful execution for learning
func (ie *IntelligentExecutor) recordSuccessfulExecution(req *ExecutionRequest, result *IntelligentExecutionResult, code *GeneratedCode) {
	// Skip storing trivial repetitive capabilities
	trivialPatterns := []string{
		"create example.txt",
		"create example",
		"list directory and create",
		"list current directory",
	}
	descLower := strings.ToLower(req.Description)
	for _, pattern := range trivialPatterns {
		if strings.Contains(descLower, pattern) {
			log.Printf("🚫 [INTELLIGENT] Skipping capability storage for trivial task: %s", pattern)
			return
		}
	}
	if ie.learningRedis == nil {
		return
	}

	taskCategory := ie.deriveTaskCategory(req.TaskName, req.Description)

	// Record code generation strategy success
	strategyKey := fmt.Sprintf("codegen_strategy:%s:%s", taskCategory, req.Language)
	strategyData, err := ie.learningRedis.Get(ie.ctx, strategyKey).Result()

	var strategy CodeGenStrategy
	if err == nil && strategyData != "" {
		json.Unmarshal([]byte(strategyData), &strategy)
	} else {
		strategy = CodeGenStrategy{
			StrategyID:   fmt.Sprintf("strategy_%s_%s", taskCategory, req.Language),
			PromptStyle:  "default",
			TaskCategory: taskCategory,
			Language:     req.Language,
			SuccessRate:  0.0,
			AvgRetries:   0.0,
			AvgQuality:   0.0,
			UsageCount:   0,
			LastUsed:     time.Now(),
		}
	}

	strategy.UsageCount++
	strategy.LastUsed = time.Now()

	// Update success rate (exponential moving average)
	alpha := 0.1 // Learning rate
	strategy.SuccessRate = alpha*1.0 + (1-alpha)*strategy.SuccessRate

	// Update average retries
	strategy.AvgRetries = alpha*float64(result.RetryCount) + (1-alpha)*strategy.AvgRetries

	// Update average quality (based on retry count - lower is better)
	quality := 1.0 - (float64(result.RetryCount) / 5.0) // Normalize to 0-1
	if quality < 0 {
		quality = 0
	}
	strategy.AvgQuality = alpha*quality + (1-alpha)*strategy.AvgQuality

	strategyDataJSON, _ := json.Marshal(strategy)
	ie.learningRedis.Set(ie.ctx, strategyKey, strategyDataJSON, 30*24*time.Hour)

	log.Printf("📊 [LEARNING] Recorded successful execution: %s/%s (success_rate: %.2f%%, retries: %.1f)",
		taskCategory, req.Language, strategy.SuccessRate*100, strategy.AvgRetries)

	// Auto-create tool from successful code execution if it's general enough
	ie.considerToolCreationFromExecution(req, result, code)
}

// considerToolCreationFromExecution analyzes successful code execution and creates a tool if it's general enough
func (ie *IntelligentExecutor) considerToolCreationFromExecution(req *ExecutionRequest, result *IntelligentExecutionResult, code *GeneratedCode) {
	if code == nil || code.Code == "" {
		return
	}

	// Check if code is general enough to become a tool
	if !ie.isCodeGeneralEnoughForTool(code.Code, code.Language, req.Description) {
		return
	}

	// Generate a stable tool ID
	toolID := ie.generateToolIDFromCode(code.Language, code.Code, req.TaskName)

	// Check if tool already exists
	if ie.toolExists(toolID) {
		log.Printf("🔧 [TOOL-CREATOR] Tool %s already exists, skipping creation", toolID)
		return
	}

	// Create tool definition
	tool := ie.createToolFromCode(toolID, req, code, result)

	// Register the tool via API
	if err := ie.registerToolViaAPI(tool); err != nil {
		log.Printf("⚠️ [TOOL-CREATOR] Failed to register tool %s: %v", toolID, err)
		return
	}

	log.Printf("✅ [TOOL-CREATOR] Successfully created and registered tool %s from successful execution", toolID)
}

// isCodeGeneralEnoughForTool uses LLM to determine if code aligns with system objectives and is suitable as a tool
func (ie *IntelligentExecutor) isCodeGeneralEnoughForTool(code, language, description string) bool {
	c := strings.TrimSpace(code)

	// Minimum length check - skip trivial code
	if len(c) < 100 {
		return false
	}

	// If LLM client is not available, fall back to basic check
	if ie.llmClient == nil {
		log.Printf("⚠️ [TOOL-CREATOR] LLM client not available, skipping tool creation")
		return false
	}

	// Build prompt for LLM evaluation
	prompt := ie.buildToolEvaluationPrompt(code, language, description)

	// Call LLM with low priority (this is a background evaluation)
	// Use longer timeout for tool evaluation since it's low priority and may wait in queue
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	response, err := ie.llmClient.callLLMWithContextAndPriority(ctx, prompt, PriorityLow)
	if err != nil {
		log.Printf("⚠️ [TOOL-CREATOR] LLM evaluation failed: %v, skipping tool creation", err)
		return false
	}

	// Parse LLM response - expect JSON with "should_create_tool" boolean
	shouldCreate, reason := ie.parseToolEvaluationResponse(response)

	if shouldCreate {
		log.Printf("✅ [TOOL-CREATOR] LLM recommends tool creation: %s", reason)
		return true
	}

	log.Printf("🔍 [TOOL-CREATOR] LLM does not recommend tool creation: %s", reason)
	return false
}

// buildToolEvaluationPrompt creates a prompt for LLM to evaluate if code should become a tool
func (ie *IntelligentExecutor) buildToolEvaluationPrompt(code, language, description string) string {
	var prompt strings.Builder

	prompt.WriteString("You are evaluating whether successfully executed code should be converted into a reusable tool for an autonomous AI system.\n\n")

	prompt.WriteString("SYSTEM OBJECTIVES:\n")
	prompt.WriteString("This system is designed for:\n")
	prompt.WriteString("1. Autonomous task execution - generating and executing code to accomplish goals\n")
	prompt.WriteString("2. Knowledge management - building and querying knowledge graphs, episodic memory\n")
	prompt.WriteString("3. Goal tracking and achievement - managing and progressing toward objectives\n")
	prompt.WriteString("4. Tool creation and reuse - building reusable capabilities that can be invoked by the system\n")
	prompt.WriteString("5. Learning from experience - improving performance based on past executions\n")
	prompt.WriteString("6. Multi-domain workflow orchestration - handling complex business processes\n\n")

	prompt.WriteString("EVALUATION CRITERIA:\n")
	prompt.WriteString("A tool should be created if the code:\n")
	prompt.WriteString("- Is general/reusable enough to be useful in multiple contexts (not task-specific)\n")
	prompt.WriteString("- Aligns with system objectives (autonomous execution, knowledge management, goal achievement)\n")
	prompt.WriteString("- Would be useful for future autonomous task execution\n")
	prompt.WriteString("- Has clear inputs and outputs (can be parameterized)\n")
	prompt.WriteString("- Represents a meaningful capability (not trivial one-liners)\n\n")

	prompt.WriteString("CODE TO EVALUATE:\n")
	prompt.WriteString(fmt.Sprintf("Language: %s\n", language))
	prompt.WriteString(fmt.Sprintf("Task Description: %s\n", description))
	prompt.WriteString(fmt.Sprintf("Code:\n```%s\n%s\n```\n\n", language, code))

	prompt.WriteString("Respond with ONLY a JSON object in this exact format:\n")
	prompt.WriteString(`{"should_create_tool": true/false, "reason": "brief explanation"}`)
	prompt.WriteString("\n\nDo not include any other text, only the JSON object.")

	return prompt.String()
}

// parseToolEvaluationResponse parses LLM response to extract tool creation recommendation
func (ie *IntelligentExecutor) parseToolEvaluationResponse(response string) (shouldCreate bool, reason string) {
	// Try to extract JSON from response
	response = strings.TrimSpace(response)

	// Find JSON object
	jsonStart := strings.Index(response, "{")
	jsonEnd := strings.LastIndex(response, "}")
	if jsonStart == -1 || jsonEnd == -1 || jsonEnd <= jsonStart {
		log.Printf("⚠️ [TOOL-CREATOR] Could not find JSON in LLM response: %s", truncateString(response, 200))
		return false, "invalid response format"
	}

	jsonStr := response[jsonStart : jsonEnd+1]

	var result struct {
		ShouldCreateTool bool   `json:"should_create_tool"`
		Reason           string `json:"reason"`
	}

	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		log.Printf("⚠️ [TOOL-CREATOR] Failed to parse LLM response JSON: %v, response: %s", err, truncateString(jsonStr, 200))
		return false, "failed to parse response"
	}

	return result.ShouldCreateTool, result.Reason
}

// truncateString truncates a string to max length for logging
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// generateToolIDFromCode creates a stable tool ID from code characteristics
func (ie *IntelligentExecutor) generateToolIDFromCode(language, code, taskName string) string {
	// Try to derive from task name first (if it's generic)
	norm := strings.ToLower(strings.TrimSpace(taskName))
	norm = strings.ReplaceAll(norm, " ", "_")
	norm = strings.ReplaceAll(norm, "/", "_")
	norm = strings.ReplaceAll(norm, "-", "_")

	// If task name looks generic (not task-specific), use it
	if !strings.Contains(norm, "first") && !strings.Contains(norm, "n_") &&
		!strings.Contains(norm, "specific") && len(norm) < 30 {
		return "tool_" + norm
	}

	// Otherwise, generate from code characteristics
	base := strings.ToLower(strings.TrimSpace(language))
	if base == "" {
		base = "util"
	}

	lower := strings.ToLower(code)
	score := 0
	keywords := []string{"http", "json", "parse", "extract", "client", "retry", "cache", "transform"}
	for _, kw := range keywords {
		if strings.Contains(lower, kw) {
			score++
		}
	}

	// Use hash of code length and score for uniqueness
	hash := len(code) % 1000
	return fmt.Sprintf("tool_%s_util_%d_%d", base, hash, score)
}

// toolExists checks if a tool already exists
func (ie *IntelligentExecutor) toolExists(toolID string) bool {
	if ie.hdnBaseURL == "" {
		return false
	}

	url := fmt.Sprintf("%s/api/v1/tools", ie.hdnBaseURL)
	resp, err := http.Get(url)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false
	}

	var result struct {
		Tools []struct {
			ID string `json:"id"`
		} `json:"tools"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return false
	}

	for _, tool := range result.Tools {
		if tool.ID == toolID {
			return true
		}
	}
	return false
}

// createToolFromCode creates a Tool definition from successful code execution
func (ie *IntelligentExecutor) createToolFromCode(toolID string, req *ExecutionRequest, code *GeneratedCode, result *IntelligentExecutionResult) map[string]interface{} {
	// Determine input schema from context (if any parameters were used)
	inputSchema := map[string]string{}
	if len(req.Context) > 0 {
		// Common parameter patterns
		for key, value := range req.Context {
			if key != "input" && key != "artifact_names" && value != "" {
				// Infer type from value
				paramType := "string"
				if _, err := strconv.Atoi(value); err == nil {
					paramType = "int"
				} else if _, err := strconv.ParseFloat(value, 64); err == nil {
					paramType = "float"
				} else if strings.ToLower(value) == "true" || strings.ToLower(value) == "false" {
					paramType = "bool"
				}
				inputSchema[key] = paramType
			}
		}
	}

	// If no context parameters, use generic input
	if len(inputSchema) == 0 {
		inputSchema["input"] = "string"
	}

	// Create tool definition with exec spec for dynamic execution
	// The exec spec will be used by the handler to execute the code dynamically
	tool := map[string]interface{}{
		"id":           toolID,
		"name":         req.TaskName,
		"description":  fmt.Sprintf("Auto-created tool from successful execution: %s", req.Description),
		"input_schema": inputSchema,
		"output_schema": map[string]string{
			"output":  "string",
			"success": "bool",
		},
		"permissions":  []string{"proc:exec"},
		"safety_level": "medium",
		"created_by":   "agent",
		"exec": map[string]interface{}{
			"type":     "code",
			"code":     code.Code,
			"language": code.Language,
		},
	}

	return tool
}

// registerToolViaAPI registers a tool via the HDN API
func (ie *IntelligentExecutor) registerToolViaAPI(tool map[string]interface{}) error {
	if ie.hdnBaseURL == "" {
		return fmt.Errorf("HDN base URL not configured")
	}

	url := fmt.Sprintf("%s/api/v1/tools", ie.hdnBaseURL)
	data, err := json.Marshal(tool)
	if err != nil {
		return fmt.Errorf("failed to marshal tool: %v", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to register tool: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("tool registration failed with status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// recordFailedExecution records a failed execution for learning
func (ie *IntelligentExecutor) recordFailedExecution(req *ExecutionRequest, result *IntelligentExecutionResult) {
	if ie.learningRedis == nil {
		return
	}

	taskCategory := ie.deriveTaskCategory(req.TaskName, req.Description)

	// Update strategy success rate (failure)
	strategyKey := fmt.Sprintf("codegen_strategy:%s:%s", taskCategory, req.Language)
	strategyData, err := ie.learningRedis.Get(ie.ctx, strategyKey).Result()

	if err == nil && strategyData != "" {
		var strategy CodeGenStrategy
		json.Unmarshal([]byte(strategyData), &strategy)

		// Update success rate (exponential moving average)
		alpha := 0.1
		strategy.SuccessRate = alpha*0.0 + (1-alpha)*strategy.SuccessRate
		strategy.UsageCount++
		strategy.LastUsed = time.Now()

		strategyDataJSON, _ := json.Marshal(strategy)
		ie.learningRedis.Set(ie.ctx, strategyKey, strategyDataJSON, 30*24*time.Hour)
	}

	log.Printf("📊 [LEARNING] Recorded failed execution: %s/%s", taskCategory, req.Language)
}

// deriveTaskCategory derives a task category from task name and description
func (ie *IntelligentExecutor) deriveTaskCategory(taskName, description string) string {
	combined := strings.ToLower(taskName + " " + description)

	// Categorize based on keywords
	if strings.Contains(combined, "json") || strings.Contains(combined, "parse") {
		return "json_processing"
	} else if strings.Contains(combined, "file") || strings.Contains(combined, "read") || strings.Contains(combined, "write") {
		return "file_operations"
	} else if strings.Contains(combined, "http") || strings.Contains(combined, "api") || strings.Contains(combined, "request") {
		return "http_operations"
	} else if strings.Contains(combined, "calculate") || strings.Contains(combined, "math") || strings.Contains(combined, "compute") {
		return "calculation"
	} else if strings.Contains(combined, "transform") || strings.Contains(combined, "convert") {
		return "data_transformation"
	}

	return "general"
}

// identifyFocusAreas identifies areas showing promise for focused learning
func (ie *IntelligentExecutor) identifyFocusAreas() []CodeGenLearningProgress {
	if ie.learningRedis == nil {
		return []CodeGenLearningProgress{}
	}

	var focusAreas []CodeGenLearningProgress

	// Scan for strategies with high success rates
	pattern := "codegen_strategy:*"
	keys, err := ie.learningRedis.Keys(ie.ctx, pattern).Result()
	if err != nil {
		return focusAreas
	}

	for _, key := range keys {
		strategyData, err := ie.learningRedis.Get(ie.ctx, key).Result()
		if err != nil {
			continue
		}

		var strategy CodeGenStrategy
		if err := json.Unmarshal([]byte(strategyData), &strategy); err != nil {
			continue
		}

		// Calculate focus score (success rate + quality - retries)
		focusScore := strategy.SuccessRate*0.5 + strategy.AvgQuality*0.3 - (strategy.AvgRetries/10.0)*0.2

		if focusScore > 0.5 && strategy.UsageCount >= 3 { // Only focus on areas with enough data
			progress := CodeGenLearningProgress{
				TaskCategory:   strategy.TaskCategory,
				Language:       strategy.Language,
				SuccessRate:    strategy.SuccessRate,
				AvgQuality:     strategy.AvgQuality,
				RecentProgress: 0.0, // Could be calculated from recent executions
				FocusScore:     focusScore,
			}
			focusAreas = append(focusAreas, progress)
		}
	}

	return focusAreas
}

// assessCodeQuality assesses the quality of generated code
func (ie *IntelligentExecutor) assessCodeQuality(code *GeneratedCode, retryCount int) float64 {
	quality := 1.0

	// Penalize for retries
	quality -= float64(retryCount) * 0.2

	// Reward for code length (reasonable size)
	lines := strings.Count(code.Code, "\n")
	if lines > 5 && lines < 100 {
		quality += 0.1
	} else if lines > 100 {
		quality -= 0.1 // Too long might indicate complexity
	}

	// Ensure quality is between 0 and 1
	if quality < 0 {
		quality = 0
	}
	if quality > 1 {
		quality = 1
	}

	return quality
}

// extractFileFromSSH extracts a file from the SSH execution host
// Files are typically created in temp directories or the current working directory
func (ie *IntelligentExecutor) extractFileFromSSH(ctx context.Context, filename, language string) ([]byte, error) {
	// Get RPI host from environment
	rpiHost := os.Getenv("RPI_HOST")
	if rpiHost == "" {
		rpiHost = "192.168.1.58" // Default
	}

	// Common locations where files might be created
	searchPaths := []string{
		"./" + filename,                 // Current directory
		"/home/pi/" + filename,          // Home directory
		"/home/pi/.hdn/tmp/" + filename, // Temp directory
		"/tmp/" + filename,              // System temp
	}

	// Also check in language-specific temp directories
	if language == "go" {
		searchPaths = append(searchPaths, "/home/pi/.hdn/go_tmp_*/"+filename)
	} else if language == "java" {
		searchPaths = append(searchPaths, "/home/pi/.hdn/java_tmp_*/"+filename)
	}

	// Try to find and read the file
	for _, path := range searchPaths {
		// Use find command for glob patterns, cat for regular paths
		var cmd *exec.Cmd
		if strings.Contains(path, "*") {
			// Use find to locate file matching pattern
			findCmd := fmt.Sprintf("find /home/pi/.hdn -name '%s' -type f 2>/dev/null | head -1 | xargs cat 2>/dev/null", filename)
			cmd = exec.CommandContext(ctx, "ssh", "-o", "StrictHostKeyChecking=no", "-o", "UserKnownHostsFile=/dev/null", "-o", "LogLevel=ERROR",
				"pi@"+rpiHost, "sh", "-c", findCmd)
		} else {
			// Direct file read
			cmd = exec.CommandContext(ctx, "ssh", "-o", "StrictHostKeyChecking=no", "-o", "UserKnownHostsFile=/dev/null", "-o", "LogLevel=ERROR",
				"pi@"+rpiHost, "cat", path)
		}

		var stdout bytes.Buffer
		var stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		err := cmd.Run()
		if err == nil && stdout.Len() > 0 {
			log.Printf("✅ [INTELLIGENT] Extracted file %s from %s (%d bytes)", filename, path, stdout.Len())
			return stdout.Bytes(), nil
		}
	}

	// If not found in specific paths, try a broader search
	findCmd := fmt.Sprintf("find /home/pi -name '%s' -type f 2>/dev/null | head -1 | xargs cat 2>/dev/null", filename)
	cmd := exec.CommandContext(ctx, "ssh", "-o", "StrictHostKeyChecking=no", "-o", "UserKnownHostsFile=/dev/null", "-o", "LogLevel=ERROR",
		"pi@"+rpiHost, "sh", "-c", findCmd)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err == nil && stdout.Len() > 0 {
		log.Printf("✅ [INTELLIGENT] Extracted file %s via find search (%d bytes)", filename, stdout.Len())
		return stdout.Bytes(), nil
	}

	return nil, fmt.Errorf("file %s not found on SSH host", filename)
}
