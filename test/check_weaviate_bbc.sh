#!/bin/bash

# Simple script to check Weaviate for BBC news items
# Usage: ./check_weaviate_bbc.sh [WEAVIATE_URL]

WEAVIATE_URL="${1:-http://weaviate.agi.svc.cluster.local:8080}"

echo "🔍 Checking Weaviate for BBC News Items"
echo "URL: $WEAVIATE_URL"
echo ""

# Check schema
echo "📋 Step 1: Checking Weaviate schema..."
SCHEMA=$(curl -s -X GET "$WEAVIATE_URL/v1/schema")
echo "$SCHEMA" | jq '.classes[] | .class' 2>/dev/null || echo "Failed to get schema or parse response"
echo ""

# Check WikipediaArticle collection
echo "📰 Step 2: Querying WikipediaArticle collection..."
QUERY='{"query": "{ Get { WikipediaArticle(limit: 10) { _additional { id } title source timestamp } } }"}'
RESPONSE=$(curl -s -X POST "$WEAVIATE_URL/v1/graphql" \
  -H "Content-Type: application/json" \
  -d "$QUERY")

echo "Raw response:"
echo "$RESPONSE" | jq '.' 2>/dev/null || echo "$RESPONSE"
echo ""

# Check for errors
ERRORS=$(echo "$RESPONSE" | jq -r '.errors' 2>/dev/null)
if [ "$ERRORS" != "null" ] && [ -n "$ERRORS" ]; then
    echo "❌ GraphQL Errors:"
    echo "$RESPONSE" | jq '.errors' 2>/dev/null
    echo ""
fi

# Check data
DATA=$(echo "$RESPONSE" | jq -r '.data.Get.WikipediaArticle' 2>/dev/null)
if [ "$DATA" != "null" ] && [ -n "$DATA" ]; then
    COUNT=$(echo "$DATA" | jq 'length' 2>/dev/null)
    echo "✅ Found $COUNT items in WikipediaArticle collection"
    if [ "$COUNT" -gt 0 ] 2>/dev/null; then
        echo ""
        echo "Sample items:"
        echo "$DATA" | jq -r '.[] | "  Title: \(.title // "N/A")\n  Source: \(.source // "N/A")\n  Timestamp: \(.timestamp // "N/A")\n"' 2>/dev/null | head -20
    fi
else
    echo "⚠️  No data returned or collection doesn't exist"
fi
echo ""

# Check if collection exists by trying to get one item with _additional.vector
echo "🔢 Step 3: Checking if items have vectors..."
VECTOR_QUERY='{"query": "{ Get { WikipediaArticle(limit: 1) { _additional { id vector } title } } }"}'
VECTOR_RESPONSE=$(curl -s -X POST "$WEAVIATE_URL/v1/graphql" \
  -H "Content-Type: application/json" \
  -d "$VECTOR_QUERY")

VECTOR_DATA=$(echo "$VECTOR_RESPONSE" | jq -r '.data.Get.WikipediaArticle[0]' 2>/dev/null)
if [ "$VECTOR_DATA" != "null" ] && [ -n "$VECTOR_DATA" ]; then
    HAS_VECTOR=$(echo "$VECTOR_DATA" | jq -r '._additional.vector != null' 2>/dev/null)
    if [ "$HAS_VECTOR" = "true" ]; then
        VECTOR_LEN=$(echo "$VECTOR_DATA" | jq -r '._additional.vector | length' 2>/dev/null)
        echo "✅ Items have vectors (dimension: $VECTOR_LEN)"
    else
        echo "❌ Items do NOT have vectors"
    fi
else
    echo "⚠️  No items found to check for vectors"
fi









