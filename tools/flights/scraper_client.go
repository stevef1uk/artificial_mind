package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/playwright-community/playwright-go"
)

func SearchFlightsWithScraper(scraperURL string, opts SearchOptions) ([]FlightInfo, error) {
	return SearchFlightsNative(opts)
}

func SearchFlightsNative(opts SearchOptions) ([]FlightInfo, error) {
	log.Printf("🚀 NATIVE VERSION 57 STARTING...")
    
	pw, err := playwright.Run()
	if err != nil { return nil, err }
	defer pw.Stop()

	browser, err := pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
		Headless:       playwright.Bool(true),
		ExecutablePath: playwright.String("/usr/bin/chromium"),
		Args: []string{
			"--no-sandbox", "--disable-setuid-sandbox", "--disable-dev-shm-usage",
			"--window-size=1600,1200", "--disable-blink-features=AutomationControlled",
		},
	})
	if err != nil { return nil, err }
	defer browser.Close()

	context, err := browser.NewContext(playwright.BrowserNewContextOptions{
		Viewport: &playwright.Size{Width: 1600, Height: 1200},
		UserAgent: playwright.String("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36"),
	})
	if err != nil { return nil, err }
	defer context.Close()

	page, err := context.NewPage()
	if err != nil { return nil, err }

	searchURL := "https://www.google.com/travel/flights?hl=en-US&gl=US&curr=EUR"
	log.Printf("Navigating to: %s", searchURL)
	if _, err = page.Goto(searchURL, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateNetworkidle}); err != nil {
		return nil, err
	}

	// 1. Consent
	acceptBtn := page.GetByRole("button", playwright.PageGetByRoleOptions{Name: "Accept all"}).First()
	if err := acceptBtn.Click(playwright.LocatorClickOptions{Timeout: playwright.Float(5000)}); err == nil {
		time.Sleep(2 * time.Second)
	}

	// 2. Interaction
	page.Click("input[aria-label='Where from?'], input[placeholder='Where from?']")
	time.Sleep(500 * time.Millisecond)
	page.Keyboard().Press("Control+A")
	page.Keyboard().Press("Backspace")
	page.Keyboard().Type(opts.Departure)
	time.Sleep(1500 * time.Millisecond)
	page.Keyboard().Press("Enter")

	page.Click("input[aria-label='Where to?'], input[placeholder='Where to?']")
	time.Sleep(500 * time.Millisecond)
	page.Keyboard().Press("Control+A")
	page.Keyboard().Press("Backspace")
	page.Keyboard().Type(opts.Destination)
	time.Sleep(1500 * time.Millisecond)
	page.Keyboard().Press("Enter")

	page.Click("input[placeholder='Departure'], input[aria-label='Departure']")
	time.Sleep(1500 * time.Millisecond)
	page.Keyboard().Press("Control+A")
	page.Keyboard().Type(opts.StartDate)
	time.Sleep(1000 * time.Millisecond)
	page.Keyboard().Press("Tab")
	time.Sleep(1000 * time.Millisecond)
	page.Keyboard().Press("Control+A")
	page.Keyboard().Type(opts.EndDate)
	time.Sleep(1000 * time.Millisecond)
	page.Keyboard().Press("Enter")
	time.Sleep(2000 * time.Millisecond)

	page.Keyboard().Press("Enter")
	time.Sleep(2000 * time.Millisecond)
	
	searchBtn := page.Locator("button").Filter(playwright.LocatorFilterOptions{HasText: "Search"}).First()
	if isVisible, _ := searchBtn.IsVisible(); isVisible {
		searchBtn.Click()
	}

	log.Println("Waiting for results...")
	time.Sleep(25 * time.Second)

	screenshotPath := "latest_flight_screenshot.png"
	_, _ = page.Screenshot(playwright.PageScreenshotOptions{Path: playwright.String(screenshotPath)})
	html, _ := page.Content()

	flights, err := ExtractFlightsFromImage(screenshotPath)
	if (err != nil || len(flights) == 0) && html != "" {
		log.Printf("⚠️ Still no results. Attempting HTML Miner fallback...")
		flights, err = MinerExtractFlights(html)
	}

	if err != nil { return nil, err }
	for i := range flights {
		flights[i].URL = page.URL()
		flights[i].CabinClass = opts.CabinClass
	}
	return flights, nil
}

func MinerExtractFlights(data string) ([]FlightInfo, error) {
    // Determine if data is HTML or OCR text
    isHTML := strings.Contains(data, "<html")
    
    snippet := data
    if isHTML {
        // Smart HTML snippet finding dense flight data
        re := regexp.MustCompile(`\["([^"]+)",\["([^"]+)",.+?\d+\][,\]]`)
        matches := re.FindAllString(data, 100)
        if len(matches) > 0 {
            snippet = strings.Join(matches, "\n")
        } else {
            pos := strings.Index(data, "round trip")
            if pos == -1 { pos = len(data) / 2 }
            start, end := pos-50000, pos+250000
            if start < 0 { start = 0 }
            if end > len(data) { end = len(data) }
            snippet = data[start:end]
        }
    } else {
        // OCR text is usually manageable size (< 100KB)
        if len(snippet) > 100000 {
            snippet = snippet[:100000]
        }
    }

	prompt := fmt.Sprintf(`Extract flight results from this %s data.
Return ONLY a JSON list of objects: "airline", "departure_time", "arrival_time", "duration", "stops", "price".

Data:
%s`, func() string { if isHTML { return "HTML" }; return "OCR text" }(), snippet)

	ollamaReq := map[string]interface{}{
		"model": "qwen3:14b", "prompt": prompt, "stream": false, "format": "json",
	}

	jsonReq, _ := json.Marshal(ollamaReq)
	resp, err := http.Post("http://localhost:11434/api/generate", "application/json", bytes.NewBuffer(jsonReq))
	if err != nil { return nil, err }
	defer resp.Body.Close()

	var ollamaResp struct { Response string `json:"response"` }
	json.NewDecoder(resp.Body).Decode(&ollamaResp)

	var flights []struct {
		Airline       string `json:"airline"`
		DepartureTime string `json:"departure_time"`
		ArrivalTime   string `json:"arrival_time"`
		Duration      string `json:"duration"`
		Stops         string `json:"stops"`
		Price         string `json:"price"`
	}

	if err := json.Unmarshal([]byte(ollamaResp.Response), &flights); err != nil {
		var wrapper struct { Flights []struct {
            Airline       string `json:"airline"`
            DepartureTime string `json:"departure_time"`
            ArrivalTime   string `json:"arrival_time"`
            Duration      string `json:"duration"`
            Stops         string `json:"stops"`
            Price         string `json:"price"`
        } `json:"flights"` }
		if err2 := json.Unmarshal([]byte(ollamaResp.Response), &wrapper); err2 == nil {
			flights = wrapper.Flights
		} else {
			return nil, fmt.Errorf("parse fail: %s", ollamaResp.Response)
		}
	}

	var result []FlightInfo
	for _, f := range flights {
		result = append(result, FlightInfo{
			Airline: f.Airline, Price: f.Price, Duration: f.Duration,
			Stops: f.Stops, DepartureTime: f.DepartureTime, ArrivalTime: f.ArrivalTime,
		})
	}
	log.Printf("🚀 Miner found %d flights", len(result))
	return result, nil
}
