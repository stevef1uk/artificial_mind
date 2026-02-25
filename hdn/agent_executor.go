package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
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

			// 1. Generic Pre-execution processing (e.g., multi-URL expansion)
			if toolID == "tool_http_get" {
				if websites, ok := params["websites"].([]interface{}); ok {
					var websiteResults []map[string]interface{}
					for _, website := range websites {
						url, _ := website.(string)
						if url == "" {
							continue
						}
						log.Printf("🌐 [AGENT-EXECUTOR] Checking website: %s", url)
						toolStartTime := time.Now()
						res, err := adapter.Execute(ctx, map[string]interface{}{"url": url})
						duration := time.Since(toolStartTime)

						siteRes := map[string]interface{}{"url": url, "duration": duration.String()}
						if err != nil {
							siteRes["status"] = "down"
							siteRes["status_text"] = "down"
							siteRes["error"] = err.Error()
						} else if rm, ok := res.(map[string]interface{}); ok {
							if status, ok := rm["status"].(int); ok {
								siteRes["status"] = status
								if status >= 200 && status < 300 {
									siteRes["status_text"] = "up"
								} else {
									siteRes["status_text"] = "warning"
								}
							}
						}
						websiteResults = append(websiteResults, siteRes)
						toolCalls = append(toolCalls, ToolCall{ToolID: toolID, Params: map[string]interface{}{"url": url}, Result: siteRes, Duration: duration})
					}
					results = append(results, map[string]interface{}{"task": task.ID, "websites": websiteResults})

					// Alert if configured
					if e.registry != nil {
						if agent, ok := e.registry.GetAgent(agentID); ok {
							e.handleWebsiteAlert(ctx, agent, websiteResults, &results)
						}
					}
					continue
				}
			}

			// 2. Standard Tool Execution
			toolStartTime := time.Now()
			result, err := adapter.Execute(ctx, params)
			duration := time.Since(toolStartTime)

			toolCall := ToolCall{
				ToolID:   toolID,
				Params:   params,
				Result:   result,
				Duration: duration,
			}
			if err != nil {
				toolCall.Error = err.Error()
				log.Printf("❌ [AGENT-EXECUTOR] Tool %s failed: %v", toolID, err)
			} else {
				log.Printf("✅ [AGENT-EXECUTOR] Tool %s completed in %v", toolID, duration)
				results = append(results, result)
			}
			toolCalls = append(toolCalls, toolCall)

			// 3. Generic Post-execution processing (Monitoring, Alerting)
			if e.registry != nil {
				if agent, ok := e.registry.GetAgent(agentID); ok {
					e.handleMonitoring(ctx, agent, &task, result, params, &results)
				}
			}
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
		execution := &AgentExecution{
			ID:        executionID,
			AgentID:   agentID,
			Input:     input,
			Status:    "success",
			Result:    result, // Use the already pruned result map
			Duration:  duration,
			ToolCalls: toolCalls, // Full tool calls are still preserved here
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
	// 1. Regex to match prices like €3,309.99, 3.291,04 €, £45.00, 3.879 €, etc.
	// We handle:
	// - Optional currency symbol (€, $, £)
	// - Optional whitespace
	// - Digits with thousands separators (comma, dot, or space)
	// - Optional decimal part (comma or dot followed by 2 digits)
	// - Optional currency symbol/code at the end
	re := regexp.MustCompile(`(?i)(?:€|\$|£|GBP|EUR|USD)\s*(\d{1,3}(?:[.,\s]\d{3})*(?:[.,]\d{2})?)|(\d{1,3}(?:[.,\s]\d{3})*(?:[.,]\d{2})?)\s*(?:€|\$|£|EUR|GBP|USD)`)

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
			// Dot is further right than comma.
			if lastComma != -1 || (len(cleanNum)-lastDot-1) != 3 {
				cleanNum = strings.ReplaceAll(cleanNum, ",", "")
			} else {
				// Only dot and exactly 3 digits after. Assume thousands separator.
				cleanNum = strings.ReplaceAll(cleanNum, ".", "")
			}
		} else if lastComma > lastDot {
			// Comma is further right than dot.
			if lastDot != -1 || (len(cleanNum)-lastComma-1) != 3 {
				cleanNum = strings.ReplaceAll(cleanNum, ".", "")
				cleanNum = strings.ReplaceAll(cleanNum, ",", ".")
			} else {
				// Only comma and exactly 3 digits after. Assume thousands separator.
				cleanNum = strings.ReplaceAll(cleanNum, ",", "")
			}
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

// extractValueFromResult pulls a specific field value from a scraper result
func (e *AgentExecutor) extractValueFromResult(result interface{}, fieldName string) string {
	if resultMap, ok := result.(map[string]interface{}); ok {
		var innerResult map[string]interface{}

		// Try structured results
		if res, ok := resultMap["result"].(map[string]interface{}); ok {
			innerResult = res
		} else if res, ok := resultMap["results"].(map[string]interface{}); ok {
			innerResult = res
		}

		// Fallback to parsing text content
		if len(innerResult) == 0 {
			if contentArr, ok := resultMap["content"].([]interface{}); ok && len(contentArr) > 0 {
				if firstItem, ok := contentArr[0].(map[string]interface{}); ok {
					if scrapedText, ok := firstItem["text"].(string); ok {
						if startIdx := strings.Index(scrapedText, "{"); startIdx != -1 {
							jsonPart := scrapedText[startIdx:]
							json.Unmarshal([]byte(jsonPart), &innerResult)
						}
						if len(innerResult) == 0 {
							// Regex fallback for price
							if strings.Contains(strings.ToLower(fieldName), "price") {
								return extractMainPrice(scrapedText)
							}
						}
					}
				}
			}
		}

		// Pull from innerResult
		if len(innerResult) > 0 {
			source := innerResult
			if extractions, ok := innerResult["extractions"].(map[string]interface{}); ok {
				source = extractions
			}

			// Try specific key
			if v, ok := source[fieldName]; ok {
				if strVal, ok := v.(string); ok && strVal != "" {
					if strings.Contains(strings.ToLower(fieldName), "price") {
						return extractMainPrice(strVal)
					}
					return strVal
				}
			}

			// Special Case: If we are looking for a price, search the entire cleaned_html as a high-coverage fallback.
			// extractMainPrice picks the largest price found, so it will correctly identify the product price
			// even if there are smaller shipping/discount prices on the page.
			if strings.Contains(strings.ToLower(fieldName), "price") {
				if html, ok := resultMap["cleaned_html"].(string); ok && html != "" {
					cleaned := extractMainPrice(html)
					if cleaned != "" {
						return cleaned
					}
				}
			}

			// Search all keys if fieldName not found
			for k, v := range source {
				if k == "page_title" || k == "page_url" || k == "cookies" || k == "cleaned_html" {
					continue
				}
				if strVal, ok := v.(string); ok && strVal != "" {
					if strings.Contains(strings.ToLower(fieldName), "price") {
						cleaned := extractMainPrice(strVal)
						if cleaned != "" {
							return cleaned
						}
					} else {
						return strVal
					}
				}
			}
		}

		// Final fallback for string results
		if strResult, ok := result.(string); ok {
			if strings.Contains(strings.ToLower(fieldName), "price") {
				return extractMainPrice(strResult)
			}
		}
	}
	return ""
}

// handleMonitoring orchestrates post-execution monitoring logic
func (e *AgentExecutor) handleMonitoring(ctx context.Context, agent *AgentInstance, task *AgentTask, result interface{}, params map[string]interface{}, results *[]interface{}) {
	monitorCfg, ok := params["monitoring"].(map[string]interface{})
	if !ok {
		log.Printf("⚠️ [AGENT-EXECUTOR] Monitoring config type is %T, could not cast to map[string]interface{}", params["monitoring"])
		return
	}

	monitorType, _ := monitorCfg["type"].(string)
	if monitorType == "value_change" {
		fieldName, _ := monitorCfg["field"].(string)
		historyPath, _ := monitorCfg["history_path"].(string)
		if fieldName == "" || historyPath == "" {
			return
		}

		currentValue := e.extractValueFromResult(result, fieldName)
		if currentValue == "" {
			log.Printf("⚠️ [AGENT-EXECUTOR] Could not extract monitoring field '%s'", fieldName)
			return
		}

		// Load history
		var lastValue string
		if b, err := os.ReadFile(historyPath); err == nil {
			var history struct {
				LastValue string `json:"last_price"` // Keeping key for compat with existing json
			}
			json.Unmarshal(b, &history)
			lastValue = history.LastValue
		}

		log.Printf("📊 [AGENT-EXECUTOR] Monitoring '%s': Current=%s, Previous=%s", fieldName, currentValue, lastValue)

		// Compare and Alert
		if lastValue != "" && lastValue != currentValue {
			log.Printf("📱 [AGENT-EXECUTOR] Change detected! Triggering alert...")

			// Find Telegram tool
			var telegramAdapter *ToolAdapter
			for i := range agent.Tools {
				if agent.Tools[i].ToolID == "tool_telegram_send" {
					telegramAdapter = &agent.Tools[i]
					break
				}
			}

			if telegramAdapter != nil {
				productName, _ := monitorCfg["product_name"].(string)
				if productName == "" {
					if resultMap, ok := result.(map[string]interface{}); ok {
						if title, ok := resultMap["page_title"].(string); ok {
							productName = strings.TrimSpace(strings.Split(title, ":")[0])
						}
					}
				}
				productURL, _ := params["url"].(string)

				// Calculate diff if price
				var changeText string
				var emoji string = "🔄 *CHANGE DETECTED*"
				if strings.Contains(strings.ToLower(fieldName), "price") {
					oldVal := parsePriceToFloat(lastValue)
					newVal := parsePriceToFloat(currentValue)
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
				}

				msg := fmt.Sprintf("%s\n\n*Product:* %s\n*Current %s:* %s\n*Previous:* %s\n%s\n\n🔗 [View Product](%s)\n\n_Checked at: %s_",
					emoji, productName, fieldName, currentValue, lastValue, changeText, productURL, time.Now().Format("15:04:05 02 Jan"))

				telParams := map[string]interface{}{
					"message":    msg,
					"chat_id":    os.Getenv("TELEGRAM_CHAT_ID"),
					"parse_mode": "Markdown",
				}

				tr, err := telegramAdapter.Execute(ctx, telParams)
				if err == nil {
					*results = append(*results, map[string]interface{}{"monitoring_alert": tr})
				}
			}
		}

		// Save history
		historyData := map[string]string{"last_price": currentValue}
		if b, err := json.MarshalIndent(historyData, "", "  "); err == nil {
			os.MkdirAll(filepath.Dir(historyPath), 0755)
			os.WriteFile(historyPath, b, 0644)
		}
	}
}

// handleWebsiteAlert formats and sends a Telegram alert for website status
func (e *AgentExecutor) handleWebsiteAlert(ctx context.Context, agent *AgentInstance, websiteResults []map[string]interface{}, results *[]interface{}) {
	// Find Telegram tool
	var telegramAdapter *ToolAdapter
	for i := range agent.Tools {
		if agent.Tools[i].ToolID == "tool_telegram_send" {
			telegramAdapter = &agent.Tools[i]
			break
		}
	}

	if telegramAdapter == nil {
		return
	}

	message := "🌐 *Website Status Report*\n\n"
	allUp := true
	for _, siteRes := range websiteResults {
		url, _ := siteRes["url"].(string)
		statusText := "❌ Down"
		if st, ok := siteRes["status_text"].(string); ok {
			if st == "up" {
				statusText = "✅ Up"
			} else if st == "warning" {
				statusText = "⚠️ Warning"
				allUp = false
			} else {
				allUp = false
			}
		} else {
			allUp = false
		}
		status, _ := siteRes["status"].(int)
		message += fmt.Sprintf("%s: %s (HTTP %v)\n", url, statusText, status)
	}

	if !allUp {
		telParams := map[string]interface{}{
			"message":    message + "\n⚠️ Issues detected!",
			"chat_id":    os.Getenv("TELEGRAM_CHAT_ID"),
			"parse_mode": "Markdown",
		}
		tr, err := telegramAdapter.Execute(ctx, telParams)
		if err == nil {
			*results = append(*results, map[string]interface{}{"telegram_alert": tr})
		}
	}
}
