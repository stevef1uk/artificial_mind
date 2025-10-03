package conversational

// TaskResult represents the result of a task execution
type TaskResult struct {
	Success  bool                   `json:"success"`
	Result   interface{}            `json:"result"`
	Error    string                 `json:"error,omitempty"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// PlanResult represents the result of a planning operation
type PlanResult struct {
	Success  bool                   `json:"success"`
	Plan     interface{}            `json:"plan"`
	Error    string                 `json:"error,omitempty"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// LearnResult represents the result of a learning operation
type LearnResult struct {
	Success  bool                   `json:"success"`
	Learned  interface{}            `json:"learned"`
	Error    string                 `json:"error,omitempty"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// InterpretResult represents the result of a natural language interpretation
type InterpretResult struct {
	Success     bool                   `json:"success"`
	Interpreted interface{}            `json:"interpreted"`
	Error       string                 `json:"error,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}
