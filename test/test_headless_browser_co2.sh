#!/bin/bash
# Test script for headless browser CO2 calculator tool
# Tests the ecotree.green CO2 calculator

set -e

YELLOW='\033[1;33m'
GREEN='\033[0;32m'
RED='\033[0;31m'
NC='\033[0m' # No Color

echo -e "${YELLOW}🧪 Testing Headless Browser CO2 Calculator Tool${NC}"
echo ""

# Check if binary exists
BIN_DIR="${BIN_DIR:-./bin}"
BROWSER_BIN="$BIN_DIR/headless-browser"

if [ ! -f "$BROWSER_BIN" ]; then
    echo -e "${RED}❌ headless-browser binary not found at $BROWSER_BIN${NC}"
    echo "   Please build it first: make build-tools"
    exit 1
fi

echo -e "${GREEN}✅ Found headless-browser binary${NC}"
echo ""

# Test the CO2 calculator
# Note: This is a simplified test - in practice, you'd need to inspect the page
# to find the correct selectors for the form fields

echo -e "${YELLOW}🌐 Testing CO2 calculator at https://ecotree.green/en/calculate-train-co2${NC}"

# Example actions JSON for the CO2 calculator
# Note: Actual selectors would need to be determined by inspecting the page
ACTIONS='[
  {
    "type": "wait",
    "selector": "body",
    "timeout": 10
  },
  {
    "type": "fill",
    "selector": "input[name=\"from\"]",
    "value": "Paris"
  },
  {
    "type": "fill",
    "selector": "input[name=\"to\"]",
    "value": "London"
  },
  {
    "type": "select",
    "selector": "select[name=\"transport_type\"]",
    "value": "train"
  },
  {
    "type": "click",
    "selector": "button[type=\"submit\"], button:contains(\"Calculate\")"
  },
  {
    "type": "wait",
    "wait_for": ".result, .co2-result, [class*=\"result\"]",
    "timeout": 10
  },
  {
    "type": "extract",
    "extract": {
      "co2_emissions": ".co2-result, .result, [class*=\"co2\"]",
      "distance": ".distance, [class*=\"distance\"]",
      "message": ".message, .result-message"
    }
  }
]'

# Run the browser tool
echo "Running headless browser..."
RESULT=$("$BROWSER_BIN" \
  -url "https://ecotree.green/en/calculate-train-co2" \
  -actions "$ACTIONS" \
  -timeout 30 2>&1)

echo ""
echo -e "${GREEN}📊 Browser Result:${NC}"
echo "$RESULT" | jq '.' 2>/dev/null || echo "$RESULT"

echo ""
echo -e "${YELLOW}ℹ️  Note: This is a basic test.${NC}"
echo "   For production use, you would:"
echo "   1. First scrape the page to find actual form selectors"
echo "   2. Use the LLM to help identify form fields if needed"
echo "   3. Then execute the form filling actions"


