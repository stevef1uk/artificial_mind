package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestExtractBasic(t *testing.T) {
	items := extractBasic("<html><head><title>T</title></head><body><h1>H</h1><p>P</p><a href='/x'>L</a></body></html>")
	if len(items) == 0 {
		t.Fatal("expected items")
	}
}

func TestFetchItems_LocalServer(t *testing.T) {
	// Local test server to avoid network flakiness and robots
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<html><head><title>WWII</title></head><body><h1>World War II</h1><p>Global conflict.</p></body></html>"))
	}))
	defer srv.Close()

	items, err := fetchItems(srv.URL)
	if err != nil {
		t.Fatalf("fetchItems error: %v", err)
	}
	if len(items) == 0 {
		t.Fatalf("expected at least one item, got 0")
	}
}
