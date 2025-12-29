#!/bin/bash

# Test script for Active Learning Loops feature
# Tests query-driven learning for high-uncertainty concepts

set -e

NAMESPACE="${K8S_NAMESPACE:-agi}"
MODE="${1:-test}"
FSM_URL="${FSM_URL:-http://localhost:8083}"

# Colors for output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Get pod names
REDIS_POD=$(kubectl get pods -n "$NAMESPACE" -l app=redis -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || kubectl get pods -n "$NAMESPACE" | grep redis | head -1 | awk '{print $1}')
FSM_POD=$(kubectl get pods -n "$NAMESPACE" -l app=fsm-server-rpi58 -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || kubectl get pods -n "$NAMESPACE" -l app=fsm-server -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)

# Helper function for Redis commands
redis_cmd() {
    kubectl exec -n "$NAMESPACE" "$REDIS_POD" -- redis-cli "$@" 2>/dev/null
}

# Helper function to check if a value is JSON
is_json() {
    echo "$1" | python3 -c "import sys, json; json.load(sys.stdin)" 2>/dev/null
}

# Check mode and execute
case "$MODE" in
    test|complete)
        echo -e "${BLUE}🔬 Testing Active Learning Loops${NC}"
        echo "=================================="
        echo ""
        
        if [ -z "$REDIS_POD" ] || [ -z "$FSM_POD" ]; then
            echo -e "${RED}❌ Required pods not found${NC}"
            echo "   Redis: ${REDIS_POD:-NOT FOUND}"
            echo "   FSM: ${FSM_POD:-NOT FOUND}"
            exit 1
        fi
        
        echo -e "${GREEN}✅ Redis pod: $REDIS_POD${NC}"
        echo -e "${GREEN}✅ FSM pod: $FSM_POD${NC}"
        echo ""
        
        # Test 1: Check for existing high-uncertainty concepts
        echo "1️⃣ Checking for high-uncertainty concepts"
        echo "------------------------------------------"
        
        DOMAIN="General"
        BELIEF_KEY="reasoning:beliefs:$DOMAIN"
        HYP_KEY="fsm:agent_1:hypotheses"
        GOAL_KEY="reasoning:curiosity_goals:$DOMAIN"
        
        # Check beliefs with high uncertainty
        BELIEF_COUNT=$(redis_cmd LLEN "$BELIEF_KEY" 2>/dev/null || echo "0")
        echo "   Beliefs in domain '$DOMAIN': $BELIEF_COUNT"
        
        if [ "$BELIEF_COUNT" -gt 0 ]; then
            echo "   Checking beliefs for high uncertainty..."
            HIGH_UNCERTAINTY_BELIEFS=0
            redis_cmd LRANGE "$BELIEF_KEY" 0 9 2>/dev/null | while read -r belief_json; do
                if [ -n "$belief_json" ] && is_json "$belief_json" >/dev/null 2>&1; then
                    epistemic=$(echo "$belief_json" | python3 -c "import sys, json; d=json.load(sys.stdin); print(d.get('uncertainty', {}).get('epistemic_uncertainty', 0))" 2>/dev/null || echo "0")
                    if [ -n "$epistemic" ] && [ "$(echo "$epistemic >= 0.4" | bc 2>/dev/null || echo 0)" = "1" ]; then
                        HIGH_UNCERTAINTY_BELIEFS=$((HIGH_UNCERTAINTY_BELIEFS + 1))
                    fi
                fi
            done
        fi
        
        # Check hypotheses with high uncertainty (stored as HASH, not LIST)
        HYP_COUNT=$(redis_cmd HKEYS "$HYP_KEY" 2>/dev/null | wc -l | tr -d ' ' || echo "0")
        if [ "$HYP_COUNT" = "0" ]; then
            # Try as LIST if HASH doesn't work
            HYP_COUNT=$(redis_cmd LLEN "$HYP_KEY" 2>/dev/null || echo "0")
        fi
        echo "   Hypotheses: $HYP_COUNT"
        
        # Check existing goals
        EXISTING_GOAL_COUNT=$(redis_cmd LLEN "$GOAL_KEY" 2>/dev/null || echo "0")
        echo "   Existing curiosity goals: $EXISTING_GOAL_COUNT"
        echo ""
        
        # Test 2: Create high-uncertainty belief for testing
        echo "2️⃣ Creating test high-uncertainty belief"
        echo "----------------------------------------"
        
        # Check if we already have high-uncertainty beliefs
        TEST_BELIEF_ID="test_active_learning_$(date +%s)"
        
        # Sample a few beliefs to check uncertainty (use Python to avoid subshell issues)
        HIGH_UNCERTAINTY_FOUND=$(redis_cmd LRANGE "$BELIEF_KEY" 0 19 2>/dev/null | python3 -c "
import sys, json
for line in sys.stdin:
    line = line.strip()
    if not line:
        continue
    try:
        belief = json.loads(line)
        uncertainty = belief.get('uncertainty', {})
        epistemic = uncertainty.get('epistemic_uncertainty', 0)
        if epistemic >= 0.4:
            print('true')
            sys.exit(0)
    except:
        pass
print('false')
" 2>/dev/null || echo "false")
        
        if [ "$HIGH_UNCERTAINTY_FOUND" = false ]; then
            echo "   Creating test high-uncertainty belief..."
            TEST_BELIEF='{
              "id": "'"$TEST_BELIEF_ID"'",
              "statement": "Quantum Computing will revolutionize cryptography and AI",
              "domain": "General",
              "confidence": 0.3,
              "uncertainty": {
                "epistemic_uncertainty": 0.75,
                "aleatoric_uncertainty": 0.1,
                "calibrated_confidence": 0.3
              },
              "created_at": "'$(date -u +%Y-%m-%dT%H:%M:%SZ)'"
            }'
            
            redis_cmd LPUSH "$BELIEF_KEY" "$TEST_BELIEF" > /dev/null 2>&1
            if [ $? -eq 0 ]; then
                echo -e "   ${GREEN}✅ Created test belief: $TEST_BELIEF_ID${NC}"
                echo "      Epistemic uncertainty: 0.75 (above threshold 0.4)"
            else
                echo -e "   ${RED}❌ Failed to create test belief${NC}"
            fi
        else
            echo -e "   ${GREEN}✅ High-uncertainty beliefs already exist${NC}"
        fi
        echo ""
        
        # Test 3: Trigger curiosity goal generation (best-effort)
        echo "3️⃣ Triggering curiosity goal generation (best-effort)"
        echo "-----------------------------------------------------"
        
        # Ensure FSM API is reachable (port-forward in k8s)
        FSM_PORT_FORWARD_PID=""
        if ! curl -s --connect-timeout 2 "$FSM_URL/health" > /dev/null 2>&1; then
            if command -v kubectl >/dev/null 2>&1; then
                # Prefer the k3s service name used in this repo
                if ! lsof -ti:8083 > /dev/null 2>&1; then
                    kubectl port-forward -n "$NAMESPACE" svc/fsm-server-rpi58 8083:8083 > /dev/null 2>&1 &
                    FSM_PORT_FORWARD_PID=$!
                    sleep 3
                fi
            fi
        fi
        
        # Trigger via FSM event API (deterministic; no reliance on timer)
        echo "   Triggering goal generation..."
        
        # NOTE: FSM does not expose a stable public HTTP endpoint to force TriggerAutonomyCycle.
        # Some environments may have /api/v1/events; we treat this as best-effort and rely on polling below.
        TRIGGER_SUCCESS=false
        HTTP_CODE=$(curl -s -o /tmp/fsm_event_resp.txt -w "%{http_code}" -X POST "$FSM_URL/api/v1/events" \
            -H "Content-Type: application/json" \
            -d '{"event":"generate_curiosity_goals","payload":{}}' 2>/dev/null || echo "000")
        RESPONSE=$(cat /tmp/fsm_event_resp.txt 2>/dev/null || echo "")
        rm -f /tmp/fsm_event_resp.txt 2>/dev/null || true

        if [ "$HTTP_CODE" = "200" ] || [ "$HTTP_CODE" = "204" ]; then
            TRIGGER_SUCCESS=true
            echo -e "   ${GREEN}✅ Triggered (HTTP $HTTP_CODE)${NC}"
        else
            echo -e "   ${YELLOW}⚠️  Trigger endpoint not available (HTTP $HTTP_CODE). Will rely on autonomy scheduler.${NC}"
        fi
        
        if [ "$TRIGGER_SUCCESS" = false ]; then
            echo -e "   ${YELLOW}⚠️  Could not trigger via API, will check existing goals...${NC}"
        fi
        
        echo "   ⏳ Waiting 5 seconds for processing..."
        sleep 5
        echo ""
        
        # Test 4: Check for active learning goals (poll up to 90s)
        echo "4️⃣ Checking for active learning goals (poll up to 90s)"
        echo "-------------------------------------------------------"
        
        NEW_GOAL_COUNT=$(redis_cmd LLEN "$GOAL_KEY" 2>/dev/null || echo "0")
        echo "   Total curiosity goals now: $NEW_GOAL_COUNT"
        
        if [ "$NEW_GOAL_COUNT" -gt "$EXISTING_GOAL_COUNT" ]; then
            echo -e "   ${GREEN}✅ New goals generated!${NC}"
        else
            echo -e "   ${YELLOW}⚠️  No new goals generated (checking existing goals for active_learning type)${NC}"
        fi
        
        ACTIVE_LEARNING_COUNT=0
        ACTIVE_LEARNING_DATA='{"count": 0, "goals": []}'
        attempts=18  # 18 * 5s = 90s
        while [ $attempts -gt 0 ]; do
            # Check for active learning goals specifically (scan full list window)
            ACTIVE_LEARNING_DATA=$(redis_cmd LRANGE "$GOAL_KEY" 0 199 2>/dev/null | python3 -c "
import sys, json
count = 0
goals = []
for line in sys.stdin:
    line = line.strip()
    if not line:
        continue
    try:
        goal = json.loads(line)
        if goal.get('type') == 'active_learning':
            count += 1
            goals.append({
                'id': goal.get('id', 'N/A'),
                'description': goal.get('description', 'N/A')[:60]
            })
    except:
        pass
print(json.dumps({'count': count, 'goals': goals}))
" 2>/dev/null || echo '{"count": 0, "goals": []}')
        
            ACTIVE_LEARNING_COUNT=$(echo "$ACTIVE_LEARNING_DATA" | python3 -c "import sys, json; print(json.load(sys.stdin).get('count', 0))" 2>/dev/null || echo "0")
            if [ "$ACTIVE_LEARNING_COUNT" -gt 0 ]; then
                break
            fi
            echo "   ⏳ Not found yet; waiting 5s... (remaining polls: $attempts)"
            sleep 5
            attempts=$((attempts - 1))
        done
        
        if [ "$ACTIVE_LEARNING_COUNT" -gt 0 ]; then
            echo -e "   ${GREEN}✅ Found $ACTIVE_LEARNING_COUNT active learning goal(s)!${NC}"
            echo "$ACTIVE_LEARNING_DATA" | python3 -c "
import sys, json
data = json.load(sys.stdin)
for goal in data.get('goals', []):
    print(f\"   - {goal['id']}: {goal['description']}...\")
" 2>/dev/null
        else
            echo -e "   ${YELLOW}⚠️  No active learning goals found${NC}"
            echo "   (This may be OK if:)"
            echo "   - No high-uncertainty concepts exist (threshold: 0.4)"
            echo "   - System needs more time to process"
            echo "   - Goals were generated but not yet stored"
        fi
        echo ""

        # Hard assertion in test mode: we should have at least 1 active_learning goal
        if [ "$ACTIVE_LEARNING_COUNT" -eq 0 ]; then
            echo -e "${RED}❌ FAIL: Expected at least 1 active_learning goal after triggering generation${NC}"
            echo "   Tip: check FSM logs for [ACTIVE-LEARNING] lines:"
            echo "   kubectl logs -n $NAMESPACE $FSM_POD --tail=300 | grep -i \"ACTIVE-LEARNING\\|CALLING\\|Warning: Failed to generate active learning\""
            exit 1
        fi
        
        # Test 5: Verify active learning goal properties
        echo "5️⃣ Verifying active learning goal properties"
        echo "----------------------------------------------"
        
        if [ "$ACTIVE_LEARNING_COUNT" -gt 0 ]; then
            # Get first active learning goal
            FIRST_AL_GOAL=$(redis_cmd LRANGE "$GOAL_KEY" 0 19 2>/dev/null | grep -o '"type":"active_learning"[^}]*}' | head -1 || echo "")
            
            if [ -n "$FIRST_AL_GOAL" ]; then
                # Extract full goal JSON
                GOAL_FULL=$(redis_cmd LRANGE "$GOAL_KEY" 0 19 2>/dev/null | python3 -c "
import sys, json
for line in sys.stdin:
    try:
        goal = json.loads(line.strip())
        if goal.get('type') == 'active_learning':
            print(json.dumps(goal, indent=2))
            break
    except:
        pass
" 2>/dev/null || echo "")
                
                if [ -n "$GOAL_FULL" ]; then
                    echo "   Sample active learning goal:"
                    echo "$GOAL_FULL" | python3 -c "
import sys, json
goal = json.load(sys.stdin)
print(f\"   ID: {goal.get('id', 'N/A')}\")
print(f\"   Type: {goal.get('type', 'N/A')}\")
print(f\"   Priority: {goal.get('priority', 'N/A')}\")
print(f\"   Domain: {goal.get('domain', 'N/A')}\")
print(f\"   Description: {goal.get('description', 'N/A')[:80]}...\")
unc = goal.get('uncertainty', {})
if unc:
    print(f\"   Uncertainty:\")
    print(f\"     - Epistemic: {unc.get('epistemic_uncertainty', 'N/A')}\")
    print(f\"     - Aleatoric: {unc.get('aleatoric_uncertainty', 'N/A')}\")
    print(f\"     - Calibrated Confidence: {unc.get('calibrated_confidence', 'N/A')}\")
print(f\"   Value: {goal.get('value', 'N/A')}\")
" 2>/dev/null || echo "   (Could not parse goal JSON)"
                fi
            fi
        else
            echo -e "   ${YELLOW}⚠️  Skipping (no active learning goals found)${NC}"
        fi
        echo ""
        
        # Test 6: Check FSM logs for active learning messages
        echo "6️⃣ Checking FSM logs for active learning messages"
        echo "---------------------------------------------------"
        
        LOG_MESSAGES=$(kubectl logs -n "$NAMESPACE" "$FSM_POD" --tail=200 2>/dev/null | grep -i "ACTIVE-LEARNING\|active learning\|high-uncertainty\|uncertainty reduction" | tail -10 || echo "")
        
        if [ -n "$LOG_MESSAGES" ]; then
            echo -e "   ${GREEN}✅ Found active learning log messages:${NC}"
            echo "$LOG_MESSAGES" | sed 's/^/   /'
        else
            echo -e "   ${YELLOW}⚠️  No active learning log messages found${NC}"
            echo "   (This may be normal if no high-uncertainty concepts were identified)"
        fi
        echo ""
        
        # Test 7: Cleanup test belief (optional)
        echo "7️⃣ Cleanup test belief (optional)"
        echo "-----------------------------------------------------"
        if [ -n "$TEST_BELIEF_ID" ] && [ "$HIGH_UNCERTAINTY_FOUND" = false ]; then
            echo "   Test belief created: $TEST_BELIEF_ID"
            echo "   To remove it, run:"
            echo "   kubectl exec -n $NAMESPACE $REDIS_POD -- redis-cli LREM $BELIEF_KEY 1 '...'"
            echo "   (or leave it for future testing)"
        else
            echo "   No test belief created (high-uncertainty beliefs already exist)"
        fi
        echo ""
        
        # Cleanup
        if [ -n "$FSM_PORT_FORWARD_PID" ]; then
            kill $FSM_PORT_FORWARD_PID 2>/dev/null || true
        fi
        
        # Summary
        echo "=================================="
        echo -e "${BLUE}📊 Test Summary${NC}"
        echo "=================================="
        echo "   Domain: $DOMAIN"
        echo "   Beliefs: $BELIEF_COUNT"
        echo "   Hypotheses: $HYP_COUNT"
        echo "   Curiosity Goals: $NEW_GOAL_COUNT"
        echo "   Active Learning Goals: $ACTIVE_LEARNING_COUNT"
        echo ""
        
        if [ "$ACTIVE_LEARNING_COUNT" -gt 0 ]; then
            echo -e "${GREEN}✅ Active Learning Loops are working!${NC}"
            echo "   Active learning goals were generated from high-uncertainty concepts."
        else
            echo -e "${YELLOW}⚠️  No active learning goals generated${NC}"
            echo "   This may be normal if:"
            echo "   - No high-uncertainty concepts exist (threshold: 0.4)"
            echo "   - System needs more time to process"
            echo "   - Domain has no concepts with uncertainty models"
            echo ""
            echo "   Try creating a high-uncertainty belief (see step 6 above)"
        fi
        echo ""
        ;;
        
    status|check)
        echo "🔍 Checking Active Learning Loops Status"
        echo "========================================"
        echo ""
        
        if [ -z "$REDIS_POD" ] || [ -z "$FSM_POD" ]; then
            echo -e "${RED}❌ Required pods not found${NC}"
            exit 1
        fi
        
        DOMAIN="General"
        GOAL_KEY="reasoning:curiosity_goals:$DOMAIN"
        
        # Count active learning goals
        ACTIVE_LEARNING_COUNT=$(redis_cmd LRANGE "$GOAL_KEY" 0 199 2>/dev/null | python3 -c "
import sys, json
count = 0
for line in sys.stdin:
    try:
        goal = json.loads(line.strip())
        if goal.get('type') == 'active_learning':
            count += 1
    except:
        pass
print(count)
" 2>/dev/null || echo "0")
        
        echo "   Active Learning Goals: $ACTIVE_LEARNING_COUNT"
        echo ""
        
        # Show recent active learning goals
        if [ "$ACTIVE_LEARNING_COUNT" -gt 0 ]; then
            echo "   Recent active learning goals:"
            redis_cmd LRANGE "$GOAL_KEY" 0 9 2>/dev/null | python3 -c "
import sys, json
for line in sys.stdin:
    try:
        goal = json.loads(line.strip())
        if goal.get('type') == 'active_learning':
            print(f\"   - {goal.get('id', 'N/A')}: {goal.get('description', 'N/A')[:60]}...\")
            print(f\"     Priority: {goal.get('priority', 'N/A')}, Value: {goal.get('value', 'N/A')}\")
    except:
        pass
" 2>/dev/null || echo "   (Could not parse goals)"
        fi
        echo ""
        
        # Check logs
        echo "   Recent FSM log messages:"
        kubectl logs -n "$NAMESPACE" "$FSM_POD" --tail=50 2>/dev/null | grep -i "ACTIVE-LEARNING\|active learning" | tail -5 || echo "   (No recent messages)"
        echo ""
        ;;
        
    *)
        echo "Usage: $0 [test|status]"
        echo ""
        echo "Modes:"
        echo "  test    - Run full test suite (default)"
        echo "  status  - Quick status check"
        echo ""
        exit 1
        ;;
esac

