package interpreter

import (
	"context"
	"fmt"
	"log"
)

// LLMClientInterface represents the LLM client interface for the interpreter
type LLMClientInterface interface {
	GenerateResponse(prompt string, context map[string]string) (string, error)
}

// LLMClientWrapperInterface represents the wrapper interface
type LLMClientWrapperInterface interface {
	CallLLM(prompt string) (string, error)
}

// LLMAdapter adapts the existing LLM client to work with the interpreter
type LLMAdapter struct {
	llmClient LLMClientWrapperInterface
}

// NewLLMAdapter creates a new LLM adapter
func NewLLMAdapter(llmClient LLMClientWrapperInterface) *LLMAdapter {
	return &LLMAdapter{
		llmClient: llmClient,
	}
}

// GenerateResponse implements the LLMClientInterface
// Uses low priority by default (for background tasks)
func (a *LLMAdapter) GenerateResponse(prompt string, ctxMap map[string]string) (string, error) {
	return a.GenerateResponseWithPriority(prompt, ctxMap, false)
}

// GenerateResponseWithPriority implements priority-aware LLM generation
func (a *LLMAdapter) GenerateResponseWithPriority(prompt string, ctxMap map[string]string, highPriority bool) (string, error) {
	log.Printf("🤖 [INTERPRETER-LLM] Generating response for prompt length: %d (priority: %v)", len(prompt), highPriority)

	// Add context to the prompt if provided
	enhancedPrompt := prompt
	if len(ctxMap) > 0 {
		enhancedPrompt += "\n\nAdditional Context:\n"
		for k, v := range ctxMap {
			enhancedPrompt += fmt.Sprintf("- %s: %s\n", k, v)
		}
	}

	// Check if wrapper supports priority (using reflection to avoid circular dependency)
	// The wrapper should implement CallLLMWithContextAndPriority
	if priorityWrapper, ok := a.llmClient.(interface {
		CallLLMWithContextAndPriority(ctx context.Context, prompt string, highPriority bool) (string, error)
	}); ok {
		reqCtx := context.Background()
		response, err := priorityWrapper.CallLLMWithContextAndPriority(reqCtx, enhancedPrompt, highPriority)
		if err != nil {
			log.Printf("❌ [INTERPRETER-LLM] LLM generation failed: %v", err)
			return "", fmt.Errorf("LLM generation failed: %v", err)
		}
		log.Printf("✅ [INTERPRETER-LLM] Generated response length: %d", len(response))
		return response, nil
	}

	// Fallback to standard method
	response, err := a.llmClient.CallLLM(enhancedPrompt)
	if err != nil {
		log.Printf("❌ [INTERPRETER-LLM] LLM generation failed: %v", err)
		return "", fmt.Errorf("LLM generation failed: %v", err)
	}

	log.Printf("✅ [INTERPRETER-LLM] Generated response length: %d", len(response))
	return response, nil
}
