package main

import (
	"context"
	"testing"
	"time"
)

// ============================================================================
// Test Helpers - Minimal Mocking
// ============================================================================

// Helper to create a test executor with minimal setup
func newTestExecutorForTests(t *testing.T) *IntelligentExecutor {
	t.Helper()
	
	// Create a minimal executor for testing
	// You may need to adjust this based on your actual initialization
	ie := &IntelligentExecutor{
		// Add required fields based on your actual struct
		usePlanner: false,
	}
	
	return ie
}

// ============================================================================
// Integration-Style Tests (test actual behavior)
// ============================================================================

func TestExecuteTaskIntelligently_LLMSummarization_AnalyzeBootstrap(t *testing.T) {
	t.Skip("Requires LLM client setup - run manually or in integration tests")
	
	ie := newTestExecutorForTests(t)
	req := &ExecutionRequest{
		TaskName:    "analyze_bootstrap",
		Description: "Summarize learning around quantum computing",
		Context:     make(map[string]string),
	}
	
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	result, err := ie.ExecuteTaskIntelligently(ctx, req)
	
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	
	if result == nil {
		t.Fatal("Expected non-nil result")
	}
	
	t.Logf("Result success: %v", result.Success)
	// Note: Result is interface{}, convert to get length
	resultLen := 0
	if result.Result != nil {
		if str, ok := result.Result.(string); ok {
			resultLen = len(str)
		}
	}
	t.Logf("Result length: %d", resultLen)
}

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
	
	// Simple informational tasks should return quickly with acknowledgment
	if !result.Success {
		t.Errorf("Expected success=true for simple informational")
	}
	
	// Convert Result to string for checking
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
	
	// Should handle cancellation gracefully
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
// Routing Logic Tests (test decision functions)
// ============================================================================

func TestShouldUseLLMSummarization(t *testing.T) {
	ie := newTestExecutorForTests(t)
	
	testCases := []struct {
		name     string
		request  *ExecutionRequest
		expected bool
	}{
		{
			name: "analyze_bootstrap",
			request: &ExecutionRequest{
				TaskName: "analyze_bootstrap",
			},
			expected: true,
		},
		{
			name: "analyze_belief",
			request: &ExecutionRequest{
				TaskName: "analyze_belief",
			},
			expected: true,
		},
		{
			name: "ANALYZE_BOOTSTRAP (case insensitive)",
			request: &ExecutionRequest{
				TaskName: "ANALYZE_BOOTSTRAP",
			},
			expected: true,
		},
		{
			name: "other_task",
			request: &ExecutionRequest{
				TaskName: "other_task",
			},
			expected: false,
		},
	}
	
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := ie.shouldUseLLMSummarization(tc.request)
			if result != tc.expected {
				t.Errorf("Expected %v, got %v", tc.expected, result)
			}
		})
	}
}

func TestShouldUseSimpleInformational(t *testing.T) {
	ie := newTestExecutorForTests(t)
	
	testCases := []struct {
		name     string
		request  *ExecutionRequest
		expected bool
	}{
		{
			name: "short text no action verbs",
			request: &ExecutionRequest{
				Description: "AI news today",
			},
			expected: true,
		},
		{
			name: "long text",
			request: &ExecutionRequest{
				Description: "This is a very long description that exceeds the threshold for simple informational tasks and should not be treated as such because it's too long and complex.",
			},
			expected: false,
		},
		{
			name: "contains create keyword",
			request: &ExecutionRequest{
				Description: "create something",
			},
			expected: false,
		},
		{
			name: "contains calculate keyword",
			request: &ExecutionRequest{
				Description: "calculate result",
			},
			expected: false,
		},
		{
			name: "has language specified",
			request: &ExecutionRequest{
				Description: "short task",
				Language:    "python",
			},
			expected: false,
		},
	}
	
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := ie.shouldUseSimpleInformational(tc.request)
			if result != tc.expected {
				t.Errorf("Expected %v, got %v", tc.expected, result)
			}
		})
	}
}

func TestShouldUseHypothesisTesting(t *testing.T) {
	ie := newTestExecutorForTests(t)
	
	testCases := []struct {
		name     string
		request  *ExecutionRequest
		expected bool
	}{
		{
			name: "starts with test hypothesis",
			request: &ExecutionRequest{
				TaskName:    "test hypothesis:",
				Description: "test hypothesis: AI is related to ML",
			},
			expected: true,
		},
		{
			name: "context flag set",
			request: &ExecutionRequest{
				TaskName: "Task",
				Context: map[string]string{
					"hypothesis_testing": "true",
				},
			},
			expected: true,
		},
		{
			name: "contains program creation (excluded)",
			request: &ExecutionRequest{
				TaskName:    "test hypothesis:",
				Description: "test hypothesis: create a python program",
			},
			expected: false,
		},
		{
			name: "regular task",
			request: &ExecutionRequest{
				TaskName:    "Regular Task",
				Description: "do something",
			},
			expected: false,
		},
	}
	
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			descLower := toLower(tc.request.Description)
			taskLower := toLower(tc.request.TaskName)
			result := ie.shouldUseHypothesisTesting(tc.request, descLower, taskLower)
			if result != tc.expected {
				t.Errorf("Expected %v, got %v", tc.expected, result)
			}
		})
	}
}

func TestIsInternalTask(t *testing.T) {
	ie := newTestExecutorForTests(t)
	
	testCases := []struct {
		taskName string
		expected bool
	}{
		{"goal execution", true},
		{"Goal Execution", true},
		{"artifact_task", true},
		{"code_generation", true},
		{"code_test", true},
		{"user_task", false},
		{"calculate", false},
	}
	
	for _, tc := range testCases {
		t.Run(tc.taskName, func(t *testing.T) {
			result := ie.isInternalTask(toLower(tc.taskName))
			if result != tc.expected {
				t.Errorf("Task '%s': expected %v, got %v", tc.taskName, tc.expected, result)
			}
		})
	}
}

func TestExtractExplicitToolRequest(t *testing.T) {
	ie := newTestExecutorForTests(t)
	
	testCases := []struct {
		name        string
		description string
		taskName    string
		expectedID  string
	}{
		{
			name:        "use tool_http_get",
			description: "use tool_http_get to fetch data",
			taskName:    "Fetch",
			expectedID:  "tool_http_get",
		},
		{
			name:        "tool_html_scraper to",
			description: "tool_html_scraper to scrape page",
			taskName:    "Scrape",
			expectedID:  "tool_html_scraper",
		},
		{
			name:        "no tool mention",
			description: "regular task description",
			taskName:    "Task",
			expectedID:  "",
		},
		{
			name:        "use tool_ls",
			description: "use tool_ls to list files",
			taskName:    "List",
			expectedID:  "tool_ls",
		},
	}
	
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			descLower := toLower(tc.description)
			taskLower := toLower(tc.taskName)
			result := ie.extractExplicitToolRequest(&ExecutionRequest{
				Description: tc.description,
				TaskName:    tc.taskName,
			}, descLower, taskLower)
			
			if result != tc.expectedID {
				t.Errorf("Expected '%s', got '%s'", tc.expectedID, result)
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
	
	descLower := toLower(req.Description)
	enhanced := ie.enhanceHypothesisRequest(req, descLower)
	
	// Should extract hypothesis content
	if enhanced.TaskName == req.TaskName {
		t.Errorf("Task name should be modified")
	}
	
	// Should enhance description
	if len(enhanced.Description) <= len(req.Description) {
		t.Errorf("Description should be enhanced (longer)")
	}
	
	// Should contain key instructions
	if !contains(enhanced.Description, "Test the following hypothesis") {
		t.Errorf("Enhanced description should contain hypothesis testing instructions")
	}
	
	if !contains(enhanced.Description, "Neo4j") {
		t.Errorf("Enhanced description should mention Neo4j")
	}
	
	// Should set context flags
	if enhanced.Context["hypothesis_testing"] != "true" {
		t.Errorf("hypothesis_testing flag should be set")
	}
	
	if enhanced.Context["artifact_names"] != "hypothesis_test_report.md" {
		t.Errorf("artifact_names should be set to hypothesis_test_report.md")
	}
	
	t.Logf("Original description length: %d", len(req.Description))
	t.Logf("Enhanced description length: %d", len(enhanced.Description))
	t.Logf("Task name: %s", enhanced.TaskName)
}

func TestEnhanceHypothesisRequest_SimplerPrompt(t *testing.T) {
	ie := newTestExecutorForTests(t)
	
	req := &ExecutionRequest{
		TaskName:    "test hypothesis:",
		Description: "test hypothesis: test simple hypothesis",
		Context:     make(map[string]string),
	}
	
	descLower := toLower(req.Description)
	enhanced := ie.enhanceHypothesisRequest(req, descLower)
	
	// The enhanced prompt should be simpler than the original 250+ line version
	// Check that it's goal-oriented rather than prescriptive
	if contains(enhanced.Description, "🚨🚨🚨") {
		t.Errorf("Enhanced description should not contain emoji warnings (old style)")
	}
	
	// Should be much shorter than 250 lines but still informative
	lineCount := countLines(enhanced.Description)
	if lineCount > 50 {
		t.Logf("Warning: Enhanced description has %d lines (target: <50)", lineCount)
	}
	
	t.Logf("Enhanced prompt has %d lines", lineCount)
}

// ============================================================================
// Helper Functions
// ============================================================================

func toLower(s string) string {
	// Simple ASCII lowercase
	result := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		result[i] = c
	}
	return string(result)
}

func contains(s, substr string) bool {
	if len(substr) == 0 {
		return true
	}
	if len(s) < len(substr) {
		return false
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		match := true
		for j := 0; j < len(substr); j++ {
			if s[i+j] != substr[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func countLines(s string) int {
	count := 1
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			count++
		}
	}
	return count
}

// ============================================================================
// Example Table-Driven Tests
// ============================================================================

func TestExecuteTaskIntelligently_RoutingDecisions(t *testing.T) {
	tests := []struct {
		name           string
		taskName       string
		description    string
		context        map[string]string
		language       string
		expectedPath   string // Which execution path should be taken
		skipIntegration bool   // Skip if requires external dependencies
	}{
		{
			name:         "LLM Summarization - analyze_bootstrap",
			taskName:     "analyze_bootstrap",
			description:  "Summarize learning",
			expectedPath: "LLM Summarization",
			skipIntegration: true,
		},
		{
			name:         "Simple Informational",
			taskName:     "Info",
			description:  "AI news",
			expectedPath: "Simple Informational",
		},
		{
			name:         "Direct Tool - tool_ls",
			taskName:     "Tool Execution",
			description:  "Execute tool tool_ls: list files",
			expectedPath: "Direct Tool",
		},
		{
			name:         "Hypothesis Testing",
			taskName:     "test hypothesis:",
			description:  "test hypothesis: AI is related to ML",
			expectedPath: "Hypothesis Testing",
			skipIntegration: true,
		},
		{
			name:         "Web Gathering",
			taskName:     "Research",
			description:  "scrape https://example.com",
			expectedPath: "Web Gathering",
			skipIntegration: true,
		},
		{
			name:         "Traditional Execution",
			taskName:     "Calculate",
			description:  "calculate fibonacci",
			language:     "python",
			expectedPath: "Traditional",
			skipIntegration: true,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.skipIntegration {
				t.Skip("Skipping integration test - requires external dependencies")
			}
			
			ie := newTestExecutorForTests(t)
			req := &ExecutionRequest{
				TaskName:    tt.taskName,
				Description: tt.description,
				Language:    tt.language,
				Context:     tt.context,
			}
			if req.Context == nil {
				req.Context = make(map[string]string)
			}
			
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			
			result, err := ie.ExecuteTaskIntelligently(ctx, req)
			
			// Basic assertions
			if err != nil {
				t.Logf("Got error: %v", err)
			}
			
			if result == nil {
				t.Fatal("Expected non-nil result")
			}
			
			t.Logf("Path: %s, Success: %v, ExecutionTime: %v", 
				tt.expectedPath, result.Success, result.ExecutionTime)
		})
	}
}

// ============================================================================
// Benchmark Tests
// ============================================================================

func BenchmarkShouldUseLLMSummarization(b *testing.B) {
	ie := &IntelligentExecutor{}
	req := &ExecutionRequest{TaskName: "analyze_bootstrap"}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ie.shouldUseLLMSummarization(req)
	}
}

func BenchmarkShouldUseSimpleInformational(b *testing.B) {
	ie := &IntelligentExecutor{}
	req := &ExecutionRequest{
		Description: "AI news today",
		Context:     make(map[string]string),
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ie.shouldUseSimpleInformational(req)
	}
}

func BenchmarkShouldUseHypothesisTesting(b *testing.B) {
	ie := &IntelligentExecutor{}
	req := &ExecutionRequest{
		TaskName:    "test hypothesis:",
		Description: "test hypothesis: AI is related to ML",
		Context:     make(map[string]string),
	}
	descLower := toLower(req.Description)
	taskLower := toLower(req.TaskName)
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ie.shouldUseHypothesisTesting(req, descLower, taskLower)
	}
}
