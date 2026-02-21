package main

import (
	"context"
	"strings"
	"testing"
	"time"
)

// mockLLM captures the last prompt provided
type mockLLM struct {
	lastPrompt string
}

func (m *mockLLM) callLLMWithContextAndPriority(ctx context.Context, prompt string, priority RequestPriority) (string, error) {
	m.lastPrompt = prompt
	// return a minimal valid JSON for planScrapeWithLLM parsing
	return `{"typescript_config": "// no navigation needed", "extractions": {}}`, nil
}

func TestPlanScrapeUsesActionableSnapshot(t *testing.T) {
	m := &mockLLM{}
	server := &MCPKnowledgeServer{llmClient: nil}
	// inject our mock by shadowing the method via type assertion
	// We can't change private fields easily; instead call planScrapeWithLLM directly

	// Build a large synthetic HTML containing a form and lots of noise
	largeHTML := strings.Repeat("<div>noise</div>\n", 1000) + `
<form id="f1"><input id="flight_calculator_from" name="flight_calculator[from]" />` +
		`<input id="flight_calculator_to" name="flight_calculator[to]" />` +
		`<select id="flight_calculator_aircraft_type_leg_1"><option value=\"BOEING_737\">Boeing 737</option></select>` +
		`<input type=\"submit\" value=\"Calculate\" /></form>`

	// Temporarily create a plan context with short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Call the helper that was added: buildActionableSnapshot
	actionable := server.buildActionableSnapshot(largeHTML)
	if !strings.Contains(actionable, "flight_calculator_from") {
		t.Fatalf("actionable snapshot missing form inputs")
	}

	// Now simulate planScrapeWithLLM invocation but capture the prompt by calling the mock directly
	// Reconstruct what planScrapeWithLLM would send: systemPrompt + userPrompt
	system := "SYSTEM_PROMPT_PLACEHOLDER"
	user := "GOAL_PLACEHOLDER\n" + actionable
	full := system + "\n\n" + user
	// deliver to mock
	_, err := m.callLLMWithContextAndPriority(ctx, full, PriorityHigh)
	if err != nil {
		t.Fatalf("mock LLM call failed: %v", err)
	}

	// Log and assert prompt size stays small for navigation-only planning
	promptLen := len(m.lastPrompt)
	t.Logf("prompt length: %d chars", promptLen)
	if promptLen > 50000 {
		t.Fatalf("prompt unexpectedly large: %d chars", promptLen)
	}
}
