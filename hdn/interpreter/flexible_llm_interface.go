package interpreter

import (
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync"
)

// Global registry for prompt hints from configured skills
var (
	promptHintsRegistry = make(map[string]*PromptHintsConfig)
	promptHintsMutex    sync.RWMutex
)

// PromptHintsConfig defines LLM prompt hints for a skill (imported from main package)
type PromptHintsConfig struct {
	Keywords          []string `json:"keywords,omitempty"`
	PromptText        string   `json:"prompt_text,omitempty"`
	ForceToolCall     bool     `json:"force_tool_call,omitempty"`
	AlwaysInclude     []string `json:"always_include_keywords,omitempty"`
	RejectText        bool     `json:"reject_text_response,omitempty"`
}

// SetPromptHints sets prompt hints for a tool ID
func SetPromptHints(toolID string, hints *PromptHintsConfig) {
	promptHintsMutex.Lock()
	defer promptHintsMutex.Unlock()
	if hints != nil {
		promptHintsRegistry[toolID] = hints
		// Also store without mcp_ prefix
		cleanID := strings.TrimPrefix(toolID, "mcp_")
		if cleanID != toolID {
			promptHintsRegistry[cleanID] = hints
		}
	}
}

// GetPromptHints returns prompt hints for a tool ID
func GetPromptHints(toolID string) *PromptHintsConfig {
	promptHintsMutex.RLock()
	defer promptHintsMutex.RUnlock()
	if hints, ok := promptHintsRegistry[toolID]; ok {
		return hints
	}
	// Try with mcp_ prefix
	if hints, ok := promptHintsRegistry["mcp_"+toolID]; ok {
		return hints
	}
	// Try without prefix
	cleanID := strings.TrimPrefix(toolID, "mcp_")
	if hints, ok := promptHintsRegistry[cleanID]; ok {
		return hints
	}
	return nil
}

// GetAllPromptHints returns all prompt hints
func GetAllPromptHints() map[string]*PromptHintsConfig {
	promptHintsMutex.RLock()
	defer promptHintsMutex.RUnlock()
	result := make(map[string]*PromptHintsConfig)
	for k, v := range promptHintsRegistry {
		result[k] = v
	}
	return result
}

// MatchesConfiguredToolKeywords checks if a message matches keywords for any configured tool
// Returns the tool ID if a match is found, empty string otherwise
func MatchesConfiguredToolKeywords(message string) string {
	messageLower := strings.ToLower(message)
	allHints := GetAllPromptHints()
	
	for toolID, hints := range allHints {
		if hints == nil {
			continue
		}
		
		// Check keywords
		for _, keyword := range hints.Keywords {
			if strings.Contains(messageLower, strings.ToLower(keyword)) {
				return toolID
			}
		}
		
		// Check always_include keywords
		for _, keyword := range hints.AlwaysInclude {
			if strings.Contains(messageLower, strings.ToLower(keyword)) {
				return toolID
			}
		}
	}
	
	return ""
}

// ShouldRouteToNaturalLanguage checks if a message should be routed to InterpretNaturalLanguage
// based on configured tool keywords
func ShouldRouteToNaturalLanguage(message string) bool {
	return MatchesConfiguredToolKeywords(message) != ""
}

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
		(strings.Contains(lowerInput, "no tools") && strings.Contains(lowerInput, "no actions")) ||
		// Belief assessment patterns
		strings.Contains(lowerInput, "assess whether this knowledge") ||
		strings.Contains(lowerInput, "worth storing as a belief") ||
		(strings.Contains(lowerInput, "is_novel") && strings.Contains(lowerInput, "is_worth_learning")) ||
		strings.Contains(lowerInput, "assess whether") && strings.Contains(lowerInput, "belief")
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

	// Filter tools based on task relevance to reduce prompt size
	filteredTools := f.filterRelevantTools(input, availableTools)
	log.Printf("🔍 [FLEXIBLE-LLM] Filtered to %d relevant tools (from %d total)", len(filteredTools), len(availableTools))

	// Build tool-aware prompt with filtered tools
	prompt := f.buildToolAwarePrompt(input, filteredTools)

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
		parsedResponse, err := f.parseFlexibleResponse(response, len(filteredTools))
		if err != nil {
			return nil, err
		}

		// Validation: Check configured prompt hints and enforce tool usage
		inputLower := strings.ToLower(input)
		allHints := GetAllPromptHints()
		
		// Check each tool with prompt hints
		for toolID, hints := range allHints {
			if hints == nil {
				continue
			}
			
			// Check if input matches keywords
			matchesKeywords := false
			for _, keyword := range hints.Keywords {
				if strings.Contains(inputLower, strings.ToLower(keyword)) {
					matchesKeywords = true
					break
				}
			}
			
			// Check always_include keywords
			matchesAlwaysInclude := false
			for _, keyword := range hints.AlwaysInclude {
				if strings.Contains(inputLower, strings.ToLower(keyword)) {
					matchesAlwaysInclude = true
					break
				}
			}
			
			if !matchesKeywords && !matchesAlwaysInclude {
				continue
			}
			
			// Check if tool is available
			hasTool := false
			actualToolID := toolID
			for _, tool := range filteredTools {
				if tool.ID == toolID || tool.ID == "mcp_"+toolID || strings.TrimPrefix(tool.ID, "mcp_") == toolID {
					hasTool = true
					actualToolID = tool.ID // Use the actual tool ID from the tool list
					break
				}
			}
			
			if hasTool && hints.ForceToolCall {
				// Reject text responses if configured
				if hints.RejectText && parsedResponse.Type == ResponseTypeText {
					log.Printf("❌ [FLEXIBLE-LLM] REJECTED: Text response when %s tool is available. Forcing tool call.", actualToolID)
					// Force tool call with default parameters
					return &FlexibleLLMResponse{
						Type: ResponseTypeToolCall,
						ToolCall: &ToolCall{
							ToolID:      actualToolID,
							Parameters:  map[string]interface{}{},
							Description: fmt.Sprintf("Using %s as requested", actualToolID),
						},
					}, nil
				}
				
				// Reject wrong tool calls if configured
				if parsedResponse.Type == ResponseTypeToolCall && parsedResponse.ToolCall != nil {
					responseToolID := parsedResponse.ToolCall.ToolID
					if responseToolID != actualToolID && strings.TrimPrefix(responseToolID, "mcp_") != strings.TrimPrefix(actualToolID, "mcp_") {
						log.Printf("❌ [FLEXIBLE-LLM] REJECTED: Wrong tool '%s'. Forcing %s.", responseToolID, actualToolID)
						// Force correct tool call
						return &FlexibleLLMResponse{
							Type: ResponseTypeToolCall,
							ToolCall: &ToolCall{
								ToolID:      actualToolID,
								Parameters:  map[string]interface{}{},
								Description: fmt.Sprintf("Using %s as requested", actualToolID),
							},
						}, nil
					}
				}
			}
		}

		return parsedResponse, nil
	}

	// Fallback to standard method (low priority)
	response, err := f.llmClient.GenerateResponse(prompt, map[string]string{})
	if err != nil {
		return nil, fmt.Errorf("failed to call LLM: %v", err)
	}

	log.Printf("✅ [FLEXIBLE-LLM] Generated response length: %d", len(response))

	// Parse the flexible response
	parsedResponse, err := f.parseFlexibleResponse(response, len(filteredTools))
	if err != nil {
		return nil, err
	}

	// Validation: Check configured prompt hints and enforce tool usage
	inputLower := strings.ToLower(input)
	allHints := GetAllPromptHints()
	
	// Check each tool with prompt hints
	for toolID, hints := range allHints {
		if hints == nil {
			continue
		}
		
		// Check if input matches keywords
		matchesKeywords := false
		for _, keyword := range hints.Keywords {
			if strings.Contains(inputLower, strings.ToLower(keyword)) {
				matchesKeywords = true
				break
			}
		}
		
		// Check always_include keywords
		matchesAlwaysInclude := false
		for _, keyword := range hints.AlwaysInclude {
			if strings.Contains(inputLower, strings.ToLower(keyword)) {
				matchesAlwaysInclude = true
				break
			}
		}
		
		if !matchesKeywords && !matchesAlwaysInclude {
			continue
		}
		
		// Check if tool is available
		hasTool := false
		actualToolID := toolID
		for _, tool := range filteredTools {
			if tool.ID == toolID || tool.ID == "mcp_"+toolID || strings.TrimPrefix(tool.ID, "mcp_") == toolID {
				hasTool = true
				actualToolID = tool.ID // Use the actual tool ID from the tool list
				break
			}
		}
		
		if hasTool && hints.ForceToolCall {
			// Reject text responses if configured
				if hints.RejectText && parsedResponse.Type == ResponseTypeText {
					log.Printf("❌ [FLEXIBLE-LLM] REJECTED: Text response when %s tool is available. Forcing tool call.", actualToolID)
				// Force tool call with default parameters
				return &FlexibleLLMResponse{
					Type: ResponseTypeToolCall,
					ToolCall: &ToolCall{
						ToolID:      actualToolID,
						Parameters:  map[string]interface{}{},
						Description: fmt.Sprintf("Using %s as requested", actualToolID),
					},
				}, nil
			}
			
			// Reject wrong tool calls if configured
			if parsedResponse.Type == ResponseTypeToolCall && parsedResponse.ToolCall != nil {
				responseToolID := parsedResponse.ToolCall.ToolID
				if responseToolID != actualToolID && strings.TrimPrefix(responseToolID, "mcp_") != strings.TrimPrefix(actualToolID, "mcp_") {
					log.Printf("❌ [FLEXIBLE-LLM] REJECTED: Wrong tool '%s'. Forcing %s.", responseToolID, actualToolID)
					// Force correct tool call
					return &FlexibleLLMResponse{
						Type: ResponseTypeToolCall,
						ToolCall: &ToolCall{
							ToolID:      actualToolID,
							Parameters:  map[string]interface{}{},
							Description: fmt.Sprintf("Using %s as requested", actualToolID),
						},
					}, nil
				}
			}
		}
	}

	return parsedResponse, nil
}

// filterRelevantTools filters tools based on task relevance to reduce prompt size
func (f *FlexibleLLMAdapter) filterRelevantTools(input string, tools []Tool) []Tool {
	if len(tools) <= 15 {
		// If we have 15 or fewer tools, include all of them
		return tools
	}

	inputLower := strings.ToLower(input)
	var relevant []Tool
	seen := make(map[string]bool)
	commonKeywords := []string{"query", "neo4j", "http", "file", "read", "write", "exec", "docker", "code", "generate", "search", "scrape", "email", "emails"}

	// Keywords that suggest specific tool usage
	toolKeywords := map[string][]string{
		"mcp_query_neo4j":           {"neo4j", "query", "cypher", "knowledge", "graph", "database", "knowledge base"},
		"mcp_get_concept":           {"concept", "get concept", "retrieve concept", "knowledge"},
		"mcp_find_related_concepts": {"related", "related concepts", "find related", "connections"},
		"mcp_search_weaviate":       {"weaviate", "search", "vector", "semantic", "similar", "episodes", "memories"},
		// Note: mcp_read_google_data keywords are now loaded from configuration
		"tool_http_get":             {"http", "url", "fetch", "get", "request", "api", "endpoint", "download", "retrieve", "web"},
		"tool_html_scraper":         {"scrape", "html", "web", "website", "article", "news", "page", "parse html"},
		"tool_file_read":            {"read", "file", "load", "open", "readfile", "read file", "content", "text"},
		"tool_file_write":           {"write", "file", "save", "store", "output", "write file", "save file", "create file"},
		"tool_ls":                   {"list", "directory", "dir", "files", "ls", "list files", "directory listing"},
		"tool_exec":                 {"exec", "execute", "command", "shell", "run", "cmd", "system", "bash", "sh"},
		"tool_codegen":              {"generate", "code", "create", "write code", "generate code", "program", "script"},
		"tool_json_parse":           {"json", "parse", "parse json", "decode", "unmarshal"},
		"tool_text_search":          {"search", "find", "text", "pattern", "match", "grep", "filter"},
		"tool_docker_list":          {"docker", "container", "image", "list docker", "docker list"},
		"tool_docker_build":         {"docker build", "build image", "dockerfile", "container build"},
		"tool_docker_exec":          {"docker exec", "run docker", "execute docker", "container exec"},
	}

	// First pass: include tools that match keywords
	for _, tool := range tools {
		if seen[tool.ID] {
			continue
		}

		matched := false
		for toolID, keywords := range toolKeywords {
			if tool.ID == toolID {
				for _, keyword := range keywords {
					if strings.Contains(inputLower, keyword) {
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

		// Check if tool ID is explicitly mentioned
		if strings.Contains(inputLower, strings.ToLower(tool.ID)) {
			relevant = append(relevant, tool)
			seen[tool.ID] = true
			continue
		}

		// Check tool description/name for keyword matches
		toolDesc := strings.ToLower(tool.Description + " " + tool.Name)
		for _, keyword := range commonKeywords {
			if strings.Contains(inputLower, keyword) && strings.Contains(toolDesc, keyword) {
				relevant = append(relevant, tool)
				seen[tool.ID] = true
				break
			}
		}
	}

	// Second pass: Always include commonly useful tools
	alwaysInclude := []string{
		"mcp_query_neo4j", // Very commonly used for knowledge queries
		"mcp_get_concept", // Common for concept retrieval
		"tool_http_get",   // Very commonly used
		"tool_file_read",  // Common for file operations
		"tool_file_write", // Common for saving results
		"tool_exec",       // Common for system operations
	}

	// Check configured prompt hints for always_include keywords
	allHints := GetAllPromptHints()
	for toolID, hints := range allHints {
		if hints == nil || len(hints.AlwaysInclude) == 0 {
			continue
		}
		for _, keyword := range hints.AlwaysInclude {
			if strings.Contains(inputLower, strings.ToLower(keyword)) {
				alwaysInclude = append(alwaysInclude, toolID)
				break
			}
		}
	}
	
	// Also check keywords for tool matching
	for toolID, hints := range allHints {
		if hints == nil || len(hints.Keywords) == 0 {
			continue
		}
		for _, keyword := range hints.Keywords {
			if strings.Contains(inputLower, strings.ToLower(keyword)) {
				// Add to toolKeywords map for filtering
				if toolKeywords[toolID] == nil {
					toolKeywords[toolID] = []string{}
				}
				toolKeywords[toolID] = append(toolKeywords[toolID], keyword)
			}
		}
	}

	for _, toolID := range alwaysInclude {
		if !seen[toolID] {
			for _, tool := range tools {
				if tool.ID == toolID {
					relevant = append(relevant, tool)
					seen[tool.ID] = true
					break
				}
			}
		}
	}

	// Third pass: Include MCP tools (they're usually relevant for knowledge tasks)
	for _, tool := range tools {
		if !seen[tool.ID] && strings.HasPrefix(tool.ID, "mcp_") {
			relevant = append(relevant, tool)
			seen[tool.ID] = true
		}
	}

	// Fourth pass: Deduplicate similar auto-created tools (keep only one per similar description)
	// This reduces prompt size by removing redundant tool_python_util_* tools with similar purposes
	type toolGroup struct {
		tools []Tool
		best  Tool
	}
	groups := make(map[string]*toolGroup)

	for _, tool := range relevant {
		// For auto-created utility tools, group by description similarity
		if strings.HasPrefix(tool.ID, "tool_python_util_") || strings.HasPrefix(tool.ID, "tool_go_util_") {
			// Create a normalized key from description (first meaningful words)
			descKey := strings.ToLower(tool.Description)
			// Remove specific IDs/numbers to group similar tools
			descKey = strings.ReplaceAll(descKey, "test_event_", "")
			// Extract first 5 words as key
			words := strings.Fields(descKey)
			if len(words) > 5 {
				descKey = strings.Join(words[:5], " ")
			} else if len(words) > 0 {
				descKey = strings.Join(words, " ")
			}

			if group, exists := groups[descKey]; exists {
				group.tools = append(group.tools, tool)
				// Keep the first one (they're similar anyway)
			} else {
				groups[descKey] = &toolGroup{
					tools: []Tool{tool},
					best:  tool,
				}
			}
		}
	}

	// Replace similar tools with just one from each group
	if len(groups) > 0 {
		newRelevant := make([]Tool, 0, len(relevant))
		seenInGroups := make(map[string]bool)

		for _, tool := range relevant {
			if strings.HasPrefix(tool.ID, "tool_python_util_") || strings.HasPrefix(tool.ID, "tool_go_util_") {
				descKey := strings.ToLower(tool.Description)
				descKey = strings.ReplaceAll(descKey, "test_event_", "")
				words := strings.Fields(descKey)
				if len(words) > 5 {
					descKey = strings.Join(words[:5], " ")
				} else if len(words) > 0 {
					descKey = strings.Join(words, " ")
				}

				if group, exists := groups[descKey]; exists {
					if !seenInGroups[descKey] {
						// Add only the first tool from this group
						newRelevant = append(newRelevant, group.best)
						seenInGroups[descKey] = true
					}
					// Skip other tools in the same group
					continue
				}
			}
			// Keep non-grouped tools
			newRelevant = append(newRelevant, tool)
		}
		relevant = newRelevant
	}

	// If we still have too many tools, limit to top 20 most relevant
	if len(relevant) > 20 {
		// Score tools by relevance
		type scoredTool struct {
			tool  Tool
			score int
		}
		scored := make([]scoredTool, len(relevant))
		for i, tool := range relevant {
			score := 0
			toolDesc := strings.ToLower(tool.Description + " " + tool.Name + " " + tool.ID)

			// Higher score for exact matches
			if strings.Contains(inputLower, strings.ToLower(tool.ID)) {
				score += 10
			}

			// Score based on keyword matches in description
			for _, keyword := range commonKeywords {
				if strings.Contains(inputLower, keyword) && strings.Contains(toolDesc, keyword) {
					score += 5
				}
			}

			// Prefer MCP tools for knowledge-related tasks
			if strings.HasPrefix(tool.ID, "mcp_") && (strings.Contains(inputLower, "query") || strings.Contains(inputLower, "knowledge") || strings.Contains(inputLower, "neo4j")) {
				score += 3
			}

			scored[i] = scoredTool{tool: tool, score: score}
		}

		// Sort by score (descending) and take top 20
		for i := 0; i < len(scored)-1; i++ {
			for j := i + 1; j < len(scored); j++ {
				if scored[i].score < scored[j].score {
					scored[i], scored[j] = scored[j], scored[i]
				}
			}
		}

		relevant = make([]Tool, 20)
		for i := 0; i < 20 && i < len(scored); i++ {
			relevant[i] = scored[i].tool
		}
	}

	return relevant
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
			// Limit code preview to first 200 chars to avoid overwhelming the prompt
			if len(codePreview) > 200 {
				codePreview = codePreview[:200] + "..."
			}
			prompt.WriteString(fmt.Sprintf("  Code (%s):\n    %s\n", language, strings.ReplaceAll(codePreview, "\n", "\n    ")))
		}

		prompt.WriteString("\n")
	}

	prompt.WriteString("🚨 CRITICAL: You MUST respond with ONLY a valid JSON object. NO explanatory text before or after the JSON.\n\n")
	
	// Add configured prompt hints
	allHints := GetAllPromptHints()
	for toolID, hints := range allHints {
		if hints != nil && hints.PromptText != "" {
			// Replace tool ID placeholder if present
			promptText := strings.ReplaceAll(hints.PromptText, "mcp_read_google_data", toolID)
			promptText = strings.ReplaceAll(promptText, "read_google_data", toolID)
			prompt.WriteString(promptText)
			prompt.WriteString("\n\n")
		}
	}
	prompt.WriteString("Respond using EXACTLY ONE of these JSON formats (no extra text, no markdown, no code blocks):\n")
	prompt.WriteString("1. STRONGLY PREFER (use this if ANY tool can help): {\"type\": \"tool_call\", \"tool_call\": {\"tool_id\": \"tool_name\", \"parameters\": {...}, \"description\": \"...\"}}\n")
	prompt.WriteString("2. Or: {\"type\": \"structured_task\", \"structured_task\": {\"task_name\": \"...\", \"description\": \"...\", \"subtasks\": [...]}}\n")
	prompt.WriteString("3. ONLY if no tool can accomplish the task: {\"type\": \"code_artifact\", \"code_artifact\": {\"language\": \"python\", \"code\": \"...\"}}\n")
	prompt.WriteString("4. Only if the user EXPLICITLY asks for a textual explanation and no action is possible: {\"type\": \"text\", \"content\": \"...\"}\n\n")
	prompt.WriteString("⚠️ IMPORTANT: Start your response with { and end with }. Do NOT wrap in markdown code blocks. Do NOT add any text outside the JSON.\n\n")

	// Enhanced guidance for tool usage
	prompt.WriteString("Rules:\n")
	prompt.WriteString("- CRITICAL: ALWAYS try to use available tools first before generating code.\n")
	prompt.WriteString("- 🚫 NEVER generate fake or hallucinated content. If you need data, use a tool to get it.\n")
	prompt.WriteString("- If the request is vague or generic, infer the most likely tool needed and use it with reasonable default parameters.\n")
	prompt.WriteString("- For knowledge queries: use mcp_query_neo4j, mcp_get_concept, or mcp_find_related_concepts to query the knowledge base.\n")
	
	// Add configured tool-specific guidance from prompt hints (reuse allHints from above)
	for toolID, hints := range allHints {
		if hints != nil && hints.PromptText != "" {
			// Add tool-specific guidance
			promptText := strings.ReplaceAll(hints.PromptText, "mcp_read_google_data", toolID)
			promptText = strings.ReplaceAll(promptText, "read_google_data", toolID)
			prompt.WriteString(fmt.Sprintf("- For %s: %s\n", toolID, promptText))
		}
	}
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
	prompt.WriteString("- For mcp_read_google_data: use this for email or calendar requests. Provide 'query' parameter (e.g., 'unread', 'recent', 'today', or empty string for all). Optional 'type' parameter: 'email', 'calendar', or 'all' (default). Optional 'limit' parameter: number of results (default: 5, max: 50). IMPORTANT: Always include limit=5 for email requests to prevent timeouts with large inboxes.\n")
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
	// First, strip markdown code blocks if present
	cleaned := f.stripMarkdownCodeBlocks(response)

	// Fix common JSON issues BEFORE extraction (to help with unescaped quotes in code strings)
	// This helps extractFirstJSONObject correctly identify string boundaries
	cleaned = f.fixCommonJSONIssues(cleaned)

	// Try to parse as JSON first
	var flexibleResp FlexibleLLMResponse
	if err := json.Unmarshal([]byte(cleaned), &flexibleResp); err == nil {
		f.normalizeResponse(&flexibleResp, cleaned)
		return &flexibleResp, nil
	}

	// If JSON parsing fails, try to extract JSON from the cleaned response using brace matching
	jsonStr := f.extractFirstJSONObject(cleaned)
	if jsonStr != "" {
		// Try to fix common JSON issues again (in case extraction introduced issues)
		fixedJSON := f.fixCommonJSONIssues(jsonStr)

		if err := json.Unmarshal([]byte(fixedJSON), &flexibleResp); err == nil {
			log.Printf("✅ [FLEXIBLE-LLM] Extracted and parsed JSON: %s", flexibleResp.Type)
			f.normalizeResponse(&flexibleResp, fixedJSON)

			if flexibleResp.Type == "" {
				log.Printf("⚠️ [FLEXIBLE-LLM] WARNING: Extracted JSON but Type is empty! Extracted JSON: %s", truncateString(jsonStr, 200))
			} else if flexibleResp.Type == ResponseTypeToolCall && flexibleResp.ToolCall != nil {
				log.Printf("🔧 [FLEXIBLE-LLM] Tool call parsed: %s", flexibleResp.ToolCall.ToolID)
			}
			return &flexibleResp, nil
		} else {
			log.Printf("⚠️ [FLEXIBLE-LLM] Failed to parse extracted JSON: %v, JSON: %s", err, truncateString(jsonStr, 200))
			// Try one more time with the original (maybe the fix made it worse)
			if err2 := json.Unmarshal([]byte(jsonStr), &flexibleResp); err2 == nil {
				log.Printf("✅ [FLEXIBLE-LLM] Parsed original extracted JSON after fix attempt failed")
				f.normalizeResponse(&flexibleResp, jsonStr)
				return &flexibleResp, nil
			}
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

// normalizeResponse attempts to fix responses where the LLM might have used a slightly different structure
func (f *FlexibleLLMAdapter) normalizeResponse(resp *FlexibleLLMResponse, rawJSON string) {
	// If Type is not a standard one, check if it's a tool ID
	isStandard := false
	for _, t := range []ResponseType{ResponseTypeToolCall, ResponseTypeCodeArtifact, ResponseTypeStructuredTask, ResponseTypeText} {
		if resp.Type == t {
			isStandard = true
			break
		}
	}

	if !isStandard && resp.Type != "" {
		// LLM likely put the tool ID in the Type field
		toolID := string(resp.Type)
		log.Printf("🔄 [FLEXIBLE-LLM] Normalizing non-standard type '%s' as tool_call", toolID)

		// If ToolCall is nil, create it
		if resp.ToolCall == nil {
			resp.ToolCall = &ToolCall{
				ToolID: toolID,
			}

			// Try to find parameters in the raw JSON
			var raw map[string]interface{}
			if err := json.Unmarshal([]byte(rawJSON), &raw); err == nil {
				if params, ok := raw["parameters"].(map[string]interface{}); ok {
					resp.ToolCall.Parameters = params
				} else {
					// Fallback: use all non-standard fields as parameters
					params = make(map[string]interface{})
					for k, v := range raw {
						if k != "type" && k != "tool_call" && k != "content" {
							params[k] = v
						}
					}
					// Also include content if it was provided (common for small models)
					if resp.Content != "" {
						params["content"] = resp.Content
					}
					resp.ToolCall.Parameters = params
				}
			}
		}
		resp.Type = ResponseTypeToolCall
	}

	// Special case for tool_call type with missing tool_call object but tool_id at root
	if resp.Type == ResponseTypeToolCall && resp.ToolCall == nil {
		var raw map[string]interface{}
		if err := json.Unmarshal([]byte(rawJSON), &raw); err == nil {
			if toolID, ok := raw["tool_id"].(string); ok {
				resp.ToolCall = &ToolCall{
					ToolID: toolID,
				}
				if params, ok := raw["parameters"].(map[string]interface{}); ok {
					resp.ToolCall.Parameters = params
				} else if content, ok := raw["content"].(string); ok {
					resp.ToolCall.Parameters = map[string]interface{}{"content": content}
				}
			}
		}
	}
}

// fixCommonJSONIssues attempts to fix common JSON issues from LLM responses
func (f *FlexibleLLMAdapter) fixCommonJSONIssues(jsonStr string) string {
	// The LLM sometimes generates JSON with unescaped quotes in code strings
	// Pattern: "code": "package main\n\nimport (\n\t"encoding/json"\n\t...
	// The quotes inside the code string need to be escaped: \"encoding/json\"

	// Use a regex to find "code": "..." and escape unescaped quotes inside the value
	// This is a best-effort fix - the proper solution is to ensure the LLM generates valid JSON

	// Find the code field and fix quotes inside it
	// We'll use a simple approach: find "code": " and then escape quotes until we find the matching closing quote
	// Use regex to find the pattern (handles whitespace variations)
	codePattern := regexp.MustCompile(`"code"\s*:\s*"`)
	codeMatch := codePattern.FindStringIndex(jsonStr)
	if codeMatch == nil {
		return jsonStr // No code field, return as-is
	}

	// Find the start of the code string value (after the opening quote)
	valueStart := codeMatch[1]
	if valueStart >= len(jsonStr) {
		return jsonStr
	}

	// Find the closing quote for the code string value
	// We need to be careful - the string might contain escaped quotes or newlines
	var result strings.Builder
	result.WriteString(jsonStr[:valueStart])

	inString := true
	escapeNext := false
	for i := valueStart; i < len(jsonStr); i++ {
		char := jsonStr[i]

		if escapeNext {
			result.WriteByte(char)
			escapeNext = false
			continue
		}

		if char == '\\' {
			result.WriteByte(char)
			escapeNext = true
			continue
		}

		if char == '"' && inString {
			// Check if this is the closing quote for the code field
			// Look ahead to see if we're at the end of the JSON object or if there's a comma/brace
			// This is a heuristic - if we see } or , after whitespace, it's likely the closing quote
			remaining := jsonStr[i+1:]
			remaining = strings.TrimSpace(remaining)
			if len(remaining) > 0 && (remaining[0] == '}' || remaining[0] == ',') {
				// This is the closing quote
				result.WriteByte(char)
				result.WriteString(jsonStr[i+1:])
				return result.String()
			}
			// Otherwise, it's an unescaped quote inside the code string - escape it
			result.WriteString("\\\"")
		} else {
			result.WriteByte(char)
		}
	}

	// If we didn't find a closing quote, return original (might be incomplete JSON)
	return jsonStr
}

// stripMarkdownCodeBlocks removes markdown code block fences from the response
func (f *FlexibleLLMAdapter) stripMarkdownCodeBlocks(response string) string {
	cleaned := strings.TrimSpace(response)

	// Remove markdown code block fences (```json, ```, etc.)
	// Handle both ```json and ``` at start/end of response
	cleaned = strings.TrimPrefix(cleaned, "```json")
	cleaned = strings.TrimPrefix(cleaned, "```")
	cleaned = strings.TrimSuffix(cleaned, "```")
	cleaned = strings.TrimSpace(cleaned)

	// Also remove any remaining backticks that might be in the middle
	// But be careful - we only want to remove markdown fences, not backticks in code strings
	// So we only remove backticks at the very start/end after trimming
	if strings.HasPrefix(cleaned, "`") {
		cleaned = strings.TrimPrefix(cleaned, "`")
	}
	if strings.HasSuffix(cleaned, "`") {
		cleaned = strings.TrimSuffix(cleaned, "`")
	}
	cleaned = strings.TrimSpace(cleaned)

	return cleaned
}

// extractFirstJSONObject extracts the first complete {...} JSON object from text using proper brace matching
func (f *FlexibleLLMAdapter) extractFirstJSONObject(text string) string {
	// Find first '{' and track braces
	start := strings.Index(text, "{")
	if start == -1 {
		return ""
	}

	depth := 0
	inString := false
	escapeNext := false

	for i := start; i < len(text); i++ {
		char := text[i]

		// Handle escape sequences
		if escapeNext {
			escapeNext = false
			continue
		}

		if char == '\\' && inString {
			escapeNext = true
			continue
		}

		// Track string boundaries (only if not escaped)
		if char == '"' && !escapeNext {
			inString = !inString
			continue
		}

		// Only process braces when not inside a string
		if !inString {
			switch char {
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					return strings.TrimSpace(text[start : i+1])
				}
			}
		}
	}

	// If we didn't find a complete object, return what we have
	return strings.TrimSpace(text[start:])
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
	Image    string   `json:"image,omitempty"`    // for Type=="image": docker image reference
	Code     string   `json:"code,omitempty"`     // for Type=="code": code to execute
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
