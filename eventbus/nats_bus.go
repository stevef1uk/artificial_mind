package eventbus

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
)

// NATSBus provides a lightweight event bus using NATS core subjects.
type NATSBus struct {
	nc      *nats.Conn
	subject string
}

type NATSConfig struct {
	URL     string
	Subject string
}

func NewNATSBus(cfg NATSConfig) (*NATSBus, error) {
	url := cfg.URL
	if url == "" {
		url = nats.DefaultURL
	}
	nc, err := nats.Connect(url,
		nats.Name("agi-eventbus"),
		nats.MaxReconnects(-1),
		nats.ReconnectWait(2*time.Second),
	)
	if err != nil {
		return nil, err
	}
	subject := cfg.Subject
	if subject == "" {
		subject = "agi.events.input"
	}
	return &NATSBus{nc: nc, subject: subject}, nil
}

func (b *NATSBus) Publish(ctx context.Context, evt CanonicalEvent) error {
	if !evt.MinimalValidate() {
		return fmt.Errorf("invalid event: missing required fields")
	}
	data, err := json.Marshal(evt)
	if err != nil {
		return err
	}
	return b.nc.Publish(b.subject, data)
}

func (b *NATSBus) Subscribe(ctx context.Context, handler func(CanonicalEvent)) (*nats.Subscription, error) {
	sub, err := b.nc.Subscribe(b.subject, func(msg *nats.Msg) {
		var evt CanonicalEvent
		if err := json.Unmarshal(msg.Data, &evt); err == nil {
			handler(evt)
		}
	})
	if err != nil {
		return nil, err
	}
	go func() {
		<-ctx.Done()
		_ = sub.Drain()
	}()
	return sub, nil
}
