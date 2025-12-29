#!/bin/bash

# Watch coherence monitor logs in real-time from Kubernetes

NAMESPACE="${K8S_NAMESPACE:-agi}"

echo "👀 Watching Coherence Monitor Logs (Kubernetes)"
echo "================================================"
echo ""
echo "Press Ctrl+C to stop"
echo ""

# Get FSM pod
FSM_POD=$(kubectl get pods -n "$NAMESPACE" -l app=fsm-server-rpi58 -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
if [ -z "$FSM_POD" ]; then
  echo "❌ FSM pod not found"
  exit 1
fi

echo "📦 Watching pod: $FSM_POD"
echo "🔍 Filtering for coherence-related logs..."
echo ""

# Follow logs and filter for coherence
kubectl logs -n "$NAMESPACE" "$FSM_POD" -f 2>/dev/null | grep --line-buffered -iE "\[Coherence\]|coherence" || {
  echo ""
  echo "⚠️  No coherence logs found. The monitor may not be running."
  echo ""
  echo "Check if monitor started:"
  echo "  kubectl logs -n $NAMESPACE $FSM_POD | grep 'Coherence monitoring loop started'"
}

