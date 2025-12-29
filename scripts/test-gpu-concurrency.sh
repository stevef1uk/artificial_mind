#!/bin/bash

# Test GPU concurrent request handling
# This script sends multiple concurrent requests and monitors queue behavior

set -e

HDN_URL="${HDN_URL:-http://localhost:8080}"
NUM_REQUESTS="${NUM_REQUESTS:-10}"
CONCURRENT="${CONCURRENT:-5}"

echo "🧪 Testing GPU Concurrency"
echo "=========================="
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
            \"description\": \"Test concurrent request $id - generate a simple hello world function\",
            \"language\": \"python\",
            \"context\": {},
            \"max_retries\": 1,
            \"timeout\": 30
        }" \
        -w "\n%{http_code}" \
        --max-time 60)
    
    local end_time=$(date +%s.%N)
    local duration=$(echo "$end_time - $start_time" | bc)
    local http_code=$(echo "$response" | tail -1)
    local body=$(echo "$response" | head -n -1)
    
    if [ "$http_code" = "200" ] || [ "$http_code" = "202" ]; then
        echo "✅ Request $id: HTTP $http_code (${duration}s)"
        if echo "$body" | grep -q "workflow_id"; then
            local wf_id=$(echo "$body" | jq -r '.workflow_id // empty' 2>/dev/null || echo "")
            echo "   Workflow ID: $wf_id"
        fi
    else
        echo "❌ Request $id: HTTP $http_code (${duration}s)"
        echo "$body" | head -3
    fi
}

# Function to get queue stats
get_queue_stats() {
    curl -s "$HDN_URL/api/v1/llm/queue/stats" 2>/dev/null | jq -r '
        "Queue: High=\(.high_priority_queue_size // 0)/\(.max_high_priority_queue // 0) (\(.high_priority_percent // 0)%), " +
        "Low=\(.low_priority_queue_size // 0)/\(.max_low_priority_queue // 0) (\(.low_priority_percent // 0)%), " +
        "Workers=\(.active_workers // 0)/\(.max_workers // 0)"
    ' 2>/dev/null || echo "Queue stats unavailable"
}

# Check if HDN is accessible
echo "🔍 Checking HDN availability..."
if ! curl -s --max-time 5 "$HDN_URL/health" > /dev/null 2>&1; then
    echo "❌ HDN server not accessible at $HDN_URL"
    echo "   Set HDN_URL environment variable if different"
    exit 1
fi
echo "✅ HDN server is accessible"
echo ""

# Get initial queue stats
echo "📊 Initial Queue Stats:"
get_queue_stats
echo ""

# Send requests in batches
echo "🚀 Sending $NUM_REQUESTS requests with $CONCURRENT concurrent..."
echo ""

start_time=$(date +%s)
pids=()
request_id=1

# Send requests in concurrent batches
while [ $request_id -le $NUM_REQUESTS ]; do
    # Wait if we have too many concurrent
    while [ ${#pids[@]} -ge $CONCURRENT ]; do
        # Check which PIDs are still running
        new_pids=()
        for pid in "${pids[@]}"; do
            if kill -0 "$pid" 2>/dev/null; then
                new_pids+=("$pid")
            else
                wait "$pid" 2>/dev/null || true
            fi
        done
        pids=("${new_pids[@]}")
        sleep 0.1
    done
    
    # Send request in background
    (send_request $request_id) &
    pids+=($!)
    request_id=$((request_id + 1))
    
    # Show queue stats every few requests
    if [ $((request_id % 3)) -eq 0 ]; then
        echo "   📊 Queue: $(get_queue_stats)"
    fi
done

# Wait for all requests to complete
echo ""
echo "⏳ Waiting for all requests to complete..."
for pid in "${pids[@]}"; do
    wait "$pid" 2>/dev/null || true
done

end_time=$(date +%s)
total_duration=$((end_time - start_time))

echo ""
echo "📊 Final Queue Stats:"
get_queue_stats
echo ""

echo "✅ Test completed in ${total_duration}s"
echo ""
echo "💡 Analysis:"
echo "   - If queue stayed at 0/0: GPU can handle all requests immediately"
echo "   - If queue filled up: GPU is saturated, requests are queuing"
echo "   - Check logs for actual processing times and worker utilization"

