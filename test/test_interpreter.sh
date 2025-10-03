#!/bin/bash

# Test script for the interpreter functionality

echo "🧪 Testing Interpreter API..."

# Test 1: Simple interpretation
echo "Test 1: Simple interpretation"
curl -X POST http://localhost:8081/api/v1/interpret \
  -H "Content-Type: application/json" \
  -d '{
    "input": "Find the first 10 prime numbers",
    "context": {},
    "session_id": "test_session_1"
  }' | jq '.'

echo -e "\n"

# Test 2: Multi-step interpretation
echo "Test 2: Multi-step interpretation"
curl -X POST http://localhost:8081/api/v1/interpret \
  -H "Content-Type: application/json" \
  -d '{
    "input": "Find the first 20 primes and show me a graph of distribution",
    "context": {},
    "session_id": "test_session_2"
  }' | jq '.'

echo -e "\n"

# Test 3: Interpret and execute
echo "Test 3: Interpret and execute"
curl -X POST http://localhost:8081/api/v1/interpret/execute \
  -H "Content-Type: application/json" \
  -d '{
    "input": "Calculate the first 5 prime numbers",
    "context": {},
    "session_id": "test_session_3"
  }' | jq '.'

echo -e "\n"
echo "✅ Interpreter tests completed!"

