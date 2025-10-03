package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"
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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	_, err = bus.Subscribe(ctx, func(evt eventbus.CanonicalEvent) {
		b, _ := json.MarshalIndent(evt, "", "  ")
		fmt.Printf("[%s] %s %s\n%s\n", time.Now().Format(time.RFC3339), evt.Type, evt.EventID, string(b))
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "subscribe error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("listening on %s\n", subject)

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	<-sigs
	fmt.Println("shutting down")
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
