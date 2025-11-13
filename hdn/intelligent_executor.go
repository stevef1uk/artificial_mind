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
) *IntelligentExecutor {
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
	sshEnabled := execMethod == "ssh" ||
		strings.TrimSpace(os.Getenv("ENABLE_ARM64_TOOLS")) == "true" ||
		runtime.GOARCH == "arm64" ||
		runtime.GOARCH == "aarch64"

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
		// If the tool is missing/not available (e.g., 404), fall back to local Docker executor
		low := strings.ToLower(msg)
		missing := strings.Contains(low, "status 404") || strings.Contains(low, "not found") || strings.Contains(low, "tool not available")
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
		prompt := "You are a concise summarizer. Output ONLY the requested sections and nothing else.\n" +
			"Constraints: paragraph <= 80 words; exactly 3 bullets; exactly 3 short follow-up questions.\n" +
			"Format:\n" + format
		if strings.TrimSpace(req.Description) != "" {
			prompt += "Description:\n" + req.Description + "\n\n"
		}
		if len(req.Context) > 0 {
			prompt += "Context:\n"
			for k, v := range req.Context {
				if strings.TrimSpace(k) != "" && strings.TrimSpace(v) != "" {
					prompt += "- " + k + ": " + v + "\n"
				}
			}
			prompt += "\n"
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

	// Determine file extension based on language
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

	// Create filename
	filename := fmt.Sprintf("%s%s", programName, ext)

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

	// Clean up old entries (older than 30 seconds)
	for key, timestamp := range ie.recentTasks {
		if now.Sub(timestamp) > 30*time.Second {
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
		req.Timeout = 600
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
	codeGenReq := &CodeGenerationRequest{
		TaskName:    req.TaskName,
		Description: req.Description,
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
	for k, v := range req.Context {
		if v != "" {
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
			Timeout:      300,
			Environment:  env,
			IsValidation: true,
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
		log.Printf("❌ [VALIDATION] Code execution failed: %s", result.Error)
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

	// If a dict is saved via to_csv, wrap it in DataFrame first
	// Replace patterns like: <name>.to_csv(path) where <name> may be a dict
	// Heuristic: ensure imports
	ensureImports := []string{"import pandas as pd"}
	for _, imp := range ensureImports {
		if !strings.Contains(fixed, imp) {
			fixed = imp + "\n" + fixed
		}
	}

	// Heuristic rewrite: summary_stats = {...}; summary_stats.to_csv(...) -> pd.DataFrame(summary_stats).to_csv(...)
	reToCSV := regexp.MustCompile(`(?m)^(\s*)([A-Za-z_][A-Za-z0-9_]*)\.to_csv\(([^)]*)\)`) // captures var.to_csv(args)
	fixed = reToCSV.ReplaceAllString(fixed, `${1}pd.DataFrame(\2).to_csv(\3)`)

	return fixed
}

// sanitizeGeneratedGoCode ensures only required imports are present and adds missing ones
// It does NOT add unused imports. It scans identifiers used in code to decide.
func sanitizeGeneratedGoCode(code string) string { return code }

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

		// Store individual program artifacts
		ie.storeChainedProgramArtifact(programResult.GeneratedCode, workflowID, fmt.Sprintf("prog%d", i+1))

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

	// Combine all outputs
	combinedOutput := strings.Join(allOutputs, "\n")

	log.Printf("🔗 [CHAINED] All programs completed successfully")

	result := &IntelligentExecutionResult{
		Success:        true,
		Result:         combinedOutput,
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
					program := ChainedProgram{
						Name:        strings.TrimSuffix(part, filepath.Ext(part)),
						Description: fmt.Sprintf("Generate %s", part),
						Context:     make(map[string]string),
						Language:    req.Language,
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
		req.Timeout = 600
	}

	result := &IntelligentExecutionResult{
		Success:         false,
		ValidationSteps: []ValidationStep{},
		WorkflowID:      workflowID,
	}

	// Generate code for this program using the existing code generator
	filteredCtx := filterCodegenContext(req.Context)
	codeGenReq := &CodeGenerationRequest{
		TaskName:    req.TaskName,
		Description: req.Description,
		Language:    req.Language,
		Context:     filteredCtx,
		Tags:        []string{"intelligent_execution", "auto_generated", "chained"},
		Executable:  true,
	}

	// Create a specific prompt for Go programs that need to parse JSON
	if req.Language == "go" && strings.Contains(strings.ToLower(req.Description), "json") {
		// Add specific instructions for JSON parsing in Go
		codeGenReq.Description = req.Description + "\n\nCRITICAL: For JSON parsing in Go, you MUST:\n1. Use map[string]interface{} to unmarshal JSON\n2. Extract the number as float64: data[\"number\"].(float64)\n3. Convert to int: int(data[\"number\"].(float64))\n4. NEVER try to unmarshal directly into int - this will fail\n\nExample:\nvar data map[string]interface{}\njson.Unmarshal([]byte(jsonStr), &data)\nnumber := int(data[\"number\"].(float64))\nresult := number * 2\nfmt.Println(result)"
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

	// Execute the generated code using the existing docker executor
	dockerReq := &DockerExecutionRequest{
		Code:     generatedCode.Code,
		Language: req.Language,
		Timeout:  req.Timeout,
	}

	execResult, err := ie.dockerExecutor.ExecuteCode(ctx, dockerReq)
	if err != nil {
		result.Error = fmt.Sprintf("Code execution failed: %v", err)
		result.ExecutionTime = time.Since(start)
		return result, nil
	}

	// Set results
	result.Success = execResult.Success
	result.Result = execResult.Output
	result.GeneratedCode = generatedCode
	result.ExecutionTime = time.Since(start)
	result.ValidationSteps = []ValidationStep{{
		Step:    "direct_execution",
		Success: execResult.Success,
		Message: "Program executed directly",
		Output:  execResult.Output,
	}}

	// Log tool metrics for intelligent execution
	ie.logIntelligentExecutionMetrics(ctx, req, result)

	return result, nil
}
