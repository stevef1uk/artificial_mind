#!/bin/bash

# Comprehensive test script for Explanation-Grounded Learning Feedback
# This script can test, diagnose, rebuild, and verify the explanation learning feature

set -e

NAMESPACE="${K8S_NAMESPACE:-agi}"
MODE="${1:-test}"

# Colors for output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

# Get pod names
REDIS_POD=$(kubectl get pods -n "$NAMESPACE" -l app=redis -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || kubectl get pods -n "$NAMESPACE" | grep redis | head -1 | awk '{print $1}')
FSM_POD=$(kubectl get pods -n "$NAMESPACE" -l app=fsm-server-rpi58 -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || kubectl get pods -n "$NAMESPACE" -l app=fsm-server -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
GOAL_MGR_POD=$(kubectl get pods -n "$NAMESPACE" -l app=goal-manager -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)

# Check mode and execute
case "$MODE" in
    test|complete)
        # Test mode: Complete a goal and verify explanation learning
        echo "🎯 Testing Explanation-Grounded Learning Feedback"
        echo "================================================="
        echo ""
        
        if [ -z "$REDIS_POD" ] || [ -z "$FSM_POD" ] || [ -z "$GOAL_MGR_POD" ]; then
            echo -e "${RED}❌ Required pods not found${NC}"
            echo "   Redis: ${REDIS_POD:-NOT FOUND}"
            echo "   FSM: ${FSM_POD:-NOT FOUND}"
            echo "   Goal Manager: ${GOAL_MGR_POD:-NOT FOUND}"
            exit 1
        fi
        
        echo -e "${GREEN}✅ Redis pod: $REDIS_POD${NC}"
        echo -e "${GREEN}✅ FSM pod: $FSM_POD${NC}"
        echo -e "${GREEN}✅ Goal Manager pod: $GOAL_MGR_POD${NC}"
        echo ""
        
        # Get a goal ID
        echo "1️⃣ Getting goal ID from active set..."
        echo "--------------------------------------"
        GOAL_ID=$(kubectl exec -n "$NAMESPACE" "$REDIS_POD" -- redis-cli SMEMBERS "goals:agent_1:active" 2>/dev/null | head -1)
        
        if [ -z "$GOAL_ID" ]; then
            echo -e "${YELLOW}⚠️  No active goals found. Creating a test goal...${NC}"
            # Create a test goal via Goal Manager API
            PORT_FORWARD_PID=""
            if ! lsof -ti:8090 > /dev/null 2>&1; then
                kubectl port-forward -n "$NAMESPACE" svc/goal-manager 8090:8090 > /dev/null 2>&1 &
                PORT_FORWARD_PID=$!
                sleep 3
            fi
            
            GOAL_PAYLOAD='{"description":"Test explanation learning - verify hypothesis accuracy","priority":"high","origin":"test:explanation_learning","status":"active","confidence":0.75,"context":{"domain":"General","test":true}}'
            RESPONSE=$(curl -s -X POST "http://localhost:8090/goal" -H "Content-Type: application/json" -d "$GOAL_PAYLOAD" 2>&1)
            GOAL_ID=$(echo "$RESPONSE" | python3 -c "import sys, json; d=json.load(sys.stdin); print(d.get('id', ''))" 2>/dev/null || echo "")
            
            if [ -n "$PORT_FORWARD_PID" ]; then
                kill $PORT_FORWARD_PID 2>/dev/null || true
            fi
            
            if [ -z "$GOAL_ID" ]; then
                echo -e "${RED}❌ Failed to create test goal${NC}"
                exit 1
            fi
            echo -e "${GREEN}   ✅ Created test goal: $GOAL_ID${NC}"
        else
            echo -e "${GREEN}   ✅ Goal ID: $GOAL_ID${NC}"
        fi
        echo ""
        
        # Start log watcher
        echo "2️⃣ Starting log watcher..."
        echo "---------------------------"
        kubectl logs -f -n "$NAMESPACE" "$FSM_POD" 2>/dev/null | grep --line-buffered "EXPLANATION-LEARNING" &
        WATCHER_PID=$!
        sleep 2
        
        # Set up port-forwarding
        echo "3️⃣ Setting up port-forwarding..."
        echo "-----------------------------------"
        PORT_FORWARD_PID=""
        if ! lsof -ti:8090 > /dev/null 2>&1; then
            kubectl port-forward -n "$NAMESPACE" svc/goal-manager 8090:8090 > /dev/null 2>&1 &
            PORT_FORWARD_PID=$!
            sleep 3
            if curl -s --connect-timeout 2 http://localhost:8090/health > /dev/null 2>&1; then
                echo -e "${GREEN}   ✅ Port-forward established${NC}"
            else
                echo -e "${YELLOW}   ⚠️  Port-forward may have failed${NC}"
            fi
        else
            echo "   ℹ️  Port 8090 already in use"
        fi
        
        # Complete goal
        echo ""
        echo "4️⃣ Completing goal via Goal Manager API..."
        echo "-------------------------------------------"
        UPDATED_AT=$(date -u +%Y-%m-%dT%H:%M:%SZ)
        ACHIEVE_PAYLOAD='{"result":{"success":true,"test":true,"executed_at":"'$UPDATED_AT'","manual_test":true}}'
        
        RESPONSE=$(curl -s -w "\nHTTP_CODE:%{http_code}" -X POST "http://localhost:8090/goal/$GOAL_ID/achieve" \
            -H "Content-Type: application/json" \
            -d "$ACHIEVE_PAYLOAD" 2>&1)
        
        HTTP_CODE=$(echo "$RESPONSE" | grep "HTTP_CODE:" | cut -d: -f2)
        
        if [ "$HTTP_CODE" = "200" ]; then
            echo -e "${GREEN}   ✅ Goal completed successfully (HTTP $HTTP_CODE)${NC}"
            echo -e "${GREEN}   ✅ NATS event should have been published${NC}"
        else
            echo -e "${RED}   ❌ API call failed (HTTP ${HTTP_CODE:-unknown})${NC}"
        fi
        
        # Clean up port-forward
        if [ -n "$PORT_FORWARD_PID" ]; then
            kill $PORT_FORWARD_PID 2>/dev/null || true
        fi
        
        # Wait for processing
        echo ""
        echo "5️⃣ Waiting for explanation learning to process..."
        echo "--------------------------------------------------"
        sleep 15
        
        # Stop watcher
        kill $WATCHER_PID 2>/dev/null || true
        sleep 1
        
        # Check for messages
        echo ""
        echo "6️⃣ Checking for explanation learning messages..."
        echo "------------------------------------------------"
        RECENT_LOGS=$(kubectl logs -n "$NAMESPACE" "$FSM_POD" --tail=500 2>/dev/null | grep -i "EXPLANATION-LEARNING" | grep -i "$GOAL_ID" | head -10)
        
        if [ -n "$RECENT_LOGS" ]; then
            echo -e "${GREEN}   ✅ SUCCESS! Found explanation learning activity:${NC}"
            echo ""
            echo "$RECENT_LOGS"
        else
            ANY_EL_LOGS=$(kubectl logs -n "$NAMESPACE" "$FSM_POD" --tail=500 2>/dev/null | grep -i "EXPLANATION-LEARNING.*Evaluating\|EXPLANATION-LEARNING.*Completed evaluation" | tail -3)
            if [ -n "$ANY_EL_LOGS" ]; then
                echo -e "${GREEN}   ✅ Found recent explanation learning activity:${NC}"
                echo ""
                echo "$ANY_EL_LOGS"
            else
                echo -e "${YELLOW}   ⚠️  No explanation learning messages found${NC}"
            fi
        fi
        
        # Check Redis
        echo ""
        echo "7️⃣ Checking Redis for learning data..."
        echo "--------------------------------------"
        KEYS=$(kubectl exec -n "$NAMESPACE" "$REDIS_POD" -- redis-cli KEYS "explanation_learning:*" 2>/dev/null)
        if [ -n "$KEYS" ]; then
            echo -e "${GREEN}   ✅ Found explanation learning keys:${NC}"
            echo "$KEYS" | head -10
        else
            echo -e "${YELLOW}   ℹ️  No explanation learning keys found yet${NC}"
        fi
        
        echo ""
        echo -e "${GREEN}✅ Test complete!${NC}"
        ;;
        
    check|verify)
        # Quick check mode: Verify the system is working
        echo "🔍 Quick Check: Explanation Learning Feature"
        echo "============================================"
        echo ""
        
        if [ -z "$FSM_POD" ]; then
            echo -e "${RED}❌ FSM pod not found${NC}"
            exit 1
        fi
        
        echo "1️⃣ Checking FSM pod..."
        echo "---------------------"
        echo -e "${GREEN}✅ FSM pod running: $FSM_POD${NC}"
        echo ""
        
        echo "2️⃣ Checking for explanation learning code in logs..."
        echo "---------------------------------------------------"
        EL_LOGS=$(kubectl logs -n "$NAMESPACE" "$FSM_POD" --tail=200 2>/dev/null | grep -i "EXPLANATION-LEARNING\|Subscribed.*goal.*achieved" | head -5)
        if [ -n "$EL_LOGS" ]; then
            echo -e "${GREEN}   ✅ Found explanation learning activity:${NC}"
            echo "$EL_LOGS"
        else
            echo -e "${YELLOW}   ⚠️  No explanation learning messages yet (this is OK if no goals completed)${NC}"
        fi
        echo ""
        
        echo "3️⃣ Checking NATS subscriptions..."
        echo "----------------------------------"
        SUBS=$(kubectl logs -n "$NAMESPACE" "$FSM_POD" 2>/dev/null | grep -i "Subscribed.*goal.*achieved\|Subscribed.*goal.*failed" | head -2)
        if [ -n "$SUBS" ]; then
            echo -e "${GREEN}   ✅ Found NATS subscriptions:${NC}"
            echo "$SUBS"
        else
            echo -e "${YELLOW}   ⚠️  NATS subscriptions not found in recent logs${NC}"
        fi
        echo ""
        
        if [ -n "$REDIS_POD" ]; then
            echo "4️⃣ Checking Redis for learning data..."
            echo "--------------------------------------"
            KEYS=$(kubectl exec -n "$NAMESPACE" "$REDIS_POD" -- redis-cli KEYS "explanation_learning:*" 2>/dev/null | head -5)
            if [ -n "$KEYS" ]; then
                echo -e "${GREEN}   ✅ Found explanation learning keys:${NC}"
                echo "$KEYS"
            else
                echo -e "${YELLOW}   ℹ️  No explanation learning keys found yet${NC}"
            fi
        fi
        
        echo ""
        echo -e "${GREEN}✅ Quick check complete!${NC}"
        ;;
        
    diagnose)
        # Diagnostic mode: Thorough diagnosis
        echo "🔍 Diagnosing Explanation Learning Deployment"
        echo "=============================================="
        echo ""
        
        if [ -z "$FSM_POD" ]; then
            echo -e "${RED}❌ FSM pod not found${NC}"
            exit 1
        fi
        
        echo "1️⃣ Pod Status..."
        echo "---------------"
        kubectl get pod -n "$NAMESPACE" "$FSM_POD" -o jsonpath='{.status.containerStatuses[0].restartCount}' 2>/dev/null | xargs -I {} echo "   Restart count: {}"
        echo ""
        
        echo "2️⃣ Checking for explanation learning code in binary..."
        echo "------------------------------------------------------"
        if kubectl exec -n "$NAMESPACE" "$FSM_POD" -- strings /app/fsm-server 2>/dev/null | grep -q "ExplanationLearningFeedback\|EvaluateGoalCompletion"; then
            echo -e "${GREEN}   ✅ Explanation learning code found in binary${NC}"
        else
            echo -e "${RED}   ❌ Explanation learning code NOT found in binary${NC}"
            echo "   💡 Binary may need to be rebuilt"
        fi
        echo ""
        
        echo "3️⃣ Checking NATS subscriptions..."
        echo "----------------------------------"
        SUBS=$(kubectl logs -n "$NAMESPACE" "$FSM_POD" 2>/dev/null | grep -i "Subscribed.*goal.*achieved\|Subscribed.*goal.*failed\|explanation.*learning" | head -10)
        if [ -n "$SUBS" ]; then
            echo "$SUBS"
        else
            echo -e "${YELLOW}   ⚠️  No subscription logs found${NC}"
        fi
        echo ""
        
        echo "4️⃣ Checking for errors..."
        echo "-------------------------"
        ERRORS=$(kubectl logs -n "$NAMESPACE" "$FSM_POD" --tail=200 2>/dev/null | grep -iE "error|panic|fatal|nil.*dereference" | grep -i "explanation\|learning" | head -10)
        if [ -n "$ERRORS" ]; then
            echo -e "${RED}   ⚠️  Found errors:${NC}"
            echo "$ERRORS"
        else
            echo -e "${GREEN}   ✅ No errors found${NC}"
        fi
        echo ""
        
        if [ -n "$REDIS_POD" ]; then
            echo "5️⃣ Checking Redis data..."
            echo "-------------------------"
            KEYS=$(kubectl exec -n "$NAMESPACE" "$REDIS_POD" -- redis-cli KEYS "explanation_learning:*" 2>/dev/null)
            if [ -n "$KEYS" ]; then
                echo -e "${GREEN}   ✅ Found ${#KEYS[@]} explanation learning keys${NC}"
                echo "$KEYS" | head -10
            else
                echo -e "${YELLOW}   ℹ️  No explanation learning keys found${NC}"
            fi
        fi
        
        echo ""
        echo -e "${GREEN}✅ Diagnosis complete!${NC}"
        ;;
        
    rebuild)
        # Rebuild mode: Rebuild and restart FSM
        echo "🔨 Rebuilding FSM Server with Explanation Learning"
        echo "==================================================="
        echo ""
        
        BRANCH="add-explanation-grounded-learning-feedback"
        
        echo "1️⃣ Switching to branch: $BRANCH"
        echo "--------------------------------"
        CURRENT_BRANCH=$(git branch --show-current 2>/dev/null || echo "unknown")
        echo "   Current branch: $CURRENT_BRANCH"
        
        if [ "$CURRENT_BRANCH" != "$BRANCH" ]; then
            git checkout "$BRANCH" 2>/dev/null || {
                echo -e "${YELLOW}   ⚠️  Branch not found locally, checking out from origin...${NC}"
                git fetch origin "$BRANCH:$BRANCH" 2>/dev/null || {
                    echo -e "${RED}   ❌ Branch not found. Make sure you've pushed the branch or it exists on origin${NC}"
                    exit 1
                }
                git checkout "$BRANCH"
            }
        fi
        echo -e "${GREEN}   ✅ Switched to branch: $BRANCH${NC}"
        echo ""
        
        echo "2️⃣ Pulling latest changes..."
        echo "----------------------------"
        git pull origin "$BRANCH" 2>/dev/null || echo -e "${YELLOW}   ⚠️  Could not pull (may be OK if already up to date)${NC}"
        echo ""
        
        echo "3️⃣ Building FSM binary..."
        echo "------------------------"
        cd fsm && go build -o ../bin/fsm-server . && cd ..
        if [ $? -eq 0 ]; then
            echo -e "${GREEN}   ✅ FSM binary built successfully${NC}"
        else
            echo -e "${RED}   ❌ Build failed${NC}"
            exit 1
        fi
        echo ""
        
        echo "4️⃣ Building Docker image..."
        echo "---------------------------"
        docker build -t fsm-server-rpi58:latest -f fsm/Dockerfile.rpi58 fsm/
        if [ $? -eq 0 ]; then
            echo -e "${GREEN}   ✅ Docker image built successfully${NC}"
        else
            echo -e "${RED}   ❌ Docker build failed${NC}"
            exit 1
        fi
        echo ""
        
        echo "5️⃣ Restarting FSM pod..."
        echo "-----------------------"
        kubectl rollout restart deployment/fsm-server-rpi58 -n "$NAMESPACE"
        kubectl rollout status deployment/fsm-server-rpi58 -n "$NAMESPACE" --timeout=120s
        if [ $? -eq 0 ]; then
            echo -e "${GREEN}   ✅ FSM pod restarted successfully${NC}"
        else
            echo -e "${RED}   ❌ Pod restart failed${NC}"
            exit 1
        fi
        echo ""
        
        echo -e "${GREEN}✅ Rebuild complete!${NC}"
        echo ""
        echo "💡 Run './test/test_explanation_learning.sh check' to verify"
        ;;
        
    *)
        echo "Usage: $0 {test|check|diagnose|rebuild}"
        echo ""
        echo "Modes:"
        echo "  test     - Complete a goal and verify explanation learning (default)"
        echo "  check    - Quick check of explanation learning status"
        echo "  diagnose - Thorough diagnosis of deployment"
        echo "  rebuild  - Rebuild FSM with explanation learning and restart"
        exit 1
        ;;
esac
