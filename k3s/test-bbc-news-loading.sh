#!/bin/bash

# Test script to verify BBC news entries are being loaded
# Tests the full pipeline: news-ingestor -> NATS -> FSM -> Weaviate -> Monitor UI

set -e

NAMESPACE="agi"
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

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
echo "BBC News Loading Test"
echo "=========================================="
echo ""

# Test 1: Check news-ingestor cronjob exists
print_status "Test 1: Checking news-ingestor cronjob..."
if kubectl get cronjob news-ingestor-cronjob -n $NAMESPACE >/dev/null 2>&1; then
    SCHEDULE=$(kubectl get cronjob news-ingestor-cronjob -n $NAMESPACE -o jsonpath='{.spec.schedule}')
    print_success "CronJob exists (schedule: $SCHEDULE)"
else
    print_error "CronJob not found"
    exit 1
fi
echo ""

# Test 2: Check if we can manually trigger a job
print_status "Test 2: Creating manual test job..."
JOB_NAME="news-ingestor-test-$(date +%s)"
if kubectl create job "$JOB_NAME" --from=cronjob/news-ingestor-cronjob -n $NAMESPACE >/dev/null 2>&1; then
    print_success "Test job created: $JOB_NAME"
    
    # Wait for job to start
    print_status "Waiting for job to start..."
    sleep 5
    
    # Get pod name
    POD_NAME=$(kubectl get pods -n $NAMESPACE -l job-name="$JOB_NAME" -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || echo "")
    if [ -n "$POD_NAME" ]; then
        print_success "Job pod running: $POD_NAME"
        
        # Wait for completion (with timeout)
        print_status "Waiting for job to complete (max 5 minutes)..."
        TIMEOUT=300
        ELAPSED=0
        while [ $ELAPSED -lt $TIMEOUT ]; do
            STATUS=$(kubectl get pod "$POD_NAME" -n $NAMESPACE -o jsonpath='{.status.phase}' 2>/dev/null || echo "Unknown")
            if [ "$STATUS" = "Succeeded" ]; then
                print_success "Job completed successfully"
                break
            elif [ "$STATUS" = "Failed" ]; then
                print_error "Job failed"
                echo "Last 50 lines of logs:"
                kubectl logs "$POD_NAME" -n $NAMESPACE --tail=50 2>&1 | sed 's/^/  /'
                exit 1
            fi
            sleep 5
            ELAPSED=$((ELAPSED + 5))
            echo -n "."
        done
        echo ""
        
        if [ $ELAPSED -ge $TIMEOUT ]; then
            print_warning "Job timed out, checking logs..."
        fi
    else
        print_warning "Could not find job pod"
    fi
else
    print_warning "Could not create test job (may already exist)"
    # Try to find existing test job
    EXISTING_JOB=$(kubectl get jobs -n $NAMESPACE -l job-name --sort-by=.metadata.creationTimestamp -o jsonpath='{.items[-1].metadata.name}' 2>/dev/null | grep news-ingestor || echo "")
    if [ -n "$EXISTING_JOB" ]; then
        POD_NAME=$(kubectl get pods -n $NAMESPACE -l job-name="$EXISTING_JOB" -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || echo "")
        if [ -n "$POD_NAME" ]; then
            print_status "Using existing job pod: $POD_NAME"
        fi
    fi
fi
echo ""

# Test 3: Check news-ingestor logs for published events
if [ -n "$POD_NAME" ]; then
    print_status "Test 3: Checking news-ingestor logs for published events..."
    LOGS=$(kubectl logs "$POD_NAME" -n $NAMESPACE 2>&1 || echo "")
    
    if echo "$LOGS" | grep -qE "ALERT|REL|publish"; then
        PUBLISHED_COUNT=$(echo "$LOGS" | grep -cE "ALERT|REL" || echo "0")
        print_success "Found $PUBLISHED_COUNT published events in logs"
        
        # Show sample events
        echo "Sample events:"
        echo "$LOGS" | grep -E "ALERT|REL" | head -5 | sed 's/^/  /'
    else
        print_warning "No published events found in logs"
        echo "Last 20 lines:"
        echo "$LOGS" | tail -20 | sed 's/^/  /'
    fi
else
    print_warning "Skipping log check (no pod found)"
fi
echo ""

# Test 4: Check NATS for published events
print_status "Test 4: Checking NATS for news events..."
NATS_POD=$(kubectl get pods -n $NAMESPACE -l app=nats -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || echo "")
if [ -n "$NATS_POD" ]; then
    # Check NATS stats (this is a simple check - actual message inspection would require NATS CLI)
    print_success "NATS pod found: $NATS_POD"
    print_status "Note: Message inspection requires NATS CLI tools"
else
    print_warning "NATS pod not found"
fi
echo ""

# Test 5: Check FSM is processing news events
print_status "Test 5: Checking FSM logs for news event processing..."
FSM_POD=$(kubectl get pods -n $NAMESPACE -l app=fsm-server-rpi58 -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || echo "")
if [ -n "$FSM_POD" ]; then
    print_success "FSM pod found: $FSM_POD"
    
    # Check recent logs for news event processing
    FSM_LOGS=$(kubectl logs "$FSM_POD" -n $NAMESPACE --tail=100 --since=10m 2>&1 || echo "")
    
    if echo "$FSM_LOGS" | grep -qE "news_alerts|news_relations|storeNewsEventInWeaviate|Stored news"; then
        print_success "FSM is processing news events"
        echo "Recent news-related log entries:"
        echo "$FSM_LOGS" | grep -E "news_alerts|news_relations|storeNewsEventInWeaviate|Stored news" | tail -5 | sed 's/^/  /'
    else
        print_warning "No news event processing found in recent FSM logs"
        print_status "This might be normal if events were processed earlier"
    fi
else
    print_error "FSM pod not found"
fi
echo ""

# Test 6: Check Weaviate for stored news events
print_status "Test 6: Checking Weaviate for stored news events..."
WEAVIATE_POD=$(kubectl get pods -n $NAMESPACE -l app=weaviate -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || echo "")
if [ -n "$WEAVIATE_POD" ]; then
    print_success "Weaviate pod found: $WEAVIATE_POD"
    
    # Query Weaviate for recent news events
    QUERY='{"query":"{Get{WikipediaArticle(limit:10 where:{path:[\"source\"] operator:Equal valueString:\"news:fsm\"} sort:[{path:[\"timestamp\"] order:desc}]){_additional{id} title source timestamp url}}}"}'
    
    RESULT=$(kubectl exec -n $NAMESPACE "$WEAVIATE_POD" -- wget -q -O- 'http://localhost:8080/v1/graphql' \
        --post-data "$QUERY" \
        --header='Content-Type: application/json' 2>/dev/null || echo "")
    
    if [ -n "$RESULT" ]; then
        # Parse result to count events
        COUNT=$(echo "$RESULT" | python3 -c "import sys, json; data=json.load(sys.stdin); print(len(data.get('data', {}).get('Get', {}).get('WikipediaArticle', [])))" 2>/dev/null || echo "0")
        
        if [ "$COUNT" -gt 0 ]; then
            print_success "Found $COUNT news events in Weaviate"
            
            # Show recent events
            echo "Recent news events (last 5):"
            echo "$RESULT" | python3 -c "
import sys, json
data = json.load(sys.stdin)
articles = data.get('data', {}).get('Get', {}).get('WikipediaArticle', [])[:5]
for a in articles:
    print(f\"  - {a.get('title', 'No title')[:60]}... ({a.get('timestamp', 'No timestamp')})\")
" 2>/dev/null || echo "  (Could not parse results)"
            
            # Check if any are from today
            TODAY=$(date +%Y-%m-%d)
            TODAY_COUNT=$(echo "$RESULT" | python3 -c "
import sys, json
from datetime import datetime
data = json.load(sys.stdin)
articles = data.get('data', {}).get('Get', {}).get('WikipediaArticle', [])
today = '$TODAY'
count = sum(1 for a in articles if a.get('timestamp', '').startswith(today))
print(count)
" 2>/dev/null || echo "0")
            
            if [ "$TODAY_COUNT" -gt 0 ]; then
                print_success "$TODAY_COUNT events from today found"
            else
                print_warning "No events from today found (most recent may be older)"
            fi
        else
            print_warning "No news events found in Weaviate"
            print_status "This could mean:"
            print_status "  1. Events haven't been stored yet (wait a few minutes)"
            print_status "  2. FSM hasn't processed them yet"
            print_status "  3. Events failed to store"
        fi
    else
        print_warning "Could not query Weaviate"
    fi
else
    print_error "Weaviate pod not found"
fi
echo ""

# Test 7: Check Monitor UI API endpoint
print_status "Test 7: Checking Monitor UI API for news events..."
MONITOR_POD=$(kubectl get pods -n $NAMESPACE -l app=monitor-ui -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || echo "")
if [ -n "$MONITOR_POD" ]; then
    print_success "Monitor UI pod found: $MONITOR_POD"
    
    # Port forward and test API (or use service)
    print_status "Testing /api/news endpoint..."
    # This would require port-forwarding or accessing via service
    print_status "Note: Access Monitor UI at http://monitor-ui.agi.svc.cluster.local:8082/api/news"
else
    print_warning "Monitor UI pod not found"
fi
echo ""

# Summary
echo "=========================================="
echo "Test Summary"
echo "=========================================="
echo ""
echo "To verify BBC news loading:"
echo "  1. Check news-ingestor logs: kubectl logs -n $NAMESPACE -l job-name=$JOB_NAME"
echo "  2. Check FSM logs: kubectl logs -n $NAMESPACE $FSM_POD | grep news"
echo "  3. Query Weaviate directly (see Test 6 above)"
echo "  4. Check Monitor UI Overview page"
echo ""
echo "To manually trigger another ingestion:"
echo "  kubectl create job news-ingestor-manual-\$(date +%s) --from=cronjob/news-ingestor-cronjob -n $NAMESPACE"
echo ""
echo "To watch FSM processing in real-time:"
echo "  kubectl logs -n $NAMESPACE $FSM_POD -f | grep -E 'news|News'"
echo ""









