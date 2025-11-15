#!/bin/bash

# Script to clean up data directories for AGI services
# This fixes permission issues and clears corrupted data

set -e

echo "🧹 Cleaning up data directories..."
echo ""

# Stop any running containers first
echo "🛑 Stopping containers..."
docker-compose down 2>/dev/null || true
docker ps -a --format '{{.Names}}' | grep -E '^(agi-|artificial_mind_)' | xargs -r docker stop 2>/dev/null || true
echo "✅ Containers stopped"
echo ""

# Clean up data directories
echo "🗑️  Removing data directory contents..."

# Redis
if [ -d "data/redis" ]; then
    echo "  Cleaning Redis data..."
    sudo rm -rf data/redis/* 2>/dev/null || rm -rf data/redis/* 2>/dev/null || echo "    ⚠️  Redis cleanup may need manual intervention"
    echo "  ✅ Redis data cleaned"
fi

# Neo4j
if [ -d "data/neo4j" ]; then
    echo "  Cleaning Neo4j data..."
    sudo rm -rf data/neo4j/* 2>/dev/null || rm -rf data/neo4j/* 2>/dev/null || echo "    ⚠️  Neo4j cleanup may need manual intervention"
    echo "  ✅ Neo4j data cleaned"
fi

# Weaviate
if [ -d "data/weaviate" ]; then
    echo "  Cleaning Weaviate data..."
    sudo rm -rf data/weaviate/* 2>/dev/null || rm -rf data/weaviate/* 2>/dev/null || echo "    ⚠️  Weaviate cleanup may need manual intervention"
    echo "  ✅ Weaviate data cleaned"
fi

# Fix ownership (if we have permissions)
echo ""
echo "🔧 Fixing ownership..."
sudo chown -R $USER:$USER data/ 2>/dev/null || echo "  ⚠️  Could not fix ownership (may need to run with sudo)"
echo ""

echo "✅ Data directory cleanup complete!"
echo ""
echo "Next steps:"
echo "  ./scripts/start_servers.sh"


