package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
)

// handleTriggerConsolidation manually triggers memory consolidation
func (s *APIServer) handleTriggerConsolidation(w http.ResponseWriter, r *http.Request) {
	if s.memoryConsolidator == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Memory consolidation not available (vectorDB or domainKnowledge not initialized)",
		})
		return
	}

	go func() {
		s.memoryConsolidator.RunConsolidation()
	}()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":   true,
		"message":   "Memory consolidation triggered",
		"timestamp": time.Now().Format(time.RFC3339),
	})
}

func (s *APIServer) handleGetState(w http.ResponseWriter, r *http.Request) {

	state := make(State)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(state)
}

func (s *APIServer) handleUpdateState(w http.ResponseWriter, r *http.Request) {
	var state State
	if err := json.NewDecoder(r.Body).Decode(&state); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "updated"})
}

// handleGetWorkingMemory returns short-term working memory for a session.
func (s *APIServer) handleGetWorkingMemory(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	sessionID := vars["id"]
	if sessionID == "" {
		http.Error(w, "Missing session id", http.StatusBadRequest)
		return
	}

	n := 50
	if q := r.URL.Query().Get("n"); q != "" {
		if v, err := strconv.Atoi(q); err == nil && v > 0 {
			n = v
		}
	}

	mem, err := s.workingMemory.GetWorkingMemory(sessionID, n)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get working memory: %v", err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(mem)
}

// handleAddWorkingMemoryEvent appends an event to session working memory
func (s *APIServer) handleAddWorkingMemoryEvent(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	sessionID := vars["id"]
	if sessionID == "" {
		http.Error(w, "Missing session id", http.StatusBadRequest)
		return
	}
	var payload map[string]any
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	if payload == nil {
		payload = map[string]any{}
	}
	payload["timestamp"] = time.Now().UTC()
	if err := s.workingMemory.AddEvent(sessionID, payload, 100); err != nil {
		http.Error(w, fmt.Sprintf("Failed to add event: %v", err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// handleSetWorkingMemoryLocals sets local variables for a session
func (s *APIServer) handleSetWorkingMemoryLocals(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	sessionID := vars["id"]
	if sessionID == "" {
		http.Error(w, "Missing session id", http.StatusBadRequest)
		return
	}
	var locals map[string]string
	if err := json.NewDecoder(r.Body).Decode(&locals); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	if err := s.workingMemory.SetLocalVariables(sessionID, locals); err != nil {
		http.Error(w, fmt.Sprintf("Failed to set locals: %v", err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// handleSetWorkingMemoryPlan stores the latest plan snapshot for a session
func (s *APIServer) handleSetWorkingMemoryPlan(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	sessionID := vars["id"]
	if sessionID == "" {
		http.Error(w, "Missing session id", http.StatusBadRequest)
		return
	}
	var plan map[string]any
	if err := json.NewDecoder(r.Body).Decode(&plan); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	if plan == nil {
		plan = map[string]any{}
	}
	plan["timestamp"] = time.Now().UTC()
	if err := s.workingMemory.SetLatestPlan(sessionID, plan); err != nil {
		http.Error(w, fmt.Sprintf("Failed to set plan: %v", err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// handleSearchEpisodes proxies search to the RAG adapter (requires episodicClient)
func (s *APIServer) handleSearchEpisodes(w http.ResponseWriter, r *http.Request) {
	if s.vectorDB == nil {
		http.Error(w, "episodic memory not configured", http.StatusServiceUnavailable)
		return
	}
	q := r.URL.Query().Get("q")
	if strings.TrimSpace(q) == "" {
		http.Error(w, "missing q", http.StatusBadRequest)
		return
	}
	limit := 20
	if ls := r.URL.Query().Get("limit"); ls != "" {
		if v, err := strconv.Atoi(ls); err == nil && v > 0 {
			limit = v
		}
	}

	filters := map[string]any{}
	if sid := r.URL.Query().Get("session_id"); sid != "" {
		filters["session_id"] = sid
	}
	if tag := r.URL.Query().Get("tag"); tag != "" {
		filters["tags"] = tag
	}

	vec := toyEmbed(q, 768)
	results, err := s.vectorDB.SearchEpisodes(vec, limit, filters)

	if err != nil {
		http.Error(w, fmt.Sprintf("search failed: %v", err), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results)
}

// handleMemorySummary aggregates beliefs, goals, working memory (optional), and recent episodes
func (s *APIServer) handleMemorySummary(w http.ResponseWriter, r *http.Request) {
	summary := map[string]any{}

	if s.selfModelManager != nil {
		if sm, err := s.selfModelManager.Load(); err == nil {
			summary["beliefs"] = sm.Beliefs
			summary["goals"] = sm.Goals
		} else {
			summary["beliefs_error"] = err.Error()
		}
	}

	if sessionID := r.URL.Query().Get("session_id"); sessionID != "" {
		if wm, err := s.workingMemory.GetWorkingMemory(sessionID, 50); err == nil {
			summary["working_memory"] = wm
		} else {
			summary["working_memory_error"] = err.Error()
		}
	}

	if s.vectorDB != nil {

		qvec := toyEmbed("recent", 768)
		if eps, err := s.vectorDB.SearchEpisodes(qvec, 10, map[string]any{}); err == nil {
			summary["recent_episodes"] = eps
		}
	}

	summary["episodic_enabled"] = (s.vectorDB != nil)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(summary)
}

// generateDailySummaryFromSystemData creates a summary from actual system events and memory
func (s *APIServer) generateDailySummaryFromSystemData(ctx context.Context) string {
	var summary strings.Builder

	today := time.Now().UTC().Format("2006-01-02")
	yesterday := time.Now().UTC().Add(-24 * time.Hour).Format("2006-01-02")

	summary.WriteString("Paragraph:\n")
	summary.WriteString("Today's system activity summary based on actual events and memory data.\n\n")

	summary.WriteString("Discoveries:\n")

	if s.workingMemory != nil {

		mem, err := s.workingMemory.GetWorkingMemory("system", 20)
		if err == nil && len(mem.RecentEvents) > 0 {
			summary.WriteString(fmt.Sprintf("- %d recent events recorded in working memory\n", len(mem.RecentEvents)))
		}
	}

	if s.redis != nil {

		keys, err := s.redis.Keys(ctx, "intelligent:*").Result()
		if err == nil {
			todayCount := 0
			for _, key := range keys {

				if strings.Contains(key, today) || strings.Contains(key, yesterday) {
					todayCount++
				}
			}
			if todayCount > 0 {
				summary.WriteString(fmt.Sprintf("- %d intelligent executions performed\n", todayCount))
			}
		}

		errorKeys, err := s.redis.Keys(ctx, "*error*").Result()
		if err == nil && len(errorKeys) > 0 {
			summary.WriteString(fmt.Sprintf("- %d error-related entries in system logs\n", len(errorKeys)))
		}

		aggPromptKey := fmt.Sprintf("token_usage:aggregated:%s:prompt", today)
		aggCompletionKey := fmt.Sprintf("token_usage:aggregated:%s:completion", today)
		aggTotalKey := fmt.Sprintf("token_usage:aggregated:%s:total", today)

		promptTokens, _ := s.redis.Get(ctx, aggPromptKey).Int()
		completionTokens, _ := s.redis.Get(ctx, aggCompletionKey).Int()
		totalTokens, _ := s.redis.Get(ctx, aggTotalKey).Int()

		indPromptKey := fmt.Sprintf("token_usage:%s:prompt", today)
		indCompletionKey := fmt.Sprintf("token_usage:%s:completion", today)
		indTotalKey := fmt.Sprintf("token_usage:%s:total", today)

		indPrompt, _ := s.redis.Get(ctx, indPromptKey).Int()
		indCompletion, _ := s.redis.Get(ctx, indCompletionKey).Int()
		indTotal, _ := s.redis.Get(ctx, indTotalKey).Int()

		promptTokens += indPrompt
		completionTokens += indCompletion
		totalTokens += indTotal

		if totalTokens > 0 {
			summary.WriteString(fmt.Sprintf("- LLM token usage (overall): %d total tokens (%d prompt + %d completion)\n",
				totalTokens, promptTokens, completionTokens))

			aggComponentKeys, err := s.redis.Keys(ctx, fmt.Sprintf("token_usage:aggregated:%s:component:*:total", today)).Result()
			if err != nil {
				aggComponentKeys = []string{}
			}

			indComponentKeys, err2 := s.redis.Keys(ctx, fmt.Sprintf("token_usage:%s:component:*:total", today)).Result()
			if err2 != nil {
				indComponentKeys = []string{}
			}

			allComponentKeys := append(aggComponentKeys, indComponentKeys...)

			if len(allComponentKeys) > 0 {
				summary.WriteString("  Component breakdown:\n")
				componentTotals := make(map[string]int)

				for _, key := range aggComponentKeys {
					parts := strings.Split(key, ":")
					if len(parts) >= 6 && parts[3] == "aggregated" && parts[4] == "component" {
						component := parts[5]
						compTotal, _ := s.redis.Get(ctx, key).Int()
						if compTotal > 0 {
							componentTotals[component] = compTotal
						}
					}
				}

				for _, key := range indComponentKeys {
					parts := strings.Split(key, ":")
					if len(parts) >= 5 && parts[3] == "component" {
						component := parts[4]
						compTotal, _ := s.redis.Get(ctx, key).Int()
						if compTotal > 0 {
							componentTotals[component] += compTotal
						}
					}
				}

				// Sort components by token usage (descending)
				type compStat struct {
					name  string
					total int
				}
				var sortedComps []compStat
				for comp, total := range componentTotals {
					sortedComps = append(sortedComps, compStat{comp, total})
				}

				for i := 0; i < len(sortedComps)-1; i++ {
					for j := i + 1; j < len(sortedComps); j++ {
						if sortedComps[i].total < sortedComps[j].total {
							sortedComps[i], sortedComps[j] = sortedComps[j], sortedComps[i]
						}
					}
				}

				for _, comp := range sortedComps {

					aggCompPromptKey := fmt.Sprintf("token_usage:aggregated:%s:component:%s:prompt", today, comp.name)
					aggCompCompletionKey := fmt.Sprintf("token_usage:aggregated:%s:component:%s:completion", today, comp.name)
					aggCompPrompt, _ := s.redis.Get(ctx, aggCompPromptKey).Int()
					aggCompCompletion, _ := s.redis.Get(ctx, aggCompCompletionKey).Int()

					indCompPromptKey := fmt.Sprintf("token_usage:%s:component:%s:prompt", today, comp.name)
					indCompCompletionKey := fmt.Sprintf("token_usage:%s:component:%s:completion", today, comp.name)
					indCompPrompt, _ := s.redis.Get(ctx, indCompPromptKey).Int()
					indCompCompletion, _ := s.redis.Get(ctx, indCompCompletionKey).Int()

					totalPrompt := aggCompPrompt + indCompPrompt
					totalCompletion := aggCompCompletion + indCompCompletion

					summary.WriteString(fmt.Sprintf("    - %s: %d total (%d prompt + %d completion)\n",
						comp.name, comp.total, totalPrompt, totalCompletion))
				}
			}
		}
	}

	if s.vectorDB != nil {
		summary.WriteString("- Episodic memory system active and indexing events in Weaviate\n")
	}

	summary.WriteString("\nActions:\n")

	summary.WriteString("- Processed intelligent execution requests\n")
	summary.WriteString("- Updated working memory with recent events\n")
	summary.WriteString("- Indexed episodic memories in vector database\n")

	if s.redis != nil {
		today := time.Now().UTC().Format("2006-01-02")
		totalKey := fmt.Sprintf("token_usage:%s:total", today)
		totalTokens, _ := s.redis.Get(ctx, totalKey).Int()
		if totalTokens > 0 {

			promptKey := fmt.Sprintf("token_usage:%s:prompt", today)
			completionKey := fmt.Sprintf("token_usage:%s:completion", today)
			promptTokens, _ := s.redis.Get(ctx, promptKey).Int()
			completionTokens, _ := s.redis.Get(ctx, completionKey).Int()

			estimatedCost := (float64(promptTokens)/1_000_000.0)*0.50 + (float64(completionTokens)/1_000_000.0)*1.50
			if estimatedCost > 0.001 {
				summary.WriteString(fmt.Sprintf("- Estimated commercial LLM cost: $%.4f (based on %d tokens)\n",
					estimatedCost, totalTokens))
			}
		}
	}

	summary.WriteString("\nQuestions:\n")
	summary.WriteString("1) What patterns can be identified in today's execution logs?\n")
	summary.WriteString("2) How can we improve the system's learning from these events?\n")
	summary.WriteString("3) What new capabilities should be prioritized based on usage?\n")

	return summary.String()
}
