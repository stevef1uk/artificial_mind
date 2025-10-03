package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// KnowledgeIntegration handles integration with the domain knowledge system
type KnowledgeIntegration struct {
	hdnURL        string
	principlesURL string
	redis         *redis.Client
	ctx           context.Context
}

// NewKnowledgeIntegration creates a new knowledge integration instance
func NewKnowledgeIntegration(hdnURL, principlesURL string, redis *redis.Client) *KnowledgeIntegration {
	return &KnowledgeIntegration{
		hdnURL:        hdnURL,
		principlesURL: principlesURL,
		redis:         redis,
		ctx:           context.Background(),
	}
}

// DomainClassificationResult represents the result of domain classification
type DomainClassificationResult struct {
	Domain      string   `json:"domain"`
	Confidence  float64  `json:"confidence"`
	Concepts    []string `json:"concepts"`
	Constraints []string `json:"constraints"`
}

// Fact represents an extracted fact
type Fact struct {
	ID          string                 `json:"id"`
	Content     string                 `json:"content"`
	Domain      string                 `json:"domain"`
	Confidence  float64                `json:"confidence"`
	Properties  map[string]interface{} `json:"properties"`
	Constraints []string               `json:"constraints"`
	CreatedAt   time.Time              `json:"created_at"`
}

// Hypothesis represents a generated hypothesis
type Hypothesis struct {
	ID          string    `json:"id"`
	Description string    `json:"description"`
	Domain      string    `json:"domain"`
	Confidence  float64   `json:"confidence"`
	Status      string    `json:"status"` // proposed, testing, confirmed, refuted
	Facts       []string  `json:"facts"`  // IDs of supporting facts
	Constraints []string  `json:"constraints"`
	CreatedAt   time.Time `json:"created_at"`
}

// Plan represents a hierarchical plan
type Plan struct {
	ID              string     `json:"id"`
	Description     string     `json:"description"`
	Domain          string     `json:"domain"`
	Steps           []PlanStep `json:"steps"`
	ExpectedValue   float64    `json:"expected_value"`
	Risk            float64    `json:"risk"`
	Cost            float64    `json:"cost"`
	SuccessRate     float64    `json:"success_rate"`
	Constraints     []string   `json:"constraints"`
	RelatedConcepts []string   `json:"related_concepts"`
	CreatedAt       time.Time  `json:"created_at"`
}

type PlanStep struct {
	ID          string                 `json:"id"`
	Description string                 `json:"description"`
	Action      string                 `json:"action"`
	Parameters  map[string]interface{} `json:"parameters"`
	Order       int                    `json:"order"`
}

// ClassifyDomain classifies input using domain knowledge
func (ki *KnowledgeIntegration) ClassifyDomain(input string) (*DomainClassificationResult, error) {
	// Search for related concepts in the knowledge base
	searchURL := fmt.Sprintf("%s/api/v1/knowledge/search?name=%s&limit=10", ki.hdnURL, input)

	resp, err := http.Get(searchURL)
	if err != nil {
		return nil, fmt.Errorf("failed to search concepts: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("knowledge search failed with status: %d", resp.StatusCode)
	}

	var searchResult struct {
		Concepts []struct {
			Name       string `json:"name"`
			Domain     string `json:"domain"`
			Definition string `json:"definition"`
		} `json:"concepts"`
		Count int `json:"count"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&searchResult); err != nil {
		return nil, fmt.Errorf("failed to decode search result: %w", err)
	}

	// Determine primary domain and confidence
	domainCounts := make(map[string]int)
	var concepts []string
	var constraints []string

	for _, concept := range searchResult.Concepts {
		domainCounts[concept.Domain]++
		concepts = append(concepts, concept.Name)
	}

	// Find most common domain
	var primaryDomain string
	maxCount := 0
	for domain, count := range domainCounts {
		if count > maxCount {
			primaryDomain = domain
			maxCount = count
		}
	}

	confidence := float64(maxCount) / float64(len(searchResult.Concepts))
	if confidence == 0 {
		confidence = 0.1 // Minimum confidence for unknown domains
	}

	return &DomainClassificationResult{
		Domain:      primaryDomain,
		Confidence:  confidence,
		Concepts:    concepts,
		Constraints: constraints,
	}, nil
}

// ExtractFacts extracts facts from input using domain knowledge
func (ki *KnowledgeIntegration) ExtractFacts(input string, domain string) ([]Fact, error) {
	// Get domain concepts for context
	concepts, err := ki.getDomainConcepts(domain)
	if err != nil {
		return nil, fmt.Errorf("failed to get domain concepts: %w", err)
	}

	// Extract facts based on domain context
	facts := []Fact{
		{
			ID:         fmt.Sprintf("fact_%d", time.Now().UnixNano()),
			Content:    input,
			Domain:     domain,
			Confidence: 0.8, // Simplified confidence calculation
			Properties: map[string]interface{}{
				"source": "user_input",
				"domain": domain,
			},
			Constraints: ki.extractConstraintsFromMap(concepts),
			CreatedAt:   time.Now(),
		},
	}

	return facts, nil
}

// getExistingHypotheses retrieves existing hypotheses from Redis to avoid duplicates
func (ki *KnowledgeIntegration) getExistingHypotheses(domain string) ([]Hypothesis, error) {
	key := fmt.Sprintf("hypotheses:%s", domain)
	hypothesesData, err := ki.redis.LRange(ki.ctx, key, 0, -1).Result()
	if err != nil {
		return nil, err
	}

	var hypotheses []Hypothesis
	for _, data := range hypothesesData {
		var hypothesis Hypothesis
		if err := json.Unmarshal([]byte(data), &hypothesis); err == nil {
			hypotheses = append(hypotheses, hypothesis)
		}
	}

	return hypotheses, nil
}

// isDuplicateHypothesis checks if a hypothesis is similar to existing ones
func (ki *KnowledgeIntegration) isDuplicateHypothesis(newHypothesis Hypothesis, existing []Hypothesis) bool {
	for _, existing := range existing {
		// Check if descriptions are very similar (simple similarity check)
		if strings.Contains(strings.ToLower(newHypothesis.Description), strings.ToLower(existing.Description)) ||
			strings.Contains(strings.ToLower(existing.Description), strings.ToLower(newHypothesis.Description)) {
			return true
		}
	}
	return false
}

// hasRecentlyExplored checks if a concept or pattern has been recently explored
func (ki *KnowledgeIntegration) hasRecentlyExplored(conceptName, domain string, hours int) bool {
	key := fmt.Sprintf("exploration:recent:%s:%s", domain, conceptName)

	// Check if we have a recent exploration record
	lastExplored, err := ki.redis.Get(ki.ctx, key).Result()
	if err != nil {
		return false // No record means not recently explored
	}

	// Parse the timestamp
	lastTime, err := time.Parse(time.RFC3339, lastExplored)
	if err != nil {
		return false
	}

	// Check if it's within the specified hours
	cutoff := time.Now().Add(-time.Duration(hours) * time.Hour)
	return lastTime.After(cutoff)
}

// recordExploration records that a concept has been explored
func (ki *KnowledgeIntegration) recordExploration(conceptName, domain string) {
	key := fmt.Sprintf("exploration:recent:%s:%s", domain, conceptName)
	now := time.Now().Format(time.RFC3339)

	// Store with 24-hour expiration
	ki.redis.Set(ki.ctx, key, now, 24*time.Hour)
}

// hasNewFactsForConcept checks if there are new facts that might change the hypothesis
func (ki *KnowledgeIntegration) hasNewFactsForConcept(conceptName, domain string, since time.Time) bool {
	// Check if there are any new facts about this concept since the last exploration
	key := fmt.Sprintf("facts:concept:%s:%s", domain, conceptName)

	factsData, err := ki.redis.LRange(ki.ctx, key, 0, -1).Result()
	if err != nil {
		return false
	}

	// Check if any facts are newer than the last exploration
	for _, factData := range factsData {
		var fact map[string]interface{}
		if err := json.Unmarshal([]byte(factData), &fact); err == nil {
			if createdAt, ok := fact["created_at"].(string); ok {
				if factTime, err := time.Parse(time.RFC3339, createdAt); err == nil {
					if factTime.After(since) {
						return true // Found a newer fact
					}
				}
			}
		}
	}

	return false
}

// extractConceptNamesFromHypothesis extracts concept names from hypothesis descriptions
func (ki *KnowledgeIntegration) extractConceptNamesFromHypothesis(description string) []string {
	var conceptNames []string

	// Simple extraction - look for capitalized words that might be concept names
	words := strings.Fields(description)
	for _, word := range words {
		// Remove punctuation and check if it's capitalized
		cleanWord := strings.Trim(word, ".,!?;:")
		if len(cleanWord) > 2 && strings.ToUpper(cleanWord[:1]) == cleanWord[:1] {
			conceptNames = append(conceptNames, cleanWord)
		}
	}

	return conceptNames
}

// getLastExplorationTime gets the last time a concept was explored
func (ki *KnowledgeIntegration) getLastExplorationTime(conceptName, domain string) time.Time {
	key := fmt.Sprintf("exploration:recent:%s:%s", domain, conceptName)

	lastExplored, err := ki.redis.Get(ki.ctx, key).Result()
	if err != nil {
		return time.Time{} // Zero time means never explored
	}

	lastTime, err := time.Parse(time.RFC3339, lastExplored)
	if err != nil {
		return time.Time{}
	}

	return lastTime
}

// GenerateHypotheses generates hypotheses using domain knowledge
func (ki *KnowledgeIntegration) GenerateHypotheses(facts []Fact, domain string) ([]Hypothesis, error) {
	log.Printf("🧠 Generating data-driven hypotheses from %d facts in domain: %s", len(facts), domain)

	// Check for existing hypotheses to avoid duplicates
	existingHypotheses, err := ki.getExistingHypotheses(domain)
	if err != nil {
		log.Printf("Warning: Failed to get existing hypotheses: %v", err)
		existingHypotheses = []Hypothesis{}
	}

	// Get actual concepts from the knowledge base
	concepts, err := ki.getDomainConcepts(domain)
	if err != nil {
		log.Printf("Warning: Failed to get domain concepts: %v", err)
		concepts = []map[string]interface{}{}
	}

	// If no concepts available, generate basic hypotheses based on facts or return empty
	if len(concepts) == 0 {
		log.Printf("ℹ️ No concepts found in domain %s, generating basic hypotheses from facts", domain)

		// If we have facts, generate basic hypotheses from them
		if len(facts) > 0 {
			var basicHypotheses []Hypothesis
			for i, fact := range facts {
				hypothesis := Hypothesis{
					ID:          fmt.Sprintf("hyp_basic_%d_%d", time.Now().UnixNano(), i),
					Description: fmt.Sprintf("Investigate %s to discover new %s opportunities", fact.Content, domain),
					Domain:      domain,
					Confidence:  fact.Confidence * 0.5,
					Status:      "proposed",
					Facts:       []string{fact.ID},
					Constraints: fact.Constraints,
					CreatedAt:   time.Now(),
				}

				// Check for duplicates before adding
				if !ki.isDuplicateHypothesis(hypothesis, existingHypotheses) {
					basicHypotheses = append(basicHypotheses, hypothesis)
				} else {
					log.Printf("⚠️ Skipping duplicate hypothesis: %s", hypothesis.Description)
				}
			}
			log.Printf("✅ Generated %d basic hypotheses from facts (after deduplication)", len(basicHypotheses))
			return basicHypotheses, nil
		}

		// No concepts and no facts - return empty
		log.Printf("ℹ️ No concepts or facts available, returning empty hypotheses")
		return []Hypothesis{}, nil
	}

	var hypotheses []Hypothesis

	// Generate hypotheses based on actual concept relationships and patterns
	for i, concept := range concepts {
		conceptName, _ := concept["name"].(string)
		conceptDef, _ := concept["definition"].(string)

		if conceptName == "" || conceptDef == "" {
			continue
		}

		// Check if we've recently explored this concept (within last 6 hours)
		// But allow re-exploration if there are new facts
		if ki.hasRecentlyExplored(conceptName, domain, 6) {
			// Check if there are new facts that might change the hypothesis
			lastExploredTime := ki.getLastExplorationTime(conceptName, domain)
			if lastExploredTime.IsZero() || !ki.hasNewFactsForConcept(conceptName, domain, lastExploredTime) {
				log.Printf("⏭️ Skipping recently explored concept: %s (explored within last 6 hours, no new facts)", conceptName)
				continue
			} else {
				log.Printf("🔄 Re-exploring concept %s due to new facts", conceptName)
			}
		}

		// Generate hypothesis based on concept analysis
		hypothesis := ki.generateConceptBasedHypothesis(conceptName, conceptDef, domain, i)
		if hypothesis != nil {
			// Check for duplicates before adding
			if !ki.isDuplicateHypothesis(*hypothesis, existingHypotheses) {
				hypotheses = append(hypotheses, *hypothesis)
				// Record that we've explored this concept
				ki.recordExploration(conceptName, domain)
				log.Printf("✅ Generated hypothesis for concept: %s", conceptName)
			} else {
				log.Printf("⚠️ Skipping duplicate concept-based hypothesis: %s", hypothesis.Description)
			}
		}
	}

	// Generate hypotheses based on concept relationships
	relationshipHypotheses := ki.generateRelationshipHypotheses(concepts, domain)
	for _, hypothesis := range relationshipHypotheses {
		// Extract concept names from hypothesis description for exploration tracking
		conceptNames := ki.extractConceptNamesFromHypothesis(hypothesis.Description)

		// Check if we've recently explored these concepts
		recentlyExplored := false
		for _, conceptName := range conceptNames {
			if ki.hasRecentlyExplored(conceptName, domain, 6) {
				log.Printf("⏭️ Skipping relationship hypothesis involving recently explored concept: %s", conceptName)
				recentlyExplored = true
				break
			}
		}

		if !recentlyExplored && !ki.isDuplicateHypothesis(hypothesis, existingHypotheses) {
			hypotheses = append(hypotheses, hypothesis)
			// Record exploration for the concepts involved
			for _, conceptName := range conceptNames {
				ki.recordExploration(conceptName, domain)
			}
		} else if recentlyExplored {
			// Already logged above
		} else {
			log.Printf("⚠️ Skipping duplicate relationship hypothesis: %s", hypothesis.Description)
		}
	}

	// Generate hypotheses based on facts if we have them
	for i, fact := range facts {
		factHypothesis := ki.generateFactBasedHypothesis(fact, concepts, domain, i)
		if factHypothesis != nil {
			// Check for duplicates before adding
			if !ki.isDuplicateHypothesis(*factHypothesis, existingHypotheses) {
				hypotheses = append(hypotheses, *factHypothesis)
			} else {
				log.Printf("⚠️ Skipping duplicate fact-based hypothesis: %s", factHypothesis.Description)
			}
		}
	}

	// Final deduplication pass to remove any remaining duplicates within the generated set
	var finalHypotheses []Hypothesis
	for _, hypothesis := range hypotheses {
		isDuplicate := false
		for _, existing := range finalHypotheses {
			if ki.isDuplicateHypothesis(hypothesis, []Hypothesis{existing}) {
				log.Printf("⚠️ Skipping duplicate hypothesis in final pass: %s", hypothesis.Description)
				isDuplicate = true
				break
			}
		}
		if !isDuplicate {
			finalHypotheses = append(finalHypotheses, hypothesis)
		}
	}

	log.Printf("✅ Generated %d data-driven hypotheses (after deduplication)", len(finalHypotheses))
	return finalHypotheses, nil
}

// generateConceptBasedHypothesis creates hypotheses based on individual concept analysis
func (ki *KnowledgeIntegration) generateConceptBasedHypothesis(conceptName, conceptDef, domain string, index int) *Hypothesis {
	// Analyze concept definition for hypothesis opportunities
	lowerDef := strings.ToLower(conceptDef)

	// Look for patterns that suggest improvement opportunities
	var hypothesisDesc string
	var confidence float64 = 0.5

	if strings.Contains(lowerDef, "study") || strings.Contains(lowerDef, "science") {
		hypothesisDesc = fmt.Sprintf("If we apply scientific methods to %s, we can enhance %s capabilities", conceptName, domain)
		confidence = 0.7
	} else if strings.Contains(lowerDef, "technology") || strings.Contains(lowerDef, "system") {
		hypothesisDesc = fmt.Sprintf("If we optimize the %s system, we can improve %s performance", conceptName, domain)
		confidence = 0.6
	} else if strings.Contains(lowerDef, "practice") || strings.Contains(lowerDef, "application") {
		hypothesisDesc = fmt.Sprintf("If we enhance %s practices, we can improve %s outcomes", conceptName, domain)
		confidence = 0.6
	} else if strings.Contains(lowerDef, "knowledge") || strings.Contains(lowerDef, "understanding") {
		hypothesisDesc = fmt.Sprintf("If we expand our knowledge of %s, we can improve %s capabilities", conceptName, domain)
		confidence = 0.5
	} else {
		// Generic hypothesis based on concept
		hypothesisDesc = fmt.Sprintf("If we explore %s further, we can discover new insights about %s", conceptName, domain)
		confidence = 0.4
	}

	return &Hypothesis{
		ID:          fmt.Sprintf("hyp_concept_%d_%d", time.Now().UnixNano(), index),
		Description: hypothesisDesc,
		Domain:      domain,
		Confidence:  confidence,
		Status:      "proposed",
		Facts:       []string{fmt.Sprintf("concept_%s", conceptName)},
		Constraints: []string{},
		CreatedAt:   time.Now(),
	}
}

// generateRelationshipHypotheses creates hypotheses based on concept relationships
func (ki *KnowledgeIntegration) generateRelationshipHypotheses(concepts []map[string]interface{}, domain string) []Hypothesis {
	var hypotheses []Hypothesis

	// Find concepts that might be related
	for i, concept1 := range concepts {
		name1, _ := concept1["name"].(string)
		def1, _ := concept1["definition"].(string)

		if name1 == "" || def1 == "" {
			continue
		}

		// Look for concepts that mention each other
		for j, concept2 := range concepts {
			if i >= j { // Avoid duplicates
				continue
			}

			name2, _ := concept2["name"].(string)
			def2, _ := concept2["definition"].(string)

			if name2 == "" || def2 == "" {
				continue
			}

			// Check if concepts reference each other
			if strings.Contains(strings.ToLower(def1), strings.ToLower(name2)) ||
				strings.Contains(strings.ToLower(def2), strings.ToLower(name1)) {

				hypothesis := Hypothesis{
					ID:          fmt.Sprintf("hyp_rel_%d_%d_%d", time.Now().UnixNano(), i, j),
					Description: fmt.Sprintf("If we combine %s and %s, we can create new %s capabilities", name1, name2, domain),
					Domain:      domain,
					Confidence:  0.6,
					Status:      "proposed",
					Facts:       []string{fmt.Sprintf("concept_%s", name1), fmt.Sprintf("concept_%s", name2)},
					Constraints: []string{},
					CreatedAt:   time.Now(),
				}
				hypotheses = append(hypotheses, hypothesis)
			}
		}
	}

	return hypotheses
}

// generateFactBasedHypothesis creates hypotheses based on facts
func (ki *KnowledgeIntegration) generateFactBasedHypothesis(fact Fact, concepts []map[string]interface{}, domain string, index int) *Hypothesis {
	// Analyze fact content for hypothesis opportunities
	lowerContent := strings.ToLower(fact.Content)

	var hypothesisDesc string
	var confidence float64 = fact.Confidence * 0.6

	// Look for patterns in fact content
	if strings.Contains(lowerContent, "working") || strings.Contains(lowerContent, "functioning") {
		hypothesisDesc = fmt.Sprintf("If %s continues working, we can leverage it to improve %s", fact.Content, domain)
	} else if strings.Contains(lowerContent, "improve") || strings.Contains(lowerContent, "enhance") {
		hypothesisDesc = fmt.Sprintf("If we build on %s, we can further improve %s", fact.Content, domain)
	} else if strings.Contains(lowerContent, "learn") || strings.Contains(lowerContent, "understand") {
		hypothesisDesc = fmt.Sprintf("If we apply insights from %s, we can improve our %s approach", fact.Content, domain)
	} else {
		hypothesisDesc = fmt.Sprintf("If we investigate %s further, we can discover new %s opportunities", fact.Content, domain)
	}

	return &Hypothesis{
		ID:          fmt.Sprintf("hyp_fact_%d_%d", time.Now().UnixNano(), index),
		Description: hypothesisDesc,
		Domain:      domain,
		Confidence:  confidence,
		Status:      "proposed",
		Facts:       []string{fact.ID},
		Constraints: fact.Constraints,
		CreatedAt:   time.Now(),
	}
}

// CreatePlan creates a hierarchical plan using domain knowledge
func (ki *KnowledgeIntegration) CreatePlan(hypothesis Hypothesis, domain string) (*Plan, error) {
	// Get domain success rates and constraints
	successRates, err := ki.getDomainSuccessRates(domain)
	if err != nil {
		return nil, fmt.Errorf("failed to get domain success rates: %w", err)
	}

	// Create plan steps based on hypothesis and domain knowledge
	steps := []PlanStep{
		{
			ID:          fmt.Sprintf("step_%d_1", time.Now().UnixNano()),
			Description: fmt.Sprintf("Test hypothesis: %s", hypothesis.Description),
			Action:      "execute_test",
			Parameters: map[string]interface{}{
				"hypothesis_id": hypothesis.ID,
				"domain":        domain,
			},
			Order: 1,
		},
		{
			ID:          fmt.Sprintf("step_%d_2", time.Now().UnixNano()),
			Description: "Measure results and validate against domain constraints",
			Action:      "measure_results",
			Parameters: map[string]interface{}{
				"domain": domain,
			},
			Order: 2,
		},
	}

	// Calculate expected value and risk based on domain knowledge
	expectedValue := 0.7 // Base value
	risk := 0.3          // Base risk
	successRate := 0.8   // Base success rate

	if rates, exists := successRates[domain]; exists {
		successRate = rates
		expectedValue = rates * 0.9 // Expected value is slightly lower than success rate
		risk = 1.0 - rates          // Risk is inverse of success rate
	}

	plan := &Plan{
		ID:              fmt.Sprintf("plan_%d", time.Now().UnixNano()),
		Description:     fmt.Sprintf("Test plan for hypothesis: %s", hypothesis.Description),
		Domain:          domain,
		Steps:           steps,
		ExpectedValue:   expectedValue,
		Risk:            risk,
		Cost:            0.1, // Base cost
		SuccessRate:     successRate,
		Constraints:     hypothesis.Constraints,
		RelatedConcepts: []string{}, // Would be populated from domain knowledge
		CreatedAt:       time.Now(),
	}

	return plan, nil
}

// CheckPrinciples checks if a plan is allowed by principles
func (ki *KnowledgeIntegration) CheckPrinciples(plan *Plan) (bool, error) {
	// Call Principles Server
	principlesReq := map[string]interface{}{
		"action": plan.Description,
		"context": map[string]interface{}{
			"domain":        plan.Domain,
			"expected_cost": plan.Cost,
			"risk":          plan.Risk,
			"constraints":   plan.Constraints,
		},
	}

	reqData, _ := json.Marshal(principlesReq)
	resp, err := http.Post(ki.principlesURL+"/action", "application/json", bytes.NewReader(reqData))
	if err != nil {
		return false, fmt.Errorf("failed to call principles server: %w", err)
	}
	defer resp.Body.Close()

	var principlesResp struct {
		Allowed bool   `json:"allowed"`
		Reason  string `json:"reason,omitempty"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&principlesResp); err != nil {
		return false, fmt.Errorf("failed to decode principles response: %w", err)
	}

	return principlesResp.Allowed, nil
}

// UpdateDomainKnowledge updates domain knowledge based on execution results
func (ki *KnowledgeIntegration) UpdateDomainKnowledge(domain string, results map[string]interface{}) error {
	// Update success rates based on results
	success := results["success"].(bool)
	executionTime := results["execution_time"].(float64)

	// Update Redis with new success data
	key := fmt.Sprintf("domain_success:%s", domain)
	successData := map[string]interface{}{
		"success":        success,
		"execution_time": executionTime,
		"timestamp":      time.Now().Unix(),
	}

	data, _ := json.Marshal(successData)
	return ki.redis.LPush(ki.ctx, key, data).Err()
}

// Helper methods
func (ki *KnowledgeIntegration) getDomainConcepts(domain string) ([]map[string]interface{}, error) {
	searchURL := fmt.Sprintf("%s/api/v1/knowledge/search?domain=%s&limit=50", ki.hdnURL, domain)

	resp, err := http.Get(searchURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var searchResult struct {
		Concepts []map[string]interface{} `json:"concepts"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&searchResult); err != nil {
		return nil, err
	}

	return searchResult.Concepts, nil
}

func (ki *KnowledgeIntegration) getDomainSuccessRates(domain string) (map[string]float64, error) {
	// Get historical success rates from Redis
	key := fmt.Sprintf("domain_success:%s", domain)
	results, err := ki.redis.LRange(ki.ctx, key, 0, 99).Result()
	if err != nil {
		return map[string]float64{domain: 0.8}, nil // Default success rate
	}

	// Calculate average success rate
	totalSuccess := 0
	totalExecutions := 0

	for _, result := range results {
		var successData map[string]interface{}
		if err := json.Unmarshal([]byte(result), &successData); err != nil {
			continue
		}

		totalExecutions++
		if success, ok := successData["success"].(bool); ok && success {
			totalSuccess++
		}
	}

	successRate := 0.8 // Default
	if totalExecutions > 0 {
		successRate = float64(totalSuccess) / float64(totalExecutions)
	}

	return map[string]float64{domain: successRate}, nil
}

func (ki *KnowledgeIntegration) extractConstraints(concepts []string) []string {
	// Simplified constraint extraction
	// In a real implementation, this would query the knowledge base for constraints
	return []string{
		"Must follow domain safety principles",
		"Must validate against domain constraints",
	}
}

func (ki *KnowledgeIntegration) extractConstraintsFromMap(concepts []map[string]interface{}) []string {
	// Extract constraints from concept data
	var constraints []string

	// Add basic constraints
	constraints = append(constraints, "Must follow domain safety principles")
	constraints = append(constraints, "Must validate against domain constraints")

	// Extract domain-specific constraints from concepts
	for _, concept := range concepts {
		if domain, ok := concept["domain"].(string); ok && domain != "" {
			constraints = append(constraints, fmt.Sprintf("Must respect %s domain rules", domain))
		}
	}

	return constraints
}
