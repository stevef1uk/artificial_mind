#!/bin/bash
set -e

# Configuration
URL="https://ecotree.green/en/calculate-flight-co2"
GOAL="Calculate CO2 for SOU to Newcastle"
# Note: For this complex test, we use specific typescript_config and extractions 
# to ensure the scraper service is working as expected.

echo "🚀 Starting EcoTree Service Test..."

# Ensure Scraper Service is running
if ! curl -s http://localhost:8080/health > /dev/null; then
    echo "⚠️ Scraper service not reachable at localhost:8080. Starting it..."
    make build-scraper-local
    ./bin/playwright-scraper > /tmp/scraper_ecotree.log 2>&1 &
    sleep 5
fi

# Construct payload
# Using proven patterns for Plane
PAYLOAD='{
  "url": "https://ecotree.green/en/calculate-flight-co2",
  "typescript_config": "await page.locator(\"#airportName\").first().fill(\"SOU\"); await page.waitForTimeout(3000); await page.locator(\".airportLine\").first().click(); await page.waitForTimeout(1000); await page.locator(\"#airportName\").nth(1).fill(\"NCL\"); await page.waitForTimeout(3000); await page.locator(\".airportLine\").first().click(); await page.waitForTimeout(1000); await page.locator(\"select\").first().selectOption(\"Return\"); await page.waitForTimeout(500); await page.getByRole(\"link\", { name: \" Calculate my emissions \" }).click(); await page.waitForTimeout(5000);",
  "extractions": {
    "co2": "Your footprint[\\s\\S]*?(?:CO|emissions)[\\s\\S]*?(\\d+(?:[.,]\\d+)?)\\s*kg",
    "distance": "Your footprint[\\s\\S]*?Kilometers[\\s\\S]*?(\\d+(?:[.,]\\d+)?)\\s*km"
  }
}'

echo "📦 Sending Payload..."
JOB_RESPONSE=$(curl -s -X POST http://localhost:8080/scrape/start \
    -H "Content-Type: application/json" \
    -d "$PAYLOAD")

JOB_ID=$(echo "$JOB_RESPONSE" | jq -r '.job_id // empty')
if [ -z "$JOB_ID" ]; then
    echo "❌ Failed to submit job. Response: $JOB_RESPONSE"
    exit 1
fi
echo "✅ Job submitted! ID: $JOB_ID"

echo -e "\n🚀 Polling for results..."
for i in {1..20}; do
    STATUS_RESPONSE=$(curl -s "http://localhost:8080/scrape/job?job_id=$JOB_ID")
    STATUS=$(echo "$STATUS_RESPONSE" | jq -r '.status // empty')
    
    echo "   [$i/20] Status: $STATUS"
    
    if [ "$STATUS" == "completed" ]; then
        echo -e "\n🎉 EcoTree Scrape Successful!"
        echo "📊 Results:"
        echo "$STATUS_RESPONSE" | jq '.result'
        
        # Verify result contains co2
        CO2=$(echo "$STATUS_RESPONSE" | jq -r '.result.co2 // empty')
        if [ -n "$CO2" ]; then
            echo "✅ Verified CO2 extracted: $CO2 kg"
            exit 0
        else
            echo "❌ CO2 extraction failed (empty result)"
            exit 1
        fi
    elif [ "$STATUS" == "failed" ]; then
        echo -e "\n❌ Scrape Failed!"
        echo "Error: $(echo "$STATUS_RESPONSE" | jq -r '.error')"
        exit 1
    fi
    
    sleep 3
done

echo "⚠️ Timed out waiting for job completion"
exit 1
