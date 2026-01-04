#!/bin/bash
# Force activation of a pending goal (for debugging)

set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

NAMESPACE=${K8S_NAMESPACE:-agi}

echo -e "${BLUE}=== Force Goal Activation ===${NC}\n"

# Get pending goals
echo -e "${YELLOW}Finding pending goals...${NC}"
GOALS=$(kubectl exec -n $NAMESPACE deployment/redis -- redis-cli LRANGE "reasoning:curiosity_goals:General" 0 9 2>/dev/null || echo "")

if [ -z "$GOALS" ] || [ "$GOALS" = "(empty list or set)" ]; then
    echo -e "${RED}❌ No goals found in Redis${NC}"
    exit 1
fi

# Show pending goals
echo ""
echo "Pending goals:"
echo "$GOALS" | jq -r 'select(.status == "pending") | "  [\(.id)] \(.type): \(.description)"' 2>/dev/null | head -5

# Get first pending goal ID
FIRST_PENDING=$(echo "$GOALS" | jq -r 'select(.status == "pending") | .id' 2>/dev/null | head -1)

if [ -z "$FIRST_PENDING" ] || [ "$FIRST_PENDING" = "null" ]; then
    echo -e "${YELLOW}⚠️  No pending goals found${NC}"
    exit 0
fi

echo ""
echo -e "${YELLOW}Activating goal: $FIRST_PENDING${NC}"

# Update goal status to active
KEY="reasoning:curiosity_goals:General"
GOALS_DATA=$(kubectl exec -n $NAMESPACE deployment/redis -- redis-cli LRANGE "$KEY" 0 199 2>/dev/null || echo "")

# Find and update the goal
UPDATED=false
INDEX=0
while IFS= read -r goal_json; do
    GOAL_ID=$(echo "$goal_json" | jq -r '.id' 2>/dev/null || echo "")
    if [ "$GOAL_ID" = "$FIRST_PENDING" ]; then
        # Update status to active
        UPDATED_GOAL=$(echo "$goal_json" | jq '.status = "active"' 2>/dev/null)
        kubectl exec -n $NAMESPACE deployment/redis -- redis-cli LSET "$KEY" $INDEX "$UPDATED_GOAL" > /dev/null 2>&1
        UPDATED=true
        echo -e "${GREEN}✅ Goal activated${NC}"
        break
    fi
    INDEX=$((INDEX + 1))
done <<< "$GOALS_DATA"

if [ "$UPDATED" = "false" ]; then
    echo -e "${RED}❌ Failed to update goal${NC}"
    exit 1
fi

echo ""
echo -e "${BLUE}Next steps:${NC}"
echo "1. Check if FSM picks up the active goal"
echo "2. Monitor FSM logs: kubectl logs -n $NAMESPACE -l app=fsm-server-rpi58 -f | grep -i goal"
echo "3. Check goal execution: ./scripts/diagnose_autonomy.sh"





