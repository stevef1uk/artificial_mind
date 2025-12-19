#!/bin/bash

# Script to bootstrap tools in HDN server (Local/Mac version)
# This can be run manually or as a startup job
# Works with local HDN server (no kubectl required)

HDN_URL="${HDN_URL:-http://localhost:8081}"

echo "🔧 Bootstrapping tools in HDN server (Local)..."
echo "Using HDN URL: $HDN_URL"
echo

# Check if HDN server is accessible
if ! curl -s -f "$HDN_URL/health" >/dev/null 2>&1 && ! curl -s -f "$HDN_URL/api/v1/intelligent/capabilities" >/dev/null 2>&1; then
    echo "❌ HDN server not accessible at $HDN_URL"
    echo "ℹ️  Make sure HDN server is running:"
    echo "   - Check if it's running: ps aux | grep hdn-server"
    echo "   - Or start it: make start-hdn"
    exit 1
fi

echo "✅ HDN server is accessible"

# Trigger tool discovery
echo
echo "Triggering tool discovery..."
RESPONSE=$(curl -s -X POST "$HDN_URL/api/v1/tools/discover")
TOOL_COUNT=$(echo "$RESPONSE" | jq -r '.found | length' 2>/dev/null || echo "0")

if [ "$TOOL_COUNT" -gt 0 ]; then
    echo "✅ Registered $TOOL_COUNT tools"
    echo "$RESPONSE" | jq -r '.found[] | "  - \(.id): \(.name)"' 2>/dev/null
else
    echo "⚠️  No tools registered (response: $RESPONSE)"
fi

# Verify tools in Redis (local Docker setup)
echo
echo "Verifying tools in Redis..."
if command -v docker >/dev/null 2>&1; then
    REDIS_CONTAINER=$(docker ps --format "{{.Names}}" | grep -i redis | head -1)
    if [ -n "$REDIS_CONTAINER" ]; then
        TOOLS_IN_REDIS=$(docker exec "$REDIS_CONTAINER" redis-cli SMEMBERS tools:registry 2>/dev/null | wc -l | tr -d ' ')
        echo "✅ Tools in Redis registry: $TOOLS_IN_REDIS"
        if [ "$TOOLS_IN_REDIS" -gt 0 ]; then
            echo
            echo "Registered tools:"
            docker exec "$REDIS_CONTAINER" redis-cli SMEMBERS tools:registry 2>/dev/null | sort
        fi
    else
        echo "⚠️  Redis container not found"
    fi
elif command -v redis-cli >/dev/null 2>&1; then
    TOOLS_IN_REDIS=$(redis-cli SMEMBERS tools:registry 2>/dev/null | wc -l | tr -d ' ')
    echo "✅ Tools in Redis registry: $TOOLS_IN_REDIS"
    if [ "$TOOLS_IN_REDIS" -gt 0 ]; then
        echo
        echo "Registered tools:"
        redis-cli SMEMBERS tools:registry 2>/dev/null | sort
    fi
else
    echo "⚠️  Cannot verify tools in Redis (docker/redis-cli not found)"
fi

echo
echo "✅ Tool bootstrap complete"
echo
echo "Tools should now be visible in Monitor UI"
echo "   Check: curl -s $HDN_URL/api/v1/tools | jq '.tools | length'"

