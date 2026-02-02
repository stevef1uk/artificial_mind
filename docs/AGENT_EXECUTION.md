# Agent Execution Guide

## ✅ Implementation Complete

Agent execution is now implemented! Agents can:
- ✅ Load from configuration
- ✅ Use MCP tools (via `mcp_*` tools)
- ✅ Use n8n webhooks (via configured skills)
- ✅ Use HDN tools (via `tool_*` tools)
- ✅ Execute tasks sequentially
- ✅ Return results via API

## 🚀 How to Execute an Agent

### 1. Start the HDN Server

```bash
cd hdn
go run . -domain ../config/domain.json
```

Verify agents loaded:
```
✅ [AGENT-REGISTRY] Successfully loaded agents from configuration
✅ [AGENT-REGISTRY] Registered agent: email_monitor_agent (Email Monitoring Specialist)
```

### 2. Execute an Agent via API

```bash
curl -X POST http://localhost:8081/api/v1/agents/email_monitor_agent/execute \
  -H "Content-Type: application/json" \
  -d '{"input": "Check for unread emails"}' | jq
```

### 3. Example Response

```json
{
  "agent_id": "email_monitor_agent",
  "input": "Check for unread emails",
  "results": [
    {
      "results": [
        {
          "id": "email123",
          "subject": "Test Email",
          "from": "sender@example.com"
        }
      ]
    }
  ],
  "duration": "1.234s",
  "tool_calls": [
    {
      "tool_id": "mcp_read_google_data",
      "params": {
        "query": "unread",
        "type": "email",
        "limit": 50
      },
      "result": {...},
      "duration": 1234000000
    }
  ]
}
```

## 🔧 How It Works

### Agent Execution Flow

1. **Agent Registry** loads agents from `config/agents.yaml`
2. **Tool Adapters** connect agent tools to:
   - MCP tools (`mcp_read_google_data`, etc.)
   - n8n webhooks (configured skills)
   - HDN tools (`tool_http_get`, etc.)
3. **Agent Executor** runs agent tasks sequentially
4. **Results** are returned with tool call details

### Tool Integration

- **MCP Tools**: Automatically routed to `MCPKnowledgeServer.callTool()`
- **n8n Webhooks**: Executed via `DynamicSkillRegistry.ExecuteSkill()`
- **HDN Tools**: Executed via `APIServer.executeToolDirect()`

## 📋 Example Agent Configuration

```yaml
agents:
  - id: email_monitor_agent
    name: Email Monitor Agent
    role: Email Monitoring Specialist
    goal: Monitor inbox for important emails
    tools:
      - mcp_read_google_data  # MCP tool
      - tool_http_get          # HDN tool
    tasks:
      - id: check_unread_emails
        description: Check for unread emails
        tools:
          - mcp_read_google_data
        parameters:
          query: unread
          type: email
          limit: 50
```

## 🧪 Testing

### Test Agent Execution

```bash
# Execute the email monitor agent
curl -X POST http://localhost:8081/api/v1/agents/email_monitor_agent/execute \
  -H "Content-Type: application/json" \
  -d '{"input": "Check emails"}' | jq
```

### Check Agent Status

```bash
# List all agents
curl http://localhost:8081/api/v1/agents | jq

# Get agent details
curl http://localhost:8081/api/v1/agents/email_monitor_agent | jq
```

## 🔮 Next Steps

- **LLM Integration**: Use LLM to select which tasks to execute based on input
- **Scheduled Execution**: Implement cron-based triggers
- **Event-Based Triggers**: Auto-execute on user requests or goals
- **Full ADK Integration**: Use ADK's runtime for more sophisticated agent behavior

## 📚 Related Documentation

- `docs/AGENT_CONFIGURATION_GUIDE.md` - Full configuration reference
- `docs/QUICK_START_AGENTS.md` - Quick start guide
- `docs/ADK_AGENT_INTEGRATION.md` - ADK integration details

