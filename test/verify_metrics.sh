#!/bin/bash

# Quick script to verify tool metrics are being aggregated correctly
HDN_URL="${1:-http://localhost:8080}"

echo "🔍 Verifying Tool Metrics Aggregation"
echo "======================================"
echo ""

echo "1. Recent tool calls (last 10):"
curl -s "$HDN_URL/api/v1/tools/calls/recent?limit=10" | jq -r '.calls[] | "  \(.tool_id): \(.timestamp)"' | head -5
echo ""

echo "2. Aggregated metrics for recently used tools:"
curl -s "$HDN_URL/api/v1/tools/metrics" | jq -r '.metrics[] | select(.tool_id == "tool_ls" or .tool_id == "tool_file_read" or .tool_id == "tool_http_get" or .tool_id == "tool_ssh_executor") | "  \(.tool_id) (\(.tool_name)): \(.total_calls) total, \(.success_calls) success"'
echo ""

echo "3. All tools with metrics:"
curl -s "$HDN_URL/api/v1/tools/metrics" | jq -r '.metrics[] | "  \(.tool_id): \(.total_calls) calls"'
echo ""

echo "💡 If recent calls show tools but metrics show 0, there may be a delay in aggregation"
echo "   Metrics are updated immediately, so check again in a moment"









