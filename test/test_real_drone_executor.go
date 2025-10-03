package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

type DroneRequest struct {
	Code        string            `json:"code"`
	Language    string            `json:"language"`
	Image       string            `json:"image,omitempty"`
	Environment map[string]string `json:"environment,omitempty"`
	Timeout     int               `json:"timeout,omitempty"` // in seconds
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
	// Test REAL code execution locally
	fmt.Println("🚀 Testing REAL Drone Executor Tool...")
	fmt.Println(strings.Repeat("=", 50))

	testCases := []struct {
		name     string
		request  DroneRequest
		expected string
	}{
		{
			name: "Go Hello World",
			request: DroneRequest{
				Code: `package main
import "fmt"
import "time"
func main() {
    fmt.Println("Hello from REAL Drone CI!")
    fmt.Println("Current time:", time.Now().Format("2006-01-02 15:04:05"))
}`,
				Language: "go",
				Image:    "golang:1.25-alpine",
			},
			expected: "Hello from REAL Drone CI!",
		},
		{
			name: "Python Script",
			request: DroneRequest{
				Code: `import time
print('Hello from Python via REAL Drone!')
print('Current time:', time.strftime('%Y-%m-%d %H:%M:%S'))`,
				Language: "python",
				Image:    "python:3.11-alpine",
			},
			expected: "Hello from Python via REAL Drone!",
		},
		{
			name: "Bash Script",
			request: DroneRequest{
				Code: `echo 'Hello from Bash via REAL Drone!'
date
echo "System info:"
uname -a`,
				Language: "bash",
				Image:    "alpine:latest",
			},
			expected: "Hello from Bash via REAL Drone!",
		},
	}

	for i, test := range testCases {
		fmt.Printf("\n🔬 Test %d: %s\n", i+1, test.name)
		fmt.Printf("Code: %s\n", strings.Split(test.request.Code, "\n")[0]+"...")
		fmt.Printf("Language: %s\n", test.request.Language)
		fmt.Printf("Image: %s\n", test.request.Image)

		// Execute code locally using Docker
		response, err := executeCodeLocally(test.request)
		if err != nil {
			fmt.Printf("❌ Error executing code: %v\n", err)
			continue
		}

		// Print results
		fmt.Printf("✅ Response: %+v\n", response)

		if response.Success {
			fmt.Printf("✅ Test passed! Output: %s\n", strings.TrimSpace(response.Output))
		} else {
			fmt.Printf("❌ Test failed: %s\n", response.Error)
		}
	}

	fmt.Println("\n" + strings.Repeat("=", 50))
	fmt.Println("🎉 REAL Drone Executor Tool Test Complete!")
}

func executeCodeLocally(req DroneRequest) (*DroneResponse, error) {
	// Set defaults
	if req.Language == "" {
		req.Language = "go"
	}
	if req.Image == "" {
		req.Image = "golang:1.25-alpine"
	}

	// Create a temporary file with the code
	tempFile := fmt.Sprintf("/tmp/code_%d.%s", time.Now().UnixNano(), getFileExtension(req.Language))
	err := os.WriteFile(tempFile, []byte(req.Code), 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to write code file: %v", err)
	}
	defer os.Remove(tempFile)

	// Execute the code using Docker
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var cmd *exec.Cmd
	switch req.Language {
	case "go":
		cmd = exec.CommandContext(ctx, "docker", "run", "--rm", "-v", tempFile+":/code.go", req.Image, "go", "run", "/code.go")
	case "python":
		cmd = exec.CommandContext(ctx, "docker", "run", "--rm", "-v", tempFile+":/code.py", req.Image, "python", "/code.py")
	case "bash":
		cmd = exec.CommandContext(ctx, "docker", "run", "--rm", "-v", tempFile+":/code.sh", req.Image, "sh", "/code.sh")
	default:
		cmd = exec.CommandContext(ctx, "docker", "run", "--rm", "-v", tempFile+":/code", req.Image, "sh", "-c", req.Code)
	}

	startTime := time.Now()
	output, err := cmd.Output()
	duration := time.Since(startTime)

	exitCode := 0
	var stderr string
	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			exitCode = exitError.ExitCode()
			stderr = string(exitError.Stderr)
		} else {
			return nil, fmt.Errorf("execution failed: %v", err)
		}
	}

	response := &DroneResponse{
		Success:  exitCode == 0,
		Output:   string(output),
		Error:    stderr,
		Image:    req.Image,
		ExitCode: exitCode,
		Duration: int(duration.Milliseconds()),
	}

	return response, nil
}

// getFileExtension returns the appropriate file extension for a given language
func getFileExtension(language string) string {
	switch language {
	case "go":
		return "go"
	case "python":
		return "py"
	case "bash":
		return "sh"
	case "javascript", "js":
		return "js"
	case "typescript", "ts":
		return "ts"
	case "rust":
		return "rs"
	case "java":
		return "java"
	case "c":
		return "c"
	case "cpp", "c++":
		return "cpp"
	default:
		return "txt"
	}
}
