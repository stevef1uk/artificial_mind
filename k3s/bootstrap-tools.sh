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

# Find an available port for port-forward by trying ports in sequence
LOCAL_PORT=""
PF_PID=""

for port in 8080 8081 8082 8083 8084 8085; do
    echo "Trying port $port..."
    # Try to set up port-forward
    kubectl port-forward -n $NAMESPACE svc/$HDN_SERVICE ${port}:8080 >/tmp/hdn-tools-pf.log 2>&1 &
    PF_PID=$!
    sleep 2
    
    # Check if process is still running and port-forward succeeded
    if kill -0 $PF_PID 2>/dev/null; then
        # Verify the port-forward is actually working
        if curl -s http://localhost:${port}/api/v1/tools/discover >/dev/null 2>&1; then
            LOCAL_PORT=$port
            echo "✅ Port-forward established on port $LOCAL_PORT (PID: $PF_PID)"
            break
        else
            # Port-forward process exists but not working, kill it and try next port
            kill $PF_PID 2>/dev/null
            wait $PF_PID 2>/dev/null
            PF_PID=""
        fi
    else
        # Process died, check if it was because port was in use
        if grep -q "address already in use\|bind:" /tmp/hdn-tools-pf.log 2>/dev/null; then
            echo "  Port $port is in use, trying next port..."
            PF_PID=""
            continue
        else
            echo "❌ Port-forward failed on port $port"
            echo "Error details:"
            cat /tmp/hdn-tools-pf.log 2>/dev/null || echo "  (no error log available)"
            exit 1
        fi
    fi
done

if [ -z "$LOCAL_PORT" ] || [ -z "$PF_PID" ]; then
    echo "❌ Could not establish port-forward on any available port"
    exit 1
fi

# Trigger tool discovery
echo
echo "Triggering tool discovery..."
RESPONSE=$(curl -s -X POST http://localhost:${LOCAL_PORT}/api/v1/tools/discover)
# Check for both 'found' and 'discovered' fields in the response
TOOL_COUNT=$(echo "$RESPONSE" | jq -r 'if .found then .found | length elif .discovered then .discovered | length else 0 end' 2>/dev/null || echo "0")

if [ "$TOOL_COUNT" -gt 0 ]; then
    echo "✅ Registered $TOOL_COUNT tools"
    # Try both response formats
    echo "$RESPONSE" | jq -r 'if .found then .found[] | "  - \(.id): \(.name)" elif .discovered then .discovered[] | "  - \(.id): \(.name)" else empty end' 2>/dev/null
else
    echo "⚠️  No tools registered (response: $RESPONSE)"
fi

# Verify tools in Redis
echo
echo "Verifying tools in Redis..."
TOOLS_IN_REDIS=$(kubectl exec -n $NAMESPACE deployment/redis -- redis-cli SMEMBERS tools:registry 2>/dev/null | wc -l)
echo "✅ Tools in Redis registry: $TOOLS_IN_REDIS"

# Cleanup
if [ -n "$PF_PID" ]; then
    kill $PF_PID 2>/dev/null
    wait $PF_PID 2>/dev/null
fi

echo
echo "✅ Tool bootstrap complete"
echo
echo "Tools should now be visible in Monitor UI"





