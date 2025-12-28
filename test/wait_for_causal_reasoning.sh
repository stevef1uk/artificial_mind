#!/bin/bash

# Script to wait and monitor for causal reasoning to appear in hypotheses

NAMESPACE="${K8S_NAMESPACE:-agi}"
MAX_WAIT=${MAX_WAIT:-300}  # 5 minutes default
CHECK_INTERVAL=${CHECK_INTERVAL:-30}  # Check every 30 seconds

echo "⏳ Waiting for Causal Reasoning in Hypotheses"
echo "=============================================="
echo ""
echo "This will check every $CHECK_INTERVAL seconds for up to $MAX_WAIT seconds"
echo ""

# Find FSM and Redis pods
FSM_POD=$(kubectl get pods -n "$NAMESPACE" --no-headers 2>/dev/null | grep -E "fsm.*Running" | awk '{print $1}' | head -1)
REDIS_POD=$(kubectl get pods -n "$NAMESPACE" --no-headers 2>/dev/null | grep -E "redis.*Running" | awk '{print $1}' | head -1)

if [ -z "$FSM_POD" ] || [ -z "$REDIS_POD" ]; then
    echo "❌ FSM or Redis pod not found"
    exit 1
fi

echo "FSM Pod: $FSM_POD"
echo "Redis Pod: $REDIS_POD"
echo ""

START_TIME=$(date +%s)
ELAPSED=0

# Set up port-forward for FSM API
FSM_PORT_FORWARD_PID=""
if kubectl port-forward -n "$NAMESPACE" "$FSM_POD" 8083:8083 >/dev/null 2>&1 & then
    FSM_PORT_FORWARD_PID=$!
    sleep 2
    echo "📡 Port-forwarded FSM API to localhost:8083"
fi

cleanup() {
    if [ -n "$FSM_PORT_FORWARD_PID" ]; then
        kill $FSM_PORT_FORWARD_PID 2>/dev/null
        wait $FSM_PORT_FORWARD_PID 2>/dev/null
    fi
}
trap cleanup EXIT

while [ $ELAPSED -lt $MAX_WAIT ]; do
    echo "[$ELAPSED s] Checking..."
    
    # Check for [CAUSAL] logs
    CAUSAL_LOGS=$(kubectl logs -n "$NAMESPACE" "$FSM_POD" --since=${CHECK_INTERVAL}s 2>/dev/null | grep -E "\[CAUSAL\]|CAUSAL-DEBUG" | tail -3)
    
    if [ -n "$CAUSAL_LOGS" ]; then
        echo "✅ Found [CAUSAL] log messages!"
        echo "$CAUSAL_LOGS" | sed 's/^/   /'
        echo ""
    fi
    
    # Check FSM API for hypotheses with causal reasoning
    HYP_RESPONSE=$(curl -s http://localhost:8083/hypotheses 2>/dev/null || echo "")
    
    CAUSAL_COUNT=0
    TOTAL_HYP_COUNT=0
    
    if [ -n "$HYP_RESPONSE" ] && echo "$HYP_RESPONSE" | grep -q '\[.*\]'; then
        # Parse JSON response and count hypotheses with causal reasoning
        TOTAL_HYP_COUNT=$(echo "$HYP_RESPONSE" | jq '. | length' 2>/dev/null || echo "0")
        CAUSAL_COUNT=$(echo "$HYP_RESPONSE" | jq '[.[] | select(.causal_type != null and .causal_type != "")] | length' 2>/dev/null || echo "0")
        
        if [ "$CAUSAL_COUNT" -gt 0 ]; then
            echo "✅ SUCCESS! Found $CAUSAL_COUNT hypothesis(es) with causal reasoning fields (out of $TOTAL_HYP_COUNT total)!"
            echo ""
            echo "Run ./test/test_causal_reasoning.sh for full analysis"
            exit 0
        else
            if [ "$TOTAL_HYP_COUNT" -gt 0 ]; then
                echo "   Found $TOTAL_HYP_COUNT hypothesis(es) but none have causal reasoning fields yet"
            else
                echo "   No hypotheses found yet"
            fi
        fi
    else
        # Fallback: check Redis directly
        HYP_KEY="fsm:agent_1:hypotheses"
        HYP_IDS=$(kubectl exec -n "$NAMESPACE" "$REDIS_POD" -- redis-cli HKEYS "$HYP_KEY" 2>/dev/null | grep -v "^$" | head -10)
        
        if [ -n "$HYP_IDS" ]; then
            for HYP_ID in $HYP_IDS; do
                HYP_DATA=$(kubectl exec -n "$NAMESPACE" "$REDIS_POD" -- redis-cli HGET "$HYP_KEY" "$HYP_ID" 2>/dev/null)
                if [ -n "$HYP_DATA" ]; then
                    TOTAL_HYP_COUNT=$((TOTAL_HYP_COUNT + 1))
                    if echo "$HYP_DATA" | grep -q '"causal_type"'; then
                        CAUSAL_COUNT=$((CAUSAL_COUNT + 1))
                    fi
                fi
            done
            
            if [ "$CAUSAL_COUNT" -gt 0 ]; then
                echo "✅ SUCCESS! Found $CAUSAL_COUNT hypothesis(es) with causal reasoning fields (out of $TOTAL_HYP_COUNT total)!"
                echo ""
                echo "Run ./test/test_causal_reasoning.sh for full analysis"
                exit 0
            else
                if [ "$TOTAL_HYP_COUNT" -gt 0 ]; then
                    echo "   Found $TOTAL_HYP_COUNT hypothesis(es) in Redis but none have causal reasoning fields yet"
                else
                    echo "   No hypotheses found in Redis"
                fi
            fi
        else
            echo "   No hypotheses found yet"
        fi
    fi
    
    # Check if hypothesis generation is happening
    HYP_GEN_LOGS=$(kubectl logs -n "$NAMESPACE" "$FSM_POD" --since=${CHECK_INTERVAL}s 2>/dev/null | grep -E "Generating.*hypotheses|Generated.*hypotheses|🧠.*hypotheses" | tail -3)
    if [ -n "$HYP_GEN_LOGS" ]; then
        echo "   📊 Recent hypothesis generation activity:"
        echo "$HYP_GEN_LOGS" | sed 's/^/      /'
    fi
    
    # Check for autonomy cycle activity
    AUTONOMY_LOGS=$(kubectl logs -n "$NAMESPACE" "$FSM_POD" --since=${CHECK_INTERVAL}s 2>/dev/null | grep -E "Autonomy|autonomy.*cycle" | tail -1)
    if [ -n "$AUTONOMY_LOGS" ]; then
        echo "   🔄 Autonomy cycle activity detected"
    fi
    
    echo ""
    
    # Wait before next check
    sleep $CHECK_INTERVAL
    ELAPSED=$((ELAPSED + CHECK_INTERVAL))
done

echo "⏰ Timeout after $MAX_WAIT seconds"
echo ""
echo "Hypothesis generation may not have run yet. Check:"
echo "  1. kubectl logs -n $NAMESPACE $FSM_POD | grep -E 'hypothesis|CAUSAL'"
echo "  2. kubectl logs -n $NAMESPACE $FSM_POD | grep 'Autonomy'"
echo "  3. FSM autonomy cycle runs every 120 seconds"
echo ""

