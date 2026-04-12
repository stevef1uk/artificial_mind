package main

import (
	"encoding/json"
	"fmt"
	"hdn/interpreter"
	"io/ioutil"
	"log"
	"net/http"

	"github.com/gorilla/mux"
)

func (s *APIServer) loadDomain() error {
	data, err := ioutil.ReadFile(s.domainPath)
	if err != nil {
		return err
	}

	var domain EnhancedDomain
	if err := json.Unmarshal(data, &domain); err != nil {
		// Try to load as legacy domain format
		var legacyDomain Domain
		if err := json.Unmarshal(data, &legacyDomain); err != nil {
			return err
		}

		domain = s.convertLegacyDomain(&legacyDomain)
	}

	s.domain = &domain

	applyDomainEnvOverrides(&s.domain.Config)

	return nil
}

// applyDomainEnvOverrides applies environment variable overrides to DomainConfig
func applyDomainEnvOverrides(cfg *DomainConfig) {
	log.Printf("DEBUG: Applying environment overrides to domain config...")
	if v := getenvTrim("LLM_PROVIDER"); v != "" {
		log.Printf("DEBUG: Setting LLM_PROVIDER from env: %s", v)
		cfg.LLMProvider = v
	}
	if v := getenvTrim("OLLAMA_BASE_URL"); v != "" {
		if cfg.Settings == nil {
			cfg.Settings = make(map[string]string)
		}
		log.Printf("DEBUG: Setting ollama_url from OLLAMA_BASE_URL: %s", v)
		cfg.Settings["ollama_url"] = v
	}
	if v := getenvTrim("OPENAI_BASE_URL"); v != "" {
		if cfg.Settings == nil {
			cfg.Settings = make(map[string]string)
		}
		log.Printf("DEBUG: Setting openai_url from OPENAI_BASE_URL: %s", v)
		cfg.Settings["openai_url"] = v
	}
	if v := getenvTrim("LLM_MODEL"); v != "" {
		if cfg.Settings == nil {
			cfg.Settings = make(map[string]string)
		}
		log.Printf("DEBUG: Setting model from LLM_MODEL: %s", v)
		cfg.Settings["model"] = v
	}
}

// SyncPromptHints synchronizes prompt hints from MCP skills to the interpreter registry
func (s *APIServer) SyncPromptHints() {
	if s.mcpKnowledgeServer != nil && s.mcpKnowledgeServer.skillRegistry != nil {
		allHints := s.mcpKnowledgeServer.GetAllPromptHints()
		if len(allHints) == 0 {
			log.Printf("ℹ️ [API] No prompt hints found in skill registry")
			return
		}
		for toolID, hints := range allHints {

			interpreterHints := &interpreter.PromptHintsConfig{
				Keywords:      hints.Keywords,
				PromptText:    hints.PromptText,
				ForceToolCall: hints.ForceToolCall,
				AlwaysInclude: hints.AlwaysInclude,
				RejectText:    hints.RejectText,
			}
			interpreter.SetPromptHints(toolID, interpreterHints)
			log.Printf("📝 [API] Registered prompt hints for tool: %s", toolID)
		}
	}
}

func (s *APIServer) convertLegacyDomain(legacy *Domain) EnhancedDomain {
	enhanced := EnhancedDomain{
		Methods: make([]EnhancedMethodDef, len(legacy.Methods)),
		Actions: make([]EnhancedActionDef, len(legacy.Actions)),
		Config:  DomainConfig{},
	}

	for i, method := range legacy.Methods {
		enhanced.Methods[i] = EnhancedMethodDef{
			MethodDef: method,
			TaskType:  TaskTypeMethod,
		}
	}

	for i, action := range legacy.Actions {
		enhanced.Actions[i] = EnhancedActionDef{
			ActionDef: action,
			TaskType:  TaskTypePrimitive,
		}
	}

	return enhanced
}

func (s *APIServer) saveDomain() error {
	data, err := json.MarshalIndent(s.domain, "", "  ")
	if err != nil {
		return err
	}
	return ioutil.WriteFile(s.domainPath, data, 0644)
}

func (s *APIServer) handleGetDomain(w http.ResponseWriter, r *http.Request) {
	response := DomainResponse{
		Methods: make([]MethodDef, len(s.domain.Methods)),
		Actions: make([]ActionDef, len(s.domain.Actions)),
		Config:  s.domain.Config,
	}

	for i, method := range s.domain.Methods {
		response.Methods[i] = method.MethodDef
	}

	for i, action := range s.domain.Actions {
		response.Actions[i] = action.ActionDef
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (s *APIServer) handleUpdateDomain(w http.ResponseWriter, r *http.Request) {
	var req DomainResponse
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	s.domain.Methods = make([]EnhancedMethodDef, len(req.Methods))
	s.domain.Actions = make([]EnhancedActionDef, len(req.Actions))

	for i, method := range req.Methods {
		s.domain.Methods[i] = EnhancedMethodDef{
			MethodDef: method,
			TaskType:  TaskTypeMethod,
		}
	}

	for i, action := range req.Actions {
		s.domain.Actions[i] = EnhancedActionDef{
			ActionDef: action,
			TaskType:  TaskTypePrimitive,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "updated"})
}

func (s *APIServer) handleSaveDomain(w http.ResponseWriter, r *http.Request) {
	if err := s.saveDomain(); err != nil {
		http.Error(w, "Failed to save domain", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "saved"})
}

func (s *APIServer) convertToLegacyDomain() Domain {
	legacy := Domain{
		Methods: make([]MethodDef, len(s.domain.Methods)),
		Actions: make([]ActionDef, len(s.domain.Actions)),
	}

	for i, method := range s.domain.Methods {
		legacy.Methods[i] = method.MethodDef
	}

	for i, action := range s.domain.Actions {
		legacy.Actions[i] = action.ActionDef
	}

	return legacy
}

func (s *APIServer) handleListDomains(w http.ResponseWriter, r *http.Request) {
	domains, err := s.domainManager.ListDomains()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to list domains: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(domains)
}

func (s *APIServer) handleCreateDomain(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string       `json:"name"`
		Description string       `json:"description"`
		Tags        []string     `json:"tags"`
		Config      DomainConfig `json:"config"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if req.Name == "" {
		http.Error(w, "Domain name is required", http.StatusBadRequest)
		return
	}

	if req.Config.LLMProvider == "" {
		req.Config = s.domain.Config
	}

	err := s.domainManager.CreateDomain(req.Name, req.Description, req.Config, req.Tags)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to create domain: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "created", "domain": req.Name})
}

func (s *APIServer) handleGetDomainByName(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	domainName := vars["name"]

	domain, err := s.domainManager.GetDomain(domainName)
	if err != nil {
		http.Error(w, fmt.Sprintf("Domain not found: %v", err), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(domain)
}

func (s *APIServer) handleDeleteDomain(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	domainName := vars["name"]

	err := s.domainManager.DeleteDomain(domainName)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to delete domain: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "deleted", "domain": domainName})
}

func (s *APIServer) handleSwitchDomain(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	domainName := vars["name"]

	domain, err := s.domainManager.GetDomain(domainName)
	if err != nil {
		http.Error(w, fmt.Sprintf("Domain not found: %v", err), http.StatusNotFound)
		return
	}

	enhancedDomain := &EnhancedDomain{
		Methods: make([]EnhancedMethodDef, len(domain.Methods)),
		Actions: make([]EnhancedActionDef, len(domain.Actions)),
		Config:  domain.Config,
	}

	for i, method := range domain.Methods {
		enhancedDomain.Methods[i] = EnhancedMethodDef{
			MethodDef: *method,
			TaskType:  TaskTypeMethod,
		}
	}

	for i, action := range domain.Actions {
		enhancedDomain.Actions[i] = EnhancedActionDef{
			ActionDef: *action,
			TaskType:  TaskTypePrimitive,
		}
	}

	dynamicActions, err := s.actionManager.GetActionsByDomain(domainName)
	if err == nil {
		for _, dynamicAction := range dynamicActions {

			actionDef := s.actionManager.ConvertToLegacyAction(dynamicAction)

			enhancedAction := EnhancedActionDef{
				ActionDef: *actionDef,
				TaskType:  TaskTypePrimitive,
			}
			enhancedDomain.Actions = append(enhancedDomain.Actions, enhancedAction)
		}
		log.Printf("✅ [DOMAIN] Loaded %d dynamic actions from Redis", len(dynamicActions))
	}

	s.domain = enhancedDomain
	s.currentDomain = domainName

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "switched", "domain": domainName})
}

func (s *APIServer) handleCreateAction(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Task          string            `json:"task"`
		Preconditions []string          `json:"preconditions"`
		Effects       []string          `json:"effects"`
		TaskType      string            `json:"task_type"`
		Description   string            `json:"description"`
		Code          string            `json:"code,omitempty"`
		Language      string            `json:"language,omitempty"`
		Context       map[string]string `json:"context"`
		Domain        string            `json:"domain"`
		Tags          []string          `json:"tags"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if req.Task == "" {
		http.Error(w, "Task name is required", http.StatusBadRequest)
		return
	}

	if req.Domain == "" {
		req.Domain = s.currentDomain
		if req.Domain == "" {
			req.Domain = "default"
		}
	}

	action := &DynamicAction{
		Task:          req.Task,
		Preconditions: req.Preconditions,
		Effects:       req.Effects,
		TaskType:      req.TaskType,
		Description:   req.Description,
		Code:          req.Code,
		Language:      req.Language,
		Context:       req.Context,
		Domain:        req.Domain,
		Tags:          req.Tags,
	}

	err := s.actionManager.CreateAction(action)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to create action: %v", err), http.StatusInternalServerError)
		return
	}

	if req.Domain == s.currentDomain {
		actionDef := s.actionManager.ConvertToLegacyAction(action)
		s.domainManager.AddAction(req.Domain, actionDef)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "created", "action": req.Task, "domain": req.Domain})
}

func (s *APIServer) handleListActions(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	domainName := vars["domain"]

	actions, err := s.actionManager.GetActionsByDomain(domainName)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to list actions: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(actions)
}

func (s *APIServer) handleGetAction(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	domainName := vars["domain"]
	actionID := vars["id"]

	action, err := s.actionManager.GetAction(domainName, actionID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Action not found: %v", err), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(action)
}

func (s *APIServer) handleDeleteAction(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	domainName := vars["domain"]
	actionID := vars["id"]

	err := s.actionManager.DeleteAction(domainName, actionID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to delete action: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "deleted", "action": actionID, "domain": domainName})
}

func (s *APIServer) handleSearchActions(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	domainName := vars["domain"]

	var req struct {
		Query    string   `json:"query"`
		TaskType string   `json:"task_type"`
		Tags     []string `json:"tags"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	actions, err := s.actionManager.SearchActions(domainName, req.Query, req.TaskType, req.Tags)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to search actions: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(actions)
}
