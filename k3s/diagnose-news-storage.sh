#!/bin/bash

# Diagnostic script for news event storage issues

NAMESPACE="agi"
FSM_DEPLOYMENT="fsm-server-rpi58"
WEAVIATE_DEPLOYMENT="weaviate"

echo "🔍 News Event Storage Diagnosis"
echo "================================"
echo

# 1. Check FSM logs for storage attempts
echo "1. Checking FSM logs for news storage..."
echo "----------------------------------------"
FSM_POD=$(kubectl get pods -n $NAMESPACE -l app=$FSM_DEPLOYMENT -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
if [ -z "$FSM_POD" ]; then
    echo "❌ FSM pod not found"
    exit 1
fi

echo "FSM Pod: $FSM_POD"
echo
echo "Checking for 'Storing news events' messages:"
kubectl logs -n $NAMESPACE $FSM_POD --tail=500 2>/dev/null | grep -i "storing news events" | tail -5
echo

echo "Checking for 'Stored news event in Weaviate' messages:"
STORED_COUNT=$(kubectl logs -n $NAMESPACE $FSM_POD --tail=1000 2>/dev/null | grep -c "✅ Stored news event in Weaviate" 2>/dev/null || echo "0")
echo "Found $STORED_COUNT storage success messages"
kubectl logs -n $NAMESPACE $FSM_POD --tail=1000 2>/dev/null | grep "✅ Stored news event in Weaviate" | tail -5
echo

echo "Checking for Weaviate storage errors:"
kubectl logs -n $NAMESPACE $FSM_POD --tail=1000 2>/dev/null | grep -i "weaviate.*error\|failed.*weaviate" | tail -10
echo

echo "Checking for DEBUG messages from storeNewsEventInWeaviate:"
kubectl logs -n $NAMESPACE $FSM_POD --tail=1000 2>/dev/null | grep "🔍 DEBUG: storeNewsEventInWeaviate" | tail -5
echo

# 2. Check if events are triggering the action
echo "2. Checking if news events trigger FSM actions..."
echo "---------------------------------------------------"
echo "Recent news event receipts:"
kubectl logs -n $NAMESPACE $FSM_POD --tail=200 2>/dev/null | grep "📨 Received NATS event.*news" | tail -5
echo

echo "Checking FSM state transitions after news events:"
kubectl logs -n $NAMESPACE $FSM_POD --tail=500 2>/dev/null | grep -E "State transition|Executing action.*store_news" | tail -10
echo

# 3. Check Weaviate directly
echo "3. Checking Weaviate for stored news events..."
echo "-----------------------------------------------"
WEAVIATE_POD=$(kubectl get pods -n $NAMESPACE -l app=$WEAVIATE_DEPLOYMENT -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
if [ -z "$WEAVIATE_POD" ]; then
    echo "⚠️  Weaviate pod not found"
else
    echo "Weaviate Pod: $WEAVIATE_POD"
    echo
    echo "Querying Weaviate for news:fsm events..."
    
    # Port forward to Weaviate
    kubectl port-forward -n $NAMESPACE svc/weaviate 8080:8080 >/dev/null 2>&1 &
    PF_PID=$!
    sleep 2
    
    QUERY='{
        "query": "{
            Get {
                WikipediaArticle(limit: 10, where: {
                    path: [\"source\"]
                    operator: Equal
                    valueString: \"news:fsm\"
                }) {
                    _additional { id }
                    title
                    source
                    timestamp
                }
            }
        }"
    }'
    
    RESPONSE=$(curl -s -X POST http://localhost:8080/v1/graphql \
        -H "Content-Type: application/json" \
        -d "$QUERY" 2>/dev/null)
    
    kill $PF_PID 2>/dev/null
    wait $PF_PID 2>/dev/null
    
    if echo "$RESPONSE" | jq -e '.data.Get.WikipediaArticle' >/dev/null 2>&1; then
        COUNT=$(echo "$RESPONSE" | jq '.data.Get.WikipediaArticle | length' 2>/dev/null)
        echo "✅ Found $COUNT news events in Weaviate"
        echo "$RESPONSE" | jq '.data.Get.WikipediaArticle[] | {title: .title, source: .source, timestamp: .timestamp}' 2>/dev/null
    else
        echo "❌ Failed to query Weaviate or no events found"
        echo "Response: $RESPONSE" | head -20
    fi
fi
echo

# 4. Check FSM configuration
echo "4. Checking FSM configuration..."
echo "---------------------------------"
echo "Checking if store_news_events action is configured:"
kubectl exec -n $NAMESPACE $FSM_POD -- cat /config/artificial_mind.yaml 2>/dev/null | grep -A 5 "store_news_events" || echo "⚠️  Could not read config"
echo

# 5. Check Weaviate URL in FSM
echo "5. Checking FSM environment..."
echo "------------------------------"
echo "WEAVIATE_URL:"
kubectl exec -n $NAMESPACE $FSM_POD -- sh -c 'echo $WEAVIATE_URL' 2>/dev/null
echo

# 6. Recommendations
echo "6. Recommendations"
echo "-----------------"
if [ "$STORED_COUNT" = "0" ]; then
    echo "❌ No news events have been stored in Weaviate"
    echo "   → Check if FSM is in the correct state when news events arrive"
    echo "   → Check if executeNewsStorage is being called"
    echo "   → Check Weaviate connectivity from FSM pod"
    echo "   → Check FSM logs for errors"
else
    echo "✅ Some events have been stored ($STORED_COUNT)"
    echo "   → Check if recent events are being stored"
    echo "   → Verify Monitor UI is querying correctly"
fi

echo
echo "To manually test storage:"
echo "  kubectl logs -n $NAMESPACE $FSM_POD -f | grep -E 'news|Weaviate|storeNewsEvent'"
echo
echo "To check Monitor UI query:"
echo "  kubectl port-forward -n $NAMESPACE svc/monitor-ui 8082:8082"
echo "  curl http://localhost:8082/api/news/events"





