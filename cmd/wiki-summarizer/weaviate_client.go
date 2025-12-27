package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

// WeaviateClient provides minimal interface for Weaviate operations
type WeaviateClient struct {
	BaseURL    string
	Class      string
	HTTPClient *http.Client
}

func NewWeaviateClient(baseURL, class string) *WeaviateClient {
	// Create transport with custom dialer to handle DNS timeouts better
	// DNS lookup timeout is separate from HTTP timeout
	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second, // DNS lookup + TCP connection timeout
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxIdleConns:        100,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
	}
	
	return &WeaviateClient{
		BaseURL:    baseURL,
		Class:      class,
		HTTPClient: &http.Client{
			Timeout:   120 * time.Second, // HTTP request timeout
			Transport: transport,
		},
	}
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

type WeaviateObject struct {
	Additional struct {
		ID string `json:"id"`
	} `json:"_additional"`
	Text      string `json:"text"`
	Timestamp string `json:"timestamp"`
	Metadata  string `json:"metadata"` // JSON string containing title, source, url, etc.
}

// SearchArticles implements VectorDBClient interface for Weaviate
func (c *WeaviateClient) SearchArticles(ctx context.Context, limit int, filters map[string]interface{}) ([]wikipediaArticle, error) {
	// Note: Weaviate doesn't support filtering on nested JSON fields directly in where clause
	// So we'll fetch all and filter in code, or use a different approach
	// Start with a reasonable multiplier, but increase if needed
	fetchLimit := limit * 3 // Increased from limit*2 to account for filtering
	maxFetchLimit := 100     // Cap at 100 to avoid huge queries
	
	if fetchLimit > maxFetchLimit {
		fetchLimit = maxFetchLimit
	}
	
	objects, err := c.SearchObjects(ctx, fetchLimit, map[string]interface{}{})
	if err != nil {
		return nil, fmt.Errorf("failed to search objects (limit=%d): %w", fetchLimit, err)
	}

	var articles []wikipediaArticle
	for _, obj := range objects {
		// Parse metadata JSON string
		var metadataMap map[string]interface{}
		if obj.Metadata != "" {
			if err := json.Unmarshal([]byte(obj.Metadata), &metadataMap); err != nil {
				// Skip objects with invalid metadata
				continue
			}
		} else {
			metadataMap = make(map[string]interface{})
		}

		// Filter for wikipedia source
		source, _ := metadataMap["source"].(string)
		if source != "wikipedia" {
			continue
		}

		// Extract title and URL from metadata
		title, _ := metadataMap["title"].(string)
		url, _ := metadataMap["url"].(string)

		// Build metadata map for article
		metadata := map[string]interface{}{
			"title":  title,
			"source": source,
			"url":    url,
		}

		article := wikipediaArticle{
			ID:        obj.Additional.ID,
			Title:     title,
			Text:      obj.Text,
			Metadata:  metadata,
			Timestamp: obj.Timestamp,
		}
		articles = append(articles, article)

		// Stop when we have enough articles
		if len(articles) >= limit {
			break
		}
	}

	return articles, nil
}

// UpdateArticleSummary implements VectorDBClient interface for Weaviate
func (c *WeaviateClient) UpdateArticleSummary(ctx context.Context, articleID, summary string) error {
	// First, fetch the current object to get its metadata
	obj, err := c.GetObject(ctx, articleID)
	if err != nil {
		return fmt.Errorf("failed to fetch object for update: %w", err)
	}

	// Parse existing metadata JSON string
	var metadataMap map[string]interface{}
	if obj.Metadata != "" {
		if err := json.Unmarshal([]byte(obj.Metadata), &metadataMap); err != nil {
			// If metadata is invalid, create a new map
			metadataMap = make(map[string]interface{})
		}
	} else {
		metadataMap = make(map[string]interface{})
	}

	// Add summary to metadata
	metadataMap["summary"] = summary

	// Serialize updated metadata back to JSON
	metadataJSON, err := json.Marshal(metadataMap)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	// Update the metadata property
	properties := map[string]interface{}{
		"metadata": string(metadataJSON),
	}

	return c.UpdateObject(ctx, articleID, properties)
}

// GetObject fetches a single object by ID from Weaviate
func (c *WeaviateClient) GetObject(ctx context.Context, objectID string) (*WeaviateObject, error) {
	// Use REST API to get object by ID
	url := fmt.Sprintf("%s/v1/objects/%s", c.BaseURL, objectID)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		// Read response body for better error messages
		bodyBytes, _ := io.ReadAll(resp.Body)
		errorMsg := string(bodyBytes)
		if errorMsg == "" {
			errorMsg = "no error details provided"
		}
		return nil, fmt.Errorf("weaviate get object failed: %s: %s", resp.Status, errorMsg)
	}

	var obj struct {
		ID         string                 `json:"id"`
		Class      string                 `json:"class"`
		Properties map[string]interface{} `json:"properties"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&obj); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Convert to WeaviateObject format
	weaviateObj := &WeaviateObject{
		Additional: struct {
			ID string `json:"id"`
		}{ID: obj.ID},
	}

	// Extract properties
	if text, ok := obj.Properties["text"].(string); ok {
		weaviateObj.Text = text
	}
	if timestamp, ok := obj.Properties["timestamp"].(string); ok {
		weaviateObj.Timestamp = timestamp
	}
	if metadata, ok := obj.Properties["metadata"].(string); ok {
		weaviateObj.Metadata = metadata
	}

	return weaviateObj, nil
}

// SearchObjects searches for objects in Weaviate
func (c *WeaviateClient) SearchObjects(ctx context.Context, limit int, where map[string]interface{}) ([]WeaviateObject, error) {
	// For AgiWiki class, we only query text, timestamp, and metadata
	// The metadata is a JSON string that contains title, source, url, etc.
	query := map[string]interface{}{
		"query": fmt.Sprintf(`
		{
			Get {
				%s(limit: %d%s) {
					_additional { id }
					text
					timestamp
					metadata
				}
			}
		}`, c.Class, limit, c.buildWhereClause(where)),
	}

	queryBytes, _ := json.Marshal(query)
	
	// Create a context with timeout that respects both the passed context and HTTP client timeout
	// Use the shorter of the two timeouts
	queryCtx := ctx
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		// If context doesn't have a deadline, create one with the HTTP client's timeout
		var cancel context.CancelFunc
		queryCtx, cancel = context.WithTimeout(ctx, c.HTTPClient.Timeout)
		defer cancel()
	}
	
	req, err := http.NewRequestWithContext(queryCtx, "POST", c.BaseURL+"/v1/graphql", bytes.NewReader(queryBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	startTime := time.Now()
	resp, err := c.HTTPClient.Do(req)
	elapsed := time.Since(startTime)
	
	if err != nil {
		// Provide more detailed error information
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("weaviate query deadline exceeded after %v: %w", elapsed, err)
		}
		if ctx.Err() == context.Canceled {
			return nil, fmt.Errorf("weaviate query canceled: %w", err)
		}
		return nil, fmt.Errorf("weaviate query failed after %v: %w", elapsed, err)
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

	classData, exists := result.Data.Get[c.Class]
	if !exists {
		return []WeaviateObject{}, nil
	}

	return classData, nil
}

// UpdateObject updates an object in Weaviate
func (c *WeaviateClient) UpdateObject(ctx context.Context, objectID string, properties map[string]interface{}) error {
	updateData := map[string]interface{}{
		"class":      c.Class,
		"properties": properties,
	}

	updateBytes, _ := json.Marshal(updateData)
	url := fmt.Sprintf("%s/v1/objects/%s", c.BaseURL, objectID)
	req, err := http.NewRequestWithContext(ctx, "PATCH", url, bytes.NewReader(updateBytes))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		// Read response body for better error messages
		bodyBytes, _ := io.ReadAll(resp.Body)
		errorMsg := string(bodyBytes)
		if errorMsg == "" {
			errorMsg = "no error details provided"
		}
		return fmt.Errorf("weaviate update failed: %s: %s", resp.Status, errorMsg)
	}

	return nil
}

// buildWhereClause builds a GraphQL where clause from filters
// Note: For AgiWiki, we can't filter on nested metadata fields directly,
// so filtering is done in SearchArticles after parsing metadata
func (c *WeaviateClient) buildWhereClause(where map[string]interface{}) string {
	// For AgiWiki class, we don't use where clause since metadata is a JSON string
	// and Weaviate doesn't support filtering on nested JSON fields in where clause
	// Filtering is done in SearchArticles after parsing the metadata
	return ""
}

// QdrantClient provides Qdrant implementation of VectorDBClient
type QdrantClient struct {
	BaseURL    string
	Collection string
	HTTPClient *http.Client
}

func NewQdrantClient(baseURL, collection string) *QdrantClient {
	return &QdrantClient{
		BaseURL:    baseURL,
		Collection: collection,
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// SearchArticles implements VectorDBClient interface for Qdrant
func (c *QdrantClient) SearchArticles(ctx context.Context, limit int, filters map[string]interface{}) ([]wikipediaArticle, error) {
	query := map[string]interface{}{
		"limit":        limit,
		"with_payload": true,
		"with_vector":  false,
		"filter": map[string]interface{}{
			"must": []map[string]interface{}{
				{
					"key": "metadata.source",
					"match": map[string]interface{}{
						"value": "wikipedia",
					},
				},
			},
		},
	}

	queryBytes, _ := json.Marshal(query)
	url := fmt.Sprintf("%s/collections/%s/points/scroll", c.BaseURL, c.Collection)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(queryBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("qdrant search failed: %s", resp.Status)
	}

	var result struct {
		Result struct {
			Points []struct {
				ID      interface{}            `json:"id"`
				Payload map[string]interface{} `json:"payload"`
			} `json:"points"`
		} `json:"result"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	var articles []wikipediaArticle
	for _, point := range result.Result.Points {
		metadata := getMapFromPayload(point.Payload, "metadata")
		article := wikipediaArticle{
			ID:        fmt.Sprintf("%v", point.ID),
			Title:     getStringFromMap(metadata, "title"),
			Text:      getStringFromPayload(point.Payload, "text"),
			Metadata:  metadata,
			Timestamp: getStringFromPayload(point.Payload, "timestamp"),
		}
		articles = append(articles, article)
	}

	return articles, nil
}

// UpdateArticleSummary implements VectorDBClient interface for Qdrant
func (c *QdrantClient) UpdateArticleSummary(ctx context.Context, articleID, summary string) error {
	// For Qdrant, we need to update the point with the new summary
	updateData := map[string]interface{}{
		"points": []map[string]interface{}{
			{
				"id": articleID,
				"payload": map[string]interface{}{
					"summary": summary,
				},
			},
		},
	}

	updateBytes, _ := json.Marshal(updateData)
	url := fmt.Sprintf("%s/collections/%s/points", c.BaseURL, c.Collection)
	req, err := http.NewRequestWithContext(ctx, "PUT", url, bytes.NewReader(updateBytes))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("qdrant update failed: %s", resp.Status)
	}

	return nil
}
