package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"strings"

	"github.com/joho/godotenv"
	"github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"
	"gopkg.in/yaml.v2"
)

// ServerConfig represents the server configuration
type ServerConfig struct {
	AgentID       string `yaml:"agent_id"`
	ConfigPath    string `yaml:"config_path"`
	HDNURL        string `yaml:"hdn_url"`
	PrinciplesURL string `yaml:"principles_url"`
	NatsURL       string `yaml:"nats_url"`
	RedisURL      string `yaml:"redis_url"`
	GoalMgrURL    string `yaml:"goal_manager_url"`
	Neo4jURI      string `yaml:"neo4j_uri"`
	Neo4jUser     string `yaml:"neo4j_user"`
	Neo4jPass     string `yaml:"neo4j_pass"`
	WeaviateURL   string `yaml:"weaviate_url"`
	OllamaURL     string `yaml:"ollama_url"`
	Autonomy      bool   `yaml:"autonomy"`
	AutonomyEvery int    `yaml:"autonomy_every_seconds"`
	// Optional: preload anchor goals on start
	AnchorGoals []struct {
		Name        string `yaml:"name"`
		Description string `yaml:"description"`
	} `yaml:"anchor_goals"`
}

// LoadServerConfig loads server configuration from file and environment
func LoadServerConfig(configPath string) (*ServerConfig, error) {
	config := &ServerConfig{
		AgentID:       "agent_1",
		ConfigPath:    configPath,
		HDNURL:        "http://localhost:8080",
		PrinciplesURL: "http://localhost:8080",
		NatsURL:       "nats://localhost:4222",
		RedisURL:      "redis://localhost:6379",
		GoalMgrURL:    "http://localhost:8090",
		Neo4jURI:      "bolt://localhost:7687",
		Neo4jUser:     "neo4j",
		Neo4jPass:     "test1234",
		WeaviateURL:   "http://localhost:8080",
		OllamaURL:     "http://localhost:11434/api/chat",
		Autonomy:      false,
		AutonomyEvery: 30,
	}

	// Load from file if exists
	if configPath != "" {
		data, err := os.ReadFile(configPath)
		if err == nil {
			if err := yaml.Unmarshal(data, config); err != nil {
				return nil, fmt.Errorf("failed to parse config file: %w", err)
			}
		}
	}

	// Override with environment variables
	if agentID := os.Getenv("FSM_AGENT_ID"); agentID != "" {
		config.AgentID = agentID
	}
	if hdnURL := os.Getenv("HDN_URL"); hdnURL != "" {
		config.HDNURL = hdnURL
	}
	if principlesURL := os.Getenv("PRINCIPLES_URL"); principlesURL != "" {
		config.PrinciplesURL = principlesURL
	}
	if natsURL := os.Getenv("NATS_URL"); natsURL != "" {
		config.NatsURL = natsURL
	}
	if redisURL := os.Getenv("REDIS_URL"); redisURL != "" {
		config.RedisURL = redisURL
	}
	if gm := os.Getenv("GOAL_MANAGER_URL"); gm != "" {
		config.GoalMgrURL = gm
	}
	if neo4jURI := os.Getenv("NEO4J_URI"); neo4jURI != "" {
		config.Neo4jURI = neo4jURI
	}
	if neo4jUser := os.Getenv("NEO4J_USER"); neo4jUser != "" {
		config.Neo4jUser = neo4jUser
	}
	if neo4jPass := os.Getenv("NEO4J_PASS"); neo4jPass != "" {
		config.Neo4jPass = neo4jPass
	}
	if weaviateURL := os.Getenv("WEAVIATE_URL"); weaviateURL != "" {
		config.WeaviateURL = weaviateURL
	}
	if ollamaURL := os.Getenv("OLLAMA_URL"); ollamaURL != "" {
		config.OllamaURL = ollamaURL
	}
	// Check both FSM_AUTONOMY and AUTONOMY for compatibility
	if auto := os.Getenv("FSM_AUTONOMY"); strings.TrimSpace(auto) != "" {
		config.Autonomy = strings.ToLower(auto) == "true"
	} else if auto := os.Getenv("AUTONOMY"); strings.TrimSpace(auto) != "" {
		// Fallback to AUTONOMY for backward compatibility
		config.Autonomy = strings.ToLower(auto) == "true"
	}
	if every := os.Getenv("FSM_AUTONOMY_EVERY"); strings.TrimSpace(every) != "" {
		if v, err := strconv.Atoi(every); err == nil && v > 0 {
			config.AutonomyEvery = v
		}
	}

	// Normalize Redis host to IPv4 if localhost to avoid ::1 resolution issues on some hosts
	if strings.Contains(config.RedisURL, "localhost") {
		config.RedisURL = strings.ReplaceAll(config.RedisURL, "localhost", "127.0.0.1")
	}

	return config, nil
}

func main() {
	// Load .env file if it exists - look in project root
	// Try multiple locations: AGI_PROJECT_ROOT, current dir, or walk up from binary/working dir
	var envPath string
	
	// Check AGI_PROJECT_ROOT environment variable first (most reliable)
	if projectRoot := os.Getenv("AGI_PROJECT_ROOT"); projectRoot != "" {
		candidate := filepath.Join(projectRoot, ".env")
		if _, err := os.Stat(candidate); err == nil {
			envPath = candidate
		}
	}
	
	// If not found, try walking up from executable location
	if envPath == "" {
		if execPath, err := os.Executable(); err == nil {
			dir := filepath.Dir(execPath)
			for dir != filepath.Dir(dir) {
				candidate := filepath.Join(dir, ".env")
				if _, err := os.Stat(candidate); err == nil {
					envPath = candidate
					break
				}
				dir = filepath.Dir(dir)
			}
		}
	}
	
	// If still not found, try current working directory and walk up
	if envPath == "" {
		if wd, err := os.Getwd(); err == nil {
			dir := wd
			for dir != filepath.Dir(dir) {
				candidate := filepath.Join(dir, ".env")
				if _, err := os.Stat(candidate); err == nil {
					envPath = candidate
					break
				}
				dir = filepath.Dir(dir)
			}
		}
	}
	
	// Fallback to current directory
	if envPath == "" {
		envPath = ".env"
	}
	
	// Try loading the .env file
	if err := godotenv.Load(envPath); err != nil {
		// .env file is optional, so we don't treat this as an error
		log.Printf("No .env file found or error loading: %v", err)
	} else {
		log.Printf("Loaded .env file from: %s", envPath)
	}

	var (
		configPath = flag.String("config", "", "Path to configuration file")
		agentID    = flag.String("agent", "agent_1", "Agent ID")
		verbose    = flag.Bool("verbose", false, "Enable verbose logging")
	)
	flag.Parse()

	if *verbose {
		log.SetFlags(log.LstdFlags | log.Lshortfile)
	}

	// Load configuration
	config, err := LoadServerConfig(*configPath)
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	if *agentID != "" {
		config.AgentID = *agentID
	}

	log.Printf("Starting FSM server for agent: %s", config.AgentID)
	log.Printf("Configuration: HDN=%s, Principles=%s, NATS=%s, Redis=%s",
		config.HDNURL, config.PrinciplesURL, config.NatsURL, config.RedisURL)

	// Log bootstrap configuration (from environment) for visibility
	{
		seeds := os.Getenv("FSM_BOOTSTRAP_SEEDS")
		depth := os.Getenv("FSM_BOOTSTRAP_MAX_DEPTH")
		nodes := os.Getenv("FSM_BOOTSTRAP_MAX_NODES")
		rpm := os.Getenv("FSM_BOOTSTRAP_RPM")
		batch := os.Getenv("FSM_BOOTSTRAP_SEED_BATCH")
		cooldown := os.Getenv("FSM_BOOTSTRAP_COOLDOWN_HOURS")
		if strings.TrimSpace(seeds) == "" {
			seeds = "(unset)"
		}
		if strings.TrimSpace(depth) == "" {
			depth = "(unset)"
		}
		if strings.TrimSpace(nodes) == "" {
			nodes = "(unset)"
		}
		if strings.TrimSpace(rpm) == "" {
			rpm = "(unset)"
		}
		if strings.TrimSpace(batch) == "" {
			batch = "(unset)"
		}
		if strings.TrimSpace(cooldown) == "" {
			cooldown = "(unset)"
		}
		log.Printf("Bootstrap: seeds=%q max_depth=%s max_nodes=%s rpm=%s seed_batch=%s cooldown_hours=%s", seeds, depth, nodes, rpm, batch, cooldown)
	}

	// Connect to NATS with reconnection options
	opts := nats.GetDefaultOptions()
	opts.Url = config.NatsURL
	opts.MaxReconnect = -1 // Unlimited reconnection attempts
	opts.ReconnectWait = 2 * time.Second
	opts.Timeout = 10 * time.Second
	opts.PingInterval = 30 * time.Second
	opts.MaxPingsOut = 3

	nc, err := opts.Connect()
	if err != nil {
		log.Fatalf("Failed to connect to NATS: %v", err)
	}
	defer nc.Close()

	// Set up connection event handlers
	nc.SetDisconnectHandler(func(nc *nats.Conn) {
		log.Printf("⚠️  NATS disconnected")
	})
	nc.SetReconnectHandler(func(nc *nats.Conn) {
		log.Printf("🔄 NATS reconnected to %s", nc.ConnectedUrl())
	})
	nc.SetClosedHandler(func(nc *nats.Conn) {
		log.Printf("❌ NATS connection closed")
	})

	// Connect to Redis
	opt, err := redis.ParseURL(config.RedisURL)
	if err != nil {
		log.Fatalf("Failed to parse Redis URL: %v", err)
	}
	rdb := redis.NewClient(opt)
	defer rdb.Close()

	// Test connections
	ctx := context.Background()
	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Fatalf("Failed to connect to Redis: %v", err)
	}

	// Create FSM engine
	engine, err := NewFSMEngine(config.ConfigPath, config.AgentID, nc, rdb, config.PrinciplesURL, config.HDNURL)
	if err != nil {
		log.Fatalf("Failed to create FSM engine: %v", err)
	}

	// Create knowledge integration
	knowledgeIntegration := NewKnowledgeIntegration(
		config.HDNURL,
		config.PrinciplesURL,
		rdb,
	)

	// Start FSM engine
	if err := engine.Start(); err != nil {
		log.Fatalf("Failed to start FSM engine: %v", err)
	}

	// Preload Anchor Goals into Redis for autonomy fallback
	if len(config.AnchorGoals) > 0 {
		for _, ag := range config.AnchorGoals {
			item := map[string]string{"name": ag.Name, "description": ag.Description}
			b, _ := json.Marshal(item)
			// keep a rolling list of anchors
			rdb.LPush(context.Background(), "reasoning:anchors:all", b)
			rdb.LTrim(context.Background(), "reasoning:anchors:all", 0, 99)
		}
		log.Printf("📌 Loaded %d anchor goals", len(config.AnchorGoals))
	}

	// Set up graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Create FSM monitor
	fsmMonitor := NewFSMMonitor(engine, rdb)

	// Start monitoring HTTP server
	go startMonitoringServer(fsmMonitor, config.AgentID)

	// Start monitoring goroutine
	go monitorFSM(engine, knowledgeIntegration)

	// Start goals poller to drive actions from Goal Manager (use GoalMgrURL, not HDN)
	go startGoalsPoller(config.AgentID, config.GoalMgrURL, rdb)

	// Optional autonomy scheduler
	if config.Autonomy {
		log.Printf("🤖 Autonomy enabled: interval=%ds", config.AutonomyEvery)
		// Fire an initial cycle shortly after start for faster feedback
		time.AfterFunc(2*time.Second, func() { engine.TriggerAutonomyCycle() })
		go func() {
			ticker := time.NewTicker(time.Duration(config.AutonomyEvery) * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					engine.TriggerAutonomyCycle()
				case <-engine.ctx.Done():
					return
				}
			}
		}()
	} else {
		log.Printf("🤖 Autonomy disabled")
	}

	// Dream Mode: Generate creative exploration goals by connecting random concepts
	dreamMode := NewDreamMode(engine, rdb, config.HDNURL)
	dreamInterval := 15 * time.Minute // Dream every 15 minutes
	if envInterval := os.Getenv("DREAM_INTERVAL_MINUTES"); envInterval != "" {
		if mins, err := strconv.Atoi(envInterval); err == nil && mins > 0 {
			dreamInterval = time.Duration(mins) * time.Minute
		}
	}
	go dreamMode.StartDreamCycle(dreamInterval)

	// Nightly scheduler: trigger daily_summary at 02:30 UTC every day
	go func(hdnURL string) {
		for {
			// Compute next 02:30 UTC
			now := time.Now().UTC()
			next := time.Date(now.Year(), now.Month(), now.Day(), 2, 30, 0, 0, time.UTC)
			if !next.After(now) {
				next = next.Add(24 * time.Hour)
			}
			d := time.Until(next)
			log.Printf("⏰ [sleep_cron] Next daily_summary at %s (in %s)", next.Format(time.RFC3339), d.String())
			t := time.NewTimer(d)
			select {
			case <-t.C:
				// Fire request to HDN intelligent execute for daily_summary
				payload := map[string]interface{}{
					"task_name":   "daily_summary",
					"description": "Summarize the day: key discoveries, actions, and questions",
					"context": map[string]string{
						"session_id":         "autonomy_daily",
						"prefer_traditional": "true",
					},
					"language": "python",
				}
				b, _ := json.Marshal(payload)
				url := strings.TrimRight(hdnURL, "/") + "/api/v1/intelligent/execute"
				req, _ := http.NewRequest("POST", url, strings.NewReader(string(b)))
				req.Header.Set("Content-Type", "application/json")
				client := &http.Client{Timeout: 90 * time.Second}
				if resp, err := client.Do(req); err != nil {
					log.Printf("❌ [sleep_cron] daily_summary trigger failed: %v", err)
				} else {
					io.Copy(io.Discard, resp.Body)
					resp.Body.Close()
					log.Printf("✅ [sleep_cron] daily_summary triggered (status=%d)", resp.StatusCode)
				}
			case <-engine.ctx.Done():
				return
			}
		}
	}(config.HDNURL)

	// Hourly news ingestion scheduler: run BBC news ingestor every hour
	go func(natsURL string) {
		// Check if news poller is disabled
		if os.Getenv("DISABLE_NEWS_POLLER") == "true" {
			log.Printf("📰 [news_cron] News poller disabled via DISABLE_NEWS_POLLER=true")
			return
		}

		// Wait 30 seconds after startup before first run
		time.Sleep(30 * time.Second)

		// Get configuration from environment
		projectRoot := os.Getenv("AGI_PROJECT_ROOT")
		if projectRoot == "" {
			projectRoot = "." // Default to current directory
		}

		ollamaURL := os.Getenv("OLLAMA_URL")
		if ollamaURL == "" {
			ollamaURL = "http://localhost:11434/api/chat"
		}

		ollamaModel := os.Getenv("OLLAMA_MODEL")
		if ollamaModel == "" {
			ollamaModel = "gemma3n"
		}

		batchSize := os.Getenv("NEWS_BATCH_SIZE")
		if batchSize == "" {
			batchSize = "10"
		}

		maxStories := os.Getenv("NEWS_MAX_STORIES")
		if maxStories == "" {
			maxStories = "30"
		}

		log.Printf("📰 [news_cron] Configuration: project_root=%s, ollama_url=%s, ollama_model=%s, batch_size=%s, max_stories=%s",
			projectRoot, ollamaURL, ollamaModel, batchSize, maxStories)

		for {
			log.Printf("📰 [news_cron] Starting hourly BBC news ingestion")

			// Run the BBC news ingestor binary
			cmd := exec.Command(filepath.Join(projectRoot, "bin", "bbc-news-ingestor"),
				"-llm",
				"-batch-size", batchSize,
				"-llm-model", ollamaModel,
				"-ollama-url", ollamaURL,
				"-max", maxStories)

			// Set environment variables
			cmd.Env = append(os.Environ(),
				"OLLAMA_URL="+ollamaURL,
				"OLLAMA_MODEL="+ollamaModel,
				"NATS_URL="+natsURL,
			)

			// Capture output
			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr

			// Run with timeout
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			cmd = exec.CommandContext(ctx, cmd.Path, cmd.Args[1:]...)
			cmd.Env = cmd.Env
			cmd.Dir = cmd.Dir
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr

			if err := cmd.Run(); err != nil {
				log.Printf("❌ [news_cron] BBC news ingestion failed: %v", err)
				log.Printf("   stdout: %s", stdout.String())
				log.Printf("   stderr: %s", stderr.String())
			} else {
				log.Printf("✅ [news_cron] BBC news ingestion completed successfully")
				if stdout.Len() > 0 {
					log.Printf("   output: %s", stdout.String())
				}
			}

			// Wait for next hour
			now := time.Now()
			next := now.Truncate(time.Hour).Add(time.Hour)
			d := time.Until(next)
			log.Printf("⏰ [news_cron] Next news ingestion at %s (in %s)", next.Format(time.RFC3339), d.String())

			select {
			case <-time.After(d):
				// Continue to next iteration
			case <-engine.ctx.Done():
				return
			}
		}
	}(config.NatsURL)

	// Wait for shutdown signal
	<-sigChan
	log.Println("Shutting down FSM server...")

	// Stop FSM engine
	if err := engine.Stop(); err != nil {
		log.Printf("Error stopping FSM engine: %v", err)
	}

	log.Println("FSM server stopped")
}

// monitorFSM monitors the FSM and publishes metrics
func monitorFSM(engine *FSMEngine, knowledgeIntegration *KnowledgeIntegration) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// Publish FSM metrics
			publishFSMMetrics(engine)
		}
	}
}

// publishFSMMetrics publishes FSM metrics to NATS
func publishFSMMetrics(engine *FSMEngine) {
	metrics := map[string]interface{}{
		"agent_id":      engine.agentID,
		"current_state": engine.GetCurrentState(),
		"timestamp":     time.Now().Unix(),
		"context":       engine.GetContext(),
	}

	data, _ := json.Marshal(metrics)
	engine.nc.Publish("agi.events.fsm.metrics", data)
}

// Example usage and testing functions
func runExampleScenario(engine *FSMEngine, knowledgeIntegration *KnowledgeIntegration) {
	log.Println("Running example scenario: 'Summarize latest logs and propose next fix'")

	// Simulate user input
	userInput := "Summarize latest logs and propose next fix"

	// Classify domain
	classification, err := knowledgeIntegration.ClassifyDomain(userInput)
	if err != nil {
		log.Printf("Domain classification failed: %v", err)
		return
	}
	log.Printf("Domain classified as: %s (confidence: %.2f)", classification.Domain, classification.Confidence)

	// Extract facts
	facts, err := knowledgeIntegration.ExtractFacts(userInput, classification.Domain)
	if err != nil {
		log.Printf("Fact extraction failed: %v", err)
		return
	}
	log.Printf("Extracted %d facts", len(facts))

	// Generate hypotheses
	hypotheses, err := knowledgeIntegration.GenerateHypotheses(facts, classification.Domain)
	if err != nil {
		log.Printf("Hypothesis generation failed: %v", err)
		return
	}
	log.Printf("Generated %d hypotheses", len(hypotheses))

	// Create plans for each hypothesis
	for _, hypothesis := range hypotheses {
		plan, err := knowledgeIntegration.CreatePlan(hypothesis, classification.Domain)
		if err != nil {
			log.Printf("Plan creation failed for hypothesis %s: %v", hypothesis.ID, err)
			continue
		}

		// Check principles
		allowed, err := knowledgeIntegration.CheckPrinciples(plan)
		if err != nil {
			log.Printf("Principles check failed for plan %s: %v", plan.ID, err)
			continue
		}

		if allowed {
			log.Printf("Plan %s is allowed by principles (expected value: %.2f, risk: %.2f)",
				plan.ID, plan.ExpectedValue, plan.Risk)
		} else {
			log.Printf("Plan %s is blocked by principles", plan.ID)
		}
	}
}

// startMonitoringServer starts the HTTP monitoring server
func startMonitoringServer(monitor *FSMMonitor, agentID string) {
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		// Lightweight health check - just verify service is running and Redis is accessible
		// Use a short timeout context to prevent health check from hanging
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		// Quick Redis ping to verify connectivity
		if err := monitor.redis.Ping(ctx).Err(); err != nil {
			http.Error(w, fmt.Sprintf("Redis health check failed: %v", err), http.StatusServiceUnavailable)
			return
		}

		// Get basic info without expensive queries
		currentState := monitor.fsmEngine.GetCurrentState()

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":        "healthy",
			"agent_id":      agentID,
			"current_state": currentState,
			"timestamp":     time.Now().Unix(),
		})
	})

	http.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		status, err := monitor.GetFSMStatus()
		if err != nil {
			http.Error(w, "Failed to get FSM status", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(status)
	})

	http.HandleFunc("/thinking", func(w http.ResponseWriter, r *http.Request) {
		thinking, err := monitor.GetThinkingProcess(agentID)
		if err != nil {
			http.Error(w, "Failed to get thinking process", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(thinking)
	})

	http.HandleFunc("/timeline", func(w http.ResponseWriter, r *http.Request) {
		hours := 24 // Default to last 24 hours
		if h := r.URL.Query().Get("hours"); h != "" {
			if parsed, err := strconv.Atoi(h); err == nil && parsed > 0 {
				hours = parsed
			}
		}

		timeline, err := monitor.GetStateTimeline(agentID, hours)
		if err != nil {
			http.Error(w, "Failed to get state timeline", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(timeline)
	})

	http.HandleFunc("/knowledge-growth", func(w http.ResponseWriter, r *http.Request) {
		hours := 24 // Default to last 24 hours
		if h := r.URL.Query().Get("hours"); h != "" {
			if parsed, err := strconv.Atoi(h); err == nil && parsed > 0 {
				hours = parsed
			}
		}

		growth, err := monitor.GetKnowledgeGrowthTimeline(agentID, hours)
		if err != nil {
			http.Error(w, "Failed to get knowledge growth timeline", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(growth)
	})

	http.HandleFunc("/hypotheses", func(w http.ResponseWriter, r *http.Request) {
		hypotheses, err := monitor.GetActiveHypotheses(agentID)
		if err != nil {
			http.Error(w, "Failed to get active hypotheses", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(hypotheses)
	})

	http.HandleFunc("/episodes", func(w http.ResponseWriter, r *http.Request) {
		limit := 10 // Default limit
		if l := r.URL.Query().Get("limit"); l != "" {
			if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
				limit = parsed
			}
		}

		episodes, err := monitor.GetRecentEpisodes(agentID, limit)
		if err != nil {
			http.Error(w, "Failed to get recent episodes", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(episodes)
	})

	// Activity log endpoint - shows what the system is doing in plain English
	http.HandleFunc("/activity", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// Get limit from query param (default 50)
		limitStr := r.URL.Query().Get("limit")
		limit := 50
		if limitStr != "" {
			if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 && parsed <= 200 {
				limit = parsed
			}
		}

		// Get agent ID from context or query param
		agent := agentID
		if agentParam := r.URL.Query().Get("agent_id"); agentParam != "" {
			agent = agentParam
		}

		// Retrieve activity log from Redis
		ctx := context.Background()
		key := fmt.Sprintf("fsm:%s:activity_log", agent)
		entries, err := monitor.redis.LRange(ctx, key, 0, int64(limit-1)).Result()
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to get activity log: %v", err), http.StatusInternalServerError)
			return
		}

		// Parse entries
		activities := []map[string]interface{}{}
		for _, entry := range entries {
			var activity map[string]interface{}
			if err := json.Unmarshal([]byte(entry), &activity); err == nil {
				activities = append(activities, activity)
			}
		}

		// Return as JSON
		json.NewEncoder(w).Encode(map[string]interface{}{
			"activities": activities,
			"count":      len(activities),
			"agent_id":   agent,
		})
	})

	// Reset FSM state to idle
	http.HandleFunc("/reset", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Force transition to idle state
		if err := monitor.ForceStateTransition("idle"); err != nil {
			http.Error(w, fmt.Sprintf("Failed to reset FSM state: %v", err), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "success",
			"message": "FSM state reset to idle",
		})
	})

	// Serve static monitoring dashboard
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			serveMonitoringDashboard(w, r, monitor, agentID)
		} else {
			http.NotFound(w, r)
		}
	})

	port := ":8083"
	log.Printf("Starting monitoring server on port %s", port)
	log.Printf("Monitoring dashboard available at: http://localhost%s/", port)
	log.Printf("API endpoints:")
	log.Printf("  GET /health - Health check")
	log.Printf("  GET /status - Full FSM status")
	log.Printf("  GET /thinking - Current thinking process")
	log.Printf("  GET /timeline?hours=24 - State transition timeline")
	log.Printf("  GET /knowledge-growth?hours=24 - Knowledge growth timeline")
	log.Printf("  GET /hypotheses - Active hypotheses")
	log.Printf("  GET /episodes?limit=10 - Recent episodes")
	log.Printf("  GET /activity?limit=50 - Recent activity log (what the system is doing)")

	if err := http.ListenAndServe(port, nil); err != nil {
		log.Printf("Monitoring server error: %v", err)
	}
}

// serveMonitoringDashboard serves a simple HTML dashboard
func serveMonitoringDashboard(w http.ResponseWriter, r *http.Request, monitor *FSMMonitor, agentID string) {
	status, err := monitor.GetFSMStatus()
	if err != nil {
		http.Error(w, "Failed to get FSM status", http.StatusInternalServerError)
		return
	}

	thinking, err := monitor.GetThinkingProcess(agentID)
	if err != nil {
		http.Error(w, "Failed to get thinking process", http.StatusInternalServerError)
		return
	}

	// Get additional data for the dashboard
	stateHistory, err := monitor.GetStateTimeline(agentID, 24)
	if err != nil {
		log.Printf("Warning: Could not get state history: %v", err)
		stateHistory = []StateTransition{}
	}

	hypotheses, err := monitor.GetActiveHypotheses(agentID)
	if err != nil {
		log.Printf("Warning: Could not get hypotheses: %v", err)
		hypotheses = []map[string]interface{}{}
	}

	episodes, err := monitor.GetRecentEpisodes(agentID, 20)
	if err != nil {
		log.Printf("Warning: Could not get episodes: %v", err)
		episodes = []map[string]interface{}{}
	}

	html := fmt.Sprintf(`
<!DOCTYPE html>
<html>
<head>
    <title>FSM Monitor - %s</title>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <style>
        body { font-family: Arial, sans-serif; margin: 20px; background: #f5f5f5; }
        .container { max-width: 1200px; margin: 0 auto; }
        .card { background: white; padding: 20px; margin: 10px 0; border-radius: 8px; box-shadow: 0 2px 4px rgba(0,0,0,0.1); }
        .status { display: flex; justify-content: space-between; align-items: center; }
        .health { padding: 5px 10px; border-radius: 4px; color: white; font-weight: bold; }
        .healthy { background: #28a745; }
        .degraded { background: #ffc107; color: black; }
        .unhealthy { background: #dc3545; }
        .idle { background: #6c757d; }
        .metric { display: inline-block; margin: 10px 20px 10px 0; }
        .metric-label { font-size: 0.9em; color: #666; }
        .metric-value { font-size: 1.2em; font-weight: bold; }
        .thinking { background: #e3f2fd; }
        .state-description { font-style: italic; color: #666; margin-top: 5px; }
        .next-actions { margin-top: 10px; }
        .action { display: inline-block; background: #007bff; color: white; padding: 4px 8px; margin: 2px; border-radius: 4px; font-size: 0.9em; }
        pre { background: #f8f9fa; padding: 10px; border-radius: 4px; overflow-x: auto; }
        .refresh { float: right; background: #007bff; color: white; padding: 8px 16px; border: none; border-radius: 4px; cursor: pointer; }
        .refresh:hover { background: #0056b3; }
        
        /* Scrolling containers */
        .scrollable { 
            max-height: 300px; 
            overflow-y: auto; 
            border: 1px solid #ddd; 
            border-radius: 4px; 
            padding: 10px; 
            background: #fafafa;
        }
        .scrollable::-webkit-scrollbar { width: 8px; }
        .scrollable::-webkit-scrollbar-track { background: #f1f1f1; border-radius: 4px; }
        .scrollable::-webkit-scrollbar-thumb { background: #c1c1c1; border-radius: 4px; }
        .scrollable::-webkit-scrollbar-thumb:hover { background: #a8a8a8; }
        
        .data-grid { 
            display: grid; 
            grid-template-columns: repeat(auto-fit, minmax(200px, 1fr)); 
            gap: 10px; 
            margin-top: 10px; 
        }
        .data-item { 
            background: white; 
            padding: 10px; 
            border-radius: 4px; 
            border: 1px solid #e0e0e0; 
            font-size: 0.9em; 
        }
        .data-item-header { 
            font-weight: bold; 
            color: #333; 
            margin-bottom: 5px; 
        }
        .data-item-content { 
            color: #666; 
            word-break: break-word; 
        }
        
        .timeline-item { 
            border-left: 3px solid #007bff; 
            padding-left: 10px; 
            margin-bottom: 10px; 
            background: white; 
            padding: 10px; 
            border-radius: 0 4px 4px 0; 
        }
        .timeline-item-header { 
            font-weight: bold; 
            color: #007bff; 
        }
        .timeline-item-time { 
            font-size: 0.8em; 
            color: #666; 
        }
        .timeline-item-details { 
            margin-top: 5px; 
            font-size: 0.9em; 
            color: #555; 
        }
    </style>
    <script>
        function refreshPage() { location.reload(); }
        setInterval(refreshPage, 5000); // Auto-refresh every 5 seconds
    </script>
</head>
<body>
    <div class="container">
        <h1>FSM Monitor - Agent: %s</h1>
        
        <div class="card">
            <div class="status">
                <h2>System Status</h2>
                <span class="health %s">%s</span>
            </div>
            <div class="metric">
                <div class="metric-label">Current State</div>
                <div class="metric-value">%s</div>
            </div>
            <div class="metric">
                <div class="metric-label">Uptime</div>
                <div class="metric-value">%s</div>
            </div>
            <div class="metric">
                <div class="metric-label">Last Activity</div>
                <div class="metric-value">%s</div>
            </div>
            <div class="state-description">%s</div>
        </div>

        <div class="card">
            <h3>Performance Metrics</h3>
            <div class="metric">
                <div class="metric-label">Transitions/sec</div>
                <div class="metric-value">%.2f</div>
            </div>
            <div class="metric">
                <div class="metric-label">Events Processed</div>
                <div class="metric-value">%d</div>
            </div>
            <div class="metric">
                <div class="metric-label">Avg State Time</div>
                <div class="metric-value">%.2fs</div>
            </div>
            <div class="metric">
                <div class="metric-label">Error Rate</div>
                <div class="metric-value">%.2f%%</div>
            </div>
        </div>

        <div class="card">
            <h3>HDN Delegation</h3>
            <div class="metric">
                <div class="metric-label">Inflight</div>
                <div class="metric-value">%v</div>
            </div>
            <div class="metric">
                <div class="metric-label">Started At</div>
                <div class="metric-value">%s</div>
            </div>
            <div class="metric">
                <div class="metric-label">Last Error</div>
                <div class="metric-value">%s</div>
            </div>
        </div>

        <div class="card">
            <h3>Last Execution</h3>
            <div class="scrollable">
                <pre>%s</pre>
            </div>
        </div>

        <div class="card">
            <h3>Knowledge Growth</h3>
            <div class="metric">
                <div class="metric-label">Concepts Created</div>
                <div class="metric-value">%d</div>
            </div>
            <div class="metric">
                <div class="metric-label">Relationships Added</div>
                <div class="metric-value">%d</div>
            </div>
            <div class="metric">
                <div class="metric-label">Growth Rate</div>
                <div class="metric-value">%.2f%%</div>
            </div>
            <div class="metric">
                <div class="metric-label">Consistency Score</div>
                <div class="metric-value">%.2f</div>
            </div>
        </div>

        <div class="card">
            <h3>Principles Checks</h3>
            <div class="metric">
                <div class="metric-label">Total Checks</div>
                <div class="metric-value">%d</div>
            </div>
            <div class="metric">
                <div class="metric-label">Allowed Actions</div>
                <div class="metric-value">%d</div>
            </div>
            <div class="metric">
                <div class="metric-label">Blocked Actions</div>
                <div class="metric-value">%d</div>
            </div>
            <div class="metric">
                <div class="metric-label">Avg Response Time</div>
                <div class="metric-value">%.2fms</div>
            </div>
        </div>

        <div class="card thinking">
            <h3>Current Thinking Process</h3>
            <p><strong>Focus:</strong> %s</p>
            <p><strong>Confidence Level:</strong> %.2f</p>
            <div class="next-actions">
                <strong>Next Actions:</strong>
                %s
            </div>
        </div>

        <div class="card">
            <h3>State History</h3>
            <div class="scrollable">
                %s
            </div>
        </div>

        <div class="card">
            <h3>Active Hypotheses</h3>
            <div class="scrollable">
                %s
            </div>
        </div>

        <div class="card">
            <h3>Recent Episodes</h3>
            <div class="scrollable">
                %s
            </div>
        </div>

        <div class="card">
            <h3>Raw Status Data</h3>
            <button class="refresh" onclick="refreshPage()">Refresh</button>
            <div class="scrollable">
                <pre>%s</pre>
            </div>
        </div>
    </div>
</body>
</html>`,
		agentID,
		agentID,
		status.HealthStatus,
		status.HealthStatus,
		status.CurrentState,
		status.Uptime.String(),
		status.LastActivity.Format("2006-01-02 15:04:05"),
		getStateDescription(status.CurrentState),
		status.Performance.TransitionsPerSecond,
		status.Performance.EventsProcessed,
		status.Performance.AverageStateTime,
		status.Performance.ErrorRate*100,
		status.Context["hdn_inflight"],
		valueOrString(status.Context["hdn_started_at"]),
		valueOrString(status.Context["hdn_last_error"]),
		formatJSON(status.Context["last_execution"]),
		status.KnowledgeGrowth.ConceptsCreated,
		status.KnowledgeGrowth.RelationshipsAdded,
		status.KnowledgeGrowth.GrowthRate,
		status.KnowledgeGrowth.ConsistencyScore,
		status.PrinciplesChecks.TotalChecks,
		status.PrinciplesChecks.AllowedActions,
		status.PrinciplesChecks.BlockedActions,
		status.PrinciplesChecks.AverageResponseTime,
		thinking["thinking_focus"],
		thinking["confidence_level"],
		formatActions(thinking["next_actions"]),
		formatStateHistory(stateHistory),
		formatHypotheses(hypotheses),
		formatEpisodes(episodes),
		formatJSON(status),
	)

	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(html))
}

// getStateDescription returns a human-readable description of the FSM state
func getStateDescription(state string) string {
	descriptions := map[string]string{
		"idle":        "Waiting for input or timer events",
		"perceive":    "Ingesting and validating new data using domain knowledge",
		"learn":       "Extracting facts and updating domain knowledge - GROWING KNOWLEDGE BASE",
		"summarize":   "Compressing episodes into structured facts",
		"hypothesize": "Generating hypotheses using domain knowledge and constraints",
		"plan":        "Creating hierarchical plans using domain-specific success rates",
		"decide":      "Choosing action using principles and domain constraints - CHECKING PRINCIPLES",
		"act":         "Executing planned action with domain-aware monitoring",
		"observe":     "Collecting outcomes and validating against domain expectations",
		"evaluate":    "Comparing outcomes to domain knowledge and updating beliefs - GROWING KNOWLEDGE BASE",
		"archive":     "Checkpointing episode and updating domain knowledge",
		"fail":        "Handling errors with domain-aware recovery",
		"paused":      "Manual pause state",
		"shutdown":    "Clean shutdown with knowledge base preservation",
	}

	if desc, exists := descriptions[state]; exists {
		return desc
	}
	return "Unknown state"
}

// formatActions formats the next actions for display
func formatActions(actions interface{}) string {
	if actions == nil {
		return "None"
	}

	actionsList, ok := actions.([]string)
	if !ok {
		return "Unknown"
	}

	var result string
	for _, action := range actionsList {
		result += fmt.Sprintf(`<span class="action">%s</span>`, action)
	}
	return result
}

// formatJSON formats data as pretty JSON
func formatJSON(data interface{}) string {
	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Sprintf("Error formatting JSON: %v", err)
	}
	return string(jsonData)
}

// valueOrString safely converts interface{} to string for HTML rendering
func valueOrString(v interface{}) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
}

// formatStateHistory formats state transitions for display
func formatStateHistory(transitions []StateTransition) string {
	if len(transitions) == 0 {
		return "<p>No state transitions recorded</p>"
	}

	var result string
	for _, transition := range transitions {
		result += fmt.Sprintf(`
			<div class="timeline-item">
				<div class="timeline-item-header">%s → %s</div>
				<div class="timeline-item-time">%s</div>
				<div class="timeline-item-details">%s</div>
			</div>`,
			transition.From,
			transition.To,
			transition.Timestamp.Format("2006-01-02 15:04:05"),
			transition.Reason,
		)
	}
	return result
}

// formatHypotheses formats hypotheses for display
func formatHypotheses(hypotheses []map[string]interface{}) string {
	if len(hypotheses) == 0 {
		return "<p>No active hypotheses</p>"
	}

	var result string
	for i, hypothesis := range hypotheses {
		id := fmt.Sprintf("hypothesis_%d", i)
		if hID, ok := hypothesis["id"].(string); ok {
			id = hID
		}

		description := "No description"
		if desc, ok := hypothesis["description"].(string); ok {
			description = desc
		}

		confidence := "Unknown"
		if conf, ok := hypothesis["confidence"].(float64); ok {
			confidence = fmt.Sprintf("%.2f", conf)
		}

		status := "Unknown"
		if stat, ok := hypothesis["status"].(string); ok {
			status = stat
		}

		result += fmt.Sprintf(`
			<div class="data-item">
				<div class="data-item-header">%s</div>
				<div class="data-item-content">
					<strong>Status:</strong> %s<br>
					<strong>Confidence:</strong> %s<br>
					<strong>Description:</strong> %s
				</div>
			</div>`,
			id,
			status,
			confidence,
			description,
		)
	}
	return result
}

// formatEpisodes formats episodes for display
func formatEpisodes(episodes []map[string]interface{}) string {
	if len(episodes) == 0 {
		return "<p>No recent episodes</p>"
	}

	var result string
	for i, episode := range episodes {
		id := fmt.Sprintf("episode_%d", i)
		if eID, ok := episode["id"].(string); ok {
			id = eID
		}

		summary := "No summary"
		if summ, ok := episode["summary"].(string); ok {
			summary = summ
		}

		timestamp := "Unknown time"
		if ts, ok := episode["timestamp"].(string); ok {
			timestamp = ts
		}

		outcome := "Unknown"
		if out, ok := episode["outcome"].(string); ok {
			outcome = out
		}

		result += fmt.Sprintf(`
			<div class="data-item">
				<div class="data-item-header">%s</div>
				<div class="data-item-content">
					<strong>Time:</strong> %s<br>
					<strong>Outcome:</strong> %s<br>
					<strong>Summary:</strong> %s
				</div>
			</div>`,
			id,
			timestamp,
			outcome,
			summary,
		)
	}
	return result
}
