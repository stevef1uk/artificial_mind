package main

import (
	"bytes"
	"context"
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

// AsyncHTTPRequest represents an async HTTP request with callback
type AsyncHTTPRequest struct {
	ID           string
	Priority     RequestPriority
	Method       string
	URL          string
	Headers      map[string]string
	Body         []byte
	Callback     func(*http.Response, error)
	Context      context.Context
	CreatedAt    time.Time
	RequestType  string
}

// AsyncHTTPResponse represents a completed HTTP response
type AsyncHTTPResponse struct {
	RequestID   string
	Response    *http.Response
	Error       error
	CompletedAt time.Time
}

// AsyncHTTPClient manages async HTTP request queues
type AsyncHTTPClient struct {
	// Priority stacks (LIFO)
	highPriorityStack []*AsyncHTTPRequest
	lowPriorityStack  []*AsyncHTTPRequest
	
	// Response queue
	responseQueue chan *AsyncHTTPResponse
	
	// Worker pool
	workerPool     chan struct{}
	maxWorkers     int
	
	// Synchronization
	mu             sync.Mutex
	requestMap     map[string]*AsyncHTTPRequest
	shutdown       chan struct{}
	wg             sync.WaitGroup
	
	// HTTP client
	httpClient     *http.Client
}

// RequestPriority indicates the priority of an HTTP request
type RequestPriority int

const (
	PriorityLow  RequestPriority = iota // Background tasks
	PriorityHigh                        // User requests
)

var (
	asyncHTTPClient     *AsyncHTTPClient
	asyncHTTPClientOnce sync.Once
)

// InitAsyncHTTPClient initializes the async HTTP client system
func InitAsyncHTTPClient() *AsyncHTTPClient {
	asyncHTTPClientOnce.Do(func() {
		maxWorkers := 5 // Default max concurrent HTTP requests
		if maxStr := os.Getenv("FSM_MAX_CONCURRENT_HTTP_REQUESTS"); maxStr != "" {
			if max, err := strconv.Atoi(maxStr); err == nil && max > 0 {
				maxWorkers = max
			}
		}
		
		timeout := 30 * time.Second
		if timeoutStr := os.Getenv("FSM_HTTP_TIMEOUT_SECONDS"); timeoutStr != "" {
			if sec, err := strconv.Atoi(timeoutStr); err == nil && sec > 0 {
				timeout = time.Duration(sec) * time.Second
			}
		}
		
		asyncHTTPClient = &AsyncHTTPClient{
			highPriorityStack: make([]*AsyncHTTPRequest, 0),
			lowPriorityStack:  make([]*AsyncHTTPRequest, 0),
			responseQueue:     make(chan *AsyncHTTPResponse, 100),
			workerPool:        make(chan struct{}, maxWorkers),
			maxWorkers:        maxWorkers,
			requestMap:        make(map[string]*AsyncHTTPRequest),
			shutdown:          make(chan struct{}),
			httpClient:        &http.Client{Timeout: timeout},
		}
		
		log.Printf("🚀 [ASYNC-HTTP] Initialized async HTTP client with %d workers", maxWorkers)
		
		// Start queue processor
		asyncHTTPClient.wg.Add(1)
		go asyncHTTPClient.processQueue()
		
		// Start response handler
		asyncHTTPClient.wg.Add(1)
		go asyncHTTPClient.processResponses()
	})
	
	return asyncHTTPClient
}

// EnqueueRequest adds a request to the appropriate priority stack (LIFO)
func (ahc *AsyncHTTPClient) EnqueueRequest(req *AsyncHTTPRequest) error {
	ahc.mu.Lock()
	defer ahc.mu.Unlock()
	
	// Generate ID if not provided
	if req.ID == "" {
		req.ID = fmt.Sprintf("http_req_%d", time.Now().UnixNano())
	}
	
	req.CreatedAt = time.Now()
	
	// Store in request map for callback routing
	ahc.requestMap[req.ID] = req
	
	// Add to appropriate stack (LIFO - append to end, pop from end)
	if req.Priority == PriorityHigh {
		ahc.highPriorityStack = append(ahc.highPriorityStack, req)
		log.Printf("📥 [ASYNC-HTTP] Enqueued HIGH priority request: %s (stack size: %d)", req.ID, len(ahc.highPriorityStack))
	} else {
		ahc.lowPriorityStack = append(ahc.lowPriorityStack, req)
		log.Printf("📥 [ASYNC-HTTP] Enqueued LOW priority request: %s (stack size: %d)", req.ID, len(ahc.lowPriorityStack))
	}
	
	return nil
}

// processQueue continuously processes requests from priority stacks
func (ahc *AsyncHTTPClient) processQueue() {
	defer ahc.wg.Done()
	
	for {
		select {
		case <-ahc.shutdown:
			log.Printf("🛑 [ASYNC-HTTP] Queue processor shutting down")
			return
		default:
			// Try to get a request from stacks (LIFO - pop from end)
			var req *AsyncHTTPRequest
			
			ahc.mu.Lock()
			// Always check high priority first
			if len(ahc.highPriorityStack) > 0 {
				// Pop from end (LIFO)
				lastIdx := len(ahc.highPriorityStack) - 1
				req = ahc.highPriorityStack[lastIdx]
				ahc.highPriorityStack = ahc.highPriorityStack[:lastIdx]
				log.Printf("📤 [ASYNC-HTTP] Popped HIGH priority request: %s", req.ID)
			} else if len(ahc.lowPriorityStack) > 0 {
				// Pop from end (LIFO)
				lastIdx := len(ahc.lowPriorityStack) - 1
				req = ahc.lowPriorityStack[lastIdx]
				ahc.lowPriorityStack = ahc.lowPriorityStack[:lastIdx]
				log.Printf("📤 [ASYNC-HTTP] Popped LOW priority request: %s", req.ID)
			}
			ahc.mu.Unlock()
			
			if req != nil {
				// Acquire worker slot
				select {
				case ahc.workerPool <- struct{}{}:
					// Worker slot acquired, process request
					ahc.wg.Add(1)
					go ahc.processRequest(req)
				case <-ahc.shutdown:
					return
				}
			} else {
				// No requests available, brief sleep
				time.Sleep(100 * time.Millisecond)
			}
		}
	}
}

// processRequest processes a single HTTP request
func (ahc *AsyncHTTPClient) processRequest(req *AsyncHTTPRequest) {
	defer ahc.wg.Done()
	defer func() { <-ahc.workerPool }() // Release worker slot
	
	log.Printf("🔄 [ASYNC-HTTP] Processing request: %s (type: %s, method: %s, url: %s)", req.ID, req.RequestType, req.Method, req.URL)
	
	// Check if request was cancelled
	select {
	case <-req.Context.Done():
		log.Printf("❌ [ASYNC-HTTP] Request %s was cancelled", req.ID)
		ahc.sendResponse(&AsyncHTTPResponse{
			RequestID:   req.ID,
			Response:    nil,
			Error:       fmt.Errorf("request cancelled"),
			CompletedAt: time.Now(),
		})
		return
	default:
	}
	
	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(req.Context, req.Method, req.URL, bytes.NewReader(req.Body))
	if err != nil {
		ahc.sendResponse(&AsyncHTTPResponse{
			RequestID:   req.ID,
			Response:    nil,
			Error:       err,
			CompletedAt: time.Now(),
		})
		return
	}
	
	// Set headers
	for k, v := range req.Headers {
		httpReq.Header.Set(k, v)
	}
	
	// Make the HTTP call
	requestStart := time.Now()
	resp, err := ahc.httpClient.Do(httpReq)
	requestDuration := time.Since(requestStart)
	
	if err != nil {
		log.Printf("❌ [ASYNC-HTTP] HTTP request failed: %v (duration: %v)", err, requestDuration)
	} else {
		log.Printf("✅ [ASYNC-HTTP] HTTP request completed: %s (status: %d, duration: %v)", req.ID, resp.StatusCode, requestDuration)
	}
	
	// Send response to response queue
	ahc.sendResponse(&AsyncHTTPResponse{
		RequestID:   req.ID,
		Response:    resp,
		Error:       err,
		CompletedAt: time.Now(),
	})
}

// sendResponse sends a response to the response queue
func (ahc *AsyncHTTPClient) sendResponse(resp *AsyncHTTPResponse) {
	select {
	case ahc.responseQueue <- resp:
		log.Printf("📨 [ASYNC-HTTP] Sent response for request: %s", resp.RequestID)
	case <-time.After(5 * time.Second):
		log.Printf("⚠️ [ASYNC-HTTP] Response queue full, dropping response for: %s", resp.RequestID)
	}
}

// processResponses processes completed HTTP responses and calls callbacks
func (ahc *AsyncHTTPClient) processResponses() {
	defer ahc.wg.Done()
	
	for {
		select {
		case <-ahc.shutdown:
			log.Printf("🛑 [ASYNC-HTTP] Response processor shutting down")
			return
		case resp := <-ahc.responseQueue:
			ahc.mu.Lock()
			req, exists := ahc.requestMap[resp.RequestID]
			if exists {
				delete(ahc.requestMap, resp.RequestID)
			}
			ahc.mu.Unlock()
			
			if !exists {
				log.Printf("⚠️ [ASYNC-HTTP] No request found for response: %s", resp.RequestID)
				if resp.Response != nil {
					resp.Response.Body.Close()
				}
				continue
			}
			
			// Call the callback function
			log.Printf("📞 [ASYNC-HTTP] Calling callback for request: %s", resp.RequestID)
			if req.Callback != nil {
				// Call callback in a goroutine to avoid blocking
				go func(callback func(*http.Response, error), response *http.Response, err error) {
					defer func() {
						if r := recover(); r != nil {
							log.Printf("❌ [ASYNC-HTTP] Callback panic for request %s: %v", resp.RequestID, r)
						}
					}()
					callback(response, err)
				}(req.Callback, resp.Response, resp.Error)
			} else {
				log.Printf("⚠️ [ASYNC-HTTP] No callback provided for request: %s", resp.RequestID)
				if resp.Response != nil {
					resp.Response.Body.Close()
				}
			}
		}
	}
}

// Shutdown gracefully shuts down the async HTTP client
func (ahc *AsyncHTTPClient) Shutdown() {
	log.Printf("🛑 [ASYNC-HTTP] Shutting down async HTTP client...")
	close(ahc.shutdown)
	ahc.wg.Wait()
	log.Printf("✅ [ASYNC-HTTP] Async HTTP client shut down")
}

// PostAsync makes an async POST request
func (ahc *AsyncHTTPClient) PostAsync(ctx context.Context, url string, contentType string, body []byte, headers map[string]string, priority RequestPriority, callback func(*http.Response, error)) error {
	if headers == nil {
		headers = make(map[string]string)
	}
	headers["Content-Type"] = contentType
	
	req := &AsyncHTTPRequest{
		Method:      "POST",
		URL:         url,
		Headers:     headers,
		Body:        body,
		Callback:    callback,
		Context:     ctx,
		Priority:    priority,
		RequestType: "POST",
	}
	
	return ahc.EnqueueRequest(req)
}

// PostAsyncSync makes an async POST request and waits for the result
func (ahc *AsyncHTTPClient) PostAsyncSync(ctx context.Context, url string, contentType string, body []byte, headers map[string]string, priority RequestPriority) (*http.Response, error) {
	resultChan := make(chan *http.Response, 1)
	errorChan := make(chan error, 1)
	
	callback := func(resp *http.Response, err error) {
		if err != nil {
			errorChan <- err
			return
		}
		resultChan <- resp
	}
	
	if err := ahc.PostAsync(ctx, url, contentType, body, headers, priority, callback); err != nil {
		return nil, err
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
	case resp := <-resultChan:
		return resp, nil
	case err := <-errorChan:
		return nil, err
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(timeout):
		return nil, fmt.Errorf("timeout waiting for async HTTP response")
	}
}

// Post makes a POST request (uses async if enabled, sync otherwise)
func Post(ctx context.Context, url string, contentType string, body []byte, headers map[string]string) (*http.Response, error) {
	useAsync := strings.TrimSpace(os.Getenv("USE_ASYNC_HTTP_QUEUE")) == "1" || 
	           strings.TrimSpace(os.Getenv("USE_ASYNC_HTTP_QUEUE")) == "true"
	
	if useAsync {
		client := InitAsyncHTTPClient()
		priority := PriorityLow // Default to low priority for FSM background tasks
		return client.PostAsyncSync(ctx, url, contentType, body, headers, priority)
	}
	
	// Fallback to synchronous HTTP call
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	
	if headers != nil {
		for k, v := range headers {
			req.Header.Set(k, v)
		}
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	
	client := &http.Client{Timeout: 30 * time.Second}
	return client.Do(req)
}

// DoAsyncSync makes an async HTTP request and waits for the result
func (ahc *AsyncHTTPClient) DoAsyncSync(ctx context.Context, req *http.Request, priority RequestPriority) (*http.Response, error) {
	// Read body if present
	var body []byte
	if req.Body != nil {
		var err error
		body, err = io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		req.Body.Close()
	}
	
	// Extract headers
	headers := make(map[string]string)
	for k, v := range req.Header {
		if len(v) > 0 {
			headers[k] = v[0]
		}
	}
	
	resultChan := make(chan *http.Response, 1)
	errorChan := make(chan error, 1)
	
	callback := func(resp *http.Response, err error) {
		if err != nil {
			errorChan <- err
			return
		}
		resultChan <- resp
	}
	
	asyncReq := &AsyncHTTPRequest{
		Method:      req.Method,
		URL:         req.URL.String(),
		Headers:     headers,
		Body:        body,
		Callback:    callback,
		Context:     ctx,
		Priority:    priority,
		RequestType: req.Method,
	}
	
	if err := ahc.EnqueueRequest(asyncReq); err != nil {
		return nil, err
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
	case resp := <-resultChan:
		return resp, nil
	case err := <-errorChan:
		return nil, err
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(timeout):
		return nil, fmt.Errorf("timeout waiting for async HTTP response")
	}
}

// Do makes an HTTP request (uses async if enabled, sync otherwise)
func Do(ctx context.Context, req *http.Request) (*http.Response, error) {
	useAsync := strings.TrimSpace(os.Getenv("USE_ASYNC_HTTP_QUEUE")) == "1" || 
	           strings.TrimSpace(os.Getenv("USE_ASYNC_HTTP_QUEUE")) == "true"
	
	if useAsync {
		client := InitAsyncHTTPClient()
		priority := PriorityLow // Default to low priority for FSM background tasks
		return client.DoAsyncSync(ctx, req, priority)
	}
	
	// Fallback to synchronous HTTP call
	client := &http.Client{Timeout: 30 * time.Second}
	return client.Do(req)
}

