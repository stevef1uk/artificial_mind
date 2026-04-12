package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"strings"

	"github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"
	"gopkg.in/yaml.v2"
)

// FSMConfig represents the loaded configuration
type FSMConfig struct {
	Name         string            `yaml:"name"`
	Description  string            `yaml:"description"`
	Version      string            `yaml:"version"`
	InitialState string            `yaml:"initial_state"`
	Agent        AgentConfig       `yaml:"agent"`
	Performance  PerformanceConfig `yaml:"performance"`
	States       []StateConfig     `yaml:"states"`
	Guards       []GuardConfig     `yaml:"guards"`
	Events       []EventConfig     `yaml:"events"`
	RedisKeys    RedisKeyConfig    `yaml:"redis_keys"`
	Monitoring   MonitoringConfig  `yaml:"monitoring"`
}

type AgentConfig struct {
	ID                        string  `yaml:"id"`
	Name                      string  `yaml:"name"`
	MaxConcurrentHypotheses   int     `yaml:"max_concurrent_hypotheses"`
	ConfidenceThreshold       float64 `yaml:"confidence_threshold"`
	RiskThreshold             float64 `yaml:"risk_threshold"`
	HypothesisScreenThreshold float64 `yaml:"hypothesis_screen_threshold"`
	GoalScreenThreshold       float64 `yaml:"goal_screen_threshold"`
}

type PerformanceConfig struct {
	StateTransitionDelay  float64 `yaml:"state_transition_delay"`
	EventLoopSleepMs      int     `yaml:"event_loop_sleep_ms"`
	TimerIntervalSeconds  int     `yaml:"timer_interval_seconds"`
	MaxEventsPerCycle     int     `yaml:"max_events_per_cycle"`
	PostProcessingSleepMs int     `yaml:"post_processing_sleep_ms"`
	IdleSleepMs           int     `yaml:"idle_sleep_ms"`
}

type StateConfig struct {
	Name        string                `yaml:"name"`
	Description string                `yaml:"description"`
	TimeoutMs   int                   `yaml:"timeout_ms"`
	Actions     []ActionConfig        `yaml:"actions"`
	On          map[string]Transition `yaml:"on"`
}

type ActionConfig struct {
	Type   string                 `yaml:"type"`
	Module string                 `yaml:"module"`
	Params map[string]interface{} `yaml:"params"`
}

type Transition struct {
	Next  string `yaml:"next"`
	Guard string `yaml:"guard,omitempty"`
}

type GuardConfig struct {
	Name   string                 `yaml:"name"`
	Module string                 `yaml:"module"`
	Params map[string]interface{} `yaml:"params"`
}

type EventConfig struct {
	Name          string `yaml:"name"`
	NatsSubject   string `yaml:"nats_subject"`
	PayloadSchema string `yaml:"payload_schema,omitempty"`
	IntervalMs    int    `yaml:"interval_ms,omitempty"`
}

type RedisKeyConfig struct {
	State          string `yaml:"state"`
	Context        string `yaml:"context"`
	Queue          string `yaml:"queue"`
	Beliefs        string `yaml:"beliefs"`
	Episodes       string `yaml:"episodes"`
	Hypotheses     string `yaml:"hypotheses"`
	DomainInsights string `yaml:"domain_insights"`
}

type MonitoringConfig struct {
	Metrics  []MetricConfig  `yaml:"metrics"`
	UIPanels []UIPanelConfig `yaml:"ui_panels"`
}

type MetricConfig struct {
	Name string `yaml:"name"`
	Type string `yaml:"type"`
}

type UIPanelConfig struct {
	Name       string `yaml:"name"`
	Type       string `yaml:"type"`
	DataSource string `yaml:"data_source"`
}

// FSMEngine represents the running state machine
type FSMEngine struct {
	config               *FSMConfig
	agentID              string
	currentState         string
	context              map[string]interface{}
	nc                   *nats.Conn
	redis                *redis.Client
	subs                 []*nats.Subscription
	ctx                  context.Context
	cancel               context.CancelFunc
	principles           *PrinciplesIntegration
	knowledgeGrowth      *KnowledgeGrowthEngine
	knowledgeIntegration *KnowledgeIntegration
	reasoning            *ReasoningEngine
	coherenceMonitor     *CoherenceMonitor
	explanationLearning  *ExplanationLearningFeedback
	goalManager          *GoalManagerClient
	stateEntryTime       time.Time // Track when current state was entered
}

// ActivityLogEntry represents a human-readable activity log entry
type ActivityLogEntry struct {
	Timestamp time.Time `json:"timestamp"`
	Message   string    `json:"message"`
	State     string    `json:"state,omitempty"`
	Action    string    `json:"action,omitempty"`
	Details   string    `json:"details,omitempty"`
	Category  string    `json:"category"` // "state_change", "action", "learning", "hypothesis", "decision"
}

// CanonicalEvent represents the standard event envelope
type CanonicalEvent struct {
	EventID   string                 `json:"event_id"`
	Source    string                 `json:"source"`
	Type      string                 `json:"type"`
	Timestamp string                 `json:"timestamp"`
	Context   map[string]interface{} `json:"context"`
	Payload   map[string]interface{} `json:"payload"`
	Security  map[string]interface{} `json:"security,omitempty"`
}

// FSMTransitionEvent represents a state transition
type FSMTransitionEvent struct {
	AgentID   string                 `json:"agent_id"`
	From      string                 `json:"from"`
	To        string                 `json:"to"`
	Reason    string                 `json:"reason"`
	Timestamp string                 `json:"timestamp"`
	Context   map[string]interface{} `json:"context,omitempty"`
}

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

// NewFSMEngine creates a new FSM engine
func NewFSMEngine(configPath string, agentID string, nc *nats.Conn, redis *redis.Client, principlesURL string, hdnURL string) (*FSMEngine, error) {
	config, err := LoadConfig(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	principles := NewPrinciplesIntegration(principlesURL)

	knowledgeGrowth := NewKnowledgeGrowthEngine(hdnURL, redis)

	knowledgeIntegration := NewKnowledgeIntegration(hdnURL, principlesURL, redis)

	reasoning := NewReasoningEngine(hdnURL, redis)

	goalMgrURL := os.Getenv("GOAL_MANAGER_URL")
	if goalMgrURL == "" {
		goalMgrURL = "http://localhost:8090"
	}
	goalManager := NewGoalManagerClient(goalMgrURL, redis)

	coherenceMonitor := NewCoherenceMonitor(redis, hdnURL, reasoning, agentID, nc, goalManager)

	explanationLearning := NewExplanationLearningFeedback(redis, hdnURL)

	engine := &FSMEngine{
		config:               config,
		agentID:              agentID,
		currentState:         config.InitialState,
		context:              make(map[string]interface{}),
		nc:                   nc,
		redis:                redis,
		ctx:                  ctx,
		cancel:               cancel,
		principles:           principles,
		knowledgeGrowth:      knowledgeGrowth,
		knowledgeIntegration: knowledgeIntegration,
		reasoning:            reasoning,
		coherenceMonitor:     coherenceMonitor,
		explanationLearning:  explanationLearning,
		goalManager:          goalManager,
		stateEntryTime:       time.Now(),
	}

	if config.Performance.StateTransitionDelay == 0 {
		config.Performance.StateTransitionDelay = 0.5
	}
	if config.Performance.EventLoopSleepMs == 0 {
		config.Performance.EventLoopSleepMs = 100
	}
	if config.Performance.TimerIntervalSeconds == 0 {
		config.Performance.TimerIntervalSeconds = 2
	}
	if config.Performance.MaxEventsPerCycle == 0 {
		config.Performance.MaxEventsPerCycle = 5
	}
	if config.Performance.PostProcessingSleepMs == 0 {
		config.Performance.PostProcessingSleepMs = 50
	}
	if config.Performance.IdleSleepMs == 0 {
		config.Performance.IdleSleepMs = 200
	}

	log.Printf("🔍 Initial state before loading: %s", engine.currentState)
	if err := engine.loadState(); err != nil {
		log.Printf("Warning: Could not load state from Redis: %v", err)
	} else {
		log.Printf("🔄 Loaded state from Redis: %s", engine.currentState)
	}

	if !principles.IsPrinciplesServerAvailable() {
		log.Printf("⚠️  WARNING: Principles Server is not available - FSM may not function correctly")
	} else {
		log.Printf("✅ Principles Server is available and ready")
	}

	log.Printf("🚀 FSM Performance Settings:")
	log.Printf("  - State transition delay: %.1f seconds", config.Performance.StateTransitionDelay)
	log.Printf("  - Event loop sleep: %dms", config.Performance.EventLoopSleepMs)
	log.Printf("  - Timer interval: %d seconds", config.Performance.TimerIntervalSeconds)
	log.Printf("  - Max events per cycle: %d", config.Performance.MaxEventsPerCycle)
	log.Printf("  - Post-processing sleep: %dms", config.Performance.PostProcessingSleepMs)
	log.Printf("  - Idle sleep: %dms", config.Performance.IdleSleepMs)

	projectID := fmt.Sprintf("fsm-agent-%s", agentID)
	engine.context["project_id"] = projectID
	engine.ensureHDNProject(projectID)

	engine.logActivity("System started", "state_change", map[string]string{
		"state":   engine.currentState,
		"details": "FSM engine initialized and ready",
	})

	return engine, nil
}

// LoadConfig loads FSM configuration from YAML file
func LoadConfig(configPath string) (*FSMConfig, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}

	var config FSMConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	return &config, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// getStringFromMap safely extracts a string value from a map
func getStringFromMap(m map[string]interface{}, key string) string {
	if val, exists := m[key]; exists && val != nil {
		if str, ok := val.(string); ok {
			return str
		}
	}
	return ""
}

// containsIgnoreCase checks if substr in s (case-insensitive)
func containsIgnoreCase(s, substr string) bool {
	ls := strings.ToLower(s)
	lsub := strings.ToLower(substr)
	return strings.Contains(ls, lsub)
}

// Helper functions for type conversion
func getString(m map[string]interface{}, key, defaultValue string) string {
	if val, ok := m[key].(string); ok {
		return val
	}
	return defaultValue
}

func getFloat64(m map[string]interface{}, key string, defaultValue float64) float64 {
	if val, ok := m[key].(float64); ok {
		return val
	}
	return defaultValue
}

// getMapKeys returns all keys from a map as a slice
func getMapKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// HypothesisTestResult represents the result of testing a hypothesis
type HypothesisTestResult struct {
	Status     string                   `json:"status"`     // confirmed, refuted, inconclusive
	Confidence float64                  `json:"confidence"` // 0.0 to 1.0
	Evaluation string                   `json:"evaluation"` // human-readable evaluation
	Evidence   []map[string]interface{} `json:"evidence"`   // supporting evidence
}

// ToolResult represents the result of executing a hypothesis testing tool
type ToolResult struct {
	Success    bool    `json:"success"`
	Confidence float64 `json:"confidence"`
	Result     string  `json:"result"`
}
