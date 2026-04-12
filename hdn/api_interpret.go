package main

import (
	"context"
	"encoding/json"
	"eventbus"
	"fmt"
	"hdn/interpreter"
	mempkg "hdn/memory"
	"hdn/utils"
	"log"
	"net/http"
	"strings"
	"time"
)

func (s *APIServer) handleInterpret(w http.ResponseWriter, r *http.Request) {
	s.interpreterAPI.HandleInterpretRequest(w, r)
}

func (s *APIServer) handleInterpretAndExecute(w http.ResponseWriter, r *http.Request) {

	release, acquired := s.acquireExecutionSlot(r)
	if !acquired {
		http.Error(w, "Server busy - too many concurrent executions. Please try again later.", http.StatusTooManyRequests)
		return
	}
	defer release()

	// First interpret the natural language input
	var req interpreter.NaturalLanguageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if req.Context == nil {
		req.Context = make(map[string]string)
	}
	if req.SessionID == "" {
		req.SessionID = fmt.Sprintf("session_%d", time.Now().UnixNano())
	}

	if s.isInformationalQuery(req.Input) {
		log.Printf("ℹ️ [API] Detected informational query, providing capability information")
		infoResponse := s.handleInformationalQuery(r.Context(), req.Input)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(infoResponse)
		return
	}

	if s.eventBus != nil {
		_ = s.eventBus.Publish(r.Context(), eventbus.CanonicalEvent{
			EventID:   eventbus.NewEventID("evt_", time.Now()),
			Source:    "api:interpret_execute",
			Type:      "user_message",
			Timestamp: time.Now().UTC(),
			Context:   eventbus.EventContext{Channel: "api", SessionID: req.SessionID},
			Payload:   eventbus.EventPayload{Text: req.Input, Metadata: map[string]interface{}{"path": "/api/v1/interpret/execute"}},
			Security:  eventbus.EventSecurity{Sensitivity: "low"},
		})
	}

	ctx := r.Context()

	flexibleInterpreter := s.interpreter.GetFlexibleInterpreter()
	if flexibleInterpreter == nil {
		log.Printf("❌ [API] Flexible interpreter not available")
		http.Error(w, "Flexible interpreter not available", http.StatusInternalServerError)
		return
	}

	flexibleResult, err := flexibleInterpreter.InterpretAndExecute(ctx, &req)
	if err != nil {
		log.Printf("❌ [API] Flexible interpretation failed: %v", err)
		http.Error(w, fmt.Sprintf("Interpretation failed: %v", err), http.StatusInternalServerError)
		return
	}

	if !flexibleResult.Success {
		http.Error(w, flexibleResult.Message, http.StatusBadRequest)
		return
	}

	if flexibleResult.Metadata != nil {
		if tc, ok := flexibleResult.Metadata["tool_candidate"].(bool); ok && tc {
			if spec, ok := flexibleResult.Metadata["proposed_tool"].(map[string]interface{}); ok {

				b, _ := json.Marshal(spec)
				// Call our own tool registration handler via in-process method
				var t Tool
				if err := json.Unmarshal(b, &t); err == nil && strings.TrimSpace(t.ID) != "" {
					_ = s.registerTool(ctx, t)
				}
			}
		}
	}

	// Handle different response types
	var executionResults []interpreter.TaskExecutionResult

	if flexibleResult.ToolCall != nil {

		log.Printf("🔧 [API] Tool call executed: %s", flexibleResult.ToolCall.ToolID)

		taskResult := interpreter.TaskExecutionResult{
			Task: interpreter.InterpretedTask{
				TaskName:    "Tool Execution",
				Description: flexibleResult.ToolCall.Description,
			},
			Success: flexibleResult.ToolExecutionResult.Success,
			Result:  utils.SafeResultSummary(flexibleResult.ToolExecutionResult.Result, 5000),
			Error:   flexibleResult.ToolExecutionResult.Error,
		}
		executionResults = append(executionResults, taskResult)
	} else if flexibleResult.StructuredTask != nil {

		log.Printf("🚀 [API] Executing structured task: %s", flexibleResult.StructuredTask.TaskName)

		intelligentReq := IntelligentExecutionRequest{
			TaskName:        flexibleResult.StructuredTask.TaskName,
			Description:     flexibleResult.StructuredTask.Description,
			Context:         flexibleResult.StructuredTask.Context,
			Language:        flexibleResult.StructuredTask.Language,
			ForceRegenerate: flexibleResult.StructuredTask.ForceRegenerate,
			MaxRetries:      flexibleResult.StructuredTask.MaxRetries,
			Timeout:         flexibleResult.StructuredTask.Timeout,
		}

		executor := NewIntelligentExecutor(
			s.domainManager,
			s.codeStorage,
			s.codeGenerator,
			s.dockerExecutor,
			s.llmClient,
			s.actionManager,
			s.plannerIntegration,
			s.selfModelManager,
			s.toolMetrics,
			s.fileStorage,
			s.hdnBaseURL,
			s.redisAddr,
		)

		highPriority := true
		if intelligentReq.Priority == "low" {
			highPriority = false
		}
		result, err := executor.ExecuteTaskIntelligently(ctx, &ExecutionRequest{
			TaskName:        intelligentReq.TaskName,
			Description:     intelligentReq.Description,
			Context:         intelligentReq.Context,
			Language:        intelligentReq.Language,
			ForceRegenerate: intelligentReq.ForceRegenerate,
			MaxRetries:      intelligentReq.MaxRetries,
			Timeout:         intelligentReq.Timeout,
			HighPriority:    highPriority,
		})

		executionResult := interpreter.TaskExecutionResult{
			Task: interpreter.InterpretedTask{
				TaskName:    intelligentReq.TaskName,
				Description: intelligentReq.Description,
			},
			Success: err == nil && result.Success,
			Result:  utils.SafeResultSummary(result.Result, 5000),
			Error: func() string {
				if err != nil {
					return err.Error()
				}
				if !result.Success {
					return result.Error
				}
				return ""
			}(),
			ExecutedAt: time.Now(),
		}

		executionResults = append(executionResults, executionResult)

		s.recordMonitorMetrics(executionResult.Success, result.ExecutionTime)

		if s.episodicClient != nil {
			ep := &mempkg.EpisodicRecord{
				SessionID: req.SessionID,
				PlanID:    "",
				Timestamp: time.Now().UTC(),
				Outcome: func() string {
					if executionResult.Success {
						return "success"
					}
					return "failure"
				}(),
				Reward:    0,
				Tags:      []string{"interpret"},
				StepIndex: 0,
				Text:      fmt.Sprintf("%s: %s", intelligentReq.TaskName, intelligentReq.Description),
				Metadata:  map[string]any{"workflow_id": result.WorkflowID},
			}
			_ = s.episodicClient.IndexEpisode(ep)
		}

		log.Printf("✅ [API] Task %s completed: success=%v", intelligentReq.TaskName, executionResult.Success)
	}

	response := interpreter.InterpretAndExecuteResponse{
		Success: true,
		Interpretation: &interpreter.InterpretationResult{
			Success:       flexibleResult.Success,
			Tasks:         []interpreter.InterpretedTask{},
			Message:       flexibleResult.Message,
			SessionID:     flexibleResult.SessionID,
			InterpretedAt: flexibleResult.InterpretedAt,
			Metadata:      flexibleResult.Metadata,
		},
		ExecutionPlan: executionResults,
		Message:       fmt.Sprintf("Successfully interpreted and executed %d task(s)", len(executionResults)),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// isInformationalQuery checks if the input is asking about capabilities, tools, or knowledge
func (s *APIServer) isInformationalQuery(input string) bool {
	inputLower := strings.ToLower(strings.TrimSpace(input))

	infoKeywords := []string{
		"what do you know",
		"what can you do",
		"what are your capabilities",
		"what tools do you have",
		"list your capabilities",
		"show me what you can do",
		"what do you know how to do",
		"what are you capable of",
		"tell me about your capabilities",
		"what tools are available",
		"list available tools",
		"what can you help with",
	}

	for _, keyword := range infoKeywords {
		if strings.Contains(inputLower, keyword) {
			return true
		}
	}

	return false
}

// handleInformationalQuery provides formatted information about system capabilities
func (s *APIServer) handleInformationalQuery(ctx context.Context, query string) interpreter.InterpretAndExecuteResponse {
	var responseText strings.Builder

	executor := NewIntelligentExecutor(
		s.domainManager,
		s.codeStorage,
		s.codeGenerator,
		s.dockerExecutor,
		s.llmClient,
		s.actionManager,
		s.plannerIntegration,
		s.selfModelManager,
		s.toolMetrics,
		s.fileStorage,
		s.hdnBaseURL,
		s.redisAddr,
	)

	capabilities, err := executor.ListCachedCapabilities()
	stats := executor.GetExecutionStats()

	responseText.WriteString("Here's what I know how to do:\n\n")

	if err == nil && len(capabilities) > 0 {
		totalCapabilities := len(capabilities)
		if totalCap, ok := stats["total_cached_capabilities"].(int); ok && totalCap > 0 {
			totalCapabilities = totalCap
		}
		responseText.WriteString(fmt.Sprintf("📚 **Capabilities**: I have learned %d capabilities that I can execute.\n\n", totalCapabilities))

		sampleCount := 10
		if len(capabilities) < sampleCount {
			sampleCount = len(capabilities)
		}
		responseText.WriteString("**Sample capabilities:**\n")
		shown := 0
		for i := 0; i < len(capabilities) && shown < sampleCount; i++ {
			cap := capabilities[i]
			desc := cap.Description
			if desc == "" {
				desc = cap.TaskName
			}

			descTrimmed := strings.TrimSpace(desc)
			if strings.HasPrefix(descTrimmed, "Execute capability:") ||
				strings.HasPrefix(descTrimmed, "🚨 CRITICAL") ||
				strings.HasPrefix(descTrimmed, "{") ||
				strings.HasPrefix(descTrimmed, "\"interpreted_at\"") ||
				len(descTrimmed) < 15 {
				continue
			}

			desc = strings.TrimSpace(desc)
			desc = strings.TrimPrefix(desc, "🚨")
			desc = strings.TrimSpace(desc)

			if strings.HasPrefix(desc, "{") {
				continue
			}
			if len(desc) > 80 {
				desc = desc[:80] + "..."
			}
			responseText.WriteString(fmt.Sprintf("  • %s (%s)\n", desc, cap.Language))
			shown++
		}
		if len(capabilities) > sampleCount {
			responseText.WriteString(fmt.Sprintf("  ... and %d more\n", len(capabilities)-sampleCount))
		}
		responseText.WriteString("\n")
	} else {
		responseText.WriteString("📚 **Capabilities**: No cached capabilities found.\n\n")
	}

	tools, err := s.listTools(ctx)
	if err == nil && len(tools) > 0 {
		responseText.WriteString(fmt.Sprintf("🔧 **Tools**: I have access to %d tools:\n", len(tools)))
		for _, tool := range tools {
			responseText.WriteString(fmt.Sprintf("  • %s: %s\n", tool.ID, tool.Name))
		}
		responseText.WriteString("\n")
	} else {
		responseText.WriteString("🔧 **Tools**: No tools available.\n\n")
	}

	responseText.WriteString("You can ask me to execute tasks, and I'll generate code to accomplish them. I can also use the available tools to help with various operations.\n")

	return interpreter.InterpretAndExecuteResponse{
		Success: true,
		Interpretation: &interpreter.InterpretationResult{
			Success:       true,
			Tasks:         []interpreter.InterpretedTask{},
			Message:       responseText.String(),
			SessionID:     fmt.Sprintf("session_%d", time.Now().UnixNano()),
			InterpretedAt: time.Now(),
		},
		ExecutionPlan: []interpreter.TaskExecutionResult{},
		Message:       "Informational query answered",
	}
}
