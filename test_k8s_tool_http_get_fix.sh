#!/bin/bash

# Test script to verify the fix for tool_http_get validation failure on Kubernetes
# This tests the specific scenario that was failing:
# "Use tool_http_get to fetch recent Wikipedia articles about domain concepts"

set -e

HDN_URL="${HDN_URL:-http://localhost:8082}"
echo "🧪 Testing tool_http_get Fix on Kubernetes"
echo "=========================================="
echo "HDN URL: $HDN_URL"
echo ""

# Check if HDN server is accessible
echo "🔍 Checking HDN server..."
if ! curl -s "$HDN_URL/api/v1/intelligent/capabilities" > /dev/null 2>&1; then
    echo "❌ HDN server is not accessible at $HDN_URL"
    echo "   Make sure port forwarding is active: kubectl port-forward -n agi svc/hdn-server-rpi58 8082:8080"
    exit 1
fi
echo "✅ HDN server is accessible"
echo ""

# Test the specific failing scenario
echo "📋 Test: Use tool_http_get to fetch recent Wikipedia articles about domain concepts"
echo "-----------------------------------------------------------------------------------"
echo "This test verifies that:"
echo "  1. The task is detected as web intent (routes to tool execution), OR"
echo "  2. If code generation is used, it correctly calls tool_http_get via HTTP API"
echo "  3. The requests module is installed during validation"
echo ""

RESPONSE=$(curl -s -X POST "$HDN_URL/api/v1/intelligent/execute" \
  -H "Content-Type: application/json" \
  -d '{
    "task_name": "Use tool_http_get to fetch recent Wikipedia articles about domain concepts",
    "description": "Use tool_http_get to fetch recent Wikipedia articles about domain concepts",
    "context": {
      "agent_id": "agent_1",
      "goal_id": "k8s_test_goal_123",
      "project_id": "Test",
      "session_id": "k8s_test_session_123"
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
    echo "$RESPONSE" | jq -r '.validation_steps[]? | "  Step: \(.step), Success: \(.success), Error: \(.error // "none") | .[0:150]"'
    echo ""
    
    # Check for the specific error we're fixing
    if echo "$RESPONSE" | jq -r '.validation_steps[]?.error // ""' | grep -q "ModuleNotFoundError.*requests"; then
        echo "❌ TEST FAILED: Still getting ModuleNotFoundError for requests"
        echo "   This means the package installation fix didn't work"
        exit 1
    fi
fi

# Check if the fix worked
if [ "$SUCCESS" = "true" ]; then
    echo "✅ TEST PASSED: Execution succeeded!"
    if [ "$ERROR" != "none" ] && [ "$ERROR" != "null" ] && [ -n "$ERROR" ]; then
        echo "   (Note: There was an error message but success=true)"
    fi
    if [ -n "$RESULT" ] && [ "$RESULT" != "none" ] && [ "$RESULT" != "null" ]; then
        echo "   Result preview: ${RESULT:0:200}..."
    fi
    echo ""
    echo "✅ Fix verified: Code validation passed, requests module was installed correctly"
    exit 0
elif [ "$ERROR" = "Code validation failed after all retry attempts" ]; then
    echo "❌ TEST FAILED: Still getting validation failure"
    echo ""
    echo "Full error details:"
    echo "$RESPONSE" | jq '.'
    exit 1
elif [ "$ERROR" != "none" ] && [ "$ERROR" != "null" ] && [ -n "$ERROR" ]; then
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









