# Agent Configuration and Execution Guide

## Overview

This guide explains how to configure and run autonomous agents using Google ADK in the Artificial Mind system.

## Configuration

### 1. Agent Configuration File

Agents are configured in `config/agents.yaml` (or set `AGENTS_CONFIG` environment variable to a custom path).

### 2. Basic Agent Structure

```yaml
agents:
  - id: my_agent
    name: My Agent
    description: What this agent does
    role: Agent Role
    goal: What the agent should achieve
    backstory: |
      Detailed description of the agent's background and capabilities
    
    tools:
      - mcp_read_google_data
      - tool_http_get
    
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
    
    behavior:
      thinking_mode: true
      max_retries: 3
      use_memory: true
      memory_window: 24h
      prefer_tools: true
      tool_timeout: 60s
    
    tasks:
      - id: task_1
        description: What this task does
        expected_output: What should be returned
        tools:
          - mcp_read_google_data
        parameters:
          query: unread
          type: email
```

### 3. Required Fields

- **id**: Unique identifier (required)
- **name**: Display name (required)
- **role**: Agent's role (required)
- **goal**: What the agent should achieve (required)
- **description**: Brief description (optional but recommended)

### 4. Available Tools

Agents can use any of these tools:
- MCP tools: `mcp_read_google_data`, `mcp_query_neo4j`, `mcp_search_weaviate`, etc.
- n8n webhooks: Any configured skill from `n8n_mcp_skills.yaml`
- HDN tools: `tool_http_get`, `tool_html_scraper`, etc.

## Running Agents

### Current Status

The agent registry is implemented and agents can be:
- ✅ **Loaded from configuration** - Agents are loaded at server startup
- ✅ **Listed via API** - `GET /api/v1/agents`
- ✅ **Inspected via API** - `GET /api/v1/agents/{id}`
- ✅ **Execution** - Agents can be executed via `POST /api/v1/agents/{id}/execute`.
- ✅ **Task Priority** - Explicitly defined tasks in YAML are prioritized over automated LLM planning.
- ✅ **Multi-Product Monitoring** - Multiple tasks can be defined per agent for sequential execution. Use task IDs starting with `price_monitor_` (e.g., `price_monitor_asus`, `price_monitor_ebay`) to enable specialized price tracking and history for different URLs.

### Testing Agent Configuration

1. **Start the HDN server:**
   ```bash
   cd hdn
   go run . -domain ../config/domain.json
   ```

2. **Check if agents loaded:**
   ```bash
   curl http://localhost:8081/api/v1/agents | jq
   ```

3. **View agent details:**
   ```bash
   curl http://localhost:8081/api/v1/agents/email_monitor_agent | jq
   ```

### How Execution Works
 
1. **Task Priority**: When an agent is executed, the system checks if it has any `tasks` defined in `agents.yaml`.
   - **If Tasks Exist**: The executor runs each task sequentially using the provided parameters. This ensures reliability and follows your exact instructions.
   - **If No Tasks Exist**: The system uses an LLM to "plan" the execution based on the agent's goal and role.
2. **Tool Routing**: Agent tools are automatically routed to the correct backend (MCP, n8n, or HDN Tools).
3. **History**: Every execution is recorded in Redis with detailed tool logs.

### Manual Execution via API

```bash
curl -X POST http://localhost:8081/api/v1/agents/price_monitor_agent/execute \
  -H "Content-Type: application/json" \
  -d '{"input": "Run monitoring"}'
```

## Example: Email Monitor Agent

The `email_monitor_agent` is configured to:
- Monitor emails using `mcp_read_google_data`
- Trigger on keywords: "check emails", "monitor inbox", "email summary"
- Run scheduled checks every 6 hours
- Use tools to read and categorize emails

## Troubleshooting

### Agents not loading?

1. Check server logs for:
   ```
   ✅ [AGENT-REGISTRY] Successfully loaded agents from configuration
   ```
   or
   ```
   ⚠️ [AGENT-REGISTRY] Failed to load agents from configuration: ...
   ```

2. Verify config file path:
   - Default: `config/agents.yaml`
   - Override: Set `AGENTS_CONFIG` environment variable

3. Validate YAML syntax:
   ```bash
   python3 -c "import yaml; yaml.safe_load(open('config/agents.yaml'))"
   ```

### Agent not found?

- Check agent ID matches exactly (case-sensitive)
- Verify agent is in the `agents.yaml` file
- Restart the HDN server after config changes

