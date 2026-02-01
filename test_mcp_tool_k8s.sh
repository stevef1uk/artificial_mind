#!/bin/bash
# Test script to verify MCP read_google_data tool works via kubectl port-forward

set -e

echo "🔍 Testing MCP read_google_data tool..."
echo ""

# Check if kubectl is available
if ! command -v kubectl &> /dev/null; then
    echo "❌ kubectl not found. Please install kubectl first."
    exit 1
fi

# Check if jq is available (optional, for pretty output)
HAS_JQ=false
if command -v jq &> /dev/null; then
    HAS_JQ=true
fi

echo "1️⃣ Checking if HDN server pod is running..."
POD_NAME=$(kubectl get pods -n agi -l app=hdn-server-rpi58 -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || echo "")
if [ -z "$POD_NAME" ]; then
    echo "❌ No HDN server pod found in namespace 'agi'"
    exit 1
fi
echo "✅ Found pod: $POD_NAME"
echo ""

echo "2️⃣ Setting up port-forward (will run in background)..."
# Kill any existing port-forward on 8081
pkill -f "kubectl.*port-forward.*8081" 2>/dev/null || true
sleep 1

# Start port-forward in background (forward local 8081 to container 8080)
kubectl port-forward -n agi pod/$POD_NAME 8081:8080 > /dev/null 2>&1 &
PF_PID=$!
echo "✅ Port-forward started: localhost:8081 -> pod:8080 (PID: $PF_PID)"
echo "   Waiting for connection..."
sleep 3

# Cleanup function
cleanup() {
    echo ""
    echo "🧹 Cleaning up port-forward..."
    kill $PF_PID 2>/dev/null || true
    pkill -f "kubectl.*port-forward.*8081" 2>/dev/null || true
}
trap cleanup EXIT

echo ""
echo "3️⃣ Testing MCP tools/list endpoint..."
# Try /mcp first (this is the correct endpoint)
MCP_ENDPOINT=""
for endpoint in "/mcp" "/api/v1/mcp"; do
    echo "   Trying $endpoint..."
    TEST_RESPONSE=$(curl -s -w "\n%{http_code}" -X POST http://localhost:8081$endpoint \
      -H "Content-Type: application/json" \
      -d '{
        "jsonrpc": "2.0",
        "id": 1,
        "method": "tools/list"
      }' 2>/dev/null)
    
    HTTP_CODE=$(echo "$TEST_RESPONSE" | tail -n1)
    RESPONSE_BODY=$(echo "$TEST_RESPONSE" | head -n-1)
    
    if [ "$HTTP_CODE" = "200" ] && ! echo "$RESPONSE_BODY" | grep -q '"error"'; then
        MCP_ENDPOINT="$endpoint"
        TOOLS_RESPONSE="$RESPONSE_BODY"
        echo "   ✅ $endpoint works!"
        break
    else
        echo "   ❌ $endpoint returned HTTP $HTTP_CODE"
    fi
done

if [ -z "$MCP_ENDPOINT" ]; then
    echo "❌ Failed to connect to MCP endpoint (tried /api/v1/mcp and /mcp)"
    exit 1
fi

echo "✅ Using MCP endpoint: $MCP_ENDPOINT"

# Check for errors
if echo "$TOOLS_RESPONSE" | grep -q '"error"'; then
    echo "❌ Error from MCP server:"
    if [ "$HAS_JQ" = true ]; then
        echo "$TOOLS_RESPONSE" | jq '.error'
    else
        echo "$TOOLS_RESPONSE"
    fi
    exit 1
fi

# Check if read_google_data tool exists
if echo "$TOOLS_RESPONSE" | grep -q "read_google_data"; then
    echo "✅ read_google_data tool found in tools list"
    if [ "$HAS_JQ" = true ]; then
        echo "$TOOLS_RESPONSE" | jq '.result.tools[] | select(.name == "read_google_data")'
    fi
else
    echo "❌ read_google_data tool NOT found in tools list"
    echo "Available tools:"
    if [ "$HAS_JQ" = true ]; then
        echo "$TOOLS_RESPONSE" | jq -r '.result.tools[].name'
    else
        echo "$TOOLS_RESPONSE" | grep -o '"name":"[^"]*"' | cut -d'"' -f4
    fi
    exit 1
fi

echo ""
echo "4️⃣ Testing MCP tools/call endpoint with read_google_data..."
CALL_RESPONSE=$(curl -s -X POST http://localhost:8081$MCP_ENDPOINT \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "id": 2,
    "method": "tools/call",
    "params": {
      "name": "read_google_data",
      "arguments": {
        "query": "recent emails",
        "type": "email"
      }
    }
  }')

if [ $? -ne 0 ]; then
    echo "❌ Failed to call MCP tool"
    exit 1
fi

echo "Response:"
if [ "$HAS_JQ" = true ]; then
    echo "$CALL_RESPONSE" | jq '.'
else
    echo "$CALL_RESPONSE"
fi

# Check for errors
if echo "$CALL_RESPONSE" | grep -q '"error"'; then
    echo ""
    echo "❌ Error calling tool:"
    if [ "$HAS_JQ" = true ]; then
        echo "$CALL_RESPONSE" | jq '.error'
    else
        echo "$CALL_RESPONSE" | grep -o '"message":"[^"]*"' | cut -d'"' -f4
    fi
    exit 1
fi

echo ""
echo "✅ Tool call successful!"
if [ "$HAS_JQ" = true ]; then
    echo "Result:"
    echo "$CALL_RESPONSE" | jq '.result'
else
    echo "Check the response above for the result"
fi

echo ""
echo "🎉 All tests passed!"

