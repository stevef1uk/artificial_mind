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

	collection := os.Getenv("QDRANT_COLLECTION")
	if collection == "" {
		collection = "agi-episodes"
	}

	fmt.Printf("🔍 Filtering Qdrant collection '%s' for actual news/Wikipedia content\n", collection)
	fmt.Println("======================================================================")

	client := &http.Client{Timeout: 30 * time.Second}
	processed := 0
	filtered := 0
	var nextOffset interface{} = nil

	for {
		// Scroll through all points
		body := map[string]any{
			"limit":        100, // Batch size
			"with_payload": true,
			"with_vector":  false, // We don't need vectors, just payload
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

			// Check if this point contains actual news/Wikipedia content
			if containsNewsContent(point) {
				filtered++
				fmt.Printf("\n📄 Point #%d (ID: %v) - NEWS/WIKI CONTENT FOUND!\n", processed, point.ID)
				fmt.Println("============================================================")

				// Extract text field
				if text, exists := point.Payload["text"]; exists {
					if textStr, ok := text.(string); ok && textStr != "" {
						fmt.Printf("📝 Text: %s\n", textStr)
					} else {
						fmt.Printf("📝 Text: (empty or not string)\n")
					}
				} else {
					fmt.Printf("📝 Text: (field not found)\n")
				}

				// Show other interesting fields
				if sessionID, exists := point.Payload["session_id"]; exists {
					fmt.Printf("🆔 Session: %v\n", sessionID)
				}
				if timestamp, exists := point.Payload["timestamp"]; exists {
					fmt.Printf("⏰ Timestamp: %v\n", timestamp)
				}
				if tags, exists := point.Payload["tags"]; exists {
					fmt.Printf("🏷️  Tags: %v\n", tags)
				}
				if outcome, exists := point.Payload["outcome"]; exists {
					fmt.Printf("✅ Outcome: %v\n", outcome)
				}

				// Show metadata for more context
				if metadata, exists := point.Payload["metadata"]; exists {
					fmt.Printf("📋 Metadata: %v\n", metadata)
				}

				// Show all payload keys for debugging
				fmt.Printf("🔑 All payload keys: ")
				keys := make([]string, 0, len(point.Payload))
				for k := range point.Payload {
					keys = append(keys, k)
				}
				fmt.Printf("%v\n", keys)
			}
		}

		// Check if there are more points
		nextOffset = scrollResp.Result.NextOffset
		if nextOffset == nil {
			break
		}
	}

	fmt.Printf("\n✅ Processed %d points total\n", processed)
	fmt.Printf("🎯 Found %d news/Wikipedia content entries\n", filtered)

	if filtered == 0 {
		fmt.Println("ℹ️  No news/Wikipedia content found. This could mean:")
		fmt.Println("   - No news ingestion has occurred yet")
		fmt.Println("   - News content is stored in a different collection")
		fmt.Println("   - News content uses different keywords than expected")
		fmt.Println("   - The news ingestion system hasn't been activated")
	}
}

func containsNewsContent(point QdrantPoint) bool {
	// Check text field for actual news content (not just event IDs)
	if text, exists := point.Payload["text"]; exists {
		if textStr, ok := text.(string); ok {
			textLower := strings.ToLower(textStr)

			// Look for actual news content patterns
			newsPatterns := []string{
				"breaking news",
				"reported by",
				"according to",
				"wikipedia article",
				"wikipedia page",
				"wikipedia entry",
				"bbc news",
				"bbc article",
				"news report",
				"news article",
				"news story",
				"headlines",
				"journalist",
				"reporter",
				"news agency",
				"wikipedia says",
				"according to wikipedia",
				"wikipedia states",
				"news source",
				"breaking",
				"latest news",
				"news update",
			}

			for _, pattern := range newsPatterns {
				if strings.Contains(textLower, pattern) {
					return true
				}
			}

			// Look for longer text that might be news content (not just "Generate python code")
			if len(textStr) > 100 &&
				(strings.Contains(textLower, "news") ||
					strings.Contains(textLower, "wikipedia") ||
					strings.Contains(textLower, "article") ||
					strings.Contains(textLower, "report")) {
				return true
			}
		}
	}

	// Check metadata for news-related sources
	if metadata, exists := point.Payload["metadata"]; exists {
		if metadataMap, ok := metadata.(map[string]interface{}); ok {
			for key, value := range metadataMap {
				keyLower := strings.ToLower(key)
				if strings.Contains(keyLower, "news") ||
					strings.Contains(keyLower, "wikipedia") ||
					strings.Contains(keyLower, "bbc") ||
					strings.Contains(keyLower, "source") {
					if valueStr, ok := value.(string); ok {
						valueLower := strings.ToLower(valueStr)
						if strings.Contains(valueLower, "news") ||
							strings.Contains(valueLower, "wikipedia") ||
							strings.Contains(valueLower, "bbc") {
							return true
						}
					}
				}
			}
		}
	}

	// Check tags for news-related tags
	if tags, exists := point.Payload["tags"]; exists {
		if tagsSlice, ok := tags.([]interface{}); ok {
			for _, tag := range tagsSlice {
				if tagStr, ok := tag.(string); ok {
					tagLower := strings.ToLower(tagStr)
					if strings.Contains(tagLower, "news") ||
						strings.Contains(tagLower, "wikipedia") ||
						strings.Contains(tagLower, "article") ||
						strings.Contains(tagLower, "ingestion") {
						return true
					}
				}
			}
		}
	}

	return false
}
