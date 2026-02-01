# 🤖 Telegram Bot Integration

The Artificial Mind system includes a fully integrated Telegram bot, enabling secure, mobile-friendly interaction with the AGI. This bot acts as a bridge to the HDN (Hierarchical Decision Network), allowing you to chat naturally, execute tools, query knowledge bases, and observe the AI's reasoning process in real-time.

## 🌟 Key Features

*   **Conversational Interface**: Chat naturally with the AGI as you would with a human. The bot maintains context across the session.
*   **Thinking Mode Visibility**: Toggle "Thinking Mode" (`/thinking`) to see the AGI's internal reasoning traces (Chain of Thought) before it answers.
*   **Tool Execution**: The bot can autonomously decide to use tools (web scraper, knowledge graph queries, vector search) to answer your questions.
*   **Secure Access**: A whitelist system (`ALLOWED_TELEGRAM_USERS`) ensures only authorized users can interact with your AGI instance.
*   **Rich Formatting**: Responses are formatted with Markdown for better readability, including code blocks, bold text, and lists.

## 🚀 Setup Guide

### 1. Create a Telegram Bot
1.  Open Telegram and search for **@BotFather**.
2.  Send the command `/newbot`.
3.  Follow the prompts to name your bot and give it a username.
4.  **Important**: Copy the HTTP API Token provided by BotFather.

### 2. Configure Environment Variables
Add the following to your `.env` file in the project root:

```env
# Telegram Configuration
TELEGRAM_BOT_TOKEN=your_token_from_botfather
ALLOWED_TELEGRAM_USERS=your_telegram_username,another_username
```

*   `TELEGRAM_BOT_TOKEN`: The token you got from BotFather.
*   `ALLOWED_TELEGRAM_USERS`: A comma-separated list of usernames (without the `@`) permitted to use the bot.
    *   *Note*: If you don't know your username, start the bot, message it, and check the logs. The bot will log your username and numeric ID.

### 3. Start the System
The bot is integrated into the standard startup scripts.

```bash
# Start everything including the bot
./quick-start.sh
```

Or, if allowed to run background processes:
```bash
./scripts/start_servers.sh
```

## 🛠️ Commands

| Command | Description |
| :--- | :--- |
| `/start` | Initialize the conversation and see a welcome message. |
| `/help` | List available commands. |
| `/thinking` | **Toggle Thinking Mode**. Enable to see "💭 Thinking Process" bubbles before the final answer. |
| `/scrape <url>` | Manually trigger the web scraper tool on a specific URL. |
| `/query <cypher>` | Execute a raw Cypher query against the Neo4j knowledge graph. |
| `/search <text>` | Perform a semantic vector search in Weaviate. |
| `/concept <name>` | Retrieve detailed information about a focused concept. |

## 🔒 Security

*   **Whitelist Enforced**: The bot checks every incoming message against `ALLOWED_TELEGRAM_USERS`. Unauthorized users receive a rejection message, and their access attempt is logged security auditing.
*   **No Public Access by Default**: Unless you leave `ALLOWED_TELEGRAM_USERS` empty (which triggers a warning) or explicitly add "everyone", the bot is private to you.
*   **Environment Variables**: Sensitive tokens are never hardcoded and are managed via `.env`.

## 🧠 How it Works

1.  **Message Received**: The bot polls the Telegram API for new messages.
2.  **Auth Check**: It verifies the sender against the whitelist.
3.  **Chat API Call**: Valid messages are forwarded to the HDN `chatURL` (`http://localhost:8080/api/v1/chat`).
4.  **AGI Processing**:
    *   The HDN processes the text, potentially using the **Conversational Layer** to understand intent.
    *   If **Thinking Mode** is on, the AGI generates a reasoning trace.
    *   The AGI may decide to call tools (e.g., `scrape_url`, `query_neo4j`) to gather information.
5.  **Response Construction**: The AGI formulates a natural language response (with optional thought summary).
6.  **Reply Sent**: The bot formats this response in Markdown and sends it back to your Telegram chat.
