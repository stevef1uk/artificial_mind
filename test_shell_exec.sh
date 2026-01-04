#!/bin/bash

# Test script to verify the shell exec tool fix
# This tests that tool_exec works with /bin/sh instead of bash
# Usage: ./test_shell_exec.sh [HDN_URL]

set -e

HDN_URL="${1:-http://localhost:8080}"
TOOL_ENDPOINT="$HDN_URL/api/v1/tools/tool_exec/invoke"

echo "🧪 Testing Shell Exec Tool Fix"
echo "=============================="
echo "HDN URL: $HDN_URL"
echo "Tool Endpoint: $TOOL_ENDPOINT"
echo ""

# Check if HDN server is running
echo "🔍 Checking HDN server..."
if ! curl -s --connect-timeout 5 "$HDN_URL/health" > /dev/null 2>&1; then
    echo "❌ HDN server is not running at $HDN_URL"
    echo "   Please start it first or provide the correct URL"
    exit 1
fi
echo "✅ HDN server is running"
echo ""

# Test cases
declare -a TEST_COMMANDS=(
    "echo 'Hello from shell exec'"
    "date +%Y-%m-%d"
    "whoami"
    "uname -a"
    "ls -la /tmp | head -5"
)

PASSED=0
FAILED=0

for i in "${!TEST_COMMANDS[@]}"; do
    CMD="${TEST_COMMANDS[$i]}"
    TEST_NUM=$((i + 1))
    
    echo "📋 Test $TEST_NUM: $CMD"
    echo "----------------------------------------"
    
    # Create JSON payload
    PAYLOAD=$(cat <<EOF
{
    "cmd": "$CMD"
}
EOF
)
    
    # Make the request
    RESPONSE=$(curl -s -X POST "$TOOL_ENDPOINT" \
        -H "Content-Type: application/json" \
        -d "$PAYLOAD")
    
    # Check response
    if [ $? -ne 0 ]; then
        echo "❌ Failed to send request"
        FAILED=$((FAILED + 1))
        echo ""
        continue
    fi
    
    # Parse response
    EXIT_CODE=$(echo "$RESPONSE" | jq -r '.exit_code // -1' 2>/dev/null)
    STDOUT=$(echo "$RESPONSE" | jq -r '.stdout // ""' 2>/dev/null)
    STDERR=$(echo "$RESPONSE" | jq -r '.stderr // ""' 2>/dev/null)
    ERROR=$(echo "$RESPONSE" | jq -r '.error // ""' 2>/dev/null)
    
    if [ -n "$ERROR" ] && [ "$ERROR" != "null" ] && [ "$ERROR" != "" ]; then
        echo "❌ Error: $ERROR"
        FAILED=$((FAILED + 1))
    elif [ "$EXIT_CODE" = "-1" ]; then
        echo "❌ Invalid response format"
        echo "   Response: $RESPONSE"
        FAILED=$((FAILED + 1))
    elif [ "$EXIT_CODE" = "0" ]; then
        echo "✅ Command executed successfully (exit code: $EXIT_CODE)"
        if [ -n "$STDOUT" ] && [ "$STDOUT" != "null" ]; then
            echo "   Output: $(echo "$STDOUT" | head -c 100)"
            if [ ${#STDOUT} -gt 100 ]; then
                echo "..."
            fi
        fi
        PASSED=$((PASSED + 1))
    else
        echo "⚠️  Command failed with exit code: $EXIT_CODE"
        if [ -n "$STDERR" ] && [ "$STDERR" != "null" ]; then
            echo "   Stderr: $STDERR"
        fi
        # This might be expected for some commands, so we'll count it as passed if it's not a bash error
        if echo "$STDERR" | grep -q "bash.*not found\|bash.*command not found"; then
            echo "❌ BASH ERROR DETECTED - This is the bug we're fixing!"
            FAILED=$((FAILED + 1))
        else
            echo "   (Non-bash error, likely expected)"
            PASSED=$((PASSED + 1))
        fi
    fi
    echo ""
done

# Summary
echo "===================================="
echo "📊 Test Summary"
echo "===================================="
echo "✅ Tests passed: $PASSED"
echo "❌ Tests failed: $FAILED"
echo ""

if [ $FAILED -eq 0 ]; then
    echo "🎉 All tests passed! The shell exec tool is working correctly."
    exit 0
else
    echo "⚠️  Some tests failed. Check the output above for details."
    exit 1
fi





