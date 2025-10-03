package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

type QdrantPoint struct {
	ID      interface{}    `json:"id"`
	Payload map[string]any `json:"payload"`
}

type QdrantScrollResponse struct {
	Result struct {
		Points      []QdrantPoint `json:"points"`
		NextOffset  interface{}   `json:"next_page_offset"`
		TotalPoints int           `json:"total"`
	} `json:"result"`
}

func main() {
	// Get Qdrant URL from environment or use default
	qdrantURL := os.Getenv("QDRANT_URL")
	if qdrantURL == "" {
		qdrantURL = "http://localhost:6333"
	}

	collection := "agi-episodes"

	fmt.Printf("📰 BBC News Data in Qdrant Collection '%s'\n", collection)
	fmt.Println("============================================================")

	client := &http.Client{Timeout: 30 * time.Second}
	processed := 0
	newsCount := 0
	var nextOffset interface{} = nil

	for {
		// Scroll through all points
		body := map[string]any{
			"limit":        100, // Batch size
			"with_payload": true,
			"with_vector":  false,
		}
		if nextOffset != nil {
			body["offset"] = nextOffset
		}

		jsonBody, _ := json.Marshal(body)
		req, _ := http.NewRequest("POST",
			fmt.Sprintf("%s/collections/%s/points/scroll", qdrantURL, collection),
			bytes.NewReader(jsonBody))
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			fmt.Printf("❌ Error making request: %v\n", err)
			return
		}

		var scrollResp QdrantScrollResponse
		if err := json.NewDecoder(resp.Body).Decode(&scrollResp); err != nil {
			fmt.Printf("❌ Error decoding response: %v\n", err)
			resp.Body.Close()
			return
		}
		resp.Body.Close()

		if len(scrollResp.Result.Points) == 0 {
			break
		}

		// Process each point
		for _, point := range scrollResp.Result.Points {
			processed++

			// Check if this is a news event
			if isNewsEvent(point) {
				newsCount++
				fmt.Printf("\n📄 News Entry #%d (ID: %v)\n", newsCount, point.ID)
				fmt.Println("----------------------------------------")

				// Extract and display news content
				displayNewsContent(point)
			}
		}

		// Check if there are more points
		nextOffset = scrollResp.Result.NextOffset
		if nextOffset == nil {
			break
		}
	}

	fmt.Printf("\n✅ Processed %d total points\n", processed)
	fmt.Printf("📰 Found %d BBC news entries\n", newsCount)

	if newsCount == 0 {
		fmt.Println("ℹ️  No BBC news entries found. This could mean:")
		fmt.Println("   - News ingestion hasn't been run recently")
		fmt.Println("   - News events are stored with different metadata")
		fmt.Println("   - The news events are in a different collection")
	}
}

func isNewsEvent(point QdrantPoint) bool {
	// Check if this is a news event by looking at the source
	if metadata, exists := point.Payload["metadata"]; exists {
		if metadataMap, ok := metadata.(map[string]interface{}); ok {
			if source, ok := metadataMap["source"].(string); ok {
				return strings.Contains(source, "news") || strings.Contains(source, "bbc")
			}
		}
	}

	// Also check the text content for news-like content
	if text, exists := point.Payload["text"]; exists {
		if textStr, ok := text.(string); ok {
			// Look for news-like content (longer text, not just "news:fsm:timer_tick")
			if len(textStr) > 50 && !strings.Contains(textStr, "timer_tick") {
				return true
			}
		}
	}

	return false
}

func displayNewsContent(point QdrantPoint) {
	// Display text content
	if text, exists := point.Payload["text"]; exists {
		if textStr, ok := text.(string); ok {
			fmt.Printf("📝 Text: %s\n", textStr)
		}
	}

	// Display metadata
	if metadata, exists := point.Payload["metadata"]; exists {
		if metadataMap, ok := metadata.(map[string]interface{}); ok {
			fmt.Println("📋 Metadata:")

			// Show headline if available
			if headline, ok := metadataMap["headline"].(string); ok && headline != "" {
				fmt.Printf("   📰 Headline: %s\n", headline)
			}

			// Show source
			if source, ok := metadataMap["source"].(string); ok {
				fmt.Printf("   🔗 Source: %s\n", source)
			}

			// Show type (alerts, relations, etc.)
			if eventType, ok := metadataMap["type"].(string); ok {
				fmt.Printf("   📊 Type: %s\n", eventType)
			}

			// Show confidence if available
			if confidence, ok := metadataMap["confidence"].(float64); ok {
				fmt.Printf("   🎯 Confidence: %.1f\n", confidence)
			}

			// Show impact if available (for alerts)
			if impact, ok := metadataMap["impact"].(string); ok {
				fmt.Printf("   ⚡ Impact: %s\n", impact)
			}

			// Show relation details if available
			if head, ok := metadataMap["head"].(string); ok && head != "" {
				fmt.Printf("   👤 Head: %s\n", head)
			}
			if relation, ok := metadataMap["relation"].(string); ok && relation != "" {
				fmt.Printf("   🔗 Relation: %s\n", relation)
			}
			if tail, ok := metadataMap["tail"].(string); ok && tail != "" {
				fmt.Printf("   🎯 Tail: %s\n", tail)
			}

			// Show URL if available
			if url, ok := metadataMap["url"].(string); ok && url != "" {
				fmt.Printf("   🌐 URL: %s\n", url)
			}

			// Show timestamp
			if timestamp, ok := metadataMap["timestamp"].(string); ok {
				fmt.Printf("   ⏰ Timestamp: %s\n", timestamp)
			}
		}
	}

	// Display other relevant fields
	if sessionID, exists := point.Payload["session_id"]; exists {
		if sessionStr, ok := sessionID.(string); ok && sessionStr != "" {
			fmt.Printf("🆔 Session: %s\n", sessionStr)
		}
	}

	if tags, exists := point.Payload["tags"]; exists {
		fmt.Printf("🏷️  Tags: %v\n", tags)
	}

	if outcome, exists := point.Payload["outcome"]; exists {
		fmt.Printf("✅ Outcome: %v\n", outcome)
	}
}
