#!/bin/bash

# Comprehensive tool system diagnosis for Kubernetes
# This script checks all aspects of the tool system

NAMESPACE="agi"
HDN_SERVICE="hdn-server-rpi58"
REDIS_DEPLOYMENT="redis"

echo "🔍 AGI Tool System Diagnosis"
echo "=============================="
echo

# Color codes
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Check functions
check_pod() {
    local pod_name=$1
    local status=$(kubectl get pod -n $NAMESPACE $pod_name -o jsonpath='{.status.phase}' 2>/dev/null)
    if [ "$status" = "Running" ]; then
        echo -e "${GREEN}✅${NC} Pod $pod_name is Running"
        return 0
    else
        echo -e "${RED}❌${NC} Pod $pod_name status: $status"
        return 1
    fi
}

check_service() {
    local svc_name=$1
    local endpoints=$(kubectl get endpoints -n $NAMESPACE $svc_name -o jsonpath='{.subsets[0].addresses[0].ip}' 2>/dev/null)
    if [ -n "$endpoints" ]; then
        echo -e "${GREEN}✅${NC} Service $svc_name has endpoints"
        return 0
    else
        echo -e "${RED}❌${NC} Service $svc_name has no endpoints"
        return 1
    fi
}

# 1. Check HDN Pod Status
echo "1. HDN Server Status"
echo "-------------------"
HDN_POD=$(kubectl get pods -n $NAMESPACE -l app=$HDN_SERVICE -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
if [ -z "$HDN_POD" ]; then
    echo -e "${RED}❌ HDN pod not found${NC}"
    exit 1
fi
check_pod "$HDN_POD"

# Get pod details
echo "   Pod: $HDN_POD"
RESTARTS=$(kubectl get pod -n $NAMESPACE $HDN_POD -o jsonpath='{.status.containerStatuses[0].restartCount}' 2>/dev/null)
echo "   Restarts: $RESTARTS"
AGE=$(kubectl get pod -n $NAMESPACE $HDN_POD -o jsonpath='{.metadata.creationTimestamp}' 2>/dev/null)
echo "   Age: $AGE"
echo

# 2. Check HDN Service
echo "2. HDN Service"
echo "--------------"
check_service "$HDN_SERVICE"
echo

# 3. Check Redis Connection
echo "3. Redis Connection"
echo "-------------------"
REDIS_POD=$(kubectl get pods -n $NAMESPACE -l app=$REDIS_DEPLOYMENT -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
if [ -z "$REDIS_POD" ]; then
    echo -e "${RED}❌ Redis pod not found${NC}"
else
    check_pod "$REDIS_POD"
    
    # Check Redis connectivity from HDN
    echo "   Testing Redis connection from HDN pod..."
    if kubectl exec -n $NAMESPACE $HDN_POD -- sh -c "timeout 2 redis-cli -h redis.agi.svc.cluster.local -p 6379 PING" 2>/dev/null | grep -q "PONG"; then
        echo -e "${GREEN}✅${NC} HDN can connect to Redis"
    else
        echo -e "${RED}❌${NC} HDN cannot connect to Redis"
    fi
fi
echo

# 4. Check Tools in Redis Registry
echo "4. Tools in Redis Registry"
echo "--------------------------"
if [ -n "$REDIS_POD" ]; then
    TOOL_COUNT=$(kubectl exec -n $NAMESPACE $REDIS_POD -- redis-cli SMEMBERS tools:registry 2>/dev/null | grep -v "^$" | wc -l)
    if [ "$TOOL_COUNT" -gt 0 ]; then
        echo -e "${GREEN}✅${NC} Found $TOOL_COUNT tools in registry"
        echo "   Tools:"
        kubectl exec -n $NAMESPACE $REDIS_POD -- redis-cli SMEMBERS tools:registry 2>/dev/null | grep -v "^$" | sed 's/^/     - /'
    else
        echo -e "${RED}❌${NC} No tools found in registry"
        echo "   This indicates tools were not bootstrapped on startup"
    fi
else
    echo -e "${YELLOW}⚠️${NC}  Cannot check Redis (pod not found)"
fi
echo

# 5. Check HDN Logs for Bootstrap
echo "5. HDN Bootstrap Logs"
echo "---------------------"
echo "   Checking for bootstrap messages..."
BOOTSTRAP_LOG=$(kubectl logs -n $NAMESPACE $HDN_POD --tail=100 2>/dev/null | grep -i "bootstrap\|BOOTSTRAP" | tail -5)
if [ -n "$BOOTSTRAP_LOG" ]; then
    echo -e "${GREEN}✅${NC} Found bootstrap logs:"
    echo "$BOOTSTRAP_LOG" | sed 's/^/     /'
else
    echo -e "${YELLOW}⚠️${NC}  No bootstrap logs found (may have been rotated)"
fi

echo "   Checking for tool registration messages..."
TOOL_REG_LOG=$(kubectl logs -n $NAMESPACE $HDN_POD --tail=200 2>/dev/null | grep -i "register.*tool\|REGISTER-TOOL" | tail -10)
if [ -n "$TOOL_REG_LOG" ]; then
    echo -e "${GREEN}✅${NC} Found tool registration logs:"
    echo "$TOOL_REG_LOG" | sed 's/^/     /'
else
    echo -e "${RED}❌${NC} No tool registration logs found"
fi
echo

# 6. Check HDN Environment Variables
echo "6. HDN Configuration"
echo "-------------------"
echo "   Execution Method:"
EXEC_METHOD=$(kubectl exec -n $NAMESPACE $HDN_POD -- sh -c 'echo $EXECUTION_METHOD' 2>/dev/null)
echo "     EXECUTION_METHOD=$EXEC_METHOD"

echo "   ARM64 Tools:"
ENABLE_ARM64=$(kubectl exec -n $NAMESPACE $HDN_POD -- sh -c 'echo $ENABLE_ARM64_TOOLS' 2>/dev/null)
echo "     ENABLE_ARM64_TOOLS=$ENABLE_ARM64"

echo "   Redis URL:"
REDIS_URL=$(kubectl exec -n $NAMESPACE $HDN_POD -- sh -c 'echo $REDIS_URL' 2>/dev/null)
echo "     REDIS_URL=$REDIS_URL"

echo "   HDN URL:"
HDN_URL=$(kubectl exec -n $NAMESPACE $HDN_POD -- sh -c 'echo $HDN_URL' 2>/dev/null)
echo "     HDN_URL=$HDN_URL"
echo

# 7. Test HDN API Endpoints
echo "7. HDN API Endpoints"
echo "-------------------"
# Find available port
LOCAL_PORT=""
PF_PID=""
for port in 8080 8081 8082 8083 8084 8085; do
    kubectl port-forward -n $NAMESPACE svc/$HDN_SERVICE ${port}:8080 >/dev/null 2>&1 &
    PF_PID=$!
    sleep 1
    if kill -0 $PF_PID 2>/dev/null && curl -s http://localhost:${port}/health >/dev/null 2>&1; then
        LOCAL_PORT=$port
        break
    else
        kill $PF_PID 2>/dev/null
        wait $PF_PID 2>/dev/null
        PF_PID=""
    fi
done

if [ -n "$LOCAL_PORT" ] && [ -n "$PF_PID" ]; then
    echo -e "${GREEN}✅${NC} Port-forward established on port $LOCAL_PORT"
    
    # Test health endpoint
    if curl -s http://localhost:${LOCAL_PORT}/health >/dev/null 2>&1; then
        echo -e "${GREEN}✅${NC} Health endpoint responding"
    else
        echo -e "${RED}❌${NC} Health endpoint not responding"
    fi
    
    # Test tools endpoint
    echo "   Testing /api/v1/tools endpoint..."
    TOOLS_RESPONSE=$(curl -s http://localhost:${LOCAL_PORT}/api/v1/tools 2>/dev/null)
    if [ -n "$TOOLS_RESPONSE" ] && echo "$TOOLS_RESPONSE" | jq -e '.tools' >/dev/null 2>&1; then
        TOOL_COUNT_API=$(echo "$TOOLS_RESPONSE" | jq '.tools | length' 2>/dev/null)
        echo -e "${GREEN}✅${NC} Tools API responding with $TOOL_COUNT_API tools"
    else
        echo -e "${RED}❌${NC} Tools API not responding or invalid response"
        echo "     Response: $TOOLS_RESPONSE"
    fi
    
    # Test tool discovery endpoint
    echo "   Testing /api/v1/tools/discover endpoint..."
    DISCOVER_RESPONSE=$(curl -s -X POST http://localhost:${LOCAL_PORT}/api/v1/tools/discover 2>/dev/null)
    if [ -n "$DISCOVER_RESPONSE" ] && echo "$DISCOVER_RESPONSE" | jq -e '.discovered' >/dev/null 2>&1; then
        DISCOVERED_COUNT=$(echo "$DISCOVER_RESPONSE" | jq '.discovered | length' 2>/dev/null)
        echo -e "${GREEN}✅${NC} Tool discovery found $DISCOVERED_COUNT tools"
    else
        echo -e "${YELLOW}⚠️${NC}  Tool discovery endpoint issue"
        echo "     Response: $DISCOVER_RESPONSE"
    fi
    
    # Cleanup port-forward
    kill $PF_PID 2>/dev/null
    wait $PF_PID 2>/dev/null
else
    echo -e "${RED}❌${NC} Could not establish port-forward"
fi
echo

# 8. Check Recent Tool Invocations
echo "8. Recent Tool Activity"
echo "-----------------------"
if [ -n "$REDIS_POD" ]; then
    echo "   Checking for tool invocation logs in HDN..."
    TOOL_INVOKE_LOG=$(kubectl logs -n $NAMESPACE $HDN_POD --tail=500 2>/dev/null | grep -i "invoke.*tool\|tool.*invoke" | tail -10)
    if [ -n "$TOOL_INVOKE_LOG" ]; then
        echo -e "${GREEN}✅${NC} Found tool invocation activity:"
        echo "$TOOL_INVOKE_LOG" | sed 's/^/     /'
    else
        echo -e "${YELLOW}⚠️${NC}  No recent tool invocations found"
    fi
fi
echo

# 9. Check SSH/Docker Configuration
echo "9. Execution Environment"
echo "------------------------"
if [ "$EXEC_METHOD" = "ssh" ]; then
    echo "   Execution method: SSH"
    RPI_HOST=$(kubectl exec -n $NAMESPACE $HDN_POD -- sh -c 'echo $RPI_HOST' 2>/dev/null)
    echo "     RPI_HOST=$RPI_HOST"
    
    echo "   Checking SSH keys..."
    if kubectl exec -n $NAMESPACE $HDN_POD -- test -f /root/.ssh/id_rsa 2>/dev/null; then
        echo -e "${GREEN}✅${NC} SSH private key found"
    else
        echo -e "${RED}❌${NC} SSH private key not found"
    fi
    
    if kubectl exec -n $NAMESPACE $HDN_POD -- test -f /root/.ssh/id_rsa.pub 2>/dev/null; then
        echo -e "${GREEN}✅${NC} SSH public key found"
    else
        echo -e "${RED}❌${NC} SSH public key not found"
    fi
elif [ "$EXEC_METHOD" = "docker" ]; then
    echo "   Execution method: Docker"
    echo "   Checking Docker socket..."
    if kubectl exec -n $NAMESPACE $HDN_POD -- test -S /var/run/docker.sock 2>/dev/null; then
        echo -e "${GREEN}✅${NC} Docker socket accessible"
    else
        echo -e "${RED}❌${NC} Docker socket not accessible"
    fi
else
    echo -e "${YELLOW}⚠️${NC}  Execution method: $EXEC_METHOD (unknown)"
fi
echo

# 10. Recommendations
echo "10. Recommendations"
echo "-------------------"
if [ "$TOOL_COUNT" -eq 0 ]; then
    echo -e "${YELLOW}⚠️${NC}  No tools found in registry"
    echo "   → Run: ./bootstrap-tools.sh"
    echo "   → Or:  ./register-all-tools.sh"
fi

if [ -z "$TOOL_REG_LOG" ]; then
    echo -e "${YELLOW}⚠️${NC}  No tool registration logs found"
    echo "   → Check if BootstrapSeedTools is being called"
    echo "   → Check HDN pod logs for errors"
fi

if [ "$EXEC_METHOD" = "ssh" ] && [ -z "$RPI_HOST" ]; then
    echo -e "${YELLOW}⚠️${NC}  SSH execution method but RPI_HOST not set"
    echo "   → Set RPI_HOST in HDN deployment"
fi

echo
echo "=============================="
echo "✅ Diagnosis complete"
echo
echo "Next steps:"
echo "1. If no tools: Run ./bootstrap-tools.sh or ./register-all-tools.sh"
echo "2. Check HDN logs: kubectl logs -n $NAMESPACE $HDN_POD --tail=100"
echo "3. Test tool invocation: curl -X POST http://localhost:8080/api/v1/tools/tool_http_get/invoke -d '{\"url\":\"https://example.com\"}'"





