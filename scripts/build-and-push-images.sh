#!/bin/bash

# Build and Push Docker Images Script
# This script builds and pushes all the required Docker images for the Kubernetes deployment

set -e

# Check for ARM64 architecture and provide helpful message
ARCH=$(uname -m)
if [ "$ARCH" = "aarch64" ]; then
    echo "✅ Running on ARM64 architecture - using Docker build system"
    echo "This script is designed to work on ARM64 systems"
else
    echo "ℹ️  Running on $ARCH architecture"
fi

# Configuration
DOCKER_USERNAME=${DOCKER_USERNAME:-"stevef1uk"}
DOCKER_PASSWORD=${DOCKER_PASSWORD:-""}

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Function to print colored output
print_status() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

print_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

print_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Check if Docker is running
if ! docker info >/dev/null 2>&1; then
    print_error "Docker is not running. Please start Docker and try again."
    exit 1
fi

# Check if required files exist
print_status "Checking for required files..."

required_files=(
    "secure/customer_public.pem"
    "secure/vendor_public.pem"
    "Dockerfile.principles.secure"
    "Dockerfile.hdn.secure"
    "Dockerfile.fsm.secure"
    "Dockerfile.monitor-ui.secure"
    "Dockerfile.goal-manager.secure"
    "Dockerfile.wiki-summarizer.secure",
    "Dockerfile.telegram-bot.secure"
)

for file in "${required_files[@]}"; do
    if [ ! -f "$file" ]; then
        print_error "Required file not found: $file"
        exit 1
    fi
done

print_success "All required files found"

# Login to Docker Hub
if [ -n "$DOCKER_PASSWORD" ]; then
    print_status "Logging in to Docker Hub..."
    echo "$DOCKER_PASSWORD" | docker login -u "$DOCKER_USERNAME" --password-stdin
    print_success "Logged in to Docker Hub"
else
    print_warning "DOCKER_PASSWORD not set. You may need to login manually: docker login"
fi

# Function to build and push an image
build_and_push() {
    local dockerfile=$1
    local image_name=$2
    local description=$3
    
    print_status "Building $description ($image_name) for ARM64..."
    
    # Use buildx for ARM64 cross-compilation if not on ARM64
    if [ "$ARCH" != "aarch64" ]; then
        # Ensure buildx is available and create builder if needed
        if ! docker buildx inspect arm64-builder >/dev/null 2>&1; then
            print_status "Creating ARM64 buildx builder..."
            docker buildx create --name arm64-builder --use --bootstrap >/dev/null 2>&1 || true
        fi
        docker buildx use arm64-builder >/dev/null 2>&1 || true
        
        if docker buildx build --platform linux/arm64 -f "$dockerfile" \
            --build-arg no-cache \
            --build-arg CUSTOMER_PUBLIC_KEY=secure/customer_public.pem \
            --build-arg VENDOR_PUBLIC_KEY=secure/vendor_public.pem \
            -t "$DOCKER_USERNAME/$image_name:secure" \
            --load .; then
            print_success "Built $image_name:secure (ARM64)"
        else
            print_error "Failed to build $image_name:secure"
            return 1
        fi
    else
        # Native ARM64 build
        if docker build -f "$dockerfile" \
            --build-arg no-cache \
            --build-arg CUSTOMER_PUBLIC_KEY=secure/customer_public.pem \
            --build-arg VENDOR_PUBLIC_KEY=secure/vendor_public.pem \
            -t "$DOCKER_USERNAME/$image_name:secure" .; then
            print_success "Built $image_name:secure"
        else
            print_error "Failed to build $image_name:secure"
            return 1
        fi
    fi
    
    print_status "Pushing $image_name:secure to Docker Hub..."
    if docker push "$DOCKER_USERNAME/$image_name:secure"; then
        print_success "Pushed $image_name:secure to Docker Hub"
    else
        print_error "Failed to push $image_name:secure to Docker Hub"
        return 1
    fi
}

# Build and push all images
print_status "Starting Docker image build and push process..."

# Build images in parallel where possible, but some depend on others
build_and_push "Dockerfile.principles.secure" "principles-server" "Principles Server"
build_and_push "Dockerfile.hdn.secure" "hdn-server" "HDN Server"
build_and_push "Dockerfile.fsm.secure" "fsm-server" "FSM Server"
build_and_push "Dockerfile.monitor-ui.secure" "monitor-ui" "Monitor UI"
build_and_push "Dockerfile.goal-manager.secure" "goal-manager" "Goal Manager"
build_and_push "Dockerfile.wiki-bootstrapper.secure" "knowledge-builder" "Knowledge Builder"
build_and_push "Dockerfile.wiki-summarizer.secure" "wiki-summarizer" "Wiki Summarizer"
build_and_push "Dockerfile.news-ingestor.secure" "data-processor" "Data Processor"
build_and_push "Dockerfile.telegram-bot.secure" "telegram-agi-bot" "Telegram AGI Bot"


print_success "All Docker images have been built and pushed successfully!"

# Show summary
print_status "Summary of pushed images:"
echo "  - $DOCKER_USERNAME/principles-server:secure"
echo "  - $DOCKER_USERNAME/hdn-server:secure"
echo "  - $DOCKER_USERNAME/fsm-server:secure"
echo "  - $DOCKER_USERNAME/monitor-ui:secure"
echo "  - $DOCKER_USERNAME/goal-manager:secure"
echo "  - $DOCKER_USERNAME/knowledge-builder:secure"
echo "  - $DOCKER_USERNAME/wiki-summarizer:secure"
echo "  - $DOCKER_USERNAME/data-processor:secure"
echo "  - $DOCKER_USERNAME/telegram-agi-bot:secure"

print_status "You can now deploy these images to your Kubernetes cluster."
