#!/bin/bash
# Complete test script for MCP scrape_url with EcoTree Calculator
# This replicates the standalone Go/Python tests through the MCP server

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

echo "============================================================"
echo "🧪 MCP EcoTree Flight CO2 Calculator Test"
echo "============================================================"
echo ""

# Check if HDN server is running
echo "🔍 Checking if HDN server is running..."
if ! curl -s http://localhost:8081/health > /dev/null 2>&1; then
    echo "❌ HDN server is not running on port 8081"
    echo ""
    echo "Please start the server first:"
    echo "  ./restart_hdn.sh"
    echo ""
    exit 1
fi
echo "✅ HDN server is running"
echo ""

# Get command line arguments or use defaults
FROM_CITY="${1:-southampton}"
TO_CITY="${2:-newcastle}"

echo "📍 Testing route: ${FROM_CITY^^} → ${TO_CITY^^}"
echo ""

# TypeScript config - proven pattern
TS_CONFIG="await page.bypassConsent();
    await page.locator('#airportName').first().fill('$FROM_CITY'); 
    await page.waitForTimeout(3000); 
    await page.getByText('Southampton, United Kingdom').first().click(); 
    await page.waitForTimeout(1000); 
    await page.locator('#airportName').nth(1).fill('$TO_CITY'); 
    await page.waitForTimeout(3000); 
    await page.getByText('Newcastle, United Kingdom').first().click(); 
    await page.waitForTimeout(1000); 
    await page.locator('select').first().selectOption('return'); 
    await page.waitForTimeout(500); 
    await page.getByRole('link', { name: ' Calculate my emissions ' }).click(); 
    await page.waitForTimeout(10000);"

# Escape the TypeScript config for JSON
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
      "typescript_config": $TS_CONFIG_ESCAPED,
      "extractions": {
        "co2": "(\\\\d+)\\\\s+kg\\\\s+Your carbon emissions",
        "distance": "(\\\\d+)\\\\s+km\\\\s+Your travelled distance"
      }
    }
  }
}
EOF
)

echo "📤 Sending MCP request to HDN server..."
echo "   (This may take 30-60 seconds as Playwright runs)"
echo ""

# Send the request and capture response
RESPONSE=$(curl -s -X POST http://localhost:8081/mcp \
  -H "Content-Type: application/json" \
  -d "$MCP_REQUEST")

# Check if response is valid JSON
if ! echo "$RESPONSE" | jq . > /dev/null 2>&1; then
    echo "❌ Invalid JSON response from server:"
    echo "$RESPONSE"
    exit 1
fi

# Extract result
echo "📥 Received response from server"
echo ""

# Check for errors
if echo "$RESPONSE" | jq -e '.error' > /dev/null 2>&1; then
    echo "❌ MCP Error:"
    echo "$RESPONSE" | jq '.error'
    exit 1
fi

# Display the results
echo "============================================================"
echo "📊 Results"
echo "============================================================"
echo ""

# Extract the text content
CONTENT=$(echo "$RESPONSE" | jq -r '.result.content[0].text // empty')

if [ -n "$CONTENT" ]; then
    echo "$CONTENT"
    echo ""
    
    # Try to extract CO2 value for comparison
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
        
        # Compare with expected values (Southampton to Newcastle)
        if [ "$FROM_CITY" = "southampton" ] && [ "$TO_CITY" = "newcastle" ]; then
            if [ "$CO2_VALUE" = "292" ]; then
                echo "✅ Result matches standalone tests! (292 kg CO2)"
            else
                echo "⚠️  Result differs from standalone tests (expected 292 kg, got ${CO2_VALUE} kg)"
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

# Display full structured data if available
echo ""
echo "============================================================"
echo "📋 Full Structured Data"
echo "============================================================"
echo ""
echo "$RESPONSE" | jq -C '.result.data // {}'

echo ""
echo "============================================================"
echo "✅ Test Complete!"
echo "============================================================"
echo ""
echo "💡 Compare this with the standalone tests:"
echo "   Python: python test_ecotree_flight.py $FROM_CITY $TO_CITY"
echo "   Go:     cd tools/ecotree_test && ./ecotree_test -from $FROM_CITY -to $TO_CITY"
echo ""
echo "📝 Check server logs for detailed execution:"
echo "   tail -f /tmp/hdn_server.log"
echo ""

