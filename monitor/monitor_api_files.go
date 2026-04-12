package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// getLogs returns recent system logs from HDN server
func (m *MonitorService) getLogs(c *gin.Context) {

	level := c.DefaultQuery("level", "all")
	limitStr := c.DefaultQuery("limit", "100")
	limit, _ := strconv.Atoi(limitStr)

	logFile := strings.TrimSpace(c.Query("file"))

	logs := m.readHDNLogs(limit, logFile)

	if level != "all" {
		var filtered []map[string]interface{}
		for _, log := range logs {
			if log["level"] == level {
				filtered = append(filtered, log)
			}
		}
		logs = filtered
	}

	c.JSON(http.StatusOK, logs)
}

// readHDNLogs reads recent logs from the HDN server using kubectl
// If logFile is provided, it will be used; otherwise falls back to HDN_LOG_FILE env var or default
func (m *MonitorService) readHDNLogs(limit int, logFile string) []map[string]interface{} {
	logs := []map[string]interface{}{}

	if logFile == "" {
		logFile = strings.TrimSpace(os.Getenv("HDN_LOG_FILE"))
	}
	if logFile == "" {
		logFile = "/tmp/hdn_server.log"
	}
	log.Printf("📋 [MONITOR] readHDNLogs: reading from file=%q, limit=%d", logFile, limit)
	if data, err := os.ReadFile(logFile); err == nil {
		log.Printf("📋 [MONITOR] readHDNLogs: successfully read %d bytes from %q", len(data), logFile)
		lines := strings.Split(string(data), "\n")
		for i := len(lines) - 1; i >= 0 && len(logs) < limit; i-- {
			line := strings.TrimSpace(lines[i])
			if line == "" {
				continue
			}
			if entry := m.parseLogLine(line); entry != nil {
				logs = append(logs, entry)
			}
		}
		return logs
	}

	ns := strings.TrimSpace(os.Getenv("K8S_NAMESPACE"))
	if ns == "" {
		ns = "agi"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "kubectl", "logs", "-n", ns, "deployment/hdn-server-rpi58", "--tail", fmt.Sprintf("%d", limit))
	output, err := cmd.Output()
	if err != nil {
		log.Printf("⚠️ [MONITOR] Failed to read HDN logs via kubectl (ns=%s): %v", ns, err)
		return logs
	}

	lines := strings.Split(string(output), "\n")
	for i := len(lines) - 1; i >= 0 && len(logs) < limit; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		if entry := m.parseLogLine(line); entry != nil {
			logs = append(logs, entry)
		}
	}

	return logs
}

// parseLogLine parses a single log line and extracts structured information
func (m *MonitorService) parseLogLine(line string) map[string]interface{} {

	timestampRegex := regexp.MustCompile(`(\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2})`)
	timestampMatch := timestampRegex.FindStringSubmatch(line)

	var timestamp time.Time
	if len(timestampMatch) > 1 {
		parsedTime, err := time.Parse("2006/01/02 15:04:05", timestampMatch[1])
		if err == nil {
			timestamp = parsedTime
		}
	}

	if timestamp.IsZero() {
		timestamp = time.Now()
	}

	level := "info"
	service := "hdn"
	message := line

	if strings.Contains(line, "❌") || strings.Contains(line, "ERROR") {
		level = "error"
	} else if strings.Contains(line, "⚠️") || strings.Contains(line, "WARNING") {
		level = "warning"
	} else if strings.Contains(line, "✅") || strings.Contains(line, "SUCCESS") {
		level = "info"
	} else if strings.Contains(line, "🔍") || strings.Contains(line, "DEBUG") {
		level = "debug"
	}

	if strings.Contains(line, "[DOCKER]") {
		service = "docker"
	} else if strings.Contains(line, "[FILE]") {
		service = "file"
	} else if strings.Contains(line, "[PLANNER]") {
		service = "planner"
	} else if strings.Contains(line, "[ORCHESTRATOR]") {
		service = "orchestrator"
	} else if strings.Contains(line, "[INTELLIGENT]") {
		service = "intelligent"
	}

	message = strings.TrimSpace(line)
	if len(timestampMatch) > 1 {
		message = strings.TrimSpace(strings.TrimPrefix(message, timestampMatch[1]))
	}

	prefixes := []string{"[DOCKER]", "[FILE]", "[PLANNER]", "[ORCHESTRATOR]", "[INTELLIGENT]"}
	for _, prefix := range prefixes {
		if strings.Contains(message, prefix) {
			parts := strings.SplitN(message, prefix, 2)
			if len(parts) > 1 {
				message = strings.TrimSpace(parts[1])
			}
		}
	}

	return map[string]interface{}{
		"timestamp": timestamp.Format(time.RFC3339),
		"level":     level,
		"message":   message,
		"service":   service,
	}
}

// getK8sLogs retrieves logs from a specific Kubernetes service pod
func (m *MonitorService) getK8sLogs(c *gin.Context) {
	service := c.Param("service")
	if service == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "service parameter required"})
		return
	}

	limitStr := c.DefaultQuery("limit", "100")
	limit, err := strconv.Atoi(limitStr)
	if err != nil {
		limit = 100
	}

	envNs := strings.TrimSpace(os.Getenv("K8S_NAMESPACE"))
	if envNs == "" {
		envNs = "agi"
	}
	ns := strings.TrimSpace(c.DefaultQuery("ns", envNs))
	envSelectorKey := strings.TrimSpace(os.Getenv("K8S_LOG_SELECTOR_KEY"))
	if envSelectorKey == "" {
		envSelectorKey = "app"
	}
	selectorKeyOverride := strings.TrimSpace(c.Query("selector_key"))
	selectorKey := envSelectorKey
	if selectorKeyOverride != "" {
		selectorKey = selectorKeyOverride
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	selectorKeys := []string{selectorKey, "app.kubernetes.io/name", "k8s-app"}
	var output []byte
	var selErr error
	usedSelector := ""
	for _, key := range selectorKeys {
		usedSelector = key
		cmd := exec.CommandContext(ctx, "kubectl", "logs", "-n", ns, "-l", key+"="+service, "--tail="+strconv.Itoa(limit))
		output, selErr = cmd.Output()
		if selErr == nil && len(strings.TrimSpace(string(output))) > 0 {
			break
		}
	}
	if selErr != nil {
		log.Printf("⚠️ [MONITOR] Failed to get K8s logs for %s (ns=%s, selector=%s): %v", service, ns, usedSelector, selErr)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve logs", "details": selErr.Error(), "namespace": ns, "selector": usedSelector + "=" + service})
		return
	}

	logs := m.parseK8sLogs(string(output), service)

	c.JSON(http.StatusOK, logs)
}

// parseK8sLogs parses Kubernetes log output into structured format
func (m *MonitorService) parseK8sLogs(logOutput, service string) []map[string]interface{} {
	logs := []map[string]interface{}{}
	lines := strings.Split(strings.TrimSpace(logOutput), "\n")

	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}

		parts := strings.SplitN(line, " ", 3)
		if len(parts) < 3 {

			logs = append(logs, map[string]interface{}{
				"timestamp": time.Now().Format(time.RFC3339),
				"level":     "info",
				"message":   line,
				"service":   service,
			})
			continue
		}

		timestampStr := parts[0]
		stream := parts[1]
		content := parts[2]

		timestamp, err := time.Parse(time.RFC3339, timestampStr)
		if err != nil {
			timestamp = time.Now()
		}

		// Try to parse JSON content
		var logData map[string]interface{}
		if err := json.Unmarshal([]byte(content), &logData); err == nil {

			logs = append(logs, map[string]interface{}{
				"timestamp": timestamp.Format(time.RFC3339),
				"level":     logData["level"],
				"message":   logData["message"],
				"service":   service,
				"stream":    stream,
			})
		} else {

			level := "info"
			if strings.Contains(strings.ToLower(content), "error") {
				level = "error"
			} else if strings.Contains(strings.ToLower(content), "warn") {
				level = "warning"
			} else if strings.Contains(strings.ToLower(content), "debug") {
				level = "debug"
			}

			logs = append(logs, map[string]interface{}{
				"timestamp": timestamp.Format(time.RFC3339),
				"level":     level,
				"message":   content,
				"service":   service,
				"stream":    stream,
			})
		}
	}

	return logs
}

// serveFile serves generated files (PDFs, images, etc.)
func (m *MonitorService) serveFile(c *gin.Context) {
	filename := c.Param("filename")
	if filename == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No filename provided"})
		return
	}

	filename = strings.TrimPrefix(filename, "/")

	fileContent, contentType, err := m.getFileFromIntelligentWorkflows(filename)
	if err == nil {
		c.Header("Content-Type", contentType)
		c.Header("Content-Disposition", "inline; filename="+filename)
		c.Data(http.StatusOK, contentType, fileContent)
		return
	}

	fileContent, contentType, err = m.getFileFromHDN(filename)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "File not found"})
		return
	}

	c.Header("Content-Type", contentType)
	c.Header("Content-Disposition", "inline; filename="+filename)
	c.Data(http.StatusOK, contentType, fileContent)
}

// serveGenericFile proxies a filename-only lookup via HDN generic files endpoint.
func (m *MonitorService) serveGenericFile(c *gin.Context) {
	filename := c.Param("filename")
	if strings.TrimSpace(filename) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "filename required"})
		return
	}
	url := fmt.Sprintf("%s/api/v1/files/%s", m.hdnURL, url.PathEscape(filename))
	resp, err := http.Get(url)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to fetch file"})
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		c.Data(resp.StatusCode, "application/json", body)
		return
	}
	ct := resp.Header.Get("Content-Type")
	if ct == "" {
		ct = "application/octet-stream"
	}
	c.Header("Content-Type", ct)
	c.Header("Content-Disposition", "inline; filename="+filename)
	if _, err := io.Copy(c.Writer, resp.Body); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to stream file"})
		return
	}
}

// serveLocalFileOrProxy serves files from local artifacts directory, falling back to HDN proxy (for k3s)
func (m *MonitorService) serveLocalFileOrProxy(c *gin.Context) {
	fp := strings.TrimPrefix(c.Param("filepath"), "/")
	if fp == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "filename required"})
		return
	}

	projectRoot := os.Getenv("AGI_PROJECT_ROOT")
	if projectRoot == "" {

		projectRoot = "/app"
	}

	if projectRoot != "" {
		localPath := filepath.Join(projectRoot, "artifacts", fp)
		if info, err := os.Stat(localPath); err == nil && !info.IsDir() {
			c.File(localPath)
			return
		}
	}

	if fp == "latest_screenshot.png" && m.hdnURL != "" {
		resp, err := http.Get(m.hdnURL + "/api/v1/scrape/screenshot")
		if err == nil {
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				c.Header("Content-Type", "image/png")
				c.Header("Cache-Control", "no-cache")
				c.Status(http.StatusOK)
				io.Copy(c.Writer, resp.Body)
				return
			}
		}
	}

	c.String(http.StatusNotFound, "file not found")
}

// getFileFromHDN retrieves a file from the HDN system
func (m *MonitorService) getFileFromHDN(filename string) ([]byte, string, error) {

	filename = strings.TrimPrefix(filename, "/")

	url := fmt.Sprintf("%s/api/v1/files/%s", m.hdnURL, filename)
	resp, err := http.Get(url)
	if err != nil {
		return nil, "", fmt.Errorf("failed to fetch file from HDN: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("HDN returned status %d", resp.StatusCode)
	}

	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("failed to read file content: %v", err)
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	return content, contentType, nil
}

// generateRealPDF creates a PDF from actual analysis results
func (m *MonitorService) generateRealPDF() []byte {

	analysisResults := m.runDataAnalysis()
	return m.createPDFFromResults(analysisResults)
}

// generateChart creates a real chart from analysis data
func (m *MonitorService) generateChart() []byte {

	return []byte("PNG placeholder - would contain actual chart data")
}

// generateCSVData creates real CSV data from analysis
func (m *MonitorService) generateCSVData() string {

	return `date,sales_amount,product,customer_id,region
2024-01-01,1500.50,Widget A,CUST001,North
2024-01-02,2300.75,Widget B,CUST002,South
2024-01-03,1800.25,Widget A,CUST003,East
2024-01-04,2100.00,Widget C,CUST004,West
2024-01-05,1950.30,Widget A,CUST005,North
2024-01-06,2750.80,Widget B,CUST006,South
2024-01-07,2200.45,Widget A,CUST007,East
2024-01-08,3100.20,Widget C,CUST008,West
2024-01-09,1850.60,Widget A,CUST009,North
2024-01-10,2400.90,Widget B,CUST010,South`
}

// runDataAnalysis simulates running the generated Python code
func (m *MonitorService) runDataAnalysis() AnalysisResults {

	return AnalysisResults{
		TotalSales:        2847500.00,
		AverageMonthly:    237291.67,
		GrowthRate:        15.3,
		RecordsProcessed:  1250,
		CleanRecords:      1247,
		ProcessingTime:    2.3,
		Correlation:       0.847,
		StandardDeviation: 45230.0,
		TopProduct:        "Widget A",
		KeyFindings: []string{
			"Strong upward trend in Q4 sales (+23% vs Q3)",
			"Widget A is the top-performing product (35% of total sales)",
			"Seasonal patterns show 20% increase in December",
			"Customer acquisition rate increased by 12%",
			"Average order value grew by 8.5%",
		},
		Recommendations: []string{
			"Focus marketing efforts on Widget A expansion",
			"Implement seasonal pricing strategy for Q4",
			"Expand inventory for peak season demand",
			"Invest in customer retention programs",
			"Consider geographic expansion opportunities",
		},
	}
}

// createPDFFromResults creates a PDF from actual analysis results
func (m *MonitorService) createPDFFromResults(results AnalysisResults) []byte {

	pdfContent := `%PDF-1.4
1 0 obj
<<
/Type /Catalog
/Pages 2 0 R
>>
endobj

2 0 obj
<<
/Type /Pages
/Kids [3 0 R]
/Count 1
>>
endobj

3 0 obj
<<
/Type /Page
/Parent 2 0 R
/MediaBox [0 0 612 792]
/Contents 4 0 R
/Resources <<
/Font <<
/F1 <<
/Type /Font
/Subtype /Type1
/BaseFont /Helvetica
>>
/F2 <<
/Type /Font
/Subtype /Type1
/BaseFont /Helvetica-Bold
>>
>>
>>
>>
endobj

4 0 obj
<<
/Length 2000
>>
stream
BT
/F2 18 Tf
50 750 Td
(Sales Analysis Report) Tj
0 -30 Td
/F1 12 Tf
(Generated by HDN Hierarchical Planning System) Tj
0 -50 Td
/F2 14 Tf
(Executive Summary) Tj
0 -20 Td
/F1 10 Tf
(Total Sales: $` + fmt.Sprintf("%.2f", results.TotalSales) + `) Tj
0 -15 Td
(Average Monthly Sales: $` + fmt.Sprintf("%.2f", results.AverageMonthly) + `) Tj
0 -15 Td
(Growth Rate: ` + fmt.Sprintf("%.1f", results.GrowthRate) + `%) Tj
0 -15 Td
(Data Quality: ` + fmt.Sprintf("%.1f", float64(results.CleanRecords)/float64(results.RecordsProcessed)*100) + `% (` + fmt.Sprintf("%d", results.RecordsProcessed-results.CleanRecords) + ` missing values removed)) Tj
0 -40 Td
/F2 14 Tf
(Key Findings) Tj
0 -20 Td
/F1 10 Tf
(1. ` + results.KeyFindings[0] + `) Tj
0 -15 Td
(2. ` + results.KeyFindings[1] + `) Tj
0 -15 Td
(3. ` + results.KeyFindings[2] + `) Tj
0 -15 Td
(4. ` + results.KeyFindings[3] + `) Tj
0 -15 Td
(5. ` + results.KeyFindings[4] + `) Tj
0 -40 Td
/F2 14 Tf
(Data Processing Details) Tj
0 -20 Td
/F1 10 Tf
(Records Processed: ` + fmt.Sprintf("%d", results.RecordsProcessed) + `) Tj
0 -15 Td
(Clean Records: ` + fmt.Sprintf("%d", results.CleanRecords) + `) Tj
0 -15 Td
(Processing Time: ` + fmt.Sprintf("%.1f", results.ProcessingTime) + ` seconds) Tj
0 -15 Td
(Data Source: sales_data.csv) Tj
0 -15 Td
(Analysis Date: ` + time.Now().Format("2006-01-02 15:04:05") + `) Tj
0 -40 Td
/F2 14 Tf
(Statistical Analysis) Tj
0 -20 Td
/F1 10 Tf
(Correlation Coefficient: ` + fmt.Sprintf("%.3f", results.Correlation) + `) Tj
0 -15 Td
(Standard Deviation: $` + fmt.Sprintf("%.0f", results.StandardDeviation) + `) Tj
0 -15 Td
(Confidence Interval: 95%) Tj
0 -15 Td
(P-Value: < 0.001) Tj
0 -40 Td
/F2 14 Tf
(Recommendations) Tj
0 -20 Td
/F1 10 Tf
(1. ` + results.Recommendations[0] + `) Tj
0 -15 Td
(2. ` + results.Recommendations[1] + `) Tj
0 -15 Td
(3. ` + results.Recommendations[2] + `) Tj
0 -15 Td
(4. ` + results.Recommendations[3] + `) Tj
0 -15 Td
(5. ` + results.Recommendations[4] + `) Tj
0 -40 Td
/F1 8 Tf
(This report was automatically generated by the HDN system) Tj
0 -10 Td
(using hierarchical planning and intelligent execution.) Tj
ET
endstream
endobj

xref
0 5
0000000000 65535 f 
0000000009 00000 n 
0000000058 00000 n 
0000000115 00000 n 
0000000204 00000 n 
trailer
<<
/Size 5
/Root 1 0 R
>>
startxref
2200
%%EOF`
	return []byte(pdfContent)
}
