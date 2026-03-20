# 🤖 Telegram Architecture (v2 with n8n Gateway)

The Artificial Mind system uses **n8n** as a centralized gateway for all Telegram interactions. This architecture eliminates polling conflicts, improves reliability, and provides a single audit trail for all messages.

## 🏗️ How it Works

1.  **Inbound (Telegram → AGI)**:
    *   Telegram pushes messages via **Webhook** to an n8n workflow.
    *   n8n forwards the message to the **HDN Server** Chat API.
    *   The AGI processes the message and generates a response.
2.  **Outbound (AGI → Telegram)**:
    *   AI agents call the `tool_telegram_send` on the HDN Server.
    *   The HDN Server forwards the message to an **n8n Outbound Webhook**.
    *   n8n sends the message to Telegram using its internal credentials.

## 🌟 Key Benefits

*   **No Polling Conflicts**: Switching to Webhooks eliminates the "409 Conflict" errors common with multiple polling bots.
*   **Centralized Credentials**: The `TELEGRAM_BOT_TOKEN` is stored only in n8n, making rotation and management simple.
*   **Rich Auditing**: Every incoming and outgoing message is logged in the n8n execution history.
*   **Scalable**: You can add processing steps (like filtering or human approval) in n8n without changing the core AGI code.

## 🚀 Setup Guide

### 1. Configure n8n Workflows
You need two workflows in n8n:
*   **Inbound Workflow**: Use the Telegram Trigger (Webhook mode) and forward to `http://hdn-server-rpi58.agi.svc.cluster.local:8080/api/v1/chat`.
*   **Outbound Workflow**: Create a Webhook node (POST) that accepts `chat_id` and `message`, and uses a Telegram Node to send them.

### 2. Configure HDN Server
The `hdn-server` must be told where the outbound gateway is. Update your cluster secrets or `.env`:

```bash
# Set the outbound gateway URL
TELEGRAM_OUTBOUND_WEBHOOK=https://k3s.sjfisher.com/webhook/UUID/send-telegram
```

### 3. Decommission old services
The standalone `telegram-bot` service is now obsolete and should be removed from the cluster:
```bash
kubectl delete deployment telegram-bot -n agi
```

## 🛠️ Testing

### Outbound Test
You can test the gateway from your terminal:
```bash
curl -X POST "https://k3s.sjfisher.com/webhook/YOUR_UUID/send-telegram" \
     -H "Content-Type: application/json" \
     -d '{"chat_id": "YOUR_CHAT_ID", "message": "Test from Gateway!"}'
```

### Inbound Test
Send any message to your bot on Telegram. It should appear in your n8n Inbound workflow executions and trigger a response from the AGI.

---
*Note: The old `telegram-bot` pod and its polling mechanism have been removed to prevent conflicts.*
