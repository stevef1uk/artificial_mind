#!/bin/bash

# Comprehensive check of coherence monitor status in Kubernetes

NAMESPACE="${K8S_NAMESPACE:-agi}"

echo "🔍 Comprehensive Coherence Monitor Status Check"
echo "================================================"
echo ""

# Get pods
FSM_POD=$(kubectl get pods -n "$NAMESPACE" -l app=fsm-server-rpi58 --sort-by=.metadata.creationTimestamp -o jsonpath='{.items[-1].metadata.name}' 2>/dev/null)
MONITOR_POD=$(kubectl get pods -n "$NAMESPACE" -l app=monitor-ui -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
REDIS_POD=$(kubectl get pods -n "$NAMESPACE" -l app=redis -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
GOAL_MGR_POD=$(kubectl get pods -n "$NAMESPACE" -l app=goal-manager -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)

echo "📦 Pods:"
echo "   FSM: ${FSM_POD:-not found}"
echo "   Monitor: ${MONITOR_POD:-not found}"
echo "   Redis: ${REDIS_POD:-not found}"
echo "   Goal Manager: ${GOAL_MGR_POD:-not found}"
echo ""

# Redis access function
redis_cmd() {
  if [ -n "$REDIS_POD" ]; then
    kubectl exec -n "$NAMESPACE" "$REDIS_POD" -- redis-cli "$@" 2>/dev/null
  else
    echo "(nil)"
  fi
}

echo "1️⃣ FSM Coherence Monitor Status:"
echo "----------------------------------"
if [ -n "$FSM_POD" ]; then
  echo "   Checking FSM logs..."
  
  # Check if monitor started
  STARTED=$(kubectl logs -n "$NAMESPACE" "$FSM_POD" 2>/dev/null | grep -i "Coherence monitoring loop started" | tail -1)
  if [ -n "$STARTED" ]; then
    echo "   ✅ Monitor started: $STARTED"
  else
    echo "   ❌ Monitor not started!"
  fi
  
  # Check for initial check
  INITIAL=$(kubectl logs -n "$NAMESPACE" "$FSM_POD" 2>/dev/null | grep -i "Running initial coherence check" | tail -1)
  if [ -n "$INITIAL" ]; then
    echo "   ✅ Initial check ran: $INITIAL"
  fi
  
  # Count coherence checks
  CHECK_COUNT=$(kubectl logs -n "$NAMESPACE" "$FSM_POD" 2>/dev/null | grep -c "Starting cross-system coherence check" 2>/dev/null || echo "0")
  CHECK_COUNT=$(echo "$CHECK_COUNT" | tr -d '\n' | tr -d ' ')
  echo "   Total coherence checks run: $CHECK_COUNT"
  
  # Get latest check results
  echo ""
  echo "   Latest check results:"
  LATEST_CHECK=$(kubectl logs -n "$NAMESPACE" "$FSM_POD" --tail=200 2>/dev/null | grep -iE "\[Coherence\].*complete|\[Coherence\].*Detected|\[Coherence\].*found" | tail -5)
  if [ -n "$LATEST_CHECK" ]; then
    echo "$LATEST_CHECK" | sed 's/^/      /'
  else
    echo "      ℹ️  No completion messages found yet"
  fi
  
  # Check for errors
  ERRORS=$(kubectl logs -n "$NAMESPACE" "$FSM_POD" --tail=200 2>/dev/null | grep -iE "\[Coherence\].*error|\[Coherence\].*failed|\[Coherence\].*panic" | tail -5)
  if [ -n "$ERRORS" ]; then
    echo ""
    echo "   ⚠️  Errors found:"
    echo "$ERRORS" | sed 's/^/      /'
  fi
else
  echo "   ❌ FSM pod not found"
fi
echo ""

echo "2️⃣ Monitor Service Goal Conversion:"
echo "-------------------------------------"
if [ -n "$MONITOR_POD" ]; then
  echo "   Checking Monitor Service logs..."
  
  # Check if consumer started
  CONSUMER_STARTED=$(kubectl logs -n "$NAMESPACE" "$MONITOR_POD" 2>/dev/null | grep -i "Starting curiosity goal consumer" | tail -1)
  if [ -n "$CONSUMER_STARTED" ]; then
    echo "   ✅ Curiosity goal consumer started"
  else
    echo "   ❌ Consumer not started!"
  fi
  
  # Check for system_coherence processing
  COHERENCE_PROCESSING=$(kubectl logs -n "$NAMESPACE" "$MONITOR_POD" --tail=200 2>/dev/null | grep -i "Checking.*system_coherence\|Processing.*system_coherence" | tail -5)
  if [ -n "$COHERENCE_PROCESSING" ]; then
    echo ""
    echo "   Recent system_coherence processing:"
    echo "$COHERENCE_PROCESSING" | sed 's/^/      /'
  else
    echo "   ⚠️  No system_coherence processing found"
  fi
  
  # Check for successful conversions
  CONVERSIONS=$(kubectl logs -n "$NAMESPACE" "$MONITOR_POD" --tail=200 2>/dev/null | grep -i "Converted curiosity goal.*coherence\|Converted curiosity goal.*system_coherence" | tail -5)
  if [ -n "$CONVERSIONS" ]; then
    echo ""
    echo "   ✅ Successful conversions:"
    echo "$CONVERSIONS" | sed 's/^/      /'
  else
    echo "   ⚠️  No coherence goal conversions found"
  fi
  
  # Check for errors
  CONVERSION_ERRORS=$(kubectl logs -n "$NAMESPACE" "$MONITOR_POD" --tail=200 2>/dev/null | grep -iE "Failed to convert.*system_coherence|error.*system_coherence" | tail -5)
  if [ -n "$CONVERSION_ERRORS" ]; then
    echo ""
    echo "   ⚠️  Conversion errors:"
    echo "$CONVERSION_ERRORS" | sed 's/^/      /'
  fi
else
  echo "   ❌ Monitor pod not found"
fi
echo ""

echo "3️⃣ Redis Data Status:"
echo "---------------------"
if [ -n "$REDIS_POD" ]; then
  INC_COUNT=$(redis_cmd LLEN "coherence:inconsistencies:agent_1" 2>/dev/null || echo "0")
  GOAL_COUNT=$(redis_cmd LLEN "reasoning:curiosity_goals:system_coherence" 2>/dev/null || echo "0")
  TASK_COUNT=$(redis_cmd LLEN "coherence:reflection_tasks:agent_1" 2>/dev/null || echo "0")
  MAPPING_COUNT=$(redis_cmd KEYS "coherence:goal_mapping:*" 2>/dev/null | wc -l | tr -d ' ')
  
  echo "   Inconsistencies: $INC_COUNT"
  echo "   Coherence goals: $GOAL_COUNT"
  echo "   Reflection tasks: $TASK_COUNT"
  echo "   Goal mappings: $MAPPING_COUNT"
  
  # Check resolved count
  if [ "$INC_COUNT" -gt 0 ]; then
    RESOLVED_COUNT=$(redis_cmd LRANGE "coherence:inconsistencies:agent_1" 0 199 2>/dev/null | grep -o '"resolved":true' | wc -l | tr -d ' ')
    echo "   Resolved inconsistencies: $RESOLVED_COUNT"
  fi
  
  # Check goal statuses
  if [ "$GOAL_COUNT" -gt 0 ]; then
    echo ""
    echo "   Goal status breakdown:"
    redis_cmd LRANGE "reasoning:curiosity_goals:system_coherence" 0 199 2>/dev/null | while read -r line; do
      if [ -n "$line" ] && [ "$line" != "(nil)" ]; then
        status=$(echo "$line" | jq -r '.status' 2>/dev/null)
        if [ -n "$status" ] && [ "$status" != "null" ]; then
          echo "$status"
        fi
      fi
    done | sort | uniq -c | awk '{print "      " $2 ": " $1}'
  fi
else
  echo "   ❌ Redis pod not found"
fi
echo ""

echo "4️⃣ Goal Manager Status:"
echo "------------------------"
if [ -n "$GOAL_MGR_POD" ]; then
  ACTIVE_GOALS=$(kubectl exec -n "$NAMESPACE" "$GOAL_MGR_POD" -- wget -qO- http://localhost:8090/goals/agent_1/active 2>/dev/null)
  if [ -n "$ACTIVE_GOALS" ]; then
    COHERENCE_ACTIVE=$(echo "$ACTIVE_GOALS" | jq '[.[] | select(.context.domain == "system_coherence")] | length' 2>/dev/null || echo "0")
    echo "   Active coherence goals: $COHERENCE_ACTIVE"
    
    if [ "$COHERENCE_ACTIVE" -gt 0 ]; then
      echo ""
      echo "   Active coherence goals:"
      echo "$ACTIVE_GOALS" | jq -r '.[] | select(.context.domain == "system_coherence") | "      - \(.id): \(.status) - \(.description[:60])..."' 2>/dev/null | head -5
    fi
  fi
else
  echo "   ❌ Goal Manager pod not found"
fi
echo ""

echo "5️⃣ Resolution Events:"
echo "----------------------"
if [ -n "$FSM_POD" ]; then
  RESOLUTION_EVENTS=$(kubectl logs -n "$NAMESPACE" "$FSM_POD" --tail=500 2>/dev/null | grep -iE "\[Coherence\].*resolv|\[Coherence\].*goal.*completed|\[Coherence\].*Marked.*resolved" | tail -10)
  if [ -n "$RESOLUTION_EVENTS" ]; then
    echo "   ✅ Resolution events found:"
    echo "$RESOLUTION_EVENTS" | sed 's/^/      /'
  else
    echo "   ℹ️  No resolution events yet (goals may not have completed)"
  fi
fi
echo ""

echo "6️⃣ Recent Activity Summary:"
echo "----------------------------"
if [ -n "$FSM_POD" ]; then
  echo "   Last 10 coherence-related log entries:"
  kubectl logs -n "$NAMESPACE" "$FSM_POD" --tail=500 2>/dev/null | grep -i "\[Coherence\]" | tail -10 | sed 's/^/      /'
fi
echo ""

echo "💡 Summary:"
echo "-----------"
if [ -n "$FSM_POD" ] && [ -n "$MONITOR_POD" ]; then
  echo "   ✅ FSM pod running"
  echo "   ✅ Monitor pod running"
  
  CHECK_COUNT_NUM=$(echo "$CHECK_COUNT" | tr -d '\n' | tr -d ' ')
  if [ -n "$CHECK_COUNT_NUM" ] && [ "$CHECK_COUNT_NUM" -gt 0 ] 2>/dev/null; then
    echo "   ✅ Coherence checks are running ($CHECK_COUNT_NUM check(s))"
  else
    echo "   ⚠️  No coherence checks completed yet"
  fi
  
  if [ "$GOAL_COUNT" -gt 0 ]; then
    echo "   ✅ Coherence goals exist ($GOAL_COUNT)"
  fi
  
  if [ "$COHERENCE_ACTIVE" -gt 0 ]; then
    echo "   ✅ Goals are being processed in Goal Manager ($COHERENCE_ACTIVE active)"
  else
    echo "   ⚠️  No active coherence goals in Goal Manager"
    echo "      (Monitor Service should convert them every 30 seconds)"
  fi
fi
echo ""

