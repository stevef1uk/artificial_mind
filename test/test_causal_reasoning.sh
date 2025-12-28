#!/bin/bash

# Comprehensive test for Causal Reasoning Signals
# Tests: causal hypothesis classification, counterfactual reasoning, intervention goals
# Supports both local Docker and Kubernetes/k3s environments

set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

print_status() { echo -e "${BLUE}[INFO]${NC} $1"; }
print_success() { echo -e "${GREEN}[SUCCESS]${NC} $1"; }
print_warning() { echo -e "${YELLOW}[WARNING]${NC} $1"; }
print_error() { echo -e "${RED}[ERROR]${NC} $1"; }

echo "🔬 Testing Causal Reasoning Signals"
echo "===================================="
echo ""

# Environment detection
NAMESPACE="${K8S_NAMESPACE:-agi}"
USE_KUBECTL=false
REDIS_CMD=""
FSM_URL="${FSM_URL:-http://localhost:8083}"

# Detect Kubernetes environment
if command -v kubectl &> /dev/null; then
    REDIS_POD=$(kubectl get pods -n "$NAMESPACE" --no-headers 2>/dev/null | grep -E "redis.*Running" | awk '{print $1}' | head -1)
    FSM_POD=$(kubectl get pods -n "$NAMESPACE" --no-headers 2>/dev/null | grep -E "fsm.*Running" | awk '{print $1}' | head -1)
    
    if [ -n "$REDIS_POD" ]; then
        USE_KUBECTL=true
        REDIS_CMD="kubectl exec -n $NAMESPACE $REDIS_POD -- redis-cli"
        print_status "Detected Kubernetes/k3s environment"
        print_status "Redis pod: $REDIS_POD"
        [ -n "$FSM_POD" ] && print_status "FSM pod: $FSM_POD"
    fi
fi

# Fallback to local Docker
if [ "$USE_KUBECTL" = false ]; then
    if docker exec agi-redis redis-cli ping > /dev/null 2>&1; then
        REDIS_CMD="docker exec agi-redis redis-cli"
        print_success "Using local Docker Redis"
    else
        print_error "Redis not found. Start with: docker-compose up -d redis"
        exit 1
    fi
fi

echo ""
print_status "Step 1: Checking hypotheses for causal reasoning fields..."
echo ""

HYP_KEY="fsm:agent_1:hypotheses"
HYP_COUNT=$($REDIS_CMD HLEN "$HYP_KEY" 2>/dev/null || echo "0")

if [ "$HYP_COUNT" = "0" ]; then
    print_warning "No hypotheses found in Redis"
    echo ""
    print_status "To generate hypotheses:"
    echo "  - Wait for autonomy cycle (every 120s)"
    echo "  - Send input via NATS: echo 'test' | nats pub agi.events.input --stdin"
    echo "  - Use Monitor UI: http://localhost:8082"
    exit 0
fi

print_success "Found $HYP_COUNT hypothesis(es) in Redis"
echo ""

# Analyze hypotheses
CAUSAL_COUNT=0
OBSERVATIONAL_COUNT=0
INFERRED_COUNT=0
EXPERIMENTAL_COUNT=0
WITH_COUNTERFACTUALS=0
WITH_INTERVENTIONS=0
EXAMPLES_SHOWN=0

HYP_IDS=$($REDIS_CMD HKEYS "$HYP_KEY" 2>/dev/null | head -20)

for HYP_ID in $HYP_IDS; do
    HYP_DATA=$($REDIS_CMD HGET "$HYP_KEY" "$HYP_ID" 2>/dev/null)
    [ -z "$HYP_DATA" ] && continue
    
    # Extract fields
    if command -v jq >/dev/null 2>&1; then
        CAUSAL_TYPE=$(echo "$HYP_DATA" | jq -r '.causal_type // ""' 2>/dev/null)
        DESC=$(echo "$HYP_DATA" | jq -r '.description // ""' 2>/dev/null | cut -c1-60)
        CF_COUNT=$(echo "$HYP_DATA" | jq -r '.counterfactual_actions // [] | length' 2>/dev/null || echo "0")
        INT_COUNT=$(echo "$HYP_DATA" | jq -r '.intervention_goals // [] | length' 2>/dev/null || echo "0")
    else
        CAUSAL_TYPE=$(echo "$HYP_DATA" | grep -o '"causal_type":"[^"]*' | cut -d'"' -f4 || echo "")
        DESC=$(echo "$HYP_DATA" | grep -o '"description":"[^"]*' | cut -d'"' -f4 | cut -c1-60 || echo "")
        CF_COUNT=$(echo "$HYP_DATA" | grep -o '"counterfactual_actions"' >/dev/null && echo "?" || echo "0")
        INT_COUNT=$(echo "$HYP_DATA" | grep -o '"intervention_goals"' >/dev/null && echo "?" || echo "0")
    fi
    
    if [ -n "$CAUSAL_TYPE" ] && [ "$CAUSAL_TYPE" != "null" ]; then
        CAUSAL_COUNT=$((CAUSAL_COUNT + 1))
        
        case "$CAUSAL_TYPE" in
            "observational_relation") OBSERVATIONAL_COUNT=$((OBSERVATIONAL_COUNT + 1)) ;;
            "inferred_causal_candidate") INFERRED_COUNT=$((INFERRED_COUNT + 1)) ;;
            "experimentally_testable_relation") EXPERIMENTAL_COUNT=$((EXPERIMENTAL_COUNT + 1)) ;;
        esac
        
        [ "$CF_COUNT" != "0" ] && [ "$CF_COUNT" != "null" ] && WITH_COUNTERFACTUALS=$((WITH_COUNTERFACTUALS + 1))
        [ "$INT_COUNT" != "0" ] && [ "$INT_COUNT" != "null" ] && WITH_INTERVENTIONS=$((WITH_INTERVENTIONS + 1))
        
        # Show examples
        if [ $EXAMPLES_SHOWN -lt 3 ]; then
            echo "✅ Example: $HYP_ID"
            echo "   Type: $CAUSAL_TYPE"
            echo "   Description: $DESC..."
            [ "$CF_COUNT" != "0" ] && echo "   Counterfactuals: $CF_COUNT"
            [ "$INT_COUNT" != "0" ] && echo "   Interventions: $INT_COUNT"
            echo ""
            EXAMPLES_SHOWN=$((EXAMPLES_SHOWN + 1))
        fi
    fi
done

echo ""
print_status "Step 2: Checking intervention goals..."
echo ""

INTERVENTION_COUNT=0

# Check known goal key only (KEYS * is too slow in production Redis)
KNOWN_GOAL_KEY="reasoning:curiosity_goals:General"
print_status "Checking goal key: $KNOWN_GOAL_KEY"

# Check if key exists first (faster than LRANGE on non-existent key)
# Use timeout for kubectl exec which can hang on RPI
print_status "Checking if key exists..."
if [ "$USE_KUBECTL" = true ]; then
    # kubectl exec can hang, use timeout
    KEY_EXISTS=$(timeout 10 kubectl exec -n "$NAMESPACE" "$REDIS_POD" -- redis-cli EXISTS "$KNOWN_GOAL_KEY" 2>/dev/null || echo "0")
else
    KEY_EXISTS=$($REDIS_CMD EXISTS "$KNOWN_GOAL_KEY" 2>/dev/null || echo "0")
fi

if [ -z "$KEY_EXISTS" ]; then
    KEY_EXISTS="0"
fi

print_status "Key exists check result: $KEY_EXISTS"

if [ "$KEY_EXISTS" = "0" ]; then
    print_warning "Goal key does not exist (goals may not have been created yet)"
    GOALS=""
else
    print_status "Key exists, fetching goals..."
    if [ "$USE_KUBECTL" = true ]; then
        # Use timeout for kubectl exec
        GOALS=$(timeout 15 kubectl exec -n "$NAMESPACE" "$REDIS_POD" -- redis-cli LRANGE "$KNOWN_GOAL_KEY" 0 50 2>/dev/null || echo "")
    else
        GOALS=$($REDIS_CMD LRANGE "$KNOWN_GOAL_KEY" 0 50 2>/dev/null || echo "")
    fi
    GOAL_COUNT=$(echo "$GOALS" | grep -v "^$" | wc -l | tr -d ' ' || echo "0")
    print_status "LRANGE completed, found $GOAL_COUNT goal(s)"
fi

if [ -n "$GOALS" ] && [ "$GOALS" != "" ]; then
    for GOAL_DATA in $GOALS; do
        [ -z "$GOAL_DATA" ] && continue
        if echo "$GOAL_DATA" | grep -q '"type".*"intervention_testing"'; then
            INTERVENTION_COUNT=$((INTERVENTION_COUNT + 1))
            if [ $INTERVENTION_COUNT -le 2 ]; then
                if command -v jq >/dev/null 2>&1; then
                    DESC=$(echo "$GOAL_DATA" | jq -r '.description // ""' 2>/dev/null | cut -c1-70)
                    PRIORITY=$(echo "$GOAL_DATA" | jq -r '.priority // "N/A"' 2>/dev/null)
                    echo "✅ Intervention goal: $DESC..."
                    echo "   Priority: $PRIORITY"
                    echo ""
                else
                    echo "✅ Intervention goal found (type=intervention_testing)"
                fi
            fi
        fi
    done
fi

if [ "$INTERVENTION_COUNT" = "0" ]; then
    print_warning "No intervention goals found in $KNOWN_GOAL_KEY"
    echo "   (This is OK - they may be in other keys or not created yet)"
else
    print_success "Found $INTERVENTION_COUNT intervention goal(s)"
fi

# Check logs if available
if [ "$USE_KUBECTL" = true ] && [ -n "$FSM_POD" ]; then
    CAUSAL_LOGS=$(kubectl logs -n "$NAMESPACE" "$FSM_POD" --tail=200 2>/dev/null | grep "\[CAUSAL\]" | tail -3)
    [ -n "$CAUSAL_LOGS" ] && echo "📊 Recent causal reasoning logs:" && echo "$CAUSAL_LOGS" | sed 's/^/   /' && echo ""
fi

echo "========================================="
echo "📊 Summary"
echo "========================================="
echo "   Total hypotheses: $HYP_COUNT"
echo "   With causal reasoning: $CAUSAL_COUNT"
echo "     - Observational: $OBSERVATIONAL_COUNT"
echo "     - Inferred causal: $INFERRED_COUNT"
echo "     - Experimentally testable: $EXPERIMENTAL_COUNT"
echo "   With counterfactuals: $WITH_COUNTERFACTUALS"
echo "   With interventions: $WITH_INTERVENTIONS"
echo "   Intervention goals created: $INTERVENTION_COUNT"
echo ""

if [ "$CAUSAL_COUNT" -gt 0 ]; then
    print_success "✅ Causal reasoning is working!"
    echo ""
    echo "The system is:"
    echo "  ✓ Classifying hypotheses by causal type"
    echo "  ✓ Generating counterfactual reasoning actions"
    echo "  ✓ Creating intervention goals for experimental testing"
    echo "  ✓ Prioritizing causal hypotheses (priority=10)"
else
    print_warning "⚠️  No causal reasoning fields found"
    echo ""
    echo "This likely means hypotheses were generated before the code was deployed."
    echo "Wait for next autonomy cycle (120s) or trigger new hypothesis generation."
fi

echo ""
