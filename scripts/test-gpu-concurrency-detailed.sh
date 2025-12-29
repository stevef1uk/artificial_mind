#!/bin/bash

# Detailed GPU concurrency test with full request/response tracking

HDN_URL="${HDN_URL:-http://hdn-server-rpi58.agi.svc.cluster.local:8080}"

echo "🧪 Detailed GPU Concurrency Test"
echo "================================"
echo "HDN URL: $HDN_URL"
echo ""

# Function to get queue stats
get_queue_stats() {
    curl -s "$HDN_URL/api/v1/llm/queue/stats" 2>/dev/null | jq -r '
        if . then
            "High: \(.high_priority_queue_size // 0)/\(.max_high_priority_queue // 0) (\(.high_priority_percent // 0)%), " +
            "Low: \(.low_priority_queue_size // 0)/\(.max_low_priority_queue // 0) (\(.low_priority_percent // 0)%), " +
            "Workers: \(.active_workers // 0)/\(.max_workers // 0)"
        else
            "unavailable"
        end
    ' 2>/dev/null || echo "error"
}

# Check HDN accessibility
echo "🔍 Checking HDN..."
if ! curl -s --max-time 5 "$HDN_URL/health" > /dev/null 2>&1; then
    echo "❌ HDN not accessible"
    exit 1
fi
echo "✅ HDN accessible"
echo ""

# Test different concurrency levels
for concurrent in 2 4 6 8 10 12; do
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "📊 Testing $concurrent concurrent requests"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    
    # Initial queue state
    echo "Initial: $(get_queue_stats)"
    
    # Send requests
    pids=()
    results=()
    start_time=$(date +%s.%N)
    
    for i in $(seq 1 $concurrent); do
        (
            req_start=$(date +%s.%N)
            response=$(curl -s -w "\n%{http_code}" -X POST "$HDN_URL/api/v1/intelligent/execute" \
                -H "Content-Type: application/json" \
                -H "X-Request-Source: ui" \
                -d "{
                    \"task_name\": \"test_${concurrent}_$i\",
                    \"description\": \"Generate a simple Python function that adds two numbers\",
                    \"language\": \"python\",
                    \"context\": {},
                    \"max_retries\": 1,
                    \"timeout\": 30
                }" \
                --max-time 120 2>&1)
            
            req_end=$(date +%s.%N)
            duration=$(echo "$req_end - $req_start" | bc)
            http_code=$(echo "$response" | tail -1)
            body=$(echo "$response" | head -n -1)
            
            echo "$i|$http_code|$duration|$body" > /tmp/req_result_$i.txt
        ) &
        pids+=($!)
    done
    
    # Monitor queue during execution
    echo ""
    echo "Monitoring queue (checking every 2s)..."
    monitor_pid=$!
    (
        for check in {1..15}; do
            sleep 2
            stats=$(get_queue_stats)
            echo "  [$check] $stats"
        done
    ) &
    monitor_pid=$!
    
    # Wait for all requests
    success=0
    failed=0
    rejected=0
    total_duration=0
    
    for pid in "${pids[@]}"; do
        wait "$pid" 2>/dev/null
    done
    
    kill $monitor_pid 2>/dev/null || true
    
    # Collect results
    echo ""
    echo "Request Results:"
    for i in $(seq 1 $concurrent); do
        if [ -f "/tmp/req_result_$i.txt" ]; then
            IFS='|' read -r id code duration body < /tmp/req_result_$i.txt
            total_duration=$(echo "$total_duration + $duration" | bc)
            
            if [ "$code" = "200" ] || [ "$code" = "202" ]; then
                echo "  ✅ Request $id: HTTP $code (${duration}s)"
                success=$((success + 1))
            elif [ "$code" = "429" ]; then
                echo "  🚫 Request $id: HTTP $code (rejected - backpressure)"
                rejected=$((rejected + 1))
            else
                echo "  ❌ Request $id: HTTP $code (${duration}s)"
                failed=$((failed + 1))
            fi
            rm -f "/tmp/req_result_$i.txt"
        fi
    done
    
    end_time=$(date +%s.%N)
    total_time=$(echo "$end_time - $start_time" | bc)
    avg_duration=$(echo "scale=2; $total_duration / $concurrent" | bc)
    
    echo ""
    echo "Summary:"
    echo "  ✅ Success: $success/$concurrent"
    echo "  🚫 Rejected: $rejected/$concurrent"
    echo "  ❌ Failed: $failed/$concurrent"
    echo "  ⏱️  Total time: ${total_time}s"
    echo "  ⏱️  Avg per request: ${avg_duration}s"
    echo "  📊 Final queue: $(get_queue_stats)"
    echo ""
    
    # Wait for queue to clear before next test
    echo "Waiting for queue to clear..."
    for i in {1..10}; do
        sleep 2
        stats=$(get_queue_stats)
        high_size=$(echo "$stats" | grep -oP 'High: \K\d+' || echo "0")
        if [ "$high_size" = "0" ]; then
            echo "  Queue cleared after $((i * 2))s"
            break
        fi
    done
    echo ""
    sleep 3
done

echo "✅ All tests completed"

