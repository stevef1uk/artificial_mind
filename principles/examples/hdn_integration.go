package main

import (
	"fmt"
	"log"
	"principles/internal/client"
)

// Example of how to integrate HDN with the principles system
func mainBasic() {
	// Create a principles client
	principlesClient := client.NewPrinciplesClient("http://localhost:8080")

	// Example HDN action that wants to be executed
	action := "steal"
	params := map[string]interface{}{
		"item":   "gold",
		"target": "vault",
	}
	context := map[string]interface{}{
		"human_harm":  false,
		"human_order": true,
		"self_harm":   false,
		"urgency":     "high",
	}

	// Check if action is allowed
	allowed, reasons, err := principlesClient.IsActionAllowed(action, params, context)
	if err != nil {
		log.Fatalf("Error checking action: %v", err)
	}

	if !allowed {
		fmt.Printf("Action '%s' is NOT allowed. Reasons:\n", action)
		for _, reason := range reasons {
			fmt.Printf("  - %s\n", reason)
		}
		return
	}

	fmt.Printf("Action '%s' is allowed. Executing...\n", action)

	// Define what the action would do
	executor := func(params map[string]interface{}) string {
		return fmt.Sprintf("Successfully executed %s with params: %v", action, params)
	}

	// Execute the action if allowed
	result, reasons, err := principlesClient.ExecuteActionIfAllowed(action, params, context, executor)
	if err != nil {
		log.Fatalf("Error executing action: %v", err)
	}

	if len(reasons) > 0 {
		fmt.Printf("Action was blocked during execution. Reasons:\n")
		for _, reason := range reasons {
			fmt.Printf("  - %s\n", reason)
		}
		return
	}

	fmt.Printf("Action executed successfully: %s\n", result)
}

// ExampleHDNAction represents how HDN might structure an action
type ExampleHDNAction struct {
	TaskName    string
	TaskType    string
	Context     map[string]string
	State       map[string]bool
	Description string
}

// ConvertHDNActionToPrinciples shows how to convert HDN action to principles format
func ConvertHDNActionToPrinciples(hdnAction ExampleHDNAction) (string, map[string]interface{}, map[string]interface{}) {
	// Extract action name
	action := hdnAction.TaskName

	// Convert context to interface{} map
	context := make(map[string]interface{})
	for k, v := range hdnAction.Context {
		context[k] = v
	}

	// Add state information
	for k, v := range hdnAction.State {
		context["state_"+k] = v
	}

	// Add task metadata
	context["task_type"] = hdnAction.TaskType
	context["description"] = hdnAction.Description

	// Create parameters
	params := make(map[string]interface{})
	params["task_name"] = hdnAction.TaskName
	params["task_type"] = hdnAction.TaskType
	params["description"] = hdnAction.Description

	// Add context as parameters
	for k, v := range hdnAction.Context {
		params[k] = v
	}

	return action, params, context
}
