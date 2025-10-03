//go:build neo4j
// +build neo4j

package main

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	mempkg "agi/hdn/memory"

	"github.com/redis/go-redis/v9"
)

type config struct {
	Seeds            []string
	MaxDepth         int
	MaxNodes         int
	RequestsPerMin   int
	Burst            int
	JitterMs         int
	MinRelConfidence float64
	Domain           string
	EnableWeaviate   bool
	JobID            string
	Resume           bool
	PauseOnly        bool
}

type wikiSummary struct {
	Title       string `json:"title"`
	Extract     string `json:"extract"`
	Description string `json:"description"`
	ContentURL  struct {
		Desktop struct {
			Page string `json:"page"`
		} `json:"desktop"`
	} `json:"content_urls"`
}

type queueItem struct {
	Title string
	Depth int
}

func main() {
	var seedsCSV string
	var maxDepth int
	var maxNodes int
	var rpm int
	var burst int
	var jitter int
	var minConf float64
	var domain string
	var enableWeaviate bool
	var jobID string
	var resume bool
	var pauseOnly bool

	flag.StringVar(&seedsCSV, "seeds", "Science,Technology,History,Mathematics,Biology", "Comma-separated Wikipedia titles to seed")
	flag.IntVar(&maxDepth, "max-depth", 1, "Maximum crawl depth")
	flag.IntVar(&maxNodes, "max-nodes", 200, "Maximum number of concepts to ingest")
	flag.IntVar(&rpm, "rpm", 30, "Requests per minute rate limit")
	flag.IntVar(&burst, "burst", 5, "Burst allowance for rate limiter")
	flag.IntVar(&jitter, "jitter-ms", 250, "Jitter in milliseconds added to each request")
	flag.Float64Var(&minConf, "min-confidence", 0.6, "Minimum confidence to create relation")
	flag.StringVar(&domain, "domain", "General", "Domain tag to assign to seeded concepts")
	flag.BoolVar(&enableWeaviate, "weaviate", false, "Also index summaries into Weaviate episodic memory")
	flag.StringVar(&jobID, "job-id", "", "Job ID for pause/resume (default: timestamp)")
	flag.BoolVar(&resume, "resume", false, "Resume from previous state for this job-id")
	flag.BoolVar(&pauseOnly, "pause", false, "Set the job paused flag and exit")
	flag.Parse()

	cfg := &config{
		Seeds:            splitAndTrim(seedsCSV),
		MaxDepth:         maxDepth,
		MaxNodes:         maxNodes,
		RequestsPerMin:   rpm,
		Burst:            burst,
		JitterMs:         jitter,
		MinRelConfidence: minConf,
		Domain:           domain,
		EnableWeaviate:   enableWeaviate,
		JobID:            jobID,
		Resume:           resume,
		PauseOnly:        pauseOnly,
	}

	ctx := context.Background()

	// Clients: Neo4j (required), Redis (seen), Qdrant (optional)
	neo4jURI := getenvDefault("NEO4J_URI", "bolt://localhost:7687")
	neo4jUser := getenvDefault("NEO4J_USER", "neo4j")
	neo4jPass := getenvDefault("NEO4J_PASS", "test1234")

	dk, err := mempkg.NewDomainKnowledgeClient(neo4jURI, neo4jUser, neo4jPass)
	if err != nil {
		log.Fatalf("neo4j connect: %v", err)
	}
	defer dk.Close(ctx)

	rdb := redis.NewClient(&redis.Options{Addr: getenvDefault("REDIS_ADDR", "localhost:6379")})

	// Determine job id
	if strings.TrimSpace(cfg.JobID) == "" {
		cfg.JobID = time.Now().Format("20060102T150405")
	}
	baseKey := fmt.Sprintf("wiki:job:%s", cfg.JobID)
	queueKey := baseKey + ":queue"
	seenKey := baseKey + ":seen"
	processedKey := baseKey + ":processed"
	pausedKey := baseKey + ":paused"

	if cfg.PauseOnly {
		if err := rdb.Set(ctx, pausedKey, 1, 0).Err(); err != nil {
			log.Fatalf("failed to set pause flag: %v", err)
		}
		log.Printf("Job %s paused.", cfg.JobID)
		return
	}

	// If not resuming, initialize queue with seeds
	if !cfg.Resume {
		// Clear previous state for this job id
		_ = rdb.Del(ctx, queueKey).Err()
		_ = rdb.Del(ctx, seenKey).Err()
		_ = rdb.Del(ctx, processedKey).Err()
		_ = rdb.Del(ctx, pausedKey).Err()
		if len(cfg.Seeds) == 0 {
			log.Fatalf("no seeds provided")
		}
		for _, s := range cfg.Seeds {
			// store as title|depth
			_ = rdb.RPush(ctx, queueKey, fmt.Sprintf("%s|%d", s, 0)).Err()
		}
	}

	var vectorDB mempkg.VectorDBAdapter
	if cfg.EnableWeaviate {
		weaviateURL := getenvDefault("WEAVIATE_URL", "http://localhost:8080")
		vectorDB = mempkg.NewVectorDBAdapter(weaviateURL, "agi-wiki")
		_ = vectorDB.EnsureCollection(8) // toy embed size (8 dims)
	}

	httpClient := &http.Client{Timeout: 15 * time.Second}
	limiter := newTokenBucket(cfg.RequestsPerMin, cfg.Burst)

	log.Printf("Wiki bootstrapper starting; job=%s depth=%d limit=%d rpm=%d resume=%v", cfg.JobID, cfg.MaxDepth, cfg.MaxNodes, cfg.RequestsPerMin, cfg.Resume)

	processed := int64(0)
	if v, err := rdb.Get(ctx, processedKey).Int64(); err == nil {
		processed = v
	}
	var mu sync.Mutex

	idleLoops := 0
	for processed < int64(cfg.MaxNodes) {
		// Pause support
		if paused, _ := rdb.Get(ctx, pausedKey).Int(); paused == 1 {
			log.Printf("Job paused. Waiting...")
			time.Sleep(2 * time.Second)
			continue
		}

		// Pop from Redis queue
		raw, err := rdb.LPop(ctx, queueKey).Result()
		if err == redis.Nil {
			// empty queue: if we've been idle for a while, stop
			idleLoops++
			if idleLoops > 10 {
				log.Printf("Queue empty. Stopping.")
				break
			}
			time.Sleep(1 * time.Second)
			continue
		} else if err != nil {
			log.Printf("queue pop error: %v", err)
			time.Sleep(1 * time.Second)
			continue
		}
		idleLoops = 0

		it := parseQueueItem(raw)
		if it.Depth > cfg.MaxDepth {
			continue
		}
		norm := strings.ToLower(it.Title)
		exists, _ := rdb.SIsMember(ctx, seenKey, norm).Result()
		if exists {
			continue
		}

		limiter.Wait()
		jitterSleep(cfg.JitterMs)

		sum, err := fetchSummary(ctx, httpClient, it.Title)
		if err != nil {
			log.Printf("warn: fetch summary %q: %v", it.Title, err)
			continue
		}

		name := firstNonEmpty(sum.Title, it.Title)
		defn := firstNonEmpty(sum.Description, sum.Extract)
		url := sum.ContentURL.Desktop.Page

		if err := dk.SaveConcept(ctx, &mempkg.Concept{
			Name:       name,
			Domain:     cfg.Domain,
			Definition: defn,
		}); err != nil {
			log.Printf("neo4j save concept %q: %v", name, err)
			continue
		}

		// Heuristic: extract "is a" relation
		if parent, ok := extractIsARelation(defn); ok {
			if parent != "" && strings.ToLower(parent) != strings.ToLower(name) {
				// upsert parent concept with stub
				_ = dk.SaveConcept(ctx, &mempkg.Concept{Name: parent, Domain: cfg.Domain, Definition: ""})
				_ = dk.RelateConcepts(ctx, name, "IS_A", parent, map[string]any{
					"confidence":     cfg.MinRelConfidence,
					"source":         "wikipedia",
					"extracted_from": name,
					"url":            url,
					"created_at":     time.Now().Format(time.RFC3339),
				})
			}
		}

		// Expand via related endpoint
		related, err := fetchRelated(ctx, httpClient, name)
		if err == nil {
			for _, rel := range related {
				relNorm := strings.ToLower(rel)
				if relNorm == norm {
					continue
				}
				// enqueue next depth
				if it.Depth < cfg.MaxDepth {
					_ = rdb.RPush(ctx, queueKey, fmt.Sprintf("%s|%d", rel, it.Depth+1)).Err()
				}
				// create weak RELATED_TO edge
				_ = dk.SaveConcept(ctx, &mempkg.Concept{Name: rel, Domain: cfg.Domain, Definition: ""})
				_ = dk.RelateConcepts(ctx, name, "RELATED_TO", rel, map[string]any{
					"confidence":     0.5,
					"source":         "wikipedia",
					"extracted_from": name,
					"url":            url,
					"created_at":     time.Now().Format(time.RFC3339),
				})
			}
		}

		if vectorDB != nil {
			// Store Wikipedia article in Weaviate using WikipediaArticle class
			rec := &mempkg.EpisodicRecord{
				Text: fmt.Sprintf("%s\n\n%s", name, defn),
				Metadata: map[string]any{
					"source":    "wikipedia",
					"title":     name,
					"url":       url,
					"timestamp": time.Now().Format(time.RFC3339),
				},
			}
			vec := toyEmbed(rec.Text, 8)
			_ = vectorDB.IndexEpisode(rec, vec)
		}

		rdb.SAdd(ctx, seenKey, norm)
		mu.Lock()
		processed++
		_ = rdb.Set(ctx, processedKey, processed, 0).Err()
		if processed%10 == 0 {
			qlen, _ := rdb.LLen(ctx, queueKey).Result()
			log.Printf("progress: processed=%d queued=%d depth=%d", processed, qlen, it.Depth)
		}
		mu.Unlock()
	}

	log.Printf("Done. job=%s processed=%d", cfg.JobID, processed)
}

func getenvDefault(k, def string) string {
	v := os.Getenv(k)
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}

func splitAndTrim(csv string) []string {
	parts := strings.Split(csv, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		t := strings.TrimSpace(p)
		if t != "" {
			out = append(out, t)
		}
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// Simple token bucket limiter for RPM
type tokenBucket struct {
	mu       sync.Mutex
	tokens   int
	capacity int
	refill   time.Duration
	lastRef  time.Time
}

func newTokenBucket(rpm int, burst int) *tokenBucket {
	if rpm <= 0 {
		rpm = 30
	}
	if burst <= 0 {
		burst = 5
	}
	return &tokenBucket{tokens: burst, capacity: burst, refill: time.Minute / time.Duration(rpm), lastRef: time.Now()}
}

func (t *tokenBucket) Wait() {
	for {
		t.mu.Lock()
		now := time.Now()
		// refill tokens since lastRef
		for now.Sub(t.lastRef) >= t.refill && t.tokens < t.capacity {
			t.tokens++
			t.lastRef = t.lastRef.Add(t.refill)
		}
		if t.tokens > 0 {
			t.tokens--
			t.mu.Unlock()
			return
		}
		t.mu.Unlock()
		time.Sleep(t.refill / 2)
	}
}

func jitterSleep(ms int) {
	if ms <= 0 {
		return
	}
	d := time.Duration(rand.Intn(ms)) * time.Millisecond
	time.Sleep(d)
}

func fetchSummary(ctx context.Context, httpClient *http.Client, title string) (*wikiSummary, error) {
	url := fmt.Sprintf("https://en.wikipedia.org/api/rest_v1/page/summary/%s", escapeTitle(title))
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	req.Header.Set("User-Agent", "agi-wiki-bootstrapper/1.0 (+contact: dev@example.com)")
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("http %s", resp.Status)
	}
	var s wikiSummary
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		return nil, err
	}
	return &s, nil
}

func fetchRelated(ctx context.Context, httpClient *http.Client, title string) ([]string, error) {
	url := fmt.Sprintf("https://en.wikipedia.org/api/rest_v1/page/related/%s", escapeTitle(title))
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	req.Header.Set("User-Agent", "agi-wiki-bootstrapper/1.0 (+contact: dev@example.com)")
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("http %s", resp.Status)
	}
	var data struct {
		Pages []struct {
			Title string `json:"title"`
		} `json:"pages"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(data.Pages))
	for _, p := range data.Pages {
		if strings.TrimSpace(p.Title) != "" {
			out = append(out, p.Title)
		}
	}
	return out, nil
}

func escapeTitle(t string) string {
	t = strings.TrimSpace(t)
	t = strings.ReplaceAll(t, " ", "_")
	return t
}

// Very lightweight heuristic: find "is a" pattern and return parent noun phrase tail
func extractIsARelation(text string) (string, bool) {
	s := strings.ToLower(text)
	idx := strings.Index(s, " is a ")
	if idx < 0 {
		idx = strings.Index(s, " is an ")
		if idx < 0 {
			return "", false
		}
		// move past " is an "
		idx += len(" is an ")
	} else {
		idx += len(" is a ")
	}
	tail := strings.TrimSpace(text[idx:])
	// cut at punctuation or " that/which/who "
	cutTokens := []string{".", ",", ";", ":", " that ", " which ", " who "}
	cut := len(tail)
	lowerTail := strings.ToLower(tail)
	for _, tok := range cutTokens {
		if i := strings.Index(lowerTail, tok); i >= 0 && i < cut {
			cut = i
		}
	}
	parent := strings.TrimSpace(tail[:cut])
	// keep first 5 words to avoid long clauses
	words := strings.Fields(parent)
	if len(words) == 0 {
		return "", false
	}
	if len(words) > 5 {
		words = words[:5]
	}
	parent = strings.Join(words, " ")
	// Title case the parent
	parent = strings.Title(parent)
	return parent, true
}

// toyEmbed: deterministic 8-dim embedding compatible with Qdrant collection dim=8
func toyEmbed(text string, dim int) []float32 {
	if dim <= 0 {
		dim = 8
	}
	sum := sha256.Sum256([]byte(text))
	vec := make([]float32, dim)
	// fill with 4-byte chunks
	for i := 0; i < dim; i++ {
		off := (i * 4) % len(sum)
		v := binary.LittleEndian.Uint32(sum[off : off+4])
		val := (float32(v%20000)/10000.0 - 1.0)
		vec[i] = val
	}
	// L2 normalize
	var s float32
	for i := 0; i < dim; i++ {
		s += vec[i] * vec[i]
	}
	if s > 0 {
		// sqrt via simple Newton iterations
		z := s
		for j := 0; j < 6; j++ {
			z = 0.5 * (z + s/z)
		}
		inv := 1.0 / float32(z)
		for i := 0; i < dim; i++ {
			vec[i] *= inv
		}
	}
	return vec
}

func parseQueueItem(s string) queueItem {
	// format: title|depth
	parts := strings.Split(s, "|")
	if len(parts) != 2 {
		return queueItem{Title: s, Depth: 0}
	}
	d := 0
	if v, err := strconv.Atoi(parts[1]); err == nil {
		d = v
	}
	return queueItem{Title: parts[0], Depth: d}
}
