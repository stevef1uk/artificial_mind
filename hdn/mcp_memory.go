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
)

// queryNeo4j executes a Cypher query against Neo4j
func (s *MCPKnowledgeServer) queryNeo4j(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	query, ok := args["query"].(string)
	if !ok || query == "" {
		return nil, fmt.Errorf("query parameter is required")
	}

	if nlQuery, ok := args["natural_language"].(string); ok && nlQuery != "" {

		log.Printf("🧠 [MCP-KNOWLEDGE] Natural language query: %s", nlQuery)

		if strings.HasPrefix(strings.ToLower(nlQuery), "what is ") {
			concept := strings.TrimSpace(strings.TrimPrefix(strings.ToLower(nlQuery), "what is "))

			escapedConcept := strings.ReplaceAll(concept, "'", "\\'")

			query = fmt.Sprintf("MATCH (c:Concept) WHERE toLower(c.name) CONTAINS toLower('%s') OR toLower(c.definition) CONTAINS toLower('%s') RETURN c LIMIT 10", escapedConcept, escapedConcept)
			log.Printf("🧠 [MCP-KNOWLEDGE] Translated to Cypher: %s", query)
		}
	}

	if s.hdnURL != "" {
		return s.queryViaHDN(ctx, query)
	}

	if s.domainKnowledge != nil {

		return s.queryViaHDN(ctx, query)
	}

	return nil, fmt.Errorf("Neo4j not available")
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

	collection := "AgiEpisodes"
	if c, ok := args["collection"].(string); ok && c != "" {
		collection = c
	}

	if collection == "AvatarContext" {
		return s.searchAvatarContext(ctx, args)
	}

	if s.vectorDB == nil {
		return nil, fmt.Errorf("Weaviate not available")
	}

	vec := s.toyEmbed(query, 768)

	if collection == "AgiEpisodes" || collection == "AgiWiki" {

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
		vec[i] = float32((hash>>i)&1) * 0.5
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

	embedding, err := s.getOllamaEmbedding(ctx, query)
	if err != nil {
		log.Printf("⚠️ [MCP-KNOWLEDGE] Failed to get embedding, falling back to keyword search: %v", err)

		return s.searchAvatarContextKeyword(ctx, query, limit)
	}

	weaviateURL := os.Getenv("WEAVIATE_URL")
	if weaviateURL == "" {
		weaviateURL = "http://localhost:8080"
	}

	vectorStr := "["
	for i, v := range embedding {
		if i > 0 {
			vectorStr += ","
		}
		vectorStr += fmt.Sprintf("%.6f", v)
	}
	vectorStr += "]"

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

	ollamaURL := os.Getenv("OLLAMA_URL")
	if ollamaURL == "" {
		ollamaURL = os.Getenv("OLLAMA_BASE_URL")
	}
	if ollamaURL == "" {
		ollamaURL = os.Getenv("OPENAI_BASE_URL")
	}
	if ollamaURL == "" {

		ollamaURL = "http://localhost:11434"
	}

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

// extractSearchKeywords extracts meaningful keywords from a query, skipping intent noise
func (s *MCPKnowledgeServer) extractSearchKeywords(query string) []string {

	actualQuery := query

	if idx := strings.Index(query, "query='"); idx >= 0 {
		start := idx + 7
		end := strings.Index(query[start:], "'")
		if end > 0 {
			actualQuery = query[start : start+end]
		}
	} else if idx := strings.Index(query, "query=\""); idx >= 0 {
		start := idx + 7
		end := strings.Index(query[start:], "\"")
		if end > 0 {
			actualQuery = query[start : start+end]
		}
	} else if idx := strings.Index(query, "about '"); idx >= 0 {
		start := idx + 7
		end := strings.Index(query[start:], "'")
		if end > 0 {
			actualQuery = query[start : start+end]
		}
	} else if idx := strings.Index(query, "about \""); idx >= 0 {
		start := idx + 7
		end := strings.Index(query[start:], "\"")
		if end > 0 {
			actualQuery = query[start : start+end]
		}
	}

	queryLower := strings.ToLower(strings.TrimSpace(actualQuery))
	queryWords := strings.Fields(queryLower)

	stopWords := map[string]bool{
		"the": true, "a": true, "an": true, "and": true, "or": true, "but": true,
		"in": true, "on": true, "at": true, "to": true, "for": true, "of": true,
		"with": true, "by": true, "is": true, "are": true, "was": true, "were": true,
		"who": true, "what": true, "where": true, "when": true, "why": true, "how": true,
		"tell": true, "me": true, "about": true, "search": true, "find": true,
		"use": true, "mcp_search_weaviate": true, "tool": true,
		"articles": true, "episodic": true, "memory": true, "news": true,
		"summarize": true, "summarise": true, "summary": true, "latest": true,
		"current": true, "recent": true, "update": true, "updated": true,
		"situation": true, "information": true, "info": true, "results": true,
		"brevity": true, "required": true, "tokens": true, "under": true, "than": true,
		"within": true, "limit": true, "words": true, "characters": true, "chars": true,
		"brief": true, "short": true, "quick": true, "concise": true, "length": true,
		"please": true, "can": true, "you": true, "answer": true,
		"query": true, "question": true, "regarding": true,
	}

	var keywords []string
	for _, word := range queryWords {
		word = strings.Trim(word, ".,!?;:()[]{}'\"")

		if word == "" || stopWords[word] || len(word) <= 1 {
			continue
		}

		isNumeric := true
		for _, char := range word {
			if char < '0' || char > '9' {
				isNumeric = false
				break
			}
		}
		if isNumeric {
			continue
		}

		if strings.HasSuffix(word, "e") && len(word) > 5 {
			word = strings.TrimSuffix(word, "e")
		}
		keywords = append(keywords, word)
	}

	if len(keywords) == 0 {
		for _, word := range queryWords {
			word = strings.Trim(word, ".,!?;:()[]{}'\"")
			if !stopWords[word] && len(word) >= 1 {
				keywords = append(keywords, word)
			}
		}
	}

	return keywords
}

// extractSearchTerm returns a joined string of meaningful keywords
func (s *MCPKnowledgeServer) extractSearchTerm(query string) string {
	keywords := s.extractSearchKeywords(query)
	if len(keywords) == 0 {
		return query
	}
	return strings.Join(keywords, " ")
}

// searchWeaviateGraphQL performs a direct GraphQL query to Weaviate for non-episodic collections
func (s *MCPKnowledgeServer) searchWeaviateGraphQL(ctx context.Context, query, collection string, limit int, vec []float32) (interface{}, error) {

	vectorStr := "["
	for i, v := range vec {
		if i > 0 {
			vectorStr += ","
		}
		vectorStr += fmt.Sprintf("%.6f", v)
	}
	vectorStr += "]"

	requestLimit := limit * 2

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

		searchTerm := s.extractSearchTerm(query)
		log.Printf("🔍 [MCP-KNOWLEDGE] WikipediaArticle search using extracted term: '%s' (original: '%s')", searchTerm, query)

		queryStr = fmt.Sprintf(`{
			Get {
				WikipediaArticle(where: {
					operator: Or,
					operands: [
						{ path: ["title"], operator: Like, valueString: "*%s*" },
						{ path: ["text"], operator: Like, valueString: "*%s*" }
					]
				}, sort: [{path: ["timestamp"], order: desc}], limit: %d) {
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

		maxDistance := 0.60

		keywords := s.extractSearchKeywords(query)
		if len(keywords) == 0 {
			log.Printf("⚠️ [MCP-KNOWLEDGE] No meaningful keywords found in query: '%s' - returning empty results", query)
			return map[string]interface{}{
				"results":    []map[string]interface{}{},
				"count":      0,
				"query":      query,
				"collection": collection,
			}, nil
		}
		log.Printf("🔍 [MCP-KNOWLEDGE] Keywords for filtering: %v", keywords)
		log.Printf("🔍 [MCP-KNOWLEDGE] Filtering with distance <= %.2f", maxDistance)

		for _, item := range collectionData {
			distance := 0.0

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

			if len(keywords) == 0 {
				log.Printf("🔍 [MCP-KNOWLEDGE] Filtered out result (no keywords to match): %v", item["title"])
				continue
			}

			title, _ := item["title"].(string)
			text, _ := item["text"].(string)
			content, _ := item["content"].(string)

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

			if collection == "WikipediaArticle" {
				log.Printf("✅ [MCP-KNOWLEDGE] Including result (BM25, primary keyword '%s' matched in title/text): %v", primaryKeyword, title)
			} else {
				log.Printf("✅ [MCP-KNOWLEDGE] Including result (distance=%.3f, primary keyword '%s' matched in title/text): %v", distance, primaryKeyword, title)
			}
			results = append(results, item)

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

	escapedName := strings.ReplaceAll(name, "'", "\\'")

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

	escapedName := strings.ReplaceAll(conceptName, "'", "\\'")

	cypher := fmt.Sprintf(`
		MATCH path = (c:Concept {name: '%s'})-[*1..%d]-(related:Concept)
		RETURN DISTINCT related, length(path) as depth
		ORDER BY depth
		LIMIT 20
	`, escapedName, maxDepth)

	return s.queryViaHDN(ctx, cypher)
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

	embedding, err := s.getOllamaEmbedding(ctx, content)
	if err != nil {
		return nil, fmt.Errorf("failed to get embedding for storage: %w", err)
	}

	weaviateURL := os.Getenv("WEAVIATE_URL")
	if weaviateURL == "" {
		weaviateURL = "http://localhost:8080"
	}

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

	embedding, err := s.getOllamaEmbedding(ctx, text)
	if err != nil {
		return nil, fmt.Errorf("failed to get embedding for storage: %w", err)
	}

	weaviateURL := os.Getenv("WEAVIATE_URL")
	if weaviateURL == "" {
		weaviateURL = "http://localhost:8080"
	}

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
