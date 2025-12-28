#!/bin/bash

# Simple script to trigger FSM to generate new data with uncertainty models

FSM_URL="${FSM_URL:-http://localhost:8083}"

echo "🔄 Triggering FSM to generate new data with uncertainty models"
echo "=============================================================="
echo ""

# Method 1: Send input to trigger learning flow (which generates hypotheses)
echo "1. Sending input to trigger learning flow..."
curl -s -X POST "$FSM_URL/input" \
  -H "Content-Type: application/json" \
  -d '{
    "input": "Artificial intelligence uses machine learning algorithms to process data and make decisions",
    "session_id": "uncertainty_test_'$(date +%s)'"
  }' > /dev/null

if [ $? -eq 0 ]; then
  echo "   ✅ Input sent successfully"
  echo "   ⏳ Waiting 10 seconds for processing..."
  sleep 10
else
  echo "   ⚠️  Failed to send input (FSM server may not be running)"
fi
echo ""

# Method 2: Check if autonomy is enabled and can be triggered
echo "2. Checking FSM status..."
STATUS=$(curl -s "$FSM_URL/status" 2>/dev/null)
if [ $? -eq 0 ]; then
  echo "   ✅ FSM server is running"
  echo "   Current state: $(echo "$STATUS" | grep -o '"current_state":"[^"]*"' | cut -d'"' -f4 || echo "unknown")"
else
  echo "   ⚠️  Cannot reach FSM server at $FSM_URL"
  echo "   Make sure FSM server is running:"
  echo "     cd fsm && ./fsm-server"
fi
echo ""

echo "3. Now run the uncertainty test:"
echo "   ./test/test_uncertainty_modeling.sh"
echo ""
echo "Or check Redis directly:"
echo "   redis-cli HGETALL fsm:agent_1:hypotheses | grep -A 20 uncertainty"
echo "   redis-cli LRANGE reasoning:curiosity_goals:General 0 0 | jq '.uncertainty'"

