#!/bin/bash
# test/test_mcp_async.sh

echo "🚀 Starting Async Plane Scrape Test..."

# 1. Start the job
RESPONSE=$(curl -s http://localhost:8081/mcp -X POST -H "Content-Type: application/json" -d '{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "tools/call",
  "params": {
    "name": "scrape_url",
    "arguments": {
      "url": "https://ecotree.green/en/calculate-flight-co2",
      "async": true,
      "typescript_config": "await page.goto('\''https://ecotree.green/en/calculate-flight-co2'\'')\nawait page.waitForTimeout(2000)\nawait page.locator('\''#airportName'\'').first().fill('\''Southampton'\'')\nawait page.waitForTimeout(3000)\nawait page.locator('\''text=/Southampton/i'\'').first().click()\nawait page.waitForTimeout(1000)\nawait page.locator('\''input[name=\"To\"]'\'').fill('\''Newcastle'\'')\nawait page.waitForTimeout(3000)\nawait page.locator('\''text=/Newcastle/i'\'').first().click()\nawait page.waitForTimeout(1000)\nawait page.locator('\''a.btn-primary.hover-arrow'\'').click()"
    }
  }
}')

JOB_ID=$(echo $RESPONSE | jq -r '.result.job_id')

if [ "$JOB_ID" == "null" ] || [ -z "$JOB_ID" ]; then
    echo "❌ Failed to start job. Response:"
    echo $RESPONSE | jq .
    exit 1
fi

echo "✅ Job started successfully! Job ID: $JOB_ID"
echo "⏳ Waiting 25 seconds for scraper to finish..."
sleep 25

# 2. Check status and get results
echo "🔍 Polling for results..."
RESULT=$(curl -s http://localhost:8081/mcp -X POST -H "Content-Type: application/json" -d "{
  \"jsonrpc\": \"2.0\",
  \"id\": 2,
  \"method\": \"tools/call\",
  \"params\": {
    \"name\": \"get_scrape_status\",
    \"arguments\": {
      \"job_id\": \"$JOB_ID\"
    }
  }
}")

STATUS=$(echo $RESULT | jq -r '.result.status')
CO2=$(echo $RESULT | jq -r '.result.result.co2_kg')

echo "📊 Final Status: $STATUS"
if [ "$STATUS" == "completed" ]; then
    echo "✅ Success! CO2 Result: $CO2 kg"
else
    echo "⚠️ Job not completed yet or failed. Full response:"
    echo $RESULT | jq .
fi
