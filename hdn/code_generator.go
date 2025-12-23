package main

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"
)

// CodeGenerator handles generating executable code using Ollama
type CodeGenerator struct {
	llmClient   *LLMClient
	codeStorage *CodeStorage
}

// CodeGenerationRequest represents a request to generate code
type CodeGenerationRequest struct {
	TaskName    string            `json:"task_name"`
	Description string            `json:"description"`
	Language    string            `json:"language"`
	Context     map[string]string `json:"context"`
	Tags        []string          `json:"tags"`
	Executable  bool              `json:"executable"`
	Tools       []Tool            `json:"tools,omitempty"`        // Available tools to use
	ToolAPIURL  string            `json:"tool_api_url,omitempty"` // Base URL for tool API
	HighPriority bool             `json:"high_priority"`          // true for user requests, false for background tasks
}

// CodeGenerationResponse represents the response from code generation
type CodeGenerationResponse struct {
	Code        *GeneratedCode `json:"code"`
	Success     bool           `json:"success"`
	Error       string         `json:"error,omitempty"`
	Suggestions []string       `json:"suggestions,omitempty"`
}

func NewCodeGenerator(llmClient *LLMClient, codeStorage *CodeStorage) *CodeGenerator {
	return &CodeGenerator{
		llmClient:   llmClient,
		codeStorage: codeStorage,
	}
}

// GenerateCode generates executable code for a given task
func (cg *CodeGenerator) GenerateCode(req *CodeGenerationRequest) (*CodeGenerationResponse, error) {
	// Build a code generation prompt
	prompt := cg.buildCodeGenerationPrompt(req)

	// Debug: log the exact LLM prompt used for code generation (truncated to avoid log flooding)
	if p := strings.TrimSpace(prompt); p != "" {
		max := 4000
		if len(p) > max {
			log.Printf("📝 [CODEGEN] LLM prompt (truncated %d/%d chars):\n%s...", max, len(p), p[:max])
		} else {
			log.Printf("📝 [CODEGEN] LLM prompt (%d chars):\n%s", len(p), p)
		}
	}

	// Call LLM to generate code with priority
	// Use a longer timeout for code generation (10 minutes) to handle backlog situations
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	priority := PriorityLow
	if req.HighPriority {
		priority = PriorityHigh
	}
	response, err := cg.llmClient.callLLMWithContextAndPriority(ctx, prompt, priority)
	if err != nil {
		return &CodeGenerationResponse{
			Success: false,
			Error:   fmt.Sprintf("Failed to generate code: %v", err),
		}, nil
	}

	// Extract code from response (with retry if wrong language detected)
	var code string
	maxRetries := 2
	for retry := 0; retry < maxRetries; retry++ {
		var extractErr error
		code, extractErr = cg.extractCodeFromResponse(response, req.Language)
		if extractErr != nil {
			// If wrong language detected, retry code generation with stronger prompt
			if strings.Contains(extractErr.Error(), "LLM generated") && strings.Contains(extractErr.Error(), "when") && retry < maxRetries-1 {
				log.Printf("🔄 [CODEGEN] Wrong language detected, retrying code generation (attempt %d/%d)", retry+1, maxRetries)
				// Enhance prompt with stronger language requirement
				enhancedPrompt := prompt + "\n\n🚨🚨🚨 CRITICAL REMINDER: You MUST generate " + req.Language + " code ONLY! The previous attempt generated the wrong language and was rejected! 🚨🚨🚨"
				ctx := context.Background()
				priority := PriorityLow
				if req.HighPriority {
					priority = PriorityHigh
				}
				response, err = cg.llmClient.callLLMWithContextAndPriority(ctx, enhancedPrompt, priority)
				if err != nil {
					return &CodeGenerationResponse{
						Success: false,
						Error:   fmt.Sprintf("Failed to generate code: %v", err),
					}, nil
				}
				continue // Retry extraction
			}
			return &CodeGenerationResponse{
				Success: false,
				Error:   fmt.Sprintf("Failed to extract code: %v", extractErr),
			}, nil
		}
		break // Success
	}

	// Minimal cleanup: only remove markdown code fences (safe operation)
	// Do NOT post-process imports or modify code structure - let the LLM generate correct code
	// If there are issues, the validation/fix mechanism will handle them
	code = cg.cleanGeneratedCode(code, req.Language)

	// Create GeneratedCode object
	generatedCode := &GeneratedCode{
		ID:          fmt.Sprintf("code_%d", time.Now().UnixNano()),
		TaskName:    req.TaskName,
		Description: req.Description,
		Language:    req.Language,
		Code:        code,
		Context:     req.Context,
		CreatedAt:   time.Now(),
		Tags:        req.Tags,
		Executable:  req.Executable,
	}

	// Store in Redis
	err = cg.codeStorage.StoreCode(generatedCode)
	if err != nil {
		return &CodeGenerationResponse{
			Success: false,
			Error:   fmt.Sprintf("Failed to store code: %v", err),
		}, nil
	}

	// Generate suggestions for improvement
	suggestions := cg.generateSuggestions(generatedCode)

	return &CodeGenerationResponse{
		Code:        generatedCode,
		Success:     true,
		Suggestions: suggestions,
	}, nil
}

// cleanGeneratedCode removes test cases and error handling from generated code
func (cg *CodeGenerator) cleanGeneratedCode(code, language string) string {
	// Be conservative: only strip surrounding markdown code fences if present.
	trimmed := strings.TrimSpace(code)
	if strings.HasPrefix(trimmed, "```") {
		// Remove the starting fence (optionally with language)
		newlineIdx := strings.Index(trimmed, "\n")
		if newlineIdx != -1 {
			trimmed = trimmed[newlineIdx+1:]
		} else {
			// Single-line fence; return as-is
			return code
		}
	}
	if strings.HasSuffix(trimmed, "```") {
		trimmed = strings.TrimSuffix(trimmed, "```")
	}
	cleaned := strings.TrimSpace(trimmed)

	// No post-processing - let the LLM generate correct code
	// If there are issues, the validation/fix mechanism will handle them
	return cleaned
}

// buildCodeGenerationPrompt creates a prompt for code generation
func (cg *CodeGenerator) buildCodeGenerationPrompt(req *CodeGenerationRequest) string {
	log.Printf("📝 [CODEGEN] Building prompt for task: %s, language: %s, description length: %d", req.TaskName, req.Language, len(req.Description))
	log.Printf("📝 [CODEGEN] Description (first 200 chars): %s", func() string {
		if len(req.Description) > 200 {
			return req.Description[:200]
		}
		return req.Description
	}())
	
	// Special case for daily_summary - generate a simple placeholder
	if strings.EqualFold(req.TaskName, "daily_summary") {
		return `Generate a simple Python script that prints a placeholder message for daily summary generation.

This is a placeholder task - the actual daily summary will be generated by the system using real data.
Just print a message indicating this is a placeholder.

Code:`
	}

	// Special handling for very simple print tasks - make the prompt extremely explicit
	// Check for simple print patterns: "print X", "create program that prints X", etc.
	// IMPORTANT: Use req.Description directly, not cleanDesc, because we want to detect simple tasks
	// before any JSON cleaning happens
	descLower := strings.ToLower(req.Description)
	log.Printf("📝 [CODEGEN] Checking for simple print task - descLower contains 'print': %v", strings.Contains(descLower, "print"))
	isSimplePrintTask := (strings.Contains(descLower, "print") || strings.Contains(descLower, "prints")) &&
		!strings.Contains(descLower, "matrix") &&
		!strings.Contains(descLower, "json") &&
		!strings.Contains(descLower, "read") &&
		!strings.Contains(descLower, "file") &&
		!strings.Contains(descLower, "calculate") &&
		!strings.Contains(descLower, "process") &&
		!strings.Contains(descLower, "parse") &&
		!strings.Contains(descLower, "operation") &&
		!strings.Contains(descLower, "chained") &&
		!strings.Contains(descLower, "sequential")
	
	if isSimplePrintTask && req.Language == "go" {
		// Extract what to print from description - try both single and double quotes
		// Also handle patterns like "prints 'X'", "print 'X'", "prints X", etc.
		printTarget := ""
		
		// Try to extract from single quotes first
		if strings.Contains(req.Description, "'") {
			// Find text between single quotes
			startIdx := strings.Index(req.Description, "'")
			if startIdx >= 0 {
				endIdx := strings.Index(req.Description[startIdx+1:], "'")
				if endIdx >= 0 {
					printTarget = req.Description[startIdx+1 : startIdx+1+endIdx]
				}
			}
		}
		
		// If no single quotes, try double quotes
		if printTarget == "" && strings.Contains(req.Description, "\"") {
			startIdx := strings.Index(req.Description, "\"")
			if startIdx >= 0 {
				endIdx := strings.Index(req.Description[startIdx+1:], "\"")
				if endIdx >= 0 {
					printTarget = req.Description[startIdx+1 : startIdx+1+endIdx]
				}
			}
		}
		
		// If still no target, try to extract after "prints" or "print"
		if printTarget == "" {
			descLower := strings.ToLower(req.Description)
			if idx := strings.Index(descLower, "prints "); idx >= 0 {
				afterPrints := req.Description[idx+7:] // Skip "prints "
				// Take the next word or phrase
				afterPrints = strings.TrimSpace(afterPrints)
				// Remove common words like "just", "the", "a", "an"
				words := strings.Fields(afterPrints)
				var filteredWords []string
				skipWords := map[string]bool{"just": true, "the": true, "a": true, "an": true, "that": true}
				for _, word := range words {
					if !skipWords[strings.ToLower(word)] {
						filteredWords = append(filteredWords, word)
					}
				}
				if len(filteredWords) > 0 {
					// Take up to 5 words as the print target
					maxWords := 5
					if len(filteredWords) < maxWords {
						maxWords = len(filteredWords)
					}
					printTarget = strings.Join(filteredWords[:maxWords], " ")
				}
			}
		}
		
		// If we found a print target, use the simple prompt
		if printTarget != "" {
			log.Printf("📝 [CODEGEN] Detected simple print task - generating explicit prompt for: %s (language: %s)", printTarget, req.Language)
			lang := req.Language
			if lang == "" {
				// If language is not specified, don't use the simple prompt - let the normal prompt handle it
				log.Printf("⚠️ [CODEGEN] Language not specified for simple print task, using normal prompt")
			} else if lang == "go" {
				return fmt.Sprintf(`Create a Go program that prints: %s

Return only the Go code in a markdown code block.`, printTarget)
			} else if lang == "python" || lang == "py" {
				return fmt.Sprintf(`Create a Python program that prints: %s

Return only the Python code in a markdown code block.`, printTarget)
			}
		}
	}

	// Simple, direct prompt - let the LLM handle everything
	// Only clean up obvious JSON blobs in description
	cleanDesc := req.Description
	if strings.Contains(cleanDesc, `{"interpreted_at"`) {
		// Try to extract description from JSON blob
		re := regexp.MustCompile(`"description":"([^"]*)"`)
		matches := re.FindStringSubmatch(cleanDesc)
		if len(matches) > 1 {
			cleanDesc = matches[1]
		}
	}

	contextStr := ""
	if len(req.Context) > 0 {
		contextStr = "\n\nContext:\n"
		for k, v := range req.Context {
			contextStr += fmt.Sprintf("- %s: %s\n", k, v)
		}
	}

	// Add instructions for tool usage if task mentions tools
	toolInstructions := ""
	descLowerForTools := strings.ToLower(cleanDesc)
	if strings.Contains(descLowerForTools, "tool_") || strings.Contains(descLowerForTools, "use tool") {
		toolInstructions = "\n\n🚨 CRITICAL: If the task mentions using a tool (like tool_http_get, tool_html_scraper, etc.), DO NOT import it as a Python module. Instead, call the tool via HTTP API:\n"
		toolInstructions += "- Get HDN_URL from environment: `hdn_url = os.getenv('HDN_URL', 'http://localhost:8080')`\n"
		toolInstructions += "- Call tool via POST request: `requests.post(f'{hdn_url}/api/v1/tools/{tool_id}/invoke', json={params})`\n"
		toolInstructions += "- Example for tool_http_get: `requests.post(f'{hdn_url}/api/v1/tools/tool_http_get/invoke', json={'url': 'https://example.com'})`\n"
		toolInstructions += "- Make sure to import `requests` and `os` modules, and handle the response JSON properly.\n"
	}

	return fmt.Sprintf(`Generate %s code for this task:

%s%s%s

Return only the code in a markdown code block.`, req.Language, cleanDesc, contextStr, toolInstructions)
}

// extractCodeFromResponse extracts code from the LLM response
func (cg *CodeGenerator) extractCodeFromResponse(response, language string) (string, error) {
	// Look for code blocks in the response
	codeBlockStart := fmt.Sprintf("```%s", language)
	codeBlockEnd := "```"

	startIdx := strings.Index(response, codeBlockStart)
	if startIdx == -1 {
		// Try generic code block
		startIdx = strings.Index(response, "```")
		if startIdx == -1 {
			// Last resort: check if the entire response is code (no markdown)
			trimmed := strings.TrimSpace(response)
			// If response looks like code (has imports, functions, etc.), use it directly
			if strings.Contains(trimmed, "package ") || strings.Contains(trimmed, "import ") ||
				strings.Contains(trimmed, "def ") || strings.Contains(trimmed, "func ") ||
				strings.Contains(trimmed, "class ") {
				log.Printf("⚠️ [CODEGEN] No code block found, but response looks like code - using entire response")
				return trimmed, nil
			}
			log.Printf("❌ [CODEGEN] No code block found in response (first 500 chars): %s",
				func() string {
					if len(response) > 500 {
						return response[:500]
					}
					return response
				}())
			return "", fmt.Errorf("no code block found in response")
		}
		// Skip the ```
		startIdx += 3
	} else {
		// Skip the ```language
		startIdx += len(codeBlockStart)
	}

	// Find the end of the code block
	endIdx := strings.Index(response[startIdx:], codeBlockEnd)
	if endIdx == -1 {
		// Try to extract everything after the code block start as code
		code := strings.TrimSpace(response[startIdx:])
		if code != "" {
			log.Printf("⚠️ [CODEGEN] No closing code block found, but extracted code from start marker (first 200 chars): %s",
				func() string {
					if len(code) > 200 {
						return code[:200]
					}
					return code
				}())
			return code, nil
		}
		log.Printf("❌ [CODEGEN] No closing code block found (first 500 chars after start): %s",
			func() string {
				if len(response[startIdx:]) > 500 {
					return response[startIdx : startIdx+500]
				}
				return response[startIdx:]
			}())
		return "", fmt.Errorf("no closing code block found")
	}

	// Extract the code
	code := strings.TrimSpace(response[startIdx : startIdx+endIdx])

	if code == "" {
		return "", fmt.Errorf("extracted code is empty")
	}

	// CRITICAL: Validate that extracted code matches the requested language
	// If JavaScript was requested but Python code was generated, reject it
	if (language == "javascript" || language == "js") {
		// Check for Python syntax in JavaScript code
		if strings.Contains(code, "import ") && (strings.Contains(code, "def ") || strings.Contains(code, "import json") || strings.Contains(code, "import statistics")) {
			// This looks like Python code, not JavaScript
			log.Printf("❌ [CODEGEN] LLM generated Python code when JavaScript was requested!")
			log.Printf("❌ [CODEGEN] Code preview (first 200 chars): %s", func() string {
				if len(code) > 200 {
					return code[:200]
				}
				return code
			}())
			return "", fmt.Errorf("LLM generated Python code when JavaScript was requested - code contains Python syntax (import statements with def, import json, import statistics)")
		}
		// Check for other Python indicators
		if strings.Contains(code, "def ") || strings.Contains(code, "if __name__") || strings.Contains(code, "print(") {
			log.Printf("❌ [CODEGEN] LLM generated Python code when JavaScript was requested!")
			return "", fmt.Errorf("LLM generated Python code when JavaScript was requested - code contains Python syntax (def, if __name__, print)")
		}
	}

	// Filter out code from wrong language - if we asked for Python, remove Go code blocks
	if language == "python" || language == "py" {
		// Remove Go code blocks (package main, func main, etc.)
		lines := strings.Split(code, "\n")
		var filteredLines []string
		inGoBlock := false
		for _, line := range lines {
			lineTrimmed := strings.TrimSpace(line)
			// Detect Go code blocks
			if strings.HasPrefix(lineTrimmed, "package ") ||
				strings.HasPrefix(lineTrimmed, "func main()") ||
				(strings.Contains(lineTrimmed, "import (") && strings.Contains(code, "package main")) {
				inGoBlock = true
				continue
			}
			// If we're in a Go block, skip until we see Python code
			if inGoBlock {
				// Check if this looks like Python code
				if (strings.HasPrefix(lineTrimmed, "import ") && !strings.Contains(lineTrimmed, "(")) ||
					strings.HasPrefix(lineTrimmed, "def ") ||
					strings.HasPrefix(lineTrimmed, "class ") ||
					strings.HasPrefix(lineTrimmed, "#") {
					inGoBlock = false
					filteredLines = append(filteredLines, line)
				}
				continue
			}
			filteredLines = append(filteredLines, line)
		}
		if len(filteredLines) > 0 {
			code = strings.Join(filteredLines, "\n")
			log.Printf("⚠️ [CODEGEN] Filtered out Go code from Python response")
		}
	} else if language == "go" {
		// For Go code, we should NOT filter anything - Go single-line imports like "import \"fmt\"" 
		// are valid and should not be confused with Python imports
		// The only time we'd filter is if there's a clear Python code block mixed in,
		// but that's very rare and the LLM should generate correct code
		// So we skip filtering for Go to avoid false positives
	}

	return code, nil
}

// generateSuggestions creates suggestions for improving the generated code
func (cg *CodeGenerator) generateSuggestions(code *GeneratedCode) []string {
	var suggestions []string

	// Language-specific suggestions
	switch code.Language {
	case "go":
		suggestions = append(suggestions, "Consider adding unit tests")
		suggestions = append(suggestions, "Add proper error handling with custom error types")
		suggestions = append(suggestions, "Consider using interfaces for better testability")
	case "python":
		suggestions = append(suggestions, "Add type hints for better code clarity")
		suggestions = append(suggestions, "Consider using dataclasses or pydantic for data structures")
		suggestions = append(suggestions, "Add docstrings following PEP 257")
	case "javascript", "typescript":
		suggestions = append(suggestions, "Add JSDoc comments for better documentation")
		suggestions = append(suggestions, "Consider using TypeScript for better type safety")
		suggestions = append(suggestions, "Add proper error handling with try-catch blocks")
	}

	// General suggestions
	suggestions = append(suggestions, "Add logging for debugging and monitoring")
	suggestions = append(suggestions, "Consider adding configuration management")
	suggestions = append(suggestions, "Add input validation and sanitization")

	return suggestions
}

// SearchCode searches for previously generated code
func (cg *CodeGenerator) SearchCode(query string, language string, tags []string) ([]CodeSearchResult, error) {
	return cg.codeStorage.SearchCode(query, language, tags)
}

// GetCode retrieves code by ID
func (cg *CodeGenerator) GetCode(id string) (*GeneratedCode, error) {
	return cg.codeStorage.GetCode(id)
}

// ListAllCode lists all generated code
func (cg *CodeGenerator) ListAllCode() ([]*GeneratedCode, error) {
	return cg.codeStorage.ListAllCode()
}

// DeleteCode removes code by ID
func (cg *CodeGenerator) DeleteCode(id string) error {
	return cg.codeStorage.DeleteCode(id)
}

// GenerateCodeFromTask generates code based on an existing HTN task
func (cg *CodeGenerator) GenerateCodeFromTask(taskName, description string, context map[string]string) (*CodeGenerationResponse, error) {
	// Determine language from context or default to Go
	language := "go"
	if lang, exists := context["language"]; exists {
		language = lang
	}

	// Extract tags from context
	var tags []string
	if taskTags, exists := context["tags"]; exists {
		tags = strings.Split(taskTags, ",")
		for i, tag := range tags {
			tags[i] = strings.TrimSpace(tag)
		}
	}

	req := &CodeGenerationRequest{
		TaskName:    taskName,
		Description: description,
		Language:    language,
		Context:     context,
		Tags:        tags,
		Executable:  true,
	}

	return cg.GenerateCode(req)
}
