#!/bin/bash

# Script to rebuild and deploy FSM with causal reasoning code
# Run this on the RPI after checking out the feature/causal-reasoning-signals branch

set -e

echo "🔨 Rebuilding FSM Server with Causal Reasoning"
echo "================================================"
echo ""

# Check if we're on the right branch
CURRENT_BRANCH=$(git rev-parse --abbrev-ref HEAD)
if [ "$CURRENT_BRANCH" != "feature/causal-reasoning-signals" ]; then
    echo "⚠️  Warning: Not on feature/causal-reasoning-signals branch"
    echo "   Current branch: $CURRENT_BRANCH"
    echo ""
    read -p "Continue anyway? (y/n) " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        echo "Please checkout the branch first:"
        echo "  git checkout feature/causal-reasoning-signals"
        exit 1
    fi
fi

# Check if secure files exist
if [ ! -f "secure/customer_public.pem" ] || [ ! -f "secure/vendor_public.pem" ]; then
    echo "❌ Error: Secure keys not found"
    echo "   Please ensure secure/customer_public.pem and secure/vendor_public.pem exist"
    exit 1
fi

echo "📦 Building FSM Docker image..."
docker build \
    -f Dockerfile.fsm.secure \
    --build-arg CUSTOMER_PUBLIC_KEY=secure/customer_public.pem \
    --build-arg VENDOR_PUBLIC_KEY=secure/vendor_public.pem \
    -t stevef1uk/fsm-server:secure \
    -t stevef1uk/fsm-server:secure-$(date +%Y%m%d-%H%M%S) \
    .

if [ $? -ne 0 ]; then
    echo "❌ Docker build failed"
    exit 1
fi

echo ""
echo "📤 Pushing FSM image to registry..."
docker push stevef1uk/fsm-server:secure

if [ $? -ne 0 ]; then
    echo "❌ Docker push failed"
    exit 1
fi

echo ""
echo "🔄 Restarting FSM deployment..."
kubectl rollout restart deployment/fsm-server-rpi58 -n agi

echo ""
echo "⏳ Waiting for deployment to be ready..."
kubectl rollout status deployment/fsm-server-rpi58 -n agi --timeout=120s

if [ $? -eq 0 ]; then
    echo ""
    echo "✅ FSM deployment restarted successfully!"
    echo ""
    echo "📊 New pod info:"
    kubectl get pods -n agi -l app=fsm-server-rpi58
    echo ""
    echo "🧪 To test causal reasoning:"
    echo "  1. Wait a few minutes for hypothesis generation"
    echo "  2. Run: ./test/test_causal_reasoning.sh"
    echo "  3. Or check logs: kubectl logs -n agi -f deployment/fsm-server-rpi58 | grep CAUSAL"
else
    echo "❌ Deployment failed to become ready"
    echo "   Check logs: kubectl logs -n agi deployment/fsm-server-rpi58"
    exit 1
fi

