package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// getProjects proxies the list of projects from the HDN API
func (m *MonitorService) getProjects(c *gin.Context) {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(m.hdnURL + "/api/v1/projects")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch projects"})
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read projects response"})
		return
	}

	c.Data(resp.StatusCode, "application/json", body)
}

// deleteProject proxies deletion to HDN API
func (m *MonitorService) deleteProject(c *gin.Context) {
	id := c.Param("id")
	if strings.TrimSpace(id) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id required"})
		return
	}
	req, err := http.NewRequest(http.MethodDelete, m.hdnURL+"/api/v1/projects/"+urlQueryEscape(id), nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	c.Data(resp.StatusCode, "application/json", body)
}

// getProject proxies a single project by id
func (m *MonitorService) getProject(c *gin.Context) {
	id := c.Param("id")
	resp, err := http.Get(fmt.Sprintf("%s/api/v1/projects/%s", m.hdnURL, id))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch project"})
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	c.Data(resp.StatusCode, "application/json", body)
}

// getProjectCheckpoints proxies checkpoints for a project
func (m *MonitorService) getProjectCheckpoints(c *gin.Context) {
	id := c.Param("id")
	resp, err := http.Get(fmt.Sprintf("%s/api/v1/projects/%s/checkpoints", m.hdnURL, id))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch checkpoints"})
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	c.Data(resp.StatusCode, "application/json", body)
}

// getProjectWorkflows proxies workflow ids for a project
func (m *MonitorService) getProjectWorkflows(c *gin.Context) {
	id := c.Param("id")
	resp, err := http.Get(fmt.Sprintf("%s/api/v1/projects/%s/workflows", m.hdnURL, id))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch project workflows"})
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	c.Data(resp.StatusCode, "application/json", body)
}

// extractProjectIDFromText finds a project name in free text and resolves it to an ID.
// Supported phrases: "under project <name>", "in project <name>", "use project <name>"
// Accepts names in double quotes, single quotes, or unquoted until EOL.
// If a matching project isn't found, it will auto-create one with that name.
func (m *MonitorService) extractProjectIDFromText(input string) (string, bool) {
	text := strings.ToLower(strings.TrimSpace(input))
	if text == "" {
		return "", false
	}
	log.Printf("🔍 [DEBUG] extractProjectIDFromText: input='%s', text='%s'", input, text)
	patterns := []string{

		`(?i)under\s+project\s+"([^"]+)"`,
		`(?i)in\s+project\s+"([^"]+)"`,
		`(?i)use\s+project\s+"([^"]+)"`,
		`(?i)against\s+project\s+"([^"]+)"`,
		`(?i)to\s+(the\s+)?project\s+"([^"]+)"`,
		`(?i)for\s+project\s+"([^"]+)"`,
		`(?i)with\s+project\s+"([^"]+)"`,
		`(?i)under\s+project\s+'([^']+)'`,
		`(?i)in\s+project\s+'([^']+)'`,
		`(?i)use\s+project\s+'([^']+)'`,
		`(?i)against\s+project\s+'([^']+)'`,
		`(?i)to\s+(the\s+)?project\s+'([^']+)'`,
		`(?i)for\s+project\s+'([^']+)'`,
		`(?i)with\s+project\s+'([^']+)'`,

		`(?i)under\s+project\s+([a-zA-Z0-9_]+)(?:\s|$|,|\.)`,
		`(?i)in\s+project\s+([a-zA-Z0-9_]+)(?:\s|$|,|\.)`,
		`(?i)use\s+project\s+([a-zA-Z0-9_]+)(?:\s|$|,|\.)`,
		`(?i)against\s+project\s+([a-zA-Z0-9_]+)(?:\s|$|,|\.)`,
		`(?i)to\s+(the\s+)?project\s+([a-zA-Z0-9_]+)(?:\s|$|,|\.)`,
		`(?i)for\s+project\s+([a-zA-Z0-9_]+)(?:\s|$|,|\.)`,
		`(?i)with\s+project\s+([a-zA-Z0-9_]+)(?:\s|$|,|\.)`,

		`(?i)project\s+([a-zA-Z0-9_]+)(?:\s|$|,|\.)`,
		`(?i)project\s+"([^"]+)"`,
		`(?i)project\s+'([^']+)'`,

		`(?i)project\s+([a-zA-Z0-9_]+)\s*$`,
	}
	var name string
	var originalName string

	for i, p := range patterns {
		re := regexp.MustCompile(p)

		if m2 := re.FindStringSubmatch(input); len(m2) > 1 {

			captured := m2[len(m2)-1]
			originalName = strings.TrimSpace(captured)

			originalName = strings.Trim(originalName, " \"'\t\n.!?,")

			name = strings.ToLower(originalName)
			log.Printf("🔍 [DEBUG] extractProjectIDFromText: pattern %d matched, captured='%s', originalName='%s', name='%s'", i, captured, originalName, name)
			break
		}
	}

	if name == "" {
		for i, p := range patterns {
			re := regexp.MustCompile(p)
			if m2 := re.FindStringSubmatch(text); len(m2) > 1 {
				captured := m2[len(m2)-1]
				name = strings.TrimSpace(captured)
				name = strings.Trim(name, " \"'\t\n.!?,")
				originalName = name
				log.Printf("🔍 [DEBUG] extractProjectIDFromText: pattern %d matched (lowercase fallback), captured='%s', name='%s'", i, captured, name)
				break
			}
		}
	}
	if name == "" {
		log.Printf("🔍 [DEBUG] extractProjectIDFromText: no project name found")
		return "", false
	}

	if id := m.findProjectIDByName(name); id != "" {
		log.Printf("🔍 [DEBUG] extractProjectIDFromText: found existing project %s with ID %s", name, id)
		return id, true
	}

	if id := m.createProjectIfMissing(originalName, ""); id != "" {
		log.Printf("🔍 [DEBUG] extractProjectIDFromText: created new project %s with ID %s", originalName, id)
		return id, true
	}
	log.Printf("🔍 [DEBUG] extractProjectIDFromText: failed to create project %s", originalName)
	return "", false
}

// findProjectIDByName fetches projects and returns the ID of the first case-insensitive match on name
func (m *MonitorService) findProjectIDByName(name string) string {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(m.hdnURL + "/api/v1/projects")
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ""
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return ""
	}
	var projects []map[string]interface{}
	if err := json.Unmarshal(body, &projects); err != nil {
		return ""
	}
	lname := strings.ToLower(strings.TrimSpace(name))
	for _, p := range projects {
		n, _ := p["name"].(string)
		id, _ := p["id"].(string)
		if strings.ToLower(strings.TrimSpace(n)) == lname {
			return id
		}
	}
	return ""
}

// createProjectIfMissing creates a project by name and returns its ID (or empty string on failure)
func (m *MonitorService) createProjectIfMissing(name, description string) string {
	if name == "" {
		log.Printf("⚠️ [DEBUG] createProjectIfMissing: empty project name")
		return ""
	}
	payload := map[string]string{"name": name}
	if description != "" {
		payload["description"] = description
	}
	b, err := json.Marshal(payload)
	if err != nil {
		log.Printf("❌ [DEBUG] createProjectIfMissing: failed to marshal payload for project '%s': %v", name, err)
		return ""
	}
	resp, err := http.Post(m.hdnURL+"/api/v1/projects", "application/json", strings.NewReader(string(b)))
	if err != nil {
		log.Printf("❌ [DEBUG] createProjectIfMissing: failed to POST project '%s': %v", name, err)
		return ""
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.Printf("❌ [DEBUG] createProjectIfMissing: HTTP %d creating project '%s', response: %s", resp.StatusCode, name, string(body))
		return ""
	}
	var project map[string]interface{}
	if err := json.Unmarshal(body, &project); err != nil {
		log.Printf("❌ [DEBUG] createProjectIfMissing: failed to unmarshal response for project '%s': %v, body: %s", name, err, string(body))
		return ""
	}
	if id, ok := project["id"].(string); ok && id != "" {
		log.Printf("✅ [DEBUG] createProjectIfMissing: successfully created project '%s' with ID %s", name, id)
		return id
	}
	log.Printf("❌ [DEBUG] createProjectIfMissing: project '%s' created but no ID in response: %v", name, project)
	return ""
}

// tryCreateProjectFromInput detects simple NL intents for creating a project and executes them.
// Returns (true, payload) if a project was created; otherwise (false, nil).
func (m *MonitorService) tryCreateProjectFromInput(input string) (bool, map[string]interface{}) {
	text := strings.TrimSpace(strings.ToLower(input))
	if text == "" {
		return false, nil
	}

	re := regexp.MustCompile(`^(create|make|new)\s+project(\s+named)?\s+([^,]+?)(\s+with\s+(description|desc)\s+(.*))?$`)
	m2 := re.FindStringSubmatch(text)
	if len(m2) == 0 {
		return false, nil
	}

	name := strings.TrimSpace(m2[3])
	var description string
	if len(m2) >= 7 {
		description = strings.TrimSpace(m2[6])
	}
	if name == "" {
		return false, nil
	}

	name = strings.Title(name)

	payload := map[string]interface{}{
		"name":        name,
		"description": description,
	}

	b, _ := json.Marshal(payload)
	resp, err := http.Post(m.hdnURL+"/api/v1/projects", "application/json", strings.NewReader(string(b)))
	if err != nil {
		return true, map[string]interface{}{
			"success": false,
			"message": "Failed to create project",
			"error":   err.Error(),
		}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var project map[string]interface{}
	_ = json.Unmarshal(body, &project)

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return true, map[string]interface{}{
			"success": true,
			"message": fmt.Sprintf("Project '%s' created", name),
			"project": project,
		}
	}

	return true, map[string]interface{}{
		"success": false,
		"message": "Failed to create project",
		"project": project,
		"status":  resp.StatusCode,
	}
}
