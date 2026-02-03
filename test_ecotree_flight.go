package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/playwright-community/playwright-go"
)

// Result holds the CO2 calculation results
type Result struct {
	Success   bool              `json:"success"`
	FromCity  string            `json:"from_city"`
	ToCity    string            `json:"to_city"`
	Title     string            `json:"title,omitempty"`
	URL       string            `json:"url,omitempty"`
	CO2Data   map[string]interface{} `json:"co2_data,omitempty"`
	Error     string            `json:"error,omitempty"`
}

func main() {
	// Parse command line flags
	fromCity := flag.String("from", "southampton", "Departure city")
	toCity := flag.String("to", "newcastle", "Destination city")
	headless := flag.Bool("headless", true, "Run browser in headless mode")
	screenshot := flag.String("screenshot", "/tmp/ecotree_results_go.png", "Screenshot output path")
	flag.Parse()

	fmt.Println("============================================================")
	fmt.Println("🌱 EcoTree Flight CO2 Calculator Test (Go)")
	fmt.Println("============================================================")
	fmt.Printf("\n📍 Route: %s → %s\n\n", strings.ToUpper(*fromCity), strings.ToUpper(*toCity))

	// Run the calculation
	result := calculateFlightCO2(*fromCity, *toCity, *headless, *screenshot)

	// Print final result
	fmt.Println("\n============================================================")
	fmt.Println("📊 Final Result:")
	jsonResult, _ := json.MarshalIndent(result, "", "  ")
	fmt.Println(string(jsonResult))
	fmt.Println("============================================================")

	if !result.Success {
		os.Exit(1)
	}
}

func calculateFlightCO2(fromCity, toCity string, headless bool, screenshotPath string) Result {
	fmt.Printf("🌍 Calculating CO2 emissions for flight: %s → %s\n", fromCity, toCity)

	// Initialize Playwright
	fmt.Println("🚀 Launching Playwright...")
	pw, err := playwright.Run()
	if err != nil {
		return Result{
			Success:  false,
			FromCity: fromCity,
			ToCity:   toCity,
			Error:    fmt.Sprintf("Failed to start Playwright: %v", err),
		}
	}
	defer func() {
		// Note: pw.Stop() can hang, so we'll just exit
		// In production, you might want to handle this differently
	}()

	// Launch browser
	fmt.Println("🌐 Launching Chromium browser...")
	browser, err := pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
		Headless: playwright.Bool(headless),
	})
	if err != nil {
		return Result{
			Success:  false,
			FromCity: fromCity,
			ToCity:   toCity,
			Error:    fmt.Sprintf("Failed to launch browser: %v", err),
		}
	}
	defer browser.Close()

	// Create new page
	page, err := browser.NewPage()
	if err != nil {
		return Result{
			Success:  false,
			FromCity: fromCity,
			ToCity:   toCity,
			Error:    fmt.Sprintf("Failed to create page: %v", err),
		}
	}
	defer page.Close()

	// Set timeout
	page.SetDefaultTimeout(30000) // 30 seconds

	// Step 1: Navigate to the calculator page
	fmt.Println("📍 Navigating to EcoTree calculator...")
	_, err = page.Goto("https://ecotree.green/en/calculate-flight-co2", playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateNetworkidle,
	})
	if err != nil {
		return Result{
			Success:  false,
			FromCity: fromCity,
			ToCity:   toCity,
			Error:    fmt.Sprintf("Failed to navigate: %v", err),
		}
	}
	fmt.Printf("✅ Page loaded: %s\n", page.URL())

	// Step 2: Click on "Plane" tab (might already be selected)
	fmt.Println("✈️  Selecting 'Plane' tab...")
	planeLink := page.GetByRole("link", playwright.PageGetByRoleOptions{Name: "Plane"})
	if err := planeLink.Click(); err != nil {
		log.Printf("   ℹ️  Plane tab click skipped (might be default): %v", err)
	}
	time.Sleep(1 * time.Second) // Wait for any animations

	// Step 3: Fill in the "From" field
	fmt.Printf("📝 Entering departure city: %s\n", fromCity)
	fromTextbox := page.GetByRole("textbox", playwright.PageGetByRoleOptions{Name: "From To Via"})
	if err := fromTextbox.Click(); err != nil {
		return Result{
			Success:  false,
			FromCity: fromCity,
			ToCity:   toCity,
			Error:    fmt.Sprintf("Failed to click 'From' textbox: %v", err),
		}
	}
	if err := fromTextbox.Fill(fromCity); err != nil {
		return Result{
			Success:  false,
			FromCity: fromCity,
			ToCity:   toCity,
			Error:    fmt.Sprintf("Failed to fill 'From' field: %v", err),
		}
	}
	time.Sleep(1 * time.Second) // Wait for autocomplete

	// Step 4: Select from autocomplete dropdown
	fmt.Printf("🔍 Selecting autocomplete suggestion for %s...\n", fromCity)
	// Wait for autocomplete to appear and click the first matching option
	capitalizedFrom := strings.Title(fromCity)
	fromSelector := fmt.Sprintf("text=/%s/i", capitalizedFrom)
	if err := page.Locator(fromSelector).First().Click(playwright.LocatorClickOptions{
		Timeout: playwright.Float(5000),
	}); err != nil {
		log.Printf("   ⚠️  Autocomplete selection issue: %v", err)
		// Try pressing Enter as fallback
		fromTextbox.Press("Enter")
	} else {
		fmt.Println("   ✅ Selected departure city")
	}
	time.Sleep(1 * time.Second)

	// Step 5: Fill in the "To" field
	fmt.Printf("📝 Entering destination city: %s\n", toCity)
	toInput := page.Locator("input[name=\"To\"]")
	if err := toInput.Click(); err != nil {
		return Result{
			Success:  false,
			FromCity: fromCity,
			ToCity:   toCity,
			Error:    fmt.Sprintf("Failed to click 'To' field: %v", err),
		}
	}
	if err := toInput.Fill(toCity); err != nil {
		return Result{
			Success:  false,
			FromCity: fromCity,
			ToCity:   toCity,
			Error:    fmt.Sprintf("Failed to fill 'To' field: %v", err),
		}
	}
	time.Sleep(1 * time.Second) // Wait for autocomplete

	// Step 6: Select from autocomplete dropdown
	fmt.Printf("🔍 Selecting autocomplete suggestion for %s...\n", toCity)
	capitalizedTo := strings.Title(toCity)
	toSelector := fmt.Sprintf("text=/%s/i", capitalizedTo)
	if err := page.Locator(toSelector).First().Click(playwright.LocatorClickOptions{
		Timeout: playwright.Float(5000),
	}); err != nil {
		log.Printf("   ⚠️  Autocomplete selection issue: %v", err)
		// Try pressing Enter as fallback
		toInput.Press("Enter")
	} else {
		fmt.Println("   ✅ Selected destination city")
	}
	time.Sleep(1 * time.Second)

	// Step 7: Click the "Calculate my emissions" button
	fmt.Println("🧮 Clicking calculate button...")
	calculateButton := page.GetByRole("link", playwright.PageGetByRoleOptions{
		Name: "Calculate my emissions",
	})
	if err := calculateButton.Click(); err != nil {
		return Result{
			Success:  false,
			FromCity: fromCity,
			ToCity:   toCity,
			Error:    fmt.Sprintf("Failed to click calculate button: %v", err),
		}
	}

	// Wait for results to load
	fmt.Println("⏳ Waiting for results...")
	if err := page.WaitForLoadState(playwright.PageWaitForLoadStateOptions{
		State: playwright.LoadStateNetworkidle,
	}); err != nil {
		log.Printf("   ⚠️  Wait for load state error: %v", err)
	}
	time.Sleep(2 * time.Second) // Extra wait for animations

	// Step 8: Extract the results
	fmt.Println("📊 Extracting CO2 emissions data...")

	// Take a screenshot
	if _, err := page.Screenshot(playwright.PageScreenshotOptions{
		Path: playwright.String(screenshotPath),
	}); err != nil {
		log.Printf("   ⚠️  Failed to save screenshot: %v", err)
	} else {
		fmt.Printf("📸 Screenshot saved to %s\n", screenshotPath)
	}

	// Extract page text content
	textContent, err := page.Evaluate("() => document.body.innerText")
	if err != nil {
		return Result{
			Success:  false,
			FromCity: fromCity,
			ToCity:   toCity,
			Error:    fmt.Sprintf("Failed to extract page text: %v", err),
		}
	}

	textStr, ok := textContent.(string)
	if !ok {
		return Result{
			Success:  false,
			FromCity: fromCity,
			ToCity:   toCity,
			Error:    "Failed to convert page text to string",
		}
	}

	// Parse CO2 values using regex
	co2Data := make(map[string]interface{})
	
	// Look for CO2 emission patterns
	co2Regex := regexp.MustCompile(`(?i)([\d,.]+)\s*(kg|tons?|tonnes?)\s*(?:of\s+)?CO2`)
	matches := co2Regex.FindAllStringSubmatch(textStr, -1)
	
	if len(matches) > 0 {
		var emissions [][]string
		for _, match := range matches {
			if len(match) >= 3 {
				emissions = append(emissions, []string{match[1], match[2]})
			}
		}
		co2Data["co2_emissions"] = emissions
		fmt.Printf("   ✅ Found CO2 values: %v\n", emissions)
	}

	// Look for specific values in the results
	// Distance pattern
	distanceRegex := regexp.MustCompile(`(?i)Kilometers\s*([\d,]+)\s*km`)
	if distMatch := distanceRegex.FindStringSubmatch(textStr); len(distMatch) >= 2 {
		co2Data["distance_km"] = distMatch[1]
	}

	// CO2 emissions pattern (specific to results section)
	emissionsRegex := regexp.MustCompile(`(?i)CO2\s*([\d,]+)\s*kg`)
	if emMatch := emissionsRegex.FindStringSubmatch(textStr); len(emMatch) >= 2 {
		co2Data["co2_kg"] = emMatch[1]
		fmt.Printf("   ✅ Your carbon emissions: %s kg CO2\n", emMatch[1])
	}

	// Store first 1000 chars of page text for debugging
	if len(textStr) > 1000 {
		co2Data["page_text"] = textStr[:1000]
	} else {
		co2Data["page_text"] = textStr
	}

	// Get page title and URL
	title, _ := page.Title()
	finalURL := page.URL()

	return Result{
		Success:  true,
		FromCity: fromCity,
		ToCity:   toCity,
		Title:    title,
		URL:      finalURL,
		CO2Data:  co2Data,
	}
}

