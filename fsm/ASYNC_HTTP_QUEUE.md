# Async HTTP Queue System for FSM Server

## Overview

The FSM server has been refactored to use an async HTTP queue system similar to the HDN server's async LLM queue. This allows FSM to queue HTTP requests to HDN and process them asynchronously, preventing blocking and improving resource management.

## Architecture

### Components

1. **Priority Stacks (LIFO)**
   - `highPriorityStack`: High priority HTTP requests
   - `lowPriorityStack`: Low priority HTTP requests (default for FSM background tasks)
   - Both stacks use LIFO (Last In First Out) behavior

2. **Worker Pool**
   - Configurable number of concurrent workers (default: 5, configurable via `FSM_MAX_CONCURRENT_HTTP_REQUESTS`)
   - Workers process HTTP requests from the priority stacks
   - High priority requests are always processed before low priority requests

3. **Response Queue**
   - Buffered channel that receives completed HTTP responses
   - Processes responses and routes them back to the original caller via callbacks

4. **Request Map**
   - Maps request IDs to request objects for callback routing
   - Ensures responses are delivered to the correct callback function

## Key Features

- **Stack-like behavior (LIFO)**: Most recent requests are processed first within each priority level
- **True async processing**: HTTP requests are queued, processed in background, and responses are delivered via callbacks
- **No timeouts in queue**: Requests can wait indefinitely in the queue (only HTTP timeouts apply)
- **Priority-based processing**: High priority requests always take precedence
- **Callback-based response handling**: Responses are routed back to the original caller via callback functions

## Usage

### Enabling Async Queue

Set the environment variable to enable the async HTTP queue system:

```bash
export USE_ASYNC_HTTP_QUEUE=1
```

### Implementation

**All HTTP calls to HDN are now routed through the async queue system when enabled.**

The main entry points are:
- `Post(ctx, url, contentType, body, headers)` - Makes POST requests
- `Do(ctx, req)` - Makes any HTTP request (GET, POST, etc.)

### Refactored Files

1. **knowledge_integration.go**
   - `extractMeaningfulFacts()` - Fact extraction via HDN interpret endpoint
   - `assessKnowledgeValue()` - Knowledge assessment via HDN interpret endpoint
   - `callMCPTool()` - MCP tool calls

2. **knowledge_growth.go**
   - `assessConceptValue()` - Concept assessment via HDN interpret endpoint

3. **engine.go**
   - `screenHypotheses()` - Hypothesis screening via HDN interpret endpoint

### Example Flow

1. FSM code calls `Post(ctx, url, contentType, body, headers)` or `Do(ctx, req)`
2. If `USE_ASYNC_HTTP_QUEUE=1`, the async path is used:
   - Request is created with a callback function
   - Request is enqueued in the appropriate priority stack
   - Function waits for result (with timeout)
3. Queue processor picks up the request from the stack (LIFO)
4. Worker makes the HTTP call to HDN
5. Response is sent to the response queue
6. Response handler processes the response and calls the callback
7. Callback returns the result to the original caller

## Configuration

- `FSM_MAX_CONCURRENT_HTTP_REQUESTS`: Maximum number of concurrent HTTP requests (default: 5)
- `USE_ASYNC_HTTP_QUEUE`: Enable async HTTP queue system (default: disabled)
- `FSM_HTTP_TIMEOUT_SECONDS`: HTTP request timeout in seconds (default: 30)
- `FSM_LLM_REQUEST_DELAY_MS`: Delay between requests in milliseconds (default: 5000)

## Benefits

1. **No blocking**: HTTP requests are queued and processed asynchronously
2. **Better resource management**: Worker pool limits concurrent requests
3. **Priority handling**: High priority requests are always processed first
4. **Stack behavior**: Most recent requests are processed first (LIFO)
5. **Scalable**: Can handle many queued requests without blocking
6. **No timeouts in queue**: Requests can wait as long as needed (only HTTP timeouts apply)

## Integration with HDN

The FSM server makes HTTP calls to HDN's `/api/v1/interpret` endpoint. When both systems use async queues:

1. FSM queues HTTP requests to HDN (async HTTP queue)
2. HDN receives the request and queues it for LLM processing (async LLM queue)
3. Both systems process requests asynchronously without blocking

This creates a fully async pipeline from FSM → HDN → LLM.

## Code Location

The async HTTP queue system is implemented in:
- `fsm/async_http_client.go` - Complete async HTTP client implementation
- `fsm/knowledge_integration.go` - Refactored to use async HTTP client
- `fsm/knowledge_growth.go` - Refactored to use async HTTP client
- `fsm/engine.go` - Refactored to use async HTTP client

## Testing

To test the async HTTP queue system:

1. Set `USE_ASYNC_HTTP_QUEUE=1`
2. Make HTTP calls from FSM (e.g., knowledge integration, hypothesis screening)
3. Observe that requests are queued and processed asynchronously
4. Check logs for `[ASYNC-HTTP]` prefixes to see the async flow

## Future Enhancements

- Add request cancellation support
- Add metrics/monitoring for queue depth and processing times
- Add request retry logic
- Add request prioritization beyond high/low (e.g., numeric priorities)
- Add request batching for efficiency

