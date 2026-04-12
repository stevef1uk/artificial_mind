package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// enqueueLLMJob enqueues an interpretation job with priority: high|normal|low
func (m *MonitorService) enqueueLLMJob(c *gin.Context) {
	var req struct {
		Input     string            `json:"input"`
		Context   map[string]string `json:"context,omitempty"`
		SessionID string            `json:"session_id,omitempty"`
		Priority  string            `json:"priority"`
		Callback  string            `json:"callback_url,omitempty"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Input) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	if req.Context == nil {
		req.Context = map[string]string{}
	}
	if req.SessionID == "" {
		req.SessionID = fmt.Sprintf("session_%d", time.Now().UnixNano())
	}
	pr := strings.ToLower(strings.TrimSpace(req.Priority))
	if pr == "" {
		pr = "normal"
	}
	if pr != "high" && pr != "normal" && pr != "low" {
		pr = "normal"
	}

	job := llmJob{
		ID:        fmt.Sprintf("llmjob_%d", time.Now().UnixNano()),
		Priority:  pr,
		Input:     req.Input,
		Context:   req.Context,
		SessionID: req.SessionID,
		Callback:  strings.TrimSpace(req.Callback),
	}
	bts, _ := json.Marshal(job)

	if m.redisClient != nil {
		key := fmt.Sprintf("llm:result:%s", job.ID)
		_ = m.redisClient.HSet(c, key, map[string]interface{}{
			"status":      "queued",
			"priority":    job.Priority,
			"enqueued_at": time.Now().Format(time.RFC3339),
			"callback":    job.Callback,
		}).Err()
		_ = m.redisClient.Expire(c, key, 2*time.Hour).Err()
	}

	queue := fmt.Sprintf("llm:%s", job.Priority)
	if m.redisClient == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "redis not configured"})
		return
	}
	if _, err := m.redisClient.LPush(c, queue, bts).Result(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "queue error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"job_id": job.ID, "status": "queued"})
}

// getLLMJobStatus returns job status/result
func (m *MonitorService) getLLMJobStatus(c *gin.Context) {
	id := c.Param("id")
	if id == "" || m.redisClient == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing id"})
		return
	}
	key := fmt.Sprintf("llm:result:%s", id)
	res, err := m.redisClient.HGetAll(c, key).Result()
	if err != nil || len(res) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	c.JSON(http.StatusOK, res)
}

// runLLMWorker processes jobs by priority and forwards to HDN interpret
func (m *MonitorService) runLLMWorker() {
	if m.redisClient == nil {
		return
	}
	go func() {
		ctx := context.Background()
		queues := []string{"llm:high", "llm:normal", "llm:low"}

		timeoutSeconds := 180
		if v := strings.TrimSpace(os.Getenv("MONITOR_LLM_WORKER_TIMEOUT_SECONDS")); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				timeoutSeconds = n
			}
		}
		client := &http.Client{Timeout: time.Duration(timeoutSeconds) * time.Second}
		for {

			vals, err := m.redisClient.BLPop(ctx, 0, queues...).Result()
			if err != nil {
				time.Sleep(500 * time.Millisecond)
				continue
			}
			if len(vals) < 2 {
				continue
			}
			payload := vals[1]
			var job llmJob
			if err := json.Unmarshal([]byte(payload), &job); err != nil {
				continue
			}

			rkey := fmt.Sprintf("llm:result:%s", job.ID)
			_ = m.redisClient.HSet(ctx, rkey, map[string]interface{}{
				"status":     "running",
				"started_at": time.Now().Format(time.RFC3339),
			}).Err()

			if m.isHDNSaturated() {

				_ = m.redisClient.LPush(ctx, fmt.Sprintf("llm:%s", job.Priority), payload).Err()
				time.Sleep(2 * time.Second)
				continue
			}

			url := m.hdnURL + "/api/v1/intelligent/execute"

			if job.Context == nil {
				job.Context = map[string]string{}
			}

			if job.SessionID != "" {
				job.Context["session_id"] = job.SessionID
			}

			job.Context["artifacts_wrapper"] = "true"
			job.Context["force_regenerate"] = "true"
			body := map[string]interface{}{
				"task_name":   "artifact_task",
				"description": job.Input,
				"context":     job.Context,
			}
			if pid, ok := job.Context["project_id"]; ok && strings.TrimSpace(pid) != "" {
				body["project_id"] = pid
			}
			bb, _ := json.Marshal(body)
			var resp *http.Response
			err = nil
			for attempt := 1; attempt <= 3; attempt++ {
				resp, err = client.Post(url, "application/json", strings.NewReader(string(bb)))
				if err == nil {
					break
				}
				if attempt < 3 {
					backoff := time.Duration(1<<uint(attempt-1)) * time.Second
					log.Printf("⚠️ HDN execute attempt %d failed: %v (retrying in %s)", attempt, err, backoff)
					time.Sleep(backoff)
				}
			}
			if err != nil {
				_ = m.redisClient.HSet(ctx, rkey, map[string]interface{}{
					"status":      "failed",
					"error":       err.Error(),
					"finished_at": time.Now().Format(time.RFC3339),
				}).Err()
				continue
			}
			rb, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()

			_ = m.redisClient.HSet(ctx, rkey, map[string]interface{}{
				"status":      "succeeded",
				"http_status": resp.StatusCode,
				"output":      string(rb),
				"finished_at": time.Now().Format(time.RFC3339),
			}).Err()
			_ = m.redisClient.Expire(ctx, rkey, 2*time.Hour).Err()

			time.Sleep(1 * time.Second)

			_ = m.redisClient.Publish(ctx, fmt.Sprintf("llm:done:%s", job.ID), string(rb)).Err()

			if job.Callback != "" {
				go func(cb string, payload []byte, code int) {
					req, _ := http.NewRequest(http.MethodPost, cb, bytes.NewReader(payload))
					req.Header.Set("Content-Type", "application/json")
					q := req.URL.Query()
					q.Add("job_id", job.ID)
					q.Add("status", "succeeded")
					q.Add("http_status", fmt.Sprintf("%d", code))
					req.URL.RawQuery = q.Encode()
					http.DefaultClient.Do(req)
				}(job.Callback, rb, resp.StatusCode)
			}
		}
	}()
}

// getExecutionMetricsFromRedis retrieves metrics from Redis
// getLLMQueueStats proxies LLM queue stats from HDN
func (m *MonitorService) getLLMQueueStats(c *gin.Context) {
	url := m.hdnURL + "/api/v1/llm/queue/stats"

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error":     "Failed to fetch LLM queue stats",
			"details":   err.Error(),
			"timestamp": time.Now().Format(time.RFC3339),
		})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		c.JSON(http.StatusBadGateway, gin.H{
			"error":     "HDN returned error",
			"status":    resp.StatusCode,
			"details":   string(body),
			"timestamp": time.Now().Format(time.RFC3339),
		})
		return
	}

	var stats map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":     "Failed to parse LLM queue stats",
			"details":   err.Error(),
			"timestamp": time.Now().Format(time.RFC3339),
		})
		return
	}

	c.JSON(http.StatusOK, stats)
}
