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
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// chatPage renders the chat interface
func (m *MonitorService) chatPage(c *gin.Context) {
	c.HTML(http.StatusOK, "chat.html", gin.H{
		"title":  "AI Chat Interface",
		"hdnURL": m.hdnURL,
	})
}

// chatAPI handles chat requests
func (m *MonitorService) chatAPI(c *gin.Context) {

	isV1API := strings.HasPrefix(c.Request.URL.Path, "/api/v1/chat")

	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to read request body"})
		return
	}

	var chatReq map[string]interface{}

	if isV1API {

		if err := json.Unmarshal(body, &chatReq); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON"})
			return
		}

		if _, ok := chatReq["show_thinking"]; !ok {
			chatReq["show_thinking"] = true
		}
	} else {
		// For old API, use the old format
		var req struct {
			Message   string `json:"message"`
			SessionID string `json:"session_id"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON"})
			return
		}
		chatReq = map[string]interface{}{
			"message":       req.Message,
			"session_id":    req.SessionID,
			"show_thinking": true,
		}
	}

	jsonData, _ := json.Marshal(chatReq)

	client := &http.Client{
		Timeout: 360 * time.Second,
	}

	resp, err := client.Post(m.hdnURL+"/api/v1/chat", "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		log.Printf("❌ [MONITOR] Chat API error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("Failed to connect to chat service at %s: %v", m.hdnURL+"/api/v1/chat", err),
		})
		return
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read response"})
		return
	}

	if resp.StatusCode != http.StatusOK {
		c.JSON(http.StatusBadGateway, gin.H{"error": string(respBody)})
		return
	}

	if isV1API {
		c.Data(http.StatusOK, "application/json", respBody)
		return
	}

	// For old API, return just the text (backward compatibility)
	var chatResponse map[string]interface{}
	if err := json.Unmarshal(respBody, &chatResponse); err == nil {
		if responseText, ok := chatResponse["response"].(string); ok {
			c.String(http.StatusOK, responseText)
			return
		}
	}

	c.String(http.StatusOK, string(respBody))
}

// getChatSessions proxies chat sessions request to HDN
func (m *MonitorService) getChatSessions(c *gin.Context) {
	url := m.hdnURL + "/api/v1/chat/sessions"
	resp, err := http.Get(url)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("Failed to connect to HDN server at %s: %v", url, err),
		})
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read response"})
		return
	}

	if resp.StatusCode == http.StatusNotFound {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error":   "Conversational API not available. The HDN server's conversational layer may not be initialized. Ensure the LLM client is properly configured.",
			"details": string(body),
		})
		return
	}

	if resp.StatusCode != http.StatusOK {
		c.JSON(http.StatusBadGateway, gin.H{
			"error":   fmt.Sprintf("HDN server returned status %d", resp.StatusCode),
			"details": string(body),
		})
		return
	}

	c.Data(resp.StatusCode, "application/json", body)
}

// getSessionThoughts proxies session thoughts request to HDN
func (m *MonitorService) getSessionThoughts(c *gin.Context) {
	sessionId := c.Param("sessionId")
	limit := c.DefaultQuery("limit", "50")

	url := fmt.Sprintf("%s/api/v1/chat/sessions/%s/thoughts?limit=%s", m.hdnURL, sessionId, limit)
	resp, err := http.Get(url)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to connect to chat service"})
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read response"})
		return
	}

	if resp.StatusCode != http.StatusOK {
		c.JSON(http.StatusBadGateway, gin.H{"error": string(body)})
		return
	}

	c.Data(resp.StatusCode, "application/json", body)
}

// streamSessionThoughts proxies session thoughts stream to HDN
func (m *MonitorService) streamSessionThoughts(c *gin.Context) {
	sessionId := c.Param("sessionId")

	url := fmt.Sprintf("%s/api/v1/chat/sessions/%s/thoughts/stream", m.hdnURL, sessionId)
	resp, err := http.Get(url)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to connect to chat service"})
		return
	}
	defer resp.Body.Close()

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("Access-Control-Allow-Origin", "*")

	io.Copy(c.Writer, resp.Body)
}

// expressSessionThoughts proxies session thoughts express request to HDN
func (m *MonitorService) expressSessionThoughts(c *gin.Context) {
	sessionId := c.Param("sessionId")

	url := fmt.Sprintf("%s/api/v1/chat/sessions/%s/thoughts/express", m.hdnURL, sessionId)
	resp, err := http.Post(url, "application/json", c.Request.Body)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to connect to chat service"})
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read response"})
		return
	}

	if resp.StatusCode != http.StatusOK {
		c.JSON(http.StatusBadGateway, gin.H{"error": string(body)})
		return
	}

	c.Data(resp.StatusCode, "application/json", body)
}

// getSessionHistory proxies session history request to HDN
func (m *MonitorService) getSessionHistory(c *gin.Context) {
	sessionId := c.Param("sessionId")
	limit := c.DefaultQuery("limit", "50")

	url := fmt.Sprintf("%s/api/v1/chat/sessions/%s/history?limit=%s", m.hdnURL, sessionId, limit)
	resp, err := http.Get(url)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to connect to chat service"})
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read response"})
		return
	}

	if resp.StatusCode != http.StatusOK {
		c.JSON(http.StatusBadGateway, gin.H{"error": string(body)})
		return
	}

	c.Data(resp.StatusCode, "application/json", body)
}

// interpretNaturalLanguage handles natural language interpretation requests
func (m *MonitorService) interpretNaturalLanguage(c *gin.Context) {
	var req struct {
		Input     string            `json:"input"`
		Context   map[string]string `json:"context,omitempty"`
		SessionID string            `json:"session_id,omitempty"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON", "details": err.Error()})
		return
	}

	if req.Context == nil {
		req.Context = make(map[string]string)
	}
	if req.SessionID == "" {
		req.SessionID = fmt.Sprintf("session_%d", time.Now().UnixNano())
	}

	if created, resp := m.tryCreateProjectFromInput(req.Input); created {
		c.JSON(http.StatusOK, resp)
		return
	}

	client := &http.Client{Timeout: 30 * time.Second}
	url := m.hdnURL + "/api/v1/interpret"

	reqBodyBytes, err := json.Marshal(req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to marshal request"})
		return
	}

	// Backoff wrapper for HDN interpreter POST
	var resp *http.Response
	for attempt := 1; attempt <= 3; attempt++ {
		resp, err = client.Post(url, "application/json", strings.NewReader(string(reqBodyBytes)))
		if err == nil {
			break
		}
		if attempt < 3 {
			backoff := time.Duration(1<<uint(attempt-1)) * time.Second
			log.Printf("⚠️ HDN interpret attempt %d failed: %v (retrying in %s)", attempt, err, backoff)
			time.Sleep(backoff)
		}
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to communicate with HDN server", "details": err.Error()})
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read response"})
		return
	}

	c.Header("Content-Type", "application/json")
	c.Data(resp.StatusCode, "application/json", body)
}

// interpretAndExecute handles natural language interpretation and execution requests
func (m *MonitorService) interpretAndExecute(c *gin.Context) {
	var req struct {
		Input     string            `json:"input"`
		Context   map[string]string `json:"context,omitempty"`
		SessionID string            `json:"session_id,omitempty"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON", "details": err.Error()})
		return
	}

	if req.Context == nil {
		req.Context = make(map[string]string)
	}
	if req.SessionID == "" {
		req.SessionID = fmt.Sprintf("session_%d", time.Now().UnixNano())
	}

	if created, resp := m.tryCreateProjectFromInput(req.Input); created {
		c.JSON(http.StatusOK, map[string]interface{}{
			"success":        true,
			"message":        resp["message"],
			"interpretation": map[string]interface{}{"tasks": []interface{}{}},
			"execution_plan": []interface{}{},
			"project":        resp["project"],
		})
		return
	}

	httpClient := &http.Client{Timeout: 65 * time.Second}

	if m.redisClient != nil {
		if err := m.redisClient.Set(context.Background(), "auto_executor:paused", "1", 2*time.Minute).Err(); err != nil {
			log.Printf("[DEBUG] Failed to set pause flag: %v", err)
		} else {
			log.Printf("[DEBUG] Set pause flag auto_executor:paused=1 TTL=2m for manual NL Execute")
		}
	}

	projectID := ""
	if pid, ok := m.extractProjectIDFromText(req.Input); ok {
		projectID = pid
		log.Printf("[DEBUG] interpretAndExecute resolved project_id: %s", projectID)
	}

	projectNameHint := extractProjectNameFromText(req.Input)
	if strings.TrimSpace(projectNameHint) != "" {
		log.Printf("[DEBUG] interpretAndExecute project name hint: %s", projectNameHint)
	}

	if strings.Contains(strings.ToLower(req.Input), "package main") ||
		strings.Contains(strings.ToLower(req.Input), "func main()") ||
		strings.Contains(strings.ToLower(req.Input), "main.go") {
		files, wantPDF, wantPreview := extractArtifactsFromInput(req.Input)
		lang := detectLanguage(req.Input, files)
		projectIDLower := strings.ToLower(projectID)
		projectNameLower := strings.ToLower(projectNameHint)
		if strings.Contains(projectIDLower, "rust") || strings.Contains(projectNameLower, "rust") {
			lang = "rust"
			log.Printf("[DEBUG] fast-path(language) override by project (%s or %s) => rust", projectID, projectNameHint)
		} else if strings.Contains(projectIDLower, "go") || strings.Contains(projectIDLower, "golang") ||
			strings.Contains(projectNameLower, "go") || strings.Contains(projectNameLower, "golang") {
			lang = "go"
			log.Printf("[DEBUG] fast-path(language) override by project (%s or %s) => go", projectID, projectNameHint)
		}
		ctxCopy := make(map[string]string)
		for k, v := range req.Context {
			ctxCopy[k] = v
		}
		ctxCopy["prefer_traditional"] = "true"
		ctxCopy["artifacts_wrapper"] = "true"
		if req.SessionID != "" {
			ctxCopy["session_id"] = req.SessionID
		}
		if len(files) > 0 {
			ctxCopy["artifact_names"] = strings.Join(files, ",")
			ctxCopy["save_code_filename"] = files[0]
		}
		if wantPDF {
			ctxCopy["save_pdf"] = "true"
		}
		if wantPreview {
			ctxCopy["want_preview"] = "true"
		}
		payload := map[string]interface{}{
			"task_name":        "artifact_task",
			"description":      req.Input,
			"context":          ctxCopy,
			"language":         lang,
			"force_regenerate": true,
		}
		if projectID != "" {
			payload["project_id"] = projectID
		}
		bts, _ := json.Marshal(payload)
		log.Printf("[DEBUG] Fast-path POST intelligent/execute lang=%s project=%s files=%v", lang, projectID, files)
		resp2, err2 := httpClient.Post(m.hdnURL+"/api/v1/intelligent/execute", "application/json", strings.NewReader(string(bts)))
		if err2 != nil {
			c.JSON(http.StatusBadGateway, gin.H{"success": false, "error": err2.Error(), "message": "fast-path intelligent execute failed"})
			return
		}
		defer resp2.Body.Close()
		var out2 struct {
			Success    bool   `json:"success"`
			WorkflowID string `json:"workflow_id"`
			Error      string `json:"error"`
		}
		_ = json.NewDecoder(resp2.Body).Decode(&out2)
		if out2.Success {

			if req.SessionID != "" {
				wmStart := map[string]interface{}{
					"type":        "execution",
					"task_name":   req.Input,
					"status":      "running",
					"workflow_id": out2.WorkflowID,
					"timestamp":   time.Now().UTC().Format(time.RFC3339),
				}
				bws, _ := json.Marshal(wmStart)
				_, _ = http.Post(m.hdnURL+"/api/v1/state/session/"+url.PathEscape(req.SessionID)+"/working_memory/event", "application/json", strings.NewReader(string(bws)))
			}

			if m.redisClient != nil && strings.HasPrefix(out2.WorkflowID, "intelligent_") && req.SessionID != "" {
				go func(wfid, sessionID, desc string) {
					deadline := time.Now().Add(3 * time.Minute)
					for time.Now().Before(deadline) {
						member, err := m.redisClient.SIsMember(context.Background(), "active_workflows", wfid).Result()
						if err == nil && !member {
							wmDone := map[string]interface{}{
								"type":        "execution",
								"task_name":   desc,
								"status":      "completed",
								"workflow_id": wfid,
								"timestamp":   time.Now().UTC().Format(time.RFC3339),
							}
							bwd, _ := json.Marshal(wmDone)
							_, _ = http.Post(m.hdnURL+"/api/v1/state/session/"+url.PathEscape(sessionID)+"/working_memory/event", "application/json", strings.NewReader(string(bwd)))
							return
						}
						time.Sleep(2 * time.Second)
					}
				}(out2.WorkflowID, req.SessionID, req.Input)
			}
			c.JSON(http.StatusOK, gin.H{
				"success":        true,
				"message":        "executed via fast-path",
				"workflow_id":    out2.WorkflowID,
				"interpretation": map[string]interface{}{"tasks": []interface{}{}},
				"execution_plan": []interface{}{},
			})
			return
		}
		c.JSON(http.StatusBadGateway, gin.H{"success": false, "error": out2.Error, "message": "fast-path intelligent execute did not succeed"})
		return
	}

	if isLikelyMultiStepArtifactRequest(req.Input) {

		projectID := ""
		if pid, ok := m.extractProjectIDFromText(req.Input); ok {
			projectID = pid
		}

		payload := map[string]interface{}{
			"task_name":    "Hierarchical Task",
			"description":  "Auto-detected multi-step artifact request",
			"context":      req.Context,
			"user_request": req.Input,
		}

		if payload["context"] == nil {
			payload["context"] = map[string]string{}
		}
		if ctxMap, ok := payload["context"].(map[string]string); ok {
			if req.SessionID != "" {
				ctxMap["session_id"] = req.SessionID
			}
		}
		if projectID != "" {
			payload["project_id"] = projectID
		}
		b, _ := json.Marshal(payload)
		httpReq, _ := http.NewRequest("POST", m.hdnURL+"/api/v1/hierarchical/execute", strings.NewReader(string(b)))
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("X-Request-Source", "ui")
		resp, err := httpClient.Do(httpReq)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start hierarchical workflow", "details": err.Error()})
			return
		}
		defer resp.Body.Close()
		var body struct {
			Success    bool   `json:"success"`
			WorkflowID string `json:"workflow_id"`
			Message    string `json:"message"`
			Error      string `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&body)
		if resp.StatusCode >= 200 && resp.StatusCode < 300 && body.Success {

			ictx := req.Context
			if ictx == nil {
				ictx = make(map[string]string)
			}
			files, wantPDF, wantPreview := extractArtifactsFromInput(req.Input)
			if len(files) > 0 {

				ictx["save_code_filename"] = files[0]

				ictx["artifact_names"] = strings.Join(files, ",")
			}
			if wantPDF {
				ictx["save_pdf"] = "true"
				// Add derived PDFs for any .py files if not already present
				var extras []string
				for _, f := range files {
					if strings.HasSuffix(strings.ToLower(f), ".py") {
						base := f[:len(f)-3]
						pdf := base + ".pdf"
						extras = append(extras, pdf)
					}
				}
				if len(extras) > 0 {
					if ictx["artifact_names"] != "" {
						ictx["artifact_names"] = ictx["artifact_names"] + "," + strings.Join(extras, ",")
					} else {
						ictx["artifact_names"] = strings.Join(extras, ",")
					}
				}
			}
			if wantPreview {
				ictx["want_preview"] = "true"
			}

			ictx["prefer_traditional"] = "true"

			ictx["artifacts_wrapper"] = "true"

			if req.SessionID != "" {
				ictx["session_id"] = req.SessionID
			}

			if projectID != "" {
				ictx["project_id"] = projectID
				log.Printf("[DEBUG] Propagating project_id %s to intelligent executor context", projectID)
			}

			// Detect artifacts and language for intelligent exec (reuse earlier parsed files/wantPDF/wantPreview)
			// If both .go and .py are requested, run two executes so both artifacts are generated
			var iworkflow string
			hasGo, hasPy := false, false
			for _, f := range files {
				lf := strings.ToLower(f)
				if strings.HasSuffix(lf, ".go") {
					hasGo = true
				}
				if strings.HasSuffix(lf, ".py") {
					hasPy = true
				}
			}
			runExec := func(language string, saveFile string) string {
				ctxCopy := make(map[string]string)
				for k, v := range ictx {
					ctxCopy[k] = v
				}
				if saveFile != "" {
					ctxCopy["save_code_filename"] = saveFile
				}
				payload := map[string]interface{}{
					"task_name":        "artifact_task",
					"description":      req.Input,
					"context":          ctxCopy,
					"language":         language,
					"force_regenerate": true,
					"max_retries":      2,
				}
				if projectID != "" {
					payload["project_id"] = projectID
				}
				bts, _ := json.Marshal(payload)
				resp, err := httpClient.Post(m.hdnURL+"/api/v1/intelligent/execute", "application/json", strings.NewReader(string(bts)))
				if err != nil {
					return ""
				}
				defer resp.Body.Close()
				var out struct {
					Success    bool   `json:"success"`
					WorkflowID string `json:"workflow_id"`
				}
				_ = json.NewDecoder(resp.Body).Decode(&out)
				if out.Success {
					return out.WorkflowID
				}
				return ""
			}
			if hasGo && hasPy {

				wfGo := runExec("go", func() string {
					for _, f := range files {
						if strings.HasSuffix(strings.ToLower(f), ".go") {
							return f
						}
					}
					return ""
				}())
				wfPy := runExec("python", func() string {
					for _, f := range files {
						if strings.HasSuffix(strings.ToLower(f), ".py") {
							return f
						}
					}
					return ""
				}())
				if wfGo != "" {
					iworkflow = wfGo
				} else {
					iworkflow = wfPy
				}
			} else {
				lang := detectLanguage(req.Input, files)

				save := ""
				for _, f := range files {
					if (lang == "go" && strings.HasSuffix(strings.ToLower(f), ".go")) || (lang == "python" && strings.HasSuffix(strings.ToLower(f), ".py")) {
						save = f
						break
					}
				}
				iworkflow = runExec(lang, save)
			}

			if iworkflow == "" {
				glang := detectLanguage(req.Input, files)
				gfiles, _, _ := extractArtifactsFromInput(req.Input)
				ctxCopy := make(map[string]string)
				for k, v := range ictx {
					ctxCopy[k] = v
				}
				if len(gfiles) > 0 {
					ctxCopy["save_code_filename"] = gfiles[0]
					ctxCopy["artifact_names"] = strings.Join(gfiles, ",")
				}
				payload := map[string]interface{}{
					"task_name":        "artifact_task",
					"description":      req.Input,
					"context":          ctxCopy,
					"language":         glang,
					"force_regenerate": true,
				}
				if projectID != "" {
					payload["project_id"] = projectID
				}
				bts, _ := json.Marshal(payload)
				if resp2, err2 := httpClient.Post(m.hdnURL+"/api/v1/intelligent/execute", "application/json", strings.NewReader(string(bts))); err2 == nil {
					defer resp2.Body.Close()
					var out2 struct {
						Success    bool   `json:"success"`
						WorkflowID string `json:"workflow_id"`
					}
					_ = json.NewDecoder(resp2.Body).Decode(&out2)
					if out2.Success {
						iworkflow = out2.WorkflowID
					}
				}
			}

			c.JSON(http.StatusOK, gin.H{
				"success": true,
				"message": body.Message,
				"workflow_id": func() string {
					if iworkflow != "" {
						return iworkflow
					}
					return body.WorkflowID
				}(),
				"interpretation": map[string]interface{}{"tasks": []interface{}{}},
				"execution_plan": []interface{}{},
			})
			return
		}
		c.JSON(resp.StatusCode, gin.H{
			"success": false,
			"message": body.Message,
			"error":   body.Error,
		})
		return
	}

	interpPayload, _ := json.Marshal(map[string]interface{}{
		"input":      req.Input,
		"context":    req.Context,
		"session_id": req.SessionID,
	})

	interpResp, err := httpClient.Post(m.hdnURL+"/api/v1/interpret", "application/json", strings.NewReader(string(interpPayload)))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to communicate with HDN interpreter", "details": err.Error()})
		return
	}
	defer interpResp.Body.Close()

	var interpretation map[string]interface{}
	if err := json.NewDecoder(interpResp.Body).Decode(&interpretation); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse interpretation"})
		return
	}

	tasksAny, ok := interpretation["tasks"].([]interface{})
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Interpreter returned unexpected structure"})
		return
	}

	if len(tasksAny) == 0 {
		files, wantPDF, wantPreview := extractArtifactsFromInput(req.Input)
		lang := detectLanguage(req.Input, files)

		projectIDLower := strings.ToLower(projectID)
		projectNameLower := strings.ToLower(projectNameHint)
		if strings.Contains(projectIDLower, "rust") || strings.Contains(projectNameLower, "rust") {
			lang = "rust"
			log.Printf("[DEBUG] fallback(language) override by project (%s or %s) => rust", projectID, projectNameHint)
		} else if strings.Contains(projectIDLower, "go") || strings.Contains(projectIDLower, "golang") ||
			strings.Contains(projectNameLower, "go") || strings.Contains(projectNameLower, "golang") {
			lang = "go"
			log.Printf("[DEBUG] fallback(language) override by project (%s or %s) => go", projectID, projectNameHint)
		}
		ctxCopy := make(map[string]string)
		for k, v := range req.Context {
			ctxCopy[k] = v
		}

		ctxCopy["prefer_traditional"] = "true"
		ctxCopy["artifacts_wrapper"] = "true"
		if req.SessionID != "" {
			ctxCopy["session_id"] = req.SessionID
		}
		if len(files) > 0 {
			ctxCopy["artifact_names"] = strings.Join(files, ",")
			ctxCopy["save_code_filename"] = files[0]
		}
		if wantPDF {
			ctxCopy["save_pdf"] = "true"
		}
		if wantPreview {
			ctxCopy["want_preview"] = "true"
		}
		payload := map[string]interface{}{
			"task_name":        "artifact_task",
			"description":      req.Input,
			"context":          ctxCopy,
			"language":         lang,
			"force_regenerate": true,
		}
		if projectID != "" {
			payload["project_id"] = projectID
		}
		bts, _ := json.Marshal(payload)
		log.Printf("[DEBUG] Fallback POST intelligent/execute lang=%s project=%s files=%v", lang, projectID, files)
		resp2, err2 := httpClient.Post(m.hdnURL+"/api/v1/intelligent/execute", "application/json", strings.NewReader(string(bts)))
		if err2 != nil {
			c.JSON(http.StatusBadGateway, gin.H{"success": false, "error": err2.Error(), "message": "fallback intelligent execute failed"})
			return
		}
		defer resp2.Body.Close()
		var out2 struct {
			Success    bool   `json:"success"`
			WorkflowID string `json:"workflow_id"`
			Error      string `json:"error"`
		}
		_ = json.NewDecoder(resp2.Body).Decode(&out2)
		if out2.Success {

			if req.SessionID != "" {
				wmStart := map[string]interface{}{
					"type":        "execution",
					"task_name":   req.Input,
					"status":      "running",
					"workflow_id": out2.WorkflowID,
					"timestamp":   time.Now().UTC().Format(time.RFC3339),
				}
				bws, _ := json.Marshal(wmStart)
				_, _ = http.Post(m.hdnURL+"/api/v1/state/session/"+url.PathEscape(req.SessionID)+"/working_memory/event", "application/json", strings.NewReader(string(bws)))
			}

			if m.redisClient != nil && strings.HasPrefix(out2.WorkflowID, "intelligent_") && req.SessionID != "" {
				go func(wfid, sessionID, desc string) {
					deadline := time.Now().Add(3 * time.Minute)
					for time.Now().Before(deadline) {
						member, err := m.redisClient.SIsMember(context.Background(), "active_workflows", wfid).Result()
						if err == nil && !member {
							wmDone := map[string]interface{}{
								"type":        "execution",
								"task_name":   desc,
								"status":      "completed",
								"workflow_id": wfid,
								"timestamp":   time.Now().UTC().Format(time.RFC3339),
							}
							bwd, _ := json.Marshal(wmDone)
							_, _ = http.Post(m.hdnURL+"/api/v1/state/session/"+url.PathEscape(sessionID)+"/working_memory/event", "application/json", strings.NewReader(string(bwd)))
							return
						}
						time.Sleep(2 * time.Second)
					}
				}(out2.WorkflowID, req.SessionID, req.Input)
			}
			c.JSON(http.StatusOK, gin.H{
				"success":        true,
				"message":        "executed via fallback",
				"workflow_id":    out2.WorkflowID,
				"interpretation": map[string]interface{}{"tasks": []interface{}{}},
				"execution_plan": []interface{}{},
			})
			return
		}
		c.JSON(http.StatusBadGateway, gin.H{"success": false, "error": out2.Error, "message": "fallback intelligent execute did not succeed"})
		return
	}

	// 2) Execute each task via intelligent executor to ensure workflow/artifacts are created
	type ExecPlanItem struct {
		Task       map[string]interface{} `json:"task"`
		Success    bool                   `json:"success"`
		Result     string                 `json:"result"`
		Preview    interface{}            `json:"preview,omitempty"`
		Error      string                 `json:"error"`
		ExecutedAt time.Time              `json:"executed_at"`
	}
	var execPlan []ExecPlanItem

	globalFiles, globalWantPDF, globalWantPreview := extractArtifactsFromInput(req.Input)
	globalLang := detectLanguage(req.Input, globalFiles)

	for _, t := range tasksAny {
		taskMap, _ := t.(map[string]interface{})
		taskName, _ := taskMap["task_name"].(string)
		description, _ := taskMap["description"].(string)
		language, _ := taskMap["language"].(string)
		ctxMap := make(map[string]string)

		for k, v := range req.Context {
			ctxMap[k] = v
		}
		if rawCtx, ok := taskMap["context"].(map[string]interface{}); ok {
			for k, v := range rawCtx {
				ctxMap[k] = fmt.Sprintf("%v", v)
			}
		}

		if req.SessionID != "" {
			ctxMap["session_id"] = req.SessionID
		}

		if projectID != "" {
			ctxMap["project_id"] = projectID
			log.Printf("[DEBUG] Adding project_id %s to task execution context", projectID)
		}

		if taskProjectID, ok := m.extractProjectIDFromText(description + " " + taskName); ok {
			ctxMap["project_id"] = taskProjectID
			log.Printf("[DEBUG] Extracted project_id %s from task description", taskProjectID)
		}

		filesHint, wantPDFHint, wantPreviewHint := extractArtifactsFromInput(description + " " + taskName)
		if len(filesHint) > 0 {
			ctxMap["artifact_names"] = strings.Join(filesHint, ",")
			ctxMap["save_code_filename"] = filesHint[0]
		}

		if len(filesHint) == 0 && len(globalFiles) > 0 {
			ctxMap["artifact_names"] = strings.Join(globalFiles, ",")
			ctxMap["save_code_filename"] = globalFiles[0]
		}
		if wantPDFHint {
			ctxMap["save_pdf"] = "true"
		}
		if wantPreviewHint {
			ctxMap["want_preview"] = "true"
		}

		if !wantPDFHint && globalWantPDF {
			ctxMap["save_pdf"] = "true"
		}
		if !wantPreviewHint && globalWantPreview {
			ctxMap["want_preview"] = "true"
		}

		ctxMap["prefer_traditional"] = "true"

		if strings.TrimSpace(language) == "" {
			files, _, _ := extractArtifactsFromInput(description + " " + taskName)
			language = detectLanguage(description+" "+taskName, files)
			log.Printf("[DEBUG] language detect(initial) files=%v => %s (global=%s)", files, language, globalLang)

			if strings.TrimSpace(language) == "" || (language == "python" && globalLang != "python") {
				language = globalLang
				log.Printf("[DEBUG] language override by global => %s", language)
			}
		}

		if strings.TrimSpace(language) != "" && globalLang != "" && language != globalLang {
			language = globalLang
			log.Printf("[DEBUG] language conflict, prefer global => %s", language)
		}

		projectIDLower := strings.ToLower(projectID)
		projectNameLower := strings.ToLower(projectNameHint)
		if strings.Contains(projectIDLower, "rust") || strings.Contains(projectNameLower, "rust") {
			language = "rust"
			log.Printf("[DEBUG] language override by project (%s) => rust", projectID)
		} else if strings.Contains(projectIDLower, "go") || strings.Contains(projectIDLower, "golang") ||
			strings.Contains(projectNameLower, "go") || strings.Contains(projectNameLower, "golang") {
			language = "go"
			log.Printf("[DEBUG] language override by project (%s) => go", projectID)
		}

		execPayload, _ := json.Marshal(map[string]interface{}{
			"task_name":        taskName,
			"description":      description,
			"context":          ctxMap,
			"language":         language,
			"project_id":       projectID,
			"force_regenerate": true,
		})

		var execResp *http.Response
		var err error
		for attempt := 1; attempt <= 3; attempt++ {
			log.Printf("[DEBUG] POST intelligent/execute attempt %d payload task=%q lang=%s project=%s", attempt, taskName, language, projectID)
			execResp, err = httpClient.Post(m.hdnURL+"/api/v1/intelligent/execute", "application/json", strings.NewReader(string(execPayload)))
			if err == nil {
				break
			}
			if attempt < 3 {
				backoff := time.Duration(1<<uint(attempt-1)) * time.Second
				log.Printf("⚠️ intelligent/execute attempt %d failed: %v (retrying in %s)", attempt, err, backoff)
				time.Sleep(backoff)
			}
		}
		if err != nil {
			execPlan = append(execPlan, ExecPlanItem{Task: taskMap, Success: false, Result: "", Error: err.Error(), ExecutedAt: time.Now()})
			continue
		}
		var execBody struct {
			Success    bool        `json:"success"`
			Result     interface{} `json:"result"`
			Preview    interface{} `json:"preview"`
			Error      string      `json:"error"`
			WorkflowID string      `json:"workflow_id"`
		}
		_ = json.NewDecoder(execResp.Body).Decode(&execBody)
		execResp.Body.Close()

		resultStr := ""
		if execBody.Result != nil {
			if s, ok := execBody.Result.(string); ok {
				resultStr = s
			} else {
				b, _ := json.Marshal(execBody.Result)
				resultStr = string(b)
			}
		}

		execPlan = append(execPlan, ExecPlanItem{Task: taskMap, Success: execBody.Success, Result: resultStr, Preview: execBody.Preview, Error: execBody.Error, ExecutedAt: time.Now()})
	}

	respPayload := map[string]interface{}{
		"success":        true,
		"interpretation": interpretation,
		"execution_plan": execPlan,
		"message":        fmt.Sprintf("Successfully interpreted and executed %d task(s)", len(execPlan)),
	}

	if projectID != "" {

		client := &http.Client{Timeout: 5 * time.Second}
		if resp, err := client.Get(m.hdnURL + "/api/v1/projects/" + projectID); err == nil {
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				var project map[string]interface{}
				if err := json.NewDecoder(resp.Body).Decode(&project); err == nil {
					respPayload["project"] = project
					log.Printf("[DEBUG] Added project to response: %s", projectID)
				}
			}
		}
	}

	c.JSON(http.StatusOK, respPayload)
}
