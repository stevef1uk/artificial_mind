package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	planner "agi/planner_evaluator"
	selfmodel "agi/self"
	"eventbus"
	"hdn/conversational"
	"hdn/interpreter"
	mempkg "hdn/memory"

	"github.com/gorilla/mux"
	"github.com/redis/go-redis/v9"
)

// toyEmbed creates a simple deterministic vector for text (for testing)
func toyEmbed(text string, dim int) []float32 {
	vec := make([]float32, dim)
	hash := 0
	for _, c := range text {
		hash = hash*31 + int(c)
	}
	for i := 0; i < dim; i++ {
		vec[i] = float32((hash>>i)&1) * 0.5 // simple binary-like features
	}
	return vec
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// sanitizeCode removes markdown fences and stray language headers (e.g., "go") from code blocks
func sanitizeCode(text string) string {
	if text == "" {
		return text
	}
	t := strings.TrimSpace(text)
	// Remove fenced block ```lang\n...\n```
	fence := regexp.MustCompile("(?s)^```[a-zA-Z0-9_-]*\n(.*?)\n```\\s*$")
	if m := fence.FindStringSubmatch(t); m != nil && len(m) > 1 {
		t = m[1]
	}
	// Remove a leading single-word language line (e.g., "go") if present
	lines := strings.Split(t, "\n")
	if len(lines) > 0 {
		first := strings.TrimSpace(lines[0])
		switch strings.ToLower(first) {
		case "go", "golang", "python", "py", "javascript", "js", "typescript", "ts", "java", "bash", "sh":
			t = strings.Join(lines[1:], "\n")
		}
	}
	// Trim again after processing
	return strings.TrimSpace(t)
}

// sanitizeConsoleOutput removes noisy environment/provisioning logs and keeps meaningful program output
func sanitizeConsoleOutput(text string) string {
	if text == "" {
		return text
	}
	lines := strings.Split(text, "\n")
	filtered := make([]string, 0, len(lines))
	// Patterns to drop
	dropPrefixes := []string{
		"Collecting ", "Downloading ", "Installing collected packages:", "Successfully installed ",
		"WARNING: Running pip as the 'root' user", "[notice] A new release of pip is available:",
		"[notice] To update, run:", "Scanning for PDFs", "Found ", "--- /app contents ---",
		"--- /app/output contents ---", "total ", "-rw-", "drwx", "Requirement already satisfied",
	}
	for _, ln := range lines {
		s := strings.TrimSpace(ln)
		if s == "" {
			continue
		}
		drop := false
		for _, p := range dropPrefixes {
			if strings.HasPrefix(s, p) {
				drop = true
				break
			}
		}
		if drop {
			continue
		}
		filtered = append(filtered, ln)
	}
	if len(filtered) == 0 {
		return strings.TrimSpace(text)
	}
	return strings.TrimSpace(strings.Join(filtered, "\n"))
}

// --------- API Types ---------

type TaskRequest struct {
	TaskName string            `json:"task_name"`
	State    State             `json:"state,omitempty"`
	Context  map[string]string `json:"context,omitempty"`
}

type TaskResponse struct {
	Success  bool     `json:"success"`
	Plan     []string `json:"plan,omitempty"`
	Message  string   `json:"message"`
	NewState State    `json:"new_state,omitempty"`
	Learned  bool     `json:"learned,omitempty"`
}

type LearnRequest struct {
	TaskName    string            `json:"task_name"`
	Description string            `json:"description"`
	Context     map[string]string `json:"context,omitempty"`
	UseLLM      bool              `json:"use_llm"`
	UseMCP      bool              `json:"use_mcp"`
}

type LearnResponse struct {
	Success bool       `json:"success"`
	Message string     `json:"message"`
	Method  *MethodDef `json:"method,omitempty"`
}

type DomainResponse struct {
	Methods []MethodDef  `json:"methods"`
	Actions []ActionDef  `json:"actions"`
	Config  DomainConfig `json:"config,omitempty"`
}

// Hierarchical Planning API Types
type HierarchicalTaskRequest struct {
	TaskName    string            `json:"task_name"`
	Description string            `json:"description"`
	Context     map[string]string `json:"context,omitempty"`
	UserRequest string            `json:"user_request"`
	ProjectID   string            `json:"project_id,omitempty"`
}

type HierarchicalTaskResponse struct {
	Success    bool   `json:"success"`
	WorkflowID string `json:"workflow_id,omitempty"`
	Message    string `json:"message"`
	Error      string `json:"error,omitempty"`
	EpisodeID  string `json:"episode_id,omitempty"`
}

type WorkflowStatusResponse struct {
	Success bool                    `json:"success"`
	Status  *planner.WorkflowStatus `json:"status,omitempty"`
	Error   string                  `json:"error,omitempty"`
}

type WorkflowControlRequest struct {
	WorkflowID  string `json:"workflow_id"`
	Reason      string `json:"reason,omitempty"`
	ResumeToken string `json:"resume_token,omitempty"`
}

type WorkflowControlResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Error   string `json:"error,omitempty"`
}

type WorkflowTemplatesResponse struct {
	Success   bool                        `json:"success"`
	Templates []*planner.WorkflowTemplate `json:"templates,omitempty"`
	Error     string                      `json:"error,omitempty"`
}

type ActiveWorkflowsResponse struct {
	Success   bool                      `json:"success"`
	Workflows []*planner.WorkflowStatus `json:"workflows"`
	Error     string                    `json:"error,omitempty"`
}

type HealthResponse struct {
	Status    string `json:"status"`
	Timestamp string `json:"timestamp"`
	Version   string `json:"version"`
}

// Intelligent execution types
type IntelligentExecutionRequest struct {
	TaskName        string            `json:"task_name"`
	Description     string            `json:"description"`
	Context         map[string]string `json:"context"`
	Language        string            `json:"language"`
	ForceRegenerate bool              `json:"force_regenerate"`
	MaxRetries      int               `json:"max_retries"`
	Timeout         int               `json:"timeout"`
	ProjectID       string            `json:"project_id,omitempty"`
	Priority        string            `json:"priority,omitempty"` // "high" or "low", defaults to "high" for user requests
}

type IntelligentExecutionResponse struct {
	Success         bool             `json:"success"`
	Result          interface{}      `json:"result,omitempty"`
	Error           string           `json:"error,omitempty"`
	GeneratedCode   *GeneratedCode   `json:"generated_code,omitempty"`
	ExecutionTime   int64            `json:"execution_time_ms"`
	RetryCount      int              `json:"retry_count"`
	UsedCachedCode  bool             `json:"used_cached_code"`
	ValidationSteps []ValidationStep `json:"validation_steps,omitempty"`
	NewAction       *DynamicAction   `json:"new_action,omitempty"`
	WorkflowID      string           `json:"workflow_id,omitempty"`
	Preview         interface{}      `json:"preview,omitempty"`
}

type PrimeNumbersRequest struct {
	Count int `json:"count"`
}

type CapabilitiesResponse struct {
	Capabilities []*GeneratedCode       `json:"capabilities"`
	Stats        map[string]interface{} `json:"stats"`
}

// --------- Enhanced Domain with Task Types ---------

type TaskType string

const (
	TaskTypePrimitive TaskType = "primitive"
	TaskTypeLLM       TaskType = "llm"
	TaskTypeMCP       TaskType = "mcp"
	TaskTypeMethod    TaskType = "method"
)

type EnhancedMethodDef struct {
	MethodDef
	TaskType    TaskType          `json:"task_type"`
	LLMPrompt   string            `json:"llm_prompt,omitempty"`
	MCPTool     string            `json:"mcp_tool,omitempty"`
	MCPParams   map[string]string `json:"mcp_params,omitempty"`
	Description string            `json:"description,omitempty"`
}

type EnhancedActionDef struct {
	ActionDef
	TaskType    TaskType          `json:"task_type"`
	LLMPrompt   string            `json:"llm_prompt,omitempty"`
	MCPTool     string            `json:"mcp_tool,omitempty"`
	MCPParams   map[string]string `json:"mcp_params,omitempty"`
	Description string            `json:"description,omitempty"`
}

type EnhancedDomain struct {
	Methods []EnhancedMethodDef `json:"methods"`
	Actions []EnhancedActionDef `json:"actions"`
	Config  DomainConfig        `json:"config"`
}

type DomainConfig struct {
	LLMProvider string            `json:"llm_provider"`
	LLMAPIKey   string            `json:"llm_api_key,omitempty"`
	MCPEndpoint string            `json:"mcp_endpoint,omitempty"`
	Settings    map[string]string `json:"settings,omitempty"`
}

// --------- API Server ---------

// LLMClientWrapper wraps the existing LLMClient to implement the interpreter interface
type LLMClientWrapper struct {
	client   *LLMClient
	priority RequestPriority // Priority for LLM requests (defaults to low for background tasks)
}

// CallLLM implements the interpreter interface
// Uses low priority by default (for background tasks)
func (w *LLMClientWrapper) CallLLM(prompt string) (string, error) {
	// Guard against uninitialized client to avoid nil dereference panics
	if w == nil || w.client == nil {
		return "", fmt.Errorf("LLM client not initialized")
	}
	// Use default priority (low) for backward compatibility
	ctx := context.Background()
	return w.client.callLLMWithContextAndPriority(ctx, prompt, PriorityLow)
}

// CallLLMWithContextAndPriority calls LLM with context and priority
// highPriority=true for user requests, false for background tasks
func (w *LLMClientWrapper) CallLLMWithContextAndPriority(ctx context.Context, prompt string, highPriority bool) (string, error) {
	if w == nil || w.client == nil {
		return "", fmt.Errorf("LLM client not initialized")
	}
	var priority RequestPriority = PriorityLow
	if highPriority {
		priority = PriorityHigh
	}
	return w.client.callLLMWithContextAndPriority(ctx, prompt, priority)
}

type APIServer struct {
	domain               *EnhancedDomain
	llmClient            *LLMClient
	mcpClient            *MCPClient
	router               *mux.Router
	domainPath           string
	domainManager        *DomainManager
	actionManager        *ActionManager
	codeStorage          *CodeStorage
	codeGenerator        *CodeGenerator
	dockerExecutor       *SimpleDockerExecutor
	fileStorage          *FileStorage
	plannerIntegration   *PlannerIntegration
	selfModelManager     *selfmodel.Manager
	interpreter          *interpreter.Interpreter
	interpreterAPI       *interpreter.InterpreterAPI
	llmWrapper           *LLMClientWrapper
	conversationalLayer  *conversational.ConversationalLayer
	conversationalAPI    *conversational.ConversationalAPI
	currentDomain        string
	redis                *redis.Client
	redisAddr            string // Redis address for learning data
	projectManager       *ProjectManager
	eventBus             *eventbus.NATSBus
	workingMemory        *mempkg.WorkingMemoryManager
	episodicClient       *mempkg.EpisodicClient
	vectorDB             mempkg.VectorDBAdapter
	domainKnowledge      mempkg.DomainKnowledgeClient
	executionSemaphore   chan struct{} // Limit concurrent executions
	uiExecutionSemaphore chan struct{} // Reserved slot for UI requests
	toolMetrics          *ToolMetricsManager
	hdnBaseURL           string // For tool calling
	mcpKnowledgeServer   *MCPKnowledgeServer
}

func NewAPIServer(domainPath string, redisAddr string) *APIServer {
	maxConcurrent := getMaxConcurrentExecutions()
	server := &APIServer{
		domainPath:           domainPath,
		router:               mux.NewRouter(),
		executionSemaphore:   make(chan struct{}, maxConcurrent-1), // General executions (N-1)
		uiExecutionSemaphore: make(chan struct{}, 1),               // Reserved UI slot (1)
	}

	// Load domain
	if err := server.loadDomain(); err != nil {
		log.Printf("Warning: Could not load domain: %v", err)
		server.domain = &EnhancedDomain{
			Methods: []EnhancedMethodDef{},
			Actions: []EnhancedActionDef{},
			Config:  DomainConfig{},
		}
	}

	// Initialize clients (LLM client will be set externally)
	server.mcpClient = NewMCPClient(server.domain.Config)

	// Initialize Redis client
	server.redis = redis.NewClient(&redis.Options{
		Addr: redisAddr,
	})
	server.redisAddr = redisAddr // Store for learning data
	
	// Test Redis connection
	ctx := context.Background()
	if err := server.redis.Ping(ctx).Err(); err != nil {
		log.Printf("⚠️  [API] Failed to connect to Redis at %s: %v", redisAddr, err)
		log.Printf("⚠️  [API] This will cause tools not to be persisted. Check REDIS_URL environment variable.")
	} else {
		log.Printf("✅ [API] Successfully connected to Redis at %s", redisAddr)
		// Verify we can write and read
		testKey := "hdn:connection_test"
		if err := server.redis.Set(ctx, testKey, "test", time.Second).Err(); err != nil {
			log.Printf("⚠️  [API] Redis write test failed: %v", err)
		} else if val, err := server.redis.Get(ctx, testKey).Result(); err != nil || val != "test" {
			log.Printf("⚠️  [API] Redis read test failed: %v", err)
		} else {
			log.Printf("✅ [API] Redis read/write test passed")
			server.redis.Del(ctx, testKey)
		}
	}

	// Initialize project manager (24h TTL like others)
	server.projectManager = NewProjectManager(redisAddr, 24)

	// Initialize the Goals project on startup
	server.ensureProjectByName("Goals")

	// Initialize working memory manager (ephemeral, e.g., 6h TTL)
	server.workingMemory = mempkg.NewWorkingMemoryManager(redisAddr, 6)

	// Optionally initialize episodic memory client (RAG adapter)
	if base := os.Getenv("RAG_ADAPTER_URL"); strings.TrimSpace(base) != "" {
		server.episodicClient = mempkg.NewEpisodicClient(base)
		log.Printf("🧠 [API] Episodic memory enabled: %s", base)
	}

	// Fallback: unified vector database client if adapter is not set
	if server.episodicClient == nil {
		qbase := os.Getenv("WEAVIATE_URL")
		if strings.TrimSpace(qbase) == "" {
			qbase = "http://localhost:8080"
		}
		// Use Weaviate as the vector database
		server.vectorDB = mempkg.NewVectorDBAdapter(qbase, "AgiEpisodes")
		_ = server.vectorDB.EnsureCollection(8) // toy dim for now

		log.Printf("🧠 [API] Episodic memory via Weaviate: %s", qbase)
	}

	// Initialize domain knowledge client (Neo4j)
	neo4jURI := os.Getenv("NEO4J_URI")
	if strings.TrimSpace(neo4jURI) == "" {
		neo4jURI = "bolt://localhost:7687"
	}
	neo4jUser := os.Getenv("NEO4J_USER")
	if strings.TrimSpace(neo4jUser) == "" {
		neo4jUser = "neo4j"
	}
	neo4jPass := os.Getenv("NEO4J_PASS")
	if strings.TrimSpace(neo4jPass) == "" {
		neo4jPass = "test1234"
	}

	var err error
	server.domainKnowledge, err = mempkg.NewDomainKnowledgeClient(neo4jURI, neo4jUser, neo4jPass)
	if err != nil {
		log.Printf("Warning: Could not initialize domain knowledge client: %v", err)
		server.domainKnowledge = nil
	} else {
		log.Printf("🧠 [API] Domain knowledge enabled: %s", neo4jURI)
	}

	// Initialize domain manager and action manager
	server.domainManager = NewDomainManager(redisAddr, 24) // 24 hour TTL
	server.actionManager = NewActionManager(redisAddr, 24)

	// Initialize code storage (generator will be created when LLM client is set)
	server.codeStorage = NewCodeStorage(redisAddr, 24)

	// Initialize file storage
	server.fileStorage = NewFileStorage(redisAddr, 24) // 24 hour TTL

	// Initialize Docker executor with file storage
	server.dockerExecutor = NewSimpleDockerExecutorWithStorage(server.fileStorage)

	// Initialize self-model manager
	server.selfModelManager = selfmodel.NewManager(redisAddr, "hdn_self_model")

	// Initialize interpreter
	server.llmWrapper = &LLMClientWrapper{client: server.llmClient}
	llmAdapter := interpreter.NewLLMAdapter(server.llmWrapper)
	server.interpreter = interpreter.NewInterpreter(llmAdapter)
	server.interpreterAPI = interpreter.NewInterpreterAPI(server.interpreter)

	// Initialize NATS event bus (best-effort)
	natsURL := os.Getenv("NATS_URL")
	if natsURL == "" {
		natsURL = "nats://localhost:4222"
	}
	if bus, err := eventbus.NewNATSBus(eventbus.NATSConfig{URL: natsURL, Subject: "agi.events.input"}); err == nil {
		server.eventBus = bus
		// Subscribe to events and feed working/episodic memory (best-effort)
		ctx := context.Background()
		_, err := server.eventBus.Subscribe(ctx, func(evt eventbus.CanonicalEvent) {
			server.processEvent(evt)
		})
		if err != nil {
			log.Printf("⚠️ [API] NATS subscribe failed: %v", err)
		} else {
			log.Printf("📡 [API] Subscribed to NATS events for memory feed")
		}

		// Also subscribe to news events
		if newsBus, err := eventbus.NewNATSBus(eventbus.NATSConfig{URL: natsURL, Subject: "agi.events.news.relations"}); err == nil {
			_, _ = newsBus.Subscribe(ctx, func(evt eventbus.CanonicalEvent) {
				server.processEvent(evt)
			})
			log.Printf("📡 [API] Subscribed to news relations events")
		}
		if alertBus, err := eventbus.NewNATSBus(eventbus.NATSConfig{URL: natsURL, Subject: "agi.events.news.alerts"}); err == nil {
			_, _ = alertBus.Subscribe(ctx, func(evt eventbus.CanonicalEvent) {
				server.processEvent(evt)
			})
			log.Printf("📡 [API] Subscribed to news alerts events")
		}
	} else {
		log.Printf("⚠️ [API] NATS unavailable: %v", err)
	}

	// Initialize tool metrics manager
	toolMetrics, err := NewToolMetricsManager(redisAddr, "/tmp")
	if err != nil {
		log.Printf("Warning: Could not initialize tool metrics manager: %v", err)
		server.toolMetrics = nil
	} else {
		server.toolMetrics = toolMetrics
		log.Printf("📊 [API] Tool metrics logging enabled: %s", toolMetrics.GetLogFilePath())
	}

	// Set HDN base URL for tool calling
	if base := strings.TrimSpace(os.Getenv("HDN_BASE_URL")); base != "" {
		server.hdnBaseURL = base
	} else {
		server.hdnBaseURL = "http://localhost:8080" // Default
	}

	// Note: Conversational layer initialization is deferred until SetLLMClient is called
	// This ensures the LLM client is available before initializing the conversational API

	server.setupRoutes()
	return server
}

// processEvent handles incoming events for memory and domain knowledge
func (s *APIServer) processEvent(evt eventbus.CanonicalEvent) {
	// Working memory: require a session id
	sid := evt.Context.SessionID
	if sid != "" && s.workingMemory != nil {
		payload := map[string]any{
			"event_id":  evt.EventID,
			"source":    evt.Source,
			"type":      evt.Type,
			"timestamp": evt.Timestamp,
			"payload":   evt.Payload,
			"security":  evt.Security,
		}
		_ = s.workingMemory.AddEvent(sid, payload, 100)
	}

	// Episodic memory: index text if available
	if s.vectorDB != nil {
		text := evt.Payload.Text
		if strings.TrimSpace(text) == "" {
			text = fmt.Sprintf("%s:%s", evt.Source, evt.Type)
		}
		ep := &mempkg.EpisodicRecord{
			SessionID: sid,
			Timestamp: time.Now().UTC(),
			Outcome:   "event",
			Tags:      []string{"event", evt.Type},
			Text:      text,
			Metadata:  map[string]any{"event_id": evt.EventID, "source": evt.Source},
		}
		vec := toyEmbed(ep.Text, 8)
		_ = s.vectorDB.IndexEpisode(ep, vec)
	}

	// Domain knowledge (Neo4j): persist selected useful events only
	if s.domainKnowledge != nil {
		// Basic filters: keep Wikipedia/BBC/article-like items, and successful tool results
		src := strings.ToLower(strings.TrimSpace(evt.Source))
		typ := strings.ToLower(strings.TrimSpace(evt.Type))
		text := strings.TrimSpace(evt.Payload.Text)
		// require some substantive text
		if len(text) >= 20 {
			allowed := false
			if strings.Contains(src, "wiki") || strings.Contains(typ, "wiki") ||
				strings.Contains(src, "bbc") || strings.Contains(typ, "news") ||
				strings.Contains(typ, "article") {
				allowed = true
			}
			// tool success/completion style events
			if strings.Contains(typ, "tool") && (strings.Contains(typ, "success") || strings.Contains(typ, "completed") || strings.Contains(typ, "result")) {
				allowed = true
			}
			if allowed {
				name := strings.TrimSpace(evt.Type)
				if name == "" {
					name = "Event"
				}
				conceptName := fmt.Sprintf("%s_%s", name, evt.EventID)
				// Use "General" domain instead of source - source (like "news:bbc") is not a semantic domain
				domain := "General"
				// If we have domain classification available, we could use it here
				// For now, use "General" to avoid polluting domains with source identifiers
				concept := &mempkg.Concept{
					Name:       conceptName,
					Domain:     domain,
					Definition: text,
					CreatedAt:  time.Now().UTC(),
					UpdatedAt:  time.Now().UTC(),
				}
				_ = s.domainKnowledge.SaveConcept(context.Background(), concept)
			}
		}
	}
}

// initializeConversationalLayer initializes the conversational AI layer
func (s *APIServer) initializeConversationalLayer() {
	// Create a conversational interface that uses the real LLM client
	// This provides proper conversational functionality with real LLM integration

	// Check if LLM client is available
	if s.llmClient == nil {
		log.Printf("⚠️ [API] LLM client not available, skipping conversational layer initialization")
		return
	}

	// Create LLM adapter for conversational layer
	llmAdapter := &ConversationalLLMAdapter{client: s.llmClient}

	// Initialize conversational layer with real LLM
	s.conversationalLayer = conversational.NewConversationalLayer(
		&SimpleChatFSM{},
		&SimpleChatHDN{server: s},
		s.redis,
		llmAdapter,
	)

	// Initialize conversational API
	s.conversationalAPI = conversational.NewConversationalAPI(s.conversationalLayer)
	// Enable execution slot sharing between Chat and Tools entry
	s.conversationalAPI.SetSlotAcquisition(s.acquireExecutionSlot)

	log.Printf("💬 [API] Conversational interface initialized with real LLM")
}

// ConversationalLLMAdapter adapts the existing LLMClient to the conversational layer interface
type ConversationalLLMAdapter struct {
	client *LLMClient
}

// GenerateResponse implements the conversational LLMClientInterface
// Uses HIGH priority for user-facing chat requests
func (a *ConversationalLLMAdapter) GenerateResponse(ctx context.Context, prompt string, maxTokens int) (string, error) {
	return a.client.callLLMWithContextAndPriority(ctx, prompt, PriorityHigh)
}

// ClassifyText implements the conversational LLMClientInterface
// Uses HIGH priority for user-facing chat requests
func (a *ConversationalLLMAdapter) ClassifyText(ctx context.Context, text string, categories []string) (string, float64, error) {
	// Simple classification using the LLM
	prompt := fmt.Sprintf("Classify the following text into one of these categories: %s\n\nText: %s\n\nCategory:", strings.Join(categories, ", "), text)
	response, err := a.client.callLLMWithContextAndPriority(ctx, prompt, PriorityHigh)
	if err != nil {
		return "", 0.0, err
	}

	// Find the best matching category
	response = strings.ToLower(strings.TrimSpace(response))
	bestMatch := ""
	bestScore := 0.0

	for _, category := range categories {
		if strings.Contains(response, strings.ToLower(category)) {
			bestMatch = category
			bestScore = 0.8 // Simple confidence score
			break
		}
	}

	if bestMatch == "" {
		bestMatch = categories[0] // Default to first category
		bestScore = 0.3
	}

	return bestMatch, bestScore, nil
}

// ExtractEntities implements the conversational LLMClientInterface
// Uses HIGH priority for user-facing chat requests
func (a *ConversationalLLMAdapter) ExtractEntities(ctx context.Context, text string, entityTypes []string) (map[string]string, error) {
	// Simple entity extraction using the LLM
	prompt := fmt.Sprintf("Extract entities from the following text. Look for: %s\n\nText: %s\n\nReturn as JSON with entity type as key and value as the extracted text.", strings.Join(entityTypes, ", "), text)
	response, err := a.client.callLLMWithContextAndPriority(ctx, prompt, PriorityHigh)
	if err != nil {
		return make(map[string]string), err
	}

	// Try to parse as JSON, fallback to simple extraction
	var entities map[string]string
	if err := json.Unmarshal([]byte(response), &entities); err != nil {
		// Fallback: create a simple entity map
		entities = make(map[string]string)
		for _, entityType := range entityTypes {
			if strings.Contains(strings.ToLower(text), strings.ToLower(entityType)) {
				entities[entityType] = text // Simple extraction
			}
		}
	}

	return entities, nil
}

// Simple adapters for basic chat functionality

// SimpleChatFSM provides basic FSM interface for chat
type SimpleChatFSM struct{}

func (f *SimpleChatFSM) GetCurrentState() string { return "chat_ready" }
func (f *SimpleChatFSM) GetContext() map[string]interface{} {
	return map[string]interface{}{"mode": "chat", "timestamp": time.Now()}
}
func (f *SimpleChatFSM) TriggerEvent(eventName string, eventData map[string]interface{}) error {
	return nil
}
func (f *SimpleChatFSM) IsHealthy() bool { return true }

// SimpleChatHDN provides basic HDN interface for chat
type SimpleChatHDN struct{ server *APIServer }

func (h *SimpleChatHDN) ExecuteTask(ctx context.Context, task string, context map[string]string) (*conversational.TaskResult, error) {
	// Use the real HDN system to execute tasks
	// Create a basic state for planning
	state := State{
		task: true,
	}

	// Execute the task using the real HDN system
	result := h.server.planTask(state, task)

	return &conversational.TaskResult{
		Success: true,
		Result:  fmt.Sprintf("Task executed successfully: %v", result),
		Metadata: map[string]interface{}{
			"executed_at": time.Now(),
			"task":        task,
			"plan":        result,
		},
	}, nil
}

func (h *SimpleChatHDN) PlanTask(ctx context.Context, task string, context map[string]string) (*conversational.PlanResult, error) {
	return &conversational.PlanResult{
		Success: true,
		Plan:    []string{task},
		Metadata: map[string]interface{}{
			"planned_at": time.Now(),
		},
	}, nil
}

func (h *SimpleChatHDN) LearnFromLLM(ctx context.Context, input string, context map[string]string) (*conversational.LearnResult, error) {
	return &conversational.LearnResult{
		Success: true,
		Learned: fmt.Sprintf("Learned from: %s", input),
		Metadata: map[string]interface{}{
			"learned_at": time.Now(),
		},
	}, nil
}

func (h *SimpleChatHDN) InterpretNaturalLanguage(ctx context.Context, input string, context map[string]string) (*conversational.InterpretResult, error) {
	log.Printf("🔍 [SIMPLE-CHAT-HDN] InterpretNaturalLanguage called with input: %s", input)
	
	// Use the actual interpreter to process the input with tool support
	if h.server == nil || h.server.interpreter == nil {
		log.Printf("⚠️ [SIMPLE-CHAT-HDN] Server or interpreter not available, using fallback")
		// Fallback to simple response if interpreter not available
		return &conversational.InterpretResult{
			Success:     true,
			Interpreted: fmt.Sprintf("Interpreted: %s", input),
			Metadata: map[string]interface{}{
				"interpreted_at": time.Now(),
			},
		}, nil
	}

	// Use flexible interpreter to get tool-aware interpretation
	flexibleInterpreter := h.server.interpreter.GetFlexibleInterpreter()
	if flexibleInterpreter == nil {
		log.Printf("⚠️ [SIMPLE-CHAT-HDN] Flexible interpreter not available, using fallback")
		return &conversational.InterpretResult{
			Success:     true,
			Interpreted: fmt.Sprintf("Interpreted: %s", input),
			Metadata: map[string]interface{}{
				"interpreted_at": time.Now(),
			},
		}, nil
	}
	
	log.Printf("✅ [SIMPLE-CHAT-HDN] Using flexible interpreter with tool support")

	// Create natural language request
	req := interpreter.NaturalLanguageRequest{
		Input:     input,
		Context:   context,
		SessionID: fmt.Sprintf("conv_%d", time.Now().UnixNano()),
	}

	// Use InterpretAndExecute to actually execute tools if the LLM chooses to use them
	result, err := flexibleInterpreter.InterpretAndExecute(ctx, &req)
	if err != nil {
		log.Printf("⚠️ [SIMPLE-CHAT-HDN] Interpretation failed: %v", err)
		return &conversational.InterpretResult{
			Success:     false,
			Interpreted: fmt.Sprintf("Interpretation failed: %v", err),
			Metadata: map[string]interface{}{
				"error": err.Error(),
			},
		}, nil
	}

	// Extract tool information if a tool was used
	metadata := map[string]interface{}{
		"interpreted_at": time.Now(),
		"response_type":  string(result.ResponseType),
	}

	// Track tool usage
	if result.ToolCall != nil {
		metadata["tool_used"] = result.ToolCall.ToolID
		metadata["tool_description"] = result.ToolCall.Description
		if result.ToolExecutionResult != nil {
			metadata["tool_success"] = result.ToolExecutionResult.Success
			if result.ToolExecutionResult.Error != "" {
				metadata["tool_error"] = result.ToolExecutionResult.Error
			}
		}
		log.Printf("🔧 [SIMPLE-CHAT-HDN] Tool %s was used in interpretation", result.ToolCall.ToolID)
	} else {
		log.Printf("⚠️ [SIMPLE-CHAT-HDN] result.ToolCall is nil! ResponseType: %s, HasToolExecutionResult: %v", result.ResponseType, result.ToolExecutionResult != nil)
		if result.ToolExecutionResult != nil {
			log.Printf("⚠️ [SIMPLE-CHAT-HDN] Tool was executed but ToolCall is nil - this shouldn't happen!")
		}
	}

	// Build interpreted text from result
	interpretedText := result.Message
	if result.ToolExecutionResult != nil && result.ToolExecutionResult.Success {
		interpretedText = fmt.Sprintf("%s\n\nTool result: %v", interpretedText, result.ToolExecutionResult.Result)
	}

	return &conversational.InterpretResult{
		Success:     result.Success,
		Interpreted: interpretedText,
		Metadata:    metadata,
	}, nil
}

// SimpleChatLLM provides basic LLM interface for chat
type SimpleChatLLM struct{}

func (l *SimpleChatLLM) GenerateResponse(ctx context.Context, prompt string, maxTokens int) (string, error) {
	prompt = strings.ToLower(strings.TrimSpace(prompt))

	// Memory-related queries
	if strings.Contains(prompt, "remember") || strings.Contains(prompt, "memory") {
		return "I have access to multiple memory systems:\n- Working Memory (Redis): Short-term context and conversation history\n- Episodic Memory (Qdrant): Semantic text embeddings and similarity search\n- Knowledge Graph (Neo4j): Structured facts and relationships\n- Goals: Current tasks and objectives I'm working on\n\nYou can ask me about specific memories, goals, or what I know about certain topics!", nil
	}

	if strings.Contains(prompt, "goals") || strings.Contains(prompt, "working on") {
		return "I'm currently working on several goals including code generation tasks, data analysis workflows, and system monitoring. I can help you create new goals or check the status of existing ones. What would you like to know about my current objectives?", nil
	}

	if strings.Contains(prompt, "tools") || strings.Contains(prompt, "capabilities") {
		return "I have access to 12 different tools including:\n- HTTP GET requests\n- HTML scraping\n- File operations\n- Shell execution\n- Docker management\n- Code generation\n- JSON parsing\n- Text search\n- And more!\n\nWhat specific tool would you like me to use?", nil
	}

	if strings.Contains(prompt, "recent") || strings.Contains(prompt, "recently") {
		return "I've been working on various tasks recently including artifact generation, code execution, and system monitoring. I can access my episodic memory to show you specific recent events and workflows. What would you like to know about my recent activities?", nil
	}

	// More intelligent response patterns
	if strings.Contains(prompt, "hello") || strings.Contains(prompt, "hi") {
		responses := []string{
			"Hello! I'm your AI assistant. How can I help you today?",
			"Hi there! What would you like to work on?",
			"Hello! I'm ready to help with your tasks. What do you need?",
		}
		return responses[time.Now().UnixNano()%int64(len(responses))], nil
	}

	if strings.Contains(prompt, "what") && strings.Contains(prompt, "do") {
		responses := []string{
			"I can help you with:\n- Code generation (Python, JavaScript, Go, etc.)\n- Data analysis and visualization\n- Web scraping and API integration\n- File operations and system tasks\n- Docker container management\n- And much more! What would you like to do?",
			"My capabilities include:\n• Writing and debugging code\n• Analyzing data and creating visualizations\n• Web scraping and API integration\n• File and system operations\n• Docker container management\n• And many other tasks! What can I help you with?",
			"I'm equipped to handle:\n- Programming tasks in multiple languages\n- Data processing and analysis\n- Web development and automation\n- System administration tasks\n- Container orchestration\n- And more! What would you like to tackle?",
		}
		return responses[time.Now().UnixNano()%int64(len(responses))], nil
	}

	if strings.Contains(prompt, "help") {
		responses := []string{
			"I can help you with:\n- Code generation (Python, JavaScript, Go, etc.)\n- Data analysis and visualization\n- Web scraping and API integration\n- File operations and system tasks\n- Docker container management\n- And much more! What would you like to do?",
			"Sure! I can assist with:\n• Programming and development\n• Data analysis and visualization\n• Web scraping and automation\n• File and system operations\n• Docker and containerization\n• And many other technical tasks! What do you need help with?",
		}
		return responses[time.Now().UnixNano()%int64(len(responses))], nil
	}

	if strings.Contains(prompt, "code") || strings.Contains(prompt, "programming") {
		return "I'd be happy to help with code! I can write, debug, and explain code in Python, JavaScript, Go, and other languages. What kind of programming task do you have in mind?", nil
	}

	if strings.Contains(prompt, "data") || strings.Contains(prompt, "analysis") {
		return "I can help with data analysis! I can process CSV files, create visualizations, perform statistical analysis, and work with various data formats. What data would you like to analyze?", nil
	}

	if strings.Contains(prompt, "web") || strings.Contains(prompt, "scraping") {
		return "I can help with web-related tasks! I can scrape websites, work with APIs, build web applications, and handle HTTP requests. What web task do you need assistance with?", nil
	}

	if strings.Contains(prompt, "docker") || strings.Contains(prompt, "container") {
		return "I can help with Docker and containerization! I can build images, manage containers, create Dockerfiles, and handle container orchestration. What Docker task do you need help with?", nil
	}

	if strings.Contains(prompt, "file") || strings.Contains(prompt, "system") {
		return "I can help with file and system operations! I can read/write files, manage directories, execute commands, and perform various system tasks. What file or system operation do you need?", nil
	}

	// Default responses for unrecognized input
	responses := []string{
		"I understand you're asking: \"" + prompt + "\". I'm here to help! Could you be more specific about what you'd like me to do?",
		"That's an interesting question: \"" + prompt + "\". I can help with programming, data analysis, web tasks, and more. What would you like to work on?",
		"I see you mentioned: \"" + prompt + "\". I'm ready to assist with various technical tasks. What specific help do you need?",
		"Thanks for your message: \"" + prompt + "\". I can help with code, data, web development, and system tasks. What would you like to do?",
	}
	return responses[time.Now().UnixNano()%int64(len(responses))], nil
}

func (l *SimpleChatLLM) ClassifyText(ctx context.Context, text string, categories []string) (string, float64, error) {
	text = strings.ToLower(text)
	if strings.Contains(text, "hello") || strings.Contains(text, "hi") {
		return "greeting", 0.9, nil
	}
	if strings.Contains(text, "what") || strings.Contains(text, "how") {
		return "question", 0.8, nil
	}
	return "general", 0.5, nil
}

func (l *SimpleChatLLM) ExtractEntities(ctx context.Context, text string, entityTypes []string) (map[string]string, error) {
	return map[string]string{
		"query": text,
		"type":  "conversation",
	}, nil
}

func (s *APIServer) setupRoutes() {
	// Health check
	s.router.HandleFunc("/health", s.handleHealth).Methods("GET")
	
	// Register MCP knowledge server routes
	s.RegisterMCPKnowledgeServerRoutes()

	// Task execution
	s.router.HandleFunc("/api/v1/task/execute", s.handleExecuteTask).Methods("POST")
	s.router.HandleFunc("/api/v1/task/plan", s.handlePlanTask).Methods("POST")

	// Learning
	s.router.HandleFunc("/api/v1/learn", s.handleLearn).Methods("POST")
	s.router.HandleFunc("/api/v1/learn/llm", s.handleLearnLLM).Methods("POST")
	s.router.HandleFunc("/api/v1/learn/mcp", s.handleLearnMCP).Methods("POST")

	// Domain management
	s.router.HandleFunc("/api/v1/domain", s.handleGetDomain).Methods("GET")
	s.router.HandleFunc("/api/v1/domain", s.handleUpdateDomain).Methods("PUT")
	s.router.HandleFunc("/api/v1/domain/save", s.handleSaveDomain).Methods("POST")

	// New domain management routes
	s.router.HandleFunc("/api/v1/domains", s.handleListDomains).Methods("GET")
	s.router.HandleFunc("/api/v1/domains", s.handleCreateDomain).Methods("POST")
	s.router.HandleFunc("/api/v1/domains/{name}", s.handleGetDomainByName).Methods("GET")
	s.router.HandleFunc("/api/v1/domains/{name}", s.handleDeleteDomain).Methods("DELETE")
	s.router.HandleFunc("/api/v1/domains/{name}/switch", s.handleSwitchDomain).Methods("POST")

	// Hierarchical Planning routes
	s.router.HandleFunc("/api/v1/hierarchical/execute", s.handleHierarchicalExecute).Methods("POST")
	s.router.HandleFunc("/api/v1/hierarchical/workflow/{id}/status", s.handleGetWorkflowStatus).Methods("GET")
	s.router.HandleFunc("/api/v1/hierarchical/workflow/{id}/details", s.handleGetWorkflowDetails).Methods("GET")
	s.router.HandleFunc("/api/v1/hierarchical/workflow/{id}/pause", s.handlePauseWorkflow).Methods("POST")
	s.router.HandleFunc("/api/v1/hierarchical/workflow/{id}/resume", s.handleResumeWorkflow).Methods("POST")
	s.router.HandleFunc("/api/v1/hierarchical/workflow/{id}/cancel", s.handleCancelWorkflow).Methods("POST")
	s.router.HandleFunc("/api/v1/hierarchical/workflows", s.handleListActiveWorkflows).Methods("GET")
	s.router.HandleFunc("/api/v1/hierarchical/templates", s.handleListWorkflowTemplates).Methods("GET")
	s.router.HandleFunc("/api/v1/hierarchical/templates", s.handleRegisterWorkflowTemplate).Methods("POST")

	// New action management routes
	s.router.HandleFunc("/api/v1/actions", s.handleCreateAction).Methods("POST")
	s.router.HandleFunc("/api/v1/actions/{domain}", s.handleListActions).Methods("GET")
	s.router.HandleFunc("/api/v1/actions/{domain}/{id}", s.handleGetAction).Methods("GET")
	s.router.HandleFunc("/api/v1/actions/{domain}/{id}", s.handleDeleteAction).Methods("DELETE")
	s.router.HandleFunc("/api/v1/actions/{domain}/search", s.handleSearchActions).Methods("POST")

	// Docker code execution routes
	s.router.HandleFunc("/api/v1/docker/execute", s.handleDockerExecute).Methods("POST")
	s.router.HandleFunc("/api/v1/docker/primes", s.handleDockerPrimes).Methods("POST")
	s.router.HandleFunc("/api/v1/docker/generate", s.handleDockerGenerateCode).Methods("POST")

	// File serving routes
	s.router.HandleFunc("/api/v1/files/{filename}", s.handleServeFile).Methods("GET")
	s.router.HandleFunc("/api/v1/files/workflow/{workflow_id}", s.handleGetWorkflowFiles).Methods("GET")
	s.router.HandleFunc("/api/v1/workflow/{workflow_id}/files/{filename}", s.handleServeWorkflowFile).Methods("GET")

	// State management
	s.router.HandleFunc("/api/v1/state", s.handleGetState).Methods("GET")
	s.router.HandleFunc("/api/v1/state", s.handleUpdateState).Methods("PUT")

	// Working memory API (short-term memory)
	s.router.HandleFunc("/state/session/{id}/working_memory", s.handleGetWorkingMemory).Methods("GET")
	s.router.HandleFunc("/api/v1/state/session/{id}/working_memory", s.handleGetWorkingMemory).Methods("GET")
	// Writers
	s.router.HandleFunc("/api/v1/state/session/{id}/working_memory/event", s.handleAddWorkingMemoryEvent).Methods("POST")
	s.router.HandleFunc("/api/v1/state/session/{id}/working_memory/locals", s.handleSetWorkingMemoryLocals).Methods("PUT")
	s.router.HandleFunc("/api/v1/state/session/{id}/working_memory/plan", s.handleSetWorkingMemoryPlan).Methods("PUT")

	// Intelligent execution routes
	s.router.HandleFunc("/api/v1/intelligent/execute", s.handleIntelligentExecute).Methods("POST")
	s.router.HandleFunc("/api/v1/intelligent/execute", s.handleIntelligentExecuteOptions).Methods("OPTIONS")
	s.router.HandleFunc("/api/v1/intelligent/primes", s.handlePrimeNumbers).Methods("POST")
	s.router.HandleFunc("/api/v1/intelligent/capabilities", s.handleListCapabilities).Methods("GET")

	// Natural language interpreter routes
	s.router.HandleFunc("/api/v1/interpret", s.handleInterpret).Methods("POST")
	s.router.HandleFunc("/api/v1/interpret/execute", s.handleInterpretAndExecute).Methods("POST")

	// Tools registry routes
	s.router.HandleFunc("/api/v1/tools", s.handleListTools).Methods("GET")
	s.router.HandleFunc("/api/v1/tools", s.handleRegisterTool).Methods("POST")
	s.router.HandleFunc("/api/v1/tools/{id}", s.handleDeleteTool).Methods("DELETE")
	s.router.HandleFunc("/api/v1/tools/discover", s.handleDiscoverTools).Methods("POST")
	// Tool invocation
	s.router.HandleFunc("/api/v1/tools/{id}/invoke", s.handleInvokeTool).Methods("POST")

	// Tool metrics routes
	s.router.HandleFunc("/api/v1/tools/metrics", s.handleGetAllToolMetrics).Methods("GET")
	s.router.HandleFunc("/api/v1/tools/{id}/metrics", s.handleGetToolMetrics).Methods("GET")
	s.router.HandleFunc("/api/v1/tools/calls/recent", s.handleGetRecentToolCalls).Methods("GET")

	// Episodic search passthrough (if RAG adapter is configured)
	s.router.HandleFunc("/api/v1/episodes/search", s.handleSearchEpisodes).Methods("GET")

	// Memory summary
	s.router.HandleFunc("/api/v1/memory/summary", s.handleMemorySummary).Methods("GET")
	s.router.HandleFunc("/api/v1/memory/goals/{id}/status", s.handleUpdateGoalStatus).Methods("POST")
	s.router.HandleFunc("/api/v1/memory/goals/{id}", s.handleDeleteSelfModelGoal).Methods("DELETE")
	// Bulk cleanup of internal/self-model goals
	s.router.HandleFunc("/api/v1/memory/goals/cleanup", s.handleCleanupSelfModelGoals).Methods("POST")

	// Project routes (initial minimal set)
	s.router.HandleFunc("/api/v1/projects", s.handleCreateProject).Methods("POST")
	s.router.HandleFunc("/api/v1/projects", s.handleListProjects).Methods("GET")

	// Conversational AI routes
	if s.conversationalAPI != nil {
		s.conversationalAPI.RegisterRoutes(s.router)
	}
	s.router.HandleFunc("/api/v1/projects/{id}", s.handleGetProject).Methods("GET")
	s.router.HandleFunc("/api/v1/projects/{id}", s.handleUpdateProject).Methods("PUT")
	s.router.HandleFunc("/api/v1/projects/{id}", s.handleDeleteProject).Methods("DELETE")

	// Project lifecycle
	s.router.HandleFunc("/api/v1/projects/{id}/pause", s.handlePauseProject).Methods("POST")
	s.router.HandleFunc("/api/v1/projects/{id}/resume", s.handleResumeProject).Methods("POST")
	s.router.HandleFunc("/api/v1/projects/{id}/archive", s.handleArchiveProject).Methods("POST")

	// Project checkpoints
	s.router.HandleFunc("/api/v1/projects/{id}/checkpoints", s.handleListProjectCheckpoints).Methods("GET")
	s.router.HandleFunc("/api/v1/projects/{id}/checkpoints", s.handleAddProjectCheckpoint).Methods("POST")

	// Project workflows
	s.router.HandleFunc("/api/v1/projects/{id}/workflows", s.handleListProjectWorkflows).Methods("GET")

	// Workflow utilities
	s.router.HandleFunc("/api/v1/workflows/resolve/{id}", s.handleResolveWorkflowID).Methods("GET")

	// Domain Knowledge routes
	s.router.HandleFunc("/api/v1/knowledge/concepts", s.handleListConcepts).Methods("GET")
	s.router.HandleFunc("/api/v1/knowledge/concepts", s.handleCreateConcept).Methods("POST")
	s.router.HandleFunc("/api/v1/knowledge/concepts/{name}", s.handleGetConcept).Methods("GET")
	s.router.HandleFunc("/api/v1/knowledge/concepts/{name}", s.handleUpdateConcept).Methods("PUT")
	s.router.HandleFunc("/api/v1/knowledge/concepts/{name}", s.handleDeleteConcept).Methods("DELETE")
	s.router.HandleFunc("/api/v1/knowledge/concepts/{name}/properties", s.handleAddConceptProperty).Methods("POST")
	s.router.HandleFunc("/api/v1/knowledge/concepts/{name}/constraints", s.handleAddConceptConstraint).Methods("POST")
	s.router.HandleFunc("/api/v1/knowledge/concepts/{name}/examples", s.handleAddConceptExample).Methods("POST")
	s.router.HandleFunc("/api/v1/knowledge/concepts/{name}/relations", s.handleRelateConcepts).Methods("POST")
	s.router.HandleFunc("/api/v1/knowledge/concepts/{name}/related", s.handleGetRelatedConcepts).Methods("GET")
	s.router.HandleFunc("/api/v1/knowledge/search", s.handleSearchConcepts).Methods("GET")
	// Cypher query proxy (used by FSM ReasoningEngine)
	s.router.HandleFunc("/api/v1/knowledge/query", s.handleKnowledgeQuery).Methods("POST")
}

func (s *APIServer) loadDomain() error {
	data, err := ioutil.ReadFile(s.domainPath)
	if err != nil {
		return err
	}

	var domain EnhancedDomain
	if err := json.Unmarshal(data, &domain); err != nil {
		// Try to load as legacy domain format
		var legacyDomain Domain
		if err := json.Unmarshal(data, &legacyDomain); err != nil {
			return err
		}

		// Convert legacy domain to enhanced domain
		domain = s.convertLegacyDomain(&legacyDomain)
	}

	s.domain = &domain

	// Apply environment overrides (domain-specific version)
	applyDomainEnvOverrides(&s.domain.Config)

	return nil
}

// applyDomainEnvOverrides applies environment variable overrides to DomainConfig
func applyDomainEnvOverrides(cfg *DomainConfig) {
	log.Printf("DEBUG: Applying environment overrides to domain config...")
	if v := getenvTrim("LLM_PROVIDER"); v != "" {
		log.Printf("DEBUG: Setting LLM_PROVIDER from env: %s", v)
		cfg.LLMProvider = v
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
	if v := getenvTrim("LLM_MODEL"); v != "" {
		if cfg.Settings == nil {
			cfg.Settings = make(map[string]string)
		}
		log.Printf("DEBUG: Setting model from LLM_MODEL: %s", v)
		cfg.Settings["model"] = v
	}
}

// SetLLMClient sets the LLM client (called from server.go after environment overrides)
func (s *APIServer) SetLLMClient(client *LLMClient) {
	s.llmClient = client
	// Initialize code generator now that LLM client is available
	s.codeGenerator = NewCodeGenerator(s.llmClient, s.codeStorage)
	// Initialize conversational layer now that LLM client is available
	s.initializeConversationalLayer()
	// Update interpreter LLM wrapper to use the initialized client
	if s.llmWrapper != nil {
		s.llmWrapper.client = s.llmClient
	}
	// Re-register routes now that conversational API is initialized
	// This ensures the conversational routes are available
	if s.conversationalAPI != nil {
		s.conversationalAPI.RegisterRoutes(s.router)
		log.Printf("💬 [API] Conversational routes registered")
	}
}

func (s *APIServer) convertLegacyDomain(legacy *Domain) EnhancedDomain {
	enhanced := EnhancedDomain{
		Methods: make([]EnhancedMethodDef, len(legacy.Methods)),
		Actions: make([]EnhancedActionDef, len(legacy.Actions)),
		Config:  DomainConfig{},
	}

	for i, method := range legacy.Methods {
		enhanced.Methods[i] = EnhancedMethodDef{
			MethodDef: method,
			TaskType:  TaskTypeMethod,
		}
	}

	for i, action := range legacy.Actions {
		enhanced.Actions[i] = EnhancedActionDef{
			ActionDef: action,
			TaskType:  TaskTypePrimitive,
		}
	}

	return enhanced
}

func (s *APIServer) saveDomain() error {
	data, err := json.MarshalIndent(s.domain, "", "  ")
	if err != nil {
		return err
	}
	return ioutil.WriteFile(s.domainPath, data, 0644)
}

// --------- API Handlers ---------

func (s *APIServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	response := HealthResponse{
		Status:    "healthy",
		Timestamp: time.Now().Format(time.RFC3339),
		Version:   "1.0.0",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (s *APIServer) handleExecuteTask(w http.ResponseWriter, r *http.Request) {
	// Acquire execution slot (UI gets priority)
	release, acquired := s.acquireExecutionSlot(r)
	if !acquired {
		http.Error(w, "Server busy - too many concurrent executions. Please try again later.", http.StatusTooManyRequests)
		return
	}
	defer release()

	var req TaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Use provided state or create default
	state := req.State
	if state == nil {
		state = make(State)
	}

	// Plan and execute
	plan := s.planTask(state, req.TaskName)
	if plan == nil {
		response := TaskResponse{
			Success: false,
			Message: "Failed to create plan for task",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
		return
	}

	// Execute the plan
	newState := s.executePlan(state, plan)

	// Record latest plan in working memory if session provided
	if req.Context != nil {
		if sid, ok := req.Context["session_id"]; ok && sid != "" {
			_ = s.workingMemory.SetLatestPlan(sid, map[string]any{"plan": plan, "task": req.TaskName, "timestamp": time.Now().UTC()})
		}
	}

	response := TaskResponse{
		Success:  true,
		Plan:     plan,
		NewState: newState,
		Message:  "Task executed successfully",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (s *APIServer) handlePlanTask(w http.ResponseWriter, r *http.Request) {
	var req TaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	state := req.State
	if state == nil {
		state = make(State)
	}

	plan := s.planTask(state, req.TaskName)

	response := TaskResponse{
		Success: plan != nil,
		Plan:    plan,
		Message: func() string {
			if plan != nil {
				return "Plan created successfully"
			}
			return "Failed to create plan"
		}(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (s *APIServer) handleLearn(w http.ResponseWriter, r *http.Request) {
	var req LearnRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Try to learn using the specified method
	var learned bool
	var method *MethodDef
	var message string

	if req.UseLLM {
		learned, method, message = s.learnWithLLM(req)
	} else if req.UseMCP {
		learned, method, message = s.learnWithMCP(req)
	} else {
		// Try traditional learning first
		learned, method, message = s.learnTraditional(req)
	}

	response := LearnResponse{
		Success: learned,
		Message: message,
		Method:  method,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (s *APIServer) handleLearnLLM(w http.ResponseWriter, r *http.Request) {
	var req LearnRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	req.UseLLM = true
	learned, method, message := s.learnWithLLM(req)

	response := LearnResponse{
		Success: learned,
		Message: message,
		Method:  method,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (s *APIServer) handleLearnMCP(w http.ResponseWriter, r *http.Request) {
	var req LearnRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	req.UseMCP = true
	learned, method, message := s.learnWithMCP(req)

	response := LearnResponse{
		Success: learned,
		Message: message,
		Method:  method,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (s *APIServer) handleGetDomain(w http.ResponseWriter, r *http.Request) {
	response := DomainResponse{
		Methods: make([]MethodDef, len(s.domain.Methods)),
		Actions: make([]ActionDef, len(s.domain.Actions)),
		Config:  s.domain.Config,
	}

	for i, method := range s.domain.Methods {
		response.Methods[i] = method.MethodDef
	}

	for i, action := range s.domain.Actions {
		response.Actions[i] = action.ActionDef
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (s *APIServer) handleUpdateDomain(w http.ResponseWriter, r *http.Request) {
	var req DomainResponse
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Convert to enhanced domain
	s.domain.Methods = make([]EnhancedMethodDef, len(req.Methods))
	s.domain.Actions = make([]EnhancedActionDef, len(req.Actions))

	for i, method := range req.Methods {
		s.domain.Methods[i] = EnhancedMethodDef{
			MethodDef: method,
			TaskType:  TaskTypeMethod,
		}
	}

	for i, action := range req.Actions {
		s.domain.Actions[i] = EnhancedActionDef{
			ActionDef: action,
			TaskType:  TaskTypePrimitive,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "updated"})
}

func (s *APIServer) handleSaveDomain(w http.ResponseWriter, r *http.Request) {
	if err := s.saveDomain(); err != nil {
		http.Error(w, "Failed to save domain", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "saved"})
}

func (s *APIServer) handleGetState(w http.ResponseWriter, r *http.Request) {
	// For now, return empty state - in a real implementation, this would track current state
	state := make(State)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(state)
}

func (s *APIServer) handleUpdateState(w http.ResponseWriter, r *http.Request) {
	var state State
	if err := json.NewDecoder(r.Body).Decode(&state); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// In a real implementation, this would update the current state
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "updated"})
}

// handleGetWorkingMemory returns short-term working memory for a session.
func (s *APIServer) handleGetWorkingMemory(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	sessionID := vars["id"]
	if sessionID == "" {
		http.Error(w, "Missing session id", http.StatusBadRequest)
		return
	}
	// parse n
	n := 50
	if q := r.URL.Query().Get("n"); q != "" {
		if v, err := strconv.Atoi(q); err == nil && v > 0 {
			n = v
		}
	}

	mem, err := s.workingMemory.GetWorkingMemory(sessionID, n)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get working memory: %v", err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(mem)
}

// handleAddWorkingMemoryEvent appends an event to session working memory
func (s *APIServer) handleAddWorkingMemoryEvent(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	sessionID := vars["id"]
	if sessionID == "" {
		http.Error(w, "Missing session id", http.StatusBadRequest)
		return
	}
	var payload map[string]any
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	if payload == nil {
		payload = map[string]any{}
	}
	payload["timestamp"] = time.Now().UTC()
	if err := s.workingMemory.AddEvent(sessionID, payload, 100); err != nil {
		http.Error(w, fmt.Sprintf("Failed to add event: %v", err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// handleSetWorkingMemoryLocals sets local variables for a session
func (s *APIServer) handleSetWorkingMemoryLocals(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	sessionID := vars["id"]
	if sessionID == "" {
		http.Error(w, "Missing session id", http.StatusBadRequest)
		return
	}
	var locals map[string]string
	if err := json.NewDecoder(r.Body).Decode(&locals); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	if err := s.workingMemory.SetLocalVariables(sessionID, locals); err != nil {
		http.Error(w, fmt.Sprintf("Failed to set locals: %v", err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// handleSetWorkingMemoryPlan stores the latest plan snapshot for a session
func (s *APIServer) handleSetWorkingMemoryPlan(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	sessionID := vars["id"]
	if sessionID == "" {
		http.Error(w, "Missing session id", http.StatusBadRequest)
		return
	}
	var plan map[string]any
	if err := json.NewDecoder(r.Body).Decode(&plan); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	if plan == nil {
		plan = map[string]any{}
	}
	plan["timestamp"] = time.Now().UTC()
	if err := s.workingMemory.SetLatestPlan(sessionID, plan); err != nil {
		http.Error(w, fmt.Sprintf("Failed to set plan: %v", err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// --------- Project Handlers (Minimal MVP) ---------

func (s *APIServer) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string            `json:"name"`
		Description string            `json:"description"`
		Owner       string            `json:"owner"`
		Tags        []string          `json:"tags"`
		Metadata    map[string]string `json:"metadata"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		http.Error(w, "Project name is required", http.StatusBadRequest)
		return
	}
	// Idempotent by name: if a project with the same name exists, return it
	if list, err := s.projectManager.ListProjects(); err == nil && list != nil {
		for _, p := range list {
			if strings.EqualFold(strings.TrimSpace(p.Name), strings.TrimSpace(req.Name)) {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(p)
				return
			}
		}
	}

	proj := &Project{
		Name:        req.Name,
		Description: req.Description,
		Owner:       req.Owner,
		Tags:        req.Tags,
		Metadata:    req.Metadata,
	}
	saved, err := s.projectManager.CreateProject(proj)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to create project: %v", err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(saved)
}

func (s *APIServer) handleListProjects(w http.ResponseWriter, r *http.Request) {
	list, err := s.projectManager.ListProjects()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to list projects: %v", err), http.StatusInternalServerError)
		return
	}
	if list == nil {
		list = []*Project{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(list)
}

func (s *APIServer) handleGetProject(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]
	p, err := s.projectManager.GetProject(id)
	if err != nil {
		http.Error(w, fmt.Sprintf("Project not found: %s", id), http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(p)
}

func (s *APIServer) handleUpdateProject(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]
	var req struct {
		Name        *string           `json:"name"`
		Description *string           `json:"description"`
		Status      *string           `json:"status"`
		Owner       *string           `json:"owner"`
		Tags        []string          `json:"tags"`
		NextAction  *string           `json:"next_action"`
		Metadata    map[string]string `json:"metadata"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	updated, err := s.projectManager.UpdateProject(id, func(p *Project) error {
		if req.Name != nil {
			p.Name = *req.Name
		}
		if req.Description != nil {
			p.Description = *req.Description
		}
		if req.Status != nil && *req.Status != "" {
			p.Status = *req.Status
		}
		if req.Owner != nil {
			p.Owner = *req.Owner
		}
		if req.Tags != nil {
			p.Tags = req.Tags
		}
		if req.NextAction != nil {
			p.NextAction = *req.NextAction
		}
		if req.Metadata != nil {
			if p.Metadata == nil {
				p.Metadata = map[string]string{}
			}
			for k, v := range req.Metadata {
				p.Metadata[k] = v
			}
		}
		return nil
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to update project: %v", err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(updated)
}

func (s *APIServer) handleDeleteProject(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]
	if strings.TrimSpace(id) == "" {
		http.Error(w, "Project id required", http.StatusBadRequest)
		return
	}
	// Safety: prevent deletion of protected/system projects (e.g., Goals)
	if p, err := s.projectManager.GetProject(id); err == nil && p != nil {
		name := strings.ToLower(strings.TrimSpace(p.Name))
		if name == "goals" || name == "fsm-agent-agent_1" {
			http.Error(w, "Deletion of protected project is not allowed", http.StatusForbidden)
			return
		}
	}
	if err := s.projectManager.DeleteProject(id); err != nil {
		http.Error(w, fmt.Sprintf("Failed to delete project: %v", err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"success": true, "id": id})
}

func (s *APIServer) handlePauseProject(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]
	updated, err := s.projectManager.UpdateProject(id, func(p *Project) error {
		p.Status = "paused"
		return nil
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to pause project: %v", err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(updated)
}

func (s *APIServer) handleResumeProject(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]
	updated, err := s.projectManager.UpdateProject(id, func(p *Project) error {
		p.Status = "active"
		return nil
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to resume project: %v", err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(updated)
}

func (s *APIServer) handleArchiveProject(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]
	updated, err := s.projectManager.UpdateProject(id, func(p *Project) error {
		p.Status = "archived"
		return nil
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to archive project: %v", err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(updated)
}

func (s *APIServer) handleAddProjectCheckpoint(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]
	var req struct {
		Summary    string                 `json:"summary"`
		NextAction string                 `json:"next_action"`
		Context    map[string]interface{} `json:"context"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	cp, err := s.projectManager.AddCheckpoint(id, &ProjectCheckpoint{
		Summary:    req.Summary,
		NextAction: req.NextAction,
		Context:    req.Context,
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to add checkpoint: %v", err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(cp)
}

func (s *APIServer) handleListProjectCheckpoints(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]
	cps, err := s.projectManager.ListCheckpoints(id, 50)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to list checkpoints: %v", err), http.StatusInternalServerError)
		return
	}
	if cps == nil {
		cps = []*ProjectCheckpoint{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(cps)
}

func (s *APIServer) handleListProjectWorkflows(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]
	ids, err := s.projectManager.ListWorkflowIDs(id)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to list project workflows: %v", err), http.StatusInternalServerError)
		return
	}
	if ids == nil {
		ids = []string{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"workflow_ids": ids,
	})
}

// ensureProjectByName creates a project if a project with the same name does not already exist
func (s *APIServer) ensureProjectByName(name string) {
	safe := strings.TrimSpace(name)
	if safe == "" {
		return
	}
	if list, err := s.projectManager.ListProjects(); err == nil && list != nil {
		for _, p := range list {
			if strings.EqualFold(strings.TrimSpace(p.Name), safe) {
				return
			}
		}
	}
	// Create minimal project
	_, _ = s.projectManager.CreateProject(&Project{
		Name:        safe,
		Description: "Auto-created for executions",
		Status:      "active",
	})
}

// resolveProjectID returns a real project ID when given an ID or a name.
// If the input matches a project's ID, it is returned as-is.
// If it matches a project's Name (case-insensitive), the project's ID is returned.
// If no match is found, a new project is created with that name and its ID returned.
func (s *APIServer) resolveProjectID(idOrName string) string {
	candidate := strings.TrimSpace(idOrName)
	if candidate == "" {
		return ""
	}
	// First, try direct ID match
	if list, err := s.projectManager.ListProjects(); err == nil && list != nil {
		for _, p := range list {
			if strings.TrimSpace(p.ID) == candidate {
				return p.ID
			}
		}
		// Then, try name match (case-insensitive)
		for _, p := range list {
			if strings.EqualFold(strings.TrimSpace(p.Name), candidate) {
				return p.ID
			}
		}
	}
	// Create if missing by name
	proj, err := s.projectManager.CreateProject(&Project{
		Name:        candidate,
		Description: "Auto-created for executions",
		Status:      "active",
	})
	if err != nil || proj == nil || strings.TrimSpace(proj.ID) == "" {
		return candidate // fallback to original; link may no-op if invalid
	}
	return proj.ID
}

// handleResolveWorkflowID maps between intelligent_ and hierarchical workflow IDs.
// - If given an intelligent_ id, returns the hierarchical UUID it maps to (if any)
// - If given a hierarchical id, returns the corresponding intelligent_ id (if any)
// Falls back to the provided id when no mapping exists.
func (s *APIServer) handleResolveWorkflowID(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	resolved := map[string]string{
		"input":     id,
		"type":      "unknown",
		"mapped_id": id,
	}

	if strings.HasPrefix(id, "intelligent_") {
		// reverse mapping: intelligent -> hierarchical
		if hid, err := s.getReverseWorkflowMapping(id); err == nil && hid != "" {
			resolved["type"] = "intelligent"
			resolved["mapped_id"] = hid
		} else {
			resolved["type"] = "intelligent"
		}
	} else {
		// forward mapping: hierarchical -> intelligent
		if iid, err := s.getWorkflowMapping(id); err == nil && iid != "" {
			resolved["type"] = "hierarchical"
			resolved["mapped_id"] = iid
		} else {
			resolved["type"] = "hierarchical"
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resolved)
}

// --------- Core Planning and Execution ---------

func (s *APIServer) planTask(state State, taskName string) []string {
	// Convert enhanced domain to legacy format for planning
	legacyDomain := s.convertToLegacyDomain()
	return HTNPlan(state, taskName, &legacyDomain)
}

func (s *APIServer) executePlan(state State, plan []string) State {
	legacyDomain := s.convertToLegacyDomain()
	return ExecutePlan(state, plan, &legacyDomain)
}

func (s *APIServer) convertToLegacyDomain() Domain {
	legacy := Domain{
		Methods: make([]MethodDef, len(s.domain.Methods)),
		Actions: make([]ActionDef, len(s.domain.Actions)),
	}

	for i, method := range s.domain.Methods {
		legacy.Methods[i] = method.MethodDef
	}

	for i, action := range s.domain.Actions {
		legacy.Actions[i] = action.ActionDef
	}

	return legacy
}

// --------- Learning Methods ---------

func (s *APIServer) learnTraditional(req LearnRequest) (bool, *MethodDef, string) {
	// Use existing learning logic
	legacyDomain := s.convertToLegacyDomain()

	// Find missing predicates for the task
	var missing []string
	for _, action := range legacyDomain.Actions {
		if action.Task == req.TaskName {
			missing = missingPredicatesForAction(&action, make(State))
			break
		}
	}

	if len(missing) == 0 {
		return false, nil, "No missing predicates found for traditional learning"
	}

	learned := LearnMethodForMissing(req.TaskName, missing, &legacyDomain)
	if learned {
		// Find the learned method
		for _, method := range legacyDomain.Methods {
			if method.Task == req.TaskName && method.IsLearned {
				return true, &method, "Successfully learned method using traditional approach"
			}
		}
	}

	return false, nil, "Failed to learn using traditional approach"
}

func (s *APIServer) learnWithLLM(req LearnRequest) (bool, *MethodDef, string) {
	if s.llmClient == nil {
		return false, nil, "LLM client not configured"
	}

	// Use LLM to generate a method
	method, err := s.llmClient.GenerateMethod(req.TaskName, req.Description, req.Context)
	if err != nil {
		return false, nil, fmt.Sprintf("LLM learning failed: %v", err)
	}

	// Add to domain
	enhancedMethod := EnhancedMethodDef{
		MethodDef: *method,
		TaskType:  TaskTypeLLM,
		LLMPrompt: req.Description,
	}
	enhancedMethod.IsLearned = true

	s.domain.Methods = append([]EnhancedMethodDef{enhancedMethod}, s.domain.Methods...)

	return true, method, "Successfully learned method using LLM"
}

func (s *APIServer) learnWithMCP(req LearnRequest) (bool, *MethodDef, string) {
	if s.mcpClient == nil {
		return false, nil, "MCP client not configured"
	}

	// Use MCP to discover tools and create method
	method, err := s.mcpClient.GenerateMethod(req.TaskName, req.Description, req.Context)
	if err != nil {
		return false, nil, fmt.Sprintf("MCP learning failed: %v", err)
	}

	// Add to domain
	enhancedMethod := EnhancedMethodDef{
		MethodDef: *method,
		TaskType:  TaskTypeMCP,
		MCPTool:   req.Description, // This would be more sophisticated in practice
	}
	enhancedMethod.IsLearned = true

	s.domain.Methods = append([]EnhancedMethodDef{enhancedMethod}, s.domain.Methods...)

	return true, method, "Successfully learned method using MCP"
}

// --------- New Domain Management Handlers ---------

func (s *APIServer) handleListDomains(w http.ResponseWriter, r *http.Request) {
	domains, err := s.domainManager.ListDomains()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to list domains: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(domains)
}

func (s *APIServer) handleCreateDomain(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string       `json:"name"`
		Description string       `json:"description"`
		Tags        []string     `json:"tags"`
		Config      DomainConfig `json:"config"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if req.Name == "" {
		http.Error(w, "Domain name is required", http.StatusBadRequest)
		return
	}

	// Use current domain config if not provided
	if req.Config.LLMProvider == "" {
		req.Config = s.domain.Config
	}

	err := s.domainManager.CreateDomain(req.Name, req.Description, req.Config, req.Tags)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to create domain: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "created", "domain": req.Name})
}

func (s *APIServer) handleGetDomainByName(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	domainName := vars["name"]

	domain, err := s.domainManager.GetDomain(domainName)
	if err != nil {
		http.Error(w, fmt.Sprintf("Domain not found: %v", err), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(domain)
}

func (s *APIServer) handleDeleteDomain(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	domainName := vars["name"]

	err := s.domainManager.DeleteDomain(domainName)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to delete domain: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "deleted", "domain": domainName})
}

func (s *APIServer) handleSwitchDomain(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	domainName := vars["name"]

	domain, err := s.domainManager.GetDomain(domainName)
	if err != nil {
		http.Error(w, fmt.Sprintf("Domain not found: %v", err), http.StatusNotFound)
		return
	}

	// Convert to EnhancedDomain format
	enhancedDomain := &EnhancedDomain{
		Methods: make([]EnhancedMethodDef, len(domain.Methods)),
		Actions: make([]EnhancedActionDef, len(domain.Actions)),
		Config:  domain.Config,
	}

	for i, method := range domain.Methods {
		enhancedDomain.Methods[i] = EnhancedMethodDef{
			MethodDef: *method,
			TaskType:  TaskTypeMethod,
		}
	}

	for i, action := range domain.Actions {
		enhancedDomain.Actions[i] = EnhancedActionDef{
			ActionDef: *action,
			TaskType:  TaskTypePrimitive,
		}
	}

	// Load dynamic actions from Redis
	dynamicActions, err := s.actionManager.GetActionsByDomain(domainName)
	if err == nil {
		for _, dynamicAction := range dynamicActions {
			// Convert dynamic action to legacy action format
			actionDef := s.actionManager.ConvertToLegacyAction(dynamicAction)

			// Add to enhanced domain
			enhancedAction := EnhancedActionDef{
				ActionDef: *actionDef,
				TaskType:  TaskTypePrimitive,
			}
			enhancedDomain.Actions = append(enhancedDomain.Actions, enhancedAction)
		}
		log.Printf("✅ [DOMAIN] Loaded %d dynamic actions from Redis", len(dynamicActions))
	}

	s.domain = enhancedDomain
	s.currentDomain = domainName

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "switched", "domain": domainName})
}

// --------- New Action Management Handlers ---------

func (s *APIServer) handleCreateAction(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Task          string            `json:"task"`
		Preconditions []string          `json:"preconditions"`
		Effects       []string          `json:"effects"`
		TaskType      string            `json:"task_type"`
		Description   string            `json:"description"`
		Code          string            `json:"code,omitempty"`
		Language      string            `json:"language,omitempty"`
		Context       map[string]string `json:"context"`
		Domain        string            `json:"domain"`
		Tags          []string          `json:"tags"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if req.Task == "" {
		http.Error(w, "Task name is required", http.StatusBadRequest)
		return
	}

	if req.Domain == "" {
		req.Domain = s.currentDomain
		if req.Domain == "" {
			req.Domain = "default"
		}
	}

	action := &DynamicAction{
		Task:          req.Task,
		Preconditions: req.Preconditions,
		Effects:       req.Effects,
		TaskType:      req.TaskType,
		Description:   req.Description,
		Code:          req.Code,
		Language:      req.Language,
		Context:       req.Context,
		Domain:        req.Domain,
		Tags:          req.Tags,
	}

	err := s.actionManager.CreateAction(action)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to create action: %v", err), http.StatusInternalServerError)
		return
	}

	// Also add to current domain if it matches
	if req.Domain == s.currentDomain {
		actionDef := s.actionManager.ConvertToLegacyAction(action)
		s.domainManager.AddAction(req.Domain, actionDef)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "created", "action": req.Task, "domain": req.Domain})
}

func (s *APIServer) handleListActions(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	domainName := vars["domain"]

	actions, err := s.actionManager.GetActionsByDomain(domainName)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to list actions: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(actions)
}

func (s *APIServer) handleGetAction(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	domainName := vars["domain"]
	actionID := vars["id"]

	action, err := s.actionManager.GetAction(domainName, actionID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Action not found: %v", err), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(action)
}

func (s *APIServer) handleDeleteAction(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	domainName := vars["domain"]
	actionID := vars["id"]

	err := s.actionManager.DeleteAction(domainName, actionID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to delete action: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "deleted", "action": actionID, "domain": domainName})
}

func (s *APIServer) handleSearchActions(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	domainName := vars["domain"]

	var req struct {
		Query    string   `json:"query"`
		TaskType string   `json:"task_type"`
		Tags     []string `json:"tags"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	actions, err := s.actionManager.SearchActions(domainName, req.Query, req.TaskType, req.Tags)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to search actions: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(actions)
}

// --------- Intelligent Execution Handlers ---------

func (s *APIServer) handleIntelligentExecute(w http.ResponseWriter, r *http.Request) {
	// Add CORS headers for Monitor UI
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

	// Acquire execution slot (UI gets priority)
	release, acquired := s.acquireExecutionSlot(r)
	if !acquired {
		http.Error(w, "Server busy - too many concurrent executions. Please try again later.", http.StatusTooManyRequests)
		return
	}
	defer release()

	var req IntelligentExecutionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Validate required fields
	if req.TaskName == "" || req.Description == "" {
		http.Error(w, "Task name and description are required", http.StatusBadRequest)
		return
	}

	// Set defaults
	if req.Language == "" {
		// Try to infer language from the user request before applying defaults
		if inferred := inferLanguageFromRequest(&req); inferred != "" {
			req.Language = inferred
		} else {
			req.Language = "python"
		}
	}
	if req.MaxRetries == 0 {
		req.MaxRetries = 3
	}
	if req.Timeout == 0 {
		req.Timeout = 120 // Reduced from 600 to prevent long-running requests
	}

	// Create intelligent executor with planner integration
	executor := NewIntelligentExecutor(
		s.domainManager,
		s.codeStorage,
		s.codeGenerator,
		s.dockerExecutor,
		s.llmClient,
		s.actionManager,
		s.plannerIntegration,
		s.selfModelManager,
		s.toolMetrics,
		s.fileStorage,
		s.hdnBaseURL,
		s.redisAddr,
	)

	// Execute intelligently with timeout (reduced from 600s to prevent GPU overload)
	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()

	// Determine priority: default to high for user API requests, unless explicitly set to low
	highPriority := true // Default to high priority for user-facing API requests
	if req.Priority == "low" {
		highPriority = false
	}

	result, err := executor.ExecuteTaskIntelligently(ctx, &ExecutionRequest{
		TaskName:        req.TaskName,
		Description:     req.Description,
		Context:         req.Context,
		Language:        req.Language,
		ForceRegenerate: req.ForceRegenerate,
		MaxRetries:      req.MaxRetries,
		Timeout:         req.Timeout,
		HighPriority:    highPriority,
	})

	if err != nil {
		http.Error(w, fmt.Sprintf("Intelligent execution failed: %v", err), http.StatusInternalServerError)
		return
	}

	if result == nil {
		http.Error(w, "Intelligent execution returned no result", http.StatusInternalServerError)
		return
	}

	// Record metrics for monitor UI (guard nil)
	s.recordMonitorMetrics(result.Success, result.ExecutionTime)

	// Working memory: append execution event if session provided
	if req.Context != nil {
		if sid, ok := req.Context["session_id"]; ok && sid != "" {
			_ = s.workingMemory.AddEvent(sid, map[string]any{
				"type":      "intelligent_execution",
				"task_name": req.TaskName,
				"status": func() string {
					if result.Success {
						return "completed"
					}
					return "failed"
				}(),
				"success":      result.Success,
				"error":        result.Error,
				"execution_ms": result.ExecutionTime.Milliseconds(),
				"workflow_id":  result.WorkflowID,
				"timestamp":    time.Now().UTC(),
			}, 100)
		}
	}

	// Persist workflow→project mapping for Monitor UI resolution
	if s.redis != nil {
		ctx := context.Background()
		pid := strings.TrimSpace(req.ProjectID)
		wid := strings.TrimSpace(result.WorkflowID)
		if pid == "" && req.Context != nil {
			if v, ok := req.Context["project_id"]; ok && strings.TrimSpace(v) != "" {
				pid = strings.TrimSpace(v)
			}
		}
		if pid != "" && wid != "" {
			_ = s.redis.Set(ctx, "workflow_project:"+wid, pid, 24*time.Hour).Err()
		}
	}

	// Link workflow to project if provided (support name or id)
	if req.ProjectID != "" || (req.Context != nil && req.Context["project_id"] != "") {
		projectID := req.ProjectID
		if projectID == "" && req.Context != nil {
			if v, ok := req.Context["project_id"]; ok && strings.TrimSpace(v) != "" {
				projectID = strings.TrimSpace(v)
			}
		}
		if projectID != "" && result.WorkflowID != "" {
			pid := s.resolveProjectID(projectID)
			if linkErr := s.projectManager.LinkWorkflow(pid, result.WorkflowID); linkErr != nil {
				log.Printf("❌ [API] Failed to link intelligent workflow %s to project %s: %v", result.WorkflowID, pid, linkErr)
			} else {
				log.Printf("✅ [API] Linked intelligent workflow %s to project %s", result.WorkflowID, pid)
			}
		}
	}

	// Persist daily summary outputs to Redis when applicable (on success or failure)
	if strings.EqualFold(req.TaskName, "daily_summary") {
		go func() {
			defer func() { recover() }()
			if s.redis == nil {
				log.Printf("⚠️ [API] daily_summary: redis client is nil; skipping persistence")
				return
			}
			ctx := context.Background()

			// Generate summary from actual system data instead of LLM result
			text := s.generateDailySummaryFromSystemData(ctx)
			payload := map[string]any{
				"date":         time.Now().UTC().Format("2006-01-02"),
				"generated_at": time.Now().UTC().Format(time.RFC3339),
				"summary":      text,
			}
			b, _ := json.Marshal(payload)
			if err := s.redis.Set(ctx, "daily_summary:latest", string(b), 0).Err(); err != nil {
				log.Printf("❌ [API] daily_summary: failed to set latest: %v", err)
			} else {
				log.Printf("📝 [API] daily_summary: wrote latest (%d bytes)", len(b))
			}
			dateKey := "daily_summary:" + time.Now().UTC().Format("2006-01-02")
			if err := s.redis.Set(ctx, dateKey, string(b), 0).Err(); err != nil {
				log.Printf("❌ [API] daily_summary: failed to set %s: %v", dateKey, err)
			}
			if err := s.redis.LPush(ctx, "daily_summary:history", string(b)).Err(); err != nil {
				log.Printf("❌ [API] daily_summary: failed to LPUSH history: %v", err)
			} else {
				_ = s.redis.LTrim(ctx, "daily_summary:history", 0, 29).Err()
			}
		}()
	}

	// Write episodic trace (best-effort)
	if s.vectorDB != nil {
		sid := ""
		if req.Context != nil {
			sid = req.Context["session_id"]
		}
		ep := &mempkg.EpisodicRecord{
			SessionID: sid,
			PlanID:    "",
			Timestamp: time.Now().UTC(),
			Outcome: func() string {
				if result.Success {
					return "success"
				}
				return "failure"
			}(),
			Reward:    0,
			Tags:      []string{"intelligent"},
			StepIndex: 0,
			Text:      fmt.Sprintf("%s: %s", req.TaskName, req.Description),
			Metadata:  map[string]any{"workflow_id": result.WorkflowID},
		}
		vec := toyEmbed(ep.Text, 8)
		if err := s.vectorDB.IndexEpisode(ep, vec); err != nil {
			log.Printf("❌ [API] Weaviate indexing failed: %v", err)
		} else {
			log.Printf("✅ [API] Episode indexed in Weaviate: %s", ep.Text[:min(50, len(ep.Text))])
		}
	}

	// Create a workflow record for the Monitor UI to display
	// Use the workflow ID from the result if available, otherwise generate a new one
	workflowID := result.WorkflowID
	if workflowID == "" {
		workflowID = fmt.Sprintf("intelligent_%d", time.Now().UnixNano())
	}
	// createIntelligentWorkflowRecord may modify the workflow ID (adds intelligent_ prefix)
	// so we need to use the returned ID for linking
	storeID := s.createIntelligentWorkflowRecord(req, result, workflowID)

	// Link workflow to project if provided (support name or id)
	// Extract project_id from request or context for linking
	projectID := req.ProjectID
	if projectID == "" && req.Context != nil {
		if pid, ok := req.Context["project_id"]; ok && strings.TrimSpace(pid) != "" {
			projectID = strings.TrimSpace(pid)
		}
	}
	if projectID != "" {
		pid := s.resolveProjectID(projectID)
		if linkErr := s.projectManager.LinkWorkflow(pid, storeID); linkErr != nil {
			log.Printf("❌ [API] Failed to link workflow %s to project %s: %v", storeID, pid, linkErr)
		} else {
			log.Printf("✅ [API] Linked workflow %s to project %s", storeID, pid)
		}
	}

	// Auto-save artifacts so the Project UI shows files by default
	// 1) If code was generated, save the code with an appropriate filename/Content-Type
	if result.GeneratedCode != nil && result.GeneratedCode.Code != "" {
		codeCT := "text/plain"
		ext := ".txt"
		switch strings.ToLower(result.GeneratedCode.Language) {
		case "python", "py":
			codeCT = "text/x-python"
			ext = ".py"
		case "javascript", "js":
			codeCT = "application/javascript"
			ext = ".js"
		case "go", "golang":
			codeCT = "text/x-go"
			ext = ".go"
		case "markdown", "md":
			codeCT = "text/markdown"
			ext = ".md"
		}
		codeFilename := result.GeneratedCode.TaskName
		if codeFilename == "" {
			codeFilename = req.TaskName
		}
		if !strings.HasSuffix(strings.ToLower(codeFilename), ext) {
			codeFilename = codeFilename + ext
		}
		_ = s.fileStorage.StoreFile(&StoredFile{
			Filename:    codeFilename,
			Content:     []byte(sanitizeCode(result.GeneratedCode.Code)),
			ContentType: codeCT,
			Size:        int64(len(sanitizeCode(result.GeneratedCode.Code))),
			WorkflowID:  workflowID,
			StepID:      "final_execution",
		})
	} else if outStr, ok := result.Result.(string); ok && looksLikeCode(outStr) { // save code-like text outputs as source files
		codeCT := "text/plain"
		ext := ".txt"
		lang := strings.ToLower(req.Language)
		// Heuristic detect language if missing
		if lang == "" {
			if looksLikePython(outStr) {
				lang = "python"
			}
		}
		switch lang {
		case "python", "py":
			codeCT = "text/x-python"
			ext = ".py"
		case "javascript", "js":
			codeCT = "application/javascript"
			ext = ".js"
		case "go", "golang":
			codeCT = "text/x-go"
			ext = ".go"
		case "markdown", "md":
			codeCT = "text/markdown"
			ext = ".md"
		}
		codeFilename := req.TaskName
		if codeFilename == "" {
			codeFilename = "generated_code"
		}
		if !strings.HasSuffix(strings.ToLower(codeFilename), ext) {
			codeFilename += ext
		}
		_ = s.fileStorage.StoreFile(&StoredFile{
			Filename:    codeFilename,
			Content:     []byte(sanitizeCode(outStr)),
			ContentType: codeCT,
			Size:        int64(len(sanitizeCode(outStr))),
			WorkflowID:  workflowID,
			StepID:      "final_execution",
		})
	} else if result.NewAction != nil && result.NewAction.Code != "" { // save code from new action as artifact
		codeCT := "text/plain"
		ext := ".txt"
		lang := strings.ToLower(result.NewAction.Language)
		switch lang {
		case "python", "py":
			codeCT = "text/x-python"
			ext = ".py"
		case "javascript", "js":
			codeCT = "application/javascript"
			ext = ".js"
		case "go", "golang":
			codeCT = "text/x-go"
			ext = ".go"
		case "markdown", "md":
			codeCT = "text/markdown"
			ext = ".md"
		}
		codeFilename := result.NewAction.Task
		if codeFilename == "" {
			codeFilename = req.TaskName
		}
		if !strings.HasSuffix(strings.ToLower(codeFilename), ext) {
			codeFilename = codeFilename + ext
		}
		_ = s.fileStorage.StoreFile(&StoredFile{
			Filename:    codeFilename,
			Content:     []byte(sanitizeCode(result.NewAction.Code)),
			ContentType: codeCT,
			Size:        int64(len(sanitizeCode(result.NewAction.Code))),
			WorkflowID:  workflowID,
			StepID:      "final_execution",
		})
	}

	// 2) Save the generic result output as a fallback (sanitized)
	if result.Result != nil {
		var content []byte
		var contentType string
		// Use unique filename based on task name and timestamp to avoid mixing outputs
		filename := fmt.Sprintf("output_%s_%d.txt", strings.ReplaceAll(req.TaskName, " ", "_"), time.Now().UnixNano())
		switch v := result.Result.(type) {
		case string:
			content = []byte(sanitizeConsoleOutput(v))
			contentType = "text/plain"
		default:
			if b, err := json.Marshal(v); err == nil {
				content = b
				contentType = "application/json"
				filename = fmt.Sprintf("output_%s_%d.json", strings.ReplaceAll(req.TaskName, " ", "_"), time.Now().UnixNano())
			}
		}
		if len(content) > 0 {
			_ = s.fileStorage.StoreFile(&StoredFile{
				Filename:    filename,
				Content:     content,
				ContentType: contentType,
				Size:        int64(len(content)),
				WorkflowID:  workflowID,
				StepID:      "final_execution",
			})
		}
	}

	// Honor artifact hints from context: save_code_filename, artifact_names, save_pdf, want_preview
	if req.Context != nil {
		// Save generated code under an explicit filename if provided
		if name, ok := req.Context["save_code_filename"]; ok && result.GeneratedCode != nil && name != "" {
			codeCT := "text/plain"
			lowerName := strings.ToLower(name)
			if strings.HasSuffix(lowerName, ".py") {
				codeCT = "text/x-python"
			} else if strings.HasSuffix(lowerName, ".go") {
				codeCT = "text/x-go"
			} else if strings.HasSuffix(lowerName, ".js") {
				codeCT = "application/javascript"
			} else if strings.HasSuffix(lowerName, ".java") {
				codeCT = "text/x-java-source"
			}
			_ = s.fileStorage.StoreFile(&StoredFile{
				Filename:    name,
				Content:     []byte(sanitizeCode(result.GeneratedCode.Code)),
				ContentType: codeCT,
				Size:        int64(len(sanitizeCode(result.GeneratedCode.Code))),
				WorkflowID:  workflowID,
				StepID:      "final_execution",
			})
		}

		// Discover existing stored files to avoid overwriting real artifacts
		existing, _ := s.fileStorage.GetFilesByWorkflow(workflowID)
		existingNames := make(map[string]struct{})
		existingPDF := false
		for _, f := range existing {
			name := strings.ToLower(f.Filename)
			existingNames[name] = struct{}{}
			if strings.HasSuffix(name, ".pdf") {
				existingPDF = true
			}
		}

		// Additional artifacts list (ensure a default fallback PDF if requested and none exists)
		list, hasList := req.Context["artifact_names"]
		parts := []string{}
		if hasList && list != "" {
			parts = strings.Split(list, ",")
			for i := range parts {
				parts[i] = strings.TrimSpace(parts[i])
			}
		}
		if pdfFlag, ok := req.Context["save_pdf"]; ok && strings.ToLower(pdfFlag) == "true" {
			hasPDF := existingPDF
			if !hasPDF {
				for _, p := range parts {
					if strings.HasSuffix(strings.ToLower(p), ".pdf") {
						hasPDF = true
						break
					}
				}
			}
			// If still no PDFs, schedule a fallback name that won't collide with real artifacts
			if !hasPDF {
				parts = append(parts, "artifacts_report.pdf")
			}
		}
		if len(parts) > 0 {
			for _, fname := range parts {
				if fname == "" {
					continue
				}
				low := strings.ToLower(fname)
				if _, exists := existingNames[low]; exists {
					// Avoid overwriting real artifacts saved by execution
					continue
				}
				if (strings.HasSuffix(low, ".py") || strings.HasSuffix(low, ".go") || strings.HasSuffix(low, ".js") || strings.HasSuffix(low, ".java")) && result.GeneratedCode != nil {
					// Save code under this filename (if different)
					contentType := "text/plain"
					switch {
					case strings.HasSuffix(low, ".py"):
						contentType = "text/x-python"
					case strings.HasSuffix(low, ".go"):
						contentType = "text/x-go"
					case strings.HasSuffix(low, ".js"):
						contentType = "application/javascript"
					case strings.HasSuffix(low, ".java"):
						contentType = "text/x-java-source"
					}
					_ = s.fileStorage.StoreFile(&StoredFile{
						Filename:    fname,
						Content:     []byte(sanitizeCode(result.GeneratedCode.Code)),
						ContentType: contentType,
						Size:        int64(len(sanitizeCode(result.GeneratedCode.Code))),
						WorkflowID:  workflowID,
						StepID:      "final_execution",
					})
				} else if strings.HasSuffix(low, ".md") {
					// Save textual result as Markdown if available
					if result.Result != nil {
						var content []byte
						switch v := result.Result.(type) {
						case string:
							content = []byte(v)
						default:
							if b, err := json.Marshal(v); err == nil {
								content = b
							}
						}
						if len(content) > 0 {
							_ = s.fileStorage.StoreFile(&StoredFile{
								Filename:    fname,
								Content:     content,
								ContentType: "text/markdown",
								Size:        int64(len(content)),
								WorkflowID:  workflowID,
								StepID:      "final_execution",
							})
						}
					}
				} else if strings.HasSuffix(low, ".pdf") {
					// Create a richer PDF: prefer last validation output, include artifact list
					var payload interface{}
					if len(result.ValidationSteps) > 0 {
						payload = result.ValidationSteps[len(result.ValidationSteps)-1].Output
					}
					if payload == nil {
						payload = result.Result
					}
					// Build a small summary string including filenames
					summary := ""
					if payloadStr, ok := payload.(string); ok && payloadStr != "" {
						summary = payloadStr
					} else if payload != nil {
						if b, err := json.Marshal(payload); err == nil {
							summary = string(b)
						}
					}
					if hasList && list != "" {
						summary = summary + "\nFiles: " + list
					}
					pdf := s.createSimplePDF("Artifacts Report", "Generated by intelligent executor", summary)
					_ = s.fileStorage.StoreFile(&StoredFile{
						Filename:    fname,
						Content:     pdf,
						ContentType: "application/pdf",
						Size:        int64(len(pdf)),
						WorkflowID:  workflowID,
						StepID:      "final_execution",
					})
				}
			}
		}
	}

	// Convert to response format
	response := IntelligentExecutionResponse{
		Success: result.Success,
		Result: func() interface{} {
			if s, ok := result.Result.(string); ok {
				return sanitizeConsoleOutput(s)
			}
			return result.Result
		}(),
		Error:           result.Error,
		GeneratedCode:   result.GeneratedCode,
		ExecutionTime:   result.ExecutionTime.Milliseconds(),
		RetryCount:      result.RetryCount,
		UsedCachedCode:  result.UsedCachedCode,
		ValidationSteps: result.ValidationSteps,
		NewAction:       result.NewAction,
		WorkflowID:      workflowID, // Add workflow ID to response
	}

	// Send HTTP response immediately
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)

	// Add grouped code preview if available (files: [{filename, language, code}])
	if result.GeneratedCode != nil {
		filename := "output.txt"
		if result.GeneratedCode.Language == "python" {
			filename = req.TaskName + ".py"
		}
		response.Preview = map[string]interface{}{
			"files": []map[string]interface{}{
				{
					"filename": filename,
					"language": result.GeneratedCode.Language,
					"code":     result.GeneratedCode.Code,
				},
			},
		}
	} else if result.Result != nil {
		if s, ok := result.Result.(string); ok {
			response.Preview = map[string]interface{}{
				"files": []map[string]interface{}{
					{
						"filename": "output.txt",
						"language": "text",
						"code":     s,
					},
				},
			}
		}
	} else if result.NewAction != nil && result.NewAction.Code != "" {
		lang := result.NewAction.Language
		if lang == "" {
			lang = "text"
		}
		response.Preview = map[string]interface{}{
			"files": []map[string]interface{}{
				{
					"filename": req.TaskName + "." + lang,
					"language": lang,
					"code":     result.NewAction.Code,
				},
			},
		}
	}
}

// looksLikeCode provides a minimal heuristic to detect code-like text outputs
func looksLikeCode(s string) bool {
	ls := strings.TrimSpace(s)
	if ls == "" {
		return false
	}
	// Common code cues
	cues := []string{
		"def ", "class ", "import ", "from ", "function ", "package ", "#include", "const ", "let ", "var ", "func ",
	}
	for _, c := range cues {
		if strings.Contains(ls, c) {
			return true
		}
	}
	// Has multiple newlines and braces/colons
	if strings.Count(ls, "\n") >= 2 && (strings.Contains(ls, ":") || strings.Contains(ls, "{") || strings.Contains(ls, "}")) {
		return true
	}
	return false
}

// looksLikePython detects simple Python snippets
func looksLikePython(s string) bool {
	ls := strings.ToLower(strings.TrimSpace(s))
	if strings.Contains(ls, "def ") || strings.Contains(ls, "import ") || strings.Contains(ls, "from ") {
		return true
	}
	return false
}

// createIntelligentWorkflowRecord creates a workflow record for the Monitor UI to display
func (s *APIServer) createIntelligentWorkflowRecord(req IntelligentExecutionRequest, result *IntelligentExecutionResult, workflowID string) string {

	// Ensure monitor can discover intelligent workflows: use intelligent_ prefix
	storeID := workflowID
	if !strings.HasPrefix(storeID, "intelligent_") {
		storeID = "intelligent_" + storeID
	}

	// Extract project_id from request or context
	projectID := req.ProjectID
	if projectID == "" && req.Context != nil {
		if pid, ok := req.Context["project_id"]; ok && strings.TrimSpace(pid) != "" {
			projectID = strings.TrimSpace(pid)
		}
	}

	// Create a workflow record that the Monitor UI can display
	workflowRecord := map[string]interface{}{
		"id":               storeID,
		"name":             req.TaskName, // Set the name field for UI display
		"task_name":        req.TaskName,
		"description":      req.Description,
		"status":           "completed",
		"progress":         100.0,
		"total_steps":      1,
		"completed_steps":  1,
		"failed_steps":     0,
		"current_step":     "intelligent_execution",
		"started_at":       time.Now().Add(-result.ExecutionTime).Format(time.RFC3339),
		"updated_at":       time.Now().Format(time.RFC3339),
		"error":            result.Error,
		"generated_code":   result.GeneratedCode,
		"execution_time":   result.ExecutionTime.Milliseconds(),
		"retry_count":      result.RetryCount,
		"used_cached_code": result.UsedCachedCode,
		"validation_steps": result.ValidationSteps,
		"files":            []interface{}{}, // Will be populated from validation steps
		"steps":            []interface{}{}, // Will be populated from validation steps
		"project_id":       projectID,       // Store project_id in workflow record
	}

	// Debug message to confirm project_id is being stored
	log.Printf("🔧 [HDN] Creating workflow record with project_id: %s for workflow: %s", projectID, storeID)

	// Extract files from file storage only (skip validation outputs to avoid duplicates in UI)
	var files []interface{}

	// Then, try to get actual generated files from file storage
	if result.GeneratedCode != nil {
		// Get files from file storage using the workflow ID that was used for file storage
		redisAddrRaw := getenvTrim("REDIS_URL")
		redisAddr := normalizeRedisAddr(redisAddrRaw)
		fileStorage := NewFileStorage(redisAddr, 24)
		storedFiles, err := fileStorage.GetFilesByWorkflow(workflowID)
		if err == nil {
			for _, file := range storedFiles {
				// Read file content
				storedFile, err := fileStorage.GetFile(file.ID)
				if err == nil {
					files = append(files, map[string]interface{}{
						"filename":     storedFile.Filename,
						"content_type": storedFile.ContentType,
						"size":         storedFile.Size,
						"content":      string(storedFile.Content),
					})
				}
			}
		}
	}

	workflowRecord["files"] = files

	// Store or update the workflow record in Redis (avoid duplicate records)
	workflowKey := fmt.Sprintf("workflow:%s", storeID)
	existing, _ := s.redis.Get(context.Background(), workflowKey).Result()
	if existing != "" {
		// Merge minimal updates
		var old map[string]interface{}
		_ = json.Unmarshal([]byte(existing), &old)
		for k, v := range workflowRecord {
			old[k] = v
		}
		workflowJSON, _ := json.Marshal(old)
		s.redis.Set(context.Background(), workflowKey, workflowJSON, 24*time.Hour)
	} else {
		workflowJSON, _ := json.Marshal(workflowRecord)
		s.redis.Set(context.Background(), workflowKey, workflowJSON, 24*time.Hour)
	}

	// Since this record is for a completed intelligent execution, ensure it's not marked as active
	activeWorkflowsKey := "active_workflows"
	s.redis.SRem(context.Background(), activeWorkflowsKey, storeID)
	s.redis.Expire(context.Background(), activeWorkflowsKey, 24*time.Hour)

	// Store workflow mapping if this is an intelligent execution workflow
	if strings.HasPrefix(storeID, "intelligent_") {
		s.storeWorkflowMapping(workflowID, storeID)
	}

	log.Printf("📊 [API] Created intelligent workflow record: %s", storeID)
	return storeID
}

// Send HTTP response for intelligent execution
func (s *APIServer) sendIntelligentExecuteResponse(w http.ResponseWriter, result *IntelligentExecutionResult, workflowID string) {
	response := map[string]interface{}{
		"success":        result.Success,
		"workflow_id":    workflowID,
		"execution_time": result.ExecutionTime.Milliseconds(),
		"error":          result.Error,
		"generated_code": result.GeneratedCode,
		"result":         result.Result,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// storeWorkflowMapping stores a mapping between hierarchical and intelligent workflow IDs
func (s *APIServer) storeWorkflowMapping(hierarchicalID, intelligentID string) {
	ctx := context.Background()

	// Store mapping: hierarchical -> intelligent
	mappingKey := fmt.Sprintf("workflow_mapping:%s", hierarchicalID)
	s.redis.Set(ctx, mappingKey, intelligentID, 24*time.Hour)

	// Store reverse mapping: intelligent -> hierarchical
	reverseMappingKey := fmt.Sprintf("workflow_mapping_reverse:%s", intelligentID)
	s.redis.Set(ctx, reverseMappingKey, hierarchicalID, 24*time.Hour)

	log.Printf("🔗 [API] Stored workflow mapping: %s -> %s", hierarchicalID, intelligentID)
}

// getWorkflowMapping retrieves the intelligent workflow ID for a hierarchical workflow ID
func (s *APIServer) getWorkflowMapping(hierarchicalID string) (string, error) {
	ctx := context.Background()
	mappingKey := fmt.Sprintf("workflow_mapping:%s", hierarchicalID)

	intelligentID, err := s.redis.Get(ctx, mappingKey).Result()
	if err != nil {
		return "", fmt.Errorf("no mapping found for hierarchical workflow %s", hierarchicalID)
	}

	return intelligentID, nil
}

// getReverseWorkflowMapping retrieves the hierarchical workflow ID for an intelligent workflow ID
func (s *APIServer) getReverseWorkflowMapping(intelligentID string) (string, error) {
	ctx := context.Background()
	reverseMappingKey := fmt.Sprintf("workflow_mapping_reverse:%s", intelligentID)

	hierarchicalID, err := s.redis.Get(ctx, reverseMappingKey).Result()
	if err != nil {
		return "", fmt.Errorf("no reverse mapping found for intelligent workflow %s", intelligentID)
	}

	return hierarchicalID, nil
}

// handleIntelligentExecuteOptions handles CORS preflight requests
func (s *APIServer) handleIntelligentExecuteOptions(w http.ResponseWriter, r *http.Request) {
	// Add CORS headers for Monitor UI
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
	w.WriteHeader(http.StatusOK)
}

// Add missing HTTP response to handleIntelligentExecute
func (s *APIServer) handleIntelligentExecuteResponse(w http.ResponseWriter, result *IntelligentExecutionResult, workflowID string) {
	response := map[string]interface{}{
		"success":        result.Success,
		"workflow_id":    workflowID,
		"execution_time": result.ExecutionTime.Milliseconds(),
		"error":          result.Error,
		"generated_code": result.GeneratedCode,
		"result":         result.Result,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (s *APIServer) handlePrimeNumbers(w http.ResponseWriter, r *http.Request) {
	var req PrimeNumbersRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if req.Count <= 0 {
		req.Count = 10 // Default to 10 primes
	}

	// Create intelligent executor
	executor := NewIntelligentExecutor(
		s.domainManager,
		s.codeStorage,
		s.codeGenerator,
		s.dockerExecutor,
		s.llmClient,
		s.actionManager,
		s.plannerIntegration,
		s.selfModelManager,
		s.toolMetrics,
		s.fileStorage,
		s.hdnBaseURL,
		s.redisAddr,
	)

	// Execute prime numbers example
	ctx := r.Context()
	result, err := executor.ExecutePrimeNumbersExample(ctx, req.Count)

	if err != nil {
		http.Error(w, fmt.Sprintf("Prime numbers execution failed: %v", err), http.StatusInternalServerError)
		return
	}

	// Convert to response format
	response := IntelligentExecutionResponse{
		Success:         result.Success,
		Result:          result.Result,
		Error:           result.Error,
		GeneratedCode:   result.GeneratedCode,
		ExecutionTime:   result.ExecutionTime.Milliseconds(),
		RetryCount:      result.RetryCount,
		UsedCachedCode:  result.UsedCachedCode,
		ValidationSteps: result.ValidationSteps,
		NewAction:       result.NewAction,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (s *APIServer) handleListCapabilities(w http.ResponseWriter, r *http.Request) {
	// Create intelligent executor
	executor := NewIntelligentExecutor(
		s.domainManager,
		s.codeStorage,
		s.codeGenerator,
		s.dockerExecutor,
		s.llmClient,
		s.actionManager,
		s.plannerIntegration,
		s.selfModelManager,
		s.toolMetrics,
		s.fileStorage,
		s.hdnBaseURL,
		s.redisAddr,
	)

	// Get cached capabilities
	capabilities, err := executor.ListCachedCapabilities()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to list capabilities: %v", err), http.StatusInternalServerError)
		return
	}

	// Get execution stats
	stats := executor.GetExecutionStats()

	response := CapabilitiesResponse{
		Capabilities: capabilities,
		Stats:        stats,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// --------- Hierarchical Planning Handlers ---------

func (s *APIServer) handleHierarchicalExecute(w http.ResponseWriter, r *http.Request) {
	var req HierarchicalTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	// Infer project_id from context when not provided at top-level
	if strings.TrimSpace(req.ProjectID) == "" && req.Context != nil {
		if pid, ok := req.Context["project_id"]; ok && strings.TrimSpace(pid) != "" {
			req.ProjectID = pid
		}
	}

	// Always run asynchronously: create initial workflow record and return 202
	wfID := fmt.Sprintf("intelligent_%d", time.Now().UnixNano())
	initial := map[string]any{
		"id":          wfID,
		"name":        req.TaskName,
		"task_name":   req.TaskName,
		"description": req.Description,
		"status":      "running",
		"progress":    0.0,
		"started_at":  time.Now().Format(time.RFC3339),
		"updated_at":  time.Now().Format(time.RFC3339),
		"steps":       []any{},
		"files":       []any{},
	}
	if b, err := json.Marshal(initial); err == nil {
		key := fmt.Sprintf("workflow:%s", wfID)
		_ = s.redis.Set(context.Background(), key, string(b), 24*time.Hour).Err()
		log.Printf("📊 [API] Created initial workflow record (running): %s", wfID)
	}

	go func(req HierarchicalTaskRequest, wfID string) {
		// For async execution, we need to create a mock request to check UI status
		// Since this is async, we'll use general slot only
		// Wait for a slot with timeout instead of immediately rejecting
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		
		select {
		case s.executionSemaphore <- struct{}{}:
			defer func() { <-s.executionSemaphore }()
		case <-ctx.Done():
			log.Printf("❌ [API] Async execution rejected - timeout waiting for execution slot after 60s")
			// Update workflow status to failed
			key := fmt.Sprintf("workflow:%s", wfID)
			if val, err := s.redis.Get(context.Background(), key).Result(); err == nil {
				var rec map[string]any
				if json.Unmarshal([]byte(val), &rec) == nil {
					rec["status"] = "failed"
					rec["error"] = "Timeout waiting for execution slot"
					rec["updated_at"] = time.Now().Format(time.RFC3339)
					if b, err := json.Marshal(rec); err == nil {
						_ = s.redis.Set(context.Background(), key, string(b), 24*time.Hour).Err()
					}
				}
			}
			return
		}

		defer func() {
			// touch updated_at
			key := fmt.Sprintf("workflow:%s", wfID)
			if val, err := s.redis.Get(context.Background(), key).Result(); err == nil {
				var rec map[string]any
				if json.Unmarshal([]byte(val), &rec) == nil {
					rec["updated_at"] = time.Now().Format(time.RFC3339)
					if b, err := json.Marshal(rec); err == nil {
						_ = s.redis.Set(context.Background(), key, string(b), 24*time.Hour).Err()
					}
				}
			}
		}()

		// Existing logic executed in background
		if s.isSimplePrompt(req) {
			executor := NewIntelligentExecutor(
				s.domainManager,
				s.codeStorage,
				s.codeGenerator,
				s.dockerExecutor,
				s.llmClient,
				s.actionManager,
				s.plannerIntegration,
				s.selfModelManager,
				s.toolMetrics,
				s.fileStorage,
				s.hdnBaseURL,
				s.redisAddr,
			)

			ctx := context.Background()
			result, err := executor.ExecuteTaskIntelligently(ctx, &ExecutionRequest{
				TaskName:        req.TaskName,
				Description:     req.Description,
				Context:         req.Context,
				Language:        "python",
				ForceRegenerate: false,
				MaxRetries:      2,
				Timeout:         600,
			})
			if err != nil {
				log.Printf("❌ [API] Async direct execution failed: %v", err)
			}
			// Persist workflow record
			storeID := s.createIntelligentWorkflowRecord(IntelligentExecutionRequest{
				TaskName:    req.TaskName,
				Description: req.Description,
				Context:     req.Context,
				Language:    "python",
			}, result, wfID)

			// Link workflow to project if provided
			if req.ProjectID != "" {
				pid := s.resolveProjectID(req.ProjectID)
				if linkErr := s.projectManager.LinkWorkflow(pid, storeID); linkErr != nil {
					log.Printf("❌ [API] Failed to link workflow %s to project %s: %v", storeID, pid, linkErr)
				} else {
					log.Printf("✅ [API] Linked workflow %s to project %s", storeID, pid)
				}
			}

			// Post-completion hooks: episodic/working memory and auto-achieve goals when artifacts exist
			sessionID := ""
			goalID := ""
			if req.Context != nil {
				sessionID = req.Context["session_id"]
				goalID = req.Context["goal_id"]
			}

			// Detect artifacts
			hasArtifacts := false
			{
				redisAddr := getenvTrim("REDIS_URL")
				if redisAddr == "" {
					redisAddr = "localhost:6379"
				} else {
					// Strip redis:// prefix if present
					if strings.HasPrefix(redisAddr, "redis://") {
						redisAddr = strings.TrimPrefix(redisAddr, "redis://")
					}
					// Remove trailing slash if present
					redisAddr = strings.TrimSuffix(redisAddr, "/")
				}
				fs := NewFileStorage(redisAddr, 24)
				if files, err := fs.GetFilesByWorkflow(wfID); err == nil && len(files) > 0 {
					hasArtifacts = true
				}
			}

			// Episodic memory entry (best-effort)
			if sessionID != "" && s.vectorDB != nil {
				text := fmt.Sprintf("Workflow %s finished: success=%v, artifacts=%v", wfID, result != nil && result.Success, hasArtifacts)
				ep := &mempkg.EpisodicRecord{
					SessionID: sessionID,
					Timestamp: time.Now().UTC(),
					Outcome: func() string {
						if result != nil && result.Success {
							return "success"
						}
						return "failure"
					}(),
					Tags: []string{"workflow", "completion"},
					Text: text,
					Metadata: map[string]any{
						"workflow_id": wfID,
						"goal_id":     goalID,
						"artifacts":   hasArtifacts,
					},
				}
				vec := toyEmbed(ep.Text, 8)
				_ = s.vectorDB.IndexEpisode(ep, vec)
			}

			// Working memory summary
			if sessionID != "" && s.workingMemory != nil {
				summary := map[string]any{
					"type":        "workflow_summary",
					"workflow_id": wfID,
					"task_name":   req.TaskName,
					"description": req.Description,
					"success":     result != nil && result.Success,
					"artifacts":   hasArtifacts,
					"timestamp":   time.Now().UTC(),
				}
				_ = s.workingMemory.AddEvent(sessionID, summary, 50)
			}

			// Auto-achieve goal when successful and artifacts produced
			if goalID != "" && result != nil && result.Success && hasArtifacts {
				base := strings.TrimSpace(os.Getenv("GOAL_MANAGER_URL"))
				if base == "" {
					base = "http://localhost:8090"
				}
				achURL := fmt.Sprintf("%s/goal/%s/achieve", strings.TrimRight(base, "/"), goalID)
				reqAch, _ := http.NewRequest("POST", achURL, nil)
				client := &http.Client{Timeout: 5 * time.Second}
				if resp, err := client.Do(reqAch); err != nil {
					log.Printf("⚠️ [API] Auto-achieve goal %s failed: %v", goalID, err)
				} else {
					if resp.Body != nil {
						resp.Body.Close()
					}
					if resp.StatusCode >= 200 && resp.StatusCode < 300 {
						log.Printf("🎯 [API] Goal %s marked achieved (workflow %s)", goalID, wfID)
					} else {
						log.Printf("⚠️ [API] Auto-achieve goal %s returned status %d", goalID, resp.StatusCode)
					}
				}
			}
			return
		}

		// Non-simple: start hierarchical workflow via planner
		execution, err := s.plannerIntegration.StartHierarchicalWorkflow(
			req.UserRequest,
			req.TaskName,
			req.Description,
			req.Context,
		)
		if err != nil {
			log.Printf("❌ [API] Failed to start hierarchical workflow: %v", err)
			return
		}
		// Optionally link to project and record event
		if req.ProjectID != "" {
			// Ensure project exists by name
			s.ensureProjectByName(req.ProjectID)
			{
				pid := s.resolveProjectID(req.ProjectID)
				if linkErr := s.projectManager.LinkWorkflow(pid, execution.ID); linkErr != nil {
					log.Printf("❌ [API] Failed to link hierarchical workflow %s to project %s: %v", execution.ID, pid, linkErr)
				} else {
					log.Printf("✅ [API] Linked hierarchical workflow %s to project %s", execution.ID, pid)
				}
			}
		}
		if req.Context != nil {
			if sid, ok := req.Context["session_id"]; ok && sid != "" {
				_ = s.workingMemory.AddEvent(sid, map[string]any{
					"type":        "hierarchical_started",
					"task_name":   req.TaskName,
					"workflow_id": execution.ID,
					"timestamp":   time.Now().UTC(),
				}, 100)
			}
		}
		log.Printf("📡 [API] Hierarchical workflow started: %s", execution.ID)
	}(req, wfID)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(HierarchicalTaskResponse{
		Success:    true,
		WorkflowID: wfID,
		Message:    "Accepted for asynchronous execution",
	})
	return

	// Unreachable legacy synchronous path retained below for reference; function returns above

	// Simple prompt heuristic: skip hierarchical planning and execute directly
	if s.isSimplePrompt(req) {
		// Create intelligent executor with planner integration
		executor := NewIntelligentExecutor(
			s.domainManager,
			s.codeStorage,
			s.codeGenerator,
			s.dockerExecutor,
			s.llmClient,
			s.actionManager,
			s.plannerIntegration,
			s.selfModelManager,
			s.toolMetrics,
			s.fileStorage,
			s.hdnBaseURL,
			s.redisAddr,
		)

		ctx := r.Context()
		result, err := executor.ExecuteTaskIntelligently(ctx, &ExecutionRequest{
			TaskName:        req.TaskName,
			Description:     req.Description,
			Context:         req.Context,
			Language:        "python",
			ForceRegenerate: false,
			MaxRetries:      2,
			Timeout:         600,
		})
		if err != nil {
			response := HierarchicalTaskResponse{
				Success: false,
				Error:   err.Error(),
				Message: "Direct intelligent execution failed",
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
			return
		}

		// Ensure a workflow record exists for monitor UI
		wfID := result.WorkflowID
		if wfID == "" {
			wfID = fmt.Sprintf("intelligent_%d", time.Now().UnixNano())
		}
		_ = s.createIntelligentWorkflowRecord(IntelligentExecutionRequest{
			TaskName:    req.TaskName,
			Description: req.Description,
			Context:     req.Context,
			Language:    "python",
		}, result, wfID)

		response := HierarchicalTaskResponse{
			Success:    true,
			WorkflowID: wfID,
			Message:    "Executed directly (simple prompt)",
		}

		// Working memory: record direct execution if session_id present in context
		if req.Context != nil {
			if sid, ok := req.Context["session_id"]; ok && sid != "" {
				_ = s.workingMemory.AddEvent(sid, map[string]any{
					"type":        "hierarchical_direct",
					"task_name":   req.TaskName,
					"workflow_id": wfID,
					"timestamp":   time.Now().UTC(),
				}, 100)
			}
		}

		// Write episodic trace (best-effort) for hierarchical execution
		if s.vectorDB != nil {
			sid := ""
			if req.Context != nil {
				sid = req.Context["session_id"]
			}
			ep := &mempkg.EpisodicRecord{
				SessionID: sid,
				PlanID:    "",
				Timestamp: time.Now().UTC(),
				Outcome: func() string {
					if result.Success {
						return "success"
					}
					return "failure"
				}(),
				Reward:    0,
				Tags:      []string{"hierarchical"},
				StepIndex: 0,
				Text:      fmt.Sprintf("%s: %s", req.TaskName, req.Description),
				Metadata:  map[string]any{"workflow_id": wfID},
			}
			vec := toyEmbed(ep.Text, 8)
			if err := s.vectorDB.IndexEpisode(ep, vec); err != nil {
				log.Printf("❌ [API] Weaviate indexing failed: %v", err)
			} else {
				log.Printf("✅ [API] Episode indexed in Weaviate: %s", ep.Text[:min(50, len(ep.Text))])
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
		return
	}

	// Start hierarchical workflow
	execution, err := s.plannerIntegration.StartHierarchicalWorkflow(
		req.UserRequest,
		req.TaskName,
		req.Description,
		req.Context,
	)
	if err != nil {
		response := HierarchicalTaskResponse{
			Success: false,
			Error:   err.Error(),
			Message: "Failed to start hierarchical workflow",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
		return
	}

	// Link workflow to project if provided
	if req.ProjectID != "" {
		{
			pid := s.resolveProjectID(req.ProjectID)
			if linkErr := s.projectManager.LinkWorkflow(pid, execution.ID); linkErr != nil {
				log.Printf("❌ [API] Failed to link workflow %s to project %s: %v", execution.ID, pid, linkErr)
			} else {
				log.Printf("✅ [API] Linked workflow %s to project %s", execution.ID, pid)
			}
		}
	}

	response := HierarchicalTaskResponse{
		Success:    true,
		WorkflowID: execution.ID,
		Message:    "Hierarchical workflow started successfully",
	}

	// Working memory: record workflow start if session_id present in context
	if req.Context != nil {
		if sid, ok := req.Context["session_id"]; ok && sid != "" {
			_ = s.workingMemory.AddEvent(sid, map[string]any{
				"type":        "hierarchical_started",
				"task_name":   req.TaskName,
				"workflow_id": execution.ID,
				"timestamp":   time.Now().UTC(),
			}, 100)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// isSimplePrompt applies a lightweight heuristic to decide if the prompt is single-step
func (s *APIServer) isSimplePrompt(req HierarchicalTaskRequest) bool {
	// Consider simple when minimal context and no strong multi-step cues
	text := strings.ToLower(strings.TrimSpace(req.UserRequest + " " + req.TaskName + " " + req.Description))
	if text == "" {
		return false
	}
	// Multi-step indicators
	cues := []string{" and then ", " then ", " step ", ";", " -> ", "→"}
	for _, c := range cues {
		if strings.Contains(text, c) {
			return false
		}
	}
	// Additional multi-step hints common in artifact-producing tasks
	extra := []string{" and ", " produce ", " create ", " generate ", " save ", " pdf", " image", " module", " file"}
	for _, c := range extra {
		if strings.Contains(text, c) {
			return false
		}
	}
	return true
}

func (s *APIServer) handleGetWorkflowStatus(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	workflowID := vars["id"]

	status, err := s.plannerIntegration.GetWorkflowStatus(workflowID)
	if err != nil {
		response := WorkflowStatusResponse{
			Success: false,
			Error:   err.Error(),
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
		return
	}

	response := WorkflowStatusResponse{
		Success: true,
		Status:  status,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleGetWorkflowDetails returns full workflow detail including steps and dependencies
func (s *APIServer) handleGetWorkflowDetails(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	workflowID := vars["id"]

	details, err := s.plannerIntegration.GetWorkflowDetails(workflowID)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"details": details,
	})
}

func (s *APIServer) handlePauseWorkflow(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	workflowID := vars["id"]

	var req WorkflowControlRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	err := s.plannerIntegration.PauseWorkflow(workflowID, req.Reason)
	if err != nil {
		response := WorkflowControlResponse{
			Success: false,
			Error:   err.Error(),
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
		return
	}

	response := WorkflowControlResponse{
		Success: true,
		Message: "Workflow paused successfully",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (s *APIServer) handleResumeWorkflow(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	workflowID := vars["id"]

	var req WorkflowControlRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	err := s.plannerIntegration.ResumeWorkflow(workflowID, req.ResumeToken)
	if err != nil {
		response := WorkflowControlResponse{
			Success: false,
			Error:   err.Error(),
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
		return
	}

	response := WorkflowControlResponse{
		Success: true,
		Message: "Workflow resumed successfully",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (s *APIServer) handleCancelWorkflow(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	workflowID := vars["id"]

	err := s.plannerIntegration.CancelWorkflow(workflowID)
	if err != nil {
		response := WorkflowControlResponse{
			Success: false,
			Error:   err.Error(),
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
		return
	}

	response := WorkflowControlResponse{
		Success: true,
		Message: "Workflow cancelled successfully",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (s *APIServer) handleListActiveWorkflows(w http.ResponseWriter, r *http.Request) {
	workflows := s.plannerIntegration.ListActiveWorkflows()

	// Ensure workflows is never nil
	if workflows == nil {
		workflows = []*planner.WorkflowStatus{}
	}

	response := ActiveWorkflowsResponse{
		Success:   true,
		Workflows: workflows,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (s *APIServer) handleListWorkflowTemplates(w http.ResponseWriter, r *http.Request) {
	templates, err := s.plannerIntegration.ListWorkflowTemplates()
	if err != nil {
		response := WorkflowTemplatesResponse{
			Success: false,
			Error:   err.Error(),
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
		return
	}

	response := WorkflowTemplatesResponse{
		Success:   true,
		Templates: templates,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (s *APIServer) handleRegisterWorkflowTemplate(w http.ResponseWriter, r *http.Request) {
	var template planner.WorkflowTemplate
	if err := json.NewDecoder(r.Body).Decode(&template); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	err := s.plannerIntegration.RegisterWorkflowTemplate(&template)
	if err != nil {
		response := WorkflowControlResponse{
			Success: false,
			Error:   err.Error(),
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
		return
	}

	response := WorkflowControlResponse{
		Success: true,
		Message: "Workflow template registered successfully",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleServeFile serves a file by filename
func (s *APIServer) handleServeFile(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	filename := vars["filename"]

	if filename == "" {
		http.Error(w, "No filename provided", http.StatusBadRequest)
		return
	}

	// Get file from storage
	file, err := s.fileStorage.GetFileByFilename(filename)
	if err != nil {
		http.Error(w, fmt.Sprintf("File not found: %s", filename), http.StatusNotFound)
		return
	}

	// Set appropriate headers
	w.Header().Set("Content-Type", file.ContentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%s", file.Filename))
	w.Header().Set("Content-Length", fmt.Sprintf("%d", file.Size))

	// Serve the file content
	w.Write(file.Content)
}

// handleGetWorkflowFiles returns metadata for all files in a workflow
func (s *APIServer) handleGetWorkflowFiles(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	workflowID := vars["workflow_id"]

	if workflowID == "" {
		http.Error(w, "No workflow ID provided", http.StatusBadRequest)
		return
	}

	// Check if this is a hierarchical workflow that needs mapping
	var targetWorkflowID string
	if strings.HasPrefix(workflowID, "intelligent_") {
		// This is already an intelligent workflow ID
		targetWorkflowID = workflowID
	} else {
		// Try to find the intelligent workflow ID for this hierarchical workflow
		intelligentID, err := s.getWorkflowMapping(workflowID)
		if err != nil {
			log.Printf("⚠️ [API] No mapping found for workflow %s, trying direct lookup", workflowID)
			targetWorkflowID = workflowID
		} else {
			log.Printf("🔗 [API] Mapped hierarchical workflow %s to intelligent workflow %s", workflowID, intelligentID)
			targetWorkflowID = intelligentID
		}
	}

	// Get files for workflow using the target workflow ID
	files, err := s.fileStorage.GetFilesByWorkflow(targetWorkflowID)
	if err != nil {
		log.Printf("❌ [API] Failed to get files for workflow %s (target: %s): %v", workflowID, targetWorkflowID, err)
		http.Error(w, fmt.Sprintf("Failed to get files for workflow: %v", err), http.StatusInternalServerError)
		return
	}

	// If we don't have enough files from file storage, try to get them from the workflow record
	workflowFiles := s.getFilesFromWorkflowRecord(workflowID)
	if len(workflowFiles) > len(files) {
		log.Printf("📁 [API] Found %d files in workflow record vs %d in file storage, using workflow record", len(workflowFiles), len(files))
		files = workflowFiles
	}

	// Deduplicate by filename keeping the latest by CreatedAt
	files = s.dedupeFilesByFilename(files)

	log.Printf("📁 [API] Retrieved %d files for workflow %s (target: %s)", len(files), workflowID, targetWorkflowID)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(files)
}

// dedupeFilesByFilename keeps the most recent file per filename
func (s *APIServer) dedupeFilesByFilename(files []FileMetadata) []FileMetadata {
	latestByName := make(map[string]FileMetadata)
	for _, f := range files {
		if existing, ok := latestByName[f.Filename]; ok {
			if f.CreatedAt.After(existing.CreatedAt) {
				latestByName[f.Filename] = f
			}
		} else {
			latestByName[f.Filename] = f
		}
	}
	// Flatten to slice
	result := make([]FileMetadata, 0, len(latestByName))
	for _, f := range latestByName {
		result = append(result, f)
	}
	return result
}

// getFilesFromWorkflowRecord retrieves files from the workflow record in Redis
func (s *APIServer) getFilesFromWorkflowRecord(workflowID string) []FileMetadata {
	ctx := context.Background()

	// Get workflow record from Redis
	workflowKey := fmt.Sprintf("workflow:%s", workflowID)
	workflowData, err := s.redis.Get(ctx, workflowKey).Result()
	if err != nil {
		log.Printf("⚠️ [API] Failed to get workflow record %s: %v", workflowID, err)
		return []FileMetadata{}
	}

	// Parse workflow data
	var workflowRecord map[string]interface{}
	if err := json.Unmarshal([]byte(workflowData), &workflowRecord); err != nil {
		log.Printf("⚠️ [API] Failed to parse workflow record %s: %v", workflowID, err)
		return []FileMetadata{}
	}

	// Extract files from workflow record
	filesInterface, ok := workflowRecord["files"].([]interface{})
	if !ok {
		log.Printf("⚠️ [API] No files found in workflow record %s", workflowID)
		return []FileMetadata{}
	}

	var files []FileMetadata
	for _, fileInterface := range filesInterface {
		file, ok := fileInterface.(map[string]interface{})
		if !ok {
			continue
		}

		// Convert to FileMetadata
		fileMetadata := FileMetadata{
			ID:          fmt.Sprintf("workflow_file_%d", len(files)+1),
			Filename:    file["filename"].(string),
			ContentType: file["content_type"].(string),
			Size:        int64(file["size"].(float64)),
			WorkflowID:  workflowID,
			StepID:      "workflow_record",
			CreatedAt:   time.Now(),
			ExpiresAt:   time.Now().Add(24 * time.Hour),
		}

		files = append(files, fileMetadata)
	}

	log.Printf("📁 [API] Retrieved %d files from workflow record %s", len(files), workflowID)
	return files
}

// handleServeWorkflowFile serves a file from a specific workflow
func (s *APIServer) handleServeWorkflowFile(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	workflowID := vars["workflow_id"]
	filename := vars["filename"]

	if workflowID == "" || filename == "" {
		http.Error(w, "Workflow ID and filename are required", http.StatusBadRequest)
		return
	}

	// Get workflow from Redis
	workflowKey := fmt.Sprintf("workflow:%s", workflowID)
	workflowData, err := s.redis.Get(context.Background(), workflowKey).Result()
	if err != nil {
		http.Error(w, fmt.Sprintf("Workflow not found: %s", workflowID), http.StatusNotFound)
		return
	}

	// Parse workflow data
	var workflow map[string]interface{}
	if err := json.Unmarshal([]byte(workflowData), &workflow); err != nil {
		http.Error(w, "Failed to parse workflow data", http.StatusInternalServerError)
		return
	}

	// Find the file in the workflow's files array
	files, ok := workflow["files"].([]interface{})
	if !ok {
		http.Error(w, "No files found in workflow", http.StatusNotFound)
		return
	}

	var targetFile map[string]interface{}
	for _, fileInterface := range files {
		file, ok := fileInterface.(map[string]interface{})
		if !ok {
			continue
		}
		if file["filename"] == filename {
			targetFile = file
			break
		}
	}

	if targetFile == nil {
		http.Error(w, fmt.Sprintf("File not found: %s", filename), http.StatusNotFound)
		return
	}

	// Get file content and metadata
	content, ok := targetFile["content"].(string)
	if !ok {
		http.Error(w, "File content not available", http.StatusInternalServerError)
		return
	}

	contentType, _ := targetFile["content_type"].(string)
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	size, _ := targetFile["size"].(float64)

	// Set appropriate headers
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%s", filename))
	w.Header().Set("Content-Length", fmt.Sprintf("%.0f", size))

	// Serve the file content
	w.Write([]byte(content))
}

// handleSearchEpisodes proxies search to the RAG adapter (requires episodicClient)
func (s *APIServer) handleSearchEpisodes(w http.ResponseWriter, r *http.Request) {
	if s.vectorDB == nil {
		http.Error(w, "episodic memory not configured", http.StatusServiceUnavailable)
		return
	}
	q := r.URL.Query().Get("q")
	if strings.TrimSpace(q) == "" {
		http.Error(w, "missing q", http.StatusBadRequest)
		return
	}
	limit := 20
	if ls := r.URL.Query().Get("limit"); ls != "" {
		if v, err := strconv.Atoi(ls); err == nil && v > 0 {
			limit = v
		}
	}
	// Optional filters: session_id, tag
	filters := map[string]any{}
	if sid := r.URL.Query().Get("session_id"); sid != "" {
		filters["session_id"] = sid
	}
	if tag := r.URL.Query().Get("tag"); tag != "" {
		filters["tags"] = tag
	}

	// Use Weaviate for episodic search
	vec := toyEmbed(q, 8)
	results, err := s.vectorDB.SearchEpisodes(vec, limit, filters)

	if err != nil {
		http.Error(w, fmt.Sprintf("search failed: %v", err), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results)
}

// handleMemorySummary aggregates beliefs, goals, working memory (optional), and recent episodes
func (s *APIServer) handleMemorySummary(w http.ResponseWriter, r *http.Request) {
	summary := map[string]any{}

	// Self-model beliefs and goals
	if s.selfModelManager != nil {
		if sm, err := s.selfModelManager.Load(); err == nil {
			summary["beliefs"] = sm.Beliefs
			summary["goals"] = sm.Goals
		} else {
			summary["beliefs_error"] = err.Error()
		}
	}

	// Optional: working memory for a session
	if sessionID := r.URL.Query().Get("session_id"); sessionID != "" {
		if wm, err := s.workingMemory.GetWorkingMemory(sessionID, 50); err == nil {
			summary["working_memory"] = wm
		} else {
			summary["working_memory_error"] = err.Error()
		}
	}

	// Optional: recent episodes
	if s.vectorDB != nil {
		// simple query vector from keyword; reuse toyEmbed for now
		qvec := toyEmbed("recent", 8)
		if eps, err := s.vectorDB.SearchEpisodes(qvec, 10, map[string]any{}); err == nil {
			summary["recent_episodes"] = eps
		}
	}

	// Indicate whether episodic adapter is configured so UIs can display guidance
	summary["episodic_enabled"] = (s.vectorDB != nil)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(summary)
}

// handleUpdateGoalStatus updates the status of a goal in the self-model
func (s *APIServer) handleUpdateGoalStatus(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	goalID := vars["id"]

	var req struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Update goal status in self-model
	if s.selfModelManager != nil {
		if err := s.selfModelManager.UpdateGoalStatus(goalID, req.Status); err != nil {
			http.Error(w, fmt.Sprintf("Failed to update goal status: %v", err), http.StatusInternalServerError)
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Goal status updated",
		"goal_id": goalID,
		"status":  req.Status,
	})
}

// handleDeleteSelfModelGoal deletes a goal from the self-model
func (s *APIServer) handleDeleteSelfModelGoal(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	goalID := vars["id"]
	if s.selfModelManager == nil {
		http.Error(w, "self model not configured", http.StatusBadRequest)
		return
	}
	if err := s.selfModelManager.DeleteGoal(goalID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"success": true, "goal_id": goalID})
}

// handleCleanupSelfModelGoals deletes self-model goals matching internal patterns
// Request JSON: { "patterns": ["^Execute task: Goal Execution$", "^Execute task: artifact_task$", "^Execute task: code_.*" ] }
func (s *APIServer) handleCleanupSelfModelGoals(w http.ResponseWriter, r *http.Request) {
	type reqBody struct {
		Patterns []string `json:"patterns"`
		Statuses []string `json:"statuses"`
	}
	var req reqBody
	_ = json.NewDecoder(r.Body).Decode(&req)

	if s.selfModelManager == nil {
		http.Error(w, "self model not configured", http.StatusBadRequest)
		return
	}

	sm, err := s.selfModelManager.Load()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Compile regexes
	var regs []*regexp.Regexp
	for _, p := range req.Patterns {
		if p == "" {
			continue
		}
		if re, err := regexp.Compile(p); err == nil {
			regs = append(regs, re)
		}
	}
	// Defaults when none provided: internal names
	if len(regs) == 0 {
		regs = []*regexp.Regexp{
			regexp.MustCompile(`^Execute task: Goal Execution$`),
			regexp.MustCompile(`^Execute task: artifact_task$`),
			regexp.MustCompile(`^Execute task: code_.*`),
		}
	}

	// Optional status filter
	statusFilter := map[string]bool{}
	for _, s := range req.Statuses {
		statusFilter[strings.ToLower(strings.TrimSpace(s))] = true
	}

	// Select goal IDs to delete
	toDelete := []string{}
	for _, g := range sm.Goals {
		name := strings.TrimSpace(g.Name)
		// If statuses provided, include only matching
		if len(statusFilter) > 0 {
			if !statusFilter[strings.ToLower(strings.TrimSpace(g.Status))] {
				continue
			}
		}
		for _, re := range regs {
			if re.MatchString(name) {
				toDelete = append(toDelete, g.ID)
				break
			}
		}
	}

	// Delete via manager to keep persistence consistent
	for _, id := range toDelete {
		_ = s.selfModelManager.DeleteGoal(id)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"success":       true,
		"deleted_count": len(toDelete),
		"deleted_ids":   toDelete,
	})
}

// recordMonitorMetrics records metrics in the format expected by the monitor UI
func (s *APIServer) recordMonitorMetrics(success bool, execTime time.Duration) {
	ctx := context.Background()

	// Increment total executions
	totalExec, _ := s.redis.Get(ctx, "metrics:total_executions").Int()
	s.redis.Set(ctx, "metrics:total_executions", totalExec+1, 0)

	// Increment successful executions if successful
	if success {
		successExec, _ := s.redis.Get(ctx, "metrics:successful_executions").Int()
		s.redis.Set(ctx, "metrics:successful_executions", successExec+1, 0)
	}

	// Update average execution time (simple moving average)
	avgTime, _ := s.redis.Get(ctx, "metrics:avg_execution_time").Float64()
	newAvg := (avgTime*float64(totalExec) + execTime.Seconds()*1000) / float64(totalExec+1)
	s.redis.Set(ctx, "metrics:avg_execution_time", newAvg, 0)

	// Update last execution time
	s.redis.Set(ctx, "metrics:last_execution", time.Now().Format(time.RFC3339), 0)

	log.Printf("📈 [API] Updated monitor metrics: Total=%d, Success=%v, AvgTime=%.2fms",
		totalExec+1, success, newAvg)
}

// createSimplePDF generates a very simple PDF from a title, subtitle and optional JSON payload
func (s *APIServer) createSimplePDF(title, subtitle string, payload interface{}) []byte {
	content := fmt.Sprintf("%v", payload)
	// Minimal PDF content (not a full renderer, but adequate placeholder)
	body := fmt.Sprintf(`%%PDF-1.4
1 0 obj<<>>endobj
2 0 obj<<>>endobj
3 0 obj<< /Length 200 >>stream
BT
/F1 18 Tf 50 750 Td (%s) Tj
/F1 12 Tf 50 730 Td (%s) Tj
/F1 10 Tf 50 700 Td (Data:) Tj
/F1 8 Tf 50 680 Td (%s) Tj
ET
endstream endobj
4 0 obj<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>endobj
5 0 obj<< /Type /Page /Parent 6 0 R /MediaBox [0 0 612 792] /Contents 3 0 R /Resources << /Font << /F1 4 0 R >> >> >>endobj
6 0 obj<< /Type /Pages /Kids [5 0 R] /Count 1 >>endobj
7 0 obj<< /Type /Catalog /Pages 6 0 R >>endobj
xref
0 8
0000000000 65535 f 
0000000010 00000 n 
0000000050 00000 n 
0000000090 00000 n 
0000000350 00000 n 
0000000450 00000 n 
0000000600 00000 n 
0000000700 00000 n 
trailer<< /Size 8 /Root 7 0 R >>
startxref
800
%%EOF`, escapePDFText(title), escapePDFText(subtitle), escapePDFText(content))
	return []byte(body)
}

func escapePDFText(s string) string {
	s = strings.ReplaceAll(s, "(", "\\(")
	s = strings.ReplaceAll(s, ")", "\\)")
	s = strings.ReplaceAll(s, "\\", "\\\\")
	return s
}

// --------- Interpreter Handlers ---------

func (s *APIServer) handleInterpret(w http.ResponseWriter, r *http.Request) {
	s.interpreterAPI.HandleInterpretRequest(w, r)
}

func (s *APIServer) handleInterpretAndExecute(w http.ResponseWriter, r *http.Request) {
	// Acquire execution slot (UI gets priority)
	release, acquired := s.acquireExecutionSlot(r)
	if !acquired {
		http.Error(w, "Server busy - too many concurrent executions. Please try again later.", http.StatusTooManyRequests)
		return
	}
	defer release()

	// First interpret the natural language input
	var req interpreter.NaturalLanguageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Set defaults
	if req.Context == nil {
		req.Context = make(map[string]string)
	}
	if req.SessionID == "" {
		req.SessionID = fmt.Sprintf("session_%d", time.Now().UnixNano())
	}

	// Check if this is an informational query about capabilities/tools/knowledge
	if s.isInformationalQuery(req.Input) {
		log.Printf("ℹ️ [API] Detected informational query, providing capability information")
		infoResponse := s.handleInformationalQuery(r.Context(), req.Input)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(infoResponse)
		return
	}

	// Emit input event on the bus (best-effort)
	if s.eventBus != nil {
		_ = s.eventBus.Publish(r.Context(), eventbus.CanonicalEvent{
			EventID:   eventbus.NewEventID("evt_", time.Now()),
			Source:    "api:interpret_execute",
			Type:      "user_message",
			Timestamp: time.Now().UTC(),
			Context:   eventbus.EventContext{Channel: "api", SessionID: req.SessionID},
			Payload:   eventbus.EventPayload{Text: req.Input, Metadata: map[string]interface{}{"path": "/api/v1/interpret/execute"}},
			Security:  eventbus.EventSecurity{Sensitivity: "low"},
		})
	}

	// Use flexible interpreter directly to preserve tool calls
	ctx := r.Context()

	// Get the flexible interpreter from the regular interpreter
	flexibleInterpreter := s.interpreter.GetFlexibleInterpreter()
	if flexibleInterpreter == nil {
		log.Printf("❌ [API] Flexible interpreter not available")
		http.Error(w, "Flexible interpreter not available", http.StatusInternalServerError)
		return
	}

	// Use flexible interpreter directly
	flexibleResult, err := flexibleInterpreter.InterpretAndExecute(ctx, &req)
	if err != nil {
		log.Printf("❌ [API] Flexible interpretation failed: %v", err)
		http.Error(w, fmt.Sprintf("Interpretation failed: %v", err), http.StatusInternalServerError)
		return
	}

	if !flexibleResult.Success {
		http.Error(w, flexibleResult.Message, http.StatusBadRequest)
		return
	}

	// Auto-register tool candidate if present in metadata
	if flexibleResult.Metadata != nil {
		if tc, ok := flexibleResult.Metadata["tool_candidate"].(bool); ok && tc {
			if spec, ok := flexibleResult.Metadata["proposed_tool"].(map[string]interface{}); ok {
				// Minimal validation and registration
				b, _ := json.Marshal(spec)
				// Call our own tool registration handler via in-process method
				var t Tool
				if err := json.Unmarshal(b, &t); err == nil && strings.TrimSpace(t.ID) != "" {
					_ = s.registerTool(ctx, t)
				}
			}
		}
	}

	// Handle different response types
	var executionResults []interpreter.TaskExecutionResult

	if flexibleResult.ToolCall != nil {
		// Tool call was executed directly by flexible interpreter
		log.Printf("🔧 [API] Tool call executed: %s", flexibleResult.ToolCall.ToolID)

		// Convert to task execution result format
		taskResult := interpreter.TaskExecutionResult{
			Task: interpreter.InterpretedTask{
				TaskName:    "Tool Execution",
				Description: flexibleResult.ToolCall.Description,
			},
			Success: flexibleResult.ToolExecutionResult.Success,
			Result:  fmt.Sprintf("%v", flexibleResult.ToolExecutionResult.Result),
			Error:   flexibleResult.ToolExecutionResult.Error,
		}
		executionResults = append(executionResults, taskResult)
	} else if flexibleResult.StructuredTask != nil {
		// Fall back to intelligent execution for structured tasks
		log.Printf("🚀 [API] Executing structured task: %s", flexibleResult.StructuredTask.TaskName)

		// Convert to intelligent execution request
		intelligentReq := IntelligentExecutionRequest{
			TaskName:        flexibleResult.StructuredTask.TaskName,
			Description:     flexibleResult.StructuredTask.Description,
			Context:         flexibleResult.StructuredTask.Context,
			Language:        flexibleResult.StructuredTask.Language,
			ForceRegenerate: flexibleResult.StructuredTask.ForceRegenerate,
			MaxRetries:      flexibleResult.StructuredTask.MaxRetries,
			Timeout:         flexibleResult.StructuredTask.Timeout,
		}

		// Execute using the intelligent executor
		executor := NewIntelligentExecutor(
			s.domainManager,
			s.codeStorage,
			s.codeGenerator,
			s.dockerExecutor,
			s.llmClient,
			s.actionManager,
			s.plannerIntegration,
			s.selfModelManager,
			s.toolMetrics,
			s.fileStorage,
			s.hdnBaseURL,
			s.redisAddr,
		)

		result, err := executor.ExecuteTaskIntelligently(ctx, &ExecutionRequest{
			TaskName:        intelligentReq.TaskName,
			Description:     intelligentReq.Description,
			Context:         intelligentReq.Context,
			Language:        intelligentReq.Language,
			ForceRegenerate: intelligentReq.ForceRegenerate,
			MaxRetries:      intelligentReq.MaxRetries,
			Timeout:         intelligentReq.Timeout,
		})

		// Record the execution result
		executionResult := interpreter.TaskExecutionResult{
			Task: interpreter.InterpretedTask{
				TaskName:    intelligentReq.TaskName,
				Description: intelligentReq.Description,
			},
			Success: err == nil && result.Success,
			Result: func() string {
				if result.Result != nil {
					if str, ok := result.Result.(string); ok {
						return str
					}
					return fmt.Sprintf("%v", result.Result)
				}
				return ""
			}(),
			Error: func() string {
				if err != nil {
					return err.Error()
				}
				if !result.Success {
					return result.Error
				}
				return ""
			}(),
			ExecutedAt: time.Now(),
		}

		executionResults = append(executionResults, executionResult)

		// Record metrics
		s.recordMonitorMetrics(executionResult.Success, result.ExecutionTime)

		// Append to episodic memory (best-effort)
		if s.episodicClient != nil {
			ep := &mempkg.EpisodicRecord{
				SessionID: req.SessionID,
				PlanID:    "",
				Timestamp: time.Now().UTC(),
				Outcome: func() string {
					if executionResult.Success {
						return "success"
					}
					return "failure"
				}(),
				Reward:    0,
				Tags:      []string{"interpret"},
				StepIndex: 0,
				Text:      fmt.Sprintf("%s: %s", intelligentReq.TaskName, intelligentReq.Description),
				Metadata:  map[string]any{"workflow_id": result.WorkflowID},
			}
			_ = s.episodicClient.IndexEpisode(ep)
		}

		log.Printf("✅ [API] Task %s completed: success=%v", intelligentReq.TaskName, executionResult.Success)
	}

	// Create response
	response := interpreter.InterpretAndExecuteResponse{
		Success: true,
		Interpretation: &interpreter.InterpretationResult{
			Success:       flexibleResult.Success,
			Tasks:         []interpreter.InterpretedTask{},
			Message:       flexibleResult.Message,
			SessionID:     flexibleResult.SessionID,
			InterpretedAt: flexibleResult.InterpretedAt,
			Metadata:      flexibleResult.Metadata,
		},
		ExecutionPlan: executionResults,
		Message:       fmt.Sprintf("Successfully interpreted and executed %d task(s)", len(executionResults)),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// isInformationalQuery checks if the input is asking about capabilities, tools, or knowledge
func (s *APIServer) isInformationalQuery(input string) bool {
	inputLower := strings.ToLower(strings.TrimSpace(input))
	
	// Keywords that suggest informational queries
	infoKeywords := []string{
		"what do you know",
		"what can you do",
		"what are your capabilities",
		"what tools do you have",
		"list your capabilities",
		"show me what you can do",
		"what do you know how to do",
		"what are you capable of",
		"tell me about your capabilities",
		"what tools are available",
		"list available tools",
		"what can you help with",
	}
	
	for _, keyword := range infoKeywords {
		if strings.Contains(inputLower, keyword) {
			return true
		}
	}
	
	return false
}

// handleInformationalQuery provides formatted information about system capabilities
func (s *APIServer) handleInformationalQuery(ctx context.Context, query string) interpreter.InterpretAndExecuteResponse {
	var responseText strings.Builder
	
	// Get capabilities
	executor := NewIntelligentExecutor(
		s.domainManager,
		s.codeStorage,
		s.codeGenerator,
		s.dockerExecutor,
		s.llmClient,
		s.actionManager,
		s.plannerIntegration,
		s.selfModelManager,
		s.toolMetrics,
		s.fileStorage,
		s.hdnBaseURL,
		s.redisAddr,
	)
	
	capabilities, err := executor.ListCachedCapabilities()
	stats := executor.GetExecutionStats()
	
	responseText.WriteString("Here's what I know how to do:\n\n")
	
	// Add capabilities summary
	if err == nil && len(capabilities) > 0 {
		totalCapabilities := len(capabilities)
		if totalCap, ok := stats["total_cached_capabilities"].(int); ok && totalCap > 0 {
			totalCapabilities = totalCap
		}
		responseText.WriteString(fmt.Sprintf("📚 **Capabilities**: I have learned %d capabilities that I can execute.\n\n", totalCapabilities))
		
		// Show sample capabilities (up to 10)
		sampleCount := 10
		if len(capabilities) < sampleCount {
			sampleCount = len(capabilities)
		}
		responseText.WriteString("**Sample capabilities:**\n")
		shown := 0
		for i := 0; i < len(capabilities) && shown < sampleCount; i++ {
			cap := capabilities[i]
			desc := cap.Description
			if desc == "" {
				desc = cap.TaskName
			}
			// Skip generic/unhelpful descriptions
			descTrimmed := strings.TrimSpace(desc)
			if strings.HasPrefix(descTrimmed, "Execute capability:") || 
			   strings.HasPrefix(descTrimmed, "🚨 CRITICAL") ||
			   strings.HasPrefix(descTrimmed, "{") || // Skip JSON strings
			   strings.HasPrefix(descTrimmed, "\"interpreted_at\"") ||
			   len(descTrimmed) < 15 {
				continue
			}
			// Clean up description - remove markdown formatting if present
			desc = strings.TrimSpace(desc)
			desc = strings.TrimPrefix(desc, "🚨")
			desc = strings.TrimSpace(desc)
			// Remove any leading JSON-like patterns
			if strings.HasPrefix(desc, "{") {
				continue
			}
			if len(desc) > 80 {
				desc = desc[:80] + "..."
			}
			responseText.WriteString(fmt.Sprintf("  • %s (%s)\n", desc, cap.Language))
			shown++
		}
		if len(capabilities) > sampleCount {
			responseText.WriteString(fmt.Sprintf("  ... and %d more\n", len(capabilities)-sampleCount))
		}
		responseText.WriteString("\n")
	} else {
		responseText.WriteString("📚 **Capabilities**: No cached capabilities found.\n\n")
	}
	
	// Get tools
	tools, err := s.listTools(ctx)
	if err == nil && len(tools) > 0 {
		responseText.WriteString(fmt.Sprintf("🔧 **Tools**: I have access to %d tools:\n", len(tools)))
		for _, tool := range tools {
			responseText.WriteString(fmt.Sprintf("  • %s: %s\n", tool.ID, tool.Name))
		}
		responseText.WriteString("\n")
	} else {
		responseText.WriteString("🔧 **Tools**: No tools available.\n\n")
	}
	
	responseText.WriteString("You can ask me to execute tasks, and I'll generate code to accomplish them. I can also use the available tools to help with various operations.\n")
	
	return interpreter.InterpretAndExecuteResponse{
		Success: true,
		Interpretation: &interpreter.InterpretationResult{
			Success:       true,
			Tasks:         []interpreter.InterpretedTask{},
			Message:       responseText.String(),
			SessionID:     fmt.Sprintf("session_%d", time.Now().UnixNano()),
			InterpretedAt: time.Now(),
		},
		ExecutionPlan: []interpreter.TaskExecutionResult{},
		Message:       "Informational query answered",
	}
}

// --------- Domain Knowledge Handlers ---------

func (s *APIServer) handleListConcepts(w http.ResponseWriter, r *http.Request) {
	if s.domainKnowledge == nil {
		http.Error(w, "Domain knowledge not available", http.StatusServiceUnavailable)
		return
	}

	domain := r.URL.Query().Get("domain")
	namePattern := r.URL.Query().Get("name")
	limitStr := r.URL.Query().Get("limit")
	limit := 50
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	concepts, err := s.domainKnowledge.SearchConcepts(r.Context(), domain, namePattern, limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"concepts": concepts,
		"count":    len(concepts),
	})
}

func (s *APIServer) handleCreateConcept(w http.ResponseWriter, r *http.Request) {
	if s.domainKnowledge == nil {
		http.Error(w, "Domain knowledge not available", http.StatusServiceUnavailable)
		return
	}

	var concept mempkg.Concept
	if err := json.NewDecoder(r.Body).Decode(&concept); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if concept.Name == "" || concept.Domain == "" {
		http.Error(w, "Name and domain are required", http.StatusBadRequest)
		return
	}

	if err := s.domainKnowledge.SaveConcept(r.Context(), &concept); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "created"})
}

func (s *APIServer) handleGetConcept(w http.ResponseWriter, r *http.Request) {
	if s.domainKnowledge == nil {
		http.Error(w, "Domain knowledge not available", http.StatusServiceUnavailable)
		return
	}

	vars := mux.Vars(r)
	conceptName := vars["name"]

	concept, err := s.domainKnowledge.GetConcept(r.Context(), conceptName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(concept)
}

func (s *APIServer) handleUpdateConcept(w http.ResponseWriter, r *http.Request) {
	if s.domainKnowledge == nil {
		http.Error(w, "Domain knowledge not available", http.StatusServiceUnavailable)
		return
	}

	vars := mux.Vars(r)
	conceptName := vars["name"]

	var concept mempkg.Concept
	if err := json.NewDecoder(r.Body).Decode(&concept); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	concept.Name = conceptName // Ensure name matches URL
	if err := s.domainKnowledge.SaveConcept(r.Context(), &concept); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "updated"})
}

func (s *APIServer) handleDeleteConcept(w http.ResponseWriter, r *http.Request) {
	// For now, we don't implement deletion as it's complex with relationships
	http.Error(w, "Concept deletion not implemented", http.StatusNotImplemented)
}

func (s *APIServer) handleAddConceptProperty(w http.ResponseWriter, r *http.Request) {
	if s.domainKnowledge == nil {
		http.Error(w, "Domain knowledge not available", http.StatusServiceUnavailable)
		return
	}

	vars := mux.Vars(r)
	conceptName := vars["name"]

	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Type        string `json:"type"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if err := s.domainKnowledge.AddProperty(r.Context(), conceptName, req.Name, req.Description, req.Type); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "added"})
}

func (s *APIServer) handleAddConceptConstraint(w http.ResponseWriter, r *http.Request) {
	if s.domainKnowledge == nil {
		http.Error(w, "Domain knowledge not available", http.StatusServiceUnavailable)
		return
	}

	vars := mux.Vars(r)
	conceptName := vars["name"]

	var req struct {
		Description string `json:"description"`
		Type        string `json:"type"`
		Severity    string `json:"severity"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if err := s.domainKnowledge.AddConstraint(r.Context(), conceptName, req.Description, req.Type, req.Severity); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "added"})
}

func (s *APIServer) handleAddConceptExample(w http.ResponseWriter, r *http.Request) {
	if s.domainKnowledge == nil {
		http.Error(w, "Domain knowledge not available", http.StatusServiceUnavailable)
		return
	}

	vars := mux.Vars(r)
	conceptName := vars["name"]

	var example mempkg.Example
	if err := json.NewDecoder(r.Body).Decode(&example); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if err := s.domainKnowledge.AddExample(r.Context(), conceptName, &example); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "added"})
}

func (s *APIServer) handleRelateConcepts(w http.ResponseWriter, r *http.Request) {
	if s.domainKnowledge == nil {
		http.Error(w, "Domain knowledge not available", http.StatusServiceUnavailable)
		return
	}

	vars := mux.Vars(r)
	srcConcept := vars["name"]

	var req struct {
		RelationType  string                 `json:"relation_type"`
		TargetConcept string                 `json:"target_concept"`
		Properties    map[string]interface{} `json:"properties,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if err := s.domainKnowledge.RelateConcepts(r.Context(), srcConcept, req.RelationType, req.TargetConcept, req.Properties); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "related"})
}

func (s *APIServer) handleGetRelatedConcepts(w http.ResponseWriter, r *http.Request) {
	if s.domainKnowledge == nil {
		http.Error(w, "Domain knowledge not available", http.StatusServiceUnavailable)
		return
	}

	vars := mux.Vars(r)
	conceptName := vars["name"]

	relationTypes := r.URL.Query()["relation_type"]
	concepts, err := s.domainKnowledge.GetRelatedConcepts(r.Context(), conceptName, relationTypes)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"concepts": concepts,
		"count":    len(concepts),
	})
}

func (s *APIServer) handleSearchConcepts(w http.ResponseWriter, r *http.Request) {
	if s.domainKnowledge == nil {
		http.Error(w, "Domain knowledge not available", http.StatusServiceUnavailable)
		return
	}

	domain := r.URL.Query().Get("domain")
	namePattern := r.URL.Query().Get("name")
	limitStr := r.URL.Query().Get("limit")
	limit := 50
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	concepts, err := s.domainKnowledge.SearchConcepts(r.Context(), domain, namePattern, limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"concepts": concepts,
		"count":    len(concepts),
	})
}

// handleKnowledgeQuery executes a raw Cypher query against the domain knowledge store
func (s *APIServer) handleKnowledgeQuery(w http.ResponseWriter, r *http.Request) {
	// Accept: { "query": "MATCH ..." }
	var req struct {
		Query string `json:"query"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	log.Printf("[HDN] /knowledge/query len=%d", len(req.Query))
	if strings.TrimSpace(req.Query) == "" {
		http.Error(w, "missing query", http.StatusBadRequest)
		return
	}

	// Use environment for Neo4j creds (same defaults as elsewhere)
	uri := os.Getenv("NEO4J_URI")
	if strings.TrimSpace(uri) == "" {
		uri = "bolt://localhost:7687"
	}
	user := os.Getenv("NEO4J_USER")
	if strings.TrimSpace(user) == "" {
		user = "neo4j"
	}
	pass := os.Getenv("NEO4J_PASS")
	if strings.TrimSpace(pass) == "" {
		pass = "test1234"
	}

	// Execute (works when built with -tags neo4j)
	rows, err := mempkg.ExecuteCypher(r.Context(), uri, user, pass, req.Query)
	if err != nil {
		log.Printf("[HDN] Cypher error: %v", err)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	log.Printf("[HDN] /knowledge/query returned %d rows", len(rows))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"results": rows,
		"count":   len(rows),
	})
}

// --------- Server Start ---------

// generateDailySummaryFromSystemData creates a summary from actual system events and memory
func (s *APIServer) generateDailySummaryFromSystemData(ctx context.Context) string {
	var summary strings.Builder

	// Get today's date for filtering
	today := time.Now().UTC().Format("2006-01-02")
	yesterday := time.Now().UTC().Add(-24 * time.Hour).Format("2006-01-02")

	summary.WriteString("Paragraph:\n")
	summary.WriteString("Today's system activity summary based on actual events and memory data.\n\n")

	// Discoveries section
	summary.WriteString("Discoveries:\n")

	// Check recent working memory events
	if s.workingMemory != nil {
		// Get events from all sessions (we'll use a default session for now)
		mem, err := s.workingMemory.GetWorkingMemory("system", 20)
		if err == nil && len(mem.RecentEvents) > 0 {
			summary.WriteString(fmt.Sprintf("- %d recent events recorded in working memory\n", len(mem.RecentEvents)))
		}
	}

	// Check Redis for system metrics
	if s.redis != nil {
		// Count intelligent executions today
		keys, err := s.redis.Keys(ctx, "intelligent:*").Result()
		if err == nil {
			todayCount := 0
			for _, key := range keys {
				// Check if key was created today (simplified check)
				if strings.Contains(key, today) || strings.Contains(key, yesterday) {
					todayCount++
				}
			}
			if todayCount > 0 {
				summary.WriteString(fmt.Sprintf("- %d intelligent executions performed\n", todayCount))
			}
		}

		// Check for any error patterns
		errorKeys, err := s.redis.Keys(ctx, "*error*").Result()
		if err == nil && len(errorKeys) > 0 {
			summary.WriteString(fmt.Sprintf("- %d error-related entries in system logs\n", len(errorKeys)))
		}
	}

	// Check episodic memory if available
	if s.vectorDB != nil {
		summary.WriteString("- Episodic memory system active and indexing events in Weaviate\n")
	}

	// Actions section
	summary.WriteString("\nActions:\n")

	// List recent API calls from logs (simplified)
	summary.WriteString("- Processed intelligent execution requests\n")
	summary.WriteString("- Updated working memory with recent events\n")
	summary.WriteString("- Indexed episodic memories in vector database\n")

	// Questions section
	summary.WriteString("\nQuestions:\n")
	summary.WriteString("1) What patterns can be identified in today's execution logs?\n")
	summary.WriteString("2) How can we improve the system's learning from these events?\n")
	summary.WriteString("3) What new capabilities should be prioritized based on usage?\n")

	return summary.String()
}

// handleGetAllToolMetrics: GET /api/v1/tools/metrics
func (s *APIServer) handleGetAllToolMetrics(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if s.toolMetrics == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "tool metrics not available"})
		return
	}

	metrics, err := s.toolMetrics.GetAllToolMetrics(ctx)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": err.Error()})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"metrics": metrics,
		"count":   len(metrics),
	})
}

// handleGetToolMetrics: GET /api/v1/tools/{id}/metrics
func (s *APIServer) handleGetToolMetrics(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if s.toolMetrics == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "tool metrics not available"})
		return
	}

	// Extract tool ID from URL path
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 4 {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "missing tool id"})
		return
	}
	toolID := parts[3]

	metrics, err := s.toolMetrics.GetToolMetrics(ctx, toolID)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": err.Error()})
		return
	}

	_ = json.NewEncoder(w).Encode(metrics)
}

// handleGetRecentToolCalls: GET /api/v1/tools/calls/recent
func (s *APIServer) handleGetRecentToolCalls(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if s.toolMetrics == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "tool metrics not available"})
		return
	}

	// Get limit from query parameter (default 50)
	limitStr := r.URL.Query().Get("limit")
	limit := int64(50)
	if limitStr != "" {
		if parsedLimit, err := strconv.ParseInt(limitStr, 10, 64); err == nil && parsedLimit > 0 {
			limit = parsedLimit
		}
	}

	calls, err := s.toolMetrics.GetRecentCalls(ctx, limit)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": err.Error()})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"calls": calls,
		"count": len(calls),
	})
}

func (s *APIServer) Start(port int) error {
	addr := fmt.Sprintf(":%d", port)
	// Ensure hdnBaseURL matches the actual server port, unless explicitly overridden
	if base := strings.TrimSpace(os.Getenv("HDN_BASE_URL")); base != "" {
		s.hdnBaseURL = base
	} else {
		s.hdnBaseURL = fmt.Sprintf("http://localhost:%d", port)
	}
	log.Printf("🌐 [HDN] Starting HTTP server on %s (HDN_BASE_URL=%s)", addr, s.hdnBaseURL)
	log.Printf("🌐 [HDN] Server is now listening for connections...")
	err := http.ListenAndServe(addr, s.router)
	if err != nil {
		log.Printf("❌ [HDN] HTTP server error: %v", err)
	}
	return err
}

// getMaxConcurrentExecutions returns the maximum number of concurrent executions
// based on environment variable, with a sensible default
func getMaxConcurrentExecutions() int {
	if maxStr := os.Getenv("HDN_MAX_CONCURRENT_EXECUTIONS"); maxStr != "" {
		if max, err := strconv.Atoi(maxStr); err == nil && max > 0 {
			return max
		}
	}
	// Default to 3 for conservative GPU usage (prevents overload)
	return 3
}

// isUIRequest checks if the request is from the UI based on headers or context
func isUIRequest(r *http.Request) bool {
	// Check for UI-specific header
	if r.Header.Get("X-Request-Source") == "ui" {
		return true
	}
	// Check for UI context in query params (for backward compatibility)
	if r.URL.Query().Get("context") == "ui" {
		return true
	}
	return false
}

// acquireExecutionSlot attempts to acquire an execution slot, preferring UI slot for UI requests
func (s *APIServer) acquireExecutionSlot(r *http.Request) (func(), bool) {
	isUI := isUIRequest(r)

	if isUI {
		// Try UI slot first
		select {
		case s.uiExecutionSemaphore <- struct{}{}:
			return func() { <-s.uiExecutionSemaphore }, true
		default:
			// UI slot busy, try general slot as fallback
			select {
			case s.executionSemaphore <- struct{}{}:
				return func() { <-s.executionSemaphore }, true
			default:
				return nil, false
			}
		}
	} else {
		// Non-UI request, use general slot only
		select {
		case s.executionSemaphore <- struct{}{}:
			return func() { <-s.executionSemaphore }, true
		default:
			return nil, false
		}
	}
}
