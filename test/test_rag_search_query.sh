#!/bin/bash

# Test RAG search via the HDN API
# This tests the full flow: user query -> Neo4j search -> RAG fallback -> results

set -e

HDN_URL="${HDN_URL:-http://localhost:8081}"
QUERY="${1:-who is Lindsay Foreman}"

echo "🔍 Testing RAG Search via HDN API"
echo "HDN URL: $HDN_URL"
echo "Query: $QUERY"
echo ""

# Test 1: Use the knowledge query endpoint (direct)
echo "📊 Test 1: Direct knowledge query endpoint..."
echo "POST $HDN_URL/api/v1/knowledge/query"
echo ""

RESPONSE=$(curl -s -X POST "$HDN_URL/api/v1/knowledge/query" \
  -H "Content-Type: application/json" \
  -d "{
    \"query\": \"$QUERY\",
    \"session_id\": \"test_rag_$(date +%s)\"
  }")

echo "Response:"
echo "$RESPONSE" | jq '.' 2>/dev/null || echo "$RESPONSE"
echo ""

# Test 2: Use the conversational chat endpoint (user-facing)
echo "📊 Test 2: Conversational chat endpoint (user-facing)..."
echo "POST $HDN_URL/api/v1/chat/text"
echo ""

CHAT_RESPONSE=$(curl -s -X POST "$HDN_URL/api/v1/chat/text" \
  -H "Content-Type: application/json" \
  -d "{
    \"message\": \"$QUERY\",
    \"session_id\": \"test_rag_chat_$(date +%s)\"
  }")

echo "Response:"
echo "$CHAT_RESPONSE" | jq '.' 2>/dev/null || echo "$CHAT_RESPONSE"
echo ""

# Extract key information
if echo "$CHAT_RESPONSE" | jq -e '.response' >/dev/null 2>&1; then
    echo "✅ Got response from chat endpoint"
    echo ""
    echo "Response text:"
    echo "$CHAT_RESPONSE" | jq -r '.response' 2>/dev/null | head -20
    echo ""
    
    # Check if RAG search was used
    if echo "$CHAT_RESPONSE" | grep -qi "weaviate\|rag\|vector\|episodic\|news"; then
        echo "✅ Response mentions RAG/Weaviate (likely used RAG search)"
    fi
fi

echo ""
echo "=========================================="
echo "Summary"
echo "=========================================="
echo ""
echo "To test RAG search, you can:"
echo ""
echo "1. Direct knowledge query:"
echo "   curl -X POST $HDN_URL/api/v1/knowledge/query \\"
echo "     -H 'Content-Type: application/json' \\"
echo "     -d '{\"query\": \"$QUERY\", \"session_id\": \"test\"}'"
echo ""
echo "2. Conversational chat (user-facing):"
echo "   curl -X POST $HDN_URL/api/v1/chat/text \\"
echo "     -H 'Content-Type: application/json' \\"
echo "     -d '{\"message\": \"$QUERY\", \"session_id\": \"test\"}'"
echo ""
echo "3. Monitor UI (if running):"
echo "   Open http://localhost:8082 and use the RAG Search tab"
echo ""









