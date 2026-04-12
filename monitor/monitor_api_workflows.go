package main

import (
	"context"
	"encoding/base64"
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

// getActiveWorkflows returns currently active workflows
func (m *MonitorService) getActiveWorkflows(c *gin.Context) {
	workflows := []WorkflowStatus{}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(m.hdnURL + "/api/v1/hierarchical/workflows")
	if err != nil {

		if netErr, ok := err.(interface{ Timeout() bool }); ok && netErr.Timeout() {
			log.Printf("⏱️ [MONITOR] Timeout getting workflows from HDN (exceeded 8s)")
			c.JSON(http.StatusGatewayTimeout, gin.H{"error": "HDN server timeout - workflows endpoint took too long to respond"})
		} else {
			log.Printf("❌ [MONITOR] Failed to get workflows from HDN: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get workflows from HDN server"})
		}
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == 200 {
		var response struct {
			Success   bool                      `json:"success"`
			Workflows []*WorkflowStatusResponse `json:"workflows"`
		}

		if err := json.NewDecoder(resp.Body).Decode(&response); err == nil {

			maxWorkflows := 100
			if len(response.Workflows) > maxWorkflows {
				log.Printf("⚠️ [MONITOR] Limiting workflows to %d most recent (found %d total)", maxWorkflows, len(response.Workflows))

				sort.Slice(response.Workflows, func(i, j int) bool {
					ti := getLastActivityTime(response.Workflows[i])
					tj := getLastActivityTime(response.Workflows[j])
					return ti.After(tj)
				})
				response.Workflows = response.Workflows[:maxWorkflows]
			}

			hierByID := make(map[string]*WorkflowStatusResponse)
			for _, hw := range response.Workflows {
				if hw != nil && hw.ID != "" {
					hierByID[hw.ID] = hw
				}
			}

			// First pass: collect workflow data without fetching files/steps
			type workflowData struct {
				wf         *WorkflowStatusResponse
				progress   float64
				totalSteps int
				completed  int
				failed     int
				taskName   string
				desc       string
				resolvedID string
				fileID     string
			}
			workflowDataList := make([]workflowData, 0, len(response.Workflows))

			for _, wf := range response.Workflows {

				progress := 0.0
				totalSteps := 0
				completedSteps := 0
				failedSteps := 0

				if wf.Progress != nil {
					if pct, ok := wf.Progress["percentage"]; ok {
						if pctFloat, ok := pct.(float64); ok {
							progress = pctFloat
						}
					}
					if total, ok := wf.Progress["total_steps"]; ok {
						if totalInt, ok := total.(float64); ok {
							totalSteps = int(totalInt)
						} else if totalInt, ok := total.(int); ok {
							totalSteps = totalInt
						}
					}
					if completed, ok := wf.Progress["completed_steps"]; ok {
						if completedInt, ok := completed.(float64); ok {
							completedSteps = int(completedInt)
						} else if completedInt, ok := completed.(int); ok {
							completedSteps = completedInt
						}
					}
					if failed, ok := wf.Progress["failed_steps"]; ok {
						if failedInt, ok := failed.(float64); ok {
							failedSteps = int(failedInt)
						} else if failedInt, ok := failed.(int); ok {
							failedSteps = failedInt
						}
					}

					if progress == 0.0 && totalSteps > 0 {
						progress = float64(completedSteps) / float64(totalSteps) * 100.0
					}
				}

				taskName := "Hierarchical Workflow"
				description := wf.CurrentStep

				if strings.HasPrefix(wf.ID, "intelligent_") {
					if strings.HasPrefix(wf.CurrentStep, "intelligent_execution:") {

						parts := strings.SplitN(wf.CurrentStep, ":", 2)
						if len(parts) > 1 {
							taskName = parts[1]
							description = "Intelligent execution workflow"
						} else {
							taskName = "Intelligent Execution"
							description = "Workflow with generated artifacts"
						}
					} else if wf.CurrentStep == "intelligent_execution" {

						taskName = "Intelligent Execution"
						description = "Workflow with generated artifacts"
					}
				}

				if wf.CurrentStep != "" && taskName == "Hierarchical Workflow" {

					if strings.Contains(wf.CurrentStep, "data_analysis") || strings.Contains(wf.CurrentStep, "step_goal") {
						taskName = "Data Analysis Pipeline"
						description = "Processing data analysis workflow steps"
					} else if strings.Contains(wf.CurrentStep, "web_scraping") {
						taskName = "Web Scraping Pipeline"
						description = "Processing web scraping workflow steps"
					} else if strings.Contains(wf.CurrentStep, "ml_pipeline") {
						taskName = "ML Pipeline"
						description = "Processing machine learning workflow steps"
					} else if strings.Contains(wf.CurrentStep, "api_integration") {
						taskName = "API Integration"
						description = "Processing API integration workflow steps"
					} else if !strings.HasPrefix(wf.ID, "intelligent_") {

						description = "Executing workflow step"
					}
				}

				resolvedID := wf.ID

				if strings.HasPrefix(wf.ID, "intelligent_") {
					if rid, ok := m.resolveWorkflowID(wf.ID); ok && rid != "" {
						resolvedID = rid

						if hw, exists := hierByID[rid]; exists && hw != nil {
							wf.Status = hw.Status
							wf.CurrentStep = hw.CurrentStep
							wf.LastActivity = hw.LastActivity
							wf.Progress = hw.Progress
							wf.Files = hw.Files
							wf.Steps = hw.Steps
						}
					}
				}

				// For intelligent workflows, use the original ID to fetch files (not resolved ID)
				var fileID string
				if strings.HasPrefix(wf.ID, "intelligent_") {
					fileID = wf.ID
				} else {
					fileID = resolvedID
				}

				workflowDataList = append(workflowDataList, workflowData{
					wf:         wf,
					progress:   progress,
					totalSteps: totalSteps,
					completed:  completedSteps,
					failed:     failedSteps,
					taskName:   taskName,
					desc:       description,
					resolvedID: resolvedID,
					fileID:     fileID,
				})
			}

			maxFileFetch := 50
			shouldFetchFiles := len(workflowDataList) <= maxFileFetch
			if !shouldFetchFiles {
				log.Printf("⚠️ [MONITOR] Skipping file/step fetching for %d workflows (limit: %d)", len(workflowDataList), maxFileFetch)
			}

			type fetchResult struct {
				workflowIdx int
				files       []FileInfo
				steps       []WorkflowStepStatus
				err         error
			}
			resultChan := make(chan fetchResult, len(workflowDataList))

			if shouldFetchFiles {
				for idx, wd := range workflowDataList {
					go func(i int, data workflowData) {
						var files []FileInfo
						var steps []WorkflowStepStatus
						var fetchErr error

						files, fetchErr = m.getWorkflowFiles(data.fileID)
						if fetchErr != nil {
							files = []FileInfo{}
						}

						steps, fetchErr = m.getWorkflowStepDetails(data.resolvedID)
						if fetchErr != nil {
							steps = []WorkflowStepStatus{}
						}

						resultChan <- fetchResult{
							workflowIdx: i,
							files:       files,
							steps:       steps,
							err:         fetchErr,
						}
					}(idx, wd)
				}
			} else {

				for idx := range workflowDataList {
					resultChan <- fetchResult{
						workflowIdx: idx,
						files:       []FileInfo{},
						steps:       []WorkflowStepStatus{},
						err:         nil,
					}
				}
			}

			fileResults := make(map[int][]FileInfo)
			stepResults := make(map[int][]WorkflowStepStatus)
			timeout := time.After(8 * time.Second)
			collected := 0
			timeoutReached := false
			for i := 0; i < len(workflowDataList); i++ {
				if timeoutReached {

					fileResults[i] = []FileInfo{}
					stepResults[i] = []WorkflowStepStatus{}
					continue
				}
				select {
				case result := <-resultChan:
					fileResults[result.workflowIdx] = result.files
					stepResults[result.workflowIdx] = result.steps
					collected++
				case <-timeout:

					log.Printf("⏱️ [MONITOR] Timeout collecting workflow files/steps (collected %d/%d)", collected, len(workflowDataList))
					timeoutReached = true
					fileResults[i] = []FileInfo{}
					stepResults[i] = []WorkflowStepStatus{}
				}
			}

			totalFiles := 0
			for idx, wd := range workflowDataList {
				files := fileResults[idx]
				stepDetails := stepResults[idx]

				if strings.HasPrefix(wd.wf.ID, "intelligent_") {

					if wd.wf.Steps != nil {
						stepDetails = []WorkflowStepStatus{}
						for _, stepInterface := range wd.wf.Steps {
							if stepMap, ok := stepInterface.(map[string]interface{}); ok {
								stepStatus := WorkflowStepStatus{
									ID:     getStringFromMap(stepMap, "id"),
									Name:   getStringFromMap(stepMap, "name"),
									Status: getStringFromMap(stepMap, "status"),
								}
								stepDetails = append(stepDetails, stepStatus)
							}
						}
					}
				}

				if len(files) > 0 {
					totalFiles += len(files)
				}

				workflows = append(workflows, WorkflowStatus{
					ID:              wd.wf.ID,
					Status:          wd.wf.Status,
					TaskName:        wd.taskName,
					Description:     wd.desc,
					Progress:        wd.progress,
					TotalSteps:      wd.totalSteps,
					CompletedSteps:  wd.completed,
					FailedSteps:     wd.failed,
					CurrentStep:     wd.wf.CurrentStep,
					StartedAt:       wd.wf.StartedAt,
					UpdatedAt:       wd.wf.LastActivity,
					CanResume:       wd.wf.CanResume,
					CanCancel:       wd.wf.CanCancel,
					Error:           wd.wf.Error,
					ProgressDetails: wd.wf.Progress,
					Files:           files,
					Steps:           stepDetails,
					GeneratedCode:   wd.wf.GeneratedCode,
				})
			}

			if totalFiles > 0 {
				log.Printf("📁 [MONITOR] Fetched files for %d hierarchical workflows (%d total files)", len(workflowDataList), totalFiles)
			}
		}
	}

	intelligentWorkflows, err := m.getIntelligentWorkflows()
	if err != nil {
		log.Printf("⚠️ [MONITOR] Failed to fetch intelligent workflows: %v", err)
	} else {
		workflows = append(workflows, intelligentWorkflows...)
	}

	unique := make(map[string]WorkflowStatus)
	for _, wf := range workflows {
		if existing, ok := unique[wf.ID]; ok {

			if len(existing.Files) == 0 && len(wf.Files) > 0 {
				existing.Files = wf.Files
			}
			if len(existing.Steps) == 0 && len(wf.Steps) > 0 {
				existing.Steps = wf.Steps
			}

			if existing.Description == "" && wf.Description != "" {
				existing.Description = wf.Description
			}
			if existing.CurrentStep == "" && wf.CurrentStep != "" {
				existing.CurrentStep = wf.CurrentStep
			}
			unique[wf.ID] = existing
		} else {
			unique[wf.ID] = wf
		}
	}

	dedup := make([]WorkflowStatus, 0, len(unique))

	hasIntelligent := false
	for id := range unique {
		if strings.HasPrefix(id, "intelligent_") {
			hasIntelligent = true
			break
		}
	}
	cutoff := time.Now().Add(-2 * time.Minute)
	for id, wf := range unique {
		if hasIntelligent && !strings.HasPrefix(id, "intelligent_") {
			continue
		}

		if strings.HasPrefix(id, "intelligent_") && strings.EqualFold(wf.Status, "running") && wf.TotalSteps == 0 && wf.StartedAt.Before(cutoff) {
			continue
		}
		dedup = append(dedup, wf)
	}

	c.JSON(http.StatusOK, dedup)
}

// getFsmEvaluations returns recent evaluation summaries for a session/goal
// Query params:
// - session_id (optional): filter by session
// - limit (optional): max items
func (m *MonitorService) getFsmEvaluations(c *gin.Context) {
	sessionID := strings.TrimSpace(c.Query("session_id"))

	_ = strings.TrimSpace(c.Query("limit"))

	base := m.hdnURL
	url := base + "/api/v1/episodes/search"

	_ = "workflow completion"
	if sessionID != "" {
		url += fmt.Sprintf("?session_id=%s", urlQueryEscape(sessionID))
	}

	req, _ := http.NewRequest("GET", url, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to fetch evaluations"})
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	c.Data(resp.StatusCode, "application/json", body)
}

// resolveWorkflowID uses HDN's resolver to map intelligent_ IDs to hierarchical UUIDs (and vice versa)
func (m *MonitorService) resolveWorkflowID(id string) (string, bool) {
	client := &http.Client{Timeout: 3 * time.Second}
	url := fmt.Sprintf("%s/api/v1/workflows/resolve/%s", m.hdnURL, urlQueryEscape(id))
	resp, err := client.Get(url)
	if err != nil {
		return "", false
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", false
	}
	var body struct {
		Input    string `json:"input"`
		Type     string `json:"type"`
		MappedID string `json:"mapped_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", false
	}
	if strings.TrimSpace(body.MappedID) == "" {
		return "", false
	}
	return body.MappedID, true
}

// getWorkflowDetails proxies detailed workflow info from HDN
func (m *MonitorService) getWorkflowDetails(c *gin.Context) {
	workflowID := c.Param("workflow_id")
	if workflowID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "workflow_id required"})
		return
	}
	urlStr := fmt.Sprintf("%s/api/v1/hierarchical/workflow/%s/details", m.hdnURL, workflowID)
	resp, err := http.Get(urlStr)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch workflow details"})
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	c.Data(resp.StatusCode, "application/json", body)
}

// listWorkflowFiles returns file metadata for a workflow (proxy to HDN)
func (m *MonitorService) listWorkflowFiles(c *gin.Context) {
	workflowID := c.Param("workflow_id")
	if workflowID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "workflow_id required"})
		return
	}
	resp, err := http.Get(fmt.Sprintf("%s/api/v1/files/workflow/%s", m.hdnURL, workflowID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch files"})
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	c.Data(resp.StatusCode, "application/json", body)
}

// getWorkflowProject returns the project (id and name) linked to a workflow, if any
func (m *MonitorService) getWorkflowProject(c *gin.Context) {
	workflowID := c.Param("workflow_id")
	if workflowID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "workflow_id required"})
		return
	}
	ctx := context.Background()
	projectID, err := m.redisClient.Get(ctx, "workflow_project:"+workflowID).Result()
	if err != nil || projectID == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "no project linked"})
		return
	}

	resp, err := http.Get(fmt.Sprintf("%s/api/v1/projects/%s", m.hdnURL, projectID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch project"})
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		c.JSON(http.StatusOK, gin.H{"id": projectID})
		return
	}
	var project map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&project); err != nil {
		c.JSON(http.StatusOK, gin.H{"id": projectID})
		return
	}
	name, _ := project["name"].(string)
	c.JSON(http.StatusOK, gin.H{"id": projectID, "name": name})
}

// analyzeLastWorkflow finds the most recent workflow for a project and triggers an analysis run
// that writes analysis.md back into the same project. It proxies to HDN intelligent execution.
func (m *MonitorService) analyzeLastWorkflow(c *gin.Context) {
	projectID := c.Param("id")
	if strings.TrimSpace(projectID) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "project id required"})
		return
	}

	resp, err := http.Get(fmt.Sprintf("%s/api/v1/projects/%s/workflows", m.hdnURL, url.PathEscape(projectID)))
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"success": false, "error": "failed to fetch project workflows"})
		return
	}
	defer resp.Body.Close()
	var wfList struct {
		WorkflowIDs []string `json:"workflow_ids"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&wfList); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"success": false, "error": "invalid workflows response"})
		return
	}
	if len(wfList.WorkflowIDs) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "error": "no workflows for project"})
		return
	}

	latestID := wfList.WorkflowIDs[len(wfList.WorkflowIDs)-1]
	latestTime := time.Time{}
	httpClient := &http.Client{Timeout: 30 * time.Second}
	httpExecClient := &http.Client{Timeout: 8 * time.Minute}
	for _, id := range wfList.WorkflowIDs {
		detailsURL := fmt.Sprintf("%s/api/v1/hierarchical/workflow/%s/details", m.hdnURL, url.PathEscape(id))
		if resp2, err2 := httpClient.Get(detailsURL); err2 == nil && resp2 != nil {
			var payload struct {
				Success bool                   `json:"success"`
				Details map[string]interface{} `json:"details"`
			}
			_ = json.NewDecoder(resp2.Body).Decode(&payload)
			resp2.Body.Close()
			if payload.Success {
				if ts, _ := payload.Details["updated_at"].(string); ts != "" {
					if t, errp := time.Parse(time.RFC3339, ts); errp == nil {
						if t.After(latestTime) {
							latestTime = t
							latestID = id
						}
					}
				}
			}
		}
	}

	execPayload := map[string]interface{}{
		"task_name":   "workflow_analysis_summary",
		"description": fmt.Sprintf("Analyze the completed workflow %s in project %s. First, fetch the workflow details from /api/v1/hierarchical/workflow/%s/details to understand what it actually did. Then examine any generated files or outputs. Write a real analysis in analysis.md covering: 1) Actual purpose and goals based on the workflow details, 2) Real steps that were executed, 3) Actual success/failure status from the workflow data, 4) Concrete lessons learned from the execution, 5) Specific recommendations for next steps. Use real data, not placeholders. Keep under 500 words.", latestID, projectID, latestID),
		"context": map[string]string{
			"save_markdown_filename": "analysis.md",
			"artifact_names":         "analysis.md",
			"analysis_mode":          "summary_only",
			"workflow_id_to_analyze": latestID,
		},
		"project_id":       projectID,
		"force_regenerate": true,
		"max_retries":      1,
	}
	bts, _ := json.Marshal(execPayload)

	ctxQuick, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	reqQuick, _ := http.NewRequestWithContext(ctxQuick, http.MethodPost, m.hdnURL+"/api/v1/intelligent/execute", strings.NewReader(string(bts)))
	reqQuick.Header.Set("Content-Type", "application/json")
	execRespQuick, errQuick := httpExecClient.Do(reqQuick)
	if errQuick == nil && execRespQuick != nil {
		var execOut struct {
			Success    bool   `json:"success"`
			WorkflowID string `json:"workflow_id"`
			Error      string `json:"error"`
		}
		_ = json.NewDecoder(execRespQuick.Body).Decode(&execOut)
		execRespQuick.Body.Close()
		if execOut.Success && strings.TrimSpace(execOut.WorkflowID) != "" {

			if m.redisClient != nil && strings.HasPrefix(execOut.WorkflowID, "intelligent_") {
				go func(wfid string) {
					deadline := time.Now().Add(2 * time.Minute)
					for time.Now().Before(deadline) {
						member, err := m.redisClient.SIsMember(context.Background(), "active_workflows", wfid).Result()
						if err == nil && !member {
							return
						}
						time.Sleep(2 * time.Second)
					}
				}(execOut.WorkflowID)
			}
			c.JSON(http.StatusOK, gin.H{
				"success":     true,
				"message":     "analysis started",
				"workflow_id": execOut.WorkflowID,
				"analyzed_id": latestID,
				"project_id":  projectID,
			})
			return
		}
	}

	go func() {

		execResp, err := httpExecClient.Post(m.hdnURL+"/api/v1/intelligent/execute", "application/json", strings.NewReader(string(bts)))
		if err != nil {
			return
		}
		defer execResp.Body.Close()
		io.Copy(io.Discard, execResp.Body)
	}()
	c.JSON(http.StatusAccepted, gin.H{
		"success":     true,
		"message":     "analysis scheduled",
		"analyzed_id": latestID,
		"project_id":  projectID,
	})
	return
}

// serveWorkflowFile serves a file from a specific workflow
func (m *MonitorService) serveWorkflowFile(c *gin.Context) {
	workflowID := c.Param("workflow_id")
	filename := c.Param("filename")

	if workflowID == "" || filename == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Workflow ID and filename are required"})
		return
	}

	escapedWorkflowID := url.PathEscape(workflowID)
	escapedFilename := url.PathEscape(filename)
	proxyURL := fmt.Sprintf("%s/api/v1/workflow/%s/files/%s", m.hdnURL, escapedWorkflowID, escapedFilename)
	resp, err := http.Get(proxyURL)
	if err != nil {

		fileContent, contentType, ferr := m.getFileFromWorkflow(workflowID, filename)
		if ferr != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": "Failed to fetch file from HDN"})
			return
		}
		c.Header("Content-Type", contentType)
		c.Header("Content-Disposition", "inline; filename="+filename)
		c.Data(http.StatusOK, contentType, fileContent)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {

		fileContent, contentType, ferr := m.getFileFromWorkflow(workflowID, filename)
		if ferr != nil {
			c.JSON(resp.StatusCode, gin.H{"error": "File not found in workflow"})
			return
		}
		c.Header("Content-Type", contentType)
		c.Header("Content-Disposition", "inline; filename="+filename)
		c.Data(http.StatusOK, contentType, fileContent)
		return
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	c.Header("Content-Type", contentType)
	c.Header("Content-Disposition", "inline; filename="+filename)

	if _, err := io.Copy(c.Writer, resp.Body); err != nil {

		fileContent, ct, ferr := m.getFileFromWorkflow(workflowID, filename)
		if ferr != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": "Failed to read file from HDN"})
			return
		}
		c.Header("Content-Type", ct)
		c.Header("Content-Disposition", "inline; filename="+filename)
		c.Data(http.StatusOK, ct, fileContent)
		return
	}
}

// getFileFromIntelligentWorkflows retrieves a file from intelligent execution workflows stored in Redis
func (m *MonitorService) getFileFromIntelligentWorkflows(filename string) ([]byte, string, error) {
	ctx := context.Background()

	if m.redisClient == nil {
		return nil, "", fmt.Errorf("redis client not initialized")
	}

	workflowIDs, err := m.redisClient.SMembers(ctx, "active_workflows").Result()
	if err != nil {
		return nil, "", fmt.Errorf("failed to get active workflows: %v", err)
	}

	for _, workflowID := range workflowIDs {
		if !strings.HasPrefix(workflowID, "intelligent_") {
			continue
		}

		workflowKey := fmt.Sprintf("workflow:%s", workflowID)
		workflowJSON, err := m.redisClient.Get(ctx, workflowKey).Result()
		if err != nil {
			continue
		}

		// Parse workflow data
		var workflowData map[string]interface{}
		if err := json.Unmarshal([]byte(workflowJSON), &workflowData); err != nil {
			continue
		}

		if filesInterface, ok := workflowData["files"]; ok {
			if filesArray, ok := filesInterface.([]interface{}); ok {
				for _, fileInterface := range filesArray {
					if fileMap, ok := fileInterface.(map[string]interface{}); ok {
						fileFilename := getStringFromMap(fileMap, "filename")
						if fileFilename == filename {

							content := getStringFromMap(fileMap, "content")
							contentType := getStringFromMap(fileMap, "content_type")
							if contentType == "" {
								contentType = "text/plain"
							}
							return []byte(content), contentType, nil
						}
					}
				}
			}
		}
	}

	return nil, "", fmt.Errorf("file not found in intelligent workflows")
}

// getFileFromWorkflow retrieves a file from a specific workflow
func (m *MonitorService) getFileFromWorkflow(workflowID, filename string) ([]byte, string, error) {
	ctx := context.Background()

	workflowKey := fmt.Sprintf("workflow:%s", workflowID)
	workflowJSON, err := m.redisClient.Get(ctx, workflowKey).Result()
	if err != nil {

		fileContent, contentType, err := m.getFileFromHDN(filename)
		if err == nil {
			return fileContent, contentType, nil
		}
		return nil, "", fmt.Errorf("workflow not found: %s", workflowID)
	}

	// Parse workflow data
	var workflowData map[string]interface{}
	if err := json.Unmarshal([]byte(workflowJSON), &workflowData); err != nil {
		return nil, "", fmt.Errorf("failed to parse workflow data: %v", err)
	}

	if filesInterface, ok := workflowData["files"]; ok {
		if filesArray, ok := filesInterface.([]interface{}); ok {
			for _, fileInterface := range filesArray {
				if fileMap, ok := fileInterface.(map[string]interface{}); ok {
					fileFilename := getStringFromMap(fileMap, "filename")
					if fileFilename == filename {

						contentStr := getStringFromMap(fileMap, "content")
						contentType := getStringFromMap(fileMap, "content_type")

						if contentType == "application/pdf" {

							content := extractPDFFromConsoleOutput(contentStr)
							if len(content) > 0 && string(content[:4]) == "%PDF" {
								return content, contentType, nil
							}

							if decoded, err := base64.StdEncoding.DecodeString(contentStr); err == nil {
								return decoded, contentType, nil
							}

							return []byte(contentStr), contentType, nil
						}

						if decoded, err := base64.StdEncoding.DecodeString(contentStr); err == nil {
							return decoded, contentType, nil
						}

						return []byte(contentStr), contentType, nil
					}
				}
			}
		}
	}

	fileContent, contentType, err := m.getFileFromHDN(filename)
	if err == nil {
		return fileContent, contentType, nil
	}

	return nil, "", fmt.Errorf("file not found in workflow")
}

// getWorkflowFiles retrieves files for a specific workflow
func (m *MonitorService) getWorkflowFiles(workflowID string) ([]FileInfo, error) {
	url := fmt.Sprintf("%s/api/v1/files/workflow/%s", m.hdnURL, workflowID)
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch workflow files from HDN: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return []FileInfo{}, nil
	}

	var files []FileInfo
	if err := json.NewDecoder(resp.Body).Decode(&files); err != nil {
		return nil, fmt.Errorf("failed to decode workflow files: %v", err)
	}

	return files, nil
}

// getIntelligentWorkflows retrieves intelligent execution workflows from Redis
func (m *MonitorService) getIntelligentWorkflows() ([]WorkflowStatus, error) {
	ctx := context.Background()

	if m.redisClient == nil {
		return []WorkflowStatus{}, nil
	}

	workflowIDs, _ := m.redisClient.SMembers(ctx, "active_workflows").Result()

	keys, _ := m.redisClient.Keys(ctx, "workflow:intelligent_*").Result()
	for _, k := range keys {

		if strings.HasPrefix(k, "workflow:") {
			id := strings.TrimPrefix(k, "workflow:")
			workflowIDs = append(workflowIDs, id)
		}
	}

	idSet := make(map[string]struct{})
	var uniqueIDs []string
	for _, id := range workflowIDs {
		if _, ok := idSet[id]; ok {
			continue
		}
		idSet[id] = struct{}{}
		uniqueIDs = append(uniqueIDs, id)
	}

	var workflows []WorkflowStatus
	workflowMap := make(map[string]WorkflowStatus)

	for _, workflowID := range uniqueIDs {

		if !strings.HasPrefix(workflowID, "intelligent_") {
			continue
		}

		workflowKey := fmt.Sprintf("workflow:%s", workflowID)
		workflowJSON, err := m.redisClient.Get(ctx, workflowKey).Result()
		if err != nil {
			log.Printf("⚠️ [MONITOR] Failed to get workflow %s: %v", workflowID, err)
			continue
		}

		// Parse workflow data
		var workflowData map[string]interface{}
		if err := json.Unmarshal([]byte(workflowJSON), &workflowData); err != nil {
			log.Printf("⚠️ [MONITOR] Failed to parse workflow %s: %v", workflowID, err)
			continue
		}

		workflow := WorkflowStatus{
			ID:             getStringFromMap(workflowData, "id"),
			Status:         getStringFromMap(workflowData, "status"),
			TaskName:       getStringFromMap(workflowData, "task_name"),
			Description:    getStringFromMap(workflowData, "description"),
			Progress:       getFloatFromMap(workflowData, "progress"),
			TotalSteps:     getIntFromMap(workflowData, "total_steps"),
			CompletedSteps: getIntFromMap(workflowData, "completed_steps"),
			FailedSteps:    getIntFromMap(workflowData, "failed_steps"),
			CurrentStep:    getStringFromMap(workflowData, "current_step"),
			Error:          getStringFromMap(workflowData, "error"),
			GeneratedCode:  workflowData["generated_code"],
		}

		if startedAtStr := getStringFromMap(workflowData, "started_at"); startedAtStr != "" {
			if startedAt, err := time.Parse(time.RFC3339, startedAtStr); err == nil {
				workflow.StartedAt = startedAt
			}
		}

		if updatedAtStr := getStringFromMap(workflowData, "updated_at"); updatedAtStr != "" {
			if updatedAt, err := time.Parse(time.RFC3339, updatedAtStr); err == nil {
				workflow.UpdatedAt = updatedAt
			}
		}

		if stepsInterface, ok := workflowData["steps"]; ok {
			if stepsArray, ok := stepsInterface.([]interface{}); ok {
				for _, stepInterface := range stepsArray {
					if stepMap, ok := stepInterface.(map[string]interface{}); ok {
						stepStatus := WorkflowStepStatus{
							ID:     getStringFromMap(stepMap, "id"),
							Name:   getStringFromMap(stepMap, "name"),
							Status: getStringFromMap(stepMap, "status"),
						}
						workflow.Steps = append(workflow.Steps, stepStatus)
					}
				}
			}
		}

		workflowMap[workflowID] = workflow
	}

	// Second pass: batch fetch files for all workflows in parallel
	type fileResult struct {
		workflowID string
		files      []FileInfo
		err        error
	}
	fileChan := make(chan fileResult, len(workflowMap))

	for workflowID := range workflowMap {
		go func(id string) {
			files, err := m.getWorkflowFiles(id)
			fileChan <- fileResult{workflowID: id, files: files, err: err}
		}(workflowID)
	}

	fileCount := 0
	totalFiles := 0
	for i := 0; i < len(workflowMap); i++ {
		result := <-fileChan
		if result.err != nil {

			log.Printf("⚠️ [MONITOR] Failed to fetch files for intelligent workflow %s: %v", result.workflowID, result.err)
		} else if len(result.files) > 0 {
			fileCount++
			totalFiles += len(result.files)
		}
		workflow := workflowMap[result.workflowID]
		workflow.Files = result.files
		workflowMap[result.workflowID] = workflow
	}

	if fileCount > 0 {
		log.Printf("📁 [MONITOR] Fetched files for %d intelligent workflows (%d total files)", fileCount, totalFiles)
	}

	for _, workflow := range workflowMap {
		workflows = append(workflows, workflow)
	}

	return workflows, nil
}

// getWorkflowStepDetails retrieves detailed step information for a workflow
func (m *MonitorService) getWorkflowStepDetails(workflowID string) ([]WorkflowStepStatus, error) {
	url := fmt.Sprintf("%s/api/v1/hierarchical/workflow/%s/steps", m.hdnURL, workflowID)
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch workflow step details from HDN: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return []WorkflowStepStatus{}, nil
	}

	var response struct {
		Success bool                 `json:"success"`
		Steps   []WorkflowStepStatus `json:"steps"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode workflow step details: %v", err)
	}

	return response.Steps, nil
}
