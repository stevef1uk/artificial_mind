#!/bin/bash
# Test the scraper service directly in K3s

set -e

echo "🧪 Testing Playwright Scraper Service in K3s"
echo "=============================================="

# Get the scraper service endpoint
SCRAPER_POD=$(kubectl get pods -n agi -l app=playwright-scraper -o jsonpath='{.items[0].metadata.name}')
echo "📍 Scraper pod: $SCRAPER_POD"

# Forward port to access the service
echo "🔄 Port forwarding to scraper service..."
kubectl port-forward -n agi pod/$SCRAPER_POD 8080:8080 &
PF_PID=$!
sleep 3

# Cleanup function
cleanup() {
    echo "🧹 Cleaning up port forward..."
    kill $PF_PID 2>/dev/null || true
}
trap cleanup EXIT

# Test health endpoint
echo ""
echo "1️⃣  Testing health endpoint..."
if curl -sf http://localhost:8080/health > /dev/null; then
    echo "✅ Health check passed"
else
    echo "❌ Health check failed"
    exit 1
fi

# Test Plane scraping
echo ""
echo "2️⃣  Testing Plane scraping (Southampton -> Newcastle)..."

TS_CONFIG='import { test } from '\''@playwright/test'\'';
test('\''test'\'', async ({ page }) => {
  await page.goto('\''https://ecotree.green/en/calculate-flight-co2'\'');
  await page.getByRole('\''link'\'', { name: '\''Plane'\'' }).click();
  await page.getByRole('\''textbox'\'', { name: '\''From To Via'\'' }).click();
  await page.getByRole('\''textbox'\'', { name: '\''From To Via'\'' }).fill('\''southampton'\'');
  await page.getByText('\''Southampton, United Kingdom'\'').click();
  await page.locator('\''input[name="To"]'\'').click();
  await page.locator('\''input[name="To"]'\'').fill('\''newcastle'\'');
  await page.getByText('\''Newcastle, United Kingdom'\'').first().click();
  await page.getByRole('\''link'\'', { name: '\'' Calculate my emissions '\'' }).click();
});'

# Start the job
echo "📤 Submitting scrape job..."
JOB_RESPONSE=$(curl -s -X POST http://localhost:8080/scrape/start \
  -H 'Content-Type: application/json' \
  -d "{\"url\": \"https://ecotree.green/en/calculate-flight-co2\", \"typescript_config\": $(echo "$TS_CONFIG" | jq -Rs .)}")

JOB_ID=$(echo "$JOB_RESPONSE" | jq -r '.job_id')
echo "✅ Job submitted: $JOB_ID"

# Poll for results
echo "⏳ Waiting for results..."
MAX_ATTEMPTS=30
ATTEMPT=0

while [ $ATTEMPT -lt $MAX_ATTEMPTS ]; do
    sleep 2
    ATTEMPT=$((ATTEMPT + 1))
    
    STATUS_RESPONSE=$(curl -s "http://localhost:8080/scrape/status/$JOB_ID")
    STATUS=$(echo "$STATUS_RESPONSE" | jq -r '.status')
    
    echo "   Attempt $ATTEMPT/$MAX_ATTEMPTS: Status = $STATUS"
    
    if [ "$STATUS" = "completed" ]; then
        echo ""
        echo "✅ Job completed!"
        echo ""
        echo "📊 Results:"
        echo "$STATUS_RESPONSE" | jq '.result'
        
        # Extract specific values
        CO2=$(echo "$STATUS_RESPONSE" | jq -r '.result.co2_kg // "N/A"')
        DISTANCE=$(echo "$STATUS_RESPONSE" | jq -r '.result.distance_km // "N/A"')
        
        echo ""
        echo "✈️  CO2 Emissions: $CO2 kg"
        echo "📏 Distance: $DISTANCE km"
        
        # Validate results
        if [ "$CO2" != "N/A" ] && [ "$DISTANCE" != "N/A" ]; then
            echo ""
            echo "🎉 Test PASSED! Successfully scraped flight emissions data."
            exit 0
        else
            echo ""
            echo "⚠️  Test completed but missing data"
            exit 1
        fi
    elif [ "$STATUS" = "failed" ]; then
        echo ""
        echo "❌ Job failed"
        echo "$STATUS_RESPONSE" | jq '.error'
        exit 1
    fi
done

echo ""
echo "❌ Timeout waiting for job to complete"
exit 1

