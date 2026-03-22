package conversational

import (
	"context"
	"encoding/json"
	"fmt"
	"hdn/utils"
	"log"
	"os"
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
	// For image generation, skip LLM summarization entirely — just confirm briefly.
	// The image is already displayed on-screen; we don't want a verbose description replacing it.
	if req.Action != nil && isImageGenerationAction(req.Action.Goal, req.UserMessage) {
		return &NLGResponse{
			Text:       "✅ Image generated.",
			Confidence: 0.95,
			Metadata: map[string]interface{}{
				"response_type": "task",
				"intent_type":   req.Intent.Type,
				"image_skip":    true,
			},
		}, nil
	}

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

// isImageGenerationAction returns true when the action goal / user message relates to image generation.
func isImageGenerationAction(goal, userMessage string) bool {
	lower := strings.ToLower(goal + " " + userMessage)
	keywords := []string{"generate_image", "generate image", "create image", "create an image", "make image", "draw image", "image generation"}
	for _, kw := range keywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
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
	var sb strings.Builder
	sb.WriteString("You are an AI assistant with access to a knowledge base and reasoning capabilities.\n")
	sb.WriteString("Based on the user's question and the information retrieved, provide a clean, helpful, and accurate answer.\n\n")

	sb.WriteString("User Question: \"")
	sb.WriteString(req.UserMessage)
	sb.WriteString("\"\nIntent: ")
	sb.WriteString(req.Intent.Type)
	sb.WriteString("\nGoal: ")
	sb.WriteString(req.Action.Goal)
	sb.WriteString("\n\n🚨 CRITICAL RULES:\n")
	sb.WriteString("1. You MUST use the information provided in the \"Knowledge/Intelligence Results\" and \"Information from Memory/Bio\" sections below, but ONLY if they are relevant to the user's specific question.\n")
	sb.WriteString("2. DO NOT repeat, echo, or include any of the labels or data from the \"Reasoning Process\" or \"Knowledge/Intelligence Results\" sections in your final response.\n")
	userName := os.Getenv("USER_NAME")
	if userName == "" {
		userName = "User"
	}
	sb.WriteString("3. Provide ONLY a clean natural language answer. 🚨 NO PREAMBLES (e.g., 'Hello " + userName + "!', 'I'd be happy to help...', 'Unfortunately...'). Just answer the question.\n")
	sb.WriteString("4. DO NOT invent, make up, or hallucinate any data that is not explicitly shown in those sections.\n")
	sb.WriteString("5. If the \"Knowledge/Intelligence Results\" contains email data or list data, present it cleanly and formatted for the user.\n")
	sb.WriteString("6. If information is present in ONE section but not the other, just use what is available. Do NOT mention that the other section was empty.\n")
	sb.WriteString("7. NEVER provide code, scripts, or commands that could be harmful or destructive.\n")
	sb.WriteString("8. If the 'Information from Memory/Bio' contains information about " + userName + ", assume this is the user you are talking to.\n")
	sb.WriteString("9. Use a natural, direct tone. DO NOT start every response with formal disclaimers or polite filler.\n")
	sb.WriteString("10. Stay focused on the CURRENT message. DO NOT volunteer updates about your knowledge gaps or what you 'couldn't find'. If you can't find it, answer based on common sense or ask a short clarifying question.\n")

	sb.WriteString("Please provide a clear, informative answer.\n\n")
	sb.WriteString("IMPORTANT: If relevant information is found in either the 'Knowledge/Intelligence Results' or 'Information from Memory/Bio' sections, use it to provide a direct answer. Do NOT explain which section the info came from. Just answer the question naturally.\n")

	// Add reasoning trace if available and requested
	if req.ShowThinking && req.ReasoningTrace != nil {
		sb.WriteString("\nReasoning Context (FOR INTERNAL USE ONLY - DO NOT REPEAT IN RESPONSE):\n")
		sb.WriteString("- Goal: " + req.ReasoningTrace.CurrentGoal + "\n")
		sb.WriteString("- FSM State: " + req.ReasoningTrace.FSMState + "\n")
		sb.WriteString("- Actions Taken: " + strings.Join(req.ReasoningTrace.Actions, ", ") + "\n")
		sb.WriteString("- Knowledge Sources: " + strings.Join(req.ReasoningTrace.KnowledgeUsed, ", ") + "\n")
		sb.WriteString("- Tools Used: " + strings.Join(req.ReasoningTrace.ToolsInvoked, ", ") + "\n")
		sb.WriteString("- Key Decisions: " + nlg.formatDecisions(req.ReasoningTrace.Decisions) + "\n")
		sb.WriteString("\nUse the above context to inform your tone and state of mind, but provide ONLY a clean response to the user's original request.\n")
	}

	// Add memory context (summaries and personal facts)
	prompt := nlg.addMemoryContext(sb.String(), req)

	// Add result data if available
	if req.Result != nil && req.Result.Success {
		formattedData := nlg.formatResultData(req.Result.Data)
		// Truncate massive results to protect LLM context
		if len(formattedData) > 50000 {
			formattedData = formattedData[:50000] + "... [RESULTS TRUNCATED]"
		}

		prompt += "\n\nKnowledge/Intelligence Results:\n" + formattedData + "\n\n"
	} else if req.Result != nil && !req.Result.Success {
		prompt += "\n\n⚠️ DIAGNOSTIC: The requested tool or knowledge query FAILED.\n"
		prompt += "Error: " + req.Result.Error + "\n"
		prompt += "Do NOT assume you have the information. Explain the error to the user and suggest they check again later.\n\n"
	}

	// Final aggressive rules at the VERY END to overcome long context
	prompt += "\n\n🚨 FINAL CRITICAL INSTRUCTIONS (MANDATORY):\n"
	prompt += "1. YOU ARE TALKING DIRECTLY TO THE USER. Use 'you' and 'your'. NEVER use the user's name (" + userName + ") or speak in the third person.\n"
	prompt += "2. NO PREAMBLES. Do not say 'Hello', 'I'd be happy to help', or 'Based on...'.\n"
	prompt += "3. NO DISCLAIMERS. Do not say 'Unfortunately', 'I couldn't find', or 'My knowledge base says...'.\n"
	prompt += "4. If you have partial information, just give it. Do NOT explain that it is partial.\n"
	prompt += "5. DO NOT volunteer information about knowledge gaps. Just answer the question directly.\n"
	prompt += "6. Provide ONLY the natural language answer. No labels, no metadata.\n"
	prompt += "7. 🖼️ IMAGE CONFIRMATION: If 'tool_generate_image' was used, assume the result is already visible. Briefly confirm (e.g., 'I've updated the background to red.') instead of suggesting the user search for it.\n"

	// Final safety truncation
	if len(prompt) > 400000 {
		prompt = prompt[:400000] + "... [PROMPT TRUNCATED]"
	}

	return prompt
}

// buildTaskPrompt builds a prompt for task execution responses
func (nlg *NLGGenerator) buildTaskPrompt(req *NLGRequest) string {
	var sb strings.Builder
	sb.WriteString("You are an AI assistant that has executed a task for the user.\n")
	sb.WriteString("Provide a clear, natural summary of what was accomplished and any relevant results.\n\n")

	sb.WriteString("User Request: \"")
	sb.WriteString(req.UserMessage)
	sb.WriteString("\"\nTask Goal: ")
	sb.WriteString(req.Action.Goal)
	sb.WriteString("\nTask Type: ")
	sb.WriteString(req.Action.Type)
	sb.WriteString("\n\n🚨 CRITICAL RULES:\n")
	sb.WriteString("1. Provide ONLY the final natural language response to the user.\n")
	sb.WriteString("2. DO NOT include any labels like \"Goal:\", \"Task:\", \"Result:\", or \"Reasoning:\".\n")
	sb.WriteString("3. DO NOT include confidence scores or metadata.\n")
	sb.WriteString("4. DO NOT volunteer information about your knowledge gaps or mention what you 'couldn't find' unless it is absolutely necessary for the answer.\n")
	sb.WriteString("5. Just tell the user what you did and show them the results. Be concise.\n\n")

	sb.WriteString("Please provide a helpful, direct summary of the task results.")

	if req.Result != nil && req.Result.Success {
		resultSummary := nlg.formatResultData(req.Result.Data)
		if len(resultSummary) > 50000 {
			resultSummary = resultSummary[:50000] + "... [TRUNCATED]"
		}
		sb.WriteString("\n\nTask Results:\n")
		sb.WriteString(resultSummary)
		sb.WriteString("\n\nSummarize what was accomplished and any important outcomes.")
	} else if req.Result != nil && !req.Result.Success {
		sb.WriteString("\n\nTask encountered an error: ")
		sb.WriteString(req.Result.Error)
		sb.WriteString("\n\nPlease explain what went wrong and suggest next steps.")
	}

	// Add memory context (summaries and personal facts)
	prompt := nlg.addMemoryContext(sb.String(), req)

	// Final aggressive rules at the VERY END
	prompt += "\n\n🚨 FINAL CRITICAL INSTRUCTIONS (MANDATORY):\n"
	prompt += "1. YOU ARE TALKING DIRECTLY TO THE USER. Use 'you' and 'your'. NEVER use the user's name (Steven Fisher) or speak in the third person.\n"
	prompt += "2. NO PREAMBLES. Do not say 'Hello', 'I'd be happy to help', or 'Based on...'.\n"
	prompt += "3. NO DISCLAIMERS. Do not say 'Unfortunately', 'I couldn't find', or 'My knowledge base says...'.\n"
	prompt += "4. If you have partial information, just give it. Do NOT explain that it is partial.\n"
	prompt += "5. DO NOT volunteer information about knowledge gaps. Just answer the question directly.\n"
	prompt += "6. Provide ONLY the natural language answer. No labels, no metadata.\n"

	// Final safety truncation
	if len(prompt) > 400000 {
		prompt = prompt[:400000] + "... [PROMPT TRUNCATED]"
	}

	return prompt
}

// buildPlanningPrompt builds a prompt for planning responses
func (nlg *NLGGenerator) buildPlanningPrompt(req *NLGRequest) string {
	var sb strings.Builder
	sb.WriteString("You are an AI assistant that has created a plan for the user.\n")
	sb.WriteString("Present the plan in a clear, structured way that the user can easily follow.\n\n")

	sb.WriteString("User Request: \"")
	sb.WriteString(req.UserMessage)
	sb.WriteString("\"\nPlanning Goal: ")
	sb.WriteString(req.Action.Goal)
	sb.WriteString("\n\n🚨 CRITICAL RULES:\n")
	sb.WriteString("1. Provide ONLY the plan itself.\n")
	sb.WriteString("2. DO NOT include labels like \"Goal:\", \"Planning:\", or \"Result:\".\n")
	sb.WriteString("3. DO NOT echo the background reasoning.\n\n")

	sb.WriteString("Please present the plan in a helpful and actionable format.")

	if req.Result != nil && req.Result.Success {
		plan := nlg.formatResultData(req.Result.Data)
		if len(plan) > 50000 {
			plan = plan[:50000] + "... [TRUNCATED]"
		}
		sb.WriteString("\n\nGenerated Plan:\n")
		sb.WriteString(plan)
		sb.WriteString("\n\nPresent this plan clearly with step-by-step instructions.")
	}

	prompt := sb.String()
	if len(prompt) > 400000 {
		prompt = prompt[:400000] + "... [PROMPT TRUNCATED]"
	}
	return prompt
}

// buildLearningPrompt builds a prompt for learning responses
func (nlg *NLGGenerator) buildLearningPrompt(req *NLGRequest) string {
	var sb strings.Builder
	sb.WriteString("You are an AI assistant that has learned new information.\n")
	sb.WriteString("Share what was learned in an educational and engaging way.\n\n")

	sb.WriteString("User Request: \"")
	sb.WriteString(req.UserMessage)
	sb.WriteString("\"\nLearning Topic: ")
	sb.WriteString(req.Action.Goal)
	sb.WriteString("\n\nPlease share the new knowledge in a helpful and educational format.")

	if req.Result != nil && req.Result.Success {
		learning := nlg.formatResultData(req.Result.Data)
		if len(learning) > 50000 {
			learning = learning[:50000] + "... [TRUNCATED]"
		}
		sb.WriteString("\n\nLearning Results:\n")
		sb.WriteString(learning)
		sb.WriteString("\n\nPresent the new knowledge in an educational and engaging way.")
	}

	prompt := sb.String()
	if len(prompt) > 400000 {
		prompt = prompt[:400000] + "... [PROMPT TRUNCATED]"
	}
	return prompt
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
	var sb strings.Builder
	sb.WriteString("You are a helpful AI assistant. Respond to the user's message in a friendly and helpful way.\n\n")
	sb.WriteString("User Message: \"")
	sb.WriteString(req.UserMessage)
	sb.WriteString("\"\n\n🚨 CRITICAL RULES:\n")
	sb.WriteString("1. Provide a helpful and engaging response.\n")
	sb.WriteString("2. DO NOT digress into unrelated personal facts from the retrieved context unless they are directly relevant to the user's message.\n")
	sb.WriteString("3. DO NOT volunteer updates about your knowledge gaps or mention what you 'don't know' or 'couldn't find' about the user's personal details unless specifically asked.\n")
	sb.WriteString("4. If the user asks about something specific (like their cats), answer that and ONLY that.\n\n")
	sb.WriteString("Please provide your response now.")

	// Add memory context
	prompt := nlg.addMemoryContext(sb.String(), req)

	if len(prompt) > 400000 {
		prompt = prompt[:400000] + "... [PROMPT TRUNCATED]"
	}
	return prompt
}

// buildGenericPrompt builds a generic prompt
func (nlg *NLGGenerator) buildGenericPrompt(req *NLGRequest) string {
	var sb strings.Builder
	sb.WriteString("You are a helpful AI assistant. Respond to the user's message appropriately.\n\n")
	sb.WriteString("User Message: \"")
	sb.WriteString(req.UserMessage)
	sb.WriteString("\"\nIntent: ")
	sb.WriteString(req.Intent.Type)
	sb.WriteString("\nGoal: ")
	sb.WriteString(req.Action.Goal)
	sb.WriteString("\n\n🚨 CRITICAL RULES:\n")
	sb.WriteString("1. Provide a helpful response focused on the user's goal.\n")
	sb.WriteString("2. DO NOT include unrelated personal details or talk about what is missing from your knowledge base.\n")
	sb.WriteString("3. Keep the response clean and relevant to the User Message.\n\n")
	sb.WriteString("Please provide a helpful response.")

	// Add memory context
	prompt := nlg.addMemoryContext(sb.String(), req)

	if len(prompt) > 400000 {
		prompt = prompt[:400000] + "... [PROMPT TRUNCATED]"
	}
	return prompt
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
	var sb strings.Builder
	// Total budget for a single tool result formatting to prevent OOM
	budget := 64 * 1024 // 64KB (conservative, but enough for helpful summaries)
	nlg.formatToolResultRecursive(result, 0, &sb, &budget)
	return sb.String()
}

func (nlg *NLGGenerator) formatToolResultRecursive(result interface{}, depth int, sb *strings.Builder, budget *int) {
	if result == nil || depth > 4 || *budget <= 0 {
		if *budget <= 0 && depth <= 4 {
			sb.WriteString("... [TRUNCATED DUE TO SIZE LIMIT]")
		}
		return
	}

	// Helper to write string with budget check
	writeSafe := func(s string) {
		if *budget <= 0 {
			return
		}
		if len(s) > *budget {
			sb.WriteString(s[:*budget])
			sb.WriteString("... [TRUNCATED]")
			*budget = 0
		} else {
			sb.WriteString(s)
			*budget -= len(s)
		}
	}

	switch v := result.(type) {
	case string:
		v = strings.TrimSpace(v)
		if strings.Contains(v, "\n") {
			lines := strings.Split(v, "\n")
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if line != "" {
					line = listCleanupRegex.ReplaceAllString(line, "")
					writeSafe("\n• " + line)
				}
			}
		} else if numberedListRegex.MatchString(v) {
			parts := numberedListRegex.Split(v, -1)
			for _, p := range parts {
				p = strings.TrimSpace(p)
				if p != "" {
					writeSafe("\n• " + p)
				}
			}
		} else {
			writeSafe(v)
		}

	case []byte:
		writeSafe(string(v))

	case map[string]interface{}:
		// 1. Try to find "main" content keys - check nested "result" first
		if inner, ok := v["result"].(map[string]interface{}); ok {
			nlg.formatToolResultRecursive(inner, depth+1, sb, budget)
			return
		}

		contentKeys := []string{"extracted_content", "headlines", "results", "items", "content", "summary", "text", "message"}
		for _, k := range contentKeys {
			if val, exists := v[k]; exists && val != nil {
				nlg.formatToolResultRecursive(val, depth+1, sb, budget)
				return
			}
		}

		// 2. Special handling for email-like records
		from := getStringFromMapCaseInsensitive(v, "from")
		subject := getStringFromMapCaseInsensitive(v, "subject")
		if from != "" || subject != "" {
			if from != "" {
				writeSafe("From: " + from)
			}
			if subject != "" {
				if from != "" {
					writeSafe(" | ")
				}
				writeSafe("Subject: " + subject)
			}
			return
		}

		// 3. Fallback: format as key-value pairs
		writeSafe("{")
		keysWritten := 0
		for mk, mv := range v {
			if keysWritten >= 10 {
				writeSafe(", ... [OTHER KEYS TRUNCATED]")
				break
			}
			// Skip technical/large fields
			if mk == "raw_html" || mk == "cleaned_html" || mk == "screenshot" || mk == "cookies" || mk == "extraction_method" {
				continue
			}

			if keysWritten > 0 {
				writeSafe(", ")
			}
			writeSafe(mk + ": ")

			// For nested values, use a local budget check to avoid one key eating everything
			nlg.formatToolResultRecursive(mv, depth+1, sb, budget)

			keysWritten++
		}
		writeSafe("}")

	case []interface{}:
		writeSafe("[")
		for i, item := range v {
			if i >= 30 { // Limit list items
				writeSafe(fmt.Sprintf("\n... [AND %d MORE ITEMS TRUNCATED]", len(v)-30))
				break
			}
			if i > 0 {
				writeSafe("\n")
			}
			writeSafe(fmt.Sprintf("[%d] ", i+1))
			nlg.formatToolResultRecursive(item, depth+1, sb, budget)
		}
		writeSafe("]")

	case int, int32, int64, float32, float64, bool:
		writeSafe(fmt.Sprintf("%v", v))

	case *InterpretResult:
		if v == nil {
			return
		}
		if v.Interpreted != nil {
			nlg.formatToolResultRecursive(v.Interpreted, depth+1, sb, budget)
		}
		if v.Metadata != nil {
			if tr, ok := v.Metadata["tool_result"]; ok {
				if v.Interpreted != nil {
					writeSafe("\n\nResult:\n")
				}
				nlg.formatToolResultRecursive(tr, depth+1, sb, budget)
			}
		}

	case InterpretResult:
		if v.Interpreted != nil {
			nlg.formatToolResultRecursive(v.Interpreted, depth+1, sb, budget)
		}
		if v.Metadata != nil {
			if tr, ok := v.Metadata["tool_result"]; ok {
				if v.Interpreted != nil {
					writeSafe("\n\nResult:\n")
				}
				nlg.formatToolResultRecursive(tr, depth+1, sb, budget)
			}
		}

	case *TaskResult:
		if v == nil {
			return
		}
		if v.Result != nil {
			nlg.formatToolResultRecursive(v.Result, depth+1, sb, budget)
		}
		if v.Metadata != nil {
			if tr, ok := v.Metadata["tool_result"]; ok {
				writeSafe("\n\n")
				nlg.formatToolResultRecursive(tr, depth+1, sb, budget)
			}
		}

	case *PlanResult:
		if v == nil {
			return
		}
		if v.Plan != nil {
			nlg.formatToolResultRecursive(v.Plan, depth+1, sb, budget)
		}
		if v.Metadata != nil {
			if tr, ok := v.Metadata["tool_result"]; ok {
				writeSafe("\n\n")
				nlg.formatToolResultRecursive(tr, depth+1, sb, budget)
			}
		}

	case *LearnResult:
		if v == nil {
			return
		}
		if v.Learned != nil {
			nlg.formatToolResultRecursive(v.Learned, depth+1, sb, budget)
		}
		if v.Metadata != nil {
			if tr, ok := v.Metadata["tool_result"]; ok {
				writeSafe("\n\n")
				nlg.formatToolResultRecursive(tr, depth+1, sb, budget)
			}
		}

	default:
		// Try to handle typed slices that are common
		if items, ok := v.([]map[string]interface{}); ok {
			genericItems := make([]interface{}, len(items))
			for i, item := range items {
				genericItems[i] = item
			}
			nlg.formatToolResultRecursive(genericItems, depth, sb, budget)
		} else {
			writeSafe(fmt.Sprintf("Object of type %T", result))
		}
	}
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
			res := ""
			if ir.Interpreted != nil {
				res = nlg.formatToolResult(ir.Interpreted)
			}
			if ir.Metadata != nil {
				if tr, ok := ir.Metadata["tool_result"]; ok {
					trFormatted := nlg.formatToolResult(tr)
					if res != "" {
						res += "\n\nResult:\n" + trFormatted
					} else {
						res = trFormatted
					}
				}
			}
			if res != "" {
				return res
			}
		} else if ir, ok := val.(InterpretResult); ok {
			res := ""
			if ir.Interpreted != nil {
				res = nlg.formatToolResult(ir.Interpreted)
			}
			if ir.Metadata != nil {
				if tr, ok := ir.Metadata["tool_result"]; ok {
					trFormatted := nlg.formatToolResult(tr)
					if res != "" {
						res += "\n\nResult:\n" + trFormatted
					} else {
						res = trFormatted
					}
				}
			}
			if res != "" {
				return res
			}
		} else {
			interpreted = val
		}

		formatted := nlg.formatToolResult(interpreted)
		if formatted != "" && formatted != "No data available" {
			return formatted
		}
	}

	// Generic fallback for all keys in data
	var sb strings.Builder
	for k, v := range data {
		formatted := nlg.formatToolResult(v)
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
		return utils.SafeResultSummary(val, 2000)
	}

	// Special case for Weaviate: properties might be in a nested "metadata" JSON string
	if metadataStr, ok := m["metadata"].(string); ok && metadataStr != "" {
		var metadata map[string]interface{}
		if err := json.Unmarshal([]byte(metadataStr), &metadata); err == nil {
			// Try looking in original_metadata if present
			if orig, ok := metadata["original_metadata"].(map[string]interface{}); ok {
				if val, exists := orig[key]; exists && val != nil {
					return utils.SafeResultSummary(val, 2000)
				}
			}
			if val, exists := metadata[key]; exists && val != nil {
				return utils.SafeResultSummary(val, 2000)
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
	return utils.SafeResultSummary(fromField, 2000)
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
		return utils.SafeResultSummary(val, 2000)
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
			return utils.SafeResultSummary(v, 2000)
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
						return utils.SafeResultSummary(v, 2000)
					}
				}
			}
			for k, v := range metadata {
				if strings.ToLower(k) == keyLower && v != nil {
					return utils.SafeResultSummary(v, 2000)
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

	// PERFORMANCE & ANTI-HALLUCINATION: Skip personal context injection for simple greetings.
	// This prevents the AI from summarizing the user's entire life story just because they said "hello".
	isGreeting := false
	if g, ok := req.Context["is_greeting"].(bool); ok && g {
		isGreeting = true
	}

	// Force context skip for common greetings or if explicitly flagged
	lowerMsg := strings.ToLower(strings.TrimSpace(req.UserMessage))
	if isGreeting || lowerMsg == "hello" || lowerMsg == "hi" || lowerMsg == "hey" || lowerMsg == "yo" {
		log.Printf("ℹ️ [NLG] Skipping memory context for greeting '%s'", req.UserMessage)
		return basePrompt
	}

	// ANTI-TANGENT: Skip personal facts (avatar_context) for tasks like scraping/research
	// unless the user message explicitly mentions "me", "my", or "myself".
	skipBio := false
	if req.Intent != nil && req.Intent.Type == "task" {
		if !strings.Contains(lowerMsg, " me ") && !strings.Contains(lowerMsg, " my ") &&
			!strings.Contains(lowerMsg, " myself") && !strings.HasPrefix(lowerMsg, "my ") {
			skipBio = true
			log.Printf("ℹ️ [NLG] Skipping bio for task intent without self-references")
		}
	}

	var sb strings.Builder
	sb.WriteString(basePrompt)

	// Start Consolidated Personal Context Section
	sb.WriteString("\n\nInformation from Memory/Bio:\n")
	hasPersonalContext := false

	// 1. Add conversation summaries for long-term memory
	// ANTI-STALE: Skip conversation summaries for rapid-changing data like weather
	isWeatherQuery := strings.Contains(lowerMsg, "weather") || strings.Contains(lowerMsg, "forecast") ||
		strings.Contains(lowerMsg, "temp") || strings.Contains(lowerMsg, "temperature") ||
		strings.Contains(lowerMsg, "rain") || strings.Contains(lowerMsg, "snow") ||
		strings.Contains(lowerMsg, "sunny")

	if summariesValue, ok := req.Context["conversation_summaries"]; ok && !isWeatherQuery {
		var summaries []string
		if s, ok := summariesValue.([]string); ok {
			summaries = s
		} else if i, ok := summariesValue.([]interface{}); ok {
			for _, item := range i {
				if str, ok := item.(string); ok {
					summaries = append(summaries, str)
				}
			}
		}

		if len(summaries) > 0 {
			hasPersonalContext = true
			sb.WriteString("### Past Conversation Summaries:\n")
			for _, summary := range summaries {
				if len(summary) > 2000 {
					summary = summary[:2000] + "... [TRUNCATED]"
				}
				cleanSummary := strings.ReplaceAll(summary, "Steven Fisher", "You")
				cleanSummary = strings.ReplaceAll(cleanSummary, "Steven", "You")
				sb.WriteString(fmt.Sprintf("- %s\n", cleanSummary))
			}
		}
	}

	// 1b. Add last vision capture or generated image (if available)
	if visionDesc, ok := req.Context["last_vision_description"].(string); ok && visionDesc != "" {
		hasPersonalContext = true
		sb.WriteString("\n### Last Seen/Generated Image:\n")
		sb.WriteString("This is the description of the last image seen by the camera or generated by you:\n")
		sb.WriteString(fmt.Sprintf("- %s\n", visionDesc))

		visionPath := "/tmp/vision_capture.jpg"
		if vp, ok := req.Context["last_vision_path"].(string); ok && vp != "" {
			visionPath = vp
		}

		sb.WriteString(fmt.Sprintf("\n🚨 USER INSTRUCTION: You can refer to this as 'that image' or 'the last capture'. If the user asks to update, change, or modify it, you MUST call 'tool_generate_image' with 'source_image' set to '%s'. Explain that you are basing the new creation on this previous view.\n", visionPath))
	}

	// 2. Add avatar context (personal info/bio)
	rawAvatar := req.Context["avatar_context"]
	if !skipBio && rawAvatar != nil {
		var avatarData *InterpretResult
		if v, ok := rawAvatar.(*InterpretResult); ok {
			avatarData = v
		} else if v, ok := rawAvatar.(InterpretResult); ok {
			avatarData = &v
		}

		if avatarData != nil {
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
					hasPersonalContext = true
					sb.WriteString("\n### Verified Facts About User (ONLY use if relevant to the request):\n")
					for _, res := range items {
						if item, ok := res.(map[string]interface{}); ok {
							rawContent := ""
							if content, ok := item["content"].(string); ok {
								rawContent = content
							} else if text, ok := item["text"].(string); ok {
								rawContent = text
							} else {
								rawContent = utils.SafeResultSummary(item, 1000)
							}

							// PERSONA-FLIP: Replace "Steven Fisher" with "You" to force LLM into first-person address
							cleanContent := strings.ReplaceAll(rawContent, "Steven Fisher", "You")
							cleanContent = strings.ReplaceAll(cleanContent, "Steven", "You")
							sb.WriteString(fmt.Sprintf("- %s\n", cleanContent))
						} else if str, ok := res.(string); ok {
							cleanStr := strings.ReplaceAll(str, "Steven Fisher", "You")
							cleanStr = strings.ReplaceAll(cleanStr, "Steven", "You")
							sb.WriteString("- " + cleanStr + "\n")
						}
					}
				}
			}
		}
	}

	// 3. Add immediate session history
	if history, ok := req.Context["conversation_history"].([]ConversationEntry); ok && len(history) > 0 {
		hasPersonalContext = true
		sb.WriteString("\n### Recent Session History (Last 10 turns):\n")
		start := 0
		if len(history) > 10 {
			start = len(history) - 10
		}
		for i := start; i < len(history); i++ {
			entry := history[i]
			sb.WriteString(fmt.Sprintf("User: %s\nAI: %s\n", entry.UserMessage, entry.AIResponse))
		}
	}

	// Sections moved inside hasPersonalContext check to avoid "missing" headers
	if hasPersonalContext {
		sb.WriteString("\nUse the above personal context to ensure continuity and recall personal facts.\n")
	}

	// 4. Add wiki/news context (General Knowledge, not Personal)
	rawWiki := req.Context["wiki_context"]
	var wikiData *InterpretResult
	if v, ok := rawWiki.(*InterpretResult); ok {
		wikiData = v
	} else if v, ok := rawWiki.(InterpretResult); ok {
		wikiData = &v
	}

	if wikiData != nil {
		if toolResult, ok := wikiData.Metadata["tool_result"].(map[string]interface{}); ok {
			var items []interface{}
			if i, ok := toolResult["results"].([]interface{}); ok {
				items = i
			}

			if len(items) > 0 {
				sb.WriteString("\n\nRetrieved News/Knowledge (AgiWiki):\n")
				for i, res := range items {
					if i >= 5 {
						break
					} // Max 5 wiki articles in context
					if item, ok := res.(map[string]interface{}); ok {
						if content, ok := item["content"].(string); ok {
							if len(content) > 2500 {
								content = content[:2500] + "... [TRUNCATED]"
							}
							sb.WriteString(fmt.Sprintf("--- ARTICLE %d ---\n%s\n", i+1, content))
						} else if text, ok := item["text"].(string); ok {
							if len(text) > 2500 {
								text = text[:2500] + "... [TRUNCATED]"
							}
							sb.WriteString(fmt.Sprintf("--- ARTICLE %d ---\n%s\n", i+1, text))
						}
					}
				}
				sb.WriteString("\nUse this information for factual context.")
			}
		}
	}

	// 5. Add news context if available separately
	if newsData, ok := req.Context["news_context"].(*InterpretResult); ok && newsData != nil {
		if toolResult, ok := newsData.Metadata["tool_result"].(map[string]interface{}); ok {
			if items, ok := toolResult["results"].([]interface{}); ok && len(items) > 0 {
				sb.WriteString("\n\nRecent News & Wikipedia Context:\n")
				for i, res := range items {
					if i >= 8 {
						break
					} // Max 8 news snippets
					if item, ok := res.(map[string]interface{}); ok {
						if snippet, ok := item["snippet"].(string); ok {
							if len(snippet) > 800 {
								snippet = snippet[:800] + "... [TRUNCATED]"
							}
							sb.WriteString("- " + snippet + "\n")
						} else if content, ok := item["content"].(string); ok {
							if len(content) > 800 {
								content = content[:800] + "... [TRUNCATED]"
							}
							sb.WriteString("- " + content + "\n")
						}
					}
				}
			}
		}
	}

	return sb.String()
}

// Summarizing functions removed (consolidated in hdn/utils)
