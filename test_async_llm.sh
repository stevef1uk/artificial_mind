#!/bin/bash
# Test script to verify async LLM queue system is working

echo "Testing Async LLM Queue System"
echo "================================"
echo ""

# Check if server is running
if ! curl -s http://localhost:8081/health > /dev/null; then
    echo "❌ HDN server is not running on port 8081"
    exit 1
fi

echo "✅ HDN server is running"
echo ""

# Check if async queue is enabled
if [ -z "$USE_ASYNC_LLM_QUEUE" ] || [ "$USE_ASYNC_LLM_QUEUE" != "1" ] && [ "$USE_ASYNC_LLM_QUEUE" != "true" ]; then
    echo "⚠️  USE_ASYNC_LLM_QUEUE is not set. The async queue system is NOT enabled."
    echo "   To enable it, set: export USE_ASYNC_LLM_QUEUE=1"
    echo ""
    echo "   Note: You'll need to restart the HDN server after setting this variable."
    echo ""
fi

echo "Making a test request to /api/v1/learn/llm endpoint..."
echo ""

# Make a test request to trigger GenerateMethod
RESPONSE=$(curl -s -X POST http://localhost:8081/api/v1/learn/llm \
  -H "Content-Type: application/json" \
  -d '{
    "task_name": "TestAsyncQueue",
    "description": "Test task to verify async LLM queue is working",
    "context": {}
  }')

echo "Response:"
echo "$RESPONSE" | jq '.' 2>/dev/null || echo "$RESPONSE"
echo ""

echo "Checking logs for async LLM activity..."
echo ""

# Check for async LLM logs in the last 50 lines
if tail -50 /tmp/hdn_server.log 2>/dev/null | grep -q "\[ASYNC-LLM\]"; then
    echo "✅ Found async LLM logs! The async queue system is working."
    echo ""
    echo "Recent async LLM log entries:"
    tail -50 /tmp/hdn_server.log 2>/dev/null | grep "\[ASYNC-LLM\]" | tail -5
else
    echo "❌ No async LLM logs found. The system is using the old semaphore-based approach."
    echo ""
    echo "To enable async queue:"
    echo "  1. Set environment variable: export USE_ASYNC_LLM_QUEUE=1"
    echo "  2. Restart the HDN server"
    echo ""
    echo "Recent LLM log entries (old system):"
    tail -50 /tmp/hdn_server.log 2>/dev/null | grep "\[LLM\]" | tail -3
fi





