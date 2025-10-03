package conversational

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
)

// ConversationMemory manages conversation context and history
type ConversationMemory struct {
	redis *redis.Client
}

// ConversationContext contains the current conversation context
type ConversationContext struct {
	SessionID           string                 `json:"session_id"`
	LastUserMessage     string                 `json:"last_user_message"`
	LastAIResponse      string                 `json:"last_ai_response"`
	LastIntent          *Intent                `json:"last_intent"`
	LastAction          *Action                `json:"last_action"`
	LastResult          *ActionResult          `json:"last_result"`
	ConversationHistory []ConversationEntry    `json:"conversation_history"`
	ContextData         map[string]interface{} `json:"context_data"`
	CreatedAt           time.Time              `json:"created_at"`
	UpdatedAt           time.Time              `json:"updated_at"`
}

// ConversationEntry represents a single conversation turn
type ConversationEntry struct {
	Timestamp   time.Time              `json:"timestamp"`
	UserMessage string                 `json:"user_message"`
	AIResponse  string                 `json:"ai_response"`
	Intent      *Intent                `json:"intent,omitempty"`
	Action      *Action                `json:"action,omitempty"`
	Result      *ActionResult          `json:"result,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// NewConversationMemory creates a new conversation memory system
func NewConversationMemory(redis *redis.Client) *ConversationMemory {
	return &ConversationMemory{
		redis: redis,
	}
}

// GetContext retrieves the conversation context for a session
func (cm *ConversationMemory) GetContext(ctx context.Context, sessionID string) (map[string]interface{}, error) {
	key := fmt.Sprintf("conversation_context:%s", sessionID)

	data, err := cm.redis.Get(ctx, key).Result()
	if err != nil {
		if err == redis.Nil {
			// No context exists, return empty context
			return make(map[string]interface{}), nil
		}
		return nil, fmt.Errorf("failed to get conversation context: %w", err)
	}

	var context ConversationContext
	err = json.Unmarshal([]byte(data), &context)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal conversation context: %w", err)
	}

	// Convert to generic map for compatibility
	result := make(map[string]interface{})
	result["session_id"] = context.SessionID
	result["last_user_message"] = context.LastUserMessage
	result["last_ai_response"] = context.LastAIResponse
	result["last_intent"] = context.LastIntent
	result["last_action"] = context.LastAction
	result["last_result"] = context.LastResult
	result["conversation_history"] = context.ConversationHistory
	result["context_data"] = context.ContextData
	result["created_at"] = context.CreatedAt
	result["updated_at"] = context.UpdatedAt

	return result, nil
}

// SaveContext saves the conversation context for a session
func (cm *ConversationMemory) SaveContext(ctx context.Context, sessionID string, contextData map[string]interface{}) error {
	key := fmt.Sprintf("conversation_context:%s", sessionID)

	// Load existing context or create new one
	existingContext, err := cm.GetContext(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("failed to load existing context: %w", err)
	}

	// Create or update context
	context := &ConversationContext{
		SessionID:   sessionID,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
		ContextData: make(map[string]interface{}),
	}

	// If we have existing context, preserve it
	if existingContext != nil {
		if created, ok := existingContext["created_at"].(time.Time); ok {
			context.CreatedAt = created
		}
		if history, ok := existingContext["conversation_history"].([]ConversationEntry); ok {
			context.ConversationHistory = history
		}
		if data, ok := existingContext["context_data"].(map[string]interface{}); ok {
			context.ContextData = data
		}
	}

	// Update with new data
	for key, value := range contextData {
		switch key {
		case "last_user_message":
			if msg, ok := value.(string); ok {
				context.LastUserMessage = msg
			}
		case "last_ai_response":
			if msg, ok := value.(string); ok {
				context.LastAIResponse = msg
			}
		case "last_intent":
			if intent, ok := value.(*Intent); ok {
				context.LastIntent = intent
			}
		case "last_action":
			if action, ok := value.(*Action); ok {
				context.LastAction = action
			}
		case "last_result":
			if result, ok := value.(*ActionResult); ok {
				context.LastResult = result
			}
		case "timestamp":
			// Skip timestamp, we'll use UpdatedAt
		default:
			context.ContextData[key] = value
		}
	}

	// Add new conversation entry if we have both user message and AI response
	if context.LastUserMessage != "" && context.LastAIResponse != "" {
		entry := ConversationEntry{
			Timestamp:   time.Now(),
			UserMessage: context.LastUserMessage,
			AIResponse:  context.LastAIResponse,
			Intent:      context.LastIntent,
			Action:      context.LastAction,
			Result:      context.LastResult,
			Metadata: map[string]interface{}{
				"session_id": sessionID,
			},
		}
		context.ConversationHistory = append(context.ConversationHistory, entry)

		// Keep only last 50 entries to prevent memory bloat
		if len(context.ConversationHistory) > 50 {
			context.ConversationHistory = context.ConversationHistory[len(context.ConversationHistory)-50:]
		}
	}

	// Marshal and save
	data, err := json.Marshal(context)
	if err != nil {
		return fmt.Errorf("failed to marshal conversation context: %w", err)
	}

	// Save with 7 day expiration
	err = cm.redis.Set(ctx, key, data, 7*24*time.Hour).Err()
	if err != nil {
		return fmt.Errorf("failed to save conversation context: %w", err)
	}

	log.Printf("💾 [CONVERSATION-MEMORY] Saved context for session: %s", sessionID)
	return nil
}

// GetHistory retrieves conversation history for a session
func (cm *ConversationMemory) GetHistory(ctx context.Context, sessionID string, limit int) ([]ConversationResponse, error) {
	context, err := cm.GetContext(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get conversation context: %w", err)
	}

	history, ok := context["conversation_history"].([]ConversationEntry)
	if !ok {
		return []ConversationResponse{}, nil
	}

	// Convert to ConversationResponse format
	var responses []ConversationResponse
	start := 0
	if limit > 0 && len(history) > limit {
		start = len(history) - limit
	}

	for i := start; i < len(history); i++ {
		entry := history[i]
		response := ConversationResponse{
			Response:  entry.AIResponse,
			SessionID: sessionID,
			Timestamp: entry.Timestamp,
			Metadata: map[string]interface{}{
				"user_message": entry.UserMessage,
				"intent":       entry.Intent,
				"action":       entry.Action,
				"result":       entry.Result,
			},
		}
		responses = append(responses, response)
	}

	return responses, nil
}

// AddEntry adds a new conversation entry
func (cm *ConversationMemory) AddEntry(ctx context.Context, sessionID string, userMessage string, aiResponse string, intent *Intent, action *Action, result *ActionResult) error {
	contextData := map[string]interface{}{
		"last_user_message": userMessage,
		"last_ai_response":  aiResponse,
		"last_intent":       intent,
		"last_action":       action,
		"last_result":       result,
		"timestamp":         time.Now(),
	}

	return cm.SaveContext(ctx, sessionID, contextData)
}

// GetSessionSummary returns a summary of the conversation session
func (cm *ConversationMemory) GetSessionSummary(ctx context.Context, sessionID string) (map[string]interface{}, error) {
	context, err := cm.GetContext(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get conversation context: %w", err)
	}

	history, ok := context["conversation_history"].([]ConversationEntry)
	if !ok {
		history = []ConversationEntry{}
	}

	// Calculate summary statistics
	summary := map[string]interface{}{
		"session_id":          sessionID,
		"total_exchanges":     len(history),
		"first_message_time":  nil,
		"last_message_time":   nil,
		"intent_distribution": make(map[string]int),
		"action_distribution": make(map[string]int),
		"average_confidence":  0.0,
		"context_data_keys":   len(context["context_data"].(map[string]interface{})),
	}

	if len(history) > 0 {
		summary["first_message_time"] = history[0].Timestamp
		summary["last_message_time"] = history[len(history)-1].Timestamp

		// Calculate intent and action distributions
		intentDist := make(map[string]int)
		actionDist := make(map[string]int)
		totalConfidence := 0.0
		confidenceCount := 0

		for _, entry := range history {
			if entry.Intent != nil {
				intentDist[entry.Intent.Type]++
				totalConfidence += entry.Intent.Confidence
				confidenceCount++
			}
			if entry.Action != nil {
				actionDist[entry.Action.Type]++
			}
		}

		summary["intent_distribution"] = intentDist
		summary["action_distribution"] = actionDist
		if confidenceCount > 0 {
			summary["average_confidence"] = totalConfidence / float64(confidenceCount)
		}
	}

	return summary, nil
}

// ClearSession clears all data for a session
func (cm *ConversationMemory) ClearSession(ctx context.Context, sessionID string) error {
	key := fmt.Sprintf("conversation_context:%s", sessionID)

	err := cm.redis.Del(ctx, key).Err()
	if err != nil {
		return fmt.Errorf("failed to clear session: %w", err)
	}

	log.Printf("🗑️ [CONVERSATION-MEMORY] Cleared session: %s", sessionID)
	return nil
}

// GetActiveSessions returns a list of active session IDs
func (cm *ConversationMemory) GetActiveSessions(ctx context.Context) ([]string, error) {
	pattern := "conversation_context:*"
	keys, err := cm.redis.Keys(ctx, pattern).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get session keys: %w", err)
	}

	var sessions []string
	for _, key := range keys {
		// Extract session ID from key
		if len(key) > len("conversation_context:") {
			sessionID := key[len("conversation_context:"):]
			sessions = append(sessions, sessionID)
		}
	}

	return sessions, nil
}

// CleanupOldSessions removes sessions older than the specified duration
func (cm *ConversationMemory) CleanupOldSessions(ctx context.Context, olderThan time.Duration) error {
	pattern := "conversation_context:*"
	keys, err := cm.redis.Keys(ctx, pattern).Result()
	if err != nil {
		return fmt.Errorf("failed to get session keys: %w", err)
	}

	cutoff := time.Now().Add(-olderThan)
	deleted := 0

	for _, key := range keys {
		data, err := cm.redis.Get(ctx, key).Result()
		if err != nil {
			continue
		}

		var context ConversationContext
		err = json.Unmarshal([]byte(data), &context)
		if err != nil {
			continue
		}

		if context.UpdatedAt.Before(cutoff) {
			err = cm.redis.Del(ctx, key).Err()
			if err == nil {
				deleted++
			}
		}
	}

	log.Printf("🧹 [CONVERSATION-MEMORY] Cleaned up %d old sessions", deleted)
	return nil
}

// GetContextData retrieves specific context data
func (cm *ConversationMemory) GetContextData(ctx context.Context, sessionID string, key string) (interface{}, error) {
	context, err := cm.GetContext(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get conversation context: %w", err)
	}

	contextData, ok := context["context_data"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("no context data available")
	}

	value, exists := contextData[key]
	if !exists {
		return nil, fmt.Errorf("key not found: %s", key)
	}

	return value, nil
}

// SetContextData sets specific context data
func (cm *ConversationMemory) SetContextData(ctx context.Context, sessionID string, key string, value interface{}) error {
	context, err := cm.GetContext(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("failed to get conversation context: %w", err)
	}

	contextData, ok := context["context_data"].(map[string]interface{})
	if !ok {
		contextData = make(map[string]interface{})
	}

	contextData[key] = value

	updateData := map[string]interface{}{
		"context_data": contextData,
	}

	return cm.SaveContext(ctx, sessionID, updateData)
}
