package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
	"time"
)

type ChatRequest struct {
	Message      string                 `json:"message"`
	SessionID    string                 `json:"session_id,omitempty"`
	ShowThinking bool                   `json:"show_thinking"`
	Context      map[string]interface{} `json:"context,omitempty"`
}

type ChatResponse struct {
	Response   string      `json:"response"`
	SessionID  string      `json:"session_id"`
	Timestamp  string      `json:"timestamp"`
	Thoughts   interface{} `json:"thoughts,omitempty"`
	Confidence float64     `json:"confidence"`
	Error      string      `json:"error,omitempty"`
}

type Client struct {
	BaseURL    string
	SessionID  string
	HTTPClient *http.Client
	Model      string
}

func NewClient(addr string) *Client {
	return &Client{
		BaseURL:   addr,
		SessionID: fmt.Sprintf("session_%d", time.Now().UnixNano()),
		HTTPClient: &http.Client{
			Transport: &http.Transport{
				DisableKeepAlives: true,
			},
			Timeout: 120 * time.Second,
		},
	}
}

func (c *Client) Chat(ctx context.Context, message string) (*ChatResponse, error) {
	fmt.Printf("Invoking Chat API at: %s/api/v1/chat\n", c.BaseURL)

	// If the URL ends in /v1, we use the OpenAI-compatible endpoint
	if strings.HasSuffix(c.BaseURL, "/v1") || strings.HasSuffix(c.BaseURL, "/v1/") {
		return c.ChatOpenAI(ctx, message)
	}

	reqBody := ChatRequest{
		Message:      message,
		SessionID:    c.SessionID,
		ShowThinking: true,
	}

	var err error
	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	var resp *http.Response
	for i := 0; i < 3; i++ {
		// Stop retrying if context is canceled
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		var req *http.Request
		req, err = http.NewRequestWithContext(ctx, "POST", fmt.Sprintf("%s/api/v1/chat", c.BaseURL), bytes.NewBuffer(jsonData))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err = c.HTTPClient.Do(req)
		if err == nil {
			break
		}
		fmt.Printf("API attempt %d failed: %v\n", i+1, err)
		time.Sleep(1 * time.Second)
	}
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	fmt.Printf("AI Raw Response: %s\n", string(body))

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status: %s, body: %s", resp.Status, string(body))
	}

	var chatResp ChatResponse
	if err := json.Unmarshal(body, &chatResp); err != nil {
		return nil, err
	}

	if chatResp.SessionID != "" {
		c.SessionID = chatResp.SessionID
	}

	return &chatResp, nil
}

type VisionContent struct {
	Type     string          `json:"type"`
	Text     string          `json:"text,omitempty"`
	ImageURL *VisionImageURL `json:"image_url,omitempty"`
}

type VisionImageURL struct {
	URL string `json:"url"`
}

type VisionMessage struct {
	Role    string          `json:"role"`
	Content []VisionContent `json:"content"`
}

type VisionRequest struct {
	Model    string          `json:"model"`
	Messages []VisionMessage `json:"messages"`
}

type VisionResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

func (c *Client) DescribeImage(ctx context.Context, apiURL string, base64Image string) (string, error) {
	fmt.Printf("Invoking Vision API at: %s\n", apiURL)

	model := "qwen3-vl"
	if c.Model != "" {
		model = c.Model
	}

	reqBody := VisionRequest{
		Model: model,
		Messages: []VisionMessage{
			{
				Role: "user",
				Content: []VisionContent{
					{
						Type: "image_url",
						ImageURL: &VisionImageURL{
							URL: fmt.Sprintf("data:image/jpeg;base64,%s", base64Image),
						},
					},
					{
						Type: "text",
						Text: "Describe this image in detail",
					},
				},
			},
		},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Vision API error (%d): %s", resp.StatusCode, string(body))
	}

	var visionResp VisionResponse
	if err := json.Unmarshal(body, &visionResp); err != nil {
		return "", err
	}

	if len(visionResp.Choices) == 0 {
		return "", fmt.Errorf("no choices in vision response")
	}

	return visionResp.Choices[0].Message.Content, nil
}

func (c *Client) ChatOpenAI(ctx context.Context, message string) (*ChatResponse, error) {
	apiURL := c.BaseURL
	if !strings.HasSuffix(apiURL, "/chat/completions") {
		if strings.HasSuffix(apiURL, "/v1") || strings.HasSuffix(apiURL, "/v1/") {
			apiURL = strings.TrimSuffix(apiURL, "/") + "/chat/completions"
		}
	}
	fmt.Printf("Invoking OpenAI-style Chat API at: %s\n", apiURL)

	model := "qwen2.5-coder:7b" // Default for Ollama on .53
	if c.Model != "" {
		model = c.Model
	}

	reqBody := VisionRequest{
		Model: model,
		Messages: []VisionMessage{
			{
				Role: "user",
				Content: []VisionContent{
					{
						Type: "text",
						Text: message,
					},
				},
			},
		},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("OpenAI-style Chat API error (%d): %s", resp.StatusCode, string(body))
	}

	var visionResp VisionResponse
	if err := json.Unmarshal(body, &visionResp); err != nil {
		return nil, err
	}

	if len(visionResp.Choices) == 0 {
		return nil, fmt.Errorf("no choices in response")
	}

	return &ChatResponse{
		Response: visionResp.Choices[0].Message.Content,
	}, nil
}
