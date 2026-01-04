#!/bin/bash

# Test script to verify the fix for tool_http_get validation failure
# This tests the specific scenario that was failing:
# "Use tool_http_get to fetch recent Wikipedia articles about domain concepts"

set -e

HDN_URL="${HDN_URL:-http://localhost:8081}"
echo "🧪 Testing tool_http_get Fix"
echo "============================"
echo "HDN URL: $HDN_URL"
echo ""

# Check if HDN server is running
echo "🔍 Checking HDN server..."
if ! curl -s "$HDN_URL/health" > /dev/null 2>&1; then
    echo "❌ HDN server is not running at $HDN_URL"
    echo "   Please start it with: ./bin/hdn-server or go run hdn/main.go -mode=server"
    exit 1
fi
echo "✅ HDN server is running"
echo ""

# Test the specific failing scenario
echo "📋 Test: Use tool_http_get to fetch recent Wikipedia articles about domain concepts"
echo "-----------------------------------------------------------------------------------"
echo "This test verifies that:"
echo "  1. The task is detected as web intent (routes to tool execution), OR"
echo "  2. If code generation is used, it correctly calls tool_http_get via HTTP API"
echo ""

RESPONSE=$(curl -s -X POST "$HDN_URL/api/v1/intelligent/execute" \
  -H "Content-Type: application/json" \
  -d '{
    "task_name": "Use tool_http_get to fetch recent Wikipedia articles about domain concepts",
    "description": "Use tool_http_get to fetch recent Wikipedia articles about domain concepts",
    "context": {
      "agent_id": "agent_1",
      "goal_id": "test_goal_123",
      "project_id": "Test",
      "session_id": "test_session_123"
    },
    "language": "python",
    "force_regenerate": true,
    "max_retries": 2,
    "timeout": 120
  }')

# Extract key fields
SUCCESS=$(echo "$RESPONSE" | jq -r '.success // false')
ERROR=$(echo "$RESPONSE" | jq -r '.error // "none"')
RETRY_COUNT=$(echo "$RESPONSE" | jq -r '.retry_count // 0')
VALIDATION_STEPS=$(echo "$RESPONSE" | jq -r '.validation_steps // [] | length')
USED_CACHED=$(echo "$RESPONSE" | jq -r '.used_cached_code // false')
RESULT=$(echo "$RESPONSE" | jq -r '.result // "none"')

echo "Results:"
echo "  Success: $SUCCESS"
echo "  Error: $ERROR"
echo "  Retry Count: $RETRY_COUNT"
echo "  Validation Steps: $VALIDATION_STEPS"
echo "  Used Cached Code: $USED_CACHED"
echo ""

# Check validation steps for errors
if [ "$VALIDATION_STEPS" -gt 0 ]; then
    echo "Validation Step Details:"
    echo "$RESPONSE" | jq -r '.validation_steps[]? | "  Step: \(.step), Success: \(.success), Error: \(.error // "none")"'
    echo ""
fi

# Check if the fix worked
if [ "$SUCCESS" = "true" ]; then
    echo "✅ TEST PASSED: Execution succeeded!"
    if [ "$ERROR" != "none" ] && [ "$ERROR" != "null" ]; then
        echo "   (Note: There was an error message but success=true)"
    fi
    if [ -n "$RESULT" ] && [ "$RESULT" != "none" ] && [ "$RESULT" != "null" ]; then
        echo "   Result preview: ${RESULT:0:200}..."
    fi
    exit 0
elif [ "$ERROR" = "Code validation failed after all retry attempts" ]; then
    echo "❌ TEST FAILED: Still getting validation failure"
    echo ""
    echo "Full error details:"
    echo "$RESPONSE" | jq '.'
    exit 1
elif [ "$ERROR" != "none" ] && [ "$ERROR" != "null" ]; then
    echo "⚠️  TEST PARTIAL: Execution failed with different error:"
    echo "   Error: $ERROR"
    echo ""
    echo "This might indicate the fix is working (different error path) or a new issue."
    echo "Full response:"
    echo "$RESPONSE" | jq '.'
    exit 2
else
    echo "❌ TEST FAILED: Execution failed without clear error message"
    echo ""
    echo "Full response:"
    echo "$RESPONSE" | jq '.'
    exit 1
fi

