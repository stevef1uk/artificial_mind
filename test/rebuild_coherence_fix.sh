#!/bin/bash

# Script to rebuild and restart containers after coherence monitor fix
# This fixes the missing Context field in PolicyGoal

set -e

NAMESPACE="${K8S_NAMESPACE:-agi}"

echo "🔧 Rebuilding Containers for Coherence Monitor Fix"
echo "=================================================="
echo ""
echo "This will:"
echo "  1. Rebuild Goal Manager (PolicyGoal struct updated)"
echo "  2. Rebuild FSM (coherence monitor logging improved)"
echo "  3. Restart both pods"
echo ""

# Check if we're on RPI
if [ ! -f "/proc/device-tree/model" ] || ! grep -q "Raspberry Pi" /proc/device-tree/model 2>/dev/null; then
    echo "⚠️  Warning: This script is designed for Raspberry Pi"
    echo "   Proceeding anyway..."
    echo ""
fi

# Get current directory
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$PROJECT_ROOT"

echo "📦 Step 1: Rebuilding Goal Manager container..."
echo "------------------------------------------------"
cd self
docker build -t goal-manager:latest -f Dockerfile .
if [ $? -eq 0 ]; then
    echo "✅ Goal Manager image built successfully"
else
    echo "❌ Failed to build Goal Manager"
    exit 1
fi
cd ..

echo ""
echo "📦 Step 2: Rebuilding FSM container..."
echo "--------------------------------------"
cd fsm
docker build -t fsm-server-rpi58:latest -f Dockerfile .
if [ $? -eq 0 ]; then
    echo "✅ FSM image built successfully"
else
    echo "❌ Failed to build FSM"
    exit 1
fi
cd ..

echo ""
echo "🔄 Step 3: Restarting Goal Manager pod..."
echo "-----------------------------------------"
GOAL_MGR_POD=$(kubectl get pods -n "$NAMESPACE" -l app=goal-manager -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
if [ -n "$GOAL_MGR_POD" ]; then
    kubectl delete pod -n "$NAMESPACE" "$GOAL_MGR_POD"
    echo "   Waiting for Goal Manager pod to restart..."
    sleep 5
    kubectl wait --for=condition=ready pod -n "$NAMESPACE" -l app=goal-manager --timeout=60s
    echo "✅ Goal Manager pod restarted"
else
    echo "⚠️  Goal Manager pod not found"
fi

echo ""
echo "🔄 Step 4: Restarting FSM pod..."
echo "---------------------------------"
FSM_POD=$(kubectl get pods -n "$NAMESPACE" -l app=fsm-server-rpi58 --sort-by=.metadata.creationTimestamp -o jsonpath='{.items[-1].metadata.name}' 2>/dev/null)
if [ -n "$FSM_POD" ]; then
    kubectl delete pod -n "$NAMESPACE" "$FSM_POD"
    echo "   Waiting for FSM pod to restart..."
    sleep 5
    kubectl wait --for=condition=ready pod -n "$NAMESPACE" -l app=fsm-server-rpi58 --timeout=60s
    echo "✅ FSM pod restarted"
else
    echo "⚠️  FSM pod not found"
fi

echo ""
echo "✅ Rebuild and restart complete!"
echo ""
echo "📊 Next steps:"
echo "  1. Wait 30-60 seconds for pods to fully start"
echo "  2. Run: ./test/check_coherence_kubernetes_status.sh"
echo "  3. Watch FSM logs for coherence events:"
echo "     kubectl logs -f -n $NAMESPACE <fsm-pod> | grep Coherence"
echo ""
echo "🔍 To verify the fix is working:"
echo "  - Check FSM logs for '🔔 [Coherence] Received goal.achieved event'"
echo "  - Check if context is present: '🔔 [Coherence] Goal X has context: ...'"
echo "  - Check if goals are matched: '✅ [Coherence] Matched coherence goal via context'"
echo ""

