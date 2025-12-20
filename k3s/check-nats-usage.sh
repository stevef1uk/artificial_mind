#!/bin/bash

# Check which services are using NATS and what they're doing

echo "🔍 Checking NATS Usage by Services"
echo "=================================="
echo

NAMESPACE="agi"

# Get NATS connection details
echo "1. Active NATS Connections:"
kubectl port-forward -n $NAMESPACE svc/nats 8223:8223 >/dev/null 2>&1 &
PF_PID=$!
sleep 3

CONN_INFO=$(curl -s http://localhost:8223/connz 2>/dev/null)
CONN_COUNT=$(echo "$CONN_INFO" | jq -r '.connections | length' 2>/dev/null)
echo "   Total connections: $CONN_COUNT"
echo

if [ "$CONN_COUNT" -gt 0 ]; then
    echo "   Connection details:"
    echo "$CONN_INFO" | jq -r '.connections[] | "   - \(.name // "unnamed"): IP \(.ip):\(.port) | CID: \(.cid) | In: \(.in_msgs) msgs | Out: \(.out_msgs) msgs | Subscriptions: \(.subscriptions)"' 2>/dev/null
fi
echo

# Check subscriptions
echo "2. NATS Subscriptions:"
SUBS_INFO=$(curl -s http://localhost:8223/subsz 2>/dev/null)
TOTAL_SUBS=$(echo "$SUBS_INFO" | jq -r '.num_subscriptions // 0' 2>/dev/null)
echo "   Total subscriptions: $TOTAL_SUBS"
echo

if [ "$TOTAL_SUBS" -gt 0 ]; then
    echo "   Subscription details:"
    # Get detailed subscription info
    curl -s http://localhost:8223/subsz?subs=1 2>/dev/null | jq -r '.subs[]? | "   - Subject: \(.subject) | Queue: \(.queue_name // "none") | Messages: \(.msgs)"' 2>/dev/null | head -20
fi
echo

kill $PF_PID 2>/dev/null
wait $PF_PID 2>/dev/null

# Check which pods match the connection IPs
echo "3. Matching connections to pods:"
HDN_POD_IP=$(kubectl get pod -n $NAMESPACE -l app=hdn-server-rpi58 -o jsonpath='{.items[0].status.podIP}' 2>/dev/null)
FSM_POD_IP=$(kubectl get pod -n $NAMESPACE -l app=fsm-server-rpi58 -o jsonpath='{.items[0].status.podIP}' 2>/dev/null)
GOAL_POD_IP=$(kubectl get pod -n $NAMESPACE -l app=goal-manager -o jsonpath='{.items[0].status.podIP}' 2>/dev/null)

echo "   HDN pod IP: ${HDN_POD_IP:-not found}"
echo "   FSM pod IP: ${FSM_POD_IP:-not found}"
echo "   Goal Manager pod IP: ${GOAL_POD_IP:-not found}"
echo

# Check HDN logs for NATS activity
echo "4. Checking HDN NATS activity:"
HDN_POD=$(kubectl get pods -n $NAMESPACE -l app=hdn-server-rpi58 -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
if [ -n "$HDN_POD" ]; then
    echo "   Checking logs for NATS-related messages..."
    kubectl logs -n $NAMESPACE $HDN_POD --tail=200 | grep -E "(NATS|Subscribed|eventbus|agi\.events)" | tail -10 || echo "   (no NATS activity found in recent logs)"
else
    echo "   HDN pod not found"
fi
echo

# Check FSM logs for NATS activity
echo "5. Checking FSM NATS activity:"
FSM_POD=$(kubectl get pods -n $NAMESPACE -l app=fsm-server-rpi58 -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
if [ -n "$FSM_POD" ]; then
    echo "   Checking logs for NATS-related messages..."
    kubectl logs -n $NAMESPACE $FSM_POD --tail=200 | grep -E "(NATS|Subscribed|📡|agi\.events)" | tail -10 || echo "   (no NATS activity found in recent logs)"
else
    echo "   FSM pod not found"
fi
echo

# Check Goal Manager logs
echo "6. Checking Goal Manager NATS activity:"
GOAL_POD=$(kubectl get pods -n $NAMESPACE -l app=goal-manager -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
if [ -n "$GOAL_POD" ]; then
    echo "   Checking logs for NATS-related messages..."
    kubectl logs -n $NAMESPACE $GOAL_POD --tail=200 | grep -E "(NATS|nats|Connecting)" | tail -10 || echo "   (no NATS activity found in recent logs)"
else
    echo "   Goal Manager pod not found"
fi
echo

echo "=================================="
echo "✅ Analysis complete"
echo
echo "Summary:"
echo "  - NATS has $CONN_COUNT active connections"
echo "  - $TOTAL_SUBS total subscriptions"
echo
echo "If subscriptions = 0, services are connected but not subscribed to any subjects"
echo "If subscriptions > 0, check which subjects are being used"

