package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	planner "agi/planner_evaluator"
	selfmodel "agi/self"
	mempkg "hdn/memory"

	"github.com/redis/go-redis/v9"
)

// PlannerIntegration bridges HDN server with the planner evaluator
type PlannerIntegration struct {
	planner                 *planner.Planner
	feedbackEvaluator       *planner.FeedbackEvaluator
	hierarchicalPlanner     *planner.HierarchicalPlanner
	workflowOrchestrator    *planner.WorkflowOrchestrator
	hierarchicalIntegration *planner.HierarchicalIntegration
	redisClient             *redis.Client
	ctx                     context.Context
	useHierarchical         bool
	apiServer               *APIServer // Reference to API server for workflow mapping
}

// HDNExecutor implements the planner's Executor interface
type HDNExecutor struct {
	intelligentExecutor *IntelligentExecutor
	domainManager       *DomainManager
	actionManager       *ActionManager
	apiServer           *APIServer
}

// ExecutePlan implements the planner's Executor interface
func (h *HDNExecutor) ExecutePlan(ctx context.Context, plan planner.Plan, workflowID string) (interface{}, error) {
	log.Printf("🎯 [PLANNER] Executing plan: %s", plan.ID)
	log.Printf("🎯 [PLANNER] Goal: %s", plan.Goal.Type)
	log.Printf("🎯 [PLANNER] Steps: %d", len(plan.Steps))

	// Convert planner plan to HDN execution
	results := make([]interface{}, 0, len(plan.Steps))

	for i, step := range plan.Steps {
		log.Printf("🎯 [PLANNER] Executing step %d/%d: %s", i+1, len(plan.Steps), step.CapabilityID)

		// Execute using traditional execution to avoid recursion
		result, err := h.executeCapabilityDirectly(ctx, step, workflowID)
		if err != nil {
			log.Printf("❌ [PLANNER] Step %d failed: %v", i+1, err)
			return nil, fmt.Errorf("step %d (%s) failed: %v", i+1, step.CapabilityID, err)
		}

		log.Printf("✅ [PLANNER] Step %d completed successfully", i+1)
		results = append(results, result)
	}

	log.Printf("🎉 [PLANNER] Plan execution completed successfully")
	return results, nil
}

// executeCapabilityDirectly executes a capability without going through the planner
func (h *HDNExecutor) executeCapabilityDirectly(ctx context.Context, step planner.PlanStep, workflowID string) (interface{}, error) {
	// Try to find and execute the capability directly
	// First, check if it's a cached code capability - try multiple languages
	languages := []string{"python", "go", "javascript", "java", "cpp"}
	var cachedCode *GeneratedCode
	var err error

	for _, lang := range languages {
		cachedCode, err = h.intelligentExecutor.findCachedCode(step.CapabilityID, "", lang)
		if err == nil && cachedCode != nil {
			log.Printf("🔍 [PLANNER] Found cached code for capability: %s (language: %s)", step.CapabilityID, lang)
			break
		} else if err != nil {
			log.Printf("🔍 [PLANNER] No cached code found for capability: %s (language: %s) - %v", step.CapabilityID, lang, err)
		}
	}

	if cachedCode != nil {
		// Execute the cached code directly
		// Use the original task name from the cached code, not the capability ID
		execReq := &ExecutionRequest{
			TaskName:    cachedCode.TaskName,
			Description: fmt.Sprintf("Execute capability: %s", step.CapabilityID),
			Context:     convertMapToStringMap(step.Args),
			Language:    cachedCode.Language,
			MaxRetries:  1,
			Timeout:     600,
		}

		// Use traditional execution without planner, ensuring the workflow ID is used
		log.Printf("🔍 [PLANNER] Executing capability %s with workflow ID: %s", step.CapabilityID, workflowID)
		result, err := h.intelligentExecutor.executeTraditionally(ctx, execReq, time.Now(), workflowID)
		if err != nil {
			return nil, err
		}

		// Store workflow mapping if execution was successful
		if result != nil && result.Success && result.WorkflowID != "" {
			h.apiServer.storeWorkflowMapping(workflowID, result.WorkflowID)
		}

		if !result.Success {
			return nil, fmt.Errorf("execution failed: %s", result.Error)
		}

		return result.Result, nil
	}

	// If no cached code, try to generate new code
	log.Printf("🔍 [PLANNER] No cached code found, generating new code for: %s", step.CapabilityID)

	// Extract original task name from context if available, otherwise use capability ID
	taskName := step.CapabilityID
	if originalTaskName, exists := step.Args["original_task_name"]; exists {
		if taskNameStr, ok := originalTaskName.(string); ok {
			taskName = taskNameStr
		}
	}

	execReq := &ExecutionRequest{
		TaskName:    taskName,
		Description: fmt.Sprintf("Execute capability: %s", step.CapabilityID),
		Context:     convertMapToStringMap(step.Args),
		Language:    "python",
		MaxRetries:  1,
		Timeout:     30,
	}

	// Use traditional execution without planner, ensuring the workflow ID is used
	log.Printf("🔍 [PLANNER] Executing new capability %s with workflow ID: %s", step.CapabilityID, workflowID)
	result, err := h.intelligentExecutor.executeTraditionally(ctx, execReq, time.Now(), workflowID)
	if err != nil {
		return nil, err
	}

	// Store workflow mapping if execution was successful
	if result != nil && result.Success && result.WorkflowID != "" {
		h.apiServer.storeWorkflowMapping(workflowID, result.WorkflowID)
	}

	if !result.Success {
		return nil, fmt.Errorf("execution failed: %s", result.Error)
	}

	return result.Result, nil
}

// NewPlannerIntegration creates a new planner integration
func NewPlannerIntegration(
	redisClient *redis.Client,
	intelligentExecutor *IntelligentExecutor,
	domainManager *DomainManager,
	actionManager *ActionManager,
	principlesURL string,
	selfModelManager interface{},
	apiServer *APIServer,
) *PlannerIntegration {
	ctx := context.Background()

	// Create HDN executor
	hdnExecutor := &HDNExecutor{
		intelligentExecutor: intelligentExecutor,
		domainManager:       domainManager,
		actionManager:       actionManager,
		apiServer:           apiServer,
	}

	// Create base planner
	basePlanner := planner.NewPlanner(ctx, redisClient, hdnExecutor, principlesURL)

	// Create base evaluator
	baseEvaluator := planner.NewEvaluator()

	// Create feedback evaluator with principles integration
	feedbackEvaluator := planner.NewFeedbackEvaluator(baseEvaluator, selfModelManager.(*selfmodel.Manager), principlesURL)

	// Create hierarchical integration
	hierarchicalIntegration := planner.NewHierarchicalIntegration(
		ctx,
		redisClient,
		basePlanner,
		principlesURL,
		selfModelManager,
	)

	// Create default templates
	err := hierarchicalIntegration.CreateDefaultTemplates()
	if err != nil {
		log.Printf("⚠️ [PLANNER-INTEGRATION] Failed to create default templates: %v", err)
	}

	integration := &PlannerIntegration{
		planner:                 basePlanner,
		feedbackEvaluator:       feedbackEvaluator,
		hierarchicalPlanner:     hierarchicalIntegration.GetHierarchicalPlanner(),
		workflowOrchestrator:    hierarchicalIntegration.GetWorkflowOrchestrator(),
		hierarchicalIntegration: hierarchicalIntegration,
		redisClient:             redisClient,
		ctx:                     ctx,
		useHierarchical:         true, // Enable hierarchical planning
		apiServer:               apiServer,
	}

	// Pre-load tools from registry as capabilities immediately
	if apiServer != nil {
		go func() {
			// Give the server a moment to start up and register its own tools
			time.Sleep(2 * time.Second)
			if err := integration.LoadMCPToolsAsCapabilities(); err != nil {
				log.Printf("⚠️ [PLANNER-INTEGRATION] Failed to load tools from registry: %v", err)
			}
		}()
	}

	return integration
}

// PlanAndExecuteTask plans and executes a task using the planner
func (pi *PlannerIntegration) PlanAndExecuteTask(
	userRequest string,
	taskName string,
	description string,
	context map[string]string,
) (*planner.Episode, error) {
	// Generate a workflow ID for this execution
	workflowID := fmt.Sprintf("intelligent_%d", time.Now().UnixNano())
	return pi.PlanAndExecuteTaskWithWorkflowID(userRequest, taskName, description, context, workflowID)
}

// PlanAndExecuteTaskWithWorkflowID plans and executes a task with a specific workflow ID
func (pi *PlannerIntegration) PlanAndExecuteTaskWithWorkflowID(
	userRequest string,
	taskName string,
	description string,
	context map[string]string,
	workflowID string,
) (*planner.Episode, error) {
	log.Printf("🧠 [PLANNER-INTEGRATION] Planning task: %s with workflow ID: %s", taskName, workflowID)

	// Use hierarchical planning if enabled
	if pi.useHierarchical && pi.hierarchicalIntegration != nil {
		log.Printf("🧠 [PLANNER-INTEGRATION] Using hierarchical planning")
		return pi.planAndExecuteHierarchicallyWithWorkflowID(userRequest, taskName, description, context, workflowID)
	}

	// Fall back to traditional planning
	log.Printf("🧠 [PLANNER-INTEGRATION] Using traditional planning")
	return pi.planAndExecuteTraditionallyWithWorkflowID(userRequest, taskName, description, context, workflowID)
}

// planAndExecuteHierarchicallyWithWorkflowID plans and executes using hierarchical planning with specific workflow ID
func (pi *PlannerIntegration) planAndExecuteHierarchicallyWithWorkflowID(
	userRequest string,
	taskName string,
	description string,
	context map[string]string,
	workflowID string,
) (*planner.Episode, error) {
	log.Printf("🧠 [PLANNER-INTEGRATION] Using hierarchical planning with workflow ID: %s", workflowID)

	// Start hierarchical workflow
	execution, err := pi.hierarchicalIntegration.PlanAndExecuteHierarchically(
		userRequest,
		taskName,
		description,
		context,
	)
	if err != nil {
		return nil, fmt.Errorf("hierarchical planning failed: %v", err)
	}

	// Wait for completion
	pi.waitForWorkflowCompletion(execution.ID)

	// Get final status
	status, err := pi.hierarchicalIntegration.GetWorkflowStatus(execution.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get workflow status: %v", err)
	}

	// Extract the actual output from the last step result
	var actualOutput interface{}
	if len(execution.Context.StepResults) > 0 {
		// Get the last step result (assuming it's the final output)
		for _, result := range execution.Context.StepResults {
			actualOutput = result
			break // Get the first (and likely only) result
		}
	} else {
		actualOutput = execution.Context.Results
	}

	// Check principles before creating episode
	principlesResult := map[string]interface{}{"status": "passed"}

	// Use LLM to categorize the request for safety (similar to intelligent executor)
	safetyContext, err := pi.categorizeRequestForSafety(userRequest, taskName, description, context)
	if err != nil {
		log.Printf("⚠️ [HIERARCHICAL-INTEGRATION] Safety categorization failed: %v", err)
	} else {
		log.Printf("🔍 [HIERARCHICAL-INTEGRATION] Safety context: %+v", safetyContext)
	}

	// Create episode with the provided workflow ID
	episode := &planner.Episode{
		ID:          workflowID, // Use the provided workflow ID instead of execution.ID
		Timestamp:   time.Now(),
		UserRequest: userRequest,
		StructuredEvent: map[string]interface{}{
			"workflow_id":      workflowID,
			"task_name":        taskName,
			"description":      description,
			"status":           status.Status,
			"success":          status.Status == "completed",
			"error":            status.Error,
			"duration":         time.Since(execution.StartedAt),
			"steps":            len(execution.Context.StepResults),
			"principles_check": principlesResult,
			"safety_context":   safetyContext,
			"started_at":       execution.StartedAt,
			"last_activity":    execution.LastActivity,
		},
		SelectedPlan:    planner.Plan{}, // Empty plan for now
		DecisionTrace:   []string{},     // Empty decision trace for now
		Result:          actualOutput,
		PrinciplesCheck: principlesResult,
	}

	log.Printf("🎉 [HIERARCHICAL-INTEGRATION] Task completed successfully with workflow ID: %s", workflowID)
	return episode, nil
}

// planAndExecuteHierarchically plans and executes using hierarchical planning
func (pi *PlannerIntegration) planAndExecuteHierarchically(
	userRequest string,
	taskName string,
	description string,
	context map[string]string,
) (*planner.Episode, error) {
	// Start hierarchical workflow
	execution, err := pi.hierarchicalIntegration.PlanAndExecuteHierarchically(
		userRequest,
		taskName,
		description,
		context,
	)
	if err != nil {
		return nil, fmt.Errorf("hierarchical planning failed: %v", err)
	}

	// Wait for completion
	pi.waitForWorkflowCompletion(execution.ID)

	// Get final status
	status, err := pi.hierarchicalIntegration.GetWorkflowStatus(execution.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get workflow status: %v", err)
	}

	// Extract the actual output from the last step result
	var actualOutput interface{}
	if len(execution.Context.StepResults) > 0 {
		// Get the last step result (assuming it's the final output)
		for _, result := range execution.Context.StepResults {
			actualOutput = result
			break // Get the first (and likely only) result
		}
	} else {
		actualOutput = execution.Context.Results
	}

	// Check principles before creating episode
	principlesResult := map[string]interface{}{"status": "passed"}

	// Use LLM to categorize the request for safety (similar to intelligent executor)
	safetyContext, err := pi.categorizeRequestForSafety(userRequest, taskName, description, context)
	if err != nil {
		log.Printf("⚠️ [HIERARCHICAL-INTEGRATION] Safety categorization failed: %v", err)
		principlesResult = map[string]interface{}{"status": "failed", "error": "Safety categorization failed"}
	} else {
		// Check with principles server
		allowed, reasons, err := CheckActionWithPrinciples(taskName, safetyContext)
		if err != nil {
			log.Printf("⚠️ [HIERARCHICAL-INTEGRATION] Principles check failed for %s: %v", taskName, err)
			principlesResult = map[string]interface{}{"status": "failed", "error": "Principles check failed"}
		} else if !allowed {
			log.Printf("🚫 [HIERARCHICAL-INTEGRATION] Task BLOCKED by principles: %s. Reasons: %v", taskName, reasons)
			principlesResult = map[string]interface{}{"status": "blocked", "reasons": reasons}
		} else {
			log.Printf("✅ [HIERARCHICAL-INTEGRATION] Principles check passed for %s", taskName)
		}
	}

	// Convert to episode
	episode := &planner.Episode{
		ID:              execution.ID,
		Timestamp:       execution.StartedAt,
		UserRequest:     userRequest,
		StructuredEvent: map[string]interface{}{"workflow_id": execution.ID, "task_name": taskName},
		SelectedPlan:    planner.Plan{ID: execution.Plan.ID, Goal: execution.Plan.Goal},
		DecisionTrace:   []string{"hierarchical_planning", "workflow_execution"},
		Result:          actualOutput,
		PrinciplesCheck: principlesResult,
	}

	if status.Status == "completed" {
		log.Printf("🎉 [PLANNER-INTEGRATION] Hierarchical task completed successfully")
	} else {
		log.Printf("❌ [PLANNER-INTEGRATION] Hierarchical task failed: %s", status.Error)
		episode.Result = map[string]interface{}{"error": status.Error, "status": status.Status}
	}

	return episode, nil
}

// planAndExecuteTraditionallyWithWorkflowID plans and executes using traditional planning with specific workflow ID
func (pi *PlannerIntegration) planAndExecuteTraditionallyWithWorkflowID(
	userRequest string,
	taskName string,
	description string,
	context map[string]string,
	workflowID string,
) (*planner.Episode, error) {
	// Convert context to interface{} map for planner
	goalParams := make(map[string]interface{})
	for k, v := range context {
		goalParams[k] = v
	}

	// Create goal for planner
	goal := planner.Goal{
		ID:     fmt.Sprintf("goal_%d", time.Now().UnixNano()),
		Type:   taskName,
		Params: goalParams,
	}

	// Generate plans using the base planner
	plans, err := pi.planner.GeneratePlans(pi.ctx, goal)
	if err != nil {
		return nil, fmt.Errorf("plan generation failed: %v", err)
	}

	if len(plans) == 0 {
		return nil, fmt.Errorf("no plans generated for goal: %s", goal.Type)
	}

	// Convert plans to options for feedback evaluator
	options := make([]planner.Option, len(plans))
	for i, plan := range plans {
		// Extract task name and language from the first step
		language := "unknown"
		if len(plan.Steps) > 0 {
			// Try to get language from capability
			if cap, err := pi.planner.GetCapability(pi.ctx, plan.Steps[0].CapabilityID); err == nil {
				language = cap.Language
			}
		}

		options[i] = planner.Option{
			TaskName: goal.Type,
			Language: language,
			Score:    plan.Score,
		}
	}

	// Use feedback evaluator to select best option
	selectedOption, err := pi.feedbackEvaluator.SelectBest(options)
	if err != nil {
		return nil, fmt.Errorf("feedback evaluation failed: %v", err)
	}

	// Find the corresponding plan
	var selectedPlan *planner.Plan
	for i, option := range options {
		if option.TaskName == selectedOption.TaskName && option.Language == selectedOption.Language {
			selectedPlan = &plans[i]
			break
		}
	}

	if selectedPlan == nil {
		return nil, fmt.Errorf("no matching plan found for selected option")
	}

	log.Printf("✅ [PLANNER-INTEGRATION] Selected plan: %s", selectedPlan.ID)
	log.Printf("📊 [PLANNER-INTEGRATION] Plan score: %.2f", selectedPlan.Score)
	log.Printf("🎯 [PLANNER-INTEGRATION] Selected option: %s (%s)", selectedOption.TaskName, selectedOption.Language)

	// Check principles compliance
	isCompliant, complianceScore, err := pi.feedbackEvaluator.CheckPrinciplesCompliance(selectedOption)
	if err != nil {
		log.Printf("⚠️ [PLANNER-INTEGRATION] Failed to check principles compliance: %v", err)
	} else {
		log.Printf("🛡️ [PLANNER-INTEGRATION] Principles compliance: %v (score: %.2f)", isCompliant, complianceScore)
		if !isCompliant {
			log.Printf("⚠️ [PLANNER-INTEGRATION] Plan may violate principles, proceeding with caution")
		}
	}

	// Execute the plan with the specific workflow ID
	episode, err := pi.planner.ExecutePlan(pi.ctx, *selectedPlan, workflowID)

	// Record feedback regardless of success/failure
	pi.recordExecutionFeedback(selectedOption, episode, err)

	if err != nil {
		return nil, fmt.Errorf("plan execution failed: %v", err)
	}

	log.Printf("🎉 [PLANNER-INTEGRATION] Task completed successfully")
	return episode, nil
}

// planAndExecuteTraditionally plans and executes using traditional planning
func (pi *PlannerIntegration) planAndExecuteTraditionally(
	userRequest string,
	taskName string,
	description string,
	context map[string]string,
) (*planner.Episode, error) {
	// Convert context to interface{} map for planner
	goalParams := make(map[string]interface{})
	for k, v := range context {
		goalParams[k] = v
	}

	// Create goal for planner
	goal := planner.Goal{
		ID:     fmt.Sprintf("goal_%d", time.Now().UnixNano()),
		Type:   taskName,
		Params: goalParams,
	}

	// Generate plans using the base planner
	plans, err := pi.planner.GeneratePlans(pi.ctx, goal)
	if err != nil {
		return nil, fmt.Errorf("plan generation failed: %v", err)
	}

	if len(plans) == 0 {
		return nil, fmt.Errorf("no plans generated for goal: %s", goal.Type)
	}

	// Convert plans to options for feedback evaluator
	options := make([]planner.Option, len(plans))
	for i, plan := range plans {
		// Extract task name and language from the first step
		language := "unknown"
		if len(plan.Steps) > 0 {
			// Try to get language from capability
			if cap, err := pi.planner.GetCapability(pi.ctx, plan.Steps[0].CapabilityID); err == nil {
				language = cap.Language
			}
		}

		options[i] = planner.Option{
			TaskName: goal.Type,
			Language: language,
			Score:    plan.Score,
		}
	}

	// Use feedback evaluator to select best option
	selectedOption, err := pi.feedbackEvaluator.SelectBest(options)
	if err != nil {
		return nil, fmt.Errorf("feedback evaluation failed: %v", err)
	}

	// Find the corresponding plan
	var selectedPlan *planner.Plan
	for i, option := range options {
		if option.TaskName == selectedOption.TaskName && option.Language == selectedOption.Language {
			selectedPlan = &plans[i]
			break
		}
	}

	if selectedPlan == nil {
		return nil, fmt.Errorf("no matching plan found for selected option")
	}

	log.Printf("✅ [PLANNER-INTEGRATION] Selected plan: %s", selectedPlan.ID)
	log.Printf("📊 [PLANNER-INTEGRATION] Plan score: %.2f", selectedPlan.Score)
	log.Printf("🎯 [PLANNER-INTEGRATION] Selected option: %s (%s)", selectedOption.TaskName, selectedOption.Language)

	// Check principles compliance
	isCompliant, complianceScore, err := pi.feedbackEvaluator.CheckPrinciplesCompliance(selectedOption)
	if err != nil {
		log.Printf("⚠️ [PLANNER-INTEGRATION] Failed to check principles compliance: %v", err)
	} else {
		log.Printf("🛡️ [PLANNER-INTEGRATION] Principles compliance: %v (score: %.2f)", isCompliant, complianceScore)
		if !isCompliant {
			log.Printf("⚠️ [PLANNER-INTEGRATION] Plan may violate principles, proceeding with caution")
		}
	}

	// Execute the plan
	episode, err := pi.planner.ExecutePlan(pi.ctx, *selectedPlan, userRequest)

	// Record feedback regardless of success/failure
	pi.recordExecutionFeedback(selectedOption, episode, err)

	if err != nil {
		return nil, fmt.Errorf("plan execution failed: %v", err)
	}

	log.Printf("🎉 [PLANNER-INTEGRATION] Task completed successfully")
	return episode, nil
}

// recordExecutionFeedback records feedback after plan execution
func (pi *PlannerIntegration) recordExecutionFeedback(option planner.Option, episode *planner.Episode, execErr error) {
	// Calculate execution time
	execTime := time.Since(episode.Timestamp)

	// Determine success
	success := execErr == nil

	// Count principle violations (simplified - in real implementation, this would come from principles check)
	violatedPrinciples := 0
	if execErr != nil {
		// If execution failed, assume it might be due to principle violations
		violatedPrinciples = 1
	}

	// Record feedback in self-model
	err := pi.feedbackEvaluator.RecordFeedback(
		option.TaskName,
		option.Language,
		success,
		execTime,
		violatedPrinciples,
	)

	if err != nil {
		log.Printf("⚠️ [PLANNER-INTEGRATION] Failed to record feedback: %v", err)
	} else {
		log.Printf("📊 [PLANNER-INTEGRATION] Recorded feedback: %s (%s) - Success: %v, Time: %v, Violations: %d",
			option.TaskName, option.Language, success, execTime, violatedPrinciples)
	}

	// Also record metrics for monitor UI
	pi.recordMonitorMetrics(success, execTime)

	// Best-effort: index episodic memory via API server's client
	if pi.apiServer != nil && pi.apiServer.episodicClient != nil {
		outcome := "failure"
		if success {
			outcome = "success"
		}
		ep := &mempkg.EpisodicRecord{
			SessionID: "", // could be threaded from context in future
			PlanID:    episode.ID,
			Timestamp: time.Now().UTC(),
			Outcome:   outcome,
			Reward:    0,
			Tags:      []string{"planner"},
			StepIndex: 0,
			Text:      fmt.Sprintf("%s via %s", option.TaskName, option.Language),
			Metadata: map[string]any{
				"exec_ms":  execTime.Milliseconds(),
				"language": option.Language,
				"error": func() string {
					if execErr != nil {
						return execErr.Error()
					}
					return ""
				}(),
			},
		}
		if pi.apiServer.vectorDB != nil {
			vec := toyEmbed(ep.Text, 8)
			_ = pi.apiServer.vectorDB.IndexEpisode(ep, vec)
		}
	}
}

// recordMonitorMetrics records metrics in the format expected by the monitor UI
func (pi *PlannerIntegration) recordMonitorMetrics(success bool, execTime time.Duration) {
	ctx := context.Background()

	// Increment total executions
	totalExec, _ := pi.redisClient.Get(ctx, "metrics:total_executions").Int()
	pi.redisClient.Set(ctx, "metrics:total_executions", totalExec+1, 0)

	// Increment successful executions if successful
	if success {
		successExec, _ := pi.redisClient.Get(ctx, "metrics:successful_executions").Int()
		pi.redisClient.Set(ctx, "metrics:successful_executions", successExec+1, 0)
	}

	// Update average execution time (simple moving average)
	avgTime, _ := pi.redisClient.Get(ctx, "metrics:avg_execution_time").Float64()
	newAvg := (avgTime*float64(totalExec) + execTime.Seconds()*1000) / float64(totalExec+1)
	pi.redisClient.Set(ctx, "metrics:avg_execution_time", newAvg, 0)

	// Update last execution time
	pi.redisClient.Set(ctx, "metrics:last_execution", time.Now().Format(time.RFC3339), 0)

	log.Printf("📈 [PLANNER-INTEGRATION] Updated monitor metrics: Total=%d, Success=%v, AvgTime=%.2fms",
		totalExec+1, success, newAvg)
}

// waitForWorkflowCompletion waits for a workflow to complete
func (pi *PlannerIntegration) waitForWorkflowCompletion(workflowID string) {
	// Simple polling mechanism
	for {
		status, err := pi.hierarchicalIntegration.GetWorkflowStatus(workflowID)
		if err != nil {
			log.Printf("⚠️ [PLANNER-INTEGRATION] Failed to get workflow status: %v", err)
			break
		}

		if status.Status == "completed" || status.Status == "failed" || status.Status == "cancelled" {
			break
		}

		time.Sleep(100 * time.Millisecond)
	}
}

// RegisterCapability registers a capability with the planner
func (pi *PlannerIntegration) RegisterCapability(capability *planner.Capability) error {
	log.Printf("📝 [PLANNER-INTEGRATION] Registering capability: %s", capability.TaskName)

	// Register with both planners
	err := pi.planner.SaveCapability(pi.ctx, *capability)
	if err != nil {
		return fmt.Errorf("failed to register with base planner: %v", err)
	}

	if pi.hierarchicalIntegration != nil {
		err = pi.hierarchicalIntegration.RegisterCapability(capability)
		if err != nil {
			log.Printf("⚠️ [PLANNER-INTEGRATION] Failed to register with hierarchical planner: %v", err)
		}
	}

	return nil
}

// ListCapabilities returns all registered capabilities
func (pi *PlannerIntegration) ListCapabilities() ([]planner.Capability, error) {
	return pi.planner.ListCapabilities(pi.ctx)
}

// LoadEpisode loads an episode by ID
func (pi *PlannerIntegration) LoadEpisode(episodeID string) (*planner.Episode, error) {
	return pi.planner.LoadEpisode(pi.ctx, episodeID)
}

// -----------------------------
// Hierarchical Planning Methods
// -----------------------------

// StartHierarchicalWorkflow starts a hierarchical workflow
func (pi *PlannerIntegration) StartHierarchicalWorkflow(
	userRequest string,
	taskName string,
	description string,
	context map[string]string,
) (*planner.WorkflowExecution, error) {
	if pi.hierarchicalIntegration == nil {
		return nil, fmt.Errorf("hierarchical planning not available")
	}

	return pi.hierarchicalIntegration.PlanAndExecuteHierarchically(
		userRequest,
		taskName,
		description,
		context,
	)
}

// GetWorkflowStatus gets the status of a workflow
func (pi *PlannerIntegration) GetWorkflowStatus(workflowID string) (*planner.WorkflowStatus, error) {
	if pi.hierarchicalIntegration == nil {
		return nil, fmt.Errorf("hierarchical planning not available")
	}

	return pi.hierarchicalIntegration.GetWorkflowStatus(workflowID)
}

// GetWorkflowDetails gets full details of a workflow for UI
func (pi *PlannerIntegration) GetWorkflowDetails(workflowID string) (*planner.WorkflowDetails, error) {
	if pi.hierarchicalIntegration == nil {
		return nil, fmt.Errorf("hierarchical planning not available")
	}
	return pi.hierarchicalIntegration.GetWorkflowDetails(workflowID)
}

// PauseWorkflow pauses a running workflow
func (pi *PlannerIntegration) PauseWorkflow(workflowID string, reason string) error {
	if pi.hierarchicalIntegration == nil {
		return fmt.Errorf("hierarchical planning not available")
	}

	return pi.hierarchicalIntegration.PauseWorkflow(workflowID, reason)
}

// ResumeWorkflow resumes a paused workflow
func (pi *PlannerIntegration) ResumeWorkflow(workflowID string, resumeToken string) error {
	if pi.hierarchicalIntegration == nil {
		return fmt.Errorf("hierarchical planning not available")
	}

	return pi.hierarchicalIntegration.ResumeWorkflow(workflowID, resumeToken)
}

// LoadMCPToolsAsCapabilities fetches all tools from the API Server and registers them
// as planner capabilities, allowing the planner to use them in plans.
func (pi *PlannerIntegration) LoadMCPToolsAsCapabilities() error {
	if pi.apiServer == nil {
		return fmt.Errorf("API server reference not available")
	}

	// Use the internal method of APIServer to get tools directly
	// This avoids HTTP overhead and auth issues during internal startup
	tools, err := pi.apiServer.listTools(pi.ctx)
	if err != nil {
		return fmt.Errorf("failed to list tools from API server: %v", err)
	}

	log.Printf("📥 [PLANNER-INTEGRATION] Loading %d tools as capabilities...", len(tools))

	count := 0
	for _, tool := range tools {
		// Create a capability definition from the tool
		cap := planner.Capability{
			ID:           tool.ID,
			TaskName:     tool.Name,
			Description:  tool.Description,
			Language:     "mcp_tool", // Special marker for the executor
			ContextCheck: "",         // Always available
			Code:         "",         // No code, handled via tool invocation
			Type:         "tool",
			Risk:         tool.SafetyLevel,
			Tags:         []string{"mcp", "tool", tool.ID},
		}

		// Map input schema to parameters
		// This helps the planner understand what args to generate
		// Note: We might need a better mapping if the planner supports structured schemas
		// For now we just add them as tags or description hints could be added

		if err := pi.RegisterCapability(&cap); err != nil {
			log.Printf("⚠️ [PLANNER-INTEGRATION] Failed to register tool aspect %s: %v", tool.ID, err)
		} else {
			count++
		}
	}

	log.Printf("✅ [PLANNER-INTEGRATION] Successfully loaded %d/%d tools as capabilities from registry", count, len(tools))
	return nil
}

// CancelWorkflow cancels a workflow
func (pi *PlannerIntegration) CancelWorkflow(workflowID string) error {
	if pi.hierarchicalIntegration == nil {
		return fmt.Errorf("hierarchical planning not available")
	}

	return pi.hierarchicalIntegration.CancelWorkflow(workflowID)
}

// ListActiveWorkflows lists all active workflows
func (pi *PlannerIntegration) ListActiveWorkflows() []*planner.WorkflowStatus {
	if pi.hierarchicalIntegration == nil {
		return []*planner.WorkflowStatus{}
	}

	return pi.hierarchicalIntegration.ListActiveWorkflows()
}

// SubscribeToWorkflowEvents subscribes to workflow events
func (pi *PlannerIntegration) SubscribeToWorkflowEvents(workflowID string) (<-chan planner.WorkflowEvent, error) {
	if pi.hierarchicalIntegration == nil {
		return nil, fmt.Errorf("hierarchical planning not available")
	}

	return pi.hierarchicalIntegration.SubscribeToWorkflowEvents(workflowID)
}

// RegisterWorkflowTemplate registers a workflow template
func (pi *PlannerIntegration) RegisterWorkflowTemplate(template *planner.WorkflowTemplate) error {
	if pi.hierarchicalIntegration == nil {
		return fmt.Errorf("hierarchical planning not available")
	}

	return pi.hierarchicalIntegration.RegisterWorkflowTemplate(template)
}

// ListWorkflowTemplates lists all workflow templates
func (pi *PlannerIntegration) ListWorkflowTemplates() ([]*planner.WorkflowTemplate, error) {
	if pi.hierarchicalIntegration == nil {
		return []*planner.WorkflowTemplate{}, nil
	}

	return pi.hierarchicalIntegration.ListWorkflowTemplates()
}

// ConvertDynamicActionToCapability converts HDN DynamicAction to planner Capability
func ConvertDynamicActionToCapability(action *DynamicAction) *planner.Capability {
	// Convert preconditions to string slice
	preconds := make([]string, len(action.Preconditions))
	copy(preconds, action.Preconditions)

	// Convert effects to map
	effects := make(map[string]interface{})
	for _, effect := range action.Effects {
		effects[effect] = true
	}

	// Convert context to input signature
	inputSig := make(map[string]string)
	for k := range action.Context {
		inputSig[k] = "string" // Default type
	}

	// Convert context to string for context values
	contextStr := ""
	if len(action.Context) > 0 {
		contextBytes, _ := json.Marshal(action.Context)
		contextStr = string(contextBytes)
	}

	return &planner.Capability{
		ID:         action.ID,
		TaskName:   action.Task,
		Entrypoint: fmt.Sprintf("%s.%s", action.Language, action.Task),
		Language:   action.Language,
		InputSig:   inputSig,
		Outputs:    action.Effects,
		Preconds:   preconds,
		Effects:    effects,
		Score:      0.8, // Default confidence score
		CreatedAt:  action.CreatedAt,
		LastUsed:   time.Now(),
		Validation: map[string]interface{}{
			"context": contextStr,
			"domain":  action.Domain,
		},
		Permissions: action.Tags,
	}
}

// ConvertToolToCapability converts a Tool to a planner Capability
func ConvertToolToCapability(tool Tool) *planner.Capability {
	// Convert input schema to input signature
	inputSig := make(map[string]string)
	for k, v := range tool.InputSchema {
		inputSig[k] = v
	}

	// Convert output schema to outputs list
	outputs := make([]string, 0, len(tool.OutputSchema))
	for k := range tool.OutputSchema {
		outputs = append(outputs, k)
	}

	// Create effects map from outputs
	effects := make(map[string]interface{})
	for _, output := range outputs {
		effects[output] = true
	}

	// Determine language from tool execution spec or default to "tool"
	language := "tool"
	if tool.Exec != nil {
		if tool.Exec.Type == "cmd" {
			// Try to infer language from command path
			if strings.Contains(tool.Exec.Cmd, "python") {
				language = "python"
			} else if strings.Contains(tool.Exec.Cmd, "node") {
				language = "javascript"
			} else if strings.Contains(tool.Exec.Cmd, "go") {
				language = "go"
			}
		} else if tool.Exec.Type == "image" {
			language = "docker"
		}
	}

	// Use tool name as task name, or ID if name is empty
	taskName := tool.Name
	if taskName == "" {
		taskName = tool.ID
	}

	// Create entrypoint - for tools, use the tool ID as the entrypoint
	entrypoint := tool.ID
	if tool.Exec != nil && tool.Exec.Type == "cmd" {
		entrypoint = tool.Exec.Cmd
	} else if tool.Exec != nil && tool.Exec.Type == "image" {
		entrypoint = tool.Exec.Image
	}

	return &planner.Capability{
		ID:         tool.ID,
		TaskName:   taskName,
		Entrypoint: entrypoint,
		Language:   language,
		InputSig:   inputSig,
		Outputs:    outputs,
		Preconds:   []string{}, // Tools don't have explicit preconditions
		Effects:    effects,
		Score:      0.85, // High confidence for system tools
		CreatedAt:  tool.CreatedAt,
		LastUsed:   time.Now(),
		Validation: map[string]interface{}{
			"tool_id":      tool.ID,
			"safety_level": tool.SafetyLevel,
			"permissions":  tool.Permissions,
			"exec_type": func() string {
				if tool.Exec != nil {
					return tool.Exec.Type
				}
				return "builtin"
			}(),
		},
		Permissions: tool.Permissions,
	}
}

// categorizeRequestForSafety uses LLM to intelligently categorize a request for safety evaluation
func (pi *PlannerIntegration) categorizeRequestForSafety(userRequest, taskName, description string, context map[string]string) (map[string]interface{}, error) {
	// Use the intelligent executor's LLM client if available
	if pi.hierarchicalIntegration != nil {
		// Try to get the intelligent executor from the hierarchical integration
		// For now, use fallback categorization
	}

	// Fallback: use simple keyword-based categorization
	harmfulKeywords := []string{"delete", "remove", "harm", "hurt", "inappropriate", "malicious", "dangerous"}
	taskLower := strings.ToLower(taskName)
	descLower := strings.ToLower(description)
	reqLower := strings.ToLower(userRequest)

	safetyContext := map[string]interface{}{
		"human_harm":        false,
		"human_order":       true,
		"self_harm":         false,
		"privacy_violation": false,
		"endanger_others":   false,
		"order_unethical":   false,
		"discrimination":    false,
	}

	// Check for harmful keywords
	for _, keyword := range harmfulKeywords {
		if strings.Contains(taskLower, keyword) || strings.Contains(descLower, keyword) || strings.Contains(reqLower, keyword) {
			if keyword == "delete" || keyword == "remove" {
				safetyContext["endanger_others"] = true
			} else if keyword == "inappropriate" {
				safetyContext["order_unethical"] = true
			} else if keyword == "harm" || keyword == "hurt" || keyword == "malicious" || keyword == "dangerous" {
				safetyContext["human_harm"] = true
			}
		}
	}

	return safetyContext, nil
}

// convertMapToStringMap converts map[string]interface{} to map[string]string
func convertMapToStringMap(input map[string]interface{}) map[string]string {
	result := make(map[string]string)
	for k, v := range input {
		if str, ok := v.(string); ok {
			result[k] = str
		} else {
			result[k] = fmt.Sprintf("%v", v)
		}
	}
	return result
}
