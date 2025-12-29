#!/bin/bash

# Script to check hypothesis generation status and provide ways to trigger it

NAMESPACE="${K8S_NAMESPACE:-agi}"

echo "🔬 Hypothesis Generation Status"
echo "==============================="
echo ""

# Find FSM pod
FSM_POD=$(kubectl get pods -n "$NAMESPACE" --no-headers 2>/dev/null | grep -E "fsm.*Running" | awk '{print $1}' | head -1)

if [ -z "$FSM_POD" ]; then
    echo "❌ FSM pod not found"
    exit 1
fi

echo "FSM Pod: $FSM_POD"
echo ""

# Check recent logs for hypothesis generation
echo "📊 Recent hypothesis generation activity:"
echo ""

RECENT_HYP_LOGS=$(kubectl logs -n "$NAMESPACE" "$FSM_POD" --tail=200 2>/dev/null | grep -E "Generating.*hypotheses|Generated.*hypotheses|hypothesis.*generated" | tail -5)

if [ -n "$RECENT_HYP_LOGS" ]; then
    echo "$RECENT_HYP_LOGS"
else
    echo "   No recent hypothesis generation activity found"
fi

echo ""
echo "🔍 Checking for [CAUSAL] log messages:"
CAUSAL_LOGS=$(kubectl logs -n "$NAMESPACE" "$FSM_POD" --tail=500 2>/dev/null | grep "\[CAUSAL\]" | tail -3)

if [ -n "$CAUSAL_LOGS" ]; then
    echo "✅ Found causal reasoning logs:"
    echo "$CAUSAL_LOGS"
else
    echo "   ⚠️  No [CAUSAL] logs found yet"
    echo "   (This means no new hypotheses have been generated since restart)"
fi

echo ""
echo "📋 Ways to trigger hypothesis generation:"
echo ""
echo "1. Wait for autonomy cycle (runs every 120 seconds)"
echo "   Current time: $(date)"
echo ""
echo "2. Send input via Monitor UI"
echo "   (Port-forward if needed: kubectl port-forward -n $NAMESPACE svc/monitor-ui 8082:8082)"
echo ""
echo "3. Send NATS event (if nats CLI available):"
echo "   echo 'Test input to trigger hypothesis generation' | nats pub agi.events.input --stdin"
echo ""
echo "4. Watch logs in real-time:"
echo "   kubectl logs -n $NAMESPACE -f $FSM_POD | grep -E 'hypothesis|CAUSAL'"
echo ""

