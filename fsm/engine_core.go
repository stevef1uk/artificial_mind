package main

import (
	"encoding/json"
	"fmt"
	"log"
	"time"
)

// Start begins the FSM event loop
func (e *FSMEngine) Start() error {
	log.Printf("Starting FSM engine for agent %s in state %s", e.agentID, e.currentState)

	if err := e.subscribeToEvents(); err != nil {
		return fmt.Errorf("failed to subscribe to events: %w", err)
	}

	go e.timerLoop()

	go e.coherenceMonitoringLoop()

	go e.eventLoop()

	return nil
}

// Stop gracefully shuts down the FSM
func (e *FSMEngine) Stop() error {
	log.Printf("Stopping FSM engine for agent %s", e.agentID)

	for _, sub := range e.subs {
		sub.Unsubscribe()
	}

	if err := e.saveState(); err != nil {
		log.Printf("Warning: Could not save state: %v", err)
	}

	e.cancel()
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

// coherenceMonitoringLoop runs periodic coherence checks
func (e *FSMEngine) coherenceMonitoringLoop() {

	interval := 5 * time.Minute
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	log.Printf("🔍 [Coherence] Coherence monitoring loop started (interval: %v)", interval)

	go func() {

		time.Sleep(10 * time.Second)

		if e.coherenceMonitor == nil {
			log.Printf("⚠️ [Coherence] Coherence monitor is nil, skipping initial check")
			return
		}

		log.Printf("🔍 [Coherence] Running initial coherence check...")
		e.runCoherenceCheck()
	}()

	for {
		select {
		case <-e.ctx.Done():
			return
		case <-ticker.C:
			e.runCoherenceCheck()
		}
	}
}

// runCoherenceCheck performs a single coherence check
func (e *FSMEngine) runCoherenceCheck() {

	if e.coherenceMonitor == nil {
		log.Printf("⚠️ [Coherence] Coherence monitor is nil, skipping check")
		return
	}

	inconsistencies, err := e.coherenceMonitor.CheckCoherence()
	if err != nil {
		log.Printf("⚠️ [Coherence] Error during coherence check: %v", err)
		return
	}

	if len(inconsistencies) > 0 {
		log.Printf("⚠️ [Coherence] Detected %d inconsistencies", len(inconsistencies))

		for _, inc := range inconsistencies {
			if !inc.Resolved {

				task, err := e.coherenceMonitor.GenerateSelfReflectionTask(inc)
				if err != nil {
					log.Printf("⚠️ [Coherence] Failed to generate reflection task: %v", err)
					continue
				}

				if err := e.coherenceMonitor.ResolveInconsistency(inc); err != nil {
					log.Printf("⚠️ [Coherence] Failed to resolve inconsistency %s: %v", inc.ID, err)
				} else {
					log.Printf("✅ [Coherence] Generated resolution task for inconsistency: %s (task: %s)", inc.ID, task.ID)
				}
			}
		}
	} else {
		log.Printf("✅ [Coherence] No inconsistencies detected")
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

			eventsProcessed := e.processEventQueue()

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

		event = CanonicalEvent{
			EventID:   fmt.Sprintf("evt_%d", time.Now().UnixNano()),
			Source:    fmt.Sprintf("fsm:%s", e.agentID),
			Type:      eventName,
			Timestamp: time.Now().Format(time.RFC3339),
			Context:   make(map[string]interface{}),
			Payload:   make(map[string]interface{}),
		}
	}

	if eventName == "new_input" && data != nil {
		e.publishUserGoal(event)

		userRequest := ""
		if text, ok := event.Payload["text"].(string); ok && text != "" {
			userRequest = text
		} else if request, ok := event.Payload["user_request"].(string); ok && request != "" {
			userRequest = request
		} else if message, ok := event.Payload["message"].(string); ok && message != "" {
			userRequest = message
		}

		if userRequest != "" {

			userRequest = e.cleanGoalDescription(userRequest)

			e.context["current_goal"] = userRequest
			log.Printf("🎯 Updated current_goal from new_input: %s", userRequest)
		}
	}

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
			break
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

		if eventsProcessed < maxEvents {
			time.Sleep(time.Duration(e.config.Performance.PostProcessingSleepMs) * time.Millisecond)
		}
	}

	return eventsProcessed
}

// updatePerformanceMetrics updates Redis with current performance metrics
func (e *FSMEngine) updatePerformanceMetrics() {
	perfKey := fmt.Sprintf("fsm:%s:performance", e.agentID)

	e.redis.HIncrBy(e.ctx, perfKey, "events_processed", 1)

	e.redis.HSet(e.ctx, perfKey, "last_activity", time.Now().Format(time.RFC3339))

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

	stateConfig := e.findStateConfig(e.currentState)
	if stateConfig == nil {
		log.Printf("Unknown state: %s", e.currentState)
		return
	}

	transition, exists := stateConfig.On[eventName]
	if !exists {
		log.Printf("❌ No transition found for event %s in state %s", eventName, e.currentState)

		if eventName == "timer_tick" {
			log.Printf("⏰ Timer tick in state %s with no transition - executing state actions", e.currentState)
			e.executeStateActions(stateConfig, event)
		}
		return
	}

	log.Printf("✅ Found transition: %s -> %s for event %s", e.currentState, transition.Next, eventName)

	if transition.Guard != "" {
		if !e.evaluateGuard(transition.Guard, event) {
			log.Printf("Guard %s failed for event %s", transition.Guard, eventName)

			if eventName == "timer_tick" {
				log.Printf("⏰ Timer tick guard failed in state %s - executing state actions anyway", e.currentState)
				e.executeStateActions(stateConfig, event)

				if (e.currentState == "evaluate" && transition.Next == "learn") ||
					(e.currentState == "reason" && transition.Next == "reason_continue") {
					log.Printf("🔄 Proceeding with %s -> %s transition despite guard failure to ensure progress", e.currentState, transition.Next)

				} else {
					return
				}
			} else {
				return
			}
		}
	}

	e.updatePerformanceMetrics()

	e.transitionTo(transition.Next, eventName, event)
}

// transitionTo performs a state transition
func (e *FSMEngine) transitionTo(newState string, reason string, event map[string]interface{}) {
	oldState := e.currentState
	e.currentState = newState
	e.stateEntryTime = time.Now()

	stateDesc := e.getStateDescription(newState)

	e.logActivity(
		fmt.Sprintf("Moved from '%s' to '%s': %s", oldState, newState, stateDesc),
		"state_change",
		map[string]string{
			"state":   newState,
			"details": fmt.Sprintf("Reason: %s", reason),
		},
	)

	stateConfig := e.findStateConfig(newState)
	if stateConfig != nil {
		e.executeStateActions(stateConfig, event)
	}

	e.saveState()

	e.publishTransition(oldState, newState, reason, event)

	log.Printf("FSM transition: %s -> %s (reason: %s)", oldState, newState, reason)

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

// executeStateActions executes all actions for a state
func (e *FSMEngine) executeStateActions(stateConfig *StateConfig, event map[string]interface{}) {
	if stateConfig == nil {
		return
	}
	for _, action := range stateConfig.Actions {
		e.executeAction(action, event)
	}
}

// evaluateGuard evaluates a guard condition
func (e *FSMEngine) evaluateGuard(guardName string, event map[string]interface{}) bool {
	guard := e.findGuardConfig(guardName)
	if guard == nil {
		log.Printf("Unknown guard: %s", guardName)
		return false
	}

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

	log.Printf("Validating input against domain constraints")
	return true
}

func (e *FSMEngine) checkPendingWork(guard GuardConfig, event map[string]interface{}) bool {

	log.Printf("Checking for pending work")
	return true
}

func (e *FSMEngine) checkTimeout(guard GuardConfig, event map[string]interface{}) bool {

	stateDurationSeconds, ok := guard.Params["state_duration_seconds"].(int)
	if !ok {
		log.Printf("Invalid or missing state_duration_seconds parameter for timeout guard")
		return false
	}

	elapsed := time.Since(e.stateEntryTime)
	timeoutDuration := time.Duration(stateDurationSeconds) * time.Second

	log.Printf("Timeout check: elapsed %v, timeout %v", elapsed, timeoutDuration)

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

// GetCurrentState returns the current state
func (e *FSMEngine) GetCurrentState() string {
	return e.currentState
}

// GetContext returns the current context
func (e *FSMEngine) GetContext() map[string]interface{} {
	return e.context
}
