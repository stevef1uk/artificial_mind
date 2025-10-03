package conversational

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// ThoughtEvent represents an AI's internal thought process
type ThoughtEvent struct {
	AgentID    string                 `json:"agent_id"`
	SessionID  string                 `json:"session_id,omitempty"`
	Type       string                 `json:"type"`       // "thinking", "decision", "action", "observation"
	State      string                 `json:"state"`      // Current FSM state
	Goal       string                 `json:"goal"`       // Current goal/objective
	Thought    string                 `json:"thought"`    // Natural language thought
	Confidence float64                `json:"confidence"` // 0.0-1.0
	ToolUsed   string                 `json:"tool_used,omitempty"`
	Action     string                 `json:"action,omitempty"`
	Result     string                 `json:"result,omitempty"`
	Timestamp  string                 `json:"timestamp"`
	Context    map[string]interface{} `json:"context,omitempty"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
}

// ThoughtExpressionService converts reasoning traces to natural language thoughts
type ThoughtExpressionService struct {
	redis     *redis.Client
	llmClient LLMClientInterface
}

// ThoughtExpressionRequest represents a request to express thoughts
type ThoughtExpressionRequest struct {
	SessionID     string                 `json:"session_id"`
	TraceData     *ReasoningTraceData    `json:"trace_data,omitempty"`
	ThoughtEvents []ThoughtEvent         `json:"thought_events,omitempty"`
	Style         string                 `json:"style,omitempty"` // "conversational", "technical", "streaming"
	Context       map[string]interface{} `json:"context,omitempty"`
}

// ThoughtExpressionResponse represents the expressed thoughts
type ThoughtExpressionResponse struct {
	Thoughts    []ExpressedThought     `json:"thoughts"`
	Summary     string                 `json:"summary,omitempty"`
	Confidence  float64                `json:"confidence"`
	GeneratedAt time.Time              `json:"generated_at"`
	SessionID   string                 `json:"session_id"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// ExpressedThought represents a single expressed thought
type ExpressedThought struct {
	Type       string                 `json:"type"`       // "thinking", "decision", "action", "observation"
	Content    string                 `json:"content"`    // Natural language thought
	State      string                 `json:"state"`      // FSM state when thought occurred
	Goal       string                 `json:"goal"`       // Goal being pursued
	Confidence float64                `json:"confidence"` // 0.0-1.0
	ToolUsed   string                 `json:"tool_used,omitempty"`
	Action     string                 `json:"action,omitempty"`
	Result     string                 `json:"result,omitempty"`
	Timestamp  time.Time              `json:"timestamp"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
}

// NewThoughtExpressionService creates a new thought expression service
func NewThoughtExpressionService(redis *redis.Client, llmClient LLMClientInterface) *ThoughtExpressionService {
	return &ThoughtExpressionService{
		redis:     redis,
		llmClient: llmClient,
	}
}

// ExpressThoughts converts reasoning traces to natural language thoughts
func (tes *ThoughtExpressionService) ExpressThoughts(ctx context.Context, req *ThoughtExpressionRequest) (*ThoughtExpressionResponse, error) {
	log.Printf("🧠 [THOUGHT-EXPRESSION] Expressing thoughts for session: %s", req.SessionID)

	var thoughts []ExpressedThought
	var confidence float64

	// Process trace data if provided
	if req.TraceData != nil {
		traceThoughts, traceConfidence := tes.processTraceData(req.TraceData, req.Style)
		thoughts = append(thoughts, traceThoughts...)
		confidence = traceConfidence
	}

	// Process thought events if provided
	if len(req.ThoughtEvents) > 0 {
		eventThoughts, eventConfidence := tes.processThoughtEvents(req.ThoughtEvents, req.Style)
		thoughts = append(thoughts, eventThoughts...)
		if confidence == 0 {
			confidence = eventConfidence
		} else {
			confidence = (confidence + eventConfidence) / 2
		}
	}

	// Generate summary if multiple thoughts
	var summary string
	if len(thoughts) > 1 {
		summary = tes.generateSummary(thoughts, req.Style)
	}

	response := &ThoughtExpressionResponse{
		Thoughts:    thoughts,
		Summary:     summary,
		Confidence:  confidence,
		GeneratedAt: time.Now(),
		SessionID:   req.SessionID,
		Metadata: map[string]interface{}{
			"thought_count": len(thoughts),
			"style":         req.Style,
		},
	}

	log.Printf("🧠 [THOUGHT-EXPRESSION] Generated %d thoughts for session: %s", len(thoughts), req.SessionID)
	return response, nil
}

// processTraceData converts reasoning trace data to expressed thoughts
func (tes *ThoughtExpressionService) processTraceData(trace *ReasoningTraceData, style string) ([]ExpressedThought, float64) {
	var thoughts []ExpressedThought

	// Process reasoning steps
	for _, step := range trace.ReasoningSteps {
		thought := ExpressedThought{
			Type:       "thinking",
			Content:    tes.formatReasoningStep(step, style),
			State:      trace.FSMState,
			Goal:       trace.CurrentGoal,
			Confidence: 0.7, // Default confidence for reasoning steps
			Timestamp:  step.Timestamp,
			Metadata: map[string]interface{}{
				"step":        step.Step,
				"duration":    step.Duration.Seconds(),
				"input_keys":  len(step.Input),
				"output_keys": len(step.Output),
			},
		}
		thoughts = append(thoughts, thought)
	}

	// Process decisions
	for _, decision := range trace.Decisions {
		thought := ExpressedThought{
			Type:       "decision",
			Content:    tes.formatDecision(decision, style),
			State:      trace.FSMState,
			Goal:       trace.CurrentGoal,
			Confidence: decision.Confidence,
			Timestamp:  decision.Timestamp,
			Metadata: map[string]interface{}{
				"options_count": len(decision.Options),
				"chosen":        decision.Chosen,
			},
		}
		thoughts = append(thoughts, thought)
	}

	// Process actions
	for _, action := range trace.Actions {
		thought := ExpressedThought{
			Type:       "action",
			Content:    tes.formatAction(action, style),
			State:      trace.FSMState,
			Goal:       trace.CurrentGoal,
			Confidence: 0.8, // High confidence for actions
			Timestamp:  time.Now(),
			Action:     action,
			Metadata: map[string]interface{}{
				"action_type": "executed",
			},
		}
		thoughts = append(thoughts, thought)
	}

	// Process tool invocations
	for _, tool := range trace.ToolsInvoked {
		thought := ExpressedThought{
			Type:       "action",
			Content:    tes.formatToolInvocation(tool, style),
			State:      trace.FSMState,
			Goal:       trace.CurrentGoal,
			Confidence: 0.8,
			Timestamp:  time.Now(),
			ToolUsed:   tool,
			Metadata: map[string]interface{}{
				"tool_type": "invoked",
			},
		}
		thoughts = append(thoughts, thought)
	}

	return thoughts, trace.Confidence
}

// processThoughtEvents converts thought events to expressed thoughts
func (tes *ThoughtExpressionService) processThoughtEvents(events []ThoughtEvent, style string) ([]ExpressedThought, float64) {
	var thoughts []ExpressedThought
	var totalConfidence float64

	for _, event := range events {
		thought := ExpressedThought{
			Type:       event.Type,
			Content:    event.Thought,
			State:      event.State,
			Goal:       event.Goal,
			Confidence: event.Confidence,
			ToolUsed:   event.ToolUsed,
			Action:     event.Action,
			Result:     event.Result,
			Timestamp:  time.Now(), // Parse from event.Timestamp if needed
			Metadata:   event.Metadata,
		}
		thoughts = append(thoughts, thought)
		totalConfidence += event.Confidence
	}

	avgConfidence := totalConfidence / float64(len(events))
	return thoughts, avgConfidence
}

// formatReasoningStep formats a reasoning step into natural language
func (tes *ThoughtExpressionService) formatReasoningStep(step ReasoningStep, style string) string {
	switch style {
	case "technical":
		return fmt.Sprintf("Step %s: %s (Duration: %v)", step.Step, step.Description, step.Duration)
	case "conversational":
		return fmt.Sprintf("I'm %s. %s", step.Step, step.Description)
	case "streaming":
		return fmt.Sprintf("(thinking) %s", step.Description)
	default:
		return step.Description
	}
}

// formatDecision formats a decision into natural language
func (tes *ThoughtExpressionService) formatDecision(decision DecisionPoint, style string) string {
	switch style {
	case "technical":
		return fmt.Sprintf("Decision: %s. Chose: %s. Reasoning: %s", decision.Description, decision.Chosen, decision.Reasoning)
	case "conversational":
		return fmt.Sprintf("I decided to %s because %s", decision.Chosen, decision.Reasoning)
	case "streaming":
		return fmt.Sprintf("(deciding) %s", decision.Description)
	default:
		return decision.Description
	}
}

// formatAction formats an action into natural language
func (tes *ThoughtExpressionService) formatAction(action string, style string) string {
	switch style {
	case "technical":
		return fmt.Sprintf("Executed action: %s", action)
	case "conversational":
		return fmt.Sprintf("I'm executing: %s", action)
	case "streaming":
		return fmt.Sprintf("(acting) %s", action)
	default:
		return action
	}
}

// formatToolInvocation formats a tool invocation into natural language
func (tes *ThoughtExpressionService) formatToolInvocation(tool string, style string) string {
	switch style {
	case "technical":
		return fmt.Sprintf("Invoked tool: %s", tool)
	case "conversational":
		return fmt.Sprintf("I'm using the %s tool", tool)
	case "streaming":
		return fmt.Sprintf("(using %s) Executing tool...", tool)
	default:
		return fmt.Sprintf("Using tool: %s", tool)
	}
}

// generateSummary creates a summary of multiple thoughts
func (tes *ThoughtExpressionService) generateSummary(thoughts []ExpressedThought, style string) string {
	if len(thoughts) == 0 {
		return ""
	}

	// Count thought types
	typeCounts := make(map[string]int)
	for _, thought := range thoughts {
		typeCounts[thought.Type]++
	}

	var summaryParts []string
	for thoughtType, count := range typeCounts {
		switch thoughtType {
		case "thinking":
			summaryParts = append(summaryParts, fmt.Sprintf("%d reasoning steps", count))
		case "decision":
			summaryParts = append(summaryParts, fmt.Sprintf("%d decisions", count))
		case "action":
			summaryParts = append(summaryParts, fmt.Sprintf("%d actions", count))
		}
	}

	switch style {
	case "conversational":
		return fmt.Sprintf("I went through %s in total.", strings.Join(summaryParts, ", "))
	case "technical":
		return fmt.Sprintf("Processed %s.", strings.Join(summaryParts, ", "))
	case "streaming":
		return fmt.Sprintf("(summary) Completed %s", strings.Join(summaryParts, ", "))
	default:
		return strings.Join(summaryParts, ", ")
	}
}

// GetRecentThoughts retrieves recent thought events for a session
func (tes *ThoughtExpressionService) GetRecentThoughts(ctx context.Context, sessionID string, limit int) ([]ThoughtEvent, error) {
	// Get recent thought events from Redis
	pattern := fmt.Sprintf("thought_events:%s:*", sessionID)
	keys, err := tes.redis.Keys(ctx, pattern).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get thought event keys: %w", err)
	}

	var events []ThoughtEvent
	for i, key := range keys {
		if i >= limit {
			break
		}

		data, err := tes.redis.Get(ctx, key).Result()
		if err != nil {
			log.Printf("⚠️ [THOUGHT-EXPRESSION] Failed to load thought event %s: %v", key, err)
			continue
		}

		var event ThoughtEvent
		err = json.Unmarshal([]byte(data), &event)
		if err != nil {
			log.Printf("❌ [THOUGHT-EXPRESSION] Failed to unmarshal thought event %s: %v", key, err)
			continue
		}

		events = append(events, event)
	}

	return events, nil
}

// StoreThoughtEvent stores a thought event in Redis
func (tes *ThoughtExpressionService) StoreThoughtEvent(ctx context.Context, event ThoughtEvent) error {
	key := fmt.Sprintf("thought_events:%s:%s", event.SessionID, event.Timestamp)

	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal thought event: %w", err)
	}

	// Store with 24 hour expiration
	err = tes.redis.Set(ctx, key, data, 24*time.Hour).Err()
	if err != nil {
		return fmt.Errorf("failed to store thought event: %w", err)
	}

	return nil
}
