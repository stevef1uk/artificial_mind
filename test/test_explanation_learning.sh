#!/bin/bash

# Quick test script for Explanation-Grounded Learning Feedback
# This creates a test goal, marks it as achieved, and watches for learning feedback

set -e

NAMESPACE="${K8S_NAMESPACE:-agi}"
GOAL_MGR_URL="${GOAL_MGR_URL:-http://localhost:8090}"

echo "🧪 Testing Explanation-Grounded Learning Feedback"
echo "================================================="
echo ""

# Get FSM pod name
FSM_POD=$(kubectl get pods -n "$NAMESPACE" -l app=fsm-server-rpi58 -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
if [ -z "$FSM_POD" ]; then
    FSM_POD=$(kubectl get pods -n "$NAMESPACE" -l app=fsm-server -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
fi

if [ -z "$FSM_POD" ]; then
    echo "❌ FSM pod not found. Is the FSM server running?"
    exit 1
fi

echo "✅ Found FSM pod: $FSM_POD"
echo ""

# Step 1: Create a test goal
echo "📝 Step 1: Creating test goal..."
echo "--------------------------------"

GOAL_PAYLOAD=$(cat <<EOF
{
  "description": "Test explanation learning - verify hypothesis accuracy",
  "priority": "high",
  "origin": "test:explanation_learning",
  "status": "active",
  "confidence": 0.75,
  "context": {
    "domain": "General",
    "test": true,
    "hypothesis_ids": []
  }
}
EOF
)

RESPONSE=$(curl -s -X POST "$GOAL_MGR_URL/goal" \
    -H "Content-Type: application/json" \
    -d "$GOAL_PAYLOAD")

if [ $? -ne 0 ]; then
    echo "❌ Failed to create goal. Is Goal Manager running at $GOAL_MGR_URL?"
    echo "   Try: kubectl port-forward -n $NAMESPACE svc/goal-manager 8090:8090"
    exit 1
fi

GOAL_ID=$(echo "$RESPONSE" | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)

if [ -z "$GOAL_ID" ]; then
    echo "⚠️  Could not extract goal ID from response:"
    echo "$RESPONSE"
    echo ""
    echo "Trying alternative method..."
    # Try to get goal ID from list
    sleep 2
    GOAL_LIST=$(curl -s "$GOAL_MGR_URL/goals/agent_1/active")
    GOAL_ID=$(echo "$GOAL_LIST" | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)
fi

if [ -z "$GOAL_ID" ]; then
    echo "❌ Could not create or find test goal"
    exit 1
fi

echo "✅ Created test goal: $GOAL_ID"
echo ""

# Step 2: Watch FSM logs in background for learning messages
echo "👀 Step 2: Starting log watcher (will show learning feedback messages)..."
echo "------------------------------------------------------------------------"
echo ""

# Start log watcher in background
(kubectl logs -f -n "$NAMESPACE" "$FSM_POD" 2>/dev/null | grep --line-buffered -E "EXPLANATION-LEARNING|goal.*achieved|goal.*failed" &
WATCHER_PID=$!

# Give it a moment to start
sleep 2

# Step 3: Mark goal as achieved
echo "✅ Step 3: Marking goal as achieved..."
echo "--------------------------------------"

ACHIEVE_PAYLOAD=$(cat <<EOF
{
  "result": {
    "success": true,
    "test": true,
    "executed_at": "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  }
}
EOF
)

curl -s -X POST "$GOAL_MGR_URL/goal/$GOAL_ID/achieve" \
    -H "Content-Type: application/json" \
    -d "$ACHIEVE_PAYLOAD" > /dev/null

if [ $? -eq 0 ]; then
    echo "✅ Goal marked as achieved"
else
    echo "⚠️  Failed to mark goal as achieved (may have been auto-achieved)"
fi

echo ""
echo "⏳ Waiting 10 seconds for learning feedback to process..."
sleep 10

# Step 4: Check Redis for learning data
echo ""
echo "📊 Step 4: Checking Redis for learning feedback data..."
echo "------------------------------------------------------"

REDIS_POD=$(kubectl get pods -n "$NAMESPACE" -l app=redis -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
if [ -z "$REDIS_POD" ]; then
    REDIS_POD=$(kubectl get pods -n "$NAMESPACE" | grep redis | head -1 | awk '{print $1}')
fi

if [ -n "$REDIS_POD" ]; then
    echo "✅ Found Redis pod: $REDIS_POD"
    echo ""
    echo "Checking for explanation learning keys..."
    kubectl exec -n "$NAMESPACE" "$REDIS_POD" -- redis-cli KEYS "explanation_learning:*" 2>/dev/null | head -10
    
    echo ""
    echo "Checking learning statistics..."
    kubectl exec -n "$NAMESPACE" "$REDIS_POD" -- redis-cli GET "explanation_learning:stats:General" 2>/dev/null || echo "No stats yet (may need more goals)"
    
    echo ""
    echo "Checking confidence scaling..."
    kubectl exec -n "$NAMESPACE" "$REDIS_POD" -- redis-cli GET "explanation_learning:confidence_scaling:General" 2>/dev/null || echo "No scaling data yet"
else
    echo "⚠️  Redis pod not found, skipping Redis checks"
fi

# Stop log watcher
kill $WATCHER_PID 2>/dev/null || true

echo ""
echo "✅ Test complete!"
echo ""
echo "📋 Summary:"
echo "  - Created test goal: $GOAL_ID"
echo "  - Marked goal as achieved"
echo "  - Checked for learning feedback"
echo ""
echo "🔍 To see more details:"
echo "  1. Watch FSM logs: kubectl logs -f -n $NAMESPACE $FSM_POD | grep EXPLANATION-LEARNING"
echo "  2. Check Redis: kubectl exec -it -n $NAMESPACE $REDIS_POD -- redis-cli"
echo "  3. Query learning stats: KEYS explanation_learning:*"
echo ""
echo "💡 Tip: Create more goals and complete them to see learning accumulate!"
echo ""

