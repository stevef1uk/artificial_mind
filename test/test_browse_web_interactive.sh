#!/bin/bash
# Test the browse_web MCP tool (Interactive Step-by-Step)

HDN_URL="${HDN_URL:-http://localhost:8081}"
MCP_ENDPOINT="${MCP_ENDPOINT:-${HDN_URL}/mcp}"

echo "🧪 Testing browse_web MCP Tool (Interactive)"
echo "=========================================="

# Create the JSON payload
PAYLOAD=$(jq -n \
  --arg url "https://ecotree.green/en/calculate-flight-co2" \
  --arg instructions "Calculate CO2 emissions for a flight from Paris (CDG) to London (LHR) for 1 passenger in a Boeing 737." \
  '{
    "jsonrpc": "2.0",
    "id": 1,
    "method": "tools/call",
    "params": {
      "name": "browse_web",
      "arguments": {
        "url": $url,
        "instructions": $instructions,
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
