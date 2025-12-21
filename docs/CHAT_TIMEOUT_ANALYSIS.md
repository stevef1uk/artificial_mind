# Chat Timeout Analysis - "What is science?"

## Issue Summary
The chain of thought chat query "what is science" is timing out.

## Log Analysis (from /tmp/hdn_server.log)

### Request Flow (22:53:41)
1. ✅ **Intent Parsing**: Successfully parsed as "query" type
2. ✅ **Concept Extraction**: Correctly extracted "science" from "What is science?"
3. ✅ **Query Simplification**: Created direct query: "Query your knowledge base about 'science'. Use the mcp_get_concept tool with name='science' and domain='General' to retrieve information."
4. ✅ **Tool Provider**: Retrieved 20 tools (16 regular + 4 MCP tools including mcp_get_concept)
5. ⏳ **LLM Request Queued**: "🔒 [LLM] Waiting for LLM request slot..."

### Root Cause

**LLM Request Queue Backlog:**
- The request is waiting for an LLM slot (max 2 concurrent with throttling)
- Many background FSM/autonomy tasks are also competing for LLM slots:
  - Knowledge assessment tasks
  - Hypothesis rating tasks
  - Curiosity goal generation
  - Learning evaluation tasks

**Timeline:**
- 22:53:41: Request queued for LLM slot
- Multiple other LLM requests already in flight
- Request likely times out (120s/180s) before getting a slot or completing

### Issues Identified

1. **Too Many Background Tasks**: FSM autonomy is generating many LLM requests that compete with user chat requests
2. **LLM Throttling Working**: The throttling (max 2 concurrent) is working, but causing queuing
3. **No Priority for User Requests**: Chat requests don't get priority over background tasks
4. **Timeout Too Short**: With queuing, 2-3 minutes may not be enough

## Solutions

### Immediate Fix
1. **Reduce Background Task Load**: Disable or reduce FSM autonomy temporarily
   ```bash
   # In .env or environment
   FSM_AUTONOMY=false
   ```

2. **Increase Chat Timeout**: Already done (3 minutes), but may need more if queuing is severe

3. **Add Priority Queue**: Give user chat requests priority over background tasks

### Long-term Fix
1. **Separate LLM Pools**: 
   - User requests: 2 slots
   - Background tasks: 1 slot (or separate pool)

2. **Better Queue Management**:
   - Priority queue for user requests
   - Background tasks can wait longer

3. **Reduce Background Activity**:
   - Lower FSM bootstrap rates (already done)
   - Reduce autonomy frequency
   - Batch background LLM calls

## Current Status

The chat request is correctly processed but times out while waiting for an LLM slot due to:
- High background task load
- LLM throttling (max 2 concurrent)
- No priority for user requests

## Recommended Actions

1. **Temporarily disable autonomy** to test:
   ```bash
   export FSM_AUTONOMY=false
   # Restart FSM server
   ```

2. **Monitor LLM queue depth** - add logging to see how many requests are queued

3. **Consider increasing LLM_MAX_CONCURRENT_REQUESTS** to 3 if GPU can handle it

4. **Implement priority queue** for user vs background requests

