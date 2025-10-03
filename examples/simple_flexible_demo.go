package main

import (
	"context"
	"fmt"
	"log"

	"agi/hdn/interpreter"
)

// MockLLMClient for testing
type MockLLMClient struct{}

func (m *MockLLMClient) CallLLM(prompt string) (string, error) {
	// Return different responses based on the input
	if containsString(prompt, "fibonacci") || containsString(prompt, "generate") {
		return `{
			"type": "tool_call",
			"tool_call": {
				"tool_id": "tool_codegen",
				"parameters": {"spec": "Python function to calculate fibonacci numbers"},
				"description": "Generate Python code for fibonacci calculation"
			}
		}`, nil
	}

	if containsString(prompt, "search") || containsString(prompt, "information") {
		return `{
			"type": "tool_call",
			"tool_call": {
				"tool_id": "tool_http_get",
				"parameters": {"url": "https://en.wikipedia.org/wiki/Artificial_intelligence"},
				"description": "Search for AI information on Wikipedia"
			}
		}`, nil
	}

	if containsString(prompt, "read") || containsString(prompt, "file") {
		return `{
			"type": "tool_call",
			"tool_call": {
				"tool_id": "tool_file_read",
				"parameters": {"path": "/etc/hostname"},
				"description": "Read the hostname file"
			}
		}`, nil
	}

	// Default fallback
	return `{
		"type": "text",
		"content": "I understand your request and will help you with that."
	}`, nil
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && s[:len(substr)] == substr ||
		len(s) > len(substr) && containsString(s[1:], substr)
}

// EnhancedMockToolProvider with more sophisticated tools
type EnhancedMockToolProvider struct{}

func (e *EnhancedMockToolProvider) GetAvailableTools(ctx context.Context) ([]interpreter.AvailableTool, error) {
	return []interpreter.AvailableTool{
		{
			ID:          "tool_codegen",
			Name:        "Code Generator",
			Description: "Generate code using LLM based on specifications",
			InputSchema: map[string]string{
				"spec": "Code generation specification",
			},
			Example: "Example: {spec: \"Python function to calculate fibonacci numbers\"}",
		},
		{
			ID:          "tool_http_get",
			Name:        "HTTP GET",
			Description: "Fetch content from a URL",
			InputSchema: map[string]string{
				"url": "URL to fetch",
			},
			Example: "Example: {url: \"https://example.com\"}",
		},
		{
			ID:          "tool_file_read",
			Name:        "File Reader",
			Description: "Read contents of a file",
			InputSchema: map[string]string{
				"path": "Path to the file to read",
			},
			Example: "Example: {path: \"/path/to/file.txt\"}",
		},
	}, nil
}

func (e *EnhancedMockToolProvider) ExecuteTool(ctx context.Context, toolCall *interpreter.ToolCall) (*interpreter.ToolExecutionResult, error) {
	log.Printf("🔧 [ENHANCED-MOCK] Executing tool: %s", toolCall.ToolID)

	switch toolCall.ToolID {
	case "tool_codegen":
		return &interpreter.ToolExecutionResult{
			Success: true,
			Result: map[string]interface{}{
				"code":     "def fibonacci(n):\n    if n <= 1:\n        return n\n    return fibonacci(n-1) + fibonacci(n-2)\n\nprint(fibonacci(10))",
				"language": "python",
				"spec":     toolCall.Parameters["spec"],
			},
			Metadata: map[string]interface{}{
				"tool_id":     toolCall.ToolID,
				"executed_at": "2024-01-01T00:00:00Z",
			},
		}, nil

	case "tool_http_get":
		return &interpreter.ToolExecutionResult{
			Success: true,
			Result: map[string]interface{}{
				"status": 200,
				"body":   "Mock web content about artificial intelligence...",
				"url":    toolCall.Parameters["url"],
			},
			Metadata: map[string]interface{}{
				"tool_id":     toolCall.ToolID,
				"executed_at": "2024-01-01T00:00:00Z",
			},
		}, nil

	case "tool_file_read":
		return &interpreter.ToolExecutionResult{
			Success: true,
			Result: map[string]interface{}{
				"content": "omen\n",
				"path":    toolCall.Parameters["path"],
			},
			Metadata: map[string]interface{}{
				"tool_id":     toolCall.ToolID,
				"executed_at": "2024-01-01T00:00:00Z",
			},
		}, nil

	default:
		return &interpreter.ToolExecutionResult{
			Success: false,
			Error:   fmt.Sprintf("Unknown tool: %s", toolCall.ToolID),
		}, nil
	}
}

func main() {
	fmt.Println("🧪 Flexible API Test with Mock LLM and Enhanced Tools")
	fmt.Println("=====================================================")

	// Create mock LLM and enhanced tool provider
	mockLLM := &MockLLMClient{}
	enhancedToolProvider := &EnhancedMockToolProvider{}

	// Create flexible interpreter
	adapter := interpreter.NewFlexibleLLMAdapter(mockLLM)
	flexibleInterpreter := interpreter.NewFlexibleInterpreter(adapter, enhancedToolProvider)

	ctx := context.Background()

	// Test 1: Code Generation (should use codegen tool)
	fmt.Println("\n1. Testing Code Generation...")
	req1 := &interpreter.NaturalLanguageRequest{
		Input:     "Generate a Python function to calculate fibonacci numbers",
		Context:   map[string]string{},
		SessionID: "test-session-1",
	}

	result1, err := flexibleInterpreter.Interpret(ctx, req1)
	if err != nil {
		log.Printf("❌ Error: %v", err)
	} else {
		fmt.Printf("✅ Success: %s\n", result1.Message)
		fmt.Printf("✅ Type: %s\n", result1.ResponseType)
		if result1.ToolExecutionResult != nil {
			fmt.Printf("✅ Tool Result: %v\n", result1.ToolExecutionResult.Result)
		}
	}

	// Test 2: Web Search (should use http_get tool)
	fmt.Println("\n2. Testing Web Search...")
	req2 := &interpreter.NaturalLanguageRequest{
		Input:     "Search for information about artificial intelligence",
		Context:   map[string]string{},
		SessionID: "test-session-2",
	}

	result2, err := flexibleInterpreter.Interpret(ctx, req2)
	if err != nil {
		log.Printf("❌ Error: %v", err)
	} else {
		fmt.Printf("✅ Success: %s\n", result2.Message)
		fmt.Printf("✅ Type: %s\n", result2.ResponseType)
		if result2.ToolExecutionResult != nil {
			fmt.Printf("✅ Tool Result: %v\n", result2.ToolExecutionResult.Result)
		}
	}

	// Test 3: File Operations (should use file tools)
	fmt.Println("\n3. Testing File Operations...")
	req3 := &interpreter.NaturalLanguageRequest{
		Input:     "Read the file /etc/hostname",
		Context:   map[string]string{},
		SessionID: "test-session-3",
	}

	result3, err := flexibleInterpreter.Interpret(ctx, req3)
	if err != nil {
		log.Printf("❌ Error: %v", err)
	} else {
		fmt.Printf("✅ Success: %s\n", result3.Message)
		fmt.Printf("✅ Type: %s\n", result3.ResponseType)
		if result3.ToolExecutionResult != nil {
			fmt.Printf("✅ Tool Result: %v\n", result3.ToolExecutionResult.Result)
		}
	}

	fmt.Println("\n🎉 Test completed!")
}
