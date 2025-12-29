#!/bin/bash

# Complete an existing goal from Redis to test explanation learning

NAMESPACE="${K8S_NAMESPACE:-agi}"
GOAL_MGR_URL="${GOAL_MGR_URL:-http://localhost:8090}"
FSM_POD=$(kubectl get pods -n "$NAMESPACE" -l app=fsm-server-rpi58 -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)

echo "🎯 Completing Existing Goal to Test Explanation Learning"
echo "========================================================="
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

echo "✅ Found Redis pod: $REDIS_POD"
echo ""

# Get a goal ID from the active set
echo "1️⃣ Getting a goal ID from active set..."
echo "----------------------------------------"
GOAL_ID=$(kubectl exec -n "$NAMESPACE" "$REDIS_POD" -- redis-cli SMEMBERS "goals:agent_1:active" 2>/dev/null | head -1)

if [ -z "$GOAL_ID" ]; then
    echo "   ❌ No goals found in active set"
    exit 1
fi

echo "   ✅ Found goal: $GOAL_ID"
echo ""

# Get goal details from Redis
echo "2️⃣ Loading goal details..."
echo "--------------------------"
GOAL_DATA=$(kubectl exec -n "$NAMESPACE" "$REDIS_POD" -- redis-cli GET "goal:$GOAL_ID" 2>/dev/null)

if [ -n "$GOAL_DATA" ]; then
    GOAL_DESC=$(echo "$GOAL_DATA" | grep -o '"description":"[^"]*"' | head -1 | cut -d'"' -f4)
    GOAL_STATUS=$(echo "$GOAL_DATA" | grep -o '"status":"[^"]*"' | head -1 | cut -d'"' -f4)
    echo "   Description: ${GOAL_DESC:-N/A}"
    echo "   Status: ${GOAL_STATUS:-N/A}"
else
    echo "   ⚠️  Could not load goal details from Redis"
fi
echo ""

# Start watching logs
echo "3️⃣ Starting log watcher..."
echo "---------------------------"
echo "   Watching for explanation learning messages..."
kubectl logs -f -n "$NAMESPACE" "$FSM_POD" 2>/dev/null | grep --line-buffered "EXPLANATION-LEARNING" &
WATCHER_PID=$!
sleep 2

# Check if Goal Manager is accessible, set up port-forward if needed
echo "3.5️⃣ Checking Goal Manager accessibility..."
echo "-------------------------------------------"
PORT_FORWARD_PID=""

if ! curl -s --connect-timeout 2 "$GOAL_MGR_URL/health" > /dev/null 2>&1; then
    echo "   ⚠️  Goal Manager not accessible at $GOAL_MGR_URL"
    echo "   💡 Setting up port-forward..."
    
    # Check if port-forward already exists
    if ! lsof -ti:8090 > /dev/null 2>&1; then
        kubectl port-forward -n "$NAMESPACE" svc/goal-manager 8090:8090 > /dev/null 2>&1 &
        PORT_FORWARD_PID=$!
        sleep 3
        
        if curl -s --connect-timeout 2 "$GOAL_MGR_URL/health" > /dev/null 2>&1; then
            echo "   ✅ Port-forward established"
        else
            echo "   ⚠️  Port-forward may have failed, trying direct Redis update..."
            PORT_FORWARD_PID=""
        fi
    else
        echo "   ℹ️  Port 8090 already in use (may be another port-forward)"
        sleep 2
    fi
else
    echo "   ✅ Goal Manager is accessible"
fi

if curl -s --connect-timeout 2 "$GOAL_MGR_URL/health" > /dev/null 2>&1; then
    echo "   ✅ Goal Manager is accessible"
    echo ""
    echo "4️⃣ Completing goal via Goal Manager API..."
    echo "-------------------------------------------"
    ACHIEVE_PAYLOAD='{
      "result": {
        "success": true,
        "test": true,
        "executed_at": "'$(date -u +%Y-%m-%dT%H:%M:%SZ)'",
        "manual_completion": true
      }
    }'
    
    echo "   POST $GOAL_MGR_URL/goal/$GOAL_ID/achieve"
    RESPONSE=$(curl -s -w "\nHTTP_CODE:%{http_code}" -X POST "$GOAL_MGR_URL/goal/$GOAL_ID/achieve" \
        -H "Content-Type: application/json" \
        -d "$ACHIEVE_PAYLOAD")
    
    HTTP_CODE=$(echo "$RESPONSE" | grep "HTTP_CODE:" | cut -d: -f2)
    RESPONSE_BODY=$(echo "$RESPONSE" | grep -v "HTTP_CODE:")
else
    # Fallback: Update Redis directly and use Goal Manager pod to publish event
    echo "   ⚠️  API not accessible, updating Redis directly..."
    GOAL_MGR_POD=$(kubectl get pods -n "$NAMESPACE" -l app=goal-manager -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
    
    if [ -n "$GOAL_MGR_POD" ]; then
        echo "   Updating goal in Redis and publishing event via Goal Manager pod..."
        
        # Use Goal Manager's internal method via kubectl exec
        ACHIEVE_PAYLOAD='{"result":{"success":true,"test":true}}'
        RESPONSE=$(kubectl exec -n "$NAMESPACE" "$GOAL_MGR_POD" -- sh -c "curl -s -X POST http://localhost:8090/goal/$GOAL_ID/achieve -H 'Content-Type: application/json' -d '$ACHIEVE_PAYLOAD'" 2>/dev/null)
        
        if [ -n "$RESPONSE" ] && echo "$RESPONSE" | grep -q '"status":"achieved"'; then
            HTTP_CODE="200"
            RESPONSE_BODY="$RESPONSE"
            echo "   ✅ Goal completed via Goal Manager pod"
        else
            HTTP_CODE="500"
            RESPONSE_BODY=""
            echo "   ⚠️  Direct completion failed"
        fi
    else
        HTTP_CODE="000"
        RESPONSE_BODY=""
        echo "   ❌ Goal Manager pod not found"
    fi
fi

# Clean up port-forward if we created it
if [ -n "$PORT_FORWARD_PID" ]; then
    kill $PORT_FORWARD_PID 2>/dev/null || true
fi

if [ "$HTTP_CODE" = "200" ] || echo "$RESPONSE_BODY" | grep -q '"status":"achieved"'; then
    echo "   ✅ Goal marked as achieved (HTTP $HTTP_CODE)"
    echo "   Response: $RESPONSE_BODY" | head -3
else
    echo "   ⚠️  Unexpected response (HTTP $HTTP_CODE):"
    echo "   $RESPONSE_BODY"
fi
echo ""

# Wait for processing
echo "5️⃣ Waiting for explanation learning to process..."
echo "--------------------------------------------------"
echo "   Waiting 20 seconds..."
sleep 20

# Check for messages
echo ""
echo "6️⃣ Checking for explanation learning messages..."
echo "------------------------------------------------"
RECENT_LOGS=$(kubectl logs -n "$NAMESPACE" "$FSM_POD" --tail=100 2>/dev/null | grep "EXPLANATION-LEARNING")

if [ -n "$RECENT_LOGS" ]; then
    echo "   ✅ SUCCESS! Found explanation learning messages:"
    echo ""
    echo "$RECENT_LOGS"
else
    echo "   ⚠️  No explanation learning messages found in recent logs"
    echo ""
    echo "   Checking Goal Manager logs for event publishing..."
    GOAL_MGR_POD=$(kubectl get pods -n "$NAMESPACE" -l app=goal-manager -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
    if [ -n "$GOAL_MGR_POD" ]; then
        echo "   Recent Goal Manager logs:"
        kubectl logs -n "$NAMESPACE" "$GOAL_MGR_POD" --tail=20 2>/dev/null | grep -i "achieved\|publishEvent\|goal.*$GOAL_ID" | tail -10
    fi
    
    echo ""
    echo "   Checking FSM logs for goal completion event..."
    kubectl logs -n "$NAMESPACE" "$FSM_POD" --tail=50 2>/dev/null | grep -i "goal.*$GOAL_ID\|goal.*achieved\|goal.*completed" | tail -10
fi

# Stop watcher
kill $WATCHER_PID 2>/dev/null || true

echo ""
echo "7️⃣ Checking Redis for learning data..."
echo "--------------------------------------"
KEYS=$(kubectl exec -n "$NAMESPACE" "$REDIS_POD" -- redis-cli KEYS "explanation_learning:*" 2>/dev/null)
if [ -n "$KEYS" ]; then
    echo "   ✅ Found explanation learning keys:"
    echo "$KEYS" | head -10
    echo ""
    echo "   Learning statistics:"
    STATS=$(kubectl exec -n "$NAMESPACE" "$REDIS_POD" -- redis-cli GET "explanation_learning:stats:General" 2>/dev/null)
    if [ -n "$STATS" ]; then
        echo "$STATS" | python3 -m json.tool 2>/dev/null || echo "$STATS"
    else
        echo "   (No stats yet)"
    fi
else
    echo "   ℹ️  No explanation learning keys found yet"
fi

echo ""
echo "✅ Test complete!"
echo ""
echo "💡 If no explanation learning messages appeared:"
echo "   1. Check if Goal Manager published the event: kubectl logs -n $NAMESPACE -l app=goal-manager | grep 'goal.achieved'"
echo "   2. Check if FSM received the event: kubectl logs -n $NAMESPACE $FSM_POD | grep 'Received.*goal'"
echo "   3. Verify NATS connectivity between Goal Manager and FSM"
echo ""

