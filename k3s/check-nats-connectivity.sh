#!/bin/bash

# Script to check NATS connectivity from services

echo "🔍 Checking NATS Connectivity"
echo "============================"
echo

NAMESPACE="agi"

# Check if NATS pod is running
echo "1. Checking NATS pod status..."
NATS_POD=$(kubectl get pods -n $NAMESPACE -l app=nats -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
if [ -z "$NATS_POD" ]; then
    echo "❌ NATS pod not found"
    exit 1
fi

echo "✅ NATS pod: $NATS_POD"
kubectl get pods -n $NAMESPACE -l app=nats
echo

# Check NATS service
echo "2. Checking NATS service..."
kubectl get svc -n $NAMESPACE nats
echo

# Test DNS resolution
echo "3. Testing DNS resolution from a pod..."
kubectl run -n $NAMESPACE nats-test --rm -i --restart=Never --image=busybox -- nslookup nats.agi.svc.cluster.local 2>&1 || echo "DNS test failed"
echo

# Test connectivity from HDN pod
echo "4. Testing NATS connectivity from HDN pod..."
HDN_POD=$(kubectl get pods -n $NAMESPACE -l app=hdn-server-rpi58 -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
if [ -n "$HDN_POD" ]; then
    echo "Testing from HDN pod: $HDN_POD"
    kubectl exec -n $NAMESPACE $HDN_POD -- sh -c "nc -zv nats.agi.svc.cluster.local 4222 2>&1 || echo 'Connection test failed'" 2>&1
else
    echo "⚠️  HDN pod not found"
fi
echo

# Test connectivity from FSM pod
echo "5. Testing NATS connectivity from FSM pod..."
FSM_POD=$(kubectl get pods -n $NAMESPACE -l app=fsm-server-rpi58 -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
if [ -n "$FSM_POD" ]; then
    echo "Testing from FSM pod: $FSM_POD"
    kubectl exec -n $NAMESPACE $FSM_POD -- sh -c "nc -zv nats.agi.svc.cluster.local 4222 2>&1 || echo 'Connection test failed'" 2>&1
else
    echo "⚠️  FSM pod not found"
fi
echo

# Check service logs for NATS connection errors
echo "6. Checking HDN logs for NATS connection messages..."
if [ -n "$HDN_POD" ]; then
    echo "Recent HDN logs (last 100 lines, filtering for NATS):"
    kubectl logs -n $NAMESPACE $HDN_POD --tail=100 | grep -i "nats" | tail -10 || echo "  (no NATS messages found)"
    echo
    echo "HDN startup logs (first 50 lines):"
    kubectl logs -n $NAMESPACE $HDN_POD --tail=200 | head -50 | grep -E "(NATS|nats|Starting|Subscribed|unavailable)" || echo "  (checking all startup logs...)"
else
    echo "⚠️  HDN pod not found"
fi
echo

echo "7. Checking FSM logs for NATS connection messages..."
if [ -n "$FSM_POD" ]; then
    echo "Recent FSM logs (last 100 lines, filtering for NATS):"
    kubectl logs -n $NAMESPACE $FSM_POD --tail=100 | grep -i "nats" | tail -10 || echo "  (no NATS messages found)"
    echo
    echo "FSM startup logs (first 50 lines):"
    kubectl logs -n $NAMESPACE $FSM_POD --tail=200 | head -50 | grep -E "(NATS|nats|Connecting|Subscribed|Failed)" || echo "  (checking all startup logs...)"
else
    echo "⚠️  FSM pod not found"
fi
echo

echo "8. Checking active NATS connections..."
# Port-forward to NATS monitoring
kubectl port-forward -n $NAMESPACE svc/nats 8223:8223 >/dev/null 2>&1 &
PF_PID=$!
sleep 3
CONN_COUNT=$(curl -s http://localhost:8223/connz 2>/dev/null | jq -r '.connections | length' 2>/dev/null || echo "unknown")
if [ "$CONN_COUNT" != "unknown" ] && [ -n "$CONN_COUNT" ]; then
    echo "✅ Active NATS connections: $CONN_COUNT"
    if [ "$CONN_COUNT" -gt 0 ]; then
        echo "Connection details:"
        curl -s http://localhost:8223/connz 2>/dev/null | jq -r '.connections[] | "  - \(.name // "unnamed"): \(.ip):\(.port) (CID: \(.cid))"' 2>/dev/null || echo "  (could not parse connection details)"
    else
        echo "⚠️  No active connections to NATS"
    fi
else
    echo "⚠️  Could not retrieve connection count"
fi
kill $PF_PID 2>/dev/null
wait $PF_PID 2>/dev/null
echo

echo "============================"
echo "✅ Diagnostics complete"
echo
echo "If services show connection errors, check:"
echo "  1. NATS pod is running: kubectl get pods -n agi -l app=nats"
echo "  2. NATS service exists: kubectl get svc -n agi nats"
echo "  3. Service logs: kubectl logs -n agi <pod-name> | grep -i nats"
echo "  4. Restart services if needed: kubectl rollout restart deployment/<service> -n agi"

