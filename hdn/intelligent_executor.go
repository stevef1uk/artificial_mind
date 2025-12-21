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
	"path/filepath"
	"regexp"
	"runtime"
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
func (ie *IntelligentExecutor) filterRelevantTools(tools []Tool, req *ExecutionRequest) []Tool {
	var relevant []Tool
	descLower := strings.ToLower(req.Description)
	taskLower := strings.ToLower(req.TaskName)
	combined := descLower + " " + taskLower

	// Keywords that suggest tool usage
	toolKeywords := map[string][]string{
		"tool_html_scraper": {"scrape", "html", "web", "fetch", "url", "website", "article", "news", "page", "content"},
		"tool_http_get":     {"http", "url", "fetch", "get", "request", "api", "endpoint", "download"},
		"tool_file_read":    {"read", "file", "load", "open", "readfile", "read file"},
		"tool_file_write":   {"write", "file", "save", "store", "output", "write file", "save file"},
		"tool_ls":           {"list", "directory", "dir", "files", "ls", "list files"},
	}

	seen := make(map[string]bool) // Track tools we've already added

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

		// Also check tool description/name for keyword matches
		toolDesc := strings.ToLower(tool.Description + " " + tool.Name + " " + tool.ID)
		for _, keyword := range []string{"scrape", "http", "fetch", "url", "web", "file", "read", "write", "calculator", "calculate", "add", "subtract", "multiply", "divide", "math"} {
			if strings.Contains(combined, keyword) && strings.Contains(toolDesc, keyword) {
				relevant = append(relevant, tool)
				seen[tool.ID] = true
				break
			}
		}
	}

	return relevant
}

// executeWithSSHTool executes code using the SSH executor tool
func (ie *IntelligentExecutor) executeWithSSHTool(ctx context.Context, code, language string) (*DockerExecutionResponse, error) {
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

	response, err := ie.llmClient.callLLMWithContext(ctx, prompt)
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
		response, err := ie.llmClient.callLLM(prompt)
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
	descLower := strings.ToLower(req.Description)
	taskLower := strings.ToLower(req.TaskName)
	// Consider multiple web-related intents and presence of URLs in context
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
		strings.Contains(descLower, "crawler") || strings.Contains(taskLower, "crawler")

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
	return ie.executeTraditionally(ctx, req, start, workflowID)
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
	// Use LLM to determine task complexity for more accurate classification
	complexity, err := ie.classifyTaskComplexity(req)
	if err != nil {
		// Fallback to simple classification if LLM fails
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
- SIMPLE: Basic code generation, single-purpose programs, simple calculations, straightforward implementations
- COMPLEX: Multi-step workflows, system integrations, architectural decisions, complex business logic, multi-component solutions

Examples:
- "Write a Python program that prints 'Hello World'" → SIMPLE
- "Create a Go function that calculates fibonacci numbers" → SIMPLE  
- "Build a REST API with authentication and database integration" → COMPLEX
- "Design a microservices architecture for e-commerce" → COMPLEX
- "Create a data pipeline that processes files and sends notifications" → COMPLEX

Respond with only one word: "simple" or "complex"`,
		req.TaskName, req.Description, req.Language)

	// Use a shorter timeout for complexity classification
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	response, err := ie.llmClient.callLLMWithContext(ctx, prompt)
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
		} else {
			// Default to Python for mathematical tasks
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

	// Use LLM to intelligently categorize the request for safety
	context, err := ie.categorizeRequestForSafety(req)
	if err != nil {
		log.Printf("❌ [INTELLIGENT] Safety categorization failed: %v", err)
		result.Error = fmt.Sprintf("Cannot verify task safety - LLM categorization failed: %v", err)
		result.ExecutionTime = time.Since(start)
		return result, nil
	}

	allowed, reasons, err := CheckActionWithPrinciples(req.TaskName, context)
	if err != nil {
		log.Printf("❌ [INTELLIGENT] Principles check FAILED for %s: %v", req.TaskName, err)
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
					if finalResult, derr := ie.executeWithSSHTool(ctx, cachedCode.Code, req.Language); derr != nil {
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

	// Enhance description with matrix operation guidance if needed
	enhancedDesc := req.Description
	if strings.Contains(strings.ToLower(enhancedDesc), "matrix") ||
		(req.Context != nil && (req.Context["matrix1"] != "" || req.Context["matrix2"] != "")) {
		if req.Language == "go" {
			enhancedDesc += "\n\n🚨 CRITICAL FOR GO MATRIX OPERATIONS:\n- You MUST read matrices from environment variables using os.Getenv(\"matrix1\") and os.Getenv(\"matrix2\")\n- Parse the JSON string format (e.g., \"[[1,2],[3,4]]\") using encoding/json\n- DO NOT hardcode matrix values - the matrices will be different each time!\n- CRITICAL: Matrix type in Go is [][]int (slice of slices), NOT [][][]int (3D array)!\n- Example: var matrix1 [][]int; matrix1Str := os.Getenv(\"matrix1\"); json.Unmarshal([]byte(matrix1Str), &matrix1)\n- 🚨 CRITICAL: For matrix ADDITION, add corresponding elements: result[i][j] = matrix1[i][j] + matrix2[i][j]\n- Example: [[1,2],[3,4]] + [[5,6],[7,8]] = [[1+5,2+6],[3+7,4+8]] = [[6,8],[10,12]]\n- Function signature for matrix addition: func addMatrices(matrix1 [][]int, matrix2 [][]int) [][]int\n- You MUST import \"os\" for os.Getenv() and \"encoding/json\" for json.Unmarshal()\n- DO NOT import \"strconv\" unless you actually use it - unused imports cause compilation errors!\n- 🚨 CRITICAL OUTPUT FORMAT: When printing matrix results, print each ROW on a SEPARATE line using fmt.Println()\n- Example: For result [[6,8],[10,12]], you MUST print:\n  fmt.Println(result[0])  // prints [6 8]\n  fmt.Println(result[1])  // prints [10 12]\n- DO NOT print fmt.Println(result) - that prints the entire matrix as [[6 8] [10 12]] on one line!\n- DO NOT use fmt.Printf with %v for the entire matrix - print each row separately!\n- The output must be: [6 8] on first line, [10 12] on second line (two separate lines)\n- Use a loop: for i := 0; i < len(result); i++ { fmt.Println(result[i]) }"
		} else if req.Language == "python" {
			enhancedDesc += "\n\n🚨 CRITICAL FOR PYTHON MATRIX OPERATIONS:\n- You MUST read matrices from environment variables using os.getenv(\"matrix1\") and os.getenv(\"matrix2\")\n- Parse the JSON string format (e.g., \"[[1,2],[3,4]]\") using json.loads()\n- DO NOT hardcode matrix values - the matrices will be different each time!\n- Example: matrix1 = json.loads(os.getenv(\"matrix1\"))"
		}
	}

	// Add guidance for reading context parameters from environment variables (for Python)
	if req.Language == "python" && req.Context != nil && len(req.Context) > 0 {
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

	codeGenReq := &CodeGenerationRequest{
		TaskName:    req.TaskName,
		Description: enhancedDesc,
		Language:    req.Language,
		Context:     filteredCtx,
		Tags:        []string{"intelligent_execution", "auto_generated"},
		Executable:  true,
		Tools:       relevantTools,
		ToolAPIURL:  ie.hdnBaseURL,
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
	// Language-specific code sanitization
	if generatedCode != nil {
		if strings.EqualFold(req.Language, "python") {
			generatedCode.Code = sanitizeGeneratedPythonCode(generatedCode.Code)
		} else if strings.EqualFold(req.Language, "go") {
			generatedCode.Code = sanitizeGeneratedGoCode(generatedCode.Code)
		}
	}
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
		result.Error = "Code validation failed after all retry attempts"
		result.ExecutionTime = time.Since(start)
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

	// Skip tool registration and use direct Docker executor for file storage
	// This avoids the 404 tool errors and uses the working Docker execution system
	log.Printf("🎯 [INTELLIGENT] Final execution using direct Docker executor for file storage")
	if finalResult, derr := ie.executeWithSSHTool(ctx, generatedCode.Code, req.Language); derr != nil {
		log.Printf("⚠️ [INTELLIGENT] Final execution failed: %v", derr)
	} else if finalResult.Success {
		log.Printf("✅ [INTELLIGENT] Final execution successful, files stored")
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
	env["QUIET"] = "0"

	// Create Docker execution request
	// (removed: unused dockerReq)

	// Choose execution method based on EXECUTION_METHOD environment variable
	executionMethod := strings.TrimSpace(os.Getenv("EXECUTION_METHOD"))
	useSSH := executionMethod == "ssh" || (executionMethod == "" && (runtime.GOARCH == "arm64" || runtime.GOARCH == "aarch64" || os.Getenv("ENABLE_ARM64_TOOLS") == "true"))

	var result *DockerExecutionResponse
	var err error

	if useSSH {
		log.Printf("🧪 [VALIDATION] Using SSH executor for validation")
		result, err = ie.executeWithSSHTool(ctx, code.Code, code.Language)
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
		// Disallow direct container orchestration from user code
		" docker ", "'docker'", "\"docker\"", "podman", "kubectl",
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
	response, err := ie.llmClient.callLLM(fixPrompt)
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
	if originalCode.Language == "go" {
		languageGuidance = `
🚨 CRITICAL FOR GO CODE FIXES:
- Read the compilation error message CAREFULLY - it tells you exactly what's wrong!
- Common Go compilation errors:
  * "undefined: X" - Missing import or typo in function/variable name
  * "X declared but not used" - Remove unused imports/variables (Go treats these as ERRORS)
  * "cannot use X (type Y) as type Z" - Type mismatch, fix the type
  * "syntax error" - Check for missing braces, parentheses, or semicolons
  * "imported and not used" - Remove the unused import
  * "assignment mismatch: 2 variables but X returns 1 value" - Function returns only 1 value, not 2!
- If the error says "declared but not used", REMOVE that import/variable - don't try to use it!
- If the error says "undefined", check if you need to import a package
- Make sure ALL imports are actually used in the code
- The error message shows the exact line and column where the problem is - fix that specific issue!

🚨 CRITICAL: json.Unmarshal USAGE - COMMON MISTAKE!
- json.Unmarshal returns ONLY 1 value (error), NOT 2 values!
- WRONG: jsonBytes, _ := json.Unmarshal([]byte(...), &data)  ❌
- CORRECT: err := json.Unmarshal([]byte(...), &data)  ✅
- If you need to read from stdin first:
  - Step 1: jsonBytes, _ := io.ReadAll(os.Stdin)  (returns []byte, error)
  - Step 2: err := json.Unmarshal(jsonBytes, &data)  (returns only error)
- Do NOT confuse json.Unmarshal (returns 1 value) with io.ReadAll (returns 2 values)!

🚨 CRITICAL: READING FROM STDIN - DO NOT HARDCODE!
- If the task says "read JSON from stdin" or "read from stdin", you MUST use io.ReadAll(os.Stdin)
- WRONG: Hardcoding JSON like json.Unmarshal([]byte("{\"key\": \"value\"}"), &data)  ❌
- CORRECT: jsonBytes, _ := io.ReadAll(os.Stdin) then json.Unmarshal(jsonBytes, &data)  ✅
- The input will be provided via stdin at runtime - do NOT hardcode test data!
- You MUST import "io" and "os" to use io.ReadAll(os.Stdin)

🚨 RUNTIME ERRORS - JSON TYPE CASTING:
- If you see errors like "Error: 'number' field is not an int64" or "field is not an int64":
  - JSON numbers in Go are ALWAYS parsed as float64, NOT int64!
  - You MUST use: data["number"].(float64) NOT data["number"].(int64)
  - Then convert to int if needed: number := int(numVal)
  - Example fix:
    OLD (WRONG): number, ok := data["number"].(int64)
    NEW (CORRECT): if numVal, ok := data["number"].(float64); ok { number := int(numVal) }
- Always use type assertion with ok check to avoid panics: if val, ok := data["key"].(float64); ok { ... }

🚨 SYSTEMATIC FIX PROCESS - DO ALL OF THESE IN ONE PASS:
1. Read ALL errors in the error message - don't just fix one! Fix ALL errors at once!
2. Create a checklist of ALL errors before fixing:
   - List every "undefined: X" error
   - List every "imported and not used" error
   - List every "declared but not used" error
   - List every "assignment mismatch" error
3. For each "undefined: X" error:
   - If X is a function (like os.Getenv, json.Unmarshal, fmt.Println), add the import for that package
   - os.Getenv requires: import "os"
   - json.Unmarshal requires: import "encoding/json"
   - fmt.Println requires: import "fmt"
   - io.ReadAll requires: import "io" AND "os" (for os.Stdin)
   - os.Stdin requires: import "os"
   - log.Fatal requires: import "log"
4. For "assignment mismatch: 2 variables but X returns 1 value" errors:
   - This means you're trying to assign 2 values from a function that returns only 1!
   - json.Unmarshal returns ONLY error: err := json.Unmarshal(bytes, &data)
   - io.ReadAll returns ([]byte, error): bytes, err := io.ReadAll(os.Stdin)
   - Do NOT mix them up!
5. For each "imported and not used" or "declared but not used" error:
   - REMOVE that import or variable completely - do NOT try to use it!
   - BUT: Before removing, check if you need to ADD a different import that was missing!
   - Example: If you see "io imported and not used" AND "undefined: json", you need to:
     * Remove "io" if it's truly unused
     * ADD "encoding/json" for json.Unmarshal
6. CRITICAL: When removing unused imports, make sure you're not removing imports that are needed!
   - If code uses json.Unmarshal, you MUST have "encoding/json"
   - If code uses io.ReadAll, you MUST have "io" and "os"
   - If code uses fmt.Println, you MUST have "fmt"
   - Check the code body to see what functions are actually used!
7. For runtime errors about type casting (especially JSON numbers):
   - JSON numbers are float64, not int64 - fix the type assertion!
8. After fixing, verify your checklist:
   - ✅ Every function used has its package imported
   - ✅ No unused imports remain
   - ✅ No unused variables remain
   - ✅ JSON number type assertions use float64, not int64
   - ✅ json.Unmarshal is called correctly (returns only error, not 2 values)
   - ✅ All "undefined" errors are fixed
   - ✅ All "imported and not used" errors are fixed
9. Example: If errors are "io imported and not used", "log imported and not used", AND "undefined: json":
   - Step 1: Check what functions are used in the code body
   - Step 2: If json.Unmarshal is used, ADD import "encoding/json"
   - Step 3: Remove "io" if io.ReadAll is not used
   - Step 4: Remove "log" if log functions are not used
   - Step 5: Verify: json.Unmarshal needs "encoding/json" ✓
10. Example: If error is "assignment mismatch: 2 variables but json.Unmarshal returns 1 value":
   - WRONG: jsonBytes, _ := json.Unmarshal([]byte(str), &data)
   - CORRECT: err := json.Unmarshal([]byte(str), &data)
   - If you need bytes from stdin: jsonBytes, _ := io.ReadAll(os.Stdin) THEN err := json.Unmarshal(jsonBytes, &data)
11. If task says "read from stdin" but code hardcodes JSON:
   - WRONG: json.Unmarshal([]byte("{\"key\": \"value\"}"), &data)  (hardcoded!)
   - CORRECT: jsonBytes, _ := io.ReadAll(os.Stdin); err := json.Unmarshal(jsonBytes, &data)
   - MUST import "io" and "os" for stdin reading
12. 🚨 CRITICAL: Fix ALL errors in ONE code revision - don't fix one error at a time!
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

// sanitizeGeneratedPythonCode patches common issues in generated CSV analysis scripts
func sanitizeGeneratedPythonCode(code string) string {
	fixed := code
	// Ensure describe includes all columns for robust stats
	fixed = strings.ReplaceAll(fixed, ".describe()", ".describe(include='all')")

	// Remove unsafe subprocess and os.system usages proactively
	// This prevents safety loops by stripping lines that invoke external commands
	reUnsafe := regexp.MustCompile(`(?m)^.*(subprocess\.|os\.system\().*$`)
	fixed = reUnsafe.ReplaceAllString(fixed, "")

	// Fix session_id references that try to convert to int
	// Pattern: session_id = int(os.getenv('session_id', 'default'))
	// Replace with: session_id = os.getenv('session_id', 'default') or remove if not needed
	reSessionIDInt := regexp.MustCompile(`(?m)^\s*session_id\s*=\s*int\(os\.getenv\(['"]session_id['"],\s*['"][^'"]*['"]\)\)`)
	fixed = reSessionIDInt.ReplaceAllString(fixed, "")
	// Also handle variations like session_id = int(os.getenv("session_id", default))
	reSessionIDInt2 := regexp.MustCompile(`(?m)^\s*session_id\s*=\s*int\(os\.getenv\(["']session_id["'],\s*[^)]+\)\)`)
	fixed = reSessionIDInt2.ReplaceAllString(fixed, "")
	// Remove any remaining session_id assignments that aren't needed
	reSessionIDAny := regexp.MustCompile(`(?m)^\s*session_id\s*=\s*os\.getenv\(['"]session_id['"][^)]*\)`)
	fixed = reSessionIDAny.ReplaceAllString(fixed, "")

	// If a dict is saved via to_csv, wrap it in DataFrame first
	// Replace patterns like: <name>.to_csv(path) where <name> may be a dict
	// Heuristic: ensure imports ONLY if pandas operations are actually used
	// Check if code uses pandas operations (pd., pandas., .to_csv, DataFrame, etc.)
	usesPandas := strings.Contains(fixed, ".to_csv(") ||
		strings.Contains(fixed, "pd.") ||
		strings.Contains(fixed, "pandas.") ||
		strings.Contains(fixed, "DataFrame") ||
		strings.Contains(fixed, ".describe(") ||
		strings.Contains(fixed, ".read_csv(") ||
		strings.Contains(fixed, ".to_excel(")

	if usesPandas {
		ensureImports := []string{"import pandas as pd"}
		for _, imp := range ensureImports {
			if !strings.Contains(fixed, imp) {
				fixed = imp + "\n" + fixed
			}
		}
	}

	// Heuristic rewrite: summary_stats = {...}; summary_stats.to_csv(...) -> pd.DataFrame(summary_stats).to_csv(...)
	reToCSV := regexp.MustCompile(`(?m)^(\s*)([A-Za-z_][A-Za-z0-9_]*)\.to_csv\(([^)]*)\)`) // captures var.to_csv(args)
	fixed = reToCSV.ReplaceAllString(fixed, `${1}pd.DataFrame(\2).to_csv(\3)`)

	return fixed
}

// sanitizeGeneratedGoCode fixes common issues in generated Go code
// It does NOT add missing imports - the LLM retry mechanism should handle that
func sanitizeGeneratedGoCode(code string) string {
	fixed := code

	// Fix common JSON type assertion errors: JSON numbers are float64, not int64
	// Pattern: data["key"].(int64) -> data["key"].(float64)
	// This handles both simple assertions and ones with ok checks
	reInt64Assertion := regexp.MustCompile(`(\w+)\["([^"]+)"\]\.\(int64\)`)
	fixed = reInt64Assertion.ReplaceAllString(fixed, `${1}["${2}"].(float64)`)

	return fixed
}

// inferLanguageFromRequest tries to determine the intended programming language
// from the task name, description, and context. Returns empty string if unknown.
func (ie *IntelligentExecutor) inferLanguageFromRequest(req *ExecutionRequest) string {
	// Strong hints in description
	desc := strings.ToLower(strings.TrimSpace(req.Description))
	if strings.Contains(desc, " go ") || strings.HasPrefix(desc, "go ") || strings.HasSuffix(desc, " in go") ||
		strings.Contains(desc, " in golang") || strings.Contains(desc, "golang") ||
		strings.Contains(desc, "main.go") {
		return "go"
	}

	// Hints in task name
	task := strings.ToLower(strings.TrimSpace(req.TaskName))
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

// inferLanguageFromIntelligentRequest tries to determine the intended programming language
// from the task name, description, and context. Returns empty string if unknown.
func inferLanguageFromRequest(req *IntelligentExecutionRequest) string {
	// Strong hints in description
	desc := strings.ToLower(strings.TrimSpace(req.Description))
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
	// Keys to drop
	drop := map[string]bool{
		"session_id":         true,
		"project_id":         true,
		"artifact_names":     true,
		"save_code_filename": true,
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

	for i, program := range programs {
		log.Printf("🔗 [CHAINED] Executing program %d/%d: %s", i+1, len(programs), program.Name)

		// Create execution request for this program
		programReq := &ExecutionRequest{
			TaskName:    program.Name,
			Description: program.Description,
			Context:     program.Context,
			Language:    program.Language,
			MaxRetries:  req.MaxRetries,
			Timeout:     req.Timeout,
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
		if program.Language == "go" {
			artifactName = fmt.Sprintf("prog%d.go", i+1)
		} else if program.Language == "python" {
			artifactName = fmt.Sprintf("prog%d.py", i+1)
		} else if program.Language == "javascript" {
			artifactName = fmt.Sprintf("prog%d.js", i+1)
		} else if program.Language == "java" {
			artifactName = fmt.Sprintf("prog%d.java", i+1)
		}
		ie.storeChainedProgramArtifact(programResult.GeneratedCode, workflowID, artifactName)

		if !programResult.Success {
			return &IntelligentExecutionResult{
				Success:       false,
				Error:         fmt.Sprintf("Program %d execution failed: %s", i+1, programResult.Error),
				ExecutionTime: time.Since(start),
				WorkflowID:    workflowID,
			}, nil
		}

		// Extract output from this program
		if output, ok := programResult.Result.(string); ok {
			lastOutput = output
			allOutputs = append(allOutputs, output)
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

	log.Printf("🔗 [CHAINED] All programs completed successfully")
	log.Printf("🔗 [CHAINED] Program outputs: %v", allOutputs)
	log.Printf("🔗 [CHAINED] Final result: %s", finalOutput)

	result := &IntelligentExecutionResult{
		Success:        true,
		Result:         finalOutput,       // Use last program's output, not combined
		GeneratedCode:  generatedCodes[0], // Use first program's code as primary
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

// parseChainedPrograms parses a request into multiple programs
func (ie *IntelligentExecutor) parseChainedPrograms(req *ExecutionRequest) ([]ChainedProgram, error) {
	// For now, implement a simple parser that looks for specific patterns
	// This could be enhanced with LLM-based parsing

	description := req.Description
	programs := []ChainedProgram{}

	// Look for "prog1" and "prog2" patterns or check for "then create" patterns (case-insensitive)
	hasProg1 := strings.Contains(description, "prog1") ||
		(strings.Contains(strings.ToLower(description), "python") && strings.Contains(strings.ToLower(description), "generates")) ||
		(strings.Contains(strings.ToLower(description), "create") && strings.Contains(strings.ToLower(description), "python"))
	hasProg2 := strings.Contains(description, "prog2") ||
		(strings.Contains(strings.ToLower(description), "go") && strings.Contains(strings.ToLower(description), "reads")) ||
		(strings.Contains(strings.ToLower(description), "then") && strings.Contains(strings.ToLower(description), "go"))

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
		return nil, fmt.Errorf("could not parse programs from request")
	}

	return programs, nil
}

// parseProgramRequirements parses the description to extract specific requirements for each program
func (ie *IntelligentExecutor) parseProgramRequirements(description, programName string) (string, string) {
	lower := strings.ToLower(description)

	// Handle single-line descriptions with "then" separator
	if strings.Contains(lower, "then") {
		parts := strings.Split(description, "then")
		if len(parts) >= 2 {
			part1 := strings.TrimSpace(parts[0])
			part2 := strings.TrimSpace(parts[1])

			// Check if this is for prog1 (first part) or prog2 (second part)
			if programName == "prog1" {
				// Extract language from first part
				lang := "python" // default
				if strings.Contains(strings.ToLower(part1), "python") {
					lang = "python"
				} else if strings.Contains(strings.ToLower(part1), "go") {
					lang = "go"
				} else if strings.Contains(strings.ToLower(part1), "javascript") || strings.Contains(strings.ToLower(part1), "js") {
					lang = "javascript"
				} else if strings.Contains(strings.ToLower(part1), "java") {
					lang = "java"
				}
				return part1, lang
			} else if programName == "prog2" {
				// Extract language from second part
				lang := "go" // default
				if strings.Contains(strings.ToLower(part2), "python") {
					lang = "python"
				} else if strings.Contains(strings.ToLower(part2), "go") {
					lang = "go"
				} else if strings.Contains(strings.ToLower(part2), "javascript") || strings.Contains(strings.ToLower(part2), "js") {
					lang = "javascript"
				} else if strings.Contains(strings.ToLower(part2), "java") {
					lang = "java"
				}
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
			enhancedDesc += "\n\n🚨 CRITICAL FOR GO MATRIX OPERATIONS:\n- You MUST read matrices from environment variables using os.Getenv(\"matrix1\") and os.Getenv(\"matrix2\")\n- Parse the JSON string format (e.g., \"[[1,2],[3,4]]\") using encoding/json\n- DO NOT hardcode matrix values - the matrices will be different each time!\n- Example: matrix1Str := os.Getenv(\"matrix1\"); json.Unmarshal([]byte(matrix1Str), &matrix1)"
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
		TaskName:    req.TaskName,
		Description: enhancedDesc,
		Language:    req.Language,
		Context:     filteredCtx,
		Tags:        []string{"intelligent_execution", "auto_generated", "chained"},
		Executable:  true,
	}

	// Create a specific prompt for Go programs that need to parse JSON
	if req.Language == "go" && strings.Contains(strings.ToLower(enhancedDesc), "json") {
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

	// Language-specific code sanitization
	if strings.EqualFold(req.Language, "python") {
		generatedCode.Code = sanitizeGeneratedPythonCode(generatedCode.Code)
	} else if strings.EqualFold(req.Language, "go") {
		generatedCode.Code = sanitizeGeneratedGoCode(generatedCode.Code)
	}

	// Validate and iterate on the generated code (retry loop for chained programs)
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
