package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
)

// SearchFlights is the entry point for all flight searches
func SearchFlights(opts SearchOptions) ([]FlightInfo, string, error) {
	scraperURL := os.Getenv("SCRAPER_URL")
	if scraperURL == "" {
		scraperURL = "http://localhost:8085" // Default local fallback if ENV not set
	}
	log.Printf("🛰️ Using Playwright Service at: %s", scraperURL)

	// If it's a round trip, we perform two separate one-way searches to be more reliable
	// (Google Flights Round-Trip UI is complex to scrape with single screenshots)
	if opts.StartDate != "" && opts.EndDate != "" {
		log.Printf("🔄 Round-trip detected. Performing dual-pass search...")
		
		// 1. Search Departing Leg
		log.Printf("🛫 Searching DEPARTING leg: %s -> %s (%s)", opts.Departure, opts.Destination, opts.StartDate)
		depOpts := opts
		depOpts.EndDate = "" // Treat as one-way
		depFlights, screenshotPath, err := SearchFlightsWithScraper(scraperURL, depOpts)
		if err != nil {
			return nil, "", fmt.Errorf("departing leg failed: %v", err)
		}
		for i := range depFlights {
            if depFlights[i].Airline == "" { depFlights[i].Airline = "Unknown" }
			depFlights[i].Airline = "[DEPART] " + depFlights[i].Airline
		}

		// 2. Search Returning Leg
		log.Printf("🛬 Searching RETURNING leg: %s -> %s (%s)", opts.Destination, opts.Departure, opts.EndDate)
		retOpts := opts
		retOpts.Departure = opts.Destination
		retOpts.Destination = opts.Departure
		retOpts.StartDate = opts.EndDate
		retOpts.EndDate = ""
		retFlights, _, err := SearchFlightsWithScraper(scraperURL, retOpts)
		if err != nil {
			log.Printf("⚠️ Warning: Returning leg search failed: %v. Returning only departing flights.", err)
			return depFlights, screenshotPath, nil
		}
		for i := range retFlights {
            if retFlights[i].Airline == "" { retFlights[i].Airline = "Unknown" }
			retFlights[i].Airline = "[RETURN] " + retFlights[i].Airline
		}

		allFlights := append(depFlights, retFlights...)
		return allFlights, screenshotPath, nil
	}

	return SearchFlightsWithScraper(scraperURL, opts)
}
 
// SearchFlightsWithScraper performs a synchronous scrape request to the Playwright service
func SearchFlightsWithScraper(scraperURL string, opts SearchOptions) ([]FlightInfo, string, error) {
	cabinQuery := ""
	cabinParam := ""
	cabinLower := strings.ToLower(opts.CabinClass)
	
	if strings.Contains(cabinLower, "business") {
		cabinQuery = " business class"
		cabinParam = "&tf=sc:b"
	} else if strings.Contains(cabinLower, "premium") {
		cabinQuery = " premium economy"
		cabinParam = "&tf=sc:p"
	} else if strings.Contains(cabinLower, "first") {
		cabinQuery = " first class"
		cabinParam = "&tf=sc:f"
	}

	log.Printf("🛂 Cabin selection: '%s' -> Query suffix: '%s', Param: '%s'", opts.CabinClass, cabinQuery, cabinParam)

	queryText := fmt.Sprintf("flights from %s to %s on %s return %s%s", opts.Departure, opts.Destination, opts.StartDate, opts.EndDate, cabinQuery)
	searchURL := fmt.Sprintf("https://www.google.com/travel/flights?q=%s&hl=%s&gl=%s&curr=%s%s", strings.ReplaceAll(queryText, " ", "+"), opts.Language, opts.Region, opts.Currency, cabinParam)
	rootURL := fmt.Sprintf("https://www.google.com/travel/flights?hl=%s&gl=%s&curr=%s", opts.Language, opts.Region, opts.Currency)

	// 2. Build the navigation script (PREFER environment variable from YAML for rapid iteration)
	envScript := os.Getenv("FLIGHT_SCRAPER_SCRIPT")
	var tsConfig string

	if envScript != "" {
		log.Printf("📜 Using custom scrape script from environment variable...")
		// Use fmt.Sprintf if the script contains %s placeholders for URLs
		if strings.Contains(envScript, "%s") {
			tsConfig = fmt.Sprintf(envScript, rootURL, searchURL)
		} else {
			tsConfig = envScript
		}
	} else {
		log.Printf("📜 Using default built-in scrape script...")
		tsConfig = fmt.Sprintf(`
		await page.setViewportSize({ width: 1920, height: 2500 });
		await page.setUserAgent("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36");
		
		// 1. Initial load to clear consent and set locale
		console.log("Stage 1: Clearing consent on Travel Flights...");
		await page.goto("%s");
		try { await page.waitForLoadState("networkidle", { timeout: 10000 }); } catch(e) {}
		await page.bypassConsent();
		await page.waitForTimeout(2000); 

		// 2. Perform the search - use the MOST stable entry point
		console.log("Stage 2: Performing search via direct URL...");
		await page.goto("%s&sort=price_asc");
		await page.waitForLoadState("networkidle", { timeout: 15000 });
		await page.bypassConsent(); // Redundant check in case search triggers it
		
		// Final check for results - USE SIMPLE CSS (No quotes for regex safety)
		console.log("Waiting for results table...");
		try {
			await page.waitForSelector("div[role=listitem], .pI9Vpc, .nS495e", { timeout: 30000 });
		} catch (e) {
			console.log("Results selector timeout - taking screenshot anyway");
		}
		
		// 3. Scroll and Expand to ensure all results load
		console.log("Stage 3: Deep scrolling and expanding results...");
		await page.evaluate(async () => {
			for (let i = 0; i < 5; i++) {
				window.scrollBy(0, 1000);
				await new Promise(r => setTimeout(r, 800));
			}
			// Attempt to click expansion buttons for 'Other flights'
			const buttons = Array.from(document.querySelectorAll("button"));
			const moreBtn = buttons.find(b => b.textContent.includes("More flights") || b.textContent.includes("Other departing flights") || b.ariaLabel?.includes("Show more"));
			if (moreBtn) moreBtn.click();
			
			window.scrollBy(0, 2000);
			await new Promise(r => setTimeout(r, 1000));
			window.scrollTo(0, 0);
		});
		await page.waitForTimeout(2000);
	`, rootURL, searchURL)
	}

	screenshotPath := getScreenshotPath()
	body, _ := json.Marshal(map[string]interface{}{
		"url":               fmt.Sprintf("https://www.google.com/travel/flights?hl=%s&gl=%s&curr=%s", opts.Language, opts.Region, opts.Currency),
		"typescript_config": tsConfig,
		"get_html":          true,
		"screenshot_path":   screenshotPath,
		"full_page":         true,
	})

	log.Printf("📥 Sending synchronous scrape request to %s...", scraperURL)
	apiURL := scraperURL + "/api/scraper/generic"
	resp, err := http.Post(apiURL, "application/json", bytes.NewBuffer(body))
	if err != nil {
		return nil, "", fmt.Errorf("request to scraper failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("scraper returned error status: %d", resp.StatusCode)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, "", fmt.Errorf("failed to decode scraper response: %v", err)
	}

	// Check if we received the screenshot
	if screenshotPath != "" {
		// File should be on PVC shared path
		log.Printf("📸 Scraper saved screenshot to shared path: %s", screenshotPath)
		
		// MAINTAIN LATEST: Copy to latest_screenshot.png so Monitor UI (wow_factor.html) shows it
		dir := strings.TrimSuffix(screenshotPath, regexp.MustCompile(`screenshot_.*\.png`).FindString(screenshotPath))
		if dir != "" {
			latestPath := dir + "latest_screenshot.png"
			imgData, err := os.ReadFile(screenshotPath)
			if err == nil {
				os.WriteFile(latestPath, imgData, 0644)
				log.Printf("📸 Updated latest_screenshot.png for Visualizer")
			}
		}
	}

	// 1. Calculate price ceiling - use a very high safety ceiling for all routes
	// Local constraints are now route-aware via outlier detection later.
	maxPrice := 25000.0 
	log.Printf("🛰️ Using route-adaptive safety cap: %.0f", maxPrice)

	// 2. Extract flights using OCR on the shared screenshot
	ocrFlights, _, _ := ExtractFlightsFromImage(screenshotPath, maxPrice)
	
	// 3. Always attempt SMART Miner on HTML to ensure nothing was missed (like EasyJet)
	var minerFlights []FlightInfo
	if html, ok := result["cleaned_html"].(string); ok && html != "" {
		if len(html) > 500 {
			log.Printf("🤖 Running SMART Miner on HTML to complement OCR results...")
			minerFlights, _ = MinerExtractFlights(html)
		}
	}
	
	// 3. Combine and de-duplicate
	flightMap := make(map[string]FlightInfo)
	genKey := func(f FlightInfo) string {
		// Use numeric price for key to avoid '£80' vs '80' mismatch
		price := parsePrice(f.Price)
		airline := strings.ToLower(f.Airline)
		// Normalize: "EasyJet (U2)" -> "easyjet"
		if idx := strings.Index(airline, "("); idx != -1 {
			airline = airline[:idx]
		}
		airline = strings.TrimSpace(airline)
		// Normalize: "10.25" -> "10:25"
		depTime := strings.ReplaceAll(f.DepartureTime, ".", ":")
		arrTime := strings.ReplaceAll(f.ArrivalTime, ".", ":")
		return fmt.Sprintf("%s-%s-%s-%.0f", airline, depTime, arrTime, price)
	}
	
	for _, f := range ocrFlights {
		flightMap[genKey(f)] = f
	}
	for _, f := range minerFlights {
		// Miner usually has better attributes and price accuracy. 
		// If Miner found it, overwrite OCR version.
		flightMap[genKey(f)] = f 
	}

	var rawResults []FlightInfo
	for _, f := range flightMap {
		rawResults = append(rawResults, f)
	}

	// 4. OUTLIER FILTERING: Remove OCR hallucinations by checking relative price
	// Approach: Robust filtering (discard astronomical prices > 4x mean)
	var flights []FlightInfo
	if len(rawResults) > 0 {
		var prices []float64
		for _, f := range rawResults {
			p := parsePrice(f.Price)
			if p > 0 && p < 99999 { prices = append(prices, p) }
		}
		
		if len(prices) > 1 {
			total := 0.0
			for _, p := range prices { total += p }
			mean := total / float64(len(prices))
			threshold := mean * 4.0 // Softened from 2x to 4x to avoid dropping valid premium/last-minute flights
			log.Printf("📊 Mean price: %.2f. Threshold (4x): %.2f. Filtering outliers...", mean, threshold)

			for _, f := range rawResults {
				p := parsePrice(f.Price)
				isRequestedAirport := false
				if opts.Departure != "" && (strings.EqualFold(f.DepartureAirport, opts.Departure) || strings.EqualFold(f.DepartureAirport, origDep)) {
					isRequestedAirport = true
				}

				// Discard if > 4x mean, UNLESS it's the specific airport requested (high trust)
				if p <= threshold || p < 150 || isRequestedAirport { 
					flights = append(flights, f)
				} else {
					log.Printf("⚠️ Dropping price outlier: %s %s (%.0f > 4x mean %.0f)", f.Airline, f.Price, p, mean)
				}
			}
		} else {
			flights = rawResults // Keep if only 1 flight found
		}
	}

	// 4. Sort by price (cheapest first)
	sort.Slice(flights, func(i, j int) bool {
		pi := parsePrice(flights[i].Price)
		pj := parsePrice(flights[j].Price)
		return pi < pj
	})

	log.Printf("📊 Combined & Sorted Results: %d (OCR: %d, Miner: %d)", len(flights), len(ocrFlights), len(minerFlights))

	finalURL, _ := result["url"].(string)
	for i := range flights {
		flights[i].CabinClass = opts.CabinClass
		if flights[i].URL == "" {
			flights[i].URL = finalURL
		}
	}

	return flights, screenshotPath, nil
}


// MinerExtractFlights uses AI to extract flight data from raw HTML snippets
func MinerExtractFlights(data string) ([]FlightInfo, error) {
	if len(data) == 0 {
		return nil, nil
	}

	// Preliminary cleanup
	tagsToRemove := []string{"script", "style", "svg", "nav", "footer", "header", "noscript", "iframe"}
	cleaned := data
	for _, tag := range tagsToRemove {
		re := regexp.MustCompile(fmt.Sprintf(`(?i)<%s[\s\S]*?</%s>`, tag, tag))
		cleaned = re.ReplaceAllString(cleaned, "")
	}

	// Remove self-closing or single tags that are noisy
	reNoisy := regexp.MustCompile(`(?i)<(?:meta|link|base|br|hr)[\s\S]*?>`)
	cleaned = reNoisy.ReplaceAllString(cleaned, "")

	snippet := cleaned
	if len(cleaned) > 25000 {
		pos := strings.Index(cleaned, "Top departing options")
		if pos == -1 { pos = strings.Index(cleaned, "Best departing flights") }
		if pos != -1 {
			start := pos - 500
			if start < 0 { start = 0 }
			end := start + 25000
			if end > len(cleaned) { end = len(cleaned) }
			snippet = cleaned[start:end]
		} else {
			snippet = cleaned[:25000]
		}
	}
	prompt := fmt.Sprintf(`### TASK: EXTRACT FLIGHT RESULTS TO JSON
### DATA SOURCE (HTML/TEXT):
%s

### RULES:
1. RETURN ONLY A VALID JSON ARRAY OF FLIGHT OBJECTS.
2. FIELDS: airline, price, departure_time, arrival_time, origin, destination, duration.
3. IGNORE ALL BAG POLICY WARNINGS (e.g., "overhead locker access", "Bags filter").
4. IGNORE COMPUTER HARDWARE.
5. IF NO FLIGHT OPTIONS ARE FOUND, RETURN [].

JSON RESULT:`, snippet)

	model := os.Getenv("LLM_MODEL")
	if model == "" { model = "qwen2.5-coder:7b" }

	log.Printf("🤖 Calling LLM Miner with model %s (snippet length: %d)...", model, len(snippet))

	ollamaReq := map[string]interface{}{
		"model":  model,
		"prompt": prompt,
		"stream": false,
	}
	jsonReq, _ := json.Marshal(ollamaReq)

	req, _ := http.NewRequest("POST", getOllamaURL()+"/api/generate", bytes.NewBuffer(jsonReq))
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{
		Timeout: 60 * time.Second,
	}
	resp, err := client.Do(req)
	if err != nil { 
		log.Printf("❌ LLM Miner request failed or timed out: %v", err)
		return nil, err 
	}
	defer resp.Body.Close()

	log.Printf("📥 LLM Miner responded. Parsing...")
	var ollamaResp struct {
		Response string `json:"response"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&ollamaResp); err != nil {
		return nil, fmt.Errorf("failed to decode LLM response: %v", err)
	}

	cleanResp := ollamaResp.Response
	// Remove markdown code blocks if present
	cleanResp = strings.TrimPrefix(cleanResp, "```json")
	cleanResp = strings.TrimPrefix(cleanResp, "```")
	cleanResp = strings.TrimSuffix(cleanResp, "```")
	cleanResp = strings.TrimSpace(cleanResp)
	
	if idx := strings.Index(cleanResp, "["); idx != -1 {
		cleanResp = cleanResp[idx:]
		if last := strings.LastIndex(cleanResp, "]"); last != -1 {
			cleanResp = cleanResp[:last+1]
		}
	} else if idx := strings.Index(cleanResp, "{"); idx != -1 {
		cleanResp = cleanResp[idx:]
		if last := strings.LastIndex(cleanResp, "}"); last != -1 {
			cleanResp = cleanResp[:last+1]
		}
	}

	log.Printf("🤖 LLM Miner cleaned JSON: %s", cleanResp)
	
	var jsonData interface{}
	if err := json.Unmarshal([]byte(cleanResp), &jsonData); err != nil {
		return nil, fmt.Errorf("failed to unmarshal Miner JSON: %v", err)
	}

	var rawFlights []interface{}
	switch v := jsonData.(type) {
	case []interface{}:
		rawFlights = v
	case map[string]interface{}:
		// 1. Check common wrapper keys
		for _, key := range []string{"flights", "flight", "flight_details", "flight_info", "data"} {
			if f, ok := v[key].([]interface{}); ok {
				rawFlights = f
				break
			}
			if f, ok := v[key].(map[string]interface{}); ok {
				rawFlights = append(rawFlights, f)
				break
			}
		}
		// 2. If still empty, check if the map itself is a flight
		if len(rawFlights) == 0 {
			if _, ok := v["airline"]; ok {
				rawFlights = append(rawFlights, v)
			} else {
				// 3. Last ditch: try to find ANY nested map with an airline
				var searchMap func(m map[string]interface{})
				searchMap = func(m map[string]interface{}) {
					if _, ok := m["airline"]; ok {
						rawFlights = append(rawFlights, m)
						return
					}
					for _, val := range m {
						if child, ok := val.(map[string]interface{}); ok {
							searchMap(child)
							if len(rawFlights) > 0 { return }
						}
					}
				}
				searchMap(v)
			}
		}
	}

	var flights []FlightInfo
	for _, item := range rawFlights {
		m, ok := item.(map[string]interface{})
		if !ok { continue }

		f := FlightInfo{}
		
		// Recursive helper to find a key in nested maps
		var findVal func(obj interface{}, key string) string
		findVal = func(obj interface{}, key string) string {
			m, ok := obj.(map[string]interface{})
			if !ok { return "" }
			
			keyLower := strings.ToLower(key)

			// 1. Try exact match first
			if v, ok := m[key]; ok {
				if child, ok := v.(map[string]interface{}); ok {
					// Special case: if we found a map for "price", look for "amount" inside it
					if strings.Contains(strings.ToLower(key), "price") {
						if amt, ok := child["amount"]; ok { return fmt.Sprintf("%v", amt) }
						if total, ok := child["total"]; ok { return fmt.Sprintf("%v", total) }
					}
					// If it's a map but not price-related, just continue searching inside
				} else {
					return fmt.Sprintf("%v", v)
				}
			}
			
			// 2. Try case-insensitive substring match in current level
			for k, v := range m {
				if strings.Contains(strings.ToLower(k), keyLower) {
					return fmt.Sprintf("%v", v)
				}
			}
			
			// 3. Recurse into children
			for _, v := range m {
				if child, ok := v.(map[string]interface{}); ok {
					if found := findVal(child, key); found != "" {
						return found
					}
				}
			}
			return ""
		}

		f.Airline = findVal(m, "airline")
		
		// Priority price check
		f.Price = findVal(m, "price")
		if f.Price == "" { f.Price = findVal(m, "amount") }
		if f.Price == "" { f.Price = findVal(m, "total") }
		if f.Price == "" { f.Price = findVal(m, "cost") }
		
		f.DepartureTime = findVal(m, "departure_time")
		if f.DepartureTime == "" { f.DepartureTime = findVal(m, "time") }
		
		f.ArrivalTime = findVal(m, "arrival_time")
		f.Duration = findVal(m, "duration")
		f.Stops = findVal(m, "stops")
		
		// Robust Airport search
		f.DepartureAirport = extractIATA(findVal(m, "departure_airport"))
		if f.DepartureAirport == "" { f.DepartureAirport = extractIATA(findVal(m, "origin")) }
		if f.DepartureAirport == "" { f.DepartureAirport = extractIATA(findVal(m, "from")) }
		if f.DepartureAirport == "" { f.DepartureAirport = extractIATA(findVal(m, "dep")) }

		f.ArrivalAirport = extractIATA(findVal(m, "arrival_airport"))
		if f.ArrivalAirport == "" { f.ArrivalAirport = extractIATA(findVal(m, "destination")) }
		if f.ArrivalAirport == "" { f.ArrivalAirport = extractIATA(findVal(m, "to")) }
		if f.ArrivalAirport == "" { f.ArrivalAirport = extractIATA(findVal(m, "arr")) }
		if f.ArrivalAirport == "" { f.ArrivalAirport = extractIATA(findVal(m, "dest")) }

		if f.Price != "" && f.Airline != "" {
			flights = append(flights, f)
		}
	}

	return flights, nil
}

func parsePrice(priceStr string) float64 {
	priceStr = strings.ReplaceAll(priceStr, ",", "")
	re := regexp.MustCompile(`[\d.]+`)
	match := re.FindString(priceStr)
	if match == "" {
		return 999999
	}
	var p float64
	fmt.Sscanf(match, "%f", &p)
	return p
}

func extractIATA(s string) string {
	if s == "" { return "" }
	s = strings.ToUpper(s)
	
	iata := ""
	// Try to find code in parentheses: "London (LHR)" -> "LHR"
	re := regexp.MustCompile(`\(([A-Z]{3})\)`)
	if m := re.FindStringSubmatch(s); len(m) > 1 {
		iata = m[1]
	} else {
		re2 := regexp.MustCompile(`\b([A-Z]{3})\b`)
		if m := re2.FindStringSubmatch(s); len(m) > 1 {
			iata = m[1]
		} else if len(m) > 0 {
			iata = m[0]
		}
	}

	if iata != "" {
		// Common OCR corrections for airport codes
		corrections := map[string]string{
			"LEW": "LGW", "LOW": "LGW", "LHA": "LHR", "COG": "CDG", "ORG": "ORY",
		}
		if corr, ok := corrections[iata]; ok {
			return corr
		}
	}
	
	return iata
}
