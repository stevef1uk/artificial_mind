package handlers

import (
	"encoding/json"
	"net/http"
)

// APIServer represents the main API server
type APIServer struct {
	// Core dependencies will be injected
	Handlers map[string]HandlerGroup
}

// HandlerGroup represents a group of related handlers
type HandlerGroup interface {
	RegisterRoutes(router interface{})
}

// BaseHandler provides common functionality for all handlers
type BaseHandler struct {
	Server *APIServer
}

// writeJSONResponse writes a JSON response
func (h *BaseHandler) writeJSONResponse(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(data)
}

// writeErrorResponse writes an error response
func (h *BaseHandler) writeErrorResponse(w http.ResponseWriter, message string, statusCode int) {
	h.writeJSONResponse(w, statusCode, map[string]interface{}{
		"success": false,
		"error":   message,
		"code":    statusCode,
	})
}

// writeSuccessResponse writes a success response
func (h *BaseHandler) writeSuccessResponse(w http.ResponseWriter, data interface{}) {
	h.writeJSONResponse(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"data":    data,
	})
}
