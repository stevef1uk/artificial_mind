package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"text/template"
	"time"
)

// N8NWebhookHandler handles execution of n8n webhook-based skills
type N8NWebhookHandler struct {
	config *SkillConfig
}

// NewN8NWebhookHandler creates a new n8n webhook handler for a skill
func NewN8NWebhookHandler(config *SkillConfig) *N8NWebhookHandler {
	return &N8NWebhookHandler{config: config}
}

// Execute executes the n8n webhook skill with the given arguments
func (h *N8NWebhookHandler) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	log.Printf("🔧 [N8N-WEBHOOK] Execute called for skill: %s, endpoint: %s, args: %+v", h.config.ID, h.config.Endpoint, args)
	
	// Check if endpoint is set
	if h.config.Endpoint == "" {
		return nil, fmt.Errorf("endpoint is empty for skill %s - check N8N_WEBHOOK_URL environment variable", h.config.ID)
	}
	
	// Build request payload from template
	payload, err := h.buildPayload(args)
	if err != nil {
		return nil, fmt.Errorf("failed to build payload: %w", err)
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, h.config.Method, h.config.Endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	if h.config.Request != nil && h.config.Request.Headers != nil {
		for k, v := range h.config.Request.Headers {
			req.Header.Set(k, v)
		}
	}

	// Add authentication
	if err := h.addAuth(req); err != nil {
		return nil, fmt.Errorf("failed to add authentication: %w", err)
	}

	// Create HTTP client with TLS configuration
	client := h.createHTTPClient()

	// Execute request
	log.Printf("📤 [N8N-WEBHOOK] Calling %s webhook: %s", h.config.ID, h.config.Endpoint)
	startTime := time.Now()
	resp, err := client.Do(req)
	duration := time.Since(startTime)
	if err != nil {
		return nil, fmt.Errorf("failed to call webhook: %w", err)
	}
	defer resp.Body.Close()

	log.Printf("✅ [N8N-WEBHOOK] Response received in %v (status: %d)", duration, resp.StatusCode)

	// Read response body
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Check for errors
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.Printf("❌ [N8N-WEBHOOK] Webhook returned error status %d. Response: %s", resp.StatusCode, string(bodyBytes))
		return nil, fmt.Errorf("webhook returned status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	// Parse response
	result, err := h.parseResponse(bodyBytes)
	if err != nil {
		log.Printf("❌ [N8N-WEBHOOK] Failed to parse response: %v", err)
		return nil, err
	}
	
	log.Printf("✅ [N8N-WEBHOOK] Successfully parsed response, returning result")
	return result, nil
}

// buildPayload builds the request payload from template and arguments
func (h *N8NWebhookHandler) buildPayload(args map[string]interface{}) ([]byte, error) {
	// Apply defaults from input_schema
	argsWithDefaults := h.applyDefaults(args)

	if h.config.Request == nil || h.config.Request.PayloadTemplate == "" {
		// No template, use args directly as JSON
		return json.Marshal(argsWithDefaults)
	}

	// Parse template with helper functions
	tmpl, err := template.New("payload").Funcs(template.FuncMap{
		"toInt": func(v interface{}) int {
			switch val := v.(type) {
			case int:
				return val
			case int64:
				return int(val)
			case float64:
				return int(val)
			case float32:
				return int(val)
			default:
				return 0
			}
		},
		"toString": func(v interface{}) string {
			return fmt.Sprintf("%v", v)
		},
	}).Parse(h.config.Request.PayloadTemplate)
	if err != nil {
		return nil, fmt.Errorf("failed to parse payload template: %w", err)
	}

	// Execute template
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, argsWithDefaults); err != nil {
		return nil, fmt.Errorf("failed to execute payload template: %w", err)
	}

	return buf.Bytes(), nil
}

// applyDefaults applies default values from input_schema to arguments
func (h *N8NWebhookHandler) applyDefaults(args map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})
	
	// Copy existing args
	for k, v := range args {
		result[k] = v
	}

	// Apply defaults from input_schema
	if h.config.InputSchema != nil {
		for key, schema := range h.config.InputSchema {
			// Skip if already set
			if _, exists := result[key]; exists {
				continue
			}

			// Get default from schema
			if param, ok := schema.(map[string]interface{}); ok {
				if def, hasDefault := param["default"]; hasDefault {
					result[key] = def
				}
			}
		}
	}

	return result
}

// addAuth adds authentication headers to the request
func (h *N8NWebhookHandler) addAuth(req *http.Request) error {
	if h.config.Auth == nil {
		return nil
	}

	switch h.config.Auth.Type {
	case "header":
		if h.config.Auth.Header == "" || h.config.Auth.SecretEnv == "" {
			return fmt.Errorf("header auth requires header name and secret_env")
		}
		secret := os.Getenv(h.config.Auth.SecretEnv)
		if secret == "" {
			return fmt.Errorf("secret environment variable %s is not set", h.config.Auth.SecretEnv)
		}
		req.Header.Set(h.config.Auth.Header, secret)

	case "bearer":
		if h.config.Auth.BearerEnv == "" {
			return fmt.Errorf("bearer auth requires bearer_env")
		}
		token := os.Getenv(h.config.Auth.BearerEnv)
		if token == "" {
			return fmt.Errorf("bearer token environment variable %s is not set", h.config.Auth.BearerEnv)
		}
		req.Header.Set("Authorization", "Bearer "+token)

	case "basic":
		// Basic auth would need username/password from config or env
		// For now, skip if not configured
		if h.config.Auth.BasicUser != "" && h.config.Auth.BasicPass != "" {
			req.SetBasicAuth(h.config.Auth.BasicUser, h.config.Auth.BasicPass)
		}
	}

	return nil
}

// createHTTPClient creates an HTTP client with appropriate TLS configuration
func (h *N8NWebhookHandler) createHTTPClient() *http.Client {
	timeout := 60 * time.Second
	if h.config.Timeout != "" {
		if parsed, err := time.ParseDuration(h.config.Timeout); err == nil {
			timeout = parsed
		}
	}

	transport := &http.Transport{}
	if h.config.TLS != nil && h.config.TLS.SkipVerify {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}

	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
	}
}

// parseResponse parses the webhook response based on configuration
func (h *N8NWebhookHandler) parseResponse(bodyBytes []byte) (interface{}, error) {
	if len(bodyBytes) == 0 {
		log.Printf("⚠️ [N8N-WEBHOOK] Empty response body")
		return map[string]interface{}{
			"results": []interface{}{},
			"message": "empty response",
		}, nil
	}

	// Determine response format
	format := "json"
	if h.config.Response != nil && h.config.Response.Format != "" {
		format = h.config.Response.Format
	}

	switch format {
	case "json":
		var result interface{}
		if err := json.Unmarshal(bodyBytes, &result); err != nil {
			return nil, fmt.Errorf("failed to parse JSON response: %w", err)
		}

		// Extract data based on response configuration
		if h.config.Response != nil {
			result = h.extractResponseData(result)
		}

		return result, nil

	case "text":
		return map[string]interface{}{
			"result": string(bodyBytes),
		}, nil

	default:
		// Default to JSON
		var result interface{}
		if err := json.Unmarshal(bodyBytes, &result); err != nil {
			return map[string]interface{}{
				"result": string(bodyBytes),
			}, nil
		}
		return result, nil
	}
}

// extractResponseData extracts data from response based on configuration
// Standard format: n8n webhooks should return {"results": [...]}
// This function normalizes various response formats to the standard format
func (h *N8NWebhookHandler) extractResponseData(data interface{}) interface{} {
	// Standard format: always return {"results": [...]}
	
	// If response is already an array, wrap it in standard format
	if resultArray, ok := data.([]interface{}); ok {
		log.Printf("📦 [N8N-WEBHOOK] Response is array with %d items, normalizing to standard format", len(resultArray))
		return map[string]interface{}{
			"results": resultArray,
		}
	}

	// If response is a map, check for results key or extract from configured key
	if resultMap, ok := data.(map[string]interface{}); ok {
		// First, check if it already has "results" key (standard format)
		if resultsData, hasResults := resultMap["results"]; hasResults {
			if resultsArray, ok := resultsData.([]interface{}); ok {
				log.Printf("✅ [N8N-WEBHOOK] Response already in standard format with %d results", len(resultsArray))
				return resultMap // Already in standard format
			}
		}
		
		// Check for configured results_key (for backward compatibility)
		if h.config.Response != nil && h.config.Response.ResultsKey != "" && h.config.Response.ResultsKey != "results" {
			if resultsData, hasResults := resultMap[h.config.Response.ResultsKey]; hasResults {
				if resultsArray, ok := resultsData.([]interface{}); ok {
					log.Printf("📦 [N8N-WEBHOOK] Extracted %d results from '%s' key, normalizing to standard format", len(resultsArray), h.config.Response.ResultsKey)
					return map[string]interface{}{
						"results": resultsArray,
					}
				}
			}
		}
		
		// Legacy support: check for emails_key (deprecated, use results_key instead)
		if h.config.Response != nil && h.config.Response.EmailsKey != "" {
			if emailsData, hasEmails := resultMap[h.config.Response.EmailsKey]; hasEmails {
				if emailsArray, ok := emailsData.([]interface{}); ok {
					log.Printf("⚠️ [N8N-WEBHOOK] Using deprecated emails_key, extracted %d items. Update workflow to use 'results' key.", len(emailsArray))
					return map[string]interface{}{
						"results": emailsArray,
					}
				}
			}
		}
		
		// If no results found, return as-is (might be error response or other format)
		log.Printf("⚠️ [N8N-WEBHOOK] Response map doesn't contain 'results' key, returning as-is")
		return resultMap
	}

	// For other types, wrap in standard format
	log.Printf("📦 [N8N-WEBHOOK] Wrapping response in standard format")
	return map[string]interface{}{
		"results": []interface{}{data},
	}
}

