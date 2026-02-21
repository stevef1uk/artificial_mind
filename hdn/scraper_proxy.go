package main

import (
	"io"
	"log"
	"net/http"
	"os"
	"strings"
)

func scraperBaseURL() string {
	if v := strings.TrimSpace(os.Getenv("SCRAPER_API_URL")); v != "" {
		return v
	}
	return "http://localhost:8085"
}

func setCORSHeaders(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
}

func handleCORSPreflight(w http.ResponseWriter, r *http.Request) bool {
	setCORSHeaders(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return true
	}
	return false
}

func (s *APIServer) proxyToScraper(w http.ResponseWriter, r *http.Request, targetPath string) {
	if handleCORSPreflight(w, r) {
		return
	}

	if r.Method != http.MethodPost && r.Method != http.MethodGet {
		setCORSHeaders(w)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	base := strings.TrimRight(scraperBaseURL(), "/")
	url := base + targetPath
	log.Printf("[HDN][SCRAPER-PROXY] %s %s", r.Method, url)

	req, err := http.NewRequest(r.Method, url, r.Body)
	if err != nil {
		setCORSHeaders(w)
		http.Error(w, "Failed to create proxy request", http.StatusBadGateway)
		return
	}
	req.Header.Set("Content-Type", r.Header.Get("Content-Type"))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("[HDN][SCRAPER-PROXY] request failed: %v", err)
		setCORSHeaders(w)
		http.Error(w, "Scraper service unavailable", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	setCORSHeaders(w)
	w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

func (s *APIServer) handleScraperMyClimateFlight(w http.ResponseWriter, r *http.Request) {
	s.proxyToScraper(w, r, "/api/myclimate/flight")
}

func (s *APIServer) handleScraperGeneric(w http.ResponseWriter, r *http.Request) {
	s.proxyToScraper(w, r, "/api/scraper/generic")
}

func (s *APIServer) handleScraperAgentDeploy(w http.ResponseWriter, r *http.Request) {
	s.proxyToScraper(w, r, "/api/scraper/agent/deploy")
}

func (s *APIServer) handleScraperHealth(w http.ResponseWriter, r *http.Request) {
	s.proxyToScraper(w, r, "/api/myclimate/health")
}
