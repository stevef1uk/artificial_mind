package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"strings"
	"time"
)

// --------- LLM Client ---------

type LLMClient struct {
	config     DomainConfig
	httpClient *http.Client
}

type LLMRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Temperature float64   `json:"temperature"`
	MaxTokens   int       `json:"max_tokens"`
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type LLMResponse struct {
	Choices []Choice  `json:"choices"`
	Error   *LLMError `json:"error,omitempty"`
}

type Choice struct {
	Message Message `json:"message"`
}

type LLMError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
}

func NewLLMClient(config DomainConfig) *LLMClient {
	// Default timeout
	timeout := 30 * time.Second
	// Allow override via settings.llm_timeout_seconds
	if v, ok := config.Settings["llm_timeout_seconds"]; ok && strings.TrimSpace(v) != "" {
		if sec, err := time.ParseDuration(strings.TrimSpace(v) + "s"); err == nil {
			timeout = sec
		}
	}
	return &LLMClient{
		config: config,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

func (c *LLMClient) GenerateMethod(taskName, description string, context map[string]string) (*MethodDef, error) {
	log.Printf("🤖 [LLM] Generating method for task: %s", taskName)
	log.Printf("🤖 [LLM] Description: %s", description)
	log.Printf("🤖 [LLM] Context: %+v", context)

	// Create a prompt for the LLM to generate a method
	prompt := c.buildMethodPrompt(taskName, description, context)
	log.Printf("🤖 [LLM] Generated prompt length: %d characters", len(prompt))

	// Call the LLM
	log.Printf("🤖 [LLM] Calling LLM with provider: %s", c.config.LLMProvider)
	response, err := c.callLLM(prompt)
	if err != nil {
		log.Printf("❌ [LLM] LLM call failed: %v", err)
		return nil, err
	}
	log.Printf("🤖 [LLM] Received response length: %d characters", len(response))

	// Parse the response into a MethodDef
	log.Printf("🤖 [LLM] Parsing LLM response")
	method, err := c.parseMethodResponse(response, taskName)
	if err != nil {
		log.Printf("❌ [LLM] Failed to parse response: %v", err)
		return nil, fmt.Errorf("failed to parse LLM response: %v", err)
	}

	log.Printf("✅ [LLM] Successfully generated method: %+v", method)
	return method, nil
}

// GenerateExecutableCode generates executable code for a given task
func (c *LLMClient) GenerateExecutableCode(taskName, description, language string, context map[string]string) (string, error) {
	log.Printf("🤖 [LLM] Generating executable code for task: %s", taskName)
	log.Printf("🤖 [LLM] Language: %s", language)
	log.Printf("🤖 [LLM] Description: %s", description)
	log.Printf("🤖 [LLM] Context: %+v", context)

	// Create a prompt for code generation
	prompt := c.buildCodePrompt(taskName, description, language, context)
	log.Printf("🤖 [LLM] Generated code prompt length: %d characters", len(prompt))

	// Call the LLM
	log.Printf("🤖 [LLM] Calling LLM with provider: %s", c.config.LLMProvider)
	response, err := c.callLLM(prompt)
	if err != nil {
		log.Printf("❌ [LLM] LLM call failed: %v", err)
		return "", err
	}
	log.Printf("🤖 [LLM] Received response length: %d characters", len(response))

	// Extract code from response
	code := c.extractCodeFromResponse(response, language)
	log.Printf("✅ [LLM] Successfully generated code: %d characters", len(code))

	return code, nil
}

func (c *LLMClient) ExecuteTask(taskName, prompt string, context map[string]string) (string, error) {
	log.Printf("🤖 [LLM] Executing task: %s", taskName)
	log.Printf("🤖 [LLM] Prompt: %s", prompt)
	log.Printf("🤖 [LLM] Context: %+v", context)

	// Create a prompt for task execution
	executionPrompt := c.buildExecutionPrompt(taskName, prompt, context)
	log.Printf("🤖 [LLM] Generated execution prompt length: %d characters", len(executionPrompt))

	// Call the LLM
	log.Printf("🤖 [LLM] Calling LLM for task execution")
	response, err := c.callLLM(executionPrompt)
	if err != nil {
		log.Printf("❌ [LLM] Task execution failed: %v", err)
		return "", err
	}

	log.Printf("✅ [LLM] Task execution completed. Response length: %d characters", len(response))
	return response, nil
}

func (c *LLMClient) buildMethodPrompt(taskName, description string, context map[string]string) string {
	contextStr := ""
	if len(context) > 0 {
		contextStr = "\nContext:\n"
		for k, v := range context {
			contextStr += fmt.Sprintf("- %s: %s\n", k, v)
		}
	}

	return fmt.Sprintf(`You are an AI assistant that helps create hierarchical task network (HTN) methods for task planning.

Task: %s
Description: %s%s

Please create a method definition for this task. The method should:
1. Have appropriate preconditions (what must be true before this task can be executed)
2. Break down the task into subtasks (smaller, more manageable tasks)
3. Be realistic and practical

Return your response as a JSON object with this exact structure:
{
  "task": "%s",
  "preconditions": ["list", "of", "preconditions"],
  "subtasks": ["list", "of", "subtask", "names"]
}

Example:
{
  "task": "WriteEmail",
  "preconditions": ["not email_written"],
  "subtasks": ["DraftEmail", "ReviewEmail", "SendEmail"]
}

Your response:`, taskName, description, contextStr, taskName)
}

// buildCodePrompt creates a prompt for code generation
func (c *LLMClient) buildCodePrompt(taskName, description, language string, context map[string]string) string {
	prompt := fmt.Sprintf(`🚫 CRITICAL RESTRICTION - MUST FOLLOW:
- NEVER use Docker commands (docker run, docker build, docker exec, etc.) - Docker is NOT available
- NEVER use subprocess.run with docker commands - this will cause FileNotFoundError
- NEVER use os.system with docker commands - this will fail
- You are already running inside a container, do NOT try to create more containers

You are an expert programmer. Generate executable %s code for the following task:

Task: %s
Description: %s
Language: %s

UNIQUE TASK ID: %s_%d

Context:
`, language, taskName, description, language, taskName, time.Now().UnixNano())

	// Add file path information for data files
	filePathInfo := ""
	for key, value := range context {
		prompt += fmt.Sprintf("- %s: %s\n", key, value)
		// Check if this looks like a data file reference
		if (strings.Contains(strings.ToLower(key), "data") ||
			strings.Contains(strings.ToLower(key), "file") ||
			strings.Contains(strings.ToLower(key), "source") ||
			strings.Contains(strings.ToLower(key), "input")) &&
			strings.Contains(value, ".") && !strings.Contains(value, " ") {
			filePathInfo += fmt.Sprintf("\nIMPORTANT: The file '%s' is available at '/app/data/%s' in the container.\n", value, value)
		}
	}

	prompt += fmt.Sprintf(`%s
Requirements:
1. Generate complete, executable %s code
2. Include proper error handling
3. Add comments explaining the logic
4. Make the code robust and efficient
5. Use the correct file paths for any data files (see IMPORTANT notes above)
6. Use ONLY standard library modules when possible to minimize dependencies
7. If you must use external packages, use only the most essential ones
8. Output must compile cleanly (for Go: run with go build) with no unused variables or imports
9. Use ASCII identifiers only; no non-ASCII names
10. Do NOT perform network/API calls unless explicitly requested
11. NEVER use Docker commands (docker run, docker build, etc.) - Docker is not available in the execution environment
12. NEVER mention Docker, containers, or containerization in comments or strings
13. Return ONLY the code, no explanations or markdown formatting

Generate the %s code:`, filePathInfo, language, language)

	return prompt
}

// extractCodeFromResponse extracts code from LLM response
func (c *LLMClient) extractCodeFromResponse(response, language string) string {
	// Remove markdown code blocks if present
	response = strings.TrimSpace(response)

	// Look for code blocks
	if strings.Contains(response, "```") {
		lines := strings.Split(response, "\n")
		var codeLines []string
		inCodeBlock := false

		for _, line := range lines {
			if strings.HasPrefix(line, "```") {
				if inCodeBlock {
					break
				}
				inCodeBlock = true
				continue
			}
			if inCodeBlock {
				codeLines = append(codeLines, line)
			}
		}

		if len(codeLines) > 0 {
			return strings.Join(codeLines, "\n")
		}
	}

	// If no code blocks, return the response as-is
	return response
}

func (c *LLMClient) buildExecutionPrompt(taskName, prompt string, context map[string]string) string {
	contextStr := ""
	if len(context) > 0 {
		contextStr = "\nContext:\n"
		for k, v := range context {
			contextStr += fmt.Sprintf("- %s: %s\n", k, v)
		}
	}

	return fmt.Sprintf(`You are an AI assistant that executes tasks. 

Task: %s
Instructions: %s%s

Please provide a detailed response on how you would execute this task. Be specific about the steps you would take and any considerations.

Your response:`, taskName, prompt, contextStr)
}

func (c *LLMClient) getModelName() string {
	if model, exists := c.config.Settings["model"]; exists {
		return model
	}

	// Default models for each provider
	switch c.config.LLMProvider {
	case "openai":
		return "gpt-3.5-turbo"
	case "anthropic":
		return "claude-3-sonnet-20240229"
	case "local", "ollama":
		return "gemma3:12b"
	default:
		return "gpt-3.5-turbo"
	}
}

func (c *LLMClient) parseMethodResponse(response, taskName string) (*MethodDef, error) {
	// Try to extract JSON from the response
	// The LLM might return the JSON wrapped in markdown or other text
	jsonStart := -1
	jsonEnd := -1

	// Look for JSON object boundaries
	for i, char := range response {
		if char == '{' && jsonStart == -1 {
			jsonStart = i
		}
		if char == '}' && jsonStart != -1 {
			jsonEnd = i
			break
		}
	}

	if jsonStart == -1 || jsonEnd == -1 {
		return nil, fmt.Errorf("could not find JSON in LLM response")
	}

	// Extract JSON
	jsonStr := response[jsonStart : jsonEnd+1]

	// Parse JSON
	var method MethodDef
	if err := json.Unmarshal([]byte(jsonStr), &method); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %v", err)
	}

	// Validate the method
	if method.Task != taskName {
		return nil, fmt.Errorf("method task name mismatch: expected %s, got %s", taskName, method.Task)
	}

	if len(method.Subtasks) == 0 {
		return nil, fmt.Errorf("method has no subtasks")
	}

	return &method, nil
}

// --------- Mock LLM Client for Testing ---------

type MockLLMClient struct {
	responses map[string]string
}

func NewMockLLMClient() *LLMClient {
	// Return a real client but with mock behavior
	client := &LLMClient{
		config: DomainConfig{
			LLMProvider: "mock",
		},
		httpClient: &http.Client{Timeout: 1 * time.Second},
	}
	return client
}

func (c *LLMClient) callLLM(prompt string) (string, error) {
	// Mock responses for testing
	if c.config.LLMProvider == "mock" {
		return c.getMockResponse(prompt)
	}

	// For real providers, use the actual implementation
	return c.callLLMReal(prompt)
}

func (c *LLMClient) callLLMWithContext(ctx context.Context, prompt string) (string, error) {
	// Mock responses for testing
	if c.config.LLMProvider == "mock" {
		return c.getMockResponse(prompt)
	}

	// For real providers, use the actual implementation with context
	return c.callLLMRealWithContext(ctx, prompt)
}

func (c *LLMClient) callLLMReal(prompt string) (string, error) {
	log.Printf("🌐 [LLM] Making API call to provider: %s", c.config.LLMProvider)
	log.Printf("🌐 [LLM] Timeout: %v", c.httpClient.Timeout)

	// Determine the API endpoint based on provider
	var apiURL string
	var apiKey string

	switch c.config.LLMProvider {
	case "openai":
		apiURL = "https://api.openai.com/v1/chat/completions"
		apiKey = c.config.LLMAPIKey
		log.Printf("🌐 [LLM] Using OpenAI API")
	case "anthropic":
		apiURL = "https://api.anthropic.com/v1/messages"
		apiKey = c.config.LLMAPIKey
		log.Printf("🌐 [LLM] Using Anthropic API")
	case "local", "ollama":
		// For local models, use Ollama. Allow override via settings["ollama_url"].
		if url, ok := c.config.Settings["ollama_url"]; ok && strings.TrimSpace(url) != "" {
			apiURL = normalizeOllamaURL(strings.TrimSpace(url))
		} else {
			apiURL = "http://localhost:11434/api/chat"
		}
		apiKey = ""
		log.Printf("🌐 [LLM] Using Ollama local API at %s", apiURL)
	default:
		log.Printf("❌ [LLM] Unsupported provider: %s", c.config.LLMProvider)
		return "", fmt.Errorf("unsupported LLM provider: %s", c.config.LLMProvider)
	}

	// Prepare the request based on provider
	var jsonData []byte
	var err error

	if c.config.LLMProvider == "local" {
		// Ollama uses a different format
		ollamaRequest := map[string]interface{}{
			"model": c.getModelName(),
			"messages": []map[string]string{
				{
					"role":    "user",
					"content": prompt,
				},
			},
			"stream": false,
		}
		jsonData, err = json.Marshal(ollamaRequest)
	} else {
		// OpenAI/Anthropic format
		request := LLMRequest{
			Model: c.getModelName(),
			Messages: []Message{
				{
					Role:    "user",
					Content: prompt,
				},
			},
			Temperature: 0.7,
			MaxTokens:   1000,
		}
		jsonData, err = json.Marshal(request)
	}

	if err != nil {
		return "", err
	}

	// Create HTTP request
	log.Printf("🌐 [LLM] Making request to: %s", apiURL)
	log.Printf("🌐 [LLM] Request data: %s", string(jsonData))
	req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", err
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	// Log request details (payload truncated to 8KB for readability)
	const maxLogBytes = 8 * 1024
	payloadPreview := jsonData
	if len(payloadPreview) > maxLogBytes {
		payloadPreview = payloadPreview[:maxLogBytes]
	}
	log.Printf("🌐 [LLM] Sending HTTP request to %s | provider=%s | model=%s | timeout=%s | payload_bytes=%d\nPayload Preview: %s",
		apiURL, c.config.LLMProvider, c.getModelName(), c.httpClient.Timeout.String(), len(jsonData), string(payloadPreview))

	// Make the request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		log.Printf("❌ [LLM] HTTP request failed: %v", err)
		return "", err
	}
	defer resp.Body.Close()

	log.Printf("🌐 [LLM] Received HTTP response with status: %d", resp.StatusCode)

	// Read response
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Printf("❌ [LLM] Failed to read response body: %v", err)
		return "", err
	}

	log.Printf("🌐 [LLM] Response body length: %d bytes", len(body))

	// Check for HTTP errors
	if resp.StatusCode != http.StatusOK {
		log.Printf("❌ [LLM] API error (status %d): %s", resp.StatusCode, string(body))
		return "", fmt.Errorf("LLM API error: %s", string(body))
	}

	// Parse response based on provider
	if c.config.LLMProvider == "local" {
		// Ollama response format
		log.Printf("🌐 [LLM] Parsing Ollama response")
		var ollamaResp map[string]interface{}
		if err := json.Unmarshal(body, &ollamaResp); err != nil {
			log.Printf("❌ [LLM] Failed to parse Ollama JSON: %v", err)
			return "", err
		}

		// Extract message content
		if message, ok := ollamaResp["message"].(map[string]interface{}); ok {
			if content, ok := message["content"].(string); ok {
				log.Printf("✅ [LLM] Successfully extracted content from Ollama response")
				return content, nil
			}
		}
		log.Printf("❌ [LLM] Could not extract content from Ollama response")
		return "", fmt.Errorf("could not extract content from Ollama response")
	} else {
		// OpenAI/Anthropic response format
		log.Printf("🌐 [LLM] Parsing %s response", c.config.LLMProvider)
		var llmResp LLMResponse
		if err := json.Unmarshal(body, &llmResp); err != nil {
			log.Printf("❌ [LLM] Failed to parse %s JSON: %v", c.config.LLMProvider, err)
			return "", err
		}

		// Check for API errors
		if llmResp.Error != nil {
			log.Printf("❌ [LLM] API error: %s", llmResp.Error.Message)
			return "", fmt.Errorf("LLM API error: %s", llmResp.Error.Message)
		}

		// Extract content
		if len(llmResp.Choices) == 0 {
			log.Printf("❌ [LLM] No choices in response")
			return "", fmt.Errorf("no response from LLM")
		}

		log.Printf("✅ [LLM] Successfully extracted content from %s response", c.config.LLMProvider)
		return llmResp.Choices[0].Message.Content, nil
	}
}

// normalizeOllamaURL ensures the provided base URL includes the /api/chat endpoint.
// Accepts either http://host:11434 or http://host:11434/api/chat and returns the full endpoint.
func normalizeOllamaURL(base string) string {
	trimmed := strings.TrimRight(base, "/")
	if strings.HasSuffix(trimmed, "/api/chat") {
		return trimmed
	}
	if strings.HasSuffix(trimmed, "/api") {
		return trimmed + "/chat"
	}
	return trimmed + "/api/chat"
}

func (c *LLMClient) getMockResponse(prompt string) (string, error) {
	// Simple mock responses based on prompt content
	if containsString(prompt, "WriteEmail") {
		return `{
  "task": "WriteEmail",
  "preconditions": ["not email_written"],
  "subtasks": ["DraftEmail", "ReviewEmail", "SendEmail"]
}`, nil
	}

	if containsString(prompt, "CreateReport") {
		return `{
  "task": "CreateReport",
  "preconditions": ["not report_created"],
  "subtasks": ["GatherData", "AnalyzeData", "WriteReport", "FormatReport"]
}`, nil
	}

	// Default response
	return `{
  "task": "GenericTask",
  "preconditions": ["not task_completed"],
  "subtasks": ["PrepareTask", "ExecuteTask", "VerifyTask"]
}`, nil
}

func (c *LLMClient) callLLMRealWithContext(ctx context.Context, prompt string) (string, error) {
	log.Printf("🌐 [LLM] Making API call to provider: %s", c.config.LLMProvider)
	log.Printf("🌐 [LLM] Timeout: %v", c.httpClient.Timeout)

	// Determine the API endpoint based on provider
	var apiURL string
	var apiKey string

	switch c.config.LLMProvider {
	case "openai":
		apiURL = "https://api.openai.com/v1/chat/completions"
		apiKey = c.config.LLMAPIKey
		log.Printf("🌐 [LLM] Using OpenAI API")
	case "anthropic":
		apiURL = "https://api.anthropic.com/v1/messages"
		apiKey = c.config.LLMAPIKey
		log.Printf("🌐 [LLM] Using Anthropic API")
	case "local", "ollama":
		// For local models, use Ollama. Allow override via settings["ollama_url"].
		if url, ok := c.config.Settings["ollama_url"]; ok && strings.TrimSpace(url) != "" {
			apiURL = normalizeOllamaURL(strings.TrimSpace(url))
		} else {
			apiURL = "http://localhost:11434/api/chat"
		}
		apiKey = ""
		log.Printf("🌐 [LLM] Using Ollama local API at %s", apiURL)
	default:
		log.Printf("❌ [LLM] Unsupported provider: %s", c.config.LLMProvider)
		return "", fmt.Errorf("unsupported LLM provider: %s", c.config.LLMProvider)
	}

	// Prepare the request based on provider
	var jsonData []byte
	var err error

	if c.config.LLMProvider == "local" {
		// Ollama uses a different format
		ollamaRequest := map[string]interface{}{
			"model": c.getModelName(),
			"messages": []map[string]string{
				{"role": "user", "content": prompt},
			},
			"stream": false,
		}
		jsonData, err = json.Marshal(ollamaRequest)
	} else {
		// OpenAI/Anthropic format
		request := map[string]interface{}{
			"model": c.getModelName(),
			"messages": []map[string]string{
				{"role": "user", "content": prompt},
			},
			"max_tokens": 1000,
		}
		jsonData, err = json.Marshal(request)
	}

	if err != nil {
		log.Printf("❌ [LLM] Failed to marshal request: %v", err)
		return "", fmt.Errorf("failed to marshal request: %v", err)
	}

	// Create HTTP request with context
	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewBuffer(jsonData))
	if err != nil {
		log.Printf("❌ [LLM] Failed to create request: %v", err)
		return "", fmt.Errorf("failed to create request: %v", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	// Make the request with context
	client := &http.Client{Timeout: c.httpClient.Timeout}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("❌ [LLM] Request failed: %v", err)
		return "", fmt.Errorf("request failed: %v", err)
	}
	defer resp.Body.Close()

	// Check for context cancellation
	select {
	case <-ctx.Done():
		log.Printf("❌ [LLM] Request cancelled: %v", ctx.Err())
		return "", ctx.Err()
	default:
	}

	// Read response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("❌ [LLM] Failed to read response: %v", err)
		return "", fmt.Errorf("failed to read response: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		log.Printf("❌ [LLM] API returned error: %d %s", resp.StatusCode, string(body))
		return "", fmt.Errorf("API returned error: %d %s", resp.StatusCode, string(body))
	}

	// Parse response based on provider
	if c.config.LLMProvider == "local" {
		// Ollama response format
		var ollamaResp map[string]interface{}
		if err := json.Unmarshal(body, &ollamaResp); err != nil {
			log.Printf("❌ [LLM] Failed to parse Ollama response: %v", err)
			return "", fmt.Errorf("failed to parse response: %v", err)
		}

		if message, ok := ollamaResp["message"].(map[string]interface{}); ok {
			if content, ok := message["content"].(string); ok {
				return content, nil
			}
		}
		return "", fmt.Errorf("unexpected Ollama response format")
	} else {
		// OpenAI/Anthropic response format
		var apiResp map[string]interface{}
		if err := json.Unmarshal(body, &apiResp); err != nil {
			log.Printf("❌ [LLM] Failed to parse API response: %v", err)
			return "", fmt.Errorf("failed to parse response: %v", err)
		}

		if choices, ok := apiResp["choices"].([]interface{}); ok && len(choices) > 0 {
			if choice, ok := choices[0].(map[string]interface{}); ok {
				if message, ok := choice["message"].(map[string]interface{}); ok {
					if content, ok := message["content"].(string); ok {
						return content, nil
					}
				}
			}
		}
		return "", fmt.Errorf("unexpected API response format")
	}
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && s[:len(substr)] == substr ||
		len(s) > len(substr) && containsString(s[1:], substr)
}
