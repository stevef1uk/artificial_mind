#!/bin/bash
# Script to fix existing beliefs in Redis that only have concept names
# This updates them to use definitions or proper statements instead

set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

NAMESPACE=${K8S_NAMESPACE:-agi}
DOMAIN=${DOMAIN:-General}
ACTION=${1:-update}  # 'update', 'clear', or 'show'

echo -e "${BLUE}=== Fix Existing Beliefs in Redis ===${NC}\n"
echo "Domain: $DOMAIN"
echo "Namespace: $NAMESPACE"
echo "Action: $ACTION"
echo ""

# Check if Redis is accessible
if ! kubectl exec -n $NAMESPACE deployment/redis -- redis-cli PING > /dev/null 2>&1; then
    echo -e "${RED}❌ Cannot connect to Redis${NC}"
    exit 1
fi

BELIEF_KEY="reasoning:beliefs:$DOMAIN"
BELIEF_COUNT=$(kubectl exec -n $NAMESPACE deployment/redis -- redis-cli LLEN "$BELIEF_KEY" 2>/dev/null || echo "0")

if [ "$BELIEF_COUNT" = "0" ]; then
    echo -e "${YELLOW}⚠️  No beliefs found in Redis for domain '$DOMAIN'${NC}"
    exit 0
fi

echo -e "${GREEN}Found $BELIEF_COUNT beliefs in Redis${NC}\n"

if [ "$ACTION" = "show" ]; then
    echo "Sample beliefs (first 10):"
    kubectl exec -n $NAMESPACE deployment/redis -- redis-cli LRANGE "$BELIEF_KEY" 0 9 2>/dev/null | while read belief_json; do
        if [ -n "$belief_json" ]; then
            STATEMENT=$(echo "$belief_json" | grep -o '"statement":"[^"]*"' | cut -d'"' -f4 || echo "")
            SOURCE=$(echo "$belief_json" | grep -o '"source":"[^"]*"' | cut -d'"' -f4 || echo "")
            CONF=$(echo "$belief_json" | grep -o '"confidence":[0-9.]*' | cut -d':' -f2 || echo "")
            if [ -n "$STATEMENT" ]; then
                echo "  - [$SOURCE] (${CONF}): ${STATEMENT:0:80}..."
            fi
        fi
    done
    exit 0
fi

if [ "$ACTION" = "clear" ]; then
    echo -e "${YELLOW}Clearing all beliefs for domain '$DOMAIN'...${NC}"
    kubectl exec -n $NAMESPACE deployment/redis -- redis-cli DEL "$BELIEF_KEY" > /dev/null 2>&1
    echo -e "${GREEN}✅ Cleared $BELIEF_COUNT beliefs${NC}"
    echo "New beliefs will be created with the correct format on the next autonomy cycle."
    exit 0
fi

if [ "$ACTION" = "update" ]; then
    echo -e "${YELLOW}⚠️  Updating beliefs requires querying Neo4j for definitions${NC}"
    echo "This is complex and may not work well. It's recommended to:"
    echo "  1. Clear existing beliefs: $0 clear"
    echo "  2. Let the system regenerate them with the new format"
    echo ""
    read -p "Do you want to proceed with clearing instead? (y/n) " -n 1 -r
    echo
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        echo -e "${YELLOW}Clearing all beliefs for domain '$DOMAIN'...${NC}"
        kubectl exec -n $NAMESPACE deployment/redis -- redis-cli DEL "$BELIEF_KEY" > /dev/null 2>&1
        echo -e "${GREEN}✅ Cleared $BELIEF_COUNT beliefs${NC}"
        echo "New beliefs will be created with the correct format on the next autonomy cycle."
    else
        echo "Aborted."
    fi
    exit 0
fi

echo -e "${RED}❌ Unknown action: $ACTION${NC}"
echo "Usage: $0 [show|clear|update]"
echo "  show  - Show sample beliefs"
echo "  clear - Clear all beliefs (recommended)"
echo "  update - Attempt to update (not recommended)"
exit 1

