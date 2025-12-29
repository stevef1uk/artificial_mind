#!/bin/bash

# Quick intelligence test - Kubernetes version

NAMESPACE="${K8S_NAMESPACE:-agi}"
HDN_URL="${HDN_URL:-http://localhost:8081}"
FSM_URL="${FSM_URL:-http://localhost:8083}"

echo "🧠 Quick Intelligence Test (Kubernetes)"
echo "======================================"
echo ""

# Check if kubectl is available
if ! command -v kubectl &> /dev/null; then
  echo "❌ kubectl not found. Please install kubectl to test Kubernetes deployment."
  exit 1
fi

# Test 1: Check if pods are running
echo "1. Checking Kubernetes pods..."

# Try multiple label selectors (different deployments use different labels)
HDN_POD=$(kubectl get pods -n "$NAMESPACE" -l app=hdn-server -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
if [ -z "$HDN_POD" ]; then
  # Try alternative label patterns
  HDN_POD=$(kubectl get pods -n "$NAMESPACE" -l 'app=hdn-server-rpi58' -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
fi
if [ -z "$HDN_POD" ]; then
  # Try finding by name pattern
  HDN_POD=$(kubectl get pods -n "$NAMESPACE" -o jsonpath='{.items[?(@.metadata.name=~"hdn.*")].metadata.name}' 2>/dev/null | awk '{print $1}')
fi

FSM_POD=$(kubectl get pods -n "$NAMESPACE" -l app=fsm-server -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
if [ -z "$FSM_POD" ]; then
  FSM_POD=$(kubectl get pods -n "$NAMESPACE" -l 'app=fsm-server-rpi58' -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
fi
if [ -z "$FSM_POD" ]; then
  FSM_POD=$(kubectl get pods -n "$NAMESPACE" -o jsonpath='{.items[?(@.metadata.name=~"fsm.*")].metadata.name}' 2>/dev/null | awk '{print $1}')
fi

if [ -n "$HDN_POD" ]; then
  HDN_STATUS=$(kubectl get pod "$HDN_POD" -n "$NAMESPACE" -o jsonpath='{.status.phase}' 2>/dev/null)
  echo "   HDN pod: $HDN_POD ($HDN_STATUS)"
  if [ "$HDN_STATUS" = "Running" ]; then
    echo "   ✅ HDN server: Running"
  else
    echo "   ⚠️  HDN server: $HDN_STATUS"
  fi
else
  echo "   ❌ HDN pod not found"
  echo "      Available pods in namespace $NAMESPACE:"
  kubectl get pods -n "$NAMESPACE" -o wide 2>/dev/null | head -10
  echo ""
  echo "      Please update the script with the correct label selector or pod name"
  echo "      Or set HDN_POD environment variable: export HDN_POD=<pod-name>"
  exit 1
fi

if [ -n "$FSM_POD" ]; then
  FSM_STATUS=$(kubectl get pod "$FSM_POD" -n "$NAMESPACE" -o jsonpath='{.status.phase}' 2>/dev/null)
  echo "   FSM pod: $FSM_POD ($FSM_STATUS)"
  if [ "$FSM_STATUS" = "Running" ]; then
    echo "   ✅ FSM server: Running"
  else
    echo "   ⚠️  FSM server: $FSM_STATUS"
  fi
fi
echo ""

# Test 2: Check if services are accessible (may need port-forward)
echo "2. Checking service accessibility..."
if curl -s --max-time 2 "${HDN_URL}/health" > /dev/null 2>&1; then
  echo "   ✅ HDN service: Accessible at $HDN_URL"
else
  echo "   ⚠️  HDN service: Not accessible at $HDN_URL"
  echo "      You may need to port-forward:"
  echo "      kubectl port-forward -n $NAMESPACE svc/hdn-server 8081:8081 &"
  echo "      Or use: HDN_URL=http://<node-ip>:<node-port>"
fi

if curl -s --max-time 2 "${FSM_URL}/health" > /dev/null 2>&1; then
  echo "   ✅ FSM server: Running"
else
  echo "   ⚠️  FSM server: Not accessible (some tests skipped)"
fi
echo ""

# Test 2: Simple code generation (with timeout)
echo "2. Testing code generation (30s timeout)..."
echo "   Generating Python hello world..."

RESPONSE=$(timeout 30 curl -s -X POST "${HDN_URL}/api/v1/intelligent/execute" \
  -H "Content-Type: application/json" \
  -d '{
    "task_name": "test_quick",
    "description": "Print hello world",
    "language": "python"
  }' 2>&1)

if echo "$RESPONSE" | grep -q "success"; then
  echo "   ✅ Code generation working"
  RETRIES=$(echo "$RESPONSE" | jq -r '.retry_count // 0' 2>/dev/null || echo "?")
  echo "   Retries: $RETRIES"
else
  echo "   ⚠️  Response: $(echo "$RESPONSE" | head -c 100)"
fi
echo ""

# Test 3: Check logs for intelligence messages
echo "3. Checking logs for intelligence messages..."
if [ -n "$HDN_POD" ]; then
  echo "   Checking HDN logs..."
  INTELLIGENCE_LOGS=$(kubectl logs -n "$NAMESPACE" "$HDN_POD" --tail=100 2>/dev/null | grep -i "intelligence\|learned\|prevention" | tail -5)
  if [ -n "$INTELLIGENCE_LOGS" ]; then
    echo "   ✅ Found intelligence messages:"
    echo "$INTELLIGENCE_LOGS" | sed 's/^/      /'
  else
    echo "   ℹ️  No intelligence messages yet (may need more executions)"
  fi
fi
echo ""
echo "   To watch logs in real-time:"
echo "   kubectl logs -n $NAMESPACE -f $HDN_POD | grep -i intelligence"
echo ""

# Test 4: Check Redis for learning data via kubectl exec
echo "4. Checking Redis for learning data..."
REDIS_POD=$(kubectl get pods -n "$NAMESPACE" -l app=redis -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
if [ -n "$REDIS_POD" ]; then
  PATTERNS=$(kubectl exec -n "$NAMESPACE" "$REDIS_POD" -- redis-cli KEYS "failure_pattern:*" 2>/dev/null | wc -l | tr -d ' ')
  STRATEGIES=$(kubectl exec -n "$NAMESPACE" "$REDIS_POD" -- redis-cli KEYS "codegen_strategy:*" 2>/dev/null | wc -l | tr -d ' ')
  echo "   Failure patterns: $PATTERNS"
  echo "   Strategies: $STRATEGIES"
  if [ "$PATTERNS" -gt "0" ] || [ "$STRATEGIES" -gt "0" ]; then
    echo "   ✅ Learning data found!"
  else
    echo "   ℹ️  No learning data yet (run more code generation tasks)"
  fi
else
  echo "   ⚠️  Redis pod not found"
fi
echo ""

echo "========================="
echo "For detailed testing, see: test/test_intelligence_manual.md"

