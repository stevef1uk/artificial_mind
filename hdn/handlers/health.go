package handlers

import (
	"net/http"
	"time"
)

// HealthHandler handles health check endpoints
type HealthHandler struct {
	BaseHandler
}

// NewHealthHandler creates a new health handler
func NewHealthHandler(server *APIServer) *HealthHandler {
	return &HealthHandler{
		BaseHandler: BaseHandler{Server: server},
	}
}

// RegisterRoutes registers health check routes
func (h *HealthHandler) RegisterRoutes(router interface{}) {
	// This will be implemented by the specific router type
}

// HandleHealth handles health check requests
func (h *HealthHandler) HandleHealth(w http.ResponseWriter, r *http.Request) {
	health := map[string]interface{}{
		"status":    "healthy",
		"timestamp": time.Now().Unix(),
		"service":   "hdn-api",
		"version":   "1.0.0",
	}

	h.writeJSONResponse(w, http.StatusOK, health)
}
