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

func generateConsentBypassScript() string {
	return `// Try to find and click consent/accept buttons
try {
  console.log("Attempting consent bypass...");
  
  // Common button texts in multiple languages
  const acceptTexts = [
    /accept/i, /agree/i, /continue/i, /allow/i, /ok/i, /yes/i,
    /accepter/i, /continuer/i, /autoriser/i, /j'accepte/i, // French
    /akzeptieren/i, /zustimmen/i,              // German
    /aceptar/i, /continuar/i                   // Spanish
  ];
  
  let clicked = false;

  // 1. Try generic role-based buttons first
  for (const pattern of acceptTexts) {
    try {
      const btn = page.getByRole('button', { name: pattern }).first();
      if (await btn.count() > 0 && await btn.isVisible()) {
        await btn.click({ timeout: 2000, force: true });
        console.log('Clicked consent button with pattern:', pattern);
        clicked = true;
        break;
      }
    } catch (e) {}
  }
  
  // 2. If not clicked, try CSS selectors for specific platforms (Yahoo, Google, etc)
  if (!clicked) {
    const selectors = [
      'button[name="agree"]',           // Yahoo specific
      'button.accept-all',              // Yahoo/Generic
      'input[type="submit"][value*="Accept"]',
      'button[id*="accept"]',
      'button[class*="accept"]',
      'button[id*="agree"]',
      'button[class*="agree"]',
      'button[id*="continue"]',
      'button[class*="continue"]',
      'a[id*="accept"]',
      'a[class*="accept"]',
      'form[action*="consent"] input[type="submit"]' // Generic consent form submit
    ];
    
    for (const selector of selectors) {
      try {
        const el = page.locator(selector).first();
        if (await el.count() > 0 && await el.isVisible()) {
          await el.click({ timeout: 2000, force: true });
          console.log('Clicked consent element:', selector);
          clicked = true;
          break;
        }
      } catch (e) {}
    }
  }

  if (clicked) {
    // Wait for navigation or content update
    await page.waitForTimeout(5000);
    console.log('Waited 5s for navigation after consent click');
  } else {
    console.log('No consent button found to click');
  }

} catch (error) {
  console.log('Error in consent bypass:', error.message);
}`
}
