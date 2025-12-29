#!/bin/bash

# Disable background LLM tasks to prioritize user requests

set -e

NAMESPACE="agi"
REDIS_POD=$(kubectl get pods -n $NAMESPACE -l app=redis -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || echo "")

if [ -z "$REDIS_POD" ]; then
    echo "❌ Redis pod not found"
    exit 1
fi

echo "Disabling background LLM tasks..."
kubectl exec -n $NAMESPACE "$REDIS_POD" -- redis-cli SET "DISABLE_BACKGROUND_LLM" "1" >/dev/null 2>&1

echo "✅ Background LLM tasks disabled"
echo ""
echo "This will:"
echo "  - Stop processing low-priority LLM requests"
echo "  - Prioritize user chat/chain-of-thought requests"
echo "  - Allow the queue to clear"
echo ""
echo "To re-enable background LLM:"
echo "  kubectl exec -n $NAMESPACE $REDIS_POD -- redis-cli DEL DISABLE_BACKGROUND_LLM"
echo ""

