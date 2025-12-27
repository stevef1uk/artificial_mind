#!/bin/bash

# Test GPU concurrency with automatic port-forward setup

set -e

PORT=8080
PF_PID_FILE="/tmp/hdn-portforward.pid"

# Cleanup function
cleanup() {
    if [ -f "$PF_PID_FILE" ]; then
        PF_PID=$(cat "$PF_PID_FILE")
        if kill -0 "$PF_PID" 2>/dev/null; then
            echo ""
            echo "🛑 Stopping port-forward (PID: $PF_PID)..."
            kill "$PF_PID" 2>/dev/null || true
        fi
        rm -f "$PF_PID_FILE"
    fi
}

trap cleanup EXIT INT TERM

# Check if port-forward already exists
if [ -f "$PF_PID_FILE" ]; then
    PF_PID=$(cat "$PF_PID_FILE")
    if kill -0 "$PF_PID" 2>/dev/null; then
        echo "✅ Port-forward already running (PID: $PF_PID)"
    else
        rm -f "$PF_PID_FILE"
    fi
fi

# Start port-forward if needed
if [ ! -f "$PF_PID_FILE" ] || ! kill -0 "$(cat "$PF_PID_FILE")" 2>/dev/null; then
    echo "🔌 Starting port-forward..."
    kubectl port-forward -n agi svc/hdn-server-rpi58 $PORT:8080 > /dev/null 2>&1 &
    PF_PID=$!
    echo "$PF_PID" > "$PF_PID_FILE"
    sleep 2
    
    if ! kill -0 "$PF_PID" 2>/dev/null; then
        echo "❌ Port-forward failed to start"
        exit 1
    fi
    echo "✅ Port-forward started (PID: $PF_PID)"
fi

HDN_URL="http://localhost:$PORT"

# Check connectivity
echo ""
echo "🔍 Testing connectivity..."
if ! curl -s --max-time 5 "$HDN_URL/health" > /dev/null 2>&1; then
    echo "❌ Cannot reach HDN. Waiting a bit longer..."
    sleep 3
    if ! curl -s --max-time 5 "$HDN_URL/health" > /dev/null 2>&1; then
        echo "❌ Still cannot reach HDN. Port-forward may have failed."
        exit 1
    fi
fi
echo "✅ HDN is accessible at $HDN_URL"
echo ""

# Run the actual test
echo "🧪 Testing GPU Concurrency"
echo "=========================="
echo ""

# Test with different concurrency levels
for concurrent in 2 4 6 8; do
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "📊 Testing $concurrent concurrent requests"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    
    # Initial stats
    echo "Initial queue:"
    curl -s "$HDN_URL/api/v1/llm/queue/stats" | jq -r '
        "High: \(.high_priority_queue_size // 0)/\(.max_high_priority_queue // 0), " +
        "Workers: \(.active_workers // 0)/\(.max_workers // 0)"
    ' 2>/dev/null || echo "unavailable"
    echo ""
    
    # Send requests
    pids=()
    start_time=$(date +%s)
    
    for i in $(seq 1 $concurrent); do
        (
            req_start=$(date +%s)
            http_code=$(curl -s -o /dev/null -w "%{http_code}" \
                -X POST "$HDN_URL/api/v1/intelligent/execute" \
                -H "Content-Type: application/json" \
                -H "X-Request-Source: ui" \
                -d "{\"task_name\":\"test_${concurrent}_$i\",\"description\":\"Generate a simple Python function that adds two numbers\",\"language\":\"python\"}" \
                --max-time 120)
            req_end=$(date +%s)
            duration=$((req_end - req_start))
            echo "$i|$http_code|$duration" > /tmp/gpu_test_$i.txt
        ) &
        pids+=($!)
    done
    
    # Monitor queue (runs until killed)
    (
        check=1
        while true; do
            sleep 2
            stats=$(curl -s "$HDN_URL/api/v1/llm/queue/stats" 2>/dev/null | jq -r '
                "High: \(.high_priority_queue_size // 0), Workers: \(.active_workers // 0)/\(.max_workers // 0)"
            ' 2>/dev/null || echo "unavailable")
            echo "  [$check] $stats"
            check=$((check + 1))
        done
    ) &
    monitor_pid=$!
    
    # Wait for requests with timeout
    timeout=180  # 3 minutes max
    elapsed=0
    while [ ${#pids[@]} -gt 0 ] && [ $elapsed -lt $timeout ]; do
        new_pids=()
        for pid in "${pids[@]}"; do
            if kill -0 "$pid" 2>/dev/null; then
                new_pids+=("$pid")
            else
                wait "$pid" 2>/dev/null || true
            fi
        done
        pids=("${new_pids[@]}")
        if [ ${#pids[@]} -gt 0 ]; then
            sleep 2
            elapsed=$((elapsed + 2))
            if [ $((elapsed % 10)) -eq 0 ]; then
                echo "  Still waiting... (${elapsed}s elapsed, ${#pids[@]} requests pending)"
            fi
        fi
    done
    
    # Kill any remaining processes
    for pid in "${pids[@]}"; do
        kill "$pid" 2>/dev/null || true
        wait "$pid" 2>/dev/null || true
    done
    
    kill $monitor_pid 2>/dev/null || true
    
    # Show results
    success=0
    rejected=0
    failed=0
    
    echo ""
    echo "Results:"
    for i in $(seq 1 $concurrent); do
        if [ -f "/tmp/gpu_test_$i.txt" ]; then
            IFS='|' read -r id code duration < /tmp/gpu_test_$i.txt
            case $code in
                200|202)
                    echo "  ✅ Request $id: HTTP $code (${duration}s)"
                    success=$((success + 1))
                    ;;
                429)
                    echo "  🚫 Request $id: HTTP $code (rejected)"
                    rejected=$((rejected + 1))
                    ;;
                *)
                    echo "  ❌ Request $id: HTTP $code (${duration}s)"
                    failed=$((failed + 1))
                    ;;
            esac
            rm -f "/tmp/gpu_test_$i.txt"
        fi
    done
    
    end_time=$(date +%s)
    total_time=$((end_time - start_time))
    
    echo ""
    echo "Summary: ✅$success 🚫$rejected ❌$failed | Total: ${total_time}s"
    
    # Final stats
    echo ""
    echo "Final queue:"
    curl -s "$HDN_URL/api/v1/llm/queue/stats" | jq -r '
        "High: \(.high_priority_queue_size // 0)/\(.max_high_priority_queue // 0), " +
        "Workers: \(.active_workers // 0)/\(.max_workers // 0)"
    ' 2>/dev/null || echo "unavailable"
    echo ""
    
    # Wait for queue to clear
    echo "Waiting for queue to clear..."
    for i in {1..10}; do
        sleep 2
        high_size=$(curl -s "$HDN_URL/api/v1/llm/queue/stats" 2>/dev/null | jq -r '.high_priority_queue_size // 0' 2>/dev/null || echo "?")
        if [ "$high_size" = "0" ]; then
            echo "  Queue cleared after $((i * 2))s"
            break
        fi
    done
    echo ""
    sleep 3
done

echo "✅ All tests completed"
echo ""
echo "💡 Analysis:"
echo "   - If queue stayed at 0: GPU can handle all requests immediately"
echo "   - If queue filled: GPU is saturated, requests are queuing"
echo "   - Check worker utilization to see actual GPU capacity"

