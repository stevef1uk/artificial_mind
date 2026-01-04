package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"async_llm"
)

func main() {
	fmt.Println("Testing Async LLM Client Directly")
	fmt.Println("==================================")
	fmt.Println("")

	// Check if async queue is enabled
	useAsync := os.Getenv("USE_ASYNC_LLM_QUEUE") == "1" || os.Getenv("USE_ASYNC_LLM_QUEUE") == "true"
	if useAsync {
		fmt.Println("✅ USE_ASYNC_LLM_QUEUE is enabled - using async queue")
	} else {
		fmt.Println("⚠️  USE_ASYNC_LLM_QUEUE is not set - using synchronous calls")
	}
	fmt.Println("")

	// Test parameters
	provider := "ollama"
	endpoint := os.Getenv("OLLAMA_URL")
	if endpoint == "" {
		endpoint = "http://localhost:11434/api/chat"
	}
	model := os.Getenv("LLM_MODEL")
	if model == "" {
		model = "gemma3:latest"
	}

	fmt.Printf("Provider: %s\n", provider)
	fmt.Printf("Endpoint: %s\n", endpoint)
	fmt.Printf("Model: %s\n", model)
	fmt.Println("")

	// Test 1: Simple prompt-based call
	fmt.Println("Test 1: Simple prompt-based call")
	fmt.Println("---------------------------------")
	ctx := context.Background()
	prompt := "Say hello in one sentence."
	
	start := time.Now()
	response, err := async_llm.CallAsync(ctx, provider, endpoint, model, prompt, nil, async_llm.PriorityLow)
	duration := time.Since(start)
	
	if err != nil {
		fmt.Printf("❌ Test 1 failed: %v\n", err)
	} else {
		fmt.Printf("✅ Test 1 succeeded (duration: %v)\n", duration)
		fmt.Printf("Response: %s\n", response)
	}
	fmt.Println("")

	// Test 2: Messages-based call
	fmt.Println("Test 2: Messages-based call")
	fmt.Println("----------------------------")
	messages := []map[string]string{
		{"role": "user", "content": "What is 2+2? Answer in one word."},
	}
	
	start = time.Now()
	response2, err2 := async_llm.CallAsync(ctx, provider, endpoint, model, "", messages, async_llm.PriorityLow)
	duration2 := time.Since(start)
	
	if err2 != nil {
		fmt.Printf("❌ Test 2 failed: %v\n", err2)
	} else {
		fmt.Printf("✅ Test 2 succeeded (duration: %v)\n", duration2)
		fmt.Printf("Response: %s\n", response2)
	}
	fmt.Println("")

	// Test 3: Multiple concurrent requests
	fmt.Println("Test 3: Multiple concurrent requests")
	fmt.Println("-----------------------------------")
	start = time.Now()
	
	results := make(chan string, 3)
	errors := make(chan error, 3)
	
	for i := 0; i < 3; i++ {
		go func(num int) {
			prompt := fmt.Sprintf("Count to %d in one sentence.", num+1)
			resp, err := async_llm.CallAsync(ctx, provider, endpoint, model, prompt, nil, async_llm.PriorityLow)
			if err != nil {
				errors <- err
			} else {
				results <- resp
			}
		}(i)
	}
	
	successCount := 0
	errorCount := 0
	for i := 0; i < 3; i++ {
		select {
		case res := <-results:
			successCount++
			fmt.Printf("  Request %d: ✅ %s\n", i+1, res[:min(50, len(res))])
		case err := <-errors:
			errorCount++
			fmt.Printf("  Request %d: ❌ %v\n", i+1, err)
		case <-time.After(2 * time.Minute):
			fmt.Printf("  Request %d: ❌ Timeout\n", i+1)
			errorCount++
		}
	}
	
	duration3 := time.Since(start)
	fmt.Printf("✅ Test 3 completed (duration: %v, success: %d, errors: %d)\n", duration3, successCount, errorCount)
	fmt.Println("")

	// Summary
	fmt.Println("Summary")
	fmt.Println("=======")
	if useAsync {
		fmt.Println("✅ Async queue was enabled and used")
		fmt.Println("   Check logs above for [ASYNC-LLM] prefixes")
	} else {
		fmt.Println("⚠️  Async queue was not enabled")
		fmt.Println("   Set USE_ASYNC_LLM_QUEUE=1 to enable")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

