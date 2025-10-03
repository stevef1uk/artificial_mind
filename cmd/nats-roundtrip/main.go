package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"eventbus"
)

func main() {
	url := getenv("NATS_URL", "nats://127.0.0.1:4222")
	subject := getenv("NATS_SUBJECT", "agi.events.input")
	deadline := flag.Duration("timeout", 5*time.Second, "roundtrip timeout")
	flag.Parse()

	bus, err := eventbus.NewNATSBus(eventbus.NATSConfig{URL: url, Subject: subject})
	if err != nil {
		fmt.Fprintf(os.Stderr, "connect error: %v\n", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), *deadline)
	defer cancel()

	received := make(chan eventbus.CanonicalEvent, 1)
	_, err = bus.Subscribe(ctx, func(evt eventbus.CanonicalEvent) {
		received <- evt
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "subscribe error: %v\n", err)
		os.Exit(1)
	}

	evt := eventbus.CanonicalEvent{
		EventID:   eventbus.NewEventID("evt_", time.Now()),
		Source:    "roundtrip:test",
		Type:      "system_event",
		Timestamp: time.Now().UTC(),
		Context:   eventbus.EventContext{Channel: "test"},
		Payload:   eventbus.EventPayload{Text: "roundtrip"},
		Security:  eventbus.EventSecurity{Sensitivity: "low"},
	}

	if err := bus.Publish(ctx, evt); err != nil {
		fmt.Fprintf(os.Stderr, "publish error: %v\n", err)
		os.Exit(1)
	}

	select {
	case got := <-received:
		ok := validateMatch(evt, got)
		b, _ := json.MarshalIndent(got, "", "  ")
		if !ok {
			fmt.Fprintf(os.Stderr, "mismatch:\n%s\n", string(b))
			os.Exit(2)
		}
		fmt.Printf("roundtrip ok\n%s\n", string(b))
		return
	case <-ctx.Done():
		var cause string
		if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
			cause = ctx.Err().Error()
		} else {
			cause = "deadline exceeded"
		}
		fmt.Fprintf(os.Stderr, "timeout: %s\n", cause)
		os.Exit(3)
	}
}

func validateMatch(a, b eventbus.CanonicalEvent) bool {
	return a.EventID == b.EventID && a.Type == b.Type && a.Source == b.Source
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
