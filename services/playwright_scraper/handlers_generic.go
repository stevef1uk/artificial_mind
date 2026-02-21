package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"agi/services/playwright_scraper/scraper_pkg"

	pw "github.com/playwright-community/playwright-go"
)

// convertParamsToStrings converts map[string]interface{} to map[string]string
func convertParamsToStrings(params map[string]interface{}) map[string]string {
	result := make(map[string]string)
	for k, v := range params {
		switch val := v.(type) {
		case string:
			result[k] = val
		case float64:
			result[k] = fmt.Sprintf("%.0f", val)
		case bool:
			result[k] = fmt.Sprintf("%v", val)
		default:
			result[k] = fmt.Sprintf("%v", val)
		}
	}
	return result
}

func setCORSHeaders(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
}

func handleCORSPreflight(w http.ResponseWriter, r *http.Request) bool {
	setCORSHeaders(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return true
	}
	return false
}

// handleGenericScrape handles requests to scrape any website with natural language instructions
func handleGenericScrape(w http.ResponseWriter, r *http.Request, browser pw.Browser, logger scraper_pkg.Logger) {
	if handleCORSPreflight(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req scraper_pkg.GenericScrapeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request: %v", err), http.StatusBadRequest)
		return
	}

	// Validate request
	if req.URL == "" {
		http.Error(w, "URL is required", http.StatusBadRequest)
		return
	}

	logger.Printf("🕸️ Generic scrape requested for: %s", req.URL)
	logger.Printf("   Instructions: %s", req.Instructions)

	// Create scraper and execute
	scraper := scraper_pkg.NewGenericScraper(browser, logger)
	result, err := scraper.Scrape(r.Context(), req)

	if err != nil {
		logger.Errorf("Scrape failed: %v", err)
		setCORSHeaders(w)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "error",
			"error":  err.Error(),
		})
		return
	}

	setCORSHeaders(w)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// handleScrapeWithWorkflow executes a named workflow and returns results
func handleScrapeWithWorkflow(w http.ResponseWriter, r *http.Request, browser pw.Browser, logger scraper_pkg.Logger) {
	if handleCORSPreflight(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		WorkflowName string                 `json:"workflow_name"`
		Params       map[string]interface{} `json:"params"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request: %v", err), http.StatusBadRequest)
		return
	}

	// Load workflow definition
	workflowPath := filepath.Join("workflows", req.WorkflowName+".json")
	file, err := os.Open(workflowPath)
	if err != nil {
		http.Error(w, fmt.Sprintf("Workflow not found: %s", req.WorkflowName), http.StatusNotFound)
		return
	}
	defer file.Close()

	var workflow scraper_pkg.WorkflowDefinition
	if err := json.NewDecoder(file).Decode(&workflow); err != nil {
		http.Error(w, fmt.Sprintf("Invalid workflow: %v", err), http.StatusBadRequest)
		return
	}

	// Execute workflow
	executor := scraper_pkg.NewWorkflowExecutor(browser, logger)
	result, err := executor.Execute(r.Context(), &workflow, convertParamsToStrings(req.Params))

	if err != nil {
		logger.Errorf("Workflow execution failed: %v", err)
		setCORSHeaders(w)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "error",
			"error":  err.Error(),
		})
		return
	}

	setCORSHeaders(w)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// handleScraperAgentDeploy deploys a scraper as a scheduled agent
func handleScraperAgentDeploy(w http.ResponseWriter, r *http.Request, browser pw.Browser, logger scraper_pkg.Logger) {
	if handleCORSPreflight(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Name             string            `json:"name"`
		URL              string            `json:"url"`
		Instructions     string            `json:"instructions"`
		Frequency        string            `json:"frequency"` // once, hourly, daily
		Extractions      map[string]string `json:"extractions,omitempty"`
		TypeScriptConfig string            `json:"typescript_config,omitempty"`
		Variables        map[string]string `json:"variables,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request: %v", err), http.StatusBadRequest)
		return
	}

	logger.Printf("🤖 Deploying agent: %s", req.Name)
	logger.Printf("   URL: %s", req.URL)
	logger.Printf("   Frequency: %s", req.Frequency)

	// Save agent configuration to file
	config := map[string]interface{}{
		"name":              req.Name,
		"type":              "scraper",
		"url":               req.URL,
		"instructions":      req.Instructions,
		"extractions":       req.Extractions,
		"typescript_config": req.TypeScriptConfig,
		"variables":         req.Variables,
		"frequency":         req.Frequency,
		"created_at":        time.Now().Format(time.RFC3339),
		"status":            "active",
	}

	// Create data directory if not exists
	dataDir := "data/agents"
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		logger.Errorf("Failed to create agent directory: %v", err)
		http.Error(w, fmt.Sprintf("Failed to create agent directory: %v", err), http.StatusInternalServerError)
		return
	}

	// Write config to file
	filename := filepath.Join(dataDir, fmt.Sprintf("%s.json", req.Name))
	file, err := os.Create(filename)
	if err != nil {
		logger.Errorf("Failed to create agent file: %v", err)
		http.Error(w, fmt.Sprintf("Failed to save agent: %v", err), http.StatusInternalServerError)
		return
	}
	defer file.Close()

	if err := json.NewEncoder(file).Encode(config); err != nil {
		logger.Errorf("Failed to write agent config: %v", err)
		http.Error(w, fmt.Sprintf("Failed to write agent config: %v", err), http.StatusInternalServerError)
		return
	}

	logger.Printf("✅ Agent saved to %s", filename)

	setCORSHeaders(w)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "success",
		"agent":  config,
		"path":   filename,
	})
}

// initGenericHandlers registers the generic scraping endpoints
func initGenericHandlers(mux *http.ServeMux, browser pw.Browser, logger scraper_pkg.Logger) {
	// Generic web scraper endpoint
	mux.HandleFunc("/api/scraper/generic", func(w http.ResponseWriter, r *http.Request) {
		handleGenericScrape(w, r, browser, logger)
	})

	// Playwright codegen endpoints (headful)
	log.Println("📝 Registering codegen endpoints...")
	mux.HandleFunc("/api/codegen/start", handleCodegenStart)
	log.Println("  ✓ /api/codegen/start")
	mux.HandleFunc("/api/codegen/status", handleCodegenStatus)
	log.Println("  ✓ /api/codegen/status")
	mux.HandleFunc("/api/codegen/result", handleCodegenResult)
	log.Println("  ✓ /api/codegen/result")
	mux.HandleFunc("/api/codegen/latest", handleCodegenLatest)
	log.Println("  ✓ /api/codegen/latest")

	// Workflow-based scraper endpoint
	mux.HandleFunc("/api/scraper/workflow", func(w http.ResponseWriter, r *http.Request) {
		handleScrapeWithWorkflow(w, r, browser, logger)
	})

	// Agent deployment endpoint
	mux.HandleFunc("/api/scraper/agent/deploy", func(w http.ResponseWriter, r *http.Request) {
		handleScraperAgentDeploy(w, r, browser, logger)
	})

	log.Println("✅ Generic scraper handlers initialized")
}
