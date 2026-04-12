package main

import (
	"context"
	"encoding/json"
	"fmt"
	"hdn/conversational"
	"hdn/interpreter"
	"log"
	"net/http"
	"strings"
	"time"
)

// ConversationalLLMAdapter adapts the existing LLMClient to the conversational layer interface
type ConversationalLLMAdapter struct {
	client *LLMClient
}

// SimpleChatFSM provides basic FSM interface for chat
type SimpleChatFSM struct{}

// SimpleChatHDN provides basic HDN interface for chat
type SimpleChatHDN struct{ server *APIServer }

// SimpleChatLLM provides basic LLM interface for chat
type SimpleChatLLM struct{}

// initializeConversationalLayer initializes the conversational AI layer
func (s *APIServer) initializeConversationalLayer() {
	if s.llmClient == nil {
		log.Printf("⚠️ [API] LLM client not available, skipping conversational layer initialization")
		return
	}

	// Singleton pattern: don't re-initialize if already exists (avoids re-registering routes)
	if s.conversationalAPI != nil {
		log.Printf("💬 [API] Conversational interface already initialized, skipping")
		return
	}

	llmAdapter := &ConversationalLLMAdapter{client: s.llmClient}

	s.conversationalLayer = conversational.NewConversationalLayer(
		&SimpleChatFSM{},
		&SimpleChatHDN{server: s},
		s.redis,
		llmAdapter,
	)

	s.conversationalAPI = conversational.NewConversationalAPI(s.conversationalLayer)

	s.conversationalAPI.SetSlotAcquisition(s.acquireExecutionSlot)

	log.Printf("💬 [API] Conversational interface initialized with real LLM")
}

// RegisterConversationalRoutes registers proxy routes that wait for the layer to be initialized
func (s *APIServer) RegisterConversationalRoutes() {
	// Main conversational endpoint
	s.router.HandleFunc("/api/v1/chat", s.handleChat).Methods("POST", "OPTIONS")
	s.router.HandleFunc("/api/v1/chat/stream", s.handleChatStream).Methods("POST", "OPTIONS")

	// Conversation management
	s.router.HandleFunc("/api/v1/chat/sessions/{sessionId}/history", s.handleGetHistory).Methods("GET", "OPTIONS")
	s.router.HandleFunc("/api/v1/chat/sessions/{sessionId}/summary", s.handleGetSessionSummary).Methods("GET", "OPTIONS")
	s.router.HandleFunc("/api/v1/chat/sessions/{sessionId}/clear", s.handleClearSession).Methods("DELETE", "OPTIONS")

	// Session management
	s.router.HandleFunc("/api/v1/chat/sessions", s.handleListSessions).Methods("GET", "OPTIONS")

	// Simple text-only chat endpoint
	s.router.HandleFunc("/api/v1/chat/text", s.handleChatText).Methods("POST", "OPTIONS")

	// Health check
	s.router.HandleFunc("/api/v1/chat/health", s.handleChatHealth).Methods("GET", "OPTIONS")

	log.Printf("✅ [API] Conversational proxy routes registered")
}

// Proxy handlers that check for initialization
func (s *APIServer) handleChat(w http.ResponseWriter, r *http.Request) {
	if s.conversationalAPI == nil {
		http.Error(w, "Conversational API not yet initialized (waiting for LLM client)", http.StatusServiceUnavailable)
		return
	}
	s.conversationalAPI.HandleChat(w, r)
}

func (s *APIServer) handleChatStream(w http.ResponseWriter, r *http.Request) {
	if s.conversationalAPI == nil {
		http.Error(w, "Conversational API not yet initialized", http.StatusServiceUnavailable)
		return
	}
	s.conversationalAPI.HandleChatStream(w, r)
}

func (s *APIServer) handleGetHistory(w http.ResponseWriter, r *http.Request) {
	if s.conversationalAPI == nil {
		http.Error(w, "Conversational API not yet initialized", http.StatusServiceUnavailable)
		return
	}
	s.conversationalAPI.HandleGetHistory(w, r)
}

func (s *APIServer) handleGetSessionSummary(w http.ResponseWriter, r *http.Request) {
	if s.conversationalAPI == nil {
		http.Error(w, "Conversational API not yet initialized", http.StatusServiceUnavailable)
		return
	}
	s.conversationalAPI.HandleGetSessionSummary(w, r)
}

func (s *APIServer) handleClearSession(w http.ResponseWriter, r *http.Request) {
	if s.conversationalAPI == nil {
		http.Error(w, "Conversational API not yet initialized", http.StatusServiceUnavailable)
		return
	}
	s.conversationalAPI.HandleClearSession(w, r)
}

func (s *APIServer) handleListSessions(w http.ResponseWriter, r *http.Request) {
	if s.conversationalAPI == nil {
		http.Error(w, "Conversational API not yet initialized", http.StatusServiceUnavailable)
		return
	}
	s.conversationalAPI.HandleListSessions(w, r)
}

func (s *APIServer) handleChatText(w http.ResponseWriter, r *http.Request) {
	if s.conversationalAPI == nil {
		http.Error(w, "Conversational API not yet initialized", http.StatusServiceUnavailable)
		return
	}
	s.conversationalAPI.HandleChatText(w, r)
}

func (s *APIServer) handleChatHealth(w http.ResponseWriter, r *http.Request) {
	if s.conversationalAPI == nil {
		http.Error(w, "Conversational API not yet initialized", http.StatusServiceUnavailable)
		return
	}
	s.conversationalAPI.HandleHealth(w, r)
}

// GenerateResponse implements the conversational LLMClientInterface
// Uses HIGH priority for user-facing chat requests
func (a *ConversationalLLMAdapter) GenerateResponse(ctx context.Context, prompt string, maxTokens int) (string, error) {
	ctx = WithComponent(ctx, "hdn-conversational")
	return a.client.callLLMWithContextAndPriority(ctx, prompt, PriorityHigh)
}

// ClassifyText implements the conversational LLMClientInterface
// Uses HIGH priority for user-facing chat requests
func (a *ConversationalLLMAdapter) ClassifyText(ctx context.Context, text string, categories []string) (string, float64, error) {

	prompt := fmt.Sprintf("Classify the following text into one of these categories: %s\n\nText: %s\n\nCategory:", strings.Join(categories, ", "), text)
	ctx = WithComponent(ctx, "hdn-conversational")
	response, err := a.client.callLLMWithContextAndPriority(ctx, prompt, PriorityHigh)
	if err != nil {
		return "", 0.0, err
	}

	response = strings.ToLower(strings.TrimSpace(response))
	bestMatch := ""
	bestScore := 0.0

	for _, category := range categories {
		if strings.Contains(response, strings.ToLower(category)) {
			bestMatch = category
			bestScore = 0.8
			break
		}
	}

	if bestMatch == "" {
		bestMatch = categories[0]
		bestScore = 0.3
	}

	return bestMatch, bestScore, nil
}

// ExtractEntities implements the conversational LLMClientInterface
// Uses HIGH priority for user-facing chat requests
func (a *ConversationalLLMAdapter) ExtractEntities(ctx context.Context, text string, entityTypes []string) (map[string]string, error) {

	prompt := fmt.Sprintf("Extract entities from the following text. Look for: %s\n\nText: %s\n\nReturn as JSON with entity type as key and value as the extracted text.", strings.Join(entityTypes, ", "), text)
	ctx = WithComponent(ctx, "hdn-conversational")
	response, err := a.client.callLLMWithContextAndPriority(ctx, prompt, PriorityHigh)
	if err != nil {
		return make(map[string]string), err
	}

	// Try to parse as JSON, fallback to simple extraction
	var entities map[string]string
	if err := json.Unmarshal([]byte(response), &entities); err != nil {

		entities = make(map[string]string)
		for _, entityType := range entityTypes {
			if strings.Contains(strings.ToLower(text), strings.ToLower(entityType)) {
				entities[entityType] = text
			}
		}
	}

	return entities, nil
}

func (f *SimpleChatFSM) GetCurrentState() string { return "chat_ready" }

func (f *SimpleChatFSM) GetContext() map[string]interface{} {
	return map[string]interface{}{"mode": "chat", "timestamp": time.Now()}
}

func (f *SimpleChatFSM) TriggerEvent(eventName string, eventData map[string]interface{}) error {
	return nil
}

func (f *SimpleChatFSM) IsHealthy() bool { return true }

func (h *SimpleChatHDN) ExecuteTask(ctx context.Context, task string, ctxMap map[string]string) (*conversational.TaskResult, error) {

	state := State{
		task: true,
	}

	result := h.server.planTask(state, task)

	if len(result) == 0 {
		log.Printf("⚠️ [SIMPLE-CHAT-HDN] No symbolic plan found for task, falling back to flexible interpretation: %s", task)
		ir, err := h.InterpretNaturalLanguage(ctx, task, ctxMap)
		if err != nil {
			return nil, err
		}

		return &conversational.TaskResult{
			Success:  ir.Success,
			Result:   ir.Interpreted,
			Metadata: ir.Metadata,
		}, nil
	}

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

func (h *SimpleChatHDN) PlanTask(ctx context.Context, task string, ctxMap map[string]string) (*conversational.PlanResult, error) {
	return &conversational.PlanResult{
		Success: true,
		Plan:    []string{task},
		Metadata: map[string]interface{}{
			"planned_at": time.Now(),
		},
	}, nil
}

func (h *SimpleChatHDN) LearnFromLLM(ctx context.Context, input string, ctxMap map[string]string) (*conversational.LearnResult, error) {
	return &conversational.LearnResult{
		Success: true,
		Learned: fmt.Sprintf("Learned from: %s", input),
		Metadata: map[string]interface{}{
			"learned_at": time.Now(),
		},
	}, nil
}

func (h *SimpleChatHDN) SaveEpisode(ctx context.Context, text string, metadata map[string]interface{}) error {
	log.Printf("📥 [SIMPLE-CHAT-HDN] SaveEpisode called")
	if h.server == nil || h.server.mcpKnowledgeServer == nil {
		return fmt.Errorf("knowledge server not available")
	}

	if metadata != nil && metadata["type"] == "personal_context" {
		log.Printf("👤 [SIMPLE-CHAT-HDN] Routing personal fact to AvatarContext")
		args := map[string]interface{}{
			"content": text,
			"source":  "conversational_learning",
		}
		_, err := h.server.mcpKnowledgeServer.saveAvatarContext(ctx, args)
		return err
	}

	args := map[string]interface{}{
		"text":     text,
		"metadata": metadata,
	}

	_, err := h.server.mcpKnowledgeServer.saveEpisode(ctx, args)
	return err
}

func (h *SimpleChatHDN) InterpretNaturalLanguage(ctx context.Context, input string, ctxMap map[string]string) (*conversational.InterpretResult, error) {
	log.Printf("🔍 [SIMPLE-CHAT-HDN] InterpretNaturalLanguage called with input: %s", input)

	if h.server == nil || h.server.interpreter == nil {
		log.Printf("⚠️ [SIMPLE-CHAT-HDN] Server or interpreter not available, using fallback")

		return &conversational.InterpretResult{
			Success:     true,
			Interpreted: fmt.Sprintf("Interpreted: %s", input),
			Metadata: map[string]interface{}{
				"interpreted_at": time.Now(),
			},
		}, nil
	}

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

	sessionID, _ := ctxMap["session_id"]
	if sessionID == "" {
		sessionID = fmt.Sprintf("conv_%d", time.Now().UnixNano())
	}

	req := interpreter.NaturalLanguageRequest{
		Input:     input,
		Context:   ctxMap,
		SessionID: sessionID,
	}

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

	metadata := map[string]interface{}{
		"interpreted_at": time.Now(),
	}

	if result.ToolExecutionResult != nil && result.ToolExecutionResult.Success {
		toolResult := map[string]interface{}{
			"success": true,
		}

		// stripLargeFields recursively removes HTML/binary/cookie fields that blow up LLM prompts
		var stripLargeFieldRecursive func(interface{}) interface{}
		stripLargeFieldRecursive = func(input interface{}) interface{} {
			if input == nil {
				return nil
			}
			switch v := input.(type) {
			case string:

				if len(v) > 10000 {
					return v[:10000] + "... [TRUNCATED]"
				}
				return v
			case map[string]interface{}:
				clean := make(map[string]interface{})
				for mk, mv := range v {
					switch mk {
					case "cleaned_html", "raw_html", "screenshot", "cookies", "response", "full_content":
						// Skip completely — these are almost always too large and lack high-value semantic content for NLG
						continue
					case "body", "text", "html", "content", "snippet":
						// Smarter truncation: keep a prefix for context but drop the massive bulk
						val := fmt.Sprintf("%v", mv)
						if len(val) > 500 {
							clean[mk] = val[:500] + "... [TRUNCATED]"
						} else {
							clean[mk] = val
						}
					default:
						clean[mk] = stripLargeFieldRecursive(mv)
					}
				}
				return clean
			case []interface{}:
				clean := make([]interface{}, len(v))
				for i, item := range v {
					clean[i] = stripLargeFieldRecursive(item)
				}
				return clean
			default:
				return v
			}
		}
		stripLargeFields := func(m map[string]interface{}) map[string]interface{} {
			res := stripLargeFieldRecursive(m)
			if resMap, ok := res.(map[string]interface{}); ok {
				return resMap
			}
			return m
		}

		if result.ToolExecutionResult.Result != nil {

			if resultMap, ok := result.ToolExecutionResult.Result.(map[string]interface{}); ok {

				resultMap = stripLargeFields(resultMap)

				if results, ok := resultMap["results"].([]interface{}); ok {
					if len(results) > 10 {
						log.Printf("✂️ [API] Truncating tool results from %d to 10 items", len(results))
						toolResult["results"] = results[:10]
					} else {
						toolResult["results"] = results
					}
				} else if results, ok := resultMap["results"].([]map[string]interface{}); ok {

					if len(results) > 10 {
						log.Printf("✂️ [API] Truncating tool results from %d to 10 items", len(results))
						toolResult["results"] = results[:10]
					} else {
						toolResult["results"] = results
					}
				} else if _, hasResults := resultMap["results"]; hasResults {
					toolResult["results"] = resultMap["results"]
				} else {

					if _, hasSubject := resultMap["Subject"]; hasSubject {

						toolResult["results"] = []interface{}{resultMap}
					} else {

						toolResult["results"] = []interface{}{resultMap}
					}
				}
			} else if resultSlice, ok := result.ToolExecutionResult.Result.([]interface{}); ok {

				if len(resultSlice) > 10 {
					log.Printf("✂️ [API] Truncating tool result slice from %d to 10 items", len(resultSlice))
					resultSlice = resultSlice[:10]
				}

				for i, item := range resultSlice {
					if itemMap, ok := item.(map[string]interface{}); ok {
						resultSlice[i] = stripLargeFields(itemMap)
					}
				}
				toolResult["results"] = resultSlice
			} else {

				toolResult["results"] = []interface{}{result.ToolExecutionResult.Result}
			}
		}

		metadata["tool_result"] = toolResult
		metadata["tool_used"] = result.ToolCall.ToolID

		if results, ok := toolResult["results"]; ok {
			if resultsArray, ok := results.([]interface{}); ok {
				log.Printf("🔧 [SIMPLE-CHAT-HDN] Added tool_result to metadata for tool: %s (with %d results)", result.ToolCall.ToolID, len(resultsArray))
			} else {
				log.Printf("🔧 [SIMPLE-CHAT-HDN] Added tool_result to metadata for tool: %s (results type: %T)", result.ToolCall.ToolID, results)
			}
		} else {
			log.Printf("⚠️ [SIMPLE-CHAT-HDN] Added tool_result to metadata for tool: %s (NO RESULTS KEY!)", result.ToolCall.ToolID)
		}
	}

	metadata["response_type"] = string(result.ResponseType)

	if result.ToolCall != nil {

		if _, exists := metadata["tool_used"]; !exists {
			metadata["tool_used"] = result.ToolCall.ToolID
		}
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

	interpretedText := result.Message
	if result.ToolExecutionResult != nil && result.ToolExecutionResult.Success {

		formattedResult := formatToolResult(result.ToolExecutionResult.Result)
		if formattedResult != "" {
			interpretedText = fmt.Sprintf("%s\n\n%s", interpretedText, formattedResult)
		}
	}

	return &conversational.InterpretResult{
		Success:     result.Success,
		Interpreted: interpretedText,
		Metadata:    metadata,
	}, nil
}

func (h *SimpleChatHDN) SearchWeaviate(ctx context.Context, query string, collection string, limit int) (*conversational.InterpretResult, error) {
	log.Printf("🔍 [SIMPLE-CHAT-HDN] SearchWeaviate called for query='%s', collection='%s'", query, collection)
	if h.server == nil || h.server.mcpKnowledgeServer == nil {
		log.Printf("⚠️ [SIMPLE-CHAT-HDN] SearchWeaviate failed: server or mcpKnowledgeServer is nil")
		return nil, fmt.Errorf("knowledge server not available")
	}

	args := map[string]interface{}{
		"query":      query,
		"collection": collection,
		"limit":      float64(limit),
	}

	result, err := h.server.mcpKnowledgeServer.searchWeaviate(ctx, args)
	if err != nil {
		return nil, err
	}

	return &conversational.InterpretResult{
		Success:     true,
		Interpreted: fmt.Sprintf("Search results for %s in %s", query, collection),
		Metadata: map[string]interface{}{
			"tool_success": true,
			"tool_result":  result,
			"tool_used":    "mcp_search_weaviate",
		},
	}, nil
}

func (l *SimpleChatLLM) GenerateResponse(ctx context.Context, prompt string, maxTokens int) (string, error) {
	prompt = strings.ToLower(strings.TrimSpace(prompt))

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
