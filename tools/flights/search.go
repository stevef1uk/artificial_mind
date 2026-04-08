package main

import (
	"fmt"
	"log"
	"time"

	"github.com/playwright-community/playwright-go"
)

func SearchFlights(opts SearchOptions) ([]FlightInfo, error) {
	// Prefer Native search as it's currently more stable for styled results
	log.Printf("🏠 Using NATIVE search logic (Version 58)...")
	return SearchFlightsNative(opts)
}

func SearchFlightsNative(opts SearchOptions) ([]FlightInfo, error) {
	log.Printf("Starting NATIVE Version 57 flights search...")

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

	searchURL := fmt.Sprintf("https://www.google.com/travel/flights?q=%s+flights+from+%s+to+%s+on+%s+return+%s&hl=en-US&gl=US&curr=EUR", opts.CabinClass, opts.Departure, opts.Destination, opts.StartDate, opts.EndDate)
	log.Printf("Navigating to: %s", searchURL)
	if _, err = page.Goto(searchURL, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateNetworkidle}); err != nil {
		return nil, fmt.Errorf("could not navigate: %v", err)
	}

	// 1. Consent
	acceptBtn := page.GetByRole("button", playwright.PageGetByRoleOptions{Name: "Accept all"}).First()
	if err := acceptBtn.Click(playwright.LocatorClickOptions{Timeout: playwright.Float(5000)}); err == nil {
		time.Sleep(2 * time.Second)
	}

	log.Println("Waiting for results...")
	time.Sleep(25 * time.Second)

	screenshotPath := "latest_flight_screenshot.png"
	_, _ = page.Screenshot(playwright.PageScreenshotOptions{Path: playwright.String(screenshotPath)})
	html, _ := page.Content()

	flights, err := ExtractFlightsFromImage(screenshotPath)
	if (err != nil || len(flights) == 0) && html != "" {
		log.Printf("⚠️ OCR produced %d results. Attempting SMART Miner fallback...", len(flights))
		flights, err = MinerExtractFlights(html)
	}

	if err != nil { return nil, err}
	for i := range flights {
		flights[i].URL = page.URL()
		flights[i].CabinClass = opts.CabinClass
	}
	return flights, nil
}
