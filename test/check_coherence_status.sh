#!/bin/bash

# Quick script to check coherence monitor status locally

AGENT_ID="${AGENT_ID:-agent_1}"

echo "🔍 Checking Coherence Monitor Status (Local)"
echo "============================================="
echo ""

# Check if FSM is running
if ! pgrep -f "fsm-server" > /dev/null; then
  echo "❌ FSM server is not running"
  echo "   Start it with: cd fsm && ./fsm-server -config ../config/artificial_mind.yaml"
  exit 1
fi
echo "✅ FSM server is running"
echo ""

# Check Redis connection
if ! redis-cli -h localhost -p 6379 PING > /dev/null 2>&1; then
  echo "❌ Cannot connect to Redis"
  exit 1
fi
echo "✅ Redis connection: OK"
echo ""

# Check for inconsistencies
echo "📊 Inconsistencies:"
INC_COUNT=$(redis-cli -h localhost -p 6379 LLEN "coherence:inconsistencies:${AGENT_ID}" 2>/dev/null)
if [ -z "$INC_COUNT" ] || [ "$INC_COUNT" = "0" ]; then
  echo "   ℹ️  None detected yet (monitor runs every 5 minutes)"
else
  echo "   ✅ Found $INC_COUNT inconsistency(ies)"
  echo ""
  echo "   Recent:"
  redis-cli -h localhost -p 6379 LRANGE "coherence:inconsistencies:${AGENT_ID}" 0 2 2>/dev/null | while read -r line; do
    if [ -n "$line" ] && [ "$line" != "(nil)" ]; then
      echo "$line" | jq -r '"   - [\(.severity)] \(.type): \(.description)"' 2>/dev/null || echo "   - $line"
    fi
  done
fi
echo ""

# Check for reflection tasks
echo "📝 Reflection Tasks:"
TASK_COUNT=$(redis-cli -h localhost -p 6379 LLEN "coherence:reflection_tasks:${AGENT_ID}" 2>/dev/null)
if [ -z "$TASK_COUNT" ] || [ "$TASK_COUNT" = "0" ]; then
  echo "   ℹ️  None yet"
else
  echo "   ✅ Found $TASK_COUNT task(s)"
fi
echo ""

# Check for curiosity goals (resolution tasks)
echo "🎯 Coherence Resolution Goals:"
GOAL_COUNT=$(redis-cli -h localhost -p 6379 LLEN "reasoning:curiosity_goals:system_coherence" 2>/dev/null)
if [ -z "$GOAL_COUNT" ] || [ "$GOAL_COUNT" = "0" ]; then
  echo "   ℹ️  None yet"
else
  echo "   ✅ Found $GOAL_COUNT goal(s) for resolution"
fi
echo ""

# Check test scenarios
echo "🧪 Test Scenarios Status:"
echo "   Active goals: $(redis-cli -h localhost -p 6379 SCARD "goals:${AGENT_ID}:active" 2>/dev/null)"
echo "   Activity log entries: $(redis-cli -h localhost -p 6379 LLEN "fsm:${AGENT_ID}:activity_log" 2>/dev/null)"
echo ""

# Check if monitor has run (look for log pattern)
echo "📋 Monitor Activity:"
echo "   The coherence monitor runs every 5 minutes automatically"
echo "   To see if it's running, check FSM logs for:"
echo "     grep -i 'coherence' <fsm-log-file>"
echo ""
echo "   Expected log messages:"
echo "     - '🔍 [Coherence] Coherence monitoring loop started'"
echo "     - '🔍 [Coherence] Starting cross-system coherence check'"
echo "     - '⚠️ [Coherence] Detected X inconsistencies'"
echo "     - '✅ [Coherence] No inconsistencies detected'"
echo ""

echo "💡 Tips:"
echo "   1. Wait 5 minutes after creating test scenarios"
echo "   2. Or check when FSM started (monitor runs 5 min after start)"
echo "   3. Re-run this script to see updates"
echo ""

