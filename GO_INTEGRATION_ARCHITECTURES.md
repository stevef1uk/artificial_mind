# MYCLIMATE SCRAPER: Go Integration Architectures

## Overview
You have a working Python self-driving scraper. Here are 4 approaches to integrate it into your Go ecosystem, from simplest to most sophisticated.

---

## OPTION 1: Python Subprocess Wrapper (Quickest) ⚡
**Complexity:** Low | **Speed:** Immediate | **Best for:** Quick integration

### Concept
Go service spawns the Python scraper as subprocess, parses JSON output.

### Implementation
```go
package main

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

type FlightResult struct {
	Status      string `json:"status"`
	From        string `json:"from"`
	To          string `json:"to"`
	DistanceKm  string `json:"distance_km"`
	EmissionsCO2 string `json:"emissions_kg_co2"`
}

func scrapeFlightGo(from, to string) (*FlightResult, error) {
	cmd := exec.Command(
		"python3", 
		"/path/to/myclimate_self_driving_scraper.py",
		from, to, "--headless",
	)
	
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("scraper failed: %w", err)
	}
	
	// Extract JSON from output (remove logging prefix)
	jsonStart := strings.Index(string(output), "{")
	if jsonStart == -1 {
		return nil, fmt.Errorf("no JSON in output")
	}
	
	var result FlightResult
	if err := json.Unmarshal(output[jsonStart:], &result); err != nil {
		return nil, fmt.Errorf("parse failed: %w", err)
	}
	
	return &result, nil
}

// Use it:
// result, _ := scrapeFlightGo("CDG", "LHR")
// fmt.Printf("Distance: %s km, Emissions: %s t CO2\n", result.DistanceKm, result.EmissionsCO2)
```

**Pros:**
- ✅ Minimal changes needed
- ✅ Reuses proven Python code
- ✅ Can integrate today

**Cons:**
- ❌ Python dependency required
- ❌ Subprocess overhead per call
- ❌ No type safety for workflow variations

---

## OPTION 2: Go-Playwright Direct Implementation (Native) 🎯
**Complexity:** Medium | **Speed:** 2-3 days | **Best for:** Long-term, no dependencies

### Concept
Rewrite scraper logic directly in Go using `go-playwright` library.

### Implementation
Create `myclimate_scraper.go`:

```go
package scraper

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"time"

	pw "github.com/playwright-community/playwright-go"
)

type FlightResult struct {
	Status         string `json:"status"`
	From           string `json:"from"`
	To             string `json:"to"`
	DistanceKm     string `json:"distance_km"`
	EmissionsCO2   string `json:"emissions_kg_co2"`
	Error          string `json:"error,omitempty"`
}

type MyclimateScraper struct {
	browser pw.Browser
	logger  Logger
}

func NewMyclimateScraper(ctx context.Context, debug bool) (*MyclimateScraper, error) {
	// Install browsers if needed
	if err := pw.Install(); err != nil {
		return nil, err
	}
	
	// Launch browser
	browser, err := pw.Chromium.Launch(pw.BrowserLaunchOptions{
		Headless: pw.Bool(!debug),
	})
	if err != nil {
		return nil, err
	}
	
	return &MyclimateScraper{browser: browser}, nil
}

func (s *MyclimateScraper) ScrapeFlightEmissions(ctx context.Context, from, to string) (*FlightResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	
	result := &FlightResult{
		From: from,
		To:   to,
	}
	
	// Create page
	page, err := s.browser.NewPage()
	if err != nil {
		result.Status = "error"
		result.Error = fmt.Sprintf("page creation: %v", err)
		return result, err
	}
	defer page.Close()
	
	// Step 1: Navigate
	fmt.Printf("[1/8] 📄 Loading calculator page...\n")
	if _, err := page.Goto("https://co2.myclimate.org/en/flight_calculators/new", 
		pw.PageGotoOptions{WaitUntil: pw.WaitUntilStateNetworkidle}); err != nil {
		result.Status = "error"
		result.Error = fmt.Sprintf("navigation: %v", err)
		return result, err
	}
	page.WaitForTimeout(2000)
	
	// Step 2: Dismiss consent
	fmt.Printf("[2/8] 🔐 Checking for consent dialog...\n")
	acceptBtn := page.Locator(`button:has-text("Accept"), button[aria-label*="Close"]`)
	visible, _ := acceptBtn.IsVisible()
	if visible {
		acceptBtn.Click()
		page.WaitForTimeout(1000)
		fmt.Printf("      ✅ Dismissed consent dialog\n")
	}
	
	// Step 3: Fill FROM
	fmt.Printf("[3/8] 🛫 Filling departure airport: %s\n", from)
	fromInput := page.Locator(`input[id="flight_calculator_from"]`)
	if err := fromInput.Fill(from); err != nil {
		result.Status = "error"
		result.Error = fmt.Sprintf("from input: %v", err)
		return result, err
	}
	fromInput.Press("ArrowDown")
	time.Sleep(300 * time.Millisecond)
	fromInput.Press("Enter")
	page.WaitForTimeout(1500)
	fmt.Printf("      ✅ Selected first option\n")
	
	// Step 4: Fill TO
	fmt.Printf("[4/8] 🛬 Filling arrival airport: %s\n", to)
	toInput := page.Locator(`input[id="flight_calculator_to"]`)
	if err := toInput.Fill(to); err != nil {
		result.Status = "error"
		result.Error = fmt.Sprintf("to input: %v", err)
		return result, err
	}
	toInput.Press("ArrowDown")
	time.Sleep(300 * time.Millisecond)
	toInput.Press("Enter")
	page.WaitForTimeout(1500)
	fmt.Printf("      ✅ Selected first option\n")
	
	// Step 5: Submit form
	fmt.Printf("[6/8] 📤 Submitting form...\n")
	submitBtn := page.Locator(`button[type="submit"]`)
	if visible, _ := submitBtn.IsVisible(); visible {
		submitBtn.Click()
	} else {
		toInput.Press("Enter")
	}
	page.WaitForTimeout(3000)
	fmt.Printf("      ✅ Form submitted\n")
	
	// Step 6: Extract results
	fmt.Printf("[7/8] 📊 Extracting results...\n")
	content, _ := page.Content()
	
	// Extract distance
	distRegex := regexp.MustCompile(`(\d+[\d\.]*)\s*km`)
	if match := distRegex.FindStringSubmatch(content); match != nil {
		result.DistanceKm = match[1]
		fmt.Printf("      ✅ Found distance: %s km\n", result.DistanceKm)
	}
	
	// Extract emissions
	emissionsRegex := regexp.MustCompile(`CO₂\s*amount[:\s]*(\d+[\d\.]*)\s*t`)
	if match := emissionsRegex.FindStringSubmatch(content); match != nil {
		result.EmissionsCO2 = match[1]
		fmt.Printf("      ✅ Found emissions: %s t CO2\n", result.EmissionsCO2)
	}
	
	result.Status = "success"
	return result, nil
}

func (s *MyclimateScraper) Close() error {
	return s.browser.Close()
}
```

**Usage:**
```go
// In your service
scraper, _ := NewMyclimateScraper(ctx, false)
defer scraper.Close()

result, _ := scraper.ScrapeFlightEmissions(ctx, "CDG", "LHR")
fmt.Printf("%+v\n", result)
```

**Pros:**
- ✅ Pure Go, no dependencies
- ✅ Type-safe
- ✅ Faster (no subprocess overhead)
- ✅ Better error handling

**Cons:**
- ⚠️ Requires go-playwright library (still Chromium dependency)
- ⚠️ Code duplication from Python version
- ⚠️ More development time

---

## OPTION 3: Parameterized Scraper Framework (Most General) 🧩
**Complexity:** High | **Speed:** 1 week | **Best for:** Multiple sites, extensibility

### Concept
Define scraping workflows as structured data (YAML/JSON), generic Go engine executes them.

### Workflow Definition
```yaml
# scrapingworkflows/myclimate.yaml
name: "MyClimate Flight Calculator"
url: "https://co2.myclimate.org/en/flight_calculators/new"
waitUntil: "networkidle"

steps:
  - name: "Dismiss Consent"
    action: "click"
    selector: 'button:has-text("Accept")'
    optional: true
    timeout: 2000

  - name: "Fill From Airport"
    action: "fill"
    selector: 'input[id="flight_calculator_from"]'
    value: "${from}"
    
  - name: "Select From Dropdown"
    action: "keyboard"
    selector: 'input[id="flight_calculator_from"]'
    keys: ["ArrowDown", "Enter"]
    wait: 1500

  - name: "Fill To Airport"
    action: "fill"
    selector: 'input[id="flight_calculator_to"]'
    value: "${to}"

  - name: "Select To Dropdown"
    action: "keyboard"
    selector: 'input[id="flight_calculator_to"]'
    keys: ["ArrowDown", "Enter"]
    wait: 1500

  - name: "Submit Form"
    action: "keyboard"
    selector: 'input[id="flight_calculator_to"]'
    keys: ["Enter"]
    wait: 3000

extractions:
  distance_km:
    pattern: '(\d+[\d\.]*)\s*km'
    type: "string"
    
  emissions_co2:
    pattern: 'CO₂\s*amount[:\s]*(\d+[\d\.]*)\s*t'
    type: "string"
```

### Go Engine
```go
package scraper

import (
	"context"
	"fmt"
	"regexp"
	"time"
	"gopkg.in/yaml.v3"
	pw "github.com/playwright-community/playwright-go"
)

type WorkflowStep struct {
	Name     string   `yaml:"name"`
	Action   string   `yaml:"action"`     // click, fill, keyboard, wait
	Selector string   `yaml:"selector"`
	Value    string   `yaml:"value"`
	Keys     []string `yaml:"keys"`
	Wait     int      `yaml:"wait"`
	Optional bool     `yaml:"optional"`
	Timeout  int      `yaml:"timeout"`
}

type Extraction struct {
	Pattern string `yaml:"pattern"`
	Type    string `yaml:"type"`
}

type ScrapingWorkflow struct {
	Name        string                   `yaml:"name"`
	URL         string                   `yaml:"url"`
	WaitUntil   string                   `yaml:"waitUntil"`
	Steps       []WorkflowStep           `yaml:"steps"`
	Extractions map[string]Extraction    `yaml:"extractions"`
}

type WorkflowEngine struct {
	browser pw.Browser
}

func (e *WorkflowEngine) ExecuteWorkflow(ctx context.Context, workflow *ScrapingWorkflow, params map[string]string) (map[string]interface{}, error) {
	page, err := e.browser.NewPage()
	if err != nil {
		return nil, err
	}
	defer page.Close()
	
	// Navigate
	fmt.Printf("📄 Loading %s...\n", workflow.URL)
	if _, err := page.Goto(workflow.URL, 
		pw.PageGotoOptions{WaitUntil: pw.WaitUntilState(workflow.WaitUntil)}); err != nil {
		return nil, err
	}
	
	// Execute steps
	for i, step := range workflow.Steps {
		fmt.Printf("[%d/%d] 🔄 %s...\n", i+1, len(workflow.Steps), step.Name)
		
		// Replace parameters in selector and value
		selector := interpolateString(step.Selector, params)
		value := interpolateString(step.Value, params)
		
		locator := page.Locator(selector)
		
		switch step.Action {
		case "click":
			if step.Optional {
				if visible, _ := locator.IsVisible(); visible {
					locator.Click()
				}
			} else {
				locator.Click()
			}
			
		case "fill":
			locator.Fill(value)
			
		case "keyboard":
			for _, key := range step.Keys {
				locator.Press(key)
				time.Sleep(100 * time.Millisecond)
			}
			
		case "wait":
			time.Sleep(time.Duration(step.Wait) * time.Millisecond)
		}
		
		if step.Wait > 0 {
			time.Sleep(time.Duration(step.Wait) * time.Millisecond)
		}
	}
	
	// Extract results
	content, _ := page.Content()
	results := make(map[string]interface{})
	
	for key, extraction := range workflow.Extractions {
		regex := regexp.MustCompile(extraction.Pattern)
		if match := regex.FindStringSubmatch(content); match != nil {
			results[key] = match[1]
		}
	}
	
	return results, nil
}

func interpolateString(s string, params map[string]string) string {
	result := s
	for k, v := range params {
		result = strings.ReplaceAll(result, "${"+k+"}", v)
	}
	return result
}
```

**Usage:**
```go
workflow := loadWorkflow("myclimate.yaml")
engine := NewWorkflowEngine(browser)
results, _ := engine.ExecuteWorkflow(ctx, workflow, map[string]string{
	"from": "CDG",
	"to":   "LHR",
})
```

**Pros:**
- ✅ Define scraping workflows without coding
- ✅ Reusable across multiple sites
- ✅ Easy to maintain and modify
- ✅ Non-developers can create workflows

**Cons:**
- ⚠️ Significant upfront development
- ⚠️ Limited expressiveness (complex logic harder)
- ⚠️ Debugging harder than direct code

---

## OPTION 4: Distributed gRPC Service (Enterprise) 🏛️
**Complexity:** High | **Speed:** 2 weeks | **Best for:** Multi-tenant, high throughput

### Concept
Run scraper as standalone service, expose via gRPC API. Separates scraping from orchestration.

### Proto Definition
```protobuf
// flight_scraper.proto
service FlightScraper {
  rpc ScrapeFlightEmissions(FlightRequest) returns (FlightResult) {}
  rpc ExecuteWorkflow(WorkflowRequest) returns (WorkflowResult) {}
}

message FlightRequest {
  string from_airport = 1;
  string to_airport = 2;
  int32 passengers = 3;
  string cabin_class = 4;
}

message FlightResult {
  string status = 1;
  string distance_km = 2;
  string emissions_kg_co2 = 3;
  string error = 4;
}

message WorkflowRequest {
  string workflow_name = 1;
  map<string, string> parameters = 2;
}

message WorkflowResult {
  string status = 1;
  bytes output = 2;  // JSON-encoded results
}
```

### Server
```go
package main

import (
	"context"
	"net"
	"google.golang.org/grpc"
	pb "your_module/pb"
)

type scrapeServer struct {
	pb.UnimplementedFlightScraperServer
	scraper *MyclimateScraper
}

func (s *scrapeServer) ScrapeFlightEmissions(ctx context.Context, req *pb.FlightRequest) (*pb.FlightResult, error) {
	result, err := s.scraper.ScrapeFlightEmissions(ctx, req.FromAirport, req.ToAirport)
	return &pb.FlightResult{
		Status:         result.Status,
		DistanceKm:     result.DistanceKm,
		EmissionsCo2:   result.EmissionsCO2,
	}, err
}

func main() {
	listener, _ := net.Listen("tcp", ":50051")
	server := grpc.NewServer()
	pb.RegisterFlightScraperServer(server, &scrapeServer{})
	server.Serve(listener)
}
```

**Pros:**
- ✅ Fully decoupled service
- ✅ Scalable across machines
- ✅ Can run multiple instances
- ✅ Language-agnostic clients

**Cons:**
- ⚠️ Operational complexity
- ⚠️ Network overhead
- ⚠️ More moving parts

---

## RECOMMENDATION: Hybrid Approach 🎯

**Phase 1 (Now):** Option 1 - Python subprocess wrapper
- Quick integration with HDN
- Proven working code
- Get value immediately

**Phase 2 (Sprint 1):** Option 2 - Go-Playwright direct implementation
- Migrate critical paths to native Go
- Eliminate Python dependency
- Better performance

**Phase 3 (Sprint 2+):** Option 3 - Parameterized framework
- Create reusable for multiple sites (news scrapers, pricing, etc.)
- Build workflow library
- Enable non-developers to create scraping tasks

**Phase 4 (if needed):** Option 4 - gRPC service
- Only if you need distributed scraping or multi-tenant isolation

---

## Integration Points with HDN

### Where in Architecture?
```
┌─────────────────────────────────────────┐
│  HDN Orchestrator (Go)                  │
│  - Plans scraping tasks                 │
│  - Manages job queue                    │
└──────────────┬──────────────────────────┘
               │
     ┌─────────▼────────────┐
     │  Scraper Service      │  ← YOUR NEW CAPABILITY
     │  (Option 1-4)         │
     │  - Executes workflows │
     │  - Returns JSON       │
     └─────────┬────────────┘
               │
     ┌─────────▼────────────┐
     │  Playwright Browser   │
     │  (Chromium)           │
     └──────────────────────┘
```

### Integration Code (HDN)
```go
// In your scraper service
import "your_module/scraper"

func (s *ScraperService) HandleJob(ctx context.Context, job *Job) {
	switch job.Type {
	case "myclimate_flight":
		result, err := ScrapeMyClimateFlight(ctx, job.Params["from"], job.Params["to"])
		// Store result, return to HDN
		
	case "workflow":
		engine := NewWorkflowEngine(s.browser)
		result, err := engine.ExecuteWorkflow(ctx, job.Workflow, job.Params)
		// Store result, return to HDN
	}
}
```

---

**Which option interests you most?** I can dive deeper into implementation details or create working code for any of them.
