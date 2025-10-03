package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

type QdrantCollection struct {
	Name string `json:"name"`
}

type QdrantCollectionsResponse struct {
	Result struct {
		Collections []QdrantCollection `json:"collections"`
	} `json:"result"`
}

func main() {
	// Get Qdrant URL from environment or use default
	qdrantURL := os.Getenv("QDRANT_URL")
	if qdrantURL == "" {
		qdrantURL = "http://localhost:6333"
	}

	fmt.Printf("🔍 Checking Qdrant collections at %s\n", qdrantURL)
	fmt.Println("==================================================")

	client := &http.Client{Timeout: 10 * time.Second}

	// Get list of collections
	req, err := http.NewRequest("GET", qdrantURL+"/collections", nil)
	if err != nil {
		fmt.Printf("❌ Error creating request: %v\n", err)
		return
	}

	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("❌ Error making request: %v\n", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		fmt.Printf("❌ Error response: %s\n", resp.Status)
		return
	}

	var collectionsResp QdrantCollectionsResponse
	if err := json.NewDecoder(resp.Body).Decode(&collectionsResp); err != nil {
		fmt.Printf("❌ Error decoding response: %v\n", err)
		return
	}

	fmt.Printf("📊 Found %d collections:\n", len(collectionsResp.Result.Collections))
	for i, collection := range collectionsResp.Result.Collections {
		fmt.Printf("  %d. %s\n", i+1, collection.Name)
	}

	// Check each collection for news-related content
	for _, collection := range collectionsResp.Result.Collections {
		fmt.Printf("\n🔍 Checking collection '%s' for news content...\n", collection.Name)
		checkCollectionForNews(client, qdrantURL, collection.Name)
	}
}

func checkCollectionForNews(client *http.Client, qdrantURL, collectionName string) {
	// Get collection info first
	req, err := http.NewRequest("GET", qdrantURL+"/collections/"+collectionName, nil)
	if err != nil {
		fmt.Printf("  ❌ Error creating request: %v\n", err)
		return
	}

	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("  ❌ Error making request: %v\n", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		fmt.Printf("  ❌ Error response: %s\n", resp.Status)
		return
	}

	// Read response to get collection info
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("  ❌ Error reading response: %v\n", err)
		return
	}

	var collectionInfo map[string]interface{}
	if err := json.Unmarshal(body, &collectionInfo); err != nil {
		fmt.Printf("  ❌ Error decoding collection info: %v\n", err)
		return
	}

	// Extract point count
	if result, ok := collectionInfo["result"].(map[string]interface{}); ok {
		if pointsCount, ok := result["points_count"].(float64); ok {
			fmt.Printf("  📊 Points in collection: %.0f\n", pointsCount)
		}
	}

	// Sample a few points to check for news content
	samplePoints(client, qdrantURL, collectionName)
}

func samplePoints(client *http.Client, qdrantURL, collectionName string) {
	// Scroll through a few points to check for news content
	body := map[string]any{
		"limit":        5, // Just sample a few points
		"with_payload": true,
		"with_vector":  false,
	}

	jsonBody, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST",
		qdrantURL+"/collections/"+collectionName+"/points/scroll",
		bytes.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("  ❌ Error sampling points: %v\n", err)
		return
	}
	defer resp.Body.Close()

	var scrollResp struct {
		Result struct {
			Points []struct {
				Payload map[string]any `json:"payload"`
			} `json:"points"`
		} `json:"result"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&scrollResp); err != nil {
		fmt.Printf("  ❌ Error decoding sample response: %v\n", err)
		return
	}

	newsFound := false
	for i, point := range scrollResp.Result.Points {
		if text, exists := point.Payload["text"]; exists {
			if textStr, ok := text.(string); ok {
				textLower := strings.ToLower(textStr)
				if strings.Contains(textLower, "news") ||
					strings.Contains(textLower, "wikipedia") ||
					strings.Contains(textLower, "bbc") ||
					strings.Contains(textLower, "article") {
					if !newsFound {
						fmt.Printf("  🎯 Found potential news content in sample:\n")
						newsFound = true
					}
					fmt.Printf("    Point %d: %s\n", i+1, textStr[:min(100, len(textStr))])
				}
			}
		}
	}

	if !newsFound {
		fmt.Printf("  ℹ️  No news content found in sample\n")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
