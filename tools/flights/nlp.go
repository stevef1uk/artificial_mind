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

	// Calculate date mappings
	today := time.Now()

	// Tomorrow
	tomorrow := today.AddDate(0, 0, 1)
	tomorrowStr := tomorrow.Format("2006-01-02")

	// This Friday (days until Friday, add 7 if negative)
	daysUntilFriday := int(time.Friday - today.Weekday())
	if daysUntilFriday <= 0 {
		daysUntilFriday += 7
	}
	thisFriday := today.AddDate(0, 0, daysUntilFriday)
	thisFridayStr := thisFriday.Format("2006-01-02")

	// Next Friday (this Friday + 7 days)
	nextFriday := thisFriday.AddDate(0, 0, 7)
	nextFridayStr := nextFriday.Format("2006-01-02")

	// This weekend (Saturday and Sunday)
	saturday := today.AddDate(0, 0, (int(time.Saturday)-int(today.Weekday())+7)%7)
	sunday := saturday.AddDate(0, 0, 1)
	weekendStr := saturday.Format("2006-01-02") + " and " + sunday.Format("2006-01-02")

	prompt := fmt.Sprintf(`### TASK: Extract flight search parameters from the natural language query.
### CONTEXT:
- Present Date: %s (%s)
- Year: %d
- TODAY IS: %s
- SPECIFIC DATE MAPPINGS (use these EXACT values):
  * "tomorrow" = %s
  * "this Friday" = %s (the upcoming Friday, NOT next week's!)
  * "next Friday" = %s (the Friday AFTER this Friday)
  * "this weekend" = %s
- Airport Codes:
  * Geneva -> GVA
  * London Gatwick or Gatwick -> LGW
  * London -> LON
  * Lisbon -> LIS

### USER QUERY:
%s

### RULES:
1. Return a VALID JSON object with: departure, destination, start_date, end_date, cabin.
2. end_date: Return date (YYYY-MM-DD). LEAVE EMPTY for one-way trips. 
3. If user says "tomorrow" and also mentions "next Friday", use tomorrow as departure and next Friday as return.
4. Default cabin is "Economy".

JSON RESULT:`, currentDate, today.Weekday(), currentYear, today.Format("2006-01-02"), tomorrowStr, thisFridayStr, nextFridayStr, weekendStr, query)

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
