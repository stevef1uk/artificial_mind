#!/bin/bash
# Test script for scrape_url MCP tool with TypeScript/Playwright config
# Uses LLM to convert TypeScript config to Go code and execute it

set -e

HDN_URL="${HDN_URL:-http://localhost:8081}"
MCP_ENDPOINT="${MCP_ENDPOINT:-${HDN_URL}/mcp}"

echo "🧪 Testing scrape_url MCP Tool with TypeScript/Playwright Config"
echo "================================================================"
echo "HDN URL: $HDN_URL"
echo "MCP Endpoint: $MCP_ENDPOINT"
echo ""

# Check if scrape_url tool is available
echo "Checking if scrape_url tool is available..."
TOOLS_RESPONSE=$(curl -s -X POST "$MCP_ENDPOINT" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}')

if echo "$TOOLS_RESPONSE" | jq -e '.result.tools[] | select(.name == "scrape_url")' > /dev/null 2>&1; then
  echo "✅ scrape_url tool is available"
  echo "$TOOLS_RESPONSE" | jq -r '.result.tools[] | select(.name == "scrape_url") | "  Description: \(.description)"'
else
  echo "❌ scrape_url tool not found"
  echo "Available tools:"
  echo "$TOOLS_RESPONSE" | jq -r '.result.tools[]?.name' 2>/dev/null | head -10
  exit 1
fi

echo ""

# Read the TypeScript config file
TS_CONFIG_FILE="${TS_CONFIG_FILE:-/home/stevef/simple-test.ts}"
if [ ! -f "$TS_CONFIG_FILE" ]; then
  echo "❌ TypeScript config file not found: $TS_CONFIG_FILE"
  echo "   Set TS_CONFIG_FILE environment variable to specify a different path"
  exit 1
fi

# Read the TypeScript file content
TS_CONFIG_CONTENT=$(cat "$TS_CONFIG_FILE")

echo "📄 Using TypeScript config from: $TS_CONFIG_FILE"
echo "   Config size: $(wc -c < "$TS_CONFIG_FILE") bytes"
echo ""

# Create the JSON payload with the TypeScript config embedded
# Use jq to properly escape the TypeScript content
SCRAPE_PAYLOAD=$(jq -n \
  --arg url "https://example.com" \
  --arg ts_config "$TS_CONFIG_CONTENT" \
  '{
    "jsonrpc": "2.0",
    "id": 2,
    "method": "tools/call",
    "params": {
      "name": "scrape_url",
      "arguments": {
        "url": $url,
        "typescript_config": $ts_config
      }
    }
  }')

echo "🚀 Calling scrape_url with TypeScript config..."
echo "   URL: https://example.com"
echo "   This will parse the TypeScript/Playwright config and execute the operations directly"
echo ""

SCRAPE_RESPONSE=$(curl -s -X POST "$MCP_ENDPOINT" \
  -H "Content-Type: application/json" \
  -d "$SCRAPE_PAYLOAD")

echo "Response received:"
echo "$SCRAPE_RESPONSE" | jq '.' 2>/dev/null || echo "$SCRAPE_RESPONSE"

# Check for errors
if echo "$SCRAPE_RESPONSE" | jq -e '.error' > /dev/null 2>&1; then
  ERROR_MSG=$(echo "$SCRAPE_RESPONSE" | jq -r '.error.message')
  echo ""
  echo "❌ Error: $ERROR_MSG"
  exit 1
fi

echo ""
echo "✅ scrape_url with TypeScript config completed"

# Check for content
if echo "$SCRAPE_RESPONSE" | jq -e '.result.content' > /dev/null 2>&1; then
  CONTENT=$(echo "$SCRAPE_RESPONSE" | jq -r '.result.content[0].text' 2>/dev/null)
  if [ -n "$CONTENT" ]; then
    echo ""
    echo "📄 Content:"
    echo "$CONTENT" | head -50
    if [ $(echo "$CONTENT" | wc -l) -gt 50 ]; then
      echo "... (truncated)"
    fi
  fi
fi

# Check for data
if echo "$SCRAPE_RESPONSE" | jq -e '.result.data' > /dev/null 2>&1; then
  DATA=$(echo "$SCRAPE_RESPONSE" | jq -r '.result.data')
  echo ""
  echo "📊 Extracted data:"
  echo "$DATA" | jq '.' 2>/dev/null || echo "$DATA"
fi

echo ""
echo "==============================="
echo "Test complete!"
