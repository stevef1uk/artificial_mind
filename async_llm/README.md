# Async LLM Client Package

## Overview

This package provides an async LLM queue system for command-line tools and other components that need to make LLM calls. It uses the same architecture as the HDN and FSM async queue systems.

## Features

- **Priority Stacks (LIFO)**: High and low priority queues with stack-like behavior
- **Worker Pool**: Configurable concurrent workers for processing requests
- **Response Queue**: Async response handling with callbacks
- **Multiple Format Support**: Supports both Ollama `/api/generate` and `/api/chat` formats
- **Automatic Fallback**: Falls back to synchronous calls if async queue is not enabled

## Usage

### Basic Usage

```go
import "async_llm"

ctx := context.Background()
response, err := async_llm.CallAsync(
    ctx,
    "ollama",                    // provider
    "http://localhost:11434/api/chat", // endpoint
    "gemma3:latest",            // model
    "Your prompt here",         // prompt
    nil,                        // messages (nil for prompt-based)
    async_llm.PriorityLow,      // priority
)
```

### With Messages Format

```go
messages := []map[string]string{
    {"role": "user", "content": "Your prompt here"},
}
response, err := async_llm.CallAsync(
    ctx,
    "ollama",
    "http://localhost:11434/api/chat",
    "gemma3:latest",
    "",  // empty prompt when using messages
    messages,
    async_llm.PriorityHigh,
)
```

## Configuration

Enable async queue:
```bash
export USE_ASYNC_LLM_QUEUE=1
```

Optional settings:
- `ASYNC_LLM_MAX_WORKERS`: Maximum concurrent workers (default: 3)
- `ASYNC_LLM_TIMEOUT_SECONDS`: HTTP timeout in seconds (default: 60)

## Components Using This Package

1. **Wiki Summarizer** (`cmd/wiki-summarizer/`)
   - Processes Wikipedia articles in batches
   - Uses async queue for LLM summarization calls

2. **BBC News Ingestor** (`cmd/bbc-news-ingestor/`)
   - Classifies news headlines in batches
   - Uses async queue for LLM classification calls

## Architecture

The async LLM client follows the same architecture as HDN and FSM:
- Priority stacks (high/low) with LIFO behavior
- Worker pool for concurrent processing
- Response queue with callback routing
- Automatic fallback to synchronous calls

## Benefits

- **No blocking**: Requests are queued and processed asynchronously
- **Better resource management**: Worker pool limits concurrent requests
- **Priority handling**: High priority requests processed first
- **Stack behavior**: Most recent requests processed first (LIFO)
- **Scalable**: Can handle many queued requests without blocking

