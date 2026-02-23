package main

import (
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// proxyScraper forwards requests to the Playwright Scraper service
func (m *MonitorService) proxyScraper(c *gin.Context) {
	fullPath := c.Request.URL.Path
	// Forward the path as-is. Both the core scraper and the generic handlers
	// now support the full paths (including prefixes if applicable).
	// Example: /api/scraper/scrape/start OR /api/codegen/latest
	targetPath := fullPath

	// Build target URL
	target := strings.TrimRight(m.scraperURL, "/") + targetPath

	if c.Request.URL.RawQuery != "" {
		target += "?" + c.Request.URL.RawQuery
	}

	method := c.Request.Method
	var req *http.Request
	var err error

	if method == http.MethodGet || method == http.MethodDelete || method == http.MethodHead {
		req, err = http.NewRequest(method, target, nil)
	} else {
		body, _ := io.ReadAll(c.Request.Body)
		req, err = http.NewRequest(method, target, strings.NewReader(string(body)))
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create proxy request"})
		return
	}

	// Copy headers
	for k, vals := range c.Request.Header {
		for _, v := range vals {
			req.Header.Add(k, v)
		}
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "scraper proxy failed", "details": err.Error()})
		return
	}
	defer resp.Body.Close()

	// Copy response headers, excluding CORS headers that we manage in the monitor middleware
	for k, vals := range resp.Header {
		lowerK := strings.ToLower(k)
		if strings.HasPrefix(lowerK, "access-control-") {
			continue
		}
		for _, v := range vals {
			c.Writer.Header().Add(k, v)
		}
	}

	c.Status(resp.StatusCode)
	_, _ = io.Copy(c.Writer, resp.Body)
}
