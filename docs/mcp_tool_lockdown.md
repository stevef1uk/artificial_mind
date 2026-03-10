# MCP Tool Lockdown

## Overview

The MCP Tool Lockdown feature allows developers to restrict specific Model Context Protocol (MCP) tools to **interactive chat channels only**. This prevents the autonomous planner and background agents from using sensitive or interactive-heavy tools automatically without human oversight.

## How it Works

The system implements a simple filtering mechanism during the tool-to-capability registration process. By adding a specific marker to the tool's description, you can signal to the HDN (Hierarchical Decision Network) planner that this tool should not be registered as an autonomous capability.

### The `[chat-only]` Marker

To lock down a tool, include the string `[chat-only]` (case-insensitive) anywhere in the tool's description.

### Implementation Details

1. **Autonomous Planner**: During startup, the `PlannerIntegration` component calls `LoadMCPToolsAsCapabilities`. It now inspects the description of every discovered tool. If the `[chat-only]` marker is found, the tool is **skipped** and not registered as a planner capability.
2. **Background Agents**: Agents defined in `config/agents.yaml` are unaffected unless you explicitly add the locked-down tool to their `tools` list.
3. **Conversational Layer**: The interactive chat (Telegram, Web UI) uses the `FlexibleInterpreter` and `CompositeToolProvider`. These components continue to discover and offer **all** tools to the LLM. The LLM can still "see" and "call" these tools when responding to user requests in a chat session.

## Usage Example

### Defined in `tools_bootstrap.json`:

```json
{
  "id": "mcp_execute_destructive_command",
  "name": "Destructive Command",
  "description": "[chat-only] Executes a command that could delete data. REQUIRES HUMAN OVERSIGHT.",
  "permissions": ["os:write"],
  "safety_level": "high",
  "exec": {
    "type": "cmd",
    "cmd": "rm -rf /tmp/test"
  }
}
```

### Configured via n8n/YAML skills:

```yaml
- name: "sensitive_search"
  description: "Search internal database. [chat-only] results should be reviewed by user."
  # ... other config ...
```

## Benefits

- **Safety**: Prevents the AI from autonomously executing dangerous actions.
- **Precision**: Limits "noisy" or "interactive-only" tools from cluttering the planner's decision space.
- **Hybrid Control**: Keeps tools available for human-in-the-loop workflows while disabling them for fully autonomous background tasks.
