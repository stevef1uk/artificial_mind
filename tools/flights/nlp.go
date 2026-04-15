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

	// Calculate "this Friday" and "next Friday"
	today := time.Now()
	daysUntilFriday := int(time.Friday - today.Weekday())
	if daysUntilFriday < 0 {
		daysUntilFriday += 7 // Next Friday if already passed
	}
	thisFriday := today.AddDate(0, 0, daysUntilFriday)
	thisFridayStr := thisFriday.Format("2006-01-02")

	prompt := fmt.Sprintf(`### TASK: Extract flight search parameters from the natural language query.
### CONTEXT:
- Present Date: %s
- IMPORTANT: If no year is specified in the query, you MUST use %d.
- SPECIAL DATE MAPPING:
  * "tomorrow" = %s
  * "this Friday" = %s (NOT next Friday!)
  * "next Friday" = the Friday AFTER this Friday
  * "this weekend" = Saturday and Sunday of THIS week
- Mapping Precision: 
  * Geneva -> GVA
  * London Gatwick or Gatwick -> LGW (NEVER default to Heathrow/LHR if Gatwick mentioned)
  * London -> LON (or LHR if Heathrow mentioned)
  * Lisbon -> LIS

### USER QUERY:
%s

### RULES:
1. Return a VALID JSON object with: departure, destination, start_date, end_date, cabin.
2. end_date: Return date (YYYY-MM-DD). LEAVE EMPTY for one-way trips. 
3. IMPORTANT: If the user says "take a plane back to the UK", "return home to London", or "flying back tomorrow", this is a ONE-WAY trip unless a stay duration or second date is explicitly mentioned.
4. Default cabin is "Economy".

JSON RESULT:`, currentDate, currentYear, time.Now().AddDate(0, 0, 1).Format("2006-01-02"), thisFridayStr, query)

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
