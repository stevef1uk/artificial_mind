package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type DroneRequest struct {
	Code        string            `json:"code"`
	Language    string            `json:"language"`
	Image       string            `json:"image,omitempty"`
	Environment map[string]string `json:"environment,omitempty"`
	Timeout     int               `json:"timeout,omitempty"`
}

type DroneResponse struct {
	Success  bool   `json:"success"`
	Output   string `json:"output"`
	Error    string `json:"error,omitempty"`
	Image    string `json:"image,omitempty"`
	ExitCode int    `json:"exit_code,omitempty"`
	Duration int    `json:"duration_ms"`
}

func main() {
	// Test the drone executor tool via HDN server
	hdnURL := "http://localhost:8094"

	// Test 1: Simple Go code
	fmt.Println("🧪 Testing Drone Executor Tool...")
	fmt.Println(strings.Repeat("=", 50))

	testCases := []struct {
		name     string
		request  DroneRequest
		expected string
	}{
		{
			name: "Go Hello World",
			request: DroneRequest{
				Code:     "fmt.Println(\"Hello from Drone CI!\")",
				Language: "go",
				Image:    "golang:1.25-alpine",
				Timeout:  300,
			},
			expected: "simulated",
		},
		{
			name: "Python Script",
			request: DroneRequest{
				Code:     "print('Hello from Python via Drone!')",
				Language: "python",
				Image:    "python:3.11-alpine",
				Timeout:  300,
			},
			expected: "simulated",
		},
		{
			name: "Bash Script",
			request: DroneRequest{
				Code:     "echo 'Hello from Bash via Drone!'",
				Language: "bash",
				Image:    "alpine:latest",
				Timeout:  300,
			},
			expected: "simulated",
		},
	}

	for i, test := range testCases {
		fmt.Printf("\n🔬 Test %d: %s\n", i+1, test.name)
		fmt.Printf("Code: %s\n", test.request.Code)
		fmt.Printf("Language: %s\n", test.request.Language)
		fmt.Printf("Image: %s\n", test.request.Image)

		// Convert request to JSON
		jsonData, err := json.Marshal(test.request)
		if err != nil {
			fmt.Printf("❌ Error marshaling request: %v\n", err)
			continue
		}

		// Call HDN server
		response, err := callDroneExecutor(hdnURL, jsonData)
		if err != nil {
			fmt.Printf("❌ Error calling HDN server: %v\n", err)
			continue
		}

		// Print results
		fmt.Printf("✅ Response: %+v\n", response)

		if response.Success {
			fmt.Printf("✅ Test passed!\n")
		} else {
			fmt.Printf("❌ Test failed: %s\n", response.Error)
		}
	}

	fmt.Println("\n" + strings.Repeat("=", 50))
	fmt.Println("🎉 Drone Executor Tool Test Complete!")
}

func callDroneExecutor(hdnURL string, jsonData []byte) (*DroneResponse, error) {
	// Call the HDN server's drone executor tool
	url := hdnURL + "/api/v1/tools/tool_drone_executor/invoke"

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var response DroneResponse
	err = json.Unmarshal(body, &response)
	if err != nil {
		return nil, err
	}

	return &response, nil
}
