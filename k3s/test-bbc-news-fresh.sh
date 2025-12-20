#!/bin/bash

# Test script to verify BBC news loading with fresh data
# Clears duplicate tracking and runs a fresh ingestion

set -e

NAMESPACE="agi"
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

print_status() {
    echo -e "${BLUE}[TEST]${NC} $1"
}

print_success() {
    echo -e "${GREEN}[PASS]${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

print_error() {
    echo -e "${RED}[FAIL]${NC} $1"
}

echo "=========================================="
echo "BBC News Fresh Loading Test"
echo "=========================================="
echo ""

# Step 1: Clear duplicate tracking in Redis
print_status "Step 1: Clearing duplicate tracking in Redis..."
REDIS_POD=$(kubectl get pods -n $NAMESPACE -l app=redis -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || echo "")
if [ -n "$REDIS_POD" ]; then
    # Use a simple Lua script passed as a string (more reliable than heredoc)
    LUA_SCRIPT='local keys = redis.call("KEYS", "news:duplicates:*"); if #keys > 0 then return redis.call("DEL", unpack(keys)) else return 0 end'
    DELETED=$(kubectl exec -n $NAMESPACE "$REDIS_POD" -- redis-cli EVAL "$LUA_SCRIPT" 0 2>/dev/null || echo "0")
    
    if [ "$DELETED" != "0" ] && [ -n "$DELETED" ]; then
        print_success "Cleared $DELETED duplicate tracking keys"
    else
        print_status "No duplicate keys found (fresh start or already cleared)"
    fi
else
    print_warning "Redis pod not found, skipping duplicate clearing"
fi
echo ""

# Step 2: Create a test job
print_status "Step 2: Creating fresh test job..."
JOB_NAME="news-ingestor-fresh-$(date +%s)"
if kubectl create job "$JOB_NAME" --from=cronjob/news-ingestor-cronjob -n $NAMESPACE >/dev/null 2>&1; then
    print_success "Test job created: $JOB_NAME"
    
    # Wait for pod
    print_status "Waiting for pod to start..."
    sleep 10
    
    POD_NAME=$(kubectl get pods -n $NAMESPACE -l job-name="$JOB_NAME" -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || echo "")
    if [ -z "$POD_NAME" ]; then
        print_error "Pod not found"
        exit 1
    fi
    
    print_success "Pod running: $POD_NAME"
    echo ""
    
    # Step 3: Monitor logs in real-time
    print_status "Step 3: Monitoring ingestion progress..."
    print_status "Watching logs (will show last 20 lines when complete)..."
    echo ""
    
    # Wait for completion with progress updates
    TIMEOUT=300
    ELAPSED=0
    while [ $ELAPSED -lt $TIMEOUT ]; do
        STATUS=$(kubectl get pod "$POD_NAME" -n $NAMESPACE -o jsonpath='{.status.phase}' 2>/dev/null || echo "Unknown")
        
        if [ "$STATUS" = "Succeeded" ]; then
            print_success "Job completed!"
            break
        elif [ "$STATUS" = "Failed" ]; then
            print_error "Job failed"
            echo "Logs:"
            kubectl logs "$POD_NAME" -n $NAMESPACE --tail=50 2>&1 | sed 's/^/  /'
            exit 1
        fi
        
        # Show progress every 30 seconds
        if [ $((ELAPSED % 30)) -eq 0 ] && [ $ELAPSED -gt 0 ]; then
            echo -n "  Still running... (${ELAPSED}s elapsed) "
            # Show recent log snippet
            RECENT=$(kubectl logs "$POD_NAME" -n $NAMESPACE --tail=3 2>&1 | tail -1)
            if [ -n "$RECENT" ]; then
                echo "| $RECENT"
            else
                echo ""
            fi
        fi
        
        sleep 5
        ELAPSED=$((ELAPSED + 5))
    done
    
    if [ $ELAPSED -ge $TIMEOUT ]; then
        print_warning "Job timed out after 5 minutes"
    fi
    
    echo ""
    print_status "Final logs:"
    echo "----------------------------------------"
    kubectl logs "$POD_NAME" -n $NAMESPACE 2>&1 | tail -30 | sed 's/^/  /'
    echo "----------------------------------------"
    echo ""
    
    # Step 4: Analyze results
    print_status "Step 4: Analyzing results..."
    LOGS=$(kubectl logs "$POD_NAME" -n $NAMESPACE 2>&1)
    
    # Count discovered stories
    DISCOVERED=$(echo "$LOGS" | grep -oP "discovered \K\d+" | head -1 || echo "0")
    if [ "$DISCOVERED" -gt 0 ]; then
        print_success "Discovered $DISCOVERED stories"
    fi
    
    # Count processed (non-duplicate) stories
    PROCESSED=$(echo "$LOGS" | grep -oP "Processing \K\d+" | head -1 || echo "0")
    if [ "$PROCESSED" -gt 0 ]; then
        print_success "Processed $PROCESSED new stories"
    else
        print_warning "No new stories processed (all duplicates or LLM failed)"
    fi
    
    # Count published events
    ALERT_COUNT=$(echo "$LOGS" | grep -cE "\[LLM\] ALERT|\[FALLBACK\] ALERT" || echo "0")
    REL_COUNT=$(echo "$LOGS" | grep -cE "\[LLM\] REL|\[FALLBACK\] REL" || echo "0")
    SKIP_COUNT=$(echo "$LOGS" | grep -cE "\[LLM\] SKIP|\[FALLBACK\] SKIP" || echo "0")
    
    echo ""
    print_status "Event classification:"
    echo "  Alerts: $ALERT_COUNT"
    echo "  Relations: $REL_COUNT"
    echo "  Skipped: $SKIP_COUNT"
    echo ""
    
    # Check for LLM errors
    LLM_ERRORS=$(echo "$LOGS" | grep -c "LLM error" || echo "0")
    if [ "$LLM_ERRORS" -gt 0 ]; then
        print_warning "Found $LLM_ERRORS LLM errors (timeouts expected if Ollama is slow)"
    fi
    
    # Step 5: Check if events made it to Weaviate
    print_status "Step 5: Checking Weaviate for new events (waiting 30s for FSM processing)..."
    sleep 30
    
    WEAVIATE_POD=$(kubectl get pods -n $NAMESPACE -l app=weaviate -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || echo "")
    if [ -n "$WEAVIATE_POD" ]; then
        # Get events from last 5 minutes
        FIVE_MINS_AGO=$(date -u -d '5 minutes ago' +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || date -u -v-5M +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || echo "")
        
        QUERY='{"query":"{Get{WikipediaArticle(limit:20 where:{operator:And operands:[{path:[\"source\"] operator:Equal valueString:\"news:fsm\"},{path:[\"timestamp\"] operator:GreaterThan valueString:\"'$FIVE_MINS_AGO'\"}]} sort:[{path:[\"timestamp\"] order:desc}]){title source timestamp url}}}"}'
        
        RESULT=$(kubectl exec -n $NAMESPACE "$WEAVIATE_POD" -- wget -q -O- 'http://localhost:8080/v1/graphql' \
            --post-data "$QUERY" \
            --header='Content-Type: application/json' 2>/dev/null || echo "")
        
        if [ -n "$RESULT" ]; then
            RECENT_COUNT=$(echo "$RESULT" | python3 -c '
import sys, json
try:
    data = json.load(sys.stdin)
    articles = data.get("data", {}).get("Get", {}).get("WikipediaArticle", [])
    # Filter out tool.created events
    news_articles = [a for a in articles if "tool.created" not in a.get("title", "")]
    print(len(news_articles))
except:
    print(0)
' 2>/dev/null || echo "0")
            
            if [ "$RECENT_COUNT" -gt 0 ]; then
                print_success "Found $RECENT_COUNT recent news events in Weaviate!"
                echo "Recent events:"
                echo "$RESULT" | python3 <<'PYTHON_SCRIPT'
import sys, json
data = json.load(sys.stdin)
articles = data.get("data", {}).get("Get", {}).get("WikipediaArticle", [])
for a in articles[:5]:
    title = a.get("title", "No title")
    if "tool.created" not in title:
        print("  - " + title[:70])
PYTHON_SCRIPT
                if [ $? -ne 0 ]; then
                    echo "  Could not parse results"
                fi
            else
                print_warning "No recent news events in Weaviate yet"
                print_status "FSM may still be processing, or events failed to store"
            fi
        fi
    fi
    
else
    print_error "Could not create test job"
    exit 1
fi

echo ""
echo "=========================================="
echo "Test Complete"
echo "=========================================="
echo ""
echo "To check Monitor UI:"
echo "  Access: http://monitor-ui.agi.svc.cluster.local:8082"
echo "  Or port-forward: kubectl port-forward -n $NAMESPACE svc/monitor-ui 8082:8082"
echo ""
echo "To check FSM processing:"
echo "  kubectl logs -n $NAMESPACE -l app=fsm-server-rpi58 --tail=100 | grep -E 'news|News|storeNewsEventInWeaviate'"
echo ""

