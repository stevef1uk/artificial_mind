package engine

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type Memory struct {
	client *redis.Client
	ttl    time.Duration
}

func NewMemory(addr string, ttlSeconds int) *Memory {
	rdb := redis.NewClient(&redis.Options{
		Addr: addr,
	})
	return &Memory{
		client: rdb,
		ttl:    time.Duration(ttlSeconds) * time.Second,
	}
}

func (m *Memory) hashInputs(inputs map[string]interface{}) string {
	b, _ := json.Marshal(inputs)
	sum := sha256.Sum256(b)
	return fmt.Sprintf("%x", sum)
}

func (m *Memory) Get(action string, inputs map[string]interface{}) (string, bool) {
	key := action + ":" + m.hashInputs(inputs)
	ctx := context.Background()
	val, err := m.client.Get(ctx, key).Result()
	if err == redis.Nil {
		// Key not found in Redis
		return "", false
	} else if err != nil {
		// Redis error - fail instead of using fallback
		fmt.Printf("Redis error: %v\n", err)
		return "", false
	}
	return val, true
}

func (m *Memory) Store(action string, inputs map[string]interface{}, result string) {
	key := action + ":" + m.hashInputs(inputs)
	ctx := context.Background()
	err := m.client.Set(ctx, key, result, m.ttl).Err()
	if err != nil {
		// Redis error - fail instead of using fallback
		fmt.Printf("Redis store error: %v\n", err)
	}
}
