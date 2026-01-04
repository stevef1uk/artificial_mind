#!/bin/bash
# Create hypothesis testing goals for existing hypotheses

set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

NAMESPACE=${K8S_NAMESPACE:-agi}

echo -e "${BLUE}=== Creating Hypothesis Testing Goals ===${NC}\n"

# Get all hypotheses
echo -e "${YELLOW}Finding existing hypotheses...${NC}"
HYP_KEYS=$(kubectl exec -n $NAMESPACE deployment/redis -- redis-cli HKEYS "fsm:agent_1:hypotheses" 2>/dev/null || echo "")

if [ -z "$HYP_KEYS" ]; then
    echo -e "${RED}❌ No hypotheses found${NC}"
    exit 1
fi

HYP_COUNT=$(echo "$HYP_KEYS" | wc -l)
echo "Found $HYP_COUNT hypotheses"

# Get pending hypothesis testing goals to avoid duplicates
echo ""
echo -e "${YELLOW}Checking existing hypothesis testing goals...${NC}"
EXISTING_GOALS=$(kubectl exec -n $NAMESPACE deployment/redis -- redis-cli LRANGE "reasoning:curiosity_goals:General" 0 199 2>/dev/null || echo "")
EXISTING_HYP_IDS=$(echo "$EXISTING_GOALS" | jq -r 'select(.type == "hypothesis_testing") | .targets[0]' 2>/dev/null | grep -v null || echo "")

NEW_GOALS=0
DOMAIN="General"
GOAL_KEY="reasoning:curiosity_goals:$DOMAIN"

echo ""
echo -e "${YELLOW}Creating hypothesis testing goals...${NC}"

for hyp_key in $HYP_KEYS; do
    # Get hypothesis data
    HYP_DATA=$(kubectl exec -n $NAMESPACE deployment/redis -- redis-cli HGET "fsm:agent_1:hypotheses" "$hyp_key" 2>/dev/null || echo "")
    
    if [ -z "$HYP_DATA" ]; then
        continue
    fi
    
    HYP_ID=$(echo "$HYP_DATA" | jq -r '.id // ""' 2>/dev/null || echo "")
    HYP_DESC=$(echo "$HYP_DATA" | jq -r '.description // ""' 2>/dev/null || echo "")
    HYP_STATUS=$(echo "$HYP_DATA" | jq -r '.status // ""' 2>/dev/null || echo "")
    
    if [ -z "$HYP_ID" ] || [ -z "$HYP_DESC" ]; then
        continue
    fi
    
    # Skip if already has a testing goal
    if echo "$EXISTING_HYP_IDS" | grep -q "^${HYP_ID}$"; then
        echo "  ⏭️  Skipping $HYP_ID (already has testing goal)"
        continue
    fi
    
    # Skip if not in proposed status (might already be tested)
    if [ "$HYP_STATUS" != "proposed" ]; then
        echo "  ⏭️  Skipping $HYP_ID (status: $HYP_STATUS)"
        continue
    fi
    
    # Create hypothesis testing goal
    GOAL_ID="hyp_test_${HYP_ID}"
    GOAL_DESC="Test hypothesis: ${HYP_DESC}"
    
    # Create goal JSON
    GOAL_JSON=$(jq -n \
        --arg id "$GOAL_ID" \
        --arg type "hypothesis_testing" \
        --arg desc "$GOAL_DESC" \
        --arg hyp_id "$HYP_ID" \
        --arg domain "$DOMAIN" \
        --arg priority "8" \
        --arg status "pending" \
        --arg created_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
        '{
            id: $id,
            type: $type,
            description: $desc,
            targets: [$hyp_id],
            priority: ($priority | tonumber),
            status: $status,
            domain: $domain,
            created_at: $created_at
        }')
    
    # Add to Redis
    kubectl exec -n $NAMESPACE deployment/redis -- redis-cli LPUSH "$GOAL_KEY" "$GOAL_JSON" > /dev/null 2>&1
    kubectl exec -n $NAMESPACE deployment/redis -- redis-cli LTRIM "$GOAL_KEY" 0 199 > /dev/null 2>&1
    
    echo "  ✅ Created goal for: ${HYP_DESC:0:60}..."
    NEW_GOALS=$((NEW_GOALS + 1))
done

echo ""
echo -e "${GREEN}✅ Created $NEW_GOALS new hypothesis testing goals${NC}"
echo ""
echo "Next steps:"
echo "1. Wait for autonomy cycle to select these goals (every 2 minutes)"
echo "2. Goals will test hypotheses"
echo "3. Confirmed hypotheses will create workflows"
echo ""
echo "Monitor with:"
echo "  kubectl logs -n $NAMESPACE -l app=fsm-server-rpi58 -f | grep -iE 'hypothesis.*test|Selected curiosity goal'"





