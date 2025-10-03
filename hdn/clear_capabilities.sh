#!/bin/bash

# Script to clear all cached capabilities and start from nothing
# This demonstrates the system's ability to build capabilities from scratch

echo "🧹 Clearing All Cached Capabilities"
echo "==================================="
echo

# Check if Redis is running
if ! docker exec redis redis-cli ping > /dev/null 2>&1; then
    echo "❌ Redis is not running. Please start it first."
    exit 1
fi

echo "✅ Redis is running"
echo

# Clear all cached code
echo "🗑️  Clearing all cached code..."
docker exec redis redis-cli FLUSHDB

# Clear all actions
echo "🗑️  Clearing all dynamic actions..."
docker exec redis redis-cli DEL $(docker exec redis redis-cli KEYS "action:*")

# Clear all indexes
echo "🗑️  Clearing all indexes..."
docker exec redis redis-cli DEL $(docker exec redis redis-cli KEYS "index:*")

# Clear all domains
echo "🗑️  Clearing all domains..."
docker exec redis redis-cli DEL $(docker exec redis redis-cli KEYS "domain:*")

echo
echo "✅ All capabilities cleared!"
echo
echo "The system now has ZERO capabilities and is ready to learn from nothing."
echo "Run ./demo_from_nothing.sh to see the system build capabilities from scratch."
