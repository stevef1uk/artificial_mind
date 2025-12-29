#!/bin/bash

# Test GPU concurrency from within k8s cluster
# This script runs inside a pod and tests the HDN server

set -e

HDN_URL="${HDN_URL:-http://hdn-server-rpi58.agi.svc.cluster.local:8080}"
NUM_REQUESTS="${NUM_REQUESTS:-20}"
CONCURRENT="${CONCURRENT:-10}"

echo "🧪 Testing GPU Concurrency (K8s)"
echo "================================"
echo "HDN URL: $HDN_URL"
echo "Total Requests: $NUM_REQUESTS"
echo "Concurrent: $CONCURRENT"
echo ""

# Function to send a single request
send_request() {
    local id=$1
    local start_time=$(date +%s.%N)
    
    response=$(curl -s -X POST "$HDN_URL/api/v1/intelligent/execute" \
        -H "Content-Type: application/json" \
        -H "X-Request-Source: ui" \
        -d "{
            \"task_name\": \"test_concurrency_$id\",
            \"description\": \"Test concurrent request $id - generate a simple hello world function in Python\",
            \"language\": \"python\",
            \"context\": {},
            \"max_retries\": 1,
            \"timeout\": 30
        }" \
        -w "\n%{http_code}" \
        --max-time 120)
    
    local end_time=$(date +%s.%N)
    local duration=$(echo "$end_time - $start_time" | bc)
    local http_code=$(echo "$response" | tail -1)
    local body=$(echo "$response" | head -n -1)
    
    if [ "$http_code" = "200" ] || [ "$http_code" = "202" ]; then
        echo "✅ Request $id: HTTP $http_code (${duration}s)"
    else
        echo "❌ Request $id: HTTP $http_code (${duration}s)"
    fi
}

# Function to get queue stats
get_queue_stats() {
    curl -s "$HDN_URL/api/v1/llm/queue/stats" 2>/dev/null | jq -r '
        "High=\(.high_priority_queue_size // 0)/\(.max_high_priority_queue // 0), " +
        "Low=\(.low_priority_queue_size // 0)/\(.max_low_priority_queue // 0), " +
        "Workers=\(.active_workers // 0)/\(.max_workers // 0)"
    ' 2>/dev/null || echo "unavailable"
}

# Check if HDN is accessible
echo "🔍 Checking HDN availability..."
if ! curl -s --max-time 5 "$HDN_URL/health" > /dev/null 2>&1; then
    echo "❌ HDN server not accessible at $HDN_URL"
    exit 1
fi
echo "✅ HDN server is accessible"
echo ""

# Get initial queue stats
echo "📊 Initial Queue Stats: $(get_queue_stats)"
echo ""

# Send requests in concurrent batches
echo "🚀 Sending $NUM_REQUESTS requests with $CONCURRENT concurrent..."
echo ""

start_time=$(date +%s)
pids=()
request_id=1
completed=0

# Send requests
while [ $request_id -le $NUM_REQUESTS ]; do
    # Wait if we have too many concurrent
    while [ ${#pids[@]} -ge $CONCURRENT ]; do
        new_pids=()
        for pid in "${pids[@]}"; do
            if kill -0 "$pid" 2>/dev/null; then
                new_pids+=("$pid")
            else
                wait "$pid" 2>/dev/null || true
                completed=$((completed + 1))
            fi
        done
        pids=("${new_pids[@]}")
        if [ ${#pids[@]} -lt $CONCURRENT ]; then
            echo "   Progress: $completed/$NUM_REQUESTS completed | Queue: $(get_queue_stats)"
        fi
        sleep 0.2
    done
    
    # Send request in background
    (send_request $request_id > /dev/null 2>&1) &
    pids+=($!)
    request_id=$((request_id + 1))
done

# Wait for all requests to complete
echo ""
echo "⏳ Waiting for all requests to complete..."
while [ ${#pids[@]} -gt 0 ]; do
    new_pids=()
    for pid in "${pids[@]}"; do
        if kill -0 "$pid" 2>/dev/null; then
            new_pids+=("$pid")
        else
            wait "$pid" 2>/dev/null || true
            completed=$((completed + 1))
        fi
    done
    pids=("${new_pids[@]}")
    if [ ${#pids[@]} -gt 0 ]; then
        echo "   Progress: $completed/$NUM_REQUESTS completed | Queue: $(get_queue_stats)"
        sleep 1
    fi
done

end_time=$(date +%s)
total_duration=$((end_time - start_time))

echo ""
echo "📊 Final Queue Stats: $(get_queue_stats)"
echo ""
echo "✅ Test completed in ${total_duration}s"
echo "   Average: $(echo "scale=2; $total_duration / $NUM_REQUESTS" | bc)s per request"

