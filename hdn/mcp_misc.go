package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// extractAndSaveScreenshot looks for base64 images in tool results and saves them
func (s *MCPKnowledgeServer) extractAndSaveScreenshot(toolName string, result interface{}) {
	if resMap, ok := result.(map[string]interface{}); ok {
		var screenshotB64 string
		if sVal, ok := resMap["screenshot"].(string); ok {
			screenshotB64 = sVal
		} else if res, ok := resMap["result"].(map[string]interface{}); ok {
			if sVal, ok := res["screenshot"].(string); ok {
				screenshotB64 = sVal
			}
		}

		if screenshotB64 == "" {
			if results, ok := resMap["results"].([]interface{}); ok && len(results) > 0 {
				if first, ok := results[0].(map[string]interface{}); ok {
					if sVal, ok := first["screenshot"].(string); ok {
						screenshotB64 = sVal
					} else if res, ok := first["result"].(map[string]interface{}); ok {
						if sVal, ok := res["screenshot"].(string); ok {
							screenshotB64 = sVal
						}
					}
				}
			}
		}

		if screenshotB64 == "" {
			if content, ok := resMap["content"].([]interface{}); ok {
				for _, item := range content {
					if imap, ok := item.(map[string]interface{}); ok {
						if itype, ok := imap["type"].(string); ok && itype == "image" {
							if idata, ok := imap["data"].(string); ok {
								screenshotB64 = idata
								break
							}
						}
					}
				}
			}
		}

		if screenshotB64 != "" {
			go func(b64 string) {
				raw := b64
				if idx := strings.Index(raw, ","); idx >= 0 {
					raw = raw[idx+1:]
				}
				if decoded, err := base64.StdEncoding.DecodeString(raw); err == nil {

					s.screenshotMu.Lock()
					s.latestScreenshot = decoded
					s.screenshotMu.Unlock()

					projectRoot := os.Getenv("AGI_PROJECT_ROOT")
					if projectRoot == "" {
						if wd, err := os.Getwd(); err == nil {
							projectRoot = wd
						}
					}
					artifactsDir := filepath.Join(projectRoot, "artifacts")
					os.MkdirAll(artifactsDir, 0755)
					path := filepath.Join(artifactsDir, "latest_screenshot.png")
					_ = os.WriteFile(path, decoded, 0644)
					log.Printf("📸 [MCP-KNOWLEDGE] Screenshot saved to %s for tool %s", path, toolName)
				}
			}(screenshotB64)
		}
	}
}

// executeToolWrapper routes MCP tool calls to the wrapped internal HDN tools
func (s *MCPKnowledgeServer) executeToolWrapper(ctx context.Context, toolName string, args map[string]interface{}) (interface{}, error) {

	switch toolName {
	case "scrape_url":
		url, ok := args["url"].(string)
		if !ok {
			return nil, fmt.Errorf("url parameter required")
		}

		tsConfig, _ := args["typescript_config"].(string)
		getHTML, _ := args["get_html"].(bool)

		if (tsConfig != "") || getHTML {
			isAsync, _ := args["async"].(bool)

			extractions := make(map[string]string)
			if ext, ok := args["extractions"].(map[string]interface{}); ok {
				for k, v := range ext {
					if vStr, ok := v.(string); ok {
						extractions[k] = vStr
					}
				}
			}

			log.Printf("🚀 [MCP-SCRAPE] Starting async job for %s", url)

			return s.scrapeWithConfig(ctx, url, "", tsConfig, isAsync, extractions, getHTML, nil)
		}

		projectRoot := os.Getenv("AGI_PROJECT_ROOT")
		if projectRoot == "" {

			if wd, err := os.Getwd(); err == nil {
				projectRoot = wd
			}
		}

		candidates := []string{
			"/app/bin/tools/html_scraper",
			filepath.Join(projectRoot, "bin", "tools", "html_scraper"),
			filepath.Join(projectRoot, "bin", "html-scraper"),
			"bin/html-scraper",
			"../bin/html-scraper",
		}

		scraperBin := ""
		for _, candidate := range candidates {
			if _, err := os.Stat(candidate); err == nil {
				if abs, err := filepath.Abs(candidate); err == nil {
					scraperBin = abs
				} else {
					scraperBin = candidate
				}
				log.Printf("🔍 [MCP-SCRAPE] Found html_scraper at: %s", scraperBin)
				break
			}
		}

		if scraperBin == "" {
			log.Printf("⚠️ [MCP-SCRAPE] html_scraper binary not found, using fallback HTTP client with HTML cleaning")

			client := NewSafeHTTPClient()
			content, err := client.SafeGetWithContentCheck(ctx, url)
			if err != nil {
				return nil, err
			}

			cleaned := cleanHTMLForDisplay(content)

			return map[string]interface{}{
				"content": []map[string]interface{}{
					{
						"type": "text",
						"text": cleaned,
					},
				},
			}, nil
		}

		cmd := exec.CommandContext(ctx, scraperBin, "-url", url)
		output, err := cmd.CombinedOutput()
		if err != nil {
			return nil, fmt.Errorf("scraper failed: %v - %s", err, string(output))
		}

		// Parse the JSON output from html-scraper
		var scraperResult map[string]interface{}
		if err := json.Unmarshal(output, &scraperResult); err != nil {

			return map[string]interface{}{
				"content": []map[string]interface{}{
					{
						"type": "text",
						"text": string(output),
					},
				},
			}, nil
		}

		// Format the scraped content nicely
		var contentText strings.Builder

		if title, ok := scraperResult["title"].(string); ok && title != "" {
			contentText.WriteString("# ")
			contentText.WriteString(title)
			contentText.WriteString("\n\n")
		}

		if items, ok := scraperResult["items"].([]interface{}); ok {
			for _, item := range items {
				if itemMap, ok := item.(map[string]interface{}); ok {
					if text, ok := itemMap["text"].(string); ok && text != "" {
						itemType, _ := itemMap["type"].(string)
						switch itemType {
						case "heading":
							contentText.WriteString("## ")
							contentText.WriteString(text)
							contentText.WriteString("\n\n")
						case "paragraph":
							contentText.WriteString(text)
							contentText.WriteString("\n\n")
						default:
							contentText.WriteString(text)
							contentText.WriteString("\n\n")
						}
					}
				}
			}
		}

		return map[string]interface{}{
			"content": []map[string]interface{}{
				{
					"type": "text",
					"text": contentText.String(),
				},
			},
		}, nil

	case "smart_scrape":
		url, _ := args["url"].(string)

		goal, _ := args["goal"].(string)
		if url == "" || goal == "" {
			return nil, fmt.Errorf("url and goal parameters required")
		}

		// Support optional hints
		var userConfig *ScrapeConfig
		if ts, _ := args["typescript_config"].(string); ts != "" {
			userConfig = &ScrapeConfig{
				TypeScriptConfig: ts,
				Extractions:      make(map[string]string),
			}
		}

		if ext, ok := args["extractions"].(map[string]interface{}); ok {
			if userConfig == nil {
				userConfig = &ScrapeConfig{Extractions: make(map[string]string)}
			}
			for k, v := range ext {
				if vStr, ok := v.(string); ok {
					userConfig.Extractions[k] = vStr
				}
			}
		}

		numHints := 0
		if userConfig != nil {
			numHints = len(userConfig.Extractions)
		}
		log.Printf("🔍 [MCP-SMART-SCRAPE] User hints: %d extractions", numHints)
		if userConfig != nil {
			for k, v := range userConfig.Extractions {
				log.Printf("   📋 %s: %s", k, v)
			}
		}

		return s.executeSmartScrape(ctx, url, goal, userConfig)

	case "execute_code":
		code, _ := args["code"].(string)
		language, _ := args["language"].(string)

		if code == "" {
			return nil, fmt.Errorf("code parameter required")
		}
		if language == "" {
			language = "python"
		}

		execMethod := strings.TrimSpace(os.Getenv("EXECUTION_METHOD"))
		enableARM := strings.TrimSpace(os.Getenv("ENABLE_ARM64_TOOLS")) == "true"
		isARM64 := runtime.GOARCH == "arm64" || runtime.GOARCH == "aarch64"

		sshEnabled := execMethod == "ssh" || (isARM64 && (enableARM || execMethod != "docker"))

		if sshEnabled {
			log.Printf("🚀 [MCP-EXEC] Attempting SSH execution (EXECUTION_METHOD=%s, ARM64=%v)", execMethod, isARM64)
			sshParams := map[string]interface{}{
				"code":     code,
				"language": language,
				"timeout":  300,
			}

			result, err := s.callExternalHDNTool(ctx, "tool_ssh_executor", sshParams)
			if err == nil {

				success, _ := result["success"].(bool)
				output, _ := result["output"].(string)
				errorMsg, _ := result["error"].(string)

				return map[string]interface{}{
					"success": success,
					"output":  output,
					"error":   errorMsg,
					"files":   nil,
				}, nil
			}

			log.Printf("⚠️ [MCP-EXEC] SSH executor failed: %v — falling back to local Docker executor", err)
		}

		executor := NewSimpleDockerExecutor()
		req := &DockerExecutionRequest{
			Language: language,
			Code:     code,
			Timeout:  60,
		}

		result, err := executor.ExecuteCode(ctx, req)
		if err != nil {
			return nil, err
		}

		return map[string]interface{}{
			"success": result.Success,
			"output":  result.Output,
			"error":   result.Error,
			"files":   result.Files,
		}, nil

	case "read_file":
		path, ok := args["path"].(string)
		if !ok {
			return nil, fmt.Errorf("path parameter required")
		}

		if strings.Contains(path, "..") {
			return nil, fmt.Errorf("invalid path: traversal not allowed")
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("failed to read file: %v", err)
		}
		return string(content), nil

	case "weather":
		return s.callExternalHDNTool(ctx, "tool_weather", args)

	case "browse_web":
		url, _ := args["url"].(string)
		instructions, _ := args["instructions"].(string)
		if url == "" || instructions == "" {
			return nil, fmt.Errorf("url and instructions parameters required")
		}

		// Convert actions array to JS if provided
		tsConfig := ""
		if actions, ok := args["actions"].([]interface{}); ok && len(actions) > 0 {
			log.Printf("🩹 [MCP-BROWSE] Converting actions array to JS...")
			tsConfig = convertStepsToJS(map[string]interface{}{"actions": actions})
		}

		async, _ := args["async"].(bool)
		return s.scrapeWithConfig(ctx, url, instructions, tsConfig, async, nil, true, nil)

	case "secret_scanner", "secret_scan":
		path, _ := args["path"].(string)
		text, _ := args["text"].(string)

		projectRoot := os.Getenv("AGI_PROJECT_ROOT")
		if projectRoot == "" {
			if wd, err := os.Getwd(); err == nil {
				projectRoot = wd
			}
		}

		candidates := []string{
			"/app/bin/tools/secret_scanner",
			filepath.Join(projectRoot, "bin", "tools", "secret_scanner"),
			"bin/tools/secret_scanner",
		}

		bin := ""
		for _, c := range candidates {
			if _, err := os.Stat(c); err == nil {
				bin = c
				break
			}
		}

		if bin == "" {
			return nil, fmt.Errorf("secret_scanner binary not found")
		}

		var cmd *exec.Cmd
		if path != "" {
			cmd = exec.CommandContext(ctx, bin, "-path", path)
		} else {
			cmd = exec.CommandContext(ctx, bin)
			cmd.Stdin = strings.NewReader(text)
		}

		output, err := cmd.CombinedOutput()
		if err != nil {
			return nil, fmt.Errorf("secret scanner failed: %v - %s", err, string(output))
		}

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

		return result, nil

	default:
		return nil, fmt.Errorf("unknown internal tool: %s", toolName)
	}
}

// queryViaHDN queries Neo4j via HDN's knowledge query endpoint
func (s *MCPKnowledgeServer) queryViaHDN(ctx context.Context, cypherQuery string) (interface{}, error) {
	queryURL := s.hdnURL + "/api/v1/knowledge/query"
	if s.hdnURL == "" {
		queryURL = "http://localhost:8081/api/v1/knowledge/query"
	} else {

		if isSelfConnectionHDN(queryURL) {

			parsedURL, err := url.Parse(queryURL)
			if err == nil {
				port := parsedURL.Port()
				if port == "" {
					port = "8081"
				}
				queryURL = fmt.Sprintf("http://localhost:%s/api/v1/knowledge/query", port)
				log.Printf("🔧 [MCP-KNOWLEDGE] Detected self-connection for HDN query, using localhost: %s", queryURL)
			}
		}
	}

	reqBody := map[string]string{"query": cypherQuery}
	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	resp, err := http.Post(queryURL, "application/json", strings.NewReader(string(jsonData)))
	if err != nil {
		return nil, fmt.Errorf("failed to query HDN: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HDN returned status %d", resp.StatusCode)
	}

	var result struct {
		Results []map[string]interface{} `json:"results"`
		Count   int                      `json:"count"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"results": result.Results,
		"count":   result.Count,
	}, nil
}

// HandleScreenshot serves the latest scrape screenshot from memory
func (s *MCPKnowledgeServer) HandleScreenshot(w http.ResponseWriter, r *http.Request) {
	s.screenshotMu.RLock()
	data := s.latestScreenshot
	s.screenshotMu.RUnlock()

	if len(data) == 0 {

		artifactsPath := "/app/artifacts/latest_screenshot.png"
		if projectRoot := os.Getenv("AGI_PROJECT_ROOT"); projectRoot != "" {
			artifactsPath = filepath.Join(projectRoot, "artifacts", "latest_screenshot.png")
		}

		var err error
		data, err = os.ReadFile(artifactsPath)
		if err != nil {
			log.Printf("⚠️ [SCREENSHOT] Could not read fallback screenshot from %s: %v", artifactsPath, err)
			http.Error(w, "No screenshot available", http.StatusNotFound)
			return
		}
	}

	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(data)
}

// readGoogleWorkspace calls n8n webhook to fetch email/calendar data
func (s *MCPKnowledgeServer) readGoogleWorkspace(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	query, _ := args["query"].(string)
	dataType, _ := args["type"].(string)

	limit := 5
	if l, ok := args["limit"].(float64); ok {
		limit = int(l)
		if limit <= 0 {
			limit = 5
		}
		if limit > 50 {
			limit = 50
			log.Printf("⚠️ [MCP-KNOWLEDGE] Limit capped at 50 to prevent timeouts")
		}
	}

	log.Printf("📥 [MCP-KNOWLEDGE] readGoogleWorkspace called with query: '%s', type: '%s', limit: %d", query, dataType, limit)

	payload := map[string]interface{}{
		"query": query,
		"type":  dataType,
		"limit": limit,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	n8nURL := os.Getenv("N8N_WEBHOOK_URL")
	if n8nURL == "" {
		n8nURL = "http://n8n.n8n.svc.cluster.local:5678/webhook/google-workspace"
	}

	req, err := http.NewRequestWithContext(ctx, "POST", n8nURL, bytes.NewReader(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	if secret := os.Getenv("N8N_WEBHOOK_SECRET"); secret != "" {
		secret = strings.TrimSpace(secret)
		secretToSend := secret
		if !isBase64Like(secret) {
			secretToSend = base64.StdEncoding.EncodeToString([]byte(secret))
		}
		req.Header.Set("X-Webhook-Secret", secretToSend)
	}

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{
		Timeout:   60 * time.Second,
		Transport: tr,
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call n8n webhook: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.Printf("❌ [MCP-KNOWLEDGE] n8n returned error status %d. Response body: %s", resp.StatusCode, string(bodyBytes))
		return nil, fmt.Errorf("n8n returned status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	log.Printf("📥 [MCP-KNOWLEDGE] n8n response status: %d, body length: %d bytes", resp.StatusCode, len(bodyBytes))
	if len(bodyBytes) == 0 {
		log.Printf("⚠️ [MCP-KNOWLEDGE] n8n returned EMPTY response body!")
		return map[string]interface{}{
			"results": []interface{}{},
			"message": "n8n returned empty response",
		}, nil
	}

	preview := string(bodyBytes)
	if len(preview) > 500 {
		preview = preview[:500] + "..."
	}
	log.Printf("📥 [MCP-KNOWLEDGE] n8n response preview: %s", preview)

	// Parse response
	var result interface{}
	if err := json.Unmarshal(bodyBytes, &result); err != nil {

		log.Printf("⚠️ [MCP-KNOWLEDGE] n8n response is not JSON, returning as string. Length: %d, Error: %v", len(bodyBytes), err)
		return map[string]interface{}{
			"result": string(bodyBytes),
		}, nil
	}

	resultType := "unknown"
	resultLen := 0
	var finalResult interface{} = result

	if resultArray, ok := result.([]interface{}); ok {
		resultType = "array"
		resultLen = len(resultArray)
		log.Printf("📧 [MCP-KNOWLEDGE] n8n returned array with %d items", resultLen)

		if resultLen > 0 {

			if firstItem, ok := resultArray[0].(map[string]interface{}); ok {
				var keys []string
				for k := range firstItem {
					keys = append(keys, k)
				}
				log.Printf("📧 [MCP-KNOWLEDGE] First item has keys: %v", keys)

				if _, hasJson := firstItem["json"]; hasJson {
					log.Printf("📧 [MCP-KNOWLEDGE] Extracting data from 'json' key (n8n allIncomingItems format)")

					extractedItems := make([]interface{}, 0, resultLen)
					for _, item := range resultArray {
						if itemMap, ok := item.(map[string]interface{}); ok {
							if jsonVal, ok := itemMap["json"]; ok {
								extractedItems = append(extractedItems, jsonVal)
							} else {

								extractedItems = append(extractedItems, item)
							}
						} else {
							extractedItems = append(extractedItems, item)
						}
					}
					finalResult = extractedItems
					resultLen = len(extractedItems)
					log.Printf("📧 [MCP-KNOWLEDGE] Extracted %d items from n8n json structure", resultLen)

					if resultLen > 0 {
						if firstExtracted, ok := extractedItems[0].(map[string]interface{}); ok {
							var extractedKeys []string
							for k := range firstExtracted {
								extractedKeys = append(extractedKeys, k)
							}
							log.Printf("📧 [MCP-KNOWLEDGE] First extracted email item has keys: %v", extractedKeys)
						}
					}
				} else {

					hasSubject := false
					hasFrom := false
					for k := range firstItem {
						kLower := strings.ToLower(k)
						if kLower == "subject" {
							hasSubject = true
						}
						if kLower == "from" {
							hasFrom = true
						}
					}
					if hasSubject || hasFrom {
						log.Printf("📧 [MCP-KNOWLEDGE] Items are already email objects (no json wrapper, hasSubject=%v, hasFrom=%v)", hasSubject, hasFrom)
						finalResult = resultArray
					}
				}
			}
		}
	} else if resultMap, ok := result.(map[string]interface{}); ok {
		resultType = "map"
		var keys []string
		for k := range resultMap {
			keys = append(keys, k)
		}
		log.Printf("📧 [MCP-KNOWLEDGE] n8n returned map with keys: %v", keys)

		hasSubject := false
		hasFrom := false
		for k := range resultMap {
			kLower := strings.ToLower(k)
			if kLower == "subject" {
				hasSubject = true
			}
			if kLower == "from" {
				hasFrom = true
			}
		}

		if hasSubject || hasFrom {
			log.Printf("📧 [MCP-KNOWLEDGE] Single email object detected (hasSubject=%v, hasFrom=%v), wrapping in array", hasSubject, hasFrom)

			finalResult = []interface{}{resultMap}
			resultLen = 1
			resultType = "array (wrapped)"
		} else if emailsData, hasEmails := resultMap["emails"]; hasEmails {

			log.Printf("📧 [MCP-KNOWLEDGE] Extracting data from 'emails' key in map")
			if emailsArray, ok := emailsData.([]interface{}); ok {
				finalResult = emailsArray
				resultLen = len(emailsArray)
				resultType = "array (from emails key)"
				log.Printf("📧 [MCP-KNOWLEDGE] Extracted %d emails from 'emails' key", resultLen)
			} else {
				log.Printf("⚠️ [MCP-KNOWLEDGE] 'emails' key is not an array, type: %T", emailsData)
				finalResult = []interface{}{emailsData}
				resultLen = 1
			}
		} else if jsonData, hasJson := resultMap["json"]; hasJson {

			log.Printf("📧 [MCP-KNOWLEDGE] Extracting data from 'json' key in map")
			if jsonArray, ok := jsonData.([]interface{}); ok {
				finalResult = jsonArray
				resultLen = len(jsonArray)
				resultType = "array (from json key)"
			} else if jsonMap, ok := jsonData.(map[string]interface{}); ok {

				hasSubject := false
				hasFrom := false
				for k := range jsonMap {
					kLower := strings.ToLower(k)
					if kLower == "subject" {
						hasSubject = true
					}
					if kLower == "from" {
						hasFrom = true
					}
				}
				if hasSubject || hasFrom {
					finalResult = []interface{}{jsonMap}
					resultLen = 1
					resultType = "array (wrapped from json)"
				} else {
					finalResult = []interface{}{jsonMap}
					resultLen = 1
				}
			} else {
				finalResult = []interface{}{jsonData}
				resultLen = 1
			}
		}
	} else {
		log.Printf("⚠️ [MCP-KNOWLEDGE] n8n returned unexpected type: %T", result)
	}

	log.Printf("✅ [MCP-KNOWLEDGE] Successfully retrieved Google Workspace data (type: %s, size: %d)", resultType, resultLen)

	if resultArray, ok := finalResult.([]interface{}); ok {
		log.Printf("📧 [MCP-KNOWLEDGE] Returning %d email(s) to caller", len(resultArray))
	}

	return finalResult, nil
}

// callExternalHDNTool calls an external tool on the HDN server
func (s *MCPKnowledgeServer) callExternalHDNTool(ctx context.Context, toolID string, params map[string]interface{}) (map[string]interface{}, error) {

	baseURL := s.hdnURL
	if baseURL == "" {
		baseURL = "http://localhost:8080"
	}
	url := fmt.Sprintf("%s/api/v1/tools/%s/invoke", baseURL, toolID)

	jsonData, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal params: %v", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 300 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("tool call failed with status %d: %s", resp.StatusCode, string(body))
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %v", err)
	}

	return result, nil
}
