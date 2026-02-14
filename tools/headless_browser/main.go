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
	screenshot := flag.String("screenshot", "", "Path to save screenshot")
	fastMode := flag.Bool("fast", false, "Use fast mode (skip some waits)")
	lastOnly := flag.Bool("last-only", false, "Only take screenshot after the last action")
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

	// Create new page with desktop viewport
	page, err := browser.NewPage(playwright.BrowserNewPageOptions{
		Viewport: &playwright.Size{
			Width:  1280,
			Height: 800,
		},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create page: %v\n", err)
		os.Exit(1)
	}

	// AD-BLOCKING: Skip tracking and heavy scripts to speed up loading
	_ = page.Route("**/*", func(route playwright.Route) {
		req := route.Request()
		url := strings.ToLower(req.URL())
		if strings.Contains(url, "google-analytics") ||
			strings.Contains(url, "googletagmanager") ||
			strings.Contains(url, "facebook.net") ||
			strings.Contains(url, "doubleclick") ||
			strings.Contains(url, "amazon-adsystem") ||
			strings.Contains(url, "hotjar") ||
			strings.Contains(url, "sentry.io") {
			_ = route.Abort("blockedbyclient")
		} else {
			_ = route.Continue()
		}
	})
	// NOTE: Don't defer page.Close() - can hang! We'll os.Exit(0) instead

	// Set timeout
	page.SetDefaultTimeout(float64(*timeout) * 1000)

	// Navigate
	if _, err := page.Goto(*url, playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateDomcontentloaded,
		Timeout:   playwright.Float(float64(*timeout) * 1000),
	}); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to navigate to %s: %v\n", *url, err)
		os.Exit(1)
	}

	// Ensure minimum stability
	page.WaitForLoadState(playwright.PageWaitForLoadStateOptions{
		State: playwright.LoadStateDomcontentloaded,
	})

	if !*fastMode {
		time.Sleep(1 * time.Second)
		page.WaitForLoadState(playwright.PageWaitForLoadStateOptions{
			State: playwright.LoadStateNetworkidle,
		})
	} else {
		// Even in fast mode, give the JS a moment to initialize
		time.Sleep(200 * time.Millisecond)
	}

	if *returnHTML {
		// Capture content
		html, err := page.Content()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to get HTML: %v\n", err)
			os.Exit(1)
		}
		// If screenshot requested even in HTML mode (e.g. for debug), take it
		if *screenshot != "" {
			saveScreenshot(page, *screenshot, true)
			saveProgress(*screenshot, "Initial page loaded", 0, 0, html)
		}
		// Wrap in JSON for cleaner parsing
		response := map[string]interface{}{
			"success": true,
			"html":    html,
		}
		jsonBytes, err := json.Marshal(response)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to marshal HTML response: %v\n", err)
			os.Exit(1)
		}
		fmt.Print(string(jsonBytes))
		os.Exit(0)
	}

	result := map[string]interface{}{
		"url":       *url,
		"success":   true,
		"extracted": make(map[string]interface{}),
	}

	title, err := page.Title()
	if err == nil {
		result["title"] = title
	}

	// Helper to inject highlight and take pre-action screenshot
	prepareAction := func(page playwright.Page, sel string, isLastAction bool, screenshotPath string, lastOnly bool, step int, total int, actionType string) {
		if sel == "" {
			return
		}

		// 1. Inject highlight CSS into main frame (safe, global style)
		_, _ = page.Evaluate(`() => {
			if (!document.getElementById('hdn-highlight-style')) {
				const s = document.createElement('style');
				s.id = 'hdn-highlight-style';
				s.innerHTML = '.hdn-active-target { outline: 12px solid #ff0000 !important; outline-offset: -2px !important; box-shadow: 0 0 40px 10px rgba(255,0,0,0.9) !important; z-index: 9999999 !important; position: relative !important; animation: hdn-pulse 0.5s infinite alternate !important; } @keyframes hdn-pulse { from { outline-color: #ff0000; box-shadow: 0 0 20px 5px rgba(255,0,0,0.5); } to { outline-color: #ff5555; box-shadow: 0 0 50px 15px rgba(255,0,0,1); } }';
				document.head.appendChild(s);
			}
			// Clean up previous highlights in main frame
			document.querySelectorAll('.hdn-active-target').forEach(e => e.classList.remove('hdn-active-target'));
		}`)

		// 2. Apply highlight to target element using Locator (handles frames)
		loc := resolveLocator(page, sel)
		if loc != nil {
			// We try to add the class. Note: If it's in a frame, the CSS style from main frame might not apply
			// unless we also inject style into that frame.
			// Best effort: Inject style into this specific frame if needed context is accessible.
			_, _ = loc.Evaluate(`el => {
				const doc = el.ownerDocument;
				if (doc && !doc.getElementById('hdn-highlight-style-frame')) {
					const s = doc.createElement('style');
					s.id = 'hdn-highlight-style-frame';
					s.innerHTML = '.hdn-active-target { outline: 12px solid #ff0000 !important; outline-offset: -2px !important; box-shadow: 0 0 40px 10px rgba(255,0,0,0.9) !important; z-index: 9999999 !important; position: relative !important; animation: hdn-pulse 0.5s infinite alternate !important; } @keyframes hdn-pulse { from { outline-color: #ff0000; box-shadow: 0 0 20px 5px rgba(255,0,0,0.5); } to { outline-color: #ff5555; box-shadow: 0 0 50px 15px rgba(255,0,0,1); } }';
					if (doc.head) doc.head.appendChild(s);
				}
				// Remove other highlights in this frame
				doc.querySelectorAll('.hdn-active-target').forEach(e => e.classList.remove('hdn-active-target'));
				
				el.classList.add('hdn-active-target'); 
				el.scrollIntoView({block:'center', inline:'center'});
			}`, nil)
		}

		// 3. Small sleep to ensure highlight renders
		time.Sleep(100 * time.Millisecond)

		// 4. Capture "Intent" screenshot (only if last or not in lastOnly mode)
		if screenshotPath != "" && (isLastAction || !lastOnly) {
			saveScreenshot(page, screenshotPath, isLastAction)
			// Update progress with ACTIVE step info
			html, _ := page.Content()
			saveProgress(screenshotPath, fmt.Sprintf("Step %d/%d (Preparing): %s %s", step, total, actionType, sel), step, total, html)
			// Longer sleep to ensure the highlight is actually painted and captured
			time.Sleep(300 * time.Millisecond)
		}
	}

	// Set a per-action timeout based on provided flag (default to flag value)
	page.SetDefaultTimeout(float64(*timeout) * 1000)

	// Execute actions
	var actionErrors []string
	for i, action := range actions {
		actionType, _ := action["type"].(string)
		selector, _ := action["selector"].(string)
		isLastAction := i == len(actions)-1
		var actionErr error

		// SILENT REPLAY MODE: If not the last action and lastOnly is requested, make it fast and invisible
		isReplay := !isLastAction && *lastOnly

		// Write progress at start of every step so UI updates even during slow actions
		if *screenshot != "" && !isReplay {
			html, _ := page.Content()
			saveProgress(*screenshot, fmt.Sprintf("Step %d/%d: starting %s...", i+1, len(actions), actionType), i+1, len(actions), html)
		}

		switch actionType {
		case "fill":
			value, _ := action["value"].(string)
			if selector != "" && value != "" {
				if !isReplay {
					prepareAction(page, selector, isLastAction, *screenshot, *lastOnly, i+1, len(actions), "fill")
				}
				log.Printf("⌨️ Filling %s with '%s'...", selector, value)
				targetLoc := resolveLocator(page, selector)
				if targetLoc != nil {
					// RESILIENCE: Click first to ensure focus
					_ = targetLoc.Click(playwright.LocatorClickOptions{Timeout: playwright.Float(2000)})
					if err := targetLoc.Fill(value); err != nil {
						actionErr = err
					} else if screenshot != nil && *screenshot != "" {
						// TAKE OUTCOME SCREENSHOT
						saveScreenshot(page, *screenshot, isLastAction)
						currentHTML, _ := page.Content()
						saveProgress(*screenshot, fmt.Sprintf("Step %d/%d (Done): Filled %s", i+1, len(actions), selector), i+1, len(actions), currentHTML)
					}
				} else {
					actionErr = fmt.Errorf("selector %s not found in any frame", selector)
				}
			}
		case "type":
			value, _ := action["value"].(string)
			if selector != "" && value != "" {
				if !isReplay {
					prepareAction(page, selector, isLastAction, *screenshot, *lastOnly, i+1, len(actions), "type")
				}
				log.Printf("⌨️ Typing '%s' into %s...", value, selector)
				targetLoc := resolveLocator(page, selector)
				if targetLoc != nil {
					// RESILIENCE: Click first to ensure focus
					_ = targetLoc.Click(playwright.LocatorClickOptions{Timeout: playwright.Float(2000)})
					if err := targetLoc.Type(value, playwright.LocatorTypeOptions{Delay: playwright.Float(100)}); err != nil {
						actionErr = err
					} else if screenshot != nil && *screenshot != "" {
						// TAKE OUTCOME SCREENSHOT
						saveScreenshot(page, *screenshot, isLastAction)
						currentHTML, _ := page.Content()
						saveProgress(*screenshot, fmt.Sprintf("Step %d/%d (Done): Typed into %s", i+1, len(actions), selector), i+1, len(actions), currentHTML)
					}
					// Mandatory settlement for dynamic forms
					time.Sleep(300 * time.Millisecond)
				} else {
					actionErr = fmt.Errorf("selector %s not found in any frame for typing", selector)
				}
			}
		case "press":
			key, _ := action["key"].(string)
			if key != "" {
				// Normalize key names for Playwright
				normalizedKey := key
				switch strings.ToLower(key) {
				case "down":
					normalizedKey = "ArrowDown"
				case "up":
					normalizedKey = "ArrowUp"
				case "left":
					normalizedKey = "ArrowLeft"
				case "right":
					normalizedKey = "ArrowRight"
				case "return":
					normalizedKey = "Enter"
				}

				log.Printf("⌨️ Pressing key %s (normalized: %s)...", key, normalizedKey)
				if *screenshot != "" && !isReplay {
					html, _ := page.Content()
					saveProgress(*screenshot, fmt.Sprintf("Step %d/%d: pressing %s...", i+1, len(actions), normalizedKey), i+1, len(actions), html)
				}
				if err := page.Keyboard().Press(normalizedKey); err != nil {
					actionErr = err
				}
				// Mandatory settlement
				time.Sleep(500 * time.Millisecond)
			}
		case "click":
			if selector != "" {
				if !isReplay {
					prepareAction(page, selector, isLastAction, *screenshot, *lastOnly, i+1, len(actions), "click")
				}
				log.Printf("🖱️ Clicking %s...", selector)
				targetLoc := resolveLocator(page, selector)
				if targetLoc != nil {
					if err := targetLoc.Click(); err != nil {
						actionErr = err
					}

					if screenshot != nil && *screenshot != "" {
						// TAKE OUTCOME SCREENSHOT
						saveScreenshot(page, *screenshot, isLastAction)
						currentHTML, _ := page.Content()
						saveProgress(*screenshot, fmt.Sprintf("Step %d/%d (Done): Clicked %s", i+1, len(actions), selector), i+1, len(actions), currentHTML)
					}

					// Mandatory settlement
					time.Sleep(300 * time.Millisecond)
				} else {
					// FUZZY FALLBACK: Try to find by text if the selector looks like a label (e.g. "Calculate")
					log.Printf("   🔍 Selector not found, trying fuzzy text search for: %s", selector)
					fuzzyScript := `(text) => {
						const targets = Array.from(document.querySelectorAll('button, a, input[type="button"], input[type="submit"], [role="button"]'));
						const match = targets.find(el => el.innerText.toLowerCase().includes(text.toLowerCase()) || el.value?.toLowerCase().includes(text.toLowerCase()));
						if (match) {
							match.click();
							return true;
						}
						return false;
					}`
					success, _ := page.Evaluate(fuzzyScript, strings.Trim(selector, "#."))
					if b, ok := success.(bool); ok && b {
						log.Printf("   ✨ Fuzzy text click worked for: %s", selector)
						time.Sleep(500 * time.Millisecond)
					} else {
						// RESILIENCE: If we already selected via Enter (in Fill), this click might fail.
						// If it's an autocomplete option, ignore the error.
						if strings.Contains(selector, "autocomplete-option") || strings.Contains(selector, "option") {
							log.Printf("   ⚠️ Warning: Click on %s failed (likely already selected/hidden)", selector)
						} else {
							actionErr = fmt.Errorf("selector %s not found in any frame for clicking", selector)
						}
					}
				}
			}
		case "wait":
			if duration, ok := action["duration"]; ok {
				var dur float64
				switch v := duration.(type) {
				case float64:
					dur = v
				case int:
					dur = float64(v)
				case int64:
					dur = float64(v)
				}
				if dur > 0 {
					log.Printf("⏳ Waiting for %v seconds as requested...", dur)
					if *screenshot != "" && !isReplay {
						html, _ := page.Content()
						saveProgress(*screenshot, fmt.Sprintf("Step %d/%d: waiting %.0fs...", i+1, len(actions), dur), i+1, len(actions), html)
					}
					time.Sleep(time.Duration(dur*1000) * time.Millisecond)
				}
			} else if selector != "" {
				// Wait for selector
				log.Printf("⏳ Waiting for %s...", selector)
				if *screenshot != "" && !isReplay {
					html, _ := page.Content()
					saveProgress(*screenshot, fmt.Sprintf("Step %d/%d: waiting for %s...", i+1, len(actions), selector), i+1, len(actions), html)
				}
				if _, err := page.WaitForSelector(selector, playwright.PageWaitForSelectorOptions{Timeout: playwright.Float(5000)}); err != nil {
					log.Printf("⚠️ Wait for selector %s timed out", selector)
				}
			}
		case "select":
			value, _ := action["value"].(string)
			if selector != "" && value != "" {
				if !isReplay {
					prepareAction(page, selector, isLastAction, *screenshot, *lastOnly, i+1, len(actions), "select")
				}
				performResilientSelect(page, selector, value)
				if screenshot != nil && *screenshot != "" {
					saveScreenshot(page, *screenshot, isLastAction)
					currentHTML, _ := page.Content()
					saveProgress(*screenshot, fmt.Sprintf("Step %d/%d (Done): Selected %s in %s", i+1, len(actions), value, selector), i+1, len(actions), currentHTML)
				}
				// Mandatory settlement
				time.Sleep(300 * time.Millisecond)
			}
		case "extract":
			log.Printf("🔍 Extraction requested for %s, will capture in final HTML...", selector)
			if !isReplay {
				prepareAction(page, selector, isLastAction, *screenshot, *lastOnly, i+1, len(actions), "extract")
			}
		case "finish":
			log.Printf("🏁 Finishing session as requested...")
			result["status"] = "Finished"
			// Force a final screenshot of the result with a longer settle delay (2s)
			time.Sleep(1 * time.Second)
			if *screenshot != "" {
				saveScreenshot(page, *screenshot, true)
			}
			// SYNC HTML: Ensure the returned HTML matches the final screenshot state perfectly
			if html, err := page.Content(); err == nil {
				result["html"] = html
			}
		}

		// Update progress - only after the action completes
		if *screenshot != "" {
			html, _ := page.Content()
			saveProgress(*screenshot, fmt.Sprintf("Step %d/%d: %s %s", i+1, len(actions), actionType, selector), i+1, len(actions), html)
			saveScreenshot(page, *screenshot, isLastAction)
		}

		if actionErr != nil {
			log.Printf("❌ Action %d (%s) failed: %v", i, actionType, actionErr)
			actionErrors = append(actionErrors, fmt.Sprintf("Action %d failed: %v", i, actionErr))

			// FAIL EARLY: If one action in a sequence fails, don't keep plowing ahead
			// especially helpful for multi-step forms where state is now invalid
			result["status"] = "Failed"
			result["error"] = actionErr.Error()
			result["failed_step"] = i
			break
		}
	}

	// Total timeout restore
	page.SetDefaultTimeout(float64(*timeout) * 1000)

	// Final check on screenshot and HTML
	if *screenshot != "" {
		saveScreenshot(page, *screenshot, true)
		saveLiveHTML(page, *screenshot+".html")
	}

	// Prepare result
	result["actions"] = actions
	if *screenshot != "" {
		result["screenshot"] = *screenshot
	}
	if len(actionErrors) > 0 {
		result["status"] = "Failed"
		result["errors"] = actionErrors
	} else {
		result["status"] = "Success"
	}
	result["last_updated"] = time.Now().Unix()
	result["step"] = len(actions)

	// Always return HTML in result for smart scraping
	if html, err := page.Content(); err == nil {
		result["html"] = html
	}

	// Capture frames
	var framesData []map[string]string
	for _, frame := range page.Frames() {
		fContent, err := frame.Content()
		if err != nil {
			continue
		}
		framesData = append(framesData, map[string]string{
			"name":    frame.Name(),
			"url":     frame.URL(),
			"content": fContent,
		})
	}
	result["frames"] = framesData

	jsonOutput, _ := json.MarshalIndent(result, "", "  ")
	fmt.Println("\n###AGI_JSON_START###")
	fmt.Println(string(jsonOutput))
	fmt.Println("###AGI_JSON_END###")

	os.Stdout.Sync()

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

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		os.Exit(0)
	}
}

func performResilientSelect(page playwright.Page, selector, value string) {
	if value == "" {
		return
	}
	log.Printf("🔽 Selecting '%s' in %s...", value, selector)

	loc := page.Locator(selector).First()

	// Check if element exists and get its type
	if count, _ := loc.Count(); count > 0 {
		// Get both tagName and type attribute
		elementInfo, err := loc.Evaluate(`el => ({
			tagName: el.tagName,
			type: el.type || el.getAttribute('type') || '',
			role: el.getAttribute('role') || ''
		})`, nil)

		if err == nil {
			if info, ok := elementInfo.(map[string]interface{}); ok {
				tagName, _ := info["tagName"].(string)
				inputType, _ := info["type"].(string)
				role, _ := info["role"].(string)

				log.Printf("   🔍 Element: <%s> type='%s' role='%s'", tagName, inputType, role)

				// Handle CHECKBOX or RADIO inputs
				if tagName == "INPUT" && (inputType == "checkbox" || inputType == "radio") {
					log.Printf("   ℹ️ Detected %s input, using click strategy", inputType)

					// Check current state
					isChecked, _ := loc.Evaluate("el => el.checked", nil)
					checked, _ := isChecked.(bool)

					// Determine if we should click based on desired value
					shouldBeChecked := value == "true" || value == "1" || value == "yes" || value == "on"

					if shouldBeChecked != checked {
						log.Printf("   🖱️ Clicking %s to change state from %v to %v", inputType, checked, shouldBeChecked)
						if err := loc.Click(playwright.LocatorClickOptions{Timeout: playwright.Float(2000)}); err != nil {
							log.Printf("   ⚠️ Click failed: %v", err)
						} else {
							log.Printf("   ✅ %s toggled successfully", inputType)
						}
					} else {
						log.Printf("   ✅ %s already in desired state (%v)", inputType, checked)
					}
					return
				}

				// Handle SELECT elements
				if tagName == "SELECT" {
					log.Printf("   ℹ️ Identified as standard HTML SELECT")
					// Try value
					if _, err := loc.SelectOption(playwright.SelectOptionValues{Values: &[]string{value}}); err == nil {
						log.Printf("   ✅ Selected via value")
						return
					}
					// Try label
					if _, err := loc.SelectOption(playwright.SelectOptionValues{Labels: &[]string{value}}); err == nil {
						log.Printf("   ✅ Selected via label")
						return
					}
					log.Printf("   ⚠️ Standard select failed, trying custom fallback...")
				}
			}
		}
	}

	// Try click-to-open dropdown (button/div that opens a list)
	if tryClickToOpenDropdown(page, selector, value) {
		return
	}

	// Typeable combobox/autocomplete: focus, clear, type, then click suggestion
	log.Printf("   ℹ️ Treating as typeable combobox on %s", selector)
	if err := page.Click(selector, playwright.PageClickOptions{Timeout: playwright.Float(2000)}); err != nil {
		log.Printf("   ⚠️ Click to focus failed (might be covered), trying force click")
		_ = page.Click(selector, playwright.PageClickOptions{Timeout: playwright.Float(1000), Force: playwright.Bool(true)})
	}
	_ = page.Fill(selector, "")
	_ = page.Type(selector, value, playwright.PageTypeOptions{Delay: playwright.Float(150)})
	time.Sleep(500 * time.Millisecond)

	suggestionFound := tryClickDropdownOption(page, value)
	if !suggestionFound {
		log.Printf("   ℹ️ No clear suggestion clicked, falling back to 'Enter'")
		_ = page.Keyboard().Press("Enter")
	}
}

// tryClickToOpenDropdown opens a click-to-open control (button/div) then clicks the option matching value.
// Returns true if an option was successfully clicked.
func tryClickToOpenDropdown(page playwright.Page, selector, value string) bool {
	loc := resolveLocator(page, selector)
	if loc == nil {
		return false
	}
	// Open the dropdown by clicking the trigger
	if err := loc.Click(playwright.LocatorClickOptions{Timeout: playwright.Float(2000)}); err != nil {
		log.Printf("   ⚠️ Click to open dropdown failed: %v", err)
		return false
	}
	time.Sleep(450 * time.Millisecond) // Let list render
	if tryClickDropdownOption(page, value) {
		log.Printf("   ✅ Selected option via click-to-open dropdown")
		return true
	}
	return false
}

// tryClickDropdownOption looks for a visible option containing value and clicks it (listbox, role=option, etc.).
// Returns true if a click succeeded.
func tryClickDropdownOption(page playwright.Page, value string) bool {
	// Strategy A: Playwright text locator (exclude the trigger by preferring elements in listbox/menu)
	listboxText := fmt.Sprintf("[role='listbox'] >> text=/%s/i", value)
	if count, _ := page.Locator(listboxText).Count(); count > 0 {
		if err := page.Locator(listboxText).First().Click(playwright.LocatorClickOptions{Timeout: playwright.Float(1000)}); err == nil {
			return true
		}
	}
	textSelector := fmt.Sprintf("text=/%s/i", value)
	if count, _ := page.Locator(textSelector).Count(); count > 0 {
		// Prefer last() so we hit the option in the overlay rather than the trigger label
		if err := page.Locator(textSelector).Last().Click(playwright.LocatorClickOptions{Timeout: playwright.Float(1000)}); err == nil {
			return true
		}
	}
	// Strategy B: Common dropdown item patterns
	for _, pat := range []string{"[role='option']", ".dropdown-item", "li", ".result-item", ".autocomplete-option", "[data-option]"} {
		complexSel := fmt.Sprintf("%s:has-text(\"%s\")", pat, value)
		if count, _ := page.Locator(complexSel).Count(); count > 0 {
			if err := page.Locator(complexSel).First().Click(playwright.LocatorClickOptions{Timeout: playwright.Float(1000)}); err == nil {
				return true
			}
		}
	}
	// Strategy C: JS fallback (visible elements containing text)
	jsScript := `(text) => {
		const candidates = document.querySelectorAll('[role="option"], [role="listbox"] li, .dropdown-item, li, [data-option], .autocomplete-option');
		for (const el of candidates) {
			if (el.innerText.trim().toLowerCase().includes(text.toLowerCase()) && el.offsetParent !== null) {
				el.click();
				return true;
			}
		}
		// Broader: any clickable with matching text
		const all = document.querySelectorAll('li, div[role="option"], .dropdown-item, span, [data-value]');
		for (const el of all) {
			if (el.innerText.toLowerCase().includes(text.toLowerCase()) && el.offsetParent !== null) {
				el.click();
				return true;
			}
		}
		return false;
	}`
	if result, err := page.Evaluate(jsScript, value); err == nil {
		if success, ok := result.(bool); ok && success {
			return true
		}
	}
	return false
}

func saveScreenshot(page playwright.Page, path string, fullPage bool) {
	// Hide overlays
	_, _ = page.Evaluate(`() => {
		const id = 'agi-clean-shot-style';
		if (!document.getElementById(id)) {
			const s = document.createElement('style');
			s.id = id;
			s.innerHTML = 'header, footer, .cookie-banner, .modal, .popup, .overlay { opacity: 0.1 !important; pointer-events: none !important; }';
			document.head.appendChild(s);
		}
	}`)

	// Resize (ensure minimum height if full page)
	if fullPage {
		if _, err := page.Evaluate(`() => Math.max(document.body.scrollHeight, 800)`); err != nil {
			// ignore
		}
		_ = page.SetViewportSize(1280, 800)
	}

	// Capture
	if _, err := page.Screenshot(playwright.PageScreenshotOptions{
		Path:     playwright.String(path),
		FullPage: playwright.Bool(fullPage),
	}); err != nil {
		log.Printf("⚠️ Screenshot failed: %v", err)
	}

	// Restore
	_, _ = page.Evaluate(`() => {
		const s = document.getElementById('agi-clean-shot-style');
		if (s) s.remove();
	}`)
}

func saveLiveHTML(page playwright.Page, path string) {
	html, err := page.Content()
	if err == nil {
		_ = os.WriteFile(path, []byte(html), 0644)
	}
}

func resolveLocator(page playwright.Page, selector string) playwright.Locator {
	// Main frame
	loc := page.Locator(selector)
	if c, _ := loc.Count(); c > 0 {
		return loc.First()
	}
	// IFrames
	for _, f := range page.Frames() {
		if f == page.MainFrame() {
			continue
		}
		fLoc := f.Locator(selector)
		if c, _ := fLoc.Count(); c > 0 {
			return fLoc.First()
		}
	}
	return nil
}

func saveProgress(path string, status string, step int, total int, html string) {
	progressPath := path + ".progress"
	prog := map[string]interface{}{
		"status":     status,
		"step":       step,
		"total":      total,
		"html":       html,
		"screenshot": path,
	}
	data, _ := json.Marshal(prog)
	_ = os.WriteFile(progressPath, data, 0644)
}
