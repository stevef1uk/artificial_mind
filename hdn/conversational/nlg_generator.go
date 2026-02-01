package conversational

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
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

	return &NLGResponse{
		Text:       strings.TrimSpace(response),
		Confidence: 0.8,
		Metadata: map[string]interface{}{
			"response_type": "knowledge",
			"intent_type":   req.Intent.Type,
		},
	}, nil
}

// generateTaskResponse generates a response for task execution
func (nlg *NLGGenerator) generateTaskResponse(ctx context.Context, req *NLGRequest) (*NLGResponse, error) {
	prompt := nlg.buildTaskPrompt(req)

	response, err := nlg.llmClient.GenerateResponse(ctx, prompt, 400)
	if err != nil {
		return nlg.generateFallbackResponse(req, "task execution"), nil
	}

	return &NLGResponse{
		Text:       strings.TrimSpace(response),
		Confidence: 0.7,
		Metadata: map[string]interface{}{
			"response_type": "task",
			"intent_type":   req.Intent.Type,
		},
	}, nil
}

// generatePlanningResponse generates a response for planning requests
func (nlg *NLGGenerator) generatePlanningResponse(ctx context.Context, req *NLGRequest) (*NLGResponse, error) {
	prompt := nlg.buildPlanningPrompt(req)

	response, err := nlg.llmClient.GenerateResponse(ctx, prompt, 600)
	if err != nil {
		return nlg.generateFallbackResponse(req, "planning"), nil
	}

	return &NLGResponse{
		Text:       strings.TrimSpace(response),
		Confidence: 0.8,
		Metadata: map[string]interface{}{
			"response_type": "planning",
			"intent_type":   req.Intent.Type,
		},
	}, nil
}

// generateLearningResponse generates a response for learning requests
func (nlg *NLGGenerator) generateLearningResponse(ctx context.Context, req *NLGRequest) (*NLGResponse, error) {
	prompt := nlg.buildLearningPrompt(req)

	response, err := nlg.llmClient.GenerateResponse(ctx, prompt, 500)
	if err != nil {
		return nlg.generateFallbackResponse(req, "learning"), nil
	}

	return &NLGResponse{
		Text:       strings.TrimSpace(response),
		Confidence: 0.8,
		Metadata: map[string]interface{}{
			"response_type": "learning",
			"intent_type":   req.Intent.Type,
		},
	}, nil
}

// generateExplanationResponse generates a response for explanation requests
func (nlg *NLGGenerator) generateExplanationResponse(ctx context.Context, req *NLGRequest) (*NLGResponse, error) {
	prompt := nlg.buildExplanationPrompt(req)

	response, err := nlg.llmClient.GenerateResponse(ctx, prompt, 600)
	if err != nil {
		return nlg.generateFallbackResponse(req, "explanation"), nil
	}

	return &NLGResponse{
		Text:       strings.TrimSpace(response),
		Confidence: 0.8,
		Metadata: map[string]interface{}{
			"response_type": "explanation",
			"intent_type":   req.Intent.Type,
		},
	}, nil
}

// generateConversationResponse generates a response for general conversation
func (nlg *NLGGenerator) generateConversationResponse(ctx context.Context, req *NLGRequest) (*NLGResponse, error) {
	prompt := nlg.buildConversationPrompt(req)

	response, err := nlg.llmClient.GenerateResponse(ctx, prompt, 300)
	if err != nil {
		return nlg.generateFallbackResponse(req, "conversation"), nil
	}

	return &NLGResponse{
		Text:       strings.TrimSpace(response),
		Confidence: 0.6,
		Metadata: map[string]interface{}{
			"response_type": "conversation",
			"intent_type":   req.Intent.Type,
		},
	}, nil
}

// generateGenericResponse generates a generic response
func (nlg *NLGGenerator) generateGenericResponse(ctx context.Context, req *NLGRequest) (*NLGResponse, error) {
	prompt := nlg.buildGenericPrompt(req)

	response, err := nlg.llmClient.GenerateResponse(ctx, prompt, 300)
	if err != nil {
		return nlg.generateFallbackResponse(req, "generic"), nil
	}

	return &NLGResponse{
		Text:       strings.TrimSpace(response),
		Confidence: 0.5,
		Metadata: map[string]interface{}{
			"response_type": "generic",
			"intent_type":   req.Intent.Type,
		},
	}, nil
}

// buildKnowledgePrompt builds a prompt for knowledge responses
func (nlg *NLGGenerator) buildKnowledgePrompt(req *NLGRequest) string {
	basePrompt := `You are an AI assistant with access to a knowledge base and reasoning capabilities. 
Based on the user's question and the information retrieved, provide a helpful and accurate answer.

User Question: "%s"
Intent: %s
Goal: %s

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
		basePrompt += fmt.Sprintf(`

Retrieved Information:
%s

Use this information to answer the user's question comprehensively.`, nlg.formatResultData(req.Result.Data))
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
		return "No data available"
	}

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
			if toolResult, ok := metadata["tool_result"].(map[string]interface{}); ok {
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

				if len(resultsList) > 0 {
					// Check if this is email data (has Subject, From, To fields)
					firstItem, isEmailData := resultsList[0].(map[string]interface{})
					if isEmailData {
						_, hasSubject := firstItem["Subject"]
						_, hasFrom := firstItem["From"]
						if hasSubject || hasFrom {
							// Format as email list
							resultSb.WriteString(fmt.Sprintf("Found %d email(s):\n\n", len(resultsList)))
							for i, res := range resultsList {
								if item, ok := res.(map[string]interface{}); ok {
									subject := getStringFromMap(item, "Subject")
									from := getStringFromMap(item, "From")
									to := getStringFromMap(item, "To")
									snippet := getStringFromMap(item, "snippet")
									
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
									if subject != "" {
										resultSb.WriteString(fmt.Sprintf("    Subject: %s\n", subject))
									}
									if from != "" {
										resultSb.WriteString(fmt.Sprintf("    From: %s\n", from))
									}
									if to != "" {
										resultSb.WriteString(fmt.Sprintf("    To: %s\n", to))
									}
									if snippet != "" {
										// Limit snippet length
										if len(snippet) > 200 {
											snippet = snippet[:200] + "..."
										}
										resultSb.WriteString(fmt.Sprintf("    Preview: %s\n", snippet))
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
		return extractContent(result)
	}

	// Fallback to formatting the entire data structure
	fallback := fmt.Sprintf("%v", data)
	log.Printf("🗣️ [NLG] Result data formatted (length: %d)", len(fallback))
	return fallback
}

// getStringFromMap safely extracts a string value from a map
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
