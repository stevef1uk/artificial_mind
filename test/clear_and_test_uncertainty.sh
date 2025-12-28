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
FSM_URL="${FSM_URL:-http://localhost:8083}"

# Send input to trigger full learning flow
curl -s -X POST "$FSM_URL/input" \
  -H "Content-Type: application/json" \
  -d '{
    "input": "Artificial intelligence and machine learning systems use neural networks to process complex data patterns",
    "session_id": "uncertainty_fresh_test_'$(date +%s)'"
  }' > /dev/null

echo "   ✅ Input sent"
echo "   ⏳ Waiting 20 seconds for processing..."
sleep 20
echo ""

echo "3. Checking NEW hypotheses for uncertainty models..."
HYP_DATA=$(redis_cmd HGETALL fsm:agent_1:hypotheses)

if [ -z "$HYP_DATA" ]; then
  echo "   ⚠️  No hypotheses found (may need more time)"
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

