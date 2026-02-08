package main

import (
	"log"
	"strings"
)

// isConsentPage detects if the HTML content is a consent/cookie page
func isConsentPage(html string) bool {
	htmlLower := strings.ToLower(html)

	// Check for common consent page indicators
	consentKeywords := []string{
		"cookie consent",
		"privacy consent",
		"accept cookies",
		"accept all",
		"manage cookies",
		"cookie settings",
		"privacy settings",
		"before you continue",
		"paramètres de confidentialité", // French: privacy settings
		"vos paramètres",                // French: your settings
		"consent.google",
		"consent.yahoo",
		"gdpr",
		"cookie policy",
	}

	matchCount := 0
	var matchedKeywords []string
	for _, keyword := range consentKeywords {
		if strings.Contains(htmlLower, keyword) {
			matchCount++
			matchedKeywords = append(matchedKeywords, keyword)
		}
	}

	// If we find 2 or more consent-related keywords, it's likely a consent page
	isConsent := matchCount >= 2
	if matchCount > 0 {
		log.Printf("🔍 [CONSENT-DETECT] Found %d consent keywords: %v (threshold: 2, is_consent: %v)",
			matchCount, matchedKeywords, isConsent)
	}
	return isConsent
}

// generateConsentBypassScript generates the native bypassConsent command
func generateConsentBypassScript() string {
	return "await page.bypassConsent()"
}
