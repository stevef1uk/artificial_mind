# Configuration-Based n8n/MCP Skills

## Overview

This document describes a configuration-based approach for adding new Skills via n8n workflows and MCP servers, eliminating the need for code changes when adding new integrations.

## Current State

### MCP Tools (Hardcoded)
- Tools are defined in `hdn/mcp_knowledge_server.go`:
  - `listTools()` returns a hardcoded list
  - `callTool()` uses a hardcoded switch statement
  - Each tool has a dedicated implementation method

### n8n Integration (Hardcoded)
- `read_google_data` tool is hardcoded:
  - Webhook URL from `N8N_WEBHOOK_URL` env var
  - Implementation in `readGoogleWorkspace()` method
  - Tool definition in `listTools()`

## Proposed Solution

### Configuration File Format

Create `config/n8n_mcp_skills.yaml` (or JSON) to define skills:

```yaml
skills:
  - id: read_google_data
    name: Read Google Workspace Data
    description: Read emails or calendar events from Google Workspace via n8n
    type: n8n_webhook
    endpoint: ${N8N_WEBHOOK_URL}
    method: POST
    auth:
      type: header
      header: X-Webhook-Secret
      secret_env: N8N_WEBHOOK_SECRET
    tls:
      skip_verify: true
    request:
      payload_template: |
        {
          "query": "{{.query}}",
          "type": "{{.type}}",
          "limit": {{.limit}}
        }
    input_schema:
      query:
        type: string
        description: Search query (e.g., 'unread', 'recent')
        default: ""
      type:
        type: string
        enum: [email, calendar, all]
        default: email
      limit:
        type: integer
        min: 1
        max: 50
        default: 5
    response:
      format: json
      structure:
        type: object
        results_key: results  # Standard key for results array (n8n workflow should return {"results": [...]})
    timeout: 60s

  - id: send_slack_message
    name: Send Slack Message
    description: Send a message to a Slack channel via n8n
    type: n8n_webhook
    endpoint: ${N8N_SLACK_WEBHOOK_URL}
    method: POST
    auth:
      type: header
      header: Authorization
      secret_env: N8N_SLACK_SECRET
    request:
      payload_template: |
        {
          "channel": "{{.channel}}",
          "message": "{{.message}}"
        }
    input_schema:
      channel:
        type: string
        description: Slack channel name
        required: true
      message:
        type: string
        description: Message text
        required: true

  - id: query_custom_api
    name: Query Custom API
    description: Query a custom API via n8n workflow
    type: n8n_webhook
    endpoint: ${CUSTOM_API_WEBHOOK_URL}
    method: POST
    request:
      payload_template: |
        {
          "action": "{{.action}}",
          "params": {{.params | toJson}}
        }
    input_schema:
      action:
        type: string
        required: true
      params:
        type: object
        description: Additional parameters
```

### Implementation Plan

1. **Configuration Loader** (`hdn/config_skill_loader.go`)
   - Load YAML/JSON configuration file
   - Parse skill definitions
   - Validate configuration

2. **Generic n8n Webhook Handler** (`hdn/n8n_webhook_handler.go`)
   - Generic HTTP client for n8n webhooks
   - Template-based request payload generation
   - Response parsing based on configuration
   - TLS configuration support
   - **Standard Response Format**: All n8n webhooks should return `{"results": [...]}` for consistency

3. **Dynamic Tool Registry** (`hdn/dynamic_skill_registry.go`)
   - Register skills from configuration
   - Map skill IDs to handlers
   - Support both n8n and MCP skill types

4. **Update MCP Knowledge Server**
   - Replace hardcoded `listTools()` with dynamic loading
   - Replace hardcoded `callTool()` switch with registry lookup
   - Keep backward compatibility with existing tools

### Benefits

1. **No Code Changes**: Add new skills by editing configuration
2. **Faster Iteration**: Test new workflows without rebuilding
3. **Easier Maintenance**: All skill definitions in one place
4. **Flexibility**: Support different auth methods, payload formats, etc.
5. **Backward Compatible**: Existing hardcoded tools still work

### Migration Path

1. Phase 1: Add configuration loader alongside existing code
2. Phase 2: Migrate `read_google_data` to configuration
3. Phase 3: Add support for more skill types
4. Phase 4: Migrate remaining hardcoded tools (optional)

## Next Steps

1. Create configuration file structure
2. Implement generic n8n webhook handler
3. Create dynamic skill registry
4. Update MCP knowledge server to use registry
5. Add configuration validation
6. Add documentation and examples

