#!/bin/bash
# Quick script to restart the monitor-ui deployment in k3s

set -e

NAMESPACE="agi"
DEPLOYMENT="monitor-ui"

echo "🔄 Restarting monitor-ui deployment..."
kubectl rollout restart deployment/$DEPLOYMENT -n $NAMESPACE

echo "⏳ Waiting for rollout to complete..."
kubectl rollout status deployment/$DEPLOYMENT -n $NAMESPACE --timeout=120s

echo "✅ Monitor UI restarted successfully!"
echo ""
echo "📊 Check logs with:"
echo "   kubectl logs -n $NAMESPACE -l app=$DEPLOYMENT --tail=50 -f"
echo ""
echo "🌐 Access UI at: http://localhost:30082"

