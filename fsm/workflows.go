package main

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"
)

// generateWorkflowsFromHypothesis creates executable workflows from confirmed hypotheses
func (e *FSMEngine) generateWorkflowsFromHypothesis(hypothesis map[string]interface{}, result HypothesisTestResult, domain string) {
	description := hypothesis["description"].(string)
	hypothesisID := hypothesis["id"].(string)

	log.Printf("🔄 Generating workflows from confirmed hypothesis: %s", description)

	// Generate different types of workflows based on the hypothesis
	workflows := e.createWorkflowsForHypothesis(description, hypothesisID, domain, result)

	// Store workflows in Redis
	workflowKey := fmt.Sprintf("fsm:%s:workflows", e.agentID)
	for _, workflow := range workflows {
		workflowData, _ := json.Marshal(workflow)
		e.redis.LPush(e.ctx, workflowKey, workflowData)
	}
	e.redis.LTrim(e.ctx, workflowKey, 0, 199) // Keep last 200 workflows

	// Create workflow execution goals
	e.createWorkflowExecutionGoals(workflows, domain)

	log.Printf("🔄 Generated %d workflows from confirmed hypothesis", len(workflows))
}

// createWorkflowsForHypothesis creates specific workflows based on hypothesis content
func (e *FSMEngine) createWorkflowsForHypothesis(description, hypothesisID, domain string, result HypothesisTestResult) []map[string]interface{} {
	var workflows []map[string]interface{}

	// Create workflow based on hypothesis type and content
	workflowID := fmt.Sprintf("workflow_%s_%d", hypothesisID, time.Now().UnixNano())

	// Determine workflow type based on hypothesis content
	workflowType := e.determineWorkflowType(description, domain)

	// Create the main workflow
	workflow := map[string]interface{}{
		"id":            workflowID,
		"name":          fmt.Sprintf("Execute: %s", description),
		"description":   fmt.Sprintf("Workflow to implement confirmed hypothesis: %s", description),
		"type":          workflowType,
		"domain":        domain,
		"status":        "pending",
		"priority":      7, // High priority for confirmed hypothesis workflows
		"created_at":    time.Now().Format(time.RFC3339),
		"hypothesis_id": hypothesisID,
		"confidence":    result.Confidence,
		"steps":         e.generateWorkflowSteps(description, domain, workflowType),
		"properties": map[string]interface{}{
			"source":         "confirmed_hypothesis",
			"evidence_count": len(result.Evidence),
		},
	}

	workflows = append(workflows, workflow)

	// Create additional specialized workflows based on hypothesis content
	additionalWorkflows := e.createSpecializedWorkflows(description, hypothesisID, domain, result)
	workflows = append(workflows, additionalWorkflows...)

	return workflows
}

// determineWorkflowType determines the appropriate workflow type based on hypothesis content
func (e *FSMEngine) determineWorkflowType(description, domain string) string {
	descLower := strings.ToLower(description)

	// Analyze hypothesis content to determine workflow type
	if strings.Contains(descLower, "optimize") || strings.Contains(descLower, "improve") {
		return "optimization"
	} else if strings.Contains(descLower, "analyze") || strings.Contains(descLower, "investigate") {
		return "analysis"
	} else if strings.Contains(descLower, "implement") || strings.Contains(descLower, "create") {
		return "implementation"
	} else if strings.Contains(descLower, "test") || strings.Contains(descLower, "validate") {
		return "testing"
	} else if strings.Contains(descLower, "learn") || strings.Contains(descLower, "understand") {
		return "learning"
	} else {
		return "general"
	}
}

// generateWorkflowSteps creates the steps for a workflow
func (e *FSMEngine) generateWorkflowSteps(description, domain, workflowType string) []map[string]interface{} {
	var steps []map[string]interface{}

	// Generate steps based on workflow type
	switch workflowType {
	case "optimization":
		steps = []map[string]interface{}{
			{
				"step":        1,
				"action":      "analyze_current_state",
				"description": "Analyze current state related to the hypothesis",
				"tool":        "data_analyzer",
			},
			{
				"step":        2,
				"action":      "identify_optimization_opportunities",
				"description": "Identify specific optimization opportunities",
				"tool":        "optimization_finder",
			},
			{
				"step":        3,
				"action":      "implement_optimization",
				"description": "Implement the optimization",
				"tool":        "optimization_implementer",
			},
			{
				"step":        4,
				"action":      "measure_results",
				"description": "Measure and validate the optimization results",
				"tool":        "performance_measurer",
			},
		}
	case "analysis":
		steps = []map[string]interface{}{
			{
				"step":        1,
				"action":      "gather_data",
				"description": "Gather relevant data for analysis",
				"tool":        "data_collector",
			},
			{
				"step":        2,
				"action":      "perform_analysis",
				"description": "Perform the analysis based on hypothesis",
				"tool":        "data_analyzer",
			},
			{
				"step":        3,
				"action":      "generate_insights",
				"description": "Generate insights from the analysis",
				"tool":        "insight_generator",
			},
			{
				"step":        4,
				"action":      "create_report",
				"description": "Create a comprehensive report",
				"tool":        "report_generator",
			},
		}
	case "implementation":
		steps = []map[string]interface{}{
			{
				"step":        1,
				"action":      "design_solution",
				"description": "Design the solution based on hypothesis",
				"tool":        "solution_designer",
			},
			{
				"step":        2,
				"action":      "implement_solution",
				"description": "Implement the designed solution",
				"tool":        "solution_implementer",
			},
			{
				"step":        3,
				"action":      "test_solution",
				"description": "Test the implemented solution",
				"tool":        "solution_tester",
			},
			{
				"step":        4,
				"action":      "deploy_solution",
				"description": "Deploy the tested solution",
				"tool":        "solution_deployer",
			},
		}
	default: // general
		steps = []map[string]interface{}{
			{
				"step":        1,
				"action":      "plan_execution",
				"description": "Plan the execution of the hypothesis",
				"tool":        "execution_planner",
			},
			{
				"step":        2,
				"action":      "execute_plan",
				"description": "Execute the planned actions",
				"tool":        "plan_executor",
			},
			{
				"step":        3,
				"action":      "evaluate_results",
				"description": "Evaluate the execution results",
				"tool":        "result_evaluator",
			},
		}
	}

	return steps
}

// createSpecializedWorkflows creates additional specialized workflows
func (e *FSMEngine) createSpecializedWorkflows(description, hypothesisID, domain string, result HypothesisTestResult) []map[string]interface{} {
	var workflows []map[string]interface{}

	// Create a monitoring workflow to track the hypothesis implementation
	monitoringWorkflow := map[string]interface{}{
		"id":            fmt.Sprintf("monitor_%s_%d", hypothesisID, time.Now().UnixNano()),
		"name":          fmt.Sprintf("Monitor: %s", description),
		"description":   fmt.Sprintf("Monitor the implementation and effects of: %s", description),
		"type":          "monitoring",
		"domain":        domain,
		"status":        "pending",
		"priority":      5,
		"created_at":    time.Now().Format(time.RFC3339),
		"hypothesis_id": hypothesisID,
		"steps": []map[string]interface{}{
			{
				"step":        1,
				"action":      "setup_monitoring",
				"description": "Set up monitoring for the hypothesis implementation",
				"tool":        "monitoring_setup",
			},
			{
				"step":        2,
				"action":      "track_metrics",
				"description": "Track relevant metrics over time",
				"tool":        "metrics_tracker",
			},
			{
				"step":        3,
				"action":      "analyze_trends",
				"description": "Analyze trends and patterns",
				"tool":        "trend_analyzer",
			},
		},
	}

	workflows = append(workflows, monitoringWorkflow)

	// Create a learning workflow to extract knowledge from the hypothesis
	learningWorkflow := map[string]interface{}{
		"id":            fmt.Sprintf("learn_%s_%d", hypothesisID, time.Now().UnixNano()),
		"name":          fmt.Sprintf("Learn from: %s", description),
		"description":   fmt.Sprintf("Extract and store knowledge from: %s", description),
		"type":          "learning",
		"domain":        domain,
		"status":        "pending",
		"priority":      6,
		"created_at":    time.Now().Format(time.RFC3339),
		"hypothesis_id": hypothesisID,
		"steps": []map[string]interface{}{
			{
				"step":        1,
				"action":      "extract_knowledge",
				"description": "Extract knowledge from the hypothesis implementation",
				"tool":        "knowledge_extractor",
			},
			{
				"step":        2,
				"action":      "update_knowledge_base",
				"description": "Update the knowledge base with new insights",
				"tool":        "knowledge_updater",
			},
			{
				"step":        3,
				"action":      "create_lessons_learned",
				"description": "Create lessons learned document",
				"tool":        "lessons_creator",
			},
		},
	}

	workflows = append(workflows, learningWorkflow)

	return workflows
}

// createWorkflowExecutionGoals creates curiosity goals to execute the workflows
func (e *FSMEngine) createWorkflowExecutionGoals(workflows []map[string]interface{}, domain string) {
	for _, workflow := range workflows {
		workflowID := workflow["id"].(string)
		workflowName := workflow["name"].(string)

		// Create a curiosity goal to execute this workflow
		goal := CuriosityGoal{
			ID:          fmt.Sprintf("workflow_exec_%s", workflowID),
			Type:        "workflow_execution",
			Description: fmt.Sprintf("Execute workflow: %s", workflowName),
			Targets:     []string{workflowID},
			Priority:    7, // High priority for workflow execution
			Status:      "pending",
			Domain:      domain,
			CreatedAt:   time.Now(),
		}

		// Store goal in Redis
		goalKey := fmt.Sprintf("reasoning:curiosity_goals:%s", domain)
		goalData, _ := json.Marshal(goal)
		e.redis.LPush(e.ctx, goalKey, goalData)
		e.redis.LTrim(e.ctx, goalKey, 0, 199) // Keep last 200 goals
	}

	log.Printf("🎯 Created %d workflow execution goals", len(workflows))
}
