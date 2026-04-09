// Package scraper_pkg provides generic web scraping capabilities
package scraper_pkg

import (
	"context"
	"encoding/base64"
	"fmt"
	"time"

	pw "github.com/playwright-community/playwright-go"
)

// GenericScrapeRequest represents a request to scrape any website
type GenericScrapeRequest struct {
	URL              string            `json:"url"`
	Instructions     string            `json:"instructions"`                // Natural language: "Get all product names and prices"
	Extractions      map[string]string `json:"extractions,omitempty"`       // CSS selectors or regex patterns
	WaitTime         int               `json:"wait_time,omitempty"`         // milliseconds to wait after navigation
	Clicks           []string          `json:"clicks,omitempty"`            // Selectors to click before scraping
	GetHTML          bool              `json:"get_html,omitempty"`          // Return raw HTML
	TypeScriptConfig string            `json:"typescript_config,omitempty"` // Playwright script to run
	Variables        map[string]string `json:"variables,omitempty"`         // Variables for the script
	ScreenshotPath   string            `json:"screenshot_path,omitempty"`   // Save screenshot to this path
}

// ScrapeResult represents the result of a scrape operation
type ScrapeResult struct {
	Status        string                 `json:"status"`
	URL           string                 `json:"url"`
	Title         string                 `json:"title"`
	Data          map[string]interface{} `json:"data"`
	HTML          string                 `json:"html,omitempty"`
	CleanedHTML   string                 `json:"cleaned_html,omitempty"`
	ExtractedAt   string                 `json:"extracted_at"`
	ExecutionMs   int                    `json:"execution_time_ms"`
	Error         string                 `json:"error,omitempty"`
	ScreenshotB64 string                 `json:"screenshot,omitempty"`
}

// GenericScraper handles scraping of arbitrary websites
type GenericScraper struct {
	browser pw.Browser
	logger  Logger
}

// NewGenericScraper creates a new generic scraper
func NewGenericScraper(browser pw.Browser, logger Logger) *GenericScraper {
	return &GenericScraper{
		browser: browser,
		logger:  logger,
	}
}

// Scrape performs a generic web scrape based on the request
func (gs *GenericScraper) Scrape(ctx context.Context, req GenericScrapeRequest) (*ScrapeResult, error) {
	startTime := time.Now()
	result := &ScrapeResult{
		URL:         req.URL,
		ExtractedAt: time.Now().Format(time.RFC3339),
		Status:      "success",
	}

	defer func() {
		result.ExecutionMs = int(time.Since(startTime).Milliseconds())
	}()

	// Create context with timeout
	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
	}

	// Create page
	page, err := gs.browser.NewPage()
	if err != nil {
		gs.logger.Errorf("Failed to create page: %v", err)
		result.Status = "error"
		result.Error = fmt.Sprintf("page creation failed: %v", err)
		return result, err
	}
	defer page.Close()

	gs.logger.Printf("📄 Navigating to: %s", req.URL)

	// Navigate to URL
	if _, err := page.Goto(req.URL, pw.PageGotoOptions{
		WaitUntil: pw.WaitUntilStateNetworkidle,
		Timeout:   pw.Float(30000),
	}); err != nil {
		gs.logger.Errorf("Navigation failed: %v", err)
		result.Status = "error"
		result.Error = fmt.Sprintf("navigation failed: %v", err)
		return result, err
	}

	gs.logger.Printf("✅ Page loaded")

	// Wait for dynamic content if specified
	if req.WaitTime > 0 {
		gs.logger.Printf("⏳ Waiting %dms for dynamic content", req.WaitTime)
		page.WaitForTimeout(float64(req.WaitTime))
	}

	// Execute clicks if specified
	for _, selector := range req.Clicks {
		gs.logger.Printf("🖱️  Clicking: %s", selector)
		if err := page.Click(selector); err != nil {
			gs.logger.Printf("⚠️  Click failed (non-fatal): %v", err)
		}
		time.Sleep(500 * time.Millisecond)
	}

	// Execute TypeScript configuration if provided
	if req.TypeScriptConfig != "" {
		gs.logger.Printf("🚀 Executing TypeScript script...")
		// Interpolate variables into script
		script := ApplyTemplateVariables(req.TypeScriptConfig, req.Variables)
		
		// Parse operations
		ops, err := ParseTypeScriptConfig(script)
		if err != nil {
			gs.logger.Errorf("Failed to parse script: %v", err)
			result.Status = "error"
			result.Error = fmt.Sprintf("script parsing failed: %v", err)
			return result, err
		}

		// Execute operations on the current page
		if err := ExecuteEngine(page, ops, gs.logger); err != nil {
			gs.logger.Errorf("Script execution failed: %v", err)
			// Non-fatal if some operations fail, but log it
		}
	}

	// 1. Get title
	title, err := page.Title()
	if err == nil {
		result.Title = title
	}

	// 2. Execute extractions
	result.Data = Extract(ctx, page, req.Instructions, req.Extractions, gs.logger)

	// Get HTML if requested
	if req.GetHTML {
		html, err := page.Content()
		if err == nil {
			result.HTML = html
			result.CleanedHTML = CleanHTML(html)
		}
	}

	// Take screenshot
	var screenshot []byte
	screenshotOptions := pw.PageScreenshotOptions{}
	if req.ScreenshotPath != "" {
		screenshotOptions.Path = pw.String(req.ScreenshotPath)
		gs.logger.Printf("📸 Saving screenshot to: %s", req.ScreenshotPath)
	}

	screenshot, err = page.Screenshot(screenshotOptions)
	if err == nil && screenshot != nil {
		// Only return base64 if NO screenshot path was provided (reduce overhead)
		if req.ScreenshotPath == "" {
			result.ScreenshotB64 = "data:image/png;base64," + base64.StdEncoding.EncodeToString(screenshot)
		} else {
			gs.logger.Printf("✅ Screenshot saved to path; skipping base64 encoding as requested.")
		}
	}

	gs.logger.Printf("✅ Scrape completed: %d extractions", len(result.Data))
	return result, nil
}
