package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"async_llm"
	"eventbus"

	"github.com/redis/go-redis/v9"
)

// Minimal scraper leveraging BBC front page structure; falls back to link discovery.

type Article struct {
	Title string
	URL   string
}

// Redis client for persistent duplicate detection
var redisClient *redis.Client

// initRedis initializes Redis connection
func initRedis() error {
	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		redisURL = "redis://redis.agi.svc.cluster.local:6379"
	}

	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		return fmt.Errorf("failed to parse Redis URL: %w", err)
	}

	redisClient = redis.NewClient(opt)

	// Test connection
	ctx := context.Background()
	if err := redisClient.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("failed to connect to Redis: %w", err)
	}

	return nil
}

// isDuplicate checks if an article has been processed before
func isDuplicate(title, url string) (bool, error) {
	if redisClient == nil {
		return false, nil // If Redis is not available, don't skip
	}

	ctx := context.Background()

	// Create a hash key based on title and URL
	key := fmt.Sprintf("news:duplicates:%s", url)

	// Check if the key exists
	exists, err := redisClient.Exists(ctx, key).Result()
	if err != nil {
		return false, err
	}

	if exists > 0 {
		return true, nil // Article already processed
	}

	// Mark as processed with 24-hour expiration
	err = redisClient.Set(ctx, key, title, 24*time.Hour).Err()
	if err != nil {
		return false, err
	}

	return false, nil // Not a duplicate
}

func main() {
	base := flag.String("url", "https://www.bbc.com/news", "BBC News URL to scrape")
	max := flag.Int("max", 15, "max stories to process")
	dry := flag.Bool("dry", false, "print decisions without publishing")
	debug := flag.Bool("debug", false, "verbose discovery debug output")
	useLLM := flag.Bool("llm", false, "use LLM to classify headlines in batches")
	batchSize := flag.Int("batch-size", 10, "LLM batch size")
	llmModel := flag.String("llm-model", getenvFirst([]string{"LLM_MODEL", "OLLAMA_MODEL"}, "llama3.1"), "LLM model name (LLM_MODEL or OLLAMA_MODEL)")
	ollamaURL := flag.String("ollama-url", getenvFirst([]string{"LLM_ENDPOINT", "LLM_URL", "OLLAMA_URL"}, "http://localhost:11434/api/chat"), "LLM endpoint / Ollama chat API URL")
	flag.Parse()

	stories, err := discoverStories(*base, *max)
	if err != nil {
		fmt.Fprintf(os.Stderr, "discover error: %v\n", err)
		os.Exit(1)
	}
	if *debug {
		fmt.Fprintf(os.Stderr, "discovered %d BBC stories from %s\n", len(stories), *base)
		for i, s := range stories {
			if i >= 10 {
				break
			}
			fmt.Fprintf(os.Stderr, "[%02d] %s\n     %s\n", i+1, s.Title, s.URL)
		}
	}
	if len(stories) == 0 {
		fmt.Fprintf(os.Stderr, "no stories discovered; try -debug and a larger -max (e.g., 50)\n")
		if *dry {
			return
		}
	}

	// Initialize Redis for duplicate detection
	if err := initRedis(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Failed to initialize Redis: %v\n", err)
		fmt.Fprintf(os.Stderr, "Continuing without duplicate detection...\n")
	}

	// Filter out duplicates before processing
	var filteredStories []Article
	for _, story := range stories {
		isDup, err := isDuplicate(story.Title, story.URL)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: Failed to check duplicate for %s: %v\n", story.Title, err)
			// Continue processing if duplicate check fails
		}
		if isDup {
			if *debug {
				fmt.Fprintf(os.Stderr, "skip duplicate: %s\n", story.Title)
			}
			continue
		}
		filteredStories = append(filteredStories, story)
	}

	if *debug {
		fmt.Fprintf(os.Stderr, "Processing %d stories (filtered from %d)\n", len(filteredStories), len(stories))
	}

	if len(filteredStories) == 0 {
		fmt.Fprintf(os.Stderr, "no new stories to process after duplicate filtering\n")
		if *dry {
			return
		}
	}

	// Prepare publishers for relations and alerts
	natsURL := getenv("NATS_URL", "nats://127.0.0.1:4222")
	relBus, err := eventbus.NewNATSBus(eventbus.NATSConfig{URL: natsURL, Subject: "agi.events.news.relations"})
	if err != nil {
		fmt.Fprintf(os.Stderr, "nats connect (relations) error: %v\n", err)
		os.Exit(2)
	}
	alertBus, err := eventbus.NewNATSBus(eventbus.NATSConfig{URL: natsURL, Subject: "agi.events.news.alerts"})
	if err != nil {
		fmt.Fprintf(os.Stderr, "nats connect (alerts) error: %v\n", err)
		os.Exit(2)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if *useLLM {
		processWithLLM(filteredStories, *batchSize, *llmModel, *ollamaURL, *dry, *debug, alertBus, relBus, ctx)
		return
	}

	for _, s := range filteredStories {
		title := normalizeHeadline(s.Title)
		decision := classifyStory(title)
		debugLogDecision(decision, title, *debug, "")
		_ = publishHeuristicDecision(decision, title, s, *dry, *debug, alertBus, relBus, ctx)
	}
}

// processWithLLM classifies headlines in batches using a local LLM (Ollama HTTP API).
func processWithLLM(stories []Article, batch int, model string, apiURL string, dry, debug bool, alertBus, relBus *eventbus.NATSBus, ctx context.Context) {
	if batch <= 0 {
		batch = 10
	}

	// Duplicate detection is now handled at the main level

	for i := 0; i < len(stories); i += batch {
		end := i + batch
		if end > len(stories) {
			end = len(stories)
		}
		chunk := stories[i:end]
		var b strings.Builder
		b.WriteString("You are classifying BBC news headlines. For each headline respond with ONE JSON object.\n")
		b.WriteString("Allowed values:\n")
		b.WriteString("- type: \"alert\", \"relation\", or \"skip\"\n")
		b.WriteString("- impact: \"low\", \"medium\", \"high\" (only when type=\"alert\")\n")
		b.WriteString("- head, relation, tail: ONLY when type=\"relation\". Copy phrases verbatim from the headline.\n")
		b.WriteString("- reason: brief justification (required for all three types).\n\n")
		b.WriteString("ABSOLUTE RULES:\n")
		b.WriteString("1. Never invent words. Every head / relation / tail must be contiguous text exactly as it appears in the headline.\n")
		b.WriteString("2. If you cannot find a clear actor + verb + object in the headline, output type=\"skip\".\n")
		b.WriteString("3. Quotes, colons, or section prefixes do NOT create actors by themselves. If unsure, skip.\n")
		b.WriteString("4. Output exactly ")
		b.WriteString(fmt.Sprintf("%d", len(chunk)))
		b.WriteString(" lines of JSONL. No markdown, no numbering, no commentary.\n\nHeadlines:\n")
		for idx, a := range chunk {
			b.WriteString(fmt.Sprintf("%d. %s\n", idx+1, normalizeHeadline(a.Title)))
		}
		b.WriteString(fmt.Sprintf("\nOutput format (exactly %d lines of JSONL, no code fences, no extra text):\n", len(chunk)))
		b.WriteString("{\"type\":\"alert\",\"impact\":\"high\",\"reason\":\"urgent breaking news\"}\n")
		b.WriteString("{\"type\":\"relation\",\"head\":\"Actor\",\"relation\":\"action\",\"tail\":\"target\",\"reason\":\"clear subject-verb-object structure\"}\n")
		b.WriteString("{\"type\":\"skip\",\"reason\":\"not newsworthy or unclear\"}\n\n")
		b.WriteString("Additional guidance:\n")
		b.WriteString("- Alert: breaking news, emergencies, major policy changes, major legal rulings, major scientific breakthroughs.\n")
		b.WriteString("- Relation: there must be a clearly named actor performing an action on a target, all in the headline text.\n")
		b.WriteString("- Skip: entertainment / opinion / interviews / feature pieces / unclear wording. Prefer skip when uncertain.\n\n")
		b.WriteString("Examples:\n")
		b.WriteString("\"Trump demands inquiry over UN 'triple sabotage'\" -> {\"type\":\"alert\",\"impact\":\"medium\",\"reason\":\"political demand\"}\n")
		b.WriteString("\"South Korea legalises tattooing by non-medical professionals\" -> {\"type\":\"relation\",\"head\":\"South Korea\",\"relation\":\"legalises\",\"tail\":\"tattooing by non-medical professionals\",\"reason\":\"clear policy change\"}\n")
		b.WriteString("\"Sonic the Hedgehog boss on how the series keeps up to speed\" -> {\"type\":\"skip\",\"reason\":\"feature interview\"}\n\n")
		b.WriteString(fmt.Sprintf("REMINDER: respond with %d raw JSON lines only. If a headline does not obviously match alert or relation, respond with skip.", len(chunk)))
		t0 := time.Now()
		resp, err := llmChatWithURL(apiURL, model, b.String())
		if err != nil {
			if debug {
				fmt.Fprintf(os.Stderr, "LLM error: %v\n", err)
			}
			continue
		}
		if debug {
			fmt.Fprintf(os.Stderr, "LLM batch %d-%d took %s\n", i+1, end, time.Since(t0))
		}
		// sanitize common wrappers (code fences)
		sanitized := strings.ReplaceAll(resp, "\r", "")
		sanitized = strings.ReplaceAll(sanitized, "```json", "")
		sanitized = strings.ReplaceAll(sanitized, "```", "")
		if debug {
			fmt.Fprintf(os.Stderr, "LLM response %d-%d:\n%s\n", i+1, end, strings.TrimSpace(sanitized))
		}
		lines := strings.Split(strings.TrimSpace(sanitized), "\n")
		for j, line := range lines {
			if j >= len(chunk) {
				break
			}
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "//") || !strings.HasPrefix(line, "{") {
				if debug && line != "" {
					fmt.Fprintf(os.Stderr, "discard non-json line: %s\n", line)
				}
				continue
			}
			var obj struct{ Type, Head, Relation, Tail, Impact, Reason string }
			if json.Unmarshal([]byte(line), &obj) != nil {
				if debug {
					fmt.Fprintf(os.Stderr, "bad line: %s\n", line)
				}
				continue
			}
			title := normalizeHeadline(chunk[j].Title)
			titleLower := strings.ToLower(title)

			switch strings.ToLower(strings.TrimSpace(obj.Type)) {
			case "alert":
				// Alert gating: more conservative filtering with confidence scoring
				impact := strings.ToLower(strings.TrimSpace(obj.Impact))
				dec := decision{kind: "alert", impact: impact, reason: obj.Reason}
				debugLogDecision(dec, title, debug, "LLM")
				if impact == "" {
					attemptFallback(title, chunk[j], dry, debug, alertBus, relBus, ctx)
					continue
				}
				isUrgent := strings.Contains(titleLower, "breaking") ||
					strings.Contains(titleLower, "urgent") ||
					strings.Contains(titleLower, "emergency") ||
					strings.Contains(titleLower, "crisis")

				// Calculate confidence based on impact and urgency
				confidence := calculateAlertConfidence(impact, isUrgent, title)

				// More conservative filtering: require higher confidence for low impact
				if impact == "low" && !isUrgent && confidence < 0.7 {
					if debug {
						fmt.Fprintf(os.Stderr, "skip alert (low impact + low confidence): %s (impact: %s, confidence: %.2f)\n", title, impact, confidence)
					}
					attemptFallback(title, chunk[j], dry, debug, alertBus, relBus, ctx)
					continue
				}

				// Skip medium impact alerts with very low confidence
				if impact == "medium" && confidence < 0.6 {
					if debug {
						fmt.Fprintf(os.Stderr, "skip alert (medium impact + low confidence): %s (impact: %s, confidence: %.2f)\n", title, impact, confidence)
					}
					attemptFallback(title, chunk[j], dry, debug, alertBus, relBus, ctx)
					continue
				}

				evt := wrapAlert("bbc", title, "", impact, confidence)
				if dry {
					printEvent("agi.events.news.alerts", evt)
					continue
				}
				if err := alertBus.Publish(ctx, evt); err != nil {
					if debug {
						fmt.Fprintf(os.Stderr, "publish alert failed: %v\n", err)
					}
					attemptFallback(title, chunk[j], dry, debug, alertBus, relBus, ctx)
					continue
				}
			case "relation":
				head := strings.TrimSpace(obj.Head)
				rel := strings.TrimSpace(obj.Relation)
				tail := strings.TrimSpace(obj.Tail)
				dec := decision{kind: "relation", head: head, relation: rel, tail: tail, reason: obj.Reason}
				debugLogDecision(dec, title, debug, "LLM")
				if head == "" || rel == "" || tail == "" {
					// Fallback: try heuristic extraction from the title
					if trip := extractRelation(title); trip != nil {
						head, rel, tail = trip[0], trip[1], trip[2]
					} else {
						if debug {
							fmt.Fprintf(os.Stderr, "incomplete relation: %s\n", line)
						}
						attemptFallback(title, chunk[j], dry, debug, alertBus, relBus, ctx)
						continue
					}
				}

				// Ensure phrases appear in the headline text
				if !containsPhrase(titleLower, head) || !containsPhrase(titleLower, tail) || !containsPhrase(titleLower, rel) {
					if debug {
						fmt.Fprintf(os.Stderr, "skip relation (phrases not in headline): head=%q rel=%q tail=%q title=%q\n", head, rel, tail, title)
					}
					attemptFallback(title, chunk[j], dry, debug, alertBus, relBus, ctx)
					continue
				}

				// Section name filtering
				if isSectionName(head) {
					if debug {
						fmt.Fprintf(os.Stderr, "skip section name: %s\n", head)
					}
					attemptFallback(title, chunk[j], dry, debug, alertBus, relBus, ctx)
					continue
				}

				if !looksLikeActor(head) {
					if debug {
						fmt.Fprintf(os.Stderr, "actor check failed: %q\n", head)
					}
					attemptFallback(title, chunk[j], dry, debug, alertBus, relBus, ctx)
					continue
				}
				// Calculate confidence for relations based on content quality
				relationConfidence := calculateRelationConfidence(head, rel, tail, title)

				// Skip relations with very low confidence
				if relationConfidence < 0.6 {
					if debug {
						fmt.Fprintf(os.Stderr, "skip relation (low confidence): %s %s %s (confidence: %.2f)\n", head, rel, tail, relationConfidence)
					}
					attemptFallback(title, chunk[j], dry, debug, alertBus, relBus, ctx)
					continue
				}

				evt := wrapRelation("bbc", head, rel, tail, title, chunk[j].URL, relationConfidence)
				if dry {
					printEvent("agi.events.news.relations", evt)
					continue
				}
				if err := relBus.Publish(ctx, evt); err != nil {
					if debug {
						fmt.Fprintf(os.Stderr, "publish relation failed: %v\n", err)
					}
					attemptFallback(title, chunk[j], dry, debug, alertBus, relBus, ctx)
					continue
				}
			default:
				if debug {
					fmt.Fprintf(os.Stderr, "skip by llm: %s\n", title)
				}
				attemptFallback(title, chunk[j], dry, debug, alertBus, relBus, ctx)
			}
		}
	}
}

// llmChat calls a local Ollama server to get a response for the given prompt.
func llmChat(model, prompt string) (string, error) {
	return llmChatWithURL(getenv("OLLAMA_URL", "http://localhost:11434/api/chat"), model, prompt)
}

func llmChatWithURL(apiURL, model, prompt string) (string, error) {
	ctx := context.Background()
	// Use async LLM client (or sync fallback)
	messages := []map[string]string{{"role": "user", "content": prompt}}
	response, err := async_llm.CallAsync(ctx, "ollama", apiURL, model, prompt, messages, async_llm.PriorityLow)
	if err != nil {
		return "", err
	}
	return response, nil
}

func discoverStories(front string, max int) ([]Article, error) {
	body, err := httpGet(front)
	if err != nil {
		return nil, err
	}

	// Try new BBC JSON structure first (React/Next.js app)
	articles := extractFromNextJSData(body, max)
	if len(articles) > 0 {
		return articles, nil
	}

	// Fallback to old regex-based extraction
	re := regexp.MustCompile(`<a[^>]+href="(/news/[^"]+)"[^>]*>([\s\S]*?)</a>`)
	matches := re.FindAllStringSubmatch(body, max*3)
	seen := map[string]bool{}
	var results []Article
	for _, m := range matches {
		href := htmlUnescape(m[1])
		text := decodeHTMLEntities(stripTags(m[2]))
		if len(strings.Fields(text)) < 3 {
			continue
		}
		abs := toAbs("https://www.bbc.com", href)
		// Only classify true articles; skip hubs/sections
		if !strings.Contains(abs, "/news/articles/") {
			continue
		}
		if seen[abs] {
			continue
		}
		seen[abs] = true
		results = append(results, Article{Title: strings.TrimSpace(text), URL: abs})
		if len(results) >= max {
			break
		}
	}
	return results, nil
}

// extractFromNextJSData extracts articles from BBC's new React/Next.js JSON structure
func extractFromNextJSData(body string, max int) []Article {
	// Look for __NEXT_DATA__ script tag
	re := regexp.MustCompile(`<script id="__NEXT_DATA__" type="application/json">(.*?)</script>`)
	matches := re.FindStringSubmatch(body)
	if len(matches) < 2 {
		return nil
	}

	// Parse the JSON data
	var nextData struct {
		Props struct {
			PageProps struct {
				Page struct {
					News struct {
						Sections []struct {
							Content []struct {
								Title    string `json:"title"`
								Href     string `json:"href"`
								Metadata struct {
									ContentType string `json:"contentType"`
									Subtype     string `json:"subtype"`
								} `json:"metadata"`
							} `json:"content"`
						} `json:"sections"`
					} `json:"news"`
				} `json:"page"`
			} `json:"pageProps"`
		} `json:"props"`
	}

	if err := json.Unmarshal([]byte(matches[1]), &nextData); err != nil {
		return nil
	}

	seen := map[string]bool{}
	var results []Article

	// Extract articles from all sections
	for _, section := range nextData.Props.PageProps.Page.News.Sections {
		for _, content := range section.Content {
			// Only process news articles
			if content.Metadata.ContentType != "article" || content.Metadata.Subtype != "news" {
				continue
			}

			// Skip if not a news article URL
			if !strings.Contains(content.Href, "/news/articles/") {
				continue
			}

			// Skip if already seen
			abs := toAbs("https://www.bbc.com", content.Href)
			if seen[abs] {
				continue
			}

			// Skip if title is too short
			if len(strings.Fields(content.Title)) < 3 {
				continue
			}

			seen[abs] = true
			results = append(results, Article{
				Title: strings.TrimSpace(content.Title),
				URL:   abs,
			})

			if len(results) >= max {
				break
			}
		}
		if len(results) >= max {
			break
		}
	}

	return results
}

type decision struct {
	kind     string // "alert" | "relation" | "skip"
	topic    string
	impact   string
	head     string
	relation string
	tail     string
	reason   string
}

// classifyStory: quick heuristics. Alert if headline contains strong urgency; otherwise try to extract a relation triplet.
func classifyStory(headline string) decision {
	hl := strings.ToLower(headline)
	urgent := []string{"breaking", "crisis", "hurricane", "earthquake", "shooting", "blast", "attack", "wildfire", "evacuate", "state of emergency"}
	for _, u := range urgent {
		if strings.Contains(hl, u) {
			return decision{kind: "alert", topic: "", impact: impactFromHeadline(hl), reason: "urgent_keyword"}
		}
	}
	// Relation extraction heuristic: look for patterns of the form X verb Y
	// Example: "Arctic sea ice shrinking faster than models predicted"
	// Simplify to head, relation, tail
	if trip := extractRelation(headline); trip != nil {
		return decision{kind: "relation", head: trip[0], relation: trip[1], tail: trip[2], reason: "pattern_match"}
	}
	return decision{kind: "skip", reason: "no_pattern_match"}
}

func debugLogDecision(dec decision, title string, debug bool, source string) {
	if !debug {
		return
	}
	prefix := ""
	if source != "" {
		prefix = fmt.Sprintf("[%s] ", source)
	}
	switch dec.kind {
	case "alert":
		fmt.Fprintf(os.Stderr, "%sALERT  :: %s | impact=%s reason=%s\n", prefix, title, dec.impact, dec.reason)
	case "relation":
		fmt.Fprintf(os.Stderr, "%sREL    :: %s | %s %s %s (reason=%s)\n", prefix, title, dec.head, dec.relation, dec.tail, dec.reason)
	default:
		fmt.Fprintf(os.Stderr, "%sSKIP   :: %s | reason=%s\n", prefix, title, dec.reason)
	}
}

func publishHeuristicDecision(dec decision, title string, story Article, dry, debug bool, alertBus, relBus *eventbus.NATSBus, ctx context.Context) bool {
	switch dec.kind {
	case "alert":
		confidence := calculateAlertConfidence(dec.impact, false, title)
		evt := wrapAlert("bbc", title, strings.TrimSpace(dec.topic), dec.impact, confidence)
		if dry {
			printEvent("agi.events.news.alerts", evt)
			return true
		}
		if err := alertBus.Publish(ctx, evt); err != nil {
			if debug {
				fmt.Fprintf(os.Stderr, "publish alert failed: %v\n", err)
			}
			return false
		}
		return true
	case "relation":
		if !looksLikeActor(dec.head) {
			if debug {
				fmt.Fprintf(os.Stderr, "SKIP   :: %s | reason=actor_check_failed head=%q\n", title, dec.head)
			}
			return false
		}
		confidence := calculateRelationConfidence(dec.head, dec.relation, dec.tail, title)
		evt := wrapRelation("bbc", dec.head, dec.relation, dec.tail, title, story.URL, confidence)
		if dry {
			printEvent("agi.events.news.relations", evt)
			return true
		}
		if err := relBus.Publish(ctx, evt); err != nil {
			if debug {
				fmt.Fprintf(os.Stderr, "publish relation failed: %v\n", err)
			}
			return false
		}
		return true
	default:
		return false
	}
}

func attemptFallback(title string, story Article, dry, debug bool, alertBus, relBus *eventbus.NATSBus, ctx context.Context) bool {
	decision := classifyStory(title)
	debugLogDecision(decision, title, debug, "FALLBACK")
	if decision.kind == "skip" {
		return false
	}
	return publishHeuristicDecision(decision, title, story, dry, debug, alertBus, relBus, ctx)
}

func impactFromHeadline(hl string) string {
	if strings.Contains(hl, "hurricane") || strings.Contains(hl, "earthquake") || strings.Contains(hl, "mass") {
		return "high"
	}
	if strings.Contains(hl, "warning") || strings.Contains(hl, "storm") || strings.Contains(hl, "shutdown") {
		return "medium"
	}
	return "low"
}

// calculateAlertConfidence calculates confidence score based on impact, urgency, and content quality
func calculateAlertConfidence(impact string, isUrgent bool, title string) float64 {
	baseConfidence := 0.5

	// Base confidence by impact level
	switch impact {
	case "high":
		baseConfidence = 0.9
	case "medium":
		baseConfidence = 0.7
	case "low":
		baseConfidence = 0.5
	}

	// Urgency boost
	if isUrgent {
		baseConfidence += 0.2
		if baseConfidence > 1.0 {
			baseConfidence = 1.0
		}
	}

	// Content quality indicators
	titleLower := strings.ToLower(title)

	// High confidence indicators
	if strings.Contains(titleLower, "official") || strings.Contains(titleLower, "confirmed") {
		baseConfidence += 0.1
	}
	if strings.Contains(titleLower, "government") || strings.Contains(titleLower, "ministry") {
		baseConfidence += 0.1
	}
	if strings.Contains(titleLower, "scientific") || strings.Contains(titleLower, "research") {
		baseConfidence += 0.1
	}

	// Low confidence indicators
	if strings.Contains(titleLower, "reportedly") || strings.Contains(titleLower, "allegedly") {
		baseConfidence -= 0.1
	}
	if strings.Contains(titleLower, "rumor") || strings.Contains(titleLower, "speculation") {
		baseConfidence -= 0.2
	}
	if strings.Contains(titleLower, "entertainment") || strings.Contains(titleLower, "celebrity") {
		baseConfidence -= 0.3
	}

	// Ensure confidence stays within bounds
	if baseConfidence < 0.0 {
		baseConfidence = 0.0
	}
	if baseConfidence > 1.0 {
		baseConfidence = 1.0
	}

	return baseConfidence
}

// calculateRelationConfidence calculates confidence score for relations based on content quality
func calculateRelationConfidence(head, relation, tail, title string) float64 {
	baseConfidence := 0.7 // Start with moderate confidence

	// Check for high-quality relation patterns
	relationLower := strings.ToLower(relation)

	// High confidence relation types
	highConfidenceRels := []string{"approves", "blocks", "bans", "legalises", "imposes", "demands", "accuses", "sentences"}
	for _, rel := range highConfidenceRels {
		if strings.Contains(relationLower, rel) {
			baseConfidence = 0.9
			break
		}
	}

	// Medium confidence relation types
	mediumConfidenceRels := []string{"warns", "calls", "claims", "increases", "decreases", "affects", "causes"}
	for _, rel := range mediumConfidenceRels {
		if strings.Contains(relationLower, rel) {
			baseConfidence = 0.8
			break
		}
	}

	// Actor quality checks
	if looksLikeActor(head) {
		baseConfidence += 0.1
	}

	// Check for specific actor types that increase confidence
	headLower := strings.ToLower(head)
	highConfidenceActors := []string{"government", "ministry", "court", "parliament", "congress", "senate"}
	for _, actor := range highConfidenceActors {
		if strings.Contains(headLower, actor) {
			baseConfidence += 0.1
			break
		}
	}

	// Check for vague or low-quality indicators
	if strings.Contains(strings.ToLower(tail), "something") || strings.Contains(strings.ToLower(tail), "various") {
		baseConfidence -= 0.2
	}
	if len(strings.Fields(tail)) < 3 {
		baseConfidence -= 0.1
	}

	// Ensure confidence stays within bounds
	if baseConfidence < 0.0 {
		baseConfidence = 0.0
	}
	if baseConfidence > 1.0 {
		baseConfidence = 1.0
	}

	return baseConfidence
}

// extractRelation returns [head, relation, tail] if a simple pattern is matched.
func extractRelation(headline string) []string {
	// Patterns like: X is/are/was ... Y; X verb Y; X vs Y (conflict)
	h := normalizeHeadline(headline)
	// faster than
	if i := strings.Index(strings.ToLower(h), " faster than "); i > 0 {
		head := strings.TrimSpace(h[:i])
		tail := strings.TrimSpace(h[i+len(" faster than "):])
		return []string{head, "is_faster_than", tail}
	}
	// increases/decreases/impacts/affects/causes/reduces/boosts/raises/lowers/cuts
	re := regexp.MustCompile(`(?i)^(.*) (increases|decreases|impacts|affects|causes|reduces|boosts|raises|lowers|cuts) (.*)$`)
	if m := re.FindStringSubmatch(h); len(m) == 4 {
		return []string{strings.TrimSpace(m[1]), strings.ToLower(m[2]), strings.TrimSpace(m[3])}
	}
	// warns of/over
	if m := regexp.MustCompile(`(?i)^(.*) warns (of|over) (.*)$`).FindStringSubmatch(h); len(m) == 4 {
		return []string{strings.TrimSpace(m[1]), "warns_" + strings.ToLower(m[2]), strings.TrimSpace(m[3])}
	}
	// approves/blocks/bans/suspends/charges
	if m := regexp.MustCompile(`(?i)^(.*) (approves|blocks|bans|suspends|charges) (.*)$`).FindStringSubmatch(h); len(m) == 4 {
		return []string{strings.TrimSpace(m[1]), strings.ToLower(m[2]), strings.TrimSpace(m[3])}
	}
	// sentences X to Y
	if m := regexp.MustCompile(`(?i)^(.*) sentences (.*) to (.*)$`).FindStringSubmatch(h); len(m) == 4 {
		return []string{strings.TrimSpace(m[1]), "sentences_to", strings.TrimSpace(m[3])}
	}
	// accuses X of Y
	if m := regexp.MustCompile(`(?i)^(.*) accuses (.*) of (.*)$`).FindStringSubmatch(h); len(m) == 4 {
		return []string{strings.TrimSpace(m[1]), "accuses_of", strings.TrimSpace(m[3])}
	}
	// legalises
	if m := regexp.MustCompile(`(?i)^(.*) legalis(e|es) (.*)$`).FindStringSubmatch(h); len(m) >= 4 {
		return []string{strings.TrimSpace(m[1]), "legalises", strings.TrimSpace(m[len(m)-1])}
	}
	// imposes
	if m := regexp.MustCompile(`(?i)^(.*) imposes (.*)$`).FindStringSubmatch(h); len(m) == 3 {
		return []string{strings.TrimSpace(m[1]), "imposes", strings.TrimSpace(m[2])}
	}
	// to close -> will_close
	if m := regexp.MustCompile(`(?i)^(.*) to close (.*)$`).FindStringSubmatch(h); len(m) == 3 {
		return []string{strings.TrimSpace(m[1]), "will_close", strings.TrimSpace(m[2])}
	}
	// calls
	if m := regexp.MustCompile(`(?i)^(.*) calls (.*)$`).FindStringSubmatch(h); len(m) == 3 {
		return []string{strings.TrimSpace(m[1]), "calls", strings.TrimSpace(m[2])}
	}
	// demands ... over ... -> demands_over
	if m := regexp.MustCompile(`(?i)^(.*) demands (.*) over (.*)$`).FindStringSubmatch(h); len(m) == 4 {
		return []string{strings.TrimSpace(m[1]), "demands_over", strings.TrimSpace(m[3])}
	}
	// claims ... (treat as claims)
	if m := regexp.MustCompile(`(?i)^(.*) claims (.*)$`).FindStringSubmatch(h); len(m) == 3 {
		return []string{strings.TrimSpace(m[1]), "claims", strings.TrimSpace(m[2])}
	}
	// as X shuts Y -> X shuts Y
	if m := regexp.MustCompile(`(?i)^.* as (.*) shuts (.*)$`).FindStringSubmatch(h); len(m) == 3 {
		return []string{strings.TrimSpace(m[1]), "shuts", strings.TrimSpace(m[2])}
	}
	return nil
}

// normalizeHeadline cleans noisy list markers, HTML entities, and drops trailing time/section and subordinate clauses.
func normalizeHeadline(h string) string {
	s := decodeHTMLEntities(strings.TrimSpace(h))
	// strip leading list numbers like "4 "
	s = regexp.MustCompile(`^\d+\s+`).ReplaceAllString(s, "")
	// remove trailing "N hrs/mins ago ..." fragments
	s = regexp.MustCompile(`\s+\d+\s+(hrs?|minutes?|mins?)\s+ago.*$`).ReplaceAllString(s, "")
	// cut subordinate clause after " after ..." to keep primary action
	if i := strings.Index(strings.ToLower(s), " after "); i > 0 {
		s = strings.TrimSpace(s[:i])
	}
	// collapse whitespace
	s = strings.Join(strings.Fields(s), " ")
	return s
}

// looksLikeActor checks if the head starts with a capital letter or known org tokens.
func looksLikeActor(head string) bool {
	h := strings.TrimSpace(head)
	if h == "" {
		return false
	}
	r := []rune(h)
	if r[0] >= 'A' && r[0] <= 'Z' {
		return true
	}
	lower := strings.ToLower(h)
	if strings.HasPrefix(lower, "us ") || strings.HasPrefix(lower, "uk ") || strings.HasPrefix(lower, "eu ") {
		return true
	}
	orgs := []string{"police", "court", "government", "white house", "parliament"}
	for _, o := range orgs {
		if strings.HasPrefix(lower, o) {
			return true
		}
	}
	return false
}

func wrapRelation(source, head, relation, tail, headline string, urlStr string, confidence float64) eventbus.CanonicalEvent {
	now := time.Now().UTC()
	md := map[string]any{
		"id":         eventbus.NewEventID("rel_", now),
		"head":       head,
		"relation":   relation,
		"tail":       tail,
		"confidence": confidence,
		"source":     source,
		"headline":   headline,
		"timestamp":  now.Format(time.RFC3339),
		"url":        urlStr,
	}
	return eventbus.CanonicalEvent{
		EventID:   eventbus.NewEventID("evt_", now),
		Source:    "news:" + source,
		Type:      "relations",
		Timestamp: now,
		Context:   eventbus.EventContext{Channel: "news"},
		Payload:   eventbus.EventPayload{Text: decodeHTMLEntities(fmt.Sprintf("%s %s %s", head, relation, tail)), Metadata: md},
		Security:  eventbus.EventSecurity{Sensitivity: "low"},
	}
}

func wrapAlert(source, headline, topic, impact string, confidence float64) eventbus.CanonicalEvent {
	now := time.Now().UTC()
	md := map[string]any{
		"alert_type": "breaking",
		"impact":     impact,
		"confidence": confidence,
		"source":     source,
		"headline":   headline,
		"timestamp":  now.Format(time.RFC3339),
	}
	if strings.TrimSpace(topic) != "" {
		md["topic"] = topic
	}
	return eventbus.CanonicalEvent{
		EventID:   eventbus.NewEventID("evt_", now),
		Source:    "news:" + source,
		Type:      "alerts",
		Timestamp: now,
		Context:   eventbus.EventContext{Channel: "news"},
		Payload:   eventbus.EventPayload{Text: decodeHTMLEntities(headline), Metadata: md},
		Security:  eventbus.EventSecurity{Sensitivity: "low"},
	}
}

func printEvent(subject string, evt eventbus.CanonicalEvent) {
	b, _ := json.MarshalIndent(evt, "", "  ")
	fmt.Printf("DRY-RUN publish to %s\n%s\n", subject, string(b))
}

func httpGet(u string) (string, error) {
	client := &http.Client{Timeout: 20 * time.Second}
	req, _ := http.NewRequest("GET", u, nil)
	req.Header.Set("User-Agent", "agi-bbc-ingestor/1.0")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return string(b), nil
}

func toAbs(base, href string) string {
	bu, err := url.Parse(base)
	if err != nil {
		return href
	}
	hu, err := url.Parse(href)
	if err != nil {
		return href
	}
	return bu.ResolveReference(hu).String()
}

func stripTags(s string) string {
	// remove simple tags and entities
	s = regexp.MustCompile(`<[^>]+>`).ReplaceAllString(s, " ")
	s = strings.ReplaceAll(s, "&amp;", "&")
	s = strings.ReplaceAll(s, "&quot;", "\"")
	s = strings.ReplaceAll(s, "&#39;", "'")
	return strings.Join(strings.Fields(s), " ")
}

func htmlUnescape(s string) string {
	r := strings.NewReplacer("&amp;", "&", "&quot;", "\"", "&#39;", "'", "&lt;", "<", "&gt;", ">")
	return r.Replace(s)
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func getenvFirst(keys []string, def string) string {
	for _, k := range keys {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	return def
}

func containsPhrase(haystackLower string, phrase string) bool {
	needle := strings.ToLower(strings.TrimSpace(phrase))
	if needle == "" {
		return false
	}
	// ensure the phrase exists exactly; collapse multiple spaces
	needle = strings.Join(strings.Fields(needle), " ")
	return strings.Contains(haystackLower, needle)
}

// isSectionName checks if a string looks like a section name rather than an actor
func isSectionName(s string) bool {
	sectionNames := []string{"news", "sport", "business", "technology", "entertainment", "health", "science", "world", "uk", "us", "europe", "asia", "africa", "middle east", "americas", "politics", "economy", "culture", "lifestyle", "travel", "weather", "opinion", "analysis", "features", "live", "breaking", "latest", "top stories", "most read", "most watched", "trending", "popular", "recommended", "editors picks", "in pictures", "in video", "in audio", "special reports", "investigations", "documentaries", "programmes", "schedules", "about", "contact", "help", "terms", "privacy", "cookies", "accessibility", "jobs", "advertise", "subscribe", "newsletter", "rss", "mobile", "apps", "social media", "facebook", "twitter", "instagram", "youtube", "tiktok", "linkedin", "pinterest", "snapchat", "whatsapp", "telegram", "discord", "reddit", "tumblr", "flickr", "vimeo", "soundcloud", "spotify", "apple music", "google play", "amazon music", "deezer", "tidal", "pandora", "iheartradio", "tunein", "stitcher", "castbox", "overcast", "pocket casts", "castro", "downcast", "podcast addict", "player fm", "stitcher", "spotify", "apple podcasts", "google podcasts", "amazon music", "audible", "libsyn", "buzzsprout", "anchor", "spreaker", "podbean", "transistor", "simplecast", "megaphone", "acast", "pocket casts", "overcast", "castro", "downcast", "podcast addict", "player fm", "stitcher", "spotify", "apple podcasts", "google podcasts", "amazon music", "audible", "libsyn", "buzzsprout", "anchor", "spreaker", "podbean", "transistor", "simplecast", "megaphone", "acast"}
	for _, sn := range sectionNames {
		if strings.EqualFold(s, sn) {
			return true
		}
	}
	return false
}

// decodeHTMLEntities handles numeric (decimal and hex) entities in addition to common named ones.
func decodeHTMLEntities(s string) string {
	// First handle common named via htmlUnescape
	s = htmlUnescape(s)
	// Decimal entities: &#39;
	s = regexp.MustCompile(`&#(\d+);`).ReplaceAllStringFunc(s, func(m string) string {
		sub := regexp.MustCompile(`\d+`).FindString(m)
		if sub == "" {
			return m
		}
		// naive parse
		var code int
		for i := 0; i < len(sub); i++ {
			code = code*10 + int(sub[i]-'0')
		}
		if code <= 0 || code > 0x10FFFF {
			return m
		}
		return string(rune(code))
	})
	// Hex entities: &#x27;
	s = regexp.MustCompile(`&#x([0-9A-Fa-f]+);`).ReplaceAllStringFunc(s, func(m string) string {
		hexPart := regexp.MustCompile(`[0-9A-Fa-f]+`).FindString(m)
		if hexPart == "" {
			return m
		}
		var code int
		for i := 0; i < len(hexPart); i++ {
			ch := hexPart[i]
			code *= 16
			switch {
			case ch >= '0' && ch <= '9':
				code += int(ch - '0')
			case ch >= 'a' && ch <= 'f':
				code += int(ch - 'a' + 10)
			case ch >= 'A' && ch <= 'F':
				code += int(ch - 'A' + 10)
			}
		}
		if code <= 0 || code > 0x10FFFF {
			return m
		}
		return string(rune(code))
	})
	return s
}
