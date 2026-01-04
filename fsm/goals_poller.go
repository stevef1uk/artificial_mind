package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

func goalsDebugEnabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("FSM_GOALS_DEBUG")))
	return v == "1" || v == "true" || v == "yes"
}

func goalsDebugf(format string, args ...any) {
	if goalsDebugEnabled() {
		log.Printf(format, args...)
	}
}

type GoalItem struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Type        string                 `json:"type"`
	Status      string                 `json:"status"`
	Context     map[string]interface{} `json:"context"`
}

func startGoalsPoller(agentID, goalMgrURL string, rdb *redis.Client) {
	log.Printf("🎯 [FSM][Goals] Starting goals poller for agent %s (polling interval: 2s)", agentID)
	ctx := context.Background()
	hdnURL := strings.TrimSpace(os.Getenv("HDN_URL"))
	if hdnURL == "" {
		hdnURL = "http://localhost:8080"
	}
	log.Printf("🎯 [FSM][Goals] Goal Manager URL: %s, HDN URL: %s", goalMgrURL, hdnURL)

	triggeredKey := fmt.Sprintf("fsm:%s:goals:triggered", agentID)

	client := &http.Client{Timeout: 10 * time.Second}

	// Start with 2s polling but may back off on 429
	pollingInterval := 2 * time.Second
	ticker := time.NewTicker(pollingInterval)
	defer ticker.Stop()
	
	var lastBackoff time.Time
	backoffMultiplier := 1

	// Start periodic cleanup task to clear triggered flags for achieved/failed goals
	cleanupTicker := time.NewTicker(5 * time.Minute)
	defer cleanupTicker.Stop()
	go func() {
		for {
			select {
			case <-cleanupTicker.C:
				cleanupStuckTriggeredFlags(ctx, agentID, goalMgrURL, rdb, triggeredKey)
			}
		}
	}()

	for {
		select {
		case <-ticker.C:
			// Pause guard: suspend auto goal triggering when manual executions are running
			if paused, err := rdb.Get(ctx, "auto_executor:paused").Result(); err == nil && strings.TrimSpace(paused) == "1" {
				log.Printf("[FSM][Goals] Auto-executor paused by Redis flag; skipping tick")
				continue
			}
			
			// Check how many active workflows are running to prevent execution slot exhaustion
			activeWorkflowCount, err := rdb.SCard(ctx, "active_workflows").Result()
			if err == nil {
				// Don't trigger new goals if too many workflows are already running
				// With default of 4 general execution slots, allow up to 3 active workflows
				// before pausing new goal triggers (leaves 1 slot free for other operations)
				maxActiveWorkflows := 3
				if activeWorkflowCount >= int64(maxActiveWorkflows) {
					goalsDebugf("[FSM][Goals] Skipping goal trigger - %d active workflows (max: %d)", activeWorkflowCount, maxActiveWorkflows)
					continue
				}
			}
			
			// Fetch active goals for this agent
			url := fmt.Sprintf("%s/goals/%s/active", goalMgrURL, agentID)
			resp, err := client.Get(url)
			if err != nil {
				log.Printf("[FSM][Goals] fetch active goals error: %v", err)
				continue
			}
			// Check response status before trying to decode
			if resp.StatusCode != http.StatusOK {
				bodyBytes, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				log.Printf("[FSM][Goals] goals fetch returned status %d: %s", resp.StatusCode, string(bodyBytes))
				continue
			}
			// Read body first to check if it's valid JSON
			bodyBytes, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil {
				log.Printf("[FSM][Goals] failed to read goals response body: %v", err)
				continue
			}
			// Check if response is empty or not JSON
			bodyStr := strings.TrimSpace(string(bodyBytes))
			if bodyStr == "" {
				continue
			}
			if !strings.HasPrefix(bodyStr, "{") && !strings.HasPrefix(bodyStr, "[") {
				previewLen := 20
				if len(bodyStr) < previewLen {
					previewLen = len(bodyStr)
				}
				fullLen := 100
				if len(bodyStr) < fullLen {
					fullLen = len(bodyStr)
				}
				log.Printf("[FSM][Goals] goals response is not JSON (starts with: %s): %s", bodyStr[:previewLen], bodyStr[:fullLen])
				continue
			}
			var payload any
			if err := json.Unmarshal(bodyBytes, &payload); err != nil {
				errorLen := 200
				if len(bodyStr) < errorLen {
					errorLen = len(bodyStr)
				}
				log.Printf("[FSM][Goals] decode goals error: %v (response: %s)", err, bodyStr[:errorLen])
				continue
			}

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

			log.Printf("🎯 [FSM][Goals] Fetched %d goals from Goal Manager (checking %d to trigger)", len(goals), len(goals))
			if len(goals) == 0 {
				continue
			}

			triggeredCount := 0
			skippedCount := 0
			for _, g := range goals {
				if g.ID == "" {
					continue
				}
				// Skip if already triggered
				exists, _ := rdb.SIsMember(ctx, triggeredKey, g.ID).Result()
				if exists {
					skippedCount++
					continue
				}

				// Already triggered limit reached - stop processing more goals this cycle
				if triggeredCount >= 1 {
					break
				}

				// Build hierarchical execute payload
				// Use goal description/name as the task_name and user_request; pass identifiers in context
				goalDesc := firstNonEmpty(g.Description, g.Name, "Execute goal")
				
				// GOAL ROUTING: Determine execution path based on goal type
				execURL, req := routeGoalExecution(goalDesc, g.ID, agentID, hdnURL)
				
				// Use goal description as task_name instead of generic "Goal Execution"
				// This gives the planner better context about what to actually do
				taskName := goalDesc
				if len(taskName) > 100 {
					// Truncate very long descriptions for task_name
					taskName = taskName[:97] + "..."
				}
				
				// If no specific routing, use default hierarchical execute
				if execURL == "" {
					req = map[string]interface{}{
						"task_name":    taskName,
						"description":  goalDesc,
						"user_request": goalDesc,
						"context": map[string]string{
							"session_id": fmt.Sprintf("goal_%s", g.ID),
							"goal_id":    g.ID,
							"agent_id":   agentID,
							"project_id": "Goals",
						},
					}
					execURL = strings.TrimRight(hdnURL, "/") + "/api/v1/hierarchical/execute"
				}
				
				// MARK AS TRIGGERED FIRST to prevent race condition where multiple pollers trigger same goal
				_ = rdb.SAdd(ctx, triggeredKey, g.ID).Err()
				_ = rdb.Expire(ctx, triggeredKey, 30*time.Minute).Err()
				
				log.Printf("🎯 [FSM][Goals] Executing goal %s (type: %s, description: %s) via %s", g.ID, g.Type, taskName, execURL)
				b, _ := json.Marshal(req)
				eresp, err := client.Post(execURL, "application/json", strings.NewReader(string(b)))
				if err != nil {
					log.Printf("❌ [FSM][Goals] execute error for goal %s: %v", g.ID, err)
					triggeredCount++
					break
				}
				bodyBytes, _ := io.ReadAll(eresp.Body)
				if eresp.Body != nil {
					eresp.Body.Close()
				}
				if eresp.StatusCode >= 200 && eresp.StatusCode < 300 {
					// Parse workflow_id from response
					var execResp struct {
						Success    bool   `json:"success"`
						WorkflowID string `json:"workflow_id"`
						Message    string `json:"message"`
					}
					workflowID := ""
					if err := json.Unmarshal(bodyBytes, &execResp); err == nil {
						workflowID = execResp.WorkflowID
					}

					log.Printf("✅ [FSM][Goals] Triggered goal %s (workflow: %s)", g.ID, workflowID)
					goalsDebugf("[FSM][Goals] triggered goal %s (workflow: %s)", g.ID, workflowID)

					// Reset backoff when we successfully trigger a goal (HDN is no longer overloaded)
					if backoffMultiplier > 1 {
						backoffMultiplier = 1
						ticker.Reset(pollingInterval)
						log.Printf("✅ [FSM][Goals] HDN recovered - reset polling interval to %v", pollingInterval)
					}

					// Start background watcher to clear triggered flag when workflow completes or fails
					if workflowID != "" {
						go watchWorkflowAndClearTriggered(ctx, agentID, g.ID, workflowID, hdnURL, goalMgrURL, rdb, triggeredKey)
					} else {
						// If no workflow_id, set up a timeout watcher to clear after reasonable time
						go watchGoalStatusAndClearTriggered(ctx, agentID, g.ID, goalMgrURL, rdb, triggeredKey)
					}

					triggeredCount++
					// Limit to 1 goal triggered per tick to prevent execution slot exhaustion
					// With only 2-3 execution slots available, triggering multiple goals simultaneously
					// causes timeouts. Process goals one at a time.
					if triggeredCount >= 1 {
						break
					}
				} else if eresp.StatusCode == http.StatusConflict {
					// Handle 409 Conflict - workflow already exists/running
					// Parse response to get existing workflow_id
					var conflictResp struct {
						Success    bool   `json:"success"`
						WorkflowID string `json:"workflow_id"`
						Message    string `json:"message"`
						Error      string `json:"error"`
					}
					workflowID := ""
					if err := json.Unmarshal(bodyBytes, &conflictResp); err == nil {
						workflowID = conflictResp.WorkflowID
					}

					if workflowID != "" {
						goalsDebugf("[FSM][Goals] goal %s already has workflow %s running - marked as triggered", g.ID, workflowID)
						// Watch the existing workflow to clear triggered flag when it completes
						go watchWorkflowAndClearTriggered(ctx, agentID, g.ID, workflowID, hdnURL, goalMgrURL, rdb, triggeredKey)
					} else {
						goalsDebugf("[FSM][Goals] goal %s duplicate (409) but no workflow_id - marked as triggered with timeout watcher", g.ID)
						// Set up timeout watcher since we don't have workflow_id
						go watchGoalStatusAndClearTriggered(ctx, agentID, g.ID, goalMgrURL, rdb, triggeredKey)
					}

					triggeredCount++
					// Don't increment skipped/continue - we did mark as triggered
				} else if eresp.StatusCode == http.StatusTooManyRequests {
					// Handle 429 Too Many Requests - HDN is overloaded
					// Remove from triggered set so we can retry later
					_ = rdb.SRem(ctx, triggeredKey, g.ID).Err()
					log.Printf("⚠️ [FSM][Goals] HDN overloaded (429) - goal %s not triggered, will retry later", g.ID)
					// Implement exponential backoff: increase polling interval
					if time.Since(lastBackoff) > 10*time.Second {
						backoffMultiplier = 2
						lastBackoff = time.Now()
						newInterval := time.Duration(int64(pollingInterval) * int64(backoffMultiplier))
						if newInterval > 30*time.Second {
							newInterval = 30 * time.Second
						}
						ticker.Reset(newInterval)
						log.Printf("⏱️ [FSM][Goals] Backed off polling interval to %v due to HDN overload", newInterval)
					}
					// Stop triggering more goals this cycle to give HDN time to recover
					break
				} else {
					log.Printf("❌ [FSM][Goals] execute failed for goal %s (status %d): %s", g.ID, eresp.StatusCode, string(bodyBytes))
					triggeredCount++
				}
			}
			if triggeredCount > 0 || skippedCount > 0 {
				log.Printf("🎯 [FSM][Goals] Poller cycle complete: triggered=%d, already_triggered=%d", triggeredCount, skippedCount)
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

// watchWorkflowAndClearTriggered monitors workflow completion and clears the triggered flag
func watchWorkflowAndClearTriggered(ctx context.Context, agentID, goalID, workflowID, hdnURL, goalMgrURL string, rdb *redis.Client, triggeredKey string) {
	if strings.TrimSpace(workflowID) == "" || strings.TrimSpace(goalID) == "" {
		return
	}

	// Poll for up to 15 minutes (workflows can take time)
	deadline := time.Now().Add(15 * time.Minute)
	checkInterval := 5 * time.Second

	for time.Now().Before(deadline) {
		completed := false
		status := ""

		// Check if workflow is still active
		// For intelligent_ workflows, check Redis set
		if strings.HasPrefix(workflowID, "intelligent_") {
			if member, err := rdb.SIsMember(ctx, "active_workflows", workflowID).Result(); err == nil {
				if !member {
					completed = true
					status = "completed"
				}
			}
		}

		// For hierarchical workflows, check workflow details endpoint
		if !completed {
			detailsURL := hdnURL + "/api/v1/hierarchical/workflow/" + url.PathEscape(workflowID) + "/details"
			client := &http.Client{Timeout: 5 * time.Second}
			if resp, err := client.Get(detailsURL); err == nil && resp != nil {
				var payload struct {
					Success bool                   `json:"success"`
					Details map[string]interface{} `json:"details"`
				}
				if err := json.NewDecoder(resp.Body).Decode(&payload); err == nil {
					resp.Body.Close()
					if payload.Success && payload.Details != nil {
						// Check workflow status
						if ws, ok := payload.Details["status"].(string); ok {
							status = strings.ToLower(ws)
							if status == "completed" || status == "failed" || status == "cancelled" {
								completed = true
							}
						} else {
							// Fallback: check if there are any running/pending steps
							if steps, ok := payload.Details["steps"].([]interface{}); ok {
								running := 0
								for _, s := range steps {
									if m, ok := s.(map[string]interface{}); ok {
										if st, _ := m["status"].(string); strings.ToLower(st) == "running" || strings.ToLower(st) == "pending" {
											running++
										}
									}
								}
								if running == 0 {
									completed = true
									status = "completed"
								}
							}
						}
					}
				} else {
					resp.Body.Close()
				}
			}
		}

		if completed {
			// Clear triggered flag so goal can be retried if needed
			_ = rdb.SRem(ctx, triggeredKey, goalID).Err()
			goalsDebugf("[FSM][Goals] workflow %s for goal %s %s - cleared triggered flag", workflowID, goalID, status)
			return
		}

		// Also check if goal status changed to achieved/failed
		if goalStatus := checkGoalStatus(ctx, goalID, goalMgrURL); goalStatus == "achieved" || goalStatus == "failed" {
			_ = rdb.SRem(ctx, triggeredKey, goalID).Err()
			goalsDebugf("[FSM][Goals] goal %s status changed to %s - cleared triggered flag", goalID, goalStatus)
			return
		}

		time.Sleep(checkInterval)
	}

	// Timeout reached - clear triggered flag to allow retry
	_ = rdb.SRem(ctx, triggeredKey, goalID).Err()
	goalsDebugf("[FSM][Goals] workflow %s for goal %s timed out after 15min - cleared triggered flag for retry", workflowID, goalID)
}

// watchGoalStatusAndClearTriggered monitors goal status changes and clears triggered flag
func watchGoalStatusAndClearTriggered(ctx context.Context, agentID, goalID, goalMgrURL string, rdb *redis.Client, triggeredKey string) {
	// Poll for up to 10 minutes
	deadline := time.Now().Add(10 * time.Minute)
	checkInterval := 10 * time.Second

	for time.Now().Before(deadline) {
		status := checkGoalStatus(ctx, goalID, goalMgrURL)
		if status == "achieved" || status == "failed" {
			_ = rdb.SRem(ctx, triggeredKey, goalID).Err()
			goalsDebugf("[FSM][Goals] goal %s status changed to %s - cleared triggered flag", goalID, status)
			return
		}
		time.Sleep(checkInterval)
	}

	// Timeout reached - clear triggered flag to allow retry
	_ = rdb.SRem(ctx, triggeredKey, goalID).Err()
	goalsDebugf("[FSM][Goals] goal %s watcher timed out after 10min - cleared triggered flag for retry", goalID)
}

// checkGoalStatus checks the current status of a goal
func checkGoalStatus(ctx context.Context, goalID, goalMgrURL string) string {
	if goalMgrURL == "" {
		return ""
	}

	// Try to get goal status from Goal Manager
	url := goalMgrURL + "/goal/" + goalID
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return ""
	}

	var goal struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&goal); err != nil {
		return ""
	}

	return goal.Status
}

// cleanupStuckTriggeredFlags periodically checks triggered goals and clears flags for achieved/failed goals
func cleanupStuckTriggeredFlags(ctx context.Context, agentID, goalMgrURL string, rdb *redis.Client, triggeredKey string) {
	// Get all triggered goal IDs
	triggeredIDs, err := rdb.SMembers(ctx, triggeredKey).Result()
	if err != nil {
		return
	}

	if len(triggeredIDs) == 0 {
		return
	}

	clearedCount := 0
	for _, goalID := range triggeredIDs {
		// Check if goal is still active
		status := checkGoalStatus(ctx, goalID, goalMgrURL)
		if status == "achieved" || status == "failed" || status == "" {
			// Goal is no longer active or doesn't exist - clear triggered flag
			_ = rdb.SRem(ctx, triggeredKey, goalID).Err()
			clearedCount++
			if status != "" {
				goalsDebugf("[FSM][Goals] cleanup: cleared triggered flag for %s goal %s", status, goalID)
			} else {
				goalsDebugf("[FSM][Goals] cleanup: cleared triggered flag for missing goal %s", goalID)
			}
		}
	}

	if clearedCount > 0 {
		goalsDebugf("[FSM][Goals] cleanup: cleared %d stuck triggered flag(s)", clearedCount)
	}
}

// routeGoalExecution determines the best execution path based on goal description
// Returns (execURL, requestPayload) - empty URL means use default hierarchical execute
func routeGoalExecution(goalDesc, goalID, agentID, hdnURL string) (string, map[string]interface{}) {
	goalDescLower := strings.ToLower(goalDesc)
	
	// Route 1: Knowledge queries → Direct knowledge base endpoint
	if strings.Contains(goalDescLower, "query_knowledge_base") || 
	   strings.Contains(goalDescLower, "query neo4j") ||
	   strings.Contains(goalDescLower, "[active-learning] query_knowledge_base") {
		// For now, still use hierarchical execute but mark as knowledge query
		// TODO: Create dedicated knowledge query endpoint in HDN
		return "", map[string]interface{}{
			"task_name":    "knowledge_query",
			"description":  goalDesc,
			"user_request": goalDesc,
			"context": map[string]string{
				"session_id":   fmt.Sprintf("goal_%s", goalID),
				"goal_id":      goalID,
				"agent_id":     agentID,
				"project_id":   "Goals",
				"routing_hint": "knowledge_query",
			},
		}
	}
	
	// Route 2: Tool calls → Direct tool execution
	if strings.Contains(goalDescLower, "use tool_") ||
	   strings.Contains(goalDescLower, "tool_http_get") ||
	   strings.Contains(goalDescLower, "tool_html_scraper") {
		return "", map[string]interface{}{
			"task_name":    "tool_execution",
			"description":  goalDesc,
			"user_request": goalDesc,
			"context": map[string]string{
				"session_id":   fmt.Sprintf("goal_%s", goalID),
				"goal_id":      goalID,
				"agent_id":     agentID,
				"project_id":   "Goals",
				"routing_hint": "tool_call",
			},
		}
	}
	
	// Route 3: Inconsistency analysis → Reasoning engine
	if strings.Contains(goalDescLower, "you have detected an inconsistency") ||
	   strings.Contains(goalDescLower, "analyze this inconsistency") ||
	   strings.Contains(goalDescLower, "behavior_loop") {
		return "", map[string]interface{}{
			"task_name":    "analyze_inconsistency",
			"description":  goalDesc,
			"user_request": goalDesc,
			"context": map[string]string{
				"session_id":   fmt.Sprintf("goal_%s", goalID),
				"goal_id":      goalID,
				"agent_id":     agentID,
				"project_id":   "Goals",
				"routing_hint": "reasoning",
			},
		}
	}
	
	// Default: return empty to use hierarchical execute
	return "", nil
}
