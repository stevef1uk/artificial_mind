package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"

	"agi/services/playwright_scraper/scraper_pkg"

	pw "github.com/playwright-community/playwright-go"
)

// MyClimateScrapeRequest represents a request to scrape MyClimate flight data
type MyClimateScrapeRequest struct {
	From       string `json:"from"`
	To         string `json:"to"`
	Passengers int    `json:"passengers,omitempty"`
	CabinClass string `json:"cabin_class,omitempty"`
}

// WorkflowExecuteRequest represents a request to execute a generic workflow
type WorkflowExecuteRequest struct {
	WorkflowName string            `json:"workflow_name"`
	Params       map[string]string `json:"params"`
}

// SimpleServiceLogger implements scraper_pkg.Logger interface
type SimpleServiceLogger struct{}

func (sl *SimpleServiceLogger) Printf(format string, v ...interface{}) {
	log.Printf("[INFO] "+format, v...)
}

func (sl *SimpleServiceLogger) Errorf(format string, v ...interface{}) {
	log.Printf("[ERROR] "+format, v...)
}

// initMyClimateHandlers registers MyClimate-specific HTTP routes
func initMyClimateHandlers(mux *http.ServeMux, browser pw.Browser, logger scraper_pkg.Logger) {
	// Dedicated MyClimate endpoint
	mux.HandleFunc("/api/myclimate/flight", func(w http.ResponseWriter, r *http.Request) {
		handleMyClimateFlightScrape(w, r, browser, logger)
	})

	// Generic workflow endpoint
	mux.HandleFunc("/api/workflow/execute", func(w http.ResponseWriter, r *http.Request) {
		handleWorkflowExecute(w, r, browser, logger)
	})

	// Load workflow definition endpoint
	mux.HandleFunc("/api/workflows", func(w http.ResponseWriter, r *http.Request) {
		handleListWorkflows(w, r)
	})

	// Health check
	mux.HandleFunc("/api/myclimate/health", func(w http.ResponseWriter, r *http.Request) {
		if handleCORSPreflight(w, r) {
			return
		}
		setCORSHeaders(w)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
}

// handleMyClimateFlightScrape handles requests to scrape flight emissions
func handleMyClimateFlightScrape(w http.ResponseWriter, r *http.Request, browser pw.Browser, logger scraper_pkg.Logger) {
	if handleCORSPreflight(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req MyClimateScrapeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request: %v", err), http.StatusBadRequest)
		return
	}

	// Validate required fields
	if req.From == "" || req.To == "" {
		http.Error(w, "Missing required fields: from, to", http.StatusBadRequest)
		return
	}

	// Set defaults
	if req.Passengers == 0 {
		req.Passengers = 1
	}
	if req.CabinClass == "" {
		req.CabinClass = "ECONOMY"
	}

	// Create scraper and execute
	myclimate := scraper_pkg.NewMyClimate(browser, logger)
	result, err := myclimate.ScrapeFlightEmissions(r.Context(), req.From, req.To, req.Passengers, req.CabinClass)

	setCORSHeaders(w)
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
	} else {
		w.WriteHeader(http.StatusOK)
	}
	json.NewEncoder(w).Encode(result)
}

// handleWorkflowExecute handles requests to execute a generic workflow
func handleWorkflowExecute(w http.ResponseWriter, r *http.Request, browser pw.Browser, logger scraper_pkg.Logger) {
	if handleCORSPreflight(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req WorkflowExecuteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request: %v", err), http.StatusBadRequest)
		return
	}

	// Load workflow from file
	workflowPath := fmt.Sprintf("workflows/%s.json", req.WorkflowName)
	workflowData, err := os.ReadFile(workflowPath)
	if err != nil {
		http.Error(w, fmt.Sprintf("Workflow not found: %s", req.WorkflowName), http.StatusNotFound)
		return
	}

	workflow, err := scraper_pkg.LoadWorkflowFromJSON(workflowData)
	if err != nil {
		http.Error(w, fmt.Sprintf("Invalid workflow: %v", err), http.StatusBadRequest)
		return
	}

	// Execute workflow
	executor := scraper_pkg.NewWorkflowExecutor(browser, logger)
	result, err := executor.Execute(r.Context(), workflow, req.Params)

	setCORSHeaders(w)
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
	} else {
		w.WriteHeader(http.StatusOK)
	}
	json.NewEncoder(w).Encode(result)
}

// handleListWorkflows lists available workflows
func handleListWorkflows(w http.ResponseWriter, r *http.Request) {
	if handleCORSPreflight(w, r) {
		return
	}
	entries, err := os.ReadDir("./workflows")
	if err != nil {
		http.Error(w, fmt.Sprintf("Error reading workflows: %v", err), http.StatusInternalServerError)
		return
	}

	workflows := []map[string]string{}
	for _, entry := range entries {
		if !entry.IsDir() && len(entry.Name()) > 5 {
			name := entry.Name()[:len(entry.Name())-5] // Remove .json
			workflows = append(workflows, map[string]string{
				"name": name,
				"path": fmt.Sprintf("/api/workflow/execute?name=%s", name),
			})
		}
	}

	setCORSHeaders(w)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"workflows": workflows,
	})
}
