#!/bin/bash

# Rebuild and redeploy Flight Search MCP server
# Run this on your build machine (Mac) or on the RPi

set -e

IMAGE_NAME="${IMAGE_NAME:-stevef1uk/flight-mcp:secure}"
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
echo "Rebuild & Deploy Flight Search MCP"
echo "=========================================="
echo ""

# Check if we're in the right directory
if [ ! -f "tools/flights/Dockerfile" ]; then
    print_error "Must run from project root (where tools/flights/Dockerfile exists)"
    exit 1
fi

print_status "Building Flight MCP Docker image..."
print_status "Image: $IMAGE_NAME"

# Build the image
docker build -f tools/flights/Dockerfile -t $IMAGE_NAME . || {
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

print_status "Deploying to k3s..."
kubectl apply -f k3s/flight-mcp.yaml || {
    print_error "Failed to apply k3s manifest"
    exit 1
}

print_status "Restarting flight-mcp deployment..."
kubectl rollout restart deployment/flight-mcp -n agi || {
    print_error "Failed to restart deployment"
    exit 1
}

print_status "Waiting for rollout to complete..."
kubectl rollout status deployment/flight-mcp -n agi --timeout=5m || {
    print_error "Rollout failed or timed out"
    exit 1
}

print_success "Flight Search MCP redeployed!"
echo ""
