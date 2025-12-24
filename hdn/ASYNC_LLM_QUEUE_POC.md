# Async LLM Queue System

## Overview

This document describes the async LLM queue system for the HDN server. The system replaces the semaphore-based approach with a proper priority/non-priority queue system that processes LLM requests asynchronously. **All LLM calls now use this async queue system when enabled.**

## Architecture

### Components

1. **Priority Stacks (LIFO)**
   - `highPriorityStack`: High priority requests (user requests, chat, tools)
   - `lowPriorityStack`: Low priority requests (background tasks, FSM, learning)
   - Both stacks use LIFO (Last In First Out) behavior - most recent requests are processed first

2. **Worker Pool**
   - Configurable number of concurrent workers (default: 2, configurable via `LLM_MAX_CONCURRENT_REQUESTS`)
   - Workers process requests from the priority stacks
   - High priority requests are always processed before low priority requests

3. **Response Queue**
   - Buffered channel that receives completed LLM responses
   - Processes responses and routes them back to the original caller via callbacks

4. **Request Map**
   - Maps request IDs to request objects for callback routing
   - Ensures responses are delivered to the correct callback function

## Key Features

- **Stack-like behavior (LIFO)**: Most recent requests are processed first within each priority level
- **True async processing**: Requests are queued, processed in background, and responses are delivered via callbacks
- **No timeouts in queue**: Requests can wait indefinitely in the queue (only HTTP timeouts apply)
- **Priority-based processing**: High priority requests always take precedence
- **Callback-based response handling**: Responses are routed back to the original caller via callback functions

## Usage

### Enabling Async Queue

Set the environment variable to enable the async queue system:

```bash
export USE_ASYNC_LLM_QUEUE=1
```

### Implementation

**All LLM calls are now routed through the async queue system when enabled.** The main entry point is `callLLMWithContextAndPriority()`, which automatically routes to async or sync based on the `USE_ASYNC_LLM_QUEUE` environment variable.

**All LLM methods use async queue:**
- `GenerateMethod()` - Generates HTN methods
- `GenerateExecutableCode()` - Generates executable code
- `ExecuteTask()` - Executes tasks via LLM
- `callLLMWithContextAndPriority()` - Generic LLM call (used by all methods)
- All other LLM calls throughout the codebase

### Example Flow

1. Any code calls an LLM method (e.g., `GenerateMethod()`, `GenerateExecutableCode()`, etc.)
2. The method calls `callLLMWithContextAndPriority()` internally
3. If `USE_ASYNC_LLM_QUEUE=1`, the async path is used:
   - Request is created with a callback function
   - Request is enqueued in the appropriate priority stack
   - Function waits for result (with timeout)
4. Queue processor picks up the request from the stack (LIFO)
5. Worker makes the HTTP call to the LLM provider
6. Response is sent to the response queue
7. Response handler processes the response and calls the callback
8. Callback returns the result to the original caller

## Configuration

- `LLM_MAX_CONCURRENT_REQUESTS`: Maximum number of concurrent LLM requests (default: 2)
- `USE_ASYNC_LLM_QUEUE`: Enable async queue system (default: disabled)
- `DISABLE_BACKGROUND_LLM`: Disable low priority requests (default: enabled)

## Benefits

1. **No blocking**: Requests are queued and processed asynchronously
2. **Better resource management**: Worker pool limits concurrent requests
3. **Priority handling**: High priority requests are always processed first
4. **Stack behavior**: Most recent requests are processed first (LIFO)
5. **Scalable**: Can handle many queued requests without blocking
6. **No timeouts in queue**: Requests can wait as long as needed (only HTTP timeouts apply)

## Future Enhancements

- ✅ **COMPLETED**: All LLM calls now use async queue (`GenerateExecutableCode`, `ExecuteTask`, etc.)
- Add request cancellation support
- Add metrics/monitoring for queue depth and processing times
- Add request retry logic
- Add request prioritization beyond high/low (e.g., numeric priorities)
- Add request batching for efficiency

## Testing

To test the async queue system:

1. Set `USE_ASYNC_LLM_QUEUE=1`
2. Make any LLM calls (e.g., `GenerateMethod`, `GenerateExecutableCode`, `ExecuteTask`, etc.)
3. Observe that requests are queued and processed asynchronously
4. Check logs for `[ASYNC-LLM]` prefixes to see the async flow

## Code Location

The async queue system is implemented in:
- `hdn/llm_client.go` - Lines 147-568 (AsyncLLMQueueManager and related functions)
- `hdn/llm_client.go` - Lines 1077-1135 (`callLLMWithContextAndPriority` - main routing function)
- `hdn/llm_client.go` - Lines 1137-1220 (`callLLMAsyncWithContextAndPriority` - async implementation)

All LLM methods automatically use the async queue through the unified `callLLMWithContextAndPriority()` entry point.

