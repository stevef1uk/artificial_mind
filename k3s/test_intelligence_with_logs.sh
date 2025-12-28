#!/bin/bash

# Test that triggers code generation and shows intelligence debug logs

NAMESPACE="${K8S_NAMESPACE:-agi}"
HDN_POD=$(kubectl get pods -n "$NAMESPACE" --no-headers 2>/dev/null | grep -E "hdn.*Running" | awk '{print $1}' | head -1)

if [ -z "$HDN_POD" ]; then
  echo "❌ HDN pod not found"
  exit 1
fi

echo "🧠 Intelligence Test with Debug Logs"
echo "===================================="
echo ""
echo "This will:"
echo "  1. Trigger Go code generation"
echo "  2. Show intelligence debug messages from logs"
echo ""

# Get baseline log count
BASELINE=$(kubectl logs -n "$NAMESPACE" "$HDN_POD" 2>/dev/null | wc -l)
echo "📊 Baseline log lines: $BASELINE"
echo ""

# Trigger code generation
TASK_NAME="intelligence_test_$(date +%s)"

# First, find a hint that will actually be used (frequency >= 2)
echo "🔍 Finding a prevention hint with frequency >= 2..."
REDIS_POD=$(kubectl get pods -n "$NAMESPACE" --no-headers 2>/dev/null | grep -E "redis.*Running" | awk '{print $1}' | head -1)
if [ -n "$REDIS_POD" ]; then
  # Find a Go hint with frequency >= 2
  GO_HINT=$(kubectl exec -n "$NAMESPACE" "$REDIS_POD" -- redis-cli KEYS "prevention_hint:*:go" 2>/dev/null | head -1)
  if [ -n "$GO_HINT" ]; then
    PATTERN_KEY=$(echo "$GO_HINT" | sed 's/prevention_hint/failure_pattern/')
    PATTERN_DATA=$(kubectl exec -n "$NAMESPACE" "$REDIS_POD" -- redis-cli GET "$PATTERN_KEY" 2>/dev/null)
    if [ -n "$PATTERN_DATA" ]; then
      FREQ=$(echo "$PATTERN_DATA" | grep -o '"frequency":[0-9]*' | grep -o '[0-9]*' || echo "0")
      if [ "$FREQ" -ge "2" ]; then
        echo "   ✅ Found: $GO_HINT (frequency: $FREQ) - will be used"
        USE_GO=true
      else
        echo "   ⚠️  Go hint frequency too low: $FREQ (need >= 2)"
        USE_GO=false
      fi
    fi
  fi
  
  # Try Python if Go doesn't work
  if [ "${USE_GO:-false}" != "true" ]; then
    PYTHON_HINT=$(kubectl exec -n "$NAMESPACE" "$REDIS_POD" -- redis-cli KEYS "prevention_hint:*:python" 2>/dev/null | head -1)
    if [ -n "$PYTHON_HINT" ]; then
      PATTERN_KEY=$(echo "$PYTHON_HINT" | sed 's/prevention_hint/failure_pattern/')
      PATTERN_DATA=$(kubectl exec -n "$NAMESPACE" "$REDIS_POD" -- redis-cli GET "$PATTERN_KEY" 2>/dev/null)
      if [ -n "$PATTERN_DATA" ]; then
        FREQ=$(echo "$PATTERN_DATA" | grep -o '"frequency":[0-9]*' | grep -o '[0-9]*' || echo "0")
        if [ "$FREQ" -ge "2" ]; then
          echo "   ✅ Found: $PYTHON_HINT (frequency: $FREQ) - will be used"
          USE_PYTHON=true
          TEST_LANG="python"
          TEST_DESC="Create a Python program that reads a file and prints its contents"
        else
          echo "   ⚠️  Python hint frequency too low: $FREQ"
        fi
      fi
    fi
  fi
fi

# Default to Go if nothing found
TEST_LANG="${TEST_LANG:-go}"
TEST_DESC="${TEST_DESC:-Create a Go program that reads a file and prints its contents}"

echo "🚀 Triggering $TEST_LANG code generation..."
echo "   Task: $TASK_NAME"
echo "   Language: $TEST_LANG"
echo "   (This should trigger intelligence code path)"
echo ""

# Get HDN service port
HDN_SVC=$(kubectl get svc -n "$NAMESPACE" | grep hdn | head -1 | awk '{print $1}')
HDN_PORT=$(kubectl get svc -n "$NAMESPACE" "$HDN_SVC" -o jsonpath='{.spec.ports[0].port}' 2>/dev/null || echo "8080")

# Start port-forward in background
echo "   Setting up port-forward to HDN service..."
kubectl port-forward -n "$NAMESPACE" "svc/$HDN_SVC" 18080:$HDN_PORT > /dev/null 2>&1 &
PORT_FORWARD_PID=$!

# Wait for port-forward to be ready
sleep 2

# Make the request via port-forward
echo "   Making request via port-forward..."
RESPONSE=$(curl -s -w "\nHTTP_CODE:%{http_code}\nTIME:%{time_total}" -X POST http://localhost:18080/api/v1/intelligent/execute \
  -H "Content-Type: application/json" \
  -d "{
    \"task_name\": \"$TASK_NAME\",
    \"description\": \"$TEST_DESC\",
    \"language\": \"$TEST_LANG\",
    \"max_retries\": 1,
    \"timeout\": 30,
    \"priority\": \"high\"
  }" 2>&1)

HTTP_CODE=$(echo "$RESPONSE" | grep "HTTP_CODE:" | cut -d: -f2)
RESPONSE_TIME=$(echo "$RESPONSE" | grep "TIME:" | cut -d: -f2)
BODY=$(echo "$RESPONSE" | grep -v "HTTP_CODE:" | grep -v "TIME:")

echo "📋 API Response:"
if [ "$HTTP_CODE" = "200" ]; then
  echo "   ✅ HTTP 200 (Success) - Response time: ${RESPONSE_TIME}s"
  if echo "$BODY" | grep -q "success.*true"; then
    echo "   ✅ Request accepted and processing"
  fi
elif [ -n "$HTTP_CODE" ]; then
  echo "   ❌ HTTP $HTTP_CODE (Error)"
  echo "   Response: $(echo "$BODY" | head -5 | tr '\n' ' ')"
  echo ""
  echo "   Request failed - cannot test intelligence"
  exit 1
else
  echo "   ⚠️  No HTTP code received"
  echo "   Full response:"
  echo "$RESPONSE" | head -10 | sed 's/^/     /'
  echo ""
  echo "   This might indicate:"
  echo "   - Network error"
  echo "   - Pod not accessible"
  echo "   - curl not available in pod"
  exit 1
fi
echo ""

# Wait for logs to appear - poll until we find them or timeout
# If response was fast (< 5s), the request might be synchronous, so check immediately
if [ -n "$RESPONSE_TIME" ] && (( $(echo "$RESPONSE_TIME < 5" | bc -l 2>/dev/null || echo 0) )); then
  echo "⏳ Request completed quickly (${RESPONSE_TIME}s) - checking logs immediately..."
  sleep 2
else
  echo "⏳ Waiting for intelligence debug messages to appear..."
  echo "   (Polling every 2 seconds, max 60 seconds)"
  echo ""
fi

INTELLIGENCE_FOUND=false
MAX_WAIT=60
ELAPSED=0
POLL_INTERVAL=2

while [ $ELAPSED -lt $MAX_WAIT ]; do
  # Check for intelligence debug messages
  INTELLIGENCE_LOGS=$(kubectl logs -n "$NAMESPACE" "$HDN_POD" --tail=500 2>/dev/null | grep -E "\[INTELLIGENCE\]|learningRedis|getPreventionHintsForTask|Retrieved learned prevention|Searched.*keys for prevention|$TASK_NAME" | tail -20)
  
  # Also check if task appears in logs (means execution started)
  TASK_STARTED=$(kubectl logs -n "$NAMESPACE" "$HDN_POD" --tail=500 2>/dev/null | grep -i "$TASK_NAME" | head -1)
  
  if [ -n "$INTELLIGENCE_LOGS" ]; then
    INTELLIGENCE_FOUND=true
    break
  fi
  
  if [ -n "$TASK_STARTED" ]; then
    echo "   Task started, waiting for intelligence messages... ($ELAPSED/$MAX_WAIT seconds)"
  else
    echo "   Waiting for task to start... ($ELAPSED/$MAX_WAIT seconds)"
  fi
  
  sleep $POLL_INTERVAL
  ELAPSED=$((ELAPSED + POLL_INTERVAL))
done

echo ""
echo "🔍 Checking for intelligence debug messages..."
echo ""

# Get final intelligence logs
INTELLIGENCE_LOGS=$(kubectl logs -n "$NAMESPACE" "$HDN_POD" --tail=500 2>/dev/null | grep -E "\[INTELLIGENCE\]|learningRedis|getPreventionHintsForTask|Retrieved learned prevention|Searched.*keys for prevention" | tail -20)

if [ -n "$INTELLIGENCE_LOGS" ]; then
  echo "✅ FOUND INTELLIGENCE DEBUG MESSAGES:"
  echo "====================================="
  echo "$INTELLIGENCE_LOGS" | sed 's/^/  /'
  echo ""
  
  # Check specific messages
  if echo "$INTELLIGENCE_LOGS" | grep -q "learningRedis is nil"; then
    echo "❌ PROBLEM: learningRedis is nil!"
    echo "   This means Redis connection wasn't initialized in IntelligentExecutor"
  fi
  
  if echo "$INTELLIGENCE_LOGS" | grep -q "getPreventionHintsForTask: Searching"; then
    echo "✅ Function is being called"
  fi
  
  if echo "$INTELLIGENCE_LOGS" | grep -q "Retrieved learned prevention hint"; then
    echo "✅ Prevention hints are being retrieved!"
  fi
  
  if echo "$INTELLIGENCE_LOGS" | grep -q "Searched.*keys for prevention hints"; then
    echo "✅ Function executed but found 0 hints"
  fi
  
  if echo "$INTELLIGENCE_LOGS" | grep -q "Added.*prevention hints"; then
    echo "✅ Prevention hints were added to prompt!"
  fi
else
  echo "⚠️  No intelligence debug messages found after waiting $ELAPSED seconds"
  echo ""
  echo "Checking if task executed at all..."
  TASK_LOGS=$(kubectl logs -n "$NAMESPACE" "$HDN_POD" --tail=500 2>/dev/null | grep -i "$TASK_NAME" | tail -10)
  if [ -n "$TASK_LOGS" ]; then
    echo "✅ Task found in logs:"
    echo "$TASK_LOGS" | sed 's/^/  /'
    echo ""
    echo "But no intelligence messages - checking recent code generation activity..."
    RECENT=$(kubectl logs -n "$NAMESPACE" "$HDN_POD" --tail=200 2>/dev/null | grep -iE "\[CODEGEN\]|\[INTELLIGENT\]|intelligent.*execut" | tail -10)
    if [ -n "$RECENT" ]; then
      echo "Found recent activity:"
      echo "$RECENT" | sed 's/^/  /'
    fi
  else
    echo "❌ Task not found in logs - request may not have been processed"
    echo ""
    echo "This could mean:"
    echo "  1. Request is queued (system very busy)"
    echo "  2. Request failed before reaching execution"
    echo "  3. Code hasn't been rebuilt/redeployed with debug logging"
  fi
fi

# Clean up port-forward
kill $PORT_FORWARD_PID 2>/dev/null

echo ""
echo "===================================="
echo "To see all intelligence logs:"
echo "  kubectl logs -n $NAMESPACE $HDN_POD | grep -i intelligence"
echo ""
echo "To watch logs in real-time:"
echo "  kubectl logs -n $NAMESPACE -f $HDN_POD | grep -i intelligence"
echo ""

