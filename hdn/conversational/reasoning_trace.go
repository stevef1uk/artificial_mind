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
func (rt *ReasoningTrace) AddStep(sessionID string, step string, description string, input map[string]interface{}) {
	trace := rt.GetTrace(sessionID)

	if trace == nil {
		log.Printf("⚠️ [REASONING-TRACE] No active trace found for session %s at step: %s", sessionID, step)
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
	if len(trace.ReasoningSteps) > 0 {
		lastStep := trace.ReasoningSteps[len(trace.ReasoningSteps)-1]
		reasoningStep.Duration = reasoningStep.Timestamp.Sub(lastStep.Timestamp)
	}

	trace.ReasoningSteps = append(trace.ReasoningSteps, reasoningStep)

	log.Printf("🧠 [REASONING-TRACE] [%s] Added step: %s - %s", sessionID, step, description)
}

// AddDecision adds a decision point to the trace
func (rt *ReasoningTrace) AddDecision(sessionID string, description string, options []string, chosen string, reasoning string, confidence float64) {
	trace := rt.GetTrace(sessionID)

	if trace == nil {
		log.Printf("⚠️ [REASONING-TRACE] No active trace found for session %s at decision: %s", sessionID, description)
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

	trace.Decisions = append(trace.Decisions, decision)

	log.Printf("🧠 [REASONING-TRACE] [%s] Added decision: %s -> %s (confidence: %.2f)", sessionID, description, chosen, confidence)
}

// AddAction adds an action to the trace
func (rt *ReasoningTrace) AddAction(sessionID string, action string) {
	trace := rt.GetTrace(sessionID)

	if trace == nil {
		log.Printf("⚠️ [REASONING-TRACE] No active trace found for session %s at action: %s", sessionID, action)
		return
	}

	trace.Actions = append(trace.Actions, action)
	log.Printf("🧠 [REASONING-TRACE] [%s] Added action: %s", sessionID, action)
}

// AddKnowledgeUsed adds knowledge source to the trace
func (rt *ReasoningTrace) AddKnowledgeUsed(sessionID string, source string) {
	trace := rt.GetTrace(sessionID)

	if trace == nil {
		log.Printf("⚠️ [REASONING-TRACE] No active trace found for session %s at knowledge: %s", sessionID, source)
		return
	}

	trace.KnowledgeUsed = append(trace.KnowledgeUsed, source)
	log.Printf("🧠 [REASONING-TRACE] [%s] Added knowledge source: %s", sessionID, source)
}

// AddToolInvoked adds a tool invocation to the trace
func (rt *ReasoningTrace) AddToolInvoked(sessionID string, tool string) {
	trace := rt.GetTrace(sessionID)

	if trace == nil {
		log.Printf("⚠️ [REASONING-TRACE] No active trace found for session %s at tool: %s", sessionID, tool)
		return
	}

	trace.ToolsInvoked = append(trace.ToolsInvoked, tool)
	log.Printf("🧠 [REASONING-TRACE] [%s] Added tool invocation: %s", sessionID, tool)
}

// SetGoal sets the current goal for the trace
func (rt *ReasoningTrace) SetGoal(sessionID string, goal string) {
	trace := rt.GetTrace(sessionID)

	if trace == nil {
		log.Printf("⚠️ [REASONING-TRACE] No active trace found for session %s at goal: %s", sessionID, goal)
		return
	}

	trace.CurrentGoal = goal
	log.Printf("🧠 [REASONING-TRACE] [%s] Set goal: %s", sessionID, goal)
}

// SetFSMState sets the current FSM state for the trace
func (rt *ReasoningTrace) SetFSMState(sessionID string, state string) {
	trace := rt.GetTrace(sessionID)

	if trace == nil {
		log.Printf("⚠️ [REASONING-TRACE] No active trace found for session %s at FSM state: %s", sessionID, state)
		return
	}

	trace.FSMState = state
	log.Printf("🧠 [REASONING-TRACE] [%s] Set FSM state: %s", sessionID, state)
}

// SetConfidence sets the overall confidence for the trace
func (rt *ReasoningTrace) SetConfidence(sessionID string, confidence float64) {
	trace := rt.GetTrace(sessionID)

	if trace == nil {
		log.Printf("⚠️ [REASONING-TRACE] No active trace found for session %s at confidence: %.2f", sessionID, confidence)
		return
	}

	trace.Confidence = confidence
	log.Printf("🧠 [REASONING-TRACE] [%s] Set confidence: %.2f", sessionID, confidence)
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

func (rt *ReasoningTrace) getTraceForSession(sessionID string) *ReasoningTraceData {
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	return rt.traces[sessionID]
}
