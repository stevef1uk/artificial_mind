package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

)

// deepResearch performs multi-step autonomous research
func (s *MCPKnowledgeServer) deepResearch(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	topic, _ := args["topic"].(string)
	depthVal, _ := args["depth"].(float64)
	depth := int(depthVal)
	sessionID, _ := args["session_id"].(string)
	if sessionID == "" {
		sessionID = "research_" + time.Now().Format("150405")
	}

	if topic == "" {
		return nil, fmt.Errorf("topic is required")
	}

	if depth <= 0 {
		depth = 1
	}
	if depth > 3 {
		depth = 3
	}

	log.Printf("🔍 [DEEP-RESEARCH] Starting research on: %s (depth: %d, session: %s)", topic, depth, sessionID)

	queryPrompt := fmt.Sprintf("Generate 3 diverse search queries to thoroughly research this topic: %s. Return only the queries, one per line.", topic)
	queriesStr, err := s.llmClient.callLLMWithContextAndPriority(ctx, queryPrompt, PriorityHigh)
	if err != nil {
		return nil, fmt.Errorf("failed to generate queries: %w", err)
	}
	queries := strings.Split(queriesStr, "\n")
	var activeQueries []string

	numberRe := regexp.MustCompile(`^(\d+[\.\)]\s*|-\s*)`)
	for _, q := range queries {
		q = strings.TrimSpace(q)
		if q == "" {
			continue
		}

		if strings.HasSuffix(q, ":") || strings.Contains(strings.ToLower(q), "here are") || strings.Contains(strings.ToLower(q), "search queries") {
			continue
		}

		q = numberRe.ReplaceAllString(q, "")
		q = strings.Trim(q, `"'`)
		q = strings.TrimSpace(q)
		if q != "" {
			activeQueries = append(activeQueries, q)
		}
	}
	if len(activeQueries) == 0 {
		activeQueries = []string{topic}
	}

	results := []map[string]interface{}{}
	visitedURLs := make(map[string]bool)

	queryLimit := depth
	if queryLimit > len(activeQueries) {
		queryLimit = len(activeQueries)
	}

	for i := 0; i < queryLimit; i++ {
		query := activeQueries[i]
		log.Printf("🌐 [DEEP-RESEARCH] Researching query %d/%d: %s", i+1, queryLimit, query)

		searchURL := fmt.Sprintf("https://www.bing.com/search?q=%s", url.QueryEscape(query))
		log.Printf("🔎 [DEEP-RESEARCH] Searching Bing for: %s", query)

		extractScript := `
const items = await page.$$eval('.b_algo', els => els.slice(0,8).map(el => {
  const a = el.querySelector('h2 a');
  const cite = el.querySelector('cite');
  if (!a) return null;
  // Bing may store the real URL in data-href or keep it as href before JS rewrites
  const href = a.getAttribute('data-href') || a.getAttribute('href') || '';
  const title = a.innerText || a.textContent || '';
  const displayUrl = cite ? (cite.innerText || cite.textContent || '') : '';
  return {href, title, displayUrl};
})).filter(x => x && x.href);
await page.evaluate(r => { window.__searchResults = r; }, items);
`

		tsExtractionResult, err := s.scrapeWithConfig(ctx, searchURL, "", extractScript, false, nil, false, nil)
		if err != nil {
			log.Printf("⚠️ [DEEP-RESEARCH] Bing search failed for query '%s': %v", query, err)
			continue
		}

		searchResult, _ := s.scrapeWithConfig(ctx, searchURL, "", "", false, nil, true, nil)

		// Build a list of real external URLs
		var topLinks []map[string]string

		getHTML := func(res interface{}) string {
			if m, ok := res.(map[string]interface{}); ok {
				if r, ok := m["result"].(map[string]interface{}); ok {
					for _, k := range []string{"cleaned_html", "raw_html"} {
						if h, ok := r[k].(string); ok && h != "" {
							return h
						}
					}
				}
			}
			return ""
		}

		searchHTML := getHTML(searchResult)
		_ = getHTML(tsExtractionResult)

		if searchHTML != "" {

			citeRe := regexp.MustCompile(`(?s)<li[^>]*class="[^"]*b_algo[^"]*"[^>]*>(.*?)</li>`)
			algoBlocks := citeRe.FindAllStringSubmatch(searchHTML, -1)
			titleRe := regexp.MustCompile(`<h2[^>]*>.*?<a[^>]*>([^<]+)</a>`)
			citeTextRe := regexp.MustCompile(`<cite[^>]*>(https?://[^<\s]+)</cite>`)
			citeDomainRe := regexp.MustCompile(`<cite[^>]*>([^<]+)</cite>`)

			for _, block := range algoBlocks {
				blockHTML := block[1]

				title := ""
				if m := titleRe.FindStringSubmatch(blockHTML); len(m) > 1 {
					title = strings.TrimSpace(m[1])
				}

				fullURL := ""
				if m := citeTextRe.FindStringSubmatch(blockHTML); len(m) > 1 {
					fullURL = strings.TrimSpace(m[1])
				} else if m := citeDomainRe.FindStringSubmatch(blockHTML); len(m) > 1 {

					domainPart := strings.TrimSpace(m[1])
					domainPart = strings.SplitN(domainPart, "›", 2)[0]
					domainPart = strings.TrimSpace(domainPart)
					if domainPart != "" && !strings.Contains(domainPart, " ") {
						fullURL = "https://" + domainPart
					}
				}
				if title != "" && fullURL != "" {
					topLinks = append(topLinks, map[string]string{
						"title": title,
						"url":   fullURL,
					})
					if len(topLinks) >= 5 {
						break
					}
				}
			}
			log.Printf("🔗 [DEEP-RESEARCH] Extracted %d links from Bing SERP HTML", len(topLinks))
		}

		if len(topLinks) == 0 {
			log.Printf("⚠️ [DEEP-RESEARCH] No links found in Bing search for query '%s'", query)
		}

		for _, link := range topLinks {
			targetURL := link["url"]
			if targetURL == "" || visitedURLs[targetURL] || !strings.HasPrefix(targetURL, "http") {
				continue
			}
			visitedURLs[targetURL] = true

			log.Printf("📄 [DEEP-RESEARCH] Visiting source: %s", targetURL)

			pageResult, err := s.scrapeWithConfig(ctx, targetURL, "", "", false, nil, true, nil)
			if err != nil {
				log.Printf("⚠️ [DEEP-RESEARCH] Failed to visit %s: %v", targetURL, err)
				continue
			}

			pageText := ""
			if m, ok := pageResult.(map[string]interface{}); ok {
				if r, ok := m["result"].(map[string]interface{}); ok {
					for _, key := range []string{"page_content", "cleaned_html", "raw_html"} {
						if h, ok := r[key].(string); ok && h != "" {

							tagRe := regexp.MustCompile(`<[^>]+>`)
							pageText = tagRe.ReplaceAllString(h, " ")

							wsRe := regexp.MustCompile(`\s+`)
							pageText = wsRe.ReplaceAllString(pageText, " ")
							if len(pageText) > 4000 {
								pageText = pageText[:4000]
							}
							break
						}
					}
				}
			}

			if pageText == "" {
				log.Printf("⚠️ [DEEP-RESEARCH] No text extracted from %s, skipping", targetURL)
				continue
			}

			log.Printf("✅ [DEEP-RESEARCH] Got %d chars from %s", len(pageText), targetURL)
			results = append(results, map[string]interface{}{
				"source":  targetURL,
				"title":   link["title"],
				"content": pageText,
			})

			if len(results) >= (i+1)*3 {
				break
			}
		}
	}

	log.Printf("✍️ [DEEP-RESEARCH] Synthesizing report from %d sources...", len(results))
	resultsJSON, _ := json.MarshalIndent(results, "", "  ")

	synthesisPrompt := fmt.Sprintf(`You are a lead researcher. I have gathered the following information on the topic: "%s".

Data Gathered:
%s

Please synthesize this into a professional research report. 
The report MUST include:
1. Executive Summary
2. Key Findings (with bullet points)
3. Diverse Perspectives (if applicable)
4. List of Sources correctly cited

Format the output in high-quality Markdown.`, topic, string(resultsJSON))

	report, err := s.llmClient.callLLMWithContextAndPriority(ctx, synthesisPrompt, PriorityHigh)
	if err != nil {
		return nil, fmt.Errorf("synthesis failed: %w", err)
	}

	return map[string]interface{}{
		"topic":       topic,
		"report":      report,
		"sources":     results,
		"session_id":  sessionID,
		"status":      "completed",
		"sources_cnt": len(results),
	}, nil
}

// picoclawQuery handles reasoning queries to the PicoClaw agentic AI via Telegram.
// It sends the prompt to a configured Telegram chat (PicoClaw's inbox) and waits for
// the response to appear in Redis, written by the /api/v1/picoclaw/response callback
// that PicoClaw (or the n8n workflow forwarding its reply) calls.
func (s *MCPKnowledgeServer) picoclawQuery(ctx context.Context, arguments map[string]interface{}) (interface{}, error) {

	prompt, _ := arguments["prompt"].(string)
	if prompt == "" {
		topic, _ := arguments["topic"].(string)
		prompt = topic
	}
	if prompt == "" {
		query, _ := arguments["query"].(string)
		prompt = query
	}
	if prompt == "" {
		text, _ := arguments["text"].(string)
		prompt = text
	}
	if prompt == "" {
		msg, _ := arguments["message"].(string)
		prompt = msg
	}

	if prompt == "" {
		for k, v := range arguments {
			if sv, ok := v.(string); ok && sv != "" && k != "chat_id" {
				prompt = sv
				log.Printf("📥 [PICOCLAW] Auto-detected '%s' as prompt from unknown param: %s", prompt, k)
				break
			}
		}
	}

	if prompt == "" {
		return nil, fmt.Errorf("prompt, topic, query, or message required (found: %v)", arguments)
	}

	prompt = strings.TrimSpace(prompt)

	// Determine the Telegram chat ID for PicoClaw.
	// Use override from the call arguments first, then env var.
	chatID, _ := arguments["chat_id"].(string)
	if chatID == "" {
		chatID = os.Getenv("PICOCLAW_TELEGRAM_CHAT_ID")
	}
	if chatID == "" {
		return nil, fmt.Errorf("PICOCLAW_TELEGRAM_CHAT_ID environment variable not set; cannot route to PicoClaw via Telegram")
	}

	log.Printf("🤖 [PICOCLAW] Sending query to PicoClaw via Telegram chat %s | Prompt: %s", chatID, prompt)

	// Send the prompt as a Telegram message to PicoClaw's channel.
	webhookURL := os.Getenv("TELEGRAM_OUTBOUND_WEBHOOK")
	botToken := os.Getenv("TELEGRAM_BOT_TOKEN")

	if webhookURL == "" && botToken == "" {
		return nil, fmt.Errorf("neither TELEGRAM_OUTBOUND_WEBHOOK nor TELEGRAM_BOT_TOKEN is set; cannot send Telegram message to PicoClaw")
	}

	// Build the outgoing message payload.
	var sendErr error
	if webhookURL != "" {
		payload := map[string]interface{}{"chat_id": chatID, "message": prompt}
		jsonData, _ := json.Marshal(payload)
		req, err := http.NewRequestWithContext(ctx, "POST", webhookURL, bytes.NewBuffer(jsonData))
		if err != nil {
			sendErr = fmt.Errorf("failed to build webhook request: %w", err)
		} else {
			req.Header.Set("Content-Type", "application/json")
			client := &http.Client{Timeout: 10 * time.Second}
			resp, err := client.Do(req)
			if err != nil {
				sendErr = fmt.Errorf("failed to call Telegram webhook: %w", err)
			} else {
				resp.Body.Close()
				if resp.StatusCode != http.StatusOK {
					sendErr = fmt.Errorf("Telegram webhook returned status %d", resp.StatusCode)
				}
			}
		}
	}

	// Fallback to direct Telegram Bot API if webhook failed or not configured.
	if sendErr != nil || webhookURL == "" {
		if botToken == "" {
			if sendErr != nil {
				return nil, fmt.Errorf("Telegram gateway failed (%v) and TELEGRAM_BOT_TOKEN not set", sendErr)
			}
			return nil, fmt.Errorf("TELEGRAM_BOT_TOKEN not set")
		}
		apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", botToken)
		payload := map[string]interface{}{"chat_id": chatID, "text": prompt}
		jsonData, _ := json.Marshal(payload)
		req, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewBuffer(jsonData))
		if err != nil {
			return nil, fmt.Errorf("failed to build Telegram API request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("failed to send Telegram message to PicoClaw: %w", err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("Telegram Bot API returned status %d when sending to PicoClaw", resp.StatusCode)
		}
	}

	log.Printf("✅ [PICOCLAW] Message sent to PicoClaw via Telegram (chat: %s). Polling Redis for response...", chatID)

	// Poll Redis for the response. PicoClaw (or n8n) should call
	// POST /api/v1/picoclaw/response with {"chat_id": "<chatID>", "response": "..."}
	// which stores the answer at hdn:picoclaw:response:<chatID>.
	cleanChatID := strings.TrimPrefix(chatID, "tg_chat_")
	redisKey := fmt.Sprintf("hdn:picoclaw:response:%s", cleanChatID)

	pollInterval := 3 * time.Second
	timeout := 3 * time.Minute
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("picoclaw query cancelled: %v", ctx.Err())
		default:
		}

		val, err := s.redis.GetDel(ctx, redisKey).Result()
		if err == nil && val != "" {
			log.Printf("✅ [PICOCLAW] Received response (%d bytes) from Redis key %s", len(val), redisKey)
			return map[string]interface{}{
				"response": val,
				"status":   "success",
			}, nil
		}

		log.Printf("⏳ [PICOCLAW] No response yet in Redis (%s), retrying in %v...", redisKey, pollInterval)
		time.Sleep(pollInterval)
	}

	return nil, fmt.Errorf("picoclaw query timed out after %v waiting for response in Redis key %s", timeout, redisKey)
}

// nemoclawQuery handles strategic queries to the Nemoclaw agentic AI via n8n webhook and waits for a response in Redis
func (s *MCPKnowledgeServer) nemoclawQuery(ctx context.Context, arguments map[string]interface{}) (interface{}, error) {

	prompt, _ := arguments["prompt"].(string)
	if prompt == "" {
		topic, _ := arguments["topic"].(string)
		prompt = topic
	}
	if prompt == "" {
		query, _ := arguments["query"].(string)
		prompt = query
	}
	if prompt == "" {
		text, _ := arguments["text"].(string)
		prompt = text
	}
	if prompt == "" {
		msg, _ := arguments["message"].(string)
		prompt = msg
	}
	if prompt == "" {
		input, _ := arguments["input"].(string)
		prompt = input
	}

	if prompt == "" {

		for k, v := range arguments {
			if s, ok := v.(string); ok && s != "" && k != "chat_id" {
				prompt = s
				log.Printf("📥 [NEMOCLAW] Auto-detected '%s' as prompt from unknown param: %s", prompt, k)
				break
			}
		}
	}

	if prompt == "" {
		return nil, fmt.Errorf("prompt, topic, query, or message required (found: %v)", arguments)
	}

	prompt = strings.ReplaceAll(prompt, "your_chat_id_here", "")
	prompt = strings.ReplaceAll(prompt, "YOUR_CHAT_ID", "")
	prompt = strings.TrimSpace(prompt)

	projectRoot := os.Getenv("AGI_PROJECT_ROOT")
	if projectRoot == "" {
		if wd, err := os.Getwd(); err == nil {
			projectRoot = wd
		}
	}

	binPath := ""
	candidates := []string{
		"/app/bin/tools/nemoclaw_ssh_query",
		filepath.Join(projectRoot, "bin", "tools", "nemoclaw_ssh_query"),
		"bin/tools/nemoclaw_ssh_query",
	}

	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			binPath = c
			break
		}
	}

	if binPath == "" {
		binPath = "/app/bin/tools/nemoclaw_ssh_query"
	}

	log.Printf("🤖 [NEMOCLAW] Proxying strategic query to NemoClaw via SSH: %s", prompt)

	cmd := exec.CommandContext(ctx, binPath, "-prompt", prompt)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("nemoclaw SSH query tool failed: %v, output: %s", err, string(out))
	}

	var result map[string]interface{}
	if err := json.Unmarshal(out, &result); err != nil {

		return map[string]interface{}{"response": string(out)}, nil
	}

	if toolErr, ok := result["error"].(string); ok && toolErr != "" {
		return nil, fmt.Errorf("nemoclaw strategic agent error: %s", toolErr)
	}

	return result, nil
}

// researchAgentQuery handles autonomous research queries via n8n webhook
func (s *MCPKnowledgeServer) researchAgentQuery(ctx context.Context, arguments map[string]interface{}) (interface{}, error) {
	topic, _ := arguments["topic"].(string)
	if topic == "" {
		query, _ := arguments["query"].(string)
		topic = query
	}
	if topic == "" {
		return nil, fmt.Errorf("topic or query required")
	}

	depthVal, _ := arguments["depth"].(float64)
	depth := int(depthVal)
	if depth <= 0 {
		depth = 1
	}

	webhookURL := os.Getenv("RESEARCH_WEBHOOK_URL")
	if webhookURL == "" {
		webhookURL = "https://k3s.sjfisher.com/webhook/40a534f4-2041-4eed-b317-738ad99b5cb0"
	}

	log.Printf("🔍 [RESEARCH-AGENT] Triggering research webhook for topic: %s (depth: %d)", topic, depth)

	body, _ := json.Marshal(map[string]interface{}{
		"topic": topic,
		"depth": depth,
	})

	req, err := http.NewRequestWithContext(ctx, "POST", webhookURL, bytes.NewBuffer(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create webhook request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	if secret := os.Getenv("N8N_WEBHOOK_SECRET"); secret != "" {
		secret = strings.TrimSpace(secret)
		secretToSend := secret
		if !isBase64Like(secret) {
			secretToSend = base64.StdEncoding.EncodeToString([]byte(secret))
			log.Printf("🔐 [RESEARCH-AGENT] Base64 encoding plain text secret for n8n webhook")
		}
		req.Header.Set("X-Webhook-Secret", secretToSend)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call research webhook: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("research n8n webhook returned error status: %d", resp.StatusCode)
	}

	var result interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode research response: %v", err)
	}

	return result, nil
}
