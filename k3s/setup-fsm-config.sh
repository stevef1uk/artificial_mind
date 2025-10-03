#!/bin/bash

# Setup FSM Configuration and Secrets Script
# This script creates the FSM ConfigMap and all necessary secrets for the AGI system

set -e

echo "🔧 Setting up FSM Configuration and Secrets..."

# Check if we're in the right directory
if [ ! -f "fsm/config/artificial_mind.yaml" ]; then
    echo "❌ Error: artificial_mind.yaml not found. Please run this script from the AGI project root."
    exit 1
fi

# Create namespace if it doesn't exist
echo "📦 Creating namespace 'agi' if it doesn't exist..."
kubectl create namespace agi --dry-run=client -o yaml | kubectl apply -f -

# Delete existing ConfigMap if it exists
echo "🗑️  Removing existing FSM ConfigMap..."
kubectl delete configmap fsm-config -n agi --ignore-not-found=true

# Create FSM ConfigMap
echo "📋 Creating FSM ConfigMap with corrected guard modules..."
kubectl create configmap fsm-config -n agi --from-file=artificial_mind.yaml=fsm/config/artificial_mind.yaml

# Create secure customer private key secret
echo "🔐 Creating secure-customer-private secret..."
kubectl create secret generic secure-customer-private -n agi \
    --from-file=customer_private.pem=secure/customer_private.pem \
    --dry-run=client -o yaml | kubectl apply -f -

# Create secure customer token secret
echo "🎫 Creating secure-customer token secret..."
if [ -f "secure/customer_token.txt" ]; then
    CUSTOMER_TOKEN=$(cat secure/customer_token.txt)
    kubectl create secret generic secure-customer -n agi \
        --from-literal=token="$CUSTOMER_TOKEN" \
        --dry-run=client -o yaml | kubectl apply -f -
else
    echo "⚠️  Warning: secure/customer_token.txt not found. Creating empty secret."
    kubectl create secret generic secure-customer -n agi \
        --from-literal=token="" \
        --dry-run=client -o yaml | kubectl apply -f -
fi

# Create secure vendor token secret
echo "🎫 Creating secure-vendor token secret..."
if [ -f "secure/vendor_token.txt" ]; then
    VENDOR_TOKEN=$(cat secure/vendor_token.txt)
    kubectl create secret generic secure-vendor -n agi \
        --from-literal=token="$VENDOR_TOKEN" \
        --dry-run=client -o yaml | kubectl apply -f -
else
    echo "⚠️  Warning: secure/vendor_token.txt not found. Creating empty secret."
    kubectl create secret generic secure-vendor -n agi \
        --from-literal=token="" \
        --dry-run=client -o yaml | kubectl apply -f -
fi

# Verify the configuration
echo "✅ Verifying configuration..."
echo "📋 ConfigMaps:"
kubectl get configmaps -n agi | grep fsm

echo "🔐 Secrets:"
kubectl get secrets -n agi | grep secure

echo "📄 FSM ConfigMap contents:"
kubectl get configmap fsm-config -n agi -o yaml | grep -A 5 -B 5 "no_pending_input"

echo ""
echo "🎉 FSM configuration and secrets setup complete!"
echo ""
echo "To restart the FSM deployment with the new config:"
echo "  kubectl rollout restart deployment fsm-server -n agi"
echo ""
echo "To check FSM logs:"
echo "  kubectl logs -n agi deployment/fsm-server -f"
echo ""
echo "To verify the FSM is using correct guard modules:"
echo "  kubectl logs -n agi deployment/fsm-server | grep -E '(guard|module)' | tail -10"
