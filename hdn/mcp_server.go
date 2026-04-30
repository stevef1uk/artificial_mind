package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

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

// GetAllPromptHints returns all prompt hints from hardcoded tools and configured skills
func (s *MCPKnowledgeServer) GetAllPromptHints() map[string]*PromptHintsConfig {
	hints := make(map[string]*PromptHintsConfig)

	hints["search_weaviate"] = &PromptHintsConfig{
		Keywords:      []string{"news", "latest news", "world events", "current events", "update on", "what is happening in", "situation in", "happenings", "current affairs"},
		PromptText:    "⚠️ FOR NEWS QUERIES: You MUST use mcp_search_weaviate with collection='WikipediaArticle'. This is the ONLY tool with real-time news access. DO NOT use mcp_get_concept for news.",
		ForceToolCall: true,
		AlwaysInclude: []string{"news", "latest", "update", "happening", "situation in"},
		RejectText:    true,
	}

	hints["search_avatar_context"] = &PromptHintsConfig{
		Keywords:   []string{"who am i", "my name", "my work", "worked at", "my skills", "my project"},
		PromptText: "FOR PERSONAL QUESTIONS: Use mcp_search_avatar_context to find information about Steven Fisher.",
	}

	if s.skillRegistry == nil {
		return hints
	}

	skillHints := s.skillRegistry.GetAllPromptHints()
	for k, v := range skillHints {
		hints[k] = v
	}

	hints["deep_research"] = &PromptHintsConfig{
		Keywords:      []string{"research", "deep research", "comprehensive", "analysis", "latest developments", "multi-step research"},
		PromptText:    "⚠️ FOR COMPLEX RESEARCH: Use deep_research for multi-step research or deep analysis tasks. Set 'topic' to a detailed research goal and 'depth' (1-3).",
		ForceToolCall: true,
		AlwaysInclude: []string{"research", "deep research", "analysis"},
		RejectText:    true,
	}

	hints["smart_scrape"] = &PromptHintsConfig{
		Keywords:      []string{"scrape", "browse", "crawl", "fetch", "visit"},
		PromptText:    "⚠️ FOR WEB SCRAPING: You MUST use smart_scrape with the 'url' and 'goal' parameters. Do NOT return a text description of how to scrape.",
		ForceToolCall: true,
		AlwaysInclude: []string{"scrape"},
		RejectText:    true,
	}

	hints["picoclaw_query"] = &PromptHintsConfig{
		Keywords:      []string{"picoclaw", "pico", "reasoning", "strategic", "local agent"},
		PromptText:    "⚠️ FOR STRATEGIC/REASONING REQUESTS: Use mcp_picoclaw_query for autonomous planning and multi-step reasoning via the local RPi agent.",
		ForceToolCall: true,
		AlwaysInclude: []string{"picoclaw"},
	}

	hints["nemoclaw_query"] = &PromptHintsConfig{
		Keywords:      []string{"nemoclaw", "nemo", "complex reasoning", "strategic planning"},
		PromptText:    "⚠️ FOR COMPLEX REASONING: Use mcp_nemoclaw_query for strategic planning or tasks requiring high reasoning depth. It queries a powerful remote reasoning agent. Responses can take up to 3 minutes.",
		ForceToolCall: true,
		AlwaysInclude: []string{"nemoclaw"},
	}

	hints["search_flights"] = &PromptHintsConfig{
		Keywords:      []string{"flight", "airline", "airport", "booking", "travel"},
		PromptText:    "✅ FOR FLIGHT RESULTS: When presenting results, ALWAYS list at least the top 5 cheapest options with their specific times and airlines. Use a bulleted list. Do NOT just summarize with a range.",
		ForceToolCall: false,
	}
	return hints
}

// HandleRequest handles an MCP JSON-RPC request
// HandleRequest handles an MCP JSON-RPC request and supports SSE handshake
func (s *MCPKnowledgeServer) HandleRequest(w http.ResponseWriter, r *http.Request) {

	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With, Accept")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	if r.Header.Get("Accept") == "text/event-stream" || r.URL.Query().Get("sse") == "true" || strings.HasSuffix(r.URL.Path, "/sse") {
		s.HandleSSESession(w, r)
		return
	}

	if r.Method == http.MethodGet {
		result, err := s.listTools()
		if err != nil {
			s.sendError(w, nil, -32603, "Internal error", err)
			return
		}

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

		result = map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]interface{}{
				"tools": map[string]interface{}{},
			},
			"serverInfo": map[string]string{
				"name":    "hdn-server",
				"version": "1.0.0",
			},
		}
	case "notifications/initialized":

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

// HandleSSESession establishes an MCP SSE session
// It sends the endpoint URL for subsequent POST requests
func (s *MCPKnowledgeServer) HandleSSESession(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With, Accept")
	w.WriteHeader(http.StatusOK)

	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}

	postEndpoint := fmt.Sprintf("%s://%s/mcp", scheme, r.Host)
	if strings.Contains(r.URL.Path, "/api/v1/mcp") {
		postEndpoint = fmt.Sprintf("%s://%s/api/v1/mcp", scheme, r.Host)
	}

	fmt.Fprintf(w, "event: endpoint\n")
	fmt.Fprintf(w, "data: %s\n\n", postEndpoint)
	flusher.Flush()

	notify := r.Context().Done()
	<-notify
	log.Printf("SSE connection closed by client")
}

// listTools returns available knowledge base tools
func (s *MCPKnowledgeServer) listTools() (interface{}, error) {
	tools := []MCPKnowledgeTool{
		{
			Name:        "query_neo4j",
			Description: "[chat-only] Query the Neo4j knowledge graph using Cypher. Use this to find concepts, relationships, and facts stored in the knowledge graph.",
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
			Description: "[chat-only] Search the Weaviate vector database for semantically similar content. Use this to find episodes, memories, or documents by meaning.",
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
						"description": "Collection name to search (options: 'AgiEpisodes', 'AgiWiki', 'WikipediaArticle'). Use 'WikipediaArticle' for latest news and Wikipedia content. (default: 'AgiEpisodes')",
						"default":     "AgiEpisodes",
					},
				},
				"required": []string{"query"},
			},
		},
		{
			Name:        "get_concept",
			Description: "[chat-only] Get a specific concept from the knowledge graph.",
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
			Description: "[chat-only] Find concepts related to a given concept in the Neo4j knowledge graph.",
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
			Description: "[chat-only] Search personal information about Steven Fisher (the user). Use this for questions about his work history, education, skills, projects, or any personal background.",
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
			Description: "[chat-only] Save personal information, preferences, or facts about Steven Fisher (the user) to long-term memory. Use this when the user shares something they want remembered.",
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
		{
			Name:        "deep_research",
			Description: "[chat-only] Perform a multi-step autonomous deep research on a topic. It will search the web, visit multiple sources, capture screenshots, and synthesize a comprehensive report with visual evidence.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"topic": map[string]interface{}{
						"type":        "string",
						"description": "The research topic or question",
					},
					"depth": map[string]interface{}{
						"type":        "integer",
						"description": "Depth of research (1-3). higher depth visits more sources. default: 1",
						"default":     1,
					},
					"session_id": map[string]interface{}{
						"type":        "string",
						"description": "Optional session ID for tracking screenshots in visualizer",
					},
				},
				"required": []string{"topic"},
			},
		},
		{
			Name:        "picoclaw_query",
			Description: "[chat-only] Query the PicoClaw local agentic AI via WebSocket using the Native Pico Protocol. This tool offers real-time bidirectional communication for complex strategic tasks and autonomous reasoning.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"prompt": map[string]interface{}{
						"type":        "string",
						"description": "The complex strategic prompt or question for the PicoClaw agent (also accepts 'query' or 'message')",
					},
					"query": map[string]interface{}{
						"type":        "string",
						"description": "Alternative to 'prompt' for natural language questions",
					},
					"message": map[string]interface{}{
						"type":        "string",
						"description": "Alternative to 'prompt' for conversational greetings",
					},
					"chat_id": map[string]interface{}{
						"type":        "string",
						"description": "Optional: override the target Telegram chat/channel ID (default is the system-configured channel)",
					},
				},
				"required": []string{},
			},
		},
		{
			Name:        "nemoclaw_query",
			Description: "[chat-only] Query the powerful NVIDIA Nemoclaw agentic AI (@nemoclaw_alps2_bot) via Telegram. Use this for extremely complex reasoning, strategic planning, or high-fidelity synthesis. This tool sends your prompt to the Nemoclaw bot and waits for its high-quality reply (can take 30-180 seconds).",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"prompt": map[string]interface{}{
						"type":        "string",
						"description": "The complex strategic prompt or question for the NemoClaw agent (also accepts 'query' or 'message')",
					},
					"query": map[string]interface{}{
						"type":        "string",
						"description": "Alternative to 'prompt' for natural language questions",
					},
					"message": map[string]interface{}{
						"type":        "string",
						"description": "Alternative to 'prompt' for conversational greetings",
					},
					"chat_id": map[string]interface{}{
						"type":        "string",
						"description": "Optional: override the target Telegram chat/channel ID (default is the system-configured channel)",
					},
				},
				"required": []string{},
			},
		},
		{
			Name:        "research_agent",
			Description: "[chat-only] Perform research using the external MCP research server. This tool is restricted to chat access only and will be skipped by the planner.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"topic": map[string]interface{}{
						"type":        "string",
						"description": "The research topic or question",
					},
					"depth": map[string]interface{}{
						"type":        "integer",
						"description": "Depth of research (1-3)",
						"default":     1,
					},
				},
				"required": []string{"topic"},
			},
		},
		{
			Name:        "weather",
			Description: "[chat-only] Fetch weather for anywhere in Europe using Open-Meteo. Defaults to Thonon-les-Bains area.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"lat": map[string]interface{}{
						"type":        "number",
						"description": "Latitude (default: 46.2836)",
					},
					"lon": map[string]interface{}{
						"type":        "number",
						"description": "Longitude (default: 6.6444)",
					},
					"tz": map[string]interface{}{
						"type":        "string",
						"description": "Timezone (e.g. 'Europe/Berlin')",
					},
				},
			},
		},
		{
			Name:        "secret_scanner",
			Description: "Scan a file or text for exposed API keys, secrets, and credentials. Supports common formats like OpenAI, AWS, GCP, GitHub, etc.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Optional: Path to the file to scan. If omitted, you must provide text via stdin.",
					},
					"text": map[string]interface{}{
						"type":        "string",
						"description": "Optional: Raw text content to scan if path is not provided.",
					},
				},
			},
		},
	}

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

	if s.skillRegistry != nil {
		configuredSkills := s.skillRegistry.ListSkills()

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

// wrapMCPResponse ensures the tool result complies with the MCP specification (requires a 'content' array)
func (s *MCPKnowledgeServer) wrapMCPResponse(result interface{}) interface{} {
	if result == nil {
		return map[string]interface{}{
			"content": []map[string]interface{}{
				{"type": "text", "text": "Success (no data returned)"},
			},
		}
	}

	if resMap, ok := result.(map[string]interface{}); ok {
		if _, hasContent := resMap["content"]; hasContent {
			return result
		}
	}

	if str, ok := result.(string); ok {
		return map[string]interface{}{
			"content": []map[string]interface{}{
				{"type": "text", "text": str},
			},
		}
	}

	jsonData, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return map[string]interface{}{
			"content": []map[string]interface{}{
				{"type": "text", "text": fmt.Sprintf("Error serializing result: %v", err)},
			},
			"isError": true,
		}
	}

	return map[string]interface{}{
		"content": []map[string]interface{}{
			{"type": "text", "text": string(jsonData)},
		},
	}
}

// callTool executes an MCP tool
func (s *MCPKnowledgeServer) callTool(ctx context.Context, toolName string, arguments map[string]interface{}) (interface{}, error) {
	startTime := time.Now()

	toolName = strings.TrimPrefix(toolName, "mcp_")

	if arguments != nil {
		if inner, ok := arguments["arguments"].(map[string]interface{}); ok {
			arguments = inner
			log.Printf("📥 [MCP-KNOWLEDGE] Unwrapped nested arguments for tool: %s", toolName)
		}
	}

	var result interface{}
	var err error

	defer func() {
		if s.toolMetrics != nil {
			status := "success"
			errorMsg := ""
			if err != nil {
				status = "failure"
				errorMsg = err.Error()
			}

			toolID := toolName
			if !strings.HasPrefix(toolID, "mcp_") && toolID != "tool_weather" {
				toolID = "mcp_" + toolID
			}

			metric := &ToolCallLog{
				ToolID:     toolID,
				Parameters: arguments,
				Response:   result,
				Status:     status,
				Error:      errorMsg,
				Duration:   time.Since(startTime).Milliseconds(),
				Timestamp:  time.Now(),
			}
			s.toolMetrics.LogToolCall(ctx, metric)
		}
	}()

	if s.skillRegistry != nil && s.skillRegistry.HasSkill(toolName) {
		log.Printf("🔧 [MCP-KNOWLEDGE] Executing configured skill: %s", toolName)
		result, err = s.skillRegistry.ExecuteSkill(ctx, toolName, arguments)
	} else {

		switch toolName {
		case "query_neo4j":
			result, err = s.queryNeo4j(ctx, arguments)
		case "search_weaviate":
			result, err = s.searchWeaviate(ctx, arguments)
		case "get_concept":
			result, err = s.getConcept(ctx, arguments)
		case "find_related_concepts":
			result, err = s.findRelatedConcepts(ctx, arguments)
		case "search_avatar_context":
			result, err = s.searchAvatarContext(ctx, arguments)
		case "save_avatar_context":
			result, err = s.saveAvatarContext(ctx, arguments)
		case "save_episode":
			result, err = s.saveEpisode(ctx, arguments)
		case "scrape_url", "execute_code", "read_file", "smart_scrape", "weather":

			result, err = s.executeToolWrapper(ctx, toolName, arguments)
		case "deep_research":
			result, err = s.deepResearch(ctx, arguments)
		case "research_agent":
			result, err = s.researchAgentQuery(ctx, arguments)
		case "picoclaw_query":
			result, err = s.picoclawQuery(ctx, arguments)
		case "nemoclaw_query":
			result, err = s.nemoclawQuery(ctx, arguments)
		case "get_scrape_status":
			jobID, _ := arguments["job_id"].(string)
			if jobID == "" {
				err = fmt.Errorf("job_id parameter required")
			} else {
				result, err = s.getScrapeStatus(ctx, jobID)
			}
		default:
			err = fmt.Errorf("unknown tool: %s", toolName)
		}
	}

	if err != nil {
		return nil, err
	}

	s.extractAndSaveScreenshot(toolName, result)

	return s.wrapMCPResponse(result), nil
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
