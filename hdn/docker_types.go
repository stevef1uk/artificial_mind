package main

// DockerExecutionRequest represents a request to execute code via Docker
type DockerExecutionRequest struct {
	Language     string            `json:"language"`
	Code         string            `json:"code"`
	Input        string            `json:"input,omitempty"`
	Timeout      int               `json:"timeout,omitempty"`
	Environment  map[string]string `json:"environment,omitempty"`
	WorkflowID   string            `json:"workflow_id,omitempty"`
	StepID       string            `json:"step_id,omitempty"`
	IsValidation bool              `json:"is_validation,omitempty"`
}

// DockerExecutionResponse represents the response from Docker execution
type DockerExecutionResponse struct {
	Success       bool              `json:"success"`
	Output        string            `json:"output"`
	Error         string            `json:"error,omitempty"`
	ExitCode      int               `json:"exit_code"`
	ExecutionTime int64             `json:"execution_time_ms"`
	ContainerID   string            `json:"container_id,omitempty"`
	Files         map[string][]byte `json:"files,omitempty"` // Generated files
}
