package main

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
)

// subscribeToEvents sets up NATS subscriptions
func (e *FSMEngine) subscribeToEvents() error {
	log.Printf("🔌 Setting up NATS subscriptions for %d events", len(e.config.Events))
	for _, event := range e.config.Events {
		if event.NatsSubject == "" {
			log.Printf("⚠️  Skipping event %s - no NATS subject", event.Name)
			continue
		}

		log.Printf("📡 Subscribing to NATS subject: %s (event: %s)", event.NatsSubject, event.Name)
		eventName := event.Name
		sub, err := e.nc.Subscribe(event.NatsSubject, func(msg *nats.Msg) {
			log.Printf("📨 Received NATS event on %s: %s", event.NatsSubject, string(msg.Data))

			if eventName == "news_relations" || eventName == "news_alerts" {
				var canonicalEvent CanonicalEvent
				if err := json.Unmarshal(msg.Data, &canonicalEvent); err == nil {

					eventMap := map[string]interface{}{
						"type":    canonicalEvent.Type,
						"payload": canonicalEvent.Payload,
					}

					e.storeNewsEventInWeaviate(eventMap)
					log.Printf("📰 Stored news event immediately (bypassing state machine): %s", eventName)
				} else {
					log.Printf("⚠️ Failed to parse news event for immediate storage: %v", err)
				}
			}

			e.handleEvent(eventName, msg.Data)
		})
		if err != nil {
			return fmt.Errorf("failed to subscribe to %s: %w", event.NatsSubject, err)
		}

		e.subs = append(e.subs, sub)
		log.Printf("✅ Successfully subscribed to %s", event.NatsSubject)
	}

	log.Printf("📡 Subscribing to goal completion events for explanation learning")
	achievedSub, err := e.nc.Subscribe("agi.goal.achieved", e.handleGoalCompletion)
	if err != nil {
		log.Printf("⚠️  Failed to subscribe to agi.goal.achieved: %v", err)
	} else {
		e.subs = append(e.subs, achievedSub)
		log.Printf("✅ Subscribed to agi.goal.achieved for explanation learning")
	}

	failedSub, err := e.nc.Subscribe("agi.goal.failed", e.handleGoalCompletion)
	if err != nil {
		log.Printf("⚠️  Failed to subscribe to agi.goal.failed: %v", err)
	} else {
		e.subs = append(e.subs, failedSub)
		log.Printf("✅ Subscribed to agi.goal.failed for explanation learning")
	}

	createdSub, err := e.nc.Subscribe("agi.goal.created", e.handleGoalCreation)
	if err != nil {
		log.Printf("⚠️  Failed to subscribe to agi.goal.created: %v", err)
	} else {
		e.subs = append(e.subs, createdSub)
		log.Printf("✅ Subscribed to agi.goal.created for goal tracking")
	}

	log.Printf("🎉 NATS subscription setup complete - %d subscriptions active", len(e.subs))
	return nil
}

// convertToCanonicalEvent converts a news event to the canonical format expected by HDN
func (e *FSMEngine) convertToCanonicalEvent(event map[string]interface{}) *CanonicalEvent {

	eventType, ok := event["type"].(string)
	if !ok {
		return nil
	}

	payload, ok := event["payload"].(map[string]interface{})
	if !ok {
		return nil
	}

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

	evaluationEvent := map[string]interface{}{
		"agent_id":      e.agentID,
		"timestamp":     time.Now().Unix(),
		"source":        "fsm.outcome_analyzer",
		"domain":        e.getCurrentDomain(),
		"current_state": e.currentState,
		"context":       e.context,
	}

	if metrics, ok := e.context["performance_metrics"].(map[string]interface{}); ok {
		evaluationEvent["metrics"] = metrics
	}

	if errorRate, ok := e.context["error_rate"].(float64); ok {
		evaluationEvent["error_rate"] = errorRate
	}

	data, _ := json.Marshal(evaluationEvent)
	e.nc.Publish("agi.evaluation.result", data)
	log.Printf("📤 Published evaluation result to Goals server")
}

// publishUserGoal publishes user goals to Goals server
func (e *FSMEngine) publishUserGoal(event CanonicalEvent) {

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

	userRequest = e.cleanGoalDescription(userRequest)

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

// cleanGoalDescription removes verbose warning messages and technical instructions from goal descriptions
func (e *FSMEngine) cleanGoalDescription(desc string) string {

	warningStart := strings.Index(desc, "🚨 CRITICAL FOR PYTHON - READING CONTEXT PARAMETERS:")
	if warningStart == -1 {

		warningStart = strings.Index(desc, "CRITICAL FOR PYTHON - READING CONTEXT PARAMETERS:")
	}

	if warningStart > 0 {

		desc = desc[:warningStart]
		desc = strings.TrimSpace(desc)
	}

	descTrimmed := strings.TrimSpace(desc)
	if strings.HasPrefix(descTrimmed, "Execute capability:") {

		parts := strings.SplitN(descTrimmed, "\n", 2)
		capPart := strings.TrimSpace(parts[0])

		for strings.HasPrefix(capPart, "Execute capability: Execute capability:") {
			capPart = strings.TrimPrefix(capPart, "Execute capability: ")
			capPart = strings.TrimSpace(capPart)
		}

		if strings.HasPrefix(capPart, "Execute capability:") && !strings.Contains(capPart, "CRITICAL") && !strings.Contains(capPart, "🚨") {

			capabilityID := strings.TrimPrefix(capPart, "Execute capability:")
			capabilityID = strings.TrimSpace(capabilityID)

			if strings.HasPrefix(capabilityID, "code_") {

				if e.redis != nil {
					codeKey := fmt.Sprintf("code:%s", capabilityID)
					if codeData, err := e.redis.Get(e.ctx, codeKey).Result(); err == nil {
						var capability struct {
							TaskName    string `json:"task_name"`
							Description string `json:"description"`
						}
						if err := json.Unmarshal([]byte(codeData), &capability); err == nil {

							if capability.TaskName != "" {
								desc = fmt.Sprintf("Execute: %s", capability.TaskName)
							} else if capability.Description != "" {

								capDesc := strings.TrimSpace(capability.Description)

								for strings.HasPrefix(capDesc, "Execute capability:") {
									capDesc = strings.TrimPrefix(capDesc, "Execute capability:")
									capDesc = strings.TrimSpace(capDesc)
								}
								desc = capDesc
							} else {

								desc = capabilityID
							}
						}
					}
				}
			}
		}
	}

	desc = strings.TrimSpace(desc)

	if len(desc) > 500 {
		desc = desc[:497] + "..."
	}

	return desc
}
