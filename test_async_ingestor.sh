#!/bin/bash
# Test script for BBC news ingestor with async LLM queue

echo "Testing BBC News Ingestor with Async LLM Queue"
echo "================================================"
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

echo "Running BBC news ingestor with async LLM queue..."
echo "Command: USE_ASYNC_LLM_QUEUE=1 /tmp/bbc-news-ingestor-test -llm -max 5 -batch-size 2 -dry -debug"
echo ""

# Run the ingestor with async queue enabled
USE_ASYNC_LLM_QUEUE=1 /tmp/bbc-news-ingestor-test -llm -max 5 -batch-size 2 -dry -debug 2>&1 | tee /tmp/ingestor_test.log

echo ""
echo "Checking for async LLM activity in output..."
echo ""

# Check for async LLM logs
if grep -q "\[ASYNC-LLM\]" /tmp/ingestor_test.log 2>/dev/null; then
    echo "✅ Found async LLM logs! The async queue system is working."
    echo ""
    echo "Async LLM log entries:"
    grep "\[ASYNC-LLM\]" /tmp/ingestor_test.log | head -10
else
    echo "❌ No async LLM logs found. The system is using synchronous calls."
    echo ""
    echo "To enable async queue:"
    echo "  1. Set environment variable: export USE_ASYNC_LLM_QUEUE=1"
    echo "  2. Run the ingestor again"
    echo ""
    echo "Recent output:"
    tail -20 /tmp/ingestor_test.log
fi





