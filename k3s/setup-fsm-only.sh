#!/bin/bash

# Quick FSM ConfigMap Setup Script
# This script only creates/updates the FSM ConfigMap

set -e

echo "🔧 Setting up FSM ConfigMap..."

# Check if we're in the right directory
if [ ! -f "fsm/config/artificial_mind.yaml" ]; then
    echo "❌ Error: artificial_mind.yaml not found. Please run this script from the k3s directory."
    exit 1
fi

# Create namespace if it doesn't exist
kubectl create namespace agi --dry-run=client -o yaml | kubectl apply -f -

# Delete existing ConfigMap if it exists
echo "🗑️  Removing existing FSM ConfigMap..."
kubectl delete configmap fsm-config -n agi --ignore-not-found=true

# Create FSM ConfigMap with corrected guard modules
echo "📋 Creating FSM ConfigMap with corrected guard modules..."
kubectl create configmap fsm-config -n agi --from-file=artificial_mind.yaml=fsm/config/artificial_mind.yaml

# Verify the configuration
echo "✅ Verifying FSM ConfigMap..."
kubectl get configmap fsm-config -n agi

echo "📄 Checking guard module configuration:"
kubectl get configmap fsm-config -n agi -o yaml | grep -A 3 -B 1 "no_pending_input"

echo ""
echo "🎉 FSM ConfigMap setup complete!"
echo ""
echo "To restart the FSM deployment:"
echo "  kubectl rollout restart deployment fsm-server-rpi58 -n agi"
echo ""
echo "To check FSM logs:"
echo "  kubectl logs -n agi deployment/fsm-server-rpi58 -f"
