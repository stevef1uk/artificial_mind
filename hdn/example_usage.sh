#!/bin/bash

# Example usage script for HTN API
# This script demonstrates how to use the API for task execution and learning

echo "=== HTN API Example Usage ==="
echo

# Start the server in the background
echo "Starting API server..."
./hdn -mode=server -port=8080 &
SERVER_PID=$!

# Wait for server to start
sleep 3

echo "Server started with PID: $SERVER_PID"
echo

# Test health endpoint
echo "1. Testing health endpoint:"
curl -s http://localhost:8080/health | jq .
echo

# Test task planning
echo "2. Testing task planning:"
curl -s -X POST http://localhost:8080/api/v1/task/plan \
  -H "Content-Type: application/json" \
  -d '{"task_name": "DeliverReport", "state": {"draft_written": false, "review_done": false, "report_submitted": false}}' | jq .
echo

# Test task execution
echo "3. Testing task execution:"
curl -s -X POST http://localhost:8080/api/v1/task/execute \
  -H "Content-Type: application/json" \
  -d '{"task_name": "DeliverReport", "state": {"draft_written": false, "review_done": false, "report_submitted": false}}' | jq .
echo

# Test LLM learning
echo "4. Testing LLM learning:"
curl -s -X POST http://localhost:8080/api/v1/learn/llm \
  -H "Content-Type: application/json" \
  -d '{"task_name": "CreatePresentation", "description": "Create a presentation with slides and content", "context": {"topic": "AI Planning", "audience": "developers"}}' | jq .
echo

# Test MCP learning (this will fail with mock tools)
echo "5. Testing MCP learning:"
curl -s -X POST http://localhost:8080/api/v1/learn/mcp \
  -H "Content-Type: application/json" \
  -d '{"task_name": "ProcessFile", "description": "Process a file using available tools", "context": {"file_path": "/tmp/data.txt"}}' | jq .
echo

# Show current domain
echo "6. Current domain after learning:"
curl -s http://localhost:8080/api/v1/domain | jq .
echo

# Test the newly learned task
echo "7. Testing newly learned task:"
curl -s -X POST http://localhost:8080/api/v1/task/plan \
  -H "Content-Type: application/json" \
  -d '{"task_name": "CreatePresentation", "state": {"presentation_created": false}}' | jq .
echo

# Cleanup
echo "Stopping server..."
kill $SERVER_PID
echo "Done!"
