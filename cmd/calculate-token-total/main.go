package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/redis/go-redis/v9"
)

func main() {
	// Get Redis address from environment
	redisAddr := os.Getenv("REDIS_URL")
	if redisAddr == "" {
		redisAddr = "localhost:6379"
	}

	// Normalize Redis address (remove redis:// prefix if present)
	redisAddr = strings.TrimPrefix(redisAddr, "redis://")
	redisAddr = strings.TrimPrefix(redisAddr, "rediss://")

	// Create Redis client
	redisClient := redis.NewClient(&redis.Options{
		Addr: redisAddr,
	})
	defer redisClient.Close()

	ctx := context.Background()

	// Test connection
	if err := redisClient.Ping(ctx).Err(); err != nil {
		log.Fatalf("Failed to connect to Redis at %s: %v", redisAddr, err)
	}

	fmt.Printf("✅ Connected to Redis at %s\n\n", redisAddr)

	// Find all token usage keys
	// Pattern 1: token_usage:YYYY-MM-DD:total (overall daily totals)
	// Pattern 2: token_usage:YYYY-MM-DD:component:COMPONENT:total (per-component daily totals)
	// Pattern 3: token_usage:aggregated:YYYY-MM-DD:component:COMPONENT:total (aggregated per-component totals)

	var totalTokens int64 = 0
	var promptTokens int64 = 0
	var completionTokens int64 = 0
	var keyCount int = 0

	// Get all keys matching token_usage patterns
	patterns := []string{
		"token_usage:*:total",
		"token_usage:*:prompt",
		"token_usage:*:completion",
		"token_usage:aggregated:*:total",
		"token_usage:aggregated:*:prompt",
		"token_usage:aggregated:*:completion",
	}

	allKeys := make(map[string]bool)

	for _, pattern := range patterns {
		keys, err := redisClient.Keys(ctx, pattern).Result()
		if err != nil {
			log.Printf("⚠️  Error getting keys for pattern %s: %v", pattern, err)
			continue
		}

		for _, key := range keys {
			allKeys[key] = true
		}
	}

	fmt.Printf("Found %d unique token usage keys\n\n", len(allKeys))

	// Process each key and sum values
	for key := range allKeys {
		val, err := redisClient.Get(ctx, key).Int64()
		if err != nil {
			// Key might not exist or might not be an integer
			continue
		}

		keyCount++
		
		// Determine what type of token this is based on the key
		if strings.Contains(key, ":total") {
			totalTokens += val
			fmt.Printf("  %s: %d (total)\n", key, val)
		} else if strings.Contains(key, ":prompt") {
			promptTokens += val
			fmt.Printf("  %s: %d (prompt)\n", key, val)
		} else if strings.Contains(key, ":completion") {
			completionTokens += val
			fmt.Printf("  %s: %d (completion)\n", key, val)
		}
	}

	fmt.Printf("\n" + strings.Repeat("=", 60) + "\n")
	fmt.Printf("SUMMARY\n")
	fmt.Printf(strings.Repeat("=", 60) + "\n")
	fmt.Printf("Keys processed: %d\n", keyCount)
	fmt.Printf("Total Prompt Tokens: %d\n", promptTokens)
	fmt.Printf("Total Completion Tokens: %d\n", completionTokens)
	fmt.Printf("Total Tokens: %d\n", totalTokens)
	fmt.Printf("\n")

	// Also calculate from prompt + completion if total is different
	calculatedTotal := promptTokens + completionTokens
	if totalTokens != calculatedTotal && totalTokens > 0 {
		fmt.Printf("Note: Calculated total (prompt + completion) = %d\n", calculatedTotal)
		fmt.Printf("      This may differ from stored totals if some keys only track totals.\n")
	}

	// Use the larger of the two totals as the most accurate
	finalTotal := totalTokens
	if calculatedTotal > totalTokens {
		finalTotal = calculatedTotal
		fmt.Printf("\nUsing calculated total (prompt + completion) as it's higher.\n")
	}

	fmt.Printf("\n" + strings.Repeat("=", 60) + "\n")
	fmt.Printf("FINAL TOTAL LLM TOKENS: %d\n", finalTotal)
	fmt.Printf(strings.Repeat("=", 60) + "\n")
}

