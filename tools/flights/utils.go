package main

import (
	"os"
	"strings"
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
		// Fallback to local directory if not specified
		return "artifacts/latest_screenshot.png"
	}
	return path
}
