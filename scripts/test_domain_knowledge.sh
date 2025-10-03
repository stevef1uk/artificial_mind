#!/bin/bash

# Test script for domain knowledge API endpoints
# Make sure Neo4j and HDN API are running

HDN_URL="http://localhost:8081"
NEO4J_URL="http://localhost:7474"

echo "🧠 Testing Domain Knowledge API"
echo "================================"

# Check if services are running
echo "🔍 Checking services..."
if ! curl -s "$HDN_URL/health" > /dev/null; then
    echo "❌ HDN API is not running on $HDN_URL"
    exit 1
fi

if ! curl -s "$NEO4J_URL" > /dev/null; then
    echo "❌ Neo4j is not running on $NEO4J_URL"
    exit 1
fi

echo "✅ Services are running"

# Test 1: List all concepts
echo -e "\n📋 Test 1: List all concepts"
curl -s "$HDN_URL/api/v1/knowledge/concepts" | jq '.'

# Test 2: Search concepts by domain
echo -e "\n🔍 Test 2: Search Math concepts"
curl -s "$HDN_URL/api/v1/knowledge/search?domain=Math" | jq '.'

# Test 3: Get specific concept
echo -e "\n📖 Test 3: Get Matrix Multiplication concept"
curl -s "$HDN_URL/api/v1/knowledge/concepts/Matrix%20Multiplication" | jq '.'

# Test 4: Get related concepts
echo -e "\n🔗 Test 4: Get concepts related to Matrix Multiplication"
curl -s "$HDN_URL/api/v1/knowledge/concepts/Matrix%20Multiplication/related" | jq '.'

# Test 5: Create a new concept
echo -e "\n➕ Test 5: Create a new concept"
curl -s -X POST "$HDN_URL/api/v1/knowledge/concepts" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Fibonacci Sequence",
    "domain": "Math",
    "definition": "A sequence where each number is the sum of the two preceding ones"
  }' | jq '.'

# Test 6: Add a property to the new concept
echo -e "\n🏷️ Test 6: Add property to Fibonacci Sequence"
curl -s -X POST "$HDN_URL/api/v1/knowledge/concepts/Fibonacci%20Sequence/properties" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Golden Ratio",
    "description": "Ratio of consecutive Fibonacci numbers approaches the golden ratio",
    "type": "mathematical"
  }' | jq '.'

# Test 7: Add a constraint
echo -e "\n⚠️ Test 7: Add constraint to Fibonacci Sequence"
curl -s -X POST "$HDN_URL/api/v1/knowledge/concepts/Fibonacci%20Sequence/constraints" \
  -H "Content-Type: application/json" \
  -d '{
    "description": "First two numbers must be 0 and 1",
    "type": "initialization",
    "severity": "error"
  }' | jq '.'

# Test 8: Add an example
echo -e "\n📝 Test 8: Add example to Fibonacci Sequence"
curl -s -X POST "$HDN_URL/api/v1/knowledge/concepts/Fibonacci%20Sequence/examples" \
  -H "Content-Type: application/json" \
  -d '{
    "input": "n=10",
    "output": "0, 1, 1, 2, 3, 5, 8, 13, 21, 34",
    "type": "sequence"
  }' | jq '.'

# Test 9: Create a relationship
echo -e "\n🔗 Test 9: Create relationship between concepts"
curl -s -X POST "$HDN_URL/api/v1/knowledge/concepts/Fibonacci%20Sequence/relations" \
  -H "Content-Type: application/json" \
  -d '{
    "relation_type": "RELATED_TO",
    "target_concept": "Prime Number",
    "properties": {
      "description": "Fibonacci numbers can be used in prime number generation"
    }
  }' | jq '.'

# Test 10: Verify the new concept
echo -e "\n✅ Test 10: Verify Fibonacci Sequence concept"
curl -s "$HDN_URL/api/v1/knowledge/concepts/Fibonacci%20Sequence" | jq '.'

echo -e "\n🎉 Domain knowledge testing complete!"
echo "Check the Neo4j browser at $NEO4J_URL to see the knowledge graph"
