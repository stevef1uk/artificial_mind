# How Async LLM Handling & Resumption Works - Simple Explanation

## 🎯 The Problem It Solves

**Before (Synchronous)**: When you make an LLM call, your code waits (blocks) until the LLM responds. If the LLM takes 2 minutes, your code waits 2 minutes. This causes:
- HTTP timeouts
- Blocked threads
- Poor resource utilization
- No way to prioritize urgent requests

**After (Asynchronous)**: Your code submits a request and immediately continues. The LLM response comes back later via a callback function. This means:
- No blocking
- No timeouts
- Better resource management
- Priority handling

## 🔄 The Flow (Step by Step)

### Step 1: Request Submission
```
Your Code → "I need LLM to generate code" → Enqueue Request
```

When your code needs an LLM call:
1. It creates a request with:
   - The prompt/question
   - A priority (HIGH for user requests, LOW for background tasks)
   - A callback function (what to do when response arrives)
   - A unique request ID

2. The request is **immediately added to a stack** (queue) and your code continues running

### Step 2: Request Queuing (LIFO - Last In, First Out)
```
Request → Priority Stack (HIGH or LOW) → Waits in line
```

The system has two stacks:
- **High Priority Stack**: User requests, chat messages, tool invocations
- **Low Priority Stack**: Background tasks, FSM learning, knowledge growth

**LIFO Behavior**: Most recent requests are processed first (like a stack of plates - you take from the top)

Example:
```
Stack: [Request A, Request B, Request C]  ← C is newest
Processing order: C → B → A  (newest first!)
```

### Step 3: Worker Pool Processing
```
Queue Processor → Checks stacks → Gets worker → Processes request
```

A background goroutine continuously:
1. Checks if there are requests in the stacks
2. Always processes HIGH priority first, then LOW priority
3. Takes the most recent request (LIFO)
4. Acquires a worker slot (limited by `LLM_MAX_CONCURRENT_REQUESTS`, default: 2)
5. If all workers are busy, request waits until a worker is free

**Worker Pool**: Limits how many LLM calls happen simultaneously (prevents overwhelming the LLM server)

### Step 4: Making the LLM Call
```
Worker → Makes HTTP call to LLM → Waits for response
```

Once a worker is assigned:
1. The worker makes the actual HTTP call to the LLM provider (Ollama, OpenAI, etc.)
2. This happens in a separate goroutine, so it doesn't block anything
3. The worker waits for the LLM response

### Step 5: Response Handling
```
LLM Response → Response Queue → Response Processor → Callback Function
```

When the LLM responds:
1. The response is sent to a **response queue** (buffered channel)
2. A separate **response processor** goroutine picks it up
3. It looks up the original request using the request ID
4. It calls the **callback function** that was provided when the request was created

### Step 6: Callback Execution (Resumption)
```
Callback Function → Your original code continues → Uses LLM response
```

The callback function is where your code "resumes":
- It receives the LLM response (or error)
- Your code can now process the response
- This happens asynchronously, so your main code flow wasn't blocked

## 📊 Visual Flow Diagram

```
┌─────────────────────────────────────────────────────────────┐
│                    YOUR CODE                                 │
│                                                               │
│  1. Need LLM? → Create request with callback                 │
│  2. Enqueue request → Returns immediately                    │
│  3. Continue doing other work...                             │
└─────────────────────────────────────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────────┐
│              ASYNC QUEUE SYSTEM                              │
│                                                               │
│  ┌─────────────────────────────────────────────────────┐   │
│  │  Priority Stacks (LIFO)                             │   │
│  │  HIGH: [C, B, A] ← newest first                     │   │
│  │  LOW:  [F, E, D] ← newest first                     │   │
│  └─────────────────────────────────────────────────────┘   │
│                          │                                   │
│                          ▼                                   │
│  ┌─────────────────────────────────────────────────────┐   │
│  │  Queue Processor (background goroutine)             │   │
│  │  - Checks stacks continuously                        │   │
│  │  - Pops most recent request (LIFO)                    │   │
│  │  - Acquires worker slot                              │   │
│  └─────────────────────────────────────────────────────┘   │
│                          │                                   │
│                          ▼                                   │
│  ┌─────────────────────────────────────────────────────┐   │
│  │  Worker Pool (max 2 concurrent)                     │   │
│  │  Worker 1: Processing Request C                    │   │
│  │  Worker 2: Processing Request B                    │   │
│  └─────────────────────────────────────────────────────┘   │
│                          │                                   │
│                          ▼                                   │
│  ┌─────────────────────────────────────────────────────┐   │
│  │  LLM HTTP Call                                      │   │
│  │  POST to Ollama/OpenAI/etc.                         │   │
│  └─────────────────────────────────────────────────────┘   │
│                          │                                   │
│                          ▼                                   │
│  ┌─────────────────────────────────────────────────────┐   │
│  │  Response Queue (buffered channel)                 │   │
│  │  [Response C, Response B, ...]                     │   │
│  └─────────────────────────────────────────────────────┘   │
│                          │                                   │
│                          ▼                                   │
│  ┌─────────────────────────────────────────────────────┐   │
│  │  Response Processor (background goroutine)         │   │
│  │  - Looks up original request by ID                 │   │
│  │  - Calls callback function                         │   │
│  └─────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────────┐
│              YOUR CALLBACK FUNCTION                         │
│                                                               │
│  func(response string, err error) {                        │
│    // Your code resumes here!                               │
│    // Process the LLM response                             │
│    // Continue with your logic                              │
│  }                                                           │
└─────────────────────────────────────────────────────────────┘
```

## 🔑 Key Concepts

### 1. **LIFO (Last In, First Out)**
Think of it like a stack of plates:
- New requests go on top
- Processing takes from the top (most recent first)
- This ensures recent requests get priority within their priority level

### 2. **Priority Levels**
- **HIGH**: User-initiated requests (chat, tools, immediate needs)
- **LOW**: Background tasks (FSM learning, knowledge growth, batch processing)

High priority requests are **always** processed before low priority, regardless of when they arrived.

### 3. **Worker Pool**
- Limits concurrent LLM calls (default: 2)
- Prevents overwhelming the LLM server
- If all workers are busy, new requests wait in the stack

### 4. **Callback Functions**
- Each request includes a callback function
- When the LLM responds, the callback is called
- This is how your code "resumes" after the async call

### 5. **Request Map**
- Maps request IDs to original requests
- Used to route responses back to the correct callback
- Cleans up after callback is called

## 💡 Example Scenario

**Scenario**: User asks a question while FSM is doing background learning

```
Time 0s:  User asks "What is 2+2?" → HIGH priority request A
Time 1s:  FSM starts learning → LOW priority request B
Time 2s:  User asks another question → HIGH priority request C
Time 3s:  FSM does more learning → LOW priority request D

Processing Order:
1. Request C (HIGH, newest) ← processed first
2. Request A (HIGH, older)  ← processed second
3. Request D (LOW, newest)  ← processed third (after all HIGH done)
4. Request B (LOW, older)  ← processed last
```

## 🎯 Benefits

1. **No Blocking**: Your code doesn't wait for LLM responses
2. **No Timeouts**: Requests can wait in queue indefinitely (only HTTP call has timeout)
3. **Priority Handling**: Urgent requests processed first
4. **Resource Management**: Worker pool prevents overload
5. **Scalability**: Can queue many requests without blocking
6. **Resumption**: Callbacks let your code continue when response arrives

## 🔧 Configuration

```bash
# Enable async queue
export USE_ASYNC_LLM_QUEUE=1

# Limit concurrent workers (default: 2)
export LLM_MAX_CONCURRENT_REQUESTS=3

# Disable background LLM work (only process HIGH priority)
export DISABLE_BACKGROUND_LLM=1
```

## 📝 Summary

**Simple Analogy**: It's like ordering food at a restaurant:
1. You place your order (enqueue request) and get a ticket number
2. You can sit down and do other things (your code continues)
3. The kitchen (worker pool) prepares orders in priority order
4. When your order is ready (LLM responds), they call your number (callback)
5. You pick up your food (process the response)

The key difference: You don't stand at the counter waiting - you get notified when it's ready!

