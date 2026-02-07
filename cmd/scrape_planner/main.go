package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"async_llm"

	"github.com/playwright-community/playwright-go"
)

type ScrapeConfig struct {
	TypeScriptConfig string            `json:"typescript_config"`
	Extractions      map[string]string `json:"extractions"`
}

func main() {
	urlFlag := flag.String("url", "", "URL to scrape")
	goalFlag := flag.String("goal", "", "What to extract")
	modelFlag := flag.String("model", "", "LLM Model to use")
	flag.Parse()

	if *urlFlag == "" || *goalFlag == "" {
		log.Fatal("Usage: scrape_planner -url <URL> -goal <GOAL> [-model <MODEL>]")
	}

	// 1. Fetch HTML using Playwright
	log.Printf("Starting Playwright to fetch %s...", *urlFlag)

	// Ensure drivers are installed
	err := playwright.Install(&playwright.RunOptions{
		Verbose: false,
	})
	if err != nil {
		log.Printf("Warning: failed to install driver resources: %v", err)
	}

	pw, err := playwright.Run()
	if err != nil {
		log.Fatalf("could not start playwright: %v", err)
	}
	defer pw.Stop()

	browser, err := pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
		Headless: playwright.Bool(true),
	})
	if err != nil {
		log.Fatalf("could not launch browser: %v", err)
	}
	defer browser.Close()

	page, err := browser.NewPage()
	if err != nil {
		log.Fatalf("could not create page: %v", err)
	}

	// 60s timeout
	page.SetDefaultTimeout(60000)

	log.Printf("Navigating to %s...", *urlFlag)
	if _, err = page.Goto(*urlFlag, playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateNetworkidle,
	}); err != nil {
		log.Fatalf("could not goto: %v", err)
	}

	// Wait a bit for renders
	time.Sleep(2 * time.Second)

	// Clean up DOM to reduce token count
	log.Println("Cleaning DOM...")
	_, err = page.Evaluate(`() => {
        // Remove scripts, styles, svgs, comments, etc
        const elements = document.querySelectorAll('script, style, svg, path, link, meta, noscript, iframe, link');
        elements.forEach(el => el.remove());
        
        // Remove hidden elements
        const all = document.querySelectorAll('*');
        all.forEach(el => {
            const style = window.getComputedStyle(el);
            if (style.display === 'none' || style.visibility === 'hidden' || style.opacity === '0') {
                el.remove();
            }
        });
        
        // Keep only structural tags and text
        // (This is hard to perfect in JS, rely on HTML content extraction)
    }`)
	if err != nil {
		log.Printf("Warning: DOM cleanup failed: %v", err)
	}

	content, err := page.Content()
	if err != nil {
		log.Fatalf("Failed to fetch HTML content: %v", err)
	}

	// Normalize quotes to single quotes so LLM sees exactly what the scraper will match against
	content = strings.ReplaceAll(content, "\"", "'")

	// Save cleaned/normalized HTML for debugging/reference
	_ = os.WriteFile("debug_input.html", []byte(content), 0644)

	// Truncate
	maxLength := 25000
	if len(content) > maxLength {
		content = content[:maxLength] + "...(truncated)"
	}
	log.Printf("Extracted content size: %d chars", len(content))
	// The previous debug_input.html write is sufficient, no need to write again after truncation.

	// 2. Prepare LLM Request
	ollamaBase := os.Getenv("OLLAMA_BASE_URL")
	if ollamaBase == "" {
		ollamaBase = "http://localhost:11434"
	}
	model := *modelFlag
	if model == "" {
		model = os.Getenv("LLM_MODEL")
	}
	if model == "" {
		model = "qwen2.5-coder:7b"
	}
	endpoint := fmt.Sprintf("%s/api/generate", ollamaBase)

	log.Printf("Using LLM: %s at %s", model, endpoint)

	// 3. Call LLM
	ctx := context.Background()

	systemPrompt := `You are a web scraper config generator.
Goal: Generate a valid JSON object with:
- "typescript_config": (string) Playwright await commands if needed.
- "extractions": (map) key=name, value=regex string. NO lookarounds (?<= or ?=).
Output ONLY the JSON.`

	userPrompt := fmt.Sprintf(`Goal: %s

HTML SNAPSHOT:
%s

TASK:
Generate a JSON configuration that PAIRS the data together into a single extraction field.
- Output ONLY valid JSON. 
- NO summaries, NO extra text.
- Standard Go regex only (NO lookarounds).
- USE EXACTLY TWO CAPTURING GROUPS () per regex to pair the name and the rate.
- THE FIRST group should be the Product Name, the SECOND group should be the Rate.
- IMPORTANT: In your regex, ALWAYS use single quotes (') for HTML attributes (e.g. class='keyword').
- NOTE: The scraper service automatically converts all double quotes (") in the HTML to single quotes (') before matching.
- AVOID matching long brittle class names (e.g. Table__Prod-sc-123). Use keywords or skip them.
- IMPORTANT: Only use keywords for classes that you EXPLICITLY see in the HTML snapshot (e.g. if you see 'Rates__StyledRate', use 'Rate'). Do NOT guess class names.
- HINT: For classes, use class='[^']*KEYWORD[^']*' (this matches even if it's one of many classes).
- HINT: Use [^>]*? to skip unknown attributes in a tag.
- HINT: Use .*? to match across tags within a row.
- HINT: AVOID matching exact closing tags like </td> or </tr> at the end of your regex. 

EXAMPLE FORMAT:
{
  "typescript_config": "",
  "extractions": {
    "products": "<tr[^>]*>.*?<p[^>]*class='[^']*ProductName[^']*'[^>]*>([^<]+)</p>.*?<div[^>]*class='[^']*Rate[^']*'[^>]*>([^<]+)%%"
  }
}
`, *goalFlag, content)

	log.Println("Calling LLM...")
	instructions := []map[string]string{
		{"role": "system", "content": systemPrompt},
	}
	response, err := async_llm.CallSync(ctx, "ollama", endpoint, model, userPrompt, instructions)
	if err != nil {
		log.Fatalf("LLM call failed: %v", err)
	}

	log.Printf("DEBUG: Raw LLM Response:\n%s\n", response)

	// 4. Parse and Output
	cleanedResponse := response

	// If the LLM returned a markdown block anywhere, use ONLY the content of the block
	if idx := strings.Index(cleanedResponse, "```json"); idx != -1 {
		cleanedResponse = cleanedResponse[idx+7:]
		if endIdx := strings.Index(cleanedResponse, "```"); endIdx != -1 {
			cleanedResponse = cleanedResponse[:endIdx]
		}
	} else if idx := strings.Index(cleanedResponse, "```"); idx != -1 {
		cleanedResponse = cleanedResponse[idx+3:]
		if endIdx := strings.Index(cleanedResponse, "```"); endIdx != -1 {
			cleanedResponse = cleanedResponse[:endIdx]
		}
	} else {
		// Just find the first { and last }
		if firstBrace := strings.Index(cleanedResponse, "{"); firstBrace != -1 {
			if lastBrace := strings.LastIndex(cleanedResponse, "}"); lastBrace != -1 {
				if lastBrace > firstBrace {
					cleanedResponse = cleanedResponse[firstBrace : lastBrace+1]
				}
			}
		}
	}

	// Validate
	var config ScrapeConfig
	// Deep cleanup for weird LLM structures
	if err := json.Unmarshal([]byte(cleanedResponse), &config); err != nil {
		log.Printf("Initial parse failed: %v", err)
	}

	// Fallback: If extractions is null, look for other keys at top level
	if len(config.Extractions) == 0 {
		var raw map[string]interface{}
		if err := json.Unmarshal([]byte(cleanedResponse), &raw); err == nil {
			config.Extractions = make(map[string]string)
			for k, v := range raw {
				if k == "typescript_config" {
					if s, ok := v.(string); ok {
						config.TypeScriptConfig = s
					}
					continue
				}
				// If it's a map or list, skip it (unless we want to handle list of extractors)
				if s, ok := v.(string); ok {
					config.Extractions[k] = s
				}
			}
		}
	}

	// Print the generated configuration
	fmt.Println("--- Generated Configuration ---")
	configJSON, _ := json.MarshalIndent(config, "", "  ")
	fmt.Println(string(configJSON))

	// Save to temporary file for the scraper service
	if err := os.WriteFile("/tmp/scrape_config.json", configJSON, 0644); err != nil {
		log.Printf("Failed to write scrape config to /tmp/scrape_config.json: %v", err)
	}
}
