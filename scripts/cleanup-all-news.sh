#!/bin/bash

# Script to delete ALL stored news events (source=news:fsm) from Weaviate
# Uses kubectl port-forward to access Weaviate from local machine
# Uses REST API to delete objects (GraphQL mutations not supported)

set -e

NAMESPACE="${K8S_NAMESPACE:-agi}"
WEAVIATE_SERVICE="${WEAVIATE_SERVICE:-weaviate}"
LOCAL_PORT=18081

echo "🔌 Setting up port forwarding to Weaviate..."
echo "   Namespace: $NAMESPACE"
echo "   Service: $WEAVIATE_SERVICE"
echo "   Local port: $LOCAL_PORT"

# Check if port is already in use
if lsof -Pi :$LOCAL_PORT -sTCP:LISTEN -t >/dev/null 2>&1; then
  echo "   ⚠️  Port $LOCAL_PORT is already in use. Killing existing process..."
  lsof -ti:$LOCAL_PORT | xargs kill -9 2>/dev/null || true
  sleep 2
fi

# Start port forwarding in background
echo "   Starting kubectl port-forward..."
kubectl port-forward -n $NAMESPACE svc/$WEAVIATE_SERVICE $LOCAL_PORT:8080 >/dev/null 2>&1 &
PF_PID=$!

# Wait for port forward to be ready
echo "   Waiting for port forward to be ready..."
sleep 3

# Check if port forward is working
if ! curl -s -f "http://localhost:$LOCAL_PORT/v1/meta" >/dev/null 2>&1; then
  echo "   ❌ Port forward failed. Trying to find Weaviate pod..."
  
  # Try to find Weaviate pod directly
  WEAVIATE_POD=$(kubectl get pods -n $NAMESPACE -l app=weaviate -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || echo "")
  
  if [ -z "$WEAVIATE_POD" ]; then
    echo "   ❌ Could not find Weaviate pod or service"
    kill $PF_PID 2>/dev/null || true
    exit 1
  fi
  
  echo "   Found pod: $WEAVIATE_POD"
  kill $PF_PID 2>/dev/null || true
  
  # Try port forwarding to pod directly
  kubectl port-forward -n $NAMESPACE pod/$WEAVIATE_POD $LOCAL_PORT:8080 >/dev/null 2>&1 &
  PF_PID=$!
  sleep 3
  
  if ! curl -s -f "http://localhost:$LOCAL_PORT/v1/meta" >/dev/null 2>&1; then
    echo "   ❌ Port forward to pod also failed"
    kill $PF_PID 2>/dev/null || true
    exit 1
  fi
fi

echo "   ✅ Port forward established (PID: $PF_PID)"
echo ""

# Cleanup function
cleanup() {
  echo ""
  echo "🧹 Cleaning up port forward..."
  kill $PF_PID 2>/dev/null || true
  wait $PF_PID 2>/dev/null || true
}

trap cleanup EXIT

WEAVIATE_URL="http://localhost:$LOCAL_PORT"

# Query for all news:fsm event IDs
echo "📊 Querying for ALL news:fsm event IDs..."

QUERY_IDS='{"query": "{ Get { WikipediaArticle(limit: 10000, where: { path: [\"source\"], operator: Equal, valueString: \"news:fsm\" }) { _additional { id } } } }"}'

IDS_RESPONSE=$(curl -s -X POST "$WEAVIATE_URL/v1/graphql" \
  -H "Content-Type: application/json" \
  -d "$QUERY_IDS")

# Extract IDs using jq or grep
if command -v jq >/dev/null 2>&1; then
  IDS=$(echo "$IDS_RESPONSE" | jq -r '.data.Get.WikipediaArticle[]?._additional.id // empty' 2>/dev/null || echo "")
else
  # Fallback: use grep to extract UUIDs
  IDS=$(echo "$IDS_RESPONSE" | grep -oE '[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}' || echo "")
fi

if [ -z "$IDS" ]; then
  echo "   ⚠️  No news:fsm events found or failed to parse response"
  echo "   Response: $IDS_RESPONSE" | head -20
  exit 0
fi

ID_COUNT=$(echo "$IDS" | wc -l | tr -d ' ')
echo "   Found $ID_COUNT news events to delete"

# Delete each ID using REST API
echo ""
echo "🗑️  Deleting $ID_COUNT news events..."

DELETED=0
FAILED=0

for ID in $IDS; do
  if [ -z "$ID" ] || [ "$ID" = "null" ]; then
    continue
  fi
  
  DELETE_RESPONSE=$(curl -s -w "\n%{http_code}" -X DELETE "$WEAVIATE_URL/v1/objects/$ID?class=WikipediaArticle" \
    -H "Content-Type: application/json")
  
  HTTP_CODE=$(echo "$DELETE_RESPONSE" | tail -1)
  
  if [ "$HTTP_CODE" = "204" ] || [ "$HTTP_CODE" = "200" ]; then
    DELETED=$((DELETED + 1))
    if [ $((DELETED % 50)) -eq 0 ]; then
      echo "   Deleted $DELETED/$ID_COUNT..."
    fi
  else
    FAILED=$((FAILED + 1))
    if [ $FAILED -le 5 ]; then
      echo "   ⚠️  Failed to delete $ID (HTTP $HTTP_CODE)"
    fi
  fi
done

echo ""
echo "   ✅ Deleted: $DELETED"
if [ $FAILED -gt 0 ]; then
  echo "   ⚠️  Failed: $FAILED"
fi

echo ""
echo "✅ Done! All old news items cleared. Re-run ingestion to populate fresh items."
