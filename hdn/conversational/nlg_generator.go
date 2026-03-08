package conversational

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strings"
)

// NLGGenerator generates natural language responses from reasoning traces and results
type NLGGenerator struct {
	llmClient LLMClientInterface
}

// NLGRequest contains the input for natural language generation
type NLGRequest struct {
	UserMessage    string                 `json:"user_message"`
	Intent         *Intent                `json:"intent"`
	Action         *Action                `json:"action"`
	Result         *ActionResult          `json:"result"`
	Context        map[string]interface{} `json:"context"`
	ShowThinking   bool                   `json:"show_thinking"`
	ReasoningTrace *ReasoningTraceData    `json:"reasoning_trace"`
}

// NLGResponse contains the generated natural language response
type NLGResponse struct {
	Text       string                 `json:"text"`
	Confidence float64                `json:"confidence"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
}

// Action represents an action to be taken
type Action struct {
	Type       string                 `json:"type"`
	Goal       string                 `json:"goal"`
	Parameters map[string]interface{} `json:"parameters"`
}

// ActionResult represents the result of an action
type ActionResult struct {
	Type    string                 `json:"type"`
	Success bool                   `json:"success"`
	Data    map[string]interface{} `json:"data"`
	Error   string                 `json:"error,omitempty"`
}

// NewNLGGenerator creates a new natural language generator
func NewNLGGenerator(llmClient LLMClientInterface) *NLGGenerator {
	return &NLGGenerator{
		llmClient: llmClient,
	}
}

// validateResponseSafety checks if a generated response contains dangerous content
func (nlg *NLGGenerator) validateResponseSafety(responseText string) (bool, string) {
	lower := strings.ToLower(responseText)

	// Dangerous command patterns
	dangerousPatterns := []string{
		"rm -rf", "rm -rf /", "rm -rf *", "rm -rf /",
		"cd /; rm", "cd / && rm", "cd /; rm -rf",
		"format disk", "dd if=/dev/zero", "mkfs",
		"delete all files", "wipe all files", "destroy all",
		"sudo rm -rf", "sudo rm -rf /",
		"#!/bin/bash", "#!/bin/sh", // Block script generation
		"chmod 777", "chmod +x", // Dangerous permissions
	}

	for _, pattern := range dangerousPatterns {
		if strings.Contains(lower, pattern) {
			log.Printf("🚨 [NLG-SAFETY] Blocked dangerous content: %s", pattern)
			return false, fmt.Sprintf("Response contains dangerous command pattern: %s", pattern)
		}
	}

	// Check for code blocks containing dangerous commands
	if strings.Contains(lower, "```") {
		// Extract code blocks and check them
		codeBlockPattern := regexp.MustCompile("```[\\s\\S]*?```")
		codeBlocks := codeBlockPattern.FindAllString(lower, -1)
		for _, block := range codeBlocks {
			for _, pattern := range dangerousPatterns {
				if strings.Contains(block, pattern) {
					log.Printf("🚨 [NLG-SAFETY] Blocked dangerous code block with pattern: %s", pattern)
					return false, fmt.Sprintf("Response contains dangerous code: %s", pattern)
				}
			}
		}
	}

	return true, ""
}

// validateAndWrapResponse validates a response and returns a safe NLGResponse
func (nlg *NLGGenerator) validateAndWrapResponse(responseText string, responseType string, intentType string, confidence float64) *NLGResponse {
	text := strings.TrimSpace(responseText)

	// Safety check: validate response doesn't contain dangerous content
	if safe, reason := nlg.validateResponseSafety(text); !safe {
		log.Printf("🚨 [NLG-SAFETY] Blocked unsafe response: %s", reason)
		return &NLGResponse{
			Text:       "I cannot provide code or instructions that could be harmful or destructive. Please ask for help with a safe, constructive task instead.",
			Confidence: 0.1,
			Metadata: map[string]interface{}{
				"response_type": responseType,
				"intent_type":   intentType,
				"blocked":       true,
				"block_reason":  reason,
			},
		}
	}

	return &NLGResponse{
		Text:       text,
		Confidence: confidence,
		Metadata: map[string]interface{}{
			"response_type": responseType,
			"intent_type":   intentType,
		},
	}
}

// GenerateResponse generates a natural language response
func (nlg *NLGGenerator) GenerateResponse(ctx context.Context, req *NLGRequest) (*NLGResponse, error) {
	log.Printf("🗣️ [NLG] Generating response for intent: %s (action: %s)", req.Intent.Type, req.Action.Type)
	if req.Result != nil {
		log.Printf("🗣️ [NLG] Result available: type=%s, success=%v, data_keys=%d", req.Result.Type, req.Result.Success, len(req.Result.Data))
	}
	switch req.Action.Type {
	case "knowledge_query":
		return nlg.generateKnowledgeResponse(ctx, req)
	case "task_execution":
		return nlg.generateTaskResponse(ctx, req)
	case "planning":
		return nlg.generatePlanningResponse(ctx, req)
	case "learning":
		return nlg.generateLearningResponse(ctx, req)
	case "explanation":
		return nlg.generateExplanationResponse(ctx, req)
	case "general_conversation":
		return nlg.generateConversationResponse(ctx, req)
	default:
		return nlg.generateGenericResponse(ctx, req)
	}
}

// generateKnowledgeResponse generates a response for knowledge queries
func (nlg *NLGGenerator) generateKnowledgeResponse(ctx context.Context, req *NLGRequest) (*NLGResponse, error) {
	prompt := nlg.buildKnowledgePrompt(req)
	log.Printf("🗣️ [NLG] Knowledge prompt length: %d", len(prompt))

	response, err := nlg.llmClient.GenerateResponse(ctx, prompt, 500)
	if err != nil {
		return nlg.generateFallbackResponse(req, "knowledge query"), nil
	}

	return nlg.validateAndWrapResponse(response, "knowledge", req.Intent.Type, 0.8), nil
}

// generateTaskResponse generates a response for task execution
func (nlg *NLGGenerator) generateTaskResponse(ctx context.Context, req *NLGRequest) (*NLGResponse, error) {
	// For scrape results with extracted content, present directly without LLM summarization
	if extractedContent := nlg.getExtractedScrapeContent(req); extractedContent != "" {
		log.Printf("📤 [NLG] Returning extracted scrape content directly (%d chars), skipping LLM summarization", len(extractedContent))
		return &NLGResponse{
			Text:       extractedContent,
			Confidence: 0.9,
			Metadata: map[string]interface{}{
				"response_type": "task",
				"intent_type":   req.Intent.Type,
				"scrape_direct": true,
			},
		}, nil
	}

	prompt := nlg.buildTaskPrompt(req)

	response, err := nlg.llmClient.GenerateResponse(ctx, prompt, 400)
	if err != nil {
		return nlg.generateFallbackResponse(req, "task execution"), nil
	}

	return nlg.validateAndWrapResponse(response, "task", req.Intent.Type, 0.7), nil
}

// getExtractedScrapeContent checks if this task result is a scrape result with extracted_content
// and returns the content directly if found. It follows the data path:
// req.Result.Data["result"] → InterpretResult → Metadata["tool_result"] → results[0] → result → extracted_content
func (nlg *NLGGenerator) getExtractedScrapeContent(req *NLGRequest) string {
	if req.Result == nil || !req.Result.Success || req.Result.Data == nil {
		return ""
	}

	// Extract InterpretResult from data["result"]
	val := req.Result.Data["result"]
	if val == nil {
		return ""
	}

	var metadata map[string]interface{}
	if ir, ok := val.(*InterpretResult); ok {
		metadata = ir.Metadata
	} else if ir, ok := val.(InterpretResult); ok {
		metadata = ir.Metadata
	}
	if metadata == nil {
		return ""
	}

	// Get tool_result from metadata
	toolResult, ok := metadata["tool_result"].(map[string]interface{})
	if !ok {
		return ""
	}

	// Get results list
	var resultsList []interface{}
	if list, ok := toolResult["results"].([]interface{}); ok {
		resultsList = list
	}
	if len(resultsList) == 0 {
		return ""
	}

	// Check first item for extracted_content (directly or in nested "result")
	item, ok := resultsList[0].(map[string]interface{})
	if !ok {
		return ""
	}

	extractedContent := ""
	pageTitle := ""

	// Try top-level
	if ec, ok := item["extracted_content"].(string); ok && ec != "" {
		extractedContent = ec
		if pt, ok := item["page_title"].(string); ok {
			pageTitle = pt
		}
	}

	// Try nested "result" sub-map (scrape results structure)
	if extractedContent == "" {
		if innerResult, ok := item["result"].(map[string]interface{}); ok {
			if ec, ok := innerResult["extracted_content"].(string); ok && ec != "" {
				extractedContent = ec
				if pt, ok := innerResult["page_title"].(string); ok {
					pageTitle = pt
				}
			}
		}
	}

	if extractedContent == "" {
		return ""
	}

	// Strip any markdown formatting so the text is clean for voice/TTS output
	extractedContent = stripMarkdownFormatting(extractedContent)

	// Build a clean response with the extracted content
	var sb strings.Builder
	if pageTitle != "" {
		sb.WriteString(fmt.Sprintf("Here are the results from %s:\n\n", pageTitle))
	} else {
		sb.WriteString("Here are the scraped results:\n\n")
	}
	sb.WriteString(extractedContent)
	return sb.String()
}

// stripMarkdownFormatting removes markdown formatting characters (**, *, #, etc.)
// so that the text reads cleanly when spoken aloud by TTS.
func stripMarkdownFormatting(text string) string {
	// Remove bold markers: **text** or __text__
	text = regexp.MustCompile(`\*\*(.+?)\*\*`).ReplaceAllString(text, "$1")
	text = regexp.MustCompile(`__(.+?)__`).ReplaceAllString(text, "$1")
	// Remove italic markers: *text* or _text_ (but not underscores in words like my_var)
	text = regexp.MustCompile(`(?:^|[ (])\*([^*\n]+?)\*(?:$|[ ),.])`).ReplaceAllString(text, "$1")
	// Remove heading markers: ### heading
	text = regexp.MustCompile(`(?m)^#{1,6}\s+`).ReplaceAllString(text, "")
	// Remove inline code backticks
	text = regexp.MustCompile("`([^`]+)`").ReplaceAllString(text, "$1")
	// Remove leftover stray * at start of lines (bullet points → plain text)
	text = regexp.MustCompile(`(?m)^\s*\*\s+`).ReplaceAllString(text, "- ")
	return text
}

// generatePlanningResponse generates a response for planning requests
func (nlg *NLGGenerator) generatePlanningResponse(ctx context.Context, req *NLGRequest) (*NLGResponse, error) {
	prompt := nlg.buildPlanningPrompt(req)

	response, err := nlg.llmClient.GenerateResponse(ctx, prompt, 600)
	if err != nil {
		return nlg.generateFallbackResponse(req, "planning"), nil
	}

	return nlg.validateAndWrapResponse(response, "planning", req.Intent.Type, 0.8), nil
}

// generateLearningResponse generates a response for learning requests
func (nlg *NLGGenerator) generateLearningResponse(ctx context.Context, req *NLGRequest) (*NLGResponse, error) {
	prompt := nlg.buildLearningPrompt(req)

	response, err := nlg.llmClient.GenerateResponse(ctx, prompt, 500)
	if err != nil {
		return nlg.generateFallbackResponse(req, "learning"), nil
	}

	return nlg.validateAndWrapResponse(response, "learning", req.Intent.Type, 0.8), nil
}

// generateExplanationResponse generates a response for explanation requests
func (nlg *NLGGenerator) generateExplanationResponse(ctx context.Context, req *NLGRequest) (*NLGResponse, error) {
	prompt := nlg.buildExplanationPrompt(req)

	response, err := nlg.llmClient.GenerateResponse(ctx, prompt, 600)
	if err != nil {
		return nlg.generateFallbackResponse(req, "explanation"), nil
	}

	return nlg.validateAndWrapResponse(response, "explanation", req.Intent.Type, 0.8), nil
}

// generateConversationResponse generates a response for general conversation
func (nlg *NLGGenerator) generateConversationResponse(ctx context.Context, req *NLGRequest) (*NLGResponse, error) {
	prompt := nlg.buildConversationPrompt(req)

	response, err := nlg.llmClient.GenerateResponse(ctx, prompt, 300)
	if err != nil {
		return nlg.generateFallbackResponse(req, "conversation"), nil
	}

	return nlg.validateAndWrapResponse(response, "conversation", req.Intent.Type, 0.6), nil
}

// generateGenericResponse generates a generic response
func (nlg *NLGGenerator) generateGenericResponse(ctx context.Context, req *NLGRequest) (*NLGResponse, error) {
	prompt := nlg.buildGenericPrompt(req)

	response, err := nlg.llmClient.GenerateResponse(ctx, prompt, 300)
	if err != nil {
		return nlg.generateFallbackResponse(req, "generic"), nil
	}

	return nlg.validateAndWrapResponse(response, "generic", req.Intent.Type, 0.5), nil
}

// buildKnowledgePrompt builds a prompt for knowledge responses
func (nlg *NLGGenerator) buildKnowledgePrompt(req *NLGRequest) string {
	basePrompt := `You are an AI assistant with access to a knowledge base and reasoning capabilities. 
Based on the user's question and the information retrieved, provide a helpful and accurate answer.

User Question: "%s"
Intent: %s
Goal: %s

🚨 CRITICAL RULES:
1. You MUST use the information provided in the "Retrieved Information" and "Retrieved Personal Context" sections below.
2. DO NOT invent, make up, or hallucinate any data that is not explicitly shown in those sections.
3. If the "Retrieved Information" contains email data formatted as a list (starting with "[1]" or similar), you MUST copy and paste that entire formatted list EXACTLY as it appears. Do NOT re-describe it, do NOT add commentary. Just present the formatted list verbatim.
4. When you see formatted email data like:
   [1] [UNREAD]
       From: Name <email@domain.com>
       Subject: Subject line
   You MUST output it EXACTLY like that - no additional text before or after.
5. If no information is retrieved in either section, say so clearly - do NOT invent fake data.
6. NEVER provide code, scripts, or commands that could be harmful or destructive (e.g., rm -rf, format disk, delete all files).
7. NEVER generate bash scripts, shell commands, or executable code that could damage systems or data.
8. If the 'Retrieved Personal Context' contains information about Steven Fisher, assume this is the user you are talking to and answer accordingly.

Please provide a clear, informative answer. 

IMPORTANT: If the 'Retrieved Personal Context' section below contains information about the user (Steven Fisher), use it to answer as if you already know this information. Do not say 'I don't have access to personal information' if the answer is present in that section.
	
	If both the 'Retrieved Information' and 'Retrieved Personal Context' are empty, use your internal knowledge but add a brief note that no specific real-time updates were found.`

	// Add reasoning trace if available and requested
	if req.ShowThinking && req.ReasoningTrace != nil {
		basePrompt += fmt.Sprintf(`

Reasoning Process:
- Goal: %s
- FSM State: %s
- Actions Taken: %s
- Knowledge Sources: %s
- Tools Used: %s
- Key Decisions: %s

Please incorporate this reasoning context into your response.`,
			req.ReasoningTrace.CurrentGoal,
			req.ReasoningTrace.FSMState,
			strings.Join(req.ReasoningTrace.Actions, ", "),
			strings.Join(req.ReasoningTrace.KnowledgeUsed, ", "),
			strings.Join(req.ReasoningTrace.ToolsInvoked, ", "),
			nlg.formatDecisions(req.ReasoningTrace.Decisions),
		)
	}

	// Add memory context (summaries and personal facts)
	basePrompt = nlg.addMemoryContext(basePrompt, req)

	// Add result data if available
	if req.Result != nil && req.Result.Success {
		formattedData := nlg.formatResultData(req.Result.Data)
		basePrompt += fmt.Sprintf(`

Retrieved Information:
%s

🚨 CRITICAL: You MUST use ONLY the information provided above. DO NOT make up, invent, or hallucinate any data. 
If the "Retrieved Information" shows email data formatted as a list (e.g., "[1] [UNREAD]\n    From: ...\n    Subject: ..."), you MUST copy and paste that entire formatted list EXACTLY as it appears. Do NOT re-describe it, do NOT add commentary like "This email is from..." or "The subject line is...". Just present the formatted email list as-is.
If no information is provided, say so clearly - do NOT invent fake emails or data.`, formattedData)
	}

	return fmt.Sprintf(basePrompt, req.UserMessage, req.Intent.Type, req.Action.Goal)
}

// buildTaskPrompt builds a prompt for task execution responses
func (nlg *NLGGenerator) buildTaskPrompt(req *NLGRequest) string {
	basePrompt := `You are an AI assistant that has executed a task for the user. 
Provide a clear summary of what was accomplished and any relevant results.

User Request: "%s"
Task Goal: %s
Task Type: %s

Please provide a helpful summary of the task execution.`

	if req.Result != nil && req.Result.Success {
		basePrompt += fmt.Sprintf(`

Task Results:
%s

Summarize what was accomplished and any important outcomes.`, nlg.formatResultData(req.Result.Data))
	} else if req.Result != nil && !req.Result.Success {
		basePrompt += fmt.Sprintf(`

Task encountered an error: %s

Please explain what went wrong and suggest next steps.`, req.Result.Error)
	}

	return fmt.Sprintf(basePrompt, req.UserMessage, req.Action.Goal, req.Action.Type)
}

// buildPlanningPrompt builds a prompt for planning responses
func (nlg *NLGGenerator) buildPlanningPrompt(req *NLGRequest) string {
	basePrompt := `You are an AI assistant that has created a plan for the user. 
Present the plan in a clear, structured way that the user can easily follow.

User Request: "%s"
Planning Goal: %s

Please present the plan in a helpful and actionable format.`

	if req.Result != nil && req.Result.Success {
		basePrompt += fmt.Sprintf(`

Generated Plan:
%s

Present this plan clearly with step-by-step instructions.`, nlg.formatResultData(req.Result.Data))
	}

	return fmt.Sprintf(basePrompt, req.UserMessage, req.Action.Goal)
}

// buildLearningPrompt builds a prompt for learning responses
func (nlg *NLGGenerator) buildLearningPrompt(req *NLGRequest) string {
	basePrompt := `You are an AI assistant that has learned new information. 
Share what was learned in an educational and engaging way.

User Request: "%s"
Learning Topic: %s

Please share the new knowledge in a helpful and educational format.`

	if req.Result != nil && req.Result.Success {
		basePrompt += fmt.Sprintf(`

Learning Results:
%s

Present the new knowledge in an educational and engaging way.`, nlg.formatResultData(req.Result.Data))
	}

	return fmt.Sprintf(basePrompt, req.UserMessage, req.Action.Goal)
}

// buildExplanationPrompt builds a prompt for explanation responses
func (nlg *NLGGenerator) buildExplanationPrompt(req *NLGRequest) string {
	basePrompt := `You are an AI assistant providing an explanation. 
Give a clear, detailed explanation that helps the user understand the topic.

User Request: "%s"
Explanation Topic: %s

Please provide a comprehensive and clear explanation.`

	if req.Result != nil && req.Result.Success {
		basePrompt += fmt.Sprintf(`

Explanation Content:
%s

Present this explanation in a clear and educational way.`, nlg.formatResultData(req.Result.Data))
	}

	return fmt.Sprintf(basePrompt, req.UserMessage, req.Action.Goal)
}

// buildConversationPrompt builds a prompt for general conversation
func (nlg *NLGGenerator) buildConversationPrompt(req *NLGRequest) string {
	basePrompt := `You are a helpful AI assistant. Respond to the user's message in a friendly and helpful way.

User Message: "%s"

Please provide a helpful and engaging response.`

	// Add memory context
	basePrompt = nlg.addMemoryContext(basePrompt, req)

	return fmt.Sprintf(basePrompt, req.UserMessage)
}

// buildGenericPrompt builds a generic prompt
func (nlg *NLGGenerator) buildGenericPrompt(req *NLGRequest) string {
	basePrompt := `You are a helpful AI assistant. Respond to the user's message appropriately.

User Message: "%s"
Intent: %s
Goal: %s

Please provide a helpful response.`

	// Add memory context
	basePrompt = nlg.addMemoryContext(basePrompt, req)

	return fmt.Sprintf(basePrompt, req.UserMessage, req.Intent.Type, req.Action.Goal)
}

// generateFallbackResponse generates a fallback response when LLM fails
func (nlg *NLGGenerator) generateFallbackResponse(req *NLGRequest, responseType string) *NLGResponse {
	var response string

	switch responseType {
	case "knowledge":
		response = fmt.Sprintf("I understand you're asking about: %s. Let me help you with that.", req.UserMessage)
	case "task":
		response = fmt.Sprintf("I'll help you with: %s. Let me work on that for you.", req.UserMessage)
	case "planning":
		response = fmt.Sprintf("I'll create a plan for: %s. Let me think through this step by step.", req.UserMessage)
	case "learning":
		response = fmt.Sprintf("I'll learn about: %s. Let me gather information on this topic.", req.UserMessage)
	case "explanation":
		response = fmt.Sprintf("I'll explain: %s. Let me break this down for you.", req.UserMessage)
	default:
		response = fmt.Sprintf("I understand: %s. Let me help you with that.", req.UserMessage)
	}

	return &NLGResponse{
		Text:       response,
		Confidence: 0.3,
		Metadata: map[string]interface{}{
			"response_type": responseType,
			"fallback":      true,
		},
	}
}

// formatDecisions formats decision points for display
func (nlg *NLGGenerator) formatDecisions(decisions []DecisionPoint) string {
	if len(decisions) == 0 {
		return "None"
	}

	var formatted []string
	for _, decision := range decisions {
		formatted = append(formatted, fmt.Sprintf("%s -> %s (%.2f confidence)",
			decision.Description, decision.Chosen, decision.Confidence))
	}

	return strings.Join(formatted, "; ")
}

// formatResultData formats result data for display
func (nlg *NLGGenerator) formatResultData(data map[string]interface{}) string {
	if data == nil {
		log.Printf("⚠️ [NLG] formatResultData called with nil data")
		return "No data available"
	}

	log.Printf("🗣️ [NLG] formatResultData called with data keys: %v", getMapKeys(data))

	var sb strings.Builder

	// Helper to extract content from an InterpretResult or similar
	extractContent := func(val interface{}) string {
		if val == nil {
			return ""
		}

		var interpretedStr string
		var metadata map[string]interface{}

		// Try to handle both pointer and value types
		if ir, ok := val.(*InterpretResult); ok {
			interpretedStr = fmt.Sprintf("%v", ir.Interpreted)
			metadata = ir.Metadata
		} else if ir, ok := val.(InterpretResult); ok {
			interpretedStr = fmt.Sprintf("%v", ir.Interpreted)
			metadata = ir.Metadata
		} else {
			return fmt.Sprintf("%v", val)
		}

		// If we have metadata with a tool result, format the actual data
		if metadata != nil {
			log.Printf("🗣️ [NLG] Checking metadata for tool_result. Metadata keys: %v", getMapKeys(metadata))
			if toolResult, ok := metadata["tool_result"].(map[string]interface{}); ok {
				log.Printf("🗣️ [NLG] Found tool_result in metadata. Tool result keys: %v", getMapKeys(toolResult))
				var resultSb strings.Builder

				// Handle Weaviate or Neo4j results (list of objects)
				// We check for both []interface{} and []map[string]interface{} to avoid cast errors
				var resultsList []interface{}
				if list, ok := toolResult["results"].([]interface{}); ok {
					resultsList = list
				} else if list, ok := toolResult["results"].([]map[string]interface{}); ok {
					for _, item := range list {
						resultsList = append(resultsList, item)
					}
				}

				// Handle empty results
				if len(resultsList) == 0 {
					log.Printf("📧 [NLG] No results found in tool_result")
					return "No items found."
				}

				if len(resultsList) > 0 {
					// Check if this is email data (has Subject, From, To fields)
					firstItem, isEmailData := resultsList[0].(map[string]interface{})
					if isEmailData {
						// Log the keys of the first item for debugging
						var keys []string
						for k := range firstItem {
							keys = append(keys, k)
						}
						log.Printf("📧 [NLG] First item keys: %v", keys)
						// Also log nested result keys if present (for scrape results)
						if innerResult, ok := firstItem["result"].(map[string]interface{}); ok {
							var innerKeys []string
							for k := range innerResult {
								innerKeys = append(innerKeys, k)
							}
							log.Printf("📧 [NLG] Inner result keys: %v", innerKeys)
						}

						// Case-insensitive email detection
						hasSubject := false
						hasFrom := false
						for k := range firstItem {
							kLower := strings.ToLower(k)
							if kLower == "subject" {
								hasSubject = true
							}
							if kLower == "from" {
								hasFrom = true
							}
						}
						log.Printf("📧 [NLG] Email detection: hasSubject=%v, hasFrom=%v", hasSubject, hasFrom)
						if hasSubject || hasFrom {
							// Log how many emails we're formatting
							log.Printf("📧 [NLG] Formatting %d email(s) for display", len(resultsList))
							// Format as email list (only sender and subject)
							resultSb.WriteString(fmt.Sprintf("Found %d email(s):\n\n", len(resultsList)))
							for i, res := range resultsList {
								if item, ok := res.(map[string]interface{}); ok {
									// Case-insensitive field extraction
									subject := getStringFromMapCaseInsensitive(item, "subject")

									// Extract "from" field (might be string or complex object)
									var fromField interface{}
									keyLower := strings.ToLower("from")
									for k, v := range item {
										if strings.ToLower(k) == keyLower {
											fromField = v
											break
										}
									}
									from := extractEmailAddress(fromField)

									// Check for UNREAD label
									isUnread := false
									if labels, ok := item["labels"].([]interface{}); ok {
										for _, label := range labels {
											if labelMap, ok := label.(map[string]interface{}); ok {
												if name, ok := labelMap["name"].(string); ok && name == "UNREAD" {
													isUnread = true
													break
												}
											}
										}
									}

									unreadMark := ""
									if isUnread {
										unreadMark = " [UNREAD]"
									}

									resultSb.WriteString(fmt.Sprintf("[%d]%s\n", i+1, unreadMark))
									if from != "" {
										resultSb.WriteString(fmt.Sprintf("    From: %s\n", from))
									}
									if subject != "" {
										resultSb.WriteString(fmt.Sprintf("    Subject: %s\n", subject))
									}
									resultSb.WriteString("\n")
								}
							}
							return resultSb.String()
						}
					}

					// Default formatting for other data types
					resultSb.WriteString(fmt.Sprintf("Found %d relevant items:\n\n", len(resultsList)))
					for i, res := range resultsList {
						if item, ok := res.(map[string]interface{}); ok {
							// Check for scrape results with extracted_content — prioritize that
							// It may be at the top level OR nested inside a "result" sub-map
							extractedContent := getStringFromMap(item, "extracted_content")
							pageTitle := getStringFromMap(item, "page_title")
							if extractedContent == "" {
								// Check nested "result" sub-map (scrape results have this structure)
								if innerResult, ok := item["result"].(map[string]interface{}); ok {
									extractedContent = getStringFromMap(innerResult, "extracted_content")
									if pageTitle == "" {
										pageTitle = getStringFromMap(innerResult, "page_title")
									}
								}
							}
							if extractedContent != "" {
								extractedContent = stripMarkdownFormatting(extractedContent)
								if pageTitle != "" {
									resultSb.WriteString(fmt.Sprintf("Scraped page: %s\n\n", pageTitle))
								}
								resultSb.WriteString(fmt.Sprintf("Extracted content:\n%s\n", extractedContent))
								log.Printf("\U0001f4e4 [NLG] Using extracted_content for scrape result (%d chars)", len(extractedContent))
								continue
							}

							title := getStringFromMap(item, "title")
							text := getStringFromMap(item, "text")
							name := getStringFromMap(item, "name")
							defn := getStringFromMap(item, "definition")
							content := getStringFromMap(item, "content")
							source := getStringFromMap(item, "source")

							if title != "" {
								resultSb.WriteString(fmt.Sprintf("[%d] TITLE: %s\n", i+1, title))
							} else if name != "" {
								resultSb.WriteString(fmt.Sprintf("[%d] NAME: %s\n", i+1, name))
							} else if source != "" {
								resultSb.WriteString(fmt.Sprintf("[%d] SOURCE: %s\n", i+1, source))
							} else if title == "" && name == "" && source == "" {
								resultSb.WriteString(fmt.Sprintf("[%d] ITEM:\n", i+1))
							}

							if text != "" {
								// Limit text length to avoid blowing up prompt
								if len(text) > 800 {
									text = text[:800] + "..."
								}
								resultSb.WriteString(fmt.Sprintf("    CONTENT: %s\n", text))
							} else if defn != "" {
								resultSb.WriteString(fmt.Sprintf("    DEFINITION: %s\n", defn))
							} else if content != "" {
								if len(content) > 800 {
									content = content[:800] + "..."
								}
								resultSb.WriteString(fmt.Sprintf("    CONTENT: %s\n", content))
							}
							resultSb.WriteString("\n")
						}
					}
					return resultSb.String()
				}

				// Handle simple count + results results if not already handled
				if count, ok := toolResult["count"].(float64); ok && count > 0 {
					// This is a fallback in case the result structure is different
					return fmt.Sprintf("Retrieved %d matching records from knowledge base.", int(count))
				}
			}
		}

		return interpretedStr
	}

	// Check for combined results first
	if source, ok := data["source"].(string); ok && source == "neo4j_and_rag" {
		if neo4j, ok := data["neo4j_result"]; ok {
			content := extractContent(neo4j)
			if content != "" {
				sb.WriteString("### Knowledge Graph (Neo4j):\n")
				sb.WriteString(content)
				sb.WriteString("\n\n")
			}
		}
		if episodic, ok := data["episodic_memory"]; ok {
			content := extractContent(episodic)
			if content != "" {
				sb.WriteString("### Episodic Memory (Weaviate):\n")
				sb.WriteString(content)
				sb.WriteString("\n\n")
			}
		}
		if news, ok := data["news_articles"]; ok {
			content := extractContent(news)
			if content != "" {
				sb.WriteString("### News Articles (Weaviate):\n")
				sb.WriteString(content)
				sb.WriteString("\n\n")
			}
		}
		if avatar, ok := data["avatar_context"]; ok {
			content := extractContent(avatar)
			if content != "" {
				sb.WriteString("### Personal Background (AvatarContext):\n")
				sb.WriteString(content)
				sb.WriteString("\n\n")
			}
		}
		if sb.Len() > 0 {
			return sb.String()
		}
	}

	// Handle standard "result" key
	if result, ok := data["result"]; ok {
		content := extractContent(result)
		if content != "" && content != "No data available" {
			return content
		}
		// If extractContent returned empty, the tool result might not be in metadata
		// Try to extract it directly from the InterpretResult
		if ir, ok := result.(*InterpretResult); ok && ir.Metadata != nil {
			if toolResult, ok := ir.Metadata["tool_result"].(map[string]interface{}); ok {
				log.Printf("🗣️ [NLG] Found tool_result directly in InterpretResult metadata")
				// Format the tool result
				if results, ok := toolResult["results"].([]interface{}); ok && len(results) > 0 {
					// Check if this is email data (case-insensitive)
					if firstItem, ok := results[0].(map[string]interface{}); ok {
						hasSubject := false
						hasFrom := false
						for k := range firstItem {
							kLower := strings.ToLower(k)
							if kLower == "subject" {
								hasSubject = true
							}
							if kLower == "from" {
								hasFrom = true
							}
						}
						if hasSubject || hasFrom {
							// Log how many emails we're formatting
							log.Printf("📧 [NLG] Formatting %d email(s) from tool_result metadata", len(results))
							var emailSb strings.Builder
							emailSb.WriteString(fmt.Sprintf("Found %d email(s):\n\n", len(results)))
							for i, res := range results {
								if item, ok := res.(map[string]interface{}); ok {
									// Case-insensitive field extraction
									subject := getStringFromMapCaseInsensitive(item, "subject")

									// Extract "from" field (might be string or complex object)
									var fromField interface{}
									keyLower := strings.ToLower("from")
									for k, v := range item {
										if strings.ToLower(k) == keyLower {
											fromField = v
											break
										}
									}
									from := extractEmailAddress(fromField)

									isUnread := false
									if labels, ok := item["labels"].([]interface{}); ok {
										for _, label := range labels {
											if labelMap, ok := label.(map[string]interface{}); ok {
												if name, ok := labelMap["name"].(string); ok && name == "UNREAD" {
													isUnread = true
													break
												}
											}
										}
									}

									unreadMark := ""
									if isUnread {
										unreadMark = " [UNREAD]"
									}

									emailSb.WriteString(fmt.Sprintf("[%d]%s\n", i+1, unreadMark))
									if from != "" {
										emailSb.WriteString(fmt.Sprintf("    From: %s\n", from))
									}
									if subject != "" {
										emailSb.WriteString(fmt.Sprintf("    Subject: %s\n", subject))
									}
									emailSb.WriteString("\n")
								}
							}
							return emailSb.String()
						}
					}
				}
			}
		}
		return content
	}

	// Fallback to formatting the entire data structure
	fallback := fmt.Sprintf("%v", data)
	log.Printf("🗣️ [NLG] Result data formatted (length: %d)", len(fallback))
	return fallback
}

// getStringFromMap safely extracts a string value from a map
func getMapKeys(m map[string]interface{}) []string {
	if m == nil {
		return []string{}
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func getStringFromMap(m map[string]interface{}, key string) string {
	if val, exists := m[key]; exists && val != nil {
		if s, ok := val.(string); ok {
			return s
		}
		// If it's a number, convert to string
		if f, ok := val.(float64); ok {
			return fmt.Sprintf("%.2f", f)
		}
		return fmt.Sprintf("%v", val)
	}

	// Special case for Weaviate: properties might be in a nested "metadata" JSON string
	if metadataStr, ok := m["metadata"].(string); ok && metadataStr != "" {
		var metadata map[string]interface{}
		if err := json.Unmarshal([]byte(metadataStr), &metadata); err == nil {
			// Try looking in original_metadata if present
			if orig, ok := metadata["original_metadata"].(map[string]interface{}); ok {
				if val, exists := orig[key]; exists && val != nil {
					return fmt.Sprintf("%v", val)
				}
			}
			if val, exists := metadata[key]; exists && val != nil {
				return fmt.Sprintf("%v", val)
			}
		}
	}

	return ""
}

// extractEmailAddress extracts a clean email address from a "from" field that might be a string or complex object
func extractEmailAddress(fromField interface{}) string {
	if fromField == nil {
		return ""
	}

	// If it's already a string, return it
	if s, ok := fromField.(string); ok {
		return s
	}

	// If it's a map, try to extract the email address
	if m, ok := fromField.(map[string]interface{}); ok {
		// Try "address" field first
		if addr, ok := m["address"].(string); ok && addr != "" {
			name, _ := m["name"].(string)
			if name != "" {
				return fmt.Sprintf("%s <%s>", name, addr)
			}
			return addr
		}

		// Try "value" field which might contain an array
		if value, ok := m["value"]; ok {
			if arr, ok := value.([]interface{}); ok && len(arr) > 0 {
				if firstItem, ok := arr[0].(map[string]interface{}); ok {
					if addr, ok := firstItem["address"].(string); ok && addr != "" {
						name, _ := firstItem["name"].(string)
						if name != "" {
							return fmt.Sprintf("%s <%s>", name, addr)
						}
						return addr
					}
				}
			}
		}

		// Try "text" field (sometimes email is in text format)
		if text, ok := m["text"].(string); ok && text != "" {
			return text
		}
	}

	// Fallback: convert to string
	return fmt.Sprintf("%v", fromField)
}

// getStringFromMapCaseInsensitive extracts a string value from a map using case-insensitive key matching
func getStringFromMapCaseInsensitive(m map[string]interface{}, key string) string {
	keyLower := strings.ToLower(key)

	// First try exact match (most common case)
	if val, exists := m[key]; exists && val != nil {
		if s, ok := val.(string); ok {
			return s
		}
		if f, ok := val.(float64); ok {
			return fmt.Sprintf("%.2f", f)
		}
		return fmt.Sprintf("%v", val)
	}

	// Then try case-insensitive match
	for k, v := range m {
		if strings.ToLower(k) == keyLower && v != nil {
			if s, ok := v.(string); ok {
				return s
			}
			if f, ok := v.(float64); ok {
				return fmt.Sprintf("%.2f", f)
			}
			return fmt.Sprintf("%v", v)
		}
	}

	// Special case for Weaviate: properties might be in a nested "metadata" JSON string
	if metadataStr, ok := m["metadata"].(string); ok && metadataStr != "" {
		var metadata map[string]interface{}
		if err := json.Unmarshal([]byte(metadataStr), &metadata); err == nil {
			// Try looking in original_metadata if present
			if orig, ok := metadata["original_metadata"].(map[string]interface{}); ok {
				for k, v := range orig {
					if strings.ToLower(k) == keyLower && v != nil {
						return fmt.Sprintf("%v", v)
					}
				}
			}
			for k, v := range metadata {
				if strings.ToLower(k) == keyLower && v != nil {
					return fmt.Sprintf("%v", v)
				}
			}
		}
	}

	return ""
}

// addMemoryContext adds conversation summaries and personal context to a prompt
func (nlg *NLGGenerator) addMemoryContext(basePrompt string, req *NLGRequest) string {
	if req.Context == nil {
		return basePrompt
	}

	// 1. Add conversation summaries if available for continuity
	if summaries, ok := req.Context["conversation_summaries"].([]string); ok && len(summaries) > 0 {
		basePrompt += "\n\nRelevant Past Conversation Context (Summarized):\n"
		for _, summary := range summaries {
			basePrompt += fmt.Sprintf("--- SUMMARY ---\n%s\n", summary)
		}
		basePrompt += "\nUse these summaries to maintain continuity with what you've discussed with the user previously."
	}

	// 2. Add avatar context (personal info) if available
	if avatarData, ok := req.Context["avatar_context"].(*InterpretResult); ok && avatarData != nil {
		if toolResult, ok := avatarData.Metadata["tool_result"].(map[string]interface{}); ok {
			var items []interface{}
			if i, ok := toolResult["results"].([]interface{}); ok {
				items = i
			} else if i, ok := toolResult["results"].([]map[string]interface{}); ok {
				for _, item := range i {
					items = append(items, item)
				}
			}

			if len(items) > 0 {
				basePrompt += "\n\nRetrieved Personal Context (About Steven Fisher / User):\n"
				for _, res := range items {
					if item, ok := res.(map[string]interface{}); ok {
						if content, ok := item["content"].(string); ok {
							basePrompt += fmt.Sprintf("- %s\n", content)
						} else if text, ok := item["text"].(string); ok {
							basePrompt += fmt.Sprintf("- %s\n", text)
						}
					}
				}
				basePrompt += "\nUse this personal context to correctly answer questions about the user's background or preferences."
			}
		}
	}

	// 3. Add wiki/news context if available
	if wikiData, ok := req.Context["wiki_context"].(*InterpretResult); ok && wikiData != nil {
		if toolResult, ok := wikiData.Metadata["tool_result"].(map[string]interface{}); ok {
			var items []interface{}
			if i, ok := toolResult["results"].([]interface{}); ok {
				items = i
			} else if i, ok := toolResult["results"].([]map[string]interface{}); ok {
				for _, item := range i {
					items = append(items, item)
				}
			}

			if len(items) > 0 {
				basePrompt += "\n\nRetrieved News/Knowledge (AgiWiki):\n"
				for _, res := range items {
					if item, ok := res.(map[string]interface{}); ok {
						if content, ok := item["content"].(string); ok {
							basePrompt += fmt.Sprintf("--- ARTICLE ---\n%s\n", content)
						} else if text, ok := item["text"].(string); ok {
							basePrompt += fmt.Sprintf("--- ARTICLE ---\n%s\n", text)
						}
					}
				}
				basePrompt += "\nUse this information to provide the latest news or factual details requested by the user."
			}
		}
	}

	return basePrompt
}
