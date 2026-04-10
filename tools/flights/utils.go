package main

import (
	"fmt"
	"os"
	"strings"
	"time"
)

func getOllamaURL() string {
	url := os.Getenv("OLLAMA_URL")
	if url == "" {
		url = "http://localhost:11434"
	}
	if !strings.HasSuffix(url, "/") {
		// No trailing slash needed as we usually append /api/generate
	}
	return url
}

func getScreenshotPath() string {
	path := os.Getenv("SCREENSHOT_PATH")
	if path == "" {
		return fmt.Sprintf("artifacts/flight_results_%d.png", time.Now().Unix())
	}
	return path
}
