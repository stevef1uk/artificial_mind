package main

import (
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// proxyFSM forwards requests to the FSM server so we can embed it same-origin
func (m *MonitorService) proxyFSM(c *gin.Context) {
	// Extract path after /api/fsm/
	p := strings.TrimPrefix(c.Param("path"), "/")
	// Build target URL
	target := m.fsmURL
	if p != "" {
		if strings.HasPrefix(p, "/") {
			target += p
		} else {
			target += "/" + p
		}
	}

	// Proxy method
	method := c.Request.Method
	var req *http.Request
	if method == http.MethodGet || method == http.MethodDelete {
		req, _ = http.NewRequest(method, target, nil)
	} else {
		// Copy body
		body, _ := io.ReadAll(c.Request.Body)
		req, _ = http.NewRequest(method, target, strings.NewReader(string(body)))
		for k, vals := range c.Request.Header {
			for _, v := range vals {
				req.Header.Add(k, v)
			}
		}
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "fsm proxy failed"})
		return
	}
	defer resp.Body.Close()

	for k, vals := range resp.Header {
		for _, v := range vals {
			c.Writer.Header().Add(k, v)
		}
	}
	c.Status(resp.StatusCode)
	io.Copy(c.Writer, resp.Body)
}
