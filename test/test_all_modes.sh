#!/bin/bash
# Test Playwright scraper with all transport types
# Southampton -> Newcastle

set -e

SCRAPER_URL="${PLAYWRIGHT_SCRAPER_URL:-http://localhost:8080}"

test_mode() {
    MODE=$1
    URL=$2
    FROM=${3:-"Southampton"}
    TO=${4:-"Newcastle"}
    TRAIN_TYPE="Long-distance rail (Electric)" # Default for train
    
    echo "🚗 Testing Scraper - $MODE ($FROM → $TO)"
    echo "=========================================================="

    # Use the proven patterns provided by the user
    if [ "$MODE" = "Car" ]; then
        TS_CONFIG="await page.locator('#geosuggest__input').first().fill('$FROM'); 
            await page.waitForTimeout(3000); 
            await page.getByText('$FROM').first().click(); 
            await page.waitForTimeout(1000); 
            await page.locator('#geosuggest__input').nth(1).fill('$TO'); 
            await page.waitForTimeout(3000); 
            await page.getByText('$TO').first().click(); 
            await page.waitForTimeout(1000); 
            await page.locator('#return').click(); 
            await page.waitForTimeout(500); 
            await page.getByRole('link', { name: ' Calculate my emissions ' }).click(); 
            await page.waitForTimeout(5000);"
    elif [ "$MODE" = "Train" ]; then
        TS_CONFIG="await page.locator('.geosuggest').first().locator('input').fill('$FROM'); 
            await page.waitForTimeout(3000); 
            await page.locator('.geosuggest').first().locator('.geosuggest__item').first().click(); 
            await page.waitForTimeout(1000); 
            await page.locator('.geosuggest').nth(1).locator('input').fill('$TO'); 
            await page.waitForTimeout(3000); 
            await page.locator('.geosuggest').nth(1).locator('.geosuggest__item').first().click(); 
            await page.waitForTimeout(1000); 
            await page.locator('#return').click(); 
            await page.waitForTimeout(500); 
            await page.getByText('$TRAIN_TYPE').click(); 
            await page.waitForTimeout(500); 
            await page.getByRole('link', { name: ' Calculate my emissions ' }).click(); 
            await page.waitForTimeout(5000);"
    elif [ "$MODE" = "Plane" ]; then
        # For Plane, use airport codes for better matching if available
        TS_CONFIG="await page.locator('#airportName').first().fill('$FROM'); 
            await page.waitForTimeout(3000); 
            await page.locator('.airportLine').first().click(); 
            await page.waitForTimeout(1000); 
            await page.locator('#airportName').nth(1).fill('$TO'); 
            await page.waitForTimeout(3000); 
            await page.locator('.airportLine').first().click(); 
            await page.waitForTimeout(1000); 
            await page.locator('select').first().selectOption('Return'); 
            await page.waitForTimeout(500); 
            await page.getByRole('link', { name: ' Calculate my emissions ' }).click(); 
            await page.waitForTimeout(5000);"
    fi

    # Start job
    echo "🚀 Starting $MODE scrape job..."
    # Match literal text instead of normalized "Carbon"
    # Simplify CO2 regex to capture integer only (avoiding decimals like 3.1)
    EXTRACTIONS='{"co2": "Your footprint[\\s\\S]*?(?:CO|emissions)[\\s\\S]*?(\\d+)\\s*kg", "distance": "Your footprint[\\s\\S]*?Kilometers[\\s\\S]*?(\\d+(?:[.,]\\d+)?)\\s*km"}'
    
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
# 1. Train: Petersfield -> London Waterloo
test_mode "Train" "https://ecotree.green/en/calculate-train-co2" "Petersfield" "London Waterloo"
echo ""

# 2. Car: Portsmouth -> London (UK)
test_mode "Car" "https://ecotree.green/en/calculate-car-co2" "Portsmouth, UK" "London, UK"
echo ""

# 3. Plane: Southampton -> Newcastle
test_mode "Plane" "https://ecotree.green/en/calculate-flight-co2" "Southampton" "Newcastle"

