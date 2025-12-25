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

// isScoringRequest detects if the input is a scoring/evaluation request that should not use tools
func isScoringRequest(input string) bool {
	lowerInput := strings.ToLower(input)
	return strings.Contains(lowerInput, "rate this hypothesis") ||
		(strings.Contains(lowerInput, "score") && strings.Contains(lowerInput, "0.0 to 1.0")) ||
		strings.Contains(lowerInput, "evaluating hypotheses") ||
		strings.Contains(lowerInput, "return only the json score") ||
		strings.Contains(lowerInput, "simple scoring task") ||
		strings.Contains(lowerInput, "no tools") && strings.Contains(lowerInput, "no actions")
}

// ProcessNaturalLanguageWithPriority processes natural language input with tool awareness and priority
// highPriority=true for user requests, false for background tasks
func (f *FlexibleLLMAdapter) ProcessNaturalLanguageWithPriority(input string, availableTools []Tool, highPriority bool) (*FlexibleLLMResponse, error) {
	log.Printf("🤖 [FLEXIBLE-LLM] Processing natural language input: %s (priority: %v)", input, highPriority)

	// For scoring requests, use empty tools list to prevent tool usage
	if isScoringRequest(input) {
		log.Printf("📊 [FLEXIBLE-LLM] Detected scoring request - using empty tools list to force text response")
		availableTools = []Tool{}
	}

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
		return f.parseFlexibleResponse(response, len(availableTools))
	}

	// Fallback to standard method (low priority)
	response, err := f.llmClient.GenerateResponse(prompt, map[string]string{})
	if err != nil {
		return nil, fmt.Errorf("failed to call LLM: %v", err)
	}

	log.Printf("✅ [FLEXIBLE-LLM] Generated response length: %d", len(response))

	// Parse the flexible response
	return f.parseFlexibleResponse(response, len(availableTools))
}

// buildToolAwarePrompt creates a prompt that includes available tools
func (f *FlexibleLLMAdapter) buildToolAwarePrompt(input string, availableTools []Tool) string {
	var prompt strings.Builder

	// Check if this is a scoring/evaluation request - force text response
	if isScoringRequest(input) {
		// For scoring requests, use a simpler prompt that forces text/JSON response
		// NO TOOLS ARE AVAILABLE - this is intentional to prevent tool usage
		prompt.WriteString("You are evaluating a hypothesis. This is a SIMPLE SCORING TASK.\n\n")
		prompt.WriteString("CRITICAL RULES:\n")
		prompt.WriteString("1. You MUST respond with type \"text\" containing ONLY a JSON object.\n")
		prompt.WriteString("2. Do NOT use tools - NO tools are available for this task.\n")
		prompt.WriteString("3. Do NOT create tasks.\n")
		prompt.WriteString("4. Do NOT attempt to execute any commands or queries.\n")
		prompt.WriteString("5. Just return the JSON score directly.\n\n")
		prompt.WriteString("User Input: ")
		prompt.WriteString(input)
		prompt.WriteString("\n\nYou MUST respond with EXACTLY this format (no other text):\n")
		prompt.WriteString("{\"type\": \"text\", \"content\": \"{\\\"score\\\": 0.75, \\\"reason\\\": \\\"Brief explanation\\\"}\"}\n\n")
		prompt.WriteString("Remember: NO tools, NO tasks, NO actions - just return the JSON score.")
		return prompt.String()
	}

	prompt.WriteString("🚨 CRITICAL INSTRUCTIONS - READ CAREFULLY:\n")
	prompt.WriteString("You MUST respond with ONLY a valid JSON object. NO markdown, NO code blocks, NO explanatory text.\n")
	prompt.WriteString("Example of correct response format:\n")
	prompt.WriteString(`{"type": "tool_call", "tool_call": {"tool_id": "mcp_get_concept", "parameters": {"name": "Science", "domain": "General"}, "description": "Retrieve information about Science"}}` + "\n\n")

	prompt.WriteString("You are an AI assistant that helps users achieve goals with concrete actions. ")
	prompt.WriteString("CRITICAL: ALWAYS prefer using available tools over generating code. Only generate code if no tool can accomplish the task.\n")
	prompt.WriteString("When a request is generic (like 'Execute a task'), analyze what the task likely requires and use the most appropriate tool.\n")
	prompt.WriteString("For example: file operations → use file tools, web requests → use HTTP tools, system commands → use exec tool.\n\n")

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
		
		// For code-based tools, include a code snippet so LLM knows what it does
		if tool.Exec != nil && tool.Exec.Type == "code" && tool.Exec.Code != "" {
			codePreview := tool.Exec.Code
			language := tool.Exec.Language
			if language == "" {
				language = "python" // default
			}
			// Limit code preview to first 500 chars to avoid overwhelming the prompt
			if len(codePreview) > 500 {
				codePreview = codePreview[:500] + "..."
			}
			prompt.WriteString(fmt.Sprintf("  Code (%s):\n    %s\n", language, strings.ReplaceAll(codePreview, "\n", "\n    ")))
		}
		
		prompt.WriteString("\n")
	}

	prompt.WriteString("🚨 CRITICAL: You MUST respond with ONLY a valid JSON object. NO explanatory text before or after the JSON.\n\n")
	prompt.WriteString("Respond using EXACTLY ONE of these JSON formats (no extra text, no markdown, no code blocks):\n")
	prompt.WriteString("1. STRONGLY PREFER (use this if ANY tool can help): {\"type\": \"tool_call\", \"tool_call\": {\"tool_id\": \"tool_name\", \"parameters\": {...}, \"description\": \"...\"}}\n")
	prompt.WriteString("2. Or: {\"type\": \"structured_task\", \"structured_task\": {\"task_name\": \"...\", \"description\": \"...\", \"subtasks\": [...]}}\n")
	prompt.WriteString("3. ONLY if no tool can accomplish the task: {\"type\": \"code_artifact\", \"code_artifact\": {\"language\": \"python\", \"code\": \"...\"}}\n")
	prompt.WriteString("4. Only if the user EXPLICITLY asks for a textual explanation and no action is possible: {\"type\": \"text\", \"content\": \"...\"}\n\n")
	prompt.WriteString("⚠️ IMPORTANT: Start your response with { and end with }. Do NOT wrap in markdown code blocks. Do NOT add any text outside the JSON.\n\n")

	// Enhanced guidance for tool usage
	prompt.WriteString("Rules:\n")
	prompt.WriteString("- CRITICAL: ALWAYS try to use available tools first before generating code.\n")
	prompt.WriteString("- If the request is vague or generic, infer the most likely tool needed and use it with reasonable default parameters.\n")
	prompt.WriteString("- For knowledge queries: use mcp_query_neo4j, mcp_get_concept, or mcp_find_related_concepts to query the knowledge base.\n")
	prompt.WriteString("- For HTTP requests: use tool_http_get with a valid URL.\n")
	prompt.WriteString("- For file operations: use tool_file_read, tool_file_write, or tool_ls.\n")
	prompt.WriteString("- For system operations: use tool_exec with appropriate commands.\n")
	prompt.WriteString("- For directory listing: use tool_ls with path parameter (default to '.' or '/tmp' if not specified).\n")
	prompt.WriteString("- For reading files: use tool_file_read with path parameter (infer common paths like /etc/hostname, /tmp, etc. if not specified).\n")
	prompt.WriteString("- When in doubt about which tool to use, prefer the most specific tool that matches the task description.\n")
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
	prompt.WriteString("\n\n🚨 FINAL REMINDER: Respond with ONLY the JSON object. Start with { and end with }. If ANY tool can help, use type: \"tool_call\". NO other text. JSON only:")

	return prompt.String()
}

// parseFlexibleResponse parses the LLM response into a flexible response structure
func (f *FlexibleLLMAdapter) parseFlexibleResponse(response string, availableToolsCount int) (*FlexibleLLMResponse, error) {
	// Try to parse as JSON first
	var flexibleResp FlexibleLLMResponse
	if err := json.Unmarshal([]byte(response), &flexibleResp); err == nil {
		log.Printf("✅ [FLEXIBLE-LLM] Parsed flexible response: %s", flexibleResp.Type)
		if flexibleResp.Type == "" {
			log.Printf("⚠️ [FLEXIBLE-LLM] WARNING: Parsed JSON but Type is empty! Response preview: %s", truncateString(response, 200))
		} else if flexibleResp.Type == ResponseTypeText && availableToolsCount > 0 {
			log.Printf("⚠️ [FLEXIBLE-LLM] WARNING: LLM returned 'text' type but %d tools are available! Response preview: %s", availableToolsCount, truncateString(response, 300))
		}
		return &flexibleResp, nil
	}

	// If JSON parsing fails, try to extract JSON from the response
	jsonStart := strings.Index(response, "{")
	jsonEnd := strings.LastIndex(response, "}")
	if jsonStart != -1 && jsonEnd != -1 && jsonEnd > jsonStart {
		jsonStr := response[jsonStart : jsonEnd+1]
		if err := json.Unmarshal([]byte(jsonStr), &flexibleResp); err == nil {
			log.Printf("✅ [FLEXIBLE-LLM] Extracted and parsed JSON: %s", flexibleResp.Type)
			if flexibleResp.Type == "" {
				log.Printf("⚠️ [FLEXIBLE-LLM] WARNING: Extracted JSON but Type is empty! Extracted JSON: %s", truncateString(jsonStr, 200))
			} else if flexibleResp.Type == ResponseTypeToolCall && flexibleResp.ToolCall != nil {
				log.Printf("🔧 [FLEXIBLE-LLM] Tool call parsed: %s", flexibleResp.ToolCall.ToolID)
			} else if flexibleResp.Type == ResponseTypeToolCall && flexibleResp.ToolCall == nil {
				log.Printf("⚠️ [FLEXIBLE-LLM] Tool call type but ToolCall is nil!")
			}
			return &flexibleResp, nil
		} else {
			log.Printf("⚠️ [FLEXIBLE-LLM] Failed to parse extracted JSON: %v, JSON: %s", err, truncateString(jsonStr, 200))
		}
	}

	// If all else fails, treat as text response
	log.Printf("💬 [FLEXIBLE-LLM] Treating as text response")
	log.Printf("💬 [FLEXIBLE-LLM] Raw response (first 500 chars): %s", truncateString(response, 500))
	log.Printf("💬 [FLEXIBLE-LLM] Response length: %d, starts with: %s", len(response), truncateString(strings.TrimSpace(response), 100))
	return &FlexibleLLMResponse{
		Type:    ResponseTypeText,
		Content: response,
	}, nil
}

// truncateString truncates a string to max length
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// ToolExecSpec represents how a tool should be executed
type ToolExecSpec struct {
	Type     string   `json:"type"`               // "cmd", "image", or "code"
	Cmd      string   `json:"cmd"`                // for Type=="cmd": absolute path inside container
	Args     []string `json:"args"`               // for Type=="cmd": command arguments
	Image    string   `json:"image,omitempty"`   // for Type=="image": docker image reference
	Code     string   `json:"code,omitempty"`    // for Type=="code": code to execute
	Language string   `json:"language,omitempty"` // for Type=="code": programming language
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
	Exec         *ToolExecSpec     `json:"exec,omitempty"` // Execution specification
}
