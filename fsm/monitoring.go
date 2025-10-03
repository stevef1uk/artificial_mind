package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
)

// FSMMonitor handles monitoring and observability for the FSM
type FSMMonitor struct {
	fsmEngine *FSMEngine
	redis     *redis.Client
	ctx       context.Context
}

// NewFSMMonitor creates a new FSM monitor
func NewFSMMonitor(fsmEngine *FSMEngine, redis *redis.Client) *FSMMonitor {
	return &FSMMonitor{
		fsmEngine: fsmEngine,
		redis:     redis,
		ctx:       context.Background(),
	}
}

// FSMStatus represents the current status of the FSM
type FSMStatus struct {
	AgentID          string                 `json:"agent_id"`
	CurrentState     string                 `json:"current_state"`
	StateHistory     []StateTransition      `json:"state_history"`
	Context          map[string]interface{} `json:"context"`
	Performance      PerformanceMetrics     `json:"performance"`
	KnowledgeGrowth  KnowledgeGrowthStats   `json:"knowledge_growth"`
	PrinciplesChecks PrinciplesStats        `json:"principles_checks"`
	LastActivity     time.Time              `json:"last_activity"`
	Uptime           time.Duration          `json:"uptime"`
	HealthStatus     string                 `json:"health_status"`
}

type StateTransition struct {
	From      string                 `json:"from"`
	To        string                 `json:"to"`
	Reason    string                 `json:"reason"`
	Timestamp time.Time              `json:"timestamp"`
	Context   map[string]interface{} `json:"context,omitempty"`
	Duration  time.Duration          `json:"duration"`
}

type PerformanceMetrics struct {
	TransitionsPerSecond float64 `json:"transitions_per_second"`
	EventsProcessed      int64   `json:"events_processed"`
	AverageStateTime     float64 `json:"average_state_time_seconds"`
	IdleTimePercentage   float64 `json:"idle_time_percentage"`
	ErrorRate            float64 `json:"error_rate"`
	MemoryUsage          int64   `json:"memory_usage_bytes"`
	CPUUsage             float64 `json:"cpu_usage_percent"`
}

type KnowledgeGrowthStats struct {
	ConceptsCreated        int       `json:"concepts_created"`
	RelationshipsAdded     int       `json:"relationships_added"`
	ConstraintsAdded       int       `json:"constraints_added"`
	ExamplesAdded          int       `json:"examples_added"`
	GapsFilled             int       `json:"gaps_filled"`
	ContradictionsResolved int       `json:"contradictions_resolved"`
	GrowthRate             float64   `json:"growth_rate_percent"`
	ConsistencyScore       float64   `json:"consistency_score"`
	LastGrowthTime         time.Time `json:"last_growth_time"`
}

type PrinciplesStats struct {
	TotalChecks         int       `json:"total_checks"`
	AllowedActions      int       `json:"allowed_actions"`
	BlockedActions      int       `json:"blocked_actions"`
	ErrorCount          int       `json:"error_count"`
	AverageResponseTime float64   `json:"average_response_time_ms"`
	LastCheckTime       time.Time `json:"last_check_time"`
	BlockedRules        []string  `json:"blocked_rules"`
}

// GetFSMStatus returns the current status of the FSM
func (fm *FSMMonitor) GetFSMStatus() (*FSMStatus, error) {
	agentID := fm.fsmEngine.agentID
	currentState := fm.fsmEngine.GetCurrentState()
	context := fm.fsmEngine.GetContext()

	// Get state history
	stateHistory, err := fm.getStateHistory(agentID)
	if err != nil {
		log.Printf("Warning: Could not get state history: %v", err)
		stateHistory = []StateTransition{}
	}

	// Get performance metrics
	performance, err := fm.getPerformanceMetrics(agentID)
	if err != nil {
		log.Printf("Warning: Could not get performance metrics: %v", err)
		performance = PerformanceMetrics{}
	}

	// Get knowledge growth stats
	knowledgeGrowth, err := fm.getKnowledgeGrowthStats(agentID)
	if err != nil {
		log.Printf("Warning: Could not get knowledge growth stats: %v", err)
		knowledgeGrowth = KnowledgeGrowthStats{}
	}

	// Get principles stats
	principlesChecks, err := fm.getPrinciplesStats(agentID)
	if err != nil {
		log.Printf("Warning: Could not get principles stats: %v", err)
		principlesChecks = PrinciplesStats{}
	}

	// Get last activity
	lastActivity, err := fm.getLastActivity(agentID)
	if err != nil {
		lastActivity = time.Now()
	}

	// Get uptime
	uptime, err := fm.getUptime(agentID)
	if err != nil {
		uptime = 0
	}

	// Determine health status
	healthStatus := fm.determineHealthStatus(performance, principlesChecks)

	status := &FSMStatus{
		AgentID:          agentID,
		CurrentState:     currentState,
		StateHistory:     stateHistory,
		Context:          context,
		Performance:      performance,
		KnowledgeGrowth:  knowledgeGrowth,
		PrinciplesChecks: principlesChecks,
		LastActivity:     lastActivity,
		Uptime:           uptime,
		HealthStatus:     healthStatus,
	}

	return status, nil
}

// GetStateTimeline returns a timeline of state transitions
func (fm *FSMMonitor) GetStateTimeline(agentID string, hours int) ([]StateTransition, error) {
	key := fmt.Sprintf("fsm:%s:state_timeline", agentID)

	// Get transitions from the last N hours
	cutoff := time.Now().Add(-time.Duration(hours) * time.Hour)

	results, err := fm.redis.LRange(fm.ctx, key, 0, -1).Result()
	if err != nil {
		return nil, err
	}

	var transitions []StateTransition
	for _, result := range results {
		var transition StateTransition
		if err := json.Unmarshal([]byte(result), &transition); err != nil {
			continue
		}

		if transition.Timestamp.After(cutoff) {
			transitions = append(transitions, transition)
		}
	}

	return transitions, nil
}

// GetKnowledgeGrowthTimeline returns knowledge growth over time
func (fm *FSMMonitor) GetKnowledgeGrowthTimeline(agentID string, hours int) ([]KnowledgeGrowthStats, error) {
	key := fmt.Sprintf("fsm:%s:knowledge_growth_timeline", agentID)

	cutoff := time.Now().Add(-time.Duration(hours) * time.Hour)

	results, err := fm.redis.LRange(fm.ctx, key, 0, -1).Result()
	if err != nil {
		return nil, err
	}

	var stats []KnowledgeGrowthStats
	for _, result := range results {
		var stat KnowledgeGrowthStats
		if err := json.Unmarshal([]byte(result), &stat); err != nil {
			continue
		}

		if stat.LastGrowthTime.After(cutoff) {
			stats = append(stats, stat)
		}
	}

	return stats, nil
}

// GetActiveHypotheses returns currently active hypotheses
func (fm *FSMMonitor) GetActiveHypotheses(agentID string) ([]map[string]interface{}, error) {
	key := fmt.Sprintf("fsm:%s:hypotheses", agentID)

	results, err := fm.redis.HGetAll(fm.ctx, key).Result()
	if err != nil {
		return nil, err
	}

	var hypotheses []map[string]interface{}
	for _, result := range results {
		var hypothesis map[string]interface{}
		if err := json.Unmarshal([]byte(result), &hypothesis); err != nil {
			continue
		}

		// Only include active hypotheses
		if status, ok := hypothesis["status"].(string); ok && status == "proposed" {
			hypotheses = append(hypotheses, hypothesis)
		}
	}

	return hypotheses, nil
}

// GetRecentEpisodes returns recent episodes
func (fm *FSMMonitor) GetRecentEpisodes(agentID string, limit int) ([]map[string]interface{}, error) {
	key := fmt.Sprintf("fsm:%s:episodes", agentID)

	results, err := fm.redis.LRange(fm.ctx, key, 0, int64(limit-1)).Result()
	if err != nil {
		return nil, err
	}

	var episodes []map[string]interface{}
	for _, result := range results {
		var episode map[string]interface{}
		if err := json.Unmarshal([]byte(result), &episode); err != nil {
			continue
		}
		episodes = append(episodes, episode)
	}

	return episodes, nil
}

// GetThinkingProcess returns what the mind is currently thinking
func (fm *FSMMonitor) GetThinkingProcess(agentID string) (map[string]interface{}, error) {
	context := fm.fsmEngine.GetContext()
	currentState := fm.fsmEngine.GetCurrentState()

	thinking := map[string]interface{}{
		"current_state":      currentState,
		"state_description":  fm.getStateDescription(currentState),
		"current_context":    context,
		"active_hypotheses":  context["hypotheses"],
		"current_domain":     context["current_domain"],
		"principles_status":  context["principles_allowed"],
		"knowledge_gaps":     context["knowledge_gaps"],
		"recent_discoveries": context["discoveries"],
		"thinking_focus":     fm.getThinkingFocus(currentState, context),
		"next_actions":       fm.getNextActions(currentState, context),
		"confidence_level":   fm.getConfidenceLevel(context),
		"last_activity":      time.Now(),
	}

	return thinking, nil
}

// Helper methods
func (fm *FSMMonitor) getStateHistory(agentID string) ([]StateTransition, error) {
	key := fmt.Sprintf("fsm:%s:state_timeline", agentID)
	results, err := fm.redis.LRange(fm.ctx, key, 0, 99).Result()
	if err != nil {
		return nil, err
	}

	var transitions []StateTransition
	for _, result := range results {
		var transition StateTransition
		if err := json.Unmarshal([]byte(result), &transition); err != nil {
			continue
		}
		transitions = append(transitions, transition)
	}

	return transitions, nil
}

func (fm *FSMMonitor) getPerformanceMetrics(agentID string) (PerformanceMetrics, error) {
	key := fmt.Sprintf("fsm:%s:performance", agentID)
	result, err := fm.redis.HGetAll(fm.ctx, key).Result()
	if err != nil {
		return PerformanceMetrics{}, err
	}

	metrics := PerformanceMetrics{}
	if val, exists := result["transitions_per_second"]; exists {
		fmt.Sscanf(val, "%f", &metrics.TransitionsPerSecond)
	}
	if val, exists := result["events_processed"]; exists {
		fmt.Sscanf(val, "%d", &metrics.EventsProcessed)
	}
	if val, exists := result["average_state_time_seconds"]; exists {
		fmt.Sscanf(val, "%f", &metrics.AverageStateTime)
	}
	if val, exists := result["idle_time_percentage"]; exists {
		fmt.Sscanf(val, "%f", &metrics.IdleTimePercentage)
	}
	if val, exists := result["error_rate"]; exists {
		fmt.Sscanf(val, "%f", &metrics.ErrorRate)
	}

	return metrics, nil
}

func (fm *FSMMonitor) getKnowledgeGrowthStats(agentID string) (KnowledgeGrowthStats, error) {
	key := fmt.Sprintf("fsm:%s:knowledge_growth", agentID)
	result, err := fm.redis.HGetAll(fm.ctx, key).Result()
	if err != nil {
		return KnowledgeGrowthStats{}, err
	}

	stats := KnowledgeGrowthStats{}
	if val, exists := result["concepts_created"]; exists {
		fmt.Sscanf(val, "%d", &stats.ConceptsCreated)
	}
	if val, exists := result["relationships_added"]; exists {
		fmt.Sscanf(val, "%d", &stats.RelationshipsAdded)
	}
	if val, exists := result["constraints_added"]; exists {
		fmt.Sscanf(val, "%d", &stats.ConstraintsAdded)
	}
	if val, exists := result["examples_added"]; exists {
		fmt.Sscanf(val, "%d", &stats.ExamplesAdded)
	}
	if val, exists := result["gaps_filled"]; exists {
		fmt.Sscanf(val, "%d", &stats.GapsFilled)
	}
	if val, exists := result["growth_rate_percent"]; exists {
		fmt.Sscanf(val, "%f", &stats.GrowthRate)
	}
	if val, exists := result["consistency_score"]; exists {
		fmt.Sscanf(val, "%f", &stats.ConsistencyScore)
	}

	return stats, nil
}

func (fm *FSMMonitor) getPrinciplesStats(agentID string) (PrinciplesStats, error) {
	key := fmt.Sprintf("fsm:%s:principles", agentID)
	result, err := fm.redis.HGetAll(fm.ctx, key).Result()
	if err != nil {
		return PrinciplesStats{}, err
	}

	stats := PrinciplesStats{}
	if val, exists := result["total_checks"]; exists {
		fmt.Sscanf(val, "%d", &stats.TotalChecks)
	}
	if val, exists := result["allowed_actions"]; exists {
		fmt.Sscanf(val, "%d", &stats.AllowedActions)
	}
	if val, exists := result["blocked_actions"]; exists {
		fmt.Sscanf(val, "%d", &stats.BlockedActions)
	}
	if val, exists := result["error_count"]; exists {
		fmt.Sscanf(val, "%d", &stats.ErrorCount)
	}
	if val, exists := result["average_response_time_ms"]; exists {
		fmt.Sscanf(val, "%f", &stats.AverageResponseTime)
	}

	return stats, nil
}

func (fm *FSMMonitor) getLastActivity(agentID string) (time.Time, error) {
	key := fmt.Sprintf("fsm:%s:last_activity", agentID)
	result, err := fm.redis.Get(fm.ctx, key).Result()
	if err != nil {
		return time.Time{}, err
	}

	var lastActivity time.Time
	if err := lastActivity.UnmarshalText([]byte(result)); err != nil {
		return time.Time{}, err
	}

	return lastActivity, nil
}

func (fm *FSMMonitor) getUptime(agentID string) (time.Duration, error) {
	key := fmt.Sprintf("fsm:%s:start_time", agentID)
	result, err := fm.redis.Get(fm.ctx, key).Result()
	if err != nil {
		return 0, err
	}

	var startTime time.Time
	if err := startTime.UnmarshalText([]byte(result)); err != nil {
		return 0, err
	}

	return time.Since(startTime), nil
}

func (fm *FSMMonitor) determineHealthStatus(performance PerformanceMetrics, principles PrinciplesStats) string {
	// Determine health based on performance and principles stats
	if performance.ErrorRate > 0.1 {
		return "unhealthy"
	}
	if principles.ErrorCount > 5 {
		return "degraded"
	}
	if performance.TransitionsPerSecond > 0 {
		return "healthy"
	}
	return "idle"
}

func (fm *FSMMonitor) getStateDescription(state string) string {
	descriptions := map[string]string{
		"idle":        "Waiting for input or timer events",
		"perceive":    "Ingesting and validating new data using domain knowledge",
		"learn":       "Extracting facts and updating domain knowledge - GROWING KNOWLEDGE BASE",
		"summarize":   "Compressing episodes into structured facts",
		"hypothesize": "Generating hypotheses using domain knowledge and constraints",
		"plan":        "Creating hierarchical plans using domain-specific success rates",
		"decide":      "Choosing action using principles and domain constraints - CHECKING PRINCIPLES",
		"act":         "Executing planned action with domain-aware monitoring",
		"observe":     "Collecting outcomes and validating against domain expectations",
		"evaluate":    "Comparing outcomes to domain knowledge and updating beliefs - GROWING KNOWLEDGE BASE",
		"archive":     "Checkpointing episode and updating domain knowledge",
		"fail":        "Handling errors with domain-aware recovery",
		"paused":      "Manual pause state",
		"shutdown":    "Clean shutdown with knowledge base preservation",
	}

	if desc, exists := descriptions[state]; exists {
		return desc
	}
	return "Unknown state"
}

func (fm *FSMMonitor) getThinkingFocus(state string, context map[string]interface{}) string {
	switch state {
	case "perceive":
		return "Analyzing input and classifying domain"
	case "learn":
		return "Extracting knowledge and discovering new concepts"
	case "hypothesize":
		return "Generating theories and explanations"
	case "plan":
		return "Creating execution plans and strategies"
	case "decide":
		return "Evaluating options and checking principles"
	case "act":
		return "Executing actions with safety monitoring"
	case "observe":
		return "Collecting and analyzing results"
	case "evaluate":
		return "Learning from outcomes and growing knowledge"
	default:
		return "Processing and thinking"
	}
}

func (fm *FSMMonitor) getNextActions(state string, context map[string]interface{}) []string {
	switch state {
	case "idle":
		return []string{"Wait for input", "Process timer events"}
	case "perceive":
		return []string{"Classify domain", "Validate input", "Extract context"}
	case "learn":
		return []string{"Extract facts", "Discover concepts", "Find gaps", "Update knowledge"}
	case "summarize":
		return []string{"Compress episodes", "Create facts", "Update beliefs"}
	case "hypothesize":
		return []string{"Generate theories", "Check constraints", "Validate hypotheses"}
	case "plan":
		return []string{"Create plans", "Rank options", "Check success rates"}
	case "decide":
		return []string{"Check principles", "Evaluate safety", "Choose action"}
	case "act":
		return []string{"Execute action", "Monitor progress", "Handle errors"}
	case "observe":
		return []string{"Collect results", "Validate outcomes", "Measure metrics"}
	case "evaluate":
		return []string{"Compare results", "Update knowledge", "Grow concepts", "Validate consistency"}
	case "archive":
		return []string{"Save episode", "Update memory", "Create checkpoint"}
	default:
		return []string{"Process state", "Handle transitions"}
	}
}

func (fm *FSMMonitor) getConfidenceLevel(context map[string]interface{}) float64 {
	if confidence, ok := context["confidence"].(float64); ok {
		return confidence
	}
	if confidence, ok := context["principles_confidence"].(float64); ok {
		return confidence
	}
	return 0.5 // Default confidence
}

// ForceStateTransition forces the FSM to transition to a specific state
func (fm *FSMMonitor) ForceStateTransition(targetState string) error {
	// Get the current FSM engine
	engine := fm.fsmEngine

	// Get current state before transition
	fromState := engine.GetCurrentState()

	// Force the state change
	engine.currentState = targetState

	// Log the forced transition
	log.Printf("🔄 [FSM] Forced state transition from %s to: %s", fromState, targetState)

	// Record the transition in state history
	transition := StateTransition{
		From:      fromState,
		To:        targetState,
		Timestamp: time.Now(),
		Reason:    "Forced by manual reset",
		Context:   map[string]interface{}{"event": "manual_reset"},
	}

	// Store the transition in Redis
	key := fmt.Sprintf("fsm:%s:state_timeline", engine.agentID)
	transitionData, err := json.Marshal(transition)
	if err != nil {
		log.Printf("Warning: Could not marshal transition: %v", err)
		return err
	}

	if err := fm.redis.LPush(fm.ctx, key, transitionData).Err(); err != nil {
		log.Printf("Warning: Could not store transition: %v", err)
		return err
	}

	// Keep only last 100 transitions
	fm.redis.LTrim(fm.ctx, key, 0, 99)

	return nil
}
