package main

import (
	"encoding/json"
	mempkg "hdn/memory"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
)

func (s *APIServer) handleListConcepts(w http.ResponseWriter, r *http.Request) {
	if s.domainKnowledge == nil {
		http.Error(w, "Domain knowledge not available", http.StatusServiceUnavailable)
		return
	}

	domain := r.URL.Query().Get("domain")
	namePattern := r.URL.Query().Get("name")
	limitStr := r.URL.Query().Get("limit")
	limit := 50
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	concepts, err := s.domainKnowledge.SearchConcepts(r.Context(), domain, namePattern, limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"concepts": concepts,
		"count":    len(concepts),
	})
}

func (s *APIServer) handleCreateConcept(w http.ResponseWriter, r *http.Request) {
	if s.domainKnowledge == nil {
		http.Error(w, "Domain knowledge not available", http.StatusServiceUnavailable)
		return
	}

	var concept mempkg.Concept
	if err := json.NewDecoder(r.Body).Decode(&concept); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if concept.Name == "" || concept.Domain == "" {
		http.Error(w, "Name and domain are required", http.StatusBadRequest)
		return
	}

	if err := s.domainKnowledge.SaveConcept(r.Context(), &concept); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "created"})
}

func (s *APIServer) handleGetConcept(w http.ResponseWriter, r *http.Request) {
	if s.domainKnowledge == nil {
		http.Error(w, "Domain knowledge not available", http.StatusServiceUnavailable)
		return
	}

	vars := mux.Vars(r)
	conceptName := vars["name"]

	concept, err := s.domainKnowledge.GetConcept(r.Context(), conceptName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(concept)
}

func (s *APIServer) handleUpdateConcept(w http.ResponseWriter, r *http.Request) {
	if s.domainKnowledge == nil {
		http.Error(w, "Domain knowledge not available", http.StatusServiceUnavailable)
		return
	}

	vars := mux.Vars(r)
	conceptName := vars["name"]

	var concept mempkg.Concept
	if err := json.NewDecoder(r.Body).Decode(&concept); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	concept.Name = conceptName
	if err := s.domainKnowledge.SaveConcept(r.Context(), &concept); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "updated"})
}

func (s *APIServer) handleDeleteConcept(w http.ResponseWriter, r *http.Request) {

	http.Error(w, "Concept deletion not implemented", http.StatusNotImplemented)
}

func (s *APIServer) handleAddConceptProperty(w http.ResponseWriter, r *http.Request) {
	if s.domainKnowledge == nil {
		http.Error(w, "Domain knowledge not available", http.StatusServiceUnavailable)
		return
	}

	vars := mux.Vars(r)
	conceptName := vars["name"]

	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Type        string `json:"type"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if err := s.domainKnowledge.AddProperty(r.Context(), conceptName, req.Name, req.Description, req.Type); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "added"})
}

func (s *APIServer) handleAddConceptConstraint(w http.ResponseWriter, r *http.Request) {
	if s.domainKnowledge == nil {
		http.Error(w, "Domain knowledge not available", http.StatusServiceUnavailable)
		return
	}

	vars := mux.Vars(r)
	conceptName := vars["name"]

	var req struct {
		Description string `json:"description"`
		Type        string `json:"type"`
		Severity    string `json:"severity"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if err := s.domainKnowledge.AddConstraint(r.Context(), conceptName, req.Description, req.Type, req.Severity); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "added"})
}

func (s *APIServer) handleAddConceptExample(w http.ResponseWriter, r *http.Request) {
	if s.domainKnowledge == nil {
		http.Error(w, "Domain knowledge not available", http.StatusServiceUnavailable)
		return
	}

	vars := mux.Vars(r)
	conceptName := vars["name"]

	var example mempkg.Example
	if err := json.NewDecoder(r.Body).Decode(&example); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if err := s.domainKnowledge.AddExample(r.Context(), conceptName, &example); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "added"})
}

func (s *APIServer) handleRelateConcepts(w http.ResponseWriter, r *http.Request) {
	if s.domainKnowledge == nil {
		http.Error(w, "Domain knowledge not available", http.StatusServiceUnavailable)
		return
	}

	vars := mux.Vars(r)
	srcConcept := vars["name"]

	var req struct {
		RelationType  string                 `json:"relation_type"`
		TargetConcept string                 `json:"target_concept"`
		Properties    map[string]interface{} `json:"properties,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if err := s.domainKnowledge.RelateConcepts(r.Context(), srcConcept, req.RelationType, req.TargetConcept, req.Properties); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "related"})
}

func (s *APIServer) handleGetRelatedConcepts(w http.ResponseWriter, r *http.Request) {
	if s.domainKnowledge == nil {
		http.Error(w, "Domain knowledge not available", http.StatusServiceUnavailable)
		return
	}

	vars := mux.Vars(r)
	conceptName := vars["name"]

	relationTypes := r.URL.Query()["relation_type"]
	concepts, err := s.domainKnowledge.GetRelatedConcepts(r.Context(), conceptName, relationTypes)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"concepts": concepts,
		"count":    len(concepts),
	})
}

func (s *APIServer) handleSearchConcepts(w http.ResponseWriter, r *http.Request) {
	if s.domainKnowledge == nil {
		http.Error(w, "Domain knowledge not available", http.StatusServiceUnavailable)
		return
	}

	domain := r.URL.Query().Get("domain")
	namePattern := r.URL.Query().Get("name")
	limitStr := r.URL.Query().Get("limit")
	limit := 50
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	concepts, err := s.domainKnowledge.SearchConcepts(r.Context(), domain, namePattern, limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"concepts": concepts,
		"count":    len(concepts),
	})
}

// handleKnowledgeQuery executes a raw Cypher query against the domain knowledge store
func (s *APIServer) handleKnowledgeQuery(w http.ResponseWriter, r *http.Request) {
	// Accept: { "query": "MATCH ..." }
	var req struct {
		Query string `json:"query"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	log.Printf("[HDN] /knowledge/query len=%d", len(req.Query))
	if strings.TrimSpace(req.Query) == "" {
		http.Error(w, "missing query", http.StatusBadRequest)
		return
	}

	uri := os.Getenv("NEO4J_URI")
	if strings.TrimSpace(uri) == "" {
		uri = "bolt://localhost:7687"
	}
	user := os.Getenv("NEO4J_USER")
	if strings.TrimSpace(user) == "" {
		user = "neo4j"
	}
	pass := os.Getenv("NEO4J_PASS")
	if strings.TrimSpace(pass) == "" {
		pass = "test1234"
	}

	rows, err := mempkg.ExecuteCypher(r.Context(), uri, user, pass, req.Query)
	if err != nil {
		log.Printf("[HDN] Cypher error: %v", err)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	log.Printf("[HDN] /knowledge/query returned %d rows", len(rows))

	// Safety: Cap results to prevent OOM on small devices
	const maxRows = 200
	if len(rows) > maxRows {
		log.Printf("⚠️ [HDN] Truncating query results from %d to %d to prevent OOM", len(rows), maxRows)
		rows = rows[:maxRows]
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"results": rows,
		"count":   len(rows),
		"total":   len(rows),
	})
}
