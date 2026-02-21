package main

import (
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// proxyScraper forwards requests to the Playwright Scraper service
func (m *MonitorService) proxyScraper(c *gin.Context) {
	// Extract path
	fullPath := c.Request.URL.Path
	targetPath := fullPath

	// If it's a generic scraper call, we strip the /api/scraper prefix
	// because the scraper binary has its own /api/scraper/... routes
	// or top-level /health routes.
	if strings.HasPrefix(fullPath, "/api/scraper/") {
		targetPath = strings.TrimPrefix(fullPath, "/api/scraper")
	}
	// Note: /api/codegen/... is preserved as is because the scraper expects it.

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

	// Copy response headers
	for k, vals := range resp.Header {
		for _, v := range vals {
			c.Writer.Header().Add(k, v)
		}
	}

	c.Status(resp.StatusCode)
	io.Copy(c.Writer, resp.Body)
}
