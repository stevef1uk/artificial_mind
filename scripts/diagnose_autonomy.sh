#!/bin/bash
# Diagnostic script to check why the system isn't creating workflows/goals/artifacts
# Works with or without port forwarding - will use kubectl exec if needed

set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}=== Autonomy & Learning Diagnostic ===${NC}\n"

# Get namespace from env or default
NAMESPACE=${K8S_NAMESPACE:-agi}
REDIS_HOST=${REDIS_HOST:-redis}
REDIS_PORT=${REDIS_PORT:-6379}

echo -e "${YELLOW}1. Checking Redis flags...${NC}"
echo "   Checking auto_executor:paused..."
PAUSED=$(kubectl exec -n $NAMESPACE deployment/redis -- redis-cli GET auto_executor:paused 2>/dev/null || echo "not_set")
if [ "$PAUSED" = "1" ]; then
    echo -e "   ${RED}❌ AUTONOMY IS PAUSED! This is why nothing is happening.${NC}"
    echo "   To unpause: kubectl exec -n $NAMESPACE deployment/redis -- redis-cli DEL auto_executor:paused"
else
    echo -e "   ${GREEN}✅ Autonomy is not paused${NC}"
fi

echo ""
echo -e "${YELLOW}2. Checking FSM configuration...${NC}"
# Try multiple label selectors as fallback
FSM_POD=$(kubectl get pods -n $NAMESPACE -l app=fsm-server-rpi58 -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || \
          kubectl get pods -n $NAMESPACE -l app=fsm-server -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || echo "")
if [ -n "$FSM_POD" ]; then
    echo "   Found FSM pod: $FSM_POD"
    echo "   Checking autonomy setting..."
    AUTONOMY=$(kubectl exec -n $NAMESPACE $FSM_POD -- env | grep -E "FSM_AUTONOMY|AUTONOMY" || echo "not_found")
    # Check for FSM_AUTONOMY=true or AUTONOMY=true (but not FSM_AUTONOMY_EVERY)
    if echo "$AUTONOMY" | grep -qiE "FSM_AUTONOMY=true|AUTONOMY=true" && ! echo "$AUTONOMY" | grep -qiE "FSM_AUTONOMY=false|AUTONOMY=false"; then
        echo -e "   ${GREEN}✅ Autonomy is enabled${NC}"
        # Show the interval if set
        INTERVAL=$(echo "$AUTONOMY" | grep "FSM_AUTONOMY_EVERY" | cut -d'=' -f2 || echo "")
        if [ -n "$INTERVAL" ]; then
            echo "   Autonomy interval: ${INTERVAL}s"
        fi
    else
        echo -e "   ${RED}❌ Autonomy is disabled in FSM!${NC}"
    fi
    
    echo "   Checking DISABLE_BACKGROUND_LLM..."
    DISABLE_BG=$(kubectl exec -n $NAMESPACE $FSM_POD -- env | grep "DISABLE_BACKGROUND_LLM" || echo "")
    if echo "$DISABLE_BG" | grep -qi "1\|true"; then
        echo -e "   ${RED}❌ Background LLM is DISABLED! This prevents goal execution.${NC}"
    else
        echo -e "   ${GREEN}✅ Background LLM is enabled${NC}"
    fi
else
    echo -e "   ${RED}❌ FSM pod not found${NC}"
fi

echo ""
echo -e "${YELLOW}3. Checking FSM state and activity...${NC}"
# Try localhost first (port forwarding), then service DNS, then kubectl exec
FSM_URL=${FSM_URL:-http://localhost:8083}
FSM_SVC_URL="http://fsm-server-rpi58.${NAMESPACE}.svc.cluster.local:8083"

# Check if accessible via localhost
if curl -s --max-time 2 "$FSM_URL/health" > /dev/null 2>&1; then
    echo "   Using localhost (port forwarding detected)"
    FSM_ACCESS_URL="$FSM_URL"
elif [ -n "$FSM_POD" ]; then
    # Try accessing via service DNS from within cluster
    if kubectl exec -n $NAMESPACE $FSM_POD -- sh -c "curl -s --max-time 2 $FSM_SVC_URL/health > /dev/null 2>&1" 2>/dev/null; then
        echo "   Using service DNS from within cluster"
        FSM_ACCESS_URL="$FSM_SVC_URL"
        USE_KUBECTL_EXEC=true
    else
        # Use kubectl exec to run curl inside the pod
        echo "   Using kubectl exec (no port forwarding)"
        FSM_ACCESS_URL="http://localhost:8083"
        USE_KUBECTL_EXEC=true
    fi
else
    FSM_ACCESS_URL=""
    USE_KUBECTL_EXEC=false
fi

if [ -n "$FSM_ACCESS_URL" ]; then
    if [ "$USE_KUBECTL_EXEC" = "true" ] && [ -n "$FSM_POD" ]; then
        echo "   Current FSM state:"
        # Try accessing via localhost inside pod (container port 8083)
        STATE_RAW=$(kubectl exec -n $NAMESPACE $FSM_POD -- sh -c "curl -s --max-time 2 http://localhost:8083/thinking 2>/dev/null" 2>/dev/null || echo "")
        if [ -n "$STATE_RAW" ]; then
            STATE=$(echo "$STATE_RAW" | jq -r '.current_state // "unknown"' 2>/dev/null || echo "unknown")
        else
            # Try service DNS as fallback
            STATE_RAW=$(kubectl exec -n $NAMESPACE $FSM_POD -- sh -c "curl -s --max-time 2 $FSM_SVC_URL/thinking 2>/dev/null" 2>/dev/null || echo "")
            STATE=$(echo "$STATE_RAW" | jq -r '.current_state // "unknown"' 2>/dev/null || echo "unknown")
        fi
        echo -e "   ${BLUE}State: $STATE${NC}"
        
        echo "   Recent activity (last 5):"
        ACTIVITY=$(kubectl exec -n $NAMESPACE $FSM_POD -- sh -c "curl -s --max-time 2 http://localhost:8083/activity?limit=5 2>/dev/null" 2>/dev/null || echo "")
        if [ -n "$ACTIVITY" ]; then
            echo "$ACTIVITY" | jq -r '.activities[]? | "   \(.timestamp) - \(.message)"' 2>/dev/null || echo "   (unable to parse: $ACTIVITY)"
        else
            echo "   (unable to fetch - FSM may not be responding)"
        fi
    else
        echo "   Current FSM state:"
        STATE=$(curl -s "$FSM_ACCESS_URL/thinking" 2>/dev/null | jq -r '.current_state // "unknown"' 2>/dev/null || echo "unknown")
        echo -e "   ${BLUE}State: $STATE${NC}"
        
        echo "   Recent activity (last 5):"
        curl -s "$FSM_ACCESS_URL/activity?limit=5" 2>/dev/null | jq -r '.activities[]? | "   \(.timestamp) - \(.message)"' 2>/dev/null || echo "   (unable to fetch)"
    fi
else
    echo -e "   ${YELLOW}⚠️  FSM server not accessible${NC}"
    echo "   💡 Tip: Set up port forwarding with:"
    echo "      kubectl port-forward -n $NAMESPACE svc/fsm-server-rpi58 8083:8083 &"
fi

# Always check FSM logs for autonomy activity
if [ -n "$FSM_POD" ]; then
    echo ""
    echo "   Recent FSM autonomy logs (last 15 lines):"
    kubectl logs -n $NAMESPACE $FSM_POD --tail=50 | grep -iE "autonomy|goal|capacity|eligible|selected" | tail -15 || echo "   (no relevant logs found)"
fi

echo ""
echo -e "${YELLOW}4. Checking goals in Redis...${NC}"
DOMAIN=$(kubectl exec -n $NAMESPACE deployment/redis -- redis-cli LRANGE "reasoning:curiosity_goals:General" 0 4 2>/dev/null | head -5 || echo "")
if [ -n "$DOMAIN" ] && [ "$DOMAIN" != "(empty list or set)" ]; then
    echo -e "   ${GREEN}✅ Goals exist in Redis${NC}"
    echo "   Sample goal:"
    echo "$DOMAIN" | head -1 | jq -r '.description // .' 2>/dev/null || echo "$DOMAIN" | head -1
else
    echo -e "   ${YELLOW}⚠️  No goals found in Redis (reasoning:curiosity_goals:General)${NC}"
fi

echo ""
echo -e "${YELLOW}5. Checking active goals status...${NC}"
ALL_GOALS=$(kubectl exec -n $NAMESPACE deployment/redis -- redis-cli LRANGE "reasoning:curiosity_goals:General" 0 199 2>/dev/null || echo "")
ACTIVE_GOALS=$(echo "$ALL_GOALS" | jq -r 'select(.status == "active")' 2>/dev/null | wc -l || echo "0")
PENDING_GOALS=$(echo "$ALL_GOALS" | jq -r 'select(.status == "pending")' 2>/dev/null | wc -l || echo "0")
COMPLETED_GOALS=$(echo "$ALL_GOALS" | jq -r 'select(.status == "completed")' 2>/dev/null | wc -l || echo "0")

echo "   Active goals: $ACTIVE_GOALS"
echo "   Pending goals: $PENDING_GOALS"
echo "   Completed goals: $COMPLETED_GOALS"

if [ "$ACTIVE_GOALS" = "0" ] && [ "$PENDING_GOALS" -gt 0 ]; then
    echo -e "   ${YELLOW}⚠️  No active goals but $PENDING_GOALS pending - goals may be filtered out${NC}"
    echo "   Checking why goals aren't being activated..."
    
    # Check processing capacity
    echo "   Checking processing capacity limits..."
    if [ -n "$FSM_POD" ]; then
        kubectl logs -n $NAMESPACE $FSM_POD --tail=100 | grep -iE "Processing capacity full|capacity full|eligible|cooldown" | tail -5 || echo "   (no capacity messages found)"
    fi
    
    # Check goal eligibility (cooldowns)
    echo "   Sample pending goals:"
    echo "$ALL_GOALS" | jq -r 'select(.status == "pending") | "   - \(.type): \(.description)"' 2>/dev/null | head -3 || echo "   (unable to parse)"
fi

echo ""
echo -e "${YELLOW}6. Checking workflows in Redis...${NC}"
WORKFLOWS=$(kubectl exec -n $NAMESPACE deployment/redis -- redis-cli LRANGE "fsm:agent_1:workflows" 0 4 2>/dev/null | head -5 || echo "")
if [ -n "$WORKFLOWS" ] && [ "$WORKFLOWS" != "(empty list or set)" ]; then
    echo -e "   ${GREEN}✅ Workflows exist in Redis${NC}"
    echo "   Sample workflow:"
    echo "$WORKFLOWS" | head -1 | jq -r '.name // .id // .' 2>/dev/null || echo "$WORKFLOWS" | head -1
else
    echo -e "   ${YELLOW}⚠️  No workflows found in Redis${NC}"
fi

echo ""
echo -e "${YELLOW}7. Checking what GPU is doing...${NC}"
# Try multiple label selectors as fallback
HDN_POD=$(kubectl get pods -n $NAMESPACE -l app=hdn-server-rpi58 -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || \
          kubectl get pods -n $NAMESPACE -l app=hdn-server -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || echo "")
if [ -n "$HDN_POD" ]; then
    echo "   Found HDN pod: $HDN_POD"
    echo "   Recent HDN logs (last 10 lines with LLM/async):"
    kubectl logs -n $NAMESPACE $HDN_POD --tail=50 | grep -iE "llm|async|request|processing" | tail -10 || echo "   (no relevant logs)"
else
    echo -e "   ${YELLOW}⚠️  HDN pod not found${NC}"
fi

echo ""
echo -e "${YELLOW}8. Checking async LLM queue status...${NC}"
# Try localhost first (port forwarding), then service DNS, then kubectl exec
HDN_URL=${HDN_URL:-http://localhost:8081}
HDN_SVC_URL="http://hdn-server-rpi58.${NAMESPACE}.svc.cluster.local:8080"

# Check if accessible via localhost
if curl -s --max-time 2 "$HDN_URL/health" > /dev/null 2>&1; then
    echo "   Using localhost (port forwarding detected)"
    HDN_ACCESS_URL="$HDN_URL"
    USE_HDN_KUBECTL_EXEC=false
elif [ -n "$HDN_POD" ]; then
    # Try accessing via service DNS from within cluster
    if kubectl exec -n $NAMESPACE $HDN_POD -- sh -c "curl -s --max-time 2 $HDN_SVC_URL/health > /dev/null 2>&1" 2>/dev/null; then
        echo "   Using service DNS from within cluster"
        HDN_ACCESS_URL="$HDN_SVC_URL"
        USE_HDN_KUBECTL_EXEC=true
    else
        # Use kubectl exec to run curl inside the pod
        echo "   Using kubectl exec (no port forwarding)"
        HDN_ACCESS_URL="http://localhost:8080"
        USE_HDN_KUBECTL_EXEC=true
    fi
else
    HDN_ACCESS_URL=""
    USE_HDN_KUBECTL_EXEC=false
fi

if [ -n "$HDN_ACCESS_URL" ]; then
    if [ "$USE_HDN_KUBECTL_EXEC" = "true" ] && [ -n "$HDN_POD" ]; then
        # Try localhost:8080 inside the pod first (container port)
        if kubectl exec -n $NAMESPACE $HDN_POD -- sh -c "curl -s --max-time 2 http://localhost:8080/health > /dev/null 2>&1" 2>/dev/null; then
            echo "   ✅ HDN server is accessible via kubectl exec (localhost:8080)"
            echo "   (Queue metrics endpoint may not be available)"
        elif kubectl exec -n $NAMESPACE $HDN_POD -- sh -c "curl -s --max-time 2 $HDN_SVC_URL/health > /dev/null 2>&1" 2>/dev/null; then
            echo "   ✅ HDN server is accessible via service DNS"
            echo "   (Queue metrics endpoint may not be available)"
        else
            echo -e "   ${YELLOW}⚠️  HDN server health check failed (tried localhost:8080 and service DNS)${NC}"
            echo "   Checking if HDN pod is running..."
            kubectl get pod -n $NAMESPACE $HDN_POD -o jsonpath='{.status.phase}' 2>/dev/null || echo "   (unable to check pod status)"
        fi
    else
        if curl -s --max-time 2 "$HDN_ACCESS_URL/health" > /dev/null 2>&1; then
            echo "   ✅ HDN server is accessible"
            echo "   (Queue metrics endpoint may not be available)"
        else
            echo -e "   ${YELLOW}⚠️  HDN server health check failed${NC}"
        fi
    fi
else
    echo -e "   ${YELLOW}⚠️  HDN server not accessible${NC}"
    echo "   💡 Tip: Set up port forwarding with:"
    echo "      kubectl port-forward -n $NAMESPACE svc/hdn-server-rpi58 8081:8080 &"
fi

echo ""
echo -e "${BLUE}=== Summary ===${NC}"
echo ""
if [ "$PAUSED" = "1" ]; then
    echo -e "${RED}🔴 CRITICAL: Autonomy is PAUSED - this is the main issue!${NC}"
    echo "   Fix: kubectl exec -n $NAMESPACE deployment/redis -- redis-cli DEL auto_executor:paused"
    echo ""
fi

if echo "$DISABLE_BG" | grep -qi "1\|true"; then
    echo -e "${RED}🔴 CRITICAL: Background LLM is DISABLED - goals won't execute!${NC}"
    echo "   Fix: Remove or set DISABLE_BACKGROUND_LLM=0 in FSM deployment"
    echo ""
fi

if [ "$ACTIVE_GOALS" = "0" ] && [ "$PAUSED" != "1" ]; then
    echo -e "${YELLOW}⚠️  WARNING: No active goals found${NC}"
    echo "   This could mean:"
    echo "   - Goals are being generated but not selected/activated"
    echo "   - Processing capacity is full"
    echo "   - All goals are in cooldown"
    echo ""
fi

echo -e "${GREEN}Diagnostic complete!${NC}"

