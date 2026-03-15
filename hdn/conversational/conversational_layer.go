package conversational

import (
	"context"
	"fmt"
	"log"
	"os"
	"runtime/debug"
	"strings"
	"time"

	"hdn/interpreter"
	"hdn/utils"

	"github.com/redis/go-redis/v9"
)

// ConversationalLayer wraps the FSM + HDN system to provide LLM-like interaction
type ConversationalLayer struct {
	fsmEngine          FSMInterface
	hdnClient          HDNClientInterface
	redis              *redis.Client
	llmClient          LLMClientInterface
	intentParser       *IntentParser
	reasoningTrace     *ReasoningTrace
	nlgGenerator       *NLGGenerator
	conversationMemory *ConversationMemory
	thoughtExpression  *ThoughtExpressionService
	summarizer         *ConversationSummarizer
}

// stripInterpretResultForContext reduces large tool results embedded in InterpretResult.Metadata.
// This is critical for keeping prompt/context sizes bounded (e.g., Telegram sessions that run long).
func (cl *ConversationalLayer) stripInterpretResultForContext(ir *InterpretResult) *InterpretResult {
	if ir == nil {
		return nil
	}

	stripped := &InterpretResult{
		Success:     ir.Success,
		Interpreted: ir.Interpreted,
		Error:       ir.Error,
		Metadata:    make(map[string]interface{}),
	}

	if ir.Metadata == nil {
		return stripped
	}

	for mk, mv := range ir.Metadata {
		switch mk {
		case "tool_used", "response_type", "interpreted_at", "tool_success":
			stripped.Metadata[mk] = mv
		case "tool_result":
			// Partially strip tool results - keep some items so RAG still works, but bound their size.
			if tr, ok := mv.(map[string]interface{}); ok {
				out := map[string]interface{}{
					"success": tr["success"],
				}

				// Handle both []interface{} and []map[string]interface{}
				var rawResults []interface{}
				if res, ok := tr["results"].([]interface{}); ok {
					rawResults = res
				} else if res, ok := tr["results"].([]map[string]interface{}); ok {
					for _, item := range res {
						rawResults = append(rawResults, item)
					}
				}

				if len(rawResults) > 0 {
					out["count"] = len(rawResults)
					// Keep up to 5 items to allow current request to function
					limit := len(rawResults)
					if limit > 5 {
						limit = 5
					}
					summaryResults := make([]interface{}, 0, limit)
					for i := 0; i < limit; i++ {
						item := rawResults[i]
						if m, ok := item.(map[string]interface{}); ok {
							// Keep map structure but truncate large strings inside it
							strippedItem := make(map[string]interface{})
							for k, v := range m {
								if s, ok := v.(string); ok {
									strippedItem[k] = utils.TruncateString(s, 2000)
								} else {
									// For non-string fields, just keep them if they are simple
									strippedItem[k] = v
								}
							}
							summaryResults = append(summaryResults, strippedItem)
						} else {
							// For non-map items, use safe summary
							summaryResults = append(summaryResults, utils.SafeResultSummary(item, 2000))
						}
					}
					out["results"] = summaryResults
				} else if count, ok := tr["count"].(int); ok {
					out["count"] = count
				} else if count, ok := tr["count"].(float64); ok {
					out["count"] = int(count)
				}

				stripped.Metadata[mk] = out
			}
		}
	}

	return stripped
}

// FSMInterface defines the interface for FSM operations
type FSMInterface interface {
	GetCurrentState() string
	GetContext() map[string]interface{}
	TriggerEvent(eventName string, eventData map[string]interface{}) error
	IsHealthy() bool
}

// HDNClientInterface defines the interface for HDN operations
type HDNClientInterface interface {
	ExecuteTask(ctx context.Context, task string, context map[string]string) (*TaskResult, error)
	PlanTask(ctx context.Context, task string, context map[string]string) (*PlanResult, error)
	LearnFromLLM(ctx context.Context, input string, context map[string]string) (*LearnResult, error)
	InterpretNaturalLanguage(ctx context.Context, input string, context map[string]string) (*InterpretResult, error)
	SearchWeaviate(ctx context.Context, query string, collection string, limit int) (*InterpretResult, error)
	SaveEpisode(ctx context.Context, text string, metadata map[string]interface{}) error
}

// ConversationRequest represents a user's conversational input
type ConversationRequest struct {
	Message      string            `json:"message"`
	SessionID    string            `json:"session_id,omitempty"`
	Context      map[string]string `json:"context,omitempty"`
	StreamMode   bool              `json:"stream_mode,omitempty"`
	ShowThinking bool              `json:"show_thinking,omitempty"`
}

// ConversationResponse represents the AI's conversational response
type ConversationResponse struct {
	Response        string                 `json:"response"`
	SessionID       string                 `json:"session_id"`
	Timestamp       time.Time              `json:"timestamp"`
	ReasoningTrace  *ReasoningTraceData    `json:"reasoning_trace,omitempty"`
	Thoughts        []ExpressedThought     `json:"thoughts,omitempty"`
	ThinkingSummary string                 `json:"thinking_summary,omitempty"`
	Confidence      float64                `json:"confidence"`
	Metadata        map[string]interface{} `json:"metadata,omitempty"`
}

// Note: ReasoningTraceData, DecisionPoint, and ReasoningStep are defined in reasoning_trace.go

// NewConversationalLayer creates a new conversational layer
func NewConversationalLayer(
	fsmEngine FSMInterface,
	hdnClient HDNClientInterface,
	redis *redis.Client,
	llmClient LLMClientInterface,
) *ConversationalLayer {
	return &ConversationalLayer{
		fsmEngine:          fsmEngine,
		hdnClient:          hdnClient,
		redis:              redis,
		llmClient:          llmClient,
		intentParser:       NewIntentParser(llmClient),
		reasoningTrace:     NewReasoningTrace(redis),
		nlgGenerator:       NewNLGGenerator(llmClient),
		conversationMemory: NewConversationMemory(redis),
		thoughtExpression:  NewThoughtExpressionService(redis, llmClient),
		summarizer:         NewConversationSummarizer(llmClient, hdnClient, redis),
	}
}

// ProcessMessage handles a conversational message and returns a response
func (cl *ConversationalLayer) ProcessMessage(ctx context.Context, req *ConversationRequest) (*ConversationResponse, error) {
	log.Printf("💬 [CONVERSATIONAL] Processing message: %s", req.Message)

	// Start reasoning trace
	cl.reasoningTrace.StartTrace(req.SessionID)

	// Step 1: Load conversation context (moved before intent parsing for better context-aware analysis)
	conversationContext, err := cl.conversationMemory.GetContext(ctx, req.SessionID)
	if err != nil {
		log.Printf("⚠️ [CONVERSATIONAL] Failed to load conversation context: %v", err)
		conversationContext = make(map[string]interface{})
	}

	// Create a lean version of the context for the reasoning trace to prevent OOM
	leanContext := make(map[string]interface{})
	for k, v := range conversationContext {
		if k != "conversation_history" && k != "conversation_summaries" &&
			k != "wiki_context" && k != "avatar_context" && k != "news_context" &&
			k != "last_result" && k != "reasoning_trace" {
			leanContext[k] = utils.SafeResultSummary(v, 1000)
		}
	}
	leanContext["session_id"] = req.SessionID

	cl.reasoningTrace.AddStep(req.SessionID, "context_loading", "Loaded conversation context", leanContext)

	// Merge explicit request context (from UI/API) into conversation context
	if req.Context != nil {
		for k, v := range req.Context {
			// Prefer explicit request context values over stored ones
			conversationContext[k] = v
		}
	}

	cl.reasoningTrace.AddStep(req.SessionID, "context_loading", "Loaded conversation context", map[string]interface{}{
		"context_keys": len(conversationContext),
	})

	// Step 2: Parse intent and determine what the user wants
	lowerMsg := strings.ToLower(strings.TrimSpace(req.Message))
	isGreeting := lowerMsg == "hello" || lowerMsg == "hi" || lowerMsg == "hey" ||
		lowerMsg == "greetings" || lowerMsg == "good morning" ||
		lowerMsg == "good afternoon" || lowerMsg == "good evening" ||
		lowerMsg == "howdy" || lowerMsg == "yo" ||
		(len(lowerMsg) < 4 && !strings.Contains(lowerMsg, "?"))

	var intent *Intent
	if isGreeting {
		log.Printf("ℹ️ [CONVERSATIONAL] Greeting detected ('%s') - using pre-defined intent and skipping RAG", req.Message)
		intent = &Intent{Type: "general_conversation", Goal: "Respond to greeting", Confidence: 1.0, OriginalMessage: req.Message}
		conversationContext["is_greeting"] = true
	} else {
		// Parse intent only for non-greetings to save time and avoid LLM bias
		var err error
		intent, err = cl.intentParser.ParseIntent(ctx, req.Message, conversationContext)
		if err != nil {
			log.Printf("⚠️ [CONVERSATIONAL] Intent parsing failed, using general_conversation: %v", err)
			intent = &Intent{Type: "general_conversation", Goal: req.Message, Confidence: 0.5, OriginalMessage: req.Message}
		}
	}

	if isGreeting {
		// Done with RAG skip
	} else {
		cl.reasoningTrace.AddStep(req.SessionID, "intent_parsing", fmt.Sprintf("Parsed user intent: %s (confidence: %.2f)", intent.Type, intent.Confidence), map[string]interface{}{
			"intent_type": intent.Type,
			"confidence":  intent.Confidence,
			"entities":    intent.Entities,
			"goal":        intent.Goal,
		})

		// Step 2b: Load relevant conversation summaries and personal context (RAG)
		summaries, err := cl.summarizer.GetRelevantSummaries(ctx, req.SessionID, req.Message)
		if err != nil {
			log.Printf("⚠️ [CONVERSATIONAL] Failed to load conversation summaries: %v", err)
		} else if len(summaries) > 0 {
			conversationContext["conversation_summaries"] = summaries
			cl.reasoningTrace.AddStep(req.SessionID, "summary_retrieval", fmt.Sprintf("Retrieved %d relevant conversation summaries", len(summaries)), map[string]interface{}{
				"summary_count": len(summaries) + 1, // +1 for personal context
			})
		}

		// ALWAYS search personal context (AvatarContext) to ensure persona consistency,
		// UNLESS this is a clear tool/scrape request where background RAG might cause hallucinations.
		isScrapeIntent := intent.Type == "task" && (strings.Contains(strings.ToLower(req.Message), "scrape") ||
			strings.Contains(strings.ToLower(req.Message), "browse") ||
			strings.Contains(strings.ToLower(req.Message), "visit") ||
			strings.Contains(strings.ToLower(req.Message), "fetch"))

		lowerMsg := strings.ToLower(req.Message)
		isPersonal := strings.Contains(lowerMsg, " me") || strings.Contains(lowerMsg, " my") ||
			strings.Contains(lowerMsg, " am i") || strings.Contains(lowerMsg, " myself") ||
			strings.Contains(lowerMsg, " i am") || strings.Contains(lowerMsg, " i work")

		if isScrapeIntent {
			log.Printf("ℹ️ [CONVERSATIONAL] Skipping background RAG for scrape-intent request to ensure tool usage")
		} else {
			avatarResult, avatarErr := cl.hdnClient.SearchWeaviate(ctx, req.Message, "AvatarContext", 3)
			if avatarErr != nil {
				log.Printf("⚠️ [CONVERSATIONAL] Avatar context search failed: %v", avatarErr)
			} else if avatarResult != nil && avatarResult.Metadata != nil {
				if toolSuccess, ok := avatarResult.Metadata["tool_success"].(bool); ok && toolSuccess {
					if toolResult, ok := avatarResult.Metadata["tool_result"].(map[string]interface{}); ok {
						// Safely extract results as []interface{}
						var items []interface{}
						if i, ok := toolResult["results"].([]interface{}); ok {
							items = i
						} else if i, ok := toolResult["results"].([]map[string]interface{}); ok {
							for _, item := range i {
								items = append(items, item)
							}
						}

						if len(items) > 0 {
							conversationContext["avatar_context"] = cl.stripInterpretResultForContext(avatarResult)
							log.Printf("✅ [CONVERSATIONAL] Found %d relevant personal facts", len(items))
							cl.reasoningTrace.AddKnowledgeUsed(req.SessionID, "avatar_context")
						}
					}
				}
			}
		}

		// Step 2c: If it's a query, also search the general knowledge base (AgiWiki/News)
		// UNLESS it's a personal query, in which case we only want facts from bio/memory.
		if intent.Type == "query" && !isPersonal {
			// Parallel search for general wiki and specialized news/Wikipedia
			// Collection: AgiWiki (vector-based)
			wikiResult, wikiErr := cl.hdnClient.SearchWeaviate(ctx, req.Message, "AgiWiki", 5)
			if wikiErr != nil {
				log.Printf("⚠️ [CONVERSATIONAL] Wiki context search failed: %v", wikiErr)
			} else if wikiResult != nil && wikiResult.Metadata != nil {
				if toolSuccess, ok := wikiResult.Metadata["tool_success"].(bool); ok && toolSuccess {
					if toolResult, ok := wikiResult.Metadata["tool_result"].(map[string]interface{}); ok {
						// Safely extract results as []interface{}
						var items []interface{}
						if i, ok := toolResult["results"].([]interface{}); ok {
							items = i
						} else if i, ok := toolResult["results"].([]map[string]interface{}); ok {
							for _, item := range i {
								items = append(items, item)
							}
						}

						if len(items) > 0 {
							conversationContext["wiki_context"] = cl.stripInterpretResultForContext(wikiResult)
							log.Printf("✅ [CONVERSATIONAL] Found %d relevant articles in AgiWiki", len(items))
							cl.reasoningTrace.AddKnowledgeUsed(req.SessionID, "agi_wiki")
						}
					}
				}
			}

			// Collection: WikipediaArticle (specialized news/keyword search)
			newsResult, newsErr := cl.hdnClient.SearchWeaviate(ctx, req.Message, "WikipediaArticle", 5)
			if newsErr != nil {
				log.Printf("⚠️ [CONVERSATIONAL] News context search failed: %v", newsErr)
			} else if newsResult != nil && newsResult.Metadata != nil {
				if toolSuccess, ok := newsResult.Metadata["tool_success"].(bool); ok && toolSuccess {
					if toolResult, ok := newsResult.Metadata["tool_result"].(map[string]interface{}); ok {
						// Safely extract results as []interface{}
						var items []interface{}
						if i, ok := toolResult["results"].([]interface{}); ok {
							items = i
						} else if i, ok := toolResult["results"].([]map[string]interface{}); ok {
							for _, item := range i {
								items = append(items, item)
							}
						}

						if len(items) > 0 {
							conversationContext["news_context"] = cl.stripInterpretResultForContext(newsResult)
							log.Printf("✅ [CONVERSATIONAL] Found %d relevant recent news/Wikipedia articles", len(items))
							cl.reasoningTrace.AddKnowledgeUsed(req.SessionID, "wikipedia_news")
						}
					}
				}
			}
		}
	}

	// Step 3: Determine the appropriate action based on intent
	action, err := cl.determineAction(ctx, intent, conversationContext)
	if err != nil {
		return cl.handleError("Failed to determine action", err, req.SessionID)
	}

	cl.reasoningTrace.AddStep(req.SessionID, "action_determination", fmt.Sprintf("Determined action: %s to achieve: %s", action.Type, action.Goal), map[string]interface{}{
		"action_type": action.Type,
		"action_goal": action.Goal,
	})

	// Step 4: Execute the action using FSM + HDN
	// Add original message to context for knowledge query extraction
	conversationContext["original_message"] = req.Message
	result, err := cl.executeAction(ctx, action, conversationContext)
	if err != nil {
		return cl.handleError("Failed to execute action", err, req.SessionID)
	}

	resultDesc := "Executed action"
	if result.Success {
		resultDesc = fmt.Sprintf("Successfully executed action, got %s result", result.Type)
	} else {
		resultDesc = fmt.Sprintf("Action execution failed: %s", result.Error)
	}
	cl.reasoningTrace.AddStep(req.SessionID, "action_execution", resultDesc, map[string]interface{}{
		"success":     result.Success,
		"output_type": result.Type,
		"error":       result.Error,
	})

	// EXTRA DEBUG: Log result summary (safe)
	if result != nil && result.Data != nil {
		if resVal, ok := result.Data["result"]; ok {
			summary := utils.SafeResultSummary(resVal, 500)
			log.Printf("📊 [CONVERSATIONAL] [%s] Action result summary: %s", req.SessionID, summary)
		}
	}

	// Step 5: Generate natural language response
	response, err := cl.nlgGenerator.GenerateResponse(ctx, &NLGRequest{
		UserMessage:    req.Message,
		Intent:         intent,
		Action:         action,
		Result:         result,
		Context:        conversationContext,
		ShowThinking:   req.ShowThinking,
		ReasoningTrace: cl.reasoningTrace.GetTrace(req.SessionID),
	})
	if result != nil && result.Data != nil {
		log.Printf("📊 [CONVERSATIONAL] [%s] GenerateResponse input context keys: %d", req.SessionID, len(conversationContext))
	}
	if err != nil {
		return cl.handleError("Failed to generate response", err, req.SessionID)
	}

	responsePreview := response.Text
	if len(responsePreview) > 100 {
		responsePreview = responsePreview[:100] + "..."
	}
	cl.reasoningTrace.AddStep(req.SessionID, "response_generation", fmt.Sprintf("Generated response (confidence: %.2f): %s", response.Confidence, responsePreview), map[string]interface{}{
		"response_length": len(response.Text),
		"confidence":      response.Confidence,
	})

	// Step 6: Save conversation context

	// Detect if an image was generated to persist its context for "change that" follow-ups
	if result != nil && result.Success {
		var toolOutput map[string]interface{}
		if tr, ok := result.Data["result"].(map[string]interface{}); ok {
			toolOutput = tr
		} else if ir, ok := result.Data["result"].(*InterpretResult); ok && ir != nil {
			if ts, ok := ir.Metadata["tool_success"].(bool); ok && ts {
				if tr, ok := ir.Metadata["tool_result"].(map[string]interface{}); ok {
					toolOutput = tr
				}
			}
		}

		if toolOutput != nil {
			if imgPath, ok := toolOutput["image"].(string); ok && imgPath != "" {
				desc := "A generated image depicting: " + req.Message
				log.Printf("🖼️ [CONVERSATIONAL] Persistence: Successfully generated image, updating context with: %s", desc)
				conversationContext["last_vision_description"] = desc
				conversationContext["last_vision_path"] = imgPath
			}
		}
	}

	// CRITICAL: Strip the result before saving to history to prevent massive context bloat.
	// We want to keep the metadata and success status, but not 10MB of raw research data.
	strippedResult := cl.stripActionResultForHistory(result)

	saveMap := map[string]interface{}{
		"last_user_message": req.Message,
		"last_ai_response":  response.Text,
		"last_intent":       intent,
		"last_action":       action,
		"last_result":       strippedResult,
		"timestamp":         time.Now(),
	}

	// Also include any updated context keys (like last_vision_description)
	if vd, ok := conversationContext["last_vision_description"].(string); ok {
		saveMap["last_vision_description"] = vd
	}
	if vp, ok := conversationContext["last_vision_path"].(string); ok {
		saveMap["last_vision_path"] = vp
	}

	err = cl.conversationMemory.SaveContext(ctx, req.SessionID, saveMap)
	if err != nil {
		log.Printf("⚠️ [CONVERSATIONAL] Failed to save conversation context: %v", err)
	}

	// Step 6b: Check if we should summarize the conversation
	shouldSummarize, _ := cl.summarizer.ShouldSummarize(ctx, req.SessionID)
	if shouldSummarize {
		log.Printf("📝 [CONVERSATIONAL] Summarization triggered for session %s", req.SessionID)
		// Get conversation history and summarize
		var turns []ConversationTurn
		if history, ok := conversationContext["conversation_history"].([]ConversationEntry); ok {
			for _, entry := range history {
				turns = append(turns, ConversationTurn{
					UserMessage:       entry.UserMessage,
					AssistantResponse: entry.AIResponse,
					Timestamp:         entry.Timestamp,
				})
			}
		} else if history, ok := conversationContext["conversation_history"].([]ConversationTurn); ok {
			turns = history
		}

		if len(turns) > 0 {
			go func() {
				defer func() {
					if rec := recover(); rec != nil {
						log.Printf("🔥 [CONVERSATIONAL] Panic in async summarization: %v\n%s", rec, string(debug.Stack()))
					}
				}()
				summarizeCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				err := cl.summarizer.SummarizeConversation(summarizeCtx, req.SessionID, turns)
				if err != nil {
					log.Printf("⚠️ [CONVERSATIONAL] Async summarization failed: %v", err)
				}
			}()
		} else {
			log.Printf("⚠️ [CONVERSATIONAL] Summarization triggered but no history found (type: %T)", conversationContext["conversation_history"])
		}
	}

	// Step 7: Complete reasoning trace
	reasoningTrace := cl.reasoningTrace.CompleteTrace(req.SessionID)

	// Step 8: ALWAYS generate and store thought expression for monitoring/dashboards
	thoughtReq := &ThoughtExpressionRequest{
		SessionID: req.SessionID,
		TraceData: reasoningTrace,
		Style:     "conversational",
		Context:   conversationContext,
	}

	// Create response
	conversationResponse := &ConversationResponse{
		Response:   response.Text,
		SessionID:  req.SessionID,
		Timestamp:  time.Now(),
		Confidence: response.Confidence,
		Metadata: map[string]interface{}{
			"intent_type":    intent.Type,
			"action_type":    action.Type,
			"fsm_state":      cl.fsmEngine.GetCurrentState(),
			"execution_time": time.Since(reasoningTrace.StartTime),
		},
	}

	thoughtExpression, err := cl.thoughtExpression.ExpressThoughts(ctx, thoughtReq)
	if err != nil {
		log.Printf("⚠️ [CONVERSATIONAL] Failed to generate thought expression: %v", err)
	} else if thoughtExpression != nil {
		// Store thoughts as ThoughtEvents in Redis for later retrieval (dashboards, etc)
		for _, thought := range thoughtExpression.Thoughts {
			thoughtEvent := ThoughtEvent{
				SessionID:  req.SessionID,
				Type:       thought.Type,
				State:      thought.State,
				Goal:       thought.Goal,
				Thought:    thought.Content,
				Confidence: thought.Confidence,
				ToolUsed:   thought.ToolUsed,
				Action:     thought.Action,
				Result:     thought.Result,
				Timestamp:  thought.Timestamp.Format(time.RFC3339Nano),
				Metadata:   thought.Metadata,
			}
			if err := cl.thoughtExpression.StoreThoughtEvent(ctx, thoughtEvent); err != nil {
				log.Printf("⚠️ [CONVERSATIONAL] Failed to store thought event: %v", err)
			}
		}
		log.Printf("💾 [CONVERSATIONAL] Stored %d thought events for session: %s", len(thoughtExpression.Thoughts), req.SessionID)

		// ONLY add thoughts to the API response if requested
		if req.ShowThinking {
			conversationResponse.Thoughts = thoughtExpression.Thoughts
			conversationResponse.ThinkingSummary = thoughtExpression.Summary
			conversationResponse.Metadata["thought_count"] = len(thoughtExpression.Thoughts)
			conversationResponse.Metadata["thinking_confidence"] = thoughtExpression.Confidence
			conversationResponse.ReasoningTrace = reasoningTrace
		}
	}

	return conversationResponse, nil
}

// determineAction determines what action to take based on intent and context
func (cl *ConversationalLayer) determineAction(ctx context.Context, intent *Intent, context map[string]interface{}) (*Action, error) {
	// Optional flag: conversation_only=true → avoid Neo4j/RAG knowledge queries
	conversationOnly := false
	if val, ok := context["conversation_only"]; ok {
		if s, ok := val.(string); ok && strings.ToLower(s) == "true" {
			conversationOnly = true
		}
	}

	// If conversation_only is enabled, always fall back to general conversation,
	// regardless of the parsed intent type. This ensures we don't trigger tools,
	// code execution, or Neo4j/RAG when the user wants pure conversational recall.
	if conversationOnly {
		return &Action{
			Type: "general_conversation",
			Goal: intent.Goal,
			Parameters: map[string]interface{}{
				"message": intent.OriginalMessage,
				"mode":    "conversation_only",
			},
		}, nil
	}

	switch intent.Type {
	case "query":
		return &Action{
			Type: "knowledge_query",
			Goal: intent.Goal,
			Parameters: map[string]interface{}{
				"query":  intent.Entities["query"],
				"domain": intent.Entities["domain"],
			},
		}, nil

	case "task":
		return &Action{
			Type: "task_execution",
			Goal: intent.Goal,
			Parameters: map[string]interface{}{
				"task": intent.Entities["task"],
			},
		}, nil

	case "plan":
		return &Action{
			Type: "planning",
			Goal: intent.Goal,
			Parameters: map[string]interface{}{
				"objective":   intent.Entities["objective"],
				"constraints": intent.Entities["constraints"],
			},
		}, nil

	case "learn":
		return &Action{
			Type: "learning",
			Goal: intent.Goal,
			Parameters: map[string]interface{}{
				"topic":  intent.Entities["topic"],
				"source": intent.Entities["source"],
			},
		}, nil

	case "personal_update":
		return &Action{
			Type: "personal_update",
			Goal: intent.Goal,
			Parameters: map[string]interface{}{
				"message": intent.OriginalMessage,
			},
		}, nil

	case "explain":
		return &Action{
			Type: "explanation",
			Goal: intent.Goal,
			Parameters: map[string]interface{}{
				"concept": intent.Entities["concept"],
				"level":   intent.Entities["level"],
			},
		}, nil

	default:
		isGreeting := false
		if g, ok := context["is_greeting"].(bool); ok && g {
			isGreeting = true
		}

		return &Action{
			Type: "general_conversation",
			Goal: "Respond to user in a helpful way",
			Parameters: map[string]interface{}{
				"message":     intent.OriginalMessage,
				"is_greeting": isGreeting,
			},
		}, nil
	}
}

// executeAction executes the determined action using FSM + HDN
func (cl *ConversationalLayer) executeAction(ctx context.Context, action *Action, context map[string]interface{}) (*ActionResult, error) {
	sessionID, _ := context["session_id"].(string)
	log.Printf("🎯 [CONVERSATIONAL] [%s] Executing action: %s - %s", sessionID, action.Type, action.Goal)

	// Convert context to string map for HDN
	hdnContext := make(map[string]string)
	forceKnowledgeQuery := false
	knowledgeSources := "neo4j"
	for k, v := range context {
		// Skip definitely-too-large keys that HDN interpreter doesn't need for tool selection.
		// Avoid passing structured objects like InterpretResult which serialize to massive strings.
		if k == "conversation_history" || k == "last_result" || k == "reasoning_trace" ||
			k == "avatar_context" || k == "wiki_context" || k == "news_context" {
			continue
		}

		// Skip any key that looks like it might be an internal memory structure to avoid hallucinations
		if strings.HasSuffix(k, "_context") {
			continue
		}

		// Capture force_knowledge_query / knowledge_sources hints (may be non-string)
		if k == "force_knowledge_query" {
			if b, ok := v.(bool); ok && b {
				forceKnowledgeQuery = true
			} else if s, ok := v.(string); ok && strings.ToLower(s) == "true" {
				forceKnowledgeQuery = true
			}
		}
		if k == "knowledge_sources" {
			if s, ok := v.(string); ok && s != "" {
				knowledgeSources = strings.ToLower(s)
			}
		}

		// Explicitly handle conversation_summaries (often []string) to preserve content
		if k == "conversation_summaries" {
			if summaries, ok := v.([]string); ok {
				hdnContext[k] = utils.TruncateString(strings.Join(summaries, "\n\n"), 5000)
				continue
			}
		}

		// Use SafeResultSummary to prevent OOM during string conversion
		hdnContext[k] = utils.SafeResultSummary(v, 5000)
	}

	// Add a concise summary of recent conversation history for tool selection context
	if history, ok := context["conversation_history"].([]ConversationEntry); ok && len(history) > 0 {
		var sb strings.Builder
		sb.WriteString("Recent context: ")
		start := 0
		if len(history) > 5 {
			start = len(history) - 5
		}
		for i := start; i < len(history); i++ {
			entry := history[i]
			aiResp := entry.AIResponse
			if len(aiResp) > 150 {
				aiResp = aiResp[:147] + "..."
			}
			sb.WriteString(fmt.Sprintf("User:%s -> AI:%s; ", entry.UserMessage, aiResp))
		}
		hdnContext["conversation_history_summary"] = sb.String()
	}

	// Add action parameters to context
	for k, v := range action.Parameters {
		hdnContext[k] = utils.SafeResultSummary(v, 5000)
	}

	if visionPath, ok := context["last_vision_path"].(string); ok && visionPath != "" {
		hdnContext["last_vision_path"] = visionPath
	}
	if visionDesc, ok := context["last_vision_description"].(string); ok && visionDesc != "" {
		hdnContext["last_vision_description"] = visionDesc
	}

	// Add original message if available in context for knowledge query extraction
	if origMsg, ok := context["original_message"].(string); ok {
		hdnContext["original_message"] = origMsg
	} else if origMsg, ok := context["message"].(string); ok {
		hdnContext["original_message"] = origMsg
	}

	switch action.Type {
	case "knowledge_query":
		// CRITICAL: Check if this is an email/calendar request BEFORE rewriting
		// If so, pass the original message directly to preserve email keywords
		originalMessage := ""
		if origMsg, ok := context["original_message"].(string); ok {
			originalMessage = origMsg
		} else if origMsg, ok := hdnContext["original_message"]; ok {
			originalMessage = origMsg
		}

		// Check if original message matches configured tool keywords
		if originalMessage != "" {
			// Use configurable tool keyword matching instead of hardcoded email checks
			if toolID := interpreter.MatchesConfiguredToolKeywords(originalMessage); toolID != "" {
				log.Printf("🔧 [CONVERSATIONAL] Detected configured tool request (%s) - preserving original message: %s", toolID, originalMessage)

				// SPECIAL CASE: If it's an image generation request, we want to skip the "core query" extraction
				// which sometimes strips useful words.
				if toolID == "tool_generate_image" {
					log.Printf("🖼️ [CONVERSATIONAL] Image generation detected - bypassing core query extraction")
					// Ensure prompt is set for tool_generate_image
 					if hdnContext != nil {
 						// Map query, description, or task to prompt
 						if q, ok := hdnContext["query"]; ok && strings.TrimSpace(fmt.Sprintf("%v", q)) != "" {
 							hdnContext["prompt"] = q
 						} else if d, ok := hdnContext["description"]; ok && strings.TrimSpace(fmt.Sprintf("%v", d)) != "" {
 							hdnContext["prompt"] = d
 						} else if t, ok := hdnContext["task"]; ok && strings.TrimSpace(fmt.Sprintf("%v", t)) != "" {
 							hdnContext["prompt"] = t
 						} else {
 							hdnContext["prompt"] = originalMessage
 						}
 					}
					interpretResult, err := cl.hdnClient.InterpretNaturalLanguage(ctx, originalMessage, hdnContext)
					if err != nil {
						return nil, fmt.Errorf("image tool interpretation failed: %w", err)
					}
					return &ActionResult{
						Type:    "conversation_result",
						Success: true,
						Data: map[string]interface{}{
							"result": interpretResult,
							"source": "hdn_natural_language",
						},
					}, nil
				}

				// Pass original message directly to preserve keywords for tool detection
				interpretResult, err := cl.hdnClient.InterpretNaturalLanguage(ctx, originalMessage, hdnContext)
				if err != nil {
					return nil, fmt.Errorf("tool request interpretation failed: %w", err)
				}
				return &ActionResult{
					Type:    "conversation_result",
					Success: true,
					Data: map[string]interface{}{
						"result": interpretResult,
						"source": "hdn_natural_language",
					},
				}, nil
			}
		}

		if forceKnowledgeQuery {
			log.Printf("🧠 [CONVERSATIONAL] Force knowledge query enabled (sources=%s) - running combined Neo4j + RAG flow", knowledgeSources)
		}
		// Use HDN's natural language interpretation for knowledge queries (allows tool usage)
		// For knowledge queries, use a simpler query format to encourage tool usage
		// Extract the core query from the goal or use the original message
		queryText := action.Goal
		// If goal is too verbose, try to extract a simpler query
		if strings.Contains(queryText, "**Goal:**") {
			// Extract text after "**Goal:**" and before any "**Rationale:**" or similar
			parts := strings.Split(queryText, "**Goal:**")
			if len(parts) > 1 {
				goalPart := strings.Split(parts[1], "**")[0]
				goalPart = strings.TrimSpace(goalPart)
				// If it's still too long, try to extract just the question part
				if len(goalPart) > 100 {
					// Look for question patterns
					if idx := strings.Index(goalPart, "What is"); idx >= 0 {
						queryText = goalPart[idx:]
						if endIdx := strings.Index(queryText, "."); endIdx > 0 && endIdx < 50 {
							queryText = queryText[:endIdx+1]
						}
					} else if idx := strings.Index(goalPart, "What are"); idx >= 0 {
						queryText = goalPart[idx:]
						if endIdx := strings.Index(queryText, "."); endIdx > 0 && endIdx < 50 {
							queryText = queryText[:endIdx+1]
						}
					} else {
						// Just use first sentence
						if endIdx := strings.Index(goalPart, "."); endIdx > 0 {
							queryText = goalPart[:endIdx+1]
						}
					}
				} else {
					queryText = goalPart
				}
			}
		}
		// For knowledge queries, create a more direct prompt that forces tool usage
		// Extract just the core concept name from the original message or goal
		// Try to get the original message from context first (if not already set above)
		if originalMessage == "" {
			if origMsg, ok := hdnContext["original_message"]; ok {
				originalMessage = origMsg
			} else if origMsg, ok := context["original_message"].(string); ok {
				originalMessage = origMsg
			}
		}

		// Extract concept name from "What is X?" pattern
		coreQuery := ""
		searchText := originalMessage
		if searchText == "" {
			searchText = queryText
			log.Printf("⚠️ [CONVERSATIONAL] Original message not found in context, using queryText: %s", queryText)
		} else {
			log.Printf("✅ [CONVERSATIONAL] Using original message for extraction: %s", originalMessage)
		}

		// Look for "What is X?", "What are X?", "Who is X?", "Who are X?" patterns
		lowerText := strings.ToLower(searchText)
		extractPattern := func(pattern string) string {
			if idx := strings.Index(lowerText, pattern); idx >= 0 {
				start := idx + len(pattern)
				end := len(searchText)
				// Find end of concept (period, question mark, or end of string)
				for i := start; i < len(searchText); i++ {
					if searchText[i] == '.' || searchText[i] == '?' || searchText[i] == '!' {
						end = i
						break
					}
				}
				extracted := strings.TrimSpace(searchText[start:end])
				if extracted != "" {
					return extracted
				}
			}
			return ""
		}

		// Helper function to filter skip words from extracted text
		filterSkipWords := func(text string) string {
			originalWords := strings.Fields(text)
			words := strings.Fields(strings.ToLower(text))
			skipWords := map[string]bool{
				"the": true, "a": true, "an": true, "and": true, "or": true, "but": true,
				"in": true, "on": true, "at": true, "to": true, "for": true, "of": true,
				"with": true, "about": true, "by": true, "from": true, "up": true, "out": true,
				"what": true, "who": true, "where": true, "when": true, "why": true, "how": true,
				"is": true, "are": true, "was": true, "were": true, "am": true, "be": true,
				"can": true, "could": true, "should": true, "would": true, "do": true, "does": true,
				"did": true, "will": true, "i": true, "you": true, "me": true, "my": true,
				"tell": true, "say": true, "show": true, "give": true, "please": true,
				"summarize": true, "summarise": true, "summary": true, "latest": true,
				"current": true, "recent": true, "update": true, "updated": true,
				"situation": true, "news": true, "info": true, "information": true,
				"create": true, "generate": true, "make": true,
			}

			var filtered []string
			for i, w := range words {
				// Trim punctuation from the word before checking against skipWords
				cleanWord := strings.Trim(w, ".,!?;:()[]{}'\"")
				if !skipWords[cleanWord] {
					filtered = append(filtered, originalWords[i])
				}
			}

			// If everything was filtered out, return the original text
			// to avoid sending empty queries to vector search
			if len(filtered) == 0 {
				return text
			}

			return strings.Join(filtered, " ")
		}

		// Try different patterns in order of specificity
		coreQuery = extractPattern("who is ")
		if coreQuery != "" {
			coreQuery = filterSkipWords(coreQuery)
			log.Printf("✅ [CONVERSATIONAL] Extracted concept name from 'Who is' pattern: '%s'", coreQuery)
		} else {
			coreQuery = extractPattern("who are ")
			if coreQuery != "" {
				coreQuery = filterSkipWords(coreQuery)
				log.Printf("✅ [CONVERSATIONAL] Extracted concept name from 'Who are' pattern: '%s'", coreQuery)
			} else {
				coreQuery = extractPattern("what is ")
				if coreQuery != "" {
					coreQuery = filterSkipWords(coreQuery)
					log.Printf("✅ [CONVERSATIONAL] Extracted concept name from 'What is' pattern: '%s'", coreQuery)
				} else {
					coreQuery = extractPattern("what are ")
					if coreQuery != "" {
						coreQuery = filterSkipWords(coreQuery)
						log.Printf("✅ [CONVERSATIONAL] Extracted concept name from 'What are' pattern: '%s'", coreQuery)
					}
				}
			}
		}

		// If we couldn't extract from patterns, try to get meaningful phrase from searchText (original message)
		if coreQuery == "" {
			// Remove common question words and extract the main subject
			words := strings.Fields(searchText)
			// Skip common question words at the start (including "tell", "me", "about")
			skipWords := map[string]bool{
				"who": true, "what": true, "where": true, "when": true, "why": true, "how": true,
				"is": true, "are": true, "the": true, "a": true, "an": true, "in": true, "of": true,
				"tell": true, "me": true, "about": true, "current": true, "latest": true, "situation": true,
				"update": true, "summary": true, "search": true, "for": true, "news": true, "find": true,
				"get": true, "show": true, "give": true, "summarize": true, "summarise": true,
				"recent": true, "info": true, "information": true, "create": true, "generate": true, "make": true,
			}
			startIdx := 0
			for i, word := range words {
				if !skipWords[strings.ToLower(word)] {
					startIdx = i
					break
				}
			}
			// Take up to 2 words for the core subject (more likely to match a concept)
			if startIdx < len(words) {
				count := 0
				var subjectWords []string
				for i := startIdx; i < len(words) && count < 2; i++ {
					// Clean word from punctuation
					word := strings.Trim(words[i], ".,?!'\"")
					if word != "" {
						subjectWords = append(subjectWords, word)
						count++
					}
				}
				coreQuery = strings.Join(subjectWords, " ")
				// Capitalize first letter
				if len(coreQuery) > 0 {
					coreQuery = strings.ToUpper(coreQuery[:1]) + coreQuery[1:]
				}
				log.Printf("✅ [CONVERSATIONAL] Extracted core subject: '%s' (from: '%s')", coreQuery, searchText)
			} else {
				coreQuery = "General"
				log.Printf("⚠️ [CONVERSATIONAL] Could not extract core subject, using 'General'")
			}
		}

		// Construct a prompt that encourages flexible tool usage
		directQuery := fmt.Sprintf("Answer the user's query about '%s'. For latest news, recent updates, or real-time information, you can use mcp_search_weaviate with collection='WikipediaArticle', OR use mcp_scrape_url/mcp_smart_scrape if you have a specific URL. If it is a definition or general concept, use mcp_get_concept. Original query for context: %s", coreQuery, originalMessage)
		log.Printf("🔍 [CONVERSATIONAL] Flexible knowledge query: %s (extracted from: %s)", directQuery, searchText)
		interpretResult, err := cl.hdnClient.InterpretNaturalLanguage(ctx, directQuery, hdnContext)
		if err != nil {
			return nil, fmt.Errorf("knowledge query failed: %w", err)
		}

		// Track tool usage if present in the result
		if interpretResult != nil && interpretResult.Metadata != nil {
			if toolID, ok := interpretResult.Metadata["tool_used"].(string); ok && toolID != "" {
				cl.reasoningTrace.AddToolInvoked(sessionID, toolID)
				log.Printf("🔧 [CONVERSATIONAL] Tracked tool invocation: %s", toolID)
			}
		}

		// Check if Neo4j returned any results
		hasNeo4jResults := false
		if interpretResult != nil {
			// First check metadata for tool result (if available)
			if interpretResult.Metadata != nil {
				if toolSuccess, ok := interpretResult.Metadata["tool_success"].(bool); ok && toolSuccess {
					// Check if tool_result is in metadata (if available)
					if toolResult, ok := interpretResult.Metadata["tool_result"].(map[string]interface{}); ok {
						if hasResultsInToolResult(toolResult) {
							hasNeo4jResults = true
						}
					}
				}
			}

			// If not found in metadata, check the interpreted text for result patterns
			// Tool results are appended to interpreted text as "Tool result: map[count:0 results:[]]"
			if !hasNeo4jResults {
				if interpreted, ok := interpretResult.Interpreted.(string); ok {
					lowerInterpreted := strings.ToLower(interpreted)
					// Check for patterns indicating NO results
					if strings.Contains(lowerInterpreted, "count:0") ||
						strings.Contains(lowerInterpreted, "results:[]") ||
						strings.Contains(lowerInterpreted, "count: 0") ||
						strings.Contains(lowerInterpreted, "no results") ||
						strings.Contains(lowerInterpreted, "returned 0 rows") {
						hasNeo4jResults = false
					} else if strings.Contains(lowerInterpreted, "count:") {
						// If count is mentioned and not 0, we likely have results
						// Extract number after "count:" to check
						if idx := strings.Index(lowerInterpreted, "count:"); idx >= 0 {
							remainder := lowerInterpreted[idx+6:]
							// Look for a number > 0
							if strings.Contains(remainder, "1") || strings.Contains(remainder, "2") ||
								strings.Contains(remainder, "3") || strings.Contains(remainder, "4") ||
								strings.Contains(remainder, "5") || strings.Contains(remainder, "6") ||
								strings.Contains(remainder, "7") || strings.Contains(remainder, "8") ||
								strings.Contains(remainder, "9") {
								hasNeo4jResults = true
							}
						}
					}
				}
			}
		}

		// Regardless of Neo4j results, also try RAG search on Weaviate for episodic memory and news.
		// This ensures we can combine structured knowledge (Neo4j) with episodic/news evidence.
		log.Printf("🔍 [CONVERSATIONAL] Attempting RAG search on Weaviate for: %s (hasNeo4jResults=%v)", coreQuery, hasNeo4jResults)

		// Use the extracted core query directly for better precision
		// This ensures we search for the specific term (e.g., "bondi") rather than the full question
		// Extract meaningful search term from text
		ragQueryText := filterSkipWords(searchText)

		// If query is about the user (e.g. "who am i", "about me"), ensure we search for the configured user name
		userName := os.Getenv("USER_NAME")
		if userName == "" {
			userName = "User" // fallback
		}
		if strings.Contains(strings.ToLower(searchText), "who am i") ||
			strings.Contains(strings.ToLower(searchText), "about me") ||
			strings.Contains(strings.ToLower(searchText), "who is "+strings.ToLower(userName)) {
			if ragQueryText == "" || len(ragQueryText) < 5 {
				ragQueryText = userName + " personal information biography work history"
			}
		}

		if ragQueryText == "" {
			log.Printf("⚠️ [CONVERSATIONAL] RAG search query is empty after filtering skip words from: '%s'", searchText)
			// Return Neo4j results (if any)
			return &ActionResult{
				Type:    "knowledge_result",
				Success: true,
				Data:    map[string]interface{}{"neo4j_result": interpretResult, "source": "neo4j_only"},
			}, nil
		}

		log.Printf("🔍 [CONVERSATIONAL] RAG search query: '%s' (extracted from: '%s')", ragQueryText, searchText)

		// 1. Try searching episodic memory (AgiEpisodes) DIRECTLY
		log.Printf("🔍 [CONVERSATIONAL] Calling SearchWeaviate for episodic memory: %s", ragQueryText)
		ragResult, ragErr := cl.hdnClient.SearchWeaviate(ctx, ragQueryText, "AgiEpisodes", 3)

		hasRAGResults := false
		if ragErr != nil {
			log.Printf("⚠️ [CONVERSATIONAL] Episodic RAG search failed: %v", ragErr)
		} else if ragResult != nil && ragResult.Metadata != nil {
			if toolSuccess, ok := ragResult.Metadata["tool_success"].(bool); ok && toolSuccess {
				if toolResult, ok := ragResult.Metadata["tool_result"].(map[string]interface{}); ok {
					if hasResultsInToolResult(toolResult) {
						hasRAGResults = true
						log.Printf("✅ [CONVERSATIONAL] RAG search found results in episodic memory")
					}
				}
			}
		}

		// 2. Try searching news (WikipediaArticle) INDEPENDENTLY and DIRECTLY
		log.Printf("🔍 [CONVERSATIONAL] Calling SearchWeaviate for news articles: %s", ragQueryText)
		newsResult, newsErr := cl.hdnClient.SearchWeaviate(ctx, ragQueryText, "WikipediaArticle", 3)

		hasNewsResults := false
		if newsErr != nil {
			log.Printf("⚠️ [CONVERSATIONAL] News RAG search failed: %v", newsErr)
		} else if newsResult != nil && newsResult.Metadata != nil {
			if toolSuccess, ok := newsResult.Metadata["tool_success"].(bool); ok && toolSuccess {
				if toolResult, ok := newsResult.Metadata["tool_result"].(map[string]interface{}); ok {
					if hasResultsInToolResult(toolResult) {
						hasNewsResults = true
						log.Printf("✅ [CONVERSATIONAL] RAG search found results in news articles (WikipediaArticle)")
					}
				}
			}
		}

		// 3. Try searching Wikipedia knowledge base (AgiWiki) INDEPENDENTLY and DIRECTLY
		log.Printf("🔍 [CONVERSATIONAL] Calling SearchWeaviate for Wikipedia articles: %s", ragQueryText)
		wikiResult, wikiErr := cl.hdnClient.SearchWeaviate(ctx, ragQueryText, "AgiWiki", 3)

		hasWikiResults := false
		if wikiErr != nil {
			log.Printf("⚠️ [CONVERSATIONAL] Wikipedia RAG search failed: %v", wikiErr)
		} else if wikiResult != nil && wikiResult.Metadata != nil {
			if toolSuccess, ok := wikiResult.Metadata["tool_success"].(bool); ok && toolSuccess {
				if toolResult, ok := wikiResult.Metadata["tool_result"].(map[string]interface{}); ok {
					if hasResultsInToolResult(toolResult) {
						hasWikiResults = true
						log.Printf("✅ [CONVERSATIONAL] RAG search found results in Wikipedia knowledge base (AgiWiki)")
					}
				}
			}
		}

		// 4. Try searching avatar context (personal info) independently and directly
		log.Printf("🔍 [CONVERSATIONAL] Calling SearchWeaviate for avatar context: %s", ragQueryText)
		avatarResult, avatarErr := cl.hdnClient.SearchWeaviate(ctx, ragQueryText, "AvatarContext", 3)

		hasAvatarResults := false
		if avatarErr != nil {
			log.Printf("⚠️ [CONVERSATIONAL] Avatar RAG search failed: %v", avatarErr)
		} else if avatarResult != nil && avatarResult.Metadata != nil {
			if toolSuccess, ok := avatarResult.Metadata["tool_success"].(bool); ok && toolSuccess {
				if toolResult, ok := avatarResult.Metadata["tool_result"].(map[string]interface{}); ok {
					if hasResultsInToolResult(toolResult) {
						hasAvatarResults = true
						log.Printf("✅ [CONVERSATIONAL] RAG search found results in avatar context")
					}
				}
			}
		}

		// 5. Combine results
		if hasRAGResults || hasNewsResults || hasWikiResults || hasAvatarResults {
			combinedData := map[string]interface{}{
				"neo4j_result": interpretResult,
				"source":       "neo4j_and_rag",
			}
			if hasRAGResults {
				combinedData["episodic_memory"] = ragResult
			}
			if hasNewsResults {
				combinedData["news_articles"] = newsResult
			}
			if hasWikiResults {
				combinedData["wikipedia_articles"] = wikiResult
			}
			if hasAvatarResults {
				combinedData["avatar_context"] = avatarResult
			}

			return &ActionResult{
				Type:    "knowledge_result",
				Success: true,
				Data:    combinedData,
			}, nil
		}

		log.Printf("🔍 [CONVERSATIONAL] RAG yielded no new info, using Neo4j-only results")
		return &ActionResult{
			Type:    "knowledge_result",
			Success: true,
			Data: map[string]interface{}{
				"neo4j_result": interpretResult,
				"source":       "neo4j_only",
			},
		}, nil

	case "task_execution":
		// CRITICAL: Check if this is an email/calendar request BEFORE executing task
		// If so, pass the original message directly to preserve email keywords
		originalMessage := ""
		if origMsg, ok := context["original_message"].(string); ok {
			originalMessage = origMsg
		} else if origMsg, ok := hdnContext["original_message"]; ok {
			originalMessage = origMsg
		}

		// Check if original message matches configured tool keywords
		if originalMessage != "" {
			// Use configurable tool keyword matching instead of hardcoded email checks
			if toolID := interpreter.MatchesConfiguredToolKeywords(originalMessage); toolID != "" {
				log.Printf("🔧 [CONVERSATIONAL] Detected configured tool request (%s) in task_execution - using InterpretNaturalLanguage: %s", toolID, originalMessage)
				// Pass original message directly to preserve keywords for tool detection
				interpretResult, err := cl.hdnClient.InterpretNaturalLanguage(ctx, originalMessage, hdnContext)
				if err != nil {
					return nil, fmt.Errorf("tool request interpretation failed: %w", err)
				}
				return &ActionResult{
					Type:    "conversation_result",
					Success: true,
					Data: map[string]interface{}{
						"result": interpretResult,
						"source": "hdn_natural_language",
					},
				}, nil
			}
		}

		// Use HDN's task execution for non-email tasks
		result, err := cl.hdnClient.ExecuteTask(ctx, action.Goal, hdnContext)
		if err != nil {
			return nil, fmt.Errorf("task execution failed: %w", err)
		}
		return &ActionResult{
			Type:    "task_result",
			Success: true,
			Data: map[string]interface{}{
				"result": result,
				"source": "hdn_task_execution",
			},
		}, nil

	case "planning":
		// Use HDN's planning capabilities
		plan, err := cl.hdnClient.PlanTask(ctx, action.Goal, hdnContext)
		if err != nil {
			return nil, fmt.Errorf("planning failed: %w", err)
		}
		return &ActionResult{
			Type:    "plan_result",
			Success: true,
			Data: map[string]interface{}{
				"plan":   plan,
				"source": "hdn_planning",
			},
		}, nil

	case "learning":
		// Use HDN's natural language interpretation for learning queries (allows tool usage)
		interpretResult, err := cl.hdnClient.InterpretNaturalLanguage(ctx, action.Goal, hdnContext)
		if err != nil {
			return nil, fmt.Errorf("learning failed: %w", err)
		}

		// Track tool usage if present in the result
		if interpretResult != nil && interpretResult.Metadata != nil {
			if toolID, ok := interpretResult.Metadata["tool_used"].(string); ok && toolID != "" {
				cl.reasoningTrace.AddToolInvoked(sessionID, toolID)
				log.Printf("🔧 [CONVERSATIONAL] Tracked tool invocation: %s", toolID)
			}
		}

		return &ActionResult{
			Type:    "learning_result",
			Success: true,
			Data: map[string]interface{}{
				"result": interpretResult,
				"source": "hdn_natural_language",
			},
		}, nil

	case "explanation":
		// Use HDN's natural language interpretation for explanations
		interpretResult, err := cl.hdnClient.InterpretNaturalLanguage(ctx, action.Goal, hdnContext)
		if err != nil {
			return nil, fmt.Errorf("explanation failed: %w", err)
		}

		// Track tool usage if present in the result
		if interpretResult != nil && interpretResult.Metadata != nil {
			if toolID, ok := interpretResult.Metadata["tool_used"].(string); ok && toolID != "" {
				cl.reasoningTrace.AddToolInvoked(sessionID, toolID)
				log.Printf("🔧 [CONVERSATIONAL] Tracked tool invocation: %s", toolID)
			}
		}

		return &ActionResult{
			Type:    "explanation_result",
			Success: true,
			Data: map[string]interface{}{
				"result": interpretResult,
				"source": "hdn_natural_language",
			},
		}, nil

	case "personal_update":
		log.Printf("📥 [CONVERSATIONAL] Processing personal information update")
		// Use InterpretNaturalLanguage to handle the storage via tool_save_avatar_context
		interpretResult, err := cl.hdnClient.InterpretNaturalLanguage(ctx, action.Goal, hdnContext)
		if err != nil {
			return nil, fmt.Errorf("personal update failed: %w", err)
		}

		// Track tool usage if present in the result
		if interpretResult != nil && interpretResult.Metadata != nil {
			if toolID, ok := interpretResult.Metadata["tool_used"].(string); ok && toolID != "" {
				cl.reasoningTrace.AddToolInvoked(sessionID, toolID)
				log.Printf("🔧 [CONVERSATIONAL] Tracked tool invocation: %s", toolID)
			}
		}

		return &ActionResult{
			Type:    "personal_update_result",
			Success: true,
			Data: map[string]interface{}{
				"result": interpretResult,
				"source": "hdn_personal_update",
			},
		}, nil

	case "general_conversation":
		// Check for greeting skip
		// hdnContext is map[string]string, so we check for "true"
		if val, ok := hdnContext["is_greeting"]; ok && val == "true" {
			log.Printf("ℹ️ [CONVERSATIONAL] Skipping interpreter for greeting")
			return &ActionResult{
				Type:    "conversation_result",
				Success: true,
				Data:    nil, // NLG will handle it directly
			}, nil
		}

		// Fall through to default logic for non-greeting general conversation
		fallthrough

	default:
		// For general conversation, use HDN's natural language interpretation
		// Pass the original message directly so the LLM gets the context needed for tool selection
		queryToUse := action.Goal
		if origMsg, ok := hdnContext["original_message"]; ok && origMsg != "" {
			queryToUse = origMsg
		}

		interpretResult, err := cl.hdnClient.InterpretNaturalLanguage(ctx, queryToUse, hdnContext)
		if err != nil {
			return nil, fmt.Errorf("general conversation failed: %w", err)
		}

		// Track tool usage if present in the result
		if interpretResult != nil && interpretResult.Metadata != nil {
			if toolID, ok := interpretResult.Metadata["tool_used"].(string); ok && toolID != "" {
				cl.reasoningTrace.AddToolInvoked(sessionID, toolID)
				log.Printf("🔧 [CONVERSATIONAL] Tracked tool invocation: %s", toolID)
			}
		}

		return &ActionResult{
			Type:    "conversation_result",
			Success: true,
			Data: map[string]interface{}{
				"result": interpretResult,
				"source": "hdn_natural_language",
			},
		}, nil
	}
}

// handleError creates an error response
func (cl *ConversationalLayer) handleError(message string, err error, sessionID string) (*ConversationResponse, error) {
	log.Printf("❌ [CONVERSATIONAL] %s: %v", message, err)

	// Complete reasoning trace with error
	cl.reasoningTrace.AddStep(sessionID, "error", message, map[string]interface{}{
		"error": err.Error(),
	})
	reasoningTrace := cl.reasoningTrace.CompleteTrace(sessionID)

	return &ConversationResponse{
		Response:       fmt.Sprintf("I apologize, but I encountered an error: %s", message),
		SessionID:      sessionID,
		Timestamp:      time.Now(),
		Confidence:     0.0,
		ReasoningTrace: reasoningTrace,
		Metadata: map[string]interface{}{
			"error":         true,
			"error_message": err.Error(),
		},
	}, nil
}

// GetConversationHistory returns the conversation history for a session
func (cl *ConversationalLayer) GetConversationHistory(ctx context.Context, sessionID string, limit int) ([]ConversationResponse, error) {
	return cl.conversationMemory.GetHistory(ctx, sessionID, limit)
}

// GetCurrentThinking returns the current reasoning process
func (cl *ConversationalLayer) GetCurrentThinking(ctx context.Context, sessionID string) (*ReasoningTraceData, error) {
	return cl.reasoningTrace.GetTrace(sessionID), nil
}

// hasResultsInToolResult checks if a tool result map contains actual results
func hasResultsInToolResult(toolResult map[string]interface{}) bool {
	if toolResult == nil {
		return false
	}

	// Check count first
	if val, ok := toolResult["count"]; ok {
		if count, ok := val.(float64); ok && count > 0 {
			return true
		}
		if count, ok := val.(int); ok && count > 0 {
			return true
		}
	}

	// Check results slice (handle both interface types)
	if val, ok := toolResult["results"]; ok {
		if results, ok := val.([]interface{}); ok && len(results) > 0 {
			return true
		}
		if results, ok := val.([]map[string]interface{}); ok && len(results) > 0 {
			return true
		}
	}

	return false
}

// stripActionResultForHistory creates a lean copy of an ActionResult suitable for saving in history
func (cl *ConversationalLayer) stripActionResultForHistory(res *ActionResult) *ActionResult {
	if res == nil {
		return nil
	}

	stripped := &ActionResult{
		Type:    res.Type,
		Success: res.Success,
		Error:   res.Error,
		Data:    make(map[string]interface{}),
	}

	// Only preserve key information, discard redundant or massive blobs
	for k, v := range res.Data {
		switch k {
		case "source":
			stripped.Data[k] = v
		case "result":
			// If result is an InterpretResult, it might contain the full tool output
			if ir, ok := v.(*InterpretResult); ok && ir != nil {
				strippedIR := &InterpretResult{
					Success:     ir.Success,
					Interpreted: ir.Interpreted,
					Error:       ir.Error,
					Metadata:    make(map[string]interface{}),
				}

				// Only preserve minimal metadata needed for context
				if ir.Metadata != nil {
					for mk, mv := range ir.Metadata {
						switch mk {
						case "tool_used", "response_type", "interpreted_at":
							strippedIR.Metadata[mk] = mv
						case "tool_result":
							// Deep strip tool results - keep only a preview/summary
							if tr, ok := mv.(map[string]interface{}); ok {
								strippedTR := map[string]interface{}{
									"success": tr["success"],
								}
								if results, ok := tr["results"].([]interface{}); ok {
									// Just record how many results we got, don't store 10MB of them
									strippedTR["count"] = len(results)
									if len(results) > 0 {
										strippedTR["preview"] = "Data preserved in NLG response but stripped from historical memory to save space."
									}
								}
								strippedIR.Metadata[mk] = strippedTR
							}
						}
					}
				}
				stripped.Data[k] = strippedIR
			} else if tr, ok := v.(*TaskResult); ok && tr != nil {
				stripped.Data[k] = &TaskResult{
					Success: tr.Success,
					Error:   tr.Error,
					// Do not store the full Result in history
					Result: "Result data stripped for history. Summary available in NLG response.",
				}
			} else {
				// For other result types, just use a safe summary
				stripped.Data[k] = utils.SafeResultSummary(v, 1000)
			}
		default:
			// Discard other potentially large keys
		}
	}

	// Logging size-checks is removed as it was memory intensive (json.Marshal called on potentially large blobs)
	return stripped
}
