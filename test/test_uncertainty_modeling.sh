#!/bin/bash

# Test script for uncertainty modeling and confidence calibration
# This tests the new uncertainty tracking features

REDIS_HOST="${REDIS_HOST:-localhost}"
REDIS_PORT="${REDIS_PORT:-6379}"
FSM_URL="${FSM_URL:-http://localhost:8083}"
HDN_URL="${HDN_URL:-http://localhost:8081}"
NAMESPACE="${K8S_NAMESPACE:-agi}"

echo "🧪 Testing Uncertainty Modeling & Confidence Calibration"
echo "=========================================================="
echo ""

# Detect if we're in a k3s/k8s environment
USE_KUBECTL=false
REDIS_POD=""
if command -v kubectl &> /dev/null; then
  # Try to find Redis pod
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

# Function to check Redis key and extract uncertainty data
check_uncertainty_in_redis() {
  local key=$1
  local type=$2
  echo "📊 Checking $type in Redis key: $key"
  
  # Get data from Redis
  if [ "$type" = "hypothesis" ]; then
    # Hypotheses are stored in a hash
    data=$(redis_cmd HGETALL "$key" | head -20)
  else
    # Goals and beliefs are in lists
    data=$(redis_cmd LRANGE "$key" 0 4)
  fi
  
  if [ -z "$data" ]; then
    echo "   ⚠️  No $type data found (may need to trigger generation)"
    return 1
  fi
  
  # Check for uncertainty fields
  if echo "$data" | grep -q "uncertainty"; then
    echo "   ✅ Uncertainty data found!"
    
    # Extract and display uncertainty values
    echo "$data" | grep -o '"uncertainty":{[^}]*}' | head -1 | while IFS= read -r line; do
      echo "   📈 Uncertainty model:"
      echo "$line" | grep -o '"epistemic_uncertainty":[0-9.]*' | sed 's/.*:/      Epistemic: /'
      echo "$line" | grep -o '"aleatoric_uncertainty":[0-9.]*' | sed 's/.*:/      Aleatoric: /'
      echo "$line" | grep -o '"calibrated_confidence":[0-9.]*' | sed 's/.*:/      Calibrated Confidence: /'
      echo "$line" | grep -o '"stability":[0-9.]*' | sed 's/.*:/      Stability: /'
      echo "$line" | grep -o '"volatility":[0-9.]*' | sed 's/.*:/      Volatility: /'
    done
    return 0
  else
    echo "   ⚠️  No uncertainty data found in $type"
    echo "   Raw data sample:"
    echo "$data" | head -3 | sed 's/^/      /'
    return 1
  fi
}

# Function to trigger FSM event
trigger_fsm_event() {
  local event=$1
  echo "🔄 Triggering FSM event: $event"
  
  response=$(curl -s -X POST "$FSM_URL/api/v1/events" \
    -H "Content-Type: application/json" \
    -d "{\"event\": \"$event\", \"payload\": {}}" 2>&1)
  
  if echo "$response" | grep -q "error\|Error"; then
    echo "   ⚠️  Event trigger may have failed: $(echo "$response" | head -c 100)"
    return 1
  else
    echo "   ✅ Event triggered"
    return 0
  fi
}

# Test 1: Check existing hypotheses for uncertainty data
echo "Test 1: Checking existing hypotheses for uncertainty models"
echo "------------------------------------------------------------"
HYP_KEY="fsm:agent_1:hypotheses"
check_uncertainty_in_redis "$HYP_KEY" "hypothesis"
echo ""

# Test 2: Check existing goals for uncertainty data
echo "Test 2: Checking existing goals for uncertainty models"
echo "-------------------------------------------------------"
GOAL_KEY="reasoning:curiosity_goals:General"
check_uncertainty_in_redis "$GOAL_KEY" "goal"
echo ""

# Test 3: Check existing beliefs for uncertainty data
echo "Test 3: Checking existing beliefs for uncertainty models"
echo "---------------------------------------------------------"
BELIEF_KEY="reasoning:beliefs:General"
check_uncertainty_in_redis "$BELIEF_KEY" "belief"
echo ""

# Test 4: Trigger hypothesis generation
echo "Test 4: Triggering hypothesis generation"
echo "-----------------------------------------"
if trigger_fsm_event "generate_hypotheses"; then
  echo "   ⏳ Waiting 3 seconds for processing..."
  sleep 3
  echo ""
  echo "   Re-checking hypotheses:"
  check_uncertainty_in_redis "$HYP_KEY" "hypothesis"
fi
echo ""

# Test 5: Trigger goal generation
echo "Test 5: Triggering curiosity goal generation"
echo "----------------------------------------------"
if trigger_fsm_event "generate_curiosity_goals"; then
  echo "   ⏳ Waiting 3 seconds for processing..."
  sleep 3
  echo ""
  echo "   Re-checking goals:"
  check_uncertainty_in_redis "$GOAL_KEY" "goal"
fi
echo ""

# Test 6: Trigger inference to create beliefs
echo "Test 6: Triggering inference to create beliefs"
echo "-----------------------------------------------"
if trigger_fsm_event "infer_beliefs"; then
  echo "   ⏳ Waiting 3 seconds for processing..."
  sleep 3
  echo ""
  echo "   Re-checking beliefs:"
  check_uncertainty_in_redis "$BELIEF_KEY" "belief"
fi
echo ""

# Test 7: Check FSM logs for uncertainty-related messages
echo "Test 7: Checking FSM logs for uncertainty messages"
echo "---------------------------------------------------"
LOGS=""
if [ "$USE_KUBECTL" = true ]; then
  # Get FSM pod and check logs
  FSM_POD=$(kubectl get pods -n "$NAMESPACE" -l app=fsm-server -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
  if [ -z "$FSM_POD" ]; then
    FSM_POD=$(kubectl get pods -n "$NAMESPACE" -o jsonpath='{.items[?(@.metadata.name=~"fsm.*")].metadata.name}' 2>/dev/null | awk '{print $1}')
  fi
  if [ -n "$FSM_POD" ]; then
    LOGS=$(kubectl logs -n "$NAMESPACE" "$FSM_POD" --tail=200 2>/dev/null | grep -i "uncertainty\|epistemic\|aleatoric\|calibrated\|stability\|volatility" | tail -10)
  fi
elif command -v journalctl &> /dev/null; then
  # Systemd service
  LOGS=$(journalctl -u fsm-server --since "5 minutes ago" --no-pager 2>/dev/null | grep -i "uncertainty\|epistemic\|aleatoric\|calibrated\|stability\|volatility" | tail -10)
elif [ -f "/tmp/fsm-server.log" ]; then
  # Log file
  LOGS=$(tail -100 /tmp/fsm-server.log 2>/dev/null | grep -i "uncertainty\|epistemic\|aleatoric\|calibrated\|stability\|volatility" | tail -10)
fi

if [ -n "$LOGS" ]; then
  echo "   ✅ Found uncertainty-related log messages:"
  echo "$LOGS" | sed 's/^/      /'
else
  echo "   ℹ️  No uncertainty-related log messages found (may need to check logs manually)"
  echo "   Try: tail -f /path/to/fsm-server.log | grep -i uncertainty"
fi
echo ""

# Test 8: Detailed JSON inspection of a hypothesis
echo "Test 8: Detailed inspection of hypothesis uncertainty model"
echo "------------------------------------------------------------"
HYP_DATA=$(redis_cmd HGETALL "$HYP_KEY" | grep -A 1 "hyp_" | head -2)
if [ -n "$HYP_DATA" ]; then
  HYP_JSON=$(echo "$HYP_DATA" | tail -1)
  if command -v jq &> /dev/null; then
    echo "   Hypothesis JSON with uncertainty:"
    echo "$HYP_JSON" | jq '.' 2>/dev/null | head -30 | sed 's/^/      /'
    
    # Extract uncertainty values
    EPISTEMIC=$(echo "$HYP_JSON" | jq -r '.uncertainty.epistemic_uncertainty // "N/A"' 2>/dev/null)
    ALEATORIC=$(echo "$HYP_JSON" | jq -r '.uncertainty.aleatoric_uncertainty // "N/A"' 2>/dev/null)
    CALIBRATED=$(echo "$HYP_JSON" | jq -r '.uncertainty.calibrated_confidence // "N/A"' 2>/dev/null)
    STABILITY=$(echo "$HYP_JSON" | jq -r '.uncertainty.stability // "N/A"' 2>/dev/null)
    
    echo ""
    echo "   📊 Uncertainty Metrics:"
    echo "      Epistemic Uncertainty: $EPISTEMIC"
    echo "      Aleatoric Uncertainty: $ALEATORIC"
    echo "      Calibrated Confidence: $CALIBRATED"
    echo "      Stability: $STABILITY"
    
    # Validate values are in expected range
    if [ "$EPISTEMIC" != "N/A" ] && [ "$(echo "$EPISTEMIC >= 0 && $EPISTEMIC <= 1" | bc 2>/dev/null)" = "1" ]; then
      echo "      ✅ Epistemic uncertainty in valid range [0,1]"
    fi
    if [ "$CALIBRATED" != "N/A" ] && [ "$(echo "$CALIBRATED >= 0 && $CALIBRATED <= 1" | bc 2>/dev/null)" = "1" ]; then
      echo "      ✅ Calibrated confidence in valid range [0,1]"
    fi
  else
    echo "   Install 'jq' for better JSON formatting"
    echo "$HYP_JSON" | head -c 500 | sed 's/^/      /'
  fi
else
  echo "   ⚠️  No hypotheses found to inspect"
fi
echo ""

# Test 9: Check goal scoring with uncertainty
echo "Test 9: Checking goal scoring with uncertainty models"
echo "------------------------------------------------------"
GOAL_DATA=$(redis_cmd LRANGE "$GOAL_KEY" 0 0)
if [ -n "$GOAL_DATA" ]; then
  if command -v jq &> /dev/null; then
    echo "$GOAL_DATA" | jq '.' 2>/dev/null | head -40 | sed 's/^/      /'
    
    # Check if goal has uncertainty
    if echo "$GOAL_DATA" | jq -e '.uncertainty' > /dev/null 2>&1; then
      echo ""
      echo "   ✅ Goal has uncertainty model"
      echo "$GOAL_DATA" | jq '.uncertainty' 2>/dev/null | sed 's/^/      /'
    else
      echo ""
      echo "   ⚠️  Goal does not have uncertainty model"
    fi
  else
    echo "$GOAL_DATA" | head -c 500 | sed 's/^/      /'
  fi
else
  echo "   ⚠️  No goals found to inspect"
fi
echo ""

# Summary
echo "=========================================================="
echo "Test Summary"
echo "=========================================================="
echo ""
echo "To verify uncertainty modeling is working:"
echo "  1. Check that hypotheses have 'uncertainty' fields in Redis"
echo "  2. Check that goals have 'uncertainty' fields in Redis"
echo "  3. Check that beliefs have 'uncertainty' fields in Redis"
echo "  4. Verify uncertainty values are in range [0,1]"
echo "  5. Check FSM logs for uncertainty-related messages"
echo ""
echo "To view data in Monitor UI:"
echo "  - Hypotheses: http://localhost:8084/hypotheses/General"
echo "  - Goals: http://localhost:8084/goals/General"
echo "  - Beliefs: http://localhost:8084/beliefs/General"
echo ""
echo "To manually inspect Redis:"
if [ "$USE_KUBECTL" = true ]; then
  echo "  kubectl exec -n $NAMESPACE $REDIS_POD -- redis-cli HGETALL fsm:agent_1:hypotheses"
  echo "  kubectl exec -n $NAMESPACE $REDIS_POD -- redis-cli LRANGE reasoning:curiosity_goals:General 0 4"
  echo "  kubectl exec -n $NAMESPACE $REDIS_POD -- redis-cli LRANGE reasoning:beliefs:General 0 4"
else
  echo "  redis-cli HGETALL fsm:agent_1:hypotheses"
  echo "  redis-cli LRANGE reasoning:curiosity_goals:General 0 4"
  echo "  redis-cli LRANGE reasoning:beliefs:General 0 4"
fi
echo ""

