package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
)

// handleDockerExecute handles code execution via Docker
func (s *APIServer) handleDockerExecute(w http.ResponseWriter, r *http.Request) {
	var req DockerExecutionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Validate required fields
	if req.Language == "" || req.Code == "" {
		http.Error(w, "Language and code are required", http.StatusBadRequest)
		return
	}

	// Set default timeout if not specified
	if req.Timeout == 0 {
		req.Timeout = 600
	}

	// Use the server's Docker executor with file storage
	executor := s.dockerExecutor

	// Execute code
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(req.Timeout+60)*time.Second)
	defer cancel()

	result, err := executor.ExecuteCode(ctx, &req)

	if err != nil {
		log.Printf("❌ [DOCKER] Execution failed: %v", err)
		http.Error(w, fmt.Sprintf("Execution failed: %v", err), http.StatusInternalServerError)
		return
	}

	// Return result
	response := DockerExecutionResponse{
		Success:       result.Success,
		Output:        result.Output,
		Error:         result.Error,
		ExitCode:      result.ExitCode,
		ExecutionTime: result.ExecutionTime,
		ContainerID:   result.ContainerID,
		Files:         result.Files,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleDockerPrimes handles prime number calculation via Docker
func (s *APIServer) handleDockerPrimes(w http.ResponseWriter, r *http.Request) {
	// Use the server's Docker executor with file storage
	executor := s.dockerExecutor

	// Execute prime calculation
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := executor.ExecutePrimeCalculation(ctx)
	if err != nil {
		log.Printf("❌ [DOCKER] Prime calculation failed: %v", err)
		http.Error(w, fmt.Sprintf("Prime calculation failed: %v", err), http.StatusInternalServerError)
		return
	}

	// Return result
	response := DockerExecutionResponse{
		Success:       result.Success,
		Output:        result.Output,
		Error:         result.Error,
		ExitCode:      result.ExitCode,
		ExecutionTime: result.ExecutionTime,
		ContainerID:   result.ContainerID,
		Files:         result.Files,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleDockerGenerateCode handles LLM code generation and execution
func (s *APIServer) handleDockerGenerateCode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TaskName    string            `json:"task_name"`
		Description string            `json:"description"`
		Language    string            `json:"language"`
		Context     map[string]string `json:"context"`
		Input       string            `json:"input,omitempty"`
		Timeout     int               `json:"timeout,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Validate required fields
	if req.TaskName == "" || req.Description == "" || req.Language == "" {
		http.Error(w, "Task name, description, and language are required", http.StatusBadRequest)
		return
	}

	// Set default timeout if not specified
	if req.Timeout == 0 {
		req.Timeout = 600
	}

	// Generate code using LLM
	code, err := s.llmClient.GenerateExecutableCode(req.TaskName, req.Description, req.Language, req.Context)
	if err != nil {
		log.Printf("❌ [DOCKER] Code generation failed: %v", err)
		http.Error(w, fmt.Sprintf("Code generation failed: %v", err), http.StatusInternalServerError)
		return
	}

	// Use the server's Docker executor with file storage
	executor := s.dockerExecutor

	// Execute generated code
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(req.Timeout+60)*time.Second)
	defer cancel()

	dockerReq := &DockerExecutionRequest{
		Language:    req.Language,
		Code:        code,
		Input:       req.Input,
		Timeout:     req.Timeout,
		Environment: req.Context,
	}

	result, err := executor.ExecuteCode(ctx, dockerReq)

	if err != nil {
		log.Printf("❌ [DOCKER] Execution failed: %v", err)
		http.Error(w, fmt.Sprintf("Execution failed: %v", err), http.StatusInternalServerError)
		return
	}

	// Return result
	response := struct {
		Success       bool   `json:"success"`
		GeneratedCode string `json:"generated_code"`
		Output        string `json:"output"`
		Error         string `json:"error,omitempty"`
		ExitCode      int    `json:"exit_code"`
		ExecutionTime int64  `json:"execution_time_ms"`
		ContainerID   string `json:"container_id,omitempty"`
	}{
		Success:       result.Success,
		GeneratedCode: code,
		Output:        result.Output,
		Error:         result.Error,
		ExitCode:      result.ExitCode,
		ExecutionTime: result.ExecutionTime,
		ContainerID:   result.ContainerID,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
