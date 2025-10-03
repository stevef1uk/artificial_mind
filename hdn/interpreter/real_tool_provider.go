package interpreter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

// RealToolProvider implements ToolProviderInterface using HDN server's tool execution
type RealToolProvider struct {
	hdnBaseURL string
	httpClient *http.Client
}

// NewRealToolProvider creates a new real tool provider
func NewRealToolProvider(hdnBaseURL string) *RealToolProvider {
	return &RealToolProvider{
		hdnBaseURL: hdnBaseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// GetAvailableTools returns available tools from HDN server
func (r *RealToolProvider) GetAvailableTools(ctx context.Context) ([]Tool, error) {
	log.Printf("🔧 [REAL-TOOL-PROVIDER] Getting available tools from HDN server")

	// Call HDN server's tools endpoint
	url := fmt.Sprintf("%s/api/v1/tools", r.hdnBaseURL)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get tools: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HDN server returned status %d", resp.StatusCode)
	}

	var toolsResponse struct {
		Tools []Tool `json:"tools"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&toolsResponse); err != nil {
		return nil, fmt.Errorf("failed to decode tools response: %v", err)
	}

	log.Printf("🔧 [REAL-TOOL-PROVIDER] Retrieved %d tools from HDN server", len(toolsResponse.Tools))
	return toolsResponse.Tools, nil
}

// ExecuteTool executes a tool through HDN server
func (r *RealToolProvider) ExecuteTool(ctx context.Context, toolID string, parameters map[string]interface{}) (interface{}, error) {
	log.Printf("🔧 [REAL-TOOL-PROVIDER] Executing tool: %s with parameters: %+v", toolID, parameters)

	// Call HDN server's tool invocation endpoint
	url := fmt.Sprintf("%s/api/v1/tools/%s/invoke", r.hdnBaseURL, toolID)

	// Prepare request body - parameters should be passed directly, not wrapped
	requestBody := parameters

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal parameters: %v", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute tool: %v", err)
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tool execution failed with status %d: %s", resp.StatusCode, string(body))
	}

	// Parse response
	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %v", err)
	}

	log.Printf("🔧 [REAL-TOOL-PROVIDER] Tool %s executed successfully", toolID)
	return result, nil
}
