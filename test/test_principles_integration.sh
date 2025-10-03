#!/bin/bash

echo "🧪 Testing HDN-Principles Integration"
echo "===================================="

# Resolve project root from env or current dir
AGI_PROJECT_ROOT=${AGI_PROJECT_ROOT:-$(pwd)}

# Start principles server in background
echo "🚀 Starting principles server..."
cd "$AGI_PROJECT_ROOT/principles"
go run main.go &
PRINCIPLES_PID=$!

# Wait for server to start
echo "⏳ Waiting for principles server to start..."
sleep 3

# Test if server is running
if ! curl -s http://localhost:8080/action > /dev/null 2>&1; then
    echo "❌ Principles server failed to start"
    kill $PRINCIPLES_PID 2>/dev/null
    exit 1
fi

echo "✅ Principles server is running"

# Run HDN principles test
echo ""
echo "🔍 Running HDN principles integration test..."
cd "$AGI_PROJECT_ROOT/hdn"
go run . -mode=principles-test

# Clean up
echo ""
echo "🧹 Cleaning up..."
kill $PRINCIPLES_PID 2>/dev/null
wait $PRINCIPLES_PID 2>/dev/null

echo "✅ Test completed!"
