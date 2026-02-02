#!/bin/bash
# Test the proxy with correct OpenAI format

echo "🧪 Testing TPU Proxy with Qwen3-1.7B"
echo "======================================"

curl -N http://192.168.1.60:11434/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "qwen3-1.7b-ax650",
    "messages": [
      {
        "role": "user",
        "content": "Write a simple Python hello world function"
      }
    ],
    "stream": false
  }'

echo ""
echo "======================================"
