package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// executeToolDirect executes a tool directly (for use by agents)
func (s *APIServer) executeToolDirect(ctx context.Context, toolID string, params map[string]interface{}) (interface{}, error) {
	// Route to appropriate tool handler based on tool ID
	if strings.HasPrefix(toolID, "mcp_") {
		if s.mcpKnowledgeServer == nil {
			return nil, fmt.Errorf("MCP knowledge server not available")
		}
		toolName := strings.TrimPrefix(toolID, "mcp_")
		return s.mcpKnowledgeServer.callTool(ctx, toolName, params)
	}

	// Handle HDN tools
	switch toolID {
	case "tool_http_get":
		url, _ := getString(params, "url")
		if strings.TrimSpace(url) == "" {
			return nil, fmt.Errorf("url required")
		}
		safeClient := NewSafeHTTPClient()
		content, err := safeClient.SafeGetWithContentCheck(ctx, url)
		if err != nil {
			return nil, fmt.Errorf("content blocked for safety: %w", err)
		}
		return map[string]interface{}{"status": 200, "body": content}, nil

	case "tool_telegram_send":
		return s.sendTelegramMessage(ctx, params)

	case "tool_file_read":
		path, _ := getString(params, "path")
		if path == "" {
			path, _ = getString(params, "file_path")
		}
		if path == "" {
			return nil, fmt.Errorf("path or file_path required")
		}
		if !fileExists(path) {
			return nil, fmt.Errorf("file not found: %s", path)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("failed to read file: %w", err)
		}
		return map[string]interface{}{"content": string(b)}, nil

	case "tool_file_write":
		path, _ := getString(params, "path")
		if path == "" {
			path, _ = getString(params, "file_path")
		}
		content, _ := getString(params, "content")
		if path == "" {
			return nil, fmt.Errorf("path required")
		}
		// Create directory if it doesn't exist
		dir := filepath.Dir(path)
		if dir != "" && dir != "." {
			if err := os.MkdirAll(dir, 0755); err != nil {
				return nil, fmt.Errorf("failed to create directory: %w", err)
			}
		}
		err := os.WriteFile(path, []byte(content), 0644)
		if err != nil {
			return nil, fmt.Errorf("failed to write file: %w", err)
		}
		return map[string]interface{}{"written": len(content), "success": true}, nil

	case "tool_ls":
		path, _ := getString(params, "path")
		if path == "" {
			path = "."
		}
		entries, err := os.ReadDir(path)
		if err != nil {
			return nil, fmt.Errorf("failed to list directory: %w", err)
		}
		names := []string{}
		for _, e := range entries {
			names = append(names, e.Name())
		}
		return map[string]interface{}{"entries": names}, nil

	case "tool_exec":
		cmd, _ := getString(params, "cmd")
		if strings.TrimSpace(cmd) == "" {
			return nil, fmt.Errorf("cmd required")
		}
		execCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
		defer cancel()
		execCmd := exec.CommandContext(execCtx, "/bin/sh", "-c", cmd)
		output, err := execCmd.CombinedOutput()
		exitCode := 0
		if err != nil {
			if exitError, ok := err.(*exec.ExitError); ok {
				exitCode = exitError.ExitCode()
			} else {
				return nil, fmt.Errorf("failed to execute command: %w", err)
			}
		}
		return map[string]interface{}{
			"output":    string(output),
			"stdout":    string(output), // Compatibility
			"exit_code": exitCode,
		}, nil

	case "tool_json_parse":
		text, _ := getString(params, "text")
		if text == "" {
			text, _ = getString(params, "json")
		}
		var result interface{}
		if err := json.Unmarshal([]byte(text), &result); err != nil {
			return nil, fmt.Errorf("failed to parse JSON: %w", err)
		}
		return map[string]interface{}{"object": result, "success": true}, nil

	case "tool_text_search":
		pattern, _ := getString(params, "pattern")
		text, _ := getString(params, "text")
		if pattern == "" || text == "" {
			return nil, fmt.Errorf("pattern and text required")
		}
		re, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("invalid pattern: %w", err)
		}
		matches := re.FindAllString(text, -1)
		return map[string]interface{}{"matches": matches, "count": len(matches)}, nil

	default:
		return nil, fmt.Errorf("unknown tool: %s", toolID)
	}
}

// sendTelegramMessage sends a message via Telegram Bot API
func (s *APIServer) sendTelegramMessage(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	botToken := os.Getenv("TELEGRAM_BOT_TOKEN")
	if botToken == "" {
		return nil, fmt.Errorf("TELEGRAM_BOT_TOKEN environment variable not set")
	}

	chatID, _ := getString(params, "chat_id")
	if chatID == "" {
		// Try to get from environment as fallback
		chatID = os.Getenv("TELEGRAM_CHAT_ID")
		if chatID == "" {
			return nil, fmt.Errorf("chat_id parameter or TELEGRAM_CHAT_ID environment variable required")
		}
	}

	message, _ := getString(params, "message")
	if message == "" {
		return nil, fmt.Errorf("message parameter required")
	}

	// Parse mode (Markdown, HTML, or plain text)
	parseMode := "Markdown"
	if pm, ok := params["parse_mode"].(string); ok && pm != "" {
		parseMode = pm
	}

	// Build Telegram API request
	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", botToken)
	payload := map[string]interface{}{
		"chat_id":    chatID,
		"text":       message,
		"parse_mode": parseMode,
	}

	// Add disable_web_page_preview if specified
	if disablePreview, ok := params["disable_web_page_preview"].(bool); ok {
		payload["disable_web_page_preview"] = disablePreview
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send Telegram message: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		// Error handling for Markdown parsing errors
		if resp.StatusCode == 400 && parseMode != "" && strings.Contains(string(body), "can't parse entities") {
			log.Printf("⚠️ [TELEGRAM] Markdown parsing failed, retrying without parse_mode: %s", string(body))
			// Retry without parse_mode for better reliability
			delete(payload, "parse_mode")
			jsonDataRetry, _ := json.Marshal(payload)
			reqRetry, _ := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewBuffer(jsonDataRetry))
			reqRetry.Header.Set("Content-Type", "application/json")
			respRetry, errRetry := client.Do(reqRetry)
			if errRetry == nil {
				defer respRetry.Body.Close()
				if respRetry.StatusCode == http.StatusOK {
					bodyRetry, _ := io.ReadAll(respRetry.Body)
					var resultRetry map[string]interface{}
					if err := json.Unmarshal(bodyRetry, &resultRetry); err == nil {
						log.Printf("✅ [TELEGRAM] Successfully sent message on retry without parse_mode")
						return map[string]interface{}{
							"success":    true,
							"message_id": resultRetry["result"].(map[string]interface{})["message_id"],
							"chat_id":    chatID,
							"note":       "sent as plain text due to markdown parsing error",
						}, nil
					}
				} else {
					bodyRetry, _ := io.ReadAll(respRetry.Body)
					log.Printf("❌ [TELEGRAM] Retry also failed with status %d: %s", respRetry.StatusCode, string(bodyRetry))
				}
			}
		}
		return nil, fmt.Errorf("Telegram API error (status %d): %s", resp.StatusCode, string(body))
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return map[string]interface{}{
		"success":    true,
		"message_id": result["result"].(map[string]interface{})["message_id"],
		"chat_id":    chatID,
	}, nil
}
