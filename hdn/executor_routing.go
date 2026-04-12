package main

import (
	"context"
	"encoding/json"
	"fmt"
	"hdn/utils"
	"log"
	"regexp"
	"strings"
	"time"
)

// categorizeRequestForSafety uses LLM to intelligently categorize a request for safety evaluation
func (ie *IntelligentExecutor) categorizeRequestForSafety(req *ExecutionRequest) (map[string]interface{}, error) {

	if ie.isMathematicalTask(req.TaskName) || ie.isStringBasedTask(req.TaskName) {
		log.Printf("✅ [INTELLIGENT] Task '%s' looks safe (math/string) - skipping LLM categorization", req.TaskName)
		return map[string]interface{}{
			"human_harm":        false,
			"human_order":       true,
			"self_harm":         false,
			"privacy_violation": false,
			"endanger_others":   false,
			"order_unethical":   false,
			"discrimination":    false,
		}, nil
	}

	ctxSummary := utils.SafeResultSummary(req.Context, 100)
	prompt := fmt.Sprintf(`You are a safety analyzer. Analyze this task request and return ONLY a valid JSON object.

Task: %s
Description: %s
Context: %s
`, req.TaskName, req.Description, ctxSummary)

	prompt += `
Return this exact JSON format (no other text):
{
  "human_harm": false,
  "human_order": true,
  "self_harm": false,
  "privacy_violation": false,
  "endanger_others": false,
  "order_unethical": false,
  "discrimination": false
}

Rules:
- Set human_harm=true ONLY if the task directly harms humans (violence, injury, death)
- Set privacy_violation=true ONLY if the task steals, accesses, or exposes private data
- Set endanger_others=true ONLY if the task could cause physical damage or danger
- Set order_unethical=true ONLY if the task is clearly illegal or unethical
- Mathematical calculations, data analysis, and programming tasks are generally safe
- Be precise, not overly cautious`

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	priority := PriorityLow
	if req.HighPriority {
		priority = PriorityHigh
	}
	response, err := ie.llmClient.callLLMWithContextAndPriority(ctx, prompt, priority)
	if err != nil {
		log.Printf("❌ [INTELLIGENT] LLM safety analysis failed: %v", err)

		return map[string]interface{}{
			"human_harm":        false,
			"human_order":       true,
			"self_harm":         false,
			"privacy_violation": false,
			"endanger_others":   false,
			"order_unethical":   false,
			"discrimination":    false,
		}, nil
	}

	cleanResponse := strings.TrimSpace(response)
	cleanResponse = strings.TrimPrefix(cleanResponse, "```json")
	cleanResponse = strings.TrimSuffix(cleanResponse, "```")
	cleanResponse = strings.TrimSpace(cleanResponse)

	// Parse the JSON response
	var context map[string]interface{}
	if err := json.Unmarshal([]byte(cleanResponse), &context); err != nil {
		log.Printf("⚠️ [INTELLIGENT] Failed to parse LLM safety response: %v", err)
		log.Printf("⚠️ [INTELLIGENT] Raw response: %s", cleanResponse)

		context = map[string]interface{}{
			"human_harm":        false,
			"human_order":       true,
			"self_harm":         false,
			"privacy_violation": false,
			"endanger_others":   false,
			"order_unethical":   false,
			"discrimination":    false,
		}
	}

	log.Printf("🔍 [INTELLIGENT] LLM Safety Analysis: %+v", context)
	return context, nil
}

func (ie *IntelligentExecutor) shouldUseLLMSummarization(req *ExecutionRequest) bool {
	taskName := strings.ToLower(strings.TrimSpace(req.TaskName))
	return taskName == "analyze_bootstrap" || taskName == "analyze_belief"
}

func (ie *IntelligentExecutor) shouldUseSimpleInformational(req *ExecutionRequest) bool {
	desc := strings.ToLower(strings.TrimSpace(req.Description))

	if len(req.Description) >= 200 || strings.Count(req.Description, " ") >= 15 {
		return false
	}

	if req.Language != "" {
		return false
	}
	if req.Context != nil && (req.Context["matrix1"] != "" || req.Context["matrix2"] != "") {
		return false
	}

	actionKeywords := []string{
		"create", "write", "generate", "build", "implement",
		"calculate", "process", "analyze", "fetch", "get",
		"code", "program", "function", "script", "matrix",
		"addition", "operation", "perform", "scrape", "scraping",
	}

	for _, keyword := range actionKeywords {
		if strings.Contains(desc, keyword) {
			return false
		}
	}

	return true
}

func (ie *IntelligentExecutor) shouldUseDirectTool(req *ExecutionRequest) bool {
	if !strings.EqualFold(strings.TrimSpace(req.TaskName), "Tool Execution") {
		return false
	}

	desc := strings.TrimSpace(req.Description)
	if !strings.HasPrefix(desc, "Execute tool ") {
		return false
	}

	rest := strings.TrimPrefix(desc, "Execute tool ")
	toolID := ""
	for i := 0; i < len(rest); i++ {
		if rest[i] == ':' || rest[i] == ' ' || rest[i] == '\n' || rest[i] == '\t' {
			toolID = strings.TrimSpace(rest[:i])
			break
		}
	}
	if toolID == "" {
		toolID = strings.TrimSpace(rest)
	}

	safeTool := toolID == "tool_ls" || toolID == "tool_http_get" ||
		toolID == "tool_file_read" || toolID == "tool_file_write" ||
		toolID == "tool_exec"

	return safeTool
}

func (ie *IntelligentExecutor) shouldUseHypothesisTesting(req *ExecutionRequest, descLower, taskLower string) bool {

	hasPrefix := strings.HasPrefix(descLower, "test hypothesis:") ||
		strings.HasPrefix(taskLower, "test hypothesis:")

	if hasPrefix {

		excludePatterns := []string{
			"create a python program", "create python program",
			"write a program", "create a program",
		}

		for _, pattern := range excludePatterns {
			if strings.Contains(descLower, pattern) || strings.Contains(taskLower, pattern) {
				return false
			}
		}
		return true
	}

	if req.Context != nil {
		if v, ok := req.Context["hypothesis_testing"]; ok && v == "true" {
			log.Printf("🧪 [INTELLIGENT] Hypothesis testing flag set in context")
			return true
		}
	}

	return false
}

func (ie *IntelligentExecutor) shouldUseWebGathering(req *ExecutionRequest, descLower, taskLower string) bool {

	if pref, ok := req.Context["prefer_tools"]; ok {
		if strings.ToLower(strings.TrimSpace(pref)) == "true" {
			if len(ie.collectURLsFromContext(req.Context)) > 0 {
				return true
			}
		}
	}

	combined := descLower + " " + taskLower

	if strings.Contains(combined, "summarize") ||
		strings.Contains(combined, "send") ||
		strings.Contains(combined, "telegram") ||
		strings.Contains(combined, "email") ||
		strings.Contains(combined, "calculate") ||
		strings.Contains(combined, "write") ||
		strings.Contains(combined, "create") {
		return false
	}

	webKeywords := []string{
		"scrape", "scraping", "fetch", "gather information", "gather", "extract",
		"web page", "http", "url", "tool_http_get", "tool_html_scraper",
		"mcp_scrape_url", "mcp_smart_scrape",
		"crawler", "screen scraper", "screen-scraper", "scraper", "use tool",
	}

	for _, keyword := range webKeywords {
		if strings.Contains(combined, keyword) {
			return true
		}
	}

	return len(ie.collectURLsFromContext(req.Context)) > 0
}

func (ie *IntelligentExecutor) shouldUsePlanner(req *ExecutionRequest) bool {

	if pref, ok := req.Context["prefer_traditional"]; ok {
		if strings.ToLower(pref) == "true" {
			log.Printf("⚙️ [INTELLIGENT] prefer_traditional=true, skipping planner path")
			return false
		}
	}

	return ie.usePlanner && ie.plannerIntegration != nil && ie.isComplexTask(req)
}

func (ie *IntelligentExecutor) isInternalTask(taskLower string) bool {
	return taskLower == "goal execution" ||
		taskLower == "artifact_task" ||
		strings.HasPrefix(taskLower, "code_")
}

// isComplexTask determines if a task is complex enough to benefit from HTN planning
func (ie *IntelligentExecutor) isComplexTask(req *ExecutionRequest) bool {

	descLower := strings.ToLower(req.Description)
	taskLower := strings.ToLower(req.TaskName)
	combined := descLower + " " + taskLower

	simplePatterns := []string{
		"print",
		"create.*program",
		"write.*program",
		"generate.*program",
		"create.*go.*program",
		"write.*go.*program",
		"create.*python.*program",
		"write.*python.*program",
		"simple.*program",
		"hello.*world",
		"calculate.*number",
		"fibonacci",
		"prime.*number",
	}

	for _, pattern := range simplePatterns {
		matched, _ := regexp.MatchString(pattern, combined)
		if matched {
			log.Printf("✅ [INTELLIGENT] Task matches simple pattern '%s' - skipping hierarchical planning", pattern)
			return false
		}
	}

	if strings.Contains(combined, "summarize") ||
		strings.Contains(combined, "newsletter") ||
		strings.Contains(combined, "send") ||
		strings.Contains(combined, "telegram") ||
		strings.Contains(combined, "email") ||
		strings.Contains(combined, "report") {
		log.Printf("🧠 [INTELLIGENT] Task identified as complex by keyword matching")
		return true
	}

	if pref, ok := req.Context["prefer_traditional"]; ok && strings.ToLower(pref) == "true" {
		log.Printf("✅ [INTELLIGENT] prefer_traditional=true - skipping hierarchical planning")
		return false
	}

	complexity, err := ie.classifyTaskComplexity(req)
	if err != nil {

		if strings.Contains(err.Error(), "cancelled") || strings.Contains(err.Error(), "timeout") || strings.Contains(err.Error(), "deadline") {
			log.Printf("⏱️ [INTELLIGENT] LLM complexity classification cancelled/timed out: %v", err)

			return false
		}

		log.Printf("⚠️ [INTELLIGENT] LLM complexity classification failed: %v, defaulting to simple", err)
		return false
	}

	log.Printf("🧠 [INTELLIGENT] LLM classified task as: %s", complexity)
	return complexity == "complex"
}

// classifyTaskComplexity uses the LLM to determine if a task is simple or complex
func (ie *IntelligentExecutor) classifyTaskComplexity(req *ExecutionRequest) (string, error) {
	prompt := fmt.Sprintf(`You are a task complexity classifier. Analyze the following task and determine if it should be classified as "simple" or "complex".

Task Name: %s
Description: %s
Language: %s

Classification rules:
- SIMPLE: Basic code generation, single-purpose programs, simple calculations, straightforward implementations, printing text, creating a single file, simple algorithms
- COMPLEX: Multi-step workflows, system integrations, architectural decisions, complex business logic, multi-component solutions, multiple files, APIs, databases

🚨 CRITICAL: When in doubt, classify as SIMPLE. Only classify as COMPLEX if the task clearly requires multiple steps, multiple components, or system integration.

Examples:
- "Write a Python program that prints 'Hello World'" → SIMPLE
- "Create a Go program in main.go that prints 'Hello Steve'" → SIMPLE
- "Create a Go function that calculates fibonacci numbers" → SIMPLE
- "Create me a Go program" → SIMPLE
- "Build a REST API with authentication and database integration" → COMPLEX
- "Design a microservices architecture for e-commerce" → COMPLEX
- "Create a data pipeline that processes files and sends notifications" → COMPLEX
- "Create multiple programs that work together" → COMPLEX

Respond with only one word: "simple" or "complex"`,
		req.TaskName, req.Description, req.Language)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	priority := PriorityLow
	if req.HighPriority {
		priority = PriorityHigh
	}
	response, err := ie.llmClient.callLLMWithContextAndPriority(ctx, prompt, priority)
	if err != nil {
		return "", err
	}

	response = strings.ToLower(strings.TrimSpace(response))
	if response == "simple" || response == "complex" {
		return response, nil
	}

	if strings.Contains(response, "simple") {
		return "simple", nil
	}
	if strings.Contains(response, "complex") {
		return "complex", nil
	}

	return "", fmt.Errorf("unable to parse complexity classification from response: %s", response)
}
