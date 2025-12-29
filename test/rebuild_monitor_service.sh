#!/bin/bash

# Rebuild Monitor Service container with debug logging

set -e

echo "🔧 Rebuilding Monitor Service Container"
echo "======================================="
echo ""

# Check if we're in the project root
if [ ! -f "go.mod" ]; then
    echo "❌ Error: Must run from project root directory"
    exit 1
fi

echo "📦 Step 1: Building Monitor Service binary..."
echo "--------------------------------------------"
mkdir -p bin
cd monitor
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags="-s -w" -o ../bin/monitor-ui .
cd ..
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
    docker build -f Dockerfile.monitor-ui.secure \
        --build-arg CUSTOMER_PUBLIC_KEY=secure/customer_public.pem \
        --build-arg VENDOR_PUBLIC_KEY=secure/vendor_public.pem \
        -t monitor-ui:latest .
else
    echo "   ⚠️  Secure keys not found, using local Dockerfile..."
    docker build -f Dockerfile.monitor-ui.local -t monitor-ui:latest .
fi

if [ $? -eq 0 ]; then
    echo "✅ Docker image built successfully"
else
    echo "❌ Failed to build Docker image"
    exit 1
fi

echo ""
echo "🔄 Step 3: Restarting Monitor Service pod..."
echo "-------------------------------------------"
NAMESPACE="${K8S_NAMESPACE:-agi}"
MONITOR_POD=$(kubectl get pods -n "$NAMESPACE" -l app=monitor-ui -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)

if [ -n "$MONITOR_POD" ]; then
    kubectl delete pod -n "$NAMESPACE" "$MONITOR_POD"
    echo "   Waiting for pod to restart..."
    sleep 5
    kubectl wait --for=condition=ready pod -n "$NAMESPACE" -l app=monitor-ui --timeout=60s
    echo "✅ Monitor Service pod restarted"
else
    echo "⚠️  Monitor Service pod not found (may need to apply deployment)"
fi

echo ""
echo "✅ Rebuild complete!"
echo ""
echo "📊 Next steps:"
echo "  1. Check Monitor Service logs:"
echo "     kubectl logs -f -n $NAMESPACE -l app=monitor-ui"
echo ""
echo "  2. Watch for debug messages when coherence goals are converted:"
echo "     kubectl logs -f -n $NAMESPACE -l app=monitor-ui | grep -E '📤|✅|system_coherence'"
echo ""
echo "  3. Run diagnostic:"
echo "     ./test/check_monitor_sending_goals.sh"
echo ""

