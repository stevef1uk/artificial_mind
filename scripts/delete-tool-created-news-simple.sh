#!/bin/bash

# Simpler script to delete tool_created news events from Weaviate
# Uses Weaviate batch delete API

WEAVIATE_URL="${WEAVIATE_URL:-http://weaviate.agi.svc.cluster.local:8080}"

# Try to detect Weaviate URL from environment or Kubernetes
if ! curl -s -f "$WEAVIATE_URL/v1/meta" >/dev/null 2>&1; then
  # Try common alternatives
  for alt_url in "http://localhost:8080" "http://weaviate:8080" "http://weaviate.agi.svc.cluster.local:8080"; do
    if curl -s -f "$alt_url/v1/meta" >/dev/null 2>&1; then
      WEAVIATE_URL="$alt_url"
      break
    fi
  done
fi

echo "🗑️  Deleting tool_created news events from Weaviate..."
echo "   Weaviate URL: $WEAVIATE_URL"

# Check if Weaviate is reachable
if ! curl -s -f "$WEAVIATE_URL/v1/meta" >/dev/null 2>&1; then
  echo "   ❌ Weaviate not reachable at $WEAVIATE_URL"
  echo "   Please set WEAVIATE_URL environment variable"
  exit 1
fi

# Use GraphQL mutation to delete
echo ""
echo "🗑️  Executing delete mutation..."

# Simple delete using GraphQL - delete all with matching title pattern
DELETE_QUERY='{
  "query": "mutation { Delete { WikipediaArticle(where: { path: [\"title\"], operator: Like, valueText: \"*News Event: agi.tool.created*\" }) { __typename } } }"
}'

RESPONSE=$(curl -s -w "\n%{http_code}" -X POST "$WEAVIATE_URL/v1/graphql" \
  -H "Content-Type: application/json" \
  -d "$DELETE_QUERY")

HTTP_CODE=$(echo "$RESPONSE" | tail -1)
BODY=$(echo "$RESPONSE" | head -n -1)

echo "   HTTP Code: $HTTP_CODE"
echo "   Response: $BODY"

if [ "$HTTP_CODE" = "200" ]; then
  echo ""
  echo "✅ Delete completed successfully!"
  echo ""
  echo "You can verify by checking the monitor UI - it should now show more news articles."
else
  echo ""
  echo "⚠️  Delete may have failed. HTTP code: $HTTP_CODE"
  echo "   Response: $BODY"
  echo ""
  echo "You may need to check Weaviate logs or try a different approach."
fi

