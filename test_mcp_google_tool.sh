#!/bin/bash
# Test script to call the MCP read_google_data tool directly

# Default to localhost, but allow override via environment variable
MCP_URL="${MCP_URL:-http://localhost:8080/mcp}"

echo "Testing MCP Google Workspace tool..."
echo "MCP URL: $MCP_URL"
echo ""

# First, list available tools to verify read_google_data is available
echo "1. Listing available MCP tools..."
TOOLS_RESPONSE=$(curl -s -X POST "$MCP_URL" \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "id": 1,
    "method": "tools/list"
  }')

echo "$TOOLS_RESPONSE" | jq '.result.tools[] | select(.name == "read_google_data")' 2>/dev/null || echo "Tool read_google_data not found in response"
echo ""

# Test calling the read_google_data tool
echo "2. Calling read_google_data tool with query='recent emails', type='email'..."
CALL_RESPONSE=$(curl -s -X POST "$MCP_URL" \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "id": 2,
    "method": "tools/call",
    "params": {
      "name": "read_google_data",
      "arguments": {
        "query": "recent emails",
        "type": "email"
      }
    }
  }')

echo "Response:"
echo "$CALL_RESPONSE" | jq '.' 2>/dev/null || echo "$CALL_RESPONSE"
echo ""

# Check for errors
if echo "$CALL_RESPONSE" | jq -e '.error' > /dev/null 2>&1; then
  echo "❌ Error occurred:"
  echo "$CALL_RESPONSE" | jq '.error'
  exit 1
else
  echo "✅ Tool call successful!"
  echo "$CALL_RESPONSE" | jq '.result' 2>/dev/null || echo "$CALL_RESPONSE"
fi

