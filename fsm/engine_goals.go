package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/nats-io/nats.go"
)

// handleGoalCompletion handles goal completion events and triggers explanation learning feedback
func (e *FSMEngine) handleGoalCompletion(msg *nats.Msg) {
	log.Printf("🧠 [EXPLANATION-LEARNING] Received goal completion event: %s", string(msg.Data))

	// Parse goal data from event
	var goalData map[string]interface{}
	if err := json.Unmarshal(msg.Data, &goalData); err != nil {
		log.Printf("⚠️ [EXPLANATION-LEARNING] Failed to parse goal data: %v", err)
		return
	}

	goalID, _ := goalData["id"].(string)
	goalDescription, _ := goalData["description"].(string)
	status, _ := goalData["status"].(string)

	domain := "General"
	if context, ok := goalData["context"].(map[string]interface{}); ok {
		if domainVal, ok := context["domain"].(string); ok && domainVal != "" {
			domain = domainVal
		}
	}

	outcomeMetrics := make(map[string]interface{})
	if metrics, ok := goalData["metrics"].(map[string]interface{}); ok {
		outcomeMetrics = metrics
	}

	outcomeMetrics["status"] = status
	if confidence, ok := goalData["confidence"].(float64); ok {
		outcomeMetrics["confidence"] = confidence
	}

	if e.explanationLearning != nil {
		go func() {

			defer func() {
				if r := recover(); r != nil {
					log.Printf("❌ [EXPLANATION-LEARNING] Panic in evaluation goroutine: %v", r)
				}
			}()

			if err := e.explanationLearning.EvaluateGoalCompletion(
				goalID,
				goalDescription,
				status,
				domain,
				outcomeMetrics,
			); err != nil {
				log.Printf("⚠️ [EXPLANATION-LEARNING] Failed to evaluate goal completion: %v", err)
			}
		}()
	} else {
		log.Printf("⚠️ [EXPLANATION-LEARNING] Explanation learning feedback not initialized")
	}
}

func (e *FSMEngine) handleGoalCreation(msg *nats.Msg) {
	log.Printf("📝 [GOAL-CREATION] Received goal creation event: %s", string(msg.Data))

	var goalData map[string]interface{}
	if err := json.Unmarshal(msg.Data, &goalData); err != nil {
		log.Printf("⚠️ [GOAL-CREATION] Failed to parse goal data: %v", err)
		return
	}

	goalID, ok := goalData["id"].(string)
	if !ok || goalID == "" {
		log.Printf("⚠️ [GOAL-CREATION] Missing or invalid goal ID")
		return
	}

	domain := "General"
	if context, ok := goalData["context"].(map[string]interface{}); ok {
		if domainVal, ok := context["domain"].(string); ok && domainVal != "" {
			domain = domainVal
		}
	}

	goalJSON, _ := json.Marshal(goalData)
	key := fmt.Sprintf("reasoning:curiosity_goals:%s", domain)

	ctx := context.Background()
	if err := e.redis.RPush(ctx, key, string(goalJSON)).Err(); err != nil {
		log.Printf("⚠️ [GOAL-CREATION] Failed to add goal %s to %s: %v", goalID, key, err)
	} else {
		log.Printf("✅ [GOAL-CREATION] Added goal %s to %s (domain: '%s')", goalID, key, domain)
	}
}
