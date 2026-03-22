#!/bin/bash
# Simple helper to ask PicoClaw on the RPI (1.60) a question directly.

QUESTION="${1:-'How are you?'}"
PICO_URL="http://192.168.1.60:18790/chat"

echo "Asking PicoClaw (192.168.1.60): \"$QUESTION\""
echo "------------------------------------------------"

# Run the curl and capture response
RESPONSE=$(curl -s -X POST "$PICO_URL" \
     -H "Content-Type: application/json" \
     -d "{\"message\": \"$QUESTION\", \"session_id\": \"cli-ask-$(date +%s)\"}")

# Extract response using grep/sed (simple alternative to jq)
echo "$RESPONSE" | python3 -c "import sys, json; print(json.load(sys.stdin).get('response', 'Error: No response'))"
