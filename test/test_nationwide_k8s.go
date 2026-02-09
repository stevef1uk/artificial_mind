package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

// Nationwide K3s Test in Go
// This script calls the HDN server running in the K3s cluster.

func main() {
	// 1. Get HDN URL from environment or use K3s NodePort default
	hdnURL := os.Getenv("HDN_URL")
	if hdnURL == "" {
		// Default to the NodePort found in the cluster (RPI IP + 30257)
		// We can try to guess the RPI IP or just use localhost if port-forwarded
		hdnURL = "http://localhost:30257/mcp"
	}

	if !strings.HasSuffix(hdnURL, "/mcp") {
		hdnURL = strings.TrimSuffix(hdnURL, "/") + "/mcp"
	}

	targetURL := "https://www.nationwide.co.uk/savings/compare-savings-accounts-and-isas/"

	fmt.Println("🧪 Testing Nationwide via K3s HDN...")
	fmt.Printf("   POST %s\n", hdnURL)
	fmt.Printf("   URL: %s\n", targetURL)
	fmt.Println("   ⏳ LLM is generating Playwright TypeScript config automatically...")

	// 2. Prepare Payload
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

	jsonData, _ := json.Marshal(payload)

	// 3. Send Request
	startTime := time.Now()
	resp, err := http.Post(hdnURL, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		log.Fatalf("❌ Request failed: %v\n   Tip: Make sure you have the right IP/Port or are port-forwarding to 30257", err)
	}
	defer resp.Body.Close()

	duration := time.Since(startTime)

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		log.Fatalf("❌ HTTP Error %d: %s", resp.StatusCode, string(body))
	}

	// 4. Parse Results
	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)

	fmt.Printf("\n✅ Received response in %v\n", duration.Round(time.Millisecond))

	if errVal, ok := result["error"]; ok {
		fmt.Printf("❌ JSON-RPC Error: %v\n", errVal)
		return
	}

	dataMap, ok := result["result"].(map[string]interface{})
	if !ok {
		fmt.Printf("⚠️  Unexpected format: %+v\n", result["result"])
		return
	}

	innerResult, _ := dataMap["result"].(map[string]interface{})
	productStr, _ := innerResult["product_names"].(string)
	rateStr, _ := innerResult["interest_rates"].(string)

	if productStr == "" && rateStr == "" {
		fmt.Println("⚠️  No data found. Check HDN logs.")
		return
	}

	products := strings.Split(strings.TrimSpace(productStr), "\n")
	rates := strings.Split(strings.TrimSpace(rateStr), "\n")

	fmt.Println("\n📊 Nationwide Savings Rates (K3s Status):")
	fmt.Printf("%-35s | %-15s\n", "PRODUCT", "INTEREST RATE")
	fmt.Println(strings.Repeat("-", 55))

	maxRows := len(products)
	if len(rates) < maxRows && len(rates) > 0 {
		maxRows = len(rates)
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

	fmt.Printf("\n📄 Page Title: %v\n", innerResult["page_title"])
	fmt.Printf("🌐 Source: %v\n", innerResult["page_url"])
}
