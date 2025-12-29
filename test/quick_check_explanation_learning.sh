#!/bin/bash

# Quick check script - just verify explanation learning is working
# Run this after deployment to quickly verify the feature

NAMESPACE="${K8S_NAMESPACE:-agi}"

echo "🔍 Quick Check: Explanation Learning Feature"
echo "============================================"
echo ""

# Check 1: FSM pod is running
echo "1️⃣ Checking FSM pod..."
FSM_POD=$(kubectl get pods -n "$NAMESPACE" -l app=fsm-server-rpi58 -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
if [ -z "$FSM_POD" ]; then
    FSM_POD=$(kubectl get pods -n "$NAMESPACE" -l app=fsm-server -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
fi

if [ -z "$FSM_POD" ]; then
    echo "   ❌ FSM pod not found"
    exit 1
fi

FSM_STATUS=$(kubectl get pod -n "$NAMESPACE" "$FSM_POD" -o jsonpath='{.status.phase}')
if [ "$FSM_STATUS" = "Running" ]; then
    echo "   ✅ FSM pod running: $FSM_POD"
else
    echo "   ⚠️  FSM pod status: $FSM_STATUS"
fi

# Check 2: Look for explanation learning in logs
echo ""
echo "2️⃣ Checking for explanation learning code in logs..."
RECENT_LOGS=$(kubectl logs -n "$NAMESPACE" "$FSM_POD" --tail=100 2>/dev/null)

if echo "$RECENT_LOGS" | grep -q "EXPLANATION-LEARNING"; then
    echo "   ✅ Found explanation learning messages in logs"
    echo "$RECENT_LOGS" | grep "EXPLANATION-LEARNING" | tail -3
else
    echo "   ⚠️  No explanation learning messages yet (this is OK if no goals completed)"
    echo "   💡 Complete a goal to trigger learning feedback"
fi

# Check 3: Verify NATS subscription
echo ""
echo "3️⃣ Checking NATS subscriptions..."
if echo "$RECENT_LOGS" | grep -q "Subscribed to agi.goal.achieved\|Subscribed to agi.goal.failed"; then
    echo "   ✅ NATS subscriptions active"
else
    echo "   ⚠️  NATS subscriptions not found in recent logs"
    echo "   💡 Check if FSM started recently: kubectl logs -n $NAMESPACE $FSM_POD | grep 'Subscribed to'"
fi

# Check 4: Redis keys (if Redis accessible)
echo ""
echo "4️⃣ Checking Redis for learning data..."
REDIS_POD=$(kubectl get pods -n "$NAMESPACE" -l app=redis -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
if [ -z "$REDIS_POD" ]; then
    REDIS_POD=$(kubectl get pods -n "$NAMESPACE" | grep redis | head -1 | awk '{print $1}')
fi

if [ -n "$REDIS_POD" ]; then
    KEYS=$(kubectl exec -n "$NAMESPACE" "$REDIS_POD" -- redis-cli KEYS "explanation_learning:*" 2>/dev/null | wc -l)
    if [ "$KEYS" -gt 0 ]; then
        echo "   ✅ Found $KEYS explanation learning keys in Redis"
        echo "   Sample keys:"
        kubectl exec -n "$NAMESPACE" "$REDIS_POD" -- redis-cli KEYS "explanation_learning:*" 2>/dev/null | head -5
    else
        echo "   ℹ️  No learning data yet (normal if no goals completed)"
    fi
else
    echo "   ⚠️  Redis pod not found"
fi

echo ""
echo "✅ Quick check complete!"
echo ""
echo "📝 Next steps:"
echo "  1. Run full test: ./test/test_explanation_learning.sh"
echo "  2. Watch logs: kubectl logs -f -n $NAMESPACE $FSM_POD | grep EXPLANATION-LEARNING"
echo "  3. Create and complete a goal to trigger learning"
echo ""

