
# Chat Examples for MCP Knowledge Integration

## Simple Examples to Test MCP Knowledge Server

Here are some simple questions you can enter into the Chain of Thought chat box that will trigger the MCP knowledge tools:

### 1. **Get a Concept** (Uses `mcp_get_concept`)
```
What is Biology?
```
This will use the `mcp_get_concept` tool to retrieve the Biology concept from Neo4j.

### 2. **Find Related Concepts** (Uses `mcp_find_related_concepts`)
```
What concepts are related to Biology?
```
This will use the `mcp_find_related_concepts` tool to find related concepts in the knowledge graph.

### 3. **Query Knowledge Base** (Uses `mcp_query_neo4j`)
```
What concepts are in the knowledge base?
```
This will use the `mcp_query_neo4j` tool to execute a Cypher query.

### 4. **Simple Knowledge Question**
```
Tell me about concepts in your knowledge base
```
This should trigger the LLM to use MCP tools to query the knowledge base.

### 5. **More Specific Query**
```
What do you know about Biology and its relationships?
```
This should use both `get_concept` and `find_related_concepts` tools.

## What to Expect

When you send one of these questions:

1. **The LLM will see MCP tools** in its available tools list
2. **The LLM will choose to use an MCP tool** (like `mcp_get_concept`)
3. **The tool will query Neo4j** via the MCP server
4. **Results will be returned** to the LLM
5. **The LLM will generate a response** incorporating the knowledge base results
6. **Thought events will be stored** showing the tool usage
7. **You can view the thoughts** in the Chain of Thought tab after refreshing

## Troubleshooting Network Errors

If you get a "Network Error":

1. **Check HDN server is running**:
   ```bash
   curl http://localhost:8081/api/v1/state
   ```

2. **Check Monitor UI can reach HDN**:
   ```bash
   curl http://localhost:8082/api/status
   ```

3. **Verify HDN_URL environment variable**:
   - Monitor UI should have `HDN_URL=http://localhost:8081` (or your HDN server URL)
   - Check Monitor UI logs for the configured HDN_URL

4. **Check HDN server logs** for:
   - LLM client initialization
   - MCP server startup
   - Conversational API route registration

5. **Verify MCP tools are available**:
   ```bash
   curl -X POST http://localhost:8081/mcp \
     -H "Content-Type: application/json" \
     -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}'
   ```

## Expected Log Output

When MCP tools are used, you should see logs like:
```
🔧 [MCP-TOOL-PROVIDER] Executing MCP tool: mcp_get_concept with parameters: ...
🧠 [MCP-KNOWLEDGE] Executing get_concept for: Biology
✅ [MCP-KNOWLEDGE] Retrieved concept from Neo4j
```

## Viewing the Results

After sending a message:
1. Wait a few seconds for thoughts to be generated
2. Click "🔄 Refresh Thoughts" in the Chain of Thought tab
3. You should see thought events showing:
   - Intent parsing
   - Tool selection (MCP tools)
   - Tool execution results
   - Response generation





