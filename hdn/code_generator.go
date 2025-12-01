package main

import (
	"fmt"
	"log"
	"strings"
	"time"
)

// CodeGenerator handles generating executable code using Ollama
type CodeGenerator struct {
	llmClient   *LLMClient
	codeStorage *CodeStorage
}

// CodeGenerationRequest represents a request to generate code
type CodeGenerationRequest struct {
	TaskName    string            `json:"task_name"`
	Description string            `json:"description"`
	Language    string            `json:"language"`
	Context     map[string]string `json:"context"`
	Tags        []string          `json:"tags"`
	Executable  bool              `json:"executable"`
	Tools       []Tool            `json:"tools,omitempty"`        // Available tools to use
	ToolAPIURL  string            `json:"tool_api_url,omitempty"` // Base URL for tool API
}

// CodeGenerationResponse represents the response from code generation
type CodeGenerationResponse struct {
	Code        *GeneratedCode `json:"code"`
	Success     bool           `json:"success"`
	Error       string         `json:"error,omitempty"`
	Suggestions []string       `json:"suggestions,omitempty"`
}

func NewCodeGenerator(llmClient *LLMClient, codeStorage *CodeStorage) *CodeGenerator {
	return &CodeGenerator{
		llmClient:   llmClient,
		codeStorage: codeStorage,
	}
}

// GenerateCode generates executable code for a given task
func (cg *CodeGenerator) GenerateCode(req *CodeGenerationRequest) (*CodeGenerationResponse, error) {
	// Build a code generation prompt
	prompt := cg.buildCodeGenerationPrompt(req)

	// Debug: log the exact LLM prompt used for code generation (truncated to avoid log flooding)
	if p := strings.TrimSpace(prompt); p != "" {
		max := 4000
		if len(p) > max {
			log.Printf("📝 [CODEGEN] LLM prompt (truncated %d/%d chars):\n%s...", max, len(p), p[:max])
		} else {
			log.Printf("📝 [CODEGEN] LLM prompt (%d chars):\n%s", len(p), p)
		}
	}

	// Call Ollama to generate code
	response, err := cg.llmClient.callLLM(prompt)
	if err != nil {
		return &CodeGenerationResponse{
			Success: false,
			Error:   fmt.Sprintf("Failed to generate code: %v", err),
		}, nil
	}

	// Extract code from response
	code, err := cg.extractCodeFromResponse(response, req.Language)
	if err != nil {
		return &CodeGenerationResponse{
			Success: false,
			Error:   fmt.Sprintf("Failed to extract code: %v", err),
		}, nil
	}

	// Clean up the code to remove test cases and error handling
	code = cg.cleanGeneratedCode(code, req.Language)

	// Create GeneratedCode object
	generatedCode := &GeneratedCode{
		ID:          fmt.Sprintf("code_%d", time.Now().UnixNano()),
		TaskName:    req.TaskName,
		Description: req.Description,
		Language:    req.Language,
		Code:        code,
		Context:     req.Context,
		CreatedAt:   time.Now(),
		Tags:        req.Tags,
		Executable:  req.Executable,
	}

	// Store in Redis
	err = cg.codeStorage.StoreCode(generatedCode)
	if err != nil {
		return &CodeGenerationResponse{
			Success: false,
			Error:   fmt.Sprintf("Failed to store code: %v", err),
		}, nil
	}

	// Generate suggestions for improvement
	suggestions := cg.generateSuggestions(generatedCode)

	return &CodeGenerationResponse{
		Code:        generatedCode,
		Success:     true,
		Suggestions: suggestions,
	}, nil
}

// cleanGeneratedCode removes test cases and error handling from generated code
func (cg *CodeGenerator) cleanGeneratedCode(code, language string) string {
	// Be conservative: only strip surrounding markdown code fences if present.
	trimmed := strings.TrimSpace(code)
	if strings.HasPrefix(trimmed, "```") {
		// Remove the starting fence (optionally with language)
		newlineIdx := strings.Index(trimmed, "\n")
		if newlineIdx != -1 {
			trimmed = trimmed[newlineIdx+1:]
		} else {
			// Single-line fence; return as-is
			return code
		}
	}
	if strings.HasSuffix(trimmed, "```") {
		trimmed = strings.TrimSuffix(trimmed, "```")
	}
	cleaned := strings.TrimSpace(trimmed)

	// For Python, check for and remove unused heavy dependencies
	if language == "python" || language == "py" {
		heavyDeps := []string{"pandas", "numpy", "matplotlib", "scipy", "sklearn", "seaborn", "plotly", "tensorflow", "torch"}
		foundHeavy := []string{}
		lines := strings.Split(cleaned, "\n")
		var filteredLines []string

		log.Printf("🔍 [CODEGEN] Checking %d lines for unused heavy dependencies (code preview: %s)", len(lines), func() string {
			preview := cleaned
			if len(preview) > 200 {
				preview = preview[:200] + "..."
			}
			return preview
		}())

		for _, line := range lines {
			lineTrimmed := strings.TrimSpace(line)
			shouldRemove := false

			// Check if this line imports a heavy dependency
			for _, dep := range heavyDeps {
				// Match: "import pandas", "import pandas as pd", "from pandas import", etc.
				// Use regex-like matching: check if line starts with import/from and contains the dep name
				isImportLine := strings.HasPrefix(lineTrimmed, "import") || strings.HasPrefix(lineTrimmed, "from")
				containsDep := strings.Contains(lineTrimmed, dep)

				if isImportLine && containsDep {
					// Additional check: make sure it's actually importing this specific package
					// Match patterns like: "import pandas", "import pandas as pd", "from pandas import"
					matches := false
					if strings.HasPrefix(lineTrimmed, fmt.Sprintf("import %s", dep)) {
						matches = true
					} else if strings.HasPrefix(lineTrimmed, fmt.Sprintf("from %s", dep)) {
						matches = true
					} else if strings.HasPrefix(lineTrimmed, "import") {
						// Check for "import pandas as pd" or "import pandas"
						parts := strings.Fields(lineTrimmed)
						if len(parts) >= 2 && parts[1] == dep {
							matches = true
						}
					} else if strings.HasPrefix(lineTrimmed, "from") {
						// Check for "from pandas import ..."
						parts := strings.Fields(lineTrimmed)
						if len(parts) >= 2 && parts[1] == dep {
							matches = true
						}
					}

					if matches {
						log.Printf("🔍 [CODEGEN] Found import of %s: %s", dep, lineTrimmed)
						// Check if the dependency is actually used in the code
						used := false
						// Also check for common aliases (pd for pandas, np for numpy, etc.)
						aliases := map[string]string{"pandas": "pd", "numpy": "np", "matplotlib": "plt", "scipy": "sp"}
						alias := ""
						if a, ok := aliases[dep]; ok {
							alias = a
							// Extract alias from "import pandas as pd"
							if strings.Contains(lineTrimmed, " as ") {
								parts := strings.Fields(lineTrimmed)
								for i, part := range parts {
									if part == "as" && i+1 < len(parts) {
										alias = parts[i+1]
										break
									}
								}
							}
						}

						for _, codeLine := range lines {
							codeLineTrimmed := strings.TrimSpace(codeLine)
							// Skip import lines when checking usage
							if strings.HasPrefix(codeLineTrimmed, "import") || strings.HasPrefix(codeLineTrimmed, "from") {
								continue
							}
							// Check for direct usage: pandas., pandas(, etc.
							if strings.Contains(codeLine, dep+".") ||
								strings.Contains(codeLine, fmt.Sprintf("%s(", dep)) ||
								strings.Contains(codeLine, fmt.Sprintf(" %s ", dep)) ||
								strings.Contains(codeLine, fmt.Sprintf("=%s", dep)) ||
								strings.Contains(codeLine, fmt.Sprintf("(%s", dep)) {
								used = true
								preview := codeLineTrimmed
								if len(preview) > 50 {
									preview = preview[:50]
								}
								log.Printf("✅ [CODEGEN] %s is used in: %s", dep, preview)
								break
							}
							// Check for alias usage: pd., np., etc.
							if alias != "" && (strings.Contains(codeLine, alias+".") ||
								strings.Contains(codeLine, fmt.Sprintf("%s(", alias)) ||
								strings.Contains(codeLine, fmt.Sprintf(" %s ", alias)) ||
								strings.Contains(codeLine, fmt.Sprintf("=%s", alias)) ||
								strings.Contains(codeLine, fmt.Sprintf("(%s", alias))) {
								used = true
								preview := codeLineTrimmed
								if len(preview) > 50 {
									preview = preview[:50]
								}
								log.Printf("✅ [CODEGEN] %s is used via alias %s in: %s", dep, alias, preview)
								break
							}
						}
						if !used {
							shouldRemove = true
							foundHeavy = append(foundHeavy, dep)
							log.Printf("⚠️ [CODEGEN] Removing unused heavy dependency import: %s (line: %s)", dep, lineTrimmed)
						}
					}
				}
			}

			if !shouldRemove {
				filteredLines = append(filteredLines, line)
			}
		}

		if len(foundHeavy) > 0 {
			cleaned = strings.Join(filteredLines, "\n")
			log.Printf("⚠️ [CODEGEN] Removed %d unused heavy dependency import(s): %v", len(foundHeavy), foundHeavy)
		} else {
			// Check if any heavy deps are imported (even if used) and warn
			for _, dep := range heavyDeps {
				if strings.Contains(cleaned, fmt.Sprintf("import %s", dep)) ||
					strings.Contains(cleaned, fmt.Sprintf("from %s", dep)) {
					foundHeavy = append(foundHeavy, dep)
				}
			}
			if len(foundHeavy) > 0 {
				log.Printf("⚠️ [CODEGEN] Generated Python code includes heavy dependencies: %v - this will cause slow execution!", foundHeavy)
			}
		}
	}

	return cleaned
}

// buildCodeGenerationPrompt creates a prompt for code generation
func (cg *CodeGenerator) buildCodeGenerationPrompt(req *CodeGenerationRequest) string {
	// Special case for daily_summary - generate a simple placeholder
	if strings.EqualFold(req.TaskName, "daily_summary") {
		return `Generate a simple Python script that prints a placeholder message for daily summary generation.

This is a placeholder task - the actual daily summary will be generated by the system using real data.
Just print a message indicating this is a placeholder.

Code:`
	}

	contextStr := ""
	filePathInfo := ""

	if len(req.Context) > 0 {
		contextStr = "\nContext:\n"
		for k, v := range req.Context {
			contextStr += fmt.Sprintf("- %s: %s\n", k, v)
			// Check if this looks like a data file reference
			if (strings.Contains(strings.ToLower(k), "data") ||
				strings.Contains(strings.ToLower(k), "file") ||
				strings.Contains(strings.ToLower(k), "source") ||
				strings.Contains(strings.ToLower(k), "input")) &&
				strings.Contains(v, ".") && !strings.Contains(v, " ") {
				filePathInfo += fmt.Sprintf("\nIMPORTANT: The file '%s' is available at '/app/data/%s' in the container.\n", v, v)
			}
		}
	}

	tagsStr := ""
	if len(req.Tags) > 0 {
		tagsStr = fmt.Sprintf("\nTags: %s\n", strings.Join(req.Tags, ", "))
	}

	// Build tool information section
	toolsSection := ""
	if len(req.Tools) > 0 && req.ToolAPIURL != "" {
		toolsSection = "\n\n🔧 AVAILABLE TOOLS - USE THESE INSTEAD OF DUMMY DATA:\n"
		toolsSection += "🚨 CRITICAL: If the task requires functionality that matches these tools, you MUST use the tools instead of generating dummy implementations.\n"
		toolsSection += "🚨 CRITICAL: For web scraping tasks, you MUST use tool_html_scraper - NEVER use http.Get() or requests.get()!\n"
		toolsSection += fmt.Sprintf("Tool API Base URL: %s\n\n", req.ToolAPIURL)
		toolsSection += "⚠️ IMPORTANT: 'tool_html_scraper' is NOT a Python function or module!\n"
		toolsSection += "⚠️ You MUST make HTTP POST requests to the tool API endpoint shown below.\n"
		toolsSection += "⚠️ DO NOT try to import or call 'tool_html_scraper' as a function - it will cause 'name not defined' errors!\n\n"

		for _, tool := range req.Tools {
			toolsSection += fmt.Sprintf("Tool: %s (%s)\n", tool.ID, tool.Name)
			toolsSection += fmt.Sprintf("Description: %s\n", tool.Description)
			if len(tool.InputSchema) > 0 {
				toolsSection += "Parameters:\n"
				for param, paramType := range tool.InputSchema {
					toolsSection += fmt.Sprintf("  - %s (%s)\n", param, paramType)
				}
			}
			toolsSection += fmt.Sprintf("Call: POST %s/api/v1/tools/%s/invoke\n", req.ToolAPIURL, tool.ID)
			toolsSection += "Request Body: JSON object with parameters\n\n"
		}

		// Add examples for common languages
		toolsSection += "EXAMPLES:\n\n"

		// Python example - MUST use tool_html_scraper for web scraping
		toolsSection += "Python - Call HTML Scraper (USE THIS FOR WEB SCRAPING!):\n"
		toolsSection += "```python\n"
		toolsSection += "import requests\n"
		toolsSection += "import json\n"
		toolsSection += "import os\n"
		toolsSection += "def main():\n"
		toolsSection += "    # 🚨 CRITICAL: tool_html_scraper is NOT a Python function - you MUST make HTTP POST requests!\n"
		toolsSection += "    # Get the tool API URL from environment or use the hardcoded value below\n"
		toolsSection += fmt.Sprintf("    TOOL_API_URL = os.getenv('TOOL_API_URL', '%s')\n", req.ToolAPIURL)
		toolsSection += "    url = 'https://www.bbc.com/news/technology'\n"
		toolsSection += "    # Make HTTP POST request to the tool API - DO NOT call tool_html_scraper() as a function!\n"
		toolsSection += "    response = requests.post(f'{TOOL_API_URL}/api/v1/tools/tool_html_scraper/invoke',\n"
		toolsSection += "        json={'url': url})\n"
		toolsSection += "    data = response.json()\n"
		toolsSection += "    # Extract items array - each item has 'tag', 'text', 'attributes'\n"
		toolsSection += "    items = data.get('items', [])\n"
		toolsSection += "    for item in items:\n"
		toolsSection += "        tag = item.get('tag', '')\n"
		toolsSection += "        text = item.get('text', '')\n"
		toolsSection += "        # Extract headlines (h1/h2/h3 tags with substantial text)\n"
		toolsSection += "        if tag in ['h1', 'h2', 'h3'] and len(text) > 20:\n"
		toolsSection += "            print(text)\n"
		toolsSection += "if __name__ == '__main__':\n"
		toolsSection += "    main()\n"
		toolsSection += "```\n\n"

		// Go example - MUST use tool_html_scraper for web scraping
		toolsSection += "Go - Call HTML Scraper (USE THIS FOR WEB SCRAPING!):\n"
		toolsSection += "```go\n"
		toolsSection += "package main\n"
		toolsSection += "import (\n"
		toolsSection += "    \"bytes\"\n"
		toolsSection += "    \"encoding/json\"\n"
		toolsSection += "    \"fmt\"\n"
		toolsSection += "    \"io\"\n"
		toolsSection += "    \"net/http\"\n"
		toolsSection += "    \"os\"\n"
		toolsSection += "    \"strings\"\n"
		toolsSection += ")\n"
		toolsSection += "func main() {\n"
		toolsSection += "    // 🚨 CRITICAL: tool_html_scraper is NOT a Go function - you MUST make HTTP POST requests!\n"
		toolsSection += "    // Get the tool API URL from environment or use the hardcoded value below\n"
		toolsSection += fmt.Sprintf("    toolAPIURL := os.Getenv(\"TOOL_API_URL\")\n")
		toolsSection += fmt.Sprintf("    if toolAPIURL == \"\" {\n")
		toolsSection += fmt.Sprintf("        toolAPIURL = \"%s\"\n", req.ToolAPIURL)
		toolsSection += "    }\n"
		toolsSection += "    url := \"https://www.bbc.com/news/technology\"\n"
		toolsSection += "    // Make HTTP POST request to the tool API - DO NOT call tool_html_scraper() as a function!\n"
		toolsSection += "    jsonData, _ := json.Marshal(map[string]string{\"url\": url})\n"
		toolsSection += "    resp, _ := http.Post(toolAPIURL+\"/api/v1/tools/tool_html_scraper/invoke\",\n"
		toolsSection += "        \"application/json\", bytes.NewBuffer(jsonData))\n"
		toolsSection += "    defer resp.Body.Close()\n"
		toolsSection += "    body, _ := io.ReadAll(resp.Body)\n"
		toolsSection += "    var result map[string]interface{}\n"
		toolsSection += "    json.Unmarshal(body, &result)\n"
		toolsSection += "    // Extract items array - each item has 'tag', 'text', 'attributes'\n"
		toolsSection += "    items, _ := result[\"items\"].([]interface{})\n"
		toolsSection += "    for _, item := range items {\n"
		toolsSection += "        if itemMap, ok := item.(map[string]interface{}); ok {\n"
		toolsSection += "            text, _ := itemMap[\"text\"].(string)\n"
		toolsSection += "            tag, _ := itemMap[\"tag\"].(string)\n"
		toolsSection += "            // Extract headlines (h1/h2/h3 tags with substantial text)\n"
		toolsSection += "            if (tag == \"h1\" || tag == \"h2\" || tag == \"h3\") && len(text) > 20 {\n"
		toolsSection += "                fmt.Println(text)\n"
		toolsSection += "            }\n"
		toolsSection += "        }\n"
		toolsSection += "    }\n"
		toolsSection += "}\n"
		toolsSection += "```\n\n"
	}

	return fmt.Sprintf(`🚫 CRITICAL RESTRICTION - MUST FOLLOW:
- NEVER use Docker commands (docker run, docker build, docker exec, etc.) - Docker is NOT available
- NEVER use subprocess.run with docker commands - this will cause FileNotFoundError
- NEVER use os.system with docker commands - this will fail
- You are already running inside a container, do NOT try to create more containers

🚨🚨🚨 CRITICAL LANGUAGE REQUIREMENT - THIS IS THE MOST IMPORTANT RULE 🚨🚨🚨:
- You MUST generate %s code ONLY - do NOT generate code in any other language!
- You MUST NOT include code from other languages in your response!
- You MUST NOT show examples in other languages!
- You MUST NOT include "go" or "package main" or "func main()" if generating Python code!
- You MUST NOT include "import pandas" or "def main()" if generating Go code!
- If the task mentions a filename with an extension (.py, .go, .js, etc.), the extension determines the language
- The filename extension MUST match the language you generate:
  * .py = Python code ONLY - NO Go code, NO JavaScript code, NO other languages!
  * .go = Go code ONLY - NO Python code, NO JavaScript code, NO other languages!
  * .js = JavaScript code ONLY - NO Python code, NO Go code, NO other languages!
  * .java = Java code ONLY - NO other languages!
- DO NOT generate Python code when asked for .go files!
- DO NOT generate Go code when asked for .py files!
- DO NOT mix languages - if you generate Python, generate ONLY Python from start to finish!
- DO NOT mix languages - if you generate Go, generate ONLY Go from start to finish!
- Your response should contain ONLY %s code - nothing else!
- If you include code from another language, the system will FAIL!

🚨 CRITICAL FOR GO CODE - CODE MUST COMPILE:
- For Go: ALWAYS start with package main and import statements
- For Go: You MUST include ALL required imports - if you use json.Unmarshal, you MUST import "encoding/json"
- For Go: You MUST include ALL required imports - if you use io.ReadAll, you MUST import "io"
- For Go: You MUST include ALL required imports - if you use fmt.Println, you MUST import "fmt"
- For Go: You MUST include ALL required imports - if you use os.Stdin, you MUST import "os"
- For Go: The code MUST compile with "go build" - missing imports will cause compilation errors!
- For Go: Use proper Go syntax - NO nested function calls like strings.ReplaceAll(strings.ReplaceAll(...))
- For Go: Use standard library functions correctly: json.Unmarshal, io.ReadAll, fmt.Println
- For Go: Keep code simple and readable - avoid deeply nested calls
- For Go: If reading JSON, use: jsonBytes, _ := io.ReadAll(os.Stdin) then json.Unmarshal(jsonBytes, &data)
- For Go: Before returning code, verify ALL functions used have their corresponding imports included!

🚨🚨🚨 YOU ARE GENERATING %s CODE - NOTHING ELSE! 🚨🚨🚨
Generate clean, executable %s code for this task.
Your response must contain ONLY %s code - no other languages, no examples in other languages, no mixed code!

Task: %s
Description: %s%s%s%s

UNIQUE TASK ID: %s_%d

Generate ONLY the core functionality:
1. Define the main function(s) needed
2. Add basic comments
3. Include necessary imports (prefer standard library when possible)
4. The main execution should run the core task and PRINT the result

IMPORTANT: 
- 🚨 CRITICAL: The program MUST compile and run without errors!
- For Go: The code MUST compile with "go build" - ALL imports MUST be included (encoding/json for json.Unmarshal, io for io.ReadAll, fmt for fmt.Println, os for os.Stdin, etc.)
- For Go: If you use ANY function from a package, you MUST import that package - missing imports will cause compilation failures!
- 🚨 CRITICAL FOR GO: You MUST NOT include any unused imports! Go compiler treats unused imports as ERRORS, not warnings!
- 🚨 CRITICAL FOR GO: Only import packages you actually USE in the code - do NOT import "strconv", "strings", "os", etc. unless you actually use functions from those packages!
- The program must compile cleanly with the language's standard compiler with no unused variables or imports
- Use ONLY the standard library unless explicitly requested otherwise
- Use ASCII identifiers only (no non-ASCII names)
- If tools are available above, you MUST use them instead of dummy data
- Do NOT create hardcoded dummy data when tools can fetch real data
- 🚨 CRITICAL: For web scraping tasks, you MUST use tool_html_scraper - NEVER use http.Get() or direct HTTP calls!
- 🚨 CRITICAL: If the task says "scrape" or "extract" from a website, you MUST use tool_html_scraper, NOT http.Get()
- 🚨 CRITICAL: If the task says "summarize trends", you must BOTH scrape AND analyze/filter/categorize the results
- 🚨 CRITICAL: 'tool_html_scraper' is NOT a function or module - you MUST make HTTP POST requests to the tool API endpoint!
- 🚨 CRITICAL: DO NOT try to import or call 'tool_html_scraper' as a function - use requests.post() or http.Post() instead!
- For file operations: use tool_file_read or tool_file_write if available
- Only perform direct network calls if no tool is available (but tool_html_scraper IS available for web scraping!)
- The code must include a print statement to output the result
  * For Go: use fmt.Print() or fmt.Println() (NOT the built-in print() function). Ensure you import "fmt" and "io" packages if using io.ReadAll()
  * For Python: use print()
  * For JavaScript: use console.log()
- Use the correct file paths for any data files (see IMPORTANT notes above)
- For mathematical tasks, create appropriate functions and print the results
- 🚨 CRITICAL FOR MATHEMATICAL TASKS: If the context provides parameters (like matrix1, matrix2, count, number, etc.), you MUST read them from the context/environment variables - DO NOT hardcode values!
- 🚨 CRITICAL FOR MATRIX OPERATIONS: If context contains matrix1, matrix2, or similar parameters, you MUST parse them from environment variables or context - DO NOT hardcode matrix values!
- 🚨 CRITICAL FOR GO MATRIX OPERATIONS: Read matrices from environment variables (os.Getenv("matrix1"), os.Getenv("matrix2")) and parse the JSON/string format - DO NOT hardcode matrix values!
- 🚨 CRITICAL FOR PYTHON MATRIX OPERATIONS: Read matrices from environment variables (os.getenv("matrix1"), os.getenv("matrix2")) and parse the JSON/string format - DO NOT hardcode matrix values!
- For data processing tasks, create functions that process the data and print the results
- Don't create test functions or validation functions - create the actual functionality
- 🚨 CRITICAL: MINIMIZE external dependencies - use standard library modules when possible!
- 🚨 CRITICAL FOR PYTHON: DO NOT import pandas, numpy, matplotlib, scipy, sklearn, or any other heavy data science libraries unless the task EXPLICITLY requires data analysis, machine learning, or complex numerical computations!
- 🚨 CRITICAL FOR PYTHON: For simple tasks like generating prime numbers, calculating statistics, or basic math - use ONLY the standard library (math, random, statistics, etc.)!
- 🚨 CRITICAL FOR PYTHON: Installing pandas/numpy takes MINUTES and is SLOW - only use if you absolutely need DataFrame operations or advanced numerical computing!
- 🚨 CRITICAL FOR PYTHON: For prime numbers, use simple loops and the math module - DO NOT use pandas!
- 🚨 CRITICAL FOR PYTHON: For basic statistics, use the statistics module - DO NOT use pandas or numpy!
- 🚨 CRITICAL FOR PYTHON: For matrix operations, you can use simple lists - DO NOT use numpy unless the task explicitly requires it!
- NEVER use input() or any user input functions - use the context parameters directly
- NEVER ask for user input - the code should run automatically with the given context
- NEVER use Docker commands (docker run, docker build, etc.) - Docker is not available in the execution environment

DO NOT include:
- Test cases
- Error handling with print statements
- Example usage with different inputs
- Validation code that runs automatically
- Any code that prints error messages
- Functions that test or validate - create the actual functionality
- 🚨 Heavy data science libraries (pandas, numpy, matplotlib, scipy, sklearn) unless the task EXPLICITLY requires them!
- 🚨 Unnecessary imports that will cause slow package installation!
- input() or any user input functions
- Any code that waits for user interaction
- Docker commands (docker run, docker build, etc.) - Docker is not available
- Any comments mentioning Docker, containers, or containerization
- Any references to Docker in comments or strings

The code should run once and print only the expected result. Ensure it compiles without errors before returning.

🚨🚨🚨 FINAL REMINDER: Generate ONLY %s code - NO other languages! 🚨🚨🚨
If you include any code from another language (Python when asked for Go, Go when asked for Python, etc.), the system will FAIL!

🚨 CRITICAL: You MUST return CODE, NOT JSON! Do NOT return task planning JSON!
🚨 CRITICAL: Your response MUST start with a code block marker (three backticks followed by the language)
🚨 CRITICAL: Do NOT return JSON like {"task": "...", "subtasks": [...]} - return CODE!

%s

Code (ONLY %s code wrapped in markdown code blocks, nothing else):`, req.Language, req.TaskName, req.Description, contextStr, filePathInfo, tagsStr, req.TaskName, time.Now().UnixNano(), toolsSection, req.Language)
}

// extractCodeFromResponse extracts code from the LLM response
func (cg *CodeGenerator) extractCodeFromResponse(response, language string) (string, error) {
	// Look for code blocks in the response
	codeBlockStart := fmt.Sprintf("```%s", language)
	codeBlockEnd := "```"

	startIdx := strings.Index(response, codeBlockStart)
	if startIdx == -1 {
		// Try generic code block
		startIdx = strings.Index(response, "```")
		if startIdx == -1 {
			// Last resort: check if the entire response is code (no markdown)
			trimmed := strings.TrimSpace(response)
			// If response looks like code (has imports, functions, etc.), use it directly
			if strings.Contains(trimmed, "package ") || strings.Contains(trimmed, "import ") ||
				strings.Contains(trimmed, "def ") || strings.Contains(trimmed, "func ") ||
				strings.Contains(trimmed, "class ") {
				log.Printf("⚠️ [CODEGEN] No code block found, but response looks like code - using entire response")
				return trimmed, nil
			}
			log.Printf("❌ [CODEGEN] No code block found in response (first 500 chars): %s",
				func() string {
					if len(response) > 500 {
						return response[:500]
					}
					return response
				}())
			return "", fmt.Errorf("no code block found in response")
		}
		// Skip the ```
		startIdx += 3
	} else {
		// Skip the ```language
		startIdx += len(codeBlockStart)
	}

	// Find the end of the code block
	endIdx := strings.Index(response[startIdx:], codeBlockEnd)
	if endIdx == -1 {
		// Try to extract everything after the code block start as code
		code := strings.TrimSpace(response[startIdx:])
		if code != "" {
			log.Printf("⚠️ [CODEGEN] No closing code block found, but extracted code from start marker (first 200 chars): %s",
				func() string {
					if len(code) > 200 {
						return code[:200]
					}
					return code
				}())
			return code, nil
		}
		log.Printf("❌ [CODEGEN] No closing code block found (first 500 chars after start): %s",
			func() string {
				if len(response[startIdx:]) > 500 {
					return response[startIdx : startIdx+500]
				}
				return response[startIdx:]
			}())
		return "", fmt.Errorf("no closing code block found")
	}

	// Extract the code
	code := strings.TrimSpace(response[startIdx : startIdx+endIdx])

	if code == "" {
		return "", fmt.Errorf("extracted code is empty")
	}

	// Filter out code from wrong language - if we asked for Python, remove Go code blocks
	if language == "python" || language == "py" {
		// Remove Go code blocks (package main, func main, etc.)
		lines := strings.Split(code, "\n")
		var filteredLines []string
		inGoBlock := false
		for _, line := range lines {
			lineTrimmed := strings.TrimSpace(line)
			// Detect Go code blocks
			if strings.HasPrefix(lineTrimmed, "package ") ||
				strings.HasPrefix(lineTrimmed, "func main()") ||
				(strings.Contains(lineTrimmed, "import (") && strings.Contains(code, "package main")) {
				inGoBlock = true
				continue
			}
			// If we're in a Go block, skip until we see Python code
			if inGoBlock {
				// Check if this looks like Python code
				if (strings.HasPrefix(lineTrimmed, "import ") && !strings.Contains(lineTrimmed, "(")) ||
					strings.HasPrefix(lineTrimmed, "def ") ||
					strings.HasPrefix(lineTrimmed, "class ") ||
					strings.HasPrefix(lineTrimmed, "#") {
					inGoBlock = false
					filteredLines = append(filteredLines, line)
				}
				continue
			}
			filteredLines = append(filteredLines, line)
		}
		if len(filteredLines) > 0 {
			code = strings.Join(filteredLines, "\n")
			log.Printf("⚠️ [CODEGEN] Filtered out Go code from Python response")
		}
	} else if language == "go" {
		// Remove Python code blocks (import pandas, def main, etc.)
		lines := strings.Split(code, "\n")
		var filteredLines []string
		inPythonBlock := false
		for _, line := range lines {
			lineTrimmed := strings.TrimSpace(line)
			// Detect Python code blocks
			if (strings.HasPrefix(lineTrimmed, "import ") && !strings.Contains(lineTrimmed, "(")) ||
				strings.HasPrefix(lineTrimmed, "def ") ||
				strings.HasPrefix(lineTrimmed, "class ") {
				// Check if this is actually Go import statement
				if strings.HasPrefix(lineTrimmed, "import (") || strings.HasPrefix(lineTrimmed, "package ") {
					inPythonBlock = false
					filteredLines = append(filteredLines, line)
					continue
				}
				inPythonBlock = true
				continue
			}
			// If we're in a Python block, skip until we see Go code
			if inPythonBlock {
				// Check if this looks like Go code
				if strings.HasPrefix(lineTrimmed, "package ") ||
					strings.HasPrefix(lineTrimmed, "func ") ||
					strings.HasPrefix(lineTrimmed, "import (") {
					inPythonBlock = false
					filteredLines = append(filteredLines, line)
				}
				continue
			}
			filteredLines = append(filteredLines, line)
		}
		if len(filteredLines) > 0 {
			code = strings.Join(filteredLines, "\n")
			log.Printf("⚠️ [CODEGEN] Filtered out Python code from Go response")
		}
	}

	return code, nil
}

// generateSuggestions creates suggestions for improving the generated code
func (cg *CodeGenerator) generateSuggestions(code *GeneratedCode) []string {
	var suggestions []string

	// Language-specific suggestions
	switch code.Language {
	case "go":
		suggestions = append(suggestions, "Consider adding unit tests")
		suggestions = append(suggestions, "Add proper error handling with custom error types")
		suggestions = append(suggestions, "Consider using interfaces for better testability")
	case "python":
		suggestions = append(suggestions, "Add type hints for better code clarity")
		suggestions = append(suggestions, "Consider using dataclasses or pydantic for data structures")
		suggestions = append(suggestions, "Add docstrings following PEP 257")
	case "javascript", "typescript":
		suggestions = append(suggestions, "Add JSDoc comments for better documentation")
		suggestions = append(suggestions, "Consider using TypeScript for better type safety")
		suggestions = append(suggestions, "Add proper error handling with try-catch blocks")
	}

	// General suggestions
	suggestions = append(suggestions, "Add logging for debugging and monitoring")
	suggestions = append(suggestions, "Consider adding configuration management")
	suggestions = append(suggestions, "Add input validation and sanitization")

	return suggestions
}

// SearchCode searches for previously generated code
func (cg *CodeGenerator) SearchCode(query string, language string, tags []string) ([]CodeSearchResult, error) {
	return cg.codeStorage.SearchCode(query, language, tags)
}

// GetCode retrieves code by ID
func (cg *CodeGenerator) GetCode(id string) (*GeneratedCode, error) {
	return cg.codeStorage.GetCode(id)
}

// ListAllCode lists all generated code
func (cg *CodeGenerator) ListAllCode() ([]*GeneratedCode, error) {
	return cg.codeStorage.ListAllCode()
}

// DeleteCode removes code by ID
func (cg *CodeGenerator) DeleteCode(id string) error {
	return cg.codeStorage.DeleteCode(id)
}

// GenerateCodeFromTask generates code based on an existing HTN task
func (cg *CodeGenerator) GenerateCodeFromTask(taskName, description string, context map[string]string) (*CodeGenerationResponse, error) {
	// Determine language from context or default to Go
	language := "go"
	if lang, exists := context["language"]; exists {
		language = lang
	}

	// Extract tags from context
	var tags []string
	if taskTags, exists := context["tags"]; exists {
		tags = strings.Split(taskTags, ",")
		for i, tag := range tags {
			tags[i] = strings.TrimSpace(tag)
		}
	}

	req := &CodeGenerationRequest{
		TaskName:    taskName,
		Description: description,
		Language:    language,
		Context:     context,
		Tags:        tags,
		Executable:  true,
	}

	return cg.GenerateCode(req)
}
