#!/bin/bash

# Test script to manually trigger the daily summary job
# This mimics what the FSM sleep_cron scheduler does at 02:30 UTC

set -e

# Get HDN URL from environment or use default
HDN_URL="${HDN_URL:-http://localhost:8081}"

# If running in Kubernetes, try to detect the service
if [ -n "$KUBERNETES_SERVICE_HOST" ]; then
    HDN_URL="http://hdn-server-rpi58.agi.svc.cluster.local:8080"
    echo "🌐 Detected Kubernetes environment, using service URL: $HDN_URL"
else
    echo "🌐 Using HDN URL: $HDN_URL"
fi

echo "📅 Triggering daily summary job..."
echo "=================================="
echo ""

# Payload matches what FSM sleep_cron sends
PAYLOAD='{
  "task_name": "daily_summary",
  "description": "Summarize the day: key discoveries, actions, and questions",
  "context": {
    "session_id": "autonomy_daily",
    "prefer_traditional": "true"
  },
  "language": "python"
}'

echo "📤 Sending request to: $HDN_URL/api/v1/intelligent/execute"
echo "📋 Payload:"
echo "$PAYLOAD" | jq .
echo ""

# Make the request
RESPONSE=$(curl -s -w "\n%{http_code}" -X POST "$HDN_URL/api/v1/intelligent/execute" \
  -H "Content-Type: application/json" \
  -d "$PAYLOAD")

# Extract HTTP status code (last line)
HTTP_CODE=$(echo "$RESPONSE" | tail -n1)
BODY=$(echo "$RESPONSE" | sed '$d')

echo ""
echo "📥 Response:"
echo "   HTTP Status: $HTTP_CODE"
echo ""

if [ "$HTTP_CODE" = "202" ] || [ "$HTTP_CODE" = "200" ]; then
    echo "✅ Daily summary job triggered successfully!"
    echo ""
    echo "Response body:"
    echo "$BODY" | jq . 2>/dev/null || echo "$BODY"
    echo ""
    echo "💡 Check HDN logs for execution progress:"
    echo "   kubectl logs -n agi -l app=hdn-server-rpi58 -f | grep daily_summary"
    echo ""
    echo "💡 Check Monitor UI for the summary:"
    echo "   GET $HDN_URL/api/daily_summary/latest"
else
    echo "❌ Failed to trigger daily summary job"
    echo ""
    echo "Response body:"
    echo "$BODY" | jq . 2>/dev/null || echo "$BODY"
    exit 1
fi









