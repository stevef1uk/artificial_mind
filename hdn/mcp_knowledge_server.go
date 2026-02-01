package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	mempkg "hdn/memory"

	"github.com/redis/go-redis/v9"
)

// MCPKnowledgeServer exposes knowledge bases (Neo4j, Weaviate, Qdrant) as MCP tools
type MCPKnowledgeServer struct {
	domainKnowledge mempkg.DomainKnowledgeClient
	vectorDB        mempkg.VectorDBAdapter
	redis           *redis.Client
	hdnURL          string // For proxying queries
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
func NewMCPKnowledgeServer(domainKnowledge mempkg.DomainKnowledgeClient, vectorDB mempkg.VectorDBAdapter, redis *redis.Client, hdnURL string) *MCPKnowledgeServer {
	return &MCPKnowledgeServer{
		domainKnowledge: domainKnowledge,
		vectorDB:        vectorDB,
		redis:           redis,
		hdnURL:          hdnURL,
	}
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
		Description: "Scrape content from a URL safely. Useful for reading documentation or checking external sites.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"url": map[string]interface{}{"type": "string"},
			},
			"required": []string{"url"},
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
		Name:        "read_google_data",
		Description: "Read emails or calendar events from Google Workspace via n8n integration.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": map[string]interface{}{"type": "string", "description": "Search query for emails or calendar events"},
				"type":  map[string]interface{}{"type": "string", "enum": []string{"email", "calendar", "all"}, "description": "Type of data to retrieve", "default": "all"},
			},
			"required": []string{"query"},
		},
	})

	return map[string]interface{}{
		"tools": tools,
	}, nil
}

// callTool executes an MCP tool
func (s *MCPKnowledgeServer) callTool(ctx context.Context, toolName string, arguments map[string]interface{}) (interface{}, error) {
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
	case "read_google_data":
		return s.readGoogleWorkspace(ctx, arguments)
	case "scrape_url", "execute_code", "read_file":
		// Route to the new wrapper
		return s.executeToolWrapper(ctx, toolName, arguments)
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
		)
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

	log.Printf("📥 [MCP-KNOWLEDGE] readGoogleWorkspace called with query: '%s', type: '%s'", query, dataType)

	// Construct request payload
	payload := map[string]interface{}{
		"query": query,
		"type":  dataType,
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
	client := &http.Client{Timeout: 60 * time.Second}
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
					// Check if first item is already an email object (has Subject, From, To)
					if _, hasSubject := firstItem["Subject"]; hasSubject {
						log.Printf("📧 [MCP-KNOWLEDGE] Items are already email objects (no json wrapper)")
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
	} else {
		log.Printf("⚠️ [MCP-KNOWLEDGE] n8n returned unexpected type: %T", result)
	}

	log.Printf("✅ [MCP-KNOWLEDGE] Successfully retrieved Google Workspace data (type: %s, size: %d)", resultType, resultLen)
	return finalResult, nil
}
