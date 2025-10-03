package memory

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// EpisodicRecord represents one episode or chunk to index in RAG.
type EpisodicRecord struct {
	ID        string                 `json:"id,omitempty"`
	SessionID string                 `json:"session_id"`
	PlanID    string                 `json:"plan_id,omitempty"`
	Timestamp time.Time              `json:"timestamp"`
	Outcome   string                 `json:"outcome,omitempty"`
	Reward    float64                `json:"reward,omitempty"`
	Tags      []string               `json:"tags,omitempty"`
	StepIndex int                    `json:"step_index,omitempty"`
	Text      string                 `json:"text"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// EpisodicClient is a thin HTTP client to a future CrewAI RAG adapter.
// It assumes endpoints like /api/episodes (POST) and /api/search (POST).
type EpisodicClient struct {
	BaseURL    string
	HTTPClient *http.Client
}

func NewEpisodicClient(baseURL string) *EpisodicClient {
	return &EpisodicClient{
		BaseURL:    baseURL,
		HTTPClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// IndexEpisode sends an episode record to the RAG service for storage and embedding.
func (c *EpisodicClient) IndexEpisode(rec *EpisodicRecord) error {
	url := fmt.Sprintf("%s/api/episodes", c.BaseURL)
	b, _ := json.Marshal(rec)
	req, _ := http.NewRequest("POST", url, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("index episode failed: %s", resp.Status)
	}
	return nil
}

// SearchEpisodes queries the RAG service using text and optional filters.
func (c *EpisodicClient) SearchEpisodes(query string, limit int, filters map[string]interface{}) ([]EpisodicRecord, error) {
	url := fmt.Sprintf("%s/api/search", c.BaseURL)
	payload := map[string]interface{}{
		"query":   query,
		"limit":   limit,
		"filters": filters,
	}
	b, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", url, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("search failed: %s", resp.Status)
	}
	var results []EpisodicRecord
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		return nil, err
	}
	return results, nil
}
