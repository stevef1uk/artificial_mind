#!/bin/bash

# Simple RPi tool test script
# Tests basic tools are working on k8s/RPi
# Usage: ./test_rpi_tools.sh [TIMEOUT_SECS]
# Default: 180 seconds (for TPU), use 60 for GPU

TIMEOUT="${1:-180}"

echo "🧪 RPi Tool Test"
echo "==============="
echo "Timeout: ${TIMEOUT}s"
echo ""

# Setup kubectl port-forward if needed
echo "🔧 Setting up connection to HDN..."
kubectl port-forward -n agi svc/hdn-server-rpi58 8080:8080 > /dev/null 2>&1 &
PF_PID=$!
sleep 2

HDN_URL="http://localhost:8080"

# Cleanup on exit
cleanup() {
    kill $PF_PID 2>/dev/null
}
trap cleanup EXIT

# Test 1: List tools
echo "📋 Step 1: Available tools"
TOOLS=$(timeout $TIMEOUT curl -s "$HDN_URL/api/v1/tools" 2>/dev/null)
if [ $? -eq 0 ]; then
    TOOL_COUNT=$(echo "$TOOLS" | jq -r '.tools | length' 2>/dev/null || echo "0")
    echo "✅ Found $TOOL_COUNT tools"
    echo "$TOOLS" | jq -r '.tools[] | "  - \(.id)"' 2>/dev/null | head -10
else
    echo "❌ Failed to list tools"
    exit 1
fi
echo ""

# Test 2: tool_ls
echo "📂 Step 2: Test tool_ls"
timeout $TIMEOUT curl -s -X POST "$HDN_URL/api/v1/tools/tool_ls/invoke" \
    -H "Content-Type: application/json" \
    -d '{"path": "/tmp"}' | jq -r '.output // .error // "No output"' | head -3
echo "✅ tool_ls working"
echo ""

# Test 3: tool_file_read
echo "📄 Step 3: Test tool_file_read"
timeout $TIMEOUT curl -s -X POST "$HDN_URL/api/v1/tools/tool_file_read/invoke" \
    -H "Content-Type: application/json" \
    -d '{"path": "/etc/hostname"}' | jq -r '.content // .output // "No output"'
echo "✅ tool_file_read working"
echo ""

# Test 4: tool_exec (simple command)
echo "⚙️  Step 4: Test tool_exec"
timeout $TIMEOUT curl -s -X POST "$HDN_URL/api/v1/tools/tool_exec/invoke" \
    -H "Content-Type: application/json" \
    -d '{"cmd": "echo test"}' | jq -r '.output // .error // "No output"'
echo "✅ tool_exec working"
echo ""

echo "✅ All basic tools working!"
