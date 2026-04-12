package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"hdn/playwright"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

func (s *MCPKnowledgeServer) getScrapeStatus(ctx context.Context, jobID string) (interface{}, error) {
	scraperURL := os.Getenv("PLAYWRIGHT_SCRAPER_URL")
	if scraperURL == "" {
		scraperURL = "http://playwright-scraper.agi.svc.cluster.local:8080"
	}

	jobURL := fmt.Sprintf("%s/scrape/job?job_id=%s", scraperURL, jobID)
	req, err := http.NewRequestWithContext(ctx, "GET", jobURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var job struct {
		ID          string                 `json:"id"`
		Status      string                 `json:"status"`
		Result      map[string]interface{} `json:"result,omitempty"`
		Error       string                 `json:"error,omitempty"`
		CompletedAt *time.Time             `json:"completed_at,omitempty"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&job); err != nil {
		return nil, fmt.Errorf("failed to decode job status: %v", err)
	}

	// Format for MCP
	var text string
	switch job.Status {
	case "completed":
		if job.Result != nil {
			resultBytes, _ := json.MarshalIndent(job.Result, "", "  ")
			text = fmt.Sprintf("Scrape Results for %s:\n%s", jobID, string(resultBytes))
		} else {
			text = fmt.Sprintf("Scrape Results for %s: (empty)", jobID)
		}
	case "failed":
		text = fmt.Sprintf("Scrape job %s failed: %v", jobID, job.Error)
	case "running", "pending":
		text = fmt.Sprintf("Scrape job %s is still %s.", jobID, job.Status)
	default:
		text = fmt.Sprintf("Scrape job %s has status: %s", jobID, job.Status)
	}

	return map[string]interface{}{
		"content": []map[string]interface{}{
			{
				"type": "text",
				"text": text,
			},
		},
		"result": job.Result,
		"status": job.Status,
	}, nil
}

// scrapeWithConfig delegates to the external Playwright scraper service with async job queue
func (s *MCPKnowledgeServer) scrapeWithConfig(ctx context.Context, url, instructions, tsConfig string, async bool, extractions map[string]string, getHTML bool, cookies []interface{}) (interface{}, error) {
	log.Printf("📝 [MCP-SCRAPE] Received TypeScript config (%d bytes) and %d extractions", len(tsConfig), len(extractions))

	scraperURL := os.Getenv("PLAYWRIGHT_SCRAPER_URL")
	if scraperURL == "" {

		scraperURL = "http://playwright-scraper.agi.svc.cluster.local:8080"
	}

	startReq := map[string]interface{}{
		"url":               url,
		"instructions":      instructions,
		"typescript_config": tsConfig,
		"extractions":       extractions,
		"get_html":          getHTML,
		"cookies":           cookies,
	}
	startReqJSON, err := json.Marshal(startReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %v", err)
	}

	log.Printf("🚀 [MCP-SCRAPE] Starting scrape job at %s/scrape/start", scraperURL)
	resp, err := http.Post(scraperURL+"/scrape/start", "application/json", bytes.NewReader(startReqJSON))
	if err != nil {
		return nil, fmt.Errorf("failed to start scrape job: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("scraper service returned %d: %s", resp.StatusCode, string(body))
	}

	var startResp struct {
		JobID     string    `json:"job_id"`
		Status    string    `json:"status"`
		CreatedAt time.Time `json:"created_at"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&startResp); err != nil {
		return nil, fmt.Errorf("failed to decode start response: %v", err)
	}

	if async {
		log.Printf("🚀 [MCP-SCRAPE] Async requested, returning job ID %s immediately", startResp.JobID)
		return map[string]interface{}{
			"content": []map[string]interface{}{
				{
					"type": "text",
					"text": fmt.Sprintf("Scrape job started. Job ID: %s. Use get_scrape_status to check results.", startResp.JobID),
				},
				{
					"type": "text",
					"text": fmt.Sprintf("JobID: %s", startResp.JobID),
				},
			},
			"job_id": startResp.JobID,
			"status": "pending",
		}, nil
	}

	log.Printf("⏳ [MCP-SCRAPE] Job %s started, polling for results...", startResp.JobID)

	pollTimeout := 300 * time.Second
	pollInterval := 2 * time.Second
	startTime := time.Now()

	for {
		if time.Since(startTime) > pollTimeout {
			return nil, fmt.Errorf("scrape job timed out after %v", pollTimeout)
		}

		jobURL := fmt.Sprintf("%s/scrape/job?job_id=%s", scraperURL, startResp.JobID)
		jobResp, err := http.Get(jobURL)
		if err != nil {
			return nil, fmt.Errorf("failed to get job status: %v", err)
		}

		var job struct {
			ID          string                 `json:"id"`
			Status      string                 `json:"status"`
			Result      map[string]interface{} `json:"result,omitempty"`
			Error       string                 `json:"error,omitempty"`
			CompletedAt *time.Time             `json:"completed_at,omitempty"`
		}

		jobData, err := io.ReadAll(io.LimitReader(jobResp.Body, 10*1024*1024))
		jobResp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("failed to read job status: %v", err)
		}

		if err := json.Unmarshal(jobData, &job); err != nil {
			return nil, fmt.Errorf("failed to decode job status: %v", err)
		}

		switch job.Status {
		case "completed":
			duration := time.Since(startTime)
			log.Printf("✅ [MCP-SCRAPE] Job %s completed in %v", startResp.JobID, duration)

			if screenshotB64, ok := job.Result["screenshot"].(string); ok && screenshotB64 != "" {
				go func() {
					raw := screenshotB64
					if idx := strings.Index(raw, ","); idx >= 0 {
						raw = raw[idx+1:]
					}
					if decoded, err := base64.StdEncoding.DecodeString(raw); err == nil {

						s.screenshotMu.Lock()
						s.latestScreenshot = decoded
						s.screenshotMu.Unlock()
						log.Printf("📸 [MCP-SCRAPE] Screenshot stored in memory (%d bytes)", len(decoded))

						projectRoot := os.Getenv("AGI_PROJECT_ROOT")
						if projectRoot == "" {
							if wd, err := os.Getwd(); err == nil {
								projectRoot = wd
							}
						}
						artifactsDir := filepath.Join(projectRoot, "artifacts")
						if artifactsDir == "/artifacts" || artifactsDir == "artifacts" {
							artifactsDir = "artifacts"
						}
						os.MkdirAll(artifactsDir, 0755)
						path := filepath.Join(artifactsDir, "latest_screenshot.png")
						if err := os.WriteFile(path, decoded, 0644); err != nil {
							log.Printf("⚠️ [MCP-SCRAPE] Failed to save screenshot to disk: %v", err)
						} else {
							log.Printf("📸 [MCP-SCRAPE] Screenshot saved to %s (%d bytes)", path, len(decoded))
						}
					}
				}()
			}

			if len(extractions) > 0 && getHTML {

				pageContent := ""
				if html, ok := job.Result["cleaned_html"].(string); ok {
					pageContent = html
				} else if html, ok := job.Result["raw_html"].(string); ok {
					pageContent = html
				} else if html, ok := job.Result["page_content"].(string); ok {
					pageContent = html
				}

				if pageContent != "" {
					log.Printf("🔍 [MCP-SCRAPE] Applying %d extraction patterns to page content (%d chars)", len(extractions), len(pageContent))
					for key, pattern := range extractions {
						re, err := regexp.Compile(pattern)
						if err != nil {
							log.Printf("⚠️  [MCP-SCRAPE] Invalid regex pattern for '%s': %v", key, err)
							continue
						}

						matches := re.FindAllStringSubmatch(pageContent, -1)
						if len(matches) > 0 {

							if len(matches[0]) > 1 {
								// If there are capture groups, join them
								var extracted []string
								for _, match := range matches {
									if len(match) > 1 {

										found := false
										for i := len(match) - 1; i >= 1; i-- {
											if match[i] != "" {
												extracted = append(extracted, match[i])
												found = true
												break
											}
										}
										if !found {
											extracted = append(extracted, match[0])
										}
									}
								}
								if len(extracted) > 0 {
									job.Result[key] = strings.Join(extracted, "\n")
									log.Printf("✅ [MCP-SCRAPE] Extracted '%s': found %d matches", key, len(extracted))
								}
							}
						} else {
							log.Printf("⚠️  [MCP-SCRAPE] No matches found for extraction pattern '%s'", key)
						}
					}
				}
			}

			resultJSON, _ := json.MarshalIndent(job.Result, "", "  ")
			return map[string]interface{}{
				"content": []map[string]interface{}{
					{
						"type": "text",
						"text": fmt.Sprintf("Scrape Results:\n%s", string(resultJSON)),
					},
				},
				"result": job.Result,
				"job_id": startResp.JobID,
			}, nil

		case "failed":
			return nil, fmt.Errorf("scrape job failed: %s", job.Error)

		case "pending", "running":

			log.Printf("⏳ [MCP-SCRAPE] Job %s status: %s (elapsed: %v)", startResp.JobID, job.Status, time.Since(startTime))
			time.Sleep(pollInterval)

		default:
			return nil, fmt.Errorf("unknown job status: %s", job.Status)
		}
	}
}

// parsePlaywrightTypeScript extracts operations from TypeScript/Playwright test code
// This wraps the shared parser and converts to internal types
func parsePlaywrightTypeScript(tsConfig, defaultURL string) ([]PlaywrightOperation, error) {

	sharedOps, err := playwright.ParseTypeScript(tsConfig, defaultURL)
	if err != nil {
		return nil, err
	}

	// Convert to internal types
	var operations []PlaywrightOperation
	for _, op := range sharedOps {
		operations = append(operations, PlaywrightOperation{
			Type:     op.Type,
			Selector: op.Selector,
			Value:    op.Value,
			Role:     op.Role,
			RoleName: op.RoleName,
			Text:     op.Text,
			Timeout:  op.Timeout,
		})
	}

	return operations, nil

}

func (s *MCPKnowledgeServer) executeSmartScrape(ctx context.Context, url string, goal string, userConfig *ScrapeConfig) (interface{}, error) {
	log.Printf("🧠 [MCP-SMART-SCRAPE] Starting smart scrape for %s with goal: %s", url, goal)

	var err error
	// 0. Fast-path check: if the user provided an explicit script, we skip the initial fetch and LLM planning.
	var config *ScrapeConfig
	bypassedLLM := false
	if userConfig != nil && userConfig.TypeScriptConfig != "" {
		log.Printf("⚡ [MCP-SMART-SCRAPE] Fast-path: User provided explicit TypeScript script, bypassing initial fetch and LLM planning")
		bypassedLLM = true

		tsConfig := userConfig.TypeScriptConfig

		isSimpleScript := strings.Contains(tsConfig, "page.goto") && !strings.Contains(tsConfig, "page.click") && !strings.Contains(tsConfig, "bypass")
		needsConsentBypass := strings.Contains(url, "amazon") || strings.Contains(url, "ebay") || strings.Contains(url, "google") || strings.Contains(url, "yahoo")

		if isSimpleScript && needsConsentBypass {
			log.Printf("🍪 [MCP-SMART-SCRAPE] Simple fast-path script detected for %s, auto-injecting bypassConsent for reliability", url)

			if strings.Contains(tsConfig, "await page.goto") {
				lines := strings.Split(tsConfig, "\n")
				for i, line := range lines {
					if strings.Contains(line, "page.goto") {
						lines[i] = line + "\n  await page.bypassConsent();"
						break
					}
				}
				tsConfig = strings.Join(lines, "\n")
			} else {
				tsConfig = tsConfig + "\nawait page.bypassConsent();"
			}
		}

		config = &ScrapeConfig{
			TypeScriptConfig: tsConfig,
			Extractions:      make(map[string]string),
		}
		if userConfig.Extractions != nil {
			for k, v := range userConfig.Extractions {
				config.Extractions[k] = v
			}
		}
	}

	var innerResult map[string]interface{}
	var cleanedHTML string
	var capturedCookies []interface{}

	if !bypassedLLM {

		log.Printf("📥 [MCP-SMART-SCRAPE] Fetching page content to plan scrape...")

		htmlResultRaw, err := s.scrapeWithConfig(ctx, url, goal, "", false, nil, true, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch page content: %v", err)
		}

		htmlResult, ok := htmlResultRaw.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("unexpected internal result format")
		}

		innerResult, ok = htmlResult["result"].(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("could not find result in scrape response")
		}

		cleanedHTML, ok = innerResult["cleaned_html"].(string)
		if !ok || cleanedHTML == "" {
			return nil, fmt.Errorf("scraper did not return cleaned_html")
		}

		if isConsentPage(cleanedHTML) {
			log.Printf("🍪 [MCP-SMART-SCRAPE] Attempting auto-consent bypass...")
			consentTS := "await page.bypassConsent();"
			bypassResult, err := s.scrapeWithConfig(ctx, url, goal, consentTS, false, nil, true, nil)
			if err != nil {
				log.Printf("⚠️ [MCP-SMART-SCRAPE] Consent bypass failed: %v", err)
			} else {
				if bypassMap, ok := bypassResult.(map[string]interface{}); ok {
					if bypassInner, ok := bypassMap["result"].(map[string]interface{}); ok {
						if newHTML, ok := bypassInner["cleaned_html"].(string); ok && newHTML != "" {
							log.Printf("✅ [MCP-SMART-SCRAPE] Successfully bypassed consent page, got %d chars of new HTML", len(newHTML))
							cleanedHTML = newHTML
						}
						if cookies, ok := bypassInner["cookies"].([]interface{}); ok {
							capturedCookies = cookies
						}
					}
				}
			}
		}
	}

	planCtx, planCancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer planCancel()

	if !bypassedLLM && s.llmClient != nil && len(cleanedHTML) > 500 {
		goalLower := strings.ToLower(goal)
		isInteractive := strings.Contains(goalLower, "calculate") ||
			strings.Contains(goalLower, "search for") ||
			strings.Contains(goalLower, "fill") ||
			strings.Contains(goalLower, "submit") ||
			strings.Contains(goalLower, "log in") ||
			strings.Contains(goalLower, "sign in")

		if !isInteractive {
			log.Printf("⚡ [MCP-SMART-SCRAPE] Trying early LLM extraction from initial HTML (%d chars) before planning navigation...", len(cleanedHTML))

			earlyHTML := cleanHTMLForPlanning(cleanedHTML)
			if len(earlyHTML) > 30000 {
				earlyHTML = earlyHTML[:30000] + "...(truncated)"
			}

			earlyPrompt := fmt.Sprintf(`You are a web scraping data extraction expert.

TASK: Extract the requested information from the HTML content below.

GOAL: %s

HTML CONTENT:
%s

INSTRUCTIONS:
- Extract ONLY the specific data requested in the goal.
- Return the data as a clean, structured list or text.
- Include actual titles, names, URLs, or values — not HTML tags.
- If the page has multiple items (e.g. news articles), list them numbered.
- Be concise but complete. Include all relevant items found.
- Do NOT wrap in JSON or code blocks. Just return the extracted content as plain text.
- Do NOT use any markdown formatting — no **, *, #, or other markup. Output plain text only, suitable for reading aloud.
- If the requested data is NOT present in the HTML, respond with exactly: NO_DATA_FOUND`, goal, earlyHTML)

			earlyResult, earlyErr := s.llmClient.callLLMWithContextAndPriority(planCtx, earlyPrompt, PriorityHigh)
			if earlyErr == nil && strings.TrimSpace(earlyResult) != "" && !strings.Contains(earlyResult, "NO_DATA_FOUND") {
				log.Printf("✅ [MCP-SMART-SCRAPE] Early fast-path LLM extraction produced %d chars — skipping navigation!", len(earlyResult))

				results := make(map[string]interface{})
				if title, ok := innerResult["page_title"].(string); ok {
					results["page_title"] = title
				}
				results["page_url"] = url
				results["extracted_content"] = stripMarkdownFormatting(strings.TrimSpace(earlyResult))
				results["extraction_method"] = "early_llm_fast_path"

				resultJSON, _ := json.MarshalIndent(results, "", "  ")
				return map[string]interface{}{
					"content": []map[string]interface{}{
						{
							"type": "text",
							"text": fmt.Sprintf("Scrape Results (Fast Extraction):\n%s", string(resultJSON)),
						},
					},
					"result": results,
				}, nil
			}
			log.Printf("⚠️ [MCP-SMART-SCRAPE] Early fast-path extraction failed or found no data, proceeding to navigation planning...")
		}
	}

	if !bypassedLLM {
		log.Printf("📋 [MCP-SMART-SCRAPE] Planning scrape config with LLM (%d chars of HTML)...", len(cleanedHTML))

		actionableHTML := s.buildActionableSnapshot(cleanedHTML)
		navGoal := "[NAVIGATION_ONLY]\n" + goal
		config, err = s.planScrapeWithLLM(planCtx, actionableHTML, navGoal, userConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to plan scrape with LLM: %v", err)
		}
		config.TypeScriptConfig = s.sanitizeNavigationScript(config.TypeScriptConfig, actionableHTML, goal)

		if userConfig != nil {
			if userConfig.TypeScriptConfig != "" && config.TypeScriptConfig == "" {
				config.TypeScriptConfig = userConfig.TypeScriptConfig
			}
			if len(userConfig.Extractions) > 0 {
				if config.Extractions == nil {
					config.Extractions = make(map[string]string)
				}
				for k, v := range userConfig.Extractions {
					if _, exists := config.Extractions[k]; !exists {
						config.Extractions[k] = v
					}
				}
			}
		}
	}

	for k, v := range config.Extractions {

		sanitized := strings.ReplaceAll(v, "(?<=", "(?:")
		sanitized = strings.ReplaceAll(sanitized, "(?=", "(?:")
		reNamedGroup := regexp.MustCompile(`\(\?<[^>]+>`)
		sanitized = reNamedGroup.ReplaceAllString(sanitized, "(")

		hasCapture := false
		for i := 0; i < len(sanitized); i++ {
			if sanitized[i] == '(' {

				if i > 0 && sanitized[i-1] == '\\' {
					continue
				}
				if i+1 < len(sanitized) && sanitized[i+1] != '?' {
					hasCapture = true
					break
				}
			}
		}

		if !hasCapture {
			sanitized = "(" + sanitized + ")"
		}

		config.Extractions[k] = sanitized
	}

	isInteractiveGoal := strings.Contains(strings.ToLower(goal), "calculate") || strings.Contains(strings.ToLower(goal), "search") || strings.Contains(strings.ToLower(goal), "fill")
	if !bypassedLLM && isInteractiveGoal && (config.TypeScriptConfig == "" || strings.TrimSpace(config.TypeScriptConfig) == "// no navigation needed") {
		log.Printf("⚠️ [MCP-SMART-SCRAPE] LLM provided no navigation for an interactive goal. Retrying with explicit warning...")

		retryUserConfig := ScrapeConfig{Extractions: make(map[string]string)}
		if userConfig != nil {
			retryUserConfig = *userConfig
		}

		retryUserConfig.TypeScriptConfig = "IMPORTANT: THE LAST PLAN FAILED BECAUSE IT HAD NO JS COMMANDS. YOU MUST PROVIDE await page.fill() AND await page.click() COMMANDS TO REACH THE RESULT."

		config, err = s.planScrapeWithLLM(planCtx, cleanedHTML, goal, &retryUserConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to plan scrape with LLM (retry): %v", err)
		}
	}

	if !bypassedLLM && (config.TypeScriptConfig == "" || strings.TrimSpace(config.TypeScriptConfig) == "// no navigation needed") {
		log.Printf("⚡ [MCP-SMART-SCRAPE] Fast-path: Extracting from existing HTML (no extra navigation needed)")

		results := make(map[string]interface{})

		if title, ok := innerResult["page_title"].(string); ok {
			results["page_title"] = title
		}
		results["page_url"] = url
		results["cleaned_html"] = cleanedHTML
		if cookies, ok := innerResult["cookies"]; ok {
			results["cookies"] = cookies
		}

		foundAny := false
		for key, pattern := range config.Extractions {
			re, err := regexp.Compile(pattern)
			if err != nil {
				log.Printf("⚠️  [MCP-SMART-SCRAPE] Invalid regex for '%s': %v", key, err)
				continue
			}
			matches := re.FindAllStringSubmatch(cleanedHTML, -1)
			if len(matches) > 0 {
				if len(matches[0]) > 1 {
					var extracted []string
					for _, m := range matches {
						if len(m) > 1 && m[1] != "" {
							extracted = append(extracted, m[1])
						}
					}
					if len(extracted) > 0 {
						results[key] = strings.Join(extracted, "\n")
						foundAny = true
						log.Printf("✅ [MCP-SMART-SCRAPE] Extracted '%s' via fast-path", key)
					}
				}
			}
		}
		if foundAny {
			stripScrapeResultFields(results)
			resultJSON, _ := json.MarshalIndent(results, "", "  ")
			return map[string]interface{}{
				"content": []map[string]interface{}{
					{
						"type": "text",
						"text": fmt.Sprintf("Scrape Results:\n%s", string(resultJSON)),
					},
				},
				"result": results,
			}, nil
		}

		if s.llmClient != nil && cleanedHTML != "" {
			log.Printf("🧠 [MCP-SMART-SCRAPE] Fast-path regex failed — trying LLM content extraction from existing HTML (%d chars)", len(cleanedHTML))

			extractionHTML := cleanHTMLForPlanning(cleanedHTML)
			if len(extractionHTML) > 30000 {
				extractionHTML = extractionHTML[:30000] + "...(truncated)"
			}

			extractionPrompt := fmt.Sprintf(`You are a web scraping data extraction expert.

TASK: Extract the requested information from the HTML content below.

GOAL: %s

HTML CONTENT:
%s

INSTRUCTIONS:
- Extract ONLY the specific data requested in the goal.
- Return the data as a clean, structured list or text.
- Include actual titles, names, URLs, or values — not HTML tags.
- If the page has multiple items (e.g. news articles), list them numbered.
- Be concise but complete. Include all relevant items found.
- Do NOT wrap in JSON or code blocks. Just return the extracted content as plain text.
- Do NOT use any markdown formatting — no **, *, #, or other markup. Output plain text only, suitable for reading aloud.`, goal, extractionHTML)

			llmResult, err := s.llmClient.callLLMWithContextAndPriority(planCtx, extractionPrompt, PriorityHigh)
			if err == nil && strings.TrimSpace(llmResult) != "" {
				log.Printf("✅ [MCP-SMART-SCRAPE] Fast-path LLM extraction produced %d chars of content", len(llmResult))
				results["extracted_content"] = stripMarkdownFormatting(strings.TrimSpace(llmResult))
				results["extraction_method"] = "llm_content_extraction"

				stripScrapeResultFields(results)
				resultJSON, _ := json.MarshalIndent(results, "", "  ")
				return map[string]interface{}{
					"content": []map[string]interface{}{
						{
							"type": "text",
							"text": fmt.Sprintf("Scrape Results (LLM Extracted):\n%s", string(resultJSON)),
						},
					},
					"result": results,
				}, nil
			}
			log.Printf("⚠️ [MCP-SMART-SCRAPE] Fast-path LLM extraction failed: %v", err)
		}

		log.Printf("⚠️ [MCP-SMART-SCRAPE] Fast-path failed to extract any data, falling back to full scrape")
	}

	log.Printf("🚀 [MCP-SMART-SCRAPE] Executing planned scrape (Navigation: %v)", config.TypeScriptConfig != "")
	log.Printf("🎯 [MCP-SMART-SCRAPE] Goal: %s", goal)

	requestHTML := true
	scrapeResultRaw, err := s.scrapeWithConfig(ctx, url, goal, config.TypeScriptConfig, false, config.Extractions, requestHTML, capturedCookies)
	if err != nil {
		return nil, err
	}

	scrapeResult, ok := scrapeResultRaw.(map[string]interface{})
	if !ok {
		return scrapeResultRaw, nil
	}

	finalInnerResult, ok := scrapeResult["result"].(map[string]interface{})
	if !ok {
		return scrapeResult, nil
	}

	isPostNavigation := config.TypeScriptConfig != ""

	missingExtractions := false
	if len(config.Extractions) > 0 {
		for key := range config.Extractions {
			if _, exists := finalInnerResult[key]; !exists {
				missingExtractions = true
				break
			}
		}
	}

	if isPostNavigation && missingExtractions {
		finalHTML, _ := finalInnerResult["cleaned_html"].(string)
		if finalHTML != "" {
			log.Printf("🔍 [MCP-SMART-SCRAPE] Navigation succeeded but extractions failed. Performing second-pass planning on final page...")

			extractionHTML := s.buildExtractionSnapshot(finalHTML, goal)
			log.Printf("🧩 [MCP-SMART-SCRAPE] Extraction snapshot size: %d chars", len(extractionHTML))

			secondPassGoal := fmt.Sprintf("RECOVERY MODE: Navigation is already complete. The data you need should be in the HTML snapshot below. DO NOT provide any navigation JS. Just find the correct regex for: %s", goal)

			secondPassConfig, err := s.planScrapeWithLLM(planCtx, extractionHTML, secondPassGoal, &ScrapeConfig{
				TypeScriptConfig: "// NAVIGATION ALREADY COMPLETED - DO NOT ADD COMMANDS HERE",
				Extractions:      config.Extractions,
			})

			if err == nil && len(secondPassConfig.Extractions) > 0 {
				log.Printf("🎯 [MCP-SMART-SCRAPE] Second-pass planned %d specialized extraction patterns", len(secondPassConfig.Extractions))

				secondResults := s.applyExtractions(finalHTML, secondPassConfig.Extractions)

				foundNew := false
				for k, v := range secondResults {
					if _, alreadyFound := finalInnerResult[k]; !alreadyFound {
						finalInnerResult[k] = v
						foundNew = true
						log.Printf("✅ [MCP-SMART-SCRAPE] Second-pass successfully extracted '%s'", k)
					}
				}

				if foundNew {

					resultJSON, _ := json.MarshalIndent(finalInnerResult, "", "  ")
					if content, ok := scrapeResult["content"].([]map[string]interface{}); ok && len(content) > 0 {
						content[0]["text"] = fmt.Sprintf("Scrape Results (Two-Step):\n%s", string(resultJSON))
					}
				}
			}
		}
	}

	metadataKeys := map[string]bool{
		"page_title": true, "page_url": true, "cleaned_html": true, "raw_html": true,
		"screenshot": true, "cookies": true, "status": true, "extracted_at": true,
		"execution_time_ms": true, "page_content": true, "url": true,
	}
	hasUsefulData := false
	for key := range finalInnerResult {
		if !metadataKeys[key] {
			hasUsefulData = true
			break
		}
	}

	if !hasUsefulData {

		finalHTML := ""
		if h, ok := finalInnerResult["cleaned_html"].(string); ok && h != "" {
			finalHTML = h
		} else if h, ok := finalInnerResult["raw_html"].(string); ok && h != "" {
			finalHTML = h
		}

		if finalHTML != "" && s.llmClient != nil {
			log.Printf("🧠 [MCP-SMART-SCRAPE] No structured data extracted — performing LLM content extraction from HTML (%d chars)", len(finalHTML))

			extractionHTML := cleanHTMLForPlanning(finalHTML)
			if len(extractionHTML) > 30000 {
				extractionHTML = extractionHTML[:30000] + "...(truncated)"
			}

			extractionPrompt := fmt.Sprintf(`You are a web scraping data extraction expert.

TASK: Extract the requested information from the HTML content below.

GOAL: %s

HTML CONTENT:
%s

INSTRUCTIONS:
- Extract ONLY the specific data requested in the goal.
- Return the data as a clean, structured list or text.
- Include actual titles, names, URLs, or values — not HTML tags.
- If the page has multiple items (e.g. news articles), list them numbered.
- Be concise but complete. Include all relevant items found.
- Do NOT wrap in JSON or code blocks. Just return the extracted content as plain text.
- Do NOT use any markdown formatting — no **, *, #, or other markup. Output plain text only, suitable for reading aloud.`, goal, extractionHTML)

			llmResult, err := s.llmClient.callLLMWithContextAndPriority(planCtx, extractionPrompt, PriorityHigh)
			if err == nil && strings.TrimSpace(llmResult) != "" {
				log.Printf("✅ [MCP-SMART-SCRAPE] LLM extraction produced %d chars of content", len(llmResult))
				finalInnerResult["extracted_content"] = stripMarkdownFormatting(strings.TrimSpace(llmResult))
				finalInnerResult["extraction_method"] = "llm_content_extraction"

				stripScrapeResultFields(finalInnerResult)

				resultJSON, _ := json.MarshalIndent(finalInnerResult, "", "  ")
				if content, ok := scrapeResult["content"].([]map[string]interface{}); ok && len(content) > 0 {
					content[0]["text"] = fmt.Sprintf("Scrape Results (LLM Extracted):\n%s", string(resultJSON))
				}
			} else {
				log.Printf("⚠️ [MCP-SMART-SCRAPE] LLM extraction failed or returned empty: %v", err)
			}
		}
	}

	if innerResult, ok := scrapeResult["result"].(map[string]interface{}); ok {
		stripScrapeResultFields(innerResult)
	}

	return scrapeResult, nil
}

// applyExtractions applies regex patterns to HTML locally
func (s *MCPKnowledgeServer) applyExtractions(html string, patterns map[string]string) map[string]string {
	results := make(map[string]string)
	for key, pattern := range patterns {

		sanitized := strings.ReplaceAll(pattern, "(?<=", "(?:")
		sanitized = strings.ReplaceAll(sanitized, "(?=", "(?:")

		re, err := regexp.Compile(sanitized)
		if err != nil {
			log.Printf("⚠️ [MCP-SMART-SCRAPE] Invalid regex for '%s': %v", key, err)
			continue
		}

		matches := re.FindAllStringSubmatch(html, -1)
		if len(matches) > 0 {
			var extracted []string
			for _, m := range matches {
				if len(m) > 1 && m[1] != "" {
					extracted = append(extracted, m[1])
				}
			}
			if len(extracted) > 0 {
				results[key] = strings.Join(extracted, "\n")
			}
		}
	}
	return results
}

func (s *MCPKnowledgeServer) planScrapeWithLLM(ctx context.Context, html string, goal string, hint *ScrapeConfig) (*ScrapeConfig, error) {
	if s.llmClient == nil {
		return nil, fmt.Errorf("LLM client not configured on knowledge server")
	}

	originalLen := len(html)
	html = cleanHTMLForPlanning(html)
	cleanedLen := len(html)
	log.Printf("🧹 [MCP-SMART-SCRAPE] HTML cleaned for planning: %d -> %d chars (reduced by %.1f%%)", originalLen, cleanedLen, float64(originalLen-cleanedLen)/float64(originalLen)*100)

	maxHTML := 120000
	if strings.HasPrefix(goal, "[NAVIGATION_ONLY]") {
		maxHTML = 20000
	}
	if len(html) > maxHTML {
		html = html[:maxHTML] + "...(truncated)"
		log.Printf("⚠️ [MCP-SMART-SCRAPE] HTML still exceeding %d limit, truncated", maxHTML)
	}

	navigationOnly := false
	if strings.HasPrefix(goal, "[NAVIGATION_ONLY]") {
		navigationOnly = true

		goal = strings.TrimPrefix(goal, "[NAVIGATION_ONLY]")
		goal = strings.TrimSpace(goal)
	}

	sampleLen := 2000
	if len(html) < sampleLen {
		sampleLen = len(html)
	}
	log.Printf("🔍 [MCP-SMART-SCRAPE] Sample of cleaned HTML for LLM (first %d chars):\n%s\n...end sample", sampleLen, html[:sampleLen])

	systemPrompt := `You are an expert web scraper configuration generator.
Your task is to analyze an HTML snapshot and generate a scraping plan to achieve a specific GOAL.

⚠️ LANDING PAGE WARNING:
If the HTML SNAPSHOT looks like a FORM (has input fields, selects) and the GOAL is to "Calculate", "Search", or "Find" something specific, you are likely on a LANDING PAGE. 
The data you need is NOT on this page yet. You MUST provide JS commands in "typescript_config" to fill the form and submit it.

Goal: Generate a valid JSON object with:
- "typescript_config": (string) A sequence of Playwright JS commands (e.g., await page.click('...')) to navigate or reveal data if required. MUST BE A STRING, NOT AN OBJECT.
- "extractions": (map<string, string>) A set of named extraction patterns. 
  - Key: The field name (e.g. "price", "title")
  - Value: A single REGEX STRING (e.g. "regex..."). DO NOT USE ARRAYS.

REGEX RULES:
1. ONLY standard Go regex (NO lookarounds like (?=...) or (?<=...)).
2. USE A SINGLE CAPTURING GROUP () only for the data you want to extract. 
   - ❌ BAD: "<span class='(.*?)'>([^<]+)</span>" (Captures class as group 1)
   - ✅ GOOD: "<span[^>]*class='title'[^>]*>\\s*([^<]+)<" (Captures data as group 1)
3. Target the HTML tags you see in the snapshot.
4. Use single quotes (') or [\"'] in your regex for attributes.
5. IMPORTANT: Use [^>]* to skip unknown attributes. e.g. "Table__ProductName[^>]*>\\s*([^<]+)<"
SPECIFICITY RULE:
3. NEVER use generic regex like "(\\d+)" or "([^<]+)" alone.
4. ALWAYS anchor your regex to nearby unique text or labels you see in the snapshot.
   - ❌ BAD: "(\\d+\\.?\\d*)"
   - ✅ GOOD: "CO2\\s*emissions[^>]*>\\s*([^<]+)"
   - ✅ GOOD: "Total[^>]*>\\s*([0-9,.]+)\\s*kg"

EXAMPLES:
- RIGHT: "price:\s*(\d+)"
- WRONG (Lookaround): "(?<=price: )(\d+)"

COMMON PATTERNS:
6. Standard tag with class: "<span[^>]*class='[^']*price[^']*'[^>]*?>\s*([$€£0-9,.]+)"
7. Div with class: "<div[^>]*class='[^']*price[^']*'[^>]*?>\s*([$€£0-9,.]+)"
8. Table cell: "<td[^>]*class='[^']*market-cap[^']*'[^>]*?>\s*([^<]+)"

MODERN WEB PATTERNS (Custom Tags & Data Attributes):
9. Custom tags (e.g. <fin-streamer>, <price-display>): Match ANY tag name you see in HTML
   - Value in content: "<custom-tag[^>]*attribute='value'[^>]*?>\s*([0-9,.]+)"
   - Value in data-value attribute: "<custom-tag[^>]*data-value='([^']+)'"
   - Value in value attribute: "<custom-tag[^>]*value='([^']+)'"
10. Try MULTIPLE patterns for the same field if unsure where value is stored:
    - First try: content between tags
    - Then try: data-value, value, data-field, or other data-* attributes
11. For data attributes, use: "<tag[^>]*data-attribute-name='([^']+)'"
12. Match partial attribute values: "data-field='[^']*price[^']*'"

INTERACTIVE PATTERNS (Autocompletes & Dropdowns):
13. CRITICAL: If you detect Stimulus.js autocomplete fields (data-controller, data-action attributes) OR visible input fields (id, name containing 'from', 'to', 'airport', 'destination', etc.), ALWAYS INCLUDE INITIALIZATION WAIT:
    - Always start with: await page.waitForLoadState('networkidle');  // Wait for JS controllers to initialize
    - For airport/airline codes: Type the CODE (e.g. 'SHA' for Shanghai, not 'Shanghai')
    - Wait for dropdown to appear: await page.waitForTimeout(2000);  // Allow XHR for suggestions
    - Click FIRST suggestion (DO NOT use text() predicates): 
      * await page.locator('ul li, [role="option"], .dropdown li').first().click();
    - CRITICAL: DO NOT use page.waitForNavigation() with autocomplete clicks - dropdowns update in-place without navigating
    - NEVER wrap autocomplete clicks in Promise.all([page.waitForNavigation(), ...]) - this will hang forever
    CRITICAL DO NOT DO: 
    - XPath with text() like xpath=//li[text()="value"] - these fail on Stimulus controllers
    - page.waitForNavigation() for dropdown selections - they don't navigate
    EXAMPLE (Correct airport selection pattern):
    await page.locator('input#flight_calculator_from').fill('CDG');
    await page.waitForTimeout(2000);
    await page.locator('ul li').first().click();
    // Repeat for "To" field with 'LHR'
14. For standard <select> elements:
    - Use .selectOption('internal_value') where 'internal_value' is the value attribute of the <option> tag.
    - NEVER use .fill() on a <select>.
15. For Radio Buttons (<input type='radio'>):
    - Use .check() on the specific radio button ID or label.

NAVIGATION RULES:
16. If the GOAL requires calculating, searching, or filtering data NOT present in the HTML SNAPSHOT (e.g., "Calculate emissions...", "Find price of..."), you MUST provide JS commands in "typescript_config" to reach the result.
17. DO NOT leave "typescript_config" empty for calculation goals. This is a fatal error.
18. ALWAYS wait after submitting a form: If you click a submit button, use **await page.waitForTimeout(3000)** to let results load. DO NOT use page.waitForNavigation() for submit buttons - many forms update in-place with AJAX instead of navigating.
19. CRITICAL EXTRACTION RULE: When you provide navigation JS in "typescript_config", you MUST ALSO provide extraction patterns in the "extractions" field for the RESULT PAGE data. Even if you can't see the result page yet, provide your best guess at extraction patterns based on the goal (e.g., for "CO2 emissions", try patterns like "CO2.*?([0-9,.]+)\\s*(kg|t)", "emissions.*?([0-9,.]+)", etc.). DO NOT leave "extractions" empty when navigating to get data.

- Use double quotes for the JSON wrapper and single quotes for JS strings: "await page.click('selector');"

FATAL FORMAT ERROR WARNING:
- NEVER EVER use nested objects like "calculation_logic" or "interaction_logic": { "step1": ... } inside "typescript_config".
- "typescript_config" MUST be either a SINGLE STRING of JS code OR a FLAT ARRAY of step objects.
- ❌ BAD (Object): "typescript_config": { "calculate": { "click": "..." } }
- ✅ GOOD (String): "typescript_config": "await page.click('...');"
- ✅ GOOD (Array): "typescript_config": { "steps": [ { "action": "click", "selector": "..." } ] }

REGEX FATAL ERROR WARNING:
- NEVER EVER use lookarounds like (?=...) or (?<=...). These will CRASH the Go backend.
- If you need to match after a label, include the label in the regex and use the capturing group for the data.
- ✅ GOOD: "Price:\\\\s*([0-9,.]+)"
- ❌ FATAL ERROR: "(?<=Price:\\\\s*)[0-9,.]+"

THINKING PROCESS:
- STEP 1: Does the goal require user interaction (typing, clicking, selecting)?
- STEP 2: If YES, write the JS sequence in "typescript_config". 
- STEP 3: Identify only the patterns for the FINAL result page after navigation.

CALCULATION RULE:
- If the GOAL is to "calculate", "search", or "find" something that requires filling a form:
- YOU MUST PROVIDE "await page.fill('...', '...');" and "await page.click('...');" commands.
- DO NOT just provide a "commit" click. You must fill the inputs first with the values from the GOAL.
- DO NOT USE PLACEHOLDERS like {{value}}. Use the actual values from the GOAL.

STRATEGY:
- Look for custom HTML tags (anything not standard like div/span/p)
- Check both tag CONTENT and tag ATTRIBUTES for values
- Use flexible but SPECIFIC patterns (e.g., include surrounding static text or classes) to avoid multiple garbage matches.
- If you see data-* attributes, they often contain the actual values
- For forms, check if fields are autocompletes that require clicking a suggestion to 'lock' the value.
- If the goal data isn't in the snapshot, plan the navigation to get there.

Output ONLY the JSON object. Start the response with '{'.`

	if navigationOnly {
		systemPrompt = `You are an expert web scraper configuration generator.

NAVIGATION-ONLY MODE:
- The HTML snapshot contains only actionable elements (forms, inputs, buttons).
- Return a JSON object with "typescript_config" as a string of Playwright commands.
- DO NOT provide extraction regexes; return "extractions": {} or omit it.
- Fill required inputs using values from the GOAL, click submit, and wait for results to load.

Output ONLY valid JSON.`
	}

	formTypeHint := ""
	htmlLower := strings.ToLower(html)
	if strings.Contains(htmlLower, "from") && strings.Contains(htmlLower, "to") &&
		(strings.Contains(htmlLower, "flight") || strings.Contains(htmlLower, "airport") || strings.Contains(htmlLower, "destination")) {
		formTypeHint = "\n### FORM TYPE DETECTED: Flight Calculator\nIMPORTANT: This form uses Stimulus.js autocomplete controllers. You MUST:\n1. Start with: await page.waitForLoadState('networkidle'); // Essential for JS controller initialization\n2. For airport fields: Use airport CODES (e.g., 'SHA' for Shanghai, not the full name)\n3. Fill input, wait for dropdown: await page.waitForTimeout(2000);\n4. Click the first matching option from dropdown\n"
	}

	userPrompt := fmt.Sprintf(`### GOAL
%s

### HTML SNAPSHOT (Truncated)
%s
%s`, goal, html, formTypeHint)

	log.Printf("🧾 [MCP-SMART-SCRAPE] Prompt sizes: system=%d chars, user=%d chars, total=%d chars", len(systemPrompt), len(userPrompt), len(systemPrompt)+len(userPrompt))

	if hint != nil {
		userPrompt += "\n### USER HINTS (PROBABLE REGEX):\n"
		userPrompt += "The user has provided regex patterns that worked in the past. \n"
		userPrompt += "VALIDATION RULE: If these patterns match the HTML SNAPSHOT below, use them. \n"
		userPrompt += "SELF-HEALING RULE: If a pattern obviously fails to match the current HTML (e.g., attributes changed), you MUST IGNORE the hint and generate a NEW working regex for that key.\n"
		if hint.TypeScriptConfig != "" {
			userPrompt += fmt.Sprintf("- Suggested TypeScript Logic: %s\n", hint.TypeScriptConfig)
		}
		if len(hint.Extractions) > 0 {
			for k, v := range hint.Extractions {
				userPrompt += fmt.Sprintf("- Key '%s' suggested regex: %s\n", k, v)
			}
		}
	}

	userPrompt += `
### TASK
Generate the JSON configuration to extract the data requested in the GOAL.

INSTRUCTIONS:
- Identify the data requested in the GOAL.
- CALCULATION/SEARCH goals REQUIRE interaction logic in "typescript_config".
- STRICT RULE: ONLY use attributes (class, id, role) that you see in the HTML SNAPSHOT.
- NEVER invent attributes like 'data-rate' or 'product-id' if they are not in the snapshot.
- If the page has a <table>, focus your regex on matching <tr> rows within <tbody>.
- Capture ONLY the data you want in the first ().
- CRITICAL: Your response MUST be valid JSON. Double all backslashes: \\s, \\d, \\S.
- Output ONLY valid JSON.`

	response, err := s.llmClient.callLLMWithContextAndPriority(ctx, systemPrompt+"\n\n"+userPrompt, PriorityHigh)
	if err != nil {
		return nil, err
	}
	log.Printf("🤖 [MCP-SMART-SCRAPE] Raw LLM planning response: %s", response)

	// Clean/Parse JSON - Enhanced extraction logic
	var config ScrapeConfig
	var parseErr error

	// Try approach 1: Parse into map first for maximum resilience
	var rawMap map[string]interface{}
	cleanedResponse := response
	if first := strings.Index(cleanedResponse, "{"); first != -1 {
		if last := strings.LastIndex(cleanedResponse, "}"); last != -1 && last > first {
			cleanedResponse = cleanedResponse[first : last+1]

			lines := strings.Split(cleanedResponse, "\n")
			for i, line := range lines {
				if idx := strings.Index(line, "//"); idx != -1 {

					isUrl := false
					if idx > 0 && line[idx-1] == ':' {
						isUrl = true
					}
					if !isUrl {
						lines[i] = line[:idx]
					}
				}
			}
			cleanedResponse = strings.Join(lines, "\n")
			cleanedResponse = sanitizeJSONResponse(cleanedResponse)

			if err := json.Unmarshal([]byte(cleanedResponse), &rawMap); err == nil {

				// Handle both "extractions", "extraction_instructions", and "data_extraction"
				var extractions map[string]interface{}
				if ex, ok := rawMap["extractions"].(map[string]interface{}); ok {
					extractions = ex
				} else if ex, ok := rawMap["extraction_instructions"].(map[string]interface{}); ok {
					extractions = ex
				} else if ex, ok := rawMap["data_extraction"].(map[string]interface{}); ok {
					extractions = ex
				} else {

					extractions = make(map[string]interface{})
					for k, v := range rawMap {
						kLower := strings.ToLower(k)
						if kLower != "typescript_config" && kLower != "goal" && kLower != "explanation" &&
							kLower != "summary" && kLower != "data_extraction" && kLower != "extractions" &&
							kLower != "extraction_instructions" && kLower != "thought" && kLower != "steps" {
							extractions[k] = v
						}
					}
				}

				if extractions != nil {
					config.Extractions = make(map[string]string)
					for k, v := range extractions {
						if s, ok := v.(string); ok {
							config.Extractions[k] = s
						} else if obj, ok := v.(map[string]interface{}); ok {

							if r, ok := obj["selector"].(string); ok {
								config.Extractions[k] = r
							} else if r, ok := obj["pattern"].(string); ok {
								config.Extractions[k] = r
							} else if r, ok := obj["regex"].(string); ok {
								config.Extractions[k] = r
							} else if r, ok := obj["value"].(string); ok {
								config.Extractions[k] = r
							}
						} else if arr, ok := v.([]interface{}); ok && len(arr) > 0 {
							if s, ok := arr[0].(string); ok {
								config.Extractions[k] = s
							}
						}
					}
				}

				if ts, ok := rawMap["typescript_config"].(string); ok {
					config.TypeScriptConfig = ts
				} else if tsArr, ok := rawMap["typescript_config"].([]interface{}); ok {

					log.Printf("🩹 [MCP-SMART-SCRAPE] Converting typescript_config array (%d items) to JS string...", len(tsArr))
					config.TypeScriptConfig = convertStepsToJS(map[string]interface{}{"steps": tsArr})
				} else if tsObj, ok := rawMap["typescript_config"].(map[string]interface{}); ok {

					log.Printf("🩹 [MCP-SMART-SCRAPE] Converting typescript_config object to JS string...")
					config.TypeScriptConfig = convertStepsToJS(tsObj)
				} else {

					log.Printf("🩹 [MCP-SMART-SCRAPE] TypeScript config not at root, searching recursively...")
					config.TypeScriptConfig = findJSInObject(rawMap)
				}
				parseErr = nil
			} else {

				log.Printf("⚠️ [MCP-SMART-SCRAPE] JSON parse failed (%v), trying regex recovery...", err)
				config.Extractions = make(map[string]string)

				rePairs := regexp.MustCompile(`"([^"]+)"\s*:\s*"([\s\S]*?)"(?:\s*[,}])`)
				pairs := rePairs.FindAllStringSubmatch(cleanedResponse, -1)
				for _, p := range pairs {
					key := p[1]
					val := p[2]
					if key == "typescript_config" {
						config.TypeScriptConfig = val
					} else if key != "extractions" && key != "extraction_instructions" && key != "goal" && key != "explanation" && key != "summary" && key != "regex" && key != "pattern" && key != "selector" {
						config.Extractions[key] = val
					}
				}

				reNested := regexp.MustCompile(`"([^"]+)"\s*:\s*[{]\s*([\s\S]*?)[}]`)
				nested := reNested.FindAllStringSubmatch(cleanedResponse, -1)
				for _, n := range nested {
					parentKey := n[1]
					inner := n[2]
					innerPairs := rePairs.FindAllStringSubmatch(inner, -1)

					foundInner := false
					for _, p := range innerPairs {
						ik := p[1]
						iv := p[2]
						if ik == "regex" || ik == "pattern" || ik == "selector" || ik == "value" {
							config.Extractions[parentKey] = iv
							foundInner = true
						}
					}

					if !foundInner && (parentKey == "extractions" || parentKey == "extraction_instructions") {
						for _, p := range innerPairs {
							config.Extractions[p[1]] = p[2]
						}
					}
				}
				if len(config.Extractions) > 0 {
					parseErr = nil
				} else {
					parseErr = err
				}
			}
		}
	}

	if parseErr != nil {

		cleanedRes := response
		if idx := strings.Index(cleanedRes, "{"); idx != -1 {
			if lastIdx := strings.LastIndex(cleanedRes, "}"); lastIdx != -1 {
				cleanedRes = cleanedRes[idx : lastIdx+1]
				if err := json.Unmarshal([]byte(cleanedRes), &config); err != nil {
					parseErr = err
				} else {
					parseErr = nil
				}
			}
		}
	}

	if parseErr != nil {
		return nil, fmt.Errorf("failed to parse LLM planning response: %v (response was: %s)", parseErr, response)
	}

	if navigationOnly {
		if config.Extractions == nil {
			config.Extractions = make(map[string]string)
		} else {

			config.Extractions = make(map[string]string)
		}
	}

	return &config, nil
}
