#!/bin/bash

# Quick Deploy Script for Avatar Context MCP Tool
# This script rebuilds and deploys the HDN server with the new search_avatar_context tool

set -e

echo "🚀 Deploying Avatar Context MCP Tool"
echo "======================================"
echo ""

# Navigate to project root
cd /home/stevef/dev/artificial_mind

# Build and deploy
echo "📦 Building and deploying HDN server..."
./k3s/rebuild-and-deploy-hdn.sh

echo ""
echo "⏳ Waiting for deployment to stabilize..."
sleep 10

echo ""
echo "🧪 Testing the new tool..."
echo ""

# Get the HDN pod
HDN_POD=$(kubectl get pods -n agi -l app=hdn-server-rpi58 -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)

if [ -n "$HDN_POD" ]; then
    echo "✅ HDN Pod: $HDN_POD"
    
    # Test 1: List tools
    echo ""
    echo "Test 1: Checking if search_avatar_context is registered..."
    kubectl exec -n agi $HDN_POD -- wget -q -O- \
        --post-data='{"jsonrpc":"2.0","id":1,"method":"tools/list"}' \
        --header='Content-Type: application/json' \
        http://localhost:8080/api/v1/mcp 2>/dev/null | \
        jq '.result.tools[] | select(.name == "search_avatar_context")' || echo "❌ Tool not found"
    
    # Test 2: Search for Accenture
    echo ""
    echo "Test 2: Searching for 'Accenture'..."
    kubectl exec -n agi $HDN_POD -- wget -q -O- \
        --post-data='{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"search_avatar_context","arguments":{"query":"Accenture","limit":2}}}' \
        --header='Content-Type: application/json' \
        http://localhost:8080/api/v1/mcp 2>/dev/null | jq '.'
    
    echo ""
    echo "✅ Deployment complete!"
    echo ""
    echo "You can now ask questions like:"
    echo "  - Did I work for Accenture?"
    echo "  - What companies have I worked for?"
    echo "  - What are my skills?"
    
else
    echo "❌ Could not find HDN pod"
    exit 1
fi
