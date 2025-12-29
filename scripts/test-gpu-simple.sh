#!/bin/bash

# Simple GPU concurrency test - shows what's actually happening

HDN_URL="${HDN_URL:-http://hdn-server-rpi58.agi.svc.cluster.local:8080}"

echo "🧪 Simple GPU Concurrency Test"
echo "=============================="
echo ""

# Test function
test_concurrency() {
    local num=$1
    echo "Testing $num concurrent requests..."
    
    # Send requests and capture results
    pids=()
    for i in $(seq 1 $num); do
        (
            start=$(date +%s)
            http_code=$(curl -s -o /dev/null -w "%{http_code}" -X POST "$HDN_URL/api/v1/intelligent/execute" \
                -H "Content-Type: application/json" \
                -H "X-Request-Source: ui" \
                -d "{\"task_name\":\"test_$i\",\"description\":\"Simple test $i\",\"language\":\"python\"}" \
                --max-time 60)
            end=$(date +%s)
            duration=$((end - start))
            echo "$i|$http_code|$duration" > /tmp/result_$i.txt
        ) &
        pids+=($!)
    done
    
    # Wait for all
    wait
    
    # Show results
    success=0
    rejected=0
    failed=0
    total_time=0
    
    for i in $(seq 1 $num); do
        if [ -f "/tmp/result_$i.txt" ]; then
            IFS='|' read -r id code time < /tmp/result_$i.txt
            total_time=$((total_time + time))
            
            case $code in
                200|202)
                    echo "  ✅ Request $id: HTTP $code (${time}s)"
                    success=$((success + 1))
                    ;;
                429)
                    echo "  🚫 Request $id: HTTP $code (rejected)"
                    rejected=$((rejected + 1))
                    ;;
                *)
                    echo "  ❌ Request $id: HTTP $code (${time}s)"
                    failed=$((failed + 1))
                    ;;
            esac
            rm -f "/tmp/result_$i.txt"
        fi
    done
    
    avg_time=$((total_time / num))
    echo "  Summary: ✅$success 🚫$rejected ❌$failed | Avg: ${avg_time}s"
    
    # Check queue
    sleep 2
    queue=$(curl -s "$HDN_URL/api/v1/llm/queue/stats" 2>/dev/null | jq -r '.high_priority_queue_size // 0' 2>/dev/null || echo "?")
    workers=$(curl -s "$HDN_URL/api/v1/llm/queue/stats" 2>/dev/null | jq -r '.active_workers // 0' 2>/dev/null || echo "?")
    echo "  Queue: $queue pending, $workers workers active"
    echo ""
}

# Run tests
for concurrent in 2 4 6 8; do
    test_concurrency $concurrent
    sleep 5
done

echo "✅ Test complete"

