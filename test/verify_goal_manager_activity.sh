#!/bin/bash

# Verify Goal Manager is receiving and processing requests

NAMESPACE="${K8S_NAMESPACE:-agi}"

echo "🔍 Verifying Goal Manager Activity"
echo "==================================="
echo ""

GOAL_MGR_POD=$(kubectl get pods -n "$NAMESPACE" -l app=goal-manager --sort-by=.metadata.creationTimestamp -o jsonpath='{.items[-1].metadata.name}' 2>/dev/null)

if [ -z "$GOAL_MGR_POD" ]; then
    echo "❌ Goal Manager pod not found!"
    exit 1
fi

echo "📦 Current Goal Manager Pod:"
echo "---------------------------"
kubectl get pod -n "$NAMESPACE" "$GOAL_MGR_POD" -o wide
echo ""

POD_AGE=$(kubectl get pod -n "$NAMESPACE" "$GOAL_MGR_POD" -o jsonpath='{.metadata.creationTimestamp}' 2>/dev/null)
echo "   Pod created: $POD_AGE"
echo ""

echo "📝 Recent Logs (last 30 lines):"
echo "------------------------------"
kubectl logs -n "$NAMESPACE" "$GOAL_MGR_POD" --tail=30 2>/dev/null
echo ""

echo "🔍 Checking for Incoming Requests:"
echo "---------------------------------"
REQUEST_COUNT=$(kubectl logs -n "$NAMESPACE" "$GOAL_MGR_POD" --tail=500 2>/dev/null | grep -c "POST /goal request received" || echo "0")
echo "   Total POST /goal requests (last 500 lines): $REQUEST_COUNT"

if [ "$REQUEST_COUNT" -gt 0 ]; then
    echo ""
    echo "   ✅ Goal Manager IS receiving requests!"
    echo ""
    echo "   Recent requests (last 10):"
    kubectl logs -n "$NAMESPACE" "$GOAL_MGR_POD" --tail=500 2>/dev/null | grep "POST /goal request received" | tail -10 | sed 's/^/      /'
    
    echo ""
    echo "   Recent goal creations with context:"
    kubectl logs -n "$NAMESPACE" "$GOAL_MGR_POD" --tail=500 2>/dev/null | grep "✅ \[GoalManager\] Goal created with context preserved" | tail -5 | sed 's/^/      /'
else
    echo ""
    echo "   ⚠️  No requests found in recent logs"
    echo ""
    echo "   Checking if pod just started..."
    STARTUP_TIME=$(kubectl logs -n "$NAMESPACE" "$GOAL_MGR_POD" 2>/dev/null | grep "Goal Manager REST listening" | head -1)
    if [ -n "$STARTUP_TIME" ]; then
        echo "   Pod started: $STARTUP_TIME"
        echo ""
        echo "   ⏳ If pod just restarted, wait 30-60 seconds for Monitor Service to send goals"
    fi
fi
echo ""

echo "🌐 Testing HTTP Endpoint:"
echo "------------------------"
# Test from within cluster
TEST_POD=$(kubectl get pods -n "$NAMESPACE" -l app=monitor-ui -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
if [ -n "$TEST_POD" ]; then
    echo "   Testing from Monitor Service pod..."
    TEST_RESULT=$(kubectl exec -n "$NAMESPACE" "$TEST_POD" -- wget -qO- --timeout=5 "http://goal-manager.${NAMESPACE}.svc.cluster.local:8090/goals/agent_1/active" 2>&1)
    if [ $? -eq 0 ] && [ -n "$TEST_RESULT" ]; then
        GOAL_COUNT=$(echo "$TEST_RESULT" | jq 'length' 2>/dev/null || echo "?")
        echo "   ✅ Endpoint responding (found $GOAL_COUNT active goals)"
        
        COHERENCE_COUNT=$(echo "$TEST_RESULT" | jq '[.[] | select(.context.domain == "system_coherence")] | length' 2>/dev/null || echo "0")
        echo "   Coherence goals: $COHERENCE_COUNT"
    else
        echo "   ❌ Endpoint not responding"
    fi
else
    echo "   ⚠️  No test pod available"
fi
echo ""

echo "📊 Active Goals Summary:"
echo "----------------------"
if [ -n "$GOAL_MGR_POD" ]; then
    ALL_GOALS=$(kubectl exec -n "$NAMESPACE" "$GOAL_MGR_POD" -- wget -qO- "http://localhost:8090/goals/agent_1/active" 2>/dev/null)
    if [ -n "$ALL_GOALS" ]; then
        TOTAL=$(echo "$ALL_GOALS" | jq 'length' 2>/dev/null || echo "0")
        COHERENCE=$(echo "$ALL_GOALS" | jq '[.[] | select(.context.domain == "system_coherence")] | length' 2>/dev/null || echo "0")
        echo "   Total active goals: $TOTAL"
        echo "   Coherence goals: $COHERENCE"
    fi
fi
echo ""

echo "💡 Troubleshooting:"
echo "------------------"
if [ "$REQUEST_COUNT" -eq 0 ]; then
    echo "   If no requests are showing:"
    echo "      1. Check Monitor Service is running: kubectl get pods -n $NAMESPACE -l app=monitor-ui"
    echo "      2. Check Monitor Service logs: kubectl logs -n $NAMESPACE -l app=monitor-ui | grep 'Converted curiosity goal'"
    echo "      3. Wait 30-60 seconds after pod restart for goals to be sent"
    echo "      4. Verify network: kubectl exec -n $NAMESPACE $TEST_POD -- wget -qO- http://goal-manager.$NAMESPACE.svc.cluster.local:8090/health"
else
    echo "   ✅ Goal Manager is working correctly!"
    echo "   To watch requests in real-time:"
    echo "      kubectl logs -f -n $NAMESPACE $GOAL_MGR_POD | grep -E '📥|POST /goal'"
fi
echo ""

