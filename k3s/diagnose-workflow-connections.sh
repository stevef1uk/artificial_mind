#!/bin/bash
# Diagnose workflow connection issues between Kubernetes services

echo "🔍 Workflow Connection Diagnostic"
echo "=================================="
echo ""

NAMESPACE="agi"

# Function to test service connectivity
test_service_connection() {
    local service_name=$1
    local url=$2
    local from_pod=$3
    
    echo "Testing $service_name from $from_pod:"
    echo "  URL: $url"
    
    # Test connection
    if [ "$from_pod" = "monitor-ui" ]; then
        RESULT=$(kubectl exec -n $NAMESPACE deployment/monitor-ui -- timeout 5 wget -qO- --timeout=5 "$url" 2>&1)
    elif [ "$from_pod" = "hdn-server" ]; then
        RESULT=$(kubectl exec -n $NAMESPACE deployment/hdn-server-rpi58 -- timeout 5 wget -qO- --timeout=5 "$url" 2>&1)
    elif [ "$from_pod" = "fsm-server" ]; then
        RESULT=$(kubectl exec -n $NAMESPACE deployment/fsm-server-rpi58 -- timeout 5 wget -qO- --timeout=5 "$url" 2>&1)
    else
        RESULT=$(timeout 5 curl -s "$url" 2>&1)
    fi
    
    if echo "$RESULT" | grep -q "200 OK\|success\|healthy"; then
        echo "  ✅ Connection successful"
        return 0
    elif echo "$RESULT" | grep -qi "timeout\|timed out"; then
        echo "  ⏱️  Connection timeout"
        return 1
    elif echo "$RESULT" | grep -qi "refused\|connection refused"; then
        echo "  ❌ Connection refused"
        return 1
    elif echo "$RESULT" | grep -qi "no route to host\|network unreachable"; then
        echo "  ❌ Network unreachable"
        return 1
    elif echo "$RESULT" | grep -qi "name or service not known\|could not resolve"; then
        echo "  ❌ DNS resolution failed"
        return 1
    else
        echo "  ⚠️  Unexpected response: $(echo "$RESULT" | head -1)"
        return 1
    fi
}

# 1. Check service DNS resolution
echo "1. DNS Resolution Tests:"
echo ""

# Test DNS from different pods
for pod in monitor-ui hdn-server-rpi58 fsm-server-rpi58; do
    echo "Testing DNS from $pod:"
    kubectl exec -n $NAMESPACE deployment/$pod -- nslookup hdn-server-rpi58.agi.svc.cluster.local 2>&1 | grep -q "Name:" && echo "  ✅ HDN DNS resolves" || echo "  ❌ HDN DNS failed"
    kubectl exec -n $NAMESPACE deployment/$pod -- nslookup fsm-server-rpi58.agi.svc.cluster.local 2>&1 | grep -q "Name:" && echo "  ✅ FSM DNS resolves" || echo "  ❌ FSM DNS failed"
    kubectl exec -n $NAMESPACE deployment/$pod -- nslookup goal-manager.agi.svc.cluster.local 2>&1 | grep -q "Name:" && echo "  ✅ Goal Manager DNS resolves" || echo "  ❌ Goal Manager DNS failed"
    kubectl exec -n $NAMESPACE deployment/$pod -- nslookup principles-server.agi.svc.cluster.local 2>&1 | grep -q "Name:" && echo "  ✅ Principles DNS resolves" || echo "  ❌ Principles DNS failed"
    kubectl exec -n $NAMESPACE deployment/$pod -- nslookup redis.agi.svc.cluster.local 2>&1 | grep -q "Name:" && echo "  ✅ Redis DNS resolves" || echo "  ❌ Redis DNS failed"
    echo ""
done

# 2. Check service endpoints
echo "2. Service Endpoint Tests:"
echo ""

# Monitor UI -> HDN
test_service_connection "HDN Health" "http://hdn-server-rpi58.agi.svc.cluster.local:8080/health" "monitor-ui"
test_service_connection "HDN Workflows" "http://hdn-server-rpi58.agi.svc.cluster.local:8080/api/v1/hierarchical/workflows" "monitor-ui"
echo ""

# Monitor UI -> FSM
test_service_connection "FSM Health" "http://fsm-server-rpi58.agi.svc.cluster.local:8083/health" "monitor-ui"
echo ""

# Monitor UI -> Goal Manager
test_service_connection "Goal Manager" "http://goal-manager.agi.svc.cluster.local:8090/goals/agent_1/active" "monitor-ui"
echo ""

# Monitor UI -> Principles
test_service_connection "Principles Server" "http://principles-server.agi.svc.cluster.local:8080/action" "monitor-ui"
echo ""

# FSM -> HDN
test_service_connection "HDN from FSM" "http://hdn-server-rpi58.agi.svc.cluster.local:8080/health" "fsm-server"
echo ""

# HDN -> Redis
echo "Testing HDN -> Redis:"
REDIS_TEST=$(kubectl exec -n $NAMESPACE deployment/hdn-server-rpi58 -- timeout 3 redis-cli -h redis.agi.svc.cluster.local -p 6379 PING 2>&1)
if echo "$REDIS_TEST" | grep -q "PONG"; then
    echo "  ✅ Redis connection successful"
else
    echo "  ❌ Redis connection failed: $REDIS_TEST"
fi
echo ""

# FSM -> Redis
echo "Testing FSM -> Redis:"
REDIS_TEST=$(kubectl exec -n $NAMESPACE deployment/fsm-server-rpi58 -- timeout 3 redis-cli -h redis.agi.svc.cluster.local -p 6379 PING 2>&1)
if echo "$REDIS_TEST" | grep -q "PONG"; then
    echo "  ✅ Redis connection successful"
else
    echo "  ❌ Redis connection failed: $REDIS_TEST"
fi
echo ""

# 3. Check environment variables
echo "3. Service Environment Variables:"
echo ""

echo "HDN Server HDN_URL:"
kubectl exec -n $NAMESPACE deployment/hdn-server-rpi58 -- env | grep HDN_URL || echo "  ⚠️  HDN_URL not set"
echo ""

echo "FSM Server HDN_URL:"
kubectl exec -n $NAMESPACE deployment/fsm-server-rpi58 -- env | grep HDN_URL || echo "  ⚠️  HDN_URL not set"
echo ""

echo "Monitor UI HDN_URL:"
kubectl exec -n $NAMESPACE deployment/monitor-ui -- env | grep HDN_URL || echo "  ⚠️  HDN_URL not set"
echo ""

# 4. Check for connection errors in logs
echo "4. Recent Connection Errors in Logs:"
echo ""

echo "HDN Server (last 50 lines):"
kubectl -n $NAMESPACE logs deployment/hdn-server-rpi58 --tail=50 2>/dev/null | grep -i "connection\|refused\|timeout\|failed\|error" | tail -5 || echo "  No connection errors found"
echo ""

echo "FSM Server (last 50 lines):"
kubectl -n $NAMESPACE logs deployment/fsm-server-rpi58 --tail=50 2>/dev/null | grep -i "connection\|refused\|timeout\|failed\|error" | tail -5 || echo "  No connection errors found"
echo ""

echo "Monitor UI (last 50 lines):"
kubectl -n $NAMESPACE logs deployment/monitor-ui --tail=50 2>/dev/null | grep -i "connection\|refused\|timeout\|failed\|error" | tail -5 || echo "  No connection errors found"
echo ""

# 5. Check workflow status
echo "5. Workflow Status:"
echo ""

echo "Active workflows in Redis:"
ACTIVE_COUNT=$(kubectl exec -n $NAMESPACE deployment/redis -- redis-cli SCARD "active_workflows" 2>/dev/null || echo "0")
echo "  Active workflows: $ACTIVE_COUNT"
if [ "$ACTIVE_COUNT" -gt "0" ]; then
    echo "  Workflow IDs:"
    kubectl exec -n $NAMESPACE deployment/redis -- redis-cli SMEMBERS "active_workflows" 2>/dev/null | head -5 | sed 's/^/    - /'
fi
echo ""

echo "Workflow records in Redis:"
WF_KEYS=$(kubectl exec -n $NAMESPACE deployment/redis -- redis-cli KEYS "workflow:*" 2>/dev/null | wc -l | tr -d ' ')
echo "  Total workflow keys: $WF_KEYS"
echo ""

# 6. Check service status
echo "6. Service Pod Status:"
kubectl -n $NAMESPACE get pods -l 'app in (monitor-ui,hdn-server-rpi58,fsm-server-rpi58,goal-manager,principles-server)' -o wide
echo ""

# 7. Summary and recommendations
echo "=================================="
echo "📊 Summary:"
echo ""
echo "Common issues and fixes:"
echo "  1. If DNS resolution fails:"
echo "     - Check CoreDNS: kubectl get pods -n kube-system | grep coredns"
echo "     - Check service endpoints: kubectl get endpoints -n agi"
echo ""
echo "  2. If connection refused:"
echo "     - Check service is running: kubectl get pods -n agi"
echo "     - Check service selector matches pod labels"
echo "     - Check service port matches container port"
echo ""
echo "  3. If HDN_URL is localhost:"
echo "     - HDN server should use: http://hdn-server-rpi58.agi.svc.cluster.local:8080"
echo "     - Update k3s/hdn-server-rpi58.yaml and restart"
echo ""
echo "  4. If workflows are stuck:"
echo "     - Check Redis connectivity"
echo "     - Check HDN server logs for workflow errors"
echo "     - Check FSM server logs for execution errors"
echo ""

