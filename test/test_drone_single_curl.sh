#!/bin/bash

# Single curl command to test Drone executor
# This is the simplest possible test

echo "🧪 Single Curl Test for Drone Executor"
echo "======================================"

# Test with a simple Go program
echo "Testing with simple Go program..."

curl -X POST \
  -H "Content-Type: application/json" \
  -d '{
    "code": "package main\nimport \"fmt\"\nfunc main() {\n    fmt.Println(\"Hello from Drone!\")\n    fmt.Println(\"This is a test of the Drone executor on RPI\")\n}",
    "language": "go",
    "image": "golang:1.21-alpine",
    "environment": {},
    "timeout": 30
  }' \
  http://localhost:8081/api/v1/tools/tool_drone_executor/invoke

echo
echo "Test completed!"
