package main

import (
	"fmt"
	"strings"
	"regexp"
)

func main() {
	// SIMULATE search.go logic
	rootURL := "https://www.google.com/travel/flights?hl=en&gl=FR"
	searchURL := "https://www.google.com/travel/flights?q=GVA+to+LGW"
	
	actionScript := `
		// Toggle trip type to One Way
		await page.evaluate(async () => {
			const btns = Array.from(document.querySelectorAll('button'));
			const tripBtn = btns.find(b => b.textContent.includes('Round trip') || b.textContent.includes('Aller-retour') || b.textContent.includes('ida y vuelta'));
			if (tripBtn) {
				tripBtn.click();
				await new Promise(r => setTimeout(r, 1000));
				const items = Array.from(document.querySelectorAll('li[role="option"], li'));
				const oneWay = items.find(i => i.textContent.includes('One way') || i.textContent.includes('Aller simple') || i.textContent.includes('Solo ida'));
				if (oneWay) oneWay.click();
			}
		});
		await page.waitForTimeout(2000);
		`

	envScript := `
            await page.setViewportSize({ width: 2560, height: 1600 });
            await page.setUserAgent("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36");
            
            // 1. Initial load for consent
            await page.goto("%s");
            await page.waitForLoadState("networkidle");
            await page.bypassConsent();
            await page.waitForTimeout(2000); 

            // 2. Search URL
            await page.goto("%s&sort=price_asc");
            await page.waitForLoadState("networkidle");
            await page.bypassConsent();
            
            // Linear Action Block (Injected by Go)
            %s

            // 3. Extraction readiness
            await page.waitForSelector("div[role=listitem]", { timeout: 30000 });
            await page.evaluate(() => { window.scrollBy(0, 1000); });
            await page.waitForTimeout(1000);
`

	tsConfig := fmt.Sprintf(envScript, rootURL, searchURL, actionScript)

	fmt.Println("=== GENERATED TSCONFIG ===")
	// fmt.Println(tsConfig)

	// SIMULATE engine.go parser
	// Regex: (?s)(?:await\s+)?page\.evaluate\(\s*\(.*?\)\s*=>\s*(?:\{([\s\S]+)\}|([\s\S]+?))\s*\)
	re := regexp.MustCompile(`(?s)(?:await\s+)?page\.evaluate\(\s*\(.*?\)\s*=>\s*(?:\{([\s\S]+)\}|([\s\S]+?))\s*\)`)
	
	lines := strings.Split(tsConfig, "\n")
	foundEvaluate := false
	for i, line := range lines {
		// Note: The actual parser in Go might be line-by-line or on the whole block
		// But let's check if the regex captures the block when presented with the WHOLE tsConfig
	}

	fmt.Println("\n=== PARSER TEST (WHOLE BLOCK) ===")
	matches := re.FindAllStringSubmatch(tsConfig, -1)
	fmt.Printf("Found %d evaluate calls\n", len(matches))
	for i, m := range matches {
		script := m[1]
		if script == "" { script = m[2] }
		fmt.Printf("\n--- Match %d ---\n", i+1)
		// fmt.Println(script)
		if strings.Contains(script, "tripBtn.click()") {
			fmt.Println("✅ SUCCESS: Toggle logic found in Match", i+1)
			if strings.HasSuffix(strings.TrimSpace(script), "}") {
				fmt.Println("✅ SUCCESS: Greedily captured trailing brace")
			} else {
				fmt.Println("❌ FAILURE: Cut off early!")
			}
		}
	}
}
