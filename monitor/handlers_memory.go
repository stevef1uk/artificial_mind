package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// getMemorySummary proxies HDN memory summary
func (m *MonitorService) getMemorySummary(c *gin.Context) {
	sessionID := c.Query("session_id")
	url := m.hdnURL + "/api/v1/memory/summary"
	if sessionID != "" {
		url += "?session_id=" + urlQueryEscape(sessionID)
	}
	resp, err := http.Get(url)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to fetch memory summary"})
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	c.Data(resp.StatusCode, "application/json", body)
}

// searchEpisodes proxies episodic search with basic filters
func (m *MonitorService) searchEpisodes(c *gin.Context) {
	q := c.Query("q")
	limit := c.DefaultQuery("limit", "20")
	sessionID := c.Query("session_id")
	tag := c.Query("tag")
	// Build query string
	params := []string{}
	if q != "" {
		params = append(params, "q="+urlQueryEscape(q))
	}
	if limit != "" {
		params = append(params, "limit="+urlQueryEscape(limit))
	}
	if sessionID != "" {
		params = append(params, "session_id="+urlQueryEscape(sessionID))
	}
	if tag != "" {
		params = append(params, "tag="+urlQueryEscape(tag))
	}
	full := m.hdnURL + "/api/v1/episodes/search"
	if len(params) > 0 {
		full += "?" + strings.Join(params, "&")
	}
	resp, err := http.Get(full)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to fetch episodes"})
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	c.Data(resp.StatusCode, "application/json", body)
}

// cleanupSelfModelGoals proxies a cleanup request to HDN to remove internal self-model goals
func (m *MonitorService) cleanupSelfModelGoals(c *gin.Context) {
	// Accept optional patterns/statuses from client; default applied at HDN
	var bodyBytes []byte
	if c.Request.Body != nil {
		bodyBytes, _ = io.ReadAll(c.Request.Body)
	}
	url := m.hdnURL + "/api/v1/memory/goals/cleanup"
	resp, err := http.Post(url, "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to cleanup goals"})
		return
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	c.Data(resp.StatusCode, "application/json", b)
}

// NewsEvent represents a news event from BBC or Wikipedia
type NewsEvent struct {
	ID        string                 `json:"id"`
	Title     string                 `json:"title"`
	Text      string                 `json:"text"`
	Source    string                 `json:"source"`
	Type      string                 `json:"type"`
	Timestamp string                 `json:"timestamp"`
	Metadata  map[string]interface{} `json:"metadata"`
	Tags      []string               `json:"tags"`
}

// getNewsEvents fetches recent news events from Weaviate
func (m *MonitorService) getNewsEvents(c *gin.Context) {
	_ = c.DefaultQuery("limit", "20") // Future use for pagination

	// Query Weaviate for news events using GraphQL
	// News events are stored in WikipediaArticle class with source="news:fsm"
	query := map[string]interface{}{
		"query": `
		{
			Get {
				WikipediaArticle(limit: 200, where: {
					path: ["source"]
					operator: Equal
					valueString: "news:fsm"
				}) {
					_additional { id }
					title
					text
					source
					url
					timestamp
				}
			}
		}`,
	}

	queryBytes, _ := json.Marshal(query)
	url := m.weaviateURL + "/v1/graphql"
	resp, err := http.Post(url, "application/json", bytes.NewReader(queryBytes))
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to fetch news events"})
		return
	}
	defer resp.Body.Close()

	var weaviateResp struct {
		Data struct {
			Get struct {
				WikipediaArticle []struct {
					Additional struct {
						ID string `json:"id"`
					} `json:"_additional"`
					Title     string `json:"title"`
					Text      string `json:"text"`
					Source    string `json:"source"`
					URL       string `json:"url"`
					Timestamp string `json:"timestamp"`
				} `json:"WikipediaArticle"`
			} `json:"Get"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&weaviateResp); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to decode news events"})
		return
	}

	if len(weaviateResp.Errors) > 0 {
		c.JSON(http.StatusBadGateway, gin.H{"error": "Weaviate query error: " + weaviateResp.Errors[0].Message})
		return
	}

	// Convert to NewsEvent format
	var newsEvents []NewsEvent
	for _, record := range weaviateResp.Data.Get.WikipediaArticle {
		// Skip boring/system content
		if len(strings.TrimSpace(record.Text)) < 20 {
			continue
		}
		lowerText := strings.ToLower(record.Text)
		if strings.Contains(lowerText, "timer_tick") ||
			strings.Contains(lowerText, "outcome\":\"") ||
			strings.Contains(lowerText, "server busy") ||
			strings.Contains(lowerText, "execution_plan") ||
			strings.Contains(lowerText, "interpretation") {
			continue
		}
		// Skip tool creation events
		if strings.Contains(record.Title, "News Event: agi.tool.created") {
			continue
		}
		// Require basic ingestion signals
		if strings.TrimSpace(record.Title) == "" {
			continue
		}

		event := NewsEvent{
			ID:        record.Additional.ID,
			Title:     record.Title,
			Text:      record.Text,
			Source:    record.Source,
			Type:      "news",
			Timestamp: record.Timestamp,
			Metadata:  make(map[string]interface{}),
			Tags:      []string{"news"},
		}

		// Add URL if present, but don't require it (some news items may not have URLs)
		if strings.TrimSpace(record.URL) != "" {
			event.Metadata = ensureMap(event.Metadata)
			event.Metadata["url"] = record.URL
		} else if url, ok := event.Metadata["url"].(string); ok && strings.TrimSpace(url) != "" {
			event.Metadata = ensureMap(event.Metadata)
			event.Metadata["url"] = url
		}
		// Note: We no longer skip items without URLs - they can still be displayed

		// Only include actual news content
		if event.Source == "news:fsm" || event.Source == "wikipedia" {
			newsEvents = append(newsEvents, event)
		}
	}

	// Dedupe by canonical URL (keep newest timestamp), but also include items without URLs
	byURL := make(map[string]NewsEvent)
	withoutURL := make([]NewsEvent, 0)
	for _, ev := range newsEvents {
		u, _ := ev.Metadata["url"].(string)
		key := strings.TrimSpace(strings.ToLower(u))
		if key == "" {
			// Items without URLs are kept separately
			withoutURL = append(withoutURL, ev)
			continue
		}
		prev, ok := byURL[key]
		if !ok || ev.Timestamp > prev.Timestamp {
			byURL[key] = ev
		}
	}
	newsEvents = newsEvents[:0]
	// Add deduplicated items with URLs
	for _, ev := range byURL {
		newsEvents = append(newsEvents, ev)
	}
	// Add items without URLs (no deduplication needed)
	newsEvents = append(newsEvents, withoutURL...)

	// Sort by timestamp (newest first)
	sort.Slice(newsEvents, func(i, j int) bool {
		return newsEvents[i].Timestamp > newsEvents[j].Timestamp
	})

	// Filter to recent window (last 72h) when parseable
	filtered := make([]NewsEvent, 0, len(newsEvents))
	cutoff := time.Now().Add(-72 * time.Hour)
	for _, ev := range newsEvents {
		if ts, err := time.Parse(time.RFC3339, ev.Timestamp); err == nil {
			if ts.After(cutoff) {
				filtered = append(filtered, ev)
			}
		} else {
			filtered = append(filtered, ev)
		}
	}

	// Limit results
	if len(filtered) > 20 {
		filtered = filtered[:20]
	}

	c.JSON(http.StatusOK, gin.H{
		"events": filtered,
		"count":  len(filtered),
	})
}

// Helper functions for extracting data from payload
func getStringFromPayload(payload map[string]interface{}, key string) string {
	if val, ok := payload[key].(string); ok {
		return val
	}
	return ""
}

func getStringFromMetadata(payload map[string]interface{}, key string) string {
	if metadata, ok := payload["metadata"].(map[string]interface{}); ok {
		if val, ok := metadata[key].(string); ok {
			return val
		}
	}
	return ""
}

func getMapFromPayload(payload map[string]interface{}, key string) map[string]interface{} {
	if val, ok := payload[key].(map[string]interface{}); ok {
		return val
	}
	return make(map[string]interface{})
}

func getStringSliceFromPayload(payload map[string]interface{}, key string) []string {
	if val, ok := payload[key].([]interface{}); ok {
		result := make([]string, len(val))
		for i, v := range val {
			if str, ok := v.(string); ok {
				result[i] = str
			}
		}
		return result
	}
	return []string{}
}

// ensureMap returns a non-nil map for safe writes
func ensureMap(m map[string]interface{}) map[string]interface{} {
	if m == nil {
		return map[string]interface{}{}
	}
	return m
}

// WikipediaEvent represents a Wikipedia event
type WikipediaEvent struct {
	ID        string                 `json:"id"`
	Text      string                 `json:"text"`
	Source    string                 `json:"source"`
	Type      string                 `json:"type"`
	Timestamp string                 `json:"timestamp"`
	Metadata  map[string]interface{} `json:"metadata"`
	Tags      []string               `json:"tags"`
}

// getWikipediaEvents fetches recent Wikipedia events from Weaviate
func (m *MonitorService) getWikipediaEvents(c *gin.Context) {
	_ = c.DefaultQuery("limit", "20") // Future use for pagination

	// Query AgiWiki class for wikipedia-sourced items. The wiki bootstrapper
	// indexes articles into this class when using Weaviate as the backing
	// vector DB.
	query := map[string]interface{}{
		"query": `
        {
            Get {
                AgiWiki(limit: 50, where: { path: ["source"], operator: Equal, valueString: "wikipedia" }) {
                    _additional { id }
                    title
                    text
                    source
                    url
                    timestamp
                }
            }
        }`,
	}

	queryBytes, _ := json.Marshal(query)
	url := m.weaviateURL + "/v1/graphql"
	resp, err := http.Post(url, "application/json", bytes.NewReader(queryBytes))
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to fetch Wikipedia events"})
		return
	}
	defer resp.Body.Close()

	var weaviateResp struct {
		Data struct {
			Get struct {
				AgiWiki []struct {
					Additional struct {
						ID string `json:"id"`
					} `json:"_additional"`
					Title     string `json:"title"`
					Text      string `json:"text"`
					Source    string `json:"source"`
					URL       string `json:"url"`
					Timestamp string `json:"timestamp"`
				} `json:"AgiWiki"`
			} `json:"Get"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&weaviateResp); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to decode Wikipedia events"})
		return
	}

	if len(weaviateResp.Errors) > 0 {
		c.JSON(http.StatusBadGateway, gin.H{"error": "Weaviate query error: " + weaviateResp.Errors[0].Message})
		return
	}

	// Convert to WikipediaEvent format with filtering for interesting items from both classes
	var wikipediaEvents []WikipediaEvent

	appendIfInteresting := func(article struct {
		Additional struct {
			ID string `json:"id"`
		} `json:"_additional"`
		Title     string `json:"title"`
		Text      string `json:"text"`
		Source    string `json:"source"`
		URL       string `json:"url"`
		Timestamp string `json:"timestamp"`
	}) {
		text := strings.TrimSpace(article.Text)
		if len(text) < 20 {
			return
		}
		lower := strings.ToLower(text)
		if strings.Contains(lower, "timer_tick") ||
			strings.Contains(lower, "server busy") ||
			strings.Contains(lower, "execution_plan") ||
			strings.Contains(lower, "interpretation") ||
			strings.Contains(lower, "outcome\":\"") {
			return
		}
		// Require title for interesting items, but URL is optional
		if strings.TrimSpace(article.Title) == "" {
			return
		}
		// Note: URL is optional - Wikipedia items can be displayed without URLs

		event := WikipediaEvent{
			ID:        article.Additional.ID,
			Text:      text,
			Source:    "wikipedia",
			Type:      "article",
			Timestamp: article.Timestamp,
			Metadata: map[string]interface{}{
				"title": article.Title,
				"url":   article.URL,
			},
			Tags: []string{"wikipedia", "article"},
		}

		wikipediaEvents = append(wikipediaEvents, event)
	}

	for _, article := range weaviateResp.Data.Get.AgiWiki {
		appendIfInteresting(article)
	}

	// Dedupe by ID
	seen := make(map[string]bool)
	deduped := make([]WikipediaEvent, 0, len(wikipediaEvents))
	for _, ev := range wikipediaEvents {
		if seen[ev.ID] {
			continue
		}
		seen[ev.ID] = true
		deduped = append(deduped, ev)
	}
	wikipediaEvents = deduped

	// Sort by timestamp (newest first) and limit results
	sort.Slice(wikipediaEvents, func(i, j int) bool {
		return wikipediaEvents[i].Timestamp > wikipediaEvents[j].Timestamp
	})

	if len(wikipediaEvents) > 20 {
		wikipediaEvents = wikipediaEvents[:20]
	}

	c.JSON(http.StatusOK, gin.H{
		"events": wikipediaEvents,
		"count":  len(wikipediaEvents),
	})
}
