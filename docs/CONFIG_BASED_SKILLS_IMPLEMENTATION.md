# Configuration-Based n8n/MCP Skills - Implementation Summary

## Overview

This implementation adds a configuration-based system for adding new Skills via n8n workflows and MCP servers, eliminating the need for code changes when adding new integrations.

## Files Created

1. **`hdn/config_skill_loader.go`**
   - Loads skill configurations from YAML/JSON files
   - Supports environment variable expansion
   - Validates skill configurations

2. **`hdn/n8n_webhook_handler.go`**
   - Generic handler for n8n webhook-based skills
   - Supports template-based request payloads
   - Handles authentication (header, bearer, basic)
   - Configurable TLS settings
   - Response parsing with data extraction

3. **`hdn/dynamic_skill_registry.go`**
   - Manages dynamically loaded skills
   - Converts skill configs to MCP tool format
   - Routes skill execution to appropriate handlers

4. **`config/n8n_mcp_skills.yaml.example`**
   - Example configuration file
   - Shows how to configure n8n webhook skills

5. **`docs/CONFIG_BASED_N8N_MCP_SKILLS.md`**
   - Design document and architecture overview

## Integration Points

### MCP Knowledge Server Updates

1. **Added `skillRegistry` field** to `MCPKnowledgeServer` struct
2. **Updated `NewMCPKnowledgeServer()`** to initialize and load skills from config
3. **Updated `listTools()`** to include configured skills (with duplicate detection)
4. **Updated `callTool()`** to route configured skills to registry first, then fall back to hardcoded tools

## Configuration File Format

Skills are defined in `n8n_mcp_skills.yaml` (or JSON):

```yaml
skills:
  - id: read_google_data
    name: Read Google Workspace Data
    description: Read emails or calendar events
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
      emails_key: emails
    timeout: 60s
```

## How It Works

1. **On Startup:**
   - MCP Knowledge Server initializes the skill registry
   - Registry loads skills from `n8n_mcp_skills.yaml` (or path from `N8N_MCP_SKILLS_CONFIG` env var)
   - Skills are validated and handlers are created

2. **Tool Listing:**
   - `listTools()` includes both hardcoded and configured skills
   - Configured skills are converted to MCP tool format
   - Duplicates are detected (hardcoded takes precedence)

3. **Tool Execution:**
   - `callTool()` first checks the skill registry
   - If found, routes to the configured handler
   - Otherwise, falls back to hardcoded tools

4. **n8n Webhook Execution:**
   - Handler builds request payload from template
   - Adds authentication headers
   - Makes HTTP request with configured TLS settings
   - Parses response and extracts data based on configuration

## Benefits

1. **No Code Changes**: Add new skills by editing YAML/JSON
2. **Faster Iteration**: Test new workflows without rebuilding
3. **Easier Maintenance**: All skill definitions in one place
4. **Flexibility**: Support different auth methods, payload formats, etc.
5. **Backward Compatible**: Existing hardcoded tools still work

## Migration Path

### Phase 1: Add Configuration (Current)
- ✅ Configuration loader
- ✅ Generic n8n webhook handler
- ✅ Dynamic skill registry
- ✅ Integration with MCP knowledge server

### Phase 2: Migrate Existing Tool
- Move `read_google_data` to configuration
- Keep hardcoded version as fallback
- Test both paths work

### Phase 3: Add More Skills
- Add new n8n workflows via configuration
- Support additional skill types (MCP tools, HTTP APIs, etc.)

### Phase 4: Optional Cleanup
- Remove hardcoded tools (if desired)
- Keep only configuration-based approach

## Usage

1. **Create configuration file:**
   ```bash
   cp config/n8n_mcp_skills.yaml.example config/n8n_mcp_skills.yaml
   ```

2. **Edit configuration:**
   - Add your n8n webhook URL
   - Configure authentication
   - Define input schema
   - Set response parsing rules

3. **Set environment variables:**
   ```bash
   export N8N_WEBHOOK_URL=https://your-n8n-instance.com/webhook/xxx
   export N8N_WEBHOOK_SECRET=your-secret
   ```

4. **Restart HDN server:**
   - Skills are loaded automatically on startup
   - Check logs for "✅ [SKILL-REGISTRY] Registered skill" messages

## Template Syntax

The `payload_template` uses Go template syntax:

- `{{.field}}` - Insert field value
- `{{.field | toJson}}` - Convert to JSON (needs custom function)
- Numbers are output as-is (float64 from JSON will work)

**Note:** For complex JSON structures, you may need to add custom template functions.

## Known Limitations

1. **Template Functions**: Limited template functions (may need to add custom ones for complex JSON)
2. **Response Parsing**: Currently supports simple key extraction (emails_key, results_key)
3. **Error Handling**: Basic error handling, may need enhancement
4. **Validation**: Input schema validation is basic, may need more robust validation

## Future Enhancements

1. Add more template functions (toJson, toArray, etc.)
2. Support for more response formats (XML, CSV, etc.)
3. Response transformation/formatting
4. Request/response logging and debugging
5. Skill versioning and updates
6. Support for MCP tool type (not just n8n webhooks)
7. Skill dependencies and chaining

## Testing

To test the configuration-based approach:

1. Create a test n8n workflow
2. Add it to `n8n_mcp_skills.yaml`
3. Restart HDN server
4. Check logs for skill registration
5. Test tool execution via MCP endpoint

## Example: Adding a New Skill

1. Create n8n workflow with webhook trigger
2. Add to `config/n8n_mcp_skills.yaml`:
   ```yaml
   - id: my_new_skill
     name: My New Skill
     description: Does something cool
     type: n8n_webhook
     endpoint: ${MY_N8N_WEBHOOK_URL}
     # ... rest of config
   ```
3. Set environment variable: `export MY_N8N_WEBHOOK_URL=...`
4. Restart HDN server
5. Skill is now available as `mcp_my_new_skill`

No code changes required! 🎉

