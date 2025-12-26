#!/bin/bash
# Check if services are overloaded and causing Monitor UI timeouts

echo "🔍 Service Overload Diagnostic"
echo "=============================="
echo ""

# Check Monitor UI pod status
echo "1. Monitor UI Pod Status:"
kubectl -n agi get pods -l app=monitor-ui -o wide
echo ""

# Check Monitor UI resource usage
echo "2. Monitor UI Resource Usage:"
MONITOR_POD=$(kubectl -n agi get pods -l app=monitor-ui -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
if [ -n "$MONITOR_POD" ]; then
    kubectl -n agi top pod $MONITOR_POD 2>/dev/null || echo "   Metrics not available (metrics-server may not be installed)"
    echo ""
    echo "   Resource Limits:"
    kubectl -n agi get pod $MONITOR_POD -o jsonpath='{.spec.containers[0].resources}' | jq '.' 2>/dev/null || echo "   Limits: 512Mi memory, 500m CPU"
else
    echo "   Monitor UI pod not found"
fi
echo ""

# Check Monitor UI logs for timeout errors
echo "3. Recent Monitor UI Timeout Errors:"
kubectl -n agi logs deployment/monitor-ui --tail=100 2>/dev/null | grep -i "timeout\|Timeout\|TIMEOUT" | tail -10 || echo "   No timeout errors in recent logs"
echo ""

# Check backend service response times
echo "4. Backend Service Response Times:"
echo "   Testing service endpoints (5 second timeout)..."
echo ""

check_service() {
    local name=$1
    local url=$2
    local start=$(date +%s%N)
    local response=$(timeout 5 curl -s -o /dev/null -w "%{http_code}" "$url" 2>/dev/null || echo "TIMEOUT")
    local end=$(date +%s%N)
    local duration=$(( (end - start) / 1000000 ))
    
    if [ "$response" = "TIMEOUT" ]; then
        echo "   ❌ $name: TIMEOUT (>5s)"
    elif [ "$response" = "200" ] || [ "$response" = "404" ]; then
        echo "   ✅ $name: ${duration}ms (HTTP $response)"
    else
        echo "   ⚠️  $name: ${duration}ms (HTTP $response)"
    fi
}

# Test services from within cluster
echo "   Testing from within cluster..."
HDN_RESP=$(kubectl exec -n agi deployment/monitor-ui -- timeout 5 wget -qO- --timeout=5 http://hdn-server-rpi58.agi.svc.cluster.local:8080/health 2>/dev/null && echo "OK" || echo "TIMEOUT")
if [ "$HDN_RESP" = "OK" ]; then
    echo "   ✅ HDN Server: Responding"
else
    echo "   ❌ HDN Server: TIMEOUT or ERROR"
fi

FSM_RESP=$(kubectl exec -n agi deployment/monitor-ui -- timeout 5 wget -qO- --timeout=5 http://fsm-server-rpi58.agi.svc.cluster.local:8083/health 2>/dev/null && echo "OK" || echo "TIMEOUT")
if [ "$FSM_RESP" = "OK" ]; then
    echo "   ✅ FSM Server: Responding"
else
    echo "   ❌ FSM Server: TIMEOUT or ERROR"
fi

GOAL_RESP=$(kubectl exec -n agi deployment/monitor-ui -- timeout 5 wget -qO- --timeout=5 http://goal-manager.agi.svc.cluster.local:8090/goals/agent_1/active 2>/dev/null && echo "OK" || echo "TIMEOUT")
if [ "$GOAL_RESP" = "OK" ]; then
    echo "   ✅ Goal Manager: Responding"
else
    echo "   ❌ Goal Manager: TIMEOUT or ERROR"
fi

PRINCIPLES_RESP=$(kubectl exec -n agi deployment/monitor-ui -- timeout 5 wget -qO- --timeout=5 --post-data='{}' --header='Content-Type: application/json' http://principles-server.agi.svc.cluster.local:8080/action 2>/dev/null && echo "OK" || echo "TIMEOUT")
if [ "$PRINCIPLES_RESP" = "OK" ]; then
    echo "   ✅ Principles Server: Responding"
else
    echo "   ❌ Principles Server: TIMEOUT or ERROR"
fi

REDIS_RESP=$(kubectl exec -n agi deployment/redis -- timeout 2 redis-cli PING 2>/dev/null || echo "TIMEOUT")
if [ "$REDIS_RESP" = "PONG" ]; then
    echo "   ✅ Redis: Responding"
else
    echo "   ❌ Redis: TIMEOUT or ERROR"
fi

NEO4J_RESP=$(kubectl exec -n agi deployment/monitor-ui -- timeout 5 wget -qO- --timeout=5 http://neo4j.agi.svc.cluster.local:7474 2>/dev/null && echo "OK" || echo "TIMEOUT")
if [ "$NEO4J_RESP" = "OK" ]; then
    echo "   ✅ Neo4j: Responding"
else
    echo "   ❌ Neo4j: TIMEOUT or ERROR"
fi

WEAVIATE_RESP=$(kubectl exec -n agi deployment/monitor-ui -- timeout 5 wget -qO- --timeout=5 http://weaviate.agi.svc.cluster.local:8080/v1/meta 2>/dev/null && echo "OK" || echo "TIMEOUT")
if [ "$WEAVIATE_RESP" = "OK" ]; then
    echo "   ✅ Weaviate: Responding"
else
    echo "   ❌ Weaviate: TIMEOUT or ERROR"
fi

NATS_RESP=$(kubectl exec -n agi deployment/monitor-ui -- timeout 5 wget -qO- --timeout=5 http://nats.agi.svc.cluster.local:8223/varz 2>/dev/null && echo "OK" || echo "TIMEOUT")
if [ "$NATS_RESP" = "OK" ]; then
    echo "   ✅ NATS: Responding"
else
    echo "   ❌ NATS: TIMEOUT or ERROR"
fi

echo ""

# Check backend service resource usage
echo "5. Backend Service Resource Usage:"
echo "   HDN Server:"
kubectl -n agi top pod -l app=hdn-server-rpi58 2>/dev/null || echo "   Metrics not available"
echo "   FSM Server:"
kubectl -n agi top pod -l app=fsm-server-rpi58 2>/dev/null || echo "   Metrics not available"
echo "   Goal Manager:"
kubectl -n agi top pod -l app=goal-manager 2>/dev/null || echo "   Metrics not available"
echo "   Redis:"
kubectl -n agi top pod -l app=redis 2>/dev/null || echo "   Metrics not available"
echo ""

# Check Redis memory usage
echo "6. Redis Memory Usage:"
REDIS_MEMORY=$(kubectl exec -n agi deployment/redis -- redis-cli INFO memory 2>/dev/null | grep used_memory_human | cut -d: -f2 | tr -d '\r' || echo "unknown")
echo "   Used Memory: $REDIS_MEMORY"
REDIS_KEYS=$(kubectl exec -n agi deployment/redis -- redis-cli DBSIZE 2>/dev/null || echo "unknown")
echo "   Total Keys: $REDIS_KEYS"
echo ""

# Check for stuck/long-running operations
echo "7. Long-Running Operations:"
echo "   Active workflows:"
ACTIVE_WF=$(kubectl exec -n agi deployment/redis -- redis-cli KEYS "workflow:*" 2>/dev/null | wc -l | tr -d ' ')
echo "   $ACTIVE_WF workflows in Redis"
echo "   Active goals:"
ACTIVE_GOALS=$(kubectl exec -n agi deployment/fsm-server-rpi58 -- timeout 5 wget -qO- http://goal-manager.agi.svc.cluster.local:8090/goals/agent_1/active 2>/dev/null | jq '. | length' 2>/dev/null || echo "unknown")
echo "   $ACTIVE_GOALS active goals"
echo ""

# Check Monitor UI endpoint directly
echo "8. Monitor UI Health Endpoint:"
MONITOR_HEALTH=$(kubectl exec -n agi deployment/monitor-ui -- timeout 3 wget -qO- --timeout=3 http://localhost:8082/health 2>/dev/null && echo "OK" || echo "TIMEOUT")
if [ "$MONITOR_HEALTH" = "OK" ]; then
    echo "   ✅ Monitor UI /health: Responding"
else
    echo "   ❌ Monitor UI /health: TIMEOUT or ERROR"
fi

MONITOR_STATUS=$(kubectl exec -n agi deployment/monitor-ui -- timeout 10 wget -qO- --timeout=10 http://localhost:8082/api/status 2>/dev/null | jq -r '.overall' 2>/dev/null || echo "TIMEOUT")
if [ "$MONITOR_STATUS" != "TIMEOUT" ]; then
    echo "   Monitor UI /api/status: $MONITOR_STATUS"
else
    echo "   ❌ Monitor UI /api/status: TIMEOUT (>10s)"
fi
echo ""

# Check for connection pool exhaustion
echo "9. Connection Issues:"
echo "   Checking for connection errors in logs..."
kubectl -n agi logs deployment/monitor-ui --tail=200 2>/dev/null | grep -i "connection\|refused\|reset\|broken" | tail -5 || echo "   No connection errors found"
echo ""

# Summary
echo "=============================="
echo "📊 Summary:"
echo ""
echo "If services are timing out:"
echo "  1. Check if Monitor UI pod is hitting resource limits (CPU/Memory)"
echo "  2. Check if backend services are slow to respond"
echo "  3. Check if Redis is overloaded with too many keys"
echo "  4. Consider increasing Monitor UI resource limits"
echo "  5. Consider making service checks parallel instead of sequential"
echo ""
echo "Quick fixes:"
echo "  - Restart Monitor UI: kubectl rollout restart deployment/monitor-ui -n agi"
echo "  - Check logs: kubectl -n agi logs -f deployment/monitor-ui"
echo "  - Scale up resources: Edit k3s/monitor-ui.yaml and increase limits"
echo ""

