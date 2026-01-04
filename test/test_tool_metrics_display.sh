#!/bin/bash

# Test script to check what tool metrics are available and compare with UI display
# Usage: ./test_tool_metrics_display.sh [HDN_URL] [MONITOR_URL]

HDN_URL="${1:-http://localhost:8080}"
MONITOR_URL="${2:-http://localhost:8082}"

echo "🔍 Tool Metrics Display Diagnostic"
echo "=================================="
echo ""

# Test 1: Get all tools from HDN
echo "📋 Step 1: Getting all registered tools from HDN..."
TOOLS_RESPONSE=$(curl -s "$HDN_URL/api/v1/tools")
TOOL_COUNT=$(echo "$TOOLS_RESPONSE" | jq -r '.tools | length' 2>/dev/null || echo "0")
echo "Found $TOOL_COUNT registered tools"
echo ""

# Test 2: Get all tool metrics from HDN
echo "📊 Step 2: Getting all tool metrics from HDN..."
METRICS_RESPONSE=$(curl -s "$HDN_URL/api/v1/tools/metrics")
METRICS_COUNT=$(echo "$METRICS_RESPONSE" | jq -r '.metrics | length' 2>/dev/null || echo "0")
echo "Found $METRICS_COUNT tools with metrics"
echo ""

# Show metrics details
if [ "$METRICS_COUNT" -gt "0" ]; then
    echo "Tool Metrics Details:"
    echo "$METRICS_RESPONSE" | jq -r '.metrics[] | "  \(.tool_id) (\(.tool_name)): \(.total_calls) total, \(.success_calls) success, \(.failure_calls) failed"' 2>/dev/null
    echo ""
fi

# Test 3: Compare tools vs metrics
echo "🔍 Step 3: Comparing tools with metrics..."
echo ""

# Get list of tool IDs
TOOL_IDS=$(echo "$TOOLS_RESPONSE" | jq -r '.tools[].id' 2>/dev/null | sort)
METRIC_IDS=$(echo "$METRICS_RESPONSE" | jq -r '.metrics[].tool_id' 2>/dev/null | sort)

echo "Tools without metrics (should show 0 in UI):"
while IFS= read -r tool_id; do
    if ! echo "$METRIC_IDS" | grep -q "^${tool_id}$"; then
        TOOL_NAME=$(echo "$TOOLS_RESPONSE" | jq -r ".tools[] | select(.id == \"$tool_id\") | .name" 2>/dev/null)
        echo "  - $tool_id ($TOOL_NAME): No metrics (should show 0)"
    fi
done <<< "$TOOL_IDS"
echo ""

echo "Tools with metrics (should show counts in UI):"
while IFS= read -r tool_id; do
    TOOL_NAME=$(echo "$METRICS_RESPONSE" | jq -r ".metrics[] | select(.tool_id == \"$tool_id\") | .tool_name" 2>/dev/null)
    TOTAL=$(echo "$METRICS_RESPONSE" | jq -r ".metrics[] | select(.tool_id == \"$tool_id\") | .total_calls" 2>/dev/null)
    echo "  - $tool_id ($TOOL_NAME): $TOTAL calls"
done <<< "$METRIC_IDS"
echo ""

# Test 4: Check Monitor UI endpoint
echo "🌐 Step 4: Checking Monitor UI metrics endpoint..."
MONITOR_METRICS=$(curl -s "$MONITOR_URL/api/tools/metrics" 2>/dev/null)
if [ $? -eq 0 ]; then
    MONITOR_COUNT=$(echo "$MONITOR_METRICS" | jq -r '.metrics | length' 2>/dev/null || echo "0")
    echo "Monitor UI returned $MONITOR_COUNT metrics"
    if [ "$MONITOR_COUNT" != "$METRICS_COUNT" ]; then
        echo "⚠️  WARNING: Monitor UI metrics count ($MONITOR_COUNT) differs from HDN metrics count ($METRICS_COUNT)"
    fi
else
    echo "⚠️  Could not reach Monitor UI (is it running?)"
fi
echo ""

# Summary
echo "=================================="
echo "📝 Summary"
echo "=================================="
echo "Registered tools: $TOOL_COUNT"
echo "Tools with metrics: $METRICS_COUNT"
echo "Tools without metrics: $((TOOL_COUNT - METRIC_IDS_COUNT))"
echo ""
echo "💡 Note: The UI should show:"
echo "   - Tools with metrics: actual counts"
echo "   - Tools without metrics: 0 counts"
echo ""
echo "If the UI is not showing all tools, check:"
echo "  1. Browser console for JavaScript errors"
echo "  2. Monitor UI logs for API errors"
echo "  3. That the tools list and metrics are being loaded correctly"









