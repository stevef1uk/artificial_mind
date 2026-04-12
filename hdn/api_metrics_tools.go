package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// recordMonitorMetrics records metrics in the format expected by the monitor UI
func (s *APIServer) recordMonitorMetrics(success bool, execTime time.Duration) {
	ctx := context.Background()

	totalExec, _ := s.redis.Get(ctx, "metrics:total_executions").Int()
	s.redis.Set(ctx, "metrics:total_executions", totalExec+1, 0)

	if success {
		successExec, _ := s.redis.Get(ctx, "metrics:successful_executions").Int()
		s.redis.Set(ctx, "metrics:successful_executions", successExec+1, 0)
	}

	avgTime, _ := s.redis.Get(ctx, "metrics:avg_execution_time").Float64()
	newAvg := (avgTime*float64(totalExec) + execTime.Seconds()*1000) / float64(totalExec+1)
	s.redis.Set(ctx, "metrics:avg_execution_time", newAvg, 0)

	s.redis.Set(ctx, "metrics:last_execution", time.Now().Format(time.RFC3339), 0)

	log.Printf("📈 [API] Updated monitor metrics: Total=%d, Success=%v, AvgTime=%.2fms",
		totalExec+1, success, newAvg)
}

// aggregateTokenUsage consolidates per-component token usage and deletes individual records
// This should be run hourly to prevent Redis from filling up
func (s *APIServer) aggregateTokenUsage(ctx context.Context) error {
	if s.redis == nil {
		return fmt.Errorf("Redis client not available")
	}

	today := time.Now().UTC().Format("2006-01-02")

	componentPattern := fmt.Sprintf("token_usage:%s:component:*:total", today)
	componentKeys, err := s.redis.Keys(ctx, componentPattern).Result()
	if err != nil {
		return fmt.Errorf("failed to get component keys: %v", err)
	}

	if len(componentKeys) == 0 {
		log.Printf("📊 [TOKEN-AGG] No component token keys found for today, skipping aggregation")
		return nil
	}

	log.Printf("📊 [TOKEN-AGG] Starting token aggregation for %d components", len(componentKeys))

	componentTotals := make(map[string]struct {
		prompt     int64
		completion int64
		total      int64
	})

	for _, key := range componentKeys {

		parts := strings.Split(key, ":")
		if len(parts) >= 5 && parts[3] == "component" {
			component := parts[4]

			totalKey := fmt.Sprintf("token_usage:%s:component:%s:total", today, component)
			promptKey := fmt.Sprintf("token_usage:%s:component:%s:prompt", today, component)
			completionKey := fmt.Sprintf("token_usage:%s:component:%s:completion", today, component)

			total, _ := s.redis.Get(ctx, totalKey).Int64()
			prompt, _ := s.redis.Get(ctx, promptKey).Int64()
			completion, _ := s.redis.Get(ctx, completionKey).Int64()

			if total > 0 {
				componentTotals[component] = struct {
					prompt     int64
					completion int64
					total      int64
				}{
					prompt:     prompt,
					completion: completion,
					total:      total,
				}
			}
		}
	}

	aggregatedExpiration := 90 * 24 * time.Hour
	for component, totals := range componentTotals {

		aggTotalKey := fmt.Sprintf("token_usage:aggregated:%s:component:%s:total", today, component)
		aggPromptKey := fmt.Sprintf("token_usage:aggregated:%s:component:%s:prompt", today, component)
		aggCompletionKey := fmt.Sprintf("token_usage:aggregated:%s:component:%s:completion", today, component)

		s.redis.Set(ctx, aggTotalKey, totals.total, aggregatedExpiration)
		s.redis.Set(ctx, aggPromptKey, totals.prompt, aggregatedExpiration)
		s.redis.Set(ctx, aggCompletionKey, totals.completion, aggregatedExpiration)

		log.Printf("📊 [TOKEN-AGG] Aggregated %s: %d total (%d prompt + %d completion)",
			component, totals.total, totals.prompt, totals.completion)

		totalKey := fmt.Sprintf("token_usage:%s:component:%s:total", today, component)
		promptKey := fmt.Sprintf("token_usage:%s:component:%s:prompt", today, component)
		completionKey := fmt.Sprintf("token_usage:%s:component:%s:completion", today, component)

		s.redis.Del(ctx, totalKey, promptKey, completionKey)
		log.Printf("🗑️ [TOKEN-AGG] Deleted individual keys for component %s", component)
	}

	overallTotalKey := fmt.Sprintf("token_usage:%s:total", today)
	overallPromptKey := fmt.Sprintf("token_usage:%s:prompt", today)
	overallCompletionKey := fmt.Sprintf("token_usage:%s:completion", today)

	overallTotal, _ := s.redis.Get(ctx, overallTotalKey).Int64()
	overallPrompt, _ := s.redis.Get(ctx, overallPromptKey).Int64()
	overallCompletion, _ := s.redis.Get(ctx, overallCompletionKey).Int64()

	if overallTotal > 0 {
		aggOverallTotalKey := fmt.Sprintf("token_usage:aggregated:%s:total", today)
		aggOverallPromptKey := fmt.Sprintf("token_usage:aggregated:%s:prompt", today)
		aggOverallCompletionKey := fmt.Sprintf("token_usage:aggregated:%s:completion", today)

		s.redis.Set(ctx, aggOverallTotalKey, overallTotal, aggregatedExpiration)
		s.redis.Set(ctx, aggOverallPromptKey, overallPrompt, aggregatedExpiration)
		s.redis.Set(ctx, aggOverallCompletionKey, overallCompletion, aggregatedExpiration)

		log.Printf("📊 [TOKEN-AGG] Aggregated overall totals: %d total (%d prompt + %d completion)",
			overallTotal, overallPrompt, overallCompletion)

	}

	log.Printf("✅ [TOKEN-AGG] Token aggregation completed successfully")
	return nil
}

// startTokenAggregationScheduler starts an hourly scheduler to aggregate token usage
func (s *APIServer) startTokenAggregationScheduler() {
	go func() {

		now := time.Now()
		next := now.Truncate(time.Hour).Add(time.Hour)
		d := time.Until(next)
		log.Printf("⏰ [TOKEN-AGG] Token aggregation scheduler will start at %s (in %s)",
			next.Format(time.RFC3339), d.String())

		time.Sleep(d)

		ctx := context.Background()
		if err := s.aggregateTokenUsage(ctx); err != nil {
			log.Printf("❌ [TOKEN-AGG] Initial aggregation failed: %v", err)
		}

		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()

		for range ticker.C {
			ctx := context.Background()
			if err := s.aggregateTokenUsage(ctx); err != nil {
				log.Printf("❌ [TOKEN-AGG] Hourly aggregation failed: %v", err)
			} else {
				log.Printf("✅ [TOKEN-AGG] Hourly aggregation completed")
			}
		}
	}()
}

// handleGetAllToolMetrics: GET /api/v1/tools/metrics
// handleLLMQueueStats returns current LLM queue statistics
func (s *APIServer) handleLLMQueueStats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	queueMgr := getAsyncLLMQueueManager()
	if queueMgr == nil {

		json.NewEncoder(w).Encode(map[string]interface{}{
			"high_priority_queue_size": 0,
			"low_priority_queue_size":  0,
			"max_high_priority_queue":  0,
			"max_low_priority_queue":   0,
			"active_workers":           0,
			"max_workers":              0,
			"high_priority_percent":    0,
			"low_priority_percent":     0,
			"background_llm_disabled":  false,
			"auto_disabled":            false,
			"timestamp":                time.Now().Format(time.RFC3339),
		})
		return
	}

	stats := queueMgr.GetStats()

	backgroundDisabled := strings.TrimSpace(os.Getenv("DISABLE_BACKGROUND_LLM")) == "1" ||
		strings.TrimSpace(os.Getenv("DISABLE_BACKGROUND_LLM")) == "true"

	// Check Redis for auto-disable state
	var autoDisabled bool
	if s.redis != nil {
		ctx := r.Context()
		val, err := s.redis.Get(ctx, "DISABLE_BACKGROUND_LLM").Result()
		autoDisabled = err == nil && (val == "1" || val == "true")
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"high_priority_queue_size": stats.HighPrioritySize,
		"low_priority_queue_size":  stats.LowPrioritySize,
		"max_high_priority_queue":  stats.MaxHighPriorityQueue,
		"max_low_priority_queue":   stats.MaxLowPriorityQueue,
		"active_workers":           stats.ActiveWorkers,
		"max_workers":              stats.MaxWorkers,
		"high_priority_percent":    stats.HighPriorityPercent,
		"low_priority_percent":     stats.LowPriorityPercent,
		"background_llm_disabled":  backgroundDisabled,
		"auto_disabled":            autoDisabled,
		"timestamp":                time.Now().Format(time.RFC3339),
	})
}

func (s *APIServer) handleGetAllToolMetrics(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if s.toolMetrics == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "tool metrics not available"})
		return
	}

	metrics, err := s.toolMetrics.GetAllToolMetrics(ctx)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": err.Error()})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"metrics": metrics,
		"count":   len(metrics),
	})
}

// handleGetToolMetrics: GET /api/v1/tools/{id}/metrics
func (s *APIServer) handleGetToolMetrics(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if s.toolMetrics == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "tool metrics not available"})
		return
	}

	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 4 {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "missing tool id"})
		return
	}
	toolID := parts[3]

	metrics, err := s.toolMetrics.GetToolMetrics(ctx, toolID)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": err.Error()})
		return
	}

	_ = json.NewEncoder(w).Encode(metrics)
}

// handleGetRecentToolCalls: GET /api/v1/tools/calls/recent
func (s *APIServer) handleGetRecentToolCalls(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if s.toolMetrics == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "tool metrics not available"})
		return
	}

	limitStr := r.URL.Query().Get("limit")
	limit := int64(50)
	if limitStr != "" {
		if parsedLimit, err := strconv.ParseInt(limitStr, 10, 64); err == nil && parsedLimit > 0 {
			limit = parsedLimit
		}
	}

	calls, err := s.toolMetrics.GetRecentCalls(ctx, limit)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": err.Error()})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"calls": calls,
		"count": len(calls),
	})
}
