package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: go run test_standalone.go <test_name>")
		fmt.Println("Available tests:")
		fmt.Println("  basic     - Basic navigation test")
		fmt.Println("  co2       - CO2 calculator test (ecotree.green)")
		fmt.Println("  form      - Simple form filling test")
		os.Exit(1)
	}

	testName := os.Args[1]
	ctx := context.Background()

	switch testName {
	case "basic":
		testBasicNavigation(ctx)
	case "co2":
		testCO2Calculator(ctx)
	case "form":
		testFormFilling(ctx)
	default:
		fmt.Printf("Unknown test: %s\n", testName)
		os.Exit(1)
	}
}

func testBasicNavigation(ctx context.Context) {
	fmt.Println("🧪 Test 1: Basic Navigation")
	fmt.Println("============================")

	browser, page, err := launchBrowser(ctx)
	if err != nil {
		log.Fatalf("Failed to launch browser: %v", err)
	}
	defer browser.Close()
	defer page.Close()

	// Navigate to a simple page
	url := "https://example.com"
	fmt.Printf("Navigating to: %s\n", url)
	if err := page.Navigate(url); err != nil {
		log.Fatalf("Failed to navigate: %v", err)
	}

	if err := page.WaitLoad(); err != nil {
		log.Fatalf("Failed to wait for page load: %v", err)
	}

	// Get title
	var title string
	_, err = page.Eval(`() => document.title`, &title)
	if err != nil {
		log.Fatalf("Failed to get title: %v", err)
	}

	fmt.Printf("✅ Page title: %s\n", title)

	// Get some text content
	var heading string
	_, err = page.Eval(`() => document.querySelector('h1')?.textContent || ''`, &heading)
	if err == nil {
		fmt.Printf("✅ Heading: %s\n", heading)
	}

	fmt.Println("✅ Basic navigation test passed!")
}

func testFormFilling(ctx context.Context) {
	fmt.Println("🧪 Test 2: Form Filling")
	fmt.Println("======================")

	browser, page, err := launchBrowser(ctx)
	if err != nil {
		log.Fatalf("Failed to launch browser: %v", err)
	}
	defer browser.Close()
	defer page.Close()

	// Use a test form page (httpbin.org has a simple form)
	url := "https://httpbin.org/forms/post"
	fmt.Printf("Navigating to: %s\n", url)
	if err := page.Navigate(url); err != nil {
		log.Fatalf("Failed to navigate: %v", err)
	}

	if err := page.WaitLoad(); err != nil {
		log.Fatalf("Failed to wait for page load: %v", err)
	}

	// Wait for form to be ready
	time.Sleep(1 * time.Second)

	// Fill form fields
	fmt.Println("Filling form fields...")

	// Find and fill customer name
	customerField, err := page.Timeout(5 * time.Second).Element("input[name='custname']")
	if err != nil {
		log.Fatalf("Failed to find customer field: %v", err)
	}
	if err := customerField.Input("Test User"); err != nil {
		log.Fatalf("Failed to fill customer field: %v", err)
	}
	fmt.Println("✅ Filled customer name")

	// Find and fill telephone
	telField, err := page.Timeout(5 * time.Second).Element("input[name='custtel']")
	if err != nil {
		log.Fatalf("Failed to find telephone field: %v", err)
	}
	if err := telField.Input("123-456-7890"); err != nil {
		log.Fatalf("Failed to fill telephone field: %v", err)
	}
	fmt.Println("✅ Filled telephone")

	// Find and select size
	sizeField, err := page.Timeout(5 * time.Second).Element("input[value='medium']")
	if err != nil {
		log.Fatalf("Failed to find size field: %v", err)
	}
	if err := sizeField.Click(proto.InputMouseButtonLeft, 1); err != nil {
		log.Fatalf("Failed to click size: %v", err)
	}
	fmt.Println("✅ Selected size")

	// Extract form values to verify
	var custName string
	_, err = customerField.Eval(`() => this.value`, &custName)
	if err == nil {
		fmt.Printf("✅ Verified customer name: %s\n", custName)
	}

	fmt.Println("✅ Form filling test passed!")
}

func testCO2Calculator(ctx context.Context) {
	fmt.Println("🧪 Test 3: Plane CO2 Calculator (ecotree.green)")
	fmt.Println("=================================================")
	fmt.Println("Journey: Southampton → Newcastle (Plane)")

	browser, page, err := launchBrowser(ctx)
	if err != nil {
		log.Fatalf("Failed to launch browser: %v", err)
	}
	defer browser.Close()
	defer page.Close()

	url := "https://ecotree.green/en/calculate-flight-co2"
	fmt.Printf("Navigating to: %s\n", url)
	
	if err := page.Navigate(url); err != nil {
		log.Fatalf("Failed to navigate: %v", err)
	}

	if err := page.WaitLoad(); err != nil {
		log.Fatalf("Failed to wait for page load: %v", err)
	}

	// Check if we got redirected or blocked
	var currentURL string
	_, err = page.Eval(`() => window.location.href`, &currentURL)
	if err == nil {
		fmt.Printf("Current URL: %s\n", currentURL)
		if currentURL != url && !strings.Contains(currentURL, "calculate-flight-co2") {
			fmt.Printf("⚠️  Page redirected to: %s\n", currentURL)
		}
	}

	// Wait for page to fully load and JavaScript to execute
	fmt.Println("Waiting for page to load (including JavaScript)...")
	time.Sleep(8 * time.Second) // Longer wait for React to render

	// Try to wait for specific elements that indicate the page is ready
	page.Timeout(15 * time.Second).WaitStable(time.Millisecond * 500)
	
	// Scroll down to ensure form is visible (React apps often lazy load)
	page.Mouse.Scroll(0, 500, 1)
	time.Sleep(2 * time.Second)

	// Get page title
	var title string
	_, err = page.Eval(`() => document.title`, &title)
	if err == nil {
		fmt.Printf("Page title: %s\n", title)
	}

	// Try to find form elements
	fmt.Println("\n🔍 Inspecting page structure...")

	// Get all input fields
	var inputCount int
	_, err = page.Eval(`() => document.querySelectorAll('input').length`, &inputCount)
	if err == nil {
		fmt.Printf("Found %d input fields\n", inputCount)
	}

	// Get all select fields
	var selectCount int
	_, err = page.Eval(`() => document.querySelectorAll('select').length`, &selectCount)
	if err == nil {
		fmt.Printf("Found %d select fields\n", selectCount)
	}

	// Get all buttons
	var buttonCount int
	_, err = page.Eval(`() => document.querySelectorAll('button').length`, &buttonCount)
	if err == nil {
		fmt.Printf("Found %d buttons\n", buttonCount)
	}

	// Get all form elements (including custom components)
	var formElementCount int
	_, err = page.Eval(`() => {
		const all = document.querySelectorAll('input, select, button, [role="textbox"], [role="button"], [contenteditable="true"]');
		return all.length;
	}`, &formElementCount)
	if err == nil {
		fmt.Printf("Found %d total form-like elements (including ARIA roles)\n", formElementCount)
	}

	// Get page HTML snippet for debugging
	var htmlSnippet string
	_, err = page.Eval(`() => {
		const body = document.body;
		if (!body) return 'No body';
		const html = body.innerHTML.substring(0, 2000);
		return html;
	}`, &htmlSnippet)
	if err == nil && htmlSnippet != "" {
		fmt.Printf("\n📄 Page HTML snippet (first 2000 chars):\n")
		fmt.Printf("%s...\n\n", htmlSnippet)
	} else {
		fmt.Printf("\n⚠️  Could not get page HTML - page may not have loaded\n")
	}

	// Save full HTML to file for inspection
	html, err := page.HTML()
	if err == nil && html != "" {
		htmlFile := "/tmp/ecotree_page.html"
		if err := os.WriteFile(htmlFile, []byte(html), 0644); err == nil {
			fmt.Printf("💾 Saved full page HTML to: %s\n", htmlFile)
			fmt.Printf("   File size: %d bytes\n", len(html))
		}
	}

	// Try to find form fields by common patterns
	fmt.Println("\n🔍 Looking for form fields...")

	// Look for "from" field - try more comprehensive selectors including React components
	fromSelectors := []string{
		"input[placeholder*='From']",
		"input[placeholder*='from']",
		"input[placeholder*='Departure']",
		"input[placeholder*='departure']",
		"input[name='from']",
		"input[name='departure']",
		"input[id*='from']",
		"input[id*='departure']",
		"input[data-testid*='from']",
		"input[type='text']", // Fallback: try first text input
		"[role='textbox']", // React autocomplete components
		"input[autocomplete='off']",
	}

	var fromField *rod.Element
	for i, selector := range fromSelectors {
		elem, err := page.Timeout(3 * time.Second).Element(selector)
		if err == nil {
			// For generic selectors like "input[type='text']", get the first one
			if i >= len(fromSelectors)-3 { // Last few are generic
				// Get all matching elements and use the first visible one
				elements, err := page.Timeout(3 * time.Second).Elements(selector)
				if err == nil && len(elements) > 0 {
					fromField = elements[0]
					fmt.Printf("✅ Found 'from' field (using first of %d matches with selector: %s)\n", len(elements), selector)
					break
				}
			} else {
				fromField = elem
				fmt.Printf("✅ Found 'from' field with selector: %s\n", selector)
				break
			}
		}
	}

	if fromField != nil {
		// Try clicking first to focus, then typing
		fromField.Click(proto.InputMouseButtonLeft, 1)
		time.Sleep(300 * time.Millisecond)
		if err := fromField.Input("Southampton"); err != nil {
			fmt.Printf("⚠️  Failed to fill 'from' field: %v\n", err)
		} else {
			fmt.Println("✅ Filled 'from' field with 'Southampton'")
			time.Sleep(2 * time.Second) // Wait for autocomplete to appear and select
			// Try to select first autocomplete option if it appears
			page.Keyboard.Press('\n') // Press Enter to select autocomplete
			time.Sleep(1 * time.Second)
		}
	} else {
		fmt.Println("⚠️  Could not find 'from' field - trying JavaScript approach...")
		// Try JavaScript approach for React components
		var jsResult bool
		_, err = page.Eval(`() => {
			const inputs = document.querySelectorAll('input[type="text"], input[placeholder*="From"], input[placeholder*="from"], input[placeholder*="Departure"], input[placeholder*="departure"]');
			if (inputs.length > 0) {
				const input = inputs[0];
				input.focus();
				input.value = 'Southampton';
				input.dispatchEvent(new Event('input', { bubbles: true }));
				input.dispatchEvent(new Event('change', { bubbles: true }));
				// Also try React's onChange
				const nativeInputValueSetter = Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, 'value').set;
				nativeInputValueSetter.call(input, 'Southampton');
				input.dispatchEvent(new Event('input', { bubbles: true }));
				return true;
			}
			return false;
		}`, &jsResult)
		if err == nil && jsResult {
			fmt.Println("✅ Filled 'from' field using JavaScript")
			time.Sleep(2 * time.Second)
		} else {
			fmt.Printf("⚠️  JavaScript fill failed: %v\n", err)
		}
	}

	// Look for "to" field - try more comprehensive selectors
	toSelectors := []string{
		"input[placeholder*='To']",
		"input[placeholder*='to']",
		"input[placeholder*='Destination']",
		"input[placeholder*='destination']",
		"input[placeholder*='Arrival']",
		"input[placeholder*='arrival']",
		"input[name='to']",
		"input[name='destination']",
		"input[name='arrival']",
		"input[id*='to']",
		"input[id*='destination']",
		"input[id*='arrival']",
		"input[data-testid*='to']",
		"input[type='text']", // Fallback: try second text input
		"[role='textbox']", // React autocomplete components
	}

	var toField *rod.Element
	for i, selector := range toSelectors {
		elem, err := page.Timeout(3 * time.Second).Element(selector)
		if err == nil {
			// For generic selectors, try to get the second input (after "from")
			if i >= len(toSelectors)-3 { // Last few are generic
				elements, err := page.Timeout(3 * time.Second).Elements(selector)
				if err == nil && len(elements) > 1 {
					toField = elements[1] // Second input is usually "to"
					fmt.Printf("✅ Found 'to' field (using second of %d matches with selector: %s)\n", len(elements), selector)
					break
				} else if err == nil && len(elements) > 0 {
					toField = elements[0]
					fmt.Printf("✅ Found 'to' field (using first of %d matches with selector: %s)\n", len(elements), selector)
					break
				}
			} else {
				toField = elem
				fmt.Printf("✅ Found 'to' field with selector: %s\n", selector)
				break
			}
		}
	}

	if toField != nil {
		// Try clicking first to focus, then typing
		toField.Click(proto.InputMouseButtonLeft, 1)
		time.Sleep(300 * time.Millisecond)
		if err := toField.Input("Newcastle"); err != nil {
			fmt.Printf("⚠️  Failed to fill 'to' field: %v\n", err)
		} else {
			fmt.Println("✅ Filled 'to' field with 'Newcastle'")
			time.Sleep(2 * time.Second) // Wait for autocomplete
			// Try to select first autocomplete option
			page.Keyboard.Press('\n') // Press Enter to select autocomplete
			time.Sleep(1 * time.Second)
		}
	} else {
		fmt.Println("⚠️  Could not find 'to' field - trying JavaScript approach...")
		// Try JavaScript approach for React components
		var jsResult bool
		_, err = page.Eval(`() => {
			const inputs = document.querySelectorAll('input[type="text"], input[placeholder*="To"], input[placeholder*="to"], input[placeholder*="Destination"], input[placeholder*="destination"], input[placeholder*="Arrival"], input[placeholder*="arrival"]');
			// Get the second input (after 'from')
			const input = inputs.length > 1 ? inputs[1] : inputs[0];
			if (input) {
				input.focus();
				input.value = 'Newcastle';
				input.dispatchEvent(new Event('input', { bubbles: true }));
				input.dispatchEvent(new Event('change', { bubbles: true }));
				// Also try React's onChange
				const nativeInputValueSetter = Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, 'value').set;
				nativeInputValueSetter.call(input, 'Newcastle');
				input.dispatchEvent(new Event('input', { bubbles: true }));
				return true;
			}
			return false;
		}`, &jsResult)
		if err == nil && jsResult {
			fmt.Println("✅ Filled 'to' field using JavaScript")
			time.Sleep(2 * time.Second)
		} else {
			fmt.Printf("⚠️  JavaScript fill failed: %v\n", err)
		}
	}

	// For plane calculator, transport type is already set to "plane"
	// No need to select transport type - it's already on the plane calculator page
	fmt.Println("ℹ️  Using plane calculator (transport type already set)")

	// Look for calculate/submit button - try more comprehensive selectors
	buttonSelectors := []string{
		"button:contains('Calculate')",
		"button:contains('calculate')",
		"button:contains('CALCULATE')",
		"button[class*='calculate']",
		"button[class*='Calculate']",
		"button[class*='submit']",
		"button[type='submit']",
		"button[type='button']",
		"input[type='submit']",
		"[role='button']",
		"a[class*='calculate']",
		"div[class*='calculate'][role='button']",
	}

	var submitButton *rod.Element
	for _, selector := range buttonSelectors {
		elem, err := page.Timeout(2 * time.Second).Element(selector)
		if err == nil {
			submitButton = elem
			fmt.Printf("✅ Found submit button with selector: %s\n", selector)
			break
		}
	}

	if submitButton != nil {
		fmt.Println("\n🖱️  Clicking calculate button...")
		if err := submitButton.ScrollIntoView(); err == nil {
			if err := submitButton.Click(proto.InputMouseButtonLeft, 1); err != nil {
				fmt.Printf("⚠️  Failed to click button: %v\n", err)
			} else {
				fmt.Println("✅ Clicked calculate button")
				fmt.Println("⏳ Waiting for calculation results...")
				time.Sleep(5 * time.Second) // Wait longer for results to appear
			}
		}
	} else {
		fmt.Println("⚠️  Could not find submit button - trying JavaScript approach...")
		// Try JavaScript to find and click button
		var clicked bool
		_, err = page.Eval(`() => {
			const buttons = document.querySelectorAll('button, [role="button"], input[type="submit"], a[class*="button"]');
			for (let btn of buttons) {
				const text = btn.textContent || btn.innerText || '';
				if (text.toLowerCase().includes('calculate') || text.toLowerCase().includes('submit')) {
					btn.click();
					return true;
				}
			}
			// Try form submit
			const forms = document.querySelectorAll('form');
			if (forms.length > 0) {
				forms[0].submit();
				return true;
			}
			return false;
		}`, &clicked)
		if err == nil && clicked {
			fmt.Println("✅ Clicked calculate button using JavaScript")
			time.Sleep(5 * time.Second)
		}
	}

	// Try to extract results with more comprehensive selectors
	fmt.Println("\n🔍 Extracting CO2 emissions result...")
	time.Sleep(2 * time.Second)

	// More comprehensive result selectors
	resultSelectors := []string{
		"[class*='result']",
		"[class*='co2']",
		"[class*='emission']",
		"[class*='carbon']",
		"[id*='result']",
		"[id*='co2']",
		"[data-testid*='result']",
		".result",
		".co2-result",
		".emission-result",
		"div:contains('kg CO2')",
		"div:contains('CO2')",
		"span:contains('kg')",
	}

	var results map[string]interface{} = make(map[string]interface{})
	var co2Value string

	// Try multiple strategies to find the CO2 value
	for _, selector := range resultSelectors {
		elem, err := page.Timeout(3 * time.Second).Element(selector)
		if err == nil {
			var text string
			_, err = elem.Eval(`() => this.textContent || this.innerText`, &text)
			if err == nil && text != "" {
				text = strings.TrimSpace(text)
				// Look for CO2 values (numbers with kg, CO2, etc.)
				if strings.Contains(strings.ToLower(text), "co2") || 
				   strings.Contains(strings.ToLower(text), "kg") ||
				   strings.Contains(strings.ToLower(text), "carbon") {
					co2Value = text
					results["co2_emissions"] = text
					fmt.Printf("✅ Found CO2 result with selector: %s\n", selector)
					break
				}
			}
		}
	}

	// If we didn't find it with specific selectors, try comprehensive JavaScript extraction
	if co2Value == "" {
		fmt.Println("🔍 Trying comprehensive JavaScript extraction...")
		var extractedValue string
		_, err = page.Eval(`() => {
			// Try multiple strategies to find CO2 value
			// 1. Look for elements with CO2-related classes/IDs
			const co2Selectors = [
				'[class*="co2"]',
				'[class*="carbon"]',
				'[class*="emission"]',
				'[id*="co2"]',
				'[id*="result"]',
				'.result',
				'.co2-result'
			];
			
			for (let selector of co2Selectors) {
				const elems = document.querySelectorAll(selector);
				for (let elem of elems) {
					const text = elem.textContent || elem.innerText || '';
					if (text.includes('kg') && (text.includes('CO2') || text.includes('CO₂'))) {
						return text.trim();
					}
				}
			}
			
			// 2. Search all text for CO2 patterns
			const bodyText = document.body.innerText || document.body.textContent || '';
			const lines = bodyText.split('\n');
			for (let line of lines) {
				line = line.trim();
				if (line.length > 0 && line.length < 200) {
					const lower = line.toLowerCase();
					if ((lower.includes('kg') && lower.includes('co2')) ||
					    (lower.includes('kg') && lower.includes('co₂')) ||
					    (lower.includes('carbon') && lower.includes('kg'))) {
						// Extract just the number and unit
						const match = line.match(/([\d,]+\.?\d*)\s*(kg|tonne|t)?\s*(co2|co₂|carbon)?/i);
						if (match) {
							return match[0];
						}
						return line;
					}
				}
			}
			
			return '';
		}`, &extractedValue)
		if err == nil && extractedValue != "" {
			co2Value = extractedValue
			results["co2_emissions"] = extractedValue
			fmt.Printf("✅ Found CO2 value using JavaScript: %s\n", extractedValue)
		}
	}

	// Print final results
	fmt.Println("\n" + strings.Repeat("=", 60))
	fmt.Println("📊 CO2 CALCULATION RESULT")
	fmt.Println(strings.Repeat("=", 60))
	
	if co2Value != "" {
		fmt.Printf("\n✅ SUCCESS! CO2 Emissions Found:\n\n")
		fmt.Printf("   🛫 Journey: Southampton → Newcastle (Plane)\n")
		fmt.Printf("   💨 CO2 Emissions: %s\n\n", co2Value)
		
		resultsJSON, _ := json.MarshalIndent(results, "", "  ")
		fmt.Println("📋 Full Results (JSON):")
		fmt.Println(string(resultsJSON))
	} else {
		fmt.Println("\n⚠️  CO2 value not found in results")
		fmt.Println("   This might mean:")
		fmt.Println("   - The calculation hasn't completed yet")
		fmt.Println("   - The result is displayed in a different format")
		fmt.Println("   - The page structure is different than expected")
		fmt.Println("\n   Check the saved HTML file for details: /tmp/ecotree_page.html")
		
		if len(results) > 0 {
			fmt.Println("\n   Partial results found:")
			resultsJSON, _ := json.MarshalIndent(results, "", "  ")
			fmt.Println(string(resultsJSON))
		}
	}
	fmt.Println(strings.Repeat("=", 60))

	// Save screenshot for debugging
	screenshotPath := "/tmp/co2_calculator_test.png"
	_, err = page.Screenshot(true, &proto.PageCaptureScreenshot{
		Format: proto.PageCaptureScreenshotFormatPng,
	})
	if err == nil {
		fmt.Printf("\n💡 Screenshot saved to: %s\n", screenshotPath)
	} else {
		fmt.Printf("\n💡 Tip: Use page.Screenshot() to debug page state\n")
	}

	fmt.Println("\n✅ CO2 calculator test completed!")
}

func launchBrowser(ctx context.Context) (*rod.Browser, *rod.Page, error) {
	fmt.Println("🚀 Launching browser...")

	l := launcher.New().
		Headless(true).
		NoSandbox(true).
		Set("disable-blink-features", "AutomationControlled").
		Set("disable-dev-shm-usage").
		Set("disable-gpu").
		UserDataDir("")

	launcherURL, err := l.Launch()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to launch browser: %w", err)
	}

	browser := rod.New().ControlURL(launcherURL)
	if err := browser.Connect(); err != nil {
		l.Cleanup()
		return nil, nil, fmt.Errorf("failed to connect to browser: %w", err)
	}

	page, err := browser.Timeout(30 * time.Second).Page(proto.TargetCreateTarget{})
	if err != nil {
		browser.Close()
		l.Cleanup()
		return nil, nil, fmt.Errorf("failed to create page: %w", err)
	}

	return browser, page, nil
}

