#!/bin/bash

# Script to thoroughly clean all databases
# Usage: ./scripts/clean_databases.sh [--confirm] [--stop-services]

set -e

CONFIRM="${1:-}"
STOP_SERVICES="${2:-}"

if [ "$CONFIRM" != "--confirm" ]; then
    echo "⚠️  WARNING: This will delete ALL data from ALL databases!"
    echo "   This includes:"
    echo "   - Stopping all services"
    echo "   - Redis (all keys)"
    echo "   - Neo4j (all nodes and relationships)"
    echo "   - Weaviate (all collections and objects)"
    echo "   - Persistent data directories"
    echo ""
    echo "   To proceed, run: ./scripts/clean_databases.sh --confirm"
    exit 1
fi

echo "🧹 Starting thorough database cleanup..."
echo ""

# Always stop services first to prevent key recreation
echo "🛑 Stopping services first to prevent key recreation..."
if [ -f "./scripts/stop_servers.sh" ]; then
    ./scripts/stop_servers.sh 2>/dev/null || echo "  ⚠️  Some services may not have stopped (may not be running)"
else
    echo "  ⚠️  stop_servers.sh not found, trying to stop services manually..."
    # Try to stop services by PID files
    for pid_file in /tmp/hdn_server.pid /tmp/fsm_server.pid /tmp/principles_server.pid /tmp/goal_manager.pid /tmp/monitor_ui.pid; do
        if [ -f "$pid_file" ]; then
            pid=$(cat "$pid_file" 2>/dev/null)
            if ps -p "$pid" > /dev/null 2>&1; then
                kill "$pid" 2>/dev/null || true
                echo "  Stopped service (PID: $pid)"
            fi
            rm -f "$pid_file"
        fi
    done
fi
sleep 3
echo ""

# 1. Clear Redis - multiple methods to ensure it works
echo "🗑️  Clearing Redis cache..."
if docker ps --format '{{.Names}}' | grep -qE '^(agi-redis|redis-server)$'; then
    CNAME=$(docker ps --format '{{.Names}}' | grep -E '^(agi-redis|redis-server)$' | head -n1)
    echo "  Using container: $CNAME"
    
    # Try FLUSHALL
    docker exec "$CNAME" redis-cli FLUSHALL >/dev/null 2>&1 && echo "  ✅ FLUSHALL executed"
    
    # Also try FLUSHDB for each database
    docker exec "$CNAME" redis-cli FLUSHDB >/dev/null 2>&1 && echo "  ✅ FLUSHDB executed"
    
    # Verify it's empty
    KEY_COUNT=$(docker exec "$CNAME" redis-cli DBSIZE 2>/dev/null || echo "0")
    echo "  📊 Redis key count after FLUSHALL: $KEY_COUNT"
    
    if [ "$KEY_COUNT" != "0" ]; then
        echo "  ⚠️  Redis still has keys, deleting them individually..."
        # Get all keys and delete them in batches
        docker exec "$CNAME" redis-cli --scan | xargs -L 100 docker exec "$CNAME" redis-cli DEL >/dev/null 2>&1 || true
        
        # Also try deleting by pattern
        for pattern in "workflow:*" "code:*" "tool:*" "fsm:*" "goal:*" "reasoning:*" "exploration:*" "metrics:*" "file:*" "wiki:*" "autonomy:*"; do
            docker exec "$CNAME" redis-cli --scan --pattern "$pattern" | xargs -L 100 docker exec "$CNAME" redis-cli DEL >/dev/null 2>&1 || true
        done
        
        # Final FLUSHALL
        docker exec "$CNAME" redis-cli FLUSHALL >/dev/null 2>&1 || true
        
        KEY_COUNT=$(docker exec "$CNAME" redis-cli DBSIZE 2>/dev/null || echo "0")
        echo "  📊 Redis key count after aggressive cleanup: $KEY_COUNT"
        
        if [ "$KEY_COUNT" != "0" ]; then
            echo "  ⚠️  Warning: Redis still has $KEY_COUNT keys remaining"
            echo "  These may be system keys or keys being recreated by running services"
        fi
    fi
    
    echo "✅ Redis cache cleared"
elif command -v redis-cli >/dev/null 2>&1; then
    redis-cli -h 127.0.0.1 -p 6379 FLUSHALL >/dev/null 2>&1 || echo "⚠️  Redis not reachable"
    echo "✅ Redis cache cleared (local)"
else
    echo "⚠️  Redis not found; skipped"
fi
echo ""

# 2. Clear Neo4j - more thorough
echo "🗑️  Clearing Neo4j graph database..."
if docker ps --format '{{.Names}}' | grep -qE '^(agi-neo4j|neo4j)$'; then
    CNAME=$(docker ps --format '{{.Names}}' | grep -E '^(agi-neo4j|neo4j)$' | head -n1)
    echo "  Using container: $CNAME"
    
    # Try multiple methods to delete all nodes
    echo "  Attempting to delete all nodes and relationships..."
    
    # Method 1: Standard cypher-shell
    docker exec -i "$CNAME" sh -c "cypher-shell -a bolt://localhost:7687 -u neo4j -p test1234 'MATCH (n) DETACH DELETE n;'" >/dev/null 2>&1 && echo "  ✅ Method 1: Standard cypher-shell succeeded" || echo "  ⚠️  Method 1 failed"
    
    # Method 2: Full path cypher-shell
    docker exec -i "$CNAME" sh -c "/var/lib/neo4j/bin/cypher-shell -a bolt://localhost:7687 -u neo4j -p test1234 'MATCH (n) DETACH DELETE n;'" >/dev/null 2>&1 && echo "  ✅ Method 2: Full path cypher-shell succeeded" || echo "  ⚠️  Method 2 failed"
    
    # Method 3: Delete constraints and indexes first, then nodes
    docker exec -i "$CNAME" sh -c "cypher-shell -a bolt://localhost:7687 -u neo4j -p test1234 'CALL apoc.schema.assert({},{},true); MATCH (n) DETACH DELETE n;'" >/dev/null 2>&1 && echo "  ✅ Method 3: With APOC succeeded" || echo "  ⚠️  Method 3 failed (APOC may not be available)"
    
    # Verify it's empty
    NODE_COUNT=$(docker exec -i "$CNAME" sh -c "cypher-shell -a bolt://localhost:7687 -u neo4j -p test1234 'MATCH (n) RETURN count(n) as count;'" 2>/dev/null | grep -o '[0-9]*' | head -1 || echo "unknown")
    echo "  📊 Neo4j node count after cleanup: $NODE_COUNT"
    
    echo "✅ Neo4j graph cleared"
else
    echo "⚠️  Neo4j container not running; skipped"
fi
echo ""

# 3. Clear Weaviate - more thorough
echo "🗑️  Clearing Weaviate vector database..."
WEAVIATE_URL="${WEAVIATE_URL:-http://localhost:8080}"
if curl -s -f "$WEAVIATE_URL/v1/meta" >/dev/null 2>&1; then
    echo "  Weaviate is reachable at $WEAVIATE_URL"
    
    # Get all schema classes
    SCHEMA_RESPONSE=$(curl -s "$WEAVIATE_URL/v1/schema" 2>/dev/null || echo "{}")
    CLASSES=$(echo "$SCHEMA_RESPONSE" | grep -o '"class":"[^"]*"' | cut -d'"' -f4 | sort -u || echo "")
    
    if [ -n "$CLASSES" ]; then
        DELETED_COUNT=0
        for CLASS in $CLASSES; do
            echo "  Deleting schema class: $CLASS"
            
            # Method 1: Delete schema class (removes all objects)
            DELETE_RESPONSE=$(curl -s -w "\n%{http_code}" -X DELETE "$WEAVIATE_URL/v1/schema/${CLASS}" 2>/dev/null || echo "")
            HTTP_CODE=$(echo "$DELETE_RESPONSE" | tail -1)
            
            if [ "$HTTP_CODE" = "204" ] || [ "$HTTP_CODE" = "200" ]; then
                echo "    ✅ Successfully deleted class: $CLASS"
                DELETED_COUNT=$((DELETED_COUNT + 1))
            else
                echo "    ⚠️  Schema deletion failed (HTTP $HTTP_CODE), trying batch delete..."
                
                # Method 2: Batch delete all objects in the class
                BATCH_DELETE=$(curl -s -w "\n%{http_code}" -X POST "$WEAVIATE_URL/v1/batch/objects" \
                    -H "Content-Type: application/json" \
                    -d "{\"match\":{\"class\":\"${CLASS}\"},\"output\":\"minimal\"}" 2>/dev/null || echo "")
                BATCH_HTTP=$(echo "$BATCH_DELETE" | tail -1)
                
                if [ "$BATCH_HTTP" = "200" ] || [ "$BATCH_HTTP" = "204" ]; then
                    echo "    ✅ Batch delete succeeded for class: $CLASS"
                    DELETED_COUNT=$((DELETED_COUNT + 1))
                else
                    echo "    ⚠️  Batch delete also failed (HTTP $BATCH_HTTP)"
                fi
            fi
        done
        echo "✅ Weaviate cleanup completed: $DELETED_COUNT classes processed"
    else
        echo "✅ Weaviate is empty or no collections found"
    fi
    
    # Verify it's empty
    FINAL_SCHEMA=$(curl -s "$WEAVIATE_URL/v1/schema" 2>/dev/null || echo "{}")
    FINAL_CLASSES=$(echo "$FINAL_SCHEMA" | grep -o '"class":"[^"]*"' | cut -d'"' -f4 | sort -u || echo "")
    if [ -n "$FINAL_CLASSES" ]; then
        echo "  ⚠️  Warning: Weaviate still has classes: $FINAL_CLASSES"
    else
        echo "  ✅ Weaviate is now empty"
    fi
else
    echo "⚠️  Weaviate not reachable at $WEAVIATE_URL; skipped"
fi
echo ""

# 4. Clear persistent data directories (optional but thorough)
echo "🗑️  Clearing persistent data directories..."
if [ -d "data/redis" ]; then
    echo "  Cleaning Redis data directory..."
    rm -rf data/redis/* 2>/dev/null || sudo rm -rf data/redis/* 2>/dev/null || echo "    ⚠️  Redis data directory cleanup may need manual intervention"
    echo "  ✅ Redis data directory cleaned"
fi

if [ -d "data/neo4j" ]; then
    echo "  Cleaning Neo4j data directory..."
    rm -rf data/neo4j/data/* data/neo4j/logs/* data/neo4j/import/* 2>/dev/null || sudo rm -rf data/neo4j/data/* data/neo4j/logs/* data/neo4j/import/* 2>/dev/null || echo "    ⚠️  Neo4j data directory cleanup may need manual intervention"
    echo "  ✅ Neo4j data directory cleaned"
fi

if [ -d "data/weaviate" ]; then
    echo "  Cleaning Weaviate data directory..."
    rm -rf data/weaviate/* 2>/dev/null || sudo rm -rf data/weaviate/* 2>/dev/null || echo "    ⚠️  Weaviate data directory cleanup may need manual intervention"
    echo "  ✅ Weaviate data directory cleaned"
fi
echo ""

# 5. Restart containers to ensure clean state
echo "🔄 Restarting containers to ensure clean state..."
docker-compose restart redis neo4j weaviate 2>/dev/null || echo "  ⚠️  Some containers may not have restarted"
echo "✅ Containers restarted"
echo ""

echo "✅ Thorough database cleanup finished!"
echo ""
echo "Verification:"
REDIS_KEYS=$(docker exec agi-redis redis-cli DBSIZE 2>/dev/null || echo "unknown")
echo "  - Redis keys: $REDIS_KEYS"
if [ "$REDIS_KEYS" != "0" ] && [ "$REDIS_KEYS" != "unknown" ]; then
    echo "    ⚠️  Warning: Redis still has $REDIS_KEYS keys"
    echo "    This may be because services are still running and recreating keys"
    echo "    Run with --stop-services flag to stop services first"
fi
echo "  - Neo4j nodes: Check via Monitor UI"
echo "  - Weaviate classes: Check via Monitor UI"
echo ""
echo "Services were stopped. To restart them:"
echo "  ./scripts/start_servers.sh"
echo ""
echo "Note: You may need to refresh the Monitor UI to see the changes."






