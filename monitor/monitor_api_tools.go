package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// getTools lists all tools from Redis registry
func (m *MonitorService) getTools(c *gin.Context) {
	if m.redisClient == nil {
		log.Printf("❌ [MONITOR] Redis client not initialized in getTools")
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "redis not configured"})
		return
	}
	ctx := context.Background()
	ids, err := m.redisClient.SMembers(ctx, "tools:registry").Result()
	if err != nil {
		log.Printf("❌ [MONITOR] Redis error in getTools: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "redis error: " + err.Error()})
		return
	}
	log.Printf("✅ [MONITOR] Found %d tool IDs in Redis registry", len(ids))
	tools := make([]map[string]interface{}, 0, len(ids))
	for _, id := range ids {
		val, err := m.redisClient.Get(ctx, "tool:"+id).Result()
		if err != nil {
			continue
		}
		var obj map[string]interface{}
		if err := json.Unmarshal([]byte(val), &obj); err == nil {
			tools = append(tools, obj)
		}
	}
	c.JSON(http.StatusOK, gin.H{"tools": tools})
}

// getToolUsage returns recent usage events, optionally filtered by agent_id
func (m *MonitorService) getToolUsage(c *gin.Context) {
	if m.redisClient == nil {
		log.Printf("❌ [MONITOR] Redis client not initialized in getToolUsage")
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "redis not configured"})
		return
	}
	agentID := strings.TrimSpace(c.Query("agent_id"))
	ctx := context.Background()
	key := "tools:global:usage_history"
	if agentID != "" {
		key = fmt.Sprintf("tools:%s:usage_history", agentID)
	}
	entries, err := m.redisClient.LRange(ctx, key, 0, 49).Result()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "redis error"})
		return
	}
	items := make([]map[string]interface{}, 0, len(entries))
	for _, e := range entries {
		var obj map[string]interface{}
		if err := json.Unmarshal([]byte(e), &obj); err == nil {
			items = append(items, obj)
		}
	}
	c.JSON(http.StatusOK, gin.H{"usage": items})
}

// getCapabilities returns capabilities grouped by domain by querying the HDN server
func (m *MonitorService) getCapabilities(c *gin.Context) {
	type Capability struct {
		ID          string   `json:"id"`
		Task        string   `json:"task"`
		Description string   `json:"description"`
		Language    string   `json:"language"`
		Tags        []string `json:"tags"`
		Domain      string   `json:"domain"`
		Code        string   `json:"code,omitempty"`
	}

	resp, err := http.Get(m.hdnURL + "/api/v1/domains")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch domains"})
		return
	}
	defer resp.Body.Close()
	// HDN returns a list of domain objects
	var domainsList []struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&domainsList); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse domains"})
		return
	}

	result := make(map[string][]Capability)

	for _, d := range domainsList {
		domain := d.Name
		url := m.hdnURL + "/api/v1/actions/" + domain
		r, err := http.Get(url)
		if err != nil {
			continue
		}
		// Actions endpoint may return null, array, or wrapped
		var raw json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&raw); err == nil && len(raw) > 0 && string(raw) != "null" {
			// Try array of actions
			var arr []struct {
				ID          string   `json:"id"`
				Task        string   `json:"task"`
				Description string   `json:"description"`
				Language    string   `json:"language"`
				Tags        []string `json:"tags"`
				Domain      string   `json:"domain"`
			}
			// Or wrapped {actions: []}
			var wrapped struct {
				Actions []struct {
					ID          string   `json:"id"`
					Task        string   `json:"task"`
					Description string   `json:"description"`
					Language    string   `json:"language"`
					Tags        []string `json:"tags"`
					Domain      string   `json:"domain"`
				} `json:"actions"`
			}
			if err := json.Unmarshal(raw, &arr); err == nil && len(arr) > 0 {
				for _, a := range arr {
					result[domain] = append(result[domain], Capability{ID: a.ID, Task: a.Task, Description: a.Description, Language: a.Language, Tags: a.Tags, Domain: a.Domain})
				}
			} else if err := json.Unmarshal(raw, &wrapped); err == nil && len(wrapped.Actions) > 0 {
				for _, a := range wrapped.Actions {
					result[domain] = append(result[domain], Capability{
						ID: a.ID, Task: a.Task, Description: a.Description, Language: a.Language, Tags: a.Tags, Domain: a.Domain,
					})
				}
			}
		}
		r.Body.Close()
	}

	{
		r, err := http.Get(m.hdnURL + "/api/v1/intelligent/capabilities")
		if err == nil {
			defer r.Body.Close()
			var payload struct {
				Capabilities []struct {
					ID          string            `json:"id"`
					TaskName    string            `json:"task_name"`
					Description string            `json:"description"`
					Language    string            `json:"language"`
					Tags        []string          `json:"tags"`
					Context     map[string]string `json:"context"`
					Code        string            `json:"code"`
				} `json:"capabilities"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err == nil {
				domain := "intelligent"
				for _, ccap := range payload.Capabilities {
					result[domain] = append(result[domain], Capability{
						ID:          ccap.ID,
						Task:        ccap.TaskName,
						Description: ccap.Description,
						Language:    ccap.Language,
						Tags:        ccap.Tags,
						Domain:      domain,
						Code:        ccap.Code,
					})
				}
			}
		}
	}

	c.JSON(http.StatusOK, result)
}

// getCapabilitiesForSession fetches capabilities from HDN (for auto-executor)
func (m *MonitorService) getCapabilitiesForSession() ([]map[string]interface{}, error) {
	resp, err := http.Get(m.hdnURL + "/api/v1/domains")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var domains []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&domains); err != nil {
		return nil, err
	}
	return domains, nil
}

// getToolMetrics proxies tool metrics from HDN server
func (m *MonitorService) getToolMetrics(c *gin.Context) {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(m.hdnURL + "/api/v1/tools/metrics")
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to fetch tool metrics", "details": err.Error()})
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to read tool metrics response", "details": err.Error()})
		return
	}

	c.Data(resp.StatusCode, "application/json", body)
}

// getToolMetricsByID proxies specific tool metrics from HDN server
func (m *MonitorService) getToolMetricsByID(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing tool id"})
		return
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(m.hdnURL + "/api/v1/tools/" + id + "/metrics")
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to fetch tool metrics", "details": err.Error()})
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to read tool metrics response", "details": err.Error()})
		return
	}

	c.Data(resp.StatusCode, "application/json", body)
}

// getRecentToolCalls proxies recent tool calls from HDN server
func (m *MonitorService) getRecentToolCalls(c *gin.Context) {
	limit := c.Query("limit")
	url := m.hdnURL + "/api/v1/tools/calls/recent"
	if limit != "" {
		url += "?limit=" + limit
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to fetch recent tool calls", "details": err.Error()})
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to read recent tool calls response", "details": err.Error()})
		return
	}

	c.Data(resp.StatusCode, "application/json", body)
}

// proxyIntelligentExecute proxies requests to the HDN server
func (m *MonitorService) proxyIntelligentExecute(c *gin.Context) {

	c.Header("Access-Control-Allow-Origin", "*")
	c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
	c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")

	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to read request body"})
		return
	}

	// Log the request to debug language issues
	var reqData map[string]interface{}
	if err := json.Unmarshal(body, &reqData); err == nil {
		if lang, ok := reqData["language"].(string); ok {
			log.Printf("🔍 [MONITOR PROXY] Received intelligent execute request with language: %s", lang)
		} else {
			log.Printf("⚠️ [MONITOR PROXY] Received intelligent execute request WITHOUT language field")
		}
		if desc, ok := reqData["description"].(string); ok {
			log.Printf("🔍 [MONITOR PROXY] Request description: %s", desc)
		}
	}

	req, err := http.NewRequest("POST", m.hdnURL+"/api/v1/intelligent/execute", bytes.NewBuffer(body))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create request to HDN server"})
		return
	}

	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to connect to HDN server"})
		return
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read HDN server response"})
		return
	}

	c.Data(resp.StatusCode, "application/json", respBody)
}
