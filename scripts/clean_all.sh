#!/bin/bash

# Script to completely clean out the system: logs and databases
# Usage: ./scripts/clean_all.sh [--confirm]

set -e

CONFIRM="${1:-}"

if [ "$CONFIRM" != "--confirm" ]; then
    echo "⚠️  WARNING: This will delete ALL logs and database data!"
    echo "   This includes:"
    echo "   - All log files in /tmp/"
    echo "   - Redis cache (all keys)"
    echo "   - Neo4j graph database (all nodes and relationships)"
    echo "   - Weaviate vector database (all collections)"
    echo ""
    echo "   To proceed, run: ./scripts/clean_all.sh --confirm"
    exit 1
fi

echo "🧹 Starting complete system cleanup..."
echo ""

# 1. Clear log files
echo "📋 Clearing log files..."
rm -f /tmp/hdn_server.log
rm -f /tmp/fsm.log
rm -f /tmp/goal_manager.log
rm -f /tmp/monitor_ui.log
rm -f /tmp/principles_server.log
rm -f /tmp/tool_calls_*.log
rm -f /tmp/monitor_ui.log
rm -f nohup.out
echo "✅ Log files cleared"
echo ""

# 2. Clear Redis
echo "🗑️  Clearing Redis cache..."
if docker ps --format '{{.Names}}' | grep -qE '^(agi-redis|redis-server)$'; then
    CNAME=$(docker ps --format '{{.Names}}' | grep -E '^(agi-redis|redis-server)$' | head -n1)
    docker exec "$CNAME" redis-cli FLUSHALL >/dev/null 2>&1
    echo "✅ Redis cache cleared (container: $CNAME)"
elif command -v redis-cli >/dev/null 2>&1; then
    redis-cli -h 127.0.0.1 -p 6379 FLUSHALL >/dev/null 2>&1 || echo "⚠️  Redis not reachable"
    echo "✅ Redis cache cleared (local)"
else
    echo "⚠️  Redis not found; skipped"
fi
echo ""

# 3. Clear Neo4j
echo "🗑️  Clearing Neo4j graph database..."
if docker ps --format '{{.Names}}' | grep -qE '^(agi-neo4j|neo4j)$'; then
    CNAME=$(docker ps --format '{{.Names}}' | grep -E '^(agi-neo4j|neo4j)$' | head -n1)
    # Try to delete all nodes and relationships
    docker exec -i "$CNAME" sh -c "cypher-shell -a bolt://localhost:7687 -u neo4j -p test1234 'MATCH (n) DETACH DELETE n;'" >/dev/null 2>&1 \
        && echo "✅ Neo4j graph cleared (container: $CNAME)" \
        || docker exec -i "$CNAME" sh -c "/var/lib/neo4j/bin/cypher-shell -a bolt://localhost:7687 -u neo4j -p test1234 'MATCH (n) DETACH DELETE n;'" >/dev/null 2>&1 \
        && echo "✅ Neo4j graph cleared (container: $CNAME)" \
        || echo "⚠️  Failed to clear Neo4j (may need manual cleanup)"
else
    echo "⚠️  Neo4j container not running; skipped"
fi
echo ""

# 4. Clear Weaviate
echo "🗑️  Clearing Weaviate vector database..."
WEAVIATE_URL="${WEAVIATE_URL:-http://localhost:8080}"
if curl -s -f "$WEAVIATE_URL/v1/meta" >/dev/null 2>&1; then
    # Get all schema classes
    SCHEMA_RESPONSE=$(curl -s "$WEAVIATE_URL/v1/schema" 2>/dev/null || echo "{}")
    CLASSES=$(echo "$SCHEMA_RESPONSE" | grep -o '"class":"[^"]*"' | cut -d'"' -f4 | sort -u || echo "")
    if [ -n "$CLASSES" ]; then
        DELETED_COUNT=0
        for CLASS in $CLASSES; do
            echo "  Deleting schema class: $CLASS (this will delete all objects)"
            # Delete the entire schema class - this removes all objects in the class
            DELETE_RESPONSE=$(curl -s -w "\n%{http_code}" -X DELETE "$WEAVIATE_URL/v1/schema/${CLASS}" 2>/dev/null || echo "")
            HTTP_CODE=$(echo "$DELETE_RESPONSE" | tail -1)
            if [ "$HTTP_CODE" = "204" ] || [ "$HTTP_CODE" = "200" ]; then
                echo "    ✅ Successfully deleted class: $CLASS"
                DELETED_COUNT=$((DELETED_COUNT + 1))
            else
                echo "    ⚠️  Failed to delete class: $CLASS (HTTP $HTTP_CODE)"
                # Try alternative: use batch delete via objects endpoint
                echo "    Trying batch delete via objects endpoint..."
                curl -s -X POST "$WEAVIATE_URL/v1/batch/objects" \
                    -H "Content-Type: application/json" \
                    -d "{\"match\":{\"class\":\"${CLASS}\"},\"output\":\"minimal\"}" >/dev/null 2>&1 || true
            fi
        done
        echo "✅ Weaviate cleanup completed: $DELETED_COUNT classes deleted"
        echo "  Note: Classes will be automatically recreated when new data is added."
    else
        echo "✅ Weaviate is empty or no collections found"
    fi
else
    echo "⚠️  Weaviate not reachable at $WEAVIATE_URL; skipped"
fi
echo ""

# 5. Optional: Clear data directories (commented out by default - very destructive!)
# Uncomment if you want to delete persistent data directories too
# echo "🗑️  Clearing data directories..."
# rm -rf data/redis/*
# rm -rf data/neo4j/data/*
# rm -rf data/weaviate/*
# echo "✅ Data directories cleared"
# echo ""

echo "✅ Complete system cleanup finished!"
echo ""
echo "Next steps:"
echo "  - Restart services: ./scripts/start_servers.sh"
echo "  - Or restart docker-compose: docker-compose restart"

