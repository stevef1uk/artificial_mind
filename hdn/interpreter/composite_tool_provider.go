package interpreter

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"
)

// CompositeToolProvider combines multiple tool providers
type CompositeToolProvider struct {
	providers []ToolProviderInterface
}

// NewCompositeToolProvider creates a composite tool provider that combines HDN and MCP tools
func NewCompositeToolProvider(hdnURL string) *CompositeToolProvider {
	providers := []ToolProviderInterface{}

	// Add HDN tool provider
	if hdnURL == "" {
		hdnURL = "http://localhost:8081"
	}
	providers = append(providers, NewRealToolProvider(hdnURL))

	// Add MCP knowledge tool provider if MCP endpoint is configured
	mcpEndpoint := os.Getenv("MCP_ENDPOINT")
	if mcpEndpoint == "" {
		// Default to HDN's MCP endpoint
		mcpEndpoint = hdnURL + "/mcp"
	}
	mcpProvider := NewMCPToolProvider(mcpEndpoint)
	providers = append(providers, mcpProvider)

	// Create composite provider
	composite := &CompositeToolProvider{
		providers: providers,
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

// verifyMCPConnection checks if MCP server is accessible and tools can be discovered
func (c *CompositeToolProvider) verifyMCPConnection() {
	ctx := context.Background()

	// Find MCP provider
	var mcpProvider *MCPToolProvider
	for _, provider := range c.providers {
		if mcp, ok := provider.(*MCPToolProvider); ok {
			mcpProvider = mcp
			break
		}
	}

	if mcpProvider == nil {
		log.Printf("⚠️ [MCP-VERIFY] MCP provider not found")
		return
	}

	log.Printf("🔍 [MCP-VERIFY] Verifying MCP server connection...")

	// Try to discover tools
	tools, err := mcpProvider.GetAvailableTools(ctx)
	if err != nil {
		log.Printf("❌ [MCP-VERIFY] Failed to discover MCP tools: %v", err)
		log.Printf("⚠️ [MCP-VERIFY] MCP knowledge tools will not be available to LLM")
		return
	}

	log.Printf("✅ [MCP-VERIFY] MCP server accessible - discovered %d tools", len(tools))
	for _, tool := range tools {
		log.Printf("   - %s: %s", tool.ID, tool.Description)
	}

	// Test execution of a simple tool (get_concept with a test query)
	if len(tools) > 0 {
		// Try to execute get_concept if available
		for _, tool := range tools {
			if tool.ID == "mcp_get_concept" {
				log.Printf("🧪 [MCP-VERIFY] Testing MCP tool execution...")
				testParams := map[string]interface{}{
					"name": "Biology",
				}
				result, err := mcpProvider.ExecuteTool(ctx, "mcp_get_concept", testParams)
				if err != nil {
					log.Printf("⚠️ [MCP-VERIFY] MCP tool execution test failed: %v", err)
					log.Printf("⚠️ [MCP-VERIFY] Tools are discoverable but execution may have issues")
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

	log.Printf("✅ [MCP-VERIFY] MCP integration verified - LLM can use knowledge base tools")
}

// GetAvailableTools retrieves tools from all providers
func (c *CompositeToolProvider) GetAvailableTools(ctx context.Context) ([]Tool, error) {
	var allTools []Tool

	for i, provider := range c.providers {
		tools, err := provider.GetAvailableTools(ctx)
		if err != nil {
			log.Printf("⚠️ [COMPOSITE-TOOL-PROVIDER] Provider %d failed to get tools: %v", i, err)
			continue // Continue with other providers
		}
		allTools = append(allTools, tools...)
	}

	log.Printf("✅ [COMPOSITE-TOOL-PROVIDER] Retrieved %d total tools from %d providers", len(allTools), len(c.providers))
	return allTools, nil
}

// ExecuteTool executes a tool by finding the appropriate provider
func (c *CompositeToolProvider) ExecuteTool(ctx context.Context, toolID string, parameters map[string]interface{}) (interface{}, error) {
	// Determine which provider to use based on tool ID
	// MCP tools have "mcp_" prefix
	if len(toolID) > 4 && toolID[:4] == "mcp_" {
		// Use MCP provider
		for _, provider := range c.providers {
			if mcpProvider, ok := provider.(*MCPToolProvider); ok {
				return mcpProvider.ExecuteTool(ctx, toolID, parameters)
			}
		}
		return nil, fmt.Errorf("MCP tool provider not found for tool: %s", toolID)
	}

	// Use HDN provider for regular tools
	for _, provider := range c.providers {
		if hdnProvider, ok := provider.(*RealToolProvider); ok {
			return hdnProvider.ExecuteTool(ctx, toolID, parameters)
		}
	}

	return nil, fmt.Errorf("no provider found for tool: %s", toolID)
}
