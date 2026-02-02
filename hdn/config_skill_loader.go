package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// SkillConfig represents a skill configuration
type SkillConfig struct {
	ID          string                 `yaml:"id" json:"id"`
	Name        string                 `yaml:"name" json:"name"`
	Description string                 `yaml:"description" json:"description"`
	Type        string                 `yaml:"type" json:"type"` // "n8n_webhook", "mcp_tool", etc.
	Endpoint    string                 `yaml:"endpoint" json:"endpoint"`
	Method      string                 `yaml:"method" json:"method"` // GET, POST, etc.
	Auth        *AuthConfig            `yaml:"auth,omitempty" json:"auth,omitempty"`
	TLS         *TLSConfig             `yaml:"tls,omitempty" json:"tls,omitempty"`
	Request     *RequestConfig         `yaml:"request,omitempty" json:"request,omitempty"`
	InputSchema map[string]interface{} `yaml:"input_schema" json:"input_schema"`
	Response    *ResponseConfig        `yaml:"response,omitempty" json:"response,omitempty"`
	Timeout     string                 `yaml:"timeout,omitempty" json:"timeout,omitempty"` // e.g., "60s"
	PromptHints *PromptHintsConfig    `yaml:"prompt_hints,omitempty" json:"prompt_hints,omitempty"` // LLM prompt configuration
}

// PromptHintsConfig defines LLM prompt hints for a skill
type PromptHintsConfig struct {
	Keywords      []string `yaml:"keywords,omitempty" json:"keywords,omitempty"`           // Keywords that trigger this tool
	PromptText    string   `yaml:"prompt_text,omitempty" json:"prompt_text,omitempty"`    // Custom prompt text for this tool
	ForceToolCall bool     `yaml:"force_tool_call,omitempty" json:"force_tool_call,omitempty"` // Force tool call when keywords detected
	AlwaysInclude []string `yaml:"always_include_keywords,omitempty" json:"always_include_keywords,omitempty"` // Keywords that always include this tool
	RejectText    bool     `yaml:"reject_text_response,omitempty" json:"reject_text_response,omitempty"` // Reject text responses when this tool is available
}

// AuthConfig defines authentication for the skill
type AuthConfig struct {
	Type       string `yaml:"type" json:"type"` // "header", "bearer", "basic"
	Header     string `yaml:"header,omitempty" json:"header,omitempty"`
	SecretEnv  string `yaml:"secret_env,omitempty" json:"secret_env,omitempty"`
	BearerEnv  string `yaml:"bearer_env,omitempty" json:"bearer_env,omitempty"`
	BasicUser  string `yaml:"basic_user,omitempty" json:"basic_user,omitempty"`
	BasicPass  string `yaml:"basic_pass,omitempty" json:"basic_pass,omitempty"`
}

// TLSConfig defines TLS settings
type TLSConfig struct {
	SkipVerify bool `yaml:"skip_verify" json:"skip_verify"`
}

// RequestConfig defines request payload configuration
type RequestConfig struct {
	PayloadTemplate string            `yaml:"payload_template,omitempty" json:"payload_template,omitempty"`
	Headers         map[string]string `yaml:"headers,omitempty" json:"headers,omitempty"`
}

// ResponseConfig defines response parsing configuration
type ResponseConfig struct {
	Format     string                 `yaml:"format" json:"format"` // "json", "text", "xml"
	Structure  map[string]interface{} `yaml:"structure,omitempty" json:"structure,omitempty"`
	EmailsKey  string                 `yaml:"emails_key,omitempty" json:"emails_key,omitempty"` // Key to extract emails array
	ResultsKey string                 `yaml:"results_key,omitempty" json:"results_key,omitempty"` // Key to extract results array
}

// SkillsConfig represents the root configuration
type SkillsConfig struct {
	Skills []SkillConfig `yaml:"skills" json:"skills"`
}

// LoadSkillsConfig loads skill configurations from a file
func LoadSkillsConfig(path string) (*SkillsConfig, error) {
	// Try multiple paths
	paths := []string{
		path,
		filepath.Join("config", path),
		filepath.Join("config", "n8n_mcp_skills.yaml"),
		filepath.Join("config", "n8n_mcp_skills.json"),
		"n8n_mcp_skills.yaml",
		"n8n_mcp_skills.json",
	}

	var file *os.File
	var err error
	var foundPath string

	for _, p := range paths {
		if file, err = os.Open(p); err == nil {
			foundPath = p
			break
		}
	}

	if file == nil {
		log.Printf("⚠️ [CONFIG-SKILLS] No skills configuration file found, using defaults")
		return &SkillsConfig{Skills: []SkillConfig{}}, nil
	}
	defer file.Close()

	log.Printf("📋 [CONFIG-SKILLS] Loading skills configuration from: %s", foundPath)

	var config SkillsConfig

	// Determine file type and parse
	if strings.HasSuffix(foundPath, ".yaml") || strings.HasSuffix(foundPath, ".yml") {
		data, err := io.ReadAll(file)
		if err != nil {
			return nil, fmt.Errorf("failed to read config file: %w", err)
		}

		// Expand environment variables in YAML
		expanded := os.ExpandEnv(string(data))
		if err := yaml.Unmarshal([]byte(expanded), &config); err != nil {
			return nil, fmt.Errorf("failed to parse YAML config: %w", err)
		}
	} else {
		// JSON
		decoder := json.NewDecoder(file)
		if err := decoder.Decode(&config); err != nil {
			return nil, fmt.Errorf("failed to parse JSON config: %w", err)
		}
	}

	// Expand environment variables in endpoint URLs
	for i := range config.Skills {
		config.Skills[i].Endpoint = os.ExpandEnv(config.Skills[i].Endpoint)
	}

	log.Printf("✅ [CONFIG-SKILLS] Loaded %d skill(s) from configuration", len(config.Skills))
	return &config, nil
}

// ValidateSkillConfig validates a skill configuration
func ValidateSkillConfig(skill *SkillConfig) error {
	if skill.ID == "" {
		return fmt.Errorf("skill ID is required")
	}
	if skill.Name == "" {
		return fmt.Errorf("skill name is required")
	}
	if skill.Type == "" {
		return fmt.Errorf("skill type is required")
	}
	if skill.Endpoint == "" {
		return fmt.Errorf("skill endpoint is required")
	}
	if skill.Method == "" {
		skill.Method = "POST" // Default to POST
	}
	return nil
}

