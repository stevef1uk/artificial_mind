package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// AgentExecutor executes agents using ADK runtime
type AgentExecutor struct {
	registry  *AgentRegistry
	history   *AgentHistory // Optional execution history
	llmClient *LLMClient    // LLM client for intelligent execution
}

// NewAgentExecutor creates a new agent executor
func NewAgentExecutor(registry *AgentRegistry) *AgentExecutor {
	return &AgentExecutor{
		registry:  registry,
		history:   nil, // Will be set if history is available
		llmClient: nil,
	}
}

// SetLLMClient sets the LLM client for intelligent selection
func (e *AgentExecutor) SetLLMClient(llm *LLMClient) {
	e.llmClient = llm
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

	// If we have an LLM client and no tasks are defined, or the input is complex,
	// use the LLM to plan the execution.
	toolCalls := make([]ToolCall, 0)
	var results []interface{}

	if e.llmClient != nil && (len(agentInstance.Config.Tasks) == 0 || len(input) > 20) {
		log.Printf("🤖 [AGENT-EXECUTOR] Using LLM to plan execution for agent %s", agentID)

		// 1. Prepare tool descriptions for the LLM
		var toolsInfo []string
		for _, tool := range agentInstance.Tools {
			toolsInfo = append(toolsInfo, fmt.Sprintf("- %s: %s", tool.ToolID, tool.ToolID)) // We don't have descriptions here, but ToolID is usually descriptive
		}

		systemPrompt := fmt.Sprintf(`You are an autonomous AI agent: %s.
Role: %s
Goal: %s
Backstory: %s
Instructions:
%s

You have access to these tools:
%s

CRITICAL: Decide which tool(s) to call to achieve the user's goal.
Output a JSON array of tool calls. Each tool call must have exactly: 
{
  "tool_id": "string",
  "params": { "key": "value" }
}

Example: 
[
  {
    "tool_id": "smart_scrape", 
    "params": {
      "url": "https://example.com", 
      "goal": "Find rates",
      "extractions": {
        "product_name": "regex_pattern_here (MUST BE REGEX, NOT XPATH)",
        "price": "regex_pattern_here"
      }
    }
  }
]

Rules:
1. Output ONLY the JSON array. NO conversational text before or after.
2. For 'smart_scrape', specific 'extractions' MUST use valid Go/RE2 Regex. Do NOT use XPath or CSS selectors.
3. Ensure it is VALID JSON. NO comments (// or /*) allowed.
4. NO triple quotes (""") inside JSON strings.
5. INTERNAL QUOTES: If you need to put a double quote inside a JSON string value (like in a regex), you MUST escape it with a backslash so it becomes \" (Example: "pattern": "class=\"value\"").
6. Escape all newlines in code strings with \n.
7. NO trailing commas in objects or arrays.
8. Every value in "params" must have a key.
9. TOOLS ARE INDEPENDENT. One tool CANNOT use a variable (like 'smart_scrape_result', 'html_content', or 'results[0]') from a previous tool. Each tool call is a fresh execution with NO shared state.
10. If you need to extract data, use 'smart_scrape' by itself. Do NOT try to use 'execute_code' to process 'smart_scrape' results; it will fail.
11. 'execute_code' runs in a VACUUM. It only has access to standard libraries. It cannot 'see' other tools or their results.`,
			agentInstance.Config.Name,
			agentInstance.Config.Role,
			agentInstance.Config.Goal,
			agentInstance.Config.Backstory,
			strings.Join(agentInstance.Config.Instructions, "\n"),
			strings.Join(toolsInfo, "\n"))

		userPrompt := fmt.Sprintf("User Input: %s", input)

		// Call LLM
		response, err := e.llmClient.callLLMWithContextAndPriority(ctx, systemPrompt+"\n\n"+userPrompt, PriorityHigh)
		if err != nil {
			log.Printf("❌ [AGENT-EXECUTOR] LLM planning failed: %v", err)
		} else {
			log.Printf("🤖 [AGENT-EXECUTOR] LLM response: %s", response)

			// Parse JSON tool calls
			var plannedCalls []struct {
				ToolID string                 `json:"tool_id"`
				Params map[string]interface{} `json:"params"`
			}

			// Sanitize response (strip markdown fences and extract JSON array)
			cleanResponse := sanitizeCode(response)
			if idx := strings.Index(cleanResponse, "["); idx != -1 {
				if lastIdx := strings.LastIndex(cleanResponse, "]"); lastIdx != -1 {
					cleanResponse = cleanResponse[idx : lastIdx+1]
				}
			}

			if err := json.Unmarshal([]byte(cleanResponse), &plannedCalls); err == nil {
				log.Printf("🤖 [AGENT-EXECUTOR] LLM planned %d tool call(s)", len(plannedCalls))

				for _, pc := range plannedCalls {
					// Execute tool
					log.Printf("🔧 [AGENT-EXECUTOR] Executing planned tool: %s with params: %v", pc.ToolID, pc.Params)

					// Find tool adapter
					var adapter *ToolAdapter
					for i := range agentInstance.Tools {
						if agentInstance.Tools[i].ToolID == pc.ToolID {
							adapter = &agentInstance.Tools[i]
							break
						}
					}

					if adapter != nil {
						toolStartTime := time.Now()
						result, err := adapter.Execute(ctx, pc.Params)
						duration := time.Since(toolStartTime)

						toolCall := ToolCall{
							ToolID:   pc.ToolID,
							Params:   pc.Params,
							Result:   result,
							Duration: duration,
						}
						if err != nil {
							toolCall.Error = err.Error()
							log.Printf("❌ [AGENT-EXECUTOR] Tool %s failed: %v", pc.ToolID, err)
						} else {
							log.Printf("✅ [AGENT-EXECUTOR] Tool %s completed in %v", pc.ToolID, duration)
							results = append(results, result)
						}
						toolCalls = append(toolCalls, toolCall)
					} else {
						log.Printf("⚠️ [AGENT-EXECUTOR] Planned tool %s not found in agent configuration", pc.ToolID)
					}
				}

				// If we successfully executed planned calls, we're done here
				if len(toolCalls) > 0 {
					goto record_history
				}
			} else {
				log.Printf("⚠️ [AGENT-EXECUTOR] Failed to parse LLM tool calls: %v", err)
			}
		}
	}

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

								if !allUp {
									log.Printf("📱 [AGENT-EXECUTOR] Website(s) down or warning, preparing Telegram notification...")
									message += "\n⚠️ Some websites have issues - please check!"

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
									log.Printf("📱 [AGENT-EXECUTOR] Skipping Telegram notification because all websites are operational")
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

			// Special handling for price_monitor specialized flow
			if strings.HasPrefix(task.ID, "price_monitor_") {
				if toolID != "mcp_smart_scrape" {
					// We handle all steps inside the mcp_smart_scrape iteration, so skip others
					continue
				}

				log.Printf("🛍️ [AGENT-EXECUTOR] Running specialized price monitor for task: %s...", task.ID)
				productURL, _ := params["url"].(string)
				if productURL == "" {
					productURL = "https://www.amazon.fr/-/en/gp/product/B0G1CC2949"
				}

				var lastPrice string
				historyPath := "config/price_history.json"
				historyMap := make(map[string]interface{})

				// 1. Read history
				if b, err := os.ReadFile(historyPath); err == nil {
					json.Unmarshal(b, &historyMap)

					// Migration: if the file has "last_price" key at root, it's the old single-item format.
					if lp, ok := historyMap["last_price"].(string); ok {
						// Only apply legacy migration if it matches our primary demo URL or if it's the only one we have
						if productURL == "https://www.amazon.fr/-/en/gp/product/B0G1CC2949" || len(historyMap) <= 2 {
							lastPrice = lp
							log.Printf("🛍️ [AGENT-EXECUTOR] Migration: found legacy price history: %s", lastPrice)
							// Remove legacy key to prevent infinite migration
							delete(historyMap, "last_price")
							delete(historyMap, "updated_at")
						}
					}

					// Try URL-indexed format
					if item, ok := historyMap[productURL].(map[string]interface{}); ok {
						if lp, ok := item["price"].(string); ok {
							lastPrice = lp
						}
					}

					if lastPrice != "" {
						log.Printf("🛍️ [AGENT-EXECUTOR] Loaded last price for %s: %s", productURL, lastPrice)
					}
				} else {
					log.Printf("🛍️ [AGENT-EXECUTOR] No previous price history found or failed to read: %v", err)
				}

				// 2. Execute scrape
				toolStartTime := time.Now()
				// Add goal to satisfy validation
				params["goal"] = "extract price"
				result, err := adapter.Execute(ctx, params)
				duration := time.Since(toolStartTime)

				productURL, _ = params["url"].(string)
				if productURL == "" {
					productURL = "https://www.amazon.fr/-/en/gp/product/B0G1CC2949"
				}

				productName := "ASUS Ascent Laptop" // Fallback
				if resultMap, ok := result.(map[string]interface{}); ok {
					if title, ok := resultMap["page_title"].(string); ok && title != "" {
						productName = strings.TrimSpace(strings.Split(title, ":")[0])
					}
					// Check for explicit product_name if scraper found it
					if res, ok := resultMap["result"].(map[string]interface{}); ok {
						if name, ok := res["product_name"].(string); ok && name != "" {
							productName = name
						}
					}
				}

				toolCall := ToolCall{
					ToolID:   toolID,
					Params:   params,
					Duration: duration,
				}

				if err != nil {
					toolCall.Error = err.Error()
					toolCalls = append(toolCalls, toolCall)
					log.Printf("❌ [AGENT-EXECUTOR] Scrape failed: %v", err)
					continue
				}

				// 3. Extract price from result
				var currentPrice string
				if resultMap, ok := result.(map[string]interface{}); ok {
					var innerResult map[string]interface{}

					// 3.1. Try structured result first (most reliable)
					if res, ok := resultMap["result"].(map[string]interface{}); ok {
						innerResult = res
						log.Printf("🛍️ [AGENT-EXECUTOR] Using structured 'result' key")
					} else if res, ok := resultMap["results"].(map[string]interface{}); ok {
						innerResult = res
						log.Printf("🛍️ [AGENT-EXECUTOR] Using structured 'results' key")
					}

					// 3.2. Fallback to parsing text content if structured result is missing or empty
					if len(innerResult) == 0 {
						if contentArr, ok := resultMap["content"].([]interface{}); ok && len(contentArr) > 0 {
							if firstItem, ok := contentArr[0].(map[string]interface{}); ok {
								if scrapedText, ok := firstItem["text"].(string); ok {
									log.Printf("🛍️ [AGENT-EXECUTOR] Parsing text content (fallback)")
									// More robust header removal: find first '{'
									if startIdx := strings.Index(scrapedText, "{"); startIdx != -1 {
										scrapedText = scrapedText[startIdx:]
									}

									if err := json.Unmarshal([]byte(scrapedText), &innerResult); err != nil {
										log.Printf("⚠️ [AGENT-EXECUTOR] Failed to unmarshal scraper text: %v", err)
										// SUPER FALLBACK: If not JSON, try regex on the whole text
										currentPrice = extractMainPrice(scrapedText)
										if currentPrice != "" {
											log.Printf("🛍️ [AGENT-EXECUTOR] Found price via regex fallback in text: %s", currentPrice)
										}
									}
								}
							}
						}
					}

					// 3.2.5. Handling string results (e.g., tool returned "Could not extract price" or raw text)
					if len(innerResult) == 0 && currentPrice == "" {
						if strResult, ok := result.(string); ok {
							log.Printf("🛍️ [AGENT-EXECUTOR] Tool returned string result, searching for price patterns...")
							currentPrice = extractMainPrice(strResult)
							if currentPrice != "" {
								log.Printf("🛍️ [AGENT-EXECUTOR] Found price via regex in string result: %s", currentPrice)
							}
						}
					}

					// 3.3. Extract value from innerResult
					if len(innerResult) > 0 {
						// Try to find in nested 'extractions' first (compatibility)
						source := innerResult
						if extractions, ok := innerResult["extractions"].(map[string]interface{}); ok {
							source = extractions
						}

						// Iterate and find the first value that looks like a price
						// Prioritize names containing "price"
						for _, key := range []string{"price", "field_1771953582615"} {
							if v, ok := source[key]; ok {
								if strVal, isStr := v.(string); isStr && strVal != "" {
									// Clean potential baggage like "Price (€3,309.99x)"
									cleaned := extractMainPrice(strVal)
									if cleaned != "" {
										currentPrice = cleaned
										log.Printf("🛍️ [AGENT-EXECUTOR] Found price in priority key '%s' (cleaned): %s", key, currentPrice)
										break
									}
									currentPrice = strVal
									log.Printf("🛍️ [AGENT-EXECUTOR] Found price in priority key '%s' (raw): %s", key, currentPrice)
									break
								}
							}
						}

						if currentPrice == "" {
							for k, v := range source {
								if k == "page_title" || k == "page_url" || k == "cookies" || k == "cleaned_html" {
									continue
								}
								if strVal, isStr := v.(string); isStr && strVal != "" {
									// Clean potential baggage like "Price (€3,309.99x)"
									cleaned := extractMainPrice(strVal)
									if cleaned != "" {
										currentPrice = cleaned
										log.Printf("🛍️ [AGENT-EXECUTOR] Found price in key '%s': %s", k, currentPrice)
										break
									}
									// Fallback if cleaning found nothing
									currentPrice = strVal
									log.Printf("🛍️ [AGENT-EXECUTOR] Found raw value in key '%s': %s", k, currentPrice)
									break
								}
							}
						}

						// 3.4. Final Fallback: Search in cleaned_html if available
						if currentPrice == "" {
							if html, ok := innerResult["cleaned_html"].(string); ok && html != "" {
								log.Printf("🛍️ [AGENT-EXECUTOR] Searching for price patterns in cleaned_html...")
								currentPrice = extractMainPrice(html)
								if currentPrice != "" {
									log.Printf("🛍️ [AGENT-EXECUTOR] Found price via regex in cleaned_html: %s", currentPrice)
								}
							}
						}
					}
				}

				if currentPrice == "" {
					log.Printf("❌ [AGENT-EXECUTOR] Could not extract price from scraper results")
					toolCall.Result = "Could not extract price"
					toolCalls = append(toolCalls, toolCall)
					continue
				}

				// Keep result lightweight to avoid LLM context blowout
				simplifiedResult := map[string]interface{}{
					"status":          "success",
					"extracted_price": currentPrice,
				}
				toolCall.Result = simplifiedResult
				toolCalls = append(toolCalls, toolCall)
				results = append(results, simplifiedResult)

				log.Printf("🛍️ [AGENT-EXECUTOR] Current Price: %s (Last Price: %s)", currentPrice, lastPrice)

				// 4. Compare and alert
				if lastPrice != "" && lastPrice != currentPrice {
					log.Printf("📱 [AGENT-EXECUTOR] Price changed from %s to %s! Sending Telegram notification...", lastPrice, currentPrice)
					var telegramAdapter *ToolAdapter
					for i := range agentInstance.Tools {
						if agentInstance.Tools[i].ToolID == "tool_telegram_send" {
							telegramAdapter = &agentInstance.Tools[i]
							break
						}
					}

					if telegramAdapter != nil {
						// Calculate percentage change
						oldVal := parsePriceToFloat(lastPrice)
						newVal := parsePriceToFloat(currentPrice)
						var changeText string
						var emoji string = "🔄"

						if oldVal > 0 && newVal > 0 {
							diff := newVal - oldVal
							percent := (diff / oldVal) * 100
							if diff < 0 {
								changeText = fmt.Sprintf("📉 Drop: %.2f%% (-%.2f)", -percent, -diff)
								emoji = "📉 *PRICE DROP*"
							} else {
								changeText = fmt.Sprintf("📈 Increase: %.2f%% (+%.2f)", percent, diff)
								emoji = "📈 *PRICE INCREASE*"
							}
						}

						msg := fmt.Sprintf("%s\n\n*Product:* %s\n*Price:* %s\n*Previous:* %s\n%s\n\n🔗 [View Product](%s)\n\n_Checked at: %s_",
							emoji, productName, currentPrice, lastPrice, changeText, productURL, time.Now().Format("15:04:05 02 Jan"))

						telParams := map[string]interface{}{
							"message":    msg,
							"chat_id":    os.Getenv("TELEGRAM_CHAT_ID"),
							"parse_mode": "Markdown",
						}
						log.Printf("📱 [AGENT-EXECUTOR] Message content:\n%s", msg)

						telegramResult, telErr := telegramAdapter.Execute(ctx, telParams)
						if telErr != nil {
							log.Printf("⚠️ [AGENT-EXECUTOR] Failed to send Telegram notification: %v", telErr)
						} else {
							results = append(results, map[string]interface{}{"telegram": telegramResult})
						}
					}
				}

				// 5. Save new history (Update map and save)
				historyMap[productURL] = map[string]string{
					"price":      currentPrice,
					"updated_at": time.Now().Format(time.RFC3339),
				}
				if jsonBytes, err := json.MarshalIndent(historyMap, "", "  "); err == nil {
					os.WriteFile(historyPath, jsonBytes, 0644)
					log.Printf("✅ [AGENT-EXECUTOR] History updated for %s", productURL)
				} else {
					log.Printf("⚠️ [AGENT-EXECUTOR] Failed to marshal history for saving: %v", err)
				}
				log.Printf("✅ [AGENT-EXECUTOR] Price monitor task completed: %s", task.ID)
				continue
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

record_history:
	duration := time.Since(startTime)
	log.Printf("✅ [AGENT-EXECUTOR] Agent %s completed in %v", agentID, duration)

	result := map[string]interface{}{
		"agent_id":   agentID,
		"input":      input,
		"results":    results,
		"duration":   duration.String(),
		"tool_calls": toolCalls,
	}

	// NEW: Response Synthesis Step
	// If we have an LLM client, use it to synthesize a human-readable summary of the results
	if len(results) > 0 {
		var summary string
		var synthesisErr error

		if e.llmClient != nil {
			log.Printf("🤖 [AGENT-EXECUTOR] Synthesizing human-readable response...")

			// Sanitize results for synthesis: strip massive fields like raw HTML/Markdown
			// to avoid blowing out the LLM context window.
			sanitizedResults := make([]interface{}, len(results))
			for i, res := range results {
				if m, ok := res.(map[string]interface{}); ok {
					// Create a shallow copy and remove blacklisted huge fields
					sanitized := make(map[string]interface{})
					for k, v := range m {
						if k != "cleaned_html" && k != "html" && k != "markdown" && k != "raw_content" {
							sanitized[k] = v
						}
					}
					sanitizedResults[i] = sanitized
				} else {
					sanitizedResults[i] = res
				}
			}

			resultsJSON, _ := json.MarshalIndent(sanitizedResults, "", "  ")
			synthesisPrompt := fmt.Sprintf(`You are the Response Synthesis Engine for an Intelligent AI Agent.
The user asked: "%s"

The agent executed tools and obtained these raw results:
%s

### TASK
Synthesize a clean, professional, and HIGHLY READABLE summary for the user.

### FORMATTING RULES
1. **USE MARKDOWN TABLES** for any list of items (e.g., products, prices, rates, stock data).
   - If there match-able columns (like "Product Name" and "Interest Rate"), align them in a table.
   - Example: 
     | Product | Rate |
     | :--- | :--- |
     | Flex Saver | 5.00%% |
2. **BE CONCISE**. Do not repeat the same data in text and table. Use the table as the primary source of truth.
3. use **BOLD** for headings and key values.
4. If no data was found or an error occurred, provide a clear, helpful explanation.
5. DO NOT mention technical internal keys like "result", "cleaned_html", or regex patterns.

Synthesized Response:`, input, string(resultsJSON))

			// Use a dedicated context for synthesis with a generous timeout, as RPI/Ollama can be slow.
			// This ensures synthesis completes even if the main request context has expired.
			synthesisCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			summary, synthesisErr = e.llmClient.callLLMWithContextAndPriority(synthesisCtx, synthesisPrompt, PriorityHigh)
			cancel()
		}

		if synthesisErr != nil {
			log.Printf("⚠️ [AGENT-EXECUTOR] Response synthesis failed: %v", synthesisErr)
		} else if summary != "" {
			log.Printf("✅ [AGENT-EXECUTOR] Response synthesized successfully")
			result["summary"] = strings.TrimSpace(summary)
		}

		// Prune or truncate the main results field to stay focused on the answer.
		// If we have a summary, we move raw data out of the way to simplify the response.
		hasSummary := result["summary"] != nil
		resultsJSON_raw, _ := json.Marshal(results)
		isHuge := len(resultsJSON_raw) > 2000000 // 2MB threshold

		if hasSummary || isHuge {
			result["raw_results_count"] = len(results)
			if hasSummary {
				// Hide raw data to focus on the summary
				result["results"] = "Raw data hidden (see summary above)"
				result["tool_calls"] = "Steps hidden (summary available)"
			} else if isHuge {
				result["results"] = "Raw data has been truncated because it exceeds 2MB. Check logs for details."
				if result["summary"] == nil {
					result["summary"] = fmt.Sprintf("The extraction result was too large to display directly (%d bytes).", len(resultsJSON_raw))
				}
			}
		}
	}

	// Record successful execution in history
	if e.history != nil && executionID != "" {
		// Prune tool calls results before saving to history to avoid huge Redis payloads
		prunedToolCalls := make([]ToolCall, len(toolCalls))
		for i, tc := range toolCalls {
			prunedToolCalls[i] = tc
			if resMap, ok := tc.Result.(map[string]interface{}); ok {
				prunedRes := make(map[string]interface{})
				for k, v := range resMap {
					// Strip massive fields
					if k == "cleaned_html" || k == "html" || k == "markdown" || k == "raw_content" || k == "body" {
						if strVal, ok := v.(string); ok && len(strVal) > 1000 {
							prunedRes[k] = fmt.Sprintf("[TRUNCATED: %d bytes]", len(strVal))
							continue
						}
					}
					prunedRes[k] = v
				}
				prunedToolCalls[i].Result = prunedRes
			}
		}

		execution := &AgentExecution{
			ID:        executionID,
			AgentID:   agentID,
			Input:     input,
			Status:    "success",
			Result:    result,
			Duration:  duration,
			ToolCalls: prunedToolCalls,
			StartedAt: startTime,
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

// extractMainPrice searches for price patterns and picks the most likely main price
func extractMainPrice(text string) string {
	// 1. Regex to match prices like €3,309.99, 3.291,04 €, £45.00, etc.
	// We handle:
	// - Optional currency symbol (€, $, £)
	// - Optional whitespace
	// - Digits with thousands separators (comma, dot, or space)
	// - Decimal part (comma or dot followed by 2 digits)
	// - Optional currency symbol/code at the end
	re := regexp.MustCompile(`(?i)(?:€|\$|£|GBP|EUR|USD)\s*(\d{1,3}(?:[.,\s]\d{3})*[.,]\d{2})|(\d{1,3}(?:[.,\s]\d{3})*[.,]\d{2})\s*(?:€|\$|£|EUR|GBP|USD)`)

	matches := re.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return ""
	}

	var bestPrice string
	var maxVal float64 = -1

	for _, m := range matches {
		matchStr := m[0]
		// Extract numeric part from match (either group 1 or 2)
		numStr := m[1]
		if numStr == "" {
			numStr = m[2]
		}

		// Normalize number string for parsing:
		// Remove spaces, remove thousands separators (careful about comma/dot swap)
		// We assume decimal is the LAST separator
		cleanNum := numStr
		cleanNum = strings.ReplaceAll(cleanNum, " ", "")

		// Handle cases where comma is thousands and dot is decimal (e.g. 3,309.99)
		// OR cases where dot is thousands and comma is decimal (e.g. 3.309,99)
		lastDot := strings.LastIndex(cleanNum, ".")
		lastComma := strings.LastIndex(cleanNum, ",")

		if lastDot > lastComma {
			// Dot is decimal (US style)
			cleanNum = strings.ReplaceAll(cleanNum, ",", "")
		} else if lastComma > lastDot {
			// Comma is decimal (EU style)
			cleanNum = strings.ReplaceAll(cleanNum, ".", "")
			cleanNum = strings.ReplaceAll(cleanNum, ",", ".")
		} else {
			// No separators or only one type. If only one, assume it's decimal if it's near the end.
			// But wait, if it's just "3309", it won't have matched our regex which asks for \d{2} decimals.
		}

		val, err := strconv.ParseFloat(cleanNum, 64)
		if err == nil {
			// Priority:
			// 1. Pick the largest price (usually the product price, not a warranty or shipping fee)
			// 2. Ignore extremely small prices if larger ones exist
			if val > maxVal {
				maxVal = val
				bestPrice = matchStr
			}
		}
	}

	return bestPrice
}

// parsePriceToFloat parses a price string (e.g. "€3,660.00") to float64
func parsePriceToFloat(s string) float64 {
	// Re-use core regex from extractMainPrice
	re := regexp.MustCompile(`\d{1,3}(?:[.,\s]\d{3})*[.,]\d{2}`)
	match := re.FindString(s)
	if match == "" {
		// Try a simpler match if formatted one fails
		re = regexp.MustCompile(`\d+[.,]\d{2}`)
		match = re.FindString(s)
	}
	if match == "" {
		return 0
	}

	cleanNum := strings.ReplaceAll(match, " ", "")
	cleanNum = strings.ReplaceAll(cleanNum, "\u00a0", "") // Non-breaking space

	lastDot := strings.LastIndex(cleanNum, ".")
	lastComma := strings.LastIndex(cleanNum, ",")

	if lastDot > lastComma {
		// Dot is decimal
		cleanNum = strings.ReplaceAll(cleanNum, ",", "")
	} else if lastComma > lastDot {
		// Comma is decimal
		cleanNum = strings.ReplaceAll(cleanNum, ".", "")
		cleanNum = strings.ReplaceAll(cleanNum, ",", ".")
	}

	val, _ := strconv.ParseFloat(cleanNum, 64)
	return val
}
