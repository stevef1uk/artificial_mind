#!/bin/bash

# Clear BBC news duplicate tracking from Redis
# This allows the ingestor to process stories again

set -e

REDIS_URL="${REDIS_URL:-redis://127.0.0.1:6379}"

echo "Clearing BBC news duplicate tracking from Redis..."
echo "Redis: $REDIS_URL"
echo ""

# Build the Go tool if it doesn't exist
if [ ! -f "bin/clear-news-duplicates" ]; then
    echo "Building clear-news-duplicates tool..."
    cd cmd/clear-news-duplicates && go build -o ../../bin/clear-news-duplicates . && cd ../..
    if [ $? -ne 0 ]; then
        echo "Error: Failed to build clear-news-duplicates tool"
        exit 1
    fi
fi

# Run the tool
export REDIS_URL
./bin/clear-news-duplicates

echo ""
echo "You can now run the ingestor again:"
echo "  ./test/run_bbc_ingestor_local.sh --max 100"

