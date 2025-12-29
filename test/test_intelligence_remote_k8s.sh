#!/bin/bash

# Intelligence test script for remote Kubernetes deployment
# Run from your Mac to test k3s on Raspberry Pi

NAMESPACE="${K8S_NAMESPACE:-agi}"
K8S_CONTEXT="${K8S_CONTEXT:-}"  # e.g., "default" or your k3s context

echo "🧠 Intelligence Test for Remote Kubernetes"
echo "=========================================="
echo ""

# Check kubectl
if ! command -v kubectl &> /dev/null; then
  echo "❌ kubectl not found. Please install kubectl."
  exit 1
fi

# Check if we can access the cluster
echo "Checking Kubernetes cluster access..."
if ! kubectl cluster-info &> /dev/null; then
  echo "❌ Cannot access Kubernetes cluster"
  echo ""
  echo "Make sure:"
  echo "  1. kubectl is configured to access your k3s cluster"
  echo "  2. You have the kubeconfig file set up"
  echo "  3. You can run: kubectl get nodes"
  exit 1
fi

echo "✅ Cluster accessible"
echo ""

# List pods to help identify them
echo "Available pods in namespace $NAMESPACE:"
kubectl get pods -n "$NAMESPACE" 2>/dev/null || {
  echo "❌ Cannot access namespace $NAMESPACE"
  echo "   Available namespaces:"
  kubectl get namespaces
  exit 1
}
echo ""

# Find pods
HDN_POD=$(kubectl get pods -n "$NAMESPACE" --no-headers 2>/dev/null | grep -E "hdn.*Running" | awk '{print $1}' | head -1)
FSM_POD=$(kubectl get pods -n "$NAMESPACE" --no-headers 2>/dev/null | grep -E "fsm.*Running" | awk '{print $1}' | head -1)
REDIS_POD=$(kubectl get pods -n "$NAMESPACE" --no-headers 2>/dev/null | grep -E "redis.*Running" | awk '{print $1}' | head -1)

if [ -z "$HDN_POD" ]; then
  echo "❌ HDN pod not found"
  echo "   Please check: kubectl get pods -n $NAMESPACE"
  exit 1
fi

echo "Found pods:"
echo "  HDN: $HDN_POD"
[ -n "$FSM_POD" ] && echo "  FSM: $FSM_POD"
[ -n "$REDIS_POD" ] && echo "  Redis: $REDIS_POD"
echo ""

# Check for port-forward
HDN_URL="${HDN_URL:-http://localhost:8081}"
echo "Checking HDN accessibility at $HDN_URL..."

if ! curl -s --max-time 2 "${HDN_URL}/health" > /dev/null 2>&1; then
  echo "⚠️  HDN not accessible locally"
  echo ""
  echo "Setting up port-forward (will run in background)..."
  echo ""
  
  # Kill any existing port-forwards
  pkill -f "kubectl.*port-forward.*hdn" 2>/dev/null
  
  # Start port-forward in background
  kubectl port-forward -n "$NAMESPACE" "$HDN_POD" 8081:8080 > /tmp/k8s-port-forward.log 2>&1 &
  PF_PID=$!
  sleep 2
  
  if kill -0 $PF_PID 2>/dev/null; then
    echo "✅ Port-forward started (PID: $PF_PID)"
    echo "   To stop: kill $PF_PID"
    echo ""
  else
    echo "❌ Port-forward failed"
    cat /tmp/k8s-port-forward.log
    exit 1
  fi
else
  echo "✅ HDN accessible"
fi
echo ""

# Test 1: Check logs for intelligence
echo "1. Checking HDN logs for intelligence messages..."
INTELLIGENCE_LOGS=$(kubectl logs -n "$NAMESPACE" "$HDN_POD" --tail=200 2>/dev/null | grep -i "intelligence\|learned\|prevention" | tail -5)
if [ -n "$INTELLIGENCE_LOGS" ]; then
  echo "   ✅ Found intelligence messages:"
  echo "$INTELLIGENCE_LOGS" | sed 's/^/      /'
else
  echo "   ℹ️  No intelligence messages yet"
  echo "   (This is normal if system hasn't executed many tasks yet)"
fi
echo ""

# Test 2: Check Redis for learning data
if [ -n "$REDIS_POD" ]; then
  echo "2. Checking Redis for learning data..."
  PATTERNS=$(kubectl exec -n "$NAMESPACE" "$REDIS_POD" -- redis-cli KEYS "failure_pattern:*" 2>/dev/null | wc -l | tr -d ' ')
  STRATEGIES=$(kubectl exec -n "$NAMESPACE" "$REDIS_POD" -- redis-cli KEYS "codegen_strategy:*" 2>/dev/null | wc -l | tr -d ' ')
  HINTS=$(kubectl exec -n "$NAMESPACE" "$REDIS_POD" -- redis-cli KEYS "prevention_hint:*" 2>/dev/null | wc -l | tr -d ' ')
  
  echo "   Failure patterns: $PATTERNS"
  echo "   Strategies: $STRATEGIES"
  echo "   Prevention hints: $HINTS"
  
  if [ "$PATTERNS" -gt "0" ] || [ "$STRATEGIES" -gt "0" ] || [ "$HINTS" -gt "0" ]; then
    echo "   ✅ Learning data found!"
  else
    echo "   ℹ️  No learning data yet"
  fi
else
  echo "2. Redis pod not found, skipping Redis check"
fi
echo ""

# Test 3: Simple code generation
echo "3. Testing code generation..."
echo "   This may take 30-60 seconds..."
echo ""

RESPONSE=$(curl -s --max-time 120 -X POST "${HDN_URL}/api/v1/intelligent/execute" \
  -H "Content-Type: application/json" \
  -d '{
    "task_name": "test_remote_intelligence",
    "description": "Print hello world in Python",
    "language": "python"
  }' 2>&1)

if echo "$RESPONSE" | grep -q "success"; then
  echo "   ✅ Code generation successful"
  RETRIES=$(echo "$RESPONSE" | jq -r '.retry_count // 0' 2>/dev/null || echo "?")
  echo "   Retries: $RETRIES"
  
  # Check for new intelligence messages
  echo ""
  echo "   Checking for new intelligence messages..."
  NEW_LOGS=$(kubectl logs -n "$NAMESPACE" "$HDN_POD" --tail=50 --since=2m 2>/dev/null | grep -i "intelligence\|learned\|prevention" | tail -3)
  if [ -n "$NEW_LOGS" ]; then
    echo "   ✅ New intelligence activity:"
    echo "$NEW_LOGS" | sed 's/^/      /'
  fi
else
  echo "   ⚠️  Response: $(echo "$RESPONSE" | head -c 200)"
fi
echo ""

echo "=========================================="
echo "Summary:"
echo "  To watch intelligence in real-time:"
echo "    kubectl logs -n $NAMESPACE -f $HDN_POD | grep -i intelligence"
echo ""
echo "  Port-forward is running (PID: $PF_PID)"
echo "  To stop port-forward: kill $PF_PID"
echo ""

