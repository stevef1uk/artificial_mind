package conversational

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

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

	// Step 1: Parse intent and determine what the user wants
	intent, err := cl.intentParser.ParseIntent(ctx, req.Message, req.Context)
	if err != nil {
		return cl.handleError("Failed to parse intent", err, req.SessionID)
	}

	cl.reasoningTrace.AddStep("intent_parsing", fmt.Sprintf("Parsed user intent: %s (confidence: %.2f)", intent.Type, intent.Confidence), map[string]interface{}{
		"intent_type": intent.Type,
		"confidence":  intent.Confidence,
		"entities":    intent.Entities,
		"goal":        intent.Goal,
	})

	// Step 2: Load conversation context
	conversationContext, err := cl.conversationMemory.GetContext(ctx, req.SessionID)
	if err != nil {
		log.Printf("⚠️ [CONVERSATIONAL] Failed to load conversation context: %v", err)
		conversationContext = make(map[string]interface{})
	}

	// Merge explicit request context (from UI/API) into conversation context
	if req.Context != nil {
		for k, v := range req.Context {
			// Prefer explicit request context values over stored ones
			conversationContext[k] = v
		}
	}

	cl.reasoningTrace.AddStep("context_loading", "Loaded conversation context", map[string]interface{}{
		"context_keys": len(conversationContext),
	})

	// Step 2b: Load relevant conversation summaries (RAG)
	summaries, err := cl.summarizer.GetRelevantSummaries(ctx, req.SessionID, req.Message)
	if err != nil {
		log.Printf("⚠️ [CONVERSATIONAL] Failed to load conversation summaries: %v", err)
	} else if len(summaries) > 0 {
		conversationContext["conversation_summaries"] = summaries
		cl.reasoningTrace.AddStep("summary_retrieval", fmt.Sprintf("Retrieved %d relevant conversation summaries", len(summaries)), map[string]interface{}{
			"summary_count": len(summaries),
		})
	}

	// Step 3: Determine the appropriate action based on intent
	action, err := cl.determineAction(ctx, intent, conversationContext)
	if err != nil {
		return cl.handleError("Failed to determine action", err, req.SessionID)
	}

	cl.reasoningTrace.AddStep("action_determination", fmt.Sprintf("Determined action: %s to achieve: %s", action.Type, action.Goal), map[string]interface{}{
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
	cl.reasoningTrace.AddStep("action_execution", resultDesc, map[string]interface{}{
		"success":     result.Success,
		"output_type": result.Type,
		"error":       result.Error,
	})

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
	if err != nil {
		return cl.handleError("Failed to generate response", err, req.SessionID)
	}

	responsePreview := response.Text
	if len(responsePreview) > 100 {
		responsePreview = responsePreview[:100] + "..."
	}
	cl.reasoningTrace.AddStep("response_generation", fmt.Sprintf("Generated response (confidence: %.2f): %s", response.Confidence, responsePreview), map[string]interface{}{
		"response_length": len(response.Text),
		"confidence":      response.Confidence,
	})

	// Step 6: Save conversation context
	err = cl.conversationMemory.SaveContext(ctx, req.SessionID, map[string]interface{}{
		"last_user_message": req.Message,
		"last_ai_response":  response.Text,
		"last_intent":       intent,
		"last_action":       action,
		"last_result":       result,
		"timestamp":         time.Now(),
	})
	if err != nil {
		log.Printf("⚠️ [CONVERSATIONAL] Failed to save conversation context: %v", err)
	}

	// Step 6b: Check if we should summarize the conversation
	shouldSummarize, _ := cl.summarizer.ShouldSummarize(ctx, req.SessionID)
	if shouldSummarize {
		// Get conversation history and summarize
		if history, ok := conversationContext["conversation_history"].([]ConversationTurn); ok {
			go func() {
				summarizeCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				err := cl.summarizer.SummarizeConversation(summarizeCtx, req.SessionID, history)
				if err != nil {
					log.Printf("⚠️ [CONVERSATIONAL] Async summarization failed: %v", err)
				}
			}()
		}
	}

	// Step 7: Complete reasoning trace
	reasoningTrace := cl.reasoningTrace.CompleteTrace(req.SessionID)

	// Step 8: Generate thought expression if requested
	var thoughtExpression *ThoughtExpressionResponse
	if req.ShowThinking {
		thoughtReq := &ThoughtExpressionRequest{
			SessionID: req.SessionID,
			TraceData: reasoningTrace,
			Style:     "conversational",
			Context:   conversationContext,
		}

		thoughtExpression, err = cl.thoughtExpression.ExpressThoughts(ctx, thoughtReq)
		if err != nil {
			log.Printf("⚠️ [CONVERSATIONAL] Failed to generate thought expression: %v", err)
		}
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

	// Add thought expression to response if available
	if thoughtExpression != nil {
		conversationResponse.Thoughts = thoughtExpression.Thoughts
		conversationResponse.ThinkingSummary = thoughtExpression.Summary
		conversationResponse.Metadata["thought_count"] = len(thoughtExpression.Thoughts)
		conversationResponse.Metadata["thinking_confidence"] = thoughtExpression.Confidence

		// Store thoughts as ThoughtEvents in Redis for later retrieval
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
	}

	// Add reasoning trace if requested
	if req.ShowThinking {
		conversationResponse.ReasoningTrace = reasoningTrace
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
				"task":    intent.Entities["task"],
				"context": context,
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
		return &Action{
			Type: "general_conversation",
			Goal: "Respond to user in a helpful way",
			Parameters: map[string]interface{}{
				"message": intent.OriginalMessage,
			},
		}, nil
	}
}

// executeAction executes the determined action using FSM + HDN
func (cl *ConversationalLayer) executeAction(ctx context.Context, action *Action, context map[string]interface{}) (*ActionResult, error) {
	log.Printf("🎯 [CONVERSATIONAL] Executing action: %s - %s", action.Type, action.Goal)

	// Convert context to string map for HDN
	hdnContext := make(map[string]string)
	forceKnowledgeQuery := false
	knowledgeSources := "neo4j"
	for k, v := range context {
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
		if str, ok := v.(string); ok {
			hdnContext[k] = str
		} else {
			hdnContext[k] = fmt.Sprintf("%v", v)
		}
	}

	// Add action parameters to context
	for k, v := range action.Parameters {
		hdnContext[k] = fmt.Sprintf("%v", v)
	}

	// Add original message if available in context for knowledge query extraction
	if origMsg, ok := context["original_message"].(string); ok {
		hdnContext["original_message"] = origMsg
	} else if origMsg, ok := context["message"].(string); ok {
		hdnContext["original_message"] = origMsg
	}

	switch action.Type {
	case "knowledge_query":
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
		// Try to get the original message from context first
		originalMessage := ""
		if origMsg, ok := hdnContext["original_message"]; ok {
			originalMessage = origMsg
		} else if origMsg, ok := context["original_message"].(string); ok {
			originalMessage = origMsg
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
			words := strings.Fields(strings.ToLower(text))
			skipWords := map[string]bool{
				"the": true, "a": true, "an": true, "and": true, "or": true, "but": true,
				"in": true, "on": true, "at": true, "to": true, "for": true, "of": true,
				"with": true, "by": true, "is": true, "are": true, "was": true, "were": true,
				"who": true, "what": true, "where": true, "when": true, "why": true, "how": true,
				"tell": true, "me": true, "about": true, "search": true, "find": true,
				"news": true, "latest": true, "current": true, "recent": true,
			}
			filtered := make([]string, 0)
			for _, word := range words {
				word = strings.Trim(word, ".,!?;:()[]{}'\"")
				if !skipWords[word] && len(word) > 2 {
					filtered = append(filtered, word)
				}
			}
			if len(filtered) > 0 {
				return strings.Join(filtered, " ")
			}
			return text // Return original if all words filtered
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
				"get": true, "show": true, "give": true,
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

		// Create a very direct tool call instruction for Neo4j
		directQuery := fmt.Sprintf("Query your knowledge base about '%s'. Use the mcp_get_concept tool with name='%s' and domain='General' to retrieve information.", coreQuery, coreQuery)
		log.Printf("🔍 [CONVERSATIONAL] Simplified knowledge query: %s (extracted from: %s)", directQuery, searchText)
		interpretResult, err := cl.hdnClient.InterpretNaturalLanguage(ctx, directQuery, hdnContext)
		if err != nil {
			return nil, fmt.Errorf("knowledge query failed: %w", err)
		}

		// Track tool usage if present in the result
		if interpretResult != nil && interpretResult.Metadata != nil {
			if toolID, ok := interpretResult.Metadata["tool_used"].(string); ok && toolID != "" {
				cl.reasoningTrace.AddToolInvoked(toolID)
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
		ragQueryText := strings.ToLower(strings.TrimSpace(coreQuery))
		if ragQueryText == "" || ragQueryText == "unknown" {
			// If extraction failed, try to extract from original message
			ragQueryText = strings.ToLower(strings.TrimSpace(searchText))
			// Remove common question words
			words := strings.Fields(ragQueryText)
			filtered := make([]string, 0)
			skipWords := map[string]bool{"who": true, "what": true, "where": true, "when": true, "why": true, "how": true,
				"is": true, "are": true, "the": true, "a": true, "an": true, "tell": true, "me": true, "about": true,
				"latest": true, "situation": true, "current": true, "update": true, "in": true, "of": true,
				"search": true, "for": true, "news": true, "find": true, "get": true, "show": true, "give": true,
				"now": true, "today": true, "currently": true, "recently": true, "right": true, "at": true, "on": true}
			for _, word := range words {
				if !skipWords[word] && len(word) > 2 {
					filtered = append(filtered, word)
				}
			}
			if len(filtered) > 0 {
				ragQueryText = strings.Join(filtered, " ")
			}
		}

		if ragQueryText == "" {
			log.Printf("⚠️ [CONVERSATIONAL] Could not extract query for RAG search, returning Neo4j-only results")
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
		// Use HDN's task execution
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
				cl.reasoningTrace.AddToolInvoked(toolID)
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
				cl.reasoningTrace.AddToolInvoked(toolID)
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
		interpretResult, err := cl.hdnClient.InterpretNaturalLanguage(ctx, "Save the following personal information: "+action.Goal, hdnContext)
		if err != nil {
			return nil, fmt.Errorf("personal update failed: %w", err)
		}

		// Track tool usage if present in the result
		if interpretResult != nil && interpretResult.Metadata != nil {
			if toolID, ok := interpretResult.Metadata["tool_used"].(string); ok && toolID != "" {
				cl.reasoningTrace.AddToolInvoked(toolID)
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

	default:
		// For general conversation, use HDN's natural language interpretation
		interpretResult, err := cl.hdnClient.InterpretNaturalLanguage(ctx, action.Goal, hdnContext)
		if err != nil {
			return nil, fmt.Errorf("general conversation failed: %w", err)
		}

		// Track tool usage if present in the result
		if interpretResult != nil && interpretResult.Metadata != nil {
			if toolID, ok := interpretResult.Metadata["tool_used"].(string); ok && toolID != "" {
				cl.reasoningTrace.AddToolInvoked(toolID)
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
	cl.reasoningTrace.AddStep("error", message, map[string]interface{}{
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
