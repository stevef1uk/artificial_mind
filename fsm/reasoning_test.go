package main

import (
	"context"
	"log"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// TestReasoningEngine demonstrates the reasoning capabilities
func TestReasoningEngine(t *testing.T) {
	log.Printf("🧠 Testing Reasoning Engine")

	// Create Redis client
	rdb := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
		DB:   0,
	})
	defer rdb.Close()

	// Test connection
	ctx := context.Background()
	_, err := rdb.Ping(ctx).Result()
	if err != nil {
		t.Skipf("Redis not available for this integration-style test: %v", err)
	}

	// Create reasoning engine
	reasoning := NewReasoningEngine("http://localhost:8080", rdb)

	// Test 1: Query beliefs
	log.Printf("\n📊 Test 1: Querying beliefs")
	beliefs, err := reasoning.QueryBeliefs("what is TCP/IP", "Networking")
	if err != nil {
		log.Printf("❌ Belief query failed: %v", err)
	} else {
		log.Printf("✅ Found %d beliefs", len(beliefs))
		for i, belief := range beliefs {
			log.Printf("  %d. %s (confidence: %.2f)", i+1, belief.Statement, belief.Confidence)
		}
	}

	// Test 2: Apply inference rules
	log.Printf("\n🔍 Test 2: Applying inference rules")
	newBeliefs, err := reasoning.InferNewBeliefs("Networking")
	if err != nil {
		log.Printf("❌ Inference failed: %v", err)
	} else {
		log.Printf("✅ Inferred %d new beliefs", len(newBeliefs))
		for i, belief := range newBeliefs {
			log.Printf("  %d. %s (confidence: %.2f)", i+1, belief.Statement, belief.Confidence)
		}
	}

	// Test 3: Generate curiosity goals
	log.Printf("\n🎯 Test 3: Generating curiosity goals")
	goals, err := reasoning.GenerateCuriosityGoals("Networking")
	if err != nil {
		log.Printf("❌ Curiosity goals generation failed: %v", err)
	} else {
		log.Printf("✅ Generated %d curiosity goals", len(goals))
		for i, goal := range goals {
			log.Printf("  %d. %s (priority: %d, type: %s)", i+1, goal.Description, goal.Priority, goal.Type)
		}
	}

	// Test 4: Log reasoning trace
	log.Printf("\n📝 Test 4: Logging reasoning trace")
	trace := ReasoningTrace{
		ID:   "test_trace_1",
		Goal: "Understand TCP/IP networking",
		Steps: []ReasoningStep{
			{
				StepNumber: 1,
				Action:     "query",
				Query:      "MATCH (c:Concept {name: 'TCP/IP'}) RETURN c",
				Result:     map[string]interface{}{"name": "TCP/IP", "domain": "Networking"},
				Reasoning:  "Looking up TCP/IP concept in knowledge base",
				Confidence: 0.9,
				Timestamp:  time.Now(),
			},
			{
				StepNumber: 2,
				Action:     "infer",
				Query:      "MATCH (a)-[:IS_A]->(b)-[:IS_A]->(c) WHERE a.name = 'TCP/IP' RETURN a, b, c",
				Result:     map[string]interface{}{"a": "TCP/IP", "b": "Protocol", "c": "Communication"},
				Reasoning:  "Applying transitivity rule: TCP/IP is a Protocol, Protocol is a Communication, therefore TCP/IP is a Communication",
				Confidence: 0.8,
				Timestamp:  time.Now(),
			},
		},
		Evidence:   []string{"TCP/IP concept", "Protocol concept", "Communication concept"},
		Conclusion: "TCP/IP enables communication through protocol hierarchy",
		Confidence: 0.85,
		Domain:     "Networking",
		CreatedAt:  time.Now(),
		Properties: map[string]interface{}{
			"test": true,
		},
	}

	err = reasoning.LogReasoningTrace(trace)
	if err != nil {
		log.Printf("❌ Trace logging failed: %v", err)
	} else {
		log.Printf("✅ Reasoning trace logged successfully")
	}

	// Test 5: Generate explanation
	log.Printf("\n💭 Test 5: Generating explanation")
	explanation, err := reasoning.ExplainReasoning("Understand TCP/IP networking", "Networking")
	if err != nil {
		log.Printf("❌ Explanation generation failed: %v", err)
	} else {
		log.Printf("✅ Generated explanation:\n%s", explanation)
	}

	log.Printf("\n🎉 Reasoning Engine test completed!")
}

// TestFSMWithReasoning demonstrates FSM with reasoning capabilities
func TestFSMWithReasoning(t *testing.T) {
	log.Printf("🤖 Testing FSM with Reasoning Layer")

	// This would be a more comprehensive test that:
	// 1. Creates an FSM engine with reasoning
	// 2. Sends input events
	// 3. Observes the reasoning states in action
	// 4. Verifies that beliefs are queried and inferred
	// 5. Checks that curiosity goals are generated
	// 6. Validates that reasoning traces are logged

	log.Printf("📋 FSM with Reasoning test would include:")
	log.Printf("  - FSM engine initialization with reasoning engine")
	log.Printf("  - Input event processing through reasoning states")
	log.Printf("  - Belief querying and inference verification")
	log.Printf("  - Curiosity goal generation validation")
	log.Printf("  - Reasoning trace logging verification")
	log.Printf("  - Explanation generation testing")
}
