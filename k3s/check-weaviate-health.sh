#!/bin/bash

# Check Weaviate health and performance

set -e

NAMESPACE="agi"
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

print_status() {
    echo -e "${BLUE}[CHECK]${NC} $1"
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
echo "Weaviate Health Check"
echo "=========================================="
echo ""

# Step 1: Check if Weaviate pod is running
print_status "Step 1: Checking Weaviate pod status..."
WEAVIATE_POD=$(kubectl get pods -n $NAMESPACE -l app=weaviate -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || echo "")

if [ -z "$WEAVIATE_POD" ]; then
    print_error "Weaviate pod not found"
    exit 1
fi

POD_STATUS=$(kubectl get pod "$WEAVIATE_POD" -n $NAMESPACE -o jsonpath='{.status.phase}' 2>/dev/null || echo "Unknown")
if [ "$POD_STATUS" = "Running" ]; then
    print_success "Weaviate pod is running: $WEAVIATE_POD"
else
    print_error "Weaviate pod status: $POD_STATUS"
fi

# Step 2: Check Weaviate health endpoint
print_status "Step 2: Checking Weaviate health endpoint..."
HEALTH_RESPONSE=$(kubectl exec -n $NAMESPACE "$WEAVIATE_POD" -- wget -q -O- 'http://localhost:8080/v1/.well-known/ready' --timeout=5 2>&1 || echo "")
if [ "$HEALTH_RESPONSE" = "ok" ]; then
    print_success "Weaviate health check passed"
else
    print_error "Weaviate health check failed: $HEALTH_RESPONSE"
fi

# Step 3: Test GraphQL query performance
print_status "Step 3: Testing GraphQL query performance..."
QUERY='{"query":"{Get{AgiWiki(limit:10){_additional{id}text timestamp metadata}}}"}'
START_TIME=$(date +%s.%N)
QUERY_RESPONSE=$(kubectl exec -n $NAMESPACE "$WEAVIATE_POD" -- wget -q -O- 'http://localhost:8080/v1/graphql' \
    --post-data "$QUERY" \
    --header='Content-Type: application/json' \
    --timeout=30 2>&1 || echo "TIMEOUT")
END_TIME=$(date +%s.%N)
ELAPSED=$(echo "$END_TIME - $START_TIME" | bc)

if [ "$QUERY_RESPONSE" = "TIMEOUT" ]; then
    print_error "GraphQL query timed out after 30 seconds"
elif echo "$QUERY_RESPONSE" | grep -q "errors"; then
    print_error "GraphQL query returned errors"
    echo "$QUERY_RESPONSE" | head -5
else
    print_success "GraphQL query completed in ${ELAPSED}s"
    RESULT_COUNT=$(echo "$QUERY_RESPONSE" | python3 -c 'import sys, json; data=json.load(sys.stdin); print(len(data.get("data", {}).get("Get", {}).get("AgiWiki", [])))' 2>/dev/null || echo "0")
    if [ "$RESULT_COUNT" -gt 0 ]; then
        print_success "Query returned $RESULT_COUNT results"
    else
        print_warning "Query returned 0 results"
    fi
fi

# Step 4: Check Weaviate metrics
print_status "Step 4: Checking Weaviate resource usage..."
kubectl top pod "$WEAVIATE_POD" -n $NAMESPACE 2>/dev/null || print_warning "Metrics not available (metrics-server may not be installed)"

# Step 5: Check recent Weaviate logs for errors
print_status "Step 5: Checking recent Weaviate logs..."
ERROR_COUNT=$(kubectl logs -n $NAMESPACE "$WEAVIATE_POD" --tail=100 2>&1 | grep -ci "error\|timeout\|failed" || echo "0")
if [ "$ERROR_COUNT" -gt 0 ]; then
    print_warning "Found $ERROR_COUNT error messages in recent logs"
    echo "  Recent errors:"
    kubectl logs -n $NAMESPACE "$WEAVIATE_POD" --tail=100 2>&1 | grep -i "error\|timeout\|failed" | tail -3 | sed 's/^/    /'
else
    print_success "No recent errors in Weaviate logs"
fi

echo ""
echo "=========================================="
echo "Health Check Complete"
echo "=========================================="
echo ""
echo "If queries are timing out, consider:"
echo "  1. Increasing Weaviate resources (CPU/memory)"
echo "  2. Optimizing queries (add filters, reduce limit)"
echo "  3. Checking if Weaviate is overloaded with too many objects"
echo ""

