package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// WorkingMemoryManager manages short-term/working memory per session using Redis TTLs.
type WorkingMemoryManager struct {
	redisClient *redis.Client
	// defaultTTLHours controls expiration for keys (hours)
	defaultTTLHours int
}

func NewWorkingMemoryManager(addr string, ttlHours int) *WorkingMemoryManager {
	client := redis.NewClient(&redis.Options{Addr: addr})
	return &WorkingMemoryManager{redisClient: client, defaultTTLHours: ttlHours}
}

// AddEvent appends an event JSON to the session's recent events list and trims to maxN.
func (m *WorkingMemoryManager) AddEvent(sessionID string, event interface{}, maxN int) error {
	ctx := context.Background()
	key := fmt.Sprintf("session:%s:events", sessionID)
	b, err := json.Marshal(event)
	if err != nil {
		return err
	}
	if err := m.redisClient.LPush(ctx, key, string(b)).Err(); err != nil {
		return err
	}
	if maxN > 0 {
		_ = m.redisClient.LTrim(ctx, key, 0, int64(maxN-1)).Err()
	}
	_ = m.redisClient.Expire(ctx, key, time.Duration(m.defaultTTLHours)*time.Hour).Err()
	return nil
}

// SetLatestPlan stores the latest plan JSON for the session.
func (m *WorkingMemoryManager) SetLatestPlan(sessionID string, plan interface{}) error {
	ctx := context.Background()
	key := fmt.Sprintf("session:%s:latest_plan", sessionID)
	b, err := json.Marshal(plan)
	if err != nil {
		return err
	}
	if err := m.redisClient.Set(ctx, key, string(b), time.Duration(m.defaultTTLHours)*time.Hour).Err(); err != nil {
		return err
	}
	return nil
}

// SetLocalVariables stores a flat map of local variables for the session.
func (m *WorkingMemoryManager) SetLocalVariables(sessionID string, vars map[string]string) error {
	ctx := context.Background()
	key := fmt.Sprintf("session:%s:locals", sessionID)
	if vars == nil {
		vars = map[string]string{}
	}
	if err := m.redisClient.HSet(ctx, key, vars).Err(); err != nil {
		return err
	}
	_ = m.redisClient.Expire(ctx, key, time.Duration(m.defaultTTLHours)*time.Hour).Err()
	return nil
}

// WorkingMemory aggregates the short-term memory view.
type WorkingMemory struct {
	SessionID    string                   `json:"session_id"`
	RecentEvents []map[string]interface{} `json:"recent_events"`
	LatestPlan   map[string]interface{}   `json:"latest_plan"`
	LocalVars    map[string]string        `json:"local_vars"`
}

// GetWorkingMemory retrieves recent events (up to n), latest plan, and local variables.
func (m *WorkingMemoryManager) GetWorkingMemory(sessionID string, n int) (*WorkingMemory, error) {
	ctx := context.Background()
	if n <= 0 {
		n = 50
	}
	eventsKey := fmt.Sprintf("session:%s:events", sessionID)
	planKey := fmt.Sprintf("session:%s:latest_plan", sessionID)
	localsKey := fmt.Sprintf("session:%s:locals", sessionID)

	// Fetch in parallel using pipeline
	pipe := m.redisClient.Pipeline()
	lrange := pipe.LRange(ctx, eventsKey, 0, int64(n-1))
	getPlan := pipe.Get(ctx, planKey)
	hgetall := pipe.HGetAll(ctx, localsKey)
	_, err := pipe.Exec(ctx)
	if err != nil && err != redis.Nil {
		// Continue best-effort for partial data when redis.Nil
		return nil, err
	}

	// Parse events
	events := []map[string]interface{}{}
	if evs, err := lrange.Result(); err == nil {
		for _, s := range evs {
			var obj map[string]interface{}
			if json.Unmarshal([]byte(s), &obj) == nil {
				events = append(events, obj)
			}
		}
	}

	// Parse plan
	var plan map[string]interface{}
	if ps, err := getPlan.Result(); err == nil && ps != "" {
		_ = json.Unmarshal([]byte(ps), &plan)
	}

	// Locals
	locals := map[string]string{}
	if m, err := hgetall.Result(); err == nil && len(m) > 0 {
		locals = m
	}

	return &WorkingMemory{
		SessionID:    sessionID,
		RecentEvents: events,
		LatestPlan:   plan,
		LocalVars:    locals,
	}, nil
}
