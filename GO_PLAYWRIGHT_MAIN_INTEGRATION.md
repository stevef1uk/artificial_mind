# Exact main.go Integration Code

## Where to Add the Code

Find this section in your `services/playwright_scraper/main.go` (around line 150-200):

```go
// FIND THIS SECTION:
func main() {
	log.Println("🎬 Starting Playwright Scraper Service...")

	// Install browsers if needed
	if err := pw.Install(); err != nil {
		log.Fatalf("Failed to install Playwright: %v", err)
	}

	// Launch browser
	browser, err := pw.Chromium.Launch()
	if err != nil {
		log.Fatalf("Failed to launch browser: %v", err)
	}
	defer browser.Close()
	log.Println("✅ Browser ready")

	// Create HTTP server
	mux := http.NewServeMux()
	jobStore := NewJobStore()

	// Existing handlers...
	mux.HandleFunc("/api/scrape", func(w http.ResponseWriter, r *http.Request) {
		handleScrapeRequest(w, r, jobStore, browser)
	})

	// YOUR CODE GOES HERE ⬇️
```

## Code to Add

**Add this code after the existing handlers (around line 200-220):**

```go
	// ============================================
	// NEW: Initialize MyClimate and Workflow handlers
	// ============================================
	logger := &SimpleServiceLogger{}
	initMyClimateHandlers(mux, browser, logger)
	log.Println("✅ MyClimate and Workflow handlers initialized")
	
	// ============================================
	// End of new code
	// ============================================

	// Start server
	port := ":8085"
	log.Printf("📡 Starting HTTP server on %s", port)
	if err := http.ListenAndServe(port, mux); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
```

---

## Complete main() Function Template

If you need to see a full working main function:

```go
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"
	pw "github.com/playwright-community/playwright-go"
)

// ============================================
// EXISTING CODE (your current implementation)
// ============================================

type ScrapeRequest struct {
	URL              string            `json:"url"`
	TypeScriptConfig string            `json:"typescript_config"`
	Extractions      map[string]string `json:"extractions,omitempty"`
	GetHTML          bool              `json:"get_html,omitempty"`
	Cookies          []pw.Cookie       `json:"cookies,omitempty"`
}

type ScrapeJob struct {
	ID               string                 `json:"id"`
	URL              string                 `json:"url"`
	TypeScriptConfig string                 `json:"typescript_config"`
	Extractions      map[string]string      `json:"extractions,omitempty"`
	GetHTML          bool                   `json:"get_html,omitempty"`
	Cookies          []pw.Cookie            `json:"cookies,omitempty"`
	Status           string                 `json:"status"`
	CreatedAt        time.Time              `json:"created_at"`
	StartedAt        *time.Time             `json:"started_at,omitempty"`
	CompletedAt      *time.Time             `json:"completed_at,omitempty"`
	Result           map[string]interface{} `json:"result,omitempty"`
	Error            string                 `json:"error,omitempty"`
}

type JobStore struct {
	mu   sync.RWMutex
	jobs map[string]*ScrapeJob
}

func NewJobStore() *JobStore {
	return &JobStore{
		jobs: make(map[string]*ScrapeJob),
	}
}

func (s *JobStore) Create(url, tsConfig string, extractions map[string]string, getHTML bool, cookies []pw.Cookie) *ScrapeJob {
	s.mu.Lock()
	defer s.mu.Unlock()

	job := &ScrapeJob{
		ID:               uuid.New().String(),
		URL:              url,
		TypeScriptConfig: tsConfig,
		Extractions:      extractions,
		GetHTML:          getHTML,
		Cookies:          cookies,
		Status:           "pending",
		CreatedAt:        time.Now(),
	}
	s.jobs[job.ID] = job
	return job
}

func (s *JobStore) Get(id string) *ScrapeJob {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.jobs[id]
}

// ============================================
// YOUR EXISTING HANDLERS (keep as-is)
// ============================================

func handleScrapeRequest(w http.ResponseWriter, r *http.Request, jobStore *JobStore, browser pw.Browser) {
	// Your existing implementation
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// ============================================
// MAIN FUNCTION
// ============================================

func main() {
	log.Println("🎬 Starting Playwright Scraper Service...")

	// Install browsers if needed
	if err := pw.Install(); err != nil {
		log.Fatalf("Failed to install Playwright: %v", err)
	}

	// Launch browser
	browser, err := pw.Chromium.Launch()
	if err != nil {
		log.Fatalf("Failed to launch browser: %v", err)
	}
	defer browser.Close()
	log.Println("✅ Browser ready (Chromium)")

	// Create HTTP server
	mux := http.NewServeMux()
	jobStore := NewJobStore()

	// ============================================
	// EXISTING HANDLERS
	// ============================================
	mux.HandleFunc("/api/scrape", func(w http.ResponseWriter, r *http.Request) {
		handleScrapeRequest(w, r, jobStore, browser)
	})

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
	})

	// ============================================
	// NEW: MyClimate and Workflow Handlers
	// ============================================
	logger := &SimpleServiceLogger{}
	initMyClimateHandlers(mux, browser, logger)
	log.Println("✅ MyClimate and Workflow handlers initialized")

	// ============================================
	// Start server
	// ============================================
	port := ":8085"
	log.Printf("📡 Starting HTTP server on %s", port)
	log.Println("   Available endpoints:")
	log.Println("   - POST /api/myclimate/flight (flight emissions)")
	log.Println("   - POST /api/workflow/execute (generic workflow)")
	log.Println("   - GET /api/workflows (list workflows)")
	log.Println("   - GET /api/myclimate/health (health check)")

	if err := http.ListenAndServe(port, mux); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
```

---

## Logger Implementation

**If you don't have a Logger interface, add this to handlers_myclimate.go:**

```go
// Logger interface for the scrapers
type Logger interface {
	Printf(format string, v ...interface{})
	Errorf(format string, v ...interface{})
}

// SimpleServiceLogger implements Logger
type SimpleServiceLogger struct{}

func (sl *SimpleServiceLogger) Printf(format string, v ...interface{}) {
	log.Printf("[INFO] "+format, v...)
}

func (sl *SimpleServiceLogger) Errorf(format string, v ...interface{}) {
	log.Printf("[ERROR] "+format, v...)
}
```

---

## Package Imports

Make sure your `main.go` has these imports:

```go
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"
	pw "github.com/playwright-community/playwright-go"
)
```

---

## go.mod Requirement

Verify your `services/playwright_scraper/go.mod` has the playwright dependency:

```go
require github.com/playwright-community/playwright-go v1.40.0
```

If missing:
```bash
cd services/playwright_scraper
go get github.com/playwright-community/playwright-go
```

---

## Testing Your Integration

After adding the code:

```bash
# Build
cd services/playwright_scraper
go build -o playwright_scraper .

# Run
./playwright_scraper

# In another terminal, test:
curl http://localhost:8085/api/myclimate/health

# Expected output:
# {"status":"ok"}

# Test scraper:
curl -X POST http://localhost:8085/api/myclimate/flight \
  -H "Content-Type: application/json" \
  -d '{"from":"CDG","to":"LHR"}' | jq .
```

---

## Minimal Working Example

**Smallest change to get this working:**

1. Copy 3 files to service directory
2. Add ONE line to main():

```go
initMyClimateHandlers(mux, browser, &SimpleServiceLogger{})
```

3. Add SimpleServiceLogger to your logger/logging package (4 lines)
4. Test!

**That's it. Total code change: <10 lines.**

---

## Next: Verify in HDN

Once the scraper service is running, you can call it from HDN:

```go
// In hdn/mcp_knowledge_server.go

import (
	"bytes"
	"encoding/json"
	"net/http"
)

func (s *Server) callMyClimateScraper(from, to string) (map[string]interface{}, error) {
	client := &http.Client{Timeout: 120 * time.Second}

	body, _ := json.Marshal(map[string]interface{}{
		"from": from,
		"to":   to,
		"passengers": 1,
		"cabin_class": "ECONOMY",
	})

	resp, err := client.Post(
		"http://localhost:8085/api/myclimate/flight",
		"application/json",
		bytes.NewReader(body),
	)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	return result, nil
}

// Use it anywhere:
// result, _ := s.callMyClimateScraper("CDG", "LHR")
// fmt.Println(result["emissions_kg_co2"])
```

---

## Verify Everything Works

Checklist:
- [ ] Code compiles: `go build`
- [ ] Service starts: `./playwright_scraper`
- [ ] Health check works: `curl http://localhost:8085/api/myclimate/health`
- [ ] Scraper works: `curl -X POST http://localhost:8085/api/myclimate/flight ...`
- [ ] Returns valid JSON with distance and emissions

**Done!** You now have a production-grade Go scraper integrated. 🎉
