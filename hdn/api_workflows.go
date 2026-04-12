package main

import (
	planner "agi/planner_evaluator"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
)

// handleResolveWorkflowID maps between intelligent_ and hierarchical workflow IDs.
// - If given an intelligent_ id, returns the hierarchical UUID it maps to (if any)
// - If given a hierarchical id, returns the corresponding intelligent_ id (if any)
// Falls back to the provided id when no mapping exists.
func (s *APIServer) handleResolveWorkflowID(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	resolved := map[string]string{
		"input":     id,
		"type":      "unknown",
		"mapped_id": id,
	}

	if strings.HasPrefix(id, "intelligent_") {

		if hid, err := s.getReverseWorkflowMapping(id); err == nil && hid != "" {
			resolved["type"] = "intelligent"
			resolved["mapped_id"] = hid
		} else {
			resolved["type"] = "intelligent"
		}
	} else {

		if iid, err := s.getWorkflowMapping(id); err == nil && iid != "" {
			resolved["type"] = "hierarchical"
			resolved["mapped_id"] = iid
		} else {
			resolved["type"] = "hierarchical"
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resolved)
}

// storeWorkflowMapping stores a mapping between hierarchical and intelligent workflow IDs
func (s *APIServer) storeWorkflowMapping(hierarchicalID, intelligentID string) {
	ctx := context.Background()

	mappingKey := fmt.Sprintf("workflow_mapping:%s", hierarchicalID)
	s.redis.Set(ctx, mappingKey, intelligentID, 24*time.Hour)

	reverseMappingKey := fmt.Sprintf("workflow_mapping_reverse:%s", intelligentID)
	s.redis.Set(ctx, reverseMappingKey, hierarchicalID, 24*time.Hour)

	log.Printf("🔗 [API] Stored workflow mapping: %s -> %s", hierarchicalID, intelligentID)
}

// getWorkflowMapping retrieves the intelligent workflow ID for a hierarchical workflow ID
func (s *APIServer) getWorkflowMapping(hierarchicalID string) (string, error) {
	ctx := context.Background()
	mappingKey := fmt.Sprintf("workflow_mapping:%s", hierarchicalID)

	intelligentID, err := s.redis.Get(ctx, mappingKey).Result()
	if err != nil {
		return "", fmt.Errorf("no mapping found for hierarchical workflow %s", hierarchicalID)
	}

	return intelligentID, nil
}

// getReverseWorkflowMapping retrieves the hierarchical workflow ID for an intelligent workflow ID
func (s *APIServer) getReverseWorkflowMapping(intelligentID string) (string, error) {
	ctx := context.Background()
	reverseMappingKey := fmt.Sprintf("workflow_mapping_reverse:%s", intelligentID)

	hierarchicalID, err := s.redis.Get(ctx, reverseMappingKey).Result()
	if err != nil {
		return "", fmt.Errorf("no reverse mapping found for intelligent workflow %s", intelligentID)
	}

	return hierarchicalID, nil
}

func (s *APIServer) handleGetWorkflowStatus(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	workflowID := vars["id"]

	status, err := s.plannerIntegration.GetWorkflowStatus(workflowID)
	if err != nil {
		response := WorkflowStatusResponse{
			Success: false,
			Error:   err.Error(),
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
		return
	}

	response := WorkflowStatusResponse{
		Success: true,
		Status:  status,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleGetWorkflowDetails returns full workflow detail including steps and dependencies
func (s *APIServer) handleGetWorkflowDetails(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	workflowID := vars["id"]

	details, err := s.plannerIntegration.GetWorkflowDetails(workflowID)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"details": details,
	})
}

func (s *APIServer) handlePauseWorkflow(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	workflowID := vars["id"]

	var req WorkflowControlRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	err := s.plannerIntegration.PauseWorkflow(workflowID, req.Reason)
	if err != nil {
		response := WorkflowControlResponse{
			Success: false,
			Error:   err.Error(),
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
		return
	}

	response := WorkflowControlResponse{
		Success: true,
		Message: "Workflow paused successfully",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (s *APIServer) handleResumeWorkflow(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	workflowID := vars["id"]

	var req WorkflowControlRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	err := s.plannerIntegration.ResumeWorkflow(workflowID, req.ResumeToken)
	if err != nil {
		response := WorkflowControlResponse{
			Success: false,
			Error:   err.Error(),
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
		return
	}

	response := WorkflowControlResponse{
		Success: true,
		Message: "Workflow resumed successfully",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (s *APIServer) handleCancelWorkflow(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	workflowID := vars["id"]

	err := s.plannerIntegration.CancelWorkflow(workflowID)
	if err != nil {
		response := WorkflowControlResponse{
			Success: false,
			Error:   err.Error(),
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
		return
	}

	response := WorkflowControlResponse{
		Success: true,
		Message: "Workflow cancelled successfully",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// cleanupStaleActiveWorkflows removes workflows from active_workflows that are actually completed
// This prevents the set from growing indefinitely with stale entries
func (s *APIServer) cleanupStaleActiveWorkflows(ctx context.Context) {
	if s.redis == nil {
		return
	}

	activeWorkflowsKey := "active_workflows"
	workflowIDs, err := s.redis.SMembers(ctx, activeWorkflowsKey).Result()
	if err != nil {
		return
	}

	if len(workflowIDs) == 0 {
		return
	}

	maxCheck := 100
	if len(workflowIDs) > maxCheck {
		log.Printf("🧹 [API] Cleaning up stale workflows (checking %d of %d)", maxCheck, len(workflowIDs))
		workflowIDs = workflowIDs[:maxCheck]
	} else {
		log.Printf("🧹 [API] Cleaning up stale workflows (checking %d)", len(workflowIDs))
	}

	removedCount := 0
	for _, workflowID := range workflowIDs {

		workflowKey := fmt.Sprintf("workflow:%s", workflowID)
		workflowJSON, err := s.redis.Get(ctx, workflowKey).Result()
		if err == nil && workflowJSON != "" {
			var workflow map[string]interface{}
			if err := json.Unmarshal([]byte(workflowJSON), &workflow); err == nil {
				status, ok := workflow["status"].(string)
				if ok && (status == "completed" || status == "failed" || status == "cancelled") {

					s.redis.SRem(ctx, activeWorkflowsKey, workflowID)
					removedCount++
				} else if status == "running" {

					if startedAtStr, ok := workflow["started_at"].(string); ok {
						if startedAt, err := time.Parse(time.RFC3339, startedAtStr); err == nil {
							if time.Since(startedAt) > 7*time.Minute {

								workflow["status"] = "failed"
								workflow["error"] = "Workflow timeout: exceeded 7 minute execution limit"
								if updatedJSON, err := json.Marshal(workflow); err == nil {
									s.redis.Set(ctx, workflowKey, string(updatedJSON), 0)
									s.redis.SRem(ctx, activeWorkflowsKey, workflowID)
									removedCount++
									log.Printf("⏱️ [API] Marked workflow %s as failed (timeout after 7 minutes)", workflowID)
								}
							}
						}
					}
				}
			}
		} else if err != nil {

			if strings.HasPrefix(workflowID, "intelligent_") {

				parts := strings.Split(workflowID, "_")
				if len(parts) >= 2 {
					if timestamp, err := strconv.ParseInt(parts[1], 10, 64); err == nil {

						workflowTime := time.Unix(0, timestamp)
						if time.Since(workflowTime) > 1*time.Hour {
							s.redis.SRem(ctx, activeWorkflowsKey, workflowID)
							removedCount++
						}
					}
				}
			}
		}
	}

	if removedCount > 0 {
		log.Printf("✅ [API] Cleaned up %d stale workflows from active_workflows", removedCount)
	}
}

func (s *APIServer) handleListActiveWorkflows(w http.ResponseWriter, r *http.Request) {

	ctx := context.Background()
	s.cleanupStaleActiveWorkflows(ctx)

	workflows := s.plannerIntegration.ListActiveWorkflows()

	if workflows == nil {
		workflows = []*planner.WorkflowStatus{}
	}

	fileWorkflowKeys, err := s.redis.Keys(ctx, "file:by_workflow:*").Result()
	if err == nil {
		log.Printf("📁 [API] Found %d workflow file index keys", len(fileWorkflowKeys))
		workflowIDSet := make(map[string]bool)

		for _, wf := range workflows {
			if wf != nil && wf.ID != "" {
				workflowIDSet[wf.ID] = true
			}
		}

		for _, key := range fileWorkflowKeys {
			workflowID := strings.TrimPrefix(key, "file:by_workflow:")
			if workflowID == "" {
				continue
			}

			if workflowIDSet[workflowID] {
				continue
			}

			workflowKey := fmt.Sprintf("workflow:%s", workflowID)
			exists, _ := s.redis.Exists(ctx, workflowKey).Result()
			if exists == 0 {

				files, err := s.fileStorage.GetFilesByWorkflow(workflowID)
				if err == nil && len(files) > 0 {
					log.Printf("📁 [API] Found %d files for workflow %s (no record exists)", len(files), workflowID)
					// Use the most recent file's creation time
					var latestFileTime time.Time
					for _, f := range files {
						if f.CreatedAt.After(latestFileTime) {
							latestFileTime = f.CreatedAt
						}
					}

					taskName := "Intelligent Execution"

					workflowJSON, err := s.redis.Get(ctx, workflowKey).Result()
					if err == nil && workflowJSON != "" {
						var workflowData map[string]interface{}
						if err := json.Unmarshal([]byte(workflowJSON), &workflowData); err == nil {
							if tn, ok := workflowData["task_name"].(string); ok && tn != "" {
								taskName = tn
							}
						}
					}

					currentStep := "intelligent_execution"
					if taskName != "Intelligent Execution" {

						if len(taskName) > 100 {
							taskName = taskName[:100] + "..."
						}
						currentStep = fmt.Sprintf("intelligent_execution:%s", taskName)
					}

					wfStatus := &planner.WorkflowStatus{
						ID:          workflowID,
						Status:      "completed",
						CurrentStep: currentStep,
						Progress: planner.WorkflowProgress{
							Percentage:     100.0,
							TotalSteps:     1,
							CompletedSteps: 1,
							FailedSteps:    0,
							CurrentStep:    currentStep,
						},
						StartedAt:    latestFileTime,
						LastActivity: latestFileTime,
						CanResume:    false,
						CanCancel:    false,
					}
					workflows = append(workflows, wfStatus)
					workflowIDSet[workflowID] = true
					log.Printf("📁 [API] Added workflow %s to list (has files but no record) - Task: %s", workflowID, taskName)
				}
			}
		}
	}

	response := ActiveWorkflowsResponse{
		Success:   true,
		Workflows: workflows,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (s *APIServer) handleListWorkflowTemplates(w http.ResponseWriter, r *http.Request) {
	templates, err := s.plannerIntegration.ListWorkflowTemplates()
	if err != nil {
		response := WorkflowTemplatesResponse{
			Success: false,
			Error:   err.Error(),
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
		return
	}

	response := WorkflowTemplatesResponse{
		Success:   true,
		Templates: templates,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (s *APIServer) handleRegisterWorkflowTemplate(w http.ResponseWriter, r *http.Request) {
	var template planner.WorkflowTemplate
	if err := json.NewDecoder(r.Body).Decode(&template); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	err := s.plannerIntegration.RegisterWorkflowTemplate(&template)
	if err != nil {
		response := WorkflowControlResponse{
			Success: false,
			Error:   err.Error(),
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
		return
	}

	response := WorkflowControlResponse{
		Success: true,
		Message: "Workflow template registered successfully",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleServeFile serves a file by filename
func (s *APIServer) handleServeFile(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	filename := vars["filename"]

	if filename == "" {
		http.Error(w, "No filename provided", http.StatusBadRequest)
		return
	}

	file, err := s.fileStorage.GetFileByFilename(filename)
	if err != nil {
		http.Error(w, fmt.Sprintf("File not found: %s", filename), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", file.ContentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%s", file.Filename))
	w.Header().Set("Content-Length", fmt.Sprintf("%d", file.Size))

	w.Write(file.Content)
}

// handleGetWorkflowFiles returns metadata for all files in a workflow
func (s *APIServer) handleGetWorkflowFiles(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	workflowID := vars["workflow_id"]

	if workflowID == "" {
		http.Error(w, "No workflow ID provided", http.StatusBadRequest)
		return
	}

	// Check if this is a hierarchical workflow that needs mapping
	var targetWorkflowID string
	if strings.HasPrefix(workflowID, "intelligent_") {

		targetWorkflowID = workflowID
	} else {

		intelligentID, err := s.getWorkflowMapping(workflowID)
		if err != nil {
			log.Printf("⚠️ [API] No mapping found for workflow %s, trying direct lookup", workflowID)
			targetWorkflowID = workflowID
		} else {
			log.Printf("🔗 [API] Mapped hierarchical workflow %s to intelligent workflow %s", workflowID, intelligentID)
			targetWorkflowID = intelligentID
		}
	}

	files, err := s.fileStorage.GetFilesByWorkflow(targetWorkflowID)
	if err != nil {
		log.Printf("❌ [API] Failed to get files for workflow %s (target: %s): %v", workflowID, targetWorkflowID, err)
		http.Error(w, fmt.Sprintf("Failed to get files for workflow: %v", err), http.StatusInternalServerError)
		return
	}

	if len(files) > 0 {
		workflowKey := fmt.Sprintf("workflow:%s", targetWorkflowID)
		exists, _ := s.redis.Exists(context.Background(), workflowKey).Result()
		if exists == 0 {
			log.Printf("📝 [API] Creating minimal workflow record for %s (files exist but record missing)", targetWorkflowID)
			minimalRecord := map[string]interface{}{
				"id":          targetWorkflowID,
				"name":        "Intelligent Execution",
				"task_name":   "Intelligent Execution",
				"description": "Workflow with generated artifacts",
				"status":      "completed",
				"progress":    100.0,
				"started_at":  files[0].CreatedAt.Format(time.RFC3339),
				"updated_at":  time.Now().Format(time.RFC3339),
				"files":       []interface{}{},
			}
			// Convert files to the format expected in workflow record
			var fileList []interface{}
			for _, f := range files {
				fileList = append(fileList, map[string]interface{}{
					"filename":     f.Filename,
					"content_type": f.ContentType,
					"size":         f.Size,
				})
			}
			minimalRecord["files"] = fileList
			if recordJSON, err := json.Marshal(minimalRecord); err == nil {
				s.redis.Set(context.Background(), workflowKey, recordJSON, 24*time.Hour)
				log.Printf("✅ [API] Created minimal workflow record for %s", targetWorkflowID)
			}
		}
	}

	workflowFiles := s.getFilesFromWorkflowRecord(workflowID)
	if len(workflowFiles) > len(files) {
		log.Printf("📁 [API] Found %d files in workflow record vs %d in file storage, using workflow record", len(workflowFiles), len(files))
		files = workflowFiles
	}

	files = s.dedupeFilesByFilename(files)

	log.Printf("📁 [API] Retrieved %d files for workflow %s (target: %s)", len(files), workflowID, targetWorkflowID)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(files)
}

// dedupeFilesByFilename keeps the most recent file per filename
func (s *APIServer) dedupeFilesByFilename(files []FileMetadata) []FileMetadata {
	latestByName := make(map[string]FileMetadata)
	for _, f := range files {
		if existing, ok := latestByName[f.Filename]; ok {
			if f.CreatedAt.After(existing.CreatedAt) {
				latestByName[f.Filename] = f
			}
		} else {
			latestByName[f.Filename] = f
		}
	}

	result := make([]FileMetadata, 0, len(latestByName))
	for _, f := range latestByName {
		result = append(result, f)
	}
	return result
}

// getFilesFromWorkflowRecord retrieves files from the workflow record in Redis
func (s *APIServer) getFilesFromWorkflowRecord(workflowID string) []FileMetadata {
	ctx := context.Background()

	workflowKey := fmt.Sprintf("workflow:%s", workflowID)
	workflowData, err := s.redis.Get(ctx, workflowKey).Result()
	if err != nil {
		log.Printf("⚠️ [API] Failed to get workflow record %s: %v", workflowID, err)
		return []FileMetadata{}
	}

	// Parse workflow data
	var workflowRecord map[string]interface{}
	if err := json.Unmarshal([]byte(workflowData), &workflowRecord); err != nil {
		log.Printf("⚠️ [API] Failed to parse workflow record %s: %v", workflowID, err)
		return []FileMetadata{}
	}

	filesInterface, ok := workflowRecord["files"].([]interface{})
	if !ok {
		log.Printf("⚠️ [API] No files found in workflow record %s", workflowID)
		return []FileMetadata{}
	}

	var files []FileMetadata
	for _, fileInterface := range filesInterface {
		file, ok := fileInterface.(map[string]interface{})
		if !ok {
			continue
		}

		fileMetadata := FileMetadata{
			ID:          fmt.Sprintf("workflow_file_%d", len(files)+1),
			Filename:    file["filename"].(string),
			ContentType: file["content_type"].(string),
			Size:        int64(file["size"].(float64)),
			WorkflowID:  workflowID,
			StepID:      "workflow_record",
			CreatedAt:   time.Now(),
			ExpiresAt:   time.Now().Add(24 * time.Hour),
		}

		files = append(files, fileMetadata)
	}

	log.Printf("📁 [API] Retrieved %d files from workflow record %s", len(files), workflowID)
	return files
}

// handleServeWorkflowFile serves a file from a specific workflow
func (s *APIServer) handleServeWorkflowFile(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	workflowID := vars["workflow_id"]
	filename := vars["filename"]

	if workflowID == "" || filename == "" {
		http.Error(w, "Workflow ID and filename are required", http.StatusBadRequest)
		return
	}

	workflowKey := fmt.Sprintf("workflow:%s", workflowID)
	workflowData, err := s.redis.Get(context.Background(), workflowKey).Result()
	if err != nil {
		http.Error(w, fmt.Sprintf("Workflow not found: %s", workflowID), http.StatusNotFound)
		return
	}

	// Parse workflow data
	var workflow map[string]interface{}
	if err := json.Unmarshal([]byte(workflowData), &workflow); err != nil {
		http.Error(w, "Failed to parse workflow data", http.StatusInternalServerError)
		return
	}

	files, ok := workflow["files"].([]interface{})
	if !ok {
		http.Error(w, "No files found in workflow", http.StatusNotFound)
		return
	}

	var targetFile map[string]interface{}
	for _, fileInterface := range files {
		file, ok := fileInterface.(map[string]interface{})
		if !ok {
			continue
		}
		if file["filename"] == filename {
			targetFile = file
			break
		}
	}

	if targetFile == nil {
		http.Error(w, fmt.Sprintf("File not found: %s", filename), http.StatusNotFound)
		return
	}

	content, ok := targetFile["content"].(string)
	if !ok {
		http.Error(w, "File content not available", http.StatusInternalServerError)
		return
	}

	contentType, _ := targetFile["content_type"].(string)
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	size, _ := targetFile["size"].(float64)

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%s", filename))
	w.Header().Set("Content-Length", fmt.Sprintf("%.0f", size))

	w.Write([]byte(content))
}
