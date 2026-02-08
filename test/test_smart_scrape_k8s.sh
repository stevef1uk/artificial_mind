#!/bin/bash
# Test smart_scrape MCP tool against HDN server running in Kubernetes

set -e

NAMESPACE="${1:-agi}"
URL="${2:-https://finance.yahoo.com/quote/AAPL}"
GOAL="${3:-Find the current stock price}"

echo "============================================================"
echo "🧪 MCP Smart Scrape Test - Kubernetes"
echo "============================================================"
echo ""
echo "📍 Namespace: $NAMESPACE"
echo "📍 URL: $URL"
echo "📍 Goal: $GOAL"
echo ""

# Find HDN service
echo "🔍 Finding HDN service in namespace $NAMESPACE..."
SERVICE=$(kubectl get svc -n "$NAMESPACE" -o name | grep hdn | head -1)

if [ -z "$SERVICE" ]; then
    echo "❌ No HDN service found in namespace $NAMESPACE"
    echo ""
    echo "Available services:"
    kubectl get svc -n "$NAMESPACE"
    exit 1
fi

echo "✅ Found service: $SERVICE"

# Get NodePort
NODEPORT=$(kubectl get "$SERVICE" -n "$NAMESPACE" -o jsonpath='{.spec.ports[0].nodePort}')
echo "✅ NodePort: $NODEPORT"

# Get node IP (use first node)
NODE_IP=$(kubectl get nodes -o jsonpath='{.items[0].status.addresses[?(@.type=="InternalIP")].address}')
echo "✅ Node IP: $NODE_IP"

SERVER_URL="http://${NODE_IP}:${NODEPORT}"
echo ""
echo "📡 Testing against: $SERVER_URL"
echo ""

# Check if server is running
echo "🔍 Checking if HDN server is responding..."
if ! curl -s "${SERVER_URL}/health" > /dev/null 2>&1; then
    echo "❌ HDN server is not responding at ${SERVER_URL}"
    echo ""
    echo "Check pod status:"
    kubectl get pods -n "$NAMESPACE" | grep hdn
    exit 1
fi
echo "✅ HDN server is responding"
echo ""

# Create MCP request for smart_scrape
MCP_REQUEST=$(cat <<EOF
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "tools/call",
  "params": {
    "name": "smart_scrape",
    "arguments": {
      "url": "$URL",
      "goal": "$GOAL"
    }
  }
}
EOF
)

echo "📤 Sending smart_scrape MCP request to Kubernetes HDN server..."
echo "   Goal: $GOAL"
echo "   (This may take 30-90 seconds as the LLM plans and executes the scrape)"
echo ""

# Send request
RESPONSE=$(curl -s -X POST "${SERVER_URL}/mcp" \
  -H "Content-Type: application/json" \
  -d "$MCP_REQUEST")

# Check if response is valid JSON
if ! echo "$RESPONSE" | jq . > /dev/null 2>&1; then
    echo "❌ Invalid JSON response from server:"
    echo "$RESPONSE"
    echo ""
    echo "Check pod logs:"
    echo "kubectl logs -n $NAMESPACE \$(kubectl get pods -n $NAMESPACE -o name | grep hdn | head -1)"
    exit 1
fi

# Check for errors
if echo "$RESPONSE" | jq -e '.error' > /dev/null 2>&1; then
    echo "❌ MCP Error:"
    echo "$RESPONSE" | jq '.error'
    echo ""
    echo "Check pod logs:"
    echo "kubectl logs -n $NAMESPACE \$(kubectl get pods -n $NAMESPACE -o name | grep hdn | head -1)"
    exit 1
fi

# Extract content
CONTENT=$(echo "$RESPONSE" | jq -r '.result.content[0].text // empty')

if [ -n "$CONTENT" ]; then
    echo "============================================================"
    echo "📊 Results"
    echo "============================================================"
    echo ""
    echo "$CONTENT"
    echo ""
    
    # Try to extract any price-like values
    PRICE=$(echo "$CONTENT" | grep -oP '\$[\d,]+\.?\d*' | head -1 || echo "")
    
    if [ -n "$PRICE" ]; then
        echo "============================================================"
        echo "✅ Success!"
        echo "============================================================"
        echo ""
        echo "🎯 Extracted Price: $PRICE"
        echo ""
    else
        echo "⚠️  Could not extract price from response (but scrape may have succeeded)"
    fi
else
    echo "⚠️  No content in response"
    echo ""
    echo "Full response:"
    echo "$RESPONSE" | jq .
fi

echo ""
echo "============================================================"
echo "✅ Test Complete!"
echo "============================================================"
echo ""
echo "💡 Useful commands:"
echo "   View pods:  kubectl get pods -n $NAMESPACE"
echo "   View logs:  kubectl logs -n $NAMESPACE \$(kubectl get pods -n $NAMESPACE -o name | grep hdn | head -1)"
echo "   Describe:   kubectl describe \$(kubectl get pods -n $NAMESPACE -o name | grep hdn | head -1) -n $NAMESPACE"
echo ""
echo "💡 Test other URLs:"
echo "   $0 $NAMESPACE 'https://finance.yahoo.com/quote/GOOGL' 'Find Google stock price'"
echo "   $0 $NAMESPACE 'https://www.google.com/finance/quote/AAPL:NASDAQ' 'Find Apple stock price'"
echo ""
