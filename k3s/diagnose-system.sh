#!/bin/bash
# Diagnostic script for AGI Kubernetes deployment

echo "🔍 AGI System Diagnostics"
echo "=========================="
echo ""

echo "1. Pod Status:"
kubectl -n agi get pods -o wide
echo ""

echo "2. Service Endpoints:"
kubectl -n agi get endpoints
echo ""

echo "3. Active Goals Count:"
ACTIVE_GOALS=$(kubectl exec -n agi deployment/redis -- redis-cli KEYS "goal:*" 2>/dev/null | wc -l)
echo "   Found $ACTIVE_GOALS goal keys in Redis"
echo ""

echo "4. Goals Already Triggered:"
TRIGGERED=$(kubectl exec -n agi deployment/redis -- redis-cli SMEMBERS "fsm:agent_1:goals:triggered" 2>/dev/null | wc -l)
echo "   $TRIGGERED goals marked as triggered (may be stuck)"
echo ""

echo "5. Goal Manager Active Goals:"
kubectl exec -n agi deployment/fsm-server-rpi58 -- wget -qO- --timeout=5 http://goal-manager.agi.svc.cluster.local:8090/goals/agent_1/active 2>/dev/null | jq '. | length' 2>/dev/null || echo "   Could not fetch active goals"
echo ""

echo "6. Recent FSM Goal Polling Activity:"
kubectl -n agi logs deployment/fsm-server-rpi58 --tail=500 | grep -i "\[FSM\]\[Goals\]" | tail -10 || echo "   No goal polling activity found in recent logs"
echo ""

echo "7. Recent HDN Errors:"
kubectl -n agi logs deployment/hdn-server-rpi58 --tail=100 | grep -i "error\|Error\|ERROR" | tail -10 || echo "   No recent errors"
echo ""

echo "8. Neo4j Write Errors:"
kubectl -n agi logs deployment/hdn-server-rpi58 --tail=200 | grep -i "read access mode\|Writing in read" | tail -5 || echo "   No Neo4j write errors found"
echo ""

echo "9. NATS Connectivity:"
NATS_CONN=$(kubectl exec -n agi deployment/nats -- wget -qO- http://localhost:8223/connz 2>/dev/null | jq '.connections | length' 2>/dev/null || echo "0")
echo "   Active NATS connections: $NATS_CONN"
echo ""

echo "10. Tools Registered:"
TOOLS=$(kubectl exec -n agi deployment/redis -- redis-cli SMEMBERS tools:registry 2>/dev/null | wc -l)
echo "   $TOOLS tools registered"
echo ""

echo "=========================="
echo "✅ Diagnostics complete"
echo ""
echo "Common Issues:"
echo "  - If goals are stuck: Clear triggered set or restart FSM"
echo "  - If Neo4j errors: Check Neo4j pod logs"
echo "  - If no goal polling: Check FSM logs for errors"

