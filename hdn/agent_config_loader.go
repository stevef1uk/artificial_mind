package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// AgentConfig represents an agent configuration
type AgentConfig struct {
	ID           string             `yaml:"id" json:"id"`
	Name         string             `yaml:"name" json:"name"`
	Description  string             `yaml:"description" json:"description"`
	Role         string             `yaml:"role" json:"role"`
	Goal         string             `yaml:"goal" json:"goal"`
	Backstory    string             `yaml:"backstory" json:"backstory"`
	Instructions []string           `yaml:"instructions,omitempty" json:"instructions,omitempty"`
	Tools        []string           `yaml:"tools" json:"tools"` // List of tool IDs this agent can use
	Capabilities *AgentCapabilities `yaml:"capabilities,omitempty" json:"capabilities,omitempty"`
	Triggers     *AgentTriggers     `yaml:"triggers,omitempty" json:"triggers,omitempty"`
	Behavior     *AgentBehavior     `yaml:"behavior,omitempty" json:"behavior,omitempty"`
	Tasks        []AgentTask        `yaml:"tasks,omitempty" json:"tasks,omitempty"`
}

// AgentCapabilities defines agent capabilities and constraints
type AgentCapabilities struct {
	MaxIterations   int  `yaml:"max_iterations,omitempty" json:"max_iterations,omitempty"`
	AllowDelegation bool `yaml:"allow_delegation,omitempty" json:"allow_delegation,omitempty"`
	Verbose         bool `yaml:"verbose,omitempty" json:"verbose,omitempty"`
}

// AgentTriggers defines when an agent should be activated
type AgentTriggers struct {
	Schedule []ScheduleTrigger `yaml:"schedule,omitempty" json:"schedule,omitempty"`
	Events   []EventTrigger    `yaml:"events,omitempty" json:"events,omitempty"`
}

// ScheduleTrigger defines a scheduled trigger (cron-based)
type ScheduleTrigger struct {
	Cron   string `yaml:"cron" json:"cron"`
	Action string `yaml:"action" json:"action"`
}

// EventTrigger defines an event-based trigger
type EventTrigger struct {
	Type     string   `yaml:"type" json:"type"` // "user_request", "goal", etc.
	Keywords []string `yaml:"keywords,omitempty" json:"keywords,omitempty"`
	GoalType string   `yaml:"goal_type,omitempty" json:"goal_type,omitempty"`
}

// AgentBehavior defines agent behavior configuration
type AgentBehavior struct {
	ThinkingMode   bool   `yaml:"thinking_mode,omitempty" json:"thinking_mode,omitempty"`
	MaxRetries     int    `yaml:"max_retries,omitempty" json:"max_retries,omitempty"`
	ResponseFormat string `yaml:"response_format,omitempty" json:"response_format,omitempty"`
	UseMemory      bool   `yaml:"use_memory,omitempty" json:"use_memory,omitempty"`
	MemoryWindow   string `yaml:"memory_window,omitempty" json:"memory_window,omitempty"` // e.g., "24h"
	PreferTools    bool   `yaml:"prefer_tools,omitempty" json:"prefer_tools,omitempty"`
	ToolTimeout    string `yaml:"tool_timeout,omitempty" json:"tool_timeout,omitempty"` // e.g., "60s"
}

// AgentTask defines a task an agent can perform
type AgentTask struct {
	ID             string                 `yaml:"id" json:"id"`
	Description    string                 `yaml:"description" json:"description"`
	ExpectedOutput string                 `yaml:"expected_output" json:"expected_output"`
	Tools          []string               `yaml:"tools,omitempty" json:"tools,omitempty"`
	Parameters     map[string]interface{} `yaml:"parameters,omitempty" json:"parameters,omitempty"`
}

// CrewConfig represents a crew (group of agents) configuration
type CrewConfig struct {
	ID          string              `yaml:"id" json:"id"`
	Name        string              `yaml:"name" json:"name"`
	Description string              `yaml:"description" json:"description"`
	Agents      []string            `yaml:"agents" json:"agents"` // List of agent IDs
	Process     *CrewProcess        `yaml:"process,omitempty" json:"process,omitempty"`
	Config      *CrewConfigSettings `yaml:"config,omitempty" json:"config,omitempty"`
}

// CrewProcess defines how agents in a crew coordinate
type CrewProcess struct {
	Type string `yaml:"type" json:"type"` // "sequential", "hierarchical", "consensual"
}

// CrewConfigSettings defines crew-level configuration
type CrewConfigSettings struct {
	Verbose       bool `yaml:"verbose,omitempty" json:"verbose,omitempty"`
	MaxIterations int  `yaml:"max_iterations,omitempty" json:"max_iterations,omitempty"`
	Memory        bool `yaml:"memory,omitempty" json:"memory,omitempty"`
}

// AgentsConfig represents the complete agents configuration file
type AgentsConfig struct {
	Agents []AgentConfig `yaml:"agents" json:"agents"`
	Crews  []CrewConfig  `yaml:"crews,omitempty" json:"crews,omitempty"`
}

// LoadAgentsConfig loads agent configurations from a YAML file
func LoadAgentsConfig(configPath string) (*AgentsConfig, error) {
	// Expand environment variables in path
	configPath = os.ExpandEnv(configPath)

	// Try multiple possible locations (including parent directory for when running from hdn/)
	possiblePaths := []string{
		configPath,
		filepath.Join(".", configPath),
		filepath.Join("config", configPath),
		filepath.Join(".", "config", configPath),
		filepath.Join("..", configPath),           // Parent directory
		filepath.Join("..", "config", configPath), // Parent config directory
	}

	var file *os.File
	var err error
	var foundPath string

	for _, path := range possiblePaths {
		file, err = os.Open(path)
		if err == nil {
			foundPath = path
			break
		}
	}

	if file == nil {
		return nil, fmt.Errorf("agents config file not found in any of: %v", possiblePaths)
	}
	defer file.Close()

	log.Printf("📋 [AGENT-CONFIG] Loading agents configuration from: %s", foundPath)

	data, err := io.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Expand environment variables in YAML content
	expandedData := os.ExpandEnv(string(data))

	var config AgentsConfig
	if err := yaml.Unmarshal([]byte(expandedData), &config); err != nil {
		return nil, fmt.Errorf("failed to parse YAML: %w", err)
	}

	// Validate configuration
	if err := ValidateAgentsConfig(&config); err != nil {
		return nil, fmt.Errorf("invalid agents config: %w", err)
	}

	log.Printf("✅ [AGENT-CONFIG] Loaded %d agent(s) and %d crew(s) from configuration", len(config.Agents), len(config.Crews))
	return &config, nil
}

// ValidateAgentsConfig validates the agents configuration
func ValidateAgentsConfig(config *AgentsConfig) error {
	agentIDs := make(map[string]bool)

	for i, agent := range config.Agents {
		if agent.ID == "" {
			return fmt.Errorf("agent[%d]: id is required", i)
		}
		if agent.Name == "" {
			return fmt.Errorf("agent[%d]: name is required", i)
		}
		if agent.Role == "" {
			return fmt.Errorf("agent[%d]: role is required", i)
		}
		if agent.Goal == "" {
			return fmt.Errorf("agent[%d]: goal is required", i)
		}

		// Check for duplicate IDs
		if agentIDs[agent.ID] {
			return fmt.Errorf("duplicate agent id: %s", agent.ID)
		}
		agentIDs[agent.ID] = true

		// Set defaults
		if agent.Capabilities == nil {
			agent.Capabilities = &AgentCapabilities{
				MaxIterations:   10,
				AllowDelegation: false,
				Verbose:         false,
			}
		}
		if agent.Capabilities.MaxIterations <= 0 {
			agent.Capabilities.MaxIterations = 10
		}

		if agent.Behavior == nil {
			agent.Behavior = &AgentBehavior{
				ThinkingMode: false,
				MaxRetries:   3,
				UseMemory:    true,
				MemoryWindow: "24h",
				PreferTools:  true,
				ToolTimeout:  "60s",
			}
		}
	}

	// Validate crews reference valid agents
	for i, crew := range config.Crews {
		if crew.ID == "" {
			return fmt.Errorf("crew[%d]: id is required", i)
		}
		if len(crew.Agents) == 0 {
			return fmt.Errorf("crew[%d]: at least one agent is required", i)
		}

		for _, agentID := range crew.Agents {
			found := false
			for _, agent := range config.Agents {
				if agent.ID == agentID {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("crew[%d]: references unknown agent: %s", i, agentID)
			}
		}
	}

	return nil
}

// SaveAgentsConfig saves agent configurations to a YAML file
func SaveAgentsConfig(configPath string, config *AgentsConfig) error {
	// Expand environment variables in path
	configPath = os.ExpandEnv(configPath)

	data, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal config to YAML: %w", err)
	}

	// Try to find the actual path if LoadAgentsConfig was used
	// (This is a bit simplified, ideally we'd track which path was actually used)
	possiblePaths := []string{
		configPath,
		filepath.Join(".", configPath),
		filepath.Join("config", configPath),
		filepath.Join(".", "config", configPath),
		filepath.Join("..", configPath),
		filepath.Join("..", "config", configPath),
	}

	var targetPath string
	for _, path := range possiblePaths {
		if _, err := os.Stat(path); err == nil {
			targetPath = path
			break
		}
	}

	if targetPath == "" {
		targetPath = configPath // Fallback to provided path
	}

	log.Printf("💾 [AGENT-CONFIG] Saving agents configuration to: %s", targetPath)
	return os.WriteFile(targetPath, data, 0644)
}
