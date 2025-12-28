#!/bin/bash

# Diagnostic script to check why hypothesis generation isn't happening

NAMESPACE="${K8S_NAMESPACE:-agi}"

echo "🔍 Diagnosing Hypothesis Generation"
echo "===================================="
echo ""

# Find FSM pod
FSM_POD=$(kubectl get pods -n "$NAMESPACE" --no-headers 2>/dev/null | grep -E "fsm.*Running" | awk '{print $1}' | head -1)
REDIS_POD=$(kubectl get pods -n "$NAMESPACE" --no-headers 2>/dev/null | grep -E "redis.*Running" | awk '{print $1}' | head -1)

if [ -z "$FSM_POD" ]; then
    echo "❌ FSM pod not found"
    exit 1
fi

echo "FSM Pod: $FSM_POD"
echo "Redis Pod: $REDIS_POD"
echo ""

# 1. Check if autonomy is enabled
echo "1️⃣ Checking Autonomy Configuration..."
AUTONOMY_ENABLED=$(kubectl exec -n "$NAMESPACE" "$FSM_POD" -- env 2>/dev/null | grep -E "^FSM_AUTONOMY=" | cut -d'=' -f2 || echo "")
AUTONOMY_INTERVAL=$(kubectl exec -n "$NAMESPACE" "$FSM_POD" -- env 2>/dev/null | grep "FSM_AUTONOMY_EVERY" | cut -d'=' -f2 || echo "")

if [ "$AUTONOMY_ENABLED" = "true" ]; then
    echo "   ✅ Autonomy is enabled"
    if [ -n "$AUTONOMY_INTERVAL" ]; then
        echo "   ⏱️  Autonomy interval: ${AUTONOMY_INTERVAL} seconds"
    else
        echo "   ⚠️  Autonomy interval not set (using default)"
    fi
else
    echo "   ❌ Autonomy is NOT enabled!"
    echo "   Set FSM_AUTONOMY=true in deployment"
fi
echo ""

# 2. Check recent autonomy cycles
echo "2️⃣ Recent Autonomy Activity (last 5 minutes)..."
AUTONOMY_LOGS=$(kubectl logs -n "$NAMESPACE" "$FSM_POD" --since=5m 2>/dev/null | grep -E "\[Autonomy\]|Autonomy.*cycle|Triggering autonomy" | tail -5)
if [ -n "$AUTONOMY_LOGS" ]; then
    echo "$AUTONOMY_LOGS" | sed 's/^/   /'
else
    echo "   ⚠️  No autonomy activity in last 5 minutes"
fi
echo ""

# 3. Check if FSM is reaching hypothesize state
echo "3️⃣ State Transitions (last 5 minutes)..."
STATE_LOGS=$(kubectl logs -n "$NAMESPACE" "$FSM_POD" --since=5m 2>/dev/null | grep -E "Transition|hypothesize|🧠.*hypotheses" | tail -10)
if [ -n "$STATE_LOGS" ]; then
    echo "$STATE_LOGS" | sed 's/^/   /'
else
    echo "   ⚠️  No state transition logs found"
fi
echo ""

# 4. Check for facts/concepts available
echo "4️⃣ Checking for Facts and Concepts..."
if [ -n "$REDIS_POD" ]; then
    # Check facts
    FACTS_KEY="fsm:agent_1:facts"
    FACT_COUNT=$(kubectl exec -n "$NAMESPACE" "$REDIS_POD" -- redis-cli HLEN "$FACTS_KEY" 2>/dev/null || echo "0")
    echo "   Facts in Redis: $FACT_COUNT"
    
    # Check concepts
    CONCEPTS_KEY="fsm:agent_1:concepts"
    CONCEPT_COUNT=$(kubectl exec -n "$NAMESPACE" "$REDIS_POD" -- redis-cli HLEN "$CONCEPTS_KEY" 2>/dev/null || echo "0")
    echo "   Concepts in Redis: $CONCEPT_COUNT"
    
    if [ "$FACT_COUNT" = "0" ] && [ "$CONCEPT_COUNT" = "0" ]; then
        echo "   ⚠️  No facts or concepts available - hypothesis generation needs these!"
    fi
else
    echo "   ⚠️  Cannot check Redis (pod not found)"
fi
echo ""

# 5. Check existing hypotheses
echo "5️⃣ Existing Hypotheses..."
if [ -n "$REDIS_POD" ]; then
    HYP_KEY="fsm:agent_1:hypotheses"
    HYP_COUNT=$(kubectl exec -n "$NAMESPACE" "$REDIS_POD" -- redis-cli HLEN "$HYP_KEY" 2>/dev/null || echo "0")
    echo "   Total hypotheses: $HYP_COUNT"
    
    if [ "$HYP_COUNT" -gt 0 ]; then
        echo "   Sample hypothesis IDs:"
        kubectl exec -n "$NAMESPACE" "$REDIS_POD" -- redis-cli HKEYS "$HYP_KEY" 2>/dev/null | head -3 | sed 's/^/      /'
    fi
else
    echo "   ⚠️  Cannot check Redis"
fi
echo ""

# 6. Check for hypothesis generation errors
echo "6️⃣ Hypothesis Generation Errors/Warnings (last 10 minutes)..."
ERROR_LOGS=$(kubectl logs -n "$NAMESPACE" "$FSM_POD" --since=10m 2>/dev/null | grep -iE "hypothesis.*fail|hypothesis.*error|hypothesis.*warn|No hypotheses|duplicate hypothesis" | tail -10)
if [ -n "$ERROR_LOGS" ]; then
    echo "$ERROR_LOGS" | sed 's/^/   /'
else
    echo "   ✅ No errors found"
fi
echo ""

# 7. Check for causal reasoning debug logs
echo "7️⃣ Causal Reasoning Debug Logs (last 10 minutes)..."
CAUSAL_LOGS=$(kubectl logs -n "$NAMESPACE" "$FSM_POD" --since=10m 2>/dev/null | grep -E "CAUSAL|enrichHypothesis" | tail -10)
if [ -n "$CAUSAL_LOGS" ]; then
    echo "$CAUSAL_LOGS" | sed 's/^/   /'
else
    echo "   ⚠️  No causal reasoning logs found (hypothesis generation may not be running)"
fi
echo ""

# 8. Check FSM current state
echo "8️⃣ Current FSM State..."
# Try to get state from FSM API
if kubectl port-forward -n "$NAMESPACE" "$FSM_POD" 8083:8083 >/dev/null 2>&1 & then
    PORT_FORWARD_PID=$!
    sleep 2
    STATE_RESPONSE=$(curl -s http://localhost:8083/state 2>/dev/null)
    kill $PORT_FORWARD_PID 2>/dev/null
    wait $PORT_FORWARD_PID 2>/dev/null
    
    if [ -n "$STATE_RESPONSE" ]; then
        CURRENT_STATE=$(echo "$STATE_RESPONSE" | jq -r '.state // .current_state // "unknown"' 2>/dev/null || echo "unknown")
        echo "   Current state: $CURRENT_STATE"
    else
        echo "   ⚠️  Could not retrieve state from API"
    fi
else
    echo "   ⚠️  Could not port-forward to FSM API"
fi
echo ""

echo "📋 Summary & Recommendations:"
echo "==============================="
echo ""
if [ "$AUTONOMY_ENABLED" != "true" ]; then
    echo "❌ CRITICAL: Autonomy is disabled - hypotheses won't generate automatically"
    echo "   Fix: Set FSM_AUTONOMY=true in deployment"
fi

if [ "$FACT_COUNT" = "0" ] && [ "$CONCEPT_COUNT" = "0" ]; then
    echo "⚠️  No facts or concepts available"
    echo "   Hypothesis generation needs facts/concepts to work"
    echo "   Try: Send some input to the FSM to generate facts"
fi

if [ -z "$AUTONOMY_LOGS" ]; then
    echo "⚠️  No autonomy activity detected"
    echo "   Wait for next autonomy cycle (every ${AUTONOMY_INTERVAL:-120} seconds)"
fi

echo ""
echo "💡 To trigger hypothesis generation manually:"
echo "   1. Send input via NATS or Monitor UI"
echo "   2. Wait for autonomy cycle (every ${AUTONOMY_INTERVAL:-120}s)"
echo "   3. Check logs: kubectl logs -n $NAMESPACE $FSM_POD | grep -E 'hypothesis|CAUSAL'"

