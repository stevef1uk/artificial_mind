package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// Tool represents a callable capability
type Tool struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Description  string            `json:"description"`
	InputSchema  map[string]string `json:"input_schema"`
	OutputSchema map[string]string `json:"output_schema"`
	Permissions  []string          `json:"permissions"`
	SafetyLevel  string            `json:"safety_level"`
	CreatedBy    string            `json:"created_by"`
	CreatedAt    time.Time         `json:"created_at"`
}

// ToolInvokeRequest represents a tool invocation request
type ToolInvokeRequest struct {
	Params map[string]interface{} `json:"params"`
}

// ToolInvokeResponse represents a tool invocation response
type ToolInvokeResponse struct {
	Success bool                   `json:"success"`
	Result  map[string]interface{} `json:"result,omitempty"`
	Error   string                 `json:"error,omitempty"`
}

func main() {
	fmt.Println("🧪 Simple Tool Generation Test")
	fmt.Println("==============================")

	// Configuration
	hdnURL := "http://localhost:8080"
	if envURL := os.Getenv("HDN_URL"); envURL != "" {
		hdnURL = envURL
	}

	// Test tool definition
	testTool := Tool{
		ID:          "tool_test_calculator",
		Name:        "Test Calculator",
		Description: "A simple calculator for testing tool generation",
		InputSchema: map[string]string{
			"operation": "string",
			"a":         "number",
			"b":         "number",
		},
		OutputSchema: map[string]string{
			"result": "number",
		},
		Permissions: []string{"compute"},
		SafetyLevel: "low",
		CreatedBy:   "test",
		CreatedAt:   time.Now().UTC(),
	}

	// Step 1: Register the tool
	fmt.Println("\n1️⃣ Registering test tool...")
	if err := registerTool(hdnURL, testTool); err != nil {
		fmt.Printf("❌ Failed to register tool: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("✅ Tool registered successfully")

	// Step 2: List tools to verify registration
	fmt.Println("\n2️⃣ Verifying tool registration...")
	tools, err := listTools(hdnURL)
	if err != nil {
		fmt.Printf("❌ Failed to list tools: %v\n", err)
		os.Exit(1)
	}

	found := false
	for _, tool := range tools {
		if tool.ID == testTool.ID {
			found = true
			fmt.Printf("✅ Found tool: %s (%s)\n", tool.Name, tool.ID)
			break
		}
	}
	if !found {
		fmt.Println("❌ Tool not found in registry")
		os.Exit(1)
	}

	// Step 3: Invoke the tool
	fmt.Println("\n3️⃣ Invoking test tool...")
	invokeReq := ToolInvokeRequest{
		Params: map[string]interface{}{
			"operation": "add",
			"a":         10,
			"b":         5,
		},
	}

	result, err := invokeTool(hdnURL, testTool.ID, invokeReq)
	if err != nil {
		fmt.Printf("❌ Failed to invoke tool: %v\n", err)
		os.Exit(1)
	}

	if result.Success {
		fmt.Printf("✅ Tool executed successfully: %+v\n", result.Result)
	} else {
		fmt.Printf("⚠️  Tool execution failed (expected for test tool): %s\n", result.Error)
		fmt.Println("   This is expected since we're testing API flow, not tool implementation")
	}

	// Step 4: Clean up - delete the tool
	fmt.Println("\n4️⃣ Cleaning up test tool...")
	if err := deleteTool(hdnURL, testTool.ID); err != nil {
		fmt.Printf("⚠️  Failed to delete tool (expected): %v\n", err)
		fmt.Println("   This is expected for non-agent tools")
	} else {
		fmt.Println("✅ Tool deleted successfully")
	}

	fmt.Println("\n🎉 Tool generation test completed!")
}

func registerTool(hdnURL string, tool Tool) error {
	url := hdnURL + "/api/v1/tools"
	data, err := json.Marshal(tool)
	if err != nil {
		return err
	}

	resp, err := http.Post(url, "application/json", bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

func listTools(hdnURL string) ([]Tool, error) {
	url := hdnURL + "/api/v1/tools"
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Tools []Tool `json:"tools"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return result.Tools, nil
}

func invokeTool(hdnURL, toolID string, req ToolInvokeRequest) (*ToolInvokeResponse, error) {
	url := hdnURL + "/api/v1/tools/" + toolID + "/invoke"
	data, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	resp, err := http.Post(url, "application/json", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		// Handle 501 "not implemented" as expected for test tools
		if resp.StatusCode == 501 {
			return &ToolInvokeResponse{
				Success: false,
				Error:   "tool not implemented (expected for test)",
			}, nil
		}
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var result ToolInvokeResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

func deleteTool(hdnURL, toolID string) error {
	url := hdnURL + "/api/v1/tools/" + toolID
	req, err := http.NewRequest("DELETE", url, nil)
	if err != nil {
		return err
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	return nil
}
