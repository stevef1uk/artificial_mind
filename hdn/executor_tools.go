package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// callTool calls a tool via the HDN server API
func (ie *IntelligentExecutor) callTool(toolID string, params map[string]interface{}) (map[string]interface{}, error) {
	if ie.hdnBaseURL == "" {
		return nil, fmt.Errorf("HDN base URL not configured for tool calling")
	}

	requestBody := params

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal tool request: %v", err)
	}

	url := fmt.Sprintf("%s/api/v1/tools/%s/invoke", ie.hdnBaseURL, toolID)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create tool request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tool request failed: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read tool response: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse tool response: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tool call failed with status %d: %v", resp.StatusCode, result)
	}

	return result, nil
}

// getAvailableTools fetches available tools from the HDN API
func (ie *IntelligentExecutor) getAvailableTools(ctx context.Context) ([]Tool, error) {
	if ie.hdnBaseURL == "" {
		return []Tool{}, nil
	}

	url := fmt.Sprintf("%s/api/v1/tools", ie.hdnBaseURL)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get tools: status %d", resp.StatusCode)
	}

	var result struct {
		Tools []Tool `json:"tools"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return result.Tools, nil
}

// filterRelevantTools filters tools relevant to the task
// NOTE: This function is intentionally permissive - it includes most tools by default
// to allow the LLM to choose the best tool for the task. Only obviously irrelevant tools are filtered.
func (ie *IntelligentExecutor) filterRelevantTools(tools []Tool, req *ExecutionRequest) []Tool {
	var relevant []Tool
	descLower := strings.ToLower(req.Description)
	taskLower := strings.ToLower(req.TaskName)
	combined := descLower + " " + taskLower

	toolKeywords := map[string][]string{
		"mcp_scrape_url":      {"scrape", "html", "web", "fetch", "url", "website", "article", "news", "page", "content", "parse html"},
		"mcp_smart_scrape":    {"scrape", "html", "web", "fetch", "url", "website", "article", "news", "page", "content", "parse html", "intelligent"},
		"tool_http_get":       {"http", "url", "fetch", "get", "request", "api", "endpoint", "download", "retrieve", "web"},
		"tool_file_read":      {"read", "file", "load", "open", "readfile", "read file", "readfile", "content", "text"},
		"tool_file_write":     {"write", "file", "save", "store", "output", "write file", "save file", "create file", "writefile"},
		"tool_ls":             {"list", "directory", "dir", "files", "ls", "list files", "directory listing", "contents"},
		"tool_exec":           {"exec", "execute", "command", "shell", "run", "cmd", "system", "bash", "sh", "terminal"},
		"tool_codegen":        {"generate", "code", "create", "write code", "generate code", "program", "script"},
		"tool_json_parse":     {"json", "parse", "parse json", "decode", "unmarshal", "deserialize"},
		"tool_text_search":    {"search", "find", "text", "pattern", "match", "grep", "filter", "text search"},
		"tool_docker_list":    {"docker", "container", "image", "list docker", "docker list", "containers"},
		"tool_docker_build":   {"docker build", "build image", "dockerfile", "container build"},
		"tool_ssh_executor":   {"ssh", "remote", "execute", "remote execution", "ssh exec"},
		"tool_generate_image": {"image", "generate image", "draw", "picture", "create image", "photo"},
	}

	seen := make(map[string]bool)

	for _, tool := range tools {
		if seen[tool.ID] {
			continue
		}

		matched := false
		for toolID, keywords := range toolKeywords {
			if tool.ID == toolID {
				for _, keyword := range keywords {
					if strings.Contains(combined, keyword) {
						relevant = append(relevant, tool)
						seen[tool.ID] = true
						matched = true
						break
					}
				}
				if matched {
					break
				}
			}
		}

		if matched {
			continue
		}

		if strings.Contains(combined, strings.ToLower(tool.ID)) {
			relevant = append(relevant, tool)
			seen[tool.ID] = true
			continue
		}

		toolDesc := strings.ToLower(tool.Description + " " + tool.Name + " " + tool.ID)
		expandedKeywords := []string{"scrape", "http", "fetch", "url", "web", "file", "read", "write", "calculator", "calculate", "add", "subtract", "multiply", "divide", "math", "exec", "execute", "command", "shell", "run", "code", "generate", "json", "parse", "search", "find", "text", "docker", "container", "ssh", "remote", "list", "directory", "dir", "image", "draw", "picture", "photo"}
		for _, keyword := range expandedKeywords {
			if strings.Contains(combined, keyword) && strings.Contains(toolDesc, keyword) {
				relevant = append(relevant, tool)
				seen[tool.ID] = true
				break
			}
		}
	}

	alwaysInclude := []string{
		"tool_http_get",
		"tool_file_read",
		"tool_file_write",
		"tool_exec",
		"tool_json_parse",
		"tool_text_search",
		"tool_ssh_executor",
		"tool_generate_image",
	}

	for _, toolID := range alwaysInclude {
		if seen[toolID] {
			continue
		}

		for _, tool := range tools {
			if tool.ID == toolID {
				relevant = append(relevant, tool)
				seen[tool.ID] = true
				break
			}
		}
	}

	if len(relevant) < 5 && len(tools) > len(relevant) {
		log.Printf("🔧 [INTELLIGENT] Only %d tools matched keywords, including additional tools for LLM flexibility", len(relevant))

		for _, tool := range tools {
			if seen[tool.ID] {
				continue
			}

			if strings.Contains(tool.ID, "tool_register") || strings.Contains(tool.ID, "tool_docker_build") {
				continue
			}
			relevant = append(relevant, tool)
			seen[tool.ID] = true
			if len(relevant) >= 10 {
				break
			}
		}
	}

	return relevant
}

// ensureRegisteredToolForTask registers a persistent tool for a task if missing.
// The tool is defined to execute via a container image matching the language,
// and invocations should pass {code, language} so execution routes via Drone in Kubernetes.
func (ie *IntelligentExecutor) ensureRegisteredToolForTask(taskName, language string) string {
	if ie.hdnBaseURL == "" {
		return ""
	}

	norm := strings.ToLower(strings.TrimSpace(taskName))
	norm = strings.ReplaceAll(norm, " ", "_")
	norm = strings.ReplaceAll(norm, "/", "_")
	toolID := "tool_" + norm

	payload := map[string]interface{}{
		"id":            toolID,
		"name":          taskName,
		"description":   "Auto-generated tool for task: " + taskName,
		"input_schema":  map[string]string{"code": "string", "language": "string"},
		"output_schema": map[string]string{"output": "string", "error": "string", "success": "bool"},
		"permissions":   []string{"proc:exec"},
		"safety_level":  "medium",
		"created_by":    "agent",
		"exec": map[string]interface{}{
			"type": "cmd",
			"cmd":  "/app/tools/docker_executor",
			"args": []string{"--code", "{{.code}}", "--language", "{{.language}}"},
		},
	}

	b, _ := json.Marshal(payload)
	url := fmt.Sprintf("%s/api/v1/tools", ie.hdnBaseURL)
	req, err := http.NewRequest("POST", url, bytes.NewReader(b))
	if err != nil {
		return toolID
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return toolID
	}
	defer resp.Body.Close()

	return toolID
}

func (ie *IntelligentExecutor) executeDirectTool(req *ExecutionRequest, start time.Time, workflowID string) (*IntelligentExecutionResult, error) {
	log.Printf("🔧 [INTELLIGENT] Routing to direct tool execution")

	desc := strings.TrimSpace(req.Description)
	rest := strings.TrimPrefix(desc, "Execute tool ")
	toolID := ""
	for i := 0; i < len(rest); i++ {
		if rest[i] == ':' || rest[i] == ' ' || rest[i] == '\n' || rest[i] == '\t' {
			toolID = strings.TrimSpace(rest[:i])
			break
		}
	}
	if toolID == "" {
		toolID = strings.TrimSpace(rest)
	}

	params := make(map[string]interface{})
	switch toolID {
	case "tool_ls":
		params["path"] = "."
	case "tool_http_get", "tool_html_scraper":
		if u, ok := req.Context["url"]; ok && strings.TrimSpace(u) != "" {
			params["url"] = u
		} else {

			urlPattern := regexp.MustCompile(`https?://[^\s]+`)
			if matches := urlPattern.FindStringSubmatch(req.Description); len(matches) > 0 {
				params["url"] = matches[0]
			} else if strings.Contains(strings.ToLower(req.Description), "wikipedia") {

				topicPattern := regexp.MustCompile(`'([^']+)'|"([^"]+)"`)
				var topic string
				if matches := topicPattern.FindStringSubmatch(req.Description); len(matches) > 0 {
					if matches[1] != "" {
						topic = matches[1]
					} else if matches[2] != "" {
						topic = matches[2]
					}
				}

				if topic == "" {

					forPattern := regexp.MustCompile(`(?:for|about)\s+([A-Z][A-Za-z]*(?:\s+[A-Z][A-Za-z]*)*)`)
					if matches := forPattern.FindStringSubmatch(req.Description); len(matches) > 0 {
						topic = matches[1]

						words := strings.Fields(topic)
						if len(words) > 3 {
							topic = strings.Join(words[:3], " ")
						}
					}
				}

				if topic == "" {
					articlePattern := regexp.MustCompile(`(?:article|page|fetch|scrape)\s+(?:for|about)\s+([A-Z][A-Za-z]*(?:\s+[A-Z][A-Za-z]*)?)`)
					if matches := articlePattern.FindStringSubmatch(req.Description); len(matches) > 0 {
						topic = matches[1]
					}
				}

				if topic != "" {

					topic = strings.ReplaceAll(strings.TrimSpace(topic), " ", "_")
					params["url"] = fmt.Sprintf("https://en.wikipedia.org/wiki/%s", topic)
				} else {

					params["url"] = "https://en.wikipedia.org/wiki/Main_Page"
				}
			} else {
				params["url"] = "http://example.com"
			}
		}
	case "tool_file_read":
		if p, ok := req.Context["path"]; ok && strings.TrimSpace(p) != "" {
			params["path"] = p
		} else {
			params["path"] = "/tmp"
		}
	case "tool_file_write":
		if p, ok := req.Context["path"]; ok && strings.TrimSpace(p) != "" {
			params["path"] = p
		} else {
			params["path"] = "/tmp/out.txt"
		}
		if c, ok := req.Context["content"]; ok {
			params["content"] = c
		} else {
			params["content"] = ""
		}
	case "tool_exec":
		if c, ok := req.Context["cmd"]; ok && strings.TrimSpace(c) != "" {
			params["cmd"] = c
		} else {
			params["cmd"] = "ls -la"
		}
	}

	toolResp, err := ie.callTool(toolID, params)

	result := &IntelligentExecutionResult{
		Success:       err == nil,
		ExecutionTime: time.Since(start),
		WorkflowID:    workflowID,
	}

	if err != nil {
		result.Error = err.Error()
	} else {
		b, _ := json.Marshal(toolResp)
		result.Result = string(b)
	}

	if ie.selfModelManager != nil {
		ie.recordExecutionEpisode(req, result, "direct_tool_call")
	}

	return result, nil
}

func (ie *IntelligentExecutor) executeExplicitTool(req *ExecutionRequest, toolID string, start time.Time, workflowID string) (*IntelligentExecutionResult, error) {
	log.Printf("🔧 [INTELLIGENT] Routing to explicit tool execution: %s", toolID)

	params := make(map[string]interface{})

	if toolID == "tool_http_get" || toolID == "tool_html_scraper" || toolID == "mcp_scrape_url" || toolID == "mcp_smart_scrape" {
		if u, ok := req.Context["url"]; ok && strings.TrimSpace(u) != "" {
			params["url"] = u
		} else {

			urlPattern := regexp.MustCompile(`https?://[^\s]+`)
			if matches := urlPattern.FindStringSubmatch(req.Description); len(matches) > 0 {
				params["url"] = matches[0]
			} else if strings.Contains(strings.ToLower(req.Description), "wikipedia") {

				topicPattern := regexp.MustCompile(`'([^']+)'|"([^"]+)"`)
				var topic string
				if matches := topicPattern.FindStringSubmatch(req.Description); len(matches) > 0 {
					if matches[1] != "" {
						topic = matches[1]
					} else if matches[2] != "" {
						topic = matches[2]
					}
				}

				if topic == "" {

					forPattern := regexp.MustCompile(`(?:for|about)\s+([A-Z][A-Za-z]*(?:\s+[A-Z][A-Za-z]*)*)`)
					if matches := forPattern.FindStringSubmatch(req.Description); len(matches) > 0 {
						topic = matches[1]

						words := strings.Fields(topic)
						if len(words) > 3 {
							topic = strings.Join(words[:3], " ")
						}
					}
				}

				if topic == "" {
					articlePattern := regexp.MustCompile(`(?:article|page|fetch|scrape)\s+(?:for|about)\s+([A-Z][A-Za-z]*(?:\s+[A-Z][A-Za-z]*)?)`)
					if matches := articlePattern.FindStringSubmatch(req.Description); len(matches) > 0 {
						topic = matches[1]
					}
				}

				if topic != "" {

					topic = strings.ReplaceAll(strings.TrimSpace(topic), " ", "_")
					params["url"] = fmt.Sprintf("https://en.wikipedia.org/wiki/%s", topic)
				} else {

					params["url"] = "https://en.wikipedia.org/wiki/Main_Page"
				}
			} else {
				params["url"] = "http://example.com"
			}
		}
	} else if toolID == "tool_generate_image" {
		if p, ok := req.Context["prompt"]; ok && strings.TrimSpace(p) != "" {
			params["prompt"] = p
		} else {
			params["prompt"] = req.Description
		}
		
		if s, ok := req.Context["source_image"]; ok && strings.TrimSpace(s) != "" {
			params["source_image"] = s
		}
	} else {
		// Forward any generic context fields to the tool as parameters
		for k, v := range req.Context {
			params[k] = v
		}
	}

	toolResp, err := ie.callTool(toolID, params)

	result := &IntelligentExecutionResult{
		Success:       err == nil,
		ExecutionTime: time.Since(start),
		WorkflowID:    workflowID,
	}

	if err != nil {
		result.Error = err.Error()
	} else {
		b, _ := json.Marshal(toolResp)
		result.Result = string(b)
	}

	if ie.selfModelManager != nil {
		ie.recordExecutionEpisode(req, result, "direct_tool_call")
	}

	return result, nil
}
