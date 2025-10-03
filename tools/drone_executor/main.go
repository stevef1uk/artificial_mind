package main

import (
	"encoding/json"
	"fmt"
	"os"
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
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <json_input>\n", os.Args[0])
		os.Exit(1)
	}

	var req DroneRequest
	if err := json.Unmarshal([]byte(os.Args[1]), &req); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing JSON: %v\n", err)
		os.Exit(1)
	}

	// Set defaults
	if req.Language == "" {
		req.Language = "go"
	}
	if req.Image == "" {
		req.Image = "golang:1.25-alpine"
	}
	if req.Timeout == 0 {
		req.Timeout = 300 // 5 minutes
	}

	// Create Drone CI pipeline
	pipeline := createDronePipeline(req)

	// Submit to Drone CI
	result, err := submitToDrone(pipeline)
	if err != nil {
		response := DroneResponse{
			Success: false,
			Error:   err.Error(),
		}
		output, _ := json.Marshal(response)
		fmt.Print(string(output))
		os.Exit(1)
	}

	// Return result
	output, _ := json.Marshal(result)
	fmt.Print(string(output))
}

func createDronePipeline(req DroneRequest) map[string]interface{} {
	// Create a dynamic Drone pipeline that builds and runs the code
	pipeline := map[string]interface{}{
		"kind": "pipeline",
		"type": "docker",
		"name": "dynamic-code-execution",
		"platform": map[string]string{
			"arch": "arm64",
			"os":   "linux",
		},
		"steps": []map[string]interface{}{
			{
				"name":     "build-and-run",
				"image":    req.Image,
				"commands": generateCommands(req),
				"volumes": []map[string]string{
					{
						"name": "docker",
						"path": "/var/run/docker.sock",
					},
				},
			},
		},
		"volumes": []map[string]interface{}{
			{
				"name": "docker",
				"host": map[string]string{
					"path": "/run/user/1000/docker.sock",
				},
			},
		},
	}

	return pipeline
}

func generateCommands(req DroneRequest) []string {
	commands := []string{}

	switch req.Language {
	case "go":
		commands = []string{
			"echo 'package main' > main.go",
			"echo 'import \"fmt\"' >> main.go",
			"echo 'func main() {' >> main.go",
			"echo req.Code >> main.go",
			"echo '}' >> main.go",
			"go run main.go",
		}
	case "python":
		commands = []string{
			"echo req.Code > main.py",
			"python main.py",
		}
	case "bash":
		commands = []string{
			"echo req.Code > script.sh",
			"chmod +x script.sh",
			"./script.sh",
		}
	case "docker":
		commands = []string{
			"echo req.Code > Dockerfile",
			"docker build -t dynamic-code .",
			"docker run --rm dynamic-code",
		}
	default:
		commands = []string{
			"echo 'Unsupported language: ' + req.Language",
			"exit 1",
		}
	}

	return commands
}

func submitToDrone(pipeline map[string]interface{}) (*DroneResponse, error) {
	// For now, simulate the execution locally
	// In a real implementation, this would submit to Drone CI API

	// This is a placeholder - you'd need to implement actual Drone CI submission
	// or use a different approach like direct Docker execution

	response := &DroneResponse{
		Success:  true,
		Output:   "Code execution simulated - would run via Drone CI",
		Duration: 1000,
	}

	return response, nil
}
