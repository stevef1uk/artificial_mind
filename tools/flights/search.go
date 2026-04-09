package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/playwright-community/playwright-go"
)

// SearchFlights is the entry point for all flight searches
func SearchFlights(opts SearchOptions) ([]FlightInfo, error) {
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
func SearchFlightsWithScraper(scraperURL string, opts SearchOptions) ([]FlightInfo, error) {
	queryText := fmt.Sprintf("flights from %s to %s on %s return %s", opts.Departure, opts.Destination, opts.StartDate, opts.EndDate)
	searchURL := fmt.Sprintf("https://www.google.com/travel/flights?q=%s&hl=%s&gl=%s&curr=%s", strings.ReplaceAll(queryText, " ", "+"), opts.Language, opts.Region, opts.Currency)
	rootURL := fmt.Sprintf("https://www.google.com/travel/flights?hl=%s&gl=%s&curr=%s", opts.Language, opts.Region, opts.Currency)

	tsConfig := fmt.Sprintf(`
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
		await page.goto("%s");
		await page.waitForTimeout(5000);
		
		// RECOVERY: If we are on a generic search page or home page, move to interaction
		// Note: page.url() and if blocks are NOT supported by the simplified engine parser,
		// but they are ignored (as warnings) so we keep them for documentation in the source.
		// The engine just ignores non-matching lines.

		// Final check for results
		console.log("Waiting for results table...");
		await page.waitForSelector("div[role='listitem'], li.pI9Vpc", { timeout: 30000 });
		
		// 3. Scroll and Expand to ensure all results load
		console.log("Stage 3: Deep scrolling and expanding results...");
		await page.evaluate(async () => {
			for (let i = 0; i < 5; i++) {
				window.scrollBy(0, 1000);
				await new Promise(r => setTimeout(r, 800));
			}
			// Attempt to click expansion buttons for 'Other flights'
			const btn = document.querySelector("button:has-text('More flights'), button:has-text('Other departing flights'), [aria-label*='Show more']");
			if (btn) btn.click();
			
			window.scrollBy(0, 2000);
			await new Promise(r => setTimeout(r, 1000));
			window.scrollTo(0, 0);
		});
		await page.waitForTimeout(2000);
	`, rootURL, searchURL)

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
		return nil, fmt.Errorf("request to scraper failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("scraper returned error status: %d", resp.StatusCode)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode scraper response: %v", err)
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
	for _, f := range ocrFlights {
		key := fmt.Sprintf("%s-%s", f.Airline, f.DepartureTime)
		flightMap[key] = f
	}
	for _, f := range minerFlights {
		key := fmt.Sprintf("%s-%s", f.Airline, f.DepartureTime)
		// Prefer Miner attributes if already present, as it's usually more accurate
		flightMap[key] = f 
	}

	var flights []FlightInfo
	for _, f := range flightMap {
		flights = append(flights, f)
	}

	log.Printf("📊 Combined Results: %d (OCR: %d, Miner: %d)", len(flights), len(ocrFlights), len(minerFlights))

	if err != nil {
		return nil, err
	}

	for i := range flights {
		flights[i].CabinClass = opts.CabinClass
	}

	return flights, nil
}

// SearchFlightsNative remains as a local fallback for testing without the service
func SearchFlightsNative(opts SearchOptions) ([]FlightInfo, error) {
	log.Printf("Starting NATIVE Version 60 flights search...")

	pw, err := playwright.Run()
	if err != nil { return nil, fmt.Errorf("could not start playwright: %v", err) }
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
	if err != nil { return nil, fmt.Errorf("could not launch browser: %v", err) }
	defer browser.Close()

	context, err := browser.NewContext(playwright.BrowserNewContextOptions{
		Viewport: &playwright.Size{Width: 1600, Height: 1200},
		UserAgent: playwright.String("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36"),
	})
	if err != nil { return nil, fmt.Errorf("could not create context: %v", err) }
	defer context.Close()

	page, err := context.NewPage()
	if err != nil { return nil, fmt.Errorf("could not create page: %v", err) }

	searchURL := fmt.Sprintf("https://www.google.com/travel/flights?q=flights+from+%s+to+%s+on+%s+return+%s&hl=en-US&gl=US&curr=EUR", opts.Departure, opts.Destination, opts.StartDate, opts.EndDate)
	log.Printf("Navigating to: %s", searchURL)
	if _, err = page.Goto(searchURL, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateNetworkidle}); err != nil {
		return nil, fmt.Errorf("could not navigate: %v", err)
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

	if err != nil { return nil, err}
	for i := range flights {
		flights[i].URL = page.URL()
		flights[i].CabinClass = opts.CabinClass
	}
	return flights, nil
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

	prompt := fmt.Sprintf(`Task: Extract ALL flight options from the Google Flights HTML/text below. 
Return a JSON array of objects with these fields ONLY:
- airline, departure_time, arrival_time, duration, stops, price

Data snippet to process:
---
%s
---
JSON array:`, snippet)

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

	var flights []FlightInfo
	if err := json.Unmarshal([]byte(cleanResp), &flights); err != nil {
		return nil, err
	}

	return flights, nil
}
