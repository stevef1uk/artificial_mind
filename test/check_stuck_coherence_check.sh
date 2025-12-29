#!/bin/bash

# Check if coherence check is stuck

NAMESPACE="${K8S_NAMESPACE:-agi}"

echo "🔍 Checking for Stuck Coherence Check"
echo "====================================="
echo ""

FSM_POD=$(kubectl get pods -n "$NAMESPACE" -l app=fsm-server-rpi58 --sort-by=.metadata.creationTimestamp -o jsonpath='{.items[-1].metadata.name}' 2>/dev/null)

if [ -z "$FSM_POD" ]; then
    echo "❌ FSM pod not found!"
    exit 1
fi

echo "📦 FSM Pod: $FSM_POD"
echo ""

echo "1️⃣ Check Latest Coherence Activity:"
echo "----------------------------------"
LATEST_ACTIVITY=$(kubectl logs -n "$NAMESPACE" "$FSM_POD" --tail=500 2>/dev/null | grep -iE "\[Coherence\]" | tail -10)
if [ -n "$LATEST_ACTIVITY" ]; then
    echo "   Latest coherence log entries:"
    echo "$LATEST_ACTIVITY" | sed 's/^/      /'
    
    # Check if stuck on belief query
    STUCK_ON_BELIEFS=$(echo "$LATEST_ACTIVITY" | grep -iE "Querying beliefs|Checking.*belief" | tail -1)
    if [ -n "$STUCK_ON_BELIEFS" ]; then
        echo ""
        echo "   ⚠️  Check appears stuck on: $STUCK_ON_BELIEFS"
        echo "   This step can take 2+ minutes for large belief sets"
    fi
else
    echo "   ℹ️  No recent coherence activity"
fi
echo ""

echo "2️⃣ Check Time Since Last Activity:"
echo "---------------------------------"
LAST_LOG_TIME=$(kubectl logs -n "$NAMESPACE" "$FSM_POD" --tail=1000 2>/dev/null | grep -iE "\[Coherence\]" | tail -1 | awk '{print $1, $2}')
if [ -n "$LAST_LOG_TIME" ]; then
    echo "   Last coherence log: $LAST_LOG_TIME"
    echo ""
    echo "   ⏳ If this was more than 5 minutes ago, the check may be stuck"
    echo "   ⏳ Belief contradiction check can take 2-5 minutes"
else
    echo "   ℹ️  Could not determine last activity time"
fi
echo ""

echo "3️⃣ Check FSM Process Status:"
echo "---------------------------"
FSM_STATUS=$(kubectl get pod -n "$NAMESPACE" "$FSM_POD" -o jsonpath='{.status.containerStatuses[0].ready}' 2>/dev/null)
RESTARTS=$(kubectl get pod -n "$NAMESPACE" "$FSM_POD" -o jsonpath='{.status.containerStatuses[0].restartCount}' 2>/dev/null)
echo "   Pod ready: $FSM_STATUS"
echo "   Restarts: $RESTARTS"
echo ""

echo "💡 Diagnosis:"
echo "------------"
echo "   If coherence check is stuck:"
echo "      → Belief contradiction check can be slow (2-5 minutes)"
echo "      → If stuck > 10 minutes, likely an issue"
echo ""
echo "   Solutions:"
echo "      1. Wait a bit longer (belief checks can be slow)"
echo "      2. Restart FSM pod to restart monitor loop:"
echo "         kubectl delete pod -n $NAMESPACE $FSM_POD"
echo "      3. Check FSM resource usage (may be CPU/memory constrained)"
echo ""

echo "🔧 Quick Fix - Restart FSM Pod:"
echo "-------------------------------"
read -p "   Restart FSM pod now? (y/n) " -n 1 -r
echo
if [[ $REPLY =~ ^[Yy]$ ]]; then
    echo "   Restarting FSM pod..."
    kubectl delete pod -n "$NAMESPACE" "$FSM_POD"
    echo "   Waiting for pod to restart..."
    sleep 5
    kubectl wait --for=condition=ready pod -n "$NAMESPACE" -l app=fsm-server-rpi58 --timeout=60s
    echo ""
    echo "   ✅ Pod restarted"
    echo ""
    echo "   Watch for coherence monitor startup:"
    echo "   kubectl logs -f -n $NAMESPACE -l app=fsm-server-rpi58 | grep -i coherence"
fi
echo ""

