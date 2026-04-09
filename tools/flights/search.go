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
	tsConfig := fmt.Sprintf(`
		await page.setViewportSize({ width: 1920, height: 2500 });
		await page.setUserAgent("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36");
		
		// 1. Initial load to clear consent and set locale
		console.log("Stage 1: Clearing consent on Travel Flights...");
		await page.goto("https://www.google.com/travel/flights?hl=%s&gl=%s&curr=%s");
		try { await page.waitForLoadState("networkidle", { timeout: 10000 }); } catch(e) {}
		await page.bypassConsent();
		await page.waitForTimeout(2000); 

		// 2. Perform the search - use the MOST stable entry point
		console.log("Stage 2: Performing search via direct URL...");
		const queryText = "flights from %s to %s on %s return %s";
		const searchURL = "https://www.google.com/travel/flights?q=" + encodeURIComponent(queryText) + "&hl=%s&gl=%s&curr=%s";
		console.log("Navigating to: " + searchURL);
		await page.goto(searchURL);
		await page.waitForTimeout(5000);
		console.log("Current URL after landing: " + page.url());
		
		// RECOVERY: If we are on a generic search page or home page, move to interaction
		if (!page.url().includes("/travel/flights")) {
			console.log("⚠️ Not on Flights page. Current URL: " + page.url() + ". Attempting recovery...");
			
			// Try to find the Flights button in the search result widget
			const flightsButton = page.locator("text='Show flights', text='View flights', [aria-label='Flights']").first();
			if (await flightsButton.isVisible()) {
				console.log("Found 'Flights' button/widget. Clicking...");
				await flightsButton.click();
				await page.waitForTimeout(8000);
			} else {
				console.log("No widget found. Navigating to ROOT flights and typing query...");
				await page.goto("https://www.google.com/travel/flights?hl=%s&gl=%s&curr=%s");
				await page.waitForTimeout(3000);
			}
		}

		// Final check for results
		console.log("Waiting for results table...");
		const resultsSelector = "div[role='listitem'], li.pI9Vpc, .Tf99Ab, [data-result-id], .KC1CH";
		try {
			await page.waitForSelector(resultsSelector, { timeout: 30000 });
			console.log("✅ Results table detected. URL: " + page.url());
		} catch (e) {
			console.log("⚠️ Results table not found. Attempting a final scroll.");
			await page.evaluate(() => { window.scrollBy(0, 800); });
			await page.waitForTimeout(4000);
		}
		
		// 3. Scroll to ensure all lazy elements load
		console.log("Stage 3: Scrolling for lazy-loading...");
		for (let i = 0; i < 4; i++) {
			await page.evaluate(() => { window.scrollBy(0, 1000); });
			await page.waitForTimeout(2000);
		}
		await page.evaluate(() => { window.scrollTo(0, 0); });
		await page.waitForTimeout(2000);
	`, opts.Language, opts.Region, opts.Currency, // Stage 1
		opts.Departure, opts.Destination, opts.StartDate, opts.EndDate, // Stage 2 Query
		opts.Language, opts.Region, opts.Currency, // Stage 2 Params
		opts.Language, opts.Region, opts.Currency) // Recovery Params

	screenshotPath := getScreenshotPath()
	body, _ := json.Marshal(map[string]interface{}{
		"url":               fmt.Sprintf("https://www.google.com/travel/flights?hl=%s&gl=%s&curr=%s", opts.Language, opts.Region, opts.Currency),
		"typescript_config": tsConfig,
		"get_html":          true,
		"screenshot_path":   screenshotPath,
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

	// Extract flights using OCR on the shared screenshot
	flights, err := ExtractFlightsFromImage(screenshotPath)
	
	// Fallback to HTML miner if OCR fails
	if (err != nil || len(flights) == 0) {
		if html, ok := result["html"].(string); ok && html != "" {
			log.Printf("⚠️ OCR produced 0 results. Attempting SMART Miner fallback on HTML...")
			flights, err = MinerExtractFlights(html)
		}
	}

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
	reScripts := regexp.MustCompile(`(?i)<script[\s\S]*?</script>`)
	cleaned := reScripts.ReplaceAllString(data, "")
	reStyles := regexp.MustCompile(`(?i)<style[\s\S]*?</style>`)
	cleaned = reStyles.ReplaceAllString(cleaned, "")

	snippet := cleaned
	if len(cleaned) > 40000 {
		pos := strings.Index(cleaned, "Top departing options")
		if pos == -1 { pos = strings.Index(cleaned, "Best departing flights") }
		if pos != -1 {
			start := pos - 1000
			if start < 0 { start = 0 }
			end := start + 40000
			if end > len(cleaned) { end = len(cleaned) }
			snippet = cleaned[start:end]
		} else {
			snippet = cleaned[:40000]
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

	log.Printf("🤖 Calling LLM Miner with model %s...", model)

	ollamaReq := map[string]interface{}{
		"model":  model,
		"prompt": prompt,
		"stream": false,
	}
	jsonReq, _ := json.Marshal(ollamaReq)

	req, _ := http.NewRequest("POST", getOllamaURL()+"/api/generate", bytes.NewBuffer(jsonReq))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil { return nil, err }
	defer resp.Body.Close()

	var ollamaResp struct {
		Response string `json:"response"`
	}
	json.NewDecoder(resp.Body).Decode(&ollamaResp)

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
