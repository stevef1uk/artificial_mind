#!/bin/bash

# Directly complete a goal by updating Redis and publishing NATS event
# This bypasses the Goal Manager API

NAMESPACE="${K8S_NAMESPACE:-agi}"
FSM_POD=$(kubectl get pods -n "$NAMESPACE" -l app=fsm-server-rpi58 -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
GOAL_MGR_POD=$(kubectl get pods -n "$NAMESPACE" -l app=goal-manager -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)

echo "🎯 Directly Completing Goal for Explanation Learning"
echo "====================================================="
echo ""

# Get Redis pod
REDIS_POD=$(kubectl get pods -n "$NAMESPACE" -l app=redis -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
if [ -z "$REDIS_POD" ]; then
    REDIS_POD=$(kubectl get pods -n "$NAMESPACE" | grep redis | head -1 | awk '{print $1}')
fi

if [ -z "$REDIS_POD" ]; then
    echo "❌ Redis pod not found"
    exit 1
fi

echo "✅ Redis pod: $REDIS_POD"
echo "✅ FSM pod: $FSM_POD"
echo "✅ Goal Manager pod: $GOAL_MGR_POD"
echo ""

# Get a goal ID
echo "1️⃣ Getting goal ID from active set..."
echo "--------------------------------------"
GOAL_ID=$(kubectl exec -n "$NAMESPACE" "$REDIS_POD" -- redis-cli SMEMBERS "goals:agent_1:active" 2>/dev/null | head -1)

if [ -z "$GOAL_ID" ]; then
    echo "❌ No goals found"
    exit 1
fi

echo "   ✅ Goal ID: $GOAL_ID"
echo ""

# Get goal data
echo "2️⃣ Loading goal data..."
echo "-----------------------"
GOAL_DATA=$(kubectl exec -n "$NAMESPACE" "$REDIS_POD" -- redis-cli GET "goal:$GOAL_ID" 2>/dev/null)

if [ -z "$GOAL_DATA" ]; then
    echo "❌ Could not load goal data"
    exit 1
fi

GOAL_DESC=$(echo "$GOAL_DATA" | python3 -c "import sys, json; d=json.load(sys.stdin); print(d.get('description', 'N/A')[:80])" 2>/dev/null || echo "N/A")
echo "   Description: $GOAL_DESC..."
echo ""

# Start watching logs
echo "3️⃣ Starting log watcher..."
echo "---------------------------"
kubectl logs -f -n "$NAMESPACE" "$FSM_POD" 2>/dev/null | grep --line-buffered "EXPLANATION-LEARNING\|Received.*goal.*achieved\|handleGoalCompletion" &
WATCHER_PID=$!
sleep 2

# Update goal status in Redis
echo "4️⃣ Updating goal status in Redis..."
echo "------------------------------------"
UPDATED_AT=$(date -u +%Y-%m-%dT%H:%M:%SZ)

# Use Python to properly update JSON
UPDATED_DATA=$(echo "$GOAL_DATA" | python3 -c "
import sys, json
goal = json.load(sys.stdin)
goal['status'] = 'achieved'
goal['updated_at'] = '$UPDATED_AT'
print(json.dumps(goal))
" 2>/dev/null)

if [ -z "$UPDATED_DATA" ]; then
    # Fallback: use sed (less reliable but works)
    UPDATED_DATA=$(echo "$GOAL_DATA" | sed 's/"status":"active"/"status":"achieved"/' | sed "s/\"updated_at\":\"[^\"]*\"/\"updated_at\":\"$UPDATED_AT\"/")
fi

# Save updated goal
echo "$UPDATED_DATA" | kubectl exec -i -n "$NAMESPACE" "$REDIS_POD" -- redis-cli SET "goal:$GOAL_ID" > /dev/null

# Update sets
kubectl exec -n "$NAMESPACE" "$REDIS_POD" -- redis-cli SREM "goals:agent_1:active" "$GOAL_ID" > /dev/null
kubectl exec -n "$NAMESPACE" "$REDIS_POD" -- redis-cli SADD "goals:agent_1:history" "$GOAL_ID" > /dev/null

echo "   ✅ Goal status updated to 'achieved'"
echo "   ✅ Removed from active set, added to history"
echo ""

# Publish NATS event via Goal Manager's internal API
echo "5️⃣ Publishing NATS event via Goal Manager API..."
echo "------------------------------------------------"
if [ -n "$GOAL_MGR_POD" ]; then
    # Use Goal Manager's internal API (localhost:8090 from inside the pod)
    # This will trigger UpdateGoalStatus which publishes the NATS event
    ACHIEVE_PAYLOAD='{"result":{"success":true,"test":true,"executed_at":"'$UPDATED_AT'"}}'
    
    echo "   Calling Goal Manager internal API..."
    RESPONSE=$(kubectl exec -n "$NAMESPACE" "$GOAL_MGR_POD" -- sh -c "curl -s -X POST http://localhost:8090/goal/$GOAL_ID/achieve -H 'Content-Type: application/json' -d '$ACHIEVE_PAYLOAD'" 2>/dev/null)
    
    if [ -n "$RESPONSE" ] && echo "$RESPONSE" | grep -q '"status":"achieved"'; then
        echo "   ✅ Goal completed via Goal Manager API"
        echo "   ✅ NATS event should have been published"
    else
        echo "   ⚠️  API call may have failed"
        echo "   Response: ${RESPONSE:0:100}"
        echo ""
        echo "   💡 Goal status is updated in Redis, but event may not be published"
        echo "   💡 Checking Goal Manager logs for event publishing..."
        kubectl logs -n "$NAMESPACE" "$GOAL_MGR_POD" --tail=10 2>/dev/null | grep -i "publishEvent\|goal.*achieved" | tail -3
    fi
else
    echo "   ⚠️  Goal Manager pod not found"
    echo "   💡 Goal status updated in Redis, but event not published"
fi

echo ""
echo "6️⃣ Waiting for explanation learning to process..."
echo "--------------------------------------------------"
sleep 15

# Check for messages
echo ""
echo "7️⃣ Checking for explanation learning messages..."
echo "------------------------------------------------"
RECENT_LOGS=$(kubectl logs -n "$NAMESPACE" "$FSM_POD" --tail=100 2>/dev/null | grep -E "EXPLANATION-LEARNING|handleGoalCompletion|Received.*goal.*achieved")

if [ -n "$RECENT_LOGS" ]; then
    echo "   ✅ SUCCESS! Found explanation learning activity:"
    echo ""
    echo "$RECENT_LOGS"
else
    echo "   ⚠️  No explanation learning messages found"
    echo ""
    echo "   Checking if FSM received any goal events..."
    kubectl logs -n "$NAMESPACE" "$FSM_POD" --tail=50 2>/dev/null | grep -i "goal.*$GOAL_ID\|goal.*achieved" | tail -5
fi

# Stop watcher
kill $WATCHER_PID 2>/dev/null || true

echo ""
echo "8️⃣ Checking Redis for learning data..."
echo "--------------------------------------"
KEYS=$(kubectl exec -n "$NAMESPACE" "$REDIS_POD" -- redis-cli KEYS "explanation_learning:*" 2>/dev/null)
if [ -n "$KEYS" ]; then
    echo "   ✅ Found explanation learning keys:"
    echo "$KEYS" | head -10
else
    echo "   ℹ️  No explanation learning keys found yet"
fi

echo ""
echo "✅ Test complete!"
echo ""
echo "💡 If no messages appeared, the NATS event may not have been published."
echo "   Check Goal Manager logs: kubectl logs -n $NAMESPACE $GOAL_MGR_POD | grep 'publishEvent\|goal.achieved'"
echo ""

