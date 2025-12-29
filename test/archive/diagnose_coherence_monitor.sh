#!/bin/bash

# Diagnostic script to check why coherence monitor isn't running

NAMESPACE="${K8S_NAMESPACE:-agi}"
AGENT_ID="${AGENT_ID:-agent_1}"

echo "🔍 Diagnosing Coherence Monitor"
echo "================================"
echo ""

# Get FSM pod
FSM_POD=$(kubectl get pods -n "$NAMESPACE" -l app=fsm-server-rpi58 -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
if [ -z "$FSM_POD" ]; then
  echo "❌ FSM pod not found"
  exit 1
fi

echo "📦 FSM Pod: $FSM_POD"
echo ""

# Check when pod was created/restarted
echo "⏰ Pod Status:"
kubectl get pod -n "$NAMESPACE" "$FSM_POD" -o jsonpath='{.status.startTime}' 2>/dev/null | xargs -I {} echo "   Started: {}"
kubectl get pod -n "$NAMESPACE" "$FSM_POD" -o jsonpath='{.status.containerStatuses[0].image}' 2>/dev/null | xargs -I {} echo "   Image: {}"
echo ""

# Check for coherence monitor startup message
echo "🔍 Checking for Coherence Monitor Startup:"
STARTUP_LOG=$(kubectl logs -n "$NAMESPACE" "$FSM_POD" 2>/dev/null | grep -i "Coherence monitoring loop started")
if [ -n "$STARTUP_LOG" ]; then
  echo "   ✅ Found startup message:"
  echo "$STARTUP_LOG" | sed 's/^/      /'
else
  echo "   ❌ No startup message found - monitor may not have started"
  echo "   This could mean:"
  echo "      - Pod is running old code (before coherence monitor was added)"
  echo "      - Pod needs to be restarted to pick up new image"
fi
echo ""

# Check for any coherence-related logs
echo "📋 All Coherence-Related Logs:"
COHERENCE_LOGS=$(kubectl logs -n "$NAMESPACE" "$FSM_POD" 2>/dev/null | grep -i "\[Coherence\]" | tail -20)
if [ -n "$COHERENCE_LOGS" ]; then
  echo "$COHERENCE_LOGS" | sed 's/^/   /'
else
  echo "   ℹ️  No coherence logs found"
fi
echo ""

# Check for errors
echo "⚠️  Recent Errors (last 50 lines):"
ERRORS=$(kubectl logs -n "$NAMESPACE" "$FSM_POD" --tail=50 2>/dev/null | grep -iE "error|panic|fatal" | tail -10)
if [ -n "$ERRORS" ]; then
  echo "$ERRORS" | sed 's/^/   /'
else
  echo "   ✅ No recent errors found"
fi
echo ""

# Check FSM startup logs
echo "🚀 FSM Startup Logs (first 30 lines):"
kubectl logs -n "$NAMESPACE" "$FSM_POD" 2>/dev/null | head -30 | sed 's/^/   /'
echo ""

# Check if NewFSMEngine is being called (should create coherence monitor)
echo "🔧 Checking if coherence monitor is initialized:"
INIT_LOG=$(kubectl logs -n "$NAMESPACE" "$FSM_POD" 2>/dev/null | grep -iE "NewFSMEngine|Starting FSM engine" | head -5)
if [ -n "$INIT_LOG" ]; then
  echo "   ✅ FSM engine initialization found:"
  echo "$INIT_LOG" | sed 's/^/      /'
else
  echo "   ⚠️  Could not find FSM engine initialization logs"
fi
echo ""

# Check Redis for any stored inconsistencies (in case monitor ran but logs aren't showing)
echo "💾 Checking Redis for stored data:"
REDIS_POD=$(kubectl get pods -n "$NAMESPACE" -l app=redis -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
if [ -n "$REDIS_POD" ]; then
  INC_KEY="coherence:inconsistencies:${AGENT_ID}"
  INC_COUNT=$(kubectl exec -n "$NAMESPACE" "$REDIS_POD" -- redis-cli LLEN "$INC_KEY" 2>/dev/null)
  echo "   Inconsistencies in Redis: ${INC_COUNT:-0}"
  
  TASK_KEY="coherence:reflection_tasks:${AGENT_ID}"
  TASK_COUNT=$(kubectl exec -n "$NAMESPACE" "$REDIS_POD" -- redis-cli LLEN "$TASK_KEY" 2>/dev/null)
  echo "   Reflection tasks in Redis: ${TASK_COUNT:-0}"
else
  echo "   ⚠️  Redis pod not found"
fi
echo ""

echo "💡 Recommendations:"
if [ -z "$STARTUP_LOG" ]; then
  echo "   1. Restart the FSM pod to pick up the new code:"
  echo "      kubectl rollout restart deployment/fsm-server-rpi58 -n $NAMESPACE"
  echo "   2. Wait for the new pod to start, then check logs again"
  echo "   3. The coherence monitor should start automatically when FSM starts"
else
  echo "   1. Monitor is running - wait for the next 5-minute interval"
  echo "   2. Check logs again in a few minutes:"
  echo "      kubectl logs -n $NAMESPACE $FSM_POD | grep -i coherence"
fi
echo ""

