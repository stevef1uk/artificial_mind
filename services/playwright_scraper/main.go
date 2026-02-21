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
	"os"
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
	URL              string            `json:"url"`
	Instructions     string            `json:"instructions"`
	TypeScriptConfig string            `json:"typescript_config"`
	Extractions      map[string]string `json:"extractions,omitempty"`
	Variables        map[string]string `json:"variables,omitempty"`
	GetHTML          bool              `json:"get_html,omitempty"`
	Cookies          []pw.Cookie       `json:"cookies,omitempty"` // Session persistence
}

// ScrapeJob represents a scrape job in the queue
type ScrapeJob struct {
	ID               string                 `json:"id"`
	URL              string                 `json:"url"`
	Instructions     string                 `json:"instructions"`
	TypeScriptConfig string                 `json:"typescript_config"`
	Extractions      map[string]string      `json:"extractions,omitempty"`
	Variables        map[string]string      `json:"variables,omitempty"`
	GetHTML          bool                   `json:"get_html,omitempty"`
	Cookies          []pw.Cookie            `json:"cookies,omitempty"`
	Status           string                 `json:"status"`
	CreatedAt        time.Time              `json:"created_at"`
	StartedAt        *time.Time             `json:"started_at,omitempty"`
	CompletedAt      *time.Time             `json:"completed_at,omitempty"`
	Result           map[string]interface{} `json:"result,omitempty"`
	Error            string                 `json:"error,omitempty"`
}

// PlaywrightOperation represents a parsed operation from TypeScript config
type PlaywrightOperation struct {
	Type          string
	Selector      string
	Value         string
	Role          string
	RoleName      string
	Text          string
	TimeoutMS     int
	Index         int    // For nth(n) selectors
	ChildSelector string // For scoped selectors (e.g., locator().locator())
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

func (s *JobStore) Create(url, instructions, tsConfig string, extractions map[string]string, variables map[string]string, getHTML bool, cookies []pw.Cookie) *ScrapeJob {
	s.mu.Lock()
	defer s.mu.Unlock()

	job := &ScrapeJob{
		ID:               uuid.New().String(),
		URL:              url,
		Instructions:     instructions,
		TypeScriptConfig: tsConfig,
		Extractions:      extractions,
		Variables:        variables,
		GetHTML:          getHTML,
		Cookies:          cookies,
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

	resolvedConfig := applyTemplateVariables(job.TypeScriptConfig, job.Variables)
	// Parse TypeScript config
	operations, err := parseTypeScriptConfig(resolvedConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to parse TypeScript config: %v", err)
	}

	log.Printf("📋 Parsed %d operations", len(operations))

	// Execute Playwright operations
	return executePlaywrightOperations(job.URL, operations, job.Instructions, job.Extractions, job.GetHTML, job.Cookies)
}

func parseTypeScriptConfig(tsConfig string) ([]PlaywrightOperation, error) {
	var operations []PlaywrightOperation

	// Log first 500 chars of the config for debugging
	preview := tsConfig
	if len(preview) > 500 {
		preview = preview[:500] + "..."
	}
	log.Printf("📝 TypeScript config (first 500 chars): %s", preview)

	// Parse operations in order by finding all 'await page.' patterns
	// stop at semicolon, newline, or end of string
	awaitRegex := regexp.MustCompile(`await\s+page\.[^;\n]+`)
	matches := awaitRegex.FindAllString(tsConfig, -1)

	for _, match := range matches {
		op := parseOperation(match)
		if op.Type != "" {
			operations = append(operations, op)
		}
	}

	return operations, nil
}

func applyTemplateVariables(tsConfig string, variables map[string]string) string {
	if tsConfig == "" || len(variables) == 0 {
		return tsConfig
	}

	replacerArgs := make([]string, 0, len(variables)*4)
	for key, value := range variables {
		if key == "" {
			continue
		}
		replacerArgs = append(replacerArgs, "{{"+key+"}}", value)
		replacerArgs = append(replacerArgs, "{{ "+key+" }}", value)
		replacerArgs = append(replacerArgs, "${"+key+"}", value)
		replacerArgs = append(replacerArgs, "${ "+key+" }", value)
	}
	if len(replacerArgs) == 0 {
		return tsConfig
	}

	replacer := strings.NewReplacer(replacerArgs...)
	return replacer.Replace(tsConfig)
}

func parseOperation(line string) PlaywrightOperation {
	// goto
	if matches := regexp.MustCompile(`await\s+page\.goto\(['"](.+?)['"]\)`).FindStringSubmatch(line); len(matches) > 1 {
		return PlaywrightOperation{Type: "goto", Selector: matches[1]}
	}

	// getByRole (click)
	if matches := regexp.MustCompile(`await\s+page\.getByRole\(['"](\w+)['"],\s*\{\s*name:\s*['"](.+?)['"]\s*\}\)\.click\(\)`).FindStringSubmatch(line); len(matches) > 2 {
		return PlaywrightOperation{Type: "getByRole", Role: matches[1], RoleName: matches[2]}
	}

	// getByRole (fill)
	if matches := regexp.MustCompile(`await\s+page\.getByRole\(['"](\w+)['"],\s*\{\s*name:\s*['"](.+?)['"]\s*\}\)\.fill\(['"](.+?)['"]\)`).FindStringSubmatch(line); len(matches) > 3 {
		return PlaywrightOperation{Type: "getByRoleFill", Role: matches[1], RoleName: matches[2], Value: matches[3]}
	}

	// getByText with .first().click()
	if matches := regexp.MustCompile(`await\s+page\.getByText\(['"](.+?)['"]\)\.first\(\)\.click\(\)`).FindStringSubmatch(line); len(matches) > 1 {
		return PlaywrightOperation{Type: "getByText", Text: matches[1]}
	}

	// getByText with .click() (no .first())
	if matches := regexp.MustCompile(`await\s+page\.getByText\(['"](.+?)['"]\)\.click\(\)`).FindStringSubmatch(line); len(matches) > 1 {
		return PlaywrightOperation{Type: "getByTextClick", Text: matches[1]}
	}

	// page.fill() - direct fill on selector
	if matches := regexp.MustCompile(`await\s+page\.fill\(['"](.+?)['"]\s*,\s*['"](.+?)['"]\s*\)`).FindStringSubmatch(line); len(matches) > 2 {
		return PlaywrightOperation{Type: "locatorFill", Selector: matches[1], Value: matches[2]}
	}

	// page.click() - direct click on selector
	if matches := regexp.MustCompile(`await\s+page\.click\(['"](.+?)['"]\s*\)`).FindStringSubmatch(line); len(matches) > 1 {
		return PlaywrightOperation{Type: "locator", Selector: matches[1]}
	}

	// page.selectOption() - direct select on selector
	if matches := regexp.MustCompile(`await\s+page\.selectOption\(['"](.+?)['"]\s*,\s*['"](.+?)['"]\s*\)`).FindStringSubmatch(line); len(matches) > 2 {
		return PlaywrightOperation{Type: "locatorSelectOptionFirst", Selector: matches[1], Value: matches[2]}
	}

	// locator with .first().locator().fill()
	if matches := regexp.MustCompile(`await\s+page\.locator\(['"](.+?)['"]\)\.first\(\)\.locator\(['"](.+?)['"]\)\.fill\(['"](.+?)['"]\)`).FindStringSubmatch(line); len(matches) > 3 {
		return PlaywrightOperation{Type: "scopedLocatorFillFirst", Selector: matches[1], ChildSelector: matches[2], Value: matches[3]}
	}

	// locator with .nth(n).locator().fill()
	if matches := regexp.MustCompile(`await\s+page\.locator\(['"](.+?)['"]\)\.nth\((\d+)\)\.locator\(['"](.+?)['"]\)\.fill\(['"](.+?)['"]\)`).FindStringSubmatch(line); len(matches) > 4 {
		index, _ := strconv.Atoi(matches[2])
		return PlaywrightOperation{Type: "scopedLocatorFillNth", Selector: matches[1], Index: index, ChildSelector: matches[3], Value: matches[4]}
	}

	// locator with .first().locator().first().click() or .click()
	if matches := regexp.MustCompile(`await\s+page\.locator\(['"](.+?)['"]\)\.first\(\)\.locator\(['"](.+?)['"]\)(?:\.first\(\)|\.nth\(\d+\))?\.click\(\)`).FindStringSubmatch(line); len(matches) > 2 {
		return PlaywrightOperation{Type: "scopedLocatorClickFirst", Selector: matches[1], ChildSelector: matches[2]}
	}

	// locator with .nth(n).locator().first().click() or .click()
	if matches := regexp.MustCompile(`await\s+page\.locator\(['"](.+?)['"]\)\.nth\((\d+)\)\.locator\(['"](.+?)['"]\)(?:\.first\(\)|\.nth\(\d+\))?\.click\(\)`).FindStringSubmatch(line); len(matches) > 3 {
		index, _ := strconv.Atoi(matches[2])
		return PlaywrightOperation{Type: "scopedLocatorClickNth", Selector: matches[1], Index: index, ChildSelector: matches[3]}
	}

	// bypassConsent special command
	if strings.Contains(line, "bypassConsent") {
		return PlaywrightOperation{Type: "bypassConsent"}
	}

	// locator with .first().selectOption()
	if matches := regexp.MustCompile(`await\s+page\.locator\(['"](.+?)['"]\)\.first\(\)\.selectOption\(['"](.+?)['"]\)`).FindStringSubmatch(line); len(matches) > 2 {
		return PlaywrightOperation{Type: "locatorSelectOptionFirst", Selector: matches[1], Value: matches[2]}
	}

	// locator with .fill()
	if matches := regexp.MustCompile(`await\s+page\.locator\(['"](.+?)['"]\)\.fill\(['"](.+?)['"]\)`).FindStringSubmatch(line); len(matches) > 2 {
		return PlaywrightOperation{Type: "locatorFill", Selector: matches[1], Value: matches[2]}
	}

	// locator with .first().fill()
	if matches := regexp.MustCompile(`await\s+page\.locator\(['"](.+?)['"]\)\.first\(\)\.fill\(['"](.+?)['"]\)`).FindStringSubmatch(line); len(matches) > 2 {
		return PlaywrightOperation{Type: "locatorFillAtIndex", Selector: matches[1], Value: matches[2], Index: 0}
	}

	// locator with .nth(n).fill()
	if matches := regexp.MustCompile(`await\s+page\.locator\(['"](.+?)['"]\)\.nth\((\d+)\)\.fill\(['"](.+?)['"]\)`).FindStringSubmatch(line); len(matches) > 3 {
		index, _ := strconv.Atoi(matches[2])
		return PlaywrightOperation{Type: "locatorFillAtIndex", Selector: matches[1], Value: matches[3], Index: index}
	}

	// locator with .first().click()
	if matches := regexp.MustCompile(`await\s+page\.locator\(['"](.+?)['"]\)\.first\(\)\.click\(\)`).FindStringSubmatch(line); len(matches) > 1 {
		return PlaywrightOperation{Type: "locatorFirst", Selector: matches[1]}
	}

	// locator with .click() (no .first())
	if matches := regexp.MustCompile(`await\s+page\.locator\(['"](.+?)['"]\)\.click\(\)`).FindStringSubmatch(line); len(matches) > 1 {
		return PlaywrightOperation{Type: "locator", Selector: matches[1]}
	}

	// page.focus() - focus on selector
	if matches := regexp.MustCompile(`await\s+page\.focus\(['"](.+?)['"]\s*\)`).FindStringSubmatch(line); len(matches) > 1 {
		return PlaywrightOperation{Type: "locator", Selector: matches[1]}
	}

	// page.waitForNavigation() - wait for page load
	if strings.Contains(line, "waitForNavigation") {
		return PlaywrightOperation{Type: "wait", TimeoutMS: 3000}
	}

	// Promise.all() - skip, it will be handled as individual operations
	if strings.Contains(line, "Promise.all") {
		return PlaywrightOperation{}
	}

	// keyboard.press
	if matches := regexp.MustCompile(`await\s+page\.keyboard\.press\(['"]([^'"]+)['"]\)`).FindStringSubmatch(line); len(matches) > 1 {
		return PlaywrightOperation{Type: "keyboardPress", Value: matches[1]}
	}

	// keyboard.type
	if matches := regexp.MustCompile(`await\s+page\.keyboard\.type\(['"]([^'"]+)['"]\)`).FindStringSubmatch(line); len(matches) > 1 {
		return PlaywrightOperation{Type: "keyboardType", Value: matches[1]}
	}

	// waitForTimeout
	if matches := regexp.MustCompile(`await\s+page\.waitForTimeout\((\d+)\)`).FindStringSubmatch(line); len(matches) > 1 {
		var timeout int
		fmt.Sscanf(matches[1], "%d", &timeout)
		return PlaywrightOperation{Type: "wait", TimeoutMS: timeout}
	}

	return PlaywrightOperation{}
}

func collectFallbackValues(operations []PlaywrightOperation) ([]string, []string) {
	var fillValues []string
	var selectValues []string

	for _, op := range operations {
		switch op.Type {
		case "getByRoleFill", "locatorFill", "locatorFillAtIndex", "scopedLocatorFillFirst", "scopedLocatorFillNth":
			value := strings.TrimSpace(op.Value)
			if value != "" {
				fillValues = append(fillValues, value)
			}
		case "locatorSelectOptionFirst":
			value := strings.TrimSpace(op.Value)
			if value != "" {
				selectValues = append(selectValues, value)
			}
		}
	}

	return fillValues, selectValues
}

func applyGenericFillFallback(page pw.Page, fillValues []string, selectValues []string) (int, int, error) {
	if len(fillValues) == 0 && len(selectValues) == 0 {
		return 0, 0, nil
	}

	// Ensure slices are not nil - JavaScript needs arrays, not null
	if fillValues == nil {
		fillValues = []string{}
	}
	if selectValues == nil {
		selectValues = []string{}
	}

	result, err := page.Evaluate(`(values, selectValues) => {
		// Defensive: ensure inputs are arrays
		if (!Array.isArray(values)) values = [];
		if (!Array.isArray(selectValues)) selectValues = [];

		const isVisible = (el) => {
			const style = window.getComputedStyle(el);
			return style && style.display !== 'none' && style.visibility !== 'hidden' && el.offsetParent !== null;
		};

		const inputs = Array.from(document.querySelectorAll('input, textarea')).filter((el) => {
			if (!isVisible(el)) return false;
			const type = (el.getAttribute('type') || '').toLowerCase();
			if (['hidden', 'submit', 'button', 'checkbox', 'radio', 'file'].includes(type)) return false;
			if (el.disabled || el.readOnly) return false;
			return true;
		});

		let filled = 0;
		for (let i = 0; i < inputs.length && filled < values.length; i++) {
			const el = inputs[i];
			if (el.value && el.value.trim() !== '') continue;
			const v = values[filled];
			if (typeof v !== 'string' || v.trim() === '') continue;
			el.focus();
			el.value = v;
			el.dispatchEvent(new Event('input', { bubbles: true }));
			el.dispatchEvent(new Event('change', { bubbles: true }));
			filled++;
		}

		const selects = Array.from(document.querySelectorAll('select')).filter((el) => isVisible(el) && !el.disabled);
		let selected = 0;
		for (let i = 0; i < selects.length && selected < selectValues.length; i++) {
			const sel = selects[i];
			const target = selectValues[selected];
			let applied = false;
			if (typeof target === 'string' && target.trim() !== '') {
				const opt = Array.from(sel.options || []).find((o) => o.value === target || (o.textContent || '').trim() === target);
				if (opt) {
					sel.value = opt.value;
					applied = true;
				}
			}

			if (!applied && sel.options && sel.options.length > 1) {
				sel.selectedIndex = 1;
				applied = true;
			}

			if (applied) {
				sel.dispatchEvent(new Event('change', { bubbles: true }));
				selected++;
			}
		}

		return { filled, selected, inputs: inputs.length, selects: selects.length };
	}`, fillValues, selectValues)
	if err != nil {
		return 0, 0, fmt.Errorf("generic fallback evaluate failed: %v", err)
	}

	if result == nil {
		return 0, 0, fmt.Errorf("generic fallback evaluate returned nil result")
	}

	resultMap, ok := result.(map[string]interface{})
	if !ok {
		return 0, 0, fmt.Errorf("unexpected fallback result type: %T", result)
	}

	return asInt(resultMap["filled"]), asInt(resultMap["selected"]), nil
}

func asInt(value interface{}) int {
	switch v := value.(type) {
	case int:
		return v
	case int32:
		return int(v)
	case int64:
		return int(v)
	case float32:
		return int(v)
	case float64:
		return int(v)
	default:
		return 0
	}
}

func executePlaywrightOperations(url string, operations []PlaywrightOperation, instructions string, extractions map[string]string, getHTML bool, cookies []pw.Cookie) (map[string]interface{}, error) {
	// Start Playwright
	pwInstance, err := pw.Run()
	if err != nil {
		return nil, fmt.Errorf("failed to start Playwright: %v", err)
	}
	defer pwInstance.Stop()

	// Launch browser
	// Launch browser
	executablePath := os.Getenv("PLAYWRIGHT_EXECUTABLE_PATH")
	if executablePath == "" {
		// Try common paths
		commonPaths := []string{
			"/usr/bin/chromium",
			"/usr/bin/google-chrome",
			"/bin/google-chrome",
			"/usr/bin/chromium-browser",
		}
		for _, p := range commonPaths {
			if _, err := os.Stat(p); err == nil {
				executablePath = p
				break
			}
		}
	}

	launchOptions := pw.BrowserTypeLaunchOptions{
		Headless: pw.Bool(true),
	}
	if executablePath != "" {
		launchOptions.ExecutablePath = &executablePath
		log.Printf("🚀 Using browser executable: %s", executablePath)
	}

	browser, err := pwInstance.Chromium.Launch(launchOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to launch browser: %v", err)
	}
	defer browser.Close()

	// Create page
	page, err := browser.NewPage()
	if err != nil {
		return nil, fmt.Errorf("failed to create page: %v", err)
	}

	// Restore cookies if provided
	if len(cookies) > 0 {
		log.Printf("🍪 Restoring %d cookies for session...", len(cookies))
		var optionalCookies []pw.OptionalCookie
		for _, c := range cookies {
			// Create pointers for optional fields
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
			/*
				if c.SameSite != "None" {
					// samesite := pw.SameSiteAttribute(c.SameSite)
					// oc.SameSite = &samesite
				}
			*/
			optionalCookies = append(optionalCookies, oc)
		}

		if err := page.Context().AddCookies(optionalCookies); err != nil {
			log.Printf("⚠️ Failed to restore cookies: %v", err)
		}
	}
	defer page.Close()

	defaultTimeoutMS := 30000
	if envTimeout := os.Getenv("SCRAPE_ACTION_TIMEOUT_MS"); envTimeout != "" {
		if parsed, err := strconv.Atoi(envTimeout); err == nil && parsed > 0 {
			defaultTimeoutMS = parsed
		}
	}
	page.SetDefaultTimeout(float64(defaultTimeoutMS))

	waitUntil := pw.WaitUntilStateLoad
	if os.Getenv("SCRAPE_WAIT_UNTIL") == "networkidle" {
		waitUntil = pw.WaitUntilStateNetworkidle
	}

	if _, err := page.Goto(url, pw.PageGotoOptions{
		WaitUntil: waitUntil,
	}); err != nil {
		return nil, fmt.Errorf("failed to navigate: %v", err)
	}

	initialURL := page.URL()

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

		case "bypassConsent":
			log.Println("🍪 Attempting auto-consent bypass...")

			// DEBUG: Print all buttons found to understand why we miss them
			buttons, err := page.Locator("button, input[type='submit'], a[role='button']").All()
			if err == nil {
				log.Printf("🔍 Found %d interactive elements:", len(buttons))
				for i, btn := range buttons {
					txt, _ := btn.TextContent()
					name, _ := btn.GetAttribute("name")
					cls, _ := btn.GetAttribute("class")
					log.Printf("   [%d] Text='%s' Name='%s' Class='%s'", i, strings.TrimSpace(txt), name, cls)
				}
			}

			// 0. Special handling for Yahoo-style "Scroll to bottom" buttons
			scrollToBottomPatterns := []string{"(?i)aller à la fin", "(?i)scroll to bottom", "(?i)suivi"}
			for _, p := range scrollToBottomPatterns {
				re := regexp.MustCompile(p)
				locator := page.GetByRole(pw.AriaRole("button"), pw.PageGetByRoleOptions{Name: re}).First()
				if count, _ := locator.Count(); count > 0 {
					log.Printf("⬇️ Clicking 'Scroll to Bottom' button: %s", p)
					_ = locator.Click(pw.LocatorClickOptions{Timeout: pw.Float(1000)})
					time.Sleep(500 * time.Millisecond) // Give it a moment to enable the next button
				}
			}

			// 1. Try generic role-based buttons first
			patterns := []string{
				"(?i)accept", "(?i)agree", "(?i)continue", "(?i)allow", "(?i)ok", "(?i)yes",
				"(?i)accepter", "(?i)continuer", "(?i)autoriser", "(?i)j'accepte", "(?i)tout accepter",
				"(?i)akzeptieren", "(?i)zustimmen",
				"(?i)aceptar", "(?i)continuar", "tout équivalents", // Generic French
			}

			clicked := false
			for _, p := range patterns {
				// Use regexp for case insensitive matching
				re := regexp.MustCompile(p)
				locator := page.GetByRole(pw.AriaRole("button"), pw.PageGetByRoleOptions{Name: re}).First()

				if count, _ := locator.Count(); count > 0 {
					// Use Force: true to click even if covered by overlay
					if err := locator.Click(pw.LocatorClickOptions{Timeout: pw.Float(2000), Force: pw.Bool(true)}); err == nil {
						log.Printf("✅ Clicked consent button pattern: %s", p)
						clicked = true
						break
					}
				}
			}

			// 2. Fallback to specific selectors
			if !clicked {
				selectors := []string{
					"button[name='agree']",
					"button.accept-all",
					"input[type='submit'][value*='Accept']",
					"button[id*='accept']", "button[class*='accept']",
					"button[id*='agree']", "button[class*='agree']",
					"button[id*='continue']", "button[class*='continue']",
					"a[id*='accept']", "a[class*='accept']",
					"form[action*='consent'] input[type='submit']",
					// Catch-all: Primary submit button in a consent form
					"form[class*='consent'] button[type='submit']",
					".consent-container button[type='submit']",
				}
				for _, sel := range selectors {
					locator := page.Locator(sel).First()
					if count, _ := locator.Count(); count > 0 {
						if err := locator.Click(pw.LocatorClickOptions{Timeout: pw.Float(2000), Force: pw.Bool(true)}); err == nil {
							log.Printf("✅ Clicked consent selector: %s", sel)
							clicked = true
							break
						}
					}
				}
			}

			if clicked {
				log.Println("⏳ Waiting 5s for navigation after consent click...")
				time.Sleep(5 * time.Second)
			} else {
				log.Println("⚠️ No consent button found to click after trying all patterns")
			}

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

		case "locatorFillAtIndex":
			// locator().nth(i).fill()
			if err := page.Locator(op.Selector).Nth(op.Index).Fill(op.Value); err != nil {
				log.Printf("   ⚠️ Failed: %v", err)
			}
			time.Sleep(300 * time.Millisecond)

		case "wait":
			if op.TimeoutMS > 0 {
				time.Sleep(time.Duration(op.TimeoutMS) * time.Millisecond)
			} else {
				time.Sleep(500 * time.Millisecond)
			}

		case "scopedLocatorFillFirst":
			// locator(s1).first().locator(s2).fill(v)
			if err := page.Locator(op.Selector).First().Locator(op.ChildSelector).Fill(op.Value); err != nil {
				log.Printf("   ⚠️ Failed: %v", err)
			}
			time.Sleep(300 * time.Millisecond)

		case "scopedLocatorFillNth":
			// locator(s1).nth(n).locator(s2).fill(v)
			if err := page.Locator(op.Selector).Nth(op.Index).Locator(op.ChildSelector).Fill(op.Value); err != nil {
				log.Printf("   ⚠️ Failed: %v", err)
			}
			time.Sleep(300 * time.Millisecond)

		case "scopedLocatorClickFirst":
			// locator(s1).first().locator(s2).first().click() or .click()
			if err := page.Locator(op.Selector).First().Locator(op.ChildSelector).First().Click(); err != nil {
				log.Printf("   ⚠️ Failed: %v", err)
			}
			time.Sleep(300 * time.Millisecond)

		case "scopedLocatorClickNth":
			// locator(s1).nth(n).locator(s2).first().click() or .click()
			if err := page.Locator(op.Selector).Nth(op.Index).Locator(op.ChildSelector).First().Click(); err != nil {
				log.Printf("   ⚠️ Failed: %v", err)
			}
			time.Sleep(300 * time.Millisecond)

		case "keyboardPress":
			if err := page.Keyboard().Press(op.Value); err != nil {
				log.Printf("   ⚠️ Failed: %v", err)
			}
			time.Sleep(300 * time.Millisecond)

		case "keyboardType":
			if err := page.Keyboard().Type(op.Value); err != nil {
				log.Printf("   ⚠️ Failed: %v", err)
			}
			time.Sleep(300 * time.Millisecond)

		case "locatorSelectOptionFirst":
			// locator().first().selectOption(v)
			values := []string{op.Value}
			if _, err := page.Locator(op.Selector).First().SelectOption(pw.SelectOptionValues{Values: &values}, pw.LocatorSelectOptionOptions{Timeout: pw.Float(5000)}); err != nil {
				log.Printf("   ⚠️ Failed: %v", err)
			}
			time.Sleep(300 * time.Millisecond)
		}
	}

	fillValues, selectValues := collectFallbackValues(operations)
	if len(operations) > 0 && page.URL() == initialURL {
		resultCount, _ := page.Locator("[id*='result'], .result, .results, [data-testid*='result']").Count()
		log.Printf("🔍 Generic fallback check: fillValues=%d, selectValues=%d, resultCount=%d", len(fillValues), len(selectValues), resultCount)
		if resultCount == 0 && (len(fillValues) > 0 || len(selectValues) > 0) {
			filled, selected, err := applyGenericFillFallback(page, fillValues, selectValues)
			if err != nil {
				log.Printf("   ⚠️ Generic fallback fill failed: %v", err)
			} else if filled > 0 || selected > 0 {
				log.Printf("🛟 Generic fallback filled %d inputs and %d selects", filled, selected)
				time.Sleep(500 * time.Millisecond)
			}
		}
	}

	// Submit fallback with explicit timeout and snapshot on failure.
	if len(operations) > 0 && page.URL() == initialURL {
		formCount, _ := page.Locator("form").Count()
		submitCount, _ := page.Locator("input[type='submit'], button[type='submit'], .submit-form, .btn-primary").Count()
		resultCount, _ := page.Locator("[id*='result'], .result, .results, [data-testid*='result']").Count()
		if formCount > 0 && submitCount > 0 && resultCount == 0 {
			log.Printf("🧽 Submit fallback: URL unchanged (%s), attempting alternate submit paths", initialURL)
			fallbackDeadline := time.Now().Add(15 * time.Second)
			attempt := 0
			snapshotHTML := ""
			for attempt < 3 && time.Now().Before(fallbackDeadline) {
				attempt++
				switch attempt {
				case 1:
					if err := page.Locator("input[type='submit'], button[type='submit'], .submit-form, .btn-primary").First().Click(); err != nil {
						log.Printf("   ⚠️ Submit fallback click failed: %v", err)
					}
				case 2:
					if _, err := page.Evaluate(`() => { const f = document.querySelector('form'); if (f) f.submit(); }`); err != nil {
						log.Printf("   ⚠️ Submit fallback form.submit() failed: %v", err)
					}
				case 3:
					if err := page.Keyboard().Press("Enter"); err != nil {
						log.Printf("   ⚠️ Submit fallback Enter failed: %v", err)
					}
				}

				time.Sleep(1500 * time.Millisecond)
				snapshotHTML, _ = page.Content()
				if len(snapshotHTML) > 20000 {
					snapshotHTML = snapshotHTML[:20000] + "...(truncated)"
				}
				log.Printf("📸 Submit snapshot captured (%d chars)", len(snapshotHTML))

				if page.URL() != initialURL {
					break
				}
				resultCount, _ = page.Locator("[id*='result'], .result, .results, [data-testid*='result']").Count()
				if resultCount > 0 {
					break
				}
			}
			if page.URL() == initialURL {
				resultCount, _ = page.Locator("[id*='result'], .result, .results, [data-testid*='result']").Count()
				if resultCount == 0 {
					return nil, &SnapshotError{
						Err:  fmt.Errorf("submit fallback did not transition from %s within 15s", initialURL),
						HTML: snapshotHTML,
					}
				}
			}
		}
	}

	// Wait for final state
	page.WaitForLoadState(pw.PageWaitForLoadStateOptions{State: pw.LoadStateNetworkidle})

	// Wait for operations to fully complete
	time.Sleep(500 * time.Millisecond)

	// Generic wait for page stability (network idle)
	// This ensures dynamic content (like results) has loaded regardless of the domain
	log.Println("⏳ Waiting for network idle...")
	page.WaitForLoadState(pw.PageWaitForLoadStateOptions{State: pw.LoadStateNetworkidle})

	// Add a fixed safety buffer for client-side rendering
	// Complex SPAs often need a moment after network idle to render
	if getHTML {
		// Reduced wait for faster planning/initial fetch
		log.Printf("⏳ Waiting for final render (1 second)...")
		time.Sleep(1 * time.Second)
	} else {
		log.Println("⏳ Waiting for final render (3 seconds)...")
		time.Sleep(3000 * time.Millisecond)
	}

	// Extract results
	log.Println("📊 Extracting results...")

	results := make(map[string]interface{})

	// Capture cookies for session persistence
	if cookies, err := page.Context().Cookies(); err == nil {
		results["cookies"] = cookies
	}

	results["page_url"] = page.URL()
	results["page_title"], _ = page.Title()

	// Get the RAW content BEFORE cleanup for extractions
	// This preserves <script> tags that may contain JSON data
	rawHTML, _ := page.Content()

	// 1. Clean up the DOM to match what the Planner saw
	_, _ = page.Evaluate(`() => {
        const elements = document.querySelectorAll('script, style, svg, path, link, meta, noscript, iframe');
        elements.forEach(el => el.remove());
        
        // Remove hidden elements
        const all = document.querySelectorAll('*');
        all.forEach(el => {
            const style = window.getComputedStyle(el);
            if (style.display === 'none' || style.visibility === 'hidden' || style.opacity === '0') {
                el.remove();
            }
        });
    }`)

	// Get the cleaned content
	cleanedHTML, _ := page.Content()
	if getHTML {
		results["cleaned_html"] = cleanedHTML
	}

	// Prepare content for extraction
	// Use RAW HTML for extractions (preserves script tags with JSON)
	// Normalize all double quotes to single quotes to match the Planner's hint and LLM's expectation
	searchContent := rawHTML

	// Write to debug file to see EXACTLY what we are matching against
	_ = os.WriteFile("/tmp/scraper_debug_content.html", []byte(searchContent), 0644)

	// 2. Explicit Extractions (CSS or Regex)
	if extractions != nil {
		log.Printf("📊 Running %d explicit extractions...", len(extractions))
		for name, selector := range extractions {
			log.Printf("   🔍 Processing %s: %s", name, selector)

			// Try as CSS selector first
			locator := page.Locator(selector).First()
			if count, err := locator.Count(); err == nil && count > 0 {
				if text, err := locator.TextContent(); err == nil && strings.TrimSpace(text) != "" {
					results[name] = strings.TrimSpace(text)
					log.Printf("   ✅ Found CSS match for %s", name)
					continue
				}
			}

			// Fallback to Regex
			re, err := regexp.Compile("(?is)" + selector)
			if err == nil {
				allMatches := re.FindAllStringSubmatch(rawHTML, -1)
				if len(allMatches) > 0 {
					m := allMatches[0]
					firstMatch := ""
					if len(m) > 1 {
						firstMatch = strings.TrimSpace(m[1])
					} else {
						firstMatch = strings.TrimSpace(m[0])
					}
					log.Printf("   ✅ Found Regex match for %s", name)
					results[name] = firstMatch
				}
			} else {
				log.Printf("   ❌ No match or invalid selector for %s", name)
			}
		}
	}

	// 3. Smart Extraction (if instructions provided and no results yet)
	if instructions != "" && len(results) <= 3 { // 3 is basic fields: url, title, cookies
		log.Printf("🔍 Running smart extraction for goal: %s", instructions)
		// Try to find prices, dates, or emails if mentioned
		content, _ := page.InnerText("body")

		if strings.Contains(strings.ToLower(instructions), "price") {
			re := regexp.MustCompile(`\$?\d+\.?\d*|\€\d+\.?\d*|£\d+\.?\d*`)
			if m := re.FindString(content); m != "" {
				results["price_found"] = m
			}
		}

		if strings.Contains(strings.ToLower(instructions), "email") {
			re := regexp.MustCompile(`[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}`)
			if m := re.FindString(content); m != "" {
				results["email_found"] = m
			}
		}
	}

	// Store raw text for debugging (Disabled to reduce payload size)
	// results["raw_text"] = bodyContent

	return results, nil
}

// HTTP Handlers

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
	job := s.store.Create(jobURL, req.Instructions, req.TypeScriptConfig, req.Extractions, req.Variables, req.GetHTML, req.Cookies)

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

		// Launch browser
		browser, err := playwright.Chromium.Launch()
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

	// Create scraper service with 3 worker threads
	service := NewScraperService(3)
	service.Start()

	// Setup HTTP routes
	http.HandleFunc("/health", service.handleHealth)
	http.HandleFunc("/scrape/start", service.handleStartScrape)
	http.HandleFunc("/scrape/job", service.handleGetJob)

	// STAGE 2: Fix endpoint
	http.HandleFunc("/scrape/fix", service.handleFixJob)

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
