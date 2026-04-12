package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
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
	scraperURL      string
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
		redisURL = "redis://localhost:6379"
	}

	// Parse Redis URL to extract host and port
	var addr string
	if strings.HasPrefix(redisURL, "redis://") {
		addr = strings.TrimPrefix(redisURL, "redis://")
	} else {
		addr = redisURL
	}

	redisClient := redis.NewClient(&redis.Options{
		Addr:         addr,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
		PoolTimeout:  4 * time.Second,
	})

	connected := make(chan bool, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		if err := redisClient.Ping(ctx).Err(); err != nil {
			log.Printf("⚠️  [MONITOR] Failed to connect to Redis at %s: %v", addr, err)
			log.Printf("⚠️  [MONITOR] Make sure Redis is running and accessible at %s", addr)
			log.Printf("⚠️  [MONITOR] For Docker Redis: docker ps | grep redis")
			log.Printf("⚠️  [MONITOR] For Kubernetes: check service DNS resolution")
			log.Printf("⚠️  [MONITOR] Monitor UI will continue but tools/metrics may not work")
			log.Printf("⚠️  [MONITOR] Will retry Redis connection in background...")
			connected <- false
		} else {
			log.Printf("✅ [MONITOR] Successfully connected to Redis at %s", addr)
			connected <- true
		}
	}()

	select {
	case success := <-connected:
		if !success {

			redisClient = nil
		}
	case <-time.After(2 * time.Second):

		log.Printf("⏳ [MONITOR] Redis connection check taking longer than expected, continuing startup...")
		go func() {
			success := <-connected
			if !success {
				redisClient = nil
			}
		}()
	}

	hdnURL := strings.TrimSpace(os.Getenv("HDN_URL"))
	if hdnURL == "" {
		hdnURL = "http://localhost:8081"
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

	neo4jURL := strings.TrimSpace(os.Getenv("NEO4J_URL"))
	if neo4jURL == "" {

		neo4jURI := strings.TrimSpace(os.Getenv("NEO4J_URI"))
		if neo4jURI != "" {
			neo4jURL = neo4jURI
		} else {
			neo4jURL = "http://localhost:7474"
		}
	}

	if strings.HasPrefix(neo4jURL, "bolt://") {
		neo4jURL = strings.Replace(neo4jURL, "bolt://", "http://", 1)

		if strings.Contains(neo4jURL, ":7687") {
			neo4jURL = strings.Replace(neo4jURL, ":7687", ":7474", 1)
		} else if !strings.Contains(neo4jURL, ":") {

			neo4jURL = neo4jURL + ":7474"
		} else {

			parts := strings.Split(neo4jURL, ":")
			if len(parts) >= 2 {
				neo4jURL = strings.Join(parts[:len(parts)-1], ":") + ":7474"
			}
		}
	} else if !strings.HasPrefix(neo4jURL, "http://") && !strings.HasPrefix(neo4jURL, "https://") {

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

		natsURL = strings.Replace(natsURL, "nats://", "http://", 1)

		if strings.Contains(natsURL, ":4222") {
			natsURL = strings.Replace(natsURL, ":4222", ":8223", 1)
		} else if !strings.Contains(natsURL, ":") {

			natsURL = natsURL + ":8223"
		} else {

			parts := strings.Split(natsURL, ":")
			if len(parts) >= 2 {
				natsURL = strings.Join(parts[:len(parts)-1], ":") + ":8223"
			}
		}
	} else if !strings.HasPrefix(natsURL, "http://") && !strings.HasPrefix(natsURL, "https://") {

		if !strings.Contains(natsURL, ":") {
			natsURL = "http://" + natsURL + ":8223"
		} else {
			natsURL = "http://" + natsURL
		}
	}

	scraperURL := strings.TrimSpace(os.Getenv("PLAYWRIGHT_SCRAPER_URL"))
	if scraperURL == "" {
		scraperURL = "http://localhost:8085"
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
		scraperURL:    scraperURL,
	}

	m.runLLMWorker()
	return m
}

func main() {

	loadDotEnv(".env")

	gin.SetMode(gin.ReleaseMode)

	r := gin.Default()

	r.Use(func(c *gin.Context) {

		if strings.HasPrefix(c.Request.URL.Path, "/api/rag/search") {
			log.Printf("🌐 [MIDDLEWARE] RAG Search request detected: %s %s", c.Request.Method, c.Request.URL.String())
			log.Printf("🌐 [MIDDLEWARE] Query params: %v", c.Request.URL.Query())
		}
		c.Next()
	})

	r.Use(func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS, HEAD, PATCH")
		c.Header("Access-Control-Allow-Headers", "Origin, Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, X-Requested-With")
		c.Header("Access-Control-Expose-Headers", "Content-Length, X-Script-File, X-Script-Modified, X-Job-ID")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	})

	staticDir := strings.TrimSpace(os.Getenv("MONITOR_STATIC_DIR"))
	if staticDir == "" {

		execPath, err := os.Executable()
		if err == nil {
			execDir := filepath.Dir(execPath)

			projectRoot := filepath.Dir(execDir)
			monitorDir := filepath.Join(projectRoot, "monitor")
			staticDir = filepath.Join(monitorDir, "static")

			if _, err := os.Stat(staticDir); err != nil {
				staticDir = "./static"
				if wd, err := os.Getwd(); err == nil {
					staticDir = filepath.Join(wd, staticDir)
				}
			}
		} else {

			staticDir = "./static"
			if wd, err := os.Getwd(); err == nil {
				staticDir = filepath.Join(wd, staticDir)
			}
		}
	} else {

		if !strings.HasPrefix(staticDir, "/") {
			if wd, err := os.Getwd(); err == nil {
				staticDir = filepath.Join(wd, staticDir)
			}
		}
	}
	log.Printf("📁 [MONITOR] Serving static files from: %s", staticDir)
	r.Static("/static", staticDir)

	screenshotDir := filepath.Join(staticDir, "smart_scrape", "screenshots")
	log.Printf("📸 [MONITOR] Screenshot directory: %s", screenshotDir)
	r.GET("/api/smart_scrape/screenshots/:name", func(c *gin.Context) {
		name := c.Param("name")
		if name == "" || strings.Contains(name, "..") {
			log.Printf("⚠️ [SCREENSHOT] Invalid name: %s", name)
			c.Status(http.StatusBadRequest)
			return
		}
		path := filepath.Join(screenshotDir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				log.Printf("❌ [SCREENSHOT] File not found: %s", path)
				c.Status(http.StatusNotFound)
				return
			}
			log.Printf("❌ [SCREENSHOT] Read error for %s: %v", path, err)
			c.Status(http.StatusInternalServerError)
			return
		}
		switch {
		case strings.HasSuffix(name, ".progress"):
			c.Data(http.StatusOK, "application/json", data)
		case strings.HasSuffix(name, ".png"):
			log.Printf("✅ [SCREENSHOT] Serving %s (%d bytes)", name, len(data))
			c.Data(http.StatusOK, "image/png", data)
		default:
			c.Data(http.StatusOK, "application/octet-stream", data)
		}
	})

	execPath, err := os.Executable()
	if err != nil {
		log.Fatal("Failed to get executable path:", err)
	}
	execDir := filepath.Dir(execPath)

	projectRoot := filepath.Dir(execDir)
	monitorDir := filepath.Join(projectRoot, "monitor")
	tmplFiles, _ := filepath.Glob(filepath.Join(monitorDir, "templates/*.html"))
	partialFiles, _ := filepath.Glob(filepath.Join(monitorDir, "templates/partials/*.html"))
	all := append([]string{}, tmplFiles...)
	all = append(all, partialFiles...)
	if len(all) == 0 {
		log.Println("Warning: no templates found in", filepath.Join(monitorDir, "templates/"))

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

	log.Printf("🚀 [MONITOR] Starting Monitor Service with PROJECT_ID support - Version 2025-09-25")

	r.GET("/", monitor.dashboard)
	r.GET("/tabs", monitor.dashboardTabs)
	r.GET("/test", monitor.testPage)
	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "healthy", "timestamp": time.Now().Format(time.RFC3339)})
	})
	r.GET("/chat", monitor.chatPage)
	r.GET("/thinking", monitor.thinkingPanel)
	r.GET("/wow", monitor.wowFactor)
	r.POST("/api/chat", monitor.chatAPI)
	r.POST("/api/v1/chat", monitor.chatAPI)

	r.GET("/api/v1/chat/sessions", monitor.getChatSessions)
	r.GET("/api/v1/chat/sessions/:sessionId/thoughts", monitor.getSessionThoughts)
	r.GET("/api/v1/chat/sessions/:sessionId/thoughts/stream", monitor.streamSessionThoughts)
	r.POST("/api/v1/chat/sessions/:sessionId/thoughts/express", monitor.expressSessionThoughts)
	r.GET("/api/v1/chat/sessions/:sessionId/history", monitor.getSessionHistory)
	r.GET("/api/status", monitor.getSystemStatus)
	r.GET("/api/llm/queue/stats", monitor.getLLMQueueStats)
	r.GET("/api/workflows", monitor.getActiveWorkflows)
	r.GET("/api/workflow/:workflow_id/details", monitor.getWorkflowDetails)
	r.GET("/api/projects", monitor.getProjects)
	r.GET("/api/projects/:id", monitor.getProject)
	r.DELETE("/api/projects/:id", monitor.deleteProject)
	r.GET("/api/projects/:id/checkpoints", monitor.getProjectCheckpoints)
	r.GET("/api/projects/:id/workflows", monitor.getProjectWorkflows)

	r.POST("/api/projects/:id/analyze_last_workflow", monitor.analyzeLastWorkflow)
	r.GET("/api/workflow/:workflow_id/files", monitor.listWorkflowFiles)
	r.GET("/api/workflow/:workflow_id/project", monitor.getWorkflowProject)

	r.GET("/api/workflow/:workflow_id/files/:filename", monitor.serveWorkflowFile)

	r.GET("/api/file/:filename", monitor.serveGenericFile)

	r.GET("/api/artifacts/*filepath", monitor.serveLocalFileOrProxy)
	r.GET("/api/files/*filename", monitor.serveFile)
	r.GET("/api/metrics", monitor.getExecutionMetrics)
	r.GET("/api/redis", monitor.getRedisInfo)
	r.GET("/api/docker", monitor.getDockerInfo)
	r.GET("/api/neo4j", monitor.getNeo4jInfo)
	r.GET("/api/neo4j/stats", monitor.getNeo4jStats)
	r.GET("/api/qdrant", monitor.getQdrantInfo)
	r.GET("/api/qdrant/stats", monitor.getQdrantStats)
	r.GET("/api/weaviate", monitor.getQdrantInfo)
	r.GET("/api/weaviate/stats", monitor.getQdrantStats)
	r.GET("/api/weaviate/records", monitor.getWeaviateRecords)
	r.GET("/api/nats", monitor.getNATSInfo)

	r.GET("/api/rag/search", monitor.ragSearch)
	r.POST("/api/goal/:id/update-status", monitor.updateGoalStatus)
	r.DELETE("/api/memory/goals/:id", monitor.deleteSelfModelGoal)
	r.GET("/api/logs", monitor.getLogs)
	r.GET("/api/k8s/services", monitor.getK8sServices)
	r.GET("/api/k8s/logs/:service", monitor.getK8sLogs)
	r.GET("/api/ws", monitor.websocketHandler)

	r.GET("/api/capabilities", monitor.getCapabilities)

	r.GET("/api/memory/summary", monitor.getMemorySummary)
	r.GET("/api/memory/episodes", monitor.searchEpisodes)

	r.POST("/api/memory/goals/cleanup", monitor.cleanupSelfModelGoals)

	r.GET("/api/news/events", monitor.getNewsEvents)

	r.GET("/api/wikipedia/events", monitor.getWikipediaEvents)

	r.GET("/api/daily_summary/latest", monitor.getDailySummaryLatest)
	r.GET("/api/daily_summary/history", monitor.getDailySummaryHistory)
	r.GET("/api/daily_summary/:date", monitor.getDailySummaryByDate)

	r.GET("/api/evaluations", monitor.getFsmEvaluations)

	r.GET("/api/goals/:agent/active", monitor.getActiveGoals)
	r.GET("/api/goal/:id", monitor.getGoalByID)
	r.POST("/api/goal/:id/achieve", monitor.achieveGoal)
	r.DELETE("/api/goal/:id", monitor.deleteGoal)
	r.POST("/api/goal/:id/suggest", monitor.suggestGoalNextSteps)

	r.POST("/api/goal/:id/execute", monitor.executeGoalSuggestedPlan)

	r.POST("/api/goals/create_from_nl", monitor.createGoalFromNL)

	r.GET("/api/agents", monitor.getAgents)
	r.GET("/api/agents/:id", monitor.getAgent)
	r.GET("/api/agents/:id/status", monitor.getAgentStatus)
	r.GET("/api/agents/:id/executions", monitor.getAgentExecutions)
	r.GET("/api/agents/:id/executions/:execution_id", monitor.getAgentExecution)
	r.POST("/api/agents/:id/execute", monitor.executeAgent)
	r.POST("/api/agents", monitor.createAgent)
	r.DELETE("/api/agents/:id", monitor.deleteAgent)

	r.GET("/api/reasoning/traces/:domain", monitor.getReasoningTraces)
	r.GET("/api/reasoning/beliefs/:domain", monitor.getBeliefs)
	r.GET("/api/reasoning/curiosity-goals/:domain", monitor.getCuriosityGoals)
	r.GET("/api/reasoning/hypotheses/:domain", monitor.getHypotheses)
	r.GET("/api/reasoning/explanations/:goal", monitor.getReasoningExplanations)
	r.GET("/api/reasoning/explanations", monitor.getRecentExplanations)
	r.GET("/api/reasoning/domains", monitor.getReasoningDomains)

	r.GET("/api/reflect", monitor.getReflection)

	r.Any("/api/fsm/*path", monitor.proxyFSM)

	r.Any("/api/scraper/*path", monitor.proxyScraper)
	r.Any("/api/codegen/*path", monitor.proxyScraper)

	r.POST("/api/interpret", monitor.interpretNaturalLanguage)
	r.POST("/api/interpret/execute", monitor.interpretAndExecute)

	r.POST("/api/llm/enqueue", monitor.enqueueLLMJob)
	r.GET("/api/llm/status/:id", monitor.getLLMJobStatus)

	r.POST("/api/v1/intelligent/execute", monitor.proxyIntelligentExecute)
	r.OPTIONS("/api/v1/intelligent/execute", monitor.handleCORS)

	r.GET("/api/tools", monitor.getTools)
	r.GET("/api/tools/usage", monitor.getToolUsage)
	r.GET("/api/tools/metrics", monitor.getToolMetrics)
	r.GET("/api/tools/:id/metrics", monitor.getToolMetricsByID)
	r.GET("/api/tools/calls/recent", monitor.getRecentToolCalls)
	r.POST("/api/clear-safety-data", monitor.clearSafetyData)
	r.POST("/api/tools/:id/invoke", func(c *gin.Context) {
		id := c.Param("id")
		if id == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "missing id"})
			return
		}

		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read request body"})
			return
		}

		hdn := strings.TrimRight(monitor.hdnURL, "/") + "/api/v1/tools/" + id + "/invoke"
		req, err := http.NewRequest(http.MethodPost, hdn, bytes.NewReader(body))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create request"})
			return
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": "hdn invoke failed", "details": err.Error()})
			return
		}
		defer resp.Body.Close()
		respBody, _ := io.ReadAll(resp.Body)
		c.Data(resp.StatusCode, "application/json", respBody)
	})
	r.DELETE("/api/tools/:id", func(c *gin.Context) {
		id := c.Param("id")
		if id == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "missing id"})
			return
		}

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

	go monitor.startCuriosityGoalConsumer()

	if os.Getenv("ENABLE_AUTO_EXECUTOR") != "false" {
		go monitor.startAutoExecutor()
	} else {
		log.Println("⏸️ Auto-executor disabled via ENABLE_AUTO_EXECUTOR=false")
	}

	fmt.Println("🚀 HDN Monitor UI starting on :8082")
	fmt.Println("📊 Dashboard: http://localhost:8082")
	fmt.Println("🔧 API: http://localhost:8082/api/status")

	if err := r.Run(":8082"); err != nil {
		log.Fatal("Failed to start monitor server:", err)
	}
}

type llmJob struct {
	ID        string            `json:"id"`
	Priority  string            `json:"priority"`
	Input     string            `json:"input"`
	Context   map[string]string `json:"context,omitempty"`
	SessionID string            `json:"session_id,omitempty"`
	Callback  string            `json:"callback_url,omitempty"`
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

// Helper function to get last activity time from workflow for sorting
func getLastActivityTime(wf *WorkflowStatusResponse) time.Time {
	if wf == nil {
		return time.Time{}
	}

	if !wf.LastActivity.IsZero() {
		return wf.LastActivity
	}

	if !wf.StartedAt.IsZero() {
		return wf.StartedAt
	}
	return time.Time{}
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

			return []byte(str), nil
		}
	}
	return nil, fmt.Errorf("content not found or not a string")
}

// extractPDFFromConsoleOutput extracts PDF content from console output that contains pip logs
func extractPDFFromConsoleOutput(content string) []byte {

	pdfStart := strings.Index(content, "%PDF")
	if pdfStart == -1 {

		return []byte(content)
	}

	pdfContent := content[pdfStart:]
	return []byte(pdfContent)
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

// createSamplePDF creates a detailed PDF report for demo purposes
func createSamplePDF() []byte {

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

// isLikelyMultiStepArtifactRequest detects prompts that imply multiple activities and saving artifacts
func isLikelyMultiStepArtifactRequest(input string) bool {
	text := strings.ToLower(strings.TrimSpace(input))
	if text == "" {
		return false
	}

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
// Priority: rust > go > javascript > java > python (Rust checked first to avoid false Go matches)
func detectLanguage(text string, files []string) string {
	lang := "python"
	lower := strings.ToLower(text)
	hasGo, hasRust, hasJS, hasJava := false, false, false, false
	for _, f := range files {
		lf := strings.ToLower(f)
		if strings.HasSuffix(lf, ".go") {
			hasGo = true
		} else if strings.HasSuffix(lf, ".rs") {
			hasRust = true
		} else if strings.HasSuffix(lf, ".js") {
			hasJS = true
		} else if strings.HasSuffix(lf, ".java") {
			hasJava = true
		}
	}

	if hasRust {
		return "rust"
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

	if strings.Contains(lower, "rust") || strings.Contains(lower, ".rs") || strings.Contains(lower, " rust program") || strings.Contains(lower, " in rust") {
		return "rust"
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

// isSourceIdentifier checks if a string is a source identifier (like "news:bbc", "wikipedia") rather than a semantic domain
func isSourceIdentifier(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))

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

// extractFirstURL finds the first http/https URL in text (simple regex)
func extractFirstURL(text string) string {
	re := regexp.MustCompile(`https?://[^\s]+`)
	m := re.FindString(text)

	m = strings.TrimRight(m, ".,);]\n\r")
	return m
}

// ConcreteTask represents a specific executable task
type ConcreteTask struct {
	Name        string
	Description string
	Language    string
	Input       string
}
