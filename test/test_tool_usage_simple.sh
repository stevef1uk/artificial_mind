#!/bin/bash

# Simple test to verify tools are being used
# Usage: ./test_tool_usage_simple.sh [HDN_URL]
# Default: http://localhost:8080

HDN_URL="${1:-http://localhost:8080}"

echo "🧪 Testing Tool Usage"
echo "HDN URL: $HDN_URL"
echo ""

# Test 1: List tools
echo "📋 Step 1: Listing available tools..."
TOOLS=$(curl -s "$HDN_URL/api/v1/tools")
TOOL_COUNT=$(echo "$TOOLS" | jq -r '.tools | length' 2>/dev/null || echo "0")
echo "Found $TOOL_COUNT tools"
if [ "$TOOL_COUNT" -gt "0" ]; then
    echo "$TOOLS" | jq -r '.tools[] | "  - \(.id): \(.name)"' 2>/dev/null | head -5
fi
echo ""

# Test 2: Direct tool invocation
echo "🔧 Step 2: Invoking tool_http_get directly..."
RESPONSE=$(curl -s -X POST "$HDN_URL/api/v1/tools/tool_http_get/invoke" \
    -H "Content-Type: application/json" \
    -d '{"url": "https://httpbin.org/get"}')
echo "Response:"
echo "$RESPONSE" | jq '.' 2>/dev/null || echo "$RESPONSE"
echo ""

# Test 3: Natural language request (interpret only - shows tool_call)
echo "💬 Step 3a: Natural language interpretation (should show tool_call)..."
NL_RESPONSE=$(curl -s -X POST "$HDN_URL/api/v1/interpret" \
    -H "Content-Type: application/json" \
    -d '{
        "input": "Use the HTTP GET tool to fetch https://httpbin.org/get",
        "session_id": "test_'$(date +%s)'"
    }')
echo "Response:"
echo "$NL_RESPONSE" | jq '.' 2>/dev/null || echo "$NL_RESPONSE"
echo ""

# Check if tool was used in interpretation
if echo "$NL_RESPONSE" | jq -e '.tool_call' >/dev/null 2>&1; then
    echo "✅ SUCCESS: Tool call detected in interpretation!"
    echo "Tool ID: $(echo "$NL_RESPONSE" | jq -r '.tool_call.tool_id')"
    echo "$NL_RESPONSE" | jq '.tool_call'
elif echo "$NL_RESPONSE" | jq -e '.interpretation.tool_call' >/dev/null 2>&1; then
    echo "✅ SUCCESS: Tool call detected!"
    echo "Tool ID: $(echo "$NL_RESPONSE" | jq -r '.interpretation.tool_call.tool_id')"
    echo "$NL_RESPONSE" | jq '.interpretation.tool_call'
else
    echo "⚠️  No tool_call in response - checking if tool was executed via tasks..."
    if echo "$NL_RESPONSE" | jq -e '.tasks[] | select(.task_name == "Tool Execution")' >/dev/null 2>&1; then
        echo "✅ Tool was executed via task execution"
    fi
fi
echo ""

# Test 3b: Test with different tools
echo "💬 Step 3b: Testing multiple tools..."
echo ""

echo "  Testing tool_file_read (with execution)..."
FILE_TEST=$(curl -s -X POST "$HDN_URL/api/v1/interpret/execute" \
    -H "Content-Type: application/json" \
    -d '{
        "input": "Read the file /etc/hostname using the file read tool",
        "session_id": "test_file_'$(date +%s)'"
    }')
if echo "$FILE_TEST" | jq -e '.execution_plan[].task.task_name == "Tool Execution"' >/dev/null 2>&1; then
    echo "    ✅ Tool executed (check metrics should increment)"
else
    echo "    ⚠️  Tool may not have been executed"
    echo "$FILE_TEST" | jq '.message // .error' 2>/dev/null | head -1
fi

echo "  Testing tool_ls (with execution)..."
LS_TEST=$(curl -s -X POST "$HDN_URL/api/v1/interpret/execute" \
    -H "Content-Type: application/json" \
    -d '{
        "input": "List the contents of /tmp directory using the ls tool",
        "session_id": "test_ls_'$(date +%s)'"
    }')
if echo "$LS_TEST" | jq -e '.execution_plan[].task.task_name == "Tool Execution"' >/dev/null 2>&1; then
    echo "    ✅ Tool executed (check metrics should increment)"
else
    echo "    ⚠️  Tool may not have been executed"
    echo "$LS_TEST" | jq '.message // .error' 2>/dev/null | head -1
fi
echo ""

# Test 4: Check tool usage metrics
echo "📊 Step 4: Checking tool usage metrics..."
METRICS=$(curl -s "$HDN_URL/api/v1/tools/calls/recent?limit=10")
CALL_COUNT=$(echo "$METRICS" | jq -r '.calls | length' 2>/dev/null || echo "0")
if [ "$CALL_COUNT" -gt "0" ]; then
    echo "Found $CALL_COUNT recent tool calls:"
    echo "$METRICS" | jq -r '.calls[] | "  - \(.tool_id) (\(.tool_name)) at \(.timestamp)"' 2>/dev/null | head -10
    echo ""
    echo "Tool usage summary:"
    echo "$METRICS" | jq -r '.calls[].tool_id' 2>/dev/null | sort | uniq -c | sort -rn
else
    echo "⚠️  No recent tool calls found"
fi
echo ""

