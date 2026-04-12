package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"
)

// logActivity logs a human-readable activity entry to Redis
func (e *FSMEngine) logActivity(message, category string, extras map[string]string) {
	entry := ActivityLogEntry{
		Timestamp: time.Now(),
		Message:   message,
		Category:  category,
		State:     e.currentState,
	}

	if state, ok := extras["state"]; ok && state != "" {
		entry.State = state
	}
	if action, ok := extras["action"]; ok && action != "" {
		entry.Action = action
	}
	if details, ok := extras["details"]; ok && details != "" {
		entry.Details = details
	}

	key := fmt.Sprintf("fsm:%s:activity_log", e.agentID)
	data, err := json.Marshal(entry)
	if err == nil {
		e.redis.LPush(e.ctx, key, data)
		e.redis.LTrim(e.ctx, key, 0, 199)
		e.redis.Expire(e.ctx, key, 7*24*time.Hour)
	}

	log.Printf("📋 [ACTIVITY] %s", message)
}

// pruneContextList ensures context lists don't grow indefinitely, preventing memory leaks and OOM
func (e *FSMEngine) pruneContextList(key string, maxItems int) {
	if val, ok := e.context[key].([]interface{}); ok {
		if len(val) > maxItems {
			e.context[key] = val[len(val)-maxItems:]
			log.Printf("✂️ Pruned context list %s to %d items (was %d)", key, maxItems, len(val))
		}
	} else if val, ok := e.context[key].([]ReasoningTrace); ok {
		if len(val) > maxItems {
			e.context[key] = val[len(val)-maxItems:]
			log.Printf("✂️ Pruned context list %s to %d items (was %d)", key, maxItems, len(val))
		}
	}
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

// Helper methods for knowledge growth
func (e *FSMEngine) getEpisodesFromContext() []map[string]interface{} {

	if episodes, ok := e.context["episodes"].([]map[string]interface{}); ok {
		return episodes
	}

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

	if domain, ok := e.context["current_domain"].(string); ok {
		return domain
	}

	episodes := e.getEpisodesFromContext()
	if len(episodes) > 0 {
		if domain, ok := episodes[0]["domain"].(string); ok {
			return domain
		}
	}

	return "General"
}

// ensureHDNProject creates the project if it does not exist
func (e *FSMEngine) ensureHDNProject(projectID string) {
	base := os.Getenv("HDN_URL")
	if base == "" {
		base = "http://localhost:8080"
	}

	getURL := fmt.Sprintf("%s/api/v1/projects/%s", base, projectID)
	if resp, err := http.Get(getURL); err == nil {
		if resp.Body != nil {
			resp.Body.Close()
		}
		if resp.StatusCode == http.StatusOK {
			return
		}
	}

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
