package interpreter

import (
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
func (a *LLMAdapter) GenerateResponse(prompt string, context map[string]string) (string, error) {
	log.Printf("🤖 [INTERPRETER-LLM] Generating response for prompt length: %d", len(prompt))

	// Add context to the prompt if provided
	enhancedPrompt := prompt
	if len(context) > 0 {
		enhancedPrompt += "\n\nAdditional Context:\n"
		for k, v := range context {
			enhancedPrompt += fmt.Sprintf("- %s: %s\n", k, v)
		}
	}

	response, err := a.llmClient.CallLLM(enhancedPrompt)
	if err != nil {
		log.Printf("❌ [INTERPRETER-LLM] LLM generation failed: %v", err)
		return "", fmt.Errorf("LLM generation failed: %v", err)
	}

	log.Printf("✅ [INTERPRETER-LLM] Generated response length: %d", len(response))
	return response, nil
}
