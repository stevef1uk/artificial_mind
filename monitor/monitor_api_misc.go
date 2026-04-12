package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// websocketHandler provides real-time updates via WebSocket
func (m *MonitorService) websocketHandler(c *gin.Context) {

	c.JSON(http.StatusOK, gin.H{"message": "WebSocket endpoint - implementation pending"})
}

// serialize converts interface{} to string (same as in suggestGoalNextSteps)
func (m *MonitorService) serialize(v interface{}) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
}

// getInterpreterPlan gets a concrete plan from HDN interpreter
func (m *MonitorService) getInterpreterPlan(input string, ctx map[string]string, sessionID string) (string, error) {
	payload := map[string]interface{}{
		"input":      input,
		"context":    ctx,
		"session_id": sessionID,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	url := m.hdnURL + "/api/v1/interpret"
	req, err := http.NewRequest("POST", url, bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("interpreter returned status %d: %s", resp.StatusCode, string(body))
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	if plan, ok := result["plan"].(string); ok {
		return plan, nil
	}
	if plan, ok := result["result"].(string); ok {
		return plan, nil
	}
	if plan, ok := result["response"].(string); ok {
		return plan, nil
	}

	return m.serialize(result), nil
}

// handleCORS handles CORS preflight requests
func (m *MonitorService) handleCORS(c *gin.Context) {
	c.Header("Access-Control-Allow-Origin", "*")
	c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
	c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")
	c.Status(http.StatusOK)
}

// Expose execution method from environment for UI decisions
func (m *MonitorService) exposeExecutionMethod(c *gin.Context) {
	method := os.Getenv("EXECUTION_METHOD")
	if method == "" {
		method = "docker"
	}
	c.JSON(http.StatusOK, gin.H{"execution_method": strings.ToLower(method)})
}
