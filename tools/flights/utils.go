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
	basePath := os.Getenv("SCREENSHOT_PATH")
	if basePath == "" {
		basePath = "artifacts/latest_screenshot.png"
	}

	// If it's a specific file path, we'll use it as a directory or prefix
	// to ensure uniqueness in shared environments like K3s.
	if strings.HasSuffix(basePath, ".png") {
		dir := strings.TrimSuffix(basePath, "latest_screenshot.png")
		if dir == "" { dir = "artifacts/" }
		return fmt.Sprintf("%sscreenshot_%d.png", dir, time.Now().UnixNano())
	}
	
	return basePath
}
