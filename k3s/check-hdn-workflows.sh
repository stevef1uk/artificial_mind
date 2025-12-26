#!/bin/bash
# Check HDN server workflows endpoint and connectivity

echo "🔍 HDN Workflows Diagnostic"
echo "==========================="
echo ""

# Get HDN service URL
HDN_URL="${HDN_URL:-http://hdn-server-rpi58.agi.svc.cluster.local:8080}"
echo "HDN URL: $HDN_URL"
echo ""

# 1. Check HDN health
echo "1. HDN Health Check:"
HEALTH=$(kubectl exec -n agi deployment/hdn-server-rpi58 -- wget -qO- --timeout=5 "$HDN_URL/health" 2>/dev/null || echo "FAILED")
if [ "$HEALTH" = "FAILED" ]; then
    echo "   ❌ HDN health check failed"
else
    echo "   ✅ HDN is healthy: $HEALTH"
fi
echo ""

# 2. Check workflows endpoint with timeout
echo "2. Workflows Endpoint (10s timeout):"
WORKFLOWS=$(kubectl exec -n agi deployment/hdn-server-rpi58 -- timeout 10 wget -qO- --timeout=10 "$HDN_URL/api/v1/hierarchical/workflows" 2>/dev/null || echo "TIMEOUT")
if [ "$WORKFLOWS" = "TIMEOUT" ]; then
    echo "   ⏱️  Workflows endpoint timed out"
else
    WF_COUNT=$(echo "$WORKFLOWS" | jq '.workflows | length' 2>/dev/null || echo "0")
    echo "   Found $WF_COUNT workflows"
    if [ "$WF_COUNT" -gt "0" ]; then
        echo "   Sample workflow IDs:"
        echo "$WORKFLOWS" | jq -r '.workflows[0:3] | .[] | "   - \(.id) (\(.status))"' 2>/dev/null || echo "   (could not parse)"
    fi
fi
echo ""

# 3. Check Redis active_workflows set
echo "3. Redis Active Workflows Set:"
ACTIVE_COUNT=$(kubectl exec -n agi deployment/redis -- redis-cli SCARD "active_workflows" 2>/dev/null || echo "0")
echo "   Active workflows in Redis: $ACTIVE_COUNT"
if [ "$ACTIVE_COUNT" -gt "0" ]; then
    echo "   Workflow IDs:"
    kubectl exec -n agi deployment/redis -- redis-cli SMEMBERS "active_workflows" 2>/dev/null | head -10 | sed 's/^/   - /'
fi
echo ""

# 4. Check Redis workflow records
echo "4. Redis Workflow Records:"
WF_KEYS=$(kubectl exec -n agi deployment/redis -- redis-cli KEYS "workflow:*" 2>/dev/null | wc -l | tr -d ' ')
echo "   Total workflow keys: $WF_KEYS"
if [ "$WF_KEYS" -gt "0" ]; then
    echo "   Sample workflow keys:"
    kubectl exec -n agi deployment/redis -- redis-cli KEYS "workflow:*" 2>/dev/null | head -5 | sed 's/^/   - /'
fi
echo ""

# 5. Check HDN server logs for errors
echo "5. Recent HDN Errors (last 50 lines):"
kubectl -n agi logs deployment/hdn-server-rpi58 --tail=50 | grep -i "error\|Error\|ERROR\|timeout\|Timeout\|TIMEOUT" | tail -10 || echo "   No recent errors found"
echo ""

# 6. Check HDN server logs for workflow-related messages
echo "6. Recent Workflow Activity (last 50 lines):"
kubectl -n agi logs deployment/hdn-server-rpi58 --tail=50 | grep -i "workflow\|orchestrator" | tail -10 || echo "   No workflow activity found"
echo ""

# 7. Check monitor service connectivity to HDN
echo "7. Monitor Service HDN Connectivity:"
MONITOR_HDN_URL=$(kubectl exec -n agi deployment/monitor-ui -- env | grep HDN_URL | cut -d= -f2 2>/dev/null || echo "not found")
echo "   Monitor HDN_URL: $MONITOR_HDN_URL"
echo ""

# 8. Test direct connection from monitor pod
echo "8. Direct Connection Test from Monitor Pod:"
TEST_RESULT=$(kubectl exec -n agi deployment/monitor-ui -- wget -qO- --timeout=5 "$HDN_URL/api/v1/hierarchical/workflows" 2>/dev/null | jq '.workflows | length' 2>/dev/null || echo "FAILED")
if [ "$TEST_RESULT" = "FAILED" ]; then
    echo "   ❌ Monitor cannot reach HDN workflows endpoint"
else
    echo "   ✅ Monitor can reach HDN: $TEST_RESULT workflows"
fi
echo ""

echo "==========================="
echo "✅ Diagnostic complete"
echo ""
echo "💡 If workflows endpoint times out:"
echo "   - Check Redis connectivity from HDN pod"
echo "   - Check if active_workflows set is very large"
echo "   - Check HDN server logs for Redis query issues"
echo ""

