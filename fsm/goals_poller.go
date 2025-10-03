package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

type GoalItem struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Type        string                 `json:"type"`
	Status      string                 `json:"status"`
	Context     map[string]interface{} `json:"context"`
}

func startGoalsPoller(agentID, goalMgrURL string, rdb *redis.Client) {
	ctx := context.Background()
	hdnURL := strings.TrimSpace(os.Getenv("HDN_URL"))
	if hdnURL == "" {
		hdnURL = "http://localhost:8080"
	}

	triggeredKey := fmt.Sprintf("fsm:%s:goals:triggered", agentID)

	client := &http.Client{Timeout: 10 * time.Second}

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// Pause guard: suspend auto goal triggering when manual executions are running
			if paused, err := rdb.Get(ctx, "auto_executor:paused").Result(); err == nil && strings.TrimSpace(paused) == "1" {
				log.Printf("[FSM][Goals] Auto-executor paused by Redis flag; skipping tick")
				continue
			}
			// Fetch active goals for this agent
			url := fmt.Sprintf("%s/goals/%s/active", goalMgrURL, agentID)
			resp, err := client.Get(url)
			if err != nil {
				log.Printf("[FSM][Goals] fetch active goals error: %v", err)
				continue
			}
			var payload any
			if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
				resp.Body.Close()
				log.Printf("[FSM][Goals] decode goals error: %v", err)
				continue
			}
			resp.Body.Close()

			var goals []GoalItem
			switch v := payload.(type) {
			case []interface{}:
				// slice of goals
				b, _ := json.Marshal(v)
				_ = json.Unmarshal(b, &goals)
			case map[string]interface{}:
				if arr, ok := v["goals"]; ok {
					b, _ := json.Marshal(arr)
					_ = json.Unmarshal(b, &goals)
				}
			}

			if len(goals) == 0 {
				continue
			}

			triggeredCount := 0
			for _, g := range goals {
				if g.ID == "" {
					continue
				}
				// Skip if already triggered
				exists, _ := rdb.SIsMember(ctx, triggeredKey, g.ID).Result()
				if exists {
					continue
				}

				// Build hierarchical execute payload
				// Use goal description/name as the user_request; pass identifiers in context
				req := map[string]interface{}{
					"task_name":    "Goal Execution",
					"description":  firstNonEmpty(g.Description, g.Name, "Execute goal"),
					"user_request": firstNonEmpty(g.Description, g.Name),
					"context": map[string]string{
						"session_id": fmt.Sprintf("goal_%s", g.ID),
						"goal_id":    g.ID,
						"agent_id":   agentID,
						"project_id": "Goals",
					},
				}
				b, _ := json.Marshal(req)
				execURL := strings.TrimRight(hdnURL, "/") + "/api/v1/hierarchical/execute"
				eresp, err := client.Post(execURL, "application/json", strings.NewReader(string(b)))
				if err != nil {
					log.Printf("[FSM][Goals] execute error for goal %s: %v", g.ID, err)
					continue
				}
				if eresp.Body != nil {
					eresp.Body.Close()
				}
				if eresp.StatusCode >= 200 && eresp.StatusCode < 300 {
					// Record as triggered to prevent duplicate execution
					_ = rdb.SAdd(ctx, triggeredKey, g.ID).Err()
					// Optional: set TTL so if something stalls we can retry later
					_ = rdb.Expire(ctx, triggeredKey, 12*time.Hour).Err()
					log.Printf("[FSM][Goals] triggered goal %s", g.ID)
					triggeredCount++
					// Limit to 3 goals triggered per tick to improve throughput
					if triggeredCount >= 3 {
						break
					}
				} else {
					log.Printf("[FSM][Goals] execute failed for goal %s (status %d)", g.ID, eresp.StatusCode)
				}
			}

		}
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
