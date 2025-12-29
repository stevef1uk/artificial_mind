#!/bin/bash

# Find goals from all sources and manually complete one to test explanation learning

NAMESPACE="${K8S_NAMESPACE:-agi}"
GOAL_MGR_URL="${GOAL_MGR_URL:-http://localhost:8090}"
FSM_POD=$(kubectl get pods -n "$NAMESPACE" -l app=fsm-server-rpi58 -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)

echo "🔍 Finding Goals from All Sources"
echo "=================================="
echo ""

# Check 1: Goal Manager active goals
echo "1️⃣ Goal Manager Active Goals:"
echo "------------------------------"
GM_GOALS=$(curl -s "$GOAL_MGR_URL/goals/agent_1/active" 2>/dev/null)
if [ -n "$GM_GOALS" ] && [ "$GM_GOALS" != "[]" ] && [ "$GM_GOALS" != "null" ]; then
    GM_COUNT=$(echo "$GM_GOALS" | grep -o '"id"' | wc -l)
    echo "   ✅ Found $GM_COUNT active goal(s) in Goal Manager"
    echo "$GM_GOALS" | grep -o '"id":"[^"]*"\|"description":"[^"]*"' | head -6
    FIRST_GM_GOAL=$(echo "$GM_GOALS" | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)
else
    echo "   ℹ️  No active goals in Goal Manager"
    FIRST_GM_GOAL=""
fi
echo ""

# Check 2: Curiosity goals in Redis
echo "2️⃣ Curiosity Goals (Redis):"
echo "----------------------------"
REDIS_POD=$(kubectl get pods -n "$NAMESPACE" -l app=redis -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
if [ -z "$REDIS_POD" ]; then
    REDIS_POD=$(kubectl get pods -n "$NAMESPACE" | grep redis | head -1 | awk '{print $1}')
fi

if [ -n "$REDIS_POD" ]; then
    CURIOSITY_KEYS=$(kubectl exec -n "$NAMESPACE" "$REDIS_POD" -- redis-cli KEYS "reasoning:curiosity_goals:*" 2>/dev/null)
    if [ -n "$CURIOSITY_KEYS" ]; then
        echo "   ✅ Found curiosity goal keys:"
        echo "$CURIOSITY_KEYS" | head -5
        # Get count from one domain
        FIRST_KEY=$(echo "$CURIOSITY_KEYS" | head -1)
        if [ -n "$FIRST_KEY" ]; then
            COUNT=$(kubectl exec -n "$NAMESPACE" "$REDIS_POD" -- redis-cli LLEN "$FIRST_KEY" 2>/dev/null)
            echo "   Count in $FIRST_KEY: $COUNT"
        fi
    else
        echo "   ℹ️  No curiosity goals found"
    fi
else
    echo "   ⚠️  Redis pod not found"
fi
echo ""

# Check 3: Check Redis for all goal-related keys
echo "3️⃣ All Goal-Related Keys in Redis:"
echo "-----------------------------------"
if [ -n "$REDIS_POD" ]; then
    ALL_GOAL_KEYS=$(kubectl exec -n "$NAMESPACE" "$REDIS_POD" -- redis-cli KEYS "*goal*" 2>/dev/null | head -20)
    if [ -n "$ALL_GOAL_KEYS" ]; then
        echo "   Found goal-related keys:"
        echo "$ALL_GOAL_KEYS"
        echo ""
        echo "   Checking active goals set..."
        ACTIVE_SET=$(kubectl exec -n "$NAMESPACE" "$REDIS_POD" -- redis-cli SMEMBERS "goals:agent_1:active" 2>/dev/null)
        if [ -n "$ACTIVE_SET" ]; then
            ACTIVE_COUNT=$(echo "$ACTIVE_SET" | wc -l)
            echo "   ✅ Found $ACTIVE_COUNT goal IDs in active set"
            echo "   Sample IDs:"
            echo "$ACTIVE_SET" | head -5
        else
            echo "   ℹ️  Active goals set is empty"
        fi
    fi
else
    echo "   ⚠️  Redis pod not found"
fi
echo ""

# Step 4: Manually complete a goal to test
echo "4️⃣ Testing Explanation Learning..."
echo "----------------------------------"

# Try to use Goal Manager goal first, or create a new one
GOAL_TO_COMPLETE=""

if [ -n "$FIRST_GM_GOAL" ]; then
    GOAL_TO_COMPLETE="$FIRST_GM_GOAL"
    echo "   Using Goal Manager goal: $GOAL_TO_COMPLETE"
else
    echo "   Creating a new test goal..."
    GOAL_PAYLOAD='{
      "description": "Test explanation learning - manual completion",
      "priority": "high",
      "origin": "test:manual_learning",
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
    
    GOAL_TO_COMPLETE=$(echo "$RESPONSE" | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)
    
    if [ -n "$GOAL_TO_COMPLETE" ]; then
        echo "   ✅ Created test goal: $GOAL_TO_COMPLETE"
        sleep 2
    else
        echo "   ❌ Failed to create goal"
        exit 1
    fi
fi

echo ""
echo "   Starting log watcher for explanation learning messages..."
kubectl logs -f -n "$NAMESPACE" "$FSM_POD" 2>/dev/null | grep --line-buffered "EXPLANATION-LEARNING" &
WATCHER_PID=$!
sleep 2

echo "   Marking goal $GOAL_TO_COMPLETE as achieved..."
ACHIEVE_PAYLOAD='{
  "result": {
    "success": true,
    "test": true,
    "executed_at": "'$(date -u +%Y-%m-%dT%H:%M:%SZ)'",
    "manual_test": true
  }
}'

RESPONSE=$(curl -s -X POST "$GOAL_MGR_URL/goal/$GOAL_TO_COMPLETE/achieve" \
    -H "Content-Type: application/json" \
    -d "$ACHIEVE_PAYLOAD")

if echo "$RESPONSE" | grep -q '"status":"achieved"'; then
    echo "   ✅ Goal marked as achieved successfully"
else
    echo "   ⚠️  Response: $RESPONSE"
fi

echo ""
echo "   ⏳ Waiting 20 seconds for explanation learning to process..."
sleep 20

# Check for messages
echo ""
echo "   Checking recent logs for explanation learning messages..."
RECENT_LOGS=$(kubectl logs -n "$NAMESPACE" "$FSM_POD" --tail=100 2>/dev/null | grep "EXPLANATION-LEARNING")

if [ -n "$RECENT_LOGS" ]; then
    echo "   ✅ SUCCESS! Found explanation learning messages:"
    echo "$RECENT_LOGS"
else
    echo "   ⚠️  No explanation learning messages found"
    echo ""
    echo "   Checking if goal.achieved event was published..."
    GOAL_MGR_POD=$(kubectl get pods -n "$NAMESPACE" -l app=goal-manager -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
    if [ -n "$GOAL_MGR_POD" ]; then
        echo "   Recent Goal Manager logs:"
        kubectl logs -n "$NAMESPACE" "$GOAL_MGR_POD" --tail=10 2>/dev/null | grep -i "achieved\|publishEvent" | tail -5
    fi
fi

# Stop watcher
kill $WATCHER_PID 2>/dev/null || true

echo ""
echo "5️⃣ Checking Redis for learning data..."
echo "--------------------------------------"
if [ -n "$REDIS_POD" ]; then
    KEYS=$(kubectl exec -n "$NAMESPACE" "$REDIS_POD" -- redis-cli KEYS "explanation_learning:*" 2>/dev/null)
    if [ -n "$KEYS" ]; then
        echo "   ✅ Found explanation learning keys:"
        echo "$KEYS" | head -10
        echo ""
        echo "   Learning statistics:"
        kubectl exec -n "$NAMESPACE" "$REDIS_POD" -- redis-cli GET "explanation_learning:stats:General" 2>/dev/null || echo "   (No stats yet)"
    else
        echo "   ℹ️  No explanation learning keys found yet"
    fi
fi

echo ""
echo "✅ Test complete!"
echo ""
echo "💡 Summary:"
echo "   - Goal Manager shows goals with status='active'"
echo "   - UI shows goals from multiple sources (Goal Manager + Curiosity + Self-Model)"
echo "   - Explanation learning only triggers when goals transition to 'achieved' or 'failed'"
echo "   - If goals are stuck in 'active', they won't trigger learning"
echo ""

