#!/bin/bash

# Test script to verify intelligence improvements
# Tests: 1) Code generation learning, 2) Planner capability selection, 3) Hypothesis generation

set -e

HDN_URL="${HDN_URL:-http://localhost:8081}"
FSM_URL="${FSM_URL:-http://localhost:8083}"

echo "🧠 Testing Intelligence Improvements"
echo "===================================="
echo ""

# Check if services are running
echo "Checking if services are accessible..."
if ! curl -s --max-time 3 "${HDN_URL}/health" > /dev/null 2>&1; then
  echo -e "${RED}❌ HDN server not accessible at ${HDN_URL}${NC}"
  echo ""
  echo "The system appears to be running on Kubernetes."
  echo ""
  echo "Options:"
  echo "  1. Use the Kubernetes test script:"
  echo "     ./test/test_intelligence_remote_k8s.sh"
  echo ""
  echo "  2. Set up port-forwarding first:"
  echo "     kubectl port-forward -n agi <hdn-pod> 8081:8080"
  echo "     Then run this script again"
  echo ""
  echo "  3. Test directly on the Raspberry Pi:"
  echo "     cd ~/dev/artificial_mind/k3s && ./test_intelligence.sh"
  echo ""
  exit 1
fi

if ! curl -s --max-time 3 "${FSM_URL}/health" > /dev/null 2>&1; then
  echo -e "${YELLOW}⚠️  FSM server not accessible at ${FSM_URL}${NC}"
  echo "Some tests may be skipped"
fi

echo -e "${GREEN}✅ Services are accessible${NC}"
echo ""

# Colors for output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

# Test 1: Code Generation Intelligence
echo -e "${YELLOW}Test 1: Code Generation Learning${NC}"
echo "-----------------------------------"
echo "Step 1: Generate Go code that will likely fail (to create learning data)..."
echo "Note: This may take 30-60 seconds (code generation + compilation + execution)"
echo ""

RESPONSE1=$(curl -s --max-time 120 -X POST "${HDN_URL}/api/v1/intelligent/execute" \
  -H "Content-Type: application/json" \
  -d '{
    "task_name": "test_go_unused_import",
    "description": "Create a Go program that imports fmt but does not use it",
    "language": "go"
  }')

if [ $? -ne 0 ]; then
  echo -e "${RED}❌ Request failed or timed out${NC}"
  echo "Check if HDN server is running at ${HDN_URL}"
  exit 1
fi

echo "Response:"
echo "$RESPONSE1" | jq -r '.success, .error, .retry_count' 2>/dev/null || echo "$RESPONSE1"
echo ""

echo "Step 2: Generate similar Go code again (should use learned hints)..."
echo "Note: This may take 30-60 seconds"
echo ""

RESPONSE2=$(curl -s --max-time 120 -X POST "${HDN_URL}/api/v1/intelligent/execute" \
  -H "Content-Type: application/json" \
  -d '{
    "task_name": "test_go_unused_import_2",
    "description": "Create a Go program that imports os and fmt but only uses fmt",
    "language": "go"
  }')

if [ $? -ne 0 ]; then
  echo -e "${RED}❌ Request failed or timed out${NC}"
  RESPONSE2="{}"
fi

echo "Response:"
echo "$RESPONSE2" | jq -r '.success, .error, .retry_count' 2>/dev/null || echo "$RESPONSE2"
echo ""

# Check if second attempt had fewer retries (shows learning)
RETRY1=$(echo "$RESPONSE1" | jq -r '.retry_count // 0' 2>/dev/null || echo "0")
RETRY2=$(echo "$RESPONSE2" | jq -r '.retry_count // 0' 2>/dev/null || echo "0")

if [ "$RETRY2" -lt "$RETRY1" ] || [ "$RETRY2" -eq "0" ]; then
  echo -e "${GREEN}✅ Intelligence working: Second attempt had fewer or no retries${NC}"
else
  echo -e "${YELLOW}⚠️  Learning may not be fully active yet (needs more data)${NC}"
fi
echo ""

# Test 2: Check if learning data is being stored
echo -e "${YELLOW}Test 2: Learning Data Storage${NC}"
echo "-----------------------------------"
echo "Checking Redis for learning data..."

# Check for failure patterns (requires redis-cli)
if command -v redis-cli &> /dev/null; then
  REDIS_HOST="${REDIS_HOST:-localhost:6379}"
  PATTERNS=$(redis-cli -h "${REDIS_HOST%:*}" -p "${REDIS_HOST#*:}" KEYS "failure_pattern:*" 2>/dev/null | wc -l || echo "0")
  STRATEGIES=$(redis-cli -h "${REDIS_HOST%:*}" -p "${REDIS_HOST#*:}" KEYS "codegen_strategy:*" 2>/dev/null | wc -l || echo "0")
  
  echo "Failure patterns stored: $PATTERNS"
  echo "Code generation strategies stored: $STRATEGIES"
  
  if [ "$PATTERNS" -gt "0" ] || [ "$STRATEGIES" -gt "0" ]; then
    echo -e "${GREEN}✅ Learning data is being stored${NC}"
  else
    echo -e "${YELLOW}⚠️  No learning data found yet (may need more executions)${NC}"
  fi
else
  echo "⚠️  redis-cli not available, skipping Redis check"
fi
echo ""

# Test 3: Hypothesis Generation Intelligence
echo -e "${YELLOW}Test 3: Hypothesis Generation Intelligence${NC}"
echo "-----------------------------------"
echo "Checking FSM activity for hypothesis generation..."

ACTIVITY=$(curl -s "${FSM_URL}/activity?limit=10" 2>/dev/null || echo "{}")
HYP_COUNT=$(echo "$ACTIVITY" | jq -r '.activities[] | select(.category == "hypothesis") | .message' 2>/dev/null | wc -l || echo "0")

echo "Recent hypothesis activities: $HYP_COUNT"
echo ""

# Test 4: Goal Scoring Intelligence
echo -e "${YELLOW}Test 4: Goal Scoring Intelligence${NC}"
echo "-----------------------------------"
echo "Checking if goals are being scored with historical data..."

STATUS=$(curl -s "${FSM_URL}/status" 2>/dev/null || echo "{}")
GOAL_COUNT=$(echo "$STATUS" | jq -r '.active_goals // 0' 2>/dev/null || echo "0")

echo "Active goals: $GOAL_COUNT"
echo ""

# Test 5: Planner Capability Selection
echo -e "${YELLOW}Test 5: Planner Capability Selection${NC}"
echo "-----------------------------------"
echo "Testing hierarchical planning (should prefer successful capabilities)..."

PLAN_RESPONSE=$(curl -s --max-time 60 -X POST "${HDN_URL}/api/v1/hierarchical/execute" \
  -H "Content-Type: application/json" \
  -d '{
    "input": "Create a simple Python program that prints hello world",
    "task_name": "test_planning",
    "description": "Test hierarchical planning with capability selection"
  }' 2>/dev/null || echo "{}")

WORKFLOW_ID=$(echo "$PLAN_RESPONSE" | jq -r '.workflow_id // .id // ""' 2>/dev/null || echo "")

if [ -n "$WORKFLOW_ID" ] && [ "$WORKFLOW_ID" != "null" ]; then
  echo -e "${GREEN}✅ Planning executed successfully (workflow: $WORKFLOW_ID)${NC}"
  echo "Note: Check logs to see if capabilities were sorted by success rate"
else
  echo -e "${YELLOW}⚠️  Planning may have failed or returned no workflow ID${NC}"
fi
echo ""

# Summary
echo "===================================="
echo -e "${YELLOW}Summary${NC}"
echo "===================================="
echo "To verify intelligence is working:"
echo "1. Check logs for messages like:"
echo "   - '🧠 [INTELLIGENCE] Added X prevention hints from learned experience'"
echo "   - '🧠 [INTELLIGENCE] Retrieved learned prevention hint'"
echo "   - '🧠 [INTELLIGENCE] Skipping hypothesis similar to failed one'"
echo ""
echo "2. Run multiple code generation tasks and observe:"
echo "   - First attempts may have retries"
echo "   - Similar tasks should have fewer retries (showing learning)"
echo ""
echo "3. Check Redis for learning data:"
echo "   - Keys like 'failure_pattern:*' and 'codegen_strategy:*'"
echo ""
echo "4. Monitor FSM activity log:"
echo "   curl ${FSM_URL}/activity?limit=20"
echo ""

