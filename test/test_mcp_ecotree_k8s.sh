#!/bin/bash
# Test MCP scrape_url against HDN server running in Kubernetes
# Now supports all three transport types with proven configs

set -e

NAMESPACE="${1:-agi}"
TRANSPORT_TYPE="${2:-plane}"

echo "============================================================"
echo "🧪 MCP EcoTree Test - Kubernetes"
echo "============================================================"
echo ""
echo "📍 Namespace: $NAMESPACE"
echo "📍 Transport Type: $TRANSPORT_TYPE"
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

# Configure based on transport type
case "$TRANSPORT_TYPE" in
    plane)
        URL="https://ecotree.green/en/calculate-flight-co2"
        # Proven TypeScript config from test_scraper_plane.sh
        TS_CONFIG="await page.locator('#airportName').first().fill('SOU'); 
    await page.waitForTimeout(3000); 
    await page.getByText('Southampton, United Kingdom').first().click(); 
    await page.waitForTimeout(1000); 
    await page.locator('#airportName').nth(1).fill('NCL'); 
    await page.waitForTimeout(3000); 
    await page.getByText('Newcastle, United Kingdom').first().click(); 
    await page.waitForTimeout(1000); 
    await page.locator('select').first().selectOption('return'); 
    await page.waitForTimeout(500); 
    await page.getByRole('link', { name: ' Calculate my emissions ' }).click(); 
    await page.waitForTimeout(5000);"
        ROUTE="Southampton → Newcastle"
        ICON="✈️"
        ;;
    train)
        URL="https://ecotree.green/en/calculate-train-co2"
        # Proven TypeScript config from test_scraper_train.sh
        TS_CONFIG="await page.locator('.geosuggest').first().locator('input').fill('Petersfield'); 
    await page.waitForTimeout(2000); 
    await page.locator('.geosuggest').first().locator('.geosuggest__item').first().click(); 
    await page.waitForTimeout(1000); 
    await page.locator('.geosuggest').nth(1).locator('input').fill('London Waterloo'); 
    await page.waitForTimeout(2000); 
    await page.locator('.geosuggest').nth(1).locator('.geosuggest__item').first().click(); 
    await page.waitForTimeout(1000); 
    await page.locator('#return').click(); 
    await page.waitForTimeout(500); 
    await page.getByText('Long-distance rail (Electric)').click(); 
    await page.waitForTimeout(500); 
    await page.getByRole('link', { name: ' Calculate my emissions ' }).click(); 
    await page.waitForTimeout(5000);"
        ROUTE="Petersfield → London Waterloo"
        ICON="🚆"
        ;;
    car)
        URL="https://ecotree.green/en/calculate-car-co2"
        # Proven TypeScript config from test_scraper_car.sh
        TS_CONFIG="await page.locator('#geosuggest__input').first().fill('Portsmouth'); 
    await page.waitForTimeout(5000); 
    await page.getByText('Portsmouth').first().click(); 
    await page.waitForTimeout(2000); 
    await page.locator('#geosuggest__input').nth(1).fill('London'); 
    await page.waitForTimeout(5000); 
    await page.getByText('London').first().click(); 
    await page.waitForTimeout(2000); 
    await page.locator('#return').click(); 
    await page.waitForTimeout(1000); 
    await page.getByRole('link', { name: ' Calculate my emissions ' }).click(); 
    await page.waitForTimeout(5000);"
        ROUTE="Portsmouth → London"
        ICON="🚗"
        ;;
    *)
        echo "❌ Unknown transport type: $TRANSPORT_TYPE"
        echo "Valid options: plane, train, car"
        exit 1
        ;;
esac

echo "$ICON Testing $TRANSPORT_TYPE: $ROUTE"
echo ""

# Escape for JSON
TS_CONFIG_ESCAPED=$(echo "$TS_CONFIG" | jq -Rs .)

# Create MCP request with extractions
MCP_REQUEST=$(cat <<EOF
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "tools/call",
  "params": {
    "name": "scrape_url",
    "arguments": {
      "url": "$URL",
      "typescript_config": $TS_CONFIG_ESCAPED,
      "extractions": {
        "co2_kg": "(\\\\d+(?:[.,]\\\\d+)?)\\\\s*kg",
        "distance_km": "(\\\\d+(?:[.,]\\\\d+)?)\\\\s*km"
      }
    }
  }
}
EOF
)

echo "📤 Sending MCP request to Kubernetes HDN server..."
echo "   (This may take 30-60 seconds as Playwright runs in the pod)"
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
    
    # Extract CO2 and distance values from the JSON result
    CO2_VALUE=$(echo "$RESPONSE" | jq -r '.result.result.co2_kg // empty')
    DISTANCE=$(echo "$RESPONSE" | jq -r '.result.result.distance_km // empty')
    
    if [ -n "$CO2_VALUE" ]; then
        echo "============================================================"
        echo "✅ Success!"
        echo "============================================================"
        echo ""
        echo "🎯 Key Results:"
        echo "   $ICON CO2 Emissions: ${CO2_VALUE} kg"
        [ -n "$DISTANCE" ] && echo "   📏 Distance: ${DISTANCE} km"
        echo ""
    else
        echo "⚠️  Could not extract CO2 value from response"
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
echo "💡 Test other transport types:"
echo "   $0 $NAMESPACE plane"
echo "   $0 $NAMESPACE train"
echo "   $0 $NAMESPACE car"
echo ""

