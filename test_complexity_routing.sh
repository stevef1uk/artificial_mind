#!/bin/bash

# Test script to verify hierarchical planner routing
# This tests both simple (direct execution) and complex (hierarchical planning) tasks

echo "🧪 Testing Task Complexity Routing"
echo "=================================="

# Test 1: Simple task (should use direct execution)
echo ""
echo "📝 Test 1: Simple Task (should use direct execution)"
echo "Request: Write a Go program that prints 'Hello World'"
echo "Expected: Direct execution, fast response, simple workflow"

curl -s -X POST "http://localhost:8081/api/v1/intelligent/execute" \
  -H "Content-Type: application/json" \
  -d '{
    "task_name": "simple_test",
    "description": "Write a Go program that prints 'Hello World'",
    "context": {
      "artifacts_wrapper": "true",
      "force_regenerate": "true"
    },
    "language": "go",
    "force_regenerate": true,
    "max_retries": 3,
    "timeout": 600
  }' | jq -r '.workflow_id, .execution_time_ms, .validation_steps[0].step // "no_validation_steps"'

echo ""
echo "⏱️  Waiting 5 seconds before next test..."
sleep 5

# Test 2: Complex task (should use hierarchical planning)
echo ""
echo "📝 Test 2: Complex Task (should use hierarchical planning)"
echo "Request: Build a REST API with authentication and database integration"
echo "Expected: Hierarchical planning, multi-step workflow, longer execution time"

curl -s -X POST "http://localhost:8081/api/v1/intelligent/execute" \
  -H "Content-Type: application/json" \
  -d '{
    "task_name": "complex_test",
    "description": "Build a REST API with authentication and database integration",
    "context": {
      "artifacts_wrapper": "true",
      "force_regenerate": "true"
    },
    "language": "go",
    "force_regenerate": true,
    "max_retries": 3,
    "timeout": 600
  }' | jq -r '.workflow_id, .execution_time_ms, .validation_steps[0].step // "no_validation_steps"'

echo ""
echo "⏱️  Waiting 5 seconds before next test..."
sleep 5

# Test 3: Medium complexity task (edge case)
echo ""
echo "📝 Test 3: Medium Complexity Task (edge case)"
echo "Request: Create a data pipeline that processes CSV files and sends email notifications"
echo "Expected: Could go either way depending on LLM classification"

curl -s -X POST "http://localhost:8081/api/v1/intelligent/execute" \
  -H "Content-Type: application/json" \
  -d '{
    "task_name": "medium_test",
    "description": "Create a data pipeline that processes CSV files and sends email notifications",
    "context": {
      "artifacts_wrapper": "true",
      "force_regenerate": "true"
    },
    "language": "go",
    "force_regenerate": true,
    "max_retries": 3,
    "timeout": 600
  }' | jq -r '.workflow_id, .execution_time_ms, .validation_steps[0].step // "no_validation_steps"'

echo ""
echo "✅ Complexity routing tests completed!"
echo ""
echo "🔍 Check the HDN server logs to see the LLM complexity classifications:"
echo "   tail -f /tmp/hdn_server.log | grep 'INTELLIGENT.*classified task as'"
