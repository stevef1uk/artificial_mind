#!/bin/bash
# Test script for Wiki Summarizer with async LLM queue

echo "Testing Wiki Summarizer with Async LLM Queue"
echo "=============================================="
echo ""

# Check if Ollama is running
if ! curl -s http://localhost:11434/api/tags > /dev/null 2>&1; then
    echo "❌ Ollama is not running or not accessible at http://localhost:11434"
    echo "   Please start Ollama first"
    exit 1
fi

echo "✅ Ollama is running"
echo ""

# Check if async queue is enabled
if [ -z "$USE_ASYNC_LLM_QUEUE" ] || [ "$USE_ASYNC_LLM_QUEUE" != "1" ] && [ "$USE_ASYNC_LLM_QUEUE" != "true" ]; then
    echo "⚠️  USE_ASYNC_LLM_QUEUE is not set. The async queue system is NOT enabled."
    echo "   To enable it, set: export USE_ASYNC_LLM_QUEUE=1"
    echo ""
fi

# Check if Weaviate/Qdrant is accessible
WEAVIATE_URL=${WEAVIATE_URL:-http://localhost:8080}
if ! curl -s "$WEAVIATE_URL/v1/.well-known/ready" > /dev/null 2>&1; then
    echo "⚠️  Weaviate is not accessible at $WEAVIATE_URL"
    echo "   The summarizer needs Weaviate to store articles"
    echo "   Continuing test anyway to verify async LLM queue..."
    echo ""
fi

echo "Running Wiki Summarizer with async LLM queue..."
echo "Command: USE_ASYNC_LLM_QUEUE=1 bin/wiki-summarizer -batch-size 2 -max-words 100 -domain General"
echo ""

# Run the summarizer with async queue enabled (limit to 3 articles for testing)
USE_ASYNC_LLM_QUEUE=1 timeout 120 bin/wiki-summarizer \
    -weaviate="$WEAVIATE_URL" \
    -redis="localhost:6379" \
    -llm-provider=ollama \
    -llm-endpoint="http://localhost:11434/api/chat" \
    -llm-model="gemma3:latest" \
    -batch-size=2 \
    -max-words=100 \
    -domain="General" \
    2>&1 | tee /tmp/wiki_summarizer_test.log

echo ""
echo "Checking for async LLM activity in output..."
echo ""

# Check for async LLM logs
if grep -q "\[ASYNC-LLM\]" /tmp/wiki_summarizer_test.log 2>/dev/null; then
    echo "✅ Found async LLM logs! The async queue system is working."
    echo ""
    echo "Async LLM log entries:"
    grep "\[ASYNC-LLM\]" /tmp/wiki_summarizer_test.log | head -15
    echo ""
    echo "Summary of activity:"
    echo "  - Initialized: $(grep -c 'Initialized async LLM' /tmp/wiki_summarizer_test.log)"
    echo "  - Enqueued: $(grep -c 'Enqueued.*request' /tmp/wiki_summarizer_test.log)"
    echo "  - Processed: $(grep -c 'Processing request' /tmp/wiki_summarizer_test.log)"
    echo "  - Completed: $(grep -c 'Request completed' /tmp/wiki_summarizer_test.log)"
    echo "  - Callbacks: $(grep -c 'Calling callback' /tmp/wiki_summarizer_test.log)"
else
    echo "❌ No async LLM logs found. The system is using synchronous calls."
    echo ""
    echo "To enable async queue:"
    echo "  1. Set environment variable: export USE_ASYNC_LLM_QUEUE=1"
    echo "  2. Run the summarizer again"
    echo ""
    echo "Recent output:"
    tail -20 /tmp/wiki_summarizer_test.log
fi





