#!/bin/bash
# Script to get your Telegram Chat ID

# Try to get token from k3s secret first, then fall back to env var or default
BOT_TOKEN=$(kubectl get secret telegram-bot-secret -n agi -o jsonpath='{.data.TELEGRAM_BOT_TOKEN}' 2>/dev/null | base64 -d 2>/dev/null)
if [ -z "$BOT_TOKEN" ]; then
    BOT_TOKEN="${TELEGRAM_BOT_TOKEN:-}"
fi
if [ -z "$BOT_TOKEN" ]; then
    echo "❌ Could not find TELEGRAM_BOT_TOKEN"
    echo "   Please set it in .env or ensure k3s secret exists"
    exit 1
fi

echo "📱 Getting Telegram updates..."
echo "💡 Make sure you've sent a message to your bot first!"
echo ""

RESPONSE=$(curl -s "https://api.telegram.org/bot${BOT_TOKEN}/getUpdates")

# Check if we got any updates
if echo "$RESPONSE" | grep -q '"result":\[\]'; then
    echo "❌ No messages found."
    echo ""
    echo "Please:"
    echo "1. Open Telegram"
    echo "2. Search for your bot"
    echo "3. Send it a message (like 'hello')"
    echo "4. Run this script again"
    exit 1
fi

# Extract chat IDs
echo "✅ Found chat(s):"
echo ""
echo "$RESPONSE" | python3 -c "
import sys, json
data = json.load(sys.stdin)
for update in data.get('result', []):
    if 'message' in update:
        msg = update['message']
        chat = msg.get('chat', {})
        chat_id = chat.get('id')
        chat_type = chat.get('type', 'unknown')
        chat_title = chat.get('title', '')
        username = chat.get('username', '')
        first_name = chat.get('first_name', '')
        
        if chat_type == 'private':
            print(f'👤 Personal Chat ID: {chat_id}')
            print(f'   Name: {first_name}')
            print(f'   Username: @{username}' if username else '   (No username)')
        elif chat_type == 'channel':
            print(f'📢 Channel ID: {chat_id}')
            print(f'   Name: {chat_title}')
            print(f'   Username: @{username}' if username else '   (No username)')
        elif chat_type == 'group':
            print(f'👥 Group ID: {chat_id}')
            print(f'   Name: {chat_title}')
        print('')
" 2>/dev/null || echo "$RESPONSE" | grep -o '"id":[0-9-]*' | head -1 | cut -d: -f2

echo ""
echo "💡 Copy the Chat ID above and add it to your .env file:"
echo "   TELEGRAM_CHAT_ID=<the_id_from_above>"

