package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
)

func SearchFlightsWithScraper(scraperURL string, opts SearchOptions) ([]FlightInfo, error) {
	log.Printf("🛰️ Using Playwright Service (Version 91) at: %s", scraperURL)

	tsConfig := fmt.Sprintf(`
		await page.setViewportSize({ width: 1920, height: 1080 });
		await page.goto("https://www.google.com/travel/flights?q=%s+flights+from+%s+to+%s+on+%s+return+%s&hl=en-US&gl=US&curr=EUR");
		await page.waitForLoadState("networkidle");
		
		// 1. Consent
		await page.getByRole("button", { name: "Accept all" }).first().click();
		await page.waitForTimeout(2000); 

		// 2. Long wait for results
		await page.waitForTimeout(30000); 
	`, opts.CabinClass, opts.Departure, opts.Destination, opts.StartDate, opts.EndDate)

	payload := map[string]interface{}{
		"url":               "https://www.google.com/travel/flights?hl=en-US&gl=US&curr=EUR",
		"typescript_config": tsConfig,
		"get_html":          true,
	}

	body, _ := json.Marshal(payload)
	resp, err := http.Post(scraperURL+"/scrape/start", "application/json", bytes.NewBuffer(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var startResp struct {
		JobID string `json:"job_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&startResp); err != nil {
		return nil, err
	}

	log.Printf("📥 Scraper job %s created. Polling...", startResp.JobID)

	// Poll for completion
	for attempt := 0; attempt < 120; attempt++ {
		time.Sleep(2 * time.Second)
		jobURL := scraperURL + "/scrape/job?job_id=" + startResp.JobID
		jobResp, err := http.Get(jobURL)
		if err != nil {
			continue
		}
		
		var job struct {
			Status string                 `json:"status"`
			Result map[string]interface{} `json:"result"`
			Error  string                 `json:"error"`
		}
		if err := json.NewDecoder(jobResp.Body).Decode(&job); err != nil {
			jobResp.Body.Close()
			continue
		}
		jobResp.Body.Close()

		if job.Status == "completed" {
			log.Printf("✅ Job %s completed. Processing results...", startResp.JobID)
			if b64, ok := job.Result["screenshot"].(string); ok && b64 != "" {
				dataStr := strings.TrimPrefix(b64, "data:image/png;base64,")
				imgData, _ := base64.StdEncoding.DecodeString(dataStr)
				tmpPath := "/home/stevef/dev/artificial_mind/tools/flights/remote_flight_screenshot.png"
				_ = os.WriteFile(tmpPath, imgData, 0644)
				
				flights, _ := ExtractFlightsFromImage(tmpPath)
				if len(flights) > 0 {
					log.Printf("🎉 Found %d flights via OCR", len(flights))
					return flights, nil
				}
			}

			html := ""
			if h, ok := job.Result["cleaned_html"].(string); ok {
				html = h
			}
			log.Printf("📊 HTML Miner fallback (%d bytes)...", len(html))
			return MinerExtractFlights(html)
		}

		if job.Status == "failed" {
			return nil, fmt.Errorf("job failed: %s", job.Error)
		}
	}

	return nil, fmt.Errorf("job timed-out")
}

func MinerExtractFlights(data string) ([]FlightInfo, error) {
	if len(data) == 0 {
		return nil, nil
	}
	isHTML := strings.Contains(data, "<html")
	snippet := data
	if isHTML {
		re := regexp.MustCompile(`\["([^"]+)",\["([^"]+)",.+?\d+\][,\]]`)
		matches := re.FindAllString(data, 100)
		if len(matches) > 0 {
			snippet = strings.Join(matches, "\n")
		} else {
			pos := strings.Index(strings.ToLower(data), "round trip")
			if pos == -1 {
				pos = len(data) / 2
			}
			start, end := pos-5000, pos+25000
			if start < 0 {
				start = 0
			}
			if end > len(data) {
				end = len(data)
			}
			snippet = data[start:end]
		}
	}

	prompt := fmt.Sprintf(`Extract flight results from this data.
Return ONLY JSON list of objects: "airline", "departure_time", "arrival_time", "duration", "stops", "price".
Data:
%s`, snippet)

	log.Printf("🤖 Calling LLM Miner (%d chars)...", len(snippet))

	// Use timed HTTP Client for Ollama
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	ollamaReq := map[string]interface{}{"model": "qwen3:14b", "prompt": prompt, "stream": false, "format": "json"}
	jsonReq, _ := json.Marshal(ollamaReq)

	client := &http.Client{}
	req, _ := http.NewRequestWithContext(ctx, "POST", "http://localhost:11434/api/generate", bytes.NewBuffer(jsonReq))
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("⚠️ Ollama call failed or timed out: %v", err)
		return nil, err
	}
	defer resp.Body.Close()

	var ollamaResp struct {
		Response string `json:"response"`
	}
	json.NewDecoder(resp.Body).Decode(&ollamaResp)

	log.Printf("🤖 LLM Response: %s", func() string {
		if len(ollamaResp.Response) > 100 {
			return ollamaResp.Response[:100] + "..."
		}
		return ollamaResp.Response
	}())

	var flights []FlightInfo
	if err := json.Unmarshal([]byte(ollamaResp.Response), &flights); err != nil {
		var wrapper struct {
			Flights []FlightInfo `json:"flights"`
		}
		if err2 := json.Unmarshal([]byte(ollamaResp.Response), &wrapper); err2 == nil {
			flights = wrapper.Flights
		}
	}
	log.Printf("🚀 Miner found %d flights", len(flights))
	return flights, nil
}
