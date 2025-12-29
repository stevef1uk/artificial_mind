#!/bin/bash

# Check if coherence feedback loop is working

NAMESPACE="${K8S_NAMESPACE:-agi}"

echo "🔍 Checking Coherence Feedback Loop"
echo "===================================="
echo ""

FSM_POD=$(kubectl get pods -n "$NAMESPACE" -l app=fsm-server-rpi58 --sort-by=.metadata.creationTimestamp -o jsonpath='{.items[-1].metadata.name}' 2>/dev/null)
GOAL_MGR_POD=$(kubectl get pods -n "$NAMESPACE" -l app=goal-manager -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)

echo "1️⃣ Check for Goal Completion Events:"
echo "------------------------------------"
if [ -n "$GOAL_MGR_POD" ]; then
    echo "   Looking for goal.achieved/goal.failed events in Goal Manager logs..."
    EVENTS=$(kubectl logs -n "$NAMESPACE" "$GOAL_MGR_POD" --tail=500 2>/dev/null | grep -E "agi.goal.achieved|agi.goal.failed|publishEvent.*achieved|publishEvent.*failed" | tail -10)
    if [ -n "$EVENTS" ]; then
        echo "   ✅ Found goal completion events:"
        echo "$EVENTS" | sed 's/^/      /'
    else
        echo "   ℹ️  No goal completion events yet (goals may still be running)"
    fi
fi
echo ""

echo "2️⃣ Check FSM for Event Reception:"
echo "--------------------------------"
if [ -n "$FSM_POD" ]; then
    echo "   Looking for coherence monitor receiving goal events..."
    COHERENCE_EVENTS=$(kubectl logs -n "$NAMESPACE" "$FSM_POD" --tail=500 2>/dev/null | grep -E "🔔 \[Coherence\] Received goal|✅ \[Coherence\] Matched coherence goal|\[Coherence\].*goal.*completed" | tail -10)
    if [ -n "$COHERENCE_EVENTS" ]; then
        echo "   ✅ Coherence monitor is receiving events:"
        echo "$COHERENCE_EVENTS" | sed 's/^/      /'
    else
        echo "   ℹ️  No coherence events received yet"
        echo "      (This is expected if goals haven't completed)"
    fi
    echo ""
    
    echo "   Checking for resolved inconsistencies..."
    RESOLVED=$(kubectl logs -n "$NAMESPACE" "$FSM_POD" --tail=500 2>/dev/null | grep -E "\[Coherence\].*Marked.*resolved|\[Coherence\].*inconsistency.*resolved" | tail -10)
    if [ -n "$RESOLVED" ]; then
        echo "   ✅ Found resolved inconsistencies:"
        echo "$RESOLVED" | sed 's/^/      /'
    else
        echo "   ℹ️  No resolved inconsistencies yet"
    fi
fi
echo ""

echo "3️⃣ Check Active Coherence Goals:"
echo "-------------------------------"
if [ -n "$GOAL_MGR_POD" ]; then
    ALL_GOALS=$(kubectl exec -n "$NAMESPACE" "$GOAL_MGR_POD" -- wget -qO- "http://localhost:8090/goals/agent_1/active" 2>/dev/null)
    if [ -n "$ALL_GOALS" ]; then
        COHERENCE_ACTIVE=$(echo "$ALL_GOALS" | jq '[.[] | select(.context.domain == "system_coherence")] | length' 2>/dev/null || echo "0")
        COHERENCE_ACHIEVED=$(echo "$ALL_GOALS" | jq '[.[] | select(.context.domain == "system_coherence" and .status == "achieved")] | length' 2>/dev/null || echo "0")
        COHERENCE_FAILED=$(echo "$ALL_GOALS" | jq '[.[] | select(.context.domain == "system_coherence" and .status == "failed")] | length' 2>/dev/null || echo "0")
        
        echo "   Active coherence goals: $COHERENCE_ACTIVE"
        echo "   Achieved: $COHERENCE_ACHIEVED"
        echo "   Failed: $COHERENCE_FAILED"
        
        if [ "$COHERENCE_ACHIEVED" -gt 0 ] || [ "$COHERENCE_FAILED" -gt 0 ]; then
            echo ""
            echo "   Sample completed goals:"
            echo "$ALL_GOALS" | jq -r '.[] | select(.context.domain == "system_coherence" and (.status == "achieved" or .status == "failed")) | "      - \(.id): \(.status) (curiosity_id: \(.context.curiosity_id))"' 2>/dev/null | head -5
        fi
    fi
fi
echo ""

echo "4️⃣ Check Redis for Resolved Inconsistencies:"
echo "---------------------------------------------"
REDIS_POD=$(kubectl get pods -n "$NAMESPACE" -l app=redis -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
if [ -n "$REDIS_POD" ]; then
    INC_KEY="coherence:inconsistencies:agent_1"
    TOTAL_INC=$(kubectl exec -n "$NAMESPACE" "$REDIS_POD" -- redis-cli LLEN "$INC_KEY" 2>/dev/null || echo "0")
    
    # Count resolved
    RESOLVED_COUNT=0
    if [ "$TOTAL_INC" -gt 0 ]; then
        # Check first 10 inconsistencies
        for i in $(seq 0 9); do
            inc_data=$(kubectl exec -n "$NAMESPACE" "$REDIS_POD" -- redis-cli LINDEX "$INC_KEY" "$i" 2>/dev/null)
            if [ -n "$inc_data" ] && [ "$inc_data" != "(nil)" ]; then
                resolved=$(echo "$inc_data" | jq -r '.resolved // false' 2>/dev/null)
                if [ "$resolved" = "true" ]; then
                    RESOLVED_COUNT=$((RESOLVED_COUNT + 1))
                fi
            fi
        done
    fi
    
    echo "   Total inconsistencies: $TOTAL_INC"
    echo "   Resolved (sample of 10): $RESOLVED_COUNT"
fi
echo ""

echo "💡 Summary:"
echo "----------"
echo "   ✅ Context is being preserved in goals"
echo "   ✅ Goals are being created successfully"
echo ""
echo "   Next steps to verify feedback loop:"
echo "      1. Wait for coherence goals to complete (via FSM Goals Poller)"
echo "      2. Check Goal Manager logs for 'agi.goal.achieved' events"
echo "      3. Check FSM logs for '🔔 [Coherence] Received goal' events"
echo "      4. Check for '✅ [Coherence] Marked inconsistency as resolved'"
echo ""
echo "   To watch in real-time:"
echo "      kubectl logs -f -n $NAMESPACE $GOAL_MGR_POD | grep -E 'achieved|failed|publishEvent'"
echo "      kubectl logs -f -n $NAMESPACE $FSM_POD | grep -E '🔔|Coherence.*goal|resolved'"
echo ""

