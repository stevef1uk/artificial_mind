#!/bin/bash

# Test script for LLM-based tool creation agent
# This script executes a task that should generate reusable code,
# which the agent should evaluate and potentially create as a tool

set -e

API_URL="${HDN_URL:-http://localhost:8080}"
TEST_NAME="test_tool_creation"

echo "🧪 Testing LLM-Based Tool Creation Agent"
echo "=========================================="
echo "API URL: $API_URL"
echo ""

# Test 1: Execute a task that generates reusable code (JSON parser/transformer)
echo "📝 Test 1: Executing task that should generate reusable code"
echo "Task: Parse and transform JSON data"
echo ""

TASK_RESPONSE=$(curl -s -X POST "$API_URL/api/v1/intelligent/execute" \
  -H "Content-Type: application/json" \
  -d '{
    "task_name": "ParseJSONData",
    "description": "Create a Python function that parses JSON data and transforms it by extracting specific fields and normalizing the structure",
    "context": {
      "input": "{\"name\": \"test\", \"value\": 123}"
    },
    "language": "python",
    "force_regenerate": false,
    "max_retries": 2,
    "timeout": 60
  }')

echo "Response:"
echo "$TASK_RESPONSE" | jq '.' 2>/dev/null || echo "$TASK_RESPONSE"
echo ""

SUCCESS=$(echo "$TASK_RESPONSE" | jq -r '.success // false' 2>/dev/null || echo "false")

if [ "$SUCCESS" = "true" ]; then
    echo "✅ Task executed successfully"
    echo ""
    echo "🔍 Checking logs for tool creation agent activity..."
    echo ""
    echo "Look for these log messages:"
    echo "  - 🔍 [TOOL-CREATOR] LLM evaluation..."
    echo "  - ✅ [TOOL-CREATOR] LLM recommends tool creation"
    echo "  - ✅ [TOOL-CREATOR] Successfully created and registered tool"
    echo ""
    echo "If running locally, check logs with:"
    echo "  tail -f /path/to/hdn-server.log | grep TOOL-CREATOR"
    echo ""
    echo "If running on Kubernetes, check logs with:"
    echo "  kubectl logs -n agi deployment/hdn-server-rpi58 --tail=100 | grep TOOL-CREATOR"
    echo ""
else
    echo "❌ Task execution failed"
    ERROR=$(echo "$TASK_RESPONSE" | jq -r '.error // "unknown error"' 2>/dev/null || echo "unknown error")
    echo "Error: $ERROR"
    exit 1
fi

# Test 2: Check if any tools were created (list tools)
echo "📋 Test 2: Checking if new tools were created"
echo ""

TOOLS_RESPONSE=$(curl -s -X GET "$API_URL/api/v1/tools" \
  -H "Content-Type: application/json")

echo "Tools list:"
echo "$TOOLS_RESPONSE" | jq '.tools[] | {id: .id, name: .name, description: .description}' 2>/dev/null || echo "$TOOLS_RESPONSE"
echo ""

# Test 3: Execute another task that might generate a different type of reusable code
echo "📝 Test 3: Executing another task (HTTP client)"
echo "Task: Create an HTTP client function"
echo ""

TASK2_RESPONSE=$(curl -s -X POST "$API_URL/api/v1/intelligent/execute" \
  -H "Content-Type: application/json" \
  -d '{
    "task_name": "HTTPClient",
    "description": "Create a Python function that makes HTTP GET requests with retry logic and error handling",
    "context": {
      "url": "https://api.example.com/data"
    },
    "language": "python",
    "force_regenerate": false,
    "max_retries": 2,
    "timeout": 60
  }')

echo "Response:"
echo "$TASK2_RESPONSE" | jq '.' 2>/dev/null || echo "$TASK2_RESPONSE"
echo ""

SUCCESS2=$(echo "$TASK2_RESPONSE" | jq -r '.success // false' 2>/dev/null || echo "false")

if [ "$SUCCESS2" = "true" ]; then
    echo "✅ Second task executed successfully"
else
    echo "⚠️  Second task failed (this is okay for testing)"
fi

echo ""
echo "🎯 Test Summary"
echo "==============="
echo "1. Executed tasks that should generate reusable code"
echo "2. Tool creation agent should have evaluated the code"
echo "3. Check logs for tool creation activity"
echo ""
echo "Next steps:"
echo "- Check server logs for [TOOL-CREATOR] messages"
echo "- Verify tools were created via /api/v1/tools endpoint"
echo "- Test created tools to ensure they work correctly"

