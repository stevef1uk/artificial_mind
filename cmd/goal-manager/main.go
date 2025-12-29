package main

import (
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/gorilla/mux"
	"github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"

	selfpkg "agi/self"
)

func main() {
	var (
		agentID  = flag.String("agent", "agent_1", "Agent ID")
		natsURL  = flag.String("nats", nats.DefaultURL, "NATS URL")
		redisURL = flag.String("redis", "redis://localhost:6379", "Redis URL")
		httpAddr = flag.String("http", ":8090", "HTTP listen addr")
		debug    = flag.Bool("debug", false, "Enable debug logging")
	)
	flag.Parse()

	// Override with environment variables
	if envAgentID := os.Getenv("GOAL_MANAGER_AGENT_ID"); envAgentID != "" {
		*agentID = envAgentID
	}
	if envNatsURL := os.Getenv("NATS_URL"); envNatsURL != "" {
		*natsURL = envNatsURL
	}
	if envRedisURL := os.Getenv("REDIS_URL"); envRedisURL != "" {
		*redisURL = envRedisURL
	}
	if envHTTPAddr := os.Getenv("GOAL_MANAGER_HTTP_ADDR"); envHTTPAddr != "" {
		*httpAddr = envHTTPAddr
	}

	log.Printf("Connecting to NATS at: %s", *natsURL)
	nc, err := nats.Connect(*natsURL)
	if err != nil {
		log.Fatalf("failed to connect to NATS: %v", err)
	}
	defer nc.Close()

	opt, err := redis.ParseURL(*redisURL)
	if err != nil {
		log.Fatalf("failed to parse Redis URL: %v", err)
	}
	rdb := redis.NewClient(opt)
	defer rdb.Close()

	gm := selfpkg.NewGoalManager(nc, rdb, *agentID)
	if *debug {
		log.Printf("🐛 DEBUG: Goal Manager created for agent %s", *agentID)
	}
	if err := gm.Start(); err != nil {
		log.Fatalf("failed to start goal manager: %v", err)
	}
	log.Printf("Goal Manager started for agent %s", *agentID)
	if *debug {
		log.Printf("🐛 DEBUG: Goal Manager is now running and listening for events")
	}

	// Minimal REST API
	r := mux.NewRouter()

	// Health check endpoint
	r.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}).Methods("GET")

	r.HandleFunc("/goals/{agent}/active", func(w http.ResponseWriter, r *http.Request) {
		// ignore agent path for now, bound to gm.agentID
		goals, err := gm.ListActiveGoals()
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(goals)
	}).Methods("GET")

	r.HandleFunc("/goal/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := mux.Vars(r)["id"]
		g, err := gm.GetGoal(id)
		if err != nil {
			http.Error(w, "not found", 404)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(g)
	}).Methods("GET")

	r.HandleFunc("/goal", func(w http.ResponseWriter, r *http.Request) {
		log.Printf("📥 [GoalManager] POST /goal request received from %s", r.RemoteAddr)
		var g selfpkg.PolicyGoal
		if err := json.NewDecoder(r.Body).Decode(&g); err != nil {
			log.Printf("❌ [GoalManager] Failed to decode goal: %v", err)
			http.Error(w, "bad request", 400)
			return
		}
		// Debug: Log if context is present
		if g.Context != nil && len(g.Context) > 0 {
			log.Printf("📥 [GoalManager] Received goal with context: %+v (domain: %v, source: %v)", 
				g.Context, g.Context["domain"], g.Context["source"])
		} else {
			log.Printf("📥 [GoalManager] Received goal without context (description: %.50s...)", g.Description)
		}
		ng, err := gm.CreateGoal(g)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		// Debug: Verify context was preserved
		if ng.Context != nil && len(ng.Context) > 0 {
			log.Printf("✅ [GoalManager] Goal created with context preserved: %+v", ng.Context)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ng)
	}).Methods("POST")

	r.HandleFunc("/goal/{id}/{action}", func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		id := vars["id"]
		action := vars["action"]
		var status string
		switch action {
		case "suspend":
			status = "suspended"
		case "resume":
			status = "active"
		case "achieve":
			status = "achieved"
		case "fail":
			status = "failed"
		default:
			http.Error(w, "unknown action", 400)
			return
		}
		g, err := gm.UpdateGoalStatus(id, status)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(g)
	}).Methods("POST")

	r.HandleFunc("/goal/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := mux.Vars(r)["id"]
		if err := gm.DeleteGoal(id); err != nil {
			if strings.Contains(err.Error(), "not found") {
				http.Error(w, err.Error(), 404)
			} else {
				http.Error(w, err.Error(), 500)
			}
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": "Goal deleted",
			"id":      id,
		})
	}).Methods("DELETE")

	log.Printf("Goal Manager REST listening on %s", *httpAddr)
	if err := http.ListenAndServe(*httpAddr, r); err != nil {
		log.Fatalf("http server error: %v", err)
	}
}
