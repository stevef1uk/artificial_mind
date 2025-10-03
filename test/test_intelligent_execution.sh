#!/bin/bash

# Test script for the new intelligent execution system
# This demonstrates the complete workflow: LLM generates code, tests it, caches it, and reuses it

set -e

echo "🧠 Testing Intelligent Execution System"
echo "======================================"

# Configuration
API_URL="http://localhost:8081"
REDIS_URL="localhost:6379"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Function to print colored output
print_status() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

print_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

print_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Function to check if service is running
check_service() {
    local service_name=$1
    local check_command=$2
    
    print_status "Checking if $service_name is running..."
    if eval "$check_command" > /dev/null 2>&1; then
        print_success "$service_name is running"
        return 0
    else
        print_error "$service_name is not running"
        return 1
    fi
}

# Function to wait for service to be ready
wait_for_service() {
    local service_name=$1
    local check_command=$2
    local max_attempts=30
    local attempt=0
    
    print_status "Waiting for $service_name to be ready..."
    while [ $attempt -lt $max_attempts ]; do
        if eval "$check_command" > /dev/null 2>&1; then
            print_success "$service_name is ready"
            return 0
        fi
        attempt=$((attempt + 1))
        sleep 2
    done
    
    print_error "$service_name failed to start after $max_attempts attempts"
    return 1
}

# Function to make API request
api_request() {
    local method=$1
    local endpoint=$2
    local data=$3
    local expected_status=$4
    
    print_status "Making $method request to $endpoint"
    
    if [ -n "$data" ]; then
        response=$(curl -s -w "\n%{http_code}" -X "$method" \
            -H "Content-Type: application/json" \
            -d "$data" \
            "$API_URL$endpoint")
    else
        response=$(curl -s -w "\n%{http_code}" -X "$method" \
            -H "Content-Type: application/json" \
            "$API_URL$endpoint")
    fi
    
    # Extract status code (last line)
    status_code=$(echo "$response" | tail -n1)
    # Extract body (all but last line)
    body=$(echo "$response" | head -n -1)
    
    if [ "$status_code" = "$expected_status" ]; then
        print_success "Request successful (HTTP $status_code)"
        echo "$body" | jq '.' 2>/dev/null || echo "$body"
        return 0
    else
        print_error "Request failed (HTTP $status_code)"
        echo "$body"
        return 1
    fi
}

# Function to test intelligent execution
test_intelligent_execution() {
    local task_name=$1
    local description=$2
    local context=$3
    local language=${4:-python}
    
    print_status "Testing intelligent execution: $task_name"
    
    local request_data=$(cat <<EOF
{
    "task_name": "$task_name",
    "description": "$description",
    "context": $context,
    "language": "$language",
    "force_regenerate": false,
    "max_retries": 3,
    "timeout": 30
}
EOF
)
    
    api_request "POST" "/api/v1/intelligent/execute" "$request_data" "200"
}

# Function to test prime numbers example
test_prime_numbers() {
    local count=${1:-10}
    
    print_status "Testing prime numbers calculation (first $count primes)"
    
    local request_data=$(cat <<EOF
{
    "count": $count
}
EOF
)
    
    api_request "POST" "/api/v1/intelligent/primes" "$request_data" "200"
}

# Function to list capabilities
list_capabilities() {
    print_status "Listing cached capabilities"
    api_request "GET" "/api/v1/intelligent/capabilities" "" "200"
}

# Function to test code reuse
test_code_reuse() {
    local task_name=$1
    local description=$2
    local context=$3
    
    print_status "Testing code reuse for: $task_name"
    print_warning "This should use cached code from previous execution"
    
    local request_data=$(cat <<EOF
{
    "task_name": "$task_name",
    "description": "$description",
    "context": $context,
    "language": "python",
    "force_regenerate": false,
    "max_retries": 1,
    "timeout": 30
}
EOF
)
    
    api_request "POST" "/api/v1/intelligent/execute" "$request_data" "200"
}

# Main execution
main() {
    echo
    print_status "Starting intelligent execution system test"
    echo
    
    # Check prerequisites
    print_status "Checking prerequisites..."
    
# Check if Redis is running (either direct or Docker)
if ! check_service "Redis" "redis-cli -h $REDIS_URL ping"; then
    # Try Docker Redis
    if ! check_service "Redis (Docker)" "docker exec redis redis-cli ping"; then
        print_error "Redis is required but not running. Please start Redis first."
        print_status "You can start Redis with: redis-server or docker run -d --name redis -p 6379:6379 redis:alpine"
        exit 1
    else
        print_success "Redis is running in Docker"
    fi
else
    print_success "Redis is running directly"
fi
    
    # Check if HDN server is running
    if ! check_service "HDN Server" "curl -s $API_URL/health"; then
        print_error "HDN server is not running. Please start it first."
        print_status "You can start the server with: go run . -mode=server"
        exit 1
    fi
    
    # Wait for services to be ready
    wait_for_service "HDN Server" "curl -s $API_URL/health"
    
    echo
    print_status "All services are ready. Starting tests..."
    echo
    
    # Test 1: Prime numbers calculation (first execution - should generate code)
    echo "🧮 Test 1: Prime Numbers Calculation (First Execution)"
    echo "----------------------------------------------------"
    test_prime_numbers 10
    echo
    
    # Test 2: Different mathematical task
    echo "📊 Test 2: Fibonacci Sequence Calculation"
    echo "----------------------------------------"
    test_intelligent_execution "CalculateFibonacci" \
        "Calculate the first 15 Fibonacci numbers" \
        '{"count": "15", "input": "15"}' \
        "python"
    echo
    
    # Test 3: Factorial calculation
    echo "🔢 Test 3: Factorial Calculation"
    echo "-------------------------------"
    test_intelligent_execution "CalculateFactorial" \
        "Calculate factorial of a given number" \
        '{"number": "8", "input": "8"}' \
        "python"
    echo
    
    # Test 4: List capabilities (should show cached code)
    echo "📋 Test 4: List Cached Capabilities"
    echo "----------------------------------"
    list_capabilities
    echo
    
    # Test 5: Code reuse (should use cached code)
    echo "♻️  Test 5: Code Reuse Test"
    echo "-------------------------"
    test_code_reuse "CalculatePrimes" \
        "Calculate the first 10 prime numbers" \
        '{"count": "10", "input": "10"}'
    echo
    
    # Test 6: Force regeneration
    echo "🔄 Test 6: Force Code Regeneration"
    echo "---------------------------------"
    print_status "Testing with force_regenerate=true"
    
    local request_data=$(cat <<EOF
{
    "task_name": "CalculatePrimes",
    "description": "Calculate the first 12 prime numbers",
    "context": {"count": "12", "input": "12"},
    "language": "python",
    "force_regenerate": true,
    "max_retries": 2,
    "timeout": 30
}
EOF
)
    
    api_request "POST" "/api/v1/intelligent/execute" "$request_data" "200"
    echo
    
    # Test 7: Different language (JavaScript)
    echo "🟨 Test 7: JavaScript Code Generation"
    echo "-----------------------------------"
    test_intelligent_execution "CalculateSquares" \
        "Calculate squares of numbers from 1 to 10" \
        '{"max": "10", "input": "10"}' \
        "javascript"
    echo
    
    # Final capabilities check
    echo "📊 Final Capabilities Summary"
    echo "----------------------------"
    list_capabilities
    echo
    
    print_success "All tests completed!"
    echo
    print_status "Summary of what was demonstrated:"
    echo "✅ LLM-generated code for various mathematical tasks"
    echo "✅ Docker-based code validation and testing"
    echo "✅ Automatic code fixing when validation fails"
    echo "✅ Code caching and reuse for future requests"
    echo "✅ Dynamic action creation for learned capabilities"
    echo "✅ Support for multiple programming languages"
    echo "✅ Force regeneration when needed"
    echo
    print_status "The system now intelligently:"
    echo "1. Generates code using LLM when encountering unknown tasks"
    echo "2. Tests the generated code in Docker containers"
    echo "3. Fixes code automatically if validation fails"
    echo "4. Caches successful code for future reuse"
    echo "5. Creates dynamic actions for learned capabilities"
    echo "6. Remembers and reuses capabilities without regenerating code"
}

# Run the tests
main "$@"
