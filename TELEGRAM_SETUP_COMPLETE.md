# ✅ Telegram Channel Setup - Almost Done!

## Channel ID Found: `-3712575871`

✅ Added to `.env` file: `TELEGRAM_CHAT_ID=-3712575871`

## ⚠️ Important: Add Bot as Administrator

The bot needs to be an administrator of your channel to send messages. Here's how:

### Steps:
1. **Open your "Agent Notifications" channel** in Telegram
2. **Tap the channel name** at the top
3. **Tap "Administrators"** (or "Admins")
4. **Tap "Add Administrator"**
5. **Search for your bot** (the same bot you use for inbound messages)
6. **Select your bot**
7. **Give it permission to "Post Messages"** ✅
8. **Tap "Done"**

### After Adding Bot as Admin:

Test it by running:
```bash
./send_test_to_channel.sh -3712575871
```

Or manually test:
```bash
BOT_TOKEN=$(kubectl get secret telegram-bot-secret -n agi -o jsonpath='{.data.TELEGRAM_BOT_TOKEN}' 2>/dev/null | base64 -d)
curl -X POST "https://api.telegram.org/bot${BOT_TOKEN}/sendMessage" \
  -H "Content-Type: application/json" \
  -d '{"chat_id": "-3712575871", "text": "✅ Test - notifications are working!"}'
```

## Once Working:

Your website status monitor agent will automatically send notifications to this channel every 15 minutes!

Example notification:
```
🌐 *Website Status Report*

https://me.sjfisher.com: ✅ Up (HTTP 200) - 234ms
https://k3s.sjfisher.com: ✅ Up (HTTP 200) - 456ms

✅ All websites are operational!
```



