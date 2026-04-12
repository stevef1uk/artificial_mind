package main

import (
	"encoding/json"
	"fmt"
	"net/http"
)

func (s *APIServer) handleLearn(w http.ResponseWriter, r *http.Request) {
	var req LearnRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Try to learn using the specified method
	var learned bool
	var method *MethodDef
	var message string

	if req.UseLLM {
		learned, method, message = s.learnWithLLM(req)
	} else if req.UseMCP {
		learned, method, message = s.learnWithMCP(req)
	} else {

		learned, method, message = s.learnTraditional(req)
	}

	response := LearnResponse{
		Success: learned,
		Message: message,
		Method:  method,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (s *APIServer) handleLearnLLM(w http.ResponseWriter, r *http.Request) {
	var req LearnRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	req.UseLLM = true
	learned, method, message := s.learnWithLLM(req)

	response := LearnResponse{
		Success: learned,
		Message: message,
		Method:  method,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (s *APIServer) handleLearnMCP(w http.ResponseWriter, r *http.Request) {
	var req LearnRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	req.UseMCP = true
	learned, method, message := s.learnWithMCP(req)

	response := LearnResponse{
		Success: learned,
		Message: message,
		Method:  method,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (s *APIServer) learnTraditional(req LearnRequest) (bool, *MethodDef, string) {

	legacyDomain := s.convertToLegacyDomain()

	// Find missing predicates for the task
	var missing []string
	for _, action := range legacyDomain.Actions {
		if action.Task == req.TaskName {
			missing = missingPredicatesForAction(&action, make(State))
			break
		}
	}

	if len(missing) == 0 {
		return false, nil, "No missing predicates found for traditional learning"
	}

	learned := LearnMethodForMissing(req.TaskName, missing, &legacyDomain)
	if learned {

		for _, method := range legacyDomain.Methods {
			if method.Task == req.TaskName && method.IsLearned {
				return true, &method, "Successfully learned method using traditional approach"
			}
		}
	}

	return false, nil, "Failed to learn using traditional approach"
}

func (s *APIServer) learnWithLLM(req LearnRequest) (bool, *MethodDef, string) {
	if s.llmClient == nil {
		return false, nil, "LLM client not configured"
	}

	method, err := s.llmClient.GenerateMethod(req.TaskName, req.Description, req.Context)
	if err != nil {
		return false, nil, fmt.Sprintf("LLM learning failed: %v", err)
	}

	enhancedMethod := EnhancedMethodDef{
		MethodDef: *method,
		TaskType:  TaskTypeLLM,
		LLMPrompt: req.Description,
	}
	enhancedMethod.IsLearned = true

	s.domain.Methods = append([]EnhancedMethodDef{enhancedMethod}, s.domain.Methods...)

	return true, method, "Successfully learned method using LLM"
}

func (s *APIServer) learnWithMCP(req LearnRequest) (bool, *MethodDef, string) {
	if s.mcpClient == nil {
		return false, nil, "MCP client not configured"
	}

	method, err := s.mcpClient.GenerateMethod(req.TaskName, req.Description, req.Context)
	if err != nil {
		return false, nil, fmt.Sprintf("MCP learning failed: %v", err)
	}

	enhancedMethod := EnhancedMethodDef{
		MethodDef: *method,
		TaskType:  TaskTypeMCP,
		MCPTool:   req.Description,
	}
	enhancedMethod.IsLearned = true

	s.domain.Methods = append([]EnhancedMethodDef{enhancedMethod}, s.domain.Methods...)

	return true, method, "Successfully learned method using MCP"
}
