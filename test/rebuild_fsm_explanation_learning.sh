#!/bin/bash

# Script to rebuild FSM server with Explanation-Grounded Learning Feedback
# Run this on your Raspberry Pi after switching to the branch

set -e

NAMESPACE="${K8S_NAMESPACE:-agi}"

echo "🧠 Rebuilding FSM Server with Explanation Learning"
echo "=================================================="
echo ""
echo "This will:"
echo "  1. Switch to branch: add-explanation-grounded-learning-feedback"
echo "  2. Pull latest changes"
echo "  3. Build FSM binary"
echo "  4. Build Docker image"
echo "  5. Restart FSM pod"
echo ""

# Check if we're in the project root
if [ ! -f "go.mod" ]; then
    echo "❌ Error: Must run from project root directory"
    exit 1
fi

# Check if we're on RPI
if [ ! -f "/proc/device-tree/model" ] || ! grep -q "Raspberry Pi" /proc/device-tree/model 2>/dev/null; then
    echo "⚠️  Warning: This script is designed for Raspberry Pi"
    echo "   Proceeding anyway..."
    echo ""
fi

echo "📦 Step 1: Switching to branch..."
echo "----------------------------------"
CURRENT_BRANCH=$(git branch --show-current)
echo "   Current branch: $CURRENT_BRANCH"

if [ "$CURRENT_BRANCH" != "add-explanation-grounded-learning-feedback" ]; then
    echo "   Switching to: add-explanation-grounded-learning-feedback"
    git fetch origin
    git checkout add-explanation-grounded-learning-feedback || {
        echo "⚠️  Branch not found locally, checking out from origin..."
        git checkout -b add-explanation-grounded-learning-feedback origin/add-explanation-grounded-learning-feedback || {
            echo "❌ Branch not found. Make sure you've pushed the branch or it exists on origin"
            exit 1
        }
    }
    echo "✅ Switched to branch: add-explanation-grounded-learning-feedback"
else
    echo "✅ Already on branch: add-explanation-grounded-learning-feedback"
    echo "   Pulling latest changes..."
    git pull origin add-explanation-grounded-learning-feedback || echo "⚠️  No remote branch or already up to date"
fi

echo ""
echo "📦 Step 2: Building FSM binary..."
echo "----------------------------------"
mkdir -p bin
cd fsm

# Build for ARM64 (Raspberry Pi)
echo "   Building for ARM64..."
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -tags neo4j -ldflags="-s -w" -o ../bin/fsm-server .

if [ $? -eq 0 ]; then
    echo "✅ Binary built successfully: bin/fsm-server"
else
    echo "❌ Failed to build binary"
    exit 1
fi
cd ..

echo ""
echo "📦 Step 3: Building Docker image..."
echo "-----------------------------------"

# Check if secure keys exist
if [ -f "secure/customer_public.pem" ] && [ -f "secure/vendor_public.pem" ]; then
    echo "   Building secure image..."
    docker build -f Dockerfile.fsm.secure \
        --build-arg CUSTOMER_PUBLIC_KEY=secure/customer_public.pem \
        --build-arg VENDOR_PUBLIC_KEY=secure/vendor_public.pem \
        -t fsm-server:latest \
        -t fsm-server:explanation-learning \
        -t stevef1uk/fsm-server:explanation-learning .
else
    echo "   ⚠️  Secure keys not found, building release image..."
    # Check if there's a release Dockerfile
    if [ -f "Dockerfile.fsm.release" ]; then
        docker build -f Dockerfile.fsm.release \
            -t fsm-server:latest \
            -t fsm-server:explanation-learning .
    else
        # Build using the fsm directory Dockerfile (if it exists)
        cd fsm
        if [ -f "Dockerfile" ]; then
            docker build -t fsm-server:latest \
                -t fsm-server:explanation-learning \
                -t fsm-server-rpi58:latest .
        else
            echo "❌ No Dockerfile found. Please check Dockerfile.fsm.secure or create one"
            exit 1
        fi
        cd ..
    fi
fi

if [ $? -eq 0 ]; then
    echo "✅ Docker image built successfully"
    echo "   Tagged as: fsm-server:explanation-learning"
else
    echo "❌ Failed to build Docker image"
    exit 1
fi

echo ""
echo "🔄 Step 4: Restarting FSM pod..."
echo "---------------------------------"

# Find FSM pod (try different label selectors)
FSM_POD=$(kubectl get pods -n "$NAMESPACE" -l app=fsm-server-rpi58 --sort-by=.metadata.creationTimestamp -o jsonpath='{.items[-1].metadata.name}' 2>/dev/null)
if [ -z "$FSM_POD" ]; then
    FSM_POD=$(kubectl get pods -n "$NAMESPACE" -l app=fsm-server --sort-by=.metadata.creationTimestamp -o jsonpath='{.items[-1].metadata.name}' 2>/dev/null)
fi

if [ -n "$FSM_POD" ]; then
    echo "   Found FSM pod: $FSM_POD"
    echo "   Deleting pod to trigger restart..."
    kubectl delete pod -n "$NAMESPACE" "$FSM_POD"
    echo "   Waiting for pod to restart..."
    sleep 5
    kubectl wait --for=condition=ready pod -n "$NAMESPACE" -l app=fsm-server-rpi58 --timeout=120s 2>/dev/null || \
    kubectl wait --for=condition=ready pod -n "$NAMESPACE" -l app=fsm-server --timeout=120s 2>/dev/null || {
        echo "⚠️  Pod not ready yet, but continuing..."
    }
    echo "✅ FSM pod restarted"
else
    echo "⚠️  FSM pod not found. You may need to deploy it manually."
    echo "   Check deployment with: kubectl get pods -n $NAMESPACE | grep fsm"
fi

echo ""
echo "✅ Rebuild complete!"
echo ""
echo "📊 Next steps:"
echo "  1. Wait 30-60 seconds for pod to fully start"
echo "  2. Check FSM logs for explanation learning:"
echo "     kubectl logs -f -n $NAMESPACE <fsm-pod> | grep EXPLANATION-LEARNING"
echo ""
echo "🔍 To verify explanation learning is working:"
echo "  - Watch for: '🧠 [EXPLANATION-LEARNING] Evaluating goal completion'"
echo "  - Watch for: '✅ [EXPLANATION-LEARNING] Completed evaluation'"
echo "  - Watch for: '📉 [EXPLANATION-LEARNING] Reducing confidence calibration'"
echo "  - Watch for: '📈 [EXPLANATION-LEARNING] Increasing confidence calibration'"
echo ""
echo "📚 Documentation:"
echo "  See docs/EXPLANATION_GROUNDED_LEARNING.md for details"
echo ""

