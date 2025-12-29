#!/bin/bash

# Quick script to check coherence monitor status locally

AGENT_ID="${AGENT_ID:-agent_1}"

echo "🔍 Checking Coherence Monitor Status (Local)"
echo "============================================="
echo ""

# Check if FSM is running (local or k8s)
FSM_RUNNING=false
USE_KUBECTL=false
NAMESPACE="${K8S_NAMESPACE:-agi}"

# Check for Kubernetes environment
if command -v kubectl &> /dev/null; then
  FSM_POD=$(kubectl get pods -n "$NAMESPACE" -l app=fsm-server-rpi58 -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
  if [ -n "$FSM_POD" ]; then
    POD_STATUS=$(kubectl get pod -n "$NAMESPACE" "$FSM_POD" -o jsonpath='{.status.phase}' 2>/dev/null)
    if [ "$POD_STATUS" = "Running" ]; then
      FSM_RUNNING=true
      USE_KUBECTL=true
      echo "✅ FSM server is running in Kubernetes (pod: $FSM_POD)"
    fi
  fi
fi

# Check for local process if not in k8s
if [ "$FSM_RUNNING" = false ]; then
  if pgrep -f "fsm-server" > /dev/null; then
    FSM_RUNNING=true
    echo "✅ FSM server is running (local process)"
  fi
fi

if [ "$FSM_RUNNING" = false ]; then
  echo "❌ FSM server is not running"
  echo "   Start it with: cd fsm && ./fsm-server -config ../config/artificial_mind.yaml"
  echo "   Or check Kubernetes: kubectl get pods -n $NAMESPACE -l app=fsm-server-rpi58"
  exit 1
fi
echo ""

# Check Redis connection (local or k8s)
REDIS_OK=false
if [ "$USE_KUBECTL" = true ]; then
  # Check Redis in k8s
  REDIS_POD=$(kubectl get pods -n "$NAMESPACE" -l app=redis -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
  if [ -n "$REDIS_POD" ]; then
    if kubectl exec -n "$NAMESPACE" "$REDIS_POD" -- redis-cli PING > /dev/null 2>&1; then
      REDIS_OK=true
      echo "✅ Redis connection: OK (k8s pod: $REDIS_POD)"
    fi
  fi
else
  # Check local Redis
  if redis-cli -h localhost -p 6379 PING > /dev/null 2>&1; then
    REDIS_OK=true
    echo "✅ Redis connection: OK (local)"
  fi
fi

if [ "$REDIS_OK" = false ]; then
  echo "❌ Cannot connect to Redis"
  if [ "$USE_KUBECTL" = true ]; then
    echo "   Check: kubectl get pods -n $NAMESPACE -l app=redis"
  else
    echo "   Make sure Redis is running locally"
  fi
  exit 1
fi
echo ""

# Redis access function
redis_cmd() {
  if [ "$USE_KUBECTL" = true ] && [ -n "$REDIS_POD" ]; then
    kubectl exec -n "$NAMESPACE" "$REDIS_POD" -- redis-cli "$@" 2>/dev/null
  else
    redis-cli -h localhost -p 6379 "$@" 2>/dev/null
  fi
}

# Check for inconsistencies
echo "📊 Inconsistencies:"
INC_COUNT=$(redis_cmd LLEN "coherence:inconsistencies:${AGENT_ID}" 2>/dev/null)
if [ -z "$INC_COUNT" ] || [ "$INC_COUNT" = "0" ]; then
  echo "   ℹ️  None detected yet (monitor runs every 5 minutes)"
else
  echo "   ✅ Found $INC_COUNT inconsistency(ies)"
  echo ""
  echo "   Recent:"
  redis_cmd LRANGE "coherence:inconsistencies:${AGENT_ID}" 0 2 2>/dev/null | while read -r line; do
    if [ -n "$line" ] && [ "$line" != "(nil)" ]; then
      echo "$line" | jq -r '"   - [\(.severity)] \(.type): \(.description)"' 2>/dev/null || echo "   - $line"
    fi
  done
fi
echo ""

# Check for reflection tasks
echo "📝 Reflection Tasks:"
TASK_COUNT=$(redis_cmd LLEN "coherence:reflection_tasks:${AGENT_ID}" 2>/dev/null)
if [ -z "$TASK_COUNT" ] || [ "$TASK_COUNT" = "0" ]; then
  echo "   ℹ️  None yet"
else
  echo "   ✅ Found $TASK_COUNT task(s)"
fi
echo ""

# Check for curiosity goals (resolution tasks)
echo "🎯 Coherence Resolution Goals:"
GOAL_COUNT=$(redis_cmd LLEN "reasoning:curiosity_goals:system_coherence" 2>/dev/null)
if [ -z "$GOAL_COUNT" ] || [ "$GOAL_COUNT" = "0" ]; then
  echo "   ℹ️  None yet"
else
  echo "   ✅ Found $GOAL_COUNT goal(s) for resolution"
fi
echo ""

# Check test scenarios
echo "🧪 Test Scenarios Status:"
echo "   Active goals: $(redis_cmd SCARD "goals:${AGENT_ID}:active" 2>/dev/null)"
echo "   Activity log entries: $(redis_cmd LLEN "fsm:${AGENT_ID}:activity_log" 2>/dev/null)"
echo ""

# Check if monitor has run (look for log pattern)
echo "📋 Monitor Activity:"
echo "   The coherence monitor runs every 5 minutes automatically"
if [ "$USE_KUBECTL" = true ] && [ -n "$FSM_POD" ]; then
  echo "   Checking FSM pod logs for coherence activity..."
  COHERENCE_LOGS=$(kubectl logs -n "$NAMESPACE" "$FSM_POD" --tail=200 2>/dev/null | grep -i "\[Coherence\]" | tail -5)
  if [ -n "$COHERENCE_LOGS" ]; then
    echo "   Recent coherence activity:"
    echo "$COHERENCE_LOGS" | sed 's/^/     /'
  else
    echo "   ℹ️  No recent coherence activity in logs"
    echo "   To check manually: kubectl logs -n $NAMESPACE $FSM_POD | grep -i coherence"
  fi
else
  echo "   To see if it's running, check FSM logs for:"
  echo "     grep -i 'coherence' <fsm-log-file>"
fi
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

