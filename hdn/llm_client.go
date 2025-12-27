package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// --------- LLM Client ---------

// Context key for component tracking
type componentKey struct{}

// WithComponent adds component information to context for token tracking
func WithComponent(ctx context.Context, component string) context.Context {
	return context.WithValue(ctx, componentKey{}, component)
}

// getComponentFromContext extracts component from context, returns "unknown" if not set
func getComponentFromContext(ctx context.Context) string {
	if component, ok := ctx.Value(componentKey{}).(string); ok && component != "" {
		return component
	}
	return "unknown"
}

// RequestPriority indicates the priority of an LLM request
type RequestPriority int

const (
	PriorityLow  RequestPriority = iota // Background tasks (FSM, learning, etc.)
	PriorityHigh                        // User requests (chat, tools, etc.)
)

// LLMRequestTicket represents a request waiting for an LLM slot
type LLMRequestTicket struct {
	Priority RequestPriority
	Acquired chan struct{} // Closed when slot is acquired
	Cancel   <-chan struct{} // Context cancellation channel
}

// Global priority queue system for LLM requests
var (
	llmRequestSemaphore chan struct{}
	llmSemaphoreOnce    sync.Once
	highPriorityQueue   chan *LLMRequestTicket
	lowPriorityQueue    chan *LLMRequestTicket
	queueDispatcherOnce sync.Once
)

// initLLMSemaphore initializes the global LLM request semaphore with priority queue
// This limits how many LLM requests can be in-flight simultaneously
// User requests get priority over background tasks
func initLLMSemaphore() {
	llmSemaphoreOnce.Do(func() {
		maxConcurrentLLM := 2 // Conservative default: only 2 concurrent LLM requests
		if maxStr := os.Getenv("LLM_MAX_CONCURRENT_REQUESTS"); maxStr != "" {
			if max, err := strconv.Atoi(maxStr); err == nil && max > 0 {
				maxConcurrentLLM = max
			}
		}
		llmRequestSemaphore = make(chan struct{}, maxConcurrentLLM)
		
		// Initialize priority queues (buffered to prevent blocking)
		highPriorityQueue = make(chan *LLMRequestTicket, 100)
		lowPriorityQueue = make(chan *LLMRequestTicket, 100)
		
		log.Printf("🔒 [LLM] Initialized LLM request semaphore with max %d concurrent requests", maxConcurrentLLM)
		log.Printf("🔒 [LLM] Priority queue enabled: user requests get priority over background tasks")
		
		// Start the queue dispatcher
		go dispatchLLMRequests()
	})
}

// dispatchLLMRequests continuously dispatches requests from priority queues
// Always serves high-priority requests first, then low-priority
func dispatchLLMRequests() {
	for {
		// Always check high priority queue first
		select {
		case ticket := <-highPriorityQueue:
			// High priority: try to acquire immediately
			select {
			case llmRequestSemaphore <- struct{}{}:
				close(ticket.Acquired)
			case <-ticket.Cancel:
				// Request was cancelled, skip
			default:
				// No slot available, put back in queue (will retry)
				select {
				case highPriorityQueue <- ticket:
				case <-ticket.Cancel:
					// Request was cancelled while re-queuing
				}
			}
			continue // Go back to check high priority again
		default:
			// No high priority requests, check low priority
		}

		// Check if background LLM work is disabled (check once per loop, not blocking)
		disableBackgroundLLM := strings.TrimSpace(os.Getenv("DISABLE_BACKGROUND_LLM")) == "1" || 
		                         strings.TrimSpace(os.Getenv("DISABLE_BACKGROUND_LLM")) == "true"
		
		select {
		case ticket := <-lowPriorityQueue:
			// If background LLM is disabled, reject low priority requests immediately
			if disableBackgroundLLM {
				log.Printf("🔒 [LLM] Rejecting low priority request (background LLM disabled)")
				// Don't close ticket.Acquired - this will cause it to timeout
				// Don't put it back in queue, just let it timeout naturally
				continue
			}
			// Double-check high priority queue before serving low priority
			select {
			case highTicket := <-highPriorityQueue:
				// High priority request arrived, serve it first
				select {
				case llmRequestSemaphore <- struct{}{}:
					close(highTicket.Acquired)
				case <-highTicket.Cancel:
					// High priority request was cancelled
				}
				// Put low priority ticket back
				select {
				case lowPriorityQueue <- ticket:
				case <-ticket.Cancel:
				}
			default:
				// No high priority requests, serve low priority
				select {
				case llmRequestSemaphore <- struct{}{}:
					close(ticket.Acquired)
				case <-ticket.Cancel:
					// Request was cancelled
				default:
					// No slot available, put back in queue
					select {
					case lowPriorityQueue <- ticket:
					case <-ticket.Cancel:
					}
				}
			}
		case <-time.After(100 * time.Millisecond):
			// Brief sleep to prevent busy-waiting
		}
	}
}

// acquireLLMSlot acquires an LLM slot with the given priority
// Returns true if acquired, false if cancelled or timed out
func acquireLLMSlot(ctx context.Context, priority RequestPriority, timeout time.Duration) bool {
	initLLMSemaphore()
	
	// Check if background LLM is disabled - reject low priority requests immediately
	if priority == PriorityLow {
		disableBackgroundLLM := strings.TrimSpace(os.Getenv("DISABLE_BACKGROUND_LLM")) == "1" || 
		                         strings.TrimSpace(os.Getenv("DISABLE_BACKGROUND_LLM")) == "true"
		if disableBackgroundLLM {
			log.Printf("🔒 [LLM] Rejecting low priority request immediately (background LLM disabled)")
			return false
		}
	}
	
	ticket := &LLMRequestTicket{
		Priority: priority,
		Acquired: make(chan struct{}),
		Cancel:   ctx.Done(),
	}
	
	// Enqueue based on priority
	var queue chan *LLMRequestTicket
	if priority == PriorityHigh {
		queue = highPriorityQueue
		log.Printf("🔒 [LLM] Enqueuing HIGH priority request")
	} else {
		queue = lowPriorityQueue
		log.Printf("🔒 [LLM] Enqueuing LOW priority request")
	}
	
	// Enqueue the request
	select {
	case queue <- ticket:
		// Successfully enqueued
	case <-ctx.Done():
		return false
	}
	
	// Wait for slot acquisition or cancellation
	select {
	case <-ticket.Acquired:
		log.Printf("🔒 [LLM] Acquired LLM request slot (priority: %v)", priority)
		return true
	case <-ctx.Done():
		log.Printf("🔒 [LLM] Request cancelled while waiting for slot")
		return false
	case <-time.After(timeout):
		log.Printf("🔒 [LLM] Timed out waiting for LLM slot")
		return false
	}
}

// ========== ASYNC LLM QUEUE SYSTEM (PROOF OF CONCEPT) ==========

// AsyncLLMRequest represents an async LLM request with callback
type AsyncLLMRequest struct {
	ID           string                    // Unique request ID
	Priority     RequestPriority           // Request priority
	Prompt       string                    // The LLM prompt
	RequestType  string                    // Type of request (e.g., "GenerateMethod", "GenerateCode")
	RequestData  map[string]interface{}    // Additional request data
	Callback     func(string, error)       // Callback function to call with result
	CreatedAt    time.Time                 // When request was created
	Context      context.Context           // Context for cancellation
}

// AsyncLLMResponse represents a completed LLM response
type AsyncLLMResponse struct {
	RequestID   string
	Response    string
	Error       error
	CompletedAt time.Time
}

// AsyncLLMQueueManager manages async LLM request queues
type AsyncLLMQueueManager struct {
	// Priority stacks (LIFO - Last In First Out)
	highPriorityStack []*AsyncLLMRequest
	lowPriorityStack  []*AsyncLLMRequest
	
	// Response queue
	responseQueue chan *AsyncLLMResponse
	
	// Worker pool
	workerPool     chan struct{}
	maxWorkers     int
	
	// Backpressure limits
	maxHighPriorityQueue int // Maximum high-priority requests in queue
	maxLowPriorityQueue  int // Maximum low-priority requests in queue
	
	// Synchronization
	mu             sync.Mutex
	requestMap     map[string]*AsyncLLMRequest // Map request ID to request for callbacks
	shutdown       chan struct{}
	wg             sync.WaitGroup
	
	// LLM client for making actual requests
	llmClient      *LLMClient
}

var (
	asyncQueueManager     *AsyncLLMQueueManager
	asyncQueueManagerOnce sync.Once
)

// getAsyncLLMQueueManager returns the global async queue manager instance
// Returns nil if not initialized
func getAsyncLLMQueueManager() *AsyncLLMQueueManager {
	return asyncQueueManager
}

// InitAsyncLLMQueue initializes the async LLM queue system
func InitAsyncLLMQueue(llmClient *LLMClient) *AsyncLLMQueueManager {
	asyncQueueManagerOnce.Do(func() {
		maxWorkers := 2 // Default max concurrent LLM requests
		if maxStr := os.Getenv("LLM_MAX_CONCURRENT_REQUESTS"); maxStr != "" {
			if max, err := strconv.Atoi(maxStr); err == nil && max > 0 {
				maxWorkers = max
			}
		}
		
		// Backpressure limits - configurable via environment variables
		maxHighPriorityQueue := 100 // High-priority requests (user chat) - allow more
		if maxStr := os.Getenv("LLM_MAX_HIGH_PRIORITY_QUEUE"); maxStr != "" {
			if max, err := strconv.Atoi(maxStr); err == nil && max > 0 {
				maxHighPriorityQueue = max
			}
		}
		
		maxLowPriorityQueue := 50 // Low-priority requests (background tasks) - limit to prevent backlog
		if maxStr := os.Getenv("LLM_MAX_LOW_PRIORITY_QUEUE"); maxStr != "" {
			if max, err := strconv.Atoi(maxStr); err == nil && max > 0 {
				maxLowPriorityQueue = max
			}
		}
		
		asyncQueueManager = &AsyncLLMQueueManager{
			highPriorityStack: make([]*AsyncLLMRequest, 0),
			lowPriorityStack:  make([]*AsyncLLMRequest, 0),
			responseQueue:     make(chan *AsyncLLMResponse, 100),
			workerPool:        make(chan struct{}, maxWorkers),
			maxWorkers:        maxWorkers,
			maxHighPriorityQueue: maxHighPriorityQueue,
			maxLowPriorityQueue:  maxLowPriorityQueue,
			requestMap:        make(map[string]*AsyncLLMRequest),
			shutdown:          make(chan struct{}),
			llmClient:         llmClient,
		}
		
		log.Printf("🚀 [ASYNC-LLM] Initialized async LLM queue system with %d workers", maxWorkers)
		log.Printf("🚀 [ASYNC-LLM] Backpressure limits: high-priority=%d, low-priority=%d", maxHighPriorityQueue, maxLowPriorityQueue)
		
		// Start queue processor
		asyncQueueManager.wg.Add(1)
		go asyncQueueManager.processQueue()
		
		// Start response handler
		asyncQueueManager.wg.Add(1)
		go asyncQueueManager.processResponses()
		
		// Start queue health monitor (logs queue sizes periodically)
		asyncQueueManager.wg.Add(1)
		go asyncQueueManager.monitorQueueHealth()
	})
	
	return asyncQueueManager
}

// EnqueueRequest adds a request to the appropriate priority stack (LIFO)
// Returns error if queue is full (backpressure)
func (aqm *AsyncLLMQueueManager) EnqueueRequest(req *AsyncLLMRequest) error {
	aqm.mu.Lock()
	defer aqm.mu.Unlock()
	
	// Generate ID if not provided
	if req.ID == "" {
		req.ID = fmt.Sprintf("req_%d", time.Now().UnixNano())
	}
	
	req.CreatedAt = time.Now()
	
	// Backpressure: Check queue sizes before enqueuing
	if req.Priority == PriorityHigh {
		// High-priority requests: check limit but allow more (user requests)
		if len(aqm.highPriorityStack) >= aqm.maxHighPriorityQueue {
			log.Printf("🚫 [ASYNC-LLM] HIGH priority queue full (%d/%d), rejecting request: %s", 
				len(aqm.highPriorityStack), aqm.maxHighPriorityQueue, req.ID)
			return fmt.Errorf("high-priority queue full (%d/%d requests)", len(aqm.highPriorityStack), aqm.maxHighPriorityQueue)
		}
		aqm.highPriorityStack = append(aqm.highPriorityStack, req)
		log.Printf("📥 [ASYNC-LLM] Enqueued HIGH priority request: %s (stack size: %d/%d)", 
			req.ID, len(aqm.highPriorityStack), aqm.maxHighPriorityQueue)
	} else {
		// Low-priority requests: enforce stricter limit to prevent backlog
		if len(aqm.lowPriorityStack) >= aqm.maxLowPriorityQueue {
			log.Printf("🚫 [ASYNC-LLM] LOW priority queue full (%d/%d), rejecting request: %s (backpressure)", 
				len(aqm.lowPriorityStack), aqm.maxLowPriorityQueue, req.ID)
			return fmt.Errorf("low-priority queue full (%d/%d requests) - backpressure applied", 
				len(aqm.lowPriorityStack), aqm.maxLowPriorityQueue)
		}
		aqm.lowPriorityStack = append(aqm.lowPriorityStack, req)
		log.Printf("📥 [ASYNC-LLM] Enqueued LOW priority request: %s (stack size: %d/%d)", 
			req.ID, len(aqm.lowPriorityStack), aqm.maxLowPriorityQueue)
	}
	
	// Store in request map for callback routing
	aqm.requestMap[req.ID] = req
	
	return nil
}

// processQueue continuously processes requests from priority stacks
func (aqm *AsyncLLMQueueManager) processQueue() {
	defer aqm.wg.Done()
	
	for {
		select {
		case <-aqm.shutdown:
			log.Printf("🛑 [ASYNC-LLM] Queue processor shutting down")
			return
		default:
			// Try to get a request from stacks (LIFO - pop from end)
			var req *AsyncLLMRequest
			
			aqm.mu.Lock()
			// Always check high priority first
			if len(aqm.highPriorityStack) > 0 {
				// Pop from end (LIFO)
				lastIdx := len(aqm.highPriorityStack) - 1
				req = aqm.highPriorityStack[lastIdx]
				aqm.highPriorityStack = aqm.highPriorityStack[:lastIdx]
				log.Printf("📤 [ASYNC-LLM] Popped HIGH priority request: %s", req.ID)
			} else if len(aqm.lowPriorityStack) > 0 {
				// Check if background LLM is disabled
				disableBackgroundLLM := strings.TrimSpace(os.Getenv("DISABLE_BACKGROUND_LLM")) == "1" || 
				                         strings.TrimSpace(os.Getenv("DISABLE_BACKGROUND_LLM")) == "true"
				if !disableBackgroundLLM {
					// Pop from end (LIFO)
					lastIdx := len(aqm.lowPriorityStack) - 1
					req = aqm.lowPriorityStack[lastIdx]
					aqm.lowPriorityStack = aqm.lowPriorityStack[:lastIdx]
					log.Printf("📤 [ASYNC-LLM] Popped LOW priority request: %s", req.ID)
				}
			}
			aqm.mu.Unlock()
			
			if req != nil {
				// Acquire worker slot
				select {
				case aqm.workerPool <- struct{}{}:
					// Worker slot acquired, process request
					aqm.wg.Add(1)
					go aqm.processRequest(req)
				case <-aqm.shutdown:
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
func (aqm *AsyncLLMQueueManager) processRequest(req *AsyncLLMRequest) {
	defer aqm.wg.Done()
	defer func() { <-aqm.workerPool }() // Release worker slot
	
	log.Printf("🔄 [ASYNC-LLM] Processing request: %s (type: %s)", req.ID, req.RequestType)
	
	// Check if request was cancelled
	select {
	case <-req.Context.Done():
		log.Printf("❌ [ASYNC-LLM] Request %s was cancelled", req.ID)
		aqm.sendResponse(&AsyncLLMResponse{
			RequestID:   req.ID,
			Response:    "",
			Error:       fmt.Errorf("request cancelled"),
			CompletedAt: time.Now(),
		})
		return
	default:
	}
	
	// Make the actual LLM HTTP call directly (bypassing semaphore system)
	response, err := aqm.makeLLMHTTPCall(req.Context, req.Prompt)
	
	// Send response to response queue
	aqm.sendResponse(&AsyncLLMResponse{
		RequestID:   req.ID,
		Response:    response,
		Error:       err,
		CompletedAt: time.Now(),
	})
	
	log.Printf("✅ [ASYNC-LLM] Completed request: %s (error: %v)", req.ID, err != nil)
}

// makeLLMHTTPCall makes the actual HTTP call to the LLM provider (bypassing semaphore)
func (aqm *AsyncLLMQueueManager) makeLLMHTTPCall(ctx context.Context, prompt string) (string, error) {
	client := aqm.llmClient
	log.Printf("🌐 [ASYNC-LLM] Making API call to provider: %s", client.config.LLMProvider)
	
	// Use context timeout if available, otherwise use client timeout
	// For code generation and other long-running tasks, context may have longer timeout
	httpTimeout := client.httpClient.Timeout
	if deadline, ok := ctx.Deadline(); ok {
		timeUntilDeadline := time.Until(deadline)
		// If context has longer timeout, use it (but cap at 10 minutes for safety)
		if timeUntilDeadline > httpTimeout && timeUntilDeadline < 10*time.Minute {
			httpTimeout = timeUntilDeadline
		} else if timeUntilDeadline > 10*time.Minute {
			httpTimeout = 10 * time.Minute
		}
		log.Printf("🌐 [ASYNC-LLM] Timeout: %v (context deadline: %v)", httpTimeout, deadline)
	} else {
		log.Printf("🌐 [ASYNC-LLM] Timeout: %v (no context deadline)", httpTimeout)
	}

	// Determine the API endpoint based on provider
	var apiURL string
	var apiKey string

	switch client.config.LLMProvider {
	case "openai":
		// Allow override of OpenAI base URL for local OpenAI-compatible servers (e.g., llama.cpp)
		if url, ok := client.config.Settings["openai_url"]; ok && strings.TrimSpace(url) != "" {
			apiURL = strings.TrimSpace(url)
			if !strings.HasSuffix(apiURL, "/v1/chat/completions") {
				apiURL = strings.TrimRight(apiURL, "/") + "/v1/chat/completions"
			}
		} else {
			apiURL = "https://api.openai.com/v1/chat/completions"
		}
		apiKey = client.config.LLMAPIKey
		log.Printf("🌐 [ASYNC-LLM] Using OpenAI API at %s", apiURL)
	case "anthropic":
		apiURL = "https://api.anthropic.com/v1/messages"
		apiKey = client.config.LLMAPIKey
		log.Printf("🌐 [ASYNC-LLM] Using Anthropic API")
	case "local", "ollama":
		// For local models, use Ollama. Allow override via settings["ollama_url"].
		if url, ok := client.config.Settings["ollama_url"]; ok && strings.TrimSpace(url) != "" {
			apiURL = normalizeOllamaURL(strings.TrimSpace(url))
		} else {
			apiURL = "http://localhost:11434/api/chat"
		}
		apiKey = ""
		log.Printf("🌐 [ASYNC-LLM] Using Ollama local API at %s", apiURL)
	default:
		log.Printf("❌ [ASYNC-LLM] Unsupported provider: %s", client.config.LLMProvider)
		return "", fmt.Errorf("unsupported LLM provider: %s", client.config.LLMProvider)
	}

	// Prepare the request based on provider
	var jsonData []byte
	var err error

	if client.config.LLMProvider == "local" {
		// Ollama uses a different format
		ollamaRequest := map[string]interface{}{
			"model": client.getModelName(),
			"messages": []map[string]string{
				{
					"role":    "user",
					"content": prompt,
				},
			},
			"stream": false,
		}
		jsonData, err = json.Marshal(ollamaRequest)
	} else {
		// OpenAI/Anthropic format
		request := LLMRequest{
			Model: client.getModelName(),
			Messages: []Message{
				{
					Role:    "user",
					Content: prompt,
				},
			},
			Temperature: 0.7,
			MaxTokens:   1000,
		}
		jsonData, err = json.Marshal(request)
	}

	if err != nil {
		return "", err
	}

	// Create HTTP request with context to allow cancellation
	log.Printf("🌐 [ASYNC-LLM] Making request to: %s", apiURL)
	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", err
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	// Track request duration
	requestStart := time.Now()

	// Create a temporary HTTP client with longer timeout for async calls
	// Use context timeout if available, otherwise use client timeout
	httpClient := client.httpClient
	if deadline, ok := ctx.Deadline(); ok {
		timeUntilDeadline := time.Until(deadline)
		// If context has longer timeout, create a new client with that timeout
		if timeUntilDeadline > httpClient.Timeout && timeUntilDeadline < 10*time.Minute {
			httpClient = &http.Client{
				Timeout:   timeUntilDeadline,
				Transport: httpClient.Transport,
			}
		} else if timeUntilDeadline > 10*time.Minute {
			httpClient = &http.Client{
				Timeout:   10 * time.Minute,
				Transport: httpClient.Transport,
			}
		}
	}

	// Make the request (will respect context cancellation)
	resp, err := httpClient.Do(req)
	
	requestDuration := time.Since(requestStart)
	
	// Warn if request took too long
	if requestDuration > 10*time.Second {
		log.Printf("⚠️ [ASYNC-LLM] Slow request detected: %v", requestDuration)
	} else {
		log.Printf("⏱️ [ASYNC-LLM] Request completed in %v", requestDuration)
	}
	if err != nil {
		log.Printf("❌ [ASYNC-LLM] HTTP request failed: %v", err)
		return "", err
	}
	defer resp.Body.Close()

	log.Printf("🌐 [ASYNC-LLM] Received HTTP response with status: %d", resp.StatusCode)

	// Read response
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Printf("❌ [ASYNC-LLM] Failed to read response body: %v", err)
		return "", err
	}

	log.Printf("🌐 [ASYNC-LLM] Response body length: %d bytes", len(body))

	// Check for HTTP errors
	if resp.StatusCode != http.StatusOK {
		log.Printf("❌ [ASYNC-LLM] API error (status %d): %s", resp.StatusCode, string(body))
		return "", fmt.Errorf("LLM API error: %s", string(body))
	}

	// Parse response based on provider
	if client.config.LLMProvider == "local" {
		// Ollama response format
		log.Printf("🌐 [ASYNC-LLM] Parsing Ollama response")
		var ollamaResp map[string]interface{}
		if err := json.Unmarshal(body, &ollamaResp); err != nil {
			log.Printf("❌ [ASYNC-LLM] Failed to parse Ollama JSON: %v", err)
			return "", err
		}

		// Extract message content
		if message, ok := ollamaResp["message"].(map[string]interface{}); ok {
			if content, ok := message["content"].(string); ok {
				log.Printf("✅ [ASYNC-LLM] Successfully extracted content from Ollama response")
				
				// Try to extract token usage from Ollama response if available
				if promptEvalCount, ok := ollamaResp["prompt_eval_count"].(float64); ok {
					evalCount := 0.0
					if ec, ok := ollamaResp["eval_count"].(float64); ok {
						evalCount = ec
					}
					totalTokens := int(promptEvalCount + evalCount)
					if totalTokens > 0 {
						component := getComponentFromContext(ctx)
						trackTokenUsage(ctx, int(promptEvalCount), int(evalCount), totalTokens, component)
					}
				}
				
				return content, nil
			}
		}
		log.Printf("❌ [ASYNC-LLM] Could not extract content from Ollama response")
		return "", fmt.Errorf("could not extract content from Ollama response")
	} else {
		// OpenAI/Anthropic response format
		log.Printf("🌐 [ASYNC-LLM] Parsing %s response", client.config.LLMProvider)
		var llmResp LLMResponse
		if err := json.Unmarshal(body, &llmResp); err != nil {
			log.Printf("❌ [ASYNC-LLM] Failed to parse %s JSON: %v", client.config.LLMProvider, err)
			return "", err
		}

		// Check for API errors
		if llmResp.Error != nil {
			log.Printf("❌ [ASYNC-LLM] API error: %s", llmResp.Error.Message)
			return "", fmt.Errorf("LLM API error: %s", llmResp.Error.Message)
		}

		// Extract content
		if len(llmResp.Choices) == 0 {
			log.Printf("❌ [ASYNC-LLM] No choices in response")
			return "", fmt.Errorf("no response from LLM")
		}

		log.Printf("✅ [ASYNC-LLM] Successfully extracted content from %s response", client.config.LLMProvider)
		
		// Track token usage if available
		if llmResp.Usage != nil {
			component := getComponentFromContext(ctx)
			trackTokenUsage(ctx, llmResp.Usage.PromptTokens, llmResp.Usage.CompletionTokens, llmResp.Usage.TotalTokens, component)
		}
		
		return llmResp.Choices[0].Message.Content, nil
	}
}

// sendResponse sends a response to the response queue
func (aqm *AsyncLLMQueueManager) sendResponse(resp *AsyncLLMResponse) {
	select {
	case aqm.responseQueue <- resp:
		log.Printf("📨 [ASYNC-LLM] Sent response for request: %s", resp.RequestID)
	case <-time.After(5 * time.Second):
		log.Printf("⚠️ [ASYNC-LLM] Response queue full, dropping response for: %s", resp.RequestID)
	}
}

// QueueStats represents current queue statistics
type QueueStats struct {
	HighPrioritySize      int
	LowPrioritySize       int
	MaxHighPriorityQueue  int
	MaxLowPriorityQueue   int
	ActiveWorkers         int
	MaxWorkers            int
	HighPriorityPercent   float64
	LowPriorityPercent    float64
}

// GetStats returns current queue statistics (thread-safe)
func (aqm *AsyncLLMQueueManager) GetStats() QueueStats {
	aqm.mu.Lock()
	defer aqm.mu.Unlock()
	
	highSize := len(aqm.highPriorityStack)
	lowSize := len(aqm.lowPriorityStack)
	activeWorkers := aqm.maxWorkers - len(aqm.workerPool)
	
	highPercent := float64(highSize) / float64(aqm.maxHighPriorityQueue) * 100
	lowPercent := float64(lowSize) / float64(aqm.maxLowPriorityQueue) * 100
	
	return QueueStats{
		HighPrioritySize:     highSize,
		LowPrioritySize:      lowSize,
		MaxHighPriorityQueue: aqm.maxHighPriorityQueue,
		MaxLowPriorityQueue:  aqm.maxLowPriorityQueue,
		ActiveWorkers:        activeWorkers,
		MaxWorkers:           aqm.maxWorkers,
		HighPriorityPercent:  highPercent,
		LowPriorityPercent:   lowPercent,
	}
}

// monitorQueueHealth periodically logs queue sizes and health metrics
// Also implements auto-disable/enable of background LLM based on queue size
func (aqm *AsyncLLMQueueManager) monitorQueueHealth() {
	defer aqm.wg.Done()
	
	ticker := time.NewTicker(10 * time.Second) // Check every 10 seconds for auto-disable
	defer ticker.Stop()
	
	// Track auto-disable state
	var autoDisabled bool
	var redisClient *redis.Client
	
	// Try to get Redis client from environment (if available)
	// This is a best-effort - if Redis isn't available, we'll just log
	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "localhost:6379"
	}
	
	ctx := context.Background()
	rdb := redis.NewClient(&redis.Options{
		Addr: redisAddr,
	})
	// Test connection
	if err := rdb.Ping(ctx).Err(); err == nil {
		redisClient = rdb
		defer rdb.Close()
	} else {
		log.Printf("⚠️ [ASYNC-LLM] Redis not available for auto-disable: %v (auto-disable disabled)", err)
		rdb.Close()
	}
	
	// Thresholds for auto-disable/enable (configurable)
	disableThreshold := 0.90  // Disable when queue is 90% full
	enableThreshold := 0.50   // Re-enable when queue drops to 50%
	
	if thresholdStr := os.Getenv("LLM_AUTO_DISABLE_THRESHOLD"); thresholdStr != "" {
		if threshold, err := strconv.ParseFloat(thresholdStr, 64); err == nil && threshold > 0 && threshold <= 1.0 {
			disableThreshold = threshold
		}
	}
	if thresholdStr := os.Getenv("LLM_AUTO_ENABLE_THRESHOLD"); thresholdStr != "" {
		if threshold, err := strconv.ParseFloat(thresholdStr, 64); err == nil && threshold > 0 && threshold <= 1.0 {
			enableThreshold = threshold
		}
	}
	
	for {
		select {
		case <-aqm.shutdown:
			return
		case <-ticker.C:
			stats := aqm.GetStats()
			
			// Log queue health every 30 seconds (every 3rd tick)
			if time.Now().Second()%30 < 10 {
				if stats.HighPrioritySize > 0 || stats.LowPrioritySize > 0 {
					log.Printf("📊 [ASYNC-LLM] Queue health: high=%d/%d (%.1f%%) low=%d/%d (%.1f%%) active_workers=%d/%d",
						stats.HighPrioritySize, stats.MaxHighPriorityQueue, stats.HighPriorityPercent,
						stats.LowPrioritySize, stats.MaxLowPriorityQueue, stats.LowPriorityPercent,
						stats.ActiveWorkers, stats.MaxWorkers)
				}
				
				// Warn if queues are getting full
				if stats.HighPriorityPercent > 80 {
					log.Printf("⚠️ [ASYNC-LLM] High-priority queue is %d%% full (%d/%d)", 
						int(stats.HighPriorityPercent), stats.HighPrioritySize, stats.MaxHighPriorityQueue)
				}
				if stats.LowPriorityPercent > 80 {
					log.Printf("⚠️ [ASYNC-LLM] Low-priority queue is %d%% full (%d/%d) - backpressure may be applied", 
						int(stats.LowPriorityPercent), stats.LowPrioritySize, stats.MaxLowPriorityQueue)
				}
			}
			
			// Auto-disable/enable logic
			lowPercent := stats.LowPriorityPercent / 100.0
			
			if !autoDisabled && lowPercent >= disableThreshold {
				// Queue is too full - disable background LLM
				if redisClient != nil {
					err := redisClient.Set(ctx, "DISABLE_BACKGROUND_LLM", "1", 0).Err()
					if err == nil {
						autoDisabled = true
						log.Printf("🛑 [ASYNC-LLM] Auto-disabled background LLM (queue at %.1f%%, threshold: %.1f%%)", 
							stats.LowPriorityPercent, disableThreshold*100)
					}
				}
			} else if autoDisabled && lowPercent <= enableThreshold {
				// Queue has cleared - re-enable background LLM
				if redisClient != nil {
					err := redisClient.Del(ctx, "DISABLE_BACKGROUND_LLM").Err()
					if err == nil {
						autoDisabled = false
						log.Printf("✅ [ASYNC-LLM] Auto-re-enabled background LLM (queue at %.1f%%, threshold: %.1f%%)", 
							stats.LowPriorityPercent, enableThreshold*100)
					}
				}
			}
		}
	}
}

// processResponses processes completed LLM responses and calls callbacks
func (aqm *AsyncLLMQueueManager) processResponses() {
	defer aqm.wg.Done()
	
	for {
		select {
		case <-aqm.shutdown:
			log.Printf("🛑 [ASYNC-LLM] Response processor shutting down")
			return
		case resp := <-aqm.responseQueue:
			aqm.mu.Lock()
			req, exists := aqm.requestMap[resp.RequestID]
			if exists {
				delete(aqm.requestMap, resp.RequestID)
			}
			aqm.mu.Unlock()
			
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

// Shutdown gracefully shuts down the async queue system
func (aqm *AsyncLLMQueueManager) Shutdown() {
	log.Printf("🛑 [ASYNC-LLM] Shutting down async queue system...")
	close(aqm.shutdown)
	aqm.wg.Wait()
	log.Printf("✅ [ASYNC-LLM] Async queue system shut down")
}

// ========== END ASYNC LLM QUEUE SYSTEM ==========

type LLMClient struct {
	config     DomainConfig
	httpClient *http.Client
}

type LLMRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Temperature float64   `json:"temperature"`
	MaxTokens   int       `json:"max_tokens"`
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type LLMResponse struct {
	Choices []Choice  `json:"choices"`
	Error   *LLMError `json:"error,omitempty"`
	Usage   *Usage    `json:"usage,omitempty"`
}

type Choice struct {
	Message Message `json:"message"`
}

type LLMError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
}

func NewLLMClient(config DomainConfig) *LLMClient {
	// Default timeout
	timeout := 30 * time.Second
	// Allow override via settings.llm_timeout_seconds
	if v, ok := config.Settings["llm_timeout_seconds"]; ok && strings.TrimSpace(v) != "" {
		if sec, err := time.ParseDuration(strings.TrimSpace(v) + "s"); err == nil {
			timeout = sec
		}
	}
	
	// Create transport with longer dial timeout to handle slow connections
	// Dial timeout is separate from HTTP timeout - it controls how long to wait
	// to establish the TCP connection. Default is often too short for external services.
	dialTimeout := 30 * time.Second
	if timeout > dialTimeout {
		// Use a portion of the HTTP timeout for dial timeout, but cap at 60s
		dialTimeout = timeout / 2
		if dialTimeout > 60*time.Second {
			dialTimeout = 60 * time.Second
		}
	}
	
	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   dialTimeout,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxIdleConns:        100,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
	}
	
	return &LLMClient{
		config: config,
		httpClient: &http.Client{
			Timeout:   timeout,
			Transport: transport,
		},
	}
}

// GenerateMethod generates a method - automatically uses async queue if enabled
func (c *LLMClient) GenerateMethod(taskName, description string, context map[string]string) (*MethodDef, error) {
	log.Printf("🤖 [LLM] Generating method for task: %s", taskName)
	log.Printf("🤖 [LLM] Description: %s", description)
	log.Printf("🤖 [LLM] Context: %+v", context)

	// Create a prompt for the LLM to generate a method
	prompt := c.buildMethodPrompt(taskName, description, context)
	log.Printf("🤖 [LLM] Generated prompt length: %d characters", len(prompt))

	// Call the LLM (will use async queue if USE_ASYNC_LLM_QUEUE is set)
	log.Printf("🤖 [LLM] Calling LLM with provider: %s", c.config.LLMProvider)
	response, err := c.callLLM(prompt)
	if err != nil {
		log.Printf("❌ [LLM] LLM call failed: %v", err)
		return nil, err
	}
	log.Printf("🤖 [LLM] Received response length: %d characters", len(response))

	// Parse the response into a MethodDef
	log.Printf("🤖 [LLM] Parsing LLM response")
	method, err := c.parseMethodResponse(response, taskName)
	if err != nil {
		log.Printf("❌ [LLM] Failed to parse response: %v", err)
		return nil, fmt.Errorf("failed to parse LLM response: %v", err)
	}

	log.Printf("✅ [LLM] Successfully generated method: %+v", method)
	return method, nil
}

// GenerateExecutableCode generates executable code for a given task
func (c *LLMClient) GenerateExecutableCode(taskName, description, language string, context map[string]string) (string, error) {
	log.Printf("🤖 [LLM] Generating executable code for task: %s", taskName)
	log.Printf("🤖 [LLM] Language: %s", language)
	log.Printf("🤖 [LLM] Description: %s", description)
	log.Printf("🤖 [LLM] Context: %+v", context)

	// Create a prompt for code generation
	prompt := c.buildCodePrompt(taskName, description, language, context)
	log.Printf("🤖 [LLM] Generated code prompt length: %d characters", len(prompt))

	// Call the LLM
	log.Printf("🤖 [LLM] Calling LLM with provider: %s", c.config.LLMProvider)
	response, err := c.callLLM(prompt)
	if err != nil {
		log.Printf("❌ [LLM] LLM call failed: %v", err)
		return "", err
	}
	log.Printf("🤖 [LLM] Received response length: %d characters", len(response))

	// Extract code from response
	code := c.extractCodeFromResponse(response, language)
	log.Printf("✅ [LLM] Successfully generated code: %d characters", len(code))

	return code, nil
}

func (c *LLMClient) ExecuteTask(taskName, prompt string, context map[string]string) (string, error) {
	log.Printf("🤖 [LLM] Executing task: %s", taskName)
	log.Printf("🤖 [LLM] Prompt: %s", prompt)
	log.Printf("🤖 [LLM] Context: %+v", context)

	// Create a prompt for task execution
	executionPrompt := c.buildExecutionPrompt(taskName, prompt, context)
	log.Printf("🤖 [LLM] Generated execution prompt length: %d characters", len(executionPrompt))

	// Call the LLM
	log.Printf("🤖 [LLM] Calling LLM for task execution")
	response, err := c.callLLM(executionPrompt)
	if err != nil {
		log.Printf("❌ [LLM] Task execution failed: %v", err)
		return "", err
	}

	log.Printf("✅ [LLM] Task execution completed. Response length: %d characters", len(response))
	return response, nil
}

func (c *LLMClient) buildMethodPrompt(taskName, description string, context map[string]string) string {
	contextStr := ""
	if len(context) > 0 {
		contextStr = "\nContext:\n"
		for k, v := range context {
			contextStr += fmt.Sprintf("- %s: %s\n", k, v)
		}
	}

	return fmt.Sprintf(`You are an AI assistant that helps create hierarchical task network (HTN) methods for task planning.

Task: %s
Description: %s%s

Please create a method definition for this task. The method should:
1. Have appropriate preconditions (what must be true before this task can be executed)
2. Break down the task into subtasks (smaller, more manageable tasks)
3. Be realistic and practical

Return your response as a JSON object with this exact structure:
{
  "task": "%s",
  "preconditions": ["list", "of", "preconditions"],
  "subtasks": ["list", "of", "subtask", "names"]
}

Example:
{
  "task": "WriteEmail",
  "preconditions": ["not email_written"],
  "subtasks": ["DraftEmail", "ReviewEmail", "SendEmail"]
}

Your response:`, taskName, description, contextStr, taskName)
}

// buildCodePrompt creates a prompt for code generation
func (c *LLMClient) buildCodePrompt(taskName, description, language string, context map[string]string) string {
	prompt := fmt.Sprintf(`🚫 CRITICAL RESTRICTION - MUST FOLLOW:
- NEVER use Docker commands (docker run, docker build, docker exec, etc.) - Docker is NOT available
- NEVER use subprocess.run with docker commands - this will cause FileNotFoundError
- NEVER use os.system with docker commands - this will fail
- You are already running inside a container, do NOT try to create more containers

You are an expert programmer. Generate executable %s code for the following task:

Task: %s
Description: %s
Language: %s

UNIQUE TASK ID: %s_%d

Context:
`, language, taskName, description, language, taskName, time.Now().UnixNano())

	// Add file path information for data files
	filePathInfo := ""
	for key, value := range context {
		prompt += fmt.Sprintf("- %s: %s\n", key, value)
		// Check if this looks like a data file reference
		if (strings.Contains(strings.ToLower(key), "data") ||
			strings.Contains(strings.ToLower(key), "file") ||
			strings.Contains(strings.ToLower(key), "source") ||
			strings.Contains(strings.ToLower(key), "input")) &&
			strings.Contains(value, ".") && !strings.Contains(value, " ") {
			filePathInfo += fmt.Sprintf("\nIMPORTANT: The file '%s' is available at '/app/data/%s' in the container.\n", value, value)
		}
	}

	prompt += fmt.Sprintf(`%s
Requirements:
1. Generate complete, executable %s code
2. Include proper error handling
3. Add comments explaining the logic
4. Make the code robust and efficient
5. Use the correct file paths for any data files (see IMPORTANT notes above)
6. Use ONLY standard library modules when possible to minimize dependencies
7. If you must use external packages, use only the most essential ones
8. Output must compile cleanly (for Go: run with go build) with no unused variables or imports
9. Use ASCII identifiers only; no non-ASCII names
10. Do NOT perform network/API calls unless explicitly requested
11. NEVER use Docker commands (docker run, docker build, etc.) - Docker is not available in the execution environment
12. NEVER mention Docker, containers, or containerization in comments or strings
13. Return ONLY the code, no explanations or markdown formatting

Generate the %s code:`, filePathInfo, language, language)

	return prompt
}

// extractCodeFromResponse extracts code from LLM response
func (c *LLMClient) extractCodeFromResponse(response, language string) string {
	// Remove markdown code blocks if present
	response = strings.TrimSpace(response)

	// Look for code blocks
	if strings.Contains(response, "```") {
		lines := strings.Split(response, "\n")
		var codeLines []string
		inCodeBlock := false

		for _, line := range lines {
			if strings.HasPrefix(line, "```") {
				if inCodeBlock {
					break
				}
				inCodeBlock = true
				continue
			}
			if inCodeBlock {
				codeLines = append(codeLines, line)
			}
		}

		if len(codeLines) > 0 {
			return strings.Join(codeLines, "\n")
		}
	}

	// If no code blocks, return the response as-is
	return response
}

func (c *LLMClient) buildExecutionPrompt(taskName, prompt string, context map[string]string) string {
	contextStr := ""
	if len(context) > 0 {
		contextStr = "\nContext:\n"
		for k, v := range context {
			contextStr += fmt.Sprintf("- %s: %s\n", k, v)
		}
	}

	return fmt.Sprintf(`You are an AI assistant that executes tasks. 

Task: %s
Instructions: %s%s

Please provide a detailed response on how you would execute this task. Be specific about the steps you would take and any considerations.

Your response:`, taskName, prompt, contextStr)
}

func (c *LLMClient) getModelName() string {
	if model, exists := c.config.Settings["model"]; exists {
		return model
	}

	// Default models for each provider
	switch c.config.LLMProvider {
	case "openai":
		return "gpt-3.5-turbo"
	case "anthropic":
		return "claude-3-sonnet-20240229"
	case "local", "ollama":
		return "gemma3:12b"
	default:
		return "gpt-3.5-turbo"
	}
}

func (c *LLMClient) parseMethodResponse(response, taskName string) (*MethodDef, error) {
	// Try to extract JSON from the response
	// The LLM might return the JSON wrapped in markdown or other text
	jsonStart := -1
	jsonEnd := -1

	// Look for JSON object boundaries
	for i, char := range response {
		if char == '{' && jsonStart == -1 {
			jsonStart = i
		}
		if char == '}' && jsonStart != -1 {
			jsonEnd = i
			break
		}
	}

	if jsonStart == -1 || jsonEnd == -1 {
		return nil, fmt.Errorf("could not find JSON in LLM response")
	}

	// Extract JSON
	jsonStr := response[jsonStart : jsonEnd+1]

	// Parse JSON
	var method MethodDef
	if err := json.Unmarshal([]byte(jsonStr), &method); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %v", err)
	}

	// Validate the method
	if method.Task != taskName {
		return nil, fmt.Errorf("method task name mismatch: expected %s, got %s", taskName, method.Task)
	}

	if len(method.Subtasks) == 0 {
		return nil, fmt.Errorf("method has no subtasks")
	}

	return &method, nil
}

// --------- Mock LLM Client for Testing ---------

type MockLLMClient struct {
	responses map[string]string
}

func NewMockLLMClient() *LLMClient {
	// Return a real client but with mock behavior
	client := &LLMClient{
		config: DomainConfig{
			LLMProvider: "mock",
		},
		httpClient: &http.Client{Timeout: 1 * time.Second},
	}
	return client
}

func (c *LLMClient) callLLM(prompt string) (string, error) {
	// Default to low priority for background tasks
	ctx := context.Background()
	return c.callLLMWithContextAndPriority(ctx, prompt, PriorityLow)
}

func (c *LLMClient) callLLMWithContext(ctx context.Context, prompt string) (string, error) {
	// Default to low priority for background tasks
	return c.callLLMWithContextAndPriority(ctx, prompt, PriorityLow)
}

// callLLMWithContextAndPriority calls LLM with context and priority
// This is the main entry point - it routes to async or sync based on environment variable
func (c *LLMClient) callLLMWithContextAndPriority(ctx context.Context, prompt string, priority RequestPriority) (string, error) {
	// Mock responses for testing
	if c.config.LLMProvider == "mock" {
		return c.getMockResponse(prompt)
	}

	// Check if async queue system should be used
	useAsync := strings.TrimSpace(os.Getenv("USE_ASYNC_LLM_QUEUE")) == "1" || 
	           strings.TrimSpace(os.Getenv("USE_ASYNC_LLM_QUEUE")) == "true"
	
	if useAsync {
		return c.callLLMAsyncWithContextAndPriority(ctx, prompt, priority)
	}

	// For real providers, use the actual implementation with context and priority (sync)
	return c.callLLMRealWithContextAndPriority(ctx, prompt, priority)
}

// callLLMAsyncWithContextAndPriority calls LLM using the async queue system
func (c *LLMClient) callLLMAsyncWithContextAndPriority(ctx context.Context, prompt string, priority RequestPriority) (string, error) {
	// Initialize async queue if not already done
	queueMgr := InitAsyncLLMQueue(c)
	
	// Create a channel to receive the result
	resultChan := make(chan string, 1)
	errorChan := make(chan error, 1)
	
	// Create callback function
	callback := func(response string, err error) {
		if err != nil {
			errorChan <- err
			return
		}
		resultChan <- response
	}
	
	// Create async request
	req := &AsyncLLMRequest{
		Priority:    priority,
		Prompt:      prompt,
		RequestType: "LLMCall",
		RequestData: map[string]interface{}{
			"prompt": prompt,
		},
		Callback: callback,
		Context:  ctx,
	}
	
	// Enqueue the request
	if err := queueMgr.EnqueueRequest(req); err != nil {
		log.Printf("❌ [ASYNC-LLM] Failed to enqueue request: %v", err)
		return "", err
	}
	
	log.Printf("🚀 [ASYNC-LLM] Request enqueued: %s (priority: %v)", req.ID, priority)
	
	// Wait for result (with timeout from context or default)
	timeout := 10 * time.Minute // Long timeout for async processing
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
		log.Printf("❌ [ASYNC-LLM] Context cancelled while waiting for response")
		return "", ctx.Err()
	case <-time.After(timeout):
		log.Printf("❌ [ASYNC-LLM] Timeout waiting for async response")
		return "", fmt.Errorf("timeout waiting for async LLM response")
	}
}

func (c *LLMClient) callLLMReal(prompt string) (string, error) {
	// Use background context with low priority
	ctx := context.Background()
	return c.callLLMRealWithContextAndPriority(ctx, prompt, PriorityLow)
}

// callLLMRealWithContextAndPriority calls LLM with context and priority
func (c *LLMClient) callLLMRealWithContextAndPriority(ctx context.Context, prompt string, priority RequestPriority) (string, error) {
	// Initialize semaphore if not already done
	initLLMSemaphore()
	
	// Acquire semaphore slot with priority
	// For high-priority requests, use a longer timeout to ensure they get through even with background load
	// For code generation and other important background tasks, also use a longer timeout
	slotTimeout := c.httpClient.Timeout
	if priority == PriorityHigh {
		// High-priority requests get 2x the HTTP timeout to ensure they can wait for slots
		slotTimeout = c.httpClient.Timeout * 2
	} else {
		// For LOW priority requests, check if the context has a longer timeout
		// This allows code generation and other important background tasks to wait longer
		if deadline, ok := ctx.Deadline(); ok {
			timeUntilDeadline := time.Until(deadline)
			// If context timeout is significantly longer than HTTP timeout (e.g., > 2 minutes),
			// use a longer slot timeout to allow important background tasks to wait
			// Cap it at 10 minutes to prevent excessive waiting
			if timeUntilDeadline > 2*time.Minute {
				if timeUntilDeadline < 10*time.Minute {
					slotTimeout = timeUntilDeadline
				} else {
					slotTimeout = 10 * time.Minute
				}
			}
		}
		// If no deadline or deadline is short, use default HTTP timeout (no change)
	}
	if !acquireLLMSlot(ctx, priority, slotTimeout) {
		return "", fmt.Errorf("failed to acquire LLM slot (cancelled or timed out)")
	}
	defer func() { <-llmRequestSemaphore }()
	
	log.Printf("🌐 [LLM] Making API call to provider: %s", c.config.LLMProvider)
	log.Printf("🌐 [LLM] Timeout: %v", c.httpClient.Timeout)

	// Determine the API endpoint based on provider
	var apiURL string
	var apiKey string

	switch c.config.LLMProvider {
	case "openai":
		// Allow override of OpenAI base URL for local OpenAI-compatible servers (e.g., llama.cpp)
		if url, ok := c.config.Settings["openai_url"]; ok && strings.TrimSpace(url) != "" {
			apiURL = strings.TrimSpace(url)
			if !strings.HasSuffix(apiURL, "/v1/chat/completions") {
				apiURL = strings.TrimRight(apiURL, "/") + "/v1/chat/completions"
			}
		} else {
			apiURL = "https://api.openai.com/v1/chat/completions"
		}
		apiKey = c.config.LLMAPIKey
		log.Printf("🌐 [LLM] Using OpenAI API at %s", apiURL)
	case "anthropic":
		apiURL = "https://api.anthropic.com/v1/messages"
		apiKey = c.config.LLMAPIKey
		log.Printf("🌐 [LLM] Using Anthropic API")
	case "local", "ollama":
		// For local models, use Ollama. Allow override via settings["ollama_url"].
		if url, ok := c.config.Settings["ollama_url"]; ok && strings.TrimSpace(url) != "" {
			apiURL = normalizeOllamaURL(strings.TrimSpace(url))
		} else {
			apiURL = "http://localhost:11434/api/chat"
		}
		apiKey = ""
		log.Printf("🌐 [LLM] Using Ollama local API at %s", apiURL)
	default:
		log.Printf("❌ [LLM] Unsupported provider: %s", c.config.LLMProvider)
		return "", fmt.Errorf("unsupported LLM provider: %s", c.config.LLMProvider)
	}

	// Prepare the request based on provider
	var jsonData []byte
	var err error

	if c.config.LLMProvider == "local" {
		// Ollama uses a different format
		ollamaRequest := map[string]interface{}{
			"model": c.getModelName(),
			"messages": []map[string]string{
				{
					"role":    "user",
					"content": prompt,
				},
			},
			"stream": false,
		}
		jsonData, err = json.Marshal(ollamaRequest)
	} else {
		// OpenAI/Anthropic format
		request := LLMRequest{
			Model: c.getModelName(),
			Messages: []Message{
				{
					Role:    "user",
					Content: prompt,
				},
			},
			Temperature: 0.7,
			MaxTokens:   1000,
		}
		jsonData, err = json.Marshal(request)
	}

	if err != nil {
		return "", err
	}

	// Create HTTP request with context to allow cancellation
	log.Printf("🌐 [LLM] Making request to: %s", apiURL)
	log.Printf("🌐 [LLM] Request data: %s", string(jsonData))
	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", err
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	// Log request details (payload truncated to 8KB for readability)
	const maxLogBytes = 8 * 1024
	payloadPreview := jsonData
	if len(payloadPreview) > maxLogBytes {
		payloadPreview = payloadPreview[:maxLogBytes]
	}
	log.Printf("🌐 [LLM] Sending HTTP request to %s | provider=%s | model=%s | timeout=%s | payload_bytes=%d\nPayload Preview: %s",
		apiURL, c.config.LLMProvider, c.getModelName(), c.httpClient.Timeout.String(), len(jsonData), string(payloadPreview))

	// Track request duration to detect slow CPU inference
	requestStart := time.Now()

	// Make the request (will respect context cancellation)
	resp, err := c.httpClient.Do(req)
	
	requestDuration := time.Since(requestStart)
	
	// Warn if request took too long (likely CPU inference instead of GPU)
	if requestDuration > 10*time.Second {
		log.Printf("⚠️ [LLM] Slow request detected: %v (expected < 10s with GPU). This may indicate CPU inference instead of GPU.", requestDuration)
	} else {
		log.Printf("⏱️ [LLM] Request completed in %v", requestDuration)
	}
	if err != nil {
		log.Printf("❌ [LLM] HTTP request failed: %v", err)
		return "", err
	}
	defer resp.Body.Close()

	log.Printf("🌐 [LLM] Received HTTP response with status: %d", resp.StatusCode)

	// Read response
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Printf("❌ [LLM] Failed to read response body: %v", err)
		return "", err
	}

	log.Printf("🌐 [LLM] Response body length: %d bytes", len(body))

	// Check for HTTP errors
	if resp.StatusCode != http.StatusOK {
		log.Printf("❌ [LLM] API error (status %d): %s", resp.StatusCode, string(body))
		return "", fmt.Errorf("LLM API error: %s", string(body))
	}

	// Parse response based on provider
	if c.config.LLMProvider == "local" {
		// Ollama response format
		log.Printf("🌐 [LLM] Parsing Ollama response")
		var ollamaResp map[string]interface{}
		if err := json.Unmarshal(body, &ollamaResp); err != nil {
			log.Printf("❌ [LLM] Failed to parse Ollama JSON: %v", err)
			return "", err
		}

		// Extract message content
		if message, ok := ollamaResp["message"].(map[string]interface{}); ok {
			if content, ok := message["content"].(string); ok {
				log.Printf("✅ [LLM] Successfully extracted content from Ollama response")
				
				// Try to extract token usage from Ollama response if available
				// Ollama may include prompt_eval_count and eval_count in some responses
				if promptEvalCount, ok := ollamaResp["prompt_eval_count"].(float64); ok {
					evalCount := 0.0
					if ec, ok := ollamaResp["eval_count"].(float64); ok {
						evalCount = ec
					}
					totalTokens := int(promptEvalCount + evalCount)
					if totalTokens > 0 {
						component := getComponentFromContext(ctx)
						trackTokenUsage(ctx, int(promptEvalCount), int(evalCount), totalTokens, component)
					}
				}
				
				return content, nil
			}
		}
		log.Printf("❌ [LLM] Could not extract content from Ollama response")
		return "", fmt.Errorf("could not extract content from Ollama response")
	} else {
		// OpenAI/Anthropic response format
		log.Printf("🌐 [LLM] Parsing %s response", c.config.LLMProvider)
		var llmResp LLMResponse
		if err := json.Unmarshal(body, &llmResp); err != nil {
			log.Printf("❌ [LLM] Failed to parse %s JSON: %v", c.config.LLMProvider, err)
			return "", err
		}

		// Check for API errors
		if llmResp.Error != nil {
			log.Printf("❌ [LLM] API error: %s", llmResp.Error.Message)
			return "", fmt.Errorf("LLM API error: %s", llmResp.Error.Message)
		}

		// Extract content
		if len(llmResp.Choices) == 0 {
			log.Printf("❌ [LLM] No choices in response")
			return "", fmt.Errorf("no response from LLM")
		}

		log.Printf("✅ [LLM] Successfully extracted content from %s response", c.config.LLMProvider)
		
		// Track token usage if available
		if llmResp.Usage != nil {
			log.Printf("📊 [LLM] Token usage found: prompt=%d, completion=%d, total=%d", 
				llmResp.Usage.PromptTokens, llmResp.Usage.CompletionTokens, llmResp.Usage.TotalTokens)
			component := getComponentFromContext(ctx)
			trackTokenUsage(ctx, llmResp.Usage.PromptTokens, llmResp.Usage.CompletionTokens, llmResp.Usage.TotalTokens, component)
		} else {
			// Log response structure for debugging if usage is missing
			log.Printf("⚠️ [LLM] No 'usage' field in response. Response keys: checking structure...")
			// Re-parse as map to inspect structure
			var respMap map[string]interface{}
			if err := json.Unmarshal(body, &respMap); err == nil {
				keys := make([]string, 0, len(respMap))
				for k := range respMap {
					keys = append(keys, k)
				}
				log.Printf("📋 [LLM] Response top-level keys: %v", keys)
				// Log a snippet of the response for debugging (first 500 chars)
				bodyPreview := string(body)
				if len(bodyPreview) > 500 {
					bodyPreview = bodyPreview[:500] + "..."
				}
				log.Printf("📄 [LLM] Response preview: %s", bodyPreview)
			}
		}
		
		return llmResp.Choices[0].Message.Content, nil
	}
}

// trackTokenUsage records token usage in Redis for daily reporting
// component: identifier for the component making the LLM call (e.g., "hdn", "fsm", "wiki-summarizer", "news-ingestor")
// This function is safe to call even if Redis is not available
func trackTokenUsage(ctx context.Context, promptTokens, completionTokens, totalTokens int, component string) {
	if totalTokens == 0 {
		return // No tokens to track
	}
	
	// Normalize component name (default to "unknown" if empty)
	if component == "" {
		component = "unknown"
	}
	// Sanitize component name (remove special chars, lowercase)
	component = strings.ToLower(strings.TrimSpace(component))
	component = strings.ReplaceAll(component, " ", "-")
	component = strings.ReplaceAll(component, "_", "-")
	
	// Get Redis address from environment
	redisAddr := os.Getenv("REDIS_URL")
	if redisAddr == "" {
		// Try to construct default address
		redisAddr = "localhost:6379"
	}
	
	// Normalize Redis address (remove redis:// prefix if present)
	redisAddr = strings.TrimPrefix(redisAddr, "redis://")
	redisAddr = strings.TrimPrefix(redisAddr, "rediss://")
	
	// Create Redis client (will be reused if called multiple times)
	// We create a new client each time to avoid connection issues
	// In production, you might want to cache this
	redisClient := redis.NewClient(&redis.Options{
		Addr: redisAddr,
	})
	defer redisClient.Close()
	
	// Test connection
	if err := redisClient.Ping(ctx).Err(); err != nil {
		log.Printf("⚠️ [TOKEN-TRACK] Redis not available for token tracking: %v", err)
		return
	}
	
	// Get today's date key
	today := time.Now().UTC().Format("2006-01-02")
	
	// Update overall daily token totals
	// Use INCRBY for atomic increments
	promptKey := fmt.Sprintf("token_usage:%s:prompt", today)
	completionKey := fmt.Sprintf("token_usage:%s:completion", today)
	totalKey := fmt.Sprintf("token_usage:%s:total", today)
	
	if err := redisClient.IncrBy(ctx, promptKey, int64(promptTokens)).Err(); err != nil {
		log.Printf("⚠️ [TOKEN-TRACK] Failed to track prompt tokens: %v", err)
	} else {
		log.Printf("📊 [TOKEN-TRACK] Tracked %d prompt tokens (daily total updating)", promptTokens)
	}
	
	if err := redisClient.IncrBy(ctx, completionKey, int64(completionTokens)).Err(); err != nil {
		log.Printf("⚠️ [TOKEN-TRACK] Failed to track completion tokens: %v", err)
	} else {
		log.Printf("📊 [TOKEN-TRACK] Tracked %d completion tokens (daily total updating)", completionTokens)
	}
	
	if err := redisClient.IncrBy(ctx, totalKey, int64(totalTokens)).Err(); err != nil {
		log.Printf("⚠️ [TOKEN-TRACK] Failed to track total tokens: %v", err)
	} else {
		log.Printf("📊 [TOKEN-TRACK] Tracked %d total tokens (daily total updating)", totalTokens)
	}
	
	// Update per-component token totals
	componentPromptKey := fmt.Sprintf("token_usage:%s:component:%s:prompt", today, component)
	componentCompletionKey := fmt.Sprintf("token_usage:%s:component:%s:completion", today, component)
	componentTotalKey := fmt.Sprintf("token_usage:%s:component:%s:total", today, component)
	
	if err := redisClient.IncrBy(ctx, componentPromptKey, int64(promptTokens)).Err(); err != nil {
		log.Printf("⚠️ [TOKEN-TRACK] Failed to track prompt tokens for component %s: %v", component, err)
	} else {
		log.Printf("📊 [TOKEN-TRACK] Tracked %d prompt tokens for component %s", promptTokens, component)
	}
	
	if err := redisClient.IncrBy(ctx, componentCompletionKey, int64(completionTokens)).Err(); err != nil {
		log.Printf("⚠️ [TOKEN-TRACK] Failed to track completion tokens for component %s: %v", component, err)
	} else {
		log.Printf("📊 [TOKEN-TRACK] Tracked %d completion tokens for component %s", completionTokens, component)
	}
	
	if err := redisClient.IncrBy(ctx, componentTotalKey, int64(totalTokens)).Err(); err != nil {
		log.Printf("⚠️ [TOKEN-TRACK] Failed to track total tokens for component %s: %v", component, err)
	} else {
		log.Printf("📊 [TOKEN-TRACK] Tracked %d total tokens for component %s", totalTokens, component)
	}
	
	// Set expiration to 24 hours for individual records (will be aggregated hourly)
	expiration := 24 * time.Hour
	redisClient.Expire(ctx, promptKey, expiration)
	redisClient.Expire(ctx, completionKey, expiration)
	redisClient.Expire(ctx, totalKey, expiration)
	redisClient.Expire(ctx, componentPromptKey, expiration)
	redisClient.Expire(ctx, componentCompletionKey, expiration)
	redisClient.Expire(ctx, componentTotalKey, expiration)
}

// normalizeOllamaURL ensures the provided base URL includes the /api/chat endpoint.
// Accepts either http://host:11434 or http://host:11434/api/chat and returns the full endpoint.
func normalizeOllamaURL(base string) string {
	trimmed := strings.TrimRight(base, "/")
	if strings.HasSuffix(trimmed, "/api/chat") {
		return trimmed
	}
	if strings.HasSuffix(trimmed, "/api") {
		return trimmed + "/chat"
	}
	return trimmed + "/api/chat"
}

func (c *LLMClient) getMockResponse(prompt string) (string, error) {
	// Mock responses for testing only - should not be used in production
	// If you're seeing this, the LLM provider is set to "mock" instead of "local", "openai", or "anthropic"
	log.Printf("⚠️ [LLM] Using MOCK LLM - this should only be used for testing!")
	log.Printf("⚠️ [LLM] To use a real LLM, set LLM_PROVIDER environment variable or configure config.json")
	log.Printf("⚠️ [LLM] Valid providers: 'local' (Ollama), 'openai', 'anthropic'")
	
	// Simple mock responses based on prompt content for task planning
	if containsString(prompt, "WriteEmail") {
		return `{
  "task": "WriteEmail",
  "preconditions": ["not email_written"],
  "subtasks": ["DraftEmail", "ReviewEmail", "SendEmail"]
}`, nil
	}

	if containsString(prompt, "CreateReport") {
		return `{
  "task": "CreateReport",
  "preconditions": ["not report_created"],
  "subtasks": ["GatherData", "AnalyzeData", "WriteReport", "FormatReport"]
}`, nil
	}

	// Default response for task planning
	return `{
  "task": "GenericTask",
  "preconditions": ["not task_completed"],
  "subtasks": ["PrepareTask", "ExecuteTask", "VerifyTask"]
}`, nil
}

func (c *LLMClient) callLLMRealWithContext(ctx context.Context, prompt string) (string, error) {
	// Default to low priority for background tasks
	return c.callLLMRealWithContextAndPriority(ctx, prompt, PriorityLow)
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && s[:len(substr)] == substr ||
		len(s) > len(substr) && containsString(s[1:], substr)
}
