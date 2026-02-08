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

// generateConsentBypassScript generates TypeScript to click common consent buttons
func generateConsentBypassScript() string {
	return `// Try to find and click consent/accept buttons
try {
  // Common button texts in multiple languages
  const acceptTexts = [
    /accept/i, /agree/i, /continue/i, /allow/i, /ok/i,
    /accepter/i, /continuer/i, /autoriser/i,  // French
    /akzeptieren/i, /zustimmen/i,              // German
    /aceptar/i, /continuar/i                   // Spanish
  ];
  
  // Try each pattern
  for (const pattern of acceptTexts) {
    try {
      await page.getByRole('button', { name: pattern }).first().click({ timeout: 2000 });
      console.log('Clicked consent button with pattern:', pattern);
      await page.waitForTimeout(2000);
      break;
    } catch (e) {
      // Try next pattern
    }
  }
  
  // Also try common button IDs/classes
  const selectors = [
    'button[id*="accept"]',
    'button[class*="accept"]',
    'button[id*="agree"]',
    'button[class*="agree"]',
    'button[id*="continue"]',
    'button[class*="continue"]',
    'a[id*="accept"]',
    'a[class*="accept"]'
  ];
  
  for (const selector of selectors) {
    try {
      await page.locator(selector).first().click({ timeout: 1000 });
      console.log('Clicked consent element:', selector);
      await page.waitForTimeout(2000);
      break;
    } catch (e) {
      // Try next selector
    }
  }
} catch (error) {
  console.log('No consent button found or already accepted');
}`
}
