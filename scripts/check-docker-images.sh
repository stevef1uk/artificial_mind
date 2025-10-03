#!/bin/bash

# Check Docker Images Script
# This script checks the status of Docker images in DockerHub

set -e

# Configuration
DOCKER_USERNAME=${DOCKER_USERNAME:-"stevef1uk"}

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

# List of required images
images=(
    "principles-server:secure"
    "hdn-server:secure"
    "fsm-server:secure"
    "monitor-ui:secure"
    "goal-manager:secure"
    "knowledge-builder:secure"
    "data-processor:secure"
)

print_status "Checking Docker images for $DOCKER_USERNAME..."

# Check if Docker is running
if ! docker info >/dev/null 2>&1; then
    print_error "Docker is not running. Please start Docker and try again."
    exit 1
fi

# Function to check if image exists locally
check_local_image() {
    local image_name=$1
    if docker image inspect "$DOCKER_USERNAME/$image_name" >/dev/null 2>&1; then
        local created=$(docker image inspect "$DOCKER_USERNAME/$image_name" --format='{{.Created}}')
        print_success "Local: $image_name (Created: $created)"
        return 0
    else
        print_warning "Local: $image_name (Not found locally)"
        return 1
    fi
}

# Function to check if image exists on DockerHub
check_remote_image() {
    local image_name=$1
    if docker manifest inspect "$DOCKER_USERNAME/$image_name" >/dev/null 2>&1; then
        print_success "Remote: $image_name (Available on DockerHub)"
        return 0
    else
        print_error "Remote: $image_name (Not found on DockerHub)"
        return 1
    fi
}

print_status "Checking local images..."
local_missing=0
for image in "${images[@]}"; do
    if ! check_local_image "$image"; then
        ((local_missing++))
    fi
done

echo ""
print_status "Checking remote images on DockerHub..."
remote_missing=0
for image in "${images[@]}"; do
    if ! check_remote_image "$image"; then
        ((remote_missing++))
    fi
done

echo ""
print_status "Summary:"
echo "  Local images missing: $local_missing"
echo "  Remote images missing: $remote_missing"

if [ $remote_missing -gt 0 ]; then
    echo ""
    print_warning "Some images are missing from DockerHub. You can build and push them using:"
    echo "  ./build-and-push-images.sh"
    echo ""
    print_status "Or set your Docker credentials and run:"
    echo "  export DOCKER_USERNAME=your_username"
    echo "  export DOCKER_PASSWORD=your_password"
    echo "  ./build-and-push-images.sh"
fi

if [ $local_missing -gt 0 ]; then
    echo ""
    print_status "To pull missing images from DockerHub:"
    for image in "${images[@]}"; do
        if ! docker image inspect "$DOCKER_USERNAME/$image" >/dev/null 2>&1; then
            echo "  docker pull $DOCKER_USERNAME/$image"
        fi
    done
fi
