package main

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"
)

// handleListAgents: GET /api/v1/agents
func (s *APIServer) handleListAgents(w http.ResponseWriter, r *http.Request) {
	if s.agentRegistry == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error": "agent registry not available",
		})
		return
	}

	agentIDs := s.agentRegistry.ListAgents()
	agents := make([]map[string]interface{}, 0, len(agentIDs))

	for _, id := range agentIDs {
		agent, ok := s.agentRegistry.GetAgent(id)
		if !ok {
			continue
		}
		agents = append(agents, map[string]interface{}{
			"id":          agent.Config.ID,
			"name":        agent.Config.Name,
			"description": agent.Config.Description,
			"role":        agent.Config.Role,
			"goal":        agent.Config.Goal,
			"tools":       agent.Config.Tools,
		})
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"agents": agents,
		"count":  len(agents),
	})
}

// handleGetAgent: GET /api/v1/agents/{id}
func (s *APIServer) handleGetAgent(w http.ResponseWriter, r *http.Request) {
	if s.agentRegistry == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error": "agent registry not available",
		})
		return
	}

	vars := mux.Vars(r)
	agentID := vars["id"]

	agent, ok := s.agentRegistry.GetAgent(agentID)
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error": "agent not found",
		})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"id":          agent.Config.ID,
		"name":        agent.Config.Name,
		"description": agent.Config.Description,
		"role":        agent.Config.Role,
		"goal":        agent.Config.Goal,
		"backstory":   agent.Config.Backstory,
		"tools":       agent.Config.Tools,
		"capabilities": map[string]interface{}{
			"max_iterations":  agent.Config.Capabilities.MaxIterations,
			"allow_delegation": agent.Config.Capabilities.AllowDelegation,
			"verbose":          agent.Config.Capabilities.Verbose,
		},
		"triggers": agent.Config.Triggers,
		"behavior": agent.Config.Behavior,
		"tasks":    agent.Config.Tasks,
	})
}

// handleListCrews: GET /api/v1/crews
func (s *APIServer) handleListCrews(w http.ResponseWriter, r *http.Request) {
	if s.agentRegistry == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error": "agent registry not available",
		})
		return
	}

	crewIDs := s.agentRegistry.ListCrews()
	crews := make([]map[string]interface{}, 0, len(crewIDs))

	for _, id := range crewIDs {
		crew, ok := s.agentRegistry.GetCrew(id)
		if !ok {
			continue
		}
		agentNames := make([]string, len(crew.Agents))
		for i, agent := range crew.Agents {
			agentNames[i] = agent.Config.Name
		}
		crews = append(crews, map[string]interface{}{
			"id":          crew.Config.ID,
			"name":        crew.Config.Name,
			"description": crew.Config.Description,
			"agents":      agentNames,
		})
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"crews": crews,
		"count": len(crews),
	})
}

// handleExecuteAgent: POST /api/v1/agents/{id}/execute
func (s *APIServer) handleExecuteAgent(w http.ResponseWriter, r *http.Request) {
	if s.agentRegistry == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error": "agent registry not available",
		})
		return
	}

	vars := mux.Vars(r)
	agentID := vars["id"]

	// Parse request body
	var req struct {
		Input string `json:"input"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error": "invalid request body",
		})
		return
	}

	if req.Input == "" {
		req.Input = "Execute agent tasks" // Default input
	}

	// Create executor and execute agent
	executor := NewAgentExecutor(s.agentRegistry)
	result, err := executor.ExecuteAgent(r.Context(), agentID, req.Input)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error": err.Error(),
		})
		return
	}

	_ = json.NewEncoder(w).Encode(result)
}

