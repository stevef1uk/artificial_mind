package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"
)

// AgentExecutor executes agents using ADK runtime
type AgentExecutor struct {
	registry *AgentRegistry
	history  *AgentHistory // Optional execution history
}

// NewAgentExecutor creates a new agent executor
func NewAgentExecutor(registry *AgentRegistry) *AgentExecutor {
	return &AgentExecutor{
		registry: registry,
		history:  nil, // Will be set if history is available
	}
}

// SetHistory sets the execution history tracker
func (e *AgentExecutor) SetHistory(history *AgentHistory) {
	e.history = history
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
	
	// Record execution start in history
	var executionID string
	if e.history != nil {
		executionID = fmt.Sprintf("%s-%d", agentID, startTime.UnixNano())
		execution := &AgentExecution{
			ID:        executionID,
			AgentID:   agentID,
			Input:     input,
			Status:    "running",
			StartedAt: startTime,
		}
		if err := e.history.RecordExecution(execution); err != nil {
			log.Printf("⚠️ [AGENT-EXECUTOR] Failed to record execution start: %v", err)
		}
	}

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
		// If task has no tools defined, use agent-level tools
		toolsToExecute := task.Tools
		if len(toolsToExecute) == 0 {
			// Fall back to agent-level tools
			for _, adapter := range agentInstance.Tools {
				toolsToExecute = append(toolsToExecute, adapter.ToolID)
			}
		}
		
		for _, toolID := range toolsToExecute {
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
			
			// Special handling for tool_http_get with websites array
			if toolID == "tool_http_get" {
				if websites, ok := params["websites"].([]interface{}); ok {
					// Execute tool_http_get for each website
					var websiteResults []map[string]interface{}
					for _, website := range websites {
						url, ok := website.(string)
						if !ok {
							log.Printf("⚠️ [AGENT-EXECUTOR] Invalid website URL: %v", website)
							continue
						}
						
						log.Printf("🌐 [AGENT-EXECUTOR] Checking website: %s", url)
						toolStartTime := time.Now()
						websiteParams := map[string]interface{}{"url": url}
						result, err := adapter.Execute(ctx, websiteParams)
						duration := time.Since(toolStartTime)
						
						websiteResult := map[string]interface{}{
							"url":      url,
							"duration": duration.String(),
						}
						
						if err != nil {
							websiteResult["error"] = err.Error()
							websiteResult["status"] = "down"
							websiteResult["status_text"] = "down" // Explicitly set status_text for failed sites
							log.Printf("❌ [AGENT-EXECUTOR] Website %s check failed: %v", url, err)
						} else {
							if resultMap, ok := result.(map[string]interface{}); ok {
								if status, ok := resultMap["status"].(int); ok {
									websiteResult["status"] = status
									if status >= 200 && status < 300 {
										websiteResult["status_text"] = "up"
									} else {
										websiteResult["status_text"] = "warning"
									}
								}
								// Don't include full body in result, just status
								if body, ok := resultMap["body"].(string); ok {
									websiteResult["body_length"] = len(body)
								}
							}
							log.Printf("✅ [AGENT-EXECUTOR] Website %s check completed in %v", url, duration)
						}
						
						websiteResults = append(websiteResults, websiteResult)
						toolCall := ToolCall{
							ToolID:   toolID,
							Params:   websiteParams,
							Result:   websiteResult,
							Duration: duration,
						}
						if err != nil {
							toolCall.Error = err.Error()
						}
						toolCalls = append(toolCalls, toolCall)
					}
					websiteTaskResult := map[string]interface{}{
						"task":     task.ID,
						"websites": websiteResults,
					}
					results = append(results, websiteTaskResult)
					
					// Log website check summary
					log.Printf("✅ [AGENT-EXECUTOR] Website check completed: %d website(s) checked", len(websiteResults))
					for _, siteMap := range websiteResults {
						url, _ := siteMap["url"].(string)
						status, _ := siteMap["status"].(int)
						statusText, _ := siteMap["status_text"].(string)
						log.Printf("   - %s: %s (HTTP %d)", url, statusText, status)
					}
					
					// Automatically send Telegram notification if tool is available
					log.Printf("🔍 [AGENT-EXECUTOR] Checking for Telegram notification capability...")
					if e.registry != nil {
						agentInstance, ok := e.registry.GetAgent(agentID)
						if ok {
							log.Printf("🔍 [AGENT-EXECUTOR] Agent has %d tools registered", len(agentInstance.Tools))
							// Check if agent has telegram tool
							var telegramAdapter *ToolAdapter
							for i := range agentInstance.Tools {
								log.Printf("🔍 [AGENT-EXECUTOR] Checking tool: %s", agentInstance.Tools[i].ToolID)
								if agentInstance.Tools[i].ToolID == "tool_telegram_send" {
									telegramAdapter = &agentInstance.Tools[i]
									log.Printf("✅ [AGENT-EXECUTOR] Found Telegram tool adapter!")
									break
								}
							}
							
							if telegramAdapter != nil {
								log.Printf("📱 [AGENT-EXECUTOR] Telegram adapter found, preparing notification...")
								// Format message
								message := "🌐 *Website Status Report*\n\n"
								allUp := true
								for _, siteMap := range websiteResults {
									url, _ := siteMap["url"].(string)
									statusText := "❌ Down"
									if status, ok := siteMap["status_text"].(string); ok {
										if status == "up" {
											statusText = "✅ Up"
										} else if status == "warning" {
											statusText = "⚠️ Warning"
											allUp = false
										} else {
											allUp = false
										}
									} else {
										allUp = false
									}
									
									statusCode := "N/A"
									if status, ok := siteMap["status"].(int); ok {
										statusCode = fmt.Sprintf("%d", status)
									}
									
									duration, _ := siteMap["duration"].(string)
									message += fmt.Sprintf("%s: %s (HTTP %s) - %s\n", url, statusText, statusCode, duration)
								}
								
								if allUp {
									message += "\n✅ All websites are operational!"
								} else {
									message += "\n⚠️ Some websites have issues - please check!"
								}
								
								// Send notification
								telegramParams := map[string]interface{}{
									"message":    message,
									"chat_id":    os.Getenv("TELEGRAM_CHAT_ID"), // Explicitly set chat_id
									"parse_mode": "Markdown",
								}
								
								log.Printf("📱 [AGENT-EXECUTOR] Sending Telegram notification to chat_id: %s", telegramParams["chat_id"])
								telegramResult, err := telegramAdapter.Execute(ctx, telegramParams)
								if err != nil {
									log.Printf("⚠️ [AGENT-EXECUTOR] Failed to send Telegram notification: %v", err)
								} else {
									log.Printf("✅ [AGENT-EXECUTOR] Telegram notification sent successfully")
									results = append(results, map[string]interface{}{
										"task":   "telegram_notification",
										"result": telegramResult,
									})
								}
							} else {
								log.Printf("⚠️ [AGENT-EXECUTOR] Telegram adapter not found in agent tools")
							}
						} else {
							log.Printf("⚠️ [AGENT-EXECUTOR] Agent instance not found for agent_id: %s", agentID)
						}
					} else {
						log.Printf("⚠️ [AGENT-EXECUTOR] Registry is nil, cannot send Telegram notification")
					}
					
					continue
				}
			}
			
			// Execute tool normally
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

	result := map[string]interface{}{
		"agent_id":  agentID,
		"input":     input,
		"results":   results,
		"duration":  duration.String(),
		"tool_calls": toolCalls,
	}

	// Record successful execution in history
	if e.history != nil && executionID != "" {
		execution := &AgentExecution{
			ID:          executionID,
			AgentID:     agentID,
			Input:       input,
			Status:      "success",
			Result:      result,
			Duration:    duration,
			ToolCalls:   toolCalls,
			StartedAt:   startTime,
		}
		now := time.Now()
		execution.CompletedAt = &now
		if err := e.history.RecordExecution(execution); err != nil {
			log.Printf("⚠️ [AGENT-EXECUTOR] Failed to record execution history: %v", err)
		}
	}

	return result, nil
}


// ToolCall represents a tool call made by an agent
type ToolCall struct {
	ToolID   string                 `json:"tool_id"`
	Params   map[string]interface{} `json:"params"`
	Result   interface{}            `json:"result,omitempty"`
	Error    string                 `json:"error,omitempty"`
	Duration time.Duration          `json:"duration"`
}

