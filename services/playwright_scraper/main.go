// Playwright Scraper Service
// Copyright (c) 2026 Steven Fisher
//
// This software is licensed for non-commercial use only.
// Commercial use requires a separate license.
// See LICENSE file for full terms.

package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	pw "github.com/playwright-community/playwright-go"

	"agi/services/playwright_scraper/scraper_pkg"
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
	URL              string            `json:"url"`
	Instructions     string            `json:"instructions"`
	UserAgent        string            `json:"user_agent,omitempty"`
	Operations       string            `json:"operations,omitempty"`
	TypeScriptConfig string            `json:"typescript_config"`
	Extractions      map[string]string `json:"extractions,omitempty"`
	Variables        map[string]string `json:"variables,omitempty"`
	GetHTML          bool              `json:"get_html,omitempty"`
	FullPage         bool              `json:"full_page,omitempty"`
	Cookies          []pw.Cookie       `json:"cookies,omitempty"` // Session persistence
}

// ScrapeJob represents a scrape job in the queue
type ScrapeJob struct {
	ID               string                 `json:"id"`
	URL              string                 `json:"url"`
	Instructions     string                 `json:"instructions"`
	UserAgent        string                 `json:"user_agent,omitempty"`
	TypeScriptConfig string                 `json:"typescript_config"`
	Extractions      map[string]string      `json:"extractions,omitempty"`
	Variables        map[string]string      `json:"variables,omitempty"`
	GetHTML          bool                   `json:"get_html,omitempty"`
	FullPage         bool                   `json:"full_page,omitempty"`
	ScreenshotPath   string                 `json:"screenshot_path,omitempty"`
	Cookies          []pw.Cookie            `json:"cookies,omitempty"`
	Status           string                 `json:"status"`
	CreatedAt        time.Time              `json:"created_at"`
	StartedAt        *time.Time             `json:"started_at,omitempty"`
	CompletedAt      *time.Time             `json:"completed_at,omitempty"`
	Result           map[string]interface{} `json:"result,omitempty"`
	Error            string                 `json:"error,omitempty"`
}

// PlaywrightOperation is now imported from scraper_pkg
type PlaywrightOperation = scraper_pkg.PlaywrightOperation

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

func (s *JobStore) Create(url, instructions, userAgent, tsConfig string, extractions map[string]string, variables map[string]string, getHTML bool, cookies []pw.Cookie) *ScrapeJob {
	s.mu.Lock()
	defer s.mu.Unlock()

	job := &ScrapeJob{
		ID:               uuid.New().String(),
		URL:              url,
		Instructions:     instructions,
		UserAgent:        userAgent,
		TypeScriptConfig: tsConfig,
		Extractions:      extractions,
		Variables:        variables,
		GetHTML:          getHTML,
		FullPage:         true, // Default to true for better OCR
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
		func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("❌ Worker %d PANIC: %v", id, r)
					// Try to update job status if possible
					// s.store.UpdateStatus(jobID, JobStatusFailed)
				}
			}()

			job, ok := s.store.Get(jobID)
			if !ok {
				log.Printf("⚠️ Worker %d: Job %s not found", id, jobID)
				return
			}

			log.Printf("🔧 Worker %d: Processing job %s", id, jobID)
			s.store.UpdateStatus(jobID, JobStatusRunning)

			result, err := s.executeJob(job)
			if err != nil {
				log.Printf("❌ Worker %d: Job %s failed: %v", id, jobID, err)
				job.Status = JobStatusFailed
				job.Error = err.Error()

				// STAGE 2: CAPTURE SNAPSHOT
				if snapshotHTML, ok := err.(interface{ Snapshot() string }); ok {
					saveSnapshot(job.ID, snapshotHTML.Snapshot())
				} else if snapshotErr, ok := err.(*SnapshotError); ok {
					saveSnapshot(job.ID, snapshotErr.HTML)
				}
			} else {
				log.Printf("✅ Worker %d: Job %s completed", id, jobID)
				job.Status = JobStatusCompleted
				job.Result = result
			}

			s.store.Update(job)
		}()
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

	// Interpolate variables
	config := scraper_pkg.ApplyTemplateVariables(job.TypeScriptConfig, job.Variables)
	
	// Parse operations
	operations, err := scraper_pkg.ParseTypeScriptConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to parse script: %v", err)
	}

	return executePlaywrightOperations(job.URL, operations, job.Instructions, job.UserAgent, job.Extractions, job.GetHTML, job.FullPage, job.ScreenshotPath, job.Cookies)
}

func executePlaywrightOperations(url string, operations []PlaywrightOperation, instructions, userAgent string, extractions map[string]string, getHTML, fullPage bool, screenshotPath string, cookies []pw.Cookie) (map[string]interface{}, error) {
	// Start Playwright
	pwInstance, err := pw.Run()
	if err != nil {
		return nil, fmt.Errorf("failed to start Playwright: %v", err)
	}
	defer pwInstance.Stop()

	// Launch browser
	executablePath := os.Getenv("PLAYWRIGHT_EXECUTABLE_PATH")

	launchOptions := pw.BrowserTypeLaunchOptions{
		Headless: pw.Bool(true),
		Args: []string{
			"--no-sandbox",
			"--disable-setuid-sandbox",
			"--disable-dev-shm-usage",
			"--window-size=1920,1080",
			"--disable-blink-features=AutomationControlled",
		},
	}
	if executablePath != "" {
		launchOptions.ExecutablePath = &executablePath
	}

	browser, err := pwInstance.Chromium.Launch(launchOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to launch browser: %v", err)
	}
	defer browser.Close()

	// Create context with working resolution
	finalUA := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36"
	if userAgent != "" {
		finalUA = userAgent
	}

	browserCtx, err := browser.NewContext(pw.BrowserNewContextOptions{
		Viewport: &pw.Size{
			Width:  1920,
			Height: 1080,
		},
		UserAgent: pw.String(finalUA),
		Locale:    pw.String(func() string {
			if l := os.Getenv("SCRAPE_LOCALE"); l != "" {
				return l
			}
			return "en-GB"
		}()),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create context: %v", err)
	}
	defer browserCtx.Close()

	// Create page from context
	page, err := browserCtx.NewPage()
	if err != nil {
		return nil, fmt.Errorf("failed to create page: %v", err)
	}
	defer page.Close()

	// Restore cookies if provided
	if len(cookies) > 0 {
		var optionalCookies []pw.OptionalCookie
		for _, c := range cookies {
			name := c.Name
			val := c.Value
			domain := c.Domain
			path := c.Path
			httpOnly := c.HttpOnly
			secure := c.Secure
			expires := c.Expires

			oc := pw.OptionalCookie{
				Name:     name,
				Value:    val,
				Domain:   &domain,
				Path:     &path,
				HttpOnly: &httpOnly,
				Secure:   &secure,
				Expires:  &expires,
			}
			optionalCookies = append(optionalCookies, oc)
		}

		if err := page.Context().AddCookies(optionalCookies); err != nil {
			log.Printf("⚠️ Failed to restore cookies: %v", err)
		}
	}

	page.SetDefaultTimeout(60000)

	if _, err := page.Goto(url, pw.PageGotoOptions{
		WaitUntil: pw.WaitUntilStateDomcontentloaded,
	}); err != nil {
		return nil, fmt.Errorf("failed to navigate: %v", err)
	}

	// EXECUTE ENGINE
	logger := &scraper_pkg.SimpleLogger{}
	if err := scraper_pkg.ExecuteEngine(page, operations, logger); err != nil {
		log.Printf("⚠️ Engine execution partially failed: %v", err)
	}

	result := make(map[string]interface{})

	// Take screenshot
	var screenshot []byte
	screenshotOptions := pw.PageScreenshotOptions{FullPage: pw.Bool(fullPage)}
	if screenshotPath != "" {
		screenshotOptions.Path = pw.String(screenshotPath)
	}
	screenshot, err = page.Screenshot(screenshotOptions)
	if err == nil && screenshot != nil {
		result["screenshot"] = "data:image/png;base64," + base64.StdEncoding.EncodeToString(screenshot)
	}

	// Capture title and cleaned HTML for regression tests
	title, _ := page.Title()
	result["title"] = title

	if html, err := page.Content(); err == nil {
		result["html"] = html
		result["cleaned_html"] = scraper_pkg.CleanHTML(html)
	}

	// 2. Execute extractions (restored logic)
	goCtx := context.Background()
	extractedData := scraper_pkg.Extract(goCtx, page, instructions, extractions, logger)
	for k, v := range extractedData {
		result[k] = v
	}

	// Also make sure 'title' is in the result if extracted specifically
	if t, ok := extractedData["title"].(string); ok && t != "" {
		result["title"] = t
	}

	return result, nil
}


func (s *ScraperService) handleStartScrape(w http.ResponseWriter, r *http.Request) {
	if handleCORSPreflight(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req ScrapeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request: %v", err), http.StatusBadRequest)
		return
	}

	// Use URL from request, but fall back to extracting from config if missing
	jobURL := req.URL
	if jobURL == "" {
		// Attempt to extract from config (simple regex)
		re := regexp.MustCompile(`page\.goto\(['"](.*?)['"]`)
		matches := re.FindStringSubmatch(req.TypeScriptConfig)
		if len(matches) > 1 {
			jobURL = matches[1]
		}
	}

	if jobURL == "" {
		http.Error(w, "Missing url", http.StatusBadRequest)
		return
	}

	// Create job
	log.Printf("📥 ScrapeRequest received (Goal: %s, Script: %t)", req.Instructions, req.TypeScriptConfig != "")
	job := s.store.Create(jobURL, req.Instructions, req.UserAgent, req.TypeScriptConfig, req.Extractions, req.Variables, req.GetHTML, req.Cookies)

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
	if handleCORSPreflight(w, r) {
		return
	}
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

func (s *ScraperService) handleFixJob(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	jobID := r.URL.Query().Get("job_id")
	goal := r.URL.Query().Get("goal")

	if jobID == "" || goal == "" {
		http.Error(w, "Missing job_id or goal parameters", http.StatusBadRequest)
		return
	}

	job, ok := s.store.Get(jobID)
	if !ok {
		http.Error(w, "Job not found", http.StatusNotFound)
		return
	}

	if job.Status != JobStatusFailed {
		http.Error(w, "Job is not in failed state", http.StatusBadRequest)
		return
	}

	// Check for snapshot
	filename := fmt.Sprintf("failed_jobs/%s.html", jobID)
	htmlBytes, err := os.ReadFile(filename)
	if err != nil {
		http.Error(w, fmt.Sprintf("Snapshot not found: %v", err), http.StatusNotFound)
		return
	}

	// Invoke scrape planner logic (Stage 2)
	// For now, we stub this out as a log message because the planner is a CLI tool
	// In the future, we will link the planner package directly.
	log.Printf("🛠️  FIX REQUESTED for Job %s", jobID)
	log.Printf("   Goal: %s", goal)
	log.Printf("   Snapshot size: %d bytes", len(htmlBytes))

	// Placeholder response
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "fix_initiated",
		"job_id":  jobID,
		"message": "Fix logic is pending implementation (Stage 2)",
	})
}

// SnapshotError wraps an error with HTML snapshot
type SnapshotError struct {
	Err  error
	HTML string
}

func (e *SnapshotError) Error() string {
	return e.Err.Error()
}

func (e *SnapshotError) Snapshot() string {
	return e.HTML
}

func saveSnapshot(jobID, html string) {
	if html == "" {
		return
	}
	// Ensure directory exists
	if err := os.MkdirAll("failed_jobs", 0755); err != nil {
		log.Printf("⚠️ Failed to create failed_jobs dir: %v", err)
		return
	}
	filename := fmt.Sprintf("failed_jobs/%s.html", jobID)
	if err := os.WriteFile(filename, []byte(html), 0644); err != nil {
		log.Printf("⚠️ Failed to save snapshot for job %s: %v", jobID, err)
	} else {
		log.Printf("📸 Saved snapshot for failed job %s to %s", jobID, filename)
	}
}

func main() {
	// Initialize Playwright
	log.Println("🔧 Installing Playwright Chromium browser (one-time setup)...")
	// Only install chromium to save time and space, especially on RPI
	err := pw.Install(&pw.RunOptions{
		Browsers: []string{"chromium"},
	})
	if err != nil {
		log.Printf("⚠️  Playwright installation warning: %v (continuing anyway)", err)
	} else {
		log.Println("✅ Playwright Chromium installed")
	}

	// Start Playwright
	playwright, err := pw.Run()
	if err != nil {
		// Log warning but continue - some operations don't need browser
		log.Printf("⚠️  Playwright initialization warning: %v (some features unavailable)", err)
	} else {
		defer playwright.Stop()

		// Launch browser with stealth options
		browser, err := playwright.Chromium.Launch(pw.BrowserTypeLaunchOptions{
			Args: []string{
				"--disable-blink-features=AutomationControlled",
				"--no-sandbox",
				"--disable-setuid-sandbox",
				"--use-fake-ui-for-media-stream",
				"--use-fake-device-for-media-stream",
				"--disable-web-security",
				"--disable-features=IsolateOrigins,site-per-process",
			},
		})
		if err != nil {
			log.Printf("⚠️  Browser launch warning: %v (MyClimate/Generic scraper features unavailable)", err)
		} else {
			defer browser.Close()

			// Initialize logger
			logger := &SimpleServiceLogger{}

			// Initialize MyClimate handlers
			initMyClimateHandlers(http.DefaultServeMux, browser, logger)
			log.Println("✅ MyClimate handlers initialized")

			// Initialize generic scraper handlers
			initGenericHandlers(http.DefaultServeMux, browser, logger)
			log.Println("✅ Generic scraper handlers initialized")
		}
	}

	// Create scraper service — configurable via SCRAPER_WORKERS env var
	workerCount := 1 // Default: 1 worker (safe for ARM)
	if wStr := os.Getenv("SCRAPER_WORKERS"); wStr != "" {
		if w, err := strconv.Atoi(wStr); err == nil && w > 0 {
			workerCount = w
		}
	}
	log.Printf("🔧 Starting scraper with %d workers (set SCRAPER_WORKERS to override)", workerCount)
	service := NewScraperService(workerCount)
	service.Start()

	// Setup HTTP routes
	http.HandleFunc("/health", service.handleHealth)
	http.HandleFunc("/api/scraper/health", service.handleHealth)

	http.HandleFunc("/scrape/start", service.handleStartScrape)
	http.HandleFunc("/api/scraper/scrape/start", service.handleStartScrape)

	http.HandleFunc("/scrape/job", service.handleGetJob)
	http.HandleFunc("/api/scraper/scrape/job", service.handleGetJob)

	// STAGE 2: Fix endpoint
	http.HandleFunc("/scrape/fix", service.handleFixJob)
	http.HandleFunc("/api/scraper/scrape/fix", service.handleFixJob)

	// Start server
	port := os.Getenv("PORT")
	if port == "" {
		port = "8085"
	}
	if !strings.HasPrefix(port, ":") {
		port = ":" + port
	}
	log.Printf("🚀 Playwright Scraper Service starting on %s", port)
	if err := http.ListenAndServe(port, nil); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
