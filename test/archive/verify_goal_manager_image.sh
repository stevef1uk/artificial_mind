#!/bin/bash

# Verify Goal Manager is running the latest image with debug logging

NAMESPACE="${K8S_NAMESPACE:-agi}"

echo "🔍 Verifying Goal Manager Image"
echo "==============================="
echo ""

GOAL_MGR_POD=$(kubectl get pods -n "$NAMESPACE" -l app=goal-manager -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)

if [ -z "$GOAL_MGR_POD" ]; then
    echo "❌ Goal Manager pod not found!"
    exit 1
fi

echo "📦 Pod Information:"
echo "-----------------"
kubectl get pod -n "$NAMESPACE" "$GOAL_MGR_POD" -o jsonpath='{.metadata.name}' && echo ""
kubectl get pod -n "$NAMESPACE" "$GOAL_MGR_POD" -o jsonpath='{.spec.containers[0].image}' && echo " (image)"
kubectl get pod -n "$NAMESPACE" "$GOAL_MGR_POD" -o jsonpath='{.status.containerStatuses[0].imageID}' && echo " (image ID)"
echo ""

echo "🔍 Checking for Debug Logging:"
echo "------------------------------"
echo "   Looking for debug messages in recent logs..."
RECENT_LOGS=$(kubectl logs -n "$NAMESPACE" "$GOAL_MGR_POD" --tail=100 2>/dev/null | grep -E "📥|✅|🐛|\[GoalManager\]" | tail -10)
if [ -n "$RECENT_LOGS" ]; then
    echo "   ✅ Found debug messages (new code is running):"
    echo "$RECENT_LOGS" | sed 's/^/      /'
else
    echo "   ⚠️  No debug messages found (may be running old code)"
    echo ""
    echo "   Recent logs (last 20 lines):"
    kubectl logs -n "$NAMESPACE" "$GOAL_MGR_POD" --tail=20 2>/dev/null | sed 's/^/      /'
fi
echo ""

echo "🧪 Testing HTTP Endpoint:"
echo "------------------------"
echo "   Sending test POST request from external pod..."
# Use a pod that has curl/wget available
TEST_POD=$(kubectl get pods -n "$NAMESPACE" -l app=monitor-ui -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
if [ -n "$TEST_POD" ]; then
    TEST_DATA='{"description":"test goal for debug","priority":"low","context":{"test":"true","domain":"test"}}'
    TEST_RESPONSE=$(kubectl exec -n "$NAMESPACE" "$TEST_POD" -- sh -c "echo '$TEST_DATA' | wget --post-data=- --header='Content-Type: application/json' -qO- http://goal-manager.${NAMESPACE}.svc.cluster.local:8090/goal 2>&1" 2>/dev/null)
    if [ $? -eq 0 ] && [ -n "$TEST_RESPONSE" ]; then
        echo "   ✅ Test request succeeded"
        echo "   Response preview: $(echo "$TEST_RESPONSE" | head -c 80)..."
        echo ""
        echo "   Checking Goal Manager logs for debug message..."
        sleep 2
        DEBUG_LOGS=$(kubectl logs -n "$NAMESPACE" "$GOAL_MGR_POD" --tail=20 2>/dev/null | grep -E "📥|POST /goal|Received goal" | tail -5)
        if [ -n "$DEBUG_LOGS" ]; then
            echo "   ✅ Found debug messages:"
            echo "$DEBUG_LOGS" | sed 's/^/      /'
        else
            echo "   ⚠️  No debug messages found (may be old code)"
        fi
    else
        echo "   ⚠️  Test request failed"
        echo "   Response: $TEST_RESPONSE"
    fi
else
    echo "   ⚠️  No test pod available (monitor-ui not found)"
fi
echo ""

echo "💡 Summary:"
echo "----------"
echo "   If no debug messages appear:"
echo "      → Goal Manager pod is running old code"
echo "      → Need to rebuild and restart: ./test/rebuild_goal_manager.sh"
echo ""
echo "   If debug messages appear:"
echo "      → Code is up to date"
echo "      → Monitor Service may not be sending requests"
echo ""

