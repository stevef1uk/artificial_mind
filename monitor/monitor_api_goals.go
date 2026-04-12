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

// updateGoalStatus updates the status of a goal in the self-model
func (m *MonitorService) updateGoalStatus(c *gin.Context) {
	goalID := c.Param("id")

	var req struct {
		Status string `json:"status"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	client := &http.Client{Timeout: 30 * time.Second}
	updateURL := fmt.Sprintf("%s/api/v1/memory/goals/%s/status", m.hdnURL, goalID)

	updateData := map[string]string{"status": req.Status}
	jsonData, _ := json.Marshal(updateData)

	resp, err := client.Post(updateURL, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		c.JSON(resp.StatusCode, gin.H{"error": string(body)})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "message": "Goal status updated"})
}

// deleteSelfModelGoal proxies deletion to HDN self-model goals
func (m *MonitorService) deleteSelfModelGoal(c *gin.Context) {
	id := c.Param("id")
	if strings.TrimSpace(id) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id required"})
		return
	}
	req, err := http.NewRequest(http.MethodDelete, m.hdnURL+"/api/v1/memory/goals/"+urlQueryEscape(id), nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	c.Data(resp.StatusCode, "application/json", body)
}

// startCuriosityGoalConsumer runs in background to convert curiosity goals to Goal Manager tasks
func (m *MonitorService) startCuriosityGoalConsumer() {
	log.Println("🎯 Starting curiosity goal consumer...")

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	processed := make(map[string]bool)

	for {
		select {
		case <-ticker.C:
			log.Printf("🔄 Curiosity goal consumer tick - processing domains...")

			domains := []string{"General", "Networking", "Math", "Programming", "system_coherence"}

			if m.redisClient == nil {
				continue
			}

			for _, domain := range domains {

				key := fmt.Sprintf("reasoning:curiosity_goals:%s", domain)
				goals, err := m.redisClient.LRange(context.Background(), key, 0, 1).Result()
				if err != nil {
					log.Printf("⚠️ Failed to get curiosity goals for %s: %v", domain, err)
					continue
				}

				log.Printf("🔍 Checking %s: found %d curiosity goals", domain, len(goals))

				for _, goalData := range goals {
					var goal map[string]interface{}
					if err := json.Unmarshal([]byte(goalData), &goal); err != nil {
						log.Printf("⚠️ Failed to parse curiosity goal: %v", err)
						continue
					}

					goalID, _ := goal["id"].(string)
					log.Printf("🔍 Processing goal %s (processed: %v)", goalID, processed[goalID])
					if goalID == "" || processed[goalID] {
						log.Printf("⏭️ Skipping goal %s (empty or already processed)", goalID)
						continue
					}

					if err := m.convertCuriosityGoalToTask(goal, domain); err != nil {
						log.Printf("⚠️ Failed to convert curiosity goal %s: %v", goalID, err)
						continue
					}

					processed[goalID] = true
					m.redisClient.LRem(context.Background(), key, 1, goalData)

					log.Printf("✅ Converted curiosity goal %s to Goal Manager task", goalID)

					break
				}
			}
		}
	}
}

// convertCuriosityGoalToTask converts a curiosity goal to a Goal Manager task
func (m *MonitorService) convertCuriosityGoalToTask(goal map[string]interface{}, domain string) error {
	description, _ := goal["description"].(string)
	if description == "" {
		return fmt.Errorf("no description in curiosity goal")
	}

	taskData := map[string]interface{}{
		"agent_id":    "agent_1",
		"description": description,
		"priority":    "medium",
		"context": map[string]interface{}{
			"source":       "curiosity_goal",
			"domain":       domain,
			"curiosity_id": goal["id"],
		},
	}

	url := m.goalMgrURL + "/goal"
	data, err := json.Marshal(taskData)
	if err != nil {
		return fmt.Errorf("failed to marshal task data: %w", err)
	}

	if domain == "system_coherence" {
		log.Printf("📤 [Monitor] Sending coherence goal to Goal Manager: %s (context: %+v)", goal["id"], taskData["context"])
	}

	req, err := http.NewRequest("POST", url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("goal manager returned status %d: %s", resp.StatusCode, string(body))
	}

	if domain == "system_coherence" {
		log.Printf("✅ [Monitor] Coherence goal sent successfully to Goal Manager: %s", goal["id"])
	}

	return nil
}

// startAutoExecutor runs in background to execute one Goal Manager task at a time
func (m *MonitorService) startAutoExecutor() {
	log.Println("🚀 Starting auto-executor...")

	if m.redisClient == nil {
		log.Println("⚠️  Redis not connected, auto-executor will wait...")
	}

	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	// Track if we're currently executing a task
	var executing bool

	processedGoals := make(map[string]bool)

	cleanupTicker := time.NewTicker(10 * time.Minute)
	defer cleanupTicker.Stop()

	for {
		select {
		case <-cleanupTicker.C:

			processedGoals = make(map[string]bool)
			log.Printf("🧹 Auto-executor: cleaned up processed goals map")
		case <-ticker.C:
			log.Printf("🔄 Auto-executor: tick received, checking for goals...")

			if m.redisClient != nil {
				if paused, _ := m.redisClient.Get(context.Background(), "auto_executor:paused").Result(); paused == "1" {
					log.Printf("⏸️ Auto-executor: paused via Redis flag, skipping this cycle")
					continue
				}
			}
			if os.Getenv("ENABLE_AUTO_EXECUTOR") == "false" {
				log.Printf("⏸️ Auto-executor: disabled via ENABLE_AUTO_EXECUTOR=false, skipping this cycle")
				continue
			}

			if m.isHDNSaturated() {
				log.Printf("⏸️ Auto-executor: HDN saturated, skipping this cycle")
				continue
			}
			if executing {
				log.Printf("⏳ Auto-executor: already executing a task, skipping this cycle")

				continue
			}

			log.Printf("🔍 Auto-executor: fetching active goals from Goal Manager...")
			goals, err := m.getActiveGoalsFromGoalManager()
			if err != nil {
				log.Printf("⚠️ Auto-executor: failed to get goals: %v", err)
				continue
			}

			log.Printf("📊 Auto-executor: found %d active goals", len(goals))
			if len(goals) == 0 {
				log.Printf("ℹ️ Auto-executor: no active goals to execute")
				continue
			}

			// Pick the highest priority unprocessed goal first
			var goalToExecute map[string]interface{}
			priorityOrder := []string{"high", "medium", "low"}

			for _, priority := range priorityOrder {
				for _, goal := range goals {
					goalID := goal["id"].(string)
					goalPriority := goal["priority"].(string)
					if !processedGoals[goalID] && goalPriority == priority {
						goalToExecute = goal
						break
					}
				}
				if goalToExecute != nil {
					break
				}
			}

			if goalToExecute == nil {
				log.Printf("ℹ️ Auto-executor: no unprocessed goals to execute")
				continue
			}

			goalID := goalToExecute["id"].(string)
			description := goalToExecute["description"].(string)

			log.Printf("🎯 Auto-executor: executing goal %s: %s", goalID, description)

			goalKey := fmt.Sprintf("processed_goal_%s", goalID)
			if processedGoals[goalKey] {
				log.Printf("⏭️ Auto-executor: goal %s already processed recently, skipping", goalID)
				continue
			}

			executing = true

			go func() {
				defer func() {
					executing = false
					log.Printf("🔄 Auto-executor: reset executing flag for goal %s", goalID)
				}()

				done := make(chan error, 1)
				go func() {
					defer func() {
						if r := recover(); r != nil {
							log.Printf("💥 Auto-executor: panic in execution goroutine: %v", r)
							done <- fmt.Errorf("panic: %v", r)
						}
					}()
					done <- m.executeGoalTask(goalID, description)
				}()

				select {
				case err := <-done:
					if err != nil {
						log.Printf("❌ Auto-executor: failed to execute goal %s: %v", goalID, err)

						processedGoals[goalKey] = true
						return
					}
					log.Printf("✅ Auto-executor: successfully executed goal %s", goalID)

					processedGoals[goalKey] = true
				case <-time.After(10 * time.Minute):
					log.Printf("⏰ Auto-executor: goal %s timed out after 10 minutes", goalID)

					processedGoals[goalKey] = true
				}
			}()
		}
	}
}

// getActiveGoalsFromGoalManager fetches active goals from Goal Manager
func (m *MonitorService) getActiveGoalsFromGoalManager() ([]map[string]interface{}, error) {
	url := m.goalMgrURL + "/goals/agent_1/active"
	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to get goals: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("goal manager returned status %d", resp.StatusCode)
	}

	var goals []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&goals); err != nil {
		return nil, fmt.Errorf("failed to decode goals: %w", err)
	}

	return goals, nil
}

// executeGoalTask executes a goal task using HDN interpreter (like Suggest Next Steps)
func (m *MonitorService) executeGoalTask(goalID, description string) error {

	sessionID := fmt.Sprintf("auto_exec_%s_%d", goalID, time.Now().Unix())

	memorySummary, _ := m.getMemorySummaryForSession(sessionID)
	domains, _ := m.getCapabilitiesForSession()

	input := fmt.Sprintf("Given the goal: %s\nUse current memory summary and capabilities to propose the next concrete action(s) to progress the goal. Return a detailed, executable plan with specific steps.", description)

	ctx := map[string]string{
		"session_id":     sessionID,
		"memory_summary": m.serialize(memorySummary),
		"domains":        m.serialize(domains),
		"goal":           m.serialize(map[string]interface{}{"description": description, "id": goalID}),
	}

	plan, err := m.getInterpreterPlan(input, ctx, sessionID)
	if err != nil {
		return fmt.Errorf("failed to get interpreter plan: %w", err)
	}

	projectID := m.findOrCreateGoalsProject()
	if extractedProjectID, found := m.extractProjectIDFromText(description); found {
		projectID = extractedProjectID
		log.Printf("🎯 [AUTO-EXECUTOR] extracted project ID %s from goal description: %s", projectID, description)
	} else {
		log.Printf("🎯 [AUTO-EXECUTOR] using default Goals project %s for goal %s", projectID, goalID)
	}

	workflowDescription := fmt.Sprintf("Auto-executor: %s", description)
	if len(workflowDescription) > 200 {
		workflowDescription = workflowDescription[:200] + "..."
	}

	executionDescription := plan

	ctxMap := map[string]string{
		"session_id": sessionID,
	}

	if url := extractFirstURL(executionDescription); url != "" {
		ctxMap["url"] = url
	}

	execData := map[string]interface{}{
		"task_name":        "execute_goal_plan",
		"description":      executionDescription,
		"context":          ctxMap,
		"project_id":       projectID,
		"force_regenerate": true,
		"max_retries":      1,
	}

	data, err := json.Marshal(execData)
	if err != nil {
		return fmt.Errorf("failed to marshal execution data: %w", err)
	}

	url := m.hdnURL + "/api/v1/intelligent/execute"
	req, err := http.NewRequest("POST", url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 8 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("execution failed with status %d: %s", resp.StatusCode, string(body))
	}

	// Parse response to check if execution was actually successful
	var execResult map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&execResult); err == nil {
		if success, ok := execResult["success"].(bool); ok && success {
			log.Printf("✅ Auto-executor: successfully executed goal %s", goalID)

			return m.markGoalAsCompleted(goalID)
		} else {
			log.Printf("⚠️ Auto-executor: goal %s execution returned success=false", goalID)
			return fmt.Errorf("execution returned success=false")
		}
	}

	log.Printf("✅ Auto-executor: executed goal %s (response unparseable, assuming success)", goalID)
	return m.markGoalAsCompleted(goalID)
}

// isHDNSaturated checks if HDN is under heavy load by comparing recent in-flight
// tool calls with the configured max concurrency. Conservative on errors.
func (m *MonitorService) isHDNSaturated() bool {
	maxConc := 8
	if v := strings.TrimSpace(os.Getenv("HDN_MAX_CONCURRENT_EXECUTIONS")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			maxConc = n
		}
	}

	type call struct {
		Status string `json:"status"`
	}
	var resp struct {
		Calls []call `json:"calls"`
	}
	hdn := strings.TrimRight(m.hdnURL, "/")
	httpClient := &http.Client{Timeout: 2 * time.Second}
	req, _ := http.NewRequest("GET", hdn+"/api/v1/tools/calls/recent", nil)
	r, err := httpClient.Do(req)
	if err != nil || r.StatusCode != 200 {
		if r != nil {
			_ = r.Body.Close()
		}
		return true
	}
	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(&resp); err != nil {
		return true
	}
	inFlight := 0
	for _, c := range resp.Calls {
		s := strings.ToLower(strings.TrimSpace(c.Status))
		if s != "success" && s != "failure" && s != "blocked" {
			inFlight++
		}
	}
	return inFlight >= maxConc
}

// markGoalAsCompleted marks a goal as completed in Goal Manager
func (m *MonitorService) markGoalAsCompleted(goalID string) error {
	url := m.goalMgrURL + "/goal/" + goalID + "/achieve"
	req, err := http.NewRequest("POST", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to mark goal as completed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to mark goal as completed: status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// convertHypothesisToConcreteTask converts abstract hypothesis testing to concrete executable tasks
func (m *MonitorService) convertHypothesisToConcreteTask(description string) ConcreteTask {

	descLower := strings.ToLower(description)

	if strings.Contains(descLower, "communication") || strings.Contains(descLower, "message") || strings.Contains(descLower, "user input") {
		return ConcreteTask{
			Name:        "analyze_communication_patterns",
			Description: "Create a Python script that analyzes communication patterns and generates a report showing message frequency, sentiment analysis, and key topics. Include data visualization with matplotlib.",
			Language:    "python",
			Input:       "Sample messages: 'Hello world', 'Testing the system', 'How are you?', 'This is a test message'",
		}
	}

	if strings.Contains(descLower, "system") || strings.Contains(descLower, "processing") || strings.Contains(descLower, "state") {
		return ConcreteTask{
			Name:        "system_monitoring_dashboard",
			Description: "Create a Python script that monitors system processes and generates a real-time dashboard showing CPU usage, memory consumption, and active processes. Include JSON output and logging.",
			Language:    "python",
			Input:       "Monitor system resources and generate a status report",
		}
	}

	if strings.Contains(descLower, "infrastructure") || strings.Contains(descLower, "network") || strings.Contains(descLower, "connectivity") {
		return ConcreteTask{
			Name:        "network_connectivity_test",
			Description: "Create a Python script that tests network connectivity, measures latency, and generates a network health report with ping tests, port scans, and connection status.",
			Language:    "python",
			Input:       "Test connectivity to localhost:4222 (NATS), localhost:6379 (Redis), localhost:7474 (Neo4j)",
		}
	}

	return ConcreteTask{
		Name:        "data_analysis_tool",
		Description: "Create a Python script that performs data analysis on the given input, generates statistics, creates visualizations, and outputs results in both JSON and CSV formats.",
		Language:    "python",
		Input:       "Analyze the hypothesis: " + description,
	}
}

// findOrCreateGoalsProject finds or creates a "Goals" project for auto-executor workflows
func (m *MonitorService) findOrCreateGoalsProject() string {

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(m.hdnURL + "/api/v1/projects")
	if err != nil {
		log.Printf("⚠️ Failed to fetch projects: %v", err)
		return "Goals"
	}
	defer resp.Body.Close()

	var projects []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&projects); err != nil {
		log.Printf("⚠️ Failed to decode projects: %v", err)
		return "Goals"
	}

	for _, project := range projects {
		if name, ok := project["name"].(string); ok && name == "Goals" {
			if id, ok := project["id"].(string); ok {
				log.Printf("✅ Found existing Goals project: %s", id)
				return id
			}
		}
	}

	projectData := map[string]string{
		"name":        "Goals",
		"description": "Auto-executor goal workflows",
	}

	data, err := json.Marshal(projectData)
	if err != nil {
		log.Printf("⚠️ Failed to marshal project data: %v", err)
		return "Goals"
	}

	req, err := http.NewRequest("POST", m.hdnURL+"/api/v1/projects", bytes.NewReader(data))
	if err != nil {
		log.Printf("⚠️ Failed to create project request: %v", err)
		return "Goals"
	}
	req.Header.Set("Content-Type", "application/json")

	createResp, err := client.Do(req)
	if err != nil {
		log.Printf("⚠️ Failed to create Goals project: %v", err)
		return "Goals"
	}
	defer createResp.Body.Close()

	if createResp.StatusCode >= 200 && createResp.StatusCode < 300 {
		var newProject map[string]interface{}
		if err := json.NewDecoder(createResp.Body).Decode(&newProject); err == nil {
			if id, ok := newProject["id"].(string); ok {
				log.Printf("✅ Created new Goals project: %s", id)
				return id
			}
		}
	}

	log.Printf("⚠️ Failed to create Goals project, using fallback")
	return "Goals"
}
