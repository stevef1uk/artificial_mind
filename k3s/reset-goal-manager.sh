#!/bin/bash

# Reset Goal Manager for agent_1 by clearing active goal sets and deleting individual goal keys
# Note: This will NOT delete history goals, only current active ones.

NAMESPACE="agi"
REDIS_POD=$(kubectl get pods -n $NAMESPACE | grep redis | awk '{print $1}')

if [ -z "$REDIS_POD" ]; then
    echo "❌ Could not find Redis pod."
    exit 1
fi

echo "🧹 Resetting Goal Manager for agent_1..."

# 1. Get all active goal IDs
echo "🔍 Fetching active goal IDs..."
ACTIVE_GOALS=$(kubectl exec -n $NAMESPACE $REDIS_POD -- redis-cli SMEMBERS goals:agent_1:active)

if [ -z "$ACTIVE_GOALS" ]; then
    echo "✅ No active goals found."
else
    COUNT=$(echo "$ACTIVE_GOALS" | wc -l)
    echo "🗑️ Found $COUNT active goals. Deleting individual goal keys..."
    
    # We can't delete them all at once due to shell arg limits, so we batch them or use scan-and-delete
    # Using scan-and-delete for safer execution
    echo "🗑️  Deleting 'goal:*' keys (this may take a moment)..."
    kubectl exec -n $NAMESPACE $REDIS_POD -- sh -c "redis-cli --scan --pattern 'goal:*' | xargs -r redis-cli DEL"
    
    echo "🗑️  Clearing active set and priority set..."
    kubectl exec -n $NAMESPACE $REDIS_POD -- redis-cli DEL goals:agent_1:active goals:agent_1:priorities
fi

echo "🚀 Reset complete! Restarting Goal Manager pod..."
kubectl delete pod -n $NAMESPACE -l app=goal-manager

echo "✅ Goal Manager reset successfully."
