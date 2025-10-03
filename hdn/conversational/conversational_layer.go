package conversational

import (
	"context"
	"fmt"
	"log"
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

	cl.reasoningTrace.AddStep("intent_parsing", "Parsed user intent", map[string]interface{}{
		"intent_type": intent.Type,
		"confidence":  intent.Confidence,
		"entities":    intent.Entities,
	})

	// Step 2: Load conversation context
	conversationContext, err := cl.conversationMemory.GetContext(ctx, req.SessionID)
	if err != nil {
		log.Printf("⚠️ [CONVERSATIONAL] Failed to load conversation context: %v", err)
		conversationContext = make(map[string]interface{})
	}

	cl.reasoningTrace.AddStep("context_loading", "Loaded conversation context", map[string]interface{}{
		"context_keys": len(conversationContext),
	})

	// Step 3: Determine the appropriate action based on intent
	action, err := cl.determineAction(ctx, intent, conversationContext)
	if err != nil {
		return cl.handleError("Failed to determine action", err, req.SessionID)
	}

	cl.reasoningTrace.AddStep("action_determination", "Determined action to take", map[string]interface{}{
		"action_type": action.Type,
		"action_goal": action.Goal,
	})

	// Step 4: Execute the action using FSM + HDN
	result, err := cl.executeAction(ctx, action, conversationContext)
	if err != nil {
		return cl.handleError("Failed to execute action", err, req.SessionID)
	}

	cl.reasoningTrace.AddStep("action_execution", "Executed action", map[string]interface{}{
		"success":     result.Success,
		"output_type": result.Type,
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

	cl.reasoningTrace.AddStep("response_generation", "Generated natural language response", map[string]interface{}{
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
	}

	// Add reasoning trace if requested
	if req.ShowThinking {
		conversationResponse.ReasoningTrace = reasoningTrace
	}

	return conversationResponse, nil
}

// determineAction determines what action to take based on intent and context
func (cl *ConversationalLayer) determineAction(ctx context.Context, intent *Intent, context map[string]interface{}) (*Action, error) {
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
	for k, v := range context {
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

	switch action.Type {
	case "knowledge_query":
		// Use HDN's intelligent execution for knowledge queries
		result, err := cl.hdnClient.ExecuteTask(ctx, action.Goal, hdnContext)
		if err != nil {
			return nil, fmt.Errorf("knowledge query failed: %w", err)
		}
		return &ActionResult{
			Type:    "knowledge_result",
			Success: true,
			Data: map[string]interface{}{
				"result": result,
				"source": "hdn_intelligent_execution",
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
		// Use HDN's learning capabilities
		learnResult, err := cl.hdnClient.LearnFromLLM(ctx, action.Goal, hdnContext)
		if err != nil {
			return nil, fmt.Errorf("learning failed: %w", err)
		}
		return &ActionResult{
			Type:    "learning_result",
			Success: true,
			Data: map[string]interface{}{
				"result": learnResult,
				"source": "hdn_learning",
			},
		}, nil

	case "explanation":
		// Use HDN's natural language interpretation for explanations
		interpretResult, err := cl.hdnClient.InterpretNaturalLanguage(ctx, action.Goal, hdnContext)
		if err != nil {
			return nil, fmt.Errorf("explanation failed: %w", err)
		}
		return &ActionResult{
			Type:    "explanation_result",
			Success: true,
			Data: map[string]interface{}{
				"result": interpretResult,
				"source": "hdn_natural_language",
			},
		}, nil

	default:
		// For general conversation, use HDN's natural language interpretation
		interpretResult, err := cl.hdnClient.InterpretNaturalLanguage(ctx, action.Goal, hdnContext)
		if err != nil {
			return nil, fmt.Errorf("general conversation failed: %w", err)
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
