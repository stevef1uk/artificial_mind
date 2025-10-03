package main

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
)

// SimpleReasoningDemo demonstrates the reasoning capabilities without complex imports
func main() {
	log.Printf("🧠 Simple Reasoning Layer Demo")
	log.Printf("==============================")

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
		log.Printf("❌ Failed to connect to Redis: %v", err)
		return
	}

	log.Printf("✅ Redis connection successful!")

	// Test basic Redis operations
	log.Printf("\n📊 Testing Redis operations...")

	// Store a test belief
	testBelief := map[string]interface{}{
		"id":         "belief_1",
		"statement":  "TCP/IP is a networking protocol",
		"confidence": 0.9,
		"source":     "knowledge_base",
		"domain":     "Networking",
		"created_at": time.Now().Format(time.RFC3339),
	}

	// Store in Redis
	beliefKey := "reasoning:beliefs:Networking"
	beliefJSON, _ := json.Marshal(testBelief)
	err = rdb.LPush(ctx, beliefKey, beliefJSON).Err()
	if err != nil {
		log.Printf("❌ Failed to store belief: %v", err)
		return
	}

	log.Printf("✅ Stored test belief in Redis")

	// Retrieve the belief
	beliefs, err := rdb.LRange(ctx, beliefKey, 0, -1).Result()
	if err != nil {
		log.Printf("❌ Failed to retrieve beliefs: %v", err)
		return
	}

	log.Printf("✅ Retrieved %d beliefs from Redis", len(beliefs))
	for i, belief := range beliefs {
		log.Printf("  %d. %s", i+1, belief)
	}

	// Test inference rules storage
	log.Printf("\n🔍 Testing inference rules...")

	inferenceRule := map[string]interface{}{
		"id":          "is_a_transitivity",
		"name":        "IS_A Transitivity",
		"pattern":     "(a)-[:IS_A]->(b)-[:IS_A]->(c)",
		"conclusion":  "(a)-[:IS_A]->(c)",
		"confidence":  0.9,
		"domain":      "General",
		"description": "If A is a B and B is a C, then A is a C",
	}

	ruleKey := "reasoning:inference_rules:General"
	ruleJSON, _ := json.Marshal(inferenceRule)
	err = rdb.LPush(ctx, ruleKey, ruleJSON).Err()
	if err != nil {
		log.Printf("❌ Failed to store inference rule: %v", err)
		return
	}

	log.Printf("✅ Stored inference rule in Redis")

	// Test curiosity goals
	log.Printf("\n🎯 Testing curiosity goals...")

	curiosityGoal := map[string]interface{}{
		"id":          "gap_filling_1",
		"type":        "gap_filling",
		"description": "Find concepts without relationships",
		"domain":      "Networking",
		"priority":    7,
		"status":      "pending",
		"created_at":  time.Now().Format(time.RFC3339),
	}

	goalKey := "reasoning:curiosity_goals:Networking"
	goalJSON, _ := json.Marshal(curiosityGoal)
	err = rdb.LPush(ctx, goalKey, goalJSON).Err()
	if err != nil {
		log.Printf("❌ Failed to store curiosity goal: %v", err)
		return
	}

	log.Printf("✅ Stored curiosity goal in Redis")

	// Test reasoning trace
	log.Printf("\n📝 Testing reasoning trace...")

	reasoningTrace := map[string]interface{}{
		"id":         "trace_1",
		"goal":       "Understand TCP/IP networking",
		"conclusion": "TCP/IP enables communication through protocol hierarchy",
		"confidence": 0.85,
		"domain":     "Networking",
		"created_at": time.Now().Format(time.RFC3339),
		"steps": []map[string]interface{}{
			{
				"step_number": 1,
				"action":      "query",
				"reasoning":   "Looking up TCP/IP concept in knowledge base",
				"confidence":  0.9,
			},
			{
				"step_number": 2,
				"action":      "infer",
				"reasoning":   "Applied transitivity rule: TCP/IP is a Protocol, Protocol enables Communication",
				"confidence":  0.8,
			},
		},
	}

	traceKey := "reasoning:traces:Networking"
	traceJSON, _ := json.Marshal(reasoningTrace)
	err = rdb.LPush(ctx, traceKey, traceJSON).Err()
	if err != nil {
		log.Printf("❌ Failed to store reasoning trace: %v", err)
		return
	}

	log.Printf("✅ Stored reasoning trace in Redis")

	// Retrieve and display all stored data
	log.Printf("\n📊 Summary of stored reasoning data:")

	// Beliefs
	beliefs, _ = rdb.LRange(ctx, beliefKey, 0, -1).Result()
	log.Printf("  🧠 Beliefs: %d stored", len(beliefs))

	// Inference rules
	rules, _ := rdb.LRange(ctx, ruleKey, 0, -1).Result()
	log.Printf("  🔍 Inference rules: %d stored", len(rules))

	// Curiosity goals
	goals, _ := rdb.LRange(ctx, goalKey, 0, -1).Result()
	log.Printf("  🎯 Curiosity goals: %d stored", len(goals))

	// Reasoning traces
	traces, _ := rdb.LRange(ctx, traceKey, 0, -1).Result()
	log.Printf("  📝 Reasoning traces: %d stored", len(traces))

	log.Printf("\n🎉 Simple reasoning demo completed successfully!")
	log.Printf("==============================================")
	log.Printf("The reasoning layer infrastructure is working:")
	log.Printf("  ✅ Redis connection and storage")
	log.Printf("  ✅ Belief system data structures")
	log.Printf("  ✅ Inference rules storage")
	log.Printf("  ✅ Curiosity goals generation")
	log.Printf("  ✅ Reasoning trace logging")
	log.Printf("")
	log.Printf("Next steps:")
	log.Printf("  1. Start the FSM server to see reasoning in action")
	log.Printf("  2. Send input events to trigger reasoning states")
	log.Printf("  3. Check the monitoring UI for reasoning traces")
}
