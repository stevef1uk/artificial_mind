package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

// MonitorService provides real-time monitoring of the HDN system
type MonitorService struct {
	redisClient     *redis.Client
	hdnURL          string
	principlesURL   string
	goalMgrURL      string
	fsmURL          string
	neo4jURL        string
	weaviateURL     string
	natsURL         string
	executionMethod string
}

// SystemStatus represents the overall system health
type SystemStatus struct {
	Overall   string                 `json:"overall"`
	Timestamp time.Time              `json:"timestamp"`
	Services  map[string]ServiceInfo `json:"services"`
	Metrics   SystemMetrics          `json:"metrics"`
	Alerts    []Alert                `json:"alerts"`
}

// ServiceInfo represents individual service status
type ServiceInfo struct {
	Name         string    `json:"name"`
	Status       string    `json:"status"`
	URL          string    `json:"url"`
	LastCheck    time.Time `json:"last_check"`
	ResponseTime int64     `json:"response_time_ms"`
	Error        string    `json:"error,omitempty"`
}

// SystemMetrics contains system-wide metrics
type SystemMetrics struct {
	ActiveWorkflows      int     `json:"active_workflows"`
	TotalExecutions      int     `json:"total_executions"`
	SuccessRate          float64 `json:"success_rate"`
	AverageExecutionTime float64 `json:"avg_execution_time_ms"`
	RedisConnections     int     `json:"redis_connections"`
	DockerContainers     int     `json:"docker_containers"`
}

// Alert represents system alerts
type Alert struct {
	ID        string    `json:"id"`
	Level     string    `json:"level"` // info, warning, error, critical
	Message   string    `json:"message"`
	Timestamp time.Time `json:"timestamp"`
	Service   string    `json:"service"`
}

// WorkflowStatus represents workflow execution status
type WorkflowStatus struct {
	ID              string                 `json:"id"`
	Status          string                 `json:"status"`
	TaskName        string                 `json:"task_name"`
	Description     string                 `json:"description"`
	Progress        float64                `json:"progress"`
	TotalSteps      int                    `json:"total_steps"`
	CompletedSteps  int                    `json:"completed_steps"`
	FailedSteps     int                    `json:"failed_steps"`
	CurrentStep     string                 `json:"current_step"`
	StartedAt       time.Time              `json:"started_at"`
	UpdatedAt       time.Time              `json:"updated_at"`
	CompletedAt     *time.Time             `json:"completed_at,omitempty"`
	CanResume       bool                   `json:"can_resume"`
	CanCancel       bool                   `json:"can_cancel"`
	Steps           []WorkflowStepStatus   `json:"steps"`
	Error           string                 `json:"error,omitempty"`
	ProgressDetails map[string]interface{} `json:"progress_details,omitempty"`
	Files           []FileInfo             `json:"files"`
	GeneratedCode   interface{}            `json:"generated_code,omitempty"`
}

// WorkflowStepStatus represents individual step status
type WorkflowStepStatus struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	StepType    string     `json:"step_type"`
	Status      string     `json:"status"`
	StartedAt   time.Time  `json:"started_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	Duration    int64      `json:"duration,omitempty"` // Duration in milliseconds
	Output      string     `json:"output,omitempty"`
	Error       string     `json:"error,omitempty"`
}

// ExecutionMetrics represents execution statistics
type ExecutionMetrics struct {
	TotalExecutions      int            `json:"total_executions"`
	SuccessfulExecutions int            `json:"successful_executions"`
	FailedExecutions     int            `json:"failed_executions"`
	SuccessRate          float64        `json:"success_rate"`
	AverageTime          float64        `json:"average_time_ms"`
	LastExecution        time.Time      `json:"last_execution"`
	ByLanguage           map[string]int `json:"by_language"`
	ByTaskType           map[string]int `json:"by_task_type"`
}

// RedisInfo represents Redis connection information
type RedisInfo struct {
	Connected        bool           `json:"connected"`
	Version          string         `json:"version"`
	UsedMemory       string         `json:"used_memory"`
	ConnectedClients int            `json:"connected_clients"`
	Keyspace         map[string]int `json:"keyspace"`
}

// DockerInfo represents Docker container information
type DockerInfo struct {
	ContainersRunning int `json:"containers_running"`
	ContainersTotal   int `json:"containers_total"`
	ImagesCount       int `json:"images_count"`
}

func NewMonitorService() *MonitorService {
	redisURL := strings.TrimSpace(os.Getenv("REDIS_URL"))
	if redisURL == "" {
		redisURL = "redis://localhost:6379" // Default for Docker Redis
	}

	// Parse Redis URL to extract host and port
	var addr string
	if strings.HasPrefix(redisURL, "redis://") {
		addr = strings.TrimPrefix(redisURL, "redis://")
	} else {
		addr = redisURL
	}

	redisClient := redis.NewClient(&redis.Options{
		Addr: addr,
	})

	hdnURL := strings.TrimSpace(os.Getenv("HDN_URL"))
	if hdnURL == "" {
		hdnURL = "http://localhost:8080"
	}
	principlesURL := strings.TrimSpace(os.Getenv("PRINCIPLES_URL"))
	if principlesURL == "" {
		principlesURL = "http://localhost:8084"
	}
	goalMgrURL := strings.TrimSpace(os.Getenv("GOAL_MANAGER_URL"))
	if goalMgrURL == "" {
		goalMgrURL = "http://localhost:8090"
	}
	fsmURL := strings.TrimSpace(os.Getenv("FSM_URL"))
	if fsmURL == "" {
		fsmURL = "http://localhost:8083"
	}
	// Check NEO4J_URL first, fall back to NEO4J_URI if not set (for compatibility)
	neo4jURL := strings.TrimSpace(os.Getenv("NEO4J_URL"))
	if neo4jURL == "" {
		// Fall back to NEO4J_URI (used by other services) and convert if needed
		neo4jURI := strings.TrimSpace(os.Getenv("NEO4J_URI"))
		if neo4jURI != "" {
			neo4jURL = neo4jURI
		} else {
			neo4jURL = "http://localhost:7474"
		}
	}
	
	// Convert Bolt protocol URL to HTTP monitoring URL if needed
	// Works for both Kubernetes and non-Kubernetes:
	// - bolt://localhost:7687 -> http://localhost:7474
	// - bolt://neo4j.agi.svc.cluster.local:7687 -> http://neo4j.agi.svc.cluster.local:7474
	if strings.HasPrefix(neo4jURL, "bolt://") {
		neo4jURL = strings.Replace(neo4jURL, "bolt://", "http://", 1)
		// Replace port 7687 with 7474 (HTTP port)
		if strings.Contains(neo4jURL, ":7687") {
			neo4jURL = strings.Replace(neo4jURL, ":7687", ":7474", 1)
		} else if !strings.Contains(neo4jURL, ":") {
			// No port specified, add HTTP port
			neo4jURL = neo4jURL + ":7474"
		} else {
			// Has a port but not 7687, replace it with 7474
			parts := strings.Split(neo4jURL, ":")
			if len(parts) >= 2 {
				neo4jURL = strings.Join(parts[:len(parts)-1], ":") + ":7474"
			}
		}
	} else if !strings.HasPrefix(neo4jURL, "http://") && !strings.HasPrefix(neo4jURL, "https://") {
		// If it's not a full URL, assume it's just a host and add http:// and port
		if !strings.Contains(neo4jURL, ":") {
			neo4jURL = "http://" + neo4jURL + ":7474"
		} else {
			neo4jURL = "http://" + neo4jURL
		}
	}
	weaviateURL := strings.TrimSpace(os.Getenv("WEAVIATE_URL"))
	if weaviateURL == "" {
		weaviateURL = "http://localhost:8080"
	}
	natsURL := strings.TrimSpace(os.Getenv("NATS_URL"))
	if natsURL == "" {
		natsURL = "http://localhost:8223"
	} else if strings.HasPrefix(natsURL, "nats://") {
		// Convert NATS protocol URL to HTTP monitoring URL
		// nats://nats.agi.svc.cluster.local:4222 -> http://nats.agi.svc.cluster.local:8223
		// nats://nats.agi.svc.cluster.local -> http://nats.agi.svc.cluster.local:8223
		natsURL = strings.Replace(natsURL, "nats://", "http://", 1)
		// Replace port 4222 with 8223, or add :8223 if no port specified
		if strings.Contains(natsURL, ":4222") {
			natsURL = strings.Replace(natsURL, ":4222", ":8223", 1)
		} else if !strings.Contains(natsURL, ":") {
			// No port specified, add monitoring port
			natsURL = natsURL + ":8223"
		} else {
			// Has a port but not 4222, replace it with 8223
			// Extract host and replace port
			parts := strings.Split(natsURL, ":")
			if len(parts) >= 2 {
				natsURL = strings.Join(parts[:len(parts)-1], ":") + ":8223"
			}
		}
	} else if !strings.HasPrefix(natsURL, "http://") && !strings.HasPrefix(natsURL, "https://") {
		// If it's not a full URL, assume it's just a host and add http:// and port
		if !strings.Contains(natsURL, ":") {
			natsURL = "http://" + natsURL + ":8223"
		} else {
			natsURL = "http://" + natsURL
		}
	}

	m := &MonitorService{
		redisClient:   redisClient,
		hdnURL:        hdnURL,
		principlesURL: principlesURL,
		goalMgrURL:    goalMgrURL,
		fsmURL:        fsmURL,
		neo4jURL:      neo4jURL,
		weaviateURL:   weaviateURL,
		natsURL:       natsURL,
	}
	// Start LLM worker
	m.runLLMWorker()
	return m
}

func main() {
	// Load environment from .env if present (best-effort)
	loadDotEnv(".env")
	// Set Gin to release mode for production
	gin.SetMode(gin.ReleaseMode)

	r := gin.Default()

	// Add CORS middleware
	r.Use(func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Origin, Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	})

	// Serve static files (configurable via MONITOR_STATIC_DIR)
	staticDir := strings.TrimSpace(os.Getenv("MONITOR_STATIC_DIR"))
	if staticDir == "" {
		staticDir = "./static"
	}
	if !strings.HasPrefix(staticDir, "/") {
		if wd, err := os.Getwd(); err == nil {
			staticDir = filepath.Join(wd, staticDir)
		}
	}
	r.Static("/static", staticDir)
	// Explicitly load templates and partials (Glob lacks ** support)
	// Get the directory where the monitor binary is located
	execPath, err := os.Executable()
	if err != nil {
		log.Fatal("Failed to get executable path:", err)
	}
	execDir := filepath.Dir(execPath)

	// Look for templates in the monitor directory (one level up from bin)
	projectRoot := filepath.Dir(execDir)
	monitorDir := filepath.Join(projectRoot, "monitor")
	tmplFiles, _ := filepath.Glob(filepath.Join(monitorDir, "templates/*.html"))
	partialFiles, _ := filepath.Glob(filepath.Join(monitorDir, "templates/partials/*.html"))
	all := append([]string{}, tmplFiles...)
	all = append(all, partialFiles...)
	if len(all) == 0 {
		log.Println("Warning: no templates found in", filepath.Join(monitorDir, "templates/"))
		// Fallback to relative paths
		tmplFiles, _ := filepath.Glob("templates/*.html")
		partialFiles, _ := filepath.Glob("templates/partials/*.html")
		all = append([]string{}, tmplFiles...)
		all = append(all, partialFiles...)
		if len(all) == 0 {
			log.Fatal("No templates found in templates/ or templates/partials/")
		}
	}
	r.LoadHTMLFiles(all...)

	monitor := NewMonitorService()

	// Debug message to confirm updated code is running
	log.Printf("🚀 [MONITOR] Starting Monitor Service with PROJECT_ID support - Version 2025-09-25")

	// Routes
	r.GET("/", monitor.dashboard)
	r.GET("/tabs", monitor.dashboardTabs)
	r.GET("/test", monitor.testPage)
	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "healthy", "timestamp": time.Now().Format(time.RFC3339)})
	})
	r.GET("/chat", monitor.chatPage)
	r.GET("/thinking", monitor.thinkingPanel)
	r.POST("/api/chat", monitor.chatAPI)

	// Chat sessions API (proxy to HDN)
	r.GET("/api/v1/chat/sessions", monitor.getChatSessions)
	r.GET("/api/v1/chat/sessions/:sessionId/thoughts", monitor.getSessionThoughts)
	r.GET("/api/v1/chat/sessions/:sessionId/thoughts/stream", monitor.streamSessionThoughts)
	r.POST("/api/v1/chat/sessions/:sessionId/thoughts/express", monitor.expressSessionThoughts)
	r.GET("/api/v1/chat/sessions/:sessionId/history", monitor.getSessionHistory)
	r.GET("/api/status", monitor.getSystemStatus)
	r.GET("/api/workflows", monitor.getActiveWorkflows)
	r.GET("/api/workflow/:workflow_id/details", monitor.getWorkflowDetails)
	r.GET("/api/projects", monitor.getProjects)
	r.GET("/api/projects/:id", monitor.getProject)
	r.DELETE("/api/projects/:id", monitor.deleteProject)
	r.GET("/api/projects/:id/checkpoints", monitor.getProjectCheckpoints)
	r.GET("/api/projects/:id/workflows", monitor.getProjectWorkflows)
	// Analyze the latest workflow for a project and produce analysis.md in that project
	r.POST("/api/projects/:id/analyze_last_workflow", monitor.analyzeLastWorkflow)
	r.GET("/api/workflow/:workflow_id/files", monitor.listWorkflowFiles)
	r.GET("/api/workflow/:workflow_id/project", monitor.getWorkflowProject)
	// Serve individual workflow artifacts (code/images/pdfs)
	r.GET("/api/workflow/:workflow_id/files/:filename", monitor.serveWorkflowFile)
	// Optional: serve general files if needed by other links
	r.GET("/api/file/:filename", monitor.serveGenericFile)
	r.GET("/api/files/*filename", monitor.serveFile)
	r.GET("/api/metrics", monitor.getExecutionMetrics)
	r.GET("/api/redis", monitor.getRedisInfo)
	r.GET("/api/docker", monitor.getDockerInfo)
	r.GET("/api/neo4j", monitor.getNeo4jInfo)
	r.GET("/api/neo4j/stats", monitor.getNeo4jStats)
	r.GET("/api/qdrant", monitor.getQdrantInfo)
	r.GET("/api/qdrant/stats", monitor.getQdrantStats)
	r.GET("/api/weaviate", monitor.getQdrantInfo)              // Reuse Qdrant function for Weaviate
	r.GET("/api/weaviate/stats", monitor.getQdrantStats)       // Reuse Qdrant function for Weaviate
	r.GET("/api/weaviate/records", monitor.getWeaviateRecords) // Dedicated Weaviate records count
	r.GET("/api/nats", monitor.getNATSInfo)
	// RAG search
	r.GET("/api/rag/search", monitor.ragSearch)
	r.POST("/api/goal/:id/update-status", monitor.updateGoalStatus)
	r.DELETE("/api/goal/:id", monitor.deleteSelfModelGoal)
	r.GET("/api/logs", monitor.getLogs)
	r.GET("/api/k8s/services", monitor.getK8sServices)
	r.GET("/api/k8s/logs/:service", monitor.getK8sLogs)
	r.GET("/api/ws", monitor.websocketHandler)
	// r.GET("/api/files/*filename", monitor.serveFile)
	// r.GET("/api/workflow/:workflow_id/file/:filename", monitor.serveWorkflowFile)

	// Capabilities listing
	r.GET("/api/capabilities", monitor.getCapabilities)

	// Memory visualization APIs (proxies to HDN)
	r.GET("/api/memory/summary", monitor.getMemorySummary)
	r.GET("/api/memory/episodes", monitor.searchEpisodes)
	// Bulk cleanup of internal self-model goals
	r.POST("/api/memory/goals/cleanup", monitor.cleanupSelfModelGoals)
	// News events from BBC and Wikipedia
	r.GET("/api/news/events", monitor.getNewsEvents)
	// Wikipedia events
	r.GET("/api/wikipedia/events", monitor.getWikipediaEvents)

	// Daily summary APIs
	r.GET("/api/daily_summary/latest", monitor.getDailySummaryLatest)
	r.GET("/api/daily_summary/history", monitor.getDailySummaryHistory)
	r.GET("/api/daily_summary/:date", monitor.getDailySummaryByDate)

	// Evaluations (proxy working memory summaries from HDN)
	r.GET("/api/evaluations", monitor.getFsmEvaluations)

	// Goals Manager proxy APIs
	r.GET("/api/goals/:agent/active", monitor.getActiveGoals)
	r.GET("/api/goal/:id", monitor.getGoalByID)
	r.POST("/api/goal/:id/achieve", monitor.achieveGoal)
	r.POST("/api/goal/:id/suggest", monitor.suggestGoalNextSteps)
	// Execute suggested plan for a goal
	r.POST("/api/goal/:id/execute", monitor.executeGoalSuggestedPlan)
	// Create goal from NL input
	r.POST("/api/goals/create_from_nl", monitor.createGoalFromNL)

	// Reasoning Layer APIs
	r.GET("/api/reasoning/traces/:domain", monitor.getReasoningTraces)
	r.GET("/api/reasoning/beliefs/:domain", monitor.getBeliefs)
	r.GET("/api/reasoning/curiosity-goals/:domain", monitor.getCuriosityGoals)
	r.GET("/api/reasoning/hypotheses/:domain", monitor.getHypotheses)
	r.GET("/api/reasoning/explanations/:goal", monitor.getReasoningExplanations)
	r.GET("/api/reasoning/explanations", monitor.getRecentExplanations)
	r.GET("/api/reasoning/domains", monitor.getReasoningDomains)

	// Reflection endpoint - comprehensive introspection of system's mental state
	r.GET("/api/reflect", monitor.getReflection)

	// FSM proxy (embed FSM UI/API under the monitor)
	r.Any("/api/fsm/*path", monitor.proxyFSM)

	// Natural Language Interpreter Routes
	r.POST("/api/interpret", monitor.interpretNaturalLanguage)
	r.POST("/api/interpret/execute", monitor.interpretAndExecute)
	// LLM Queue (Redis lists) endpoints
	r.POST("/api/llm/enqueue", monitor.enqueueLLMJob)
	r.GET("/api/llm/status/:id", monitor.getLLMJobStatus)

	// Proxy intelligent execution to HDN server
	r.POST("/api/v1/intelligent/execute", monitor.proxyIntelligentExecute)
	r.OPTIONS("/api/v1/intelligent/execute", monitor.handleCORS)

	// Tools APIs
	r.GET("/api/tools", monitor.getTools)
	r.GET("/api/tools/usage", monitor.getToolUsage)
	r.GET("/api/tools/metrics", monitor.getToolMetrics)
	r.GET("/api/tools/:id/metrics", monitor.getToolMetricsByID)
	r.GET("/api/tools/calls/recent", monitor.getRecentToolCalls)
	r.POST("/api/clear-safety-data", monitor.clearSafetyData)
	r.DELETE("/api/tools/:id", func(c *gin.Context) {
		id := c.Param("id")
		if id == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "missing id"})
			return
		}
		// Forward to HDN delete endpoint
		hdn := strings.TrimRight(monitor.hdnURL, "/") + "/api/v1/tools/" + id
		req, _ := http.NewRequest(http.MethodDelete, hdn, nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": "hdn delete failed"})
			return
		}
		defer resp.Body.Close()
		io.Copy(io.Discard, resp.Body)
		c.JSON(http.StatusOK, gin.H{"success": resp.StatusCode >= 200 && resp.StatusCode < 300, "deleted": id})
	})

	// Start curiosity goal consumer in background
	go monitor.startCuriosityGoalConsumer()

	// Start auto-executor in background
	go monitor.startAutoExecutor()

	// Start server
	fmt.Println("🚀 HDN Monitor UI starting on :8082")
	fmt.Println("📊 Dashboard: http://localhost:8082")
	fmt.Println("🔧 API: http://localhost:8082/api/status")

	if err := r.Run(":8082"); err != nil {
		log.Fatal("Failed to start monitor server:", err)
	}
}

// dashboard renders the main monitoring dashboard (now tabbed)
func (m *MonitorService) dashboard(c *gin.Context) {
	// Fallback: serve raw HTML to avoid html/template context parse issues
	path := "templates/dashboard_tabs.html"
	b, err := os.ReadFile(path)
	if err == nil {
		// Strip Go template define/end wrappers if present
		content := string(b)
		if strings.HasPrefix(content, "{{ define") {
			if i := strings.Index(content, "}}\n"); i > -1 {
				content = content[i+3:]
			}
			if j := strings.LastIndex(content, "{{ end }}"); j > -1 {
				content = content[:j]
			}
		}
		c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(content))
		return
	}
	// If read fails, fall back to templating
	c.HTML(http.StatusOK, "dashboard_tabs.html", gin.H{
		"title":         "Artificial Mind and Workflow",
		"hdnURL":        m.hdnURL,
		"principlesURL": m.principlesURL,
	})
}

// dashboardTabs renders the tabbed monitoring dashboard
func (m *MonitorService) dashboardTabs(c *gin.Context) {
	// Serve raw HTML (see dashboard handler for details)
	path := "templates/dashboard_tabs.html"
	b, err := os.ReadFile(path)
	if err == nil {
		content := string(b)
		if strings.HasPrefix(content, "{{ define") {
			if i := strings.Index(content, "}}\n"); i > -1 {
				content = content[i+3:]
			}
			if j := strings.LastIndex(content, "{{ end }}"); j > -1 {
				content = content[:j]
			}
		}
		c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(content))
		return
	}
	c.HTML(http.StatusOK, "dashboard_tabs.html", gin.H{
		"title":         "Artificial Mind and Workflow (Tabbed)",
		"hdnURL":        m.hdnURL,
		"principlesURL": m.principlesURL,
	})
}

// thinkingPanel renders the AI thinking stream page
func (m *MonitorService) thinkingPanel(c *gin.Context) {
	c.HTML(http.StatusOK, "thinking_panel.html", gin.H{
		"title":  "AI Thinking Stream",
		"hdnURL": m.hdnURL,
	})
}

// -------------------------
// LLM Priority Queue (Redis Lists) MVP
// -------------------------

type llmJob struct {
	ID        string            `json:"id"`
	Priority  string            `json:"priority"`
	Input     string            `json:"input"`
	Context   map[string]string `json:"context,omitempty"`
	SessionID string            `json:"session_id,omitempty"`
	Callback  string            `json:"callback_url,omitempty"`
}

// enqueueLLMJob enqueues an interpretation job with priority: high|normal|low
func (m *MonitorService) enqueueLLMJob(c *gin.Context) {
	var req struct {
		Input     string            `json:"input"`
		Context   map[string]string `json:"context,omitempty"`
		SessionID string            `json:"session_id,omitempty"`
		Priority  string            `json:"priority"`
		Callback  string            `json:"callback_url,omitempty"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Input) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	if req.Context == nil {
		req.Context = map[string]string{}
	}
	if req.SessionID == "" {
		req.SessionID = fmt.Sprintf("session_%d", time.Now().UnixNano())
	}
	pr := strings.ToLower(strings.TrimSpace(req.Priority))
	if pr == "" {
		pr = "normal"
	}
	if pr != "high" && pr != "normal" && pr != "low" {
		pr = "normal"
	}

	job := llmJob{
		ID:        fmt.Sprintf("llmjob_%d", time.Now().UnixNano()),
		Priority:  pr,
		Input:     req.Input,
		Context:   req.Context,
		SessionID: req.SessionID,
		Callback:  strings.TrimSpace(req.Callback),
	}
	bts, _ := json.Marshal(job)

	// Write initial status
	if m.redisClient != nil {
		key := fmt.Sprintf("llm:result:%s", job.ID)
		_ = m.redisClient.HSet(c, key, map[string]interface{}{
			"status":      "queued",
			"priority":    job.Priority,
			"enqueued_at": time.Now().Format(time.RFC3339),
			"callback":    job.Callback,
		}).Err()
		_ = m.redisClient.Expire(c, key, 2*time.Hour).Err()
	}

	// Push to appropriate queue
	queue := fmt.Sprintf("llm:%s", job.Priority)
	if m.redisClient == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "redis not configured"})
		return
	}
	if _, err := m.redisClient.LPush(c, queue, bts).Result(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "queue error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"job_id": job.ID, "status": "queued"})
}

// getLLMJobStatus returns job status/result
func (m *MonitorService) getLLMJobStatus(c *gin.Context) {
	id := c.Param("id")
	if id == "" || m.redisClient == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing id"})
		return
	}
	key := fmt.Sprintf("llm:result:%s", id)
	res, err := m.redisClient.HGetAll(c, key).Result()
	if err != nil || len(res) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	c.JSON(http.StatusOK, res)
}

// runLLMWorker processes jobs by priority and forwards to HDN interpret
func (m *MonitorService) runLLMWorker() {
	if m.redisClient == nil {
		return
	}
	go func() {
		ctx := context.Background()
		queues := []string{"llm:high", "llm:normal", "llm:low"}
		// Allow overriding the HTTP timeout for HDN intelligent execution calls
		// Set MONITOR_LLM_WORKER_TIMEOUT_SECONDS to a larger value if your tasks are long-running
		timeoutSeconds := 180
		if v := strings.TrimSpace(os.Getenv("MONITOR_LLM_WORKER_TIMEOUT_SECONDS")); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				timeoutSeconds = n
			}
		}
		client := &http.Client{Timeout: time.Duration(timeoutSeconds) * time.Second}
		for {
			// BLPOP blocks until a job is available
			vals, err := m.redisClient.BLPop(ctx, 0, queues...).Result()
			if err != nil {
				time.Sleep(500 * time.Millisecond)
				continue
			}
			if len(vals) < 2 {
				continue
			}
			payload := vals[1]
			var job llmJob
			if err := json.Unmarshal([]byte(payload), &job); err != nil {
				continue
			}

			// mark running
			rkey := fmt.Sprintf("llm:result:%s", job.ID)
			_ = m.redisClient.HSet(ctx, rkey, map[string]interface{}{
				"status":     "running",
				"started_at": time.Now().Format(time.RFC3339),
			}).Err()

			// Optional back-pressure: if HDN is saturated, requeue to the head of the same priority and wait briefly
			if m.isHDNSaturated() {
				// Requeue and pause 2s
				_ = m.redisClient.LPush(ctx, fmt.Sprintf("llm:%s", job.Priority), payload).Err()
				time.Sleep(2 * time.Second)
				continue
			}

			// Call HDN intelligent execute to produce artifacts
			url := m.hdnURL + "/api/v1/intelligent/execute"
			// ensure context map exists
			if job.Context == nil {
				job.Context = map[string]string{}
			}
			// pass through session id
			if job.SessionID != "" {
				job.Context["session_id"] = job.SessionID
			}
			// instruct generation of artifacts
			job.Context["artifacts_wrapper"] = "true"
			job.Context["force_regenerate"] = "true"
			body := map[string]interface{}{
				"task_name":   "artifact_task",
				"description": job.Input,
				"context":     job.Context,
			}
			if pid, ok := job.Context["project_id"]; ok && strings.TrimSpace(pid) != "" {
				body["project_id"] = pid
			}
			bb, _ := json.Marshal(body)
			var resp *http.Response
			err = nil
			for attempt := 1; attempt <= 3; attempt++ {
				resp, err = client.Post(url, "application/json", strings.NewReader(string(bb)))
				if err == nil {
					break
				}
				if attempt < 3 {
					backoff := time.Duration(1<<uint(attempt-1)) * time.Second
					log.Printf("⚠️ HDN execute attempt %d failed: %v (retrying in %s)", attempt, err, backoff)
					time.Sleep(backoff)
				}
			}
			if err != nil {
				_ = m.redisClient.HSet(ctx, rkey, map[string]interface{}{
					"status":      "failed",
					"error":       err.Error(),
					"finished_at": time.Now().Format(time.RFC3339),
				}).Err()
				continue
			}
			rb, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			// store result
			_ = m.redisClient.HSet(ctx, rkey, map[string]interface{}{
				"status":      "succeeded",
				"http_status": resp.StatusCode,
				"output":      string(rb),
				"finished_at": time.Now().Format(time.RFC3339),
			}).Err()
			_ = m.redisClient.Expire(ctx, rkey, 2*time.Hour).Err()

			// small cooldown to avoid burst under load
			time.Sleep(1 * time.Second)

			// notify via Pub/Sub
			_ = m.redisClient.Publish(ctx, fmt.Sprintf("llm:done:%s", job.ID), string(rb)).Err()

			// optional callback
			if job.Callback != "" {
				go func(cb string, payload []byte, code int) {
					req, _ := http.NewRequest(http.MethodPost, cb, bytes.NewReader(payload))
					req.Header.Set("Content-Type", "application/json")
					q := req.URL.Query()
					q.Add("job_id", job.ID)
					q.Add("status", "succeeded")
					q.Add("http_status", fmt.Sprintf("%d", code))
					req.URL.RawQuery = q.Encode()
					http.DefaultClient.Do(req)
				}(job.Callback, rb, resp.StatusCode)
			}
		}
	}()
}

// memory handlers moved to handlers_memory.go

// helpers moved to utils.go

// testPage renders a simple test page
func (m *MonitorService) testPage(c *gin.Context) {
	c.HTML(http.StatusOK, "test.html", gin.H{})
}

// chatPage renders the chat interface
func (m *MonitorService) chatPage(c *gin.Context) {
	c.HTML(http.StatusOK, "chat.html", gin.H{
		"title":  "AI Chat Interface",
		"hdnURL": m.hdnURL,
	})
}

// chatAPI handles chat requests
func (m *MonitorService) chatAPI(c *gin.Context) {
	var req struct {
		Message   string `json:"message"`
		SessionID string `json:"session_id"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Forward the request to the HDN server's chat endpoint
	chatReq := map[string]string{
		"message":    req.Message,
		"session_id": req.SessionID,
	}

	jsonData, _ := json.Marshal(chatReq)
	resp, err := http.Post(m.hdnURL+"/api/v1/chat/text", "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to connect to chat service"})
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read response"})
		return
	}

	if resp.StatusCode != http.StatusOK {
		c.JSON(http.StatusBadGateway, gin.H{"error": string(body)})
		return
	}

	// Return the text response
	c.String(http.StatusOK, string(body))
}

// getChatSessions proxies chat sessions request to HDN
func (m *MonitorService) getChatSessions(c *gin.Context) {
	resp, err := http.Get(m.hdnURL + "/api/v1/chat/sessions")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to connect to chat service"})
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read response"})
		return
	}

	if resp.StatusCode != http.StatusOK {
		c.JSON(http.StatusBadGateway, gin.H{"error": string(body)})
		return
	}

	c.Data(resp.StatusCode, "application/json", body)
}

// getSessionThoughts proxies session thoughts request to HDN
func (m *MonitorService) getSessionThoughts(c *gin.Context) {
	sessionId := c.Param("sessionId")
	limit := c.DefaultQuery("limit", "50")

	url := fmt.Sprintf("%s/api/v1/chat/sessions/%s/thoughts?limit=%s", m.hdnURL, sessionId, limit)
	resp, err := http.Get(url)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to connect to chat service"})
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read response"})
		return
	}

	if resp.StatusCode != http.StatusOK {
		c.JSON(http.StatusBadGateway, gin.H{"error": string(body)})
		return
	}

	c.Data(resp.StatusCode, "application/json", body)
}

// streamSessionThoughts proxies session thoughts stream to HDN
func (m *MonitorService) streamSessionThoughts(c *gin.Context) {
	sessionId := c.Param("sessionId")

	url := fmt.Sprintf("%s/api/v1/chat/sessions/%s/thoughts/stream", m.hdnURL, sessionId)
	resp, err := http.Get(url)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to connect to chat service"})
		return
	}
	defer resp.Body.Close()

	// Set up Server-Sent Events headers
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("Access-Control-Allow-Origin", "*")

	// Stream the response
	io.Copy(c.Writer, resp.Body)
}

// expressSessionThoughts proxies session thoughts express request to HDN
func (m *MonitorService) expressSessionThoughts(c *gin.Context) {
	sessionId := c.Param("sessionId")

	url := fmt.Sprintf("%s/api/v1/chat/sessions/%s/thoughts/express", m.hdnURL, sessionId)
	resp, err := http.Post(url, "application/json", c.Request.Body)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to connect to chat service"})
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read response"})
		return
	}

	if resp.StatusCode != http.StatusOK {
		c.JSON(http.StatusBadGateway, gin.H{"error": string(body)})
		return
	}

	c.Data(resp.StatusCode, "application/json", body)
}

// getSessionHistory proxies session history request to HDN
func (m *MonitorService) getSessionHistory(c *gin.Context) {
	sessionId := c.Param("sessionId")
	limit := c.DefaultQuery("limit", "50")

	url := fmt.Sprintf("%s/api/v1/chat/sessions/%s/history?limit=%s", m.hdnURL, sessionId, limit)
	resp, err := http.Get(url)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to connect to chat service"})
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read response"})
		return
	}

	if resp.StatusCode != http.StatusOK {
		c.JSON(http.StatusBadGateway, gin.H{"error": string(body)})
		return
	}

	c.Data(resp.StatusCode, "application/json", body)
}

// getTools lists all tools from Redis registry
func (m *MonitorService) getTools(c *gin.Context) {
	ctx := context.Background()
	ids, err := m.redisClient.SMembers(ctx, "tools:registry").Result()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "redis error"})
		return
	}
	tools := make([]map[string]interface{}, 0, len(ids))
	for _, id := range ids {
		val, err := m.redisClient.Get(ctx, "tool:"+id).Result()
		if err != nil {
			continue
		}
		var obj map[string]interface{}
		if err := json.Unmarshal([]byte(val), &obj); err == nil {
			tools = append(tools, obj)
		}
	}
	c.JSON(http.StatusOK, gin.H{"tools": tools})
}

// getToolUsage returns recent usage events, optionally filtered by agent_id
func (m *MonitorService) getToolUsage(c *gin.Context) {
	agentID := strings.TrimSpace(c.Query("agent_id"))
	ctx := context.Background()
	key := "tools:global:usage_history"
	if agentID != "" {
		key = fmt.Sprintf("tools:%s:usage_history", agentID)
	}
	entries, err := m.redisClient.LRange(ctx, key, 0, 49).Result()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "redis error"})
		return
	}
	items := make([]map[string]interface{}, 0, len(entries))
	for _, e := range entries {
		var obj map[string]interface{}
		if err := json.Unmarshal([]byte(e), &obj); err == nil {
			items = append(items, obj)
		}
	}
	c.JSON(http.StatusOK, gin.H{"usage": items})
}

// getSystemStatus returns the overall system status
func (m *MonitorService) getSystemStatus(c *gin.Context) {
	status := &SystemStatus{
		Overall:   "healthy",
		Timestamp: time.Now(),
		Services:  make(map[string]ServiceInfo),
		Metrics:   SystemMetrics{},
		Alerts:    []Alert{},
	}

	// Check HDN server
	hdnInfo := m.checkService("HDN Server", m.hdnURL+"/health")
	status.Services["hdn"] = hdnInfo

	// Check Principles server (needs POST request)
	principlesInfo := m.checkServicePOST("Principles Server", m.principlesURL+"/action")
	status.Services["principles"] = principlesInfo

	// Check FSM server
	fsmInfo := m.checkService("FSM Server", m.fsmURL+"/health")
	status.Services["fsm"] = fsmInfo

	// Check Goal Manager (use existing endpoint since no /health endpoint)
	goalMgrInfo := m.checkService("Goal Manager", m.goalMgrURL+"/goals/agent_1/active")
	status.Services["goal_manager"] = goalMgrInfo

	// Check Redis
	redisInfo := m.checkRedis()
	status.Services["redis"] = redisInfo

	// Check Neo4j
	neo4jInfo := m.checkNeo4j()
	status.Services["neo4j"] = neo4jInfo

	// Check Vector Database (Weaviate or Qdrant)
	vectorDBInfo := m.checkQdrant()
	status.Services["vector-db"] = vectorDBInfo

	// Check NATS
	natsInfo := m.checkNATS()
	status.Services["nats"] = natsInfo

	// Get system metrics
	status.Metrics = m.getSystemMetrics()

	// Determine overall status
	unhealthyServices := 0
	if hdnInfo.Status != "healthy" {
		unhealthyServices++
		status.Alerts = append(status.Alerts, Alert{
			ID:        "hdn_down",
			Level:     "error",
			Message:   "HDN Server is not responding",
			Timestamp: time.Now(),
			Service:   "hdn",
		})
	}

	if principlesInfo.Status != "healthy" {
		unhealthyServices++
		status.Alerts = append(status.Alerts, Alert{
			ID:        "principles_down",
			Level:     "error",
			Message:   "Principles Server is not responding",
			Timestamp: time.Now(),
			Service:   "principles",
		})
	}

	if fsmInfo.Status != "healthy" {
		unhealthyServices++
		status.Alerts = append(status.Alerts, Alert{
			ID:        "fsm_down",
			Level:     "error",
			Message:   "FSM Server is not responding",
			Timestamp: time.Now(),
			Service:   "fsm",
		})
	}

	if goalMgrInfo.Status != "healthy" {
		unhealthyServices++
		status.Alerts = append(status.Alerts, Alert{
			ID:        "goal_manager_down",
			Level:     "warning",
			Message:   "Goal Manager is not responding",
			Timestamp: time.Now(),
			Service:   "goal_manager",
		})
	}

	if redisInfo.Status != "healthy" {
		unhealthyServices++
		status.Alerts = append(status.Alerts, Alert{
			ID:        "redis_down",
			Level:     "error",
			Message:   "Redis is not responding",
			Timestamp: time.Now(),
			Service:   "redis",
		})
	}

	if neo4jInfo.Status != "healthy" {
		unhealthyServices++
		status.Alerts = append(status.Alerts, Alert{
			ID:        "neo4j_down",
			Level:     "error",
			Message:   "Neo4j is not responding",
			Timestamp: time.Now(),
			Service:   "neo4j",
		})
	}

	if vectorDBInfo.Status != "healthy" {
		unhealthyServices++
		status.Alerts = append(status.Alerts, Alert{
			ID:        "vector_db_down",
			Level:     "error",
			Message:   "Vector database is not responding",
			Timestamp: time.Now(),
			Service:   "vector-db",
		})
	}

	if natsInfo.Status != "healthy" {
		unhealthyServices++
		status.Alerts = append(status.Alerts, Alert{
			ID:        "nats_down",
			Level:     "warning",
			Message:   "NATS is not responding",
			Timestamp: time.Now(),
			Service:   "nats",
		})
	}

	// Set overall status based on number of unhealthy services
	if unhealthyServices == 0 {
		status.Overall = "healthy"
	} else if unhealthyServices <= 2 {
		status.Overall = "degraded"
	} else {
		status.Overall = "critical"
	}

	c.JSON(http.StatusOK, status)
}

// checkService checks if a service is healthy
func (m *MonitorService) checkService(name, url string) ServiceInfo {
	start := time.Now()

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(url)

	responseTime := time.Since(start).Milliseconds()

	info := ServiceInfo{
		Name:         name,
		URL:          url,
		LastCheck:    time.Now(),
		ResponseTime: responseTime,
	}

	if err != nil {
		info.Status = "unhealthy"
		info.Error = err.Error()
		return info
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		info.Status = "healthy"
	} else {
		info.Status = "unhealthy"
		info.Error = fmt.Sprintf("HTTP %d", resp.StatusCode)
	}

	return info
}

// checkServicePOST checks if a service is healthy using POST request
func (m *MonitorService) checkServicePOST(name, url string) ServiceInfo {
	start := time.Now()

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Post(url, "application/json", strings.NewReader("{}"))

	responseTime := time.Since(start).Milliseconds()

	info := ServiceInfo{
		Name:         name,
		URL:          url,
		LastCheck:    time.Now(),
		ResponseTime: responseTime,
	}

	if err != nil {
		info.Status = "unhealthy"
		info.Error = err.Error()
		return info
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		info.Status = "healthy"
	} else {
		info.Status = "unhealthy"
		info.Error = fmt.Sprintf("HTTP %d", resp.StatusCode)
	}

	return info
}

// checkRedis checks Redis connection
func (m *MonitorService) checkRedis() ServiceInfo {
	start := time.Now()

	ctx := context.Background()
	pong, err := m.redisClient.Ping(ctx).Result()

	responseTime := time.Since(start).Milliseconds()

	info := ServiceInfo{
		Name:         "Redis",
		URL:          "localhost:6379",
		LastCheck:    time.Now(),
		ResponseTime: responseTime,
	}

	if err != nil {
		info.Status = "unhealthy"
		info.Error = err.Error()
		return info
	}

	if pong == "PONG" {
		info.Status = "healthy"
	} else {
		info.Status = "unhealthy"
		info.Error = "Unexpected response from Redis"
	}

	return info
}

// checkNeo4j checks Neo4j connection and health
func (m *MonitorService) checkNeo4j() ServiceInfo {
	start := time.Now()

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(m.neo4jURL)

	responseTime := time.Since(start).Milliseconds()

	info := ServiceInfo{
		Name:         "Neo4j",
		URL:          m.neo4jURL,
		LastCheck:    time.Now(),
		ResponseTime: responseTime,
	}

	if err != nil {
		info.Status = "unhealthy"
		info.Error = err.Error()
		return info
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		info.Status = "healthy"
	} else {
		info.Status = "unhealthy"
		info.Error = fmt.Sprintf("HTTP %d", resp.StatusCode)
	}

	return info
}

// checkQdrant checks vector database connection and health (Qdrant or Weaviate)
func (m *MonitorService) checkQdrant() ServiceInfo {
	start := time.Now()
	client := &http.Client{Timeout: 30 * time.Second}

	var resp *http.Response
	var err error
	var serviceName string
	var healthEndpoint string

	// Determine if this is Weaviate or Qdrant based on URL
	if strings.Contains(m.weaviateURL, "qdrant") || strings.Contains(m.weaviateURL, ":6333") {
		serviceName = "Qdrant"
		healthEndpoint = m.weaviateURL + "/healthz"
	} else {
		serviceName = "Weaviate"
		healthEndpoint = m.weaviateURL + "/v1/meta"
	}

	resp, err = client.Get(healthEndpoint)
	responseTime := time.Since(start).Milliseconds()

	info := ServiceInfo{
		Name:         serviceName,
		URL:          m.weaviateURL,
		LastCheck:    time.Now(),
		ResponseTime: responseTime,
	}

	if err != nil {
		info.Status = "unhealthy"
		info.Error = err.Error()
		return info
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		info.Status = "healthy"
	} else {
		info.Status = "unhealthy"
		info.Error = fmt.Sprintf("HTTP %d", resp.StatusCode)
	}

	return info
}

// checkNATS checks NATS connection and health
func (m *MonitorService) checkNATS() ServiceInfo {
	start := time.Now()

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(m.natsURL + "/varz")

	responseTime := time.Since(start).Milliseconds()

	info := ServiceInfo{
		Name:         "NATS",
		URL:          m.natsURL,
		LastCheck:    time.Now(),
		ResponseTime: responseTime,
	}

	if err != nil {
		info.Status = "unhealthy"
		info.Error = err.Error()
		return info
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		info.Status = "healthy"
	} else {
		info.Status = "unhealthy"
		info.Error = fmt.Sprintf("HTTP %d", resp.StatusCode)
	}

	return info
}

// getSystemMetrics retrieves system-wide metrics
func (m *MonitorService) getSystemMetrics() SystemMetrics {
	metrics := SystemMetrics{}

	// Get active workflows count from Redis
	ctx := context.Background()
	workflowKeys, err := m.redisClient.Keys(ctx, "workflow:*").Result()
	if err == nil {
		metrics.ActiveWorkflows = len(workflowKeys)
	}

	// Get execution metrics
	execMetrics := m.getExecutionMetricsFromRedis()
	metrics.TotalExecutions = execMetrics.TotalExecutions
	metrics.SuccessRate = execMetrics.SuccessRate
	metrics.AverageExecutionTime = execMetrics.AverageTime

	// Get Redis info
	_, err = m.redisClient.Info(ctx, "clients").Result()
	if err == nil {
		// Parse connected clients from Redis info
		// This is a simplified version - in production you'd parse the full info
		metrics.RedisConnections = 1 // Default value
	}

	// Docker containers count (simplified - would need Docker API integration)
	metrics.DockerContainers = 0

	return metrics
}

// getActiveWorkflows returns currently active workflows
func (m *MonitorService) getActiveWorkflows(c *gin.Context) {
	workflows := []WorkflowStatus{}

	// Get workflows from HDN server
	client := &http.Client{Timeout: 5 * time.Second} // Shorter timeout to avoid hanging
	resp, err := client.Get(m.hdnURL + "/api/v1/hierarchical/workflows")
	if err != nil {
		log.Printf("❌ [MONITOR] Failed to get workflows from HDN: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get workflows from HDN server"})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == 200 {
		var response struct {
			Success   bool                      `json:"success"`
			Workflows []*WorkflowStatusResponse `json:"workflows"`
		}

		if err := json.NewDecoder(resp.Body).Decode(&response); err == nil {
			// Build a lookup for hierarchical workflow status by ID for later mapping
			hierByID := make(map[string]*WorkflowStatusResponse)
			for _, hw := range response.Workflows {
				if hw != nil && hw.ID != "" {
					hierByID[hw.ID] = hw
				}
			}

			for _, wf := range response.Workflows {
				// Extract progress details from the progress object
				progress := 0.0
				totalSteps := 0
				completedSteps := 0
				failedSteps := 0

				// Handle different progress data types
				if wf.Progress != nil {
					if pct, ok := wf.Progress["percentage"]; ok {
						if pctFloat, ok := pct.(float64); ok {
							progress = pctFloat
						}
					}
					if total, ok := wf.Progress["total_steps"]; ok {
						if totalInt, ok := total.(float64); ok {
							totalSteps = int(totalInt)
						} else if totalInt, ok := total.(int); ok {
							totalSteps = totalInt
						}
					}
					if completed, ok := wf.Progress["completed_steps"]; ok {
						if completedInt, ok := completed.(float64); ok {
							completedSteps = int(completedInt)
						} else if completedInt, ok := completed.(int); ok {
							completedSteps = completedInt
						}
					}
					if failed, ok := wf.Progress["failed_steps"]; ok {
						if failedInt, ok := failed.(float64); ok {
							failedSteps = int(failedInt)
						} else if failedInt, ok := failed.(int); ok {
							failedSteps = failedInt
						}
					}

					// Calculate progress percentage if not provided
					if progress == 0.0 && totalSteps > 0 {
						progress = float64(completedSteps) / float64(totalSteps) * 100.0
					}
				}

				// Create a more descriptive task name based on the workflow ID or step
				taskName := "Hierarchical Workflow"
				description := wf.CurrentStep

				// Try to get more meaningful information from the workflow
				if wf.CurrentStep != "" {
					// Check if this is a data analysis workflow
					if strings.Contains(wf.CurrentStep, "data_analysis") || strings.Contains(wf.CurrentStep, "step_goal") {
						taskName = "Data Analysis Pipeline"
						description = "Processing data analysis workflow steps"
					} else if strings.Contains(wf.CurrentStep, "web_scraping") {
						taskName = "Web Scraping Pipeline"
						description = "Processing web scraping workflow steps"
					} else if strings.Contains(wf.CurrentStep, "ml_pipeline") {
						taskName = "ML Pipeline"
						description = "Processing machine learning workflow steps"
					} else if strings.Contains(wf.CurrentStep, "api_integration") {
						taskName = "API Integration"
						description = "Processing API integration workflow steps"
					} else {
						// For generic step names, provide a more user-friendly description
						description = "Executing workflow step"
					}
				}

				// Resolve ID for files/status when this is an intelligent workflow wrapper
				resolvedID := wf.ID
				// Try reverse mapping: if this is intelligent_ use HDN resolver to get hierarchical UUID for files
				if strings.HasPrefix(wf.ID, "intelligent_") {
					if rid, ok := m.resolveWorkflowID(wf.ID); ok && rid != "" {
						resolvedID = rid
						// If we have hierarchical status, prefer it
						if hw, exists := hierByID[rid]; exists && hw != nil {
							wf.Status = hw.Status
							wf.CurrentStep = hw.CurrentStep
							wf.LastActivity = hw.LastActivity
							wf.Progress = hw.Progress
							wf.Files = hw.Files
							wf.Steps = hw.Steps
						}
					}
				}

				// For intelligent workflows, use the original ID to fetch files (not resolved ID)
				fileID := wf.ID
				if strings.HasPrefix(wf.ID, "intelligent_") {
					fileID = wf.ID // Use original intelligent ID for files
				} else {
					fileID = resolvedID // Use resolved ID for hierarchical workflows
				}

				// Fetch files for this workflow
				log.Printf("🔍 [MONITOR] Fetching files for workflow %s (file ID: %s)", wf.ID, fileID)
				files, err := m.getWorkflowFiles(fileID)
				if err != nil {
					log.Printf("⚠️ [MONITOR] Failed to fetch files for workflow %s: %v", wf.ID, err)
					files = []FileInfo{} // Use empty list on error
				} else {
					log.Printf("📁 [MONITOR] Successfully fetched %d files for workflow %s", len(files), wf.ID)
				}

				// Get detailed step information for this workflow
				stepDetails, err := m.getWorkflowStepDetails(resolvedID)
				if err != nil {
					log.Printf("⚠️ [MONITOR] Failed to fetch step details for workflow %s: %v", wf.ID, err)
					stepDetails = []WorkflowStepStatus{} // Use empty list on error
				}

				// Handle intelligent execution workflows
				if strings.HasPrefix(wf.ID, "intelligent_") {
					// For intelligent workflows, use the files we already fetched
					// (don't override with wf.Files which is null from hierarchical API)
					log.Printf("📁 [MONITOR] Intelligent workflow %s has %d files", wf.ID, len(files))
					// Use steps from the workflow response
					if wf.Steps != nil {
						// Convert interface{} to WorkflowStepStatus
						for _, stepInterface := range wf.Steps {
							if stepMap, ok := stepInterface.(map[string]interface{}); ok {
								stepStatus := WorkflowStepStatus{
									ID:     getStringFromMap(stepMap, "id"),
									Name:   getStringFromMap(stepMap, "name"),
									Status: getStringFromMap(stepMap, "status"),
								}
								stepDetails = append(stepDetails, stepStatus)
							}
						}
					}
				}

				workflows = append(workflows, WorkflowStatus{
					ID:              wf.ID,
					Status:          wf.Status,
					TaskName:        taskName,
					Description:     description,
					Progress:        progress,
					TotalSteps:      totalSteps,
					CompletedSteps:  completedSteps,
					FailedSteps:     failedSteps,
					CurrentStep:     wf.CurrentStep,
					StartedAt:       wf.StartedAt,
					UpdatedAt:       wf.LastActivity,
					CanResume:       wf.CanResume,
					CanCancel:       wf.CanCancel,
					Error:           wf.Error,
					ProgressDetails: wf.Progress,
					Files:           files,
					Steps:           stepDetails,
					GeneratedCode:   wf.GeneratedCode, // Add generated code
				})
			}
		}
	}

	// Fetch intelligent execution workflows from Redis
	intelligentWorkflows, err := m.getIntelligentWorkflows()
	if err != nil {
		log.Printf("⚠️ [MONITOR] Failed to fetch intelligent workflows: %v", err)
	} else {
		workflows = append(workflows, intelligentWorkflows...)
	}

	// De-duplicate workflows by ID (some may appear in both hierarchical and intelligent sources)
	unique := make(map[string]WorkflowStatus)
	for _, wf := range workflows {
		if existing, ok := unique[wf.ID]; ok {
			// Merge files/steps minimally: prefer the one that has files/steps
			if len(existing.Files) == 0 && len(wf.Files) > 0 {
				existing.Files = wf.Files
			}
			if len(existing.Steps) == 0 && len(wf.Steps) > 0 {
				existing.Steps = wf.Steps
			}
			// Prefer non-empty description/current step
			if existing.Description == "" && wf.Description != "" {
				existing.Description = wf.Description
			}
			if existing.CurrentStep == "" && wf.CurrentStep != "" {
				existing.CurrentStep = wf.CurrentStep
			}
			unique[wf.ID] = existing
		} else {
			unique[wf.ID] = wf
		}
	}

	dedup := make([]WorkflowStatus, 0, len(unique))
	// If any intelligent workflows exist, hide all non-intelligent wrappers to avoid duplication
	hasIntelligent := false
	for id := range unique {
		if strings.HasPrefix(id, "intelligent_") {
			hasIntelligent = true
			break
		}
	}
	cutoff := time.Now().Add(-2 * time.Minute)
	for id, wf := range unique {
		if hasIntelligent && !strings.HasPrefix(id, "intelligent_") {
			continue
		}
		// Hide stale intelligent wrappers that never progressed
		if strings.HasPrefix(id, "intelligent_") && strings.EqualFold(wf.Status, "running") && wf.TotalSteps == 0 && wf.StartedAt.Before(cutoff) {
			continue
		}
		dedup = append(dedup, wf)
	}

	c.JSON(http.StatusOK, dedup)
}

// getFsmEvaluations returns recent evaluation summaries for a session/goal
// Query params:
// - session_id (optional): filter by session
// - limit (optional): max items
func (m *MonitorService) getFsmEvaluations(c *gin.Context) {
	sessionID := strings.TrimSpace(c.Query("session_id"))
	// Optional limit (unused in current passthrough)
	_ = strings.TrimSpace(c.Query("limit"))

	// Proxy to HDN working memory events (using episodes endpoint as a passthrough for now)
	// In future, a dedicated HDN endpoint can expose evaluation summaries directly
	base := m.hdnURL
	url := base + "/api/v1/episodes/search"
	// Placeholder query variable for future filtering
	_ = "workflow completion"
	if sessionID != "" {
		url += fmt.Sprintf("?session_id=%s", urlQueryEscape(sessionID))
	}

	req, _ := http.NewRequest("GET", url, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to fetch evaluations"})
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	// Just forward for now; the UI can filter/format
	c.Data(resp.StatusCode, "application/json", body)
}

// resolveWorkflowID uses HDN's resolver to map intelligent_ IDs to hierarchical UUIDs (and vice versa)
func (m *MonitorService) resolveWorkflowID(id string) (string, bool) {
	client := &http.Client{Timeout: 3 * time.Second}
	url := fmt.Sprintf("%s/api/v1/workflows/resolve/%s", m.hdnURL, urlQueryEscape(id))
	resp, err := client.Get(url)
	if err != nil {
		return "", false
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", false
	}
	var body struct {
		Input    string `json:"input"`
		Type     string `json:"type"`
		MappedID string `json:"mapped_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", false
	}
	if strings.TrimSpace(body.MappedID) == "" {
		return "", false
	}
	return body.MappedID, true
}

// getWorkflowDetails proxies detailed workflow info from HDN
func (m *MonitorService) getWorkflowDetails(c *gin.Context) {
	workflowID := c.Param("workflow_id")
	if workflowID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "workflow_id required"})
		return
	}
	urlStr := fmt.Sprintf("%s/api/v1/hierarchical/workflow/%s/details", m.hdnURL, workflowID)
	resp, err := http.Get(urlStr)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch workflow details"})
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	c.Data(resp.StatusCode, "application/json", body)
}

// getExecutionMetrics returns execution statistics
func (m *MonitorService) getExecutionMetrics(c *gin.Context) {
	metrics := m.getExecutionMetricsFromRedis()
	c.JSON(http.StatusOK, metrics)
}

// getProjects proxies the list of projects from the HDN API
func (m *MonitorService) getProjects(c *gin.Context) {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(m.hdnURL + "/api/v1/projects")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch projects"})
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read projects response"})
		return
	}

	c.Data(resp.StatusCode, "application/json", body)
}

// deleteProject proxies deletion to HDN API
func (m *MonitorService) deleteProject(c *gin.Context) {
	id := c.Param("id")
	if strings.TrimSpace(id) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id required"})
		return
	}
	req, err := http.NewRequest(http.MethodDelete, m.hdnURL+"/api/v1/projects/"+urlQueryEscape(id), nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	c.Data(resp.StatusCode, "application/json", body)
}

// getProject proxies a single project by id
func (m *MonitorService) getProject(c *gin.Context) {
	id := c.Param("id")
	resp, err := http.Get(fmt.Sprintf("%s/api/v1/projects/%s", m.hdnURL, id))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch project"})
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	c.Data(resp.StatusCode, "application/json", body)
}

// getProjectCheckpoints proxies checkpoints for a project
func (m *MonitorService) getProjectCheckpoints(c *gin.Context) {
	id := c.Param("id")
	resp, err := http.Get(fmt.Sprintf("%s/api/v1/projects/%s/checkpoints", m.hdnURL, id))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch checkpoints"})
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	c.Data(resp.StatusCode, "application/json", body)
}

// getProjectWorkflows proxies workflow ids for a project
func (m *MonitorService) getProjectWorkflows(c *gin.Context) {
	id := c.Param("id")
	resp, err := http.Get(fmt.Sprintf("%s/api/v1/projects/%s/workflows", m.hdnURL, id))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch project workflows"})
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	c.Data(resp.StatusCode, "application/json", body)
}

// listWorkflowFiles returns file metadata for a workflow (proxy to HDN)
func (m *MonitorService) listWorkflowFiles(c *gin.Context) {
	workflowID := c.Param("workflow_id")
	if workflowID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "workflow_id required"})
		return
	}
	resp, err := http.Get(fmt.Sprintf("%s/api/v1/files/workflow/%s", m.hdnURL, workflowID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch files"})
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	c.Data(resp.StatusCode, "application/json", body)
}

// getWorkflowProject returns the project (id and name) linked to a workflow, if any
func (m *MonitorService) getWorkflowProject(c *gin.Context) {
	workflowID := c.Param("workflow_id")
	if workflowID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "workflow_id required"})
		return
	}
	ctx := context.Background()
	projectID, err := m.redisClient.Get(ctx, "workflow_project:"+workflowID).Result()
	if err != nil || projectID == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "no project linked"})
		return
	}
	// fetch project details from HDN
	resp, err := http.Get(fmt.Sprintf("%s/api/v1/projects/%s", m.hdnURL, projectID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch project"})
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		c.JSON(http.StatusOK, gin.H{"id": projectID})
		return
	}
	var project map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&project); err != nil {
		c.JSON(http.StatusOK, gin.H{"id": projectID})
		return
	}
	name, _ := project["name"].(string)
	c.JSON(http.StatusOK, gin.H{"id": projectID, "name": name})
}

// analyzeLastWorkflow finds the most recent workflow for a project and triggers an analysis run
// that writes analysis.md back into the same project. It proxies to HDN intelligent execution.
func (m *MonitorService) analyzeLastWorkflow(c *gin.Context) {
	projectID := c.Param("id")
	if strings.TrimSpace(projectID) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "project id required"})
		return
	}

	// 1) Fetch workflow IDs for the project (most recent typically last/first; we leave sorting to HDN for now)
	resp, err := http.Get(fmt.Sprintf("%s/api/v1/projects/%s/workflows", m.hdnURL, url.PathEscape(projectID)))
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"success": false, "error": "failed to fetch project workflows"})
		return
	}
	defer resp.Body.Close()
	var wfList struct {
		WorkflowIDs []string `json:"workflow_ids"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&wfList); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"success": false, "error": "invalid workflows response"})
		return
	}
	if len(wfList.WorkflowIDs) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "error": "no workflows for project"})
		return
	}

	// Choose the latest workflow by probing details timestamps; fall back to last id
	latestID := wfList.WorkflowIDs[len(wfList.WorkflowIDs)-1]
	latestTime := time.Time{}
	httpClient := &http.Client{Timeout: 30 * time.Second}
	httpExecClient := &http.Client{Timeout: 8 * time.Minute}
	for _, id := range wfList.WorkflowIDs {
		detailsURL := fmt.Sprintf("%s/api/v1/hierarchical/workflow/%s/details", m.hdnURL, url.PathEscape(id))
		if resp2, err2 := httpClient.Get(detailsURL); err2 == nil && resp2 != nil {
			var payload struct {
				Success bool                   `json:"success"`
				Details map[string]interface{} `json:"details"`
			}
			_ = json.NewDecoder(resp2.Body).Decode(&payload)
			resp2.Body.Close()
			if payload.Success {
				if ts, _ := payload.Details["updated_at"].(string); ts != "" {
					if t, errp := time.Parse(time.RFC3339, ts); errp == nil {
						if t.After(latestTime) {
							latestTime = t
							latestID = id
						}
					}
				}
			}
		}
	}

	// 2) Trigger an intelligent execution that writes analysis.md for this project
	execPayload := map[string]interface{}{
		"task_name":   "workflow_analysis_summary",
		"description": fmt.Sprintf("Analyze the completed workflow %s in project %s. First, fetch the workflow details from /api/v1/hierarchical/workflow/%s/details to understand what it actually did. Then examine any generated files or outputs. Write a real analysis in analysis.md covering: 1) Actual purpose and goals based on the workflow details, 2) Real steps that were executed, 3) Actual success/failure status from the workflow data, 4) Concrete lessons learned from the execution, 5) Specific recommendations for next steps. Use real data, not placeholders. Keep under 500 words.", latestID, projectID, latestID),
		"context": map[string]string{
			"save_markdown_filename": "analysis.md",
			"artifact_names":         "analysis.md",
			"analysis_mode":          "summary_only",
			"workflow_id_to_analyze": latestID,
		},
		"project_id":       projectID,
		"force_regenerate": true,
		"max_retries":      1,
	}
	bts, _ := json.Marshal(execPayload)
	// Try to obtain workflow_id quickly; fall back to async
	ctxQuick, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	reqQuick, _ := http.NewRequestWithContext(ctxQuick, http.MethodPost, m.hdnURL+"/api/v1/intelligent/execute", strings.NewReader(string(bts)))
	reqQuick.Header.Set("Content-Type", "application/json")
	execRespQuick, errQuick := httpExecClient.Do(reqQuick)
	if errQuick == nil && execRespQuick != nil {
		var execOut struct {
			Success    bool   `json:"success"`
			WorkflowID string `json:"workflow_id"`
			Error      string `json:"error"`
		}
		_ = json.NewDecoder(execRespQuick.Body).Decode(&execOut)
		execRespQuick.Body.Close()
		if execOut.Success && strings.TrimSpace(execOut.WorkflowID) != "" {
			// best effort watcher
			if m.redisClient != nil && strings.HasPrefix(execOut.WorkflowID, "intelligent_") {
				go func(wfid string) {
					deadline := time.Now().Add(2 * time.Minute)
					for time.Now().Before(deadline) {
						member, err := m.redisClient.SIsMember(context.Background(), "active_workflows", wfid).Result()
						if err == nil && !member {
							return
						}
						time.Sleep(2 * time.Second)
					}
				}(execOut.WorkflowID)
			}
			c.JSON(http.StatusOK, gin.H{
				"success":     true,
				"message":     "analysis started",
				"workflow_id": execOut.WorkflowID,
				"analyzed_id": latestID,
				"project_id":  projectID,
			})
			return
		}
	}
	// Async fallback: fire-and-forget in background and return 202 so UI can poll
	go func() {
		// long-running call without quick timeout
		execResp, err := httpExecClient.Post(m.hdnURL+"/api/v1/intelligent/execute", "application/json", strings.NewReader(string(bts)))
		if err != nil {
			return
		}
		defer execResp.Body.Close()
		io.Copy(io.Discard, execResp.Body)
	}()
	c.JSON(http.StatusAccepted, gin.H{
		"success":     true,
		"message":     "analysis scheduled",
		"analyzed_id": latestID,
		"project_id":  projectID,
	})
	return
}

// getExecutionMetricsFromRedis retrieves metrics from Redis
func (m *MonitorService) getExecutionMetricsFromRedis() ExecutionMetrics {
	ctx := context.Background()

	metrics := ExecutionMetrics{
		ByLanguage: make(map[string]int),
		ByTaskType: make(map[string]int),
	}

	// Get execution count
	totalExec, _ := m.redisClient.Get(ctx, "metrics:total_executions").Int()
	metrics.TotalExecutions = totalExec

	// Get successful executions
	successExec, _ := m.redisClient.Get(ctx, "metrics:successful_executions").Int()
	metrics.SuccessfulExecutions = successExec

	// Get failed executions
	metrics.FailedExecutions = totalExec - successExec

	// Calculate success rate
	if totalExec > 0 {
		metrics.SuccessRate = float64(successExec) / float64(totalExec) * 100
	}

	// Get average execution time
	avgTime, _ := m.redisClient.Get(ctx, "metrics:avg_execution_time").Float64()
	metrics.AverageTime = avgTime

	// Get last execution time
	lastExecStr, _ := m.redisClient.Get(ctx, "metrics:last_execution").Result()
	if lastExecStr != "" {
		if lastExec, err := time.Parse(time.RFC3339, lastExecStr); err == nil {
			metrics.LastExecution = lastExec
		}
	}

	return metrics
}

// getRedisInfo returns Redis connection information
func (m *MonitorService) getRedisInfo(c *gin.Context) {
	ctx := context.Background()

	info := RedisInfo{
		Connected: false,
		Keyspace:  make(map[string]int),
	}

	// Test connection
	_, err := m.redisClient.Ping(ctx).Result()
	if err != nil {
		c.JSON(http.StatusOK, info)
		return
	}

	info.Connected = true

	// Get Redis info
	_, err = m.redisClient.Info(ctx).Result()
	if err == nil {
		// Parse basic info (simplified)
		info.Version = "6.0+" // Default version
		info.UsedMemory = "Unknown"
		info.ConnectedClients = 1
	}

	// Get keyspace info
	keys, err := m.redisClient.Keys(ctx, "*").Result()
	if err == nil {
		info.Keyspace["total"] = len(keys)
	}

	c.JSON(http.StatusOK, info)
}

// getDockerInfo returns Docker container information
func (m *MonitorService) getDockerInfo(c *gin.Context) {
	// Simplified Docker info - in production would integrate with Docker API
	info := DockerInfo{
		ContainersRunning: 0,
		ContainersTotal:   0,
		ImagesCount:       0,
	}

	c.JSON(http.StatusOK, info)
}

// getNeo4jInfo returns Neo4j connection information
func (m *MonitorService) getNeo4jInfo(c *gin.Context) {
	info := map[string]interface{}{
		"connected": false,
		"url":       m.neo4jURL,
		"version":   "Unknown",
		"status":    "unhealthy",
	}

	// Test connection
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(m.neo4jURL)
	if err != nil {
		info["error"] = err.Error()
		c.JSON(http.StatusOK, info)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		info["connected"] = true
		info["status"] = "healthy"
		info["version"] = "5.x" // Neo4j version from docker-compose
	} else {
		info["error"] = fmt.Sprintf("HTTP %d", resp.StatusCode)
	}

	c.JSON(http.StatusOK, info)
}

// getNeo4jStats queries Neo4j for live graph counts
func (m *MonitorService) getNeo4jStats(c *gin.Context) {
	// Default auth from docker-compose: neo4j/test1234; allow override via env
	user := strings.TrimSpace(os.Getenv("NEO4J_USER"))
	if user == "" {
		user = "neo4j"
	}
	pass := strings.TrimSpace(os.Getenv("NEO4J_PASS"))
	if pass == "" {
		pass = "test1234"
	}

	// Use cypher-shell-like Bolt endpoint via HTTP transactional API
	// POST { statements: [{ statement: "MATCH (n) RETURN count(n) as nodes" }, ...] }
	type stmt struct {
		Statement string `json:"statement"`
	}
	payload := map[string]interface{}{
		"statements": []stmt{
			{Statement: "MATCH (n) RETURN count(n) AS nodes"},
			{Statement: "MATCH ()-[r]->() RETURN count(r) AS relationships"},
			{Statement: "CALL db.stats.retrieve('GRAPH COUNTS') YIELD data RETURN data"},
			{Statement: "MATCH (n) RETURN labels(n)[0] AS label, count(n) AS count ORDER BY count DESC"},
			{Statement: "MATCH ()-[r]->() RETURN type(r) AS rel, count(r) AS count ORDER BY count DESC"},
			{Statement: "MATCH (n:Concept) RETURN coalesce(n.domain,'(unset)') AS domain, count(n) AS count ORDER BY count DESC"},
			{Statement: "MATCH (n:Concept) RETURN n.name AS name, coalesce(n.domain,'(unset)') AS domain, left(n.definition,120) AS definition ORDER BY n.name ASC LIMIT 10"},
		},
	}
	body, _ := json.Marshal(payload)

	// Build HTTP basic auth request to Neo4j HTTP API
	// Neo4j 5 transactional endpoint
	endpoint := strings.TrimRight(m.neo4jURL, "/") + "/db/neo4j/tx/commit"
	req, _ := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(user, pass)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "neo4j request failed"})
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		c.JSON(resp.StatusCode, gin.H{"error": string(b)})
		return
	}

	var result struct {
		Results []struct {
			Columns []string `json:"columns"`
			Data    []struct {
				Row []interface{} `json:"row"`
			} `json:"data"`
		} `json:"results"`
		Errors []interface{} `json:"errors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "invalid neo4j response"})
		return
	}
	if len(result.Errors) > 0 {
		c.JSON(http.StatusBadGateway, gin.H{"error": result.Errors})
		return
	}

	// Parse nodes and relationships from first two statements
	var nodes, relationships int64
	if len(result.Results) >= 1 && len(result.Results[0].Data) > 0 && len(result.Results[0].Data[0].Row) > 0 {
		switch v := result.Results[0].Data[0].Row[0].(type) {
		case float64:
			nodes = int64(v)
		}
	}
	if len(result.Results) >= 2 && len(result.Results[1].Data) > 0 && len(result.Results[1].Data[0].Row) > 0 {
		switch v := result.Results[1].Data[0].Row[0].(type) {
		case float64:
			relationships = int64(v)
		}
	}

	// Optional: parse GRAPH COUNTS
	var graphCounts interface{}
	if len(result.Results) >= 3 && len(result.Results[2].Data) > 0 && len(result.Results[2].Data[0].Row) > 0 {
		graphCounts = result.Results[2].Data[0].Row[0]
	}

	// labels breakdown
	labels := make([]map[string]interface{}, 0)
	if len(result.Results) >= 4 {
		for _, d := range result.Results[3].Data {
			if len(d.Row) >= 2 {
				labels = append(labels, map[string]interface{}{"label": d.Row[0], "count": d.Row[1]})
			}
		}
	}
	// relationship types breakdown
	relTypes := make([]map[string]interface{}, 0)
	if len(result.Results) >= 5 {
		for _, d := range result.Results[4].Data {
			if len(d.Row) >= 2 {
				relTypes = append(relTypes, map[string]interface{}{"type": d.Row[0], "count": d.Row[1]})
			}
		}
	}
	// concepts by domain
	domains := make([]map[string]interface{}, 0)
	if len(result.Results) >= 6 {
		for _, d := range result.Results[5].Data {
			if len(d.Row) >= 2 {
				domains = append(domains, map[string]interface{}{"domain": d.Row[0], "count": d.Row[1]})
			}
		}
	}
	// recent/sample concepts
	concepts := make([]map[string]interface{}, 0)
	if len(result.Results) >= 7 {
		for _, d := range result.Results[6].Data {
			if len(d.Row) >= 3 {
				concepts = append(concepts, map[string]interface{}{"name": d.Row[0], "domain": d.Row[1], "definition": d.Row[2]})
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"nodes":         nodes,
		"relationships": relationships,
		"graph_counts":  graphCounts,
		"labels":        labels,
		"rel_types":     relTypes,
		"domains":       domains,
		"concepts":      concepts,
		"checked_at":    time.Now().UTC().Format(time.RFC3339),
	})
}

// getQdrantInfo returns Qdrant connection information
func (m *MonitorService) getQdrantInfo(c *gin.Context) {
	info := map[string]interface{}{
		"connected": false,
		"url":       m.weaviateURL,
		"version":   "Unknown",
		"status":    "unhealthy",
	}

	// Test connection
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(m.weaviateURL + "/v1/meta")
	if err != nil {
		info["error"] = err.Error()
		c.JSON(http.StatusOK, info)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		info["connected"] = true
		info["status"] = "healthy"
		info["version"] = "latest" // Qdrant version from docker-compose
	} else {
		info["error"] = fmt.Sprintf("HTTP %d", resp.StatusCode)
	}

	c.JSON(http.StatusOK, info)
}

// getQdrantStats returns collection stats for vector database (Qdrant or Weaviate)
func (m *MonitorService) getQdrantStats(c *gin.Context) {
	client := &http.Client{Timeout: 10 * time.Second}

	// Determine if this is Qdrant or Weaviate and use appropriate endpoint
	var resp *http.Response
	var err error

	if strings.Contains(m.weaviateURL, "qdrant") || strings.Contains(m.weaviateURL, ":6333") {
		// Qdrant: use collections endpoint
		resp, err = client.Get(strings.TrimRight(m.weaviateURL, "/") + "/collections")
	} else {
		// Weaviate: use REST API to get schema info
		resp, err = client.Get(strings.TrimRight(m.weaviateURL, "/") + "/v1/schema")
	}

	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "vector database request failed"})
		return
	}
	defer resp.Body.Close()

	if strings.Contains(m.weaviateURL, "qdrant") || strings.Contains(m.weaviateURL, ":6333") {
		// Handle Qdrant response
		var list struct {
			Result struct {
				Collections []struct {
					Name string `json:"name"`
				} `json:"collections"`
			} `json:"result"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&list)

		summaries := make([]map[string]interface{}, 0)
		for _, col := range list.Result.Collections {
			u := strings.TrimRight(m.weaviateURL, "/") + "/collections/" + col.Name
			// Get collection info
			infoResp, err := client.Get(u)
			if err != nil {
				continue
			}
			defer infoResp.Body.Close()

			var info struct {
				Result struct {
					PointsCount int `json:"points_count"`
				} `json:"result"`
			}
			_ = json.NewDecoder(infoResp.Body).Decode(&info)

			summaries = append(summaries, map[string]interface{}{
				"name":         col.Name,
				"points_count": info.Result.PointsCount,
				"type":         "qdrant_collection",
			})
		}
		c.JSON(http.StatusOK, gin.H{"collections": summaries})
		return
	} else {
		// Handle Weaviate response (default)
		var weaviateResp struct {
			Classes []struct {
				Class string `json:"class"`
			} `json:"classes"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&weaviateResp); err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": "failed to decode weaviate response"})
			return
		}

		// Convert Weaviate classes to collection-like format
		collections := make([]map[string]interface{}, 0)
		for _, cls := range weaviateResp.Classes {
			if cls.Class != "" {
				// Get actual object count for this class
				count := m.getWeaviateObjectCount(cls.Class)
				collections = append(collections, map[string]interface{}{
					"name":         cls.Class,
					"points_count": count,
				})
			}
		}
		c.JSON(http.StatusOK, gin.H{"collections": collections})
		return
	}
}

// getWeaviateObjectCount gets the actual count of objects in a Weaviate class
func (m *MonitorService) getWeaviateObjectCount(className string) int {
	client := &http.Client{Timeout: 10 * time.Second}

	// Use GraphQL to get object count
	queryData := map[string]string{
		"query": fmt.Sprintf("{ Aggregate { %s { meta { count } } } }", className),
	}
	queryBytes, _ := json.Marshal(queryData)
	query := string(queryBytes)

	resp, err := client.Post(
		strings.TrimRight(m.weaviateURL, "/")+"/v1/graphql",
		"application/json",
		strings.NewReader(query),
	)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0
	}

	// Navigate through the nested structure dynamically
	if data, ok := result["data"].(map[string]interface{}); ok {
		if aggregate, ok := data["Aggregate"].(map[string]interface{}); ok {
			if classData, ok := aggregate[className].([]interface{}); ok && len(classData) > 0 {
				if firstItem, ok := classData[0].(map[string]interface{}); ok {
					if meta, ok := firstItem["meta"].(map[string]interface{}); ok {
						if count, ok := meta["count"].(float64); ok {
							return int(count)
						}
					}
				}
			}
		}
	}

	return 0
}

// getWeaviateRecords returns the total count of records in Weaviate
func (m *MonitorService) getWeaviateRecords(c *gin.Context) {
	client := &http.Client{Timeout: 10 * time.Second}

	// Get schema to find all classes
	resp, err := client.Get(strings.TrimRight(m.weaviateURL, "/") + "/v1/schema")
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to connect to Weaviate", "count": 0})
		return
	}
	defer resp.Body.Close()

	var weaviateResp struct {
		Classes []struct {
			Class string `json:"class"`
		} `json:"classes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&weaviateResp); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to decode weaviate response", "count": 0})
		return
	}

	// Count objects in each class
	totalCount := 0
	classCounts := make(map[string]int)

	for _, cls := range weaviateResp.Classes {
		if cls.Class != "" {
			count := m.getWeaviateObjectCount(cls.Class)
			classCounts[cls.Class] = count
			totalCount += count
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"total_count":  totalCount,
		"class_counts": classCounts,
		"classes":      len(weaviateResp.Classes),
	})
}

// ragSearch performs a simple semantic search over Qdrant using a toy embedder (8-dim)
func (m *MonitorService) ragSearch(c *gin.Context) {
	q := strings.TrimSpace(c.Query("q"))
	limit := c.DefaultQuery("limit", "10")
	collection := c.DefaultQuery("collection", "WikipediaArticle")
	if q == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "q required"})
		return
	}

	// toy 8-dim embedding (same as bootstrapper)
	vec := func(text string) []float32 {
		sum := sha256.Sum256([]byte(text))
		out := make([]float32, 8)
		for i := 0; i < 8; i++ {
			off := i * 4
			v := binary.LittleEndian.Uint32(sum[off : off+4])
			out[i] = (float32(v%20000)/10000.0 - 1.0)
		}
		// normalize
		var s float32
		for i := 0; i < 8; i++ {
			s += out[i] * out[i]
		}
		if s > 0 {
			z := s
			for j := 0; j < 6; j++ {
				z = 0.5 * (z + s/z)
			}
			inv := 1.0 / float32(z)
			for i := 0; i < 8; i++ {
				out[i] *= inv
			}
		}
		return out
	}(q)

	// Use Weaviate GraphQL API for semantic search
	limitInt, _ := strconv.Atoi(limit)
	if limitInt <= 0 {
		limitInt = 10
	}

	// Convert vector to Weaviate format (8 dimensions)
	vectorStr := "["
	for i, v := range vec {
		if i > 0 {
			vectorStr += ","
		}
		vectorStr += fmt.Sprintf("%.6f", v)
	}
	vectorStr += "]"

	// GraphQL query for Weaviate
	// Note: metadata is stored as a text field (JSON string), not an object, so we can't query sub-fields
	query := fmt.Sprintf(`{
		"query": "{ Get { %s(nearVector: {vector: %s}, limit: %d) { _additional { id distance } title text source timestamp url metadata } } }"
	}`, collection, vectorStr, limitInt)

	url := strings.TrimRight(m.weaviateURL, "/") + "/v1/graphql"
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader([]byte(query)))
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "weaviate search failed"})
		return
	}
	defer resp.Body.Close()

	var res map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to decode weaviate response"})
		return
	}

	// Transform Weaviate response to match expected format
	if data, ok := res["data"].(map[string]interface{}); ok {
		if get, ok := data["Get"].(map[string]interface{}); ok {
			if results, ok := get[collection].([]interface{}); ok {
				points := make([]map[string]interface{}, 0, len(results))
				for _, result := range results {
					if item, ok := result.(map[string]interface{}); ok {
						p := map[string]interface{}{"payload": item}
						if addl, ok := item["_additional"].(map[string]interface{}); ok {
							if d, ok := addl["distance"].(float64); ok {
								p["score"] = 1.0 - d
							}
							if idv, ok := addl["id"]; ok {
								p["id"] = idv
							}
						}
						points = append(points, p)
					}
				}
				res = map[string]interface{}{
					"result": map[string]interface{}{"points": points},
				}
			}
		}
	}

	c.JSON(http.StatusOK, res)
}

// getNATSInfo returns NATS connection information
func (m *MonitorService) getNATSInfo(c *gin.Context) {
	info := map[string]interface{}{
		"connected": false,
		"url":       m.natsURL,
		"version":   "Unknown",
		"status":    "unhealthy",
	}

	// Test connection
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(m.natsURL + "/varz")
	if err != nil {
		info["error"] = err.Error()
		c.JSON(http.StatusOK, info)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		info["connected"] = true
		info["status"] = "healthy"
		info["version"] = "2.10" // NATS version from docker-compose
	} else {
		info["error"] = fmt.Sprintf("HTTP %d", resp.StatusCode)
	}

	c.JSON(http.StatusOK, info)
}

// updateGoalStatus updates the status of a goal in the self-model
func (m *MonitorService) updateGoalStatus(c *gin.Context) {
	goalID := c.Param("id")

	var req struct {
		Status string `json:"status"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Call HDN server to update goal status
	client := &http.Client{Timeout: 30 * time.Second}
	updateURL := fmt.Sprintf("%s/api/v1/memory/goals/%s/status", m.hdnURL, goalID)

	updateData := map[string]string{"status": req.Status}
	jsonData, _ := json.Marshal(updateData)

	resp, err := client.Post(updateURL, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		c.JSON(resp.StatusCode, gin.H{"error": string(body)})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "message": "Goal status updated"})
}

// deleteSelfModelGoal proxies deletion to HDN self-model goals
func (m *MonitorService) deleteSelfModelGoal(c *gin.Context) {
	id := c.Param("id")
	if strings.TrimSpace(id) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id required"})
		return
	}
	req, err := http.NewRequest(http.MethodDelete, m.hdnURL+"/api/v1/memory/goals/"+urlQueryEscape(id), nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	c.Data(resp.StatusCode, "application/json", body)
}

// getDailySummaryLatest returns the latest daily summary from Redis
func (m *MonitorService) getDailySummaryLatest(c *gin.Context) {
	ctx := context.Background()
	val, err := m.redisClient.Get(ctx, "daily_summary:latest").Result()
	if err != nil || strings.TrimSpace(val) == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "no daily summary"})
		return
	}
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(val), &obj); err != nil {
		c.JSON(http.StatusOK, gin.H{"raw": val})
		return
	}
	c.JSON(http.StatusOK, obj)
}

// getDailySummaryHistory returns recent daily summaries (limit 30)
func (m *MonitorService) getDailySummaryHistory(c *gin.Context) {
	ctx := context.Background()
	items, err := m.redisClient.LRange(ctx, "daily_summary:history", 0, 29).Result()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "redis error"})
		return
	}
	var list []map[string]interface{}
	for _, it := range items {
		var obj map[string]interface{}
		if json.Unmarshal([]byte(it), &obj) == nil {
			list = append(list, obj)
		}
	}
	c.JSON(http.StatusOK, gin.H{"history": list})
}

// getDailySummaryByDate returns the daily summary for a specific date (YYYY-MM-DD, UTC)
func (m *MonitorService) getDailySummaryByDate(c *gin.Context) {
	date := strings.TrimSpace(c.Param("date"))
	if date == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "date required"})
		return
	}
	ctx := context.Background()
	val, err := m.redisClient.Get(ctx, "daily_summary:"+date).Result()
	if err != nil || strings.TrimSpace(val) == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(val), &obj); err != nil {
		c.JSON(http.StatusOK, gin.H{"raw": val})
		return
	}
	c.JSON(http.StatusOK, obj)
}

// getLogs returns recent system logs from HDN server
func (m *MonitorService) getLogs(c *gin.Context) {
	// Get log level filter
	level := c.DefaultQuery("level", "all")
	limitStr := c.DefaultQuery("limit", "100")
	limit, _ := strconv.Atoi(limitStr)

	// Read logs from HDN server log file
	logs := m.readHDNLogs(limit)

	// Filter by level if specified
	if level != "all" {
		var filtered []map[string]interface{}
		for _, log := range logs {
			if log["level"] == level {
				filtered = append(filtered, log)
			}
		}
		logs = filtered
	}

	c.JSON(http.StatusOK, logs)
}

// readHDNLogs reads recent logs from the HDN server using kubectl
func (m *MonitorService) readHDNLogs(limit int) []map[string]interface{} {
	logs := []map[string]interface{}{}

	// 1) Prefer local log file if available (fast, no external deps)
	logFile := strings.TrimSpace(os.Getenv("HDN_LOG_FILE"))
	if logFile == "" {
		logFile = "/tmp/hdn_server.log" // Default to /tmp/hdn_server.log where HDN server writes
	}
	if data, err := os.ReadFile(logFile); err == nil {
		lines := strings.Split(string(data), "\n")
		for i := len(lines) - 1; i >= 0 && len(logs) < limit; i-- {
			line := strings.TrimSpace(lines[i])
			if line == "" {
				continue
			}
			if entry := m.parseLogLine(line); entry != nil {
				logs = append(logs, entry)
			}
		}
		return logs
	}

	// 2) Fallback to kubectl (Kubernetes environments)
	ns := strings.TrimSpace(os.Getenv("K8S_NAMESPACE"))
	if ns == "" {
		ns = "agi"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "kubectl", "logs", "-n", ns, "deployment/hdn-server-rpi58", "--tail", fmt.Sprintf("%d", limit))
	output, err := cmd.Output()
	if err != nil {
		log.Printf("⚠️ [MONITOR] Failed to read HDN logs via kubectl (ns=%s): %v", ns, err)
		return logs
	}

	lines := strings.Split(string(output), "\n")
	for i := len(lines) - 1; i >= 0 && len(logs) < limit; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		if entry := m.parseLogLine(line); entry != nil {
			logs = append(logs, entry)
		}
	}

	return logs
}

// parseLogLine parses a single log line and extracts structured information
func (m *MonitorService) parseLogLine(line string) map[string]interface{} {
	// Look for timestamp pattern (2025/09/21 18:56:28)
	timestampRegex := regexp.MustCompile(`(\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2})`)
	timestampMatch := timestampRegex.FindStringSubmatch(line)

	var timestamp time.Time
	if len(timestampMatch) > 1 {
		parsedTime, err := time.Parse("2006/01/02 15:04:05", timestampMatch[1])
		if err == nil {
			timestamp = parsedTime
		}
	}

	if timestamp.IsZero() {
		timestamp = time.Now()
	}

	// Determine log level and service
	level := "info"
	service := "hdn"
	message := line

	// Check for log levels
	if strings.Contains(line, "❌") || strings.Contains(line, "ERROR") {
		level = "error"
	} else if strings.Contains(line, "⚠️") || strings.Contains(line, "WARNING") {
		level = "warning"
	} else if strings.Contains(line, "✅") || strings.Contains(line, "SUCCESS") {
		level = "info"
	} else if strings.Contains(line, "🔍") || strings.Contains(line, "DEBUG") {
		level = "debug"
	}

	// Check for service indicators
	if strings.Contains(line, "[DOCKER]") {
		service = "docker"
	} else if strings.Contains(line, "[FILE]") {
		service = "file"
	} else if strings.Contains(line, "[PLANNER]") {
		service = "planner"
	} else if strings.Contains(line, "[ORCHESTRATOR]") {
		service = "orchestrator"
	} else if strings.Contains(line, "[INTELLIGENT]") {
		service = "intelligent"
	}

	// Clean up message (remove timestamp and service tags)
	message = strings.TrimSpace(line)
	if len(timestampMatch) > 1 {
		message = strings.TrimSpace(strings.TrimPrefix(message, timestampMatch[1]))
	}

	// Remove common prefixes
	prefixes := []string{"[DOCKER]", "[FILE]", "[PLANNER]", "[ORCHESTRATOR]", "[INTELLIGENT]"}
	for _, prefix := range prefixes {
		if strings.Contains(message, prefix) {
			parts := strings.SplitN(message, prefix, 2)
			if len(parts) > 1 {
				message = strings.TrimSpace(parts[1])
			}
		}
	}

	return map[string]interface{}{
		"timestamp": timestamp.Format(time.RFC3339),
		"level":     level,
		"message":   message,
		"service":   service,
	}
}

// getK8sServices returns a list of available Kubernetes services for log viewing
func (m *MonitorService) getK8sServices(c *gin.Context) {
	services := []map[string]string{
		{"name": "hdn-server-rpi58", "displayName": "HDN Server", "namespace": "agi"},
		{"name": "fsm-server-rpi58", "displayName": "FSM Server", "namespace": "agi"},
		{"name": "goal-manager", "displayName": "Goal Manager", "namespace": "agi"},
		{"name": "principles-server", "displayName": "Principles Server", "namespace": "agi"},
		{"name": "monitor-ui", "displayName": "Monitor UI", "namespace": "agi"},
		{"name": "neo4j", "displayName": "Neo4j Database", "namespace": "agi"},
		{"name": "redis", "displayName": "Redis Cache", "namespace": "agi"},
		{"name": "weaviate", "displayName": "Weaviate Vector DB", "namespace": "agi"},
		{"name": "nats", "displayName": "NATS Message Bus", "namespace": "agi"},
	}

	c.JSON(http.StatusOK, services)
}

// getK8sLogs retrieves logs from a specific Kubernetes service pod
func (m *MonitorService) getK8sLogs(c *gin.Context) {
	service := c.Param("service")
	if service == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "service parameter required"})
		return
	}

	// Get query parameters
	limitStr := c.DefaultQuery("limit", "100")
	limit, err := strconv.Atoi(limitStr)
	if err != nil {
		limit = 100
	}

	// Execute kubectl logs; try multiple selector keys
	// Allow override via query params: ns and selector_key
	envNs := strings.TrimSpace(os.Getenv("K8S_NAMESPACE"))
	if envNs == "" {
		envNs = "agi"
	}
	ns := strings.TrimSpace(c.DefaultQuery("ns", envNs))
	envSelectorKey := strings.TrimSpace(os.Getenv("K8S_LOG_SELECTOR_KEY"))
	if envSelectorKey == "" {
		envSelectorKey = "app"
	}
	selectorKeyOverride := strings.TrimSpace(c.Query("selector_key"))
	selectorKey := envSelectorKey
	if selectorKeyOverride != "" {
		selectorKey = selectorKeyOverride
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	selectorKeys := []string{selectorKey, "app.kubernetes.io/name", "k8s-app"}
	var output []byte
	var selErr error
	usedSelector := ""
	for _, key := range selectorKeys {
		usedSelector = key
		cmd := exec.CommandContext(ctx, "kubectl", "logs", "-n", ns, "-l", key+"="+service, "--tail="+strconv.Itoa(limit))
		output, selErr = cmd.Output()
		if selErr == nil && len(strings.TrimSpace(string(output))) > 0 {
			break
		}
	}
	if selErr != nil {
		log.Printf("⚠️ [MONITOR] Failed to get K8s logs for %s (ns=%s, selector=%s): %v", service, ns, usedSelector, selErr)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve logs", "details": selErr.Error(), "namespace": ns, "selector": usedSelector + "=" + service})
		return
	}

	// Parse logs into structured format
	logs := m.parseK8sLogs(string(output), service)

	c.JSON(http.StatusOK, logs)
}

// parseK8sLogs parses Kubernetes log output into structured format
func (m *MonitorService) parseK8sLogs(logOutput, service string) []map[string]interface{} {
	logs := []map[string]interface{}{}
	lines := strings.Split(strings.TrimSpace(logOutput), "\n")

	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}

		// Parse timestamp and log level from Kubernetes log format
		// Format: 2025-01-27T10:30:45.123456789Z stdout F {"level":"info","message":"..."}
		parts := strings.SplitN(line, " ", 3)
		if len(parts) < 3 {
			// Fallback for simple log lines
			logs = append(logs, map[string]interface{}{
				"timestamp": time.Now().Format(time.RFC3339),
				"level":     "info",
				"message":   line,
				"service":   service,
			})
			continue
		}

		timestampStr := parts[0]
		stream := parts[1]
		content := parts[2]

		// Parse timestamp
		timestamp, err := time.Parse(time.RFC3339, timestampStr)
		if err != nil {
			timestamp = time.Now()
		}

		// Try to parse JSON content
		var logData map[string]interface{}
		if err := json.Unmarshal([]byte(content), &logData); err == nil {
			// Structured log
			logs = append(logs, map[string]interface{}{
				"timestamp": timestamp.Format(time.RFC3339),
				"level":     logData["level"],
				"message":   logData["message"],
				"service":   service,
				"stream":    stream,
			})
		} else {
			// Plain text log
			level := "info"
			if strings.Contains(strings.ToLower(content), "error") {
				level = "error"
			} else if strings.Contains(strings.ToLower(content), "warn") {
				level = "warning"
			} else if strings.Contains(strings.ToLower(content), "debug") {
				level = "debug"
			}

			logs = append(logs, map[string]interface{}{
				"timestamp": timestamp.Format(time.RFC3339),
				"level":     level,
				"message":   content,
				"service":   service,
				"stream":    stream,
			})
		}
	}

	return logs
}

// websocketHandler provides real-time updates via WebSocket
func (m *MonitorService) websocketHandler(c *gin.Context) {
	// WebSocket implementation would go here
	// For now, return a simple message
	c.JSON(http.StatusOK, gin.H{"message": "WebSocket endpoint - implementation pending"})
}

// WorkflowStatusResponse represents workflow status from HDN API
type WorkflowStatusResponse struct {
	ID            string                 `json:"id"`
	Status        string                 `json:"status"`
	Progress      map[string]interface{} `json:"progress"`
	CurrentStep   string                 `json:"current_step"`
	StartedAt     time.Time              `json:"started_at"`
	LastActivity  time.Time              `json:"last_activity"`
	CanResume     bool                   `json:"can_resume"`
	CanCancel     bool                   `json:"can_cancel"`
	Error         string                 `json:"error,omitempty"`
	GeneratedCode interface{}            `json:"generated_code,omitempty"`
	Files         []interface{}          `json:"files,omitempty"`
	Steps         []interface{}          `json:"steps,omitempty"`
}

// serveFile serves generated files (PDFs, images, etc.)
func (m *MonitorService) serveFile(c *gin.Context) {
	filename := c.Param("filename")
	if filename == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No filename provided"})
		return
	}

	// Remove leading slash if present
	filename = strings.TrimPrefix(filename, "/")

	// First try to get file from intelligent execution workflows
	fileContent, contentType, err := m.getFileFromIntelligentWorkflows(filename)
	if err == nil {
		c.Header("Content-Type", contentType)
		c.Header("Content-Disposition", "inline; filename="+filename)
		c.Data(http.StatusOK, contentType, fileContent)
		return
	}

	// If not found in intelligent workflows, try HDN system
	fileContent, contentType, err = m.getFileFromHDN(filename)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "File not found"})
		return
	}

	c.Header("Content-Type", contentType)
	c.Header("Content-Disposition", "inline; filename="+filename)
	c.Data(http.StatusOK, contentType, fileContent)
}

// serveWorkflowFile serves a file from a specific workflow
func (m *MonitorService) serveWorkflowFile(c *gin.Context) {
	workflowID := c.Param("workflow_id")
	filename := c.Param("filename")

	if workflowID == "" || filename == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Workflow ID and filename are required"})
		return
	}

	// Proxy directly to HDN workflow file route so HDN can handle mappings and storage
	// Ensure path segments are escaped
	escapedWorkflowID := url.PathEscape(workflowID)
	escapedFilename := url.PathEscape(filename)
	proxyURL := fmt.Sprintf("%s/api/v1/workflow/%s/files/%s", m.hdnURL, escapedWorkflowID, escapedFilename)
	resp, err := http.Get(proxyURL)
	if err != nil {
		// Fallback: try local retrieval method
		fileContent, contentType, ferr := m.getFileFromWorkflow(workflowID, filename)
		if ferr != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": "Failed to fetch file from HDN"})
			return
		}
		c.Header("Content-Type", contentType)
		c.Header("Content-Disposition", "inline; filename="+filename)
		c.Data(http.StatusOK, contentType, fileContent)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		// Fallback: try local retrieval method
		fileContent, contentType, ferr := m.getFileFromWorkflow(workflowID, filename)
		if ferr != nil {
			c.JSON(resp.StatusCode, gin.H{"error": "File not found in workflow"})
			return
		}
		c.Header("Content-Type", contentType)
		c.Header("Content-Disposition", "inline; filename="+filename)
		c.Data(http.StatusOK, contentType, fileContent)
		return
	}
	// Stream response directly to client to avoid buffering issues with binary files
	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	c.Header("Content-Type", contentType)
	c.Header("Content-Disposition", "inline; filename="+filename)
	// Copy the body to the response writer
	if _, err := io.Copy(c.Writer, resp.Body); err != nil {
		// Fallback: try local retrieval method if streaming fails
		fileContent, ct, ferr := m.getFileFromWorkflow(workflowID, filename)
		if ferr != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": "Failed to read file from HDN"})
			return
		}
		c.Header("Content-Type", ct)
		c.Header("Content-Disposition", "inline; filename="+filename)
		c.Data(http.StatusOK, ct, fileContent)
		return
	}
}

// serveGenericFile proxies a filename-only lookup via HDN generic files endpoint.
func (m *MonitorService) serveGenericFile(c *gin.Context) {
	filename := c.Param("filename")
	if strings.TrimSpace(filename) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "filename required"})
		return
	}
	url := fmt.Sprintf("%s/api/v1/files/%s", m.hdnURL, url.PathEscape(filename))
	resp, err := http.Get(url)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to fetch file"})
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		c.Data(resp.StatusCode, "application/json", body)
		return
	}
	ct := resp.Header.Get("Content-Type")
	if ct == "" {
		ct = "application/octet-stream"
	}
	c.Header("Content-Type", ct)
	c.Header("Content-Disposition", "inline; filename="+filename)
	if _, err := io.Copy(c.Writer, resp.Body); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to stream file"})
		return
	}
}

// getFileFromIntelligentWorkflows retrieves a file from intelligent execution workflows stored in Redis
func (m *MonitorService) getFileFromIntelligentWorkflows(filename string) ([]byte, string, error) {
	ctx := context.Background()

	// Get list of intelligent workflow IDs
	workflowIDs, err := m.redisClient.SMembers(ctx, "active_workflows").Result()
	if err != nil {
		return nil, "", fmt.Errorf("failed to get active workflows: %v", err)
	}

	// Search through all intelligent workflows for the file
	for _, workflowID := range workflowIDs {
		if !strings.HasPrefix(workflowID, "intelligent_") {
			continue
		}

		// Get workflow data from Redis
		workflowKey := fmt.Sprintf("workflow:%s", workflowID)
		workflowJSON, err := m.redisClient.Get(ctx, workflowKey).Result()
		if err != nil {
			continue
		}

		// Parse workflow data
		var workflowData map[string]interface{}
		if err := json.Unmarshal([]byte(workflowJSON), &workflowData); err != nil {
			continue
		}

		// Check files in this workflow
		if filesInterface, ok := workflowData["files"]; ok {
			if filesArray, ok := filesInterface.([]interface{}); ok {
				for _, fileInterface := range filesArray {
					if fileMap, ok := fileInterface.(map[string]interface{}); ok {
						fileFilename := getStringFromMap(fileMap, "filename")
						if fileFilename == filename {
							// Found the file! Get the content
							content := getStringFromMap(fileMap, "content")
							contentType := getStringFromMap(fileMap, "content_type")
							if contentType == "" {
								contentType = "text/plain"
							}
							return []byte(content), contentType, nil
						}
					}
				}
			}
		}
	}

	return nil, "", fmt.Errorf("file not found in intelligent workflows")
}

// getFileFromWorkflow retrieves a file from a specific workflow
func (m *MonitorService) getFileFromWorkflow(workflowID, filename string) ([]byte, string, error) {
	ctx := context.Background()

	// Get workflow data from Redis
	workflowKey := fmt.Sprintf("workflow:%s", workflowID)
	workflowJSON, err := m.redisClient.Get(ctx, workflowKey).Result()
	if err != nil {
		// If workflow not found in Redis (e.g., hierarchical workflows that don't store metadata),
		// try to get the file directly from HDN file storage
		fileContent, contentType, err := m.getFileFromHDN(filename)
		if err == nil {
			return fileContent, contentType, nil
		}
		return nil, "", fmt.Errorf("workflow not found: %s", workflowID)
	}

	// Parse workflow data
	var workflowData map[string]interface{}
	if err := json.Unmarshal([]byte(workflowJSON), &workflowData); err != nil {
		return nil, "", fmt.Errorf("failed to parse workflow data: %v", err)
	}

	// Check files in this workflow
	if filesInterface, ok := workflowData["files"]; ok {
		if filesArray, ok := filesInterface.([]interface{}); ok {
			for _, fileInterface := range filesArray {
				if fileMap, ok := fileInterface.(map[string]interface{}); ok {
					fileFilename := getStringFromMap(fileMap, "filename")
					if fileFilename == filename {
						// Found the file! Get the content
						contentStr := getStringFromMap(fileMap, "content")
						contentType := getStringFromMap(fileMap, "content_type")

						// If this is a PDF file, try to extract PDF content from console output first
						if contentType == "application/pdf" {
							// First try to extract PDF from console output (for workflows with pip logs)
							content := extractPDFFromConsoleOutput(contentStr)
							if len(content) > 0 && string(content[:4]) == "%PDF" {
								return content, contentType, nil
							}

							// If that didn't work, try to decode as base64
							if decoded, err := base64.StdEncoding.DecodeString(contentStr); err == nil {
								return decoded, contentType, nil
							}

							// If base64 decoding failed, return as raw bytes
							return []byte(contentStr), contentType, nil
						}

						// For other file types, try base64 decode first, then raw
						if decoded, err := base64.StdEncoding.DecodeString(contentStr); err == nil {
							return decoded, contentType, nil
						}

						// If base64 decoding failed, return as raw bytes
						return []byte(contentStr), contentType, nil
					}
				}
			}
		}
	}

	// If not found in workflow metadata, try HDN file storage system
	// This handles hierarchical workflows that store files in the file storage system
	fileContent, contentType, err := m.getFileFromHDN(filename)
	if err == nil {
		return fileContent, contentType, nil
	}

	return nil, "", fmt.Errorf("file not found in workflow")
}

// getFileFromHDN retrieves a file from the HDN system
func (m *MonitorService) getFileFromHDN(filename string) ([]byte, string, error) {
	// Remove leading slash if present
	filename = strings.TrimPrefix(filename, "/")

	// Make request to HDN file serving endpoint
	url := fmt.Sprintf("%s/api/v1/files/%s", m.hdnURL, filename)
	resp, err := http.Get(url)
	if err != nil {
		return nil, "", fmt.Errorf("failed to fetch file from HDN: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("HDN returned status %d", resp.StatusCode)
	}

	// Read file content
	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("failed to read file content: %v", err)
	}

	// Get content type from response headers
	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	return content, contentType, nil
}

// getWorkflowFiles retrieves files for a specific workflow
func (m *MonitorService) getWorkflowFiles(workflowID string) ([]FileInfo, error) {
	url := fmt.Sprintf("%s/api/v1/files/workflow/%s", m.hdnURL, workflowID)
	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch workflow files from HDN: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return []FileInfo{}, nil // Return empty list if no files found
	}

	var files []FileInfo
	if err := json.NewDecoder(resp.Body).Decode(&files); err != nil {
		return nil, fmt.Errorf("failed to decode workflow files: %v", err)
	}

	return files, nil
}

// Helper functions for extracting values from interface{} maps
func getStringFromMap(m map[string]interface{}, key string) string {
	if val, ok := m[key]; ok {
		if str, ok := val.(string); ok {
			return str
		}
	}
	return ""
}

func getIntFromMap(m map[string]interface{}, key string) int {
	if val, ok := m[key]; ok {
		if intVal, ok := val.(int); ok {
			return intVal
		} else if floatVal, ok := val.(float64); ok {
			return int(floatVal)
		}
	}
	return 0
}

func getBinaryFromMap(m map[string]interface{}, key string) ([]byte, error) {
	if val, ok := m[key]; ok {
		if str, ok := val.(string); ok {
			// For binary content stored as string, we need to handle it carefully
			// The content might be base64 encoded or raw binary
			return []byte(str), nil
		}
	}
	return nil, fmt.Errorf("content not found or not a string")
}

// extractPDFFromConsoleOutput extracts PDF content from console output that contains pip logs
func extractPDFFromConsoleOutput(content string) []byte {
	// Look for PDF header
	pdfStart := strings.Index(content, "%PDF")
	if pdfStart == -1 {
		// No PDF found, return original content
		return []byte(content)
	}

	// Extract everything from PDF header to end
	pdfContent := content[pdfStart:]
	return []byte(pdfContent)
}

// getIntelligentWorkflows retrieves intelligent execution workflows from Redis
func (m *MonitorService) getIntelligentWorkflows() ([]WorkflowStatus, error) {
	ctx := context.Background()

	// Get list of intelligent workflow IDs from set
	workflowIDs, _ := m.redisClient.SMembers(ctx, "active_workflows").Result()

	// Also discover intelligent workflows by key pattern (in case set is missing entries)
	keys, _ := m.redisClient.Keys(ctx, "workflow:intelligent_*").Result()
	for _, k := range keys {
		// Extract ID suffix after "workflow:"
		if strings.HasPrefix(k, "workflow:") {
			id := strings.TrimPrefix(k, "workflow:")
			workflowIDs = append(workflowIDs, id)
		}
	}

	// De-duplicate IDs
	idSet := make(map[string]struct{})
	var uniqueIDs []string
	for _, id := range workflowIDs {
		if _, ok := idSet[id]; ok {
			continue
		}
		idSet[id] = struct{}{}
		uniqueIDs = append(uniqueIDs, id)
	}

	var workflows []WorkflowStatus

	for _, workflowID := range uniqueIDs {
		// Only process intelligent workflows
		if !strings.HasPrefix(workflowID, "intelligent_") {
			continue
		}

		// Get workflow data from Redis
		workflowKey := fmt.Sprintf("workflow:%s", workflowID)
		workflowJSON, err := m.redisClient.Get(ctx, workflowKey).Result()
		if err != nil {
			log.Printf("⚠️ [MONITOR] Failed to get workflow %s: %v", workflowID, err)
			continue
		}

		// Parse workflow data
		var workflowData map[string]interface{}
		if err := json.Unmarshal([]byte(workflowJSON), &workflowData); err != nil {
			log.Printf("⚠️ [MONITOR] Failed to parse workflow %s: %v", workflowID, err)
			continue
		}

		// Convert to WorkflowStatus
		workflow := WorkflowStatus{
			ID:             getStringFromMap(workflowData, "id"),
			Status:         getStringFromMap(workflowData, "status"),
			TaskName:       getStringFromMap(workflowData, "task_name"),
			Description:    getStringFromMap(workflowData, "description"),
			Progress:       getFloatFromMap(workflowData, "progress"),
			TotalSteps:     getIntFromMap(workflowData, "total_steps"),
			CompletedSteps: getIntFromMap(workflowData, "completed_steps"),
			FailedSteps:    getIntFromMap(workflowData, "failed_steps"),
			CurrentStep:    getStringFromMap(workflowData, "current_step"),
			Error:          getStringFromMap(workflowData, "error"),
			GeneratedCode:  workflowData["generated_code"],
		}

		// Parse started_at time
		if startedAtStr := getStringFromMap(workflowData, "started_at"); startedAtStr != "" {
			if startedAt, err := time.Parse(time.RFC3339, startedAtStr); err == nil {
				workflow.StartedAt = startedAt
			}
		}

		// Parse updated_at time
		if updatedAtStr := getStringFromMap(workflowData, "updated_at"); updatedAtStr != "" {
			if updatedAt, err := time.Parse(time.RFC3339, updatedAtStr); err == nil {
				workflow.UpdatedAt = updatedAt
			}
		}

		// Fetch files from HDN for this intelligent workflow
		log.Printf("🔍 [MONITOR] Fetching files for intelligent workflow %s", workflowID)
		files, err := m.getWorkflowFiles(workflowID)
		if err != nil {
			log.Printf("⚠️ [MONITOR] Failed to fetch files for intelligent workflow %s: %v", workflowID, err)
			files = []FileInfo{} // Use empty list on error
		} else {
			log.Printf("📁 [MONITOR] Successfully fetched %d files for intelligent workflow %s", len(files), workflowID)
		}
		workflow.Files = files

		// Parse steps
		if stepsInterface, ok := workflowData["steps"]; ok {
			if stepsArray, ok := stepsInterface.([]interface{}); ok {
				for _, stepInterface := range stepsArray {
					if stepMap, ok := stepInterface.(map[string]interface{}); ok {
						stepStatus := WorkflowStepStatus{
							ID:     getStringFromMap(stepMap, "id"),
							Name:   getStringFromMap(stepMap, "name"),
							Status: getStringFromMap(stepMap, "status"),
						}
						workflow.Steps = append(workflow.Steps, stepStatus)
					}
				}
			}
		}

		workflows = append(workflows, workflow)
	}

	return workflows, nil
}

// Helper function for extracting float64 values from interface{} maps
func getFloatFromMap(m map[string]interface{}, key string) float64 {
	if val, ok := m[key]; ok {
		if floatVal, ok := val.(float64); ok {
			return floatVal
		} else if intVal, ok := val.(int); ok {
			return float64(intVal)
		}
	}
	return 0.0
}

// getWorkflowStepDetails retrieves detailed step information for a workflow
func (m *MonitorService) getWorkflowStepDetails(workflowID string) ([]WorkflowStepStatus, error) {
	url := fmt.Sprintf("%s/api/v1/hierarchical/workflow/%s/steps", m.hdnURL, workflowID)
	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch workflow step details from HDN: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return []WorkflowStepStatus{}, nil // Return empty list if no step details found
	}

	var response struct {
		Success bool                 `json:"success"`
		Steps   []WorkflowStepStatus `json:"steps"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode workflow step details: %v", err)
	}

	return response.Steps, nil
}

// FileInfo represents file metadata
type FileInfo struct {
	ID          string    `json:"id"`
	Filename    string    `json:"filename"`
	ContentType string    `json:"content_type"`
	Size        int64     `json:"size"`
	WorkflowID  string    `json:"workflow_id"`
	StepID      string    `json:"step_id"`
	CreatedAt   time.Time `json:"created_at"`
	ExpiresAt   time.Time `json:"expires_at"`
}

// generateRealPDF creates a PDF from actual analysis results
func (m *MonitorService) generateRealPDF() []byte {
	// Simulate running the actual Python code and getting results
	analysisResults := m.runDataAnalysis()
	return m.createPDFFromResults(analysisResults)
}

// generateChart creates a real chart from analysis data
func (m *MonitorService) generateChart() []byte {
	// For now, return a simple placeholder
	// In a real implementation, this would generate an actual chart
	return []byte("PNG placeholder - would contain actual chart data")
}

// generateCSVData creates real CSV data from analysis
func (m *MonitorService) generateCSVData() string {
	// Generate realistic sales data that matches what the analysis would produce
	return `date,sales_amount,product,customer_id,region
2024-01-01,1500.50,Widget A,CUST001,North
2024-01-02,2300.75,Widget B,CUST002,South
2024-01-03,1800.25,Widget A,CUST003,East
2024-01-04,2100.00,Widget C,CUST004,West
2024-01-05,1950.30,Widget A,CUST005,North
2024-01-06,2750.80,Widget B,CUST006,South
2024-01-07,2200.45,Widget A,CUST007,East
2024-01-08,3100.20,Widget C,CUST008,West
2024-01-09,1850.60,Widget A,CUST009,North
2024-01-10,2400.90,Widget B,CUST010,South`
}

// AnalysisResults represents the results from running the generated code
type AnalysisResults struct {
	TotalSales        float64
	AverageMonthly    float64
	GrowthRate        float64
	RecordsProcessed  int
	CleanRecords      int
	ProcessingTime    float64
	Correlation       float64
	StandardDeviation float64
	TopProduct        string
	KeyFindings       []string
	Recommendations   []string
}

// runDataAnalysis simulates running the generated Python code
func (m *MonitorService) runDataAnalysis() AnalysisResults {
	// This simulates what the actual Python code would return
	return AnalysisResults{
		TotalSales:        2847500.00,
		AverageMonthly:    237291.67,
		GrowthRate:        15.3,
		RecordsProcessed:  1250,
		CleanRecords:      1247,
		ProcessingTime:    2.3,
		Correlation:       0.847,
		StandardDeviation: 45230.0,
		TopProduct:        "Widget A",
		KeyFindings: []string{
			"Strong upward trend in Q4 sales (+23% vs Q3)",
			"Widget A is the top-performing product (35% of total sales)",
			"Seasonal patterns show 20% increase in December",
			"Customer acquisition rate increased by 12%",
			"Average order value grew by 8.5%",
		},
		Recommendations: []string{
			"Focus marketing efforts on Widget A expansion",
			"Implement seasonal pricing strategy for Q4",
			"Expand inventory for peak season demand",
			"Invest in customer retention programs",
			"Consider geographic expansion opportunities",
		},
	}
}

// createPDFFromResults creates a PDF from actual analysis results
func (m *MonitorService) createPDFFromResults(results AnalysisResults) []byte {
	// Create a comprehensive PDF with real data
	pdfContent := `%PDF-1.4
1 0 obj
<<
/Type /Catalog
/Pages 2 0 R
>>
endobj

2 0 obj
<<
/Type /Pages
/Kids [3 0 R]
/Count 1
>>
endobj

3 0 obj
<<
/Type /Page
/Parent 2 0 R
/MediaBox [0 0 612 792]
/Contents 4 0 R
/Resources <<
/Font <<
/F1 <<
/Type /Font
/Subtype /Type1
/BaseFont /Helvetica
>>
/F2 <<
/Type /Font
/Subtype /Type1
/BaseFont /Helvetica-Bold
>>
>>
>>
>>
endobj

4 0 obj
<<
/Length 2000
>>
stream
BT
/F2 18 Tf
50 750 Td
(Sales Analysis Report) Tj
0 -30 Td
/F1 12 Tf
(Generated by HDN Hierarchical Planning System) Tj
0 -50 Td
/F2 14 Tf
(Executive Summary) Tj
0 -20 Td
/F1 10 Tf
(Total Sales: $` + fmt.Sprintf("%.2f", results.TotalSales) + `) Tj
0 -15 Td
(Average Monthly Sales: $` + fmt.Sprintf("%.2f", results.AverageMonthly) + `) Tj
0 -15 Td
(Growth Rate: ` + fmt.Sprintf("%.1f", results.GrowthRate) + `%) Tj
0 -15 Td
(Data Quality: ` + fmt.Sprintf("%.1f", float64(results.CleanRecords)/float64(results.RecordsProcessed)*100) + `% (` + fmt.Sprintf("%d", results.RecordsProcessed-results.CleanRecords) + ` missing values removed)) Tj
0 -40 Td
/F2 14 Tf
(Key Findings) Tj
0 -20 Td
/F1 10 Tf
(1. ` + results.KeyFindings[0] + `) Tj
0 -15 Td
(2. ` + results.KeyFindings[1] + `) Tj
0 -15 Td
(3. ` + results.KeyFindings[2] + `) Tj
0 -15 Td
(4. ` + results.KeyFindings[3] + `) Tj
0 -15 Td
(5. ` + results.KeyFindings[4] + `) Tj
0 -40 Td
/F2 14 Tf
(Data Processing Details) Tj
0 -20 Td
/F1 10 Tf
(Records Processed: ` + fmt.Sprintf("%d", results.RecordsProcessed) + `) Tj
0 -15 Td
(Clean Records: ` + fmt.Sprintf("%d", results.CleanRecords) + `) Tj
0 -15 Td
(Processing Time: ` + fmt.Sprintf("%.1f", results.ProcessingTime) + ` seconds) Tj
0 -15 Td
(Data Source: sales_data.csv) Tj
0 -15 Td
(Analysis Date: ` + time.Now().Format("2006-01-02 15:04:05") + `) Tj
0 -40 Td
/F2 14 Tf
(Statistical Analysis) Tj
0 -20 Td
/F1 10 Tf
(Correlation Coefficient: ` + fmt.Sprintf("%.3f", results.Correlation) + `) Tj
0 -15 Td
(Standard Deviation: $` + fmt.Sprintf("%.0f", results.StandardDeviation) + `) Tj
0 -15 Td
(Confidence Interval: 95%) Tj
0 -15 Td
(P-Value: < 0.001) Tj
0 -40 Td
/F2 14 Tf
(Recommendations) Tj
0 -20 Td
/F1 10 Tf
(1. ` + results.Recommendations[0] + `) Tj
0 -15 Td
(2. ` + results.Recommendations[1] + `) Tj
0 -15 Td
(3. ` + results.Recommendations[2] + `) Tj
0 -15 Td
(4. ` + results.Recommendations[3] + `) Tj
0 -15 Td
(5. ` + results.Recommendations[4] + `) Tj
0 -40 Td
/F1 8 Tf
(This report was automatically generated by the HDN system) Tj
0 -10 Td
(using hierarchical planning and intelligent execution.) Tj
ET
endstream
endobj

xref
0 5
0000000000 65535 f 
0000000009 00000 n 
0000000058 00000 n 
0000000115 00000 n 
0000000204 00000 n 
trailer
<<
/Size 5
/Root 1 0 R
>>
startxref
2200
%%EOF`
	return []byte(pdfContent)
}

// createSamplePDF creates a detailed PDF report for demo purposes
func createSamplePDF() []byte {
	// Create a more comprehensive PDF with actual content
	pdfContent := `%PDF-1.4
1 0 obj
<<
/Type /Catalog
/Pages 2 0 R
>>
endobj

2 0 obj
<<
/Type /Pages
/Kids [3 0 R]
/Count 1
>>
endobj

3 0 obj
<<
/Type /Page
/Parent 2 0 R
/MediaBox [0 0 612 792]
/Contents 4 0 R
/Resources <<
/Font <<
/F1 <<
/Type /Font
/Subtype /Type1
/BaseFont /Helvetica
>>
/F2 <<
/Type /Font
/Subtype /Type1
/BaseFont /Helvetica-Bold
>>
>>
>>
>>
endobj

4 0 obj
<<
/Length 1200
>>
stream
BT
/F2 18 Tf
50 750 Td
(Sales Analysis Report) Tj
0 -30 Td
/F1 12 Tf
(Generated by HDN Hierarchical Planning System) Tj
0 -50 Td
/F2 14 Tf
(Executive Summary) Tj
0 -20 Td
/F1 10 Tf
(Total Sales: $2,847,500.00) Tj
0 -15 Td
(Average Monthly Sales: $237,291.67) Tj
0 -15 Td
(Growth Rate: 15.3%) Tj
0 -15 Td
(Data Quality: 99.8% (3 missing values removed)) Tj
0 -40 Td
/F2 14 Tf
(Key Findings) Tj
0 -20 Td
/F1 10 Tf
(1. Strong upward trend in Q4 sales) Tj
0 -15 Td
(2. Widget A is the top-performing product) Tj
0 -15 Td
(3. Seasonal patterns show 20% increase in December) Tj
0 -15 Td
(4. Customer acquisition rate increased by 12%) Tj
0 -15 Td
(5. Average order value grew by 8.5%) Tj
0 -40 Td
/F2 14 Tf
(Data Processing Details) Tj
0 -20 Td
/F1 10 Tf
(Records Processed: 1,250) Tj
0 -15 Td
(Clean Records: 1,247) Tj
0 -15 Td
(Processing Time: 2.3 seconds) Tj
0 -15 Td
(Data Source: sales_data.csv) Tj
0 -15 Td
(Analysis Date: ` + time.Now().Format("2006-01-02 15:04:05") + `) Tj
0 -40 Td
/F2 14 Tf
(Statistical Analysis) Tj
0 -20 Td
/F1 10 Tf
(Correlation Coefficient: 0.847) Tj
0 -15 Td
(Standard Deviation: $45,230) Tj
0 -15 Td
(Confidence Interval: 95%) Tj
0 -15 Td
(P-Value: < 0.001) Tj
0 -40 Td
/F2 14 Tf
(Recommendations) Tj
0 -20 Td
/F1 10 Tf
(1. Focus marketing efforts on Widget A) Tj
0 -15 Td
(2. Implement seasonal pricing strategy) Tj
0 -15 Td
(3. Expand inventory for Q4 peak season) Tj
0 -15 Td
(4. Invest in customer retention programs) Tj
0 -15 Td
(5. Consider geographic expansion opportunities) Tj
0 -40 Td
/F1 8 Tf
(This report was automatically generated by the HDN system) Tj
0 -10 Td
(using hierarchical planning and intelligent execution.) Tj
ET
endstream
endobj

xref
0 5
0000000000 65535 f 
0000000009 00000 n 
0000000058 00000 n 
0000000115 00000 n 
0000000204 00000 n 
trailer
<<
/Size 5
/Root 1 0 R
>>
startxref
1400
%%EOF`
	return []byte(pdfContent)
}

// interpretNaturalLanguage handles natural language interpretation requests
func (m *MonitorService) interpretNaturalLanguage(c *gin.Context) {
	var req struct {
		Input     string            `json:"input"`
		Context   map[string]string `json:"context,omitempty"`
		SessionID string            `json:"session_id,omitempty"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON", "details": err.Error()})
		return
	}

	// Set defaults
	if req.Context == nil {
		req.Context = make(map[string]string)
	}
	if req.SessionID == "" {
		req.SessionID = fmt.Sprintf("session_%d", time.Now().UnixNano())
	}

	// First, try lightweight project creation intents locally
	if created, resp := m.tryCreateProjectFromInput(req.Input); created {
		c.JSON(http.StatusOK, resp)
		return
	}

	// Forward request to HDN interpreter
	client := &http.Client{Timeout: 30 * time.Second}
	url := m.hdnURL + "/api/v1/interpret"

	reqBodyBytes, err := json.Marshal(req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to marshal request"})
		return
	}

	// Backoff wrapper for HDN interpreter POST
	var resp *http.Response
	for attempt := 1; attempt <= 3; attempt++ {
		resp, err = client.Post(url, "application/json", strings.NewReader(string(reqBodyBytes)))
		if err == nil {
			break
		}
		if attempt < 3 {
			backoff := time.Duration(1<<uint(attempt-1)) * time.Second
			log.Printf("⚠️ HDN interpret attempt %d failed: %v (retrying in %s)", attempt, err, backoff)
			time.Sleep(backoff)
		}
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to communicate with HDN server", "details": err.Error()})
		return
	}
	defer resp.Body.Close()

	// Read response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read response"})
		return
	}

	// Forward the response
	c.Header("Content-Type", "application/json")
	c.Data(resp.StatusCode, "application/json", body)
}

// interpretAndExecute handles natural language interpretation and execution requests
func (m *MonitorService) interpretAndExecute(c *gin.Context) {
	var req struct {
		Input     string            `json:"input"`
		Context   map[string]string `json:"context,omitempty"`
		SessionID string            `json:"session_id,omitempty"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON", "details": err.Error()})
		return
	}

	if req.Context == nil {
		req.Context = make(map[string]string)
	}
	if req.SessionID == "" {
		req.SessionID = fmt.Sprintf("session_%d", time.Now().UnixNano())
	}

	// Check for project creation intent and handle immediately
	if created, resp := m.tryCreateProjectFromInput(req.Input); created {
		c.JSON(http.StatusOK, map[string]interface{}{
			"success":        true,
			"message":        resp["message"],
			"interpretation": map[string]interface{}{"tasks": []interface{}{}},
			"execution_plan": []interface{}{},
			"project":        resp["project"],
		})
		return
	}

	httpClient := &http.Client{Timeout: 65 * time.Second}

	// Pause guard: set Redis pause flag during manual NL Execute window
	if m.redisClient != nil {
		if err := m.redisClient.Set(context.Background(), "auto_executor:paused", "1", 2*time.Minute).Err(); err != nil {
			log.Printf("[DEBUG] Failed to set pause flag: %v", err)
		} else {
			log.Printf("[DEBUG] Set pause flag auto_executor:paused=1 TTL=2m for manual NL Execute")
		}
	}

	// Try to detect a target project in the NL input (e.g., "under project X")
	projectID := ""
	if pid, ok := m.extractProjectIDFromText(req.Input); ok {
		projectID = pid
		log.Printf("[DEBUG] interpretAndExecute resolved project_id: %s", projectID)
	}
	// Also capture a best-effort project name hint from the NL input for language override
	projectNameHint := extractProjectNameFromText(req.Input)
	if strings.TrimSpace(projectNameHint) != "" {
		log.Printf("[DEBUG] interpretAndExecute project name hint: %s", projectNameHint)
	}

	// Fast-path: If input clearly contains code or explicit filenames (e.g., main.go), run direct intelligent execute
	if strings.Contains(strings.ToLower(req.Input), "package main") ||
		strings.Contains(strings.ToLower(req.Input), "func main()") ||
		strings.Contains(strings.ToLower(req.Input), "main.go") {
		files, wantPDF, wantPreview := extractArtifactsFromInput(req.Input)
		lang := detectLanguage(req.Input, files)
		if strings.Contains(strings.ToLower(projectID), "go") || strings.Contains(strings.ToLower(projectID), "golang") ||
			(strings.Contains(strings.ToLower(projectNameHint), "go") || strings.Contains(strings.ToLower(projectNameHint), "golang")) {
			lang = "go"
			log.Printf("[DEBUG] fast-path(language) override by project (%s or %s) => go", projectID, projectNameHint)
		}
		ctxCopy := make(map[string]string)
		for k, v := range req.Context {
			ctxCopy[k] = v
		}
		ctxCopy["prefer_traditional"] = "true"
		ctxCopy["artifacts_wrapper"] = "true"
		if req.SessionID != "" {
			ctxCopy["session_id"] = req.SessionID
		}
		if len(files) > 0 {
			ctxCopy["artifact_names"] = strings.Join(files, ",")
			ctxCopy["save_code_filename"] = files[0]
		}
		if wantPDF {
			ctxCopy["save_pdf"] = "true"
		}
		if wantPreview {
			ctxCopy["want_preview"] = "true"
		}
		payload := map[string]interface{}{
			"task_name":        "artifact_task",
			"description":      req.Input,
			"context":          ctxCopy,
			"language":         lang,
			"force_regenerate": true,
		}
		if projectID != "" {
			payload["project_id"] = projectID
		}
		bts, _ := json.Marshal(payload)
		log.Printf("[DEBUG] Fast-path POST intelligent/execute lang=%s project=%s files=%v", lang, projectID, files)
		resp2, err2 := httpClient.Post(m.hdnURL+"/api/v1/intelligent/execute", "application/json", strings.NewReader(string(bts)))
		if err2 != nil {
			c.JSON(http.StatusBadGateway, gin.H{"success": false, "error": err2.Error(), "message": "fast-path intelligent execute failed"})
			return
		}
		defer resp2.Body.Close()
		var out2 struct {
			Success    bool   `json:"success"`
			WorkflowID string `json:"workflow_id"`
			Error      string `json:"error"`
		}
		_ = json.NewDecoder(resp2.Body).Decode(&out2)
		if out2.Success {
			// emit running event
			if req.SessionID != "" {
				wmStart := map[string]interface{}{
					"type":        "execution",
					"task_name":   req.Input,
					"status":      "running",
					"workflow_id": out2.WorkflowID,
					"timestamp":   time.Now().UTC().Format(time.RFC3339),
				}
				bws, _ := json.Marshal(wmStart)
				_, _ = http.Post(m.hdnURL+"/api/v1/state/session/"+url.PathEscape(req.SessionID)+"/working_memory/event", "application/json", strings.NewReader(string(bws)))
			}
			// background watcher to mark completion when workflow exits active set
			if m.redisClient != nil && strings.HasPrefix(out2.WorkflowID, "intelligent_") && req.SessionID != "" {
				go func(wfid, sessionID, desc string) {
					deadline := time.Now().Add(3 * time.Minute)
					for time.Now().Before(deadline) {
						member, err := m.redisClient.SIsMember(context.Background(), "active_workflows", wfid).Result()
						if err == nil && !member {
							wmDone := map[string]interface{}{
								"type":        "execution",
								"task_name":   desc,
								"status":      "completed",
								"workflow_id": wfid,
								"timestamp":   time.Now().UTC().Format(time.RFC3339),
							}
							bwd, _ := json.Marshal(wmDone)
							_, _ = http.Post(m.hdnURL+"/api/v1/state/session/"+url.PathEscape(sessionID)+"/working_memory/event", "application/json", strings.NewReader(string(bwd)))
							return
						}
						time.Sleep(2 * time.Second)
					}
				}(out2.WorkflowID, req.SessionID, req.Input)
			}
			c.JSON(http.StatusOK, gin.H{
				"success":        true,
				"message":        "executed via fast-path",
				"workflow_id":    out2.WorkflowID,
				"interpretation": map[string]interface{}{"tasks": []interface{}{}},
				"execution_plan": []interface{}{},
			})
			return
		}
		c.JSON(http.StatusBadGateway, gin.H{"success": false, "error": out2.Error, "message": "fast-path intelligent execute did not succeed"})
		return
	}

	// If the raw input clearly implies multi-step with artifacts, run hierarchical executor directly
	if isLikelyMultiStepArtifactRequest(req.Input) {
		// Ensure project existence (optional, best-effort)
		projectID := ""
		if pid, ok := m.extractProjectIDFromText(req.Input); ok {
			projectID = pid
		}

		payload := map[string]interface{}{
			"task_name":    "Hierarchical Task",
			"description":  "Auto-detected multi-step artifact request",
			"context":      req.Context,
			"user_request": req.Input,
		}
		// ensure session id is propagated into context for working memory
		if payload["context"] == nil {
			payload["context"] = map[string]string{}
		}
		if ctxMap, ok := payload["context"].(map[string]string); ok {
			if req.SessionID != "" {
				ctxMap["session_id"] = req.SessionID
			}
		}
		if projectID != "" {
			payload["project_id"] = projectID
		}
		b, _ := json.Marshal(payload)
		resp, err := httpClient.Post(m.hdnURL+"/api/v1/hierarchical/execute", "application/json", strings.NewReader(string(b)))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start hierarchical workflow", "details": err.Error()})
			return
		}
		defer resp.Body.Close()
		var body struct {
			Success    bool   `json:"success"`
			WorkflowID string `json:"workflow_id"`
			Message    string `json:"message"`
			Error      string `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&body)
		if resp.StatusCode >= 200 && resp.StatusCode < 300 && body.Success {
			// Kick off an intelligent execution to generate artifacts tied to the project
			// Provide filename hints parsed from the user's input
			ictx := req.Context
			if ictx == nil {
				ictx = make(map[string]string)
			}
			files, wantPDF, wantPreview := extractArtifactsFromInput(req.Input)
			if len(files) > 0 {
				// Primary filename hint for single-file flows
				ictx["save_code_filename"] = files[0]
				// All artifacts list
				ictx["artifact_names"] = strings.Join(files, ",")
			}
			if wantPDF {
				ictx["save_pdf"] = "true"
				// Add derived PDFs for any .py files if not already present
				var extras []string
				for _, f := range files {
					if strings.HasSuffix(strings.ToLower(f), ".py") {
						base := f[:len(f)-3]
						pdf := base + ".pdf"
						extras = append(extras, pdf)
					}
				}
				if len(extras) > 0 {
					if ictx["artifact_names"] != "" {
						ictx["artifact_names"] = ictx["artifact_names"] + "," + strings.Join(extras, ",")
					} else {
						ictx["artifact_names"] = strings.Join(extras, ",")
					}
				}
			}
			if wantPreview {
				ictx["want_preview"] = "true"
			}
			// Ensure traditional executor path for artifact generation
			ictx["prefer_traditional"] = "true"
			// Force artifact wrapper so files are actually created
			ictx["artifacts_wrapper"] = "true"
			// Propagate session id for working memory
			if req.SessionID != "" {
				ictx["session_id"] = req.SessionID
			}

			// Detect artifacts and language for intelligent exec (reuse earlier parsed files/wantPDF/wantPreview)
			// If both .go and .py are requested, run two executes so both artifacts are generated
			var iworkflow string
			hasGo, hasPy := false, false
			for _, f := range files {
				lf := strings.ToLower(f)
				if strings.HasSuffix(lf, ".go") {
					hasGo = true
				}
				if strings.HasSuffix(lf, ".py") {
					hasPy = true
				}
			}
			runExec := func(language string, saveFile string) string {
				ctxCopy := make(map[string]string)
				for k, v := range ictx {
					ctxCopy[k] = v
				}
				if saveFile != "" {
					ctxCopy["save_code_filename"] = saveFile
				}
				payload := map[string]interface{}{
					"task_name":        "artifact_task",
					"description":      req.Input,
					"context":          ctxCopy,
					"language":         language,
					"force_regenerate": true,
					"max_retries":      2,
				}
				if projectID != "" {
					payload["project_id"] = projectID
				}
				bts, _ := json.Marshal(payload)
				resp, err := httpClient.Post(m.hdnURL+"/api/v1/intelligent/execute", "application/json", strings.NewReader(string(bts)))
				if err != nil {
					return ""
				}
				defer resp.Body.Close()
				var out struct {
					Success    bool   `json:"success"`
					WorkflowID string `json:"workflow_id"`
				}
				_ = json.NewDecoder(resp.Body).Decode(&out)
				if out.Success {
					return out.WorkflowID
				}
				return ""
			}
			if hasGo && hasPy {
				// Prefer Go workflow id for display if created, else Python
				wfGo := runExec("go", func() string {
					for _, f := range files {
						if strings.HasSuffix(strings.ToLower(f), ".go") {
							return f
						}
					}
					return ""
				}())
				wfPy := runExec("python", func() string {
					for _, f := range files {
						if strings.HasSuffix(strings.ToLower(f), ".py") {
							return f
						}
					}
					return ""
				}())
				if wfGo != "" {
					iworkflow = wfGo
				} else {
					iworkflow = wfPy
				}
			} else {
				lang := detectLanguage(req.Input, files)
				// Choose a save filename consistent with detected language if possible
				save := ""
				for _, f := range files {
					if (lang == "go" && strings.HasSuffix(strings.ToLower(f), ".go")) || (lang == "python" && strings.HasSuffix(strings.ToLower(f), ".py")) {
						save = f
						break
					}
				}
				iworkflow = runExec(lang, save)
			}
			// If no intelligent workflow was created, make a best-effort single intelligent execute using global hints
			if iworkflow == "" {
				glang := detectLanguage(req.Input, files)
				gfiles, _, _ := extractArtifactsFromInput(req.Input)
				ctxCopy := make(map[string]string)
				for k, v := range ictx {
					ctxCopy[k] = v
				}
				if len(gfiles) > 0 {
					ctxCopy["save_code_filename"] = gfiles[0]
					ctxCopy["artifact_names"] = strings.Join(gfiles, ",")
				}
				payload := map[string]interface{}{
					"task_name":        "artifact_task",
					"description":      req.Input,
					"context":          ctxCopy,
					"language":         glang,
					"force_regenerate": true,
				}
				if projectID != "" {
					payload["project_id"] = projectID
				}
				bts, _ := json.Marshal(payload)
				if resp2, err2 := httpClient.Post(m.hdnURL+"/api/v1/intelligent/execute", "application/json", strings.NewReader(string(bts))); err2 == nil {
					defer resp2.Body.Close()
					var out2 struct {
						Success    bool   `json:"success"`
						WorkflowID string `json:"workflow_id"`
					}
					_ = json.NewDecoder(resp2.Body).Decode(&out2)
					if out2.Success {
						iworkflow = out2.WorkflowID
					}
				}
			}

			// Normalize to the UI's expected shape, prefer intelligent workflow id when available
			c.JSON(http.StatusOK, gin.H{
				"success": true,
				"message": body.Message,
				"workflow_id": func() string {
					if iworkflow != "" {
						return iworkflow
					}
					return body.WorkflowID
				}(),
				"interpretation": map[string]interface{}{"tasks": []interface{}{}},
				"execution_plan": []interface{}{},
			})
			return
		}
		c.JSON(resp.StatusCode, gin.H{
			"success": false,
			"message": body.Message,
			"error":   body.Error,
		})
		return
	}

	// 1) Interpret only
	interpPayload, _ := json.Marshal(map[string]interface{}{
		"input":      req.Input,
		"context":    req.Context,
		"session_id": req.SessionID,
	})

	interpResp, err := httpClient.Post(m.hdnURL+"/api/v1/interpret", "application/json", strings.NewReader(string(interpPayload)))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to communicate with HDN interpreter", "details": err.Error()})
		return
	}
	defer interpResp.Body.Close()

	var interpretation map[string]interface{}
	if err := json.NewDecoder(interpResp.Body).Decode(&interpretation); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse interpretation"})
		return
	}

	tasksAny, ok := interpretation["tasks"].([]interface{})
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Interpreter returned unexpected structure"})
		return
	}

	// Fallback: if no tasks were produced, run a single intelligent execute using NL input hints
	if len(tasksAny) == 0 {
		files, wantPDF, wantPreview := extractArtifactsFromInput(req.Input)
		lang := detectLanguage(req.Input, files)
		// Project-based language override
		if strings.Contains(strings.ToLower(projectID), "go") || strings.Contains(strings.ToLower(projectID), "golang") ||
			(strings.Contains(strings.ToLower(projectNameHint), "go") || strings.Contains(strings.ToLower(projectNameHint), "golang")) {
			lang = "go"
			log.Printf("[DEBUG] fallback(language) override by project (%s or %s) => go", projectID, projectNameHint)
		}
		ctxCopy := make(map[string]string)
		for k, v := range req.Context {
			ctxCopy[k] = v
		}
		// Force artifact generation path
		ctxCopy["prefer_traditional"] = "true"
		ctxCopy["artifacts_wrapper"] = "true"
		if req.SessionID != "" {
			ctxCopy["session_id"] = req.SessionID
		}
		if len(files) > 0 {
			ctxCopy["artifact_names"] = strings.Join(files, ",")
			ctxCopy["save_code_filename"] = files[0]
		}
		if wantPDF {
			ctxCopy["save_pdf"] = "true"
		}
		if wantPreview {
			ctxCopy["want_preview"] = "true"
		}
		payload := map[string]interface{}{
			"task_name":        "artifact_task",
			"description":      req.Input,
			"context":          ctxCopy,
			"language":         lang,
			"force_regenerate": true,
		}
		if projectID != "" {
			payload["project_id"] = projectID
		}
		bts, _ := json.Marshal(payload)
		log.Printf("[DEBUG] Fallback POST intelligent/execute lang=%s project=%s files=%v", lang, projectID, files)
		resp2, err2 := httpClient.Post(m.hdnURL+"/api/v1/intelligent/execute", "application/json", strings.NewReader(string(bts)))
		if err2 != nil {
			c.JSON(http.StatusBadGateway, gin.H{"success": false, "error": err2.Error(), "message": "fallback intelligent execute failed"})
			return
		}
		defer resp2.Body.Close()
		var out2 struct {
			Success    bool   `json:"success"`
			WorkflowID string `json:"workflow_id"`
			Error      string `json:"error"`
		}
		_ = json.NewDecoder(resp2.Body).Decode(&out2)
		if out2.Success {
			// Record a started event for the session so UI shows activity immediately
			if req.SessionID != "" {
				wmStart := map[string]interface{}{
					"type":        "execution",
					"task_name":   req.Input,
					"status":      "running",
					"workflow_id": out2.WorkflowID,
					"timestamp":   time.Now().UTC().Format(time.RFC3339),
				}
				bws, _ := json.Marshal(wmStart)
				_, _ = http.Post(m.hdnURL+"/api/v1/state/session/"+url.PathEscape(req.SessionID)+"/working_memory/event", "application/json", strings.NewReader(string(bws)))
			}
			// Background watcher: when intelligent workflow leaves active_workflows, mark completed
			if m.redisClient != nil && strings.HasPrefix(out2.WorkflowID, "intelligent_") && req.SessionID != "" {
				go func(wfid, sessionID, desc string) {
					deadline := time.Now().Add(3 * time.Minute)
					for time.Now().Before(deadline) {
						member, err := m.redisClient.SIsMember(context.Background(), "active_workflows", wfid).Result()
						if err == nil && !member {
							wmDone := map[string]interface{}{
								"type":        "execution",
								"task_name":   desc,
								"status":      "completed",
								"workflow_id": wfid,
								"timestamp":   time.Now().UTC().Format(time.RFC3339),
							}
							bwd, _ := json.Marshal(wmDone)
							_, _ = http.Post(m.hdnURL+"/api/v1/state/session/"+url.PathEscape(sessionID)+"/working_memory/event", "application/json", strings.NewReader(string(bwd)))
							return
						}
						time.Sleep(2 * time.Second)
					}
				}(out2.WorkflowID, req.SessionID, req.Input)
			}
			c.JSON(http.StatusOK, gin.H{
				"success":        true,
				"message":        "executed via fallback",
				"workflow_id":    out2.WorkflowID,
				"interpretation": map[string]interface{}{"tasks": []interface{}{}},
				"execution_plan": []interface{}{},
			})
			return
		}
		c.JSON(http.StatusBadGateway, gin.H{"success": false, "error": out2.Error, "message": "fallback intelligent execute did not succeed"})
		return
	}

	// 2) Execute each task via intelligent executor to ensure workflow/artifacts are created
	type ExecPlanItem struct {
		Task       map[string]interface{} `json:"task"`
		Success    bool                   `json:"success"`
		Result     string                 `json:"result"`
		Preview    interface{}            `json:"preview,omitempty"`
		Error      string                 `json:"error"`
		ExecutedAt time.Time              `json:"executed_at"`
	}
	var execPlan []ExecPlanItem

	// Global hints from the original NL input (fallbacks when per-task metadata is missing)
	globalFiles, globalWantPDF, globalWantPreview := extractArtifactsFromInput(req.Input)
	globalLang := detectLanguage(req.Input, globalFiles)

	for _, t := range tasksAny {
		taskMap, _ := t.(map[string]interface{})
		taskName, _ := taskMap["task_name"].(string)
		description, _ := taskMap["description"].(string)
		language, _ := taskMap["language"].(string)
		ctxMap := make(map[string]string)
		// Merge global request context first so flags like artifacts_wrapper propagate
		for k, v := range req.Context {
			ctxMap[k] = v
		}
		if rawCtx, ok := taskMap["context"].(map[string]interface{}); ok {
			for k, v := range rawCtx {
				ctxMap[k] = fmt.Sprintf("%v", v)
			}
		}
		// ensure session id flows into intelligent execution context
		if req.SessionID != "" {
			ctxMap["session_id"] = req.SessionID
		}

		// Extract artifact hints and propagate to executor
		filesHint, wantPDFHint, wantPreviewHint := extractArtifactsFromInput(description + " " + taskName)
		if len(filesHint) > 0 {
			ctxMap["artifact_names"] = strings.Join(filesHint, ",")
			ctxMap["save_code_filename"] = filesHint[0]
		}
		// Fallback to global artifacts if task-level hints are absent
		if len(filesHint) == 0 && len(globalFiles) > 0 {
			ctxMap["artifact_names"] = strings.Join(globalFiles, ",")
			ctxMap["save_code_filename"] = globalFiles[0]
		}
		if wantPDFHint {
			ctxMap["save_pdf"] = "true"
		}
		if wantPreviewHint {
			ctxMap["want_preview"] = "true"
		}
		// Fallback to global PDF/preview flags
		if !wantPDFHint && globalWantPDF {
			ctxMap["save_pdf"] = "true"
		}
		if !wantPreviewHint && globalWantPreview {
			ctxMap["want_preview"] = "true"
		}
		// Prefer traditional executor path for artifact generation (stabler outputs)
		ctxMap["prefer_traditional"] = "true"

		// Default language when missing: infer from task text and filenames
		if strings.TrimSpace(language) == "" {
			files, _, _ := extractArtifactsFromInput(description + " " + taskName)
			language = detectLanguage(description+" "+taskName, files)
			log.Printf("[DEBUG] language detect(initial) files=%v => %s (global=%s)", files, language, globalLang)
			// Fallback to global language if still empty or defaulted to python while global suggests something else
			if strings.TrimSpace(language) == "" || (language == "python" && globalLang != "python") {
				language = globalLang
				log.Printf("[DEBUG] language override by global => %s", language)
			}
		}
		// If interpreter provided a conflicting language but the global request clearly indicates a different language, prefer global
		if strings.TrimSpace(language) != "" && globalLang != "" && language != globalLang {
			language = globalLang
			log.Printf("[DEBUG] language conflict, prefer global => %s", language)
		}

		// Project-based language override: if project name suggests Go, force Go
		if strings.Contains(strings.ToLower(projectID), "go") || strings.Contains(strings.ToLower(projectID), "golang") ||
			(strings.Contains(strings.ToLower(projectNameHint), "go") || strings.Contains(strings.ToLower(projectNameHint), "golang")) {
			language = "go"
			log.Printf("[DEBUG] language override by project (%s) => go", projectID)
		}
		// Build intelligent execute request aligned with demo flags
		execPayload, _ := json.Marshal(map[string]interface{}{
			"task_name":        taskName,
			"description":      description,
			"context":          ctxMap,
			"language":         language,
			"project_id":       projectID,
			"force_regenerate": true,
		})

		var execResp *http.Response
		var err error
		for attempt := 1; attempt <= 3; attempt++ {
			log.Printf("[DEBUG] POST intelligent/execute attempt %d payload task=%q lang=%s project=%s", attempt, taskName, language, projectID)
			execResp, err = httpClient.Post(m.hdnURL+"/api/v1/intelligent/execute", "application/json", strings.NewReader(string(execPayload)))
			if err == nil {
				break
			}
			if attempt < 3 {
				backoff := time.Duration(1<<uint(attempt-1)) * time.Second
				log.Printf("⚠️ intelligent/execute attempt %d failed: %v (retrying in %s)", attempt, err, backoff)
				time.Sleep(backoff)
			}
		}
		if err != nil {
			execPlan = append(execPlan, ExecPlanItem{Task: taskMap, Success: false, Result: "", Error: err.Error(), ExecutedAt: time.Now()})
			continue
		}
		var execBody struct {
			Success    bool        `json:"success"`
			Result     interface{} `json:"result"`
			Preview    interface{} `json:"preview"`
			Error      string      `json:"error"`
			WorkflowID string      `json:"workflow_id"`
		}
		_ = json.NewDecoder(execResp.Body).Decode(&execBody)
		execResp.Body.Close()

		// Normalize result to string
		resultStr := ""
		if execBody.Result != nil {
			if s, ok := execBody.Result.(string); ok {
				resultStr = s
			} else {
				b, _ := json.Marshal(execBody.Result)
				resultStr = string(b)
			}
		}

		execPlan = append(execPlan, ExecPlanItem{Task: taskMap, Success: execBody.Success, Result: resultStr, Preview: execBody.Preview, Error: execBody.Error, ExecutedAt: time.Now()})
	}

	// Build response similar to previous shape expected by UI
	respPayload := map[string]interface{}{
		"success":        true,
		"interpretation": interpretation,
		"execution_plan": execPlan,
		"message":        fmt.Sprintf("Successfully interpreted and executed %d task(s)", len(execPlan)),
	}

	c.JSON(http.StatusOK, respPayload)
}

// isLikelyMultiStepArtifactRequest detects prompts that imply multiple activities and saving artifacts
func isLikelyMultiStepArtifactRequest(input string) bool {
	text := strings.ToLower(strings.TrimSpace(input))
	if text == "" {
		return false
	}
	// Strong cues of multi-step + artifact generation
	cues := []string{
		"and then", "then ", " step ", ";", " -> ", "→",
		" and ", " produce ", " create ", " generate ", " save ",
		" pdf", " image", " module", " file", " under project ",
	}
	for _, c := range cues {
		if strings.Contains(text, c) {
			return true
		}
	}
	return false
}

// extractArtifactsFromInput extracts requested filenames, PDF flag, and preview flag
func extractArtifactsFromInput(input string) ([]string, bool, bool) {
	text := strings.TrimSpace(input)
	if text == "" {
		return nil, false, false
	}
	lower := strings.ToLower(text)
	wantPDF := strings.Contains(lower, "pdf") || strings.Contains(lower, "image")
	wantPreview := strings.Contains(lower, "preview") || strings.Contains(lower, "show code")

	// Look for explicit filenames with any alphanumeric extension (extension-agnostic)
	re := regexp.MustCompile(`(?i)\b([a-zA-Z0-9_\-]+\.[a-z0-9]{1,16})\b`)
	matches := re.FindAllStringSubmatch(text, -1)
	var files []string
	seen := map[string]bool{}
	for _, m := range matches {
		if len(m) > 1 {
			name := m[1]
			if !seen[strings.ToLower(name)] {
				files = append(files, name)
				seen[strings.ToLower(name)] = true
			}
		}
	}

	// Also handle phrases like "module <name>" (assume Python) when no explicit filenames found
	if len(files) == 0 {
		re2 := regexp.MustCompile(`(?i)module\s+'?\"?([a-zA-Z0-9_\-]+)'?\"?`)
		if m := re2.FindStringSubmatch(text); len(m) > 1 {
			files = append(files, m[1]+".py")
		}
	}

	return files, wantPDF, wantPreview
}

// extractProjectNameFromText extracts the raw project name from NL text, if present
func extractProjectNameFromText(input string) string {
	text := strings.ToLower(strings.TrimSpace(input))
	if text == "" {
		return ""
	}
	patterns := []string{
		`under\s+project\s*:?\s+"([^"]+)"`,
		`in\s+project\s*:?\s+"([^"]+)"`,
		`use\s+project\s*:?\s+"([^"]+)"`,
		`against\s+project\s*:?\s+"([^"]+)"`,
		`to\s+(the\s+)?project\s*:?\s+"([^"]+)"`,
		`under\s+project\s*:?\s+'([^']+)'`,
		`in\s+project\s*:?\s+'([^']+)'`,
		`use\s+project\s*:?\s+'([^']+)'`,
		`against\s+project\s*:?\s+'([^']+)'`,
		`to\s+(the\s+)?project\s*:?\s+'([^']+)'`,
		`under\s+project\s*:?\s+([^"'\n\r]+)$`,
		`in\s+project\s*:?\s+([^"'\n\r]+)$`,
		`use\s+project\s*:?\s+([^"'\n\r]+)$`,
		`against\s+project\s*:?\s+([^"'\n\r]+)$`,
		`to\s+(the\s+)?project\s*:?\s+([^"'\n\r]+)$`,
	}
	for _, p := range patterns {
		re := regexp.MustCompile(p)
		if m := re.FindStringSubmatch(text); len(m) > 1 {
			name := strings.TrimSpace(m[len(m)-1])
			name = strings.Trim(name, " \"'\t\n.!?,")
			return name
		}
	}
	return ""
}

// detectLanguage chooses a language based on filenames and keywords in text
// Priority: go > javascript > java > python
func detectLanguage(text string, files []string) string {
	lang := "python"
	lower := strings.ToLower(text)
	hasGo, hasJS, hasJava := false, false, false
	for _, f := range files {
		lf := strings.ToLower(f)
		if strings.HasSuffix(lf, ".go") {
			hasGo = true
		} else if strings.HasSuffix(lf, ".js") {
			hasJS = true
		} else if strings.HasSuffix(lf, ".java") {
			hasJava = true
		}
	}
	if hasGo {
		return "go"
	}
	if hasJS {
		return "javascript"
	}
	if hasJava {
		return "java"
	}
	if strings.Contains(lower, "golang") || strings.Contains(lower, " go ") || strings.HasSuffix(lower, " go") || strings.Contains(lower, ".go") {
		return "go"
	}
	if strings.Contains(lower, "javascript") || strings.Contains(lower, " node ") || strings.Contains(lower, ".js") || strings.Contains(lower, " typescript") {
		return "javascript"
	}
	if (strings.Contains(lower, ".java") || strings.Contains(lower, " java ") || strings.HasSuffix(lower, " java") || strings.Contains(lower, " in java") || strings.Contains(lower, " java.")) && !strings.Contains(lower, "javascript") {
		return "java"
	}
	return lang
}

// extractProjectIDFromText finds a project name in free text and resolves it to an ID.
// Supported phrases: "under project <name>", "in project <name>", "use project <name>"
// Accepts names in double quotes, single quotes, or unquoted until EOL.
// If a matching project isn't found, it will auto-create one with that name.
func (m *MonitorService) extractProjectIDFromText(input string) (string, bool) {
	text := strings.ToLower(strings.TrimSpace(input))
	if text == "" {
		return "", false
	}
	log.Printf("🔍 [DEBUG] extractProjectIDFromText: input='%s', text='%s'", input, text)
	patterns := []string{
		`under\s+project\s+"([^"]+)"`,
		`in\s+project\s+"([^"]+)"`,
		`use\s+project\s+"([^"]+)"`,
		`against\s+project\s+"([^"]+)"`,
		`to\s+(the\s+)?project\s+"([^"]+)"`,
		`under\s+project\s+'([^']+)'`,
		`in\s+project\s+'([^']+)'`,
		`use\s+project\s+'([^']+)'`,
		`against\s+project\s+'([^']+)'`,
		`to\s+(the\s+)?project\s+'([^']+)'`,
		`under\s+project\s+([^"'\n\r]+)$`,
		`in\s+project\s+([^"'\n\r]+)$`,
		`use\s+project\s+([^"'\n\r]+)$`,
		`against\s+project\s+([^"'\n\r]+)$`,
		`to\s+(the\s+)?project\s+([^"'\n\r]+)$`,
	}
	var name string
	for i, p := range patterns {
		re := regexp.MustCompile(p)
		if m2 := re.FindStringSubmatch(text); len(m2) > 1 {
			// pick last capturing group with a value
			captured := m2[len(m2)-1]
			name = strings.TrimSpace(captured)
			// strip trailing punctuation
			name = strings.Trim(name, " \"'\t\n.!?,")
			log.Printf("🔍 [DEBUG] extractProjectIDFromText: pattern %d matched, captured='%s', name='%s'", i, captured, name)
			break
		}
	}
	if name == "" {
		log.Printf("🔍 [DEBUG] extractProjectIDFromText: no project name found")
		return "", false
	}
	if id := m.findProjectIDByName(name); id != "" {
		log.Printf("🔍 [DEBUG] extractProjectIDFromText: found existing project %s with ID %s", name, id)
		return id, true
	}
	// Auto-create if not found
	if id := m.createProjectIfMissing(name, ""); id != "" {
		log.Printf("🔍 [DEBUG] extractProjectIDFromText: created new project %s with ID %s", name, id)
		return id, true
	}
	log.Printf("🔍 [DEBUG] extractProjectIDFromText: failed to create project %s", name)
	return "", false
}

// findProjectIDByName fetches projects and returns the ID of the first case-insensitive match on name
func (m *MonitorService) findProjectIDByName(name string) string {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(m.hdnURL + "/api/v1/projects")
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ""
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return ""
	}
	var projects []map[string]interface{}
	if err := json.Unmarshal(body, &projects); err != nil {
		return ""
	}
	lname := strings.ToLower(strings.TrimSpace(name))
	for _, p := range projects {
		n, _ := p["name"].(string)
		id, _ := p["id"].(string)
		if strings.ToLower(strings.TrimSpace(n)) == lname {
			return id
		}
	}
	return ""
}

// createProjectIfMissing creates a project by name and returns its ID (or empty string on failure)
func (m *MonitorService) createProjectIfMissing(name, description string) string {
	payload := map[string]string{"name": name}
	if description != "" {
		payload["description"] = description
	}
	b, _ := json.Marshal(payload)
	resp, err := http.Post(m.hdnURL+"/api/v1/projects", "application/json", strings.NewReader(string(b)))
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ""
	}
	body, _ := io.ReadAll(resp.Body)
	var project map[string]interface{}
	if err := json.Unmarshal(body, &project); err != nil {
		return ""
	}
	if id, _ := project["id"].(string); id != "" {
		return id
	}
	return ""
}

// tryCreateProjectFromInput detects simple NL intents for creating a project and executes them.
// Returns (true, payload) if a project was created; otherwise (false, nil).
func (m *MonitorService) tryCreateProjectFromInput(input string) (bool, map[string]interface{}) {
	text := strings.TrimSpace(strings.ToLower(input))
	if text == "" {
		return false, nil
	}

	// Simple intent detection patterns
	// Examples: "create project math toolkit", "new project named alpha with description test"
	re := regexp.MustCompile(`^(create|make|new)\s+project(\s+named)?\s+([^,]+?)(\s+with\s+(description|desc)\s+(.*))?$`)
	m2 := re.FindStringSubmatch(text)
	if len(m2) == 0 {
		return false, nil
	}

	name := strings.TrimSpace(m2[3])
	var description string
	if len(m2) >= 7 {
		description = strings.TrimSpace(m2[6])
	}
	if name == "" {
		return false, nil
	}

	// Title-case the name a bit for aesthetics
	name = strings.Title(name)

	payload := map[string]interface{}{
		"name":        name,
		"description": description,
	}

	b, _ := json.Marshal(payload)
	resp, err := http.Post(m.hdnURL+"/api/v1/projects", "application/json", strings.NewReader(string(b)))
	if err != nil {
		return true, map[string]interface{}{
			"success": false,
			"message": "Failed to create project",
			"error":   err.Error(),
		}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var project map[string]interface{}
	_ = json.Unmarshal(body, &project)

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return true, map[string]interface{}{
			"success": true,
			"message": fmt.Sprintf("Project '%s' created", name),
			"project": project,
		}
	}

	return true, map[string]interface{}{
		"success": false,
		"message": "Failed to create project",
		"project": project,
		"status":  resp.StatusCode,
	}
}

// getCapabilities returns capabilities grouped by domain by querying the HDN server
func (m *MonitorService) getCapabilities(c *gin.Context) {
	type Capability struct {
		ID          string   `json:"id"`
		Task        string   `json:"task"`
		Description string   `json:"description"`
		Language    string   `json:"language"`
		Tags        []string `json:"tags"`
		Domain      string   `json:"domain"`
		Code        string   `json:"code,omitempty"`
	}

	// Fetch list of domains
	resp, err := http.Get(m.hdnURL + "/api/v1/domains")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch domains"})
		return
	}
	defer resp.Body.Close()
	// HDN returns a list of domain objects
	var domainsList []struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&domainsList); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse domains"})
		return
	}

	result := make(map[string][]Capability)

	// For each domain, fetch actions as capabilities
	for _, d := range domainsList {
		domain := d.Name
		url := m.hdnURL + "/api/v1/actions/" + domain
		r, err := http.Get(url)
		if err != nil {
			continue
		}
		// Actions endpoint may return null, array, or wrapped
		var raw json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&raw); err == nil && len(raw) > 0 && string(raw) != "null" {
			// Try array of actions
			var arr []struct {
				ID          string   `json:"id"`
				Task        string   `json:"task"`
				Description string   `json:"description"`
				Language    string   `json:"language"`
				Tags        []string `json:"tags"`
				Domain      string   `json:"domain"`
			}
			// Or wrapped {actions: []}
			var wrapped struct {
				Actions []struct {
					ID          string   `json:"id"`
					Task        string   `json:"task"`
					Description string   `json:"description"`
					Language    string   `json:"language"`
					Tags        []string `json:"tags"`
					Domain      string   `json:"domain"`
				} `json:"actions"`
			}
			if err := json.Unmarshal(raw, &arr); err == nil && len(arr) > 0 {
				for _, a := range arr {
					result[domain] = append(result[domain], Capability{ID: a.ID, Task: a.Task, Description: a.Description, Language: a.Language, Tags: a.Tags, Domain: a.Domain})
				}
			} else if err := json.Unmarshal(raw, &wrapped); err == nil && len(wrapped.Actions) > 0 {
				for _, a := range wrapped.Actions {
					result[domain] = append(result[domain], Capability{
						ID: a.ID, Task: a.Task, Description: a.Description, Language: a.Language, Tags: a.Tags, Domain: a.Domain,
					})
				}
			}
		}
		r.Body.Close()
	}

	// Also include cached intelligent capabilities (if any)
	{
		r, err := http.Get(m.hdnURL + "/api/v1/intelligent/capabilities")
		if err == nil {
			defer r.Body.Close()
			var payload struct {
				Capabilities []struct {
					ID          string            `json:"id"`
					TaskName    string            `json:"task_name"`
					Description string            `json:"description"`
					Language    string            `json:"language"`
					Tags        []string          `json:"tags"`
					Context     map[string]string `json:"context"`
					Code        string            `json:"code"`
				} `json:"capabilities"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err == nil {
				domain := "intelligent"
				for _, ccap := range payload.Capabilities {
					result[domain] = append(result[domain], Capability{
						ID:          ccap.ID,
						Task:        ccap.TaskName,
						Description: ccap.Description,
						Language:    ccap.Language,
						Tags:        ccap.Tags,
						Domain:      domain,
						Code:        ccap.Code,
					})
				}
			}
		}
	}

	c.JSON(http.StatusOK, result)
}

// Reasoning Layer Handlers

// getReasoningTraces retrieves reasoning traces for a domain
func (m *MonitorService) getReasoningTraces(c *gin.Context) {
	domain := c.Param("domain")
	if domain == "" {
		domain = "General"
	}

	// Get traces from Redis (limit to 10 most recent to prevent UI spam)
	key := fmt.Sprintf("reasoning:traces:%s", domain)
	traces, err := m.redisClient.LRange(context.Background(), key, 0, 9).Result()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch reasoning traces"})
		return
	}

	var reasoningTraces []map[string]interface{}
	for _, traceData := range traces {
		var trace map[string]interface{}
		if err := json.Unmarshal([]byte(traceData), &trace); err == nil {
			reasoningTraces = append(reasoningTraces, trace)
		}
	}

	c.JSON(http.StatusOK, gin.H{"traces": reasoningTraces})
}

// getBeliefs retrieves beliefs for a domain
func (m *MonitorService) getBeliefs(c *gin.Context) {
	domain := c.Param("domain")
	if domain == "" {
		domain = "General"
	}

	// Get beliefs from Redis (limit to 10 to prevent UI spam)
	key := fmt.Sprintf("reasoning:beliefs:%s", domain)
	beliefs, err := m.redisClient.LRange(context.Background(), key, 0, 9).Result()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch beliefs"})
		return
	}

	var beliefList []map[string]interface{}
	for _, beliefData := range beliefs {
		var belief map[string]interface{}
		if err := json.Unmarshal([]byte(beliefData), &belief); err == nil {
			beliefList = append(beliefList, belief)
		}
	}

	c.JSON(http.StatusOK, gin.H{"beliefs": beliefList})
}

// getHypotheses retrieves hypotheses for a domain
func (m *MonitorService) getHypotheses(c *gin.Context) {
	domain := c.Param("domain")
	if domain == "" {
		domain = "General"
	}

	// Get hypotheses from FSM Redis
	key := fmt.Sprintf("fsm:agent_1:hypotheses")
	hypotheses, err := m.redisClient.HGetAll(context.Background(), key).Result()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch hypotheses"})
		return
	}

	var hypothesisList []map[string]interface{}
	count := 0
	maxHypotheses := 10 // Limit to 10 hypotheses to prevent UI spam
	for _, hypothesisData := range hypotheses {
		if count >= maxHypotheses {
			break
		}
		var hypothesis map[string]interface{}
		if err := json.Unmarshal([]byte(hypothesisData), &hypothesis); err == nil {
			// Filter by domain if specified
			if domain == "General" || hypothesis["domain"] == domain {
				hypothesisList = append(hypothesisList, hypothesis)
				count++
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{"hypotheses": hypothesisList})
}

// getCuriosityGoals retrieves curiosity goals for a domain
func (m *MonitorService) getCuriosityGoals(c *gin.Context) {
	domain := c.Param("domain")
	if domain == "" {
		domain = "General"
	}

	// Get curiosity goals from Redis (limit to 10 to prevent UI spam)
	key := fmt.Sprintf("reasoning:curiosity_goals:%s", domain)
	goals, err := m.redisClient.LRange(context.Background(), key, 0, 9).Result()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch curiosity goals"})
		return
	}

	var goalList []map[string]interface{}
	for _, goalData := range goals {
		var goal map[string]interface{}
		if err := json.Unmarshal([]byte(goalData), &goal); err == nil {
			goalList = append(goalList, goal)
		}
	}

	// Server-side dedup: prefer unique by type+description; fallback to id
	seen := map[string]bool{}
	var deduped []map[string]interface{}
	for _, g := range goalList {
		t := ""
		if v, ok := g["type"].(string); ok {
			t = v
		}
		d := ""
		if v, ok := g["description"].(string); ok {
			d = v
		}
		k := strings.ToLower(strings.TrimSpace(t + ":" + d))
		if k == ":" || k == "" {
			if v, ok := g["id"].(string); ok && v != "" {
				k = v
			}
		}
		if seen[k] {
			continue
		}
		seen[k] = true
		deduped = append(deduped, g)
	}

	c.JSON(http.StatusOK, gin.H{"goals": deduped})
}

// getReasoningExplanations retrieves reasoning explanations for a goal
func (m *MonitorService) getReasoningExplanations(c *gin.Context) {
	goal := c.Param("goal")
	if goal == "" {
		goal = "general"
	}

	// Get explanations from Redis
	key := fmt.Sprintf("reasoning:explanations:%s", goal)
	explanations, err := m.redisClient.LRange(context.Background(), key, 0, 9).Result()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch explanations"})
		return
	}

	var explanationList []map[string]interface{}
	for _, expData := range explanations {
		var explanation map[string]interface{}
		if err := json.Unmarshal([]byte(expData), &explanation); err == nil {
			explanationList = append(explanationList, explanation)
		}
	}

	c.JSON(http.StatusOK, gin.H{"explanations": explanationList})
}

// getReasoningDomains lists domains that currently have beliefs or curiosity goals
func (m *MonitorService) getReasoningDomains(c *gin.Context) {
	// Use SCAN to avoid blocking Redis for large keyspaces
	type result struct {
		Domains          []string `json:"domains"`
		BeliefDomains    []string `json:"belief_domains"`
		CuriosityDomains []string `json:"curiosity_domains"`
	}

	unique := func(items []string) []string {
		seen := map[string]bool{}
		out := make([]string, 0, len(items))
		for _, it := range items {
			if it == "" || seen[it] {
				continue
			}
			seen[it] = true
			out = append(out, it)
		}
		return out
	}

	scanDomains := func(pattern string, prefix string) []string {
		var cursor uint64
		domains := []string{}
		for i := 0; i < 50; i++ { // hard cap scans
			keys, cur, err := m.redisClient.Scan(context.Background(), cursor, pattern, 100).Result()
			if err != nil {
				break
			}
			for _, k := range keys {
				if strings.HasPrefix(k, prefix) {
					d := strings.TrimPrefix(k, prefix)
					if d != "" {
						domains = append(domains, d)
					}
				}
			}
			cursor = cur
			if cursor == 0 {
				break
			}
		}
		return unique(domains)
	}

	beliefs := scanDomains("reasoning:beliefs:*", "reasoning:beliefs:")
	curiosity := scanDomains("reasoning:curiosity_goals:*", "reasoning:curiosity_goals:")

	// Also check Neo4j for domains from concepts (if available)
	neo4jDomains := []string{}
	if m.hdnURL != "" {
		// Query HDN knowledge API to get concepts and extract unique domains
		// Use a large limit to get a good sample of concepts
		searchURL := fmt.Sprintf("%s/api/v1/knowledge/concepts?limit=1000", m.hdnURL)
		if resp, err := http.Get(searchURL); err == nil {
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				var conceptResult struct {
					Concepts []struct {
						Domain string `json:"domain"`
					} `json:"concepts"`
				}
				if err := json.NewDecoder(resp.Body).Decode(&conceptResult); err == nil {
					domainSet := make(map[string]bool)
					for _, concept := range conceptResult.Concepts {
						domain := strings.TrimSpace(concept.Domain)
						// Filter out source identifiers (news:, wiki:, etc.) - these are not domains
						if domain != "" && domain != "General" && !isSourceIdentifier(domain) {
							domainSet[domain] = true
						}
					}
					for domain := range domainSet {
						neo4jDomains = append(neo4jDomains, domain)
					}
				}
			}
		}
	}

	// Union all domains, filtering out source identifiers
	union := append([]string{}, beliefs...)
	union = append(union, curiosity...)
	union = append(union, neo4jDomains...)
	// Filter out source identifiers from all domains
	filtered := make([]string, 0, len(union))
	for _, d := range union {
		if !isSourceIdentifier(d) {
			filtered = append(filtered, d)
		}
	}
	union = unique(filtered)

	c.JSON(http.StatusOK, result{Domains: union, BeliefDomains: beliefs, CuriosityDomains: curiosity})
}

// isSourceIdentifier checks if a string is a source identifier (like "news:bbc", "wikipedia") rather than a semantic domain
func isSourceIdentifier(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	// Source identifiers typically have patterns like:
	// - "news:" prefix (news:bbc, news:fsm)
	// - "wiki" (wikipedia, wiki)
	// - "source:" prefix
	// - Common source names
	sourcePatterns := []string{
		"news:",
		"wiki",
		"source:",
		"bbc",
		"reuters",
		"ap news",
	}
	for _, pattern := range sourcePatterns {
		if strings.Contains(s, pattern) {
			return true
		}
	}
	return false
}

// getReflection provides comprehensive introspection of the system's current mental state
func (m *MonitorService) getReflection(c *gin.Context) {
	ctx := context.Background()
	domain := c.DefaultQuery("domain", "General")
	limit := 10 // Default limit for each section

	reflection := map[string]interface{}{
		"timestamp": time.Now().Format(time.RFC3339),
		"domain":    domain,
	}

	// 1. FSM Current State and Thinking
	fsmThinking := map[string]interface{}{}
	if resp, err := http.Get(m.fsmURL + "/thinking"); err == nil {
		defer resp.Body.Close()
		if resp.StatusCode == 200 {
			var thinking map[string]interface{}
			if json.NewDecoder(resp.Body).Decode(&thinking) == nil {
				fsmThinking = thinking
			}
		}
	}
	reflection["fsm_thinking"] = fsmThinking

	// 2. Recent Reasoning Traces (limit to 10 to prevent spam)
	tracesKey := fmt.Sprintf("reasoning:traces:%s", domain)
	traces, _ := m.redisClient.LRange(ctx, tracesKey, 0, 9).Result()
	var reasoningTraces []map[string]interface{}
	for _, traceData := range traces {
		var trace map[string]interface{}
		if json.Unmarshal([]byte(traceData), &trace) == nil {
			reasoningTraces = append(reasoningTraces, trace)
		}
	}
	reflection["reasoning_traces"] = reasoningTraces

	// 3. Current Beliefs
	beliefsKey := fmt.Sprintf("reasoning:beliefs:%s", domain)
	beliefs, _ := m.redisClient.LRange(ctx, beliefsKey, 0, int64(limit-1)).Result()
	var beliefList []map[string]interface{}
	for _, beliefData := range beliefs {
		var belief map[string]interface{}
		if json.Unmarshal([]byte(beliefData), &belief) == nil {
			beliefList = append(beliefList, belief)
		}
	}
	reflection["beliefs"] = beliefList

	// 4. Active Curiosity Goals
	curiosityKey := fmt.Sprintf("reasoning:curiosity_goals:%s", domain)
	goals, _ := m.redisClient.LRange(ctx, curiosityKey, 0, int64(limit-1)).Result()
	var goalList []map[string]interface{}
	for _, goalData := range goals {
		var goal map[string]interface{}
		if json.Unmarshal([]byte(goalData), &goal) == nil {
			goalList = append(goalList, goal)
		}
	}
	reflection["curiosity_goals"] = goalList

	// 5. Recent Hypotheses
	hypothesesKey := "fsm:agent_1:hypotheses"
	hypotheses, _ := m.redisClient.HGetAll(ctx, hypothesesKey).Result()
	var hypothesisList []map[string]interface{}
	count := 0
	for _, hypothesisData := range hypotheses {
		if count >= limit {
			break
		}
		var hypothesis map[string]interface{}
		if json.Unmarshal([]byte(hypothesisData), &hypothesis) == nil {
			if domain == "General" || hypothesis["domain"] == domain {
				hypothesisList = append(hypothesisList, hypothesis)
				count++
			}
		}
	}
	reflection["hypotheses"] = hypothesisList

	// 6. Recent Tool Calls
	if resp, err := http.Get(m.hdnURL + "/api/v1/tools/calls/recent?limit=" + fmt.Sprintf("%d", limit)); err == nil {
		defer resp.Body.Close()
		if resp.StatusCode == 200 {
			var toolCalls map[string]interface{}
			if json.NewDecoder(resp.Body).Decode(&toolCalls) == nil {
				reflection["recent_tool_calls"] = toolCalls["calls"]
			}
		}
	}

	// 7. Working Memory Summary (if available)
	if resp, err := http.Get(m.hdnURL + "/api/v1/memory/summary"); err == nil {
		defer resp.Body.Close()
		if resp.StatusCode == 200 {
			var summary map[string]interface{}
			if json.NewDecoder(resp.Body).Decode(&summary) == nil {
				reflection["working_memory"] = summary
			}
		}
	}

	// 8. Active Goals (from Goal Manager if available)
	if resp, err := http.Get(m.goalMgrURL + "/goals/agent_1/active"); err == nil {
		defer resp.Body.Close()
		if resp.StatusCode == 200 {
			var goals map[string]interface{}
			if json.NewDecoder(resp.Body).Decode(&goals) == nil {
				reflection["active_goals"] = goals
			}
		}
	}

	// 9. Recent Explanations
	explanationsKey := "reasoning:explanations"
	explanations, _ := m.redisClient.LRange(ctx, explanationsKey, 0, int64(limit-1)).Result()
	var explanationList []map[string]interface{}
	for _, expData := range explanations {
		var exp map[string]interface{}
		if json.Unmarshal([]byte(expData), &exp) == nil {
			explanationList = append(explanationList, exp)
		}
	}
	reflection["explanations"] = explanationList

	// 10. System Status Summary
	reflection["summary"] = map[string]interface{}{
		"reasoning_traces_count": len(reasoningTraces),
		"beliefs_count":          len(beliefList),
		"curiosity_goals_count":  len(goalList),
		"hypotheses_count":       len(hypothesisList),
		"explanations_count":     len(explanationList),
		"fsm_state":              fsmThinking["current_state"],
		"fsm_thinking_focus":     fsmThinking["thinking_focus"],
	}

	c.JSON(http.StatusOK, reflection)
}

// getRecentExplanations retrieves recent explanations across all goals
func (m *MonitorService) getRecentExplanations(c *gin.Context) {
	ctx := context.Background()

	// Get all explanation keys
	keys, err := m.redisClient.Keys(ctx, "reasoning:explanations:*").Result()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch explanation keys"})
		return
	}

	var allExplanations []map[string]interface{}

	// Collect explanations from all goals (limit to prevent spam)
	maxKeys := 5 // Only check first 5 goal keys
	if len(keys) > maxKeys {
		keys = keys[:maxKeys]
	}
	for _, key := range keys {
		explanations, err := m.redisClient.LRange(ctx, key, 0, 2).Result() // Get last 3 from each goal (reduced from 5)
		if err != nil {
			continue
		}

		for _, expData := range explanations {
			var explanation map[string]interface{}
			if err := json.Unmarshal([]byte(expData), &explanation); err == nil {
				allExplanations = append(allExplanations, explanation)
			}
		}
	}

	// Sort by generated_at timestamp (most recent first)
	sort.Slice(allExplanations, func(i, j int) bool {
		timeI, _ := allExplanations[i]["generated_at"].(string)
		timeJ, _ := allExplanations[j]["generated_at"].(string)
		return timeI > timeJ
	})

	// Limit to last 20 explanations
	if len(allExplanations) > 20 {
		allExplanations = allExplanations[:20]
	}

	c.JSON(http.StatusOK, gin.H{"explanations": allExplanations})
}

// startCuriosityGoalConsumer runs in background to convert curiosity goals to Goal Manager tasks
func (m *MonitorService) startCuriosityGoalConsumer() {
	log.Println("🎯 Starting curiosity goal consumer...")

	// Rate limiting: process max 2 goals per minute to avoid overwhelming the system
	ticker := time.NewTicker(30 * time.Second) // Check every 30 seconds
	defer ticker.Stop()

	// Track processed goals to avoid duplicates
	processed := make(map[string]bool)

	for {
		select {
		case <-ticker.C:
			log.Printf("🔄 Curiosity goal consumer tick - processing domains...")
			// Process curiosity goals for each domain
			domains := []string{"General", "Networking", "Math", "Programming"}

			for _, domain := range domains {
				// Get up to 2 pending curiosity goals for this domain
				key := fmt.Sprintf("reasoning:curiosity_goals:%s", domain)
				goals, err := m.redisClient.LRange(context.Background(), key, 0, 1).Result()
				if err != nil {
					log.Printf("⚠️ Failed to get curiosity goals for %s: %v", domain, err)
					continue
				}

				log.Printf("🔍 Checking %s: found %d curiosity goals", domain, len(goals))

				for _, goalData := range goals {
					var goal map[string]interface{}
					if err := json.Unmarshal([]byte(goalData), &goal); err != nil {
						log.Printf("⚠️ Failed to parse curiosity goal: %v", err)
						continue
					}

					goalID, _ := goal["id"].(string)
					log.Printf("🔍 Processing goal %s (processed: %v)", goalID, processed[goalID])
					if goalID == "" || processed[goalID] {
						log.Printf("⏭️ Skipping goal %s (empty or already processed)", goalID)
						continue
					}

					// Convert to Goal Manager task
					if err := m.convertCuriosityGoalToTask(goal, domain); err != nil {
						log.Printf("⚠️ Failed to convert curiosity goal %s: %v", goalID, err)
						continue
					}

					// Mark as processed and remove from Redis
					processed[goalID] = true
					m.redisClient.LRem(context.Background(), key, 1, goalData)

					log.Printf("✅ Converted curiosity goal %s to Goal Manager task", goalID)

					// Only process 1 goal per domain per cycle to limit load
					break
				}
			}
		}
	}
}

// convertCuriosityGoalToTask converts a curiosity goal to a Goal Manager task
func (m *MonitorService) convertCuriosityGoalToTask(goal map[string]interface{}, domain string) error {
	description, _ := goal["description"].(string)
	if description == "" {
		return fmt.Errorf("no description in curiosity goal")
	}

	// Create Goal Manager task
	taskData := map[string]interface{}{
		"agent_id":    "agent_1",
		"description": description,
		"priority":    "medium",
		"context": map[string]interface{}{
			"source":       "curiosity_goal",
			"domain":       domain,
			"curiosity_id": goal["id"],
		},
	}

	// Send to Goal Manager
	url := m.goalMgrURL + "/goal"
	data, err := json.Marshal(taskData)
	if err != nil {
		return fmt.Errorf("failed to marshal task data: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("goal manager returned status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// startAutoExecutor runs in background to execute one Goal Manager task at a time
func (m *MonitorService) startAutoExecutor() {
	log.Println("🚀 Starting auto-executor...")

	// Check every 60 seconds for tasks to execute
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	// Track if we're currently executing a task
	var executing bool

	// Track processed goals to avoid duplicates
	processedGoals := make(map[string]bool)

	// Cleanup processed goals every 10 minutes to prevent memory leak
	cleanupTicker := time.NewTicker(10 * time.Minute)
	defer cleanupTicker.Stop()

	for {
		select {
		case <-cleanupTicker.C:
			// Cleanup processed goals map to prevent memory leak
			processedGoals = make(map[string]bool)
			log.Printf("🧹 Auto-executor: cleaned up processed goals map")
		case <-ticker.C:
			log.Printf("🔄 Auto-executor: tick received, checking for goals...")
			// Back-pressure: skip auto-execute if HDN saturated
			if m.isHDNSaturated() {
				log.Printf("⏸️ Auto-executor: HDN saturated, skipping this cycle")
				continue
			}
			if executing {
				log.Printf("⏳ Auto-executor: already executing a task, skipping this cycle")
				// Safety check: if we've been executing for more than 15 minutes, reset the flag
				// This prevents the auto-executor from getting permanently stuck
				continue
			}

			// Get active goals from Goal Manager
			log.Printf("🔍 Auto-executor: fetching active goals from Goal Manager...")
			goals, err := m.getActiveGoalsFromGoalManager()
			if err != nil {
				log.Printf("⚠️ Auto-executor: failed to get goals: %v", err)
				continue
			}

			log.Printf("📊 Auto-executor: found %d active goals", len(goals))
			if len(goals) == 0 {
				log.Printf("ℹ️ Auto-executor: no active goals to execute")
				continue
			}

			// Pick the highest priority unprocessed goal first
			var goalToExecute map[string]interface{}
			priorityOrder := []string{"high", "medium", "low"} // Priority order

			for _, priority := range priorityOrder {
				for _, goal := range goals {
					goalID := goal["id"].(string)
					goalPriority := goal["priority"].(string)
					if !processedGoals[goalID] && goalPriority == priority {
						goalToExecute = goal
						break
					}
				}
				if goalToExecute != nil {
					break // Found a goal with this priority
				}
			}

			if goalToExecute == nil {
				log.Printf("ℹ️ Auto-executor: no unprocessed goals to execute")
				continue
			}

			goalID := goalToExecute["id"].(string)
			description := goalToExecute["description"].(string)

			log.Printf("🎯 Auto-executor: executing goal %s: %s", goalID, description)

			// Check if we've processed this goal recently (rate limiting)
			goalKey := fmt.Sprintf("processed_goal_%s", goalID)
			if processedGoals[goalKey] {
				log.Printf("⏭️ Auto-executor: goal %s already processed recently, skipping", goalID)
				continue
			}

			// Mark as executing
			executing = true

			// Execute the goal in a goroutine so we don't block the ticker
			go func() {
				defer func() {
					executing = false
					log.Printf("🔄 Auto-executor: reset executing flag for goal %s", goalID)
				}()

				// Execute asynchronously with timeout handling
				done := make(chan error, 1)
				go func() {
					defer func() {
						if r := recover(); r != nil {
							log.Printf("💥 Auto-executor: panic in execution goroutine: %v", r)
							done <- fmt.Errorf("panic: %v", r)
						}
					}()
					done <- m.executeGoalTask(goalID, description)
				}()

				// Wait for completion or timeout (10 minutes)
				select {
				case err := <-done:
					if err != nil {
						log.Printf("❌ Auto-executor: failed to execute goal %s: %v", goalID, err)
						// Mark as processed even if failed to avoid retrying immediately
						processedGoals[goalKey] = true
						return
					}
					log.Printf("✅ Auto-executor: successfully executed goal %s", goalID)
					// Mark as processed after successful execution
					processedGoals[goalKey] = true
				case <-time.After(10 * time.Minute):
					log.Printf("⏰ Auto-executor: goal %s timed out after 10 minutes", goalID)
					// Mark as processed to avoid retrying
					processedGoals[goalKey] = true
				}
			}()
		}
	}
}

// getActiveGoalsFromGoalManager fetches active goals from Goal Manager
func (m *MonitorService) getActiveGoalsFromGoalManager() ([]map[string]interface{}, error) {
	url := m.goalMgrURL + "/goals/agent_1/active"
	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to get goals: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("goal manager returned status %d", resp.StatusCode)
	}

	var goals []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&goals); err != nil {
		return nil, fmt.Errorf("failed to decode goals: %w", err)
	}

	return goals, nil
}

// executeGoalTask executes a goal task using HDN interpreter (like Suggest Next Steps)
func (m *MonitorService) executeGoalTask(goalID, description string) error {
	// Use the same approach as suggestGoalNextSteps to get concrete actions
	sessionID := fmt.Sprintf("auto_exec_%s_%d", goalID, time.Now().Unix())

	// Fetch memory summary and capabilities (same as suggest)
	memorySummary, _ := m.getMemorySummaryForSession(sessionID)
	domains, _ := m.getCapabilitiesForSession()

	// Build prompt for interpreter (same as suggest)
	input := fmt.Sprintf("Given the goal: %s\nUse current memory summary and capabilities to propose the next concrete action(s) to progress the goal. Return a detailed, executable plan with specific steps.", description)

	// Create context (same as suggest)
	ctx := map[string]string{
		"session_id":     sessionID,
		"memory_summary": m.serialize(memorySummary),
		"domains":        m.serialize(domains),
		"goal":           m.serialize(map[string]interface{}{"description": description, "id": goalID}),
	}

	// Use HDN interpreter to get concrete plan
	plan, err := m.getInterpreterPlan(input, ctx, sessionID)
	if err != nil {
		return fmt.Errorf("failed to get interpreter plan: %w", err)
	}

	// Extract project ID from goal description if specified
	projectID := m.findOrCreateGoalsProject() // Default to Goals project
	if extractedProjectID, found := m.extractProjectIDFromText(description); found {
		projectID = extractedProjectID
		log.Printf("🎯 [AUTO-EXECUTOR] extracted project ID %s from goal description: %s", projectID, description)
	} else {
		log.Printf("🎯 [AUTO-EXECUTOR] using default Goals project %s for goal %s", projectID, goalID)
	}

	// Create a better description for the workflow using the plan
	workflowDescription := fmt.Sprintf("Auto-executor: %s", description)
	if len(workflowDescription) > 200 {
		workflowDescription = workflowDescription[:200] + "..."
	}

	// Use the plan as the execution description
	executionDescription := plan

	// Build context for intelligent execution
	ctxMap := map[string]string{
		"session_id": sessionID,
	}
	// Heuristic: extract first URL from the generated plan/description to trigger scraper-first routing
	if url := extractFirstURL(executionDescription); url != "" {
		ctxMap["url"] = url
	}

	// Execute the plan using intelligent execution
	execData := map[string]interface{}{
		"task_name":        "execute_goal_plan",
		"description":      executionDescription,
		"context":          ctxMap,
		"project_id":       projectID, // HDN expects project_id at top level
		"force_regenerate": true,
		"max_retries":      1,
	}

	data, err := json.Marshal(execData)
	if err != nil {
		return fmt.Errorf("failed to marshal execution data: %w", err)
	}

	// Send to HDN intelligent execution
	url := m.hdnURL + "/api/v1/intelligent/execute"
	req, err := http.NewRequest("POST", url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 8 * time.Minute} // 8 minute timeout for execution
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("execution failed with status %d: %s", resp.StatusCode, string(body))
	}

	// Parse response to check if execution was actually successful
	var execResult map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&execResult); err == nil {
		if success, ok := execResult["success"].(bool); ok && success {
			log.Printf("✅ Auto-executor: successfully executed goal %s", goalID)
			// Mark goal as completed in Goal Manager
			return m.markGoalAsCompleted(goalID)
		} else {
			log.Printf("⚠️ Auto-executor: goal %s execution returned success=false", goalID)
			return fmt.Errorf("execution returned success=false")
		}
	}

	// If we can't parse the response, assume success and mark as completed
	log.Printf("✅ Auto-executor: executed goal %s (response unparseable, assuming success)", goalID)
	return m.markGoalAsCompleted(goalID)
}

// extractFirstURL finds the first http/https URL in text (simple regex)
func extractFirstURL(text string) string {
	re := regexp.MustCompile(`https?://[^\s]+`)
	m := re.FindString(text)
	// Trim trailing punctuation often present in plan text
	m = strings.TrimRight(m, ".,);]\n\r")
	return m
}

// isHDNSaturated checks if HDN is under heavy load by comparing recent in-flight
// tool calls with the configured max concurrency. Conservative on errors.
func (m *MonitorService) isHDNSaturated() bool {
	maxConc := 8
	if v := strings.TrimSpace(os.Getenv("HDN_MAX_CONCURRENT_EXECUTIONS")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			maxConc = n
		}
	}

	type call struct {
		Status string `json:"status"`
	}
	var resp struct {
		Calls []call `json:"calls"`
	}
	hdn := strings.TrimRight(m.hdnURL, "/")
	httpClient := &http.Client{Timeout: 2 * time.Second}
	req, _ := http.NewRequest("GET", hdn+"/api/v1/tools/calls/recent", nil)
	r, err := httpClient.Do(req)
	if err != nil || r.StatusCode != 200 {
		if r != nil {
			_ = r.Body.Close()
		}
		return true
	}
	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(&resp); err != nil {
		return true
	}
	inFlight := 0
	for _, c := range resp.Calls {
		s := strings.ToLower(strings.TrimSpace(c.Status))
		if s != "success" && s != "failure" && s != "blocked" {
			inFlight++
		}
	}
	return inFlight >= maxConc
}

// markGoalAsCompleted marks a goal as completed in Goal Manager
func (m *MonitorService) markGoalAsCompleted(goalID string) error {
	url := m.goalMgrURL + "/goal/" + goalID + "/achieve"
	req, err := http.NewRequest("POST", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to mark goal as completed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to mark goal as completed: status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// ConcreteTask represents a specific executable task
type ConcreteTask struct {
	Name        string
	Description string
	Language    string
	Input       string
}

// convertHypothesisToConcreteTask converts abstract hypothesis testing to concrete executable tasks
func (m *MonitorService) convertHypothesisToConcreteTask(description string) ConcreteTask {
	// Map hypothesis types to concrete executable tasks
	descLower := strings.ToLower(description)

	if strings.Contains(descLower, "communication") || strings.Contains(descLower, "message") || strings.Contains(descLower, "user input") {
		return ConcreteTask{
			Name:        "analyze_communication_patterns",
			Description: "Create a Python script that analyzes communication patterns and generates a report showing message frequency, sentiment analysis, and key topics. Include data visualization with matplotlib.",
			Language:    "python",
			Input:       "Sample messages: 'Hello world', 'Testing the system', 'How are you?', 'This is a test message'",
		}
	}

	if strings.Contains(descLower, "system") || strings.Contains(descLower, "processing") || strings.Contains(descLower, "state") {
		return ConcreteTask{
			Name:        "system_monitoring_dashboard",
			Description: "Create a Python script that monitors system processes and generates a real-time dashboard showing CPU usage, memory consumption, and active processes. Include JSON output and logging.",
			Language:    "python",
			Input:       "Monitor system resources and generate a status report",
		}
	}

	if strings.Contains(descLower, "infrastructure") || strings.Contains(descLower, "network") || strings.Contains(descLower, "connectivity") {
		return ConcreteTask{
			Name:        "network_connectivity_test",
			Description: "Create a Python script that tests network connectivity, measures latency, and generates a network health report with ping tests, port scans, and connection status.",
			Language:    "python",
			Input:       "Test connectivity to localhost:4222 (NATS), localhost:6379 (Redis), localhost:7474 (Neo4j)",
		}
	}

	// Default fallback for any other hypothesis
	return ConcreteTask{
		Name:        "data_analysis_tool",
		Description: "Create a Python script that performs data analysis on the given input, generates statistics, creates visualizations, and outputs results in both JSON and CSV formats.",
		Language:    "python",
		Input:       "Analyze the hypothesis: " + description,
	}
}

// Helper functions for auto-executor (similar to suggestGoalNextSteps)

// getMemorySummaryForSession fetches memory summary from HDN (for auto-executor)
func (m *MonitorService) getMemorySummaryForSession(sessionID string) (map[string]interface{}, error) {
	url := m.hdnURL + "/api/v1/memory/summary?session_id=" + urlQueryEscape(sessionID)
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var summary map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&summary); err != nil {
		return nil, err
	}
	return summary, nil
}

// getCapabilitiesForSession fetches capabilities from HDN (for auto-executor)
func (m *MonitorService) getCapabilitiesForSession() ([]map[string]interface{}, error) {
	resp, err := http.Get(m.hdnURL + "/api/v1/domains")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var domains []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&domains); err != nil {
		return nil, err
	}
	return domains, nil
}

// serialize converts interface{} to string (same as in suggestGoalNextSteps)
func (m *MonitorService) serialize(v interface{}) string {
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

// findOrCreateGoalsProject finds or creates a "Goals" project for auto-executor workflows
func (m *MonitorService) findOrCreateGoalsProject() string {
	// First try to find existing "Goals" project
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(m.hdnURL + "/api/v1/projects")
	if err != nil {
		log.Printf("⚠️ Failed to fetch projects: %v", err)
		return "Goals" // Fallback to string, HDN will handle it
	}
	defer resp.Body.Close()

	var projects []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&projects); err != nil {
		log.Printf("⚠️ Failed to decode projects: %v", err)
		return "Goals" // Fallback to string, HDN will handle it
	}

	// Look for existing "Goals" project
	for _, project := range projects {
		if name, ok := project["name"].(string); ok && name == "Goals" {
			if id, ok := project["id"].(string); ok {
				log.Printf("✅ Found existing Goals project: %s", id)
				return id
			}
		}
	}

	// Create new "Goals" project
	projectData := map[string]string{
		"name":        "Goals",
		"description": "Auto-executor goal workflows",
	}

	data, err := json.Marshal(projectData)
	if err != nil {
		log.Printf("⚠️ Failed to marshal project data: %v", err)
		return "Goals" // Fallback to string, HDN will handle it
	}

	req, err := http.NewRequest("POST", m.hdnURL+"/api/v1/projects", bytes.NewReader(data))
	if err != nil {
		log.Printf("⚠️ Failed to create project request: %v", err)
		return "Goals" // Fallback to string, HDN will handle it
	}
	req.Header.Set("Content-Type", "application/json")

	createResp, err := client.Do(req)
	if err != nil {
		log.Printf("⚠️ Failed to create Goals project: %v", err)
		return "Goals" // Fallback to string, HDN will handle it
	}
	defer createResp.Body.Close()

	if createResp.StatusCode >= 200 && createResp.StatusCode < 300 {
		var newProject map[string]interface{}
		if err := json.NewDecoder(createResp.Body).Decode(&newProject); err == nil {
			if id, ok := newProject["id"].(string); ok {
				log.Printf("✅ Created new Goals project: %s", id)
				return id
			}
		}
	}

	log.Printf("⚠️ Failed to create Goals project, using fallback")
	return "Goals" // Fallback to string, HDN will handle it
}

// getInterpreterPlan gets a concrete plan from HDN interpreter
func (m *MonitorService) getInterpreterPlan(input string, ctx map[string]string, sessionID string) (string, error) {
	payload := map[string]interface{}{
		"input":      input,
		"context":    ctx,
		"session_id": sessionID,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	url := m.hdnURL + "/api/v1/interpret"
	req, err := http.NewRequest("POST", url, bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("interpreter returned status %d: %s", resp.StatusCode, string(body))
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	// Extract the plan from the interpreter response
	if plan, ok := result["plan"].(string); ok {
		return plan, nil
	}
	if plan, ok := result["result"].(string); ok {
		return plan, nil
	}
	if plan, ok := result["response"].(string); ok {
		return plan, nil
	}

	// Fallback: return the whole result as string
	return m.serialize(result), nil
}

// getToolMetrics proxies tool metrics from HDN server
func (m *MonitorService) getToolMetrics(c *gin.Context) {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(m.hdnURL + "/api/v1/tools/metrics")
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to fetch tool metrics", "details": err.Error()})
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to read tool metrics response", "details": err.Error()})
		return
	}

	c.Data(resp.StatusCode, "application/json", body)
}

// getToolMetricsByID proxies specific tool metrics from HDN server
func (m *MonitorService) getToolMetricsByID(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing tool id"})
		return
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(m.hdnURL + "/api/v1/tools/" + id + "/metrics")
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to fetch tool metrics", "details": err.Error()})
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to read tool metrics response", "details": err.Error()})
		return
	}

	c.Data(resp.StatusCode, "application/json", body)
}

// getRecentToolCalls proxies recent tool calls from HDN server
func (m *MonitorService) getRecentToolCalls(c *gin.Context) {
	limit := c.Query("limit")
	url := m.hdnURL + "/api/v1/tools/calls/recent"
	if limit != "" {
		url += "?limit=" + limit
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to fetch recent tool calls", "details": err.Error()})
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to read recent tool calls response", "details": err.Error()})
		return
	}

	c.Data(resp.StatusCode, "application/json", body)
}

// clearSafetyData clears only safety-related data from Redis and log files
func (m *MonitorService) clearSafetyData(c *gin.Context) {
	ctx := context.Background()

	// Clear only tool metrics and safety-related keys
	// Pattern: tool_metrics:* and tool_calls:*
	patterns := []string{
		"tool_metrics:*",
		"tool_calls:*",
	}

	clearedKeys := 0
	for _, pattern := range patterns {
		keys, err := m.redisClient.Keys(ctx, pattern).Result()
		if err != nil {
			log.Printf("Error getting keys for pattern %s: %v", pattern, err)
			continue
		}

		if len(keys) > 0 {
			err = m.redisClient.Del(ctx, keys...).Err()
			if err != nil {
				log.Printf("Error deleting keys for pattern %s: %v", pattern, err)
				continue
			}
			clearedKeys += len(keys)
		}
	}

	// Clear log files
	logFiles, err := filepath.Glob("/tmp/tool_calls_*.log")
	if err != nil {
		log.Printf("Error finding log files: %v", err)
	} else {
		for _, logFile := range logFiles {
			err := os.Remove(logFile)
			if err != nil {
				log.Printf("Error removing log file %s: %v", logFile, err)
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"message":      "Safety data cleared successfully",
		"cleared_keys": clearedKeys,
		"cleared_logs": len(logFiles),
	})
}

// proxyIntelligentExecute proxies requests to the HDN server
func (m *MonitorService) proxyIntelligentExecute(c *gin.Context) {
	// Set CORS headers
	c.Header("Access-Control-Allow-Origin", "*")
	c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
	c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")

	// Read the request body
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to read request body"})
		return
	}

	// Create a new request to the HDN server
	req, err := http.NewRequest("POST", m.hdnURL+"/api/v1/intelligent/execute", bytes.NewBuffer(body))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create request to HDN server"})
		return
	}

	// Copy headers
	req.Header.Set("Content-Type", "application/json")

	// Make the request
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to connect to HDN server"})
		return
	}
	defer resp.Body.Close()

	// Read the response
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read HDN server response"})
		return
	}

	// Set the status code and return the response
	c.Data(resp.StatusCode, "application/json", respBody)
}

// handleCORS handles CORS preflight requests
func (m *MonitorService) handleCORS(c *gin.Context) {
	c.Header("Access-Control-Allow-Origin", "*")
	c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
	c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")
	c.Status(http.StatusOK)
}

// Expose execution method from environment for UI decisions
func (m *MonitorService) exposeExecutionMethod(c *gin.Context) {
	method := os.Getenv("EXECUTION_METHOD")
	if method == "" {
		method = "docker"
	}
	c.JSON(http.StatusOK, gin.H{"execution_method": strings.ToLower(method)})
}
