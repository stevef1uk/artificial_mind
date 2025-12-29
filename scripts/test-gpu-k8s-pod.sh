#!/bin/bash
# Run this script inside a k8s pod with curl and jq

HDN_URL="${HDN_URL:-http://hdn-server-rpi58.agi.svc.cluster.local:8080}"

echo "🧪 Testing GPU Concurrency"
echo "HDN URL: $HDN_URL"
echo ""

# Test with 4 concurrent requests
echo "Sending 4 concurrent requests..."
start_time=$(date +%s)

for i in 1 2 3 4; do
  (
    req_start=$(date +%s)
    http_code=$(curl -s -o /dev/null -w "%{http_code}" \
      -X POST "$HDN_URL/api/v1/intelligent/execute" \
      -H "Content-Type: application/json" \
      -H "X-Request-Source: ui" \
      -d "{\"task_name\":\"test_$i\",\"description\":\"Generate a simple Python function\",\"language\":\"python\"}" \
      --max-time 120)
    req_end=$(date +%s)
    duration=$((req_end - req_start))
    echo "Request $i: HTTP $http_code (${duration}s)"
  ) &
done

wait
end_time=$(date +%s)
total_time=$((end_time - start_time))

echo ""
echo "Total time: ${total_time}s"
echo ""
echo "Queue stats:"
curl -s "$HDN_URL/api/v1/llm/queue/stats" | jq '{
  high_queue: .high_priority_queue_size,
  high_max: .max_high_priority_queue,
  low_queue: .low_priority_queue_size,
  low_max: .max_low_priority_queue,
  workers: "\(.active_workers)/\(.max_workers)"
}'

