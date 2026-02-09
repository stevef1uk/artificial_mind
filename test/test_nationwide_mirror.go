package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

// Nationwide Mirror Test in Go
// This script mirrors the behavior of test_nationwide_auto_config.py
// by calling the HDN server's MCP endpoint.

func main() {
	hdnURL := "http://localhost:8081/mcp"
	targetURL := "https://www.nationwide.co.uk/savings/compare-savings-accounts-and-isas/"

	fmt.Println("🧪 Testing Nationwide with Auto-Generation via Go mirror...")
	fmt.Printf("   POST %s\n", hdnURL)
	fmt.Printf("   URL: %s\n", targetURL)
	fmt.Println("   ⏳ LLM is generating Playwright TypeScript config automatically (via smart_scrape)...")

	// 1. Prepare Payload
	payload := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name": "smart_scrape",
			"arguments": map[string]interface{}{
				"url":  targetURL,
				"goal": "Extract savings products and interest rates",
				"extractions": map[string]string{
					"product_names":  `Table__ProductName[^>]*>\s*([^<]+)<`,
					"interest_rates": `data-ref=['"]heading['"]>\s*([0-9.]+%)</div>`,
				},
			},
		},
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		log.Fatalf("❌ Failed to marshal payload: %v", err)
	}

	// 2. Send Request
	startTime := time.Now()
	resp, err := http.Post(hdnURL, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		log.Fatalf("❌ Request failed: %v", err)
	}
	defer resp.Body.Close()

	duration := time.Since(startTime)

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		log.Fatalf("❌ HTTP Error %d: %s", resp.StatusCode, string(body))
	}

	// 3. Parse and Print Response
	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		log.Fatalf("❌ Failed to decode response: %v", err)
	}

	fmt.Printf("\n✅ Received response in %v\n", duration.Round(time.Millisecond))

	if errVal, ok := result["error"]; ok {
		fmt.Printf("❌ JSON-RPC Error: %v\n", errVal)
		return
	}

	// Extract data from the result map
	// The structure is result["result"] -> map which contains "result" and "content"
	dataMap, ok := result["result"].(map[string]interface{})
	if !ok {
		fmt.Printf("⚠️  Warning: result[\"result\"] is not a map. Raw result: %+v\n", result["result"])
		return
	}

	// Now get the actual extraction result
	innerResult, ok := dataMap["result"].(map[string]interface{})
	if !ok {
		fmt.Printf("⚠️  Warning: dataMap[\"result\"] is not a map. Raw dataMap: %+v\n", dataMap)
		return
	}

	productStr, _ := innerResult["product_names"].(string)
	rateStr, _ := innerResult["interest_rates"].(string)

	if productStr == "" && rateStr == "" {
		fmt.Println("⚠️  Warning: No data found in product_names or interest_rates.")
		pretty, _ := json.MarshalIndent(innerResult, "", "  ")
		fmt.Printf("Raw Data Returned:\n%s\n", string(pretty))
		return
	}

	products := strings.Split(strings.TrimSpace(productStr), "\n")
	rates := strings.Split(strings.TrimSpace(rateStr), "\n")

	fmt.Println("\n📊 Nationwide Savings Rates (AI Extracted):")
	fmt.Printf("%-35s | %-15s\n", "PRODUCT", "INTEREST RATE")
	fmt.Println(strings.Repeat("-", 55))

	maxRows := len(products)
	if len(rates) < maxRows && len(rates) > 0 {
		maxRows = len(rates)
	}
	if maxRows == 0 && (len(products) > 0 || len(rates) > 0) {
		if len(products) > 0 {
			maxRows = len(products)
		} else {
			maxRows = len(rates)
		}
	}

	for i := 0; i < maxRows; i++ {
		p := ""
		if i < len(products) {
			p = products[i]
		}
		r := ""
		if i < len(rates) {
			r = rates[i]
		}
		fmt.Printf("%-35s | %-15s\n", p, r)
	}

	if len(products) != len(rates) {
		fmt.Printf("\n⚠️ Note: Mismatch in counts (Products: %d, Rates: %d).\n", len(products), len(rates))
	}

	fmt.Printf("\n📄 Page Title: %v\n", innerResult["page_title"])
	fmt.Printf("🌐 Source: %v\n", innerResult["page_url"])
}
