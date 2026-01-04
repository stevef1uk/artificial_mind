#!/bin/bash

# Test script to verify all MCP knowledge tools are properly implemented
# Usage: ./test_mcp_tools.sh [HDN_URL]

set -e

HDN_URL="${1:-http://localhost:8081}"
MCP_ENDPOINT="${HDN_URL}/mcp"

echo "🧪 Testing MCP Knowledge Tools"
echo "MCP Endpoint: $MCP_ENDPOINT"
echo ""

# Test 1: List tools
echo "📋 Test 1: Listing available tools..."
TOOLS_RESPONSE=$(curl -s -X POST "$MCP_ENDPOINT" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}')

TOOL_COUNT=$(echo "$TOOLS_RESPONSE" | jq -r '.result.tools | length' 2>/dev/null || echo "0")
if [ "$TOOL_COUNT" -gt "0" ]; then
  echo "✅ Found $TOOL_COUNT tools:"
  echo "$TOOLS_RESPONSE" | jq -r '.result.tools[] | "  - \(.name): \(.description)"' 2>/dev/null || true
else
  echo "❌ No tools found"
  exit 1
fi
echo ""

# Test 2: Test query_neo4j
echo "📊 Test 2: Testing query_neo4j..."
QUERY_NEO4J_RESPONSE=$(curl -s -X POST "$MCP_ENDPOINT" \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "id": 2,
    "method": "tools/call",
    "params": {
      "name": "query_neo4j",
      "arguments": {
        "query": "MATCH (c:Concept) RETURN c LIMIT 3"
      }
    }
  }')

if echo "$QUERY_NEO4J_RESPONSE" | jq -e '.result' >/dev/null 2>&1; then
  echo "✅ query_neo4j works"
  COUNT=$(echo "$QUERY_NEO4J_RESPONSE" | jq -r '.result.count // 0' 2>/dev/null || echo "0")
  echo "   Returned count: $COUNT"
else
  echo "❌ query_neo4j failed:"
  echo "$QUERY_NEO4J_RESPONSE" | jq '.' 2>/dev/null || echo "$QUERY_NEO4J_RESPONSE"
fi
echo ""

# Test 3: Test get_concept
echo "🔍 Test 3: Testing get_concept..."
GET_CONCEPT_RESPONSE=$(curl -s -X POST "$MCP_ENDPOINT" \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "id": 3,
    "method": "tools/call",
    "params": {
      "name": "get_concept",
      "arguments": {
        "name": "Science",
        "domain": "General"
      }
    }
  }')

if echo "$GET_CONCEPT_RESPONSE" | jq -e '.result' >/dev/null 2>&1; then
  echo "✅ get_concept works"
  COUNT=$(echo "$GET_CONCEPT_RESPONSE" | jq -r '.result.count // 0' 2>/dev/null || echo "0")
  echo "   Returned count: $COUNT"
else
  echo "❌ get_concept failed:"
  echo "$GET_CONCEPT_RESPONSE" | jq '.' 2>/dev/null || echo "$GET_CONCEPT_RESPONSE"
fi
echo ""

# Test 4: Test find_related_concepts
echo "🔗 Test 4: Testing find_related_concepts..."
FIND_RELATED_RESPONSE=$(curl -s -X POST "$MCP_ENDPOINT" \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "id": 4,
    "method": "tools/call",
    "params": {
      "name": "find_related_concepts",
      "arguments": {
        "concept_name": "Science",
        "max_depth": 1
      }
    }
  }')

if echo "$FIND_RELATED_RESPONSE" | jq -e '.result' >/dev/null 2>&1; then
  echo "✅ find_related_concepts works"
  COUNT=$(echo "$FIND_RELATED_RESPONSE" | jq -r '.result.count // 0' 2>/dev/null || echo "0")
  echo "   Returned count: $COUNT"
else
  echo "❌ find_related_concepts failed:"
  echo "$FIND_RELATED_RESPONSE" | jq '.' 2>/dev/null || echo "$FIND_RELATED_RESPONSE"
fi
echo ""

# Test 5: Test search_weaviate
echo "🔎 Test 5: Testing search_weaviate..."
SEARCH_WEAVIATE_RESPONSE=$(curl -s -X POST "$MCP_ENDPOINT" \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "id": 5,
    "method": "tools/call",
    "params": {
      "name": "search_weaviate",
      "arguments": {
        "query": "test",
        "collection": "WikipediaArticle",
        "limit": 3
      }
    }
  }')

if echo "$SEARCH_WEAVIATE_RESPONSE" | jq -e '.result' >/dev/null 2>&1; then
  echo "✅ search_weaviate works"
  COUNT=$(echo "$SEARCH_WEAVIATE_RESPONSE" | jq -r '.result.count // 0' 2>/dev/null || echo "0")
  echo "   Returned count: $COUNT"
else
  echo "❌ search_weaviate failed:"
  echo "$SEARCH_WEAVIATE_RESPONSE" | jq '.' 2>/dev/null || echo "$SEARCH_WEAVIATE_RESPONSE"
fi
echo ""

echo "=========================================="
echo "Test Complete"
echo "=========================================="









