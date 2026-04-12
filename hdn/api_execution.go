package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	mempkg "hdn/memory"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

func (s *APIServer) handleExecuteTask(w http.ResponseWriter, r *http.Request) {

	release, acquired := s.acquireExecutionSlot(r)
	if !acquired {
		http.Error(w, "Server busy - too many concurrent executions. Please try again later.", http.StatusTooManyRequests)
		return
	}
	defer release()

	var req TaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	state := req.State
	if state == nil {
		state = make(State)
	}

	plan := s.planTask(state, req.TaskName)
	if plan == nil {
		response := TaskResponse{
			Success: false,
			Message: "Failed to create plan for task",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
		return
	}

	newState := s.executePlan(state, plan)

	if req.Context != nil {
		if sid, ok := req.Context["session_id"]; ok && sid != "" {
			_ = s.workingMemory.SetLatestPlan(sid, map[string]any{"plan": plan, "task": req.TaskName, "timestamp": time.Now().UTC()})
		}
	}

	response := TaskResponse{
		Success:  true,
		Plan:     plan,
		NewState: newState,
		Message:  "Task executed successfully",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (s *APIServer) handlePlanTask(w http.ResponseWriter, r *http.Request) {
	var req TaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	state := req.State
	if state == nil {
		state = make(State)
	}

	plan := s.planTask(state, req.TaskName)

	response := TaskResponse{
		Success: plan != nil,
		Plan:    plan,
		Message: func() string {
			if plan != nil {
				return "Plan created successfully"
			}
			return "Failed to create plan"
		}(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (s *APIServer) planTask(state State, taskName string) []string {

	legacyDomain := s.convertToLegacyDomain()
	return HTNPlan(state, taskName, &legacyDomain)
}

func (s *APIServer) executePlan(state State, plan []string) State {
	legacyDomain := s.convertToLegacyDomain()
	return ExecutePlan(state, plan, &legacyDomain)
}

func (s *APIServer) handleIntelligentExecute(w http.ResponseWriter, r *http.Request) {

	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

	go s.cleanupStaleActiveWorkflows(context.Background())

	release, acquired := s.acquireExecutionSlot(r)
	if !acquired {
		http.Error(w, "Server busy - too many concurrent executions. Please try again later.", http.StatusTooManyRequests)
		return
	}
	defer release()

	var req IntelligentExecutionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if req.TaskName == "" || req.Description == "" {
		http.Error(w, "Task name and description are required", http.StatusBadRequest)
		return
	}

	if req.Language == "" {

		if inferred := inferLanguageFromRequest(&req); inferred != "" {
			req.Language = inferred
			log.Printf("🔍 [API] Language inferred from request: %s", req.Language)
		} else {
			req.Language = "python"
			log.Printf("🔍 [API] No language detected, defaulting to: %s", req.Language)
		}
	} else {
		log.Printf("🔍 [API] Language explicitly provided: %s", req.Language)
	}
	if req.MaxRetries == 0 {
		req.MaxRetries = 3
	}
	if req.Timeout == 0 {
		req.Timeout = 300
	}

	executor := NewIntelligentExecutor(
		s.domainManager,
		s.codeStorage,
		s.codeGenerator,
		s.dockerExecutor,
		s.llmClient,
		s.actionManager,
		s.plannerIntegration,
		s.selfModelManager,
		s.toolMetrics,
		s.fileStorage,
		s.hdnBaseURL,
		s.redisAddr,
	)

	ctx, cancel := context.WithTimeout(r.Context(), 300*time.Second)
	defer cancel()

	highPriority := true
	if req.Priority == "low" {
		highPriority = false
	}

	result, err := executor.ExecuteTaskIntelligently(ctx, &ExecutionRequest{
		TaskName:        req.TaskName,
		Description:     req.Description,
		Context:         req.Context,
		Language:        req.Language,
		ForceRegenerate: req.ForceRegenerate,
		MaxRetries:      req.MaxRetries,
		Timeout:         req.Timeout,
		HighPriority:    highPriority,
	})

	if err != nil {

		if ctx.Err() == context.DeadlineExceeded {
			log.Printf("⏱️ [API] Execution timed out after 300 seconds for task: %s", req.TaskName)

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			response := IntelligentExecutionResponse{
				Success:       false,
				Error:         fmt.Sprintf("Execution timed out after 300 seconds: %v", err),
				ExecutionTime: 300000,
				RetryCount:    0,
				WorkflowID:    fmt.Sprintf("intelligent_%d", time.Now().UnixNano()),
			}
			json.NewEncoder(w).Encode(response)
			return
		}
		http.Error(w, fmt.Sprintf("Intelligent execution failed: %v", err), http.StatusInternalServerError)
		return
	}

	if result == nil {

		if ctx.Err() == context.DeadlineExceeded {
			log.Printf("⏱️ [API] Execution timed out (result is nil) for task: %s", req.TaskName)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			response := IntelligentExecutionResponse{
				Success:       false,
				Error:         "Execution timed out after 300 seconds (no result returned)",
				ExecutionTime: 300000,
				RetryCount:    0,
				WorkflowID:    fmt.Sprintf("intelligent_%d", time.Now().UnixNano()),
			}
			json.NewEncoder(w).Encode(response)
			return
		}
		http.Error(w, "Intelligent execution returned no result", http.StatusInternalServerError)
		return
	}

	if !result.Success && result.Error == "" {
		log.Printf("⚠️ [API] Execution failed but no error message set for task: %s", req.TaskName)

		if ctx.Err() == context.DeadlineExceeded {
			result.Error = "Execution timed out after 300 seconds"
		} else {
			result.Error = "Execution failed (no error details available)"
		}
	}

	s.recordMonitorMetrics(result.Success, result.ExecutionTime)

	if req.Context != nil {
		if sid, ok := req.Context["session_id"]; ok && sid != "" {
			_ = s.workingMemory.AddEvent(sid, map[string]any{
				"type":      "intelligent_execution",
				"task_name": req.TaskName,
				"status": func() string {
					if result.Success {
						return "completed"
					}
					return "failed"
				}(),
				"success":      result.Success,
				"error":        result.Error,
				"execution_ms": result.ExecutionTime.Milliseconds(),
				"workflow_id":  result.WorkflowID,
				"timestamp":    time.Now().UTC(),
			}, 100)
		}
	}

	if s.redis != nil {
		ctx := context.Background()
		pid := strings.TrimSpace(req.ProjectID)
		wid := strings.TrimSpace(result.WorkflowID)
		if pid == "" && req.Context != nil {
			if v, ok := req.Context["project_id"]; ok && strings.TrimSpace(v) != "" {
				pid = strings.TrimSpace(v)
			}
		}
		if pid != "" && wid != "" {
			_ = s.redis.Set(ctx, "workflow_project:"+wid, pid, 24*time.Hour).Err()
		}
	}

	if req.ProjectID != "" || (req.Context != nil && req.Context["project_id"] != "") {
		projectID := req.ProjectID
		if projectID == "" && req.Context != nil {
			if v, ok := req.Context["project_id"]; ok && strings.TrimSpace(v) != "" {
				projectID = strings.TrimSpace(v)
			}
		}
		if projectID != "" && result.WorkflowID != "" {
			pid := s.resolveProjectID(projectID)
			if linkErr := s.projectManager.LinkWorkflow(pid, result.WorkflowID); linkErr != nil {
				log.Printf("❌ [API] Failed to link intelligent workflow %s to project %s: %v", result.WorkflowID, pid, linkErr)
			} else {
				log.Printf("✅ [API] Linked intelligent workflow %s to project %s", result.WorkflowID, pid)
			}
		}
	}

	if strings.EqualFold(req.TaskName, "daily_summary") {
		go func() {
			defer func() { recover() }()
			if s.redis == nil {
				log.Printf("⚠️ [API] daily_summary: redis client is nil; skipping persistence")
				return
			}
			ctx := context.Background()

			text := s.generateDailySummaryFromSystemData(ctx)
			payload := map[string]any{
				"date":         time.Now().UTC().Format("2006-01-02"),
				"generated_at": time.Now().UTC().Format(time.RFC3339),
				"summary":      text,
			}
			b, _ := json.Marshal(payload)
			if err := s.redis.Set(ctx, "daily_summary:latest", string(b), 0).Err(); err != nil {
				log.Printf("❌ [API] daily_summary: failed to set latest: %v", err)
			} else {
				log.Printf("📝 [API] daily_summary: wrote latest (%d bytes)", len(b))
			}
			dateKey := "daily_summary:" + time.Now().UTC().Format("2006-01-02")
			if err := s.redis.Set(ctx, dateKey, string(b), 0).Err(); err != nil {
				log.Printf("❌ [API] daily_summary: failed to set %s: %v", dateKey, err)
			}
			if err := s.redis.LPush(ctx, "daily_summary:history", string(b)).Err(); err != nil {
				log.Printf("❌ [API] daily_summary: failed to LPUSH history: %v", err)
			} else {
				_ = s.redis.LTrim(ctx, "daily_summary:history", 0, 29).Err()
			}
		}()
	}

	if s.vectorDB != nil {
		sid := ""
		if req.Context != nil {
			sid = req.Context["session_id"]
		}
		ep := &mempkg.EpisodicRecord{
			SessionID: sid,
			PlanID:    "",
			Timestamp: time.Now().UTC(),
			Outcome: func() string {
				if result.Success {
					return "success"
				}
				return "failure"
			}(),
			Reward:    0,
			Tags:      []string{"intelligent"},
			StepIndex: 0,
			Text:      fmt.Sprintf("%s: %s", req.TaskName, req.Description),
			Metadata:  map[string]any{"workflow_id": result.WorkflowID},
		}
		vec := toyEmbed(ep.Text, 768)
		if err := s.vectorDB.IndexEpisode(ep, vec); err != nil {
			log.Printf("❌ [API] Weaviate indexing failed: %v", err)
		} else {
			log.Printf("✅ [API] Episode indexed in Weaviate: %s", ep.Text[:min(50, len(ep.Text))])
		}
	}

	workflowID := result.WorkflowID
	log.Printf("🔍 [API] result.WorkflowID from executor: %s", workflowID)
	if workflowID == "" {
		workflowID = fmt.Sprintf("intelligent_%d", time.Now().UnixNano())
		log.Printf("⚠️ [API] result.WorkflowID was empty, generated new ID: %s", workflowID)
	}

	storeID := s.createIntelligentWorkflowRecord(req, result, workflowID)
	log.Printf("🔍 [API] storeID returned from createIntelligentWorkflowRecord: %s", storeID)

	projectID := req.ProjectID
	if projectID == "" && req.Context != nil {
		if pid, ok := req.Context["project_id"]; ok && strings.TrimSpace(pid) != "" {
			projectID = strings.TrimSpace(pid)
		}
	}
	if projectID != "" {
		pid := s.resolveProjectID(projectID)
		if linkErr := s.projectManager.LinkWorkflow(pid, storeID); linkErr != nil {
			log.Printf("❌ [API] Failed to link workflow %s to project %s: %v", storeID, pid, linkErr)
		} else {
			log.Printf("✅ [API] Linked workflow %s to project %s", storeID, pid)
		}
	}

	if result.GeneratedCode != nil && result.GeneratedCode.Code != "" {
		codeCT := "text/plain"
		ext := ".txt"
		switch strings.ToLower(result.GeneratedCode.Language) {
		case "python", "py":
			codeCT = "text/x-python"
			ext = ".py"
		case "javascript", "js":
			codeCT = "application/javascript"
			ext = ".js"
		case "go", "golang":
			codeCT = "text/x-go"
			ext = ".go"
		case "markdown", "md":
			codeCT = "text/markdown"
			ext = ".md"
		}
		codeFilename := result.GeneratedCode.TaskName
		if codeFilename == "" {
			codeFilename = req.TaskName
		}
		if !strings.HasSuffix(strings.ToLower(codeFilename), ext) {
			codeFilename = codeFilename + ext
		}
		_ = s.fileStorage.StoreFile(&StoredFile{
			Filename:    codeFilename,
			Content:     []byte(sanitizeCode(result.GeneratedCode.Code)),
			ContentType: codeCT,
			Size:        int64(len(sanitizeCode(result.GeneratedCode.Code))),
			WorkflowID:  workflowID,
			StepID:      "final_execution",
		})
	} else if outStr, ok := result.Result.(string); ok && looksLikeCode(outStr) {
		codeCT := "text/plain"
		ext := ".txt"
		lang := strings.ToLower(req.Language)

		if lang == "" {
			if looksLikePython(outStr) {
				lang = "python"
			}
		}
		switch lang {
		case "python", "py":
			codeCT = "text/x-python"
			ext = ".py"
		case "javascript", "js":
			codeCT = "application/javascript"
			ext = ".js"
		case "go", "golang":
			codeCT = "text/x-go"
			ext = ".go"
		case "markdown", "md":
			codeCT = "text/markdown"
			ext = ".md"
		}
		codeFilename := req.TaskName
		if codeFilename == "" {
			codeFilename = "generated_code"
		}
		if !strings.HasSuffix(strings.ToLower(codeFilename), ext) {
			codeFilename += ext
		}
		_ = s.fileStorage.StoreFile(&StoredFile{
			Filename:    codeFilename,
			Content:     []byte(sanitizeCode(outStr)),
			ContentType: codeCT,
			Size:        int64(len(sanitizeCode(outStr))),
			WorkflowID:  workflowID,
			StepID:      "final_execution",
		})
	} else if result.NewAction != nil && result.NewAction.Code != "" {
		codeCT := "text/plain"
		ext := ".txt"
		lang := strings.ToLower(result.NewAction.Language)
		switch lang {
		case "python", "py":
			codeCT = "text/x-python"
			ext = ".py"
		case "javascript", "js":
			codeCT = "application/javascript"
			ext = ".js"
		case "go", "golang":
			codeCT = "text/x-go"
			ext = ".go"
		case "markdown", "md":
			codeCT = "text/markdown"
			ext = ".md"
		}
		codeFilename := result.NewAction.Task
		if codeFilename == "" {
			codeFilename = req.TaskName
		}
		if !strings.HasSuffix(strings.ToLower(codeFilename), ext) {
			codeFilename = codeFilename + ext
		}
		_ = s.fileStorage.StoreFile(&StoredFile{
			Filename:    codeFilename,
			Content:     []byte(sanitizeCode(result.NewAction.Code)),
			ContentType: codeCT,
			Size:        int64(len(sanitizeCode(result.NewAction.Code))),
			WorkflowID:  workflowID,
			StepID:      "final_execution",
		})
	}

	if result.Result != nil {
		var content []byte
		var contentType string

		filename := fmt.Sprintf("output_%s_%d.txt", strings.ReplaceAll(req.TaskName, " ", "_"), time.Now().UnixNano())
		switch v := result.Result.(type) {
		case string:
			content = []byte(sanitizeConsoleOutput(v))
			contentType = "text/plain"
		default:
			if b, err := json.Marshal(v); err == nil {
				content = b
				contentType = "application/json"
				filename = fmt.Sprintf("output_%s_%d.json", strings.ReplaceAll(req.TaskName, " ", "_"), time.Now().UnixNano())
			}
		}
		if len(content) > 0 {
			_ = s.fileStorage.StoreFile(&StoredFile{
				Filename:    filename,
				Content:     content,
				ContentType: contentType,
				Size:        int64(len(content)),
				WorkflowID:  workflowID,
				StepID:      "final_execution",
			})
		}
	}

	if req.Context != nil {

		if name, ok := req.Context["save_code_filename"]; ok && result.GeneratedCode != nil && name != "" {
			codeCT := "text/plain"
			lowerName := strings.ToLower(name)
			if strings.HasSuffix(lowerName, ".py") {
				codeCT = "text/x-python"
			} else if strings.HasSuffix(lowerName, ".go") {
				codeCT = "text/x-go"
			} else if strings.HasSuffix(lowerName, ".js") {
				codeCT = "application/javascript"
			} else if strings.HasSuffix(lowerName, ".java") {
				codeCT = "text/x-java-source"
			}
			_ = s.fileStorage.StoreFile(&StoredFile{
				Filename:    name,
				Content:     []byte(sanitizeCode(result.GeneratedCode.Code)),
				ContentType: codeCT,
				Size:        int64(len(sanitizeCode(result.GeneratedCode.Code))),
				WorkflowID:  workflowID,
				StepID:      "final_execution",
			})
		}

		existing, _ := s.fileStorage.GetFilesByWorkflow(workflowID)
		existingNames := make(map[string]struct{})
		existingPDF := false
		for _, f := range existing {
			name := strings.ToLower(f.Filename)
			existingNames[name] = struct{}{}
			if strings.HasSuffix(name, ".pdf") {
				existingPDF = true
			}
		}

		list, hasList := req.Context["artifact_names"]
		parts := []string{}
		if hasList && list != "" {
			parts = strings.Split(list, ",")
			for i := range parts {
				parts[i] = strings.TrimSpace(parts[i])
			}
		}
		if pdfFlag, ok := req.Context["save_pdf"]; ok && strings.ToLower(pdfFlag) == "true" {
			hasPDF := existingPDF
			if !hasPDF {
				for _, p := range parts {
					if strings.HasSuffix(strings.ToLower(p), ".pdf") {
						hasPDF = true
						break
					}
				}
			}

			if !hasPDF {
				parts = append(parts, "artifacts_report.pdf")
			}
		}
		if len(parts) > 0 {
			for _, fname := range parts {
				if fname == "" {
					continue
				}
				low := strings.ToLower(fname)
				if _, exists := existingNames[low]; exists {

					continue
				}
				if (strings.HasSuffix(low, ".py") || strings.HasSuffix(low, ".go") || strings.HasSuffix(low, ".js") || strings.HasSuffix(low, ".java")) && result.GeneratedCode != nil {

					contentType := "text/plain"
					switch {
					case strings.HasSuffix(low, ".py"):
						contentType = "text/x-python"
					case strings.HasSuffix(low, ".go"):
						contentType = "text/x-go"
					case strings.HasSuffix(low, ".js"):
						contentType = "application/javascript"
					case strings.HasSuffix(low, ".java"):
						contentType = "text/x-java-source"
					}
					_ = s.fileStorage.StoreFile(&StoredFile{
						Filename:    fname,
						Content:     []byte(sanitizeCode(result.GeneratedCode.Code)),
						ContentType: contentType,
						Size:        int64(len(sanitizeCode(result.GeneratedCode.Code))),
						WorkflowID:  workflowID,
						StepID:      "final_execution",
					})
				} else if strings.HasSuffix(low, ".md") {

					if result.Result != nil {
						var content []byte
						switch v := result.Result.(type) {
						case string:
							content = []byte(v)
						default:
							if b, err := json.Marshal(v); err == nil {
								content = b
							}
						}
						if len(content) > 0 {
							_ = s.fileStorage.StoreFile(&StoredFile{
								Filename:    fname,
								Content:     content,
								ContentType: "text/markdown",
								Size:        int64(len(content)),
								WorkflowID:  workflowID,
								StepID:      "final_execution",
							})
						}
					}
				} else if strings.HasSuffix(low, ".pdf") {
					// Create a richer PDF: prefer last validation output, include artifact list
					var payload interface{}
					if len(result.ValidationSteps) > 0 {
						payload = result.ValidationSteps[len(result.ValidationSteps)-1].Output
					}
					if payload == nil {
						payload = result.Result
					}

					summary := ""
					if payloadStr, ok := payload.(string); ok && payloadStr != "" {
						summary = payloadStr
					} else if payload != nil {
						if b, err := json.Marshal(payload); err == nil {
							summary = string(b)
						}
					}
					if hasList && list != "" {
						summary = summary + "\nFiles: " + list
					}
					pdf := s.createSimplePDF("Artifacts Report", "Generated by intelligent executor", summary)
					_ = s.fileStorage.StoreFile(&StoredFile{
						Filename:    fname,
						Content:     pdf,
						ContentType: "application/pdf",
						Size:        int64(len(pdf)),
						WorkflowID:  workflowID,
						StepID:      "final_execution",
					})
				}
			}
		}
	}

	response := IntelligentExecutionResponse{
		Success: result.Success,
		Result: func() interface{} {
			if s, ok := result.Result.(string); ok {
				return sanitizeConsoleOutput(s)
			}
			return result.Result
		}(),
		Error:           result.Error,
		GeneratedCode:   result.GeneratedCode,
		ExecutionTime:   result.ExecutionTime.Milliseconds(),
		RetryCount:      result.RetryCount,
		UsedCachedCode:  result.UsedCachedCode,
		ValidationSteps: result.ValidationSteps,
		NewAction:       result.NewAction,
		WorkflowID:      workflowID,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)

	if result.GeneratedCode != nil {
		filename := "output.txt"
		if result.GeneratedCode.Language == "python" {
			filename = req.TaskName + ".py"
		}
		response.Preview = map[string]interface{}{
			"files": []map[string]interface{}{
				{
					"filename": filename,
					"language": result.GeneratedCode.Language,
					"code":     result.GeneratedCode.Code,
				},
			},
		}
	} else if result.Result != nil {
		if s, ok := result.Result.(string); ok {
			response.Preview = map[string]interface{}{
				"files": []map[string]interface{}{
					{
						"filename": "output.txt",
						"language": "text",
						"code":     s,
					},
				},
			}
		}
	} else if result.NewAction != nil && result.NewAction.Code != "" {
		lang := result.NewAction.Language
		if lang == "" {
			lang = "text"
		}
		response.Preview = map[string]interface{}{
			"files": []map[string]interface{}{
				{
					"filename": req.TaskName + "." + lang,
					"language": lang,
					"code":     result.NewAction.Code,
				},
			},
		}
	}
}

// createIntelligentWorkflowRecord creates a workflow record for the Monitor UI to display
func (s *APIServer) createIntelligentWorkflowRecord(req IntelligentExecutionRequest, result *IntelligentExecutionResult, workflowID string) string {

	storeID := workflowID
	if !strings.HasPrefix(storeID, "intelligent_") {
		storeID = "intelligent_" + storeID
	}

	projectID := req.ProjectID
	if projectID == "" && req.Context != nil {
		if pid, ok := req.Context["project_id"]; ok && strings.TrimSpace(pid) != "" {
			projectID = strings.TrimSpace(pid)
		}
	}

	workflowRecord := map[string]interface{}{
		"id":               storeID,
		"name":             req.TaskName,
		"task_name":        req.TaskName,
		"description":      req.Description,
		"status":           "completed",
		"progress":         100.0,
		"total_steps":      1,
		"completed_steps":  1,
		"failed_steps":     0,
		"current_step":     "intelligent_execution",
		"started_at":       time.Now().Add(-result.ExecutionTime).Format(time.RFC3339),
		"updated_at":       time.Now().Format(time.RFC3339),
		"error":            result.Error,
		"generated_code":   result.GeneratedCode,
		"execution_time":   result.ExecutionTime.Milliseconds(),
		"retry_count":      result.RetryCount,
		"used_cached_code": result.UsedCachedCode,
		"validation_steps": result.ValidationSteps,
		"files":            []interface{}{},
		"steps":            []interface{}{},
		"project_id":       projectID,
	}

	log.Printf("🔧 [HDN] Creating workflow record with project_id: %s for workflow: %s", projectID, storeID)

	// Extract files from file storage only (skip validation outputs to avoid duplicates in UI)
	var files []interface{}

	redisAddrRaw := getenvTrim("REDIS_URL")
	redisAddr := normalizeRedisAddr(redisAddrRaw)
	fileStorage := NewFileStorage(redisAddr, 24)

	storedFiles, err := fileStorage.GetFilesByWorkflow(storeID)
	if err == nil {
		for _, file := range storedFiles {

			storedFile, err := fileStorage.GetFile(file.ID)
			if err == nil {
				files = append(files, map[string]interface{}{
					"filename":     storedFile.Filename,
					"content_type": storedFile.ContentType,
					"size":         storedFile.Size,
					"content":      string(storedFile.Content),
				})
			}
		}
	}

	if files == nil {
		files = []interface{}{}
	}
	workflowRecord["files"] = files

	workflowKey := fmt.Sprintf("workflow:%s", storeID)
	existing, _ := s.redis.Get(context.Background(), workflowKey).Result()
	if existing != "" {
		// Merge minimal updates
		var old map[string]interface{}
		_ = json.Unmarshal([]byte(existing), &old)
		for k, v := range workflowRecord {
			old[k] = v
		}

		if old["files"] == nil {
			old["files"] = []interface{}{}
		}
		workflowJSON, _ := json.Marshal(old)
		s.redis.Set(context.Background(), workflowKey, workflowJSON, 24*time.Hour)
	} else {
		workflowJSON, _ := json.Marshal(workflowRecord)
		s.redis.Set(context.Background(), workflowKey, workflowJSON, 24*time.Hour)
	}

	updateWorkflowFiles := func() {

		storedFiles, err := fileStorage.GetFilesByWorkflow(storeID)
		if err != nil || len(storedFiles) == 0 {

			if workflowID != storeID {
				storedFiles, err = fileStorage.GetFilesByWorkflow(workflowID)
			}
		}

		if err == nil && len(storedFiles) > 0 {
			var fileList []interface{}
			for _, file := range storedFiles {
				storedFile, err := fileStorage.GetFile(file.ID)
				if err == nil {
					fileList = append(fileList, map[string]interface{}{
						"filename":     storedFile.Filename,
						"content_type": storedFile.ContentType,
						"size":         storedFile.Size,
						"content":      string(storedFile.Content),
					})
				}
			}
			if len(fileList) > 0 {

				existing, _ := s.redis.Get(context.Background(), workflowKey).Result()
				if existing != "" {
					var old map[string]interface{}
					if err := json.Unmarshal([]byte(existing), &old); err == nil {

						if old["files"] == nil {
							old["files"] = []interface{}{}
						}
						old["files"] = fileList
						workflowJSON, _ := json.Marshal(old)
						s.redis.Set(context.Background(), workflowKey, workflowJSON, 24*time.Hour)
						log.Printf("📁 [API] Updated workflow record %s with %d files", storeID, len(fileList))
					}
				} else {

					workflowRecord["files"] = fileList
					workflowJSON, _ := json.Marshal(workflowRecord)
					s.redis.Set(context.Background(), workflowKey, workflowJSON, 24*time.Hour)
					log.Printf("📁 [API] Updated new workflow record %s with %d files", storeID, len(fileList))
				}
			}
		}
	}

	updateWorkflowFiles()

	go func() {
		time.Sleep(2 * time.Second)
		updateWorkflowFiles()

		time.Sleep(3 * time.Second)
		updateWorkflowFiles()
	}()

	activeWorkflowsKey := "active_workflows"
	s.redis.SRem(context.Background(), activeWorkflowsKey, storeID)
	s.redis.Expire(context.Background(), activeWorkflowsKey, 24*time.Hour)

	if strings.HasPrefix(storeID, "intelligent_") {
		s.storeWorkflowMapping(workflowID, storeID)
	}

	log.Printf("📊 [API] Created intelligent workflow record: %s", storeID)
	return storeID
}

// Send HTTP response for intelligent execution
func (s *APIServer) sendIntelligentExecuteResponse(w http.ResponseWriter, result *IntelligentExecutionResult, workflowID string) {
	response := map[string]interface{}{
		"success":        result.Success,
		"workflow_id":    workflowID,
		"execution_time": result.ExecutionTime.Milliseconds(),
		"error":          result.Error,
		"generated_code": result.GeneratedCode,
		"result":         result.Result,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleIntelligentExecuteOptions handles CORS preflight requests
func (s *APIServer) handleIntelligentExecuteOptions(w http.ResponseWriter, r *http.Request) {

	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
	w.WriteHeader(http.StatusOK)
}

// Add missing HTTP response to handleIntelligentExecute
func (s *APIServer) handleIntelligentExecuteResponse(w http.ResponseWriter, result *IntelligentExecutionResult, workflowID string) {
	response := map[string]interface{}{
		"success":        result.Success,
		"workflow_id":    workflowID,
		"execution_time": result.ExecutionTime.Milliseconds(),
		"error":          result.Error,
		"generated_code": result.GeneratedCode,
		"result":         result.Result,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (s *APIServer) handlePrimeNumbers(w http.ResponseWriter, r *http.Request) {
	var req PrimeNumbersRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if req.Count <= 0 {
		req.Count = 10
	}

	executor := NewIntelligentExecutor(
		s.domainManager,
		s.codeStorage,
		s.codeGenerator,
		s.dockerExecutor,
		s.llmClient,
		s.actionManager,
		s.plannerIntegration,
		s.selfModelManager,
		s.toolMetrics,
		s.fileStorage,
		s.hdnBaseURL,
		s.redisAddr,
	)

	ctx := r.Context()
	result, err := executor.ExecutePrimeNumbersExample(ctx, req.Count)

	if err != nil {
		http.Error(w, fmt.Sprintf("Prime numbers execution failed: %v", err), http.StatusInternalServerError)
		return
	}

	response := IntelligentExecutionResponse{
		Success:         result.Success,
		Result:          result.Result,
		Error:           result.Error,
		GeneratedCode:   result.GeneratedCode,
		ExecutionTime:   result.ExecutionTime.Milliseconds(),
		RetryCount:      result.RetryCount,
		UsedCachedCode:  result.UsedCachedCode,
		ValidationSteps: result.ValidationSteps,
		NewAction:       result.NewAction,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (s *APIServer) handleListCapabilities(w http.ResponseWriter, r *http.Request) {

	executor := NewIntelligentExecutor(
		s.domainManager,
		s.codeStorage,
		s.codeGenerator,
		s.dockerExecutor,
		s.llmClient,
		s.actionManager,
		s.plannerIntegration,
		s.selfModelManager,
		s.toolMetrics,
		s.fileStorage,
		s.hdnBaseURL,
		s.redisAddr,
	)

	capabilities, err := executor.ListCachedCapabilities()
	if err != nil {
		log.Printf("⚠️ [API] Failed to list cached capabilities: %v", err)
		capabilities = []*GeneratedCode{}
	}

	if s.plannerIntegration != nil {
		coreCaps, err := s.plannerIntegration.ListCapabilities()
		if err == nil {
			for _, cap := range coreCaps {

				capabilities = append([]*GeneratedCode{{
					ID:          cap.ID,
					TaskName:    cap.TaskName,
					Description: cap.Description,
					Language:    cap.Language,
					Code:        cap.Code,
					Tags:        append(cap.Tags, "core"),
					Executable:  true,
					CreatedAt:   time.Now(),
				}}, capabilities...)
			}
		}
	}

	stats := executor.GetExecutionStats()

	response := CapabilitiesResponse{
		Capabilities: capabilities,
		Stats:        stats,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (s *APIServer) handleHierarchicalExecute(w http.ResponseWriter, r *http.Request) {
	var req HierarchicalTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if strings.TrimSpace(req.ProjectID) == "" && req.Context != nil {
		if pid, ok := req.Context["project_id"]; ok && strings.TrimSpace(pid) != "" {
			req.ProjectID = pid
		}
	}

	ctx := context.Background()
	s.cleanupStaleActiveWorkflows(ctx)

	if s.redis != nil {
		duplicateKey := fmt.Sprintf("workflow:duplicate:%s:%s", req.TaskName, req.Description)

		hash := sha256.Sum256([]byte(strings.TrimSpace(req.TaskName) + "|" + strings.TrimSpace(req.Description)))
		taskHash := hex.EncodeToString(hash[:])
		duplicateKey = fmt.Sprintf("workflow:duplicate:%s", taskHash)

		existingWorkflowID, err := s.redis.Get(ctx, duplicateKey).Result()
		if err == nil && existingWorkflowID != "" {

			workflowKey := fmt.Sprintf("workflow:%s", existingWorkflowID)
			workflowJSON, err := s.redis.Get(ctx, workflowKey).Result()
			if err == nil && workflowJSON != "" {
				var workflow map[string]interface{}
				if err := json.Unmarshal([]byte(workflowJSON), &workflow); err == nil {
					status, _ := workflow["status"].(string)
					startedAtStr, _ := workflow["started_at"].(string)

					if status == "running" || status == "pending" {
						response := HierarchicalTaskResponse{
							Success:    false,
							Error:      fmt.Sprintf("A workflow for '%s' is already running. Please wait for it to complete.", req.TaskName),
							Message:    "Duplicate workflow",
							WorkflowID: existingWorkflowID,
						}
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(http.StatusConflict)
						json.NewEncoder(w).Encode(response)
						return
					}

					if startedAtStr != "" {
						if startedAt, err := time.Parse(time.RFC3339, startedAtStr); err == nil {
							if time.Since(startedAt) < 1*time.Hour {
								response := HierarchicalTaskResponse{
									Success:    false,
									Error:      "This task was processed recently. Please wait before requesting again.",
									Message:    "Duplicate workflow",
									WorkflowID: existingWorkflowID,
								}
								w.Header().Set("Content-Type", "application/json")
								w.WriteHeader(http.StatusConflict)
								json.NewEncoder(w).Encode(response)
								return
							}
						}
					}
				}
			}
		}
	}

	isUI := isUIRequest(r)
	activeWorkflowCount, err := s.redis.SCard(ctx, "active_workflows").Result()
	if err == nil {
		// UI requests: allow up to 15 active workflows
		// Non-UI requests: allow up to 15 active workflows
		var maxActiveWorkflows int64
		if isUI {
			maxActiveWorkflows = 15
		} else {
			maxActiveWorkflows = 15
		}

		if activeWorkflowCount >= maxActiveWorkflows {
			log.Printf("⚠️ [API] Rejecting hierarchical execute - %d active workflows (max: %d, UI: %v)", activeWorkflowCount, maxActiveWorkflows, isUI)
			response := HierarchicalTaskResponse{
				Success: false,
				Error:   fmt.Sprintf("Server busy - %d workflows already running (max: %d). Please try again later.", activeWorkflowCount, maxActiveWorkflows),
				Message: "Too many concurrent workflows",
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			json.NewEncoder(w).Encode(response)
			return
		}
	}

	wfID := fmt.Sprintf("intelligent_%d", time.Now().UnixNano())
	initial := map[string]any{
		"id":          wfID,
		"name":        req.TaskName,
		"task_name":   req.TaskName,
		"description": req.Description,
		"status":      "running",
		"progress":    0.0,
		"started_at":  time.Now().Format(time.RFC3339),
		"updated_at":  time.Now().Format(time.RFC3339),
		"steps":       []any{},
		"files":       []any{},
	}
	if b, err := json.Marshal(initial); err == nil {
		key := fmt.Sprintf("workflow:%s", wfID)
		_ = s.redis.Set(ctx, key, string(b), 24*time.Hour).Err()

		_ = s.redis.SAdd(ctx, "active_workflows", wfID).Err()

		if s.redis != nil {
			hash := sha256.Sum256([]byte(strings.TrimSpace(req.TaskName) + "|" + strings.TrimSpace(req.Description)))
			taskHash := hex.EncodeToString(hash[:])
			duplicateKey := fmt.Sprintf("workflow:duplicate:%s", taskHash)
			_ = s.redis.Set(ctx, duplicateKey, wfID, 24*time.Hour).Err()
		}

		log.Printf("📊 [API] Created initial workflow record (running): %s", wfID)
	}

	go func(req HierarchicalTaskRequest, wfID string, isUI bool) {

		slotTimeout := 10 * time.Second
		if isUI {
			slotTimeout = 60 * time.Second
		}

		ctx, cancel := context.WithTimeout(context.Background(), slotTimeout)
		defer cancel()

		// Try to acquire slot - UI requests try UI slot first, then general
		var acquired bool
		var release func()

		if isUI {

			select {
			case s.uiExecutionSemaphore <- struct{}{}:
				acquired = true
				release = func() { <-s.uiExecutionSemaphore }
			default:

				select {
				case s.executionSemaphore <- struct{}{}:
					acquired = true
					release = func() { <-s.executionSemaphore }
				case <-ctx.Done():
					acquired = false
				}
			}
		} else {

			select {
			case s.executionSemaphore <- struct{}{}:
				acquired = true
				release = func() { <-s.executionSemaphore }
			case <-ctx.Done():
				acquired = false
			}
		}

		if !acquired {
			log.Printf("❌ [API] Async execution rejected - timeout waiting for execution slot after %v (UI: %v)", slotTimeout, isUI)

			key := fmt.Sprintf("workflow:%s", wfID)
			if val, err := s.redis.Get(context.Background(), key).Result(); err == nil {
				var rec map[string]any
				if json.Unmarshal([]byte(val), &rec) == nil {
					rec["status"] = "failed"
					rec["error"] = fmt.Sprintf("Timeout waiting for execution slot after %v", slotTimeout)
					rec["updated_at"] = time.Now().Format(time.RFC3339)
					if b, err := json.Marshal(rec); err == nil {
						_ = s.redis.Set(context.Background(), key, string(b), 24*time.Hour).Err()
					}
				}
			}
			return
		}
		defer release()

		defer func() {

			key := fmt.Sprintf("workflow:%s", wfID)
			if val, err := s.redis.Get(context.Background(), key).Result(); err == nil {
				var rec map[string]any
				if json.Unmarshal([]byte(val), &rec) == nil {
					rec["updated_at"] = time.Now().Format(time.RFC3339)
					if b, err := json.Marshal(rec); err == nil {
						_ = s.redis.Set(context.Background(), key, string(b), 24*time.Hour).Err()
					}
				}
			}
		}()

		if s.isSimplePrompt(req) {
			executor := NewIntelligentExecutor(
				s.domainManager,
				s.codeStorage,
				s.codeGenerator,
				s.dockerExecutor,
				s.llmClient,
				s.actionManager,
				s.plannerIntegration,
				s.selfModelManager,
				s.toolMetrics,
				s.fileStorage,
				s.hdnBaseURL,
				s.redisAddr,
			)

			execCtx, execCancel := context.WithTimeout(context.Background(), 10*time.Minute)
			defer execCancel()

			highPriority := isUI
			result, err := executor.ExecuteTaskIntelligently(execCtx, &ExecutionRequest{
				TaskName:        req.TaskName,
				Description:     req.Description,
				Context:         req.Context,
				Language:        "python",
				ForceRegenerate: false,
				MaxRetries:      2,
				Timeout:         600,
				HighPriority:    highPriority,
			})
			if err != nil {
				log.Printf("❌ [API] Async direct execution failed: %v", err)
			}

			workflowIDForRecord := result.WorkflowID
			if workflowIDForRecord == "" {

				workflowIDForRecord = wfID
				log.Printf("⚠️ [API] result.WorkflowID was empty, using initial wfID: %s", wfID)
			} else if workflowIDForRecord != wfID {
				log.Printf("🔧 [API] Updating workflow ID from initial %s to executor's %s (for file consistency)", wfID, workflowIDForRecord)
			}
			storeID := s.createIntelligentWorkflowRecord(IntelligentExecutionRequest{
				TaskName:    req.TaskName,
				Description: req.Description,
				Context:     req.Context,
				Language:    "python",
			}, result, workflowIDForRecord)

			if req.ProjectID != "" {
				pid := s.resolveProjectID(req.ProjectID)
				if linkErr := s.projectManager.LinkWorkflow(pid, storeID); linkErr != nil {
					log.Printf("❌ [API] Failed to link workflow %s to project %s: %v", storeID, pid, linkErr)
				} else {
					log.Printf("✅ [API] Linked workflow %s to project %s", storeID, pid)
				}
			}

			sessionID := ""
			goalID := ""
			if req.Context != nil {
				sessionID = req.Context["session_id"]
				goalID = req.Context["goal_id"]
			}

			hasArtifacts := false
			{
				redisAddr := getenvTrim("REDIS_URL")
				if redisAddr == "" {
					redisAddr = "localhost:6379"
				} else {

					redisAddr = strings.TrimPrefix(redisAddr, "redis://")

					redisAddr = strings.TrimSuffix(redisAddr, "/")
				}
				fs := NewFileStorage(redisAddr, 24)
				if files, err := fs.GetFilesByWorkflow(wfID); err == nil && len(files) > 0 {
					hasArtifacts = true
				}
			}

			if sessionID != "" && s.vectorDB != nil {
				text := fmt.Sprintf("Workflow %s finished: success=%v, artifacts=%v", wfID, result != nil && result.Success, hasArtifacts)
				ep := &mempkg.EpisodicRecord{
					SessionID: sessionID,
					Timestamp: time.Now().UTC(),
					Outcome: func() string {
						if result != nil && result.Success {
							return "success"
						}
						return "failure"
					}(),
					Tags: []string{"workflow", "completion"},
					Text: text,
					Metadata: map[string]any{
						"workflow_id": wfID,
						"goal_id":     goalID,
						"artifacts":   hasArtifacts,
					},
				}
				vec := toyEmbed(ep.Text, 768)
				_ = s.vectorDB.IndexEpisode(ep, vec)
			}

			if sessionID != "" && s.workingMemory != nil {
				summary := map[string]any{
					"type":        "workflow_summary",
					"workflow_id": wfID,
					"task_name":   req.TaskName,
					"description": req.Description,
					"success":     result != nil && result.Success,
					"artifacts":   hasArtifacts,
					"timestamp":   time.Now().UTC(),
				}
				_ = s.workingMemory.AddEvent(sessionID, summary, 50)
			}

			if goalID != "" && result != nil && result.Success && hasArtifacts {
				base := strings.TrimSpace(os.Getenv("GOAL_MANAGER_URL"))
				if base == "" {
					base = "http://localhost:8090"
				}
				achURL := fmt.Sprintf("%s/goal/%s/achieve", strings.TrimRight(base, "/"), goalID)
				reqAch, _ := http.NewRequest("POST", achURL, nil)
				client := &http.Client{Timeout: 5 * time.Second}
				if resp, err := client.Do(reqAch); err != nil {
					log.Printf("⚠️ [API] Auto-achieve goal %s failed: %v", goalID, err)
				} else {
					if resp.Body != nil {
						resp.Body.Close()
					}
					if resp.StatusCode >= 200 && resp.StatusCode < 300 {
						log.Printf("🎯 [API] Goal %s marked achieved (workflow %s)", goalID, wfID)
					} else {
						log.Printf("⚠️ [API] Auto-achieve goal %s returned status %d", goalID, resp.StatusCode)
					}
				}
			}
			return
		}

		execution, err := s.plannerIntegration.StartHierarchicalWorkflow(
			req.UserRequest,
			req.TaskName,
			req.Description,
			req.Context,
		)
		if err != nil {
			log.Printf("❌ [API] Failed to start hierarchical workflow: %v", err)
			return
		}

		if req.ProjectID != "" {

			s.ensureProjectByName(req.ProjectID)
			{
				pid := s.resolveProjectID(req.ProjectID)
				if linkErr := s.projectManager.LinkWorkflow(pid, execution.ID); linkErr != nil {
					log.Printf("❌ [API] Failed to link hierarchical workflow %s to project %s: %v", execution.ID, pid, linkErr)
				} else {
					log.Printf("✅ [API] Linked hierarchical workflow %s to project %s", execution.ID, pid)
				}
			}
		}
		if req.Context != nil {
			if sid, ok := req.Context["session_id"]; ok && sid != "" {
				_ = s.workingMemory.AddEvent(sid, map[string]any{
					"type":        "hierarchical_started",
					"task_name":   req.TaskName,
					"workflow_id": execution.ID,
					"timestamp":   time.Now().UTC(),
				}, 100)
			}
		}
		log.Printf("📡 [API] Hierarchical workflow started: %s", execution.ID)
	}(req, wfID, isUI)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(HierarchicalTaskResponse{
		Success:    true,
		WorkflowID: wfID,
		Message:    "Accepted for asynchronous execution",
	})

	execution, err := s.plannerIntegration.StartHierarchicalWorkflow(
		req.UserRequest,
		req.TaskName,
		req.Description,
		req.Context,
	)
	if err != nil {
		response := HierarchicalTaskResponse{
			Success: false,
			Error:   err.Error(),
			Message: "Failed to start hierarchical workflow",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
		return
	}

	if req.ProjectID != "" {
		{
			pid := s.resolveProjectID(req.ProjectID)
			if linkErr := s.projectManager.LinkWorkflow(pid, execution.ID); linkErr != nil {
				log.Printf("❌ [API] Failed to link workflow %s to project %s: %v", execution.ID, pid, linkErr)
			} else {
				log.Printf("✅ [API] Linked workflow %s to project %s", execution.ID, pid)
			}
		}
	}

	response := HierarchicalTaskResponse{
		Success:    true,
		WorkflowID: execution.ID,
		Message:    "Hierarchical workflow started successfully",
	}

	if req.Context != nil {
		if sid, ok := req.Context["session_id"]; ok && sid != "" {
			_ = s.workingMemory.AddEvent(sid, map[string]any{
				"type":        "hierarchical_started",
				"task_name":   req.TaskName,
				"workflow_id": execution.ID,
				"timestamp":   time.Now().UTC(),
			}, 100)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// isSimplePrompt applies a lightweight heuristic to decide if the prompt is single-step
func (s *APIServer) isSimplePrompt(req HierarchicalTaskRequest) bool {

	text := strings.ToLower(strings.TrimSpace(req.UserRequest + " " + req.TaskName + " " + req.Description))
	if text == "" {
		return false
	}

	if strings.Contains(text, "test hypothesis:") {
		return true
	}

	cues := []string{" and then ", " then ", " step ", ";", " -> ", "→"}
	for _, c := range cues {
		if strings.Contains(text, c) {
			return false
		}
	}

	extra := []string{" and ", " produce ", " create ", " generate ", " save ", " pdf", " image", " module", " file"}
	for _, c := range extra {
		if strings.Contains(text, c) {
			return false
		}
	}
	return true
}
