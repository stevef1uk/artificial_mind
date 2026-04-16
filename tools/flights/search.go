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

	isOneWay := opts.EndDate == "" || opts.EndDate == opts.StartDate
	if !isOneWay {
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
			if depFlights[i].Airline == "" {
				depFlights[i].Airline = "Unknown"
			}
			depFlights[i].Airline = "[DEPART] " + depFlights[i].Airline
		}

		// 2. Search Returning Leg
		retOpts := opts
		retOpts.Departure = opts.Destination
		retOpts.Destination = opts.Departure
		retOpts.StartDate = opts.EndDate
		retOpts.EndDate = "" // Treat as one-way
		log.Printf("🛬 Searching RETURNING leg: %s -> %s (%s)", retOpts.Departure, retOpts.Destination, retOpts.StartDate)
		retFlights, _, err := SearchFlightsWithScraper(scraperURL, retOpts)
		if err != nil {
			log.Printf("⚠️ Warning: Returning leg search failed: %v. Returning only departing flights.", err)
			return depFlights, screenshotPath, nil
		}
		for i := range retFlights {
			if retFlights[i].Airline == "" {
				retFlights[i].Airline = "Unknown"
			}
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

	queryText := fmt.Sprintf("flights from %s to %s on %s%s", opts.Departure, opts.Destination, opts.StartDate, cabinQuery)
	tripParams := "&tt=oneway&it=oneway"

	if opts.EndDate != "" && opts.EndDate != opts.StartDate {
		queryText = fmt.Sprintf("flights from %s to %s on %s return %s%s", opts.Departure, opts.Destination, opts.StartDate, opts.EndDate, cabinQuery)
		tripParams = "&tt=roundtrip"
	}

	searchURL := fmt.Sprintf("https://www.google.com/travel/flights?q=%s&hl=%s&gl=%s&curr=%s%s%s",
		strings.ReplaceAll(queryText, " ", "+"), opts.Language, opts.Region, opts.Currency, cabinParam, tripParams)
	rootURL := fmt.Sprintf("https://www.google.com/travel/flights?hl=%s&gl=%s&curr=%s", opts.Language, opts.Region, opts.Currency)

	// 2. Build the navigation script (PREFER environment variable from YAML for rapid iteration)
	envScript := os.Getenv("FLIGHT_SCRAPER_SCRIPT")
	var tsConfig string
	isOneWaySearch := opts.EndDate == "" || opts.EndDate == opts.StartDate

	// Construct the dynamic action script based on goal
	actionScript := ""
	if isOneWaySearch {
		actionScript = `
		// Toggle trip type to One Way
		await page.evaluate(async () => {
			const btns = Array.from(document.querySelectorAll('button'));
			const tripBtn = btns.find(b => b.textContent.includes('Round trip') || b.textContent.includes('Aller-retour') || b.textContent.includes('ida y vuelta'));
			if (tripBtn) {
				tripBtn.click();
				await new Promise(r => setTimeout(r, 1000));
				const items = Array.from(document.querySelectorAll('li[role="option"], li'));
				const oneWay = items.find(i => i.textContent.includes('One way') || i.textContent.includes('Aller simple') || i.textContent.includes('Solo ida'));
				if (oneWay) oneWay.click();
			}
		});
		await page.waitForTimeout(2000);
		`
	}

	if envScript != "" {
		log.Printf("📜 Using custom scrape script from environment variable...")
		// The new template style expects: 1. rootURL, 2. searchURL, 3. actionScript
		placeholderCount := strings.Count(envScript, "%s")
		if placeholderCount == 3 {
			tsConfig = fmt.Sprintf(envScript, rootURL, searchURL, actionScript)
		} else if placeholderCount == 2 {
			tsConfig = fmt.Sprintf(envScript, rootURL, searchURL)
		} else {
			tsConfig = envScript
		}
	} else {
		log.Printf("📜 Using default built-in scrape script...")
		tsConfig = fmt.Sprintf(`
		await page.setViewportSize({ width: 2560, height: 1600 });
		await page.setUserAgent("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36");
		
		await page.goto("%s");
		await page.waitForLoadState("networkidle");
		await page.bypassConsent();
		await page.waitForTimeout(2000); 

		await page.goto("%s&sort=price_asc");
		await page.waitForLoadState("networkidle");
		await page.bypassConsent();
		
		%s

		await page.waitForSelector("div[role=listitem]", { timeout: 30000 });
	`, rootURL, searchURL, actionScript)
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
	ocrFlights, _, _ := ExtractFlightsFromImage(screenshotPath, maxPrice, opts)

	// 3. Always attempt SMART Miner on HTML to ensure nothing was missed (like EasyJet)
	var minerFlights []FlightInfo
	if html, ok := result["cleaned_html"].(string); ok && html != "" {
		if len(html) > 500 {
			log.Printf("🤖 Running SMART Miner on HTML to complement OCR results...")
			minerFlights, _ = MinerExtractFlights(html, opts)
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
		key := genKey(f)
		// SMART MERGE: if Miner found it but has missing fields, keep existing OCR fields
		if existing, ok := flightMap[key]; ok {
			if (f.Airline == "" || f.Airline == "Unknown") && existing.Airline != "" {
				f.Airline = existing.Airline
			}
			if (f.Duration == "" || strings.Contains(strings.ToLower(f.Duration), "unknown") || strings.Contains(strings.ToLower(f.Duration), "specified")) && existing.Duration != "" {
				f.Duration = existing.Duration
			}
			if (f.DepartureAirport == "" || f.DepartureAirport == "Unknown") && existing.DepartureAirport != "" {
				f.DepartureAirport = existing.DepartureAirport
			}
			if (f.ArrivalAirport == "" || f.ArrivalAirport == "Unknown") && existing.ArrivalAirport != "" {
				f.ArrivalAirport = existing.ArrivalAirport
			}

			// Miner price is usually better normalized, BUT validate for hallucinations
			// If miner price is suspiciously low compared to OCR mean, prefer OCR
			if f.Price == "" || f.Price == "0" {
				f.Price = existing.Price
			} else {
				minerPrice := parsePrice(f.Price)
				// Get mean of OCR prices for comparison
				var ocrPrices []float64
				for _, of := range ocrFlights {
					if parsePrice(of.Price) > 10 {
						ocrPrices = append(ocrPrices, parsePrice(of.Price))
					}
				}
				var ocrMean float64
				if len(ocrPrices) > 0 {
					sum := 0.0
					for _, p := range ocrPrices {
						sum += p
					}
					ocrMean = sum / float64(len(ocrPrices))
				}

				// If miner price is less than 30% of OCR mean, it's likely hallucinated - use OCR
				if ocrMean > 0 && minerPrice < ocrMean*0.3 {
					log.Printf("⚠️ Suspiciously low miner price £%.0f (OCR mean: £%.0f) - using OCR", minerPrice, ocrMean)
					f.Price = existing.Price
				} else if !strings.ContainsAny(f.Price, "£$€") && strings.ContainsAny(existing.Price, "£$€") {
					// Restore currency symbol from OCR if Miner dropped it
					reSym := regexp.MustCompile(`[£$€]`)
					sym := reSym.FindString(existing.Price)
					if sym == "" {
						sym = "£"
					}
					f.Price = sym + f.Price
				}
			}
		}
		flightMap[key] = f
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
			if p > 0 && p < 99999 {
				prices = append(prices, p)
			}
		}

		if len(prices) > 1 {
			total := 0.0
			for _, p := range prices {
				total += p
			}
			mean := total / float64(len(prices))
			threshold := mean * 4.0 // Softened from 2x to 4x to avoid dropping valid premium/last-minute flights
			log.Printf("📊 Mean price: %.2f. Threshold (4x): %.2f. Filtering outliers...", mean, threshold)

			for _, f := range rawResults {
				p := parsePrice(f.Price)
				isRequestedAirport := false
				if opts.Departure != "" && strings.EqualFold(f.DepartureAirport, opts.Departure) {
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
func MinerExtractFlights(data string, opts SearchOptions) ([]FlightInfo, error) {
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
	if len(cleaned) > 12000 {
		pos := strings.Index(cleaned, "Top departing options")
		if pos == -1 {
			pos = strings.Index(cleaned, "Best departing flights")
		}
		if pos != -1 {
			start := pos - 500
			if start < 0 {
				start = 0
			}
			end := start + 12000
			if end > len(cleaned) {
				end = len(cleaned)
			}
			snippet = cleaned[start:end]
		} else {
			snippet = cleaned[:12000]
		}
	}
	prompt := fmt.Sprintf(`### TASK: EXTRACT FLIGHT RESULTS TO JSON
### TARGET ROUTE: %s to %s (%s)
### CITY GROUPS: LIS=Lisbon, RIO=GIG/SDU, LON=LHR/LGW/LTN/STN, PAR=CDG/ORY, NYC=JFK/EWR/LGA
### CRITICAL: DO NOT HALLUCINATE PRICES - USE ONLY THE EXACT PRICES SHOWN IN THE DATA!
### DATA SOURCE (HTML/TEXT):
%s

### RULES:
1. RETURN ONLY A VALID JSON ARRAY OF FLIGHT OBJECTS.
2. FIELDS: airline, price (include currency symbol e.g. €450), departure_time, arrival_time (24h format), origin (IATA code e.g. LIS), destination (IATA code e.g. GIG), duration.
3. DO NOT HALLUCINATE. ONLY use prices you can see in the data. If you cannot find a price, return [].
4. IF THE DATA SOURCE DOES NOT MATCH %s TO %s (or its members), OR NO FLIGHTS ARE FOUND, RETURN [].
5. IGNORE ALL BAG POLICY WARNINGS.
6. EXCLUDE NON-AIRCRAFT RESULTS: DO NOT return any results operated by trains, buses, railways, or coaches (e.g. SBB, SNCF, National Express, Renfe, Amtrak, etc). Only return legitimate airline flights.

JSON RESULT:`, opts.Departure, opts.Destination, opts.CabinClass, snippet, opts.Departure, opts.Destination)

	model := os.Getenv("LLM_MODEL")
	if model == "" {
		model = "qwen3:14b"
	}

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
		Timeout: 120 * time.Second,
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
							if len(rawFlights) > 0 {
								return
							}
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
		if !ok {
			continue
		}

		f := FlightInfo{}

		// Recursive helper to find a key in nested maps
		var findVal func(obj interface{}, key string) string
		findVal = func(obj interface{}, key string) string {
			m, ok := obj.(map[string]interface{})
			if !ok {
				return ""
			}

			keyLower := strings.ToLower(key)

			// 1. Try exact match first
			if v, ok := m[key]; ok {
				if child, ok := v.(map[string]interface{}); ok {
					// Special case: if we found a map for "price", look for "amount" inside it
					if strings.Contains(strings.ToLower(key), "price") {
						if amt, ok := child["amount"]; ok {
							return fmt.Sprintf("%v", amt)
						}
						if total, ok := child["total"]; ok {
							return fmt.Sprintf("%v", total)
						}
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
		if f.Price == "" {
			f.Price = findVal(m, "amount")
		}
		if f.Price == "" {
			f.Price = findVal(m, "total")
		}
		if f.Price == "" {
			f.Price = findVal(m, "cost")
		}

		f.DepartureTime = findVal(m, "departure_time")
		if f.DepartureTime == "" {
			f.DepartureTime = findVal(m, "time")
		}

		f.ArrivalTime = findVal(m, "arrival_time")
		f.Duration = findVal(m, "duration")
		f.Stops = findVal(m, "stops")

		// Robust Airport search
		f.DepartureAirport = extractIATA(findVal(m, "departure_airport"))
		if f.DepartureAirport == "" {
			f.DepartureAirport = extractIATA(findVal(m, "origin"))
		}
		if f.DepartureAirport == "" {
			f.DepartureAirport = extractIATA(findVal(m, "from"))
		}
		if f.DepartureAirport == "" {
			f.DepartureAirport = extractIATA(findVal(m, "dep"))
		}

		f.ArrivalAirport = extractIATA(findVal(m, "arrival_airport"))
		if f.ArrivalAirport == "" {
			f.ArrivalAirport = extractIATA(findVal(m, "destination"))
		}
		if f.ArrivalAirport == "" {
			f.ArrivalAirport = extractIATA(findVal(m, "to"))
		}
		if f.ArrivalAirport == "" {
			f.ArrivalAirport = extractIATA(findVal(m, "arr"))
		}
		if f.ArrivalAirport == "" {
			f.ArrivalAirport = extractIATA(findVal(m, "dest"))
		}

		if f.Price != "" && f.Airline != "" {
			flights = append(flights, f)
		}
	}

	return flights, nil
}

func parsePrice(priceStr string) float64 {
	// 1. Remove thousands separators.
	// In many regions, comma is a decimal, but on Google Flights with hl=en,
	// comma is usually a thousands separator.
	// However, if we see something like "1.379", it might be thousands.
	// Let's be smart: if there's a comma followed by 3 digits at the end, it's thousands.

	// Simple heuristic: remove commas always, but if there's a dot, keep it as the primary decimal
	clean := strings.ReplaceAll(priceStr, ",", "")
	if !strings.Contains(clean, ".") && strings.Contains(priceStr, ",") {
		// If there's no dot but there was a comma, maybe the comma WAS the decimal?
		// e.g. "137,00" -> "137.00"
		clean = strings.ReplaceAll(priceStr, ",", ".")
	}

	re := regexp.MustCompile(`[\d.]+`)
	match := re.FindString(clean)
	if match == "" {
		return 999999
	}
	var p float64
	fmt.Sscanf(match, "%f", &p)
	return p
}

func extractIATA(s string) string {
	s = strings.TrimSpace(strings.ToUpper(s))
	if s == "" {
		return ""
	}

	// If already looks like a code, just clean it
	if len(s) == 3 && regexp.MustCompile(`^[A-Z]{3}$`).MatchString(s) {
		return s
	}

	var iata string
	re := regexp.MustCompile(`\(([A-Z]{3})\)`)
	if m := re.FindStringSubmatch(s); len(m) > 1 {
		iata = m[1]
	} else {
		re2 := regexp.MustCompile(`\b([A-Z]{3})\b`)
		if m := re2.FindStringSubmatch(s); len(m) > 1 {
			iata = m[1]
		}
	}

	// HEURISTIC: If no 3-letter code found, but it looks like a city name,
	// we could map it, but for now we just return "" and let the smart merge use OCR code.

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
