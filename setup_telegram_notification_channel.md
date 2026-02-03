# Setting Up a Telegram Notification Channel

## Option 1: Create a New Channel (Recommended)

### Step 1: Create the Channel
1. Open Telegram
2. Tap the menu (☰) → **"New Channel"**
3. Name it: **"Agent Notifications"** (or any name you like)
4. Add a description: "Notifications from AI agents" (optional)
5. Tap **"Next"**
6. Choose who can add members:
   - **"Only members"** = Private (recommended)
   - "Everyone" = Public
7. Tap **"Create"**

**Note:** If you don't see privacy options during creation, you can make it private later:
- Open the channel → Channel name → "Channel Type" → Change to "Private"

### Step 2: Add Your Bot as Administrator
1. In your new channel, tap the channel name at the top
2. Tap **"Administrators"**
3. Tap **"Add Administrator"**
4. Search for and select **your bot** (the same one you're using for inbound messages)
5. Give it permission to **"Post Messages"**
6. Tap **"Done"**

### Step 3: Get the Channel ID
1. Send a test message to the channel (any message)
2. Run this command:
   ```bash
   ./get_telegram_chat_id.sh
   ```
3. Look for the **Channel ID** (it will be a negative number like `-1001234567890`)

### Step 4: Configure
Add to your `.env` file:
```env
TELEGRAM_CHAT_ID=-1001234567890
```

Now your agents will send notifications to this channel, and your inbound bot will continue working normally!

## Option 2: Use Your Personal Chat (Simpler)

You can also send notifications to your personal chat with the bot:

1. Send a message to your bot (like "test")
2. Run `./get_telegram_chat_id.sh`
3. Copy your **Personal Chat ID** (a positive number)
4. Add to `.env`: `TELEGRAM_CHAT_ID=123456789`

This way notifications come to you directly, separate from any commands you send to the bot.

