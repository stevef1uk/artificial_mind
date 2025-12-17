#!/bin/bash

# Script to clean old reasoning traces from Redis
# Usage: ./scripts/clean_reasoning_traces.sh

set -e

echo "🧹 Cleaning reasoning traces from Redis..."
echo ""

if docker ps --format '{{.Names}}' | grep -qE '^(agi-redis|redis-server)$'; then
    CNAME=$(docker ps --format '{{.Names}}' | grep -E '^(agi-redis|redis-server)$' | head -n1)
    echo "Using container: $CNAME"
    
    # Get all reasoning trace keys
    TRACE_KEYS=$(docker exec "$CNAME" redis-cli KEYS "reasoning:traces:*" 2>/dev/null || echo "")
    
    if [ -n "$TRACE_KEYS" ]; then
        echo "Found reasoning trace keys:"
        echo "$TRACE_KEYS" | head -10
        echo ""
        
        # Trim each key to keep only last 10 traces
        echo "$TRACE_KEYS" | while read -r key; do
            if [ -n "$key" ]; then
                docker exec "$CNAME" redis-cli LTRIM "$key" 0 9 >/dev/null 2>&1
                COUNT=$(docker exec "$CNAME" redis-cli LLEN "$key" 2>/dev/null || echo "0")
                echo "  Trimmed $key to $COUNT traces"
            fi
        done
        
        echo ""
        echo "✅ Reasoning traces cleaned"
    else
        echo "No reasoning trace keys found"
    fi
    
    # Also clean explanations
    EXPLANATION_KEYS=$(docker exec "$CNAME" redis-cli KEYS "reasoning:explanations:*" 2>/dev/null || echo "")
    if [ -n "$EXPLANATION_KEYS" ]; then
        echo ""
        echo "Cleaning reasoning explanations..."
        echo "$EXPLANATION_KEYS" | while read -r key; do
            if [ -n "$key" ]; then
                docker exec "$CNAME" redis-cli LTRIM "$key" 0 4 >/dev/null 2>&1
                COUNT=$(docker exec "$CNAME" redis-cli LLEN "$key" 2>/dev/null || echo "0")
                echo "  Trimmed $key to $COUNT explanations"
            fi
        done
    fi
    
    echo ""
    echo "✅ Cleanup complete"
else
    echo "⚠️  Redis container not found"
    exit 1
fi

