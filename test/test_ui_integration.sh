#!/bin/bash

# Test script for UI integration with natural language interpreter

echo "🧪 Testing UI Integration with Natural Language Interpreter..."

# Check if services are running
echo "Checking if services are running..."

# Check HDN server
if curl -s http://localhost:8081/health > /dev/null; then
    echo "✅ HDN Server is running on port 8081"
else
    echo "❌ HDN Server is not running. Please start it first."
    exit 1
fi

# Check Monitor UI
if curl -s http://localhost:8082/api/status > /dev/null; then
    echo "✅ Monitor UI is running on port 8082"
else
    echo "❌ Monitor UI is not running. Please start it first."
    exit 1
fi

echo ""
echo "🎯 Testing Natural Language Interpretation..."

# Test 1: Simple interpretation
echo "Test 1: Simple interpretation"
curl -X POST http://localhost:8082/api/interpret \
  -H "Content-Type: application/json" \
  -d '{
    "input": "Find the first 10 prime numbers",
    "context": {},
    "session_id": "test_session_1"
  }' | jq '.'

echo -e "\n"

# Test 2: Multi-step interpretation
echo "Test 2: Multi-step interpretation"
curl -X POST http://localhost:8082/api/interpret \
  -H "Content-Type: application/json" \
  -d '{
    "input": "Find the first 20 primes and show me a graph of distribution",
    "context": {},
    "session_id": "test_session_2"
  }' | jq '.'

echo -e "\n"

# Test 3: Interpret and execute
echo "Test 3: Interpret and execute"
curl -X POST http://localhost:8082/api/interpret/execute \
  -H "Content-Type: application/json" \
  -d '{
    "input": "Calculate the first 5 prime numbers",
    "context": {},
    "session_id": "test_session_3"
  }' | jq '.'

echo -e "\n"
echo "✅ UI Integration tests completed!"
echo ""
echo "🌐 You can now visit http://localhost:8082 to use the natural language interface!"
echo "   Try entering: 'Find the first 20 primes and show me a graph of distribution'"

