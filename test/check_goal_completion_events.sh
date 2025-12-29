#!/bin/bash

# Check if goal completion events are being published and received

NAMESPACE="${K8S_NAMESPACE:-agi}"
FSM_POD=$(kubectl get pods -n "$NAMESPACE" -l app=fsm-server-rpi58 -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
GOAL_MGR_POD=$(kubectl get pods -n "$NAMESPACE" -l app=goal-manager -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)

echo "🔍 Checking Goal Completion Event Flow"
echo "======================================="
echo ""

echo "1️⃣ Checking Goal Manager for event publishing..."
echo "------------------------------------------------"
if [ -n "$GOAL_MGR_POD" ]; then
    echo "   Goal Manager pod: $GOAL_MGR_POD"
    echo "   Recent publishEvent calls:"
    kubectl logs -n "$NAMESPACE" "$GOAL_MGR_POD" --tail=100 2>/dev/null | grep -i "publishEvent\|goal.*achieved\|goal.*failed" | tail -10 || echo "   (No recent events found)"
else
    echo "   ⚠️  Goal Manager pod not found"
fi
echo ""

echo "2️⃣ Checking FSM for goal completion event reception..."
echo "------------------------------------------------------"
if [ -n "$FSM_POD" ]; then
    echo "   FSM pod: $FSM_POD"
    echo "   Recent goal completion events received:"
    kubectl logs -n "$NAMESPACE" "$FSM_POD" --tail=200 2>/dev/null | grep -i "Received.*goal\|handleGoalCompletion\|agi.goal.achieved\|agi.goal.failed" | tail -10 || echo "   (No recent events found)"
    
    echo ""
    echo "   Checking for explanation learning handler..."
    kubectl logs -n "$NAMESPACE" "$FSM_POD" 2>/dev/null | grep -i "handleGoalCompletion\|EXPLANATION-LEARNING.*Evaluating" | tail -5 || echo "   (No explanation learning activity found)"
else
    echo "   ⚠️  FSM pod not found"
fi
echo ""

echo "3️⃣ Testing NATS connectivity..."
echo "-------------------------------"
# Check if we can see NATS events
echo "   Checking if NATS is accessible from pods..."
kubectl exec -n "$NAMESPACE" "$GOAL_MGR_POD" -- sh -c 'echo "NATS_URL: $NATS_URL"' 2>/dev/null || echo "   (Cannot check NATS_URL)"

echo ""
echo "4️⃣ Checking for any completed goals in history..."
echo "-------------------------------------------------"
REDIS_POD=$(kubectl get pods -n "$NAMESPACE" -l app=redis -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
if [ -n "$REDIS_POD" ]; then
    HISTORY_COUNT=$(kubectl exec -n "$NAMESPACE" "$REDIS_POD" -- redis-cli SCARD "goals:agent_1:history" 2>/dev/null)
    echo "   Goals in history: $HISTORY_COUNT"
    
    if [ "$HISTORY_COUNT" -gt 0 ]; then
        echo "   Sample goal IDs from history:"
        kubectl exec -n "$NAMESPACE" "$REDIS_POD" -- redis-cli SMEMBERS "goals:agent_1:history" 2>/dev/null | head -5
        
        echo ""
        echo "   Checking if any of these triggered explanation learning..."
        SAMPLE_GOAL=$(kubectl exec -n "$NAMESPACE" "$REDIS_POD" -- redis-cli SMEMBERS "goals:agent_1:history" 2>/dev/null | head -1)
        if [ -n "$SAMPLE_GOAL" ]; then
            GOAL_DATA=$(kubectl exec -n "$NAMESPACE" "$REDIS_POD" -- redis-cli GET "goal:$SAMPLE_GOAL" 2>/dev/null)
            GOAL_STATUS=$(echo "$GOAL_DATA" | grep -o '"status":"[^"]*"' | head -1 | cut -d'"' -f4)
            GOAL_UPDATED=$(echo "$GOAL_DATA" | grep -o '"updated_at":"[^"]*"' | head -1 | cut -d'"' -f4)
            echo "   Sample goal: $SAMPLE_GOAL"
            echo "   Status: $GOAL_STATUS"
            echo "   Updated: $GOAL_UPDATED"
        fi
    fi
else
    echo "   ⚠️  Redis pod not found"
fi

echo ""
echo "✅ Check complete!"
echo ""
echo "💡 If no events are being published/received:"
echo "   1. Goals may be completing but events not being published"
echo "   2. NATS connectivity issue between Goal Manager and FSM"
echo "   3. FSM may not be receiving the events even if published"
echo ""

