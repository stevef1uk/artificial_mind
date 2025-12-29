#!/bin/bash
# Diagnostic script to check why beliefs and inferences aren't being generated
# Works with or without port forwarding - will use kubectl exec if needed

set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}=== Beliefs & Inferences Diagnostic ===${NC}\n"

# Get namespace from env or default
NAMESPACE=${K8S_NAMESPACE:-agi}
REDIS_HOST=${REDIS_HOST:-redis}
REDIS_PORT=${REDIS_PORT:-6379}

echo -e "${YELLOW}1. Checking Redis for stored beliefs...${NC}"
# Check if there are any beliefs stored in Redis
BELIEF_KEYS=$(kubectl exec -n $NAMESPACE deployment/redis -- redis-cli KEYS "reasoning:beliefs:*" 2>/dev/null || echo "")
if [ -n "$BELIEF_KEYS" ]; then
    echo -e "   ${GREEN}✅ Found belief keys in Redis:${NC}"
    echo "$BELIEF_KEYS" | while read key; do
        COUNT=$(kubectl exec -n $NAMESPACE deployment/redis -- redis-cli LLEN "$key" 2>/dev/null || echo "0")
        echo "   - $key: $COUNT beliefs"
    done
else
    echo -e "   ${RED}❌ No beliefs found in Redis (keys: reasoning:beliefs:*)${NC}"
fi

echo ""
echo -e "${YELLOW}2. Checking if autonomy is paused...${NC}"
PAUSED=$(kubectl exec -n $NAMESPACE deployment/redis -- redis-cli GET auto_executor:paused 2>/dev/null || echo "not_set")
if [ "$PAUSED" = "1" ]; then
    echo -e "   ${RED}❌ AUTONOMY IS PAUSED! This prevents belief/inference generation.${NC}"
    echo "   To unpause: kubectl exec -n $NAMESPACE deployment/redis -- redis-cli DEL auto_executor:paused"
else
    echo -e "   ${GREEN}✅ Autonomy is not paused${NC}"
fi

echo ""
echo -e "${YELLOW}3. Checking FSM autonomy configuration...${NC}"
FSM_POD=$(kubectl get pods -n $NAMESPACE -l app=fsm-server-rpi58 -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || \
          kubectl get pods -n $NAMESPACE -l app=fsm-server -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || echo "")
if [ -n "$FSM_POD" ]; then
    echo "   Found FSM pod: $FSM_POD"
    AUTONOMY=$(kubectl exec -n $NAMESPACE $FSM_POD -- env | grep -E "^FSM_AUTONOMY=|^AUTONOMY=" | grep -v "FSM_AUTONOMY_EVERY" || echo "")
    if echo "$AUTONOMY" | grep -qiE "FSM_AUTONOMY=true|AUTONOMY=true"; then
        echo -e "   ${GREEN}✅ Autonomy is enabled${NC}"
        INTERVAL=$(kubectl exec -n $NAMESPACE $FSM_POD -- env | grep "FSM_AUTONOMY_EVERY" | cut -d'=' -f2 || echo "")
        if [ -n "$INTERVAL" ]; then
            echo "   Autonomy interval: ${INTERVAL}s"
        fi
    else
        echo -e "   ${RED}❌ Autonomy is disabled in FSM!${NC}"
        echo "   This prevents the autonomy cycle from running, which generates beliefs and inferences."
    fi
else
    echo -e "   ${RED}❌ FSM pod not found${NC}"
fi

echo ""
echo -e "${YELLOW}4. Checking Neo4j for Concepts (required for beliefs/inferences)...${NC}"
# Try to find Neo4j pod
NEO4J_POD=$(kubectl get pods -n $NAMESPACE -l app=neo4j -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || \
            kubectl get pods -n $NAMESPACE | grep neo4j | head -1 | awk '{print $1}' || echo "")

if [ -n "$NEO4J_POD" ]; then
    echo "   Found Neo4j pod: $NEO4J_POD"
    
    # Get Neo4j credentials from env or use defaults
    NEO4J_USER=${NEO4J_USER:-neo4j}
    NEO4J_PASS=${NEO4J_PASS:-test1234}
    
    # Check if we can query Neo4j
    CONCEPT_COUNT=$(kubectl exec -n $NAMESPACE $NEO4J_POD -- sh -c \
        "cypher-shell -u $NEO4J_USER -p $NEO4J_PASS 'MATCH (c:Concept) RETURN count(c) as count' 2>/dev/null | tail -1 | tr -d '[:space:]' || echo 'error'" 2>/dev/null || echo "error")
    
    if [ "$CONCEPT_COUNT" != "error" ] && [ -n "$CONCEPT_COUNT" ]; then
        if [ "$CONCEPT_COUNT" -gt 0 ]; then
            echo -e "   ${GREEN}✅ Found $CONCEPT_COUNT Concepts in Neo4j${NC}"
            
            # Check concepts by domain
            echo "   Concepts by domain:"
            kubectl exec -n $NAMESPACE $NEO4J_POD -- sh -c \
                "cypher-shell -u $NEO4J_USER -p $NEO4J_PASS 'MATCH (c:Concept) RETURN coalesce(c.domain, \"(unset)\") as domain, count(c) as count ORDER BY count DESC LIMIT 10' 2>/dev/null | tail -n +2 | head -10" 2>/dev/null || echo "   (unable to query)"
        else
            echo -e "   ${RED}❌ No Concepts found in Neo4j!${NC}"
            echo "   This is likely why no beliefs or inferences are being generated."
            echo "   The system needs to bootstrap knowledge first (e.g., via wiki-bootstrapper)."
        fi
    else
        echo -e "   ${YELLOW}⚠️  Unable to query Neo4j directly${NC}"
        echo "   Trying via HDN API..."
        
        # Try via HDN API
        HDN_POD=$(kubectl get pods -n $NAMESPACE -l app=hdn-server-rpi58 -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || \
                  kubectl get pods -n $NAMESPACE -l app=hdn-server -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || echo "")
        
        if [ -n "$HDN_POD" ]; then
            HDN_URL="http://hdn-server-rpi58.${NAMESPACE}.svc.cluster.local:8080"
            QUERY='{"query": "MATCH (c:Concept) RETURN count(c) as count"}'
            CONCEPT_COUNT_VIA_HDN=$(kubectl exec -n $NAMESPACE $HDN_POD -- sh -c \
                "curl -s -X POST -H 'Content-Type: application/json' -d '$QUERY' $HDN_URL/api/v1/knowledge/query 2>/dev/null | grep -o '\"count\":[0-9]*' | cut -d':' -f2 || echo 'error'" 2>/dev/null || echo "error")
            
            if [ "$CONCEPT_COUNT_VIA_HDN" != "error" ] && [ -n "$CONCEPT_COUNT_VIA_HDN" ]; then
                if [ "$CONCEPT_COUNT_VIA_HDN" -gt 0 ]; then
                    echo -e "   ${GREEN}✅ Found $CONCEPT_COUNT_VIA_HDN Concepts via HDN API${NC}"
                else
                    echo -e "   ${RED}❌ No Concepts found via HDN API!${NC}"
                fi
            else
                echo -e "   ${YELLOW}⚠️  Unable to query via HDN API${NC}"
            fi
        fi
    fi
else
    echo -e "   ${YELLOW}⚠️  Neo4j pod not found${NC}"
fi

echo ""
echo -e "${YELLOW}5. Checking HDN server accessibility (required for querying Neo4j)...${NC}"
HDN_POD=$(kubectl get pods -n $NAMESPACE -l app=hdn-server-rpi58 -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || \
          kubectl get pods -n $NAMESPACE -l app=hdn-server -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || echo "")
if [ -n "$HDN_POD" ]; then
    echo "   Found HDN pod: $HDN_POD"
    HDN_URL="http://hdn-server-rpi58.${NAMESPACE}.svc.cluster.local:8080"
    if kubectl exec -n $NAMESPACE $HDN_POD -- sh -c "curl -s --max-time 2 $HDN_URL/health > /dev/null 2>&1" 2>/dev/null; then
        echo -e "   ${GREEN}✅ HDN server is accessible${NC}"
    else
        echo -e "   ${YELLOW}⚠️  HDN server health check failed${NC}"
    fi
else
    echo -e "   ${RED}❌ HDN pod not found${NC}"
fi

echo ""
echo -e "${YELLOW}6. Checking recent autonomy cycle activity...${NC}"
if [ -n "$FSM_POD" ]; then
    # Check FSM logs for autonomy cycle activity
    RECENT_AUTONOMY=$(kubectl logs -n $NAMESPACE $FSM_POD --tail=50 2>/dev/null | grep -i "autonomy\|belief\|inference" | tail -5 || echo "")
    if [ -n "$RECENT_AUTONOMY" ]; then
        echo "   Recent autonomy/belief/inference activity:"
        echo "$RECENT_AUTONOMY" | while read line; do
            echo "   - $line"
        done
    else
        echo -e "   ${YELLOW}⚠️  No recent autonomy/belief/inference activity in logs${NC}"
    fi
fi

echo ""
echo -e "${YELLOW}7. Checking curiosity goals (triggers belief queries)...${NC}"
DOMAIN=${DOMAIN:-General}
GOAL_KEY="reasoning:curiosity_goals:$DOMAIN"
GOAL_COUNT=$(kubectl exec -n $NAMESPACE deployment/redis -- redis-cli LLEN "$GOAL_KEY" 2>/dev/null || echo "0")
if [ "$GOAL_COUNT" -gt 0 ]; then
    echo -e "   ${GREEN}✅ Found $GOAL_COUNT curiosity goals for domain '$DOMAIN'${NC}"
    
    # Show a few recent goals
    echo "   Recent goals:"
    kubectl exec -n $NAMESPACE deployment/redis -- redis-cli LRANGE "$GOAL_KEY" 0 2 2>/dev/null | while read goal_json; do
        if [ -n "$goal_json" ]; then
            DESC=$(echo "$goal_json" | grep -o '"description":"[^"]*"' | cut -d'"' -f4 || echo "")
            TYPE=$(echo "$goal_json" | grep -o '"type":"[^"]*"' | cut -d'"' -f4 || echo "")
            STATUS=$(echo "$goal_json" | grep -o '"status":"[^"]*"' | cut -d'"' -f4 || echo "")
            if [ -n "$DESC" ]; then
                echo "   - [$STATUS] $TYPE: ${DESC:0:60}..."
            fi
        fi
    done
else
    echo -e "   ${YELLOW}⚠️  No curiosity goals found for domain '$DOMAIN'${NC}"
    echo "   This might indicate the autonomy cycle isn't generating goals."
fi

echo ""
echo -e "${BLUE}=== Summary & Recommendations ===${NC}\n"

ISSUES=0

if [ "$PAUSED" = "1" ]; then
    echo -e "${RED}🔴 ISSUE #$((++ISSUES)): Autonomy is PAUSED${NC}"
    echo "   Fix: kubectl exec -n $NAMESPACE deployment/redis -- redis-cli DEL auto_executor:paused"
    echo ""
fi

if [ -z "$FSM_POD" ] || ! echo "$AUTONOMY" | grep -qiE "FSM_AUTONOMY=true|AUTONOMY=true"; then
    echo -e "${RED}🔴 ISSUE #$((++ISSUES)): Autonomy is disabled in FSM${NC}"
    echo "   Fix: Set FSM_AUTONOMY=true in FSM deployment configuration"
    echo ""
fi

if [ "$CONCEPT_COUNT" = "0" ] || [ "$CONCEPT_COUNT" = "error" ]; then
    echo -e "${RED}🔴 ISSUE #$((++ISSUES)): No Concepts in Neo4j${NC}"
    echo "   This is the most likely cause - beliefs and inferences require Concepts in Neo4j."
    echo "   Solutions:"
    echo "   1. Run wiki-bootstrapper to seed knowledge:"
    echo "      kubectl exec -n $NAMESPACE <hdn-pod> -- /bin/sh -c 'curl -X POST http://localhost:8080/api/v1/tools/tool_wiki_bootstrapper/invoke -H \"Content-Type: application/json\" -d \"{\\\"seeds\\\":\\\"artificial intelligence\\\",\\\"max_depth\\\":1,\\\"max_nodes\\\":50,\\\"domain\\\":\\\"General\\\"}\"'"
    echo "   2. Or trigger autonomy cycle with bootstrap-enabled goals"
    echo ""
fi

if [ "$GOAL_COUNT" = "0" ]; then
    echo -e "${YELLOW}⚠️  WARNING: No curiosity goals found${NC}"
    echo "   The autonomy cycle should be generating goals. Check FSM logs for errors."
    echo ""
fi

if [ "$ISSUES" -eq 0 ]; then
    echo -e "${GREEN}✅ No obvious issues found.${NC}"
    echo "   If beliefs/inferences still aren't being generated:"
    echo "   1. Check FSM logs for errors: kubectl logs -n $NAMESPACE $FSM_POD | grep -i error"
    echo "   2. Verify autonomy cycle is running: kubectl logs -n $NAMESPACE $FSM_POD | grep -i autonomy"
    echo "   3. Check if HDN can query Neo4j: kubectl logs -n $NAMESPACE $HDN_POD | grep -i neo4j"
fi

echo ""

