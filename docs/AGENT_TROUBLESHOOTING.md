# Agent Troubleshooting Guide

## Common Issues

### Issue: "unknown tool: read_google_data"

**Symptom:** Agent execution fails with error "unknown tool: read_google_data"

**Cause:** The server is running old code that doesn't properly route `mcp_read_google_data` to the skill registry.

**Solution:**
1. Rebuild the HDN server:
   ```bash
   cd /home/stevef/dev/artificial_mind
   make build-hdn
   ```

2. Restart the HDN server:
   ```bash
   # Stop the current server (if running via start_servers.sh)
   ./scripts/stop_servers.sh
   
   # Or kill the process manually
   pkill -f hdn-server
   
   # Restart using your preferred method
   ./scripts/start_servers.sh
   # OR
   ./quick-start.sh
   ```

3. Verify agents reloaded:
   ```bash
   curl http://localhost:8081/api/v1/agents | jq
   ```

4. Test agent execution again:
   ```bash
   curl -X POST http://localhost:8081/api/v1/agents/email_monitor_agent/execute \
     -H "Content-Type: application/json" \
     -d '{"input": "Check for unread emails"}' | jq
   ```

### Issue: Agent not found

**Symptom:** API returns "agent not found"

**Solution:**
1. Check if agent config file exists: `config/agents.yaml`
2. Check server logs for agent loading messages
3. Verify agent ID matches exactly (case-sensitive)
4. Restart server after config changes

### Issue: Tool not available

**Symptom:** "tool X not available for agent Y"

**Solution:**
1. Verify tool is listed in agent's `tools:` section
2. Check if tool exists:
   - MCP tools: Check `mcp_knowledge_server.go`
   - n8n skills: Check `config/n8n_mcp_skills.yaml`
   - HDN tools: Check tool registry
3. Ensure tool systems are initialized (MCP server, skill registry)

### Issue: Incorrect Price Extraction

**Symptom:** Agent picks up shipping cost or monthly payment instead of main price.

**Cause:** Multiple numerical values on the page confuse the extraction regex.

**Solution:**
1. Ensure `extractMainPrice` logic is updated in `hdn/agent_executor.go`.
2. Remove specific `extractions: price` regex from `agents.yaml` to trigger the more robust fallback logic.
3. Use the Smart Scrape UI to verify what the "Cleaned HTML" contains.

### Issue: History File Not Updating

**Symptom:** Agent runs but price history remains static or file is empty.

**Cause:** Permission issues or incorrect `history_path` in `agents.yaml`.

**Solution:**
1. Verify `history_path` is relative to project root (e.g., `config/price_history.json`).
2. Ensure the process has write permissions to that directory.
3. Check logs for `[MONITORING] Successfully updated historic state`.

## Verification Steps

### 1. Check Agent Configuration
```bash
# Validate YAML syntax
python3 -c "import yaml; yaml.safe_load(open('config/agents.yaml'))"

# View agent config
cat config/agents.yaml
```

### 2. Check Server Logs
```bash
# If running via start_servers.sh
tail -f /tmp/hdn_server.log

# Look for:
# ✅ [AGENT-REGISTRY] Successfully loaded agents from configuration
# ✅ [AGENT-REGISTRY] Registered agent: email_monitor_agent
```

### 3. Test Agent API
```bash
# List agents
curl http://localhost:8081/api/v1/agents | jq

# Get agent details
curl http://localhost:8081/api/v1/agents/email_monitor_agent | jq

# Execute agent
curl -X POST http://localhost:8081/api/v1/agents/email_monitor_agent/execute \
  -H "Content-Type: application/json" \
  -d '{"input": "test"}' | jq
```

## Quick Fixes

### Rebuild and Restart
```bash
cd /home/stevef/dev/artificial_mind
make build-hdn
pkill -f hdn-server
./scripts/start_servers.sh
```

### Check Tool Availability
```bash
# List all tools
curl http://localhost:8081/api/v1/tools | jq '.tools[] | .id'

# Check for specific tool
curl http://localhost:8081/api/v1/tools | jq '.tools[] | select(.id == "mcp_read_google_data")'
```

