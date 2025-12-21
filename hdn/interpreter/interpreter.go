package interpreter

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"
	"time"
)

// Interpreter handles natural language input processing
type Interpreter struct {
	llmClient           LLMClientInterface
	flexibleInterpreter *FlexibleInterpreter
}

// NaturalLanguageRequest represents a natural language input
type NaturalLanguageRequest struct {
	Input     string            `json:"input"`
	Context   map[string]string `json:"context,omitempty"`
	SessionID string            `json:"session_id,omitempty"`
}

// InterpretedTask represents a parsed task from natural language
type InterpretedTask struct {
	TaskName        string            `json:"task_name"`
	Description     string            `json:"description"`
	Context         map[string]string `json:"context"`
	Language        string            `json:"language"`
	ForceRegenerate bool              `json:"force_regenerate"`
	MaxRetries      int               `json:"max_retries"`
	Timeout         int               `json:"timeout"`
	IsMultiStep     bool              `json:"is_multi_step"`
	SubTasks        []InterpretedTask `json:"sub_tasks,omitempty"`
	OriginalInput   string            `json:"original_input"`
}

// InterpretationResult contains the result of natural language interpretation
type InterpretationResult struct {
	Success       bool                   `json:"success"`
	Tasks         []InterpretedTask      `json:"tasks"`
	Message       string                 `json:"message"`
	SessionID     string                 `json:"session_id"`
	InterpretedAt time.Time              `json:"interpreted_at"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
}

// NewInterpreter creates a new interpreter instance
func NewInterpreter(llmClient LLMClientInterface) *Interpreter {
	// Create a composite tool provider that combines HDN and MCP tools
	// Use environment variable or default to localhost for backward compatibility
	hdnURL := os.Getenv("HDN_URL")
	if hdnURL == "" {
		hdnURL = "http://localhost:8081"
	}
	toolProvider := NewCompositeToolProvider(hdnURL)

	// Create flexible LLM adapter
	flexibleLLM := NewFlexibleLLMAdapter(llmClient)

	// Create flexible interpreter
	flexibleInterpreter := NewFlexibleInterpreter(flexibleLLM, toolProvider)

	return &Interpreter{
		llmClient:           llmClient,
		flexibleInterpreter: flexibleInterpreter,
	}
}

// GetFlexibleInterpreter returns the flexible interpreter instance
func (i *Interpreter) GetFlexibleInterpreter() *FlexibleInterpreter {
	return i.flexibleInterpreter
}

// Interpret processes natural language input and converts it to structured tasks
func (i *Interpreter) Interpret(ctx context.Context, req *NaturalLanguageRequest) (*InterpretationResult, error) {
	return i.InterpretWithPriority(ctx, req, false) // Default to LOW priority (will be overridden by API)
}

// InterpretWithPriority processes natural language input with specified priority
func (i *Interpreter) InterpretWithPriority(ctx context.Context, req *NaturalLanguageRequest, highPriority bool) (*InterpretationResult, error) {
	log.Printf("🧠 [INTERPRETER] Processing natural language input: %s (priority: %v)", req.Input, highPriority)

	// Use flexible interpreter if available
	if i.flexibleInterpreter != nil {
		flexibleResult, err := i.flexibleInterpreter.InterpretWithPriority(ctx, req, highPriority)
		if err != nil {
			log.Printf("⚠️ [INTERPRETER] Flexible interpretation failed, falling back to legacy: %v", err)
		} else {
			// Convert flexible result to legacy format
			return i.convertFlexibleToLegacy(flexibleResult), nil
		}
	}

	// Fallback to legacy interpretation
	return i.legacyInterpret(ctx, req)
}

// legacyInterpret provides the original interpretation logic
func (i *Interpreter) legacyInterpret(ctx context.Context, req *NaturalLanguageRequest) (*InterpretationResult, error) {
	// First, try to detect if this is a multi-step request
	isMultiStep, err := i.detectMultiStepRequest(req.Input)
	if err != nil {
		log.Printf("⚠️ [INTERPRETER] Multi-step detection failed: %v", err)
		isMultiStep = false
	}

	var tasks []InterpretedTask

	if isMultiStep {
		// Parse multi-step request
		tasks, err = i.parseMultiStepRequest(ctx, req)
		if err != nil {
			return &InterpretationResult{
				Success: false,
				Message: fmt.Sprintf("Failed to parse multi-step request: %v", err),
			}, err
		}
	} else {
		// Parse single task request
		task, err := i.parseSingleTask(ctx, req)
		if err != nil {
			return &InterpretationResult{
				Success: false,
				Message: fmt.Sprintf("Failed to parse single task: %v", err),
			}, err
		}
		tasks = []InterpretedTask{task}
	}

	log.Printf("✅ [INTERPRETER] Successfully interpreted %d task(s)", len(tasks))

	return &InterpretationResult{
		Success:       true,
		Tasks:         tasks,
		Message:       fmt.Sprintf("Successfully interpreted %d task(s)", len(tasks)),
		SessionID:     req.SessionID,
		InterpretedAt: time.Now(),
	}, nil
}

// convertFlexibleToLegacy converts a flexible interpretation result to legacy format
func (i *Interpreter) convertFlexibleToLegacy(flexibleResult *FlexibleInterpretationResult) *InterpretationResult {
	var tasks []InterpretedTask

	// Convert based on response type
	switch flexibleResult.ResponseType {
	case ResponseTypeStructuredTask:
		if flexibleResult.StructuredTask != nil {
			tasks = []InterpretedTask{*flexibleResult.StructuredTask}
		}
	case ResponseTypeCodeArtifact:
		// Convert code artifact to a task
		task := InterpretedTask{
			TaskName:        "Code Generation",
			Description:     fmt.Sprintf("Generate %s code: %s", flexibleResult.CodeArtifact.Language, flexibleResult.CodeArtifact.Code),
			Language:        flexibleResult.CodeArtifact.Language,
			ForceRegenerate: false,
			MaxRetries:      3,
			Timeout:         300,
			IsMultiStep:     false,
			OriginalInput:   flexibleResult.Message,
		}
		tasks = []InterpretedTask{task}
	case ResponseTypeToolCall:
		// Convert tool call to a task
		task := InterpretedTask{
			TaskName:        "Tool Execution",
			Description:     fmt.Sprintf("Execute tool %s: %s", flexibleResult.ToolCall.ToolID, flexibleResult.ToolCall.Description),
			Language:        "go",
			ForceRegenerate: false,
			MaxRetries:      3,
			Timeout:         300,
			IsMultiStep:     false,
			OriginalInput:   flexibleResult.Message,
		}
		tasks = []InterpretedTask{task}
	default:
		// For plain text responses, do not emit a synthetic task.
		// Return an empty task list and surface the text in the message field only.
		tasks = []InterpretedTask{}
	}

	return &InterpretationResult{
		Success:       flexibleResult.Success,
		Tasks:         tasks,
		Message:       flexibleResult.Message,
		SessionID:     flexibleResult.SessionID,
		InterpretedAt: flexibleResult.InterpretedAt,
		Metadata:      flexibleResult.Metadata,
	}
}

// detectMultiStepRequest determines if the input contains multiple tasks
func (i *Interpreter) detectMultiStepRequest(input string) (bool, error) {
	// Look for common multi-step indicators
	multiStepPatterns := []string{
		`\band\b.*\bthen\b`,
		`\bfirst\b.*\bthen\b`,
		`\bcalculate\b.*\band\b.*\bshow\b`,
		`\bcreate\b.*\band\b.*\bdisplay\b`,
		`\bgenerate\b.*\band\b.*\bplot\b`,
		`\bfind\b.*\band\b.*\bdisplay\b`,
		`\bfind\b.*\band\b.*\bvisualiz(e|e)\b`,
		`\b(prime|primes)\b.*\band\b.*\b(graph|plot|chart|display|visualiz(e|e))\b`,
		`\bdisplay\b.*\b(graph|plot|chart)\b`,
		`\bvisualiz(e|e)\b.*\b(distribution|graph|plot|chart)\b`,
		`\bplot\b.*\band\b.*\bsave\b`,
		`\b;`,
		`\bthen\b`,
		`\bnext\b`,
		`\bafter\b`,
	}

	for _, pattern := range multiStepPatterns {
		matched, err := regexp.MatchString(`(?i)`+pattern, input)
		if err != nil {
			return false, err
		}
		if matched {
			return true, nil
		}
	}

	return false, nil
}

// parseSingleTask parses a single task from natural language
func (i *Interpreter) parseSingleTask(ctx context.Context, req *NaturalLanguageRequest) (InterpretedTask, error) {
	// Create a structured prompt for the LLM
	prompt := i.createTaskParsingPrompt(req.Input, req.Context)

	// Get LLM response
	response, err := i.llmClient.GenerateResponse(prompt, req.Context)
	if err != nil {
		return InterpretedTask{}, fmt.Errorf("LLM parsing failed: %v", err)
	}

	// Parse the structured response
	task, err := i.parseLLMResponse(response)
	if err != nil {
		return InterpretedTask{}, fmt.Errorf("failed to parse LLM response: %v", err)
	}

	// Set defaults
	if task.Language == "" {
		task.Language = i.inferLanguageFromRequest(req.Input, req.Context)
		if task.Language == "" {
			task.Language = "python"
		}
	}
	if task.MaxRetries == 0 {
		task.MaxRetries = 3
	}
	if task.Timeout == 0 {
		task.Timeout = 600
	}

	task.OriginalInput = req.Input
	task.IsMultiStep = false

	return task, nil
}

// parseMultiStepRequest parses a multi-step request into multiple tasks
func (i *Interpreter) parseMultiStepRequest(ctx context.Context, req *NaturalLanguageRequest) ([]InterpretedTask, error) {
	// Create a structured prompt for multi-step parsing
	prompt := i.createMultiStepParsingPrompt(req.Input, req.Context)

	// Get LLM response
	response, err := i.llmClient.GenerateResponse(prompt, req.Context)
	if err != nil {
		return nil, fmt.Errorf("LLM multi-step parsing failed: %v", err)
	}

	// Parse the structured response
	tasks, err := i.parseMultiStepLLMResponse(response)
	if err != nil {
		return nil, fmt.Errorf("failed to parse multi-step LLM response: %v", err)
	}

	// Set defaults for all tasks
	for i := range tasks {
		if tasks[i].Language == "" {
			tasks[i].Language = "python"
		}
		if tasks[i].MaxRetries == 0 {
			tasks[i].MaxRetries = 3
		}
		if tasks[i].Timeout == 0 {
			tasks[i].Timeout = 600
		}
		tasks[i].OriginalInput = req.Input
		tasks[i].IsMultiStep = true
	}

	return tasks, nil
}

// createTaskParsingPrompt creates a prompt for single task parsing
func (i *Interpreter) createTaskParsingPrompt(input string, context map[string]string) string {
	return fmt.Sprintf(`You are an AI task interpreter. Parse the following natural language request into a structured task format.

User Input: "%s"

Context: %s

Parse this into a JSON structure with the following fields:
- task_name: A descriptive name for the task (e.g., "CalculatePrimes", "CreateGraph", "AnalyzeData")
- description: A clear description of what the task should do
- context: A map of key-value pairs with relevant parameters extracted from the input
- language: The programming language to use (default: "python")
- force_regenerate: Whether to force regeneration of code (default: false)
- max_retries: Maximum number of retries (default: 3)
- timeout: Timeout in seconds (default: 30)

Examples:
- "Find the first 10 prime numbers" -> {"task_name": "CalculatePrimes", "description": "Calculate the first 10 prime numbers", "context": {"count": "10"}, "language": "python"}
- "Create a bar chart of sales data" -> {"task_name": "CreateBarChart", "description": "Create a bar chart visualization of sales data", "context": {"chart_type": "bar", "data_source": "sales"}, "language": "python"}

Return only the JSON structure, no additional text.`, input, formatContext(context))
}

// createMultiStepParsingPrompt creates a prompt for multi-step parsing
func (i *Interpreter) createMultiStepParsingPrompt(input string, context map[string]string) string {
	return fmt.Sprintf(`You are an AI task interpreter. Parse the following natural language request into multiple structured tasks.

User Input: "%s"

Context: %s

This appears to be a multi-step request. Break it down into individual tasks that can be executed in sequence.

Parse this into a JSON array of task objects, each with the following fields:
- task_name: A descriptive name for the task
- description: A clear description of what the task should do
- context: A map of key-value pairs with relevant parameters
- language: The programming language to use (default: "python")
- force_regenerate: Whether to force regeneration of code (default: false)
- max_retries: Maximum number of retries (default: 3)
- timeout: Timeout in seconds (default: 30)

Example:
"Find the first 20 primes and show me a graph of distribution" -> [
  {"task_name": "CalculatePrimes", "description": "Calculate the first 20 prime numbers", "context": {"count": "20"}, "language": "python"},
  {"task_name": "CreateDistributionGraph", "description": "Create a graph showing the distribution of prime numbers", "context": {"data_source": "primes_result", "graph_type": "distribution"}, "language": "python"}
]

Return only the JSON array, no additional text.`, input, formatContext(context))
}

// parseLLMResponse parses a single task from LLM response
func (i *Interpreter) parseLLMResponse(response string) (InterpretedTask, error) {
	// Clean the response (remove markdown formatting if present)
	cleaned := strings.TrimSpace(response)

	// Extract first complete JSON object using brace counting
	cleaned = extractFirstJSONObject(cleaned)

	// Unmarshal into a tolerant structure that accepts any JSON types in context
	type tempTask struct {
		TaskName        string                   `json:"task_name"`
		Description     string                   `json:"description"`
		Context         map[string]interface{}   `json:"context"`
		Language        string                   `json:"language"`
		ForceRegenerate bool                     `json:"force_regenerate"`
		MaxRetries      int                      `json:"max_retries"`
		Timeout         int                      `json:"timeout"`
		IsMultiStep     bool                     `json:"is_multi_step"`
		SubTasks        []map[string]interface{} `json:"sub_tasks,omitempty"`
		OriginalInput   string                   `json:"original_input"`
	}

	var t tempTask
	if err := json.Unmarshal([]byte(cleaned), &t); err != nil {
		return InterpretedTask{}, err
	}

	// Coerce context values to strings
	ctx := make(map[string]string)
	for k, v := range t.Context {
		ctx[k] = fmt.Sprintf("%v", v)
	}

	return InterpretedTask{
		TaskName:        t.TaskName,
		Description:     t.Description,
		Context:         ctx,
		Language:        t.Language,
		ForceRegenerate: t.ForceRegenerate,
		MaxRetries:      t.MaxRetries,
		Timeout:         t.Timeout,
		IsMultiStep:     t.IsMultiStep,
		OriginalInput:   t.OriginalInput,
	}, nil
}

// parseMultiStepLLMResponse parses multiple tasks from LLM response
func (i *Interpreter) parseMultiStepLLMResponse(response string) ([]InterpretedTask, error) {
	// Clean the response
	cleaned := strings.TrimSpace(response)

	// Extract first complete JSON array using bracket counting
	cleaned = extractFirstJSONArray(cleaned)

	// Unmarshal into tolerant temporary structures
	type tempTask struct {
		TaskName        string                 `json:"task_name"`
		Description     string                 `json:"description"`
		Context         map[string]interface{} `json:"context"`
		Language        string                 `json:"language"`
		ForceRegenerate bool                   `json:"force_regenerate"`
		MaxRetries      int                    `json:"max_retries"`
		Timeout         int                    `json:"timeout"`
		IsMultiStep     bool                   `json:"is_multi_step"`
		OriginalInput   string                 `json:"original_input"`
	}

	var tmp []tempTask
	if err := json.Unmarshal([]byte(cleaned), &tmp); err != nil {
		return nil, err
	}

	var tasks []InterpretedTask
	for _, t := range tmp {
		ctx := make(map[string]string)
		for k, v := range t.Context {
			ctx[k] = fmt.Sprintf("%v", v)
		}
		tasks = append(tasks, InterpretedTask{
			TaskName:        t.TaskName,
			Description:     t.Description,
			Context:         ctx,
			Language:        t.Language,
			ForceRegenerate: t.ForceRegenerate,
			MaxRetries:      t.MaxRetries,
			Timeout:         t.Timeout,
			IsMultiStep:     t.IsMultiStep,
			OriginalInput:   t.OriginalInput,
		})
	}

	return tasks, nil
}

// extractFirstJSONObject extracts the first complete {...} JSON object from text
func extractFirstJSONObject(text string) string {
	// Remove common markdown fence starts
	text = strings.ReplaceAll(text, "```json", "")
	text = strings.ReplaceAll(text, "```", "")
	// Find first '{' and track braces
	start := strings.Index(text, "{")
	if start == -1 {
		return strings.TrimSpace(text)
	}
	depth := 0
	for i := start; i < len(text); i++ {
		switch text[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return strings.TrimSpace(text[start : i+1])
			}
		}
	}
	return strings.TrimSpace(text[start:])
}

// extractFirstJSONArray extracts the first complete [...] JSON array from text
func extractFirstJSONArray(text string) string {
	// Remove common markdown fence starts
	text = strings.ReplaceAll(text, "```json", "")
	text = strings.ReplaceAll(text, "```", "")
	// Find first '[' and track brackets
	start := strings.Index(text, "[")
	if start == -1 {
		// Fallback to object extraction in case LLM returns an object
		return extractFirstJSONObject(text)
	}
	depth := 0
	for i := start; i < len(text); i++ {
		switch text[i] {
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return strings.TrimSpace(text[start : i+1])
			}
		}
	}
	return strings.TrimSpace(text[start:])
}

// formatContext formats context map for display
func formatContext(context map[string]string) string {
	if len(context) == 0 {
		return "None"
	}

	contextStr := ""
	for k, v := range context {
		contextStr += fmt.Sprintf("%s: %s, ", k, v)
	}
	return strings.TrimSuffix(contextStr, ", ")
}

// inferLanguageFromRequest detects the programming language from natural language input
func (i *Interpreter) inferLanguageFromRequest(input string, context map[string]string) string {
	inputLower := strings.ToLower(input)

	// Check for explicit language mentions
	if strings.Contains(inputLower, "go program") || strings.Contains(inputLower, "golang") ||
		strings.Contains(inputLower, "go code") || strings.Contains(inputLower, "go script") {
		return "go"
	}

	if strings.Contains(inputLower, "python") || strings.Contains(inputLower, "py script") {
		return "python"
	}

	if strings.Contains(inputLower, "javascript") || strings.Contains(inputLower, "js") ||
		strings.Contains(inputLower, "node") {
		return "javascript"
	}

	if strings.Contains(inputLower, "java") {
		return "java"
	}

	if strings.Contains(inputLower, "rust") {
		return "rust"
	}

	if strings.Contains(inputLower, "c++") || strings.Contains(inputLower, "cpp") {
		return "cpp"
	}

	if strings.Contains(inputLower, "c#") || strings.Contains(inputLower, "csharp") {
		return "csharp"
	}

	// Check context for language hints
	if lang, ok := context["language"]; ok && lang != "" {
		return strings.ToLower(lang)
	}

	// Check for file extensions in the input
	if strings.Contains(inputLower, ".go") {
		return "go"
	}
	if strings.Contains(inputLower, ".py") {
		return "python"
	}
	if strings.Contains(inputLower, ".js") {
		return "javascript"
	}
	if strings.Contains(inputLower, ".java") {
		return "java"
	}

	return ""
}
