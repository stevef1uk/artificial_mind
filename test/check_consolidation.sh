#!/bin/bash

# Quick script to check if memory consolidation is working

set -e

REDIS_URL="${REDIS_URL:-redis://localhost:6379}"
WEAVIATE_URL="${WEAVIATE_URL:-http://localhost:8080}"
HDN_PORT="${HDN_PORT:-8081}"

echo "🔍 Checking Memory Consolidation Status"
echo "========================================"
echo ""

# 1. Check if HDN server is running
echo "1️⃣ Checking HDN server..."
if curl -s "http://localhost:$HDN_PORT/health" > /dev/null 2>&1; then
    echo "   ✅ HDN server is running"
else
    echo "   ❌ HDN server is not responding"
    echo "   💡 Start it with: ./restart.sh or make start-hdn"
    exit 1
fi

# 2. Check server logs for consolidation messages
echo ""
echo "2️⃣ Checking server logs for consolidation activity..."
if [ -f "/tmp/hdn-server.log" ]; then
    if grep -q "CONSOLIDATION" /tmp/hdn-server.log 2>/dev/null; then
        echo "   ✅ Found consolidation logs"
        echo ""
        echo "   Recent consolidation activity:"
        grep "CONSOLIDATION" /tmp/hdn-server.log | tail -5 | sed 's/^/   /'
    else
        echo "   ⚠️  No consolidation logs found yet"
        echo "   💡 Consolidation runs 30s after startup, then every hour"
    fi
else
    echo "   ℹ️  Log file not found at /tmp/hdn-server.log"
    echo "   💡 Check your server logs for: grep CONSOLIDATION"
fi

# 3. Check Redis for consolidated schemas
echo ""
echo "3️⃣ Checking Redis for consolidated schemas..."
if command -v redis-cli &> /dev/null; then
    SCHEMA_COUNT=$(redis-cli -u "$REDIS_URL" KEYS "consolidation:schema:*" 2>/dev/null | wc -l | tr -d ' ')
    if [ "$SCHEMA_COUNT" -gt 0 ]; then
        echo "   ✅ Found $SCHEMA_COUNT consolidated schema(s)"
        echo ""
        echo "   Schema keys:"
        redis-cli -u "$REDIS_URL" KEYS "consolidation:schema:*" 2>/dev/null | head -3 | while read key; do
            echo "   - $key"
            # Try to show a preview of the schema
            pattern=$(redis-cli -u "$REDIS_URL" GET "$key" 2>/dev/null | grep -o '"pattern":"[^"]*' | cut -d'"' -f4 | head -c 60)
            if [ -n "$pattern" ]; then
                echo "     Pattern: ${pattern}..."
            fi
        done
    else
        echo "   ⚠️  No schemas found yet"
        echo "   💡 Need ≥5 similar episodes for compression"
    fi
else
    echo "   ⚠️  redis-cli not found, skipping Redis check"
fi

# 4. Check Redis for skills
echo ""
echo "4️⃣ Checking Redis for extracted skills..."
if command -v redis-cli &> /dev/null; then
    SKILL_COUNT=$(redis-cli -u "$REDIS_URL" KEYS "consolidation:skill:*" 2>/dev/null | wc -l | tr -d ' ')
    if [ "$SKILL_COUNT" -gt 0 ]; then
        echo "   ✅ Found $SKILL_COUNT skill abstraction(s)"
        echo ""
        echo "   Skill keys:"
        redis-cli -u "$REDIS_URL" KEYS "consolidation:skill:*" 2>/dev/null | head -3 | while read key; do
            echo "   - $key"
            # Try to show skill name and success rate
            skill_data=$(redis-cli -u "$REDIS_URL" GET "$key" 2>/dev/null)
            name=$(echo "$skill_data" | grep -o '"name":"[^"]*' | cut -d'"' -f4 | head -1)
            success=$(echo "$skill_data" | grep -o '"success_rate":[0-9.]*' | cut -d':' -f2 | head -1)
            if [ -n "$name" ]; then
                echo "     Name: $name"
            fi
            if [ -n "$success" ]; then
                echo "     Success Rate: $(echo "$success * 100" | bc 2>/dev/null | cut -d'.' -f1)%"
            fi
        done
    else
        echo "   ⚠️  No skills found yet"
        echo "   💡 Need ≥3 workflow repetitions for skill extraction"
    fi
else
    echo "   ⚠️  redis-cli not found, skipping Redis check"
fi

# 5. Check Redis for archived traces
echo ""
echo "5️⃣ Checking Redis for archived traces..."
if command -v redis-cli &> /dev/null; then
    ARCHIVED_COUNT=$(redis-cli -u "$REDIS_URL" KEYS "archive:reasoning_trace:*" 2>/dev/null | wc -l | tr -d ' ')
    if [ "$ARCHIVED_COUNT" -gt 0 ]; then
        echo "   ✅ Found $ARCHIVED_COUNT archived trace(s)"
    else
        echo "   ℹ️  No archived traces (traces may not be old enough yet)"
        echo "   💡 Traces are archived after 7 days with low utility"
    fi
else
    echo "   ⚠️  redis-cli not found, skipping Redis check"
fi

# 6. Check Neo4j for promoted concepts
echo ""
echo "6️⃣ Checking Neo4j for promoted concepts..."
if command -v curl &> /dev/null; then
    NEO4J_USER="${NEO4J_USER:-neo4j}"
    NEO4J_PASS="${NEO4J_PASS:-test1234}"
    NEO4J_AUTH=$(echo -n "$NEO4J_USER:$NEO4J_PASS" | base64 2>/dev/null || echo "")
    
    if [ -n "$NEO4J_AUTH" ]; then
        CONCEPT_COUNT=$(curl -s -H "Authorization: Basic $NEO4J_AUTH" \
            -H "Content-Type: application/json" \
            -X POST "http://localhost:7474/db/data/cypher" \
            -d '{"query": "MATCH (c:Concept) WHERE c.domain = \"Skills\" RETURN count(c) as count"}' \
            2>/dev/null | grep -o '"count":[0-9]*' | grep -o '[0-9]*' || echo "0")
        
        if [ "$CONCEPT_COUNT" != "0" ] && [ -n "$CONCEPT_COUNT" ]; then
            echo "   ✅ Found concepts in Neo4j (may include test data)"
            echo "   💡 Check Neo4j Browser: http://localhost:7474"
        else
            echo "   ℹ️  No Skills concepts found yet"
            echo "   💡 Concepts are promoted when stability ≥0.7"
        fi
    else
        echo "   ⚠️  Could not encode Neo4j credentials"
    fi
else
    echo "   ⚠️  curl not found, skipping Neo4j check"
fi

# Summary
echo ""
echo "========================================"
echo "📊 Summary"
echo "========================================"
echo ""
echo "💡 Tips:"
echo "  - Consolidation runs 30s after server start, then every hour"
echo "  - View live logs: tail -f /tmp/hdn-server.log | grep CONSOLIDATION"
echo "  - Or check process logs: ps aux | grep hdn-server"
echo "  - Create test episodes to trigger consolidation faster"
echo ""
echo "🧪 To test consolidation:"
echo "  ./test/test_memory_consolidation.sh"
echo ""

