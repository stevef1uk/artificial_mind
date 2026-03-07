package types

import "context"

// ThoughtEvent represents an AI's internal thought process
type ThoughtEvent struct {
	AgentID    string                 `json:"agent_id"`
	SessionID  string                 `json:"session_id,omitempty"`
	Type       string                 `json:"type"`       // "thinking", "decision", "action", "observation"
	State      string                 `json:"state"`      // Current FSM state
	Goal       string                 `json:"goal"`       // Current goal/objective
	Thought    string                 `json:"thought"`    // Natural language thought
	Confidence float64                `json:"confidence"` // 0.0-1.0
	ToolUsed   string                 `json:"tool_used,omitempty"`
	Action     string                 `json:"action,omitempty"`
	Result     string                 `json:"result,omitempty"`
	Timestamp  string                 `json:"timestamp"`
	Context    map[string]interface{} `json:"context,omitempty"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
}

// ThoughtExpressionServiceInterface allows storing ThoughtEvents (for dependency injection)
type ThoughtExpressionServiceInterface interface {
	StoreThoughtEvent(ctx context.Context, event ThoughtEvent) error
}
