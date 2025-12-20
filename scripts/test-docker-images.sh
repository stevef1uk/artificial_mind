#!/bin/bash

# Test Docker Images Script
# This script demonstrates how to test the Docker images locally from the command line

set -e

# Configuration
DOCKER_USERNAME=${DOCKER_USERNAME:-"stevef1uk"}
SECURE_DIR="${SECURE_DIR:-secure}"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

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

# Check prerequisites
check_prerequisites() {
    print_status "Checking prerequisites..."
    
    if ! docker info >/dev/null 2>&1; then
        print_error "Docker is not running. Please start Docker and try again."
        exit 1
    fi
    
    if [ ! -f "$SECURE_DIR/customer_private.pem" ]; then
        print_error "Customer private key not found at $SECURE_DIR/customer_private.pem"
        print_error "This is required to decrypt and run the secure images."
        exit 1
    fi
    
    if [ -z "${SECURE_VENDOR_TOKEN:-}" ]; then
        print_warning "SECURE_VENDOR_TOKEN not set. Some images may require this."
        print_warning "Set it with: export SECURE_VENDOR_TOKEN='your-token'"
    fi
    
    print_success "Prerequisites check passed"
}

# Test news-ingestor (data-processor) image
test_news_ingestor() {
    print_status "Testing news-ingestor (data-processor) image..."
    
    docker run --rm \
        -v "$(pwd)/$SECURE_DIR/customer_private.pem:/keys/customer_private.pem:ro" \
        -e SECURE_CUSTOMER_PRIVATE_PATH="/keys/customer_private.pem" \
        -e SECURE_VENDOR_TOKEN="${SECURE_VENDOR_TOKEN:-}" \
        -e UNPACK_WORK_DIR="/tmp/unpack" \
        -e NATS_URL="${NATS_URL:-nats://localhost:4222}" \
        -e OLLAMA_URL="${OLLAMA_URL:-http://localhost:11434/api/chat}" \
        -e OLLAMA_MODEL="${OLLAMA_MODEL:-Qwen2.5-VL-7B-Instruct:latest}" \
        "$DOCKER_USERNAME/data-processor:secure" \
        -url "https://www.bbc.com/news" \
        -max 5 \
        -debug
    
    print_success "News ingestor test completed"
}

# Test wiki-bootstrapper (knowledge-builder) image
test_wiki_bootstrapper() {
    print_status "Testing wiki-bootstrapper (knowledge-builder) image..."
    
    docker run --rm \
        -v "$(pwd)/$SECURE_DIR/customer_private.pem:/keys/customer_private.pem:ro" \
        -e SECURE_CUSTOMER_PRIVATE_PATH="/keys/customer_private.pem" \
        -e SECURE_VENDOR_TOKEN="${SECURE_VENDOR_TOKEN:-}" \
        -e UNPACK_WORK_DIR="/tmp/unpack" \
        -e NEO4J_URI="${NEO4J_URI:-bolt://localhost:7687}" \
        -e NEO4J_USER="${NEO4J_USER:-neo4j}" \
        -e NEO4J_PASS="${NEO4J_PASS:-test1234}" \
        -e REDIS_ADDR="${REDIS_ADDR:-localhost:6379}" \
        -e WEAVIATE_URL="${WEAVIATE_URL:-http://localhost:8080}" \
        -e NATS_URL="${NATS_URL:-nats://localhost:4222}" \
        "$DOCKER_USERNAME/knowledge-builder:secure" \
        -weaviate \
        -max-nodes 10 \
        -max-depth 2 \
        -rpm 60 \
        -burst 10 \
        -jitter-ms 25 \
        -seeds "Science,Technology"
    
    print_success "Wiki bootstrapper test completed"
}

# Test wiki-summarizer image
test_wiki_summarizer() {
    print_status "Testing wiki-summarizer image..."
    
    docker run --rm \
        -v "$(pwd)/$SECURE_DIR/customer_private.pem:/keys/customer_private.pem:ro" \
        -e SECURE_CUSTOMER_PRIVATE_PATH="/keys/customer_private.pem" \
        -e SECURE_VENDOR_TOKEN="${SECURE_VENDOR_TOKEN:-}" \
        -e UNPACK_WORK_DIR="/tmp/unpack" \
        -e WEAVIATE_URL="${WEAVIATE_URL:-http://localhost:8080}" \
        -e REDIS_ADDR="${REDIS_ADDR:-localhost:6379}" \
        -e LLM_PROVIDER="${LLM_PROVIDER:-ollama}" \
        -e LLM_ENDPOINT="${LLM_ENDPOINT:-http://localhost:11434/api/generate}" \
        -e LLM_MODEL="${LLM_MODEL:-Qwen2.5-VL-7B-Instruct:latest}" \
        -e BATCH_SIZE="${BATCH_SIZE:-5}" \
        -e MAX_WORDS="${MAX_WORDS:-250}" \
        -e DOMAIN="${DOMAIN:-General}" \
        "$DOCKER_USERNAME/wiki-summarizer:secure" \
        -weaviate="${WEAVIATE_URL:-http://localhost:8080}" \
        -redis="${REDIS_ADDR:-localhost:6379}" \
        -llm-provider="${LLM_PROVIDER:-ollama}" \
        -llm-endpoint="${LLM_ENDPOINT:-http://localhost:11434/api/generate}" \
        -llm-model="${LLM_MODEL:-Qwen2.5-VL-7B-Instruct:latest}" \
        -batch-size="${BATCH_SIZE:-5}" \
        -max-words="${MAX_WORDS:-250}" \
        -domain="${DOMAIN:-General}" \
        -job-id="test_$(date +%s)"
    
    print_success "Wiki summarizer test completed"
}

# Show usage
show_usage() {
    echo "Usage: $0 [image-name]"
    echo ""
    echo "Test Docker images locally from the command line"
    echo ""
    echo "Available images:"
    echo "  news-ingestor    - Test news ingestor (data-processor) image"
    echo "  wiki-bootstrapper - Test wiki bootstrapper (knowledge-builder) image"
    echo "  wiki-summarizer  - Test wiki summarizer image"
    echo "  all             - Test all images"
    echo ""
    echo "Environment variables:"
    echo "  DOCKER_USERNAME       - Docker Hub username (default: stevef1uk)"
    echo "  SECURE_DIR            - Directory containing keys (default: secure)"
    echo "  SECURE_VENDOR_TOKEN   - Vendor token for license validation"
    echo "  NATS_URL              - NATS server URL"
    echo "  NEO4J_URI             - Neo4j connection URI"
    echo "  NEO4J_USER            - Neo4j username"
    echo "  NEO4J_PASS            - Neo4j password"
    echo "  REDIS_ADDR            - Redis address"
    echo "  WEAVIATE_URL          - Weaviate server URL"
    echo "  OLLAMA_URL            - Ollama API URL"
    echo "  OLLAMA_MODEL          - Ollama model name"
    echo ""
    echo "Examples:"
    echo "  $0 news-ingestor"
    echo "  $0 wiki-bootstrapper"
    echo "  $0 wiki-summarizer"
    echo "  $0 all"
    echo ""
    echo "With custom environment:"
    echo "  NATS_URL=nats://localhost:4222 $0 news-ingestor"
}

# Main
main() {
    if [ $# -eq 0 ]; then
        show_usage
        exit 0
    fi
    
    check_prerequisites
    
    case "$1" in
        news-ingestor|data-processor)
            test_news_ingestor
            ;;
        wiki-bootstrapper|knowledge-builder)
            test_wiki_bootstrapper
            ;;
        wiki-summarizer)
            test_wiki_summarizer
            ;;
        all)
            print_status "Testing all images..."
            test_news_ingestor
            echo ""
            test_wiki_bootstrapper
            echo ""
            test_wiki_summarizer
            ;;
        *)
            print_error "Unknown image: $1"
            show_usage
            exit 1
            ;;
    esac
}

main "$@"

