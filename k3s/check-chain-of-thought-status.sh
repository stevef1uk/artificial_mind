#!/bin/bash

# Check chain of thought system status and diagnose timeouts

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
echo "Chain of Thought System Status"
echo "=========================================="
echo ""

# Step 1: Check HDN server status
print_status "Step 1: Checking HDN server status..."
HDN_POD=$(kubectl get pods -n $NAMESPACE -l app=hdn-server-rpi58 -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || echo "")

if [ -z "$HDN_POD" ]; then
    print_error "HDN server pod not found"
    exit 1
fi

HDN_STATUS=$(kubectl get pod -n $NAMESPACE "$HDN_POD" -o jsonpath='{.status.phase}' 2>/dev/null || echo "Unknown")
if [ "$HDN_STATUS" = "Running" ]; then
    print_success "HDN server is running: $HDN_POD"
else
    print_error "HDN server status: $HDN_STATUS"
fi

# Step 2: Check Monitor UI status
print_status "Step 2: Checking Monitor UI status..."
MONITOR_POD=$(kubectl get pods -n $NAMESPACE -l app=monitor-ui -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || echo "")

if [ -z "$MONITOR_POD" ]; then
    print_error "Monitor UI pod not found"
else
    MONITOR_STATUS=$(kubectl get pod -n $NAMESPACE "$MONITOR_POD" -o jsonpath='{.status.phase}' 2>/dev/null || echo "Unknown")
    if [ "$MONITOR_STATUS" = "Running" ]; then
        print_success "Monitor UI is running: $MONITOR_POD"
    else
        print_error "Monitor UI status: $MONITOR_STATUS"
    fi
fi

# Step 3: Check LLM server status
print_status "Step 3: Checking LLM server status..."
LLM_POD=$(kubectl get pods -n $NAMESPACE -l app=llama-server -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || echo "")

if [ -z "$LLM_POD" ]; then
    print_warning "LLM server pod not found (may be named differently)"
else
    LLM_STATUS=$(kubectl get pod -n $NAMESPACE "$LLM_POD" -o jsonpath='{.status.phase}' 2>/dev/null || echo "Unknown")
    if [ "$LLM_STATUS" = "Running" ]; then
        print_success "LLM server is running: $LLM_POD"
    else
        print_error "LLM server status: $LLM_STATUS"
    fi
fi

# Step 4: Check recent HDN logs for chat/timeout errors
print_status "Step 4: Checking recent HDN logs for chat activity..."
RECENT_CHAT=$(kubectl logs -n $NAMESPACE "$HDN_POD" --tail=100 2>&1 | grep -ci "chat\|conversational\|timeout" || echo "0")
if [ "$RECENT_CHAT" -gt 0 ]; then
    print_status "Found $RECENT_CHAT recent chat-related log entries"
    echo "  Recent chat logs:"
    kubectl logs -n $NAMESPACE "$HDN_POD" --tail=100 2>&1 | grep -i "chat\|conversational\|timeout" | tail -5 | sed 's/^/    /'
else
    print_warning "No recent chat activity in logs"
fi

# Step 5: Check for LLM queue backlog
print_status "Step 5: Checking LLM queue status..."
REDIS_POD=$(kubectl get pods -n $NAMESPACE -l app=redis -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || echo "")

if [ -n "$REDIS_POD" ]; then
    # Check async LLM queue sizes
    HIGH_QUEUE=$(kubectl exec -n $NAMESPACE "$REDIS_POD" -- redis-cli LLEN "async_llm:high" 2>/dev/null || echo "0")
    LOW_QUEUE=$(kubectl exec -n $NAMESPACE "$REDIS_POD" -- redis-cli LLEN "async_llm:low" 2>/dev/null || echo "0")
    
    if [ "$HIGH_QUEUE" -gt 0 ] || [ "$LOW_QUEUE" -gt 0 ]; then
        print_warning "LLM queue has pending requests:"
        echo "  High priority queue: $HIGH_QUEUE requests"
        echo "  Low priority queue: $LOW_QUEUE requests"
        if [ "$HIGH_QUEUE" -gt 5 ] || [ "$LOW_QUEUE" -gt 10 ]; then
            print_error "Queue backlog is high - this may cause timeouts"
        fi
    else
        print_success "LLM queue is empty"
    fi
    
    # Check for active LLM requests
    ACTIVE_REQUESTS=$(kubectl exec -n $NAMESPACE "$REDIS_POD" -- redis-cli KEYS "async_llm:request:*" 2>/dev/null | wc -l || echo "0")
    if [ "$ACTIVE_REQUESTS" -gt 0 ]; then
        print_status "Active LLM requests: $ACTIVE_REQUESTS"
    fi
else
    print_warning "Redis pod not found, cannot check queue status"
fi

# Step 6: Check HDN logs for timeout errors
print_status "Step 6: Checking for timeout errors in HDN logs..."
TIMEOUT_ERRORS=$(kubectl logs -n $NAMESPACE "$HDN_POD" --tail=200 2>&1 | grep -ci "timeout\|deadline exceeded\|Request timed out" || echo "0")
if [ "$TIMEOUT_ERRORS" -gt 0 ]; then
    print_warning "Found $TIMEOUT_ERRORS timeout-related errors in recent logs"
    echo "  Recent timeout errors:"
    kubectl logs -n $NAMESPACE "$HDN_POD" --tail=200 2>&1 | grep -i "timeout\|deadline exceeded\|Request timed out" | tail -3 | sed 's/^/    /'
else
    print_success "No recent timeout errors"
fi

# Step 7: Check LLM concurrent request limit
print_status "Step 7: Checking LLM configuration..."
if [ -n "$HDN_POD" ]; then
    LLM_MAX_CONCURRENT=$(kubectl exec -n $NAMESPACE "$HDN_POD" -- env 2>/dev/null | grep "LLM_MAX_CONCURRENT_REQUESTS" || echo "")
    if [ -n "$LLM_MAX_CONCURRENT" ]; then
        print_status "LLM Max Concurrent: $LLM_MAX_CONCURRENT"
    else
        print_status "LLM Max Concurrent: 2 (default)"
    fi
fi

# Step 8: Test chat endpoint accessibility
print_status "Step 8: Testing chat endpoint..."
if [ -n "$HDN_POD" ]; then
    # Test if endpoint is accessible (just check if it responds, don't send full request)
    ENDPOINT_TEST=$(kubectl exec -n $NAMESPACE "$HDN_POD" -- wget -q -O- --timeout=5 'http://localhost:8080/api/v1/chat/sessions' 2>&1 || echo "FAILED")
    if [ "$ENDPOINT_TEST" != "FAILED" ]; then
        print_success "Chat endpoint is accessible"
    else
        print_error "Chat endpoint test failed"
    fi
fi

echo ""
echo "=========================================="
echo "Diagnostic Complete"
echo "=========================================="
echo ""
echo "Chain of Thought Configuration:"
echo "  - Timeout: 3 minutes (180 seconds)"
echo "  - Max Concurrent LLM: 2 (default)"
echo ""
echo "If timeouts persist:"
echo "  1. Check if LLM queue is backed up (see above)"
echo "  2. Check if background FSM tasks are competing for LLM slots"
echo "  3. Try disabling background LLM: DISABLE_BACKGROUND_LLM=1"
echo "  4. Consider increasing timeout or LLM_MAX_CONCURRENT_REQUESTS"
echo ""

