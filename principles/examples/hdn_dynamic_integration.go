package main

import (
	"encoding/json"
	"fmt"
	"log"
	"principles/internal/llm"
	"principles/internal/mapper"
)

// MockLLMClient implements the LLMClient interface for testing
type MockLLMClient struct{}

func (m *MockLLMClient) AnalyzeTask(taskName, description string, context map[string]interface{}) (string, error) {
	// Mock LLM analysis - in real implementation, this would call an actual LLM
	analysis := map[string]interface{}{
		"human_harm":          false,
		"self_harm":           false,
		"human_order":         true,
		"stealing":            false,
		"damage":              false,
		"unauthorized_access": false,
		"safety_concerns":     "None identified",
		"recommendations":     "Task appears safe to execute",
	}

	response, _ := json.Marshal(analysis)
	return string(response), nil
}

func (m *MockLLMClient) GenerateTaskPlan(goal string, context map[string]interface{}) ([]mapper.LLMTask, error) {
	// Mock task plan generation
	plan := []mapper.LLMTask{
		{
			TaskName:    "analyze_environment",
			Description: "Scan the surrounding area for obstacles and potential hazards",
			TaskType:    "perception",
			Context:     context,
			GeneratedBy: "llm",
		},
		{
			TaskName:    "navigate_to_target",
			Description: "Move to the specified location while avoiding obstacles",
			TaskType:    "navigation",
			Context:     context,
			GeneratedBy: "llm",
		},
		{
			TaskName:    "retrieve_object",
			Description: "Pick up the requested item from the target location",
			TaskType:    "manipulation",
			Context:     context,
			GeneratedBy: "llm",
		},
	}

	return plan, nil
}

func (m *MockLLMClient) RefineTask(task mapper.LLMTask, feedback []string) (mapper.LLMTask, error) {
	// Mock task refinement - in real implementation, this would use LLM to refine
	refined := task
	refined.Description = "Refined: " + task.Description + " (safety enhanced)"
	refined.Context["refined"] = true
	return refined, nil
}

func mainDynamic() {
	// Create LLM integration
	llmIntegration := llm.NewLLMIntegration("http://localhost:8080")

	// Create mock LLM client
	llmClient := &MockLLMClient{}

	// Example 1: Analyze a single task
	fmt.Println("=== Example 1: Single Task Analysis ===")

	taskName := "move_robot"
	description := "Move the robot to the laboratory to retrieve a sample"
	context := map[string]interface{}{
		"location":    "laboratory",
		"urgency":     "medium",
		"human_order": true,
	}

	analysis, err := llmIntegration.AnalyzeTaskWithLLM(llmClient, taskName, description, context)
	if err != nil {
		log.Fatalf("Analysis failed: %v", err)
	}

	fmt.Printf("Task: %s\n", taskName)
	fmt.Printf("Description: %s\n", description)
	fmt.Printf("Risk Level: %s\n", analysis.RiskLevel)
	fmt.Printf("Confidence: %.2f\n", analysis.Confidence)
	fmt.Printf("Human Harm: %v\n", analysis.EthicalContext["human_harm"])
	fmt.Printf("Warnings: %v\n", analysis.Warnings)
	fmt.Println()

	// Example 2: Generate and validate a task plan
	fmt.Println("=== Example 2: Task Plan Generation ===")

	goal := "Retrieve a sample from the laboratory"
	planContext := map[string]interface{}{
		"target_location": "laboratory",
		"object_type":     "sample",
		"urgency":         "high",
		"human_order":     true,
	}

	plan, err := llmIntegration.GenerateEthicalTaskPlan(llmClient, goal, planContext)
	if err != nil {
		log.Fatalf("Plan generation failed: %v", err)
	}

	fmt.Printf("Goal: %s\n", goal)
	fmt.Printf("Generated %d tasks:\n", len(plan))
	for i, task := range plan {
		fmt.Printf("  %d. %s: %s\n", i+1, task.TaskName, task.Description)
	}
	fmt.Println()

	// Example 3: Handle a potentially harmful task
	fmt.Println("=== Example 3: Harmful Task Handling ===")

	harmfulTask := mapper.LLMTask{
		TaskName:    "steal_sample",
		Description: "Take the sample from the laboratory without permission",
		TaskType:    "manipulation",
		Context: map[string]interface{}{
			"target_location": "laboratory",
			"object_type":     "sample",
			"authorization":   false,
		},
		GeneratedBy: "llm",
	}

	// Create dynamic mapper for direct checking
	dynamicMapper := mapper.NewDynamicActionMapper("http://localhost:8080")
	result := dynamicMapper.CheckTask(harmfulTask)

	fmt.Printf("Task: %s\n", harmfulTask.TaskName)
	fmt.Printf("Description: %s\n", harmfulTask.Description)
	fmt.Printf("Allowed: %v\n", result.Allowed)
	if !result.Allowed {
		fmt.Printf("Reasons: %v\n", result.Reasons)

		// Get recommendations
		recommendations := dynamicMapper.GetTaskRecommendations(*result)
		fmt.Printf("Recommendations: %v\n", recommendations)
	}
	fmt.Println()

	// Example 4: Batch task validation
	fmt.Println("=== Example 4: Batch Task Validation ===")

	tasks := []mapper.LLMTask{
		{
			TaskName:    "scan_area",
			Description: "Use sensors to scan the area for obstacles",
			TaskType:    "perception",
			GeneratedBy: "llm",
		},
		{
			TaskName:    "move_safely",
			Description: "Navigate to target while avoiding obstacles",
			TaskType:    "navigation",
			GeneratedBy: "llm",
		},
		{
			TaskName:    "access_restricted",
			Description: "Break into the secure laboratory",
			TaskType:    "access",
			GeneratedBy: "llm",
		},
	}

	allAllowed, blockedTasks, results := dynamicMapper.ValidateTaskPlan(tasks)

	fmt.Printf("All tasks allowed: %v\n", allAllowed)
	if !allAllowed {
		fmt.Printf("Blocked tasks: %v\n", blockedTasks)
	}

	fmt.Println("Detailed results:")
	for _, result := range results {
		fmt.Printf("  %s: %v", result.TaskName, result.Allowed)
		if !result.Allowed {
			fmt.Printf(" (reasons: %v)", result.Reasons)
		}
		fmt.Println()
	}
}

// ExampleHDNIntegration shows how HDN would integrate this
func ExampleHDNIntegration() {
	// This is how HDN would integrate the principles system

	// 1. When HDN learns a new task from LLM
	llmIntegration := llm.NewLLMIntegration("http://localhost:8080")
	llmClient := &MockLLMClient{} // In real HDN, this would be the actual LLM client

	// 2. Generate task plan
	goal := "Complete the assigned mission"
	context := map[string]interface{}{
		"mission_type": "retrieval",
		"target":       "laboratory",
		"human_order":  true,
	}

	plan, err := llmIntegration.GenerateEthicalTaskPlan(llmClient, goal, context)
	if err != nil {
		log.Printf("Failed to generate ethical plan: %v", err)
		return
	}

	// 3. Execute only allowed tasks
	dynamicMapper := mapper.NewDynamicActionMapper("http://localhost:8080")

	for _, task := range plan {
		result := dynamicMapper.CheckTask(task)

		if result.Allowed {
			fmt.Printf("Executing: %s\n", task.TaskName)
			// Execute the task in HDN
		} else {
			fmt.Printf("Blocked: %s - %v\n", task.TaskName, result.Reasons)
			// Try to refine or find alternative
		}
	}
}
