package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// PrinciplesClient provides a client interface for the principles API
type PrinciplesClient struct {
	baseURL    string
	httpClient *http.Client
}

// ActionRequest represents a request to check an action
type ActionRequest struct {
	Action  string                 `json:"action"`
	Params  map[string]interface{} `json:"params"`
	Context map[string]interface{} `json:"context"`
}

// ActionResponse represents the response from the principles API
type ActionResponse struct {
	Result  string   `json:"result,omitempty"`
	Reasons []string `json:"reasons,omitempty"`
}

// NewPrinciplesClient creates a new principles client
func NewPrinciplesClient(baseURL string) *PrinciplesClient {
	return &PrinciplesClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// CheckAction checks if an action is ethically allowed
func (pc *PrinciplesClient) CheckAction(action string, params, context map[string]interface{}) (*ActionResponse, error) {
	req := ActionRequest{
		Action:  action,
		Params:  params,
		Context: context,
	}

	// Marshal request
	reqBody, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %v", err)
	}

	// Send HTTP request
	resp, err := pc.httpClient.Post(
		pc.baseURL+"/action",
		"application/json",
		bytes.NewReader(reqBody),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %v", err)
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("principles API returned status %d", resp.StatusCode)
	}

	// Decode response
	var actionResp ActionResponse
	if err := json.NewDecoder(resp.Body).Decode(&actionResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %v", err)
	}

	return &actionResp, nil
}

// IsActionAllowed returns true if the action is allowed, false otherwise
func (pc *PrinciplesClient) IsActionAllowed(action string, params, context map[string]interface{}) (bool, []string, error) {
	resp, err := pc.CheckAction(action, params, context)
	if err != nil {
		return false, nil, err
	}

	// If there are reasons, the action is not allowed
	return len(resp.Reasons) == 0, resp.Reasons, nil
}

// ExecuteActionIfAllowed checks if an action is allowed and returns the result
func (pc *PrinciplesClient) ExecuteActionIfAllowed(action string, params, context map[string]interface{}, executor func(map[string]interface{}) string) (string, []string, error) {
	// First check if action is allowed
	allowed, reasons, err := pc.IsActionAllowed(action, params, context)
	if err != nil {
		return "", nil, err
	}

	if !allowed {
		return "", reasons, nil
	}

	// Action is allowed, execute it
	result := executor(params)
	return result, nil, nil
}
