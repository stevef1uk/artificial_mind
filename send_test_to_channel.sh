#!/bin/bash
# Quick script to send a test message to a channel and get its ID

# Get token from k3s
BOT_TOKEN=$(kubectl get secret telegram-bot-secret -n agi -o jsonpath='{.data.TELEGRAM_BOT_TOKEN}' 2>/dev/null | base64 -d 2>/dev/null)

if [ -z "$1" ]; then
    echo "Usage: $0 <channel_username_without_@>"
    echo "Example: $0 mychannel"
    echo ""
    echo "Or if you know the channel ID (negative number), you can test it:"
    echo "Usage: $0 -1001234567890"
    exit 1
fi

CHANNEL="$1"

# Check if it's a numeric ID (starts with -)
if [[ "$CHANNEL" =~ ^- ]]; then
    CHAT_ID="$CHANNEL"
    echo "📤 Testing with Channel ID: $CHAT_ID"
else
    # Remove @ if present
    CHANNEL=$(echo "$CHANNEL" | sed 's/^@//')
    CHAT_ID="@${CHANNEL}"
    echo "📤 Testing with Channel username: @${CHANNEL}"
fi

echo "Sending test message..."
RESPONSE=$(curl -s -X POST "https://api.telegram.org/bot${BOT_TOKEN}/sendMessage" \
    -H "Content-Type: application/json" \
    -d "{
        \"chat_id\": \"${CHAT_ID}\",
        \"text\": \"✅ Test message from agent system\"
    }")

if echo "$RESPONSE" | grep -q '"ok":true'; then
    ACTUAL_CHAT_ID=$(echo "$RESPONSE" | python3 -c "import sys, json; print(json.load(sys.stdin)['result']['chat']['id'])" 2>/dev/null)
    echo ""
    echo "✅ Success! Message sent."
    echo ""
    echo "📋 Channel ID: $ACTUAL_CHAT_ID"
    echo ""
    echo "Add this to your .env file:"
    echo "TELEGRAM_CHAT_ID=$ACTUAL_CHAT_ID"
else
    ERROR=$(echo "$RESPONSE" | python3 -c "import sys, json; print(json.load(sys.stdin).get('description', 'Unknown error'))" 2>/dev/null)
    echo ""
    echo "❌ Failed: $ERROR"
    echo ""
    echo "Make sure:"
    echo "  1. Bot is an administrator of the channel"
    echo "  2. Bot has permission to post messages"
    echo "  3. Channel username is correct (if using username)"
fi



