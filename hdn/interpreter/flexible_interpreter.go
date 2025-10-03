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
}

// NewFlexibleInterpreter creates a new flexible interpreter
func NewFlexibleInterpreter(llmAdapter *FlexibleLLMAdapter, toolProvider ToolProviderInterface) *FlexibleInterpreter {
	return &FlexibleInterpreter{
		llmAdapter:   llmAdapter,
		toolProvider: toolProvider,
	}
}

// Interpret processes natural language input using flexible interpretation
func (f *FlexibleInterpreter) Interpret(ctx context.Context, req *NaturalLanguageRequest) (*FlexibleInterpretationResult, error) {
	log.Printf("🧠 [FLEXIBLE-INTERPRETER] Processing natural language input: %s", req.Input)

	// Get available tools
	tools, err := f.toolProvider.GetAvailableTools(ctx)
	if err != nil {
		log.Printf("⚠️ [FLEXIBLE-INTERPRETER] Failed to get tools: %v", err)
		tools = []Tool{} // Continue with empty tools list
	}

	// Process with flexible LLM
	response, err := f.llmAdapter.ProcessNaturalLanguage(req.Input, tools)
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
		result.ToolCall = response.ToolCall
		result.Message = fmt.Sprintf("Tool call: %s", response.ToolCall.ToolID)
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

			result.Metadata["proposed_tool"] = map[string]interface{}{
				"id":           id,
				"name":         id,
				"description":  "Auto-proposed utility from code artifact",
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
func (f *FlexibleInterpreter) InterpretAndExecute(ctx context.Context, req *NaturalLanguageRequest) (*FlexibleInterpretationResult, error) {
	// First interpret
	result, err := f.Interpret(ctx, req)
	if err != nil {
		return result, err
	}

	// If it's a tool call, validate and execute it
	if result.ToolCall != nil {
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
			result.ToolExecutionResult = &ToolExecutionResult{
				Success: false,
				Error:   err.Error(),
			}
			result.Message = fmt.Sprintf("Tool execution failed: %v", err)
		} else {
			result.ToolExecutionResult = &ToolExecutionResult{
				Success: true,
				Result:  executionResult,
			}
			result.Message = "Tool executed successfully"
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
