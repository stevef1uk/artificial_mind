# MCP Learning Integration

## Overview

The learning process now uses the MCP (Model Context Protocol) server to access knowledge bases, providing a more standardized and intelligent way to query and discover knowledge during learning.

## What Was Integrated

### 1. **MCP Client Helpers**
- Added `callMCPTool()` method to both `KnowledgeIntegration` and `KnowledgeGrowthEngine`
- Provides a standardized way to call MCP tools (query_neo4j, get_concept, find_related_concepts)
- Includes automatic fallback to direct API calls if MCP is unavailable

### 2. **Knowledge Existence Checking**
- **`knowledgeAlreadyExists()`** now uses MCP `get_concept` tool
- Checks for duplicate knowledge before storing
- Uses MCP `query_neo4j` for broader Cypher-based searches
- More efficient and standardized than direct HTTP calls

### 3. **Domain Concept Retrieval**
- **`getDomainConcepts()`** now uses MCP `query_neo4j` tool
- Retrieves concepts via Cypher queries: `MATCH (c:Concept {domain: 'X'}) RETURN c`
- Falls back to direct API if MCP fails
- Works in both `knowledge_integration.go` and `knowledge_growth.go`

### 4. **Concept Existence Checking**
- **`conceptAlreadyExists()`** in `knowledge_growth.go` now uses MCP
- Uses `get_concept` tool for precise lookups
- Falls back to direct API for compatibility

### 5. **Related Concept Discovery**
- **`enhanceConceptsWithRelated()`** uses MCP `find_related_concepts` tool
- Enhances concepts with related concepts during hypothesis generation
- Provides better context for learning and hypothesis formation
- Discovers relationships automatically via knowledge graph

## Benefits

### 1. **Standardized Interface**
- All knowledge queries go through MCP protocol
- Consistent error handling and response format
- Easier to extend with new MCP tools

### 2. **Better Knowledge Discovery**
- Can use Cypher queries for complex searches
- Automatic relationship discovery via `find_related_concepts`
- More intelligent duplicate detection

### 3. **Improved Learning Context**
- Concepts are enhanced with related concepts during learning
- Better hypothesis generation with relationship context
- More informed knowledge assessment

### 4. **Resilience**
- Automatic fallback to direct API if MCP fails
- No breaking changes to existing functionality
- Graceful degradation

## Implementation Details

### MCP Tools Used

1. **`query_neo4j`**: Execute Cypher queries
   - Used for: Domain concept retrieval, broad knowledge searches
   - Example: `MATCH (c:Concept {domain: 'Math'}) RETURN c LIMIT 50`

2. **`get_concept`**: Get specific concept by name
   - Used for: Duplicate checking, precise lookups
   - Example: `get_concept(name: "Biology", domain: "Science")`

3. **`find_related_concepts`**: Find related concepts
   - Used for: Enhancing learning context, relationship discovery
   - Example: `find_related_concepts(concept_name: "Biology", max_depth: 1)`

### Code Changes

#### `fsm/knowledge_integration.go`
- Added `mcpEndpoint` field to `KnowledgeIntegration`
- Added `callMCPTool()` helper method
- Updated `getDomainConcepts()` to use MCP
- Updated `knowledgeAlreadyExists()` to use MCP
- Added `enhanceConceptsWithRelated()` method
- Added `getDomainConceptsDirect()` fallback method

#### `fsm/knowledge_growth.go`
- Added `mcpEndpoint` field to `KnowledgeGrowthEngine`
- Added `callMCPTool()` helper method
- Updated `getDomainConcepts()` to use MCP
- Updated `conceptAlreadyExists()` to use MCP
- Added `getDomainConceptsDirect()` fallback method
- Added `conceptAlreadyExistsDirect()` fallback method

## Usage

The integration is automatic - no code changes needed in calling code. The learning process will:

1. **During fact extraction**: Use MCP to check if knowledge already exists
2. **During concept discovery**: Use MCP to check for duplicate concepts
3. **During hypothesis generation**: Use MCP to enhance concepts with related concepts
4. **During knowledge growth**: Use MCP to query domain concepts

## Configuration

The MCP endpoint is automatically configured based on `hdnURL`:
- Default: `http://localhost:8081/mcp`
- Can be overridden by setting `MCP_ENDPOINT` environment variable (if needed)

## Testing

To verify MCP integration is working:

1. **Check logs** for MCP tool calls:
   ```
   ✅ Retrieved X concepts via MCP
   🔍 Found existing concept via MCP: [concept_name]
   🔗 Enhanced concept X with Y related concepts via MCP
   ```

2. **Check fallback behavior**:
   - If MCP is unavailable, should see: `⚠️ MCP query failed, falling back to direct API`
   - System should continue working with direct API

3. **Test knowledge queries**:
   - Learning should still work normally
   - Duplicate detection should be more accurate
   - Concept relationships should be discovered automatically

## Future Enhancements

1. **Weaviate Search Integration**: Use `search_weaviate` tool for semantic similarity searches
2. **Natural Language Queries**: Use natural language parameter in `query_neo4j`
3. **Caching**: Cache MCP results for frequently accessed concepts
4. **Metrics**: Track MCP tool usage and performance

## Troubleshooting

### MCP calls failing
- Check MCP server is running: `curl http://localhost:8081/mcp`
- Verify `hdnURL` is correct
- Check logs for specific error messages

### Fallback to direct API
- This is expected if MCP is unavailable
- System will continue working normally
- Check MCP server logs for issues

### No related concepts found
- This is normal if concepts have no relationships
- MCP will return empty results, system handles gracefully
- Relationships will be discovered as knowledge base grows









