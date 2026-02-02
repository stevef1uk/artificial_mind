# Quick Start: Configuring and Running Agents

## ✅ Current Status

- **Configuration**: ✅ Working - Agents load from `config/agents.yaml`
- **Registry**: ✅ Working - Agents are registered at startup
- **API**: ✅ Working - Can list and inspect agents
- **Execution**: ⏳ Not yet implemented

## 📝 How to Configure an Agent

### 1. Edit the Configuration File

Edit `config/agents.yaml`:

```yaml
agents:
  - id: my_agent
    name: My Agent Name
    description: What this agent does
    role: Agent Role (e.g., "Email Specialist")
    goal: What the agent should achieve
    backstory: |
      Detailed description of the agent's background
    
    tools:
      - mcp_read_google_data  # MCP tools
      - tool_http_get         # HDN tools
    
    capabilities:
      max_iterations: 10
      allow_delegation: false
      verbose: true
    
    triggers:
      events:
        - type: user_request
          keywords:
            - check emails
            - monitor inbox
```

### 2. Restart the HDN Server

After editing the config, restart the server:

```bash
cd hdn
go run . -domain ../config/domain.json
```

Look for these log messages:
```
✅ [AGENT-REGISTRY] Successfully loaded agents from configuration
✅ [AGENT-REGISTRY] Registered agent: my_agent (Agent Role)
```

### 3. Verify Agent is Loaded

```bash
# List all agents
curl http://localhost:8081/api/v1/agents | jq

# Get specific agent details
curl http://localhost:8081/api/v1/agents/my_agent | jq
```

## 🚀 Running Agents (Coming Soon)

Currently, agents are **configured and registered** but **not yet executable**. 

To make them runnable, we need to implement:

1. **Agent Executor** - Execute agents using ADK runtime
2. **Tool Adapters** - Connect agent tools to MCP/n8n tools  
3. **Execution API** - `POST /api/v1/agents/{id}/execute`

## 📋 Example: Email Monitor Agent

The `email_monitor_agent` is already configured:

```bash
# Check if it's loaded
curl http://localhost:8081/api/v1/agents/email_monitor_agent | jq
```

This agent is configured to:
- Use `mcp_read_google_data` tool
- Trigger on keywords: "check emails", "monitor inbox"
- Run scheduled checks every 6 hours (when scheduler is implemented)

## 🔧 Troubleshooting

### Agents not loading?

1. **Check server logs** for:
   ```
   ✅ [AGENT-REGISTRY] Successfully loaded agents from configuration
   ```

2. **Verify config path**:
   - Default: `config/agents.yaml` (relative to project root)
   - When running from `hdn/`, it looks in `../config/agents.yaml`
   - Override: Set `AGENTS_CONFIG` environment variable

3. **Validate YAML**:
   ```bash
   python3 -c "import yaml; yaml.safe_load(open('config/agents.yaml'))"
   ```

### Agent not found in API?

- Restart the server after config changes
- Check agent ID matches exactly (case-sensitive)
- Verify the config file was found (check logs)

## 📚 Next Steps

1. **Test configuration** - Verify agents load correctly
2. **Implement execution** - Add agent executor and tool adapters
3. **Add triggers** - Implement scheduled and event-based triggers
4. **Test execution** - Run agents and verify they use tools correctly

