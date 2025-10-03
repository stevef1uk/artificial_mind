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

type SimpleTest struct {
	Name     string
	Code     string
	Language string
	Image    string
}

func main() {
	fmt.Println("🧪 Testing Simple Drone Executor")
	fmt.Println("=================================")
	fmt.Printf("OS: %s\n", runtime.GOOS)
	fmt.Printf("Architecture: %s\n", runtime.GOARCH)
	fmt.Println()

	// Check if Docker is available
	if !isDockerAvailable() {
		fmt.Println("❌ Docker is not available or not running")
		fmt.Println("Please ensure Docker is installed and running")
		return
	}
	fmt.Println("✅ Docker is available")

	// Test cases
	tests := []SimpleTest{
		{
			Name: "Simple Go Hello World",
			Code: `package main
import "fmt"
func main() {
    fmt.Println("Hello from Drone!")
}`,
			Language: "go",
			Image:    "golang:1.21-alpine",
		},
		{
			Name: "Simple Python Hello World",
			Code: `print("Hello from Python via Drone!")
import sys
print(f"Python version: {sys.version}")`,
			Language: "python",
			Image:    "python:3.11-alpine",
		},
		{
			Name: "Simple Bash Hello World",
			Code: `echo "Hello from Bash via Drone!"
echo "Current date: $(date)"
echo "Architecture: $(uname -m)"`,
			Language: "bash",
			Image:    "alpine:latest",
		},
	}

	// Run tests
	for i, test := range tests {
		fmt.Printf("\n🔬 Test %d: %s\n", i+1, test.Name)
		fmt.Printf("Language: %s, Image: %s\n", test.Language, test.Image)
		fmt.Println(strings.Repeat("-", 40))

		success, output, err := runSimpleTest(test)
		if err != nil {
			fmt.Printf("❌ Error: %v\n", err)
		} else if success {
			fmt.Printf("✅ Success!\nOutput:\n%s\n", output)
		} else {
			fmt.Printf("❌ Failed\nOutput:\n%s\n", output)
		}
	}

	fmt.Println("\n🎉 Simple Drone Test Complete!")
}

func isDockerAvailable() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "docker", "version")
	err := cmd.Run()
	return err == nil
}

func runSimpleTest(test SimpleTest) (bool, string, error) {
	// Create temporary file
	tempFile := fmt.Sprintf("/tmp/drone_test_%d.%s", time.Now().UnixNano(), getFileExtension(test.Language))
	err := os.WriteFile(tempFile, []byte(test.Code), 0644)
	if err != nil {
		return false, "", fmt.Errorf("failed to write temp file: %v", err)
	}
	defer os.Remove(tempFile)

	// Build Docker command
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var cmd *exec.Cmd
	switch test.Language {
	case "go":
		cmd = exec.CommandContext(ctx, "docker", "run", "--rm", "-v", tempFile+":/code.go", test.Image, "go", "run", "/code.go")
	case "python":
		cmd = exec.CommandContext(ctx, "docker", "run", "--rm", "-v", tempFile+":/code.py", test.Image, "python", "/code.py")
	case "bash":
		cmd = exec.CommandContext(ctx, "docker", "run", "--rm", "-v", tempFile+":/code.sh", test.Image, "sh", "/code.sh")
	default:
		return false, "", fmt.Errorf("unsupported language: %s", test.Language)
	}

	// Execute command
	output, err := cmd.CombinedOutput()
	outputStr := string(output)

	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			return false, outputStr, fmt.Errorf("command failed with exit code %d", exitError.ExitCode())
		}
		return false, outputStr, fmt.Errorf("execution failed: %v", err)
	}

	return true, outputStr, nil
}

func getFileExtension(language string) string {
	switch language {
	case "go":
		return "go"
	case "python":
		return "py"
	case "bash":
		return "sh"
	default:
		return "txt"
	}
}
