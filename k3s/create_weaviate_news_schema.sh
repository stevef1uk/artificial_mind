#!/bin/bash
# Create WikipediaArticle schema class in Weaviate for news events

WEAVIATE_URL="${WEAVIATE_URL:-http://weaviate.agi.svc.cluster.local:8080}"

echo "Creating WikipediaArticle schema in Weaviate..."
echo "URL: $WEAVIATE_URL"

SCHEMA='{
  "class": "WikipediaArticle",
  "description": "Wikipedia articles and news events",
  "vectorizer": "none",
  "properties": [
    {
      "name": "title",
      "dataType": ["string"],
      "description": "Article title"
    },
    {
      "name": "text",
      "dataType": ["text"],
      "description": "Article text content"
    },
    {
      "name": "source",
      "dataType": ["string"],
      "description": "Source of the article (e.g., news:fsm)"
    },
    {
      "name": "url",
      "dataType": ["string"],
      "description": "Article URL"
    },
    {
      "name": "timestamp",
      "dataType": ["string"],
      "description": "Article timestamp in RFC3339 format"
    },
    {
      "name": "metadata",
      "dataType": ["string"],
      "description": "Additional metadata as JSON string"
    }
  ]
}'

RESPONSE=$(curl -s -X POST "$WEAVIATE_URL/v1/schema" \
  -H "Content-Type: application/json" \
  -d "$SCHEMA")

echo "$RESPONSE" | jq '.' 2>/dev/null || echo "$RESPONSE"

# Verify it was created
echo ""
echo "Verifying schema creation..."
VERIFY=$(curl -s "$WEAVIATE_URL/v1/schema/WikipediaArticle")
if echo "$VERIFY" | jq -e '.class' > /dev/null 2>&1; then
  echo "✅ WikipediaArticle schema created successfully!"
  echo "$VERIFY" | jq '.class, .properties[].name'
else
  echo "❌ Failed to create schema or verify"
  echo "$VERIFY"
fi





