#!/bin/bash
# Quick fix script to unpause autonomy and reset stuck states

set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

NAMESPACE=${K8S_NAMESPACE:-agi}

echo -e "${BLUE}=== Autonomy Fix Script ===${NC}\n"

# 1. Unpause autonomy
echo -e "${YELLOW}1. Unpausing autonomy...${NC}"
kubectl exec -n $NAMESPACE deployment/redis -- redis-cli DEL auto_executor:paused 2>/dev/null || true
echo -e "${GREEN}✅ Autonomy unpaused${NC}"

# 2. Check and reset stuck active goals
echo -e "${YELLOW}2. Checking for stuck active goals...${NC}"
DOMAINS=$(kubectl exec -n $NAMESPACE deployment/redis -- redis-cli KEYS "reasoning:curiosity_goals:*" 2>/dev/null | grep -v "^$" || echo "")
if [ -n "$DOMAINS" ]; then
    for domain_key in $DOMAINS; do
        echo "   Checking $domain_key..."
        # Get goals and check for stuck active ones (older than 1 hour)
        GOALS=$(kubectl exec -n $NAMESPACE deployment/redis -- redis-cli LRANGE "$domain_key" 0 199 2>/dev/null || echo "")
        if [ -n "$GOALS" ]; then
            # Count active goals
            ACTIVE_COUNT=$(echo "$GOALS" | jq -r 'select(.status == "active")' 2>/dev/null | wc -l || echo "0")
            if [ "$ACTIVE_COUNT" -gt 2 ]; then
                echo -e "   ${YELLOW}⚠️  Found $ACTIVE_COUNT active goals (limit is 2)${NC}"
                echo "   Consider resetting old active goals manually if they're stuck"
            fi
        fi
    done
else
    echo "   No goals found"
fi

# 3. Check FSM state
echo -e "${YELLOW}3. Checking FSM state...${NC}"
FSM_POD=$(kubectl get pods -n $NAMESPACE -l app=fsm-server-rpi58 -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || echo "")
if [ -n "$FSM_POD" ]; then
    echo "   FSM pod: $FSM_POD"
    echo "   Recent autonomy logs:"
    kubectl logs -n $NAMESPACE $FSM_POD --tail=20 | grep -i "autonomy\|goal" | tail -5 || echo "   (no relevant logs)"
else
    echo -e "   ${RED}❌ FSM pod not found${NC}"
fi

echo ""
echo -e "${GREEN}=== Fix Complete ===${NC}"
echo ""
echo "Next steps:"
echo "1. Run the diagnostic script: ./scripts/diagnose_autonomy.sh"
echo "2. Check FSM logs: kubectl logs -n $NAMESPACE -l app=fsm-server-rpi58 --tail=50"
echo "3. Monitor goals: kubectl exec -n $NAMESPACE deployment/redis -- redis-cli LRANGE reasoning:curiosity_goals:General 0 4"





