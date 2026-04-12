package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// executeWithSSHTool executes code using the SSH executor tool
// isValidation indicates if this is a validation attempt (true) or final execution (false)
// workflowID is the workflow ID to use for file storage
func (ie *IntelligentExecutor) executeWithSSHTool(ctx context.Context, code, language string, env map[string]string, isValidation bool, workflowID string) (*DockerExecutionResponse, error) {

	execMethod := strings.TrimSpace(os.Getenv("EXECUTION_METHOD"))
	isARM64 := runtime.GOARCH == "arm64" || runtime.GOARCH == "aarch64"

	sshEnabled := false
	if execMethod == "ssh" {

		sshEnabled = true
	} else if isARM64 {

		enableARM64Tools := strings.TrimSpace(os.Getenv("ENABLE_ARM64_TOOLS")) == "true"
		sshEnabled = enableARM64Tools || execMethod != "docker"
	}

	if !sshEnabled {
		log.Printf("🔁 [INTELLIGENT] SSH executor disabled (EXECUTION_METHOD=%s, ENABLE_ARM64_TOOLS=%s, GOARCH=%s) — falling back to local Docker executor",
			execMethod, os.Getenv("ENABLE_ARM64_TOOLS"), runtime.GOARCH)

		if ie.dockerExecutor == nil {
			err := fmt.Errorf("docker executor unavailable for SSH fallback")
			return &DockerExecutionResponse{Success: false, Error: err.Error(), ExitCode: 1}, err
		}

		dockerEnv := map[string]string{"QUIET": "0"}
		for k, v := range env {
			dockerEnv[k] = v
		}

		req := &DockerExecutionRequest{
			Language:     language,
			Code:         code,
			Timeout:      300,
			Environment:  dockerEnv,
			IsValidation: isValidation,
			WorkflowID:   workflowID,
		}

		startTime := time.Now()
		resp, err := ie.dockerExecutor.ExecuteCode(ctx, req)
		duration := time.Since(startTime).Milliseconds()

		if ie.toolMetrics != nil {
			status := "success"
			errorMsg := ""
			if err != nil || !resp.Success {
				status = "failure"
				if err != nil {
					errorMsg = err.Error()
				} else if resp.Error != "" {
					errorMsg = resp.Error
				}
			}
			callLog := &ToolCallLog{
				ToolID:   "tool_docker_exec",
				ToolName: "Docker Exec",
				Parameters: map[string]interface{}{
					"language": language,
					"code":     code,
					"timeout":  300,
				},
				Status:    status,
				Error:     errorMsg,
				Duration:  duration,
				Timestamp: time.Now(),
				Response: map[string]interface{}{
					"success":   resp.Success,
					"output":    resp.Output,
					"exit_code": resp.ExitCode,
				},
			}
			_ = ie.toolMetrics.LogToolCall(ctx, callLog)
		}

		if err != nil {
			return resp, err
		}
		return resp, nil
	}

	params := map[string]interface{}{
		"code":     code,
		"language": language,
		"image":    ie.getImageForLanguage(language),
		"timeout":  300,
	}

	if len(env) > 0 {
		envJSON, err := json.Marshal(env)
		if err == nil {
			params["environment"] = string(envJSON)
		}
	}

	result, err := ie.callTool("tool_ssh_executor", params)
	if err != nil {
		msg := fmt.Sprintf("SSH tool call failed: %v", err)

		low := strings.ToLower(msg)
		missing := strings.Contains(low, "status 404") ||
			strings.Contains(low, "status 501") ||
			strings.Contains(low, "not found") ||
			strings.Contains(low, "tool not available") ||
			strings.Contains(low, "tool not implemented")
		disabled := strings.Contains(low, "ssh executor disabled")
		if missing {

			if runtime.GOARCH == "arm64" || runtime.GOARCH == "aarch64" || strings.TrimSpace(os.Getenv("ENABLE_ARM64_TOOLS")) == "true" {
				log.Printf("❌ [INTELLIGENT] SSH tool expected but missing on this platform (GOARCH=%s, ENABLE_ARM64_TOOLS=%s): %v", runtime.GOARCH, os.Getenv("ENABLE_ARM64_TOOLS"), err)
			}
			log.Printf("🔁 [INTELLIGENT] Falling back to local Docker executor")

			if ie.dockerExecutor == nil {
				return &DockerExecutionResponse{Success: false, Error: "docker executor unavailable", ExitCode: 1}, fmt.Errorf("docker executor unavailable")
			}
			req := &DockerExecutionRequest{
				Language:     language,
				Code:         code,
				Timeout:      300,
				Environment:  map[string]string{"QUIET": "0"},
				IsValidation: true,
			}
			startTime := time.Now()
			resp, derr := ie.dockerExecutor.ExecuteCode(ctx, req)
			duration := time.Since(startTime).Milliseconds()

			if ie.toolMetrics != nil {
				status := "success"
				errorMsg := ""
				if derr != nil || !resp.Success {
					status = "failure"
					if derr != nil {
						errorMsg = derr.Error()
					} else if resp.Error != "" {
						errorMsg = resp.Error
					}
				}
				callLog := &ToolCallLog{
					ToolID:   "tool_docker_exec",
					ToolName: "Docker Exec",
					Parameters: map[string]interface{}{
						"language": language,
						"code":     code,
						"timeout":  300,
					},
					Status:    status,
					Error:     errorMsg,
					Duration:  duration,
					Timestamp: time.Now(),
					Response: map[string]interface{}{
						"success":   resp.Success,
						"output":    resp.Output,
						"exit_code": resp.ExitCode,
					},
				}
				_ = ie.toolMetrics.LogToolCall(ctx, callLog)
			}

			if derr != nil {
				return &DockerExecutionResponse{Success: false, Error: derr.Error(), ExitCode: 1}, derr
			}
			return resp, nil
		} else if disabled {
			log.Printf("🔁 [INTELLIGENT] SSH tool disabled according to API response — falling back to local Docker executor")
			if ie.dockerExecutor == nil {
				return &DockerExecutionResponse{Success: false, Error: "docker executor unavailable", ExitCode: 1}, fmt.Errorf("docker executor unavailable")
			}
			req := &DockerExecutionRequest{
				Language:     language,
				Code:         code,
				Timeout:      300,
				Environment:  map[string]string{"QUIET": "0"},
				IsValidation: true,
			}
			startTime := time.Now()
			resp, derr := ie.dockerExecutor.ExecuteCode(ctx, req)
			duration := time.Since(startTime).Milliseconds()

			if ie.toolMetrics != nil {
				status := "success"
				errorMsg := ""
				if derr != nil || !resp.Success {
					status = "failure"
					if derr != nil {
						errorMsg = derr.Error()
					} else if resp.Error != "" {
						errorMsg = resp.Error
					}
				}
				callLog := &ToolCallLog{
					ToolID:   "tool_docker_exec",
					ToolName: "Docker Exec",
					Parameters: map[string]interface{}{
						"language": language,
						"code":     code,
						"timeout":  300,
					},
					Status:    status,
					Error:     errorMsg,
					Duration:  duration,
					Timestamp: time.Now(),
					Response: map[string]interface{}{
						"success":   resp.Success,
						"output":    resp.Output,
						"exit_code": resp.ExitCode,
					},
				}
				_ = ie.toolMetrics.LogToolCall(ctx, callLog)
			}

			if derr != nil {
				return &DockerExecutionResponse{Success: false, Error: derr.Error(), ExitCode: 1}, derr
			}
			return resp, nil
		}
		return &DockerExecutionResponse{
			Success:  false,
			Error:    msg,
			ExitCode: 1,
		}, err
	}

	success, _ := result["success"].(bool)
	output, _ := result["output"].(string)
	errorMsg, _ := result["error"].(string)
	exitCode := 0
	if ec, ok := result["exit_code"].(float64); ok {
		exitCode = int(ec)
	}

	return &DockerExecutionResponse{
		Success:  success,
		Output:   output,
		Error:    errorMsg,
		ExitCode: exitCode,
		Files:    make(map[string][]byte),
	}, nil
}

// getImageForLanguage returns the appropriate Docker image for a language
func (ie *IntelligentExecutor) getImageForLanguage(language string) string {
	switch strings.ToLower(language) {
	case "go":
		return "golang:1.21-alpine"
	case "python", "py":
		return "python:3.11-slim"
	case "javascript", "js", "node":
		return "node:18-slim"
	case "bash", "sh":
		return "alpine:latest"
	default:
		return "alpine:latest"
	}
}

// extractFileFromSSH extracts a file from the SSH execution host
// Files are typically created in temp directories or the current working directory
func (ie *IntelligentExecutor) extractFileFromSSH(ctx context.Context, filename, language string) ([]byte, error) {

	rpiHost := os.Getenv("RPI_HOST")
	if rpiHost == "" {
		rpiHost = "192.168.1.58"
	}

	searchPaths := []string{
		"./" + filename,
		"/home/pi/" + filename,
		"/home/pi/.hdn/tmp/" + filename,
		"/tmp/" + filename,
	}

	if language == "go" {
		searchPaths = append(searchPaths, "/home/pi/.hdn/go_tmp_*/"+filename)
	} else if language == "java" {
		searchPaths = append(searchPaths, "/home/pi/.hdn/java_tmp_*/"+filename)
	}

	for _, path := range searchPaths {
		// Use find command for glob patterns, cat for regular paths
		var cmd *exec.Cmd
		if strings.Contains(path, "*") {

			findCmd := fmt.Sprintf("find /home/pi/.hdn -name '%s' -type f 2>/dev/null | head -1 | xargs cat 2>/dev/null", filename)
			cmd = exec.CommandContext(ctx, "ssh", "-o", "StrictHostKeyChecking=no", "-o", "UserKnownHostsFile=/dev/null", "-o", "LogLevel=ERROR",
				"pi@"+rpiHost, "sh", "-c", findCmd)
		} else {

			cmd = exec.CommandContext(ctx, "ssh", "-o", "StrictHostKeyChecking=no", "-o", "UserKnownHostsFile=/dev/null", "-o", "LogLevel=ERROR",
				"pi@"+rpiHost, "cat", path)
		}

		var stdout bytes.Buffer
		var stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		err := cmd.Run()
		if err == nil && stdout.Len() > 0 {
			log.Printf("✅ [INTELLIGENT] Extracted file %s from %s (%d bytes)", filename, path, stdout.Len())
			return stdout.Bytes(), nil
		}
	}

	findCmd := fmt.Sprintf("find /home/pi -name '%s' -type f 2>/dev/null | head -1 | xargs cat 2>/dev/null", filename)
	cmd := exec.CommandContext(ctx, "ssh", "-o", "StrictHostKeyChecking=no", "-o", "UserKnownHostsFile=/dev/null", "-o", "LogLevel=ERROR",
		"pi@"+rpiHost, "sh", "-c", findCmd)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err == nil && stdout.Len() > 0 {
		log.Printf("✅ [INTELLIGENT] Extracted file %s via find search (%d bytes)", filename, stdout.Len())
		return stdout.Bytes(), nil
	}

	return nil, fmt.Errorf("file %s not found on SSH host", filename)
}
