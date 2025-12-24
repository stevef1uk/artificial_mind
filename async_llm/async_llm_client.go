package async_llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// RequestPriority indicates the priority of an LLM request
type RequestPriority int

const (
	PriorityLow  RequestPriority = iota // Background tasks
	PriorityHigh                        // User requests
)

// AsyncLLMRequest represents an async LLM request with callback
type AsyncLLMRequest struct {
	ID           string
	Priority     RequestPriority
	Provider     string // "ollama", "openai", etc.
	Endpoint     string // Full API endpoint URL
	Model        string
	Prompt       string // For /api/generate format
	Messages     []map[string]string // For /api/chat format
	Callback     func(string, error)
	Context      context.Context
	CreatedAt    time.Time
	RequestType  string
}

// AsyncLLMResponse represents a completed LLM response
type AsyncLLMResponse struct {
	RequestID   string
	Response    string
	Error       error
	CompletedAt time.Time
}

// AsyncLLMClient manages async LLM request queues
type AsyncLLMClient struct {
	// Priority stacks (LIFO)
	highPriorityStack []*AsyncLLMRequest
	lowPriorityStack  []*AsyncLLMRequest
	
	// Response queue
	responseQueue chan *AsyncLLMResponse
	
	// Worker pool
	workerPool     chan struct{}
	maxWorkers     int
	
	// Synchronization
	mu             sync.Mutex
	requestMap     map[string]*AsyncLLMRequest
	shutdown       chan struct{}
	wg             sync.WaitGroup
	
	// HTTP client
	httpClient     *http.Client
}

var (
	asyncLLMClient     *AsyncLLMClient
	asyncLLMClientOnce sync.Once
)

// InitAsyncLLMClient initializes the async LLM client system
func InitAsyncLLMClient() *AsyncLLMClient {
	asyncLLMClientOnce.Do(func() {
		maxWorkers := 3 // Default max concurrent LLM requests
		if maxStr := os.Getenv("ASYNC_LLM_MAX_WORKERS"); maxStr != "" {
			if max, err := strconv.Atoi(maxStr); err == nil && max > 0 {
				maxWorkers = max
			}
		}
		
		timeout := 60 * time.Second
		if timeoutStr := os.Getenv("ASYNC_LLM_TIMEOUT_SECONDS"); timeoutStr != "" {
			if sec, err := strconv.Atoi(timeoutStr); err == nil && sec > 0 {
				timeout = time.Duration(sec) * time.Second
			}
		}
		
		asyncLLMClient = &AsyncLLMClient{
			highPriorityStack: make([]*AsyncLLMRequest, 0),
			lowPriorityStack:  make([]*AsyncLLMRequest, 0),
			responseQueue:     make(chan *AsyncLLMResponse, 100),
			workerPool:        make(chan struct{}, maxWorkers),
			maxWorkers:        maxWorkers,
			requestMap:        make(map[string]*AsyncLLMRequest),
			shutdown:          make(chan struct{}),
			httpClient:        &http.Client{Timeout: timeout},
		}
		
		log.Printf("🚀 [ASYNC-LLM] Initialized async LLM client with %d workers", maxWorkers)
		
		// Start queue processor
		asyncLLMClient.wg.Add(1)
		go asyncLLMClient.processQueue()
		
		// Start response handler
		asyncLLMClient.wg.Add(1)
		go asyncLLMClient.processResponses()
	})
	
	return asyncLLMClient
}

// EnqueueRequest adds a request to the appropriate priority stack (LIFO)
func (alc *AsyncLLMClient) EnqueueRequest(req *AsyncLLMRequest) error {
	alc.mu.Lock()
	defer alc.mu.Unlock()
	
	// Generate ID if not provided
	if req.ID == "" {
		req.ID = fmt.Sprintf("llm_req_%d", time.Now().UnixNano())
	}
	
	req.CreatedAt = time.Now()
	
	// Store in request map for callback routing
	alc.requestMap[req.ID] = req
	
	// Add to appropriate stack (LIFO - append to end, pop from end)
	if req.Priority == PriorityHigh {
		alc.highPriorityStack = append(alc.highPriorityStack, req)
		log.Printf("📥 [ASYNC-LLM] Enqueued HIGH priority request: %s (stack size: %d)", req.ID, len(alc.highPriorityStack))
	} else {
		alc.lowPriorityStack = append(alc.lowPriorityStack, req)
		log.Printf("📥 [ASYNC-LLM] Enqueued LOW priority request: %s (stack size: %d)", req.ID, len(alc.lowPriorityStack))
	}
	
	return nil
}

// processQueue continuously processes requests from priority stacks
func (alc *AsyncLLMClient) processQueue() {
	defer alc.wg.Done()
	
	for {
		select {
		case <-alc.shutdown:
			log.Printf("🛑 [ASYNC-LLM] Queue processor shutting down")
			return
		default:
			// Try to get a request from stacks (LIFO - pop from end)
			var req *AsyncLLMRequest
			
			alc.mu.Lock()
			// Always check high priority first
			if len(alc.highPriorityStack) > 0 {
				// Pop from end (LIFO)
				lastIdx := len(alc.highPriorityStack) - 1
				req = alc.highPriorityStack[lastIdx]
				alc.highPriorityStack = alc.highPriorityStack[:lastIdx]
				log.Printf("📤 [ASYNC-LLM] Popped HIGH priority request: %s", req.ID)
			} else if len(alc.lowPriorityStack) > 0 {
				// Pop from end (LIFO)
				lastIdx := len(alc.lowPriorityStack) - 1
				req = alc.lowPriorityStack[lastIdx]
				alc.lowPriorityStack = alc.lowPriorityStack[:lastIdx]
				log.Printf("📤 [ASYNC-LLM] Popped LOW priority request: %s", req.ID)
			}
			alc.mu.Unlock()
			
			if req != nil {
				// Acquire worker slot
				select {
				case alc.workerPool <- struct{}{}:
					// Worker slot acquired, process request
					alc.wg.Add(1)
					go alc.processRequest(req)
				case <-alc.shutdown:
					return
				}
			} else {
				// No requests available, brief sleep
				time.Sleep(100 * time.Millisecond)
			}
		}
	}
}

// processRequest processes a single LLM request
func (alc *AsyncLLMClient) processRequest(req *AsyncLLMRequest) {
	defer alc.wg.Done()
	defer func() { <-alc.workerPool }() // Release worker slot
	
	log.Printf("🔄 [ASYNC-LLM] Processing request: %s (provider: %s, endpoint: %s)", req.ID, req.Provider, req.Endpoint)
	
	// Check if request was cancelled
	select {
	case <-req.Context.Done():
		log.Printf("❌ [ASYNC-LLM] Request %s was cancelled", req.ID)
		alc.sendResponse(&AsyncLLMResponse{
			RequestID:   req.ID,
			Response:    "",
			Error:       fmt.Errorf("request cancelled"),
			CompletedAt: time.Now(),
		})
		return
	default:
	}
	
	// Build request body based on provider and format
	var jsonData []byte
	var err error
	
	if strings.Contains(req.Endpoint, "/api/generate") {
		// Ollama /api/generate format
		request := map[string]interface{}{
			"model":  req.Model,
			"prompt": req.Prompt,
			"stream": false,
		}
		jsonData, err = json.Marshal(request)
	} else if strings.Contains(req.Endpoint, "/api/chat") || strings.Contains(req.Endpoint, "/v1/chat/completions") {
		// Ollama /api/chat or OpenAI format
		if len(req.Messages) > 0 {
			// Use messages format
			request := map[string]interface{}{
				"model":    req.Model,
				"messages": req.Messages,
				"stream":   false,
			}
			jsonData, err = json.Marshal(request)
		} else {
			// Fallback to prompt format
			request := map[string]interface{}{
				"model":    req.Model,
				"messages": []map[string]string{{"role": "user", "content": req.Prompt}},
				"stream":   false,
			}
			jsonData, err = json.Marshal(request)
		}
	} else {
		// Default: try prompt format
		request := map[string]interface{}{
			"model":  req.Model,
			"prompt": req.Prompt,
			"stream": false,
		}
		jsonData, err = json.Marshal(request)
	}
	
	if err != nil {
		alc.sendResponse(&AsyncLLMResponse{
			RequestID:   req.ID,
			Response:    "",
			Error:       err,
			CompletedAt: time.Now(),
		})
		return
	}
	
	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(req.Context, "POST", req.Endpoint, bytes.NewReader(jsonData))
	if err != nil {
		alc.sendResponse(&AsyncLLMResponse{
			RequestID:   req.ID,
			Response:    "",
			Error:       err,
			CompletedAt: time.Now(),
		})
		return
	}
	
	httpReq.Header.Set("Content-Type", "application/json")
	
	// Make the HTTP call
	requestStart := time.Now()
	resp, err := alc.httpClient.Do(httpReq)
	requestDuration := time.Since(requestStart)
	
	if err != nil {
		log.Printf("❌ [ASYNC-LLM] HTTP request failed: %v (duration: %v)", err, requestDuration)
		alc.sendResponse(&AsyncLLMResponse{
			RequestID:   req.ID,
			Response:    "",
			Error:       err,
			CompletedAt: time.Now(),
		})
		return
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		err := fmt.Errorf("LLM API error (status %d): %s", resp.StatusCode, string(body))
		log.Printf("❌ [ASYNC-LLM] %v", err)
		alc.sendResponse(&AsyncLLMResponse{
			RequestID:   req.ID,
			Response:    "",
			Error:       err,
			CompletedAt: time.Now(),
		})
		return
	}
	
	// Read and parse response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		alc.sendResponse(&AsyncLLMResponse{
			RequestID:   req.ID,
			Response:    "",
			Error:       err,
			CompletedAt: time.Now(),
		})
		return
	}
	
	// Parse response based on format
	var responseText string
	if strings.Contains(req.Endpoint, "/api/generate") {
		// Ollama /api/generate format
		var result struct {
			Response string `json:"response"`
		}
		if err := json.Unmarshal(body, &result); err == nil {
			responseText = strings.TrimSpace(result.Response)
		} else {
			responseText = string(body)
		}
	} else {
		// Ollama /api/chat or OpenAI format
		var obj struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			Content string `json:"content"`
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		}
		if err := json.Unmarshal(body, &obj); err == nil {
			if strings.TrimSpace(obj.Message.Content) != "" {
				responseText = obj.Message.Content
			} else if strings.TrimSpace(obj.Content) != "" {
				responseText = obj.Content
			} else if len(obj.Choices) > 0 && strings.TrimSpace(obj.Choices[0].Message.Content) != "" {
				responseText = obj.Choices[0].Message.Content
			} else {
				responseText = string(body)
			}
		} else {
			responseText = string(body)
		}
	}
	
	log.Printf("✅ [ASYNC-LLM] Request completed: %s (duration: %v, response length: %d)", req.ID, requestDuration, len(responseText))
	
	alc.sendResponse(&AsyncLLMResponse{
		RequestID:   req.ID,
		Response:    responseText,
		Error:       nil,
		CompletedAt: time.Now(),
	})
}

// sendResponse sends a response to the response queue
func (alc *AsyncLLMClient) sendResponse(resp *AsyncLLMResponse) {
	select {
	case alc.responseQueue <- resp:
		log.Printf("📨 [ASYNC-LLM] Sent response for request: %s", resp.RequestID)
	case <-time.After(5 * time.Second):
		log.Printf("⚠️ [ASYNC-LLM] Response queue full, dropping response for: %s", resp.RequestID)
	}
}

// processResponses processes completed LLM responses and calls callbacks
func (alc *AsyncLLMClient) processResponses() {
	defer alc.wg.Done()
	
	for {
		select {
		case <-alc.shutdown:
			log.Printf("🛑 [ASYNC-LLM] Response processor shutting down")
			return
		case resp := <-alc.responseQueue:
			alc.mu.Lock()
			req, exists := alc.requestMap[resp.RequestID]
			if exists {
				delete(alc.requestMap, resp.RequestID)
			}
			alc.mu.Unlock()
			
			if !exists {
				log.Printf("⚠️ [ASYNC-LLM] No request found for response: %s", resp.RequestID)
				continue
			}
			
			// Call the callback function
			log.Printf("📞 [ASYNC-LLM] Calling callback for request: %s", resp.RequestID)
			if req.Callback != nil {
				// Call callback in a goroutine to avoid blocking
				go func(callback func(string, error), response string, err error) {
					defer func() {
						if r := recover(); r != nil {
							log.Printf("❌ [ASYNC-LLM] Callback panic for request %s: %v", resp.RequestID, r)
						}
					}()
					callback(response, err)
				}(req.Callback, resp.Response, resp.Error)
			} else {
				log.Printf("⚠️ [ASYNC-LLM] No callback provided for request: %s", resp.RequestID)
			}
		}
	}
}

// Shutdown gracefully shuts down the async LLM client
func (alc *AsyncLLMClient) Shutdown() {
	log.Printf("🛑 [ASYNC-LLM] Shutting down async LLM client...")
	close(alc.shutdown)
	alc.wg.Wait()
	log.Printf("✅ [ASYNC-LLM] Async LLM client shut down")
}

// CallAsync makes an async LLM call and waits for the result
func CallAsync(ctx context.Context, provider, endpoint, model string, prompt string, messages []map[string]string, priority RequestPriority) (string, error) {
	useAsync := strings.TrimSpace(os.Getenv("USE_ASYNC_LLM_QUEUE")) == "1" || 
	           strings.TrimSpace(os.Getenv("USE_ASYNC_LLM_QUEUE")) == "true"
	
	if !useAsync {
		// Fallback to synchronous call
		return CallSync(ctx, provider, endpoint, model, prompt, messages)
	}
	
	client := InitAsyncLLMClient()
	resultChan := make(chan string, 1)
	errorChan := make(chan error, 1)
	
	callback := func(response string, err error) {
		if err != nil {
			errorChan <- err
			return
		}
		resultChan <- response
	}
	
	req := &AsyncLLMRequest{
		Provider:    provider,
		Endpoint:    endpoint,
		Model:       model,
		Prompt:      prompt,
		Messages:    messages,
		Callback:    callback,
		Context:     ctx,
		Priority:    priority,
		RequestType: "LLMCall",
	}
	
	if err := client.EnqueueRequest(req); err != nil {
		return "", err
	}
	
	// Wait for result
	timeout := 5 * time.Minute
	if deadline, ok := ctx.Deadline(); ok {
		timeUntilDeadline := time.Until(deadline)
		if timeUntilDeadline > 0 && timeUntilDeadline < timeout {
			timeout = timeUntilDeadline
		}
	}
	
	select {
	case response := <-resultChan:
		return response, nil
	case err := <-errorChan:
		return "", err
	case <-ctx.Done():
		return "", ctx.Err()
	case <-time.After(timeout):
		return "", fmt.Errorf("timeout waiting for async LLM response")
	}
}

// CallSync makes a synchronous LLM call (fallback)
func CallSync(ctx context.Context, provider, endpoint, model string, prompt string, messages []map[string]string) (string, error) {
	var jsonData []byte
	var err error
	
	if strings.Contains(endpoint, "/api/generate") {
		// Ollama /api/generate format
		request := map[string]interface{}{
			"model":  model,
			"prompt": prompt,
			"stream": false,
		}
		jsonData, err = json.Marshal(request)
	} else {
		// Ollama /api/chat or OpenAI format
		if len(messages) > 0 {
			request := map[string]interface{}{
				"model":    model,
				"messages": messages,
				"stream":   false,
			}
			jsonData, err = json.Marshal(request)
		} else {
			request := map[string]interface{}{
				"model":    model,
				"messages": []map[string]string{{"role": "user", "content": prompt}},
				"stream":   false,
			}
			jsonData, err = json.Marshal(request)
		}
	}
	
	if err != nil {
		return "", err
	}
	
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(jsonData))
	if err != nil {
		return "", err
	}
	
	req.Header.Set("Content-Type", "application/json")
	
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("LLM API error (status %d): %s", resp.StatusCode, string(body))
	}
	
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	
	// Parse response
	if strings.Contains(endpoint, "/api/generate") {
		var result struct {
			Response string `json:"response"`
		}
		if err := json.Unmarshal(body, &result); err == nil {
			return strings.TrimSpace(result.Response), nil
		}
		return string(body), nil
	} else {
		var obj struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			Content string `json:"content"`
		}
		if err := json.Unmarshal(body, &obj); err == nil {
			if strings.TrimSpace(obj.Message.Content) != "" {
				return obj.Message.Content, nil
			}
			if strings.TrimSpace(obj.Content) != "" {
				return obj.Content, nil
			}
		}
		return string(body), nil
	}
}

