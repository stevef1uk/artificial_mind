#!/bin/bash

# Clear old hypotheses and test with fresh data

NAMESPACE="${K8S_NAMESPACE:-agi}"

echo "🧹 Clearing Old Data and Testing Fresh Uncertainty Models"
echo "=========================================================="
echo ""

# Detect k3s
USE_KUBECTL=false
REDIS_POD=""
if command -v kubectl &> /dev/null; then
  REDIS_POD=$(kubectl get pods -n "$NAMESPACE" -l app=redis -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
  if [ -n "$REDIS_POD" ]; then
    USE_KUBECTL=true
  fi
fi

redis_cmd() {
  if [ "$USE_KUBECTL" = true ]; then
    kubectl exec -n "$NAMESPACE" "$REDIS_POD" -- redis-cli "$@" 2>/dev/null
  else
    redis-cli -h "${REDIS_HOST:-localhost}" -p "${REDIS_PORT:-6379}" "$@" 2>/dev/null
  fi
}

echo "1. Clearing old hypotheses..."
redis_cmd DEL fsm:agent_1:hypotheses
echo "   ✅ Cleared"
echo ""

echo "2. Triggering new hypothesis generation..."

# Find FSM pod if in k3s
FSM_POD=""
if [ "$USE_KUBECTL" = true ]; then
  FSM_POD=$(kubectl get pods -n "$NAMESPACE" -l app=fsm-server-rpi58 -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
  if [ -z "$FSM_POD" ]; then
    FSM_POD=$(kubectl get pods -n "$NAMESPACE" -o jsonpath='{.items[?(@.metadata.name=~"fsm.*")].metadata.name}' 2>/dev/null | awk '{print $1}')
  fi
fi

FSM_URL="${FSM_URL:-http://localhost:8083}"

# Try to send input - use kubectl exec if in k3s, otherwise curl
if [ "$USE_KUBECTL" = true ] && [ -n "$FSM_POD" ]; then
  echo "   Using FSM pod: $FSM_POD"
  # Use kubectl exec to send input via the pod
  INPUT_DATA='{"input": "Artificial intelligence and machine learning systems use neural networks to process complex data patterns", "session_id": "uncertainty_fresh_test_'$(date +%s)'"}'
  kubectl exec -n "$NAMESPACE" "$FSM_POD" -- sh -c "echo '$INPUT_DATA' | curl -s -X POST http://localhost:8083/input -H 'Content-Type: application/json' -d @-" > /dev/null 2>&1
  echo "   ✅ Input sent via pod"
else
  # Try direct curl
  curl -s -X POST "$FSM_URL/input" \
    -H "Content-Type: application/json" \
    -d '{
      "input": "Artificial intelligence and machine learning systems use neural networks to process complex data patterns",
      "session_id": "uncertainty_fresh_test_'$(date +%s)'"
    }' > /dev/null 2>&1
  echo "   ✅ Input sent"
fi

echo "   ⏳ Waiting 30 seconds for processing (hypothesis generation can take time)..."
sleep 30
echo ""

# Check FSM status
echo "   Checking FSM status..."
if [ "$USE_KUBECTL" = true ] && [ -n "$FSM_POD" ]; then
  FSM_STATUS=$(kubectl exec -n "$NAMESPACE" "$FSM_POD" -- sh -c "curl -s http://localhost:8083/status 2>/dev/null" | grep -o '"current_state":"[^"]*"' | cut -d'"' -f4 || echo "unknown")
else
  FSM_STATUS=$(curl -s "$FSM_URL/status" 2>/dev/null | grep -o '"current_state":"[^"]*"' | cut -d'"' -f4 || echo "unknown")
fi
echo "   FSM current state: ${FSM_STATUS:-unknown}"
echo ""

echo "3. Checking NEW hypotheses for uncertainty models..."
HYP_DATA=$(redis_cmd HGETALL fsm:agent_1:hypotheses)

if [ -z "$HYP_DATA" ]; then
  echo "   ⚠️  No hypotheses found yet"
  echo ""
  echo "   This could mean:"
  echo "     - Hypothesis generation is still in progress (wait longer)"
  echo "     - FSM needs to be in the right state (check: curl $FSM_URL/status)"
  echo "     - No facts were extracted from the input"
  echo ""
  echo "   Try waiting another 30 seconds and checking again:"
  echo "     sleep 30"
  echo "     kubectl exec -n $NAMESPACE $REDIS_POD -- redis-cli HGETALL fsm:agent_1:hypotheses"
  echo ""
  echo "   Or check FSM logs for hypothesis generation:"
  if [ "$USE_KUBECTL" = true ]; then
    FSM_POD=$(kubectl get pods -n "$NAMESPACE" -l app=fsm-server -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
    if [ -z "$FSM_POD" ]; then
      FSM_POD=$(kubectl get pods -n "$NAMESPACE" -o jsonpath='{.items[?(@.metadata.name=~"fsm.*")].metadata.name}' 2>/dev/null | awk '{print $1}')
    fi
    if [ -n "$FSM_POD" ]; then
      echo "     kubectl logs -n $NAMESPACE $FSM_POD --tail=50 | grep -i hypothesis"
    fi
  fi
  exit 1
fi

# Count hypotheses with uncertainty
TOTAL_HYPS=$(echo "$HYP_DATA" | grep -c "hyp_" || echo "0")
UNCERTAIN_HYPS=$(echo "$HYP_DATA" | grep -c "uncertainty" || echo "0")

echo "   Total hypotheses: $TOTAL_HYPS"
echo "   Hypotheses with uncertainty: $UNCERTAIN_HYPS"
echo ""

if [ "$UNCERTAIN_HYPS" -eq 0 ]; then
  echo "   ❌ No uncertainty models found in new hypotheses"
  echo "   Sample data:"
  echo "$HYP_DATA" | head -10 | sed 's/^/      /'
  exit 1
fi

# Check specifically for hyp_fact with uncertainty
FACT_HYPS_WITH_UNCERTAINTY=$(echo "$HYP_DATA" | grep -A 20 "hyp_fact" | grep -c "uncertainty" || echo "0")
echo "   hyp_fact hypotheses with uncertainty: $FACT_HYPS_WITH_UNCERTAINTY"
echo ""

if [ "$FACT_HYPS_WITH_UNCERTAINTY" -gt 0 ]; then
  echo "   ✅ SUCCESS: New hyp_fact hypotheses have uncertainty models!"
  echo ""
  echo "   Sample hyp_fact with uncertainty:"
  echo "$HYP_DATA" | grep -A 15 "hyp_fact" | grep -A 15 "uncertainty" | head -20 | sed 's/^/      /'
else
  echo "   ⚠️  No hyp_fact hypotheses with uncertainty found yet"
  echo "   (May need to wait longer or check if hyp_fact hypotheses were generated)"
  echo ""
  echo "   Sample of what was found:"
  echo "$HYP_DATA" | head -20 | sed 's/^/      /'
fi

echo ""
echo "4. Run full test: ./test/test_uncertainty_modeling.sh"

