package conversational

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strings"
)

var (
	listCleanupRegex  = regexp.MustCompile(`^(\d+\.|\*|-|•)\s*`)
	numberedListRegex = regexp.MustCompile(`\s*\d+\.\s+`)
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
	log.Printf("🗣️ [NLG] Task prompt length: %d", len(prompt))
	if len(prompt) > 500000 {
		log.Printf("⚠️ [NLG] VERY LARGE PROMPT detected: %d bytes", len(prompt))
	}

	response, err := nlg.llmClient.GenerateResponse(ctx, prompt, 600)
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
// and also formats list-style outputs for TTS/chatbot use.
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

	// Format numbered/bulleted lists: put each item on its own line.
	// Only match a digit sequence followed by ". " when preceded by whitespace or
	// start-of-string, to avoid splitting mid-sentence numbers (e.g. "in 2022. As").
	// Numbered lists
	text = regexp.MustCompile(`(?m)(^|\s)(\d+)\.\s+`).ReplaceAllStringFunc(text, func(s string) string {
		// Keep the captured leading whitespace/newline stripped; insert our own newline.
		trimmed := strings.TrimLeft(s, " \t\r\n")
		return "\n" + trimmed
	})
	// Bulleted lists
	text = regexp.MustCompile(`\s-\s+`).ReplaceAllString(text, "\n- ")

	// Format URLs as <URL:...> blocks for TTS/chatbot skipping.
	// Matches full http(s):// URLs and any relative path like /news/..., /sport/..., etc.
	urlPattern := regexp.MustCompile(`(https?://[\w\-\./?%&=#:~@+]+|/[\w][\w\-/]*(?:\?[\w=&%+\-\.]*)?)`)
	text = urlPattern.ReplaceAllStringFunc(text, func(url string) string {
		return "<URL:" + url + ">"
	})

	// Remove accidental double newlines
	text = regexp.MustCompile(`\n{3,}`).ReplaceAllString(text, "\n\n")
	// Trim leading/trailing whitespace
	return strings.TrimSpace(text)
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
2. DO NOT repeat, echo, or include any of the labels or data from the "Reasoning Process" or "Retrieved Information" sections in your final response. 
3. Provide ONLY the final natural language answer to the user. DO NOT include metadata like confidence scores, goals, or action summaries.
4. DO NOT invent, make up, or hallucinate any data that is not explicitly shown in those sections.
5. If the "Retrieved Information" contains email data or list data, present it cleanly and formatted for the user.
6. If no information is retrieved in either section, say so clearly - do NOT invent fake data.
7. NEVER provide code, scripts, or commands that could be harmful or destructive.
8. If the 'Retrieved Personal Context' contains information about Steven Fisher, assume this is the user you are talking to.

Please provide a clear, informative answer. 

IMPORTANT: If both the 'Retrieved Information' and 'Retrieved Personal Context' are empty, use your internal knowledge but add a brief note that no specific real-time updates were found.`

	// Add reasoning trace if available and requested
	if req.ShowThinking && req.ReasoningTrace != nil {
		basePrompt += fmt.Sprintf(`

Reasoning Context (FOR INTERNAL USE ONLY - DO NOT REPEAT IN RESPONSE):
- Goal: %s
- FSM State: %s
- Actions Taken: %s
- Knowledge Sources: %s
- Tools Used: %s
- Key Decisions: %s

Use the above context to inform your tone and state of mind, but provide ONLY a clean response to the user's original request.`,
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
Provide a clear, natural summary of what was accomplished and any relevant results.

User Request: "%s"
Task Goal: %s
Task Type: %s

🚨 CRITICAL RULES:
1. Provide ONLY the final natural language response to the user.
2. DO NOT include any labels like "Goal:", "Task:", "Result:", or "Reasoning:".
3. DO NOT include confidence scores or metadata.
4. DO NOT repeat the user's request verbatim.
5. Just tell the user what you did and show them the results.

Please provide a helpful summary of the task execution.`

	if req.Result != nil && req.Result.Success {
		resultSummary := nlg.formatResultData(req.Result.Data)
		if len(resultSummary) > 10000 {
			resultSummary = resultSummary[:10000] + "... [TRUNCATED]"
		}
		basePrompt += "\n\nTask Results:\n" + resultSummary + "\n\nSummarize what was accomplished and any important outcomes."
	} else if req.Result != nil && !req.Result.Success {
		basePrompt += "\n\nTask encountered an error: " + req.Result.Error + "\n\nPlease explain what went wrong and suggest next steps."
	}

	// Use strings.Replace to safely interpolate the first %s for User Message
	// This avoids issues if the result summary contains % characters
	finalPrompt := strings.Replace(basePrompt, "%s", req.UserMessage, 1)
	// Replace %s for Goal and Type if they are in the template
	finalPrompt = strings.Replace(finalPrompt, "%s", req.Action.Goal, 1)
	finalPrompt = strings.Replace(finalPrompt, "%s", req.Action.Type, 1)

	return finalPrompt
}

// buildPlanningPrompt builds a prompt for planning responses
func (nlg *NLGGenerator) buildPlanningPrompt(req *NLGRequest) string {
	basePrompt := `You are an AI assistant that has created a plan for the user. 
Present the plan in a clear, structured way that the user can easily follow.

User Request: "%s"
Planning Goal: %s

🚨 CRITICAL RULES:
1. Provide ONLY the plan itself.
2. DO NOT include labels like "Goal:", "Planning:", or "Result:".
3. DO NOT echo the background reasoning.

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

// formatToolResult recursively flattens and formats complex tool results into a human-readable string
func (nlg *NLGGenerator) formatToolResult(result interface{}) string {
	return nlg.formatToolResultInternal(result, 0)
}

func (nlg *NLGGenerator) formatToolResultInternal(result interface{}, depth int) string {
	if result == nil || depth > 5 { // Hard depth limit to prevent circular recursion
		return ""
	}

	// Handle maps (dictionaries)
	if m, ok := result.(map[string]interface{}); ok {
		// 1. Try to find "main" content keys - check nested "result" first
		if inner, ok := m["result"].(map[string]interface{}); ok {
			return nlg.formatToolResultInternal(inner, depth+1)
		}

		contentKeys := []string{"extracted_content", "headlines", "results", "items", "content", "summary", "text", "message"}
		for _, k := range contentKeys {
			if val, exists := m[k]; exists && val != nil {
				return nlg.formatToolResultInternal(val, depth+1)
			}
		}

		// 2. If it's a simple map with one key, use its value
		if len(m) == 1 {
			for _, v := range m {
				return nlg.formatToolResultInternal(v, depth+1)
			}
		}

		// 3. Special handling for email-like records
		from := getStringFromMapCaseInsensitive(m, "from")
		subject := getStringFromMapCaseInsensitive(m, "subject")
		if from != "" || subject != "" {
			var sb strings.Builder
			if from != "" {
				sb.WriteString(fmt.Sprintf("From: %s", from))
			}
			if subject != "" {
				if sb.Len() > 0 {
					sb.WriteString(" | ")
				}
				sb.WriteString(fmt.Sprintf("Subject: %s", subject))
			}
			return sb.String()
		}

		// 4. Fallback: format as key-value pairs
		var lines []string
		for k, v := range m {
			// Skip technical/large fields
			if k == "raw_html" || k == "cleaned_html" || k == "screenshot" || k == "cookies" || k == "extraction_method" {
				continue
			}
			// Safely format value using recursive call instead of %v
			valStr := nlg.formatToolResultInternal(v, depth+1)
			if len(valStr) > 500 {
				valStr = valStr[:500] + "..."
			}
			lines = append(lines, fmt.Sprintf("%s: %s", k, valStr))
			if len(lines) >= 20 { // Cap fallback keys
				lines = append(lines, "... [TRUNCATED]")
				break
			}
		}
		if len(lines) == 0 {
			return ""
		}
		return strings.Join(lines, ", ")
	}

	// Handle slices (lists)
	if s, ok := result.([]interface{}); ok {
		var lines []string
		for i, item := range s {
			if i >= 100 { // Limit to 100 items to prevent OOM
				lines = append(lines, fmt.Sprintf("... [AND %d MORE ITEMS TRUNCATED]", len(s)-100))
				break
			}
			line := nlg.formatToolResultInternal(item, depth+1)
			if line != "" {
				lines = append(lines, fmt.Sprintf("[%d] %s", i+1, line))
			}
		}
		res := strings.Join(lines, "\n")
		if len(res) > 50000 { // 50KB limit per sub-list result
			return res[:50000] + "... [TRUNCATED]"
		}
		return res
	}

	// Handle strings
	if s, ok := result.(string); ok {
		s = strings.TrimSpace(s)

		// If it has multiple lines, format as bullets
		if strings.Contains(s, "\n") {
			lines := strings.Split(s, "\n")
			var cleaned []string
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if line != "" {
					line = listCleanupRegex.ReplaceAllString(line, "")
					cleaned = append(cleaned, "• "+line)
				}
			}
			return strings.Join(cleaned, "\n")
		}

		// Check for numbered list without newlines
		if numberedListRegex.MatchString(s) {
			parts := numberedListRegex.Split(s, -1)
			var cleaned []string
			for _, p := range parts {
				p = strings.TrimSpace(p)
				if p != "" {
					cleaned = append(cleaned, "• "+p)
				}
			}
			if len(cleaned) > 1 {
				return strings.Join(cleaned, "\n")
			}
		}

		return s
	}

	// Fallback for other types
	s := ""
	switch v := result.(type) {
	case string:
		s = v
	case []byte:
		s = string(v)
	case map[string]interface{}:
		// Format map keys only or summarize to prevent OOM
		keys := getMapKeys(v)
		if len(keys) > 10 {
			s = fmt.Sprintf("Map with %d keys: [%s, ...]", len(keys), strings.Join(keys[:10], ", "))
		} else {
			s = fmt.Sprintf("Map with keys: [%s]", strings.Join(keys, ", "))
		}
	case []interface{}:
		s = fmt.Sprintf("Array of length %d", len(v))
		if len(v) > 0 {
			// Try to summarize the first item briefly
			s += fmt.Sprintf(" (First item: %v)", nlg.formatToolResultInternal(v[0], 0))
			if len(s) > 200 {
				s = s[:200] + "..."
			}
		}
	default:
		// Safe summary for unknown types
		s = fmt.Sprintf("Object of type %T", result)
	}

	if len(s) > 10000 {
		return s[:10000] + "... [TRUNCATED]"
	}
	return s
}

// formatResultData formats result data for display
func (nlg *NLGGenerator) formatResultData(data map[string]interface{}) string {
	if data == nil {
		return "No data available"
	}

	// Start with a Clean Extraction attempt for "result" key
	if val, ok := data["result"]; ok {
		var interpreted interface{}
		if ir, ok := val.(*InterpretResult); ok {
			interpreted = ir.Interpreted
			if ir.Metadata != nil {
				if tr, ok := ir.Metadata["tool_result"]; ok {
					return nlg.formatToolResultInternal(tr, 0)
				}
			}
		} else if ir, ok := val.(InterpretResult); ok {
			interpreted = ir.Interpreted
			if ir.Metadata != nil {
				if tr, ok := ir.Metadata["tool_result"]; ok {
					return nlg.formatToolResultInternal(tr, 0)
				}
			}
		} else {
			interpreted = val
		}

		formatted := nlg.formatToolResultInternal(interpreted, 0)
		if formatted != "" && formatted != "No data available" {
			return formatted
		}
	}

	// Generic fallback for all keys in data
	var sb strings.Builder
	for k, v := range data {
		formatted := nlg.formatToolResultInternal(v, 0)
		if formatted != "" {
			if sb.Len() > 0 {
				sb.WriteString("\n\n")
			}
			sb.WriteString(fmt.Sprintf("%s:\n%s", strings.ToUpper(k), formatted))
		}
	}

	if sb.Len() == 0 {
		return "No relevant data found."
	}
	res := sb.String()
	if len(res) > 256*1024 { // 256KB limit
		return res[:256*1024] + "... [TRUNCATED due to size]"
	}
	return res
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
		basePrompt += "\n\nBACKGROUND: Relevant Past Conversation Context (Summarized):\n"
		for _, summary := range summaries {
			if len(summary) > 2000 {
				summary = summary[:2000] + "... [TRUNCATED]"
			}
			basePrompt += fmt.Sprintf("--- SUMMARY ---\n%s\n", summary)
		}
		basePrompt += "\nUse these summaries ONLY for continuity. Do NOT repeat them unless relevant to the current request."
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
				basePrompt += "\n\nBACKGROUND: User Profile Context (About Steven Fisher / User):\n"
				for _, res := range items {
					if item, ok := res.(map[string]interface{}); ok {
						if content, ok := item["content"].(string); ok {
							if len(content) > 2000 {
								content = content[:2000] + "... [TRUNCATED]"
							}
							basePrompt += fmt.Sprintf("- %s\n", content)
						} else if text, ok := item["text"].(string); ok {
							if len(text) > 2000 {
								text = text[:2000] + "... [TRUNCATED]"
							}
							basePrompt += fmt.Sprintf("- %s\n", text)
						}
					}
				}
				basePrompt += "\nUse this professional/personal background ONLY to inform your tone or if specifically asked about the user. Do NOT summarize this profile unless requested."
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
							if len(content) > 3000 {
								content = content[:3000] + "... [TRUNCATED]"
							}
							basePrompt += fmt.Sprintf("--- ARTICLE ---\n%s\n", content)
						} else if text, ok := item["text"].(string); ok {
							if len(text) > 3000 {
								text = text[:3000] + "... [TRUNCATED]"
							}
							basePrompt += fmt.Sprintf("--- ARTICLE ---\n%s\n", text)
						}
					}
				}
				basePrompt += "\nUse this information to provide the latest news or factual details requested by the user."
			}
		}
	}

	// 4. Add news context if available separately
	if newsData, ok := req.Context["news_context"].(*InterpretResult); ok && newsData != nil {
		if toolResult, ok := newsData.Metadata["tool_result"].(map[string]interface{}); ok {
			var items []interface{}
			if i, ok := toolResult["results"].([]interface{}); ok {
				items = i
			}

			if len(items) > 0 {
				basePrompt += "\n\nRecent News & Wikipedia Context:\n"
				for _, res := range items {
					if item, ok := res.(map[string]interface{}); ok {
						if snippet, ok := item["snippet"].(string); ok {
							if len(snippet) > 1000 {
								snippet = snippet[:1000] + "... [TRUNCATED]"
							}
							basePrompt += fmt.Sprintf("- %s\n", snippet)
						} else if content, ok := item["content"].(string); ok {
							if len(content) > 1000 {
								content = content[:1000] + "... [TRUNCATED]"
							}
							basePrompt += fmt.Sprintf("- %s\n", content)
						}
					}
				}
			}
		}
	}

	return basePrompt
}
