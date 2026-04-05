package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

type ScrapeRequest struct {
	URL              string            `json:"url"`
	Instructions     string            `json:"instructions"`
	TypeScriptConfig string            `json:"typescript_config"`
	GetHTML          bool              `json:"get_html"`
}

type ScrapeJobResponse struct {
	JobID string `json:"job_id"`
	Status string `json:"status"`
}

type ScrapeResult struct {
	Status        string `json:"status"`
	ScreenshotB64 string `json:"screenshot"`
	Error         string `json:"error"`
}

func SearchFlightsWithScraper(scraperURL string, opts SearchOptions) ([]FlightInfo, error) {
	log.Printf("Using scraper service at: %s", scraperURL)

	// Set defaults
	if opts.Language == "" {
		opts.Language = "en"
	}
	if opts.Region == "" {
		opts.Region = "FR"
	}
	if opts.Currency == "" {
		opts.Currency = "EUR"
	}

	searchURL := fmt.Sprintf("https://www.google.com/travel/flights?hl=%s&gl=%s&curr=%s", opts.Language, opts.Region, opts.Currency)
	log.Printf("Construction Search URL: %s", searchURL)

	// Build the script - using the robust version from K8s manifest
	defaultScript := fmt.Sprintf(`
		await page.goto("%s");
		await page.waitForTimeout(5000);
		try { await page.bypassConsent(); } catch(e) {}
		await page.waitForTimeout(2000);
		
		// 1. Departure
		const fromLoc = page.locator("input[placeholder*='Where from'], input[placeholder*='D\\'où'], input[aria-label*='Where from'], input[value*='Current']").first();
		await fromLoc.click();
		await page.waitForTimeout(500);
		await fromLoc.fill("%%s");
		await page.waitForTimeout(1500);
		await page.keyboard.press("Enter");
		await page.waitForTimeout(1000);
		
		// 2. Destination
		const toLoc = page.locator("input[placeholder*='Where to'], input[placeholder*='Où allez-vous'], input[aria-label*='Where to']").first();
		await toLoc.click();
		await page.waitForTimeout(500);
		await toLoc.fill("%%s");
		await page.waitForTimeout(1500);
		await page.keyboard.press("Enter");
		await page.waitForTimeout(1000);
		
		// 3. Dates
		const dateLoc = page.locator("input[placeholder*='Departure'], input[placeholder*='Départ'], input[aria-label*='Departure']").first();
		await dateLoc.click();
		await page.waitForTimeout(2000);
		await page.keyboard.press("Control+A");
		await page.keyboard.press("Backspace");
		await page.keyboard.type("%%s");
		await page.waitForTimeout(1000);
		await page.keyboard.press("Tab");
		await page.keyboard.press("Control+A");
		await page.keyboard.press("Backspace");
		await page.keyboard.type("%%s");
		await page.waitForTimeout(1000);
		await page.keyboard.press("Enter");
		await page.waitForTimeout(2000);
		
		// Close any overlays
		await page.keyboard.press("Escape");
		await page.waitForTimeout(1000);
		
		// 4. Search
		const searchBtn = page.locator("button:has-text('Search'), button:has-text('Rechercher'), button[aria-label*='Search']").first();
		if (await searchBtn.isVisible()) {
			await searchBtn.click();
		} else {
			await page.keyboard.press("Enter");
		}
		
		// Wait for results to actually render
		try {
			await page.waitForSelector("div[role='listitem'], li.pI9Vpc", { timeout: 30000 });
		} catch (e) {
			await page.mouse.wheel(0, 800);
			await page.waitForTimeout(5000);
		}
		
		await page.waitForTimeout(10000); // Final buffer for animations
	`, searchURL)

	script := os.Getenv("FLIGHT_SCRAPER_SCRIPT")
	if script == "" {
		script = fmt.Sprintf(defaultScript, opts.Departure, opts.Destination, opts.StartDate, opts.EndDate)
	} else {
		// Replace placeholders in provided script too
		script = fmt.Sprintf(script, opts.Departure, opts.Destination, opts.StartDate, opts.EndDate)
	}

	reqBody := ScrapeRequest{
		URL:              "https://www.google.com/travel/flights",
		Instructions:     "Search for flights",
		TypeScriptConfig: script,
		GetHTML:          false,
	}

	jsonBody, _ := json.Marshal(reqBody)
	resp, err := http.Post(scraperURL+"/scrape/start", "application/json", bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to start scrape job: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("scraper returned error status %d: %s", resp.StatusCode, string(body))
	}

	var jobResp ScrapeJobResponse
	if err := json.NewDecoder(resp.Body).Decode(&jobResp); err != nil {
		return nil, fmt.Errorf("failed to decode job response: %v", err)
	}

	log.Printf("Scrape job started: %s", jobResp.JobID)

	// Poll for completion
	maxAttempts := 60
	for i := 0; i < maxAttempts; i++ {
		time.Sleep(2 * time.Second)
		
		jobURL := fmt.Sprintf("%s/scrape/job?job_id=%s", scraperURL, jobResp.JobID)
		resp, err := http.Get(jobURL)
		if err != nil {
			log.Printf("Warning: failed to poll job status: %v", err)
			continue
		}
		
		var jobInfo struct {
			Status string       `json:"status"`
			Result ScrapeResult `json:"result"`
			Error  string       `json:"error"`
		}
		
		err = json.NewDecoder(resp.Body).Decode(&jobInfo)
		resp.Body.Close()
		if err != nil {
			log.Printf("Warning: failed to decode job status: %v", err)
			continue
		}

		if jobInfo.Status == "completed" {
			log.Println("Scrape job completed!")
			return processScraperResult(jobInfo.Result, opts)
		}
		
		if jobInfo.Status == "failed" {
			return nil, fmt.Errorf("scrape job failed: %s", jobInfo.Error)
		}
		
		log.Printf("Job status: %s (attempt %d/%d)", jobInfo.Status, i+1, maxAttempts)
	}

	return nil, fmt.Errorf("scrape job timed out after %d seconds", maxAttempts*2)
}

func processScraperResult(result ScrapeResult, opts SearchOptions) ([]FlightInfo, error) {
	if result.ScreenshotB64 == "" {
		return nil, fmt.Errorf("no screenshot in scraper result")
	}

	// Decode Base64 screenshot
	data, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(result.ScreenshotB64, "data:image/png;base64,"))
	if err != nil {
		return nil, fmt.Errorf("failed to decode screenshot: %v", err)
	}

	// Save to temporary file for OCR
	tmpFile := fmt.Sprintf("scraper_screenshot_%s.png", time.Now().Format("20060102_150405"))
	if err := os.WriteFile(tmpFile, data, 0644); err != nil {
		return nil, fmt.Errorf("failed to save temporary screenshot: %v", err)
	}
	defer os.Remove(tmpFile)

	// Run OCR
	flights, err := ExtractFlightsFromImage(tmpFile)
	if err != nil {
		return nil, err
	}

	for i := range flights {
		flights[i].CabinClass = opts.CabinClass
	}

	return flights, nil
}
