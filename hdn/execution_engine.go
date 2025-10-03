package main

import (
	"fmt"
	"log"
	"time"
)

// --------- Enhanced Execution Engine ---------

type ExecutionEngine struct {
	domain    *EnhancedDomain
	llmClient *LLMClient
	mcpClient *MCPClient
	state     State
}

type ExecutionResult struct {
	Success  bool          `json:"success"`
	Result   interface{}   `json:"result,omitempty"`
	Error    string        `json:"error,omitempty"`
	NewState State         `json:"new_state,omitempty"`
	Duration time.Duration `json:"duration"`
}

func NewExecutionEngine(domain *EnhancedDomain, llmClient *LLMClient, mcpClient *MCPClient) *ExecutionEngine {
	return &ExecutionEngine{
		domain:    domain,
		llmClient: llmClient,
		mcpClient: mcpClient,
		state:     make(State),
	}
}

func (e *ExecutionEngine) ExecuteTask(taskName string, context map[string]string) (*ExecutionResult, error) {
	start := time.Now()
	log.Printf("⚙️ [ENGINE] Executing task: %s", taskName)
	log.Printf("⚙️ [ENGINE] Context: %+v", context)

	// Find the task definition
	taskDef := e.findTaskDefinition(taskName)
	if taskDef == nil {
		log.Printf("❌ [ENGINE] Task definition not found: %s", taskName)
		return &ExecutionResult{
			Success:  false,
			Error:    fmt.Sprintf("Task definition not found: %s", taskName),
			Duration: time.Since(start),
		}, nil
	}
	log.Printf("✅ [ENGINE] Found task definition: %T", taskDef)

	// Execute based on task type
	var result interface{}
	var err error

	// Type assertion to get the actual task type
	switch t := taskDef.(type) {
	case *EnhancedActionDef:
		log.Printf("⚙️ [ENGINE] Executing EnhancedActionDef with type: %s", t.TaskType)
		switch t.TaskType {
		case TaskTypePrimitive:
			log.Printf("⚙️ [ENGINE] Executing primitive task")
			result, err = e.executePrimitiveTask(taskDef, context)
		case TaskTypeLLM:
			log.Printf("⚙️ [ENGINE] Executing LLM task")
			result, err = e.executeLLMTask(taskDef, context)
		case TaskTypeMCP:
			log.Printf("⚙️ [ENGINE] Executing MCP task")
			result, err = e.executeMCPTask(taskDef, context)
		default:
			log.Printf("❌ [ENGINE] Unknown action task type: %s", t.TaskType)
			err = fmt.Errorf("unknown action task type: %s", t.TaskType)
		}
	case *EnhancedMethodDef:
		log.Printf("⚙️ [ENGINE] Executing EnhancedMethodDef with type: %s", t.TaskType)
		switch t.TaskType {
		case TaskTypeMethod:
			log.Printf("⚙️ [ENGINE] Executing method task")
			result, err = e.executeMethodTask(taskDef, context)
		case TaskTypeLLM:
			log.Printf("⚙️ [ENGINE] Executing LLM method task")
			result, err = e.executeLLMTask(taskDef, context)
		case TaskTypeMCP:
			log.Printf("⚙️ [ENGINE] Executing MCP method task")
			result, err = e.executeMCPTask(taskDef, context)
		default:
			log.Printf("❌ [ENGINE] Unknown method task type: %s", t.TaskType)
			err = fmt.Errorf("unknown method task type: %s", t.TaskType)
		}
	default:
		log.Printf("❌ [ENGINE] Unknown task definition type: %T", taskDef)
		err = fmt.Errorf("unknown task definition type: %T", taskDef)
	}

	// Update state
	if err == nil {
		log.Printf("✅ [ENGINE] Task execution successful, updating state")
		e.updateState(taskDef, result)
	} else {
		log.Printf("❌ [ENGINE] Task execution failed: %v", err)
	}

	errorMsg := ""
	if err != nil {
		errorMsg = err.Error()
	}

	duration := time.Since(start)
	log.Printf("⚙️ [ENGINE] Task %s completed in %v. Success: %v", taskName, duration, err == nil)

	return &ExecutionResult{
		Success:  err == nil,
		Result:   result,
		Error:    errorMsg,
		NewState: e.state,
		Duration: duration,
	}, nil
}

func (e *ExecutionEngine) findTaskDefinition(taskName string) interface{} {
	// Check methods first
	for _, method := range e.domain.Methods {
		if method.Task == taskName {
			return &method
		}
	}

	// Check actions
	for _, action := range e.domain.Actions {
		if action.Task == taskName {
			return &action
		}
	}

	return nil
}

func (e *ExecutionEngine) executePrimitiveTask(taskDef interface{}, context map[string]string) (interface{}, error) {
	// For primitive tasks, we simulate execution
	log.Printf("Executing primitive task: %s", taskDef.(*EnhancedActionDef).Task)

	// Simulate work
	time.Sleep(100 * time.Millisecond)

	// Return success
	return map[string]interface{}{
		"task":    taskDef.(*EnhancedActionDef).Task,
		"status":  "completed",
		"message": "Primitive task executed successfully",
		"context": context,
	}, nil
}

func (e *ExecutionEngine) executeLLMTask(taskDef interface{}, context map[string]string) (interface{}, error) {
	if e.llmClient == nil {
		return nil, fmt.Errorf("LLM client not configured")
	}

	method := taskDef.(*EnhancedMethodDef)

	// Use LLM to execute the task
	prompt := method.LLMPrompt
	if prompt == "" {
		prompt = fmt.Sprintf("Execute the task: %s", method.Task)
	}

	result, err := e.llmClient.ExecuteTask(method.Task, prompt, context)
	if err != nil {
		return nil, fmt.Errorf("LLM execution failed: %v", err)
	}

	return map[string]interface{}{
		"task":     method.Task,
		"status":   "completed",
		"result":   result,
		"context":  context,
		"executor": "llm",
	}, nil
}

func (e *ExecutionEngine) executeMCPTask(taskDef interface{}, context map[string]string) (interface{}, error) {
	if e.mcpClient == nil {
		return nil, fmt.Errorf("MCP client not configured")
	}

	method := taskDef.(*EnhancedMethodDef)

	// Execute MCP tools
	var results []interface{}

	for _, subtask := range method.Subtasks {
		// Extract tool name and parameters
		toolName, params := e.extractMCPToolInfo(subtask, context)

		// Execute the tool
		result, err := e.mcpClient.ExecuteTool(toolName, params)
		if err != nil {
			return nil, fmt.Errorf("MCP tool execution failed for %s: %v", toolName, err)
		}

		results = append(results, map[string]interface{}{
			"tool":   toolName,
			"result": result,
		})
	}

	return map[string]interface{}{
		"task":     method.Task,
		"status":   "completed",
		"results":  results,
		"context":  context,
		"executor": "mcp",
	}, nil
}

func (e *ExecutionEngine) executeMethodTask(taskDef interface{}, context map[string]string) (interface{}, error) {
	method := taskDef.(*EnhancedMethodDef)

	// Check preconditions
	ok, missing := checkPreconditions(e.state, method.Preconditions)
	if !ok {
		return nil, fmt.Errorf("preconditions not met: %v", missing)
	}

	// Execute subtasks
	var results []interface{}
	localState := copyState(e.state)

	for _, subtask := range method.Subtasks {
		// Recursively execute subtask
		subResult, err := e.ExecuteTask(subtask, context)
		if err != nil {
			return nil, fmt.Errorf("subtask execution failed: %v", err)
		}

		if !subResult.Success {
			return nil, fmt.Errorf("subtask failed: %s", subResult.Error)
		}

		results = append(results, subResult.Result)

		// Update local state based on subtask effects
		if action := e.findActionDefinition(subtask); action != nil {
			localState = applyEffects(localState, action.Effects)
		}
	}

	// Update global state
	e.state = localState

	return map[string]interface{}{
		"task":     method.Task,
		"status":   "completed",
		"subtasks": results,
		"context":  context,
		"executor": "method",
	}, nil
}

func (e *ExecutionEngine) extractMCPToolInfo(subtask string, context map[string]string) (string, map[string]interface{}) {
	// Simple extraction - in practice, this would be more sophisticated
	// For now, assume subtask names like "MCP_file_read" or "MCP_web_search"

	if len(subtask) > 4 && subtask[:4] == "MCP_" {
		toolName := subtask[4:]

		// Create parameters based on context
		params := make(map[string]interface{})
		for k, v := range context {
			params[k] = v
		}

		return toolName, params
	}

	// Default fallback - convert context to interface{} map
	params := make(map[string]interface{})
	for k, v := range context {
		params[k] = v
	}
	return subtask, params
}

func (e *ExecutionEngine) findActionDefinition(taskName string) *ActionDef {
	for _, action := range e.domain.Actions {
		if action.Task == taskName {
			return &action.ActionDef
		}
	}
	return nil
}

func (e *ExecutionEngine) updateState(taskDef interface{}, result interface{}) {
	// Update state based on task execution
	// This is a simplified version - in practice, you'd have more sophisticated state management

	switch t := taskDef.(type) {
	case *EnhancedActionDef:
		// Apply effects
		e.state = applyEffects(e.state, t.Effects)

		// Mark task as completed
		e.state[t.Task+"_completed"] = true

	case *EnhancedMethodDef:
		// Mark method as completed
		e.state[t.Task+"_completed"] = true
	}
}

func (e *ExecutionEngine) GetState() State {
	return e.state
}

func (e *ExecutionEngine) SetState(state State) {
	e.state = state
}

func (e *ExecutionEngine) ResetState() {
	e.state = make(State)
}

// --------- Enhanced Planning with Task Types ---------

func (e *ExecutionEngine) PlanTask(taskName string, state State) ([]string, error) {
	// Use the existing HTN planner but with enhanced domain
	legacyDomain := e.convertToLegacyDomain()
	plan := HTNPlan(state, taskName, &legacyDomain)

	if plan == nil {
		return nil, fmt.Errorf("failed to create plan for task: %s", taskName)
	}

	return plan, nil
}

func (e *ExecutionEngine) convertToLegacyDomain() Domain {
	legacy := Domain{
		Methods: make([]MethodDef, len(e.domain.Methods)),
		Actions: make([]ActionDef, len(e.domain.Actions)),
	}

	for i, method := range e.domain.Methods {
		legacy.Methods[i] = method.MethodDef
	}

	for i, action := range e.domain.Actions {
		legacy.Actions[i] = action.ActionDef
	}

	return legacy
}

// --------- Learning Integration ---------

func (e *ExecutionEngine) LearnTask(taskName, description string, context map[string]string, useLLM, useMCP bool) (*MethodDef, error) {
	var method *MethodDef
	var err error

	if useLLM && e.llmClient != nil {
		method, err = e.llmClient.GenerateMethod(taskName, description, context)
		if err != nil {
			return nil, fmt.Errorf("LLM learning failed: %v", err)
		}
	} else if useMCP && e.mcpClient != nil {
		method, err = e.mcpClient.GenerateMethod(taskName, description, context)
		if err != nil {
			return nil, fmt.Errorf("MCP learning failed: %v", err)
		}
	} else {
		return nil, fmt.Errorf("no learning method available")
	}

	// Add the learned method to the domain
	enhancedMethod := EnhancedMethodDef{
		MethodDef: *method,
		TaskType: func() TaskType {
			if useLLM {
				return TaskTypeLLM
			} else {
				return TaskTypeMCP
			}
		}(),
		Description: description,
	}
	enhancedMethod.IsLearned = true

	e.domain.Methods = append([]EnhancedMethodDef{enhancedMethod}, e.domain.Methods...)

	return method, nil
}
