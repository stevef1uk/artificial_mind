#!/bin/bash

# Check if coherence monitor is actually running (even if startup logs are gone)

NAMESPACE="${K8S_NAMESPACE:-agi}"

echo "🔍 Checking if Coherence Monitor is Running"
echo "==========================================="
echo ""

FSM_POD=$(kubectl get pods -n "$NAMESPACE" -l app=fsm-server-rpi58 --sort-by=.metadata.creationTimestamp -o jsonpath='{.items[-1].metadata.name}' 2>/dev/null)

if [ -z "$FSM_POD" ]; then
    echo "❌ FSM pod not found!"
    exit 1
fi

echo "📦 FSM Pod: $FSM_POD"
POD_AGE=$(kubectl get pod -n "$NAMESPACE" "$FSM_POD" -o jsonpath='{.metadata.creationTimestamp}' 2>/dev/null)
echo "   Created: $POD_AGE"
echo ""

echo "1️⃣ Check for ANY Coherence Activity (all logs):"
echo "----------------------------------------------"
# Search ALL logs, not just recent
ALL_COHERENCE=$(kubectl logs -n "$NAMESPACE" "$FSM_POD" 2>/dev/null | grep -iE "\[Coherence\]|coherence" | wc -l | tr -d ' ')
echo "   Total coherence log entries: $ALL_COHERENCE"

if [ "$ALL_COHERENCE" -gt 0 ]; then
    echo "   ✅ Coherence monitor has been active!"
    echo ""
    echo "   First coherence log entry:"
    kubectl logs -n "$NAMESPACE" "$FSM_POD" 2>/dev/null | grep -iE "\[Coherence\]|coherence" | head -1 | sed 's/^/      /'
    echo ""
    echo "   Most recent coherence log entry:"
    kubectl logs -n "$NAMESPACE" "$FSM_POD" 2>/dev/null | grep -iE "\[Coherence\]|coherence" | tail -1 | sed 's/^/      /'
else
    echo "   ❌ No coherence activity found in logs"
fi
echo ""

echo "2️⃣ Check for Coherence Monitor Startup Message:"
echo "-----------------------------------------------"
STARTUP_MSG=$(kubectl logs -n "$NAMESPACE" "$FSM_POD" 2>/dev/null | grep -iE "Coherence monitoring loop started|coherenceMonitoringLoop" | head -1)
if [ -n "$STARTUP_MSG" ]; then
    echo "   ✅ Found startup message:"
    echo "   $STARTUP_MSG" | sed 's/^/      /'
else
    echo "   ⚠️  Startup message not found (logs may have rolled over)"
fi
echo ""

echo "3️⃣ Check for Recent Coherence Checks:"
echo "------------------------------------"
RECENT_CHECKS=$(kubectl logs -n "$NAMESPACE" "$FSM_POD" --tail=2000 2>/dev/null | grep -iE "Starting cross-system coherence check|Coherence check complete" | tail -5)
if [ -n "$RECENT_CHECKS" ]; then
    echo "   ✅ Found recent coherence checks:"
    echo "$RECENT_CHECKS" | sed 's/^/      /'
else
    echo "   ⚠️  No recent coherence checks found"
    echo "   (Monitor may have stopped, or checks are very infrequent)"
fi
echo ""

echo "4️⃣ Check Redis for Evidence of Running Monitor:"
echo "-----------------------------------------------"
REDIS_POD=$(kubectl get pods -n "$NAMESPACE" -l app=redis -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
if [ -n "$REDIS_POD" ]; then
    INC_COUNT=$(kubectl exec -n "$NAMESPACE" "$REDIS_POD" -- redis-cli LLEN "coherence:inconsistencies:agent_1" 2>/dev/null || echo "0")
    MAPPING_COUNT=$(kubectl exec -n "$NAMESPACE" "$REDIS_POD" -- redis-cli KEYS "coherence:goal_mapping:*" 2>/dev/null | wc -l | tr -d ' ')
    
    echo "   Inconsistencies in Redis: $INC_COUNT"
    echo "   Goal mappings: $MAPPING_COUNT"
    
    if [ "$INC_COUNT" -gt 0 ] || [ "$MAPPING_COUNT" -gt 0 ]; then
        echo "   ✅ Evidence that monitor has run (created inconsistencies/mappings)"
    fi
fi
echo ""

echo "💡 Diagnosis:"
echo "------------"
if [ "$ALL_COHERENCE" -eq 0 ]; then
    echo "   ❌ Coherence monitor appears to NOT be running"
    echo "   → No coherence logs found at all"
    echo "   → Monitor may not have started, or stopped"
    echo ""
    echo "   🔧 Solution: Restart FSM pod to see startup logs"
    echo "      kubectl delete pod -n $NAMESPACE $FSM_POD"
    echo "      kubectl logs -f -n $NAMESPACE -l app=fsm-server-rpi58 | grep -i coherence"
elif [ -z "$RECENT_CHECKS" ]; then
    echo "   ⚠️  Coherence monitor started but no recent checks"
    echo "   → Monitor may have stopped, or checks are very slow"
    echo "   → Checks should run every 5 minutes"
    echo ""
    echo "   🔧 Solution: Restart FSM pod to restart monitor loop"
else
    echo "   ✅ Coherence monitor appears to be running"
    echo "   → Found coherence activity in logs"
    echo "   → Recent checks detected"
fi
echo ""

