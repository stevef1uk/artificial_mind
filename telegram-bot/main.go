package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	telegramAPIBase = "https://api.telegram.org/bot"
	mcpServerURL    = "http://localhost:8081/mcp"
)

type Update struct {
	UpdateID int     `json:"update_id"`
	Message  Message `json:"message"`
}

type Message struct {
	MessageID int    `json:"message_id"`
	From      User   `json:"from"`
	Chat      Chat   `json:"chat"`
	Text      string `json:"text"`
}

type User struct {
	ID        int    `json:"id"`
	FirstName string `json:"first_name"`
	Username  string `json:"username"`
}

type Chat struct {
	ID int `json:"id"`
}

type TelegramResponse struct {
	OK     bool     `json:"ok"`
	Result []Update `json:"result"`
}

type MCPRequest struct {
	JSONRPC string                 `json:"jsonrpc"`
	ID      int                    `json:"id"`
	Method  string                 `json:"method"`
	Params  map[string]interface{} `json:"params"`
}

type MCPResponse struct {
	JSONRPC string                 `json:"jsonrpc"`
	ID      int                    `json:"id"`
	Result  map[string]interface{} `json:"result"`
	Error   *MCPError              `json:"error,omitempty"`
}

type MCPError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type ConversationRequest struct {
	Message      string            `json:"message"`
	SessionID    string            `json:"session_id,omitempty"`
	Context      map[string]string `json:"context,omitempty"`
	ShowThinking bool              `json:"show_thinking,omitempty"`
}

type ConversationResponse struct {
	Response        string             `json:"response"`
	SessionID       string             `json:"session_id"`
	ThinkingSummary string             `json:"thinking_summary,omitempty"`
	Thoughts        []ExpressedThought `json:"thoughts,omitempty"`
}

type ExpressedThought struct {
	Type     string `json:"type"`
	Content  string `json:"content"`
	ToolUsed string `json:"tool_used,omitempty"`
	Action   string `json:"action,omitempty"`
}

type TelegramBot struct {
	token           string
	mcpURL          string
	chatURL         string
	lastUpdate      int
	thinkingEnabled map[int]bool
	allowedUsers    map[string]bool
}

func NewTelegramBot(token, mcpURL, chatURL string, allowed []string) *TelegramBot {
	allowedMap := make(map[string]bool)
	for _, u := range allowed {
		allowedMap[strings.ToLower(strings.TrimPrefix(u, "@"))] = true
	}

	return &TelegramBot{
		token:           token,
		mcpURL:          mcpURL,
		chatURL:         chatURL,
		lastUpdate:      0,
		thinkingEnabled: make(map[int]bool),
		allowedUsers:    allowedMap,
	}
}

func (bot *TelegramBot) getUpdates() ([]Update, error) {
	url := fmt.Sprintf("%s%s/getUpdates?offset=%d&timeout=30", telegramAPIBase, bot.token, bot.lastUpdate+1)

	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var telegramResp TelegramResponse
	if err := json.NewDecoder(resp.Body).Decode(&telegramResp); err != nil {
		return nil, err
	}

	if !telegramResp.OK {
		return nil, fmt.Errorf("telegram API error")
	}

	return telegramResp.Result, nil
}

func (bot *TelegramBot) sendMessage(chatID int, text string) error {
	// Standard Telegram maxLength is 4096, using 4000 for safety
	const maxLength = 4000

	if len(text) <= maxLength {
		return bot.sendSingleMessage(chatID, text)
	}

	// Truncated message - send first part with truncated notice
	log.Printf("⚠️ Message to %d is too long (%d chars), truncating to %d", chatID, len(text), maxLength)
	truncated := text[:maxLength] + "\n\n... (truncated, message too long)"
	if err := bot.sendSingleMessage(chatID, truncated); err != nil {
		return err
	}

	// Notify about truncation
	infoMsg := fmt.Sprintf("⚠️ *Response was %d characters* (limit is %d). Showing first %d characters only.", len(text), maxLength, maxLength)
	return bot.sendSingleMessage(chatID, infoMsg)
}

func (bot *TelegramBot) sendSingleMessage(chatID int, text string) error {
	url := fmt.Sprintf("%s%s/sendMessage", telegramAPIBase, bot.token)

	payload := map[string]interface{}{
		"chat_id":    chatID,
		"text":       text,
		"parse_mode": "Markdown",
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	resp, err := http.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		return nil
	}

	// If Markdown failed, retry as plain text
	if resp.StatusCode == http.StatusBadRequest {
		log.Printf("⚠️ Telegram Markdown failed for chat %d, retrying as plain text", chatID)
		delete(payload, "parse_mode")
		jsonData, _ = json.Marshal(payload)
		resp2, err := http.Post(url, "application/json", bytes.NewBuffer(jsonData))
		if err != nil {
			return err
		}
		defer resp2.Body.Close()

		if resp2.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp2.Body)
			return fmt.Errorf("telegram API error (plain text retry): %d %s", resp2.StatusCode, string(body))
		}
		return nil
	}

	body, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("telegram API error: %d %s", resp.StatusCode, string(body))
}

func (bot *TelegramBot) callChatAPI(chatID int, message string) (string, error) {
	sessionID := fmt.Sprintf("tg_chat_%d", chatID)
	req := ConversationRequest{
		Message:      message,
		SessionID:    sessionID,
		ShowThinking: bot.thinkingEnabled[chatID],
	}

	reqJSON, err := json.Marshal(req)
	if err != nil {
		return "", err
	}

	httpReq, err := http.NewRequest("POST", bot.chatURL, bytes.NewBuffer(reqJSON))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Request-Source", "telegram")

	// Use a client with timeout to prevent hanging (3.5 minutes to allow for chat processing + tool execution)
	client := &http.Client{
		Timeout: 210 * time.Second, // 3.5 minutes
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("Chat service error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Chat service returned status: %s", resp.Status)
	}

	var chatResp ConversationResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return "", fmt.Errorf("failed to parse chat response: %v", err)
	}

	var response strings.Builder
	if bot.thinkingEnabled[chatID] {
		response.WriteString("💭 *Thinking Process:*\n")
		if chatResp.ThinkingSummary != "" {
			response.WriteString("_" + chatResp.ThinkingSummary + "_\n\n")
		}

		for _, thought := range chatResp.Thoughts {
			prefix := "• "
			if thought.Type == "action" || thought.ToolUsed != "" {
				prefix = "🔧 "
			} else if thought.Type == "decision" {
				prefix = "🤔 "
			}

			content := thought.Content
			if thought.ToolUsed != "" {
				content = fmt.Sprintf("Using tool: *%s*", thought.ToolUsed)
			}

			response.WriteString(fmt.Sprintf("%s %s\n", prefix, content))
		}
		response.WriteString("\n")
	}

	response.WriteString(chatResp.Response)
	log.Printf("✅ Chat API response received for chat %d (%d chars)", chatID, len(chatResp.Response))
	return response.String(), nil
}

func (bot *TelegramBot) callMCPTool(toolName string, args map[string]interface{}) (string, error) {
	mcpReq := MCPRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/call",
		Params: map[string]interface{}{
			"name":      toolName,
			"arguments": args,
		},
	}

	reqJSON, err := json.Marshal(mcpReq)
	if err != nil {
		return "", err
	}

	httpReq, err := http.NewRequest("POST", bot.mcpURL, bytes.NewBuffer(reqJSON))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Request-Source", "telegram")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("MCP server error: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var mcpResp MCPResponse
	if err := json.Unmarshal(body, &mcpResp); err != nil {
		return "", fmt.Errorf("failed to parse MCP response: %v", err)
	}

	if mcpResp.Error != nil {
		return "", fmt.Errorf("MCP error: %s", mcpResp.Error.Message)
	}

	// Extract text from MCP response
	if content, ok := mcpResp.Result["content"].([]interface{}); ok && len(content) > 0 {
		if contentItem, ok := content[0].(map[string]interface{}); ok {
			if text, ok := contentItem["text"].(string); ok {
				// Deduplicate consecutive lines for cleaner output
				return deduplicateLines(text), nil
			}
		}
	}

	// Fallback: return entire result as JSON
	resultJSON, _ := json.MarshalIndent(mcpResp.Result, "", "  ")
	return string(resultJSON), nil
}

// deduplicateLines removes consecutive duplicate lines
func deduplicateLines(text string) string {
	lines := strings.Split(text, "\n")
	if len(lines) == 0 {
		return text
	}

	var result []string
	lastLine := ""

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		// Skip if same as previous line (case-insensitive)
		if strings.EqualFold(trimmed, lastLine) {
			continue
		}
		result = append(result, line)
		lastLine = trimmed
	}

	return strings.Join(result, "\n")
}

func (bot *TelegramBot) handleMessage(msg Message) {
	username := strings.ToLower(msg.From.Username)
	if len(bot.allowedUsers) > 0 && !bot.allowedUsers[username] {
		log.Printf("🚫 Unauthorized access attempt from %s (ID: %d)", msg.From.Username, msg.From.ID)
		bot.sendMessage(msg.Chat.ID, "🚫 *Unauthorized*\nSorry, you are not on the whitelist for this AGI bot.")
		return
	}
	log.Printf("📩 Message from %s (@%s, UserID: %d): %s", msg.From.FirstName, msg.From.Username, msg.From.ID, msg.Text)

	text := strings.TrimSpace(msg.Text)

	var response string
	var err error

	// Parse commands
	if strings.HasPrefix(text, "/start") || strings.HasPrefix(text, "/help") {
		response = `🤖 *AGI Assistant Bot*

Available commands:
/scrape <url> - Scrape a website and get clean text
/query <cypher> - Query Neo4j knowledge graph
/search <text> - Search Weaviate vector database
/concept <name> - Get concept details
/related <name> - Find related concepts
/avatar <query> - Search personal context (Avatar)
/browse <url> <instructions> - Browse web with AI instructions
/thinking - Toggle thinking mode (show/hide reasoning)
/help - Show this message

Examples:
/scrape https://bbc.co.uk
/browse https://ecotree.green calculate carbon for flight
/avatar what are my skills?`
	} else if strings.HasPrefix(text, "/scrape ") {
		url := strings.TrimSpace(strings.TrimPrefix(text, "/scrape"))
		if url == "" {
			response = "❌ Please provide a URL. Example: /scrape https://bbc.co.uk"
		} else {
			response, err = bot.callMCPTool("scrape_url", map[string]interface{}{"url": url})
			if err != nil {
				response = fmt.Sprintf("❌ Error: %v", err)
			}
		}
	} else if strings.HasPrefix(text, "/browse ") {
		parts := strings.SplitN(strings.TrimSpace(strings.TrimPrefix(text, "/browse")), " ", 2)
		if len(parts) < 2 {
			response = "❌ Usage: /browse <url> <instructions>\nExample: /browse https://ecotree.green calculate carbon for flight"
		} else {
			response, err = bot.callMCPTool("browse_web", map[string]interface{}{
				"url":          parts[0],
				"instructions": parts[1],
			})
			if err != nil {
				response = fmt.Sprintf("❌ Error: %v", err)
			}
		}
	} else if strings.HasPrefix(text, "/query ") {
		query := strings.TrimSpace(strings.TrimPrefix(text, "/query"))
		if query == "" {
			response = "❌ Please provide a Cypher query. Example: /query MATCH (n) RETURN n LIMIT 5"
		} else {
			response, err = bot.callMCPTool("query_neo4j", map[string]interface{}{"query": query})
			if err != nil {
				response = fmt.Sprintf("❌ Error: %v", err)
			}
		}
	} else if strings.HasPrefix(text, "/search ") {
		searchText := strings.TrimSpace(strings.TrimPrefix(text, "/search"))
		if searchText == "" {
			response = "❌ Please provide search text. Example: /search machine learning"
		} else {
			response, err = bot.callMCPTool("search_weaviate", map[string]interface{}{"query": searchText})
			if err != nil {
				response = fmt.Sprintf("❌ Error: %v", err)
			}
		}
	} else if strings.HasPrefix(text, "/concept ") {
		conceptName := strings.TrimSpace(strings.TrimPrefix(text, "/concept"))
		if conceptName == "" {
			response = "❌ Please provide a concept name. Example: /concept AI"
		} else {
			response, err = bot.callMCPTool("get_concept", map[string]interface{}{"name": conceptName})
			if err != nil {
				response = fmt.Sprintf("❌ Error: %v", err)
			}
		}
	} else if strings.HasPrefix(text, "/related ") {
		conceptName := strings.TrimSpace(strings.TrimPrefix(text, "/related"))
		if conceptName == "" {
			response = "❌ Please provide a concept name. Example: /related AI"
		} else {
			response, err = bot.callMCPTool("find_related_concepts", map[string]interface{}{"name": conceptName})
			if err != nil {
				response = fmt.Sprintf("❌ Error: %v", err)
			}
		}
	} else if strings.HasPrefix(text, "/avatar ") {
		query := strings.TrimSpace(strings.TrimPrefix(text, "/avatar"))
		if query == "" {
			response = "❌ Please provide a query. Example: /avatar what are my skills?"
		} else {
			response, err = bot.callMCPTool("search_avatar_context", map[string]interface{}{"query": query})
			if err != nil {
				response = fmt.Sprintf("❌ Error: %v", err)
			}
		}
	} else if strings.HasPrefix(text, "/thinking") {
		bot.thinkingEnabled[msg.Chat.ID] = !bot.thinkingEnabled[msg.Chat.ID]
		status := "disabled"
		if bot.thinkingEnabled[msg.Chat.ID] {
			status = "enabled"
		}
		response = fmt.Sprintf("💭 Thought expression is now *%s*.", status)
	} else {
		// Default to Chat API
		response, err = bot.callChatAPI(msg.Chat.ID, text)
		if err != nil {
			response = fmt.Sprintf("❌ Chat error: %v", err)
		}
	}

	// Send response
	if err := bot.sendMessage(msg.Chat.ID, response); err != nil {
		log.Printf("Error sending message: %v", err)
	}
}

func (bot *TelegramBot) start() {
	log.Println("🤖 Telegram bot started. Polling for messages...")

	for {
		updates, err := bot.getUpdates()
		if err != nil {
			log.Printf("Error getting updates: %v", err)
			time.Sleep(5 * time.Second)
			continue
		}

		for _, update := range updates {
			if update.Message.Text != "" {
				go bot.handleMessage(update.Message)
			}
			bot.lastUpdate = update.UpdateID
		}

		time.Sleep(1 * time.Second)
	}
}

func main() {
	token := os.Getenv("TELEGRAM_BOT_TOKEN")
	if token == "" {
		log.Fatal("TELEGRAM_BOT_TOKEN environment variable not set")
	}

	mcpURL := os.Getenv("MCP_SERVER_URL")
	if mcpURL == "" {
		mcpURL = mcpServerURL
		log.Printf("Using default MCP server URL: %s", mcpURL)
	}

	chatURL := os.Getenv("CHAT_SERVER_URL")
	if chatURL == "" {
		// Default to HDN server on 8080
		chatURL = "http://localhost:8080/api/v1/chat"
		log.Printf("Using default Chat service URL: %s", chatURL)
	}

	allowedUsersStr := os.Getenv("ALLOWED_TELEGRAM_USERS")
	var allowedUsers []string
	if allowedUsersStr != "" {
		allowedUsers = strings.Split(allowedUsersStr, ",")
		log.Printf("🔒 Bot restricted to: %s", allowedUsersStr)
	} else {
		log.Printf("⚠️ WARNING: No ALLOWED_TELEGRAM_USERS set. Bot is open to everyone!")
	}

	bot := NewTelegramBot(token, mcpURL, chatURL, allowedUsers)
	bot.start()
}
