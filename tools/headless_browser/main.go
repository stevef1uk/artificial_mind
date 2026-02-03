package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/playwright-community/playwright-go"
)

func main() {
	url := flag.String("url", "", "URL to navigate to")
	actionsJSON := flag.String("actions", "[]", "JSON array of actions to perform")
	timeout := flag.Int("timeout", 30, "Timeout in seconds")
	returnHTML := flag.Bool("html", false, "Return HTML instead of JSON")
	fastMode := flag.Bool("fast", false, "Use fast mode (skip some waits)")
	flag.Parse()

	if *url == "" {
		fmt.Fprintln(os.Stderr, "missing -url")
		os.Exit(2)
	}

	// Parse actions
	var actions []map[string]interface{}
	if err := json.Unmarshal([]byte(*actionsJSON), &actions); err != nil {
		log.Printf("⚠️ Failed to parse actions JSON: %v, using empty actions", err)
		actions = []map[string]interface{}{}
	}

	// Initialize Playwright
	pw, err := playwright.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to start Playwright: %v\n", err)
		os.Exit(1)
	}
	// NOTE: Don't defer pw.Stop() - it hangs! We'll os.Exit(0) instead

	// Launch browser
	browser, err := pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
		Headless: playwright.Bool(true),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to launch browser: %v\n", err)
		os.Exit(1)
	}
	// NOTE: Don't defer browser.Close() - can hang! We'll os.Exit(0) instead

	// Create new page
	page, err := browser.NewPage()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create page: %v\n", err)
		os.Exit(1)
	}
	// NOTE: Don't defer page.Close() - can hang! We'll os.Exit(0) instead

	// Set timeout
	page.SetDefaultTimeout(float64(*timeout) * 1000) // Playwright uses milliseconds

	// Navigate
	if _, err := page.Goto(*url, playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateNetworkidle,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to navigate: %v\n", err)
		os.Exit(1)
	}

	// If fast mode, skip some waits
	if !*fastMode {
		time.Sleep(2 * time.Second)
		page.WaitForLoadState(playwright.PageWaitForLoadStateOptions{
			State: playwright.LoadStateNetworkidle,
		})
	}

	// If HTML mode, just return HTML
	if *returnHTML {
		html, err := page.Content()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to get HTML: %v\n", err)
			os.Exit(1)
		}
		fmt.Print(html)
		return
	}

	// Execute actions
	result := map[string]interface{}{
		"url":      *url,
		"success":  true,
		"extracted": make(map[string]interface{}),
	}

	// Get page title
	title, err := page.Title()
	if err == nil {
		result["title"] = title
	}

	// Execute actions
	for _, action := range actions {
		actionType, _ := action["type"].(string)
		selector, _ := action["selector"].(string)

		switch actionType {
		case "fill":
			value, _ := action["value"].(string)
			if selector != "" && value != "" {
				if err := page.Fill(selector, value); err != nil {
					log.Printf("⚠️ Failed to fill %s: %v", selector, err)
				}
			}
		case "press":
			// Press a keyboard key (e.g., "Enter", "Tab", "Escape")
			key, _ := action["key"].(string)
			if key != "" {
				if err := page.Keyboard().Press(key); err != nil {
					log.Printf("⚠️ Failed to press key %s: %v", key, err)
				}
			}
		case "click":
			if selector != "" {
				if err := page.Click(selector); err != nil {
					log.Printf("⚠️ Failed to click %s: %v", selector, err)
				} else {
					if !*fastMode {
						time.Sleep(1 * time.Second)
					}
				}
			}
		case "wait":
			if selector != "" {
				waitTimeout := float64(*timeout) * 1000
				if timeoutVal, ok := action["timeout"].(float64); ok {
					waitTimeout = timeoutVal * 1000
				}
				page.WaitForSelector(selector, playwright.PageWaitForSelectorOptions{
					Timeout: playwright.Float(waitTimeout),
				})
			}
		case "select":
			value, _ := action["value"].(string)
			if selector != "" && value != "" {
				_, err := page.SelectOption(selector, playwright.SelectOptionValues{Values: &[]string{value}})
				if err != nil {
					log.Printf("⚠️ Failed to select %s: %v", selector, err)
				}
			}
		case "extract":
			if extractMap, ok := action["extract"].(map[string]interface{}); ok {
				for key, sel := range extractMap {
					if selStr, ok := sel.(string); ok {
						text, err := page.TextContent(selStr)
						if err == nil && text != "" {
							result["extracted"].(map[string]interface{})[key] = strings.TrimSpace(text)
						} else {
							// Try innerText as fallback
							text, err := page.InnerText(selStr)
							if err == nil && text != "" {
								result["extracted"].(map[string]interface{})[key] = strings.TrimSpace(text)
							}
						}
					}
				}
			}
		}
	}

	// Output JSON result
	jsonOutput, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to marshal result: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(string(jsonOutput))
	os.Stdout.Sync() // Force flush stdout before cleanup
	
	// Try to close cleanly with a timeout
	done := make(chan bool, 1)
	go func() {
		if page != nil {
			page.Close()
		}
		if browser != nil {
			browser.Close()
		}
		if pw != nil {
			pw.Stop()
		}
		done <- true
	}()
	
	// Wait up to 2 seconds for clean shutdown, then force exit
	select {
	case <-done:
		// Clean shutdown completed
	case <-time.After(2 * time.Second):
		// Timeout - force exit
		os.Exit(0)
	}
}
