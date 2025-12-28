#!/bin/bash

# Rebuild and redeploy HDN server with memory consolidation API
# Run this on your build machine (Mac) or on the RPi

set -e

IMAGE_NAME="${IMAGE_NAME:-stevef1uk/hdn-server:secure}"
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

print_status() {
    echo -e "${BLUE}[REBUILD]${NC} $1"
}

print_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

print_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

echo "=========================================="
echo "Rebuild & Deploy HDN with Consolidation"
echo "=========================================="
echo ""

# Check if we're in the right directory
if [ ! -f "Dockerfile.hdn.secure" ]; then
    print_error "Must run from project root (where Dockerfile.hdn.secure exists)"
    exit 1
fi

print_status "Building HDN Docker image..."
print_status "Image: $IMAGE_NAME"

# Build the image
docker build -f Dockerfile.hdn.secure -t $IMAGE_NAME . || {
    print_error "Docker build failed"
    exit 1
}

print_success "Docker image built"

print_status "Pushing image to registry..."
docker push $IMAGE_NAME || {
    print_error "Docker push failed"
    print_status "If building on RPi, you might need to load image directly instead"
    exit 1
}

print_success "Image pushed to registry"

print_status "Restarting HDN deployment..."
kubectl rollout restart deployment/hdn-server-rpi58 -n agi || {
    print_error "Failed to restart deployment"
    exit 1
}

print_status "Waiting for rollout to complete..."
kubectl rollout status deployment/hdn-server-rpi58 -n agi --timeout=5m || {
    print_error "Rollout failed or timed out"
    exit 1
}

print_success "HDN server redeployed!"
echo ""
print_status "Testing consolidation endpoint..."
sleep 5  # Give pod a moment to be ready

HDN_POD=$(kubectl get pods -n agi -l app=hdn-server-rpi58 -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
if [ -n "$HDN_POD" ]; then
    RESPONSE=$(kubectl exec -n agi $HDN_POD -- wget -q -O- \
        --post-data='' \
        --header='Content-Type: application/json' \
        http://localhost:8080/api/v1/memory/consolidate 2>&1 || echo "")
    
    if echo "$RESPONSE" | grep -q "success"; then
        print_success "Consolidation endpoint is working!"
        echo "$RESPONSE"
    else
        print_error "Endpoint test failed"
        echo "Response: $RESPONSE"
    fi
else
    print_error "Could not find HDN pod"
fi

echo ""
print_success "Done! You can now trigger consolidation with:"
echo "  ./k3s/trigger-consolidation.sh"

