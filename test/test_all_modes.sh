#!/bin/bash
# Test Playwright scraper with all transport types
# Southampton -> Newcastle

set -e

SCRAPER_URL="${PLAYWRIGHT_SCRAPER_URL:-http://localhost:8080}"

test_mode() {
    MODE=$1
    URL=$2
    FROM="Southampton"
    TO="Newcastle"
    
    # Use airport codes for flight robustness
    if [ "$MODE" = "Plane" ]; then
        FROM="Southampton" # Use name for more reliable dropdown matching
        TO="Newcastle"
    fi

    echo "🚗 Testing Scraper - $MODE ($FROM → $TO)"
    echo "=========================================================="

    # Use different selector for Plane
    SELECTOR="#geosuggest__input"
    if [ "$MODE" = "Plane" ]; then
        SELECTOR="#airportName"
    fi

    TS_CONFIG="import { test } from '@playwright/test';
test('test', async ({ page }) => {
  await page.goto('$URL');
  await page.waitForTimeout(3000);
  
  // Fill From
  await page.locator('$SELECTOR').first().fill('$FROM');
  await page.waitForTimeout(3000);
  // Try to click the suggestion explicitly for robustness
  await page.getByText('$FROM').first().click();
  await page.waitForTimeout(1000);
  
  // Fill To
  await page.locator('$SELECTOR').nth(1).fill('$TO');
  await page.waitForTimeout(3000);
  // Try to click the suggestion explicitly for robustness
  await page.getByText('$TO').first().click();
  await page.waitForTimeout(1000);
  
  // Click Calculate
  await page.getByRole('link', { name: ' Calculate my emissions ' }).click();
  
  // Extra wait for computation
  await page.waitForTimeout(5000);
});"

    # Start job
    echo "🚀 Starting $MODE scrape job..."
    # Custom extractions (using [\s\S]*? to handle complex spacing/elements)
    EXTRACTIONS='{"co2_dynamic": "carbon emissions[\\s\\S]*?(\\d+(?:[.,]\\d+)?)\\s*kg", "dist_dynamic": "travelled distance[\\s\\S]*?(\\d+(?:[.,]\\d+)?)\\s*km"}'
    
    START_RESP=$(curl -s -X POST "$SCRAPER_URL/scrape/start" \
        -H 'Content-Type: application/json' \
        -d "{\"url\": \"$URL\", \"typescript_config\": $(echo "$TS_CONFIG" | jq -Rs .), \"extractions\": $EXTRACTIONS}")

    JOB_ID=$(echo "$START_RESP" | jq -r '.job_id')
    if [ -z "$JOB_ID" ] || [ "$JOB_ID" = "null" ]; then
        echo "❌ Failed to start job"
        echo "$START_RESP" | jq '.'
        return 1
    fi

    echo "✅ Job started: $JOB_ID"

    # Poll for results
    TIMEOUT=120
    START_TIME=$(date +%s)

    while true; do
        ELAPSED=$(($(date +%s) - START_TIME))
        if [ $ELAPSED -ge $TIMEOUT ]; then
            echo "❌ Timeout after ${TIMEOUT}s"
            return 1
        fi
        sleep 2
        JOB_RESP=$(curl -s "$SCRAPER_URL/scrape/job?job_id=$JOB_ID")
        STATUS=$(echo "$JOB_RESP" | jq -r '.status')
        if [ "$STATUS" = "completed" ]; then
            echo "✅ Completed in ${ELAPSED}s!"
            echo "📊 Results for $MODE:"
            echo "$JOB_RESP" | jq '.result'
            return 0
        elif [ "$STATUS" = "failed" ]; then
            echo "❌ Job failed"
            echo "$JOB_RESP" | jq '.error'
            return 1
        fi
        echo -n "."
    done
}

echo "🏁 Starting Local Scraper Mode Tests"
test_mode "Car" "https://ecotree.green/en/calculate-car-co2"
echo ""
test_mode "Train" "https://ecotree.green/en/calculate-train-co2"
echo ""
test_mode "Plane" "https://ecotree.green/en/calculate-flight-co2"
