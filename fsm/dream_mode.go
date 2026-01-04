package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"time"

	"github.com/redis/go-redis/v9"
)

// DreamMode generates creative exploration goals by randomly connecting concepts
type DreamMode struct {
	fsm        *FSMEngine
	redis      *redis.Client
	hdnURL     string
	goalMgrURL string
	ctx        context.Context
	enabled    bool
}

// NewDreamMode creates a new dream mode instance
func NewDreamMode(fsm *FSMEngine, redis *redis.Client, hdnURL string) *DreamMode {
	return &DreamMode{
		fsm:        fsm,
		redis:      redis,
		hdnURL:     hdnURL,
		goalMgrURL: "http://goal-manager:8090",
		ctx:        context.Background(),
		enabled:    true,
	}
}

// DreamConcept represents a knowledge base concept for dreaming
type DreamConcept struct {
	Name        string
	Description string
	Domain      string
}

// GenerateDreamGoals creates goals by randomly connecting concepts
func (dm *DreamMode) GenerateDreamGoals(maxGoals int) ([]CuriosityGoal, error) {
	if !dm.enabled {
		return nil, nil
	}

	var goals []CuriosityGoal
	
	log.Printf("💭 [Dream] Entering dream mode - generating creative exploration goals...")
	
	// Use mock concepts for now (TODO: integrate with Neo4j later)
	concepts := []DreamConcept{
		{Name: "neural networks", Domain: "Deep Learning"},
		{Name: "symbolic reasoning", Domain: "AI"},
		{Name: "memory consolidation", Domain: "Neuroscience"},
		{Name: "attention mechanisms", Domain: "Deep Learning"},
		{Name: "reinforcement learning", Domain: "Machine Learning"},
		{Name: "cognitive architectures", Domain: "Cognitive Science"},
		{Name: "knowledge graphs", Domain: "Knowledge Representation"},
		{Name: "transfer learning", Domain: "Machine Learning"},
		{Name: "meta-learning", Domain: "Machine Learning"},
		{Name: "evolutionary algorithms", Domain: "Optimization"},
	}
	
	if len(concepts) < 2 {
		return goals, nil
	}
	
	// Shuffle concepts
	rand.Seed(time.Now().UnixNano())
	rand.Shuffle(len(concepts), func(i, j int) {
		concepts[i], concepts[j] = concepts[j], concepts[i]
	})
	
	// Create dream goals by pairing concepts
	for i := 0; i < maxGoals && i*2+1 < len(concepts); i++ {
		c1 := concepts[i*2]
		c2 := concepts[i*2+1]
		
		templates := []string{
			"Explore the relationship between '%s' and '%s' - what connections exist?",
			"Compare and contrast '%s' with '%s' to identify similarities and differences",
			"Investigate how '%s' might influence or interact with '%s'",
			"Analyze potential synergies between '%s' and '%s'",
			"Discover unexpected connections linking '%s' to '%s'",
		}
		
		template := templates[rand.Intn(len(templates))]
		description := fmt.Sprintf(template, c1.Name, c2.Name)
		
		domain := c1.Domain
		if c1.Domain != c2.Domain {
			domain = fmt.Sprintf("%s + %s", c1.Domain, c2.Domain)
		}
		
		goal := CuriosityGoal{
			ID:          fmt.Sprintf("dream_%d", time.Now().UnixNano()+int64(i)),
			Type:        "dream_exploration",
			Description: description,
			Domain:      domain,
			Priority:    6,
			Status:      "pending",
			Targets:     []string{c1.Name, c2.Name},
			CreatedAt:   time.Now(),
			Uncertainty: nil,
			Value:       0.7,
		}
		
		goals = append(goals, goal)
		log.Printf("💭 [Dream] Created: %s ↔ %s (domain: '%s', status: '%s')", c1.Name, c2.Name, goal.Domain, goal.Status)
	}
	
	log.Printf("💭 [Dream] Generated %d dream exploration goals", len(goals))
	return goals, nil
}

// StartDreamCycle triggers periodic dream mode
func (dm *DreamMode) StartDreamCycle(interval time.Duration) {
	log.Printf("💭 [Dream] Dream mode enabled with interval: %v", interval)
	
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	
	for range ticker.C {
		goals, err := dm.GenerateDreamGoals(3)
		if err != nil {
			log.Printf("⚠️ [Dream] Error: %v", err)
			continue
		}
		
		if len(goals) == 0 {
			continue
		}
		
		for _, goal := range goals {
			goalData, _ := json.Marshal(goal)
			key := fmt.Sprintf("reasoning:curiosity_goals:%s", goal.Domain)
			err := dm.redis.LPush(dm.ctx, key, string(goalData)).Err()
			if err != nil {
				log.Printf("❌ [Dream] Failed to store goal to Redis key '%s': %v", key, err)
			} else {
				log.Printf("💭 [Dream] Stored: %s (ID: %s, key: 'reasoning:curiosity_goals:%s')", goal.Description, goal.ID, goal.Domain)
			}

			dm.postGoalToManager(goal)
		}
		
		log.Printf("💭 [Dream] Cycle complete - created %d goals", len(goals))
	}
}

func (dm *DreamMode) postGoalToManager(goal CuriosityGoal) {
	client := &http.Client{Timeout: 10 * time.Second}
	
	goalRequest := map[string]interface{}{
		"id":          goal.ID,
		"agent_id":    "agent_1",
		"description": goal.Description,
		"priority":    goal.Priority,
		"status":      goal.Status,
		"confidence":  goal.Value,
		"context": map[string]interface{}{
			"domain":       goal.Domain,
			"source":       "dream_mode",
			"targets":      goal.Targets,
		},
	}
	
	body, err := json.Marshal(goalRequest)
	if err != nil {
		log.Printf("⚠️ [Dream] Failed to marshal goal for Goal Manager: %v", err)
		return
	}
	
	resp, err := client.Post(
		dm.goalMgrURL+"/goal",
		"application/json",
		bytes.NewReader(body),
	)
	
	if err != nil {
		log.Printf("⚠️ [Dream] Failed to POST goal to Goal Manager: %v", err)
		return
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		log.Printf("⚠️ [Dream] Goal Manager returned status %d for goal %s", resp.StatusCode, goal.ID)
	} else {
		log.Printf("✅ [Dream] Posted goal %s to Goal Manager", goal.ID)
	}
}
