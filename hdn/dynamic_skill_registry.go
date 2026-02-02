package main

import (
	"context"
	"fmt"
	"log"
	"strings"
)

// DynamicSkillRegistry manages skills loaded from configuration
type DynamicSkillRegistry struct {
	skills map[string]*SkillConfig
	handlers map[string]SkillHandler
}

// SkillHandler interface for executing skills
type SkillHandler interface {
	Execute(ctx context.Context, args map[string]interface{}) (interface{}, error)
}

// NewDynamicSkillRegistry creates a new dynamic skill registry
func NewDynamicSkillRegistry() *DynamicSkillRegistry {
	return &DynamicSkillRegistry{
		skills:   make(map[string]*SkillConfig),
		handlers: make(map[string]SkillHandler),
	}
}

// LoadSkillsFromConfig loads skills from configuration file
func (r *DynamicSkillRegistry) LoadSkillsFromConfig(configPath string) error {
	config, err := LoadSkillsConfig(configPath)
	if err != nil {
		return fmt.Errorf("failed to load skills config: %w", err)
	}

	for i := range config.Skills {
		skill := &config.Skills[i]
		if err := ValidateSkillConfig(skill); err != nil {
			log.Printf("⚠️ [SKILL-REGISTRY] Skipping invalid skill %s: %v", skill.ID, err)
			continue
		}

		// Create handler based on skill type
		var handler SkillHandler
		switch skill.Type {
		case "n8n_webhook":
			handler = NewN8NWebhookHandler(skill)
		default:
			log.Printf("⚠️ [SKILL-REGISTRY] Unknown skill type '%s' for skill %s, skipping", skill.Type, skill.ID)
			continue
		}

		r.skills[skill.ID] = skill
		r.handlers[skill.ID] = handler
		log.Printf("✅ [SKILL-REGISTRY] Registered skill: %s (%s), endpoint: %s", skill.ID, skill.Type, skill.Endpoint)
	}

	log.Printf("✅ [SKILL-REGISTRY] Loaded %d skill(s) from configuration", len(r.skills))
	return nil
}

// GetSkill returns a skill configuration by ID
func (r *DynamicSkillRegistry) GetSkill(id string) (*SkillConfig, bool) {
	skill, ok := r.skills[id]
	return skill, ok
}

// ExecuteSkill executes a skill by ID
func (r *DynamicSkillRegistry) ExecuteSkill(ctx context.Context, skillID string, args map[string]interface{}) (interface{}, error) {
	handler, ok := r.handlers[skillID]
	if !ok {
		return nil, fmt.Errorf("skill not found: %s", skillID)
	}

	return handler.Execute(ctx, args)
}

// ListSkills returns all registered skills as MCP tools
func (r *DynamicSkillRegistry) ListSkills() []MCPKnowledgeTool {
	tools := make([]MCPKnowledgeTool, 0, len(r.skills))
	for _, skill := range r.skills {
		// Convert SkillConfig to MCPKnowledgeTool
		// Use skill ID as the tool name (e.g., "read_google_data") to match expected MCP tool naming
		tool := MCPKnowledgeTool{
			Name:        skill.ID, // Use ID, not display name, for MCP tool name
			Description: skill.Description,
			InputSchema: r.buildInputSchema(skill),
		}
		tools = append(tools, tool)
	}
	return tools
}

// buildInputSchema converts skill input schema to MCP format
func (r *DynamicSkillRegistry) buildInputSchema(skill *SkillConfig) map[string]interface{} {
	if skill.InputSchema == nil {
		return map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		}
	}

	// Convert flat input_schema to MCP format
	properties := make(map[string]interface{})
	required := []string{}

	for key, value := range skill.InputSchema {
		if param, ok := value.(map[string]interface{}); ok {
			prop := make(map[string]interface{})
			
			if paramType, ok := param["type"].(string); ok {
				prop["type"] = paramType
			}
			if desc, ok := param["description"].(string); ok {
				prop["description"] = desc
			}
			if def, ok := param["default"]; ok {
				prop["default"] = def
			}
			if enum, ok := param["enum"].([]interface{}); ok {
				prop["enum"] = enum
			}
			if min, ok := param["minimum"].(int); ok {
				prop["minimum"] = min
			}
			if max, ok := param["maximum"].(int); ok {
				prop["maximum"] = max
			}
			if req, ok := param["required"].(bool); ok && req {
				required = append(required, key)
			}

			properties[key] = prop
		}
	}

	schema := map[string]interface{}{
		"type":       "object",
		"properties": properties,
	}

	if len(required) > 0 {
		schema["required"] = required
	}

	return schema
}

// HasSkill checks if a skill is registered
func (r *DynamicSkillRegistry) HasSkill(id string) bool {
	_, ok := r.skills[id]
	return ok
}

// GetSkillIDs returns all registered skill IDs
func (r *DynamicSkillRegistry) GetSkillIDs() []string {
	ids := make([]string, 0, len(r.skills))
	for id := range r.skills {
		ids = append(ids, id)
	}
	return ids
}

// ConvertSkillIDToToolName converts a skill ID to MCP tool name format
func ConvertSkillIDToToolName(skillID string) string {
	// Remove "mcp_" prefix if present, or add it if not
	if strings.HasPrefix(skillID, "mcp_") {
		return strings.TrimPrefix(skillID, "mcp_")
	}
	return skillID
}

// GetPromptHints returns prompt hints for a skill by tool ID (with or without mcp_ prefix)
func (r *DynamicSkillRegistry) GetPromptHints(toolID string) *PromptHintsConfig {
	// Try with mcp_ prefix
	skillID := strings.TrimPrefix(toolID, "mcp_")
	if skill, ok := r.skills[skillID]; ok && skill.PromptHints != nil {
		return skill.PromptHints
	}
	// Try without prefix
	if skill, ok := r.skills[toolID]; ok && skill.PromptHints != nil {
		return skill.PromptHints
	}
	return nil
}

// GetAllPromptHints returns a map of tool ID to prompt hints for all configured skills
func (r *DynamicSkillRegistry) GetAllPromptHints() map[string]*PromptHintsConfig {
	hints := make(map[string]*PromptHintsConfig)
	for id, skill := range r.skills {
		if skill.PromptHints != nil {
			// Store with both mcp_ prefix and without
			hints["mcp_"+id] = skill.PromptHints
			hints[id] = skill.PromptHints
		}
	}
	return hints
}

