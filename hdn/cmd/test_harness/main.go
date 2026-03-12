package main

import (
	"fmt"
	"hdn/interpreter"
	"strings"
)

func main() {
	fmt.Println("🚀 Starting Flexible Interpreter Test Harness")

	// Pre-register Weaviate hints to simulate what api.go does
	interpreter.SetPromptHints("search_weaviate", &interpreter.PromptHintsConfig{
		Keywords:      []string{"news", "latest news", "world events"},
		AlwaysInclude: []string{"news", "latest"},
	})

	// 1. Keyword Match Testing
	testScenarios := []struct {
		name         string
		input        string
		expectedTool string
	}{
		{
			name:         "Simple research request",
			input:        "research the latest developments in drone warfare",
			expectedTool: "mcp_research_agent",
		},
		{
			name:         "Deep research request",
			input:        "perform deep research into quantum computing",
			expectedTool: "mcp_research_agent",
		},
		{
			name:         "News request (should go to Weaviate)",
			input:        "get the latest news about Paris",
			expectedTool: "search_weaviate",
		},
		{
			name:         "Research vs News collision (Research should win)",
			input:        "latest news research on AI benchmarks",
			expectedTool: "mcp_research_agent",
		},
		{
			name:         "Scrape request",
			input:        "scrape https://example.com/data",
			expectedTool: "mcp_smart_scrape",
		},
		{
			name:         "Visit well-known site",
			input:        "visit hacker news",
			expectedTool: "mcp_smart_scrape",
		},
	}

	fmt.Println("\n--- Keyword Matching Tests ---")
	for _, tc := range testScenarios {
		matchedTool := interpreter.MatchesConfiguredToolKeywords(tc.input)
		status := "❌ FAIL"
		if matchedTool == tc.expectedTool {
			status = "✅ PASS"
		} else if tc.expectedTool == "mcp_research_agent" && matchedTool == "deep_research" {
			status = "✅ PASS (Grouped Research)"
		}
		fmt.Printf("[%s] %-40s -> %s (Expected: %s)\n", status, tc.name, matchedTool, tc.expectedTool)
	}

	// 2. Forced Parameter Extraction Simulation
	fmt.Println("\n--- Parameter Extraction Verification ---")

	testExtraction := func(toolID string, input string) {
		params := make(map[string]interface{})
		actualToolID := toolID

		// Simulate the logic from validateAndEnforceHints in flexible_llm_interface.go
		if actualToolID == "mcp_research_agent" || strings.TrimPrefix(actualToolID, "mcp_") == "research_agent" || actualToolID == "deep_research" || actualToolID == "mcp_deep_research" {
			params["query"] = input
			params["topic"] = input
			params["depth"] = 2
		}

		fmt.Printf("Input: %s\nTarget Tool: %s\nExtracted Params: %v\n\n", input, actualToolID, params)
	}

	testExtraction("mcp_research_agent", "drone warfare research")
	testExtraction("deep_research", "autonomous flight analysis")
}
