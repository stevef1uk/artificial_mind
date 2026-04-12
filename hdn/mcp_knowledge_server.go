package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"

	mempkg "hdn/memory"
	"hdn/interpreter"

	"github.com/redis/go-redis/v9"
)

type MCPKnowledgeServer struct {
	domainKnowledge  mempkg.DomainKnowledgeClient
	vectorDB         mempkg.VectorDBAdapter
	redis            *redis.Client
	hdnURL           string                // For proxying queries
	skillRegistry    *DynamicSkillRegistry // Dynamic skills from configuration
	llmClient        *LLMClient            // LLM client for prompt-driven browser automation
	fileStorage      *FileStorage          // For storing artifacts (screenshots, etc) in Redis
	latestScreenshot []byte                // In-memory latest screenshot for k3s (no shared volume)
	screenshotMu     sync.RWMutex          // Protects latestScreenshot
	toolMetrics      *ToolMetricsManager   // For logging tool usage
}

// MCPKnowledgeRequest represents an MCP JSON-RPC request for knowledge server
type MCPKnowledgeRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// MCPKnowledgeResponse represents an MCP JSON-RPC response for knowledge server
type MCPKnowledgeResponse struct {
	JSONRPC string             `json:"jsonrpc"`
	ID      interface{}        `json:"id"`
	Result  interface{}        `json:"result,omitempty"`
	Error   *MCPKnowledgeError `json:"error,omitempty"`
}

// MCPKnowledgeError represents an MCP error for knowledge server
type MCPKnowledgeError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// MCPKnowledgeTool represents an MCP tool definition for knowledge server
type MCPKnowledgeTool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"inputSchema"`
}

// NewMCPKnowledgeServer creates a new MCP knowledge server
func NewMCPKnowledgeServer(domainKnowledge mempkg.DomainKnowledgeClient, vectorDB mempkg.VectorDBAdapter, redis *redis.Client, hdnURL string, llmClient *LLMClient, fileStorage *FileStorage, toolMetrics *ToolMetricsManager) *MCPKnowledgeServer {
	server := &MCPKnowledgeServer{
		domainKnowledge: domainKnowledge,
		vectorDB:        vectorDB,
		redis:           redis,
		hdnURL:          hdnURL,
		skillRegistry:   NewDynamicSkillRegistry(),
		llmClient:       llmClient,
		fileStorage:     fileStorage,
		toolMetrics:     toolMetrics,
	}

	configPath := os.Getenv("N8N_MCP_SKILLS_CONFIG")
	if configPath == "" {
		configPath = "n8n_mcp_skills.yaml"
	}
	log.Printf("🔍 [MCP-KNOWLEDGE] Attempting to load skills from config: %s", configPath)
	if err := server.skillRegistry.LoadSkillsFromConfig(configPath); err != nil {
		log.Printf("⚠️ [MCP-KNOWLEDGE] Failed to load skills from configuration: %v (continuing with hardcoded tools)", err)
	} else {
		log.Printf("✅ [MCP-KNOWLEDGE] Successfully loaded skills from configuration")

		// Register skills prompt hints with the global interpreter registry
		hints := server.skillRegistry.GetAllPromptHints()
		for toolID, hintsConfig := range hints {
			// Convert Skill prompt hints to interpreter.PromptHintsConfig
			interpreterHints := &interpreter.PromptHintsConfig{
				Keywords:      hintsConfig.Keywords,
				PromptText:    hintsConfig.PromptText,
				ForceToolCall: hintsConfig.ForceToolCall,
				AlwaysInclude: hintsConfig.AlwaysInclude,
				RejectText:    hintsConfig.RejectText,
			}
			interpreter.SetPromptHints(toolID, interpreterHints)
			log.Printf("✅ [MCP-KNOWLEDGE] Registered prompt hints for dynamic skill: %s", toolID)
		}
	}

	return server
}

// PlaywrightOperation represents a parsed operation from TypeScript
type PlaywrightOperation struct {
	Type     string // "goto", "click", "fill", "getByRole", "getByText", "locator", "wait", "extract"
	Selector string // CSS selector or locator
	Value    string // For fill operations
	Role     string // For getByRole
	RoleName string // Name for getByRole
	Text     string // For getByText
	Timeout  int    // Timeout in seconds
}

func runCommandWithLiveOutput(ctx context.Context, cmd *exec.Cmd, logPrefix string) (string, string, error) {
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return "", "", fmt.Errorf("failed to create stdout pipe: %w", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return "", "", fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return "", "", err
	}

	var stdoutBuf bytes.Buffer
	var stderrBuf bytes.Buffer
	var wg sync.WaitGroup

	readPipe := func(r io.Reader, buf *bytes.Buffer, logLines bool) {
		defer wg.Done()
		scanner := bufio.NewScanner(r)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			buf.WriteString(line)
			buf.WriteByte('\n')
			if logLines {
				log.Printf("%s %s", logPrefix, line)
			}
		}
		if err := scanner.Err(); err != nil {
			log.Printf("%s scanner error: %v", logPrefix, err)
		}
	}

	wg.Add(2)
	go readPipe(stdoutPipe, &stdoutBuf, false)
	go readPipe(stderrPipe, &stderrBuf, true)

	wg.Wait()
	waitErr := cmd.Wait()

	return stdoutBuf.String(), stderrBuf.String(), waitErr
}

// RegisterMCPKnowledgeServerRoutes registers MCP knowledge server routes
func (s *APIServer) RegisterMCPKnowledgeServerRoutes() {
	if s.mcpKnowledgeServer == nil {
		hdnURL := os.Getenv("HDN_URL")
		if hdnURL == "" {
			hdnURL = "http://localhost:8081"
		}
		s.mcpKnowledgeServer = NewMCPKnowledgeServer(
			s.domainKnowledge,
			s.vectorDB,
			s.redis,
			hdnURL,
			s.llmClient,
			s.fileStorage,
			s.toolMetrics,
		)

	}

	s.router.HandleFunc("/mcp", s.mcpKnowledgeServer.HandleRequest).Methods("POST", "GET", "OPTIONS")
	s.router.HandleFunc("/api/v1/mcp", s.mcpKnowledgeServer.HandleRequest).Methods("POST", "GET", "OPTIONS")

	s.router.HandleFunc("/sse", s.mcpKnowledgeServer.HandleRequest).Methods("GET", "OPTIONS")
	s.router.HandleFunc("/api/v1/sse", s.mcpKnowledgeServer.HandleRequest).Methods("GET", "OPTIONS")

	s.router.HandleFunc("/api/v1/scrape/screenshot", s.mcpKnowledgeServer.HandleScreenshot).Methods("GET")

	log.Printf("✅ [MCP-KNOWLEDGE] MCP knowledge server registered at /mcp, /sse and /api/v1/mcp")
}

// isSelfConnectionHDN checks if the endpoint is pointing to the same server (self-connection)
// This detects Kubernetes service DNS patterns and localhost patterns
func isSelfConnectionHDN(endpoint string) bool {
	lower := strings.ToLower(endpoint)

	if strings.Contains(lower, ".svc.cluster.local") {

		if strings.Contains(lower, "hdn") || strings.Contains(lower, "hdn-server") {
			return true
		}
	}

	if strings.Contains(lower, "localhost") || strings.Contains(lower, "127.0.0.1") {
		return true
	}

	return false
}

// executeSmartScrape performs an AI-powered scrape by first fetching and then planning
// stripScrapeResultFields removes large/binary fields from a results map before serialization
// so that the NLG receives clean, readable content instead of raw HTML.
func stripScrapeResultFields(m map[string]interface{}) {
	for _, key := range []string{"cleaned_html", "raw_html", "screenshot", "cookies"} {
		delete(m, key)
	}
}

// stripMarkdownFormatting removes markdown formatting characters (**, *, #, backticks, etc.)
// so that extracted content reads cleanly when spoken aloud by TTS on remote devices.
var (
	reMarkdownBold       = regexp.MustCompile(`\*\*(.+?)\*\*`)
	reMarkdownUnderBold  = regexp.MustCompile(`__(.+?)__`)
	reMarkdownHeading    = regexp.MustCompile(`(?m)^#{1,6}\s+`)
	reMarkdownInlineCode = regexp.MustCompile("`([^`]+)`")
	reMarkdownCodeBlock  = regexp.MustCompile("(?s)```(?:[a-z]*\\n)?(.*?)\\n?```")
	reMarkdownBullet     = regexp.MustCompile(`(?m)^\s*\*\s+`)
)

func stripMarkdownFormatting(text string) string {

	text = reMarkdownCodeBlock.ReplaceAllString(text, "$1")

	text = reMarkdownInlineCode.ReplaceAllString(text, "$1")

	text = reMarkdownBold.ReplaceAllString(text, "$1")
	text = reMarkdownUnderBold.ReplaceAllString(text, "$1")
	text = reMarkdownHeading.ReplaceAllString(text, "")
	text = reMarkdownBullet.ReplaceAllString(text, "- ")

	text = strings.ReplaceAll(text, "```", "")
	text = strings.ReplaceAll(text, "`", "")

	return strings.TrimSpace(text)
}

type ScrapeConfig struct {
	TypeScriptConfig string            `json:"typescript_config"`
	Extractions      map[string]string `json:"extractions"`
}

// cleanHTMLForPlanning simplifies HTML to make it more digestible for the planning LLM
// convertStepsToJS takes a JSON object (often generated by larger models) and converts it to Playwright JS
// convertStepsToJS takes a JSON object (often generated by larger models) and converts it to Playwright JS
func convertStepsToJS(tsObj map[string]interface{}) string {
	var js string

	// Case 1: Structured array under "steps", "actions", or "interaction_logic"
	var steps []interface{}
	var ok bool

	if val, found := tsObj["steps"]; found {
		steps, ok = val.([]interface{})
	}
	if !ok {
		if val, found := tsObj["actions"]; found {
			steps, ok = val.([]interface{})
		}
	}
	if !ok {
		if val, found := tsObj["interaction_logic"]; found {
			steps, ok = val.([]interface{})
		}
	}
	if !ok {
		if val, found := tsObj["interactions"]; found {
			steps, ok = val.([]interface{})
		}
	}

	if ok && len(steps) > 0 {
		log.Printf("🩹 [MCP-SMART-SCRAPE] Converting structured steps array to JS...")
		for _, s := range steps {

			if rawCmd, ok := s.(string); ok && rawCmd != "" {
				js += rawCmd + "\n"
				continue
			}

			step, ok := s.(map[string]interface{})
			if !ok {
				continue
			}

			if cmd, ok := step["command"].(string); ok && cmd != "" {
				js += cmd + "\n"
				continue
			}
			if cmd, ok := step["code"].(string); ok && cmd != "" {
				js += cmd + "\n"
				continue
			}
			if cmd, ok := step["js"].(string); ok && cmd != "" {
				js += cmd + "\n"
				continue
			}
			if cmd, ok := step["javascript"].(string); ok && cmd != "" {
				js += cmd + "\n"
				continue
			}

			action, _ := step["action"].(string)
			if action == "" {
				action, _ = step["type"].(string)
			}
			selector, _ := step["selector"].(string)
			if selector == "" {
				selector, _ = step["target"].(string)
			}
			value, _ := step["value"].(string)

			action = strings.ToLower(strings.TrimSpace(action))

			switch action {
			case "fill", "type", "fill_input":
				js += fmt.Sprintf("await page.locator('%s').fill('%s');\n", selector, value)
			case "click":
				js += fmt.Sprintf("await page.locator('%s').click();\n", selector)
			case "selectoption", "select_option":
				js += fmt.Sprintf("await page.locator('%s').selectOption('%s');\n", selector, value)
			case "wait":
				js += "await page.waitForTimeout(1000);\n"
			case "waitfortimeout", "wait_fortimeout":
				js += fmt.Sprintf("await page.waitForTimeout(%v);\n", value)
			}
		}
	} else {

		log.Printf("🩹 [MCP-SMART-SCRAPE] Non-array typescript_config detected, attempting recursive extraction...")
		js = extractStringsFromObject(tsObj)
	}
	return js
}

// extractStringsFromObject recursively finds all string values in an object and joins them
// findJSInObject recursively searches for anything that looks like a JS plan
func findJSInObject(obj map[string]interface{}) string {
	for k, v := range obj {
		if isJSKey(k) {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
			if nest, ok := v.(map[string]interface{}); ok {
				return convertStepsToJS(nest)
			}
			if arr, ok := v.([]interface{}); ok {
				return convertStepsToJS(map[string]interface{}{"steps": arr})
			}
		}
		if nest, ok := v.(map[string]interface{}); ok {
			if found := findJSInObject(nest); found != "" {
				return found
			}
		}
	}
	return ""
}

func isJSKey(k string) bool {
	k = strings.ToLower(k)
	return k == "typescript_config" || k == "steps" || k == "actions" || k == "interaction_logic" || k == "interactions" || k == "calculation_logic" || k == "navigation"
}

func extractStringsFromObject(obj map[string]interface{}) string {
	var js string
	for k, v := range obj {
		kLower := strings.ToLower(k)

		if kLower == "goal" || kLower == "explanation" || kLower == "summary" || kLower == "thought" {
			continue
		}

		if s, ok := v.(string); ok && s != "" {
			js += s + "\n"
		} else if nested, ok := v.(map[string]interface{}); ok {
			js += extractStringsFromObject(nested)
		} else if arr, ok := v.([]interface{}); ok {
			for _, item := range arr {
				if s, ok := item.(string); ok {
					js += s + "\n"
				} else if m, ok := item.(map[string]interface{}); ok {
					js += extractStringsFromObject(m)
				}
			}
		}
	}
	return js
}

func cleanHTMLForPlanning(html string) string {

	tagsToRemove := []string{"script", "style", "svg", "path", "link", "meta", "noscript", "iframe", "head", "header", "footer", "nav", "aside", "form"}

	tagsToRemove = []string{"script", "style", "svg", "path", "link", "meta", "noscript", "iframe", "head", "header", "footer", "nav", "aside"}
	for _, tag := range tagsToRemove {
		re := regexp.MustCompile(`(?is)<` + tag + `[^>]*>.*?</` + tag + `>`)
		html = re.ReplaceAllString(html, "")
		re = regexp.MustCompile(`(?is)<` + tag + `[^>]*/>`)
		html = re.ReplaceAllString(html, "")
	}

	re := regexp.MustCompile(`(?s)<!--.*?-->`)
	html = re.ReplaceAllString(html, "")

	reAttr := regexp.MustCompile(`(?i)\s([a-z0-9-]+)=(?:'[^']*'|"[^"]*"|[^\s>]+)`)
	html = reAttr.ReplaceAllStringFunc(html, func(fullMatch string) string {
		attr := strings.ToLower(strings.TrimSpace(fullMatch))
		whitelist := []string{"id=", "class=", "name=", "value=", "type=", "placeholder=", "href=", "data-", "aria-label="}
		for _, w := range whitelist {
			if strings.HasPrefix(attr, w) {
				return fullMatch
			}
		}
		return ""
	})

	re = regexp.MustCompile(`\n+`)
	html = re.ReplaceAllString(html, "\n")
	re = regexp.MustCompile(`[ \t]+`)
	html = re.ReplaceAllString(html, " ")

	return strings.TrimSpace(html)
}

func sanitizeJSONResponse(js string) string {

	js = regexp.MustCompile("(?s)^```(?:json)?\n?").ReplaceAllString(js, "")
	js = regexp.MustCompile("(?s)\n?```$").ReplaceAllString(js, "")

	first := strings.Index(js, "{")
	last := strings.LastIndex(js, "}")
	if first != -1 && last != -1 && last > first {
		js = js[first : last+1]
	}

	reBacktick := regexp.MustCompile("`([^`]*)`")
	js = reBacktick.ReplaceAllStringFunc(js, func(m string) string {
		inner := strings.Trim(m, "`")
		inner = strings.ReplaceAll(inner, "\\", "\\\\")
		inner = strings.ReplaceAll(inner, "\"", "\\\"")
		inner = strings.ReplaceAll(inner, "\n", "\\n")
		return "\"" + inner + "\""
	})

	js = strings.ReplaceAll(js, "\\'", "'")
	js = strings.ReplaceAll(js, "\\\\'", "'")

	js = strings.ReplaceAll(js, "\\\\[", "[")
	js = strings.ReplaceAll(js, "\\\\]", "]")
	js = strings.ReplaceAll(js, "\\\\[", "[")

	js = regexp.MustCompile(`,\s*([}\]])`).ReplaceAllString(js, "$1")

	reNewline := regexp.MustCompile(`"(?:[^"\\]|\\.)*"`)
	js = reNewline.ReplaceAllStringFunc(js, func(s string) string {
		return strings.ReplaceAll(s, "\n", "\\n")
	})

	return strings.TrimSpace(js)
}
