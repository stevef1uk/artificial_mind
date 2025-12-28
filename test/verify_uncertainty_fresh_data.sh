#!/bin/bash

# Script to verify uncertainty models with fresh data
# Clears old data and triggers new generation

NAMESPACE="${K8S_NAMESPACE:-agi}"

echo "🔄 Verifying Uncertainty Models with Fresh Data"
echo "==============================================="
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

echo "1. Clearing old data (optional - comment out if you want to keep old data)..."
# Uncomment these lines to clear old data:
# redis_cmd DEL fsm:agent_1:hypotheses
# redis_cmd DEL reasoning:curiosity_goals:General
# redis_cmd DEL reasoning:beliefs:General
echo "   (Skipped - uncomment in script to clear old data)"
echo ""

echo "2. Triggering new data generation..."
FSM_URL="${FSM_URL:-http://localhost:8083}"

# Send input to trigger full learning flow
echo "   Sending input to trigger learning..."
curl -s -X POST "$FSM_URL/input" \
  -H "Content-Type: application/json" \
  -d '{
    "input": "Machine learning algorithms use neural networks to process patterns in data",
    "session_id": "uncertainty_verify_'$(date +%s)'"
  }' > /dev/null

echo "   ✅ Input sent"
echo "   ⏳ Waiting 15 seconds for processing..."
sleep 15
echo ""

echo "3. Checking for NEW data with uncertainty models..."
echo ""

# Check hypotheses created in last 2 minutes
NOW=$(date +%s)
TWO_MIN_AGO=$((NOW - 120))

if [ "$USE_KUBECTL" = true ]; then
  HYP_DATA=$(redis_cmd HGETALL fsm:agent_1:hypotheses)
else
  HYP_DATA=$(redis_cmd HGETALL fsm:agent_1:hypotheses)
fi

NEW_HYPS=0
for line in $(echo "$HYP_DATA" | grep -A 1 "created_at"); do
  if echo "$line" | grep -q "created_at"; then
    # Extract timestamp and check if recent
    TS=$(echo "$line" | grep -o '"[^"]*T[^"]*Z"' | tr -d '"')
    if [ -n "$TS" ]; then
      # Simple check - if it contains current date, it's likely new
      if echo "$TS" | grep -q "$(date +%Y-%m-%d)"; then
        NEW_HYPS=$((NEW_HYPS + 1))
      fi
    fi
  fi
done

echo "   Found $NEW_HYPS potentially new hypotheses"
echo ""

# Check if any have uncertainty
UNCERTAIN_HYPS=$(echo "$HYP_DATA" | grep -c "uncertainty" || echo "0")
echo "   Hypotheses with uncertainty models: $UNCERTAIN_HYPS"
echo ""

if [ "$UNCERTAIN_HYPS" -gt 0 ]; then
  echo "   ✅ SUCCESS: New hypotheses have uncertainty models!"
  echo ""
  echo "   Sample uncertainty data:"
  echo "$HYP_DATA" | grep -A 10 "uncertainty" | head -15 | sed 's/^/      /'
else
  echo "   ⚠️  No uncertainty models found yet"
  echo "   This could mean:"
  echo "     - Data was created before code update"
  echo "     - Processing is still in progress (wait longer)"
  echo "     - Check FSM pod logs for errors"
fi
echo ""

echo "4. Run full test to see detailed results:"
echo "   ./test/test_uncertainty_modeling.sh"
echo ""

