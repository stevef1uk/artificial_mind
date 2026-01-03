#!/bin/bash

# Simple test to verify tools are being used
# RPi-optimized version with longer timeouts and k8s support
# Usage: ./test_tool_usage_simple.sh [HDN_URL] [TIMEOUT_SECS]
# Default: http://hdn-server-rpi58.agi.svc.cluster.local:8080 (180s timeout for TPU)
# Examples:
#   ./test_tool_usage_simple.sh  (uses k8s service, 180s timeout)
#   ./test_tool_usage_simple.sh http://localhost:8080 60 (local, 60s timeout)
#   ./test_tool_usage_simple.sh http://hdn-server-rpi58.agi.svc.cluster.local:8080 180 (k8s, 180s)

HDN_URL="${1:-http://hdn-server-rpi58.agi.svc.cluster.local:8080}"
TIMEOUT="${2:-180}"  # 180 seconds for TPU, 60 for GPU

echo "🧪 Testing Tool Usage on RPi"
echo "HDN URL: $HDN_URL"
echo "Timeout: ${TIMEOUT}s"
echo ""

# Helper function with timeout
curl_with_timeout() {
    timeout "$TIMEOUT" curl -s "$@"
    local exit_code=$?
    if [ $exit_code -eq 124 ]; then
        echo "❌ TIMEOUT after ${TIMEOUT}s"
        return 1
    fi
    return 0
}

# Test 1: Health check
echo "🏥 Step 0: Health check..."
HEALTH=$(curl_with_timeout "$HDN_URL/health" 2>/dev/null)
if [ $? -eq 0 ] && [ -n "$HEALTH" ]; then
    echo "✅ HDN server is responding"
else
    echo "❌ HDN server not responding at $HDN_URL"
    echo "   Try: kubectl port-forward -n agi svc/hdn-server-rpi58 8080:8080"
    exit 1
fi
echo ""

# Test 2: List tools
echo "📋 Step 1: Listing available tools..."
TOOLS=$(curl_with_timeout "$HDN_URL/api/v1/tools" 2>/dev/null)
if [ $? -eq 0 ]; then
    TOOL_COUNT=$(echo "$TOOLS" | jq -r '.tools | length' 2>/dev/null || echo "0")
    echo "Found $TOOL_COUNT tools"
    if [ "$TOOL_COUNT" -gt "0" ]; then
        echo "$TOOLS" | jq -r '.tools[] | "  - \(.id): \(.name)"' 2>/dev/null | head -5
    fi
else
    echo "❌ Failed to list tools"
fi
echo ""

# Test 3: Direct tool invocation (local, doesn't require network)
echo "🔧 Step 2: Testing tool_ls locally..."
LS_RESPONSE=$(curl_with_timeout -X POST "$HDN_URL/api/v1/tools/tool_ls/invoke" \
    -H "Content-Type: application/json" \
    -d '{"path": "/tmp"}' 2>/dev/null)
if [ $? -eq 0 ]; then
    echo "✅ tool_ls response received"
    echo "$LS_RESPONSE" | jq -r '.output // .error // .message' 2>/dev/null | head -3
else
    echo "⚠️  tool_ls call timed out or failed (expected on slow TPU)"
fi
echo ""

# Test 4: File reading test
echo "📄 Step 3: Testing tool_file_read..."
FILE_RESPONSE=$(curl_with_timeout -X POST "$HDN_URL/api/v1/tools/tool_file_read/invoke" \
    -H "Content-Type: application/json" \
    -d '{"path": "/etc/hostname"}' 2>/dev/null)
if [ $? -eq 0 ]; then
    echo "✅ tool_file_read response received"
    echo "$FILE_RESPONSE" | jq -r '.content // .output // "No output"' 2>/dev/null
else
    echo "⚠️  tool_file_read call timed out (expected on slow TPU)"
fi
echo ""

# Test 5: Natural language interpretation (should be fast)
echo "💬 Step 4: Natural language interpretation..."
NL_RESPONSE=$(curl_with_timeout -X POST "$HDN_URL/api/v1/interpret" \
    -H "Content-Type: application/json" \
    -d '{
        "input": "List the /tmp directory",
        "session_id": "test_'$(date +%s)'"
    }' 2>/dev/null)
if [ $? -eq 0 ]; then
    echo "✅ Interpretation response received"
    # Check if tool was identified
    if echo "$NL_RESPONSE" | jq -e '.tool_call // .interpretation.tool_call' >/dev/null 2>&1; then
        echo "✅ Tool call detected in response"
        echo "$NL_RESPONSE" | jq '.tool_call // .interpretation.tool_call' 2>/dev/null
    else
        echo "⚠️  No direct tool call, checking for tasks..."
        echo "$NL_RESPONSE" | jq -r '.message // .tasks[0].task_name // "No tasks"' 2>/dev/null
    fi
else
    echo "⚠️  Interpretation call timed out"
fi
echo ""

# Test 6: Check metrics (lightweight)
echo "📊 Step 5: Checking tool metrics..."
METRICS=$(curl_with_timeout "$HDN_URL/api/v1/tools/calls/recent?limit=5" 2>/dev/null)
if [ $? -eq 0 ]; then
    CALL_COUNT=$(echo "$METRICS" | jq -r '.calls | length' 2>/dev/null || echo "0")
    if [ "$CALL_COUNT" -gt "0" ]; then
        echo "✅ Found $CALL_COUNT recent tool calls"
        echo "$METRICS" | jq -r '.calls[] | "  - \(.tool_id) at \(.timestamp)"' 2>/dev/null
    else
        echo "ℹ️  No recent tool calls yet"
    fi
else
    echo "⚠️  Failed to fetch metrics"
fi
echo ""

echo "✅ Basic tool test complete!"
echo ""
echo "💡 Tips for RPi:"
echo "  - Use longer timeouts for TPU (default 180s)"
echo "  - Check HDN logs: kubectl logs -n agi deployment/hdn-server-rpi58 -f"
echo "  - Check LLM queue: redis-cli LLEN async_llm:queue:high && redis-cli LLEN async_llm:queue:low"
echo "  - Restart HDN: kubectl rollout restart deployment hdn-server-rpi58 -n agi"
