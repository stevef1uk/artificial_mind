#!/bin/bash
# Test Playwright scraper with Train transport type

set -e

SCRAPER_URL="${PLAYWRIGHT_SCRAPER_URL:-http://localhost:8080}"

echo "🚆 Testing Scraper - Train (Petersfield → London Waterloo)"
echo "=========================================================="

# TypeScript config for Train (using direct URL navigation)
TS_CONFIG='import { test } from '\''@playwright/test'\'';
test('\''test'\'', async ({ page }) => {
  await page.goto('\''https://ecotree.green/en/calculate-train-co2'\'');
  await page.waitForTimeout(1000);
  
  // Fill From
  await page.locator('\''#geosuggest__input'\'').first().fill('\''Petersfield'\'');
  await page.waitForTimeout(2000);
  await page.keyboard.press('\''Enter'\''); // Robust selection
  await page.waitForTimeout(1000);
  
  // Fill To
  await page.locator('\''#geosuggest__input'\'').nth(1).fill('\''London Waterloo'\'');
  await page.waitForTimeout(2000);
  await page.keyboard.press('\''Enter'\''); // Robust selection
  await page.waitForTimeout(1000);
  
  await page.getByRole('\''link'\'', { name: '\'' Calculate my emissions '\'' }).click();
});'

# Start job
echo "🚀 Starting scrape job..."
START_RESP=$(curl -s -X POST "$SCRAPER_URL/scrape/start" \
    -H 'Content-Type: application/json' \
    -d "{\"url\": \"https://ecotree.green/en/calculate-train-co2\", \"typescript_config\": $(echo "$TS_CONFIG" | jq -Rs .)}")

JOB_ID=$(echo "$START_RESP" | jq -r '.job_id')
if [ -z "$JOB_ID" ] || [ "$JOB_ID" = "null" ]; then
    echo "❌ Failed to start job"
    echo "$START_RESP" | jq '.'
    exit 1
fi

echo "✅ Job started: $JOB_ID"

# Poll for results
echo "⏳ Polling for results..."
TIMEOUT=90
START_TIME=$(date +%s)

while true; do
    ELAPSED=$(($(date +%s) - START_TIME))
    
    if [ $ELAPSED -ge $TIMEOUT ]; then
        echo "❌ Timeout after ${TIMEOUT}s"
        exit 1
    fi
    
    sleep 2
    
    JOB_RESP=$(curl -s "$SCRAPER_URL/scrape/job?job_id=$JOB_ID")
    STATUS=$(echo "$JOB_RESP" | jq -r '.status')
    
    if [ "$STATUS" = "completed" ]; then
        echo "✅ Completed in ${ELAPSED}s!"
        echo ""
        echo "📊 Results:"
        echo "$JOB_RESP" | jq '.result'
        
        CO2=$(echo "$JOB_RESP" | jq -r '.result.co2_kg // "N/A"')
        DISTANCE=$(echo "$JOB_RESP" | jq -r '.result.distance_km // "N/A"')
        
        echo ""
        echo "🚆 CO2 Emissions: $CO2 kg"
        echo "📏 Distance: $DISTANCE km"
        exit 0
    elif [ "$STATUS" = "failed" ]; then
        echo "❌ Job failed"
        echo "$JOB_RESP" | jq '.error'
        exit 1
    fi
    
    echo "   [${ELAPSED}s] Status: $STATUS"
done

