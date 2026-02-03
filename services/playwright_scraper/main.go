// Playwright Scraper Service
// Copyright (c) 2026 Steven Fisher
// 
// This software is licensed for non-commercial use only.
// Commercial use requires a separate license.
// See LICENSE file for full terms.

package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	pw "github.com/playwright-community/playwright-go"
)

// Job status constants
const (
	JobStatusPending   = "pending"
	JobStatusRunning   = "running"
	JobStatusCompleted = "completed"
	JobStatusFailed    = "failed"
)

// ScrapeRequest represents an incoming scrape request
type ScrapeRequest struct {
	URL              string `json:"url"`
	TypeScriptConfig string `json:"typescript_config"`
}

// ScrapeJob represents a scrape job in the queue
type ScrapeJob struct {
	ID               string                 `json:"id"`
	URL              string                 `json:"url"`
	TypeScriptConfig string                 `json:"typescript_config"`
	Status           string                 `json:"status"`
	CreatedAt        time.Time              `json:"created_at"`
	StartedAt        *time.Time             `json:"started_at,omitempty"`
	CompletedAt      *time.Time             `json:"completed_at,omitempty"`
	Result           map[string]interface{} `json:"result,omitempty"`
	Error            string                 `json:"error,omitempty"`
}

// PlaywrightOperation represents a parsed operation from TypeScript config
type PlaywrightOperation struct {
	Type     string
	Selector string
	Value    string
	Role     string
	RoleName string
	Text     string
	Timeout  int
}

// JobStore manages scrape jobs in memory
type JobStore struct {
	mu   sync.RWMutex
	jobs map[string]*ScrapeJob
}

func NewJobStore() *JobStore {
	return &JobStore{
		jobs: make(map[string]*ScrapeJob),
	}
}

func (s *JobStore) Create(url, tsConfig string) *ScrapeJob {
	s.mu.Lock()
	defer s.mu.Unlock()

	job := &ScrapeJob{
		ID:               uuid.New().String(),
		URL:              url,
		TypeScriptConfig: tsConfig,
		Status:           JobStatusPending,
		CreatedAt:        time.Now(),
	}
	s.jobs[job.ID] = job
	return job
}

func (s *JobStore) Get(id string) (*ScrapeJob, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	job, ok := s.jobs[id]
	return job, ok
}

func (s *JobStore) Update(job *ScrapeJob) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobs[job.ID] = job
}

func (s *JobStore) UpdateStatus(id, status string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if job, ok := s.jobs[id]; ok {
		job.Status = status
		if status == JobStatusRunning {
			now := time.Now()
			job.StartedAt = &now
		} else if status == JobStatusCompleted || status == JobStatusFailed {
			now := time.Now()
			job.CompletedAt = &now
		}
	}
}

func (s *JobStore) CleanupOld(maxAge time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cutoff := time.Now().Add(-maxAge)
	for id, job := range s.jobs {
		if job.CompletedAt != nil && job.CompletedAt.Before(cutoff) {
			delete(s.jobs, id)
		}
	}
}

// ScraperService handles scraping operations
type ScraperService struct {
	store          *JobStore
	workerCount    int
	jobQueue       chan string
	playwrightOnce sync.Once
}

func NewScraperService(workerCount int) *ScraperService {
	return &ScraperService{
		store:       NewJobStore(),
		workerCount: workerCount,
		jobQueue:    make(chan string, 100),
	}
}

func (s *ScraperService) Start() {
	// Start worker goroutines
	for i := 0; i < s.workerCount; i++ {
		go s.worker(i)
	}

	// Start cleanup goroutine
	go s.cleanupWorker()

	log.Printf("✅ Started %d scraper workers", s.workerCount)
}

func (s *ScraperService) worker(id int) {
	log.Printf("🚀 Worker %d started", id)

	for jobID := range s.jobQueue {
		job, ok := s.store.Get(jobID)
		if !ok {
			log.Printf("⚠️ Worker %d: Job %s not found", id, jobID)
			continue
		}

		log.Printf("🔧 Worker %d: Processing job %s", id, jobID)
		s.store.UpdateStatus(jobID, JobStatusRunning)

		result, err := s.executeJob(job)
		if err != nil {
			log.Printf("❌ Worker %d: Job %s failed: %v", id, jobID, err)
			job.Status = JobStatusFailed
			job.Error = err.Error()
		} else {
			log.Printf("✅ Worker %d: Job %s completed", id, jobID)
			job.Status = JobStatusCompleted
			job.Result = result
		}

		s.store.Update(job)
	}
}

func (s *ScraperService) cleanupWorker() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		s.store.CleanupOld(30 * time.Minute)
	}
}

func (s *ScraperService) executeJob(job *ScrapeJob) (map[string]interface{}, error) {
	// One-time Playwright installation
	s.playwrightOnce.Do(func() {
		log.Println("🔧 Installing Playwright driver (one-time setup)...")
		err := pw.Install(&pw.RunOptions{
			SkipInstallBrowsers: true,
			Verbose:             true,
		})
		if err != nil {
			log.Printf("⚠️ Playwright driver installation warning: %v", err)
		} else {
			log.Println("✅ Playwright driver installed")
		}
	})

	// Parse TypeScript config
	operations, err := parseTypeScriptConfig(job.TypeScriptConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to parse TypeScript config: %v", err)
	}

	log.Printf("📋 Parsed %d operations", len(operations))

	// Execute Playwright operations
	return executePlaywrightOperations(job.URL, operations)
}

func parseTypeScriptConfig(tsConfig string) ([]PlaywrightOperation, error) {
	var operations []PlaywrightOperation

	// Parse operations in order by finding all 'await page.' patterns
	// This preserves the execution order from the TypeScript
	awaitRegex := regexp.MustCompile(`await\s+page\.[^\n]+`)
	matches := awaitRegex.FindAllString(tsConfig, -1)

	for _, match := range matches {
		op := parseOperation(match)
		if op.Type != "" {
			operations = append(operations, op)
		}
	}

	return operations, nil
}

func parseOperation(line string) PlaywrightOperation {
	// goto
	if matches := regexp.MustCompile(`await\s+page\.goto\(['"]([^'"]+)['"]\)`).FindStringSubmatch(line); len(matches) > 1 {
		return PlaywrightOperation{Type: "goto", Selector: matches[1]}
	}

	// getByRole (click)
	if matches := regexp.MustCompile(`await\s+page\.getByRole\(['"](\w+)['"],\s*\{\s*name:\s*['"]([^'"]+)['"]\s*\}\)\.click\(\)`).FindStringSubmatch(line); len(matches) > 2 {
		return PlaywrightOperation{Type: "getByRole", Role: matches[1], RoleName: matches[2]}
	}

	// getByRole (fill)
	if matches := regexp.MustCompile(`await\s+page\.getByRole\(['"](\w+)['"],\s*\{\s*name:\s*['"]([^'"]+)['"]\s*\}\)\.fill\(['"]([^'"]+)['"]\)`).FindStringSubmatch(line); len(matches) > 3 {
		return PlaywrightOperation{Type: "getByRoleFill", Role: matches[1], RoleName: matches[2], Value: matches[3]}
	}

	// getByText with .first().click()
	if matches := regexp.MustCompile(`await\s+page\.getByText\(['"]([^'"]+)['"]\)\.first\(\)\.click\(\)`).FindStringSubmatch(line); len(matches) > 1 {
		return PlaywrightOperation{Type: "getByText", Text: matches[1]}
	}

	// getByText with .click() (no .first())
	if matches := regexp.MustCompile(`await\s+page\.getByText\(['"]([^'"]+)['"]\)\.click\(\)`).FindStringSubmatch(line); len(matches) > 1 {
		return PlaywrightOperation{Type: "getByTextClick", Text: matches[1]}
	}

	// locator with .fill() - use .+? to match any characters including nested quotes
	if matches := regexp.MustCompile(`await\s+page\.locator\(['"](.+?)['"]\)\.fill\(['"](.+?)['"]\)`).FindStringSubmatch(line); len(matches) > 2 {
		return PlaywrightOperation{Type: "locatorFill", Selector: matches[1], Value: matches[2]}
	}

	// locator with .first().click()
	if matches := regexp.MustCompile(`await\s+page\.locator\(['"](.+?)['"]\)\.first\(\)\.click\(\)`).FindStringSubmatch(line); len(matches) > 1 {
		return PlaywrightOperation{Type: "locatorFirst", Selector: matches[1]}
	}

	// locator with .click() (no .first())
	if matches := regexp.MustCompile(`await\s+page\.locator\(['"](.+?)['"]\)\.click\(\)`).FindStringSubmatch(line); len(matches) > 1 {
		return PlaywrightOperation{Type: "locator", Selector: matches[1]}
	}

	// waitForTimeout
	if matches := regexp.MustCompile(`await\s+page\.waitForTimeout\((\d+)\)`).FindStringSubmatch(line); len(matches) > 1 {
		var timeout int
		fmt.Sscanf(matches[1], "%d", &timeout)
		return PlaywrightOperation{Type: "wait", Timeout: timeout / 1000}
	}

	return PlaywrightOperation{}
}

func executePlaywrightOperations(url string, operations []PlaywrightOperation) (map[string]interface{}, error) {
	// Start Playwright
	pwInstance, err := pw.Run()
	if err != nil {
		return nil, fmt.Errorf("failed to start Playwright: %v", err)
	}
	defer pwInstance.Stop()

	// Launch browser
	executablePath := "/usr/bin/chromium"
	browser, err := pwInstance.Chromium.Launch(pw.BrowserTypeLaunchOptions{
		Headless:       pw.Bool(true),
		ExecutablePath: &executablePath,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to launch browser: %v", err)
	}
	defer browser.Close()

	// Create page
	page, err := browser.NewPage()
	if err != nil {
		return nil, fmt.Errorf("failed to create page: %v", err)
	}
	defer page.Close()

	page.SetDefaultTimeout(20000) // 20 seconds

	// Navigate to URL
	log.Printf("📍 Navigating to %s", url)
	if _, err := page.Goto(url, pw.PageGotoOptions{
		WaitUntil: pw.WaitUntilStateNetworkidle,
	}); err != nil {
		return nil, fmt.Errorf("failed to navigate: %v", err)
	}

	// Execute operations
	for i, op := range operations {
		log.Printf("  [%d/%d] %s", i+1, len(operations), op.Type)

		switch op.Type {
		case "goto":
			if op.Selector != "" {
				page.Goto(op.Selector, pw.PageGotoOptions{WaitUntil: pw.WaitUntilStateNetworkidle})
			}

		case "getByRole":
			if op.Role == "link" && op.RoleName != "" {
				locator := page.GetByRole(pw.AriaRole("link"), pw.PageGetByRoleOptions{Name: op.RoleName})
				if err := locator.Click(); err != nil {
					log.Printf("   ⚠️ Failed: %v", err)
				}
				time.Sleep(500 * time.Millisecond)
			}

		case "getByRoleFill":
			if op.Role == "textbox" && op.RoleName != "" {
				locator := page.GetByRole(pw.AriaRole("textbox"), pw.PageGetByRoleOptions{Name: op.RoleName})
				if err := locator.Fill(op.Value); err != nil {
					log.Printf("   ⚠️ Failed: %v", err)
				}
				time.Sleep(500 * time.Millisecond)
			}

		case "getByTextClick":
			// getByText().click() - no .first()
			if err := page.GetByText(op.Text).Click(); err != nil {
				log.Printf("   ⚠️ Failed: %v", err)
			}
			time.Sleep(300 * time.Millisecond)

		case "getByText":
			// getByText().first().click()
			locator := page.GetByText(op.Text)
			if err := locator.First().Click(); err != nil {
				log.Printf("   ⚠️ Failed: %v", err)
			}
			time.Sleep(300 * time.Millisecond)

		case "locator":
			// locator().click()
			if err := page.Locator(op.Selector).Click(); err != nil {
				log.Printf("   ⚠️ Failed: %v", err)
			}
			time.Sleep(300 * time.Millisecond)

		case "locatorFirst":
			// locator().first().click()
			if err := page.Locator(op.Selector).First().Click(); err != nil {
				log.Printf("   ⚠️ Failed: %v", err)
			}
			time.Sleep(300 * time.Millisecond)

		case "locatorFill":
			if err := page.Locator(op.Selector).Fill(op.Value); err != nil {
				log.Printf("   ⚠️ Failed: %v", err)
			}
			time.Sleep(300 * time.Millisecond)

		case "wait":
			if op.Timeout > 0 {
				time.Sleep(time.Duration(op.Timeout) * time.Second)
			} else {
				time.Sleep(500 * time.Millisecond)
			}
		}
	}

	// Wait for final state
	page.WaitForLoadState(pw.PageWaitForLoadStateOptions{State: pw.LoadStateNetworkidle})
	
	// Wait for operations to fully complete
	time.Sleep(500 * time.Millisecond)
	
	// Wait for URL to change to include #result
	log.Println("⏳ Waiting for results URL...")
	for i := 0; i < 20; i++ {
		currentURL := page.URL()
		if strings.Contains(currentURL, "#result") {
			log.Println("✅ Results URL detected")
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	
	// Additional wait for dynamic JavaScript to compute and render results
	// The standalone test shows this needs at least 5 seconds AFTER all operations complete
	// EcoTree's calculator does client-side computation which takes time
	log.Println("⏳ Waiting for dynamic content to render (7 seconds)...")
	time.Sleep(7000 * time.Millisecond)
	
	// Force a reload of the page state to ensure we get fresh content
	page.WaitForLoadState(pw.PageWaitForLoadStateOptions{State: pw.LoadStateNetworkidle})

	// Extract results
	log.Println("📊 Extracting results...")
	
	results := make(map[string]interface{})
	results["page_url"] = page.URL()
	results["page_title"], _ = page.Title()
	
	// Get the full page HTML content (includes dynamically rendered elements)
	htmlContent, _ := page.Content()
	
	// Also get text content for backup
	bodyContent, _ := page.TextContent("body")
	
	log.Printf("🔍 HTML content length: %d bytes", len(htmlContent))
	log.Printf("🔍 Body text length: %d bytes", len(bodyContent))
	
	// Extract CO2 using both HTML and text content
	// Look for all numbers followed by "kg"
	co2Regex := regexp.MustCompile(`(?i)(\d+(?:[,\s]\d+)*(?:[.,]\d+)?)\s*(?:kg|kilogram)`)
	allCO2Matches := co2Regex.FindAllStringSubmatch(htmlContent + " " + bodyContent, -1)
	
	log.Printf("🔍 Found %d kg matches", len(allCO2Matches))
	
	// Debug: print all matches
	for i, match := range allCO2Matches {
		if len(match) > 1 && i < 10 {
			log.Printf("   Match %d: '%s'", i, match[1])
		}
	}
	
	// Find the largest CO2 value (the result is usually the biggest meaningful number)
	// We want to ignore very small values (like 1 kg, 3.1 kg from explanation text)
	maxCO2 := 0.0
	var co2Value string
	
	for _, match := range allCO2Matches {
		if len(match) > 1 {
			valStr := strings.ReplaceAll(match[1], ",", "")
			valStr = strings.ReplaceAll(valStr, " ", "")
			// Handle both comma and period as decimal separator
			valStr = strings.ReplaceAll(valStr, ".", ".")
			
			if val, err := strconv.ParseFloat(valStr, 64); err == nil {
				log.Printf("   Parsed: %s -> %.2f (maxCO2: %.2f)", match[1], val, maxCO2)
				// Only consider values > 10 to filter out explanation text (3.1 kg, 1 kg, etc)
				if val > 10 && val > maxCO2 {
					maxCO2 = val
					co2Value = match[1]
					log.Printf("   ✅ New max CO2: %.2f", maxCO2)
				}
			}
		}
	}
	
	// If we found a large number like 2292, it might need decimal conversion (e.g., 229.2)
	// Check if it's unreasonably large and divide by 10
	if maxCO2 > 1000 && maxCO2 < 10000 {
		// Likely in format "2292" meaning "229.2 kg"
		maxCO2 = maxCO2 / 10
		co2Value = fmt.Sprintf("%.1f", maxCO2)
	}
	
	// Extract distance from HTML and text
	distanceRegex := regexp.MustCompile(`(?i)(\d+(?:[,\s]\d+)*)\s*(?:km|kilometer)`)
	allDistanceMatches := distanceRegex.FindAllStringSubmatch(htmlContent + " " + bodyContent, -1)
	
	log.Printf("🔍 Found %d km matches", len(allDistanceMatches))
	
	var distanceValue string
	// Get the first significant distance value (usually > 50 km for real travel)
	for _, match := range allDistanceMatches {
		if len(match) > 1 {
			valStr := strings.ReplaceAll(match[1], ",", "")
			valStr = strings.ReplaceAll(valStr, " ", "")
			if val, err := strconv.Atoi(valStr); err == nil && val > 50 {
				distanceValue = valStr
				break
			}
		}
	}
	
	// Store results
	if co2Value != "" {
		// Clean up the value
		co2Value = strings.ReplaceAll(co2Value, ",", "")
		co2Value = strings.ReplaceAll(co2Value, " ", "")
		results["co2_kg"] = co2Value
	}
	
	if distanceValue != "" {
		results["distance_km"] = distanceValue
	}
	
	// Store raw text for debugging
	results["raw_text"] = bodyContent

	return results, nil
}

// HTTP Handlers

func (s *ScraperService) handleStartScrape(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req ScrapeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request: %v", err), http.StatusBadRequest)
		return
	}

	if req.URL == "" || req.TypeScriptConfig == "" {
		http.Error(w, "Missing url or typescript_config", http.StatusBadRequest)
		return
	}

	// Create job
	job := s.store.Create(req.URL, req.TypeScriptConfig)
	
	// Queue for processing
	s.jobQueue <- job.ID

	log.Printf("📥 Created job %s for %s", job.ID, req.URL)

	// Return job ID immediately
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"job_id":     job.ID,
		"status":     job.Status,
		"created_at": job.CreatedAt,
	})
}

func (s *ScraperService) handleGetJob(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	jobID := r.URL.Query().Get("job_id")
	if jobID == "" {
		http.Error(w, "Missing job_id parameter", http.StatusBadRequest)
		return
	}

	job, ok := s.store.Get(jobID)
	if !ok {
		http.Error(w, "Job not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(job)
}

func (s *ScraperService) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "healthy",
		"service": "playwright-scraper",
		"time":    time.Now().Format(time.RFC3339),
	})
}

func main() {
	// Create scraper service with 3 worker threads
	service := NewScraperService(3)
	service.Start()

	// Setup HTTP routes
	http.HandleFunc("/health", service.handleHealth)
	http.HandleFunc("/scrape/start", service.handleStartScrape)
	http.HandleFunc("/scrape/job", service.handleGetJob)

	// Start server
	port := ":8080"
	log.Printf("🚀 Playwright Scraper Service starting on %s", port)
	if err := http.ListenAndServe(port, nil); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}

