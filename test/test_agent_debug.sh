#!/bin/bash
# Debug script for agent execution

echo "🔍 Checking agent and skill status..."

echo ""
echo "1. Checking if agents are loaded:"
curl -s http://localhost:8081/api/v1/agents | jq '.agents[] | {id, name, tools}'

echo ""
echo "2. Checking server logs for skill registry:"
tail -100 /tmp/hdn_server.log 2>/dev/null | grep -E "SKILL-REGISTRY|MCP-KNOWLEDGE.*skill|AGENT-REGISTRY.*skill" | tail -10

echo ""
echo "3. Testing agent execution:"
curl -X POST http://localhost:8081/api/v1/agents/email_monitor_agent/execute \
  -H "Content-Type: application/json" \
  -d '{"input": "Check for unread emails"}' | jq '.'

echo ""
echo "4. Recent agent-related logs:"
tail -30 /tmp/hdn_server.log 2>/dev/null | grep -E "AGENT" | tail -10

