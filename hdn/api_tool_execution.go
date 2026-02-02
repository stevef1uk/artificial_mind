package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
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
