package principles

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Client provides a client interface for the principles API
type Client struct {
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

// NewClient creates a new principles client
func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// CheckAction checks if an action is ethically allowed
func (c *Client) CheckAction(action string, params, context map[string]interface{}) (*ActionResponse, error) {
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
	resp, err := c.httpClient.Post(
		c.baseURL+"/action",
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
func (c *Client) IsActionAllowed(action string, params, context map[string]interface{}) (bool, []string, error) {
	resp, err := c.CheckAction(action, params, context)
	if err != nil {
		return false, nil, err
	}

	// If there are reasons, the action is not allowed
	allowed := len(resp.Reasons) == 0
	return allowed, resp.Reasons, nil
}
