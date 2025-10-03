#!/bin/bash

echo "🧪 Testing Ollama Integration with HDN"
echo "====================================="

# Check if Ollama is running
echo "🔍 Checking if Ollama is running..."
if curl -s http://localhost:11434/api/tags > /dev/null; then
    echo "✅ Ollama is running"
else
    echo "❌ Ollama is not running. Please start Ollama first:"
    echo "   ollama serve"
    exit 1
fi

# Check if gemma3:12b model is available
echo "🔍 Checking if gemma3:12b model is available..."
if curl -s http://localhost:11434/api/tags | grep -q "gemma3:12b"; then
    echo "✅ gemma3:12b model is available"
else
    echo "❌ gemma3:12b model not found. Please pull it first:"
    echo "   ollama pull gemma3:12b"
    exit 1
fi

# Test Ollama API directly
echo "🔍 Testing Ollama API directly..."
response=$(curl -s -X POST http://localhost:11434/api/chat \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gemma3:12b",
    "messages": [
      {
        "role": "user",
        "content": "Hello! Please respond with just: Test successful"
      }
    ],
    "stream": false
  }')

if echo "$response" | grep -q "Test successful"; then
    echo "✅ Ollama API is working correctly"
else
    echo "❌ Ollama API test failed"
    echo "Response: $response"
    exit 1
fi

# Resolve project root from env or current dir
AGI_PROJECT_ROOT=${AGI_PROJECT_ROOT:-$(pwd)}

# Test HDN with Ollama
echo "🔍 Testing HDN with Ollama integration..."
cd "$AGI_PROJECT_ROOT/hdn"

# Run a simple test
echo "Running HDN test with Ollama..."
go run . -mode=test-llm

echo "✅ Ollama integration test completed!"
