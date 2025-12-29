#!/bin/bash
# Quick test to verify workflow connectivity is working

NAMESPACE="agi"
HDN_URL="http://hdn-server-rpi58.agi.svc.cluster.local:8080"

echo "🔍 Quick Workflow Connectivity Test"
echo "===================================="
echo ""

# Test 1: HDN Health
echo "1. Testing HDN health..."
HEALTH=$(kubectl exec -n $NAMESPACE deployment/hdn-server-rpi58 -- timeout 3 wget -qO- --timeout=3 "$HDN_URL/health" 2>&1)
if echo "$HEALTH" | grep -q "healthy\|ok"; then
    echo "   ✅ HDN is healthy"
else
    echo "   ❌ HDN health check failed: $HEALTH"
    exit 1
fi

# Test 2: HDN_URL config
echo "2. Checking HDN_URL configuration..."
HDN_ENV=$(kubectl exec -n $NAMESPACE deployment/hdn-server-rpi58 -- env | grep HDN_URL | cut -d= -f2)
if [ "$HDN_ENV" = "http://hdn-server-rpi58.agi.svc.cluster.local:8080" ]; then
    echo "   ✅ HDN_URL correctly configured"
else
    echo "   ❌ HDN_URL incorrect: $HDN_ENV"
    exit 1
fi

# Test 3: Workflows endpoint
echo "3. Testing workflows endpoint..."
WORKFLOWS=$(kubectl exec -n $NAMESPACE deployment/monitor-ui -- timeout 5 wget -qO- --timeout=5 "$HDN_URL/api/v1/hierarchical/workflows" 2>&1)
if echo "$WORKFLOWS" | jq -e '.workflows' >/dev/null 2>&1; then
    WF_COUNT=$(echo "$WORKFLOWS" | jq '.workflows | length')
    echo "   ✅ Workflows endpoint accessible ($WF_COUNT workflows)"
else
    echo "   ❌ Workflows endpoint failed"
    exit 1
fi

# Test 4: FSM -> HDN connection
echo "4. Testing FSM to HDN connection..."
FSM_TEST=$(kubectl exec -n $NAMESPACE deployment/fsm-server-rpi58 -- timeout 3 wget -qO- --timeout=3 "$HDN_URL/health" 2>&1)
if echo "$FSM_TEST" | grep -q "healthy\|ok"; then
    echo "   ✅ FSM can reach HDN"
else
    echo "   ❌ FSM cannot reach HDN"
    exit 1
fi

echo ""
echo "✅ All connectivity tests passed!"
echo ""
echo "To run comprehensive workflow tests, use:"
echo "  ./k3s/test-workflows.sh"

