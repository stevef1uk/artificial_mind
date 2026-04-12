package main

import (
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
)

// dashboard renders the main monitoring dashboard (now tabbed)
func (m *MonitorService) dashboard(c *gin.Context) {

	path := "templates/dashboard_tabs.html"
	b, err := os.ReadFile(path)
	if err == nil {

		content := string(b)
		if strings.HasPrefix(content, "{{ define") {
			if i := strings.Index(content, "}}\n"); i > -1 {
				content = content[i+3:]
			}
			if j := strings.LastIndex(content, "{{ end }}"); j > -1 {
				content = content[:j]
			}
		}
		c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(content))
		return
	}

	c.HTML(http.StatusOK, "dashboard_tabs.html", gin.H{
		"title":         "Artificial Mind and Workflow",
		"hdnURL":        m.hdnURL,
		"principlesURL": m.principlesURL,
	})
}

// dashboardTabs renders the tabbed monitoring dashboard
func (m *MonitorService) dashboardTabs(c *gin.Context) {

	path := "templates/dashboard_tabs.html"
	b, err := os.ReadFile(path)
	if err == nil {
		content := string(b)
		if strings.HasPrefix(content, "{{ define") {
			if i := strings.Index(content, "}}\n"); i > -1 {
				content = content[i+3:]
			}
			if j := strings.LastIndex(content, "{{ end }}"); j > -1 {
				content = content[:j]
			}
		}
		c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(content))
		return
	}
	c.HTML(http.StatusOK, "dashboard_tabs.html", gin.H{
		"title":         "Artificial Mind and Workflow (Tabbed)",
		"hdnURL":        m.hdnURL,
		"principlesURL": m.principlesURL,
	})
}

// thinkingPanel renders the AI thinking stream page
func (m *MonitorService) thinkingPanel(c *gin.Context) {
	c.HTML(http.StatusOK, "thinking_panel.html", gin.H{
		"title":  "AI Thinking Stream",
		"hdnURL": m.hdnURL,
	})
}

// wowFactor renders the AI Thinking Real-time Visualization (WOW Factor)
func (m *MonitorService) wowFactor(c *gin.Context) {
	c.HTML(http.StatusOK, "wow_factor.html", gin.H{
		"title":  "Artificial Mind - Real-time Visualization",
		"hdnURL": m.hdnURL,
	})
}

// testPage renders a simple test page
func (m *MonitorService) testPage(c *gin.Context) {
	c.HTML(http.StatusOK, "test.html", gin.H{})
}
