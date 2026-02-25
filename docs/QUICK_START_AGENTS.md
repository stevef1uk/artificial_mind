# Quick Start: Configuring and Running Agents

## ✅ Current Status

- **Configuration**: ✅ Working - Agents load from `config/agents.yaml`
- **Registry**: ✅ Working - Agents are registered at startup
- **API**: ✅ Working - Can list and inspect agents
- **Execution**: ✅ Working - Agents can be executed via API: `POST /api/v1/agents/{id}/execute`

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

## 🚀 Running Agents
Any registered agent can be executed manually via the REST API:

```bash
# Execute the Price Monitor Agent
curl -X POST http://localhost:8081/api/v1/agents/price_monitor_agent/execute \
  -H "Content-Type: application/json" \
  -d '{"input": "Run price monitoring task"}'
```

## 📋 Example: Price Monitor Agent
The `price_monitor_agent` demonstrates autonomous browser interaction and state tracking:

1. **Configuration**: Uses `mcp_smart_scrape` to navigate and extract dynamic data (e.g., from Amazon).
2. **Monitoring**: A `monitoring` block tracks a specific value (e.g., price) and compares it against a `history_path` JSON file.
3. **Alerting**: If the value decreases, the agent automatically triggers a Telegram alert.

```yaml
# In config/agents.yaml
tasks:
  - id: price_monitor_flow
    tools: [mcp_smart_scrape]
    parameters:
      url: "https://www.amazon.fr/..."
      monitoring:
        type: "value_change"
        field: "price"
        history_path: "config/price_history.json"
```

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

