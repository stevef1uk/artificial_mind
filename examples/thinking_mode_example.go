package main

import (
	"encoding/json"
	"log"
	"time"
)

// This example demonstrates how to use the thinking mode feature
// in your AGI system to see what's going on inside the AI's head

func main() {
	log.Println("🧠 Thinking Mode Example - AGI System Introspection")
	log.Println("==================================================")

	// Example 1: Basic thinking mode usage
	exampleBasicThinkingMode()

	// Example 2: Streaming thoughts in real-time
	exampleStreamingThoughts()

	// Example 3: Thought inspection and debugging
	exampleThoughtInspection()

	log.Println("\n✅ All examples completed!")
}

// Example 1: Basic thinking mode usage
func exampleBasicThinkingMode() {
	log.Println("\n📝 Example 1: Basic Thinking Mode Usage")
	log.Println("--------------------------------------")

	// Simulate a conversation request with thinking enabled
	conversationRequest := map[string]interface{}{
		"message":       "Please learn about black holes and explain them to me",
		"session_id":    "example_session_001",
		"show_thinking": true,
		"context": map[string]string{
			"domain": "astronomy",
			"level":  "beginner",
		},
	}

	// Convert to JSON for API call
	requestJSON, _ := json.MarshalIndent(conversationRequest, "", "  ")
	log.Printf("📤 Request to /api/v1/chat:\n%s\n", string(requestJSON))

	// Expected response structure
	expectedResponse := map[string]interface{}{
		"response":   "Black holes are regions of space where gravity is so strong that nothing can escape, not even light...",
		"session_id": "example_session_001",
		"timestamp":  time.Now().Format(time.RFC3339),
		"confidence": 0.85,
		"thoughts": []map[string]interface{}{
			{
				"type":       "thinking",
				"content":    "I need to learn about black holes using Wikipedia scraper",
				"state":      "plan",
				"goal":       "Learn about black holes and explain them",
				"confidence": 0.8,
				"timestamp":  time.Now().Format(time.RFC3339),
			},
			{
				"type":       "decision",
				"content":    "I'll use the wiki_scraper tool to get comprehensive information",
				"state":      "decide",
				"goal":       "Learn about black holes and explain them",
				"confidence": 0.9,
				"action":     "scrape_wikipedia",
				"timestamp":  time.Now().Format(time.RFC3339),
			},
			{
				"type":       "action",
				"content":    "Executing Wikipedia scrape for black hole articles",
				"state":      "act",
				"goal":       "Learn about black holes and explain them",
				"confidence": 0.95,
				"tool_used":  "wiki_scraper",
				"action":     "scrape_wikipedia",
				"result":     "Found 3 comprehensive articles",
				"timestamp":  time.Now().Format(time.RFC3339),
			},
			{
				"type":       "observation",
				"content":    "Successfully extracted 12 key facts about black holes",
				"state":      "observe",
				"goal":       "Learn about black holes and explain them",
				"confidence": 0.9,
				"result":     "Knowledge base updated with black hole facts",
				"timestamp":  time.Now().Format(time.RFC3339),
			},
		},
		"thinking_summary": "I went through 4 reasoning steps, 1 decision, and 1 action in total.",
		"metadata": map[string]interface{}{
			"intent_type":         "learn_and_explain",
			"action_type":         "knowledge_acquisition",
			"fsm_state":           "observe",
			"execution_time":      "2.3s",
			"thought_count":       4,
			"thinking_confidence": 0.88,
		},
	}

	responseJSON, _ := json.MarshalIndent(expectedResponse, "", "  ")
	log.Printf("📥 Expected Response:\n%s\n", string(responseJSON))
}

// Example 2: Streaming thoughts in real-time
func exampleStreamingThoughts() {
	log.Println("\n🌊 Example 2: Streaming Thoughts in Real-Time")
	log.Println("--------------------------------------------")

	// Simulate Server-Sent Events stream
	log.Println("📡 Connecting to /api/v1/chat/sessions/example_session_001/thoughts/stream")
	log.Println("📡 Event stream events:")

	events := []map[string]interface{}{
		{
			"type":       "connected",
			"session_id": "example_session_001",
		},
		{
			"type": "thought",
			"data": map[string]interface{}{
				"type":       "thinking",
				"content":    "Starting to process user request about black holes",
				"state":      "perceive",
				"goal":       "Learn about black holes",
				"confidence": 0.7,
				"timestamp":  time.Now().Format(time.RFC3339),
			},
		},
		{
			"type": "thought",
			"data": map[string]interface{}{
				"type":       "decision",
				"content":    "I'll use Wikipedia scraper to get comprehensive information",
				"state":      "plan",
				"goal":       "Learn about black holes",
				"confidence": 0.9,
				"action":     "scrape_wikipedia",
				"timestamp":  time.Now().Format(time.RFC3339),
			},
		},
		{
			"type": "thought",
			"data": map[string]interface{}{
				"type":       "action",
				"content":    "Executing Wikipedia scrape...",
				"state":      "act",
				"goal":       "Learn about black holes",
				"confidence": 0.95,
				"tool_used":  "wiki_scraper",
				"timestamp":  time.Now().Format(time.RFC3339),
			},
		},
		{
			"type": "complete",
		},
	}

	for i, event := range events {
		eventJSON, _ := json.Marshal(event)
		log.Printf("📡 Event %d: %s\n", i+1, string(eventJSON))
		time.Sleep(100 * time.Millisecond) // Simulate real-time delay
	}
}

// Example 3: Thought inspection and debugging
func exampleThoughtInspection() {
	log.Println("\n🔍 Example 3: Thought Inspection and Debugging")
	log.Println("---------------------------------------------")

	// Example API calls for thought inspection
	apiCalls := []struct {
		method string
		url    string
		desc   string
	}{
		{
			"GET",
			"/api/v1/chat/sessions/example_session_001/thoughts?limit=50",
			"Get recent thoughts for a session",
		},
		{
			"GET",
			"/api/v1/chat/sessions/example_session_001/thoughts/stream",
			"Stream thoughts in real-time",
		},
		{
			"POST",
			"/api/v1/chat/sessions/example_session_001/thoughts/express",
			"Convert reasoning traces to natural language",
		},
		{
			"GET",
			"/api/v1/chat/sessions/example_session_001/reasoning",
			"Get detailed reasoning trace",
		},
		{
			"GET",
			"/api/v1/chat/sessions/example_session_001/thinking",
			"Get thinking summary",
		},
	}

	for i, call := range apiCalls {
		log.Printf("🔧 API Call %d: %s %s", i+1, call.method, call.url)
		log.Printf("   Description: %s", call.desc)

		if call.method == "POST" {
			// Example POST body for thought expression
			postBody := map[string]interface{}{
				"style": "conversational",
				"context": map[string]interface{}{
					"domain": "astronomy",
					"level":  "beginner",
				},
			}
			bodyJSON, _ := json.MarshalIndent(postBody, "   ", "  ")
			log.Printf("   Body: %s", string(bodyJSON))
		}
		log.Println()
	}

	// Example thought expression request
	log.Println("📝 Example Thought Expression Request:")
	expressionRequest := map[string]interface{}{
		"session_id": "example_session_001",
		"style":      "conversational",
		"context": map[string]interface{}{
			"domain": "astronomy",
			"level":  "beginner",
		},
	}

	exprJSON, _ := json.MarshalIndent(expressionRequest, "", "  ")
	log.Printf("%s\n", string(exprJSON))

	// Example thought expression response
	log.Println("📥 Example Thought Expression Response:")
	expressionResponse := map[string]interface{}{
		"success": true,
		"response": map[string]interface{}{
			"thoughts": []map[string]interface{}{
				{
					"type":       "thinking",
					"content":    "I'm analyzing the user's request about black holes",
					"state":      "perceive",
					"goal":       "Learn about black holes",
					"confidence": 0.8,
					"timestamp":  time.Now().Format(time.RFC3339),
				},
				{
					"type":       "decision",
					"content":    "I decided to use Wikipedia scraper because it has comprehensive information",
					"state":      "plan",
					"goal":       "Learn about black holes",
					"confidence": 0.9,
					"action":     "scrape_wikipedia",
					"timestamp":  time.Now().Format(time.RFC3339),
				},
			},
			"summary":      "I went through 2 reasoning steps, 1 decision in total.",
			"confidence":   0.85,
			"generated_at": time.Now().Format(time.RFC3339),
			"session_id":   "example_session_001",
			"metadata": map[string]interface{}{
				"thought_count": 2,
				"style":         "conversational",
			},
		},
	}

	respJSON, _ := json.MarshalIndent(expressionResponse, "", "  ")
	log.Printf("%s\n", string(respJSON))
}

// Example of how to integrate with your existing system
func exampleIntegration() {
	log.Println("\n🔗 Integration Example")
	log.Println("---------------------")

	// 1. Initialize the thought expression service
	log.Println("1. Initialize ThoughtExpressionService:")
	log.Println("   thoughtService := NewThoughtExpressionService(redis, llmClient)")

	// 2. Initialize the thought stream service
	log.Println("2. Initialize ThoughtStreamService:")
	log.Println("   streamService := NewThoughtStreamService(natsConn, redis)")

	// 3. Start listening for thought events
	log.Println("3. Start listening for thought events:")
	log.Println("   streamService.StartListening(ctx, &ThoughtStreamConfig{})")

	// 4. Register handlers
	log.Println("4. Register thought event handlers:")
	log.Println("   streamService.RegisterHandler(\"monitor\", monitorHandler)")
	log.Println("   streamService.RegisterHandler(\"logger\", loggerHandler)")

	// 5. Use in conversational layer
	log.Println("5. Use in ConversationalLayer:")
	log.Println("   // Already integrated in ProcessMessage method")
	log.Println("   // Just set ShowThinking: true in ConversationRequest")

	// 6. Monitor thoughts via API
	log.Println("6. Monitor thoughts via API:")
	log.Println("   GET /api/v1/chat/sessions/{sessionId}/thoughts")
	log.Println("   GET /api/v1/chat/sessions/{sessionId}/thoughts/stream")
	log.Println("   POST /api/v1/chat/sessions/{sessionId}/thoughts/express")

	log.Println("\n✅ Integration complete!")
}

// Example usage in different scenarios
func exampleUsageScenarios() {
	log.Println("\n🎯 Usage Scenarios")
	log.Println("-----------------")

	scenarios := []struct {
		scenario    string
		description string
		apiCall     string
	}{
		{
			"Debugging AI Decisions",
			"When the AI makes unexpected decisions, inspect its reasoning",
			"GET /api/v1/chat/sessions/{id}/thoughts?limit=20",
		},
		{
			"Real-time Monitoring",
			"Watch the AI think in real-time during complex tasks",
			"GET /api/v1/chat/sessions/{id}/thoughts/stream",
		},
		{
			"Educational Tool",
			"Show students how AI reasoning works",
			"POST /api/v1/chat with show_thinking: true",
		},
		{
			"Performance Analysis",
			"Analyze which reasoning steps take the most time",
			"GET /api/v1/chat/sessions/{id}/reasoning",
		},
		{
			"Transparency Report",
			"Generate human-readable explanations of AI decisions",
			"POST /api/v1/chat/sessions/{id}/thoughts/express",
		},
	}

	for i, scenario := range scenarios {
		log.Printf("%d. %s", i+1, scenario.scenario)
		log.Printf("   %s", scenario.description)
		log.Printf("   API: %s\n", scenario.apiCall)
	}
}
