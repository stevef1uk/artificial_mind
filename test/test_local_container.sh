#!/bin/bash
set -e

CONTAINER_NAME="playwright-scraper-test-$(date +%s)"
PORT=8182
INTERNAL_PORT=8085
URL="https://finance.yahoo.com/quote/AAPL"
BYPASS_TS=$(cat /tmp/bypass_debug.ts | jq -R -s '.')

echo "🏗️  Starting container ${CONTAINER_NAME} on port ${PORT}..."
docker run --rm -d --name "${CONTAINER_NAME}" -e PORT=${INTERNAL_PORT} -p "${PORT}:${INTERNAL_PORT}" playwright-scraper-dev

# Wait for service to be ready
echo "⏳ Waiting for service to be ready..."
for i in {1..30}; do
    if curl -s "http://localhost:${PORT}/health" > /dev/null; then
        echo "✅ Service is ready!"
        break
    fi
    sleep 1
done

echo "🚀 Sending scrape request..."

# Construct JSON payload
# Note: extractions map is empty because we just want to see if we get the final page HTML
PAYLOAD=$(jq -n \
  --arg url "$URL" \
  --arg ts "$BYPASS_TS" \
  '{
    url: $url,
    typescript_config: ($ts | fromjson), 
    extractions: { price: "regex:([0-9]+\\.[0-9]+)" },
    get_html: true,
    async: true
  }')

echo "Payload:"
echo "$PAYLOAD" | jq .

RESPONSE=$(curl -s -X POST "http://localhost:${PORT}/scrape/start" \
  -H "Content-Type: application/json" \
  -d "$PAYLOAD")

echo "Response:"
echo "$RESPONSE" | jq .

JOB_ID=$(echo "$RESPONSE" | jq -r .job_id)
if [ "$JOB_ID" == "null" ] || [ -z "$JOB_ID" ]; then
    echo "❌ Failed to start job"
    docker stop "${CONTAINER_NAME}"
    exit 1
fi

echo "✅ Job started with ID: ${JOB_ID}"
echo "⏳ Polling for results..."

for i in {1..60}; do
    STATUS_RESP=$(curl -s "http://localhost:${PORT}/scrape/job?job_id=${JOB_ID}")
    STATUS=$(echo "$STATUS_RESP" | jq -r .status)
    
    echo "   [$((i*2))s] Status: ${STATUS}"
    
    if [ "$STATUS" == "completed" ] || [ "$STATUS" == "failed" ]; then
        echo "✅ Job finished with status: ${STATUS}"
        echo ""
        echo "📊 Final Result:"
        echo "$STATUS_RESP" | jq .
        
        # Check result content
        HTML=$(echo "$STATUS_RESP" | jq -r .result.cleaned_html)
        if [[ "$HTML" == *"consent"* ]] || [[ "$HTML" == *"confidentialité"* ]]; then
            echo "❌ STILL ON CONSENT PAGE!"
        else
            echo "✅ SUCCESS! Likely bypassed consent page."
            # Check for stock price indicators
            if [[ "$HTML" == *"AAPL"* ]]; then
                echo "   Found 'AAPL' in content."
            fi
        fi
        break
    fi
    sleep 2
done

echo "🛑 Stopping container..."
docker stop "${CONTAINER_NAME}"
