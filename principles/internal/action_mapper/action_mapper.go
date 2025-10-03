package action_mapper

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// ActionMapper converts HDN actions to principles API format
type ActionMapper struct {
	principlesAPIURL string
	httpClient       *http.Client
}

// HDNAction represents an action from HDN system
type HDNAction struct {
	TaskName    string            `json:"task_name"`
	TaskType    string            `json:"task_type,omitempty"`
	Context     map[string]string `json:"context,omitempty"`
	State       map[string]bool   `json:"state,omitempty"`
	Description string            `json:"description,omitempty"`
}

// PrinciplesRequest represents the format expected by principles API
type PrinciplesRequest struct {
	Action  string                 `json:"action"`
	Params  map[string]interface{} `json:"params"`
	Context map[string]interface{} `json:"context"`
}

// PrinciplesResponse represents the response from principles API
type PrinciplesResponse struct {
	Result  string   `json:"result,omitempty"`
	Reasons []string `json:"reasons,omitempty"`
}

// NewActionMapper creates a new action mapper
func NewActionMapper(principlesAPIURL string) *ActionMapper {
	return &ActionMapper{
		principlesAPIURL: principlesAPIURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// MapHDNActionToPrinciples converts an HDN action to principles API format
func (am *ActionMapper) MapHDNActionToPrinciples(hdnAction HDNAction) PrinciplesRequest {
	// Extract action name from task name
	action := hdnAction.TaskName

	// Convert context to the format expected by principles
	context := make(map[string]interface{})
	for k, v := range hdnAction.Context {
		context[k] = v
	}

	// Add state information to context
	for k, v := range hdnAction.State {
		context["state_"+k] = v
	}

	// Add task type to context
	if hdnAction.TaskType != "" {
		context["task_type"] = hdnAction.TaskType
	}

	// Add description to context
	if hdnAction.Description != "" {
		context["description"] = hdnAction.Description
	}

	// Create parameters map
	params := make(map[string]interface{})
	params["task_name"] = hdnAction.TaskName
	if hdnAction.TaskType != "" {
		params["task_type"] = hdnAction.TaskType
	}
	if hdnAction.Description != "" {
		params["description"] = hdnAction.Description
	}

	// Add context as parameters too
	for k, v := range hdnAction.Context {
		params[k] = v
	}

	return PrinciplesRequest{
		Action:  action,
		Params:  params,
		Context: context,
	}
}

// CheckActionWithPrinciples sends an action to the principles API for ethical evaluation
func (am *ActionMapper) CheckActionWithPrinciples(hdnAction HDNAction) (*PrinciplesResponse, error) {
	principlesReq := am.MapHDNActionToPrinciples(hdnAction)

	// Marshal the request
	reqBody, err := json.Marshal(principlesReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %v", err)
	}

	// Send HTTP request to principles API
	resp, err := am.httpClient.Post(
		am.principlesAPIURL+"/action",
		"application/json",
		bytes.NewReader(reqBody),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to send request to principles API: %v", err)
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("principles API returned status %d", resp.StatusCode)
	}

	// Decode response
	var principlesResp PrinciplesResponse
	if err := json.NewDecoder(resp.Body).Decode(&principlesResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %v", err)
	}

	return &principlesResp, nil
}

// IsActionAllowed checks if an action is ethically allowed
func (am *ActionMapper) IsActionAllowed(hdnAction HDNAction) (bool, []string, error) {
	resp, err := am.CheckActionWithPrinciples(hdnAction)
	if err != nil {
		return false, nil, err
	}

	// If there are reasons, the action is not allowed
	return len(resp.Reasons) == 0, resp.Reasons, nil
}
