# Avatar Context MCP Tool

## Overview

A new MCP tool has been added to the HDN server to enable searching personal information about Steven Fisher stored in the Weaviate `AvatarContext` collection.

## Tool Details

### Tool Name
`search_avatar_context`

### Description
Search personal information about Steven Fisher (the user). Use this for questions about his work history, education, skills, projects, or any personal background.

### Parameters

| Parameter | Type    | Required | Default | Description |
|-----------|---------|----------|---------|-------------|
| query     | string  | Yes      | -       | Question or search query about Steven Fisher's personal information |
| limit     | integer | No       | 5       | Maximum number of results to return |

### Example Queries

- "Did I work for Accenture?"
- "What companies have I worked for?"
- "What are my skills?"
- "What programming languages do I know?"
- "What is my education background?"

## Usage

### Via MCP JSON-RPC

```bash
curl -X POST http://localhost:8080/api/v1/mcp \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "id": 1,
    "method": "tools/call",
    "params": {
      "name": "search_avatar_context",
      "arguments": {
        "query": "Accenture",
        "limit": 3
      }
    }
  }'
```

### Response Format

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "results": [
      {
        "content": "...",
        "source": "linkedin.pdf",
        "id": "..."
      }
    ],
    "count": 1,
    "query": "Accenture",
    "collection": "AvatarContext"
  }
}
```

## Implementation Details

### Data Source
- **Collection**: `AvatarContext` in Weaviate (running in the `agi` namespace)
- **Schema**: 
  - `content` (text): The actual content/information
  - `source` (text): Source of the information (e.g., "linkedin.pdf", "summary.txt")
- **Embedding Model**: `nomic-embed-text:latest` via Ollama

### Search Method
The tool uses a **two-tier search approach**:

1. **Primary: Vector Search** (Semantic)
   - Queries are converted to embeddings using Ollama's `nomic-embed-text:latest` model
   - Uses Weaviate's `nearVector` search for semantic similarity
   - Returns results with distance scores (lower = more similar)
   - Provides much better understanding of natural language queries

2. **Fallback: Keyword Search**
   - If embedding service is unavailable, falls back to keyword matching
   - Uses Weaviate's `Like` operator on both `content` and `source` fields
   - Ensures the tool still works even if Ollama is down

### Environment Variables
- `WEAVIATE_URL`: URL of the Weaviate instance (defaults to `http://localhost:8080`)
- `OLLAMA_BASE_URL`: URL of the Ollama service for embeddings (defaults to `http://ollama.agi.svc.cluster.local:11434`)
- `OPENAI_BASE_URL`: Alternative URL for embedding service (fallback)

## Testing

Run the test script to verify the tool works:

```bash
./test/test_avatar_context_search.sh
```

## Integration with Conversational Layer

The conversational layer can now automatically use this tool when detecting questions about the user's personal information. The LLM will be able to:

1. Detect when a question is about Steven Fisher's personal background
2. Invoke the `search_avatar_context` tool with appropriate search terms
3. Use the retrieved information to answer the question accurately

## Deployment

To deploy the updated HDN server with this new tool:

1. Build and push the Docker image:
   ```bash
   ./scripts/build-and-push-images.sh
   ```

2. Restart the HDN server pod:
   ```bash
   kubectl rollout restart deployment/hdn-server-rpi58 -n agi
   ```

3. Verify the tool is available:
   ```bash
   curl -X POST http://localhost:30257/api/v1/mcp \
     -H "Content-Type: application/json" \
     -d '{"jsonrpc": "2.0", "id": 1, "method": "tools/list"}' | jq '.result.tools[] | select(.name == "search_avatar_context")'
   ```

## Future Enhancements

1. **Vector Search**: Currently uses keyword matching. Could be enhanced to use semantic vector search for better results.
2. **Filtering**: Add ability to filter by source (e.g., only search LinkedIn data)
3. **Ranking**: Implement relevance scoring to return the most relevant results first
4. **Caching**: Cache frequently asked questions for faster response times
