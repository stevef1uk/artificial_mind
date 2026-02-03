#!/bin/bash
# Test the MCP scrape_url tool with EcoTree TypeScript config

set -e

echo "============================================================"
echo "🧪 Testing MCP scrape_url with EcoTree Calculator"
echo "============================================================"
echo ""

# TypeScript config from your Playwright test
TS_CONFIG="import { test, expect } from '@playwright/test';

test('test', async ({ page }) => {
  await page.goto('https://ecotree.green/en/calculate-flight-co2');
  await page.getByRole('link', { name: 'Plane' }).click();
  await page.getByRole('textbox', { name: 'From To Via' }).click();
  await page.getByRole('textbox', { name: 'From To Via' }).fill('southampton');
  await page.getByText('Southampton, United Kingdom').click();
  await page.locator('input[name=\"To\"]').click();
  await page.locator('input[name=\"To\"]').fill('newcastle');
  await page.getByText('Newcastle, United Kingdom, (').click();
  await page.getByRole('link', { name: ' Calculate my emissions ' }).click();
});"

# Escape the TypeScript config for JSON
TS_CONFIG_ESCAPED=$(echo "$TS_CONFIG" | jq -Rs .)

# MCP request JSON
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

echo "📤 Sending MCP request to HDN server..."
echo ""

# Send the request
curl -s -X POST http://localhost:8081/mcp \
  -H "Content-Type: application/json" \
  -d "$MCP_REQUEST" | jq .

echo ""
echo "============================================================"
echo "✅ Test complete!"
echo "============================================================"

