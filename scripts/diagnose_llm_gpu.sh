#!/bin/bash
# Diagnostic script to check if LLM server is using GPU and responding quickly

set -e

echo "🔍 LLM GPU Diagnostic Script"
echo "=============================="
echo ""

# Get LLM server URL from environment or use default
LLM_URL="${OPENAI_BASE_URL:-http://192.168.1.45:8085}"
if [ -z "$OPENAI_BASE_URL" ] && [ -n "$OLLAMA_BASE_URL" ]; then
    LLM_URL="${OLLAMA_BASE_URL%/api/chat}"
fi

echo "Testing LLM server at: $LLM_URL"
echo ""

# Test 1: Check if server is reachable
echo "1. Checking server connectivity..."
if curl -s --max-time 5 "$LLM_URL" > /dev/null 2>&1 || curl -s --max-time 5 "$LLM_URL/health" > /dev/null 2>&1; then
    echo "   ✅ Server is reachable"
else
    echo "   ❌ Server is not reachable at $LLM_URL"
    echo "   Please check:"
    echo "   - Is the LLM server running?"
    echo "   - Is the URL correct?"
    exit 1
fi

# Test 2: Test inference speed
echo ""
echo "2. Testing inference speed (this may take a while)..."
MODEL="${LLM_MODEL:-gemma-3-1b-it-q4_k_m.gguf}"

START_TIME=$(date +%s.%N)

# Try OpenAI-compatible format first
RESPONSE=$(curl -s --max-time 120 \
  -H "Content-Type: application/json" \
  -d "{
    \"model\": \"$MODEL\",
    \"messages\": [{\"role\": \"user\", \"content\": \"Say hello in one word.\"}],
    \"max_tokens\": 10
  }" \
  "$LLM_URL/v1/chat/completions" 2>&1)

END_TIME=$(date +%s.%N)
DURATION=$(echo "$END_TIME - $START_TIME" | bc)

# If OpenAI format failed, try Ollama format
if echo "$RESPONSE" | grep -q "error\|404\|Connection refused"; then
    echo "   Trying Ollama format..."
    START_TIME=$(date +%s.%N)
    RESPONSE=$(curl -s --max-time 120 \
      -H "Content-Type: application/json" \
      -d "{
        \"model\": \"$MODEL\",
        \"messages\": [{\"role\": \"user\", \"content\": \"Say hello in one word.\"}],
        \"stream\": false
      }" \
      "${LLM_URL%/v1/chat/completions}/api/chat" 2>&1)
    END_TIME=$(date +%s.%N)
    DURATION=$(echo "$END_TIME - $START_TIME" | bc)
fi

echo "   Response time: ${DURATION}s"

if (( $(echo "$DURATION < 5" | bc -l) )); then
    echo "   ✅ Fast response (< 5s) - GPU likely being used"
elif (( $(echo "$DURATION < 15" | bc -l) )); then
    echo "   ⚠️  Moderate response (5-15s) - May be using GPU with some CPU"
else
    echo "   ❌ Slow response (> 15s) - Likely using CPU only"
    echo "   This will cause LLM slot timeouts!"
fi

# Test 3: Check response content
echo ""
echo "3. Checking response content..."
if echo "$RESPONSE" | grep -q "hello\|Hello\|hi\|Hi"; then
    echo "   ✅ Response contains expected content"
else
    echo "   ⚠️  Response may be incomplete or error:"
    echo "   $(echo "$RESPONSE" | head -c 200)"
fi

# Summary
echo ""
echo "=============================="
echo "Summary:"
echo "  Server URL: $LLM_URL"
echo "  Model: $MODEL"
echo "  Response Time: ${DURATION}s"
echo ""

if (( $(echo "$DURATION > 15" | bc -l) )); then
    echo "⚠️  WARNING: Slow response detected!"
    echo ""
    echo "Recommended actions:"
    echo "1. Check if LLM server is using GPU:"
    echo "   - For llama.cpp: Check if started with -ngl flag"
    echo "   - For Ollama: Check GPU usage in logs"
    echo ""
    echo "2. See docs/GPU_DIAGNOSTIC_RPI.md for detailed instructions"
    echo ""
    echo "3. Temporary workaround: Reduce LLM_MAX_CONCURRENT_REQUESTS to 1"
    exit 1
else
    echo "✅ LLM server appears to be working correctly"
    exit 0
fi









