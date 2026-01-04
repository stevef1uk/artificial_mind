package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"time"
)

// DreamMode generates creative exploration goals by randomly connecting concepts
type DreamMode struct {
	fsm     *FSMEngine
	redis   *RedisClient
	ctx     context.Context
	enabled bool
}

// NewDreamMode creates a new dream mode instance
func NewDreamMode(fsm *FSMEngine, redis *RedisClient) *DreamMode {
	return &DreamMode{
		fsm:     fsm,
		redis:   redis,
		ctx:     context.Background(),
		enabled: true,
	}
}

// GenerateDreamGoals creates goals by randomly connecting concepts from the knowledge base
func (dm *DreamMode) GenerateDreamGoals(maxGoals int) ([]CuriosityGoal, error) {
	if !dm.enabled {
		return nil, nil
	}

	var goals []CuriosityGoal
	
	log.Printf("💭 [Dream] Entering dream mode - generating creative exploration goals...")
	
	// Query random concepts from Neo4j knowledge base
	concepts, err := dm.fetchRandomConcepts(maxGoals * 2)
	if err != nil {
		log.Printf("⚠️ [Dream] Failed to fetch concepts: %v", err)
		return goals, err
	}
	
	if len(concepts) < 2 {
		log.Printf("⚠️ [Dream] Not enough concepts for dreaming (need at least 2, got %d)", len(concepts))
		return goals, nil
	}
	
	log.Printf("💭 [Dream] Retrieved %d concepts from knowledge base", len(concepts))
	
	// Randomly pair concepts and create exploration goals
	rand.Seed(time.Now().UnixNano())
	usedPairs := make(map[string]bool)
	
	for i := 0; i < maxGoals && len(goals) < maxGoals; i++ {
		// Pick two random different concepts
		idx1 := rand.Intn(len(concepts))
		idx2 := rand.Intn(len(concepts))
		
		// Ensure they're different
		if idx1 == idx2 {
			idx2 = (idx2 + 1) % len(concepts)
		}
		
		concept1 := concepts[idx1]
		concept2 := concepts[idx2]
		
		// Create a unique pair key
		pairKey := fmt.Sprintf("%s<->%s", concept1.Name, concept2.Name)
		reversePairKey := fmt.Sprintf("%s<->%s", concept2.Name, concept1.Name)
		
		if usedPairs[pairKey] || usedPairs[reversePairKey] {
			continue
		}
		usedPairs[pairKey] = true
		
		// Create dream goal exploring relationship
		goal := dm.createDreamGoal(concept1, concept2)
		goals = append(goals, goal)
		
		log.Printf("💭 [Dream] Created dream goal: %s ↔ %s", concept1.Name, concept2.Name)
	}
	
	log.Printf("💭 [Dream] Generated %d dream exploration goals", len(goals))
	return goals, nil
}

// fetchRandomConcepts gets random concepts from Neo4j
func (dm *DreamMode) fetchRandomConcepts(count int) ([]Concept, error) {
	// Query Neo4j via MCP tool through HDN
	query := map[string]interface{}{
		"tool_name": "query_neo4j",
		"arguments": map[string]interface{}{
			"query": fmt.Sprintf(`
				MATCH (c:Concept)
				WHERE c.name IS NOT NULL
				WITH c, rand() as r
				ORDER BY r
				LIMIT %d
				RETURN c.name as name, 
				       coalesce(c.description, '') as description,
				       coalesce(c.domain, 'General') as domain
			`, count),
		},
	}
	
	queryData, _ := json.Marshal(query)
	resp, err := dm.fsm.httpPostJSON(dm.ctx, dm.fsm.hdnURL+"/mcp", queryData, 15*time.Second)
	if err != nil {
		return nil, fmt.Errorf("failed to query Neo4j: %w", err)
	}
	
	var result struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	
	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, fmt.Errorf("failed to parse Neo4j response: %w", err)
	}
	
	// Parse concepts from response
	var concepts []Concept
	if len(result.Content) > 0 {
		var records []map[string]interface{}
		if err := json.Unmarshal([]byte(result.Content[0].Text), &records); err == nil {
			for _, record := range records {
				concept := Concept{
					Name:        getString(record, "name"),
					Description: getString(record, "description"),
					Domain:      getString(record, "domain"),
				}
				if concept.Name != "" {
					concepts = append(concepts, concept)
				}
			}
		}
	}
	
	return concepts, nil
}

// Concept represents a knowledge base concept
type Concept struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Domain      string `json:"domain"`
}

// createDreamGoal generates a creative exploration goal for two concepts
func (dm *DreamMode) createDreamGoal(c1, c2 Concept) CuriosityGoal {
	// Choose a random exploration template
	templates := []string{
		"Explore the relationship between '%s' and '%s' - what connections exist?",
		"Compare and contrast '%s' with '%s' to identify similarities and differences",
		"Investigate how '%s' might influence or interact with '%s'",
		"Analyze potential synergies between '%s' and '%s'",
		"Discover unexpected connections linking '%s' to '%s'",
		"Examine whether '%s' and '%s' share common principles or patterns",
		"Explore how insights from '%s' could apply to '%s'",
		"Investigate the conceptual distance between '%s' and '%s'",
	}
	
	template := templates[rand.Intn(len(templates))]
	description := fmt.Sprintf(template, c1.Name, c2.Name)
	
	// Determine domain (use the more specific one, or combine)
	domain := c1.Domain
	if c1.Domain == "General" && c2.Domain != "General" {
		domain = c2.Domain
	} else if c1.Domain != c2.Domain {
		domain = fmt.Sprintf("%s + %s", c1.Domain, c2.Domain)
	}
	
	goalID := fmt.Sprintf("dream_%d", time.Now().UnixNano())
	
	return CuriosityGoal{
		ID:          goalID,
		Type:        "dream_exploration",
		Description: description,
		Domain:      domain,
		Priority:    6, // Medium-high priority
		Status:      "pending",
		Targets:     []string{c1.Name, c2.Name},
		CreatedAt:   time.Now(),
		Uncertainty: UncertaintyMetrics{
			EpistemicUncertainty:  0.8, // High uncertainty = high potential for discovery
			AleatoricUncertainty:  0.3,
			CalibratedConfidence:  0.5,
			Stability:             0.5,
			ConfidenceHistory:     []ConfidenceSnapshot{{Confidence: 0.5, Timestamp: time.Now(), Source: "initial"}},
			LastUpdated:           time.Now(),
			DecayRatePerHour:      0.01,
		},
		Value: 0.7, // Creative exploration has good potential value
	}
}

// getString safely extracts a string from map
func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// StartDreamCycle triggers periodic dream mode
func (dm *DreamMode) StartDreamCycle(interval time.Duration) {
	log.Printf("💭 [Dream] Dream mode enabled with interval: %v", interval)
	
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	
	for range ticker.C {
		goals, err := dm.GenerateDreamGoals(3) // Generate 3 dream goals per cycle
		if err != nil {
			log.Printf("⚠️ [Dream] Error generating dream goals: %v", err)
			continue
		}
		
		if len(goals) == 0 {
			continue
		}
		
		// Store dream goals in Redis for the goals poller to pick up
		for _, goal := range goals {
			goalData, _ := json.Marshal(goal)
			key := fmt.Sprintf("reasoning:curiosity_goals:%s", goal.Domain)
			dm.redis.LPush(dm.ctx, key, string(goalData))
			log.Printf("💭 [Dream] Stored dream goal in domain '%s': %s", goal.Domain, goal.Description)
		}
		
		log.Printf("💭 [Dream] Dream cycle complete - created %d new exploration goals", len(goals))
	}
}
