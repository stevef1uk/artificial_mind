package main

import (
	"regexp"
	"strings"
)

// cleanHTMLForDisplay removes HTML tags and extracts readable text
// This is a simple fallback when html_scraper binary is not available
func cleanHTMLForDisplay(html string) string {
	// Remove script and style tags with their content
	scriptRe := regexp.MustCompile(`(?i)<script[^>]*>.*?</script>`)
	html = scriptRe.ReplaceAllString(html, "")

	styleRe := regexp.MustCompile(`(?i)<style[^>]*>.*?</style>`)
	html = styleRe.ReplaceAllString(html, "")

	// Remove HTML comments
	commentRe := regexp.MustCompile(`<!--.*?-->`)
	html = commentRe.ReplaceAllString(html, "")

	// Extract text from common content tags
	// Replace heading tags with markdown-style headings
	h1Re := regexp.MustCompile(`(?i)<h1[^>]*>(.*?)</h1>`)
	html = h1Re.ReplaceAllString(html, "\n# $1\n")

	h2Re := regexp.MustCompile(`(?i)<h2[^>]*>(.*?)</h2>`)
	html = h2Re.ReplaceAllString(html, "\n## $1\n")

	h3Re := regexp.MustCompile(`(?i)<h3[^>]*>(.*?)</h3>`)
	html = h3Re.ReplaceAllString(html, "\n### $1\n")

	// Replace <p> tags with newlines
	pRe := regexp.MustCompile(`(?i)<p[^>]*>(.*?)</p>`)
	html = pRe.ReplaceAllString(html, "\n$1\n")

	// Replace <br> tags with newlines
	brRe := regexp.MustCompile(`(?i)<br\s*/?>`)
	html = brRe.ReplaceAllString(html, "\n")

	// Replace <a> tags but keep the text
	aRe := regexp.MustCompile(`(?i)<a[^>]*>(.*?)</a>`)
	html = aRe.ReplaceAllString(html, "$1")

	// Remove all remaining HTML tags
	tagRe := regexp.MustCompile(`<[^>]*>`)
	html = tagRe.ReplaceAllString(html, "")

	// Decode common HTML entities
	html = strings.ReplaceAll(html, "&nbsp;", " ")
	html = strings.ReplaceAll(html, "&amp;", "&")
	html = strings.ReplaceAll(html, "&lt;", "<")
	html = strings.ReplaceAll(html, "&gt;", ">")
	html = strings.ReplaceAll(html, "&quot;", "\"")
	html = strings.ReplaceAll(html, "&#39;", "'")

	// Clean up excessive whitespace
	// Replace multiple spaces with single space
	spaceRe := regexp.MustCompile(`[ \t]+`)
	html = spaceRe.ReplaceAllString(html, " ")

	// Replace multiple newlines with double newline
	newlineRe := regexp.MustCompile(`\n{3,}`)
	html = newlineRe.ReplaceAllString(html, "\n\n")

	// Trim leading/trailing whitespace
	html = strings.TrimSpace(html)

	// Limit to reasonable size (first 10000 characters)
	if len(html) > 10000 {
		html = html[:10000] + "\n\n... (content truncated for readability)"
	}

	return html
}
