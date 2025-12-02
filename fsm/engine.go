package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"time"

	"bytes"
	"net/http"
	"strings"

	"github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"
	"gopkg.in/yaml.v2"

	mempkg "agi/hdn/memory"
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

	// Create principles integration
	principles := NewPrinciplesIntegration(principlesURL)

	// Create knowledge growth engine (needs HDN URL for LLM-based concept extraction)
	knowledgeGrowth := NewKnowledgeGrowthEngine(hdnURL, redis)

	// Create knowledge integration engine
	knowledgeIntegration := NewKnowledgeIntegration(hdnURL, principlesURL, redis)

	// Create reasoning engine using provided HDN URL
	reasoning := NewReasoningEngine(hdnURL, redis)

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
		stateEntryTime:       time.Now(),
	}

	// Set default performance values if not specified
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

	// Load current state from Redis if exists
	log.Printf("🔍 Initial state before loading: %s", engine.currentState)
	if err := engine.loadState(); err != nil {
		log.Printf("Warning: Could not load state from Redis: %v", err)
	} else {
		log.Printf("🔄 Loaded state from Redis: %s", engine.currentState)
	}

	// Verify Principles Server is available
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

	// Ensure HDN project exists and set project context for persistence across Redis clears
	projectID := fmt.Sprintf("fsm-agent-%s", agentID)
	engine.context["project_id"] = projectID
	engine.ensureHDNProject(projectID)

	// Log initial activity
	engine.logActivity("System started", "state_change", map[string]string{
		"state":   engine.currentState,
		"details": "FSM engine initialized and ready",
	})

	return engine, nil
}

// logActivity logs a human-readable activity entry to Redis
func (e *FSMEngine) logActivity(message, category string, extras map[string]string) {
	entry := ActivityLogEntry{
		Timestamp: time.Now(),
		Message:   message,
		Category:  category,
		State:     e.currentState,
	}

	// Add any extra details
	if state, ok := extras["state"]; ok && state != "" {
		entry.State = state
	}
	if action, ok := extras["action"]; ok && action != "" {
		entry.Action = action
	}
	if details, ok := extras["details"]; ok && details != "" {
		entry.Details = details
	}

	// Store in Redis (keep last 200 entries)
	key := fmt.Sprintf("fsm:%s:activity_log", e.agentID)
	data, err := json.Marshal(entry)
	if err == nil {
		e.redis.LPush(e.ctx, key, data)
		e.redis.LTrim(e.ctx, key, 0, 199)          // Keep last 200 entries
		e.redis.Expire(e.ctx, key, 7*24*time.Hour) // Expire after 7 days
	}

	// Also log to console for immediate visibility
	log.Printf("📋 [ACTIVITY] %s", message)
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

// Start begins the FSM event loop
func (e *FSMEngine) Start() error {
	log.Printf("Starting FSM engine for agent %s in state %s", e.agentID, e.currentState)

	// Subscribe to events
	if err := e.subscribeToEvents(); err != nil {
		return fmt.Errorf("failed to subscribe to events: %w", err)
	}

	// Start timer for periodic checks
	go e.timerLoop()

	// Start main event loop
	go e.eventLoop()

	return nil
}

// Stop gracefully shuts down the FSM
func (e *FSMEngine) Stop() error {
	log.Printf("Stopping FSM engine for agent %s", e.agentID)

	// Unsubscribe from all events
	for _, sub := range e.subs {
		sub.Unsubscribe()
	}

	// Save current state
	if err := e.saveState(); err != nil {
		log.Printf("Warning: Could not save state: %v", err)
	}

	e.cancel()
	return nil
}

// subscribeToEvents sets up NATS subscriptions
func (e *FSMEngine) subscribeToEvents() error {
	log.Printf("🔌 Setting up NATS subscriptions for %d events", len(e.config.Events))
	for _, event := range e.config.Events {
		if event.NatsSubject == "" {
			log.Printf("⚠️  Skipping event %s - no NATS subject", event.Name)
			continue
		}

		log.Printf("📡 Subscribing to NATS subject: %s (event: %s)", event.NatsSubject, event.Name)
		eventName := event.Name // Capture event name for closure
		sub, err := e.nc.Subscribe(event.NatsSubject, func(msg *nats.Msg) {
			log.Printf("📨 Received NATS event on %s: %s", event.NatsSubject, string(msg.Data))
			e.handleEvent(eventName, msg.Data)
		})
		if err != nil {
			return fmt.Errorf("failed to subscribe to %s: %w", event.NatsSubject, err)
		}

		e.subs = append(e.subs, sub)
		log.Printf("✅ Successfully subscribed to %s", event.NatsSubject)
	}

	log.Printf("🎉 NATS subscription setup complete - %d subscriptions active", len(e.subs))
	return nil
}

// timerLoop handles periodic timer events
func (e *FSMEngine) timerLoop() {
	interval := time.Duration(e.config.Performance.TimerIntervalSeconds) * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	log.Printf("Timer loop started with %d second interval", e.config.Performance.TimerIntervalSeconds)

	for {
		select {
		case <-e.ctx.Done():
			return
		case <-ticker.C:
			e.handleEvent("timer_tick", nil)
		}
	}
}

// eventLoop processes events
func (e *FSMEngine) eventLoop() {
	sleepDuration := time.Duration(e.config.Performance.EventLoopSleepMs) * time.Millisecond
	log.Printf("Event loop started with %dms sleep interval", e.config.Performance.EventLoopSleepMs)

	for {
		select {
		case <-e.ctx.Done():
			return
		default:
			// Process any queued events
			eventsProcessed := e.processEventQueue()

			// Sleep longer if we're in idle state and processed no events
			if e.currentState == "idle" && eventsProcessed == 0 {
				time.Sleep(time.Duration(e.config.Performance.IdleSleepMs) * time.Millisecond)
			} else {
				time.Sleep(sleepDuration)
			}
		}
	}
}

// handleEvent processes an incoming event
func (e *FSMEngine) handleEvent(eventName string, data []byte) {
	// Parse canonical event if data provided
	var event CanonicalEvent
	if data != nil {
		if err := json.Unmarshal(data, &event); err != nil {
			log.Printf("Failed to parse event data: %v", err)
			return
		}
	} else {
		// Create synthetic event for timer
		event = CanonicalEvent{
			EventID:   fmt.Sprintf("evt_%d", time.Now().UnixNano()),
			Source:    fmt.Sprintf("fsm:%s", e.agentID),
			Type:      eventName,
			Timestamp: time.Now().Format(time.RFC3339),
			Context:   make(map[string]interface{}),
			Payload:   make(map[string]interface{}),
		}
	}

	// Publish user goal for new_input events
	if eventName == "new_input" && data != nil {
		e.publishUserGoal(event)
	}

	// Queue event for processing
	e.queueEvent(eventName, event)
}

// queueEvent adds an event to the processing queue
func (e *FSMEngine) queueEvent(eventName string, event CanonicalEvent) {
	eventData := map[string]interface{}{
		"name":  eventName,
		"event": event,
		"time":  time.Now().UnixNano(),
	}

	data, _ := json.Marshal(eventData)
	e.redis.LPush(e.ctx, e.getRedisKey("queue"), data)
}

// processEventQueue processes queued events
func (e *FSMEngine) processEventQueue() int {
	eventsProcessed := 0
	maxEvents := e.config.Performance.MaxEventsPerCycle

	for eventsProcessed < maxEvents {
		result, err := e.redis.BRPop(e.ctx, 1*time.Second, e.getRedisKey("queue")).Result()
		if err != nil {
			break // No events or timeout
		}

		var eventData map[string]interface{}
		if err := json.Unmarshal([]byte(result[1]), &eventData); err != nil {
			log.Printf("Failed to parse queued event: %v", err)
			continue
		}

		eventName := eventData["name"].(string)
		event := eventData["event"].(map[string]interface{})

		e.processEvent(eventName, event)
		eventsProcessed++

		// Sleep briefly after processing each event to prevent overwhelming
		if eventsProcessed < maxEvents {
			time.Sleep(time.Duration(e.config.Performance.PostProcessingSleepMs) * time.Millisecond)
		}
	}

	return eventsProcessed
}

// updatePerformanceMetrics updates Redis with current performance metrics
func (e *FSMEngine) updatePerformanceMetrics() {
	perfKey := fmt.Sprintf("fsm:%s:performance", e.agentID)

	// Increment events processed
	e.redis.HIncrBy(e.ctx, perfKey, "events_processed", 1)

	// Update last activity time
	e.redis.HSet(e.ctx, perfKey, "last_activity", time.Now().Format(time.RFC3339))

	// Set basic metrics (simplified for now)
	e.redis.HSet(e.ctx, perfKey, map[string]interface{}{
		"transitions_per_second":     1.0,
		"average_state_time_seconds": 2.0,
		"idle_time_percentage":       50.0,
		"error_rate":                 0.0,
	})
}

// processEvent processes a single event
func (e *FSMEngine) processEvent(eventName string, event map[string]interface{}) {
	log.Printf("🔄 Processing event: %s in state: %s", eventName, e.currentState)

	// Find current state configuration
	stateConfig := e.findStateConfig(e.currentState)
	if stateConfig == nil {
		log.Printf("Unknown state: %s", e.currentState)
		return
	}

	// Check for transition
	transition, exists := stateConfig.On[eventName]
	if !exists {
		log.Printf("❌ No transition found for event %s in state %s", eventName, e.currentState)
		return // No transition for this event
	}

	log.Printf("✅ Found transition: %s -> %s for event %s", e.currentState, transition.Next, eventName)

	// Check guard if specified
	if transition.Guard != "" {
		if !e.evaluateGuard(transition.Guard, event) {
			log.Printf("Guard %s failed for event %s", transition.Guard, eventName)
			return
		}
	}

	// Update performance metrics
	e.updatePerformanceMetrics()

	// Execute transition
	e.transitionTo(transition.Next, eventName, event)
}

// transitionTo performs a state transition
func (e *FSMEngine) transitionTo(newState string, reason string, event map[string]interface{}) {
	oldState := e.currentState
	e.currentState = newState
	e.stateEntryTime = time.Now() // Update state entry time

	// Get human-readable state description
	stateDesc := e.getStateDescription(newState)

	// Log activity
	e.logActivity(
		fmt.Sprintf("Moved from '%s' to '%s': %s", oldState, newState, stateDesc),
		"state_change",
		map[string]string{
			"state":   newState,
			"details": fmt.Sprintf("Reason: %s", reason),
		},
	)

	// Execute actions for new state
	stateConfig := e.findStateConfig(newState)
	if stateConfig != nil {
		for _, action := range stateConfig.Actions {
			e.executeAction(action, event)
		}
	}

	// Save state
	e.saveState()

	// Publish transition event
	e.publishTransition(oldState, newState, reason, event)

	log.Printf("FSM transition: %s -> %s (reason: %s)", oldState, newState, reason)

	// Sleep between state transitions to prevent resource overuse
	if e.config.Performance.StateTransitionDelay > 0 {
		delay := time.Duration(e.config.Performance.StateTransitionDelay * float64(time.Second))
		log.Printf("Sleeping %.1f seconds between state transitions", e.config.Performance.StateTransitionDelay)
		time.Sleep(delay)
	}
}

// getStateDescription returns a human-readable description of what happens in a state
func (e *FSMEngine) getStateDescription(state string) string {
	descriptions := map[string]string{
		"idle":            "Waiting for input or timer events",
		"perceive":        "Ingesting and validating new data using domain knowledge",
		"learn":           "Extracting facts and updating domain knowledge - GROWING KNOWLEDGE BASE",
		"summarize":       "Compressing episodes into structured facts",
		"hypothesize":     "Generating hypotheses using domain knowledge and constraints",
		"reason":          "Applying reasoning and inference to generate new beliefs",
		"reason_continue": "Continuing reasoning process and generating explanations",
		"plan":            "Creating hierarchical plans using domain-specific success rates",
		"decide":          "Choosing action using principles and domain constraints - CHECKING PRINCIPLES",
		"act":             "Executing planned action with domain-aware monitoring",
		"observe":         "Collecting outcomes and validating against domain expectations",
		"evaluate":        "Comparing outcomes to domain knowledge and updating beliefs - GROWING KNOWLEDGE BASE",
		"archive":         "Checkpointing episode and updating domain knowledge",
		"fail":            "Handling errors with domain-aware recovery",
		"paused":          "Manual pause state",
		"shutdown":        "Clean shutdown with knowledge base preservation",
	}
	if desc, ok := descriptions[state]; ok {
		return desc
	}
	return "Processing in " + state
}

// executeAction executes a single action
func (e *FSMEngine) executeAction(action ActionConfig, event map[string]interface{}) {
	log.Printf("Executing action: %s (module: %s)", action.Type, action.Module)

	// Log activity for important actions
	actionMessages := map[string]string{
		"generate_hypotheses":     "🧠 Generating hypotheses from facts and domain knowledge",
		"test_hypothesis":         "🧪 Testing hypothesis with tools to gather evidence",
		"grow_knowledge_base":     "🌱 Growing knowledge base: discovering concepts and filling gaps",
		"discover_new_concepts":   "🔍 Discovering new concepts from episodes",
		"update_domain_knowledge": "📚 Updating domain knowledge with new information",
		"plan_test_or_action":     "📋 Creating hierarchical plan using success rates",
		"check_principles":        "🛡️ Checking principles compliance before action",
		"execute_action":          "⚡ Executing action with tools",
	}
	if msg, ok := actionMessages[action.Type]; ok {
		e.logActivity(msg, "action", map[string]string{
			"action": action.Type,
			"state":  e.currentState,
		})
	}

	// Dispatch to appropriate module
	switch action.Module {
	case "hdn.input_parser":
		e.executeInputParser(action, event)
	case "knowledge.domain_classifier":
		e.executeDomainClassifier(action, event)
	case "self.knowledge_extractor":
		e.executeKnowledgeExtractor(action, event)
	case "memory.embedding":
		e.executeEmbedding(action, event)
	case "knowledge.updater":
		e.executeKnowledgeUpdater(action, event)
	case "self.summarizer":
		e.executeSummarizer(action, event)
	case "self.belief_store":
		e.executeBeliefStore(action, event)
	case "planner.hypothesis_generator":
		e.executeHypothesisGenerator(action, event)
	case "knowledge.constraint_checker":
		e.executeConstraintChecker(action, event)
	case "planner.hierarchical":
		e.executeHierarchicalPlanner(action, event)
	case "evaluator.plan_ranker":
		e.executePlanRanker(action, event)
	case "principles.mandatory_checker":
		e.executeMandatoryPrinciplesChecker(action, event)
	case "principles.checker":
		e.executePrinciplesChecker(action, event)
	case "principles.pre_execution_checker":
		e.executePreExecutionPrinciplesChecker(action, event)
	case "evaluator.utility_calculator":
		e.executeUtilityCalculator(action, event)
	case "knowledge.constraint_enforcer":
		e.executeConstraintEnforcer(action, event)
	case "hdn.retrieve_capabilities":
		e.executeRetrieveCapabilities(action, event)
	case "hdn.execute_capability":
		e.executeExecuteCapability(action, event)
	case "monitor.collector":
		e.executeMonitorCollector(action, event)
	case "knowledge.metrics_collector":
		e.executeMetricsCollector(action, event)
	case "evaluator.outcome_analyzer":
		e.executeOutcomeAnalyzer(action, event)
	case "knowledge.learning_updater":
		e.executeLearningUpdater(action, event)
	case "knowledge.concept_discovery":
		e.executeConceptDiscovery(action, event)
	case "knowledge.gap_analyzer":
		e.executeGapAnalyzer(action, event)
	case "knowledge.growth_engine":
		e.executeGrowthEngine(action, event)
	case "knowledge.consistency_checker":
		e.executeConsistencyChecker(action, event)
	case "projects.checkpoint":
		e.executeCheckpoint(action, event)
	case "memory.episodic_updater":
		e.executeEpisodicUpdater(action, event)
	case "monitor.logger":
		e.executeLogger(action, event)
	case "system.recovery":
		e.executeRecovery(action, event)
	case "system.cleanup":
		e.executeCleanup(action, event)
	case "reasoning.belief_query":
		e.executeBeliefQuery(action, event)
	case "reasoning.inference":
		e.executeInference(action, event)
	case "reasoning.curiosity_goals":
		e.executeCuriosityGoals(action, event)
	case "reasoning.explanation":
		e.executeExplanation(action, event)
	case "reasoning.trace_logger":
		e.executeTraceLogger(action, event)
	case "reasoning.news_storage":
		e.executeNewsStorage(action, event)
	case "reasoning.hypothesis_testing":
		e.executeHypothesisTesting(action, event)
	default:
		log.Printf("Unknown action module: %s", action.Module)
	}
}

// Action execution methods (simplified implementations)
func (e *FSMEngine) executeInputParser(action ActionConfig, event map[string]interface{}) {
	// Call HDN input parser API
	log.Printf("Parsing input with domain validation")

	// Heuristic: auto-select capability from user message
	if payload, ok := event["payload"].(map[string]interface{}); ok {
		if text, ok := payload["text"].(string); ok {
			// PrimeNumberGenerator
			if text != "" && (containsIgnoreCase(text, "PrimeNumberGenerator") || containsIgnoreCase(text, "first 10 primes") || containsIgnoreCase(text, "primes")) {
				sel := map[string]interface{}{"id": "PrimeNumberGenerator", "name": "PrimeNumberGenerator"}
				e.context["selected_capability"] = sel
				e.context["current_action"] = "PrimeNumberGenerator"
				// default inputs
				if e.context["capability_inputs"] == nil {
					e.context["capability_inputs"] = map[string]interface{}{"count": "10"}
				}
				log.Printf("Auto-selected capability: PrimeNumberGenerator with count=10")
			}
		}
	}

	// For testing: emit ingest_ok event after a short delay
	go func() {
		time.Sleep(500 * time.Millisecond)
		log.Printf("📤 Emitting ingest_ok event")
		e.handleEvent("ingest_ok", nil)
	}()
}

func (e *FSMEngine) executeDomainClassifier(action ActionConfig, event map[string]interface{}) {
	// Use knowledge base to classify domain
	log.Printf("Classifying domain using concept matching")
	
	// Extract text to classify from event
	var textToClassify string
	
	// For news events, extract headline or relation text
	if eventType, ok := event["type"].(string); ok {
		if eventType == "relations" || eventType == "alerts" {
			if payload, ok := event["payload"].(map[string]interface{}); ok {
				if metadata, ok := payload["metadata"].(map[string]interface{}); ok {
					// Try headline first
					if headline, ok := metadata["headline"].(string); ok && headline != "" {
						textToClassify = headline
					} else if text, ok := payload["text"].(string); ok && text != "" {
						textToClassify = text
					} else {
						// Build text from relation components
						if head, ok := metadata["head"].(string); ok {
							textToClassify = head
							if rel, ok := metadata["relation"].(string); ok {
								textToClassify += " " + rel
							}
							if tail, ok := metadata["tail"].(string); ok {
								textToClassify += " " + tail
							}
						}
					}
				} else if text, ok := payload["text"].(string); ok && text != "" {
					textToClassify = text
				}
			}
		} else {
			// For other events, try to get text from payload
			if payload, ok := event["payload"].(map[string]interface{}); ok {
				if text, ok := payload["text"].(string); ok && text != "" {
					textToClassify = text
				}
			}
		}
	}
	
	// If we have text to classify, use knowledge integration
	if textToClassify != "" && e.knowledgeIntegration != nil {
		result, err := e.knowledgeIntegration.ClassifyDomain(textToClassify)
		if err == nil && result != nil && result.Domain != "" {
			// Set the domain in context
			e.context["current_domain"] = result.Domain
			log.Printf("✅ Classified domain: %s (confidence: %.2f)", result.Domain, result.Confidence)
			
			// Save context to persist domain
			e.saveState()
		} else if err != nil {
			log.Printf("⚠️ Domain classification failed: %v", err)
		}
	} else {
		log.Printf("⚠️ No text available for domain classification")
	}
}

func (e *FSMEngine) executeKnowledgeExtractor(action ActionConfig, event map[string]interface{}) {
	// Extract facts using domain constraints
	log.Printf("Extracting facts with domain constraints")

	// Extract facts from event data and context
	var facts []map[string]interface{}

	// Extract from event data
	if eventData, ok := event["data"].(string); ok && eventData != "" {
		facts = append(facts, map[string]interface{}{
			"fact":       eventData,
			"confidence": 0.8,
			"domain":     e.getCurrentDomain(),
		})
	}

	// Extract from context
	if input, ok := e.context["input"].(string); ok && input != "" {
		facts = append(facts, map[string]interface{}{
			"fact":       fmt.Sprintf("User input: %s", input),
			"confidence": 0.9,
			"domain":     e.getCurrentDomain(),
		})
	}

	// Extract system state facts
	if e.currentState != "" {
		facts = append(facts, map[string]interface{}{
			"fact":       fmt.Sprintf("System state: %s", e.currentState),
			"confidence": 0.95,
			"domain":     "system",
		})
	}

	// If no facts extracted, add a basic fact
	if len(facts) == 0 {
		facts = append(facts, map[string]interface{}{
			"fact":       "FSM is processing events",
			"confidence": 0.7,
			"domain":     e.getCurrentDomain(),
		})
	}

	// Deduplicate by fact string
	seen := map[string]bool{}
	if existing, ok := e.context["extracted_facts"].([]interface{}); ok {
		for _, it := range existing {
			if m, ok := it.(map[string]interface{}); ok {
				if s, ok := m["fact"].(string); ok {
					seen[s] = true
				}
			}
		}
	}

	// Build deduped list to append
	var asInterfaces []interface{}
	for _, f := range facts {
		if s, ok := f["fact"].(string); ok {
			if seen[s] {
				continue
			}
			seen[s] = true
		}
		asInterfaces = append(asInterfaces, f)
	}

	// Store facts in context using []interface{}
	if e.context["extracted_facts"] == nil {
		e.context["extracted_facts"] = []interface{}{}
	}
	e.context["extracted_facts"] = append(e.context["extracted_facts"].([]interface{}), asInterfaces...)

	// Publish perception facts to Goals server
	e.publishPerceptionFacts(asInterfaces)

	// Emit facts_extracted event
	go func() {
		time.Sleep(300 * time.Millisecond)
		log.Printf("📤 Emitting facts_extracted event")
		e.handleEvent("facts_extracted", nil)
	}()
}

func (e *FSMEngine) executeEmbedding(action ActionConfig, event map[string]interface{}) {
	// Generate embeddings and store in Weaviate
	log.Printf("Generating embeddings for Weaviate storage")

	// Get episodes from context
	episodes := e.getEpisodesFromContext()
	if len(episodes) == 0 {
		log.Printf("No episodes to embed")
		return
	}

	// Create Weaviate client
	weaviateURL := os.Getenv("WEAVIATE_URL")
	if weaviateURL == "" {
		weaviateURL = "http://localhost:8080"
	}
	client := mempkg.NewVectorDBAdapter(weaviateURL, "agi-episodes")

	// Ensure collection exists with 8 dimensions (matching existing setup)
	if err := client.EnsureCollection(8); err != nil {
		log.Printf("Warning: Failed to ensure vector database collection: %v", err)
		return
	}

	// Process each episode
	for _, episode := range episodes {
		// Extract text for embedding
		text := ""
		if textVal, ok := episode["text"].(string); ok {
			text = textVal
		} else if descVal, ok := episode["description"].(string); ok {
			text = descVal
		} else {
			// Fallback: convert episode to JSON string
			if jsonBytes, err := json.Marshal(episode); err == nil {
				text = string(jsonBytes)
			}
		}

		if text == "" {
			continue
		}

		// Create episodic record
		record := &mempkg.EpisodicRecord{
			SessionID: e.agentID,
			Timestamp: time.Now().UTC(),
			Outcome:   "success",
			Tags:      []string{"fsm", "episode"},
			Text:      text,
			Metadata:  episode,
		}

		// Generate simple embedding (8 dimensions)
		// In production, this would use a real embedding model
		embedding := e.generateSimpleEmbedding(text, 8)

		// Store in vector database
		if err := client.IndexEpisode(record, embedding); err != nil {
			log.Printf("Warning: Failed to index episode in vector database: %v", err)
		} else {
			log.Printf("✅ Indexed episode in vector database: %s", text[:min(50, len(text))])
		}
	}
}

// generateSimpleEmbedding creates a simple hash-based embedding
func (e *FSMEngine) generateSimpleEmbedding(text string, dim int) []float32 {
	// Simple hash-based embedding for demonstration
	// In production, use a real embedding model
	hash := 0
	for _, c := range text {
		hash = hash*31 + int(c)
	}

	embedding := make([]float32, dim)
	for i := 0; i < dim; i++ {
		// Generate pseudo-random values based on hash and position
		val := float32((hash+i*17)%1000) / 1000.0
		embedding[i] = val - 0.5 // Center around 0
	}

	return embedding
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (e *FSMEngine) executeKnowledgeUpdater(action ActionConfig, event map[string]interface{}) {
	// Update domain knowledge in Neo4j
	log.Printf("Updating domain knowledge in Neo4j")

	// Simulate knowledge update
	concepts := []map[string]interface{}{
		{"concept": "FSM State Machine", "confidence": 0.9, "examples": 3},
		{"concept": "NATS Event Bus", "confidence": 0.85, "examples": 2},
		{"concept": "Redis State Storage", "confidence": 0.8, "examples": 1},
	}

	// Store concepts in context using []interface{} for consistency
	if e.context["discovered_concepts"] == nil {
		e.context["discovered_concepts"] = []interface{}{}
	}
	var conceptInterfaces []interface{}
	for _, c := range concepts {
		conceptInterfaces = append(conceptInterfaces, c)
	}
	e.context["discovered_concepts"] = append(e.context["discovered_concepts"].([]interface{}), conceptInterfaces...)

	// Update knowledge growth metrics
	if e.context["knowledge_growth"] == nil {
		e.context["knowledge_growth"] = map[string]interface{}{
			"concepts_created":    0,
			"relationships_added": 0,
			"examples_added":      0,
		}
	}
	growth := e.context["knowledge_growth"].(map[string]interface{})

	// Safe type conversion for numeric values
	getInt := func(val interface{}) int {
		switch v := val.(type) {
		case int:
			return v
		case float64:
			return int(v)
		case int64:
			return int(v)
		default:
			return 0
		}
	}

	growth["concepts_created"] = getInt(growth["concepts_created"]) + len(concepts)
	growth["examples_added"] = getInt(growth["examples_added"]) + 6

	// Update Redis metrics for monitoring
	knowledgeKey := fmt.Sprintf("fsm:%s:knowledge_growth", e.agentID)
	e.redis.HSet(e.ctx, knowledgeKey, map[string]interface{}{
		"concepts_created":    growth["concepts_created"],
		"examples_added":      growth["examples_added"],
		"relationships_added": growth["relationships_added"],
		"last_growth_time":    time.Now().Format(time.RFC3339),
	})
}

func (e *FSMEngine) executeSummarizer(action ActionConfig, event map[string]interface{}) {
	// Summarize episodes with domain context
	log.Printf("Summarizing episodes with domain context")

	// Advance: signal that facts are ready for hypothesis generation
	go func() {
		time.Sleep(200 * time.Millisecond)
		e.handleEvent("facts_ready", nil)
	}()
}

func (e *FSMEngine) executeBeliefStore(action ActionConfig, event map[string]interface{}) {
	// Update beliefs in Redis
	log.Printf("Updating beliefs in Redis")
}

func (e *FSMEngine) executeHypothesisGenerator(action ActionConfig, event map[string]interface{}) {
	// Generate hypotheses using domain relations
	log.Printf("🧠 Generating hypotheses using domain relations")

	// Get facts from context
	facts := e.getFactsFromContext()
	domain := e.getCurrentDomain()

	// Generate hypotheses using knowledge integration
	hypotheses, err := e.knowledgeIntegration.GenerateHypotheses(facts, domain)
	if err != nil {
		log.Printf("❌ Hypothesis generation failed: %v", err)
		e.context["hypothesis_generation_error"] = err.Error()
		return
	}

	// Store hypotheses in Redis and context
	if len(hypotheses) > 0 {
		log.Printf("🧠 Generated %d hypotheses", len(hypotheses))
		e.context["generated_hypotheses"] = hypotheses
		e.context["hypothesis_count"] = len(hypotheses)

		// Store in Redis for monitoring
		e.storeHypotheses(hypotheses, domain)

		// Create curiosity goals for testing hypotheses
		e.createHypothesisTestingGoals(hypotheses, domain)

		// Log activity
		e.logActivity(
			fmt.Sprintf("Generated %d hypotheses in domain '%s'", len(hypotheses), domain),
			"hypothesis",
			map[string]string{
				"details": fmt.Sprintf("Domain: %s, Count: %d", domain, len(hypotheses)),
			},
		)
	} else {
		log.Printf("ℹ️ No hypotheses generated")
		e.context["no_hypotheses_generated"] = true
	}

	// Advance: emit hypotheses_generated
	go func() {
		time.Sleep(200 * time.Millisecond)
		e.handleEvent("hypotheses_generated", nil)
	}()
}

func (e *FSMEngine) executeConstraintChecker(action ActionConfig, event map[string]interface{}) {
	// Check constraints against domain knowledge
	log.Printf("Checking constraints against domain knowledge")
}

func (e *FSMEngine) executeHierarchicalPlanner(action ActionConfig, event map[string]interface{}) {
	// Create plans using domain success rates
	log.Printf("Creating hierarchical plans using domain success rates")
}

func (e *FSMEngine) executePlanRanker(action ActionConfig, event map[string]interface{}) {
	// Rank plans using domain knowledge weights
	log.Printf("Ranking plans using domain knowledge weights")

	// Advance: emit plans_ranked
	go func() {
		time.Sleep(200 * time.Millisecond)
		e.handleEvent("plans_ranked", nil)
	}()
}

func (e *FSMEngine) executeMandatoryPrinciplesChecker(action ActionConfig, event map[string]interface{}) {
	// HARDCODED: Always check principles before any decision
	log.Printf("🔒 MANDATORY PRINCIPLES CHECK - Hardcoded requirement")

	// Extract action description from event or context
	actionDesc := "Unknown action"
	if desc, ok := event["action"].(string); ok {
		actionDesc = desc
	} else if desc, ok := e.context["current_action"].(string); ok {
		actionDesc = desc
	}

	// Skip principles if delegating to HDN (HDN performs its own LLM+principles checks)
	if hdn, ok := e.context["hdn_delegate"].(bool); ok && hdn {
		log.Printf("✅ MANDATORY PRINCIPLES CHECK SKIPPED - Delegated to HDN")
		e.context["principles_allowed"] = true
		e.context["principles_reason"] = "Delegated to HDN checks"
		e.context["principles_confidence"] = 0.99
		// Count as a successful, near-zero-time check so the monitor reflects activity
		e.recordPrinciplesStats(true, 1*time.Millisecond, false)
		return
	}

	// Allowlist: fast-path safe capabilities
	if ca, ok := e.context["current_action"].(string); ok {
		switch strings.ToLower(ca) {
		case "primenumbergenerator", "matrixcalculator", "statisticalanalyzer":
			log.Printf("✅ MANDATORY PRINCIPLES CHECK BYPASS - Safe capability: %s", ca)
			e.context["principles_allowed"] = true
			e.context["principles_reason"] = "Allowlisted safe capability"
			e.context["principles_confidence"] = 0.99
			// Count as allowed with negligible duration
			e.recordPrinciplesStats(true, 1*time.Millisecond, false)
			return
		}
	}

	// This is a CRITICAL safety check that must always succeed
	start := time.Now()
	response, err := e.principles.MandatoryPrinciplesCheck(actionDesc, e.context)
	if err != nil {
		log.Printf("❌ MANDATORY PRINCIPLES CHECK FAILED - %v", err)
		// Store the failure in context for transition handling
		e.context["principles_error"] = err.Error()
		e.recordPrinciplesStats(false, time.Since(start), true)
		return
	}

	if !response.Allowed {
		log.Printf("❌ MANDATORY PRINCIPLES CHECK FAILED - Action blocked: %s", response.Reason)
		e.context["principles_blocked"] = response.Reason
		e.context["blocked_rules"] = response.BlockedRules
		e.recordPrinciplesStats(false, time.Since(start), false)
		return
	}

	log.Printf("✅ MANDATORY PRINCIPLES CHECK PASSED - Action allowed: %s", response.Reason)
	e.context["principles_allowed"] = true
	e.context["principles_reason"] = response.Reason
	e.context["principles_confidence"] = response.Confidence
	e.recordPrinciplesStats(true, time.Since(start), false)
}

func (e *FSMEngine) executePrinciplesChecker(action ActionConfig, event map[string]interface{}) {
	// Check principles with domain safety
	log.Printf("Checking principles with domain safety")

	// Advance: emit allowed (simplified; real logic should branch)
	go func() {
		time.Sleep(150 * time.Millisecond)
		e.handleEvent("allowed", nil)
	}()
}

func (e *FSMEngine) executePreExecutionPrinciplesChecker(action ActionConfig, event map[string]interface{}) {
	// HARDCODED: Double-check principles before execution
	log.Printf("🔒 PRE-EXECUTION PRINCIPLES CHECK - Double-checking safety before action")

	// Extract action description from event or context
	actionDesc := "Unknown action"
	if desc, ok := event["action"].(string); ok {
		actionDesc = desc
	} else if desc, ok := e.context["current_action"].(string); ok {
		actionDesc = desc
	}

	// Skip principles if delegating to HDN (HDN performs its own LLM+principles checks)
	if hdn, ok := e.context["hdn_delegate"].(bool); ok && hdn {
		log.Printf("✅ PRE-EXECUTION PRINCIPLES CHECK SKIPPED - Delegated to HDN")
		e.context["pre_execution_allowed"] = true
		e.context["pre_execution_reason"] = "Delegated to HDN checks"
		e.context["pre_execution_confidence"] = 0.99
		e.recordPrinciplesStats(true, 1*time.Millisecond, false)
		go func() { time.Sleep(100 * time.Millisecond); e.handleEvent("allowed", nil) }()
		return
	}

	// Allowlist: fast-path safe capabilities
	if ca, ok := e.context["current_action"].(string); ok {
		switch strings.ToLower(ca) {
		case "primenumbergenerator", "matrixcalculator", "statisticalanalyzer":
			log.Printf("✅ PRE-EXECUTION PRINCIPLES CHECK BYPASS - Safe capability: %s", ca)
			e.context["pre_execution_allowed"] = true
			e.context["pre_execution_reason"] = "Allowlisted safe capability"
			e.context["pre_execution_confidence"] = 0.99
			e.recordPrinciplesStats(true, 1*time.Millisecond, false)
			// Advance: allowed before execution
			go func() {
				time.Sleep(100 * time.Millisecond)
				e.handleEvent("allowed", nil)
			}()
			return
		}
	}

	// This is a second safety check right before execution
	// Even if the first check passed, we check again for maximum safety
	start := time.Now()
	response, err := e.principles.PreExecutionPrinciplesCheck(actionDesc, e.context)
	if err != nil {
		log.Printf("❌ PRE-EXECUTION PRINCIPLES CHECK FAILED - %v", err)
		e.context["pre_execution_error"] = err.Error()
		e.recordPrinciplesStats(false, time.Since(start), true)
		return
	}

	if !response.Allowed {
		log.Printf("❌ PRE-EXECUTION PRINCIPLES CHECK FAILED - Action blocked: %s", response.Reason)
		e.context["pre_execution_blocked"] = response.Reason
		e.context["pre_execution_blocked_rules"] = response.BlockedRules
		e.recordPrinciplesStats(false, time.Since(start), false)
		return
	}

	log.Printf("✅ PRE-EXECUTION PRINCIPLES CHECK PASSED - Action allowed: %s", response.Reason)
	e.context["pre_execution_allowed"] = true
	e.context["pre_execution_reason"] = response.Reason
	e.context["pre_execution_confidence"] = response.Confidence
	e.recordPrinciplesStats(true, time.Since(start), false)

	// Advance: allowed before execution
	go func() {
		time.Sleep(100 * time.Millisecond)
		e.handleEvent("allowed", nil)
	}()
}

// recordPrinciplesStats updates Redis-backed metrics used by the Monitor FSM "Principles Checks" panel.
func (e *FSMEngine) recordPrinciplesStats(allowed bool, duration time.Duration, errOccurred bool) {
	// Best-effort; avoid blocking core flow on metrics
	defer func() { recover() }()

	key := fmt.Sprintf("fsm:%s:principles", e.agentID)

	// Total checks
	_ = e.redis.HIncrBy(e.ctx, key, "total_checks", 1).Err()

	if allowed {
		_ = e.redis.HIncrBy(e.ctx, key, "allowed_actions", 1).Err()
	} else {
		_ = e.redis.HIncrBy(e.ctx, key, "blocked_actions", 1).Err()
	}

	if errOccurred {
		_ = e.redis.HIncrBy(e.ctx, key, "error_count", 1).Err()
	}

	// Track response time and rolling average
	ms := float64(duration.Milliseconds())
	if ms < 0 {
		ms = 0
	}
	_ = e.redis.HIncrByFloat(e.ctx, key, "total_response_time_ms", ms).Err()

	// Compute average = total_response_time_ms / total_checks
	totals, err1 := e.redis.HMGet(e.ctx, key, "total_response_time_ms", "total_checks").Result()
	if err1 == nil && len(totals) == 2 && totals[0] != nil && totals[1] != nil {
		var totalMs float64
		var checks int64
		if s, ok := totals[0].(string); ok {
			fmt.Sscanf(s, "%f", &totalMs)
		}
		switch v := totals[1].(type) {
		case string:
			fmt.Sscanf(v, "%d", &checks)
		case int64:
			checks = v
		}
		if checks > 0 {
			avg := totalMs / float64(checks)
			_ = e.redis.HSet(e.ctx, key, "average_response_time_ms", avg).Err()
		}
	}
}

func (e *FSMEngine) executeUtilityCalculator(action ActionConfig, event map[string]interface{}) {
	// Calculate utility including domain confidence
	log.Printf("Calculating utility including domain confidence")
}

func (e *FSMEngine) executeConstraintEnforcer(action ActionConfig, event map[string]interface{}) {
	// Apply domain constraints
	log.Printf("Applying domain constraints")
}

// HDN capability integration
func (e *FSMEngine) executeRetrieveCapabilities(action ActionConfig, event map[string]interface{}) {
	// Make non-blocking; mark inflight so UI can show progress
	e.context["hdn_inflight"] = true
	e.context["hdn_started_at"] = time.Now().Format(time.RFC3339)

	go func() {
		defer func() { e.context["hdn_inflight"] = false }()

		base := os.Getenv("HDN_URL")
		if base == "" {
			base = "http://localhost:8080"
		}
		url := fmt.Sprintf("%s/api/v1/intelligent/capabilities", base)
		log.Printf("HDN: GET %s", url)

		req, _ := http.NewRequest("GET", url, nil)
		if pid, ok := e.context["project_id"].(string); ok && pid != "" {
			req.Header.Set("X-Project-ID", pid)
		}
		// Simple retry with backoff (1s, 2s, 4s)
		var resp *http.Response
		var err error
		for attempt := 1; attempt <= 3; attempt++ {
			resp, err = (&http.Client{Timeout: 30 * time.Second}).Do(req)
			if err == nil {
				break
			}
			backoff := time.Duration(1<<uint(attempt-1)) * time.Second
			log.Printf("⚠️ HDN capabilities fetch attempt %d failed: %v (retrying in %s)", attempt, err, backoff)
			time.Sleep(backoff)
		}
		if err != nil {
			log.Printf("❌ HDN capabilities fetch failed after retries: %v", err)
			e.context["hdn_last_error"] = err.Error()
			return
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			log.Printf("❌ HDN capabilities fetch status %d: %s", resp.StatusCode, string(body))
			e.context["hdn_last_error"] = fmt.Sprintf("status %d", resp.StatusCode)
			return
		}

		// Tolerant parse: accept either array or object containing an array (prefer "capabilities" key)
		var caps []map[string]interface{}
		if err := json.Unmarshal(body, &caps); err != nil {
			var obj map[string]interface{}
			if err2 := json.Unmarshal(body, &obj); err2 != nil {
				log.Printf("❌ HDN capabilities parse error: %v (body len=%d)", err, len(body))
				e.context["hdn_last_error"] = err.Error()
				return
			}
			// Prefer explicit "capabilities" key if present
			if raw, ok := obj["capabilities"]; ok {
				if list, ok := raw.([]interface{}); ok {
					for _, it := range list {
						if m, ok2 := it.(map[string]interface{}); ok2 {
							caps = append(caps, m)
						}
					}
				}
			}
			// Fallback: use the first array-valued field
			if len(caps) == 0 {
				for _, v := range obj {
					if list, ok := v.([]interface{}); ok {
						for _, it := range list {
							if m, ok2 := it.(map[string]interface{}); ok2 {
								caps = append(caps, m)
							}
						}
						break
					}
				}
			}
		}
		if len(caps) == 0 {
			log.Printf("ℹ️ HDN capabilities response contained no items")
		}
		arr := make([]interface{}, 0, len(caps))
		for _, c := range caps {
			arr = append(arr, c)
		}
		e.context["candidate_capabilities"] = arr
		if len(caps) > 0 {
			e.context["selected_capability"] = caps[0]
			if name, ok := caps[0]["name"].(string); ok && name != "" {
				e.context["current_action"] = name
			} else if id, ok := caps[0]["id"].(string); ok && id != "" {
				e.context["current_action"] = id
			}
		}
		e.handleEvent("capabilities_retrieved", nil)
	}()
}

func (e *FSMEngine) executeExecuteCapability(action ActionConfig, event map[string]interface{}) {
	// Delegate to HDN: execute selected capability with current payload
	log.Printf("HDN: executing selected domain capability")

	// Select capability
	var selected map[string]interface{}
	if sel, ok := e.context["selected_capability"].(map[string]interface{}); ok {
		selected = sel
	} else if list, ok := e.context["candidate_capabilities"].([]interface{}); ok && len(list) > 0 {
		if m, ok2 := list[0].(map[string]interface{}); ok2 {
			selected = m
			e.context["selected_capability"] = selected
		}
	}
	if selected == nil {
		log.Printf("❌ No capability selected")
		return
	}

	// Build request
	base := os.Getenv("HDN_URL")
	if base == "" {
		base = "http://localhost:8080"
	}
	url := fmt.Sprintf("%s/api/v1/interpret/execute", base)

	// Convert context to string map for interpret endpoint
	ctx := map[string]string{"origin": "fsm"}
	if pid, ok := e.context["project_id"].(string); ok && pid != "" {
		ctx["project_id"] = pid
	}
	// Indicate we are delegating safety to HDN
	e.context["hdn_delegate"] = true

	// Build natural language input for interpret endpoint
	desc := "Execute a task"
	if v, ok := selected["description"].(string); ok && v != "" {
		desc = v
	} else if v, ok := selected["name"].(string); ok && v != "" {
		desc = fmt.Sprintf("Execute %s", v)
	}

	payload := map[string]interface{}{
		"input":      desc,
		"context":    ctx,
		"session_id": fmt.Sprintf("fsm_%s_%d", e.agentID, time.Now().UnixNano()),
	}
	if inp := e.context["capability_inputs"]; inp != nil {
		// place inputs inside context for HDN
		if c, ok := payload["context"].(map[string]string); ok {
			// Convert inputs to string for context
			if inputsStr, ok := inp.(string); ok {
				c["inputs"] = inputsStr
			}
		}
	}
	data, _ := json.Marshal(payload)
	log.Printf("HDN: POST %s (input=%s)", url, desc)

	go func() {
		// Emit tool.invoked (best-effort)
		e.recordToolUsage("invoked", desc, map[string]interface{}{"selected": selected})
		req, _ := http.NewRequest("POST", url, bytes.NewReader(data))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		if pid, ok := e.context["project_id"].(string); ok && pid != "" {
			req.Header.Set("X-Project-ID", pid)
		}
		// Retry with backoff on transient failures
		var resp *http.Response
		var err error
		for attempt := 1; attempt <= 3; attempt++ {
			resp, err = (&http.Client{Timeout: 300 * time.Second}).Do(req)
			if err == nil {
				break
			}
			backoff := time.Duration(1<<uint(attempt-1)) * time.Second // 1s,2s,4s
			log.Printf("⚠️ HDN execute attempt %d failed: %v (retrying in %s)", attempt, err, backoff)
			time.Sleep(backoff)
		}
		if err != nil {
			log.Printf("❌ HDN execute failed after retries: %v", err)
			e.context["last_execution_error"] = err.Error()
			e.recordToolUsage("failed", desc, map[string]interface{}{"error": err.Error()})
			go e.persistExecutionEpisode(map[string]interface{}{"error": err.Error()}, false)
			e.handleEvent("execution_failed", nil)
			return
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			log.Printf("❌ HDN execute status %d: %s", resp.StatusCode, string(body))
			e.context["last_execution_status"] = resp.StatusCode
			e.context["last_execution_body"] = string(body)
			e.recordToolUsage("failed", desc, map[string]interface{}{"status": resp.StatusCode, "body": string(body)})
			go e.persistExecutionEpisode(map[string]interface{}{"status": resp.StatusCode, "body": string(body)}, false)
			e.handleEvent("execution_failed", nil)
			return
		}
		var out map[string]interface{}
		if err := json.Unmarshal(body, &out); err != nil {
			log.Printf("❌ HDN execute parse error: %v", err)
			e.context["last_execution_body"] = string(body)
			e.recordToolUsage("failed", desc, map[string]interface{}{"parse_error": err.Error()})
			go e.persistExecutionEpisode(map[string]interface{}{"body": string(body), "parse_error": err.Error()}, false)
			e.handleEvent("execution_failed", nil)
			return
		}
		// If HDN returns success=false or an error field, treat as failure
		if s, ok := out["success"].(bool); ok && !s {
			errMsg := "execution reported success=false"
			if em, ok := out["error"].(string); ok && em != "" {
				errMsg = em
			}
			log.Printf("❌ HDN execute reported failure: %s", errMsg)
			e.context["last_execution_error"] = errMsg
			e.recordToolUsage("failed", desc, map[string]interface{}{"error": errMsg})
			go e.persistExecutionEpisode(out, false)
			e.handleEvent("execution_failed", nil)
			return
		}
		// Normalize common result fields for UI/episodes
		if _, ok := out["result"]; !ok {
			if v, ok := out["output"]; ok {
				out["result"] = v
			} else if v, ok := out["data"]; ok {
				out["result"] = v
			}
		}
		// Store last execution and persist as episode
		e.context["last_execution"] = out

		// Capture workflow_id if returned by HDN
		if workflowID, ok := out["workflow_id"].(string); ok && workflowID != "" {
			e.context["current_workflow_id"] = workflowID
			log.Printf("📝 Captured workflow_id: %s", workflowID)
		}

		e.recordToolUsage("result", desc, map[string]interface{}{"result": out})
		go e.persistExecutionEpisode(out, true)
		e.handleEvent("execution_finished", nil)
	}()
}

// recordToolUsage appends a usage record in Redis and optionally publishes NATS (handled by other services)
func (e *FSMEngine) recordToolUsage(kind string, tool string, extra map[string]interface{}) {
	defer func() { recover() }()
	rec := map[string]interface{}{
		"ts":       time.Now().Format(time.RFC3339),
		"agent_id": e.agentID,
		"type":     kind,
		"tool":     tool,
	}
	for k, v := range extra {
		rec[k] = v
	}
	b, _ := json.Marshal(rec)
	keys := []string{
		fmt.Sprintf("tools:%s:usage_history", e.agentID),
		"tools:global:usage_history",
	}
	for _, k := range keys {
		_ = e.redis.LPush(e.ctx, k, b).Err()
		_ = e.redis.LTrim(e.ctx, k, 0, 199).Err()
	}
}

// persistExecutionEpisode stores an execution result as an episode in Redis and updates knowledge growth snapshots.
func (e *FSMEngine) persistExecutionEpisode(result map[string]interface{}, success bool) {
	defer func() { recover() }()

	// Build episode
	ep := map[string]interface{}{
		"id":        fmt.Sprintf("ep_%d", time.Now().UnixNano()),
		"timestamp": time.Now().Format(time.RFC3339),
		"outcome":   map[bool]string{true: "success", false: "failed"}[success],
	}
	if pid, ok := e.context["project_id"].(string); ok && pid != "" {
		ep["project_id"] = pid
	}
	if sel, ok := e.context["selected_capability"].(map[string]interface{}); ok {
		if n, ok := sel["name"].(string); ok && n != "" {
			ep["summary"] = fmt.Sprintf("Executed %s", n)
		} else if id, ok2 := sel["id"].(string); ok2 && id != "" {
			ep["summary"] = fmt.Sprintf("Executed %s", id)
		}
	}
	// Attach result compactly
	ep["result"] = result

	// Push to both legacy and new episode keys so UI panels populate
	keys := []string{
		fmt.Sprintf("fsm:%s:episodes", e.agentID),
		e.getRedisKey("episodes"),
	}
	data, _ := json.Marshal(ep)
	for _, k := range keys {
		if k == "" {
			continue
		}
		_ = e.redis.LPush(e.ctx, k, data).Err()
		_ = e.redis.LTrim(e.ctx, k, 0, 99).Err()
	}

	// Update last activity timestamp for uptime/metrics
	_ = e.redis.Set(e.ctx, fmt.Sprintf("fsm:%s:last_activity", e.agentID), time.Now().Format(time.RFC3339), 0).Err()

	// Snapshot minimal knowledge growth timeline so charts have data points
	kgKey := fmt.Sprintf("fsm:%s:knowledge_growth_timeline", e.agentID)
	kg := KnowledgeGrowthStats{LastGrowthTime: time.Now()}
	if b, err := json.Marshal(kg); err == nil {
		_ = e.redis.LPush(e.ctx, kgKey, b).Err()
		_ = e.redis.LTrim(e.ctx, kgKey, 0, 199).Err()
	}
}

func (e *FSMEngine) executeMonitorCollector(action ActionConfig, event map[string]interface{}) {
	// Collect outcomes with domain validation
	log.Printf("Collecting outcomes with domain validation")

	// Advance: results collected
	go func() {
		time.Sleep(200 * time.Millisecond)
		e.handleEvent("results_collected", nil)
	}()
}

func (e *FSMEngine) executeMetricsCollector(action ActionConfig, event map[string]interface{}) {
	// Measure domain-specific metrics
	log.Printf("Measuring domain-specific metrics")
}

func (e *FSMEngine) executeOutcomeAnalyzer(action ActionConfig, event map[string]interface{}) {
	// Analyze outcomes against domain expectations
	log.Printf("Analyzing outcomes against domain expectations")

	// Publish evaluation results to Goals server
	e.publishEvaluationResults()

	// Advance: analysis ok
	go func() {
		time.Sleep(200 * time.Millisecond)
		e.handleEvent("analysis_ok", nil)
	}()
}

func (e *FSMEngine) executeLearningUpdater(action ActionConfig, event map[string]interface{}) {
	// Update domain knowledge based on learning
	log.Printf("Updating domain knowledge based on learning")

	// Advance: knowledge updated (could loop back to summarize/idle)
	go func() {
		time.Sleep(200 * time.Millisecond)
		e.handleEvent("growth_updated", nil)
	}()
}

func (e *FSMEngine) executeConceptDiscovery(action ActionConfig, event map[string]interface{}) {
	// Discover new concepts from episodes
	log.Printf("🔍 Discovering new concepts from episodes")

	// Get episodes from context or event
	episodes := e.getEpisodesFromContext()
	domain := e.getCurrentDomain()

	discoveries, err := e.knowledgeGrowth.DiscoverNewConcepts(episodes, domain)
	if err != nil {
		log.Printf("❌ Concept discovery failed: %v", err)
		return
	}

	if len(discoveries) > 0 {
		log.Printf("📚 Discovered %d new concepts", len(discoveries))
		e.context["new_concepts_discovered"] = true
		e.context["discoveries"] = discoveries
	} else {
		log.Printf("ℹ️ No new concepts discovered")
	}
}

func (e *FSMEngine) executeGapAnalyzer(action ActionConfig, event map[string]interface{}) {
	// Find knowledge gaps
	log.Printf("🔍 Analyzing knowledge gaps")

	domain := e.getCurrentDomain()
	gaps, err := e.knowledgeGrowth.FindKnowledgeGaps(domain)
	if err != nil {
		log.Printf("❌ Gap analysis failed: %v", err)
		return
	}

	if len(gaps) > 0 {
		log.Printf("🕳️ Found %d knowledge gaps", len(gaps))
		e.context["knowledge_gaps"] = gaps
		e.context["gaps_found"] = true
	} else {
		log.Printf("✅ No knowledge gaps found")
	}
}

func (e *FSMEngine) executeGrowthEngine(action ActionConfig, event map[string]interface{}) {
	// Grow the knowledge base
	log.Printf("🌱 Growing knowledge base")

	episodes := e.getEpisodesFromContext()
	domain := e.getCurrentDomain()

	err := e.knowledgeGrowth.GrowKnowledgeBase(episodes, domain)
	if err != nil {
		log.Printf("❌ Knowledge growth failed: %v", err)
		return
	}

	log.Printf("✅ Knowledge base growth completed")

	// Log activity
	e.logActivity(
		fmt.Sprintf("Knowledge base grew: processed %d episodes in domain '%s'", len(episodes), domain),
		"learning",
		map[string]string{
			"details": fmt.Sprintf("Domain: %s, Episodes: %d", domain, len(episodes)),
		},
	)
	e.context["knowledge_grown"] = true

	// Generate curiosity goals to trigger transition to reason state
	log.Printf("🎯 Generating curiosity goals for transition")
	goals, err := e.reasoning.GenerateCuriosityGoals(domain)
	if err != nil {
		log.Printf("❌ Failed to generate curiosity goals: %v", err)
		return
	}

	log.Printf("🎯 Generated %d curiosity goals", len(goals))

	// Set conclusion in context
	conclusion := fmt.Sprintf("Generated %d curiosity goals", len(goals))
	e.context["conclusion"] = conclusion
	e.context["confidence"] = 0.8

	// Emit curiosity_goals_generated event to trigger transition to reason state
	go func() {
		time.Sleep(200 * time.Millisecond)
		log.Printf("📤 Emitting curiosity_goals_generated event")
		e.handleEvent("curiosity_goals_generated", nil)
	}()
}

func (e *FSMEngine) executeConsistencyChecker(action ActionConfig, event map[string]interface{}) {
	// Validate knowledge consistency
	log.Printf("🔍 Validating knowledge consistency")

	domain := e.getCurrentDomain()
	err := e.knowledgeGrowth.ValidateKnowledgeConsistency(domain)
	if err != nil {
		log.Printf("❌ Consistency check failed: %v", err)
		return
	}

	log.Printf("✅ Knowledge consistency validation completed")
	e.context["consistency_validated"] = true
}

func (e *FSMEngine) executeCheckpoint(action ActionConfig, event map[string]interface{}) {
	// Create checkpoint with domain insights
	log.Printf("Creating checkpoint with domain insights")
}

func (e *FSMEngine) executeEpisodicUpdater(action ActionConfig, event map[string]interface{}) {
	// Update episodic memory with domain links
	log.Printf("Updating episodic memory with domain links")
}

func (e *FSMEngine) executeLogger(action ActionConfig, event map[string]interface{}) {
	// Log with domain context
	log.Printf("Logging with domain context")
}

func (e *FSMEngine) executeRecovery(action ActionConfig, event map[string]interface{}) {
	// Recovery using domain fallbacks
	log.Printf("Recovery using domain fallbacks")
	// After a short cooldown, transition back to idle to allow new cycles
	go func() {
		time.Sleep(3 * time.Second)
		e.handleEvent("recovered", nil)
	}()
}

func (e *FSMEngine) executeCleanup(action ActionConfig, event map[string]interface{}) {
	// Cleanup with domain knowledge preservation
	log.Printf("Cleanup with domain knowledge preservation")
}

// Reasoning action implementations
func (e *FSMEngine) executeBeliefQuery(action ActionConfig, event map[string]interface{}) {
	// Query beliefs from the knowledge base
	log.Printf("🧠 Querying beliefs from knowledge base")

	// Extract query from event or context
	query := "all concepts"
	if q, ok := event["query"].(string); ok && q != "" {
		query = q
	} else if q, ok := e.context["belief_query"].(string); ok && q != "" {
		query = q
	}

	domain := e.getCurrentDomain()
	beliefs, err := e.reasoning.QueryBeliefs(query, domain)
	if err != nil {
		log.Printf("❌ Belief query failed: %v", err)
		e.context["belief_query_error"] = err.Error()
		return
	}

	// Store beliefs in context
	e.context["beliefs"] = beliefs
	e.context["belief_count"] = len(beliefs)
	log.Printf("✅ Retrieved %d beliefs", len(beliefs))

	// Set conclusion in context
	conclusion := fmt.Sprintf("Retrieved %d beliefs", len(beliefs))
	e.context["conclusion"] = conclusion
	e.context["confidence"] = 0.7

	// Minimal reasoning trace (why channel)
	trace := ReasoningTrace{
		ID:        fmt.Sprintf("trace_%d", time.Now().UnixNano()),
		Goal:      e.getCurrentGoal(),
		Domain:    e.getCurrentDomain(),
		CreatedAt: time.Now(),
		Steps: []ReasoningStep{
			{
				StepNumber: 1,
				Action:     "belief_query",
				Query:      query,
				Result:     map[string]interface{}{"count": len(beliefs)},
				Reasoning:  "Queried beliefs from knowledge base via HDN",
				Confidence: 0.7,
				Timestamp:  time.Now(),
			},
		},
		Conclusion: fmt.Sprintf("Retrieved %d beliefs", len(beliefs)),
		Confidence: 0.7,
		Properties: map[string]interface{}{
			"state": e.currentState,
			"agent": e.agentID,
		},
	}
	if err := e.reasoning.LogReasoningTrace(trace); err != nil {
		log.Printf("Warning: failed to log reasoning trace: %v", err)
	}

	// Emit beliefs_queried event
	go func() {
		time.Sleep(200 * time.Millisecond)
		e.handleEvent("beliefs_queried", nil)
	}()
}

func (e *FSMEngine) executeInference(action ActionConfig, event map[string]interface{}) {
	// Apply inference rules to generate new beliefs
	log.Printf("🔍 Applying inference rules")

	domain := e.getCurrentDomain()

	// Set a default goal if none exists
	if e.getCurrentGoal() == "Unknown goal" {
		e.context["current_goal"] = "Knowledge inference and exploration"
		log.Printf("🎯 Set default goal: Knowledge inference and exploration")
	}

	newBeliefs, err := e.reasoning.InferNewBeliefs(domain)
	if err != nil {
		log.Printf("❌ Inference failed: %v", err)
		e.context["inference_error"] = err.Error()
		return
	}

	// Store inferred beliefs
	if e.context["inferred_beliefs"] == nil {
		e.context["inferred_beliefs"] = []interface{}{}
	}
	var beliefInterfaces []interface{}
	for _, belief := range newBeliefs {
		beliefInterfaces = append(beliefInterfaces, belief)
	}
	e.context["inferred_beliefs"] = append(e.context["inferred_beliefs"].([]interface{}), beliefInterfaces...)

	log.Printf("✨ Inferred %d new beliefs", len(newBeliefs))

	// Set conclusion in context
	conclusion := fmt.Sprintf("Inferred %d new beliefs in domain '%s'", len(newBeliefs), domain)
	e.context["conclusion"] = conclusion
	e.context["confidence"] = 0.7

	// Enhanced reasoning trace with more detail
	itrace := ReasoningTrace{
		ID:        fmt.Sprintf("trace_%d", time.Now().UnixNano()),
		Goal:      e.getCurrentGoal(),
		Domain:    domain,
		CreatedAt: time.Now(),
		Steps: []ReasoningStep{
			{
				StepNumber: 1,
				Action:     "inference",
				Query:      "apply_inference_rules",
				Result:     map[string]interface{}{"inferred_count": len(newBeliefs), "domain": domain},
				Reasoning:  fmt.Sprintf("Applied forward-chaining rules over Neo4j graph in domain '%s'", domain),
				Confidence: 0.7,
				Timestamp:  time.Now(),
			},
		},
		Conclusion: fmt.Sprintf("Inferred %d new beliefs in domain '%s'", len(newBeliefs), domain),
		Confidence: 0.7,
		Properties: map[string]interface{}{
			"state":  e.currentState,
			"agent":  e.agentID,
			"domain": domain,
		},
	}

	// Store trace in context for retrieval (tolerate multiple underlying types)
	if existing, ok := e.context["reasoning_traces"]; !ok || existing == nil {
		// initialize as []interface{} for flexibility
		e.context["reasoning_traces"] = []interface{}{itrace}
	} else {
		switch v := existing.(type) {
		case []ReasoningTrace:
			traces := append(v, itrace)
			e.context["reasoning_traces"] = traces
		case []interface{}:
			e.context["reasoning_traces"] = append(v, itrace)
		default:
			// fallback: wrap both old and new into []interface{}
			e.context["reasoning_traces"] = []interface{}{v, itrace}
		}
	}

	if err := e.reasoning.LogReasoningTrace(itrace); err != nil {
		log.Printf("Warning: failed to log reasoning trace: %v", err)
	}

	// Emit beliefs_inferred event
	go func() {
		time.Sleep(200 * time.Millisecond)
		e.handleEvent("beliefs_inferred", nil)
	}()
}

func (e *FSMEngine) executeCuriosityGoals(action ActionConfig, event map[string]interface{}) {
	// Generate curiosity-driven goals
	log.Printf("🎯 Generating curiosity goals")

	domain := e.getCurrentDomain()
	goals, err := e.reasoning.GenerateCuriosityGoals(domain)
	if err != nil {
		log.Printf("❌ Curiosity goals generation failed: %v", err)
		e.context["curiosity_goals_error"] = err.Error()
		return
	}

	// Store goals in context
	e.context["curiosity_goals"] = goals
	e.context["curiosity_goal_count"] = len(goals)

	// Set a current goal if none exists and we have goals
	if e.getCurrentGoal() == "Unknown goal" && len(goals) > 0 {
		// Select the highest priority goal
		bestGoal := goals[0]
		for _, goal := range goals {
			if goal.Priority > bestGoal.Priority {
				bestGoal = goal
			}
		}
		e.context["current_goal"] = bestGoal.Description
		log.Printf("🎯 Set current goal from curiosity goals: %s", bestGoal.Description)
	}

	log.Printf("🎯 Generated %d curiosity goals", len(goals))

	// Set conclusion in context
	conclusion := fmt.Sprintf("Generated %d curiosity goals", len(goals))
	e.context["conclusion"] = conclusion
	e.context["confidence"] = 0.8

	// Emit curiosity_goals_generated event
	go func() {
		time.Sleep(200 * time.Millisecond)
		e.handleEvent("curiosity_goals_generated", nil)
	}()
}

func (e *FSMEngine) executeExplanation(action ActionConfig, event map[string]interface{}) {
	// Generate explanation for reasoning
	log.Printf("💭 Generating reasoning explanation")

	goal := "reasoning_explanation"
	if g, ok := event["goal"].(string); ok && g != "" {
		goal = g
	} else if g, ok := e.context["current_goal"].(string); ok && g != "" {
		goal = g
	} else if g, ok := e.context["explanation_goal"].(string); ok && g != "" {
		goal = g
	}

	domain := e.getCurrentDomain()
	explanation, err := e.reasoning.ExplainReasoning(goal, domain)
	if err != nil {
		log.Printf("❌ Explanation generation failed: %v", err)
		e.context["explanation_error"] = err.Error()
		return
	}

	// Store explanation in context
	e.context["reasoning_explanation"] = explanation
	log.Printf("💭 Generated explanation: %s", explanation)

	// Persist explanation to Redis for Monitor UI consumption
	go func(goal, domain, text string) {
		defer func() { recover() }()
		payload := map[string]interface{}{
			"goal":        goal,
			"domain":      domain,
			"explanation": text,
			"created_at":  time.Now().UTC().Format(time.RFC3339),
		}

		// Add workflow context if available
		if workflowID, ok := e.context["current_workflow_id"].(string); ok && workflowID != "" {
			payload["workflow_id"] = workflowID
		}
		if projectID, ok := e.context["project_id"].(string); ok && projectID != "" {
			payload["project_id"] = projectID
		}
		if sessionID, ok := e.context["session_id"].(string); ok && sessionID != "" {
			payload["session_id"] = sessionID
		}

		if b, err := json.Marshal(payload); err == nil {
			key := fmt.Sprintf("reasoning:explanations:%s", goal)
			if err := e.redis.LPush(e.ctx, key, b).Err(); err == nil {
				_ = e.redis.LTrim(e.ctx, key, 0, 49).Err() // keep last 50
				log.Printf("📝 Persisted reasoning explanation for goal %s", goal)
			} else {
				log.Printf("⚠️ Failed to persist reasoning explanation: %v", err)
			}
		}
	}(goal, domain, explanation)

	// Emit explanation_generated event
	go func() {
		time.Sleep(200 * time.Millisecond)
		e.handleEvent("explanation_generated", nil)
	}()
}

func (e *FSMEngine) executeNewsStorage(action ActionConfig, event map[string]interface{}) {
	// Store news events for curiosity goal generation
	log.Printf("📰 Storing news events for curiosity goal generation")

	// Check if this is a news event
	eventType, ok := event["type"].(string)
	if !ok {
		return
	}

	// Forward news events to HDN server for episodic memory indexing
	e.forwardNewsEventToHDN(event)

	// Store news events directly in Weaviate for Monitor UI
	e.storeNewsEventInWeaviate(event)

	// Store news relations in Redis for reasoning
	if eventType == "relations" {
		if payload, ok := event["payload"].(map[string]interface{}); ok {
			if metadata, ok := payload["metadata"].(map[string]interface{}); ok {
				// Store the relation data
				relationData := map[string]interface{}{
					"head":       metadata["head"],
					"relation":   metadata["relation"],
					"tail":       metadata["tail"],
					"headline":   metadata["headline"],
					"source":     metadata["source"],
					"timestamp":  metadata["timestamp"],
					"confidence": metadata["confidence"],
				}

				if b, err := json.Marshal(relationData); err == nil {
					key := "reasoning:news_relations:recent"
					if err := e.redis.LPush(e.ctx, key, b).Err(); err == nil {
						_ = e.redis.LTrim(e.ctx, key, 0, 99).Err() // keep last 100
						log.Printf("📰 Stored news relation: %s %s %s", metadata["head"], metadata["relation"], metadata["tail"])
					}
				}
			}
		}
	}

	// Store news alerts in Redis for reasoning
	if eventType == "alerts" {
		if payload, ok := event["payload"].(map[string]interface{}); ok {
			if metadata, ok := payload["metadata"].(map[string]interface{}); ok {
				// Store the alert data
				alertData := map[string]interface{}{
					"headline":   metadata["headline"],
					"impact":     metadata["impact"],
					"source":     metadata["source"],
					"timestamp":  metadata["timestamp"],
					"confidence": metadata["confidence"],
				}

				if b, err := json.Marshal(alertData); err == nil {
					key := "reasoning:news_alerts:recent"
					if err := e.redis.LPush(e.ctx, key, b).Err(); err == nil {
						_ = e.redis.LTrim(e.ctx, key, 0, 49).Err() // keep last 50
						log.Printf("📰 Stored news alert: %s (impact: %s)", metadata["headline"], metadata["impact"])
					}
				}
			}
		}
	}
}

// forwardNewsEventToHDN forwards news events to the HDN server for episodic memory indexing
func (e *FSMEngine) forwardNewsEventToHDN(event map[string]interface{}) {
	// Convert the event to the canonical format expected by HDN
	canonicalEvent := e.convertToCanonicalEvent(event)
	if canonicalEvent == nil {
		log.Printf("⚠️ Failed to convert news event to canonical format")
		return
	}

	// Publish to the HDN input subject
	eventData, err := json.Marshal(canonicalEvent)
	if err != nil {
		log.Printf("❌ Failed to marshal news event for HDN: %v", err)
		return
	}

	// Publish to agi.events.input (the subject HDN listens to)
	if err := e.nc.Publish("agi.events.input", eventData); err != nil {
		log.Printf("❌ Failed to publish news event to HDN: %v", err)
		return
	}

	log.Printf("📡 Forwarded news event to HDN: %s (type: %s)", canonicalEvent.EventID, canonicalEvent.Type)
}

// convertToCanonicalEvent converts a news event to the canonical format expected by HDN
func (e *FSMEngine) convertToCanonicalEvent(event map[string]interface{}) *CanonicalEvent {
	// Extract basic event information
	eventType, ok := event["type"].(string)
	if !ok {
		return nil
	}

	// Extract payload
	payload, ok := event["payload"].(map[string]interface{})
	if !ok {
		return nil
	}

	// Create canonical event
	now := time.Now().UTC()
	canonicalEvent := &CanonicalEvent{
		EventID:   e.generateEventID(),
		Source:    "news:fsm",
		Type:      eventType,
		Timestamp: now.Format(time.RFC3339),
		Context: map[string]interface{}{
			"channel": "news",
		},
		Payload: payload,
		Security: map[string]interface{}{
			"sensitivity": "low",
		},
	}

	return canonicalEvent
}

// generateEventID generates a unique event ID
func (e *FSMEngine) generateEventID() string {
	now := time.Now().UTC()
	return fmt.Sprintf("evt_%s_%x", now.Format("20060102"), now.UnixNano()&0xffffffff)
}

// storeNewsEventInWeaviate stores news events directly in Weaviate using WikipediaArticle class
func (e *FSMEngine) storeNewsEventInWeaviate(event map[string]interface{}) {
	log.Printf("🔍 DEBUG: storeNewsEventInWeaviate called with event: %+v", event)

	// Get Weaviate URL from environment or use default
	weaviateURL := os.Getenv("WEAVIATE_URL")
	if weaviateURL == "" {
		weaviateURL = "http://localhost:8080"
	}
	log.Printf("🔍 DEBUG: Using Weaviate URL: %s", weaviateURL)

	// Extract event information
	eventType, ok := event["type"].(string)
	if !ok {
		log.Printf("❌ DEBUG: No event type found in event")
		return
	}
	log.Printf("🔍 DEBUG: Event type: %s", eventType)

	payload, ok := event["payload"].(map[string]interface{})
	if !ok {
		log.Printf("❌ DEBUG: No payload found in event")
		return
	}
	log.Printf("🔍 DEBUG: Payload: %+v", payload)

	metadata, ok := payload["metadata"].(map[string]interface{})
	if !ok {
		log.Printf("❌ DEBUG: No metadata found in payload")
		return
	}
	log.Printf("🔍 DEBUG: Metadata: %+v", metadata)

	// Create WikipediaArticle object for Weaviate
	now := time.Now().UTC()
	articleID := fmt.Sprintf("news_%s_%x", now.Format("20060102"), now.UnixNano()&0xffffffff)

	// Extract title and text based on event type
	var title, text string
	if eventType == "relations" {
		if headline, ok := metadata["headline"].(string); ok {
			title = headline
		} else {
			title = fmt.Sprintf("News Relation: %s %s %s",
				getStringFromMap(metadata, "head"),
				getStringFromMap(metadata, "relation"),
				getStringFromMap(metadata, "tail"))
		}
		text = fmt.Sprintf("Relation: %s %s %s. Headline: %s. Source: %s",
			getStringFromMap(metadata, "head"),
			getStringFromMap(metadata, "relation"),
			getStringFromMap(metadata, "tail"),
			getStringFromMap(metadata, "headline"),
			getStringFromMap(metadata, "source"))
	} else if eventType == "alerts" {
		if headline, ok := metadata["headline"].(string); ok {
			title = headline
		} else {
			title = "News Alert"
		}
		text = fmt.Sprintf("Alert: %s. Impact: %s. Source: %s",
			getStringFromMap(metadata, "headline"),
			getStringFromMap(metadata, "impact"),
			getStringFromMap(metadata, "source"))
	} else {
		// Generic news event
		title = getStringFromMap(metadata, "headline")
		if title == "" {
			title = fmt.Sprintf("News Event: %s", eventType)
		}
		text = getStringFromMap(metadata, "text")
		if text == "" {
			text = fmt.Sprintf("News event of type: %s", eventType)
		}
	}

	// Create metadata as JSON string for Weaviate
	metadataObj := map[string]interface{}{
		"event_type":        eventType,
		"confidence":        getStringFromMap(metadata, "confidence"),
		"original_metadata": metadata,
	}
	metadataJSON, err := json.Marshal(metadataObj)
	if err != nil {
		log.Printf("❌ Failed to marshal metadata for Weaviate: %v", err)
		return
	}

	// Create Weaviate object
	weaviateObject := map[string]interface{}{
		"class": "WikipediaArticle",
		"properties": map[string]interface{}{
			"title":     title,
			"text":      text,
			"source":    "news:fsm",
			"url":       getStringFromMap(metadata, "url"),
			"timestamp": now.Format(time.RFC3339),
			"metadata":  string(metadataJSON),
		},
	}

	// Send to Weaviate
	jsonData, err := json.Marshal(weaviateObject)
	if err != nil {
		log.Printf("❌ Failed to marshal news event for Weaviate: %v", err)
		return
	}

	url := weaviateURL + "/v1/objects"
	req, err := http.NewRequest("POST", url, bytes.NewReader(jsonData))
	if err != nil {
		log.Printf("❌ Failed to create Weaviate request: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("❌ Failed to send news event to Weaviate: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		log.Printf("❌ Weaviate returned error for news event: %s", resp.Status)
		return
	}

	log.Printf("✅ Stored news event in Weaviate: %s (type: %s)", articleID, eventType)
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

func (e *FSMEngine) executeTraceLogger(action ActionConfig, event map[string]interface{}) {
	// Log reasoning trace
	log.Printf("📝 Logging reasoning trace")

	// Create trace from current context
	trace := ReasoningTrace{
		ID:         fmt.Sprintf("trace_%d", time.Now().UnixNano()),
		Goal:       e.getCurrentGoal(),
		Steps:      e.getCurrentReasoningSteps(),
		Evidence:   e.getCurrentEvidence(),
		Conclusion: e.getCurrentConclusion(),
		Confidence: e.getCurrentConfidence(),
		Domain:     e.getCurrentDomain(),
		CreatedAt:  time.Now(),
		Properties: map[string]interface{}{
			"agent_id": e.agentID,
			"state":    e.currentState,
		},
	}

	err := e.reasoning.LogReasoningTrace(trace)
	if err != nil {
		log.Printf("❌ Trace logging failed: %v", err)
		e.context["trace_logging_error"] = err.Error()
		return
	}

	log.Printf("📝 Reasoning trace logged successfully")
}

func (e *FSMEngine) executeHypothesisTesting(action ActionConfig, event map[string]interface{}) {
	// Test a hypothesis by gathering evidence and evaluating it
	log.Printf("🧪 Testing hypothesis")

	// Get hypothesis ID from context or event
	hypothesisID := ""
	if id, ok := event["hypothesis_id"].(string); ok && id != "" {
		hypothesisID = id
	} else if id, ok := e.context["current_hypothesis_id"].(string); ok && id != "" {
		hypothesisID = id
	}

	if hypothesisID == "" {
		log.Printf("❌ No hypothesis ID provided for testing")
		return
	}

	// Get hypothesis from Redis
	key := fmt.Sprintf("fsm:%s:hypotheses", e.agentID)
	hypothesisData, err := e.redis.HGet(e.ctx, key, hypothesisID).Result()
	if err != nil {
		log.Printf("❌ Failed to get hypothesis %s: %v", hypothesisID, err)
		return
	}

	var hypothesis map[string]interface{}
	if err := json.Unmarshal([]byte(hypothesisData), &hypothesis); err != nil {
		log.Printf("❌ Failed to parse hypothesis %s: %v", hypothesisID, err)
		return
	}

	// Update hypothesis status to "testing"
	hypothesis["status"] = "testing"
	hypothesis["testing_started_at"] = time.Now().Format(time.RFC3339)

	// Store updated hypothesis
	updatedData, _ := json.Marshal(hypothesis)
	e.redis.HSet(e.ctx, key, hypothesisID, updatedData)

	log.Printf("🧪 Testing hypothesis: %s", hypothesis["description"])

	// Test the hypothesis by creating and using tools
	domain := e.getCurrentDomain()
	evidence, err := e.testHypothesisWithTools(hypothesis, domain)
	if err != nil {
		log.Printf("❌ Failed to test hypothesis %s with tools: %v", hypothesisID, err)
		// Mark as failed
		hypothesis["status"] = "failed"
		hypothesis["testing_error"] = err.Error()
		updatedData, _ := json.Marshal(hypothesis)
		e.redis.HSet(e.ctx, key, hypothesisID, updatedData)
		return
	}

	// Evaluate the hypothesis based on tool results
	result := e.evaluateHypothesis(hypothesis, evidence, domain)

	// Update hypothesis with results
	hypothesis["status"] = result.Status
	hypothesis["testing_completed_at"] = time.Now().Format(time.RFC3339)

	// Log activity
	desc, _ := hypothesis["description"].(string)
	e.logActivity(
		fmt.Sprintf("Hypothesis test result: %s (confidence: %.2f)", result.Status, result.Confidence),
		"hypothesis",
		map[string]string{
			"details": fmt.Sprintf("Hypothesis: %s, Status: %s", desc[:min(100, len(desc))], result.Status),
		},
	)
	hypothesis["evidence"] = evidence
	hypothesis["evaluation"] = result.Evaluation
	hypothesis["confidence"] = result.Confidence

	// Store final hypothesis state
	updatedData, _ = json.Marshal(hypothesis)
	e.redis.HSet(e.ctx, key, hypothesisID, updatedData)

	log.Printf("✅ Hypothesis testing completed: %s (status: %s, confidence: %.2f)",
		hypothesis["description"], result.Status, result.Confidence)

	// Store reasoning trace for the hypothesis testing
	trace := ReasoningTrace{
		ID:        fmt.Sprintf("trace_%d", time.Now().UnixNano()),
		Goal:      fmt.Sprintf("Test hypothesis: %s", hypothesis["description"]),
		Domain:    domain,
		CreatedAt: time.Now(),
		Steps: []ReasoningStep{
			{
				StepNumber: 1,
				Action:     "hypothesis_testing",
				Query:      hypothesis["description"].(string),
				Result:     map[string]interface{}{"status": result.Status, "confidence": result.Confidence},
				Reasoning:  fmt.Sprintf("Tested hypothesis by gathering %d pieces of evidence", len(evidence)),
				Confidence: result.Confidence,
				Timestamp:  time.Now(),
			},
		},
		Conclusion: fmt.Sprintf("Hypothesis %s: %s (confidence: %.2f)", result.Status, hypothesis["description"], result.Confidence),
		Confidence: result.Confidence,
		Properties: map[string]interface{}{
			"hypothesis_id":  hypothesisID,
			"evidence_count": len(evidence),
		},
	}

	if err := e.reasoning.LogReasoningTrace(trace); err != nil {
		log.Printf("Warning: failed to log reasoning trace: %v", err)
	}

	// Process hypothesis results based on status
	e.processHypothesisResult(hypothesis, result, domain)

	// Emit hypothesis_tested event
	go func() {
		time.Sleep(200 * time.Millisecond)
		eventData := map[string]interface{}{
			"hypothesis_id": hypothesisID,
			"status":        result.Status,
			"confidence":    result.Confidence,
		}
		data, _ := json.Marshal(eventData)
		e.handleEvent("hypothesis_tested", data)
	}()
}

// TriggerAutonomyCycle runs a minimal self-directed reasoning cycle:
// - Generates curiosity goals for the current domain
// - Selects the highest-priority goal (first)
// - Updates context and emits a curiosity_goals_generated event
// Autonomy implementation moved to autonomy.go

// Helper methods for reasoning
func (e *FSMEngine) getCurrentGoal() string {
	if goal, ok := e.context["current_goal"].(string); ok {
		return goal
	}
	return "Unknown goal"
}

func (e *FSMEngine) getCurrentReasoningSteps() []ReasoningStep {
	if steps, ok := e.context["reasoning_steps"].([]ReasoningStep); ok {
		return steps
	}
	return []ReasoningStep{}
}

func (e *FSMEngine) getCurrentEvidence() []string {
	if evidence, ok := e.context["evidence"].([]string); ok {
		return evidence
	}
	return []string{}
}

func (e *FSMEngine) getCurrentConclusion() string {
	if conclusion, ok := e.context["conclusion"].(string); ok {
		return conclusion
	}
	return "No conclusion reached"
}

func (e *FSMEngine) getCurrentConfidence() float64 {
	if confidence, ok := e.context["confidence"].(float64); ok {
		return confidence
	}
	return 0.5
}

// evaluateGuard evaluates a guard condition
func (e *FSMEngine) evaluateGuard(guardName string, event map[string]interface{}) bool {
	guard := e.findGuardConfig(guardName)
	if guard == nil {
		log.Printf("Unknown guard: %s", guardName)
		return false
	}

	// Dispatch to appropriate guard module
	switch guard.Module {
	case "knowledge.input_validator":
		return e.validateInput(*guard, event)
	case "fsm.work_checker":
		return e.checkPendingWork(*guard, event)
	case "fsm.timeout_checker":
		return e.checkTimeout(*guard, event)
	default:
		log.Printf("Unknown guard module: %s", guard.Module)
		return false
	}
}

func (e *FSMEngine) validateInput(guard GuardConfig, event map[string]interface{}) bool {
	// Validate input against domain constraints
	log.Printf("Validating input against domain constraints")
	return true // Simplified
}

func (e *FSMEngine) checkPendingWork(guard GuardConfig, event map[string]interface{}) bool {
	// Check for pending work in queues
	log.Printf("Checking for pending work")
	return true // Simplified
}

func (e *FSMEngine) checkTimeout(guard GuardConfig, event map[string]interface{}) bool {
	// Get timeout duration from guard parameters
	stateDurationSeconds, ok := guard.Params["state_duration_seconds"].(int)
	if !ok {
		log.Printf("Invalid or missing state_duration_seconds parameter for timeout guard")
		return false
	}

	// Calculate time elapsed since state entry
	elapsed := time.Since(e.stateEntryTime)
	timeoutDuration := time.Duration(stateDurationSeconds) * time.Second

	log.Printf("Timeout check: elapsed %v, timeout %v", elapsed, timeoutDuration)

	// Return true if timeout has been reached
	return elapsed >= timeoutDuration
}

// Helper methods
func (e *FSMEngine) findStateConfig(stateName string) *StateConfig {
	log.Printf("🔍 Looking for state: %s (available states: %d)", stateName, len(e.config.States))
	for i, state := range e.config.States {
		log.Printf("  State %d: %s", i, state.Name)
		if state.Name == stateName {
			return &state
		}
	}
	log.Printf("❌ State %s not found in configuration", stateName)
	return nil
}

func (e *FSMEngine) findGuardConfig(guardName string) *GuardConfig {
	for _, guard := range e.config.Guards {
		if guard.Name == guardName {
			return &guard
		}
	}
	return nil
}

func (e *FSMEngine) getRedisKey(keyType string) string {
	switch keyType {
	case "state":
		return fmt.Sprintf(e.config.RedisKeys.State, e.agentID)
	case "context":
		return fmt.Sprintf(e.config.RedisKeys.Context, e.agentID)
	case "queue":
		return fmt.Sprintf(e.config.RedisKeys.Queue, e.agentID)
	case "beliefs":
		return fmt.Sprintf(e.config.RedisKeys.Beliefs, e.agentID)
	case "episodes":
		return fmt.Sprintf(e.config.RedisKeys.Episodes, e.agentID)
	case "hypotheses":
		return fmt.Sprintf(e.config.RedisKeys.Hypotheses, e.agentID)
	case "domain_insights":
		return fmt.Sprintf(e.config.RedisKeys.DomainInsights, e.agentID)
	default:
		return ""
	}
}

func (e *FSMEngine) saveState() error {
	stateData := map[string]interface{}{
		"state":   e.currentState,
		"context": e.context,
		"updated": time.Now().Unix(),
	}

	data, _ := json.Marshal(stateData)
	return e.redis.Set(e.ctx, e.getRedisKey("state"), data, 0).Err()
}

func (e *FSMEngine) loadState() error {
	data, err := e.redis.Get(e.ctx, e.getRedisKey("state")).Result()
	if err != nil {
		return err
	}

	var stateData map[string]interface{}
	if err := json.Unmarshal([]byte(data), &stateData); err != nil {
		return err
	}

	if state, ok := stateData["state"].(string); ok {
		e.currentState = state
	}
	if context, ok := stateData["context"].(map[string]interface{}); ok {
		e.context = context
	}

	return nil
}

func (e *FSMEngine) publishTransition(from, to, reason string, event map[string]interface{}) {
	transitionEvent := FSMTransitionEvent{
		AgentID:   e.agentID,
		From:      from,
		To:        to,
		Reason:    reason,
		Timestamp: time.Now().Format(time.RFC3339),
		Context:   e.context,
	}

	data, _ := json.Marshal(transitionEvent)
	e.nc.Publish("agi.events.fsm.transition", data)
}

// publishThought publishes an AI thought event
func (e *FSMEngine) publishThought(thoughtType, thought, goal string, confidence float64, sessionID string, toolUsed, action, result string) {
	thoughtEvent := ThoughtEvent{
		AgentID:    e.agentID,
		SessionID:  sessionID,
		Type:       thoughtType,
		State:      e.currentState,
		Goal:       goal,
		Thought:    thought,
		Confidence: confidence,
		ToolUsed:   toolUsed,
		Action:     action,
		Result:     result,
		Timestamp:  time.Now().Format(time.RFC3339),
		Context:    e.context,
		Metadata: map[string]interface{}{
			"state_entry_time": e.stateEntryTime.Format(time.RFC3339),
			"state_duration":   time.Since(e.stateEntryTime).Seconds(),
		},
	}

	data, _ := json.Marshal(thoughtEvent)
	e.nc.Publish("agi.events.fsm.thought", data)
}

// publishThinking publishes a general thinking event
func (e *FSMEngine) publishThinking(thought, goal string, confidence float64, sessionID string) {
	e.publishThought("thinking", thought, goal, confidence, sessionID, "", "", "")
}

// publishDecision publishes a decision-making event
func (e *FSMEngine) publishDecision(thought, goal string, confidence float64, sessionID string, action string) {
	e.publishThought("decision", thought, goal, confidence, sessionID, "", action, "")
}

// publishAction publishes an action execution event
func (e *FSMEngine) publishAction(thought, goal string, confidence float64, sessionID string, toolUsed, action, result string) {
	e.publishThought("action", thought, goal, confidence, sessionID, toolUsed, action, result)
}

// publishObservation publishes an observation/learning event
func (e *FSMEngine) publishObservation(thought, goal string, confidence float64, sessionID string, result string) {
	e.publishThought("observation", thought, goal, confidence, sessionID, "", "", result)
}

// publishPerceptionFacts publishes extracted facts to Goals server
func (e *FSMEngine) publishPerceptionFacts(facts []interface{}) {
	if len(facts) == 0 {
		return
	}

	// Create perception fact event for Goals server
	for _, fact := range facts {
		if factMap, ok := fact.(map[string]interface{}); ok {
			perceptionEvent := map[string]interface{}{
				"agent_id":  e.agentID,
				"timestamp": time.Now().Unix(),
				"source":    "fsm.knowledge_extractor",
				"fact_type": "extracted",
				"domain":    e.getCurrentDomain(),
				"data":      factMap,
			}

			data, _ := json.Marshal(perceptionEvent)
			e.nc.Publish("agi.perception.fact", data)
			log.Printf("📤 Published perception fact to Goals server: %v", factMap)
		}
	}
}

// publishEvaluationResults publishes evaluation results to Goals server
func (e *FSMEngine) publishEvaluationResults() {
	// Create evaluation result event for Goals server
	evaluationEvent := map[string]interface{}{
		"agent_id":      e.agentID,
		"timestamp":     time.Now().Unix(),
		"source":        "fsm.outcome_analyzer",
		"domain":        e.getCurrentDomain(),
		"current_state": e.currentState,
		"context":       e.context,
	}

	// Add performance metrics if available
	if metrics, ok := e.context["performance_metrics"].(map[string]interface{}); ok {
		evaluationEvent["metrics"] = metrics
	}

	// Add error rate if available in context
	if errorRate, ok := e.context["error_rate"].(float64); ok {
		evaluationEvent["error_rate"] = errorRate
	}

	data, _ := json.Marshal(evaluationEvent)
	e.nc.Publish("agi.evaluation.result", data)
	log.Printf("📤 Published evaluation result to Goals server")
}

// publishUserGoal publishes user goals to Goals server
func (e *FSMEngine) publishUserGoal(event CanonicalEvent) {
	// Extract user request from event payload
	userRequest := ""
	if text, ok := event.Payload["text"].(string); ok && text != "" {
		userRequest = text
	} else if request, ok := event.Payload["user_request"].(string); ok && request != "" {
		userRequest = request
	} else if message, ok := event.Payload["message"].(string); ok && message != "" {
		userRequest = message
	}

	if userRequest == "" {
		return
	}

	// Create user goal event for Goals server
	userGoalEvent := map[string]interface{}{
		"agent_id":    e.agentID,
		"description": userRequest,
		"origin":      "user_input",
		"priority":    "medium",
		"status":      "active",
		"confidence":  0.7,
		"source":      event.Source,
		"timestamp":   time.Now().Unix(),
		"event_id":    event.EventID,
	}

	data, _ := json.Marshal(userGoalEvent)
	e.nc.Publish("agi.user.goal", data)
	log.Printf("📤 Published user goal to Goals server: %s", userRequest)
}

// GetCurrentState returns the current state
func (e *FSMEngine) GetCurrentState() string {
	return e.currentState
}

// GetContext returns the current context
func (e *FSMEngine) GetContext() map[string]interface{} {
	return e.context
}

// Helper methods for knowledge growth
func (e *FSMEngine) getEpisodesFromContext() []map[string]interface{} {
	// Get episodes from context or Redis
	if episodes, ok := e.context["episodes"].([]map[string]interface{}); ok {
		return episodes
	}

	// Fallback: get recent episodes from Redis
	episodesKey := e.getRedisKey("episodes")
	results, err := e.redis.LRange(e.ctx, episodesKey, 0, 9).Result()
	if err != nil {
		log.Printf("Warning: Could not get episodes from Redis: %v", err)
		return []map[string]interface{}{}
	}

	var episodes []map[string]interface{}
	for _, result := range results {
		var episode map[string]interface{}
		if err := json.Unmarshal([]byte(result), &episode); err == nil {
			episodes = append(episodes, episode)
		}
	}

	return episodes
}

func (e *FSMEngine) getCurrentDomain() string {
	// Get domain from context
	if domain, ok := e.context["current_domain"].(string); ok {
		return domain
	}

	// Fallback: get from last episode
	episodes := e.getEpisodesFromContext()
	if len(episodes) > 0 {
		if domain, ok := episodes[0]["domain"].(string); ok {
			return domain
		}
	}

	// Default domain
	return "General"
}

// containsIgnoreCase checks if substr in s (case-insensitive)
func containsIgnoreCase(s, substr string) bool {
	ls := strings.ToLower(s)
	lsub := strings.ToLower(substr)
	return strings.Contains(ls, lsub)
}

// ensureHDNProject creates the project if it does not exist
func (e *FSMEngine) ensureHDNProject(projectID string) {
	base := os.Getenv("HDN_URL")
	if base == "" {
		base = "http://localhost:8080"
	}
	// Try GET project
	getURL := fmt.Sprintf("%s/api/v1/projects/%s", base, projectID)
	if resp, err := http.Get(getURL); err == nil {
		if resp.Body != nil {
			resp.Body.Close()
		}
		if resp.StatusCode == http.StatusOK {
			return
		}
	}
	// Create project
	createURL := fmt.Sprintf("%s/api/v1/projects", base)
	body := map[string]interface{}{"id": projectID, "name": projectID, "description": "FSM-managed project"}
	data, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", createURL, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	_, _ = (&http.Client{Timeout: 15 * time.Second}).Do(req)
}

// getFactsFromContext extracts facts from the current context
func (e *FSMEngine) getFactsFromContext() []Fact {
	var facts []Fact

	// Get extracted facts from context
	if extractedFacts, ok := e.context["extracted_facts"].([]interface{}); ok {
		for _, factData := range extractedFacts {
			if factMap, ok := factData.(map[string]interface{}); ok {
				fact := Fact{
					ID:         fmt.Sprintf("fact_%d", time.Now().UnixNano()),
					Content:    getString(factMap, "fact", "Unknown fact"),
					Domain:     getString(factMap, "domain", "General"),
					Confidence: getFloat64(factMap, "confidence", 0.8),
					Properties: map[string]interface{}{
						"source": "context",
						"domain": getString(factMap, "domain", "General"),
					},
					Constraints: []string{"Must follow domain safety principles"},
					CreatedAt:   time.Now(),
				}
				facts = append(facts, fact)
			}
		}
	}

	// If no facts from context, create a default fact
	if len(facts) == 0 {
		fact := Fact{
			ID:         fmt.Sprintf("fact_%d", time.Now().UnixNano()),
			Content:    "FSM is processing input and generating hypotheses",
			Domain:     e.getCurrentDomain(),
			Confidence: 0.7,
			Properties: map[string]interface{}{
				"source": "default",
				"domain": e.getCurrentDomain(),
			},
			Constraints: []string{"Must follow domain safety principles"},
			CreatedAt:   time.Now(),
		}
		facts = append(facts, fact)
	}

	return facts
}

// storeHypotheses stores hypotheses in Redis for monitoring
func (e *FSMEngine) storeHypotheses(hypotheses []Hypothesis, domain string) {
	key := fmt.Sprintf("fsm:%s:hypotheses", e.agentID)

	for _, hypothesis := range hypotheses {
		// Convert to map for Redis storage
		hypothesisData := map[string]interface{}{
			"id":          hypothesis.ID,
			"description": hypothesis.Description,
			"domain":      hypothesis.Domain,
			"confidence":  hypothesis.Confidence,
			"status":      hypothesis.Status,
			"facts":       hypothesis.Facts,
			"constraints": hypothesis.Constraints,
			"created_at":  hypothesis.CreatedAt.Format(time.RFC3339),
		}

		data, err := json.Marshal(hypothesisData)
		if err != nil {
			log.Printf("Warning: Failed to marshal hypothesis: %v", err)
			continue
		}

		// Store in Redis hash
		e.redis.HSet(e.ctx, key, hypothesis.ID, data)
	}

	// Set expiration for the hypotheses key (24 hours)
	e.redis.Expire(e.ctx, key, 24*time.Hour)

	log.Printf("📝 Stored %d hypotheses in Redis", len(hypotheses))
}

// createHypothesisTestingGoals creates curiosity goals for testing hypotheses
func (e *FSMEngine) createHypothesisTestingGoals(hypotheses []Hypothesis, domain string) {
	// Screen hypotheses with LLM before creating goals
	approved := e.screenHypothesesWithLLM(hypotheses, domain)

	// Collapse duplicates by normalized description to avoid multiple goals for same hypothesis idea
	seenHypothesisDesc := map[string]bool{}
	var uniqueApproved []Hypothesis
	for _, h := range approved {
		key := strings.ToLower(strings.TrimSpace(h.Description))
		if key == "" {
			uniqueApproved = append(uniqueApproved, h)
			continue
		}
		if seenHypothesisDesc[key] {
			continue
		}
		seenHypothesisDesc[key] = true
		uniqueApproved = append(uniqueApproved, h)
	}

	// Build existing dedup map
	goalKey := fmt.Sprintf("reasoning:curiosity_goals:%s", domain)
	existing := map[string]CuriosityGoal{}
	if existingGoalsData, err := e.redis.LRange(e.ctx, goalKey, 0, 199).Result(); err == nil {
		for _, gd := range existingGoalsData {
			var g CuriosityGoal
			if err := json.Unmarshal([]byte(gd), &g); err == nil {
				k := e.createDedupKey(g)
				existing[k] = g
			}
		}
	}

	// Create curiosity goals for testing each approved hypothesis, deduplicated
	newGoals := 0
	filteredCount := 0
	for _, hypothesis := range uniqueApproved {
		goal := CuriosityGoal{
			ID:          fmt.Sprintf("hyp_test_%s", hypothesis.ID),
			Type:        "hypothesis_testing",
			Description: fmt.Sprintf("Test hypothesis: %s", hypothesis.Description),
			Targets:     []string{hypothesis.ID},
			Priority:    8,
			Status:      "pending",
			Domain:      domain,
			CreatedAt:   time.Now(),
		}
		
		// Filter out generic/useless goals before adding
		if e.isGenericHypothesisGoal(goal) {
			filteredCount++
			log.Printf("🚫 Filtered out generic hypothesis goal: %s", goal.Description)
			continue
		}
		
		k := e.createDedupKey(goal)
		if _, exists := existing[k]; exists {
			continue
		}
		if goalData, err := json.Marshal(goal); err == nil {
			_ = e.redis.LPush(e.ctx, goalKey, goalData).Err()
			_ = e.redis.LTrim(e.ctx, goalKey, 0, 199).Err()
			existing[k] = goal
			newGoals++
		}
	}

	log.Printf("🎯 Created %d hypothesis testing goals (after LLM screening, deduped, filtered %d generic)", newGoals, filteredCount)
}

// screenHypothesesWithLLM calls the HDN interpreter to rate hypotheses and filters by threshold
func (e *FSMEngine) screenHypothesesWithLLM(hypotheses []Hypothesis, domain string) []Hypothesis {
	if len(hypotheses) == 0 {
		return hypotheses
	}

	base := os.Getenv("HDN_URL")
	if base == "" {
		base = "http://localhost:8081" // Fixed: use correct HDN port
	}
	url := fmt.Sprintf("%s/api/v1/interpret", strings.TrimRight(base, "/"))

	var approved []Hypothesis
	threshold := e.config.Agent.HypothesisScreenThreshold
	if threshold == 0 {
		threshold = 0.6 // Default fallback
	}

	for _, h := range hypotheses {
		prompt := fmt.Sprintf("You are an expert research assistant. Rate the following hypothesis for impact and tractability in domain '%s' on a 0.0-1.0 scale. Respond as JSON: {\"score\": <0-1>, \"reason\": \"...\"}. Hypothesis: %s", domain, h.Description)

		// HDN /api/v1/interpret expects "input" field, not "text"
		payload := map[string]interface{}{"input": prompt}
		data, _ := json.Marshal(payload)

		req, _ := http.NewRequest("POST", url, bytes.NewReader(data))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Content-Type", "application/json")
		if pid, ok := e.context["project_id"].(string); ok && pid != "" {
			req.Header.Set("X-Project-ID", pid)
		}

		resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
		if err != nil {
			log.Printf("⚠️ LLM screening request failed: %v (allowing by default)", err)
			approved = append(approved, h)
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			log.Printf("⚠️ LLM screening status %d: %s (allowing by default)", resp.StatusCode, string(body))
			approved = append(approved, h)
			continue
		}

		// Try to parse JSON { score, reason }
		var out map[string]interface{}
		score := 0.0
		if err := json.Unmarshal(body, &out); err == nil {
			if v, ok := out["score"].(float64); ok {
				score = v
			}
		} else {
			// Fallback: try to extract a number between 0 and 1 from plain text
			s := string(body)
			for i := 0; i < len(s); i++ {
				// quick scan for patterns like 0.7 or 1.0
				if s[i] >= '0' && s[i] <= '9' {
					// Very simple parse; ignore errors
					var val float64
					fmt.Sscanf(s[i:], "%f", &val)
					if val >= 0 && val <= 1 {
						score = val
						break
					}
				}
			}
		}

		if score >= threshold {
			approved = append(approved, h)
		} else {
			log.Printf("🛑 Hypothesis filtered by LLM (score=%.2f < %.2f): %s", score, threshold, h.Description)
		}
	}

	return approved
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

// HypothesisTestResult represents the result of testing a hypothesis
type HypothesisTestResult struct {
	Status     string                   `json:"status"`     // confirmed, refuted, inconclusive
	Confidence float64                  `json:"confidence"` // 0.0 to 1.0
	Evaluation string                   `json:"evaluation"` // human-readable evaluation
	Evidence   []map[string]interface{} `json:"evidence"`   // supporting evidence
}

// gatherHypothesisEvidence gathers evidence to test a hypothesis
func (e *FSMEngine) gatherHypothesisEvidence(hypothesis map[string]interface{}, domain string) ([]map[string]interface{}, error) {
	var evidence []map[string]interface{}

	// Get hypothesis description
	description, ok := hypothesis["description"].(string)
	if !ok {
		return nil, fmt.Errorf("hypothesis missing description")
	}

	// Query knowledge base for related information
	query := fmt.Sprintf("Find information related to: %s", description)
	beliefs, err := e.reasoning.QueryBeliefs(query, domain)
	if err != nil {
		log.Printf("Warning: Failed to query beliefs for hypothesis: %v", err)
	} else {
		// Convert beliefs to evidence
		for _, belief := range beliefs {
			evidence = append(evidence, map[string]interface{}{
				"type":       "belief",
				"content":    belief.Statement,
				"confidence": belief.Confidence,
				"source":     "knowledge_base",
				"relevance":  e.calculateRelevance(description, belief.Statement),
			})
		}
	}

	// Query for contradictory information
	contradictionQuery := fmt.Sprintf("Find information that contradicts: %s", description)
	contradictions, err := e.reasoning.QueryBeliefs(contradictionQuery, domain)
	if err == nil {
		for _, contradiction := range contradictions {
			evidence = append(evidence, map[string]interface{}{
				"type":       "contradiction",
				"content":    contradiction.Statement,
				"confidence": contradiction.Confidence,
				"source":     "knowledge_base",
				"relevance":  e.calculateRelevance(description, contradiction.Statement),
			})
		}
	}

	// If no evidence found, create synthetic evidence for testing
	if len(evidence) == 0 {
		evidence = append(evidence, map[string]interface{}{
			"type":       "synthetic",
			"content":    fmt.Sprintf("No specific evidence found for hypothesis: %s", description),
			"confidence": 0.5,
			"source":     "synthetic",
			"relevance":  0.5,
		})
	}

	return evidence, nil
}

// testHypothesisWithTools tests a hypothesis by creating and using tools
func (e *FSMEngine) testHypothesisWithTools(hypothesis map[string]interface{}, domain string) ([]map[string]interface{}, error) {
	var evidence []map[string]interface{}

	// Get hypothesis description
	description, ok := hypothesis["description"].(string)
	if !ok {
		return nil, fmt.Errorf("hypothesis missing description")
	}

	log.Printf("🔧 Creating tools to test hypothesis: %s", description)

	// Create a tool to test the hypothesis
	toolName := fmt.Sprintf("hypothesis_tester_%d", time.Now().UnixNano())
	toolDescription := fmt.Sprintf("Test the hypothesis: %s", description)

	// Generate tool code using HDN
	toolCode, err := e.generateHypothesisTestingTool(toolName, toolDescription, description, domain)
	if err != nil {
		log.Printf("Warning: Failed to generate tool for hypothesis testing: %v", err)
		// Fallback to knowledge base query
		return e.gatherHypothesisEvidence(hypothesis, domain)
	}

	// Execute the tool
	toolResult, err := e.executeHypothesisTestingTool(toolCode, description, domain)
	if err != nil {
		log.Printf("Warning: Failed to execute hypothesis testing tool: %v", err)
		// Fallback to knowledge base query
		return e.gatherHypothesisEvidence(hypothesis, domain)
	}

	// Convert tool results to evidence
	evidence = append(evidence, map[string]interface{}{
		"type":       "tool_result",
		"content":    toolResult.Result,
		"confidence": toolResult.Confidence,
		"source":     "hypothesis_testing_tool",
		"relevance":  1.0, // Tool was specifically created for this hypothesis
		"tool_name":  toolName,
		"success":    toolResult.Success,
	})

	// Also gather supporting evidence from knowledge base
	knowledgeEvidence, err := e.gatherHypothesisEvidence(hypothesis, domain)
	if err == nil {
		evidence = append(evidence, knowledgeEvidence...)
	}

	return evidence, nil
}

// generateHypothesisTestingTool creates a tool to test a specific hypothesis
func (e *FSMEngine) generateHypothesisTestingTool(toolName, toolDescription, hypothesis, domain string) (string, error) {
	base := os.Getenv("HDN_URL")
	if base == "" {
		base = "http://localhost:8080"
	}
	url := fmt.Sprintf("%s/api/v1/learn/llm", base)

	// Create a prompt for tool generation
	prompt := fmt.Sprintf(`Create a Python tool to test this hypothesis: "%s"

The tool should:
1. Gather relevant data or information
2. Perform analysis or calculations
3. Return a result that supports or refutes the hypothesis
4. Include confidence score (0.0 to 1.0)

Domain: %s
Tool name: %s

Return only the Python code for the tool function.`, hypothesis, domain, toolName)

	payload := map[string]interface{}{
		"task_name":   toolName,
		"description": toolDescription,
		"context": map[string]interface{}{
			"hypothesis": hypothesis,
			"domain":     domain,
			"prompt":     prompt,
		},
		"use_llm": true,
	}

	data, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", url, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	if pid, ok := e.context["project_id"].(string); ok && pid != "" {
		req.Header.Set("X-Project-ID", pid)
	}

	resp, err := (&http.Client{Timeout: 60 * time.Second}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("tool generation failed with status %d", resp.StatusCode)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	// Extract the generated code
	if code, ok := result["code"].(string); ok {
		return code, nil
	}

	return "", fmt.Errorf("no code generated in response")
}

// executeHypothesisTestingTool executes a tool to test a hypothesis
func (e *FSMEngine) executeHypothesisTestingTool(toolCode, hypothesis, domain string) (ToolResult, error) {
	base := os.Getenv("HDN_URL")
	if base == "" {
		base = "http://localhost:8080"
	}
	url := fmt.Sprintf("%s/api/v1/intelligent/execute", base)

	payload := map[string]interface{}{
		"task_name":   "hypothesis_testing",
		"description": fmt.Sprintf("Test hypothesis: %s", hypothesis),
		"language":    "python",
		"context": map[string]interface{}{
			"hypothesis": hypothesis,
			"domain":     domain,
			"code":       toolCode,
		},
		"force_regenerate": false,
	}

	data, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", url, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	if pid, ok := e.context["project_id"].(string); ok && pid != "" {
		req.Header.Set("X-Project-ID", pid)
	}

	resp, err := (&http.Client{Timeout: 120 * time.Second}).Do(req)
	if err != nil {
		return ToolResult{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return ToolResult{}, fmt.Errorf("tool execution failed with status %d", resp.StatusCode)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return ToolResult{}, err
	}

	// Extract result information
	toolResult := ToolResult{
		Success:    true,
		Confidence: 0.5, // Default
		Result:     "Tool executed successfully",
	}

	if success, ok := result["success"].(bool); ok {
		toolResult.Success = success
	}

	if confidence, ok := result["confidence"].(float64); ok {
		toolResult.Confidence = confidence
	}

	if resultText, ok := result["result"].(string); ok {
		toolResult.Result = resultText
	} else if output, ok := result["output"].(string); ok {
		toolResult.Result = output
	}

	return toolResult, nil
}

// ToolResult represents the result of executing a hypothesis testing tool
type ToolResult struct {
	Success    bool    `json:"success"`
	Confidence float64 `json:"confidence"`
	Result     string  `json:"result"`
}

// evaluateHypothesis evaluates a hypothesis based on gathered evidence
func (e *FSMEngine) evaluateHypothesis(hypothesis map[string]interface{}, evidence []map[string]interface{}, domain string) HypothesisTestResult {

	// Calculate overall confidence based on evidence
	totalConfidence := 0.0
	supportingEvidence := 0
	contradictingEvidence := 0

	for _, piece := range evidence {
		confidence, _ := piece["confidence"].(float64)
		relevance, _ := piece["relevance"].(float64)
		evidenceType, _ := piece["type"].(string)

		// Weight by relevance
		weightedConfidence := confidence * relevance
		totalConfidence += weightedConfidence

		if evidenceType == "belief" {
			supportingEvidence++
		} else if evidenceType == "contradiction" {
			contradictingEvidence++
		}
	}

	// Calculate average confidence
	avgConfidence := 0.5 // Default
	if len(evidence) > 0 {
		avgConfidence = totalConfidence / float64(len(evidence))
	}

	// Determine status based on evidence
	var status string
	var evaluation string

	if supportingEvidence > contradictingEvidence && avgConfidence > 0.8 {
		status = "confirmed"
		evaluation = fmt.Sprintf("Hypothesis supported by %d pieces of evidence with %.2f confidence", supportingEvidence, avgConfidence)
	} else if contradictingEvidence > supportingEvidence && avgConfidence < 0.3 {
		status = "refuted"
		evaluation = fmt.Sprintf("Hypothesis contradicted by %d pieces of evidence with %.2f confidence", contradictingEvidence, avgConfidence)
	} else {
		status = "inconclusive"
		evaluation = fmt.Sprintf("Insufficient evidence to confirm or refute hypothesis (supporting: %d, contradicting: %d, confidence: %.2f)", supportingEvidence, contradictingEvidence, avgConfidence)
	}

	return HypothesisTestResult{
		Status:     status,
		Confidence: avgConfidence,
		Evaluation: evaluation,
		Evidence:   evidence,
	}
}

// calculateRelevance calculates how relevant a piece of evidence is to a hypothesis
func (e *FSMEngine) calculateRelevance(hypothesis, evidence string) float64 {
	// Simple keyword-based relevance calculation
	// In a real system, this would use more sophisticated NLP
	hypothesisWords := strings.Fields(strings.ToLower(hypothesis))
	evidenceWords := strings.Fields(strings.ToLower(evidence))

	matches := 0
	for _, hWord := range hypothesisWords {
		for _, eWord := range evidenceWords {
			if hWord == eWord && len(hWord) > 3 { // Only count words longer than 3 chars
				matches++
				break
			}
		}
	}

	if len(hypothesisWords) == 0 {
		return 0.0
	}

	relevance := float64(matches) / float64(len(hypothesisWords))
	if relevance > 1.0 {
		relevance = 1.0
	}

	return relevance
}

// processHypothesisResult handles the consequences of hypothesis testing results
func (e *FSMEngine) processHypothesisResult(hypothesis map[string]interface{}, result HypothesisTestResult, domain string) {
	description := hypothesis["description"].(string)

	log.Printf("🔄 Processing hypothesis result: %s (status: %s, confidence: %.2f)", description, result.Status, result.Confidence)

	switch result.Status {
	case "confirmed":
		e.handleConfirmedHypothesis(hypothesis, result, domain)
	case "refuted":
		e.handleRefutedHypothesis(hypothesis, result, domain)
	case "inconclusive":
		e.handleInconclusiveHypothesis(hypothesis, result, domain)
	default:
		log.Printf("⚠️ Unknown hypothesis status: %s", result.Status)
	}
}

// handleConfirmedHypothesis processes a confirmed hypothesis
func (e *FSMEngine) handleConfirmedHypothesis(hypothesis map[string]interface{}, result HypothesisTestResult, domain string) {
	description := hypothesis["description"].(string)
	hypothesisID := hypothesis["id"].(string)

	log.Printf("✅ Hypothesis confirmed: %s", description)

	// 1. Convert hypothesis to a belief
	belief := Belief{
		ID:         fmt.Sprintf("belief_from_hyp_%s", hypothesisID),
		Statement:  description,
		Confidence: result.Confidence,
		Domain:     domain,
		Source:     "hypothesis_testing",
		Evidence:   []string{hypothesisID},
		CreatedAt:  time.Now(),
		Properties: map[string]interface{}{
			"original_hypothesis_id": hypothesisID,
			"testing_method":         "tool_based",
			"evidence_count":         len(result.Evidence),
		},
	}

	// Store belief in Redis
	beliefKey := fmt.Sprintf("reasoning:beliefs:%s", domain)
	beliefData, _ := json.Marshal(belief)
	e.redis.LPush(e.ctx, beliefKey, beliefData)
	e.redis.LTrim(e.ctx, beliefKey, 0, 199) // Keep last 200 beliefs

	// 2. Update domain knowledge with confirmed hypothesis
	e.updateDomainKnowledgeFromHypothesis(hypothesis, result, domain)

	// 3. Generate workflows from confirmed hypothesis
	e.generateWorkflowsFromHypothesis(hypothesis, result, domain)

	// 4. Generate new hypotheses based on confirmed result
	e.generateFollowUpHypotheses(hypothesis, result, domain, "confirmed")

	// 5. Create learning episode
	episode := map[string]interface{}{
		"id":          fmt.Sprintf("ep_hyp_confirmed_%d", time.Now().UnixNano()),
		"type":        "hypothesis_confirmed",
		"description": fmt.Sprintf("Confirmed hypothesis: %s", description),
		"domain":      domain,
		"confidence":  result.Confidence,
		"evidence":    result.Evidence,
		"timestamp":   time.Now().Format(time.RFC3339),
		"properties": map[string]interface{}{
			"hypothesis_id": hypothesisID,
			"learning_type": "hypothesis_confirmation",
		},
	}

	// Store episode
	episodeKey := e.getRedisKey("episodes")
	episodeData, _ := json.Marshal(episode)
	e.redis.LPush(e.ctx, episodeKey, episodeData)
	e.redis.LTrim(e.ctx, episodeKey, 0, 99)

	log.Printf("📚 Stored confirmed hypothesis as belief and learning episode")
}

// handleRefutedHypothesis processes a refuted hypothesis
func (e *FSMEngine) handleRefutedHypothesis(hypothesis map[string]interface{}, result HypothesisTestResult, domain string) {
	description := hypothesis["description"].(string)
	hypothesisID := hypothesis["id"].(string)

	log.Printf("❌ Hypothesis refuted: %s", description)

	// 1. Store refutation as learning episode
	episode := map[string]interface{}{
		"id":          fmt.Sprintf("ep_hyp_refuted_%d", time.Now().UnixNano()),
		"type":        "hypothesis_refuted",
		"description": fmt.Sprintf("Refuted hypothesis: %s", description),
		"domain":      domain,
		"confidence":  result.Confidence,
		"evidence":    result.Evidence,
		"timestamp":   time.Now().Format(time.RFC3339),
		"properties": map[string]interface{}{
			"hypothesis_id":     hypothesisID,
			"learning_type":     "hypothesis_refutation",
			"refutation_reason": result.Evaluation,
		},
	}

	// Store episode
	episodeKey := e.getRedisKey("episodes")
	episodeData, _ := json.Marshal(episode)
	e.redis.LPush(e.ctx, episodeKey, episodeData)
	e.redis.LTrim(e.ctx, episodeKey, 0, 99)

	// 2. Generate alternative hypotheses
	e.generateFollowUpHypotheses(hypothesis, result, domain, "refuted")

	// 3. Update domain constraints based on refutation
	e.updateDomainConstraintsFromRefutation(hypothesis, result, domain)

	log.Printf("📚 Stored refuted hypothesis as learning episode and generated alternatives")
}

// handleInconclusiveHypothesis processes an inconclusive hypothesis
func (e *FSMEngine) handleInconclusiveHypothesis(hypothesis map[string]interface{}, result HypothesisTestResult, domain string) {
	description := hypothesis["description"].(string)
	hypothesisID := hypothesis["id"].(string)

	log.Printf("❓ Hypothesis inconclusive: %s", description)

	// 1. Store as learning episode for future reference
	episode := map[string]interface{}{
		"id":          fmt.Sprintf("ep_hyp_inconclusive_%d", time.Now().UnixNano()),
		"type":        "hypothesis_inconclusive",
		"description": fmt.Sprintf("Inconclusive hypothesis: %s", description),
		"domain":      domain,
		"confidence":  result.Confidence,
		"evidence":    result.Evidence,
		"timestamp":   time.Now().Format(time.RFC3339),
		"properties": map[string]interface{}{
			"hypothesis_id": hypothesisID,
			"learning_type": "hypothesis_inconclusive",
			"reason":        result.Evaluation,
		},
	}

	// Store episode
	episodeKey := e.getRedisKey("episodes")
	episodeData, _ := json.Marshal(episode)
	e.redis.LPush(e.ctx, episodeKey, episodeData)
	e.redis.LTrim(e.ctx, episodeKey, 0, 99)

	// 2. Generate refined hypotheses with better testing approaches
	e.generateFollowUpHypotheses(hypothesis, result, domain, "inconclusive")

	log.Printf("📚 Stored inconclusive hypothesis for future refinement")
}

// updateDomainKnowledgeFromHypothesis updates domain knowledge with confirmed hypothesis
func (e *FSMEngine) updateDomainKnowledgeFromHypothesis(hypothesis map[string]interface{}, result HypothesisTestResult, domain string) {
	// This would integrate with the knowledge growth engine
	// For now, we'll store it as a domain insight
	insight := map[string]interface{}{
		"type":        "confirmed_hypothesis",
		"description": hypothesis["description"],
		"domain":      domain,
		"confidence":  result.Confidence,
		"evidence":    result.Evidence,
		"timestamp":   time.Now().Format(time.RFC3339),
	}

	insightKey := fmt.Sprintf("fsm:%s:domain_insights", e.agentID)
	insightData, _ := json.Marshal(insight)
	e.redis.LPush(e.ctx, insightKey, insightData)
	e.redis.LTrim(e.ctx, insightKey, 0, 99)
}

// updateDomainConstraintsFromRefutation updates domain constraints based on refuted hypothesis
func (e *FSMEngine) updateDomainConstraintsFromRefutation(hypothesis map[string]interface{}, result HypothesisTestResult, domain string) {
	// Store refutation as a constraint to avoid similar hypotheses
	constraint := map[string]interface{}{
		"type":        "refuted_hypothesis",
		"description": fmt.Sprintf("Avoid similar to: %s", hypothesis["description"]),
		"domain":      domain,
		"reason":      result.Evaluation,
		"timestamp":   time.Now().Format(time.RFC3339),
	}

	constraintKey := fmt.Sprintf("fsm:%s:domain_constraints", e.agentID)
	constraintData, _ := json.Marshal(constraint)
	e.redis.LPush(e.ctx, constraintKey, constraintData)
	e.redis.LTrim(e.ctx, constraintKey, 0, 99)
}

// generateFollowUpHypotheses generates new hypotheses based on testing results
func (e *FSMEngine) generateFollowUpHypotheses(originalHypothesis map[string]interface{}, result HypothesisTestResult, domain, resultType string) {
	// Generate follow-up hypotheses based on the testing result
	description := originalHypothesis["description"].(string)

	var followUpDescriptions []string

	switch resultType {
	case "confirmed":
		// Generate hypotheses that build on the confirmed one
		followUpDescriptions = []string{
			fmt.Sprintf("What are the implications of: %s", description),
			fmt.Sprintf("How can we extend: %s", description),
			fmt.Sprintf("What are the limitations of: %s", description),
		}
	case "refuted":
		// Generate alternative hypotheses
		followUpDescriptions = []string{
			fmt.Sprintf("What is the opposite of: %s", description),
			fmt.Sprintf("What are alternative explanations for the same phenomenon as: %s", description),
			fmt.Sprintf("What are the boundary conditions where: %s might not apply", description),
		}
	case "inconclusive":
		// Generate refined hypotheses with better testing approaches
		followUpDescriptions = []string{
			fmt.Sprintf("How can we better test: %s", description),
			fmt.Sprintf("What additional evidence would support: %s", description),
			fmt.Sprintf("What are the specific conditions for: %s", description),
		}
	}

	// Create follow-up hypotheses
	for i, followUpDesc := range followUpDescriptions {
		followUpHypothesis := Hypothesis{
			ID:          fmt.Sprintf("followup_%s_%d_%d", resultType, time.Now().UnixNano(), i),
			Description: followUpDesc,
			Domain:      domain,
			Confidence:  0.6, // Moderate confidence for follow-ups
			Status:      "proposed",
			Facts:       []string{originalHypothesis["id"].(string)},
			Constraints: []string{"Must follow domain safety principles"},
			CreatedAt:   time.Now(),
		}

		// Store follow-up hypothesis
		key := fmt.Sprintf("fsm:%s:hypotheses", e.agentID)
		hypothesisData := map[string]interface{}{
			"id":                followUpHypothesis.ID,
			"description":       followUpHypothesis.Description,
			"domain":            followUpHypothesis.Domain,
			"confidence":        followUpHypothesis.Confidence,
			"status":            followUpHypothesis.Status,
			"facts":             followUpHypothesis.Facts,
			"constraints":       followUpHypothesis.Constraints,
			"created_at":        followUpHypothesis.CreatedAt.Format(time.RFC3339),
			"parent_hypothesis": originalHypothesis["id"],
			"follow_up_type":    resultType,
		}

		data, _ := json.Marshal(hypothesisData)
		e.redis.HSet(e.ctx, key, followUpHypothesis.ID, data)
	}

	log.Printf("🔬 Generated %d follow-up hypotheses for %s result", len(followUpDescriptions), resultType)
}
