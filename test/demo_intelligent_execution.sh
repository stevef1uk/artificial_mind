#!/bin/bash

# Simple demo script for the intelligent execution system
# This shows the key features: LLM code generation, Docker testing, caching, and reuse

echo "🧠 HDN Intelligent Execution System Demo"
echo "======================================="
echo

# Check if services are running
echo "🔍 Checking services..."

# Check Redis (either direct or Docker)
if ! redis-cli -h localhost:6379 ping > /dev/null 2>&1; then
    # Try Docker Redis
    if ! docker exec redis redis-cli ping > /dev/null 2>&1; then
        echo "❌ Redis is not running. Please start it with: redis-server or docker run -d --name redis -p 6379:6379 redis:alpine"
        exit 1
    else
        echo "✅ Redis is running in Docker"
    fi
else
    echo "✅ Redis is running directly"
fi

# Check HDN server
if ! curl -s http://localhost:8080/health > /dev/null 2>&1; then
    echo "❌ HDN server is not running. Please start it with: go run . -mode=server"
    exit 1
fi
echo "✅ HDN server is running"

echo
echo "🚀 Starting intelligent execution demo..."
echo

# Test 1: Calculate first 10 prime numbers (first time - will generate code)
echo "📊 Test 1: Calculate First 10 Prime Numbers (First Execution)"
echo "------------------------------------------------------------"
echo "This will use the LLM to generate Python code, test it in Docker, and cache it."
echo

curl -s -X POST http://localhost:8080/api/v1/intelligent/primes \
  -H "Content-Type: application/json" \
  -d '{"count": 10}' | jq '{
    success: .success,
    used_cached_code: .used_cached_code,
    retry_count: .retry_count,
    execution_time_ms: .execution_time_ms,
    result: .result,
    generated_code: .generated_code.task_name
  }'

echo
echo

# Test 2: Calculate first 15 prime numbers (should use cached code)
echo "📊 Test 2: Calculate First 15 Prime Numbers (Reuse Cached Code)"
echo "---------------------------------------------------------------"
echo "This should reuse the cached code from the previous execution."
echo

curl -s -X POST http://localhost:8080/api/v1/intelligent/primes \
  -H "Content-Type: application/json" \
  -d '{"count": 15}' | jq '{
    success: .success,
    used_cached_code: .used_cached_code,
    retry_count: .retry_count,
    execution_time_ms: .execution_time_ms,
    result: .result
  }'

echo
echo

# Test 3: Different mathematical task - Fibonacci sequence
echo "📊 Test 3: Calculate Fibonacci Sequence (New Task)"
echo "--------------------------------------------------"
echo "This will generate new code for a different mathematical task."
echo

curl -s -X POST http://localhost:8080/api/v1/intelligent/execute \
  -H "Content-Type: application/json" \
  -d '{
    "task_name": "CalculateFibonacci",
    "description": "Calculate the first 12 Fibonacci numbers",
    "context": {"count": "12", "input": "12"},
    "language": "python",
    "force_regenerate": false,
    "max_retries": 3,
    "timeout": 30
  }' | jq '{
    success: .success,
    used_cached_code: .used_cached_code,
    retry_count: .retry_count,
    execution_time_ms: .execution_time_ms,
    result: .result,
    generated_code: .generated_code.task_name
  }'

echo
echo

# Test 4: List all cached capabilities
echo "📋 Test 4: List Cached Capabilities"
echo "----------------------------------"
echo "This shows all the capabilities the system has learned and cached."
echo

curl -s -X GET http://localhost:8080/api/v1/intelligent/capabilities | jq '{
  total_capabilities: (.capabilities | length),
  capabilities: [.capabilities[] | {
    task_name: .task_name,
    language: .language,
    created_at: .created_at,
    tags: .tags
  }],
  stats: .stats
}'

echo
echo

# Test 5: Force regeneration
echo "🔄 Test 5: Force Code Regeneration"
echo "---------------------------------"
echo "This will force the system to regenerate code even if cached code exists."
echo

curl -s -X POST http://localhost:8080/api/v1/intelligent/execute \
  -H "Content-Type: application/json" \
  -d '{
    "task_name": "CalculatePrimes",
    "description": "Calculate the first 8 prime numbers",
    "context": {"count": "8", "input": "8"},
    "language": "python",
    "force_regenerate": true,
    "max_retries": 2,
    "timeout": 30
  }' | jq '{
    success: .success,
    used_cached_code: .used_cached_code,
    retry_count: .retry_count,
    execution_time_ms: .execution_time_ms,
    result: .result
  }'

echo
echo
echo "🎉 Demo completed!"
echo
echo "Key features demonstrated:"
echo "✅ LLM-generated code for mathematical tasks"
echo "✅ Docker-based code validation and testing"
echo "✅ Automatic code caching for future reuse"
echo "✅ Code reuse without regeneration"
echo "✅ Force regeneration when needed"
echo "✅ Capability tracking and statistics"
echo
echo "The system now intelligently:"
echo "1. Generates code using LLM when encountering unknown tasks"
echo "2. Tests the generated code in Docker containers"
echo "3. Fixes code automatically if validation fails"
echo "4. Caches successful code for future reuse"
echo "5. Creates dynamic actions for learned capabilities"
echo "6. Remembers and reuses capabilities without regenerating code"
