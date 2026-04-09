package scraper_pkg

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	pw "github.com/playwright-community/playwright-go"
)

// Extract performs extractions based on instructions and explicit rules
func Extract(ctx context.Context, page pw.Page, instructions string, extractions map[string]string, logger Logger) map[string]interface{} {
	data := make(map[string]interface{})

	// 1. If instructions provided, use smart extraction
	if instructions != "" {
		logger.Printf("🔍 Extracting based on instructions: %s", instructions)
		smartData, err := SmartExtract(ctx, page, instructions, logger)
		if err == nil {
			for k, v := range smartData {
				data[k] = v
			}
		}
	}

	// 2. If explicit extractions provided, use those
	if len(extractions) > 0 {
		logger.Printf("📋 Applying %d extraction rules", len(extractions))
		for key, selector := range extractions {
			logger.Printf("  → %s: %s", key, selector)
			value, err := ExtractBySelector(ctx, page, selector)
			if err == nil {
				data[key] = value
			}
		}
	}

	return data
}

// SmartExtract uses heuristic patterns to extract data based on instructions
func SmartExtract(ctx context.Context, page pw.Page, instructions string, logger Logger) (map[string]interface{}, error) {
	data := make(map[string]interface{})

	// Get page content
	content, err := page.Content()
	if err != nil {
		return data, err
	}

	// Common extraction patterns
	patterns := GeneratePatterns(instructions)

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
	ExtractFromCommonSelectors(ctx, page, instructions, data, logger)

	return data, nil
}

// GeneratePatterns creates regex patterns based on instructions
func GeneratePatterns(instructions string) map[string]string {
	patterns := make(map[string]string)

	// Common extraction patterns
	if strings.Contains(strings.ToLower(instructions), "price") {
		patterns["prices"] = `(?i)(?:€|\$|£|GBP|EUR|USD)\s*\d{1,3}(?:[.,\s]\d{3})*[.,]\d{2}|\d{1,3}(?:[.,\s]\d{3})*[.,]\d{2}\s*(?:€|\$|£|EUR|GBP|USD)`
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

// ExtractFromCommonSelectors tries common CSS selectors
func ExtractFromCommonSelectors(ctx context.Context, page pw.Page, instructions string, data map[string]interface{}, logger Logger) {
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

	// If instructions mention "price", try semantic/structured price selectors first
	if strings.Contains(strings.ToLower(instructions), "price") {
		if _, exists := data["prices"]; !exists {
			semanticPriceSelectors := []string{
				`[itemprop="price"]`,
				`[itemprop="lowPrice"]`,
				`[data-price]`,
				`[data-product-price]`,
				`meta[property="product:price:amount"]`,
				`meta[property="og:price:amount"]`,
				`.price .current`,
				`.price-current`,
				`.product-price`,
				`.sale-price`,
				`.offer-price`,
				`.final-price`,
			}
			for _, sel := range semanticPriceSelectors {
				loc := page.Locator(sel).First()
				if cnt, err := loc.Count(); err == nil && cnt > 0 {
					// Try content/data attributes first (meta tags, microdata)
					if val, err := loc.GetAttribute("content"); err == nil && val != "" {
						data["prices"] = val
						break
					}
					if val, err := loc.GetAttribute("data-price"); err == nil && val != "" {
						data["prices"] = val
						break
					}
					if val, err := loc.GetAttribute("data-product-price"); err == nil && val != "" {
						data["prices"] = val
						break
					}
					if text, err := loc.TextContent(); err == nil && strings.TrimSpace(text) != "" {
						data["prices"] = strings.TrimSpace(text)
						break
					}
				}
			}
		}
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

// ExtractBySelector extracts content using a CSS selector or regex
func ExtractBySelector(ctx context.Context, page pw.Page, selector string) (interface{}, error) {
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

	// Escape regex if needed or assume it's valid
	re, err := regexp.Compile(selector)
	if err != nil {
		return nil, err
	}
	
	matches := re.FindStringSubmatch(content)

	if len(matches) > 1 {
		return matches[1], nil
	}

	if len(matches) > 0 {
		return matches[0], nil
	}

	return nil, fmt.Errorf("no match found for pattern")
}

// CleanHTML removes scripts, styles, and other non-content elements to make HTML LLM-friendly
func CleanHTML(html string) string {
	if html == "" {
		return ""
	}

	// Remove scripts
	reScripts := regexp.MustCompile(`(?i)<script[\s\S]*?</script>`)
	cleaned := reScripts.ReplaceAllString(html, "")

	// Remove styles
	reStyles := regexp.MustCompile(`(?i)<style[\s\S]*?</style>`)
	cleaned = reStyles.ReplaceAllString(cleaned, "")

	// Remove comments
	reComments := regexp.MustCompile(`<!--[\s\S]*?-->`)
	cleaned = reComments.ReplaceAllString(cleaned, "")

	// Remove SVG elements
	reSVG := regexp.MustCompile(`(?i)<svg[\s\S]*?</svg>`)
	cleaned = reSVG.ReplaceAllString(cleaned, "")

	// Remove multiple newlines
	reNewlines := regexp.MustCompile(`\n\s*\n`)
	cleaned = reNewlines.ReplaceAllString(cleaned, "\n")

	return strings.TrimSpace(cleaned)
}
