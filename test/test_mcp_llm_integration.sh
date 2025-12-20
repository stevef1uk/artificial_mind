#!/bin/bash
# Test script to verify MCP knowledge tools are available to the LLM

set -e

HDN_URL="${HDN_URL:-http://localhost:8081}"
MCP_ENDPOINT="${MCP_ENDPOINT:-${HDN_URL}/mcp}"

echo "🧪 Testing MCP Knowledge Server Integration with LLM"
echo "=================================================="
echo "HDN URL: $HDN_URL"
echo "MCP Endpoint: $MCP_ENDPOINT"
echo ""

# Test 1: Verify MCP tools are discoverable
echo "Test 1: Checking if MCP tools are available..."
TOOLS_RESPONSE=$(curl -s -X POST "$MCP_ENDPOINT" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}')

TOOL_COUNT=$(echo "$TOOLS_RESPONSE" | jq -r '.result.tools | length' 2>/dev/null || echo "0")
if [ "$TOOL_COUNT" -gt "0" ]; then
  echo "✅ MCP server has $TOOL_COUNT tools available"
  echo "$TOOLS_RESPONSE" | jq -r '.result.tools[] | "  - \(.name): \(.description)"' 2>/dev/null || true
else
  echo "❌ No tools found in MCP server"
  exit 1
fi

echo ""

# Test 2: Test that interpreter can discover tools (via API)
echo "Test 2: Testing interpreter tool discovery..."
INTERPRET_URL="${HDN_URL}/api/v1/interpret/execute"
TEST_QUERY="What tools are available for querying knowledge?"

echo "Sending test query: '$TEST_QUERY'"
INTERPRET_RESPONSE=$(curl -s -X POST "$INTERPRET_URL" \
  -H "Content-Type: application/json" \
  -d "{
    \"input\": \"$TEST_QUERY\",
    \"session_id\": \"test_mcp_$(date +%s)\"
  }" || echo "{}")

echo "Response received (checking for tool mentions)..."
if echo "$INTERPRET_RESPONSE" | grep -qi "mcp_\|query_neo4j\|get_concept"; then
  echo "✅ LLM appears to be aware of MCP tools"
else
  echo "⚠️  LLM response doesn't mention MCP tools (may need to ask a knowledge query)"
  echo "Response preview: $(echo "$INTERPRET_RESPONSE" | head -c 200)"
fi

echo ""

# Test 3: Test a knowledge query that should trigger MCP tool usage
echo "Test 3: Testing knowledge query that should use MCP tools..."
KNOWLEDGE_QUERY="What do you know about Biology? Query your knowledge base."

echo "Sending knowledge query: '$KNOWLEDGE_QUERY'"
KNOWLEDGE_RESPONSE=$(curl -s -X POST "$INTERPRET_URL" \
  -H "Content-Type: application/json" \
  -d "{
    \"input\": \"$KNOWLEDGE_QUERY\",
    \"session_id\": \"test_knowledge_$(date +%s)\"
  }" || echo "{}")

echo "Response received..."
if echo "$KNOWLEDGE_RESPONSE" | grep -qi "biology\|concept\|neo4j\|knowledge"; then
  echo "✅ Knowledge query processed (may have used MCP tools)"
else
  echo "⚠️  Response doesn't show knowledge base usage"
fi
echo "Response preview: $(echo "$KNOWLEDGE_RESPONSE" | head -c 300)"

echo ""

# Test 4: Direct MCP tool execution test
echo "Test 4: Testing direct MCP tool execution..."
MCP_TOOL_RESPONSE=$(curl -s -X POST "$MCP_ENDPOINT" \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "id": 2,
    "method": "tools/call",
    "params": {
      "name": "get_concept",
      "arguments": {
        "name": "Biology"
      }
    }
  }')

if echo "$MCP_TOOL_RESPONSE" | jq -e '.result' > /dev/null 2>&1; then
  CONCEPT_NAME=$(echo "$MCP_TOOL_RESPONSE" | jq -r '.result.results[0].c.Props.name' 2>/dev/null || echo "")
  if [ "$CONCEPT_NAME" = "Biology" ]; then
    echo "✅ MCP tool execution successful - retrieved concept: $CONCEPT_NAME"
  else
    echo "⚠️  MCP tool executed but unexpected result"
  fi
else
  echo "❌ MCP tool execution failed"
  echo "$MCP_TOOL_RESPONSE" | jq '.' 2>/dev/null || echo "$MCP_TOOL_RESPONSE"
fi

echo ""
echo "=================================================="
echo "✅ Integration test complete!"
echo ""
echo "To verify LLM can use MCP tools:"
echo "1. Ask a question that requires knowledge: 'What is Biology?'"
echo "2. Check the response - it should query the knowledge base"
echo "3. Look for tool calls in the interpreter logs"

