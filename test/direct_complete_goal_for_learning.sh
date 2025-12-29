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

# Set up port-forwarding from host (not inside pod)
echo "3.5️⃣ Setting up port-forwarding..."
echo "-----------------------------------"
# Check if port 8090 is already in use
if lsof -ti:8090 > /dev/null 2>&1; then
    echo "   ℹ️  Port 8090 already in use (may be another port-forward)"
    PORT_FORWARD_PID=""
else
    echo "   Setting up port-forward for Goal Manager..."
    kubectl port-forward -n "$NAMESPACE" svc/goal-manager 8090:8090 > /dev/null 2>&1 &
    PORT_FORWARD_PID=$!
    sleep 3
    
    if curl -s --connect-timeout 2 http://localhost:8090/health > /dev/null 2>&1; then
        echo "   ✅ Port-forward established"
    else
        echo "   ⚠️  Port-forward may have failed"
        PORT_FORWARD_PID=""
    fi
fi

# Complete goal via Goal Manager API
echo ""
echo "4️⃣ Completing goal via Goal Manager API..."
echo "-------------------------------------------"
UPDATED_AT=$(date -u +%Y-%m-%dT%H:%M:%SZ)
ACHIEVE_PAYLOAD='{"result":{"success":true,"test":true,"executed_at":"'$UPDATED_AT'","manual_test":true}}'

echo "   POST http://localhost:8090/goal/$GOAL_ID/achieve"

# Try calling API from host (via port-forward)
RESPONSE=$(curl -s -w "\nHTTP_CODE:%{http_code}" -X POST "http://localhost:8090/goal/$GOAL_ID/achieve" \
    -H "Content-Type: application/json" \
    -d "$ACHIEVE_PAYLOAD" 2>&1)

HTTP_CODE=$(echo "$RESPONSE" | grep "HTTP_CODE:" | cut -d: -f2)
RESPONSE_BODY=$(echo "$RESPONSE" | grep -v "HTTP_CODE:")

if [ "$HTTP_CODE" = "200" ] || echo "$RESPONSE_BODY" | grep -q '"status":"achieved"'; then
    echo "   ✅ Goal completed successfully (HTTP $HTTP_CODE)"
    echo "   ✅ NATS event should have been published"
    SUCCESS=true
else
    echo "   ⚠️  API call failed (HTTP ${HTTP_CODE:-unknown})"
    echo "   Response: ${RESPONSE_BODY:0:200}"
    SUCCESS=false
    
    # Fallback: Update Redis directly (but event won't be published)
    echo ""
    echo "   💡 Fallback: Updating Redis directly..."
    UPDATED_DATA=$(echo "$GOAL_DATA" | python3 -c "
import sys, json
goal = json.load(sys.stdin)
goal['status'] = 'achieved'
goal['updated_at'] = '$UPDATED_AT'
print(json.dumps(goal))
" 2>/dev/null || echo "$GOAL_DATA" | sed 's/"status":"active"/"status":"achieved"/')
    
    echo "$UPDATED_DATA" | kubectl exec -i -n "$NAMESPACE" "$REDIS_POD" -- redis-cli SET "goal:$GOAL_ID" > /dev/null
    kubectl exec -n "$NAMESPACE" "$REDIS_POD" -- redis-cli SREM "goals:agent_1:active" "$GOAL_ID" > /dev/null
    kubectl exec -n "$NAMESPACE" "$REDIS_POD" -- redis-cli SADD "goals:agent_1:history" "$GOAL_ID" > /dev/null
    
    echo "   ✅ Goal updated in Redis"
    echo "   ⚠️  But NATS event NOT published (Goal Manager API not called)"
    echo "   💡 Explanation learning will NOT trigger without the NATS event"
fi

# Clean up port-forward
if [ -n "$PORT_FORWARD_PID" ]; then
    kill $PORT_FORWARD_PID 2>/dev/null || true
fi

echo ""
echo "6️⃣ Waiting for explanation learning to process..."
echo "--------------------------------------------------"
sleep 15

# Stop watcher first to capture its output
kill $WATCHER_PID 2>/dev/null || true
sleep 1

# Check for messages in recent logs
echo ""
echo "7️⃣ Checking for explanation learning messages..."
echo "------------------------------------------------"
# Check for any explanation learning activity related to this goal (use --tail with larger number and no --since for better compatibility)
RECENT_LOGS=$(kubectl logs -n "$NAMESPACE" "$FSM_POD" --tail=500 2>/dev/null | grep -i "EXPLANATION-LEARNING" | grep -i "$GOAL_ID")

if [ -n "$RECENT_LOGS" ]; then
    echo "   ✅ SUCCESS! Found explanation learning activity for goal $GOAL_ID:"
    echo ""
    echo "$RECENT_LOGS" | head -10
else
    # Fallback: check for any recent explanation learning messages
    ANY_EL_LOGS=$(kubectl logs -n "$NAMESPACE" "$FSM_POD" --tail=500 2>/dev/null | grep -i "EXPLANATION-LEARNING.*Evaluating\|EXPLANATION-LEARNING.*Completed evaluation" | tail -3)
    if [ -n "$ANY_EL_LOGS" ]; then
        echo "   ✅ Found recent explanation learning activity:"
        echo ""
        echo "$ANY_EL_LOGS"
        echo ""
        echo "   💡 Note: Messages may have appeared in the 'Waiting...' section above"
    else
        echo "   ⚠️  No explanation learning messages found in recent logs"
        echo "   💡 Check the 'Waiting...' section above - messages may appear there"
        echo ""
        echo "   Checking if FSM received the goal event..."
        kubectl logs -n "$NAMESPACE" "$FSM_POD" --tail=100 2>/dev/null | grep -i "EXPLANATION-LEARNING\|Received.*goal.*$GOAL_ID" | tail -5
    fi
fi

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

