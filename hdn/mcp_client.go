package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"time"
)

// --------- MCP Client ---------

type MCPClient struct {
	config     DomainConfig
	httpClient *http.Client
	endpoint   string
}

type MCPRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int         `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

type MCPResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int         `json:"id"`
	Result  interface{} `json:"result,omitempty"`
	Error   *MCPError   `json:"error,omitempty"`
}

type MCPError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type MCPTool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"inputSchema"`
}

type MCPToolList struct {
	Tools []MCPTool `json:"tools"`
}

type MCPToolCall struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

func NewMCPClient(config DomainConfig) *MCPClient {
	endpoint := config.MCPEndpoint
	if endpoint == "" {
		endpoint = "http://localhost:3000/mcp" // Default MCP endpoint
	}

	return &MCPClient{
		config: config,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		endpoint: endpoint,
	}
}

func (c *MCPClient) GenerateMethod(taskName, description string, context map[string]string) (*MethodDef, error) {
	// First, discover available tools
	tools, err := c.listTools()
	if err != nil {
		return nil, fmt.Errorf("failed to list MCP tools: %v", err)
	}

	// Find relevant tools for the task
	relevantTools := c.findRelevantTools(tools, taskName, description)
	if len(relevantTools) == 0 {
		return nil, fmt.Errorf("no relevant MCP tools found for task: %s", taskName)
	}

	// Generate a method using the relevant tools
	method := c.createMethodFromTools(taskName, relevantTools, context)

	return method, nil
}

func (c *MCPClient) findRelevantTools(tools []MCPTool, taskName, description string) []MCPTool {
	var relevant []MCPTool

	// Simple keyword matching - in a real implementation, this would be more sophisticated
	keywords := []string{taskName, description}

	for _, tool := range tools {
		if c.isToolRelevant(tool, keywords) {
			relevant = append(relevant, tool)
		}
	}

	return relevant
}

func (c *MCPClient) isToolRelevant(tool MCPTool, keywords []string) bool {
	// Check if any keyword appears in the tool name or description
	for _, keyword := range keywords {
		if containsString(tool.Name, keyword) || containsString(tool.Description, keyword) {
			return true
		}
	}
	return false
}

func (c *MCPClient) createMethodFromTools(taskName string, tools []MCPTool, context map[string]string) *MethodDef {
	// Create subtasks from the tools
	var subtasks []string
	var preconditions []string

	// Add a precondition that the task hasn't been completed
	preconditions = append(preconditions, "not "+taskName+"_completed")

	// Create subtasks for each tool
	for _, tool := range tools {
		subtaskName := "MCP_" + tool.Name
		subtasks = append(subtasks, subtaskName)

		// Add preconditions for tool execution
		preconditions = append(preconditions, "not "+subtaskName+"_completed")
	}

	// Add a final completion subtask
	subtasks = append(subtasks, "Complete_"+taskName)

	return &MethodDef{
		Task:          taskName,
		Preconditions: preconditions,
		Subtasks:      subtasks,
		IsLearned:     true,
	}
}

func (c *MCPClient) sendRequest(request MCPRequest) (*MCPResponse, error) {
	// Marshal request
	jsonData, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}

	// Create HTTP request
	req, err := http.NewRequest("POST", c.endpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")

	// Make the request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// Read response
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// Check for HTTP errors
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("MCP API error: %s", string(body))
	}

	// Parse response
	var response MCPResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, err
	}

	return &response, nil
}

// --------- Mock MCP Client for Testing ---------

type MockMCPClient struct {
	tools []MCPTool
}

func NewMockMCPClient() *MCPClient {
	// Return a real client but with mock behavior
	client := &MCPClient{
		config: DomainConfig{
			MCPEndpoint: "mock://localhost:3000/mcp",
		},
		httpClient: &http.Client{Timeout: 1 * time.Second},
		endpoint:   "mock://localhost:3000/mcp",
	}
	return client
}

func (c *MCPClient) listTools() ([]MCPTool, error) {
	// Mock tools for testing
	if c.endpoint == "mock://localhost:3000/mcp" {
		return c.getMockTools(), nil
	}

	// For real MCP endpoints, use the actual implementation
	return c.listToolsReal()
}

func (c *MCPClient) listToolsReal() ([]MCPTool, error) {
	request := MCPRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/list",
	}

	response, err := c.sendRequest(request)
	if err != nil {
		return nil, err
	}

	if response.Error != nil {
		return nil, fmt.Errorf("MCP list tools error: %s", response.Error.Message)
	}

	// Parse the result
	resultBytes, err := json.Marshal(response.Result)
	if err != nil {
		return nil, err
	}

	var toolList MCPToolList
	if err := json.Unmarshal(resultBytes, &toolList); err != nil {
		return nil, err
	}

	return toolList.Tools, nil
}

func (c *MCPClient) getMockTools() []MCPTool {
	return []MCPTool{
		{
			Name:        "file_read",
			Description: "Read contents of a file",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Path to the file to read",
					},
				},
				"required": []string{"path"},
			},
		},
		{
			Name:        "file_write",
			Description: "Write contents to a file",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Path to the file to write",
					},
					"content": map[string]interface{}{
						"type":        "string",
						"description": "Content to write to the file",
					},
				},
				"required": []string{"path", "content"},
			},
		},
		{
			Name:        "web_search",
			Description: "Search the web for information",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query": map[string]interface{}{
						"type":        "string",
						"description": "Search query",
					},
				},
				"required": []string{"query"},
			},
		},
		{
			Name:        "database_query",
			Description: "Execute a database query",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query": map[string]interface{}{
						"type":        "string",
						"description": "SQL query to execute",
					},
				},
				"required": []string{"query"},
			},
		},
	}
}

func (c *MCPClient) ExecuteTool(toolName string, arguments map[string]interface{}) (interface{}, error) {
	// Mock tool execution for testing
	if c.endpoint == "mock://localhost:3000/mcp" {
		return c.mockToolExecution(toolName, arguments)
	}

	// For real MCP endpoints, use the actual implementation
	return c.executeToolReal(toolName, arguments)
}

func (c *MCPClient) executeToolReal(toolName string, arguments map[string]interface{}) (interface{}, error) {
	request := MCPRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/call",
		Params: map[string]interface{}{
			"name":      toolName,
			"arguments": arguments,
		},
	}

	response, err := c.sendRequest(request)
	if err != nil {
		return nil, err
	}

	if response.Error != nil {
		return nil, fmt.Errorf("MCP tool call error: %s", response.Error.Message)
	}

	return response.Result, nil
}

func (c *MCPClient) mockToolExecution(toolName string, arguments map[string]interface{}) (interface{}, error) {
	switch toolName {
	case "file_read":
		path, ok := arguments["path"].(string)
		if !ok {
			return nil, fmt.Errorf("missing or invalid path argument")
		}
		return map[string]interface{}{
			"content": fmt.Sprintf("Mock content of file: %s", path),
			"path":    path,
		}, nil

	case "file_write":
		path, ok1 := arguments["path"].(string)
		content, ok2 := arguments["content"].(string)
		if !ok1 || !ok2 {
			return nil, fmt.Errorf("missing or invalid arguments")
		}
		return map[string]interface{}{
			"success": true,
			"path":    path,
			"bytes":   len(content),
		}, nil

	case "web_search":
		query, ok := arguments["query"].(string)
		if !ok {
			return nil, fmt.Errorf("missing or invalid query argument")
		}
		return map[string]interface{}{
			"results": []map[string]interface{}{
				{
					"title":   fmt.Sprintf("Search result for: %s", query),
					"url":     "https://example.com",
					"snippet": fmt.Sprintf("This is a mock search result for the query: %s", query),
				},
			},
		}, nil

	case "database_query":
		query, ok := arguments["query"].(string)
		if !ok {
			return nil, fmt.Errorf("missing or invalid query argument")
		}
		return map[string]interface{}{
			"rows": []map[string]interface{}{
				{"id": 1, "name": "Mock Result 1"},
				{"id": 2, "name": "Mock Result 2"},
			},
			"query": query,
		}, nil

	default:
		return nil, fmt.Errorf("unknown tool: %s", toolName)
	}
}
