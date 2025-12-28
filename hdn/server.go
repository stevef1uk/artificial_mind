package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"

	selfmodel "agi/self"
	mempkg "hdn/memory"

	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"
)

type ServerConfig struct {
	LLMProvider string            `json:"llm_provider"`
	LLMAPIKey   string            `json:"llm_api_key"`
	MCPEndpoint string            `json:"mcp_endpoint"`
	Settings    map[string]string `json:"settings"`
	Server      struct {
		Port int    `json:"port"`
		Host string `json:"host"`
	} `json:"server"`
}

// LegacyDomain represents the old domain format for backward compatibility
type LegacyDomain struct {
	Methods []MethodDef `json:"methods"`
	Actions []ActionDef `json:"actions"`
}

func main() {
	// Load .env file if it exists (before parsing flags so env vars can override)
	if err := loadEnvFile(); err != nil {
		log.Printf("Note: Could not load .env file: %v (continuing without it)", err)
	}

	// Parse command line flags
	var (
		configPath = flag.String("config", "config.json", "Path to configuration file")
		domainPath = flag.String("domain", "domain.json", "Path to domain file")
		port       = flag.Int("port", 8080, "Port to run the server on")
		mode       = flag.String("mode", "server", "Mode: server, cli, principles-test, or test-llm")
	)
	flag.Parse()

	// Load configuration
	config, err := loadConfig(*configPath)
	if err != nil {
		log.Printf("Warning: Could not load config: %v", err)
		config = &ServerConfig{
			LLMProvider: "mock",
			MCPEndpoint: "mock://localhost:3000/mcp",
			Settings:    make(map[string]string),
		}
		config.Server.Port = *port
	}

	// Override via environment variables
	applyEnvOverrides(config)

	// Override port if specified
	if *port != 8080 {
		config.Server.Port = *port
	}

	if *mode == "server" {
		// Start API server
		startAPIServer(*domainPath, config)
	} else if *mode == "principles-test" {
		// Run principles integration test
		TestPrinciplesIntegration()
	} else if *mode == "test-llm" {
		// Run LLM integration test
		TestLLMIntegration()
	} else {
		// Run CLI mode (original behavior)
		runCLI(*domainPath)
	}
}

func loadConfig(path string) (*ServerConfig, error) {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var config ServerConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	return &config, nil
}

// applyEnvOverrides allows environment variables to override key LLM settings
func applyEnvOverrides(cfg *ServerConfig) {
	log.Printf("DEBUG: Applying environment overrides...")
	if v := getenvTrim("LLM_PROVIDER"); v != "" {
		log.Printf("DEBUG: Setting LLM_PROVIDER from env: %s", v)
		cfg.LLMProvider = v
	}
	if v := getenvTrim("LLM_API_KEY"); v != "" {
		cfg.LLMAPIKey = v
	}
	if v := getenvTrim("OPENAI_API_KEY"); v != "" {
		cfg.LLMAPIKey = v
	}
	if v := getenvTrim("ANTHROPIC_API_KEY"); v != "" {
		cfg.LLMAPIKey = v
	}
	if v := getenvTrim("LLM_MODEL"); v != "" {
		if cfg.Settings == nil {
			cfg.Settings = make(map[string]string)
		}
		cfg.Settings["model"] = v
	}
	if v := getenvTrim("OLLAMA_URL"); v != "" {
		if cfg.Settings == nil {
			cfg.Settings = make(map[string]string)
		}
		cfg.Settings["ollama_url"] = v
	}
	if v := getenvTrim("OLLAMA_BASE_URL"); v != "" {
		if cfg.Settings == nil {
			cfg.Settings = make(map[string]string)
		}
		log.Printf("DEBUG: Setting ollama_url from OLLAMA_BASE_URL: %s", v)
		cfg.Settings["ollama_url"] = v
	}
	if v := getenvTrim("OPENAI_BASE_URL"); v != "" {
		if cfg.Settings == nil {
			cfg.Settings = make(map[string]string)
		}
		log.Printf("DEBUG: Setting openai_url from OPENAI_BASE_URL: %s", v)
		cfg.Settings["openai_url"] = v
	}
}

func getenvTrim(key string) string {
	v := os.Getenv(key)
	return strings.TrimSpace(v)
}

// loadEnvFile loads environment variables from .env file
// Looks for .env in the current directory and parent directories (up to project root)
func loadEnvFile() error {
	// Try current directory first
	if err := godotenv.Load(".env"); err == nil {
		log.Printf("✅ [ENV] Loaded .env file from current directory")
		return nil
	}

	// Try parent directories (up to 3 levels up to find project root)
	dir, err := os.Getwd()
	if err != nil {
		return err
	}

	for i := 0; i < 3; i++ {
		envPath := filepath.Join(dir, ".env")
		if err := godotenv.Load(envPath); err == nil {
			log.Printf("✅ [ENV] Loaded .env file from: %s", envPath)
			return nil
		}
		// Move up one directory
		parent := filepath.Dir(dir)
		if parent == dir {
			break // Reached root
		}
		dir = parent
	}

	return fmt.Errorf(".env file not found")
}

// normalizeRedisAddr normalizes a Redis address from environment variable
// Handles redis:// prefix and ensures proper format
func normalizeRedisAddr(addr string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return "localhost:6379"
	}
	// Strip redis:// prefix if present
	if strings.HasPrefix(addr, "redis://") {
		addr = strings.TrimPrefix(addr, "redis://")
	}
	// Remove trailing slash if present
	addr = strings.TrimSuffix(addr, "/")
	// Ensure we have a valid address format (host:port)
	if !strings.Contains(addr, ":") {
		// If no port specified, add default Redis port
		addr = addr + ":6379"
	}
	return addr
}

func startAPIServer(domainPath string, config *ServerConfig) {
	// Create enhanced domain with config
	enhancedDomain := &EnhancedDomain{
		Methods: []EnhancedMethodDef{},
		Actions: []EnhancedActionDef{},
		Config: DomainConfig{
			LLMProvider: config.LLMProvider,
			LLMAPIKey:   config.LLMAPIKey,
			MCPEndpoint: config.MCPEndpoint,
			Settings:    config.Settings,
		},
	}

	// Load existing domain if it exists
	if data, err := ioutil.ReadFile(domainPath); err == nil {
		var legacyDomain LegacyDomain
		if err := json.Unmarshal(data, &legacyDomain); err == nil {
			// Convert legacy domain
			enhancedDomain.Methods = make([]EnhancedMethodDef, len(legacyDomain.Methods))
			enhancedDomain.Actions = make([]EnhancedActionDef, len(legacyDomain.Actions))

			for i, method := range legacyDomain.Methods {
				enhancedDomain.Methods[i] = EnhancedMethodDef{
					MethodDef: method,
					TaskType:  TaskTypeMethod,
				}
			}

			for i, action := range legacyDomain.Actions {
				enhancedDomain.Actions[i] = EnhancedActionDef{
					ActionDef: action,
					TaskType:  TaskTypePrimitive,
				}
			}

			// Legacy domain doesn't have config, so we keep the enhanced domain's default config
		}
	}

	// Apply environment variable overrides again to ensure they take precedence
	envConfig := &ServerConfig{
		LLMProvider: enhancedDomain.Config.LLMProvider,
		LLMAPIKey:   enhancedDomain.Config.LLMAPIKey,
		MCPEndpoint: enhancedDomain.Config.MCPEndpoint,
		Settings:    enhancedDomain.Config.Settings,
	}
	log.Printf("DEBUG: Before env override - Settings: %+v", envConfig.Settings)
	log.Printf("DEBUG: Environment variables - LLM_MODEL: %s, OLLAMA_BASE_URL: %s", os.Getenv("LLM_MODEL"), os.Getenv("OLLAMA_BASE_URL"))
	applyEnvOverrides(envConfig)
	log.Printf("DEBUG: After env override - Settings: %+v", envConfig.Settings)

	// Update the enhanced domain with the final config (including env overrides)
	enhancedDomain.Config.LLMProvider = envConfig.LLMProvider
	enhancedDomain.Config.LLMAPIKey = envConfig.LLMAPIKey
	enhancedDomain.Config.MCPEndpoint = envConfig.MCPEndpoint
	enhancedDomain.Config.Settings = envConfig.Settings

	// Initialize domain and action managers (env override REDIS_URL)
	redisAddrRaw := getenvTrim("REDIS_URL")
	redisAddr := normalizeRedisAddr(redisAddrRaw)
	if redisAddrRaw == "" {
		log.Printf("⚠️  [REDIS] REDIS_URL not set, using default: %s", redisAddr)
	} else {
		log.Printf("✅ [REDIS] Using Redis address: %s (from REDIS_URL: %s)", redisAddr, redisAddrRaw)
	}

	// Create API server
	server := NewAPIServer(domainPath, redisAddr)
	server.domain = enhancedDomain

	// Initialize clients
	llmClient := NewLLMClient(enhancedDomain.Config)
	server.SetLLMClient(llmClient) // Use single shared LLM client
	server.mcpClient = NewMCPClient(enhancedDomain.Config)

	// Initialize principles client (env override PRINCIPLES_URL)
	principlesURL := getenvTrim("PRINCIPLES_URL")
	if principlesURL == "" {
		principlesURL = "http://principles-server:8080"
	}
	InitializePrinciplesClient(principlesURL)
	server.domainManager = NewDomainManager(redisAddr, 24) // 24 hour TTL
	server.actionManager = server.domainManager.GetActionManager()
	server.currentDomain = "default"

	// Create default domain if it doesn't exist
	exists, err := server.domainManager.DomainExists("default")
	if err == nil && !exists {
		server.domainManager.CreateDomain("default", "Default domain for HDN actions", enhancedDomain.Config, []string{"default"})
	}

	// Initialize planner integration
	redisClient := redis.NewClient(&redis.Options{
		Addr: redisAddr,
	})
	// Test Redis connection
	ctx := context.Background()
	if err := redisClient.Ping(ctx).Err(); err != nil {
		log.Printf("⚠️  [REDIS] Failed to connect to Redis at %s: %v", redisAddr, err)
	} else {
		log.Printf("✅ [REDIS] Successfully connected to Redis at %s", redisAddr)
	}

	// Initialize self-model manager
	selfModelManager := selfmodel.NewManager(redisAddr, "hdn_self_model")

	// Get HDN base URL from environment variable, fallback to localhost for local development
	hdnBaseURL := getenvTrim("HDN_URL")
	if hdnBaseURL == "" {
		hdnBaseURL = "http://localhost:8080" // Default for local development
	}
	log.Printf("✅ [HDN] Using HDN base URL: %s", hdnBaseURL)

	// Create intelligent executor for planner
	intelligentExecutor := NewIntelligentExecutor(
		server.domainManager,
		server.codeStorage,
		server.codeGenerator,
		server.dockerExecutor,
		server.llmClient,
		server.actionManager,
		nil, // No planner integration yet - will be set below
		selfModelManager,
		server.toolMetrics,
		server.fileStorage,
		hdnBaseURL, // HDN base URL for tool calling (from HDN_URL env var)
		redisAddr,  // Redis address for learning data
	)

	// Create planner integration
	server.plannerIntegration = NewPlannerIntegration(
		redisClient,
		intelligentExecutor,
		server.domainManager,
		server.actionManager,
		principlesURL,    // Principles server URL
		selfModelManager, // Pass self-model manager for hierarchical planning
		server,           // Pass API server for workflow mapping
	)

	// Update intelligent executor with planner integration
	intelligentExecutor.plannerIntegration = server.plannerIntegration
	intelligentExecutor.usePlanner = true

	// Bootstrap seed tools (from file or defaults) and gate via principles
	server.BootstrapSeedTools(context.Background())

	// Register any existing tools as capabilities (in case tools were registered before planner integration)
	log.Printf("🔧 [HDN] Registering existing tools as capabilities...")
	server.registerExistingToolsAsCapabilities(context.Background())
	log.Printf("✅ [HDN] Finished registering tools as capabilities")

	// Start token aggregation scheduler (runs hourly to consolidate token usage)
	server.startTokenAggregationScheduler()
	log.Printf("✅ [HDN] Token aggregation scheduler started (hourly)")

	// Initialize and start memory consolidation
	if server.vectorDB != nil && server.domainKnowledge != nil {
		consolidator := mempkg.NewMemoryConsolidator(
			redisClient,
			server.vectorDB,
			server.domainKnowledge,
			mempkg.DefaultConsolidationConfig(),
		)
		server.memoryConsolidator = consolidator
		consolidator.Start()
		log.Printf("✅ [HDN] Memory consolidation scheduler started (interval: %v)", mempkg.DefaultConsolidationConfig().Interval)
	} else {
		log.Printf("⚠️ [HDN] Memory consolidation disabled (vectorDB or domainKnowledge not available)")
	}

	// Start server
	log.Printf("🔧 [HDN] About to start HTTP server...")
	fmt.Printf("Starting HTN API Server on %s:%d\n", config.Server.Host, config.Server.Port)
	log.Printf("🚀 [HDN] Starting HDN Server with PROJECT_ID support - Version 2025-09-25")
	fmt.Printf("Domain file: %s\n", domainPath)
	fmt.Printf("LLM Provider: %s\n", config.LLMProvider)
	fmt.Printf("MCP Endpoint: %s\n", config.MCPEndpoint)
	fmt.Println("\nAPI Endpoints:")
	fmt.Println("  GET  /health                    - Health check")
	fmt.Println("  POST /api/v1/task/execute       - Execute a task")
	fmt.Println("  POST /api/v1/task/plan          - Plan a task")
	fmt.Println("  POST /api/v1/learn              - Learn a new method")
	fmt.Println("  POST /api/v1/learn/llm          - Learn using LLM")
	fmt.Println("  POST /api/v1/learn/mcp          - Learn using MCP")
	fmt.Println("  GET  /api/v1/domain             - Get domain")
	fmt.Println("  PUT  /api/v1/domain             - Update domain")
	fmt.Println("  POST /api/v1/domain/save        - Save domain")
	fmt.Println("  GET  /api/v1/state              - Get current state")
	fmt.Println("  PUT  /api/v1/state              - Update state")
	fmt.Println("\nNew Domain Management:")
	fmt.Println("  GET  /api/v1/domains            - List all domains")
	fmt.Println("  POST /api/v1/domains            - Create new domain")
	fmt.Println("  GET  /api/v1/domains/{name}     - Get domain by name")
	fmt.Println("  DELETE /api/v1/domains/{name}   - Delete domain")
	fmt.Println("  POST /api/v1/domains/{name}/switch - Switch to domain")
	fmt.Println("\nNew Action Management:")
	fmt.Println("  POST /api/v1/actions            - Create new action")
	fmt.Println("  GET  /api/v1/actions/{domain}   - List actions in domain")
	fmt.Println("  GET  /api/v1/actions/{domain}/{id} - Get action by ID")
	fmt.Println("  DELETE /api/v1/actions/{domain}/{id} - Delete action")
	fmt.Println("  POST /api/v1/actions/{domain}/search - Search actions")
	fmt.Println("\nDocker Code Execution:")
	fmt.Println("  POST /api/v1/docker/execute     - Execute code in Docker container")
	fmt.Println("  POST /api/v1/docker/primes      - Calculate primes via Docker")
	fmt.Println("  POST /api/v1/docker/generate    - Generate and execute code via LLM + Docker")
	fmt.Println("\nIntelligent Execution:")
	fmt.Println("  POST /api/v1/intelligent/execute - Execute any task intelligently using LLM")
	fmt.Println("  POST /api/v1/intelligent/primes  - Calculate primes via intelligent execution")
	fmt.Println("  GET  /api/v1/intelligent/capabilities - List learned capabilities")
	fmt.Println()

	if err := server.Start(config.Server.Port); err != nil {
		log.Fatal("Server failed to start:", err)
	}
}

func runCLI(domainPath string) {
	// Original CLI behavior
	domain, err := LoadDomain(domainPath)
	if err != nil {
		fmt.Println("Failed to load domain:", err)
		return
	}

	// initial world state: missing draft => GetReview will fail initially
	state := State{
		"draft_written":    false,
		"review_done":      false,
		"report_submitted": false,
	}

	goal := "DeliverReport"

	fmt.Println("=== DB-driven HTN planner (with learning) ===")
	fmt.Printf("Goal: %s\n\n", goal)

	maxRetries := 3
	for attempt := 1; attempt <= maxRetries; attempt++ {
		fmt.Printf(">> Planning attempt %d\n", attempt)
		plan := HTNPlan(copyState(state), goal, domain)
		if plan == nil {
			fmt.Println("Plan failed. Attempting to learn...")

			// Try to discover missing predicates by inspecting actions that would be needed.
			// Strategy: look for the action corresponding to top-level subtasks in methods or the goal itself.
			// For simplicity we attempt: for each primitive action defined, check its missing preconds and try learning.
			learned := false
			for _, a := range domain.Actions {
				missing := missingPredicatesForAction(&a, state)
				if len(missing) > 0 {
					// Try to learn a method to satisfy missing preds for that action
					if LearnMethodForMissing(a.Task, missing, domain) {
						learned = true
						// persist updated domain
						if err := SaveDomain(domainPath, domain); err != nil {
							fmt.Println("Warning: could not save domain.json:", err)
						}
						break
					}
				}
			}

			if !learned {
				fmt.Println("No learnable providers found for missing prerequisites. Stopping.")
				return
			}
			// loop will retry planning
			continue
		}

		// plan found
		fmt.Println("✅ Plan found:")
		for i, p := range plan {
			fmt.Printf("  %d. %s\n", i+1, p)
		}

		// execute
		fmt.Println("\n🚀 Executing plan...")
		state = ExecutePlan(state, plan, domain)

		fmt.Println("\n--- State after execution ---")
		for k, v := range state {
			fmt.Printf("  %s = %v\n", k, v)
		}
		break
	}

	fmt.Println("\n--- Final domain methods (showing learned flag where applicable) ---")
	for _, m := range domain.Methods {
		learnedTag := ""
		if m.IsLearned {
			learnedTag = " (learned)"
		}
		fmt.Printf(" - %s%s -> %v (preconds: %v)\n", m.Task, learnedTag, m.Subtasks, m.Preconditions)
	}
}
