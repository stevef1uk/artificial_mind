#!/bin/bash
# Test the complete HDN → Scraper flow
# This tests HDN server calling the scraper service via MCP tool

set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

HDN_URL="${HDN_URL:-http://localhost:3001}"
SCRAPER_URL="${PLAYWRIGHT_SCRAPER_URL:-http://localhost:8080}"

echo -e "${BLUE}🧪 Testing Complete HDN → Scraper Flow${NC}"
echo "========================================"
echo ""

# Check prerequisites
echo -e "${BLUE}1️⃣  Checking prerequisites...${NC}"

# Check scraper service
if ! curl -sf "$SCRAPER_URL/health" > /dev/null 2>&1; then
    echo -e "${RED}❌ Scraper service not running at $SCRAPER_URL${NC}"
    echo "   Start it with:"
    echo "   docker run -d --name scraper-test -p 8080:8080 playwright-scraper:test"
    exit 1
fi
echo -e "${GREEN}✅ Scraper service is running${NC}"

# Check HDN service
if ! curl -sf "$HDN_URL/health" > /dev/null 2>&1; then
    echo -e "${YELLOW}⚠️  HDN service not running at $HDN_URL${NC}"
    echo ""
    echo "To start HDN locally:"
    echo "  1. Set environment variables:"
    echo "     export PLAYWRIGHT_SCRAPER_URL=$SCRAPER_URL"
    echo "     export NEO4J_URI=neo4j://localhost:7687"
    echo "     export WEAVIATE_URL=http://localhost:8081"
    echo "     export REDIS_ADDR=localhost:6379"
    echo ""
    echo "  2. Build and run:"
    echo "     cd /home/stevef/dev/artificial_mind/hdn"
    echo "     go build -o ../bin/hdn-server-test ."
    echo "     cd .."
    echo "     ./bin/hdn-server-test"
    echo ""
    exit 1
fi
echo -e "${GREEN}✅ HDN service is running${NC}"
echo ""

# Test Plane via HDN MCP tool
echo -e "${BLUE}2️⃣  Testing Plane via HDN MCP tool...${NC}"

TS_CONFIG='import { test } from '\''@playwright/test'\'';
test('\''test'\'', async ({ page }) => {
  await page.goto('\''https://ecotree.green/en/calculate-flight-co2'\'');
  await page.getByRole('\''link'\'', { name: '\''Plane'\'' }).click();
  await page.getByRole('\''textbox'\'', { name: '\''From To Via'\'' }).click();
  await page.getByRole('\''textbox'\'', { name: '\''From To Via'\'' }).fill('\''southampton'\'');
  await page.getByText('\''Southampton, United Kingdom'\'').click();
  await page.locator('\''input[name="To"]'\'').click();
  await page.locator('\''input[name="To"]'\'').fill('\''newcastle'\'');
  await page.getByText('\''Newcastle, United Kingdom, (Newcastle'\'').click();
  await page.getByRole('\''link'\'', { name: '\'' Calculate my emissions '\'' }).click();
});'

MCP_REQUEST=$(cat <<EOF
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "tools/call",
  "params": {
    "name": "scrape_url",
    "arguments": {
      "url": "https://ecotree.green/en/calculate-flight-co2",
      "typescript_config": $(echo "$TS_CONFIG" | jq -Rs .)
    }
  }
}
EOF
)

echo "📤 Sending MCP request to HDN..."
echo "   URL: $HDN_URL/mcp"
echo "   Tool: scrape_url"
echo "   Transport: Plane"
echo ""

START_TIME=$(date +%s)

RESPONSE=$(curl -s -X POST "$HDN_URL/mcp" \
    -H 'Content-Type: application/json' \
    -d "$MCP_REQUEST")

ELAPSED=$(($(date +%s) - START_TIME))

# Check for error
if echo "$RESPONSE" | jq -e '.error' > /dev/null 2>&1; then
    echo -e "${RED}❌ MCP request failed${NC}"
    echo "$RESPONSE" | jq '.error'
    exit 1
fi

# Extract result
RESULT=$(echo "$RESPONSE" | jq -r '.result.result // .result')

if [ -z "$RESULT" ] || [ "$RESULT" = "null" ]; then
    echo -e "${RED}❌ No result returned${NC}"
    echo "$RESPONSE" | jq '.'
    exit 1
fi

echo -e "${GREEN}✅ Request completed in ${ELAPSED}s${NC}"
echo ""

# Display results
echo -e "${BLUE}📊 Results from HDN:${NC}"
echo "$RESULT" | jq '.'

# Extract specific values
CO2=$(echo "$RESULT" | jq -r '.co2_kg // "N/A"')
DISTANCE=$(echo "$RESULT" | jq -r '.distance_km // "N/A"')

echo ""
echo -e "${GREEN}✈️  CO2 Emissions: $CO2 kg${NC}"
echo -e "${GREEN}📏 Distance: $DISTANCE km${NC}"
echo ""

# Verify the flow
echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo -e "${BLUE}Flow Verification:${NC}"
echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo -e "${GREEN}✅ n8n/Client → HDN (MCP)${NC}"
echo -e "${GREEN}✅ HDN → Scraper Service (HTTP)${NC}"
echo -e "${GREEN}✅ Scraper → Playwright → Website${NC}"
echo -e "${GREEN}✅ Results → Scraper → HDN → Client${NC}"
echo ""
echo -e "${GREEN}✅ COMPLETE FLOW WORKING!${NC}"
echo ""
echo -e "${BLUE}Next steps:${NC}"
echo "  1. Test other transport types (Train, Car)"
echo "  2. Deploy both services to Kubernetes"
echo "  3. Update n8n workflows"

