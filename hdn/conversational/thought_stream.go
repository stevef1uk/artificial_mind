package conversational

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"
)

// ThoughtStreamService manages real-time thought streaming via NATS
type ThoughtStreamService struct {
	nc       *nats.Conn
	redis    *redis.Client
	subs     map[string]*nats.Subscription
	mu       sync.RWMutex
	handlers map[string]ThoughtEventHandler
}

// ThoughtEventHandler handles incoming thought events
type ThoughtEventHandler interface {
	HandleThoughtEvent(ctx context.Context, event ThoughtEvent) error
}

// ThoughtStreamConfig contains configuration for thought streaming
type ThoughtStreamConfig struct {
	SubjectPrefix string        `json:"subject_prefix"`
	BufferSize    int           `json:"buffer_size"`
	TTL           time.Duration `json:"ttl"`
}

// NewThoughtStreamService creates a new thought stream service
func NewThoughtStreamService(nc *nats.Conn, redis *redis.Client) *ThoughtStreamService {
	return &ThoughtStreamService{
		nc:       nc,
		redis:    redis,
		subs:     make(map[string]*nats.Subscription),
		handlers: make(map[string]ThoughtEventHandler),
	}
}

// StartListening starts listening for thought events
func (ts *ThoughtStreamService) StartListening(ctx context.Context, config *ThoughtStreamConfig) error {
	if config == nil {
		config = &ThoughtStreamConfig{
			SubjectPrefix: "agi.events.fsm.thought",
			BufferSize:    1000,
			TTL:           24 * time.Hour,
		}
	}

	// Subscribe to thought events
	subject := config.SubjectPrefix
	sub, err := ts.nc.Subscribe(subject, func(m *nats.Msg) {
		ts.handleThoughtMessage(ctx, m, config)
	})
	if err != nil {
		return fmt.Errorf("failed to subscribe to thought events: %w", err)
	}

	ts.mu.Lock()
	ts.subs[subject] = sub
	ts.mu.Unlock()

	log.Printf("🧠 [THOUGHT-STREAM] Started listening for thought events on subject: %s", subject)
	return nil
}

// StopListening stops listening for thought events
func (ts *ThoughtStreamService) StopListening() {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	for subject, sub := range ts.subs {
		sub.Unsubscribe()
		delete(ts.subs, subject)
		log.Printf("🧠 [THOUGHT-STREAM] Stopped listening on subject: %s", subject)
	}
}

// RegisterHandler registers a thought event handler
func (ts *ThoughtStreamService) RegisterHandler(name string, handler ThoughtEventHandler) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.handlers[name] = handler
	log.Printf("🧠 [THOUGHT-STREAM] Registered handler: %s", name)
}

// UnregisterHandler unregisters a thought event handler
func (ts *ThoughtStreamService) UnregisterHandler(name string) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	delete(ts.handlers, name)
	log.Printf("🧠 [THOUGHT-STREAM] Unregistered handler: %s", name)
}

// handleThoughtMessage processes incoming thought messages
func (ts *ThoughtStreamService) handleThoughtMessage(ctx context.Context, m *nats.Msg, config *ThoughtStreamConfig) {
	var event ThoughtEvent
	if err := json.Unmarshal(m.Data, &event); err != nil {
		log.Printf("❌ [THOUGHT-STREAM] Failed to unmarshal thought event: %v", err)
		return
	}

	// Store thought event in Redis
	if err := ts.storeThoughtEvent(ctx, &event, config.TTL); err != nil {
		log.Printf("⚠️ [THOUGHT-STREAM] Failed to store thought event: %v", err)
	}

	// Notify handlers
	ts.mu.RLock()
	handlers := make([]ThoughtEventHandler, 0, len(ts.handlers))
	for _, handler := range ts.handlers {
		handlers = append(handlers, handler)
	}
	ts.mu.RUnlock()

	// Call handlers asynchronously
	for _, handler := range handlers {
		go func(h ThoughtEventHandler) {
			if err := h.HandleThoughtEvent(ctx, event); err != nil {
				log.Printf("❌ [THOUGHT-STREAM] Handler error: %v", err)
			}
		}(handler)
	}

	log.Printf("🧠 [THOUGHT-STREAM] Processed thought event: %s (session: %s)", event.Type, event.SessionID)
}

// storeThoughtEvent stores a thought event in Redis
func (ts *ThoughtStreamService) storeThoughtEvent(ctx context.Context, event *ThoughtEvent, ttl time.Duration) error {
	key := fmt.Sprintf("thought_events:%s:%s", event.SessionID, event.Timestamp)

	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal thought event: %w", err)
	}

	err = ts.redis.Set(ctx, key, data, ttl).Err()
	if err != nil {
		return fmt.Errorf("failed to store thought event: %w", err)
	}

	return nil
}

// PublishThoughtEvent publishes a thought event
func (ts *ThoughtStreamService) PublishThoughtEvent(event *ThoughtEvent) error {
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal thought event: %w", err)
	}

	subject := "agi.events.fsm.thought"
	err = ts.nc.Publish(subject, data)
	if err != nil {
		return fmt.Errorf("failed to publish thought event: %w", err)
	}

	log.Printf("🧠 [THOUGHT-STREAM] Published thought event: %s (session: %s)", event.Type, event.SessionID)
	return nil
}

// GetThoughtEvents retrieves thought events for a session
func (ts *ThoughtStreamService) GetThoughtEvents(ctx context.Context, sessionID string, limit int) ([]ThoughtEvent, error) {
	pattern := fmt.Sprintf("thought_events:%s:*", sessionID)
	keys, err := ts.redis.Keys(ctx, pattern).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get thought event keys: %w", err)
	}

	var events []ThoughtEvent
	for i, key := range keys {
		if i >= limit {
			break
		}

		data, err := ts.redis.Get(ctx, key).Result()
		if err != nil {
			log.Printf("⚠️ [THOUGHT-STREAM] Failed to load thought event %s: %v", key, err)
			continue
		}

		var event ThoughtEvent
		err = json.Unmarshal([]byte(data), &event)
		if err != nil {
			log.Printf("❌ [THOUGHT-STREAM] Failed to unmarshal thought event %s: %v", key, err)
			continue
		}

		events = append(events, event)
	}

	return events, nil
}

// GetRecentThoughtEvents retrieves recent thought events across all sessions
func (ts *ThoughtStreamService) GetRecentThoughtEvents(ctx context.Context, limit int) ([]ThoughtEvent, error) {
	pattern := "thought_events:*"
	keys, err := ts.redis.Keys(ctx, pattern).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get thought event keys: %w", err)
	}

	var events []ThoughtEvent
	for i, key := range keys {
		if i >= limit {
			break
		}

		data, err := ts.redis.Get(ctx, key).Result()
		if err != nil {
			continue
		}

		var event ThoughtEvent
		err = json.Unmarshal([]byte(data), &event)
		if err != nil {
			continue
		}

		events = append(events, event)
	}

	return events, nil
}

// CleanupOldEvents removes old thought events
func (ts *ThoughtStreamService) CleanupOldEvents(ctx context.Context, olderThan time.Duration) error {
	pattern := "thought_events:*"
	keys, err := ts.redis.Keys(ctx, pattern).Result()
	if err != nil {
		return fmt.Errorf("failed to get thought event keys: %w", err)
	}

	cutoff := time.Now().Add(-olderThan)
	deleted := 0

	for _, key := range keys {
		data, err := ts.redis.Get(ctx, key).Result()
		if err != nil {
			continue
		}

		var event ThoughtEvent
		err = json.Unmarshal([]byte(data), &event)
		if err != nil {
			continue
		}

		// Parse timestamp to check age
		eventTime, err := time.Parse(time.RFC3339, event.Timestamp)
		if err != nil {
			continue
		}

		if eventTime.Before(cutoff) {
			err = ts.redis.Del(ctx, key).Err()
			if err == nil {
				deleted++
			}
		}
	}

	log.Printf("🧠 [THOUGHT-STREAM] Cleaned up %d old thought events", deleted)
	return nil
}

// MonitorThoughtStream provides monitoring information about the thought stream
func (ts *ThoughtStreamService) MonitorThoughtStream(ctx context.Context) (map[string]interface{}, error) {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	stats := map[string]interface{}{
		"active_subscriptions": len(ts.subs),
		"registered_handlers":  len(ts.handlers),
		"subjects":             make([]string, 0, len(ts.subs)),
	}

	for subject := range ts.subs {
		stats["subjects"] = append(stats["subjects"].([]string), subject)
	}

	// Get recent event count
	recentEvents, err := ts.GetRecentThoughtEvents(ctx, 100)
	if err == nil {
		stats["recent_events_count"] = len(recentEvents)
	}

	return stats, nil
}
