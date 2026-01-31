#!/bin/bash
export TELEGRAM_BOT_TOKEN="8545418305:AAFPE3FFH2chFAVxNH-nNQrTO_X5lKOOGys"
export MCP_SERVER_URL="http://localhost:8081/mcp"
export CHAT_SERVER_URL="http://localhost:8080/api/v1/chat"

cd "$(dirname "$0")"
./telegram-bot
