#!/bin/bash
# Test script to verify agent tool adapter fix

echo "🔍 Testing agent tool adapter fix..."
echo ""

# Check if server is running
if ! curl -s http://localhost:8081/health > /dev/null 2>&1; then
    echo "❌ Server is not running. Please start it first."
    exit 1
fi

echo "✅ Server is running"
echo ""

# Check startup logs for skill loading
echo "1. Checking if skills were loaded:"
if grep -q "Loading skills configuration from: ../config/n8n_mcp_skills.yaml" /tmp/hdn_server.log 2>/dev/null; then
    echo "   ✅ Skills config found and loaded"
elif grep -q "Loading skills configuration from:" /tmp/hdn_server.log 2>/dev/null; then
    echo "   ⚠️  Skills config loaded from different path:"
    grep "Loading skills configuration from:" /tmp/hdn_server.log | tail -1
else
    echo "   ❌ No skills config loading found in logs"
fi

# Check how many skills were loaded
SKILL_COUNT=$(grep "SKILL-REGISTRY.*Loaded" /tmp/hdn_server.log 2>/dev/null | tail -1 | grep -oP '\d+(?= skill)' || echo "0")
echo "   Skills loaded: $SKILL_COUNT"
echo ""

# Check agent registry initialization
echo "2. Checking agent registry initialization:"
if grep -q "Skill registry wired up" /tmp/hdn_server.log 2>/dev/null; then
    SKILLS_AVAILABLE=$(grep "Skill registry wired up" /tmp/hdn_server.log | tail -1 | grep -oP '\d+(?= skills available)' || echo "0")
    echo "   ✅ Skill registry wired up with $SKILLS_AVAILABLE skills"
else
    echo "   ⚠️  Skill registry wiring not found in logs"
fi
echo ""

# Test agent execution
echo "3. Testing agent execution:"
RESPONSE=$(curl -s http://localhost:8081/api/v1/agents/email_monitor_agent/execute \
  -H "Content-Type: application/json" \
  -d '{"input": "Check for unread emails"}')

ERROR=$(echo "$RESPONSE" | jq -r '.tool_calls[0].error // "none"')

if [ "$ERROR" = "none" ] || [ -z "$ERROR" ]; then
    echo "   ✅ Agent execution succeeded (no error)"
elif [ "$ERROR" = "unknown tool: read_google_data" ]; then
    echo "   ❌ Still getting 'unknown tool' error - fix may not be applied"
    echo "   Response: $RESPONSE" | jq '.tool_calls[0]'
else
    echo "   ⚠️  Different error: $ERROR"
    echo "   Response: $RESPONSE" | jq '.tool_calls[0]'
fi

echo ""
echo "4. Recent agent-related logs:"
tail -20 /tmp/hdn_server.log 2>/dev/null | grep -E "AGENT-TOOL|AGENT-REGISTRY|n8n skill adapter" | tail -5 || echo "   (No recent agent logs)"

