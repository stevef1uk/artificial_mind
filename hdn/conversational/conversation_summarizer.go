package conversational

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// ConversationTurn represents a single turn in a conversation
type ConversationTurn struct {
	UserMessage       string
	AssistantResponse string
	Timestamp         time.Time
}

// ConversationSummarizer handles summarizing conversations and storing them in the knowledge base
type ConversationSummarizer struct {
	llmClient              LLMClientInterface
	hdnClient              HDNClientInterface
	redis                  *redis.Client
	summarizeAfterMessages int // Trigger summary after N messages
}

// NewConversationSummarizer creates a new conversation summarizer
func NewConversationSummarizer(llm LLMClientInterface, hdn HDNClientInterface, redisClient *redis.Client) *ConversationSummarizer {
	return &ConversationSummarizer{
		llmClient:              llm,
		hdnClient:              hdn,
		redis:                  redisClient,
		summarizeAfterMessages: 5, // Default: summarize every 5 messages
	}
}

// ShouldSummarize checks if a conversation should be summarized
func (cs *ConversationSummarizer) ShouldSummarize(ctx context.Context, sessionID string) (bool, error) {
	key := fmt.Sprintf("conversation_message_count:%s", sessionID)
	count, err := cs.redis.Incr(ctx, key).Result()
	if err != nil {
		return false, fmt.Errorf("failed to increment message count: %w", err)
	}

	// Set expiry on the counter (24 hours)
	cs.redis.Expire(ctx, key, 24*time.Hour)

	// Check if we've hit the threshold
	if count >= int64(cs.summarizeAfterMessages) {
		// Reset counter
		cs.redis.Del(ctx, key)
		return true, nil
	}

	return false, nil
}

// SummarizeConversation generates a summary of the conversation and stores it in Weaviate
func (cs *ConversationSummarizer) SummarizeConversation(ctx context.Context, sessionID string, conversationHistory []ConversationTurn) error {
	if len(conversationHistory) == 0 {
		return nil // Nothing to summarize
	}

	log.Printf("📝 [SUMMARIZER] Generating summary for session %s (%d messages)", sessionID, len(conversationHistory))

	// Build the conversation text
	var conversationText strings.Builder
	for i, turn := range conversationHistory {
		conversationText.WriteString(fmt.Sprintf("Turn %d:\n", i+1))
		conversationText.WriteString(fmt.Sprintf("User: %s\n", turn.UserMessage))
		conversationText.WriteString(fmt.Sprintf("Assistant: %s\n\n", turn.AssistantResponse))
	}

	// Generate summary using LLM
	summaryPrompt := fmt.Sprintf(`You are a conversation summarizer. Analyze the following conversation and create a concise summary that captures:
1. Key facts the user shared (name, preferences, goals, etc.)
2. Important topics discussed
3. Decisions made or tasks identified
4. Any ongoing context that should be remembered

Conversation:
%s

Provide a structured summary in the following format:

**User Information:**
- [Any personal details shared]

**Topics Discussed:**
- [Main topics]

**Key Facts:**
- [Important information]

**Action Items/Tasks:**
- [Any tasks or goals mentioned]

**Context to Remember:**
- [Ongoing context]

Keep the summary concise but comprehensive.`, conversationText.String())

	summary, err := cs.llmClient.GenerateResponse(ctx, summaryPrompt, 500)
	if err != nil {
		return fmt.Errorf("failed to generate summary: %w", err)
	}

	summary = strings.TrimSpace(summary)
	log.Printf("✅ [SUMMARIZER] Generated summary (%d chars)", len(summary))

	// Extract topics for metadata (simple keyword extraction)
	topics := cs.extractTopics(conversationHistory)

	// Store summary in Weaviate as an AgiEpisode
	timestamp := time.Now().Format(time.RFC3339)
	episodeText := fmt.Sprintf("Conversation Summary (Session: %s, Time: %s)\n\n%s", sessionID, timestamp, summary)

	// Use the HDN client to store in Weaviate
	log.Printf("📦 [SUMMARIZER] Storing summary in Weaviate: session=%s, topics=%v", sessionID, topics)

	// Prepare metadata for Weaviate
	metadata := map[string]interface{}{
		"summary":           summary,
		"session_id":        sessionID,
		"last_summary_time": timestamp,
		"topics":            topics,
		"message_count":     len(conversationHistory),
		"type":              "conversation_summary",
	}

	err = cs.hdnClient.SaveEpisode(ctx, episodeText, metadata)
	if err != nil {
		log.Printf("⚠️ [SUMMARIZER] Failed to store summary in Weaviate: %v", err)
	}

	// For now, also store in Redis as a backup
	summaryKey := fmt.Sprintf("conversation_summary:%s:%d", sessionID, time.Now().Unix())
	_ = cs.redis.Set(ctx, summaryKey, episodeText, 30*24*time.Hour).Err() // Keep for 30 days

	// Store metadata about the summary in Redis
	metadataKey := fmt.Sprintf("conversation_summary_metadata:%s", sessionID)
	// Store as JSON in Redis
	b, _ := json.Marshal(metadata)
	cs.redis.Set(ctx, metadataKey, string(b), 30*24*time.Hour)

	log.Printf("✅ [SUMMARIZER] Summary stored successfully")
	return nil
}

// extractTopics extracts key topics from conversation history
func (cs *ConversationSummarizer) extractTopics(history []ConversationTurn) []string {
	topics := make(map[string]bool)

	// Simple keyword extraction - look for capitalized words and common topics
	for _, turn := range history {
		words := strings.Fields(turn.UserMessage)
		for _, word := range words {
			word = strings.TrimSpace(strings.Trim(word, ".,!?;:"))
			// Add words that start with capital letter and are longer than 3 chars
			if len(word) > 3 && word[0] >= 'A' && word[0] <= 'Z' {
				topics[word] = true
			}
		}
	}

	// Convert to slice
	result := make([]string, 0, len(topics))
	for topic := range topics {
		result = append(result, topic)
		if len(result) >= 10 { // Limit to 10 topics
			break
		}
	}

	return result
}

// GetRelevantSummaries retrieves relevant conversation summaries for the current query
func (cs *ConversationSummarizer) GetRelevantSummaries(ctx context.Context, sessionID string, query string) ([]string, error) {
	// Search for summaries related to this session
	pattern := fmt.Sprintf("conversation_summary:%s:*", sessionID)
	keys, err := cs.redis.Keys(ctx, pattern).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get summary keys: %w", err)
	}

	if len(keys) == 0 {
		return []string{}, nil
	}

	// Get all summaries for this session
	summaries := make([]string, 0, len(keys))
	for _, key := range keys {
		summary, err := cs.redis.Get(ctx, key).Result()
		if err != nil {
			log.Printf("⚠️ [SUMMARIZER] Failed to get summary %s: %v", key, err)
			continue
		}
		summaries = append(summaries, summary)
	}

	log.Printf("📚 [SUMMARIZER] Retrieved %d summaries for session %s", len(summaries), sessionID)
	return summaries, nil
}
