package main

import (
	"context"
	"encoding/json"
	"fmt"
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
	"hdn/utils"

	"github.com/gorilla/mux"
	"github.com/redis/go-redis/v9"
)

var (
	listCleanupRegex  = regexp.MustCompile(`^(\d+\.|\*|-|•)\s*`)
	numberedListRegex = regexp.MustCompile(`\s*\d+\.\s+`)
	commentRegex      = regexp.MustCompile(`(?m)(^|\s)//.*$`)
	fenceRegex        = regexp.MustCompile("(?s)^```[a-zA-Z0-9_-]*\n(.*?)\n```\\s*$")
)

// toyEmbed creates a simple deterministic vector for text (for testing)
func toyEmbed(text string, dim int) []float32 {
	vec := make([]float32, dim)
	hash := 0
	for _, c := range text {
		hash = hash*31 + int(c)
	}
	for i := 0; i < dim; i++ {
		vec[i] = float32((hash>>i)&1) * 0.5
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

	t = commentRegex.ReplaceAllString(t, "")

	if m := fenceRegex.FindStringSubmatch(t); len(m) > 1 {
		t = m[1]
	}

	t = strings.Trim(t, "`")
	t = strings.TrimSpace(t)

	lines := strings.Split(t, "\n")
	if len(lines) > 0 {
		first := strings.TrimSpace(lines[0])
		switch strings.ToLower(first) {
		case "go", "golang", "python", "py", "javascript", "js", "typescript", "ts", "java", "bash", "sh", "json":
			t = strings.Join(lines[1:], "\n")
		}
	}

	t = strings.TrimSpace(t)

	log.Printf("🛠️ [SANITIZE] Sanitized code length: %d", len(t))
	if len(t) > 0 {
		preview := t
		if len(preview) > 500 {
			preview = preview[:500] + "..."
		}
		log.Printf("🛠️ [SANITIZE] Preview: %s", preview)
	}

	return t
}

// formatToolResult recursively flattens and formats complex tool results into a human-readable string
func formatToolResult(result interface{}) string {
	return formatToolResultInternal(result, 0)
}

func formatToolResultInternal(result interface{}, depth int) string {
	if result == nil || depth > 5 {
		return ""
	}

	if m, ok := result.(map[string]interface{}); ok {

		if content, exists := m["content"]; exists {
			if items, ok := content.([]interface{}); ok {
				var sb strings.Builder
				for _, item := range items {
					if imap, ok := item.(map[string]interface{}); ok {
						if text, ok := imap["text"].(string); ok {
							if strings.HasPrefix(text, "DATA_JSON: ") {
								if sb.Len() > 0 && !strings.HasSuffix(sb.String(), "\n") {
									sb.WriteString("\n")
								}
								sb.WriteString("--- BEGIN STRUCTURED DATA ---\n")
								sb.WriteString(strings.TrimPrefix(text, "DATA_JSON: "))
								sb.WriteString("\n--- END STRUCTURED DATA ---\n")
							} else {
								if sb.Len() > 0 && !strings.HasSuffix(sb.String(), "\n") {
									sb.WriteString("\n")
								}
								sb.WriteString(text)
							}
						}
					}
				}
				if sb.Len() > 0 {
					return sb.String()
				}
			}
		}

		if inner, ok := m["result"].(map[string]interface{}); ok {
			return formatToolResultInternal(inner, depth+1)
		}

		contentKeys := []string{"extracted_content", "headlines", "results", "items", "content", "summary", "text", "message"}
		for _, k := range contentKeys {
			if k == "content" {
				continue
			}
			if val, exists := m[k]; exists && val != nil {

				return formatToolResultInternal(val, depth+1)
			}
		}

		if len(m) == 1 {
			for _, v := range m {
				return formatToolResultInternal(v, depth+1)
			}
		}

		// 4. Fallback: format as key-value pairs
		var lines []string
		for k, v := range m {

			if k == "raw_html" || k == "cleaned_html" || k == "screenshot" || k == "cookies" || k == "extraction_method" {
				continue
			}

			valSummary := utils.SafeResultSummary(v, 2000)
			lines = append(lines, fmt.Sprintf("%s: %s", k, valSummary))

			if len(lines) > 50 {
				lines = append(lines, "... [TRUNCATED DUE TO LENGTH]")
				break
			}
		}
		if len(lines) == 0 {
			return ""
		}
		return strings.Join(lines, "\n")
	}

	if s, ok := result.([]interface{}); ok {
		var lines []string
		for i, item := range s {
			line := formatToolResultInternal(item, depth+1)
			if line != "" {
				lines = append(lines, fmt.Sprintf("[%d] %s", i+1, line))
			}
		}
		return strings.Join(lines, "\n")
	}

	if s, ok := result.(string); ok {
		s = strings.TrimSpace(s)

		if strings.Contains(s, "\n") {
			lines := strings.Split(s, "\n")
			var cleaned []string
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if line != "" {
					line = listCleanupRegex.ReplaceAllString(line, "")
					cleaned = append(cleaned, "• "+line)
				}
			}
			return strings.Join(cleaned, "\n")
		}

		if numberedListRegex.MatchString(s) {
			parts := numberedListRegex.Split(s, -1)
			var cleaned []string
			for _, p := range parts {
				p = strings.TrimSpace(p)
				if p != "" {
					cleaned = append(cleaned, "• "+p)
				}
			}
			if len(cleaned) > 1 {
				return strings.Join(cleaned, "\n")
			}
		}

		return s
	}

	return fmt.Sprintf("%v", result)
}

// sanitizeConsoleOutput removes noisy environment/provisioning logs and keeps meaningful program output
func sanitizeConsoleOutput(text string) string {
	if text == "" {
		return text
	}
	lines := strings.Split(text, "\n")
	filtered := make([]string, 0, len(lines))

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

// LLMClientWrapper wraps the existing LLMClient to implement the interpreter interface
type LLMClientWrapper struct {
	client   *LLMClient
	priority RequestPriority // Priority for LLM requests (defaults to low for background tasks)
}

// CallLLM implements the interpreter interface
// Uses low priority by default (for background tasks)
func (w *LLMClientWrapper) CallLLM(prompt string) (string, error) {

	if w == nil || w.client == nil {
		return "", fmt.Errorf("LLM client not initialized")
	}

	ctx := context.Background()
	return w.client.callLLMWithContextAndPriority(ctx, prompt, PriorityLow)
}

// CallLLMWithContextAndPriority calls LLM with context and priority
// highPriority=true for user requests, false for background tasks
func (w *LLMClientWrapper) CallLLMWithContextAndPriority(ctx context.Context, prompt string, highPriority bool) (string, error) {
	if w == nil || w.client == nil {
		return "", fmt.Errorf("LLM client not initialized")
	}

	if getComponentFromContext(ctx) == "unknown" {
		ctx = WithComponent(ctx, "hdn-interpreter")
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
	memoryConsolidator   *mempkg.MemoryConsolidator
	agentRegistry        *AgentRegistry  // Agent registry for autonomous agents
	agentExecutor        *AgentExecutor  // Agent executor
	agentHistory         *AgentHistory   // Agent execution history
	agentScheduler       *AgentScheduler // Agent scheduler for cron triggers
}

func NewAPIServer(domainPath string, redisAddr string) *APIServer {
	maxConcurrent := getMaxConcurrentExecutions()
	server := &APIServer{
		domainPath:           domainPath,
		router:               mux.NewRouter(),
		executionSemaphore:   make(chan struct{}, maxConcurrent-1),
		uiExecutionSemaphore: make(chan struct{}, 1),
	}

	if err := server.loadDomain(); err != nil {
		log.Printf("Warning: Could not load domain: %v", err)
		server.domain = &EnhancedDomain{
			Methods: []EnhancedMethodDef{},
			Actions: []EnhancedActionDef{},
			Config:  DomainConfig{},
		}
	}

	server.mcpClient = NewMCPClient(server.domain.Config)

	server.redis = redis.NewClient(&redis.Options{
		Addr: redisAddr,
	})
	server.redisAddr = redisAddr

	ctx := context.Background()
	if err := server.redis.Ping(ctx).Err(); err != nil {
		log.Printf("⚠️  [API] Failed to connect to Redis at %s: %v", redisAddr, err)
		log.Printf("⚠️  [API] This will cause tools not to be persisted. Check REDIS_URL environment variable.")
	} else {
		log.Printf("✅ [API] Successfully connected to Redis at %s", redisAddr)

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

	server.projectManager = NewProjectManager(redisAddr, 24)

	server.ensureProjectByName("Goals")

	server.workingMemory = mempkg.NewWorkingMemoryManager(redisAddr, 6)

	if base := os.Getenv("RAG_ADAPTER_URL"); strings.TrimSpace(base) != "" {
		server.episodicClient = mempkg.NewEpisodicClient(base)
		log.Printf("🧠 [API] Episodic memory enabled: %s", base)
	}

	if server.episodicClient == nil {
		qbase := os.Getenv("WEAVIATE_URL")
		if strings.TrimSpace(qbase) == "" {
			qbase = "http://localhost:8080"
		}

		server.vectorDB = mempkg.NewVectorDBAdapter(qbase, "AgiEpisodes")
		_ = server.vectorDB.EnsureCollection(768)

		log.Printf("🧠 [API] Episodic memory via Weaviate: %s", qbase)
	}

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

	server.domainManager = NewDomainManager(redisAddr, 24)
	server.actionManager = NewActionManager(redisAddr, 24)

	server.codeStorage = NewCodeStorage(redisAddr, 24)

	server.fileStorage = NewFileStorage(redisAddr, 24)

	server.dockerExecutor = NewSimpleDockerExecutorWithStorage(server.fileStorage)

	server.selfModelManager = selfmodel.NewManager(redisAddr, "hdn_self_model")

	server.llmWrapper = &LLMClientWrapper{client: server.llmClient}
	llmAdapter := interpreter.NewLLMAdapter(server.llmWrapper)

	thoughtExprSvc := conversational.NewThoughtExpressionService(server.redis, nil)
	server.interpreter = interpreter.NewInterpreterWithThoughtExpression(llmAdapter, thoughtExprSvc)
	server.interpreterAPI = interpreter.NewInterpreterAPI(server.interpreter)

	natsURL := os.Getenv("NATS_URL")
	if natsURL == "" {
		natsURL = "nats://localhost:4222"
	}
	if bus, err := eventbus.NewNATSBus(eventbus.NATSConfig{URL: natsURL, Subject: "agi.events.input"}); err == nil {
		server.eventBus = bus

		ctx := context.Background()
		_, err := server.eventBus.Subscribe(ctx, func(evt eventbus.CanonicalEvent) {
			server.processEvent(evt)
		})
		if err != nil {
			log.Printf("⚠️ [API] NATS subscribe failed: %v", err)
		} else {
			log.Printf("📡 [API] Subscribed to NATS events for memory feed")
		}

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

	toolMetrics, err := NewToolMetricsManager(redisAddr, "/tmp")
	if err != nil {
		log.Printf("Warning: Could not initialize tool metrics manager: %v", err)
		server.toolMetrics = nil
	} else {
		server.toolMetrics = toolMetrics
		log.Printf("📊 [API] Tool metrics logging enabled: %s", toolMetrics.GetLogFilePath())
	}

	if base := strings.TrimSpace(os.Getenv("HDN_BASE_URL")); base != "" {
		server.hdnBaseURL = base
	} else {
		server.hdnBaseURL = "http://localhost:8080"
	}

	server.setupRoutes()
	return server
}

// processEvent handles incoming events for memory and domain knowledge
func (s *APIServer) processEvent(evt eventbus.CanonicalEvent) {

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
		vec := toyEmbed(ep.Text, 768)
		_ = s.vectorDB.IndexEpisode(ep, vec)
	}

	if s.domainKnowledge != nil {

		src := strings.ToLower(strings.TrimSpace(evt.Source))
		typ := strings.ToLower(strings.TrimSpace(evt.Type))
		text := strings.TrimSpace(evt.Payload.Text)

		if len(text) >= 20 {
			allowed := false
			if strings.Contains(src, "wiki") || strings.Contains(typ, "wiki") ||
				strings.Contains(src, "bbc") || strings.Contains(typ, "news") ||
				strings.Contains(typ, "article") {
				allowed = true
			}

			if strings.Contains(typ, "tool") && (strings.Contains(typ, "success") || strings.Contains(typ, "completed") || strings.Contains(typ, "result")) {
				allowed = true
			}
			if allowed {
				name := strings.TrimSpace(evt.Type)
				if name == "" {
					name = "Event"
				}
				conceptName := fmt.Sprintf("%s_%s", name, evt.EventID)

				domain := "General"

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


func (s *APIServer) setupRoutes() {

	s.router.HandleFunc("/health", s.handleHealth).Methods("GET")

	s.router.HandleFunc("/api/v1/scraper/myclimate/flight", s.handleScraperMyClimateFlight).Methods("POST", "OPTIONS")
	s.router.HandleFunc("/api/v1/scraper/generic", s.handleScraperGeneric).Methods("POST", "OPTIONS")
	s.router.HandleFunc("/api/v1/scraper/agent/deploy", s.handleScraperAgentDeploy).Methods("POST", "OPTIONS")
	s.router.HandleFunc("/api/v1/scraper/health", s.handleScraperHealth).Methods("GET", "OPTIONS")

	s.router.HandleFunc("/api/v1/memory/consolidate", s.handleTriggerConsolidation).Methods("POST")

	s.RegisterMCPKnowledgeServerRoutes()
	s.RegisterConversationalRoutes()

	s.router.HandleFunc("/api/v1/agents", s.handleListAgents).Methods("GET")
	s.router.HandleFunc("/api/v1/agents", s.handleCreateAgent).Methods("POST")
	s.router.HandleFunc("/api/v1/agents", s.handleAgentOptions).Methods("OPTIONS")
	s.router.HandleFunc("/api/v1/agents/{id}", s.handleGetAgent).Methods("GET")
	s.router.HandleFunc("/api/v1/agents/{id}", s.handleDeleteAgent).Methods("DELETE")
	s.router.HandleFunc("/api/v1/agents/{id}", s.handleAgentOptions).Methods("OPTIONS")
	s.router.HandleFunc("/api/v1/agents/{id}/execute", s.handleExecuteAgent).Methods("POST")
	s.router.HandleFunc("/api/v1/agents/{id}/executions", s.handleGetAgentExecutions).Methods("GET")
	s.router.HandleFunc("/api/v1/agents/{id}/executions/{execution_id}", s.handleGetAgentExecution).Methods("GET")
	s.router.HandleFunc("/api/v1/agents/{id}/status", s.handleGetAgentStatus).Methods("GET")
	s.router.HandleFunc("/api/v1/crews", s.handleListCrews).Methods("GET")

	s.router.HandleFunc("/api/v1/task/execute", s.handleExecuteTask).Methods("POST")
	s.router.HandleFunc("/api/v1/task/plan", s.handlePlanTask).Methods("POST")

	s.router.HandleFunc("/api/v1/learn", s.handleLearn).Methods("POST")
	s.router.HandleFunc("/api/v1/learn/llm", s.handleLearnLLM).Methods("POST")
	s.router.HandleFunc("/api/v1/learn/mcp", s.handleLearnMCP).Methods("POST")

	s.router.HandleFunc("/api/v1/domain", s.handleGetDomain).Methods("GET")
	s.router.HandleFunc("/api/v1/domain", s.handleUpdateDomain).Methods("PUT")
	s.router.HandleFunc("/api/v1/domain/save", s.handleSaveDomain).Methods("POST")

	s.router.HandleFunc("/api/v1/domains", s.handleListDomains).Methods("GET")
	s.router.HandleFunc("/api/v1/domains", s.handleCreateDomain).Methods("POST")
	s.router.HandleFunc("/api/v1/domains/{name}", s.handleGetDomainByName).Methods("GET")
	s.router.HandleFunc("/api/v1/domains/{name}", s.handleDeleteDomain).Methods("DELETE")
	s.router.HandleFunc("/api/v1/domains/{name}/switch", s.handleSwitchDomain).Methods("POST")

	s.router.HandleFunc("/api/v1/hierarchical/execute", s.handleHierarchicalExecute).Methods("POST")
	s.router.HandleFunc("/api/v1/hierarchical/workflow/{id}/status", s.handleGetWorkflowStatus).Methods("GET")
	s.router.HandleFunc("/api/v1/hierarchical/workflow/{id}/details", s.handleGetWorkflowDetails).Methods("GET")
	s.router.HandleFunc("/api/v1/hierarchical/workflow/{id}/pause", s.handlePauseWorkflow).Methods("POST")
	s.router.HandleFunc("/api/v1/hierarchical/workflow/{id}/resume", s.handleResumeWorkflow).Methods("POST")
	s.router.HandleFunc("/api/v1/hierarchical/workflow/{id}/cancel", s.handleCancelWorkflow).Methods("POST")
	s.router.HandleFunc("/api/v1/hierarchical/workflows", s.handleListActiveWorkflows).Methods("GET")
	s.router.HandleFunc("/api/v1/hierarchical/templates", s.handleListWorkflowTemplates).Methods("GET")
	s.router.HandleFunc("/api/v1/hierarchical/templates", s.handleRegisterWorkflowTemplate).Methods("POST")

	s.router.HandleFunc("/api/v1/actions", s.handleCreateAction).Methods("POST")
	s.router.HandleFunc("/api/v1/actions/{domain}", s.handleListActions).Methods("GET")
	s.router.HandleFunc("/api/v1/actions/{domain}/{id}", s.handleGetAction).Methods("GET")
	s.router.HandleFunc("/api/v1/actions/{domain}/{id}", s.handleDeleteAction).Methods("DELETE")
	s.router.HandleFunc("/api/v1/actions/{domain}/search", s.handleSearchActions).Methods("POST")

	s.router.HandleFunc("/api/v1/docker/execute", s.handleDockerExecute).Methods("POST")
	s.router.HandleFunc("/api/v1/docker/primes", s.handleDockerPrimes).Methods("POST")
	s.router.HandleFunc("/api/v1/docker/generate", s.handleDockerGenerateCode).Methods("POST")

	s.router.HandleFunc("/api/v1/files/{filename}", s.handleServeFile).Methods("GET")
	s.router.HandleFunc("/api/v1/files/workflow/{workflow_id}", s.handleGetWorkflowFiles).Methods("GET")
	s.router.HandleFunc("/api/v1/workflow/{workflow_id}/files/{filename}", s.handleServeWorkflowFile).Methods("GET")

	s.router.HandleFunc("/api/v1/state", s.handleGetState).Methods("GET")
	s.router.HandleFunc("/api/v1/state", s.handleUpdateState).Methods("PUT")

	s.router.HandleFunc("/state/session/{id}/working_memory", s.handleGetWorkingMemory).Methods("GET")
	s.router.HandleFunc("/api/v1/state/session/{id}/working_memory", s.handleGetWorkingMemory).Methods("GET")

	s.router.HandleFunc("/api/v1/state/session/{id}/working_memory/event", s.handleAddWorkingMemoryEvent).Methods("POST")
	s.router.HandleFunc("/api/v1/state/session/{id}/working_memory/locals", s.handleSetWorkingMemoryLocals).Methods("PUT")
	s.router.HandleFunc("/api/v1/state/session/{id}/working_memory/plan", s.handleSetWorkingMemoryPlan).Methods("PUT")

	s.router.HandleFunc("/api/v1/intelligent/execute", s.handleIntelligentExecute).Methods("POST")
	s.router.HandleFunc("/api/v1/intelligent/execute", s.handleIntelligentExecuteOptions).Methods("OPTIONS")
	s.router.HandleFunc("/api/v1/intelligent/primes", s.handlePrimeNumbers).Methods("POST")
	s.router.HandleFunc("/api/v1/intelligent/capabilities", s.handleListCapabilities).Methods("GET")

	s.router.HandleFunc("/api/v1/interpret", s.handleInterpret).Methods("POST")
	s.router.HandleFunc("/api/v1/interpret/execute", s.handleInterpretAndExecute).Methods("POST")

	s.router.HandleFunc("/api/v1/tools", s.handleListTools).Methods("GET")
	s.router.HandleFunc("/api/v1/tools", s.handleRegisterTool).Methods("POST")
	s.router.HandleFunc("/api/v1/tools/{id}", s.handleDeleteTool).Methods("DELETE")
	s.router.HandleFunc("/api/v1/tools/discover", s.handleDiscoverTools).Methods("POST")

	s.router.HandleFunc("/api/v1/tools/{id}/invoke", s.handleInvokeTool).Methods("POST")

	s.router.HandleFunc("/api/v1/tools/metrics", s.handleGetAllToolMetrics).Methods("GET")
	s.router.HandleFunc("/api/v1/tools/{id}/metrics", s.handleGetToolMetrics).Methods("GET")
	s.router.HandleFunc("/api/v1/tools/calls/recent", s.handleGetRecentToolCalls).Methods("GET")

	s.router.HandleFunc("/api/v1/llm/queue/stats", s.handleLLMQueueStats).Methods("GET")

	s.router.HandleFunc("/api/v1/episodes/search", s.handleSearchEpisodes).Methods("GET")

	s.router.HandleFunc("/api/v1/memory/summary", s.handleMemorySummary).Methods("GET")
	s.router.HandleFunc("/api/v1/memory/goals/{id}/status", s.handleUpdateGoalStatus).Methods("POST")
	s.router.HandleFunc("/api/v1/memory/goals/{id}", s.handleDeleteSelfModelGoal).Methods("DELETE")

	s.router.HandleFunc("/api/v1/memory/goals/cleanup", s.handleCleanupSelfModelGoals).Methods("POST")

	s.router.HandleFunc("/api/v1/projects", s.handleCreateProject).Methods("POST")
	s.router.HandleFunc("/api/v1/projects", s.handleListProjects).Methods("GET")

	s.router.HandleFunc("/api/v1/projects/{id}", s.handleGetProject).Methods("GET")
	s.router.HandleFunc("/api/v1/projects/{id}", s.handleUpdateProject).Methods("PUT")
	s.router.HandleFunc("/api/v1/projects/{id}", s.handleDeleteProject).Methods("DELETE")

	s.router.HandleFunc("/api/v1/projects/{id}/pause", s.handlePauseProject).Methods("POST")
	s.router.HandleFunc("/api/v1/projects/{id}/resume", s.handleResumeProject).Methods("POST")
	s.router.HandleFunc("/api/v1/projects/{id}/archive", s.handleArchiveProject).Methods("POST")

	s.router.HandleFunc("/api/v1/projects/{id}/checkpoints", s.handleListProjectCheckpoints).Methods("GET")
	s.router.HandleFunc("/api/v1/projects/{id}/checkpoints", s.handleAddProjectCheckpoint).Methods("POST")

	s.router.HandleFunc("/api/v1/projects/{id}/workflows", s.handleListProjectWorkflows).Methods("GET")

	s.router.HandleFunc("/api/v1/workflows/resolve/{id}", s.handleResolveWorkflowID).Methods("GET")

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

	s.router.HandleFunc("/api/v1/knowledge/query", s.handleKnowledgeQuery).Methods("POST")

	s.router.HandleFunc("/api/v1/nemoclaw/response", s.handleNemoClawResponse).Methods("POST")
}

func (s *APIServer) handleNemoClawResponse(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ChatID   string `json:"chat_id"`
		Response string `json:"response"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.ChatID == "" || req.Response == "" {
		http.Error(w, "chat_id and response are required", http.StatusBadRequest)
		return
	}

	cleanChatID := strings.TrimPrefix(req.ChatID, "tg_chat_")
	key := fmt.Sprintf("hdn:nemoclaw:response:%s", cleanChatID)
	log.Printf("📥 [API] Received NemoClaw response for chat %s (clean: %s), saving to Redis key: %s", req.ChatID, cleanChatID, key)

	err := s.redis.Set(r.Context(), key, req.Response, 10*time.Minute).Err()
	if err != nil {
		log.Printf("❌ [API] Failed to save NemoClaw response to Redis: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

// SetLLMClient sets the LLM client (called from server.go after environment overrides)
func (s *APIServer) SetLLMClient(client *LLMClient) {
	s.llmClient = client

	if s.mcpKnowledgeServer != nil {
		s.mcpKnowledgeServer.SetLLMClient(client)
	}

	s.codeGenerator = NewCodeGenerator(s.llmClient, s.codeStorage)

	s.initializeConversationalLayer()

	if s.llmWrapper != nil {
		s.llmWrapper.client = s.llmClient
	}

	s.SyncPromptHints()
}

func (s *APIServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	response := HealthResponse{
		Status:    "healthy",
		Timestamp: time.Now().Format(time.RFC3339),
		Version:   "1.0.0",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// looksLikeCode provides a minimal heuristic to detect code-like text outputs
func looksLikeCode(s string) bool {
	ls := strings.TrimSpace(s)
	if ls == "" {
		return false
	}

	cues := []string{
		"def ", "class ", "import ", "from ", "function ", "package ", "#include", "const ", "let ", "var ", "func ",
	}
	for _, c := range cues {
		if strings.Contains(ls, c) {
			return true
		}
	}

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

// createSimplePDF generates a very simple PDF from a title, subtitle and optional JSON payload
func (s *APIServer) createSimplePDF(title, subtitle string, payload interface{}) []byte {
	content := fmt.Sprintf("%v", payload)

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

// SetMCPKnowledgeServer sets the MCP knowledge server for the API
func (s *APIServer) SetMCPKnowledgeServer(server *MCPKnowledgeServer) {
	s.mcpKnowledgeServer = server

	s.SyncPromptHints()
}

func (s *APIServer) Start(port int) error {
	addr := fmt.Sprintf(":%d", port)

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

	return 20
}

// isUIRequest checks if the request is from the UI based on headers or context
func isUIRequest(r *http.Request) bool {

	source := r.Header.Get("X-Request-Source")
	if source == "ui" || source == "telegram" {
		return true
	}

	if r.URL.Query().Get("context") == "ui" {
		return true
	}
	return false
}

// acquireExecutionSlot attempts to acquire an execution slot, preferring UI slot for UI requests
func (s *APIServer) acquireExecutionSlot(r *http.Request) (func(), bool) {
	isUI := isUIRequest(r)

	if isUI {

		select {
		case s.uiExecutionSemaphore <- struct{}{}:
			return func() { <-s.uiExecutionSemaphore }, true
		default:

			select {
			case s.executionSemaphore <- struct{}{}:
				return func() { <-s.executionSemaphore }, true
			default:
				return nil, false
			}
		}
	} else {

		select {
		case s.executionSemaphore <- struct{}{}:
			return func() { <-s.executionSemaphore }, true
		default:
			return nil, false
		}
	}
}
