package interpreter

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

// InterpreterAPI handles HTTP requests for natural language interpretation
type InterpreterAPI struct {
	interpreter *Interpreter
}

// NewInterpreterAPI creates a new interpreter API handler
func NewInterpreterAPI(interpreter *Interpreter) *InterpreterAPI {
	return &InterpreterAPI{
		interpreter: interpreter,
	}
}

// HandleInterpretRequest handles natural language interpretation requests
func (api *InterpreterAPI) HandleInterpretRequest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req NaturalLanguageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Invalid JSON",
			"details": err.Error(),
		})
		return
	}

	// Validate required fields
	if req.Input == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Input is required",
		})
		return
	}

	// Set defaults
	if req.Context == nil {
		req.Context = make(map[string]string)
	}
	if req.SessionID == "" {
		req.SessionID = fmt.Sprintf("session_%d", time.Now().UnixNano())
	}

	// Determine priority: LOW for background tasks (FSM, etc.), HIGH for user requests
	// Check if request comes from FSM or other background services
	isBackgroundTask := false
	if origin, ok := req.Context["origin"]; ok && (origin == "fsm" || origin == "background") {
		isBackgroundTask = true
		log.Printf("🔵 [INTERPRETER-API] Detected background task (origin: %s), using LOW priority", origin)
	}

	// Check if this is a complex request that should be processed asynchronously
	isComplex := api.isComplexRequest(req.Input)

	if isComplex {
		// Process asynchronously for complex requests
		log.Printf("🔄 [INTERPRETER-API] Processing complex request asynchronously: %s", req.Input[:min(100, len(req.Input))])

		// Return immediate response
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success":    true,
			"message":    "Complex request accepted for asynchronous processing",
			"session_id": req.SessionID,
			"status":     "processing",
		})

		// Process in background
		go func() {
			ctx := context.Background()
			result, err := api.interpreter.InterpretWithPriority(ctx, &req, !isBackgroundTask)
			if err != nil {
				log.Printf("❌ [INTERPRETER-API] Async interpretation failed: %v", err)
			} else {
				log.Printf("✅ [INTERPRETER-API] Async interpretation completed: %d tasks generated", len(result.Tasks))
			}
		}()

		return
	}

	// Process simple requests synchronously with timeout
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	result, err := api.interpreter.InterpretWithPriority(ctx, &req, !isBackgroundTask)
	if err != nil {
		log.Printf("❌ [INTERPRETER-API] Interpretation failed: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Interpretation failed",
			"details": fmt.Sprintf("%v", err),
		})
		return
	}

	// Return the result
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// isComplexRequest determines if a request is complex and should be processed asynchronously
func (api *InterpreterAPI) isComplexRequest(input string) bool {
	lowerInput := strings.ToLower(input)

	// Exclude goal-related requests from async processing
	goalPatterns := []string{
		"goals suggest next steps",
		"suggest next steps",
		"next steps",
		"goal suggestion",
		"what should i do next",
		"recommend next action",
		"propose next steps",
		"goals suggest",
		"suggest",
		"goals",
		"next",
		"steps",
		"recommend",
		"propose",
		"action",
		"what should",
		"what to do",
		"what can i do",
		"help me",
		"advice",
		"guidance",
	}

	for _, pattern := range goalPatterns {
		if strings.Contains(lowerInput, pattern) {
			return false // Process goal requests synchronously
		}
	}

	// Check for complex request patterns
	complexPatterns := []string{
		"compare.*performance",
		"build.*docker.*container",
		"multiple.*programs",
		"create.*and.*save",
		"generate.*and.*compare",
		"first.*1000.*prime",
		"calculate.*first.*1000",
		"performance.*comparison",
		"time.*comparison",
		"benchmark",
		"measure.*time",
		"execution.*time",
	}

	for _, pattern := range complexPatterns {
		if strings.Contains(lowerInput, pattern) {
			return true
		}
	}

	// Also check for long requests (more than 200 characters) but not for goal requests
	return len(input) > 200
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// HandleInterpretAndExecute handles natural language input with immediate execution
func (api *InterpreterAPI) HandleInterpretAndExecute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req NaturalLanguageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Invalid JSON",
			"details": err.Error(),
		})
		return
	}

	// Validate required fields
	if req.Input == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Input is required",
		})
		return
	}

	// Set defaults
	if req.Context == nil {
		req.Context = make(map[string]string)
	}
	if req.SessionID == "" {
		req.SessionID = fmt.Sprintf("session_%d", time.Now().UnixNano())
	}

	// Process the request
	ctx := r.Context()
	result, err := api.interpreter.Interpret(ctx, &req)
	if err != nil {
		log.Printf("❌ [INTERPRETER-API] Interpretation failed: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Interpretation failed",
			"details": fmt.Sprintf("%v", err),
		})
		return
	}

	if !result.Success {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   result.Message,
		})
		return
	}

	// Create execution results
	executionResults := make([]TaskExecutionResult, len(result.Tasks))

	for i, task := range result.Tasks {
		executionResults[i] = TaskExecutionResult{
			Task:       task,
			Success:    false, // Will be updated by actual execution
			Result:     "",
			Error:      "",
			ExecutedAt: time.Now(),
		}
	}

	// Return the interpretation and execution plan
	response := InterpretAndExecuteResponse{
		Success:        true,
		Interpretation: result,
		ExecutionPlan:  executionResults,
		Message:        fmt.Sprintf("Interpreted %d task(s) ready for execution", len(result.Tasks)),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// TaskExecutionResult represents the result of executing an interpreted task
type TaskExecutionResult struct {
	Task       InterpretedTask `json:"task"`
	Success    bool            `json:"success"`
	Result     string          `json:"result"`
	Error      string          `json:"error"`
	ExecutedAt time.Time       `json:"executed_at"`
}

// InterpretAndExecuteResponse represents the response for interpret and execute requests
type InterpretAndExecuteResponse struct {
	Success        bool                  `json:"success"`
	Interpretation *InterpretationResult `json:"interpretation"`
	ExecutionPlan  []TaskExecutionResult `json:"execution_plan"`
	Message        string                `json:"message"`
}
