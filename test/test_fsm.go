package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"
)

// FSMTestClient tests FSM state transitions
type FSMTestClient struct {
	nc    *nats.Conn
	redis *redis.Client
	ctx   context.Context
}

// NewFSMTestClient creates a new test client
func NewFSMTestClient() (*FSMTestClient, error) {
	// Connect to NATS
	nc, err := nats.Connect("nats://localhost:4222")
	if err != nil {
		return nil, fmt.Errorf("failed to connect to NATS: %w", err)
	}

	// Connect to Redis
	opt, err := redis.ParseURL("redis://localhost:6379")
	if err != nil {
		return nil, fmt.Errorf("failed to parse Redis URL: %w", err)
	}
	rdb := redis.NewClient(opt)

	// Test connections
	ctx := context.Background()
	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	return &FSMTestClient{
		nc:    nc,
		redis: rdb,
		ctx:   ctx,
	}, nil
}

// GetFSMState gets the current FSM state
func (c *FSMTestClient) GetFSMState() (string, error) {
	data, err := c.redis.Get(c.ctx, "fsm:agent_1:state").Result()
	if err != nil {
		return "", err
	}

	var stateData map[string]interface{}
	if err := json.Unmarshal([]byte(data), &stateData); err != nil {
		return "", err
	}

	if state, ok := stateData["state"].(string); ok {
		return state, nil
	}

	return "", fmt.Errorf("no state found in data")
}

// SendEvent sends a NATS event to the FSM
func (c *FSMTestClient) SendEvent(eventType, subject string, payload map[string]interface{}) error {
	event := map[string]interface{}{
		"event_id":  fmt.Sprintf("test_%d", time.Now().UnixNano()),
		"source":    "test_client",
		"type":      eventType,
		"timestamp": time.Now().Format(time.RFC3339),
		"context":   map[string]interface{}{"test": true},
		"payload":   payload,
		"security":  map[string]interface{}{"sensitivity": "low"},
	}

	data, err := json.Marshal(event)
	if err != nil {
		return err
	}

	return c.nc.Publish(subject, data)
}

// WaitForStateChange waits for the FSM to change to a specific state
func (c *FSMTestClient) WaitForStateChange(targetState string, timeout time.Duration) error {
	start := time.Now()
	for time.Since(start) < timeout {
		state, err := c.GetFSMState()
		if err != nil {
			log.Printf("Error getting state: %v", err)
			time.Sleep(100 * time.Millisecond)
			continue
		}

		if state == targetState {
			return nil
		}

		log.Printf("Current state: %s (waiting for %s)", state, targetState)
		time.Sleep(100 * time.Millisecond)
	}

	return fmt.Errorf("timeout waiting for state change to %s", targetState)
}

// TestFSMTransitions tests various FSM transitions
func (c *FSMTestClient) TestFSMTransitions() error {
	log.Println("🧪 Starting FSM Transition Tests")
	log.Println("================================")

	// Test 1: Send new_input event and expect transition to perceive
	log.Println("\n📝 Test 1: Send new_input event")

	// Get initial state
	initialState, err := c.GetFSMState()
	if err != nil {
		return fmt.Errorf("failed to get initial state: %w", err)
	}
	log.Printf("Initial state: %s", initialState)

	// Send new_input event
	err = c.SendEvent("user_message", "agi.events.input", map[string]interface{}{
		"text": "Test message for FSM transition",
	})
	if err != nil {
		return fmt.Errorf("failed to send new_input event: %w", err)
	}
	log.Println("✅ Sent new_input event")

	// Wait for state change
	err = c.WaitForStateChange("perceive", 5*time.Second)
	if err != nil {
		log.Printf("❌ Failed to transition to perceive: %v", err)
	} else {
		log.Println("✅ Successfully transitioned to perceive state")
	}

	// Test 2: Send another new_input event while in perceive state
	log.Println("\n📝 Test 2: Send new_input event while in perceive state")

	err = c.SendEvent("user_message", "agi.events.input", map[string]interface{}{
		"text": "Another test message while in perceive state",
	})
	if err != nil {
		return fmt.Errorf("failed to send second new_input event: %w", err)
	}
	log.Println("✅ Sent second new_input event")

	// Wait a bit to see if it processes
	time.Sleep(2 * time.Second)

	currentState, err := c.GetFSMState()
	if err != nil {
		log.Printf("Error getting current state: %v", err)
	} else {
		log.Printf("Current state after second event: %s", currentState)
	}

	// Test 3: Send timer_tick event
	log.Println("\n📝 Test 3: Send timer_tick event")

	err = c.SendEvent("timer_tick", "agi.events.timer.tick", map[string]interface{}{})
	if err != nil {
		return fmt.Errorf("failed to send timer_tick event: %w", err)
	}
	log.Println("✅ Sent timer_tick event")

	// Wait a bit to see the effect
	time.Sleep(2 * time.Second)

	currentState, err = c.GetFSMState()
	if err != nil {
		log.Printf("Error getting current state: %v", err)
	} else {
		log.Printf("Current state after timer_tick: %s", currentState)
	}

	log.Println("\n🎉 FSM Transition Tests Complete!")
	return nil
}

// MonitorFSMState monitors FSM state changes in real-time
func (c *FSMTestClient) MonitorFSMState() {
	log.Println("👀 Monitoring FSM state changes...")
	log.Println("Press Ctrl+C to stop")

	lastState := ""
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			state, err := c.GetFSMState()
			if err != nil {
				log.Printf("Error getting state: %v", err)
				continue
			}

			if state != lastState {
				log.Printf("🔄 State changed: %s -> %s", lastState, state)
				lastState = state
			}
		}
	}
}

func main() {
	client, err := NewFSMTestClient()
	if err != nil {
		log.Fatalf("Failed to create test client: %v", err)
	}
	defer client.nc.Close()
	defer client.redis.Close()

	log.Println("🚀 FSM Test Client Started")
	log.Println("==========================")

	// Run the tests
	if err := client.TestFSMTransitions(); err != nil {
		log.Fatalf("Test failed: %v", err)
	}

	// Start monitoring
	client.MonitorFSMState()
}
