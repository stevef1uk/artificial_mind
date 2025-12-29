#!/bin/bash

# Check if Monitor Service is sending goals to Goal Manager

NAMESPACE="${K8S_NAMESPACE:-agi}"

echo "🔍 Checking Monitor Service → Goal Manager Communication"
echo "========================================================"
echo ""

MONITOR_POD=$(kubectl get pods -n "$NAMESPACE" -l app=monitor-ui -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)

if [ -z "$MONITOR_POD" ]; then
    echo "❌ Monitor pod not found!"
    exit 1
fi

echo "📦 Monitor Pod: $MONITOR_POD"
echo ""

echo "1️⃣ Checking for coherence goal conversions:"
echo "--------------------------------------------"
CONVERSIONS=$(kubectl logs -n "$NAMESPACE" "$MONITOR_POD" --tail=200 | grep "✅ Converted curiosity goal.*system_coherence")
if [ -n "$CONVERSIONS" ]; then
    echo "   Recent conversions:"
    echo "$CONVERSIONS" | tail -10 | sed 's/^/      /'
else
    echo "   ⚠️  No coherence goal conversions found"
fi
echo ""

echo "2️⃣ Checking for debug messages (sending to Goal Manager):"
echo "---------------------------------------------------------"
DEBUG_SEND=$(kubectl logs -n "$NAMESPACE" "$MONITOR_POD" --tail=200 | grep "📤 \[Monitor\] Sending coherence goal")
if [ -n "$DEBUG_SEND" ]; then
    echo "   Found debug messages:"
    echo "$DEBUG_SEND" | tail -10 | sed 's/^/      /'
else
    echo "   ⚠️  No debug messages found (Monitor Service may not have new code deployed)"
fi
echo ""

echo "3️⃣ Checking for errors sending to Goal Manager:"
echo "-----------------------------------------------"
ERRORS=$(kubectl logs -n "$NAMESPACE" "$MONITOR_POD" --tail=200 | grep -iE "goal.*manager.*error|failed.*goal.*manager|goal manager returned|error.*goal" | tail -10)
if [ -n "$ERRORS" ]; then
    echo "   ⚠️  Errors found:"
    echo "$ERRORS" | sed 's/^/      /'
else
    echo "   ✅ No errors found"
fi
echo ""

echo "4️⃣ Checking Monitor Service configuration:"
echo "-------------------------------------------"
GOAL_MGR_URL=$(kubectl exec -n "$NAMESPACE" "$MONITOR_POD" -- env | grep GOAL_MANAGER_URL || echo "not set")
echo "   GOAL_MANAGER_URL: $GOAL_MGR_URL"
echo ""

echo "5️⃣ Testing connectivity to Goal Manager:"
echo "----------------------------------------"
TEST_RESULT=$(kubectl exec -n "$NAMESPACE" "$MONITOR_POD" -- wget -qO- --timeout=5 "http://goal-manager.${NAMESPACE}.svc.cluster.local:8090/goals/agent_1/active" 2>&1)
if echo "$TEST_RESULT" | grep -q "200 OK\|\["; then
    echo "   ✅ Can reach Goal Manager"
else
    echo "   ❌ Cannot reach Goal Manager"
    echo "   Response: $TEST_RESULT"
fi
echo ""

echo "6️⃣ Checking if coherence goals exist in Redis:"
echo "---------------------------------------------"
REDIS_POD=$(kubectl get pods -n "$NAMESPACE" -l app=redis -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
if [ -n "$REDIS_POD" ]; then
    COHERENCE_COUNT=$(kubectl exec -n "$NAMESPACE" "$REDIS_POD" -- redis-cli LLEN "reasoning:curiosity_goals:system_coherence" 2>/dev/null || echo "0")
    echo "   Coherence goals in Redis: $COHERENCE_COUNT"
    
    if [ "$COHERENCE_COUNT" -gt 0 ]; then
        echo ""
        echo "   Sample coherence goals:"
        kubectl exec -n "$NAMESPACE" "$REDIS_POD" -- redis-cli LRANGE "reasoning:curiosity_goals:system_coherence" 0 2 2>/dev/null | \
            grep -o '"id":"[^"]*"' | head -3 | sed 's/^/      - /'
    fi
else
    echo "   ⚠️  Redis pod not found"
fi
echo ""

echo "💡 Summary:"
echo "----------"
if [ -z "$DEBUG_SEND" ]; then
    echo "   ⚠️  Monitor Service may be running old code (no debug messages)"
    echo "   → Rebuild Monitor Service container to get debug logging"
fi

if [ -z "$CONVERSIONS" ]; then
    echo "   ⚠️  No coherence goals being converted"
    echo "   → Check if coherence monitor is generating goals"
    echo "   → Check if Monitor Service is processing system_coherence domain"
fi
echo ""

