#!/bin/bash
set -e

# Configuration
URL="https://www.nationwide.co.uk/savings/cash-isas/"
GOAL="List each savings product and its rate as a pair (e.g. Account Name, Rate)"
MODEL="qwen2.5-coder:7b"

echo "🚀 Step 1: Use Scrape Planner to generate configuration..."
PLAN_OUTPUT=$(bin/scrape-planner -url "$URL" -goal "$GOAL" -model "$MODEL")

# Extract JSON from output (simple awk/sed extraction looking for start/end of JSON)
# We look for the line "--- Generated Configuration ---" and take everything after it
CONFIG_JSON=$(echo "$PLAN_OUTPUT" | sed -n '/--- Generated Configuration ---/,$p' | tail -n +2)

if [ -z "$CONFIG_JSON" ]; then
    echo "❌ Failed to generate configuration"
    exit 1
fi

echo "📋 Generated Configuration:"
echo "$CONFIG_JSON" | jq '.'

# Save to temporary file
echo "$CONFIG_JSON" > /tmp/scrape_config.json

echo -e "\n🚀 Step 2: Send configuration to Scraper Service..."

# Ensure Scraper Service is running (check health)
if curl -s http://localhost:8080/health > /dev/null; then
    echo "✅ Scraper service is running"
else
    echo "⚠️ Scraper service not reachable at localhost:8080. Is it running?"
    echo "   Starting scraper service locally..."
    make build-scraper-local
    ./bin/playwright-scraper > /tmp/scraper.log 2>&1 &
    SCRAPER_PID=$!
    echo "   Scraper PID: $SCRAPER_PID"
    sleep 5
fi

# Construct payload
# We need to merge the URL into the JSON config
PAYLOAD=$(jq -n \
    --arg url "$URL" \
    --slurpfile config /tmp/scrape_config.json \
    '{url: $url, typescript_config: $config[0].typescript_config, extractions: $config[0].extractions}')

echo "📦 Payload:"
echo "$PAYLOAD" | jq '.'

# Send Request
JOB_RESPONSE=$(curl -s -X POST http://localhost:8080/scrape/start \
    -H "Content-Type: application/json" \
    -d "$PAYLOAD")

JOB_ID=$(echo "$JOB_RESPONSE" | jq -r '.job_id // empty')
if [ -z "$JOB_ID" ]; then
    echo "❌ Failed to submit job. Response:"
    echo "$JOB_RESPONSE"
    exit 1
fi
echo "✅ Job submitted! ID: $JOB_ID"

echo -e "\n🚀 Step 3: Polling for results..."
for i in {1..10}; do
    STATUS_RESPONSE=$(curl -s "http://localhost:8080/scrape/job?job_id=$JOB_ID")
    STATUS=$(echo "$STATUS_RESPONSE" | jq -r '.status // empty')
    
    if [ -z "$STATUS" ]; then
        echo "   [$i/10] ❌ Error getting status. Response: $STATUS_RESPONSE"
        sleep 2
        continue
    fi
    
    echo "   [$i/10] Status: $STATUS"
    
    if [ "$STATUS" == "completed" ]; then
        echo -e "\n🎉 Scrape Successful!"
        echo "📊 Results:"
        echo "$STATUS_RESPONSE" | jq '.result'
        exit 0
    elif [ "$STATUS" == "failed" ]; then
        echo -e "\n❌ Scrape Failed!"
        echo "Error: $(echo "$STATUS_RESPONSE" | jq -r '.error')"
        exit 1
    fi
    
    sleep 2
done

echo "⚠️ Timed out waiting for job completion"
exit 1
