#!/bin/bash

# Check goal statuses and manually trigger explanation learning test

NAMESPACE="${K8S_NAMESPACE:-agi}"
GOAL_MGR_URL="${GOAL_MGR_URL:-http://localhost:8090}"
FSM_POD=$(kubectl get pods -n "$NAMESPACE" -l app=fsm-server-rpi58 -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)

echo "🔍 Checking Goals and Triggering Explanation Learning"
echo "====================================================="
echo ""

# Step 1: Check active goals
echo "1️⃣ Checking active goals..."
echo "---------------------------"
ACTIVE_GOALS=$(curl -s "$GOAL_MGR_URL/goals/agent_1/active" 2>/dev/null)

if [ -n "$ACTIVE_GOALS" ] && [ "$ACTIVE_GOALS" != "[]" ]; then
    GOAL_COUNT=$(echo "$ACTIVE_GOALS" | grep -o '"id"' | wc -l)
    echo "   Found $GOAL_COUNT active goal(s)"
    echo ""
    echo "   Sample goals:"
    echo "$ACTIVE_GOALS" | grep -o '"id":"[^"]*"\|"description":"[^"]*"\|"status":"[^"]*"' | head -9
    echo ""
    
    # Get first goal ID
    FIRST_GOAL_ID=$(echo "$ACTIVE_GOALS" | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)
    
    if [ -n "$FIRST_GOAL_ID" ]; then
        echo "   Using goal: $FIRST_GOAL_ID for testing"
    fi
else
    echo "   No active goals found"
    FIRST_GOAL_ID=""
fi
echo ""

# Step 2: Check for completed goals (achieved/failed)
echo "2️⃣ Checking for recently completed goals..."
echo "--------------------------------------------"
# Try to get goal history or check Redis
echo "   (Checking if any goals have completed recently)"
echo ""

# Step 3: Manually complete a goal to test
if [ -n "$FIRST_GOAL_ID" ]; then
    echo "3️⃣ Manually completing goal to trigger explanation learning..."
    echo "------------------------------------------------------------"
    
    echo "   Goal ID: $FIRST_GOAL_ID"
    echo "   Starting log watcher..."
    
    # Start watching logs
    kubectl logs -f -n "$NAMESPACE" "$FSM_POD" 2>/dev/null | grep --line-buffered "EXPLANATION-LEARNING" &
    WATCHER_PID=$!
    sleep 2
    
    echo "   Marking goal as achieved..."
    ACHIEVE_PAYLOAD='{
      "result": {
        "success": true,
        "test": true,
        "executed_at": "'$(date -u +%Y-%m-%dT%H:%M:%SZ)'",
        "manual_test": true
      }
    }'
    
    RESPONSE=$(curl -s -X POST "$GOAL_MGR_URL/goal/$FIRST_GOAL_ID/achieve" \
        -H "Content-Type: application/json" \
        -d "$ACHIEVE_PAYLOAD")
    
    if [ $? -eq 0 ]; then
        echo "   ✅ Goal marked as achieved"
        echo "   ⏳ Waiting 15 seconds for explanation learning..."
        sleep 15
        
        # Check if we saw any messages
        echo ""
        echo "   Checking recent logs for explanation learning messages..."
        RECENT_LOGS=$(kubectl logs -n "$NAMESPACE" "$FSM_POD" --tail=50 2>/dev/null | grep "EXPLANATION-LEARNING")
        
        if [ -n "$RECENT_LOGS" ]; then
            echo "   ✅ Found explanation learning messages:"
            echo "$RECENT_LOGS"
        else
            echo "   ⚠️  No explanation learning messages found in recent logs"
            echo "   💡 This might mean:"
            echo "      - Goal completion event wasn't published"
            echo "      - FSM didn't receive the event"
            echo "      - Check if NATS is working"
        fi
    else
        echo "   ❌ Failed to mark goal as achieved"
    fi
    
    # Stop watcher
    kill $WATCHER_PID 2>/dev/null || true
else
    echo "3️⃣ Creating a new test goal to complete..."
    echo "-------------------------------------------"
    
    GOAL_PAYLOAD='{
      "description": "Quick test goal for explanation learning",
      "priority": "high",
      "origin": "test:manual",
      "status": "active",
      "confidence": 0.8,
      "context": {
        "domain": "General",
        "test": true
      }
    }'
    
    RESPONSE=$(curl -s -X POST "$GOAL_MGR_URL/goal" \
        -H "Content-Type: application/json" \
        -d "$GOAL_PAYLOAD")
    
    NEW_GOAL_ID=$(echo "$RESPONSE" | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)
    
    if [ -n "$NEW_GOAL_ID" ]; then
        echo "   ✅ Created goal: $NEW_GOAL_ID"
        echo "   Marking as achieved immediately..."
        
        # Start watching
        kubectl logs -f -n "$NAMESPACE" "$FSM_POD" 2>/dev/null | grep --line-buffered "EXPLANATION-LEARNING" &
        WATCHER_PID=$!
        sleep 2
        
        ACHIEVE_PAYLOAD='{
          "result": {
            "success": true,
            "test": true,
            "executed_at": "'$(date -u +%Y-%m-%dT%H:%M:%SZ)'"
          }
        }'
        
        curl -s -X POST "$GOAL_MGR_URL/goal/$NEW_GOAL_ID/achieve" \
            -H "Content-Type: application/json" \
            -d "$ACHIEVE_PAYLOAD" > /dev/null
        
        echo "   ⏳ Waiting 15 seconds..."
        sleep 15
        
        # Check logs
        RECENT_LOGS=$(kubectl logs -n "$NAMESPACE" "$FSM_POD" --tail=50 2>/dev/null | grep "EXPLANATION-LEARNING")
        if [ -n "$RECENT_LOGS" ]; then
            echo "   ✅ Found explanation learning messages:"
            echo "$RECENT_LOGS"
        else
            echo "   ⚠️  No messages found"
        fi
        
        kill $WATCHER_PID 2>/dev/null || true
    fi
fi

echo ""
echo "4️⃣ Checking NATS events..."
echo "--------------------------"
echo "   Checking if goal.achieved events are being published..."
echo "   (This requires NATS monitoring - checking logs for event publishing)"

# Check Goal Manager logs for event publishing
GOAL_MGR_POD=$(kubectl get pods -n "$NAMESPACE" -l app=goal-manager -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
if [ -n "$GOAL_MGR_POD" ]; then
    echo "   Goal Manager pod: $GOAL_MGR_POD"
    echo "   Recent goal.achieved events:"
    kubectl logs -n "$NAMESPACE" "$GOAL_MGR_POD" --tail=20 2>/dev/null | grep -i "goal.achieved\|publishEvent.*achieved" | tail -5 || echo "   (No recent events found)"
fi

echo ""
echo "5️⃣ Checking Redis for explanation learning data..."
echo "--------------------------------------------------"
REDIS_POD=$(kubectl get pods -n "$NAMESPACE" -l app=redis -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
if [ -z "$REDIS_POD" ]; then
    REDIS_POD=$(kubectl get pods -n "$NAMESPACE" | grep redis | head -1 | awk '{print $1}')
fi

if [ -n "$REDIS_POD" ]; then
    KEYS=$(kubectl exec -n "$NAMESPACE" "$REDIS_POD" -- redis-cli KEYS "explanation_learning:*" 2>/dev/null)
    if [ -n "$KEYS" ]; then
        echo "   ✅ Found explanation learning keys:"
        echo "$KEYS" | head -10
    else
        echo "   ℹ️  No explanation learning keys found"
    fi
fi

echo ""
echo "✅ Check complete!"
echo ""
echo "💡 If goals aren't completing automatically:"
echo "   1. Goals may be waiting for workflows to finish"
echo "   2. Check workflow status: kubectl get pods -n $NAMESPACE | grep workflow"
echo "   3. Goals might need manual completion for testing"
echo "   4. Check Goal Manager logs for event publishing issues"
echo ""

