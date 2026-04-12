package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// considerToolCreationFromExecution analyzes successful code execution and creates a tool if it's general enough
func (ie *IntelligentExecutor) considerToolCreationFromExecution(req *ExecutionRequest, result *IntelligentExecutionResult, code *GeneratedCode) {
	if code == nil || code.Code == "" {
		return
	}

	if !ie.isCodeGeneralEnoughForTool(code.Code, code.Language, req.Description) {
		return
	}

	toolID := ie.generateToolIDFromCode(code.Language, code.Code, req.TaskName)

	if ie.toolExists(toolID) {
		log.Printf("🔧 [TOOL-CREATOR] Tool %s already exists, skipping creation", toolID)
		return
	}

	tool := ie.createToolFromCode(toolID, req, code, result)

	if err := ie.registerToolViaAPI(tool); err != nil {
		log.Printf("⚠️ [TOOL-CREATOR] Failed to register tool %s: %v", toolID, err)
		return
	}

	log.Printf("✅ [TOOL-CREATOR] Successfully created and registered tool %s from successful execution", toolID)
}

// isCodeGeneralEnoughForTool uses LLM to determine if code aligns with system objectives and is suitable as a tool
func (ie *IntelligentExecutor) isCodeGeneralEnoughForTool(code, language, description string) bool {
	c := strings.TrimSpace(code)

	if len(c) < 100 {
		return false
	}

	if ie.llmClient == nil {
		log.Printf("⚠️ [TOOL-CREATOR] LLM client not available, skipping tool creation")
		return false
	}

	prompt := ie.buildToolEvaluationPrompt(code, language, description)

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()

	response, err := ie.llmClient.callLLMWithContextAndPriority(ctx, prompt, PriorityLow)
	if err != nil {
		log.Printf("⚠️ [TOOL-CREATOR] LLM evaluation failed: %v, skipping tool creation", err)
		return false
	}

	shouldCreate, reason := ie.parseToolEvaluationResponse(response)

	if shouldCreate {
		log.Printf("✅ [TOOL-CREATOR] LLM recommends tool creation: %s", reason)
		return true
	}

	log.Printf("🔍 [TOOL-CREATOR] LLM does not recommend tool creation: %s", reason)
	return false
}

// buildToolEvaluationPrompt creates a prompt for LLM to evaluate if code should become a tool
func (ie *IntelligentExecutor) buildToolEvaluationPrompt(code, language, description string) string {
	var prompt strings.Builder

	prompt.WriteString("You are evaluating whether successfully executed code should be converted into a reusable tool for an autonomous AI system.\n\n")

	prompt.WriteString("SYSTEM OBJECTIVES:\n")
	prompt.WriteString("This system is designed for:\n")
	prompt.WriteString("1. Autonomous task execution - generating and executing code to accomplish goals\n")
	prompt.WriteString("2. Knowledge management - building and querying knowledge graphs, episodic memory\n")
	prompt.WriteString("3. Goal tracking and achievement - managing and progressing toward objectives\n")
	prompt.WriteString("4. Tool creation and reuse - building reusable capabilities that can be invoked by the system\n")
	prompt.WriteString("5. Learning from experience - improving performance based on past executions\n")
	prompt.WriteString("6. Multi-domain workflow orchestration - handling complex business processes\n\n")

	prompt.WriteString("EVALUATION CRITERIA:\n")
	prompt.WriteString("A tool should be created if the code:\n")
	prompt.WriteString("- Is general/reusable enough to be useful in multiple contexts (not task-specific)\n")
	prompt.WriteString("- Aligns with system objectives (autonomous execution, knowledge management, goal achievement)\n")
	prompt.WriteString("- Would be useful for future autonomous task execution\n")
	prompt.WriteString("- Has clear inputs and outputs (can be parameterized)\n")
	prompt.WriteString("- Represents a meaningful capability (not trivial one-liners)\n\n")

	prompt.WriteString("CODE TO EVALUATE:\n")
	prompt.WriteString(fmt.Sprintf("Language: %s\n", language))
	prompt.WriteString(fmt.Sprintf("Task Description: %s\n", description))
	prompt.WriteString(fmt.Sprintf("Code:\n```%s\n%s\n```\n\n", language, code))

	prompt.WriteString("Respond with ONLY a JSON object in this exact format:\n")
	prompt.WriteString(`{"should_create_tool": true/false, "reason": "brief explanation"}`)
	prompt.WriteString("\n\nDo not include any other text, only the JSON object.")

	return prompt.String()
}

// parseToolEvaluationResponse parses LLM response to extract tool creation recommendation
func (ie *IntelligentExecutor) parseToolEvaluationResponse(response string) (shouldCreate bool, reason string) {

	response = strings.TrimSpace(response)

	jsonStart := strings.Index(response, "{")
	jsonEnd := strings.LastIndex(response, "}")
	if jsonStart == -1 || jsonEnd == -1 || jsonEnd <= jsonStart {
		log.Printf("⚠️ [TOOL-CREATOR] Could not find JSON in LLM response: %s", truncateString(response, 200))
		return false, "invalid response format"
	}

	jsonStr := response[jsonStart : jsonEnd+1]

	var result struct {
		ShouldCreateTool bool   `json:"should_create_tool"`
		Reason           string `json:"reason"`
	}

	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		log.Printf("⚠️ [TOOL-CREATOR] Failed to parse LLM response JSON: %v, response: %s", err, truncateString(jsonStr, 200))
		return false, "failed to parse response"
	}

	return result.ShouldCreateTool, result.Reason
}

// generateToolIDFromCode creates a stable tool ID from code characteristics
func (ie *IntelligentExecutor) generateToolIDFromCode(language, code, taskName string) string {

	norm := strings.ToLower(strings.TrimSpace(taskName))
	norm = strings.ReplaceAll(norm, " ", "_")
	norm = strings.ReplaceAll(norm, "/", "_")
	norm = strings.ReplaceAll(norm, "-", "_")

	if !strings.Contains(norm, "first") && !strings.Contains(norm, "n_") &&
		!strings.Contains(norm, "specific") && len(norm) < 30 {
		return "tool_" + norm
	}

	base := strings.ToLower(strings.TrimSpace(language))
	if base == "" {
		base = "util"
	}

	lower := strings.ToLower(code)
	score := 0
	keywords := []string{"http", "json", "parse", "extract", "client", "retry", "cache", "transform"}
	for _, kw := range keywords {
		if strings.Contains(lower, kw) {
			score++
		}
	}

	hash := len(code) % 1000
	return fmt.Sprintf("tool_%s_util_%d_%d", base, hash, score)
}

// toolExists checks if a tool already exists
func (ie *IntelligentExecutor) toolExists(toolID string) bool {
	if ie.hdnBaseURL == "" {
		return false
	}

	url := fmt.Sprintf("%s/api/v1/tools", ie.hdnBaseURL)
	resp, err := http.Get(url)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false
	}

	var result struct {
		Tools []struct {
			ID string `json:"id"`
		} `json:"tools"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return false
	}

	for _, tool := range result.Tools {
		if tool.ID == toolID {
			return true
		}
	}
	return false
}

// createToolFromCode creates a Tool definition from successful code execution
func (ie *IntelligentExecutor) createToolFromCode(toolID string, req *ExecutionRequest, code *GeneratedCode, result *IntelligentExecutionResult) map[string]interface{} {

	inputSchema := map[string]string{}
	if len(req.Context) > 0 {

		for key, value := range req.Context {
			if key != "input" && key != "artifact_names" && value != "" {

				paramType := "string"
				if _, err := strconv.Atoi(value); err == nil {
					paramType = "int"
				} else if _, err := strconv.ParseFloat(value, 64); err == nil {
					paramType = "float"
				} else if strings.ToLower(value) == "true" || strings.ToLower(value) == "false" {
					paramType = "bool"
				}
				inputSchema[key] = paramType
			}
		}
	}

	if len(inputSchema) == 0 {
		inputSchema["input"] = "string"
	}

	tool := map[string]interface{}{
		"id":           toolID,
		"name":         req.TaskName,
		"description":  fmt.Sprintf("Auto-created tool from successful execution: %s", req.Description),
		"input_schema": inputSchema,
		"output_schema": map[string]string{
			"output":  "string",
			"success": "bool",
		},
		"permissions":  []string{"proc:exec"},
		"safety_level": "medium",
		"created_by":   "agent",
		"exec": map[string]interface{}{
			"type":     "code",
			"code":     code.Code,
			"language": code.Language,
		},
	}

	return tool
}

// registerToolViaAPI registers a tool via the HDN API
func (ie *IntelligentExecutor) registerToolViaAPI(tool map[string]interface{}) error {
	if ie.hdnBaseURL == "" {
		return fmt.Errorf("HDN base URL not configured")
	}

	url := fmt.Sprintf("%s/api/v1/tools", ie.hdnBaseURL)
	data, err := json.Marshal(tool)
	if err != nil {
		return fmt.Errorf("failed to marshal tool: %v", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to register tool: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("tool registration failed with status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}
