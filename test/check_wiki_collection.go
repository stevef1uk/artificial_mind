package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
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

	collection := "agi-wiki"

	fmt.Printf("🔍 Checking Qdrant collection '%s' for Wikipedia content\n", collection)
	fmt.Println("=========================================================")

	client := &http.Client{Timeout: 30 * time.Second}
	processed := 0
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
			fmt.Printf("\n📄 Point #%d (ID: %v)\n", processed, point.ID)
			fmt.Println("----------------------------------------")

			// Show all payload fields
			for key, value := range point.Payload {
				if key == "vector" { // Skip vector data
					continue
				}

				if valueStr, ok := value.(string); ok {
					// Truncate very long strings for readability
					if len(valueStr) > 200 {
						valueStr = valueStr[:200] + "..."
					}
					fmt.Printf("📝 %s: %s\n", key, valueStr)
				} else {
					fmt.Printf("📝 %s: %v\n", key, value)
				}
			}
		}

		// Check if there are more points
		nextOffset = scrollResp.Result.NextOffset
		if nextOffset == nil {
			break
		}
	}

	fmt.Printf("\n✅ Processed %d points total from agi-wiki collection\n", processed)
}
