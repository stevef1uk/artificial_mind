package main

import (
	"context"
	"fmt"
	"os"

	"github.com/redis/go-redis/v9"
)

func main() {
	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		redisURL = "redis://127.0.0.1:6379"
	}

	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to parse Redis URL: %v\n", err)
		os.Exit(1)
	}

	client := redis.NewClient(opt)
	defer client.Close()

	ctx := context.Background()

	// Test connection
	if err := client.Ping(ctx).Err(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to connect to Redis: %v\n", err)
		os.Exit(1)
	}

	// Find all duplicate keys
	keys, err := client.Keys(ctx, "news:duplicates:*").Result()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get keys: %v\n", err)
		os.Exit(1)
	}

	if len(keys) == 0 {
		fmt.Println("No duplicate keys found")
		return
	}

	fmt.Printf("Found %d duplicate tracking keys\n", len(keys))

	// Delete all keys
	deleted, err := client.Del(ctx, keys...).Result()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to delete keys: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✅ Cleared %d duplicate tracking keys\n", deleted)
}






