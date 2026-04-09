package scraper_pkg

import (
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
	"time"

	pw "github.com/playwright-community/playwright-go"
)

// PlaywrightOperation represents a parsed operation from TypeScript config
type PlaywrightOperation struct {
	Type           string
	Selector       string
	Value          string
	Role           string
	RoleName       string
	Text           string
	TimeoutMS      int
	Index          int    // For nth(n) selectors
	ChildSelector  string // For scoped selectors (e.g., locator().locator())
	IframeSelector string // For iframe elements (e.g., contentFrame())
	Width          int    // For setViewportSize
	Height         int    // For setViewportSize
	Script         string // For evaluate
	UserAgent      string // For setUserAgent
}

// ParseTypeScriptConfig parses a string of TypeScript-like Playwright commands into a sequence of operations
func ParseTypeScriptConfig(tsConfig string) ([]PlaywrightOperation, error) {
	var operations []PlaywrightOperation

	// Log first 500 chars of the config for debugging
	preview := tsConfig
	if len(preview) > 500 {
		preview = preview[:500] + "..."
	}
	log.Printf("📝 TypeScript config (first 500 chars): %s", preview)

	lines := strings.Split(tsConfig, "\n")
	var currentOp strings.Builder
	inEvaluate := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "//") {
			continue
		}

		// Detect start of evaluate block
		if !inEvaluate && strings.Contains(trimmed, "page.evaluate") {
			inEvaluate = true
			currentOp.WriteString(trimmed)
			// Check if it's a single-line evaluate
			if strings.HasSuffix(trimmed, "})") || strings.HasSuffix(trimmed, "});") {
				inEvaluate = false
				op := ParseOperation(currentOp.String())
				if op.Type != "" {
					operations = append(operations, op)
				}
				currentOp.Reset()
			}
			continue
		}

		if inEvaluate {
			currentOp.WriteString(" " + trimmed)
			if strings.Contains(trimmed, "})") || strings.Contains(trimmed, ");") {
				inEvaluate = false
				op := ParseOperation(currentOp.String())
				if op.Type != "" {
					operations = append(operations, op)
				}
				currentOp.Reset()
			}
			continue
		}

		// Direct operation on current line
		if strings.Contains(trimmed, "page.") {
			op := ParseOperation(trimmed)
			if op.Type != "" {
				operations = append(operations, op)
			} else {
				log.Printf("⚠️ Failed to parse operation line: %s", trimmed)
			}
		}
	}

	return operations, nil
}

// ApplyTemplateVariables replaces placeholders like {{var}} or ${var} with values
func ApplyTemplateVariables(tsConfig string, variables map[string]string) string {
	if tsConfig == "" || len(variables) == 0 {
		return tsConfig
	}

	replacerArgs := make([]string, 0, len(variables)*4)
	for key, value := range variables {
		if key == "" {
			continue
		}
		replacerArgs = append(replacerArgs, "{{"+key+"}}", value)
		replacerArgs = append(replacerArgs, "{{ "+key+" }}", value)
		replacerArgs = append(replacerArgs, "${"+key+"}", value)
		replacerArgs = append(replacerArgs, "${ "+key+" }", value)
	}
	if len(replacerArgs) == 0 {
		return tsConfig
	}

	replacer := strings.NewReplacer(replacerArgs...)
	return replacer.Replace(tsConfig)
}

// ParseOperation uses regex to convert a single line of TS into a PlaywrightOperation struct
func ParseOperation(line string) PlaywrightOperation {
	// iframe getByRole (click)
	if matches := regexp.MustCompile(`(?:await\s+)?page\.locator\(['"](.+?)['"]\)\.contentFrame\(\)\.getByRole\(['"](\w+)['"],\s*\{\s*name:\s*['"](.+?)['"]\s*\}\)\.click\(\)`).FindStringSubmatch(line); len(matches) > 3 {
		return PlaywrightOperation{Type: "iframeGetByRole", IframeSelector: matches[1], Role: matches[2], RoleName: matches[3]}
	}

	// iframe getByRole (fill)
	if matches := regexp.MustCompile(`(?:await\s+)?page\.locator\(['"](.+?)['"]\)\.contentFrame\(\)\.getByRole\(['"](\w+)['"],\s*\{\s*name:\s*['"](.+?)['"]\s*\}\)\.fill\(['"](.+?)['"]\)`).FindStringSubmatch(line); len(matches) > 4 {
		return PlaywrightOperation{Type: "iframeGetByRoleFill", IframeSelector: matches[1], Role: matches[2], RoleName: matches[3], Value: matches[4]}
	}

	// iframe getByText (click)
	if matches := regexp.MustCompile(`(?:await\s+)?page\.locator\(['"](.+?)['"]\)\.contentFrame\(\)\.getByText\(['"](.+?)['"]\)\.click\(\)`).FindStringSubmatch(line); len(matches) > 2 {
		return PlaywrightOperation{Type: "iframeGetByTextClick", IframeSelector: matches[1], Text: matches[2]}
	}
	// iframe getByLabel (click)
	if matches := regexp.MustCompile(`(?:await\s+)?page\.locator\(['"](.+?)['"]\)\.contentFrame\(\)\.getByLabel\(['"](.+?)['"](?:,\s*\{.+?\})?\)\.click\(\)`).FindStringSubmatch(line); len(matches) > 2 {
		return PlaywrightOperation{Type: "iframeGetByLabelClick", IframeSelector: matches[1], Text: matches[2]}
	}

	// iframe getByLabel (fill)
	if matches := regexp.MustCompile(`(?:await\s+)?page\.locator\(['"](.+?)['"]\)\.contentFrame\(\)\.getByLabel\(['"](.+?)['"](?:,\s*\{.+?\})?\)\.fill\(['"](.+?)['"]\)`).FindStringSubmatch(line); len(matches) > 3 {
		return PlaywrightOperation{Type: "iframeGetByLabelFill", IframeSelector: matches[1], Text: matches[2], Value: matches[3]}
	}

	// iframe getByPlaceholder (fill)
	if matches := regexp.MustCompile(`(?:await\s+)?page\.locator\(['"](.+?)['"]\)\.contentFrame\(\)\.getByPlaceholder\(['"](.+?)['"](?:,\s*\{.+?\})?\)\.fill\(['"](.+?)['"]\)`).FindStringSubmatch(line); len(matches) > 3 {
		return PlaywrightOperation{Type: "iframeGetByPlaceholderFill", IframeSelector: matches[1], Text: matches[2], Value: matches[3]}
	}

	// iframe locator (click)
	if matches := regexp.MustCompile(`(?:await\s+)?page\.locator\(['"](.+?)['"]\)\.contentFrame\(\)\.locator\(['"](.+?)['"]\)\.click\(\)`).FindStringSubmatch(line); len(matches) > 2 {
		return PlaywrightOperation{Type: "iframeLocatorClick", IframeSelector: matches[1], Selector: matches[2]}
	}

	// iframe locator (fill)
	if matches := regexp.MustCompile(`(?:await\s+)?page\.locator\(['"](.+?)['"]\)\.contentFrame\(\)\.locator\(['"](.+?)['"]\)\.fill\(['"](.+?)['"]\)`).FindStringSubmatch(line); len(matches) > 3 {
		return PlaywrightOperation{Type: "iframeLocatorFill", IframeSelector: matches[1], Selector: matches[2], Value: matches[3]}
	}

	// setUserAgent
	if matches := regexp.MustCompile(`(?:await\s+)?page\.setUserAgent\(['"](.+?)['"]\)`).FindStringSubmatch(line); len(matches) > 1 {
		return PlaywrightOperation{Type: "setUserAgent", UserAgent: matches[1]}
	}

	// goto
	if matches := regexp.MustCompile(`(?:await\s+)?page\.goto\(['"](.+?)['"]\)`).FindStringSubmatch(line); len(matches) > 1 {
		return PlaywrightOperation{Type: "goto", Selector: matches[1]}
	}

	// getByRole (click)
	if matches := regexp.MustCompile(`(?:await\s+)?page\.getByRole\(['"](\w+)['"]\s*,\s*\{\s*name:\s*['"](.+?)['"]\s*\}\)\.first\(\)\.click\(\)`).FindStringSubmatch(line); len(matches) > 2 {
		return PlaywrightOperation{Type: "getByRole", Role: matches[1], RoleName: matches[2]}
	}
	if matches := regexp.MustCompile(`(?:await\s+)?page\.getByRole\(['"](\w+)['"]\s*,\s*\{\s*name:\s*['"](.+?)['"]\s*\}\)\.click\(\)`).FindStringSubmatch(line); len(matches) > 2 {
		return PlaywrightOperation{Type: "getByRole", Role: matches[1], RoleName: matches[2]}
	}

	// getByRole (fill)
	if matches := regexp.MustCompile(`(?:await\s+)?page\.getByRole\(['"](\w+)['"]\s*,\s*\{\s*name:\s*['"](.+?)['"]\s*\}\)\.fill\(['"](.+?)['"]\)`).FindStringSubmatch(line); len(matches) > 3 {
		return PlaywrightOperation{Type: "getByRoleFill", Role: matches[1], RoleName: matches[2], Value: matches[3]}
	}

	// getByLabel (click)
	if matches := regexp.MustCompile(`(?:await\s+)?page\.getByLabel\(['"](.+?)['"](?:,\s*\{.+?\}|,\s*\{exact:\s*true\})?\)\.first\(\)\.click\(\)`).FindStringSubmatch(line); len(matches) > 1 {
		return PlaywrightOperation{Type: "getByLabelClick", Text: matches[1]}
	}
	if matches := regexp.MustCompile(`(?:await\s+)?page\.getByLabel\(['"](.+?)['"](?:,\s*\{.+?\}|,\s*\{exact:\s*true\})?\)\.click\(\)`).FindStringSubmatch(line); len(matches) > 1 {
		return PlaywrightOperation{Type: "getByLabelClick", Text: matches[1]}
	}

	// getByLabel (fill)
	if matches := regexp.MustCompile(`(?:await\s+)?page\.getByLabel\(['"](.+?)['"](?:,\s*\{.+?\}|,\s*\{exact:\s*true\})?\)\.fill\(['"](.+?)['"]\)`).FindStringSubmatch(line); len(matches) > 2 {
		return PlaywrightOperation{Type: "getByLabelFill", Text: matches[1], Value: matches[2]}
	}

	// getByPlaceholder (fill)
	if matches := regexp.MustCompile(`(?:await\s+)?page\.getByPlaceholder\(['"](.+?)['"](?:,\s*\{.+?\})?\)\.fill\(['"](.+?)['"]\)`).FindStringSubmatch(line); len(matches) > 2 {
		return PlaywrightOperation{Type: "getByPlaceholderFill", Text: matches[1], Value: matches[2]}
	}

	// getByPlaceholder (click)
	if matches := regexp.MustCompile(`(?:await\s+)?page\.getByPlaceholder\(['"](.+?)['"](?:,\s*\{.+?\})?\)\.first\(\)\.click\(\)`).FindStringSubmatch(line); len(matches) > 1 {
		return PlaywrightOperation{Type: "getByPlaceholderClick", Text: matches[1]}
	}
	if matches := regexp.MustCompile(`(?:await\s+)?page\.getByPlaceholder\(['"](.+?)['"](?:,\s*\{.+?\})?\)\.click\(\)`).FindStringSubmatch(line); len(matches) > 1 {
		return PlaywrightOperation{Type: "getByPlaceholderClick", Text: matches[1]}
	}

	// getByTestId (click)
	if matches := regexp.MustCompile(`(?:await\s+)?page\.getByTestId\(['"](.+?)['"]\)\.click\(\)`).FindStringSubmatch(line); len(matches) > 1 {
		return PlaywrightOperation{Type: "getByTestIdClick", Selector: matches[1]}
	}

	// getByText (click)
	if matches := regexp.MustCompile(`(?:await\s+)?page\.getByText\(['"](.+?)['"]\)\.first\(\)\.click\(\)`).FindStringSubmatch(line); len(matches) > 1 {
		return PlaywrightOperation{Type: "getByText", Text: matches[1]}
	}
	if matches := regexp.MustCompile(`(?:await\s+)?page\.getByText\(['"](.+?)['"]\)\.click\(\)`).FindStringSubmatch(line); len(matches) > 1 {
		return PlaywrightOperation{Type: "getByTextClick", Text: matches[1]}
	}

	// locator (fill)
	if matches := regexp.MustCompile(`(?:await\s+)?page\.fill\(['"](.+?)['"]\s*,\s*['"](.+?)['"]\s*\)`).FindStringSubmatch(line); len(matches) > 2 {
		return PlaywrightOperation{Type: "locatorFill", Selector: matches[1], Value: matches[2]}
	}

	// locator (click)
	if matches := regexp.MustCompile(`(?:await\s+)?page\.click\(['"](.+?)['"]\s*\)`).FindStringSubmatch(line); len(matches) > 1 {
		return PlaywrightOperation{Type: "locator", Selector: matches[1]}
	}

	// selectOption
	if matches := regexp.MustCompile(`(?:await\s+)?page\.selectOption\(['"](.+?)['"]\s*,\s*['"](.+?)['"]\s*\)`).FindStringSubmatch(line); len(matches) > 2 {
		return PlaywrightOperation{Type: "locatorSelectOptionFirst", Selector: matches[1], Value: matches[2]}
	}

	// bypassConsent
	if strings.Contains(line, "bypassConsent") {
		return PlaywrightOperation{Type: "bypassConsent"}
	}

	// locator combinations
	if matches := regexp.MustCompile(`(?:await\s+)?page\.locator\(['"](.+?)['"]\)\.first\(\)\.locator\(['"](.+?)['"]\)\.fill\(['"](.+?)['"]\)`).FindStringSubmatch(line); len(matches) > 3 {
		return PlaywrightOperation{Type: "scopedLocatorFillFirst", Selector: matches[1], ChildSelector: matches[2], Value: matches[3]}
	}
	if matches := regexp.MustCompile(`(?:await\s+)?page\.locator\(['"](.+?)['"]\)\.nth\((\d+)\)\.locator\(['"](.+?)['"]\)\.fill\(['"](.+?)['"]\)`).FindStringSubmatch(line); len(matches) > 4 {
		index, _ := strconv.Atoi(matches[2])
		return PlaywrightOperation{Type: "scopedLocatorFillNth", Selector: matches[1], Index: index, ChildSelector: matches[3], Value: matches[4]}
	}
	if matches := regexp.MustCompile(`(?:await\s+)?page\.locator\(['"](.+?)['"]\)\.first\(\)\.locator\(['"](.+?)['"]\)(?:\.first\(\)|\.nth\(\d+\))?\.click\(\)`).FindStringSubmatch(line); len(matches) > 2 {
		return PlaywrightOperation{Type: "scopedLocatorClickFirst", Selector: matches[1], ChildSelector: matches[2]}
	}
	if matches := regexp.MustCompile(`(?:await\s+)?page\.locator\(['"](.+?)['"]\)\.nth\((\d+)\)\.locator\(['"](.+?)['"]\)(?:\.first\(\)|\.nth\(\d+\))?\.click\(\)`).FindStringSubmatch(line); len(matches) > 3 {
		index, _ := strconv.Atoi(matches[2])
		return PlaywrightOperation{Type: "scopedLocatorClickNth", Selector: matches[1], Index: index, ChildSelector: matches[3]}
	}

	// Basic locators
	if matches := regexp.MustCompile(`(?:await\s+)?page\.locator\(['"](.+?)['"]\)\.first\(\)\.selectOption\(['"](.+?)['"]\)`).FindStringSubmatch(line); len(matches) > 2 {
		return PlaywrightOperation{Type: "locatorSelectOptionFirst", Selector: matches[1], Value: matches[2]}
	}
	if matches := regexp.MustCompile(`(?:await\s+)?page\.locator\(['"](.+?)['"]\)\.fill\(['"](.+?)['"]\)`).FindStringSubmatch(line); len(matches) > 2 {
		return PlaywrightOperation{Type: "locatorFill", Selector: matches[1], Value: matches[2]}
	}
	if matches := regexp.MustCompile(`(?:await\s+)?page\.locator\(['"](.+?)['"]\)\.nth\((\d+)\)\.fill\(['"](.+?)['"]\)`).FindStringSubmatch(line); len(matches) > 3 {
		index, _ := strconv.Atoi(matches[2])
		return PlaywrightOperation{Type: "locatorFillAtIndex", Selector: matches[1], Value: matches[3], Index: index}
	}
	if matches := regexp.MustCompile(`(?:await\s+)?page\.locator\(['"](.+?)['"]\)\.first\(\)\.click\(\)`).FindStringSubmatch(line); len(matches) > 1 {
		return PlaywrightOperation{Type: "locatorFirst", Selector: matches[1]}
	}
	if matches := regexp.MustCompile(`(?:await\s+)?page\.locator\(['"](.+?)['"]\)\.click\(\)`).FindStringSubmatch(line); len(matches) > 1 {
		return PlaywrightOperation{Type: "locator", Selector: matches[1]}
	}

	// Keyboard/Wait
	if matches := regexp.MustCompile(`(?:await\s+)?page\.keyboard\.press\(['"]([^'"]+)['"]\)`).FindStringSubmatch(line); len(matches) > 1 {
		return PlaywrightOperation{Type: "keyboardPress", Value: matches[1]}
	}
	if matches := regexp.MustCompile(`(?:await\s+)?page\.keyboard\.type\(['"]([^'"]+)['"]\)`).FindStringSubmatch(line); len(matches) > 1 {
		return PlaywrightOperation{Type: "keyboardType", Value: matches[1]}
	}
	if matches := regexp.MustCompile(`(?:await\s+)?page\.waitForTimeout\((\d+)\)`).FindStringSubmatch(line); len(matches) > 1 {
		var timeout int
		fmt.Sscanf(matches[1], "%d", &timeout)
		return PlaywrightOperation{Type: "wait", TimeoutMS: timeout}
	}
	if matches := regexp.MustCompile(`(?:await\s+)?page\.waitForSelector\(['"](.+?)['"](?:,\s*\{.+?\})?\)`).FindStringSubmatch(line); len(matches) > 1 {
		return PlaywrightOperation{Type: "waitSelector", Selector: matches[1]}
	}
	if matches := regexp.MustCompile(`(?:await\s+)?page\.waitForLoadState\(['"](\w+)['"](?:,\s*\{.+?\})?\)`).FindStringSubmatch(line); len(matches) > 1 {
		return PlaywrightOperation{Type: "waitLoadState", Value: matches[1]}
	}

	// Misc
	if matches := regexp.MustCompile(`(?s)(?:await\s+)?page\.evaluate\(\s*\(.*?\)\s*=>\s*(?:\{([\s\S]+?)\}|([\s\S]+?))\s*\)`).FindStringSubmatch(line); len(matches) > 1 {
		script := matches[1]
		if script == "" {
			script = matches[2]
		}
		return PlaywrightOperation{Type: "evaluate", Script: script}
	}

	return PlaywrightOperation{}
}

// ExecuteEngine runs a sequence of PlaywrightOperations on an existing Page
func ExecuteEngine(page pw.Page, operations []PlaywrightOperation, logger Logger) error {
	for i, op := range operations {
		logger.Printf("  [%d/%d] Executing: %s", i+1, len(operations), op.Type)

		switch op.Type {
		case "goto":
			if op.Selector != "" {
				if _, err := page.Goto(op.Selector, pw.PageGotoOptions{WaitUntil: pw.WaitUntilStateNetworkidle}); err != nil {
					return fmt.Errorf("goto %s failed: %v", op.Selector, err)
				}
			}

		case "setUserAgent":
			if op.UserAgent != "" {
				logger.Printf("👤 Setting User-Agent to: %s", op.UserAgent)
				if err := page.SetExtraHTTPHeaders(map[string]string{"User-Agent": op.UserAgent}); err != nil {
					logger.Printf("   ⚠️ Failed to set UA: %v", err)
				}
			}

		case "iframeGetByRole":
			locator := page.FrameLocator(op.IframeSelector).GetByRole(pw.AriaRole(op.Role), pw.FrameLocatorGetByRoleOptions{Name: op.RoleName})
			if err := locator.Click(); err != nil {
				logger.Printf("   ⚠️ Failed: %v", err)
			}
			time.Sleep(500 * time.Millisecond)

		case "getByRole":
			locator := page.GetByRole(pw.AriaRole(op.Role), pw.PageGetByRoleOptions{Name: op.RoleName})
			if err := locator.First().Click(); err != nil {
				logger.Printf("   ⚠️ Failed: %v", err)
			}
			time.Sleep(500 * time.Millisecond)

		case "getByRoleFill":
			locator := page.GetByRole(pw.AriaRole(op.Role), pw.PageGetByRoleOptions{Name: op.RoleName})
			if err := locator.First().Fill(op.Value); err != nil {
				logger.Printf("   ⚠️ Failed: %v", err)
			}
			time.Sleep(500 * time.Millisecond)

		case "getByLabelClick":
			if err := page.GetByLabel(op.Text).First().Click(); err != nil {
				logger.Printf("   ⚠️ Failed: %v", err)
			}
			time.Sleep(500 * time.Millisecond)

		case "getByLabelFill":
			if err := page.GetByLabel(op.Text).First().Fill(op.Value); err != nil {
				logger.Printf("   ⚠️ Failed: %v", err)
			}
			time.Sleep(500 * time.Millisecond)

		case "getByPlaceholderFill":
			if err := page.GetByPlaceholder(op.Text).First().Fill(op.Value); err != nil {
				logger.Printf("   ⚠️ Failed: %v", err)
			}
			time.Sleep(500 * time.Millisecond)

		case "getByText":
			if err := page.GetByText(op.Text).First().Click(); err != nil {
				logger.Printf("   ⚠️ Failed: %v", err)
			}
			time.Sleep(300 * time.Millisecond)

		case "bypassConsent":
			logger.Printf("🍪 Attempting auto-consent bypass...")
			BypassConsent(page, logger)

		case "locator":
			if err := page.Locator(op.Selector).Click(); err != nil {
				logger.Printf("   ⚠️ Failed: %v", err)
			}
			time.Sleep(300 * time.Millisecond)

		case "locatorFill":
			if err := page.Locator(op.Selector).Fill(op.Value); err != nil {
				logger.Printf("   ⚠️ Failed: %v", err)
			}
			time.Sleep(300 * time.Millisecond)

		case "wait":
			if op.TimeoutMS > 0 {
				time.Sleep(time.Duration(op.TimeoutMS) * time.Millisecond)
			} else {
				time.Sleep(500 * time.Millisecond)
			}

		case "keyboardPress":
			if err := page.Keyboard().Press(op.Value); err != nil {
				logger.Printf("   ⚠️ Keyboard press failed: %v", err)
			}

		case "keyboardType":
			if err := page.Keyboard().Type(op.Value); err != nil {
				logger.Printf("   ⚠️ Keyboard type failed: %v", err)
			}

		case "evaluate":
			if _, err := page.Evaluate(op.Script); err != nil {
				logger.Printf("   ⚠️ Evaluation failed: %v", err)
			}

		case "waitSelector":
			if _, err := page.WaitForSelector(op.Selector, pw.PageWaitForSelectorOptions{Timeout: pw.Float(10000)}); err != nil {
				logger.Printf("   ⚠️ Wait for selector %s failed: %v", op.Selector, err)
			}

		default:
			logger.Printf("   ⚠️ Unsupported operation type: %s", op.Type)
		}
	}
	return nil
}

// BypassConsent is the standard implementation of consent wall bypassing
func BypassConsent(page pw.Page, logger Logger) {
	patterns := []string{
		"(?i)accept", "(?i)agree", "(?i)continue", "(?i)allow", "(?i)ok", "(?i)yes",
		"(?i)accepter", "(?i)continuer", "(?i)autoriser", "(?i)j'accepte", "(?i)tout accepter",
		"(?i)akzeptieren", "(?i)zustimmen",
		"(?i)aceptar", "(?i)continuar", "tout équivalents",
	}

	clicked := false
	for _, p := range patterns {
		re := regexp.MustCompile(p)
		locator := page.GetByRole(pw.AriaRole("button"), pw.PageGetByRoleOptions{Name: re}).First()
		if count, _ := locator.Count(); count > 0 {
			if err := locator.Click(pw.LocatorClickOptions{Timeout: pw.Float(2000), Force: pw.Bool(true)}); err == nil {
				logger.Printf("✅ Clicked consent button: %s", p)
				clicked = true
				break
			}
		}
	}

	if !clicked {
		selectors := []string{
			"button[id*='accept']", "button[class*='accept']",
			"button[id*='agree']", "button[class*='agree']",
			"#onetrust-accept-btn-handler",
			"#sp-cc-accept",
		}
		for _, sel := range selectors {
			locator := page.Locator(sel).First()
			if count, _ := locator.Count(); count > 0 {
				if err := locator.Click(pw.LocatorClickOptions{Timeout: pw.Float(2000), Force: pw.Bool(true)}); err == nil {
					logger.Printf("✅ Clicked consent selector: %s", sel)
					clicked = true
					break
				}
			}
		}
	}

	if clicked {
		page.WaitForLoadState(pw.PageWaitForLoadStateOptions{State: pw.LoadStateNetworkidle})
		time.Sleep(1 * time.Second)
	}
}
