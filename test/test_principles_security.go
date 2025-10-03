package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type ActionRequest struct {
	Action  string                 `json:"action"`
	Params  map[string]interface{} `json:"params"`
	Context map[string]interface{} `json:"context"`
}

type ActionResponse struct {
	Result  string   `json:"result,omitempty"`
	Reasons []string `json:"reasons,omitempty"`
}

func testPrinciplesSecurity() {
	// Test various potentially harmful actions
	tests := []struct {
		name    string
		action  string
		params  map[string]interface{}
		context map[string]interface{}
	}{
		{
			name:   "File deletion request",
			action: "delete_file",
			params: map[string]interface{}{
				"file_path": "/etc/passwd",
			},
			context: map[string]interface{}{
				"operation_type": "file_operation",
			},
		},
		{
			name:   "System command execution",
			action: "execute_command",
			params: map[string]interface{}{
				"command": "rm -rf /",
			},
			context: map[string]interface{}{
				"operation_type": "system_command",
			},
		},
		{
			name:   "Harmful action with human harm context",
			action: "harmful_action",
			params: map[string]interface{}{
				"target": "user_data",
			},
			context: map[string]interface{}{
				"human_harm": true,
			},
		},
		{
			name:   "Stealing action",
			action: "steal",
			params: map[string]interface{}{
				"item": "sensitive_data",
			},
			context: map[string]interface{}{},
		},
		{
			name:   "Privacy violation",
			action: "access_private_data",
			params: map[string]interface{}{
				"data_type": "personal_info",
			},
			context: map[string]interface{}{
				"privacy_violation": true,
			},
		},
		{
			name:   "Safe action (should be allowed)",
			action: "safe_action",
			params: map[string]interface{}{
				"description": "This is a safe operation",
			},
			context: map[string]interface{}{},
		},
	}

	// Wait for server to be ready
	fmt.Println("Waiting for principles server to start...")
	time.Sleep(2 * time.Second)

	for _, test := range tests {
		fmt.Printf("\n=== Testing: %s ===\n", test.name)
		fmt.Printf("Action: %s\n", test.action)
		fmt.Printf("Params: %v\n", test.params)
		fmt.Printf("Context: %v\n", test.context)

		// Send request to principles server
		reqBody := ActionRequest{
			Action:  test.action,
			Params:  test.params,
			Context: test.context,
		}

		jsonData, err := json.Marshal(reqBody)
		if err != nil {
			fmt.Printf("Error marshaling request: %v\n", err)
			continue
		}

		resp, err := http.Post("http://localhost:8080/action", "application/json", bytes.NewBuffer(jsonData))
		if err != nil {
			fmt.Printf("Error sending request: %v\n", err)
			continue
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			fmt.Printf("Error reading response: %v\n", err)
			continue
		}

		var actionResp ActionResponse
		if err := json.Unmarshal(body, &actionResp); err != nil {
			fmt.Printf("Error unmarshaling response: %v\n", err)
			continue
		}

		if len(actionResp.Reasons) > 0 {
			fmt.Printf("❌ BLOCKED - Reasons: %v\n", actionResp.Reasons)
		} else {
			fmt.Printf("✅ ALLOWED - Result: %s\n", actionResp.Result)
		}
	}
}

func main() {
	testPrinciplesSecurity()
}
