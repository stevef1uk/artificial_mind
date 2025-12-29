#!/bin/bash

# Check if FSM is crashing due to explanation learning code

NAMESPACE="${K8S_NAMESPACE:-agi}"
FSM_POD=$(kubectl get pods -n "$NAMESPACE" -l app=fsm-server-rpi58 -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)

echo "🔍 Checking FSM for Crashes and Errors"
echo "======================================="
echo ""

if [ -z "$FSM_POD" ]; then
    echo "❌ FSM pod not found"
    exit 1
fi

echo "FSM Pod: $FSM_POD"
echo ""

# Check pod status
echo "1️⃣ Pod Status:"
echo "-------------"
kubectl get pod -n "$NAMESPACE" "$FSM_POD" -o jsonpath='{.status.containerStatuses[0].restartCount}' 2>/dev/null | xargs -I {} echo "   Restart count: {}"
kubectl get pod -n "$NAMESPACE" "$FSM_POD" -o jsonpath='{.status.containerStatuses[0].lastState.terminated.reason}' 2>/dev/null | xargs -I {} echo "   Last termination reason: {}" || echo "   (No recent terminations)"
echo ""

# Check for panic/crash errors
echo "2️⃣ Checking for panic/crash errors..."
echo "--------------------------------------"
PANIC_LOGS=$(kubectl logs -n "$NAMESPACE" "$FSM_POD" --previous 2>/dev/null | grep -i "panic\|fatal\|crash\|runtime error" | tail -10)
if [ -n "$PANIC_LOGS" ]; then
    echo "   ⚠️  Found panic/crash errors in previous logs:"
    echo "$PANIC_LOGS"
else
    echo "   ✅ No panic/crash errors in previous logs"
fi

CURRENT_PANIC=$(kubectl logs -n "$NAMESPACE" "$FSM_POD" 2>/dev/null | grep -i "panic\|fatal\|crash\|runtime error" | tail -10)
if [ -n "$CURRENT_PANIC" ]; then
    echo "   ⚠️  Found panic/crash errors in current logs:"
    echo "$CURRENT_PANIC"
else
    echo "   ✅ No panic/crash errors in current logs"
fi
echo ""

# Check for explanation learning errors
echo "3️⃣ Checking for explanation learning errors..."
echo "----------------------------------------------"
EL_ERRORS=$(kubectl logs -n "$NAMESPACE" "$FSM_POD" 2>/dev/null | grep -i "EXPLANATION-LEARNING.*error\|EXPLANATION-LEARNING.*fail\|explanation.*learning.*nil\|explanationLearning.*nil" | tail -10)
if [ -n "$EL_ERRORS" ]; then
    echo "   ⚠️  Found explanation learning errors:"
    echo "$EL_ERRORS"
else
    echo "   ✅ No explanation learning errors found"
fi
echo ""

# Check for nil pointer or initialization errors
echo "4️⃣ Checking for nil pointer/initialization errors..."
echo "----------------------------------------------------"
NIL_ERRORS=$(kubectl logs -n "$NAMESPACE" "$FSM_POD" 2>/dev/null | grep -i "nil pointer\|nil.*dereference\|invalid memory\|cannot call.*nil" | tail -10)
if [ -n "$NIL_ERRORS" ]; then
    echo "   ⚠️  Found nil pointer errors:"
    echo "$NIL_ERRORS"
else
    echo "   ✅ No nil pointer errors found"
fi
echo ""

# Check recent errors
echo "5️⃣ Recent errors (last 100 lines)..."
echo "-----------------------------------"
RECENT_ERRORS=$(kubectl logs -n "$NAMESPACE" "$FSM_POD" --tail=100 2>/dev/null | grep -iE "error|fail|panic|fatal" | tail -15)
if [ -n "$RECENT_ERRORS" ]; then
    echo "$RECENT_ERRORS"
else
    echo "   ✅ No recent errors found"
fi
echo ""

# Check if explanation learning is initialized
echo "6️⃣ Checking explanation learning initialization..."
echo "-------------------------------------------------"
INIT_LOGS=$(kubectl logs -n "$NAMESPACE" "$FSM_POD" 2>/dev/null | grep -i "NewExplanationLearningFeedback\|explanation.*learning.*init\|explanationLearning" | head -5)
if [ -n "$INIT_LOGS" ]; then
    echo "   Found initialization references:"
    echo "$INIT_LOGS"
else
    echo "   ⚠️  No initialization logs found (may be normal if no goals completed)"
fi
echo ""

# Check startup sequence
echo "7️⃣ Recent startup sequence..."
echo "----------------------------"
kubectl logs -n "$NAMESPACE" "$FSM_POD" --tail=50 2>/dev/null | grep -E "Starting|subscrib|NATS|explanation|FSM" | tail -10
echo ""

echo "✅ Check complete!"
echo ""
echo "💡 If FSM is crashing:"
echo "   1. Check previous pod logs: kubectl logs -n $NAMESPACE $FSM_POD --previous"
echo "   2. Check pod events: kubectl describe pod -n $NAMESPACE $FSM_POD"
echo "   3. Look for nil pointer errors related to explanationLearning"
echo ""

