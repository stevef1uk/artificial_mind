// FILE: hierarchical_planner.go
package planner

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// -----------------------------
// Hierarchical Planning Types
// -----------------------------

// WorkflowStep represents a step in a hierarchical workflow
type WorkflowStep struct {
	ID             string                 `json:"id"`
	StepType       string                 `json:"step_type"` // "capability", "subgoal", "condition", "loop"
	CapabilityID   string                 `json:"capability_id,omitempty"`
	SubGoal        *Goal                  `json:"sub_goal,omitempty"`
	Args           map[string]interface{} `json:"args"`
	Preconditions  []string               `json:"preconditions"`
	Postconditions []string               `json:"postconditions"`
	EstimatedCost  float64                `json:"estimated_cost"`
	Confidence     float64                `json:"confidence"`
	Timeout        int                    `json:"timeout"`
	RetryCount     int                    `json:"retry_count"`
	MaxRetries     int                    `json:"max_retries"`
	Status         string                 `json:"status"` // "pending", "running", "completed", "failed", "skipped"
	Result         interface{}            `json:"result,omitempty"`
	Error          string                 `json:"error,omitempty"`
	StartedAt      *time.Time             `json:"started_at,omitempty"`
	CompletedAt    *time.Time             `json:"completed_at,omitempty"`
	Dependencies   []string               `json:"dependencies"` // IDs of steps this depends on
	Children       []string               `json:"children"`     // IDs of child steps
	Parent         string                 `json:"parent,omitempty"`
	Parallel       bool                   `json:"parallel"`            // Can run in parallel with siblings
	Condition      string                 `json:"condition,omitempty"` // Condition for execution
	LoopConfig     *LoopConfig            `json:"loop_config,omitempty"`
}

// LoopConfig represents configuration for loop steps
type LoopConfig struct {
	MaxIterations int           `json:"max_iterations"`
	Condition     string        `json:"condition"`
	Variable      string        `json:"variable"`
	Values        []interface{} `json:"values"`
	StepTemplate  *WorkflowStep `json:"step_template"`
}

// HierarchicalPlan represents a multi-step hierarchical plan
type HierarchicalPlan struct {
	ID               string         `json:"id"`
	Goal             Goal           `json:"goal"`
	Steps            []WorkflowStep `json:"steps"`
	RootSteps        []string       `json:"root_steps"` // Steps with no dependencies
	ExecutionOrder   []string       `json:"execution_order"`
	EstimatedUtility float64        `json:"estimated_utility"`
	PrinciplesRisk   float64        `json:"principles_risk"`
	Score            float64        `json:"score"`
	TotalCost        float64        `json:"total_cost"`
	MaxDepth         int            `json:"max_depth"`
	CreatedAt        time.Time      `json:"created_at"`
	UpdatedAt        time.Time      `json:"updated_at"`
}

// ExecutionContext represents the context during plan execution
type ExecutionContext struct {
	PlanID      string                 `json:"plan_id"`
	CurrentStep string                 `json:"current_step"`
	State       map[string]interface{} `json:"state"`
	Variables   map[string]interface{} `json:"variables"`
	Results     map[string]interface{} `json:"results"`
	StepResults map[string]interface{} `json:"step_results"`
	Error       string                 `json:"error,omitempty"`
	StartedAt   time.Time              `json:"started_at"`
	LastUpdated time.Time              `json:"last_updated"`
}

// WorkflowTemplate represents a reusable workflow template
type WorkflowTemplate struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Steps       []WorkflowStep `json:"steps"`
	Parameters  []string       `json:"parameters"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

// -----------------------------
// Hierarchical Planner
// -----------------------------

type HierarchicalPlanner struct {
	ctx           context.Context
	redis         *redis.Client
	evalCfg       EvaluatorConfig
	principlesURL string
	executor      Executor
	templates     map[string]*WorkflowTemplate
}

func NewHierarchicalPlanner(ctx context.Context, r *redis.Client, exec Executor, principlesURL string) *HierarchicalPlanner {
	return &HierarchicalPlanner{
		ctx:           ctx,
		redis:         r,
		evalCfg:       EvaluatorConfig{WUtil: 4, WCost: 1, WRisk: 10, WConf: 2},
		principlesURL: principlesURL,
		executor:      exec,
		templates:     make(map[string]*WorkflowTemplate),
	}
}

// -----------------------------
// Hierarchical Plan Generation
// -----------------------------

// GenerateHierarchicalPlan creates a multi-step hierarchical plan for a complex goal
func (hp *HierarchicalPlanner) GenerateHierarchicalPlan(ctx context.Context, goal Goal) (*HierarchicalPlan, error) {
	log.Printf("🧠 [HIERARCHICAL] Generating hierarchical plan for goal: %s", goal.Type)

	plan := &HierarchicalPlan{
		ID:               uuid.New().String(),
		Goal:             goal,
		Steps:            []WorkflowStep{},
		RootSteps:        []string{},
		ExecutionOrder:   []string{},
		EstimatedUtility: 0.8,
		PrinciplesRisk:   0.0,
		Score:            0.0,
		TotalCost:        0.0,
		MaxDepth:         0,
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
	}

	// Step 1: Decompose the goal into sub-goals and capabilities
	steps, err := hp.decomposeGoal(ctx, goal, 0, "")
	if err != nil {
		return nil, fmt.Errorf("goal decomposition failed: %v", err)
	}

	plan.Steps = steps
	plan.MaxDepth = hp.calculateMaxDepth(steps)

	// Step 2: Build dependency graph and execution order
	log.Printf("🔍 [HIERARCHICAL] Building execution order for %d steps", len(steps))
	plan.RootSteps = hp.findRootSteps(steps)
	plan.ExecutionOrder = hp.buildExecutionOrder(steps)
	log.Printf("🔍 [HIERARCHICAL] Generated execution order: %v", plan.ExecutionOrder)

	// Step 3: Calculate costs and scores
	plan.TotalCost = hp.calculateTotalCost(steps)
	plan.Score = hp.scoreHierarchicalPlan(plan)

	log.Printf("✅ [HIERARCHICAL] Generated plan with %d steps, depth %d", len(plan.Steps), plan.MaxDepth)
	return plan, nil
}

// decomposeGoal recursively decomposes a goal into workflow steps
func (hp *HierarchicalPlanner) decomposeGoal(ctx context.Context, goal Goal, depth int, parentID string) ([]WorkflowStep, error) {
	var steps []WorkflowStep

	// Check if we have a workflow template for this goal type
	// Look for templates that match the goal type (e.g., "data_analysis" matches "data_analysis_template")
	templateKey := goal.Type + "_template"
	if template, exists := hp.templates[templateKey]; exists {
		log.Printf("🔧 [HIERARCHICAL] Using template for goal: %s (template: %s)", goal.Type, templateKey)
		return hp.instantiateTemplate(template, goal, depth, parentID)
	}

	// Also check for exact match
	if template, exists := hp.templates[goal.Type]; exists {
		log.Printf("🔧 [HIERARCHICAL] Using template for goal: %s (exact match)", goal.Type)
		return hp.instantiateTemplate(template, goal, depth, parentID)
	}

	// Find matching capabilities
	capabilities, err := hp.FindMatchingCapabilities(ctx, goal)
	if err != nil {
		return nil, err
	}

	if len(capabilities) == 0 {
		// No direct capabilities, try to decompose further
		return hp.decomposeComplexGoal(ctx, goal, depth, parentID)
	}

	// Create steps for each capability
	for i, cap := range capabilities {
		stepID := fmt.Sprintf("step_%s_%d", goal.ID, i)

		step := WorkflowStep{
			ID:             stepID,
			StepType:       "capability",
			CapabilityID:   cap.ID,
			Args:           goal.Params,
			Preconditions:  cap.Preconds,
			Postconditions: hp.extractPostconditions(cap.Effects),
			EstimatedCost:  1.0,
			Confidence:     cap.Score,
			Timeout:        30,
			RetryCount:     0,
			MaxRetries:     3,
			Status:         "pending",
			Dependencies:   []string{},
			Children:       []string{},
			Parent:         parentID,
			Parallel:       false,
		}

		// Add postconditions as preconditions for dependent steps
		if i > 0 {
			step.Dependencies = append(step.Dependencies, steps[i-1].ID)
		}

		steps = append(steps, step)
	}

	return steps, nil
}

// decomposeComplexGoal handles complex goals that need further decomposition
func (hp *HierarchicalPlanner) decomposeComplexGoal(ctx context.Context, goal Goal, depth int, parentID string) ([]WorkflowStep, error) {
	log.Printf("🔍 [HIERARCHICAL] Decomposing complex goal: %s", goal.Type)

	// Use LLM or rule-based decomposition
	subGoals, err := hp.decomposeWithLLM(ctx, goal)
	if err != nil {
		return nil, fmt.Errorf("LLM decomposition failed: %v", err)
	}

	var steps []WorkflowStep

	// Create sub-goal steps
	for i, subGoal := range subGoals {
		stepID := fmt.Sprintf("subgoal_%s_%d", goal.ID, i)

		// Check if this sub-goal is already decomposed to prevent infinite recursion
		var step WorkflowStep
		if strings.HasPrefix(subGoal.Type, "prepare_") || strings.HasPrefix(subGoal.Type, "execute_") || strings.HasPrefix(subGoal.Type, "finalize_") {
			// Extract original task name by removing the prefix
			originalTaskName := subGoal.Type
			if strings.HasPrefix(subGoal.Type, "prepare_") {
				originalTaskName = strings.TrimPrefix(subGoal.Type, "prepare_")
			} else if strings.HasPrefix(subGoal.Type, "execute_") {
				originalTaskName = strings.TrimPrefix(subGoal.Type, "execute_")
			} else if strings.HasPrefix(subGoal.Type, "finalize_") {
				originalTaskName = strings.TrimPrefix(subGoal.Type, "finalize_")
			}

			// Treat as a capability step instead of subgoal to prevent recursion
			// Use the original task name as the capability ID for proper cache lookup
			step = WorkflowStep{
				ID:             stepID,
				StepType:       "capability",
				CapabilityID:   originalTaskName, // Use original task name for cache lookup
				Args:           subGoal.Params,
				Preconditions:  []string{},
				Postconditions: []string{subGoal.Type + "_completed"},
				EstimatedCost:  1.0,
				Confidence:     0.7,
				Timeout:        60,
				RetryCount:     0,
				MaxRetries:     2,
				Status:         "pending",
				Dependencies:   []string{},
				Children:       []string{},
				Parent:         parentID,
				Parallel:       false,
			}
			log.Printf("⚠️ [HIERARCHICAL] Converting decomposed goal %s to capability step (original: %s) to prevent recursion", subGoal.Type, originalTaskName)
		} else {
			// Regular subgoal step
			step = WorkflowStep{
				ID:             stepID,
				StepType:       "subgoal",
				SubGoal:        &subGoal,
				Args:           subGoal.Params,
				Preconditions:  []string{},
				Postconditions: []string{subGoal.Type + "_completed"},
				EstimatedCost:  1.0,
				Confidence:     0.7,
				Timeout:        60,
				RetryCount:     0,
				MaxRetries:     2,
				Status:         "pending",
				Dependencies:   []string{},
				Children:       []string{},
				Parent:         parentID,
				Parallel:       false,
			}
		}

		// Add dependencies between sub-goals
		if i > 0 {
			step.Dependencies = append(step.Dependencies, steps[i-1].ID)
		}

		steps = append(steps, step)
	}

	return steps, nil
}

// decomposeWithLLM uses LLM to decompose complex goals
func (hp *HierarchicalPlanner) decomposeWithLLM(ctx context.Context, goal Goal) ([]Goal, error) {
	return hp.decomposeWithLLMWithPath(ctx, goal, make(map[string]bool))
}

// decomposeWithLLMWithPath uses LLM to decompose complex goals with cycle detection
func (hp *HierarchicalPlanner) decomposeWithLLMWithPath(ctx context.Context, goal Goal, path map[string]bool) ([]Goal, error) {
	// Check for cycles
	if path[goal.Type] {
		log.Printf("❌ [HIERARCHICAL] Cycle detected! Goal %s is already in the decomposition path: %+v", goal.Type, path)
		return nil, fmt.Errorf("cycle detected in goal decomposition: %s", goal.Type)
	}

	// Add current goal to path
	path[goal.Type] = true
	defer func() {
		// Remove current goal from path when returning
		delete(path, goal.Type)
	}()

	// This would integrate with the LLM client to decompose goals
	// For now, return a simple decomposition based on goal type

	switch goal.Type {
	case "data_analysis":
		return []Goal{
			{ID: uuid.New().String(), Type: "data_loading", Params: goal.Params},
			{ID: uuid.New().String(), Type: "data_cleaning", Params: goal.Params},
			{ID: uuid.New().String(), Type: "data_analysis", Params: goal.Params},
			{ID: uuid.New().String(), Type: "report_generation", Params: goal.Params},
		}, nil
	case "web_scraping":
		return []Goal{
			{ID: uuid.New().String(), Type: "url_validation", Params: goal.Params},
			{ID: uuid.New().String(), Type: "content_extraction", Params: goal.Params},
			{ID: uuid.New().String(), Type: "data_processing", Params: goal.Params},
			{ID: uuid.New().String(), Type: "data_storage", Params: goal.Params},
		}, nil
	default:
		// Check if this is already a decomposed goal to prevent infinite recursion
		if strings.HasPrefix(goal.Type, "prepare_") || strings.HasPrefix(goal.Type, "execute_") || strings.HasPrefix(goal.Type, "finalize_") {
			log.Printf("⚠️ [HIERARCHICAL] Goal %s appears to be already decomposed, treating as primitive", goal.Type)
			return []Goal{goal}, nil
		}

		// Generic decomposition
		return []Goal{
			{ID: uuid.New().String(), Type: "prepare_" + goal.Type, Params: goal.Params},
			{ID: uuid.New().String(), Type: "execute_" + goal.Type, Params: goal.Params},
			{ID: uuid.New().String(), Type: "finalize_" + goal.Type, Params: goal.Params},
		}, nil
	}
}

// instantiateTemplate creates steps from a workflow template
func (hp *HierarchicalPlanner) instantiateTemplate(template *WorkflowTemplate, goal Goal, depth int, parentID string) ([]WorkflowStep, error) {
	var steps []WorkflowStep

	// First pass: create all steps with new IDs
	for i, templateStep := range template.Steps {
		stepID := fmt.Sprintf("template_%s_%d", goal.ID, i)

		step := WorkflowStep{
			ID:             stepID,
			StepType:       templateStep.StepType,
			CapabilityID:   templateStep.CapabilityID,
			SubGoal:        templateStep.SubGoal,
			Args:           hp.substituteParameters(templateStep.Args, goal.Params),
			Preconditions:  templateStep.Preconditions,
			Postconditions: templateStep.Postconditions,
			EstimatedCost:  templateStep.EstimatedCost,
			Confidence:     templateStep.Confidence,
			Timeout:        templateStep.Timeout,
			RetryCount:     0,
			MaxRetries:     templateStep.MaxRetries,
			Status:         "pending",
			Dependencies:   []string{}, // Will be updated in second pass
			Children:       []string{},
			Parent:         parentID,
			Parallel:       templateStep.Parallel,
			Condition:      templateStep.Condition,
			LoopConfig:     templateStep.LoopConfig,
		}

		steps = append(steps, step)
	}

	// Second pass: update dependencies to reference new step IDs
	for i, templateStep := range template.Steps {
		var newDependencies []string
		for _, depID := range templateStep.Dependencies {
			// Find the corresponding new step ID for this dependency
			for j, templateDepStep := range template.Steps {
				if templateDepStep.ID == depID {
					newDepID := fmt.Sprintf("template_%s_%d", goal.ID, j)
					newDependencies = append(newDependencies, newDepID)
					break
				}
			}
		}
		steps[i].Dependencies = newDependencies
	}

	return steps, nil
}

// substituteParameters replaces template parameters with actual values
func (hp *HierarchicalPlanner) substituteParameters(templateArgs map[string]interface{}, goalParams map[string]interface{}) map[string]interface{} {
	args := make(map[string]interface{})

	for k, v := range templateArgs {
		if str, ok := v.(string); ok && strings.HasPrefix(str, "${") && strings.HasSuffix(str, "}") {
			// Template parameter
			paramName := strings.TrimPrefix(strings.TrimSuffix(str, "}"), "${")
			if val, exists := goalParams[paramName]; exists {
				args[k] = val
			} else {
				args[k] = v // Keep original if not found
			}
		} else {
			args[k] = v
		}
	}

	return args
}

// -----------------------------
// Plan Analysis and Optimization
// -----------------------------

// findRootSteps finds steps with no dependencies
func (hp *HierarchicalPlanner) findRootSteps(steps []WorkflowStep) []string {
	var rootSteps []string
	stepMap := make(map[string]WorkflowStep)

	for _, step := range steps {
		stepMap[step.ID] = step
	}

	for _, step := range steps {
		if len(step.Dependencies) == 0 {
			rootSteps = append(rootSteps, step.ID)
		}
	}

	return rootSteps
}

// buildExecutionOrder creates the execution order considering dependencies
func (hp *HierarchicalPlanner) buildExecutionOrder(steps []WorkflowStep) []string {
	var executionOrder []string
	stepMap := make(map[string]WorkflowStep)
	visited := make(map[string]bool)

	log.Printf("🔍 [HIERARCHICAL] Building execution order for %d steps", len(steps))
	for i, step := range steps {
		stepMap[step.ID] = step
		log.Printf("🔍 [HIERARCHICAL] Step %d: %s (deps: %v)", i, step.ID, step.Dependencies)
	}

	// Topological sort - visit ALL steps, not just root steps
	var visit func(string)
	visit = func(stepID string) {
		if visited[stepID] {
			return
		}
		visited[stepID] = true

		step := stepMap[stepID]
		for _, depID := range step.Dependencies {
			visit(depID)
		}

		executionOrder = append(executionOrder, stepID)
	}

	// Visit all steps, not just root steps
	// This ensures we get all steps in the execution order
	for _, step := range steps {
		visit(step.ID)
	}

	log.Printf("🔍 [HIERARCHICAL] Final execution order: %v", executionOrder)
	return executionOrder
}

// calculateMaxDepth calculates the maximum depth of the plan
func (hp *HierarchicalPlanner) calculateMaxDepth(steps []WorkflowStep) int {
	maxDepth := 0
	stepMap := make(map[string]WorkflowStep)

	for _, step := range steps {
		stepMap[step.ID] = step
	}

	var calculateDepth func(string, int) int
	calculateDepth = func(stepID string, currentDepth int) int {
		step := stepMap[stepID]
		depth := currentDepth

		for _, childID := range step.Children {
			childDepth := calculateDepth(childID, currentDepth+1)
			if childDepth > depth {
				depth = childDepth
			}
		}

		return depth
	}

	for _, step := range steps {
		if len(step.Dependencies) == 0 {
			depth := calculateDepth(step.ID, 0)
			if depth > maxDepth {
				maxDepth = depth
			}
		}
	}

	return maxDepth
}

// calculateTotalCost calculates the total estimated cost of the plan
func (hp *HierarchicalPlanner) calculateTotalCost(steps []WorkflowStep) float64 {
	totalCost := 0.0
	for _, step := range steps {
		totalCost += step.EstimatedCost
	}
	return totalCost
}

// scoreHierarchicalPlan scores a hierarchical plan
func (hp *HierarchicalPlanner) scoreHierarchicalPlan(plan *HierarchicalPlan) float64 {
	util := plan.EstimatedUtility
	cost := plan.TotalCost
	risk := plan.PrinciplesRisk

	// Calculate average confidence
	conf := 0.0
	for _, step := range plan.Steps {
		conf += step.Confidence
	}
	if len(plan.Steps) > 0 {
		conf = conf / float64(len(plan.Steps))
	}

	// Depth penalty (deeper plans are more complex)
	depthPenalty := float64(plan.MaxDepth) * 0.1

	// Step count penalty (more steps = more complexity)
	stepPenalty := float64(len(plan.Steps)) * 0.05

	score := hp.evalCfg.WUtil*util - hp.evalCfg.WCost*cost - hp.evalCfg.WRisk*risk + hp.evalCfg.WConf*conf - depthPenalty - stepPenalty
	return score
}

// -----------------------------
// Plan Execution
// -----------------------------

// ExecuteHierarchicalPlan executes a hierarchical plan step by step
func (hp *HierarchicalPlanner) ExecuteHierarchicalPlan(ctx context.Context, plan *HierarchicalPlan, userRequest string) (*ExecutionContext, error) {
	log.Printf("🚀 [HIERARCHICAL] Starting execution of plan: %s", plan.ID)

	execCtx := &ExecutionContext{
		PlanID:      plan.ID,
		CurrentStep: "",
		State:       make(map[string]interface{}),
		Variables:   make(map[string]interface{}),
		Results:     make(map[string]interface{}),
		StepResults: make(map[string]interface{}),
		StartedAt:   time.Now(),
		LastUpdated: time.Now(),
	}

	// Execute steps in order
	for _, stepID := range plan.ExecutionOrder {
		step := hp.findStepByID(plan.Steps, stepID)
		if step == nil {
			return execCtx, fmt.Errorf("step not found: %s", stepID)
		}

		execCtx.CurrentStep = stepID
		execCtx.LastUpdated = time.Now()

		// Check if step can be executed (dependencies satisfied)
		if !hp.canExecuteStep(step, execCtx) {
			log.Printf("⏳ [HIERARCHICAL] Step %s waiting for dependencies", stepID)
			continue
		}

		// Execute the step
		result, err := hp.executeStep(ctx, step, execCtx)
		if err != nil {
			log.Printf("❌ [HIERARCHICAL] Step %s failed: %v", stepID, err)
			execCtx.Error = err.Error()

			// Handle retries
			if step.RetryCount < step.MaxRetries {
				step.RetryCount++
				log.Printf("🔄 [HIERARCHICAL] Retrying step %s (attempt %d/%d)", stepID, step.RetryCount, step.MaxRetries)
				continue
			}

			return execCtx, fmt.Errorf("step %s failed after %d retries: %v", stepID, step.MaxRetries, err)
		}

		// Update execution context
		execCtx.StepResults[stepID] = result
		execCtx.Results[stepID] = result

		// Update state based on step effects
		hp.updateStateFromStep(step, result, execCtx)

		log.Printf("✅ [HIERARCHICAL] Step %s completed successfully", stepID)
	}

	log.Printf("🎉 [HIERARCHICAL] Plan execution completed successfully")
	return execCtx, nil
}

// executeStep executes a single workflow step
func (hp *HierarchicalPlanner) executeStep(ctx context.Context, step *WorkflowStep, execCtx *ExecutionContext) (interface{}, error) {
	log.Printf("🔄 [HIERARCHICAL] Executing step: %s (type: %s)", step.ID, step.StepType)

	now := time.Now()
	step.StartedAt = &now
	step.Status = "running"

	switch step.StepType {
	case "capability":
		return hp.executeCapabilityStep(ctx, step, execCtx)
	case "subgoal":
		return hp.executeSubGoalStep(ctx, step, execCtx)
	case "condition":
		return hp.executeConditionStep(ctx, step, execCtx)
	case "loop":
		return hp.executeLoopStep(ctx, step, execCtx)
	default:
		return nil, fmt.Errorf("unknown step type: %s", step.StepType)
	}
}

// executeCapabilityStep executes a capability step
func (hp *HierarchicalPlanner) executeCapabilityStep(ctx context.Context, step *WorkflowStep, execCtx *ExecutionContext) (interface{}, error) {
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

	// Execute using the base planner's executor
	result, err := hp.executor.ExecutePlan(ctx, capabilityPlan, "")
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
func (hp *HierarchicalPlanner) executeSubGoalStep(ctx context.Context, step *WorkflowStep, execCtx *ExecutionContext) (interface{}, error) {
	if step.SubGoal == nil {
		return nil, fmt.Errorf("sub-goal step has no sub-goal")
	}

	// Generate a hierarchical plan for the sub-goal
	subPlan, err := hp.GenerateHierarchicalPlan(ctx, *step.SubGoal)
	if err != nil {
		return nil, fmt.Errorf("failed to generate sub-plan: %v", err)
	}

	// Execute the sub-plan
	subExecCtx, err := hp.ExecuteHierarchicalPlan(ctx, subPlan, fmt.Sprintf("Sub-goal: %s", step.SubGoal.Type))
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
func (hp *HierarchicalPlanner) executeConditionStep(ctx context.Context, step *WorkflowStep, execCtx *ExecutionContext) (interface{}, error) {
	// Evaluate the condition
	conditionMet, err := hp.evaluateCondition(step.Condition, execCtx)
	if err != nil {
		return nil, fmt.Errorf("condition evaluation failed: %v", err)
	}

	if conditionMet {
		step.Status = "completed"
		step.Result = "condition_met"
	} else {
		step.Status = "skipped"
		step.Result = "condition_not_met"
	}

	now := time.Now()
	step.CompletedAt = &now

	return step.Result, nil
}

// executeLoopStep executes a loop step
func (hp *HierarchicalPlanner) executeLoopStep(ctx context.Context, step *WorkflowStep, execCtx *ExecutionContext) (interface{}, error) {
	if step.LoopConfig == nil {
		return nil, fmt.Errorf("loop step has no loop configuration")
	}

	var results []interface{}
	iteration := 0

	for iteration < step.LoopConfig.MaxIterations {
		// Check loop condition
		conditionMet, err := hp.evaluateCondition(step.LoopConfig.Condition, execCtx)
		if err != nil {
			return nil, fmt.Errorf("loop condition evaluation failed: %v", err)
		}

		if !conditionMet {
			break
		}

		// Execute loop step template
		loopStep := *step.LoopConfig.StepTemplate
		loopStep.ID = fmt.Sprintf("%s_iteration_%d", step.ID, iteration)
		loopStep.Args = hp.substituteLoopVariables(loopStep.Args, step.LoopConfig.Variable, iteration)

		result, err := hp.executeStep(ctx, &loopStep, execCtx)
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
// Helper Functions
// -----------------------------

// findStepByID finds a step by its ID
func (hp *HierarchicalPlanner) findStepByID(steps []WorkflowStep, stepID string) *WorkflowStep {
	for i := range steps {
		if steps[i].ID == stepID {
			return &steps[i]
		}
	}
	return nil
}

// canExecuteStep checks if a step can be executed (dependencies satisfied)
func (hp *HierarchicalPlanner) canExecuteStep(step *WorkflowStep, execCtx *ExecutionContext) bool {
	for _, depID := range step.Dependencies {
		if result, exists := execCtx.StepResults[depID]; !exists || result == nil {
			return false
		}
	}
	return true
}

// updateStateFromStep updates the execution state based on step results
func (hp *HierarchicalPlanner) updateStateFromStep(step *WorkflowStep, result interface{}, execCtx *ExecutionContext) {
	// Update state based on postconditions
	for _, postcondition := range step.Postconditions {
		execCtx.State[postcondition] = true
	}

	// Store step result
	execCtx.StepResults[step.ID] = result
}

// evaluateCondition evaluates a condition string
func (hp *HierarchicalPlanner) evaluateCondition(condition string, execCtx *ExecutionContext) (bool, error) {
	// Simple condition evaluation - can be enhanced with more complex logic
	if condition == "" {
		return true, nil
	}

	// For now, just check if the condition exists in state
	_, exists := execCtx.State[condition]
	return exists, nil
}

// substituteLoopVariables substitutes loop variables in arguments
func (hp *HierarchicalPlanner) substituteLoopVariables(args map[string]interface{}, variable string, iteration int) map[string]interface{} {
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

// extractPostconditions extracts postconditions from effects
func (hp *HierarchicalPlanner) extractPostconditions(effects map[string]interface{}) []string {
	var postconditions []string
	for effect := range effects {
		postconditions = append(postconditions, effect)
	}
	return postconditions
}

// -----------------------------
// Workflow Template Management
// -----------------------------

// RegisterWorkflowTemplate registers a workflow template
func (hp *HierarchicalPlanner) RegisterWorkflowTemplate(template *WorkflowTemplate) error {
	hp.templates[template.ID] = template
	log.Printf("✅ [HIERARCHICAL] Registered workflow template: %s", template.Name)
	return nil
}

// GetWorkflowTemplate retrieves a workflow template
func (hp *HierarchicalPlanner) GetWorkflowTemplate(id string) (*WorkflowTemplate, error) {
	template, exists := hp.templates[id]
	if !exists {
		return nil, fmt.Errorf("template not found: %s", id)
	}
	return template, nil
}

// ListWorkflowTemplates lists all workflow templates
func (hp *HierarchicalPlanner) ListWorkflowTemplates() []*WorkflowTemplate {
	var templates []*WorkflowTemplate
	for _, template := range hp.templates {
		templates = append(templates, template)
	}
	return templates
}

// -----------------------------
// Integration with Base Planner
// -----------------------------

// FindMatchingCapabilities delegates to the base planner
func (hp *HierarchicalPlanner) FindMatchingCapabilities(ctx context.Context, goal Goal) ([]Capability, error) {
	basePlanner := &Planner{
		ctx:           hp.ctx,
		redis:         hp.redis,
		evalCfg:       hp.evalCfg,
		principlesURL: hp.principlesURL,
		executor:      hp.executor,
	}
	return basePlanner.FindMatchingCapabilities(ctx, goal)
}

// CheckPlanAgainstPrinciples delegates to the base planner
func (hp *HierarchicalPlanner) CheckPlanAgainstPrinciples(ctx context.Context, plan HierarchicalPlan) (bool, string, error) {
	// Convert hierarchical plan to simple plan for principles check
	simplePlan := Plan{
		ID:               plan.ID,
		Goal:             plan.Goal,
		Steps:            []PlanStep{},
		EstimatedUtility: plan.EstimatedUtility,
		PrinciplesRisk:   plan.PrinciplesRisk,
		Score:            plan.Score,
	}

	// Add steps from hierarchical plan
	for _, step := range plan.Steps {
		if step.StepType == "capability" {
			simplePlan.Steps = append(simplePlan.Steps, PlanStep{
				CapabilityID:  step.CapabilityID,
				Args:          step.Args,
				EstimatedCost: step.EstimatedCost,
				Confidence:    step.Confidence,
			})
		}
	}

	basePlanner := &Planner{
		ctx:           hp.ctx,
		redis:         hp.redis,
		evalCfg:       hp.evalCfg,
		principlesURL: hp.principlesURL,
		executor:      hp.executor,
	}

	return basePlanner.CheckPlanAgainstPrinciples(ctx, simplePlan)
}
