package main

import (
	"context"
	"fmt"
	"strings"
)

// executeToolDirect executes a tool directly (for use by agents)
func (s *APIServer) executeToolDirect(ctx context.Context, toolID string, params map[string]interface{}) (interface{}, error) {
	// Route to appropriate tool handler based on tool ID
	if strings.HasPrefix(toolID, "mcp_") {
		if s.mcpKnowledgeServer == nil {
			return nil, fmt.Errorf("MCP knowledge server not available")
		}
		toolName := strings.TrimPrefix(toolID, "mcp_")
		return s.mcpKnowledgeServer.callTool(ctx, toolName, params)
	}

	// Handle HDN tools
	switch toolID {
	case "tool_http_get":
		url, _ := getString(params, "url")
		if strings.TrimSpace(url) == "" {
			return nil, fmt.Errorf("url required")
		}
		safeClient := NewSafeHTTPClient()
		content, err := safeClient.SafeGetWithContentCheck(ctx, url)
		if err != nil {
			return nil, fmt.Errorf("content blocked for safety: %w", err)
		}
		return map[string]interface{}{"status": 200, "body": content}, nil

	default:
		return nil, fmt.Errorf("unknown tool: %s", toolID)
	}
}

