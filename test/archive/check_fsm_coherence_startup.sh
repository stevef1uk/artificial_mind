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
echo "   Created: $(kubectl get pod -n "$NAMESPACE" "$FSM_POD" -o jsonpath='{.metadata.creationTimestamp}')"
echo ""

echo "1️⃣ Check FSM Startup Logs:"
echo "-------------------------"
echo "   Looking for coherence monitor startup message..."
STARTUP_LOGS=$(kubectl logs -n "$NAMESPACE" "$FSM_POD" 2>/dev/null | grep -iE "Coherence|coherence.*monitor|coherence.*loop" | head -20)
if [ -n "$STARTUP_LOGS" ]; then
    echo "$STARTUP_LOGS" | sed 's/^/      /'
else
    echo "      ❌ No coherence-related startup messages found"
fi
echo ""

echo "2️⃣ Check for Errors:"
echo "-------------------"
ERRORS=$(kubectl logs -n "$NAMESPACE" "$FSM_POD" 2>/dev/null | grep -iE "error|panic|fatal|nil.*coherence|coherence.*nil" | tail -10)
if [ -n "$ERRORS" ]; then
    echo "   ⚠️  Errors found:"
    echo "$ERRORS" | sed 's/^/      /'
else
    echo "   ✅ No errors found"
fi
echo ""

echo "3️⃣ Check FSM Engine Initialization:"
echo "-----------------------------------"
ENGINE_LOGS=$(kubectl logs -n "$NAMESPACE" "$FSM_POD" 2>/dev/null | grep -iE "Starting FSM|FSM engine|NewFSMEngine" | head -5)
if [ -n "$ENGINE_LOGS" ]; then
    echo "   FSM engine initialization:"
    echo "$ENGINE_LOGS" | sed 's/^/      /'
else
    echo "   ⚠️  No FSM engine initialization logs found"
fi
echo ""

echo "4️⃣ Check Full Startup Sequence:"
echo "--------------------------------"
echo "   First 50 lines of logs:"
kubectl logs -n "$NAMESPACE" "$FSM_POD" 2>/dev/null | head -50 | sed 's/^/      /'
echo ""

echo "5️⃣ Check if Coherence Monitor Field Exists:"
echo "-------------------------------------------"
# This is harder to check, but we can look for the NewCoherenceMonitor call
MONITOR_CREATE=$(kubectl logs -n "$NAMESPACE" "$FSM_POD" 2>/dev/null | grep -iE "NewCoherenceMonitor|coherence.*monitor.*created" | head -5)
if [ -n "$MONITOR_CREATE" ]; then
    echo "   Found coherence monitor creation:"
    echo "$MONITOR_CREATE" | sed 's/^/      /'
else
    echo "   ⚠️  No coherence monitor creation logs found"
    echo "      (This might be normal if it's created silently)"
fi
echo ""

echo "💡 Diagnosis:"
echo "------------"
echo "   If coherence monitor 'not started' but system is working:"
echo "      → Monitor may have started but logs rolled over"
echo "      → Or monitor is created but loop goroutine didn't start"
echo ""
echo "   To fix:"
echo "      1. Check if FSM pod needs restart"
echo "      2. Check FSM code version matches latest"
echo "      3. Restart FSM pod: kubectl delete pod -n $NAMESPACE $FSM_POD"
echo ""

