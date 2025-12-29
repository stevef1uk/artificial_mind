#!/bin/bash

# Quick script to rebuild Goal Manager container on RPI

set -e

echo "🔧 Rebuilding Goal Manager Container"
echo "===================================="
echo ""

# Check if we're in the project root
if [ ! -f "go.mod" ]; then
    echo "❌ Error: Must run from project root directory"
    exit 1
fi

# Build the binary first
echo "📦 Step 1: Building Goal Manager binary..."
echo "------------------------------------------"
mkdir -p bin
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags="-s -w" -o ./bin/goal-manager ./cmd/goal-manager
if [ $? -eq 0 ]; then
    echo "✅ Binary built successfully"
else
    echo "❌ Failed to build binary"
    exit 1
fi

echo ""
echo "📦 Step 2: Building Docker image..."
echo "----------------------------------"

# Check if secure keys exist
if [ -f "secure/customer_public.pem" ] && [ -f "secure/vendor_public.pem" ]; then
    echo "   Building secure image..."
    docker build -f Dockerfile.goal-manager.secure \
        --build-arg CUSTOMER_PUBLIC_KEY=secure/customer_public.pem \
        --build-arg VENDOR_PUBLIC_KEY=secure/vendor_public.pem \
        -t goal-manager:latest \
        -t stevef1uk/goal-manager:secure-local .
else
    echo "   ⚠️  Secure keys not found, building release image..."
    docker build -f Dockerfile.goal-manager.release \
        -t goal-manager:latest \
        -t stevef1uk/goal-manager:secure-local .
fi

if [ $? -eq 0 ]; then
    echo "✅ Docker image built successfully"
    echo "   Tagged as: goal-manager:latest and stevef1uk/goal-manager:secure-local"
else
    echo "❌ Failed to build Docker image"
    exit 1
fi

echo ""
echo "🔄 Step 3: Updating Kubernetes deployment to use local image..."
echo "-------------------------------------------------------------"
NAMESPACE="${K8S_NAMESPACE:-agi}"

# Patch the deployment to use local image and Never pull policy
echo "   Patching deployment to use local image..."
kubectl set image deployment/goal-manager -n "$NAMESPACE" goal-manager=stevef1uk/goal-manager:secure-local
kubectl patch deployment goal-manager -n "$NAMESPACE" -p '{"spec":{"template":{"spec":{"containers":[{"name":"goal-manager","imagePullPolicy":"Never"}]}}}}'

if [ $? -eq 0 ]; then
    echo "✅ Deployment patched successfully"
    echo "   Waiting for rollout..."
    kubectl rollout status deployment/goal-manager -n "$NAMESPACE" --timeout=60s
    echo "✅ Goal Manager pod restarted with local image"
else
    echo "⚠️  Failed to patch deployment, trying pod restart..."
    GOAL_MGR_POD=$(kubectl get pods -n "$NAMESPACE" -l app=goal-manager -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
    if [ -n "$GOAL_MGR_POD" ]; then
        kubectl delete pod -n "$NAMESPACE" "$GOAL_MGR_POD"
        sleep 5
        kubectl wait --for=condition=ready pod -n "$NAMESPACE" -l app=goal-manager --timeout=60s
    fi
fi

echo ""
echo "✅ Rebuild complete!"
echo ""
echo "📊 Next steps:"
echo "  1. Check Goal Manager logs:"
echo "     kubectl logs -f -n $NAMESPACE -l app=goal-manager"
echo ""
echo "  2. Watch for context debug messages:"
echo "     kubectl logs -f -n $NAMESPACE -l app=goal-manager | grep -E '📥|✅|🐛'"
echo ""

