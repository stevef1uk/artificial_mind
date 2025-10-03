package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
	return &WeaviateClient{
		BaseURL:    baseURL,
		Class:      class,
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
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
	Title      string                 `json:"title"`
	Text       string                 `json:"text"`
	Source     string                 `json:"source"`
	URL        string                 `json:"url"`
	Timestamp  string                 `json:"timestamp"`
	Properties map[string]interface{} `json:"properties,omitempty"`
}

// SearchArticles implements VectorDBClient interface for Weaviate
func (c *WeaviateClient) SearchArticles(ctx context.Context, limit int, filters map[string]interface{}) ([]wikipediaArticle, error) {
	where := map[string]interface{}{
		"source": "wikipedia",
	}

	objects, err := c.SearchObjects(ctx, limit, where)
	if err != nil {
		return nil, err
	}

	var articles []wikipediaArticle
	for _, obj := range objects {
		metadata := map[string]interface{}{
			"title":  obj.Title,
			"source": obj.Source,
			"url":    obj.URL,
		}

		article := wikipediaArticle{
			ID:        obj.Additional.ID,
			Title:     obj.Title,
			Text:      obj.Text,
			Metadata:  metadata,
			Timestamp: obj.Timestamp,
		}
		articles = append(articles, article)
	}

	return articles, nil
}

// UpdateArticleSummary implements VectorDBClient interface for Weaviate
func (c *WeaviateClient) UpdateArticleSummary(ctx context.Context, articleID, summary string) error {
	properties := map[string]interface{}{
		"summary": summary,
	}

	return c.UpdateObject(ctx, articleID, properties)
}

// SearchObjects searches for objects in Weaviate
func (c *WeaviateClient) SearchObjects(ctx context.Context, limit int, where map[string]interface{}) ([]WeaviateObject, error) {
	query := map[string]interface{}{
		"query": fmt.Sprintf(`
		{
			Get {
				%s(limit: %d%s) {
					_additional { id }
					title
					text
					source
					url
					timestamp
				}
			}
		}`, c.Class, limit, c.buildWhereClause(where)),
	}

	queryBytes, _ := json.Marshal(query)
	req, err := http.NewRequestWithContext(ctx, "POST", c.BaseURL+"/v1/graphql", bytes.NewReader(queryBytes))
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
		return fmt.Errorf("weaviate update failed: %s", resp.Status)
	}

	return nil
}

// buildWhereClause builds a GraphQL where clause from filters
func (c *WeaviateClient) buildWhereClause(where map[string]interface{}) string {
	if len(where) == 0 {
		return ""
	}

	// For now, just use the first condition (source filter)
	for key, value := range where {
		return fmt.Sprintf(`, where: { path: ["%s"], operator: Equal, valueString: "%v" }`, key, value)
	}

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
