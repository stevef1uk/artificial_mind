package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"async_llm"

	"github.com/redis/go-redis/v9"
)

type config struct {
	QdrantURL   string
	RedisAddr   string
	LLMProvider string
	LLMEndpoint string
	LLMModel    string
	BatchSize   int
	MaxWords    int
	Domain      string
	JobID       string
	Resume      bool
	PauseOnly   bool
}

type wikiArticle struct {
	Title       string `json:"title"`
	Extract     string `json:"extract"`
	Description string `json:"description"`
	ContentURL  struct {
		Desktop struct {
			Page string `json:"page"`
		} `json:"desktop"`
	} `json:"content_urls"`
}

type llmClient struct {
	provider string
	endpoint string
	model    string
	client   *http.Client // Kept for backward compatibility, but async client is used
}

// VectorDBClient interface for both Qdrant and Weaviate
type VectorDBClient interface {
	SearchArticles(ctx context.Context, limit int, filters map[string]interface{}) ([]wikipediaArticle, error)
	UpdateArticleSummary(ctx context.Context, articleID, summary string) error
}

// isQdrantURL checks if the URL is for Qdrant
func isQdrantURL(url string) bool {
	return strings.Contains(url, "qdrant") || strings.Contains(url, ":6333")
}

// isWeaviateURL checks if the URL points to Weaviate
func isWeaviateURL(url string) bool {
	return strings.Contains(url, "weaviate") || strings.Contains(url, ":8080")
}

func main() {
	var qdrantURL string
	var redisAddr string
	var llmProvider string
	var llmEndpoint string
	var llmModel string
	var batchSize int
	var maxWords int
	var domain string
	var jobID string
	var resume bool
	var pauseOnly bool

	flag.StringVar(&qdrantURL, "weaviate", "http://localhost:8080", "Vector database URL (Weaviate)")
	flag.StringVar(&redisAddr, "redis", "localhost:6379", "Redis address")
	flag.StringVar(&llmProvider, "llm-provider", "ollama", "LLM provider (ollama, openai, etc.)")
	flag.StringVar(&llmEndpoint, "llm-endpoint", "http://localhost:11434/api/generate", "LLM endpoint")
	flag.StringVar(&llmModel, "llm-model", "gemma3n:latest", "LLM model name")
	flag.IntVar(&batchSize, "batch-size", 5, "Number of articles to process per batch")
	flag.IntVar(&maxWords, "max-words", 250, "Maximum words in summary")
	flag.StringVar(&domain, "domain", "General", "Domain to process")
	flag.StringVar(&jobID, "job-id", "", "Job ID for pause/resume (default: timestamp)")
	flag.BoolVar(&resume, "resume", false, "Resume from previous state for this job-id")
	flag.BoolVar(&pauseOnly, "pause", false, "Set the job paused flag and exit")
	flag.Parse()

	// Environment variable fallbacks for Kubernetes deployment
	if weaviateURL := os.Getenv("WEAVIATE_URL"); weaviateURL != "" {
		log.Printf("DEBUG: Environment variable override: WEAVIATE_URL=%s", weaviateURL)
		qdrantURL = weaviateURL
		log.Printf("DEBUG: Updated qdrantURL to: %s", qdrantURL)
	}
	if redisAddrEnv := os.Getenv("REDIS_ADDR"); redisAddrEnv != "" {
		redisAddr = redisAddrEnv
	}
	if llmProviderEnv := os.Getenv("LLM_PROVIDER"); llmProviderEnv != "" {
		llmProvider = llmProviderEnv
	}
	if llmEndpointEnv := os.Getenv("LLM_ENDPOINT"); llmEndpointEnv != "" {
		llmEndpoint = llmEndpointEnv
	}
	if llmModelEnv := os.Getenv("LLM_MODEL"); llmModelEnv != "" {
		llmModel = llmModelEnv
	}
	if batchSizeEnv := os.Getenv("BATCH_SIZE"); batchSizeEnv != "" {
		if bs, err := strconv.Atoi(batchSizeEnv); err == nil {
			batchSize = bs
		}
	}
	if maxWordsEnv := os.Getenv("MAX_WORDS"); maxWordsEnv != "" {
		if mw, err := strconv.Atoi(maxWordsEnv); err == nil {
			maxWords = mw
		}
	}
	if domainEnv := os.Getenv("DOMAIN"); domainEnv != "" {
		domain = domainEnv
	}

	cfg := &config{
		QdrantURL:   qdrantURL,
		RedisAddr:   redisAddr,
		LLMProvider: llmProvider,
		LLMEndpoint: llmEndpoint,
		LLMModel:    llmModel,
		BatchSize:   batchSize,
		MaxWords:    maxWords,
		Domain:      domain,
		JobID:       jobID,
		Resume:      resume,
		PauseOnly:   pauseOnly,
	}

	// Debug logging
	log.Printf("DEBUG: Parsed qdrantURL: %s", qdrantURL)
	log.Printf("DEBUG: Config QdrantURL: %s", cfg.QdrantURL)
	log.Printf("DEBUG: Environment WEAVIATE_URL: %s", os.Getenv("WEAVIATE_URL"))

	if cfg.JobID == "" {
		cfg.JobID = fmt.Sprintf("wiki_summarizer_%d", time.Now().Unix())
	}

	ctx := context.Background()

	// Connect to Redis
	rdb := redis.NewClient(&redis.Options{
		Addr: cfg.RedisAddr,
	})
	defer rdb.Close()

	// Connect to vector database (Weaviate or Qdrant)
	var vectorDB VectorDBClient
	if isQdrantURL(cfg.QdrantURL) {
		vectorDB = NewQdrantClient(cfg.QdrantURL, "agi-wiki")
		log.Printf("🔗 Using Qdrant: %s", cfg.QdrantURL)
	} else {
		// Use AgiWiki class in Weaviate, which is where the wiki bootstrapper
		// stores articles when indexing into the agi-wiki collection.
		vectorDB = NewWeaviateClient(cfg.QdrantURL, "AgiWiki")
		log.Printf("🔗 Using Weaviate: %s", cfg.QdrantURL)
	}

	// Initialize LLM client
	llm := &llmClient{
		provider: cfg.LLMProvider,
		endpoint: cfg.LLMEndpoint,
		model:    cfg.LLMModel,
		client:   &http.Client{Timeout: 60 * time.Second},
	}

	// Handle pause-only mode
	if cfg.PauseOnly {
		if err := rdb.Set(ctx, fmt.Sprintf("wiki_summarizer:paused:%s", cfg.JobID), "1", 0).Err(); err != nil {
			log.Fatalf("failed to set pause flag: %v", err)
		}
		log.Printf("✅ Job %s paused", cfg.JobID)
		return
	}

	// Resume from previous state
	processedKey := fmt.Sprintf("wiki_summarizer:processed:%s", cfg.JobID)
	pausedKey := fmt.Sprintf("wiki_summarizer:paused:%s", cfg.JobID)
	processed := int64(0)
	if cfg.Resume {
		if v, err := rdb.Get(ctx, processedKey).Int64(); err == nil {
			processed = v
		}
	}

	log.Printf("🚀 Starting Wikipedia summarizer job: %s", cfg.JobID)
	log.Printf("📊 Qdrant: %s", cfg.QdrantURL)
	log.Printf("🔴 Redis: %s", cfg.RedisAddr)
	log.Printf("🤖 LLM: %s (%s)", cfg.LLMProvider, cfg.LLMEndpoint)
	log.Printf("📝 Max words: %d", cfg.MaxWords)
	log.Printf("📦 Batch size: %d", cfg.BatchSize)

	// Process Wikipedia articles in batches (limit to 10 for testing)
	maxProcessed := int64(10)
	idleBatches := 0 // Track consecutive batches with no progress
	maxIdleBatches := 5 // Exit if 5 consecutive batches have no new articles

	for processed < maxProcessed {
		// Check if paused
		if paused, _ := rdb.Get(ctx, pausedKey).Int(); paused == 1 {
			log.Printf("⏸️ Job paused. Waiting...")
			time.Sleep(5 * time.Second)
			continue
		}

		// Get batch of Wikipedia articles from vector database
		articles, err := vectorDB.SearchArticles(ctx, cfg.BatchSize, map[string]interface{}{})
		if err != nil {
			log.Printf("❌ Failed to get articles: %v", err)
			time.Sleep(10 * time.Second)
			continue
		}

		if len(articles) == 0 {
			log.Printf("✅ No more articles to process")
			break
		}

		log.Printf("📚 Processing batch of %d articles", len(articles))

		batchProcessed := 0
		// Process each article
		for _, article := range articles {
			// Check if already processed (skip for now to test)
			articleKey := fmt.Sprintf("wiki_summarizer:processed_article:%s", article.ID)
			log.Printf("🔍 Checking Redis key: %s", articleKey)
			exists, err := rdb.Exists(ctx, articleKey).Result()
			if err != nil {
				log.Printf("❌ Redis error: %v", err)
			}
			log.Printf("🔍 Redis key exists: %d", exists)
			if exists > 0 {
				log.Printf("⏭️ Skipping already processed: %s", article.Title)
				continue
			}

			log.Printf("🤖 Generating summary for: %s", article.Title)
			log.Printf("📝 Original text: %s", article.Text[:min(100, len(article.Text))]+"...")

			// Generate summary using LLM
			summary, err := llm.generateSummary(article, cfg.MaxWords)
			if err != nil {
				log.Printf("❌ Failed to generate summary for %s: %v", article.Title, err)
				continue
			}

			log.Printf("📄 Generated summary: %s", summary[:min(100, len(summary))]+"...")

			// Update the article in vector database with the new summary
			if err := vectorDB.UpdateArticleSummary(ctx, article.ID, summary); err != nil {
				log.Printf("❌ Failed to update article %s: %v", article.Title, err)
				continue
			}

			// Mark as processed
			rdb.Set(ctx, articleKey, "1", 24*time.Hour)
			processed++
			batchProcessed++

			log.Printf("✅ Summarized: %s (%d words)", article.Title, len(strings.Fields(summary)))

			// Break if we've reached our limit
			if processed >= maxProcessed {
				log.Printf("🎯 Reached processing limit of %d articles", maxProcessed)
				break
			}
		}

		// Check if this batch had any progress
		if batchProcessed == 0 {
			idleBatches++
			log.Printf("⚠️ Batch had no new articles to process (idle batches: %d/%d)", idleBatches, maxIdleBatches)
			if idleBatches >= maxIdleBatches {
				log.Printf("✅ No new articles found after %d consecutive batches. Exiting.", maxIdleBatches)
				break
			}
		} else {
			idleBatches = 0 // Reset counter if we made progress
		}

		// Update processed count
		rdb.Set(ctx, processedKey, processed, 0)

		// Small delay between batches
		time.Sleep(2 * time.Second)
	}

	log.Printf("🎉 Wikipedia summarizer completed. Job: %s, Processed: %d articles", cfg.JobID, processed)
}

type wikipediaArticle struct {
	ID        string                 `json:"id"`
	Title     string                 `json:"title"`
	Text      string                 `json:"text"`
	Metadata  map[string]interface{} `json:"metadata"`
	Timestamp string                 `json:"timestamp"`
}

func (llm *llmClient) generateSummary(article wikipediaArticle, maxWords int) (string, error) {
	title := article.Title
	originalText := article.Text

	// Build prompt for summarization
	prompt := fmt.Sprintf(`You are an expert summarizer. Create a concise, informative summary of the following Wikipedia article.

Title: %s
Original Text: %s

Requirements:
- Maximum %d words
- Focus on key concepts, definitions, and important facts
- Use clear, accessible language
- Maintain accuracy and objectivity
- Include the most important information that would help someone understand the topic

Summary:`, title, originalText, maxWords)

	// Call LLM based on provider
	switch llm.provider {
	case "ollama":
		return llm.callOllama(prompt)
	case "openai":
		return llm.callOpenAI(prompt)
	default:
		return "", fmt.Errorf("unsupported LLM provider: %s", llm.provider)
	}
}

func (llm *llmClient) callOllama(prompt string) (string, error) {
	log.Printf("🤖 Calling Ollama with prompt length: %d", len(prompt))
	log.Printf("📤 Sending request to: %s", llm.endpoint)

	ctx := context.Background()
	// Use async LLM client (or sync fallback)
	response, err := async_llm.CallAsync(ctx, "ollama", llm.endpoint, llm.model, prompt, nil, async_llm.PriorityLow)
	if err != nil {
		log.Printf("❌ Ollama request failed: %v", err)
		return "", err
	}

	log.Printf("✅ Ollama response length: %d", len(response))
	return response, nil
}

func (llm *llmClient) callOpenAI(prompt string) (string, error) {
	log.Printf("🤖 Calling OpenAI with prompt length: %d", len(prompt))
	log.Printf("📤 Sending request to: %s", llm.endpoint)

	ctx := context.Background()
	// Use async LLM client (or sync fallback)
	// The async_llm client handles OpenAI endpoints (including /v1/chat/completions)
	response, err := async_llm.CallAsync(ctx, "openai", llm.endpoint, llm.model, prompt, nil, async_llm.PriorityLow)
	if err != nil {
		log.Printf("❌ OpenAI request failed: %v", err)
		return "", err
	}

	log.Printf("✅ OpenAI response length: %d", len(response))
	return response, nil
}

// Helper functions
func getStringFromPayload(payload map[string]interface{}, key string) string {
	if val, exists := payload[key]; exists {
		if str, ok := val.(string); ok {
			return str
		}
	}
	return ""
}

func getStringFromMap(m map[string]interface{}, key string) string {
	if val, exists := m[key]; exists {
		if str, ok := val.(string); ok {
			return str
		}
	}
	return ""
}

func getMapFromPayload(payload map[string]interface{}, key string) map[string]interface{} {
	if val, exists := payload[key]; exists {
		if m, ok := val.(map[string]interface{}); ok {
			return m
		}
	}
	return make(map[string]interface{})
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
