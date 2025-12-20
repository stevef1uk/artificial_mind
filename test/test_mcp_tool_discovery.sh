#!/bin/bash
# Test to verify MCP tools are being discovered by the interpreter

set -e

HDN_URL="${HDN_URL:-http://localhost:8081}"

echo "🔍 Testing MCP Tool Discovery"
echo "=============================="
echo ""

# Test 1: Check if composite provider can discover tools
echo "Test 1: Simulating tool discovery..."
echo "This would require checking interpreter logs, but we can test via API:"
echo ""

# Test 2: Ask a question that should trigger MCP tool usage
echo "Test 2: Asking knowledge question that should use MCP tools..."
RESPONSE=$(curl -s -X POST "${HDN_URL}/api/v1/interpret/execute" \
  -H "Content-Type: application/json" \
  -d '{
    "input": "What is Biology? Use mcp_get_concept tool to query your knowledge base.",
    "session_id": "test_discovery_'$(date +%s)'"
  }')

echo "Response received..."
echo "$RESPONSE" | jq -r '.interpretation.tool_call // "No tool call found"' 2>/dev/null || echo "Could not parse response"

# Check if tool was called
if echo "$RESPONSE" | jq -e '.interpretation.tool_call.tool_id' > /dev/null 2>&1; then
  TOOL_ID=$(echo "$RESPONSE" | jq -r '.interpretation.tool_call.tool_id')
  if [[ "$TOOL_ID" == mcp_* ]]; then
    echo "✅ MCP tool was called: $TOOL_ID"
  else
    echo "⚠️  Tool was called but not MCP: $TOOL_ID"
  fi
else
  echo "⚠️  No tool call in response"
  echo "Full response:"
  echo "$RESPONSE" | jq '.' 2>/dev/null | head -30
fi

echo ""
echo "=============================="
echo "Note: Check HDN server logs for:"
echo "  - [COMPOSITE-TOOL-PROVIDER] Retrieved X total tools"
echo "  - [FLEXIBLE-LLM] Available tools: X"
echo "  - Tool execution logs"

