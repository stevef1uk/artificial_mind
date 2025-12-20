package memory

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

// getStringFromMap safely extracts a string value from a map
func getStringFromVectorMap(props map[string]any, key string) string {
	if val, exists := props[key]; exists && val != nil {
		if str, ok := val.(string); ok {
			return str
		}
	}
	return ""
}

// VectorDBAdapter provides a unified interface for Weaviate
type VectorDBAdapter interface {
	// Collection management
	EnsureCollection(dim int) error

	// Episodic memory operations
	IndexEpisode(rec *EpisodicRecord, vec []float32) error
	SearchEpisodes(queryVec []float32, limit int, filters map[string]any) ([]EpisodicRecord, error)

	// Generic search operations
	SearchArticles(ctx context.Context, limit int, filters map[string]interface{}) ([]Article, error)
	UpdateArticleSummary(ctx context.Context, articleID, summary string) error

	// Health check
	HealthCheck() error
}

// Article represents a generic article structure
type Article struct {
	ID        string                 `json:"id"`
	Title     string                 `json:"title"`
	Text      string                 `json:"text"`
	Summary   string                 `json:"summary,omitempty"`
	Metadata  map[string]interface{} `json:"metadata"`
	Timestamp string                 `json:"timestamp"`
}

// NewVectorDBAdapter creates a Weaviate vector database client
func NewVectorDBAdapter(baseURL, collection string) VectorDBAdapter {
	return NewWeaviateAdapter(baseURL, collection)
}

// WeaviateAdapter wraps Weaviate operations
type WeaviateAdapter struct {
	baseURL    string
	collection string
	httpClient *http.Client
}

func NewWeaviateAdapter(baseURL, collection string) *WeaviateAdapter {
	return &WeaviateAdapter{
		baseURL:    baseURL,
		collection: collection,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// WeaviateObject represents a Weaviate object
type WeaviateObject struct {
	Additional struct {
		ID       string  `json:"id"`
		Distance float64 `json:"distance"`
	} `json:"_additional"`
	Title     string `json:"title,omitempty"`
	Text      string `json:"text"`
	Source    string `json:"source,omitempty"`
	Timestamp string `json:"timestamp"`
	Metadata  string `json:"metadata"`
}

// WeaviateResponse represents a Weaviate API response
type WeaviateResponse struct {
	Data struct {
		Get map[string][]WeaviateObject `json:"Get"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

// Implement VectorDBAdapter interface for Weaviate
func (w *WeaviateAdapter) EnsureCollection(dim int) error {
	// Weaviate collections are created automatically when first object is added
	// For now, just return success
	return nil
}

func (w *WeaviateAdapter) IndexEpisode(rec *EpisodicRecord, vec []float32) error {
	// Use a sanitized class name derived from the configured collection
	className := sanitizeWeaviateClass(w.collection)

	// Extract properties based on the record type
	properties := map[string]interface{}{
		"text":      rec.Text,
		"timestamp": rec.Timestamp.Format(time.RFC3339),
	}

	// Serialize metadata map to a JSON string to avoid schema type mismatches
	// Note: Weaviate auto-schema for AgiEpisodes only has text, timestamp, and metadata fields
	// So we store everything in the metadata JSON string, not as separate properties
	if rec.Metadata != nil {
		if b, err := json.Marshal(rec.Metadata); err == nil {
			properties["metadata"] = string(b)
		}
	}

	// Note: We don't set source, title, url as separate properties because AgiEpisodes
	// class doesn't have those fields in its schema. All information is in the metadata JSON string.

	// Create object in Weaviate
	createData := map[string]interface{}{
		"class":      className,
		"properties": properties,
		"vector":     vec,
	}

	jsonData, _ := json.Marshal(createData)
	url := fmt.Sprintf("%s/v1/objects", w.baseURL)

	// Debug logging
	propKeys := make([]string, 0, len(properties))
	for k := range properties {
		propKeys = append(propKeys, k)
	}
	log.Printf("[Weaviate] IndexEpisode baseURL=%s collection=%s class=%s props=%v vec_dim=%d", w.baseURL, w.collection, className, strings.Join(propKeys, ","), len(vec))

	req, err := http.NewRequest("POST", url, bytes.NewReader(jsonData))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := w.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		// include response body for diagnostics
		var bodyBytes []byte
		if resp.Body != nil {
			bodyBytes, _ = io.ReadAll(resp.Body)
		}
		return fmt.Errorf("weaviate create object failed: %s: %s", resp.Status, strings.TrimSpace(string(bodyBytes)))
	}

	return nil
}

func (w *WeaviateAdapter) SearchEpisodes(queryVec []float32, limit int, filters map[string]any) ([]EpisodicRecord, error) {
	className := sanitizeWeaviateClass(w.collection)
	// Build GraphQL query for Weaviate
	whereClause := w.buildWhereClause(filters)
	query := fmt.Sprintf(`
	{
		Get {
            %s(limit: %d%s) {
				_additional {
					id
					distance
				}
				text
				timestamp
				metadata
			}
		}
    }`, className, limit, whereClause)

	queryData := map[string]interface{}{
		"query": query,
	}

	jsonData, _ := json.Marshal(queryData)
	url := fmt.Sprintf("%s/v1/graphql", w.baseURL)
	req, err := http.NewRequest("POST", url, bytes.NewReader(jsonData))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := w.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("weaviate search failed: %s", resp.Status)
	}

	var result WeaviateResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	if len(result.Errors) > 0 {
		return nil, fmt.Errorf("weaviate errors: %v", result.Errors)
	}

	// Convert Weaviate objects to EpisodicRecord
	var episodes []EpisodicRecord
	classData, exists := result.Data.Get[className]
	if !exists {
		return episodes, nil
	}

	for _, obj := range classData {
		// Parse timestamp
		timestampStr := obj.Timestamp
		timestamp, err := time.Parse(time.RFC3339, timestampStr)
		if err != nil {
			timestamp = time.Now() // Fallback to current time
		}

		// Parse metadata JSON string to map
		metadataMap := make(map[string]interface{})
		if obj.Metadata != "" {
			json.Unmarshal([]byte(obj.Metadata), &metadataMap)
		}

		episode := EpisodicRecord{
			ID:        obj.Additional.ID,
			Text:      obj.Text,
			Timestamp: timestamp,
			Metadata:  metadataMap,
		}
		episodes = append(episodes, episode)
	}

	return episodes, nil
}

func (w *WeaviateAdapter) SearchArticles(ctx context.Context, limit int, filters map[string]interface{}) ([]Article, error) {
	className := sanitizeWeaviateClass(w.collection)
	// Similar to SearchEpisodes but returns Article format
	whereClause := w.buildWhereClause(filters)
	query := fmt.Sprintf(`
	{
		Get {
            %s(limit: %d%s) {
				_additional {
					id
				}
				title
				text
				source
				timestamp
				metadata
			}
		}
    }`, className, limit, whereClause)

	queryData := map[string]interface{}{
		"query": query,
	}

	jsonData, _ := json.Marshal(queryData)
	url := fmt.Sprintf("%s/v1/graphql", w.baseURL)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonData))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := w.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("weaviate search failed: %s", resp.Status)
	}

	var result WeaviateResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	if len(result.Errors) > 0 {
		return nil, fmt.Errorf("weaviate errors: %v", result.Errors)
	}

	// Convert Weaviate objects to Article
	var articles []Article
	classData, exists := result.Data.Get[className]
	if !exists {
		return articles, nil
	}

	for _, obj := range classData {
		// Parse metadata JSON string to map
		metadataMap := make(map[string]interface{})
		if obj.Metadata != "" {
			json.Unmarshal([]byte(obj.Metadata), &metadataMap)
		}

		article := Article{
			ID:        obj.Additional.ID,
			Title:     obj.Title,
			Text:      obj.Text,
			Summary:   "", // Summary not available in WikipediaArticle
			Timestamp: obj.Timestamp,
			Metadata:  metadataMap,
		}
		articles = append(articles, article)
	}

	return articles, nil
}

func (w *WeaviateAdapter) UpdateArticleSummary(ctx context.Context, articleID, summary string) error {
	// Update object in Weaviate
	updateData := map[string]interface{}{
		"properties": map[string]interface{}{
			"summary": summary,
		},
	}

	jsonData, _ := json.Marshal(updateData)
	url := fmt.Sprintf("%s/v1/objects/%s", w.baseURL, articleID)
	req, err := http.NewRequestWithContext(ctx, "PATCH", url, bytes.NewReader(jsonData))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := w.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("weaviate update failed: %s", resp.Status)
	}

	return nil
}

func (w *WeaviateAdapter) HealthCheck() error {
	// Check Weaviate health
	url := fmt.Sprintf("%s/v1/meta", w.baseURL)
	resp, err := w.httpClient.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("weaviate health check failed: %s", resp.Status)
	}

	return nil
}

// Helper functions for Weaviate
func (w *WeaviateAdapter) buildWhereClause(filters map[string]any) string {
	if len(filters) == 0 {
		return ""
	}

	conditions := []string{}
	for key, value := range filters {
		conditions = append(conditions, fmt.Sprintf(`%s: {equal: "%v"}`, key, value))
	}

	return fmt.Sprintf(", where: {%s}", strings.Join(conditions, ", "))
}

func getMapFromVectorProperties(m map[string]interface{}, key string) map[string]interface{} {
	if val, exists := m[key]; exists {
		if mapVal, ok := val.(map[string]interface{}); ok {
			return mapVal
		}
	}
	return make(map[string]interface{})
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// sanitizeWeaviateClass converts a collection name into a valid Weaviate class name:
// - must start with an uppercase letter
// - remove/replace invalid characters
// - collapse dashes/underscores and capitalize words
func sanitizeWeaviateClass(name string) string {
	if name == "" {
		return "DefaultClass"
	}
	// Split on non-alphanumeric to words
	parts := []string{}
	cur := ""
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			cur += string(r)
		} else {
			if cur != "" {
				parts = append(parts, cur)
				cur = ""
			}
		}
	}
	if cur != "" {
		parts = append(parts, cur)
	}
	if len(parts) == 0 {
		return "DefaultClass"
	}
	// Capitalize and join
	b := strings.Builder{}
	for _, p := range parts {
		if len(p) == 0 {
			continue
		}
		// Special case: if the part contains "episodes", preserve the capital E
		if strings.Contains(strings.ToLower(p), "episodes") {
			// Replace "episodes" with "Episodes" while preserving the rest
			result := strings.ReplaceAll(strings.ToLower(p), "episodes", "Episodes")
			// Capitalize the first letter
			if len(result) > 0 {
				result = strings.ToUpper(result[:1]) + result[1:]
			}
			b.WriteString(result)
		} else {
			up := strings.ToUpper(p[:1]) + strings.ToLower(p[1:])
			b.WriteString(up)
		}
	}
	out := b.String()
	// Ensure starts with letter
	if out == "" || !(out[0] >= 'A' && out[0] <= 'Z') {
		out = "C" + out
	}
	return out
}
