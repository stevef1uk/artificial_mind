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
			Name:        "get_concept",
			Description: "Get a specific concept from the Neo4j knowledge graph by name and domain.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Name of the concept",
					},
					"domain": map[string]interface{}{
						"type":        "string",
						"description": "Domain of the concept (optional)",
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
func (s *MCPKnowledgeServer) scrapeWithConfig(ctx context.Context, url, tsConfig string, async bool, extractions map[string]string, getHTML bool) (interface{}, error) {
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
	pollTimeout := 90 * time.Second
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
										for i := 1; i < len(match); i++ {
											if match[i] != "" {
												extracted = append(extracted, match[i])
											}
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

		// Check if typescript_config is provided - if so, parse and execute directly
		if tsConfig, ok := args["typescript_config"].(string); ok && tsConfig != "" {
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

			return s.scrapeWithConfig(ctx, url, tsConfig, isAsync, extractions, false)
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
			filepath.Join(projectRoot, "bin", "html-scraper"),
			filepath.Join(projectRoot, "bin", "tools", "html_scraper"),
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
				break
			}
		}

		if scraperBin == "" {
			// Fallback to raw HTTP client if scraper not found
			client := NewSafeHTTPClient()
			content, err := client.SafeGetWithContentCheck(ctx, url)
			if err != nil {
				return nil, err
			}
			return map[string]interface{}{
				"content": []map[string]interface{}{
					{
						"type": "text",
						"text": content,
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
		if ts, ok := args["typescript_config"].(string); ok {
			if userConfig == nil {
				userConfig = &ScrapeConfig{Extractions: make(map[string]string)}
			}
			userConfig.TypeScriptConfig = ts
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

		// Use the Simple Docker Executor
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
			log.Printf("🤖 [BROWSE-WEB] Generating actions from instructions using LLM...")

			// First, get the page HTML to analyze
			log.Printf("🌐 [BROWSE-WEB] Fetching page HTML for analysis...")
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

			htmlOutputStr, stderrHTML, err := runCommandWithLiveOutput(htmlCtx, getHTMLCmd, "🔍 [BROWSE-WEB][HTML]")
			if err != nil {
				if errors.Is(htmlCtx.Err(), context.DeadlineExceeded) {
					log.Printf("⚠️ [BROWSE-WEB] HTML fetch timed out after 20s")
				} else {
					log.Printf("⚠️ [BROWSE-WEB] Failed to get page HTML: %v, will proceed without it", err)
				}
				if stderrHTML != "" {
					log.Printf("🔍 [BROWSE-WEB][HTML] stderr:\n%s", stderrHTML)
				}
			} else {
				// Extract JSON from output - look for the browser result JSON object
				// The browser tool outputs JSON with fields: success, url, title, extracted, html
				outputStr := htmlOutputStr

				// Try to find JSON that looks like our browser result (contains "success" and "html" fields)
				// Look for the pattern: {"success":... which is the actual result
				resultPattern := `{"success"`
				resultStart := strings.LastIndex(outputStr, resultPattern)

				if resultStart == -1 {
					// Fallback: look for any JSON object at the end
					resultStart = strings.LastIndex(outputStr, "{")
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

	// Convert actions to JSON
	actionsJSON, err := json.Marshal(actions)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal actions: %w", err)
	}

	// Execute command with live output and timeout
	toolCtx, toolCancel := context.WithTimeout(ctx, time.Duration(timeout+15)*time.Second)
	defer toolCancel()
	runCmd := exec.CommandContext(toolCtx, browserBin,
		"-url", url,
		"-actions", string(actionsJSON),
		"-timeout", fmt.Sprintf("%d", timeout),
	)

	stdoutStr, stderrStr, runErr := runCommandWithLiveOutput(toolCtx, runCmd, "🔍 [BROWSE-WEB][TOOL]")
	if runErr != nil {
		if errors.Is(toolCtx.Err(), context.DeadlineExceeded) {
			return nil, fmt.Errorf("browser execution timed out after %ds", timeout+15)
		}
		// Include stderr in error message for debugging
		return nil, fmt.Errorf("browser execution failed: %v - stdout: %s - stderr: %s", runErr, stdoutStr, stderrStr)
	}

	// Log stderr (contains debug output) in case anything was buffered
	if stderrStr != "" {
		log.Printf("🔍 [BROWSE-WEB] Browser tool debug output:\n%s", stderrStr)
	}
	// Extract JSON from output (may have log messages mixed in)
	// Look for the last complete JSON object in the output
	outputStr := stdoutStr

	// Find the last opening brace
	jsonStart := strings.LastIndex(outputStr, "{")
	if jsonStart == -1 {
		return nil, fmt.Errorf("failed to find JSON object in browser output")
	}

	// Find the matching closing brace by counting braces
	braceCount := 0
	jsonEnd := -1
	for i := jsonStart; i < len(outputStr); i++ {
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

	if jsonEnd == -1 {
		return nil, fmt.Errorf("failed to find complete JSON object in browser output")
	}

	jsonStr := outputStr[jsonStart : jsonEnd+1]

	// Parse JSON result
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		// Try cleaning the JSON string (remove any non-ASCII characters that might interfere)
		cleaned := strings.Map(func(r rune) rune {
			// Keep ASCII printable characters, newlines, tabs, and common JSON characters
			if (r >= 32 && r <= 126) || r == '\n' || r == '\t' || r == '\r' {
				return r
			}
			return -1
		}, jsonStr)
		if err2 := json.Unmarshal([]byte(cleaned), &result); err2 != nil {
			return nil, fmt.Errorf("failed to parse browser result: %w (cleaned also failed: %v) - json preview: %s", err, err2, jsonStr[:min(200, len(jsonStr))])
		}
	}

	// Check if execution was successful
	if success, ok := result["success"].(bool); ok && !success {
		if errMsg, ok := result["error"].(string); ok {
			return nil, fmt.Errorf("browser execution failed: %s", errMsg)
		}
		return nil, fmt.Errorf("browser execution failed")
	}

	// Return extracted data in MCP content format
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
	}, nil
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

	// Extract input fields with their attributes - be more specific
	inputRe := regexp.MustCompile(`(?i)<input[^>]*>`)
	inputs := inputRe.FindAllString(html, -1)
	if len(inputs) > 0 {
		info.WriteString("Input fields found (use these EXACT selectors):\n")
		for i, input := range inputs {
			if i < 15 { // Show more inputs
				// Extract key attributes
				var attrs []string
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
					info.WriteString(fmt.Sprintf("  Input %d: %s\n", i+1, strings.Join(attrs, ", ")))
				} else {
					info.WriteString(fmt.Sprintf("  Input %d: %s (no clear identifiers)\n", i+1, input[:min(80, len(input))]))
				}
			}
		}
		if len(inputs) > 15 {
			info.WriteString(fmt.Sprintf("  ... and %d more inputs\n", len(inputs)-15))
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

	waitErr := cmd.Wait()
	wg.Wait()

	return stdoutBuf.String(), stderrBuf.String(), waitErr
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

	prompt := fmt.Sprintf(`You are a JSON generator. Your ONLY job is to return a valid JSON array. Nothing else.

URL: %s
User Task: %s
%s
Available Form Elements (for reference):
%s

Page HTML (for reference):
%s

INSTRUCTIONS:
1. Look at "Available Form Elements" above
2. Find selectors that match the user task
3. Return ONLY a JSON array starting with [ and ending with ]
4. Each action object must have: "type", "selector", optionally "value", "wait_for", "timeout"
5. Action types: "wait", "fill", "click", "select", "extract"
6. MUST start with: {"type":"wait","selector":"body","timeout":5}
7. MUST use selectors from "Available Form Elements" list - copy them exactly
8. DO NOT invent selectors - if not in the list, don't use it
9. After each fill, add: {"type":"wait","selector":"body","timeout":1}
10. After click, wait for results: {"type":"wait","wait_for":"SELECTOR","timeout":15}

REQUIRED OUTPUT FORMAT (copy this structure, replace REAL_SELECTOR with actual selectors from list):
[{"type":"wait","selector":"body","timeout":5},{"type":"wait","wait_for":"REAL_SELECTOR","timeout":10},{"type":"fill","selector":"REAL_SELECTOR","value":"Southampton"},{"type":"wait","selector":"body","timeout":1},{"type":"fill","selector":"REAL_SELECTOR","value":"Newcastle"},{"type":"wait","selector":"body","timeout":1},{"type":"click","selector":"REAL_SELECTOR"},{"type":"wait","wait_for":"REAL_SELECTOR","timeout":15},{"type":"extract","extract":{"co2_emissions":"REAL_SELECTOR"}}]

CRITICAL: Return ONLY the JSON array. No text before. No text after. No explanations. No emojis. Just the JSON array starting with [ and ending with ].`, url, instructions, selectorListStr, formInfo, htmlPreview)

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
				var test []interface{}
				if err := json.Unmarshal([]byte(potentialJSON), &test); err == nil {
					log.Printf("✅ [BROWSE-WEB] Successfully parsed extracted JSON!")
					jsonStr = potentialJSON
				} else {
					log.Printf("❌ [BROWSE-WEB] Extracted JSON failed to parse: %v", err)
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
	// Remove markdown code blocks if present
	response = strings.TrimSpace(response)

	// Remove common prefixes that LLM adds before JSON (case-insensitive)
	prefixes := []string{"SOURCES:", "Here is", "Here's", "The JSON", "JSON:", "Actions:", "Here are", "Koffie:", "Koffie", "Coffee:", "Coffee"}
	for _, prefix := range prefixes {
		// Case-insensitive prefix check
		responseLower := strings.ToLower(response)
		prefixLower := strings.ToLower(prefix)
		if strings.HasPrefix(responseLower, prefixLower) {
			// Find the first [ after the prefix
			afterPrefix := response[len(prefix):]
			startIdx := strings.Index(afterPrefix, "[")
			if startIdx != -1 {
				response = afterPrefix[startIdx:]
				break
			}
		}
	}

	// Also handle colon-separated prefixes (e.g., "Koffie: [")
	if idx := strings.Index(response, ": ["); idx != -1 {
		// Check if it's a known prefix pattern
		beforeColon := strings.TrimSpace(response[:idx])
		if len(beforeColon) < 20 { // Reasonable prefix length
			response = response[idx+2:] // Skip ": "
		}
	}

	// Remove markdown code fences (```json ... ``` or ``` ... ```)
	if strings.Contains(response, "```") {
		lines := strings.Split(response, "\n")
		var jsonLines []string
		inCodeBlock := false
		for _, line := range lines {
			if strings.HasPrefix(strings.TrimSpace(line), "```") {
				inCodeBlock = !inCodeBlock
				continue
			}
			if inCodeBlock {
				jsonLines = append(jsonLines, line)
			} else if !inCodeBlock && strings.TrimSpace(line) != "" {
				// If not in code block but line is not empty, might be JSON
				jsonLines = append(jsonLines, line)
			}
		}
		if len(jsonLines) > 0 {
			response = strings.Join(jsonLines, "\n")
		}
	}

	// Remove any text before the first [
	// LLM sometimes adds explanation before the JSON
	startIdx := strings.Index(response, "[")
	if startIdx == -1 {
		// Try to find JSON object instead
		startIdx = strings.Index(response, "{")
		if startIdx == -1 {
			return ""
		}
		// If we found {, wrap it in array
		// But first, let's try to find [ anyway by looking for common patterns
		// Look for "type" which is in our action objects
		typeIdx := strings.Index(response, `"type"`)
		if typeIdx != -1 {
			// Look backwards for [
			for i := typeIdx; i >= 0; i-- {
				if response[i] == '[' {
					startIdx = i
					break
				}
			}
		}
		if startIdx == -1 {
			return ""
		}
	}

	// Remove everything before the JSON array
	if startIdx > 0 {
		response = response[startIdx:]
	}

	// Find matching closing bracket (respecting string boundaries)
	depth := 0
	endIdx := -1
	inString := false
	escapeNext := false

	for i := 0; i < len(response); i++ {
		char := response[i]

		if escapeNext {
			escapeNext = false
			continue
		}

		if char == '\\' {
			escapeNext = true
			continue
		}

		if char == '"' {
			inString = !inString
			continue
		}

		if !inString {
			if char == '[' {
				depth++
			} else if char == ']' {
				depth--
				if depth == 0 {
					endIdx = i
					break
				}
			}
		}
	}

	if endIdx >= 0 && endIdx < len(response) {
		jsonStr := response[0 : endIdx+1]
		// Validate it's valid JSON by trying to parse it
		var test []interface{}
		err := json.Unmarshal([]byte(jsonStr), &test)
		if err == nil {
			return jsonStr
		}
		// If parsing failed, log the error and try to fix common issues
		log.Printf("⚠️ [BROWSE-WEB] JSON parse error: %v, attempting to fix...", err)
		// Try removing trailing text after ]
		if endIdx+1 < len(response) {
			// Maybe there's more text after the JSON
			return jsonStr
		}
		return jsonStr
	}

	return ""
}

// queryViaHDN queries Neo4j via HDN's knowledge query endpoint
func (s *MCPKnowledgeServer) queryViaHDN(ctx context.Context, cypherQuery string) (interface{}, error) {
	queryURL := s.hdnURL + "/api/v1/knowledge/query"
	if s.hdnURL == "" {
		queryURL = "http://localhost:8080/api/v1/knowledge/query"
	} else {
		// If connecting to ourselves (Kubernetes service DNS or same host), use localhost
		// This prevents connection issues when MCP server tries to call HDN via service DNS
		if isSelfConnectionHDN(queryURL) {
			// Parse URL to get port, default to 8080
			parsedURL, err := url.Parse(queryURL)
			if err == nil {
				port := parsedURL.Port()
				if port == "" {
					port = "8080" // Default HDN port
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
				if !skipWords[word] && len(word) > 2 {
					meaningfulWords = append(meaningfulWords, word)
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

	// 1. Fetch page HTML using the scraper service (in get_html mode)
	log.Printf("📥 [MCP-SMART-SCRAPE] Fetching page content to plan scrape...")
	htmlResultRaw, err := s.scrapeWithConfig(ctx, url, "", false, nil, true) // Pass true for getHTML
	if err != nil {
		return nil, fmt.Errorf("failed to fetch page content: %v", err)
	}

	htmlResult, ok := htmlResultRaw.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("unexpected internal result format")
	}

	// The scraper returns results inside a "result" key for polling
	innerResult, ok := htmlResult["result"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("could not find result in scrape response")
	}

	cleanedHTML, ok := innerResult["cleaned_html"].(string)
	if !ok || cleanedHTML == "" {
		return nil, fmt.Errorf("scraper did not return cleaned_html")
	}

	// 2. Plan the scrape using LLM
	log.Printf("📋 [MCP-SMART-SCRAPE] Planning scrape config with LLM (%d chars of HTML)...", len(cleanedHTML))
	config, err := s.planScrapeWithLLM(ctx, cleanedHTML, goal, userConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to plan scrape with LLM: %v", err)
	}

	// Merge user results if provided
	if userConfig != nil {
		if config.TypeScriptConfig == "" {
			config.TypeScriptConfig = userConfig.TypeScriptConfig
		}
		if len(config.Extractions) == 0 {
			config.Extractions = userConfig.Extractions
		}
	}

	log.Printf("🚀 [MCP-SMART-SCRAPE] Executing planned scrape with %d extraction patterns", len(config.Extractions))

	// 3. Execute the planned scrape - request HTML if we have extraction patterns
	requestHTML := len(config.Extractions) > 0
	return s.scrapeWithConfig(ctx, url, config.TypeScriptConfig, false, config.Extractions, requestHTML)
}

type ScrapeConfig struct {
	TypeScriptConfig string            `json:"typescript_config"`
	Extractions      map[string]string `json:"extractions"`
}

func (s *MCPKnowledgeServer) planScrapeWithLLM(ctx context.Context, html string, goal string, hint *ScrapeConfig) (*ScrapeConfig, error) {
	if s.llmClient == nil {
		return nil, fmt.Errorf("LLM client not configured on knowledge server")
	}

	// Normalize HTML for LLM
	html = strings.ReplaceAll(html, "\"", "'")
	if len(html) > 125000 {
		html = html[:125000] + "...(truncated)"
	}

	systemPrompt := `You are an expert web scraper configuration generator.
Your task is to analyze an HTML snapshot and generate a scraping plan to achieve a specific GOAL.

Goal: Generate a valid JSON object with:
- "typescript_config": (string) A sequence of Playwright commands (e.g., await page.click('...')) to navigate or reveal data (like clicking tabs) if required by the goal.
- "extractions": (map) A set of named extraction patterns where key is the field name and value is a REGEX string to extract that data.

REGEX RULES:
1. ONLY standard Go regex (NO lookarounds like ?<= or ?=).
2. USE CAPTURING GROUPS () if you want to extract specific text. The scraper uses the first group.
3. Use single quotes (') for HTML attributes in your regex.
4. Use [^>]*? to skip unknown attributes in a tag.
5. Use .*? to match across tags or within rows.
Output ONLY the JSON object.`

	userPrompt := fmt.Sprintf(`### GOAL
%s

### HTML SNAPSHOT (Truncated)
%s
`, goal, html)

	if hint != nil {
		userPrompt += "\n### USER HINTS (Prioritize these concepts):\n"
		if hint.TypeScriptConfig != "" {
			userPrompt += fmt.Sprintf("- TypeScript Hint: %s\n", hint.TypeScriptConfig)
		}
		if len(hint.Extractions) > 0 {
			userPrompt += "- Extraction Hints (Key Names to use):\n"
			for k := range hint.Extractions {
				userPrompt += fmt.Sprintf("  - %s\n", k)
			}
		}
	}

	userPrompt += `
### TASK
Generate the JSON configuration to extract the data requested in the GOAL.

INSTRUCTIONS:
- Identify the data requested in the GOAL.
- Create specific field names in 'extractions' for each piece of data (e.g., "account_name", "interest_rate").
- If the goal asks for a list or table, create regex patterns that match one row at a time. The scraper will find all occurrences.
- Use class='[^']*KEYWORD[^']*' to match elements based on partial class names you see in the HTML.
- If you need to click something first (like a "Show More" button or a tab), include it in 'typescript_config'.
- Output ONLY valid JSON.`

	// Call LLM
	response, err := s.llmClient.callLLMWithContextAndPriority(ctx, systemPrompt+"\n\n"+userPrompt, PriorityHigh)
	if err != nil {
		return nil, err
	}

	// Clean/Parse JSON - Enhanced extraction logic
	var config ScrapeConfig
	var parseErr error

	// Try approach 1: Find first { and try parsing incrementally from there
	if idx := strings.Index(response, "{"); idx != -1 {
		// Try progressively from first { to end of string
		for endIdx := idx + 1; endIdx <= len(response); endIdx++ {
			candidate := response[idx:endIdx]

			// Try to unmarshal this candidate
			var tempConfig ScrapeConfig
			if err := json.Unmarshal([]byte(candidate), &tempConfig); err == nil {
				// Success! We found valid JSON
				config = tempConfig
				parseErr = nil
				break
			}
			parseErr = err

			// Skip if we haven't found a closing brace yet
			if strings.LastIndex(candidate, "}") == -1 {
				continue
			}
		}
	}

	// If that didn't work, try the old approach (first { to last })
	if parseErr != nil {
		cleanedResponse := response
		if idx := strings.Index(cleanedResponse, "{"); idx != -1 {
			if lastIdx := strings.LastIndex(cleanedResponse, "}"); lastIdx != -1 {
				cleanedResponse = cleanedResponse[idx : lastIdx+1]
				if err := json.Unmarshal([]byte(cleanedResponse), &config); err != nil {
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

	return &config, nil
}
