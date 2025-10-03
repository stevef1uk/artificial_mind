package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"eventbus"
)

func main() {
	url := getenv("NATS_URL", "nats://127.0.0.1:4222")
	subject := getenv("NATS_SUBJECT", "agi.events.input")

	bus, err := eventbus.NewNATSBus(eventbus.NATSConfig{URL: url, Subject: subject})
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to connect NATS: %v\n", err)
		os.Exit(1)
	}

	evt := eventbus.CanonicalEvent{
		EventID:   eventbus.NewEventID("evt_", time.Now()),
		Source:    "user:alice",
		Type:      "user_message",
		Timestamp: time.Now().UTC(),
		Context:   eventbus.EventContext{Channel: "chat", SessionID: "sess_abc123", ProjectID: "project_42"},
		Payload:   eventbus.EventPayload{Text: "Please generate a summary of the latest logs.", Attachments: []string{}, Metadata: map[string]any{}},
		Security:  eventbus.EventSecurity{Sensitivity: "low", OriginAuth: "api_key_****"},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := bus.Publish(ctx, evt); err != nil {
		fmt.Fprintf(os.Stderr, "publish error: %v\n", err)
		os.Exit(1)
	}
	b, _ := json.MarshalIndent(evt, "", "  ")
	fmt.Printf("published to %s\n%s\n", subject, string(b))
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
