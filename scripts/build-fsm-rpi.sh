#!/bin/bash

# Build FSM Docker container for Raspberry Pi (ARM64)
# This script builds the FSM server container natively on ARM64

set -e

# Configuration
DOCKER_USERNAME=${DOCKER_USERNAME:-"stevef1uk"}
IMAGE_NAME="${DOCKER_USERNAME}/fsm-server"
TAG=${TAG:-"secure"}
DOCKERFILE="Dockerfile.fsm.secure"

# Colors
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}🔨 Building FSM Server Docker Container for Raspberry Pi${NC}"
echo "=============================================================="
echo ""

# Check if we're on ARM64
ARCH=$(uname -m)
if [ "$ARCH" != "aarch64" ]; then
    echo -e "${YELLOW}⚠️  Warning: Not running on ARM64 (detected: $ARCH)${NC}"
    echo "   This script is designed for Raspberry Pi (ARM64)"
    echo "   The build will still work but may be slower"
    echo ""
fi

# Check if Docker is running
if ! docker info >/dev/null 2>&1; then
    echo -e "${RED}❌ Docker is not running. Please start Docker and try again.${NC}"
    exit 1
fi

# Check for required files
echo -e "${BLUE}📋 Checking required files...${NC}"

if [ ! -f "$DOCKERFILE" ]; then
    echo -e "${RED}❌ Dockerfile not found: $DOCKERFILE${NC}"
    exit 1
fi

if [ ! -f "secure/customer_public.pem" ]; then
    echo -e "${YELLOW}⚠️  Warning: secure/customer_public.pem not found${NC}"
    echo "   The secure build requires this file. Continuing anyway..."
fi

if [ ! -f "secure/vendor_public.pem" ]; then
    echo -e "${YELLOW}⚠️  Warning: secure/vendor_public.pem not found${NC}"
    echo "   The secure build requires this file. Continuing anyway..."
fi

echo -e "${GREEN}✅ File checks complete${NC}"
echo ""

# Check if we're in the right directory
if [ ! -f "fsm/go.mod" ]; then
    echo -e "${RED}❌ Error: fsm/go.mod not found${NC}"
    echo "   Please run this script from the project root directory"
    exit 1
fi

# Build the image
echo -e "${BLUE}🏗️  Building Docker image...${NC}"
echo "   Image: ${IMAGE_NAME}:${TAG}"
echo "   Dockerfile: ${DOCKERFILE}"
echo ""

# Build with or without secure keys
if [ -f "secure/customer_public.pem" ] && [ -f "secure/vendor_public.pem" ]; then
    echo -e "${BLUE}   Using secure build with encryption keys${NC}"
    docker build -f "$DOCKERFILE" \
        --build-arg CUSTOMER_PUBLIC_KEY=secure/customer_public.pem \
        --build-arg VENDOR_PUBLIC_KEY=secure/vendor_public.pem \
        -t "${IMAGE_NAME}:${TAG}" \
        -t "${IMAGE_NAME}:latest" \
        .
else
    echo -e "${YELLOW}   ⚠️  Building without secure keys (may fail if Dockerfile requires them)${NC}"
    docker build -f "$DOCKERFILE" \
        -t "${IMAGE_NAME}:${TAG}" \
        -t "${IMAGE_NAME}:latest" \
        .
fi

if [ $? -eq 0 ]; then
    echo ""
    echo -e "${GREEN}✅ Build successful!${NC}"
    echo ""
    echo "Image built: ${IMAGE_NAME}:${TAG}"
    echo ""
    echo "Next steps:"
    echo "  1. Test the image:"
    echo "     docker run --rm ${IMAGE_NAME}:${TAG} --help"
    echo ""
    echo "  2. Tag for your registry (if needed):"
    echo "     docker tag ${IMAGE_NAME}:${TAG} your-registry/fsm-server:${TAG}"
    echo ""
    echo "  3. Push to registry (if needed):"
    echo "     docker push ${IMAGE_NAME}:${TAG}"
    echo ""
    echo "  4. Use in Kubernetes/deployment:"
    echo "     Update your deployment YAML to use: ${IMAGE_NAME}:${TAG}"
else
    echo ""
    echo -e "${RED}❌ Build failed${NC}"
    exit 1
fi

