#!/bin/bash

# Manually trigger a daily summary generation

set -e

NAMESPACE="agi"
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

print_status() {
    echo -e "${BLUE}[TRIGGER]${NC} $1"
}

print_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

print_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

echo "=========================================="
echo "Manual Daily Summary Trigger"
echo "=========================================="
echo ""

# Get HDN pod
HDN_POD=$(kubectl get pods -n $NAMESPACE -l app=hdn-server-rpi58 -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || echo "")

if [ -z "$HDN_POD" ]; then
    print_error "HDN pod not found"
    exit 1
fi

print_status "Triggering daily summary via HDN API..."
print_status "Pod: $HDN_POD"

# Create the payload
PAYLOAD='{
  "task_name": "daily_summary",
  "description": "Summarize the day: key discoveries, actions, and questions",
  "context": {
    "session_id": "manual_trigger",
    "prefer_traditional": "true"
  },
  "language": "python"
}'

# Send request to HDN
RESPONSE=$(kubectl exec -n $NAMESPACE "$HDN_POD" -- wget -q -O- \
    --post-data "$PAYLOAD" \
    --header='Content-Type: application/json' \
    http://localhost:8080/api/v1/intelligent/execute 2>&1 || echo "")

if [ -z "$RESPONSE" ]; then
    print_error "No response from HDN"
    exit 1
fi

# Check if response indicates success
if echo "$RESPONSE" | grep -q "workflow_id"; then
    print_success "Daily summary trigger sent successfully"
    echo "$RESPONSE" | python3 -c '
import sys, json
try:
    data = json.load(sys.stdin)
    print(f"  Workflow ID: {data.get(\"workflow_id\", \"unknown\")}")
    print(f"  Success: {data.get(\"success\", False)}")
    if "error" in data:
        print(f"  Error: {data[\"error\"]}")
except:
    print("  Response received (could not parse JSON)")
' 2>/dev/null || echo "  Response: $RESPONSE"
else
    print_error "Unexpected response from HDN"
    echo "  Response: $RESPONSE"
    exit 1
fi

echo ""
print_status "Waiting 10 seconds for summary generation..."
sleep 10

# Check if summary was created in Redis
REDIS_POD=$(kubectl get pods -n $NAMESPACE -l app=redis -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || echo "")
if [ -n "$REDIS_POD" ]; then
    LATEST=$(kubectl exec -n $NAMESPACE "$REDIS_POD" -- redis-cli GET "daily_summary:latest" 2>/dev/null || echo "")
    if [ -n "$LATEST" ] && [ "$LATEST" != "(nil)" ]; then
        print_success "Daily summary created in Redis!"
        echo "$LATEST" | python3 -c '
import sys, json
try:
    data = json.load(sys.stdin)
    print(f"  Date: {data.get("date", "unknown")}")
    print(f"  Generated: {data.get("generated_at", "unknown")}")
    summary = data.get("summary", "")
    print(f"  Summary length: {len(summary)} characters")
    if len(summary) > 0:
        preview = summary[:300] + "..." if len(summary) > 300 else summary
        print(f"  Preview:\n{preview}")
except Exception as e:
    print(f"  (Could not parse: {e})")
' 2>/dev/null || echo "  (Summary exists but could not parse)"
    else
        print_error "Summary not found in Redis after 10 seconds"
        print_status "Check HDN logs for errors:"
        echo "  kubectl logs -n $NAMESPACE $HDN_POD --tail=50 | grep daily_summary"
    fi
fi

echo ""
echo "=========================================="
echo "Trigger Complete"
echo "=========================================="
echo ""
echo "To view the summary in the UI:"
echo "  1. Open Monitor UI"
echo "  2. Go to Overview tab"
echo "  3. Check the Daily Summary section"
echo ""

