package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"eventbus"
)

type mode string

const (
	modeRaw       mode = "raw"
	modeEntities  mode = "entities"
	modeRelations mode = "relations"
	modeSummary   mode = "summaries"
	modeAlert     mode = "alerts"
	modeSentiment mode = "sentiment"
)

func main() {
	url := getenv("NATS_URL", "nats://127.0.0.1:4222")
	src := getenv("NEWS_SOURCE", "bbc")
	m := mode(getenv("NEWS_MODE", string(modeRelations)))

	subject := subjectForMode(m)
	bus, err := eventbus.NewNATSBus(eventbus.NATSConfig{URL: url, Subject: subject})
	if err != nil {
		fmt.Fprintf(os.Stderr, "connect error: %v\n", err)
		os.Exit(1)
	}

	// CLI flags for quick ad-hoc publishing
	headline := flag.String("headline", "Scientists warn Arctic sea ice shrinking faster", "headline for raw article")
	body := flag.String("body", "...", "body for raw article")
	topic := flag.String("topic", "climate", "topic for summary/alert/sentiment")
	relation := flag.String("relation", "is_shrinking_faster_than", "relation verb")
	head := flag.String("head", "Arctic sea ice", "relation head")
	tail := flag.String("tail", "climate models predicted", "relation tail")
	impact := flag.String("impact", "high", "impact for alerts")
	sentiment := flag.Float64("sent", -0.2, "sentiment score")
	flag.Parse()

	now := time.Now().UTC()
	evt := eventbus.CanonicalEvent{
		EventID:   eventbus.NewEventID("evt_", now),
		Source:    "news:" + src,
		Type:      string(m),
		Timestamp: now,
		Context:   eventbus.EventContext{Channel: "news"},
		Payload:   payloadForMode(m, src, *headline, *body, *topic, *relation, *head, *tail, *impact, *sentiment, now),
		Security:  eventbus.EventSecurity{Sensitivity: "low"},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := bus.Publish(ctx, evt); err != nil {
		fmt.Fprintf(os.Stderr, "publish error: %v\n", err)
		os.Exit(2)
	}
	b, _ := json.MarshalIndent(evt, "", "  ")
	fmt.Printf("published to %s\n%s\n", subject, string(b))
}

func subjectForMode(m mode) string {
	return "agi.events.news." + string(m)
}

func payloadForMode(m mode, src, headline, body, topic, relation, head, tail, impact string, sent float64, t time.Time) eventbus.EventPayload {
	md := map[string]any{"source": src, "timestamp": t.Format(time.RFC3339)}
	switch m {
	case modeRaw:
		md["headline"] = headline
		md["url"] = "https://example.com/article"
		return eventbus.EventPayload{Text: body, Metadata: md}
	case modeEntities:
		md["entities"] = []string{"AI", "climate change", "OpenAI"}
		return eventbus.EventPayload{Text: strings.Join(md["entities"].([]string), ", "), Metadata: md}
	case modeRelations:
		md["id"] = "rel_" + t.Format("20060102_150405")
		md["head"] = head
		md["relation"] = relation
		md["tail"] = tail
		md["confidence"] = 0.87
		return eventbus.EventPayload{Text: fmt.Sprintf("%s %s %s", head, relation, tail), Metadata: md}
	case modeSummary:
		md["topic"] = topic
		md["summary"] = "UN report warns of faster warming"
		md["confidence"] = 0.9
		md["sources"] = []string{"bbc", "guardian"}
		return eventbus.EventPayload{Text: md["summary"].(string), Metadata: md}
	case modeAlert:
		md["alert_type"] = "breaking"
		md["topic"] = topic
		md["impact"] = impact
		md["confidence"] = 0.95
		return eventbus.EventPayload{Text: fmt.Sprintf("%s alert on %s (impact %s)", md["alert_type"], topic, impact), Metadata: md}
	case modeSentiment:
		md["topic"] = topic
		md["sentiment"] = sent
		md["region"] = "UK"
		md["sources"] = []string{src}
		return eventbus.EventPayload{Text: fmt.Sprintf("sentiment %0.2f for %s", sent, topic), Metadata: md}
	default:
		return eventbus.EventPayload{Text: "unknown mode", Metadata: md}
	}
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
