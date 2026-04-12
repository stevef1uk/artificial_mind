package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// getSystemStatus returns the overall system status
func (m *MonitorService) getSystemStatus(c *gin.Context) {
	status := &SystemStatus{
		Overall:   "healthy",
		Timestamp: time.Now(),
		Services:  make(map[string]ServiceInfo),
		Metrics:   SystemMetrics{},
		Alerts:    []Alert{},
	}

	// Check all services in parallel using goroutines for faster response
	type serviceResult struct {
		key  string
		info ServiceInfo
	}
	results := make(chan serviceResult, 8)

	go func() {
		results <- serviceResult{key: "hdn", info: m.checkService("HDN Server", m.hdnURL+"/health")}
	}()
	go func() {
		results <- serviceResult{key: "principles", info: m.checkServicePOST("Principles Server", m.principlesURL+"/action")}
	}()
	go func() {
		results <- serviceResult{key: "fsm", info: m.checkService("FSM Server", m.fsmURL+"/health")}
	}()
	go func() {
		results <- serviceResult{key: "goal_manager", info: m.checkService("Goal Manager", m.goalMgrURL+"/goals/agent_1/active")}
	}()
	go func() {
		results <- serviceResult{key: "redis", info: m.checkRedis()}
	}()
	go func() {
		results <- serviceResult{key: "neo4j", info: m.checkNeo4j()}
	}()
	go func() {
		results <- serviceResult{key: "vector-db", info: m.checkQdrant()}
	}()
	go func() {
		results <- serviceResult{key: "nats", info: m.checkNATS()}
	}()
	go func() {

		results <- serviceResult{key: "scraper", info: m.checkService("Scraper Service", m.hdnURL+"/health")}
	}()

	timeout := time.After(4 * time.Second)
	collected := 0
	expectedServices := []string{"hdn", "principles", "fsm", "goal_manager", "redis", "neo4j", "vector-db", "nats", "scraper"}
	timeoutReached := false

	for collected < len(expectedServices) && !timeoutReached {
		select {
		case result := <-results:
			status.Services[result.key] = result.info
			collected++
		case <-timeout:

			log.Printf("⏱️ [MONITOR] getSystemStatus timeout: only %d/%d services responded", collected, len(expectedServices))
			timeoutReached = true
		}
	}

	if timeoutReached || collected < len(expectedServices) {
		for _, key := range expectedServices {
			if _, exists := status.Services[key]; !exists {
				status.Services[key] = ServiceInfo{
					Name:         key,
					Status:       "unhealthy",
					LastCheck:    time.Now(),
					ResponseTime: 4000,
					Error:        "Service check timed out",
				}
			}
		}
	}

	status.Metrics = m.getSystemMetrics()

	unhealthyServices := 0
	serviceAlerts := map[string]struct {
		level   string
		message string
	}{
		"hdn":          {"error", "HDN Server is not responding"},
		"principles":   {"error", "Principles Server is not responding"},
		"fsm":          {"error", "FSM Server is not responding"},
		"goal_manager": {"warning", "Goal Manager is not responding"},
		"redis":        {"error", "Redis is not responding"},
		"neo4j":        {"error", "Neo4j is not responding"},
		"vector-db":    {"error", "Vector database is not responding"},
		"nats":         {"warning", "NATS is not responding"},
		"scraper":      {"warning", "Scraper service is not responding"},
	}

	for key, alertConfig := range serviceAlerts {
		if svc, exists := status.Services[key]; exists && svc.Status != "healthy" {
			unhealthyServices++
			status.Alerts = append(status.Alerts, Alert{
				ID:        key + "_down",
				Level:     alertConfig.level,
				Message:   alertConfig.message,
				Timestamp: time.Now(),
				Service:   key,
			})
		}
	}

	if unhealthyServices == 0 {
		status.Overall = "healthy"
	} else if unhealthyServices <= 2 {
		status.Overall = "degraded"
	} else {
		status.Overall = "critical"
	}

	c.JSON(http.StatusOK, status)
}

// checkService checks if a service is healthy
func (m *MonitorService) checkService(name, url string) ServiceInfo {
	start := time.Now()

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(url)

	responseTime := time.Since(start).Milliseconds()

	info := ServiceInfo{
		Name:         name,
		URL:          url,
		LastCheck:    time.Now(),
		ResponseTime: responseTime,
	}

	if err != nil {
		info.Status = "unhealthy"
		info.Error = err.Error()
		return info
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		info.Status = "healthy"
	} else {
		info.Status = "unhealthy"
		info.Error = fmt.Sprintf("HTTP %d", resp.StatusCode)
	}

	return info
}

// checkServicePOST checks if a service is healthy using POST request
func (m *MonitorService) checkServicePOST(name, url string) ServiceInfo {
	start := time.Now()

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Post(url, "application/json", strings.NewReader("{}"))

	responseTime := time.Since(start).Milliseconds()

	info := ServiceInfo{
		Name:         name,
		URL:          url,
		LastCheck:    time.Now(),
		ResponseTime: responseTime,
	}

	if err != nil {
		info.Status = "unhealthy"
		info.Error = err.Error()
		return info
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		info.Status = "healthy"
	} else {
		info.Status = "unhealthy"
		info.Error = fmt.Sprintf("HTTP %d", resp.StatusCode)
	}

	return info
}

// checkRedis checks Redis connection
func (m *MonitorService) checkRedis() ServiceInfo {
	start := time.Now()

	info := ServiceInfo{
		Name:         "Redis",
		URL:          "localhost:6379",
		LastCheck:    time.Now(),
		ResponseTime: 0,
	}

	if m.redisClient == nil {
		info.Status = "unhealthy"
		info.Error = "Redis client not initialized"
		return info
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	pong, err := m.redisClient.Ping(ctx).Result()

	responseTime := time.Since(start).Milliseconds()
	info.ResponseTime = responseTime

	if err != nil {
		info.Status = "unhealthy"
		info.Error = err.Error()
		return info
	}

	if pong == "PONG" {
		info.Status = "healthy"
	} else {
		info.Status = "unhealthy"
		info.Error = "Unexpected response from Redis"
	}

	return info
}

// checkNeo4j checks Neo4j connection and health
func (m *MonitorService) checkNeo4j() ServiceInfo {
	start := time.Now()

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(m.neo4jURL)

	responseTime := time.Since(start).Milliseconds()

	info := ServiceInfo{
		Name:         "Neo4j",
		URL:          m.neo4jURL,
		LastCheck:    time.Now(),
		ResponseTime: responseTime,
	}

	if err != nil {
		info.Status = "unhealthy"
		info.Error = err.Error()
		return info
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		info.Status = "healthy"
	} else {
		info.Status = "unhealthy"
		info.Error = fmt.Sprintf("HTTP %d", resp.StatusCode)
	}

	return info
}

// checkQdrant checks vector database connection and health (Qdrant or Weaviate)
func (m *MonitorService) checkQdrant() ServiceInfo {
	start := time.Now()

	client := &http.Client{Timeout: 3 * time.Second}

	var resp *http.Response
	var err error
	var serviceName string
	var healthEndpoint string

	if strings.Contains(m.weaviateURL, "qdrant") || strings.Contains(m.weaviateURL, ":6333") {
		serviceName = "Qdrant"
		healthEndpoint = m.weaviateURL + "/healthz"
	} else {
		serviceName = "Weaviate"
		healthEndpoint = m.weaviateURL + "/v1/meta"
	}

	resp, err = client.Get(healthEndpoint)
	responseTime := time.Since(start).Milliseconds()

	info := ServiceInfo{
		Name:         serviceName,
		URL:          m.weaviateURL,
		LastCheck:    time.Now(),
		ResponseTime: responseTime,
	}

	if err != nil {
		info.Status = "unhealthy"
		info.Error = err.Error()
		return info
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		info.Status = "healthy"
	} else {
		info.Status = "unhealthy"
		info.Error = fmt.Sprintf("HTTP %d", resp.StatusCode)
	}

	return info
}

// checkNATS checks NATS connection and health
func (m *MonitorService) checkNATS() ServiceInfo {
	start := time.Now()

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(m.natsURL + "/varz")

	responseTime := time.Since(start).Milliseconds()

	info := ServiceInfo{
		Name:         "NATS",
		URL:          m.natsURL,
		LastCheck:    time.Now(),
		ResponseTime: responseTime,
	}

	if err != nil {
		info.Status = "unhealthy"
		info.Error = err.Error()
		return info
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		info.Status = "healthy"
	} else {
		info.Status = "unhealthy"
		info.Error = fmt.Sprintf("HTTP %d", resp.StatusCode)
	}

	return info
}

// getSystemMetrics retrieves system-wide metrics
func (m *MonitorService) getSystemMetrics() SystemMetrics {
	metrics := SystemMetrics{}

	if m.redisClient == nil {
		return metrics
	}

	ctx := context.Background()
	workflowKeys, err := m.redisClient.Keys(ctx, "workflow:*").Result()
	if err == nil {
		metrics.ActiveWorkflows = len(workflowKeys)
	}

	execMetrics := m.getExecutionMetricsFromRedis()
	metrics.TotalExecutions = execMetrics.TotalExecutions
	metrics.SuccessRate = execMetrics.SuccessRate
	metrics.AverageExecutionTime = execMetrics.AverageTime

	_, err = m.redisClient.Info(ctx, "clients").Result()
	if err == nil {

		metrics.RedisConnections = 1
	}

	metrics.DockerContainers = 0

	return metrics
}

// getExecutionMetrics returns execution statistics
func (m *MonitorService) getExecutionMetrics(c *gin.Context) {
	metrics := m.getExecutionMetricsFromRedis()
	c.JSON(http.StatusOK, metrics)
}

func (m *MonitorService) getExecutionMetricsFromRedis() ExecutionMetrics {
	metrics := ExecutionMetrics{
		ByLanguage: make(map[string]int),
		ByTaskType: make(map[string]int),
	}

	if m.redisClient == nil {
		return metrics
	}

	ctx := context.Background()

	totalExec, _ := m.redisClient.Get(ctx, "metrics:total_executions").Int()
	metrics.TotalExecutions = totalExec

	successExec, _ := m.redisClient.Get(ctx, "metrics:successful_executions").Int()
	metrics.SuccessfulExecutions = successExec

	metrics.FailedExecutions = totalExec - successExec

	if totalExec > 0 {
		metrics.SuccessRate = float64(successExec) / float64(totalExec) * 100
	}

	avgTime, _ := m.redisClient.Get(ctx, "metrics:avg_execution_time").Float64()
	metrics.AverageTime = avgTime

	lastExecStr, _ := m.redisClient.Get(ctx, "metrics:last_execution").Result()
	if lastExecStr != "" {
		if lastExec, err := time.Parse(time.RFC3339, lastExecStr); err == nil {
			metrics.LastExecution = lastExec
		}
	}

	return metrics
}

// getRedisInfo returns Redis connection information
func (m *MonitorService) getRedisInfo(c *gin.Context) {
	info := RedisInfo{
		Connected: false,
		Keyspace:  make(map[string]int),
	}

	if m.redisClient == nil {
		c.JSON(http.StatusOK, info)
		return
	}

	ctx := context.Background()

	_, err := m.redisClient.Ping(ctx).Result()
	if err != nil {
		c.JSON(http.StatusOK, info)
		return
	}

	info.Connected = true

	_, err = m.redisClient.Info(ctx).Result()
	if err == nil {

		info.Version = "6.0+"
		info.UsedMemory = "Unknown"
		info.ConnectedClients = 1
	}

	keys, err := m.redisClient.Keys(ctx, "*").Result()
	if err == nil {
		info.Keyspace["total"] = len(keys)
	}

	c.JSON(http.StatusOK, info)
}

// getDockerInfo returns Docker container information
func (m *MonitorService) getDockerInfo(c *gin.Context) {

	info := DockerInfo{
		ContainersRunning: 0,
		ContainersTotal:   0,
		ImagesCount:       0,
	}

	c.JSON(http.StatusOK, info)
}

// getNeo4jInfo returns Neo4j connection information
func (m *MonitorService) getNeo4jInfo(c *gin.Context) {
	info := map[string]interface{}{
		"connected": false,
		"url":       m.neo4jURL,
		"version":   "Unknown",
		"status":    "unhealthy",
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(m.neo4jURL)
	if err != nil {
		info["error"] = err.Error()
		c.JSON(http.StatusOK, info)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		info["connected"] = true
		info["status"] = "healthy"
		info["version"] = "5.x"
	} else {
		info["error"] = fmt.Sprintf("HTTP %d", resp.StatusCode)
	}

	c.JSON(http.StatusOK, info)
}

// getNeo4jStats queries Neo4j for live graph counts
func (m *MonitorService) getNeo4jStats(c *gin.Context) {

	user := strings.TrimSpace(os.Getenv("NEO4J_USER"))
	if user == "" {
		user = "neo4j"
	}
	pass := strings.TrimSpace(os.Getenv("NEO4J_PASS"))
	if pass == "" {
		pass = "test1234"
	}

	// Use cypher-shell-like Bolt endpoint via HTTP transactional API
	// POST { statements: [{ statement: "MATCH (n) RETURN count(n) as nodes" }, ...] }
	type stmt struct {
		Statement string `json:"statement"`
	}
	payload := map[string]interface{}{
		"statements": []stmt{
			{Statement: "MATCH (n) RETURN count(n) AS nodes"},
			{Statement: "MATCH ()-[r]->() RETURN count(r) AS relationships"},
			{Statement: "CALL db.stats.retrieve('GRAPH COUNTS') YIELD data RETURN data"},
			{Statement: "MATCH (n) RETURN labels(n)[0] AS label, count(n) AS count ORDER BY count DESC"},
			{Statement: "MATCH ()-[r]->() RETURN type(r) AS rel, count(r) AS count ORDER BY count DESC"},
			{Statement: "MATCH (n:Concept) RETURN coalesce(n.domain,'(unset)') AS domain, count(n) AS count ORDER BY count DESC"},
			{Statement: "MATCH (n:Concept) RETURN n.name AS name, coalesce(n.domain,'(unset)') AS domain, left(n.definition,120) AS definition ORDER BY n.name ASC LIMIT 10"},
		},
	}
	body, _ := json.Marshal(payload)

	endpoint := strings.TrimRight(m.neo4jURL, "/") + "/db/neo4j/tx/commit"
	req, _ := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(user, pass)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "neo4j request failed"})
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		c.JSON(resp.StatusCode, gin.H{"error": string(b)})
		return
	}

	var result struct {
		Results []struct {
			Columns []string `json:"columns"`
			Data    []struct {
				Row []interface{} `json:"row"`
			} `json:"data"`
		} `json:"results"`
		Errors []interface{} `json:"errors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "invalid neo4j response"})
		return
	}
	if len(result.Errors) > 0 {
		c.JSON(http.StatusBadGateway, gin.H{"error": result.Errors})
		return
	}

	// Parse nodes and relationships from first two statements
	var nodes, relationships int64
	if len(result.Results) >= 1 && len(result.Results[0].Data) > 0 && len(result.Results[0].Data[0].Row) > 0 {
		switch v := result.Results[0].Data[0].Row[0].(type) {
		case float64:
			nodes = int64(v)
		}
	}
	if len(result.Results) >= 2 && len(result.Results[1].Data) > 0 && len(result.Results[1].Data[0].Row) > 0 {
		switch v := result.Results[1].Data[0].Row[0].(type) {
		case float64:
			relationships = int64(v)
		}
	}

	// Optional: parse GRAPH COUNTS
	var graphCounts interface{}
	if len(result.Results) >= 3 && len(result.Results[2].Data) > 0 && len(result.Results[2].Data[0].Row) > 0 {
		graphCounts = result.Results[2].Data[0].Row[0]
	}

	labels := make([]map[string]interface{}, 0)
	if len(result.Results) >= 4 {
		for _, d := range result.Results[3].Data {
			if len(d.Row) >= 2 {
				labels = append(labels, map[string]interface{}{"label": d.Row[0], "count": d.Row[1]})
			}
		}
	}

	relTypes := make([]map[string]interface{}, 0)
	if len(result.Results) >= 5 {
		for _, d := range result.Results[4].Data {
			if len(d.Row) >= 2 {
				relTypes = append(relTypes, map[string]interface{}{"type": d.Row[0], "count": d.Row[1]})
			}
		}
	}

	domains := make([]map[string]interface{}, 0)
	if len(result.Results) >= 6 {
		for _, d := range result.Results[5].Data {
			if len(d.Row) >= 2 {
				domains = append(domains, map[string]interface{}{"domain": d.Row[0], "count": d.Row[1]})
			}
		}
	}

	concepts := make([]map[string]interface{}, 0)
	if len(result.Results) >= 7 {
		for _, d := range result.Results[6].Data {
			if len(d.Row) >= 3 {
				concepts = append(concepts, map[string]interface{}{"name": d.Row[0], "domain": d.Row[1], "definition": d.Row[2]})
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"nodes":         nodes,
		"relationships": relationships,
		"graph_counts":  graphCounts,
		"labels":        labels,
		"rel_types":     relTypes,
		"domains":       domains,
		"concepts":      concepts,
		"checked_at":    time.Now().UTC().Format(time.RFC3339),
	})
}

// getQdrantInfo returns Qdrant connection information
func (m *MonitorService) getQdrantInfo(c *gin.Context) {
	info := map[string]interface{}{
		"connected": false,
		"url":       m.weaviateURL,
		"version":   "Unknown",
		"status":    "unhealthy",
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(m.weaviateURL + "/v1/meta")
	if err != nil {
		info["error"] = err.Error()
		c.JSON(http.StatusOK, info)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		info["connected"] = true
		info["status"] = "healthy"
		info["version"] = "latest"
	} else {
		info["error"] = fmt.Sprintf("HTTP %d", resp.StatusCode)
	}

	c.JSON(http.StatusOK, info)
}

// getQdrantStats returns collection stats for vector database (Qdrant or Weaviate)
func (m *MonitorService) getQdrantStats(c *gin.Context) {
	client := &http.Client{Timeout: 10 * time.Second}

	// Determine if this is Qdrant or Weaviate and use appropriate endpoint
	var resp *http.Response
	var err error

	if strings.Contains(m.weaviateURL, "qdrant") || strings.Contains(m.weaviateURL, ":6333") {

		resp, err = client.Get(strings.TrimRight(m.weaviateURL, "/") + "/collections")
	} else {

		resp, err = client.Get(strings.TrimRight(m.weaviateURL, "/") + "/v1/schema")
	}

	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "vector database request failed"})
		return
	}
	defer resp.Body.Close()

	if strings.Contains(m.weaviateURL, "qdrant") || strings.Contains(m.weaviateURL, ":6333") {
		// Handle Qdrant response
		var list struct {
			Result struct {
				Collections []struct {
					Name string `json:"name"`
				} `json:"collections"`
			} `json:"result"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&list)

		summaries := make([]map[string]interface{}, 0)
		for _, col := range list.Result.Collections {
			u := strings.TrimRight(m.weaviateURL, "/") + "/collections/" + col.Name

			infoResp, err := client.Get(u)
			if err != nil {
				continue
			}
			defer infoResp.Body.Close()

			var info struct {
				Result struct {
					PointsCount int `json:"points_count"`
				} `json:"result"`
			}
			_ = json.NewDecoder(infoResp.Body).Decode(&info)

			summaries = append(summaries, map[string]interface{}{
				"name":         col.Name,
				"points_count": info.Result.PointsCount,
				"type":         "qdrant_collection",
			})
		}
		c.JSON(http.StatusOK, gin.H{"collections": summaries})
		return
	} else {
		// Handle Weaviate response (default)
		var weaviateResp struct {
			Classes []struct {
				Class string `json:"class"`
			} `json:"classes"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&weaviateResp); err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": "failed to decode weaviate response"})
			return
		}

		collections := make([]map[string]interface{}, 0)
		for _, cls := range weaviateResp.Classes {
			if cls.Class != "" {

				count := m.getWeaviateObjectCount(cls.Class)
				collections = append(collections, map[string]interface{}{
					"name":         cls.Class,
					"points_count": count,
				})
			}
		}
		c.JSON(http.StatusOK, gin.H{"collections": collections})
		return
	}
}

// getWeaviateObjectCount gets the actual count of objects in a Weaviate class
func (m *MonitorService) getWeaviateObjectCount(className string) int {
	client := &http.Client{Timeout: 10 * time.Second}

	queryData := map[string]string{
		"query": fmt.Sprintf("{ Aggregate { %s { meta { count } } } }", className),
	}
	queryBytes, _ := json.Marshal(queryData)
	query := string(queryBytes)

	resp, err := client.Post(
		strings.TrimRight(m.weaviateURL, "/")+"/v1/graphql",
		"application/json",
		strings.NewReader(query),
	)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0
	}

	if data, ok := result["data"].(map[string]interface{}); ok {
		if aggregate, ok := data["Aggregate"].(map[string]interface{}); ok {
			if classData, ok := aggregate[className].([]interface{}); ok && len(classData) > 0 {
				if firstItem, ok := classData[0].(map[string]interface{}); ok {
					if meta, ok := firstItem["meta"].(map[string]interface{}); ok {
						if count, ok := meta["count"].(float64); ok {
							return int(count)
						}
					}
				}
			}
		}
	}

	return 0
}

// getWeaviateRecords returns the total count of records in Weaviate
func (m *MonitorService) getWeaviateRecords(c *gin.Context) {
	client := &http.Client{Timeout: 10 * time.Second}

	resp, err := client.Get(strings.TrimRight(m.weaviateURL, "/") + "/v1/schema")
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to connect to Weaviate", "count": 0})
		return
	}
	defer resp.Body.Close()

	var weaviateResp struct {
		Classes []struct {
			Class string `json:"class"`
		} `json:"classes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&weaviateResp); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to decode weaviate response", "count": 0})
		return
	}

	totalCount := 0
	classCounts := make(map[string]int)

	for _, cls := range weaviateResp.Classes {
		if cls.Class != "" {
			count := m.getWeaviateObjectCount(cls.Class)
			classCounts[cls.Class] = count
			totalCount += count
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"total_count":  totalCount,
		"class_counts": classCounts,
		"classes":      len(weaviateResp.Classes),
	})
}

// getNATSInfo returns NATS connection information
func (m *MonitorService) getNATSInfo(c *gin.Context) {
	info := map[string]interface{}{
		"connected": false,
		"url":       m.natsURL,
		"version":   "Unknown",
		"status":    "unhealthy",
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(m.natsURL + "/varz")
	if err != nil {
		info["error"] = err.Error()
		c.JSON(http.StatusOK, info)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		info["connected"] = true
		info["status"] = "healthy"
		info["version"] = "2.10"
	} else {
		info["error"] = fmt.Sprintf("HTTP %d", resp.StatusCode)
	}

	c.JSON(http.StatusOK, info)
}

// getK8sServices returns a list of available Kubernetes services for log viewing
func (m *MonitorService) getK8sServices(c *gin.Context) {
	services := []map[string]string{
		{"name": "hdn-server-rpi58", "displayName": "HDN Server", "namespace": "agi"},
		{"name": "fsm-server-rpi58", "displayName": "FSM Server", "namespace": "agi"},
		{"name": "goal-manager", "displayName": "Goal Manager", "namespace": "agi"},
		{"name": "principles-server", "displayName": "Principles Server", "namespace": "agi"},
		{"name": "monitor-ui", "displayName": "Monitor UI", "namespace": "agi"},
		{"name": "neo4j", "displayName": "Neo4j Database", "namespace": "agi"},
		{"name": "redis", "displayName": "Redis Cache", "namespace": "agi"},
		{"name": "weaviate", "displayName": "Weaviate Vector DB", "namespace": "agi"},
		{"name": "nats", "displayName": "NATS Message Bus", "namespace": "agi"},
	}

	c.JSON(http.StatusOK, services)
}
