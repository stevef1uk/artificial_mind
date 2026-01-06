package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings" // Added missing import
	"testing"

	miniredis "github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestWikiBootstrapperPathResolution(t *testing.T) {
	// Setup test environment
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer mr.Close()

	s := NewAPIServer("domain.json", mr.Addr())
	s.redis = redis.NewClient(&redis.Options{Addr: mr.Addr()})
	s.eventBus = nil

	// Create dummy bin/tools/wiki_bootstrapper (underscore)
	cwd, _ := os.Getwd()
	binDir := filepath.Join(cwd, "bin", "tools")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("failed to create bin dir: %v", err)
	}
	defer os.RemoveAll(filepath.Join(cwd, "bin"))

	dummyBin := filepath.Join(binDir, "wiki_bootstrapper")
	// Create a dummy script that verifies it was called
	content := "#!/bin/sh\necho \"dummy binary executed\"\n"
	if err := os.WriteFile(dummyBin, []byte(content), 0755); err != nil {
		t.Fatalf("failed to create dummy binary: %v", err)
	}

	// Invoke the tool
	reqBody := map[string]interface{}{
		"seeds": "Science",
	}
	b, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/api/v1/tools/tool_wiki_bootstrapper/invoke", bytes.NewReader(b))
	rec := httptest.NewRecorder()

	s.handleInvokeTool(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d. Body: %s", rec.Code, rec.Body.String())
	}

	if !strings.Contains(rec.Body.String(), "dummy binary executed") {
		t.Errorf("expected output to contain 'dummy binary executed', got: %s", rec.Body.String())
	}
}
