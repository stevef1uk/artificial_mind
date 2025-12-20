# MCP Knowledge Server Integration

## Overview

The MCP (Model Context Protocol) Knowledge Server exposes your knowledge bases (Neo4j, Weaviate) as tools that the LLM can discover and use automatically.

## Architecture

1. **MCP Knowledge Server** (`hdn/mcp_knowledge_server.go`)
   - Exposes knowledge bases as MCP tools
   - Endpoints: `/mcp` and `/api/v1/mcp`
   - Tools: `query_neo4j`, `search_weaviate`, `get_concept`, `find_related_concepts`

2. **MCP Tool Provider** (`hdn/interpreter/mcp_tool_provider.go`)
   - Discovers MCP tools and makes them available to the interpreter
   - Converts MCP tool format to interpreter Tool format

3. **Composite Tool Provider** (`hdn/interpreter/composite_tool_provider.go`)
   - Combines HDN tools and MCP tools
   - Routes tool execution to the appropriate provider

4. **Flexible Interpreter** (`hdn/interpreter/flexible_interpreter.go`)
   - Passes available tools to the LLM
   - LLM can choose to use tools when processing queries

## Available MCP Tools

### 1. `mcp_query_neo4j`
Query the Neo4j knowledge graph using Cypher.

**Parameters:**
- `query` (string, required): Cypher query to execute
- `natural_language` (string, optional): Natural language query (will be translated)

**Example:**
```json
{
  "tool_id": "mcp_query_neo4j",
  "parameters": {
    "query": "MATCH (c:Concept) RETURN c LIMIT 5"
  }
}
```

### 2. `mcp_get_concept`
Get a specific concept from Neo4j by name.

**Parameters:**
- `name` (string, required): Name of the concept
- `domain` (string, optional): Domain of the concept

**Example:**
```json
{
  "tool_id": "mcp_get_concept",
  "parameters": {
    "name": "Biology"
  }
}
```

### 3. `mcp_find_related_concepts`
Find concepts related to a given concept.

**Parameters:**
- `concept_name` (string, required): Name of the concept
- `max_depth` (integer, optional): Maximum relationship depth (default: 2)

**Example:**
```json
{
  "tool_id": "mcp_find_related_concepts",
  "parameters": {
    "concept_name": "Biology",
    "max_depth": 1
  }
}
```

### 4. `mcp_search_weaviate`
Search the Weaviate vector database (requires text-to-vector conversion).

**Parameters:**
- `query` (string, required): Text query
- `limit` (integer, optional): Max results (default: 10)
- `collection` (string, optional): Collection name (default: "AgiEpisodes")

## Configuration

### Environment Variables

- `MCP_ENDPOINT`: MCP server endpoint (default: `http://localhost:8081/mcp`)
- `HDN_URL`: HDN server URL (used if MCP_ENDPOINT not set)

### How It Works

1. **Tool Discovery**: When the interpreter processes a query, it:
   - Calls `GetAvailableTools()` on the composite tool provider
   - Composite provider queries both HDN tools API and MCP server
   - All tools are combined and passed to the LLM

2. **Tool Execution**: When the LLM decides to use a tool:
   - If tool ID starts with `mcp_`, it's routed to MCP provider
   - Otherwise, it's routed to HDN tool provider
   - Results are returned to the LLM for processing

3. **LLM Decision**: The LLM sees all available tools in its prompt and can:
   - Choose to use `mcp_query_neo4j` for knowledge queries
   - Choose to use `mcp_get_concept` for specific concept lookups
   - Choose to use regular HDN tools for other operations

## Testing

### Manual Test

1. **Check MCP tools are available:**
```bash
curl -X POST http://localhost:8081/mcp \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}' | jq '.result.tools'
```

2. **Test direct tool execution:**
```bash
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

3. **Test via interpreter (should use MCP tools):**
```bash
curl -X POST http://localhost:8081/api/v1/interpret/execute \
  -H "Content-Type: application/json" \
  -d '{
    "input": "What do you know about Biology? Query your knowledge base.",
    "session_id": "test_session"
  }'
```

### Automated Test

Run the integration test script:
```bash
./test/test_mcp_llm_integration.sh
```

## How to Verify LLM is Using MCP Tools

1. **Check interpreter logs** for tool discovery:
   ```
   ✅ [COMPOSITE-TOOL-PROVIDER] Retrieved X total tools from 2 providers
   ```

2. **Check for tool calls** in LLM responses:
   - Look for `"type": "tool_call"` in interpreter responses
   - Tool ID should start with `mcp_` for knowledge queries

3. **Ask knowledge questions**:
   - "What is Biology?" → Should use `mcp_get_concept`
   - "What concepts are related to Biology?" → Should use `mcp_find_related_concepts`
   - "Query your knowledge base about X" → Should use `mcp_query_neo4j`

## Troubleshooting

### LLM not using MCP tools

1. **Check tool discovery:**
   - Verify MCP endpoint is accessible
   - Check composite tool provider logs
   - Ensure tools are being passed to LLM

2. **Check LLM prompt:**
   - LLM should see MCP tools in the "Available Tools" section
   - Tools should be formatted correctly

3. **Check tool execution:**
   - Verify MCP server is responding
   - Check tool ID format (should be `mcp_*`)

### MCP endpoint not found

- Ensure MCP knowledge server routes are registered
- Check HDN server logs for MCP registration
- Verify `MCP_ENDPOINT` environment variable

## Future Enhancements

1. **Text-to-vector conversion** for Weaviate search
2. **Natural language to Cypher** translation
3. **Tool result caching** for performance
4. **Tool usage analytics** and metrics

