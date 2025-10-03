package selfmodel

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

var ctx = context.Background()

// Core structures

type Beliefs map[string]interface{}

type Goal struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Status    string    `json:"status"` // pending, active, completed, failed
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Episode struct {
	Timestamp time.Time   `json:"timestamp"`
	Event     string      `json:"event"`
	Decision  string      `json:"decision"`
	Result    string      `json:"result"`
	Success   bool        `json:"success"`
	Metadata  interface{} `json:"metadata,omitempty"`
}

type SelfModel struct {
	Goals   []Goal    `json:"goals"`
	Beliefs Beliefs   `json:"beliefs"`
	History []Episode `json:"history"`
}

// Manager

type Manager struct {
	client *redis.Client
	key    string // Redis key for storing self-model
}

func NewManager(redisAddr string, key string) *Manager {
	rdb := redis.NewClient(&redis.Options{
		Addr: redisAddr,
	})
	return &Manager{
		client: rdb,
		key:    key,
	}
}

// Load state from Redis
func (m *Manager) Load() (*SelfModel, error) {
	data, err := m.client.Get(ctx, m.key).Result()
	if err == redis.Nil {
		return &SelfModel{
			Goals:   []Goal{},
			Beliefs: Beliefs{},
			History: []Episode{},
		}, nil
	} else if err != nil {
		return nil, fmt.Errorf("error loading self model: %w", err)
	}

	var sm SelfModel
	if err := json.Unmarshal([]byte(data), &sm); err != nil {
		return nil, fmt.Errorf("error unmarshalling self model: %w", err)
	}
	return &sm, nil
}

// Save state to Redis
func (m *Manager) Save(sm *SelfModel) error {
	data, err := json.Marshal(sm)
	if err != nil {
		return fmt.Errorf("error marshalling self model: %w", err)
	}
	return m.client.Set(ctx, m.key, data, 0).Err()
}

// High-level operations

func (m *Manager) UpdateBelief(key string, value interface{}) error {
	sm, err := m.Load()
	if err != nil {
		return err
	}
	sm.Beliefs[key] = value
	return m.Save(sm)
}

func (m *Manager) AddGoal(name string) error {
	sm, err := m.Load()
	if err != nil {
		return err
	}
	goal := Goal{
		ID:        fmt.Sprintf("%d", time.Now().UnixNano()),
		Name:      name,
		Status:    "pending",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	sm.Goals = append(sm.Goals, goal)
	return m.Save(sm)
}

func (m *Manager) UpdateGoalStatus(id string, status string) error {
	sm, err := m.Load()
	if err != nil {
		return err
	}
	for i := range sm.Goals {
		if sm.Goals[i].ID == id {
			sm.Goals[i].Status = status
			sm.Goals[i].UpdatedAt = time.Now()
			break
		}
	}
	return m.Save(sm)
}

// DeleteGoal removes a goal by ID from the self-model
func (m *Manager) DeleteGoal(id string) error {
	sm, err := m.Load()
	if err != nil {
		return err
	}
	filtered := make([]Goal, 0, len(sm.Goals))
	for _, g := range sm.Goals {
		if g.ID != id {
			filtered = append(filtered, g)
		}
	}
	sm.Goals = filtered
	return m.Save(sm)
}

func (m *Manager) RecordEpisode(event, decision, result string, success bool, metadata interface{}) error {
	sm, err := m.Load()
	if err != nil {
		return err
	}
	ep := Episode{
		Timestamp: time.Now(),
		Event:     event,
		Decision:  decision,
		Result:    result,
		Success:   success,
		Metadata:  metadata,
	}
	sm.History = append(sm.History, ep)
	return m.Save(sm)
}
