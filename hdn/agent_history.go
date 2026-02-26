package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
)

// AgentExecution represents a single agent execution record
type AgentExecution struct {
	ID          string                 `json:"id"`
	AgentID     string                 `json:"agent_id"`
	Input       string                 `json:"input"`
	Status      string                 `json:"status"` // "success", "error", "running"
	Result      interface{}            `json:"result,omitempty"`
	Error       string                 `json:"error,omitempty"`
	Duration    time.Duration          `json:"duration"`
	ToolCalls   []ToolCall             `json:"tool_calls,omitempty"`
	StartedAt   time.Time              `json:"started_at"`
	CompletedAt *time.Time             `json:"completed_at,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// AgentHistory manages execution history for agents
type AgentHistory struct {
	redisClient *redis.Client
	ctx         context.Context
}

// NewAgentHistory creates a new agent history manager
func NewAgentHistory(redisClient *redis.Client) *AgentHistory {
	return &AgentHistory{
		redisClient: redisClient,
		ctx:         context.Background(),
	}
}

// RecordExecution records an agent execution
func (h *AgentHistory) RecordExecution(execution *AgentExecution) error {
	// Generate ID if not set
	if execution.ID == "" {
		execution.ID = fmt.Sprintf("%s-%d", execution.AgentID, time.Now().UnixNano())
	}

	// Set timestamps
	if execution.StartedAt.IsZero() {
		execution.StartedAt = time.Now()
	}
	if execution.CompletedAt == nil && execution.Status != "running" {
		now := time.Now()
		execution.CompletedAt = &now
	}

	// Serialize execution
	data, err := json.Marshal(execution)
	if err != nil {
		return fmt.Errorf("failed to marshal execution: %w", err)
	}

	// Store in Redis with TTL of 30 days
	key := fmt.Sprintf("agent:execution:%s:%s", execution.AgentID, execution.ID)
	ttl := 30 * 24 * time.Hour

	if err := h.redisClient.Set(h.ctx, key, data, ttl).Err(); err != nil {
		return fmt.Errorf("failed to store execution: %w", err)
	}

	// Add to agent's execution list (sorted set by timestamp for easy retrieval)
	listKey := fmt.Sprintf("agent:executions:%s", execution.AgentID)
	score := float64(execution.StartedAt.Unix())
	if err := h.redisClient.ZAdd(h.ctx, listKey, redis.Z{
		Score:  score,
		Member: execution.ID,
	}).Err(); err != nil {
		log.Printf("⚠️ [AGENT-HISTORY] Failed to add to execution list: %v", err)
	}

	// Set TTL on the list as well
	h.redisClient.Expire(h.ctx, listKey, ttl)

	log.Printf("📝 [AGENT-HISTORY] Recorded execution %s for agent %s (status: %s)",
		execution.ID, execution.AgentID, execution.Status)

	return nil
}

// GetExecutions retrieves execution history for an agent
func (h *AgentHistory) GetExecutions(agentID string, limit int) ([]*AgentExecution, error) {
	if limit <= 0 {
		limit = 50 // Default limit
	}

	listKey := fmt.Sprintf("agent:executions:%s", agentID)

	// Get execution IDs from sorted set (most recent first)
	ids, err := h.redisClient.ZRevRange(h.ctx, listKey, 0, int64(limit-1)).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get execution IDs: %w", err)
	}

	if len(ids) == 0 {
		return []*AgentExecution{}, nil
	}

	// Fetch all executions in bulk using MGet
	keys := make([]string, len(ids))
	for i, id := range ids {
		keys[i] = fmt.Sprintf("agent:execution:%s:%s", agentID, id)
	}

	results, err := h.redisClient.MGet(h.ctx, keys...).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to MGet executions: %w", err)
	}

	executions := make([]*AgentExecution, 0, len(results))
	for i, res := range results {
		if res == nil {
			log.Printf("⚠️ [AGENT-HISTORY] Execution data missing for %s", ids[i])
			continue
		}

		str, ok := res.(string)
		if !ok {
			// Redis go-redis v9 return interface{} which is usually string or []byte
			if bytes, ok := res.([]byte); ok {
				str = string(bytes)
			} else {
				log.Printf("⚠️ [AGENT-HISTORY] Unexpected type for execution data: %T", res)
				continue
			}
		}

		var execution AgentExecution
		if err := json.Unmarshal([]byte(str), &execution); err != nil {
			log.Printf("⚠️ [AGENT-HISTORY] Failed to unmarshal execution %s: %v", ids[i], err)
			continue
		}

		executions = append(executions, &execution)
	}

	return executions, nil
}

// GetExecution retrieves a specific execution by ID
func (h *AgentHistory) GetExecution(agentID string, executionID string) (*AgentExecution, error) {
	key := fmt.Sprintf("agent:execution:%s:%s", agentID, executionID)
	data, err := h.redisClient.Get(h.ctx, key).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, fmt.Errorf("execution not found")
		}
		return nil, fmt.Errorf("failed to get execution: %w", err)
	}

	var execution AgentExecution
	if err := json.Unmarshal([]byte(data), &execution); err != nil {
		return nil, fmt.Errorf("failed to unmarshal execution: %w", err)
	}

	return &execution, nil
}

// GetAgentStats returns statistics for an agent
func (h *AgentHistory) GetAgentStats(agentID string) (map[string]interface{}, error) {
	listKey := fmt.Sprintf("agent:executions:%s", agentID)

	// Get total count
	count, err := h.redisClient.ZCard(h.ctx, listKey).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get execution count: %w", err)
	}

	// Get recent executions for stats
	executions, err := h.GetExecutions(agentID, 100)
	if err != nil {
		return nil, err
	}

	successCount := 0
	errorCount := 0
	var totalDuration time.Duration

	for _, exec := range executions {
		if exec.Status == "success" {
			successCount++
		} else if exec.Status == "error" {
			errorCount++
		}
		totalDuration += exec.Duration
	}

	avgDuration := time.Duration(0)
	if len(executions) > 0 {
		avgDuration = totalDuration / time.Duration(len(executions))
	}

	successRate := 0.0
	if count > 0 {
		successRate = float64(successCount) / float64(count) * 100
	}

	return map[string]interface{}{
		"total_executions": count,
		"success_count":    successCount,
		"error_count":      errorCount,
		"success_rate":     successRate,
		"avg_duration":     avgDuration.String(),
		"last_execution":   getLastExecutionTime(executions),
	}, nil
}

func getLastExecutionTime(executions []*AgentExecution) *time.Time {
	if len(executions) == 0 {
		return nil
	}
	return &executions[0].StartedAt
}
