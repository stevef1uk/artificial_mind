package main

import (
	"context"
	"fmt"
	"log"
	"time"
)

// AgentExecutor executes agents using ADK runtime
type AgentExecutor struct {
	registry *AgentRegistry
}

// NewAgentExecutor creates a new agent executor
func NewAgentExecutor(registry *AgentRegistry) *AgentExecutor {
	return &AgentExecutor{
		registry: registry,
	}
}

// ExecuteAgent executes an agent with the given input
// This is a simplified executor that runs agent tasks sequentially
// Full ADK integration will be added later
func (e *AgentExecutor) ExecuteAgent(ctx context.Context, agentID string, input string) (interface{}, error) {
	agentInstance, ok := e.registry.GetAgent(agentID)
	if !ok {
		return nil, fmt.Errorf("agent %s not found", agentID)
	}

	log.Printf("🚀 [AGENT-EXECUTOR] Executing agent: %s with input: %s", agentID, input)
	startTime := time.Now()

	// For now, execute the first task that matches the input
	// TODO: Full ADK integration with LLM-based task selection
	toolCalls := make([]ToolCall, 0)
	var results []interface{}

	// Execute tasks sequentially
	log.Printf("🔍 [AGENT-EXECUTOR] Agent has %d task(s) and %d tool adapter(s)", len(agentInstance.Config.Tasks), len(agentInstance.Tools))
	for i, tool := range agentInstance.Tools {
		log.Printf("🔍 [AGENT-EXECUTOR] Tool adapter %d: %s", i, tool.ToolID)
	}
	
	for _, task := range agentInstance.Config.Tasks {
		log.Printf("📋 [AGENT-EXECUTOR] Executing task: %s (has %d tools)", task.ID, len(task.Tools))
		
		// Merge task parameters with any provided params
		params := make(map[string]interface{})
		for k, v := range task.Parameters {
			params[k] = v
		}
		log.Printf("🔍 [AGENT-EXECUTOR] Task %s parameters: %v", task.ID, params)
		
		// Execute tools for this task
		for _, toolID := range task.Tools {
			log.Printf("🔍 [AGENT-EXECUTOR] Looking for tool: %s", toolID)
			// Find tool adapter
			var adapter *ToolAdapter
			for i := range agentInstance.Tools {
				if agentInstance.Tools[i].ToolID == toolID {
					adapter = &agentInstance.Tools[i]
					log.Printf("✅ [AGENT-EXECUTOR] Found tool adapter for %s", toolID)
					break
				}
			}
			
			if adapter == nil {
				availableTools := make([]string, len(agentInstance.Tools))
				for i, t := range agentInstance.Tools {
					availableTools[i] = t.ToolID
				}
				log.Printf("⚠️ [AGENT-EXECUTOR] Tool %s not found for task %s (available tools: %v)", toolID, task.ID, availableTools)
				continue
			}
			
			// Execute tool
			toolStartTime := time.Now()
			result, err := adapter.Execute(ctx, params)
			duration := time.Since(toolStartTime)
			
			toolCall := ToolCall{
				ToolID:   toolID,
				Params:   params,
				Duration: duration,
			}
			
			if err != nil {
				toolCall.Error = err.Error()
				log.Printf("❌ [AGENT-EXECUTOR] Tool %s failed: %v", toolID, err)
			} else {
				toolCall.Result = result
				log.Printf("✅ [AGENT-EXECUTOR] Tool %s completed in %v", toolID, duration)
				results = append(results, result)
			}
			
			toolCalls = append(toolCalls, toolCall)
		}
	}

	duration := time.Since(startTime)
	log.Printf("✅ [AGENT-EXECUTOR] Agent %s completed in %v", agentID, duration)

	return map[string]interface{}{
		"agent_id":  agentID,
		"input":     input,
		"results":   results,
		"duration":  duration.String(),
		"tool_calls": toolCalls,
	}, nil
}

// ToolCall represents a tool call made by an agent
type ToolCall struct {
	ToolID   string                 `json:"tool_id"`
	Params   map[string]interface{} `json:"params"`
	Result   interface{}            `json:"result,omitempty"`
	Error    string                 `json:"error,omitempty"`
	Duration time.Duration          `json:"duration"`
}

