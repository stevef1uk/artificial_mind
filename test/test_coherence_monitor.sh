#!/bin/bash

# Test script for Cross-System Consistency Checking (Coherence Monitor)
# This tests the new coherence monitoring functionality

REDIS_HOST="${REDIS_HOST:-localhost}"
REDIS_PORT="${REDIS_PORT:-6379}"
FSM_URL="${FSM_URL:-http://localhost:8083}"
GOAL_MGR_URL="${GOAL_MGR_URL:-http://localhost:8084}"
AGENT_ID="${AGENT_ID:-agent_1}"

echo "🧩 Testing Cross-System Consistency Checking (Coherence Monitor)"
echo "================================================================="
echo ""

# Detect if we're in a k3s/k8s environment
USE_KUBECTL=false
REDIS_POD=""
NAMESPACE="${K8S_NAMESPACE:-agi}"
if command -v kubectl &> /dev/null; then
  REDIS_POD=$(kubectl get pods -n "$NAMESPACE" -l app=redis -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
  if [ -n "$REDIS_POD" ]; then
    USE_KUBECTL=true
    echo "🔍 Detected k3s/k8s environment"
    echo "   Using Redis pod: $REDIS_POD"
  fi
fi

# Redis access function (works with both local and k3s)
redis_cmd() {
  if [ "$USE_KUBECTL" = true ]; then
    kubectl exec -n "$NAMESPACE" "$REDIS_POD" -- redis-cli "$@" 2>/dev/null
  else
    redis-cli -h "$REDIS_HOST" -p "$REDIS_PORT" "$@" 2>/dev/null
  fi
}

# Check if Redis is accessible
if ! redis_cmd PING > /dev/null 2>&1; then
  if [ "$USE_KUBECTL" = true ]; then
    echo "❌ Cannot connect to Redis pod: $REDIS_POD"
    echo "   Check if pod is running: kubectl get pods -n $NAMESPACE -l app=redis"
  else
    echo "❌ Cannot connect to Redis at $REDIS_HOST:$REDIS_PORT"
    echo "   Make sure Redis is running and accessible"
  fi
  exit 1
fi
echo "✅ Redis connection: OK"
echo ""

# Function to check for inconsistencies in Redis
check_inconsistencies() {
  echo "📊 Checking for detected inconsistencies..."
  echo ""
  
  # Check all inconsistencies
  key="coherence:inconsistencies:${AGENT_ID}"
  count=$(redis_cmd LLEN "$key" 2>/dev/null | tr -d '\r\n')
  
  if [ -z "$count" ] || [ "$count" = "0" ]; then
    echo "   ℹ️  No inconsistencies found yet (this is normal if the monitor hasn't run)"
    echo "   The coherence monitor runs every 5 minutes automatically"
  else
    echo "   ✅ Found $count inconsistency(ies)"
    echo ""
    echo "   Recent inconsistencies:"
    redis_cmd LRANGE "$key" 0 4 2>/dev/null | while read -r line; do
      if [ -n "$line" ] && [ "$line" != "(nil)" ]; then
        echo "$line" | jq -r '"   - Type: \(.type) | Severity: \(.severity) | \(.description)"' 2>/dev/null || echo "   - $line"
      fi
    done
  fi
  echo ""
}

# Function to check for reflection tasks
check_reflection_tasks() {
  echo "📝 Checking for self-reflection tasks..."
  echo ""
  
  key="coherence:reflection_tasks:${AGENT_ID}"
  count=$(redis_cmd LLEN "$key" 2>/dev/null | tr -d '\r\n')
  
  if [ -z "$count" ] || [ "$count" = "0" ]; then
    echo "   ℹ️  No reflection tasks found yet"
  else
    echo "   ✅ Found $count reflection task(s)"
    echo ""
    echo "   Recent tasks:"
    redis_cmd LRANGE "$key" 0 4 2>/dev/null | while read -r line; do
      if [ -n "$line" ] && [ "$line" != "(nil)" ]; then
        echo "$line" | jq -r '"   - Priority: \(.priority) | Status: \(.status) | \(.description)"' 2>/dev/null || echo "   - $line"
      fi
    done
  fi
  echo ""
}

# Function to check coherence monitor logs
check_logs() {
  echo "📋 Checking FSM logs for coherence monitor activity..."
  echo ""
  
  if [ "$USE_KUBECTL" = true ]; then
    FSM_POD=$(kubectl get pods -n "$NAMESPACE" --no-headers 2>/dev/null | grep -E "fsm.*Running" | awk '{print $1}' | head -1)
    if [ -n "$FSM_POD" ]; then
      echo "   Recent coherence monitor logs:"
      kubectl logs -n "$NAMESPACE" "$FSM_POD" --tail=100 2>/dev/null | grep -E "\[Coherence\]|coherence" | tail -10
    else
      echo "   ⚠️  FSM pod not found"
    fi
  else
    echo "   ℹ️  For local testing, check FSM server logs for [Coherence] messages"
    echo "   The coherence monitor logs:"
    echo "     - 🔍 [Coherence] Starting cross-system coherence check"
    echo "     - ⚠️  [Coherence] Detected X inconsistencies"
    echo "     - ✅ [Coherence] No inconsistencies detected"
  fi
  echo ""
}

# Function to create test scenarios
create_test_scenarios() {
  echo "🧪 Creating test scenarios to trigger inconsistencies..."
  echo ""
  
  # Scenario 1: Create conflicting goals
  echo "1. Creating conflicting goals in Goal Manager..."
  if command -v curl &> /dev/null; then
    # Goal 1: Increase something
    curl -s -X POST "$GOAL_MGR_URL/goals" \
      -H "Content-Type: application/json" \
      -d "{
        \"agent_id\": \"$AGENT_ID\",
        \"description\": \"Increase API response time\",
        \"priority\": \"high\",
        \"status\": \"active\"
      }" > /dev/null
    
    # Goal 2: Decrease the same thing (conflict!)
    curl -s -X POST "$GOAL_MGR_URL/goals" \
      -H "Content-Type: application/json" \
      -d "{
        \"agent_id\": \"$AGENT_ID\",
        \"description\": \"Decrease API response time\",
        \"priority\": \"high\",
        \"status\": \"active\"
      }" > /dev/null
    
    echo "   ✅ Created conflicting goals (increase vs decrease API response time)"
  else
    echo "   ⚠️  curl not found, skipping goal creation"
  fi
  echo ""
  
  # Scenario 2: Create activity log entries that might trigger behavior loop detection
  echo "2. Creating activity log entries for behavior loop detection..."
  activity_key="fsm:${AGENT_ID}:activity_log"
  
  # Create some repetitive state transitions
  for i in {1..6}; do
    activity=$(cat <<EOF
{
  "timestamp": "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
  "message": "State transition",
  "state": "reason",
  "category": "state_change"
}
EOF
)
    redis_cmd LPUSH "$activity_key" "$activity" > /dev/null 2>&1
  done
  
  echo "   ✅ Created activity log entries"
  echo ""
  
  # Scenario 3: Create a goal that's been active for a long time (goal drift)
  echo "3. Creating a stale goal for goal drift detection..."
  goal_key="goal:test_stale_goal_$(date +%s)"
  stale_goal=$(cat <<EOF
{
  "id": "test_stale_goal_$(date +%s)",
  "agent_id": "$AGENT_ID",
  "description": "Test goal for drift detection",
  "status": "active",
  "created_at": "$(date -u -v-25H +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || date -u -d '25 hours ago' +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || echo "$(date -u +%Y-%m-%dT%H:%M:%SZ)")",
  "updated_at": "$(date -u -v-25H +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || date -u -d '25 hours ago' +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || echo "$(date -u +%Y-%m-%dT%H:%M:%SZ)")"
}
EOF
)
  redis_cmd SET "$goal_key" "$stale_goal" > /dev/null 2>&1
  redis_cmd SADD "goals:${AGENT_ID}:active" "test_stale_goal_$(date +%s)" > /dev/null 2>&1
  
  echo "   ✅ Created stale goal (25 hours old)"
  echo ""
  
  echo "✅ Test scenarios created!"
  echo "   ⏳ Wait up to 5 minutes for the coherence monitor to run, or trigger manually"
  echo ""
}

# Function to manually trigger coherence check (if API exists)
trigger_coherence_check() {
  echo "🔄 Attempting to trigger coherence check..."
  echo ""
  
  # Note: The coherence monitor runs automatically every 5 minutes
  # There's no direct API to trigger it, but we can wait for the next cycle
  echo "   ℹ️  The coherence monitor runs automatically every 5 minutes"
  echo "   To trigger it manually, you would need to:"
  echo "     1. Wait for the next 5-minute interval"
  echo "     2. Or modify the code to add a manual trigger endpoint"
  echo ""
  echo "   For now, let's check if it has run recently..."
  echo ""
}

# Main test flow
echo "Test 1: Check current state"
echo "---------------------------"
check_inconsistencies
check_reflection_tasks
check_logs

echo ""
echo "Test 2: Create test scenarios"
echo "------------------------------"
read -p "Create test scenarios? (y/n) " -n 1 -r
echo ""
if [[ $REPLY =~ ^[Yy]$ ]]; then
  create_test_scenarios
fi

echo ""
echo "Test 3: Wait and re-check"
echo "-------------------------"
echo "The coherence monitor runs every 5 minutes."
echo "You can:"
echo "  1. Wait 5 minutes and run this script again"
echo "  2. Check Redis directly:"
echo "     redis-cli LRANGE coherence:inconsistencies:${AGENT_ID} 0 10"
echo "     redis-cli LRANGE coherence:reflection_tasks:${AGENT_ID} 0 10"
echo "  3. Watch FSM logs for [Coherence] messages"
echo ""

echo "Test 4: Manual Redis inspection"
echo "-------------------------------"
echo "Useful Redis keys to check:"
echo "  - coherence:inconsistencies:${AGENT_ID}"
echo "  - coherence:inconsistencies:${AGENT_ID}:belief_contradiction"
echo "  - coherence:inconsistencies:${AGENT_ID}:policy_conflict"
echo "  - coherence:inconsistencies:${AGENT_ID}:goal_drift"
echo "  - coherence:inconsistencies:${AGENT_ID}:behavior_loop"
echo "  - coherence:reflection_tasks:${AGENT_ID}"
echo "  - reasoning:curiosity_goals:system_coherence"
echo ""

echo "Example Redis commands:"
echo "  redis-cli LLEN coherence:inconsistencies:${AGENT_ID}"
echo "  redis-cli LRANGE coherence:inconsistencies:${AGENT_ID} 0 0 | jq"
echo "  redis-cli LRANGE coherence:reflection_tasks:${AGENT_ID} 0 0 | jq"
echo ""

echo "✅ Test script complete!"
echo ""
echo "Next steps:"
echo "  1. Wait 5 minutes for the coherence monitor to run"
echo "  2. Re-run this script to see detected inconsistencies"
echo "  3. Check FSM logs for [Coherence] messages"
echo "  4. Verify that curiosity goals are created in reasoning:curiosity_goals:system_coherence"
echo ""

