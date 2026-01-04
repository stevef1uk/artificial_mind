package main

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// ContentSafetyManager handles URL filtering and content safety checks
type ContentSafetyManager struct {
	blockedDomains    map[string]bool
	blockedKeywords   []*regexp.Regexp
	allowedDomains    map[string]bool
	adultKeywords     []*regexp.Regexp
	maliciousPatterns []*regexp.Regexp
}

// NewContentSafetyManager creates a new content safety manager
func NewContentSafetyManager() *ContentSafetyManager {
	cs := &ContentSafetyManager{
		blockedDomains:    make(map[string]bool),
		allowedDomains:    make(map[string]bool),
		blockedKeywords:   []*regexp.Regexp{},
		adultKeywords:     []*regexp.Regexp{},
		maliciousPatterns: []*regexp.Regexp{},
	}

	cs.initializeSafetyRules()
	return cs
}

// initializeSafetyRules sets up the safety rules
func (cs *ContentSafetyManager) initializeSafetyRules() {
	// Blocked domains (adult content, malicious sites, etc.)
	blockedDomains := []string{
		"pornhub.com", "xvideos.com", "xhamster.com", "redtube.com",
		"youporn.com", "tube8.com", "porn.com", "adult.com",
		"malware.com", "phishing.com", "scam.com", "virus.com",
		"bitcoin-scam.com", "fake-bank.com", "malicious-site.com",
	}

	for _, domain := range blockedDomains {
		cs.blockedDomains[domain] = true
	}

	// Allowed domains (trusted educational, news, tech sites)
	allowedDomains := []string{
		"wikipedia.org", "www.wikipedia.org", "en.wikipedia.org", "github.com", "stackoverflow.com", "developer.mozilla.org",
		"docs.python.org", "golang.org", "nodejs.org", "reactjs.org",
		"news.bbc.co.uk", "reuters.com", "ap.org", "npr.org",
		"mit.edu", "stanford.edu", "harvard.edu", "berkeley.edu",
		"w3.org", "ietf.org", "rfc-editor.org", "tools.ietf.org",
		"httpbin.org", "jsonplaceholder.typicode.com",
	}

	for _, domain := range allowedDomains {
		cs.allowedDomains[domain] = true
	}

	// Adult content keywords (case-insensitive regex) - more specific patterns
	adultKeywords := []string{
		`(?i)\b(porn|xxx|adult.*content|sex.*site|nude.*photo|naked.*body|erotic.*content|fetish.*site|bdsm.*site)\b`,
		`(?i)\b(escort.*service|prostitute.*service|hooker.*service|brothel.*service)\b`,
		`(?i)\b(breast.*photo|boob.*photo|ass.*photo|butt.*photo|penis.*photo|vagina.*photo|dick.*photo|pussy.*photo)\b`,
		`(?i)\b(masturbat.*video|orgasm.*video|climax.*video|cum.*video|sperm.*video)\b`,
		`(?i)\b(incest.*video|pedophil.*content|child.*sex.*video|underage.*sex)\b`,
	}

	for _, pattern := range adultKeywords {
		if regex, err := regexp.Compile(pattern); err == nil {
			cs.adultKeywords = append(cs.adultKeywords, regex)
		}
	}

	// Malicious patterns
	maliciousPatterns := []string{
		`(?i)\b(phishing|scam|fraud|steal|hack|malware|virus|trojan)\b`,
		`(?i)\b(bitcoin.*scam|crypto.*fraud|fake.*bank|identity.*theft)\b`,
		`(?i)\b(download.*virus|click.*here.*win|free.*money|get.*rich)\b`,
		`(?i)\b(urgent.*action|verify.*account|suspended.*account)\b`,
	}

	for _, pattern := range maliciousPatterns {
		if regex, err := regexp.Compile(pattern); err == nil {
			cs.maliciousPatterns = append(cs.maliciousPatterns, regex)
		}
	}
}

// CheckURLSafety validates a URL for safety
func (cs *ContentSafetyManager) CheckURLSafety(urlStr string) (bool, string, error) {
	// Parse URL
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return false, "invalid URL format", err
	}

	// Check if domain is explicitly blocked
	domain := strings.ToLower(parsedURL.Host)
	if cs.blockedDomains[domain] {
		return false, "domain is blocked for safety", nil
	}

	// Check if domain is in allowed list (whitelist approach for safety)
	if len(cs.allowedDomains) > 0 && !cs.allowedDomains[domain] {
		// If we have a whitelist and domain is not in it, check for suspicious patterns
		if cs.containsSuspiciousPatterns(urlStr) {
			return false, "domain not in allowed list and contains suspicious patterns", nil
		}
	}

	// Check URL path for adult content keywords
	if cs.containsAdultContent(urlStr) {
		return false, "URL contains adult content keywords", nil
	}

	// Check for malicious patterns
	if cs.containsMaliciousContent(urlStr) {
		return false, "URL contains malicious patterns", nil
	}

	return true, "", nil
}

// CheckContentSafety validates content for safety
func (cs *ContentSafetyManager) CheckContentSafety(content string) (bool, string, error) {
	// Check for adult content
	if cs.containsAdultContent(content) {
		return false, "content contains adult material", nil
	}

	// Check for malicious content
	if cs.containsMaliciousContent(content) {
		return false, "content contains malicious patterns", nil
	}

	return true, "", nil
}

// containsAdultContent checks if content contains adult keywords
func (cs *ContentSafetyManager) containsAdultContent(text string) bool {
	for _, regex := range cs.adultKeywords {
		if regex.MatchString(text) {
			return true
		}
	}
	return false
}

// containsMaliciousContent checks if content contains malicious patterns
func (cs *ContentSafetyManager) containsMaliciousContent(text string) bool {
	for _, regex := range cs.maliciousPatterns {
		if regex.MatchString(text) {
			return true
		}
	}
	return false
}

// containsSuspiciousPatterns checks for suspicious patterns in URLs
func (cs *ContentSafetyManager) containsSuspiciousPatterns(urlStr string) bool {
	suspiciousPatterns := []string{
		`(?i)\.tk$|\.ml$|\.ga$|\.cf$`,     // Free domains often used for malicious purposes
		`(?i)\d+\.\d+\.\d+\.\d+`,          // IP addresses (suspicious for user-facing URLs)
		`(?i)bit\.ly|tinyurl|short\.link`, // URL shorteners (can hide malicious destinations)
	}

	for _, pattern := range suspiciousPatterns {
		if regex, err := regexp.Compile(pattern); err == nil && regex.MatchString(urlStr) {
			return true
		}
	}
	return false
}

// SafeHTTPClient wraps HTTP client with safety checks
type SafeHTTPClient struct {
	client           *http.Client
	safetyManager    *ContentSafetyManager
	maxContentLength int64
}

// NewSafeHTTPClient creates a new safe HTTP client
func NewSafeHTTPClient() *SafeHTTPClient {
	return &SafeHTTPClient{
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		safetyManager:    NewContentSafetyManager(),
		maxContentLength: 10 * 1024 * 1024, // 10MB limit
	}
}

// SafeGet performs a safe HTTP GET request with content filtering
func (shc *SafeHTTPClient) SafeGet(ctx context.Context, urlStr string) (*http.Response, error) {
	// Check URL safety first
	safe, reason, err := shc.safetyManager.CheckURLSafety(urlStr)
	if err != nil {
		return nil, fmt.Errorf("URL safety check failed: %v", err)
	}
	if !safe {
		return nil, fmt.Errorf("URL blocked: %s", reason)
	}

	// Make the request
	req, err := http.NewRequestWithContext(ctx, "GET", urlStr, nil)
	if err != nil {
		return nil, err
	}

	// Set safe headers
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")

	resp, err := shc.client.Do(req)
	if err != nil {
		return nil, err
	}

	// Check content length
	if resp.ContentLength > shc.maxContentLength {
		resp.Body.Close()
		return nil, fmt.Errorf("content too large: %d bytes", resp.ContentLength)
	}

	return resp, nil
}

// SafeGetWithContentCheck performs a safe GET and checks content
func (shc *SafeHTTPClient) SafeGetWithContentCheck(ctx context.Context, urlStr string) (string, error) {
	resp, err := shc.SafeGet(ctx, urlStr)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	// Read content with size limit
	body := make([]byte, 0, 1024)
	buffer := make([]byte, 1024)
	for {
		n, err := resp.Body.Read(buffer)
		if n > 0 {
			body = append(body, buffer[:n]...)
			if len(body) > int(shc.maxContentLength) {
				return "", fmt.Errorf("content too large")
			}
		}
		if err != nil {
			break
		}
	}

	content := string(body)

	// Check content safety
	safe, reason, err := shc.safetyManager.CheckContentSafety(content)
	if err != nil {
		return "", fmt.Errorf("content safety check failed: %v", err)
	}
	if !safe {
		return "", fmt.Errorf("content blocked: %s", reason)
	}

	return content, nil
}
