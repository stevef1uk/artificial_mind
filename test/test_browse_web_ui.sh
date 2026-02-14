#!/bin/bash
# Test the browse_web MCP tool with UI/Screenshot feedback

HDN_URL="${HDN_URL:-http://localhost:8080}"
MCP_ENDPOINT="${MCP_ENDPOINT:-${HDN_URL}/mcp}"
SCREENSHOT_PATH="/home/stevef/dev/artificial_mind/monitor/static/smart_scrape/screenshots/test_ui.png"

echo "🧪 Testing browse_web MCP Tool with UI Screenshot"
echo "==============================================="
echo "Screenshots will be saved to: $SCREENSHOT_PATH"

# Ensure directory exists
mkdir -p "$(dirname "$SCREENSHOT_PATH")"

# Create the JSON payload
PAYLOAD=$(jq -n \
  --arg url "https://ecotree.green/en/calculate-flight-co2" \
  --arg instructions "Calculate CO2 emissions for a flight from Paris (CDG) to London (LHR) for 1 passenger in a Boeing 737." \
  --arg screenshot "$SCREENSHOT_PATH" \
  '{
    "jsonrpc": "2.0",
    "id": 1,
    "method": "tools/call",
    "params": {
      "name": "browse_web",
      "arguments": {
        "url": $url,
        "instructions": $instructions,
        "screenshot": $screenshot,
        "timeout": 180
      }
    }
  }')

echo "🚀 Calling browse_web..."
echo "START_TIME: $(date)"

curl -X POST "$MCP_ENDPOINT" \
  -H "Content-Type: application/json" \
  -d "$PAYLOAD" | jq '.'

echo "END_TIME: $(date)"
