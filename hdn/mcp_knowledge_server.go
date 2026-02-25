package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	mempkg "hdn/memory"
	"hdn/playwright"

	"github.com/redis/go-redis/v9"
)

// MCPKnowledgeServer exposes knowledge bases (Neo4j, Weaviate, Qdrant) as MCP tools
type MCPKnowledgeServer struct {
	domainKnowledge mempkg.DomainKnowledgeClient
	vectorDB        mempkg.VectorDBAdapter
	redis           *redis.Client
	hdnURL          string                // For proxying queries
	skillRegistry   *DynamicSkillRegistry // Dynamic skills from configuration
	llmClient       *LLMClient            // LLM client for prompt-driven browser automation
}

// MCPKnowledgeRequest represents an MCP JSON-RPC request for knowledge server
type MCPKnowledgeRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// MCPKnowledgeResponse represents an MCP JSON-RPC response for knowledge server
type MCPKnowledgeResponse struct {
	JSONRPC string             `json:"jsonrpc"`
	ID      interface{}        `json:"id"`
	Result  interface{}        `json:"result,omitempty"`
	Error   *MCPKnowledgeError `json:"error,omitempty"`
}

// MCPKnowledgeError represents an MCP error for knowledge server
type MCPKnowledgeError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// MCPKnowledgeTool represents an MCP tool definition for knowledge server
type MCPKnowledgeTool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"inputSchema"`
}

// NewMCPKnowledgeServer creates a new MCP knowledge server
func NewMCPKnowledgeServer(domainKnowledge mempkg.DomainKnowledgeClient, vectorDB mempkg.VectorDBAdapter, redis *redis.Client, hdnURL string, llmClient *LLMClient) *MCPKnowledgeServer {
	server := &MCPKnowledgeServer{
		domainKnowledge: domainKnowledge,
		vectorDB:        vectorDB,
		redis:           redis,
		hdnURL:          hdnURL,
		skillRegistry:   NewDynamicSkillRegistry(),
		llmClient:       llmClient,
	}

	// Load skills from configuration
	configPath := os.Getenv("N8N_MCP_SKILLS_CONFIG")
	if configPath == "" {
		configPath = "n8n_mcp_skills.yaml" // Default path
	}
	log.Printf("🔍 [MCP-KNOWLEDGE] Attempting to load skills from config: %s", configPath)
	if err := server.skillRegistry.LoadSkillsFromConfig(configPath); err != nil {
		log.Printf("⚠️ [MCP-KNOWLEDGE] Failed to load skills from configuration: %v (continuing with hardcoded tools)", err)
	} else {
		log.Printf("✅ [MCP-KNOWLEDGE] Successfully loaded skills from configuration")
	}

	return server
}

// SetLLMClient sets or updates the LLM client on the MCP knowledge server
func (s *MCPKnowledgeServer) SetLLMClient(llmClient *LLMClient) {
	s.llmClient = llmClient
	if llmClient != nil {
		log.Printf("✅ [MCP-KNOWLEDGE] LLM client set on MCP knowledge server")
	} else {
		log.Printf("⚠️ [MCP-KNOWLEDGE] LLM client set to nil on MCP knowledge server")
	}
}

// GetPromptHints returns prompt hints for a tool by ID
func (s *MCPKnowledgeServer) GetPromptHints(toolID string) *PromptHintsConfig {
	if s.skillRegistry == nil {
		return nil
	}
	return s.skillRegistry.GetPromptHints(toolID)
}

// GetAllPromptHints returns all prompt hints from configured skills
func (s *MCPKnowledgeServer) GetAllPromptHints() map[string]*PromptHintsConfig {
	if s.skillRegistry == nil {
		return make(map[string]*PromptHintsConfig)
	}
	return s.skillRegistry.GetAllPromptHints()
}

// HandleRequest handles an MCP JSON-RPC request
// HandleRequest handles an MCP JSON-RPC request and supports SSE handshake
func (s *MCPKnowledgeServer) HandleRequest(w http.ResponseWriter, r *http.Request) {
	// Enable CORS for all requests
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With, Accept")

	// Handle preflight requests
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Check if this is an SSE connection request
	// MCP spec: connection initialization often happens via SSE
	if r.Header.Get("Accept") == "text/event-stream" || r.URL.Query().Get("sse") == "true" || strings.HasSuffix(r.URL.Path, "/sse") {
		s.handleSSESession(w, r)
		return
	}

	// Allow GET for simple probing/discovery (returns tool list)
	if r.Method == http.MethodGet {
		result, err := s.listTools()
		if err != nil {
			s.sendError(w, nil, -32603, "Internal error", err)
			return
		}
		// Wrap in JSON-RPC response for consistency
		// Use "0" as ID for probe
		response := MCPKnowledgeResponse{
			JSONRPC: "2.0",
			ID:      0,
			Result:  result,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req MCPKnowledgeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.sendError(w, nil, -32700, "Parse error", err)
		return
	}

	var result interface{}
	var err error

	switch req.Method {
	case "initialize":
		// Handle MCP initialization handshake
		result = map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]interface{}{
				"tools": map[string]interface{}{}, // Advertise tool support
			},
			"serverInfo": map[string]string{
				"name":    "hdn-server",
				"version": "1.0.0",
			},
		}
	case "notifications/initialized":
		// Client acknowledgment, nothing to return but success
		result = map[string]interface{}{}
	case "tools/list":
		result, err = s.listTools()
	case "tools/call":
		var params struct {
			Name      string                 `json:"name"`
			Arguments map[string]interface{} `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			s.sendError(w, req.ID, -32602, "Invalid params", err)
			return
		}
		result, err = s.callTool(r.Context(), params.Name, params.Arguments)
	default:
		s.sendError(w, req.ID, -32601, fmt.Sprintf("Method not found: %s", req.Method), nil)
		return
	}

	if err != nil {
		s.sendError(w, req.ID, -32000, "Server error", err)
		return
	}

	s.sendResponse(w, req.ID, result)
}

// handleSSESession establishes an MCP SSE session
// It sends the endpoint URL for subsequent POST requests
func (s *MCPKnowledgeServer) handleSSESession(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	// Add CORS headers for SSE
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With, Accept")
	w.WriteHeader(http.StatusOK)

	// Construct the POST endpoint URL
	// We use the request host to ensure reachability from the client's perspective
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	// For MCP, the endpoint provided in the SSE 'endpoint' event is where the client should send JSON-RPC messages
	// We point it back to the same /mcp path which handles POST
	postEndpoint := fmt.Sprintf("%s://%s/mcp", scheme, r.Host)
	if strings.Contains(r.URL.Path, "/api/v1/mcp") {
		postEndpoint = fmt.Sprintf("%s://%s/api/v1/mcp", scheme, r.Host)
	}

	// Send the initial 'endpoint' event as per MCP spec
	// usage: event: endpoint\ndata: <url>\n\n
	fmt.Fprintf(w, "event: endpoint\n")
	fmt.Fprintf(w, "data: %s\n\n", postEndpoint)
	flusher.Flush()

	// Keep the connection open until client disconnects
	// This is a requirement for SSE transport typically
	// We can monitor context cancellation
	notify := r.Context().Done()
	<-notify
	log.Printf("SSE connection closed by client")
}

// listTools returns available knowledge base tools
func (s *MCPKnowledgeServer) listTools() (interface{}, error) {
	tools := []MCPKnowledgeTool{
		{
			Name:        "query_neo4j",
			Description: "Query the Neo4j knowledge graph using Cypher. Use this to find concepts, relationships, and facts stored in the knowledge graph.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query": map[string]interface{}{
						"type":        "string",
						"description": "Cypher query to execute (e.g., 'MATCH (c:Concept {name: $name}) RETURN c')",
					},
					"natural_language": map[string]interface{}{
						"type":        "string",
						"description": "Optional: Natural language query that will be translated to Cypher",
					},
				},
				"required": []string{"query"},
			},
		},
		{
			Name:        "search_weaviate",
			Description: "Search the Weaviate vector database for semantically similar content. Use this to find episodes, memories, or documents by meaning.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query": map[string]interface{}{
						"type":        "string",
						"description": "Text query to search for semantically similar content",
					},
					"limit": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum number of results to return (default: 10)",
						"default":     10,
					},
					"collection": map[string]interface{}{
						"type":        "string",
						"description": "Collection name to search (default: 'AgiEpisodes')",
						"default":     "AgiEpisodes",
					},
				},
				"required": []string{"query"},
			},
		},
		{
			Name: "get_concept",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Name of the concept",
					},
					"domain": map[string]interface{}{
						"type": "string",
					},
				},
				"required": []string{"name"},
			},
		},
		{
			Name:        "find_related_concepts",
			Description: "Find concepts related to a given concept in the Neo4j knowledge graph.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"concept_name": map[string]interface{}{
						"type":        "string",
						"description": "Name of the concept to find relations for",
					},
					"max_depth": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum relationship depth (default: 2)",
						"default":     2,
					},
				},
				"required": []string{"concept_name"},
			},
		},
		{
			Name:        "search_avatar_context",
			Description: "Search personal information about Steven Fisher (the user). Use this for questions about his work history, education, skills, projects, or any personal background. Examples: 'Did I work for Accenture?', 'What companies have I worked for?', 'What are my skills?'",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query": map[string]interface{}{
						"type":        "string",
						"description": "Question or search query about Steven Fisher's personal information",
					},
					"limit": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum number of results to return (default: 5)",
						"default":     5,
					},
				},
				"required": []string{"query"},
			},
		},
		{
			Name:        "save_avatar_context",
			Description: "Save personal information, preferences, or facts about Steven Fisher (the user) to long-term memory. Use this when the user shares something they want remembered. Example: 'I prefer to be called Steve', 'I worked at Google in 2020'.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"content": map[string]interface{}{
						"type":        "string",
						"description": "The personal fact or information to save",
					},
					"source": map[string]interface{}{
						"type":        "string",
						"description": "Optional source of the information (e.g. 'user_chat')",
					},
				},
				"required": []string{"content"},
			},
		},
	}

	// Add standard HDN tools
	tools = append(tools, MCPKnowledgeTool{
		Name:        "scrape_url",
		Description: "Scrape content from a URL safely. Useful for reading documentation or checking external sites. Can use a TypeScript/Playwright config (provided as string) to generate custom Go scraping code via LLM.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"url": map[string]interface{}{
					"type":        "string",
					"description": "URL to scrape",
				},
				"async": map[string]interface{}{
					"type":        "boolean",
					"description": "If true, returns immediately with a job_id instead of waiting for results. Default: false.",
					"default":     false,
				},
				"typescript_config": map[string]interface{}{
					"type":        "string",
					"description": "Optional: TypeScript/Playwright code (as string) that will be converted to Go code via LLM for custom scraping logic",
				},
				"extractions": map[string]interface{}{
					"type":        "object",
					"description": "Optional: Map of extraction names to regex patterns. Example: {\"co2\": \"(\\\\d+) kg\"}. The regex will be applied to the page content.",
					"additionalProperties": map[string]interface{}{
						"type": "string",
					},
				},
			},
			"required": []string{"url"},
		},
	})

	tools = append(tools, MCPKnowledgeTool{
		Name:        "get_scrape_status",
		Description: "Check the status and retrieve results of a previously started scrape job.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"job_id": map[string]interface{}{
					"type":        "string",
					"description": "The ID of the scrape job to check",
				},
			},
			"required": []string{"job_id"},
		},
	})

	tools = append(tools, MCPKnowledgeTool{
		Name:        "smart_scrape",
		Description: "Perform an AI-powered scrape of a URL to extract specific information based on a goal. Automatically plans the scrape logic and executes it.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"url": map[string]interface{}{
					"type":        "string",
					"description": "URL to scrape",
				},
				"goal": map[string]interface{}{
					"type":        "string",
					"description": "Clear description of what data you want to extract (e.g. 'find all savings account names and their interest rates')",
				},
			},
			"required": []string{"url", "goal"},
		},
	})

	tools = append(tools, MCPKnowledgeTool{
		Name:        "execute_code",
		Description: "Execute Python or Go code in a secure sandbox. Use for calculation, data processing, or simple scripts.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"code":     map[string]interface{}{"type": "string", "description": "The source code to execute"},
				"language": map[string]interface{}{"type": "string", "enum": []string{"python", "go"}, "default": "python"},
			},
			"required": []string{"code"},
		},
	})

	tools = append(tools, MCPKnowledgeTool{
		Name:        "save_episode",
		Description: "Save a conversation summary or significant event to the episodic memory knowledge base.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"text":     map[string]interface{}{"type": "string", "description": "The content of the episode or summary"},
				"metadata": map[string]interface{}{"type": "object", "description": "Optional metadata as a JSON object"},
			},
			"required": []string{"text"},
		},
	})

	tools = append(tools, MCPKnowledgeTool{
		Name:        "browse_web",
		Description: "Browse a website using a headless browser. Provide natural language instructions describing what to do (e.g., 'Fill the from field with Southampton, fill the to field with Newcastle, select plane transport type, click calculate, and extract the CO2 emissions result'). The LLM will automatically determine the correct selectors and actions. Returns extracted data as JSON.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"url": map[string]interface{}{
					"type":        "string",
					"description": "The URL to browse to",
				},
				"instructions": map[string]interface{}{
					"type":        "string",
					"description": "Natural language instructions describing what to do on the page (e.g., 'Fill from field with Southampton, to field with Newcastle, select plane, click calculate, extract CO2 result')",
				},
				"actions": map[string]interface{}{
					"type":        "array",
					"description": "Optional: Pre-defined actions array. If not provided, the LLM will generate actions from instructions. Each action has: type, selector, value, extract, wait_for, timeout",
					"items": map[string]interface{}{
						"type": "object",
					},
				},
				"timeout": map[string]interface{}{
					"type":        "integer",
					"description": "Overall timeout in seconds (default: 60)",
					"default":     60,
				},
			},
			"required": []string{"url", "instructions"},
		},
	})

	// Add dynamically configured skills from registry
	if s.skillRegistry != nil {
		configuredSkills := s.skillRegistry.ListSkills()
		// Check for duplicates and only add new skills
		existingNames := make(map[string]bool)
		for _, tool := range tools {
			existingNames[tool.Name] = true
		}
		for _, skill := range configuredSkills {
			if !existingNames[skill.Name] {
				tools = append(tools, skill)
				log.Printf("✅ [MCP-KNOWLEDGE] Added configured skill: %s", skill.Name)
			} else {
				log.Printf("⚠️ [MCP-KNOWLEDGE] Skipping duplicate configured skill: %s (hardcoded version takes precedence)", skill.Name)
			}
		}
	}

	return map[string]interface{}{
		"tools": tools,
	}, nil
}

// callTool executes an MCP tool
func (s *MCPKnowledgeServer) callTool(ctx context.Context, toolName string, arguments map[string]interface{}) (interface{}, error) {
	// First check if this is a configured skill
	if s.skillRegistry != nil && s.skillRegistry.HasSkill(toolName) {
		log.Printf("🔧 [MCP-KNOWLEDGE] Executing configured skill: %s", toolName)
		return s.skillRegistry.ExecuteSkill(ctx, toolName, arguments)
	}

	// Fall back to hardcoded tools
	switch toolName {
	case "query_neo4j":
		return s.queryNeo4j(ctx, arguments)
	case "search_weaviate":
		return s.searchWeaviate(ctx, arguments)
	case "get_concept":
		return s.getConcept(ctx, arguments)
	case "find_related_concepts":
		return s.findRelatedConcepts(ctx, arguments)
	case "search_avatar_context":
		return s.searchAvatarContext(ctx, arguments)
	case "save_avatar_context":
		return s.saveAvatarContext(ctx, arguments)
	case "save_episode":
		return s.saveEpisode(ctx, arguments)
	case "scrape_url", "execute_code", "read_file", "smart_scrape":
		// Route to the new wrapper
		return s.executeToolWrapper(ctx, toolName, arguments)
	case "browse_web":
		return s.browseWeb(ctx, arguments)
	case "get_scrape_status":
		// Handle nested arguments from n8n
		args := arguments
		if inner, ok := arguments["arguments"].(map[string]interface{}); ok {
			args = inner
		}
		jobID, _ := args["job_id"].(string)
		if jobID == "" {
			return nil, fmt.Errorf("job_id parameter required")
		}
		return s.getScrapeStatus(ctx, jobID)
	default:
		return nil, fmt.Errorf("unknown tool: %s", toolName)
	}
}

// queryNeo4j executes a Cypher query against Neo4j
func (s *MCPKnowledgeServer) queryNeo4j(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	query, ok := args["query"].(string)
	if !ok || query == "" {
		return nil, fmt.Errorf("query parameter is required")
	}

	// If natural_language is provided, try to translate it first
	if nlQuery, ok := args["natural_language"].(string); ok && nlQuery != "" {
		// For now, use the natural language query as-is
		// In the future, we could use LLM to translate to Cypher
		log.Printf("🧠 [MCP-KNOWLEDGE] Natural language query: %s", nlQuery)
		// Simple translation: if it's a "what is X" query, convert to Cypher
		if strings.HasPrefix(strings.ToLower(nlQuery), "what is ") {
			concept := strings.TrimSpace(strings.TrimPrefix(strings.ToLower(nlQuery), "what is "))
			// Escape single quotes to prevent Cypher injection
			escapedConcept := strings.ReplaceAll(concept, "'", "\\'")
			// Use direct string matching since queryViaHDN doesn't support parameters
			// SEARCH BOTH NAME AND DEFINITION
			query = fmt.Sprintf("MATCH (c:Concept) WHERE toLower(c.name) CONTAINS toLower('%s') OR toLower(c.definition) CONTAINS toLower('%s') RETURN c LIMIT 10", escapedConcept, escapedConcept)
			log.Printf("🧠 [MCP-KNOWLEDGE] Translated to Cypher: %s", query)
		}
	}

	// Use HDN's knowledge query endpoint if available
	if s.hdnURL != "" {
		return s.queryViaHDN(ctx, query)
	}

	// Fallback: direct Neo4j query if domainKnowledge is available
	if s.domainKnowledge != nil {
		// This would require exposing ExecuteCypher from domainKnowledge
		// For now, use HDN endpoint
		return s.queryViaHDN(ctx, query)
	}

	return nil, fmt.Errorf("Neo4j not available")
}

func (s *MCPKnowledgeServer) getScrapeStatus(ctx context.Context, jobID string) (interface{}, error) {
	scraperURL := os.Getenv("PLAYWRIGHT_SCRAPER_URL")
	if scraperURL == "" {
		scraperURL = "http://playwright-scraper.agi.svc.cluster.local:8080"
	}

	jobURL := fmt.Sprintf("%s/scrape/job?job_id=%s", scraperURL, jobID)
	req, err := http.NewRequestWithContext(ctx, "GET", jobURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var job struct {
		ID          string                 `json:"id"`
		Status      string                 `json:"status"`
		Result      map[string]interface{} `json:"result,omitempty"`
		Error       string                 `json:"error,omitempty"`
		CompletedAt *time.Time             `json:"completed_at,omitempty"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&job); err != nil {
		return nil, fmt.Errorf("failed to decode job status: %v", err)
	}

	// Format for MCP
	var text string
	switch job.Status {
	case "completed":
		if job.Result != nil {
			resultBytes, _ := json.MarshalIndent(job.Result, "", "  ")
			text = fmt.Sprintf("Scrape Results for %s:\n%s", jobID, string(resultBytes))
		} else {
			text = fmt.Sprintf("Scrape Results for %s: (empty)", jobID)
		}
	case "failed":
		text = fmt.Sprintf("Scrape job %s failed: %v", jobID, job.Error)
	case "running", "pending":
		text = fmt.Sprintf("Scrape job %s is still %s.", jobID, job.Status)
	default:
		text = fmt.Sprintf("Scrape job %s has status: %s", jobID, job.Status)
	}

	return map[string]interface{}{
		"content": []map[string]interface{}{
			{
				"type": "text",
				"text": text,
			},
		},
		"result": job.Result,
		"status": job.Status,
	}, nil
}

// scrapeWithConfig delegates to the external Playwright scraper service with async job queue
func (s *MCPKnowledgeServer) scrapeWithConfig(ctx context.Context, url, tsConfig string, async bool, extractions map[string]string, getHTML bool, cookies []interface{}) (interface{}, error) {
	log.Printf("📝 [MCP-SCRAPE] Received TypeScript config (%d bytes) and %d extractions", len(tsConfig), len(extractions))

	// Call external scraper service with async job queue
	scraperURL := os.Getenv("PLAYWRIGHT_SCRAPER_URL")
	if scraperURL == "" {
		// Default to Kubernetes service in same namespace
		scraperURL = "http://playwright-scraper.agi.svc.cluster.local:8080"
	}

	// Start scrape job
	startReq := map[string]interface{}{
		"url":               url,
		"typescript_config": tsConfig,
		"extractions":       extractions,
		"get_html":          getHTML,
		"cookies":           cookies,
	}
	startReqJSON, err := json.Marshal(startReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %v", err)
	}

	log.Printf("🚀 [MCP-SCRAPE] Starting scrape job at %s/scrape/start", scraperURL)
	resp, err := http.Post(scraperURL+"/scrape/start", "application/json", bytes.NewReader(startReqJSON))
	if err != nil {
		return nil, fmt.Errorf("failed to start scrape job: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("scraper service returned %d: %s", resp.StatusCode, string(body))
	}

	var startResp struct {
		JobID     string    `json:"job_id"`
		Status    string    `json:"status"`
		CreatedAt time.Time `json:"created_at"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&startResp); err != nil {
		return nil, fmt.Errorf("failed to decode start response: %v", err)
	}

	if async {
		log.Printf("🚀 [MCP-SCRAPE] Async requested, returning job ID %s immediately", startResp.JobID)
		return map[string]interface{}{
			"content": []map[string]interface{}{
				{
					"type": "text",
					"text": fmt.Sprintf("Scrape job started. Job ID: %s. Use get_scrape_status to check results.", startResp.JobID),
				},
				{
					"type": "text",
					"text": fmt.Sprintf("JobID: %s", startResp.JobID),
				},
			},
			"job_id": startResp.JobID,
			"status": "pending",
		}, nil
	}

	log.Printf("⏳ [MCP-SCRAPE] Job %s started, polling for results...", startResp.JobID)

	// Poll for results (with timeout)
	pollTimeout := 300 * time.Second // 5 minutes to allow for form navigation + second-pass recovery
	pollInterval := 2 * time.Second
	startTime := time.Now()

	for {
		if time.Since(startTime) > pollTimeout {
			return nil, fmt.Errorf("scrape job timed out after %v", pollTimeout)
		}

		// Get job status
		jobURL := fmt.Sprintf("%s/scrape/job?job_id=%s", scraperURL, startResp.JobID)
		jobResp, err := http.Get(jobURL)
		if err != nil {
			return nil, fmt.Errorf("failed to get job status: %v", err)
		}

		var job struct {
			ID          string                 `json:"id"`
			Status      string                 `json:"status"`
			Result      map[string]interface{} `json:"result,omitempty"`
			Error       string                 `json:"error,omitempty"`
			CompletedAt *time.Time             `json:"completed_at,omitempty"`
		}
		if err := json.NewDecoder(jobResp.Body).Decode(&job); err != nil {
			jobResp.Body.Close()
			return nil, fmt.Errorf("failed to decode job status: %v", err)
		}
		jobResp.Body.Close()

		switch job.Status {
		case "completed":
			duration := time.Since(startTime)
			log.Printf("✅ [MCP-SCRAPE] Job %s completed in %v", startResp.JobID, duration)

			// Apply extractions to the result if patterns are provided
			if len(extractions) > 0 && getHTML {
				// Get the page content for extraction
				pageContent := ""
				if html, ok := job.Result["cleaned_html"].(string); ok {
					pageContent = html
				} else if html, ok := job.Result["raw_html"].(string); ok {
					pageContent = html
				} else if html, ok := job.Result["page_content"].(string); ok {
					pageContent = html
				}

				// Apply each extraction pattern
				if pageContent != "" {
					log.Printf("🔍 [MCP-SCRAPE] Applying %d extraction patterns to page content (%d chars)", len(extractions), len(pageContent))
					for key, pattern := range extractions {
						re, err := regexp.Compile(pattern)
						if err != nil {
							log.Printf("⚠️  [MCP-SCRAPE] Invalid regex pattern for '%s': %v", key, err)
							continue
						}

						matches := re.FindAllStringSubmatch(pageContent, -1)
						if len(matches) > 0 {
							// Store extracted values
							if len(matches[0]) > 1 {
								// If there are capture groups, join them
								var extracted []string
								for _, match := range matches {
									if len(match) > 1 {
										// Prioritize the last non-empty capture group
										// (LLMs often capture a class in group 1 and data in group 2)
										found := false
										for i := len(match) - 1; i >= 1; i-- {
											if match[i] != "" {
												extracted = append(extracted, match[i])
												found = true
												break
											}
										}
										if !found {
											extracted = append(extracted, match[0])
										}
									}
								}
								if len(extracted) > 0 {
									job.Result[key] = strings.Join(extracted, "\n")
									log.Printf("✅ [MCP-SCRAPE] Extracted '%s': found %d matches", key, len(extracted))
								}
							}
						} else {
							log.Printf("⚠️  [MCP-SCRAPE] No matches found for extraction pattern '%s'", key)
						}
					}
				}
			}

			// Return result in MCP format
			resultJSON, _ := json.MarshalIndent(job.Result, "", "  ")
			return map[string]interface{}{
				"content": []map[string]interface{}{
					{
						"type": "text",
						"text": fmt.Sprintf("Scrape Results:\n%s", string(resultJSON)),
					},
				},
				"result": job.Result,
				"job_id": startResp.JobID,
			}, nil

		case "failed":
			return nil, fmt.Errorf("scrape job failed: %s", job.Error)

		case "pending", "running":
			// Continue polling
			log.Printf("⏳ [MCP-SCRAPE] Job %s status: %s (elapsed: %v)", startResp.JobID, job.Status, time.Since(startTime))
			time.Sleep(pollInterval)

		default:
			return nil, fmt.Errorf("unknown job status: %s", job.Status)
		}
	}
}

// PlaywrightOperation represents a parsed operation from TypeScript
type PlaywrightOperation struct {
	Type     string // "goto", "click", "fill", "getByRole", "getByText", "locator", "wait", "extract"
	Selector string // CSS selector or locator
	Value    string // For fill operations
	Role     string // For getByRole
	RoleName string // Name for getByRole
	Text     string // For getByText
	Timeout  int    // Timeout in seconds
}

// parsePlaywrightTypeScript extracts operations from TypeScript/Playwright test code
// This wraps the shared parser and converts to internal types
func parsePlaywrightTypeScript(tsConfig, defaultURL string) ([]PlaywrightOperation, error) {
	// Use the shared parser
	sharedOps, err := playwright.ParseTypeScript(tsConfig, defaultURL)
	if err != nil {
		return nil, err
	}

	// Convert to internal types
	var operations []PlaywrightOperation
	for _, op := range sharedOps {
		operations = append(operations, PlaywrightOperation{
			Type:     op.Type,
			Selector: op.Selector,
			Value:    op.Value,
			Role:     op.Role,
			RoleName: op.RoleName,
			Text:     op.Text,
			Timeout:  op.Timeout,
		})
	}

	return operations, nil

	/* OLD PARSER CODE - replaced by shared parser
	var operations []PlaywrightOperation

	lines := strings.Split(tsConfig, "\n")

	// Extract URL from page.goto if present, otherwise use defaultURL
	currentURL := defaultURL

	for _, line := range lines {
		line = strings.TrimSpace(line)

		// Parse page.goto
		if strings.Contains(line, "page.goto") {
			// Extract URL from page.goto('url') or page.goto("url")
			if idx := strings.Index(line, "goto("); idx != -1 {
				urlStart := idx + 5
				// Skip whitespace
				for urlStart < len(line) && (line[urlStart] == ' ' || line[urlStart] == '\t') {
					urlStart++
				}
				if urlStart < len(line) && (line[urlStart] == '\'' || line[urlStart] == '"') {
					quote := line[urlStart]
					urlStart++ // Skip opening quote
					urlEnd := urlStart
					// Find closing quote (same type as opening)
					for urlEnd < len(line) && line[urlEnd] != quote {
						urlEnd++
					}
					if urlStart <= urlEnd && urlEnd < len(line) {
						currentURL = line[urlStart:urlEnd]
					}
				}
			}
			operations = append(operations, PlaywrightOperation{Type: "goto", Selector: currentURL})
			continue
		}

		// Parse page.getByRole('link', { name: 'Plane' }).click()
		if strings.Contains(line, "getByRole") && strings.Contains(line, "click()") {
			// Extract role and name
			role := ""
			name := ""
			if idx := strings.Index(line, "getByRole("); idx != -1 {
				roleStart := idx + 10
				if roleStart < len(line) {
					// Find role (first string)
					roleEnd := roleStart
					for roleEnd < len(line) && line[roleEnd] != '\'' && line[roleEnd] != '"' && line[roleEnd] != ',' {
						roleEnd++
					}
					if roleStart < roleEnd {
						role = line[roleStart:roleEnd]
					}
					// Find name: 'value'
					if nameIdx := strings.Index(line, "name:"); nameIdx != -1 {
						nameStart := nameIdx + 5
						for nameStart < len(line) && (line[nameStart] == ' ' || line[nameStart] == '\'' || line[nameStart] == '"') {
							nameStart++
						}
						nameEnd := nameStart
						for nameEnd < len(line) && line[nameEnd] != '\'' && line[nameEnd] != '"' && line[nameEnd] != ',' {
							nameEnd++
						}
						if nameStart < nameEnd {
							name = line[nameStart:nameEnd]
						}
					}
				}
			}
			operations = append(operations, PlaywrightOperation{
				Type:     "getByRole",
				Role:     role,
				RoleName: name,
			})
			continue
		}

		// Parse page.getByRole('textbox', { name: 'From To Via' }).click()
		if strings.Contains(line, "getByRole") && strings.Contains(line, "click()") {
			// Already handled above, but check for textbox
			continue
		}

		// Parse page.getByRole('textbox', { name: 'From To Via' }).fill('southampton')
		if strings.Contains(line, "getByRole") && strings.Contains(line, "fill(") {
			role := ""
			name := ""
			value := ""
			if idx := strings.Index(line, "getByRole("); idx != -1 {
				roleStart := idx + 10
				if roleStart < len(line) {
					roleEnd := roleStart
					for roleEnd < len(line) && line[roleEnd] != '\'' && line[roleEnd] != '"' && line[roleEnd] != ',' {
						roleEnd++
					}
					if roleStart < roleEnd {
						role = line[roleStart:roleEnd]
					}
					if nameIdx := strings.Index(line, "name:"); nameIdx != -1 {
						nameStart := nameIdx + 5
						for nameStart < len(line) && (line[nameStart] == ' ' || line[nameStart] == '\'' || line[nameStart] == '"') {
							nameStart++
						}
						nameEnd := nameStart
						for nameEnd < len(line) && line[nameEnd] != '\'' && line[nameEnd] != '"' && line[nameEnd] != ',' {
							nameEnd++
						}
						if nameStart < nameEnd {
							name = line[nameStart:nameEnd]
						}
					}
				}
			}
			// Extract fill value
			if fillIdx := strings.Index(line, "fill("); fillIdx != -1 {
				valueStart := fillIdx + 5
				for valueStart < len(line) && (line[valueStart] == ' ' || line[valueStart] == '\'' || line[valueStart] == '"') {
					valueStart++
				}
				valueEnd := valueStart
				for valueEnd < len(line) && line[valueEnd] != '\'' && line[valueEnd] != '"' && line[valueEnd] != ')' {
					valueEnd++
				}
				if valueStart < valueEnd {
					value = line[valueStart:valueEnd]
				}
			}
			operations = append(operations, PlaywrightOperation{
				Type:     "getByRoleFill",
				Role:     role,
				RoleName: name,
				Value:    value,
			})
			continue
		}

		// Parse page.getByText('Southampton, United Kingdom').click()
		if strings.Contains(line, "getByText") && strings.Contains(line, "click()") {
			text := ""
			if idx := strings.Index(line, "getByText("); idx != -1 {
				textStart := idx + 10
				for textStart < len(line) && (line[textStart] == ' ' || line[textStart] == '\'' || line[textStart] == '"') {
					textStart++
				}
				textEnd := textStart
				for textEnd < len(line) && line[textEnd] != '\'' && line[textEnd] != '"' && line[textEnd] != ')' {
					textEnd++
				}
				if textStart < textEnd {
					text = line[textStart:textEnd]
				}
			}
			operations = append(operations, PlaywrightOperation{
				Type: "getByText",
				Text: text,
			})
			continue
		}

		// Parse page.locator('input[name="To"]').click()
		if strings.Contains(line, "locator(") && strings.Contains(line, "click()") {
			selector := ""
			if idx := strings.Index(line, "locator("); idx != -1 {
				selStart := idx + 8
				// Skip whitespace
				for selStart < len(line) && (line[selStart] == ' ' || line[selStart] == '\t') {
					selStart++
				}
				// Find the opening quote
				if selStart < len(line) && (line[selStart] == '\'' || line[selStart] == '"') {
					quote := line[selStart]
					selStart++ // Skip opening quote
					selEnd := selStart
					// Find closing quote, handling escaped quotes
					for selEnd < len(line) {
						if line[selEnd] == '\\' && selEnd+1 < len(line) {
							selEnd += 2 // Skip escaped character
							continue
						}
						if line[selEnd] == quote {
							break
						}
						selEnd++
					}
					if selStart < selEnd {
						selector = line[selStart:selEnd]
					}
				}
			}
			operations = append(operations, PlaywrightOperation{
				Type:     "locator",
				Selector: selector,
			})
			continue
		}

		// Parse page.locator('input[name="To"]').fill('newcastle')
		if strings.Contains(line, "locator(") && strings.Contains(line, "fill(") {
			selector := ""
			value := ""
			if idx := strings.Index(line, "locator("); idx != -1 {
				selStart := idx + 8
				// Skip whitespace
				for selStart < len(line) && (line[selStart] == ' ' || line[selStart] == '\t') {
					selStart++
				}
				// Find the opening quote
				if selStart < len(line) && (line[selStart] == '\'' || line[selStart] == '"') {
					quote := line[selStart]
					selStart++ // Skip opening quote
					selEnd := selStart
					// Find closing quote, handling escaped quotes
					for selEnd < len(line) {
						if line[selEnd] == '\\' && selEnd+1 < len(line) {
							selEnd += 2 // Skip escaped character
							continue
						}
						if line[selEnd] == quote {
							break
						}
						selEnd++
					}
					if selStart < selEnd {
						selector = line[selStart:selEnd]
					}
				}
			}
			if fillIdx := strings.Index(line, "fill("); fillIdx != -1 {
				valueStart := fillIdx + 5
				// Skip whitespace
				for valueStart < len(line) && (valueStart == ' ' || line[valueStart] == '\t') {
					valueStart++
				}
				// Find the opening quote
				if valueStart < len(line) && (line[valueStart] == '\'' || line[valueStart] == '"') {
					quote := line[valueStart]
					valueStart++ // Skip opening quote
					valueEnd := valueStart
					// Find closing quote
					for valueEnd < len(line) && line[valueEnd] != quote {
						valueEnd++
					}
					if valueStart < valueEnd {
						value = line[valueStart:valueEnd]
					}
				}
			}
			operations = append(operations, PlaywrightOperation{
				Type:     "locatorFill",
				Selector: selector,
				Value:    value,
			})
			continue
		}
	}

	return operations, nil
	*/
}

// browseWebWithActions executes browser actions directly
func (s *MCPKnowledgeServer) browseWebWithActions(ctx context.Context, url string, actions []map[string]interface{}) (interface{}, error) {
	log.Printf("🚀 [BROWSE-WEB] Starting browseWebWithActions for URL: %s with %d actions", url, len(actions))

	// Find the headless_browser binary
	projectRoot := os.Getenv("AGI_PROJECT_ROOT")
	if projectRoot == "" {
		if wd, err := os.Getwd(); err == nil {
			projectRoot = wd
		}
	}

	candidates := []string{
		filepath.Join(projectRoot, "bin", "headless-browser"),
		filepath.Join(projectRoot, "bin", "tools", "headless_browser"),
		"bin/headless-browser",
		"../bin/headless-browser",
	}

	browserBin := ""
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			if abs, err := filepath.Abs(candidate); err == nil {
				browserBin = abs
			} else {
				browserBin = candidate
			}
			break
		}
	}

	if browserBin == "" {
		return nil, fmt.Errorf("headless-browser binary not found. Please build it first: cd tools/headless_browser && go build -o ../../bin/headless-browser")
	}

	// Convert actions to JSON
	actionsJSON, err := json.Marshal(actions)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal actions: %v", err)
	}

	// Run the browser tool
	runCmd := exec.CommandContext(ctx, browserBin,
		"-url", url,
		"-actions", string(actionsJSON),
		"-timeout", "60",
	)

	log.Printf("🔧 [BROWSE-WEB] Executing command: %s %v", browserBin, runCmd.Args[1:])
	log.Printf("🔧 [BROWSE-WEB] Actions JSON: %s", string(actionsJSON))

	output, err := runCmd.CombinedOutput()
	log.Printf("✅ [BROWSE-WEB] Command completed, output length: %d bytes, err: %v", len(output), err)
	if len(output) > 0 && len(output) < 500 {
		log.Printf("🔍 [BROWSE-WEB] Output content: %s", string(output))
	}
	// If we have output, proceed even if there was an error (browser might have been killed after producing output)
	if err != nil && len(output) == 0 {
		return nil, fmt.Errorf("browser execution failed: %v\nOutput: %s", err, string(output))
	}
	if err != nil && len(output) > 0 {
		log.Printf("⚠️ [BROWSE-WEB] Browser had error but produced output, proceeding: %v", err)
	}

	// Parse JSON result
	var result map[string]interface{}
	if err := json.Unmarshal(output, &result); err != nil {
		return map[string]interface{}{
			"content": []map[string]interface{}{
				{
					"type": "text",
					"text": string(output),
				},
			},
		}, nil
	}

	// Format response
	contentText := fmt.Sprintf("Scraped data from %s\n\n", url)
	if extracted, ok := result["extracted"].(map[string]interface{}); ok && len(extracted) > 0 {
		contentText += "Extracted data:\n"
		for k, v := range extracted {
			contentText += fmt.Sprintf("  %s: %v\n", k, v)
		}
	}

	return map[string]interface{}{
		"content": []map[string]interface{}{
			{
				"type": "text",
				"text": contentText,
			},
		},
		"data": result["extracted"],
	}, nil
}

// executeToolWrapper routes MCP tool calls to the wrapped internal HDN tools
func (s *MCPKnowledgeServer) executeToolWrapper(ctx context.Context, toolName string, args map[string]interface{}) (interface{}, error) {
	// Map MCP tool names to HDN tool IDs (remove the "mcp_" prefix if present, as it was added by client for namespacing)
	// But actually, we are *serving* tools here.
	// If we exposed "scrape_url", the client sees "scrape_url".
	// The client might wrap it as "mcp_scrape_url".

	// We'll trust that toolName matches what we exported in listTools.

	switch toolName {
	case "scrape_url":
		url, ok := args["url"].(string)
		if !ok {
			return nil, fmt.Errorf("url parameter required")
		}

		// Check if typescript_config is provided OR if HTML is requested - if so, use Playwright scraper
		tsConfig, _ := args["typescript_config"].(string)
		getHTML, _ := args["get_html"].(bool)

		if (tsConfig != "") || getHTML {
			isAsync, _ := args["async"].(bool)

			// Handle extractions parameter
			extractions := make(map[string]string)
			if ext, ok := args["extractions"].(map[string]interface{}); ok {
				for k, v := range ext {
					if vStr, ok := v.(string); ok {
						extractions[k] = vStr
					}
				}
			}

			// Returns detailed result including HTML if requested
			return s.scrapeWithConfig(ctx, url, tsConfig, isAsync, extractions, getHTML, nil)
		}

		// Use the html-scraper binary tool for better content extraction
		projectRoot := os.Getenv("AGI_PROJECT_ROOT")
		if projectRoot == "" {
			// Try to get current working directory
			if wd, err := os.Getwd(); err == nil {
				projectRoot = wd
			}
		}

		// Find the html-scraper binary
		candidates := []string{
			"/app/bin/tools/html_scraper", // Kubernetes deployment path
			filepath.Join(projectRoot, "bin", "tools", "html_scraper"),
			filepath.Join(projectRoot, "bin", "html-scraper"),
			"bin/html-scraper",
			"../bin/html-scraper",
		}

		scraperBin := ""
		for _, candidate := range candidates {
			if _, err := os.Stat(candidate); err == nil {
				if abs, err := filepath.Abs(candidate); err == nil {
					scraperBin = abs
				} else {
					scraperBin = candidate
				}
				log.Printf("🔍 [MCP-SCRAPE] Found html_scraper at: %s", scraperBin)
				break
			}
		}

		if scraperBin == "" {
			log.Printf("⚠️ [MCP-SCRAPE] html_scraper binary not found, using fallback HTTP client with HTML cleaning")
			// Fallback to raw HTTP client if scraper not found
			client := NewSafeHTTPClient()
			content, err := client.SafeGetWithContentCheck(ctx, url)
			if err != nil {
				return nil, err
			}

			// Clean up HTML to make it more readable
			// Remove script tags, style tags, and extract text
			cleaned := cleanHTMLForDisplay(content)

			return map[string]interface{}{
				"content": []map[string]interface{}{
					{
						"type": "text",
						"text": cleaned,
					},
				},
			}, nil
		}

		// Run the html-scraper binary
		cmd := exec.CommandContext(ctx, scraperBin, "-url", url)
		output, err := cmd.CombinedOutput()
		if err != nil {
			return nil, fmt.Errorf("scraper failed: %v - %s", err, string(output))
		}

		// Parse the JSON output from html-scraper
		var scraperResult map[string]interface{}
		if err := json.Unmarshal(output, &scraperResult); err != nil {
			// If not JSON, return raw output
			return map[string]interface{}{
				"content": []map[string]interface{}{
					{
						"type": "text",
						"text": string(output),
					},
				},
			}, nil
		}

		// Format the scraped content nicely
		var contentText strings.Builder

		// Add title if present
		if title, ok := scraperResult["title"].(string); ok && title != "" {
			contentText.WriteString("# ")
			contentText.WriteString(title)
			contentText.WriteString("\n\n")
		}

		// Add items (paragraphs, headings, etc.)
		if items, ok := scraperResult["items"].([]interface{}); ok {
			for _, item := range items {
				if itemMap, ok := item.(map[string]interface{}); ok {
					if text, ok := itemMap["text"].(string); ok && text != "" {
						itemType, _ := itemMap["type"].(string)
						switch itemType {
						case "heading":
							contentText.WriteString("## ")
							contentText.WriteString(text)
							contentText.WriteString("\n\n")
						case "paragraph":
							contentText.WriteString(text)
							contentText.WriteString("\n\n")
						default:
							contentText.WriteString(text)
							contentText.WriteString("\n\n")
						}
					}
				}
			}
		}

		// Return in MCP content format
		return map[string]interface{}{
			"content": []map[string]interface{}{
				{
					"type": "text",
					"text": contentText.String(),
				},
			},
		}, nil

	case "smart_scrape":
		url, _ := args["url"].(string)

		goal, _ := args["goal"].(string)
		if url == "" || goal == "" {
			return nil, fmt.Errorf("url and goal parameters required")
		}

		// Support optional hints
		var userConfig *ScrapeConfig
		if ts, _ := args["typescript_config"].(string); ts != "" {
			userConfig = &ScrapeConfig{
				TypeScriptConfig: ts,
				Extractions:      make(map[string]string),
			}
		}

		if ext, ok := args["extractions"].(map[string]interface{}); ok {
			if userConfig == nil {
				userConfig = &ScrapeConfig{Extractions: make(map[string]string)}
			}
			for k, v := range ext {
				if vStr, ok := v.(string); ok {
					userConfig.Extractions[k] = vStr
				}
			}
		}

		numHints := 0
		if userConfig != nil {
			numHints = len(userConfig.Extractions)
		}
		log.Printf("🔍 [MCP-SMART-SCRAPE] User hints: %d extractions", numHints)
		if userConfig != nil {
			for k, v := range userConfig.Extractions {
				log.Printf("   📋 %s: %s", k, v)
			}
		}

		return s.executeSmartScrape(ctx, url, goal, userConfig)

	case "execute_code":
		code, _ := args["code"].(string)
		language, _ := args["language"].(string)

		if code == "" {
			return nil, fmt.Errorf("code parameter required")
		}
		if language == "" {
			language = "python" // Default
		}

		// Check if we should use the SSH Executor (bypass Docker on RPi)
		execMethod := strings.TrimSpace(os.Getenv("EXECUTION_METHOD"))
		enableARM := strings.TrimSpace(os.Getenv("ENABLE_ARM64_TOOLS")) == "true"
		isARM64 := runtime.GOARCH == "arm64" || runtime.GOARCH == "aarch64"

		sshEnabled := execMethod == "ssh" || (isARM64 && (enableARM || execMethod != "docker"))

		if sshEnabled {
			log.Printf("🚀 [MCP-EXEC] Attempting SSH execution (EXECUTION_METHOD=%s, ARM64=%v)", execMethod, isARM64)
			sshParams := map[string]interface{}{
				"code":     code,
				"language": language,
				"timeout":  300,
			}

			// Call the external tool_ssh_executor
			result, err := s.callExternalHDNTool(ctx, "tool_ssh_executor", sshParams)
			if err == nil {
				// Map the result to match expected output format
				success, _ := result["success"].(bool)
				output, _ := result["output"].(string)
				errorMsg, _ := result["error"].(string)

				// Return in standard format
				return map[string]interface{}{
					"success": success,
					"output":  output,
					"error":   errorMsg,
					"files":   nil, // SSH executor doesn't return files directly yet
				}, nil
			}

			log.Printf("⚠️ [MCP-EXEC] SSH executor failed: %v — falling back to local Docker executor", err)
		}

		// Use the Simple Docker Executor (Fallback)
		executor := NewSimpleDockerExecutor() // No storage for ephemeral MCP calls
		req := &DockerExecutionRequest{
			Language: language,
			Code:     code,
			Timeout:  60, // 60s timeout for MCP calls
		}

		result, err := executor.ExecuteCode(ctx, req)
		if err != nil {
			return nil, err
		}

		return map[string]interface{}{
			"success": result.Success,
			"output":  result.Output,
			"error":   result.Error,
			"files":   result.Files,
		}, nil

	case "read_file":
		path, ok := args["path"].(string)
		if !ok {
			return nil, fmt.Errorf("path parameter required")
		}

		// Simple security check (prevent traversing up too far)
		if strings.Contains(path, "..") {
			return nil, fmt.Errorf("invalid path: traversal not allowed")
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("failed to read file: %v", err)
		}
		return string(content), nil

	default:
		return nil, fmt.Errorf("unknown internal tool: %s", toolName)
	}
}

// browseWeb uses a headless browser to navigate, fill forms, click buttons, and extract data
// It's prompt-driven: if instructions are provided, uses LLM to generate actions from page HTML
func (s *MCPKnowledgeServer) browseWeb(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	url, ok := args["url"].(string)
	if !ok || url == "" {
		return nil, fmt.Errorf("url parameter required")
	}

	instructions, _ := args["instructions"].(string)
	if instructions == "" {
		return nil, fmt.Errorf("instructions parameter required - describe what to do on the page")
	}

	timeout := 60
	if t, ok := args["timeout"].(float64); ok && t > 0 {
		timeout = int(t)
	}

	screenshotPath, _ := args["screenshot"].(string)
	getHTML, _ := args["get_html"].(bool)

	// Parse actions if provided (optional - LLM will generate if not provided)
	var actions []map[string]interface{}
	if actionsRaw, ok := args["actions"].([]interface{}); ok && len(actionsRaw) > 0 {
		for _, actionRaw := range actionsRaw {
			if actionMap, ok := actionRaw.(map[string]interface{}); ok {
				actions = append(actions, actionMap)
			}
		}
		log.Printf("🌐 [BROWSE-WEB] Using %d pre-defined actions", len(actions))
	} else {
		log.Printf("🌐 [BROWSE-WEB] No actions provided, will use LLM to generate from instructions")
	}

	// Find the headless_browser binary
	projectRoot := os.Getenv("AGI_PROJECT_ROOT")
	if projectRoot == "" {
		if wd, err := os.Getwd(); err == nil {
			projectRoot = wd
		}
	}

	candidates := []string{
		filepath.Join(projectRoot, "bin", "headless-browser"),
		filepath.Join(projectRoot, "bin", "tools", "headless_browser"),
		"bin/headless-browser",
		"../bin/headless-browser",
	}

	browserBin := ""
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			if abs, err := filepath.Abs(candidate); err == nil {
				browserBin = abs
			} else {
				browserBin = candidate
			}
			break
		}
	}

	if browserBin == "" {
		return nil, fmt.Errorf("headless-browser binary not found. Please build it first: cd tools/headless_browser && go build -o ../../bin/headless-browser")
	}

	// If no actions provided, use LLM to generate them from instructions
	if len(actions) == 0 {
		if s.llmClient == nil {
			log.Printf("⚠️ [BROWSE-WEB] LLM client not available, cannot generate actions from instructions")
		} else {
			// Update progress early
			if screenshotPath != "" {
				saveProgress(screenshotPath, "Analyzing page structure...", -1, -1, "")
			}
			log.Printf("🤖 [BROWSE-WEB] Generating actions from instructions using LLM...")

			// First, get the page HTML to analyze
			var htmlOutputStr string
			var skipFetch bool

			// OPTIMIZATION: If HTML was provided in arguments, use it directly
			providedHTML, _ := args["page_html"].(string)
			if providedHTML != "" {
				log.Printf("📄 [BROWSE-WEB] Using provided page HTML (%d bytes) for action generation", len(providedHTML))
				htmlMap := map[string]interface{}{"success": true, "html": providedHTML}
				jsonBytes, _ := json.Marshal(htmlMap)
				htmlOutputStr = string(jsonBytes)
				skipFetch = true

				// Update progress with provided HTML
				if screenshotPath != "" {
					saveProgress(screenshotPath, "Generating action plan...", -1, -1, providedHTML)
				}
			}

			if !skipFetch {
				log.Printf("🌐 [BROWSE-WEB] Fetching page HTML for analysis...")
				if screenshotPath != "" {
					saveProgress(screenshotPath, "Fetching live page...", -1, -1, "")
				}
				// Use shorter timeout for HTML fetch - we just need the HTML structure, not full rendering
				htmlCtx, htmlCancel := context.WithTimeout(ctx, 20*time.Second)
				defer htmlCancel()
				getHTMLCmd := exec.CommandContext(htmlCtx, browserBin,
					"-url", url,
					"-actions", "[]", // Empty actions - just navigate
					"-timeout", "15", // Reduced from 30 to 15 for faster HTML fetch
					"-html", // Return HTML
					"-fast", // Use fast mode for HTML-only operations
				)

				var stderrHTML string
				var err error
				htmlOutputStr, stderrHTML, err = runCommandWithLiveOutput(htmlCtx, getHTMLCmd, "🔍 [BROWSE-WEB][HTML]")
				if err != nil {
					if errors.Is(htmlCtx.Err(), context.DeadlineExceeded) {
						log.Printf("⚠️ [BROWSE-WEB] HTML fetch timed out after 20s")
					} else {
						log.Printf("⚠️ [BROWSE-WEB] Failed to get page HTML: %v, will proceed without it", err)
					}
					if stderrHTML != "" {
						log.Printf("🔍 [BROWSE-WEB][HTML] stderr:\n%s", stderrHTML)
					}
				}
			}

			if htmlOutputStr != "" {
				// Extract JSON from output - look for the browser result JSON object
				// The browser tool outputs JSON with fields: success, url, title, extracted, html
				outputStr := htmlOutputStr

				// Try to find JSON that looks like our browser result (contains "success" and "html" fields)
				// Look for the pattern: {"success":... which is the actual result
				resultPattern := `{"success"`
				resultStart := strings.Index(outputStr, resultPattern)

				if resultStart == -1 {
					// Fallback: look for any JSON object
					resultStart = strings.Index(outputStr, "{")
				}

				if resultStart == -1 {
					log.Printf("⚠️ [BROWSE-WEB] Could not find JSON object in HTML output")
				} else {
					// Find matching closing brace by counting braces (respecting string boundaries)
					braceCount := 0
					jsonEnd := -1
					inString := false
					escapeNext := false

					for i := resultStart; i < len(outputStr); i++ {
						if escapeNext {
							escapeNext = false
							continue
						}

						if outputStr[i] == '\\' {
							escapeNext = true
							continue
						}

						if outputStr[i] == '"' && !escapeNext {
							inString = !inString
							continue
						}

						if !inString {
							if outputStr[i] == '{' {
								braceCount++
							} else if outputStr[i] == '}' {
								braceCount--
								if braceCount == 0 {
									jsonEnd = i
									break
								}
							}
						}
					}

					if jsonEnd != -1 {
						jsonStr := outputStr[resultStart : jsonEnd+1]

						// Try to parse JSON
						var htmlResult map[string]interface{}
						if err := json.Unmarshal([]byte(jsonStr), &htmlResult); err != nil {
							log.Printf("⚠️ [BROWSE-WEB] Failed to parse HTML result JSON: %v", err)
							log.Printf("📄 [BROWSE-WEB] JSON preview (first 200 chars): %s", jsonStr[:min(200, len(jsonStr))])
						} else {
							// Successfully parsed JSON - verify it's our result object
							if _, hasSuccess := htmlResult["success"]; hasSuccess {
								if html, ok := htmlResult["html"].(string); ok && html != "" {
									log.Printf("📄 [BROWSE-WEB] Got page HTML: %d bytes", len(html))
									// Update progress for planning
									if screenshotPath != "" {
										saveProgress(screenshotPath, "Planning interaction sequence...", -1, -1, html)
									}
									// Use LLM to generate actions from HTML and instructions
									actions, err = s.generateActionsFromInstructions(ctx, url, instructions, html)
									if err != nil {
										log.Printf("⚠️ [BROWSE-WEB] LLM action generation failed: %v, will try with empty actions", err)
										actions = []map[string]interface{}{}
									} else {
										log.Printf("✅ [BROWSE-WEB] LLM generated %d actions", len(actions))
									}
								} else {
									log.Printf("⚠️ [BROWSE-WEB] HTML field missing or empty in result")
								}
							} else {
								log.Printf("⚠️ [BROWSE-WEB] Extracted JSON doesn't look like browser result (no 'success' field)")
							}
						}
					} else {
						log.Printf("⚠️ [BROWSE-WEB] Could not find complete JSON in HTML output")
					}
				}
			}
		}
	}

	var lastErr error
	var successfulActions []map[string]interface{}

	// SELF-HEALING LOOP: Try up to 3 times to complete the task by re-planning if actions fail
	for attempt := 0; attempt < 3; attempt++ {
		toolCtx, toolCancel := context.WithTimeout(ctx, time.Duration(timeout+15)*time.Second)

		if attempt > 0 {
			log.Printf("🔄 [BROWSE-WEB] Healing attempt %d/3...", attempt)
		}

		// Convert CURRENT actions to JSON
		actionsJSON, err := json.Marshal(actions)
		if err != nil {
			toolCancel()
			return nil, fmt.Errorf("failed to marshal actions: %w", err)
		}

		browserArgs := []string{
			"-url", url,
			"-actions", string(actionsJSON),
			"-timeout", fmt.Sprintf("%d", 15),
			"-fast",
		}
		if screenshotPath != "" {
			browserArgs = append(browserArgs, "-screenshot", screenshotPath)
		}
		if getHTML {
			browserArgs = append(browserArgs, "-html")
		}

		runCmd := exec.CommandContext(toolCtx, browserBin, browserArgs...)
		stdoutStr, stderrStr, runErr := runCommandWithLiveOutput(toolCtx, runCmd, "🔍 [BROWSE-WEB][TOOL]")
		toolCancel() // Cancel context immediately after command finishes

		// Log stderr
		if stderrStr != "" {
			log.Printf("🔍 [BROWSE-WEB] Browser tool debug output (attempt %d):\n%s", attempt, stderrStr)
		}

		// Extract JSON from result
		outputStr := stdoutStr
		jsonStart := strings.Index(outputStr, "{")
		if jsonStart == -1 {
			log.Printf("⚠️ [BROWSE-WEB] Attempt %d: No JSON found in output (stdout len: %d)", attempt, len(stdoutStr))
			// If it's the last attempt and we have no JSON, return error
			if attempt == 2 {
				return nil, fmt.Errorf("failed to find JSON object in browser output after 3 attempts. Last stdout: %s", stdoutStr)
			}
			lastErr = fmt.Errorf("no JSON found in output")
			continue
		}

		// Find matching closing brace
		braceCount := 0
		jsonEnd := -1
		inString := false
		escapeNext := false
		for i := jsonStart; i < len(outputStr); i++ {
			if escapeNext {
				escapeNext = false
				continue
			}
			if outputStr[i] == '\\' {
				escapeNext = true
				continue
			}
			if outputStr[i] == '"' && !escapeNext {
				inString = !inString
				continue
			}
			if !inString {
				if outputStr[i] == '{' {
					braceCount++
				} else if outputStr[i] == '}' {
					braceCount--
					if braceCount == 0 {
						jsonEnd = i
						break
					}
				}
			}
		}

		if jsonEnd == -1 {
			if attempt == 2 {
				return nil, fmt.Errorf("failed to find complete JSON in browser output")
			}
			continue
		}

		jsonStr := outputStr[jsonStart : jsonEnd+1]
		var result map[string]interface{}
		if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
			lastErr = err
			continue
		}

		// Check if it failed at a specific step
		status, _ := result["status"].(string)
		if status == "Failed" {
			failedStepIdx, _ := result["failed_step"].(float64)
			idx := int(failedStepIdx)
			log.Printf("⚠️ [BROWSE-WEB] Action %d failed. Attempting to heal...", idx)

			// Capture the ones that worked
			if idx > 0 {
				successfulActions = append(successfulActions, actions[:idx]...)
			}

			// Get current HTML to plan repair
			currentHTML, _ := result["html"].(string)
			if currentHTML == "" {
				return nil, fmt.Errorf("action %d failed and no HTML returned for healing", idx)
			}

			// Call LLM with the *current* state to get a REPAIR plan
			log.Printf("🤖 [BROWSE-WEB] Asking LLM for repair actions starting from failed step...")
			failedAction, _ := json.Marshal(actions[idx])
			repairPrompt := fmt.Sprintf(`### SELF-HEAL REQUEST ###
The previous action sequence failed. 
FAILED ACTION: %s
ERROR: %v

Original Goal: %s

INSTRUCTION: 
1. Analyze why the previous action failed using the provided HTML.
2. Provide a NEW sequence of actions to RECOVER and COMPLETE the task.
3. Your response MUST be a complete plan from this current state to the goal.
4. Do not just return one step. Return ALL remaining steps.`, string(failedAction), result["error"], instructions)

			repairActions, err := s.generateActionsFromInstructions(ctx, url, repairPrompt, currentHTML)
			if err != nil {
				return nil, fmt.Errorf("healing failed — LLM could not generate repair plan: %v", err)
			}

			// New sequence is Successful Steps + Repair Steps
			actions = append(successfulActions, repairActions...)
			lastErr = fmt.Errorf("step %d failed: %v", idx, result["error"])
			continue // Try again with the healed sequence
		}

		// If success!
		if runErr != nil && status != "Failed" {
			return nil, fmt.Errorf("browser execution failed: %v", runErr)
		}

		extracted := result["extracted"]
		if extracted == nil {
			extracted = make(map[string]interface{})
		}

		return map[string]interface{}{
			"content": []map[string]interface{}{
				{
					"type": "text",
					"text": fmt.Sprintf("Successfully browsed to %s\n\nExtracted data:\n%s", url, formatExtractedData(extracted)),
				},
			},
			"extracted": extracted,
			"url":       url,
			"title":     result["title"],
			"html":      result["html"],
		}, nil
	}

	return nil, fmt.Errorf("max healing attempts reached. Last error: %v", lastErr)
}

// formatExtractedData formats extracted data as a readable string
func formatExtractedData(data interface{}) string {
	if data == nil {
		return "No data extracted"
	}

	if dataMap, ok := data.(map[string]interface{}); ok {
		var builder strings.Builder
		for k, v := range dataMap {
			builder.WriteString(fmt.Sprintf("  %s: %v\n", k, v))
		}
		return builder.String()
	}

	return fmt.Sprintf("%v", data)
}

// extractFormStructure extracts key form elements from HTML to help LLM generate better selectors
func extractFormStructure(html string) string {
	var info strings.Builder

	// IDENTIFY MAIN CALCULATOR FIELDS
	calcKeywords := []string{"from", "to", "origin", "destination", "start", "end", "calc", "emission", "depart", "arrive", "address"}

	// Extract input fields with their attributes - be more specific
	inputRe := regexp.MustCompile(`(?i)<input[^>]*>`)
	inputs := inputRe.FindAllString(html, -1)
	if len(inputs) > 0 {
		info.WriteString("Input fields found (use these EXACT selectors):\n")
		for i, input := range inputs {
			// Check if this input seems like a calculator field
			isCalcHint := false
			inputLower := strings.ToLower(input)
			for _, kw := range calcKeywords {
				if strings.Contains(inputLower, kw) {
					isCalcHint = true
					break
				}
			}

			if i < 20 { // Show more inputs
				// Extract key attributes
				var attrs []string
				hint := ""
				if isCalcHint {
					hint = " [Likely Calculator Field]"
				}

				if idMatch := regexp.MustCompile(`(?i)id=["']([^"']+)["']`).FindStringSubmatch(input); len(idMatch) > 1 {
					attrs = append(attrs, fmt.Sprintf("id='%s' → selector: #%s", idMatch[1], idMatch[1]))
				}
				if nameMatch := regexp.MustCompile(`(?i)name=["']([^"']+)["']`).FindStringSubmatch(input); len(nameMatch) > 1 {
					attrs = append(attrs, fmt.Sprintf("name='%s' → selector: input[name='%s']", nameMatch[1], nameMatch[1]))
				}
				if placeholderMatch := regexp.MustCompile(`(?i)placeholder=["']([^"']+)["']`).FindStringSubmatch(input); len(placeholderMatch) > 1 {
					attrs = append(attrs, fmt.Sprintf("placeholder='%s' → selector: input[placeholder='%s']", placeholderMatch[1], placeholderMatch[1]))
				}
				if dataMatch := regexp.MustCompile(`(?i)data-[^=]+=["']([^"']+)["']`).FindStringSubmatch(input); len(dataMatch) > 0 {
					dataAttr := regexp.MustCompile(`(?i)(data-[^=]+)=`).FindStringSubmatch(input)
					if len(dataAttr) > 1 {
						attrs = append(attrs, fmt.Sprintf("%s → selector: input[%s]", dataAttr[1], dataAttr[1]))
					}
				}
				if len(attrs) > 0 {
					info.WriteString(fmt.Sprintf("  Input %d:%s %s\n", i+1, hint, strings.Join(attrs, ", ")))
				} else {
					info.WriteString(fmt.Sprintf("  Input %d: %s (no clear identifiers)\n", i+1, input[:min(80, len(input))]))
				}
			}
		}
		if len(inputs) > 20 {
			info.WriteString(fmt.Sprintf("  ... and %d more inputs\n", len(inputs)-20))
		}
	}

	// Extract buttons (including React/Vue components that might render as divs with role="button")
	buttonRe := regexp.MustCompile(`(?i)<button[^>]*>.*?</button>`)
	buttons := buttonRe.FindAllString(html, -1)

	// Also look for div/span/a elements with role="button" or button-like classes (simplified - no backreferences)
	buttonLikeDivRe := regexp.MustCompile(`(?i)<div[^>]*(?:role=["']button["']|class=["'][^"']*button[^"']*["'])[^>]*>`)
	buttonLikeSpanRe := regexp.MustCompile(`(?i)<span[^>]*(?:role=["']button["']|class=["'][^"']*button[^"']*["'])[^>]*>`)
	buttonLikeARe := regexp.MustCompile(`(?i)<a[^>]*(?:role=["']button["']|class=["'][^"']*button[^"']*["'])[^>]*>`)
	buttonLikeDiv := buttonLikeDivRe.FindAllString(html, -1)
	buttonLikeSpan := buttonLikeSpanRe.FindAllString(html, -1)
	buttonLikeA := buttonLikeARe.FindAllString(html, -1)
	totalButtonLike := len(buttonLikeDiv) + len(buttonLikeSpan) + len(buttonLikeA)

	if len(buttons) > 0 || totalButtonLike > 0 {
		info.WriteString(fmt.Sprintf("\nButtons found: %d (plus %d button-like elements)\n", len(buttons), totalButtonLike))
		for i, btn := range buttons {
			if i < 10 { // Show more buttons
				// Extract attributes
				var attrs []string
				if idMatch := regexp.MustCompile(`(?i)id=["']([^"']+)["']`).FindStringSubmatch(btn); len(idMatch) > 1 {
					attrs = append(attrs, fmt.Sprintf("id='%s' → selector: #%s", idMatch[1], idMatch[1]))
				}
				if classMatch := regexp.MustCompile(`(?i)class=["']([^"']+)["']`).FindStringSubmatch(btn); len(classMatch) > 1 {
					// Extract first meaningful class
					classes := strings.Fields(classMatch[1])
					if len(classes) > 0 {
						attrs = append(attrs, fmt.Sprintf("class='%s' → selector: .%s", classes[0], classes[0]))
					}
				}
				// Extract text content
				textRe := regexp.MustCompile(`(?i)>([^<]+)<`)
				if textMatch := textRe.FindStringSubmatch(btn); len(textMatch) > 1 {
					text := strings.TrimSpace(textMatch[1])
					if text != "" {
						attrs = append(attrs, fmt.Sprintf("text='%s'", text))
					}
				}
				if len(attrs) > 0 {
					info.WriteString(fmt.Sprintf("  Button %d: %s\n", i+1, strings.Join(attrs, ", ")))
				}
			}
		}
	}

	// Look for form-related IDs and classes
	idRe := regexp.MustCompile(`(?i)id=["']([^"']*(?:from|to|origin|destination|calculate|submit|co2|result)[^"']*)["']`)
	ids := idRe.FindAllStringSubmatch(html, -1)
	if len(ids) > 0 {
		info.WriteString("\nRelevant IDs found:\n")
		for i, match := range ids {
			if i < 10 {
				info.WriteString(fmt.Sprintf("  - #%s\n", match[1]))
			}
		}
	}

	result := info.String()
	if result == "" {
		return "No clear form structure detected. Look for input fields, buttons, and form elements in the HTML."
	}

	return result
}

// extractSelectorsFromFormInfo extracts all valid selectors mentioned in the form info string
func extractSelectorsFromFormInfo(formInfo string) []string {
	var selectors []string
	// Look for patterns like "selector: #id" or "selector: input[name='name']"
	selectorRe := regexp.MustCompile(`selector:\s*([^\n,]+)`)
	matches := selectorRe.FindAllStringSubmatch(formInfo, -1)
	for _, match := range matches {
		if len(match) > 1 {
			selector := strings.TrimSpace(match[1])
			if selector != "" {
				selectors = append(selectors, selector)
			}
		}
	}
	// Also extract IDs directly (patterns like "id='xyz' → selector: #xyz")
	idRe := regexp.MustCompile(`id=['"]([^'"]+)['"]`)
	idMatches := idRe.FindAllStringSubmatch(formInfo, -1)
	for _, match := range idMatches {
		if len(match) > 1 {
			selectors = append(selectors, "#"+match[1])
		}
	}
	// Extract name attributes (patterns like "name='xyz' → selector: input[name='xyz']")
	nameRe := regexp.MustCompile(`name=['"]([^'"]+)['"]`)
	nameMatches := nameRe.FindAllStringSubmatch(formInfo, -1)
	for _, match := range nameMatches {
		if len(match) > 1 {
			selectors = append(selectors, fmt.Sprintf("input[name='%s']", match[1]))
		}
	}
	return selectors
}

// isSelectorValid checks if a selector matches any of the valid selectors or common patterns
func isSelectorValid(selector string, validSelectors []string) bool {
	// Always allow "body" selector
	if selector == "body" {
		return true
	}
	// Check if selector matches any valid selector exactly
	for _, valid := range validSelectors {
		if selector == valid {
			return true
		}
		// Also check if selector contains the valid selector (for comma-separated selectors)
		if strings.Contains(selector, valid) {
			return true
		}
	}
	// Check for common valid patterns
	validPatterns := []string{
		"input[",
		"button[",
		"select[",
		"textarea[",
		"[data-",
		"#",
		".",
	}
	for _, pattern := range validPatterns {
		if strings.Contains(selector, pattern) {
			return true // Might be valid, let it through
		}
	}
	return false
}

func parseFromTo(instructions string) (string, string) {
	// Extract "from" and "to" values from natural language instructions.
	// Examples handled: "from Southampton to Newcastle", "from field with Southampton, to field with Newcastle"
	fromRe := regexp.MustCompile(`(?i)\bfrom\b\s+(?:field\s+)?(?:with\s+)?([A-Za-z][A-Za-z\s\-'"]+)`)
	toRe := regexp.MustCompile(`(?i)\bto\b\s+(?:field\s+)?(?:with\s+)?([A-Za-z][A-Za-z\s\-'"]+)`)

	fromMatch := fromRe.FindStringSubmatch(instructions)
	toMatch := toRe.FindStringSubmatch(instructions)

	from := ""
	to := ""
	if len(fromMatch) > 1 {
		from = strings.TrimSpace(strings.Trim(fromMatch[1], " ,.;\"'"))
	}
	if len(toMatch) > 1 {
		to = strings.TrimSpace(strings.Trim(toMatch[1], " ,.;\"'"))
	}
	return from, to
}

func buildEcotreeActions(instructions string) ([]map[string]interface{}, error) {
	from, to := parseFromTo(instructions)
	if from == "" || to == "" {
		return nil, fmt.Errorf("unable to parse from/to locations from instructions: %q", instructions)
	}

	// Use name-based selectors (multiple inputs share id=airportName, so name is more reliable)
	fromSelector := "input[name='From']"
	toSelector := "input[name='To']"
	// Try multiple strategies to find calculate button - prioritize form submit buttons
	// Look for buttons within forms first, then try the visible button
	calcSelector := "form button[type='submit'], form button.btn-primary, button[type='submit']:visible, .btn.btn-primary.hover-arrow, button.btn-primary, text=/calculate.*emissions/i"
	resultSelector := "text=/kg.*co2/i, text=/CO2.*emissions/i, .result, [data-testid*='result'], [class*='result']"

	actions := []map[string]interface{}{
		{"type": "wait", "selector": "body", "timeout": 5},
		{"type": "wait", "wait_for": fromSelector, "timeout": 10},
		{"type": "fill", "selector": fromSelector, "value": from},
		{"type": "wait", "selector": "body", "timeout": 1},
		{"type": "fill", "selector": toSelector, "value": to},
		{"type": "wait", "selector": "body", "timeout": 1},
		{"type": "click", "selector": calcSelector},
		{"type": "wait", "selector": "body", "timeout": 3},          // Wait for calculation to start
		{"type": "wait", "wait_for": resultSelector, "timeout": 20}, // Longer wait for result
		{"type": "wait", "selector": "body", "timeout": 2},          // Additional wait to ensure result is fully rendered
		{"type": "extract", "extract": map[string]string{
			"co2_emissions": resultSelector,
		}},
	}
	return actions, nil
}

func runCommandWithLiveOutput(ctx context.Context, cmd *exec.Cmd, logPrefix string) (string, string, error) {
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return "", "", fmt.Errorf("failed to create stdout pipe: %w", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return "", "", fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return "", "", err
	}

	var stdoutBuf bytes.Buffer
	var stderrBuf bytes.Buffer
	var wg sync.WaitGroup

	readPipe := func(r io.Reader, buf *bytes.Buffer, logLines bool) {
		defer wg.Done()
		scanner := bufio.NewScanner(r)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			buf.WriteString(line)
			buf.WriteByte('\n')
			if logLines {
				log.Printf("%s %s", logPrefix, line)
			}
		}
		if err := scanner.Err(); err != nil {
			log.Printf("%s scanner error: %v", logPrefix, err)
		}
	}

	wg.Add(2)
	go readPipe(stdoutPipe, &stdoutBuf, false)
	go readPipe(stderrPipe, &stderrBuf, true)

	// Wait for readers to finish first to avoid "file already closed" when cmd.Wait closes them
	wg.Wait()
	waitErr := cmd.Wait()

	return stdoutBuf.String(), stderrBuf.String(), waitErr
}
func saveProgress(path string, status string, step int, total int, html string) {
	progressPath := path + ".progress"
	prog := map[string]interface{}{
		"status": status,
		"step":   step,
		"total":  total,
		"html":   html,
	}
	data, _ := json.Marshal(prog)
	_ = os.WriteFile(progressPath, data, 0644)
}

// generateActionsFromInstructions uses LLM to generate browser actions from natural language instructions
func (s *MCPKnowledgeServer) generateActionsFromInstructions(ctx context.Context, url, instructions, pageHTML string) ([]map[string]interface{}, error) {
	// Extract key form elements from HTML to help LLM find the right selectors
	// Look for input fields, buttons, and form structure
	formInfo := extractFormStructure(pageHTML)

	// Truncate HTML to avoid token limits, but try to keep form-related content
	// Reduce to 30KB to speed up LLM processing
	htmlPreview := pageHTML
	if len(htmlPreview) > 30000 {
		// Try to find form-related content first
		formStart := strings.Index(strings.ToLower(htmlPreview), "<form")
		if formStart != -1 && formStart < 30000 {
			// Keep form content plus some context
			start := max(0, formStart-3000)
			end := min(len(htmlPreview), formStart+27000)
			htmlPreview = htmlPreview[start:end]
			if start > 0 {
				htmlPreview = "... (earlier content truncated) ...\n" + htmlPreview
			}
			if end < len(pageHTML) {
				htmlPreview = htmlPreview + "\n... (later content truncated) ..."
			}
		} else {
			// Fallback: just truncate from start
			htmlPreview = htmlPreview[:30000] + "\n... (truncated)"
		}
	}

	// Create prompt for LLM to generate actions (optimized for size)
	// Use very explicit instructions to force JSON output
	// First, extract and list the actual selectors more prominently
	selectorList := extractSelectorsFromFormInfo(formInfo)
	selectorListStr := ""
	if len(selectorList) > 0 {
		selectorListStr = "\n\nAVAILABLE SELECTORS (copy these EXACTLY - do not invent new ones):\n"
		for i, sel := range selectorList {
			if i < 20 { // Limit to first 20
				selectorListStr += fmt.Sprintf("- %s\n", sel)
			}
		}
		if len(selectorList) > 20 {
			selectorListStr += fmt.Sprintf("... and %d more\n", len(selectorList)-20)
		}
	} else {
		selectorListStr = "\n\nWARNING: No clear selectors found. Look in the Form Elements section below for selectors.\n"
	}

	prompt := fmt.Sprintf(`You are an expert browser automation agent. Your goal is to generate a JSON array of actions to achieve the user's mission.
	
URL: %s
User Goal: %s
%s
Available Form Elements:
%s

STRATEGY RULES:
1. **STABILITY FIRST**: Use #id, name='...', or placeholder='...' selectors. They are more stable than classes or text.
2. **SPELLING MATTERS**: If you click an autocomplete suggestion, use the EXACT text you saw in the HTML or dropdown. (e.g., if you typed "Paris", the dropdown might say "Paris - Charles de Gaulle (CDG)").
3. **DROPDOWN FLOW**: To handle autocompletes:
   a. "fill" the input
   b. "wait" 1 second
   c. "click" the correctly spelled option from the list.
4. **CO2/CALCULATORS**: For flight calculators, look for "From", "To", "One-way/Return", and "Passengers".
5. **NO HALLUCIDATIONS**: ONLY use selectors or text fragments actually visible in the provided HTML or form elements list.

JSON FORMAT:
Return ONLY a JSON array [{}, {}].
Allowed "type": wait, fill, click, select, extract.
Selector: Use CSS or "text=Exact Text". Regex "text=/.../i" is allowed only if exact match fails.

EXAMPLE:
[{"type":"wait","selector":"body","timeout":2},{"type":"fill","selector":"#from","value":"Paris"},{"type":"wait","timeout":1},{"type":"click","selector":"text=/Charles de Gaulle/i"}]`, url, instructions, selectorListStr, formInfo)

	// Call LLM with timeout context (3 minutes for large HTML processing)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	log.Printf("📞 [BROWSE-WEB] Calling LLM with prompt size: %d bytes (HTML preview: %d bytes)", len(prompt), len(htmlPreview))
	startTime := time.Now()

	// Use callLLMWithContextAndPriority for better timeout handling and high priority
	// Use PriorityHigh for user-facing browser automation
	response, err := s.llmClient.callLLMWithContextAndPriority(ctx, prompt, PriorityHigh)

	duration := time.Since(startTime)
	log.Printf("⏱️ [BROWSE-WEB] LLM call completed in %v", duration)

	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("LLM call timed out after %v (prompt size: %d bytes)", duration, len(prompt))
		}
		return nil, fmt.Errorf("LLM call failed (after %v): %w", duration, err)
	}

	// Log the raw response for debugging (show start and end)
	log.Printf("📝 [BROWSE-WEB] LLM raw response length: %d bytes", len(response))
	if len(response) > 1000 {
		start := response[:500]
		end := response[len(response)-500:]
		log.Printf("📝 [BROWSE-WEB] LLM response START (first 500 chars): %s", start)
		log.Printf("📝 [BROWSE-WEB] LLM response END (last 500 chars): %s", end)
	} else {
		log.Printf("📝 [BROWSE-WEB] LLM raw response: %s", response)
	}

	// Extract JSON from response (may be wrapped in markdown or have text before/after)
	jsonStr := extractJSONFromResponse(response)
	if jsonStr == "" {
		log.Printf("⚠️ [BROWSE-WEB] Failed to extract JSON from LLM response. Full response length: %d", len(response))
		// Save full response to file for debugging
		if err := os.WriteFile("/tmp/llm_response_debug.txt", []byte(response), 0644); err == nil {
			log.Printf("💾 [BROWSE-WEB] Saved full LLM response to /tmp/llm_response_debug.txt for debugging")
		}
		// Try to find any JSON-like patterns - look for array start
		if strings.Contains(response, "[") {
			log.Printf("🔍 [BROWSE-WEB] Response contains '[', attempting manual extraction...")
			// Try to find the first [ and last ]
			firstBracket := strings.Index(response, "[")
			lastBracket := strings.LastIndex(response, "]")
			if firstBracket != -1 && lastBracket != -1 && lastBracket > firstBracket {
				potentialJSON := response[firstBracket : lastBracket+1]
				log.Printf("🔍 [BROWSE-WEB] Found potential JSON (length: %d), first 200 chars: %s", len(potentialJSON), potentialJSON[:min(200, len(potentialJSON))])
				// Try to parse it
				if _, err := tryParseJSON(potentialJSON); err == nil {
					log.Printf("✅ [BROWSE-WEB] Successfully parsed extracted JSON!")
					jsonStr = potentialJSON
				} else {
					log.Printf("❌ [BROWSE-WEB] Extracted JSON still failed to parse: %v", err)
				}
			}
		}
		if jsonStr == "" {
			return nil, fmt.Errorf("no JSON found in LLM response")
		}
	}

	log.Printf("✅ [BROWSE-WEB] Extracted JSON from LLM response (length: %d)", len(jsonStr))

	// Parse actions
	var actions []map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &actions); err != nil {
		return nil, fmt.Errorf("failed to parse LLM-generated actions: %w", err)
	}

	// Validate actions - check if selectors are reasonable (not hallucinated)
	// Extract all valid selectors from formInfo
	validSelectors := extractSelectorsFromFormInfo(formInfo)
	log.Printf("🔍 [BROWSE-WEB] Valid selectors from form structure: %v", validSelectors)

	// Log generated actions for debugging
	log.Printf("📋 [BROWSE-WEB] LLM generated %d actions:", len(actions))
	for i, action := range actions {
		actionType, _ := action["type"].(string)
		selector, _ := action["selector"].(string)
		waitFor, _ := action["wait_for"].(string)
		value, _ := action["value"].(string)

		// Warn if selector doesn't match any known pattern
		if selector != "" && selector != "body" {
			if !isSelectorValid(selector, validSelectors) {
				log.Printf("⚠️ [BROWSE-WEB] Action [%d] uses potentially invalid selector: %s (not found in form structure)", i+1, selector)
			}
		}
		log.Printf("  [%d] %s: selector=%s, wait_for=%s, value=%s", i+1, actionType, selector, waitFor, value)
	}

	return actions, nil
}

// extractJSONFromResponse extracts JSON array from LLM response (handles markdown code blocks)
func extractJSONFromResponse(response string) string {
	trimmed := strings.TrimSpace(response)

	// Remove common prefixes
	prefixes := []string{"SOURCES:", "Here is", "Here's", "The JSON", "JSON:", "Actions:", "Here are", "Koffie:", "Koffie", "Coffee:", "Coffee"}
	jsonCandidate := trimmed
	for _, prefix := range prefixes {
		lower := strings.ToLower(jsonCandidate)
		pLower := strings.ToLower(prefix)
		if strings.HasPrefix(lower, pLower) {
			after := jsonCandidate[len(prefix):]
			idx := strings.Index(after, "[")
			if idx != -1 {
				jsonCandidate = after[idx:]
				break
			}
		}
	}

	// Handle markdown code fences
	if strings.Contains(jsonCandidate, "```") {
		re := regexp.MustCompile("(?s)```(?:json)?\n?(.*?)\n?```")
		match := re.FindStringSubmatch(jsonCandidate)
		if len(match) > 1 {
			return strings.TrimSpace(match[1])
		}
	}

	// Fallback: find first [ and last ]
	start := strings.Index(jsonCandidate, "[")
	end := strings.LastIndex(jsonCandidate, "]")
	if start != -1 && end != -1 && end > start {
		return strings.TrimSpace(jsonCandidate[start : end+1])
	}

	return jsonCandidate
}

// tryParseJSON attempts to parse JSON, and if it fails, tries to repair common LLM errors
func tryParseJSON(jsonStr string) (string, error) {
	var test interface{}
	// First try: Plain parse
	if err := json.Unmarshal([]byte(jsonStr), &test); err == nil {
		return jsonStr, nil
	}

	// Second try: Basic cleaning
	cleaned := strings.TrimSpace(jsonStr)
	if err := json.Unmarshal([]byte(cleaned), &test); err == nil {
		return cleaned, nil
	}

	// Third try: Repairing unclosed strings and brackets
	repaired := repairJSON(cleaned)
	if err := json.Unmarshal([]byte(repaired), &test); err == nil {
		return repaired, nil
	}

	return "", fmt.Errorf("failed to parse JSON after repair")
}

// repairJSON attempts to fix common LLM mistakes like unclosed quotes or missing brackets
func repairJSON(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}

	// 1. Fix unclosed quotes at end of lines or before commas/braces
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		quoteCount := strings.Count(line, "\"")
		if quoteCount%2 != 0 {
			// Odd number of quotes - likely missing a closing quote
			trimmed := strings.TrimRight(line, " \t\r,}]")
			lines[i] = trimmed + "\"" + line[len(trimmed):]
		}
	}
	s = strings.Join(lines, "\n")

	// 2. Remove trailing commas before closing braces/brackets
	s = regexp.MustCompile(`,(\s*[\]\}])`).ReplaceAllString(s, "$1")

	// 3. Balance brackets and braces
	depth := 0
	for _, char := range s {
		if char == '{' {
			depth++
		} else if char == '}' {
			depth--
		}
	}
	for depth > 0 {
		s += "}"
		depth--
	}

	depth = 0
	for _, char := range s {
		if char == '[' {
			depth++
		} else if char == ']' {
			depth--
		}
	}
	for depth > 0 {
		s += "]"
		depth--
	}

	return s
}

// queryViaHDN queries Neo4j via HDN's knowledge query endpoint
func (s *MCPKnowledgeServer) queryViaHDN(ctx context.Context, cypherQuery string) (interface{}, error) {
	queryURL := s.hdnURL + "/api/v1/knowledge/query"
	if s.hdnURL == "" {
		queryURL = "http://localhost:8081/api/v1/knowledge/query"
	} else {
		// If connecting to ourselves (Kubernetes service DNS or same host), use localhost
		// This prevents connection issues when MCP server tries to call HDN via service DNS
		if isSelfConnectionHDN(queryURL) {
			// Parse URL to get port, default to 8080
			parsedURL, err := url.Parse(queryURL)
			if err == nil {
				port := parsedURL.Port()
				if port == "" {
					port = "8081" // Default HDN port
				}
				queryURL = fmt.Sprintf("http://localhost:%s/api/v1/knowledge/query", port)
				log.Printf("🔧 [MCP-KNOWLEDGE] Detected self-connection for HDN query, using localhost: %s", queryURL)
			}
		}
	}

	reqBody := map[string]string{"query": cypherQuery}
	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	resp, err := http.Post(queryURL, "application/json", strings.NewReader(string(jsonData)))
	if err != nil {
		return nil, fmt.Errorf("failed to query HDN: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HDN returned status %d", resp.StatusCode)
	}

	var result struct {
		Results []map[string]interface{} `json:"results"`
		Count   int                      `json:"count"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"results": result.Results,
		"count":   result.Count,
	}, nil
}

// searchWeaviate searches the Weaviate vector database
func (s *MCPKnowledgeServer) searchWeaviate(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	query, ok := args["query"].(string)
	if !ok || query == "" {
		return nil, fmt.Errorf("query parameter is required")
	}

	log.Printf("🔍 [MCP-KNOWLEDGE] searchWeaviate called with query: '%s'", query)

	limit := 10
	if l, ok := args["limit"].(float64); ok {
		limit = int(l)
	}
	if limit <= 0 {
		limit = 10
	}

	collection := "AgiEpisodes" // default collection
	if c, ok := args["collection"].(string); ok && c != "" {
		collection = c
	}

	// Route AvatarContext to specialized semantic search
	if collection == "AvatarContext" {
		return s.searchAvatarContext(ctx, args)
	}

	if s.vectorDB == nil {
		return nil, fmt.Errorf("Weaviate not available")
	}

	// Convert text query to vector using toy embedder (same as used elsewhere in the system)
	// This is a simple hash-based embedding - in production you'd use a real embedding model
	vec := s.toyEmbed(query, 8)

	// Handle different collection types
	if collection == "AgiEpisodes" || collection == "AgiWiki" {
		// Use SearchEpisodes for episodic memory collections
		results, err := s.vectorDB.SearchEpisodes(vec, limit, map[string]any{})
		if err != nil {
			return nil, fmt.Errorf("weaviate search failed: %w", err)
		}

		// Convert EpisodicRecord to map for JSON response
		var resultMaps []map[string]interface{}
		for _, r := range results {
			resultMaps = append(resultMaps, map[string]interface{}{
				"text":       r.Text,
				"timestamp":  r.Timestamp,
				"metadata":   r.Metadata,
				"session_id": r.SessionID,
				"outcome":    r.Outcome,
				"tags":       r.Tags,
			})
		}

		return map[string]interface{}{
			"results":    resultMaps,
			"count":      len(resultMaps),
			"query":      query,
			"collection": collection,
		}, nil
	} else {
		// For WikipediaArticle and other collections, use direct Weaviate GraphQL query
		// This matches the approach used in monitor/main.go
		return s.searchWeaviateGraphQL(ctx, query, collection, limit, vec)
	}
}

// toyEmbed creates a simple deterministic vector for text (same as used in api.go)
func (s *MCPKnowledgeServer) toyEmbed(text string, dim int) []float32 {
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

// searchAvatarContext searches the AvatarContext collection for personal information about Steven Fisher
func (s *MCPKnowledgeServer) searchAvatarContext(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	query, ok := args["query"].(string)
	if !ok || query == "" {
		return nil, fmt.Errorf("query parameter is required")
	}

	log.Printf("🔍 [MCP-KNOWLEDGE] searchAvatarContext called with query: '%s'", query)

	limit := 5
	if l, ok := args["limit"].(float64); ok {
		limit = int(l)
	}
	if limit <= 0 {
		limit = 5
	}

	// Get embedding for the query using Ollama's nomic-embed-text model
	embedding, err := s.getOllamaEmbedding(ctx, query)
	if err != nil {
		log.Printf("⚠️ [MCP-KNOWLEDGE] Failed to get embedding, falling back to keyword search: %v", err)
		// Fallback to keyword-based search if embedding fails
		return s.searchAvatarContextKeyword(ctx, query, limit)
	}

	// Get Weaviate URL from environment
	weaviateURL := os.Getenv("WEAVIATE_URL")
	if weaviateURL == "" {
		weaviateURL = "http://localhost:8080"
	}

	// Convert embedding to GraphQL vector format
	vectorStr := "["
	for i, v := range embedding {
		if i > 0 {
			vectorStr += ","
		}
		vectorStr += fmt.Sprintf("%.6f", v)
	}
	vectorStr += "]"

	// Build GraphQL query using nearVector for semantic search
	queryStr := fmt.Sprintf(`{
		Get {
			AvatarContext(nearVector: {vector: %s}, limit: %d) {
				_additional {
					id
					distance
				}
				content
				source
			}
		}
	}`, vectorStr, limit)

	queryData := map[string]interface{}{
		"query": queryStr,
	}
	log.Printf("🔍 [MCP-KNOWLEDGE] Sending vector search query to Weaviate for AvatarContext")

	jsonData, err := json.Marshal(queryData)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal GraphQL query: %w", err)
	}

	url := strings.TrimRight(weaviateURL, "/") + "/v1/graphql"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("weaviate request failed: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("weaviate returned status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var graphqlResp struct {
		Data struct {
			Get struct {
				AvatarContext []struct {
					Additional struct {
						ID       string  `json:"id"`
						Distance float64 `json:"distance"`
					} `json:"_additional"`
					Content string `json:"content"`
					Source  string `json:"source"`
				} `json:"AvatarContext"`
			} `json:"Get"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	if err := json.Unmarshal(bodyBytes, &graphqlResp); err != nil {
		return nil, fmt.Errorf("failed to parse GraphQL response: %w", err)
	}

	if len(graphqlResp.Errors) > 0 {
		return nil, fmt.Errorf("weaviate GraphQL error: %s", graphqlResp.Errors[0].Message)
	}

	// Convert results to a more user-friendly format
	var results []map[string]interface{}
	for _, item := range graphqlResp.Data.Get.AvatarContext {
		results = append(results, map[string]interface{}{
			"content":  item.Content,
			"source":   item.Source,
			"id":       item.Additional.ID,
			"distance": item.Additional.Distance,
		})
	}

	log.Printf("✅ [MCP-KNOWLEDGE] Found %d results in AvatarContext using vector search", len(results))

	return map[string]interface{}{
		"results":    results,
		"count":      len(results),
		"query":      query,
		"collection": "AvatarContext",
		"method":     "vector_search",
	}, nil
}

// getOllamaEmbedding gets an embedding vector from Ollama using nomic-embed-text model
func (s *MCPKnowledgeServer) getOllamaEmbedding(ctx context.Context, text string) ([]float32, error) {
	// Get Ollama URL from environment
	ollamaURL := os.Getenv("OLLAMA_BASE_URL")
	if ollamaURL == "" {
		ollamaURL = os.Getenv("OPENAI_BASE_URL") // Fallback
	}
	if ollamaURL == "" {
		ollamaURL = "http://ollama.agi.svc.cluster.local:11434"
	}

	// Prepare request for Ollama embeddings API
	reqBody := map[string]interface{}{
		"model":  "nomic-embed-text:latest",
		"prompt": text,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	url := strings.TrimRight(ollamaURL, "/") + "/api/embeddings"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama returned status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var ollamaResp struct {
		Embedding []float64 `json:"embedding"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&ollamaResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if len(ollamaResp.Embedding) == 0 {
		return nil, fmt.Errorf("received empty embedding from Ollama")
	}

	// Convert float64 to float32
	embedding := make([]float32, len(ollamaResp.Embedding))
	for i, v := range ollamaResp.Embedding {
		embedding[i] = float32(v)
	}

	log.Printf("✅ [MCP-KNOWLEDGE] Got embedding vector of size %d from Ollama", len(embedding))
	return embedding, nil
}

// searchAvatarContextKeyword is a fallback keyword-based search for AvatarContext
func (s *MCPKnowledgeServer) searchAvatarContextKeyword(ctx context.Context, query string, limit int) (interface{}, error) {
	log.Printf("🔍 [MCP-KNOWLEDGE] Using keyword-based search for AvatarContext")

	weaviateURL := os.Getenv("WEAVIATE_URL")
	if weaviateURL == "" {
		weaviateURL = "http://localhost:8080"
	}

	// Build GraphQL query using Like operator for keyword matching
	queryStr := fmt.Sprintf(`{
		Get {
			AvatarContext(where: {
				operator: Or,
				operands: [
					{ path: ["content"], operator: Like, valueString: "*%s*" },
					{ path: ["source"], operator: Like, valueString: "*%s*" }
				]
			}, limit: %d) {
				_additional {
					id
				}
				content
				source
			}
		}
	}`, query, query, limit)

	queryData := map[string]interface{}{
		"query": queryStr,
	}

	jsonData, err := json.Marshal(queryData)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal GraphQL query: %w", err)
	}

	url := strings.TrimRight(weaviateURL, "/") + "/v1/graphql"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("weaviate request failed: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("weaviate returned status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var graphqlResp struct {
		Data struct {
			Get struct {
				AvatarContext []struct {
					Additional struct {
						ID string `json:"id"`
					} `json:"_additional"`
					Content string `json:"content"`
					Source  string `json:"source"`
				} `json:"AvatarContext"`
			} `json:"Get"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	if err := json.Unmarshal(bodyBytes, &graphqlResp); err != nil {
		return nil, fmt.Errorf("failed to parse GraphQL response: %w", err)
	}

	if len(graphqlResp.Errors) > 0 {
		return nil, fmt.Errorf("weaviate GraphQL error: %s", graphqlResp.Errors[0].Message)
	}

	var results []map[string]interface{}
	for _, item := range graphqlResp.Data.Get.AvatarContext {
		results = append(results, map[string]interface{}{
			"content": item.Content,
			"source":  item.Source,
			"id":      item.Additional.ID,
		})
	}

	log.Printf("✅ [MCP-KNOWLEDGE] Found %d results in AvatarContext using keyword search", len(results))

	return map[string]interface{}{
		"results":    results,
		"count":      len(results),
		"query":      query,
		"collection": "AvatarContext",
		"method":     "keyword_search",
	}, nil
}

// searchWeaviateGraphQL performs a direct GraphQL query to Weaviate for non-episodic collections
func (s *MCPKnowledgeServer) searchWeaviateGraphQL(ctx context.Context, query, collection string, limit int, vec []float32) (interface{}, error) {
	// Get Weaviate URL from vectorDB adapter
	// We need to construct the GraphQL query directly
	// For now, we'll need to access the baseURL from the adapter
	// This is a simplified version - in practice you'd want to expose baseURL from the adapter

	// Convert vector to string format for GraphQL
	vectorStr := "["
	for i, v := range vec {
		if i > 0 {
			vectorStr += ","
		}
		vectorStr += fmt.Sprintf("%.6f", v)
	}
	vectorStr += "]"

	// Build GraphQL query based on collection type
	// Note: Weaviate's nearVector doesn't support distance threshold in the query itself
	// We'll filter results after retrieval based on the distance value
	// For cosine distance: lower is better (0 = identical, 1 = completely different)
	// We request more results than needed, then filter by distance threshold
	requestLimit := limit * 2 // Request more to account for filtering

	var queryStr string
	if collection == "AgiWiki" {
		queryStr = fmt.Sprintf(`{
			Get {
				AgiWiki(nearVector: {vector: %s}, limit: %d) {
					_additional {
						id
						distance
					}
					title
					text
					source
					timestamp
					url
					metadata
				}
			}
		}`, vectorStr, requestLimit)
	} else if collection == "WikipediaArticle" {
		// FIXED: Use Like filter for WikipediaArticle to ensure better keyword matching than BM25
		// This handles cases like 'Ukraine' matching 'Ukrainians' and avoids BM25 tokenization issues.
		searchTerm := query
		// Apply the same stemming as in keyword extraction for consistency
		if strings.HasSuffix(strings.ToLower(searchTerm), "e") && len(searchTerm) > 5 {
			searchTerm = searchTerm[:len(searchTerm)-1]
		}

		queryStr = fmt.Sprintf(`{
			Get {
				WikipediaArticle(where: {
					operator: Or,
					operands: [
						{ path: ["title"], operator: Like, valueString: "*%s*" },
						{ path: ["text"], operator: Like, valueString: "*%s*" }
					]
				}, limit: %d) {
					_additional {
						id
						score
					}
					title
					text
					source
					timestamp
					url
					metadata
				}
			}
		}`, searchTerm, searchTerm, requestLimit)
	} else {
		// Generic collection query using vector search fallback
		queryStr = fmt.Sprintf(`{
			Get {
				%s(nearVector: {vector: %s}, limit: %d) {
					_additional {
						id
						distance
					}
					text
					timestamp
					metadata
				}
			}
		}`, collection, vectorStr, requestLimit)
	}

	// We need Weaviate URL - try to get it from environment or use a default
	// For now, return an error indicating we need the URL
	// In practice, you'd want to store the baseURL in MCPKnowledgeServer
	weaviateURL := os.Getenv("WEAVIATE_URL")
	if weaviateURL == "" {
		weaviateURL = "http://localhost:8080"
	}

	queryData := map[string]interface{}{
		"query": queryStr,
	}
	log.Printf("🔍 [MCP-KNOWLEDGE] Sending GraphQL query to Weaviate: %s", queryStr)

	jsonData, err := json.Marshal(queryData)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal GraphQL query: %w", err)
	}

	url := strings.TrimRight(weaviateURL, "/") + "/v1/graphql"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("weaviate request failed: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}
	log.Printf("🔍 [MCP-KNOWLEDGE] Weaviate raw response: %s", string(bodyBytes))

	var result struct {
		Data struct {
			Get map[string][]map[string]interface{} `json:"Get"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	if err := json.Unmarshal(bodyBytes, &result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if len(result.Errors) > 0 {
		return nil, fmt.Errorf("weaviate errors: %v", result.Errors)
	}

	// Extract results from the collection and filter by distance
	var results []map[string]interface{}
	if collectionData, ok := result.Data.Get[collection]; ok {
		// STRICT BUT LENIENT filtering: keyword matching is primary, distance is secondary
		// We still prefer high-precision matches but avoid filtering everything out.
		// Distance threshold is more relaxed (0.6) to account for hash-based embeddings.
		maxDistance := 0.60

		// Extract keywords from query for MANDATORY filtering
		// First, try to extract the actual search term from tool call instructions
		// The LLM might pass something like "Search... about 'bondi'. Use mcp_search_weaviate with query='bondi'"
		// We want to extract just "bondi"
		actualQuery := query

		// Try multiple patterns to extract the actual search term
		// Pattern 1: query='...' or query="..."
		if idx := strings.Index(query, "query='"); idx >= 0 {
			start := idx + 7
			end := strings.Index(query[start:], "'")
			if end > 0 {
				actualQuery = query[start : start+end]
				log.Printf("🔍 [MCP-KNOWLEDGE] Extracted query from 'query=' pattern: '%s' (original: '%s')", actualQuery, query)
			}
		} else if idx := strings.Index(query, "query=\""); idx >= 0 {
			start := idx + 7
			end := strings.Index(query[start:], "\"")
			if end > 0 {
				actualQuery = query[start : start+end]
				log.Printf("🔍 [MCP-KNOWLEDGE] Extracted query from 'query=\"' pattern: '%s' (original: '%s')", actualQuery, query)
			}
		} else if idx := strings.Index(query, "about '"); idx >= 0 {
			// Pattern: "about 'bondi'"
			start := idx + 7
			end := strings.Index(query[start:], "'")
			if end > 0 {
				actualQuery = query[start : start+end]
				log.Printf("🔍 [MCP-KNOWLEDGE] Extracted query from 'about' pattern: '%s' (original: '%s')", actualQuery, query)
			}
		} else if idx := strings.Index(query, "about \""); idx >= 0 {
			// Pattern: "about \"bondi\""
			start := idx + 7
			end := strings.Index(query[start:], "\"")
			if end > 0 {
				actualQuery = query[start : start+end]
				log.Printf("🔍 [MCP-KNOWLEDGE] Extracted query from 'about \"' pattern: '%s' (original: '%s')", actualQuery, query)
			}
		} else {
			// If no pattern match, try to extract the last meaningful word/phrase
			// This handles cases where LLM passes "tell me about" instead of "bondi"
			words := strings.Fields(strings.ToLower(query))
			skipWords := map[string]bool{
				"the": true, "a": true, "an": true, "and": true, "or": true, "but": true,
				"in": true, "on": true, "at": true, "to": true, "for": true, "of": true,
				"with": true, "by": true, "is": true, "are": true, "was": true, "were": true,
				"who": true, "what": true, "where": true, "when": true, "why": true, "how": true,
				"tell": true, "me": true, "about": true, "search": true, "find": true,
				"use": true, "mcp_search_weaviate": true, "tool": true,
				"articles": true, "episodic": true, "memory": true, "news": true,
			}
			meaningfulWords := make([]string, 0)
			for _, word := range words {
				word = strings.Trim(word, ".,!?;:()[]{}'\"")
				// Be less aggressive with filtering for short meaningful words
				// Allow words like "me", "i", "who" if they are the only words
				if !skipWords[word] || (len(words) <= 3 && (word == "who" || word == "me" || word == "i")) {
					if len(word) >= 1 {
						meaningfulWords = append(meaningfulWords, word)
					}
				}
			}
			if len(meaningfulWords) > 0 {
				// Use the last 1-2 meaningful words - usually the actual search term
				// For "tell me about bondi", this would extract "bondi"
				// For "search for lindsay foreman", this would extract "lindsay foreman"
				startIdx := len(meaningfulWords) - 2
				if startIdx < 0 {
					startIdx = 0
				}
				actualQuery = strings.Join(meaningfulWords[startIdx:], " ")
				log.Printf("🔍 [MCP-KNOWLEDGE] Extracted query from meaningful words: '%s' (original: '%s', all meaningful: %v)", actualQuery, query, meaningfulWords)
			} else {
				// If no meaningful words found, the query is invalid - log and use empty
				log.Printf("⚠️ [MCP-KNOWLEDGE] No meaningful words found in query: '%s' - will filter out all results", query)
				actualQuery = "" // This will cause all results to be filtered out
			}
		}

		// If extraction failed completely, we can't search - return empty results
		if actualQuery == "" {
			log.Printf("⚠️ [MCP-KNOWLEDGE] Query extraction failed completely, returning empty results")
			return map[string]interface{}{
				"results":    []map[string]interface{}{},
				"count":      0,
				"query":      query,
				"collection": collection,
			}, nil
		}

		queryLower := strings.ToLower(strings.TrimSpace(actualQuery))
		queryWords := strings.Fields(queryLower)
		// Remove common stop words
		stopWords := map[string]bool{
			"the": true, "a": true, "an": true, "and": true, "or": true, "but": true,
			"in": true, "on": true, "at": true, "to": true, "for": true, "of": true,
			"with": true, "by": true, "is": true, "are": true, "was": true, "were": true,
			"who": true, "what": true, "where": true, "when": true, "why": true, "how": true,
			"tell": true, "me": true, "about": true, "search": true, "find": true,
			"use": true, "mcp_search_weaviate": true, "tool": true,
		}
		keywords := make([]string, 0)
		for _, word := range queryWords {
			word = strings.Trim(word, ".,!?;:()[]{}'\"")
			if !stopWords[word] && len(word) > 2 {
				// Simple stemming: if word ends in 'e' and is long, remove it to match variations
				// e.g. 'ukraine' -> 'ukrain' matches 'ukrainians'
				if strings.HasSuffix(word, "e") && len(word) > 5 {
					word = strings.TrimSuffix(word, "e")
				}
				keywords = append(keywords, word)
			}
		}

		// If no keywords extracted, try to extract from the whole query
		// But be careful - if the whole query is just stop words, we can't search
		if len(keywords) == 0 {
			cleaned := strings.Trim(queryLower, ".,!?;:()[]{}'\"")
			// Only use whole query if it's a single meaningful word (not a phrase of stop words)
			words := strings.Fields(cleaned)
			meaningfulCount := 0
			for _, word := range words {
				if !stopWords[word] && len(word) > 2 {
					meaningfulCount++
				}
			}
			// If there are meaningful words, extract them
			if meaningfulCount > 0 {
				meaningful := make([]string, 0)
				for _, word := range words {
					if !stopWords[word] && len(word) > 2 {
						meaningful = append(meaningful, word)
					}
				}
				if len(meaningful) > 0 {
					keywords = meaningful
				}
			} else if len(cleaned) > 2 && len(words) == 1 {
				// Single word that's not a stop word - use it
				keywords = []string{cleaned}
			}
			// If still no keywords, we'll filter everything out (which is correct)
		}

		log.Printf("🔍 [MCP-KNOWLEDGE] Extracted keywords: %v from query: '%s'", keywords, actualQuery)

		log.Printf("🔍 [MCP-KNOWLEDGE] Filtering with distance <= %.2f and keywords: %v", maxDistance, keywords)

		for _, item := range collectionData {
			distance := 0.0
			// Step 1: Check distance threshold (MANDATORY for vector search, skip for BM25)
			// BM25 (WikipediaArticle) uses 'score' not 'distance', so skip this check
			if collection != "WikipediaArticle" {
				hasDistance := false
				if additional, ok := item["_additional"].(map[string]interface{}); ok {
					if d, ok := additional["distance"].(float64); ok {
						distance = d
						hasDistance = true
					}
				}

				if !hasDistance {
					log.Printf("🔍 [MCP-KNOWLEDGE] Filtered out result (no distance): %v", item["title"])
					continue
				}

				if distance > maxDistance {
					log.Printf("🔍 [MCP-KNOWLEDGE] Filtered out result (distance %.3f > %.2f): %v", distance, maxDistance, item["title"])
					continue
				}
			}

			// Step 2: Keyword matching (PRIMARY)
			// Since hash-based embeddings are unreliable, keyword matching remains primary,
			// but we relax some of the earlier ultra-strict constraints so that relevant
			// Wikipedia hits are not completely filtered out.
			if len(keywords) == 0 {
				log.Printf("🔍 [MCP-KNOWLEDGE] Filtered out result (no keywords to match): %v", item["title"])
				continue
			}

			// Get title, text, or content for matching
			title, _ := item["title"].(string)
			text, _ := item["text"].(string)
			content, _ := item["content"].(string)

			// Also check metadata summary if available (common in AgiWiki)
			metadataSummary := ""
			if metadataStr, ok := item["metadata"].(string); ok && metadataStr != "" {
				var meta map[string]interface{}
				if err := json.Unmarshal([]byte(metadataStr), &meta); err == nil {
					if summary, ok := meta["summary"].(string); ok {
						metadataSummary = summary
					}
				}
			}

			if title == "" && text == "" && content == "" && metadataSummary == "" {
				log.Printf("🔍 [MCP-KNOWLEDGE] Filtered out result (no identifiable text fields): %v", item)
				continue
			}

			titleLower := strings.ToLower(title)
			textLower := strings.ToLower(text)
			contentLower := strings.ToLower(content)
			metaLower := strings.ToLower(metadataSummary)

			// Relaxed rule: primary keyword MUST be in one of the text fields
			primaryKeyword := keywords[0]
			matched := strings.Contains(titleLower, primaryKeyword) ||
				strings.Contains(textLower, primaryKeyword) ||
				strings.Contains(contentLower, primaryKeyword) ||
				strings.Contains(metaLower, primaryKeyword)

			if !matched {
				if collection == "WikipediaArticle" {
					log.Printf("🔍 [MCP-KNOWLEDGE] Filtered out result (primary keyword '%s' not in title or text preview): %v", primaryKeyword, title)
				} else {
					log.Printf("🔍 [MCP-KNOWLEDGE] Filtered out result (primary keyword '%s' not in title or text preview, distance=%.3f): %v", primaryKeyword, distance, title)
				}
				continue
			}

			// Passed all filters
			if collection == "WikipediaArticle" {
				log.Printf("✅ [MCP-KNOWLEDGE] Including result (BM25, primary keyword '%s' matched in title/text): %v", primaryKeyword, title)
			} else {
				log.Printf("✅ [MCP-KNOWLEDGE] Including result (distance=%.3f, primary keyword '%s' matched in title/text): %v", distance, primaryKeyword, title)
			}
			results = append(results, item)

			// Limit results to requested limit
			if len(results) >= limit {
				break
			}
		}
	}

	log.Printf("✅ [MCP-KNOWLEDGE] RAG search returned %d results (after distance filtering) for query: %s", len(results), query)

	return map[string]interface{}{
		"results":    results,
		"count":      len(results),
		"query":      query,
		"collection": collection,
	}, nil
}

// getConcept retrieves a concept from Neo4j
func (s *MCPKnowledgeServer) getConcept(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	name, ok := args["name"].(string)
	if !ok || name == "" {
		return nil, fmt.Errorf("name parameter is required")
	}

	domain, _ := args["domain"].(string)

	// Escape single quotes in name and domain to prevent Cypher injection
	escapedName := strings.ReplaceAll(name, "'", "\\'")

	// Build query with embedded parameters (safer than parameterized queries for this endpoint)
	// Build query searching both name and definition
	cypher := fmt.Sprintf("MATCH (c:Concept) WHERE (toLower(c.name) CONTAINS toLower('%s') OR toLower(c.definition) CONTAINS toLower('%s'))", escapedName, escapedName)
	if domain != "" {
		escapedDomain := strings.ReplaceAll(domain, "'", "\\'")
		cypher += fmt.Sprintf(" AND c.domain = '%s'", escapedDomain)
	}
	cypher += " RETURN c LIMIT 5"

	return s.queryViaHDN(ctx, cypher)
}

// findRelatedConcepts finds concepts related to a given concept
func (s *MCPKnowledgeServer) findRelatedConcepts(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	conceptName, ok := args["concept_name"].(string)
	if !ok || conceptName == "" {
		return nil, fmt.Errorf("concept_name parameter is required")
	}

	maxDepth := 2
	if d, ok := args["max_depth"].(float64); ok {
		maxDepth = int(d)
	}

	// Escape single quotes in concept name to prevent Cypher injection
	escapedName := strings.ReplaceAll(conceptName, "'", "\\'")

	// Build query with embedded parameters
	cypher := fmt.Sprintf(`
		MATCH path = (c:Concept {name: '%s'})-[*1..%d]-(related:Concept)
		RETURN DISTINCT related, length(path) as depth
		ORDER BY depth
		LIMIT 20
	`, escapedName, maxDepth)

	return s.queryViaHDN(ctx, cypher)
}

// sendResponse sends an MCP JSON-RPC response
func (s *MCPKnowledgeServer) sendResponse(w http.ResponseWriter, id interface{}, result interface{}) {
	response := MCPKnowledgeResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// sendError sends an MCP JSON-RPC error response
func (s *MCPKnowledgeServer) sendError(w http.ResponseWriter, id interface{}, code int, message string, err error) {
	if err != nil {
		message = fmt.Sprintf("%s: %v", message, err)
	}

	response := MCPKnowledgeResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error: &MCPKnowledgeError{
			Code:    code,
			Message: message,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// RegisterMCPKnowledgeServerRoutes registers MCP knowledge server routes
func (s *APIServer) RegisterMCPKnowledgeServerRoutes() {
	if s.mcpKnowledgeServer == nil {
		hdnURL := os.Getenv("HDN_URL")
		if hdnURL == "" {
			hdnURL = "http://localhost:8081"
		}
		s.mcpKnowledgeServer = NewMCPKnowledgeServer(
			s.domainKnowledge,
			s.vectorDB,
			s.redis,
			hdnURL,
			s.llmClient, // Pass LLM client for prompt-driven browser automation
		)

		// Register prompt hints from configured skills with interpreter
		// This is done after interpreter initialization in SetLLMClient
	}

	// Register MCP endpoint
	s.router.HandleFunc("/mcp", s.mcpKnowledgeServer.HandleRequest).Methods("POST", "GET", "OPTIONS")
	s.router.HandleFunc("/api/v1/mcp", s.mcpKnowledgeServer.HandleRequest).Methods("POST", "GET", "OPTIONS")
	// Register explicit /sse endpoint for clients that expect it
	s.router.HandleFunc("/sse", s.mcpKnowledgeServer.HandleRequest).Methods("GET", "OPTIONS")
	s.router.HandleFunc("/api/v1/sse", s.mcpKnowledgeServer.HandleRequest).Methods("GET", "OPTIONS")

	log.Printf("✅ [MCP-KNOWLEDGE] MCP knowledge server registered at /mcp, /sse and /api/v1/mcp")
}

// isSelfConnectionHDN checks if the endpoint is pointing to the same server (self-connection)
// This detects Kubernetes service DNS patterns and localhost patterns
func isSelfConnectionHDN(endpoint string) bool {
	lower := strings.ToLower(endpoint)

	// Check for Kubernetes service DNS patterns (e.g., hdn-server-*.svc.cluster.local)
	if strings.Contains(lower, ".svc.cluster.local") {
		// Extract service name and check if it matches HDN service pattern
		if strings.Contains(lower, "hdn") || strings.Contains(lower, "hdn-server") {
			return true
		}
	}

	// Check if it's already localhost
	if strings.Contains(lower, "localhost") || strings.Contains(lower, "127.0.0.1") {
		return true
	}

	return false
}

// saveAvatarContext saves a personal fact to the AvatarContext collection
func (s *MCPKnowledgeServer) saveAvatarContext(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	content, ok := args["content"].(string)
	if !ok || content == "" {
		return nil, fmt.Errorf("content parameter is required")
	}

	source := "user_chat"
	if s, ok := args["source"].(string); ok && s != "" {
		source = s
	}

	log.Printf("📥 [MCP-KNOWLEDGE] saveAvatarContext called with content length: %d", len(content))

	// Get embedding for the content
	embedding, err := s.getOllamaEmbedding(ctx, content)
	if err != nil {
		return nil, fmt.Errorf("failed to get embedding for storage: %w", err)
	}

	// Get Weaviate URL
	weaviateURL := os.Getenv("WEAVIATE_URL")
	if weaviateURL == "" {
		weaviateURL = "http://localhost:8080"
	}

	// Prepare the object to store
	properties := map[string]interface{}{
		"content":   content,
		"source":    source,
		"timestamp": time.Now().Format(time.RFC3339),
	}

	createData := map[string]interface{}{
		"class":      "AvatarContext",
		"properties": properties,
		"vector":     embedding,
	}

	jsonData, err := json.Marshal(createData)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal object for storage: %w", err)
	}

	url := strings.TrimRight(weaviateURL, "/") + "/v1/objects"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send storage request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("storage request failed with status %d: %s", resp.StatusCode, string(body))
	}

	log.Printf("✅ [MCP-KNOWLEDGE] Successfully saved personal fact to AvatarContext")
	return map[string]interface{}{
		"success": true,
		"message": "Information saved to personal context",
	}, nil
}

// saveEpisode saves a conversation summary or significant event to the AgiEpisodes collection
func (s *MCPKnowledgeServer) saveEpisode(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	text, ok := args["text"].(string)
	if !ok || text == "" {
		return nil, fmt.Errorf("text parameter is required")
	}

	metadataMap, _ := args["metadata"].(map[string]interface{})
	metadataStr := ""
	if metadataMap != nil {
		if b, err := json.Marshal(metadataMap); err == nil {
			metadataStr = string(b)
		}
	}

	log.Printf("📥 [MCP-KNOWLEDGE] saveEpisode called with text length: %d", len(text))

	// Get embedding for the text
	embedding, err := s.getOllamaEmbedding(ctx, text)
	if err != nil {
		return nil, fmt.Errorf("failed to get embedding for storage: %w", err)
	}

	// Get Weaviate URL
	weaviateURL := os.Getenv("WEAVIATE_URL")
	if weaviateURL == "" {
		weaviateURL = "http://localhost:8080"
	}

	// Prepare the object to store
	properties := map[string]interface{}{
		"text":      text,
		"metadata":  metadataStr,
		"timestamp": time.Now().Format(time.RFC3339),
	}

	createData := map[string]interface{}{
		"class":      "AgiEpisodes",
		"properties": properties,
		"vector":     embedding,
	}

	jsonData, err := json.Marshal(createData)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal object for storage: %w", err)
	}

	url := strings.TrimRight(weaviateURL, "/") + "/v1/objects"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send storage request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("storage request failed with status %d: %s", resp.StatusCode, string(body))
	}

	log.Printf("✅ [MCP-KNOWLEDGE] Successfully saved episode to AgiEpisodes")
	return map[string]interface{}{
		"success": true,
		"message": "Episode saved to knowledge base",
	}, nil
}

// readGoogleWorkspace calls n8n webhook to fetch email/calendar data
func (s *MCPKnowledgeServer) readGoogleWorkspace(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	query, _ := args["query"].(string)
	dataType, _ := args["type"].(string)

	// Get limit parameter (default to 5, max 50 to prevent timeouts)
	limit := 5
	if l, ok := args["limit"].(float64); ok {
		limit = int(l)
		if limit <= 0 {
			limit = 5
		}
		if limit > 50 {
			limit = 50 // Cap at 50 to prevent timeouts
			log.Printf("⚠️ [MCP-KNOWLEDGE] Limit capped at 50 to prevent timeouts")
		}
	}

	log.Printf("📥 [MCP-KNOWLEDGE] readGoogleWorkspace called with query: '%s', type: '%s', limit: %d", query, dataType, limit)

	// Construct request payload
	payload := map[string]interface{}{
		"query": query,
		"type":  dataType,
		"limit": limit,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// n8n service URL in cluster (configurable)
	n8nURL := os.Getenv("N8N_WEBHOOK_URL")
	if n8nURL == "" {
		n8nURL = "http://n8n.n8n.svc.cluster.local:5678/webhook/google-workspace"
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, "POST", n8nURL, bytes.NewReader(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// Add authentication header if secret is configured
	if secret := os.Getenv("N8N_WEBHOOK_SECRET"); secret != "" {
		req.Header.Set("X-Webhook-Secret", secret)
	}

	// Execute with increased timeout for n8n webhooks (can take 10-30 seconds)
	// Skip TLS verification for self-signed certificates (same as test program)
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{
		Timeout:   60 * time.Second,
		Transport: tr,
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call n8n webhook: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.Printf("❌ [MCP-KNOWLEDGE] n8n returned error status %d. Response body: %s", resp.StatusCode, string(bodyBytes))
		return nil, fmt.Errorf("n8n returned status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	// Log raw response for debugging
	log.Printf("📥 [MCP-KNOWLEDGE] n8n response status: %d, body length: %d bytes", resp.StatusCode, len(bodyBytes))
	if len(bodyBytes) == 0 {
		log.Printf("⚠️ [MCP-KNOWLEDGE] n8n returned EMPTY response body!")
		return map[string]interface{}{
			"results": []interface{}{},
			"message": "n8n returned empty response",
		}, nil
	}

	// Log first 500 chars of response for debugging
	preview := string(bodyBytes)
	if len(preview) > 500 {
		preview = preview[:500] + "..."
	}
	log.Printf("📥 [MCP-KNOWLEDGE] n8n response preview: %s", preview)

	// Parse response
	var result interface{}
	if err := json.Unmarshal(bodyBytes, &result); err != nil {
		// If not JSON, return string
		log.Printf("⚠️ [MCP-KNOWLEDGE] n8n response is not JSON, returning as string. Length: %d, Error: %v", len(bodyBytes), err)
		return map[string]interface{}{
			"result": string(bodyBytes),
		}, nil
	}

	// Log the structure of the result for debugging
	resultType := "unknown"
	resultLen := 0
	var finalResult interface{} = result

	if resultArray, ok := result.([]interface{}); ok {
		resultType = "array"
		resultLen = len(resultArray)
		log.Printf("📧 [MCP-KNOWLEDGE] n8n returned array with %d items", resultLen)

		// n8n "allIncomingItems" mode returns array of objects with "json" key containing the actual data
		// Extract the actual email data from the n8n structure
		if resultLen > 0 {
			// Check if first item has "json" key (n8n structure)
			if firstItem, ok := resultArray[0].(map[string]interface{}); ok {
				var keys []string
				for k := range firstItem {
					keys = append(keys, k)
				}
				log.Printf("📧 [MCP-KNOWLEDGE] First item has keys: %v", keys)

				// If it has "json" key, extract the actual data
				if _, hasJson := firstItem["json"]; hasJson {
					log.Printf("📧 [MCP-KNOWLEDGE] Extracting data from 'json' key (n8n allIncomingItems format)")
					// Extract all items' json data
					extractedItems := make([]interface{}, 0, resultLen)
					for _, item := range resultArray {
						if itemMap, ok := item.(map[string]interface{}); ok {
							if jsonVal, ok := itemMap["json"]; ok {
								extractedItems = append(extractedItems, jsonVal)
							} else {
								// If no json key, use the item itself
								extractedItems = append(extractedItems, item)
							}
						} else {
							extractedItems = append(extractedItems, item)
						}
					}
					finalResult = extractedItems
					resultLen = len(extractedItems)
					log.Printf("📧 [MCP-KNOWLEDGE] Extracted %d items from n8n json structure", resultLen)

					// Log first extracted item structure
					if resultLen > 0 {
						if firstExtracted, ok := extractedItems[0].(map[string]interface{}); ok {
							var extractedKeys []string
							for k := range firstExtracted {
								extractedKeys = append(extractedKeys, k)
							}
							log.Printf("📧 [MCP-KNOWLEDGE] First extracted email item has keys: %v", extractedKeys)
						}
					}
				} else {
					// Check if first item is already an email object (has Subject/subject, From/from) - case insensitive
					hasSubject := false
					hasFrom := false
					for k := range firstItem {
						kLower := strings.ToLower(k)
						if kLower == "subject" {
							hasSubject = true
						}
						if kLower == "from" {
							hasFrom = true
						}
					}
					if hasSubject || hasFrom {
						log.Printf("📧 [MCP-KNOWLEDGE] Items are already email objects (no json wrapper, hasSubject=%v, hasFrom=%v)", hasSubject, hasFrom)
						finalResult = resultArray
					}
				}
			}
		}
	} else if resultMap, ok := result.(map[string]interface{}); ok {
		resultType = "map"
		var keys []string
		for k := range resultMap {
			keys = append(keys, k)
		}
		log.Printf("📧 [MCP-KNOWLEDGE] n8n returned map with keys: %v", keys)

		// Check if this is a single email object (has Subject/subject, From/from, To/to) - case insensitive
		hasSubject := false
		hasFrom := false
		for k := range resultMap {
			kLower := strings.ToLower(k)
			if kLower == "subject" {
				hasSubject = true
			}
			if kLower == "from" {
				hasFrom = true
			}
		}

		if hasSubject || hasFrom {
			log.Printf("📧 [MCP-KNOWLEDGE] Single email object detected (hasSubject=%v, hasFrom=%v), wrapping in array", hasSubject, hasFrom)
			// Wrap single email in array for consistent handling
			finalResult = []interface{}{resultMap}
			resultLen = 1
			resultType = "array (wrapped)"
		} else if emailsData, hasEmails := resultMap["emails"]; hasEmails {
			// New format from "Format as Array" node: { emails: [...] }
			log.Printf("📧 [MCP-KNOWLEDGE] Extracting data from 'emails' key in map")
			if emailsArray, ok := emailsData.([]interface{}); ok {
				finalResult = emailsArray
				resultLen = len(emailsArray)
				resultType = "array (from emails key)"
				log.Printf("📧 [MCP-KNOWLEDGE] Extracted %d emails from 'emails' key", resultLen)
			} else {
				log.Printf("⚠️ [MCP-KNOWLEDGE] 'emails' key is not an array, type: %T", emailsData)
				finalResult = []interface{}{emailsData}
				resultLen = 1
			}
		} else if jsonData, hasJson := resultMap["json"]; hasJson {
			// If it has "json" key, extract it
			log.Printf("📧 [MCP-KNOWLEDGE] Extracting data from 'json' key in map")
			if jsonArray, ok := jsonData.([]interface{}); ok {
				finalResult = jsonArray
				resultLen = len(jsonArray)
				resultType = "array (from json key)"
			} else if jsonMap, ok := jsonData.(map[string]interface{}); ok {
				// Single email in json key - check case-insensitively
				hasSubject := false
				hasFrom := false
				for k := range jsonMap {
					kLower := strings.ToLower(k)
					if kLower == "subject" {
						hasSubject = true
					}
					if kLower == "from" {
						hasFrom = true
					}
				}
				if hasSubject || hasFrom {
					finalResult = []interface{}{jsonMap}
					resultLen = 1
					resultType = "array (wrapped from json)"
				} else {
					finalResult = []interface{}{jsonMap}
					resultLen = 1
				}
			} else {
				finalResult = []interface{}{jsonData}
				resultLen = 1
			}
		}
	} else {
		log.Printf("⚠️ [MCP-KNOWLEDGE] n8n returned unexpected type: %T", result)
	}

	log.Printf("✅ [MCP-KNOWLEDGE] Successfully retrieved Google Workspace data (type: %s, size: %d)", resultType, resultLen)

	// Log the actual count if it's an array
	if resultArray, ok := finalResult.([]interface{}); ok {
		log.Printf("📧 [MCP-KNOWLEDGE] Returning %d email(s) to caller", len(resultArray))
	}

	return finalResult, nil
}

// executeSmartScrape performs an AI-powered scrape by first fetching and then planning
func (s *MCPKnowledgeServer) executeSmartScrape(ctx context.Context, url string, goal string, userConfig *ScrapeConfig) (interface{}, error) {
	log.Printf("🧠 [MCP-SMART-SCRAPE] Starting smart scrape for %s with goal: %s", url, goal)

	var err error
	// 0. Fast-path check: if the user provided an explicit script, we skip the initial fetch and LLM planning.
	var config *ScrapeConfig
	bypassedLLM := false
	if userConfig != nil && userConfig.TypeScriptConfig != "" {
		log.Printf("⚡ [MCP-SMART-SCRAPE] Fast-path: User provided explicit TypeScript script, bypassing initial fetch and LLM planning")
		bypassedLLM = true
		config = &ScrapeConfig{
			TypeScriptConfig: userConfig.TypeScriptConfig,
			Extractions:      make(map[string]string),
		}
		if userConfig.Extractions != nil {
			for k, v := range userConfig.Extractions {
				config.Extractions[k] = v
			}
		}
	}

	var innerResult map[string]interface{}
	var cleanedHTML string
	var capturedCookies []interface{}

	if !bypassedLLM {
		// 1. Fetch page HTML using the scraper service (in get_html mode)
		log.Printf("📥 [MCP-SMART-SCRAPE] Fetching page content to plan scrape...")
		htmlResultRaw, err := s.scrapeWithConfig(ctx, url, "", false, nil, true, nil) // Pass true for getHTML
		if err != nil {
			return nil, fmt.Errorf("failed to fetch page content: %v", err)
		}

		htmlResult, ok := htmlResultRaw.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("unexpected internal result format")
		}

		// The scraper returns results inside a "result" key for polling
		innerResult, ok = htmlResult["result"].(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("could not find result in scrape response")
		}

		cleanedHTML, ok = innerResult["cleaned_html"].(string)
		if !ok || cleanedHTML == "" {
			return nil, fmt.Errorf("scraper did not return cleaned_html")
		}

		// 1.5. Check if this is a consent/cookie page and handle it
		if isConsentPage(cleanedHTML) {
			log.Printf("🍪 [MCP-SMART-SCRAPE] Detected consent/cookie page, attempting to bypass...")
			consentTS := generateConsentBypassScript()
			bypassResult, err := s.scrapeWithConfig(ctx, url, consentTS, false, nil, true, nil)
			if err != nil {
				log.Printf("⚠️ [MCP-SMART-SCRAPE] Consent bypass failed: %v", err)
			} else {
				if bypassMap, ok := bypassResult.(map[string]interface{}); ok {
					if bypassInner, ok := bypassMap["result"].(map[string]interface{}); ok {
						if newHTML, ok := bypassInner["cleaned_html"].(string); ok && newHTML != "" {
							log.Printf("✅ [MCP-SMART-SCRAPE] Successfully bypassed consent page, got %d chars of new HTML", len(newHTML))
							cleanedHTML = newHTML
						}
						if cookies, ok := bypassInner["cookies"].([]interface{}); ok {
							capturedCookies = cookies
						}
					}
				}
			}
		}
	}

	// 2. Plan the scrape using LLM (if not explicitly provided by user)
	planCtx, planCancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer planCancel()

	if !bypassedLLM {
		log.Printf("📋 [MCP-SMART-SCRAPE] Planning scrape config with LLM (%d chars of HTML)...", len(cleanedHTML))

		// Build a compact actionable snapshot (only forms/inputs/buttons)
		actionableHTML := s.buildActionableSnapshot(cleanedHTML)
		navGoal := "[NAVIGATION_ONLY]\n" + goal
		config, err = s.planScrapeWithLLM(planCtx, actionableHTML, navGoal, userConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to plan scrape with LLM: %v", err)
		}
		config.TypeScriptConfig = s.sanitizeNavigationScript(config.TypeScriptConfig, actionableHTML, goal)

		// Merge user results/hints if provided
		if userConfig != nil {
			if userConfig.TypeScriptConfig != "" && config.TypeScriptConfig == "" {
				config.TypeScriptConfig = userConfig.TypeScriptConfig
			}
			if len(userConfig.Extractions) > 0 {
				if config.Extractions == nil {
					config.Extractions = make(map[string]string)
				}
				for k, v := range userConfig.Extractions {
					if _, exists := config.Extractions[k]; !exists {
						config.Extractions[k] = v
					}
				}
			}
		}
	}

	// 2.5 ALWAYS sanitize ALL extractions here (both LLM-generated and user-provided)
	// This ensures we fix lookarounds and missing groups before fast-path or navigation.
	for k, v := range config.Extractions {
		// 1. Fix lookarounds and named groups (Go doesn't like (?<name>...))
		sanitized := strings.ReplaceAll(v, "(?<=", "(?:")
		sanitized = strings.ReplaceAll(sanitized, "(?=", "(?:")
		reNamedGroup := regexp.MustCompile(`\(\?<[^>]+>`)
		sanitized = reNamedGroup.ReplaceAllString(sanitized, "(")

		// 2. Wrap in capturing group if AI or user forgot a CAPTURING group
		hasCapture := false
		for i := 0; i < len(sanitized); i++ {
			if sanitized[i] == '(' {
				// check if escaped
				if i > 0 && sanitized[i-1] == '\\' {
					continue
				}
				if i+1 < len(sanitized) && sanitized[i+1] != '?' {
					hasCapture = true
					break
				}
			}
		}

		if !hasCapture {
			sanitized = "(" + sanitized + ")"
		}

		config.Extractions[k] = sanitized
	}

	// 2.5. Self-Correction: If the goal requires calculation/search but LLM provided no JS, retry once with a more direct warning
	isInteractiveGoal := strings.Contains(strings.ToLower(goal), "calculate") || strings.Contains(strings.ToLower(goal), "search") || strings.Contains(strings.ToLower(goal), "fill")
	if !bypassedLLM && isInteractiveGoal && (config.TypeScriptConfig == "" || strings.TrimSpace(config.TypeScriptConfig) == "// no navigation needed") {
		log.Printf("⚠️ [MCP-SMART-SCRAPE] LLM provided no navigation for an interactive goal. Retrying with explicit warning...")

		retryUserConfig := ScrapeConfig{Extractions: make(map[string]string)}
		if userConfig != nil {
			retryUserConfig = *userConfig
		}
		// We'll use a hack - put the warning in the TypeScript config hint if it's empty
		retryUserConfig.TypeScriptConfig = "IMPORTANT: THE LAST PLAN FAILED BECAUSE IT HAD NO JS COMMANDS. YOU MUST PROVIDE await page.fill() AND await page.click() COMMANDS TO REACH THE RESULT."

		config, err = s.planScrapeWithLLM(planCtx, cleanedHTML, goal, &retryUserConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to plan scrape with LLM (retry): %v", err)
		}
	}

	// 3. Fast Path: If no extra navigation is required, extract from the HTML we already have
	if !bypassedLLM && (config.TypeScriptConfig == "" || strings.TrimSpace(config.TypeScriptConfig) == "// no navigation needed") {
		log.Printf("⚡ [MCP-SMART-SCRAPE] Fast-path: Extracting from existing HTML (no extra navigation needed)")

		// Prepare a result object similar to what scrapeWithConfig returns
		results := make(map[string]interface{})
		// Use the page title from the first scrape if available
		if title, ok := innerResult["page_title"].(string); ok {
			results["page_title"] = title
		}
		results["page_url"] = url
		results["cleaned_html"] = cleanedHTML
		if cookies, ok := innerResult["cookies"]; ok {
			results["cookies"] = cookies
		}

		// Apply extraction patterns to cleanedHTML
		foundAny := false
		for key, pattern := range config.Extractions {
			re, err := regexp.Compile(pattern)
			if err != nil {
				log.Printf("⚠️  [MCP-SMART-SCRAPE] Invalid regex for '%s': %v", key, err)
				continue
			}
			matches := re.FindAllStringSubmatch(cleanedHTML, -1)
			if len(matches) > 0 {
				if len(matches[0]) > 1 {
					var extracted []string
					for _, m := range matches {
						if len(m) > 1 && m[1] != "" {
							extracted = append(extracted, m[1])
						}
					}
					if len(extracted) > 0 {
						results[key] = strings.Join(extracted, "\n")
						foundAny = true
						log.Printf("✅ [MCP-SMART-SCRAPE] Extracted '%s' via fast-path", key)
					}
				}
			}
		}
		if foundAny {
			resultJSON, _ := json.MarshalIndent(results, "", "  ")
			return map[string]interface{}{
				"content": []map[string]interface{}{
					{
						"type": "text",
						"text": fmt.Sprintf("Scrape Results:\n%s", string(resultJSON)),
					},
				},
				"result": results,
			}, nil
		}
		log.Printf("⚠️ [MCP-SMART-SCRAPE] Fast-path failed to extract any data, falling back to full scrape")
	}

	// 3. Execute the planned scrape
	log.Printf("🚀 [MCP-SMART-SCRAPE] Executing planned scrape (Navigation: %v)", config.TypeScriptConfig != "")

	// Initial run: Execute navigation and/or extractions
	requestHTML := true // Always request HTML so we can do second-pass if needed
	scrapeResultRaw, err := s.scrapeWithConfig(ctx, url, config.TypeScriptConfig, false, config.Extractions, requestHTML, capturedCookies)
	if err != nil {
		return nil, err
	}

	scrapeResult, ok := scrapeResultRaw.(map[string]interface{})
	if !ok {
		return scrapeResultRaw, nil
	}

	finalInnerResult, ok := scrapeResult["result"].(map[string]interface{})
	if !ok {
		return scrapeResult, nil
	}

	// 4. Two-Step Scrape: If navigation was performed, the extraction patterns might have been guesses.
	// Check if we need a second pass to specialize extractions for the final page.
	isPostNavigation := config.TypeScriptConfig != ""

	// Check if any extractions were requested but NOT found
	// Second-pass should ONLY trigger when extractions were attempted but failed, not when missing entirely
	missingExtractions := false
	if len(config.Extractions) > 0 {
		for key := range config.Extractions {
			if _, exists := finalInnerResult[key]; !exists {
				missingExtractions = true
				break
			}
		}
	}
	// Note: If LLM doesn't provide extractions with navigation, that's a prompt issue, not a second-pass trigger

	if isPostNavigation && missingExtractions {
		finalHTML, _ := finalInnerResult["cleaned_html"].(string)
		if finalHTML != "" {
			log.Printf("🔍 [MCP-SMART-SCRAPE] Navigation succeeded but extractions failed. Performing second-pass planning on final page...")

			extractionHTML := s.buildExtractionSnapshot(finalHTML, goal)
			log.Printf("🧩 [MCP-SMART-SCRAPE] Extraction snapshot size: %d chars", len(extractionHTML))

			// Plan specialized extractions for the final page HTML
			// Use a very specific goal for the second pass to avoid re-navigation loops
			secondPassGoal := fmt.Sprintf("RECOVERY MODE: Navigation is already complete. The data you need should be in the HTML snapshot below. DO NOT provide any navigation JS. Just find the correct regex for: %s", goal)

			secondPassConfig, err := s.planScrapeWithLLM(planCtx, extractionHTML, secondPassGoal, &ScrapeConfig{
				TypeScriptConfig: "// NAVIGATION ALREADY COMPLETED - DO NOT ADD COMMANDS HERE",
				Extractions:      config.Extractions, // Pass existing as hints
			})

			if err == nil && len(secondPassConfig.Extractions) > 0 {
				log.Printf("🎯 [MCP-SMART-SCRAPE] Second-pass planned %d specialized extraction patterns", len(secondPassConfig.Extractions))

				// Apply new patterns to finalHTML
				secondResults := s.applyExtractions(finalHTML, secondPassConfig.Extractions)

				// Merge results into finalInnerResult
				foundNew := false
				for k, v := range secondResults {
					if _, alreadyFound := finalInnerResult[k]; !alreadyFound {
						finalInnerResult[k] = v
						foundNew = true
						log.Printf("✅ [MCP-SMART-SCRAPE] Second-pass successfully extracted '%s'", k)
					}
				}

				if foundNew {
					// Update the display text in scrapeResult
					resultJSON, _ := json.MarshalIndent(finalInnerResult, "", "  ")
					if content, ok := scrapeResult["content"].([]map[string]interface{}); ok && len(content) > 0 {
						content[0]["text"] = fmt.Sprintf("Scrape Results (Two-Step):\n%s", string(resultJSON))
					}
				}
			}
		}
	}

	return scrapeResult, nil
}

// applyExtractions applies regex patterns to HTML locally
func (s *MCPKnowledgeServer) applyExtractions(html string, patterns map[string]string) map[string]string {
	results := make(map[string]string)
	for key, pattern := range patterns {
		// Fix common regex issues
		sanitized := strings.ReplaceAll(pattern, "(?<=", "(?:")
		sanitized = strings.ReplaceAll(sanitized, "(?=", "(?:")

		re, err := regexp.Compile(sanitized)
		if err != nil {
			log.Printf("⚠️ [MCP-SMART-SCRAPE] Invalid regex for '%s': %v", key, err)
			continue
		}

		matches := re.FindAllStringSubmatch(html, -1)
		if len(matches) > 0 {
			var extracted []string
			for _, m := range matches {
				if len(m) > 1 && m[1] != "" {
					extracted = append(extracted, m[1])
				}
			}
			if len(extracted) > 0 {
				results[key] = strings.Join(extracted, "\n")
			}
		}
	}
	return results
}

type ScrapeConfig struct {
	TypeScriptConfig string            `json:"typescript_config"`
	Extractions      map[string]string `json:"extractions"`
}

func (s *MCPKnowledgeServer) planScrapeWithLLM(ctx context.Context, html string, goal string, hint *ScrapeConfig) (*ScrapeConfig, error) {
	if s.llmClient == nil {
		return nil, fmt.Errorf("LLM client not configured on knowledge server")
	}

	// Clean HTML for LLM to avoid distraction and stay within context limits
	originalLen := len(html)
	html = cleanHTMLForPlanning(html)
	cleanedLen := len(html)
	log.Printf("🧹 [MCP-SMART-SCRAPE] HTML cleaned for planning: %d -> %d chars (reduced by %.1f%%)", originalLen, cleanedLen, float64(originalLen-cleanedLen)/float64(originalLen)*100)

	// Normalize HTML for LLM - remove destructive replacement that breaks regex matching
	// Since we have a 32k token window (~131k chars), 120k is a safe limit to avoid losing the system prompt
	maxHTML := 120000
	if strings.HasPrefix(goal, "[NAVIGATION_ONLY]") {
		maxHTML = 20000
	}
	if len(html) > maxHTML {
		html = html[:maxHTML] + "...(truncated)"
		log.Printf("⚠️ [MCP-SMART-SCRAPE] HTML still exceeding %d limit, truncated", maxHTML)
	}
	// Detect navigation-only mode: caller may prefix the goal with [NAVIGATION_ONLY]
	// to instruct the LLM to only generate Playwright JS for navigation and
	// NOT generate extraction regexes (those will be planned on the final page).
	navigationOnly := false
	if strings.HasPrefix(goal, "[NAVIGATION_ONLY]") {
		navigationOnly = true
		// strip the marker before including the goal in prompts
		goal = strings.TrimPrefix(goal, "[NAVIGATION_ONLY]")
		goal = strings.TrimSpace(goal)
	}

	// Debug: Show sample of cleaned HTML to help troubleshoot extraction issues
	sampleLen := 2000
	if len(html) < sampleLen {
		sampleLen = len(html)
	}
	log.Printf("🔍 [MCP-SMART-SCRAPE] Sample of cleaned HTML for LLM (first %d chars):\n%s\n...end sample", sampleLen, html[:sampleLen])

	systemPrompt := `You are an expert web scraper configuration generator.
Your task is to analyze an HTML snapshot and generate a scraping plan to achieve a specific GOAL.

⚠️ LANDING PAGE WARNING:
If the HTML SNAPSHOT looks like a FORM (has input fields, selects) and the GOAL is to "Calculate", "Search", or "Find" something specific, you are likely on a LANDING PAGE. 
The data you need is NOT on this page yet. You MUST provide JS commands in "typescript_config" to fill the form and submit it.

Goal: Generate a valid JSON object with:
- "typescript_config": (string) A sequence of Playwright JS commands (e.g., await page.click('...')) to navigate or reveal data if required. MUST BE A STRING, NOT AN OBJECT.
- "extractions": (map<string, string>) A set of named extraction patterns. 
  - Key: The field name (e.g. "price", "title")
  - Value: A single REGEX STRING (e.g. "regex..."). DO NOT USE ARRAYS.

REGEX RULES:
1. ONLY standard Go regex (NO lookarounds like (?=...) or (?<=...)).
2. USE A SINGLE CAPTURING GROUP () only for the data you want to extract. 
   - ❌ BAD: "<span class='(.*?)'>([^<]+)</span>" (Captures class as group 1)
   - ✅ GOOD: "<span[^>]*class='title'[^>]*>\\s*([^<]+)<" (Captures data as group 1)
3. Target the HTML tags you see in the snapshot.
4. Use single quotes (') or [\"'] in your regex for attributes.
5. IMPORTANT: Use [^>]* to skip unknown attributes. e.g. "Table__ProductName[^>]*>\\s*([^<]+)<"
SPECIFICITY RULE:
3. NEVER use generic regex like "(\\d+)" or "([^<]+)" alone.
4. ALWAYS anchor your regex to nearby unique text or labels you see in the snapshot.
   - ❌ BAD: "(\\d+\\.?\\d*)"
   - ✅ GOOD: "CO2\\s*emissions[^>]*>\\s*([^<]+)"
   - ✅ GOOD: "Total[^>]*>\\s*([0-9,.]+)\\s*kg"

EXAMPLES:
- RIGHT: "price:\s*(\d+)"
- WRONG (Lookaround): "(?<=price: )(\d+)"

COMMON PATTERNS:
6. Standard tag with class: "<span[^>]*class='[^']*price[^']*'[^>]*?>\s*([$€£0-9,.]+)"
7. Div with class: "<div[^>]*class='[^']*price[^']*'[^>]*?>\s*([$€£0-9,.]+)"
8. Table cell: "<td[^>]*class='[^']*market-cap[^']*'[^>]*?>\s*([^<]+)"

MODERN WEB PATTERNS (Custom Tags & Data Attributes):
9. Custom tags (e.g. <fin-streamer>, <price-display>): Match ANY tag name you see in HTML
   - Value in content: "<custom-tag[^>]*attribute='value'[^>]*?>\s*([0-9,.]+)"
   - Value in data-value attribute: "<custom-tag[^>]*data-value='([^']+)'"
   - Value in value attribute: "<custom-tag[^>]*value='([^']+)'"
10. Try MULTIPLE patterns for the same field if unsure where value is stored:
    - First try: content between tags
    - Then try: data-value, value, data-field, or other data-* attributes
11. For data attributes, use: "<tag[^>]*data-attribute-name='([^']+)'"
12. Match partial attribute values: "data-field='[^']*price[^']*'"

INTERACTIVE PATTERNS (Autocompletes & Dropdowns):
13. CRITICAL: If you detect Stimulus.js autocomplete fields (data-controller, data-action attributes) OR visible input fields (id, name containing 'from', 'to', 'airport', 'destination', etc.), ALWAYS INCLUDE INITIALIZATION WAIT:
    - Always start with: await page.waitForLoadState('networkidle');  // Wait for JS controllers to initialize
    - For airport/airline codes: Type the CODE (e.g. 'SHA' for Shanghai, not 'Shanghai')
    - Wait for dropdown to appear: await page.waitForTimeout(2000);  // Allow XHR for suggestions
    - Click FIRST suggestion (DO NOT use text() predicates): 
      * await page.locator('ul li, [role="option"], .dropdown li').first().click();
    - CRITICAL: DO NOT use page.waitForNavigation() with autocomplete clicks - dropdowns update in-place without navigating
    - NEVER wrap autocomplete clicks in Promise.all([page.waitForNavigation(), ...]) - this will hang forever
    CRITICAL DO NOT DO: 
    - XPath with text() like xpath=//li[text()="value"] - these fail on Stimulus controllers
    - page.waitForNavigation() for dropdown selections - they don't navigate
    EXAMPLE (Correct airport selection pattern):
    await page.locator('input#flight_calculator_from').fill('CDG');
    await page.waitForTimeout(2000);
    await page.locator('ul li').first().click();
    // Repeat for "To" field with 'LHR'
14. For standard <select> elements:
    - Use .selectOption('internal_value') where 'internal_value' is the value attribute of the <option> tag.
    - NEVER use .fill() on a <select>.
15. For Radio Buttons (<input type='radio'>):
    - Use .check() on the specific radio button ID or label.

NAVIGATION RULES:
16. If the GOAL requires calculating, searching, or filtering data NOT present in the HTML SNAPSHOT (e.g., "Calculate emissions...", "Find price of..."), you MUST provide JS commands in "typescript_config" to reach the result.
17. DO NOT leave "typescript_config" empty for calculation goals. This is a fatal error.
18. ALWAYS wait after submitting a form: If you click a submit button, use **await page.waitForTimeout(3000)** to let results load. DO NOT use page.waitForNavigation() for submit buttons - many forms update in-place with AJAX instead of navigating.
19. CRITICAL EXTRACTION RULE: When you provide navigation JS in "typescript_config", you MUST ALSO provide extraction patterns in the "extractions" field for the RESULT PAGE data. Even if you can't see the result page yet, provide your best guess at extraction patterns based on the goal (e.g., for "CO2 emissions", try patterns like "CO2.*?([0-9,.]+)\\s*(kg|t)", "emissions.*?([0-9,.]+)", etc.). DO NOT leave "extractions" empty when navigating to get data.

- Use double quotes for the JSON wrapper and single quotes for JS strings: "await page.click('selector');"

FATAL FORMAT ERROR WARNING:
- NEVER EVER use nested objects like "calculation_logic" or "interaction_logic": { "step1": ... } inside "typescript_config".
- "typescript_config" MUST be either a SINGLE STRING of JS code OR a FLAT ARRAY of step objects.
- ❌ BAD (Object): "typescript_config": { "calculate": { "click": "..." } }
- ✅ GOOD (String): "typescript_config": "await page.click('...');"
- ✅ GOOD (Array): "typescript_config": { "steps": [ { "action": "click", "selector": "..." } ] }

REGEX FATAL ERROR WARNING:
- NEVER EVER use lookarounds like (?=...) or (?<=...). These will CRASH the Go backend.
- If you need to match after a label, include the label in the regex and use the capturing group for the data.
- ✅ GOOD: "Price:\\\\s*([0-9,.]+)"
- ❌ FATAL ERROR: "(?<=Price:\\\\s*)[0-9,.]+"

THINKING PROCESS:
- STEP 1: Does the goal require user interaction (typing, clicking, selecting)?
- STEP 2: If YES, write the JS sequence in "typescript_config". 
- STEP 3: Identify only the patterns for the FINAL result page after navigation.

CALCULATION RULE:
- If the GOAL is to "calculate", "search", or "find" something that requires filling a form:
- YOU MUST PROVIDE "await page.fill('...', '...');" and "await page.click('...');" commands.
- DO NOT just provide a "commit" click. You must fill the inputs first with the values from the GOAL.
- DO NOT USE PLACEHOLDERS like {{value}}. Use the actual values from the GOAL (e.g. CDG, LHR).

STRATEGY:
- Look for custom HTML tags (anything not standard like div/span/p)
- Check both tag CONTENT and tag ATTRIBUTES for values
- Use flexible but SPECIFIC patterns (e.g., include surrounding static text or classes) to avoid multiple garbage matches.
- If you see data-* attributes, they often contain the actual values
- For forms, check if fields are autocompletes that require clicking a suggestion to 'lock' the value.
- If the goal data isn't in the snapshot, plan the navigation to get there.

Output ONLY the JSON object. Start the response with '{'.`

	if navigationOnly {
		systemPrompt = `You are an expert web scraper configuration generator.

NAVIGATION-ONLY MODE:
- The HTML snapshot contains only actionable elements (forms, inputs, buttons).
- Return a JSON object with "typescript_config" as a string of Playwright commands.
- DO NOT provide extraction regexes; return "extractions": {} or omit it.
- Fill required inputs using values from the GOAL, click submit, and wait for results to load.

Output ONLY valid JSON.`
	}

	// Detect form type to add appropriate hints
	formTypeHint := ""
	htmlLower := strings.ToLower(html)
	if strings.Contains(htmlLower, "from") && strings.Contains(htmlLower, "to") &&
		(strings.Contains(htmlLower, "flight") || strings.Contains(htmlLower, "airport") || strings.Contains(htmlLower, "destination")) {
		formTypeHint = "\n### FORM TYPE DETECTED: Flight Calculator\nIMPORTANT: This form uses Stimulus.js autocomplete controllers. You MUST:\n1. Start with: await page.waitForLoadState('networkidle'); // Essential for JS controller initialization\n2. For airport fields: Use airport CODES (e.g., 'SHA' for Shanghai, not the full name)\n3. Fill input, wait for dropdown: await page.waitForTimeout(2000);\n4. Click the first matching option from dropdown\n"
	}

	userPrompt := fmt.Sprintf(`### GOAL
%s

### HTML SNAPSHOT (Truncated)
%s
%s`, goal, html, formTypeHint)

	log.Printf("🧾 [MCP-SMART-SCRAPE] Prompt sizes: system=%d chars, user=%d chars, total=%d chars", len(systemPrompt), len(userPrompt), len(systemPrompt)+len(userPrompt))

	if hint != nil {
		userPrompt += "\n### USER HINTS (PROBABLE REGEX):\n"
		userPrompt += "The user has provided regex patterns that worked in the past. \n"
		userPrompt += "VALIDATION RULE: If these patterns match the HTML SNAPSHOT below, use them. \n"
		userPrompt += "SELF-HEALING RULE: If a pattern obviously fails to match the current HTML (e.g., attributes changed), you MUST IGNORE the hint and generate a NEW working regex for that key.\n"
		if hint.TypeScriptConfig != "" {
			userPrompt += fmt.Sprintf("- Suggested TypeScript Logic: %s\n", hint.TypeScriptConfig)
		}
		if len(hint.Extractions) > 0 {
			for k, v := range hint.Extractions {
				userPrompt += fmt.Sprintf("- Key '%s' suggested regex: %s\n", k, v)
			}
		}
	}

	userPrompt += `
### TASK
Generate the JSON configuration to extract the data requested in the GOAL.

INSTRUCTIONS:
- Identify the data requested in the GOAL.
- CALCULATION/SEARCH goals REQUIRE interaction logic in "typescript_config".
- STRICT RULE: ONLY use attributes (class, id, role) that you see in the HTML SNAPSHOT.
- NEVER invent attributes like 'data-rate' or 'product-id' if they are not in the snapshot.
- If the page has a <table>, focus your regex on matching <tr> rows within <tbody>.
- Capture ONLY the data you want in the first ().
- CRITICAL: Your response MUST be valid JSON. Double all backslashes: \\s, \\d, \\S.
- Output ONLY valid JSON.`

	// Call LLM
	response, err := s.llmClient.callLLMWithContextAndPriority(ctx, systemPrompt+"\n\n"+userPrompt, PriorityHigh)
	if err != nil {
		return nil, err
	}
	log.Printf("🤖 [MCP-SMART-SCRAPE] Raw LLM planning response: %s", response)

	// Clean/Parse JSON - Enhanced extraction logic
	var config ScrapeConfig
	var parseErr error

	// Try approach 1: Parse into map first for maximum resilience
	var rawMap map[string]interface{}
	cleanedResponse := response
	if first := strings.Index(cleanedResponse, "{"); first != -1 {
		if last := strings.LastIndex(cleanedResponse, "}"); last != -1 && last > first {
			cleanedResponse = cleanedResponse[first : last+1]

			// robust: remove JS comments (like // ...)
			lines := strings.Split(cleanedResponse, "\n")
			for i, line := range lines {
				if idx := strings.Index(line, "//"); idx != -1 {
					// Check if it's a URL (preceded by :)
					isUrl := false
					if idx > 0 && line[idx-1] == ':' {
						isUrl = true
					}
					if !isUrl {
						lines[i] = line[:idx]
					}
				}
			}
			cleanedResponse = strings.Join(lines, "\n")
			cleanedResponse = sanitizeJSONResponse(cleanedResponse)

			if err := json.Unmarshal([]byte(cleanedResponse), &rawMap); err == nil {
				// Successfully parsed into map, now map to struct

				// Handle both "extractions", "extraction_instructions", and "data_extraction"
				var extractions map[string]interface{}
				if ex, ok := rawMap["extractions"].(map[string]interface{}); ok {
					extractions = ex
				} else if ex, ok := rawMap["extraction_instructions"].(map[string]interface{}); ok {
					extractions = ex
				} else if ex, ok := rawMap["data_extraction"].(map[string]interface{}); ok {
					extractions = ex
				} else {
					// RESILIENCE: If no wrapper, assume root keys (excluding meta keys) are extractions
					extractions = make(map[string]interface{})
					for k, v := range rawMap {
						kLower := strings.ToLower(k)
						if kLower != "typescript_config" && kLower != "goal" && kLower != "explanation" &&
							kLower != "summary" && kLower != "data_extraction" && kLower != "extractions" &&
							kLower != "extraction_instructions" && kLower != "thought" && kLower != "steps" {
							extractions[k] = v
						}
					}
				}

				if extractions != nil {
					config.Extractions = make(map[string]string)
					for k, v := range extractions {
						if s, ok := v.(string); ok {
							config.Extractions[k] = s
						} else if obj, ok := v.(map[string]interface{}); ok {
							// Handle nested objects like {"price": {"regex": "..."}}
							if r, ok := obj["selector"].(string); ok {
								config.Extractions[k] = r
							} else if r, ok := obj["pattern"].(string); ok {
								config.Extractions[k] = r
							} else if r, ok := obj["regex"].(string); ok {
								config.Extractions[k] = r
							} else if r, ok := obj["value"].(string); ok {
								config.Extractions[k] = r
							}
						} else if arr, ok := v.([]interface{}); ok && len(arr) > 0 {
							if s, ok := arr[0].(string); ok {
								config.Extractions[k] = s
							}
						}
					}
				}

				// RESILIENCE: Handle typescript_config as string or object anywhere in the response
				if ts, ok := rawMap["typescript_config"].(string); ok {
					config.TypeScriptConfig = ts
				} else if tsObj, ok := rawMap["typescript_config"].(map[string]interface{}); ok {
					// Handle known keys or flat objects
					log.Printf("🩹 [MCP-SMART-SCRAPE] Converting typescript_config object to JS string...")
					config.TypeScriptConfig = convertStepsToJS(tsObj)
				} else {
					// ULTIMATE RESILIENCE: Search for anything named 'typescript_config' or 'interactions' or 'steps' recursively
					log.Printf("🩹 [MCP-SMART-SCRAPE] TypeScript config not at root, searching recursively...")
					config.TypeScriptConfig = findJSInObject(rawMap)
				}
				parseErr = nil
			} else {
				// FALLBACK: Use regex to extract pairs if JSON is still broken
				log.Printf("⚠️ [MCP-SMART-SCRAPE] JSON parse failed (%v), trying regex recovery...", err)
				config.Extractions = make(map[string]string)

				// Find "key": "value" pairs
				rePairs := regexp.MustCompile(`"([^"]+)"\s*:\s*"([\s\S]*?)"(?:\s*[,}])`)
				pairs := rePairs.FindAllStringSubmatch(cleanedResponse, -1)
				for _, p := range pairs {
					key := p[1]
					val := p[2]
					if key == "typescript_config" {
						config.TypeScriptConfig = val
					} else if key != "extractions" && key != "extraction_instructions" && key != "goal" && key != "explanation" && key != "summary" && key != "regex" && key != "pattern" && key != "selector" {
						config.Extractions[key] = val
					}
				}

				// Also look for values inside any nested object blocks (like {"price": {"regex": "..."}})
				reNested := regexp.MustCompile(`"([^"]+)"\s*:\s*[{]\s*([\s\S]*?)[}]`)
				nested := reNested.FindAllStringSubmatch(cleanedResponse, -1)
				for _, n := range nested {
					parentKey := n[1]
					inner := n[2]
					innerPairs := rePairs.FindAllStringSubmatch(inner, -1)

					// If the nested object has "regex", "pattern", "selector" or "value", map it as the parent key's value
					foundInner := false
					for _, p := range innerPairs {
						ik := p[1]
						iv := p[2]
						if ik == "regex" || ik == "pattern" || ik == "selector" || ik == "value" {
							config.Extractions[parentKey] = iv
							foundInner = true
						}
					}

					// Fallback: If it's just more fields inside an "extractions" object
					if !foundInner && (parentKey == "extractions" || parentKey == "extraction_instructions") {
						for _, p := range innerPairs {
							config.Extractions[p[1]] = p[2]
						}
					}
				}
				if len(config.Extractions) > 0 {
					parseErr = nil
				} else {
					parseErr = err
				}
			}
		}
	}

	if parseErr != nil {
		// Approach 2: Direct unmarshal into struct
		cleanedRes := response
		if idx := strings.Index(cleanedRes, "{"); idx != -1 {
			if lastIdx := strings.LastIndex(cleanedRes, "}"); lastIdx != -1 {
				cleanedRes = cleanedRes[idx : lastIdx+1]
				if err := json.Unmarshal([]byte(cleanedRes), &config); err != nil {
					parseErr = err
				} else {
					parseErr = nil
				}
			}
		}
	}

	if parseErr != nil {
		return nil, fmt.Errorf("failed to parse LLM planning response: %v (response was: %s)", parseErr, response)
	}

	// If this was a navigation-only planning pass, ensure extractions are empty
	if navigationOnly {
		if config.Extractions == nil {
			config.Extractions = make(map[string]string)
		} else {
			// clear any accidental extraction patterns
			config.Extractions = make(map[string]string)
		}
	}

	return &config, nil
}

// callExternalHDNTool calls an external tool on the HDN server
func (s *MCPKnowledgeServer) callExternalHDNTool(ctx context.Context, toolID string, params map[string]interface{}) (map[string]interface{}, error) {
	// Construct the URL for the internal tool invocation
	// Use hdnURL which points to the API server
	baseURL := s.hdnURL
	if baseURL == "" {
		baseURL = "http://localhost:8080"
	}
	url := fmt.Sprintf("%s/api/v1/tools/%s/invoke", baseURL, toolID)

	jsonData, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal params: %v", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// Add Agent attributes header if available in context?
	// For now, simple invocation is enough

	// Use a long timeout for execution tools
	client := &http.Client{Timeout: 300 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("tool call failed with status %d: %s", resp.StatusCode, string(body))
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %v", err)
	}

	return result, nil
}

// cleanHTMLForPlanning simplifies HTML to make it more digestible for the planning LLM
// convertStepsToJS takes a JSON object (often generated by larger models) and converts it to Playwright JS
// convertStepsToJS takes a JSON object (often generated by larger models) and converts it to Playwright JS
func convertStepsToJS(tsObj map[string]interface{}) string {
	var js string

	// Case 1: Structured array under "steps", "actions", or "interaction_logic"
	var steps []interface{}
	var ok bool

	if val, found := tsObj["steps"]; found {
		steps, ok = val.([]interface{})
	}
	if !ok {
		if val, found := tsObj["actions"]; found {
			steps, ok = val.([]interface{})
		}
	}
	if !ok {
		if val, found := tsObj["interaction_logic"]; found {
			steps, ok = val.([]interface{})
		}
	}
	if !ok {
		if val, found := tsObj["interactions"]; found {
			steps, ok = val.([]interface{})
		}
	}

	if ok && len(steps) > 0 {
		log.Printf("🩹 [MCP-SMART-SCRAPE] Converting structured steps array to JS...")
		for _, s := range steps {
			step, ok := s.(map[string]interface{})
			if !ok {
				continue
			}

			// RESILIENCE: If the model provided a raw command string
			if cmd, ok := step["command"].(string); ok && cmd != "" {
				js += cmd + "\n"
				continue
			}
			if cmd, ok := step["code"].(string); ok && cmd != "" {
				js += cmd + "\n"
				continue
			}
			if cmd, ok := step["js"].(string); ok && cmd != "" {
				js += cmd + "\n"
				continue
			}
			if cmd, ok := step["javascript"].(string); ok && cmd != "" {
				js += cmd + "\n"
				continue
			}

			action, _ := step["action"].(string)
			if action == "" {
				action, _ = step["type"].(string)
			}
			selector, _ := step["selector"].(string)
			if selector == "" {
				selector, _ = step["target"].(string)
			}
			value, _ := step["value"].(string)

			// Normalize action name
			action = strings.ToLower(strings.TrimSpace(action))

			switch action {
			case "fill", "type", "fill_input":
				js += fmt.Sprintf("await page.locator('%s').fill('%s');\n", selector, value)
			case "click":
				js += fmt.Sprintf("await page.locator('%s').click();\n", selector)
			case "selectoption", "select_option":
				js += fmt.Sprintf("await page.locator('%s').selectOption('%s');\n", selector, value)
			case "wait":
				js += "await page.waitForTimeout(1000);\n"
			case "waitfortimeout", "wait_fortimeout":
				js += fmt.Sprintf("await page.waitForTimeout(%v);\n", value)
			}
		}
	} else {
		// Case 2: Flat object or nested object with dynamic keys
		log.Printf("🩹 [MCP-SMART-SCRAPE] Non-array typescript_config detected, attempting recursive extraction...")
		js = extractStringsFromObject(tsObj)
	}
	return js
}

// extractStringsFromObject recursively finds all string values in an object and joins them
// findJSInObject recursively searches for anything that looks like a JS plan
func findJSInObject(obj map[string]interface{}) string {
	for k, v := range obj {
		if isJSKey(k) {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
			if nest, ok := v.(map[string]interface{}); ok {
				return convertStepsToJS(nest)
			}
			if arr, ok := v.([]interface{}); ok {
				return convertStepsToJS(map[string]interface{}{"steps": arr})
			}
		}
		if nest, ok := v.(map[string]interface{}); ok {
			if found := findJSInObject(nest); found != "" {
				return found
			}
		}
	}
	return ""
}

func isJSKey(k string) bool {
	k = strings.ToLower(k)
	return k == "typescript_config" || k == "steps" || k == "actions" || k == "interaction_logic" || k == "interactions" || k == "calculation_logic" || k == "navigation"
}

func extractStringsFromObject(obj map[string]interface{}) string {
	var js string
	for k, v := range obj {
		kLower := strings.ToLower(k)
		// Skip meta keys
		if kLower == "goal" || kLower == "explanation" || kLower == "summary" || kLower == "thought" {
			continue
		}

		if s, ok := v.(string); ok && s != "" {
			js += s + "\n"
		} else if nested, ok := v.(map[string]interface{}); ok {
			js += extractStringsFromObject(nested)
		} else if arr, ok := v.([]interface{}); ok {
			for _, item := range arr {
				if s, ok := item.(string); ok {
					js += s + "\n"
				} else if m, ok := item.(map[string]interface{}); ok {
					js += extractStringsFromObject(m)
				}
			}
		}
	}
	return js
}

func cleanHTMLForPlanning(html string) string {
	// 1. Remove noise tags with their content
	tagsToRemove := []string{"script", "style", "svg", "path", "link", "meta", "noscript", "iframe", "head", "header", "footer", "nav", "aside", "form"}
	// Note: We keep "form" if we need to see inputs, but actually MyClimate uses a lot of wrappers.
	// Wait, don't remove "form" if the goal is to fill a form!
	tagsToRemove = []string{"script", "style", "svg", "path", "link", "meta", "noscript", "iframe", "head", "header", "footer", "nav", "aside"}
	for _, tag := range tagsToRemove {
		re := regexp.MustCompile(`(?is)<` + tag + `[^>]*>.*?</` + tag + `>`)
		html = re.ReplaceAllString(html, "")
		re = regexp.MustCompile(`(?is)<` + tag + `[^>]*/>`)
		html = re.ReplaceAllString(html, "")
	}

	// 2. Remove HTML comments
	re := regexp.MustCompile(`(?s)<!--.*?-->`)
	html = re.ReplaceAllString(html, "")

	// 3. Keep ONLY critical attributes for selection/identification
	// whitelist: id, class, name, value, type, placeholder, href, data-*, aria-label
	reAttr := regexp.MustCompile(`(?i)\s([a-z0-9-]+)=(?:'[^']*'|"[^"]*"|[^\s>]+)`)
	html = reAttr.ReplaceAllStringFunc(html, func(fullMatch string) string {
		attr := strings.ToLower(strings.TrimSpace(fullMatch))
		whitelist := []string{"id=", "class=", "name=", "value=", "type=", "placeholder=", "href=", "data-", "aria-label="}
		for _, w := range whitelist {
			if strings.HasPrefix(attr, w) {
				return fullMatch
			}
		}
		return ""
	})

	// 4. Compact multiple spaces and newlines
	re = regexp.MustCompile(`\n+`)
	html = re.ReplaceAllString(html, "\n")
	re = regexp.MustCompile(`[ \t]+`)
	html = re.ReplaceAllString(html, " ")

	return strings.TrimSpace(html)
}

func sanitizeJSONResponse(js string) string {
	// 1. Remove markdown code blocks if present
	js = regexp.MustCompile("(?s)^```(?:json)?\n?").ReplaceAllString(js, "")
	js = regexp.MustCompile("(?s)\n?```$").ReplaceAllString(js, "")

	// 2. Find the first '{' and last '}' to isolate the JSON object
	first := strings.Index(js, "{")
	last := strings.LastIndex(js, "}")
	if first != -1 && last != -1 && last > first {
		js = js[first : last+1]
	}

	// 2.5. Replace backtick-delimited strings with quoted JSON strings
	// This handles cases like: "typescript_config": `await page.click('...')`
	reBacktick := regexp.MustCompile("`([^`]*)`")
	js = reBacktick.ReplaceAllStringFunc(js, func(m string) string {
		inner := strings.Trim(m, "`")
		inner = strings.ReplaceAll(inner, "\\", "\\\\")
		inner = strings.ReplaceAll(inner, "\"", "\\\"")
		inner = strings.ReplaceAll(inner, "\n", "\\n")
		return "\"" + inner + "\""
	})

	// 3. Fix backslash-escaped single quotes (LLM often thinks JSON needs these)
	js = strings.ReplaceAll(js, "\\'", "'")
	js = strings.ReplaceAll(js, "\\\\'", "'") // Fix double-escaped ones too

	// 4. Fix over-escaped brackets like \\[ and \\] which are invalid JSON escape sequences
	js = strings.ReplaceAll(js, "\\\\[", "[")
	js = strings.ReplaceAll(js, "\\\\]", "]")
	js = strings.ReplaceAll(js, "\\\\[", "[") // handles double-escaped too

	// 5. Robust: remove trailing commas before closing braces/brackets
	js = regexp.MustCompile(`,\s*([}\]])`).ReplaceAllString(js, "$1")

	// 5. Fix literal newlines in JSON strings (LLM sometimes leaves them unescaped)
	reNewline := regexp.MustCompile(`"(?:[^"\\]|\\.)*"`)
	js = reNewline.ReplaceAllStringFunc(js, func(s string) string {
		return strings.ReplaceAll(s, "\n", "\\n")
	})

	return strings.TrimSpace(js)
}

// buildActionableSnapshot extracts only actionable elements (forms, inputs, selects,
// buttons, labels, anchors) from a larger HTML snapshot so the LLM can focus
// on navigation interactions. Falls back to cleaned full HTML if nothing found.
func (s *MCPKnowledgeServer) buildActionableSnapshot(html string) string {
	// Prefer extracting actionable controls from the most relevant form if present
	reForm := regexp.MustCompile(`(?is)<form[^>]*>.*?</form>`)
	forms := reForm.FindAllString(html, -1)
	if len(forms) > 0 {
		// regex to extract actionable tags inside the form
		reControl := regexp.MustCompile(`(?is)<(?:input|select|button|textarea|label|a)[^>]*?(?:>.*?</(?:input|select|button|textarea|label|a)>|/>)`)

		bestScore := -1
		bestForm := ""
		bestControls := []string{}

		for _, f := range forms {
			controls := reControl.FindAllString(f, -1)
			score := len(controls)
			lower := strings.ToLower(f)
			if strings.Contains(lower, "flight_calculator") {
				score += 10
			}
			if strings.Contains(lower, "flight") {
				score += 3
			}
			if strings.Contains(lower, "calculator") {
				score += 3
			}
			if strings.Contains(lower, "from") && strings.Contains(lower, "to") {
				score += 2
			}

			if score > bestScore {
				bestScore = score
				bestForm = f
				bestControls = controls
			}
		}

		if len(bestControls) > 0 {
			snippet := strings.Join(bestControls, "\n")
			if len(snippet) > 10000 {
				snippet = snippet[:10000] + "...(truncated)"
			}
			return cleanHTMLForPlanning(snippet)
		}
		if bestForm != "" {
			return cleanHTMLForPlanning(bestForm)
		}

		// if no individual controls found, fall back to returning full form blocks
		joined := strings.Join(forms, "\n")
		return cleanHTMLForPlanning(joined)
	}

	// Otherwise extract small actionable elements
	re := regexp.MustCompile(`(?is)<(?:input|select|button|textarea|label|a)[^>]*?(?:>.*?</(?:input|select|button|textarea|label|a)>|/>)`)
	matches := re.FindAllString(html, -1)
	if len(matches) > 0 {
		snippet := strings.Join(matches, "\n")
		if len(snippet) > 20000 {
			snippet = snippet[:20000] + "...(truncated)"
		}
		return cleanHTMLForPlanning(snippet)
	}

	// Fallback: return the general cleaned HTML
	return cleanHTMLForPlanning(html)
}

// sanitizeNavigationScript applies general-purpose fixes to LLM-produced
// navigation JS by using the actionable HTML snapshot and goal text.
func (s *MCPKnowledgeServer) sanitizeNavigationScript(js, actionableHTML, goal string) string {
	if strings.TrimSpace(js) == "" {
		return js
	}

	// Replace click on input[name=...][value=...] with selectOption when a select exists.
	selectNames := map[string]struct{}{}
	reSelect := regexp.MustCompile(`(?is)<select[^>]*\bname=['"]([^'"]+)['"]`)
	for _, m := range reSelect.FindAllStringSubmatch(actionableHTML, -1) {
		if len(m) > 1 {
			selectNames[m[1]] = struct{}{}
		}
	}
	reClickInputValue := regexp.MustCompile(`page\.click\(\s*['"]input\[name=['"]([^'"]+)['"]\]\[value=['"]([^'"]+)['"]\]['"]\s*\)`)
	js = reClickInputValue.ReplaceAllStringFunc(js, func(m string) string {
		subs := reClickInputValue.FindStringSubmatch(m)
		if len(subs) != 3 {
			return m
		}
		name := subs[1]
		value := subs[2]
		if _, ok := selectNames[name]; ok {
			return fmt.Sprintf("await page.selectOption('select[name=\"%s\"]', '%s');", name, value)
		}
		return m
	})

	// Ensure a submit action exists for calculate/search/find goals.
	goalLower := strings.ToLower(goal)
	needsSubmit := strings.Contains(goalLower, "calculate") || strings.Contains(goalLower, "submit") || strings.Contains(goalLower, "search") || strings.Contains(goalLower, "find")
	if needsSubmit {
		hasSubmit := strings.Contains(js, "type=\\\"submit\\\"") || strings.Contains(js, "type='submit'") || strings.Contains(js, "keyboard.press('Enter')") || strings.Contains(js, "keyboard.press(\"Enter\")")
		if !hasSubmit {
			js = strings.TrimSpace(js) + "\n" + "await page.locator('input[type=\"submit\"], button[type=\"submit\"]').first().click();\nawait page.waitForTimeout(3000);"
			log.Printf("🧽 [MCP-SMART-SCRAPE] Added generic submit click to navigation script")
		}

		const fallbackMarker = "MCP_SUBMIT_FALLBACK"
		if !strings.Contains(js, fallbackMarker) {
			fallback := "\n" + "/* " + fallbackMarker + " */\n" +
				"const __mcpInitialUrl = page.url();\n" +
				"try { await page.waitForLoadState('networkidle'); } catch (e) {}\n" +
				"await page.waitForTimeout(1500);\n" +
				"const __mcpHasResults = await page.locator('[id*=\"result\"], .result, .results, [data-testid*=\"result\"]').first().count();\n" +
				"const __mcpHasForm = await page.locator('form').first().count();\n" +
				"if (page.url() === __mcpInitialUrl && __mcpHasResults === 0 && __mcpHasForm > 0) {\n" +
				"  try { await page.locator('input[type=\"submit\"], button[type=\"submit\"], .submit-form, .btn-primary').first().click(); } catch (e) {}\n" +
				"  try { await page.waitForLoadState('networkidle'); } catch (e) {}\n" +
				"  await page.waitForTimeout(1500);\n" +
				"}\n" +
				"if (page.url() === __mcpInitialUrl && __mcpHasResults === 0 && __mcpHasForm > 0) {\n" +
				"  try { await page.locator('form').first().evaluate((f) => f.submit()); } catch (e) {}\n" +
				"  try { await page.waitForLoadState('networkidle'); } catch (e) {}\n" +
				"  await page.waitForTimeout(1500);\n" +
				"}\n" +
				"if (page.url() === __mcpInitialUrl && __mcpHasResults === 0 && __mcpHasForm > 0) {\n" +
				"  try { await page.keyboard.press('Enter'); } catch (e) {}\n" +
				"  await page.waitForTimeout(1500);\n" +
				"}\n"
			js = strings.TrimSpace(js) + fallback
			log.Printf("🧽 [MCP-SMART-SCRAPE] Added submit fallback verification to navigation script")
		}
	}

	return js
}

// buildExtractionSnapshot reduces HTML to likely result sections based on ids mentioned in the goal.
// Falls back to a truncated cleaned HTML when no matching ids are found.
func (s *MCPKnowledgeServer) buildExtractionSnapshot(html string, goal string) string {
	// Find any id=... mentions in the goal text
	ids := map[string]struct{}{}
	reGoalID := regexp.MustCompile(`(?i)id=([a-z0-9_-]+)`) // simple id=flight_calc_details
	for _, m := range reGoalID.FindAllStringSubmatch(goal, -1) {
		if len(m) > 1 {
			ids[m[1]] = struct{}{}
		}
	}

	if len(ids) > 0 {
		blocks := []string{}
		seen := map[string]struct{}{}
		for id := range ids {
			// Try to capture the full element by id first
			reByID := regexp.MustCompile(fmt.Sprintf(`(?is)<[^>]*\bid=['"]%s['"][^>]*>.*?</[^>]+>`, regexp.QuoteMeta(id)))
			if match := reByID.FindString(html); match != "" {
				if _, ok := seen[match]; !ok {
					blocks = append(blocks, match)
					seen[match] = struct{}{}
				}
				continue
			}
			// Fallback: take a window around the id attribute occurrence
			needle := fmt.Sprintf("id=\"%s\"", id)
			idx := strings.Index(html, needle)
			if idx == -1 {
				needle = fmt.Sprintf("id='%s'", id)
				idx = strings.Index(html, needle)
			}
			if idx != -1 {
				start := idx - 1000
				if start < 0 {
					start = 0
				}
				end := idx + 2000
				if end > len(html) {
					end = len(html)
				}
				window := html[start:end]
				if _, ok := seen[window]; !ok {
					blocks = append(blocks, window)
					seen[window] = struct{}{}
				}
			}
		}

		if len(blocks) > 0 {
			snippet := strings.Join(blocks, "\n")
			if len(snippet) > 20000 {
				snippet = snippet[:20000] + "...(truncated)"
			}
			return cleanHTMLForPlanning(snippet)
		}
	}

	// Fallback: return a truncated cleaned HTML
	snippet := cleanHTMLForPlanning(html)
	if len(snippet) > 20000 {
		snippet = snippet[:20000] + "...(truncated)"
	}
	return snippet
}
