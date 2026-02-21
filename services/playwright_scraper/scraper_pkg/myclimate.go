// Package scraper_pkg provides self-driving web scraping capabilities using Playwright
package scraper_pkg

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	pw "github.com/playwright-community/playwright-go"
)

// FlightResult represents the extracted flight emissions data
type FlightResult struct {
	Status        string `json:"status"`
	From          string `json:"from"`
	To            string `json:"to"`
	Passengers    int    `json:"passengers"`
	CabinClass    string `json:"cabin_class"`
	DistanceKm    string `json:"distance_km"`
	EmissionsCO2  string `json:"emissions_kg_co2"`
	EmissionsKg   string `json:"emissions_kg,omitempty"`
	EmissionsTons string `json:"emissions_tons,omitempty"`
	EmissionsUnit string `json:"emissions_unit,omitempty"`
	ExtractedAt   string `json:"extracted_at"`
	Error         string `json:"error,omitempty"`
	ExecutionTime int    `json:"execution_time_ms"`
}

// Logger interface for observability
type Logger interface {
	Printf(format string, v ...interface{})
	Errorf(format string, v ...interface{})
}

// SimpleLogger is a basic logger implementation
type SimpleLogger struct{}

func (sl *SimpleLogger) Printf(format string, v ...interface{}) {
	fmt.Printf(format+"\n", v...)
}

func (sl *SimpleLogger) Errorf(format string, v ...interface{}) {
	fmt.Printf("ERROR: "+format+"\n", v...)
}

// MyClimate scraper for flight emissions calculator
type MyClimate struct {
	browser pw.Browser
	logger  Logger
	timeout time.Duration
}

// NewMyClimate creates a new MyClimate scraper with a running browser
func NewMyClimate(browser pw.Browser, logger Logger) *MyClimate {
	if logger == nil {
		logger = &SimpleLogger{}
	}
	return &MyClimate{
		browser: browser,
		logger:  logger,
		timeout: 60 * time.Second,
	}
}

// ScrapeFlightEmissions scrapes flight emissions from the MyClimate calculator
func (m *MyClimate) ScrapeFlightEmissions(ctx context.Context, from, to string, passengers int, cabinClass string) (*FlightResult, error) {
	startTime := time.Now()

	result := &FlightResult{
		From:       from,
		To:         to,
		Passengers: passengers,
		CabinClass: cabinClass,
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(ctx, m.timeout)
	defer cancel()

	page, err := m.browser.NewPage()
	if err != nil {
		result.Status = "error"
		result.Error = fmt.Sprintf("page creation: %v", err)
		m.logger.Errorf("Failed to create page: %v", err)
		return result, err
	}
	defer page.Close()

	// Step 1: Navigate to the calculator
	if err := m.navigateToCalculator(page); err != nil {
		result.Status = "error"
		result.Error = fmt.Sprintf("navigation: %v", err)
		return result, err
	}

	// Step 2: Dismiss consent dialog if present
	if err := m.dismissConsentDialog(page); err != nil {
		m.logger.Printf("Warning: Could not dismiss consent dialog (may be optional): %v", err)
	}

	// Step 3-4: Fill form fields
	if err := m.fillFlightDetails(page, from, to); err != nil {
		result.Status = "error"
		result.Error = fmt.Sprintf("form interaction: %v", err)
		return result, err
	}

	// Step 5: Submit form
	if err := m.submitForm(page); err != nil {
		result.Status = "error"
		result.Error = fmt.Sprintf("form submission: %v", err)
		return result, err
	}

	// Step 6: Extract results
	if err := m.extractResults(page, result); err != nil {
		result.Status = "error"
		result.Error = fmt.Sprintf("extraction: %v", err)
		return result, err
	}

	result.Status = "success"
	result.ExtractedAt = time.Now().Format(time.RFC3339)
	result.ExecutionTime = int(time.Since(startTime).Milliseconds())

	return result, nil
}

func (m *MyClimate) navigateToCalculator(page pw.Page) error {
	m.logger.Printf("[1/6] 📄 Loading flight calculator...")

	_, err := page.Goto(
		"https://co2.myclimate.org/en/flight_calculators/new",
		pw.PageGotoOptions{
			WaitUntil: pw.WaitUntilStateNetworkidle,
		},
	)
	if err != nil {
		return err
	}

	page.WaitForTimeout(2000)
	m.logger.Printf("      ✅ Page loaded")
	return nil
}

func (m *MyClimate) dismissConsentDialog(page pw.Page) error {
	m.logger.Printf("[2/6] 🔐 Checking for consent dialog...")

	// Try multiple consent button selectors
	selectors := []string{
		`button:has-text("Accept")`,
		`button[aria-label*="Accept"]`,
		`button[aria-label*="Close"]`,
		`button:has-text("Accept All")`,
	}

	for _, selector := range selectors {
		locator := page.Locator(selector)
		isVisible, err := locator.IsVisible()
		if err == nil && isVisible {
			m.logger.Printf("      Found consent button: %s", selector)
			if err := locator.Click(); err != nil {
				return err
			}
			page.WaitForTimeout(1000)
			m.logger.Printf("      ✅ Dismissed consent dialog")
			return nil
		}
	}

	m.logger.Printf("      ℹ️  No consent dialog visible")
	return nil
}

func (m *MyClimate) fillFlightDetails(page pw.Page, from, to string) error {
	// Fill FROM airport with fallback strategies
	m.logger.Printf("[3/6] 🛫 Filling departure airport: %s", from)
	if err := m.fillAirportField(page, "from", from, "flight_calculator_from"); err != nil {
		return fmt.Errorf("from airport: %w", err)
	}

	// Fill TO airport
	m.logger.Printf("[4/6] 🛬 Filling arrival airport: %s", to)
	if err := m.fillAirportField(page, "to", to, "flight_calculator_to"); err != nil {
		return fmt.Errorf("to airport: %w", err)
	}

	return nil
}

func (m *MyClimate) fillAirportField(page pw.Page, fieldName, value, expectedID string) error {
	// Strategy 1: Use exact ID
	locator := page.Locator(fmt.Sprintf(`input[id="%s"]`, expectedID))
	isVisible, err := locator.IsVisible()

	if err != nil || !isVisible {
		// Strategy 2: Try name attribute
		locator = page.Locator(fmt.Sprintf(`input[name="flight_calculator[%s]"]`, fieldName))
		isVisible, err = locator.IsVisible()

		if err != nil || !isVisible {
			// Strategy 3: Try placeholder
			locator = page.Locator(fmt.Sprintf(`input[placeholder*="%s"]`, strings.ToTitle(fieldName)))
			isVisible, err = locator.IsVisible()
		}
	}

	if err != nil || !isVisible {
		return fmt.Errorf("could not find %s input field", fieldName)
	}

	// Fill the field
	if err := locator.Fill(value); err != nil {
		return fmt.Errorf("fill failed: %w", err)
	}
	page.WaitForTimeout(500)

	// Use keyboard to select from dropdown (ArrowDown + Enter)
	if err := locator.Press("ArrowDown"); err != nil {
		return fmt.Errorf("dropdown open failed: %w", err)
	}
	page.WaitForTimeout(300)

	if err := locator.Press("Enter"); err != nil {
		return fmt.Errorf("dropdown select failed: %w", err)
	}

	page.WaitForTimeout(1500)
	m.logger.Printf("      ✅ Selected first option")
	return nil
}

func (m *MyClimate) submitForm(page pw.Page) error {
	m.logger.Printf("[5/6] 📤 Submitting form...")

	// Try multiple submit button strategies
	submitStrategies := []struct {
		selector    string
		description string
	}{
		{`button[type="submit"]`, "submit button"},
		{`button:has-text("Calculate")`, "Calculate button"},
		{`button:has-text("Submit")`, "Submit button"},
	}

	for _, strategy := range submitStrategies {
		locator := page.Locator(strategy.selector)
		isVisible, err := locator.IsVisible()
		if err == nil && isVisible {
			m.logger.Printf("      Found %s", strategy.description)
			if err := locator.Click(); err == nil {
				page.WaitForTimeout(3000)
				m.logger.Printf("      ✅ Form submitted via %s", strategy.description)
				return nil
			}
		}
	}

	// Fallback: Press Enter on the last field
	m.logger.Printf("      ⚠️  No submit button found, using keyboard Enter")
	toInput := page.Locator(`input[id="flight_calculator_to"]`)
	if err := toInput.Press("Enter"); err != nil {
		return fmt.Errorf("keyboard submit failed: %w", err)
	}

	page.WaitForTimeout(3000)
	m.logger.Printf("      ✅ Form submitted via keyboard")
	return nil
}

func (m *MyClimate) extractResults(page pw.Page, result *FlightResult) error {
	m.logger.Printf("[6/6] 📊 Extracting results...")

	content, err := page.Content()
	if err != nil {
		return fmt.Errorf("get content failed: %w", err)
	}

	// Extract distance with multiple patterns
	distancePatterns := []struct {
		pattern string
		desc    string
	}{
		{`(\d+[\d\.]*)\s*km`, "distance pattern"},
		{`Distance[:\s]*(\d+[\d\.]*)\s*(?:km|km\.)`, "labeled distance"},
	}

	for _, p := range distancePatterns {
		regex := regexp.MustCompile(p.pattern)
		if matches := regex.FindStringSubmatch(content); matches != nil {
			result.DistanceKm = matches[1]
			m.logger.Printf("      ✅ Found distance (%s): %s km", p.desc, result.DistanceKm)
			break
		}
	}

	// Extract emissions with multiple patterns
	emissionsPatterns := []struct {
		pattern string
		desc    string
		unit    string
	}{
		{`CO₂\s*amount[:\s]*(\d+[\d\.]*)\s*t`, "CO2 amount", "t"},
		{`(\d+[\d\.]*)\s*t\s*CO2`, "tonnes CO2", "t"},
		{`(\d+[\d\.]*)\s*kg\s*CO2`, "kg CO2", "kg"},
	}

	for _, p := range emissionsPatterns {
		regex := regexp.MustCompile(p.pattern)
		if matches := regex.FindStringSubmatch(content); matches != nil {
			value := matches[1]
			result.EmissionsCO2 = value
			result.EmissionsUnit = p.unit
			if p.unit == "kg" {
				result.EmissionsKg = value
				if f, err := strconv.ParseFloat(value, 64); err == nil {
					result.EmissionsTons = fmt.Sprintf("%.3f", f/1000.0)
				}
			} else {
				result.EmissionsTons = value
			}
			m.logger.Printf("      ✅ Found emissions (%s): %s %s", p.desc, value, p.unit)
			break
		}
	}

	if result.DistanceKm == "" && result.EmissionsCO2 == "" {
		m.logger.Printf("      ⚠️  Could not extract any results")
	}

	return nil
}
