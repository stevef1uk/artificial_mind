#!/bin/bash

# Test GPU concurrency from outside the cluster
# This script uses port-forward or NodePort to access HDN

set -e

# Try to detect HDN URL
if [ -n "$HDN_URL" ]; then
    HDN_URL="$HDN_URL"
elif kubectl get svc -n agi hdn-server-rpi58 -o jsonpath='{.spec.type}' 2>/dev/null | grep -q NodePort; then
    NODE_IP=$(kubectl get nodes -o jsonpath='{.items[0].status.addresses[?(@.type=="InternalIP")].address}' 2>/dev/null | head -1)
    NODE_PORT=$(kubectl get svc -n agi hdn-server-rpi58 -o jsonpath='{.spec.ports[0].nodePort}' 2>/dev/null)
    if [ -n "$NODE_IP" ] && [ -n "$NODE_PORT" ]; then
        HDN_URL="http://${NODE_IP}:${NODE_PORT}"
        echo "Using NodePort: $HDN_URL"
    else
        echo "❌ Could not determine NodePort. Please set HDN_URL or use port-forward"
        exit 1
    fi
else
    echo "⚠️  No NodePort found. Starting port-forward..."
    echo "   Run this in another terminal: kubectl port-forward -n agi svc/hdn-server-rpi58 8080:8080"
    HDN_URL="http://localhost:8080"
    echo "   Using: $HDN_URL"
    sleep 2
fi

echo ""
echo "🧪 Testing GPU Concurrency"
echo "=========================="
echo "HDN URL: $HDN_URL"
echo ""

# Check connectivity
echo "🔍 Checking connectivity..."
if ! curl -s --max-time 5 "$HDN_URL/health" > /dev/null 2>&1; then
    echo "❌ Cannot reach HDN at $HDN_URL"
    echo ""
    echo "Options:"
    echo "  1. Set HDN_URL environment variable"
    echo "  2. Use port-forward: kubectl port-forward -n agi svc/hdn-server-rpi58 8080:8080"
    echo "  3. Run test from inside cluster (use test-gpu-k8s-pod.sh)"
    exit 1
fi
echo "✅ HDN is accessible"
echo ""

# Get initial queue stats
echo "📊 Initial Queue Stats:"
curl -s "$HDN_URL/api/v1/llm/queue/stats" | jq -r '
    "High: \(.high_priority_queue_size // 0)/\(.max_high_priority_queue // 0) (\(.high_priority_percent // 0)%), " +
    "Low: \(.low_priority_queue_size // 0)/\(.max_low_priority_queue // 0) (\(.low_priority_percent // 0)%), " +
    "Workers: \(.active_workers // 0)/\(.max_workers // 0)"
' 2>/dev/null || echo "Could not get queue stats"
echo ""

# Test function
test_concurrency() {
    local num=$1
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "📊 Testing $num concurrent requests"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    
    pids=()
    start_time=$(date +%s)
    
    # Send requests
    for i in $(seq 1 $num); do
        (
            req_start=$(date +%s)
            http_code=$(curl -s -o /dev/null -w "%{http_code}" \
                -X POST "$HDN_URL/api/v1/intelligent/execute" \
                -H "Content-Type: application/json" \
                -H "X-Request-Source: ui" \
                -d "{\"task_name\":\"test_${num}_$i\",\"description\":\"Generate a simple Python function that adds two numbers\",\"language\":\"python\"}" \
                --max-time 120)
            req_end=$(date +%s)
            duration=$((req_end - req_start))
            echo "$i|$http_code|$duration" > /tmp/gpu_test_$i.txt
        ) &
        pids+=($!)
    done
    
    # Monitor queue during execution
    (
        for check in {1..10}; do
            sleep 2
            stats=$(curl -s "$HDN_URL/api/v1/llm/queue/stats" 2>/dev/null | jq -r '
                "High: \(.high_priority_queue_size // 0), Workers: \(.active_workers // 0)/\(.max_workers // 0)"
            ' 2>/dev/null || echo "unavailable")
            echo "  [$check] $stats"
        done
    ) &
    monitor_pid=$!
    
    # Wait for requests
    wait
    
    kill $monitor_pid 2>/dev/null || true
    
    # Collect results
    success=0
    rejected=0
    failed=0
    
    echo ""
    echo "Results:"
    for i in $(seq 1 $num); do
        if [ -f "/tmp/gpu_test_$i.txt" ]; then
            IFS='|' read -r id code duration < /tmp/gpu_test_$i.txt
            case $code in
                200|202)
                    echo "  ✅ Request $id: HTTP $code (${duration}s)"
                    success=$((success + 1))
                    ;;
                429)
                    echo "  🚫 Request $id: HTTP $code (rejected - backpressure)"
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
    
    # Final queue stats
    echo ""
    echo "Final Queue Stats:"
    curl -s "$HDN_URL/api/v1/llm/queue/stats" | jq -r '
        "High: \(.high_priority_queue_size // 0)/\(.max_high_priority_queue // 0), " +
        "Low: \(.low_priority_queue_size // 0)/\(.max_low_priority_queue // 0), " +
        "Workers: \(.active_workers // 0)/\(.max_workers // 0)"
    ' 2>/dev/null || echo "unavailable"
    echo ""
}

# Run tests
for concurrent in 2 4 6 8; do
    test_concurrency $concurrent
    echo "Waiting for queue to clear..."
    sleep 10
done

echo "✅ All tests completed"

