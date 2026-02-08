#!/bin/bash
# Quick test of smart_scrape with enhanced LLM prompts

HDN_URL="http://localhost:8082"
URL="${1:-https://www.nationwide.co.uk/savings/compare-savings-accounts-and-isas/}"
GOAL="${2:-Find the best savings account interest rate}"

echo "🧪 Testing Smart Scrape with Enhanced LLM Prompts"
echo "=================================================="
echo "URL: $URL"
echo "Goal: $GOAL"
echo ""

# Create MCP request
REQUEST=$(cat <<EOF
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "tools/call",
  "params": {
    "name": "smart_scrape",
    "arguments": {
      "url": "$URL",
      "goal": "$GOAL"
    }
  }
}
EOF
)

echo "📤 Sending request..."
echo ""

# Send request and capture response
RESPONSE=$(curl -s -X POST "$HDN_URL/mcp" \
  -H "Content-Type: application/json" \
  -d "$REQUEST")

# Check if valid JSON
if ! echo "$RESPONSE" | jq . > /dev/null 2>&1; then
    echo "❌ Invalid JSON response:"
    echo "$RESPONSE"
    exit 1
fi

# Check for errors
if echo "$RESPONSE" | jq -e '.error' > /dev/null 2>&1; then
    echo "❌ Error:"
    echo "$RESPONSE" | jq '.error'
    exit 1
fi

# Extract and display content
CONTENT=$(echo "$RESPONSE" | jq -r '.result.content[0].text // empty')

if [ -n "$CONTENT" ]; then
    echo "✅ Success!"
    echo ""
    echo "📊 Results:"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "$CONTENT"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
else
    echo "⚠️ No content in response"
    echo ""
    echo "Full response:"
    echo "$RESPONSE" | jq .
fi

echo ""
echo "💡 Check logs for debug output:"
echo "   tail -100 /tmp/hdn_test.log | grep 'SMART-SCRAPE'"
