#!/bin/bash

# Investigate FSM pod restarts

NAMESPACE="${K8S_NAMESPACE:-agi}"
FSM_POD=$(kubectl get pods -n "$NAMESPACE" -l app=fsm-server-rpi58 -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)

echo "🔍 Investigating FSM Pod Restarts"
echo "=================================="
echo ""

if [ -z "$FSM_POD" ]; then
    echo "❌ FSM pod not found"
    exit 1
fi

echo "FSM Pod: $FSM_POD"
echo ""

# Check pod events
echo "1️⃣ Pod Events (last 20)..."
echo "-------------------------"
kubectl get events -n "$NAMESPACE" --field-selector involvedObject.name=$FSM_POD --sort-by='.lastTimestamp' | tail -20
echo ""

# Check pod status details
echo "2️⃣ Pod Status Details..."
echo "------------------------"
kubectl get pod -n "$NAMESPACE" "$FSM_POD" -o jsonpath='{.status.containerStatuses[0].lastState.terminated}' | python3 -m json.tool 2>/dev/null || kubectl get pod -n "$NAMESPACE" "$FSM_POD" -o yaml | grep -A 10 "lastState:" | head -15
echo ""

# Check previous container logs (if available)
echo "3️⃣ Previous Container Logs (last 50 lines)..."
echo "----------------------------------------------"
PREV_LOGS=$(kubectl logs -n "$NAMESPACE" "$FSM_POD" --previous 2>&1 | tail -50)
if echo "$PREV_LOGS" | grep -q "Error\|panic\|fatal"; then
    echo "$PREV_LOGS" | grep -iE "error|panic|fatal|crash|nil|dereference" | tail -20
else
    echo "   ℹ️  No obvious errors in previous logs"
    echo "   Last 10 lines:"
    echo "$PREV_LOGS" | tail -10
fi
echo ""

# Check for OOM kills
echo "4️⃣ Checking for OOM kills..."
echo "----------------------------"
kubectl get pod -n "$NAMESPACE" "$FSM_POD" -o jsonpath='{.status.containerStatuses[0].lastState.terminated.reason}' 2>/dev/null | grep -i oom && echo "   ⚠️  Pod was killed due to OOM" || echo "   ✅ No OOM kill detected"
kubectl describe pod -n "$NAMESPACE" "$FSM_POD" | grep -i "oom\|memory\|limit" | head -5
echo ""

# Check resource limits
echo "5️⃣ Resource Limits..."
echo "--------------------"
kubectl get pod -n "$NAMESPACE" "$FSM_POD" -o jsonpath='{.spec.containers[0].resources}' | python3 -m json.tool 2>/dev/null || kubectl describe pod -n "$NAMESPACE" "$FSM_POD" | grep -A 5 "Limits:" | head -10
echo ""

# Check if explanation learning code is in the binary
echo "6️⃣ Checking if explanation learning code is present..."
echo "------------------------------------------------------"
if kubectl exec -n "$NAMESPACE" "$FSM_POD" -- strings /app/fsm-server 2>/dev/null | grep -q "ExplanationLearningFeedback\|EvaluateGoalCompletion"; then
    echo "   ✅ Explanation learning code found in binary"
else
    echo "   ⚠️  Explanation learning code NOT found in binary"
    echo "   💡 Binary may need to be rebuilt"
fi
echo ""

# Check current logs for startup issues
echo "7️⃣ Current Startup Logs (first 50 lines)..."
echo "-------------------------------------------"
kubectl logs -n "$NAMESPACE" "$FSM_POD" 2>/dev/null | head -50 | grep -E "Starting|error|Error|panic|fatal|explanation|NATS|Subscribed" | head -20
echo ""

echo "✅ Investigation complete!"
echo ""
echo "💡 Next steps:"
echo "   1. If OOM: Increase memory limits or optimize memory usage"
echo "   2. If exit code 1/2: Check application startup errors"
echo "   3. If no errors: Check if health checks are failing"
echo ""

