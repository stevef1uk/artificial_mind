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

	"github.com/playwright-community/playwright-go"
)

// SearchFlights is the entry point for all flight searches
func SearchFlights(opts SearchOptions) ([]FlightInfo, string, error) {
	// 1. Check for Playwright Scraper Service (Primary for K3s/Offloading)
	scraperURL := os.Getenv("SCRAPER_URL")
	if scraperURL != "" {
		log.Printf("🛰️ Using Playwright Service at: %s", scraperURL)
		return SearchFlightsWithScraper(scraperURL, opts)
	}
 
	// 2. Fallback to Native logic
	log.Printf("🏠 Using NATIVE search logic (Version 60)...")
	return SearchFlightsNative(opts)
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
		
		// Final check for results
		console.log("Waiting for results table...");
		try {
			await page.waitForSelector("div[role='listitem'], .pI9Vpc, [jsname='I897t'], [aria-label^='Flight'], .nS495e, h3:has-text('options'), h3:has-text('flights'), div:has-text('Best'), div:has-text('Cheapest')", { timeout: 30000 });
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
	}

	// 1. Extract flights using OCR on the shared screenshot
	ocrFlights, _ := ExtractFlightsFromImage(screenshotPath)
	
	// 2. Always attempt SMART Miner on HTML to ensure nothing was missed (like EasyJet)
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
		// Normalize time: "10.25" -> "10:25"
		time := strings.ReplaceAll(f.DepartureTime, ".", ":")
		return fmt.Sprintf("%s-%s-%.0f", airline, time, price)
	}
	
	for _, f := range ocrFlights {
		flightMap[genKey(f)] = f
	}
	for _, f := range minerFlights {
		// Miner usually has better attributes but might miss time. 
		// If it's a "close enough" match, we could merge, but for now just add.
		flightMap[genKey(f)] = f 
	}

	var flights []FlightInfo
	for _, f := range flightMap {
		flights = append(flights, f)
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

// SearchFlightsNative remains as a local fallback for testing without the service
func SearchFlightsNative(opts SearchOptions) ([]FlightInfo, string, error) {
	pw, err := playwright.Run()
	if err != nil { return nil, "", fmt.Errorf("could not start playwright: %v", err) }
	defer pw.Stop()

	executablePath := opts.BrowserPath
	if executablePath == "" { executablePath = "/usr/bin/chromium" }

	browser, err := pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
		Headless: playwright.Bool(opts.Headless),
		ExecutablePath: playwright.String(executablePath),
		Args: []string{
			"--no-sandbox", "--disable-setuid-sandbox", "--disable-dev-shm-usage",
			"--window-size=1600,1200", "--disable-blink-features=AutomationControlled",
		},
	})
	if err != nil { return nil, "", fmt.Errorf("could not launch browser: %v", err) }
	defer browser.Close()

	context, err := browser.NewContext(playwright.BrowserNewContextOptions{
		Viewport: &playwright.Size{Width: 1600, Height: 1200},
		UserAgent: playwright.String("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36"),
	})
	if err != nil { return nil, "", fmt.Errorf("could not create context: %v", err) }
	defer context.Close()

	page, err := context.NewPage()
	if err != nil { return nil, "", fmt.Errorf("could not create page: %v", err) }

	cabinQuery := ""
	cabinLower := strings.ToLower(opts.CabinClass)
	if strings.Contains(cabinLower, "business") {
		cabinQuery = "+business+class"
	} else if strings.Contains(cabinLower, "premium") {
		cabinQuery = "+premium+economy"
	} else if strings.Contains(cabinLower, "first") {
		cabinQuery = "+first+class"
	}

	searchURL := fmt.Sprintf("https://www.google.com/travel/flights?q=flights+from+%s+to+%s+on+%s+return+%s%s&hl=%s&gl=%s&curr=%s", opts.Departure, opts.Destination, opts.StartDate, opts.EndDate, cabinQuery, opts.Language, opts.Region, opts.Currency)
	log.Printf("Navigating to: %s", searchURL)
	if _, err = page.Goto(searchURL, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateNetworkidle}); err != nil {
		return nil, "", fmt.Errorf("could not navigate: %v", err)
	}

	// Wait and take screenshot
	time.Sleep(15 * time.Second)
	screenshotPath := getScreenshotPath()
	_, _ = page.Screenshot(playwright.PageScreenshotOptions{Path: playwright.String(screenshotPath)})
	html, _ := page.Content()

	flights, err := ExtractFlightsFromImage(screenshotPath)
	if (err != nil || len(flights) == 0) && html != "" {
		flights, err = MinerExtractFlights(html)
	}

	if err != nil {
		return nil, screenshotPath, err
	}
	for i := range flights {
		flights[i].URL = page.URL()
		flights[i].CabinClass = opts.CabinClass
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
	if len(cleaned) > 15000 {
		pos := strings.Index(cleaned, "Top departing options")
		if pos == -1 { pos = strings.Index(cleaned, "Best departing flights") }
		if pos != -1 {
			start := pos - 500
			if start < 0 { start = 0 }
			end := start + 15000
			if end > len(cleaned) { end = len(cleaned) }
			snippet = cleaned[start:end]
		} else {
			snippet = cleaned[:15000]
		}
	}

	prompt := fmt.Sprintf(`### TASK: Extract REAL FLIGHT DATA from the provided text snippet.
### IMPORTANT: 
- RETURN A VALID JSON ARRAY of FLAT OBJECTS ONLY.
- DO NOT use nested objects.
- IF NO FLIGHTS ARE EXPRESSLY LISTED IN THE DATA, RETURN AN EMPTY ARRAY '[]'.
- DO NOT hallucinate flights.
- DO NOT return hardware, products, or unrelated data.
- FIELDS: airline, departure_time, arrival_time, duration, stops, price, departure_airport, arrival_airport

REQUIRED JSON FORMAT EXAMPLE:
[
  {
    "airline": "EasyJet",
    "departure_time": "06:05 AM",
    "arrival_time": "08:30 AM",
    "duration": "1h 25m",
    "stops": "Nonstop",
    "price": "€76",
    "departure_airport": "LTN",
    "arrival_airport": "CDG"
  }
]

DATA SNIPPET TO PROCESS:
---
%s
---
JSON ARRAY:`, snippet)

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
	if idx := strings.Index(cleanResp, "["); idx != -1 {
		cleanResp = cleanResp[idx:]
	}
	if idx := strings.LastIndex(cleanResp, "]"); idx != -1 {
		cleanResp = cleanResp[:idx+1]
	}

	log.Printf("🤖 LLM Miner cleaned JSON: %s", cleanResp)
	
	var rawFlights []interface{}
	if err := json.Unmarshal([]byte(cleanResp), &rawFlights); err != nil {
		return nil, fmt.Errorf("failed to unmarshal raw flights: %v (JSON: %s)", err, cleanResp)
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
				return fmt.Sprintf("%v", v)
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
		// Fallback to searching for the first 3-letter capital word if it looks like a code
		re2 := regexp.MustCompile(`\b([A-Z]{3})\b`)
		if m := re2.FindStringSubmatch(s); len(m) > 0 {
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
