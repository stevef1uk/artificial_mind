#!/bin/bash

# Test script for tool metrics logging system
echo "🧪 Testing Tool Metrics System"
echo "================================"

# Check if HDN server is running
echo "1. Checking if HDN server is running..."
if curl -s http://localhost:8080/health > /dev/null; then
    echo "✅ HDN server is running"
else
    echo "❌ HDN server is not running. Please start it first."
    exit 1
fi

# Test tool metrics endpoint
echo ""
echo "2. Testing tool metrics endpoint..."
METRICS_RESPONSE=$(curl -s http://localhost:8080/api/v1/tools/metrics)
if echo "$METRICS_RESPONSE" | grep -q "metrics"; then
    echo "✅ Tool metrics endpoint is working"
    echo "Response: $METRICS_RESPONSE"
else
    echo "❌ Tool metrics endpoint failed"
    echo "Response: $METRICS_RESPONSE"
fi

# Test tool invocation to generate some metrics
echo ""
echo "3. Testing tool invocation to generate metrics..."
HTTP_TOOL_RESPONSE=$(curl -s -X POST http://localhost:8080/api/v1/tools/tool_http_get/invoke \
    -H "Content-Type: application/json" \
    -d '{"url": "https://httpbin.org/get"}')
if echo "$HTTP_TOOL_RESPONSE" | grep -q "status"; then
    echo "✅ Tool invocation successful"
    echo "Response: $HTTP_TOOL_RESPONSE"
else
    echo "❌ Tool invocation failed"
    echo "Response: $HTTP_TOOL_RESPONSE"
fi

# Check metrics after tool call
echo ""
echo "4. Checking metrics after tool call..."
sleep 2
METRICS_AFTER=$(curl -s http://localhost:8080/api/v1/tools/metrics)
echo "Metrics after tool call: $METRICS_AFTER"

# Test recent calls endpoint
echo ""
echo "5. Testing recent calls endpoint..."
RECENT_CALLS=$(curl -s http://localhost:8080/api/v1/tools/calls/recent?limit=5)
if echo "$RECENT_CALLS" | grep -q "calls"; then
    echo "✅ Recent calls endpoint is working"
    echo "Response: $RECENT_CALLS"
else
    echo "❌ Recent calls endpoint failed"
    echo "Response: $RECENT_CALLS"
fi

# Check log file
echo ""
echo "6. Checking tool call log file..."
LOG_FILE="/tmp/tool_calls_$(date +%Y-%m-%d).log"
if [ -f "$LOG_FILE" ]; then
    echo "✅ Log file exists: $LOG_FILE"
    echo "Last few lines:"
    tail -3 "$LOG_FILE"
else
    echo "❌ Log file not found: $LOG_FILE"
fi

echo ""
echo "🎉 Test completed!"
echo ""
echo "To view the updated Tools pane with metrics:"
echo "1. Open http://localhost:8082 in your browser"
echo "2. Navigate to the Tools section"
echo "3. Click 'Refresh Metrics' to see the latest data"
echo "4. Metrics will auto-refresh every 30 seconds"
