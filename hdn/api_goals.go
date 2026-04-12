package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/gorilla/mux"
)

// handleUpdateGoalStatus updates the status of a goal in the self-model
func (s *APIServer) handleUpdateGoalStatus(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	goalID := vars["id"]

	var req struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if s.selfModelManager != nil {
		if err := s.selfModelManager.UpdateGoalStatus(goalID, req.Status); err != nil {
			http.Error(w, fmt.Sprintf("Failed to update goal status: %v", err), http.StatusInternalServerError)
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Goal status updated",
		"goal_id": goalID,
		"status":  req.Status,
	})
}

// handleDeleteSelfModelGoal deletes a goal from the self-model
func (s *APIServer) handleDeleteSelfModelGoal(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	goalID := vars["id"]
	if s.selfModelManager == nil {
		http.Error(w, "self model not configured", http.StatusBadRequest)
		return
	}
	if err := s.selfModelManager.DeleteGoal(goalID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"success": true, "goal_id": goalID})
}

// handleCleanupSelfModelGoals deletes self-model goals matching internal patterns
// Request JSON: { "patterns": ["^Execute task: Goal Execution$", "^Execute task: artifact_task$", "^Execute task: code_.*" ] }
func (s *APIServer) handleCleanupSelfModelGoals(w http.ResponseWriter, r *http.Request) {
	type reqBody struct {
		Patterns []string `json:"patterns"`
		Statuses []string `json:"statuses"`
	}
	var req reqBody
	_ = json.NewDecoder(r.Body).Decode(&req)

	if s.selfModelManager == nil {
		http.Error(w, "self model not configured", http.StatusBadRequest)
		return
	}

	sm, err := s.selfModelManager.Load()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Compile regexes
	var regs []*regexp.Regexp
	for _, p := range req.Patterns {
		if p == "" {
			continue
		}
		if re, err := regexp.Compile(p); err == nil {
			regs = append(regs, re)
		}
	}

	if len(regs) == 0 {
		regs = []*regexp.Regexp{
			regexp.MustCompile(`^Execute task: Goal Execution$`),
			regexp.MustCompile(`^Execute task: artifact_task$`),
			regexp.MustCompile(`^Execute task: code_.*`),
		}
	}

	statusFilter := map[string]bool{}
	for _, s := range req.Statuses {
		statusFilter[strings.ToLower(strings.TrimSpace(s))] = true
	}

	toDelete := []string{}
	for _, g := range sm.Goals {
		name := strings.TrimSpace(g.Name)

		if len(statusFilter) > 0 {
			if !statusFilter[strings.ToLower(strings.TrimSpace(g.Status))] {
				continue
			}
		}
		for _, re := range regs {
			if re.MatchString(name) {
				toDelete = append(toDelete, g.ID)
				break
			}
		}
	}

	for _, id := range toDelete {
		_ = s.selfModelManager.DeleteGoal(id)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"success":       true,
		"deleted_count": len(toDelete),
		"deleted_ids":   toDelete,
	})
}
