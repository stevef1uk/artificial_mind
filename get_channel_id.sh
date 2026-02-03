#!/bin/bash
# Script to get Channel ID by having the bot send a test message

# Get token from k3s secret
BOT_TOKEN=$(kubectl get secret telegram-bot-secret -n agi -o jsonpath='{.data.TELEGRAM_BOT_TOKEN}' 2>/dev/null | base64 -d 2>/dev/null)
if [ -z "$BOT_TOKEN" ]; then
    BOT_TOKEN="${TELEGRAM_BOT_TOKEN:-}"
fi
if [ -z "$BOT_TOKEN" ]; then
    echo "❌ Could not find TELEGRAM_BOT_TOKEN"
    exit 1
fi

echo "📱 Telegram Channel ID Finder"
echo ""
echo "To get your channel ID, you need to:"
echo ""
echo "Method 1: Forward a message from channel to bot"
echo "  1. Open your channel in Telegram"
echo "  2. Long-press any message"
echo "  3. Tap 'Forward'"
echo "  4. Search for your bot and forward the message"
echo "  5. Run: ./get_telegram_chat_id.sh"
echo ""
echo "Method 2: Use channel username (if public)"
echo "  If your channel is public, enter its username (without @):"
read -p "Channel username (or press Enter to skip): " CHANNEL_USERNAME

if [ -n "$CHANNEL_USERNAME" ]; then
    # Remove @ if user included it
    CHANNEL_USERNAME=$(echo "$CHANNEL_USERNAME" | sed 's/^@//')
    
    echo ""
    echo "📤 Sending test message to @${CHANNEL_USERNAME}..."
    
    # Send a test message
    RESPONSE=$(curl -s -X POST "https://api.telegram.org/bot${BOT_TOKEN}/sendMessage" \
        -H "Content-Type: application/json" \
        -d "{
            \"chat_id\": \"@${CHANNEL_USERNAME}\",
            \"text\": \"Test message to get channel ID\"
        }")
    
    # Check if successful
    if echo "$RESPONSE" | grep -q '"ok":true'; then
        CHAT_ID=$(echo "$RESPONSE" | python3 -c "import sys, json; print(json.load(sys.stdin)['result']['chat']['id'])" 2>/dev/null)
        echo ""
        echo "✅ Success! Channel ID: $CHAT_ID"
        echo ""
        echo "Add this to your .env file:"
        echo "TELEGRAM_CHAT_ID=$CHAT_ID"
    else
        ERROR=$(echo "$RESPONSE" | python3 -c "import sys, json; print(json.load(sys.stdin).get('description', 'Unknown error'))" 2>/dev/null)
        echo ""
        echo "❌ Failed: $ERROR"
        echo ""
        echo "Make sure:"
        echo "  1. Channel is public, OR"
        echo "  2. Bot is an admin of the channel"
    fi
else
    echo ""
    echo "Using Method 1 instead..."
    echo ""
    echo "After forwarding a message from your channel to the bot, run:"
    echo "  ./get_telegram_chat_id.sh"
fi


