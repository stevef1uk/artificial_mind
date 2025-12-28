#!/bin/bash

# Test script for Causal Reasoning Signals
# This script tests the new causal reasoning features:
# - Causal hypothesis classification
# - Counterfactual reasoning actions
# - Intervention-style goals
# Supports both local Docker and Kubernetes/k3s environments

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

print_status() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

print_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

print_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

echo "🔬 Testing Causal Reasoning Signals"
echo "===================================="
echo ""

# Detect environment (local Docker vs Kubernetes/k3s)
NAMESPACE="${K8S_NAMESPACE:-agi}"
USE_KUBECTL=false
REDIS_CMD=""
FSM_URL="${FSM_URL:-http://localhost:8083}"
HDN_URL="${HDN_URL:-http://localhost:8081}"

# Check for kubectl and Kubernetes environment
if command -v kubectl &> /dev/null; then
    REDIS_POD=$(kubectl get pods -n "$NAMESPACE" -l app=redis -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
    if [ -n "$REDIS_POD" ]; then
        # Try alternative label patterns
        if [ -z "$REDIS_POD" ]; then
            REDIS_POD=$(kubectl get pods -n "$NAMESPACE" --no-headers 2>/dev/null | grep -E "redis.*Running" | awk '{print $1}' | head -1)
        fi
    fi
    
    if [ -n "$REDIS_POD" ]; then
        USE_KUBECTL=true
        REDIS_CMD="kubectl exec -n $NAMESPACE $REDIS_POD -- redis-cli"
        print_status "Detected Kubernetes/k3s environment"
        print_status "Using Redis pod: $REDIS_POD"
        
        # Get FSM and HDN service URLs (may need port-forward)
        FSM_POD=$(kubectl get pods -n "$NAMESPACE" -l app=fsm-server -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
        if [ -z "$FSM_POD" ]; then
            FSM_POD=$(kubectl get pods -n "$NAMESPACE" --no-headers 2>/dev/null | grep -E "fsm.*Running" | awk '{print $1}' | head -1)
        fi
        
        HDN_POD=$(kubectl get pods -n "$NAMESPACE" -l app=hdn-server -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
        if [ -z "$HDN_POD" ]; then
            HDN_POD=$(kubectl get pods -n "$NAMESPACE" --no-headers 2>/dev/null | grep -E "hdn.*Running" | awk '{print $1}' | head -1)
        fi
        
        # Check if services are accessible, if not suggest port-forward
        if ! curl -s --max-time 2 "${FSM_URL}/health" > /dev/null 2>&1; then
            print_warning "FSM service not accessible at $FSM_URL"
            print_status "You may need to port-forward:"
            echo "   kubectl port-forward -n $NAMESPACE svc/fsm-server 8083:8083 &"
            FSM_URL="http://localhost:8083"
        fi
        
        if ! curl -s --max-time 2 "${HDN_URL}/health" > /dev/null 2>&1; then
            print_warning "HDN service not accessible at $HDN_URL"
            print_status "You may need to port-forward:"
            echo "   kubectl port-forward -n $NAMESPACE svc/hdn-server 8081:8081 &"
            HDN_URL="http://localhost:8081"
        fi
    fi
fi

# Fallback to local Docker
if [ "$USE_KUBECTL" = false ]; then
    print_status "Checking Redis connection (local Docker)..."
    if docker exec agi-redis redis-cli ping > /dev/null 2>&1; then
        print_success "Redis is running (Docker)"
        REDIS_CMD="docker exec agi-redis redis-cli"
    else
        print_error "Redis is not running. Please start Redis first."
        echo "   Try: docker-compose up -d redis"
        exit 1
    fi
fi

# Check if FSM server is running
print_status "Checking FSM server..."
if curl -s --max-time 2 "${FSM_URL}/health" > /dev/null 2>&1; then
    print_success "FSM server is running at $FSM_URL"
    if [ "$USE_KUBECTL" = true ] && [ -n "$FSM_POD" ]; then
        FSM_STATUS=$(kubectl get pod "$FSM_POD" -n "$NAMESPACE" -o jsonpath='{.status.phase}' 2>/dev/null)
        echo "   Pod: $FSM_POD ($FSM_STATUS)"
    fi
else
    print_warning "FSM server is not accessible at $FSM_URL"
    if [ "$USE_KUBECTL" = false ]; then
        echo "   You may need to start it manually:"
        echo "   cd fsm && go run . -config config/artificial_mind.yaml"
    else
        echo "   You may need to port-forward:"
        echo "   kubectl port-forward -n $NAMESPACE svc/fsm-server 8083:8083 &"
    fi
    echo ""
    read -p "Press Enter to continue (will test with Redis only) or Ctrl+C to exit..."
fi

# Check if HDN server is running (needed for hypothesis generation)
print_status "Checking HDN server..."
if curl -s --max-time 2 "${HDN_URL}/health" > /dev/null 2>&1; then
    print_success "HDN server is running at $HDN_URL"
    HDN_RUNNING=true
    if [ "$USE_KUBECTL" = true ] && [ -n "$HDN_POD" ]; then
        HDN_STATUS=$(kubectl get pod "$HDN_POD" -n "$NAMESPACE" -o jsonpath='{.status.phase}' 2>/dev/null)
        echo "   Pod: $HDN_POD ($HDN_STATUS)"
    fi
else
    print_warning "HDN server is not accessible at $HDN_URL. Some features may not work."
    HDN_RUNNING=false
fi

echo ""
print_status "Step 1: Triggering hypothesis generation..."
echo ""

# FSM receives input via NATS events, not HTTP
# We can either:
# 1. Check existing hypotheses (they may already exist)
# 2. Trigger autonomy cycle (if enabled)
# 3. Send NATS event (requires nats CLI)

print_status "FSM receives input via NATS events, not HTTP"
print_status "Checking if we can trigger hypothesis generation..."

# Option 1: Check if autonomy is enabled and trigger it
if curl -s http://localhost:8083/status > /dev/null 2>&1; then
    print_status "FSM status endpoint available, checking autonomy..."
    # Autonomy cycle will trigger hypothesis generation if there are facts
fi

# Option 2: Try to send NATS event (if nats CLI is available)
if command -v nats >/dev/null 2>&1; then
    print_status "NATS CLI available, sending test event..."
    TEST_INPUT="If we optimize machine learning algorithms, we can improve prediction accuracy. Neural networks enable deep learning capabilities."
    
    echo "$TEST_INPUT" | nats pub agi.events.input --stdin 2>/dev/null
    if [ $? -eq 0 ]; then
        print_success "NATS event sent"
        print_status "Waiting for hypothesis generation (15 seconds)..."
        sleep 15
    else
        print_warning "Failed to send NATS event (NATS may not be accessible)"
    fi
else
    print_warning "NATS CLI not available. Install it to send events:"
    echo "   brew install nats-io/nats-tools/nats"
    echo "   Or check existing hypotheses below"
fi

echo ""
print_status "Step 2: Checking hypotheses via FSM API..."
echo ""

# First, try to get hypotheses from FSM API endpoint
HYPOTHESES_JSON=""
if curl -s --max-time 5 "${FSM_URL}/hypotheses" > /dev/null 2>&1; then
    print_status "Fetching hypotheses from FSM API at $FSM_URL..."
    HYPOTHESES_JSON=$(curl -s --max-time 5 "${FSM_URL}/hypotheses" 2>/dev/null)
    if [ -n "$HYPOTHESES_JSON" ] && [ "$HYPOTHESES_JSON" != "null" ] && [ "$HYPOTHESES_JSON" != "[]" ]; then
        print_success "Found hypotheses via FSM API"
        if command -v jq >/dev/null 2>&1; then
            HYP_COUNT=$(echo "$HYPOTHESES_JSON" | jq 'length' 2>/dev/null || echo "?")
            echo "   Count: $HYP_COUNT"
            # Check first hypothesis for causal fields
            FIRST_HYP=$(echo "$HYPOTHESES_JSON" | jq '.[0]' 2>/dev/null)
            if [ -n "$FIRST_HYP" ] && [ "$FIRST_HYP" != "null" ]; then
                CAUSAL_TYPE=$(echo "$FIRST_HYP" | jq -r '.causal_type // "N/A"' 2>/dev/null)
                if [ "$CAUSAL_TYPE" != "N/A" ] && [ -n "$CAUSAL_TYPE" ]; then
                    print_success "✅ First hypothesis has causal_type: $CAUSAL_TYPE"
                else
                    print_warning "⚠️  First hypothesis missing causal_type field"
                fi
            fi
        fi
    else
        print_warning "FSM API returned empty or no hypotheses"
    fi
else
    print_warning "Could not access FSM /hypotheses endpoint"
fi

echo ""
print_status "Step 3: Checking hypotheses in Redis..."
echo ""

# Get hypotheses from Redis
# Try different possible keys
HYPOTHESES_KEY=""
for key in "fsm:agent_1:hypotheses" "fsm:hypotheses:General" "hypotheses:General"; do
    COUNT=$($REDIS_CMD EXISTS "$key" 2>/dev/null || echo "0")
    if [ "$COUNT" != "0" ]; then
        HYPOTHESES_KEY="$key"
        break
    fi
done

# If no key found, try to find any hypothesis-related keys
if [ -z "$HYPOTHESES_KEY" ]; then
    print_status "Searching for hypothesis keys in Redis..."
    FOUND_KEYS=$($REDIS_CMD KEYS "*hypothesis*" 2>/dev/null | head -5)
    if [ -n "$FOUND_KEYS" ]; then
        print_status "Found potential keys:"
        echo "$FOUND_KEYS"
        # Use the first one
        HYPOTHESES_KEY=$(echo "$FOUND_KEYS" | head -1)
    fi
fi

if [ -z "$HYPOTHESES_KEY" ]; then
    HYPOTHESES_KEY="fsm:agent_1:hypotheses"  # Default
fi

HYPOTHESES_COUNT=$($REDIS_CMD HLen "$HYPOTHESES_KEY" 2>/dev/null || echo "0")

if [ "$HYPOTHESES_COUNT" = "0" ]; then
    print_warning "No hypotheses found in Redis"
    echo ""
    # Don't exit if we have hypotheses from FSM API - we can still analyze those
    if [ -z "$HYPOTHESES_JSON" ] || [ "$HYPOTHESES_JSON" = "[]" ]; then
        print_status "This could mean:"
        echo "  1. Hypothesis generation hasn't run yet"
        echo "  2. No hypotheses were generated (check FSM logs)"
        echo "  3. Hypotheses are stored under a different key"
        echo ""
        print_status "Let's check what keys exist in Redis..."
        $REDIS_CMD KEYS "fsm:*hypotheses*" 2>/dev/null | head -10
        echo ""
        print_status "You can manually trigger hypothesis generation by:"
        echo "  1. Sending input to FSM: curl -X POST http://localhost:8083/input -H 'Content-Type: application/json' -d '{\"input\": \"test\", \"session_id\": \"test\"}'"
        echo "  2. Or checking existing hypotheses in Monitor UI: http://localhost:8082"
        exit 0
    else
        print_status "But we have hypotheses from FSM API, so we'll analyze those instead"
    fi
fi

if [ "$HYPOTHESES_COUNT" != "0" ]; then
    print_success "Found $HYPOTHESES_COUNT hypothesis(es) in Redis"
    echo ""
    
    # Get all hypothesis IDs
    HYP_IDS=$($REDIS_CMD HKeys "$HYPOTHESES_KEY" 2>/dev/null | head -5)
    
    if [ -z "$HYP_IDS" ]; then
        print_warning "Could not retrieve hypothesis IDs from Redis"
        HYP_IDS=""
    fi
else
    HYP_IDS=""
fi

echo ""
print_status "Step 4: Analyzing hypotheses for causal reasoning fields..."
echo ""

CAUSAL_COUNT=0
OBSERVATIONAL_COUNT=0
INFERRED_COUNT=0
EXPERIMENTAL_COUNT=0
TOTAL_WITH_COUNTERFACTUALS=0
TOTAL_WITH_INTERVENTIONS=0

# If we have hypotheses from FSM API, analyze those first
if [ -n "$HYPOTHESES_JSON" ] && command -v jq >/dev/null 2>&1; then
    print_status "Analyzing hypotheses from FSM API..."
    HYP_COUNT=$(echo "$HYPOTHESES_JSON" | jq 'length' 2>/dev/null || echo "0")
    
    for i in $(seq 0 $((HYP_COUNT - 1))); do
        HYP=$(echo "$HYPOTHESES_JSON" | jq ".[$i]" 2>/dev/null)
        if [ -z "$HYP" ] || [ "$HYP" = "null" ]; then
            continue
        fi
        
        HYP_ID=$(echo "$HYP" | jq -r '.id // "unknown"' 2>/dev/null)
        DESCRIPTION=$(echo "$HYP" | jq -r '.description // "N/A"' 2>/dev/null)
        CAUSAL_TYPE=$(echo "$HYP" | jq -r '.causal_type // "N/A"' 2>/dev/null)
        COUNTERFACTUALS=$(echo "$HYP" | jq '.counterfactual_actions // []' 2>/dev/null)
        INTERVENTIONS=$(echo "$HYP" | jq '.intervention_goals // []' 2>/dev/null)
        
        print_status "Analyzing hypothesis: $HYP_ID"
        echo "  Description: ${DESCRIPTION:0:80}..."
        
        if [ "$CAUSAL_TYPE" != "N/A" ] && [ -n "$CAUSAL_TYPE" ]; then
            CAUSAL_COUNT=$((CAUSAL_COUNT + 1))
            echo "  ✅ Causal Type: $CAUSAL_TYPE"
            
            case "$CAUSAL_TYPE" in
                "observational_relation")
                    OBSERVATIONAL_COUNT=$((OBSERVATIONAL_COUNT + 1))
                    ;;
                "inferred_causal_candidate")
                    INFERRED_COUNT=$((INFERRED_COUNT + 1))
                    ;;
                "experimentally_testable_relation")
                    EXPERIMENTAL_COUNT=$((EXPERIMENTAL_COUNT + 1))
                    ;;
            esac
        else
            echo "  ⚠️  No causal_type field found"
        fi
        
        CF_COUNT=$(echo "$COUNTERFACTUALS" | jq 'length' 2>/dev/null || echo "0")
        if [ "$CF_COUNT" != "0" ] && [ "$CF_COUNT" != "null" ]; then
            TOTAL_WITH_COUNTERFACTUALS=$((TOTAL_WITH_COUNTERFACTUALS + 1))
            echo "  ✅ Counterfactual Actions: $CF_COUNT"
        else
            echo "  ⚠️  No counterfactual_actions field found"
        fi
        
        INT_COUNT=$(echo "$INTERVENTIONS" | jq 'length' 2>/dev/null || echo "0")
        if [ "$INT_COUNT" != "0" ] && [ "$INT_COUNT" != "null" ]; then
            TOTAL_WITH_INTERVENTIONS=$((TOTAL_WITH_INTERVENTIONS + 1))
            echo "  ✅ Intervention Goals: $INT_COUNT"
        else
            echo "  ⚠️  No intervention_goals field found"
        fi
        
        echo ""
        
        # Limit to first 10 for readability
        if [ $i -ge 9 ]; then
            print_status "... (showing first 10 of $HYP_COUNT hypotheses)"
            break
        fi
    done
fi

# Also check Redis if we have keys
if [ "$HYPOTHESES_COUNT" != "0" ] && [ -n "$HYP_IDS" ]; then
    print_status "Also analyzing hypotheses from Redis..."
    
    for HYP_ID in $HYP_IDS; do
    print_status "Analyzing hypothesis: $HYP_ID"
    
    # Get hypothesis data
    HYP_DATA=$($REDIS_CMD HGet "$HYPOTHESES_KEY" "$HYP_ID" 2>/dev/null)
    
    if [ -z "$HYP_DATA" ]; then
        print_warning "  Could not retrieve data for $HYP_ID"
        continue
    fi
    
    # Check for causal reasoning fields using jq if available, otherwise use grep
    if command -v jq >/dev/null 2>&1; then
        DESCRIPTION=$(echo "$HYP_DATA" | jq -r '.description // "N/A"' 2>/dev/null)
        CAUSAL_TYPE=$(echo "$HYP_DATA" | jq -r '.causal_type // "N/A"' 2>/dev/null)
        COUNTERFACTUALS=$(echo "$HYP_DATA" | jq -r '.counterfactual_actions // []' 2>/dev/null)
        INTERVENTIONS=$(echo "$HYP_DATA" | jq -r '.intervention_goals // []' 2>/dev/null)
    else
        # Fallback to grep (less precise but works)
        DESCRIPTION=$(echo "$HYP_DATA" | grep -o '"description":"[^"]*' | cut -d'"' -f4 || echo "N/A")
        CAUSAL_TYPE=$(echo "$HYP_DATA" | grep -o '"causal_type":"[^"]*' | cut -d'"' -f4 || echo "N/A")
        COUNTERFACTUALS=$(echo "$HYP_DATA" | grep -o '"counterfactual_actions"' || echo "")
        INTERVENTIONS=$(echo "$HYP_DATA" | grep -o '"intervention_goals"' || echo "")
    fi
    
    echo "  Description: ${DESCRIPTION:0:80}..."
    
    if [ "$CAUSAL_TYPE" != "N/A" ] && [ -n "$CAUSAL_TYPE" ]; then
        CAUSAL_COUNT=$((CAUSAL_COUNT + 1))
        echo "  ✅ Causal Type: $CAUSAL_TYPE"
        
        case "$CAUSAL_TYPE" in
            "observational_relation")
                OBSERVATIONAL_COUNT=$((OBSERVATIONAL_COUNT + 1))
                ;;
            "inferred_causal_candidate")
                INFERRED_COUNT=$((INFERRED_COUNT + 1))
                ;;
            "experimentally_testable_relation")
                EXPERIMENTAL_COUNT=$((EXPERIMENTAL_COUNT + 1))
                ;;
        esac
    else
        echo "  ⚠️  No causal_type field found"
    fi
    
    if [ -n "$COUNTERFACTUALS" ] && [ "$COUNTERFACTUALS" != "[]" ]; then
        TOTAL_WITH_COUNTERFACTUALS=$((TOTAL_WITH_COUNTERFACTUALS + 1))
        if command -v jq >/dev/null 2>&1; then
            CF_COUNT=$(echo "$COUNTERFACTUALS" | jq 'length' 2>/dev/null || echo "?")
            echo "  ✅ Counterfactual Actions: $CF_COUNT"
        else
            echo "  ✅ Counterfactual Actions: present"
        fi
    else
        echo "  ⚠️  No counterfactual_actions field found"
    fi
    
    if [ -n "$INTERVENTIONS" ] && [ "$INTERVENTIONS" != "[]" ]; then
        TOTAL_WITH_INTERVENTIONS=$((TOTAL_WITH_INTERVENTIONS + 1))
        if command -v jq >/dev/null 2>&1; then
            INT_COUNT=$(echo "$INTERVENTIONS" | jq 'length' 2>/dev/null || echo "?")
            echo "  ✅ Intervention Goals: $INT_COUNT"
        else
            echo "  ✅ Intervention Goals: present"
        fi
    else
        echo "  ⚠️  No intervention_goals field found"
    fi
    
    echo ""
    done
else
    if [ -z "$HYPOTHESES_JSON" ] || [ "$HYPOTHESES_JSON" = "[]" ]; then
        print_warning "No hypotheses found to analyze"
    fi
fi

echo ""
print_status "Step 5: Summary of Causal Reasoning Implementation"
echo "=========================================================="
echo ""

if [ "$CAUSAL_COUNT" -gt 0 ]; then
    print_success "✅ Causal reasoning is working!"
    echo ""
    echo "  Total hypotheses with causal_type: $CAUSAL_COUNT"
    echo "    - Observational relations: $OBSERVATIONAL_COUNT"
    echo "    - Inferred causal candidates: $INFERRED_COUNT"
    echo "    - Experimentally testable: $EXPERIMENTAL_COUNT"
    echo ""
    echo "  Hypotheses with counterfactual actions: $TOTAL_WITH_COUNTERFACTUALS"
    echo "  Hypotheses with intervention goals: $TOTAL_WITH_INTERVENTIONS"
    echo ""
    
    if [ "$TOTAL_WITH_INTERVENTIONS" -gt 0 ]; then
        print_success "✅ Intervention goals are being generated!"
        echo ""
        print_status "Checking for intervention goals in curiosity goals..."
        INTERVENTION_GOALS=$($REDIS_CMD LRANGE "reasoning:curiosity_goals:General" 0 20 2>/dev/null | grep -i "intervention\|experiment\|test" | wc -l || echo "0")
        if [ "$INTERVENTION_GOALS" -gt 0 ]; then
            print_success "Found $INTERVENTION_GOALS intervention-style goals in curiosity goals!"
        fi
    fi
else
    print_warning "⚠️  No hypotheses with causal reasoning fields found"
    echo ""
    print_status "This likely means hypotheses were generated BEFORE the causal reasoning code was added."
    echo ""
    print_status "To test the new causal reasoning features:"
    echo "  1. Restart the FSM server to load the new code:"
    echo "     cd fsm && go run . -config config/artificial_mind.yaml"
    echo ""
    echo "  2. After restart, trigger new hypothesis generation:"
    echo "     - Send input via Monitor UI: http://localhost:8082"
    echo "     - Or send NATS event: echo 'test' | nats pub agi.events.input --stdin"
    echo "     - Or wait for autonomy cycle (if enabled)"
    echo ""
    echo "  3. Then run this test script again to see causal reasoning in action"
    echo ""
    print_status "The new code will:"
    echo "  - Classify hypotheses as causal vs correlation"
    echo "  - Generate counterfactual reasoning actions"
    echo "  - Create intervention-style experimental goals"
    echo "  - Prioritize causal hypotheses for testing"
fi

echo ""
print_status "Step 6: Checking FSM logs for causal reasoning messages..."
echo ""

if [ "$USE_KUBECTL" = true ] && [ -n "$FSM_POD" ]; then
    print_status "Checking FSM pod logs..."
    CAUSAL_LOG_COUNT=$(kubectl logs -n "$NAMESPACE" "$FSM_POD" --tail=500 2>/dev/null | grep -c "\[CAUSAL\]" || echo "0")
    if [ "$CAUSAL_LOG_COUNT" -gt 0 ]; then
        print_success "Found $CAUSAL_LOG_COUNT causal reasoning log entries"
        echo ""
        print_status "Recent causal reasoning logs:"
        kubectl logs -n "$NAMESPACE" "$FSM_POD" --tail=500 2>/dev/null | grep "\[CAUSAL\]" | tail -5
    else
        print_warning "No [CAUSAL] log entries found in FSM pod logs"
        echo "  This suggests hypothesis generation hasn't run with the new code"
        echo ""
        print_status "To watch logs in real-time:"
        echo "  kubectl logs -n $NAMESPACE -f $FSM_POD | grep CAUSAL"
    fi
else
    # Local Docker environment
    if [ -f "/tmp/fsm_server.log" ]; then
        CAUSAL_LOG_COUNT=$(grep -c "\[CAUSAL\]" /tmp/fsm_server.log 2>/dev/null || echo "0")
        if [ "$CAUSAL_LOG_COUNT" -gt 0 ]; then
            print_success "Found $CAUSAL_LOG_COUNT causal reasoning log entries"
            echo ""
            print_status "Recent causal reasoning logs:"
            grep "\[CAUSAL\]" /tmp/fsm_server.log | tail -5
        else
            print_warning "No [CAUSAL] log entries found"
            echo "  This suggests hypothesis generation hasn't run with the new code"
        fi
    else
        print_warning "FSM log file not found at /tmp/fsm_server.log"
        echo "  Check where your FSM server logs are written"
    fi
fi

echo ""
echo "=========================================================="
print_status "Test Complete!"
echo ""
print_status "Next steps:"
if [ "$USE_KUBECTL" = true ]; then
    echo "  1. View hypotheses in Monitor UI (port-forward if needed)"
    echo "  2. View hypotheses via FSM API: curl ${FSM_URL}/hypotheses | jq"
    echo "  3. Check FSM logs: kubectl logs -n $NAMESPACE -f $FSM_POD | grep CAUSAL"
    echo "  4. Trigger hypothesis generation:"
    echo "     - Send NATS event: echo 'test input' | nats pub agi.events.input --stdin"
    echo "     - Or wait for autonomy cycle (if enabled)"
    echo "     - Or use Monitor UI to send input"
    echo "  5. Verify intervention goals are created with higher priority"
    echo ""
    echo "To manually test causal reasoning:"
    echo "  1. Restart FSM deployment to load new code:"
    echo "     kubectl rollout restart deployment/fsm-server -n $NAMESPACE"
    echo "  2. Wait for pod to be ready: kubectl wait --for=condition=ready pod -l app=fsm-server -n $NAMESPACE"
    echo "  3. Send input via Monitor UI or NATS to trigger hypothesis generation"
    echo "  4. Check that hypotheses have causal_type, counterfactual_actions, and intervention_goals fields"
else
    echo "  1. View hypotheses in Monitor UI: http://localhost:8082"
    echo "  2. View hypotheses via FSM API: curl ${FSM_URL}/hypotheses | jq"
    echo "  3. Check FSM logs: tail -f /tmp/fsm_server.log | grep CAUSAL"
    echo "  4. Trigger hypothesis generation:"
    echo "     - Send NATS event: echo 'test input' | nats pub agi.events.input --stdin"
    echo "     - Or wait for autonomy cycle (if enabled)"
    echo "     - Or use Monitor UI to send input"
    echo "  5. Verify intervention goals are created with higher priority"
    echo ""
    echo "To manually test causal reasoning:"
    echo "  1. Restart FSM server to load new code: cd fsm && go run . -config config/artificial_mind.yaml"
    echo "  2. Send input via Monitor UI or NATS to trigger hypothesis generation"
    echo "  3. Check that hypotheses have causal_type, counterfactual_actions, and intervention_goals fields"
fi
echo ""

