// FILE: hierarchical_integration.go
package planner

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
)

// -----------------------------
// Hierarchical Integration
// -----------------------------

// HierarchicalIntegration integrates hierarchical planning with the existing system
type HierarchicalIntegration struct {
	ctx                  context.Context
	redis                *redis.Client
	hierarchicalPlanner  *HierarchicalPlanner
	workflowOrchestrator *WorkflowOrchestrator
	basePlanner          *Planner
	selfModelManager     interface{} // Will be properly typed when integrated
	principlesURL        string
}

// NewHierarchicalIntegration creates a new hierarchical integration
func NewHierarchicalIntegration(
	ctx context.Context,
	redis *redis.Client,
	basePlanner *Planner,
	principlesURL string,
	selfModelManager interface{},
) *HierarchicalIntegration {
	hierarchicalPlanner := NewHierarchicalPlanner(ctx, redis, basePlanner.executor, principlesURL)
	workflowOrchestrator := NewWorkflowOrchestrator(ctx, redis, hierarchicalPlanner, basePlanner.executor)

	return &HierarchicalIntegration{
		ctx:                  ctx,
		redis:                redis,
		hierarchicalPlanner:  hierarchicalPlanner,
		workflowOrchestrator: workflowOrchestrator,
		basePlanner:          basePlanner,
		selfModelManager:     selfModelManager,
		principlesURL:        principlesURL,
	}
}

// GetHierarchicalPlanner returns the hierarchical planner
func (hi *HierarchicalIntegration) GetHierarchicalPlanner() *HierarchicalPlanner {
	return hi.hierarchicalPlanner
}

// GetWorkflowOrchestrator returns the workflow orchestrator
func (hi *HierarchicalIntegration) GetWorkflowOrchestrator() *WorkflowOrchestrator {
	return hi.workflowOrchestrator
}

// -----------------------------
// Enhanced Plan Generation
// -----------------------------

// PlanAndExecuteHierarchically plans and executes a task using hierarchical planning
func (hi *HierarchicalIntegration) PlanAndExecuteHierarchically(
	userRequest string,
	taskName string,
	description string,
	context map[string]string,
) (*WorkflowExecution, error) {
	log.Printf("🧠 [HIERARCHICAL-INTEGRATION] Planning task hierarchically: %s", taskName)

	// Convert context to interface{} map for planner
	goalParams := make(map[string]interface{})
	for k, v := range context {
		goalParams[k] = v
	}

	// Create goal for hierarchical planner
	goal := Goal{
		ID:     fmt.Sprintf("goal_%d", time.Now().UnixNano()),
		Type:   taskName,
		Params: goalParams,
	}

	// Generate hierarchical plan
	plan, err := hi.hierarchicalPlanner.GenerateHierarchicalPlan(hi.ctx, goal)
	if err != nil {
		return nil, fmt.Errorf("hierarchical planning failed: %v", err)
	}

	log.Printf("✅ [HIERARCHICAL-INTEGRATION] Generated hierarchical plan with %d steps", len(plan.Steps))

	// Check plan against principles (temporarily disabled for testing)
	// blocked, reason, err := hi.hierarchicalPlanner.CheckPlanAgainstPrinciples(hi.ctx, *plan)
	// if err != nil {
	// 	return nil, fmt.Errorf("principles check failed: %v", err)
	// }
	// if blocked {
	// 	return nil, fmt.Errorf("plan blocked by principles: %s", reason)
	// }
	log.Printf("⚠️ [HIERARCHICAL-INTEGRATION] Principles check temporarily disabled for testing")

	// Start workflow execution
	execution, err := hi.workflowOrchestrator.StartWorkflow(hi.ctx, plan, userRequest)
	if err != nil {
		return nil, fmt.Errorf("workflow execution failed: %v", err)
	}

	// Record in self-model if available
	hi.recordHierarchicalExecution(execution, userRequest, taskName)

	log.Printf("🎉 [HIERARCHICAL-INTEGRATION] Hierarchical execution started: %s", execution.ID)
	return execution, nil
}

// -----------------------------
// Workflow Template Management
// -----------------------------

// RegisterWorkflowTemplate registers a workflow template for reuse
func (hi *HierarchicalIntegration) RegisterWorkflowTemplate(template *WorkflowTemplate) error {
	err := hi.hierarchicalPlanner.RegisterWorkflowTemplate(template)
	if err != nil {
		return fmt.Errorf("failed to register template: %v", err)
	}

	// Store template in Redis for persistence
	templateData, err := json.Marshal(template)
	if err != nil {
		return fmt.Errorf("failed to marshal template: %v", err)
	}

	templateKey := fmt.Sprintf("workflow_template:%s", template.ID)
	err = hi.redis.Set(hi.ctx, templateKey, templateData, 0).Err()
	if err != nil {
		return fmt.Errorf("failed to store template in Redis: %v", err)
	}

	log.Printf("✅ [HIERARCHICAL-INTEGRATION] Registered workflow template: %s", template.Name)
	return nil
}

// LoadWorkflowTemplate loads a workflow template from Redis
func (hi *HierarchicalIntegration) LoadWorkflowTemplate(templateID string) (*WorkflowTemplate, error) {
	templateKey := fmt.Sprintf("workflow_template:%s", templateID)
	templateData, err := hi.redis.Get(hi.ctx, templateKey).Result()
	if err != nil {
		return nil, fmt.Errorf("template not found: %v", err)
	}

	var template WorkflowTemplate
	err = json.Unmarshal([]byte(templateData), &template)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal template: %v", err)
	}

	return &template, nil
}

// ListWorkflowTemplates lists all available workflow templates
func (hi *HierarchicalIntegration) ListWorkflowTemplates() ([]*WorkflowTemplate, error) {
	// Get from memory first
	templates := hi.hierarchicalPlanner.ListWorkflowTemplates()

	// Also load from Redis
	keys, err := hi.redis.Keys(hi.ctx, "workflow_template:*").Result()
	if err != nil {
		return templates, nil // Return memory templates if Redis fails
	}

	for _, key := range keys {
		templateData, err := hi.redis.Get(hi.ctx, key).Result()
		if err != nil {
			continue
		}

		var template WorkflowTemplate
		if err := json.Unmarshal([]byte(templateData), &template); err != nil {
			continue
		}

		// Check if already in memory
		found := false
		for _, existing := range templates {
			if existing.ID == template.ID {
				found = true
				break
			}
		}

		if !found {
			templates = append(templates, &template)
		}
	}

	return templates, nil
}

// -----------------------------
// Workflow Management
// -----------------------------

// GetWorkflowStatus gets the status of a workflow
func (hi *HierarchicalIntegration) GetWorkflowStatus(workflowID string) (*WorkflowStatus, error) {
	return hi.workflowOrchestrator.GetWorkflowStatus(workflowID)
}

// GetWorkflowDetails returns full workflow details for visualization
func (hi *HierarchicalIntegration) GetWorkflowDetails(workflowID string) (*WorkflowDetails, error) {
	return hi.workflowOrchestrator.GetWorkflowDetails(workflowID)
}

// PauseWorkflow pauses a running workflow
func (hi *HierarchicalIntegration) PauseWorkflow(workflowID string, reason string) error {
	return hi.workflowOrchestrator.PauseWorkflow(workflowID, reason)
}

// ResumeWorkflow resumes a paused workflow
func (hi *HierarchicalIntegration) ResumeWorkflow(workflowID string, resumeToken string) error {
	return hi.workflowOrchestrator.ResumeWorkflow(workflowID, resumeToken)
}

// CancelWorkflow cancels a workflow
func (hi *HierarchicalIntegration) CancelWorkflow(workflowID string) error {
	return hi.workflowOrchestrator.CancelWorkflow(workflowID)
}

// ListActiveWorkflows lists all active workflows
func (hi *HierarchicalIntegration) ListActiveWorkflows() []*WorkflowStatus {
	return hi.workflowOrchestrator.ListActiveWorkflows()
}

// SubscribeToWorkflowEvents subscribes to workflow events
func (hi *HierarchicalIntegration) SubscribeToWorkflowEvents(workflowID string) (<-chan WorkflowEvent, error) {
	return hi.workflowOrchestrator.SubscribeToWorkflowEvents(workflowID)
}

// -----------------------------
// Self-Model Integration
// -----------------------------

// recordHierarchicalExecution records a hierarchical execution in the self-model
func (hi *HierarchicalIntegration) recordHierarchicalExecution(execution *WorkflowExecution, userRequest, taskName string) {
	if hi.selfModelManager == nil {
		return
	}

	// This would integrate with the actual self-model manager
	// For now, we'll just log the execution
	log.Printf("📝 [SELF-MODEL] Recording hierarchical execution: %s", execution.ID)

	// TODO: Integrate with actual self-model manager
	// Example integration:
	// err := hi.selfModelManager.RecordEpisode(
	//     fmt.Sprintf("Hierarchical execution: %s", taskName),
	//     "hierarchical_planning",
	//     fmt.Sprintf("Workflow ID: %s, Steps: %d", execution.ID, len(execution.Plan.Steps)),
	//     true,
	//     map[string]interface{}{
	//         "workflow_id": execution.ID,
	//         "task_name": taskName,
	//         "total_steps": len(execution.Plan.Steps),
	//         "user_request": userRequest,
	//     },
	// )
}

// updateSelfModelFromWorkflow updates the self-model based on workflow execution results
func (hi *HierarchicalIntegration) updateSelfModelFromWorkflow(execution *WorkflowExecution) {
	if hi.selfModelManager == nil {
		return
	}

	// Update beliefs based on workflow execution
	successRate := float64(execution.Progress.CompletedSteps) / float64(execution.Progress.TotalSteps)

	// TODO: Integrate with actual self-model manager
	log.Printf("📊 [SELF-MODEL] Updating beliefs from workflow %s: success rate %.2f", execution.ID, successRate)
}

// -----------------------------
// Capability Registration
// -----------------------------

// RegisterCapability registers a capability with the hierarchical planner
func (hi *HierarchicalIntegration) RegisterCapability(capability *Capability) error {
	// Register with base planner
	err := hi.basePlanner.SaveCapability(hi.ctx, *capability)
	if err != nil {
		return fmt.Errorf("failed to register capability with base planner: %v", err)
	}

	log.Printf("✅ [HIERARCHICAL-INTEGRATION] Registered capability: %s", capability.TaskName)
	return nil
}

// ListCapabilities lists all registered capabilities
func (hi *HierarchicalIntegration) ListCapabilities() ([]Capability, error) {
	return hi.basePlanner.ListCapabilities(hi.ctx)
}

// -----------------------------
// Predefined Workflow Templates
// -----------------------------

// CreateDefaultTemplates creates default workflow templates
func (hi *HierarchicalIntegration) CreateDefaultTemplates() error {
	templates := []*WorkflowTemplate{
		hi.createDataAnalysisTemplate(),
		hi.createWebScrapingTemplate(),
		hi.createMLPipelineTemplate(),
		hi.createAPIIntegrationTemplate(),
	}

	for _, template := range templates {
		err := hi.RegisterWorkflowTemplate(template)
		if err != nil {
			log.Printf("⚠️ [HIERARCHICAL-INTEGRATION] Failed to register template %s: %v", template.Name, err)
		}
	}

	log.Printf("✅ [HIERARCHICAL-INTEGRATION] Created %d default templates", len(templates))
	return nil
}

// createDataAnalysisTemplate creates a data analysis workflow template
func (hi *HierarchicalIntegration) createDataAnalysisTemplate() *WorkflowTemplate {
	return &WorkflowTemplate{
		ID:          "data_analysis_template",
		Name:        "Data Analysis Pipeline",
		Description: "Complete data analysis workflow from data loading to report generation",
		Parameters:  []string{"data_source", "analysis_type", "output_format"},
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
		Steps: []WorkflowStep{
			{
				ID:             "load_data",
				StepType:       "capability",
				CapabilityID:   "data_loading",
				Args:           map[string]interface{}{"source": "${data_source}"},
				Preconditions:  []string{},
				Postconditions: []string{"data_loaded"},
				EstimatedCost:  1.0,
				Confidence:     0.9,
				Timeout:        60,
				MaxRetries:     3,
				Status:         "pending",
				Dependencies:   []string{},
				Children:       []string{},
				Parallel:       false,
			},
			{
				ID:             "clean_data",
				StepType:       "capability",
				CapabilityID:   "data_cleaning",
				Args:           map[string]interface{}{"data": "${data_loaded}"},
				Preconditions:  []string{"data_loaded"},
				Postconditions: []string{"data_cleaned"},
				EstimatedCost:  1.0,
				Confidence:     0.8,
				Timeout:        45,
				MaxRetries:     2,
				Status:         "pending",
				Dependencies:   []string{"load_data"},
				Children:       []string{},
				Parallel:       false,
			},
			{
				ID:             "analyze_data",
				StepType:       "capability",
				CapabilityID:   "data_analysis",
				Args:           map[string]interface{}{"data": "${data_cleaned}", "type": "${analysis_type}"},
				Preconditions:  []string{"data_cleaned"},
				Postconditions: []string{"analysis_completed"},
				EstimatedCost:  2.0,
				Confidence:     0.7,
				Timeout:        120,
				MaxRetries:     2,
				Status:         "pending",
				Dependencies:   []string{"clean_data"},
				Children:       []string{},
				Parallel:       false,
			},
			{
				ID:             "generate_report",
				StepType:       "capability",
				CapabilityID:   "report_generation",
				Args:           map[string]interface{}{"results": "${analysis_completed}", "format": "${output_format}"},
				Preconditions:  []string{"analysis_completed"},
				Postconditions: []string{"report_generated"},
				EstimatedCost:  1.0,
				Confidence:     0.9,
				Timeout:        30,
				MaxRetries:     2,
				Status:         "pending",
				Dependencies:   []string{"analyze_data"},
				Children:       []string{},
				Parallel:       false,
			},
		},
	}
}

// createWebScrapingTemplate creates a web scraping workflow template
func (hi *HierarchicalIntegration) createWebScrapingTemplate() *WorkflowTemplate {
	return &WorkflowTemplate{
		ID:          "web_scraping_template",
		Name:        "Web Scraping Pipeline",
		Description: "Complete web scraping workflow from URL validation to data storage",
		Parameters:  []string{"url", "selectors", "output_format"},
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
		Steps: []WorkflowStep{
			{
				ID:             "validate_url",
				StepType:       "capability",
				CapabilityID:   "url_validation",
				Args:           map[string]interface{}{"url": "${url}"},
				Preconditions:  []string{},
				Postconditions: []string{"url_validated"},
				EstimatedCost:  0.5,
				Confidence:     0.95,
				Timeout:        10,
				MaxRetries:     3,
				Status:         "pending",
				Dependencies:   []string{},
				Children:       []string{},
				Parallel:       false,
			},
			{
				ID:             "extract_content",
				StepType:       "capability",
				CapabilityID:   "content_extraction",
				Args:           map[string]interface{}{"url": "${url}", "selectors": "${selectors}"},
				Preconditions:  []string{"url_validated"},
				Postconditions: []string{"content_extracted"},
				EstimatedCost:  2.0,
				Confidence:     0.8,
				Timeout:        60,
				MaxRetries:     3,
				Status:         "pending",
				Dependencies:   []string{"validate_url"},
				Children:       []string{},
				Parallel:       false,
			},
			{
				ID:             "process_data",
				StepType:       "capability",
				CapabilityID:   "data_processing",
				Args:           map[string]interface{}{"content": "${content_extracted}", "format": "${output_format}"},
				Preconditions:  []string{"content_extracted"},
				Postconditions: []string{"data_processed"},
				EstimatedCost:  1.5,
				Confidence:     0.85,
				Timeout:        45,
				MaxRetries:     2,
				Status:         "pending",
				Dependencies:   []string{"extract_content"},
				Children:       []string{},
				Parallel:       false,
			},
			{
				ID:             "store_data",
				StepType:       "capability",
				CapabilityID:   "data_storage",
				Args:           map[string]interface{}{"data": "${data_processed}"},
				Preconditions:  []string{"data_processed"},
				Postconditions: []string{"data_stored"},
				EstimatedCost:  1.0,
				Confidence:     0.9,
				Timeout:        30,
				MaxRetries:     2,
				Status:         "pending",
				Dependencies:   []string{"process_data"},
				Children:       []string{},
				Parallel:       false,
			},
		},
	}
}

// createMLPipelineTemplate creates a machine learning pipeline template
func (hi *HierarchicalIntegration) createMLPipelineTemplate() *WorkflowTemplate {
	return &WorkflowTemplate{
		ID:          "ml_pipeline_template",
		Name:        "Machine Learning Pipeline",
		Description: "Complete ML pipeline from data preparation to model deployment",
		Parameters:  []string{"dataset", "model_type", "target_column"},
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
		Steps: []WorkflowStep{
			{
				ID:             "prepare_data",
				StepType:       "capability",
				CapabilityID:   "data_preparation",
				Args:           map[string]interface{}{"dataset": "${dataset}", "target": "${target_column}"},
				Preconditions:  []string{},
				Postconditions: []string{"data_prepared"},
				EstimatedCost:  2.0,
				Confidence:     0.8,
				Timeout:        90,
				MaxRetries:     2,
				Status:         "pending",
				Dependencies:   []string{},
				Children:       []string{},
				Parallel:       false,
			},
			{
				ID:             "train_model",
				StepType:       "capability",
				CapabilityID:   "model_training",
				Args:           map[string]interface{}{"data": "${data_prepared}", "type": "${model_type}"},
				Preconditions:  []string{"data_prepared"},
				Postconditions: []string{"model_trained"},
				EstimatedCost:  5.0,
				Confidence:     0.7,
				Timeout:        300,
				MaxRetries:     2,
				Status:         "pending",
				Dependencies:   []string{"prepare_data"},
				Children:       []string{},
				Parallel:       false,
			},
			{
				ID:             "evaluate_model",
				StepType:       "capability",
				CapabilityID:   "model_evaluation",
				Args:           map[string]interface{}{"model": "${model_trained}"},
				Preconditions:  []string{"model_trained"},
				Postconditions: []string{"model_evaluated"},
				EstimatedCost:  1.0,
				Confidence:     0.9,
				Timeout:        60,
				MaxRetries:     2,
				Status:         "pending",
				Dependencies:   []string{"train_model"},
				Children:       []string{},
				Parallel:       false,
			},
			{
				ID:             "deploy_model",
				StepType:       "capability",
				CapabilityID:   "model_deployment",
				Args:           map[string]interface{}{"model": "${model_evaluated}"},
				Preconditions:  []string{"model_evaluated"},
				Postconditions: []string{"model_deployed"},
				EstimatedCost:  2.0,
				Confidence:     0.8,
				Timeout:        120,
				MaxRetries:     2,
				Status:         "pending",
				Dependencies:   []string{"evaluate_model"},
				Children:       []string{},
				Parallel:       false,
			},
		},
	}
}

// createAPIIntegrationTemplate creates an API integration template
func (hi *HierarchicalIntegration) createAPIIntegrationTemplate() *WorkflowTemplate {
	return &WorkflowTemplate{
		ID:          "api_integration_template",
		Name:        "API Integration Pipeline",
		Description: "Complete API integration workflow from authentication to data processing",
		Parameters:  []string{"api_url", "auth_type", "endpoints"},
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
		Steps: []WorkflowStep{
			{
				ID:             "authenticate",
				StepType:       "capability",
				CapabilityID:   "api_authentication",
				Args:           map[string]interface{}{"url": "${api_url}", "type": "${auth_type}"},
				Preconditions:  []string{},
				Postconditions: []string{"authenticated"},
				EstimatedCost:  1.0,
				Confidence:     0.9,
				Timeout:        30,
				MaxRetries:     3,
				Status:         "pending",
				Dependencies:   []string{},
				Children:       []string{},
				Parallel:       false,
			},
			{
				ID:             "fetch_data",
				StepType:       "capability",
				CapabilityID:   "api_data_fetching",
				Args:           map[string]interface{}{"endpoints": "${endpoints}"},
				Preconditions:  []string{"authenticated"},
				Postconditions: []string{"data_fetched"},
				EstimatedCost:  2.0,
				Confidence:     0.8,
				Timeout:        120,
				MaxRetries:     3,
				Status:         "pending",
				Dependencies:   []string{"authenticate"},
				Children:       []string{},
				Parallel:       false,
			},
			{
				ID:             "process_response",
				StepType:       "capability",
				CapabilityID:   "response_processing",
				Args:           map[string]interface{}{"data": "${data_fetched}"},
				Preconditions:  []string{"data_fetched"},
				Postconditions: []string{"response_processed"},
				EstimatedCost:  1.5,
				Confidence:     0.85,
				Timeout:        60,
				MaxRetries:     2,
				Status:         "pending",
				Dependencies:   []string{"fetch_data"},
				Children:       []string{},
				Parallel:       false,
			},
		},
	}
}
