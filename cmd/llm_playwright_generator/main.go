package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/joho/godotenv"
	openai "github.com/sashabaranov/go-openai"
)

type generatorConfig struct {
	Provider string
	Model    string
	APIKey   string
	BaseURL  string
}

func main() {
	_ = godotenv.Load(".env")

	provider := flag.String("provider", "openai", "LLM provider (openai only)")
	model := flag.String("model", "", "Model name")
	output := flag.String("output", "", "Write output to file")
	flag.Parse()

	if flag.NArg() < 2 {
		fmt.Println("Usage: llm_playwright_generator [--provider openai|claude] [--model <model>] [--output <file>] <url> <goal>")
		os.Exit(1)
	}

	url := flag.Arg(0)
	goal := strings.Join(flag.Args()[1:], " ")

	cfg, err := loadConfig(*provider, *model)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("==========================================")
	fmt.Println("LLM Playwright Code Generator")
	fmt.Println("==========================================")
	fmt.Printf("Provider: %s\n", strings.ToUpper(cfg.Provider))
	fmt.Printf("URL:  %s\n", url)
	fmt.Printf("Goal: %s\n", goal)
	fmt.Println("==========================================\n")

	html, err := fetchWebsite(url)
	if err != nil {
		fmt.Printf("Failed to fetch %s: %v\n", url, err)
		os.Exit(1)
	}

	cleanedHTML := cleanHTMLForLLM(html, 10000)
	prompt := buildPrompt(goal, cleanedHTML)

	fmt.Printf("Calling %s to generate Playwright code...\n", strings.ToUpper(cfg.Provider))
	response, err := callLLM(cfg, prompt)
	if err != nil {
		fmt.Printf("LLM call failed: %v\n", err)
		os.Exit(1)
	}

	code := extractCodeBlock(response)
	if code == "" {
		code = response
	}

	if *output != "" {
		if err := os.WriteFile(*output, []byte(code), 0644); err != nil {
			fmt.Printf("Failed to write output: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("\nSaved to: %s\n", *output)
	} else {
		fmt.Println("\nGenerated Playwright Code:\n")
		fmt.Println("```typescript")
		fmt.Println(code)
		fmt.Println("```")
	}
}

func loadConfig(provider string, model string) (generatorConfig, error) {
	cfg := generatorConfig{Provider: strings.ToLower(provider)}

	if cfg.Provider != "openai" {
		return cfg, fmt.Errorf("unsupported provider: %s", provider)
	}

	cfg.APIKey = firstEnv("PLAYWRIGHT_GEN_OPENAI_API_KEY", "OPENAI_API_KEY")
	if cfg.APIKey == "" {
		return cfg, fmt.Errorf("PLAYWRIGHT_GEN_OPENAI_API_KEY not set")
	}
	cfg.Model = model
	if cfg.Model == "" {
		cfg.Model = firstEnv("PLAYWRIGHT_GEN_OPENAI_MODEL", "OPENAI_MODEL")
	}
	if cfg.Model == "" {
		cfg.Model = "gpt-4o-mini"
	}
	cfg.BaseURL = firstEnv("PLAYWRIGHT_GEN_OPENAI_BASE_URL", "OPENAI_BASE_URL")
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.openai.com/v1"
	}
	return cfg, nil
}

func buildPrompt(goal string, html string) string {
	return fmt.Sprintf(`You are a Playwright code generation expert. Analyze the provided HTML and generate ONLY valid Playwright TypeScript code to achieve the goal.

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

Return ONLY the code block, starting with await statements. NO markdown, NO explanations.`, goal, html)
}

func fetchWebsite(url string) (string, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("unexpected status: %s", resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	return string(body), nil
}

func callLLM(cfg generatorConfig, prompt string) (string, error) {
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	openAIConfig := openai.DefaultConfig(cfg.APIKey)
	if cfg.BaseURL != "" {
		openAIConfig.BaseURL = cfg.BaseURL
	}

	client := openai.NewClientWithConfig(openAIConfig)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := client.CreateChatCompletion(
		ctx,
		openai.ChatCompletionRequest{
			Model: cfg.Model,
			Messages: []openai.ChatCompletionMessage{
				{
					Role:    openai.ChatMessageRoleUser,
					Content: prompt,
				},
			},
			MaxTokens:   2000,
			Temperature: 0.2,
		},
	)
	if err != nil {
		return "", err
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("no response choices returned")
	}

	return resp.Choices[0].Message.Content, nil
}

func cleanHTMLForLLM(html string, maxChars int) string {
	re := regexp.MustCompile(`(?s)<script[^>]*>.*?</script>`)
	html = re.ReplaceAllString(html, "")

	re = regexp.MustCompile(`(?s)<style[^>]*>.*?</style>`)
	html = re.ReplaceAllString(html, "")

	re = regexp.MustCompile(`(?s)<!--.*?-->`)
	html = re.ReplaceAllString(html, "")

	html = strings.Join(strings.Fields(html), " ")

	if len(html) > maxChars {
		html = html[:maxChars] + "..."
	}

	return html
}

func extractCodeBlock(response string) string {
	re := regexp.MustCompile("```(?:typescript|javascript|js)?\\n?([\\s\\S]*?)```")
	matches := re.FindStringSubmatch(response)
	if len(matches) > 1 {
		return strings.TrimSpace(matches[1])
	}

	if strings.Contains(response, "await page") {
		return response
	}

	return ""
}

func firstEnv(keys ...string) string {
	for _, key := range keys {
		if value := os.Getenv(key); strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
