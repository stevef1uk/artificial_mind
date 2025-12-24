#!/bin/bash

# Script to bootstrap tools in HDN server
# This can be run manually or as a startup job

NAMESPACE="agi"
HDN_SERVICE="hdn-server-rpi58"

echo "🔧 Bootstrapping tools in HDN server..."
echo

# Check if HDN pod is running
HDN_POD=$(kubectl get pods -n $NAMESPACE -l app=$HDN_SERVICE -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
if [ -z "$HDN_POD" ]; then
    echo "❌ HDN pod not found"
    exit 1
fi

echo "✅ HDN pod: $HDN_POD"

# Set up port-forward
echo "Setting up port-forward..."
kubectl port-forward -n $NAMESPACE svc/$HDN_SERVICE 8080:8080 >/tmp/hdn-tools-pf.log 2>&1 &
PF_PID=$!
sleep 3

if ! kill -0 $PF_PID 2>/dev/null; then
    echo "❌ Port-forward failed"
    exit 1
fi

echo "✅ Port-forward established (PID: $PF_PID)"

# Trigger tool discovery
echo
echo "Triggering tool discovery..."
RESPONSE=$(curl -s -X POST http://localhost:8080/api/v1/tools/discover)
TOOL_COUNT=$(echo "$RESPONSE" | jq -r '.found | length' 2>/dev/null || echo "0")

if [ "$TOOL_COUNT" -gt 0 ]; then
    echo "✅ Registered $TOOL_COUNT tools"
    echo "$RESPONSE" | jq -r '.found[] | "  - \(.id): \(.name)"' 2>/dev/null
else
    echo "⚠️  No tools registered (response: $RESPONSE)"
fi

# Verify tools in Redis
echo
echo "Verifying tools in Redis..."
TOOLS_IN_REDIS=$(kubectl exec -n $NAMESPACE deployment/redis -- redis-cli SMEMBERS tools:registry 2>/dev/null | wc -l)
echo "✅ Tools in Redis registry: $TOOLS_IN_REDIS"

# Cleanup
kill $PF_PID 2>/dev/null
wait $PF_PID 2>/dev/null

echo
echo "✅ Tool bootstrap complete"
echo
echo "Tools should now be visible in Monitor UI"





