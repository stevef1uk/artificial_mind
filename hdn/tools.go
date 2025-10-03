package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"eventbus"
	"log"
)

// detectPythonPackagesForDocker parses Python code and returns a minimal list of pip packages
// to install inside a sandbox container. It's intentionally conservative.
func detectPythonPackagesForDocker(code string) []string {
	lower := strings.ToLower(code)
	// Known common packages mapping to pip names with minimal versions where sensible
	type pkg struct{ key, pip string }
	candidates := []pkg{
		{"pandas", "pandas>=1.3.0"},
		{"numpy", "numpy>=1.21.0"},
		{"matplotlib", "matplotlib>=3.5.0"},
		{"reportlab", "reportlab>=3.6.0"},
		{"seaborn", "seaborn"},
		{"scipy", "scipy"},
		{"sklearn", "scikit-learn"},
		{"requests", "requests"},
		{"beautifulsoup4", "beautifulsoup4"},
		{"bs4", "beautifulsoup4"},
		{"opencv", "opencv-python"},
		{"cv2", "opencv-python"},
		{"plotly", "plotly"},
		{"openpyxl", "openpyxl"},
		{"xlrd", "xlrd"},
	}
	// Skip if code explicitly sets a virtualenv or denies network installs
	if strings.Contains(lower, "pip install") || strings.Contains(lower, "venv") {
		return nil
	}
	uniq := map[string]bool{}
	out := []string{}
	for _, c := range candidates {
		if strings.Contains(lower, c.key) {
			if !uniq[c.pip] {
				uniq[c.pip] = true
				out = append(out, c.pip)
			}
		}
	}
	return out
}

// ResponseWrapper wraps the HTTP response writer to capture the response
type ResponseWrapper struct {
	http.ResponseWriter
	statusCode int
	body       []byte
}

func (rw *ResponseWrapper) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *ResponseWrapper) Write(data []byte) (int, error) {
	rw.body = append(rw.body, data...)
	return rw.ResponseWriter.Write(data)
}

// logToolCallResult logs the final result of a tool call
func (s *APIServer) logToolCallResult(ctx context.Context, toolCallLog *ToolCallLog, wrapper *ResponseWrapper, startTime time.Time) {
	toolCallLog.Duration = time.Since(startTime).Milliseconds()

	// Determine status based on HTTP status code
	if wrapper.statusCode >= 200 && wrapper.statusCode < 300 {
		toolCallLog.Status = "success"
	} else {
		toolCallLog.Status = "failure"
		// Try to extract error message from response body
		if len(wrapper.body) > 0 {
			var errorResp map[string]interface{}
			if err := json.Unmarshal(wrapper.body, &errorResp); err == nil {
				if errorMsg, ok := errorResp["error"].(string); ok {
					toolCallLog.Error = errorMsg
				}
			}
		}
	}

	// Try to parse response as JSON for logging
	if len(wrapper.body) > 0 {
		var responseData interface{}
		if err := json.Unmarshal(wrapper.body, &responseData); err == nil {
			toolCallLog.Response = responseData
		} else {
			toolCallLog.Response = string(wrapper.body)
		}
	}

	// Log the tool call
	if s.toolMetrics != nil {
		_ = s.toolMetrics.LogToolCall(ctx, toolCallLog)
	}
}

// Tool represents a callable capability the agent can use
type Tool struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Description  string            `json:"description"`
	InputSchema  map[string]string `json:"input_schema"`
	OutputSchema map[string]string `json:"output_schema"`
	Permissions  []string          `json:"permissions"`
	SafetyLevel  string            `json:"safety_level"`
	CreatedBy    string            `json:"created_by"`
	CreatedAt    time.Time         `json:"created_at"`
	Exec         *ToolExecSpec     `json:"exec,omitempty"`
}

type ToolExecSpec struct {
	Type  string   `json:"type"` // "cmd" or "image"
	Cmd   string   `json:"cmd"`  // for Type==cmd: absolute path inside container
	Args  []string `json:"args"`
	Image string   `json:"image,omitempty"` // for Type==image: docker image reference
}

func (s *APIServer) toolKey(id string) string { return "tool:" + id }
func (s *APIServer) toolsRegistryKey() string { return "tools:registry" }
func (s *APIServer) toolsUsageKey(agentID string) string {
	return fmt.Sprintf("tools:%s:usage_history", agentID)
}

// deleteTool removes a tool from registry
func (s *APIServer) deleteTool(ctx context.Context, id string) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("id required")
	}
	// Enforce: only allow deletion of agent-created (auto-generated) tools
	if val, err := s.redis.Get(ctx, s.toolKey(id)).Result(); err == nil {
		var t Tool
		if json.Unmarshal([]byte(val), &t) == nil {
			if !strings.EqualFold(strings.TrimSpace(t.CreatedBy), "agent") {
				return fmt.Errorf("deletion not allowed for non-agent tool")
			}
		}
	}
	if err := s.redis.Del(ctx, s.toolKey(id)).Err(); err != nil {
		return err
	}
	_ = s.redis.SRem(ctx, s.toolsRegistryKey(), id).Err()
	return nil
}

// registerTool stores the tool in Redis and publishes a discovery/created event
func (s *APIServer) registerTool(ctx context.Context, t Tool) error {
	if strings.TrimSpace(t.ID) == "" {
		return fmt.Errorf("tool ID is required")
	}
	if t.CreatedAt.IsZero() {
		t.CreatedAt = time.Now().UTC()
	}
	b, err := json.Marshal(t)
	if err != nil {
		return err
	}
	if err := s.redis.Set(ctx, s.toolKey(t.ID), b, 0).Err(); err != nil {
		return err
	}
	_ = s.redis.SAdd(ctx, s.toolsRegistryKey(), t.ID).Err()

	// Best-effort event emission
	if s.eventBus != nil {
		typeName := "agi.tool.discovered"
		if strings.EqualFold(t.CreatedBy, "agent") || strings.EqualFold(t.CreatedBy, "system") {
			typeName = "agi.tool.created"
		}
		evt := eventbus.CanonicalEvent{
			EventID:   eventbus.NewEventID("tool_", time.Now()),
			Source:    "hdn.api",
			Type:      typeName,
			Timestamp: time.Now().UTC(),
			Context:   eventbus.EventContext{ProjectID: "", SessionID: ""},
			Payload:   eventbus.EventPayload{Metadata: map[string]interface{}{"tool_id": t.ID, "name": t.Name, "safety": t.SafetyLevel}},
		}
		_ = s.eventBus.Publish(ctx, evt)
	}
	return nil
}

// listTools returns all tools in the registry
func (s *APIServer) listTools(ctx context.Context) ([]Tool, error) {
	ids, err := s.redis.SMembers(ctx, s.toolsRegistryKey()).Result()
	if err != nil {
		return nil, err
	}
	log.Printf("🔧 [LIST-TOOLS] Found %d tool IDs in registry: %v", len(ids), ids)
	tools := make([]Tool, 0, len(ids))
	for _, id := range ids {
		val, err := s.redis.Get(ctx, s.toolKey(id)).Result()
		if err != nil {
			log.Printf("❌ [LIST-TOOLS] Failed to get tool %s: %v", id, err)
			continue
		}
		var t Tool
		if err := json.Unmarshal([]byte(val), &t); err == nil {
			tools = append(tools, t)
			log.Printf("✅ [LIST-TOOLS] Successfully loaded tool: %s", id)
		} else {
			log.Printf("❌ [LIST-TOOLS] Failed to unmarshal tool %s: %v", id, err)
		}
	}
	log.Printf("🔧 [LIST-TOOLS] Returning %d tools", len(tools))
	return tools, nil
}

// handleListTools: GET /api/v1/tools
func (s *APIServer) handleListTools(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tools, err := s.listTools(ctx)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"tools": tools})
}

// handleRegisterTool: POST /api/v1/tools
func (s *APIServer) handleRegisterTool(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var t Tool
	if err := json.NewDecoder(r.Body).Decode(&t); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "invalid json"})
		return
	}
	if strings.TrimSpace(t.ID) == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "id is required"})
		return
	}
	if t.CreatedBy == "" {
		t.CreatedBy = "system"
	}
	if err := s.registerTool(ctx, t); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "tool": t})
}

// handleDeleteTool: DELETE /api/v1/tools/{id}
func (s *APIServer) handleDeleteTool(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 4 {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "missing tool id"})
		return
	}
	id := parts[3]
	if err := s.deleteTool(r.Context(), id); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "deleted": id})
}

// handleDiscoverTools: POST /api/v1/tools/discover (simple stub scanning env/binaries)
func (s *APIServer) handleDiscoverTools(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	found := []Tool{}

	// Simple built-ins for now: http_get tool
	httpTool := Tool{
		ID:          "tool_http_get",
		Name:        "HTTP GET",
		Description: "Fetches a URL and returns response body (sets a friendly User-Agent by default)",
		InputSchema: map[string]string{"url": "string", "user_agent": "string", "headers": "json"},
		OutputSchema: map[string]string{
			"status": "int",
			"body":   "string",
		},
		Permissions: []string{"net:read"},
		SafetyLevel: "low",
		CreatedBy:   "system",
	}
	_ = s.registerTool(ctx, httpTool)
	found = append(found, httpTool)

	// Wiki Bootstrapper (host-exec of bin/wiki-bootstrapper)
	wikiTool := Tool{
		ID:          "tool_wiki_bootstrapper",
		Name:        "Wikipedia Bootstrapper",
		Description: "Ingests Wikipedia concepts into Neo4j with rate limiting and pause/resume",
		InputSchema: map[string]string{
			"seeds":          "string", // comma-separated titles
			"max_depth":      "int",
			"max_nodes":      "int",
			"rpm":            "int",
			"burst":          "int",
			"jitter_ms":      "int",
			"min_confidence": "float",
			"domain":         "string",
			"job_id":         "string",
			"resume":         "bool",
			"pause":          "bool",
		},
		OutputSchema: map[string]string{"output": "string"},
		Permissions:  []string{"proc:exec", "net:read"},
		SafetyLevel:  "medium",
		CreatedBy:    "system",
	}
	_ = s.registerTool(ctx, wikiTool)
	found = append(found, wikiTool)

	// Optional: if Docker is available, register a docker_exec tool
	// Check environment variable first, then fall back to docker socket check
	executionMethod := os.Getenv("EXECUTION_METHOD")
	useDocker := executionMethod == "docker" || (executionMethod == "" && (os.Getenv("DOCKER_HOST") != "" || fileExists("/var/run/docker.sock")))
	if useDocker {
		dockerTool := Tool{
			ID:           "tool_docker_exec",
			Name:         "Docker Exec",
			Description:  "Runs code or images in Docker with sandboxed IO",
			InputSchema:  map[string]string{"image": "string", "cmd": "string"},
			OutputSchema: map[string]string{"stdout": "string", "stderr": "string", "exit_code": "int"},
			Permissions:  []string{"docker"},
			SafetyLevel:  "medium",
			CreatedBy:    "system",
		}
		_ = s.registerTool(ctx, dockerTool)
		found = append(found, dockerTool)
	}

	// Add Drone executor only when explicitly enabled or on ARM64
	// Conditions:
	// - EXECUTION_METHOD=drone
	// - OR ENABLE_ARM64_TOOLS=true
	// - OR running on ARM64
	execMethod := strings.TrimSpace(os.Getenv("EXECUTION_METHOD"))
	if execMethod == "drone" || os.Getenv("ENABLE_ARM64_TOOLS") == "true" || runtime.GOARCH == "arm64" || runtime.GOARCH == "aarch64" {
		arm64Tools := []Tool{
			{ID: "tool_drone_executor", Name: "Drone Executor", Description: "Execute code via Drone CI with Docker socket access", InputSchema: map[string]string{"code": "string", "language": "string", "image": "string", "environment": "json", "timeout": "int"}, OutputSchema: map[string]string{"success": "bool", "output": "string", "error": "string", "image": "string", "exit_code": "int", "duration_ms": "int"}, Permissions: []string{"drone:execute", "docker:build"}, SafetyLevel: "high", CreatedBy: "system"},
		}
		for _, t := range arm64Tools {
			_ = s.registerTool(ctx, t)
			found = append(found, t)
		}
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "discovered": found})
}

func fileExists(path string) bool {
	if _, err := os.Stat(path); err == nil {
		return true
	}
	return false
}

func getString(m map[string]interface{}, key string) (string, bool) {
	if m == nil {
		return "", false
	}
	if v, ok := m[key]; ok {
		if s, ok2 := v.(string); ok2 {
			return s, true
		}
	}
	return "", false
}

// getFileExtension returns the appropriate file extension for a given language
func getFileExtension(language string) string {
	switch language {
	case "go":
		return "go"
	case "python":
		return "py"
	case "bash":
		return "sh"
	case "javascript", "js":
		return "js"
	case "typescript", "ts":
		return "ts"
	case "rust":
		return "rs"
	case "java":
		return "java"
	case "c":
		return "c"
	case "cpp", "c++":
		return "cpp"
	default:
		return "txt"
	}
}

// BootstrapSeedTools loads tools_bootstrap.json if present or when registry is empty
func (s *APIServer) BootstrapSeedTools(ctx context.Context) {
	log.Printf("🔧 [BOOTSTRAP] Starting BootstrapSeedTools")
	// If registry already has entries, skip unless forced
	if n, err := s.redis.SCard(ctx, s.toolsRegistryKey()).Result(); err == nil && n > 0 {
		log.Printf("🔧 [BOOTSTRAP] Registry already has %d tools, but will still register ARM64 tools", n)
		// Don't return early - we still need to register ARM64 tools
	}

	// Look for tools_bootstrap.json in working dir or config dir
	paths := []string{"tools_bootstrap.json", filepath.Join("config", "tools_bootstrap.json")}
	for _, p := range paths {
		if !fileExists(p) {
			continue
		}
		f, err := os.Open(p)
		if err != nil {
			continue
		}
		var seeds []Tool
		if err := json.NewDecoder(f).Decode(&seeds); err == nil {
			for _, t := range seeds {
				// Principles gate: check tool by name with minimal context
				ctxMap := map[string]interface{}{"category": "tool_bootstrap", "safety_level": t.SafetyLevel}
				allowed, _, _ := CheckActionWithPrinciples("register_tool:"+t.ID, ctxMap)
				if !allowed {
					continue
				}
				_ = s.registerTool(ctx, t)
			}
			return
		}
		_ = f.Close()
	}

	// Fallback: register a minimal nucleus set
	defaults := []Tool{
		{ID: "tool_http_get", Name: "HTTP GET", Description: "Fetch URL", InputSchema: map[string]string{"url": "string"}, OutputSchema: map[string]string{"status": "int", "body": "string"}, Permissions: []string{"net:read"}, SafetyLevel: "low", CreatedBy: "system"},
		// Register html_scraper without Docker exec; we will run a host binary in handleInvokeTool
		{ID: "tool_html_scraper", Name: "HTML Scraper", Description: "Parse HTML and extract title/headings/paragraphs/links", InputSchema: map[string]string{"url": "string"}, OutputSchema: map[string]string{"items": "array"}, Permissions: []string{"net:read"}, SafetyLevel: "low", CreatedBy: "system"},
		{ID: "tool_file_read", Name: "File Reader", Description: "Read file", InputSchema: map[string]string{"path": "string"}, OutputSchema: map[string]string{"content": "string"}, Permissions: []string{"fs:read"}, SafetyLevel: "medium", CreatedBy: "system"},
		{ID: "tool_file_write", Name: "File Writer", Description: "Write file", InputSchema: map[string]string{"path": "string", "content": "string"}, OutputSchema: map[string]string{"written": "int"}, Permissions: []string{"fs:write"}, SafetyLevel: "high", CreatedBy: "system"},
		{ID: "tool_ls", Name: "List Directory", Description: "List dir", InputSchema: map[string]string{"path": "string"}, OutputSchema: map[string]string{"entries": "string[]"}, Permissions: []string{"fs:read"}, SafetyLevel: "low", CreatedBy: "system"},
		{ID: "tool_exec", Name: "Shell Exec", Description: "Run shell command (sandboxed)", InputSchema: map[string]string{"cmd": "string"}, OutputSchema: map[string]string{"stdout": "string", "stderr": "string", "exit_code": "int"}, Permissions: []string{"proc:exec"}, SafetyLevel: "high", CreatedBy: "system"},
		{ID: "tool_docker_list", Name: "Docker List", Description: "List docker entities", InputSchema: map[string]string{"type": "string"}, OutputSchema: map[string]string{"items": "string[]"}, Permissions: []string{"docker"}, SafetyLevel: "medium", CreatedBy: "system"},
		{ID: "tool_codegen", Name: "Codegen", Description: "Generate code via LLM", InputSchema: map[string]string{"spec": "string"}, OutputSchema: map[string]string{"code": "string"}, Permissions: []string{"llm"}, SafetyLevel: "medium", CreatedBy: "system"},
		{ID: "tool_docker_build", Name: "Docker Build", Description: "Build tool image", InputSchema: map[string]string{"path": "string"}, OutputSchema: map[string]string{"image": "string"}, Permissions: []string{"docker"}, SafetyLevel: "medium", CreatedBy: "system"},
		{ID: "tool_register", Name: "Register Tool", Description: "Register tool metadata", InputSchema: map[string]string{"tool": "json"}, OutputSchema: map[string]string{"ok": "bool"}, Permissions: []string{"registry:write"}, SafetyLevel: "low", CreatedBy: "system"},
		{ID: "tool_json_parse", Name: "JSON Parse", Description: "Parse JSON", InputSchema: map[string]string{"text": "string"}, OutputSchema: map[string]string{"object": "json"}, Permissions: []string{}, SafetyLevel: "low", CreatedBy: "system"},
		{ID: "tool_text_search", Name: "Text Search", Description: "Search text", InputSchema: map[string]string{"pattern": "string", "text": "string"}, OutputSchema: map[string]string{"matches": "string[]"}, Permissions: []string{}, SafetyLevel: "low", CreatedBy: "system"},
	}

	// Add Drone executor only when explicitly enabled or on ARM64
	execMethod := strings.TrimSpace(os.Getenv("EXECUTION_METHOD"))
	if execMethod == "drone" || os.Getenv("ENABLE_ARM64_TOOLS") == "true" || runtime.GOARCH == "arm64" || runtime.GOARCH == "aarch64" {
		log.Printf("🔧 [BOOTSTRAP] Registering Drone/ARM64 tools (EXECUTION_METHOD=%s, ENABLE_ARM64_TOOLS=%s, GOARCH=%s)", execMethod, os.Getenv("ENABLE_ARM64_TOOLS"), runtime.GOARCH)
		arm64Tools := []Tool{
			{ID: "tool_drone_executor", Name: "Drone Executor", Description: "Execute code via Drone CI with Docker socket access", InputSchema: map[string]string{"code": "string", "language": "string", "image": "string", "environment": "json", "timeout": "int"}, OutputSchema: map[string]string{"success": "bool", "output": "string", "error": "string", "image": "string", "exit_code": "int", "duration_ms": "int"}, Permissions: []string{"drone:execute", "docker:build"}, SafetyLevel: "high", CreatedBy: "system"},
		}
		for _, t := range arm64Tools {
			log.Printf("🔧 [BOOTSTRAP] Registering ARM64 tool: %s", t.ID)
			_ = s.registerTool(ctx, t)
		}
	}

	for _, t := range defaults {
		_ = s.registerTool(ctx, t)
	}
}

// handleInvokeTool: POST /api/v1/tools/{id}/invoke
// Supports a few seed tools locally (http_get, file_read, ls); others are placeholders
func (s *APIServer) handleInvokeTool(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	startTime := time.Now()
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 4 {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "missing tool id"})
		return
	}
	id := parts[3]
	log.Printf("[HDN] /tools/%s/invoke", id)
	var params map[string]interface{}
	_ = json.NewDecoder(r.Body).Decode(&params)

	// Initialize tool call log
	toolCallLog := &ToolCallLog{
		ToolID:     id,
		Parameters: params,
		AgentID:    strings.TrimSpace(r.Header.Get("X-Agent-ID")),
		ProjectID:  strings.TrimSpace(r.Header.Get("X-Project-ID")),
		Timestamp:  startTime,
		Status:     "pending",
	}

	// Wrap the response writer to capture the response
	wrapper := &ResponseWrapper{
		ResponseWriter: w,
		statusCode:     200, // default
	}
	w = wrapper

	// Defer logging the tool call result
	defer func() {
		s.logToolCallResult(ctx, toolCallLog, wrapper, startTime)
	}()

	// Load tool metadata to enrich context and potential enforcement (best-effort)
	var meta Tool
	if val, err := s.redis.Get(ctx, s.toolKey(id)).Result(); err == nil {
		_ = json.Unmarshal([]byte(val), &meta)
	}

	// Update tool call log with metadata
	toolCallLog.ToolName = meta.Name
	toolCallLog.Permissions = meta.Permissions
	toolCallLog.SafetyLevel = meta.SafetyLevel

	// Principles: pre-execution gate (with permissive default)
	agentID := strings.TrimSpace(r.Header.Get("X-Agent-ID"))
	projectID := strings.TrimSpace(r.Header.Get("X-Project-ID"))
	principlesCtx := map[string]interface{}{
		"category":     "tool_invoke",
		"tool_id":      id,
		"permissions":  meta.Permissions,
		"safety_level": meta.SafetyLevel,
		"agent_id":     agentID,
		"project_id":   projectID,
	}
	allowed, _, _ := CheckActionWithPrinciples("invoke_tool:"+id, principlesCtx)
	if !allowed {
		toolCallLog.Status = "blocked"
		toolCallLog.Error = "blocked by principles"
		toolCallLog.Duration = time.Since(startTime).Milliseconds()
		if s.toolMetrics != nil {
			_ = s.toolMetrics.LogToolCall(ctx, toolCallLog)
		}
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "blocked by principles"})
		return
	}

	// Sandbox permission enforcement (permissive by default)
	if !permissionsAllowed(meta.Permissions) {
		toolCallLog.Status = "blocked"
		toolCallLog.Error = "permissions not allowed by sandbox"
		toolCallLog.Duration = time.Since(startTime).Milliseconds()
		if s.toolMetrics != nil {
			_ = s.toolMetrics.LogToolCall(ctx, toolCallLog)
		}
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "permissions not allowed by sandbox"})
		return
	}

	switch id {
	case "tool_http_get":
		url, _ := getString(params, "url")
		if strings.TrimSpace(url) == "" {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "url required"})
			return
		}

		// Use safe HTTP client with content filtering
		safeClient := NewSafeHTTPClient()
		content, err := safeClient.SafeGetWithContentCheck(ctx, url)
		if err != nil {
			// Log the blocked request
			toolCallLog.Status = "blocked"
			toolCallLog.Error = "content safety: " + err.Error()
			toolCallLog.Duration = time.Since(startTime).Milliseconds()
			if s.toolMetrics != nil {
				_ = s.toolMetrics.LogToolCall(ctx, toolCallLog)
			}
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "content blocked for safety", "reason": err.Error()})
			return
		}

		_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": 200, "body": content})
		return
	case "tool_file_read":
		path, _ := getString(params, "path")
		if !fileExists(path) {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "file not found"})
			return
		}
		b, err := os.ReadFile(path)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": err.Error()})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"content": string(b)})
		return
	case "tool_ls":
		dir, _ := getString(params, "path")
		entries, err := os.ReadDir(dir)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": err.Error()})
			return
		}
		names := []string{}
		for _, e := range entries {
			names = append(names, e.Name())
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"entries": names})
		return
	case "tool_exec":
		cmd, _ := getString(params, "cmd")
		if strings.TrimSpace(cmd) == "" {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "cmd required"})
			return
		}

		// Execute command safely using os/exec
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// Use bash to execute the command
		execCmd := exec.CommandContext(ctx, "bash", "-c", cmd)
		output, err := execCmd.Output()
		exitCode := 0
		var stderr string
		if err != nil {
			if exitError, ok := err.(*exec.ExitError); ok {
				exitCode = exitError.ExitCode()
				stderr = string(exitError.Stderr)
			} else {
				w.WriteHeader(http.StatusInternalServerError)
				_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": err.Error()})
				return
			}
		}

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"stdout":    string(output),
			"stderr":    stderr,
			"exit_code": exitCode,
		})
		return
	case "tool_drone_executor":
		log.Printf("🔧 [DRONE-TOOL] Starting Drone executor tool invocation")
		log.Printf("🔧 [DRONE-TOOL] Platform check: GOARCH=%s, ENABLE_ARM64_TOOLS=%s, EXECUTION_METHOD=%s", runtime.GOARCH, os.Getenv("ENABLE_ARM64_TOOLS"), os.Getenv("EXECUTION_METHOD"))

		// Enforce runtime gate: only allow when EXECUTION_METHOD=drone or on ARM64 (or explicitly enabled)
		execMethod := strings.TrimSpace(os.Getenv("EXECUTION_METHOD"))
		if !(execMethod == "drone" || os.Getenv("ENABLE_ARM64_TOOLS") == "true" || runtime.GOARCH == "arm64" || runtime.GOARCH == "aarch64") {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "Drone executor disabled on this platform", "hint": "Set EXECUTION_METHOD=drone or ENABLE_ARM64_TOOLS=true"})
			return
		}

		code, _ := getString(params, "code")
		language, _ := getString(params, "language")
		image, _ := getString(params, "image")

		log.Printf("🔧 [DRONE-TOOL] Parameters: language=%s, image=%s, code_length=%d", language, image, len(code))

		if strings.TrimSpace(code) == "" {
			log.Printf("❌ [DRONE-TOOL] No code provided")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "code required"})
			return
		}

		// Set defaults
		if language == "" {
			language = "go"
		}
		if image == "" {
			// Choose image based on language
			switch strings.ToLower(language) {
			case "go", "golang":
				image = "golang:1.21-alpine"
			case "python", "py":
				image = "python:3.11-slim"
			case "javascript", "node", "js":
				image = "node:18-slim"
			case "bash", "sh":
				image = "alpine:latest"
			default:
				image = "alpine:latest"
			}
		}
		log.Printf("🔧 [DRONE-TOOL] Using defaults: language=%s, image=%s", language, image)

		// Submit job to Drone CI (best-effort)
		log.Printf("🚀 [DRONE-TOOL] Attempting Drone CI submission")
		droneResp, err := s.submitToDroneCI(code, language, image)
		if err != nil {
			log.Printf("❌ [DRONE-TOOL] Drone CI submission failed: %v", err)
			// Continue to local execution even if submission fails
			droneResp = map[string]interface{}{"success": false, "error": "Drone CI submission failed: " + err.Error()}
		} else {
			log.Printf("✅ [DRONE-TOOL] Drone CI submission successful: %+v", droneResp)
		}

		// Additionally execute locally (SSH) to provide immediate run output
		log.Printf("🔧 [DRONE-TOOL] Attempting SSH fallback execution")
		localRun, execErr := s.fallbackSSHExecution(code, language, image)
		if execErr != nil {
			log.Printf("❌ [DRONE-TOOL] SSH execution failed: %v", execErr)
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"error":            execErr.Error(),
				"drone_submission": droneResp,
			})
			return
		}

		log.Printf("✅ [DRONE-TOOL] SSH execution successful: %+v", localRun)

		// Combine results: prefer local run output while returning drone submission metadata
		combined := map[string]interface{}{}
		for k, v := range localRun {
			combined[k] = v
		}
		combined["drone_submission"] = droneResp
		log.Printf("🔧 [DRONE-TOOL] Returning combined results: %+v", combined)
		_ = json.NewEncoder(w).Encode(combined)
		return
	default:
		// Handle wiki bootstrapper by running host binary if present
		if id == "tool_wiki_bootstrapper" {
			// Determine binary path; HDN runs from hdn/, while binary is at repo-root/bin/wiki-bootstrapper
			candidates := []string{
				filepath.Join("bin", "wiki-bootstrapper"),
				filepath.Join("..", "bin", "wiki-bootstrapper"),
			}
			bin := ""
			for _, c := range candidates {
				if fileExists(c) {
					bin = c
					break
				}
			}
			if bin == "" {
				w.WriteHeader(http.StatusNotFound)
				_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "wiki bootstrapper not built"})
				return
			}
			args := []string{}
			if v, ok := getString(params, "seeds"); ok && v != "" {
				args = append(args, "--seeds", v)
			}
			if v, ok := params["max_depth"].(float64); ok {
				args = append(args, "--max-depth", fmt.Sprintf("%d", int(v)))
			}
			if v, ok := params["max_nodes"].(float64); ok {
				args = append(args, "--max-nodes", fmt.Sprintf("%d", int(v)))
			}
			if v, ok := params["rpm"].(float64); ok {
				args = append(args, "--rpm", fmt.Sprintf("%d", int(v)))
			}
			if v, ok := params["burst"].(float64); ok {
				args = append(args, "--burst", fmt.Sprintf("%d", int(v)))
			}
			if v, ok := params["jitter_ms"].(float64); ok {
				args = append(args, "--jitter-ms", fmt.Sprintf("%d", int(v)))
			}
			if v, ok := params["min_confidence"].(float64); ok {
				args = append(args, "--min-confidence", fmt.Sprintf("%g", v))
			}
			if v, ok := getString(params, "domain"); ok && v != "" {
				args = append(args, "--domain", v)
			}
			if v, ok := getString(params, "job_id"); ok && v != "" {
				args = append(args, "--job-id", v)
			}
			if bv, ok := params["resume"].(bool); ok && bv {
				args = append(args, "--resume")
			}
			if bv, ok := params["pause"].(bool); ok && bv {
				args = append(args, "--pause")
			}

			out, err := runHostCommand(ctx, bin, args, nil)
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": err.Error(), "output": string(out)})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"output": string(out)})
			return
		}

		// Handle html_scraper by running host binary if present (no Docker)
		if id == "tool_html_scraper" {
			candidates := []string{
				filepath.Join("bin", "html-scraper"),
				filepath.Join("..", "bin", "html-scraper"),
				filepath.Join("bin", "tools", "html_scraper"),
				filepath.Join("..", "bin", "tools", "html_scraper"),
			}
			bin := ""
			for _, c := range candidates {
				if fileExists(c) {
					bin = c
					break
				}
			}
			if bin == "" {
				w.WriteHeader(http.StatusNotFound)
				_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "html scraper not built"})
				return
			}
			url, _ := getString(params, "url")
			if strings.TrimSpace(url) == "" {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "url required"})
				return
			}
			args := []string{"-url", url}
			out, err := runHostCommand(ctx, bin, args, nil)
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": err.Error(), "output": string(out)})
				return
			}
			// Try JSON first; else return raw text
			var obj interface{}
			if json.Unmarshal(out, &obj) == nil {
				_ = json.NewEncoder(w).Encode(obj)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"output": string(out)})
			return
		}

		// Generic exec-spec runner: if the tool metadata has an exec spec, run it
		// Case 1: Run a Docker image directly (image type)
		if meta.Exec != nil && strings.EqualFold(meta.Exec.Type, "image") && strings.TrimSpace(meta.Exec.Image) != "" {
			img := meta.Exec.Image

			// Prefer Drone executor path when Docker is not available (e.g., in Kubernetes)
			executionMethod := os.Getenv("EXECUTION_METHOD")
			useDrone := executionMethod == "drone" || (executionMethod == "" && !fileExists("/var/run/docker.sock"))
			if useDrone {
				code, _ := getString(params, "code")
				language, _ := getString(params, "language")
				if strings.TrimSpace(language) == "" {
					language = "bash"
				}
				if strings.TrimSpace(img) == "" {
					if v, ok := getString(params, "image"); ok && v != "" {
						img = v
					}
				}
				if strings.TrimSpace(code) == "" {
					w.WriteHeader(http.StatusBadRequest)
					_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "code required for drone execution of image tool", "hint": "provide 'code' and 'language' params"})
					return
				}

				// Submit via Drone executor (CI path) and return its response
				droneResp, err := s.submitToDroneCI(code, language, img)
				if err != nil {
					w.WriteHeader(http.StatusInternalServerError)
					_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": err.Error()})
					return
				}
				_ = json.NewEncoder(w).Encode(droneResp)
				return
			}

			// Build a tiny Python driver that executes code directly (no Docker)
			driver := "import json,sys\n" +
				"# Execute code directly without Docker - we're already in a container\n" +
				"params = {}\n" +
				"try:\n    params = json.loads(sys.stdin.read() or '{}')\nexcept Exception:\n    params = {}\n" +
				"# Get code and language from parameters\n" +
				"code = params.get('code', '')\n" +
				"language = params.get('language', 'python')\n" +
				"# Execute the code directly\n" +
				"if language.lower() in ['python', 'py']:\n" +
				"    exec(code)\n" +
				"elif language.lower() in ['bash', 'sh']:\n" +
				"    import subprocess\n" +
				"    result = subprocess.run(['bash', '-c', code], capture_output=True, text=True)\n" +
				"    print(result.stdout)\n" +
				"    if result.stderr:\n" +
				"        print(result.stderr, file=sys.stderr)\n" +
				"else:\n" +
				"    print(f'Unsupported language: {language}')\n"
			req := &DockerExecutionRequest{Language: "python", Code: driver, Timeout: 120, Environment: map[string]string{"QUIET": "1"}}
			// Pass params via stdin as JSON (we already decoded earlier; re-encode here)
			if b, err := json.Marshal(params); err == nil {
				req.Input = string(b)
			}
			resp, derr := s.dockerExecutor.ExecuteCode(ctx, req)
			if derr != nil {
				w.WriteHeader(http.StatusInternalServerError)
				_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": derr.Error()})
				return
			}
			if !resp.Success {
				w.WriteHeader(http.StatusInternalServerError)
				_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": resp.Error, "output": resp.Output})
				return
			}
			var obj interface{}
			if json.Unmarshal([]byte(resp.Output), &obj) == nil {
				_ = json.NewEncoder(w).Encode(obj)
			} else {
				_ = json.NewEncoder(w).Encode(map[string]interface{}{"output": resp.Output})
			}
			return
		}

		// Case 2: Run a command path inside Docker sandbox (cmd type)
		if meta.Exec != nil && strings.EqualFold(meta.Exec.Type, "cmd") && strings.TrimSpace(meta.Exec.Cmd) != "" {
			// If Docker socket is not available (typical in Kubernetes), prefer Drone executor
			executionMethod := os.Getenv("EXECUTION_METHOD")
			useDrone := executionMethod == "drone" || (executionMethod == "" && !fileExists("/var/run/docker.sock"))
			if useDrone {
				code, _ := getString(params, "code")
				language, _ := getString(params, "language")
				// If no code provided, allow safe host-exec for allowlisted paths under /app/bin/tools/*
				if strings.TrimSpace(code) == "" {
					if strings.HasPrefix(meta.Exec.Cmd, "/app/bin/tools/") && fileExists(meta.Exec.Cmd) {
						args := meta.Exec.Args
						for i := range args {
							args[i] = substitutePlaceholders(args[i], params)
						}
						out, err := runHostCommand(ctx, meta.Exec.Cmd, args, nil)
						if err != nil {
							w.WriteHeader(http.StatusInternalServerError)
							_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": err.Error(), "output": string(out)})
							return
						}
						var obj interface{}
						if json.Unmarshal(out, &obj) == nil {
							_ = json.NewEncoder(w).Encode(obj)
							return
						}
						_ = json.NewEncoder(w).Encode(map[string]interface{}{"output": string(out)})
						return
					}
					w.WriteHeader(http.StatusBadRequest)
					_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "code required for drone execution of cmd tool or use /app/bin/tools/*"})
					return
				}
				if strings.TrimSpace(language) == "" {
					language = "bash"
				}
				resp, err := s.submitToDroneCI(code, language, "")
				if err != nil {
					w.WriteHeader(http.StatusInternalServerError)
					_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": err.Error()})
					return
				}
				_ = json.NewEncoder(w).Encode(resp)
				return
			}

			// Docker available: run inside Docker sandbox via SimpleDockerExecutor
			cmdPath := meta.Exec.Cmd // expected to be /app/tools/<binary> inside container
			// Build args with placeholder substitution
			subbed := make([]string, 0, len(meta.Exec.Args))
			for _, a := range meta.Exec.Args {
				subbed = append(subbed, substitutePlaceholders(a, params))
			}
			// Optional stdin support
			stdinVal := ""
			if v, ok := params["stdin"].(string); ok && v != "" {
				stdinVal = v
			}
			py := "import json,subprocess,sys\n" +
				"cmd = " + jsonArrayLiteral(append([]string{cmdPath}, subbed...)) + "\n" +
				"inp = " + jsonStringLiteral(stdinVal) + "\n" +
				"p = subprocess.run(cmd, input=inp.encode('utf-8') if inp else None, capture_output=True)\n" +
				"out = p.stdout.decode('utf-8', errors='ignore')\n" +
				"err = p.stderr.decode('utf-8', errors='ignore')\n" +
				"print(out)\n"
			req := &DockerExecutionRequest{Language: "python", Code: py, Timeout: 60, Environment: map[string]string{"QUIET": "1"}}
			resp, derr := s.dockerExecutor.ExecuteCode(ctx, req)
			if derr != nil {
				w.WriteHeader(http.StatusInternalServerError)
				_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": derr.Error()})
				return
			}
			if !resp.Success {
				w.WriteHeader(http.StatusInternalServerError)
				_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": resp.Error, "output": resp.Output})
				return
			}
			var obj interface{}
			out := strings.TrimSpace(resp.Output)
			parsed := false
			if json.Unmarshal([]byte(out), &obj) == nil {
				parsed = true
			} else {
				lines := strings.Split(out, "\n")
				for i := len(lines) - 1; i >= 0; i-- {
					line := strings.TrimSpace(lines[i])
					if line == "" {
						continue
					}
					if (strings.HasPrefix(line, "{") && strings.HasSuffix(line, "}")) || (strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]")) {
						if json.Unmarshal([]byte(line), &obj) == nil {
							parsed = true
							break
						}
					}
				}
			}
			if parsed {
				_ = json.NewEncoder(w).Encode(obj)
			} else {
				_ = json.NewEncoder(w).Encode(map[string]interface{}{"output": resp.Output})
			}
			return
		}
		w.WriteHeader(http.StatusNotImplemented)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "tool not implemented"})
		return
	}

	// no-op
}

func substitutePlaceholders(s string, params map[string]interface{}) string {
	out := s
	for k, v := range params {
		vv := ""
		switch t := v.(type) {
		case string:
			vv = t
		default:
			b, _ := json.Marshal(v)
			vv = string(b)
		}
		out = strings.ReplaceAll(out, "{"+k+"}", vv)
	}
	return out
}

func runHostCommand(ctx context.Context, cmd string, args []string, stdin []byte) ([]byte, error) {
	c := exec.CommandContext(ctx, cmd, args...)
	if len(stdin) > 0 {
		c.Stdin = strings.NewReader(string(stdin))
	}
	b, err := c.CombinedOutput()
	if err != nil {
		return b, fmt.Errorf("command failed: %s %s: %w", cmd, strings.Join(args, " "), err)
	}
	return b, nil
}

func jsonArrayLiteral(parts []string) string {
	// build Python list literal safely quoted
	qs := make([]string, 0, len(parts))
	for _, p := range parts {
		qs = append(qs, jsonStringLiteral(p))
	}
	return "[" + strings.Join(qs, ",") + "]"
}

func jsonStringLiteral(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// permissionsAllowed: default-permissive policy while running inside Docker
// Set ALLOWED_TOOL_PERMS to a comma-separated list to restrict (optional).
func permissionsAllowed(perms []string) bool {
	// If env is unset, allow everything (default permissive)
	env := strings.TrimSpace(os.Getenv("ALLOWED_TOOL_PERMS"))
	if env == "" {
		return true
	}
	allowed := map[string]bool{}
	for _, p := range strings.Split(env, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			allowed[p] = true
		}
	}
	for _, p := range perms {
		if !allowed[p] {
			return false
		}
	}
	return true
}

// submitToDroneCI submits a job to Drone CI for execution
func (s *APIServer) submitToDroneCI(code, language, image string) (map[string]interface{}, error) {
	log.Printf("🚀 [DRONE-CI] Starting Drone CI submission")

	// Drone CI configuration - try multiple URLs
	droneURLs := []string{
		"http://192.168.1.63:8888",
		"http://rpi5b:8888",
		"http://localhost:8888",
	}
	log.Printf("🚀 [DRONE-CI] Will try URLs: %v", droneURLs)

	// Read token from environment for configurability
	droneToken := strings.TrimSpace(os.Getenv("DRONE_TOKEN"))
	if droneToken == "" {
		log.Printf("❌ [DRONE-CI] DRONE_TOKEN is not set")
		return nil, fmt.Errorf("DRONE_TOKEN is not set")
	}
	log.Printf("🚀 [DRONE-CI] Using token: %s...", droneToken[:8])

	// Create HTTP client with timeout
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	// Try each Drone CI URL until one works
	for i, droneURL := range droneURLs {
		log.Printf("🚀 [DRONE-CI] Trying URL %d/%d: %s", i+1, len(droneURLs), droneURL)

		// Use configurable repository instead of hardcoding
		existingRepo := strings.TrimSpace(os.Getenv("DRONE_REPO"))
		if existingRepo == "" {
			existingRepo = "stevef/agi" // fallback default
		}
		log.Printf("🚀 [DRONE-CI] Using repository: %s", existingRepo)

		// Step 1: Trigger a build on the existing repository
		buildURL := fmt.Sprintf("%s/api/repos/%s/builds", droneURL, existingRepo)
		log.Printf("🚀 [DRONE-CI] Making POST request to: %s", buildURL)

		buildReq, err := http.NewRequest("POST", buildURL, nil)
		if err != nil {
			log.Printf("❌ [DRONE-CI] Failed to create request for %s: %v", droneURL, err)
			continue
		}
		buildReq.Header.Set("Authorization", "Bearer "+droneToken)

		// Submit the build
		buildResp, err := client.Do(buildReq)
		if err != nil {
			log.Printf("❌ [DRONE-CI] Request failed for %s: %v", droneURL, err)
			continue
		}
		defer buildResp.Body.Close()

		log.Printf("🚀 [DRONE-CI] Response status from %s: %d", droneURL, buildResp.StatusCode)

		if buildResp.StatusCode != http.StatusOK {
			log.Printf("❌ [DRONE-CI] Non-OK status from %s: %d", droneURL, buildResp.StatusCode)
			continue
		}

		// Parse the response to get build details
		var buildResponse map[string]interface{}
		if err := json.NewDecoder(buildResp.Body).Decode(&buildResponse); err != nil {
			log.Printf("❌ [DRONE-CI] Failed to parse response from %s: %v", droneURL, err)
			continue
		}

		log.Printf("✅ [DRONE-CI] Success! Build response: %+v", buildResponse)

		// Success! We found a working Drone CI URL
		result := map[string]interface{}{
			"success":      true,
			"output":       "Job submitted to Drone CI successfully",
			"error":        "",
			"image":        image,
			"exit_code":    0,
			"duration_ms":  0,
			"method":       "drone_ci_submission",
			"repo_name":    existingRepo,
			"drone_url":    droneURL,
			"build_id":     buildResponse["id"],
			"build_number": buildResponse["number"],
		}
		log.Printf("✅ [DRONE-CI] Returning success result: %+v", result)
		return result, nil
	}

	// If we get here, all Drone CI URLs failed
	// Fallback to SSH execution on RPI host
	return s.fallbackSSHExecution(code, language, image)
}

// fallbackSSHExecution executes code on RPI host via SSH
func (s *APIServer) fallbackSSHExecution(code, language, image string) (map[string]interface{}, error) {
	log.Printf("🔧 [SSH-FALLBACK] Starting SSH fallback execution")

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Get RPI host from environment or use default
	rpiHost := os.Getenv("RPI_HOST")
	if rpiHost == "" {
		rpiHost = "192.168.1.63" // Default RPI host
	}
	log.Printf("🔧 [SSH-FALLBACK] Using RPI host: %s", rpiHost)

	// Create temporary file on RPI host under $HOME to support rootless Docker bind mounts
	tempFile := fmt.Sprintf("/home/pi/.hdn/tmp/drone_code_%d.%s", time.Now().UnixNano(), getFileExtension(language))
	log.Printf("🔧 [SSH-FALLBACK] Creating temp file: %s", tempFile)

	// Write code to temporary file on RPI via SSH
	writeCmd := fmt.Sprintf("mkdir -p $(dirname %s) && cat > %s << 'EOF'\n%s\nEOF", tempFile, tempFile, code)
	log.Printf("🔧 [SSH-FALLBACK] Writing code via SSH command: %s", writeCmd[:100]+"...")

	sshCmd := exec.CommandContext(ctx, "ssh", "-o", "StrictHostKeyChecking=no", "-o", "UserKnownHostsFile=/dev/null",
		"pi@"+rpiHost, writeCmd)

	log.Printf("🔧 [SSH-FALLBACK] Executing SSH write command")
	if err := sshCmd.Run(); err != nil {
		log.Printf("❌ [SSH-FALLBACK] Failed to write code file via SSH: %v", err)
		return nil, fmt.Errorf("failed to write code file via SSH: %v", err)
	}
	log.Printf("✅ [SSH-FALLBACK] Code file written successfully")

	// Build host execution command (no Docker)
	log.Printf("🔧 [SSH-FALLBACK] Building host execution command for language: %s", language)
	var execCmd *exec.Cmd

	switch language {
	case "go":
		// Execute Go directly on the RPI host without Docker
		goHostCmd := fmt.Sprintf(`set -euo pipefail
	WORK="$(mktemp -d /home/pi/.hdn/go_tmp_XXXXXX)"
	mkdir -p "$WORK"
	cp %s "$WORK"/main.go
	cd "$WORK"
	# Ensure Go is on PATH for non-interactive SSH shells
	export PATH="$PATH:/usr/local/go/bin:/home/pi/go/bin:/usr/local/bin:/usr/bin"
	if ! command -v go >/dev/null 2>&1; then echo 'go not installed on host' >&2; exit 127; fi
	if ! ls go.mod >/dev/null 2>&1; then go mod init tmpmod >/dev/null 2>&1 || true; fi
	GOFLAGS= go build -o app ./main.go
	./app
	`, tempFile)
		execCmd = exec.CommandContext(ctx, "ssh", "-o", "StrictHostKeyChecking=no", "-o", "UserKnownHostsFile=/dev/null",
			"pi@"+rpiHost, "bash", "-lc", goHostCmd)

	case "python":
		// Execute Python directly on the host in a venv; install detected packages; run the script
		pkgs := detectPythonPackagesForDocker(code)
		pkgLine := ""
		if len(pkgs) > 0 {
			pkgLine = fmt.Sprintf("pip install %s && ", strings.Join(pkgs, " "))
		}
		hostCmd := fmt.Sprintf(`set -euo pipefail
VENV="/home/pi/.hdn/venv"
python3 -m venv "$VENV" >/dev/null 2>&1 || true
. "$VENV"/bin/activate
python -m pip install --upgrade pip >/dev/null 2>&1 || true
%spython %s`, pkgLine, tempFile)
		execCmd = exec.CommandContext(ctx, "ssh", "-o", "StrictHostKeyChecking=no", "-o", "UserKnownHostsFile=/dev/null",
			"pi@"+rpiHost, "bash", "-lc", hostCmd)

	case "bash":
		// Run shell script directly on the host
		bashHostCmd := fmt.Sprintf("set -euo pipefail\nsh %s\n", tempFile)
		execCmd = exec.CommandContext(ctx, "ssh", "-o", "StrictHostKeyChecking=no", "-o", "UserKnownHostsFile=/dev/null",
			"pi@"+rpiHost, "bash", "-lc", bashHostCmd)

	case "java":
		// Execute Java directly on the host using system JDK
		javaHostCmd := fmt.Sprintf(`set -euo pipefail
WORK="$(mktemp -d /home/pi/.hdn/java_tmp_XXXXXX)"
mkdir -p "$WORK"
cp %s "$WORK"/Main.java || cp %s "$WORK"/App.java || true
cd "$WORK"
if ! command -v javac >/dev/null 2>&1; then echo 'javac not installed on host' >&2; exit 127; fi
SRC=Main.java; [ -f App.java ] && SRC=App.java
javac "$SRC"
MAIN=${SRC%%.java}
java "$MAIN"
`, tempFile, tempFile)
		execCmd = exec.CommandContext(ctx, "ssh", "-o", "StrictHostKeyChecking=no", "-o", "UserKnownHostsFile=/dev/null",
			"pi@"+rpiHost, "bash", "-lc", javaHostCmd)

	default:
		// Fallback: run as a shell command directly on host
		execCmd = exec.CommandContext(ctx, "ssh", "-o", "StrictHostKeyChecking=no", "-o", "UserKnownHostsFile=/dev/null",
			"pi@"+rpiHost, "bash", "-lc", code)
	}

	log.Printf("🔧 [SSH-FALLBACK] Executing host command via SSH")
	startTime := time.Now()
	output, err := execCmd.Output()
	duration := time.Since(startTime)
	log.Printf("🔧 [SSH-FALLBACK] Host execution completed in %v", duration)

	exitCode := 0
	var stderr string
	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			exitCode = exitError.ExitCode()
			stderr = string(exitError.Stderr)
			log.Printf("❌ [SSH-FALLBACK] Command failed with exit code %d: %s", exitCode, stderr)
		} else {
			log.Printf("❌ [SSH-FALLBACK] SSH execution failed: %v", err)
			return nil, fmt.Errorf("SSH execution failed: %v", err)
		}
	} else {
		log.Printf("✅ [SSH-FALLBACK] Command executed successfully")
	}

	log.Printf("🔧 [SSH-FALLBACK] Output: %s", string(output))
	if stderr != "" {
		log.Printf("🔧 [SSH-FALLBACK] Stderr: %s", stderr)
	}

	// Clean up temporary file
	log.Printf("🔧 [SSH-FALLBACK] Cleaning up temp file: %s", tempFile)
	cleanupCmd := exec.CommandContext(ctx, "ssh", "-o", "StrictHostKeyChecking=no", "-o", "UserKnownHostsFile=/dev/null",
		"pi@"+rpiHost, "rm", "-f", tempFile)
	cleanupCmd.Run() // Best effort cleanup

	result := map[string]interface{}{
		"success":     exitCode == 0,
		"output":      string(output),
		"error":       stderr,
		"image":       image,
		"exit_code":   exitCode,
		"duration_ms": duration.Milliseconds(),
		"method":      "ssh_docker_execution",
		"host":        rpiHost,
	}
	log.Printf("✅ [SSH-FALLBACK] Returning result: %+v", result)
	return result, nil
}

// fallbackDockerExecution provides a fallback when Drone CI is unavailable
func (s *APIServer) fallbackDockerExecution(code, language, image string) (map[string]interface{}, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create temporary file
	tempFile := fmt.Sprintf("/tmp/drone_code_%d.%s", time.Now().UnixNano(), getFileExtension(language))
	err := os.WriteFile(tempFile, []byte(code), 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to write code file: %v", err)
	}
	defer os.Remove(tempFile)

	// Execute using Docker (this will work if Docker is available)
	var cmd *exec.Cmd
	switch language {
	case "go":
		cmd = exec.CommandContext(ctx, "docker", "run", "--rm", "-v", tempFile+":/code.go", image, "go", "run", "/code.go")
	case "python":
		cmd = exec.CommandContext(ctx, "docker", "run", "--rm", "-v", tempFile+":/code.py", image, "python", "/code.py")
	case "bash":
		cmd = exec.CommandContext(ctx, "docker", "run", "--rm", "-v", tempFile+":/code.sh", image, "sh", "/code.sh")
	default:
		cmd = exec.CommandContext(ctx, "docker", "run", "--rm", "-v", tempFile+":/code", image, "sh", "-c", code)
	}

	startTime := time.Now()
	output, err := cmd.Output()
	duration := time.Since(startTime)

	exitCode := 0
	var stderr string
	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			exitCode = exitError.ExitCode()
			stderr = string(exitError.Stderr)
		} else {
			return nil, fmt.Errorf("execution failed: %v", err)
		}
	}

	return map[string]interface{}{
		"success":     exitCode == 0,
		"output":      string(output),
		"error":       stderr,
		"image":       image,
		"exit_code":   exitCode,
		"duration_ms": duration.Milliseconds(),
		"method":      "fallback_docker_execution",
	}, nil
}

// generateDroneCommands generates the commands for Drone CI execution
func (s *APIServer) generateDroneCommands(code, language string) []string {
	switch language {
	case "go":
		return []string{
			"echo 'package main' > main.go",
			"echo 'import \"fmt\"' >> main.go",
			"echo 'func main() {' >> main.go",
			fmt.Sprintf("echo '%s' >> main.go", strings.ReplaceAll(code, "'", "\\'")),
			"echo '}' >> main.go",
			"go run main.go",
		}
	case "python":
		return []string{
			fmt.Sprintf("echo '%s' > main.py", strings.ReplaceAll(code, "'", "\\'")),
			"python main.py",
		}
	case "bash":
		return []string{
			fmt.Sprintf("echo '%s' > script.sh", strings.ReplaceAll(code, "'", "\\'")),
			"chmod +x script.sh",
			"./script.sh",
		}
	default:
		return []string{
			fmt.Sprintf("echo '%s' > code", strings.ReplaceAll(code, "'", "\\'")),
			"sh code",
		}
	}
}
