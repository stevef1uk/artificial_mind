#!/bin/bash

# Multi-Architecture Docker Build Script for AGI Project
# =====================================================

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Default values
REGISTRY=""
TAG="latest"
PLATFORMS="linux/amd64,linux/arm64"
PUSH=false
BUILD_ARGS=""

# Help function
show_help() {
    echo "Multi-Architecture Docker Build Script for AGI Project"
    echo "======================================================"
    echo ""
    echo "Usage: $0 [OPTIONS]"
    echo ""
    echo "Options:"
    echo "  -r, --registry REGISTRY    Docker registry (required)"
    echo "  -t, --tag TAG             Image tag (default: latest)"
    echo "  -p, --platforms PLATFORMS Comma-separated platforms (default: linux/amd64,linux/arm64)"
    echo "  --push                    Push images to registry"
    echo "  --build-arg KEY=VALUE     Pass build argument"
    echo "  -h, --help               Show this help"
    echo ""
    echo "Examples:"
    echo "  $0 -r myregistry.com -t v1.0.0 --push"
    echo "  $0 -r myregistry.com -p linux/amd64 -t x86-only"
    echo "  $0 -r myregistry.com --build-arg CUSTOMER_PUBLIC_KEY=secure/customer_public.pem"
    echo ""
    echo "Platforms:"
    echo "  linux/amd64    - x86_64 (Intel/AMD)"
    echo "  linux/arm64    - ARM64 (Raspberry Pi, Apple Silicon)"
    echo "  linux/arm/v7   - ARMv7 (older Raspberry Pi)"
    echo "  windows/amd64  - Windows x86_64"
}

# Parse command line arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        -r|--registry)
            REGISTRY="$2"
            shift 2
            ;;
        -t|--tag)
            TAG="$2"
            shift 2
            ;;
        -p|--platforms)
            PLATFORMS="$2"
            shift 2
            ;;
        --push)
            PUSH=true
            shift
            ;;
        --build-arg)
            BUILD_ARGS="$BUILD_ARGS --build-arg $2"
            shift 2
            ;;
        -h|--help)
            show_help
            exit 0
            ;;
        *)
            echo -e "${RED}Error: Unknown option $1${NC}"
            show_help
            exit 1
            ;;
    esac
done

# Validate required parameters
if [[ -z "$REGISTRY" ]]; then
    echo -e "${RED}Error: Registry is required. Use -r or --registry${NC}"
    show_help
    exit 1
fi

# Check if Docker buildx is available
if ! docker buildx version >/dev/null 2>&1; then
    echo -e "${RED}Error: Docker buildx is not available. Please install Docker buildx.${NC}"
    echo "Install with: docker buildx install"
    exit 1
fi

# Create buildx builder if it doesn't exist
BUILDER_NAME="agi-multiarch-builder"
if ! docker buildx ls | grep -q "$BUILDER_NAME"; then
    echo -e "${BLUE}Creating buildx builder: $BUILDER_NAME${NC}"
    docker buildx create --name "$BUILDER_NAME" --use
else
    echo -e "${BLUE}Using existing buildx builder: $BUILDER_NAME${NC}"
    docker buildx use "$BUILDER_NAME"
fi

# Function to build a service
build_service() {
    local service=$1
    local dockerfile=$2
    local context=$3
    
    echo -e "${BLUE}Building $service for platforms: $PLATFORMS${NC}"
    
    local image_name="$REGISTRY/$service:$TAG"
    local build_cmd="docker buildx build --platform $PLATFORMS $BUILD_ARGS -f $dockerfile -t $image_name $context"
    
    if [[ "$PUSH" == "true" ]]; then
        build_cmd="$build_cmd --push"
    else
        build_cmd="$build_cmd --load"
    fi
    
    echo -e "${YELLOW}Executing: $build_cmd${NC}"
    
    if eval "$build_cmd"; then
        echo -e "${GREEN}✅ Successfully built $service${NC}"
    else
        echo -e "${RED}❌ Failed to build $service${NC}"
        return 1
    fi
}

# Main build process
echo -e "${GREEN}🚀 Starting multi-architecture build for AGI Project${NC}"
echo -e "${BLUE}Registry: $REGISTRY${NC}"
echo -e "${BLUE}Tag: $TAG${NC}"
echo -e "${BLUE}Platforms: $PLATFORMS${NC}"
echo -e "${BLUE}Push: $PUSH${NC}"
echo ""

# Check if secure files exist for secure builds
if [[ "$BUILD_ARGS" == *"CUSTOMER_PUBLIC_KEY"* ]] || [[ "$BUILD_ARGS" == *"VENDOR_PUBLIC_KEY"* ]]; then
    if [[ ! -f "secure/customer_public.pem" ]] || [[ ! -f "secure/vendor_public.pem" ]]; then
        echo -e "${YELLOW}⚠️  Warning: Secure files not found. Creating them...${NC}"
        if [[ -f "scripts/create-secure-files.sh" ]]; then
            ./scripts/create-secure-files.sh
        else
            echo -e "${RED}Error: Secure files required but not found. Please run: scripts/create-secure-files.sh${NC}"
            exit 1
        fi
    fi
fi

# Build all services
services=(
    "principles-server:Dockerfile.principles.secure:."
    "hdn-server:Dockerfile.hdn.secure:."
    "fsm-server:Dockerfile.fsm.secure:."
    "goal-manager:Dockerfile.goal-manager.secure:."
    "monitor-ui:Dockerfile.monitor-ui.secure:."
    "news-ingestor:Dockerfile.news-ingestor.secure:."
    "wiki-bootstrapper:Dockerfile.wiki-bootstrapper.secure:."
    "wiki-summarizer:Dockerfile.wiki-summarizer.secure:."
)

# Build each service
failed_services=()
for service_info in "${services[@]}"; do
    IFS=':' read -r service dockerfile context <<< "$service_info"
    
    if ! build_service "$service" "$dockerfile" "$context"; then
        failed_services+=("$service")
    fi
    echo ""
done

# Summary
echo -e "${GREEN}🎉 Multi-architecture build completed!${NC}"
echo ""

if [[ ${#failed_services[@]} -eq 0 ]]; then
    echo -e "${GREEN}✅ All services built successfully${NC}"
else
    echo -e "${RED}❌ Failed services: ${failed_services[*]}${NC}"
    exit 1
fi

# Show built images
if [[ "$PUSH" == "false" ]]; then
    echo -e "${BLUE}Built images (local):${NC}"
    for service_info in "${services[@]}"; do
        IFS=':' read -r service dockerfile context <<< "$service_info"
        echo "  - $REGISTRY/$service:$TAG"
    done
    echo ""
    echo -e "${YELLOW}Note: Images are built locally. Use --push to push to registry.${NC}"
else
    echo -e "${BLUE}Pushed images:${NC}"
    for service_info in "${services[@]}"; do
        IFS=':' read -r service dockerfile context <<< "$service_info"
        echo "  - $REGISTRY/$service:$TAG"
    done
fi

echo ""
echo -e "${GREEN}🚀 Multi-architecture build complete!${NC}"
