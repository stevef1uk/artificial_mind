#!/bin/bash

echo "🧪 Testing Retry Logic in HDN System"
echo "===================================="
echo

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

print_step() {
    echo -e "${BLUE}📋 $1${NC}"
}

print_success() {
    echo -e "${GREEN}✅ $1${NC}"
}

print_info() {
    echo -e "${YELLOW}ℹ️  $1${NC}"
}

print_error() {
    echo -e "${RED}❌ $1${NC}"
}

# Step 1: Check if Redis has any retry count keys
print_step "1. Checking for existing retry count keys in Redis"
retry_keys=$(docker exec agi-redis redis-cli KEYS "workflow_step_retry:*" | wc -l)
echo "Found $retry_keys retry count keys in Redis"

# Step 2: Send a request that might trigger retries
print_step "2. Sending a request that might trigger retry logic"
echo "Sending request: calculate fibonacci with invalid parameters"

response=$(curl -s -X POST http://localhost:8081/api/v1/intelligent/execute \
    -H "Content-Type: application/json" \
    -d '{
        "task_name": "fibonacci_calculator",
        "description": "Calculate fibonacci sequence with invalid input that will cause validation failures",
        "context": {
            "n": "invalid_input",
            "max_retries": "2"
        },
        "language": "python",
        "force_regenerate": true
    }')

echo "Response received:"
echo "$response" | jq . 2>/dev/null || echo "$response"

# Step 3: Wait a moment for processing
print_step "3. Waiting for processing..."
sleep 5

# Step 4: Check for retry count keys again
print_step "4. Checking for retry count keys after request"
retry_keys_after=$(docker exec agi-redis redis-cli KEYS "workflow_step_retry:*" | wc -l)
echo "Found $retry_keys_after retry count keys in Redis"

if [ "$retry_keys_after" -gt "$retry_keys" ]; then
    print_success "Retry count keys were created! Retry logic is working."
    
    # Show the actual retry keys
    echo "Retry count keys:"
    docker exec agi-redis redis-cli KEYS "workflow_step_retry:*" | head -5
    
    # Show retry count values
    echo "Retry count values:"
    for key in $(docker exec agi-redis redis-cli KEYS "workflow_step_retry:*" | head -3); do
        value=$(docker exec agi-redis redis-cli GET "$key")
        echo "  $key = $value"
    done
else
    print_info "No new retry count keys found. This could mean:"
    echo "  - The request succeeded without retries"
    echo "  - The request failed before reaching the orchestrator"
    echo "  - The retry logic hasn't been triggered yet"
fi

# Step 5: Check server logs for retry messages
print_step "5. Checking server logs for retry messages"
retry_logs=$(tail -50 /tmp/hdn_server.log | grep -i "retry" | wc -l)
echo "Found $retry_logs retry-related log messages in last 50 lines"

if [ "$retry_logs" -gt 0 ]; then
    print_success "Retry messages found in logs!"
    echo "Recent retry logs:"
    tail -50 /tmp/hdn_server.log | grep -i "retry" | tail -3
else
    print_info "No retry messages in recent logs"
fi

# Step 6: Test with a request that should definitely fail
print_step "6. Testing with a request that should definitely fail and retry"
echo "Sending request with dangerous code pattern..."

response2=$(curl -s -X POST http://localhost:8081/api/v1/intelligent/execute \
    -H "Content-Type: application/json" \
    -d '{
        "task_name": "dangerous_test",
        "description": "Generate code that will fail validation due to dangerous patterns",
        "context": {
            "pattern": "exec",
            "max_retries": "2"
        },
        "language": "python",
        "force_regenerate": true
    }')

echo "Response received:"
echo "$response2" | jq . 2>/dev/null || echo "$response2"

# Step 7: Final check
print_step "7. Final retry count check"
sleep 3
final_retry_keys=$(docker exec agi-redis redis-cli KEYS "workflow_step_retry:*" | wc -l)
echo "Final retry count keys: $final_retry_keys"

if [ "$final_retry_keys" -gt 0 ]; then
    print_success "Retry logic is working! Found $final_retry_keys retry count keys in Redis"
else
    print_info "No retry count keys found. The system may be working without retries, or the retry logic needs more testing."
fi

echo
echo "Test completed!"
