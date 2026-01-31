package interpreter

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"os"
	"strings"
	"time"
)

// CompositeToolProvider combines multiple tool providers
type CompositeToolProvider struct {
	providers  []ToolProviderInterface
	toolRouter map[string]ToolProviderInterface
}

// NewCompositeToolProvider creates a composite tool provider that combines HDN and MCP tools
func NewCompositeToolProvider(hdnURL string) *CompositeToolProvider {
	providers := []ToolProviderInterface{}

	// Add HDN tool provider
	if hdnURL == "" {
		hdnURL = "http://localhost:8081"
	}
	providers = append(providers, NewRealToolProvider(hdnURL))

	// Add MCP knowledge tool provider(s)
	// Check for multiple endpoints first (comma separated)
	mcpEndpointsStr := os.Getenv("MCP_ENDPOINTS")
	var mcpEndpoints []string

	if mcpEndpointsStr != "" {
		parts := strings.Split(mcpEndpointsStr, ",")
		for _, p := range parts {
			if trimmed := strings.TrimSpace(p); trimmed != "" {
				mcpEndpoints = append(mcpEndpoints, trimmed)
			}
		}
	}

	// Fallback to legacy single endpoint if no plural defined
	if len(mcpEndpoints) == 0 {
		mcpEndpoint := os.Getenv("MCP_ENDPOINT")
		if mcpEndpoint == "" {
			// Default to HDN's MCP endpoint
			mcpEndpoint = hdnURL + "/mcp"

			// If connecting to ourselves (Kubernetes service DNS or same host), use localhost
			// This prevents connection issues when HDN tries to connect to itself via service DNS
			if isSelfConnection(mcpEndpoint) {
				// Parse URL to get port, default to 8080
				parsedURL, err := url.Parse(mcpEndpoint)
				if err == nil {
					port := parsedURL.Port()
					if port == "" {
						port = "8080" // Default HDN port
					}
					mcpEndpoint = fmt.Sprintf("http://localhost:%s/mcp", port)
					log.Printf("🔧 [MCP] Detected self-connection, using localhost: %s", mcpEndpoint)
				}
			}
		}
		mcpEndpoints = append(mcpEndpoints, mcpEndpoint)
	}

	// Create providers for all endpoints
	for _, endpoint := range mcpEndpoints {
		mcpProvider := NewMCPToolProvider(endpoint)
		providers = append(providers, mcpProvider)
		log.Printf("🔧 [MCP] Added MCP provider for endpoint: %s", endpoint)
	}

	// Create composite provider
	composite := &CompositeToolProvider{
		providers:  providers,
		toolRouter: make(map[string]ToolProviderInterface),
	}

	// Verify MCP server connection asynchronously after a delay
	// This allows the HTTP server to start listening first
	go func() {
		// Wait a bit for the server to start
		time.Sleep(2 * time.Second)
		composite.verifyMCPConnection()
	}()

	return composite
}

// verifyMCPConnection checks if MCP servers are accessible and tools can be discovered
func (c *CompositeToolProvider) verifyMCPConnection() {
	ctx := context.Background()

	// Find MCP providers
	var mcpProviders []*MCPToolProvider
	for _, provider := range c.providers {
		if mcp, ok := provider.(*MCPToolProvider); ok {
			mcpProviders = append(mcpProviders, mcp)
		}
	}

	if len(mcpProviders) == 0 {
		log.Printf("⚠️ [MCP-VERIFY] No MCP providers found")
		return
	}

	log.Printf("🔍 [MCP-VERIFY] Verifying %d MCP server connections...", len(mcpProviders))

	for i, mcpProvider := range mcpProviders {
		// Try to discover tools
		tools, err := mcpProvider.GetAvailableTools(ctx)
		if err != nil {
			log.Printf("❌ [MCP-VERIFY] Provider %d: Failed to discover MCP tools: %v", i, err)
			continue
		}

		log.Printf("✅ [MCP-VERIFY] Provider %d accessible - discovered %d tools", i, len(tools))
		for _, tool := range tools {
			log.Printf("   - %s: %s", tool.ID, tool.Description)
		}

		// Test execution of a simple tool (get_concept with a test query) if available
		// Only test on the first provider that has get_concept to avoid spamming
		for _, tool := range tools {
			if tool.ID == "mcp_get_concept" {
				log.Printf("🧪 [MCP-VERIFY] Testing MCP tool execution (get_concept)...")
				testParams := map[string]interface{}{
					"name": "Biology",
				}
				result, err := mcpProvider.ExecuteTool(ctx, "mcp_get_concept", testParams)
				if err != nil {
					log.Printf("⚠️ [MCP-VERIFY] MCP tool execution test failed: %v", err)
				} else {
					// Check if we got a result
					if resultMap, ok := result.(map[string]interface{}); ok {
						if count, ok := resultMap["count"].(float64); ok && count > 0 {
							log.Printf("✅ [MCP-VERIFY] MCP tool execution successful - retrieved %v results", count)
						} else {
							log.Printf("⚠️ [MCP-VERIFY] MCP tool executed but returned empty results (this may be normal)")
						}
					} else {
						log.Printf("✅ [MCP-VERIFY] MCP tool execution successful")
					}
				}
				break
			}
		}
	}

	log.Printf("✅ [MCP-VERIFY] MCP integration verification complete")
}

// GetAvailableTools retrieves tools from all providers
func (c *CompositeToolProvider) GetAvailableTools(ctx context.Context) ([]Tool, error) {
	var allTools []Tool

	// Reset tool router
	c.toolRouter = make(map[string]ToolProviderInterface)

	for i, provider := range c.providers {
		tools, err := provider.GetAvailableTools(ctx)
		if err != nil {
			log.Printf("⚠️ [COMPOSITE-TOOL-PROVIDER] Provider %d failed to get tools: %v", i, err)
			continue // Continue with other providers
		}

		// Register tools in router and list
		for _, tool := range tools {
			c.toolRouter[tool.ID] = provider
			allTools = append(allTools, tool)
		}
	}

	log.Printf("✅ [COMPOSITE-TOOL-PROVIDER] Retrieved %d total tools from %d providers", len(allTools), len(c.providers))
	return allTools, nil
}

// ExecuteTool executes a tool by finding the appropriate provider
func (c *CompositeToolProvider) ExecuteTool(ctx context.Context, toolID string, parameters map[string]interface{}) (interface{}, error) {
	// 1. Try to find provider in the router (most reliable method)
	if provider, ok := c.toolRouter[toolID]; ok {
		return provider.ExecuteTool(ctx, toolID, parameters)
	}

	// 2. Fallback: If not found, refresh tools and try again
	// This handles cases where new tools appeared or GetAvailableTools wasn't called yet
	log.Printf("⚠️ [COMPOSITE] Tool %s not found in router, refreshing tools...", toolID)
	_, err := c.GetAvailableTools(ctx)
	if err != nil {
		log.Printf("⚠️ [COMPOSITE] Failed to refresh tools: %v", err)
	}

	// 3. Try again after refresh
	if provider, ok := c.toolRouter[toolID]; ok {
		return provider.ExecuteTool(ctx, toolID, parameters)
	}

	return nil, fmt.Errorf("no provider found for tool: %s (checked %d providers)", toolID, len(c.providers))
}

// isSelfConnection checks if the endpoint is pointing to the same server (self-connection)
// This detects Kubernetes service DNS patterns and localhost patterns
func isSelfConnection(endpoint string) bool {
	lower := strings.ToLower(endpoint)

	// Check for Kubernetes service DNS patterns (e.g., hdn-server-*.svc.cluster.local)
	if strings.Contains(lower, ".svc.cluster.local") {
		// Extract service name and check if it matches HDN service pattern
		if strings.Contains(lower, "hdn") || strings.Contains(lower, "hdn-server") {
			return true
		}
	}

	// Check if it's already localhost
	if strings.Contains(lower, "localhost") || strings.Contains(lower, "127.0.0.1") {
		return true
	}

	return false
}
