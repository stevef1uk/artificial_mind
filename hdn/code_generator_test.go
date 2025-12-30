package main

import (
	"strings"
	"testing"
	"time"
)

// createTestTool creates a test tool for use in tests
func createTestTool(id, description string) Tool {
	return Tool{
		ID:          id,
		Name:        id,
		Description: description,
		InputSchema: map[string]string{
			"url": "string",
		},
		CreatedBy:   "test",
		CreatedAt:   time.Now(),
		SafetyLevel: "safe",
	}
}

// TestBuildCodeGenerationPrompt_PythonIncludesTools tests that Python includes tools by default
func TestBuildCodeGenerationPrompt_PythonIncludesTools(t *testing.T) {
	cg := &CodeGenerator{}
	req := &CodeGenerationRequest{
		TaskName:    "TestTask",
		Description: "Calculate the sum of two numbers",
		Language:    "python",
		Tools: []Tool{
			createTestTool("tool_http_get", "Fetch URL"),
		},
		ToolAPIURL: "http://host.docker.internal:8081",
	}

	prompt := cg.buildCodeGenerationPrompt(req)

	// Should include tool instructions
	if !strings.Contains(prompt, "🔧 AVAILABLE TOOLS") {
		t.Error("Python prompt should include tool instructions")
	}
	if !strings.Contains(prompt, "tool_http_get") {
		t.Error("Python prompt should include tool names")
	}
	if !strings.Contains(prompt, "requests.post") {
		t.Error("Python prompt should include Python-specific tool usage instructions")
	}
}

// TestBuildCodeGenerationPrompt_PythonSimpleTaskExcludesTools tests that Python simple tasks exclude tools
func TestBuildCodeGenerationPrompt_PythonSimpleTaskExcludesTools(t *testing.T) {
	cg := &CodeGenerator{}
	req := &CodeGenerationRequest{
		TaskName:    "TestTask",
		Description: "Print hello world",
		Language:    "python",
		Tools: []Tool{
			createTestTool("tool_http_get", "Fetch URL"),
		},
		ToolAPIURL: "http://host.docker.internal:8081",
	}

	prompt := cg.buildCodeGenerationPrompt(req)

	// Should NOT include tool instructions for simple tasks
	if strings.Contains(prompt, "🔧 AVAILABLE TOOLS") {
		t.Error("Python simple task prompt should NOT include tool instructions")
	}
}

// TestBuildCodeGenerationPrompt_GoExcludesToolsByDefault tests that Go excludes tools unless explicitly mentioned
func TestBuildCodeGenerationPrompt_GoExcludesToolsByDefault(t *testing.T) {
	cg := &CodeGenerator{}
	req := &CodeGenerationRequest{
		TaskName:    "TestTask",
		Description: "Calculate the sum of two numbers",
		Language:    "go",
		Tools: []Tool{
			createTestTool("tool_http_get", "Fetch URL"),
		},
		ToolAPIURL: "http://host.docker.internal:8081",
	}

	prompt := cg.buildCodeGenerationPrompt(req)

	// Should NOT include tool instructions
	if strings.Contains(prompt, "🔧 AVAILABLE TOOLS") {
		t.Error("Go prompt should NOT include tool instructions unless explicitly mentioned")
	}
	if strings.Contains(prompt, "net/http") {
		t.Error("Go prompt should NOT include tool usage instructions unless explicitly mentioned")
	}
}

// TestBuildCodeGenerationPrompt_GoIncludesToolsWhenExplicitlyMentioned tests that Go includes tools when explicitly mentioned
func TestBuildCodeGenerationPrompt_GoIncludesToolsWhenExplicitlyMentioned(t *testing.T) {
	cg := &CodeGenerator{}
	req := &CodeGenerationRequest{
		TaskName:    "TestTask",
		Description: "Use tool_http_get to fetch data from an API",
		Language:    "go",
		Tools: []Tool{
			createTestTool("tool_http_get", "Fetch URL"),
		},
		ToolAPIURL: "http://host.docker.internal:8081",
	}

	prompt := cg.buildCodeGenerationPrompt(req)

	// Should include tool instructions when explicitly mentioned
	if !strings.Contains(prompt, "🔧 AVAILABLE TOOLS") {
		t.Error("Go prompt should include tool instructions when explicitly mentioned")
	}
	if !strings.Contains(prompt, "tool_http_get") {
		t.Error("Go prompt should include tool names when explicitly mentioned")
	}
	if !strings.Contains(prompt, "net/http") {
		t.Error("Go prompt should include Go-specific tool usage instructions when explicitly mentioned")
	}
}

// TestBuildCodeGenerationPrompt_GoIncludesToolsWhenTaskNameMentionsTool tests that Go includes tools when task name mentions tool
func TestBuildCodeGenerationPrompt_GoIncludesToolsWhenTaskNameMentionsTool(t *testing.T) {
	cg := &CodeGenerator{}
	req := &CodeGenerationRequest{
		TaskName:    "Use tool_http_get",
		Description: "Fetch data from an API",
		Language:    "go",
		Tools: []Tool{
			createTestTool("tool_http_get", "Fetch URL"),
		},
		ToolAPIURL: "http://host.docker.internal:8081",
	}

	prompt := cg.buildCodeGenerationPrompt(req)

	// Should include tool instructions when task name mentions tool
	if !strings.Contains(prompt, "🔧 AVAILABLE TOOLS") {
		t.Error("Go prompt should include tool instructions when task name mentions tool")
	}
}

// TestBuildCodeGenerationPrompt_JavaScriptExcludesToolsByDefault tests that JavaScript excludes tools unless explicitly mentioned
func TestBuildCodeGenerationPrompt_JavaScriptExcludesToolsByDefault(t *testing.T) {
	cg := &CodeGenerator{}
	req := &CodeGenerationRequest{
		TaskName:    "TestTask",
		Description: "Calculate the mean of an array",
		Language:    "javascript",
		Tools: []Tool{
			createTestTool("tool_http_get", "Fetch URL"),
		},
		ToolAPIURL: "http://host.docker.internal:8081",
	}

	prompt := cg.buildCodeGenerationPrompt(req)

	// Should NOT include tool instructions
	if strings.Contains(prompt, "🔧 AVAILABLE TOOLS") {
		t.Error("JavaScript prompt should NOT include tool instructions unless explicitly mentioned")
	}
}

// TestBuildCodeGenerationPrompt_JavaScriptIncludesToolsWhenExplicitlyMentioned tests that JavaScript includes tools when explicitly mentioned
func TestBuildCodeGenerationPrompt_JavaScriptIncludesToolsWhenExplicitlyMentioned(t *testing.T) {
	cg := &CodeGenerator{}
	req := &CodeGenerationRequest{
		TaskName:    "TestTask",
		Description: "Call tool tool_http_get to fetch data",
		Language:    "javascript",
		Tools: []Tool{
			createTestTool("tool_http_get", "Fetch URL"),
		},
		ToolAPIURL: "http://host.docker.internal:8081",
	}

	prompt := cg.buildCodeGenerationPrompt(req)

	// Should include tool instructions when explicitly mentioned
	if !strings.Contains(prompt, "🔧 AVAILABLE TOOLS") {
		t.Error("JavaScript prompt should include tool instructions when explicitly mentioned")
	}
}

// TestBuildCodeGenerationPrompt_RustExcludesToolsByDefault tests that Rust excludes tools unless explicitly mentioned
func TestBuildCodeGenerationPrompt_RustExcludesToolsByDefault(t *testing.T) {
	cg := &CodeGenerator{}
	req := &CodeGenerationRequest{
		TaskName:    "TestTask",
		Description: "Create a struct and print it",
		Language:    "rust",
		Tools: []Tool{
			createTestTool("tool_http_get", "Fetch URL"),
		},
		ToolAPIURL: "http://host.docker.internal:8081",
	}

	prompt := cg.buildCodeGenerationPrompt(req)

	// Should NOT include tool instructions
	if strings.Contains(prompt, "🔧 AVAILABLE TOOLS") {
		t.Error("Rust prompt should NOT include tool instructions unless explicitly mentioned")
	}
}

// TestBuildCodeGenerationPrompt_RustIncludesToolsWhenExplicitlyMentioned tests that Rust includes tools when explicitly mentioned
func TestBuildCodeGenerationPrompt_RustIncludesToolsWhenExplicitlyMentioned(t *testing.T) {
	cg := &CodeGenerator{}
	req := &CodeGenerationRequest{
		TaskName:    "TestTask",
		Description: "Invoke tool tool_http_get to fetch data",
		Language:    "rust",
		Tools: []Tool{
			createTestTool("tool_http_get", "Fetch URL"),
		},
		ToolAPIURL: "http://host.docker.internal:8081",
	}

	prompt := cg.buildCodeGenerationPrompt(req)

	// Should include tool instructions when explicitly mentioned
	if !strings.Contains(prompt, "🔧 AVAILABLE TOOLS") {
		t.Error("Rust prompt should include tool instructions when explicitly mentioned")
	}
}

// TestBuildCodeGenerationPrompt_PythonComplexTaskIncludesTools tests that Python complex tasks include tools
func TestBuildCodeGenerationPrompt_PythonComplexTaskIncludesTools(t *testing.T) {
	cg := &CodeGenerator{}
	req := &CodeGenerationRequest{
		TaskName:    "TestTask",
		Description: "Read a JSON file and parse it, then calculate statistics",
		Language:    "python",
		Tools: []Tool{
			createTestTool("tool_file_read", "Read file"),
			createTestTool("tool_json_parse", "Parse JSON"),
		},
		ToolAPIURL: "http://host.docker.internal:8081",
	}

	prompt := cg.buildCodeGenerationPrompt(req)

	// Should include tool instructions for complex tasks
	if !strings.Contains(prompt, "🔧 AVAILABLE TOOLS") {
		t.Error("Python complex task prompt should include tool instructions")
	}
}

// TestBuildCodeGenerationPrompt_ExplicitToolMentionPatterns tests various patterns for explicit tool mentions
func TestBuildCodeGenerationPrompt_ExplicitToolMentionPatterns(t *testing.T) {
	cg := &CodeGenerator{}
	testCases := []struct {
		name        string
		description string
		taskName    string
		shouldInclude bool
	}{
		{"use tool_ pattern", "Use tool_http_get to fetch data", "TestTask", true},
		{"call tool pattern", "Call tool tool_http_get", "TestTask", true},
		{"invoke tool pattern", "Invoke tool tool_file_read", "TestTask", true},
		{"tool_ in description", "Fetch data using tool_http_get", "TestTask", true},
		{"tool_ in task name", "Fetch data", "Use tool_http_get", true},
		{"no tool mention", "Calculate sum of numbers", "TestTask", false},
		{"tool mention in different case", "USE TOOL tool_http_get", "TestTask", true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := &CodeGenerationRequest{
				TaskName:    tc.taskName,
				Description: tc.description,
				Language:    "go",
				Tools: []Tool{
					createTestTool("tool_http_get", "Fetch URL"),
				},
				ToolAPIURL: "http://host.docker.internal:8081",
			}

			prompt := cg.buildCodeGenerationPrompt(req)
			hasTools := strings.Contains(prompt, "🔧 AVAILABLE TOOLS")

			if tc.shouldInclude && !hasTools {
				t.Errorf("Expected tools to be included for: %s", tc.description)
			}
			if !tc.shouldInclude && hasTools {
				t.Errorf("Expected tools to NOT be included for: %s", tc.description)
			}
		})
	}
}

// TestBuildCodeGenerationPrompt_NoToolsProvided tests behavior when no tools are provided
func TestBuildCodeGenerationPrompt_NoToolsProvided(t *testing.T) {
	cg := &CodeGenerator{}
	
	// Python with no tools but explicit mention
	reqPython := &CodeGenerationRequest{
		TaskName:    "TestTask",
		Description: "Use tool_http_get to fetch data",
		Language:    "python",
		Tools:       []Tool{},
		ToolAPIURL:  "http://host.docker.internal:8081",
	}

	promptPython := cg.buildCodeGenerationPrompt(reqPython)
	// Should still have fallback instructions for Python
	if !strings.Contains(promptPython, "CRITICAL") || !strings.Contains(promptPython, "tool") {
		t.Error("Python prompt should include fallback tool instructions when tools are mentioned")
	}

	// Go with no tools and no explicit mention
	reqGo := &CodeGenerationRequest{
		TaskName:    "TestTask",
		Description: "Calculate sum",
		Language:    "go",
		Tools:       []Tool{},
		ToolAPIURL:  "http://host.docker.internal:8081",
	}

	promptGo := cg.buildCodeGenerationPrompt(reqGo)
	// Should NOT have tool instructions
	if strings.Contains(promptGo, "CRITICAL") && strings.Contains(promptGo, "tool") {
		t.Error("Go prompt should NOT include tool instructions when no tools and no explicit mention")
	}
}

// TestBuildCodeGenerationPrompt_LanguageEnforcement tests that language enforcement is always included
func TestBuildCodeGenerationPrompt_LanguageEnforcement(t *testing.T) {
	cg := &CodeGenerator{}
	req := &CodeGenerationRequest{
		TaskName:    "TestTask",
		Description: "Calculate sum",
		Language:    "go",
		Tools:       []Tool{},
	}

	prompt := cg.buildCodeGenerationPrompt(req)

	// Should always include language enforcement
	if !strings.Contains(prompt, "CRITICAL LANGUAGE REQUIREMENT") {
		t.Error("Prompt should always include language enforcement")
	}
	if !strings.Contains(prompt, "go code ONLY") {
		t.Error("Prompt should enforce the specified language")
	}
	if !strings.Contains(prompt, "package main") {
		t.Error("Go prompt should include Go-specific requirements")
	}
}

// TestBuildCodeGenerationPrompt_ContextIncluded tests that context is included in prompt
func TestBuildCodeGenerationPrompt_ContextIncluded(t *testing.T) {
	cg := &CodeGenerator{}
	req := &CodeGenerationRequest{
		TaskName:    "TestTask",
		Description: "Calculate sum",
		Language:    "python",
		Context: map[string]string{
			"num1": "10",
			"num2": "20",
		},
		Tools: []Tool{},
	}

	prompt := cg.buildCodeGenerationPrompt(req)

	// Should include context
	if !strings.Contains(prompt, "Context:") {
		t.Error("Prompt should include context section")
	}
	if !strings.Contains(prompt, "num1: 10") {
		t.Error("Prompt should include context values")
	}
	if !strings.Contains(prompt, "num2: 20") {
		t.Error("Prompt should include all context values")
	}
}

// TestBuildCodeGenerationPrompt_ToolAPIURLIncluded tests that ToolAPIURL is included when tools are present
func TestBuildCodeGenerationPrompt_ToolAPIURLIncluded(t *testing.T) {
	cg := &CodeGenerator{}
	req := &CodeGenerationRequest{
		TaskName:    "TestTask",
		Description: "Use tool_http_get to fetch data",
		Language:    "go",
		Tools: []Tool{
			createTestTool("tool_http_get", "Fetch URL"),
		},
		ToolAPIURL: "http://custom.url:8081",
	}

	prompt := cg.buildCodeGenerationPrompt(req)

	// Should include the custom ToolAPIURL
	if !strings.Contains(prompt, "http://custom.url:8081") {
		t.Error("Prompt should include the custom ToolAPIURL")
	}
}

// TestBuildCodeGenerationPrompt_SimpleTaskInstruction tests that simple tasks get special instruction
func TestBuildCodeGenerationPrompt_SimpleTaskInstruction(t *testing.T) {
	cg := &CodeGenerator{}
	req := &CodeGenerationRequest{
		TaskName:    "TestTask",
		Description: "Print hello world",
		Language:    "python",
		Tools:       []Tool{},
	}

	prompt := cg.buildCodeGenerationPrompt(req)

	// Should include instruction about avoiding unnecessary imports
	if !strings.Contains(prompt, "DO NOT import external libraries") {
		t.Error("Simple task prompt should include instruction about avoiding unnecessary imports")
	}
}

// TestFixLocalhostReferences tests that localhost references are correctly replaced without panicking
func TestFixLocalhostReferences(t *testing.T) {
	cg := &CodeGenerator{}

	testCases := []struct {
		name     string
		input    string
		expected string
		language string
	}{
		{
			name:     "Python double quote string literal localhost:8081",
			input:    `url = "http://localhost:8081/api"`,
			expected: `url = "http://host.docker.internal:8081/api"`,
			language: "python",
		},
		{
			name:     "Python single quote string literal localhost:8080",
			input:    `url = 'http://localhost:8080/api'`,
			expected: `url = 'http://host.docker.internal:8080/api'`,
			language: "python",
		},
		{
			name:     "Python f-string double quote localhost:8081",
			input:    `url = f"http://localhost:8081/api"`,
			expected: `url = f"http://host.docker.internal:8081/api"`,
			language: "python",
		},
		{
			name:     "Python f-string single quote localhost:8080",
			input:    `url = f'http://localhost:8080/api'`,
			expected: `url = f'http://host.docker.internal:8080/api'`,
			language: "python",
		},
		{
			name:     "Python variable assignment double quote",
			input:    `hdn_url = "http://localhost:8081"`,
			expected: `hdn_url = "http://host.docker.internal:8081"`,
			language: "python",
		},
		{
			name:     "Python variable assignment single quote",
			input:    `hdn_url = 'http://localhost:8080'`,
			expected: `hdn_url = 'http://host.docker.internal:8080'`,
			language: "python",
		},
		{
			name:     "Go string literal localhost:8081",
			input:    `url := "http://localhost:8081/api"`,
			expected: `url := "http://host.docker.internal:8081/api"`,
			language: "go",
		},
		{
			name:     "JavaScript string literal localhost:8080",
			input:    `const url = "http://localhost:8080/api";`,
			expected: `const url = "http://host.docker.internal:8080/api";`,
			language: "javascript",
		},
		{
			name:     "Multiple localhost references",
			input:    `url1 = "http://localhost:8081"; url2 = 'http://localhost:8080'`,
			expected: `url1 = "http://host.docker.internal:8081"; url2 = 'http://host.docker.internal:8080'`,
			language: "python",
		},
		{
			name:     "No localhost references should remain unchanged",
			input:    `url = "http://example.com/api"`,
			expected: `url = "http://example.com/api"`,
			language: "python",
		},
		{
			name:     "Localhost without port should remain unchanged",
			input:    `url = "http://localhost/api"`,
			expected: `url = "http://localhost/api"`,
			language: "python",
		},
		{
			name:     "Localhost with different port should remain unchanged",
			input:    `url = "http://localhost:3000/api"`,
			expected: `url = "http://host.docker.internal:3000/api"`,
			language: "python",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// This should not panic - the previous bug would cause a panic here
			result := cg.fixLocalhostReferences(tc.input, tc.language)

			if result != tc.expected {
				t.Errorf("Expected:\n%s\nGot:\n%s", tc.expected, result)
			}

			// Verify localhost:8081 and localhost:8080 are replaced
			if strings.Contains(result, "localhost:8081") || strings.Contains(result, "localhost:8080") {
				t.Errorf("Result still contains localhost:8081 or localhost:8080: %s", result)
			}
		})
	}
}

// TestFixLocalhostReferences_NoPanic tests that the function doesn't panic with various edge cases
func TestFixLocalhostReferences_NoPanic(t *testing.T) {
	cg := &CodeGenerator{}

	// These patterns previously caused panics due to invalid backreferences
	edgeCases := []struct {
		name  string
		input string
	}{
		{"Empty string", ""},
		{"Only quotes", `""`},
		{"Mismatched quotes", `"http://localhost:8081'`},
		{"Nested quotes", `url = "http://'localhost':8081"`},
		{"Backslash in string", `url = "http://localhost:8081\\path"`},
		{"Newline in string", "url = \"http://localhost:8081\\n/api\""},
		{"Tab in string", "url = \"http://localhost:8081\\t/api\""},
		{"Unicode characters", `url = "http://localhost:8081/测试"`},
		{"Very long string", strings.Repeat(`"http://localhost:8081"`, 100)},
		{"Mixed content", `code = "test"; url = "http://localhost:8081"; more = "code"`},
	}

	for _, tc := range edgeCases {
		t.Run(tc.name, func(t *testing.T) {
			// This should not panic
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("Function panicked with input '%s': %v", tc.input, r)
				}
			}()

			_ = cg.fixLocalhostReferences(tc.input, "python")
		})
	}
}

