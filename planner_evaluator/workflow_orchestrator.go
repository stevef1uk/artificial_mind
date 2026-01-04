// FILE: workflow_orchestrator.go
package planner

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// -----------------------------
// Workflow Orchestrator
// -----------------------------

// WorkflowOrchestrator manages the execution of hierarchical workflows
type WorkflowOrchestrator struct {
	ctx             context.Context
	redis           *redis.Client
	planner         *HierarchicalPlanner
	executor        Executor
	activeWorkflows map[string]*WorkflowExecution
	mutex           sync.RWMutex
	eventChannels   map[string]chan WorkflowEvent
}

// WorkflowExecution represents an active workflow execution
type WorkflowExecution struct {
	ID           string            `json:"id"`
	Plan         *HierarchicalPlan `json:"plan"`
	Context      *ExecutionContext `json:"context"`
	Status       string            `json:"status"` // "running", "paused", "completed", "failed", "cancelled"
	StartedAt    time.Time         `json:"started_at"`
	LastActivity time.Time         `json:"last_activity"`
	Progress     WorkflowProgress  `json:"progress"`
	Error        string            `json:"error,omitempty"`
	PauseReason  string            `json:"pause_reason,omitempty"`
	ResumeToken  string            `json:"resume_token,omitempty"`
}

// WorkflowProgress tracks execution progress
type WorkflowProgress struct {
	TotalSteps     int     `json:"total_steps"`
	CompletedSteps int     `json:"completed_steps"`
	FailedSteps    int     `json:"failed_steps"`
	SkippedSteps   int     `json:"skipped_steps"`
	CurrentStep    string  `json:"current_step"`
	Percentage     float64 `json:"percentage"`
	ETA            string  `json:"eta,omitempty"`
}

// WorkflowEvent represents an event in workflow execution
type WorkflowEvent struct {
	Type       string                 `json:"type"` // "step_started", "step_completed", "step_failed", "workflow_paused", "workflow_resumed", "workflow_completed", "workflow_failed"
	WorkflowID string                 `json:"workflow_id"`
	StepID     string                 `json:"step_id,omitempty"`
	Timestamp  time.Time              `json:"timestamp"`
	Data       map[string]interface{} `json:"data,omitempty"`
}

// WorkflowStatus represents the current status of a workflow
type WorkflowStatus struct {
	ID           string           `json:"id"`
	Status       string           `json:"status"`
	Progress     WorkflowProgress `json:"progress"`
	CurrentStep  string           `json:"current_step"`
	Error        string           `json:"error,omitempty"`
	StartedAt    time.Time        `json:"started_at"`
	LastActivity time.Time        `json:"last_activity"`
	CanResume    bool             `json:"can_resume"`
	CanCancel    bool             `json:"can_cancel"`
}

// WorkflowDetails provides a full view of a workflow for visualization
type WorkflowDetails struct {
	ID             string           `json:"id"`
	Status         string           `json:"status"`
	Progress       WorkflowProgress `json:"progress"`
	CurrentStep    string           `json:"current_step"`
	Error          string           `json:"error,omitempty"`
	StartedAt      time.Time        `json:"started_at"`
	LastActivity   time.Time        `json:"last_activity"`
	ExecutionOrder []string         `json:"execution_order"`
	Steps          []WorkflowStep   `json:"steps"`
}

// NewWorkflowOrchestrator creates a new workflow orchestrator
func NewWorkflowOrchestrator(ctx context.Context, redis *redis.Client, planner *HierarchicalPlanner, executor Executor) *WorkflowOrchestrator {
	return &WorkflowOrchestrator{
		ctx:             ctx,
		redis:           redis,
		planner:         planner,
		executor:        executor,
		activeWorkflows: make(map[string]*WorkflowExecution),
		eventChannels:   make(map[string]chan WorkflowEvent),
	}
}

// -----------------------------
// Workflow Execution Management
// -----------------------------

// StartWorkflow starts a new workflow execution
func (wo *WorkflowOrchestrator) StartWorkflow(ctx context.Context, plan *HierarchicalPlan, userRequest string) (*WorkflowExecution, error) {
	log.Printf("🚀 [ORCHESTRATOR] Starting workflow for plan: %s", plan.ID)

	execution := &WorkflowExecution{
		ID:           uuid.New().String(),
		Plan:         plan,
		Context:      &ExecutionContext{PlanID: plan.ID, State: make(map[string]interface{}), Variables: make(map[string]interface{}), Results: make(map[string]interface{}), StepResults: make(map[string]interface{}), StartedAt: time.Now(), LastUpdated: time.Now()},
		Status:       "running",
		StartedAt:    time.Now(),
		LastActivity: time.Now(),
		Progress: WorkflowProgress{
			TotalSteps:     len(plan.Steps),
			CompletedSteps: 0,
			FailedSteps:    0,
			SkippedSteps:   0,
			CurrentStep:    "",
			Percentage:     0.0,
		},
	}

	// Register the workflow
	wo.mutex.Lock()
	wo.activeWorkflows[execution.ID] = execution
	wo.eventChannels[execution.ID] = make(chan WorkflowEvent, 100)
	wo.mutex.Unlock()

	_ = wo.redis.SAdd(context.Background(), "active_workflows", execution.ID).Err()

	// Start execution in a goroutine
	go wo.executeWorkflow(ctx, execution, userRequest)

	log.Printf("✅ [ORCHESTRATOR] Workflow %s started", execution.ID)
	return execution, nil
}

// executeWorkflow executes a workflow asynchronously
func (wo *WorkflowOrchestrator) executeWorkflow(ctx context.Context, execution *WorkflowExecution, userRequest string) {
	defer func() {
		wo.mutex.Lock()
		// Keep completed workflows in the map for a short time to allow status queries
		// Only remove failed or cancelled workflows immediately
		if execution.Status == "completed" {
			// Store completed workflow in Redis for persistence
			wo.storeCompletedWorkflowInRedis(execution)

			// Keep completed workflows in memory for 30 seconds to allow status queries
			go func() {
				time.Sleep(30 * time.Second)
				wo.mutex.Lock()
				delete(wo.activeWorkflows, execution.ID)
				wo.mutex.Unlock()
			}()
		} else {
			// Remove failed/cancelled workflows immediately
			delete(wo.activeWorkflows, execution.ID)
		}
		close(wo.eventChannels[execution.ID])
		delete(wo.eventChannels, execution.ID)
		wo.mutex.Unlock()
	}()

	// Emit workflow started event
	wo.emitEvent(execution.ID, WorkflowEvent{
		Type:       "workflow_started",
		WorkflowID: execution.ID,
		Timestamp:  time.Now(),
		Data:       map[string]interface{}{"plan_id": execution.Plan.ID, "total_steps": len(execution.Plan.Steps)},
	})

	// Execute steps with proper dependency resolution
	log.Printf("🔄 [ORCHESTRATOR] Starting execution of %d steps: %v", len(execution.Plan.ExecutionOrder), execution.Plan.ExecutionOrder)

	// Track pending steps that need to be re-evaluated
	pendingSteps := make(map[string]bool)
	for _, stepID := range execution.Plan.ExecutionOrder {
		pendingSteps[stepID] = true
	}

	// Keep track of steps that have been processed in this iteration
	processedInIteration := make(map[string]bool)

	// Continue until all steps are completed or failed
	for len(pendingSteps) > 0 {
		// Check if workflow was cancelled
		if execution.Status == "cancelled" {
			log.Printf("🛑 [ORCHESTRATOR] Workflow %s was cancelled", execution.ID)
			return
		}

		// Check if workflow was paused
		if execution.Status == "paused" {
			log.Printf("⏸️ [ORCHESTRATOR] Workflow %s is paused", execution.ID)
			wo.waitForResume(execution)
		}

		// Reset processed steps for this iteration
		processedInIteration = make(map[string]bool)

		// Process steps in execution order
		for _, stepID := range execution.Plan.ExecutionOrder {
			// Skip if step is not pending
			if !pendingSteps[stepID] {
				continue
			}

			log.Printf("🔄 [ORCHESTRATOR] Processing step: %s", stepID)

			step := wo.findStepByID(execution.Plan.Steps, stepID)
			if step == nil {
				log.Printf("❌ [ORCHESTRATOR] Step not found: %s", stepID)
				wo.failWorkflow(execution, fmt.Sprintf("step not found: %s", stepID))
				return
			}

			log.Printf("🔍 [ORCHESTRATOR] Found step: %s (type: %s, deps: %v)", stepID, step.StepType, step.Dependencies)

			// Update progress
			execution.Progress.CurrentStep = stepID
			execution.Progress.Percentage = float64(execution.Progress.CompletedSteps) / float64(execution.Progress.TotalSteps) * 100
			execution.LastActivity = time.Now()

			// Check if step can be executed
			canExecute := wo.canExecuteStep(step, execution.Context)
			log.Printf("🔍 [ORCHESTRATOR] Step %s can execute: %v", stepID, canExecute)
			if !canExecute {
				log.Printf("⏳ [ORCHESTRATOR] Step %s waiting for dependencies: %v", stepID, step.Dependencies)
				step.Status = "pending"
				// Keep this step in pendingSteps for next iteration
				continue
			}

			// Emit step started event
			wo.emitEvent(execution.ID, WorkflowEvent{
				Type:       "step_started",
				WorkflowID: execution.ID,
				StepID:     stepID,
				Timestamp:  time.Now(),
				Data:       map[string]interface{}{"step_type": step.StepType, "capability_id": step.CapabilityID},
			})

			// Execute the step
			result, err := wo.executeStep(ctx, step, execution.Context, execution.ID)
			if err != nil {
				log.Printf("❌ [ORCHESTRATOR] Step %s failed: %v", stepID, err)

				// Handle retries - check Redis for persistent retry count
				currentRetryCount := wo.getStepRetryCount(execution.ID, stepID)
				if currentRetryCount < step.MaxRetries {
					newRetryCount := currentRetryCount + 1
					wo.setStepRetryCount(execution.ID, stepID, newRetryCount)
					step.RetryCount = newRetryCount
					log.Printf("🔄 [ORCHESTRATOR] Retrying step %s (attempt %d/%d)", stepID, newRetryCount, step.MaxRetries)

					// Emit retry event
					wo.emitEvent(execution.ID, WorkflowEvent{
						Type:       "step_retry",
						WorkflowID: execution.ID,
						StepID:     stepID,
						Timestamp:  time.Now(),
						Data:       map[string]interface{}{"retry_count": newRetryCount, "max_retries": step.MaxRetries},
					})

					// Keep step pending for retry
					continue
				}

				// Step failed after all retries
				step.Status = "failed"
				step.Error = err.Error()
				execution.Progress.FailedSteps++

				// Emit step failed event
				wo.emitEvent(execution.ID, WorkflowEvent{
					Type:       "step_failed",
					WorkflowID: execution.ID,
					StepID:     stepID,
					Timestamp:  time.Now(),
					Data:       map[string]interface{}{"error": err.Error(), "retry_count": step.RetryCount},
				})

				// Check if this is a critical step
				if wo.isCriticalStep(step) {
					wo.failWorkflow(execution, fmt.Sprintf("critical step %s failed: %v", stepID, err))
					return
				}

				// Non-critical step failed, remove from pending
				delete(pendingSteps, stepID)
				processedInIteration[stepID] = true
				continue
			}

			// Step completed successfully
			step.Status = "completed"
			step.Result = result
			execution.Progress.CompletedSteps++

			// Clear retry count since step completed successfully
			wo.clearStepRetryCount(execution.ID, stepID)

			// Update execution context
			wo.updateStateFromStep(step, result, execution.Context)

			// Emit step completed event
			wo.emitEvent(execution.ID, WorkflowEvent{
				Type:       "step_completed",
				WorkflowID: execution.ID,
				StepID:     stepID,
				Timestamp:  time.Now(),
				Data:       map[string]interface{}{"result": result},
			})

			log.Printf("✅ [ORCHESTRATOR] Step %s completed successfully", stepID)

			// Remove from pending steps
			delete(pendingSteps, stepID)
			processedInIteration[stepID] = true
		}

		// If no steps were processed in this iteration, we have a deadlock
		if len(processedInIteration) == 0 {
			log.Printf("❌ [ORCHESTRATOR] Workflow deadlock detected - no steps can be executed")
			wo.failWorkflow(execution, "workflow deadlock: no steps can be executed due to unmet dependencies")
			return
		}
	}

	// Workflow completed - only mark as completed if all steps were actually completed
	if execution.Progress.FailedSteps == 0 && execution.Progress.CompletedSteps == execution.Progress.TotalSteps {
		execution.Status = "completed"
		execution.Progress.Percentage = 100.0

		// Update metrics in Redis
		wo.updateMetrics(execution, true)

		// Emit workflow completed event
		wo.emitEvent(execution.ID, WorkflowEvent{
			Type:       "workflow_completed",
			WorkflowID: execution.ID,
			Timestamp:  time.Now(),
			Data:       map[string]interface{}{"total_steps": execution.Progress.TotalSteps, "completed_steps": execution.Progress.CompletedSteps},
		})

		log.Printf("🎉 [ORCHESTRATOR] Workflow %s completed successfully", execution.ID)
	} else if execution.Progress.FailedSteps > 0 {
		wo.updateMetrics(execution, false)
		wo.failWorkflow(execution, fmt.Sprintf("workflow completed with %d failed steps", execution.Progress.FailedSteps))
	} else {
		// Workflow didn't complete all steps - mark as failed
		wo.updateMetrics(execution, false)
		wo.failWorkflow(execution, fmt.Sprintf("workflow incomplete: only %d/%d steps completed", execution.Progress.CompletedSteps, execution.Progress.TotalSteps))
	}
}

// executeStep executes a single workflow step
func (wo *WorkflowOrchestrator) executeStep(ctx context.Context, step *WorkflowStep, execCtx *ExecutionContext, workflowID string) (interface{}, error) {
	log.Printf("🔄 [ORCHESTRATOR] Executing step: %s (type: %s)", step.ID, step.StepType)

	now := time.Now()
	step.StartedAt = &now
	step.Status = "running"

	switch step.StepType {
	case "capability":
		return wo.executeCapabilityStep(ctx, step, execCtx, workflowID)
	case "subgoal":
		return wo.executeSubGoalStep(ctx, step, execCtx)
	case "condition":
		return wo.executeConditionStep(ctx, step, execCtx)
	case "loop":
		return wo.executeLoopStep(ctx, step, execCtx, workflowID)
	default:
		return nil, fmt.Errorf("unknown step type: %s", step.StepType)
	}
}

// executeCapabilityStep executes a capability step
func (wo *WorkflowOrchestrator) executeCapabilityStep(ctx context.Context, step *WorkflowStep, execCtx *ExecutionContext, workflowID string) (interface{}, error) {
	// Create a simple plan for the capability
	capabilityPlan := Plan{
		ID:   uuid.New().String(),
		Goal: Goal{ID: step.ID, Type: step.CapabilityID, Params: step.Args},
		Steps: []PlanStep{
			{
				CapabilityID:  step.CapabilityID,
				Args:          step.Args,
				EstimatedCost: step.EstimatedCost,
				Confidence:    step.Confidence,
			},
		},
		EstimatedUtility: 0.8,
		PrinciplesRisk:   0.0,
		Score:            step.Confidence,
	}

	// Execute using the executor
	result, err := wo.executor.ExecutePlan(ctx, capabilityPlan, workflowID)
	if err != nil {
		step.Error = err.Error()
		step.Status = "failed"
		return nil, err
	}

	step.Result = result
	step.Status = "completed"
	now := time.Now()
	step.CompletedAt = &now

	return result, nil
}

// executeSubGoalStep executes a sub-goal step by recursively planning and executing
func (wo *WorkflowOrchestrator) executeSubGoalStep(ctx context.Context, step *WorkflowStep, execCtx *ExecutionContext) (interface{}, error) {
	if step.SubGoal == nil {
		return nil, fmt.Errorf("sub-goal step has no sub-goal")
	}

	// Generate a hierarchical plan for the sub-goal
	subPlan, err := wo.planner.GenerateHierarchicalPlan(ctx, *step.SubGoal)
	if err != nil {
		return nil, fmt.Errorf("failed to generate sub-plan: %v", err)
	}

	// Execute the sub-plan directly instead of creating a sub-workflow
	subExecCtx, err := wo.planner.ExecuteHierarchicalPlan(ctx, subPlan, fmt.Sprintf("Sub-goal: %s", step.SubGoal.Type))
	if err != nil {
		step.Error = err.Error()
		step.Status = "failed"
		return nil, err
	}

	step.Result = subExecCtx.Results
	step.Status = "completed"
	now := time.Now()
	step.CompletedAt = &now

	return subExecCtx.Results, nil
}

// executeConditionStep executes a conditional step
func (wo *WorkflowOrchestrator) executeConditionStep(ctx context.Context, step *WorkflowStep, execCtx *ExecutionContext) (interface{}, error) {
	// Evaluate the condition
	conditionMet, err := wo.evaluateCondition(step.Condition, execCtx)
	if err != nil {
		return nil, fmt.Errorf("condition evaluation failed: %v", err)
	}

	if conditionMet {
		step.Status = "completed"
		step.Result = "condition_met"
	} else {
		step.Status = "skipped"
		step.Result = "condition_not_met"
		// Note: Skipped steps are tracked at the workflow level, not execution context level
	}

	now := time.Now()
	step.CompletedAt = &now

	return step.Result, nil
}

// executeLoopStep executes a loop step
func (wo *WorkflowOrchestrator) executeLoopStep(ctx context.Context, step *WorkflowStep, execCtx *ExecutionContext, workflowID string) (interface{}, error) {
	if step.LoopConfig == nil {
		return nil, fmt.Errorf("loop step has no loop configuration")
	}

	var results []interface{}
	iteration := 0

	for iteration < step.LoopConfig.MaxIterations {
		// Check loop condition
		conditionMet, err := wo.evaluateCondition(step.LoopConfig.Condition, execCtx)
		if err != nil {
			return nil, fmt.Errorf("loop condition evaluation failed: %v", err)
		}

		if !conditionMet {
			break
		}

		// Execute loop step template
		loopStep := *step.LoopConfig.StepTemplate
		loopStep.ID = fmt.Sprintf("%s_iteration_%d", step.ID, iteration)
		loopStep.Args = wo.substituteLoopVariables(loopStep.Args, step.LoopConfig.Variable, iteration)

		result, err := wo.executeStep(ctx, &loopStep, execCtx, workflowID)
		if err != nil {
			return nil, fmt.Errorf("loop iteration %d failed: %v", iteration, err)
		}

		results = append(results, result)
		iteration++
	}

	step.Result = results
	step.Status = "completed"
	now := time.Now()
	step.CompletedAt = &now

	return results, nil
}

// -----------------------------
// Workflow Control Operations
// -----------------------------

// PauseWorkflow pauses a running workflow
func (wo *WorkflowOrchestrator) PauseWorkflow(workflowID string, reason string) error {
	wo.mutex.Lock()
	defer wo.mutex.Unlock()

	execution, exists := wo.activeWorkflows[workflowID]
	if !exists {
		return fmt.Errorf("workflow not found: %s", workflowID)
	}

	if execution.Status != "running" {
		return fmt.Errorf("workflow is not running: %s", execution.Status)
	}

	execution.Status = "paused"
	execution.PauseReason = reason
	execution.ResumeToken = uuid.New().String()

	// Emit pause event
	wo.emitEvent(workflowID, WorkflowEvent{
		Type:       "workflow_paused",
		WorkflowID: workflowID,
		Timestamp:  time.Now(),
		Data:       map[string]interface{}{"reason": reason, "resume_token": execution.ResumeToken},
	})

	log.Printf("⏸️ [ORCHESTRATOR] Workflow %s paused: %s", workflowID, reason)
	return nil
}

// ResumeWorkflow resumes a paused workflow
func (wo *WorkflowOrchestrator) ResumeWorkflow(workflowID string, resumeToken string) error {
	wo.mutex.Lock()
	defer wo.mutex.Unlock()

	execution, exists := wo.activeWorkflows[workflowID]
	if !exists {
		return fmt.Errorf("workflow not found: %s", workflowID)
	}

	if execution.Status != "paused" {
		return fmt.Errorf("workflow is not paused: %s", execution.Status)
	}

	if execution.ResumeToken != resumeToken {
		return fmt.Errorf("invalid resume token")
	}

	execution.Status = "running"
	execution.PauseReason = ""
	execution.ResumeToken = ""

	// Emit resume event
	wo.emitEvent(workflowID, WorkflowEvent{
		Type:       "workflow_resumed",
		WorkflowID: workflowID,
		Timestamp:  time.Now(),
		Data:       map[string]interface{}{"resume_token": resumeToken},
	})

	log.Printf("▶️ [ORCHESTRATOR] Workflow %s resumed", workflowID)
	return nil
}

// CancelWorkflow cancels a running or paused workflow
func (wo *WorkflowOrchestrator) CancelWorkflow(workflowID string) error {
	wo.mutex.Lock()
	defer wo.mutex.Unlock()

	execution, exists := wo.activeWorkflows[workflowID]
	if !exists {
		return fmt.Errorf("workflow not found: %s", workflowID)
	}

	if execution.Status == "completed" || execution.Status == "failed" {
		return fmt.Errorf("workflow is already finished: %s", execution.Status)
	}

	execution.Status = "cancelled"

	// Emit cancel event
	wo.emitEvent(workflowID, WorkflowEvent{
		Type:       "workflow_cancelled",
		WorkflowID: workflowID,
		Timestamp:  time.Now(),
		Data:       map[string]interface{}{"reason": "user_cancelled"},
	})

	log.Printf("🛑 [ORCHESTRATOR] Workflow %s cancelled", workflowID)
	return nil
}

// GetWorkflowStatus gets the current status of a workflow
func (wo *WorkflowOrchestrator) GetWorkflowStatus(workflowID string) (*WorkflowStatus, error) {
	wo.mutex.RLock()
	defer wo.mutex.RUnlock()

	execution, exists := wo.activeWorkflows[workflowID]
	if !exists {
		return nil, fmt.Errorf("workflow not found: %s", workflowID)
	}

	status := &WorkflowStatus{
		ID:           execution.ID,
		Status:       execution.Status,
		Progress:     execution.Progress,
		CurrentStep:  execution.Progress.CurrentStep,
		Error:        execution.Error,
		StartedAt:    execution.StartedAt,
		LastActivity: execution.LastActivity,
		CanResume:    execution.Status == "paused",
		CanCancel:    execution.Status == "running" || execution.Status == "paused",
	}

	return status, nil
}

// GetWorkflowDetails returns full details including steps and execution order
func (wo *WorkflowOrchestrator) GetWorkflowDetails(workflowID string) (*WorkflowDetails, error) {
	wo.mutex.RLock()
	defer wo.mutex.RUnlock()

	execution, exists := wo.activeWorkflows[workflowID]
	if !exists {
		return nil, fmt.Errorf("workflow not found: %s", workflowID)
	}

	// Copy steps to avoid data races
	stepsCopy := make([]WorkflowStep, len(execution.Plan.Steps))
	copy(stepsCopy, execution.Plan.Steps)

	details := &WorkflowDetails{
		ID:             execution.ID,
		Status:         execution.Status,
		Progress:       execution.Progress,
		CurrentStep:    execution.Progress.CurrentStep,
		Error:          execution.Error,
		StartedAt:      execution.StartedAt,
		LastActivity:   execution.LastActivity,
		ExecutionOrder: append([]string{}, execution.Plan.ExecutionOrder...),
		Steps:          stepsCopy,
	}

	return details, nil
}

// ListActiveWorkflows lists all active workflows
func (wo *WorkflowOrchestrator) ListActiveWorkflows() []*WorkflowStatus {
	wo.mutex.RLock()
	defer wo.mutex.RUnlock()

	var statuses []*WorkflowStatus

	// First, add in-memory active workflows
	for _, execution := range wo.activeWorkflows {
		status := &WorkflowStatus{
			ID:           execution.ID,
			Status:       execution.Status,
			Progress:     execution.Progress,
			CurrentStep:  execution.Progress.CurrentStep,
			Error:        execution.Error,
			StartedAt:    execution.StartedAt,
			LastActivity: execution.LastActivity,
			CanResume:    execution.Status == "paused",
			CanCancel:    execution.Status == "running" || execution.Status == "paused",
		}
		statuses = append(statuses, status)
	}

	// Then, add completed workflows from Redis
	wo.addCompletedWorkflowsFromRedis(&statuses)

	return statuses
}

// -----------------------------
// Event Management
// -----------------------------

// SubscribeToWorkflowEvents subscribes to workflow events
func (wo *WorkflowOrchestrator) SubscribeToWorkflowEvents(workflowID string) (<-chan WorkflowEvent, error) {
	wo.mutex.RLock()
	defer wo.mutex.RUnlock()

	channel, exists := wo.eventChannels[workflowID]
	if !exists {
		return nil, fmt.Errorf("workflow not found: %s", workflowID)
	}

	return channel, nil
}

// emitEvent emits a workflow event
func (wo *WorkflowOrchestrator) emitEvent(workflowID string, event WorkflowEvent) {
	wo.mutex.RLock()
	channel, exists := wo.eventChannels[workflowID]
	wo.mutex.RUnlock()

	if exists {
		select {
		case channel <- event:
		default:
			// Channel is full, skip event
			log.Printf("⚠️ [ORCHESTRATOR] Event channel full for workflow %s", workflowID)
		}
	}
}

// storeCompletedWorkflowInRedis stores a completed workflow in Redis for persistence
func (wo *WorkflowOrchestrator) storeCompletedWorkflowInRedis(execution *WorkflowExecution) {
	ctx := context.Background()

	// Get files from file storage for this workflow
	var files []interface{}
	wo.populateFilesFromStorage(ctx, execution.ID, &files)

	// Check if there's a workflow mapping to an intelligent workflow with more files
	intelligentWorkflowID := wo.getMappedIntelligentWorkflow(ctx, execution.ID)
	if intelligentWorkflowID != "" {
		log.Printf("🔗 [ORCHESTRATOR] Found workflow mapping: %s -> %s", execution.ID, intelligentWorkflowID)
		// Get files from the intelligent workflow instead
		wo.populateFilesFromStorage(ctx, intelligentWorkflowID, &files)
	}

	// Create workflow record similar to intelligent workflows
	workflowRecord := map[string]interface{}{
		"id":              execution.ID,
		"status":          execution.Status,
		"task_name":       "Hierarchical Workflow",
		"description":     "Data analysis workflow with hierarchical planning",
		"progress":        execution.Progress.Percentage,
		"total_steps":     execution.Progress.TotalSteps,
		"completed_steps": execution.Progress.CompletedSteps,
		"failed_steps":    execution.Progress.FailedSteps,
		"current_step":    execution.Progress.CurrentStep,
		"started_at":      execution.StartedAt,
		"last_activity":   execution.LastActivity,
		"can_resume":      false,
		"can_cancel":      false,
		"error":           execution.Error,
		"files":           files, // Now populated with actual files
	}

	// Store in Redis with 24-hour TTL
	workflowKey := fmt.Sprintf("workflow:%s", execution.ID)
	workflowJSON, _ := json.Marshal(workflowRecord)
	wo.redis.Set(ctx, workflowKey, workflowJSON, 24*time.Hour)

	// Ensure completed workflow is removed from the active set
	activeWorkflowsKey := "active_workflows"
	wo.redis.SRem(ctx, activeWorkflowsKey, execution.ID)
	wo.redis.Expire(ctx, activeWorkflowsKey, 24*time.Hour)

	log.Printf("📊 [ORCHESTRATOR] Stored completed hierarchical workflow in Redis: %s with %d files", execution.ID, len(files))
}

// populateFilesFromStorage populates the files array by querying file storage for the workflow
func (wo *WorkflowOrchestrator) populateFilesFromStorage(ctx context.Context, workflowID string, files *[]interface{}) {
	// Get all file IDs for this workflow
	indexKey := fmt.Sprintf("file:by_workflow:%s", workflowID)
	fileIDs, err := wo.redis.SMembers(ctx, indexKey).Result()
	if err != nil {
		log.Printf("⚠️ [ORCHESTRATOR] Failed to get file IDs for workflow %s: %v", workflowID, err)
		return
	}

	log.Printf("📁 [ORCHESTRATOR] Found %d files for workflow %s", len(fileIDs), workflowID)

	// Get metadata for each file
	for _, fileID := range fileIDs {
		metadataKey := fmt.Sprintf("file:metadata:%s", fileID)
		metadataData, err := wo.redis.Get(ctx, metadataKey).Result()
		if err != nil {
			log.Printf("⚠️ [ORCHESTRATOR] Failed to get metadata for file %s: %v", fileID, err)
			continue
		}

		var metadata map[string]interface{}
		err = json.Unmarshal([]byte(metadataData), &metadata)
		if err != nil {
			log.Printf("⚠️ [ORCHESTRATOR] Failed to unmarshal metadata for file %s: %v", fileID, err)
			continue
		}

		// Get file content
		contentKey := fmt.Sprintf("file:content:%s", fileID)
		content, err := wo.redis.Get(ctx, contentKey).Result()
		if err != nil {
			log.Printf("⚠️ [ORCHESTRATOR] Failed to get content for file %s: %v", fileID, err)
			continue
		}

		// Create file entry for UI
		fileEntry := map[string]interface{}{
			"filename":     metadata["filename"],
			"content_type": metadata["content_type"],
			"size":         metadata["size"],
			"content":      content,
		}

		*files = append(*files, fileEntry)
		log.Printf("📄 [ORCHESTRATOR] Added file %s (%d bytes) to workflow %s", metadata["filename"], len(content), workflowID)
	}
}

// getMappedIntelligentWorkflow retrieves the intelligent workflow ID for a hierarchical workflow ID
func (wo *WorkflowOrchestrator) getMappedIntelligentWorkflow(ctx context.Context, hierarchicalID string) string {
	mappingKey := fmt.Sprintf("workflow_mapping:%s", hierarchicalID)
	intelligentID, err := wo.redis.Get(ctx, mappingKey).Result()
	if err != nil {
		log.Printf("⚠️ [ORCHESTRATOR] No workflow mapping found for %s: %v", hierarchicalID, err)
		return ""
	}
	return intelligentID
}

// addCompletedWorkflowsFromRedis adds completed workflows from Redis to the status list
func (wo *WorkflowOrchestrator) addCompletedWorkflowsFromRedis(statuses *[]*WorkflowStatus) {
	// Use timeout context to prevent hanging
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Get all workflow IDs from active_workflows set
	activeWorkflowsKey := "active_workflows"
	workflowIDs, err := wo.redis.SMembers(ctx, activeWorkflowsKey).Result()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			log.Printf("⏱️ [ORCHESTRATOR] Timeout getting active workflow IDs from Redis")
		} else {
			log.Printf("⚠️ [ORCHESTRATOR] Failed to get active workflow IDs: %v", err)
		}
		return
	}

	log.Printf("📋 [ORCHESTRATOR] Found %d active workflow IDs in Redis", len(workflowIDs))

	// Limit to most recent 50 workflows to prevent slow queries
	maxWorkflows := 50
	if len(workflowIDs) > maxWorkflows {
		log.Printf("⚠️ [ORCHESTRATOR] Limiting to %d most recent workflows (found %d total)", maxWorkflows, len(workflowIDs))
		workflowIDs = workflowIDs[:maxWorkflows]
	}

	// Get workflow details from Redis with timeout per workflow
	for i, workflowID := range workflowIDs {
		// Check if context is cancelled (timeout)
		if ctx.Err() != nil {
			log.Printf("⏱️ [ORCHESTRATOR] Timeout while processing workflows (processed %d/%d)", i, len(workflowIDs))
			break
		}

		workflowKey := fmt.Sprintf("workflow:%s", workflowID)
		workflowData, err := wo.redis.Get(ctx, workflowKey).Result()
		if err != nil {
			if err == redis.Nil {
				// Workflow key doesn't exist, skip it
				continue
			}
			if ctx.Err() == context.DeadlineExceeded {
				log.Printf("⏱️ [ORCHESTRATOR] Timeout getting workflow %s", workflowID)
				break
			}
			log.Printf("⚠️ [ORCHESTRATOR] Failed to get workflow %s: %v", workflowID, err)
			continue
		}

		var workflowRecord map[string]interface{}
		err = json.Unmarshal([]byte(workflowData), &workflowRecord)
		if err != nil {
			log.Printf("⚠️ [ORCHESTRATOR] Failed to unmarshal workflow %s: %v", workflowID, err)
			continue
		}

		// Safely extract fields with type assertions
		id, _ := workflowRecord["id"].(string)
		statusStr, _ := workflowRecord["status"].(string)
		currentStep, _ := workflowRecord["current_step"].(string)
		errorStr, _ := workflowRecord["error"].(string)

		// Convert to WorkflowStatus
		status := &WorkflowStatus{
			ID:          id,
			Status:      statusStr,
			CurrentStep: currentStep,
			Error:       errorStr,
			CanResume:   false,
			CanCancel:   false,
		}

		// Parse timestamps
		if startedAtStr, ok := workflowRecord["started_at"].(string); ok {
			if startedAt, err := time.Parse(time.RFC3339, startedAtStr); err == nil {
				status.StartedAt = startedAt
			}
		}
		if lastActivityStr, ok := workflowRecord["last_activity"].(string); ok {
			if lastActivity, err := time.Parse(time.RFC3339, lastActivityStr); err == nil {
				status.LastActivity = lastActivity
			}
		}

		// Parse progress if available
		if progress, ok := workflowRecord["progress"].(float64); ok {
			totalSteps := 0
			completedSteps := 0
			failedSteps := 0
			if ts, ok := workflowRecord["total_steps"].(float64); ok {
				totalSteps = int(ts)
			}
			if cs, ok := workflowRecord["completed_steps"].(float64); ok {
				completedSteps = int(cs)
			}
			if fs, ok := workflowRecord["failed_steps"].(float64); ok {
				failedSteps = int(fs)
			}
			status.Progress = WorkflowProgress{
				Percentage:     progress,
				TotalSteps:     totalSteps,
				CompletedSteps: completedSteps,
				FailedSteps:    failedSteps,
				CurrentStep:    currentStep,
			}
		}

		*statuses = append(*statuses, status)
	}

	log.Printf("📄 [ORCHESTRATOR] Added %d workflows from Redis to status list", len(*statuses))
}

// updateMetrics updates Redis metrics for workflow execution
func (wo *WorkflowOrchestrator) updateMetrics(execution *WorkflowExecution, success bool) {
	ctx := context.Background()

	// Update total executions
	wo.redis.Incr(ctx, "metrics:total_executions")

	// Update successful executions
	if success {
		wo.redis.Incr(ctx, "metrics:successful_executions")
	}

	// Update last execution time
	wo.redis.Set(ctx, "metrics:last_execution", time.Now().Format(time.RFC3339), 0)

	// Calculate and update average execution time
	executionTime := time.Since(execution.StartedAt).Seconds()

	// Get current average
	currentAvg, err := wo.redis.Get(ctx, "metrics:avg_execution_time").Float64()
	if err != nil {
		currentAvg = 0
	}

	// Get total executions count
	totalExec, err := wo.redis.Get(ctx, "metrics:total_executions").Int()
	if err != nil {
		totalExec = 1
	}

	// Calculate new average
	newAvg := (currentAvg*float64(totalExec-1) + executionTime) / float64(totalExec)
	wo.redis.Set(ctx, "metrics:avg_execution_time", newAvg, 0)

	log.Printf("📊 [ORCHESTRATOR] Updated metrics: success=%v, time=%.2fs, avg=%.2fs", success, executionTime, newAvg)
}

// -----------------------------
// Helper Functions
// -----------------------------

// findStepByID finds a step by its ID
func (wo *WorkflowOrchestrator) findStepByID(steps []WorkflowStep, stepID string) *WorkflowStep {
	for i := range steps {
		if steps[i].ID == stepID {
			return &steps[i]
		}
	}
	return nil
}

// canExecuteStep checks if a step can be executed (dependencies satisfied)
func (wo *WorkflowOrchestrator) canExecuteStep(step *WorkflowStep, execCtx *ExecutionContext) bool {
	log.Printf("🔍 [ORCHESTRATOR] Checking dependencies for step %s: %v", step.ID, step.Dependencies)
	log.Printf("🔍 [ORCHESTRATOR] Available step results: %v", execCtx.StepResults)

	for _, depID := range step.Dependencies {
		if result, exists := execCtx.StepResults[depID]; !exists || result == nil {
			log.Printf("❌ [ORCHESTRATOR] Dependency %s not satisfied for step %s (exists: %v, result: %v)", depID, step.ID, exists, result)
			return false
		}
		log.Printf("✅ [ORCHESTRATOR] Dependency %s satisfied for step %s", depID, step.ID)
	}
	log.Printf("✅ [ORCHESTRATOR] All dependencies satisfied for step %s", step.ID)
	return true
}

// updateStateFromStep updates the execution state based on step results
func (wo *WorkflowOrchestrator) updateStateFromStep(step *WorkflowStep, result interface{}, execCtx *ExecutionContext) {
	// Update state based on postconditions
	for _, postcondition := range step.Postconditions {
		execCtx.State[postcondition] = true
	}

	// Store step result
	execCtx.StepResults[step.ID] = result
}

// evaluateCondition evaluates a condition string
func (wo *WorkflowOrchestrator) evaluateCondition(condition string, execCtx *ExecutionContext) (bool, error) {
	// Simple condition evaluation - can be enhanced with more complex logic
	if condition == "" {
		return true, nil
	}

	// For now, just check if the condition exists in state
	_, exists := execCtx.State[condition]
	return exists, nil
}

// substituteLoopVariables substitutes loop variables in arguments
func (wo *WorkflowOrchestrator) substituteLoopVariables(args map[string]interface{}, variable string, iteration int) map[string]interface{} {
	newArgs := make(map[string]interface{})

	for k, v := range args {
		if str, ok := v.(string); ok && strings.Contains(str, "${"+variable+"}") {
			newArgs[k] = strings.ReplaceAll(str, "${"+variable+"}", fmt.Sprintf("%d", iteration))
		} else {
			newArgs[k] = v
		}
	}

	return newArgs
}

// isCriticalStep checks if a step is critical (workflow fails if this step fails)
func (wo *WorkflowOrchestrator) isCriticalStep(step *WorkflowStep) bool {
	// For now, all steps are critical
	// This can be enhanced with step metadata or configuration
	return true
}

// failWorkflow marks a workflow as failed
func (wo *WorkflowOrchestrator) failWorkflow(execution *WorkflowExecution, errorMsg string) {
	execution.Status = "failed"
	execution.Error = errorMsg
	execution.LastActivity = time.Now()

	// Clear all retry counts for this workflow since it's failed
	wo.clearAllStepRetryCounts(execution.ID)

	// Ensure failed workflow is removed from the active set
	ctx := context.Background()
	activeWorkflowsKey := "active_workflows"
	wo.redis.SRem(ctx, activeWorkflowsKey, execution.ID)
	wo.redis.Expire(ctx, activeWorkflowsKey, 24*time.Hour)

	// Emit workflow failed event
	wo.emitEvent(execution.ID, WorkflowEvent{
		Type:       "workflow_failed",
		WorkflowID: execution.ID,
		Timestamp:  time.Now(),
		Data:       map[string]interface{}{"error": errorMsg},
	})

	log.Printf("❌ [ORCHESTRATOR] Workflow %s failed: %s", execution.ID, errorMsg)
}

// waitForResume waits for a paused workflow to be resumed
func (wo *WorkflowOrchestrator) waitForResume(execution *WorkflowExecution) {
	// Simple polling mechanism - can be enhanced with channels
	for execution.Status == "paused" {
		time.Sleep(100 * time.Millisecond)
	}
}

// waitForWorkflowCompletion waits for a workflow to complete
func (wo *WorkflowOrchestrator) waitForWorkflowCompletion(workflowID string) {
	// Simple polling mechanism - can be enhanced with channels
	for {
		wo.mutex.RLock()
		execution, exists := wo.activeWorkflows[workflowID]
		wo.mutex.RUnlock()

		if !exists {
			break
		}

		if execution.Status == "completed" || execution.Status == "failed" || execution.Status == "cancelled" {
			break
		}

		time.Sleep(100 * time.Millisecond)
	}
}

// getStepRetryCount gets the retry count for a step from Redis
func (wo *WorkflowOrchestrator) getStepRetryCount(workflowID, stepID string) int {
	ctx := context.Background()
	retryKey := fmt.Sprintf("workflow_step_retry:%s:%s", workflowID, stepID)

	count, err := wo.redis.Get(ctx, retryKey).Int()
	if err != nil {
		// If key doesn't exist or error, return 0
		return 0
	}
	return count
}

// setStepRetryCount sets the retry count for a step in Redis
func (wo *WorkflowOrchestrator) setStepRetryCount(workflowID, stepID string, count int) {
	ctx := context.Background()
	retryKey := fmt.Sprintf("workflow_step_retry:%s:%s", workflowID, stepID)

	// Store retry count with 24 hour TTL
	wo.redis.Set(ctx, retryKey, count, 24*time.Hour)
}

// clearStepRetryCount clears the retry count for a step in Redis
func (wo *WorkflowOrchestrator) clearStepRetryCount(workflowID, stepID string) {
	ctx := context.Background()
	retryKey := fmt.Sprintf("workflow_step_retry:%s:%s", workflowID, stepID)
	wo.redis.Del(ctx, retryKey)
}

// clearAllStepRetryCounts clears all retry counts for a workflow in Redis
func (wo *WorkflowOrchestrator) clearAllStepRetryCounts(workflowID string) {
	ctx := context.Background()
	pattern := fmt.Sprintf("workflow_step_retry:%s:*", workflowID)

	// Get all keys matching the pattern
	keys, err := wo.redis.Keys(ctx, pattern).Result()
	if err != nil {
		log.Printf("⚠️ [ORCHESTRATOR] Failed to get retry count keys for workflow %s: %v", workflowID, err)
		return
	}

	// Delete all retry count keys for this workflow
	if len(keys) > 0 {
		wo.redis.Del(ctx, keys...)
		log.Printf("🧹 [ORCHESTRATOR] Cleared %d retry count keys for workflow %s", len(keys), workflowID)
	}
}
