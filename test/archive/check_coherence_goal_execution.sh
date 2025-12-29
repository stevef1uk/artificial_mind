#!/bin/bash

# Check if coherence goals are being executed by FSM Goals Poller

NAMESPACE="${K8S_NAMESPACE:-agi}"

echo "🔍 Checking Coherence Goal Execution"
echo "===================================="
echo ""

FSM_POD=$(kubectl get pods -n "$NAMESPACE" -l app=fsm-server-rpi58 --sort-by=.metadata.creationTimestamp -o jsonpath='{.items[-1].metadata.name}' 2>/dev/null)
GOAL_MGR_POD=$(kubectl get pods -n "$NAMESPACE" -l app=goal-manager -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)

echo "1️⃣ Check Active Coherence Goals in Goal Manager:"
echo "------------------------------------------------"
if [ -n "$GOAL_MGR_POD" ]; then
    ALL_GOALS=$(kubectl exec -n "$NAMESPACE" "$GOAL_MGR_POD" -- wget -qO- "http://localhost:8090/goals/agent_1/active" 2>/dev/null)
    if [ -n "$ALL_GOALS" ]; then
        COHERENCE_GOALS=$(echo "$ALL_GOALS" | jq '[.[] | select(.context.domain == "system_coherence")]' 2>/dev/null)
        COHERENCE_COUNT=$(echo "$COHERENCE_GOALS" | jq 'length' 2>/dev/null || echo "0")
        
        echo "   Active coherence goals: $COHERENCE_COUNT"
        
        if [ "$COHERENCE_COUNT" -gt 0 ]; then
            echo ""
            echo "   Sample coherence goals:"
            echo "$COHERENCE_GOALS" | jq -r '.[] | "      - \(.id): \(.description[0:60])... (status: \(.status))"' 2>/dev/null | head -5
            
            # Get goal IDs
            GOAL_IDS=$(echo "$COHERENCE_GOALS" | jq -r '.[].id' 2>/dev/null | head -5)
        else
            echo "   ⚠️  No active coherence goals found"
        fi
    fi
fi
echo ""

echo "2️⃣ Check FSM Goals Poller Activity:"
echo "----------------------------------"
if [ -n "$FSM_POD" ]; then
    echo "   Recent goal triggers (last 20):"
    TRIGGERS=$(kubectl logs -n "$NAMESPACE" "$FSM_POD" --tail=500 2>/dev/null | grep -E "\[FSM\]\[Goals\].*triggered.*goal" | tail -20)
    if [ -n "$TRIGGERS" ]; then
        echo "$TRIGGERS" | sed 's/^/      /'
        
        # Check if any coherence goals were triggered
        COHERENCE_TRIGGERS=$(echo "$TRIGGERS" | grep -E "coherence|system_coherence" || true)
        if [ -n "$COHERENCE_TRIGGERS" ]; then
            echo ""
            echo "   ✅ Found coherence goal triggers!"
            echo "$COHERENCE_TRIGGERS" | sed 's/^/      /'
        else
            echo ""
            echo "   ⚠️  No coherence-specific triggers found"
            echo "      (Coherence goals may be mixed with other goals)"
        fi
    else
        echo "      ℹ️  No recent goal triggers found"
    fi
    echo ""
    
    # Check for workflow completions
    echo "   Recent workflow completions (last 10):"
    COMPLETIONS=$(kubectl logs -n "$NAMESPACE" "$FSM_POD" --tail=500 2>/dev/null | grep -E "\[FSM\]\[Goals\].*workflow.*completed" | tail -10)
    if [ -n "$COMPLETIONS" ]; then
        echo "$COMPLETIONS" | sed 's/^/      /'
    else
        echo "      ℹ️  No recent workflow completions"
    fi
fi
echo ""

echo "3️⃣ Check Goal Manager for Completed Coherence Goals:"
echo "-----------------------------------------------------"
if [ -n "$GOAL_MGR_POD" ] && [ -n "$GOAL_IDS" ]; then
    echo "   Checking status of sample coherence goals..."
    for goal_id in $GOAL_IDS; do
        GOAL_STATUS=$(kubectl exec -n "$NAMESPACE" "$GOAL_MGR_POD" -- wget -qO- "http://localhost:8090/goal/$goal_id" 2>/dev/null | jq -r '.status' 2>/dev/null)
        if [ -n "$GOAL_STATUS" ] && [ "$GOAL_STATUS" != "null" ]; then
            echo "      $goal_id: $GOAL_STATUS"
        fi
    done
else
    echo "   ℹ️  No coherence goals to check"
fi
echo ""

echo "4️⃣ Check for Goal Status Updates:"
echo "---------------------------------"
if [ -n "$GOAL_MGR_POD" ]; then
    echo "   Recent goal status updates in Goal Manager logs..."
    STATUS_UPDATES=$(kubectl logs -n "$NAMESPACE" "$GOAL_MGR_POD" --tail=200 2>/dev/null | grep -E "UpdateGoalStatus|achieved|failed" | tail -10)
    if [ -n "$STATUS_UPDATES" ]; then
        echo "$STATUS_UPDATES" | sed 's/^/      /'
    else
        echo "      ℹ️  No status updates found"
    fi
fi
echo ""

echo "💡 Analysis:"
echo "-----------"
echo "   If coherence goals exist but aren't being triggered:"
echo "      → FSM Goals Poller may be prioritizing other goals"
echo "      → Or goals are being triggered but workflows are slow"
echo ""
echo "   If goals are triggered but not completing:"
echo "      → Check HDN workflow execution logs"
echo "      → Check for workflow errors"
echo ""
echo "   To manually trigger a coherence goal completion (for testing):"
echo "      kubectl exec -n $NAMESPACE $GOAL_MGR_POD -- wget --post-data='' -qO- http://localhost:8090/goal/<goal_id>/achieve"
echo ""

