#!/bin/bash

# Test script for LLM-based tool creation agent on Kubernetes
# This script tests the tool creation agent via Kubernetes deployment

set -e

NAMESPACE="${NAMESPACE:-agi}"
DEPLOYMENT="${DEPLOYMENT:-hdn-server-rpi58}"
# Use a different local port to avoid conflicts (8080 might be in use)
SERVICE_PORT="${SERVICE_PORT:-18080}"

echo "🧪 Testing LLM-Based Tool Creation Agent on Kubernetes"
echo "========================================================"
echo "Namespace: $NAMESPACE"
echo "Deployment: $DEPLOYMENT"
echo ""

# Check if deployment exists
if ! kubectl get deployment $DEPLOYMENT -n $NAMESPACE >/dev/null 2>&1; then
    echo "❌ Deployment $DEPLOYMENT not found in namespace $NAMESPACE"
    echo "Please ensure the HDN server is deployed:"
    echo "  kubectl apply -f k3s/hdn-server-rpi58.yaml"
    exit 1
fi

# Check if pod is running
POD_NAME=$(kubectl get pods -n $NAMESPACE -l app=$DEPLOYMENT -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || echo "")
if [ -z "$POD_NAME" ]; then
    echo "❌ No running pods found for deployment $DEPLOYMENT"
    exit 1
fi

echo "✅ Found pod: $POD_NAME"
echo ""

# Check if port is already in use
if lsof -i :$SERVICE_PORT >/dev/null 2>&1 || netstat -tuln 2>/dev/null | grep -q ":$SERVICE_PORT " || ss -tuln 2>/dev/null | grep -q ":$SERVICE_PORT "; then
    echo "⚠️  Port $SERVICE_PORT is already in use"
    echo "   Attempting to kill existing port-forward..."
    pkill -f "kubectl port-forward.*$SERVICE_PORT" 2>/dev/null || true
    sleep 2
fi

# Set up port-forward in background
echo "🔌 Setting up port-forward to pod..."
kubectl port-forward -n $NAMESPACE pod/$POD_NAME $SERVICE_PORT:8080 > /tmp/k8s-port-forward-$SERVICE_PORT.log 2>&1 &
PORT_FORWARD_PID=$!
sleep 3

# Verify port-forward process is running
if ! kill -0 $PORT_FORWARD_PID 2>/dev/null; then
    echo "❌ Port-forward process died immediately"
    cat /tmp/k8s-port-forward-$SERVICE_PORT.log 2>/dev/null || echo "No log file"
    exit 1
fi

# Cleanup function
cleanup() {
    echo ""
    echo "🧹 Cleaning up port-forward..."
    kill $PORT_FORWARD_PID 2>/dev/null || true
}
trap cleanup EXIT

# Test if port-forward is working - check for actual HDN server response
TEST_RESPONSE=$(curl -s http://localhost:$SERVICE_PORT/api/v1/tools 2>&1)
if echo "$TEST_RESPONSE" | grep -qE "(tools|404.*tools|\[\])"; then
    echo "✅ Port-forward established and HDN server responding"
elif echo "$TEST_RESPONSE" | grep -q "Hello"; then
    echo "⚠️  WARNING: Port-forward may be connecting to wrong service (got 'Hello' response)"
    echo "   Response: $TEST_RESPONSE"
    echo "   Checking if port $SERVICE_PORT is already in use..."
    lsof -i :$SERVICE_PORT 2>/dev/null || echo "   Port appears free"
    echo ""
    echo "   Trying to use service directly instead..."
    # Try using service endpoint if available
    SERVICE_NAME="hdn-server-rpi58"
    if kubectl get svc $SERVICE_NAME -n $NAMESPACE >/dev/null 2>&1; then
        SERVICE_IP=$(kubectl get svc $SERVICE_NAME -n $NAMESPACE -o jsonpath='{.spec.clusterIP}')
        echo "   Service IP: $SERVICE_IP"
        echo "   Note: You may need to run this from within the cluster or use a different port"
    fi
    echo ""
    echo "   Attempting to continue with port-forward anyway..."
elif [ -z "$TEST_RESPONSE" ]; then
    echo "❌ Port-forward failed - no response from server"
    echo "   Check if port $SERVICE_PORT is available and pod is running"
    kill $PORT_FORWARD_PID 2>/dev/null || true
    exit 1
else
    echo "⚠️  Unexpected response: $TEST_RESPONSE"
fi

API_URL="http://localhost:$SERVICE_PORT"
echo "✅ Using API URL: $API_URL"
echo ""

# Test 1: Execute a task that generates reusable code
echo "📝 Test 1: Executing task that should generate reusable code"
echo "Task: Parse and transform JSON data"
echo ""

# First verify the endpoint is accessible
echo "🔍 Verifying API endpoint..."
HEALTH_CHECK=$(curl -s "$API_URL/api/v1/tools" 2>&1)
if echo "$HEALTH_CHECK" | grep -q "tools\|\[\]"; then
    echo "✅ API is accessible"
else
    echo "⚠️  API check response: $HEALTH_CHECK"
fi
echo ""

# Try intelligent/execute first, fallback to interpret/execute if not available
ENDPOINT="$API_URL/api/v1/intelligent/execute"
TEST_ENDPOINT=$(curl -s -o /dev/null -w "%{http_code}" -X POST "$ENDPOINT" -H "Content-Type: application/json" -d '{"test":true}' 2>&1)

if [ "$TEST_ENDPOINT" = "404" ] || [ "$TEST_ENDPOINT" = "000" ]; then
    echo "⚠️  /api/v1/intelligent/execute not available (HTTP $TEST_ENDPOINT)"
    echo "🔄 Trying /api/v1/interpret/execute instead..."
    ENDPOINT="$API_URL/api/v1/interpret/execute"
    USE_INTERPRET=true
else
    USE_INTERPRET=false
fi
echo ""

TASK_RESPONSE=$(curl -s -w "\nHTTP_STATUS:%{http_code}" -X POST "$ENDPOINT" \
  -H "Content-Type: application/json" \
  -d "$(if [ "$USE_INTERPRET" = "true" ]; then
    echo '{
      "input": "Create a Python function that parses JSON data and transforms it by extracting specific fields and normalizing the structure. Input: {\"name\": \"test\", \"value\": 123}",
      "context": {}
    }'
  else
    echo '{
      "task_name": "ParseJSONData",
      "description": "Create a Python function that parses JSON data and transforms it by extracting specific fields and normalizing the structure",
      "context": {
        "input": "{\"name\": \"test\", \"value\": 123}"
      },
      "language": "python",
      "force_regenerate": false,
      "max_retries": 2,
      "timeout": 60
    }'
  fi)" 2>&1)

HTTP_STATUS=$(echo "$TASK_RESPONSE" | grep "HTTP_STATUS:" | cut -d: -f2)
TASK_BODY=$(echo "$TASK_RESPONSE" | grep -v "HTTP_STATUS:")

echo "HTTP Status: ${HTTP_STATUS:-unknown}"
echo "Response:"
echo "$TASK_BODY" | jq '.' 2>/dev/null || echo "$TASK_BODY"
echo ""

SUCCESS=$(echo "$TASK_BODY" | jq -r '.success // false' 2>/dev/null || echo "false")

if [ "$SUCCESS" = "true" ]; then
    echo "✅ Task executed successfully"
    echo ""
    echo "🔍 Checking logs for tool creation agent activity..."
    echo ""
    
    # Check logs for tool creation activity
    echo "Recent tool creation logs:"
    kubectl logs -n $NAMESPACE $POD_NAME --tail=200 | grep -E "(TOOL-CREATOR|considerToolCreation|isCodeGeneralEnoughForTool|LLM.*recommends)" | tail -10 || echo "No tool creation logs found yet"
    echo ""
else
    echo "❌ Task execution failed"
    ERROR=$(echo "$TASK_RESPONSE" | jq -r '.error // "unknown error"' 2>/dev/null || echo "unknown error")
    echo "Error: $ERROR"
    echo ""
    echo "Checking pod logs for errors:"
    kubectl logs -n $NAMESPACE $POD_NAME --tail=50 | grep -i error | tail -5
    exit 1
fi

# Wait a bit for tool creation to complete
echo "⏳ Waiting 5 seconds for tool creation to complete..."
sleep 5

# Test 2: Check if any tools were created
echo "📋 Test 2: Checking if new tools were created"
echo ""

TOOLS_RESPONSE=$(curl -s -X GET "$API_URL/api/v1/tools" \
  -H "Content-Type: application/json")

echo "Agent-created tools:"
echo "$TOOLS_RESPONSE" | jq '.tools[] | select(.created_by == "agent") | {id: .id, name: .name, description: .description, exec_type: .exec.type}' 2>/dev/null || echo "No agent-created tools found"
echo ""

# Test 3: Execute another task
echo "📝 Test 3: Executing another task (Data Transformer)"
echo ""

TASK2_RESPONSE=$(curl -s -X POST "$API_URL/api/v1/intelligent/execute" \
  -H "Content-Type: application/json" \
  -d '{
    "task_name": "DataTransformer",
    "description": "Create a Python function that transforms data structures and normalizes formats with error handling",
    "context": {},
    "language": "python",
    "force_regenerate": true,
    "max_retries": 2,
    "timeout": 60
  }')

SUCCESS2=$(echo "$TASK2_RESPONSE" | jq -r '.success // false' 2>/dev/null || echo "false")

if [ "$SUCCESS2" = "true" ]; then
    echo "✅ Second task executed successfully"
else
    echo "⚠️  Second task failed (this is okay for testing)"
fi

# Wait for tool creation
sleep 5

# Final check of logs
echo ""
echo "📊 Final Tool Creation Logs:"
echo "============================"
kubectl logs -n $NAMESPACE $POD_NAME --tail=300 | grep -E "(TOOL-CREATOR|considerToolCreation)" | tail -20 || echo "No tool creation logs found"

echo ""
echo "🎯 Test Summary"
echo "==============="
echo "1. ✅ Executed tasks that should generate reusable code"
echo "2. 🔍 Tool creation agent should have evaluated the code"
echo "3. 📋 Check tools list for agent-created tools"
echo ""
echo "To monitor logs in real-time:"
echo "  kubectl logs -n $NAMESPACE $POD_NAME -f | grep TOOL-CREATOR"
echo ""
echo "To check all agent-created tools:"
echo "  kubectl port-forward -n $NAMESPACE pod/$POD_NAME 8080:8080"
echo "  curl http://localhost:8080/api/v1/tools | jq '.tools[] | select(.created_by == \"agent\")'"

