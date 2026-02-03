#!/bin/bash
# Simple script to find channel ID - multiple methods

# Get token from k3s
BOT_TOKEN=$(kubectl get secret telegram-bot-secret -n agi -o jsonpath='{.data.TELEGRAM_BOT_TOKEN}' 2>/dev/null | base64 -d 2>/dev/null)

echo "🔍 Finding Your Channel ID"
echo ""
echo "Method 1: If your channel has a username (public channel)"
echo "  Enter your channel username (like 'mychannel' without @):"
read -p "Channel username: " CHANNEL_USERNAME

if [ -n "$CHANNEL_USERNAME" ]; then
    CHANNEL_USERNAME=$(echo "$CHANNEL_USERNAME" | sed 's/^@//')
    echo ""
    echo "📤 Sending test message to @${CHANNEL_USERNAME}..."
    
    RESPONSE=$(curl -s -X POST "https://api.telegram.org/bot${BOT_TOKEN}/sendMessage" \
        -H "Content-Type: application/json" \
        -d "{
            \"chat_id\": \"@${CHANNEL_USERNAME}\",
            \"text\": \"Test - please ignore\"
        }")
    
    if echo "$RESPONSE" | grep -q '"ok":true'; then
        CHAT_ID=$(echo "$RESPONSE" | python3 -c "import sys, json; print(json.load(sys.stdin)['result']['chat']['id'])" 2>/dev/null)
        echo ""
        echo "✅ SUCCESS! Found Channel ID: $CHAT_ID"
        echo ""
        echo "Add this to your .env file:"
        echo "TELEGRAM_CHAT_ID=$CHAT_ID"
        exit 0
    else
        ERROR=$(echo "$RESPONSE" | python3 -c "import sys, json; print(json.load(sys.stdin).get('description', 'Unknown'))" 2>/dev/null)
        echo "❌ Failed: $ERROR"
        echo ""
        echo "This might mean:"
        echo "  - Channel is private (no username)"
        echo "  - Bot is not an admin"
        echo ""
    fi
fi

echo ""
echo "Method 2: Find Channel ID in Telegram app"
echo ""
echo "1. Open your channel in Telegram"
echo "2. Tap the channel name at the top"
echo "3. Look for 'Channel ID' or 'Chat ID'"
echo "   (Some Telegram clients show this)"
echo ""
echo "OR try this:"
echo "4. In channel settings, look for 'Link' or 'Invite Link'"
echo "5. The link might contain the channel ID"
echo ""
echo "Method 3: Use a helper bot"
echo ""
echo "1. Add @userinfobot to your channel as a member"
echo "2. Send any message in the channel"
echo "3. The bot will reply with the channel ID"
echo ""
echo "Method 4: Manual test"
echo ""
echo "If you think you know the channel ID (it's a negative number),"
echo "we can test it. Enter it here (or press Enter to skip):"
read -p "Channel ID to test: " TEST_ID

if [ -n "$TEST_ID" ]; then
    echo ""
    echo "📤 Testing Channel ID: $TEST_ID"
    RESPONSE=$(curl -s -X POST "https://api.telegram.org/bot${BOT_TOKEN}/sendMessage" \
        -H "Content-Type: application/json" \
        -d "{
            \"chat_id\": \"${TEST_ID}\",
            \"text\": \"✅ Test message - if you see this, the ID is correct!\"
        }")
    
    if echo "$RESPONSE" | grep -q '"ok":true'; then
        echo ""
        echo "✅ SUCCESS! The Channel ID works!"
        echo ""
        echo "Add this to your .env file:"
        echo "TELEGRAM_CHAT_ID=$TEST_ID"
    else
        ERROR=$(echo "$RESPONSE" | python3 -c "import sys, json; print(json.load(sys.stdin).get('description', 'Unknown'))" 2>/dev/null)
        echo ""
        echo "❌ Failed: $ERROR"
        echo "That Channel ID doesn't work. Try Method 3 (helper bot)."
    fi
fi


