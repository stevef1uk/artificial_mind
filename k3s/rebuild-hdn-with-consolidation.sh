#!/bin/bash

# Rebuild and redeploy HDN server with memory consolidation API endpoint

set -e

NAMESPACE="${NAMESPACE:-agi}"
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
echo "Rebuild HDN with Consolidation API"
echo "=========================================="
echo ""

# Check if we're in the right directory
if [ ! -f "hdn/server.go" ]; then
    print_error "Must run from project root (where hdn/ directory exists)"
    exit 1
fi

print_status "Building HDN server with Neo4j support..."
cd hdn
go build -tags neo4j -o ../bin/hdn-server . || {
    print_error "Build failed"
    exit 1
}
cd ..
print_success "Build complete"

print_status "Creating Docker image..."
# You'll need to adjust this based on your Docker build setup
# For k3s on RPi, you might be using a local registry or loading directly
print_status "Note: Adjust Docker build/push commands for your setup"

# Example for local k3s (load image directly):
# docker build -f Dockerfile.hdn.secure -t hdn-server:latest .
# docker save hdn-server:latest | ssh rpi "docker load"

# Or if using a registry:
# docker build -f Dockerfile.hdn.secure -t your-registry/hdn-server:latest .
# docker push your-registry/hdn-server:latest

print_status "Redeploying HDN server..."
kubectl rollout restart deployment/hdn-server-rpi58 -n $NAMESPACE || {
    print_error "Failed to restart deployment"
    exit 1
}

print_status "Waiting for rollout..."
kubectl rollout status deployment/hdn-server-rpi58 -n $NAMESPACE --timeout=5m || {
    print_error "Rollout failed or timed out"
    exit 1
}

print_success "HDN server redeployed!"
echo ""
print_status "You can now trigger consolidation with:"
echo "  ./k3s/trigger-consolidation.sh"
echo ""
print_status "Or test the endpoint:"
echo "  kubectl exec -n $NAMESPACE <hdn-pod> -- wget -q -O- --post-data='' --header='Content-Type: application/json' http://localhost:8080/api/v1/memory/consolidate"

