#!/bin/bash

# Intelligence test script for Kubernetes deployment

NAMESPACE="${K8S_NAMESPACE:-agi}"
HDN_URL="${HDN_URL:-http://localhost:8081}"

echo "🧠 Intelligence Test for Kubernetes"
echo "==================================="
echo ""

# Check kubectl
if ! command -v kubectl &> /dev/null; then
  echo "❌ kubectl not found. Please install kubectl."
  exit 1
fi

# Get pod names (try multiple label selectors)
HDN_POD="${HDN_POD:-$(kubectl get pods -n "$NAMESPACE" -l app=hdn-server -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)}"
if [ -z "$HDN_POD" ]; then
  HDN_POD=$(kubectl get pods -n "$NAMESPACE" -l 'app=hdn-server-rpi58' -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
fi
if [ -z "$HDN_POD" ]; then
  HDN_POD=$(kubectl get pods -n "$NAMESPACE" -o jsonpath='{.items[?(@.metadata.name=~"hdn.*")].metadata.name}' 2>/dev/null | awk '{print $1}')
fi

FSM_POD="${FSM_POD:-$(kubectl get pods -n "$NAMESPACE" -l app=fsm-server -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)}"
if [ -z "$FSM_POD" ]; then
  FSM_POD=$(kubectl get pods -n "$NAMESPACE" -l 'app=fsm-server-rpi58' -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
fi
if [ -z "$FSM_POD" ]; then
  FSM_POD=$(kubectl get pods -n "$NAMESPACE" -o jsonpath='{.items[?(@.metadata.name=~"fsm.*")].metadata.name}' 2>/dev/null | awk '{print $1}')
fi

REDIS_POD="${REDIS_POD:-$(kubectl get pods -n "$NAMESPACE" -l app=redis -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)}"
if [ -z "$REDIS_POD" ]; then
  REDIS_POD=$(kubectl get pods -n "$NAMESPACE" -o jsonpath='{.items[?(@.metadata.name=~"redis.*")].metadata.name}' 2>/dev/null | awk '{print $1}')
fi

if [ -z "$HDN_POD" ]; then
  echo "❌ HDN pod not found in namespace $NAMESPACE"
  echo "   Check: kubectl get pods -n $NAMESPACE"
  exit 1
fi

echo "Found pods:"
echo "  HDN: $HDN_POD"
[ -n "$FSM_POD" ] && echo "  FSM: $FSM_POD"
[ -n "$REDIS_POD" ] && echo "  Redis: $REDIS_POD"
echo ""

# Test 1: Check if port-forward is needed
echo "1. Checking service accessibility..."
if ! curl -s --max-time 2 "${HDN_URL}/health" > /dev/null 2>&1; then
  echo "   ⚠️  HDN not accessible at $HDN_URL"
  echo "   Setting up port-forward..."
  echo "   Run this in another terminal:"
  echo "   kubectl port-forward -n $NAMESPACE svc/hdn-server 8081:8081"
  echo ""
  read -p "Press Enter after setting up port-forward, or Ctrl+C to exit..."
fi

# Test 2: Simple code generation
echo "2. Testing code generation..."
echo "   This may take 30-60 seconds..."
echo ""

RESPONSE=$(curl -s --max-time 120 -X POST "${HDN_URL}/api/v1/intelligent/execute" \
  -H "Content-Type: application/json" \
  -d '{
    "task_name": "test_k8s",
    "description": "Print hello world in Python",
    "language": "python"
  }')

if echo "$RESPONSE" | grep -q "success"; then
  echo "   ✅ Code generation successful"
  RETRIES=$(echo "$RESPONSE" | jq -r '.retry_count // 0' 2>/dev/null || echo "?")
  echo "   Retries: $RETRIES"
else
  echo "   ⚠️  Response: $(echo "$RESPONSE" | head -c 200)"
fi
echo ""

# Test 3: Check logs for intelligence
echo "3. Checking HDN logs for intelligence messages..."
INTELLIGENCE_COUNT=$(kubectl logs -n "$NAMESPACE" "$HDN_POD" --tail=200 2>/dev/null | grep -c "INTELLIGENCE\|learned\|prevention" || echo "0")
echo "   Found $INTELLIGENCE_COUNT intelligence-related log entries"
if [ "$INTELLIGENCE_COUNT" -gt "0" ]; then
  echo "   ✅ Intelligence is active!"
  echo ""
  echo "   Recent intelligence messages:"
  kubectl logs -n "$NAMESPACE" "$HDN_POD" --tail=50 2>/dev/null | grep -i "intelligence\|learned\|prevention" | tail -3 | sed 's/^/      /'
else
  echo "   ℹ️  No intelligence messages yet (may need more executions)"
fi
echo ""

# Test 4: Check Redis learning data
if [ -n "$REDIS_POD" ]; then
  echo "4. Checking Redis for learning data..."
  PATTERNS=$(kubectl exec -n "$NAMESPACE" "$REDIS_POD" -- redis-cli KEYS "failure_pattern:*" 2>/dev/null | wc -l | tr -d ' ')
  STRATEGIES=$(kubectl exec -n "$NAMESPACE" "$REDIS_POD" -- redis-cli KEYS "codegen_strategy:*" 2>/dev/null | wc -l | tr -d ' ')
  echo "   Failure patterns: $PATTERNS"
  echo "   Strategies: $STRATEGIES"
  if [ "$PATTERNS" -gt "0" ] || [ "$STRATEGIES" -gt "0" ]; then
    echo "   ✅ Learning data found!"
  else
    echo "   ℹ️  No learning data yet"
  fi
else
  echo "4. Redis pod not found, skipping Redis check"
fi
echo ""

echo "==================================="
echo "To watch logs in real-time:"
echo "  kubectl logs -n $NAMESPACE -f $HDN_POD | grep -i intelligence"
echo ""

