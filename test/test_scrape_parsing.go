package main

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

type ScrapeConfig struct {
	TypeScriptConfig string            `json:"typescript_config"`
	Extractions      map[string]string `json:"extractions"`
}

func parseResilient(response string) (*ScrapeConfig, error) {
	var config ScrapeConfig
	var parseErr error

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

			reRepair := regexp.MustCompile(`\\([^"\\/bfnrtu])`)
			cleanedResponse = reRepair.ReplaceAllString(cleanedResponse, `\\$1`)
			cleanedResponse = strings.ReplaceAll(cleanedResponse, `[\s\S]`, `[\\s\\S]`)

			if err := json.Unmarshal([]byte(cleanedResponse), &rawMap); err == nil {
				var extractions map[string]interface{}
				if ex, ok := rawMap["extractions"].(map[string]interface{}); ok {
					extractions = ex
				} else if ex, ok := rawMap["extraction_instructions"].(map[string]interface{}); ok {
					extractions = ex
				} else {
					extractions = make(map[string]interface{})
					for k, v := range rawMap {
						if k != "typescript_config" && k != "goal" && k != "explanation" && k != "summary" {
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
							if p, ok := obj["pattern"].(string); ok {
								config.Extractions[k] = p
							} else if r, ok := obj["regex"].(string); ok {
								config.Extractions[k] = r
							} else if v, ok := obj["value"].(string); ok {
								config.Extractions[k] = v
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
				}
				parseErr = nil
			} else {
				config.Extractions = make(map[string]string)
				rePairs := regexp.MustCompile(`"([^"]+)"\s*:\s*"([\s\S]*?)"(?:\s*[,}])`)
				pairs := rePairs.FindAllStringSubmatch(cleanedResponse, -1)
				for _, p := range pairs {
					key := p[1]
					val := p[2]
					if key == "typescript_config" {
						config.TypeScriptConfig = val
					} else if key != "extractions" && key != "extraction_instructions" && key != "goal" && key != "explanation" && key != "summary" && key != "regex" && key != "pattern" {
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
						if ik == "regex" || ik == "pattern" || ik == "value" {
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
		return nil, parseErr
	}

	return &config, nil
}

func main() {
	testCases := []struct {
		name     string
		input    string
		expected map[string]string
	}{
		{
			name: "Normal / Standard Format",
			input: `{
				"extractions": {
					"price": "regex1",
					"symbol": "regex2"
				},
				"typescript_config": "await page.goto('...')"
			}`,
			expected: map[string]string{"price": "regex1", "symbol": "regex2"},
		},
		{
			name: "Root Level (Resilient)",
			input: `{
				"price": "regex1",
				"symbol": "regex2"
			}`,
			expected: map[string]string{"price": "regex1", "symbol": "regex2"},
		},
		{
			name: "Nested Regex Objects (Healed)",
			input: `{
				"price": { "regex": "regex1" },
				"symbol": { "pattern": "regex2" }
			}`,
			expected: map[string]string{"price": "regex1", "symbol": "regex2"},
		},
		{
			name:     "Markdown Wrapped",
			input:    "Here is the plan:\n```json\n{\n  \"extractions\": {\"price\": \"regex1\"}\n}\n```",
			expected: map[string]string{"price": "regex1"},
		},
		{
			name: "Broken JSON (Pair Recovery)",
			input: `{
				"price": "regex1",
				"extractions": {
					"symbol": "regex2",
				}
			}`, // Trailing comma might break unmarshal but regex pair recovery should catch it
			expected: map[string]string{"price": "regex1", "symbol": "regex2"},
		},
	}

	for _, tc := range testCases {
		fmt.Printf("🧪 Running test: %s\n", tc.name)
		cfg, err := parseResilient(tc.input)
		if err != nil {
			fmt.Printf("❌ Failed: %v\n", err)
			continue
		}

		success := true
		for k, v := range tc.expected {
			if cfg.Extractions[k] != v {
				fmt.Printf("❌ Mismatch for %s: expected %s, got %s\n", k, v, cfg.Extractions[k])
				success = false
			}
		}

		if success {
			fmt.Printf("✅ Success! Found %d extractions\n", len(cfg.Extractions))
		}
		fmt.Println()
	}
}
