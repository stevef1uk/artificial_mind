#!/bin/bash

# Test script to run BBC news ingestor locally
# This will scrape BBC news and publish events to NATS

set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

print_status() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

print_success() {
    echo -e "${GREEN}[OK]${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

print_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

echo "=========================================="
echo "BBC News Ingestor - Local Test"
echo "=========================================="
echo ""

# Check if binary exists
if [ ! -f "bin/bbc-news-ingestor" ]; then
    print_error "Binary not found: bin/bbc-news-ingestor"
    print_status "Building it now..."
    make build-bbc-news-ingestor || {
        print_error "Failed to build. Make sure you're in the project root."
        exit 1
    }
fi

# Check prerequisites
print_status "Checking prerequisites..."

# Check NATS
NATS_URL="${NATS_URL:-nats://127.0.0.1:4222}"
if nc -z $(echo $NATS_URL | sed 's|nats://||' | cut -d: -f1) $(echo $NATS_URL | sed 's|nats://||' | cut -d: -f2) 2>/dev/null; then
    print_success "NATS is accessible at $NATS_URL"
else
    print_warning "NATS may not be running at $NATS_URL"
    print_status "The ingestor will try to connect, but may fail if NATS is down"
fi

# Check Redis (optional)
REDIS_URL="${REDIS_URL:-redis://127.0.0.1:6379}"
REDIS_HOST=$(echo $REDIS_URL | sed 's|redis://||' | cut -d: -f1)
REDIS_PORT=$(echo $REDIS_URL | sed 's|redis://||' | cut -d: -f2)
if nc -z $REDIS_HOST $REDIS_PORT 2>/dev/null; then
    print_success "Redis is accessible at $REDIS_URL"
else
    print_warning "Redis not accessible at $REDIS_URL (duplicate detection will be disabled)"
fi

echo ""
print_status "Configuration:"
echo "  NATS_URL: $NATS_URL"
echo "  REDIS_URL: ${REDIS_URL:-not set}"
echo "  LLM_MODEL: ${LLM_MODEL:-llama3.1 (default)}"
echo "  OLLAMA_URL: ${OLLAMA_URL:-http://localhost:11434/api/chat (default)}"
echo ""

# Parse arguments
DRY_RUN=false
USE_LLM=false
MAX_STORIES=15
DEBUG=false
BATCH_SIZE=10

while [[ $# -gt 0 ]]; do
    case $1 in
        --dry)
            DRY_RUN=true
            shift
            ;;
        --llm)
            USE_LLM=true
            shift
            ;;
        --max)
            MAX_STORIES="$2"
            shift 2
            ;;
        --debug)
            DEBUG=true
            shift
            ;;
        --batch-size)
            BATCH_SIZE="$2"
            shift 2
            ;;
        --help)
            echo "Usage: $0 [OPTIONS]"
            echo ""
            echo "Options:"
            echo "  --dry          Dry run (don't publish to NATS)"
            echo "  --llm          Use LLM for classification (requires Ollama)"
            echo "  --max N        Maximum stories to process (default: 15)"
            echo "  --debug        Verbose debug output"
            echo "  --batch-size N LLM batch size (default: 10)"
            echo ""
            echo "Environment variables:"
            echo "  NATS_URL       NATS server URL (default: nats://127.0.0.1:4222)"
            echo "  REDIS_URL      Redis URL for duplicate detection (optional)"
            echo "  LLM_MODEL      LLM model name (default: llama3.1)"
            echo "  OLLAMA_URL     Ollama API URL (default: http://localhost:11434/api/chat)"
            echo ""
            exit 0
            ;;
        *)
            print_error "Unknown option: $1"
            echo "Use --help for usage"
            exit 1
            ;;
    esac
done

# Build command
CMD="./bin/bbc-news-ingestor"
CMD="$CMD -max $MAX_STORIES"

if [ "$DRY_RUN" = true ]; then
    CMD="$CMD -dry"
    print_status "Running in DRY RUN mode (no events will be published)"
fi

if [ "$USE_LLM" = true ]; then
    CMD="$CMD -llm -batch-size $BATCH_SIZE"
    if [ -n "$LLM_MODEL" ]; then
        CMD="$CMD -llm-model $LLM_MODEL"
    fi
    if [ -n "$OLLAMA_URL" ]; then
        CMD="$CMD -ollama-url $OLLAMA_URL"
    fi
    print_status "Using LLM for classification"
else
    print_status "Using heuristic classification"
fi

if [ "$DEBUG" = true ]; then
    CMD="$CMD -debug"
    print_status "Debug output enabled"
fi

echo ""
print_status "Running command:"
echo "  $CMD"
echo ""
echo "=========================================="
echo ""

# Run the ingestor
export NATS_URL
export REDIS_URL
$CMD

EXIT_CODE=$?

echo ""
echo "=========================================="
if [ $EXIT_CODE -eq 0 ]; then
    print_success "Ingestor completed successfully!"
else
    print_error "Ingestor exited with code $EXIT_CODE"
fi
echo "=========================================="
echo ""

# If not dry run, give tips on what to check next
if [ "$DRY_RUN" = false ]; then
    print_status "Next steps to verify:"
    echo ""
    echo "1. Check if FSM server received events:"
    echo "   (if FSM server is running, check its logs for 'news' events)"
    echo ""
    echo "2. Check if events were stored in Weaviate:"
    echo "   ./test/check_weaviate_bbc.sh http://localhost:8080"
    echo ""
    echo "3. Monitor NATS events (if you have nats CLI):"
    echo "   nats sub 'agi.events.news.>'"
    echo ""
fi

exit $EXIT_CODE






