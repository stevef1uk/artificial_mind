#!/bin/bash

# Check the status of coherence resolution feedback loop

AGENT_ID="${AGENT_ID:-agent_1}"
NAMESPACE="${K8S_NAMESPACE:-agi}"

echo "🔍 Coherence Resolution Status Check"
echo "===================================="
echo ""

# Check if running in k8s or locally
USE_KUBECTL=false
if command -v kubectl &> /dev/null; then
  REDIS_POD=$(kubectl get pods -n "$NAMESPACE" -l app=redis -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
  GOAL_MGR_POD=$(kubectl get pods -n "$NAMESPACE" -l app=goal-manager -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
  if [ -n "$REDIS_POD" ] && [ -n "$GOAL_MGR_POD" ]; then
    USE_KUBECTL=true
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

echo "1️⃣ Coherence Goals Status:"
echo "---------------------------"
GOAL_KEY="reasoning:curiosity_goals:system_coherence"
GOAL_COUNT=$(redis_cmd LLEN "$GOAL_KEY" 2>/dev/null || echo "0")
echo "   Total goals: $GOAL_COUNT"

if [ "$GOAL_COUNT" -gt 0 ]; then
  echo ""
  echo "   Status breakdown:"
  redis_cmd LRANGE "$GOAL_KEY" 0 199 2>/dev/null | while read -r line; do
    if [ -n "$line" ] && [ "$line" != "(nil)" ]; then
      status=$(echo "$line" | jq -r '.status' 2>/dev/null)
      id=$(echo "$line" | jq -r '.id' 2>/dev/null)
      if [ -n "$status" ] && [ "$status" != "null" ]; then
        echo "     - $id: $status"
      fi
    fi
  done | sort | uniq -c | awk '{print "       " $2 ": " $1}'
fi
echo ""

echo "2️⃣ Inconsistencies Status:"
echo "---------------------------"
INC_KEY="coherence:inconsistencies:${AGENT_ID}"
INC_COUNT=$(redis_cmd LLEN "$INC_KEY" 2>/dev/null || echo "0")
echo "   Total inconsistencies: $INC_COUNT"

if [ "$INC_COUNT" -gt 0 ]; then
  RESOLVED_COUNT=$(redis_cmd LRANGE "$INC_KEY" 0 199 2>/dev/null | grep -o '"resolved":true' | wc -l | tr -d ' ')
  UNRESOLVED_COUNT=$((INC_COUNT - RESOLVED_COUNT))
  echo "   Resolved: $RESOLVED_COUNT"
  echo "   Unresolved: $UNRESOLVED_COUNT"
fi
echo ""

echo "3️⃣ Goal Manager Tasks:"
echo "----------------------"
if [ "$USE_KUBECTL" = true ] && [ -n "$GOAL_MGR_POD" ]; then
  echo "   Checking Goal Manager for coherence tasks..."
  ACTIVE_GOALS=$(kubectl exec -n "$NAMESPACE" "$GOAL_MGR_POD" -- wget -qO- http://localhost:8090/goals/agent_1/active 2>/dev/null)
  if [ -n "$ACTIVE_GOALS" ]; then
    COHERENCE_ACTIVE=$(echo "$ACTIVE_GOALS" | jq '[.[] | select(.context.domain == "system_coherence")] | length' 2>/dev/null || echo "0")
    echo "   Active coherence goals in Goal Manager: $COHERENCE_ACTIVE"
    
    if [ "$COHERENCE_ACTIVE" -gt 0 ]; then
      echo ""
      echo "   Active coherence goals:"
      echo "$ACTIVE_GOALS" | jq -r '.[] | select(.context.domain == "system_coherence") | "     - \(.id): \(.status) - \(.description[:50])..."' 2>/dev/null
    fi
  fi
else
  echo "   ⚠️  Goal Manager check skipped (local mode or not available)"
fi
echo ""

echo "4️⃣ Goal Mappings:"
echo "-----------------"
MAPPING_COUNT=$(redis_cmd KEYS "coherence:goal_mapping:*" 2>/dev/null | wc -l | tr -d ' ')
echo "   Active mappings: $MAPPING_COUNT"
if [ "$MAPPING_COUNT" -gt 0 ]; then
  echo "   (These track which curiosity goals map to which inconsistencies)"
fi
echo ""

echo "5️⃣ Recent Resolution Events:"
echo "-----------------------------"
if [ "$USE_KUBECTL" = true ]; then
  FSM_POD=$(kubectl get pods -n "$NAMESPACE" -l app=fsm-server-rpi58 -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
  if [ -n "$FSM_POD" ]; then
    echo "   Checking FSM logs for resolution events..."
    RESOLUTION_EVENTS=$(kubectl logs -n "$NAMESPACE" "$FSM_POD" --tail=200 2>/dev/null | grep -iE "coherence.*resolv|coherence.*goal.*completed" | tail -5)
    if [ -n "$RESOLUTION_EVENTS" ]; then
      echo "$RESOLUTION_EVENTS" | sed 's/^/     /'
    else
      echo "     ℹ️  No resolution events found in recent logs"
      echo "     (This is normal if no goals have completed yet)"
    fi
  fi
else
  echo "   ⚠️  FSM log check skipped (local mode)"
fi
echo ""

echo "💡 Analysis:"
echo "-----------"
if [ "$GOAL_COUNT" -gt 0 ]; then
  PENDING_COUNT=$(redis_cmd LRANGE "$GOAL_KEY" 0 199 2>/dev/null | grep -o '"status":"pending"' | wc -l | tr -d ' ')
  if [ "$PENDING_COUNT" -gt 0 ]; then
    echo "   ⚠️  Found $PENDING_COUNT pending coherence goals"
    echo "   These need to be:"
    echo "     1. Converted to Goal Manager tasks (happens every 30s)"
    echo "     2. Executed by FSM Goals Poller"
    echo "     3. Completed (achieved/failed)"
    echo "     4. Then they'll be marked as resolved"
    echo ""
    echo "   Next steps:"
    echo "     - Wait for Monitor Service to convert goals (check Goal Manager)"
    echo "     - Wait for goals to execute and complete"
    echo "     - Check logs for resolution events"
  fi
fi

if [ "$INC_COUNT" -gt 0 ] && [ "$RESOLVED_COUNT" = "0" ]; then
  echo "   ⚠️  Found $INC_COUNT inconsistencies, none resolved yet"
  echo "   Resolution happens when:"
  echo "     1. Coherence goal is created"
  echo "     2. Goal Manager task completes"
  echo "     3. NATS event (agi.goal.achieved/failed) is published"
  echo "     4. Coherence monitor receives event and marks as resolved"
fi
echo ""

