package main

import (
	"log"
	"regexp"
	"strings"
	"time"

	pw "github.com/playwright-community/playwright-go"
)

func main() {
	log.Println("🚀 Starting Playwright test...")
	
	// Initialize Playwright
	err := pw.Install(&pw.RunOptions{Verbose: false})
	if err != nil {
		log.Fatalf("Failed to install Playwright: %v", err)
	}
	
	playwright, err := pw.Run()
	if err != nil {
		log.Fatalf("Failed to start Playwright: %v", err)
	}
	defer playwright.Stop()
	
	// Launch browser
	browser, err := playwright.Chromium.Launch(pw.BrowserTypeLaunchOptions{
		Headless: pw.Bool(true),
	})
	if err != nil {
		log.Fatalf("Failed to launch browser: %v", err)
	}
	defer browser.Close()
	
	page, err := browser.NewPage()
	if err != nil {
		log.Fatalf("Failed to create page: %v", err)
	}
	
	log.Println("🚀 Navigating to EcoTree...")
	_, err = page.Goto("https://ecotree.green/en/calculate-flight-co2")
	if err != nil {
		log.Fatalf("Failed to navigate: %v", err)
	}
	
	log.Println("✈️  Selecting Plane...")
	err = page.GetByRole("link", pw.PageGetByRoleOptions{Name: "Plane"}).Click()
	if err != nil {
		log.Printf("Click Plane error: %v", err)
	}
	
	log.Println("📍 Entering Southampton...")
	err = page.GetByRole("textbox", pw.PageGetByRoleOptions{Name: "From To Via"}).Click()
	if err != nil {
		log.Printf("Click From error: %v", err)
	}
	
	err = page.GetByRole("textbox", pw.PageGetByRoleOptions{Name: "From To Via"}).Fill("southampton")
	if err != nil {
		log.Printf("Fill Southampton error: %v", err)
	}
	
	err = page.GetByText("Southampton, United Kingdom").Click()
	if err != nil {
		log.Printf("Click Southampton result error: %v", err)
	}
	
	log.Println("📍 Entering Newcastle...")
	err = page.Locator("input[name=\"To\"]").Click()
	if err != nil {
		log.Printf("Click To error: %v", err)
	}
	
	err = page.Locator("input[name=\"To\"]").Fill("newcastle")
	if err != nil {
		log.Printf("Fill Newcastle error: %v", err)
	}
	
	err = page.GetByText("Newcastle, United Kingdom").First().Click()
	if err != nil {
		log.Printf("Click Newcastle result error: %v", err)
	}
	
	log.Println("🔢 Clicking Calculate...")
	err = page.GetByRole("link", pw.PageGetByRoleOptions{Name: " Calculate my emissions "}).Click()
	if err != nil {
		log.Printf("Click Calculate error: %v", err)
	}
	
	log.Println("⏳ Waiting for results (5 seconds)...")
	time.Sleep(5 * time.Second)
	
	log.Printf("\n📊 Current URL: %s\n", page.URL())
	
	// Get all text content
	content, err := page.TextContent("body")
	if err != nil {
		log.Fatalf("Failed to get content: %v", err)
	}
	
	// Get HTML content
	htmlContent, err := page.Content()
	if err != nil {
		log.Fatalf("Failed to get HTML: %v", err)
	}
	
	log.Printf("📏 Text content length: %d bytes", len(content))
	log.Printf("📏 HTML content length: %d bytes", len(htmlContent))
	
	// Find all numbers followed by kg
	kgRegex := regexp.MustCompile(`(?i)(\d+(?:[.,]\d+)?)\s*kg`)
	kgMatches := kgRegex.FindAllStringSubmatch(content, -1)
	
	log.Printf("\n🔍 All 'kg' values found in TEXT: ")
	kgValues := []string{}
	for _, match := range kgMatches {
		if len(match) > 1 {
			kgValues = append(kgValues, match[1])
		}
	}
	log.Printf("%v", kgValues)
	
	// Try in HTML too
	kgMatchesHTML := kgRegex.FindAllStringSubmatch(htmlContent, -1)
	log.Printf("\n🔍 All 'kg' values found in HTML: ")
	kgValuesHTML := []string{}
	seen := make(map[string]bool)
	for _, match := range kgMatchesHTML {
		if len(match) > 1 && !seen[match[1]] {
			kgValuesHTML = append(kgValuesHTML, match[1])
			seen[match[1]] = true
		}
	}
	log.Printf("%v", kgValuesHTML)
	
	// Find all numbers followed by km
	kmRegex := regexp.MustCompile(`(?i)(\d+(?:[.,]\d+)?)\s*km`)
	kmMatches := kmRegex.FindAllStringSubmatch(content, -1)
	
	log.Printf("\n🔍 All 'km' values found in TEXT: ")
	kmValues := []string{}
	for _, match := range kmMatches {
		if len(match) > 1 {
			kmValues = append(kmValues, match[1])
		}
	}
	log.Printf("%v", kmValues)
	
	// Try in HTML too
	kmMatchesHTML := kmRegex.FindAllStringSubmatch(htmlContent, -1)
	log.Printf("\n🔍 All 'km' values found in HTML: ")
	kmValuesHTML := []string{}
	seenKm := make(map[string]bool)
	for _, match := range kmMatchesHTML {
		if len(match) > 1 && !seenKm[match[1]] {
			kmValuesHTML = append(kmValuesHTML, match[1])
			seenKm[match[1]] = true
		}
	}
	log.Printf("%v\n", kmValuesHTML)
	
	// Try to find specific result elements
	log.Println("\n🎯 Looking for result elements...")
	
	// Check if there are specific elements we should target
	// Look for elements containing "Kilometers" or "CO2"
	if strings.Contains(content, "Kilometers") {
		log.Println("✅ Found 'Kilometers' in content")
	} else {
		log.Println("❌ 'Kilometers' NOT found in content")
	}
	
	if strings.Contains(content, "Your travelled distance") {
		log.Println("✅ Found 'Your travelled distance' in content")
	} else {
		log.Println("❌ 'Your travelled distance' NOT found in content")
	}
	
	if strings.Contains(content, "Your carbon emissions") {
		log.Println("✅ Found 'Your carbon emissions' in content")
	} else {
		log.Println("❌ 'Your carbon emissions' NOT found in content")
	}
	
	// Save screenshot
	_, err = page.Screenshot(pw.PageScreenshotOptions{
		Path: pw.String("/tmp/ecotree_go_result.png"),
	})
	if err != nil {
		log.Printf("Screenshot error: %v", err)
	} else {
		log.Println("\n📸 Screenshot saved to /tmp/ecotree_go_result.png")
	}
	
	log.Println("\n✅ Test complete!")
}

