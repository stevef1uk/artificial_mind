package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

func main() {
	url := "http://localhost:9090/mcp"
	payload := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name": "smart_scrape",
			"arguments": map[string]interface{}{
				"url":  "https://finance.yahoo.com/quote/AAPL",
				"goal": "Find Apple share price",
				"extractions": map[string]string{
					"price": "FAKE_BROKEN_SELECTOR_THAT_WILL_FAIL",
				},
			},
		},
	}

	data, _ := json.Marshal(payload)
	fmt.Println("🚀 Testing Self-Healing with BROKEN hint for AAPL price...")

	resp, err := http.Post(url, "application/json", bytes.NewBuffer(data))
	if err != nil {
		fmt.Printf("❌ Failed to call HDN: %v\n", err)
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var response map[string]interface{}
	if err := json.Unmarshal(body, &response); err != nil {
		fmt.Printf("❌ Failed to parse response: %v\n", err)
		fmt.Printf("Raw: %s\n", string(body))
		return
	}

	// Print the result highlights
	fmt.Printf("✅ Scrape completed!\n")
	pretty, _ := json.MarshalIndent(response, "", "  ")
	fmt.Println(string(pretty))
}
