package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
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
func (s *MCPKnowledgeServer) HandleRequest(w http.ResponseWriter, r *http.Request) {
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
	}

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
			query = fmt.Sprintf("MATCH (c:Concept) WHERE toLower(c.name) CONTAINS toLower('%s') RETURN c LIMIT 10", escapedConcept)
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

// queryViaHDN queries Neo4j via HDN's knowledge query endpoint
func (s *MCPKnowledgeServer) queryViaHDN(ctx context.Context, cypherQuery string) (interface{}, error) {
	url := s.hdnURL + "/api/v1/knowledge/query"
	if s.hdnURL == "" {
		url = "http://localhost:8081/api/v1/knowledge/query"
	}

	reqBody := map[string]string{"query": cypherQuery}
	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	resp, err := http.Post(url, "application/json", strings.NewReader(string(jsonData)))
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
				"text":      r.Text,
				"timestamp": r.Timestamp,
				"metadata":  r.Metadata,
				"session_id": r.SessionID,
				"outcome":   r.Outcome,
				"tags":      r.Tags,
			})
		}

		return map[string]interface{}{
			"results": resultMaps,
			"count":   len(resultMaps),
			"query":   query,
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
	if collection == "WikipediaArticle" {
		queryStr = fmt.Sprintf(`{
			Get {
				WikipediaArticle(nearVector: {vector: %s}, limit: %d) {
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
	} else {
		// Generic collection query
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

	if resp.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("weaviate returned status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var result struct {
		Data struct {
			Get map[string][]map[string]interface{} `json:"Get"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
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
			// Step 1: Check distance threshold (MANDATORY)
			var distance float64
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
			
			// Step 2: Keyword matching (PRIMARY)
			// Since hash-based embeddings are unreliable, keyword matching remains primary,
			// but we relax some of the earlier ultra-strict constraints so that relevant
			// Wikipedia hits are not completely filtered out.
			if len(keywords) == 0 {
				log.Printf("🔍 [MCP-KNOWLEDGE] Filtered out result (no keywords to match): %v", item["title"])
				continue
			}
			
			// Get title and text for matching
			title, hasTitle := item["title"].(string)
			text, hasText := item["text"].(string)
			
			if !hasTitle && !hasText {
				log.Printf("🔍 [MCP-KNOWLEDGE] Filtered out result (no title or text): %v", item)
				continue
			}
			
			titleLower := ""
			if hasTitle {
				titleLower = strings.ToLower(title)
			}
			
			// Relaxed rule: primary keyword MUST be in title OR text (for collections with titles),
			// so good matches in the article body are not discarded.
			primaryKeyword := keywords[0]
			primaryInTitle := strings.Contains(titleLower, primaryKeyword)
			primaryInText := false
			if hasText {
				textLower := strings.ToLower(text)
				// Check first 1000 chars (more than before to catch context)
				textPreview := textLower
				if len(textPreview) > 1000 {
					textPreview = textPreview[:1000]
				}
				primaryInText = strings.Contains(textPreview, primaryKeyword)
			}

			if !primaryInTitle && !primaryInText {
				log.Printf("🔍 [MCP-KNOWLEDGE] Filtered out result (primary keyword '%s' not in title or text preview, distance=%.3f): %v", primaryKeyword, distance, title)
				continue
			}
			
			// Passed all filters
			log.Printf("✅ [MCP-KNOWLEDGE] Including result (distance=%.3f, primary keyword '%s' matched in title/text): %v", distance, primaryKeyword, title)
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
	cypher := fmt.Sprintf("MATCH (c:Concept {name: '%s'", escapedName)
	if domain != "" {
		escapedDomain := strings.ReplaceAll(domain, "'", "\\'")
		cypher += fmt.Sprintf(", domain: '%s'", escapedDomain)
	}
	cypher += "}) RETURN c"

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
	s.router.HandleFunc("/mcp", s.mcpKnowledgeServer.HandleRequest).Methods("POST")
	s.router.HandleFunc("/api/v1/mcp", s.mcpKnowledgeServer.HandleRequest).Methods("POST")

	log.Printf("✅ [MCP-KNOWLEDGE] MCP knowledge server registered at /mcp and /api/v1/mcp")
}
