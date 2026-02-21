// Package scraper_pkg provides generic web scraping capabilities
package scraper_pkg

import (
	"context"
	"encoding/base64"
	"fmt"
	"regexp"
	"strings"
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
}

// ScrapeResult represents the result of a scrape operation
type ScrapeResult struct {
	Status        string                 `json:"status"`
	URL           string                 `json:"url"`
	Title         string                 `json:"title"`
	Data          map[string]interface{} `json:"data"`
	HTML          string                 `json:"html,omitempty"`
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

	// Get title
	title, err := page.Title()
	if err == nil {
		result.Title = title
	}

	// Execute extractions
	result.Data = make(map[string]interface{})

	// If instructions provided, use smart extraction
	if req.Instructions != "" {
		gs.logger.Printf("🔍 Extracting based on instructions: %s", req.Instructions)
		extracted, err := gs.smartExtract(ctx, page, req.Instructions)
		if err == nil {
			result.Data = extracted
		}
	}

	// If explicit extractions provided, use those
	if len(req.Extractions) > 0 {
		gs.logger.Printf("📋 Applying %d extraction rules", len(req.Extractions))
		for key, selector := range req.Extractions {
			gs.logger.Printf("  → %s: %s", key, selector)
			value, err := gs.extractBySelector(ctx, page, selector)
			if err == nil {
				result.Data[key] = value
			}
		}
	}

	// Get HTML if requested
	if req.GetHTML {
		html, err := page.Content()
		if err == nil {
			result.HTML = html
		}
	}

	// Take screenshot
	screenshot, err := page.Screenshot(pw.PageScreenshotOptions{})
	if err == nil && screenshot != nil {
		result.ScreenshotB64 = "data:image/png;base64," + base64.StdEncoding.EncodeToString(screenshot)
	}

	gs.logger.Printf("✅ Scrape completed: %d extractions", len(result.Data))
	return result, nil
}

// smartExtract uses heuristic patterns to extract data based on instructions
func (gs *GenericScraper) smartExtract(ctx context.Context, page pw.Page, instructions string) (map[string]interface{}, error) {
	data := make(map[string]interface{})

	// Get page content
	content, err := page.Content()
	if err != nil {
		return data, err
	}

	// Common extraction patterns
	patterns := gs.generatePatterns(instructions)

	for key, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		matches := re.FindAllString(content, -1)

		if len(matches) > 0 {
			if len(matches) == 1 {
				data[key] = matches[0]
			} else {
				data[key] = matches
			}
		}
	}

	// Try CSS selector-based extraction
	gs.extractFromCommonSelectors(ctx, page, instructions, data)

	return data, nil
}

// generatePatterns creates regex patterns based on instructions
func (gs *GenericScraper) generatePatterns(instructions string) map[string]string {
	patterns := make(map[string]string)

	// Common extraction patterns
	if strings.Contains(strings.ToLower(instructions), "price") {
		patterns["prices"] = `\$?\d+\.?\d*|\€\d+\.?\d*|£\d+\.?\d*`
	}

	if strings.Contains(strings.ToLower(instructions), "email") {
		patterns["emails"] = `[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}`
	}

	if strings.Contains(strings.ToLower(instructions), "phone") {
		patterns["phones"] = `\+?1?\d{9,15}`
	}

	if strings.Contains(strings.ToLower(instructions), "date") {
		patterns["dates"] = `\d{1,2}[/-]\d{1,2}[/-]\d{2,4}|\d{4}-\d{2}-\d{2}`
	}

	if strings.Contains(strings.ToLower(instructions), "link") {
		patterns["links"] = `https?://[^\s\)<>]+`
	}

	if strings.Contains(strings.ToLower(instructions), "time") {
		patterns["times"] = `\d{1,2}:\d{2}(?::\d{2})?(?:\s?(?:AM|PM|am|pm))?`
	}

	return patterns
}

// extractFromCommonSelectors tries common CSS selectors
func (gs *GenericScraper) extractFromCommonSelectors(ctx context.Context, page pw.Page, instructions string, data map[string]interface{}) {
	commonSelectors := map[string]string{
		"headings":   "h1, h2, h3, h4, h5, h6",
		"paragraphs": "p",
		"links":      "a",
		"images":     "img",
		"buttons":    "button",
		"titles":     "title, .title, .heading",
		"products":   "[class*='product'], [id*='product']",
		"items":      "[class*='item'], .list-item",
		"cards":      "[class*='card']",
	}

	for key, selector := range commonSelectors {
		// Skip if already extracted
		if _, exists := data[key]; exists {
			continue
		}

		// Only extract if mentioned in instructions
		if !strings.Contains(strings.ToLower(instructions), strings.ToLower(key)) {
			continue
		}

		values := []string{}
		locators := page.Locator(selector)
		count, err := locators.Count()
		if err != nil || count == 0 {
			continue
		}

		for i := 0; i < count && i < 10; i++ { // Limit to 10 items
			locator := locators.Nth(i)
			text, err := locator.TextContent()
			if err == nil && strings.TrimSpace(text) != "" {
				values = append(values, strings.TrimSpace(text))
			}
		}

		if len(values) > 0 {
			if len(values) == 1 {
				data[key] = values[0]
			} else {
				data[key] = values
			}
		}
	}
}

// extractBySelector extracts content using a CSS selector
func (gs *GenericScraper) extractBySelector(ctx context.Context, page pw.Page, selector string) (interface{}, error) {
	// Try as CSS selector first
	locators := page.Locator(selector)
	count, err := locators.Count()

	if err == nil && count > 0 {
		values := []string{}

		for i := 0; i < count && i < 100; i++ { // Limit to 100 items
			locator := locators.Nth(i)

			// Try to get text content first
			text, err := locator.TextContent()
			if err == nil && strings.TrimSpace(text) != "" {
				values = append(values, strings.TrimSpace(text))
				continue
			}

			// Try attribute extraction (value, href, src, etc.)
			for _, attr := range []string{"value", "href", "src", "alt", "title"} {
				val, err := locator.GetAttribute(attr)
				if err == nil && val != "" {
					values = append(values, val)
					break
				}
			}
		}

		if len(values) == 0 {
			return nil, fmt.Errorf("no content found for selector")
		}

		if len(values) == 1 {
			return values[0], nil
		}

		return values, nil
	}

	// Try as regex pattern
	content, err := page.Content()
	if err != nil {
		return nil, err
	}

	re := regexp.MustCompile(selector)
	matches := re.FindStringSubmatch(content)

	if len(matches) > 1 {
		return matches[1], nil
	}

	if len(matches) > 0 {
		return matches[0], nil
	}

	return nil, fmt.Errorf("no match found for pattern")
}
