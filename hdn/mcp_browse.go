package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// browseWebWithActions executes browser actions directly
func (s *MCPKnowledgeServer) browseWebWithActions(ctx context.Context, url string, actions []map[string]interface{}) (interface{}, error) {
	log.Printf("🚀 [BROWSE-WEB] Starting browseWebWithActions for URL: %s with %d actions", url, len(actions))

	projectRoot := os.Getenv("AGI_PROJECT_ROOT")
	if projectRoot == "" {
		if wd, err := os.Getwd(); err == nil {
			projectRoot = wd
		}
	}

	candidates := []string{
		filepath.Join(projectRoot, "bin", "headless-browser"),
		filepath.Join(projectRoot, "bin", "tools", "headless_browser"),
		"bin/headless-browser",
		"../bin/headless-browser",
	}

	browserBin := ""
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			if abs, err := filepath.Abs(candidate); err == nil {
				browserBin = abs
			} else {
				browserBin = candidate
			}
			break
		}
	}

	if browserBin == "" {
		return nil, fmt.Errorf("headless-browser binary not found. Please build it first: cd tools/headless_browser && go build -o ../../bin/headless-browser")
	}

	actionsJSON, err := json.Marshal(actions)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal actions: %v", err)
	}

	runCmd := exec.CommandContext(ctx, browserBin,
		"-url", url,
		"-actions", string(actionsJSON),
		"-timeout", "60",
	)

	log.Printf("🔧 [BROWSE-WEB] Executing command: %s %v", browserBin, runCmd.Args[1:])
	log.Printf("🔧 [BROWSE-WEB] Actions JSON: %s", string(actionsJSON))

	output, err := runCmd.CombinedOutput()
	log.Printf("✅ [BROWSE-WEB] Command completed, output length: %d bytes, err: %v", len(output), err)
	if len(output) > 0 && len(output) < 500 {
		log.Printf("🔍 [BROWSE-WEB] Output content: %s", string(output))
	}

	if err != nil && len(output) == 0 {
		return nil, fmt.Errorf("browser execution failed: %v\nOutput: %s", err, string(output))
	}
	if err != nil && len(output) > 0 {
		log.Printf("⚠️ [BROWSE-WEB] Browser had error but produced output, proceeding: %v", err)
	}

	// Parse JSON result
	var result map[string]interface{}
	if err := json.Unmarshal(output, &result); err != nil {
		return map[string]interface{}{
			"content": []map[string]interface{}{
				{
					"type": "text",
					"text": string(output),
				},
			},
		}, nil
	}

	contentText := fmt.Sprintf("Scraped data from %s\n\n", url)
	if extracted, ok := result["extracted"].(map[string]interface{}); ok && len(extracted) > 0 {
		contentText += "Extracted data:\n"
		for k, v := range extracted {
			contentText += fmt.Sprintf("  %s: %v\n", k, v)
		}
	}

	return map[string]interface{}{
		"content": []map[string]interface{}{
			{
				"type": "text",
				"text": contentText,
			},
		},
		"data": result["extracted"],
	}, nil
}

// browseWeb uses a headless browser to navigate, fill forms, click buttons, and extract data
// It's prompt-driven: if instructions are provided, uses LLM to generate actions from page HTML
func (s *MCPKnowledgeServer) browseWeb(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	url, ok := args["url"].(string)
	if !ok || url == "" {
		return nil, fmt.Errorf("url parameter required")
	}

	instructions, _ := args["instructions"].(string)
	if instructions == "" {
		return nil, fmt.Errorf("instructions parameter required - describe what to do on the page")
	}

	log.Printf("🌐 [BROWSE-WEB] Delegating to Playwright scraper. URL: %s", url)

	res, err := s.scrapeWithConfig(ctx, url, instructions, "", false, nil, false, nil)
	if err != nil {
		return nil, err
	}

	if m, ok := res.(map[string]interface{}); ok {
		if resultData, ok2 := m["result"].(map[string]interface{}); ok2 {
			log.Printf("✅ [BROWSE-WEB] Successfully extracted data using Playwright scraper")
			return resultData, nil
		}
	}

	return res, nil
}

// formatExtractedData formats extracted data as a readable string
func formatExtractedData(data interface{}) string {
	if data == nil {
		return "No data extracted"
	}

	if dataMap, ok := data.(map[string]interface{}); ok {
		var builder strings.Builder
		for k, v := range dataMap {
			builder.WriteString(fmt.Sprintf("  %s: %v\n", k, v))
		}
		return builder.String()
	}

	return fmt.Sprintf("%v", data)
}

// extractFormStructure extracts key form elements from HTML to help LLM generate better selectors
func extractFormStructure(html string) string {
	var info strings.Builder

	calcKeywords := []string{"from", "to", "origin", "destination", "start", "end", "calc", "emission", "depart", "arrive", "address"}

	inputRe := regexp.MustCompile(`(?i)<input[^>]*>`)
	inputs := inputRe.FindAllString(html, -1)
	if len(inputs) > 0 {
		info.WriteString("Input fields found (use these EXACT selectors):\n")
		for i, input := range inputs {

			isCalcHint := false
			inputLower := strings.ToLower(input)
			for _, kw := range calcKeywords {
				if strings.Contains(inputLower, kw) {
					isCalcHint = true
					break
				}
			}

			if i < 20 {
				// Extract key attributes
				var attrs []string
				hint := ""
				if isCalcHint {
					hint = " [Likely Calculator Field]"
				}

				if idMatch := regexp.MustCompile(`(?i)id=["']([^"']+)["']`).FindStringSubmatch(input); len(idMatch) > 1 {
					attrs = append(attrs, fmt.Sprintf("id='%s' → selector: #%s", idMatch[1], idMatch[1]))
				}
				if nameMatch := regexp.MustCompile(`(?i)name=["']([^"']+)["']`).FindStringSubmatch(input); len(nameMatch) > 1 {
					attrs = append(attrs, fmt.Sprintf("name='%s' → selector: input[name='%s']", nameMatch[1], nameMatch[1]))
				}
				if placeholderMatch := regexp.MustCompile(`(?i)placeholder=["']([^"']+)["']`).FindStringSubmatch(input); len(placeholderMatch) > 1 {
					attrs = append(attrs, fmt.Sprintf("placeholder='%s' → selector: input[placeholder='%s']", placeholderMatch[1], placeholderMatch[1]))
				}
				if dataMatch := regexp.MustCompile(`(?i)data-[^=]+=["']([^"']+)["']`).FindStringSubmatch(input); len(dataMatch) > 0 {
					dataAttr := regexp.MustCompile(`(?i)(data-[^=]+)=`).FindStringSubmatch(input)
					if len(dataAttr) > 1 {
						attrs = append(attrs, fmt.Sprintf("%s → selector: input[%s]", dataAttr[1], dataAttr[1]))
					}
				}
				if len(attrs) > 0 {
					info.WriteString(fmt.Sprintf("  Input %d:%s %s\n", i+1, hint, strings.Join(attrs, ", ")))
				} else {
					info.WriteString(fmt.Sprintf("  Input %d: %s (no clear identifiers)\n", i+1, input[:min(80, len(input))]))
				}
			}
		}
		if len(inputs) > 20 {
			info.WriteString(fmt.Sprintf("  ... and %d more inputs\n", len(inputs)-20))
		}
	}

	buttonRe := regexp.MustCompile(`(?i)<button[^>]*>.*?</button>`)
	buttons := buttonRe.FindAllString(html, -1)

	buttonLikeDivRe := regexp.MustCompile(`(?i)<div[^>]*(?:role=["']button["']|class=["'][^"']*button[^"']*["'])[^>]*>`)
	buttonLikeSpanRe := regexp.MustCompile(`(?i)<span[^>]*(?:role=["']button["']|class=["'][^"']*button[^"']*["'])[^>]*>`)
	buttonLikeARe := regexp.MustCompile(`(?i)<a[^>]*(?:role=["']button["']|class=["'][^"']*button[^"']*["'])[^>]*>`)
	buttonLikeDiv := buttonLikeDivRe.FindAllString(html, -1)
	buttonLikeSpan := buttonLikeSpanRe.FindAllString(html, -1)
	buttonLikeA := buttonLikeARe.FindAllString(html, -1)
	totalButtonLike := len(buttonLikeDiv) + len(buttonLikeSpan) + len(buttonLikeA)

	if len(buttons) > 0 || totalButtonLike > 0 {
		info.WriteString(fmt.Sprintf("\nButtons found: %d (plus %d button-like elements)\n", len(buttons), totalButtonLike))
		for i, btn := range buttons {
			if i < 10 {
				// Extract attributes
				var attrs []string
				if idMatch := regexp.MustCompile(`(?i)id=["']([^"']+)["']`).FindStringSubmatch(btn); len(idMatch) > 1 {
					attrs = append(attrs, fmt.Sprintf("id='%s' → selector: #%s", idMatch[1], idMatch[1]))
				}
				if classMatch := regexp.MustCompile(`(?i)class=["']([^"']+)["']`).FindStringSubmatch(btn); len(classMatch) > 1 {

					classes := strings.Fields(classMatch[1])
					if len(classes) > 0 {
						attrs = append(attrs, fmt.Sprintf("class='%s' → selector: .%s", classes[0], classes[0]))
					}
				}

				textRe := regexp.MustCompile(`(?i)>([^<]+)<`)
				if textMatch := textRe.FindStringSubmatch(btn); len(textMatch) > 1 {
					text := strings.TrimSpace(textMatch[1])
					if text != "" {
						attrs = append(attrs, fmt.Sprintf("text='%s'", text))
					}
				}
				if len(attrs) > 0 {
					info.WriteString(fmt.Sprintf("  Button %d: %s\n", i+1, strings.Join(attrs, ", ")))
				}
			}
		}
	}

	idRe := regexp.MustCompile(`(?i)id=["']([^"']*(?:from|to|origin|destination|calculate|submit|co2|result)[^"']*)["']`)
	ids := idRe.FindAllStringSubmatch(html, -1)
	if len(ids) > 0 {
		info.WriteString("\nRelevant IDs found:\n")
		for i, match := range ids {
			if i < 10 {
				info.WriteString(fmt.Sprintf("  - #%s\n", match[1]))
			}
		}
	}

	result := info.String()
	if result == "" {
		return "No clear form structure detected. Look for input fields, buttons, and form elements in the HTML."
	}

	return result
}

// extractSelectorsFromFormInfo extracts all valid selectors mentioned in the form info string
func extractSelectorsFromFormInfo(formInfo string) []string {
	var selectors []string

	selectorRe := regexp.MustCompile(`selector:\s*([^\n,]+)`)
	matches := selectorRe.FindAllStringSubmatch(formInfo, -1)
	for _, match := range matches {
		if len(match) > 1 {
			selector := strings.TrimSpace(match[1])
			if selector != "" {
				selectors = append(selectors, selector)
			}
		}
	}

	idRe := regexp.MustCompile(`id=['"]([^'"]+)['"]`)
	idMatches := idRe.FindAllStringSubmatch(formInfo, -1)
	for _, match := range idMatches {
		if len(match) > 1 {
			selectors = append(selectors, "#"+match[1])
		}
	}

	nameRe := regexp.MustCompile(`name=['"]([^'"]+)['"]`)
	nameMatches := nameRe.FindAllStringSubmatch(formInfo, -1)
	for _, match := range nameMatches {
		if len(match) > 1 {
			selectors = append(selectors, fmt.Sprintf("input[name='%s']", match[1]))
		}
	}
	return selectors
}

// isSelectorValid checks if a selector matches any of the valid selectors or common patterns
func isSelectorValid(selector string, validSelectors []string) bool {

	if selector == "body" {
		return true
	}

	for _, valid := range validSelectors {
		if selector == valid {
			return true
		}

		if strings.Contains(selector, valid) {
			return true
		}
	}

	validPatterns := []string{
		"input[",
		"button[",
		"select[",
		"textarea[",
		"[data-",
		"#",
		".",
	}
	for _, pattern := range validPatterns {
		if strings.Contains(selector, pattern) {
			return true
		}
	}
	return false
}

func parseFromTo(instructions string) (string, string) {

	fromRe := regexp.MustCompile(`(?i)\bfrom\b\s+(?:field\s+)?(?:with\s+)?([A-Za-z][A-Za-z\s\-'"]+)`)
	toRe := regexp.MustCompile(`(?i)\bto\b\s+(?:field\s+)?(?:with\s+)?([A-Za-z][A-Za-z\s\-'"]+)`)

	fromMatch := fromRe.FindStringSubmatch(instructions)
	toMatch := toRe.FindStringSubmatch(instructions)

	from := ""
	to := ""
	if len(fromMatch) > 1 {
		from = strings.TrimSpace(strings.Trim(fromMatch[1], " ,.;\"'"))
	}
	if len(toMatch) > 1 {
		to = strings.TrimSpace(strings.Trim(toMatch[1], " ,.;\"'"))
	}
	return from, to
}

func buildEcotreeActions(instructions string) ([]map[string]interface{}, error) {
	from, to := parseFromTo(instructions)
	if from == "" || to == "" {
		return nil, fmt.Errorf("unable to parse from/to locations from instructions: %q", instructions)
	}

	fromSelector := "input[name='From']"
	toSelector := "input[name='To']"

	calcSelector := "form button[type='submit'], form button.btn-primary, button[type='submit']:visible, .btn.btn-primary.hover-arrow, button.btn-primary, text=/calculate.*emissions/i"
	resultSelector := "text=/kg.*co2/i, text=/CO2.*emissions/i, .result, [data-testid*='result'], [class*='result']"

	actions := []map[string]interface{}{
		{"type": "wait", "selector": "body", "timeout": 5},
		{"type": "wait", "wait_for": fromSelector, "timeout": 10},
		{"type": "fill", "selector": fromSelector, "value": from},
		{"type": "wait", "selector": "body", "timeout": 1},
		{"type": "fill", "selector": toSelector, "value": to},
		{"type": "wait", "selector": "body", "timeout": 1},
		{"type": "click", "selector": calcSelector},
		{"type": "wait", "selector": "body", "timeout": 3},
		{"type": "wait", "wait_for": resultSelector, "timeout": 20},
		{"type": "wait", "selector": "body", "timeout": 2},
		{"type": "extract", "extract": map[string]string{
			"co2_emissions": resultSelector,
		}},
	}
	return actions, nil
}

func (s *MCPKnowledgeServer) saveProgress(path string, status string, step int, total int, html string) {
	progressPath := path + ".progress"
	prog := map[string]interface{}{
		"status": status,
		"step":   step,
		"total":  total,
		"html":   html,
	}
	data, _ := json.Marshal(prog)
	_ = os.WriteFile(progressPath, data, 0644)

	if s.fileStorage != nil {
		filename := filepath.Base(progressPath)
		file := &StoredFile{
			Filename:    filename,
			Content:     data,
			ContentType: "application/json",
			Size:        int64(len(data)),
		}
		if err := s.fileStorage.StoreFile(file); err != nil {
			log.Printf("⚠️ [MCP-KNOWLEDGE] Failed to store progress in Redis: %v", err)
		} else {
			log.Printf("💾 [MCP-KNOWLEDGE] Stored progress in Redis: %s", filename)
		}
	}
}

// generateActionsFromInstructions uses LLM to generate browser actions from natural language instructions
func (s *MCPKnowledgeServer) generateActionsFromInstructions(ctx context.Context, url, instructions, pageHTML string) ([]map[string]interface{}, error) {

	formInfo := extractFormStructure(pageHTML)

	htmlPreview := pageHTML
	if len(htmlPreview) > 30000 {

		formStart := strings.Index(strings.ToLower(htmlPreview), "<form")
		if formStart != -1 && formStart < 30000 {

			start := max(0, formStart-3000)
			end := min(len(htmlPreview), formStart+27000)
			htmlPreview = htmlPreview[start:end]
			if start > 0 {
				htmlPreview = "... (earlier content truncated) ...\n" + htmlPreview
			}
			if end < len(pageHTML) {
				htmlPreview = htmlPreview + "\n... (later content truncated) ..."
			}
		} else {

			htmlPreview = htmlPreview[:30000] + "\n... (truncated)"
		}
	}

	selectorList := extractSelectorsFromFormInfo(formInfo)
	selectorListStr := ""
	if len(selectorList) > 0 {
		selectorListStr = "\n\nAVAILABLE SELECTORS (copy these EXACTLY - do not invent new ones):\n"
		for i, sel := range selectorList {
			if i < 20 {
				selectorListStr += fmt.Sprintf("- %s\n", sel)
			}
		}
		if len(selectorList) > 20 {
			selectorListStr += fmt.Sprintf("... and %d more\n", len(selectorList)-20)
		}
	} else {
		selectorListStr = "\n\nWARNING: No clear selectors found. Look in the Form Elements section below for selectors.\n"
	}

	prompt := fmt.Sprintf(`You are an expert browser automation agent. Your goal is to generate a JSON array of actions to achieve the user's mission.
	
URL: %s
User Goal: %s
%s
Available Form Elements:
%s

STRATEGY RULES:
1. **STABILITY FIRST**: Use #id, name='...', or placeholder='...' selectors. They are more stable than classes or text.
2. **SPELLING MATTERS**: If you click an autocomplete suggestion, use the EXACT text you saw in the HTML or dropdown. (e.g., if you typed "Paris", the dropdown might say "Paris - Charles de Gaulle (CDG)").
3. **DROPDOWN FLOW**: To handle autocompletes:
   a. "fill" the input
   b. "wait" 1 second
   c. "click" the correctly spelled option from the list.
4. **CO2/CALCULATORS**: For flight calculators, look for "From", "To", "One-way/Return", and "Passengers".
5. **NO HALLUCIDATIONS**: ONLY use selectors or text fragments actually visible in the provided HTML or form elements list.

JSON FORMAT:
Return ONLY a JSON array [{}, {}].
Allowed "type": wait, fill, click, select, extract.
Selector: Use CSS or "text=Exact Text". Regex "text=/.../i" is allowed only if exact match fails.

EXAMPLE:
[{"type":"wait","selector":"body","timeout":2},{"type":"fill","selector":"#from","value":"Paris"},{"type":"wait","timeout":1},{"type":"click","selector":"text=/Charles de Gaulle/i"}]`, url, instructions, selectorListStr, formInfo)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	log.Printf("📞 [BROWSE-WEB] Calling LLM with prompt size: %d bytes (HTML preview: %d bytes)", len(prompt), len(htmlPreview))
	startTime := time.Now()

	response, err := s.llmClient.callLLMWithContextAndPriority(ctx, prompt, PriorityHigh)

	duration := time.Since(startTime)
	log.Printf("⏱️ [BROWSE-WEB] LLM call completed in %v", duration)

	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("LLM call timed out after %v (prompt size: %d bytes)", duration, len(prompt))
		}
		return nil, fmt.Errorf("LLM call failed (after %v): %w", duration, err)
	}

	log.Printf("📝 [BROWSE-WEB] LLM raw response length: %d bytes", len(response))
	if len(response) > 1000 {
		start := response[:500]
		end := response[len(response)-500:]
		log.Printf("📝 [BROWSE-WEB] LLM response START (first 500 chars): %s", start)
		log.Printf("📝 [BROWSE-WEB] LLM response END (last 500 chars): %s", end)
	} else {
		log.Printf("📝 [BROWSE-WEB] LLM raw response: %s", response)
	}

	jsonStr := extractJSONFromResponse(response)
	if jsonStr == "" {
		log.Printf("⚠️ [BROWSE-WEB] Failed to extract JSON from LLM response. Full response length: %d", len(response))

		if err := os.WriteFile("/tmp/llm_response_debug.txt", []byte(response), 0644); err == nil {
			log.Printf("💾 [BROWSE-WEB] Saved full LLM response to /tmp/llm_response_debug.txt for debugging")
		}

		if strings.Contains(response, "[") {
			log.Printf("🔍 [BROWSE-WEB] Response contains '[', attempting manual extraction...")

			firstBracket := strings.Index(response, "[")
			lastBracket := strings.LastIndex(response, "]")
			if firstBracket != -1 && lastBracket != -1 && lastBracket > firstBracket {
				potentialJSON := response[firstBracket : lastBracket+1]
				log.Printf("🔍 [BROWSE-WEB] Found potential JSON (length: %d), first 200 chars: %s", len(potentialJSON), potentialJSON[:min(200, len(potentialJSON))])

				if _, err := tryParseJSON(potentialJSON); err == nil {
					log.Printf("✅ [BROWSE-WEB] Successfully parsed extracted JSON!")
					jsonStr = potentialJSON
				} else {
					log.Printf("❌ [BROWSE-WEB] Extracted JSON still failed to parse: %v", err)
				}
			}
		}
		if jsonStr == "" {
			return nil, fmt.Errorf("no JSON found in LLM response")
		}
	}

	log.Printf("✅ [BROWSE-WEB] Extracted JSON from LLM response (length: %d)", len(jsonStr))

	// Parse actions
	var actions []map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &actions); err != nil {
		return nil, fmt.Errorf("failed to parse LLM-generated actions: %w", err)
	}

	validSelectors := extractSelectorsFromFormInfo(formInfo)
	log.Printf("🔍 [BROWSE-WEB] Valid selectors from form structure: %v", validSelectors)

	log.Printf("📋 [BROWSE-WEB] LLM generated %d actions:", len(actions))
	for i, action := range actions {
		actionType, _ := action["type"].(string)
		selector, _ := action["selector"].(string)
		waitFor, _ := action["wait_for"].(string)
		value, _ := action["value"].(string)

		if selector != "" && selector != "body" {
			if !isSelectorValid(selector, validSelectors) {
				log.Printf("⚠️ [BROWSE-WEB] Action [%d] uses potentially invalid selector: %s (not found in form structure)", i+1, selector)
			}
		}
		log.Printf("  [%d] %s: selector=%s, wait_for=%s, value=%s", i+1, actionType, selector, waitFor, value)
	}

	return actions, nil
}

// extractJSONFromResponse extracts JSON array from LLM response (handles markdown code blocks)
func extractJSONFromResponse(response string) string {
	trimmed := strings.TrimSpace(response)

	prefixes := []string{"SOURCES:", "Here is", "Here's", "The JSON", "JSON:", "Actions:", "Here are", "Koffie:", "Koffie", "Coffee:", "Coffee"}
	jsonCandidate := trimmed
	for _, prefix := range prefixes {
		lower := strings.ToLower(jsonCandidate)
		pLower := strings.ToLower(prefix)
		if strings.HasPrefix(lower, pLower) {
			after := jsonCandidate[len(prefix):]
			idx := strings.Index(after, "[")
			if idx != -1 {
				jsonCandidate = after[idx:]
				break
			}
		}
	}

	if strings.Contains(jsonCandidate, "```") {
		re := regexp.MustCompile("(?s)```(?:json)?\n?(.*?)\n?```")
		match := re.FindStringSubmatch(jsonCandidate)
		if len(match) > 1 {
			return strings.TrimSpace(match[1])
		}
	}

	start := strings.Index(jsonCandidate, "[")
	end := strings.LastIndex(jsonCandidate, "]")
	if start != -1 && end != -1 && end > start {
		return strings.TrimSpace(jsonCandidate[start : end+1])
	}

	return jsonCandidate
}

// tryParseJSON attempts to parse JSON, and if it fails, tries to repair common LLM errors
func tryParseJSON(jsonStr string) (string, error) {
	var test interface{}

	if err := json.Unmarshal([]byte(jsonStr), &test); err == nil {
		return jsonStr, nil
	}

	cleaned := strings.TrimSpace(jsonStr)
	if err := json.Unmarshal([]byte(cleaned), &test); err == nil {
		return cleaned, nil
	}

	repaired := repairJSON(cleaned)
	if err := json.Unmarshal([]byte(repaired), &test); err == nil {
		return repaired, nil
	}

	return "", fmt.Errorf("failed to parse JSON after repair")
}

// repairJSON attempts to fix common LLM mistakes like unclosed quotes or missing brackets
func repairJSON(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}

	lines := strings.Split(s, "\n")
	for i, line := range lines {
		quoteCount := strings.Count(line, "\"")
		if quoteCount%2 != 0 {

			trimmed := strings.TrimRight(line, " \t\r,}]")
			lines[i] = trimmed + "\"" + line[len(trimmed):]
		}
	}
	s = strings.Join(lines, "\n")

	s = regexp.MustCompile(`,(\s*[\]\}])`).ReplaceAllString(s, "$1")

	depth := 0
	for _, char := range s {
		if char == '{' {
			depth++
		} else if char == '}' {
			depth--
		}
	}
	for depth > 0 {
		s += "}"
		depth--
	}

	depth = 0
	for _, char := range s {
		if char == '[' {
			depth++
		} else if char == ']' {
			depth--
		}
	}
	for depth > 0 {
		s += "]"
		depth--
	}

	return s
}

// buildActionableSnapshot extracts only actionable elements (forms, inputs, selects,
// buttons, labels, anchors) from a larger HTML snapshot so the LLM can focus
// on navigation interactions. Falls back to cleaned full HTML if nothing found.
func (s *MCPKnowledgeServer) buildActionableSnapshot(html string) string {

	reForm := regexp.MustCompile(`(?is)<form[^>]*>.*?</form>`)
	forms := reForm.FindAllString(html, -1)
	if len(forms) > 0 {

		reControl := regexp.MustCompile(`(?is)<(?:input|select|button|textarea|label|a)[^>]*?(?:>.*?</(?:input|select|button|textarea|label|a)>|/>)`)

		bestScore := -1
		bestForm := ""
		bestControls := []string{}

		for _, f := range forms {
			controls := reControl.FindAllString(f, -1)
			score := len(controls)
			lower := strings.ToLower(f)
			if strings.Contains(lower, "flight_calculator") {
				score += 10
			}
			if strings.Contains(lower, "flight") {
				score += 3
			}
			if strings.Contains(lower, "calculator") {
				score += 3
			}
			if strings.Contains(lower, "from") && strings.Contains(lower, "to") {
				score += 2
			}

			if score > bestScore {
				bestScore = score
				bestForm = f
				bestControls = controls
			}
		}

		if len(bestControls) > 0 {
			snippet := strings.Join(bestControls, "\n")
			if len(snippet) > 10000 {
				snippet = snippet[:10000] + "...(truncated)"
			}
			return cleanHTMLForPlanning(snippet)
		}
		if bestForm != "" {
			return cleanHTMLForPlanning(bestForm)
		}

		joined := strings.Join(forms, "\n")
		return cleanHTMLForPlanning(joined)
	}

	re := regexp.MustCompile(`(?is)<(?:input|select|button|textarea|label|a)[^>]*?(?:>.*?</(?:input|select|button|textarea|label|a)>|/>)`)
	matches := re.FindAllString(html, -1)
	if len(matches) > 0 {
		snippet := strings.Join(matches, "\n")
		if len(snippet) > 20000 {
			snippet = snippet[:20000] + "...(truncated)"
		}
		return cleanHTMLForPlanning(snippet)
	}

	return cleanHTMLForPlanning(html)
}

// sanitizeNavigationScript applies general-purpose fixes to LLM-produced
// navigation JS by using the actionable HTML snapshot and goal text.
func (s *MCPKnowledgeServer) sanitizeNavigationScript(js, actionableHTML, goal string) string {
	if strings.TrimSpace(js) == "" {
		return js
	}

	selectNames := map[string]struct{}{}
	reSelect := regexp.MustCompile(`(?is)<select[^>]*\bname=['"]([^'"]+)['"]`)
	for _, m := range reSelect.FindAllStringSubmatch(actionableHTML, -1) {
		if len(m) > 1 {
			selectNames[m[1]] = struct{}{}
		}
	}
	reClickInputValue := regexp.MustCompile(`page\.click\(\s*['"]input\[name=['"]([^'"]+)['"]\]\[value=['"]([^'"]+)['"]\]['"]\s*\)`)
	js = reClickInputValue.ReplaceAllStringFunc(js, func(m string) string {
		subs := reClickInputValue.FindStringSubmatch(m)
		if len(subs) != 3 {
			return m
		}
		name := subs[1]
		value := subs[2]
		if _, ok := selectNames[name]; ok {
			return fmt.Sprintf("await page.selectOption('select[name=\"%s\"]', '%s');", name, value)
		}
		return m
	})

	goalLower := strings.ToLower(goal)
	needsSubmit := strings.Contains(goalLower, "calculate") || strings.Contains(goalLower, "submit") || strings.Contains(goalLower, "search") || strings.Contains(goalLower, "find")
	if needsSubmit {
		hasSubmit := strings.Contains(js, "type=\\\"submit\\\"") || strings.Contains(js, "type='submit'") || strings.Contains(js, "keyboard.press('Enter')") || strings.Contains(js, "keyboard.press(\"Enter\")")
		if !hasSubmit {
			js = strings.TrimSpace(js) + "\n" + "await page.locator('input[type=\"submit\"], button[type=\"submit\"]').first().click();\nawait page.waitForTimeout(3000);"
			log.Printf("🧽 [MCP-SMART-SCRAPE] Added generic submit click to navigation script")
		}

		const fallbackMarker = "MCP_SUBMIT_FALLBACK"
		if !strings.Contains(js, fallbackMarker) {
			fallback := "\n" + "/* " + fallbackMarker + " */\n" +
				"const __mcpInitialUrl = page.url();\n" +
				"try { await page.waitForLoadState('networkidle'); } catch (e) {}\n" +
				"await page.waitForTimeout(1500);\n" +
				"const __mcpHasResults = await page.locator('[id*=\"result\"], .result, .results, [data-testid*=\"result\"]').first().count();\n" +
				"const __mcpHasForm = await page.locator('form').first().count();\n" +
				"if (page.url() === __mcpInitialUrl && __mcpHasResults === 0 && __mcpHasForm > 0) {\n" +
				"  try { await page.locator('input[type=\"submit\"], button[type=\"submit\"], .submit-form, .btn-primary').first().click(); } catch (e) {}\n" +
				"  try { await page.waitForLoadState('networkidle'); } catch (e) {}\n" +
				"  await page.waitForTimeout(1500);\n" +
				"}\n" +
				"if (page.url() === __mcpInitialUrl && __mcpHasResults === 0 && __mcpHasForm > 0) {\n" +
				"  try { await page.locator('form').first().evaluate((f) => f.submit()); } catch (e) {}\n" +
				"  try { await page.waitForLoadState('networkidle'); } catch (e) {}\n" +
				"  await page.waitForTimeout(1500);\n" +
				"}\n" +
				"if (page.url() === __mcpInitialUrl && __mcpHasResults === 0 && __mcpHasForm > 0) {\n" +
				"  try { await page.keyboard.press('Enter'); } catch (e) {}\n" +
				"  await page.waitForTimeout(1500);\n" +
				"}\n"
			js = strings.TrimSpace(js) + fallback
			log.Printf("🧽 [MCP-SMART-SCRAPE] Added submit fallback verification to navigation script")
		}
	}

	return js
}

// buildExtractionSnapshot reduces HTML to likely result sections based on ids mentioned in the goal.
// Falls back to a truncated cleaned HTML when no matching ids are found.
func (s *MCPKnowledgeServer) buildExtractionSnapshot(html string, goal string) string {

	ids := map[string]struct{}{}
	reGoalID := regexp.MustCompile(`(?i)id=([a-z0-9_-]+)`)
	for _, m := range reGoalID.FindAllStringSubmatch(goal, -1) {
		if len(m) > 1 {
			ids[m[1]] = struct{}{}
		}
	}

	if len(ids) > 0 {
		blocks := []string{}
		seen := map[string]struct{}{}
		for id := range ids {

			reByID := regexp.MustCompile(fmt.Sprintf(`(?is)<[^>]*\bid=['"]%s['"][^>]*>.*?</[^>]+>`, regexp.QuoteMeta(id)))
			if match := reByID.FindString(html); match != "" {
				if _, ok := seen[match]; !ok {
					blocks = append(blocks, match)
					seen[match] = struct{}{}
				}
				continue
			}

			needle := fmt.Sprintf("id=\"%s\"", id)
			idx := strings.Index(html, needle)
			if idx == -1 {
				needle = fmt.Sprintf("id='%s'", id)
				idx = strings.Index(html, needle)
			}
			if idx != -1 {
				start := idx - 1000
				if start < 0 {
					start = 0
				}
				end := idx + 2000
				if end > len(html) {
					end = len(html)
				}
				window := html[start:end]
				if _, ok := seen[window]; !ok {
					blocks = append(blocks, window)
					seen[window] = struct{}{}
				}
			}
		}

		if len(blocks) > 0 {
			snippet := strings.Join(blocks, "\n")
			if len(snippet) > 20000 {
				snippet = snippet[:20000] + "...(truncated)"
			}
			return cleanHTMLForPlanning(snippet)
		}
	}

	snippet := cleanHTMLForPlanning(html)
	if len(snippet) > 20000 {
		snippet = snippet[:20000] + "...(truncated)"
	}
	return snippet
}
