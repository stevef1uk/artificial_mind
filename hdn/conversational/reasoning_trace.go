package conversational

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"sync"

	"github.com/redis/go-redis/v9"
)

// ReasoningTrace tracks the AI's reasoning process
type ReasoningTrace struct {
	redis  *redis.Client
	traces map[string]*ReasoningTraceData
	mu     sync.RWMutex
}

// ReasoningTraceData contains the complete reasoning trace
type ReasoningTraceData struct {
	SessionID      string                 `json:"session_id"`
	StartTime      time.Time              `json:"start_time"`
	EndTime        time.Time              `json:"end_time"`
	CurrentGoal    string                 `json:"current_goal"`
	FSMState       string                 `json:"fsm_state"`
	Actions        []string               `json:"actions"`
	KnowledgeUsed  []string               `json:"knowledge_used"`
	ToolsInvoked   []string               `json:"tools_invoked"`
	Decisions      []DecisionPoint        `json:"decisions"`
	Confidence     float64                `json:"confidence"`
	ReasoningSteps []ReasoningStep        `json:"reasoning_steps"`
	Metadata       map[string]interface{} `json:"metadata,omitempty"`
}

// DecisionPoint represents a key decision made during reasoning
type DecisionPoint struct {
	Description string                 `json:"description"`
	Options     []string               `json:"options"`
	Chosen      string                 `json:"chosen"`
	Reasoning   string                 `json:"reasoning"`
	Confidence  float64                `json:"confidence"`
	Timestamp   time.Time              `json:"timestamp"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// ReasoningStep represents a single step in the reasoning process
type ReasoningStep struct {
	Step        string                 `json:"step"`
	Description string                 `json:"description"`
	Input       map[string]interface{} `json:"input,omitempty"`
	Output      map[string]interface{} `json:"output,omitempty"`
	Duration    time.Duration          `json:"duration"`
	Timestamp   time.Time              `json:"timestamp"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// NewReasoningTrace creates a new reasoning trace system
func NewReasoningTrace(redis *redis.Client) *ReasoningTrace {
	return &ReasoningTrace{
		redis:  redis,
		traces: make(map[string]*ReasoningTraceData),
	}
}

// StartTrace starts a new reasoning trace for a session
func (rt *ReasoningTrace) StartTrace(sessionID string) {
	trace := &ReasoningTraceData{
		SessionID:      sessionID,
		StartTime:      time.Now(),
		Actions:        make([]string, 0),
		KnowledgeUsed:  make([]string, 0),
		ToolsInvoked:   make([]string, 0),
		Decisions:      make([]DecisionPoint, 0),
		ReasoningSteps: make([]ReasoningStep, 0),
		Metadata:       make(map[string]interface{}),
	}

	rt.mu.Lock()
	rt.traces[sessionID] = trace
	rt.mu.Unlock()
	log.Printf("🧠 [REASONING-TRACE] Started trace for session: %s", sessionID)
}

// AddStep adds a reasoning step to the trace
func (rt *ReasoningTrace) AddStep(step string, description string, input map[string]interface{}) {
	latestTrace := rt.getLatestTrace()

	if latestTrace == nil {
		log.Printf("⚠️ [REASONING-TRACE] No active trace found for step: %s", step)
		return
	}

	reasoningStep := ReasoningStep{
		Step:        step,
		Description: description,
		Input:       input,
		Timestamp:   time.Now(),
		Metadata:    make(map[string]interface{}),
	}

	// Calculate duration if this is not the first step
	if len(latestTrace.ReasoningSteps) > 0 {
		lastStep := latestTrace.ReasoningSteps[len(latestTrace.ReasoningSteps)-1]
		reasoningStep.Duration = reasoningStep.Timestamp.Sub(lastStep.Timestamp)
	}

	latestTrace.ReasoningSteps = append(latestTrace.ReasoningSteps, reasoningStep)

	log.Printf("🧠 [REASONING-TRACE] Added step: %s - %s", step, description)
}

// AddDecision adds a decision point to the trace
func (rt *ReasoningTrace) AddDecision(description string, options []string, chosen string, reasoning string, confidence float64) {
	// Find the most recent trace
	latestTrace := rt.getLatestTrace()

	if latestTrace == nil {
		log.Printf("⚠️ [REASONING-TRACE] No active trace found for decision: %s", description)
		return
	}

	decision := DecisionPoint{
		Description: description,
		Options:     options,
		Chosen:      chosen,
		Reasoning:   reasoning,
		Confidence:  confidence,
		Timestamp:   time.Now(),
		Metadata:    make(map[string]interface{}),
	}

	latestTrace.Decisions = append(latestTrace.Decisions, decision)

	log.Printf("🧠 [REASONING-TRACE] Added decision: %s -> %s (confidence: %.2f)", description, chosen, confidence)
}

// AddAction adds an action to the trace
func (rt *ReasoningTrace) AddAction(action string) {
	// Find the most recent trace
	latestTrace := rt.getLatestTrace()

	if latestTrace == nil {
		log.Printf("⚠️ [REASONING-TRACE] No active trace found for action: %s", action)
		return
	}

	latestTrace.Actions = append(latestTrace.Actions, action)
	log.Printf("🧠 [REASONING-TRACE] Added action: %s", action)
}

// AddKnowledgeUsed adds knowledge source to the trace
func (rt *ReasoningTrace) AddKnowledgeUsed(source string) {
	// Find the most recent trace
	latestTrace := rt.getLatestTrace()

	if latestTrace == nil {
		log.Printf("⚠️ [REASONING-TRACE] No active trace found for knowledge: %s", source)
		return
	}

	latestTrace.KnowledgeUsed = append(latestTrace.KnowledgeUsed, source)
	log.Printf("🧠 [REASONING-TRACE] Added knowledge source: %s", source)
}

// AddToolInvoked adds a tool invocation to the trace
func (rt *ReasoningTrace) AddToolInvoked(tool string) {
	// Find the most recent trace
	latestTrace := rt.getLatestTrace()

	if latestTrace == nil {
		log.Printf("⚠️ [REASONING-TRACE] No active trace found for tool: %s", tool)
		return
	}

	latestTrace.ToolsInvoked = append(latestTrace.ToolsInvoked, tool)
	log.Printf("🧠 [REASONING-TRACE] Added tool invocation: %s", tool)
}

// SetGoal sets the current goal for the trace
func (rt *ReasoningTrace) SetGoal(goal string) {
	// Find the most recent trace
	latestTrace := rt.getLatestTrace()

	if latestTrace == nil {
		log.Printf("⚠️ [REASONING-TRACE] No active trace found for goal: %s", goal)
		return
	}

	latestTrace.CurrentGoal = goal
	log.Printf("🧠 [REASONING-TRACE] Set goal: %s", goal)
}

// SetFSMState sets the current FSM state for the trace
func (rt *ReasoningTrace) SetFSMState(state string) {
	// Find the most recent trace
	latestTrace := rt.getLatestTrace()

	if latestTrace == nil {
		log.Printf("⚠️ [REASONING-TRACE] No active trace found for FSM state: %s", state)
		return
	}

	latestTrace.FSMState = state
	log.Printf("🧠 [REASONING-TRACE] Set FSM state: %s", state)
}

// SetConfidence sets the overall confidence for the trace
func (rt *ReasoningTrace) SetConfidence(confidence float64) {
	// Find the most recent trace
	latestTrace := rt.getLatestTrace()

	if latestTrace == nil {
		log.Printf("⚠️ [REASONING-TRACE] No active trace found for confidence: %.2f", confidence)
		return
	}

	latestTrace.Confidence = confidence
	log.Printf("🧠 [REASONING-TRACE] Set confidence: %.2f", confidence)
}

// CompleteTrace completes the reasoning trace and returns it
func (rt *ReasoningTrace) CompleteTrace(sessionID string) *ReasoningTraceData {
	rt.mu.RLock()
	trace, exists := rt.traces[sessionID]
	rt.mu.RUnlock()
	if !exists {
		log.Printf("⚠️ [REASONING-TRACE] No trace found for session: %s", sessionID)
		return nil
	}

	trace.EndTime = time.Now()

	// Calculate overall confidence if not set
	if trace.Confidence == 0 {
		trace.Confidence = rt.calculateOverallConfidence(trace)
	}

	// Save to Redis for persistence
	rt.saveTraceToRedis(sessionID, trace)

	// Remove from memory
	rt.mu.Lock()
	delete(rt.traces, sessionID)
	rt.mu.Unlock()

	log.Printf("🧠 [REASONING-TRACE] Completed trace for session: %s (duration: %v)", sessionID, trace.EndTime.Sub(trace.StartTime))

	return trace
}

// GetTrace returns the current trace for a session
func (rt *ReasoningTrace) GetTrace(sessionID string) *ReasoningTraceData {
	rt.mu.RLock()
	trace, exists := rt.traces[sessionID]
	rt.mu.RUnlock()
	if !exists {
		// Try to load from Redis
		return rt.loadTraceFromRedis(sessionID)
	}
	return trace
}

// calculateOverallConfidence calculates overall confidence from decisions and steps
func (rt *ReasoningTrace) calculateOverallConfidence(trace *ReasoningTraceData) float64 {
	if len(trace.Decisions) == 0 {
		return 0.5 // Default confidence
	}

	totalConfidence := 0.0
	for _, decision := range trace.Decisions {
		totalConfidence += decision.Confidence
	}

	return totalConfidence / float64(len(trace.Decisions))
}

// saveTraceToRedis saves the trace to Redis for persistence
func (rt *ReasoningTrace) saveTraceToRedis(sessionID string, trace *ReasoningTraceData) {
	key := fmt.Sprintf("reasoning_trace:%s", sessionID)

	data, err := json.Marshal(trace)
	if err != nil {
		log.Printf("❌ [REASONING-TRACE] Failed to marshal trace: %v", err)
		return
	}

	// Save with 24 hour expiration
	err = rt.redis.Set(context.Background(), key, data, 24*time.Hour).Err()
	if err != nil {
		log.Printf("❌ [REASONING-TRACE] Failed to save trace to Redis: %v", err)
	}
}

// loadTraceFromRedis loads a trace from Redis
func (rt *ReasoningTrace) loadTraceFromRedis(sessionID string) *ReasoningTraceData {
	key := fmt.Sprintf("reasoning_trace:%s", sessionID)

	data, err := rt.redis.Get(context.Background(), key).Result()
	if err != nil {
		log.Printf("⚠️ [REASONING-TRACE] Failed to load trace from Redis: %v", err)
		return nil
	}

	var trace ReasoningTraceData
	err = json.Unmarshal([]byte(data), &trace)
	if err != nil {
		log.Printf("❌ [REASONING-TRACE] Failed to unmarshal trace: %v", err)
		return nil
	}

	return &trace
}

// GetRecentTraces returns recent reasoning traces
func (rt *ReasoningTrace) GetRecentTraces(limit int) ([]*ReasoningTraceData, error) {
	// Get all trace keys from Redis
	pattern := "reasoning_trace:*"
	keys, err := rt.redis.Keys(context.Background(), pattern).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get trace keys: %w", err)
	}

	var traces []*ReasoningTraceData
	for i, key := range keys {
		if i >= limit {
			break
		}

		data, err := rt.redis.Get(context.Background(), key).Result()
		if err != nil {
			log.Printf("⚠️ [REASONING-TRACE] Failed to load trace %s: %v", key, err)
			continue
		}

		var trace ReasoningTraceData
		err = json.Unmarshal([]byte(data), &trace)
		if err != nil {
			log.Printf("❌ [REASONING-TRACE] Failed to unmarshal trace %s: %v", key, err)
			continue
		}

		traces = append(traces, &trace)
	}

	return traces, nil
}

// ClearOldTraces removes traces older than the specified duration
func (rt *ReasoningTrace) ClearOldTraces(olderThan time.Duration) error {
	pattern := "reasoning_trace:*"
	keys, err := rt.redis.Keys(context.Background(), pattern).Result()
	if err != nil {
		return fmt.Errorf("failed to get trace keys: %w", err)
	}

	cutoff := time.Now().Add(-olderThan)
	deleted := 0

	for _, key := range keys {
		data, err := rt.redis.Get(context.Background(), key).Result()
		if err != nil {
			continue
		}

		var trace ReasoningTraceData
		err = json.Unmarshal([]byte(data), &trace)
		if err != nil {
			continue
		}

		if trace.StartTime.Before(cutoff) {
			err = rt.redis.Del(context.Background(), key).Err()
			if err == nil {
				deleted++
			}
		}
	}

	log.Printf("🧠 [REASONING-TRACE] Cleared %d old traces", deleted)
	return nil
}

// getLatestTrace returns the most recently started trace
func (rt *ReasoningTrace) getLatestTrace() *ReasoningTraceData {
	var latestTrace *ReasoningTraceData
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	for _, trace := range rt.traces {
		if latestTrace == nil || trace.StartTime.After(latestTrace.StartTime) {
			latestTrace = trace
		}
	}
	return latestTrace
}
