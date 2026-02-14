# MCP Server Initialization Check

## Overview

The system now automatically verifies MCP server connectivity and tool availability when the interpreter is initialized. This ensures that knowledge base tools are available to the LLM before processing any queries.

## What Gets Checked

When the HDN server starts and creates the interpreter, the composite tool provider automatically:

1. **Discovers MCP Tools**
   - Connects to the MCP server endpoint
   - Retrieves available tools
   - Logs the number of tools discovered

2. **Lists Available Tools**
   - Logs each MCP tool's ID and description
   - Helps verify tools are properly configured

3. **Tests Tool Execution**
   - Executes a test query using `mcp_get_concept` with "Biology"
   - Verifies the tool execution path works
   - Checks that results are returned correctly

## Log Output

When the server starts, you should see logs like:

```
🔍 [MCP-VERIFY] Verifying MCP server connection...
✅ [MCP-VERIFY] MCP server accessible - discovered 4 tools
   - mcp_query_neo4j: Query the Neo4j knowledge graph using Cypher...
   - mcp_search_weaviate: Search the Weaviate vector database...
   - mcp_get_concept: Get a specific concept from the Neo4j knowledge graph...
   - mcp_find_related_concepts: Find concepts related to a given concept...
🧪 [MCP-VERIFY] Testing MCP tool execution...
✅ [MCP-VERIFY] MCP tool execution successful - retrieved 1 results
✅ [MCP-VERIFY] MCP integration verified - LLM can use knowledge base tools
```

## Error Scenarios

### MCP Server Not Accessible

If the MCP server is not reachable, you'll see:

```
❌ [MCP-VERIFY] Failed to discover MCP tools: <error>
⚠️ [MCP-VERIFY] MCP knowledge tools will not be available to LLM
```

**Action**: Check that:
- MCP endpoint is correct (`MCP_ENDPOINT` env var or default `http://localhost:8081/mcp`)
- HDN server is running
- MCP routes are registered

### Tools Discoverable But Execution Fails

If tools are found but execution fails:

```
✅ [MCP-VERIFY] MCP server accessible - discovered 4 tools
⚠️ [MCP-VERIFY] MCP tool execution test failed: <error>
⚠️ [MCP-VERIFY] Tools are discoverable but execution may have issues
```

**Action**: Check that:
- Neo4j is accessible
- Knowledge query endpoint works
- Database has data

### Empty Results

If execution succeeds but returns no data:

```
✅ [MCP-VERIFY] MCP tool execution successful
⚠️ [MCP-VERIFY] MCP tool executed but returned empty results (this may be normal)
```

**Action**: This is normal if the test concept doesn't exist. The tools are working correctly.

## Configuration

The check uses the same configuration as the tool provider:

- `MCP_ENDPOINT`: MCP server endpoint (default: `http://localhost:8081/mcp`)
- `HDN_URL`: HDN server URL (used if MCP_ENDPOINT not set)

## When It Runs

The verification runs:
- **Once** when the interpreter is created
- **At server startup** (when `NewInterpreter` is called)
- **Before** any queries are processed

This ensures issues are caught early rather than during user queries.

## Benefits

1. **Early Detection**: Problems are found at startup, not during user queries
2. **Clear Logging**: Easy to see what tools are available
3. **Confidence**: Know that MCP integration is working before use
4. **Debugging**: Clear error messages help identify configuration issues

## Manual Verification

If you want to manually verify MCP connectivity:

```bash
# Check MCP tools are available
curl -X POST http://localhost:8081/mcp \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}' | jq '.result.tools'

# Test tool execution
curl -X POST http://localhost:8081/mcp \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "id": 2,
    "method": "tools/call",
    "params": {
      "name": "get_concept",
      "arguments": {"name": "Biology"}
    }
  }' | jq '.result'
```

## Integration with Composite Tool Provider

The verification is part of `NewCompositeToolProvider()`, which means:
- It runs automatically when the interpreter is created
- No additional configuration needed
- Works with both HDN and MCP tool providers
- Non-blocking: if MCP fails, HDN tools still work









