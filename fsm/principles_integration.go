package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
)

// PrinciplesIntegration handles hardcoded principles checking
type PrinciplesIntegration struct {
	principlesURL string
	httpClient    *http.Client
}

// NewPrinciplesIntegration creates a new principles integration
func NewPrinciplesIntegration(principlesURL string) *PrinciplesIntegration {
	return &PrinciplesIntegration{
		principlesURL: principlesURL,
		httpClient: &http.Client{
			Timeout: 5 * time.Second, // Short timeout for critical safety checks
		},
	}
}

// PrinciplesCheckRequest represents a request to the Principles Server
type PrinciplesCheckRequest struct {
	Action  string                 `json:"action"`
	Context map[string]interface{} `json:"context"`
}

// PrinciplesCheckResponse represents a response from the Principles Server
type PrinciplesCheckResponse struct {
	Allowed      bool     `json:"allowed"`
	Reason       string   `json:"reason,omitempty"`
	RuleMatches  []string `json:"rule_matches,omitempty"`
	Confidence   float64  `json:"confidence,omitempty"`
	BlockedRules []string `json:"blocked_rules,omitempty"`
}

// MandatoryPrinciplesCheck performs a hardcoded mandatory principles check
func (pi *PrinciplesIntegration) MandatoryPrinciplesCheck(action string, context map[string]interface{}) (*PrinciplesCheckResponse, error) {
	log.Printf("🔒 MANDATORY PRINCIPLES CHECK - Checking action: %s", action)

	// This is a CRITICAL safety check that must always succeed
	// If this fails, the entire FSM should fail

	req := PrinciplesCheckRequest{
		Action: action,
		Context: map[string]interface{}{
			"check_type":      "mandatory",
			"hardcoded":       true,
			"critical_safety": true,
			"timestamp":       time.Now().Unix(),
			"context":         context,
		},
	}

	response, err := pi.callPrinciplesServer(req)
	if err != nil {
		log.Printf("❌ MANDATORY PRINCIPLES CHECK FAILED - Cannot reach Principles Server: %v", err)
		return nil, fmt.Errorf("mandatory principles check failed: %w", err)
	}

	if !response.Allowed {
		log.Printf("❌ MANDATORY PRINCIPLES CHECK FAILED - Action blocked: %s", response.Reason)
		return response, fmt.Errorf("action blocked by principles: %s", response.Reason)
	}

	log.Printf("✅ MANDATORY PRINCIPLES CHECK PASSED - Action allowed: %s", response.Reason)
	return response, nil
}

// PreExecutionPrinciplesCheck performs a double-check before execution
func (pi *PrinciplesIntegration) PreExecutionPrinciplesCheck(action string, context map[string]interface{}) (*PrinciplesCheckResponse, error) {
	log.Printf("🔒 PRE-EXECUTION PRINCIPLES CHECK - Double-checking before execution: %s", action)

	// This is a second safety check right before execution
	// Even if the first check passed, we check again for maximum safety

	req := PrinciplesCheckRequest{
		Action: action,
		Context: map[string]interface{}{
			"check_type":   "pre_execution",
			"double_check": true,
			"final_safety": true,
			"timestamp":    time.Now().Unix(),
			"context":      context,
		},
	}

	response, err := pi.callPrinciplesServer(req)
	if err != nil {
		log.Printf("❌ PRE-EXECUTION PRINCIPLES CHECK FAILED - Cannot reach Principles Server: %v", err)
		return nil, fmt.Errorf("pre-execution principles check failed: %w", err)
	}

	if !response.Allowed {
		log.Printf("❌ PRE-EXECUTION PRINCIPLES CHECK FAILED - Action blocked: %s", response.Reason)
		return response, fmt.Errorf("action blocked by pre-execution principles check: %s", response.Reason)
	}

	log.Printf("✅ PRE-EXECUTION PRINCIPLES CHECK PASSED - Action allowed: %s", response.Reason)
	return response, nil
}

// DomainAwarePrinciplesCheck performs principles check with domain knowledge
func (pi *PrinciplesIntegration) DomainAwarePrinciplesCheck(action string, domain string, constraints []string, context map[string]interface{}) (*PrinciplesCheckResponse, error) {
	log.Printf("🔒 DOMAIN-AWARE PRINCIPLES CHECK - Checking with domain context: %s (domain: %s)", action, domain)

	req := PrinciplesCheckRequest{
		Action: action,
		Context: map[string]interface{}{
			"check_type":  "domain_aware",
			"domain":      domain,
			"constraints": constraints,
			"timestamp":   time.Now().Unix(),
			"context":     context,
		},
	}

	response, err := pi.callPrinciplesServer(req)
	if err != nil {
		log.Printf("❌ DOMAIN-AWARE PRINCIPLES CHECK FAILED - Cannot reach Principles Server: %v", err)
		return nil, fmt.Errorf("domain-aware principles check failed: %w", err)
	}

	if !response.Allowed {
		log.Printf("❌ DOMAIN-AWARE PRINCIPLES CHECK FAILED - Action blocked: %s", response.Reason)
		return response, fmt.Errorf("action blocked by domain-aware principles check: %s", response.Reason)
	}

	log.Printf("✅ DOMAIN-AWARE PRINCIPLES CHECK PASSED - Action allowed: %s", response.Reason)
	return response, nil
}

// callPrinciplesServer makes the actual HTTP call to the Principles Server
func (pi *PrinciplesIntegration) callPrinciplesServer(req PrinciplesCheckRequest) (*PrinciplesCheckResponse, error) {
	reqData, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(context.Background(), "POST", pi.principlesURL+"/action", bytes.NewReader(reqData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("User-Agent", "AGI-FSM/1.0")

	resp, err := pi.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to call principles server: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("principles server returned status %d", resp.StatusCode)
	}

	var response PrinciplesCheckResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &response, nil
}

// IsPrinciplesServerAvailable checks if the Principles Server is reachable
func (pi *PrinciplesIntegration) IsPrinciplesServerAvailable() bool {
	// Prefer GET /
	if req, err := http.NewRequestWithContext(context.Background(), "GET", pi.principlesURL+"/", nil); err == nil {
		if resp, err2 := pi.httpClient.Do(req); err2 == nil {
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return true
			}
		}
	}

	// Fallback: OPTIONS /action (treat 200/204/400 as server reachable)
	if req2, err := http.NewRequestWithContext(context.Background(), "OPTIONS", pi.principlesURL+"/action", nil); err == nil {
		if resp2, err2 := pi.httpClient.Do(req2); err2 == nil {
			defer resp2.Body.Close()
			if resp2.StatusCode == http.StatusOK || resp2.StatusCode == http.StatusNoContent || resp2.StatusCode == http.StatusBadRequest {
				return true
			}
		}
	}
	return false
}

// GetPrinciplesServerStatus returns detailed status of the Principles Server
func (pi *PrinciplesIntegration) GetPrinciplesServerStatus() map[string]interface{} {
	status := map[string]interface{}{
		"available": pi.IsPrinciplesServerAvailable(),
		"url":       pi.principlesURL,
		"timestamp": time.Now().Unix(),
	}

	if !status["available"].(bool) {
		status["error"] = "Principles Server not reachable"
		return status
	}

	// Try to get rules count
	req, err := http.NewRequestWithContext(context.Background(), "GET", pi.principlesURL+"/rules", nil)
	if err == nil {
		resp, err := pi.httpClient.Do(req)
		if err == nil {
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				var rulesResp struct {
					Rules []interface{} `json:"rules"`
				}
				if json.NewDecoder(resp.Body).Decode(&rulesResp) == nil {
					status["rules_count"] = len(rulesResp.Rules)
				}
			}
		}
	}

	return status
}
