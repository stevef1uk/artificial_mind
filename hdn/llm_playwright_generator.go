package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
)

// PlaywrightGenerator uses LLM to analyze HTML and generate Playwright code
type PlaywrightGenerator struct {
	llmURL   string
	apiKey   string
	provider string // "claude" or "openai"
	model    string
}

// playLLMRequest for Claude or OpenAI
type playLLMRequest struct {
	Model       string           `json:"model"`
	Messages    []playLLMMessage `json:"messages"`
	MaxTokens   int              `json:"max_tokens"`
	Temperature float64          `json:"temperature"`
}

type playLLMMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ClaudeResponse from Claude API
type ClaudeResponse struct {
	Content []struct {
		Text string `json:"text"`
	} `json:"content"`
}

// OpenAIResponse from OpenAI API
type OpenAIResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// AnalyzeAndGeneratePlaywright analyzes HTML and generates Playwright code
func (pg *PlaywrightGenerator) AnalyzeAndGeneratePlaywright(htmlContent string, goal string) (string, error) {
	// Clean HTML to reduce token count
	cleanedHTML := cleanHTMLForLLM(htmlContent, 10000) // Keep first 10k chars

	prompt := fmt.Sprintf(`You are a Playwright code generation expert. Analyze the provided HTML and generate ONLY valid Playwright TypeScript code to achieve the goal.

GOAL: %s

HTML STRUCTURE:
<html>
%s
</html>

REQUIREMENTS:
1. Generate ONLY executable Playwright code (no explanations)
2. Use exact selectors found in the HTML
3. Use page.fill(), page.click(), page.selectOption(), page.keyboard.press()
4. Include proper waits and error handling
5. Start with: await page.waitForLoadState('networkidle');
6. End with final state check or data extraction

Return ONLY the code block, starting with await statements. NO markdown, NO explanations.`, goal, cleanedHTML)

	resp, err := pg.callLLM(prompt)
	if err != nil {
		return "", fmt.Errorf("LLM call failed: %v", err)
	}

	// Extract code from response (remove markdown if present)
	code := extractCodeBlock(resp)
	if code == "" {
		code = resp // Use raw response if no markdown found
	}

	return code, nil
}

func (pg *PlaywrightGenerator) callLLM(prompt string) (string, error) {
	// Build request based on provider
	var reqBody []byte
	var url string
	var headers map[string]string

	if pg.provider == "openai" {
		// OpenAI API request
		req := playLLMRequest{
			Model: pg.model, // gpt-4o-mini is cheapest and still good
			Messages: []playLLMMessage{
				{
					Role:    "user",
					Content: prompt,
				},
			},
			MaxTokens:   2000,
			Temperature: 0.2,
		}
		body, _ := json.Marshal(req)
		reqBody = body
		url = "https://api.openai.com/v1/chat/completions"
		headers = map[string]string{
			"Content-Type":  "application/json",
			"Authorization": fmt.Sprintf("Bearer %s", pg.apiKey),
		}
	} else {
		// Claude API request (default)
		req := playLLMRequest{
			Model: pg.model,
			Messages: []playLLMMessage{
				{
					Role:    "user",
					Content: prompt,
				},
			},
			MaxTokens:   2000,
			Temperature: 0.2,
		}
		body, _ := json.Marshal(req)
		reqBody = body
		url = "https://api.anthropic.com/v1/messages"
		headers = map[string]string{
			"Content-Type": "application/json",
			"x-api-key":    pg.apiKey,
		}
	}

	client := &http.Client{Timeout: 30 * time.Second}
	httpReq, _ := http.NewRequest("POST", url, bytes.NewBuffer(reqBody))

	for k, v := range headers {
		httpReq.Header.Set(k, v)
	}

	resp, err := client.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("LLM request failed: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if pg.provider == "openai" {
		var oaiResp OpenAIResponse
		if err := json.Unmarshal(body, &oaiResp); err == nil {
			if len(oaiResp.Choices) > 0 {
				return oaiResp.Choices[0].Message.Content, nil
			}
		}
	} else {
		var claudeResp ClaudeResponse
		if err := json.Unmarshal(body, &claudeResp); err == nil {
			if len(claudeResp.Content) > 0 {
				return claudeResp.Content[0].Text, nil
			}
		}
	}

	return string(body), nil
}

func cleanHTMLForLLM(html string, maxChars int) string {
	// Remove script tags
	re := regexp.MustCompile(`(?s)<script[^>]*>.*?</script>`)
	html = re.ReplaceAllString(html, "")

	// Remove style tags
	re = regexp.MustCompile(`(?s)<style[^>]*>.*?</style>`)
	html = re.ReplaceAllString(html, "")

	// Remove comments
	re = regexp.MustCompile(`(?s)<!--.*?-->`)
	html = re.ReplaceAllString(html, "")

	// Condense whitespace
	html = strings.Join(strings.Fields(html), " ")

	if len(html) > maxChars {
		html = html[:maxChars] + "..."
	}

	return html
}

func extractCodeBlock(response string) string {
	// Try to extract code from markdown code block
	re := regexp.MustCompile("```(?:typescript|javascript|js)?\\n?([\\s\\S]*?)```")
	matches := re.FindStringSubmatch(response)
	if len(matches) > 1 {
		return strings.TrimSpace(matches[1])
	}

	// If no markdown block, return the response if it looks like code
	if strings.Contains(response, "await page") {
		return response
	}

	return ""
}

// NewPlaywrightGenerator creates a new generator with OpenAI or Claude
func NewPlaywrightGenerator(provider string) *PlaywrightGenerator {
	pg := &PlaywrightGenerator{
		provider: strings.ToLower(provider),
	}

	if pg.provider == "openai" {
		// OpenAI configuration
		pg.apiKey = os.Getenv("OPENAI_API_KEY")
		pg.model = os.Getenv("OPENAI_MODEL")
		if pg.model == "" {
			pg.model = "gpt-4o-mini" // Cheapest model, still excellent for code
		}
	} else {
		// Claude configuration (default)
		pg.provider = "claude"
		pg.apiKey = os.Getenv("ANTHROPIC_API_KEY")
		pg.model = os.Getenv("CLAUDE_MODEL")
		if pg.model == "" {
			pg.model = "claude-3-5-sonnet-20241022"
		}
	}

	return pg
}
