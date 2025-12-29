#!/bin/bash

# Diagnostic script to check if explanation learning is properly deployed

NAMESPACE="${K8S_NAMESPACE:-agi}"

echo "🔍 Diagnosing Explanation Learning Deployment"
echo "=============================================="
echo ""

# Get FSM pod
FSM_POD=$(kubectl get pods -n "$NAMESPACE" -l app=fsm-server-rpi58 -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
if [ -z "$FSM_POD" ]; then
    FSM_POD=$(kubectl get pods -n "$NAMESPACE" -l app=fsm-server -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
fi

if [ -z "$FSM_POD" ]; then
    echo "❌ FSM pod not found"
    exit 1
fi

echo "FSM Pod: $FSM_POD"
echo ""

# Check 1: When was pod started?
echo "1️⃣ Pod Information:"
echo "-------------------"
START_TIME=$(kubectl get pod -n "$NAMESPACE" "$FSM_POD" -o jsonpath='{.status.startTime}' 2>/dev/null)
echo "   Start Time: $START_TIME"
AGE=$(kubectl get pod -n "$NAMESPACE" "$FSM_POD" -o jsonpath='{.status.startTime}' 2>/dev/null | xargs -I {} date -d {} +%s 2>/dev/null || echo "unknown")
if [ "$AGE" != "unknown" ]; then
    NOW=$(date +%s)
    DIFF=$((NOW - AGE))
    echo "   Age: $DIFF seconds ($(($DIFF / 60)) minutes)"
fi
echo ""

# Check 2: Look for subscription messages in ALL logs
echo "2️⃣ Checking for NATS subscription messages..."
echo "--------------------------------------------"
ALL_LOGS=$(kubectl logs -n "$NAMESPACE" "$FSM_POD" 2>/dev/null)

if echo "$ALL_LOGS" | grep -q "Subscribed to agi.goal.achieved for explanation learning"; then
    echo "   ✅ Found: 'Subscribed to agi.goal.achieved for explanation learning'"
    echo "$ALL_LOGS" | grep "Subscribed to agi.goal.achieved for explanation learning" | head -1
else
    echo "   ❌ NOT FOUND: 'Subscribed to agi.goal.achieved for explanation learning'"
    echo "   ⚠️  This suggests the new code is NOT running"
fi

if echo "$ALL_LOGS" | grep -q "Subscribed to agi.goal.failed for explanation learning"; then
    echo "   ✅ Found: 'Subscribed to agi.goal.failed for explanation learning'"
else
    echo "   ❌ NOT FOUND: 'Subscribed to agi.goal.failed for explanation learning'"
fi

if echo "$ALL_LOGS" | grep -q "Subscribing to goal completion events for explanation learning"; then
    echo "   ✅ Found: 'Subscribing to goal completion events for explanation learning'"
else
    echo "   ❌ NOT FOUND: 'Subscribing to goal completion events for explanation learning'"
fi
echo ""

# Check 3: Look for explanation learning initialization
echo "3️⃣ Checking for explanation learning initialization..."
echo "----------------------------------------------------"
if echo "$ALL_LOGS" | grep -qi "explanation.*learning\|NewExplanationLearningFeedback"; then
    echo "   ✅ Found explanation learning references in logs"
    echo "$ALL_LOGS" | grep -i "explanation.*learning\|NewExplanationLearningFeedback" | head -3
else
    echo "   ❌ No explanation learning references found"
fi
echo ""

# Check 4: Check startup sequence
echo "4️⃣ Recent startup messages (last 50 lines)..."
echo "--------------------------------------------"
kubectl logs -n "$NAMESPACE" "$FSM_POD" --tail=50 2>/dev/null | grep -E "Starting|subscrib|NATS|explanation|FSM" | tail -10
echo ""

# Check 5: Verify binary has the code (if possible)
echo "5️⃣ Checking if binary contains explanation learning code..."
echo "----------------------------------------------------------"
if kubectl exec -n "$NAMESPACE" "$FSM_POD" -- sh -c 'strings /app/fsm-server 2>/dev/null | grep -q "ExplanationLearningFeedback"' 2>/dev/null; then
    echo "   ✅ Found 'ExplanationLearningFeedback' in binary"
elif kubectl exec -n "$NAMESPACE" "$FSM_POD" -- sh -c 'strings /app/fsm-server 2>/dev/null | grep -q "explanation_learning"' 2>/dev/null; then
    echo "   ✅ Found 'explanation_learning' in binary"
else
    echo "   ⚠️  Could not verify code in binary (binary may be stripped)"
    echo "   💡 Checking for function names..."
    if kubectl exec -n "$NAMESPACE" "$FSM_POD" -- sh -c 'strings /app/fsm-server 2>/dev/null | grep -q "EvaluateGoalCompletion"' 2>/dev/null; then
        echo "   ✅ Found 'EvaluateGoalCompletion' in binary"
    else
        echo "   ⚠️  Could not find 'EvaluateGoalCompletion' in binary"
    fi
fi
echo ""

# Check 6: Redis keys
echo "6️⃣ Checking Redis for explanation learning data..."
echo "-------------------------------------------------"
REDIS_POD=$(kubectl get pods -n "$NAMESPACE" -l app=redis -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
if [ -z "$REDIS_POD" ]; then
    REDIS_POD=$(kubectl get pods -n "$NAMESPACE" | grep redis | head -1 | awk '{print $1}')
fi

if [ -n "$REDIS_POD" ]; then
    KEYS=$(kubectl exec -n "$NAMESPACE" "$REDIS_POD" -- redis-cli KEYS "explanation_learning:*" 2>/dev/null)
    if [ -n "$KEYS" ]; then
        echo "   ✅ Found explanation learning keys:"
        echo "$KEYS" | head -10
    else
        echo "   ℹ️  No explanation learning keys found (normal if no goals completed)"
    fi
else
    echo "   ⚠️  Redis pod not found"
fi
echo ""

# Summary
echo "📋 Summary:"
echo "----------"
if echo "$ALL_LOGS" | grep -q "Subscribed to agi.goal.achieved for explanation learning"; then
    echo "   ✅ Explanation learning code appears to be running"
    echo "   💡 Try completing a goal to trigger learning feedback"
else
    echo "   ❌ Explanation learning code does NOT appear to be running"
    echo "   💡 The pod may be running old code"
    echo ""
    echo "   🔧 Next steps:"
    echo "      1. Verify you're on the correct branch: git branch"
    echo "      2. Rebuild and restart: ./test/rebuild_fsm_explanation_learning.sh"
    echo "      3. Check pod image: kubectl describe pod -n $NAMESPACE $FSM_POD | grep Image"
fi
echo ""

