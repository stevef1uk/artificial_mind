package interpreter

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
)

// ResponseType represents the type of response from the flexible LLM
type ResponseType string

const (
	ResponseTypeToolCall       ResponseType = "tool_call"
	ResponseTypeCodeArtifact   ResponseType = "code_artifact"
	ResponseTypeStructuredTask ResponseType = "structured_task"
	ResponseTypeText           ResponseType = "text"
)

// FlexibleLLMResponse represents a flexible response from the LLM
type FlexibleLLMResponse struct {
	Type           ResponseType           `json:"type"`
	Content        string                 `json:"content"`
	ToolCall       *ToolCall              `json:"tool_call,omitempty"`
	CodeArtifact   *CodeArtifact          `json:"code_artifact,omitempty"`
	StructuredTask *InterpretedTask       `json:"structured_task,omitempty"`
	Metadata       map[string]interface{} `json:"metadata,omitempty"`
}

// ToolCall represents a tool call request
type ToolCall struct {
	ToolID      string                 `json:"tool_id"`
	Parameters  map[string]interface{} `json:"parameters"`
	Description string                 `json:"description"`
}

// CodeArtifact represents generated code
type CodeArtifact struct {
	Language string `json:"language"`
	Code     string `json:"code"`
}

// FlexibleLLMAdapter wraps an LLM client to provide flexible response parsing
type FlexibleLLMAdapter struct {
	llmClient LLMClientInterface
}

// NewFlexibleLLMAdapter creates a new flexible LLM adapter
func NewFlexibleLLMAdapter(llmClient LLMClientInterface) *FlexibleLLMAdapter {
	return &FlexibleLLMAdapter{
		llmClient: llmClient,
	}
}

// ProcessNaturalLanguage processes natural language input with tool awareness
// Uses default (low) priority for backward compatibility
func (f *FlexibleLLMAdapter) ProcessNaturalLanguage(input string, availableTools []Tool) (*FlexibleLLMResponse, error) {
	return f.ProcessNaturalLanguageWithPriority(input, availableTools, false)
}

// ProcessNaturalLanguageWithPriority processes natural language input with tool awareness and priority
// highPriority=true for user requests, false for background tasks
func (f *FlexibleLLMAdapter) ProcessNaturalLanguageWithPriority(input string, availableTools []Tool, highPriority bool) (*FlexibleLLMResponse, error) {
	log.Printf("🤖 [FLEXIBLE-LLM] Processing natural language input: %s (priority: %v)", input, highPriority)
	log.Printf("🤖 [FLEXIBLE-LLM] Available tools: %d", len(availableTools))

	// Build tool-aware prompt
	prompt := f.buildToolAwarePrompt(input, availableTools)

	// Call the LLM - check if the client supports priority
	if priorityClient, ok := f.llmClient.(interface {
		GenerateResponseWithPriority(prompt string, context map[string]string, highPriority bool) (string, error)
	}); ok {
		// Use priority-aware method
		response, err := priorityClient.GenerateResponseWithPriority(prompt, map[string]string{}, highPriority)
		if err != nil {
			return nil, fmt.Errorf("failed to call LLM: %v", err)
		}
		log.Printf("✅ [FLEXIBLE-LLM] Generated response length: %d", len(response))
		return f.parseFlexibleResponse(response)
	}

	// Fallback to standard method (low priority)
	response, err := f.llmClient.GenerateResponse(prompt, map[string]string{})
	if err != nil {
		return nil, fmt.Errorf("failed to call LLM: %v", err)
	}

	log.Printf("✅ [FLEXIBLE-LLM] Generated response length: %d", len(response))

	// Parse the flexible response
	return f.parseFlexibleResponse(response)
}

// buildToolAwarePrompt creates a prompt that includes available tools
func (f *FlexibleLLMAdapter) buildToolAwarePrompt(input string, availableTools []Tool) string {
	var prompt strings.Builder

	prompt.WriteString("You are an AI assistant that helps users achieve goals with concrete actions. ")
	prompt.WriteString("ALWAYS prefer using available tools over generating code. Only generate code if no tool can accomplish the task.\n\n")

	prompt.WriteString("Available Tools:\n")
	for _, tool := range availableTools {
		prompt.WriteString(fmt.Sprintf("- %s: %s\n", tool.ID, tool.Description))

		// Include input schema for each tool
		if len(tool.InputSchema) > 0 {
			prompt.WriteString("  Parameters:\n")
			for paramName, paramType := range tool.InputSchema {
				prompt.WriteString(fmt.Sprintf("    - %s (%s): required\n", paramName, paramType))
			}
		}
		prompt.WriteString("\n")
	}

	prompt.WriteString("Respond using EXACTLY ONE of these JSON formats (no extra text):\n")
	prompt.WriteString("1. STRONGLY PREFER: {\"type\": \"tool_call\", \"tool_call\": {\"tool_id\": \"tool_name\", \"parameters\": {...}, \"description\": \"...\"}}\n")
	prompt.WriteString("2. Or: {\"type\": \"structured_task\", \"structured_task\": {\"task_name\": \"...\", \"description\": \"...\", \"subtasks\": [...]}}\n")
	prompt.WriteString("3. ONLY if no tool can accomplish the task: {\"type\": \"code_artifact\", \"code_artifact\": {\"language\": \"python\", \"code\": \"...\"}}\n")
	prompt.WriteString("4. Only if the user EXPLICITLY asks for a textual explanation and no action is possible: {\"type\": \"text\", \"content\": \"...\"}\n\n")

	// Enhanced guidance for tool usage
	prompt.WriteString("Rules:\n")
	prompt.WriteString("- ALWAYS try to use available tools first before generating code.\n")
	prompt.WriteString("- For knowledge queries: use mcp_query_neo4j, mcp_get_concept, or mcp_find_related_concepts to query the knowledge base.\n")
	prompt.WriteString("- For HTTP requests: use tool_http_get with a valid URL.\n")
	prompt.WriteString("- For file operations: use tool_file_read, tool_file_write, or tool_ls.\n")
	prompt.WriteString("- For system operations: use tool_exec with appropriate commands.\n")
	prompt.WriteString("- If tools are relevant, choose tool_call and set ALL required parameters with realistic values.\n")
	prompt.WriteString("- For mcp_get_concept: provide 'name' parameter with the concept name.\n")
	prompt.WriteString("- For mcp_query_neo4j: provide 'query' parameter with a Cypher query.\n")
	prompt.WriteString("- For mcp_find_related_concepts: provide 'concept_name' parameter.\n")
	prompt.WriteString("- For tool_http_get: always provide a valid URL in the 'url' parameter.\n")
	prompt.WriteString("- For tool_file_read: always provide a valid file path in the 'path' parameter.\n")
	prompt.WriteString("- For tool_ls: always provide a valid directory path in the 'path' parameter.\n")
	prompt.WriteString("- For tool_exec: always provide a valid shell command in the 'cmd' parameter.\n")
	prompt.WriteString("- Avoid generic requests for more information; propose a minimal actionable plan with assumptions noted in description.\n")
	prompt.WriteString("- Do NOT include any commentary outside the JSON object.\n\n")

	prompt.WriteString("User Input: ")
	prompt.WriteString(input)
	prompt.WriteString("\n\nChoose the most appropriate response type (favor structured_task/tool_call) and provide ONLY the JSON object.")

	return prompt.String()
}

// parseFlexibleResponse parses the LLM response into a flexible response structure
func (f *FlexibleLLMAdapter) parseFlexibleResponse(response string) (*FlexibleLLMResponse, error) {
	// Try to parse as JSON first
	var flexibleResp FlexibleLLMResponse
	if err := json.Unmarshal([]byte(response), &flexibleResp); err == nil {
		log.Printf("✅ [FLEXIBLE-LLM] Parsed flexible response: %s", flexibleResp.Type)
		return &flexibleResp, nil
	}

	// If JSON parsing fails, try to extract JSON from the response
	jsonStart := strings.Index(response, "{")
	jsonEnd := strings.LastIndex(response, "}")
	if jsonStart != -1 && jsonEnd != -1 && jsonEnd > jsonStart {
		jsonStr := response[jsonStart : jsonEnd+1]
		if err := json.Unmarshal([]byte(jsonStr), &flexibleResp); err == nil {
			log.Printf("✅ [FLEXIBLE-LLM] Extracted and parsed JSON: %s", flexibleResp.Type)
			if flexibleResp.Type == ResponseTypeToolCall && flexibleResp.ToolCall != nil {
				log.Printf("🔧 [FLEXIBLE-LLM] Tool call parsed: %s", flexibleResp.ToolCall.ToolID)
			} else if flexibleResp.Type == ResponseTypeToolCall && flexibleResp.ToolCall == nil {
				log.Printf("⚠️ [FLEXIBLE-LLM] Tool call type but ToolCall is nil!")
			}
			return &flexibleResp, nil
		}
	}

	// If all else fails, treat as text response
	log.Printf("💬 [FLEXIBLE-LLM] Treating as text response")
	return &FlexibleLLMResponse{
		Type:    ResponseTypeText,
		Content: response,
	}, nil
}

// Tool represents an available tool
type Tool struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Description  string            `json:"description"`
	InputSchema  map[string]string `json:"input_schema"`
	OutputSchema map[string]string `json:"output_schema"`
	Permissions  []string          `json:"permissions"`
	SafetyLevel  string            `json:"safety_level"`
	CreatedBy    string            `json:"created_by"`
	CreatedAt    string            `json:"created_at"`
}
