package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"
)

// SearchFlightsWithScraper uses the remote playwright_scraper service to perform the interaction
func SearchFlightsWithScraper(scraperURL string, opts SearchOptions) ([]FlightInfo, error) {
	log.Printf("🚀 Connecting to Playwright Service at: %s", scraperURL)

	// Build exact interaction steps for the remote service
	steps := []map[string]interface{}{
		{"type": "goto", "params": map[string]interface{}{"url": "https://www.google.com/travel/flights?hl=en-US&gl=US&curr=EUR", "waitUntil": "networkidle", "timeout": 60000}},
		{"type": "bypassConsent", "params": map[string]interface{}{"timeout": 5000}},
		{"type": "locator", "params": map[string]interface{}{"selector": "input[aria-label='Where from?'], input[placeholder='Where from?']", "action": "click"}},
		{"type": "keyboardPress", "params": map[string]interface{}{"key": "Control+A"}},
		{"type": "keyboardPress", "params": map[string]interface{}{"key": "Backspace"}},
		{"type": "keyboardType", "params": map[string]interface{}{"text": opts.Departure, "delay": 100}},
		{"type": "keyboardPress", "params": map[string]interface{}{"key": "Enter"}},
		{"type": "wait", "params": map[string]interface{}{"ms": 1000}},
		
		{"type": "locator", "params": map[string]interface{}{"selector": "input[aria-label='Where to?'], input[placeholder='Where to?']", "action": "click"}},
		{"type": "keyboardPress", "params": map[string]interface{}{"key": "Control+A"}},
		{"type": "keyboardPress", "params": map[string]interface{}{"key": "Backspace"}},
		{"type": "keyboardType", "params": map[string]interface{}{"text": opts.Destination, "delay": 100}},
		{"type": "keyboardPress", "params": map[string]interface{}{"key": "Enter"}},
		{"type": "wait", "params": map[string]interface{}{"ms": 1000}},
		
		{"type": "locator", "params": map[string]interface{}{"selector": "input[placeholder='Departure'], input[aria-label='Departure']", "action": "click"}},
		{"type": "wait", "params": map[string]interface{}{"ms": 1000}},
		{"type": "keyboardPress", "params": map[string]interface{}{"key": "Control+A"}},
		{"type": "keyboardType", "params": map[string]interface{}{"text": opts.StartDate, "delay": 50}},
		{"type": "keyboardPress", "params": map[string]interface{}{"key": "Tab"}},
		{"type": "wait", "params": map[string]interface{}{"ms": 500}},
		{"type": "keyboardPress", "params": map[string]interface{}{"key": "Control+A"}},
		{"type": "keyboardType", "params": map[string]interface{}{"text": opts.EndDate, "delay": 50}},
		{"type": "keyboardPress", "params": map[string]interface{}{"key": "Enter"}},
		{"type": "wait", "params": map[string]interface{}{"ms": 1000}},
		
		{"type": "keyboardPress", "params": map[string]interface{}{"key": "Enter"}},
		{"type": "wait", "params": map[string]interface{}{"ms": 2000}},
		{"type": "locator", "params": map[string]interface{}{"selector": "button:has-text('Search'), button:has-text('Explore')", "action": "click"}},
		{"type": "wait", "params": map[string]interface{}{"ms": 25000}},
	}

	payload := map[string]interface{}{"operations": steps}
	jsonReq, _ := json.Marshal(payload)
	
	resp, err := http.Post(scraperURL+"/api/multi-selector", "application/json", bytes.NewBuffer(jsonReq))
	if err != nil { return nil, fmt.Errorf("failed to call scraper service: %v", err) }
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("scraper service returned status: %d", resp.StatusCode)
	}

	var scraperResp struct {
		JobId string `json:"job_id"`
		Status string `json:"status"`
		Data struct {
			Text string `json:"text"`
			HTML string `json:"html"`
		} `json:"data"`
	}
	
	if err := json.NewDecoder(resp.Body).Decode(&scraperResp); err != nil {
		return nil, fmt.Errorf("failed to decode scraper response: %v", err)
	}

	if scraperResp.Status == "failed" {
		return nil, fmt.Errorf("scraper job failed")
	}

	log.Printf("📥 Scraper job finished. Processing data...")
	
	flights := ParseFlightText(scraperResp.Data.Text)
	if len(flights) == 0 && scraperResp.Data.HTML != "" {
		log.Printf("⚠️ OCR text parse failed. Attempting SMART Miner on HTML (%d bytes)...", len(scraperResp.Data.HTML))
		flights, err = MinerExtractFlights(scraperResp.Data.HTML)
	}

    if err != nil { return nil, err }
	for i := range flights {
		flights[i].URL = "https://www.google.com/travel/flights" // Placeholder for now
	}

	return flights, nil
}

func MinerExtractFlights(data string) ([]FlightInfo, error) {
    isHTML := strings.Contains(data, "<html")
    snippet := data
    if isHTML {
        re := regexp.MustCompile(`\["([^"]+)",\["([^"]+)",.+?\d+\][,\]]`)
        matches := re.FindAllString(data, 100)
        if len(matches) > 0 {
            snippet = strings.Join(matches, "\n")
        } else {
            pos := strings.Index(data, "round trip")
            if pos == -1 { pos = len(data) / 2 }
            start, end := pos-50000, pos+250000
            if start < 0 { start = 0 }
            if end > len(data) { end = len(data) }
            snippet = data[start:end]
        }
    } else {
        if len(snippet) > 100000 { snippet = snippet[:100000] }
    }

	prompt := fmt.Sprintf(`Extract flight results from this %s data.
Return ONLY a JSON list of objects: "airline", "departure_time", "arrival_time", "duration", "stops", "price".

Data:
%s`, func() string { if isHTML { return "HTML" }; return "OCR text" }(), snippet)

	ollamaReq := map[string]interface{}{
		"model": "qwen3:14b", "prompt": prompt, "stream": false, "format": "json",
	}

	jsonReq, _ := json.Marshal(ollamaReq)
	resp, err := http.Post("http://localhost:11434/api/generate", "application/json", bytes.NewBuffer(jsonReq))
	if err != nil { return nil, err }
	defer resp.Body.Close()

	var ollamaResp struct { Response string `json:"response"` }
	json.NewDecoder(resp.Body).Decode(&ollamaResp)

	var flights []struct {
		Airline       string `json:"airline"`
		DepartureTime string `json:"departure_time"`
		ArrivalTime   string `json:"arrival_time"`
		Duration      string `json:"duration"`
		Stops         string `json:"stops"`
		Price         string `json:"price"`
	}

	if err := json.Unmarshal([]byte(ollamaResp.Response), &flights); err != nil {
		var wrapper struct { Flights []struct {
            Airline       string `json:"airline"`
            DepartureTime string `json:"departure_time"`
            ArrivalTime   string `json:"arrival_time"`
            Duration      string `json:"duration"`
            Stops         string `json:"stops"`
            Price         string `json:"price"`
        } `json:"flights"` }
		if err2 := json.Unmarshal([]byte(ollamaResp.Response), &wrapper); err2 == nil {
			flights = wrapper.Flights
		} else {
			return nil, fmt.Errorf("parse fail: %s", ollamaResp.Response)
		}
	}

	var result []FlightInfo
	for _, f := range flights {
		result = append(result, FlightInfo{
			Airline: f.Airline, Price: f.Price, Duration: f.Duration,
			Stops: f.Stops, DepartureTime: f.DepartureTime, ArrivalTime: f.ArrivalTime,
		})
	}
	return result, nil
}
