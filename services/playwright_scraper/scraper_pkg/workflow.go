package scraper_pkg

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	pw "github.com/playwright-community/playwright-go"
)

// WorkflowAction represents a single action in a scraping workflow
type WorkflowAction struct {
	Name     string   `json:"name"`
	Action   string   `json:"action"` // click, fill, keyboard, wait, extract
	Selector string   `json:"selector"`
	Value    string   `json:"value"`
	Keys     []string `json:"keys"`
	Wait     int      `json:"wait"`
	Optional bool     `json:"optional"`
	Timeout  int      `json:"timeout"`
}

// ExtractionRule defines how to extract data from the page
type ExtractionRule struct {
	Pattern string `json:"pattern"`
	Type    string `json:"type"` // string, number, array, object
}

// WorkflowDefinition describes a complete scraping workflow
type WorkflowDefinition struct {
	Name        string                    `json:"name"`
	Description string                    `json:"description"`
	URL         string                    `json:"url"`
	WaitUntil   string                    `json:"wait_until"` // load, domcontentloaded, networkidle
	Actions     []WorkflowAction          `json:"actions"`
	Extractions map[string]ExtractionRule `json:"extractions"`
}

// WorkflowResult represents the result of executing a workflow
type WorkflowResult struct {
	Status        string                 `json:"status"`
	Results       map[string]interface{} `json:"results,omitempty"`
	Error         string                 `json:"error,omitempty"`
	ExecutionTime int                    `json:"execution_time_ms"`
	HTML          string                 `json:"html,omitempty"`
	URL           string                 `json:"final_url"`
}

// WorkflowExecutor executes scraping workflows
type WorkflowExecutor struct {
	browser pw.Browser
	logger  Logger
	timeout time.Duration
}

// NewWorkflowExecutor creates a new workflow executor
func NewWorkflowExecutor(browser pw.Browser, logger Logger) *WorkflowExecutor {
	if logger == nil {
		logger = &SimpleLogger{}
	}
	return &WorkflowExecutor{
		browser: browser,
		logger:  logger,
		timeout: 120 * time.Second,
	}
}

// Execute runs a workflow and returns the results
func (we *WorkflowExecutor) Execute(ctx context.Context, workflow *WorkflowDefinition, params map[string]string) (*WorkflowResult, error) {
	startTime := time.Now()

	result := &WorkflowResult{
		Results: make(map[string]interface{}),
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(ctx, we.timeout)
	defer cancel()

	page, err := we.browser.NewPage()
	if err != nil {
		result.Status = "error"
		result.Error = fmt.Sprintf("page creation: %v", err)
		return result, err
	}
	defer page.Close()

	// Navigate to URL
	we.logger.Printf("📄 Loading: %s", workflow.URL)
	waitUntil := pw.WaitUntilStateLoad
	switch strings.ToLower(workflow.WaitUntil) {
	case "domcontentloaded":
		waitUntil = pw.WaitUntilStateDomcontentloaded
	case "networkidle":
		waitUntil = pw.WaitUntilStateNetworkidle
	}

	if _, err := page.Goto(workflow.URL, pw.PageGotoOptions{WaitUntil: waitUntil}); err != nil {
		result.Status = "error"
		result.Error = fmt.Sprintf("navigation: %v", err)
		return result, err
	}

	// Execute actions
	for i, action := range workflow.Actions {
		we.logger.Printf("[%d/%d] 🔄 %s", i+1, len(workflow.Actions), action.Name)

		// Interpolate parameters in selector and value
		selector := we.interpolateParams(action.Selector, params)
		value := we.interpolateParams(action.Value, params)

		if err := we.executeAction(page, action, selector, value); err != nil {
			if !action.Optional {
				result.Status = "error"
				result.Error = fmt.Sprintf("action %d (%s) failed: %v", i, action.Name, err)
				we.logger.Errorf("Action failed: %v", err)
				return result, err
			}
			we.logger.Printf("      ⚠️  Optional action skipped: %v", err)
		}
	}

	// Get final URL
	finalURL := page.URL()
	result.URL = finalURL

	// Extract results
	if workflow.Extractions != nil && len(workflow.Extractions) > 0 {
		we.logger.Printf("📊 Extracting data...")
		content, _ := page.Content()

		for key, rule := range workflow.Extractions {
			we.logger.Printf("  Extract: %s", key)
			value := we.extract(content, &rule)
			result.Results[key] = value
		}
	}

	result.Status = "success"
	result.ExecutionTime = int(time.Since(startTime).Milliseconds())

	return result, nil
}

func (we *WorkflowExecutor) executeAction(page pw.Page, action WorkflowAction, selector, value string) error {
	timeout := time.Duration(action.Timeout) * time.Millisecond
	if timeout == 0 {
		timeout = 5 * time.Second
	}

	switch strings.ToLower(action.Action) {
	case "click":
		locator := page.Locator(selector)
		if action.Optional {
			isVisible, _ := locator.IsVisible()
			if !isVisible {
				return fmt.Errorf("element not visible")
			}
		}
		return locator.Click()

	case "fill":
		locator := page.Locator(selector)
		if err := locator.Fill(value); err != nil {
			return err
		}
		we.logger.Printf("      ✅ Filled: %s", action.Name)

	case "keyboard":
		locator := page.Locator(selector)
		for _, key := range action.Keys {
			if err := locator.Press(key); err != nil {
				return err
			}
			time.Sleep(100 * time.Millisecond)
		}
		we.logger.Printf("      ✅ Keyboard: %v", action.Keys)

	case "wait":
		page.WaitForTimeout(float64(action.Wait))
		we.logger.Printf("      ✅ Waited %dms", action.Wait)

	case "screenshot":
		// Optional: take screenshot for debugging
		_, err := page.Screenshot(pw.PageScreenshotOptions{
			Path: pw.String(fmt.Sprintf("/tmp/screenshot_%d.png", time.Now().Unix())),
		})
		if err != nil {
			we.logger.Printf("      ⚠️  Screenshot failed: %v", err)
		}

	default:
		return fmt.Errorf("unknown action: %s", action.Action)
	}

	if action.Wait > 0 {
		page.WaitForTimeout(float64(action.Wait))
	}

	return nil
}

func (we *WorkflowExecutor) interpolateParams(s string, params map[string]string) string {
	result := s
	for key, value := range params {
		placeholder := "${" + key + "}"
		result = strings.ReplaceAll(result, placeholder, value)
	}
	return result
}

// LoadWorkflowFromJSON loads a workflow definition from JSON
func LoadWorkflowFromJSON(jsonData []byte) (*WorkflowDefinition, error) {
	var workflow WorkflowDefinition
	if err := json.Unmarshal(jsonData, &workflow); err != nil {
		return nil, err
	}
	return &workflow, nil
}

func (we *WorkflowExecutor) extract(content string, rule *ExtractionRule) interface{} {
	regex, err := regexp.Compile(rule.Pattern)
	if err != nil {
		we.logger.Errorf("Invalid regex: %v", err)
		return nil
	}

	switch strings.ToLower(rule.Type) {
	case "number":
		matches := regex.FindStringSubmatch(content)
		if len(matches) > 1 {
			return matches[1]
		}
		return nil

	case "array":
		matches := regex.FindAllStringSubmatch(content, -1)
		results := make([]string, 0)
		for _, match := range matches {
			if len(match) > 1 {
				results = append(results, match[1])
			}
		}
		return results

	default: // string
		matches := regex.FindStringSubmatch(content)
		if len(matches) > 1 {
			return matches[1]
		}
		return nil
	}
}
