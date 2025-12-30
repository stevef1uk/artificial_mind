package interpreter

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"
)

// FlexibleInterpretationResult represents the result of flexible interpretation
type FlexibleInterpretationResult struct {
	Success       bool         `json:"success"`
	ResponseType  ResponseType `json:"response_type"`
	Message       string       `json:"message"`
	SessionID     string       `json:"session_id"`
	InterpretedAt time.Time    `json:"interpreted_at"`

	// Response-specific data
	ToolCall       *ToolCall        `json:"tool_call,omitempty"`
	CodeArtifact   *CodeArtifact    `json:"code_artifact,omitempty"`
	StructuredTask *InterpretedTask `json:"structured_task,omitempty"`
	TextResponse   string           `json:"text_response,omitempty"`

	// Execution results
	ToolExecutionResult *ToolExecutionResult   `json:"tool_execution_result,omitempty"`
	Metadata            map[string]interface{} `json:"metadata,omitempty"`
}

// ToolExecutionResult represents the result of tool execution
type ToolExecutionResult struct {
	Success bool        `json:"success"`
	Result  interface{} `json:"result,omitempty"`
	Error   string      `json:"error,omitempty"`
}

// FlexibleInterpreter handles flexible natural language processing
type FlexibleInterpreter struct {
	llmAdapter   *FlexibleLLMAdapter
	toolProvider ToolProviderInterface
	recentToolCalls map[string]time.Time // Loop protection: track recent tool calls
}

// NewFlexibleInterpreter creates a new flexible interpreter
func NewFlexibleInterpreter(llmAdapter *FlexibleLLMAdapter, toolProvider ToolProviderInterface) *FlexibleInterpreter {
	return &FlexibleInterpreter{
		llmAdapter:     llmAdapter,
		toolProvider:   toolProvider,
		recentToolCalls: make(map[string]time.Time),
	}
}

// Interpret processes natural language input using flexible interpretation
// Uses high priority by default (assumes user requests unless context indicates otherwise)
func (f *FlexibleInterpreter) Interpret(ctx context.Context, req *NaturalLanguageRequest) (*FlexibleInterpretationResult, error) {
	return f.InterpretWithPriority(ctx, req, true) // Default to high priority for user requests
}

// InterpretWithPriority processes natural language input with specified priority
func (f *FlexibleInterpreter) InterpretWithPriority(ctx context.Context, req *NaturalLanguageRequest, highPriority bool) (*FlexibleInterpretationResult, error) {
	log.Printf("🧠 [FLEXIBLE-INTERPRETER] Processing natural language input: %s (priority: %v)", req.Input, highPriority)

	// Get available tools
	tools, err := f.toolProvider.GetAvailableTools(ctx)
	if err != nil {
		log.Printf("⚠️ [FLEXIBLE-INTERPRETER] Failed to get tools: %v", err)
		log.Printf("⚠️ [FLEXIBLE-INTERPRETER] Continuing with empty tools list - LLM will generate code instead of using tools")
		tools = []Tool{} // Continue with empty tools list
	} else if len(tools) == 0 {
		log.Printf("⚠️ [FLEXIBLE-INTERPRETER] No tools available - LLM will generate code instead of using tools")
	} else {
		log.Printf("✅ [FLEXIBLE-INTERPRETER] Retrieved %d tools for LLM to use", len(tools))
	}

	// Process with flexible LLM using priority
	response, err := f.llmAdapter.ProcessNaturalLanguageWithPriority(req.Input, tools, highPriority)
	if err != nil {
		return &FlexibleInterpretationResult{
			Success:   false,
			Message:   fmt.Sprintf("Failed to process input: %v", err),
			SessionID: req.SessionID,
		}, err
	}

	// Create result based on response type
	result := &FlexibleInterpretationResult{
		Success:       true,
		ResponseType:  response.Type,
		Message:       f.getMessageForResponseType(response),
		SessionID:     req.SessionID,
		InterpretedAt: time.Now(),
	}

	// Set response-specific data
	switch response.Type {
	case ResponseTypeToolCall:
		if response.ToolCall == nil {
			log.Printf("❌ [FLEXIBLE-INTERPRETER] Response type is tool_call but ToolCall is nil in response!")
		} else {
			log.Printf("🔧 [FLEXIBLE-INTERPRETER] Parsed tool call: %s", response.ToolCall.ToolID)
		}
		result.ToolCall = response.ToolCall
		if response.ToolCall != nil {
			result.Message = fmt.Sprintf("Tool call: %s", response.ToolCall.ToolID)
		} else {
			result.Message = "Tool call requested but ToolCall is nil"
		}
	case ResponseTypeCodeArtifact:
		result.CodeArtifact = response.CodeArtifact
		result.Message = fmt.Sprintf("Code artifact generated: %s", response.CodeArtifact.Language)
		// Heuristic: flag only non-trivial, generally-useful code as tool candidates
		if result.Metadata == nil {
			result.Metadata = map[string]interface{}{}
		}
		if isNonTrivialGenericUtility(response.CodeArtifact.Language, response.CodeArtifact.Code) {
			result.Metadata["tool_candidate"] = true
			// Provide a proposed tool spec for downstream registration (no side effects here)
			id := proposeToolID(response.CodeArtifact.Language, response.CodeArtifact.Code)
			// Wrap the generated code with a small driver that reads JSON text from stdin
			driver := "" +
				response.CodeArtifact.Code + "\n" +
				"import sys, json\n" +
				"if __name__ == '__main__':\n" +
				"    data = sys.argv[1] if len(sys.argv) > 1 else sys.stdin.read()\n" +
				"    try:\n" +
				"        # If the generated code defines extract_keys_from_json(json_string), use it\n" +
				"        f = globals().get('extract_keys_from_json')\n" +
				"        if callable(f):\n" +
				"            out = f(data)\n" +
				"        else:\n" +
				"            out = {'ok': True}\n" +
				"        # If function returned a JSON string, decode to native so we print arrays/objects\n" +
				"        if isinstance(out, str):\n" +
				"            try:\n" +
				"                decoded = json.loads(out)\n" +
				"                out = decoded\n" +
				"            except Exception:\n" +
				"                pass\n" +
				"    except Exception as e:\n" +
				"        out = {'error': str(e)}\n" +
				"    print(json.dumps(out))\n"

			// Generate a descriptive name and description from the code
			// Extract function/class names or key operations to create a better description
			description := generateToolDescriptionFromCode(response.CodeArtifact.Language, response.CodeArtifact.Code)
			
			result.Metadata["proposed_tool"] = map[string]interface{}{
				"id":           id,
				"name":         id,
				"description":  description,
				"permissions":  []string{"proc:exec"},
				"safety_level": "medium",
				"created_by":   "agent",
				// Provide minimal IO contract for the Monitor and API
				"input_schema":  map[string]string{"stdin": "string"},
				"output_schema": map[string]string{"output": "json"},
				"exec": map[string]interface{}{
					"type": "cmd",
					"cmd":  "python",
					"args": []string{"-c", driver, "{stdin}"},
				},
			}
		} else {
			result.Metadata["tool_candidate"] = false
		}
	case ResponseTypeStructuredTask:
		result.StructuredTask = response.StructuredTask
		result.Message = fmt.Sprintf("Structured task: %s", response.StructuredTask.TaskName)
	case ResponseTypeText:
		result.TextResponse = response.Content
		result.Message = "Text response provided"
	}

	log.Printf("✅ [FLEXIBLE-INTERPRETER] Successfully processed input with response type: %s", response.Type)
	return result, nil
}

// InterpretAndExecute processes and executes natural language input
// Uses high priority by default (assumes user requests)
func (f *FlexibleInterpreter) InterpretAndExecute(ctx context.Context, req *NaturalLanguageRequest) (*FlexibleInterpretationResult, error) {
	return f.InterpretAndExecuteWithPriority(ctx, req, true) // Default to high priority for user requests
}

// InterpretAndExecuteWithPriority processes and executes with specified priority
func (f *FlexibleInterpreter) InterpretAndExecuteWithPriority(ctx context.Context, req *NaturalLanguageRequest, highPriority bool) (*FlexibleInterpretationResult, error) {
	// First interpret with priority
	result, err := f.InterpretWithPriority(ctx, req, highPriority)
	if err != nil {
		return result, err
	}

	// If it's a tool call, validate and execute it
	log.Printf("🔍 [FLEXIBLE-INTERPRETER] InterpretAndExecuteWithPriority: checking for tool call. result.ToolCall=%v, result.ResponseType=%s", result.ToolCall != nil, result.ResponseType)
	
	// CRITICAL: Refuse to execute tools for scoring requests - they should return text/JSON only
	if isScoringRequest(req.Input) && result.ToolCall != nil {
		log.Printf("🚫 [FLEXIBLE-INTERPRETER] BLOCKED tool execution for scoring request - forcing text response")
		result.ToolCall = nil
		result.ResponseType = ResponseTypeText
		if result.TextResponse == "" {
			result.TextResponse = `{"error": "Tool execution blocked for scoring request. Expected text/JSON response only."}`
		}
		result.Message = "Scoring request - tool execution blocked, text response expected"
		return result, nil
	}
	
	if result.ToolCall != nil {
		// Loop protection: Check for rapid repeated identical tool calls
		toolCallKey := fmt.Sprintf("%s:%v", result.ToolCall.ToolID, result.ToolCall.Parameters)
		now := time.Now()
		
		// Check if we've seen this exact tool call recently (within 2 seconds)
		if lastSeen, exists := f.recentToolCalls[toolCallKey]; exists {
			if now.Sub(lastSeen) < 2*time.Second {
				log.Printf("⚠️ [FLEXIBLE-INTERPRETER] Loop protection: Tool call '%s' with same parameters executed recently (%.2fs ago), skipping to prevent loop", result.ToolCall.ToolID, now.Sub(lastSeen).Seconds())
				result.ToolExecutionResult = &ToolExecutionResult{
					Success: false,
					Error:   fmt.Sprintf("Tool call '%s' executed too recently, possible loop detected", result.ToolCall.ToolID),
				}
				result.Message = result.ToolExecutionResult.Error
				result.Success = false
				return result, nil
			}
		}
		
		// Update the recent tool calls map
		f.recentToolCalls[toolCallKey] = now
		
		// Clean up old entries (older than 1 minute for better memory management)
		for key, timestamp := range f.recentToolCalls {
			if now.Sub(timestamp) > 1*time.Minute {
				delete(f.recentToolCalls, key)
			}
		}
		
		log.Printf("🔧 [FLEXIBLE-INTERPRETER] Executing tool: %s with parameters: %+v", result.ToolCall.ToolID, result.ToolCall.Parameters)
		// Validate the tool ID against available tools to avoid invoking non-existent endpoints
		tools, terr := f.toolProvider.GetAvailableTools(ctx)
		if terr == nil && len(tools) > 0 {
			valid := false
			for _, t := range tools {
				if strings.TrimSpace(strings.ToLower(t.ID)) == strings.TrimSpace(strings.ToLower(result.ToolCall.ToolID)) {
					valid = true
					break
				}
			}
			if !valid {
				// Build a concise list of valid IDs for guidance
				var ids []string
				for _, t := range tools {
					ids = append(ids, t.ID)
				}
				log.Printf("❌ [FLEXIBLE-INTERPRETER] Invalid tool_id '%s'; must be one of: %s", result.ToolCall.ToolID, strings.Join(ids, ", "))
				result.ToolExecutionResult = &ToolExecutionResult{
					Success: false,
					Error:   fmt.Sprintf("invalid tool_id '%s'; must be one of: %s", result.ToolCall.ToolID, strings.Join(ids, ", ")),
				}
				result.Message = result.ToolExecutionResult.Error
				result.Success = false
				return result, nil
			}
		}
		executionResult, err := f.toolProvider.ExecuteTool(ctx, result.ToolCall.ToolID, result.ToolCall.Parameters)
		if err != nil {
			log.Printf("❌ [FLEXIBLE-INTERPRETER] Tool execution failed: %v", err)
			result.ToolExecutionResult = &ToolExecutionResult{
				Success: false,
				Error:   err.Error(),
			}
			result.Message = fmt.Sprintf("Tool execution failed: %v", err)
		} else {
			log.Printf("✅ [FLEXIBLE-INTERPRETER] Tool %s executed successfully", result.ToolCall.ToolID)
			result.ToolExecutionResult = &ToolExecutionResult{
				Success: true,
				Result:  executionResult,
			}
			result.Message = "Tool executed successfully"
		}
	} else {
		// For scoring requests, text response without tool call is expected and correct
		if isScoringRequest(req.Input) {
			log.Printf("✅ [FLEXIBLE-INTERPRETER] Text response for scoring request (no tool call expected)")
		} else {
			log.Printf("⚠️ [FLEXIBLE-INTERPRETER] Response type is %s but ToolCall is nil", result.ResponseType)
		}
	}

	return result, nil
}

// getMessageForResponseType generates an appropriate message for the response type
func (f *FlexibleInterpreter) getMessageForResponseType(response *FlexibleLLMResponse) string {
	switch response.Type {
	case ResponseTypeToolCall:
		return fmt.Sprintf("Selected tool: %s", response.ToolCall.ToolID)
	case ResponseTypeCodeArtifact:
		return fmt.Sprintf("Generated %s code", response.CodeArtifact.Language)
	case ResponseTypeStructuredTask:
		return fmt.Sprintf("Created task: %s", response.StructuredTask.TaskName)
	case ResponseTypeText:
		return "Provided text response"
	default:
		return "Processed input successfully"
	}
}

// isNonTrivialGenericUtility returns true when code looks reusable and not trivial
func isNonTrivialGenericUtility(language, code string) bool {
	c := strings.TrimSpace(code)
	// Relax threshold so the system proposes tools more often
	if len(c) < 60 { // allow shorter but still non-trivial snippets
		return false
	}
	lower := strings.ToLower(c)
	// Skip obvious trivial prints/echoes/no-ops without structure
	if strings.Contains(lower, "print(") && !strings.Contains(lower, "def ") {
		return false
	}
	if strings.Contains(lower, "console.log(") && !strings.Contains(lower, "function ") {
		return false
	}
	// Positive signals of generic utility
	signals := []string{
		"parse", "extract", "transform", "normalize", "validate", "serialize", "client", "retry", "cache",
		"http", "json", "csv", "yaml", "xml", "regex", "scrape", "render", "template",
		"cli", "library", "module", "package",
	}
	score := 0
	for _, s := range signals {
		if strings.Contains(lower, s) {
			score++
		}
	}
	// Require at least some structure
	hasStructure := strings.Contains(lower, "def ") || strings.Contains(lower, "function ") || strings.Contains(lower, "class ") || strings.Contains(lower, "package ")
	// More permissive: one signal is enough if structure is present
	return hasStructure && score >= 1
}

// generateToolDescriptionFromCode creates a descriptive description from code
func generateToolDescriptionFromCode(language, code string) string {
	c := strings.TrimSpace(code)
	if len(c) == 0 {
		return "Auto-proposed utility from code artifact"
	}
	
	lower := strings.ToLower(c)
	
	// Try to extract function/class names
	var funcNames []string
	if language == "python" {
		// Look for def statements
		lines := strings.Split(c, "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "def ") {
				parts := strings.Fields(line)
				if len(parts) > 1 {
					funcName := strings.Split(parts[1], "(")[0]
					funcNames = append(funcNames, funcName)
				}
			}
		}
	} else if language == "javascript" || language == "js" {
		// Look for function declarations
		lines := strings.Split(c, "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if strings.Contains(line, "function ") {
				parts := strings.Fields(line)
				for i, part := range parts {
					if part == "function" && i+1 < len(parts) {
						funcName := strings.Split(parts[i+1], "(")[0]
						funcNames = append(funcNames, funcName)
					}
				}
			}
		}
	}
	
	// Build description based on code content
	var descParts []string
	
	// Add function names if found
	if len(funcNames) > 0 {
		descParts = append(descParts, fmt.Sprintf("Functions: %s", strings.Join(funcNames, ", ")))
	}
	
	// Detect operations from keywords
	operations := []string{}
	if strings.Contains(lower, "parse") || strings.Contains(lower, "json.loads") {
		operations = append(operations, "parse")
	}
	if strings.Contains(lower, "transform") || strings.Contains(lower, "convert") {
		operations = append(operations, "transform")
	}
	if strings.Contains(lower, "extract") {
		operations = append(operations, "extract")
	}
	if strings.Contains(lower, "filter") {
		operations = append(operations, "filter")
	}
	if strings.Contains(lower, "sort") {
		operations = append(operations, "sort")
	}
	if strings.Contains(lower, "http") || strings.Contains(lower, "request") {
		operations = append(operations, "HTTP requests")
	}
	if strings.Contains(lower, "matrix") || strings.Contains(lower, "array") {
		operations = append(operations, "array/matrix operations")
	}
	if strings.Contains(lower, "calculate") || strings.Contains(lower, "compute") {
		operations = append(operations, "calculate")
	}
	
	if len(operations) > 0 {
		descParts = append(descParts, fmt.Sprintf("Operations: %s", strings.Join(operations, ", ")))
	}
	
	// Build final description
	if len(descParts) > 0 {
		return fmt.Sprintf("Auto-proposed utility: %s (%s)", strings.Join(descParts, "; "), language)
	}
	
	// Fallback: try to extract a meaningful snippet
	if len(c) > 100 {
		// Use first meaningful line
		lines := strings.Split(c, "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if len(line) > 20 && !strings.HasPrefix(line, "#") && !strings.HasPrefix(line, "//") && !strings.HasPrefix(line, "import") {
				if len(line) > 80 {
					line = line[:80] + "..."
				}
				return fmt.Sprintf("Auto-proposed utility: %s (%s)", line, language)
			}
		}
	}
	
	return fmt.Sprintf("Auto-proposed utility from code artifact (%s)", language)
}

// proposeToolID creates a stable-ish tool id from language + code shape
func proposeToolID(language, code string) string {
	base := strings.ToLower(strings.TrimSpace(language))
	if base == "" {
		base = "tool"
	}
	// crude hash: length + a few keyword hits
	lower := strings.ToLower(code)
	score := 0
	for _, s := range []string{"http", "json", "parse", "extract", "client", "retry", "cache"} {
		if strings.Contains(lower, s) {
			score++
		}
	}
	return fmt.Sprintf("tool_%s_util_%d_%d", base, len(code), score)
}

// ToolProviderInterface defines the interface for tool providers
type ToolProviderInterface interface {
	GetAvailableTools(ctx context.Context) ([]Tool, error)
	ExecuteTool(ctx context.Context, toolID string, parameters map[string]interface{}) (interface{}, error)
}
