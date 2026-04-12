package main

import (
	"bytes"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/gin-gonic/gin"
)

// getAgents: GET /api/agents
func (m *MonitorService) getAgents(c *gin.Context) {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(m.hdnURL + "/api/v1/agents")
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to fetch agents", "details": err.Error()})
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to read agents response", "details": err.Error()})
		return
	}

	c.Data(resp.StatusCode, "application/json", body)
}

// getAgent: GET /api/agents/:id
func (m *MonitorService) getAgent(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing agent id"})
		return
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(m.hdnURL + "/api/v1/agents/" + url.QueryEscape(id))
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to fetch agent", "details": err.Error()})
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to read agent response", "details": err.Error()})
		return
	}

	c.Data(resp.StatusCode, "application/json", body)
}

// getAgentStatus: GET /api/agents/:id/status
func (m *MonitorService) getAgentStatus(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing agent id"})
		return
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(m.hdnURL + "/api/v1/agents/" + url.QueryEscape(id) + "/status")
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to fetch agent status", "details": err.Error()})
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to read agent status response", "details": err.Error()})
		return
	}

	c.Data(resp.StatusCode, "application/json", body)
}

// getAgentExecutions: GET /api/agents/:id/executions
func (m *MonitorService) getAgentExecutions(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing agent id"})
		return
	}

	limit := c.Query("limit")
	url := m.hdnURL + "/api/v1/agents/" + url.QueryEscape(id) + "/executions"
	if limit != "" {
		url += "?limit=" + limit
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to fetch agent executions", "details": err.Error()})
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to read agent executions response", "details": err.Error()})
		return
	}

	c.Data(resp.StatusCode, "application/json", body)
}

// getAgentExecution: GET /api/agents/:id/executions/:execution_id
func (m *MonitorService) getAgentExecution(c *gin.Context) {
	id := c.Param("id")
	executionID := c.Param("execution_id")
	if id == "" || executionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing agent id or execution id"})
		return
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(m.hdnURL + "/api/v1/agents/" + url.QueryEscape(id) + "/executions/" + url.QueryEscape(executionID))
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to fetch agent execution", "details": err.Error()})
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to read agent execution response", "details": err.Error()})
		return
	}

	c.Data(resp.StatusCode, "application/json", body)
}

// createAgent: POST /api/agents
func (m *MonitorService) createAgent(c *gin.Context) {

	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read request body", "details": err.Error()})
		return
	}

	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("POST", m.hdnURL+"/api/v1/agents", bytes.NewReader(body))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create request", "details": err.Error()})
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to forward request to HDN", "details": err.Error()})
		return
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to read HDN response", "details": err.Error()})
		return
	}

	c.Data(resp.StatusCode, "application/json", respBody)
}

// executeAgent: POST /api/agents/:id/execute
func (m *MonitorService) executeAgent(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing agent id"})
		return
	}

	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read request body", "details": err.Error()})
		return
	}

	client := &http.Client{Timeout: 2 * time.Minute}
	req, err := http.NewRequest("POST", m.hdnURL+"/api/v1/agents/"+url.QueryEscape(id)+"/execute", bytes.NewReader(body))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create request", "details": err.Error()})
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to execute agent", "details": err.Error()})
		return
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to read execution response", "details": err.Error()})
		return
	}

	c.Data(resp.StatusCode, "application/json", respBody)
}

// deleteAgent proxies agent deletion to HDN server
func (m *MonitorService) deleteAgent(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "agent ID is required"})
		return
	}

	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("DELETE", m.hdnURL+"/api/v1/agents/"+url.QueryEscape(id), nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create request", "details": err.Error()})
		return
	}

	resp, err := client.Do(req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to delete agent", "details": err.Error()})
		return
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to read delete response", "details": err.Error()})
		return
	}

	c.Data(resp.StatusCode, "application/json", respBody)
}
