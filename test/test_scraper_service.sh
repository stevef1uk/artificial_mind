#!/bin/bash
# Test script for standalone Playwright scraper service

set -e

SCRAPER_URL="${PLAYWRIGHT_SCRAPER_URL:-http://localhost:8080}"

echo "🧪 Testing Playwright Scraper Service"
echo "======================================"
echo "Service URL: $SCRAPER_URL"
echo ""

# Test 1: Health check
echo "1️⃣  Testing health endpoint..."
HEALTH=$(curl -s "$SCRAPER_URL/health")
if echo "$HEALTH" | jq -e '.status == "healthy"' > /dev/null; then
    echo "✅ Health check passed"
    echo "$HEALTH" | jq '.'
else
    echo "❌ Health check failed"
    exit 1
fi
echo ""

# Test 2: Start scrape job (EcoTree Car example)
echo "2️⃣  Starting scrape job (EcoTree Car: Portsmouth -> London)..."
TS_CONFIG='import { test } from '\''@playwright/test'\'';
test('\''test'\'', async ({ page }) => {
  await page.goto('\''https://ecotree.green/en/calculate-car-co2'\'');
  await page.waitForTimeout(200);
  await page.locator('\''div.geosuggest:nth-of-type(1) #geosuggest__input'\'').fill('\''Portsmouth'\'');
  await page.waitForTimeout(200);
  await page.getByText('\''Portsmouth'\'').first().click();
  await page.waitForTimeout(200);
  await page.locator('\''div.geosuggest:nth-of-type(2) #geosuggest__input'\'').fill('\''London'\'');
  await page.waitForTimeout(200);
  await page.getByText('\''London'\'').first().click();
  await page.waitForTimeout(200);
  await page.getByRole('\''link'\'', { name: '\'' Calculate my emissions '\'' }).click();
});'

START_RESP=$(curl -s -X POST "$SCRAPER_URL/scrape/start" \
    -H 'Content-Type: application/json' \
    -d "{\"url\": \"https://ecotree.green/en/calculate-car-co2\", \"typescript_config\": $(echo "$TS_CONFIG" | jq -Rs .)}")

JOB_ID=$(echo "$START_RESP" | jq -r '.job_id')
if [ -z "$JOB_ID" ] || [ "$JOB_ID" = "null" ]; then
    echo "❌ Failed to start job"
    echo "$START_RESP" | jq '.'
    exit 1
fi

echo "✅ Job started: $JOB_ID"
echo "$START_RESP" | jq '.'
echo ""

# Test 3: Poll for results
echo "3️⃣  Polling for results (timeout: 90s)..."
TIMEOUT=90
ELAPSED=0
STATUS="pending"

while [ "$STATUS" != "completed" ] && [ "$STATUS" != "failed" ] && [ $ELAPSED -lt $TIMEOUT ]; do
    sleep 2
    ELAPSED=$((ELAPSED + 2))
    
    JOB_RESP=$(curl -s "$SCRAPER_URL/scrape/job?job_id=$JOB_ID")
    STATUS=$(echo "$JOB_RESP" | jq -r '.status')
    
    echo "   [$ELAPSED s] Status: $STATUS"
    
    if [ "$STATUS" = "completed" ]; then
        echo ""
        echo "✅ Job completed in ${ELAPSED}s!"
        echo ""
        echo "📊 Results:"
        echo "$JOB_RESP" | jq '.result'
        
        # Extract CO2 and distance
        CO2=$(echo "$JOB_RESP" | jq -r '.result.co2_kg // "N/A"')
        DISTANCE=$(echo "$JOB_RESP" | jq -r '.result.distance_km // "N/A"')
        
        echo ""
        echo "🚗 CO2 Emissions: $CO2 kg"
        echo "📏 Distance: $DISTANCE km"
        exit 0
    elif [ "$STATUS" = "failed" ]; then
        echo ""
        echo "❌ Job failed"
        echo "$JOB_RESP" | jq '.error'
        exit 1
    fi
done

if [ $ELAPSED -ge $TIMEOUT ]; then
    echo ""
    echo "❌ Job timed out after ${TIMEOUT}s"
    exit 1
fi

