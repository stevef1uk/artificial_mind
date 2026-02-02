# Google ADK Agent Integration

## Overview

This document describes the integration of Google's Agent Development Kit (ADK) for Go to enable autonomous agents that can use tools and execute tasks. The integration follows a configuration-based approach similar to the n8n/MCP skills system.

## Architecture

### Components

1. **Agent Configuration** (`config/agents.yaml`)
   - Defines agents with roles, goals, and tool assignments
   - Supports triggers (scheduled, event-based)
   - Defines tasks and expected outputs
   - Supports crews (multi-agent coordination)

2. **Agent Config Loader** (`hdn/agent_config_loader.go`)
   - Loads and validates agent configurations from YAML
   - Similar structure to `config_skill_loader.go`

3. **Agent Registry** (`hdn/agent_registry.go`)
   - Manages loaded agents
   - Maps agent IDs to ADK agent instances
   - Handles tool integration

4. **Agent Executor** (`hdn/agent_executor.go`)
   - Executes agents using ADK
   - Integrates with existing MCP tools and n8n webhooks
   - Handles agent workflows and task execution

5. **Trigger System** (`hdn/agent_triggers.go`)
   - Scheduled triggers (cron-based)
   - Event-based triggers (user requests, goals)
   - Autonomous agent activation

## Configuration Format

See `config/agents.yaml.example` for the complete configuration structure.

### Key Features

- **Agent Definition**: Role, goal, backstory, tools
- **Tool Integration**: References to MCP tools and configured skills
- **Triggers**: Schedule (cron) and event-based activation
- **Tasks**: Predefined tasks with expected outputs
- **Crews**: Multi-agent coordination (sequential, hierarchical, consensual)

## Integration with Existing Systems

- **MCP Tools**: Agents can use all MCP tools (query_neo4j, search_weaviate, etc.)
- **n8n Webhooks**: Agents can use configured n8n skills
- **HDN Tools**: Agents can use HDN's tool registry
- **LLM Integration**: Uses existing LLM client infrastructure

## Implementation Status

✅ **Completed:**
1. ✅ Added ADK Go dependency (`google.golang.org/adk`)
2. ✅ Created agent configuration loader (`hdn/agent_config_loader.go`)
3. ✅ Created agent registry (`hdn/agent_registry.go`) with ADK integration
4. ✅ Created YAML configuration structure (`config/agents.yaml.example`)

🔄 **In Progress:**
- Agent executor implementation
- Tool adapter integration (MCP, n8n)

📋 **Next Steps:**
1. Implement agent executor using ADK
2. Create tool adapters to integrate MCP tools and n8n webhooks
3. Add trigger system (scheduled and event-based)
4. Create API endpoints for agent management and execution
5. Integrate with existing LLM client infrastructure
6. Add agent execution monitoring and logging

