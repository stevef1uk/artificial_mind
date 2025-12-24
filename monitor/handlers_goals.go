package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// helper: extract integer parameter from a description like "... max_nodes=10 ..."
func extractIntParam(s, key string) int {
	lower := strings.ToLower(s)
	idx := strings.Index(lower, key+"=")
	if idx < 0 {
		return 0
	}
	rest := s[idx+len(key)+1:]
	// read until non-digit
	n := 0
	for i := 0; i < len(rest); i++ {
		if rest[i] < '0' || rest[i] > '9' {
			break
		}
		n = n*10 + int(rest[i]-'0')
	}
	return n
}

// helper: extract string parameter from a description like "... domain=General ..."
func extractStringParam(s, key string) string {
	lower := strings.ToLower(s)
	idx := strings.Index(lower, key+"=")
	if idx < 0 {
		return ""
	}
	rest := s[idx+len(key)+1:]
	// read until delimiter ';' or space
	end := len(rest)
	for i := 0; i < len(rest); i++ {
		if rest[i] == ';' || rest[i] == ' ' {
			end = i
			break
		}
	}
	return strings.TrimSpace(rest[:end])
}

// getActiveGoals proxies to Goal Manager: /goals/{agent}/active
func (m *MonitorService) getActiveGoals(c *gin.Context) {
	agent := c.Param("agent")
	if agent == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "agent required"})
		return
	}
	url := m.goalMgrURL + "/goals/" + agent + "/active"
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		log.Printf("❌ Failed to connect to Goal Manager at %s: %v", url, err)
		c.JSON(http.StatusBadGateway, gin.H{
			"error":   "failed to fetch active goals",
			"message": fmt.Sprintf("Cannot connect to Goal Manager at %s. Please check if the service is running.", m.goalMgrURL),
			"details": err.Error(),
		})
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	// Try to parse and deduplicate by normalized description to suppress duplicates
	// Support both raw array and wrapped { goals: [...] }
	type Goal = map[string]interface{}
	var (
		rawArr  []Goal
		wrapped struct {
			Goals []Goal `json:"goals"`
		}
	)
	if err := json.Unmarshal(body, &rawArr); err == nil && len(rawArr) >= 0 {
		dedup := dedupGoalsByDescription(rawArr)
		out, _ := json.Marshal(dedup)
		c.Data(resp.StatusCode, "application/json", out)
		return
	}
	if err := json.Unmarshal(body, &wrapped); err == nil && wrapped.Goals != nil {
		dedup := dedupGoalsByDescription(wrapped.Goals)
		out, _ := json.Marshal(gin.H{"goals": dedup})
		c.Data(resp.StatusCode, "application/json", out)
		return
	}

	// Fallback: return as-is if unexpected format
	c.Data(resp.StatusCode, "application/json", body)
}

// dedupGoalsByDescription collapses goals with the same normalized description, keeping the most recently updated
func dedupGoalsByDescription(goals []map[string]interface{}) []map[string]interface{} {
	normalize := func(s string) string { return strings.ToLower(strings.TrimSpace(s)) }
	parseTime := func(v interface{}) time.Time {
		if s, ok := v.(string); ok && s != "" {
			if t, err := time.Parse(time.RFC3339, s); err == nil {
				return t
			}
		}
		return time.Time{}
	}
	// group by description key
	byKey := map[string]map[string]interface{}{}
	for _, g := range goals {
		desc := ""
		if d, ok := g["description"].(string); ok && strings.TrimSpace(d) != "" {
			desc = d
		} else if n, ok := g["name"].(string); ok {
			desc = n
		}
		key := normalize(desc)
		if key == "" {
			key = normalize(fmt.Sprintf("%v", g["id"]))
		}
		if existing, ok := byKey[key]; ok {
			// keep the most recently updated (fall back to created_at)
			curU := parseTime(existing["updated_at"])
			curC := parseTime(existing["created_at"])
			newU := parseTime(g["updated_at"])
			newC := parseTime(g["created_at"])
			if newU.After(curU) || (curU.IsZero() && newC.After(curC)) {
				byKey[key] = g
			}
		} else {
			byKey[key] = g
		}
	}
	out := make([]map[string]interface{}, 0, len(byKey))
	for _, g := range byKey {
		out = append(out, g)
	}
	// optional: sort by updated_at desc for stable UI
	sort.Slice(out, func(i, j int) bool {
		ti := parseTime(out[i]["updated_at"])
		tj := parseTime(out[j]["updated_at"])
		if ti.Equal(tj) {
			return parseTime(out[i]["created_at"]).After(parseTime(out[j]["created_at"]))
		}
		return ti.After(tj)
	})
	return out
}

// getGoalByID proxies to Goal Manager: /goal/{id}
func (m *MonitorService) getGoalByID(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id required"})
		return
	}
	url := m.goalMgrURL + "/goal/" + id
	resp, err := http.Get(url)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to fetch goal"})
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	c.Data(resp.StatusCode, "application/json", body)
}

// achieveGoal proxies to Goal Manager: POST /goal/{id}/achieve
func (m *MonitorService) achieveGoal(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id required"})
		return
	}
	url := m.goalMgrURL + "/goal/" + id + "/achieve"
	req, _ := http.NewRequest("POST", url, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to achieve goal"})
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	c.Data(resp.StatusCode, "application/json", body)
}

// suggestGoalNextSteps fetches memory and capabilities and asks HDN interpreter to suggest next actions
func (m *MonitorService) suggestGoalNextSteps(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id required"})
		return
	}

	// Start async operation and return immediately
	go m.suggestGoalNextStepsAsync(id)

	// Return immediately with status
	c.JSON(http.StatusAccepted, gin.H{
		"success": true,
		"message": "Goal suggestion started asynchronously",
		"goal_id": id,
		"status":  "processing",
	})
}

// suggestGoalNextStepsAsync performs the actual suggestion work asynchronously
func (m *MonitorService) suggestGoalNextStepsAsync(id string) {
	// Fetch goal details
	gresp, gerr := http.Get(m.goalMgrURL + "/goal/" + id)
	if gerr != nil {
		log.Printf("❌ Failed to fetch goal %s: %v", id, gerr)
		return
	}
	defer gresp.Body.Close()
	var goal map[string]interface{}
	_ = json.NewDecoder(gresp.Body).Decode(&goal)

	// Session id: prefer goal context.session_id or synthesize
	sessionID := fmt.Sprintf("goal_%s", id)
	if ctx, ok := goal["context"].(map[string]interface{}); ok {
		if s, ok := ctx["session_id"].(string); ok && s != "" {
			sessionID = s
		}
	}

	// Fetch memory summary (beliefs, goals, working memory, recent episodes)
	msURL := m.hdnURL + "/api/v1/memory/summary?session_id=" + urlQueryEscape(sessionID)
	msResp, msErr := http.Get(msURL)
	var memorySummary map[string]interface{}
	if msErr == nil {
		defer msResp.Body.Close()
		_ = json.NewDecoder(msResp.Body).Decode(&memorySummary)
	}

	// Fetch capabilities
	capsResp, capsErr := http.Get(m.hdnURL + "/api/v1/domains")
	var domains []map[string]interface{}
	if capsErr == nil {
		defer capsResp.Body.Close()
		_ = json.NewDecoder(capsResp.Body).Decode(&domains)
	}

	// Build prompt for interpreter with robust goal fallback
	goalText := ""
	if s, _ := goal["description"].(string); strings.TrimSpace(s) != "" {
		goalText = s
	} else if s, _ := goal["name"].(string); strings.TrimSpace(s) != "" {
		goalText = s
	} else {
		goalText = fmt.Sprintf("goal %s", id)
	}
	input := fmt.Sprintf("Given the goal: %s\nUse current memory summary and capabilities to propose the next concrete action(s) to progress the goal. Return a short plan.", goalText)

	// Interpreter expects context values as strings; serialize rich objects
	serialize := func(v interface{}) string {
		if v == nil {
			return ""
		}
		if s, ok := v.(string); ok {
			return s
		}
		b, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprintf("%v", v)
		}
		return string(b)
	}

	ctx := map[string]string{
		"session_id":     sessionID,
		"memory_summary": serialize(memorySummary),
		"domains":        serialize(domains),
		"goal":           serialize(goal),
	}

	payload := map[string]interface{}{
		"input":      input,
		"context":    ctx,
		"session_id": sessionID,
	}
	b, _ := json.Marshal(payload)

	// Use explicit client with generous timeout; interpreter can be slow
	interpClient := &http.Client{Timeout: 300 * time.Second} // Increased timeout for async operation
	iresp, ierr := interpClient.Post(m.hdnURL+"/api/v1/interpret", "application/json", bytes.NewReader(b))
	if ierr != nil {
		log.Printf("❌ Failed to consult interpreter for goal %s: %v", id, ierr)
		return
	}
	defer iresp.Body.Close()
	body, _ := io.ReadAll(iresp.Body)
	// If interpreter returned non-200, include body for easier debugging
	if iresp.StatusCode < 200 || iresp.StatusCode >= 300 {
		log.Printf("❌ Interpreter error for goal %s: %s - %s", id, iresp.Status, string(body))
		return
	}
	// Best-effort: log minimal reasoning artifacts under a domain inferred from goal context
	if m.redisClient != nil {
		// Determine domain: prefer goal.context.domain, else extract from text "domain=...", else General
		domain := "General"
		if ctx, ok := goal["context"].(map[string]interface{}); ok {
			if d, ok := ctx["domain"].(string); ok && strings.TrimSpace(d) != "" {
				domain = d
			}
		}
		if d := extractStringParam(goalText, "domain"); d != "" {
			domain = d
		}

		// Trace entry
		trace := map[string]interface{}{
			"id":         fmt.Sprintf("trace_%d", time.Now().UnixNano()),
			"goal":       goalText,
			"action":     "suggest",
			"conclusion": "Suggestion generated",
			"confidence": 0.6,
			"domain":     domain,
			"created_at": time.Now().UTC().Format(time.RFC3339),
			"steps": []map[string]interface{}{
				{
					"step_number": 1,
					"action":      "interpret",
					"reasoning":   "HDN Interpreter proposed next steps",
					"confidence":  0.6,
				},
			},
			"suggestion": func() interface{} {
				var v interface{}
				_ = json.Unmarshal(body, &v)
				return v
			}(),
		}
		if bts, err := json.Marshal(trace); err == nil {
			key := fmt.Sprintf("reasoning:traces:%s", domain)
			_, _ = m.redisClient.LPush(context.Background(), key, bts).Result()
			_, _ = m.redisClient.LTrim(context.Background(), key, 0, 99).Result()
		}

		// Belief entry (lightweight)
		belief := map[string]interface{}{
			"id":         fmt.Sprintf("belief_%d", time.Now().UnixNano()),
			"statement":  fmt.Sprintf("Suggestion generated for goal: %s", goalText),
			"confidence": 0.6,
			"source":     "monitor.suggest",
			"domain":     domain,
			"created_at": time.Now().UTC().Format(time.RFC3339),
		}
		if bb, err := json.Marshal(belief); err == nil {
			bkey := fmt.Sprintf("reasoning:beliefs:%s", domain)
			_, _ = m.redisClient.LPush(context.Background(), bkey, bb).Result()
			_, _ = m.redisClient.LTrim(context.Background(), bkey, 0, 199).Result()
		}

		// Explanation entry (summary)
		explanation := map[string]interface{}{
			"explanation": "Interpreter produced suggested next steps",
			"goal":        goalText,
			"created_at":  time.Now().UTC().Format(time.RFC3339),
		}
		if eb, err := json.Marshal(explanation); err == nil {
			ekey := fmt.Sprintf("reasoning:explanations:%s", strings.ToLower(goalText))
			_, _ = m.redisClient.LPush(context.Background(), ekey, eb).Result()
			_, _ = m.redisClient.LTrim(context.Background(), ekey, 0, 49).Result()
		}
	}

	// Store the result in working memory for the UI to display
	if m.redisClient != nil {
		result := map[string]interface{}{
			"type":       "goal_suggestion",
			"goal_id":    id,
			"session_id": sessionID,
			"result": func() interface{} {
				var v interface{}
				_ = json.Unmarshal(body, &v)
				return v
			}(),
			"timestamp": time.Now().UTC().Format(time.RFC3339),
		}
		if resultData, err := json.Marshal(result); err == nil {
			key := fmt.Sprintf("goal_suggestions:%s", id)
			_, _ = m.redisClient.Set(context.Background(), key, resultData, 24*time.Hour).Result()
		}
	}

	log.Printf("✅ Goal suggestion completed for goal %s", id)
}

// executeGoalSuggestedPlan calls hierarchical execute with the suggestion (fallback: goal description)
func (m *MonitorService) executeGoalSuggestedPlan(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id required"})
		return
	}

	// Optional: accept a body with a chosen plan/action
	var req struct {
		Action  string            `json:"action"`
		Context map[string]string `json:"context"`
	}
	_ = c.ShouldBindJSON(&req)

	// Start async execution and return immediately
	go m.executeGoalSuggestedPlanAsync(id, req)

	// Return immediately with status
	c.JSON(http.StatusAccepted, gin.H{
		"success": true,
		"message": "Goal execution started asynchronously",
		"goal_id": id,
		"status":  "processing",
	})
}

// executeGoalSuggestedPlanAsync performs the actual execution work asynchronously
func (m *MonitorService) executeGoalSuggestedPlanAsync(id string, req struct {
	Action  string            `json:"action"`
	Context map[string]string `json:"context"`
}) {

	// Pause guard: set Redis pause flag during manual goal execution window
	if m.redisClient != nil {
		if err := m.redisClient.Set(context.Background(), "auto_executor:paused", "1", 2*time.Minute).Err(); err != nil {
			log.Printf("[DEBUG] Failed to set pause flag for manual goal execution: %v", err)
		} else {
			log.Printf("[DEBUG] Set pause flag auto_executor:paused=1 TTL=2m for manual goal execution")
		}
	}

	// Fetch goal
	gresp, gerr := http.Get(m.goalMgrURL + "/goal/" + id)
	if gerr != nil {
		log.Printf("❌ Failed to fetch goal %s: %v", id, gerr)
		return
	}
	defer gresp.Body.Close()
	var goal map[string]interface{}
	_ = json.NewDecoder(gresp.Body).Decode(&goal)

	// Build description
	description := req.Action
	if strings.TrimSpace(description) == "" {
		if s, _ := goal["description"].(string); s != "" {
			description = s
		} else if s, _ := goal["name"].(string); s != "" {
			description = s
		} else {
			description = "Execute goal"
		}
	}
	// Debug: log what description we're working with
	fmt.Printf("[DEBUG] executeGoalSuggestedPlan description: %q\n", description)

	// Session
	sessionID := fmt.Sprintf("goal_%s", id)
	if ctx, ok := goal["context"].(map[string]interface{}); ok {
		if s, ok := ctx["session_id"].(string); ok && s != "" {
			sessionID = s
		}
	}
	if req.Context == nil {
		req.Context = map[string]string{}
	}
	req.Context["session_id"] = sessionID
	req.Context["goal_id"] = id

	// Guard: avoid duplicate execution for the same session if a recent run is still active
	// Check working memory recent events for a running goal_execution in the last 2 minutes
	{
		msURL := m.hdnURL + "/api/v1/memory/summary?session_id=" + urlQueryEscape(sessionID)
		if resp, err := http.Get(msURL); err == nil && resp != nil {
			var ms struct {
				WorkingMemory struct {
					SessionID    string                   `json:"session_id"`
					RecentEvents []map[string]interface{} `json:"recent_events"`
				} `json:"working_memory"`
			}
			_ = json.NewDecoder(resp.Body).Decode(&ms)
			resp.Body.Close()
			cutoff := time.Now().Add(-2 * time.Minute)
			isRunning := false
			for _, ev := range ms.WorkingMemory.RecentEvents {
				if tp, _ := ev["type"].(string); strings.EqualFold(tp, "goal_execution") {
					if st, _ := ev["status"].(string); strings.EqualFold(st, "running") {
						if ts, _ := ev["timestamp"].(string); ts != "" {
							if t, err := time.Parse(time.RFC3339, ts); err == nil {
								if t.After(cutoff) {
									isRunning = true
									break
								}
							}
						} else {
							// No timestamp provided; conservatively treat as running
							isRunning = true
							break
						}
					}
				}
			}
			if isRunning {
				log.Printf("⚠️ Execution already running for goal %s session %s", id, sessionID)
				return
			}
		}
	}
	// Extract project ID from user input if specified
	if projectID, found := m.extractProjectIDFromText(description); found {
		req.Context["project_id"] = projectID
		log.Printf("🎯 Manual request: using project ID %s from user input", projectID)
	} else if _, ok := req.Context["project_id"]; !ok {
		// Fallback to Goals project if no project specified
		req.Context["project_id"] = "Goals"
		log.Printf("🎯 Manual request: using default Goals project")
	}

	// If the action explicitly requests any tool, call the tool directly and return
	if strings.HasPrefix(strings.ToLower(description), "invoke tool_") {
		// Debug: log what we're detecting
		fmt.Printf("[DEBUG] Tool detected in description: %q\n", description)

		// Parse key=value params loosely from description
		params := map[string]interface{}{}
		lower := strings.ToLower(description)

		// Extract tool name from description
		toolName := ""
		if strings.Contains(lower, "tool_http_get") {
			toolName = "tool_http_get"
		} else if strings.Contains(lower, "tool_html_scraper") {
			toolName = "tool_html_scraper"
		} else if strings.Contains(lower, "tool_wiki_bootstrapper") {
			toolName = "tool_wiki_bootstrapper"
		} else {
			// Generic tool extraction - look for "tool_" followed by word characters
			start := strings.Index(lower, "tool_")
			if start >= 0 {
				end := start + 5 // after "tool_"
				for end < len(lower) && (lower[end] >= 'a' && lower[end] <= 'z' || lower[end] == '_') {
					end++
				}
				toolName = lower[start:end]
			}
		}

		if toolName == "" {
			log.Printf("❌ Could not identify tool from description for goal %s: %s", id, description)
			return
		}

		// Extract URL parameter for HTTP tools
		if toolName == "tool_http_get" || toolName == "tool_html_scraper" {
			if strings.Contains(lower, "url=") {
				params["url"] = extractStringParam(description, "url")
			}
		}

		// Parse other parameters for wiki bootstrapper
		if toolName == "tool_wiki_bootstrapper" {
			// crude extraction by splitting on ';' and ','
			parts := strings.Split(description, ";")
			// also consider commas inside seeds list later
			for _, p := range parts {
				p = strings.TrimSpace(p)
				if strings.Contains(p, "seeds=") {
					v := strings.TrimSpace(p[strings.Index(p, "seeds=")+len("seeds="):])
					// cut trailing tokens if any
					if i := strings.Index(v, " "); i > 0 {
						v = v[:i]
					}
					params["seeds"] = v
				}
				if strings.Contains(lower, "max_depth=") {
					// extract integer uses the original description to be safe
					params["max_depth"] = extractIntParam(description, "max_depth")
				}
				if strings.Contains(lower, "max_nodes=") {
					params["max_nodes"] = extractIntParam(description, "max_nodes")
				}
				if strings.Contains(lower, "rpm=") {
					params["rpm"] = extractIntParam(description, "rpm")
				}
				if strings.Contains(lower, "job_id=") {
					params["job_id"] = extractStringParam(description, "job_id")
				}
				if strings.Contains(lower, "domain=") {
					params["domain"] = extractStringParam(description, "domain")
				}
			}
		}

		tb, _ := json.Marshal(params)
		toolClient := &http.Client{Timeout: 180 * time.Second}
		toolURL := m.hdnURL + "/api/v1/tools/" + toolName + "/invoke"
		fmt.Printf("[DEBUG] Invoking tool %s with URL: %s\n", toolName, toolURL)
		fmt.Printf("[DEBUG] Tool parameters: %s\n", string(tb))
		tResp, tErr := toolClient.Post(toolURL, "application/json", bytes.NewReader(tb))
		if tErr != nil {
			log.Printf("❌ Failed to invoke tool %s for goal %s: %v", toolName, id, tErr)
			return
		}
		defer tResp.Body.Close()
		tBody, _ := io.ReadAll(tResp.Body)
		if tResp.StatusCode < 200 || tResp.StatusCode >= 300 {
			log.Printf("❌ Tool invocation error for goal %s tool %s: %s - %s", id, toolName, tResp.Status, string(tBody))
			return
		}
		// Store the tool result and update goal status
		result := map[string]interface{}{
			"success":     true,
			"tool_name":   toolName,
			"tool_output": string(tBody),
			"executed_at": time.Now().UTC().Format(time.RFC3339),
			"description": description,
		}

		// Update goal status to achieved via Goal Manager
		goalUpdateURL := m.goalMgrURL + "/goal/" + id + "/achieve"
		goalUpdatePayload := map[string]interface{}{
			"result": result,
		}
		goalUpdateData, _ := json.Marshal(goalUpdatePayload)
		goalUpdateReq, _ := http.NewRequest("POST", goalUpdateURL, bytes.NewReader(goalUpdateData))
		goalUpdateReq.Header.Set("Content-Type", "application/json")
		goalUpdateResp, _ := http.DefaultClient.Do(goalUpdateReq)
		if goalUpdateResp != nil {
			goalUpdateResp.Body.Close()
		}

		// Store result in working memory for Monitor UI to display
		if sessionID := req.Context["session_id"]; sessionID != "" {
			wmEvent := map[string]interface{}{
				"type":      "goal_completion",
				"goal_id":   id,
				"tool_name": toolName,
				"success":   true,
				"result":    result,
				"timestamp": time.Now().UTC().Format(time.RFC3339),
			}
			wmData, _ := json.Marshal(wmEvent)
			http.Post(m.hdnURL+"/api/v1/state/session/"+url.PathEscape(sessionID)+"/working_memory/event", "application/json", strings.NewReader(string(wmData)))
		}

		// Log the successful tool execution
		log.Printf("✅ Tool execution completed for goal %s: %s", id, toolName)
		return
	}

	// Execute via HDN hierarchical endpoint (default path)
	payload := map[string]interface{}{
		"task_name":    "Goal Execution",
		"description":  description,
		"context":      req.Context,
		"user_request": description,
		// Set top-level project_id to ensure linkage like Execute path
		"project_id": req.Context["project_id"],
	}
	b, _ := json.Marshal(payload)
	execClient := &http.Client{Timeout: 120 * time.Second}
	var eresp *http.Response
	var err error
	for attempt := 1; attempt <= 3; attempt++ {
		log.Printf("[DEBUG] POST hierarchical/execute attempt %d desc=%q project=%s", attempt, description, req.Context["project_id"])
		eresp, err = execClient.Post(m.hdnURL+"/api/v1/hierarchical/execute", "application/json", strings.NewReader(string(b)))
		if err == nil {
			break
		}
		if attempt < 3 {
			backoff := time.Duration(1<<uint(attempt-1)) * time.Second
			log.Printf("⚠️ hierarchical/execute attempt %d failed: %v (retrying in %s)", attempt, err, backoff)
			time.Sleep(backoff)
		}
	}
	if err != nil {
		log.Printf("❌ Failed to start execution for goal %s: %v", id, err)
		return
	}
	defer eresp.Body.Close()
	body, _ := io.ReadAll(eresp.Body)
	if eresp.StatusCode < 200 || eresp.StatusCode >= 300 {
		log.Printf("❌ Execution error for goal %s: %s - %s", id, eresp.Status, string(body))
		return
	}

	// If hierarchical exec started, also trigger an intelligent execution to generate artifacts
	// This mirrors the Execute path so workflows display artifacts and are linked to the project
	var hier struct {
		Success    bool   `json:"success"`
		WorkflowID string `json:"workflow_id"`
		Message    string `json:"message"`
	}
	_ = json.Unmarshal(body, &hier)

	// Record a started event so Goals/FSM reflect progress immediately
	if sid := req.Context["session_id"]; strings.TrimSpace(sid) != "" {
		wmStart := map[string]interface{}{
			"type":        "goal_execution",
			"task_name":   description,
			"status":      "running",
			"workflow_id": hier.WorkflowID,
			"timestamp":   time.Now().UTC().Format(time.RFC3339),
		}
		bws, _ := json.Marshal(wmStart)
		_, _ = http.Post(m.hdnURL+"/api/v1/state/session/"+url.PathEscape(sid)+"/working_memory/event", "application/json", strings.NewReader(string(bws)))
	}

	ictx := map[string]string{}
	for k, v := range req.Context {
		ictx[k] = v
	}
	// Prefer traditional executor path for artifact generation and ensure session_id
	ictx["prefer_traditional"] = "true"
	if req.Context["session_id"] != "" {
		ictx["session_id"] = req.Context["session_id"]
	}
	// Force artifacts wrapper to ensure code files are generated when filenames are present
	ictx["artifacts_wrapper"] = "true"

	// Extract artifact hints and desired language from the description
	files, wantPDF, wantPreview := extractArtifactsFromInput(description)
	if len(files) > 0 {
		ictx["artifact_names"] = strings.Join(files, ",")
		ictx["save_code_filename"] = files[0]
	}
	if wantPDF {
		ictx["save_pdf"] = "true"
	}
	if wantPreview {
		ictx["want_preview"] = "true"
	}
	// Language detection from artifacts and keywords with priority: go > javascript > java > python
	lang := "python"
	lowerDesc := strings.ToLower(description)
	// From filenames (strongest signal)
	hasGo, hasJS, hasJava := false, false, false
	for _, f := range files {
		lf := strings.ToLower(f)
		if strings.HasSuffix(lf, ".go") {
			hasGo = true
		} else if strings.HasSuffix(lf, ".js") {
			hasJS = true
		} else if strings.HasSuffix(lf, ".java") {
			hasJava = true
		}
	}
	if hasGo {
		lang = "go"
	} else if hasJS {
		lang = "javascript"
	} else if hasJava {
		lang = "java"
	} else {
		// From keywords (fallback)
		if strings.Contains(lowerDesc, "golang") || strings.Contains(lowerDesc, " go ") || strings.HasSuffix(lowerDesc, " go") || strings.Contains(lowerDesc, " go,") || strings.Contains(lowerDesc, " go.") {
			lang = "go"
		} else if strings.Contains(lowerDesc, "javascript") || strings.Contains(lowerDesc, " node ") || strings.Contains(lowerDesc, ".js") || strings.Contains(lowerDesc, " typescript") {
			lang = "javascript"
		} else if (strings.Contains(lowerDesc, ".java") || strings.Contains(lowerDesc, " java ") || strings.HasSuffix(lowerDesc, " java") || strings.Contains(lowerDesc, " in java") || strings.Contains(lowerDesc, " java.")) && !strings.Contains(lowerDesc, "javascript") {
			lang = "java"
		}
	}
	// Project-based language override: if project suggests Go, force Go
	if pid := req.Context["project_id"]; strings.Contains(strings.ToLower(pid), "go") || strings.Contains(strings.ToLower(pid), "golang") {
		lang = "go"
	}

	iPayload := map[string]interface{}{
		"task_name":        "artifact_task",
		"description":      description,
		"context":          ictx,
		"language":         lang,
		"force_regenerate": true,
		"project_id":       req.Context["project_id"],
		"max_retries":      2,
	}
	ib, _ := json.Marshal(iPayload)
	intelClient := &http.Client{Timeout: 120 * time.Second}
	var iresp *http.Response
	var ierr error
	for attempt := 1; attempt <= 3; attempt++ {
		log.Printf("[DEBUG] POST intelligent/execute attempt %d desc=%q lang=%s project=%s", attempt, description, lang, req.Context["project_id"])
		iresp, ierr = intelClient.Post(m.hdnURL+"/api/v1/intelligent/execute", "application/json", strings.NewReader(string(ib)))
		if ierr == nil {
			break
		}
		if attempt < 3 {
			backoff := time.Duration(1<<uint(attempt-1)) * time.Second
			log.Printf("⚠️ intelligent/execute attempt %d failed: %v (retrying in %s)", attempt, ierr, backoff)
			time.Sleep(backoff)
		}
	}
	var iwf string
	if ierr == nil {
		defer iresp.Body.Close()
		var ibody struct {
			Success    bool   `json:"success"`
			WorkflowID string `json:"workflow_id"`
		}
		_ = json.NewDecoder(iresp.Body).Decode(&ibody)
		if ibody.Success {
			iwf = ibody.WorkflowID
		}
	}

	// Always mark a completion event (success or failure) so PENDING clears
	if sid := req.Context["session_id"]; strings.TrimSpace(sid) != "" {
		status := "completed"
		if iwf == "" && hier.WorkflowID == "" { // if we failed to launch any workflow
			status = "failed"
		}
		wmDone := map[string]interface{}{
			"type":      "goal_execution",
			"task_name": description,
			"status":    status,
			"workflow_id": func() string {
				if iwf != "" {
					return iwf
				}
				return hier.WorkflowID
			}(),
			"timestamp": time.Now().UTC().Format(time.RFC3339),
		}
		bwd, _ := json.Marshal(wmDone)
		_, _ = http.Post(m.hdnURL+"/api/v1/state/session/"+url.PathEscape(sid)+"/working_memory/event", "application/json", strings.NewReader(string(bwd)))
	}

	// Kick off a background watcher to mark the goal achieved when the workflow finishes
	go m.watchWorkflowAndAchieveGoal(func() string {
		if iwf != "" {
			return iwf
		}
		return hier.WorkflowID
	}(), id, description)

	// Best-effort: log minimal execution trace and belief under inferred domain
	if m.redisClient != nil {
		domain := "General"
		if d := extractStringParam(description, "domain"); d != "" {
			domain = d
		}
		trace := map[string]interface{}{
			"id":         fmt.Sprintf("trace_%d", time.Now().UnixNano()),
			"goal":       description,
			"action":     "execute",
			"conclusion": "Execution started",
			"confidence": 0.7,
			"domain":     domain,
			"created_at": time.Now().UTC().Format(time.RFC3339),
			"steps": []map[string]interface{}{
				{
					"step_number": 1,
					"action":      "plan",
					"reasoning":   "Selected plan/action for execution",
					"confidence":  0.7,
				},
			},
			"workflow_id": func() string {
				if iwf != "" {
					return iwf
				}
				return hier.WorkflowID
			}(),
		}
		if bts, err := json.Marshal(trace); err == nil {
			key := fmt.Sprintf("reasoning:traces:%s", domain)
			_, _ = m.redisClient.LPush(context.Background(), key, bts).Result()
			_, _ = m.redisClient.LTrim(context.Background(), key, 0, 99).Result()
		}
		belief := map[string]interface{}{
			"id":         fmt.Sprintf("belief_%d", time.Now().UnixNano()),
			"statement":  fmt.Sprintf("Execution started for goal: %s", description),
			"confidence": 0.7,
			"source":     "monitor.execute",
			"domain":     domain,
			"created_at": time.Now().UTC().Format(time.RFC3339),
		}
		if bb, err := json.Marshal(belief); err == nil {
			bkey := fmt.Sprintf("reasoning:beliefs:%s", domain)
			_, _ = m.redisClient.LPush(context.Background(), bkey, bb).Result()
			_, _ = m.redisClient.LTrim(context.Background(), bkey, 0, 199).Result()
		}
	}

	// Log the successful execution
	workflowID := func() string {
		if iwf != "" {
			return iwf
		}
		return hier.WorkflowID
	}()
	log.Printf("✅ Goal execution completed for goal %s: workflow %s", id, workflowID)
}

// watchWorkflowAndAchieveGoal polls for workflow completion and marks the goal achieved
func (m *MonitorService) watchWorkflowAndAchieveGoal(workflowID, goalID, description string) {
	if strings.TrimSpace(workflowID) == "" || strings.TrimSpace(goalID) == "" {
		return
	}
	// Poll up to ~2 minutes
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		completed := false
		// Prefer fast path for intelligent_ workflows via Redis set membership
		if strings.HasPrefix(workflowID, "intelligent_") {
			if m.redisClient != nil {
				if member, err := m.redisClient.SIsMember(context.Background(), "active_workflows", workflowID).Result(); err == nil {
					if !member {
						completed = true
					}
				}
			}
		}
		// Fallback for hierarchical workflows: try details endpoint; consider completed if no running/pending steps
		if !completed {
			detailsURL := m.hdnURL + "/api/v1/hierarchical/workflow/" + url.PathEscape(workflowID) + "/details"
			if resp, err := http.Get(detailsURL); err == nil && resp != nil {
				var payload struct {
					Success bool                   `json:"success"`
					Details map[string]interface{} `json:"details"`
				}
				_ = json.NewDecoder(resp.Body).Decode(&payload)
				resp.Body.Close()
				if payload.Success && payload.Details != nil {
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
						}
					}
				}
			}
		}
		if completed {
			// Mark achieved in Goal Manager (best-effort)
			goalUpdateURL := m.goalMgrURL + "/goal/" + goalID + "/achieve"
			result := map[string]interface{}{
				"success":     true,
				"executed_at": time.Now().UTC().Format(time.RFC3339),
				"description": description,
				"workflow_id": workflowID,
			}
			body, _ := json.Marshal(map[string]interface{}{"result": result})
			req, _ := http.NewRequest("POST", goalUpdateURL, bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			_ = req.ParseForm()
			_, _ = http.DefaultClient.Do(req)
			return
		}
		time.Sleep(3 * time.Second)
	}
}

// createGoalFromNL converts a natural language request into a goal and creates it via Goal Manager
func (m *MonitorService) createGoalFromNL(c *gin.Context) {
	var req struct {
		AgentID     string            `json:"agent_id"`
		Description string            `json:"description"`
		Priority    string            `json:"priority"`
		Context     map[string]string `json:"context"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
		return
	}
	if strings.TrimSpace(req.Description) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "description required"})
		return
	}
	if req.AgentID == "" {
		req.AgentID = "agent_1"
	}
	// Extract project ID from user input if specified
	var projectID string
	if extractedProjectID, found := m.extractProjectIDFromText(req.Description); found {
		projectID = extractedProjectID
		log.Printf("🎯 Goal creation: extracted project ID %s from user input", projectID)
	}

	// Build goal payload compatible with Goal Manager
	// User-created goals get high priority by default to take precedence over system tasks
	priority := strings.TrimSpace(req.Priority)
	if priority == "" {
		priority = "high" // User goals take priority over system-generated goals
	}
	payload := map[string]interface{}{
		"description": req.Description,
		"priority":    priority,
		"origin":      "ui:nl",
		"status":      "active",
	}

	// Add project context if extracted
	if projectID != "" {
		if req.Context == nil {
			req.Context = make(map[string]string)
		}
		req.Context["project_id"] = projectID
	}
	b, _ := json.Marshal(payload)
	
	// Use HTTP client with timeout
	client := &http.Client{Timeout: 10 * time.Second}
	goalURL := m.goalMgrURL + "/goal"
	log.Printf("📤 Creating goal via Goal Manager at %s", goalURL)
	
	resp, err := client.Post(goalURL, "application/json", strings.NewReader(string(b)))
	if err != nil {
		log.Printf("❌ Failed to connect to Goal Manager at %s: %v", goalURL, err)
		c.JSON(http.StatusBadGateway, gin.H{
			"error":   "failed to create goal",
			"message": fmt.Sprintf("Cannot connect to Goal Manager at %s. Please check if the service is running.", m.goalMgrURL),
			"details": err.Error(),
		})
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.Printf("❌ Goal Manager returned error status %d: %s", resp.StatusCode, string(body))
		c.JSON(resp.StatusCode, gin.H{
			"error":   "failed to create goal",
			"message": fmt.Sprintf("Goal Manager returned error: %s", resp.Status),
			"details": string(body),
		})
		return
	}
	
	c.Data(resp.StatusCode, "application/json", body)
}
