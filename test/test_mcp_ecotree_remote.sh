#!/bin/bash
# Test MCP scrape_url against a remote HDN server (e.g., RPI)

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Get server URL from command line or use default
SERVER_URL="${1:-http://localhost:8081}"
FROM_CITY="${2:-southampton}"
TO_CITY="${3:-newcastle}"

echo "============================================================"
echo "🧪 MCP EcoTree Test - Remote Server"
echo "============================================================"
echo ""
echo "📍 Server: $SERVER_URL"
echo "📍 Route: ${FROM_CITY^^} → ${TO_CITY^^}"
echo ""

# Check if server is running
echo "🔍 Checking if HDN server is running..."
if ! curl -s "${SERVER_URL}/health" > /dev/null 2>&1; then
    echo "❌ HDN server is not running at ${SERVER_URL}"
    echo ""
    echo "Usage: $0 [server_url] [from_city] [to_city]"
    echo "Example: $0 http://192.168.1.100:8081 southampton newcastle"
    exit 1
fi
echo "✅ HDN server is running"
echo ""

# TypeScript config
TS_CONFIG="import { test, expect } from '@playwright/test';

test('test', async ({ page }) => {
  await page.goto('https://ecotree.green/en/calculate-flight-co2');
  await page.getByRole('link', { name: 'Plane' }).click();
  await page.getByRole('textbox', { name: 'From To Via' }).click();
  await page.getByRole('textbox', { name: 'From To Via' }).fill('$FROM_CITY');
  await page.getByText('$(echo ${FROM_CITY^})').click();
  await page.locator('input[name=\"To\"]').click();
  await page.locator('input[name=\"To\"]').fill('$TO_CITY');
  await page.getByText('$(echo ${TO_CITY^})').click();
  await page.getByRole('link', { name: ' Calculate my emissions ' }).click();
});"

# Escape for JSON
TS_CONFIG_ESCAPED=$(echo "$TS_CONFIG" | jq -Rs .)

# Create MCP request
MCP_REQUEST=$(cat <<EOF
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "tools/call",
  "params": {
    "name": "scrape_url",
    "arguments": {
      "url": "https://ecotree.green/en/calculate-flight-co2",
      "typescript_config": $TS_CONFIG_ESCAPED
    }
  }
}
EOF
)

echo "📤 Sending MCP request..."
echo "   (This may take 30-60 seconds as Playwright runs on the server)"
echo ""

# Send request
RESPONSE=$(curl -s -X POST "${SERVER_URL}/mcp" \
  -H "Content-Type: application/json" \
  -d "$MCP_REQUEST")

# Check if response is valid JSON
if ! echo "$RESPONSE" | jq . > /dev/null 2>&1; then
    echo "❌ Invalid JSON response from server:"
    echo "$RESPONSE"
    exit 1
fi

# Check for errors
if echo "$RESPONSE" | jq -e '.error' > /dev/null 2>&1; then
    echo "❌ MCP Error:"
    echo "$RESPONSE" | jq '.error'
    exit 1
fi

# Extract content
CONTENT=$(echo "$RESPONSE" | jq -r '.result.content[0].text // empty')

if [ -n "$CONTENT" ]; then
    echo "============================================================"
    echo "📊 Results"
    echo "============================================================"
    echo ""
    echo "$CONTENT"
    echo ""
    
    # Extract CO2 value
    CO2_VALUE=$(echo "$CONTENT" | grep -oP 'CO2 Emissions: \K[\d,]+' || echo "")
    DISTANCE=$(echo "$CONTENT" | grep -oP 'Distance: \K[\d,]+' || echo "")
    
    if [ -n "$CO2_VALUE" ]; then
        echo "============================================================"
        echo "✅ Success!"
        echo "============================================================"
        echo ""
        echo "🎯 Key Results:"
        echo "   • CO2 Emissions: ${CO2_VALUE} kg"
        [ -n "$DISTANCE" ] && echo "   • Distance: ${DISTANCE} km"
        echo ""
        
        # Compare with expected
        if [ "$FROM_CITY" = "southampton" ] && [ "$TO_CITY" = "newcastle" ]; then
            if [ "$CO2_VALUE" = "292" ]; then
                echo "✅ Result matches expected value! (292 kg CO2)"
            else
                echo "⚠️  Result differs from expected (expected 292 kg, got ${CO2_VALUE} kg)"
            fi
        fi
    else
        echo "⚠️  Could not extract CO2 value from response"
    fi
else
    echo "⚠️  No content in response"
    echo ""
    echo "Full response:"
    echo "$RESPONSE" | jq .
fi

echo ""
echo "============================================================"
echo "✅ Test Complete!"
echo "============================================================"

