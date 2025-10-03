package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type DroneExecutorRequest struct {
	Code        string            `json:"code"`
	Language    string            `json:"language"`
	Image       string            `json:"image"`
	Environment map[string]string `json:"environment"`
	Timeout     int               `json:"timeout"`
}

type DroneExecutorResponse struct {
	Success    bool   `json:"success"`
	Output     string `json:"output"`
	Error      string `json:"error"`
	Image      string `json:"image"`
	ExitCode   int    `json:"exit_code"`
	DurationMs int    `json:"duration_ms"`
}

func main() {
	fmt.Println("Testing Drone Executor Tool on RPi...")
	fmt.Printf("Current architecture: %s\n", getArchitecture())

	// Test Go code
	goCode := `package main
import "fmt"
import "runtime"
func main() {
    fmt.Println("Hello from Go on RPi!")
    fmt.Println("Architecture:", runtime.GOARCH)
}`

	// Test Python code
	pythonCode := `import platform
print("Hello from Python on RPi!")
print("Architecture:", platform.machine())
print("Platform:", platform.platform())`

	// Test Bash code
	bashCode := `echo "Hello from Bash on RPi!"
echo "Architecture: $(uname -m)"
echo "Platform: $(uname -a)"`

	tests := []struct {
		name     string
		code     string
		language string
		image    string
	}{
		{"Go Test", goCode, "go", "golang:1.21-alpine"},
		{"Python Test", pythonCode, "python", "python:3.11-alpine"},
		{"Bash Test", bashCode, "bash", "alpine:latest"},
	}

	for _, test := range tests {
		fmt.Printf("\n=== Testing %s ===\n", test.name)

		req := DroneExecutorRequest{
			Code:        test.code,
			Language:    test.language,
			Image:       test.image,
			Environment: map[string]string{},
			Timeout:     30,
		}

		// Try to call the drone executor tool via HDN API
		response, err := callDroneExecutor(req)
		if err != nil {
			fmt.Printf("Error calling drone executor: %v\n", err)
			continue
		}

		fmt.Printf("Success: %t\n", response.Success)
		fmt.Printf("Output: %s\n", response.Output)
		if response.Error != "" {
			fmt.Printf("Error: %s\n", response.Error)
		}
		fmt.Printf("Exit Code: %d\n", response.ExitCode)
		fmt.Printf("Duration: %dms\n", response.DurationMs)
	}
}

func getArchitecture() string {
	// This will show the actual architecture when run on RPi
	return "arm64" // This should be detected at runtime
}

func callDroneExecutor(req DroneExecutorRequest) (*DroneExecutorResponse, error) {
	// Try different HDN server endpoints - these should work from within the RPi cluster
	endpoints := []string{
		"http://hdn-server.agi.svc.cluster.local:8080",
		"http://localhost:8080",
	}

	for _, endpoint := range endpoints {
		fmt.Printf("Trying endpoint: %s\n", endpoint)

		// First try to get tools
		toolsURL := endpoint + "/api/v1/tools"
		resp, err := http.Get(toolsURL)
		if err != nil {
			fmt.Printf("Failed to get tools from %s: %v\n", endpoint, err)
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode != 200 {
			fmt.Printf("Tools endpoint returned status %d\n", resp.StatusCode)
			continue
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			fmt.Printf("Failed to read tools response: %v\n", err)
			continue
		}

		fmt.Printf("Tools response: %s\n", string(body))

		// Try to call drone executor tool
		executorURL := endpoint + "/api/v1/tools/tool_drone_executor"
		jsonData, err := json.Marshal(req)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request: %v", err)
		}

		resp, err = http.Post(executorURL, "application/json", bytes.NewBuffer(jsonData))
		if err != nil {
			fmt.Printf("Failed to call drone executor: %v\n", err)
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode == 200 {
			var response DroneExecutorResponse
			if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
				return nil, fmt.Errorf("failed to decode response: %v", err)
			}
			return &response, nil
		}

		fmt.Printf("Drone executor returned status %d\n", resp.StatusCode)
	}

	return nil, fmt.Errorf("failed to call drone executor on any endpoint")
}
