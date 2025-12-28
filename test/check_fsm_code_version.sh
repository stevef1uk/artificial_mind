#!/bin/bash

# Quick script to check if FSM pod has the causal reasoning code

NAMESPACE="${K8S_NAMESPACE:-agi}"

echo "🔍 Checking FSM Code Version"
echo "============================"
echo ""

# Find FSM pod
FSM_POD=$(kubectl get pods -n "$NAMESPACE" --no-headers 2>/dev/null | grep -E "fsm.*Running" | awk '{print $1}' | head -1)

if [ -z "$FSM_POD" ]; then
    echo "❌ FSM pod not found"
    exit 1
fi

echo "FSM Pod: $FSM_POD"
echo ""

# Check when pod was started
echo "Pod Start Time:"
kubectl get pod "$FSM_POD" -n "$NAMESPACE" -o jsonpath='{.status.startTime}' 2>/dev/null
echo ""
echo ""

# Check if we can see the causal reasoning code in the binary
echo "Checking for causal reasoning code in FSM binary..."
echo ""

# Try to exec into pod and check if the binary has the new functions
if kubectl exec -n "$NAMESPACE" "$FSM_POD" -- sh -c 'strings /app/fsm-server 2>/dev/null | grep -q "enrichHypothesisWithCausalReasoning"' 2>/dev/null; then
    echo "✅ Found 'enrichHypothesisWithCausalReasoning' in binary"
else
    echo "⚠️  Could not find 'enrichHypothesisWithCausalReasoning' in binary"
    echo "   (This might be normal if binary is stripped or function name is different)"
fi

if kubectl exec -n "$NAMESPACE" "$FSM_POD" -- sh -c 'strings /app/fsm-server 2>/dev/null | grep -q "classifyCausalType"' 2>/dev/null; then
    echo "✅ Found 'classifyCausalType' in binary"
else
    echo "⚠️  Could not find 'classifyCausalType' in binary"
fi

if kubectl exec -n "$NAMESPACE" "$FSM_POD" -- sh -c 'strings /app/fsm-server 2>/dev/null | grep -q "generateInterventionGoals"' 2>/dev/null; then
    echo "✅ Found 'generateInterventionGoals' in binary"
else
    echo "⚠️  Could not find 'generateInterventionGoals' in binary"
fi

echo ""
echo "Checking recent logs for causal reasoning..."
RECENT_LOGS=$(kubectl logs -n "$NAMESPACE" "$FSM_POD" --tail=100 2>/dev/null | grep -i "causal\|hypothesis" | tail -5)

if [ -n "$RECENT_LOGS" ]; then
    echo "Recent hypothesis/causal logs:"
    echo "$RECENT_LOGS"
else
    echo "No recent hypothesis/causal logs found"
fi

echo ""
echo "To trigger new hypothesis generation:"
echo "  1. Wait for autonomy cycle"
echo "  2. Or send NATS event: echo 'test' | nats pub agi.events.input --stdin"
echo "  3. Or use Monitor UI"
echo ""
echo "Then check new hypotheses:"
echo "  ./test/test_causal_reasoning.sh"

