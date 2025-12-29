#!/bin/bash

# Check if goals are naturally completing and if those trigger explanation learning

NAMESPACE="${K8S_NAMESPACE:-agi}"
FSM_POD=$(kubectl get pods -n "$NAMESPACE" -l app=fsm-server-rpi58 -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
GOAL_MGR_POD=$(kubectl get pods -n "$NAMESPACE" -l app=goal-manager -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)

echo "🔍 Checking Natural Goal Completions and Explanation Learning"
echo "============================================================="
echo ""

echo "1️⃣ Checking for recently completed goals..."
echo "-------------------------------------------"
REDIS_POD=$(kubectl get pods -n "$NAMESPACE" -l app=redis -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
if [ -z "$REDIS_POD" ]; then
    REDIS_POD=$(kubectl get pods -n "$NAMESPACE" | grep redis | head -1 | awk '{print $1}')
fi

if [ -n "$REDIS_POD" ]; then
    # Get goals from history (recently completed)
    HISTORY_GOALS=$(kubectl exec -n "$NAMESPACE" "$REDIS_POD" -- redis-cli SMEMBERS "goals:agent_1:history" 2>/dev/null | head -10)
    
    if [ -n "$HISTORY_GOALS" ]; then
        echo "   ✅ Found goals in history"
        echo "   Checking recent completions..."
        
        RECENT_COMPLETED=""
        for GOAL_ID in $HISTORY_GOALS; do
            GOAL_DATA=$(kubectl exec -n "$NAMESPACE" "$REDIS_POD" -- redis-cli GET "goal:$GOAL_ID" 2>/dev/null)
            if [ -n "$GOAL_DATA" ]; then
                STATUS=$(echo "$GOAL_DATA" | grep -o '"status":"[^"]*"' | head -1 | cut -d'"' -f4)
                UPDATED=$(echo "$GOAL_DATA" | grep -o '"updated_at":"[^"]*"' | head -1 | cut -d'"' -f4)
                
                if [ "$STATUS" = "achieved" ] || [ "$STATUS" = "failed" ]; then
                    # Check if updated in last hour
                    if [ -n "$UPDATED" ]; then
                        RECENT_COMPLETED="$RECENT_COMPLETED $GOAL_ID"
                        echo "   - $GOAL_ID: $STATUS (updated: $UPDATED)"
                    fi
                fi
            fi
        done
        
        if [ -n "$RECENT_COMPLETED" ]; then
            FIRST_RECENT=$(echo $RECENT_COMPLETED | awk '{print $1}')
            echo ""
            echo "   Using recent goal for testing: $FIRST_RECENT"
        fi
    else
        echo "   ℹ️  No goals in history"
    fi
else
    echo "   ⚠️  Redis pod not found"
fi
echo ""

echo "2️⃣ Checking Goal Manager logs for event publishing..."
echo "----------------------------------------------------"
if [ -n "$GOAL_MGR_POD" ]; then
    echo "   Recent publishEvent calls (last 50 lines):"
    kubectl logs -n "$NAMESPACE" "$GOAL_MGR_POD" --tail=50 2>/dev/null | grep -i "publishEvent\|UpdateGoalStatus.*achieved\|UpdateGoalStatus.*failed" | tail -10 || echo "   (No recent events found)"
    
    echo ""
    echo "   All goal.achieved event publications (ever):"
    kubectl logs -n "$NAMESPACE" "$GOAL_MGR_POD" 2>/dev/null | grep -i "publishEvent.*achieved\|UpdateGoalStatus.*achieved" | tail -5 || echo "   (No events found)"
else
    echo "   ⚠️  Goal Manager pod not found"
fi
echo ""

echo "3️⃣ Checking FSM logs for received events..."
echo "-------------------------------------------"
if [ -n "$FSM_POD" ]; then
    echo "   Recent goal completion events received:"
    kubectl logs -n "$NAMESPACE" "$FSM_POD" --tail=200 2>/dev/null | grep -i "Received.*goal\|handleGoalCompletion\|agi.goal.achieved\|agi.goal.failed" | tail -10 || echo "   (No events found)"
    
    echo ""
    echo "   Explanation learning activity:"
    kubectl logs -n "$NAMESPACE" "$FSM_POD" 2>/dev/null | grep "EXPLANATION-LEARNING" | tail -10 || echo "   (No explanation learning activity found)"
else
    echo "   ⚠️  FSM pod not found"
fi
echo ""

echo "4️⃣ Summary and Recommendations..."
echo "----------------------------------"
echo "   If no events are being published:"
echo "   - Goals may be completing but UpdateGoalStatus not being called"
echo "   - Check if workflows are calling Goal Manager API when they complete"
echo ""
echo "   If events are published but not received:"
echo "   - Check NATS connectivity"
echo "   - Verify FSM subscription is active"
echo ""
echo "   If events are received but no explanation learning:"
echo "   - Check handleGoalCompletion function in FSM logs"
echo "   - Verify explanationLearning is initialized"
echo ""

