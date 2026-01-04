#!/bin/bash

# Test script to verify all tools are being used, not just HTTP GET
# Usage: ./test_all_tools.sh [HDN_URL]

HDN_URL="${1:-http://localhost:8080}"

echo "🧪 Testing All Tools Usage"
echo "HDN URL: $HDN_URL"
echo ""

# Test each tool with a specific request
declare -A TOOL_TESTS=(
    ["tool_http_get"]="Fetch https://httpbin.org/get using HTTP GET"
    ["tool_file_read"]="Read the file /etc/hostname using file read tool"
    ["tool_ls"]="List files in /tmp directory using ls tool"
    ["tool_html_scraper"]="Scrape https://httpbin.org/html using HTML scraper tool"
)

TOOLS_WORKING=0
TOOLS_FAILED=0

for TOOL_ID in "${!TOOL_TESTS[@]}"; do
    TEST_INPUT="${TOOL_TESTS[$TOOL_ID]}"
    echo "🔧 Testing $TOOL_ID..."
    echo "   Request: \"$TEST_INPUT\""
    
    RESPONSE=$(curl -s -X POST "$HDN_URL/api/v1/interpret" \
        -H "Content-Type: application/json" \
        -d "{
            \"input\": \"$TEST_INPUT\",
            \"session_id\": \"test_${TOOL_ID}_$(date +%s)\"
        }")
    
    # Check if tool was called
    DETECTED_TOOL=$(echo "$RESPONSE" | jq -r '.tool_call.tool_id // empty' 2>/dev/null)
    
    if [ -n "$DETECTED_TOOL" ] && [ "$DETECTED_TOOL" != "null" ]; then
        if [ "$DETECTED_TOOL" = "$TOOL_ID" ]; then
            echo "   ✅ Correct tool used: $DETECTED_TOOL"
            TOOLS_WORKING=$((TOOLS_WORKING + 1))
        else
            echo "   ⚠️  Wrong tool used: expected $TOOL_ID, got $DETECTED_TOOL"
            TOOLS_FAILED=$((TOOLS_FAILED + 1))
        fi
    else
        echo "   ❌ No tool_call detected"
        echo "   Response snippet: $(echo "$RESPONSE" | jq -r '.message // .error // "unknown"' 2>/dev/null | head -c 100)"
        TOOLS_FAILED=$((TOOLS_FAILED + 1))
    fi
    echo ""
done

# Summary
echo "===================================="
echo "📊 Test Summary"
echo "===================================="
echo "✅ Tools working: $TOOLS_WORKING"
echo "❌ Tools failed: $TOOLS_FAILED"
echo ""

# Check tool metrics
echo "📈 Recent tool usage from metrics:"
METRICS=$(curl -s "$HDN_URL/api/v1/tools/calls/recent?limit=20" 2>/dev/null)
if echo "$METRICS" | jq -e '.calls' >/dev/null 2>&1; then
    echo "$METRICS" | jq -r '.calls[] | "  - \(.tool_id) (\(.tool_name))"' 2>/dev/null | sort | uniq -c | sort -rn
else
    echo "  ⚠️  Could not fetch metrics"
fi









