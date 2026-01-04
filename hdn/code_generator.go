package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"regexp"
	"runtime"
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
	TaskName     string            `json:"task_name"`
	Description  string            `json:"description"`
	Language     string            `json:"language"`
	Context      map[string]string `json:"context"`
	Tags         []string          `json:"tags"`
	Executable   bool              `json:"executable"`
	Tools        []Tool            `json:"tools,omitempty"`        // Available tools to use
	ToolAPIURL   string            `json:"tool_api_url,omitempty"` // Base URL for tool API
	HighPriority bool              `json:"high_priority"`          // true for user requests, false for background tasks
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

	// Truncate prompt if it's too large to prevent context size errors
	// Most LLMs have context limits (e.g., 128K tokens ≈ 500K chars)
	// Use a conservative limit of 300K chars to leave room for response
	const maxPromptSize = 300 * 1024 // 300KB
	if len(prompt) > maxPromptSize {
		log.Printf("⚠️ [CODEGEN] Prompt size (%d chars) exceeds limit (%d chars), truncating...", len(prompt), maxPromptSize)
		prompt = cg.truncatePrompt(prompt, maxPromptSize)
		log.Printf("📝 [CODEGEN] Prompt truncated to %d chars", len(prompt))
	}

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
	// Add component information to context for token tracking
	ctx = WithComponent(ctx, "hdn-code-generator")
	priority := PriorityLow
	if req.HighPriority {
		priority = PriorityHigh
	}
	response, err := cg.llmClient.callLLMWithContextAndPriority(ctx, prompt, priority)
	if err != nil {
		// Check if error is due to context size
		errStr := err.Error()
		if strings.Contains(errStr, "Context size has been exceeded") || 
		   strings.Contains(errStr, "context size") || 
		   strings.Contains(errStr, "context_length_exceeded") ||
		   strings.Contains(errStr, "maximum context length") {
			log.Printf("❌ [CODEGEN] Context size error detected - prompt may still be too large")
			return &CodeGenerationResponse{
				Success: false,
				Error:   fmt.Sprintf("Context size exceeded. The task description or context is too large. Please simplify the request or break it into smaller tasks. Original error: %v", err),
			}, nil
		}
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
				// Add component information to context for token tracking
				ctx = WithComponent(ctx, "hdn-code-generator")
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
	code = cg.cleanGeneratedCode(code, req.Language, req.ToolAPIURL)

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
func (cg *CodeGenerator) cleanGeneratedCode(code, language string, toolAPIURL string) string {
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

	// For Go code, ensure it starts with package declaration
	if language == "go" {
		cleaned = cg.ensureGoPackageDeclaration(cleaned)
	}

	// CRITICAL: Replace localhost with host.docker.internal in generated code ONLY for Docker execution
	// This is a safety measure in case the LLM ignores instructions or uses hardcoded localhost
	// For SSH execution, keep localhost as-is
	cleaned = cg.fixLocalhostReferences(cleaned, language, toolAPIURL)

	// No other post-processing - let the LLM generate correct code
	// If there are issues, the validation/fix mechanism will handle them
	return cleaned
}

// fixLocalhostReferences replaces localhost with host.docker.internal in generated code
// ONLY if using Docker execution. For SSH execution, localhost is kept as-is.
// This ensures Docker containers can reach the host HDN server, but SSH execution uses localhost correctly
func (cg *CodeGenerator) fixLocalhostReferences(code, language string, toolAPIURL string) string {
	originalCode := code

	// Determine execution method - only replace localhost for Docker execution
	executionMethod := strings.TrimSpace(os.Getenv("EXECUTION_METHOD"))
	useDocker := executionMethod == "docker" || (executionMethod == "" && !strings.Contains(toolAPIURL, "localhost"))
	// If ToolAPIURL contains host.docker.internal, we're using Docker
	if strings.Contains(toolAPIURL, "host.docker.internal") {
		useDocker = true
	}
	// If ToolAPIURL contains localhost and execution method is ssh, don't replace
	if strings.Contains(toolAPIURL, "localhost") && executionMethod == "ssh" {
		useDocker = false
	}

	// Skip replacement for SSH execution
	if !useDocker {
		return code
	}

	// Pattern 1: Python os.getenv with localhost default (single or double quotes)
	// Matches: os.getenv('HDN_URL', 'http://localhost:8081') or os.getenv("HDN_URL", "http://localhost:8081")
	// Use separate patterns for single and double quotes since Go doesn't support backreferences in patterns
	pattern1Single := regexp.MustCompile(`os\.getenv\('HDN_URL',\s*'http://localhost:(\d+)'\)`)
	code = pattern1Single.ReplaceAllStringFunc(code, func(match string) string {
		submatches := pattern1Single.FindStringSubmatch(match)
		if len(submatches) >= 2 {
			port := submatches[1]
			return fmt.Sprintf("os.getenv('HDN_URL', 'http://host.docker.internal:%s')", port)
		}
		return match
	})
	pattern1Double := regexp.MustCompile(`os\.getenv\("HDN_URL",\s*"http://localhost:(\d+)"\)`)
	code = pattern1Double.ReplaceAllStringFunc(code, func(match string) string {
		submatches := pattern1Double.FindStringSubmatch(match)
		if len(submatches) >= 2 {
			port := submatches[1]
			return fmt.Sprintf(`os.getenv("HDN_URL", "http://host.docker.internal:%s")`, port)
		}
		return match
	})

	// Pattern 2: Go os.Getenv with localhost fallback
	// Matches: hdnURL := os.Getenv("HDN_URL"); if hdnURL == "" { hdnURL = "http://localhost:8081" }
	pattern2 := regexp.MustCompile(`localhost:(\d+)`)
	code = pattern2.ReplaceAllString(code, `host.docker.internal:$1`)

	// Pattern 3: Direct string literals with localhost:8081 or localhost:8080
	// Matches: "http://localhost:8081" or 'http://localhost:8080'
	// Note: Go's regexp (RE2) doesn't support backreferences, so we use separate patterns for single and double quotes
	pattern3Double := regexp.MustCompile(`(")(http://)localhost:(8081|8080)"`)
	code = pattern3Double.ReplaceAllString(code, `${1}${2}host.docker.internal:$3"`)
	pattern3Single := regexp.MustCompile(`(')(http://)localhost:(8081|8080)'`)
	code = pattern3Single.ReplaceAllString(code, `${1}${2}host.docker.internal:$3'`)

	// Pattern 4: f-string with localhost (Python)
	// Matches: f'http://localhost:8081' or f"http://localhost:8080"
	// Note: Go's regexp (RE2) doesn't support backreferences, so we use separate patterns for single and double quotes
	pattern4Double := regexp.MustCompile(`f(")(http://)localhost:(8081|8080)"`)
	code = pattern4Double.ReplaceAllString(code, `f${1}${2}host.docker.internal:$3"`)
	pattern4Single := regexp.MustCompile(`f(')(http://)localhost:(8081|8080)'`)
	code = pattern4Single.ReplaceAllString(code, `f${1}${2}host.docker.internal:$3'`)

	// Pattern 5: Variable assignments
	// Matches: hdn_url = "http://localhost:8081" or url = 'http://localhost:8080'
	// Note: Go's regexp (RE2) doesn't support backreferences, so we use separate patterns for single and double quotes
	pattern5Double := regexp.MustCompile(`(\w+)\s*=\s*(")(http://)localhost:(8081|8080)"`)
	code = pattern5Double.ReplaceAllString(code, `${1} = ${2}${3}host.docker.internal:$4"`)
	pattern5Single := regexp.MustCompile(`(\w+)\s*=\s*(')(http://)localhost:(8081|8080)'`)
	code = pattern5Single.ReplaceAllString(code, `${1} = ${2}${3}host.docker.internal:$4'`)

	// Log if we made any changes
	if code != originalCode {
		log.Printf("🔧 [CODEGEN] Fixed localhost references in generated %s code (replaced with host.docker.internal)", language)
	}

	return code
}

// ensureGoPackageDeclaration ensures Go code starts with a package declaration
func (cg *CodeGenerator) ensureGoPackageDeclaration(code string) string {
	trimmed := strings.TrimSpace(code)
	if trimmed == "" {
		return code
	}

	// Check if code already starts with package declaration
	lines := strings.Split(trimmed, "\n")
	if len(lines) > 0 {
		firstLine := strings.TrimSpace(lines[0])
		// Check if first line is a package declaration
		if strings.HasPrefix(firstLine, "package ") {
			// Already has package declaration, return as-is
			return trimmed
		}
	}

	// Check if code contains "import" but no "package" - this is the error case
	if strings.Contains(trimmed, "import") && !strings.Contains(trimmed, "package ") {
		log.Printf("⚠️ [CODEGEN] Go code missing package declaration, adding 'package main'")
		// Add package main at the beginning
		return "package main\n\n" + trimmed
	}

	// If code doesn't have package or import, it might be incomplete
	// But we'll let validation catch that - just ensure package is present if imports are
	return trimmed
}

// truncatePrompt intelligently truncates a prompt to fit within size limits
// It preserves the most important parts (task description, language requirements) and truncates less critical sections
func (cg *CodeGenerator) truncatePrompt(prompt string, maxSize int) string {
	if len(prompt) <= maxSize {
		return prompt
	}

	// Strategy: Keep the beginning (task description, language requirements) and truncate from the middle/end
	// Most important parts are usually at the beginning
	// Reserve space for the ending (code block tag)
	const headerReserve = 2000  // Reserve for header (task description, language enforcement)
	const footerReserve = 500    // Reserve for footer (code block tag)
	availableSize := maxSize - headerReserve - footerReserve

	if availableSize <= 0 {
		// If we can't fit even the essentials, just truncate directly
		return prompt[:maxSize] + "\n\n[Prompt truncated due to size limits]"
	}

	// Extract header (first headerReserve chars)
	header := prompt
	if len(header) > headerReserve {
		header = prompt[:headerReserve]
	}

	// Extract footer (last footerReserve chars)
	footer := ""
	if len(prompt) > footerReserve {
		footer = prompt[len(prompt)-footerReserve:]
	}

	// If header + footer already exceed maxSize, just truncate
	if len(header)+len(footer) >= maxSize {
		return prompt[:maxSize] + "\n\n[Prompt truncated due to size limits]"
	}

	// Try to extract a middle section that fits
	middleSize := maxSize - len(header) - len(footer) - 100 // 100 chars for truncation message
	if middleSize > 0 && len(prompt) > len(header)+len(footer) {
		// Extract a portion from the middle
		middleStart := len(header)
		middleEnd := len(prompt) - len(footer)
		if middleEnd > middleStart {
			middle := prompt[middleStart:middleEnd]
			if len(middle) > middleSize {
				// Truncate middle section
				middle = middle[:middleSize] + "\n\n[... context truncated due to size limits ...]"
			}
			return header + middle + footer
		}
	}

	// Fallback: just truncate
	return prompt[:maxSize] + "\n\n[Prompt truncated due to size limits]"
}

// filterRelevantToolsForTask filters tools based on task relevance to reduce prompt size
// For knowledge base queries, only include knowledge-related tools
// For other tasks, include tools that match keywords in the task description
func (cg *CodeGenerator) filterRelevantToolsForTask(tools []Tool, taskName, description string) []Tool {
	if len(tools) <= 5 {
		// If we have 5 or fewer tools, include all of them
		return tools
	}

	taskLower := strings.ToLower(taskName)
	descLower := strings.ToLower(description)
	combined := taskLower + " " + descLower

	// Check if this is a knowledge base query task
	isKnowledgeBaseQuery := strings.Contains(combined, "neo4j") ||
		strings.Contains(combined, "knowledge base") ||
		strings.Contains(combined, "knowledge graph") ||
		strings.Contains(combined, "query knowledge") ||
		strings.Contains(combined, "cypher") ||
		strings.Contains(combined, "graph database") ||
		strings.Contains(combined, "concept") ||
		strings.Contains(combined, "retrieve from") ||
		strings.Contains(combined, "fetch from") ||
		strings.Contains(combined, "get data from")

	// Keywords that suggest specific tool usage
	toolKeywords := map[string][]string{
		"mcp_query_neo4j":           {"neo4j", "query", "cypher", "knowledge", "graph", "database", "knowledge base", "concept"},
		"mcp_get_concept":           {"concept", "get concept", "retrieve concept", "knowledge", "biology"},
		"mcp_find_related_concepts": {"related", "related concepts", "find related", "connections"},
		"mcp_search_weaviate":       {"weaviate", "search", "vector", "semantic", "similar", "episodes", "memories"},
		"tool_http_get":             {"http", "url", "fetch", "get", "request", "api", "endpoint", "download", "retrieve", "web"},
		"tool_html_scraper":         {"scrape", "html", "web", "website", "article", "news", "page", "parse html"},
		"tool_file_read":            {"read", "file", "load", "open", "readfile", "read file", "content", "text"},
		"tool_file_write":           {"write", "file", "save", "store", "output", "write file", "save file", "create file"},
		"tool_ls":                   {"list", "directory", "dir", "files", "ls", "list files", "directory listing"},
		"tool_exec":                 {"exec", "execute", "command", "shell", "run", "cmd", "system", "bash", "sh"},
		"tool_codegen":              {"generate", "code", "create", "write code", "generate code", "program", "script"},
		"tool_json_parse":           {"json", "parse", "parse json", "decode", "unmarshal"},
		"tool_text_search":          {"search", "find", "text", "pattern", "match", "grep", "filter"},
	}

	var relevant []Tool
	seen := make(map[string]bool)

	// For knowledge base queries, prioritize knowledge-related tools
	if isKnowledgeBaseQuery {
		// First, include knowledge-related tools
		knowledgeToolIDs := []string{"mcp_query_neo4j", "mcp_get_concept", "mcp_find_related_concepts", "mcp_search_weaviate"}
		for _, toolID := range knowledgeToolIDs {
			for _, tool := range tools {
				if tool.ID == toolID && !seen[tool.ID] {
					relevant = append(relevant, tool)
					seen[tool.ID] = true
					break
				}
			}
		}
		// Also include file tools (for saving results) and http_get (for API calls)
		utilityToolIDs := []string{"tool_file_write", "tool_http_get"}
		for _, toolID := range utilityToolIDs {
			for _, tool := range tools {
				if tool.ID == toolID && !seen[tool.ID] {
					relevant = append(relevant, tool)
					seen[tool.ID] = true
					break
				}
			}
		}
		log.Printf("📝 [CODEGEN] Knowledge base query detected - filtered to %d relevant tools", len(relevant))
		return relevant
	}

	// For other tasks, match tools based on keywords
	for _, tool := range tools {
		if seen[tool.ID] {
			continue
		}

		matched := false
		if keywords, ok := toolKeywords[tool.ID]; ok {
			for _, keyword := range keywords {
				if strings.Contains(combined, keyword) {
					relevant = append(relevant, tool)
					seen[tool.ID] = true
					matched = true
					break
				}
			}
		}

		if matched {
			continue
		}

		// Check if tool ID is explicitly mentioned
		if strings.Contains(combined, strings.ToLower(tool.ID)) {
			relevant = append(relevant, tool)
			seen[tool.ID] = true
			continue
		}

		// Check tool description for keyword matches
		toolDesc := strings.ToLower(tool.Description)
		if tool.Name != "" {
			toolDesc += " " + strings.ToLower(tool.Name)
		}
		// Check if any keywords from the task match the tool description
		commonKeywords := []string{"query", "neo4j", "http", "file", "read", "write", "exec", "docker", "code", "generate", "search", "scrape"}
		for _, keyword := range commonKeywords {
			if strings.Contains(combined, keyword) && strings.Contains(toolDesc, keyword) {
				relevant = append(relevant, tool)
				seen[tool.ID] = true
				break
			}
		}
	}

	// If we still have too many tools, limit to most relevant
	if len(relevant) > 15 {
		// Score tools by relevance
		type scoredTool struct {
			tool  Tool
			score int
		}
		scored := make([]scoredTool, len(relevant))
		for i, tool := range relevant {
			score := 0
			toolDesc := strings.ToLower(tool.Description + " " + tool.Name + " " + tool.ID)

			// Higher score for exact matches
			if strings.Contains(combined, strings.ToLower(tool.ID)) {
				score += 10
			}

			// Score based on keyword matches
			commonKeywords := []string{"query", "neo4j", "http", "file", "read", "write", "exec", "code", "generate", "search"}
			for _, keyword := range commonKeywords {
				if strings.Contains(combined, keyword) && strings.Contains(toolDesc, keyword) {
					score += 5
				}
			}

			scored[i] = scoredTool{tool: tool, score: score}
		}

		// Sort by score (descending) and take top 15
		for i := 0; i < len(scored)-1; i++ {
			for j := i + 1; j < len(scored); j++ {
				if scored[i].score < scored[j].score {
					scored[i], scored[j] = scored[j], scored[i]
				}
			}
		}

		relevant = make([]Tool, 0, 15)
		for i := 0; i < 15 && i < len(scored); i++ {
			relevant = append(relevant, scored[i].tool)
		}
	}

	return relevant
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

	// Detect if this is a simple task that doesn't need external libraries
	// This is used to skip tool instructions that might cause unnecessary imports
	descLower := strings.ToLower(req.Description)
	isSimpleTask := (strings.Contains(descLower, "print") || strings.Contains(descLower, "prints")) &&
		!strings.Contains(descLower, "matrix") &&
		!strings.Contains(descLower, "json") &&
		!strings.Contains(descLower, "read") &&
		!strings.Contains(descLower, "file") &&
		!strings.Contains(descLower, "calculate") &&
		!strings.Contains(descLower, "process") &&
		!strings.Contains(descLower, "parse") &&
		!strings.Contains(descLower, "operation") &&
		!strings.Contains(descLower, "chained") &&
		!strings.Contains(descLower, "sequential") &&
		!strings.Contains(descLower, "http") &&
		!strings.Contains(descLower, "api") &&
		!strings.Contains(descLower, "web") &&
		!strings.Contains(descLower, "network")

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
	
	// Truncate description if it's too large to prevent context size issues
	const maxDescriptionSize = 10000 // 10KB for description
	if len(cleanDesc) > maxDescriptionSize {
		log.Printf("⚠️ [CODEGEN] Truncating description from %d to %d chars", len(cleanDesc), maxDescriptionSize)
		cleanDesc = cleanDesc[:maxDescriptionSize] + "\n\n[... description truncated due to size limits ...]"
	}

	contextStr := ""
	if len(req.Context) > 0 {
		contextStr = "\n\nContext:\n"
		// Limit context value size to prevent prompt from getting too large
		const maxContextValueSize = 5000 // 5KB per context value
		for k, v := range req.Context {
			contextValue := v
			if len(contextValue) > maxContextValueSize {
				contextValue = contextValue[:maxContextValueSize] + "\n[... truncated ...]"
				log.Printf("⚠️ [CODEGEN] Truncated context value for key '%s' from %d to %d chars", k, len(v), maxContextValueSize)
			}
			contextStr += fmt.Sprintf("- %s: %s\n", k, contextValue)
		}
	}

	// Add available tools and instructions for tool usage
	// For Python: include tools by default (unless it's a simple task)
	// For other languages: only include tools if user explicitly asks for them
	toolInstructions := ""

	// Check if user explicitly mentions tools in description/task
	taskLower := strings.ToLower(req.TaskName)
	descLowerForTools := strings.ToLower(cleanDesc)
	explicitlyMentionsTools := strings.Contains(descLowerForTools, "tool_") ||
		strings.Contains(descLowerForTools, "use tool") ||
		strings.Contains(descLowerForTools, "call tool") ||
		strings.Contains(descLowerForTools, "invoke tool") ||
		strings.Contains(taskLower, "tool_") ||
		strings.Contains(taskLower, "use tool")

	// Determine if we should include tools:
	// - Python: include if not a simple task
	// - Other languages: only if user explicitly mentions tools
	// - NEVER include tools for hypothesis testing tasks (they need direct code generation)
	isHypothesisTask := strings.HasPrefix(strings.ToLower(req.TaskName), "test hypothesis:") ||
		strings.HasPrefix(strings.ToLower(cleanDesc), "test hypothesis:") ||
		strings.Contains(strings.ToLower(cleanDesc), "🚨🚨🚨 critical: do not use tools")
	
	shouldIncludeTools := false
	if isHypothesisTask {
		shouldIncludeTools = false // Never include tools for hypothesis testing
		log.Printf("📝 [CODEGEN] Hypothesis testing task detected - skipping tool instructions")
	} else if req.Language == "python" || req.Language == "py" {
		shouldIncludeTools = !isSimpleTask
	} else {
		shouldIncludeTools = explicitlyMentionsTools
	}

	if shouldIncludeTools {
		if len(req.Tools) > 0 {
			// Filter tools based on task relevance to reduce prompt size
			filteredTools := cg.filterRelevantToolsForTask(req.Tools, req.TaskName, cleanDesc)
			log.Printf("📝 [CODEGEN] Filtered tools: %d relevant tools out of %d total", len(filteredTools), len(req.Tools))
			
			if len(filteredTools) == 0 {
				// If no relevant tools found, don't include tool instructions
				log.Printf("⚠️ [CODEGEN] No relevant tools found for task, skipping tool instructions")
			} else {
				toolInstructions = "\n\n🔧 AVAILABLE TOOLS (use these via HTTP API, do NOT import as modules):\n"
				// Limit number of tools to prevent prompt from getting too large
				const maxTools = 20
				toolsToInclude := filteredTools
				if len(toolsToInclude) > maxTools {
					log.Printf("⚠️ [CODEGEN] Limiting filtered tools from %d to %d to prevent prompt size issues", len(toolsToInclude), maxTools)
					toolsToInclude = toolsToInclude[:maxTools]
					toolInstructions += fmt.Sprintf("(Showing %d of %d relevant tools)\n", maxTools, len(filteredTools))
				}
			for _, tool := range toolsToInclude {
				// Truncate tool description if too long
				desc := tool.Description
				const maxToolDescSize = 500
				if len(desc) > maxToolDescSize {
					desc = desc[:maxToolDescSize] + "..."
				}
				toolInstructions += fmt.Sprintf("- %s: %s\n", tool.ID, desc)
				if len(tool.InputSchema) > 0 {
					toolInstructions += "  Parameters: "
					params := []string{}
					// Limit number of parameters shown
					const maxParams = 10
					paramCount := 0
					for paramName, paramType := range tool.InputSchema {
						if paramCount >= maxParams {
							break
						}
						params = append(params, fmt.Sprintf("%s (%s)", paramName, paramType))
						paramCount++
					}
					toolInstructions += strings.Join(params, ", ")
					if len(tool.InputSchema) > maxParams {
						toolInstructions += fmt.Sprintf(" (and %d more)", len(tool.InputSchema)-maxParams)
					}
					toolInstructions += "\n"
				}
			}
			toolInstructions += "\n🚨 CRITICAL: To use these tools, call them via HTTP API:\n"

			// Language-specific tool usage instructions
			if req.Language == "go" {
				// Go-specific instructions
				if req.ToolAPIURL != "" {
					toolInstructions += fmt.Sprintf("- Base URL: %s\n", req.ToolAPIURL)
					toolInstructions += fmt.Sprintf("- Use this URL directly OR get from environment: `hdnURL := os.Getenv(\"HDN_URL\"); if hdnURL == \"\" { hdnURL = \"%s\" }`\n", req.ToolAPIURL)
				} else {
					toolInstructions += "- Get HDN_URL from environment: `hdnURL := os.Getenv(\"HDN_URL\"); if hdnURL == \"\" { hdnURL = \"http://host.docker.internal:8081\" }`\n"
				}
				toolInstructions += "\n🚨 CRITICAL: NEVER use 'localhost' - always use 'host.docker.internal' or the HDN_URL environment variable!\n"
				toolInstructions += "- Import required packages: `\"net/http\", \"bytes\", \"encoding/json\", \"os\"`\n"
				toolInstructions += "- Call tool via POST request:\n"
				toolInstructions += "  ```go\n"
				toolInstructions += "  params := map[string]interface{}{\"url\": \"https://example.com\"}\n"
				toolInstructions += "  jsonData, _ := json.Marshal(params)\n"
				toolInstructions += "  req, _ := http.NewRequest(\"POST\", hdnURL+\"/api/v1/tools/tool_http_get/invoke\", bytes.NewBuffer(jsonData))\n"
				toolInstructions += "  req.Header.Set(\"Content-Type\", \"application/json\")\n"
				toolInstructions += "  client := &http.Client{}\n"
				toolInstructions += "  resp, _ := client.Do(req)\n"
				toolInstructions += "  defer resp.Body.Close()\n"
				toolInstructions += "  ```\n"
				toolInstructions += "- PREFER using available tools over writing custom code when a tool can accomplish the task!\n"
			} else if req.Language == "python" || req.Language == "py" {
				// Python-specific instructions
				if req.ToolAPIURL != "" {
					toolInstructions += fmt.Sprintf("- Base URL: %s\n", req.ToolAPIURL)
					toolInstructions += fmt.Sprintf("- ALWAYS use this URL: `hdn_url = os.getenv('HDN_URL', '%s')`\n", req.ToolAPIURL)
					toolInstructions += fmt.Sprintf("- CRITICAL: Use the exact URL '%s' - do NOT use 'host.docker.internal' or 'localhost' unless this URL contains them!\n", req.ToolAPIURL)
				} else {
					// Default based on execution method - check if Docker or SSH
					defaultURL := "http://localhost:8080"
					execMethod := strings.TrimSpace(os.Getenv("EXECUTION_METHOD"))
					if execMethod == "docker" || (execMethod == "" && runtime.GOARCH != "arm64" && runtime.GOARCH != "aarch64") {
						defaultURL = "http://host.docker.internal:8080"
					}
					toolInstructions += fmt.Sprintf("- Get HDN_URL from environment: `hdn_url = os.getenv('HDN_URL', '%s')`\n", defaultURL)
					toolInstructions += fmt.Sprintf("- CRITICAL: Use the exact default '%s' - do NOT change it!\n", defaultURL)
				}
				toolInstructions += "\n🚨 CRITICAL: Always use the HDN_URL environment variable with the provided default URL!\n"
				toolInstructions += "- Call tool via POST request: `requests.post(f'{hdn_url}/api/v1/tools/{tool_id}/invoke', json={params})`\n"
				toolInstructions += "- Example for tool_http_get: `requests.post(f'{hdn_url}/api/v1/tools/tool_http_get/invoke', json={'url': 'https://example.com'})`\n"
				toolInstructions += "- Make sure to import `requests` and `os` modules, and handle the response JSON properly.\n"
				toolInstructions += "- PREFER using available tools over writing custom code when a tool can accomplish the task!\n"
			} else {
				// Generic instructions (fallback)
				if req.ToolAPIURL != "" {
					toolInstructions += fmt.Sprintf("- Base URL: %s\n", req.ToolAPIURL)
				} else {
					toolInstructions += "- Get HDN_URL from environment variable HDN_URL (default: http://host.docker.internal:8081)\n"
				}
				toolInstructions += "- Call tool via POST request to {hdn_url}/api/v1/tools/{tool_id}/invoke with JSON body containing parameters\n"
				toolInstructions += "- PREFER using available tools over writing custom code when a tool can accomplish the task!\n"
			}
			}
		} else {
			// Fallback: add instructions if task mentions tools but no tools provided
			// Only show this for Python or if user explicitly mentions tools
			if (req.Language == "python" || req.Language == "py") || explicitlyMentionsTools {
				if req.Language == "go" {
					toolInstructions = "\n\n🚨 CRITICAL: If the task mentions using a tool, DO NOT import it as a module. Instead, call the tool via HTTP API using Go's net/http package:\n"
					toolInstructions += "- Get HDN_URL from environment: `hdnURL := os.Getenv(\"HDN_URL\"); if hdnURL == \"\" { hdnURL = \"http://host.docker.internal:8081\" }`\n"
					toolInstructions += "- Use http.NewRequest with POST method and JSON body\n"
				} else {
					toolInstructions = "\n\n🚨 CRITICAL: If the task mentions using a tool (like tool_http_get, tool_html_scraper, etc.), DO NOT import it as a Python module. Instead, call the tool via HTTP API:\n"
					toolInstructions += "- Get HDN_URL from environment: `hdn_url = os.getenv('HDN_URL', 'http://host.docker.internal:8081')`\n"
					toolInstructions += "- Call tool via POST request: `requests.post(f'{hdn_url}/api/v1/tools/{tool_id}/invoke', json={params})`\n"
					toolInstructions += "- Example for tool_http_get: `requests.post(f'{hdn_url}/api/v1/tools/tool_http_get/invoke', json={'url': 'https://example.com'})`\n"
					toolInstructions += "- Make sure to import `requests` and `os` modules, and handle the response JSON properly.\n"
					toolInstructions += "🚨 IMPORTANT: tool_http_get is ONLY for external HTTP requests (like fetching web pages). DO NOT use it for Neo4j/knowledge base queries!\n"
					toolInstructions += "🚨 For Neo4j/knowledge base queries, use /api/v1/knowledge/query endpoint instead!\n"
				}
			}
		}
	}

	// Build a strong language enforcement message
	langEnforcement := ""
	if req.Language != "" {
		langEnforcement = fmt.Sprintf("\n\n🚨🚨🚨 CRITICAL LANGUAGE REQUIREMENT 🚨🚨🚨\nYou MUST generate %s code ONLY! Do NOT generate any other language!\nIf the task description mentions another language, IGNORE it - you MUST generate %s code!\n", req.Language, req.Language)

		// Add language-specific critical requirements
		if req.Language == "go" {
			langEnforcement += "\n🚨 CRITICAL GO REQUIREMENTS:\n"
			langEnforcement += "- You MUST start with 'package main' on the first line!\n"
			langEnforcement += "- You MUST include 'func main()' as the entry point!\n"
			langEnforcement += "- The code structure MUST be: package main, then imports, then func main()\n"
		}

		langEnforcement += "🚨🚨🚨 END OF CRITICAL REQUIREMENT 🚨🚨🚨\n"
		log.Printf("🔍 [CODEGEN] Added language enforcement for: %s", req.Language)
	} else {
		log.Printf("⚠️ [CODEGEN] WARNING: No language specified in request!")
	}

	// Add specific instructions for knowledge base query tasks
	knowledgeBaseInstructions := ""
	// Use existing taskLower and descLowerForTools variables (already defined above)
	// Expand pattern matching to catch more variations
	// BUT skip for hypothesis testing tasks (they have their own detailed instructions)
	// Check if this is a hypothesis task (reuse the same check from above)
	isHypothesisTaskForKB := strings.HasPrefix(taskLower, "test hypothesis:") ||
		strings.HasPrefix(descLowerForTools, "test hypothesis:") ||
		strings.Contains(descLowerForTools, "🚨🚨🚨 critical: do not use tools")
	
	isKnowledgeBaseQuery := !isHypothesisTaskForKB && (
		strings.Contains(taskLower, "query_knowledge_base") ||
		strings.Contains(descLowerForTools, "query knowledge base") ||
		strings.Contains(descLowerForTools, "query neo4j") ||
		strings.Contains(taskLower, "query_knowledge_base") ||
		strings.Contains(descLowerForTools, "neo4j") ||
		strings.Contains(descLowerForTools, "knowledge base") ||
		strings.Contains(descLowerForTools, "knowledge graph") ||
		strings.Contains(descLowerForTools, "bio concept") ||
		strings.Contains(descLowerForTools, "concept") ||
		strings.Contains(descLowerForTools, "cypher") ||
		strings.Contains(descLowerForTools, "graph database") ||
		strings.Contains(descLowerForTools, "retrieve from") ||
		strings.Contains(descLowerForTools, "fetch from") ||
		strings.Contains(descLowerForTools, "get data from"))

	if isKnowledgeBaseQuery {
		if req.Language == "python" || req.Language == "py" {
			knowledgeBaseInstructions = "\n\n🚨 CRITICAL: This is a knowledge base query task. You MUST use the knowledge query endpoint:\n"
			knowledgeBaseInstructions += "```python\n"
			knowledgeBaseInstructions += "import requests\n"
			knowledgeBaseInstructions += "import os\n"
			knowledgeBaseInstructions += "hdn_url = os.getenv('HDN_URL', 'http://host.docker.internal:8081')\n"
			knowledgeBaseInstructions += "# PREFER returning explicit properties for easier access: RETURN c.name AS name, c.description AS description\n"
			knowledgeBaseInstructions += "# This makes the response structure clearer: result['name'] instead of result['c']['name']\n"
			knowledgeBaseInstructions += "response = requests.post(f'{hdn_url}/api/v1/knowledge/query',\n"
			knowledgeBaseInstructions += "    json={'query': 'MATCH (c:Concept) WHERE toLower(c.name) CONTAINS toLower(\\'CONCEPT_NAME\\') RETURN c.name AS name, c.description AS description LIMIT 10'})\n"
			knowledgeBaseInstructions += "response.raise_for_status()  # Check for HTTP errors\n"
			knowledgeBaseInstructions += "data = response.json()  # MUST parse JSON before using 'data'\n"
			knowledgeBaseInstructions += "results = data.get('results', [])  # Now safe to use 'data'\n"
			knowledgeBaseInstructions += "count = data.get('count', 0)\n"
			knowledgeBaseInstructions += "# Process results: each result is a dict with keys matching RETURN clause aliases\n"
			knowledgeBaseInstructions += "# If query uses 'RETURN c.name AS name', access via result['name']\n"
			knowledgeBaseInstructions += "# If query uses 'RETURN c', access via result['c']['name'] (node object)\n"
			knowledgeBaseInstructions += "# PREFER using explicit property returns (RETURN c.name AS name) for clarity!\n"
			knowledgeBaseInstructions += "for result in results:\n"
			knowledgeBaseInstructions += "    # If using explicit property returns (recommended):\n"
			knowledgeBaseInstructions += "    name = result.get('name', 'Unknown')\n"
			knowledgeBaseInstructions += "    description = result.get('description', '')\n"
			knowledgeBaseInstructions += "    print(f\"Concept: {name}\")\n"
			knowledgeBaseInstructions += "    if description:\n"
			knowledgeBaseInstructions += "        print(f\"  Description: {description}\")\n"
			knowledgeBaseInstructions += "```\n"
			knowledgeBaseInstructions += "🚨 IMPORTANT: ALWAYS call response.json() to get 'data' BEFORE checking 'results' in data!\n"
			knowledgeBaseInstructions += "🚨 CRITICAL: Response format: {\"results\": [{\"c\": {\"name\": \"...\", \"description\": \"...\", ...}}, ...], \"count\": N}\n"
			knowledgeBaseInstructions += "🚨 CRITICAL: Each result is a dict where keys match RETURN clause variables (e.g., 'c' for RETURN c)\n"
			knowledgeBaseInstructions += "🚨 CRITICAL: The node object itself is a dict with properties - access via result['c']['name'], not result['c'].get('name')\n"
			knowledgeBaseInstructions += "🚨 CRITICAL: DO NOT use /api/v1/tools/tool_http_get/invoke for knowledge base queries - that will return 403!\n"
			knowledgeBaseInstructions += "🚨 CRITICAL: DO NOT use /api/v1/tools/tool_mcp_query_neo4j/invoke - that endpoint does NOT exist (returns 501)!\n"
			knowledgeBaseInstructions += "🚨 CRITICAL: DO NOT use /api/v1/nodes/{id}/properties - that endpoint does NOT exist!\n"
			knowledgeBaseInstructions += "🚨 CRITICAL: DO NOT try to access node properties via REST API - use Cypher queries via /api/v1/knowledge/query instead!\n"
			knowledgeBaseInstructions += "🚨 CRITICAL: The ONLY correct endpoint for Neo4j queries is: POST /api/v1/knowledge/query with JSON body {\"query\": \"CYPHER_QUERY\"}\n"
		} else if req.Language == "go" {
			knowledgeBaseInstructions = "\n\n🚨 CRITICAL: This is a knowledge base query task. You MUST use the knowledge query endpoint:\n"
			knowledgeBaseInstructions += "Query Neo4j directly via POST to /api/v1/knowledge/query with Cypher query in JSON body: {\"query\": \"CYPHER_QUERY\"}\n"
			knowledgeBaseInstructions += "Response format: {\"results\": [...], \"count\": N}\n"
			knowledgeBaseInstructions += "🚨 DO NOT use /api/v1/tools/tool_mcp_query_neo4j/invoke - that endpoint does NOT exist (returns 501)!\n"
			knowledgeBaseInstructions += "🚨 DO NOT use /api/v1/nodes/{id}/properties - that endpoint does NOT exist!\n"
		}
	}

	// Check if this is a Wikipedia scraping/fetching task
	isWikipediaScrape := strings.Contains(descLowerForTools, "wikipedia") && 
		(strings.Contains(descLowerForTools, "scrape") || 
		 strings.Contains(descLowerForTools, "fetch") || 
		 strings.Contains(descLowerForTools, "extract") ||
		 strings.Contains(descLowerForTools, "article"))
	
	wikipediaInstructions := ""
	if isWikipediaScrape {
		if req.Language == "python" || req.Language == "py" {
			// Determine if this is for tool_html_scraper or tool_http_get
			if strings.Contains(descLowerForTools, "html_scraper") {
				wikipediaInstructions = "\n\n🚨 CRITICAL: This task requires scraping Wikipedia with tool_html_scraper. Follow these steps exactly:\n"
				wikipediaInstructions += "1. Construct Wikipedia URL: https://en.wikipedia.org/wiki/ARTICLE_NAME (replace spaces with underscores)\n"
				wikipediaInstructions += "2. Call tool_html_scraper with the URL\n"
				wikipediaInstructions += "3. The tool returns: {\"items\": [...]}, NOT {\"title\": ...}\n"
				wikipediaInstructions += "4. IMPORTANT: tool_html_scraper returns a dict with 'items' array, NOT 'title', 'paragraphs', or 'links'\n"
				wikipediaInstructions += "5. Each item in 'items' array is a dict with keys like 'tag', 'text', 'href'\n"
				wikipediaInstructions += "6. Parse and process the items array to extract meaningful content\n"
				wikipediaInstructions += "7. EXAMPLE response: {\"items\": [{\"tag\": \"h1\", \"text\": \"Machine Learning\"}, {\"tag\": \"p\", \"text\": \"Machine learning is...\"}]}\n"
			} else if strings.Contains(descLowerForTools, "tool_http_get") || strings.Contains(descLowerForTools, "http_get") {
				wikipediaInstructions = "\n\n🚨 CRITICAL: This task requires fetching Wikipedia with tool_http_get. Follow these steps exactly:\n"
				wikipediaInstructions += "1. DETERMINE WHICH WIKIPEDIA ARTICLES TO FETCH: Extract topic names from the task description\n"
				wikipediaInstructions += "2. Construct Wikipedia URLs: https://en.wikipedia.org/wiki/ARTICLE_NAME (replace spaces with underscores)\n"
				wikipediaInstructions += "3. Call tool_http_get via HTTP API for EACH article\n"
				wikipediaInstructions += "4. Example URLs:\n"
				wikipediaInstructions += "   - https://en.wikipedia.org/wiki/Machine_Learning\n"
				wikipediaInstructions += "   - https://en.wikipedia.org/wiki/Artificial_Intelligence\n"
				wikipediaInstructions += "   - https://en.wikipedia.org/wiki/Deep_Learning\n"
				wikipediaInstructions += "5. tool_http_get response format: {\"status\": 200, \"body\": \"<html>...\"}\n"
				wikipediaInstructions += "6. Extract meaningful text from the HTML body (titles, key concepts, definitions)\n"
				wikipediaInstructions += "7. Return a structured summary of what you learned from the Wikipedia articles\n"
			} else {
				wikipediaInstructions = "\n\n🚨 CRITICAL: This task requires fetching and analyzing Wikipedia. Follow these steps exactly:\n"
				wikipediaInstructions += "1. DETERMINE WHICH WIKIPEDIA ARTICLES TO FETCH from the task description\n"
				wikipediaInstructions += "2. Construct Wikipedia URLs: https://en.wikipedia.org/wiki/ARTICLE_NAME (replace spaces with underscores)\n"
				wikipediaInstructions += "3. Use tool_http_get to fetch the Wikipedia articles\n"
				wikipediaInstructions += "4. Parse the HTML response to extract key information\n"
				wikipediaInstructions += "5. Return a structured summary with key concepts, definitions, and relationships\n"
			}
		}
	}

	// Add general instruction about avoiding unnecessary imports
	importInstruction := ""
	if isSimpleTask {
		importInstruction = "\n\n🚨 IMPORTANT: This is a simple task. DO NOT import external libraries unless explicitly required. Use only built-in language features."
	}

	// Add warning about internal system flags
	internalFlagsWarning := "\n\n🚨 CRITICAL: DO NOT use internal system flags like 'force_regenerate', 'artifacts_wrapper', or 'allow_requests' in your code!\n"
	internalFlagsWarning += "🚨 These are internal system flags and should NOT be checked or used in generated code.\n"
	internalFlagsWarning += "🚨 If you see these in environment variables, IGNORE them - they are not part of the task requirements.\n"
	internalFlagsWarning += "🚨 DO NOT add checks like 'if force_regenerate: return' or 'if not allow_requests: return' - these will break execution!\n"

	// Add warning about interactive input
	inputWarning := "\n\n🚨 CRITICAL: DO NOT use interactive input functions like input(), raw_input(), or sys.stdin.read()!\n"
	inputWarning += "🚨 The code runs in a non-interactive environment (Docker container or SSH) - there is NO user input available!\n"
	if req.Language == "python" || req.Language == "py" {
		inputWarning += "🚨 Instead, read parameters from environment variables using: os.getenv('PARAMETER_NAME', 'default_value')\n"
		inputWarning += "🚨 Example: concept_name = os.getenv('concept_name', 'Biology')  # NOT: concept_name = input('Enter concept: ')\n"
	} else if req.Language == "go" {
		inputWarning += "🚨 Instead, read parameters from environment variables using: os.Getenv('PARAMETER_NAME')\n"
		inputWarning += "🚨 Example: conceptName := os.Getenv('concept_name'); if conceptName == \"\" { conceptName = \"Biology\" }\n"
	} else {
		inputWarning += "🚨 Instead, read parameters from environment variables or command-line arguments.\n"
	}
	inputWarning += "🚨 If the task requires a parameter, use a sensible default value or read from environment variables.\n"

	codeBlockTag := "```" + req.Language
	return fmt.Sprintf(`Generate %s code for this task:

%s%s%s%s%s%s%s%s%s

Return only the %s code in a markdown code block with the language tag: %s
`, req.Language, cleanDesc, langEnforcement, contextStr, toolInstructions, knowledgeBaseInstructions, wikipediaInstructions, importInstruction, internalFlagsWarning, inputWarning, req.Language, codeBlockTag)
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
				strings.Contains(trimmed, "class ") || strings.Contains(trimmed, "fn main") ||
				strings.Contains(trimmed, "use ") || strings.Contains(trimmed, "public class") ||
				strings.Contains(trimmed, "public static void main") {
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
	// Check for language mismatches and reject them
	if language == "go" {
		// Check for Python syntax in Go code
		if strings.Contains(code, "import time") && strings.Contains(code, "def ") {
			// This looks like Python code, not Go
			log.Printf("❌ [CODEGEN] LLM generated Python code when Go was requested!")
			log.Printf("❌ [CODEGEN] Code preview (first 200 chars): %s", func() string {
				if len(code) > 200 {
					return code[:200]
				}
				return code
			}())
			return "", fmt.Errorf("LLM generated Python code when %s was requested! Code starts with: %s", language, func() string {
				if len(code) > 100 {
					return code[:100]
				}
				return code
			}())
		}
		// Check for Go-specific syntax - if missing, might be wrong language
		if !strings.Contains(code, "package ") && !strings.Contains(code, "func ") {
			// Might be wrong language, but be lenient - could be a fragment
			if strings.Contains(code, "def ") || strings.Contains(code, "import time") {
				log.Printf("⚠️ [CODEGEN] Go code missing 'package' and 'func', but contains Python syntax - likely wrong language")
				return "", fmt.Errorf("LLM generated Python code when %s was requested! Code contains Python syntax.", language)
			}
		}
	} else if language == "javascript" || language == "js" {
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
