#!/bin/bash
# Test the new ASYNC MCP scrape_url loop
# 1. Start job with async: true
# 2. Get Job ID
# 3. Poll get_scrape_status until completed

set -e

NAMESPACE="${1:-agi}"
echo "🧪 Testing ASYNC MCP Scrape Flow in Namespace: $NAMESPACE"

# Find HDN service NodePort
# Find HDN service NodePort
SERVER_URL="http://localhost:8083"

echo "📡 Server URL: $SERVER_URL"

# 1. Start Async Job
echo "📤 Step 1: Starting asynchronous scrape job..."

REQUEST=$(cat <<EOF
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "tools/call",
  "params": {
    "name": "scrape_url",
    "arguments": {
      "url": "https://ecotree.green/en/calculate-car-co2",
      "async": true,
      "typescript_config": "await page.goto('https://ecotree.green/en/calculate-car-co2'); await page.waitForTimeout(2000); await page.locator('#return').click(); await page.waitForTimeout(1000);"
    }
  }
}
EOF
)

RESPONSE=$(curl -s -X POST "${SERVER_URL}/mcp" -H "Content-Type: application/json" -d "$REQUEST")
JOB_ID=$(echo "$RESPONSE" | jq -r '.result.job_id')

if [ "$JOB_ID" == "null" ] || [ -z "$JOB_ID" ]; then
    echo "❌ Failed to get Job ID. Response:"
    echo "$RESPONSE" | jq .
    exit 1
fi

echo "✅ Job started! ID: $JOB_ID"
echo ""

# 2. Poll for status
echo "⏳ Step 2: Polling for results using get_scrape_status..."

MAX_ATTEMPTS=60
ATTEMPT=0

while [ $ATTEMPT -lt $MAX_ATTEMPTS ]; do
    ATTEMPT=$((ATTEMPT + 1))
    
    POLL_REQUEST=$(cat <<EOF
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "tools/call",
  "params": {
    "name": "get_scrape_status",
    "arguments": {
      "job_id": "$JOB_ID"
    }
  }
}
EOF
)

    POLL_RESPONSE=$(curl -s -X POST "${SERVER_URL}/mcp" -H "Content-Type: application/json" -d "$POLL_REQUEST")
    STATUS=$(echo "$POLL_RESPONSE" | jq -r '.result.status')
    
    echo "   [$ATTEMPT/$MAX_ATTEMPTS] Status: $STATUS"
    
    if [ "$STATUS" == "completed" ]; then
        echo ""
        echo "🎉 Job Completed Successfully!"
        echo "📊 Results Data:"
        echo "$POLL_RESPONSE" | jq '.result.result'
        exit 0
    elif [ "$STATUS" == "failed" ]; then
        echo "❌ Job failed!"
        echo "$POLL_RESPONSE" | jq .
        exit 1
    fi
    
    sleep 2
done

echo "❌ Timeout waiting for async job"
exit 1
