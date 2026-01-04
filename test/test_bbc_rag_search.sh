#!/bin/bash

# Test script to verify BBC news items are in Weaviate and searchable via RAG
# Usage: ./test_bbc_rag_search.sh [WEAVIATE_URL] [QUERY]

WEAVIATE_URL="${1:-http://localhost:8080}"
QUERY="${2:-Lindsay Foreman}"

echo "🔍 Testing BBC News RAG Search"
echo "Weaviate URL: $WEAVIATE_URL"
echo "Query: $QUERY"
echo ""

# Test 1: Check if WikipediaArticle collection exists and has items
echo "📊 Step 1: Checking WikipediaArticle collection..."
QUERY_GRAPHQL='{
  "query": "{
    Get {
      WikipediaArticle(limit: 5) {
        _additional {
          id
        }
        title
        source
        timestamp
      }
    }
  }"
}'

RESPONSE=$(curl -s -X POST "$WEAVIATE_URL/v1/graphql" \
  -H "Content-Type: application/json" \
  -d "$QUERY_GRAPHQL")

COUNT=$(echo "$RESPONSE" | jq -r '.data.Get.WikipediaArticle | length' 2>/dev/null)
COUNT=${COUNT:-0}
if [ -z "$COUNT" ] || [ "$COUNT" = "null" ]; then
    COUNT=0
fi
echo "Found $COUNT WikipediaArticle items"
if [ "$COUNT" -gt 0 ] 2>/dev/null; then
    echo "Sample items:"
    echo "$RESPONSE" | jq -r '.data.Get.WikipediaArticle[] | "  - \(.title) (source: \(.source))"' 2>/dev/null | head -5
fi
echo ""

# Test 2: Check for BBC news items specifically
echo "📰 Step 2: Checking for BBC news items..."
QUERY_BBC='{
  "query": "{
    Get {
      WikipediaArticle(where: {path: [\"source\"], operator: Like, valueString: \"*bbc*\"}, limit: 10) {
        _additional {
          id
        }
        title
        source
        text
        timestamp
      }
    }
  }"
}'

RESPONSE_BBC=$(curl -s -X POST "$WEAVIATE_URL/v1/graphql" \
  -H "Content-Type: application/json" \
  -d "$QUERY_BBC")

BBC_COUNT=$(echo "$RESPONSE_BBC" | jq -r '.data.Get.WikipediaArticle | length' 2>/dev/null)
BBC_COUNT=${BBC_COUNT:-0}
if [ -z "$BBC_COUNT" ] || [ "$BBC_COUNT" = "null" ]; then
    BBC_COUNT=0
fi
echo "Found $BBC_COUNT BBC news items"
if [ "$BBC_COUNT" -gt 0 ] 2>/dev/null; then
    echo "Sample BBC items:"
    echo "$RESPONSE_BBC" | jq -r '.data.Get.WikipediaArticle[] | "  - \(.title) (\(.timestamp))"' 2>/dev/null | head -5
else
    echo "⚠️  No BBC news items found in Weaviate!"
    echo "   This could mean:"
    echo "   1. BBC ingestor hasn't run yet"
    echo "   2. FSM server isn't storing news events"
    echo "   3. Items are stored without vectors (check logs)"
fi
echo ""

# Test 3: Check if items have vectors
echo "🔢 Step 3: Checking if items have vectors..."
QUERY_VECTOR='{
  "query": "{
    Get {
      WikipediaArticle(limit: 1) {
        _additional {
          id
          vector
        }
        title
      }
    }
  }"
}'

RESPONSE_VECTOR=$(curl -s -X POST "$WEAVIATE_URL/v1/graphql" \
  -H "Content-Type: application/json" \
  -d "$QUERY_VECTOR")

HAS_VECTOR=$(echo "$RESPONSE_VECTOR" | jq -r '.data.Get.WikipediaArticle[0]._additional.vector != null' 2>/dev/null || echo "false")
if [ "$HAS_VECTOR" = "true" ]; then
    VECTOR_DIM=$(echo "$RESPONSE_VECTOR" | jq -r '.data.Get.WikipediaArticle[0]._additional.vector | length' 2>/dev/null || echo "0")
    echo "✅ Items have vectors (dimension: $VECTOR_DIM)"
else
    echo "❌ Items do NOT have vectors - they won't be searchable!"
    echo "   This is the problem - vectors need to be generated when storing."
fi
echo ""

# Test 4: Try a vector similarity search (if we can generate a query vector)
echo "🔍 Step 4: Testing vector similarity search..."
# Generate a simple 8-dim vector for the query (same as toyEmbed)
# This is a simplified version - in production you'd use the same embedding function
QUERY_TEXT="$QUERY"
# Simple hash-based vector (matching the approach)
HASH=0
for ((i=0; i<${#QUERY_TEXT}; i++)); do
    CHAR=$(printf "%d" "'${QUERY_TEXT:$i:1}")
    HASH=$((HASH * 31 + CHAR))
done

# Generate 8-dim vector
VECTOR="["
for i in {0..7}; do
    if [ $i -gt 0 ]; then
        VECTOR+=","
    fi
    # Simple hash-based value
    VAL=$(( (HASH + i * 17) % 1000 ))
    NORMALIZED=$(echo "scale=6; ($VAL / 1000.0) - 0.5" | bc)
    VECTOR+="$NORMALIZED"
done
VECTOR+="]"

QUERY_SIMILARITY="{
  \"query\": \"{
    Get {
      WikipediaArticle(nearVector: {vector: $VECTOR}, limit: 5) {
        _additional {
          id
          distance
        }
        title
        text
        source
      }
    }
  }\"
}"

RESPONSE_SIM=$(curl -s -X POST "$WEAVIATE_URL/v1/graphql" \
  -H "Content-Type: application/json" \
  -d "$QUERY_SIMILARITY")

SIM_COUNT=$(echo "$RESPONSE_SIM" | jq -r '.data.Get.WikipediaArticle | length' 2>/dev/null)
SIM_COUNT=${SIM_COUNT:-0}
if [ -z "$SIM_COUNT" ] || [ "$SIM_COUNT" = "null" ]; then
    SIM_COUNT=0
fi
echo "Vector similarity search returned $SIM_COUNT results"
if [ "$SIM_COUNT" -gt 0 ] 2>/dev/null; then
    echo "Top results:"
    echo "$RESPONSE_SIM" | jq -r '.data.Get.WikipediaArticle[] | "  - \(.title) (distance: \(._additional.distance), source: \(.source))"' 2>/dev/null | head -5
else
    echo "⚠️  No results from vector search"
    if [ "$HAS_VECTOR" != "true" ]; then
        echo "   This is expected if items don't have vectors"
    fi
fi
echo ""

# Summary
echo "📋 Summary:"
echo "  - Total WikipediaArticle items: $COUNT"
echo "  - BBC news items: $BBC_COUNT"
echo "  - Items have vectors: $HAS_VECTOR"
echo "  - Vector search results: $SIM_COUNT"
echo ""
if [ "$BBC_COUNT" -eq 0 ] 2>/dev/null; then
    echo "❌ ISSUE: No BBC news items found in Weaviate"
    echo "   Check: FSM server logs for 'storeNewsEventInWeaviate' messages"
elif [ "$HAS_VECTOR" != "true" ]; then
    echo "❌ ISSUE: Items stored without vectors - not searchable"
    echo "   Fix: Rebuild FSM server with vector generation fix"
else
    echo "✅ BBC news items appear to be properly indexed in Weaviate"
fi

