package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/redis/go-redis/v9"
)

// ToolCallLog represents a single tool call with all relevant information
type ToolCallLog struct {
	ID          string                 `json:"id"`
	ToolID      string                 `json:"tool_id"`
	ToolName    string                 `json:"tool_name"`
	Parameters  map[string]interface{} `json:"parameters"`
	Status      string                 `json:"status"` // "success", "failure", "blocked"
	Error       string                 `json:"error,omitempty"`
	Duration    int64                  `json:"duration_ms"`
	AgentID     string                 `json:"agent_id"`
	ProjectID   string                 `json:"project_id"`
	Timestamp   time.Time              `json:"timestamp"`
	Response    interface{}            `json:"response,omitempty"`
	Permissions []string               `json:"permissions"`
	SafetyLevel string                 `json:"safety_level"`
}

// ToolMetrics represents aggregated metrics for a tool
type ToolMetrics struct {
	ToolID       string    `json:"tool_id"`
	ToolName     string    `json:"tool_name"`
	TotalCalls   int64     `json:"total_calls"`
	SuccessCalls int64     `json:"success_calls"`
	FailureCalls int64     `json:"failure_calls"`
	BlockedCalls int64     `json:"blocked_calls"`
	AvgDuration  float64   `json:"avg_duration_ms"`
	LastCalled   time.Time `json:"last_called"`
	CreatedAt    time.Time `json:"created_at"`
}

// ToolMetricsManager handles logging and metrics for tool calls
type ToolMetricsManager struct {
	redisClient *redis.Client
	logFile     *os.File
	logPath     string
}

// NewToolMetricsManager creates a new tool metrics manager
func NewToolMetricsManager(redisAddr string, logDir string) (*ToolMetricsManager, error) {
	// Ensure log directory exists
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create log directory: %v", err)
	}

	// Create log file with timestamp
	logPath := filepath.Join(logDir, fmt.Sprintf("tool_calls_%s.log", time.Now().Format("2006-01-02")))
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to create log file: %v", err)
	}

	// Initialize Redis client
	redisClient := redis.NewClient(&redis.Options{
		Addr: redisAddr,
	})

	return &ToolMetricsManager{
		redisClient: redisClient,
		logFile:     logFile,
		logPath:     logPath,
	}, nil
}

// LogToolCall logs a tool call to both file and Redis
func (tmm *ToolMetricsManager) LogToolCall(ctx context.Context, call *ToolCallLog) error {
	// Generate unique ID if not provided
	if call.ID == "" {
		call.ID = fmt.Sprintf("tool_call_%d", time.Now().UnixNano())
	}

	// Set timestamp if not provided
	if call.Timestamp.IsZero() {
		call.Timestamp = time.Now()
	}

	// Log to file
	logEntry, err := json.Marshal(call)
	if err != nil {
		return fmt.Errorf("failed to marshal tool call log: %v", err)
	}

	// Write to log file with timestamp prefix
	logLine := fmt.Sprintf("[%s] %s\n", call.Timestamp.Format(time.RFC3339), string(logEntry))
	if _, err := tmm.logFile.WriteString(logLine); err != nil {
		log.Printf("Warning: failed to write to tool call log file: %v", err)
	}

	// Store in Redis for real-time metrics
	if err := tmm.updateRedisMetrics(ctx, call); err != nil {
		log.Printf("⚠️ [METRICS] Failed to update Redis metrics for tool %s: %v", call.ToolID, err)
		log.Printf("⚠️ [METRICS] Redis client status: %+v", tmm.redisClient)
	} else {
		log.Printf("✅ [METRICS] Successfully updated Redis metrics for tool %s", call.ToolID)
	}

	return nil
}

// updateRedisMetrics updates the metrics in Redis
func (tmm *ToolMetricsManager) updateRedisMetrics(ctx context.Context, call *ToolCallLog) error {
	// Update tool-specific metrics
	metricsKey := fmt.Sprintf("tool_metrics:%s", call.ToolID)

	// Get current metrics
	var metrics ToolMetrics
	metricsData, err := tmm.redisClient.Get(ctx, metricsKey).Result()
	if err == nil {
		json.Unmarshal([]byte(metricsData), &metrics)
	} else {
		// Initialize new metrics
		metrics = ToolMetrics{
			ToolID:    call.ToolID,
			ToolName:  call.ToolName,
			CreatedAt: call.Timestamp,
		}
	}

	// Update metrics
	metrics.TotalCalls++
	metrics.LastCalled = call.Timestamp

	switch call.Status {
	case "success":
		metrics.SuccessCalls++
	case "failure":
		metrics.FailureCalls++
	case "blocked":
		metrics.BlockedCalls++
	}
	
	// Log metrics update for debugging
	log.Printf("📊 [METRICS] Updated metrics for tool %s: total=%d, success=%d, failure=%d, blocked=%d", 
		call.ToolID, metrics.TotalCalls, metrics.SuccessCalls, metrics.FailureCalls, metrics.BlockedCalls)

	// Update average duration
	if call.Duration > 0 {
		if metrics.AvgDuration == 0 {
			metrics.AvgDuration = float64(call.Duration)
		} else {
			// Simple moving average
			metrics.AvgDuration = (metrics.AvgDuration*float64(metrics.TotalCalls-1) + float64(call.Duration)) / float64(metrics.TotalCalls)
		}
	}

	// Store updated metrics
	metricsBytes, err := json.Marshal(metrics)
	if err != nil {
		return fmt.Errorf("failed to marshal metrics: %v", err)
	}

	err = tmm.redisClient.Set(ctx, metricsKey, metricsBytes, 0).Err()
	if err != nil {
		log.Printf("⚠️ [METRICS] Redis Set failed for key %s: %v", metricsKey, err)
		log.Printf("⚠️ [METRICS] Redis client options: Addr=%v", tmm.redisClient.Options().Addr)
		return fmt.Errorf("failed to store metrics in Redis: %v", err)
	}
	log.Printf("✅ [METRICS] Stored metrics in Redis key: %s", metricsKey)

	// Store individual call log in Redis (with TTL for cleanup)
	callKey := fmt.Sprintf("tool_call:%s", call.ID)
	callData, err := json.Marshal(call)
	if err != nil {
		return fmt.Errorf("failed to marshal call log: %v", err)
	}

	// Store call log with 7-day TTL
	err = tmm.redisClient.Set(ctx, callKey, callData, 7*24*time.Hour).Err()
	if err != nil {
		return fmt.Errorf("failed to store call log in Redis: %v", err)
	}

	// Add to recent calls list (keep last 100)
	recentKey := "tool_calls:recent"
	err = tmm.redisClient.LPush(ctx, recentKey, callData).Err()
	if err != nil {
		return fmt.Errorf("failed to add to recent calls: %v", err)
	}

	// Trim to last 100 calls
	tmm.redisClient.LTrim(ctx, recentKey, 0, 99)

	return nil
}

// GetToolMetrics retrieves metrics for a specific tool
func (tmm *ToolMetricsManager) GetToolMetrics(ctx context.Context, toolID string) (*ToolMetrics, error) {
	metricsKey := fmt.Sprintf("tool_metrics:%s", toolID)
	metricsData, err := tmm.redisClient.Get(ctx, metricsKey).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, fmt.Errorf("no metrics found for tool %s", toolID)
		}
		return nil, fmt.Errorf("failed to get metrics from Redis: %v", err)
	}

	var metrics ToolMetrics
	err = json.Unmarshal([]byte(metricsData), &metrics)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal metrics: %v", err)
	}

	return &metrics, nil
}

// GetAllToolMetrics retrieves metrics for all tools
func (tmm *ToolMetricsManager) GetAllToolMetrics(ctx context.Context) ([]ToolMetrics, error) {
	// Get all tool metrics keys
	keys, err := tmm.redisClient.Keys(ctx, "tool_metrics:*").Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get tool metrics keys: %v", err)
	}

	var allMetrics []ToolMetrics
	for _, key := range keys {
		metricsData, err := tmm.redisClient.Get(ctx, key).Result()
		if err != nil {
			log.Printf("Warning: failed to get metrics for key %s: %v", key, err)
			continue
		}

		var metrics ToolMetrics
		err = json.Unmarshal([]byte(metricsData), &metrics)
		if err != nil {
			log.Printf("Warning: failed to unmarshal metrics for key %s: %v", key, err)
			continue
		}

		allMetrics = append(allMetrics, metrics)
	}

	return allMetrics, nil
}

// GetRecentCalls retrieves recent tool calls
func (tmm *ToolMetricsManager) GetRecentCalls(ctx context.Context, limit int64) ([]ToolCallLog, error) {
	recentKey := "tool_calls:recent"
	callsData, err := tmm.redisClient.LRange(ctx, recentKey, 0, limit-1).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get recent calls: %v", err)
	}

	var calls []ToolCallLog
	for _, callData := range callsData {
		var call ToolCallLog
		err := json.Unmarshal([]byte(callData), &call)
		if err != nil {
			log.Printf("Warning: failed to unmarshal call log: %v", err)
			continue
		}
		calls = append(calls, call)
	}

	return calls, nil
}

// GetLogFilePath returns the current log file path
func (tmm *ToolMetricsManager) GetLogFilePath() string {
	return tmm.logPath
}

// Close closes the log file
func (tmm *ToolMetricsManager) Close() error {
	if tmm.logFile != nil {
		return tmm.logFile.Close()
	}
	return nil
}
