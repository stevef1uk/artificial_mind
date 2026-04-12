package main

import (
	planner "agi/planner_evaluator"
	"context"
	"encoding/json"
	"fmt"
	"hdn/utils"
	"log"
	"os"
	"regexp"
	"runtime"
	"strings"
	"time"
)

// ExecuteTaskIntelligently executes a task using the complete intelligent workflow
// This simplified version maintains all functionality while being easier to understand and maintain
func (ie *IntelligentExecutor) ExecuteTaskIntelligently(ctx context.Context, req *ExecutionRequest) (*IntelligentExecutionResult, error) {
	start := time.Now()

	workflowID := ""

	log.Printf("🧠 [INTELLIGENT] Starting execution for task: %s", req.TaskName)
	log.Printf("🧠 [INTELLIGENT] Description: %s", req.Description)
	log.Printf("🧠 [INTELLIGENT] Context: %s", utils.SafeResultSummary(req.Context, 100))
	log.Printf("🎯 [INTELLIGENT] HighPriority: %v", req.HighPriority)

	if ctx.Err() != nil {
		log.Printf("⏱️ [INTELLIGENT] Context canceled before execution: %v", ctx.Err())
		workflowID = fmt.Sprintf("intelligent_%d", time.Now().UnixNano())
		return &IntelligentExecutionResult{
			Success:         false,
			Error:           fmt.Sprintf("Execution timed out or was canceled: %v", ctx.Err()),
			ExecutionTime:   time.Since(start),
			RetryCount:      0,
			ValidationSteps: []ValidationStep{},
			WorkflowID:      workflowID,
		}, nil
	}

	if ie.shouldUseLLMSummarization(req) {
		return ie.executeLLMSummarization(ctx, req, start, workflowID)
	}

	if ie.shouldUseSimpleInformational(req) {
		return ie.executeSimpleInformational(req, start, workflowID)
	}

	if ie.shouldUseDirectTool(req) {
		return ie.executeDirectTool(req, start, workflowID)
	}

	descLower := strings.ToLower(strings.TrimSpace(req.Description))
	taskLower := strings.ToLower(strings.TrimSpace(req.TaskName))

	// Proactive image generation routing
	isImageGen := (strings.Contains(descLower, "image") || strings.Contains(descLower, "picture") ||
		strings.Contains(descLower, "draw") || strings.Contains(descLower, "artwork") ||
		strings.Contains(descLower, "photo") || strings.Contains(descLower, "illustration")) &&
		(strings.Contains(descLower, "create") || strings.Contains(descLower, "generate") ||
			strings.Contains(descLower, "make") || strings.Contains(descLower, "draw") ||
			strings.Contains(descLower, "show me") || strings.Contains(descLower, "render"))

	if isImageGen && !strings.Contains(descLower, "pico") && !strings.Contains(descLower, "nemo") {
		log.Printf("🖼️ [INTELLIGENT] Proactively routing to local image generation tool")
		
		if req.Context == nil {
			req.Context = make(map[string]string)
		}
		// Set prompt to description
		req.Context["prompt"] = req.Description
		
		return ie.executeExplicitTool(req, "tool_generate_image", start, workflowID)
	}

	if ie.shouldUseHypothesisTesting(req, descLower, taskLower) {
		log.Printf("🧪 [INTELLIGENT] Detected hypothesis testing task - enhancing request")
		req = ie.enhanceHypothesisRequest(req, descLower)

		log.Printf("🧪 [INTELLIGENT] Bypassing planner for hypothesis execution")
		return ie.executeTraditionally(ctx, req, start, workflowID)
	}

	if toolID := ie.extractExplicitToolRequest(req, descLower, taskLower); toolID != "" {
		return ie.executeExplicitTool(req, toolID, start, workflowID)
	}

	if ie.shouldUseWebGathering(req, descLower, taskLower) {

		webResult, err := ie.executeWebGathering(ctx, req, start, workflowID)
		if err == nil && webResult != nil && webResult.Success {
			return webResult, nil
		}

		log.Printf("⚠️ [INTELLIGENT] Web gathering failed or returned no results, falling back")
	}

	if ie.selfModelManager != nil && !ie.isInternalTask(taskLower) {
		goalName := fmt.Sprintf("Execute task: %s", req.TaskName)
		if err := ie.selfModelManager.AddGoal(goalName); err != nil {
			log.Printf("⚠️ [SELF-MODEL] Failed to add goal: %v", err)
		} else {
			log.Printf("🎯 [SELF-MODEL] Added goal: %s", goalName)
		}
	}

	if ctx.Err() != nil {
		log.Printf("⏱️ [INTELLIGENT] Context canceled before execution: %v", ctx.Err())
		return &IntelligentExecutionResult{
			Success:         false,
			Error:           fmt.Sprintf("Execution timed out or was canceled: %v", ctx.Err()),
			ExecutionTime:   time.Since(start),
			RetryCount:      0,
			ValidationSteps: []ValidationStep{},
			WorkflowID:      workflowID,
		}, nil
	}

	if ie.isChainedProgramRequest(req) {
		log.Printf("🔗 [INTELLIGENT] Detected chained program request, using multi-step execution")
		return ie.executeChainedPrograms(ctx, req, start, workflowID)
	}

	if ie.shouldUsePlanner(req) {
		log.Printf("🎯 [INTELLIGENT] Using planner integration for complex task planning")
		return ie.executeWithPlanner(ctx, req, start)
	}

	log.Printf("🤖 [INTELLIGENT] Using traditional intelligent execution (no planner)")
	result, err := ie.executeTraditionally(ctx, req, start, workflowID)

	if ctx.Err() != nil && (result == nil || !result.Success) {
		log.Printf("⏱️ [INTELLIGENT] Context canceled during execution: %v", ctx.Err())
		if result == nil {
			result = &IntelligentExecutionResult{
				Success:         false,
				Error:           fmt.Sprintf("Execution timed out or was canceled: %v", ctx.Err()),
				ExecutionTime:   time.Since(start),
				RetryCount:      0,
				ValidationSteps: []ValidationStep{},
				WorkflowID:      workflowID,
			}
		} else if result.Error == "" {
			result.Error = fmt.Sprintf("Execution timed out or was canceled: %v", ctx.Err())
		}
	}

	return result, err
}

func (ie *IntelligentExecutor) executeLLMSummarization(ctx context.Context, req *ExecutionRequest, start time.Time, workflowID string) (*IntelligentExecutionResult, error) {
	log.Printf("📝 [INTELLIGENT] Using direct LLM summarization path for %s", req.TaskName)

	format := "Paragraph: <text>\nBullets:\n- <b1>\n- <b2>\n- <b3>\nQuestions:\n1) <q1>\n2) <q2>\n3) <q3>\n\n"
	prompt := "You are a concise knowledge summarizer. Analyze the bootstrapped knowledge and provide a factual summary.\n" +
		"Output ONLY the requested sections and nothing else.\n" +
		"Constraints: paragraph <= 80 words; exactly 3 bullets; exactly 3 short follow-up questions.\n" +
		"Focus on the actual concepts and knowledge that were bootstrapped, not educational approaches or project management.\n" +
		"Format:\n" + format

	if desc := strings.TrimSpace(req.Description); desc != "" {
		if strings.Contains(desc, "around") {
			parts := strings.Split(desc, "around")
			if len(parts) > 1 {
				seed := strings.TrimSpace(parts[1])
				prompt += fmt.Sprintf("Task: Summarize the knowledge concepts that were bootstrapped about: %s\n\n", seed)
			}
		} else {
			prompt += "Description:\n" + desc + "\n\n"
		}
	}

	if len(req.Context) > 0 {
		relevantCtx := false
		for k, v := range req.Context {
			if k != "session_id" && k != "project_id" && k != "prefer_traditional" &&
				strings.TrimSpace(k) != "" && strings.TrimSpace(v) != "" {
				if !relevantCtx {
					prompt += "Context:\n"
					relevantCtx = true
				}
				prompt += "- " + k + ": " + v + "\n"
			}
		}
		if relevantCtx {
			prompt += "\n"
		}
	}

	priority := PriorityLow
	if req.HighPriority {
		priority = PriorityHigh
	}

	ctx = WithComponent(ctx, "hdn-intelligent-executor")
	response, err := ie.llmClient.callLLMWithContextAndPriority(ctx, prompt, priority)

	result := &IntelligentExecutionResult{
		Success:         err == nil,
		Result:          response,
		ExecutionTime:   time.Since(start),
		RetryCount:      1,
		ValidationSteps: []ValidationStep{},
		WorkflowID:      workflowID,
	}

	if err != nil {
		result.Error = err.Error()
	}

	ie.recordMonitorMetrics(result.Success, result.ExecutionTime)
	if ie.selfModelManager != nil {
		ie.recordExecutionEpisode(req, result, "llm_summarize")
	}

	log.Printf("✅ [INTELLIGENT] Direct LLM summarization completed (len=%d)", len(response))
	return result, nil
}

func (ie *IntelligentExecutor) executeSimpleInformational(req *ExecutionRequest, start time.Time, workflowID string) (*IntelligentExecutionResult, error) {
	log.Printf("📝 [INTELLIGENT] Simple informational task - returning acknowledgment")

	result := &IntelligentExecutionResult{
		Success:         true,
		Result:          fmt.Sprintf("Informational task acknowledged: %s", req.Description),
		ExecutionTime:   time.Since(start),
		RetryCount:      0,
		ValidationSteps: []ValidationStep{},
		WorkflowID:      workflowID,
	}

	ie.recordMonitorMetrics(result.Success, result.ExecutionTime)
	if ie.selfModelManager != nil {
		ie.recordExecutionEpisode(req, result, "informational")
	}

	return result, nil
}

// executeWebGathering is a dedicated path for scraping tasks that tries to automatically find URLs
func (ie *IntelligentExecutor) executeWebGathering(ctx context.Context, req *ExecutionRequest, start time.Time, workflowID string) (*IntelligentExecutionResult, error) {
	log.Printf("🌐 [INTELLIGENT] Executing web gathering strategy for %s", req.TaskName)

	toolID := "mcp_scrape_url"
	descLower := strings.ToLower(req.Description)
	if strings.Contains(descLower, "smart") ||
		strings.Contains(descLower, "find") ||
		strings.Contains(descLower, "extract") ||
		strings.Contains(descLower, "identify") ||
		strings.Contains(descLower, "scrape") ||
		strings.Contains(descLower, "headline") {
		toolID = "mcp_smart_scrape"
	}

	params := make(map[string]interface{})
	url := ""
	if u, ok := req.Context["url"]; ok && strings.TrimSpace(u) != "" {
		url = u
	} else {

		urlPattern := regexp.MustCompile(`https?://[^\s]+`)
		if matches := urlPattern.FindStringSubmatch(req.Description); len(matches) > 0 {
			url = matches[0]

			url = strings.TrimRight(url, ".,!?;:]})")
		}
	}

	if url == "" {
		if strings.Contains(strings.ToLower(req.Description), "hacker news") || strings.Contains(strings.ToLower(req.Description), "hn") {
			url = "https://news.ycombinator.com"
		} else if strings.Contains(strings.ToLower(req.Description), "wikipedia") {

			url = "https://en.wikipedia.org/wiki/Main_Page"
		} else {

			return nil, fmt.Errorf("no URL provided for web gathering")
		}
	}

	params["url"] = url
	if toolID == "mcp_smart_scrape" {
		params["goal"] = req.Description
	}

	toolResp, err := ie.callTool(toolID, params)
	result := &IntelligentExecutionResult{
		Success:       err == nil,
		ExecutionTime: time.Since(start),
		WorkflowID:    workflowID,
	}

	if err != nil {
		result.Error = err.Error()
	} else {
		b, _ := json.Marshal(toolResp)
		result.Result = string(b)
	}

	return result, nil
}

// executeInfoGatheringWithTools invokes html scraper / http get tools based on context and aggregates results.
func (ie *IntelligentExecutor) executeInfoGatheringWithTools(ctx context.Context, req *ExecutionRequest) (string, error) {

	urls := ie.collectURLsFromContext(req.Context)
	if len(urls) == 0 {
		return "", fmt.Errorf("no urls provided in context for information gathering")
	}

	var summaries []string

	for _, u := range urls {

		scraperResp, err := ie.callTool("mcp_scrape_url", map[string]interface{}{"url": u})
		if err == nil && scraperResp != nil {

			b, _ := json.Marshal(scraperResp)
			summaries = append(summaries, fmt.Sprintf("URL: %s\nDATA: %s", u, string(b)))
			continue
		}

		httpResp, err2 := ie.callTool("tool_http_get", map[string]interface{}{"url": u})
		if err2 != nil || httpResp == nil {

			continue
		}
		status := 0
		if s, ok := httpResp["status"].(float64); ok {
			status = int(s)
		}
		body, _ := httpResp["body"].(string)
		// Truncate body for compactness
		const maxBody = 512
		if len(body) > maxBody {
			body = body[:maxBody] + "..."
		}
		summaries = append(summaries, fmt.Sprintf("URL: %s\nSTATUS: %d\nBODY: %s", u, status, body))
	}

	if len(summaries) == 0 {
		return "", fmt.Errorf("failed to fetch any url with available tools")
	}
	return strings.Join(summaries, "\n\n"), nil
}

// executeWithPlanner executes a task using the planner integration
func (ie *IntelligentExecutor) executeWithPlanner(ctx context.Context, req *ExecutionRequest, start time.Time) (*IntelligentExecutionResult, error) {
	workflowID := fmt.Sprintf("intelligent_%d", time.Now().UnixNano())
	result := &IntelligentExecutionResult{
		Success:         false,
		ValidationSteps: []ValidationStep{},
		WorkflowID:      workflowID,
	}

	ie.registerCapabilitiesWithPlanner()

	episode, err := ie.plannerIntegration.PlanAndExecuteTaskWithWorkflowID(
		fmt.Sprintf("Execute task: %s", req.TaskName),
		req.TaskName,
		req.Description,
		req.Context,
		workflowID,
	)
	if err != nil {
		log.Printf("❌ [PLANNER] Planning/execution failed: %v", err)
		log.Printf("🔄 [PLANNER] Falling back to traditional execution")

		workflowID := fmt.Sprintf("intelligent_%d", time.Now().UnixNano())
		return ie.executeTraditionally(ctx, req, start, workflowID)
	}

	result.Success = true
	result.Result = episode.Result
	result.ExecutionTime = time.Since(start)
	result.RetryCount = 1

	if ie.selfModelManager != nil {
		ie.recordExecutionEpisode(req, result, "planner_execution")
	}

	ie.recordMonitorMetrics(result.Success, result.ExecutionTime)

	log.Printf("🎉 [PLANNER] Task completed successfully via planner")
	return result, nil
}

// executeTraditionally executes a task using the traditional intelligent execution
func (ie *IntelligentExecutor) executeTraditionally(ctx context.Context, req *ExecutionRequest, start time.Time, workflowID string) (*IntelligentExecutionResult, error) {

	taskKey := fmt.Sprintf("%s:%s", req.TaskName, req.Description)
	now := time.Now()

	trivialPatterns := []string{
		"create example.txt",
		"create example",
		"list directory and create",
		"list current directory",
	}
	descLower := strings.ToLower(req.Description)
	for _, pattern := range trivialPatterns {
		if strings.Contains(descLower, pattern) {

			trivialKey := fmt.Sprintf("trivial:%s", pattern)
			if ie.recentTasks == nil {
				ie.recentTasks = make(map[string]time.Time)
			}
			if lastSeen, exists := ie.recentTasks[trivialKey]; exists {
				if now.Sub(lastSeen) < 1*time.Minute {
					log.Printf("⚠️ [INTELLIGENT] Trivial task filter: '%s' executed recently, skipping to prevent repetition", pattern)
					return &IntelligentExecutionResult{
						Success:        false,
						Error:          fmt.Sprintf("Trivial task '%s' executed too recently, skipping to prevent repetition", pattern),
						ExecutionTime:  time.Since(start),
						WorkflowID:     workflowID,
						RetryCount:     0,
						UsedCachedCode: false,
					}, nil
				}
			}
			ie.recentTasks[trivialKey] = now
		}
	}

	if ie.recentTasks == nil {
		ie.recentTasks = make(map[string]time.Time)
	}

	if lastSeen, exists := ie.recentTasks[taskKey]; exists {
		if now.Sub(lastSeen) < 5*time.Second {
			log.Printf("⚠️ [INTELLIGENT] Loop protection: Task '%s' executed recently, skipping to prevent loop", req.TaskName)
			return &IntelligentExecutionResult{
				Success:        false,
				Error:          "Task executed too recently, possible loop detected",
				ExecutionTime:  time.Since(start),
				WorkflowID:     workflowID,
				RetryCount:     0,
				UsedCachedCode: false,
			}, nil
		}
	}

	ie.recentTasks[taskKey] = now

	for key, timestamp := range ie.recentTasks {
		if now.Sub(timestamp) > 5*time.Minute {
			delete(ie.recentTasks, key)
		}
	}

	if req.Language == "" {

		if inferred := ie.inferLanguageFromRequest(req); inferred != "" {
			req.Language = inferred
			log.Printf("🔍 [INTELLIGENT] Language inferred from request: %s", req.Language)
		} else {

			req.Language = "python"
			log.Printf("🔍 [INTELLIGENT] No language detected, defaulting to: %s", req.Language)
		}
	} else {
		log.Printf("🔍 [INTELLIGENT] Language explicitly provided: %s", req.Language)
	}
	if req.MaxRetries == 0 {
		req.MaxRetries = ie.maxRetries
	}
	if req.Timeout == 0 {
		req.Timeout = 300
	}

	fileStorageWorkflowID := workflowID
	if workflowID != "" && !strings.HasPrefix(workflowID, "intelligent_") {

		if ie.learningRedis != nil {
			mappingKey := fmt.Sprintf("workflow_mapping:%s", workflowID)
			if mappedID, err := ie.learningRedis.Get(ctx, mappingKey).Result(); err == nil && mappedID != "" {
				fileStorageWorkflowID = mappedID
				log.Printf("🔗 [INTELLIGENT] Using mapped intelligent workflow ID: %s -> %s (for file storage)", workflowID, fileStorageWorkflowID)
			} else {

				fileStorageWorkflowID = fmt.Sprintf("intelligent_%d", time.Now().UnixNano())
				log.Printf("🔄 [INTELLIGENT] Normalized workflow ID: %s -> %s (for file storage, no mapping found)", workflowID, fileStorageWorkflowID)
			}
		} else {

			fileStorageWorkflowID = fmt.Sprintf("intelligent_%d", time.Now().UnixNano())
			log.Printf("🔄 [INTELLIGENT] Normalized workflow ID: %s -> %s (for file storage, no Redis client)", workflowID, fileStorageWorkflowID)
		}
	} else if workflowID == "" {
		fileStorageWorkflowID = fmt.Sprintf("intelligent_%d", time.Now().UnixNano())
	}

	result := &IntelligentExecutionResult{
		Success:         false,
		ValidationSteps: []ValidationStep{},
		WorkflowID:      fileStorageWorkflowID,
	}

	if ie.isChainedProgramRequest(req) {
		log.Printf("🔗 [INTELLIGENT] Detected chained program request, using multi-step execution")
		return ie.executeChainedPrograms(ctx, req, start, fileStorageWorkflowID)
	}

	descLowerKB := strings.ToLower(req.Description)
	taskLowerKB := strings.ToLower(req.TaskName)
	combinedLower := descLowerKB + " " + taskLowerKB

	needsRequests := strings.Contains(descLowerKB, "query neo4j") || strings.Contains(taskLowerKB, "query neo4j") ||
		strings.Contains(descLowerKB, "query knowledge base") || strings.Contains(taskLowerKB, "query knowledge base") ||
		strings.Contains(descLowerKB, "query knowledge graph") || strings.Contains(taskLowerKB, "query knowledge graph") ||
		strings.Contains(descLowerKB, "knowledge base") || strings.Contains(taskLowerKB, "knowledge base") ||
		strings.Contains(descLowerKB, "knowledge graph") || strings.Contains(taskLowerKB, "knowledge graph") ||
		strings.Contains(descLowerKB, "neo4j") || strings.Contains(taskLowerKB, "neo4j") ||
		strings.Contains(descLowerKB, "mcp") || strings.Contains(taskLowerKB, "mcp") ||
		strings.Contains(descLowerKB, "api") || strings.Contains(taskLowerKB, "api") ||
		strings.Contains(descLowerKB, "http") || strings.Contains(taskLowerKB, "http") ||
		strings.Contains(descLowerKB, "rest") || strings.Contains(taskLowerKB, "rest") ||
		strings.Contains(descLowerKB, "web service") || strings.Contains(taskLowerKB, "web service") ||
		strings.Contains(descLowerKB, "call service") || strings.Contains(taskLowerKB, "call service") ||
		strings.Contains(descLowerKB, "make request") || strings.Contains(taskLowerKB, "make request") ||
		strings.Contains(descLowerKB, "fetch data") || strings.Contains(taskLowerKB, "fetch data") ||
		strings.Contains(descLowerKB, "retrieve data") || strings.Contains(taskLowerKB, "retrieve data")

	if needsRequests {
		if req.Context == nil {
			req.Context = make(map[string]string)
		}
		req.Context["allow_requests"] = "true"
		preview := combinedLower
		if len(preview) > 100 {
			preview = preview[:100]
		}
		log.Printf("🔓 [INTELLIGENT] Allowing HTTP requests for API/knowledge base query task (detected: %s)", preview)
	}

	if req.Context != nil {
		if v, ok := req.Context["hypothesis_testing"]; ok && (strings.EqualFold(strings.TrimSpace(v), "true") || strings.TrimSpace(v) == "1") {
			req.Context["allow_requests"] = "true"
			log.Printf("🔓 [INTELLIGENT] Allowing HTTP requests (context flag: hypothesis_testing=true)")
		}
	}

	if unsafeReason := isRequestUnsafeStatic(req); unsafeReason != "" {
		log.Printf("🚫 [INTELLIGENT] Request blocked by static safety pre-check: %s", unsafeReason)
		result.ValidationSteps = append(result.ValidationSteps, ValidationStep{
			Step:     "static_safety_check",
			Success:  false,
			Message:  "Code rejected by safety policy",
			Error:    unsafeReason,
			Duration: 0,
			Output:   "",
		})
		result.Success = false
		result.Error = "Task blocked by safety policy"
		result.ExecutionTime = time.Since(start)
		return result, nil
	}

	log.Printf("🔒 [INTELLIGENT] Checking principles before any processing")

	if ctx.Err() != nil {
		log.Printf("⏱️ [INTELLIGENT] Context canceled before safety check: %v", ctx.Err())
		result.Success = false
		result.Error = fmt.Sprintf("Execution timed out or was canceled: %v", ctx.Err())
		result.ExecutionTime = time.Since(start)
		return result, nil
	}

	catStart := time.Now()
	context, err := ie.categorizeRequestForSafety(req)
	log.Printf("⏱️ [INTELLIGENT] categorizeRequestForSafety took %v", time.Since(catStart))
	if err != nil {

		if ctx.Err() != nil || strings.Contains(err.Error(), "cancelled") || strings.Contains(err.Error(), "timeout") || strings.Contains(err.Error(), "deadline") {
			log.Printf("⏱️ [INTELLIGENT] Safety categorization cancelled/timed out: %v", err)
			result.Success = false
			result.Error = fmt.Sprintf("Execution timed out or was canceled: %v", ctx.Err())
			result.ExecutionTime = time.Since(start)
			return result, nil
		}
		log.Printf("❌ [INTELLIGENT] Safety categorization failed: %v", err)
		result.Success = false
		result.Error = fmt.Sprintf("Cannot verify task safety - LLM categorization failed: %v", err)
		result.ExecutionTime = time.Since(start)
		return result, nil
	}

	princStart := time.Now()
	allowed, reasons, err := CheckActionWithPrinciples(req.TaskName, context)
	log.Printf("⏱️ [INTELLIGENT] CheckActionWithPrinciples took %v", time.Since(princStart))
	if err != nil {
		log.Printf("❌ [INTELLIGENT] Principles check FAILED for %s: %v", req.TaskName, err)
		result.Success = false
		result.Error = fmt.Sprintf("Cannot verify task safety - principles server unavailable: %v", err)
		result.ExecutionTime = time.Since(start)
		return result, nil
	} else if !allowed {
		log.Printf("🚫 [INTELLIGENT] Task BLOCKED by principles: %s. Reasons: %v", req.TaskName, reasons)

		result.ValidationSteps = append(result.ValidationSteps, ValidationStep{
			Step:     "static_safety_check",
			Success:  false,
			Message:  "Code rejected by safety policy",
			Error:    fmt.Sprintf("blocked by safety policy: %v", reasons),
			Duration: 0,
			Output:   "",
		})
		result.Success = false
		result.Error = fmt.Sprintf("Task blocked by principles: %v", reasons)
		result.ExecutionTime = time.Since(start)
		return result, nil
	} else {
		log.Printf("✅ [INTELLIGENT] Principles check passed for %s", req.TaskName)
	}

	if !req.ForceRegenerate || (req.ForceRegenerate && now.Sub(ie.recentTasks[taskKey]) > 10*time.Second) {
		cacheStart := time.Now()
		cachedCode, err := ie.findCompatibleCachedCode(req)
		log.Printf("⏱️ [INTELLIGENT] findCompatibleCachedCode took %v", time.Since(cacheStart))
		if err == nil && cachedCode != nil {
			log.Printf("✅ [INTELLIGENT] Found compatible cached code for task: %s", req.TaskName)
			result.UsedCachedCode = true

			validationResult := ie.validateCode(ctx, cachedCode, req, fileStorageWorkflowID)
			result.ValidationSteps = append(result.ValidationSteps, validationResult)

			if validationResult.Success {
				log.Printf("✅ [INTELLIGENT] Compatible cached code validation successful")

				if unsafe := isCodeUnsafeStatic(cachedCode.Code, req.Language, req.Context); unsafe != "" {
					log.Printf("🚫 [INTELLIGENT] Skipping final tool execution due to safety: %s", unsafe)
				} else {

					log.Printf("🎯 [INTELLIGENT] Final execution using direct Docker executor for file storage (cached code)")
					if finalResult, derr := ie.executeWithSSHTool(ctx, cachedCode.Code, req.Language, req.Context, false, fileStorageWorkflowID); derr != nil {
						log.Printf("⚠️ [INTELLIGENT] Final execution failed: %v", derr)
					} else if finalResult.Success {
						log.Printf("✅ [INTELLIGENT] Final execution successful, files stored")

						if names, ok := req.Context["artifact_names"]; ok && names != "" && ie.fileStorage != nil {
							parts := strings.Split(names, ",")
							for _, fname := range parts {
								fname = strings.TrimSpace(fname)
								if fname != "" {
									log.Printf("📁 [INTELLIGENT] Attempting to extract artifact: %s", fname)

									if fileContent, err := ie.extractFileFromSSH(ctx, fname, req.Language); err == nil && len(fileContent) > 0 {

										artifactWorkflowID := fileStorageWorkflowID
										if artifactWorkflowID == "" {
											artifactWorkflowID = fmt.Sprintf("intelligent_%d", time.Now().UnixNano())
										} else if !strings.HasPrefix(artifactWorkflowID, "intelligent_") {
											artifactWorkflowID = fmt.Sprintf("intelligent_%s", artifactWorkflowID)
										}

										contentType := "application/octet-stream"
										if strings.HasSuffix(fname, ".md") {
											contentType = "text/markdown"
										} else if strings.HasSuffix(fname, ".pdf") {
											contentType = "application/pdf"
										} else if strings.HasSuffix(fname, ".txt") {
											contentType = "text/plain"
										} else if strings.HasSuffix(fname, ".json") {
											contentType = "application/json"
										}
										storedFile := &StoredFile{
											Filename:    fname,
											Content:     fileContent,
											ContentType: contentType,
											Size:        int64(len(fileContent)),
											WorkflowID:  artifactWorkflowID,
											StepID:      "",
										}
										if err := ie.fileStorage.StoreFile(storedFile); err != nil {
											log.Printf("⚠️ [INTELLIGENT] Failed to store artifact %s: %v", fname, err)
										} else {
											log.Printf("✅ [INTELLIGENT] Stored artifact: %s (%d bytes)", fname, len(fileContent))
										}
									} else {
										log.Printf("⚠️ [INTELLIGENT] Could not extract artifact %s: %v", fname, err)
									}
								}
							}
						}
					} else {
						log.Printf("⚠️ [INTELLIGENT] Final execution failed: %s", finalResult.Error)
					}
				}

				result.Success = true
				result.GeneratedCode = cachedCode
				result.Result = validationResult.Output
				result.ExecutionTime = time.Since(start)

				result.WorkflowID = fileStorageWorkflowID
				log.Printf("🔍 [INTELLIGENT] Set result.WorkflowID = %s (cached code path, matches fileStorageWorkflowID)", result.WorkflowID)
				return result, nil
			} else {
				log.Printf("⚠️ [INTELLIGENT] Compatible cached code validation failed, will regenerate")
			}
		} else {
			log.Printf("🔍 [INTELLIGENT] No compatible cached code found: %v", err)
		}
	}

	log.Printf("🤖 [INTELLIGENT] Generating new code using LLM")

	tools, err := ie.getAvailableTools(ctx)
	if err != nil {
		log.Printf("⚠️ [INTELLIGENT] Failed to get tools: %v (continuing without tools)", err)
		tools = []Tool{}
	}

	relevantTools := ie.filterRelevantTools(tools, req)
	if len(relevantTools) > 0 {
		toolNames := make([]string, len(relevantTools))
		for i, t := range relevantTools {
			toolNames[i] = t.ID
		}
		log.Printf("🔧 [INTELLIGENT] Found %d relevant tools for task: %v", len(relevantTools), toolNames)
	}

	filteredCtx := filterCodegenContext(req.Context)

	isSimpleTask := false
	descPreviewLen := 100
	if len(req.Description) < descPreviewLen {
		descPreviewLen = len(req.Description)
	}
	log.Printf("📝 [INTELLIGENT] Checking for simple task using LLM - description: %s", req.Description[:descPreviewLen])

	complexity, err := ie.classifyTaskComplexity(req)
	if err != nil {

		log.Printf("⚠️ [INTELLIGENT] LLM classification failed: %v, using string matching fallback", err)
		simpleDescLower := strings.ToLower(req.Description)
		if (strings.Contains(simpleDescLower, "print") || strings.Contains(simpleDescLower, "prints")) &&
			!strings.Contains(simpleDescLower, "matrix") &&
			!strings.Contains(simpleDescLower, "json") &&
			!strings.Contains(simpleDescLower, "read") &&
			!strings.Contains(simpleDescLower, "file") &&
			!strings.Contains(simpleDescLower, "calculate") &&
			!strings.Contains(simpleDescLower, "process") &&
			!strings.Contains(simpleDescLower, "parse") &&
			!strings.Contains(simpleDescLower, "operation") {
			isSimpleTask = true
			log.Printf("📝 [INTELLIGENT] String matching fallback: Detected simple task")
		}
	} else {
		isSimpleTask = (complexity == "simple")
		if isSimpleTask {
			log.Printf("📝 [INTELLIGENT] LLM classified as simple task - skipping description enhancement")
		} else {
			log.Printf("📝 [INTELLIGENT] LLM classified as complex task - will enhance description if needed")
		}
	}

	enhancedDesc := req.Description
	if !isSimpleTask {
		if strings.Contains(strings.ToLower(enhancedDesc), "matrix") ||
			(req.Context != nil && (req.Context["matrix1"] != "" || req.Context["matrix2"] != "")) {
			if req.Language == "go" {
				enhancedDesc += "\n\n🚨 CRITICAL GO MATRIX REQUIREMENTS:\n1. Read from env: matrix1Str := os.Getenv(\"matrix1\"); json.Unmarshal([]byte(matrix1Str), &matrix1) - DO NOT hardcode!\n2. Import: \"os\", \"encoding/json\", \"fmt\"\n3. Output: Print each row separately - for i := 0; i < len(result); i++ { fmt.Println(result[i]) }\n4. WRONG: fmt.Println(result) prints [[6 8] [10 12]] on one line - this FAILS!\n5. CORRECT output format: [6 8] on line 1, [10 12] on line 2"
			} else if req.Language == "python" {
				enhancedDesc += "\n\n🚨 CRITICAL FOR PYTHON MATRIX OPERATIONS:\n- You MUST read matrices from environment variables using os.getenv(\"matrix1\") and os.getenv(\"matrix2\")\n- Parse the JSON string format (e.g., \"[[1,2],[3,4]]\") using json.loads()\n- DO NOT hardcode matrix values - the matrices will be different each time!\n- Example: matrix1 = json.loads(os.getenv(\"matrix1\"))"
			}
		}
	}

	if !isSimpleTask && req.Language == "python" && req.Context != nil && len(req.Context) > 0 {

		hasParams := false
		for k, v := range req.Context {
			if k != "input" && k != "artifact_names" && v != "" {
				hasParams = true
				break
			}
		}
		if hasParams {
			enhancedDesc += "\n\n🚨 CRITICAL FOR PYTHON - READING CONTEXT PARAMETERS:\n- You MUST read ALL context parameters from environment variables using os.getenv()\n- DO NOT hardcode values - the parameters will be different each time!\n- Example: count = int(os.getenv('count', '10'))  # Read 'count' from environment, default to '10' if not set\n- Example: number = int(os.getenv('number', '0'))  # Read 'number' from environment\n- You MUST import 'os' to use os.getenv()\n- Convert string values to appropriate types (int() for numbers, etc.)\n- The context provides these parameters: " + func() string {
				params := []string{}
				for k := range req.Context {
					if k != "input" && k != "artifact_names" {
						params = append(params, k)
					}
				}
				return strings.Join(params, ", ")
			}() + "\n- DO NOT hardcode these values - read them from environment variables!"
		}
	}

	if !isSimpleTask && (req.Language == "javascript" || req.Language == "js") && req.Context != nil && len(req.Context) > 0 {

		hasParams := false
		for k, v := range req.Context {
			if k != "input" && k != "artifact_names" && v != "" {
				hasParams = true
				break
			}
		}
		if hasParams {
			enhancedDesc += "\n\n🚨 CRITICAL FOR JAVASCRIPT - READING CONTEXT PARAMETERS:\n- You MUST read ALL context parameters from environment variables using process.env\n- DO NOT hardcode values - the parameters will be different each time!\n- Example: const count = parseInt(process.env.count || '10', 10);  // Read 'count' from environment, default to '10'\n- Example: const dataStr = process.env.data || process.env.input || ''; const data = dataStr.split(',').map(Number);\n- Convert string values to appropriate types (parseInt() for integers, parseFloat() for floats, split() for arrays)\n- The context provides these parameters: " + func() string {
				params := []string{}
				for k := range req.Context {
					if k != "input" && k != "artifact_names" {
						params = append(params, k)
					}
				}
				return strings.Join(params, ", ")
			}() + "\n- DO NOT hardcode these values - read them from process.env!"
		}
	}

	if ie.learningRedis == nil {
		log.Printf("⚠️  [INTELLIGENCE] learningRedis is nil - cannot retrieve prevention hints")
	}
	preventionHints := ie.getPreventionHintsForTask(req)
	if len(preventionHints) > 0 {
		enhancedDesc += "\n\n🧠 LEARNED FROM EXPERIENCE - Common errors to avoid:\n"
		for _, hint := range preventionHints {
			enhancedDesc += fmt.Sprintf("- %s\n", hint)
		}
		log.Printf("🧠 [INTELLIGENCE] Added %d prevention hints from learned experience", len(preventionHints))
	}

	executionMethod := strings.TrimSpace(os.Getenv("EXECUTION_METHOD"))
	forceDocker := req.Language == "rust" || req.Language == "java"
	useSSH := !forceDocker && (executionMethod == "ssh" || (executionMethod == "" && (runtime.GOARCH == "arm64" || runtime.GOARCH == "aarch64" || os.Getenv("ENABLE_ARM64_TOOLS") == "true")))

	toolAPIURL := ie.hdnBaseURL
	if toolAPIURL == "" {
		if url := os.Getenv("HDN_URL"); url != "" {
			toolAPIURL = url
		} else {
			toolAPIURL = "http://localhost:8080"
		}
	}

	if !useSSH && strings.Contains(toolAPIURL, "localhost") {
		toolAPIURL = strings.Replace(toolAPIURL, "localhost", "host.docker.internal", -1)
		log.Printf("🌐 [INTELLIGENT] Updated ToolAPIURL for Docker: %s", toolAPIURL)
	} else if useSSH {

		nodePort := os.Getenv("HDN_NODEPORT")
		if nodePort != "" {

			if strings.Contains(toolAPIURL, "hdn-server-rpi58.agi.svc.cluster.local") {

				toolAPIURL = fmt.Sprintf("http://hdn-server-rpi58.agi.svc.cluster.local:%s", nodePort)
			} else if strings.Contains(toolAPIURL, "localhost") {

				toolAPIURL = fmt.Sprintf("http://hdn-server-rpi58.agi.svc.cluster.local:%s", nodePort)
			} else {

				toolAPIURL = strings.Replace(toolAPIURL, ":8080", fmt.Sprintf(":%s", nodePort), -1)
			}
			log.Printf("🌐 [INTELLIGENT] Using NodePort %s with service DNS for SSH (resolves to node IP via /etc/hosts): %s", nodePort, toolAPIURL)
		} else if strings.Contains(toolAPIURL, "localhost") {

			if k8sService := os.Getenv("HDN_K8S_SERVICE"); k8sService != "" {
				toolAPIURL = strings.Replace(toolAPIURL, "localhost:8080", k8sService, -1)
				log.Printf("🌐 [INTELLIGENT] Using Kubernetes service DNS for SSH: %s", toolAPIURL)
			} else {

				toolAPIURL = strings.Replace(toolAPIURL, "localhost:8080", "hdn-server-rpi58.agi.svc.cluster.local:8080", -1)
				log.Printf("🌐 [INTELLIGENT] Using Kubernetes service DNS (ClusterIP) for SSH: %s", toolAPIURL)
			}
		} else {
			log.Printf("🌐 [INTELLIGENT] Using ToolAPIURL for SSH execution: %s", toolAPIURL)
		}
	}

	codeGenReq := &CodeGenerationRequest{
		TaskName:     req.TaskName,
		Description:  enhancedDesc,
		Language:     req.Language,
		Context:      filteredCtx,
		Tags:         []string{"intelligent_execution", "auto_generated"},
		Executable:   true,
		Tools:        relevantTools,
		ToolAPIURL:   toolAPIURL,
		HighPriority: req.HighPriority,
	}

	genStart := time.Now()
	codeGenResult, err := ie.codeGenerator.GenerateCode(codeGenReq)
	log.Printf("⏱️ [INTELLIGENT] GenerateCode took %v", time.Since(genStart))
	if err != nil {
		result.Error = fmt.Sprintf("Code generation failed: %v", err)
		result.ExecutionTime = time.Since(start)
		return result, nil
	}

	if !codeGenResult.Success {
		result.Error = fmt.Sprintf("Code generation failed: %s", codeGenResult.Error)
		result.ExecutionTime = time.Since(start)
		return result, nil
	}

	generatedCode := codeGenResult.Code
	log.Printf("✅ [INTELLIGENT] Generated code successfully")

	success := false
	for attempt := 0; attempt < req.MaxRetries; attempt++ {
		log.Printf("🔄 [INTELLIGENT] Validation attempt %d/%d", attempt+1, req.MaxRetries)

		valStart := time.Now()
		validationResult := ie.validateCode(ctx, generatedCode, req, fileStorageWorkflowID)
		log.Printf("⏱️ [INTELLIGENT] validateCode (attempt %d) took %v", attempt+1, time.Since(valStart))
		result.ValidationSteps = append(result.ValidationSteps, validationResult)
		result.RetryCount = attempt + 1

		if validationResult.Success {
			log.Printf("✅ [INTELLIGENT] Code validation successful on attempt %d", attempt+1)
			success = true
			break
		} else {
			log.Printf("❌ [INTELLIGENT] Code validation failed on attempt %d: %s", attempt+1, validationResult.Error)

			ie.learnFromValidationFailure(validationResult, req)

			if attempt < req.MaxRetries-1 {
				log.Printf("🔧 [INTELLIGENT] Attempting to fix code using LLM feedback")
				fixedCode, fixErr := ie.fixCodeWithLLM(generatedCode, validationResult, req)
				if fixErr != nil {
					log.Printf("❌ [INTELLIGENT] Code fixing failed: %v", fixErr)
					continue
				}
				generatedCode = fixedCode
				log.Printf("✅ [INTELLIGENT] Code fixed, retrying validation")
			}
		}
	}

	if !success {

		errorMsg := ""
		if len(result.ValidationSteps) > 0 {
			lastStep := result.ValidationSteps[len(result.ValidationSteps)-1]
			if lastStep.Error != "" {
				errorMsg = lastStep.Error
			} else if lastStep.Output != "" {

				if strings.Contains(strings.ToLower(lastStep.Output), "error") ||
					strings.Contains(strings.ToLower(lastStep.Output), "failed") ||
					strings.Contains(strings.ToLower(lastStep.Output), "traceback") ||
					strings.Contains(strings.ToLower(lastStep.Output), "connection refused") ||
					strings.Contains(strings.ToLower(lastStep.Output), "compilation") {
					errorMsg = fmt.Sprintf("Execution failed: %s", lastStep.Output)
				} else {

					outputPreview := lastStep.Output
					if len(outputPreview) > 500 {
						outputPreview = outputPreview[:500] + "..."
					}
					errorMsg = fmt.Sprintf("Code validation failed after all retry attempts. Last output: %s", outputPreview)
				}
			} else {
				errorMsg = "Code validation failed after all retry attempts (no error details available)"
			}
			result.Result = lastStep.Output
		} else {
			errorMsg = "Code validation failed after all retry attempts (no validation steps recorded)"
		}

		if errorMsg == "" {
			errorMsg = "Code validation failed after all retry attempts"
		}

		result.Error = errorMsg
		result.ExecutionTime = time.Since(start)
		log.Printf("❌ [INTELLIGENT] Execution failed: %s", errorMsg)
		return result, nil
	}

	log.Printf("🎯 [INTELLIGENT] Final execution to store generated files via tool")

	if names, ok := req.Context["artifact_names"]; ok && names != "" && generatedCode != nil {
		log.Printf("📋 [INTELLIGENT] Artifact names requested: %s", names)
		parts := strings.Split(names, ",")
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}

		codeExts := []string{".py", ".go", ".js", ".java"}
		for _, fname := range parts {
			low := strings.ToLower(fname)
			for _, ext := range codeExts {
				if strings.HasSuffix(low, ext) {

					log.Printf("📄 [INTELLIGENT] Will save code as artifact: %s", fname)

					req.Context["save_code_filename"] = fname
					break
				}
			}

		}
	} else {
		log.Printf("📋 [INTELLIGENT] No artifact_names in context or no generated code")
	}

	log.Printf("🔍 [EXEC2] Generated code:")
	log.Printf("--- START CODE ---")
	log.Printf("%s", generatedCode.Code)
	log.Printf("--- END CODE ---")

	log.Printf("🎯 [INTELLIGENT] Final execution using SSH executor (workflow: %s)", fileStorageWorkflowID)

	var finalResult *DockerExecutionResponse

	if success && len(result.ValidationSteps) > 0 {
		lastStep := result.ValidationSteps[len(result.ValidationSteps)-1]
		if lastStep.Success {
			log.Printf("⏩ [INTELLIGENT] Skipping redundant final execution; reusing successful validation result")
			finalResult = &DockerExecutionResponse{
				Success: true,
				Output:  lastStep.Output,
				Files:   lastStep.Files,
			}
		}
	}

	if finalResult == nil {
		log.Printf("🔍 [INTELLIGENT] Performing final code execution")
		fr, derr := ie.executeWithSSHTool(ctx, generatedCode.Code, req.Language, req.Context, false, fileStorageWorkflowID)
		if derr != nil {
			log.Printf("⚠️ [INTELLIGENT] Final execution failed: %v", derr)
			finalResult = &DockerExecutionResponse{Success: false, Error: derr.Error()}
		} else {
			finalResult = fr
		}
	}

	if finalResult.Success {
		log.Printf("✅ [INTELLIGENT] Final execution result processed")
		log.Printf("✅ [INTELLIGENT] Final execution successful")
		log.Printf("📊 [INTELLIGENT] Execution output length: %d bytes", len(finalResult.Output))

		if names, ok := req.Context["artifact_names"]; ok && names != "" && ie.fileStorage != nil {
			log.Printf("📁 [INTELLIGENT] artifact_names context flag set: %s", names)
			log.Printf("📁 [INTELLIGENT] Extracting artifacts for workflow: %s", fileStorageWorkflowID)
			parts := strings.Split(names, ",")
			var successCount int
			for _, fname := range parts {
				fname = strings.TrimSpace(fname)
				if fname != "" {
					log.Printf("📁 [INTELLIGENT] Attempting to extract artifact: %s", fname)

					if fileContent, err := ie.extractFileFromSSH(ctx, fname, req.Language); err == nil && len(fileContent) > 0 {

						artifactWorkflowID := fileStorageWorkflowID
						if artifactWorkflowID == "" {
							artifactWorkflowID = fmt.Sprintf("intelligent_%d", time.Now().UnixNano())
						} else if !strings.HasPrefix(artifactWorkflowID, "intelligent_") {
							artifactWorkflowID = fmt.Sprintf("intelligent_%s", artifactWorkflowID)
						}

						contentType := "application/octet-stream"
						if strings.HasSuffix(fname, ".md") {
							contentType = "text/markdown"
						} else if strings.HasSuffix(fname, ".pdf") {
							contentType = "application/pdf"
						} else if strings.HasSuffix(fname, ".txt") {
							contentType = "text/plain"
						} else if strings.HasSuffix(fname, ".json") {
							contentType = "application/json"
						}
						storedFile := &StoredFile{
							Filename:    fname,
							Content:     fileContent,
							ContentType: contentType,
							Size:        int64(len(fileContent)),
							WorkflowID:  artifactWorkflowID,
							StepID:      "",
						}
						if err := ie.fileStorage.StoreFile(storedFile); err != nil {
							log.Printf("⚠️ [INTELLIGENT] Failed to store artifact %s: %v", fname, err)
						} else {
							log.Printf("✅ [INTELLIGENT] Stored artifact: %s (%d bytes, workflow: %s)", fname, len(fileContent), artifactWorkflowID)
							successCount++
						}
					} else {
						log.Printf("⚠️ [INTELLIGENT] Could not extract artifact %s: %v", fname, err)
					}
				}
			}
			log.Printf("📁 [INTELLIGENT] Artifact extraction complete: %d/%d files stored", successCount, len(parts))
		} else {
			if names, ok := req.Context["artifact_names"]; ok && names != "" {
				log.Printf("⚠️ [INTELLIGENT] artifact_names set but fileStorage is nil - artifacts will not be saved")
			} else {
				log.Printf("📁 [INTELLIGENT] No artifact_names context flag - skipping artifact extraction")
			}
		}
	} else {
		log.Printf("⚠️ [INTELLIGENT] Final execution failed: %s", finalResult.Error)
	}

	log.Printf("💾 [INTELLIGENT] Caching successful code")
	err = ie.codeStorage.StoreCode(generatedCode)
	if err != nil {
		log.Printf("⚠️ [INTELLIGENT] Failed to cache code: %v", err)
	}

	log.Printf("🎯 [INTELLIGENT] Creating dynamic action for future reuse")
	dynamicAction := &DynamicAction{
		Task:          req.TaskName,
		Preconditions: []string{},
		Effects:       []string{req.TaskName + "_completed"},
		TaskType:      "intelligent_execution",
		Description:   req.Description,
		Code:          generatedCode.Code,
		Language:      generatedCode.Language,
		Context:       req.Context,
		CreatedAt:     time.Now(),
		Domain:        "intelligent",
		Tags:          []string{"intelligent_execution", "auto_generated", "validated"},
	}

	err = ie.actionManager.CreateAction(dynamicAction)
	if err != nil {
		log.Printf("⚠️ [INTELLIGENT] Failed to create dynamic action: %v", err)
	} else {
		result.NewAction = dynamicAction
		log.Printf("✅ [INTELLIGENT] Created dynamic action: %s", dynamicAction.Task)
	}

	result.WorkflowID = fileStorageWorkflowID
	log.Printf("🔍 [INTELLIGENT] Set result.WorkflowID = %s (matches fileStorageWorkflowID for file retrieval)", result.WorkflowID)

	result.Success = true
	result.GeneratedCode = generatedCode
	if len(result.ValidationSteps) > 0 {
		lastStep := result.ValidationSteps[len(result.ValidationSteps)-1]
		result.Result = lastStep.Output
		log.Printf("📊 [INTELLIGENT] Setting result.Result from validation step output: %q (length: %d)", lastStep.Output, len(lastStep.Output))
	} else {
		log.Printf("⚠️ [INTELLIGENT] No validation steps to extract output from")
	}
	result.ExecutionTime = time.Since(start)

	if ie.selfModelManager != nil {
		ie.recordExecutionEpisode(req, result, "traditional_execution")
	}

	if result.Success && generatedCode != nil {
		ie.recordSuccessfulExecution(req, result, generatedCode)
	} else if !result.Success {
		ie.recordFailedExecution(req, result)
	}

	ie.recordMonitorMetrics(result.Success, result.ExecutionTime)

	log.Printf("🎉 [INTELLIGENT] Intelligent execution completed successfully in %v", result.ExecutionTime)
	return result, nil
}

// registerCapabilitiesWithPlanner registers existing capabilities with the planner
func (ie *IntelligentExecutor) registerCapabilitiesWithPlanner() {
	if ie.plannerIntegration == nil {
		return
	}

	actions, err := ie.actionManager.GetActionsByDomain("default")
	if err != nil {
		log.Printf("⚠️ [PLANNER] Failed to list actions: %v", err)
		return
	}

	for _, action := range actions {
		capability := ConvertDynamicActionToCapability(action)
		if err := ie.plannerIntegration.RegisterCapability(capability); err != nil {
			log.Printf("⚠️ [PLANNER] Failed to register capability %s: %v", action.Task, err)
		} else {
			log.Printf("✅ [PLANNER] Registered capability: %s", action.Task)
		}
	}

	cachedCode, err := ie.codeStorage.ListAllCode()
	if err != nil {
		log.Printf("⚠️ [PLANNER] Failed to list cached code: %v", err)
		return
	}

	for _, code := range cachedCode {
		capability := &planner.Capability{
			ID:         code.ID,
			TaskName:   code.TaskName,
			Entrypoint: fmt.Sprintf("%s.%s", code.Language, code.TaskName),
			Language:   code.Language,
			InputSig:   make(map[string]string),
			Outputs:    []string{code.TaskName + "_completed"},
			Preconds:   []string{},
			Effects:    map[string]interface{}{code.TaskName + "_completed": true},
			Score:      0.9,
			CreatedAt:  code.CreatedAt,
			LastUsed:   time.Now(),
			Validation: map[string]interface{}{
				"executable": code.Executable,
				"tags":       code.Tags,
			},
			Permissions: code.Tags,
		}

		if err := ie.plannerIntegration.RegisterCapability(capability); err != nil {
			log.Printf("⚠️ [PLANNER] Failed to register cached code capability %s: %v", code.TaskName, err)
		} else {
			log.Printf("✅ [PLANNER] Registered cached code capability: %s", code.TaskName)
		}
	}
}
