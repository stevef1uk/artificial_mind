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
- ⏳ **Execution** - Agent execution is not yet implemented

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

### Next Steps for Execution

To actually **run** an agent, we need to implement:

1. **Agent Executor** - Execute agents using ADK's runtime
2. **Tool Adapters** - Connect agent tools to actual MCP/n8n tools
3. **Trigger System** - Handle scheduled and event-based triggers
4. **API Endpoint** - `POST /api/v1/agents/{id}/execute` to manually trigger

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

