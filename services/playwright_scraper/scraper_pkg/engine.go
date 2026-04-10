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
	X              int
	Y              int
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
func ParseTypeScriptConfig(config string) ([]PlaywrightOperation, error) {
	var operations []PlaywrightOperation
	lines := strings.Split(config, "\n")

	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}

		// Handle multiline evaluate or other block operations
		if strings.Contains(line, "page.evaluate") || strings.Contains(line, "page.setViewportSize") || strings.Contains(line, "page.waitForSelector") || strings.Contains(line, "page.mouse.wheel") || strings.Contains(line, "page.waitForLoadState") {
			// Find start of the block or arguments
			content := line
			
			// Detect if it opens a brace that needs matching
			if strings.Contains(line, "{") {
				braceCount := strings.Count(line, "{") - strings.Count(line, "}")
				for braceCount > 0 && i+1 < len(lines) {
					i++
					content += "\n" + lines[i]
					braceCount += strings.Count(lines[i], "{") - strings.Count(lines[i], "}")
				}
			} else if strings.HasSuffix(line, ";") || strings.Contains(line, ");") {
				// Single line but complex
			} else if i+1 < len(lines) && !strings.HasSuffix(line, ";") {
				// Try to peek next line for completion
				if strings.Contains(lines[i+1], ");") || strings.Contains(lines[i+1], "}") {
					i++
					content += "\n" + lines[i]
				}
			}

			trimmed := strings.TrimSpace(content)
			op := ParseOperation(trimmed)
			if op.Type != "" {
				operations = append(operations, op)
			} else {
				log.Printf("⚠️ Failed to parse complex operation (Stage 1): %s", trimmed)
			}
			continue
		}

		// Simple single-line fallback
		op := ParseOperation(line)
		if op.Type != "" {
			operations = append(operations, op)
		} else {
			if strings.Contains(line, "page.") {
				log.Printf("⚠️ Failed to parse simple operation line: %s", line)
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

	// setViewportSize
	if matches := regexp.MustCompile(`(?:await\s+)?page\.setViewportSize\(\{\s*width:\s*(\d+),\s*height:\s*(\d+)\s*\}\)`).FindStringSubmatch(line); len(matches) > 2 {
		w, _ := strconv.Atoi(matches[1])
		h, _ := strconv.Atoi(matches[2])
		return PlaywrightOperation{Type: "setViewportSize", Width: w, Height: h}
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
	if matches := regexp.MustCompile(`(?:await\s+)?page\.waitForSelector\(['"](.+?)['"](?:,\s*\{\s*timeout:\s*(\d+)\s*\})?\)`).FindStringSubmatch(line); len(matches) > 1 {
		timeout := 0
		if len(matches) > 2 {
			timeout, _ = strconv.Atoi(matches[2])
		}
		return PlaywrightOperation{Type: "waitSelector", Selector: matches[1], TimeoutMS: timeout}
	}
	if matches := regexp.MustCompile(`(?:await\s+)?page\.waitForLoadState\(['"](\w+)['"](?:,\s*\{\s*timeout:\s*(\d+)\s*\})?\)`).FindStringSubmatch(line); len(matches) > 1 {
		timeout := 0
		if len(matches) > 2 {
			timeout, _ = strconv.Atoi(matches[2])
		}
		return PlaywrightOperation{Type: "waitLoadState", Value: matches[1], TimeoutMS: timeout}
	}
	if matches := regexp.MustCompile(`(?:await\s+)?page\.mouse\.wheel\((\d+),\s*(\d+)\)`).FindStringSubmatch(line); len(matches) > 2 {
		x, _ := strconv.Atoi(matches[1])
		y, _ := strconv.Atoi(matches[2])
		return PlaywrightOperation{Type: "mouseWheel", X: x, Y: y}
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
		case "setViewportSize":
			if op.Width > 0 && op.Height > 0 {
				if err := page.SetViewportSize(op.Width, op.Height); err != nil {
					logger.Printf("   ⚠️ Failed to set viewport: %v", err)
				}
			}
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
			script := op.Script
			// Surgical: Wrap in async IIFE if script contains await
			if strings.Contains(script, "await") && !strings.Contains(script, "async ()") {
				script = fmt.Sprintf("(async () => { %s })()", script)
			}
			if _, err := page.Evaluate(script); err != nil {
				logger.Printf("   ⚠️ Evaluation failed: %v", err)
			}

		case "waitSelector":
			timeout := float64(30000)
			if op.TimeoutMS > 0 {
				timeout = float64(op.TimeoutMS)
			}
			if _, err := page.WaitForSelector(op.Selector, pw.PageWaitForSelectorOptions{Timeout: pw.Float(timeout)}); err != nil {
				logger.Printf("   ⚠️ Wait for selector %s failed: %v", op.Selector, err)
			}

		case "waitLoadState":
			state := pw.LoadStateNetworkidle
			if op.Value == "load" {
				state = pw.LoadStateLoad
			} else if op.Value == "domcontentloaded" {
				state = pw.LoadStateDomcontentloaded
			}
			timeout := float64(30000)
			if op.TimeoutMS > 0 {
				timeout = float64(op.TimeoutMS)
			}
			if err := page.WaitForLoadState(pw.PageWaitForLoadStateOptions{State: &state, Timeout: pw.Float(timeout)}); err != nil {
				logger.Printf("   ⚠️ Wait for load state %s failed: %v", op.Value, err)
			}

		case "mouseWheel":
			if err := page.Mouse().Wheel(float64(op.X), float64(op.Y)); err != nil {
				logger.Printf("   ⚠️ Mouse wheel failed: %v", err)
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
