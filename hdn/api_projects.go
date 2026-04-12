package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
)

func (s *APIServer) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string            `json:"name"`
		Description string            `json:"description"`
		Owner       string            `json:"owner"`
		Tags        []string          `json:"tags"`
		Metadata    map[string]string `json:"metadata"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		http.Error(w, "Project name is required", http.StatusBadRequest)
		return
	}

	if list, err := s.projectManager.ListProjects(); err == nil && list != nil {
		for _, p := range list {
			if strings.EqualFold(strings.TrimSpace(p.Name), strings.TrimSpace(req.Name)) {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(p)
				return
			}
		}
	}

	proj := &Project{
		Name:        req.Name,
		Description: req.Description,
		Owner:       req.Owner,
		Tags:        req.Tags,
		Metadata:    req.Metadata,
	}
	saved, err := s.projectManager.CreateProject(proj)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to create project: %v", err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(saved)
}

func (s *APIServer) handleListProjects(w http.ResponseWriter, r *http.Request) {
	list, err := s.projectManager.ListProjects()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to list projects: %v", err), http.StatusInternalServerError)
		return
	}
	if list == nil {
		list = []*Project{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(list)
}

func (s *APIServer) handleGetProject(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]
	p, err := s.projectManager.GetProject(id)
	if err != nil {
		http.Error(w, fmt.Sprintf("Project not found: %s", id), http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(p)
}

func (s *APIServer) handleUpdateProject(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]
	var req struct {
		Name        *string           `json:"name"`
		Description *string           `json:"description"`
		Status      *string           `json:"status"`
		Owner       *string           `json:"owner"`
		Tags        []string          `json:"tags"`
		NextAction  *string           `json:"next_action"`
		Metadata    map[string]string `json:"metadata"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	updated, err := s.projectManager.UpdateProject(id, func(p *Project) error {
		if req.Name != nil {
			p.Name = *req.Name
		}
		if req.Description != nil {
			p.Description = *req.Description
		}
		if req.Status != nil && *req.Status != "" {
			p.Status = *req.Status
		}
		if req.Owner != nil {
			p.Owner = *req.Owner
		}
		if req.Tags != nil {
			p.Tags = req.Tags
		}
		if req.NextAction != nil {
			p.NextAction = *req.NextAction
		}
		if req.Metadata != nil {
			if p.Metadata == nil {
				p.Metadata = map[string]string{}
			}
			for k, v := range req.Metadata {
				p.Metadata[k] = v
			}
		}
		return nil
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to update project: %v", err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(updated)
}

func (s *APIServer) handleDeleteProject(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]
	if strings.TrimSpace(id) == "" {
		http.Error(w, "Project id required", http.StatusBadRequest)
		return
	}

	if p, err := s.projectManager.GetProject(id); err == nil && p != nil {
		name := strings.ToLower(strings.TrimSpace(p.Name))
		if name == "goals" || name == "fsm-agent-agent_1" {
			http.Error(w, "Deletion of protected project is not allowed", http.StatusForbidden)
			return
		}
	}
	if err := s.projectManager.DeleteProject(id); err != nil {
		http.Error(w, fmt.Sprintf("Failed to delete project: %v", err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"success": true, "id": id})
}

func (s *APIServer) handlePauseProject(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]
	updated, err := s.projectManager.UpdateProject(id, func(p *Project) error {
		p.Status = "paused"
		return nil
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to pause project: %v", err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(updated)
}

func (s *APIServer) handleResumeProject(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]
	updated, err := s.projectManager.UpdateProject(id, func(p *Project) error {
		p.Status = "active"
		return nil
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to resume project: %v", err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(updated)
}

func (s *APIServer) handleArchiveProject(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]
	updated, err := s.projectManager.UpdateProject(id, func(p *Project) error {
		p.Status = "archived"
		return nil
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to archive project: %v", err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(updated)
}

func (s *APIServer) handleAddProjectCheckpoint(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]
	var req struct {
		Summary    string                 `json:"summary"`
		NextAction string                 `json:"next_action"`
		Context    map[string]interface{} `json:"context"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	cp, err := s.projectManager.AddCheckpoint(id, &ProjectCheckpoint{
		Summary:    req.Summary,
		NextAction: req.NextAction,
		Context:    req.Context,
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to add checkpoint: %v", err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(cp)
}

func (s *APIServer) handleListProjectCheckpoints(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]
	cps, err := s.projectManager.ListCheckpoints(id, 50)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to list checkpoints: %v", err), http.StatusInternalServerError)
		return
	}
	if cps == nil {
		cps = []*ProjectCheckpoint{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(cps)
}

func (s *APIServer) handleListProjectWorkflows(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]
	ids, err := s.projectManager.ListWorkflowIDs(id)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to list project workflows: %v", err), http.StatusInternalServerError)
		return
	}
	if ids == nil {
		ids = []string{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"workflow_ids": ids,
	})
}

// ensureProjectByName creates a project if a project with the same name does not already exist
func (s *APIServer) ensureProjectByName(name string) {
	safe := strings.TrimSpace(name)
	if safe == "" {
		return
	}
	if list, err := s.projectManager.ListProjects(); err == nil && list != nil {
		for _, p := range list {
			if strings.EqualFold(strings.TrimSpace(p.Name), safe) {
				return
			}
		}
	}

	_, _ = s.projectManager.CreateProject(&Project{
		Name:        safe,
		Description: "Auto-created for executions",
		Status:      "active",
	})
}

// resolveProjectID returns a real project ID when given an ID or a name.
// If the input matches a project's ID, it is returned as-is.
// If it matches a project's Name (case-insensitive), the project's ID is returned.
// If no match is found, a new project is created with that name and its ID returned.
func (s *APIServer) resolveProjectID(idOrName string) string {
	candidate := strings.TrimSpace(idOrName)
	if candidate == "" {
		return ""
	}

	if list, err := s.projectManager.ListProjects(); err == nil && list != nil {
		for _, p := range list {
			if strings.TrimSpace(p.ID) == candidate {
				return p.ID
			}
		}

		for _, p := range list {
			if strings.EqualFold(strings.TrimSpace(p.Name), candidate) {
				return p.ID
			}
		}
	}

	proj, err := s.projectManager.CreateProject(&Project{
		Name:        candidate,
		Description: "Auto-created for executions",
		Status:      "active",
	})
	if err != nil || proj == nil || strings.TrimSpace(proj.ID) == "" {
		return candidate
	}
	return proj.ID
}
