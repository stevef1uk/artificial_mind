package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/redis/go-redis/v9"
)

type GoalManagerClient struct {
	baseURL string
	client  *http.Client
	redis   *redis.Client
}

func NewGoalManagerClient(baseURL string, rc *redis.Client) *GoalManagerClient {
	return &GoalManagerClient{
		baseURL: baseURL,
		client: &http.Client{Timeout: 10 * time.Second},
		redis:   rc,
	}
}

func (gmc *GoalManagerClient) PostCuriosityGoal(goal CuriosityGoal, source string) error {
	if gmc.baseURL == "" {
		return fmt.Errorf("goal manager URL not configured")
	}

	goalRequest := map[string]interface{}{
		"id":          goal.ID,
		"agent_id":    "agent_1",
		"description": goal.Description,
		"priority":    fmt.Sprintf("%d", goal.Priority),
		"status":      goal.Status,
		"confidence":  goal.Value,
		"context": map[string]interface{}{
			"domain":       goal.Domain,
			"source":       source,
		},
	}

	if len(goal.Targets) > 0 {
		goalRequest["context"].(map[string]interface{})["targets"] = goal.Targets
	}

	body, err := json.Marshal(goalRequest)
	if err != nil {
		log.Printf("⚠️ [GOAL-MGR] Failed to marshal goal %s: %v", goal.ID, err)
		return err
	}

	resp, err := gmc.client.Post(
		gmc.baseURL+"/goal",
		"application/json",
		bytes.NewReader(body),
	)

	if err != nil {
		log.Printf("⚠️ [GOAL-MGR] Failed to POST goal %s to Goal Manager: %v", goal.ID, err)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		log.Printf("⚠️ [GOAL-MGR] Goal Manager returned status %d for goal %s (source: %s)", resp.StatusCode, goal.ID, source)
		return fmt.Errorf("goal manager returned status %d", resp.StatusCode)
	}

	log.Printf("✅ [GOAL-MGR] Posted goal %s to Goal Manager (source: %s, domain: %s)", goal.ID, source, goal.Domain)
	return nil
}

func (gmc *GoalManagerClient) IsGoalAlreadyInManager(ctx context.Context, goalID string) bool {
	if gmc.redis == nil {
		return false
	}

	exists := gmc.redis.SIsMember(ctx, "goals:agent_1:active", goalID).Val()
	return exists
}
