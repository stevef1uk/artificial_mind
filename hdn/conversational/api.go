package conversational

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/mux"
)

// ConversationalAPI handles HTTP requests for conversational interactions
type ConversationalAPI struct {
	conversationalLayer *ConversationalLayer
}

// NewConversationalAPI creates a new conversational API handler
func NewConversationalAPI(conversationalLayer *ConversationalLayer) *ConversationalAPI {
	return &ConversationalAPI{
		conversationalLayer: conversationalLayer,
	}
}

// RegisterRoutes registers the conversational API routes
func (api *ConversationalAPI) RegisterRoutes(router *mux.Router) {
	// Main conversational endpoint
	router.HandleFunc("/api/v1/chat", api.handleChat).Methods("POST")
	router.HandleFunc("/api/v1/chat/stream", api.handleChatStream).Methods("POST")

	// Conversation management
	router.HandleFunc("/api/v1/chat/sessions/{sessionId}/history", api.handleGetHistory).Methods("GET")
	router.HandleFunc("/api/v1/chat/sessions/{sessionId}/summary", api.handleGetSessionSummary).Methods("GET")
	router.HandleFunc("/api/v1/chat/sessions/{sessionId}/clear", api.handleClearSession).Methods("DELETE")

	// Reasoning and thinking
	router.HandleFunc("/api/v1/chat/sessions/{sessionId}/thinking", api.handleGetThinking).Methods("GET")
	router.HandleFunc("/api/v1/chat/sessions/{sessionId}/reasoning", api.handleGetReasoning).Methods("GET")
	router.HandleFunc("/api/v1/chat/sessions/{sessionId}/thoughts", api.handleGetThoughts).Methods("GET")
	router.HandleFunc("/api/v1/chat/sessions/{sessionId}/thoughts/stream", api.handleGetThoughtsStream).Methods("GET")
	router.HandleFunc("/api/v1/chat/sessions/{sessionId}/thoughts/express", api.handleExpressThoughts).Methods("POST")

	// Session management
	router.HandleFunc("/api/v1/chat/sessions", api.handleListSessions).Methods("GET")
	router.HandleFunc("/api/v1/chat/sessions/cleanup", api.handleCleanupSessions).Methods("POST")

	// Simple text-only chat endpoint
	router.HandleFunc("/api/v1/chat/text", api.handleChatText).Methods("POST")

	// Health check
	router.HandleFunc("/api/v1/chat/health", api.handleHealth).Methods("GET")
}

// handleChat handles conversational chat requests
func (api *ConversationalAPI) handleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req ConversationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		api.writeErrorResponse(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Validate required fields
	if req.Message == "" {
		api.writeErrorResponse(w, "Message is required", http.StatusBadRequest)
		return
	}

	// Set defaults
	if req.SessionID == "" {
		req.SessionID = fmt.Sprintf("session_%d", time.Now().UnixNano())
	}
	if req.Context == nil {
		req.Context = make(map[string]string)
	}

	// Process the message
	ctx := r.Context()
	response, err := api.conversationalLayer.ProcessMessage(ctx, &req)
	if err != nil {
		log.Printf("❌ [CONVERSATIONAL-API] Failed to process message: %v", err)
		api.writeErrorResponse(w, "Failed to process message", http.StatusInternalServerError)
		return
	}

	// Write response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// handleChatStream handles streaming chat requests
func (api *ConversationalAPI) handleChatStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req ConversationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		api.writeErrorResponse(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Validate required fields
	if req.Message == "" {
		api.writeErrorResponse(w, "Message is required", http.StatusBadRequest)
		return
	}

	// Set defaults
	if req.SessionID == "" {
		req.SessionID = fmt.Sprintf("session_%d", time.Now().UnixNano())
	}
	if req.Context == nil {
		req.Context = make(map[string]string)
	}
	req.StreamMode = true

	// Set up streaming response
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// Send initial connection event
	fmt.Fprintf(w, "data: {\"type\":\"connected\",\"session_id\":\"%s\"}\n\n", req.SessionID)
	w.(http.Flusher).Flush()

	// Process the message with streaming
	ctx := r.Context()
	response, err := api.conversationalLayer.ProcessMessage(ctx, &req)
	if err != nil {
		log.Printf("❌ [CONVERSATIONAL-API] Failed to process streaming message: %v", err)
		fmt.Fprintf(w, "data: {\"type\":\"error\",\"message\":\"Failed to process message\"}\n\n")
		w.(http.Flusher).Flush()
		return
	}

	// Send the response
	responseData, _ := json.Marshal(response)
	fmt.Fprintf(w, "data: {\"type\":\"response\",\"data\":%s}\n\n", string(responseData))
	w.(http.Flusher).Flush()

	// Send completion event
	fmt.Fprintf(w, "data: {\"type\":\"complete\"}\n\n")
	w.(http.Flusher).Flush()
}

// handleGetHistory retrieves conversation history for a session
func (api *ConversationalAPI) handleGetHistory(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	sessionID := vars["sessionId"]
	if sessionID == "" {
		api.writeErrorResponse(w, "Session ID is required", http.StatusBadRequest)
		return
	}

	// Get limit from query parameter
	limit := 50 // Default limit
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	ctx := r.Context()
	history, err := api.conversationalLayer.GetConversationHistory(ctx, sessionID, limit)
	if err != nil {
		log.Printf("❌ [CONVERSATIONAL-API] Failed to get history: %v", err)
		api.writeErrorResponse(w, "Failed to get conversation history", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"session_id": sessionID,
		"history":    history,
		"count":      len(history),
	})
}

// handleGetSessionSummary retrieves session summary
func (api *ConversationalAPI) handleGetSessionSummary(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	sessionID := vars["sessionId"]
	if sessionID == "" {
		api.writeErrorResponse(w, "Session ID is required", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	summary, err := api.conversationalLayer.conversationMemory.GetSessionSummary(ctx, sessionID)
	if err != nil {
		log.Printf("❌ [CONVERSATIONAL-API] Failed to get session summary: %v", err)
		api.writeErrorResponse(w, "Failed to get session summary", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(summary)
}

// handleClearSession clears a conversation session
func (api *ConversationalAPI) handleClearSession(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	sessionID := vars["sessionId"]
	if sessionID == "" {
		api.writeErrorResponse(w, "Session ID is required", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	err := api.conversationalLayer.conversationMemory.ClearSession(ctx, sessionID)
	if err != nil {
		log.Printf("❌ [CONVERSATIONAL-API] Failed to clear session: %v", err)
		api.writeErrorResponse(w, "Failed to clear session", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":    true,
		"session_id": sessionID,
		"message":    "Session cleared successfully",
	})
}

// handleGetThinking retrieves current thinking process
func (api *ConversationalAPI) handleGetThinking(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	sessionID := vars["sessionId"]
	if sessionID == "" {
		api.writeErrorResponse(w, "Session ID is required", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	thinking, err := api.conversationalLayer.GetCurrentThinking(ctx, sessionID)
	if err != nil {
		log.Printf("❌ [CONVERSATIONAL-API] Failed to get thinking: %v", err)
		api.writeErrorResponse(w, "Failed to get thinking process", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(thinking)
}

// handleGetReasoning retrieves reasoning trace
func (api *ConversationalAPI) handleGetReasoning(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	sessionID := vars["sessionId"]
	if sessionID == "" {
		api.writeErrorResponse(w, "Session ID is required", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	reasoning, err := api.conversationalLayer.GetCurrentThinking(ctx, sessionID)
	if err != nil {
		log.Printf("❌ [CONVERSATIONAL-API] Failed to get reasoning: %v", err)
		api.writeErrorResponse(w, "Failed to get reasoning trace", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(reasoning)
}

// handleListSessions lists active conversation sessions
func (api *ConversationalAPI) handleListSessions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sessions, err := api.conversationalLayer.conversationMemory.GetActiveSessions(ctx)
	if err != nil {
		log.Printf("❌ [CONVERSATIONAL-API] Failed to list sessions: %v", err)
		api.writeErrorResponse(w, "Failed to list sessions", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"sessions": sessions,
		"count":    len(sessions),
	})
}

// handleCleanupSessions cleans up old sessions
func (api *ConversationalAPI) handleCleanupSessions(w http.ResponseWriter, r *http.Request) {
	// Get cleanup parameters
	hours := 24 // Default to 24 hours
	if h := r.URL.Query().Get("hours"); h != "" {
		if parsed, err := strconv.Atoi(h); err == nil && parsed > 0 {
			hours = parsed
		}
	}

	ctx := r.Context()
	olderThan := time.Duration(hours) * time.Hour
	err := api.conversationalLayer.conversationMemory.CleanupOldSessions(ctx, olderThan)
	if err != nil {
		log.Printf("❌ [CONVERSATIONAL-API] Failed to cleanup sessions: %v", err)
		api.writeErrorResponse(w, "Failed to cleanup sessions", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":    true,
		"older_than": hours,
		"message":    "Sessions cleaned up successfully",
	})
}

// handleChatText handles simple text-only chat requests
func (api *ConversationalAPI) handleChatText(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req ConversationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Validate required fields
	if req.Message == "" {
		http.Error(w, "Message is required", http.StatusBadRequest)
		return
	}

	// Generate session ID if not provided
	if req.SessionID == "" {
		req.SessionID = fmt.Sprintf("text_chat_%d", time.Now().UnixNano())
	}

	// Process the message
	ctx := r.Context()
	response, err := api.conversationalLayer.ProcessMessage(ctx, &req)
	if err != nil {
		http.Error(w, fmt.Sprintf("Error processing message: %v", err), http.StatusInternalServerError)
		return
	}

	// Return just the text response
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(response.Response))
}

// handleHealth provides health check for conversational API
func (api *ConversationalAPI) handleHealth(w http.ResponseWriter, r *http.Request) {
	// Check if the conversational layer is healthy
	// For now, we'll just return a basic health status
	health := map[string]interface{}{
		"status":    "healthy",
		"timestamp": time.Now().Unix(),
		"service":   "conversational-api",
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(health)
}

// writeErrorResponse writes an error response
func (api *ConversationalAPI) writeErrorResponse(w http.ResponseWriter, message string, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": false,
		"error":   message,
		"code":    statusCode,
	})
}

// handleGetThoughts retrieves thought events for a session
func (api *ConversationalAPI) handleGetThoughts(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	sessionID := vars["sessionId"]

	limitStr := r.URL.Query().Get("limit")
	limit := 50 // default
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	ctx := r.Context()
	thoughts, err := api.conversationalLayer.thoughtExpression.GetRecentThoughts(ctx, sessionID, limit)
	if err != nil {
		log.Printf("❌ [CONVERSATIONAL-API] Failed to get thoughts: %v", err)
		api.writeErrorResponse(w, "Failed to get thoughts", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":    true,
		"thoughts":   thoughts,
		"count":      len(thoughts),
		"session_id": sessionID,
	})
}

// handleGetThoughtsStream streams thought events for a session
func (api *ConversationalAPI) handleGetThoughtsStream(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	sessionID := vars["sessionId"]

	// Set up Server-Sent Events
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// Send initial connection event
	fmt.Fprintf(w, "data: {\"type\":\"connected\",\"session_id\":\"%s\"}\n\n", sessionID)
	w.(http.Flusher).Flush()

	// For now, return recent thoughts (in a real implementation, this would be a WebSocket or SSE stream)
	ctx := r.Context()
	thoughts, err := api.conversationalLayer.thoughtExpression.GetRecentThoughts(ctx, sessionID, 10)
	if err != nil {
		fmt.Fprintf(w, "data: {\"type\":\"error\",\"message\":\"Failed to get thoughts\"}\n\n")
		w.(http.Flusher).Flush()
		return
	}

	// Send each thought as an event
	for _, thought := range thoughts {
		thoughtData, _ := json.Marshal(thought)
		fmt.Fprintf(w, "data: {\"type\":\"thought\",\"data\":%s}\n\n", string(thoughtData))
		w.(http.Flusher).Flush()
	}

	// Send completion event
	fmt.Fprintf(w, "data: {\"type\":\"complete\"}\n\n")
	w.(http.Flusher).Flush()
}

// handleExpressThoughts converts reasoning traces to natural language thoughts
func (api *ConversationalAPI) handleExpressThoughts(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	sessionID := vars["sessionId"]

	var req ThoughtExpressionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		api.writeErrorResponse(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Set session ID from URL
	req.SessionID = sessionID

	// Set defaults
	if req.Style == "" {
		req.Style = "conversational"
	}

	ctx := r.Context()
	response, err := api.conversationalLayer.thoughtExpression.ExpressThoughts(ctx, &req)
	if err != nil {
		log.Printf("❌ [CONVERSATIONAL-API] Failed to express thoughts: %v", err)
		api.writeErrorResponse(w, "Failed to express thoughts", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":  true,
		"response": response,
	})
}
