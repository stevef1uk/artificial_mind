# Testing the LLM-Based Tool Creation Agent

## Prerequisites

1. **Rebuild and redeploy** the HDN server with the new tool creation agent code
2. Ensure LLM client is configured and working
3. Ensure HDN base URL is set correctly (for tool registration)

## Testing Steps

### 1. Rebuild and Deploy

```bash
# Build the HDN server
cd hdn
go build -o bin/hdn-server .

# Or if using Docker/Kubernetes:
# Rebuild the Docker image and redeploy
```

### 2. Execute a Task That Should Generate Reusable Code

The tool creation agent will evaluate successful code executions. Use tasks that generate general-purpose, reusable code:

**Example 1: JSON Parser/Transformer**
```bash
curl -X POST http://localhost:8080/api/v1/intelligent/execute \
  -H "Content-Type: application/json" \
  -d '{
    "task_name": "ParseJSONData",
    "description": "Create a Python function that parses JSON data and transforms it by extracting specific fields and normalizing the structure",
    "context": {
      "input": "{\"name\": \"test\", \"value\": 123}"
    },
    "language": "python",
    "force_regenerate": false,
    "max_retries": 2,
    "timeout": 60
  }'
```

**Example 2: HTTP Client with Retry**
```bash
curl -X POST http://localhost:8080/api/v1/intelligent/execute \
  -H "Content-Type: application/json" \
  -d '{
    "task_name": "HTTPClient",
    "description": "Create a Python function that makes HTTP GET requests with retry logic and error handling",
    "context": {
      "url": "https://api.example.com/data"
    },
    "language": "python",
    "force_regenerate": false,
    "max_retries": 2,
    "timeout": 60
  }'
```

**Example 3: Data Transformer**
```bash
curl -X POST http://localhost:8080/api/v1/intelligent/execute \
  -H "Content-Type: application/json" \
  -d '{
    "task_name": "TransformData",
    "description": "Create a Python function that transforms data structures, normalizes formats, and handles edge cases",
    "context": {},
    "language": "python",
    "force_regenerate": false,
    "max_retries": 2,
    "timeout": 60
  }'
```

### 3. Monitor Logs for Tool Creation Activity

**Local Testing:**
```bash
tail -f /path/to/hdn-server.log | grep TOOL-CREATOR
```

**Kubernetes:**
```bash
kubectl logs -n agi deployment/hdn-server-rpi58 --tail=100 -f | grep TOOL-CREATOR
```

**What to Look For:**

1. **LLM Evaluation:**
   ```
   🔍 [TOOL-CREATOR] LLM evaluation...
   ✅ [TOOL-CREATOR] LLM recommends tool creation: [reason]
   ```
   OR
   ```
   🔍 [TOOL-CREATOR] LLM does not recommend tool creation: [reason]
   ```

2. **Tool Registration:**
   ```
   ✅ [TOOL-CREATOR] Successfully created and registered tool [tool_id] from successful execution
   ```

3. **Errors (if any):**
   ```
   ⚠️ [TOOL-CREATOR] LLM evaluation failed: [error]
   ⚠️ [TOOL-CREATOR] Failed to register tool [tool_id]: [error]
   ```

### 4. Verify Tool Was Created

```bash
curl -X GET http://localhost:8080/api/v1/tools | jq '.tools[] | {id: .id, name: .name, description: .description}'
```

Look for tools with IDs like:
- `tool_parsejsondata`
- `tool_python_util_*`
- `tool_*_util_*`

### 5. Test the Created Tool

If a tool was created, test it:

```bash
curl -X POST http://localhost:8080/api/v1/tools/[tool_id]/invoke \
  -H "Content-Type: application/json" \
  -d '{
    "params": {
      "input": "{\"test\": \"data\"}"
    }
  }'
```

## Expected Behavior

### When Code Should Become a Tool:
- Code is general/reusable (not task-specific)
- Code aligns with system objectives (autonomous execution, knowledge management, etc.)
- Code has clear inputs/outputs
- Code represents a meaningful capability

### When Code Should NOT Become a Tool:
- Code is too task-specific (e.g., "generate first 10 primes")
- Code is trivial (simple print statements)
- Code doesn't align with system objectives
- Code is too short or lacks structure

## Troubleshooting

### No Tool Creation Activity in Logs

1. **Check if execution was successful:**
   ```bash
   kubectl logs -n agi deployment/hdn-server-rpi58 --tail=200 | grep "✅.*execution.*success"
   ```

2. **Check if LLM client is available:**
   ```bash
   kubectl logs -n agi deployment/hdn-server-rpi58 --tail=100 | grep "LLM client not available"
   ```

3. **Check if code meets minimum requirements:**
   - Code must be at least 100 characters
   - Code must have been successfully executed

### LLM Evaluation Fails

1. **Check LLM connectivity:**
   ```bash
   kubectl logs -n agi deployment/hdn-server-rpi58 --tail=100 | grep "LLM.*failed"
   ```

2. **Check if background LLM is disabled:**
   - Environment variable: `DISABLE_BACKGROUND_LLM=1` will prevent tool creation
   - Tool creation uses `PriorityLow` which may be rejected if background LLM is disabled

### Tool Registration Fails

1. **Check HDN base URL:**
   - Should be set in `IntelligentExecutor` initialization
   - Default: `http://localhost:8080`

2. **Check API endpoint:**
   ```bash
   curl -X GET http://localhost:8080/api/v1/tools
   ```

## Test Script

Use the provided test script:

```bash
./test_tool_creation_agent.sh
```

This script will:
1. Execute tasks that should generate reusable code
2. Check for tool creation in logs
3. List created tools
4. Provide next steps

## Success Criteria

✅ Tool creation agent is triggered after successful code execution
✅ LLM evaluates code and provides reasoning
✅ Tools are created for general-purpose, reusable code
✅ Tools are registered and available via `/api/v1/tools`
✅ Created tools can be invoked successfully

