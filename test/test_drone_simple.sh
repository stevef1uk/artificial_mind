#!/bin/bash

# Simple Drone Executor Test via HDN API
# Quick test to verify if Drone can create and run a simple program

echo "🧪 Simple Drone Executor Test"
echo "============================="

# Configuration
HDN_SERVER="http://localhost:8081"
TOOL_ENDPOINT="$HDN_SERVER/api/v1/tools/tool_drone_executor/invoke"

# Test 1: Check if HDN server is running
echo "1. Checking HDN server connectivity..."
if curl -s --connect-timeout 5 "$HDN_SERVER/api/v1/tools" > /dev/null; then
    echo "✅ HDN server is reachable"
else
    echo "❌ HDN server is not reachable at $HDN_SERVER"
    echo "Please ensure HDN server is running on port 8080"
    exit 1
fi

# Test 2: Simple Go program
echo
echo "2. Testing simple Go program execution..."

GO_PAYLOAD='{
    "code": "package main\nimport \"fmt\"\nfunc main() {\n    fmt.Println(\"Hello from Drone!\")\n    fmt.Println(\"Test successful!\")\n}",
    "language": "go",
    "image": "golang:1.21-alpine",
    "environment": {},
    "timeout": 30
}'

echo "Sending Go test..."
GO_RESPONSE=$(curl -s -X POST \
    -H "Content-Type: application/json" \
    -d "$GO_PAYLOAD" \
    "$TOOL_ENDPOINT")

echo "Response:"
echo "$GO_RESPONSE" | jq . 2>/dev/null || echo "$GO_RESPONSE"

# Check if successful
GO_SUCCESS=$(echo "$GO_RESPONSE" | jq -r '.success' 2>/dev/null)
if [ "$GO_SUCCESS" = "true" ]; then
    echo "✅ Go test PASSED"
    echo "Output: $(echo "$GO_RESPONSE" | jq -r '.output' 2>/dev/null)"
else
    echo "❌ Go test FAILED"
    echo "Error: $(echo "$GO_RESPONSE" | jq -r '.error' 2>/dev/null)"
fi

# Test 3: Simple Python program
echo
echo "3. Testing simple Python program execution..."

PYTHON_PAYLOAD='{
    "code": "print(\"Hello from Python via Drone!\")\nprint(\"Test successful!\")\nimport sys\nprint(f\"Python version: {sys.version}\")",
    "language": "python",
    "image": "python:3.11-alpine",
    "environment": {},
    "timeout": 30
}'

echo "Sending Python test..."
PYTHON_RESPONSE=$(curl -s -X POST \
    -H "Content-Type: application/json" \
    -d "$PYTHON_PAYLOAD" \
    "$TOOL_ENDPOINT")

echo "Response:"
echo "$PYTHON_RESPONSE" | jq . 2>/dev/null || echo "$PYTHON_RESPONSE"

# Check if successful
PYTHON_SUCCESS=$(echo "$PYTHON_RESPONSE" | jq -r '.success' 2>/dev/null)
if [ "$PYTHON_SUCCESS" = "true" ]; then
    echo "✅ Python test PASSED"
    echo "Output: $(echo "$PYTHON_RESPONSE" | jq -r '.output' 2>/dev/null)"
else
    echo "❌ Python test FAILED"
    echo "Error: $(echo "$PYTHON_RESPONSE" | jq -r '.error' 2>/dev/null)"
fi

echo
echo "🎉 Simple Drone Executor Test Complete!"
