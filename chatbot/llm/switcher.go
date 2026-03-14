package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type SwitchRequest struct {
	Model string `json:"model"`
}

func SwitchModel(ctx context.Context, switcherURL string, model string) error {
	fmt.Printf("[Switcher] Switching model to: %s via %s\n", model, switcherURL)

	reqBody := SwitchRequest{
		Model: model,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}

	client := &http.Client{Timeout: 120 * time.Second}
	req, err := http.NewRequestWithContext(ctx, "POST", switcherURL+"/switch", bytes.NewBuffer(jsonData))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("switcher returned status: %d", resp.StatusCode)
	}

	return nil
}

func WaitForHealth(ctx context.Context, apiURL string) error {
	// Extract base URL for health check
	healthURL := apiURL
	if strings.Contains(apiURL, "/v1") {
		parts := strings.Split(apiURL, "/v1")
		healthURL = parts[0] + "/health"
	} else if !strings.HasSuffix(apiURL, "/health") {
		healthURL = strings.TrimSuffix(apiURL, "/") + "/health"
	}

	fmt.Printf("[Switcher] Waiting for health at: %s\n", healthURL)
	client := &http.Client{Timeout: 5 * time.Second}

	// Check immediately once
	req, _ := http.NewRequestWithContext(ctx, "GET", healthURL, nil)
	resp, err := client.Do(req)
	if err == nil && resp.StatusCode == http.StatusOK {
		resp.Body.Close()
		fmt.Printf("[Switcher] Backend is healthy\n")
		return nil
	}
	if resp != nil {
		resp.Body.Close()
	}

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			req, err := http.NewRequestWithContext(ctx, "GET", healthURL, nil)
			if err != nil {
				continue
			}
			resp, err := client.Do(req)
			if err == nil {
				if resp.StatusCode == http.StatusOK {
					resp.Body.Close()
					fmt.Printf("[Switcher] Backend is healthy\n")
					return nil
				}
				resp.Body.Close()
			}
		}
	}
}
