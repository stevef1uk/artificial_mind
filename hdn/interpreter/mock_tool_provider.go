package interpreter

import (
	"context"
	"fmt"
	"log"
)

// MockToolProvider provides a simple mock implementation of ToolProviderInterface
type MockToolProvider struct{}

// GetAvailableTools returns a list of mock tools
func (m *MockToolProvider) GetAvailableTools(ctx context.Context) ([]Tool, error) {
	log.Printf("🔧 [MOCK-TOOL-PROVIDER] Providing mock tools")

	return []Tool{
		{
			ID:          "tool_codegen",
			Description: "Generate code in various programming languages",
		},
		{
			ID:          "tool_http_get",
			Description: "Make HTTP GET requests to fetch data from URLs",
		},
		{
			ID:          "tool_file_read",
			Description: "Read files from the filesystem",
		},
		{
			ID:          "tool_ls",
			Description: "List directory contents",
		},
		{
			ID:          "tool_calculate",
			Description: "Perform mathematical calculations",
		},
	}, nil
}

// ExecuteTool simulates tool execution
func (m *MockToolProvider) ExecuteTool(ctx context.Context, toolID string, parameters map[string]interface{}) (interface{}, error) {
	log.Printf("🔧 [MOCK-TOOL-PROVIDER] Executing tool: %s", toolID)

	switch toolID {
	case "tool_codegen":
		return map[string]interface{}{
			"success":  true,
			"code":     "print('Hello, World!')",
			"language": "python",
		}, nil
	case "tool_http_get":
		return map[string]interface{}{
			"success": true,
			"url":     parameters["url"],
			"status":  "200",
			"content": "Mock HTTP response",
		}, nil
	case "tool_file_read":
		return map[string]interface{}{
			"success": true,
			"path":    parameters["path"],
			"content": "Mock file content",
		}, nil
	case "tool_ls":
		return map[string]interface{}{
			"success": true,
			"path":    parameters["path"],
			"files":   []string{"file1.txt", "file2.txt", "directory1"},
		}, nil
	case "tool_calculate":
		return map[string]interface{}{
			"success":    true,
			"result":     "42",
			"expression": parameters["expression"],
		}, nil
	default:
		return nil, fmt.Errorf("unknown tool: %s", toolID)
	}
}
