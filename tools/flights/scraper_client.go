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
	"regexp"
	"strings"
	"time"
)

const VERSION = "57"

func SearchFlightsWithScraper(scraperURL string, opts SearchOptions) ([]FlightInfo, error) {
	tsConfig := fmt.Sprintf(`
		await page.setViewportSize({ width: 1920, height: 2500 });
		await page.setUserAgent("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36");
		
		// 1. Initial load to clear consent
		console.log("Stage 1: Clearing consent...");
		await page.goto("https://www.google.com/travel/flights?hl=%s&gl=%s&curr=%s");
		try { await page.waitForLoadState("networkidle", { timeout: 10000 }); } catch(e) {}
		await page.bypassConsent();
		await page.waitForTimeout(3000); 

		// 2. Now perform the actual search using the /search path which is more robust
		console.log("Stage 2: Performing flight search...");
		// Use a more structured query and include explicit origin/destination params if possible
		const query = "%s+to+%s+%s+to+%s";
		const searchURL = "https://www.google.com/travel/flights/search?q=" + encodeURIComponent(query) + "&hl=%s&gl=%s&curr=%s";
		console.log("Navigating to: " + searchURL);
		await page.goto(searchURL);
		
		// Wait for the results selector
		console.log("Waiting for results table...");
		const resultsSelector = "div[role='listitem'], li.pI9Vpc, .Tf99Ab";
		try {
			await page.waitForSelector(resultsSelector, { timeout: 35000 });
			console.log("✅ Results table detected.");
		} catch (e) {
			console.log("⚠️ Results table not found within timeout, trying one last scroll...");
			await page.evaluate(() => { window.scrollBy(0, 1000); });
			await page.waitForTimeout(5000);
		}
		
		await page.waitForTimeout(5000); // Buffer for animations
		
		// 3. Scroll to ensure all lazy elements load
		console.log("Stage 3: Scrolling for lazy-loading...");
		for (let i = 0; i < 4; i++) {
			await page.evaluate(() => { window.scrollBy(0, 800); });
			await page.waitForTimeout(1500);
		}
		await page.evaluate(() => { window.scrollTo(0, 0); });
		await page.waitForTimeout(2000);
	`, opts.Language, opts.Region, opts.Currency, opts.Departure, opts.Destination, opts.StartDate, opts.EndDate, opts.Language, opts.Region, opts.Currency)

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

	// 0. Preliminary cleanup: strip massive <script> and <style> blocks that confuse the LLM
	// Google Flights embeds multi-megabyte JSON blobs in <script> tags which are irrelevant to the visual results.
	reScripts := regexp.MustCompile(`(?i)<script[\s\S]*?</script>`)
	cleaned := reScripts.ReplaceAllString(data, "")
	reStyles := regexp.MustCompile(`(?i)<style[\s\S]*?</style>`)
	cleaned = reStyles.ReplaceAllString(cleaned, "")
	
	// Smart snippet selection using the CLEANED data
	snippet := cleaned
	if len(cleaned) > 40000 {
		// High-confidence anchors that usually appear at the start of the results table
		resultsAnchors := []string{
			"Top departing options",
			"Best departing flights",
			"Other departing flights",
			"Price, stops, and airline",
			"Select flight",
		}
		
		pos := -1
		for _, target := range resultsAnchors {
			if idx := strings.Index(cleaned, target); idx != -1 {
				// Avoid false positives at the very start of the doc (unlikely to be the table)
				if idx > 1000 {
					pos = idx
					log.Printf("🎯 Found results anchor '%s' at position %d", target, pos)
					break
				}
			}
		}

		// Fallback to searching for flight results indicators (Price + Nonstop etc)
		// Usually follow the flight-heavy section
		if pos == -1 {
			indicators := []string{"Top departing options", "Best departing flights", "Other departing flights", "£", "€", "Nonstop", "1 stop", "Select flight"}
			for _, indicator := range indicators {
				if idx := strings.LastIndex(cleaned, indicator); idx != -1 {
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
			if end > len(cleaned) { end = len(cleaned) }
			snippet = cleaned[start:end]
		} else {
			// Deep fallback: the center of the cleaned HTML
			log.Println("⚠️ No anchors found, using center-weighted fallback")
			start := len(cleaned) / 6 
			end := start + 40000
			if end > len(cleaned) { end = len(cleaned) }
			snippet = cleaned[start:end]
		}
	}

	preview := snippet
	if len(preview) > 200 {
		preview = preview[:200]
	}
	log.Printf("📝 Snippet preview (first 200): %s", strings.ReplaceAll(preview, "\n", " "))

	prompt := fmt.Sprintf(`Task: Extract ALL flight options from the Google Flights HTML/text below. 
Return a JSON array of objects with these fields ONLY:
- airline (string, e.g. "British Airways")
- departure_time (string, e.g. "10:30 AM")
- arrival_time (string, e.g. "1:45 PM")
- duration (string, e.g. "1h 15m")
- stops (string, e.g. "Nonstop")
- price (string, e.g. "£45")

🚨 CRITICAL RULES:
1. ONLY return a JSON array of flights. 
2. NEVER include FAQ questions, help text, or "People also ask" sections.
3. If no actual flight prices/airlines are present, return [].
4. An airline MUST BE a specific carrier name, not a general concept.
5. NO preambles or chat. ONLY JSON.

Example Valid Result:
[{"airline": "Air France", "departure_time": "12:00 PM", "arrival_time": "1:15 PM", "duration": "1h 15m", "stops": "Nonstop", "price": "£84"}]

Data snippet to process:
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

	log.Printf("🚀 Miner found %d flights (pre-dedupe)", len(flights))

	// Validation and Normalization: Ensure they actually look like flights and aren't FAQs or boilerplate
	validFlights := []FlightInfo{}
	for _, f := range flights {
		// Clean airline name
		f.Airline = strings.TrimSpace(f.Airline)
		f.Price = strings.TrimSpace(f.Price)
		
		// CRITICAL: A flight MUST have an airline and a price to be valid
		// This prevents the Miner from hallucinating FAQs as flights
		if f.Airline == "" || f.Price == "" || strings.EqualFold(f.Airline, "unknown") || strings.EqualFold(f.Price, "unknown") {
			continue
		}
		
		// Filter out obviously non-flight content (e.g., FAQ answers the LLM might have grabbed)
		if len(f.Airline) > 50 || len(f.Price) > 20 {
			continue
		}

		validFlights = append(validFlights, f)
	}

	// De-duplicate identical flights (fixes LLM seeing same flight twice in different sections)
	uniqueFlights := []FlightInfo{}
	seen := make(map[string]bool)
	for _, f := range validFlights {
		key := fmt.Sprintf("%s-%s-%s-%s", f.Airline, f.DepartureTime, f.ArrivalTime, f.Price)
		if !seen[key] {
			seen[key] = true
			uniqueFlights = append(uniqueFlights, f)
		}
	}
	
	if len(uniqueFlights) == 0 && len(flights) > 0 {
		log.Printf("⚠️ All %d extracted items failed validation (hallucinated FAQs?). Returning 0.", len(flights))
	} else {
		log.Printf("🚀 Miner produced %d unique valid flights", len(uniqueFlights))
	}
	
	return uniqueFlights, nil
}
