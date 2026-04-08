package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

const VERSION = "57"

func SearchFlightsWithScraper(scraperURL string, opts SearchOptions) ([]FlightInfo, error) {
	tsConfig := fmt.Sprintf(`
		await page.setViewportSize({ width: 1920, height: 2000 });
		await page.setUserAgent("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36");
		
		// 1. Initial load to clear consent
		console.log("Stage 1: Clearing consent...");
		await page.goto("https://www.google.com/travel/flights?hl=%s&gl=%s&curr=%s");
		await page.waitForLoadState("networkidle");
		await page.bypassConsent();
		await page.waitForTimeout(3000); 

		// 2. Now perform the actual search with cookies set
		console.log("Stage 2: Performing flight search...");
		await page.goto("https://www.google.com/travel/flights?q=%s+flights+from+%s+to+%s+on+%s+return+%s&hl=%s&gl=%s&curr=%s");
		await page.waitForLoadState("networkidle");
		await page.bypassConsent(); // Try again in case navigation triggered another banner
		
		// 3. Long wait for results to populate and scroll to trigger lazy loading
		await page.waitForTimeout(20000); 
		console.log("Stage 3: Scrolling to bottom...");
		await page.evaluate(() => { window.scrollTo(0, document.body.scrollHeight); });
		await page.waitForTimeout(2000);
		await page.evaluate(() => { window.scrollTo(0, 0); });
		await page.waitForTimeout(1000);
	`, opts.Language, opts.Region, opts.Currency, opts.CabinClass, opts.Departure, opts.Destination, opts.StartDate, opts.EndDate, opts.Language, opts.Region, opts.Currency)

	payload := map[string]interface{}{
		"url":               fmt.Sprintf("https://www.google.com/travel/flights?hl=%s&gl=%s&curr=%s", opts.Language, opts.Region, opts.Currency),
		"user_agent":        "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36",
		"typescript_config": tsConfig,
		"get_html":          true,
		"full_page":         true,
	}

	body, _ := json.Marshal(payload)
	resp, err := http.Post(scraperURL+"/scrape/start", "application/json", bytes.NewBuffer(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var startResp struct {
		JobID string `json:"job_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&startResp); err != nil {
		return nil, err
	}

	log.Printf("📥 Scraper job %s created. Polling...", startResp.JobID)

	// Poll for completion
	for attempt := 0; attempt < 120; attempt++ {
		time.Sleep(2 * time.Second)
		jobURL := scraperURL + "/scrape/job?job_id=" + startResp.JobID
		jobResp, err := http.Get(jobURL)
		if err != nil {
			continue
		}
		
		var job struct {
			Status string                 `json:"status"`
			Result map[string]interface{} `json:"result"`
			Error  string                 `json:"error"`
		}
		if err := json.NewDecoder(jobResp.Body).Decode(&job); err != nil {
			jobResp.Body.Close()
			continue
		}
		jobResp.Body.Close()

		if job.Status == "completed" {
			log.Printf("✅ Job %s completed. Processing results...", startResp.JobID)
			if b64, ok := job.Result["screenshot"].(string); ok && b64 != "" {
				dataStr := strings.TrimPrefix(b64, "data:image/png;base64,")
				imgData, _ := base64.StdEncoding.DecodeString(dataStr)
				tmpPath := getScreenshotPath()
				_ = os.WriteFile(tmpPath, imgData, 0644)
				
				flights, _ := ExtractFlightsFromImage(tmpPath)
				if len(flights) > 0 {
					log.Printf("🎉 Found %d flights via OCR", len(flights))
					return flights, nil
				}
				
				// OCR Failed or too short - try Miner extraction on RAW HTML if available
				log.Printf("⚠️ OCR produced %d results. Attempting SMART Miner fallback on HTML...", len(flights))
				if html, ok := job.Result["raw_html"].(string); ok && html != "" {
					minerFlights, err := MinerExtractFlights(html)
					if err == nil && len(minerFlights) > 0 {
						log.Printf("🎉 Found %d flights via SMART HTML Miner", len(minerFlights))
						return minerFlights, nil
					}
					log.Printf("⚠️ Miner found 0 flights in HTML")
				}

				return nil, fmt.Errorf("no flights found via OCR or Miner")
			}
			return nil, fmt.Errorf("no screenshot returned from scraper")
		}

		if job.Status == "failed" {
			return nil, fmt.Errorf("job failed: %s", job.Error)
		}
	}

	return nil, fmt.Errorf("job timed-out")
}

func MinerExtractFlights(data string) ([]FlightInfo, error) {
	if len(data) == 0 {
		return nil, nil
	}
	
	// Smart snippet selection
	snippet := data
	if len(data) > 40000 {
		// High-confidence anchors that usually appear at the start of the results table
		resultsAnchors := []string{
			"Top departing options",
			"Best departing flights",
			"Other departing flights",
			"British Airways",
			"Air France",
			"Price, stops, and airline",
		}
		
		pos := -1
		for _, target := range resultsAnchors {
			if idx := strings.Index(data, target); idx != -1 {
				pos = idx
				log.Printf("🎯 Found results anchor '%s' at position %d", target, pos)
				break
			}
		}

		// Fallback to searching for flight results indicators (Price + Nonstop etc)
		// Usually follow the flight-heavy section
		if pos == -1 {
			indicators := []string{"Top departing options", "Best departing flights", "Other departing flights", "£", "€", "Nonstop", "1 stop", "Select flight"}
			for _, indicator := range indicators {
				if idx := strings.LastIndex(data, indicator); idx != -1 {
					// Results usually aren't right at the top
					if idx > 2000 {
						pos = idx
						log.Printf("🎯 Found indicator anchor '%s' at position %d", indicator, pos)
						break
					}
				}
			}
		}

		if pos != -1 {
			start := pos - 1000
			if start < 0 { start = 0 }
			end := start + 40000 // 40KB is fast and usually enough
			if end > len(data) { end = len(data) }
			snippet = data[start:end]
		} else {
			// Deep fallback: the center of the cleaned HTML
			log.Println("⚠️ No anchors found, using center-weighted fallback")
			start := len(data) / 6 
			end := start + 40000
			if end > len(data) { end = len(data) }
			snippet = data[start:end]
		}
	}

	preview := snippet
	if len(preview) > 200 {
		preview = preview[:200]
	}
	log.Printf("📝 Snippet preview (first 200): %s", strings.ReplaceAll(preview, "\n", " "))

	prompt := fmt.Sprintf(`Task: Extract ALL flight options from the Google Flights HTML/text below.
Return a JSON array of objects with these fields ONLY:
- airline (string). IMPORTANT: This is the name of the carrier (e.g. "British Airways").
- departure_time (string, e.g. "10:30 AM")
- arrival_time (string, e.g. "1:45 PM")
- duration (string, e.g. "1h 15m")
- stops (string, e.g. "Nonstop")
- price (string, e.g. "£45"). IMPORTANT: The field name MUST be 'price', not 'total_price' or 'cost'.

Rules:
1. ONLY output the JSON array. No preamble or explanation.
2. If no flights are found, return [].
3. Ensure every object has all the fields listed above.
4. Field names MUST BE lowercase snake_case (airline, price, departure_time, etc).
5. price field MUST be a string (e.g. "£146").
6. airline field MUST be present for every flight.
7. Extract EVERY flight you can find in the data. There should be multiple options.

Data snippet:
---
%s
---
JSON array:`, snippet)

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	model := os.Getenv("LLM_MODEL")
	if model == "" || strings.Contains(model, "7b") || strings.Contains(model, "coder") {
		// Force more capable model for miner extraction
		model = "qwen3:14b"
	}

	log.Printf("🤖 Calling LLM Miner (%d chars) with model %s...", len(snippet), model)

	ollamaReq := map[string]interface{}{
		"model":  model,
		"prompt": prompt,
		"stream": false,
	}
	jsonReq, _ := json.Marshal(ollamaReq)

	client := &http.Client{}
	req, _ := http.NewRequestWithContext(ctx, "POST", getOllamaURL()+"/api/generate", bytes.NewBuffer(jsonReq))
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama call failed: %v", err)
	}
	defer resp.Body.Close()

	var ollamaResp struct {
		Response string `json:"response"`
	}
	json.NewDecoder(resp.Body).Decode(&ollamaResp)

	// Clean up the response - sometimes local models wrap in backticks
	cleanResp := ollamaResp.Response
	if idx := strings.Index(cleanResp, "["); idx != -1 {
		cleanResp = cleanResp[idx:]
	}
	if idx := strings.LastIndex(cleanResp, "]"); idx != -1 {
		cleanResp = cleanResp[:idx+1]
	}

	log.Printf("🤖 LLM Response (Cleaned): %s", cleanResp)

	// Normalization: replace common LLM-invented field names with our required ones
	cleanResp = strings.ReplaceAll(cleanResp, "\"total_price\":", "\"price\":")
	cleanResp = strings.ReplaceAll(cleanResp, "\"total_cost\":", "\"price\":")
	cleanResp = strings.ReplaceAll(cleanResp, "\"cost\":", "\"price\":")
	cleanResp = strings.ReplaceAll(cleanResp, "\"carrier\":", "\"airline\":")
	cleanResp = strings.ReplaceAll(cleanResp, "\"departure_airport\":", "\"origin\":") // Extra info is fine
	cleanResp = strings.ReplaceAll(cleanResp, "\"arrival_airport\":", "\"destination\":")

	var flights []FlightInfo
	err = json.Unmarshal([]byte(cleanResp), &flights)
	if err != nil {
		// Try parsing as an object with a "flights" array if the LLM was naughty
		var wrapper struct {
			Flights []FlightInfo `json:"flights"`
		}
		if err2 := json.Unmarshal([]byte(cleanResp), &wrapper); err2 == nil && len(wrapper.Flights) > 0 {
			flights = wrapper.Flights
		} else {
			// Deep cleanup: try to find anything between [ and ]
			log.Printf("⚠️ JSON unmarshal failed: %v. Raw length: %d", err, len(cleanResp))
			return nil, err
		}
	}

	log.Printf("🚀 Miner found %d flights", len(flights))
	return flights, nil
}
