package conversational

import (
	"context"
	"fmt"
	"log"
	"strings"
)

// NLGGenerator generates natural language responses from reasoning traces and results
type NLGGenerator struct {
	llmClient LLMClientInterface
}

// NLGRequest contains the input for natural language generation
type NLGRequest struct {
	UserMessage    string                 `json:"user_message"`
	Intent         *Intent                `json:"intent"`
	Action         *Action                `json:"action"`
	Result         *ActionResult          `json:"result"`
	Context        map[string]interface{} `json:"context"`
	ShowThinking   bool                   `json:"show_thinking"`
	ReasoningTrace *ReasoningTraceData    `json:"reasoning_trace"`
}

// NLGResponse contains the generated natural language response
type NLGResponse struct {
	Text       string                 `json:"text"`
	Confidence float64                `json:"confidence"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
}

// Action represents an action to be taken
type Action struct {
	Type       string                 `json:"type"`
	Goal       string                 `json:"goal"`
	Parameters map[string]interface{} `json:"parameters"`
}

// ActionResult represents the result of an action
type ActionResult struct {
	Type    string                 `json:"type"`
	Success bool                   `json:"success"`
	Data    map[string]interface{} `json:"data"`
	Error   string                 `json:"error,omitempty"`
}

// NewNLGGenerator creates a new natural language generator
func NewNLGGenerator(llmClient LLMClientInterface) *NLGGenerator {
	return &NLGGenerator{
		llmClient: llmClient,
	}
}

// GenerateResponse generates a natural language response
func (nlg *NLGGenerator) GenerateResponse(ctx context.Context, req *NLGRequest) (*NLGResponse, error) {
	log.Printf("🗣️ [NLG] Generating response for intent: %s", req.Intent.Type)

	// Choose the appropriate generation strategy based on the action type
	switch req.Action.Type {
	case "knowledge_query":
		return nlg.generateKnowledgeResponse(ctx, req)
	case "task_execution":
		return nlg.generateTaskResponse(ctx, req)
	case "planning":
		return nlg.generatePlanningResponse(ctx, req)
	case "learning":
		return nlg.generateLearningResponse(ctx, req)
	case "explanation":
		return nlg.generateExplanationResponse(ctx, req)
	case "general_conversation":
		return nlg.generateConversationResponse(ctx, req)
	default:
		return nlg.generateGenericResponse(ctx, req)
	}
}

// generateKnowledgeResponse generates a response for knowledge queries
func (nlg *NLGGenerator) generateKnowledgeResponse(ctx context.Context, req *NLGRequest) (*NLGResponse, error) {
	prompt := nlg.buildKnowledgePrompt(req)

	response, err := nlg.llmClient.GenerateResponse(ctx, prompt, 500)
	if err != nil {
		return nlg.generateFallbackResponse(req, "knowledge query"), nil
	}

	return &NLGResponse{
		Text:       strings.TrimSpace(response),
		Confidence: 0.8,
		Metadata: map[string]interface{}{
			"response_type": "knowledge",
			"intent_type":   req.Intent.Type,
		},
	}, nil
}

// generateTaskResponse generates a response for task execution
func (nlg *NLGGenerator) generateTaskResponse(ctx context.Context, req *NLGRequest) (*NLGResponse, error) {
	prompt := nlg.buildTaskPrompt(req)

	response, err := nlg.llmClient.GenerateResponse(ctx, prompt, 400)
	if err != nil {
		return nlg.generateFallbackResponse(req, "task execution"), nil
	}

	return &NLGResponse{
		Text:       strings.TrimSpace(response),
		Confidence: 0.7,
		Metadata: map[string]interface{}{
			"response_type": "task",
			"intent_type":   req.Intent.Type,
		},
	}, nil
}

// generatePlanningResponse generates a response for planning requests
func (nlg *NLGGenerator) generatePlanningResponse(ctx context.Context, req *NLGRequest) (*NLGResponse, error) {
	prompt := nlg.buildPlanningPrompt(req)

	response, err := nlg.llmClient.GenerateResponse(ctx, prompt, 600)
	if err != nil {
		return nlg.generateFallbackResponse(req, "planning"), nil
	}

	return &NLGResponse{
		Text:       strings.TrimSpace(response),
		Confidence: 0.8,
		Metadata: map[string]interface{}{
			"response_type": "planning",
			"intent_type":   req.Intent.Type,
		},
	}, nil
}

// generateLearningResponse generates a response for learning requests
func (nlg *NLGGenerator) generateLearningResponse(ctx context.Context, req *NLGRequest) (*NLGResponse, error) {
	prompt := nlg.buildLearningPrompt(req)

	response, err := nlg.llmClient.GenerateResponse(ctx, prompt, 500)
	if err != nil {
		return nlg.generateFallbackResponse(req, "learning"), nil
	}

	return &NLGResponse{
		Text:       strings.TrimSpace(response),
		Confidence: 0.8,
		Metadata: map[string]interface{}{
			"response_type": "learning",
			"intent_type":   req.Intent.Type,
		},
	}, nil
}

// generateExplanationResponse generates a response for explanation requests
func (nlg *NLGGenerator) generateExplanationResponse(ctx context.Context, req *NLGRequest) (*NLGResponse, error) {
	prompt := nlg.buildExplanationPrompt(req)

	response, err := nlg.llmClient.GenerateResponse(ctx, prompt, 600)
	if err != nil {
		return nlg.generateFallbackResponse(req, "explanation"), nil
	}

	return &NLGResponse{
		Text:       strings.TrimSpace(response),
		Confidence: 0.8,
		Metadata: map[string]interface{}{
			"response_type": "explanation",
			"intent_type":   req.Intent.Type,
		},
	}, nil
}

// generateConversationResponse generates a response for general conversation
func (nlg *NLGGenerator) generateConversationResponse(ctx context.Context, req *NLGRequest) (*NLGResponse, error) {
	prompt := nlg.buildConversationPrompt(req)

	response, err := nlg.llmClient.GenerateResponse(ctx, prompt, 300)
	if err != nil {
		return nlg.generateFallbackResponse(req, "conversation"), nil
	}

	return &NLGResponse{
		Text:       strings.TrimSpace(response),
		Confidence: 0.6,
		Metadata: map[string]interface{}{
			"response_type": "conversation",
			"intent_type":   req.Intent.Type,
		},
	}, nil
}

// generateGenericResponse generates a generic response
func (nlg *NLGGenerator) generateGenericResponse(ctx context.Context, req *NLGRequest) (*NLGResponse, error) {
	prompt := nlg.buildGenericPrompt(req)

	response, err := nlg.llmClient.GenerateResponse(ctx, prompt, 300)
	if err != nil {
		return nlg.generateFallbackResponse(req, "generic"), nil
	}

	return &NLGResponse{
		Text:       strings.TrimSpace(response),
		Confidence: 0.5,
		Metadata: map[string]interface{}{
			"response_type": "generic",
			"intent_type":   req.Intent.Type,
		},
	}, nil
}

// buildKnowledgePrompt builds a prompt for knowledge responses
func (nlg *NLGGenerator) buildKnowledgePrompt(req *NLGRequest) string {
	basePrompt := `You are an AI assistant with access to a knowledge base and reasoning capabilities. 
Based on the user's question and the information retrieved, provide a helpful and accurate answer.

User Question: "%s"
Intent: %s
Goal: %s

Please provide a clear, informative answer based on the available information.`

	// Add reasoning trace if available and requested
	if req.ShowThinking && req.ReasoningTrace != nil {
		basePrompt += fmt.Sprintf(`

Reasoning Process:
- Goal: %s
- FSM State: %s
- Actions Taken: %s
- Knowledge Sources: %s
- Tools Used: %s
- Key Decisions: %s

Please incorporate this reasoning context into your response.`,
			req.ReasoningTrace.CurrentGoal,
			req.ReasoningTrace.FSMState,
			strings.Join(req.ReasoningTrace.Actions, ", "),
			strings.Join(req.ReasoningTrace.KnowledgeUsed, ", "),
			strings.Join(req.ReasoningTrace.ToolsInvoked, ", "),
			nlg.formatDecisions(req.ReasoningTrace.Decisions),
		)
	}

	// Add result data if available
	if req.Result != nil && req.Result.Success {
		basePrompt += fmt.Sprintf(`

Retrieved Information:
%s

Use this information to answer the user's question comprehensively.`, nlg.formatResultData(req.Result.Data))
	}

	return fmt.Sprintf(basePrompt, req.UserMessage, req.Intent.Type, req.Action.Goal)
}

// buildTaskPrompt builds a prompt for task execution responses
func (nlg *NLGGenerator) buildTaskPrompt(req *NLGRequest) string {
	basePrompt := `You are an AI assistant that has executed a task for the user. 
Provide a clear summary of what was accomplished and any relevant results.

User Request: "%s"
Task Goal: %s
Task Type: %s

Please provide a helpful summary of the task execution.`

	if req.Result != nil && req.Result.Success {
		basePrompt += fmt.Sprintf(`

Task Results:
%s

Summarize what was accomplished and any important outcomes.`, nlg.formatResultData(req.Result.Data))
	} else if req.Result != nil && !req.Result.Success {
		basePrompt += fmt.Sprintf(`

Task encountered an error: %s

Please explain what went wrong and suggest next steps.`, req.Result.Error)
	}

	return fmt.Sprintf(basePrompt, req.UserMessage, req.Action.Goal, req.Action.Type)
}

// buildPlanningPrompt builds a prompt for planning responses
func (nlg *NLGGenerator) buildPlanningPrompt(req *NLGRequest) string {
	basePrompt := `You are an AI assistant that has created a plan for the user. 
Present the plan in a clear, structured way that the user can easily follow.

User Request: "%s"
Planning Goal: %s

Please present the plan in a helpful and actionable format.`

	if req.Result != nil && req.Result.Success {
		basePrompt += fmt.Sprintf(`

Generated Plan:
%s

Present this plan clearly with step-by-step instructions.`, nlg.formatResultData(req.Result.Data))
	}

	return fmt.Sprintf(basePrompt, req.UserMessage, req.Action.Goal)
}

// buildLearningPrompt builds a prompt for learning responses
func (nlg *NLGGenerator) buildLearningPrompt(req *NLGRequest) string {
	basePrompt := `You are an AI assistant that has learned new information. 
Share what was learned in an educational and engaging way.

User Request: "%s"
Learning Topic: %s

Please share the new knowledge in a helpful and educational format.`

	if req.Result != nil && req.Result.Success {
		basePrompt += fmt.Sprintf(`

Learning Results:
%s

Present the new knowledge in an educational and engaging way.`, nlg.formatResultData(req.Result.Data))
	}

	return fmt.Sprintf(basePrompt, req.UserMessage, req.Action.Goal)
}

// buildExplanationPrompt builds a prompt for explanation responses
func (nlg *NLGGenerator) buildExplanationPrompt(req *NLGRequest) string {
	basePrompt := `You are an AI assistant providing an explanation. 
Give a clear, detailed explanation that helps the user understand the topic.

User Request: "%s"
Explanation Topic: %s

Please provide a comprehensive and clear explanation.`

	if req.Result != nil && req.Result.Success {
		basePrompt += fmt.Sprintf(`

Explanation Content:
%s

Present this explanation in a clear and educational way.`, nlg.formatResultData(req.Result.Data))
	}

	return fmt.Sprintf(basePrompt, req.UserMessage, req.Action.Goal)
}

// buildConversationPrompt builds a prompt for general conversation
func (nlg *NLGGenerator) buildConversationPrompt(req *NLGRequest) string {
	return fmt.Sprintf(`You are a helpful AI assistant. Respond to the user's message in a friendly and helpful way.

User Message: "%s"

Please provide a helpful and engaging response.`, req.UserMessage)
}

// buildGenericPrompt builds a generic prompt
func (nlg *NLGGenerator) buildGenericPrompt(req *NLGRequest) string {
	return fmt.Sprintf(`You are a helpful AI assistant. Respond to the user's message appropriately.

User Message: "%s"
Intent: %s
Goal: %s

Please provide a helpful response.`, req.UserMessage, req.Intent.Type, req.Action.Goal)
}

// generateFallbackResponse generates a fallback response when LLM fails
func (nlg *NLGGenerator) generateFallbackResponse(req *NLGRequest, responseType string) *NLGResponse {
	var response string

	switch responseType {
	case "knowledge":
		response = fmt.Sprintf("I understand you're asking about: %s. Let me help you with that.", req.UserMessage)
	case "task":
		response = fmt.Sprintf("I'll help you with: %s. Let me work on that for you.", req.UserMessage)
	case "planning":
		response = fmt.Sprintf("I'll create a plan for: %s. Let me think through this step by step.", req.UserMessage)
	case "learning":
		response = fmt.Sprintf("I'll learn about: %s. Let me gather information on this topic.", req.UserMessage)
	case "explanation":
		response = fmt.Sprintf("I'll explain: %s. Let me break this down for you.", req.UserMessage)
	default:
		response = fmt.Sprintf("I understand: %s. Let me help you with that.", req.UserMessage)
	}

	return &NLGResponse{
		Text:       response,
		Confidence: 0.3,
		Metadata: map[string]interface{}{
			"response_type": responseType,
			"fallback":      true,
		},
	}
}

// formatDecisions formats decision points for display
func (nlg *NLGGenerator) formatDecisions(decisions []DecisionPoint) string {
	if len(decisions) == 0 {
		return "None"
	}

	var formatted []string
	for _, decision := range decisions {
		formatted = append(formatted, fmt.Sprintf("%s -> %s (%.2f confidence)",
			decision.Description, decision.Chosen, decision.Confidence))
	}

	return strings.Join(formatted, "; ")
}

// formatResultData formats result data for display
func (nlg *NLGGenerator) formatResultData(data map[string]interface{}) string {
	if data == nil {
		return "No data available"
	}

	// Try to extract meaningful information
	if result, ok := data["result"]; ok {
		return fmt.Sprintf("%v", result)
	}

	// Fallback to formatting the entire data structure
	return fmt.Sprintf("%v", data)
}
