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
	if len(os.Args) < 3 {
		fmt.Println("Usage: go run test_prompt_driven.go <url> <instructions>")
		fmt.Println("Example:")
		fmt.Println("  go run test_prompt_driven.go 'https://ecotree.green/en/calculate-flight-co2' 'Fill from field with Southampton, to field with Newcastle, click calculate, extract CO2 emissions'")
		os.Exit(1)
	}

	url := os.Args[1]
	instructions := os.Args[2]

	ctx := context.Background()
	result, err := browseWithInstructions(ctx, url, instructions)
	if err != nil {
		log.Fatalf("Failed: %v", err)
	}

	fmt.Println("\n" + strings.Repeat("=", 60))
	fmt.Println("📊 BROWSER AUTOMATION RESULT")
	fmt.Println(strings.Repeat("=", 60))
	fmt.Printf("\nURL: %s\n", result.URL)
	fmt.Printf("Title: %s\n", result.Title)
	
	if len(result.Extracted) > 0 {
		fmt.Println("\n✅ Extracted Data:")
		for k, v := range result.Extracted {
			fmt.Printf("  %s: %v\n", k, v)
		}
	} else {
		fmt.Println("\n⚠️  No data extracted")
	}
	
	if result.Error != "" {
		fmt.Printf("\n❌ Error: %s\n", result.Error)
	}
	
	fmt.Println(strings.Repeat("=", 60))
}

type BrowseResult struct {
	URL       string
	Title     string
	Extracted map[string]interface{}
	Error     string
}

func browseWithInstructions(ctx context.Context, url, instructions string) (BrowseResult, error) {
	result := BrowseResult{
		URL:       url,
		Extracted: make(map[string]interface{}),
	}

	// Launch browser
	browser, page, err := launchBrowser(ctx)
	if err != nil {
		return result, fmt.Errorf("failed to launch browser: %w", err)
	}
	defer browser.Close()
	defer page.Close()

	// Navigate
	fmt.Printf("🌐 Navigating to: %s\n", url)
	if err := page.Navigate(url); err != nil {
		return result, fmt.Errorf("failed to navigate: %w", err)
	}

	if err := page.WaitLoad(); err != nil {
		return result, fmt.Errorf("failed to wait for page load: %w", err)
	}

	// Wait for page to stabilize
	fmt.Println("⏳ Waiting for page to load...")
	time.Sleep(5 * time.Second)
	page.Timeout(15 * time.Second).WaitStable(time.Millisecond * 500)

	// Get page title
	var title string
	_, err = page.Eval(`() => document.title`, &title)
	if err == nil {
		result.Title = title
		fmt.Printf("📄 Page title: %s\n", title)
	}

	// Get page HTML for analysis
	fmt.Println("📥 Getting page HTML...")
	html, err := page.HTML()
	if err != nil {
		return result, fmt.Errorf("failed to get page HTML: %w", err)
	}

	// For now, print instructions and HTML preview
	fmt.Printf("\n📋 Instructions: %s\n", instructions)
	fmt.Printf("📄 Page HTML size: %d bytes\n", len(html))
	fmt.Println("\n💡 This is a demonstration of prompt-driven browser automation.")
	fmt.Println("   In the full implementation, the LLM would:")
	fmt.Println("   1. Analyze the page HTML")
	fmt.Println("   2. Generate actions based on your instructions")
	fmt.Println("   3. Execute those actions")
	fmt.Println("   4. Extract the requested data")
	fmt.Println("\n   For now, you can manually inspect the HTML to find selectors.")
	fmt.Println("   HTML saved to: /tmp/browser_page.html")

	// Save HTML for inspection
	if err := os.WriteFile("/tmp/browser_page.html", []byte(html), 0644); err == nil {
		fmt.Printf("💾 Saved page HTML to /tmp/browser_page.html\n")
	}

	// Try to extract some basic info using JavaScript
	fmt.Println("\n🔍 Attempting basic extraction...")
	
	// Extract all text content for analysis
	var pageText string
	_, err = page.Eval(`() => {
		return document.body.innerText || document.body.textContent || '';
	}`, &pageText)
	
	if err == nil && pageText != "" {
		fmt.Printf("📝 Page text extracted: %d characters\n", len(pageText))
		
		// Look for CO2-related content
		lines := strings.Split(pageText, "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			lower := strings.ToLower(line)
			if (strings.Contains(lower, "co2") || strings.Contains(lower, "carbon")) && 
			   (strings.Contains(lower, "kg") || strings.Contains(lower, "tonne") || strings.Contains(lower, "g")) {
				result.Extracted["potential_co2_value"] = line
				fmt.Printf("✅ Found potential CO2 value: %s\n", line)
				break
			}
		}
	}

	// Try to find form elements using JavaScript
	fmt.Println("\n🔍 Analyzing page structure...")
	var formInfo string
	_, err = page.Eval(`() => {
		const info = {
			inputs: document.querySelectorAll('input').length,
			buttons: document.querySelectorAll('button').length,
			selects: document.querySelectorAll('select').length,
			forms: document.querySelectorAll('form').length,
			hasFromField: !!document.querySelector('input[placeholder*="From"], input[placeholder*="from"], input[name*="from"]'),
			hasToField: !!document.querySelector('input[placeholder*="To"], input[placeholder*="to"], input[name*="to"]'),
			hasCalculateButton: !!document.querySelector('button:contains("Calculate"), button[class*="calculate"]')
		};
		return JSON.stringify(info);
	}`, &formInfo)
	
	if err == nil && formInfo != "" {
		var info map[string]interface{}
		if err := json.Unmarshal([]byte(formInfo), &info); err == nil {
			fmt.Printf("📊 Page structure:\n")
			fmt.Printf("  - Input fields: %.0f\n", info["inputs"])
			fmt.Printf("  - Buttons: %.0f\n", info["buttons"])
			fmt.Printf("  - Select fields: %.0f\n", info["selects"])
			fmt.Printf("  - Forms: %.0f\n", info["forms"])
			fmt.Printf("  - Has 'from' field: %v\n", info["hasFromField"])
			fmt.Printf("  - Has 'to' field: %v\n", info["hasToField"])
			fmt.Printf("  - Has calculate button: %v\n", info["hasCalculateButton"])
			
			result.Extracted["page_structure"] = info
		}
	}

	return result, nil
}

func launchBrowser(ctx context.Context) (*rod.Browser, *rod.Page, error) {
	fmt.Println("🚀 Launching browser...")

	l := launcher.New().
		Headless(true).
		NoSandbox(true).
		Set("disable-blink-features", "AutomationControlled").
		Set("disable-dev-shm-usage").
		Set("disable-gpu")

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

