package main

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
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
	URL       string                 `json:"url,omitempty"`
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
					Metadata  string `json:"metadata"`
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
		// If the class doesn't exist, return empty results instead of an error
		// This can happen if Weaviate schema hasn't been initialized yet
		errorMsg := weaviateResp.Errors[0].Message
		if strings.Contains(errorMsg, "Cannot query field") || strings.Contains(errorMsg, "does not exist") {
			log.Printf("⚠️ Weaviate class WikipediaArticle does not exist yet, returning empty news events")
			c.JSON(http.StatusOK, gin.H{
				"events": []NewsEvent{},
				"count":  0,
			})
			return
		}
		c.JSON(http.StatusBadGateway, gin.H{"error": "Weaviate query error: " + errorMsg})
		return
	}

	// Convert to NewsEvent format
	log.Printf("📊 [getNewsEvents] Weaviate returned %d raw WikipediaArticle records with source=news:fsm", len(weaviateResp.Data.Get.WikipediaArticle))
	var newsEvents []NewsEvent
	skippedCount := 0
	skipReasons := map[string]int{
		"text_too_short":     0,
		"text_contains_noise": 0,
		"tool_created":       0,
		"conversation":        0,
		"empty_title":         0,
	}
	for _, record := range weaviateResp.Data.Get.WikipediaArticle {
		// Skip boring/system content
		if len(strings.TrimSpace(record.Text)) < 20 {
			skipReasons["text_too_short"]++
			skippedCount++
			continue
		}
		lowerText := strings.ToLower(record.Text)
		if strings.Contains(lowerText, "timer_tick") ||
			strings.Contains(lowerText, "outcome\":\"") ||
			strings.Contains(lowerText, "server busy") ||
			strings.Contains(lowerText, "execution_plan") ||
			strings.Contains(lowerText, "interpretation") {
			skipReasons["text_contains_noise"]++
			skippedCount++
			continue
		}
		// Skip tool creation events
		if strings.Contains(record.Title, "News Event: agi.tool.created") {
			skipReasons["tool_created"]++
			skippedCount++
			continue
		}
		// Skip user messages and other conversational events
		if strings.Contains(record.Title, "News Event: user_message") ||
			strings.Contains(record.Title, "News Event: assistant_message") ||
			strings.Contains(record.Title, "News Event: conversation") {
			skipReasons["conversation"]++
			skippedCount++
			continue
		}
		// Require basic ingestion signals
		if strings.TrimSpace(record.Title) == "" {
			skipReasons["empty_title"]++
			skippedCount++
			continue
		}

		// Parse metadata JSON string if present to extract URL
		var metadataMap map[string]interface{}
		if record.Metadata != "" {
			if err := json.Unmarshal([]byte(record.Metadata), &metadataMap); err == nil {
				// Try to extract URL from original_metadata if present
				if origMeta, ok := metadataMap["original_metadata"].(map[string]interface{}); ok {
					if url, ok := origMeta["url"].(string); ok && strings.TrimSpace(url) != "" {
						record.URL = url
					}
				}
				// Also check top-level metadata for URL
				if url, ok := metadataMap["url"].(string); ok && strings.TrimSpace(url) != "" && record.URL == "" {
					record.URL = url
				}
			}
		}

		event := NewsEvent{
			ID:        record.Additional.ID,
			Title:     record.Title,
			Text:      record.Text,
			Source:    record.Source,
			Type:      "news",
			Timestamp: record.Timestamp,
			Metadata:  metadataMap,
			Tags:      []string{"news"},
		}

		// Add URL if present, but don't require it (some news items may not have URLs)
		if strings.TrimSpace(record.URL) != "" {
			event.URL = record.URL
			if event.Metadata == nil {
				event.Metadata = make(map[string]interface{})
			}
			event.Metadata["url"] = record.URL
		}
		// Note: We no longer skip items without URLs - they can still be displayed

		// Only include actual news content
		if event.Source == "news:fsm" || event.Source == "wikipedia" {
			newsEvents = append(newsEvents, event)
		} else {
			skippedCount++
		}
	}
	log.Printf("📊 [getNewsEvents] After initial filtering: %d passed, %d skipped", len(newsEvents), skippedCount)
	log.Printf("📊 [getNewsEvents] Skip reasons: text_too_short=%d, text_contains_noise=%d, tool_created=%d, conversation=%d, empty_title=%d",
		skipReasons["text_too_short"], skipReasons["text_contains_noise"], skipReasons["tool_created"],
		skipReasons["conversation"], skipReasons["empty_title"])

	// Dedupe by canonical URL (keep newest timestamp), but also include items without URLs
	// Log counts for debugging
	log.Printf("📊 [getNewsEvents] After filtering: %d news events", len(newsEvents))
	
	byURL := make(map[string]NewsEvent)
	withoutURL := make([]NewsEvent, 0)
	for _, ev := range newsEvents {
		// Check both Metadata["url"] and ev.URL for URL
		u, _ := ev.Metadata["url"].(string)
		if u == "" {
			u = ev.URL
		}
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
	log.Printf("📊 [getNewsEvents] After deduplication: %d with URLs, %d without URLs", len(byURL), len(withoutURL))
	
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

	// Filter to recent window (last 30 days) when parseable
	filtered := make([]NewsEvent, 0, len(newsEvents))
	cutoff := time.Now().Add(-30 * 24 * time.Hour)
	parsedCount := 0
	withinWindowCount := 0
	unparseableCount := 0
	for _, ev := range newsEvents {
		if ts, err := time.Parse(time.RFC3339, ev.Timestamp); err == nil {
			parsedCount++
			if ts.After(cutoff) {
				filtered = append(filtered, ev)
				withinWindowCount++
			}
		} else {
			unparseableCount++
			filtered = append(filtered, ev)
		}
	}
	log.Printf("📊 [getNewsEvents] Time filter: %d parsed, %d within 30 days, %d unparseable (included)", parsedCount, withinWindowCount, unparseableCount)

	// Limit results
	if len(filtered) > 20 {
		filtered = filtered[:20]
	}
	log.Printf("📊 [getNewsEvents] Final count: %d events (limited from %d)", len(filtered), len(newsEvents))

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
	Title     string                 `json:"title,omitempty"`
	Text      string                 `json:"text"`
	Source    string                 `json:"source"`
	Type      string                 `json:"type"`
	Timestamp string                 `json:"timestamp"`
	URL       string                 `json:"url,omitempty"`
	Metadata  map[string]interface{} `json:"metadata"`
	Tags      []string               `json:"tags"`
}

// getWikipediaEvents fetches recent Wikipedia events from Weaviate
func (m *MonitorService) getWikipediaEvents(c *gin.Context) {
	_ = c.DefaultQuery("limit", "20") // Future use for pagination

	// Query AgiWiki class for wikipedia-sourced items. The wiki bootstrapper
	// indexes articles into this class when using Weaviate as the backing
	// vector DB. The collection name "agi-wiki" gets sanitized to "AgiWiki".
	// Note: Weaviate stores metadata as a JSON string, so we query text, timestamp, and metadata,
	// then parse metadata to extract source, title, and url.
	query := map[string]interface{}{
		"query": `
        {
            Get {
                AgiWiki(limit: 200) {
                    _additional { id }
                    text
                    timestamp
                    metadata
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
					Text      string `json:"text"`
					Timestamp string `json:"timestamp"`
					Metadata  string `json:"metadata"`
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
		// If the class doesn't exist, return empty results instead of an error
		errorMsg := weaviateResp.Errors[0].Message
		if strings.Contains(errorMsg, "Cannot query field") || strings.Contains(errorMsg, "does not exist") {
			log.Printf("⚠️ Weaviate class AgiWiki does not exist yet, returning empty Wikipedia events")
			c.JSON(http.StatusOK, gin.H{
				"events": []WikipediaEvent{},
				"count":  0,
			})
			return
		}
		c.JSON(http.StatusBadGateway, gin.H{"error": "Weaviate query error: " + errorMsg})
		return
	}

	// Convert to WikipediaEvent format with filtering for interesting items
	var wikipediaEvents []WikipediaEvent

	for _, item := range weaviateResp.Data.Get.AgiWiki {
		// Parse metadata JSON string
		var metadataMap map[string]interface{}
		if item.Metadata != "" {
			if err := json.Unmarshal([]byte(item.Metadata), &metadataMap); err != nil {
				continue
			}
		} else {
			metadataMap = make(map[string]interface{})
		}

		// Only process items with source="wikipedia" in metadata
		source, _ := metadataMap["source"].(string)
		if source != "wikipedia" {
			continue
		}

		// Extract title and URL from metadata
		title, _ := metadataMap["title"].(string)
		url, _ := metadataMap["url"].(string)

		// Use timestamp from metadata if available, otherwise use the timestamp field
		timestamp := item.Timestamp
		if ts, ok := metadataMap["timestamp"].(string); ok && ts != "" {
			timestamp = ts
		}

		text := strings.TrimSpace(item.Text)
		if len(text) < 20 {
			continue
		}
		lower := strings.ToLower(text)
		if strings.Contains(lower, "timer_tick") ||
			strings.Contains(lower, "server busy") ||
			strings.Contains(lower, "execution_plan") ||
			strings.Contains(lower, "interpretation") ||
			strings.Contains(lower, "outcome\":\"") {
			continue
		}
		// Require title for interesting items
		if strings.TrimSpace(title) == "" {
			continue
		}

		event := WikipediaEvent{
			ID:        item.Additional.ID,
			Title:     title,
			Text:      text,
			Source:    "wikipedia",
			Type:      "article",
			Timestamp: timestamp,
			URL:       url,
			Metadata:  metadataMap,
			Tags:      []string{"wikipedia", "article"},
		}

		wikipediaEvents = append(wikipediaEvents, event)
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
