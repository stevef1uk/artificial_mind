#!/bin/bash

# Check Goal Manager status and activity

NAMESPACE="${K8S_NAMESPACE:-agi}"

echo "🔍 Goal Manager Status Check"
echo "============================"
echo ""

# Get pod
GOAL_MGR_POD=$(kubectl get pods -n "$NAMESPACE" -l app=goal-manager -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)

if [ -z "$GOAL_MGR_POD" ]; then
    echo "❌ Goal Manager pod not found!"
    exit 1
fi

echo "📦 Pod Information:"
echo "------------------"
kubectl get pod -n "$NAMESPACE" "$GOAL_MGR_POD"
echo ""

echo "📊 Pod Status:"
echo "-------------"
kubectl get pod -n "$NAMESPACE" "$GOAL_MGR_POD" -o jsonpath='{.status.phase}' && echo ""
kubectl get pod -n "$NAMESPACE" "$GOAL_MGR_POD" -o jsonpath='{.status.containerStatuses[0].ready}' && echo " (ready)"
echo ""

echo "📝 Recent Logs (last 30 lines):"
echo "-------------------------------"
kubectl logs -n "$NAMESPACE" "$GOAL_MGR_POD" --tail=30
echo ""

echo "🔍 Checking for debug messages:"
echo "-------------------------------"
DEBUG_MSGS=$(kubectl logs -n "$NAMESPACE" "$GOAL_MGR_POD" --tail=100 | grep -E "📥|✅|🐛|DEBUG|Received goal")
if [ -n "$DEBUG_MSGS" ]; then
    echo "$DEBUG_MSGS" | tail -10
else
    echo "   ℹ️  No debug messages found (may not have received any goals yet)"
fi
echo ""

echo "🌐 Testing HTTP Endpoint:"
echo "------------------------"
# Try to reach the endpoint from within the cluster
GOAL_MGR_SVC="goal-manager.${NAMESPACE}.svc.cluster.local:8090"
echo "   Testing: http://$GOAL_MGR_SVC/goals/agent_1/active"

# Try from within the pod
TEST_RESULT=$(kubectl exec -n "$NAMESPACE" "$GOAL_MGR_POD" -- wget -qO- "http://localhost:8090/goals/agent_1/active" 2>/dev/null)
if [ $? -eq 0 ] && [ -n "$TEST_RESULT" ]; then
    GOAL_COUNT=$(echo "$TEST_RESULT" | jq 'length' 2>/dev/null || echo "?")
    echo "   ✅ Endpoint responding (found $GOAL_COUNT active goals)"
else
    echo "   ❌ Endpoint not responding or empty"
    echo "   Checking if server started..."
    kubectl logs -n "$NAMESPACE" "$GOAL_MGR_POD" | grep -iE "listening|started|error" | tail -5
fi
echo ""

echo "📡 Checking for incoming requests:"
echo "---------------------------------"
REQUESTS=$(kubectl logs -n "$NAMESPACE" "$GOAL_MGR_POD" --tail=200 | grep -iE "POST|GET|/goal" | tail -10)
if [ -n "$REQUESTS" ]; then
    echo "$REQUESTS"
else
    echo "   ℹ️  No HTTP requests logged (Monitor Service may not be sending goals)"
fi
echo ""

echo "🔗 Checking Monitor Service:"
echo "---------------------------"
MONITOR_POD=$(kubectl get pods -n "$NAMESPACE" -l app=monitor-ui -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
if [ -n "$MONITOR_POD" ]; then
    echo "   Monitor pod: $MONITOR_POD"
    RECENT_CONVERSIONS=$(kubectl logs -n "$NAMESPACE" "$MONITOR_POD" --tail=50 | grep "✅ Converted curiosity goal.*system_coherence" | tail -5)
    if [ -n "$RECENT_CONVERSIONS" ]; then
        echo "   Recent coherence goal conversions:"
        echo "$RECENT_CONVERSIONS" | sed 's/^/      /'
    else
        echo "   ℹ️  No recent coherence goal conversions"
    fi
    
    # Check for errors sending to Goal Manager
    ERRORS=$(kubectl logs -n "$NAMESPACE" "$MONITOR_POD" --tail=100 | grep -iE "goal.*manager.*error|failed.*goal.*manager|goal manager returned" | tail -5)
    if [ -n "$ERRORS" ]; then
        echo ""
        echo "   ⚠️  Errors sending to Goal Manager:"
        echo "$ERRORS" | sed 's/^/      /'
    fi
else
    echo "   ⚠️  Monitor pod not found"
fi
echo ""

echo "💡 Troubleshooting:"
echo "------------------"
echo "   If Goal Manager is not receiving requests:"
echo "     1. Check Monitor Service logs for errors"
echo "     2. Verify GOAL_MANAGER_URL env var in Monitor Service"
echo "     3. Test network connectivity: kubectl exec -n $NAMESPACE $MONITOR_POD -- wget -qO- http://goal-manager.$NAMESPACE.svc.cluster.local:8090/health"
echo ""
echo "   If Goal Manager is not starting:"
echo "     1. Check pod events: kubectl describe pod -n $NAMESPACE $GOAL_MGR_POD"
echo "     2. Check for image pull errors"
echo "     3. Verify the image was built correctly"
echo ""

