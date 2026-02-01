package main

import (
	"context"
	"fmt"
	"os"
	"testing"
)

func TestWeaviateSearch(t *testing.T) {
	// Set the environment variable for Weaviate URL (using the port-forward)
	os.Setenv("WEAVIATE_URL", "http://127.0.0.1:9999")

	fmt.Println("🚀 Testing Weaviate Search locally...")

	s := &MCPKnowledgeServer{}

	ctx := context.Background()
	queries := []string{"Ukraine", "Ukrainians", "coyote"}

	for _, q := range queries {
		fmt.Printf("\n--- Query: '%s' ---\n", q)
		result, err := s.searchWeaviateGraphQL(ctx, q, "WikipediaArticle", 5, nil)
		if err != nil {
			t.Errorf("Error searching for '%s': %v", q, err)
			continue
		}

		resMap, ok := result.(map[string]interface{})
		if !ok {
			t.Error("Invalid result format")
			continue
		}

		items, _ := resMap["results"].([]map[string]interface{})
		fmt.Printf("✅ Found %d results\n", len(items))
		for i, item := range items {
			fmt.Printf("  [%d] %s\n", i+1, item["title"])
		}

		if q == "Ukraine" && len(items) == 0 {
			t.Error("Expected results for Ukraine (stemmed match for Ukrainians)")
		}
		if q == "Ukrainians" && len(items) == 0 {
			t.Error("Expected results for Ukrainians")
		}
	}
}
