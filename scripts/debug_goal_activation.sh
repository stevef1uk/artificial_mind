#!/bin/bash
# Deep dive into why goals aren't being activated

set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

NAMESPACE=${K8S_NAMESPACE:-agi}

echo -e "${BLUE}=== Goal Activation Debug ===${NC}\n"

FSM_POD=$(kubectl get pods -n $NAMESPACE -l app=fsm-server-rpi58 -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || echo "")

if [ -z "$FSM_POD" ]; then
    echo -e "${RED}❌ FSM pod not found${NC}"
    exit 1
fi

echo -e "${YELLOW}1. Checking FSM autonomy cycle logs...${NC}"
echo "   Last 5 autonomy cycles:"
kubectl logs -n $NAMESPACE $FSM_POD --tail=200 | grep -E "🤖.*Autonomy|Triggering autonomy cycle" | tail -5

echo ""
echo -e "${YELLOW}2. Checking for capacity/eligibility issues...${NC}"
kubectl logs -n $NAMESPACE $FSM_POD --tail=200 | grep -iE "Processing capacity full|capacity full|No eligible goals|skipping goal selection|cooldown" | tail -10

echo ""
echo -e "${YELLOW}3. Checking goal selection process...${NC}"
kubectl logs -n $NAMESPACE $FSM_POD --tail=200 | grep -iE "Selected curiosity goal|goal selection|LLM selected goal|using heuristic goal" | tail -5

echo ""
echo -e "${YELLOW}4. Checking goal status updates...${NC}"
kubectl logs -n $NAMESPACE $FSM_POD --tail=200 | grep -iE "Updated goal.*status|Marked goal.*as|goal.*active" | tail -10

echo ""
echo -e "${YELLOW}5. Checking Redis for goal processing capacity...${NC}"
# Check if there are any active goals that might be blocking
ACTIVE_COUNT=$(kubectl exec -n $NAMESPACE deployment/redis -- redis-cli LRANGE "reasoning:curiosity_goals:General" 0 199 2>/dev/null | jq -r 'select(.status == "active")' 2>/dev/null | wc -l || echo "0")
echo "   Active goals in Redis: $ACTIVE_COUNT"
echo "   FSM_MAX_ACTIVE_GOALS setting:"
kubectl exec -n $NAMESPACE $FSM_POD -- env | grep FSM_MAX_ACTIVE_GOALS || echo "   (using default: 1)"

echo ""
echo -e "${YELLOW}6. Checking recent goal generation...${NC}"
kubectl logs -n $NAMESPACE $FSM_POD --tail=200 | grep -iE "Generated.*goals|curiosity goals|Added.*new goals" | tail -5

echo ""
echo -e "${YELLOW}7. Checking for errors in autonomy cycle...${NC}"
kubectl logs -n $NAMESPACE $FSM_POD --tail=200 | grep -iE "error|failed|warning" | grep -iE "autonomy|goal|curiosity" | tail -10

echo ""
echo -e "${BLUE}=== Summary ===${NC}"
echo "If you see 'Processing capacity full' - there may be stuck active goals"
echo "If you see 'No eligible goals' - goals may be in cooldown or filtered"
echo "If you see no autonomy logs - autonomy cycle may not be running"





