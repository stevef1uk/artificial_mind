package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

func main() {
	// Get n8n webhook URL from environment or use default
	n8nURL := os.Getenv("N8N_WEBHOOK_URL")
	if n8nURL == "" {
		// Try cluster-internal URL first, then localhost (for port-forwarding)
		n8nURL = "http://n8n.n8n.svc.cluster.local:5678/webhook/google-workspace"
		fmt.Printf("Using default n8n URL: %s\n", n8nURL)
		fmt.Printf("Set N8N_WEBHOOK_URL environment variable to override\n")
		fmt.Printf("For local testing, use: kubectl port-forward -n n8n svc/n8n 5678:5678\n")
		fmt.Printf("Then set: export N8N_WEBHOOK_URL=http://localhost:5678/webhook/google-workspace\n\n")
	} else {
		fmt.Printf("Using n8n URL: %s\n\n", n8nURL)
	}

	// Get limit from command line or use default
	limit := 10
	if len(os.Args) > 1 {
		var err error
		if _, err = fmt.Sscanf(os.Args[1], "%d", &limit); err != nil {
			fmt.Printf("Invalid limit argument, using default: 10\n")
			limit = 10
		}
	}

	// Get query from command line or use default
	query := ""
	if len(os.Args) > 2 {
		query = os.Args[2]
	}

	// Construct request payload
	payload := map[string]interface{}{
		"query": query,
		"type":  "email",
		"limit": limit,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		fmt.Printf("❌ Failed to marshal request: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("📤 Request payload:\n")
	fmt.Printf("   Query: '%s'\n", query)
	fmt.Printf("   Type: email\n")
	fmt.Printf("   Limit: %d\n\n", limit)
	fmt.Printf("📤 Request JSON: %s\n\n", string(jsonData))

	// Create request
	req, err := http.NewRequest("POST", n8nURL, bytes.NewReader(jsonData))
	if err != nil {
		fmt.Printf("❌ Failed to create request: %v\n", err)
		os.Exit(1)
	}
	req.Header.Set("Content-Type", "application/json")

	// Add authentication header if secret is configured
	if secret := os.Getenv("N8N_WEBHOOK_SECRET"); secret != "" {
		req.Header.Set("X-Webhook-Secret", secret)
		fmt.Printf("🔐 Using webhook secret for authentication\n\n")
	}

	// Execute with timeout
	// Skip TLS verification for testing (certificate may not match domain)
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{
		Timeout:   60 * time.Second,
		Transport: tr,
	}
	fmt.Printf("⏳ Calling n8n webhook...\n\n")
	
	startTime := time.Now()
	resp, err := client.Do(req)
	duration := time.Since(startTime)
	
	if err != nil {
		fmt.Printf("❌ Failed to call n8n webhook: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	fmt.Printf("✅ Response received in %v\n", duration)
	fmt.Printf("📥 Status Code: %d\n", resp.StatusCode)
	fmt.Printf("📥 Content-Type: %s\n", resp.Header.Get("Content-Type"))
	fmt.Printf("📥 Response Headers:\n")
	for k, v := range resp.Header {
		if len(v) > 0 {
			fmt.Printf("   %s: %s\n", k, v[0])
		}
	}
	fmt.Printf("\n")

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("❌ Failed to read response body: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("📥 Response body length: %d bytes\n", len(bodyBytes))
	
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		fmt.Printf("❌ Error response (status %d):\n%s\n", resp.StatusCode, string(bodyBytes))
		os.Exit(1)
	}
	
	if len(bodyBytes) == 0 {
		fmt.Printf("⚠️  Empty response body received\n")
		fmt.Printf("   This might indicate:\n")
		fmt.Printf("   - Authentication issue (check N8N_WEBHOOK_SECRET)\n")
		fmt.Printf("   - Webhook not configured correctly\n")
		fmt.Printf("   - n8n workflow not active\n")
		os.Exit(1)
	}
	
	fmt.Printf("\n")

	// Parse response
	var result interface{}
	if err := json.Unmarshal(bodyBytes, &result); err != nil {
		fmt.Printf("⚠️ Response is not valid JSON: %v\n", err)
		fmt.Printf("📄 Raw response (first 1000 chars):\n%s\n", string(bodyBytes[:min(1000, len(bodyBytes))]))
		os.Exit(1)
	}

	// Analyze response structure
	fmt.Printf("🔍 Analyzing response structure...\n\n")
	analyzeResponse(result, 0)

	// Try to format as emails
	fmt.Printf("\n📧 Attempting to format as emails...\n\n")
	formatEmails(result)
}

func analyzeResponse(data interface{}, indent int) {
	indentStr := ""
	for i := 0; i < indent; i++ {
		indentStr += "  "
	}

	switch v := data.(type) {
	case map[string]interface{}:
		fmt.Printf("%s📦 Map with %d keys:\n", indentStr, len(v))
		var keys []string
		for k := range v {
			keys = append(keys, k)
		}
		fmt.Printf("%s   Keys: %v\n", indentStr, keys)
		
		// Check for email-like fields
		hasSubject := false
		hasFrom := false
		hasTo := false
		for k := range v {
			kLower := fmt.Sprintf("%v", k)
			if kLower == "subject" || kLower == "Subject" {
				hasSubject = true
			}
			if kLower == "from" || kLower == "From" {
				hasFrom = true
			}
			if kLower == "to" || kLower == "To" {
				hasTo = true
			}
		}
		if hasSubject || hasFrom {
			fmt.Printf("%s   ✅ Looks like an email object (hasSubject=%v, hasFrom=%v, hasTo=%v)\n", indentStr, hasSubject, hasFrom, hasTo)
		}

		// Recursively analyze nested structures (limit depth)
		if indent < 2 {
			for k, val := range v {
				if k == "json" || k == "results" || k == "items" {
					fmt.Printf("%s   📂 Key '%s':\n", indentStr, k)
					analyzeResponse(val, indent+2)
				}
			}
		}

	case []interface{}:
		fmt.Printf("%s📋 Array with %d items\n", indentStr, len(v))
		if len(v) > 0 {
			fmt.Printf("%s   First item type: %T\n", indentStr, v[0])
			if len(v) > 0 && indent < 2 {
				fmt.Printf("%s   📂 First item:\n", indentStr)
				analyzeResponse(v[0], indent+2)
			}
		}

	default:
		fmt.Printf("%s%s: %v\n", indentStr, fmt.Sprintf("%T", v), v)
	}
}

func formatEmails(data interface{}) {
	var emails []interface{}

	// Extract emails from various possible structures
	switch v := data.(type) {
	case []interface{}:
		emails = v
	case map[string]interface{}:
		// Check if it has an "emails" key (new format from Format as Array node)
		if emailsData, ok := v["emails"]; ok {
			if arr, ok := emailsData.([]interface{}); ok {
				emails = arr
			}
		} else {
			// Check if it's a single email
			hasSubject := false
			hasFrom := false
			for k := range v {
				kLower := fmt.Sprintf("%v", k)
				if kLower == "subject" || kLower == "Subject" {
					hasSubject = true
				}
				if kLower == "from" || kLower == "From" {
					hasFrom = true
				}
			}
			if hasSubject || hasFrom {
				emails = []interface{}{v}
			} else if jsonData, ok := v["json"]; ok {
				// Check if json key contains array
				if arr, ok := jsonData.([]interface{}); ok {
					emails = arr
				} else if m, ok := jsonData.(map[string]interface{}); ok {
					emails = []interface{}{m}
				}
			} else if results, ok := v["results"]; ok {
				if arr, ok := results.([]interface{}); ok {
					emails = arr
				}
			}
		}
	}

	if len(emails) == 0 {
		fmt.Printf("❌ Could not extract email list from response structure\n")
		return
	}

	fmt.Printf("✅ Found %d email(s)\n\n", len(emails))

	for i, email := range emails {
		if emailMap, ok := email.(map[string]interface{}); ok {
			fmt.Printf("[%d]\n", i+1)
			
			// Extract From field (case-insensitive)
			from := extractField(emailMap, "from")
			if from != "" {
				fmt.Printf("    From: %s\n", from)
			}
			
			// Extract Subject field (case-insensitive)
			subject := extractField(emailMap, "subject")
			if subject != "" {
				fmt.Printf("    Subject: %s\n", subject)
			}
			
			// Check for UNREAD label
			isUnread := false
			if labels, ok := emailMap["labelIds"].([]interface{}); ok {
				for _, label := range labels {
					if labelStr, ok := label.(string); ok && labelStr == "UNREAD" {
						isUnread = true
						break
					}
				}
			}
			if isUnread {
				fmt.Printf("    [UNREAD]\n")
			}
			
			fmt.Printf("\n")
		}
	}
}

func extractField(m map[string]interface{}, fieldName string) string {
	// Try exact match first
	if val, ok := m[fieldName]; ok && val != nil {
		return extractEmailAddress(val)
	}
	
	// Try case-insensitive match
	fieldLower := fmt.Sprintf("%v", fieldName)
	for k, v := range m {
		if fmt.Sprintf("%v", k) == fieldLower || fmt.Sprintf("%v", k) == fieldName {
			return extractEmailAddress(v)
		}
	}
	
	return ""
}

// extractEmailAddress extracts a clean email address from a "from" field that might be a string or complex object
func extractEmailAddress(fromField interface{}) string {
	if fromField == nil {
		return ""
	}
	
	// If it's already a string, return it
	if s, ok := fromField.(string); ok {
		return s
	}
	
	// If it's a map, try to extract the email address
	if m, ok := fromField.(map[string]interface{}); ok {
		// Try "address" field first
		if addr, ok := m["address"].(string); ok && addr != "" {
			name, _ := m["name"].(string)
			if name != "" {
				return fmt.Sprintf("%s <%s>", name, addr)
			}
			return addr
		}
		
		// Try "value" field which might contain an array
		if value, ok := m["value"]; ok {
			if arr, ok := value.([]interface{}); ok && len(arr) > 0 {
				if firstItem, ok := arr[0].(map[string]interface{}); ok {
					if addr, ok := firstItem["address"].(string); ok && addr != "" {
						name, _ := firstItem["name"].(string)
						if name != "" {
							return fmt.Sprintf("%s <%s>", name, addr)
						}
						return addr
					}
				}
			}
		}
		
		// Try "text" field (sometimes email is in text format)
		if text, ok := m["text"].(string); ok && text != "" {
			return text
		}
	}
	
	// Fallback: convert to string
	return fmt.Sprintf("%v", fromField)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

