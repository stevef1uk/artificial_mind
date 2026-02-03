#!/bin/bash
# Run all three tests (Python, Go, MCP) and compare results

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$PROJECT_ROOT"

FROM_CITY="${1:-southampton}"
TO_CITY="${2:-newcastle}"

echo "============================================================"
echo "🔬 Playwright Test Comparison"
echo "============================================================"
echo ""
echo "Testing route: ${FROM_CITY^^} → ${TO_CITY^^}"
echo ""
echo "This will run the same test in three different ways:"
echo "  1. Python Playwright (standalone)"
echo "  2. Go Playwright (standalone)"
echo "  3. MCP Server (Go Playwright via TypeScript config)"
echo ""
read -p "Press Enter to start tests..."
echo ""

# Results storage
PYTHON_CO2=""
GO_CO2=""
MCP_CO2=""
PYTHON_TIME=""
GO_TIME=""
MCP_TIME=""

# Test 1: Python Playwright
echo "============================================================"
echo "1️⃣  Python Playwright Test"
echo "============================================================"
echo ""

if [ -f "test_ecotree_flight.py" ]; then
    START_TIME=$(date +%s)
    
    # Check if playwright_venv exists
    if [ -d "playwright_venv" ]; then
        echo "Using Python virtual environment..."
        source playwright_venv/bin/activate 2>/dev/null || true
        PYTHON_OUTPUT=$(python test_ecotree_flight.py "$FROM_CITY" "$TO_CITY" 2>&1 || true)
        deactivate 2>/dev/null || true
    else
        echo "⚠️  Python venv not found, trying system Python..."
        PYTHON_OUTPUT=$(python3 test_ecotree_flight.py "$FROM_CITY" "$TO_CITY" 2>&1 || true)
    fi
    
    END_TIME=$(date +%s)
    PYTHON_TIME=$((END_TIME - START_TIME))
    
    # Extract CO2 value from Python output
    PYTHON_CO2=$(echo "$PYTHON_OUTPUT" | grep -oP '"co2_kg": "\K[^"]+' | head -1 || echo "N/A")
    
    if [ "$PYTHON_CO2" != "N/A" ]; then
        echo "✅ Python test completed in ${PYTHON_TIME}s"
        echo "   CO2: ${PYTHON_CO2} kg"
    else
        echo "⚠️  Python test completed but could not extract CO2 value"
    fi
else
    echo "⚠️  Python test script not found"
fi

echo ""
sleep 1

# Test 2: Go Playwright  
echo "============================================================"
echo "2️⃣  Go Playwright Test"
echo "============================================================"
echo ""

if [ -f "tools/ecotree_test/ecotree_test" ]; then
    START_TIME=$(date +%s)
    
    GO_OUTPUT=$(tools/ecotree_test/ecotree_test -from "$FROM_CITY" -to "$TO_CITY" 2>&1 || true)
    
    END_TIME=$(date +%s)
    GO_TIME=$((END_TIME - START_TIME))
    
    # Extract CO2 value from Go output
    GO_CO2=$(echo "$GO_OUTPUT" | grep -oP '"co2_kg": "\K[^"]+' | head -1 || echo "N/A")
    
    if [ "$GO_CO2" != "N/A" ]; then
        echo "✅ Go test completed in ${GO_TIME}s"
        echo "   CO2: ${GO_CO2} kg"
    else
        echo "⚠️  Go test completed but could not extract CO2 value"
    fi
else
    echo "⚠️  Go test binary not found"
    echo "   Build it with: cd tools/ecotree_test && go build"
fi

echo ""
sleep 1

# Test 3: MCP Server
echo "============================================================"
echo "3️⃣  MCP Server Test"
echo "============================================================"
echo ""

if curl -s http://localhost:8081/health > /dev/null 2>&1; then
    START_TIME=$(date +%s)
    
    # Create TypeScript config
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

    TS_CONFIG_ESCAPED=$(echo "$TS_CONFIG" | jq -Rs .)
    
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
    
    MCP_OUTPUT=$(curl -s -X POST http://localhost:8081/mcp \
      -H "Content-Type: application/json" \
      -d "$MCP_REQUEST")
    
    END_TIME=$(date +%s)
    MCP_TIME=$((END_TIME - START_TIME))
    
    # Extract CO2 value from MCP output
    MCP_CO2=$(echo "$MCP_OUTPUT" | jq -r '.result.data.co2_kg // "N/A"')
    
    if [ "$MCP_CO2" != "N/A" ]; then
        echo "✅ MCP test completed in ${MCP_TIME}s"
        echo "   CO2: ${MCP_CO2} kg"
    else
        echo "⚠️  MCP test completed but could not extract CO2 value"
        echo "   Response: $(echo "$MCP_OUTPUT" | jq -c '.error // .result.content[0].text' | head -c 100)"
    fi
else
    echo "❌ HDN server is not running"
    echo "   Start it with: ./restart_hdn.sh"
fi

echo ""
echo ""

# Final comparison
echo "============================================================"
echo "📊 Results Comparison"
echo "============================================================"
echo ""

printf "%-20s | %-15s | %-10s\n" "Test Method" "CO2 (kg)" "Time (s)"
echo "-----------------------------------------------------"
printf "%-20s | %-15s | %-10s\n" "Python Playwright" "$PYTHON_CO2" "$PYTHON_TIME"
printf "%-20s | %-15s | %-10s\n" "Go Playwright" "$GO_CO2" "$GO_TIME"
printf "%-20s | %-15s | %-10s\n" "MCP Server" "$MCP_CO2" "$MCP_TIME"
echo ""

# Validate results match
if [ "$PYTHON_CO2" != "N/A" ] && [ "$GO_CO2" != "N/A" ] && [ "$MCP_CO2" != "N/A" ]; then
    if [ "$PYTHON_CO2" = "$GO_CO2" ] && [ "$GO_CO2" = "$MCP_CO2" ]; then
        echo "✅ All three tests produced identical results!"
        echo "   🎯 CO2 Emissions: ${MCP_CO2} kg"
        echo ""
        echo "🎉 MCP server Playwright integration is working perfectly!"
    else
        echo "⚠️  Results differ between tests:"
        echo "   Python: ${PYTHON_CO2} kg"
        echo "   Go:     ${GO_CO2} kg"
        echo "   MCP:    ${MCP_CO2} kg"
    fi
else
    echo "⚠️  Some tests failed or couldn't extract results"
fi

echo ""
echo "============================================================"
echo ""
echo "💡 Tips:"
echo "   • Check logs: tail -f /tmp/hdn_server.log"
echo "   • Test other routes: $0 london paris"
echo "   • Individual tests:"
echo "     - Python: python test_ecotree_flight.py <from> <to>"
echo "     - Go:     cd tools/ecotree_test && ./ecotree_test -from <from> -to <to>"
echo "     - MCP:    ./test_mcp_ecotree_complete.sh <from> <to>"
echo ""

