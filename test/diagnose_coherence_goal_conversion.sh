#!/bin/bash

# Diagnose why coherence goals aren't being converted to Goal Manager tasks

NAMESPACE="${K8S_NAMESPACE:-agi}"

echo "🔍 Diagnosing Coherence Goal Conversion"
echo "========================================"
echo ""

# Check if running in k8s
USE_KUBECTL=false
if command -v kubectl &> /dev/null; then
  MONITOR_POD=$(kubectl get pods -n "$NAMESPACE" -l app=monitor-ui -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
  REDIS_POD=$(kubectl get pods -n "$NAMESPACE" -l app=redis -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
  if [ -n "$MONITOR_POD" ] && [ -n "$REDIS_POD" ]; then
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

echo "1️⃣ Check Coherence Goals in Redis:"
echo "-----------------------------------"
GOAL_KEY="reasoning:curiosity_goals:system_coherence"
GOAL_COUNT=$(redis_cmd LLEN "$GOAL_KEY" 2>/dev/null || echo "0")
echo "   Total coherence goals: $GOAL_COUNT"

if [ "$GOAL_COUNT" -gt 0 ]; then
  echo "   Sample goal (first one):"
  redis_cmd LRANGE "$GOAL_KEY" 0 0 2>/dev/null | while read -r line; do
    if [ -n "$line" ] && [ "$line" != "(nil)" ]; then
      echo "$line" | jq -r '"     ID: \(.id)\n     Status: \(.status)\n     Description: \(.description[:80])..."' 2>/dev/null || echo "     Raw: $line"
    fi
  done
fi
echo ""

echo "2️⃣ Check Monitor Service Logs:"
echo "------------------------------"
if [ "$USE_KUBECTL" = true ] && [ -n "$MONITOR_POD" ]; then
  echo "   Checking for curiosity goal consumer activity..."
  
  # Check if consumer started
  STARTED=$(kubectl logs -n "$NAMESPACE" "$MONITOR_POD" 2>/dev/null | grep -i "Starting curiosity goal consumer" | tail -1)
  if [ -n "$STARTED" ]; then
    echo "   ✅ Curiosity goal consumer started"
  else
    echo "   ❌ Curiosity goal consumer NOT started!"
  fi
  echo ""
  
  # Check for system_coherence processing
  echo "   Recent system_coherence processing attempts:"
  COHERENCE_LOGS=$(kubectl logs -n "$NAMESPACE" "$MONITOR_POD" --tail=100 2>/dev/null | grep -i "system_coherence\|Checking.*system_coherence" | tail -10)
  if [ -n "$COHERENCE_LOGS" ]; then
    echo "$COHERENCE_LOGS" | sed 's/^/     /'
  else
    echo "     ℹ️  No system_coherence processing logs found"
  fi
  echo ""
  
  # Check for errors
  echo "   Recent errors:"
  ERRORS=$(kubectl logs -n "$NAMESPACE" "$MONITOR_POD" --tail=100 2>/dev/null | grep -iE "error.*system_coherence|failed.*system_coherence|Failed to convert.*system_coherence" | tail -10)
  if [ -n "$ERRORS" ]; then
    echo "$ERRORS" | sed 's/^/     /'
  else
    echo "     ✅ No errors found"
  fi
  echo ""
  
  # Check for successful conversions
  echo "   Successful conversions (last 20):"
  SUCCESS=$(kubectl logs -n "$NAMESPACE" "$MONITOR_POD" --tail=200 2>/dev/null | grep -i "Converted curiosity goal.*system_coherence\|Converted curiosity goal.*coherence_resolution" | tail -20)
  if [ -n "$SUCCESS" ]; then
    echo "$SUCCESS" | sed 's/^/     /'
  else
    echo "     ⚠️  No successful conversions found for coherence goals"
  fi
else
  echo "   ⚠️  Monitor Service check skipped (local mode or not available)"
fi
echo ""

echo "3️⃣ Check Goal Manager:"
echo "----------------------"
if [ "$USE_KUBECTL" = true ]; then
  GOAL_MGR_POD=$(kubectl get pods -n "$NAMESPACE" -l app=goal-manager -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
  if [ -n "$GOAL_MGR_POD" ]; then
    ACTIVE_GOALS=$(kubectl exec -n "$NAMESPACE" "$GOAL_MGR_POD" -- wget -qO- http://localhost:8090/goals/agent_1/active 2>/dev/null)
    if [ -n "$ACTIVE_GOALS" ]; then
      COHERENCE_COUNT=$(echo "$ACTIVE_GOALS" | jq '[.[] | select(.context.domain == "system_coherence")] | length' 2>/dev/null || echo "0")
      echo "   Active coherence goals: $COHERENCE_COUNT"
      
      # Check all goals (not just active) for coherence
      ALL_GOALS=$(kubectl exec -n "$NAMESPACE" "$GOAL_MGR_POD" -- wget -qO- http://localhost:8090/goals/agent_1/all 2>/dev/null)
      if [ -n "$ALL_GOALS" ]; then
        ALL_COHERENCE=$(echo "$ALL_GOALS" | jq '[.[] | select(.context.domain == "system_coherence")] | length' 2>/dev/null || echo "0")
        echo "   Total coherence goals (all statuses): $ALL_COHERENCE"
      fi
    fi
  fi
fi
echo ""

echo "💡 Recommendations:"
echo "------------------"
if [ "$GOAL_COUNT" -gt 0 ]; then
  echo "   ✅ Coherence goals exist in Redis ($GOAL_COUNT)"
  echo ""
  echo "   If Monitor Service isn't processing them:"
  echo "     1. Check Monitor Service logs for errors"
  echo "     2. Verify Monitor Service is running: kubectl get pods -n $NAMESPACE -l app=monitor-ui"
  echo "     3. Check if curiosity goal consumer started"
  echo "     4. Wait 30 seconds and check logs again (consumer runs every 30s)"
  echo ""
  echo "   To manually trigger conversion (for testing):"
  echo "     - Restart Monitor Service pod to force consumer restart"
  echo "     - Or wait for next 30-second tick"
fi
echo ""

