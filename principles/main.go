package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"principles/internal/actions"
	"principles/internal/api"
	"principles/internal/engine"
	"strings"
)

func main() {
	var port int
	flag.IntVar(&port, "port", 8084, "Port to listen on")
	flag.Parse()

	// Load principles from JSON file
	principles, err := engine.LoadPrinciplesFromFile("config/principles.json")
	if err != nil {
		panic(fmt.Sprintf("Failed to load principles: %v", err))
	}

	// Get Redis URL from environment variable
	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		redisURL = "localhost:6379"
	} else {
		// Parse redis:// URL to extract host:port
		if strings.HasPrefix(redisURL, "redis://") {
			redisURL = strings.TrimPrefix(redisURL, "redis://")
		}
	}

	// Setup Redis memory
	memory := engine.NewMemory(redisURL, 3600)

	eng := engine.NewEngine(principles, memory)

	http.HandleFunc("/action", api.MakeHandler(eng, actions.PerformExampleAction))
	http.HandleFunc("/check-plan", api.MakePlanCheckHandler(eng))
	fmt.Printf("API running on :%d\n", port)
	http.ListenAndServe(fmt.Sprintf(":%d", port), nil)
}
