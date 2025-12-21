# LLM Priority Queue Implementation

## Overview
Implemented a priority queue system for LLM requests to ensure user-facing requests (chat, tools) get priority over background tasks (FSM autonomy, learning, etc.).

## Test Results
✅ **LLM Model Tool Call Support**: Confirmed that `Qwen2.5-VL-7B-Instruct:latest` correctly supports tool calls and returns valid JSON tool_call responses.

## Implementation Details

### Priority Levels
- **PriorityHigh**: User-facing requests (chat, conversational AI, tools from UI)
- **PriorityLow**: Background tasks (FSM autonomy, learning, knowledge assessment)

### Architecture

1. **Priority Queue System** (`hdn/llm_client.go`):
   - Two separate queues: `highPriorityQueue` and `lowPriorityQueue`
   - Dispatcher goroutine that always serves high priority first
   - Semaphore-based slot management (max 2 concurrent by default)

2. **Request Flow**:
   - User requests → `ConversationalLLMAdapter` → `PriorityHigh`
   - Background tasks → Default LLM calls → `PriorityLow`
   - Flexible interpreter defaults to `PriorityHigh` (assumes user requests)

3. **Key Changes**:
   - `acquireLLMSlot()`: Enqueues requests based on priority
   - `dispatchLLMRequests()`: Continuously dispatches, prioritizing high-priority queue
   - `callLLMWithContextAndPriority()`: New method accepting priority parameter
   - `ConversationalLLMAdapter.GenerateResponse()`: Uses `PriorityHigh`
   - `FlexibleInterpreter`: Defaults to high priority for user requests

## Usage

### User Requests (High Priority)
- Chat API (`/api/v1/chat`)
- Tools text entry (`/api/v1/intelligent/execute`)
- Conversational AI layer

### Background Tasks (Low Priority)
- FSM autonomy tasks
- Learning and knowledge assessment
- Hypothesis rating
- Curiosity goal generation

## Configuration

The priority queue respects existing LLM throttling:
- `LLM_MAX_CONCURRENT_REQUESTS`: Max concurrent LLM requests (default: 2)
- User requests will always be served before background tasks when slots are available

## Benefits

1. **User Experience**: Chat and tool requests no longer timeout due to background task backlog
2. **Resource Management**: Background tasks still get processed, but don't block user requests
3. **Backward Compatible**: Existing code defaults to low priority, maintaining current behavior

## Testing

To test the priority queue:
1. Start the system with background tasks running
2. Send a chat request ("what is science")
3. Check logs for "🔒 [LLM] Enqueuing HIGH priority request"
4. Verify the request completes even with background tasks queued

