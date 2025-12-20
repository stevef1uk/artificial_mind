package interpreter

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
)

// MCPToolProvider provides MCP tools to the interpreter
type MCPToolProvider struct {
	mcpEndpoint string
	httpClient  *http.Client
}

// NewMCPToolProvider creates a new MCP tool provider
func NewMCPToolProvider(mcpEndpoint string) *MCPToolProvider {
	if mcpEndpoint == "" {
		mcpEndpoint = "http://localhost:8081/mcp"
	}
	return &MCPToolProvider{
		mcpEndpoint: mcpEndpoint,
		httpClient:  &http.Client{},
	}
}

// GetAvailableTools retrieves available tools from the MCP server
func (m *MCPToolProvider) GetAvailableTools(ctx context.Context) ([]Tool, error) {
	// Call MCP tools/list endpoint
	reqBody := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/list",
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	resp, err := m.httpClient.Post(m.mcpEndpoint, "application/json", strings.NewReader(string(jsonData)))
	if err != nil {
		return nil, fmt.Errorf("failed to call MCP server: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("MCP server returned status %d", resp.StatusCode)
	}

	var mcpResponse struct {
		JSONRPC string `json:"jsonrpc"`
		ID      int    `json:"id"`
		Result  struct {
			Tools []struct {
				Name        string                 `json:"name"`
				Description string                 `json:"description"`
				InputSchema map[string]interface{} `json:"inputSchema"`
			} `json:"tools"`
		} `json:"result"`
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error,omitempty"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&mcpResponse); err != nil {
		return nil, fmt.Errorf("failed to decode MCP response: %w", err)
	}

	if mcpResponse.Error != nil {
		return nil, fmt.Errorf("MCP error: %s", mcpResponse.Error.Message)
	}

	// Convert MCP tools to interpreter Tool format
	tools := make([]Tool, 0, len(mcpResponse.Result.Tools))
	for _, mcpTool := range mcpResponse.Result.Tools {
		// Convert input schema to string map format
		inputSchema := make(map[string]string)
		if props, ok := mcpTool.InputSchema["properties"].(map[string]interface{}); ok {
			for key, val := range props {
				if prop, ok := val.(map[string]interface{}); ok {
					if propType, ok := prop["type"].(string); ok {
						inputSchema[key] = propType
					} else {
						inputSchema[key] = "string" // default
					}
				}
			}
		}

		tool := Tool{
			ID:          fmt.Sprintf("mcp_%s", mcpTool.Name),
			Name:        mcpTool.Name,
			Description: mcpTool.Description,
			InputSchema: inputSchema,
		}
		tools = append(tools, tool)
	}

	log.Printf("✅ [MCP-TOOL-PROVIDER] Retrieved %d tools from MCP server", len(tools))
	return tools, nil
}

// ExecuteTool executes an MCP tool
func (m *MCPToolProvider) ExecuteTool(ctx context.Context, toolID string, parameters map[string]interface{}) (interface{}, error) {
	// Extract tool name from tool ID (remove "mcp_" prefix)
	toolName := strings.TrimPrefix(toolID, "mcp_")

	// Call MCP tools/call endpoint
	reqBody := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name":      toolName,
			"arguments": parameters,
		},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	resp, err := m.httpClient.Post(m.mcpEndpoint, "application/json", strings.NewReader(string(jsonData)))
	if err != nil {
		return nil, fmt.Errorf("failed to call MCP server: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("MCP server returned status %d", resp.StatusCode)
	}

	var mcpResponse struct {
		JSONRPC string      `json:"jsonrpc"`
		ID      int         `json:"id"`
		Result  interface{} `json:"result,omitempty"`
		Error   *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error,omitempty"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&mcpResponse); err != nil {
		return nil, fmt.Errorf("failed to decode MCP response: %w", err)
	}

	if mcpResponse.Error != nil {
		return nil, fmt.Errorf("MCP error: %s", mcpResponse.Error.Message)
	}

	log.Printf("✅ [MCP-TOOL-PROVIDER] Executed tool %s successfully", toolName)
	return mcpResponse.Result, nil
}

