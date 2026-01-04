package main

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"time"
)

type OllamaRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Stream   bool      `json:"stream"`
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type OllamaResponse struct {
	Model     string `json:"model"`
	CreatedAt string `json:"created_at"`
	Message   struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"message"`
	Done bool `json:"done"`
}

type ToolCallResponse struct {
	Type     string   `json:"type"`
	ToolCall *ToolCall `json:"tool_call,omitempty"`
}

type ToolCall struct {
	ToolID     string                 `json:"tool_id"`
	Parameters map[string]interface{} `json:"parameters"`
	Description string                `json:"description"`
}

func main() {
	// Get configuration from environment
	ollamaURL := os.Getenv("OLLAMA_BASE_URL")
	if ollamaURL == "" {
		ollamaURL = "http://192.168.1.45:11434/api/chat"
	}

	model := os.Getenv("LLM_MODEL")
	if model == "" {
		model = "Qwen2.5-VL-7B-Instruct:latest"
	}

	log.Printf("🧪 Testing LLM Tool Call Support")
	log.Printf("   Model: %s", model)
	log.Printf("   URL: %s", ollamaURL)

	// Create a simple tool call prompt
	prompt := `You are an AI assistant that helps users achieve goals with concrete actions. ALWAYS prefer using available tools over generating code.

Available Tools:
- mcp_get_concept: Get a specific concept from the Neo4j knowledge graph by name and domain.
  Parameters:
    - name (string): required
    - domain (string): required

Respond using EXACTLY ONE of these JSON formats (no extra text):
1. STRONGLY PREFER: {"type": "tool_call", "tool_call": {"tool_id": "tool_name", "parameters": {...}, "description": "..."}}
2. Or: {"type": "structured_task", "structured_task": {"task_name": "...", "description": "...", "subtasks": [...]}}
3. ONLY if no tool can accomplish the task: {"type": "code_artifact", "code_artifact": {"language": "python", "code": "..."}}
4. Only if the user EXPLICITLY asks for a textual explanation and no action is possible: {"type": "text", "content": "..."}

Rules:
- ALWAYS try to use available tools first before generating code.
- For knowledge queries: use mcp_get_concept to query the knowledge base.
- If tools are relevant, choose tool_call and set ALL required parameters with realistic values.
- For mcp_get_concept: provide 'name' parameter with the concept name and 'domain' parameter.
- Do NOT include any commentary outside the JSON object.

User Input: Query your knowledge base about 'science'. Use the mcp_get_concept tool with name='science' and domain='General' to retrieve information.

Choose the most appropriate response type (favor tool_call) and provide ONLY the JSON object.`

	// Create request
	request := OllamaRequest{
		Model: model,
		Messages: []Message{
			{
				Role:    "user",
				Content: prompt,
			},
		},
		Stream: false,
	}

	jsonData, err := json.Marshal(request)
	if err != nil {
		log.Fatalf("❌ Failed to marshal request: %v", err)
	}

	log.Printf("\n📤 Sending test request...")
	log.Printf("   Prompt length: %d bytes", len(prompt))

	// Make HTTP request
	client := &http.Client{
		Timeout: 120 * time.Second,
	}

	req, err := http.NewRequest("POST", ollamaURL, bytes.NewBuffer(jsonData))
	if err != nil {
		log.Fatalf("❌ Failed to create request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")

	startTime := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		log.Fatalf("❌ HTTP request failed: %v", err)
	}
	defer resp.Body.Close()

	elapsed := time.Since(startTime)
	log.Printf("⏱️  Response time: %v", elapsed)

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		log.Fatalf("❌ API returned error: %d %s", resp.StatusCode, string(body))
	}

	// Read response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Fatalf("❌ Failed to read response: %v", err)
	}

	log.Printf("📥 Received response: %d bytes", len(body))

	// Parse Ollama response
	var ollamaResp OllamaResponse
	if err := json.Unmarshal(body, &ollamaResp); err != nil {
		log.Printf("⚠️  Failed to parse as Ollama response: %v", err)
		log.Printf("📄 Raw response: %s", string(body))
		return
	}

	responseText := ollamaResp.Message.Content
	log.Printf("\n📝 LLM Response:")
	log.Printf("   Length: %d characters", len(responseText))
	log.Printf("   Content:\n%s", responseText)

	// Try to parse as JSON tool call
	log.Printf("\n🔍 Analyzing response...")

	// Try to extract JSON from response
	jsonStart := -1
	jsonEnd := -1
	for i, char := range responseText {
		if char == '{' && jsonStart == -1 {
			jsonStart = i
		}
		if char == '}' {
			jsonEnd = i
		}
	}

	if jsonStart == -1 || jsonEnd == -1 || jsonEnd <= jsonStart {
		log.Printf("❌ No JSON object found in response")
		log.Printf("   Response appears to be plain text, not JSON")
		log.Printf("\n💡 The model may not support structured tool calls")
		return
	}

	jsonStr := responseText[jsonStart : jsonEnd+1]
	log.Printf("   Extracted JSON: %s", jsonStr)

	// Try to parse as ToolCallResponse
	var toolResp ToolCallResponse
	if err := json.Unmarshal([]byte(jsonStr), &toolResp); err != nil {
		log.Printf("❌ Failed to parse JSON: %v", err)
		log.Printf("   The JSON format may not match expected structure")
		return
	}

	log.Printf("\n✅ Successfully parsed JSON response!")
	log.Printf("   Type: %s", toolResp.Type)

	if toolResp.Type == "tool_call" {
		if toolResp.ToolCall == nil {
			log.Printf("⚠️  Response type is 'tool_call' but tool_call field is nil")
			return
		}
		log.Printf("   Tool ID: %s", toolResp.ToolCall.ToolID)
		log.Printf("   Description: %s", toolResp.ToolCall.Description)
		log.Printf("   Parameters: %+v", toolResp.ToolCall.Parameters)

		// Validate parameters
		if toolResp.ToolCall.ToolID == "mcp_get_concept" {
			if name, ok := toolResp.ToolCall.Parameters["name"]; ok {
				log.Printf("   ✅ Parameter 'name': %v", name)
			} else {
				log.Printf("   ⚠️  Missing required parameter 'name'")
			}
			if domain, ok := toolResp.ToolCall.Parameters["domain"]; ok {
				log.Printf("   ✅ Parameter 'domain': %v", domain)
			} else {
				log.Printf("   ⚠️  Missing required parameter 'domain'")
			}
		}

		log.Printf("\n🎉 SUCCESS: Model supports tool calls!")
		log.Printf("   The model correctly returned a tool_call response")
	} else {
		log.Printf("\n⚠️  Model returned type '%s' instead of 'tool_call'", toolResp.Type)
		log.Printf("   This suggests the model may not be following tool call instructions")
		
		if toolResp.Type == "text" {
			log.Printf("   The model chose to return text instead of using a tool")
		} else if toolResp.Type == "structured_task" {
			log.Printf("   The model chose structured_task instead of tool_call")
		}
	}
}

