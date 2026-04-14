package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"
)

func ExtractOptionsFromQuery(query string) (SearchOptions, error) {
	currentYear := time.Now().Year()
	currentDate := time.Now().Format("2006-01-02 (Monday)")
	prompt := fmt.Sprintf(`Task: Extract flight search parameters from the natural language query.
Present Date: %s

Query: %s

Return a JSON object with these fields ONLY:
- departure (airport code, e.g. "LHR")
- destination (airport code, e.g. "CDG")
- start_date (YYYY-MM-DD)
- end_date (ONLY if a return flight or stay duration was specifically mentioned. Otherwise, leave it empty. YYYY-MM-DD or empty.)
- cabin (Default to "Economy". Use "Business" or "First" if specifically mentioned.)

IMPORTANT: If no year is specified in the query, you MUST use %d.
EXAMPLES:
- "Find morning flights to JFK" -> cabin: "Economy"
- "Business class to PAR" -> cabin: "Business"
- "LHR to CDG tomorrow" -> cabin: "Economy"

ONLY return JSON.`, currentDate, query, currentYear)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	model := os.Getenv("LLM_MODEL")
	if model == "" {
		model = "qwen3:14b"
	}

	ollamaReq := map[string]interface{}{
		"model":  model,
		"prompt": prompt,
		"stream": false,
	}
	jsonReq, _ := json.Marshal(ollamaReq)

	client := &http.Client{}
	req, _ := http.NewRequestWithContext(ctx, "POST", getOllamaURL()+"/api/generate", bytes.NewBuffer(jsonReq))
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return SearchOptions{}, err
	}
	defer resp.Body.Close()

	var ollamaResp struct {
		Response string `json:"response"`
	}
	json.NewDecoder(resp.Body).Decode(&ollamaResp)

	// Simple JSON cleaner (remove markdown blocks)
	cleanResp := ollamaResp.Response
	if idx := bytes.Index([]byte(cleanResp), []byte("```json")); idx != -1 {
		cleanResp = cleanResp[idx+7:]
	} else if idx := bytes.Index([]byte(cleanResp), []byte("```")); idx != -1 {
		cleanResp = cleanResp[idx+3:]
	}
	if idx := bytes.LastIndex([]byte(cleanResp), []byte("```")); idx != -1 {
		cleanResp = cleanResp[:idx]
	}

	var result struct {
		Departure   string `json:"departure"`
		Destination string `json:"destination"`
		StartDate   string `json:"start_date"`
		EndDate     string `json:"end_date"`
		Cabin       string `json:"cabin"`
	}
	err = json.Unmarshal([]byte(cleanResp), &result)
	if err != nil {
		return SearchOptions{}, fmt.Errorf("failed to parse LLM extraction: %v, raw: %s", err, cleanResp)
	}

	// Safety: Ensure dates are in the future (at least current year)
	yearStr := fmt.Sprintf("%d", currentYear)
	normalizeYear := func(d string) string {
		if len(d) >= 4 && d[:4] < yearStr {
			return yearStr + d[4:]
		}
		return d
	}
	result.StartDate = normalizeYear(result.StartDate)
	result.EndDate = normalizeYear(result.EndDate)

	return SearchOptions{
		Departure:   result.Departure,
		Destination: result.Destination,
		StartDate:   result.StartDate,
		EndDate:     result.EndDate,
		CabinClass:  result.Cabin,
	}, nil
}
