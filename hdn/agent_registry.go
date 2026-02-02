package main

import (
	"context"
	"fmt"
	"iter"
	"log"
	"strings"
	"sync"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/session"
)

// AgentRegistry manages configured agents using Google ADK
type AgentRegistry struct {
	agents             map[string]*AgentInstance
	crews              map[string]*CrewInstance
	config             *AgentsConfig
	mutex              sync.RWMutex
	mcpKnowledgeServer *MCPKnowledgeServer   // For MCP tools
	skillRegistry      *DynamicSkillRegistry // For n8n webhooks
	apiServer          *APIServer            // For HDN tools
}

// AgentInstance represents a running agent instance
type AgentInstance struct {
	Config *AgentConfig
	Agent  agent.Agent   // ADK agent instance
	Tools  []ToolAdapter // Adapters to our tool system
}

// CrewInstance represents a crew (group of agents)
type CrewInstance struct {
	Config *CrewConfig
	Agents []*AgentInstance
}

// ToolAdapter adapts our tools (MCP, n8n) to ADK's tool interface
type ToolAdapter struct {
	ToolID  string
	Execute func(ctx context.Context, params map[string]interface{}) (interface{}, error)
}

// NewAgentRegistry creates a new agent registry
func NewAgentRegistry() *AgentRegistry {
	return &AgentRegistry{
		agents: make(map[string]*AgentInstance),
		crews:  make(map[string]*CrewInstance),
	}
}

// SetMCPKnowledgeServer sets the MCP knowledge server for tool access
func (r *AgentRegistry) SetMCPKnowledgeServer(mcp *MCPKnowledgeServer) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.mcpKnowledgeServer = mcp
}

// SetSkillRegistry sets the skill registry for n8n webhooks
func (r *AgentRegistry) SetSkillRegistry(skills *DynamicSkillRegistry) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.skillRegistry = skills
}

// SetAPIServer sets the API server for HDN tools
func (r *AgentRegistry) SetAPIServer(server *APIServer) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.apiServer = server
}

// LoadAgentsFromConfig loads agents from configuration file
func (r *AgentRegistry) LoadAgentsFromConfig(configPath string) error {
	config, err := LoadAgentsConfig(configPath)
	if err != nil {
		return fmt.Errorf("failed to load agents config: %w", err)
	}

	r.mutex.Lock()
	defer r.mutex.Unlock()

	r.config = config

	// Debug: Log skill registry state before loading agents
	if r.skillRegistry != nil {
		skillIDs := r.skillRegistry.GetSkillIDs()
		log.Printf("🔍 [AGENT-REGISTRY] Skill registry available with %d skills: %v", len(skillIDs), skillIDs)
	} else {
		log.Printf("⚠️ [AGENT-REGISTRY] Skill registry is nil when loading agents")
	}

	// Load agents
	for i := range config.Agents {
		agentConfig := &config.Agents[i]
		if err := r.registerAgent(agentConfig); err != nil {
			log.Printf("⚠️ [AGENT-REGISTRY] Failed to register agent %s: %v", agentConfig.ID, err)
			continue
		}
		log.Printf("✅ [AGENT-REGISTRY] Registered agent: %s (%s)", agentConfig.ID, agentConfig.Role)
	}

	// Load crews
	for i := range config.Crews {
		crewConfig := &config.Crews[i]
		if err := r.registerCrew(crewConfig); err != nil {
			log.Printf("⚠️ [AGENT-REGISTRY] Failed to register crew %s: %v", crewConfig.ID, err)
			continue
		}
		log.Printf("✅ [AGENT-REGISTRY] Registered crew: %s with %d agents", crewConfig.ID, len(crewConfig.Agents))
	}

	log.Printf("✅ [AGENT-REGISTRY] Loaded %d agent(s) and %d crew(s) from configuration", len(r.agents), len(r.crews))
	return nil
}

// registerAgent registers a single agent
func (r *AgentRegistry) registerAgent(config *AgentConfig) error {
	// Create ADK agent with configuration
	// Using ADK's agent.New to create a custom agent
	adkAgent, err := agent.New(agent.Config{
		Name:        config.ID,
		Description: config.Description,
		Run: func(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
			// Agent execution logic will be implemented here
			// This will integrate with our tool system
			return func(yield func(*session.Event, error) bool) {
				// Agent execution will yield events
				// For now, placeholder implementation
				// TODO: Implement actual agent logic using tools
			}
		},
	})
	if err != nil {
		return fmt.Errorf("failed to create ADK agent: %w", err)
	}

	// Create tool adapters for this agent's tools
	toolAdapters := make([]ToolAdapter, 0, len(config.Tools))
	for _, toolID := range config.Tools {
		// Debug: Check skill registry state
		if r.skillRegistry != nil {
			skillIDs := r.skillRegistry.GetSkillIDs()
			log.Printf("🔍 [AGENT-REGISTRY] Creating adapter for %s. Available skills: %v", toolID, skillIDs)
		} else {
			log.Printf("⚠️ [AGENT-REGISTRY] Skill registry is nil when creating adapter for %s", toolID)
		}

		adapter, err := r.createToolAdapter(toolID)
		if err != nil {
			log.Printf("⚠️ [AGENT-REGISTRY] Failed to create tool adapter for %s: %v", toolID, err)
			continue
		}
		toolAdapters = append(toolAdapters, adapter)
	}

	instance := &AgentInstance{
		Config: config,
		Agent:  adkAgent,
		Tools:  toolAdapters,
	}

	r.agents[config.ID] = instance
	return nil
}

// createToolAdapter creates an adapter from our tool system to ADK tools
func (r *AgentRegistry) createToolAdapter(toolID string) (ToolAdapter, error) {
	// Determine tool type and create appropriate adapter
	// Check for configured skills first (n8n webhooks) - these can be referenced with or without mcp_ prefix
	if r.skillRegistry != nil {
		// Try exact match first
		if r.skillRegistry.HasSkill(toolID) {
			log.Printf("🔧 [AGENT-REGISTRY] Routing %s to n8n skill adapter (exact match)", toolID)
			return r.createN8NToolAdapter(toolID, toolID) // Preserve original toolID
		}
		// Try without mcp_ prefix
		if strings.HasPrefix(toolID, "mcp_") {
			toolNameWithoutPrefix := strings.TrimPrefix(toolID, "mcp_")
			if r.skillRegistry.HasSkill(toolNameWithoutPrefix) {
				log.Printf("🔧 [AGENT-REGISTRY] Routing %s to n8n skill adapter (without mcp_ prefix: %s)", toolID, toolNameWithoutPrefix)
				// Preserve original toolID (mcp_read_google_data) but use skill name (read_google_data) for execution
				return r.createN8NToolAdapter(toolID, toolNameWithoutPrefix)
			}
			log.Printf("⚠️ [AGENT-REGISTRY] Skill %s not found in registry (checked as %s)", toolID, toolNameWithoutPrefix)
		}
	} else {
		log.Printf("⚠️ [AGENT-REGISTRY] Skill registry is nil when creating adapter for %s", toolID)
	}

	if strings.HasPrefix(toolID, "mcp_") {
		// MCP tool - use MCPKnowledgeServer
		return r.createMCPToolAdapter(toolID)
	} else if strings.HasPrefix(toolID, "n8n_") {
		// n8n webhook - use DynamicSkillRegistry
		skillID := strings.TrimPrefix(toolID, "n8n_")
		return r.createN8NToolAdapter(toolID, skillID)
	} else if strings.HasPrefix(toolID, "tool_") {
		// HDN tool - use APIServer
		return r.createHDNToolAdapter(toolID)
	}

	return ToolAdapter{
		ToolID: toolID,
		Execute: func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
			return nil, fmt.Errorf("unknown tool type: %s", toolID)
		},
	}, nil
}

// createMCPToolAdapter creates an adapter for MCP tools
func (r *AgentRegistry) createMCPToolAdapter(toolID string) (ToolAdapter, error) {
	if r.mcpKnowledgeServer == nil {
		return ToolAdapter{
			ToolID: toolID,
			Execute: func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
				return nil, fmt.Errorf("MCP knowledge server not available")
			},
		}, nil
	}

	// Strip mcp_ prefix to get the actual tool name
	toolName := strings.TrimPrefix(toolID, "mcp_")

	// Check if this is a configured skill (n8n webhook) - these should be handled via skill registry
	// But callTool already checks the skill registry, so we can just pass it through
	// However, we need to ensure the tool name matches the skill ID

	return ToolAdapter{
		ToolID: toolID,
		Execute: func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
			log.Printf("🔧 [AGENT-TOOL] Calling MCP tool: %s (from agent tool ID: %s)", toolName, toolID)

			// callTool will check skill registry first, then fall back to hardcoded tools
			result, err := r.mcpKnowledgeServer.callTool(ctx, toolName, params)
			if err != nil {
				log.Printf("⚠️ [AGENT-TOOL] MCP tool call failed: %v", err)
				// If it failed and we have a skill registry, try direct skill execution
				if r.skillRegistry != nil && r.skillRegistry.HasSkill(toolName) {
					log.Printf("🔄 [AGENT-TOOL] Retrying as configured skill: %s", toolName)
					return r.skillRegistry.ExecuteSkill(ctx, toolName, params)
				}
			}
			return result, err
		},
	}, nil
}

// createN8NToolAdapter creates an adapter for n8n webhook tools
// toolID: The original tool ID (e.g., "mcp_read_google_data") - used for matching
// skillID: The skill ID in the registry (e.g., "read_google_data") - used for execution
func (r *AgentRegistry) createN8NToolAdapter(toolID string, skillID string) (ToolAdapter, error) {
	if r.skillRegistry == nil {
		return ToolAdapter{
			ToolID: toolID, // Preserve original toolID
			Execute: func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
				return nil, fmt.Errorf("skill registry not available")
			},
		}, nil
	}

	if !r.skillRegistry.HasSkill(skillID) {
		return ToolAdapter{
			ToolID: toolID, // Preserve original toolID
			Execute: func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
				return nil, fmt.Errorf("skill %s not found", skillID)
			},
		}, nil
	}

	return ToolAdapter{
		ToolID: toolID, // Preserve original toolID for matching
		Execute: func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
			log.Printf("🔧 [AGENT-TOOL] Calling n8n skill: %s (skill ID: %s)", toolID, skillID)
			return r.skillRegistry.ExecuteSkill(ctx, skillID, params) // Use skillID for execution
		},
	}, nil
}

// createHDNToolAdapter creates an adapter for HDN tools
func (r *AgentRegistry) createHDNToolAdapter(toolID string) (ToolAdapter, error) {
	if r.apiServer == nil {
		return ToolAdapter{
			ToolID: toolID,
			Execute: func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
				return nil, fmt.Errorf("API server not available")
			},
		}, nil
	}

	return ToolAdapter{
		ToolID: toolID,
		Execute: func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
			log.Printf("🔧 [AGENT-TOOL] Calling HDN tool: %s", toolID)
			// Use the API server's tool execution
			return r.apiServer.executeToolDirect(ctx, toolID, params)
		},
	}, nil
}

// registerCrew registers a crew (group of agents)
func (r *AgentRegistry) registerCrew(config *CrewConfig) error {
	agents := make([]*AgentInstance, 0, len(config.Agents))

	for _, agentID := range config.Agents {
		agent, ok := r.agents[agentID]
		if !ok {
			return fmt.Errorf("agent %s not found for crew %s", agentID, config.ID)
		}
		agents = append(agents, agent)
	}

	instance := &CrewInstance{
		Config: config,
		Agents: agents,
	}

	r.crews[config.ID] = instance
	return nil
}

// GetAgent returns an agent by ID
func (r *AgentRegistry) GetAgent(id string) (*AgentInstance, bool) {
	r.mutex.RLock()
	defer r.mutex.RUnlock()
	agent, ok := r.agents[id]
	return agent, ok
}

// GetCrew returns a crew by ID
func (r *AgentRegistry) GetCrew(id string) (*CrewInstance, bool) {
	r.mutex.RLock()
	defer r.mutex.Unlock()
	crew, ok := r.crews[id]
	return crew, ok
}

// ListAgents returns all registered agent IDs
func (r *AgentRegistry) ListAgents() []string {
	r.mutex.RLock()
	defer r.mutex.RUnlock()
	ids := make([]string, 0, len(r.agents))
	for id := range r.agents {
		ids = append(ids, id)
	}
	return ids
}

// ListCrews returns all registered crew IDs
func (r *AgentRegistry) ListCrews() []string {
	r.mutex.RLock()
	defer r.mutex.RUnlock()
	ids := make([]string, 0, len(r.crews))
	for id := range r.crews {
		ids = append(ids, id)
	}
	return ids
}
