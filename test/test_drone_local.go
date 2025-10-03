package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// Test the Drone executor logic locally
func main() {
	fmt.Println("🧪 Testing Drone Executor Logic Locally")
	fmt.Println("======================================")
	fmt.Printf("OS: %s, Architecture: %s\n", runtime.GOOS, runtime.GOARCH)
	fmt.Println()

	// Test cases
	testCases := []struct {
		name     string
		code     string
		language string
		image    string
	}{
		{
			name: "Simple Go Hello World",
			code: `package main
import "fmt"
func main() {
    fmt.Println("Hello from Drone!")
    fmt.Println("This is a test of the Drone executor on RPI")
}`,
			language: "go",
			image:    "golang:1.21-alpine",
		},
		{
			name: "Simple Python Hello World",
			code: `print("Hello from Python via Drone!")
print("This is a test of the Drone executor on RPI")
import sys
print(f"Python version: {sys.version}")`,
			language: "python",
			image:    "python:3.11-alpine",
		},
		{
			name: "Simple Bash Hello World",
			code: `echo "Hello from Bash via Drone!"
echo "This is a test of the Drone executor on RPI"
echo "Current date: $(date)"
echo "Architecture: $(uname -m)"`,
			language: "bash",
			image:    "alpine:latest",
		},
	}

	for i, test := range testCases {
		fmt.Printf("\n🔬 Test %d: %s\n", i+1, test.name)
		fmt.Printf("Language: %s, Image: %s\n", test.language, test.image)
		fmt.Println(strings.Repeat("-", 40))

		// Test the execution logic
		result, err := executeCode(test.code, test.language, test.image)
		if err != nil {
			fmt.Printf("❌ Error: %v\n", err)
		} else {
			fmt.Printf("✅ Success: %t\n", result["success"])
			fmt.Printf("Output: %s\n", result["output"])
			if result["error"] != "" {
				fmt.Printf("Error: %s\n", result["error"])
			}
			fmt.Printf("Duration: %dms\n", result["duration_ms"])
		}
	}

	fmt.Println("\n🎉 Local Drone Executor Test Complete!")
}

// executeCode simulates the Drone executor execution logic
func executeCode(code, language, image string) (map[string]interface{}, error) {
	// Check if Docker is available
	if !isDockerAvailable() {
		return map[string]interface{}{
			"success":     false,
			"output":      "",
			"error":       "Docker is not available",
			"image":       image,
			"exit_code":   1,
			"duration_ms": 0,
			"method":      "local_test",
		}, nil
	}

	// Create temporary file
	tempFile := fmt.Sprintf("/tmp/drone_test_%d.%s", time.Now().UnixNano(), getFileExtension(language))
	err := os.WriteFile(tempFile, []byte(code), 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to write code file: %v", err)
	}
	defer os.Remove(tempFile)

	// Execute using Docker
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var cmd *exec.Cmd
	switch language {
	case "go":
		cmd = exec.CommandContext(ctx, "docker", "run", "--rm", "-v", tempFile+":/code.go", image, "go", "run", "/code.go")
	case "python":
		cmd = exec.CommandContext(ctx, "docker", "run", "--rm", "-v", tempFile+":/code.py", image, "python", "/code.py")
	case "bash":
		cmd = exec.CommandContext(ctx, "docker", "run", "--rm", "-v", tempFile+":/code.sh", image, "sh", "/code.sh")
	default:
		cmd = exec.CommandContext(ctx, "docker", "run", "--rm", "-v", tempFile+":/code", image, "sh", "-c", code)
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

	return map[string]interface{}{
		"success":     exitCode == 0,
		"output":      string(output),
		"error":       stderr,
		"image":       image,
		"exit_code":   exitCode,
		"duration_ms": duration.Milliseconds(),
		"method":      "local_docker_execution",
	}, nil
}

// isDockerAvailable checks if Docker is available
func isDockerAvailable() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "docker", "version")
	err := cmd.Run()
	return err == nil
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
