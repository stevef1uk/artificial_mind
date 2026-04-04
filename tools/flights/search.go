package main

import (
	"fmt"
	"log"
	"time"

	"github.com/playwright-community/playwright-go"
)

type FlightInfo struct {
	Price         string `json:"price"`
	Airline       string `json:"airline"`
	Duration      string `json:"duration"`
	Stops         string `json:"stops"`
	DepartureTime string `json:"departure_time"`
	ArrivalTime   string `json:"arrival_time"`
	URL           string `json:"url"`
	CabinClass    string `json:"cabin_class"`
}

func SearchFlights(departure, destination, startDate, endDate, cabinClass string) ([]FlightInfo, error) {
	pw, err := playwright.Run()
	if err != nil {
		return nil, fmt.Errorf("could not start playwright: %v", err)
	}
	defer pw.Stop()

	browser, err := pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
		Headless: playwright.Bool(true),
		ExecutablePath: playwright.String("/usr/bin/chromium"),
		Args: []string{
			"--no-sandbox",
			"--disable-setuid-sandbox",
			"--disable-dev-shm-usage",
			"--window-size=1600,1200",
		},
	})
	if err != nil {
		return nil, fmt.Errorf("could not launch browser: %v", err)
	}
	defer browser.Close()

	context, err := browser.NewContext(playwright.BrowserNewContextOptions{
		Viewport: &playwright.Size{
			Width:  1600,
			Height: 1200,
		},
		UserAgent: playwright.String("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36"),
	})
	if err != nil {
		return nil, fmt.Errorf("could not create context: %v", err)
	}
	defer context.Close()

	page, err := context.NewPage()
	if err != nil {
		return nil, fmt.Errorf("could not create page: %v", err)
	}

	// Go directly to the search path to bypass some of the landing page noise
	searchURL := "https://www.google.com/travel/flights?hl=en&gl=FR&curr=EUR"
	log.Printf("Navigating to: %s", searchURL)

	if _, err = page.Goto(searchURL, playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateNetworkidle,
	}); err != nil {
		return nil, fmt.Errorf("could not navigate to URL: %v", err)
	}

	// Handle cookie consent
	log.Println("Checking for cookie consent...")
	if err := page.GetByRole("button", playwright.PageGetByRoleOptions{
		Name: "Accept all",
	}).First().Click(playwright.LocatorClickOptions{
		Timeout: playwright.Float(5000),
	}); err == nil {
		log.Println("Clicked 'Accept all' button")
		time.Sleep(2 * time.Second)
	}

	// 1. Set Cabin Class first (it's often the 3rd dropdown)
	if cabinClass != "Economy" && cabinClass != "" {
		log.Printf("Setting cabin class: %s", cabinClass)
		// Find the dropdown that says "Economy"
		cabinDropdown := page.Locator("div[aria-haspopup='listbox']").Filter(playwright.LocatorFilterOptions{
			HasText: "Economy",
		}).First()
		if err := cabinDropdown.Click(); err == nil {
			time.Sleep(1000 * time.Millisecond)
			page.GetByRole("option", playwright.PageGetByRoleOptions{
				Name: cabinClass,
			}).First().Click()
			time.Sleep(1000 * time.Millisecond)
		}
	}

	// 2. Set Departure
	log.Printf("Entering departure: %s", departure)
	departureInput := page.Locator("input[aria-label='Where from?'], input[placeholder='Where from?']").First()
	departureInput.Click()
	time.Sleep(500 * time.Millisecond)
	// Clear and type
	page.Keyboard().Press("Control+A")
	page.Keyboard().Press("Backspace")
	page.Keyboard().Type(departure)
	time.Sleep(1500 * time.Millisecond)
	page.Keyboard().Press("Enter")
	time.Sleep(1000 * time.Millisecond)

	// 3. Set Destination
	log.Printf("Entering destination: %s", destination)
	destinationInput := page.Locator("input[aria-label='Where to?'], input[placeholder='Where to?']").First()
	destinationInput.Click()
	time.Sleep(500 * time.Millisecond)
	// Clear and type
	page.Keyboard().Press("Control+A")
	page.Keyboard().Press("Backspace")
	page.Keyboard().Type(destination)
	time.Sleep(1500 * time.Millisecond)
	page.Keyboard().Press("Enter")
	time.Sleep(1000 * time.Millisecond)

	// 4. Set Dates
	log.Printf("Entering dates: %s to %s", startDate, endDate)
	
	// Click the departure input to open the picker
	page.Locator("input[placeholder='Departure'], input[aria-label='Departure']").First().Click()
	time.Sleep(1500 * time.Millisecond)

	// In the overlay, the first input should have focus.
	// We'll type start date, Tab, type end date, Enter.
	log.Println("Typing dates sequence...")
	page.Keyboard().Press("Control+A")
	page.Keyboard().Press("Backspace")
	page.Keyboard().Type(startDate)
	time.Sleep(1000 * time.Millisecond)
	
	page.Keyboard().Press("Tab")
	time.Sleep(500 * time.Millisecond)
	
	// The Tab should move us to the Return field.
	page.Keyboard().Press("Control+A")
	page.Keyboard().Press("Backspace")
	page.Keyboard().Type(endDate)
	time.Sleep(1000 * time.Millisecond)
	page.Keyboard().Press("Enter")
	time.Sleep(1000 * time.Millisecond)

	// Click "Done"
	log.Println("Clicking Done...")
	doneBtn := page.GetByRole("button", playwright.PageGetByRoleOptions{
		Name: "Done",
	}).First()
	
	if isVisible, _ := doneBtn.IsVisible(); isVisible {
		doneBtn.Click()
	} else {
		log.Println("Done button not found, pressing Enter...")
		page.Keyboard().Press("Enter")
	}
	time.Sleep(2000 * time.Millisecond)

	// 5. Final Search Trigger
	log.Println("Triggering search...")
	page.Keyboard().Press("Enter")
	time.Sleep(2000 * time.Millisecond)

	// Sometimes we need to click the search button explicitly if it's visible
	searchBtn := page.Locator("button").Filter(playwright.LocatorFilterOptions{
		HasText: "Search",
	}).First()
	
	if isVisible, _ := searchBtn.IsVisible(); isVisible {
		log.Println("Clicking Search button...")
		searchBtn.Click()
	}

	// Wait for results to load - look for price elements or flight rows
	log.Println("Waiting for results to load...")
	
	// Wait up to 30 seconds for the URL to change or results to appear
	for i := 0; i < 30; i++ {
		time.Sleep(1 * time.Second)
		// If we see "Best departing flights" or similar, we are likely there
		content, _ := page.Content()
		if (len(content) > 10000) && (i > 10) { // arbitrary size check
			break
		}
	}

	// Extra wait for animations
	time.Sleep(5 * time.Second)

	// Take screenshot for OCR
	screenshotPath := fmt.Sprintf("screenshot_%s.png", time.Now().Format("20060102_150405"))
	if _, err := page.Screenshot(playwright.PageScreenshotOptions{
		Path: playwright.String(screenshotPath),
		FullPage: playwright.Bool(true),
	}); err != nil {
		return nil, fmt.Errorf("could not take screenshot: %v", err)
	}

	log.Printf("Screenshot saved to: %s", screenshotPath)

	// Here we call OCR to extract data
	flights, err := ExtractFlightsFromImage(screenshotPath)
	if err != nil {
		return nil, fmt.Errorf("could not extract flights from image: %v", err)
	}

	for i := range flights {
		flights[i].URL = page.URL()
		flights[i].CabinClass = cabinClass
	}

	return flights, nil
}
