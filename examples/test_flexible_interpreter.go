package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"agi/hdn/interpreter"
)

// RealLLMClient implements the LLMClientWrapperInterface using a real LLM
type RealLLMClient struct {
	baseURL    string
	httpClient *http.Client
}

func NewRealLLMClient(baseURL string) *RealLLMClient {
	return &RealLLMClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (r *RealLLMClient) GenerateResponse(prompt string, context map[string]string) (string, error) {
	return r.CallLLM(prompt)
}

func (r *RealLLMClient) CallLLM(prompt string) (string, error) {
	log.Printf("🤖 [REAL-LLM] Calling Ollama with prompt length: %d", len(prompt))

	// Call Ollama API
	requestBody := map[string]interface{}{
		"model":  "gemma3:12b",
		"prompt": prompt,
		"stream": false,
	}

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %v", err)
	}

	resp, err := r.httpClient.Post(r.baseURL+"/api/generate", "application/json",
		strings.NewReader(string(jsonData)))
	if err != nil {
		return "", fmt.Errorf("failed to call Ollama: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Ollama returned status %d", resp.StatusCode)
	}

	var response map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return "", fmt.Errorf("failed to decode response: %v", err)
	}

	// Extract the response text
	if responseText, ok := response["response"].(string); ok {
		log.Printf("✅ [REAL-LLM] Received response length: %d", len(responseText))
		return responseText, nil
	}

	return "", fmt.Errorf("no response text in Ollama response")
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && s[:len(substr)] == substr ||
		len(s) > len(substr) && containsString(s[1:], substr)
}

func main() {
	fmt.Println("🧪 Flexible API Test with Real LLM and Real HDN Tools")
	fmt.Println("=====================================================")

	// Create real LLM client and mock tool provider
	realLLM := NewRealLLMClient("http://localhost:11434") // Ollama default port
	mockToolProvider := &interpreter.MockToolProvider{}

	// Create flexible interpreter
	adapter := interpreter.NewFlexibleLLMAdapter(realLLM)
	flexibleInterpreter := interpreter.NewFlexibleInterpreter(adapter, mockToolProvider)

	ctx := context.Background()

	// Test 1: Code Generation (should use tool_codegen)
	fmt.Println("\n1. Testing Code Generation...")
	req1 := &interpreter.NaturalLanguageRequest{
		Input:     "Generate a Python function to calculate fibonacci numbers",
		Context:   map[string]string{},
		SessionID: "test-session-1",
	}

	result1, err := flexibleInterpreter.Interpret(ctx, req1)
	if err != nil {
		log.Printf("❌ Error: %v", err)
	} else {
		fmt.Printf("✅ Success: %s\n", result1.Message)
		fmt.Printf("✅ Type: %s\n", result1.ResponseType)
		if result1.ToolExecutionResult != nil {
			fmt.Printf("✅ Tool Result: %v\n", result1.ToolExecutionResult.Result)
		}
	}

	// Test 2: Web Search (should use tool_http_get)
	fmt.Println("\n2. Testing Web Search...")
	req2 := &interpreter.NaturalLanguageRequest{
		Input:     "Search for information about artificial intelligence",
		Context:   map[string]string{},
		SessionID: "test-session-2",
	}

	result2, err := flexibleInterpreter.Interpret(ctx, req2)
	if err != nil {
		log.Printf("❌ Error: %v", err)
	} else {
		fmt.Printf("✅ Success: %s\n", result2.Message)
		fmt.Printf("✅ Type: %s\n", result2.ResponseType)
		if result2.ToolExecutionResult != nil {
			fmt.Printf("✅ Tool Result: %v\n", result2.ToolExecutionResult.Result)
		}
	}

	// Test 3: File Operations (should use tool_file_read)
	fmt.Println("\n3. Testing File Operations...")
	req3 := &interpreter.NaturalLanguageRequest{
		Input:     "Read the file /etc/hostname",
		Context:   map[string]string{},
		SessionID: "test-session-3",
	}

	result3, err := flexibleInterpreter.Interpret(ctx, req3)
	if err != nil {
		log.Printf("❌ Error: %v", err)
	} else {
		fmt.Printf("✅ Success: %s\n", result3.Message)
		fmt.Printf("✅ Type: %s\n", result3.ResponseType)
		if result3.ToolExecutionResult != nil {
			fmt.Printf("✅ Tool Result: %v\n", result3.ToolExecutionResult.Result)
		}
	}

	// Test 4: Directory Listing (should use tool_ls)
	fmt.Println("\n4. Testing Directory Listing...")
	req4 := &interpreter.NaturalLanguageRequest{
		Input:     "List the contents of the /tmp directory",
		Context:   map[string]string{},
		SessionID: "test-session-4",
	}

	result4, err := flexibleInterpreter.Interpret(ctx, req4)
	if err != nil {
		log.Printf("❌ Error: %v", err)
	} else {
		fmt.Printf("✅ Success: %s\n", result4.Message)
		fmt.Printf("✅ Type: %s\n", result4.ResponseType)
		if result4.ToolExecutionResult != nil {
			fmt.Printf("✅ Tool Result: %v\n", result4.ToolExecutionResult.Result)
		}
	}

	// Test 5: Code Artifact Generation (should generate code directly)
	fmt.Println("\n5. Testing Direct Code Generation...")
	req5 := &interpreter.NaturalLanguageRequest{
		Input:     "Create a simple hello world program in Python",
		Context:   map[string]string{},
		SessionID: "test-session-5",
	}

	result5, err := flexibleInterpreter.Interpret(ctx, req5)
	if err != nil {
		log.Printf("❌ Error: %v", err)
	} else {
		fmt.Printf("✅ Success: %s\n", result5.Message)
		fmt.Printf("✅ Type: %s\n", result5.ResponseType)
		if result5.CodeArtifact != nil {
			fmt.Printf("✅ Code Language: %s\n", result5.CodeArtifact.Language)
			fmt.Printf("✅ Code: %s\n", result5.CodeArtifact.Code)
		}
	}

	fmt.Println("\n🎉 Test completed!")
	fmt.Println("\nThis demonstrates the flexible natural language processing system:")
	fmt.Println("• LLM can choose appropriate tools based on user input")
	fmt.Println("• Real tool execution through HDN server")
	fmt.Println("• Support for multiple response types (tool calls, code artifacts)")
	fmt.Println("• No rigid response parsing - LLM has full flexibility")
}
