#!/bin/bash

# Test script for coherence resolution feedback loop
# Tests that coherence goals are marked as resolved when Goal Manager tasks complete

AGENT_ID="${AGENT_ID:-agent_1}"
NAMESPACE="${K8S_NAMESPACE:-agi}"

echo "🧪 Testing Coherence Resolution Feedback Loop"
echo "=============================================="
echo ""

# Check if running in k8s or locally
USE_KUBECTL=false
if command -v kubectl &> /dev/null; then
  FSM_POD=$(kubectl get pods -n "$NAMESPACE" -l app=fsm-server-rpi58 -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
  REDIS_POD=$(kubectl get pods -n "$NAMESPACE" -l app=redis -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
  if [ -n "$FSM_POD" ] && [ -n "$REDIS_POD" ]; then
    USE_KUBECTL=true
    echo "✅ Detected Kubernetes environment"
  fi
fi

# Redis access function
redis_cmd() {
  if [ "$USE_KUBECTL" = true ] && [ -n "$REDIS_POD" ]; then
    kubectl exec -n "$NAMESPACE" "$REDIS_POD" -- redis-cli "$@" 2>/dev/null
  else
    redis-cli -h localhost -p 6379 "$@" 2>/dev/null
  fi
}

echo "📋 Step 1: Check Current State"
echo "-----------------------------"
echo ""

# Check for existing inconsistencies
INC_KEY="coherence:inconsistencies:${AGENT_ID}"
INC_COUNT=$(redis_cmd LLEN "$INC_KEY" 2>/dev/null || echo "0")
echo "   Current inconsistencies: ${INC_COUNT}"

# Check for coherence goals
GOAL_KEY="reasoning:curiosity_goals:system_coherence"
GOAL_COUNT=$(redis_cmd LLEN "$GOAL_KEY" 2>/dev/null || echo "0")
echo "   Current coherence goals: ${GOAL_COUNT}"
echo ""

# Show recent inconsistencies
if [ "$INC_COUNT" -gt 0 ]; then
  echo "   Recent inconsistencies:"
  redis_cmd LRANGE "$INC_KEY" 0 2 2>/dev/null | while read -r line; do
    if [ -n "$line" ] && [ "$line" != "(nil)" ]; then
      echo "$line" | jq -r '"     - [\(.severity)] \(.type): \(.description[:60])... (resolved: \(.resolved))"' 2>/dev/null || echo "     - $line"
    fi
  done
fi
echo ""

# Show recent coherence goals
if [ "$GOAL_COUNT" -gt 0 ]; then
  echo "   Recent coherence goals:"
  redis_cmd LRANGE "$GOAL_KEY" 0 2 2>/dev/null | while read -r line; do
    if [ -n "$line" ] && [ "$line" != "(nil)" ]; then
      echo "$line" | jq -r '"     - \(.id): \(.status) - \(.description[:50])..."' 2>/dev/null || echo "     - $line"
    fi
  done
fi
echo ""

echo "📋 Step 2: Create Test Inconsistency (if none exist)"
echo "---------------------------------------------------"
echo ""

# Create a test inconsistency if none exist
if [ "$INC_COUNT" = "0" ]; then
  echo "   Creating test inconsistency..."
  
  TEST_INC=$(cat <<EOF
{
  "id": "test_inconsistency_$(date +%s)",
  "type": "behavior_loop",
  "severity": "medium",
  "description": "Test inconsistency for feedback loop testing",
  "details": {"transition": "test->test", "count": 5},
  "detected_at": "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
  "resolved": false
}
EOF
)
  
  redis_cmd LPUSH "$INC_KEY" "$TEST_INC" > /dev/null 2>&1
  echo "   ✅ Created test inconsistency"
  echo ""
fi

echo "📋 Step 3: Check Goal Manager for Active Coherence Goals"
echo "--------------------------------------------------------"
echo ""

# Check Goal Manager for active goals with coherence context
if [ "$USE_KUBECTL" = true ]; then
  GOAL_MGR_POD=$(kubectl get pods -n "$NAMESPACE" -l app=goal-manager -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
  if [ -n "$GOAL_MGR_POD" ]; then
    echo "   Checking Goal Manager for coherence goals..."
    ACTIVE_GOALS=$(kubectl exec -n "$NAMESPACE" "$GOAL_MGR_POD" -- wget -qO- http://localhost:8090/goals/agent_1/active 2>/dev/null)
    if [ -n "$ACTIVE_GOALS" ]; then
      COHERENCE_COUNT=$(echo "$ACTIVE_GOALS" | jq '[.[] | select(.context.domain == "system_coherence")] | length' 2>/dev/null || echo "0")
      echo "   Active coherence goals in Goal Manager: $COHERENCE_COUNT"
      
      if [ "$COHERENCE_COUNT" -gt 0 ]; then
        echo "   Coherence goals:"
        echo "$ACTIVE_GOALS" | jq -r '.[] | select(.context.domain == "system_coherence") | "     - \(.id): \(.status) - \(.description[:50])..."' 2>/dev/null
      fi
    fi
  fi
else
  echo "   ⚠️  Goal Manager check skipped (local mode)"
fi
echo ""

echo "📋 Step 4: Monitor for Resolution Events"
echo "----------------------------------------"
echo ""

echo "   The coherence monitor subscribes to NATS events:"
echo "     - agi.goal.achieved"
echo "     - agi.goal.failed"
echo ""
echo "   When a coherence resolution goal completes, it should:"
echo "     1. Mark the inconsistency as resolved"
echo "     2. Update the curiosity goal status"
echo "     3. Clean up the mapping"
echo ""

if [ "$USE_KUBECTL" = true ] && [ -n "$FSM_POD" ]; then
  echo "   Watching FSM logs for resolution events (Ctrl+C to stop)..."
  echo "   Look for messages like:"
  echo "     '✅ [Coherence] Coherence resolution goal ... completed'"
  echo "     '✅ [Coherence] Marked inconsistency ... as resolved'"
  echo "     '✅ [Coherence] Updated curiosity goal ... status to ...'"
  echo ""
  echo "   Press Enter to start watching logs..."
  read
  
  kubectl logs -n "$NAMESPACE" "$FSM_POD" -f 2>/dev/null | grep --line-buffered -iE "\[Coherence\]|coherence.*resolv|goal.*completed" || true
else
  echo "   To test manually:"
  echo "     1. Wait for coherence monitor to detect inconsistencies (runs every 5 min)"
  echo "     2. Check that curiosity goals are created in Redis"
  echo "     3. Wait for Monitor Service to convert them to Goal Manager tasks"
  echo "     4. Wait for Goal Manager task to complete"
  echo "     5. Check that inconsistency is marked as resolved"
  echo "     6. Check that curiosity goal status is updated"
fi
echo ""

echo "📋 Step 5: Verify Resolution"
echo "----------------------------"
echo ""

echo "   Run this script again after a goal completes to verify:"
echo "     ./test_coherence_resolution_feedback.sh"
echo ""
echo "   Or check manually:"
echo ""
echo "   Check inconsistencies:"
echo "     redis-cli LRANGE coherence:inconsistencies:${AGENT_ID} 0 4 | jq"
echo ""
echo "   Check coherence goals:"
echo "     redis-cli LRANGE reasoning:curiosity_goals:system_coherence 0 4 | jq"
echo ""
echo "   Look for:"
echo "     - inconsistencies with resolved: true"
echo "     - curiosity goals with status: 'achieved' or 'failed'"
echo ""

echo "💡 Tips:"
echo "   - Coherence monitor runs every 5 minutes"
echo "   - Goals are converted to Goal Manager tasks every 30 seconds"
echo "   - Resolution happens automatically when Goal Manager tasks complete"
echo "   - Old goals (>7 days) are cleaned up automatically"
echo ""

