#!/bin/bash

# Deep dive into why coherence goals aren't staying active

NAMESPACE="${K8S_NAMESPACE:-agi}"

echo "🔍 Deep Dive: Coherence Goals Execution Flow"
echo "============================================"
echo ""

# Get pods
FSM_POD=$(kubectl get pods -n "$NAMESPACE" -l app=fsm-server-rpi58 --sort-by=.metadata.creationTimestamp -o jsonpath='{.items[-1].metadata.name}' 2>/dev/null)
GOAL_MGR_POD=$(kubectl get pods -n "$NAMESPACE" -l app=goal-manager -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
REDIS_POD=$(kubectl get pods -n "$NAMESPACE" -l app=redis -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)

echo "1️⃣ Check Goal Manager for ALL Goals (not just active):"
echo "------------------------------------------------------"
if [ -n "$GOAL_MGR_POD" ]; then
  # Check all goals endpoint if it exists, or check archived
  ALL_GOALS=$(kubectl exec -n "$NAMESPACE" "$GOAL_MGR_POD" -- wget -qO- http://localhost:8090/goals/agent_1/all 2>/dev/null)
  if [ -n "$ALL_GOALS" ]; then
    COHERENCE_ALL=$(echo "$ALL_GOALS" | jq '[.[] | select(.context.domain == "system_coherence")]' 2>/dev/null)
    COHERENCE_COUNT=$(echo "$COHERENCE_ALL" | jq 'length' 2>/dev/null || echo "0")
    echo "   Total coherence goals (all statuses): $COHERENCE_COUNT"
    
    if [ "$COHERENCE_COUNT" -gt 0 ]; then
      echo ""
      echo "   Coherence goals by status:"
      echo "$COHERENCE_ALL" | jq -r '.[] | "      - \(.id): \(.status) - created: \(.created_at)"' 2>/dev/null | head -10
      
      # Check for achieved/failed
      ACHIEVED=$(echo "$COHERENCE_ALL" | jq '[.[] | select(.status == "achieved" or .status == "failed")] | length' 2>/dev/null || echo "0")
      echo ""
      echo "   Achieved/Failed: $ACHIEVED"
    fi
  else
    echo "   ⚠️  Could not fetch all goals"
  fi
fi
echo ""

echo "2️⃣ Check FSM Goals Poller Activity:"
echo "------------------------------------"
if [ -n "$FSM_POD" ]; then
  echo "   Recent goal triggers (last 20):"
  TRIGGERS=$(kubectl logs -n "$NAMESPACE" "$FSM_POD" --tail=500 2>/dev/null | grep -iE "\[FSM\]\[Goals\].*triggered.*goal|\[FSM\]\[Goals\].*coherence" | tail -20)
  if [ -n "$TRIGGERS" ]; then
    echo "$TRIGGERS" | sed 's/^/      /'
  else
    echo "      ℹ️  No recent goal triggers found"
  fi
  echo ""
  
  # Check for coherence goal IDs in triggers
  echo "   Checking for coherence goal IDs in triggers..."
  COHERENCE_TRIGGERS=$(kubectl logs -n "$NAMESPACE" "$FSM_POD" --tail=1000 2>/dev/null | grep -iE "triggered goal g_.*coherence|coherence.*goal.*triggered" | tail -10)
  if [ -n "$COHERENCE_TRIGGERS" ]; then
    echo "$COHERENCE_TRIGGERS" | sed 's/^/      /'
  else
    echo "      ℹ️  No coherence-specific triggers found"
  fi
fi
echo ""

echo "3️⃣ Check Goal Manager Logs for Events:"
echo "--------------------------------------"
if [ -n "$GOAL_MGR_POD" ]; then
  echo "   Recent goal.achieved events (last 20):"
  ACHIEVED_EVENTS=$(kubectl logs -n "$NAMESPACE" "$GOAL_MGR_POD" --tail=500 2>/dev/null | grep -i "agi.goal.achieved\|goal.achieved" | tail -20)
  if [ -n "$ACHIEVED_EVENTS" ]; then
    echo "$ACHIEVED_EVENTS" | sed 's/^/      /'
  else
    echo "      ℹ️  No achieved events found"
  fi
  echo ""
  
  echo "   Recent goal creation events:"
  CREATED_EVENTS=$(kubectl logs -n "$NAMESPACE" "$GOAL_MGR_POD" --tail=500 2>/dev/null | grep -i "agi.goal.created" | tail -10)
  if [ -n "$CREATED_EVENTS" ]; then
    echo "$CREATED_EVENTS" | sed 's/^/      /'
  fi
fi
echo ""

echo "4️⃣ Check FSM for NATS Event Reception:"
echo "--------------------------------------"
if [ -n "$FSM_POD" ]; then
  echo "   Checking if FSM is receiving goal.achieved events..."
  # The coherence monitor subscribes to agi.goal.achieved
  NATS_EVENTS=$(kubectl logs -n "$NAMESPACE" "$FSM_POD" --tail=1000 2>/dev/null | grep -iE "agi.goal.achieved|agi.goal.failed|Received.*goal.*event" | tail -10)
  if [ -n "$NATS_EVENTS" ]; then
    echo "$NATS_EVENTS" | sed 's/^/      /'
  else
    echo "      ℹ️  No NATS goal events found in FSM logs"
    echo "      (This might mean events aren't being received or logged)"
  fi
fi
echo ""

echo "5️⃣ Check Redis for Goal Mappings:"
echo "---------------------------------"
if [ -n "$REDIS_POD" ]; then
  MAPPING_KEYS=$(kubectl exec -n "$NAMESPACE" "$REDIS_POD" -- redis-cli KEYS "coherence:goal_mapping:*" 2>/dev/null | head -10)
  MAPPING_COUNT=$(echo "$MAPPING_KEYS" | wc -l | tr -d ' ')
  echo "   Active mappings: $MAPPING_COUNT"
  
  if [ "$MAPPING_COUNT" -gt 0 ]; then
    echo ""
    echo "   Sample mappings:"
    for key in $(echo "$MAPPING_KEYS" | head -5); do
      inconsistency_id=$(kubectl exec -n "$NAMESPACE" "$REDIS_POD" -- redis-cli GET "$key" 2>/dev/null)
      curiosity_id=$(echo "$key" | sed 's/coherence:goal_mapping://')
      echo "      $curiosity_id -> $inconsistency_id"
    done
  fi
fi
echo ""

echo "💡 Analysis:"
echo "-----------"
echo "   If goals are being converted but not staying active:"
echo "     1. They might be completing immediately (check workflow completion logs)"
echo "     2. They might be getting archived right away"
echo "     3. FSM Goals Poller might be executing them very quickly"
echo ""
echo "   If goals complete but no resolution events:"
echo "     1. NATS events might not be reaching FSM"
echo "     2. Coherence monitor subscription might not be working"
echo "     3. Events might be published but not logged"
echo ""


