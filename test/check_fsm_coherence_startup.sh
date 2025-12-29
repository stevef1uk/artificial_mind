#!/bin/bash

# Check why coherence monitor isn't starting in FSM

NAMESPACE="${K8S_NAMESPACE:-agi}"

echo "🔍 Checking FSM Coherence Monitor Startup"
echo "========================================="
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

echo "1️⃣ Check FSM Startup Sequence:"
echo "-----------------------------"
echo "   First 100 lines of logs (startup sequence):"
STARTUP_LOGS=$(kubectl logs -n "$NAMESPACE" "$FSM_POD" 2>/dev/null | head -100)
echo "$STARTUP_LOGS" | sed 's/^/      /'
echo ""

echo "2️⃣ Search for Coherence Monitor Startup:"
echo "----------------------------------------"
COHERENCE_START=$(kubectl logs -n "$NAMESPACE" "$FSM_POD" 2>/dev/null | grep -iE "Coherence.*monitor|coherence.*loop|coherenceMonitoringLoop" | head -10)
if [ -n "$COHERENCE_START" ]; then
    echo "   ✅ Found coherence monitor references:"
    echo "$COHERENCE_START" | sed 's/^/      /'
else
    echo "   ❌ No coherence monitor startup messages found"
fi
echo ""

echo "3️⃣ Check for Errors:"
echo "-------------------"
ERRORS=$(kubectl logs -n "$NAMESPACE" "$FSM_POD" 2>/dev/null | grep -iE "error|panic|fatal|nil.*coherence|coherence.*nil|failed.*coherence" | tail -20)
if [ -n "$ERRORS" ]; then
    echo "   ⚠️  Errors found:"
    echo "$ERRORS" | sed 's/^/      /'
else
    echo "   ✅ No errors found"
fi
echo ""

echo "4️⃣ Check FSM Engine Initialization:"
echo "-----------------------------------"
ENGINE_LOGS=$(kubectl logs -n "$NAMESPACE" "$FSM_POD" 2>/dev/null | grep -iE "Starting FSM|FSM engine|NewFSMEngine|coherenceMonitor" | head -10)
if [ -n "$ENGINE_LOGS" ]; then
    echo "   FSM engine logs:"
    echo "$ENGINE_LOGS" | sed 's/^/      /'
else
    echo "   ⚠️  No FSM engine initialization logs found"
fi
echo ""

echo "5️⃣ Check Pod Age vs Expected Behavior:"
echo "--------------------------------------"
# Check if pod is very new (might not have started monitor yet)
POD_START_TIME=$(kubectl get pod -n "$NAMESPACE" "$FSM_POD" -o jsonpath='{.status.startTime}' 2>/dev/null)
if [ -n "$POD_START_TIME" ]; then
    echo "   Pod start time: $POD_START_TIME"
    echo ""
    echo "   ⏳ Coherence monitor starts 10 seconds after FSM engine starts"
    echo "   ⏳ First coherence check runs 10 seconds after monitor starts"
    echo "   ⏳ Subsequent checks run every 5 minutes"
fi
echo ""

echo "💡 Diagnosis:"
echo "------------"
echo "   If coherence monitor 'not started' but system has 200 inconsistencies:"
echo "      → Monitor WAS running (created the inconsistencies)"
echo "      → Current FSM pod may have restarted"
echo "      → Monitor should start automatically on pod startup"
echo ""
echo "   To verify monitor is starting:"
echo "      kubectl logs -f -n $NAMESPACE $FSM_POD | grep -i coherence"
echo ""
echo "   If monitor doesn't start:"
echo "      1. Check FSM code version matches latest"
echo "      2. Restart FSM pod: kubectl delete pod -n $NAMESPACE $FSM_POD"
echo "      3. Watch startup logs for coherence monitor initialization"
echo ""
