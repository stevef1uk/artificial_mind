package main

import (
	"context"
	"strings"
	"testing"
)

// Helper to create test executor
func newTestExecutorForTests(t *testing.T) *IntelligentExecutor {
	t.Helper()
	return &IntelligentExecutor{
		usePlanner: false,
	}
}

// ============================================================================
// Basic Functionality Tests
// ============================================================================

func TestExecuteTaskIntelligently_SimpleInformational(t *testing.T) {
	ie := newTestExecutorForTests(t)
	req := &ExecutionRequest{
		TaskName:    "Info",
		Description: "AI news today",
		Context:     make(map[string]string),
	}
	
	ctx := context.Background()
	result, err := ie.ExecuteTaskIntelligently(ctx, req)
	
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	
	if result == nil {
		t.Fatal("Expected non-nil result")
	}
	
	if !result.Success {
		t.Errorf("Expected success=true for simple informational")
	}
	
	resultStr := ""
	if result.Result != nil {
		if str, ok := result.Result.(string); ok {
			resultStr = str
		}
	}
	
	if len(resultStr) == 0 {
		t.Errorf("Expected non-empty result")
	}
	
	t.Logf("Result: %s", resultStr)
}

func TestExecuteTaskIntelligently_ContextCanceled(t *testing.T) {
	ie := newTestExecutorForTests(t)
	req := &ExecutionRequest{
		TaskName:    "Test",
		Description: "Some task",
		Context:     make(map[string]string),
	}
	
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately
	
	result, err := ie.ExecuteTaskIntelligently(ctx, req)
	
	if err != nil {
		t.Logf("Got error (may be expected): %v", err)
	}
	
	if result == nil {
		t.Fatal("Expected non-nil result even on cancellation")
	}
	
	if result.Success {
		t.Errorf("Expected success=false for canceled context")
	}
	
	if len(result.Error) == 0 {
		t.Errorf("Expected error message for canceled context")
	}
	
	t.Logf("Cancellation error: %s", result.Error)
}

// ============================================================================
// Routing Logic Tests
// ============================================================================

func TestShouldUseLLMSummarization(t *testing.T) {
	ie := newTestExecutorForTests(t)
	
	tests := []struct {
		name     string
		taskName string
		want     bool
	}{
		{"analyze_bootstrap", "analyze_bootstrap", true},
		{"analyze_belief", "analyze_belief", true},
		{"ANALYZE_BOOTSTRAP", "ANALYZE_BOOTSTRAP", true},
		{"other_task", "other_task", false},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &ExecutionRequest{TaskName: tt.taskName}
			got := ie.shouldUseLLMSummarization(req)
			if got != tt.want {
				t.Errorf("shouldUseLLMSummarization() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestShouldUseSimpleInformational(t *testing.T) {
	ie := newTestExecutorForTests(t)
	
	tests := []struct {
		name        string
		description string
		language    string
		want        bool
	}{
		{"short text", "AI news today", "", true},
		{"long text", strings.Repeat("word ", 50), "", false},
		{"has create", "create something", "", false},
		{"has language", "short", "python", false},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &ExecutionRequest{
				Description: tt.description,
				Language:    tt.language,
				Context:     make(map[string]string),
			}
			got := ie.shouldUseSimpleInformational(req)
			if got != tt.want {
				t.Errorf("shouldUseSimpleInformational() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestShouldUseHypothesisTesting(t *testing.T) {
	ie := newTestExecutorForTests(t)
	
	tests := []struct {
		name        string
		taskName    string
		description string
		context     map[string]string
		want        bool
	}{
		{
			name:        "starts with test hypothesis",
			taskName:    "test hypothesis:",
			description: "test hypothesis: AI is related to ML",
			context:     make(map[string]string),
			want:        true,
		},
		{
			name:        "context flag set",
			taskName:    "Task",
			description: "description",
			context:     map[string]string{"hypothesis_testing": "true"},
			want:        true,
		},
		{
			name:        "program creation excluded",
			taskName:    "test hypothesis:",
			description: "test hypothesis: create a python program",
			context:     make(map[string]string),
			want:        false,
		},
		{
			name:        "regular task",
			taskName:    "Regular Task",
			description: "do something",
			context:     make(map[string]string),
			want:        false,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &ExecutionRequest{
				TaskName:    tt.taskName,
				Description: tt.description,
				Context:     tt.context,
			}
			descLower := strings.ToLower(req.Description)
			taskLower := strings.ToLower(req.TaskName)
			got := ie.shouldUseHypothesisTesting(req, descLower, taskLower)
			if got != tt.want {
				t.Errorf("shouldUseHypothesisTesting() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsInternalTask(t *testing.T) {
	ie := newTestExecutorForTests(t)
	
	tests := []struct {
		taskName string
		want     bool
	}{
		{"goal execution", true},
		{"Goal Execution", true},
		{"artifact_task", true},
		{"code_generation", true},
		{"user_task", false},
		{"calculate", false},
	}
	
	for _, tt := range tests {
		t.Run(tt.taskName, func(t *testing.T) {
			got := ie.isInternalTask(strings.ToLower(tt.taskName))
			if got != tt.want {
				t.Errorf("isInternalTask(%q) = %v, want %v", tt.taskName, got, tt.want)
			}
		})
	}
}

// ============================================================================
// Hypothesis Enhancement Tests
// ============================================================================

func TestEnhanceHypothesisRequest(t *testing.T) {
	ie := newTestExecutorForTests(t)
	
	req := &ExecutionRequest{
		TaskName:    "test hypothesis:",
		Description: "test hypothesis: machine learning domain is related to AI",
		Context:     make(map[string]string),
	}
	
	descLower := strings.ToLower(req.Description)
	enhanced := ie.enhanceHypothesisRequest(req, descLower)
	
	// Check task name was modified
	if enhanced.TaskName == req.TaskName {
		t.Errorf("Task name should be modified")
	}
	
	// Check description was enhanced
	if len(enhanced.Description) <= len(req.Description) {
		t.Errorf("Description should be enhanced (longer)")
	}
	
	// Check key content is present
	enhancedLower := strings.ToLower(enhanced.Description)
	requiredTerms := []string{"neo4j", "python", "report", "hypothesis"}
	for _, term := range requiredTerms {
		if !strings.Contains(enhancedLower, term) {
			t.Errorf("Enhanced description missing key term: %s", term)
		}
	}
	
	// Check context flags were set
	if enhanced.Context["hypothesis_testing"] != "true" {
		t.Errorf("hypothesis_testing flag should be set to 'true'")
	}
	
	if enhanced.Context["artifact_names"] != "hypothesis_test_report.md" {
		t.Errorf("artifact_names should be set to 'hypothesis_test_report.md'")
	}
	
	if enhanced.Context["allow_requests"] != "true" {
		t.Errorf("allow_requests flag should be set to 'true'")
	}
	
	t.Logf("Original length: %d chars", len(req.Description))
	t.Logf("Enhanced length: %d chars", len(enhanced.Description))
	t.Logf("Task name: %s", enhanced.TaskName)
}

func TestEnhanceHypothesisRequest_NoOldStyle(t *testing.T) {
	ie := newTestExecutorForTests(t)
	
	req := &ExecutionRequest{
		TaskName:    "test hypothesis:",
		Description: "test hypothesis: test simple hypothesis",
		Context:     make(map[string]string),
	}
	
	descLower := strings.ToLower(req.Description)
	enhanced := ie.enhanceHypothesisRequest(req, descLower)
	
	// Should not have old prescriptive style with emojis
	enhancedDesc := enhanced.Description
	if strings.Contains(enhancedDesc, "🚨🚨🚨") {
		t.Errorf("Enhanced description should not contain emoji warnings (old style)")
	}
	
	// Check it's reasonably sized (not 250+ lines)
	lineCount := strings.Count(enhancedDesc, "\n")
	t.Logf("Enhanced prompt has %d lines", lineCount)
	
	if lineCount > 100 {
		t.Logf("Warning: Enhanced description has %d lines (may be too long)", lineCount)
	}
}

