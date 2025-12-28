package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// KnowledgeIntegration handles integration with the domain knowledge system
type KnowledgeIntegration struct {
	hdnURL        string
	principlesURL string
	mcpEndpoint   string // MCP server endpoint
	redis         *redis.Client
	ctx           context.Context
}

// NewKnowledgeIntegration creates a new knowledge integration instance
func NewKnowledgeIntegration(hdnURL, principlesURL string, redis *redis.Client) *KnowledgeIntegration {
	mcpEndpoint := hdnURL + "/mcp"
	if hdnURL == "" {
		mcpEndpoint = "http://localhost:8081/mcp"
	}
	return &KnowledgeIntegration{
		hdnURL:        hdnURL,
		principlesURL: principlesURL,
		mcpEndpoint:   mcpEndpoint,
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
	ID          string            `json:"id"`
	Description string            `json:"description"`
	Domain      string            `json:"domain"`
	Confidence  float64           `json:"confidence"` // Legacy field, use Uncertainty.CalibratedConfidence
	Status      string            `json:"status"`     // proposed, testing, confirmed, refuted
	Facts       []string          `json:"facts"`      // IDs of supporting facts
	Constraints []string          `json:"constraints"`
	CreatedAt   time.Time         `json:"created_at"`
	Uncertainty *UncertaintyModel `json:"uncertainty,omitempty"` // Formal uncertainty model
	// Causal reasoning fields
	CausalType            string   `json:"causal_type,omitempty"`            // "observational_relation", "inferred_causal_candidate", "experimentally_testable_relation"
	CounterfactualActions []string `json:"counterfactual_actions,omitempty"` // Actions for counterfactual reasoning ("what outcome would change my belief?")
	InterventionGoals     []string `json:"intervention_goals,omitempty"`     // Goals for intervention-style experiments
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
	// URL-encode the input to handle spaces and special characters
	encodedInput := url.QueryEscape(input)
	searchURL := fmt.Sprintf("%s/api/v1/knowledge/search?name=%s&limit=10", ki.hdnURL, encodedInput)

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
// Now with relevance filtering to learn more useful knowledge
func (ki *KnowledgeIntegration) ExtractFacts(input string, domain string) ([]Fact, error) {
	// Get domain concepts for context
	concepts, err := ki.getDomainConcepts(domain)
	if err != nil {
		return nil, fmt.Errorf("failed to get domain concepts: %w", err)
	}

	// Extract meaningful facts using LLM (not just wrapping input)
	facts, err := ki.extractMeaningfulFacts(input, domain, concepts)
	if err != nil {
		log.Printf("⚠️ Failed to extract meaningful facts: %v, falling back to basic extraction", err)
		// Fallback to basic extraction
		facts = []Fact{
			{
				ID:         fmt.Sprintf("fact_%d", time.Now().UnixNano()),
				Content:    input,
				Domain:     domain,
				Confidence: 0.5, // Lower confidence for fallback
				Properties: map[string]interface{}{
					"source": "user_input",
					"domain": domain,
				},
				Constraints: ki.extractConstraintsFromMap(concepts),
				CreatedAt:   time.Now(),
			},
		}
	}

	// Score relevance and filter
	facts = ki.filterByRelevance(facts, domain)

	return facts, nil
}

// extractMeaningfulFacts uses LLM to extract actual facts from text
func (ki *KnowledgeIntegration) extractMeaningfulFacts(input string, domain string, concepts []map[string]interface{}) ([]Fact, error) {
	if len(strings.TrimSpace(input)) == 0 {
		return []Fact{}, nil
	}

	// Get user interests/goals for relevance
	userInterests := ki.getUserInterests()

	// Build context about domain concepts
	conceptNames := []string{}
	for _, c := range concepts {
		if name, ok := c["name"].(string); ok {
			conceptNames = append(conceptNames, name)
		}
	}
	conceptContext := strings.Join(conceptNames, ", ")
	if conceptContext == "" {
		conceptContext = "general knowledge"
	}

	// Create prompt for fact extraction with relevance focus
	prompt := fmt.Sprintf(`Extract actionable, useful facts from the following text in the %s domain.

Text: %s

Domain Context: %s

User Interests: %s

Extract facts that are:
1. ACTIONABLE - Can be used to accomplish tasks or make decisions
2. SPECIFIC - Concrete information, not vague statements
3. RELEVANT - Related to user interests or domain knowledge
4. USEFUL - Will help with future tasks or understanding

For each fact, provide:
- A clear, specific statement
- Why it's useful/actionable
- Relevance score (0.0-1.0) based on user interests

Return as JSON array:
[
  {
    "fact": "Specific actionable fact",
    "usefulness": "Why this fact is useful",
    "relevance": 0.85,
    "actionable": true
  }
]

Skip facts that are:
- Too vague or generic
- Not actionable
- Not relevant to user interests
- Just restating obvious information

If no useful facts found, return empty array [].`, domain, input, conceptContext, userInterests)

	// Call HDN interpret endpoint
	hdnURL := strings.TrimSuffix(ki.hdnURL, "/")
	if hdnURL == "" {
		hdnURL = "http://localhost:8081"
	}

	interpretURL := fmt.Sprintf("%s/api/v1/interpret", hdnURL)
	reqData := map[string]interface{}{
		"input": prompt, // API expects "input" not "text"
		"context": map[string]string{
			"origin": "fsm", // Mark as background task for LOW priority
		},
	}

	reqJSON, err := json.Marshal(reqData)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal fact extraction request: %w", err)
	}

	// Use async HTTP client (or sync fallback)
	resp, err := Post(ki.ctx, interpretURL, "application/json", reqJSON, nil)
	if err != nil {
		return nil, fmt.Errorf("fact extraction LLM call failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fact extraction returned status %d", resp.StatusCode)
	}

	var interpretResp map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&interpretResp); err != nil {
		return nil, fmt.Errorf("failed to decode fact extraction response: %w", err)
	}

	// Extract facts from response
	factsJSON, ok := interpretResp["result"].(string)
	if !ok {
		if result, ok := interpretResp["output"].(string); ok {
			factsJSON = result
		} else {
			return nil, fmt.Errorf("no result in fact extraction response")
		}
	}

	// Parse facts from JSON
	var factData []map[string]interface{}
	if err := json.Unmarshal([]byte(factsJSON), &factData); err != nil {
		// Try to extract JSON array from text response
		start := strings.Index(factsJSON, "[")
		end := strings.LastIndex(factsJSON, "]")
		if start >= 0 && end > start {
			factsJSON = factsJSON[start : end+1]
			if err := json.Unmarshal([]byte(factsJSON), &factData); err != nil {
				return nil, fmt.Errorf("failed to parse facts JSON: %w", err)
			}
		} else {
			return nil, fmt.Errorf("no JSON array found in response")
		}
	}

	// Convert to Fact structs
	var facts []Fact
	for _, fd := range factData {
		factContent, _ := fd["fact"].(string)
		relevance, _ := fd["relevance"].(float64)
		actionable, _ := fd["actionable"].(bool)

		if factContent == "" {
			continue
		}

		// Ensure relevance is valid
		if relevance < 0.0 {
			relevance = 0.0
		}
		if relevance > 1.0 {
			relevance = 1.0
		}

		// Only include actionable facts with reasonable relevance
		if actionable && relevance >= 0.3 {
			fact := Fact{
				ID:         fmt.Sprintf("fact_%d", time.Now().UnixNano()),
				Content:    factContent,
				Domain:     domain,
				Confidence: relevance, // Use relevance as confidence
				Properties: map[string]interface{}{
					"source":     "llm_extraction",
					"domain":     domain,
					"relevance":  relevance,
					"actionable": actionable,
					"usefulness": fd["usefulness"],
				},
				Constraints: ki.extractConstraintsFromMap(concepts),
				CreatedAt:   time.Now(),
			}
			facts = append(facts, fact)
			previewLen := 50
			if len(factContent) < previewLen {
				previewLen = len(factContent)
			}
			log.Printf("✨ Extracted relevant fact: %s (relevance: %.2f)", factContent[:previewLen], relevance)
		}
	}

	if len(facts) > 0 {
		log.Printf("📚 Extracted %d relevant facts from input", len(facts))
	}

	return facts, nil
}

// filterByRelevance filters facts by relevance score and checks for novelty
func (ki *KnowledgeIntegration) filterByRelevance(facts []Fact, domain string) []Fact {
	userInterests := ki.getUserInterests()
	var filtered []Fact

	for _, fact := range facts {
		// Get relevance score from properties
		relevance := 0.5 // Default
		if rel, ok := fact.Properties["relevance"].(float64); ok {
			relevance = rel
		} else {
			// Calculate relevance if not provided
			relevance = ki.calculateRelevance(fact.Content, userInterests, domain)
		}

		// Only keep facts with relevance >= 0.4
		if relevance >= 0.4 {
			// Check if this fact is novel and worth learning
			isNovel, isWorthLearning, err := ki.assessKnowledgeValue(fact.Content, domain)
			if err != nil {
				log.Printf("⚠️ Failed to assess knowledge value for fact, defaulting to store: %v", err)
				// Default to storing if assessment fails
				isNovel = true
				isWorthLearning = true
			}

			if isNovel && isWorthLearning {
				// Check if knowledge already exists
				alreadyExists, err := ki.knowledgeAlreadyExists(fact.Content, domain)
				if err != nil {
					log.Printf("⚠️ Failed to check if knowledge exists, defaulting to store: %v", err)
					alreadyExists = false
				}

				if !alreadyExists {
					fact.Confidence = relevance
					fact.Properties["relevance"] = relevance
					fact.Properties["novel"] = isNovel
					fact.Properties["worth_learning"] = isWorthLearning
					filtered = append(filtered, fact)
				} else {
					previewLen := 50
					if len(fact.Content) < previewLen {
						previewLen = len(fact.Content)
					}
					log.Printf("⏭️ Skipping fact (already known): %s", fact.Content[:previewLen])
				}
			} else {
				previewLen := 50
				if len(fact.Content) < previewLen {
					previewLen = len(fact.Content)
				}
				if !isNovel {
					log.Printf("⏭️ Skipping fact (not novel/obvious): %s", fact.Content[:previewLen])
				} else {
					log.Printf("⏭️ Skipping fact (not worth learning): %s", fact.Content[:previewLen])
				}
			}
		} else {
			previewLen := 50
			if len(fact.Content) < previewLen {
				previewLen = len(fact.Content)
			}
			log.Printf("🛑 Filtered out low-relevance fact: %s (relevance: %.2f)",
				fact.Content[:previewLen], relevance)
		}
	}

	log.Printf("📊 Relevance/novelty filtering: %d facts kept out of %d", len(filtered), len(facts))
	return filtered
}

// calculateRelevance calculates relevance score for a fact
func (ki *KnowledgeIntegration) calculateRelevance(factContent string, userInterests string, domain string) float64 {
	relevance := 0.5 // Base relevance

	// Boost relevance if fact mentions user interests
	if userInterests != "" {
		lowerFact := strings.ToLower(factContent)
		lowerInterests := strings.ToLower(userInterests)
		interestWords := strings.Fields(lowerInterests)
		matches := 0
		for _, word := range interestWords {
			if len(word) > 3 && strings.Contains(lowerFact, word) {
				matches++
			}
		}
		if matches > 0 {
			relevance += float64(matches) * 0.15
		}
	}

	// Boost relevance for actionable keywords
	actionableKeywords := []string{"can", "should", "must", "need", "how to", "way to", "method", "process", "step"}
	lowerFact := strings.ToLower(factContent)
	for _, keyword := range actionableKeywords {
		if strings.Contains(lowerFact, keyword) {
			relevance += 0.1
			break
		}
	}

	// Penalize vague facts
	vagueKeywords := []string{"something", "things", "stuff", "various", "many", "some"}
	for _, keyword := range vagueKeywords {
		if strings.Contains(lowerFact, keyword) {
			relevance -= 0.1
			break
		}
	}

	// Ensure relevance is between 0 and 1
	if relevance < 0 {
		relevance = 0
	}
	if relevance > 1 {
		relevance = 1
	}

	return relevance
}

// getUserInterests retrieves user interests/goals from Redis
func (ki *KnowledgeIntegration) getUserInterests() string {
	// Try to get user interests from Redis
	interestsKey := "user:interests"
	interests, err := ki.redis.Get(ki.ctx, interestsKey).Result()
	if err == nil && interests != "" {
		return interests
	}

	// Try to get from recent goals (what user is working on)
	goalsKey := "reasoning:curiosity_goals:all"
	goalsData, err := ki.redis.LRange(ki.ctx, goalsKey, 0, 4).Result()
	if err == nil && len(goalsData) > 0 {
		var interests []string
		for _, goalData := range goalsData {
			var goal map[string]interface{}
			if err := json.Unmarshal([]byte(goalData), &goal); err == nil {
				if desc, ok := goal["description"].(string); ok {
					interests = append(interests, desc)
				}
			}
		}
		if len(interests) > 0 {
			return strings.Join(interests, ", ")
		}
	}

	return "general knowledge, problem solving, task completion"
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

// hasSimilarFailedHypothesis checks if a similar hypothesis has failed before
// 🧠 INTELLIGENCE: Avoid testing hypotheses similar to ones that failed
func (ki *KnowledgeIntegration) hasSimilarFailedHypothesis(description, domain string) bool {
	key := fmt.Sprintf("fsm:%s:hypotheses", "agent_1") // TODO: get agent ID properly
	hypothesesData, err := ki.redis.HGetAll(ki.ctx, key).Result()
	if err != nil {
		return false // No data available
	}

	descLower := strings.ToLower(description)

	// Check each stored hypothesis
	for _, hypData := range hypothesesData {
		var hypothesis map[string]interface{}
		if err := json.Unmarshal([]byte(hypData), &hypothesis); err != nil {
			continue
		}

		// Check if this hypothesis failed or was refuted
		status, ok := hypothesis["status"].(string)
		if !ok || (status != "refuted" && status != "failed") {
			continue // Only check failed/refuted hypotheses
		}

		// Check if descriptions are similar
		existingDesc, ok := hypothesis["description"].(string)
		if !ok {
			continue
		}
		existingDescLower := strings.ToLower(existingDesc)

		// Check for similarity (shared keywords or substring match)
		// Extract key terms (words longer than 4 chars)
		newTerms := ki.extractKeyTerms(descLower)
		existingTerms := ki.extractKeyTerms(existingDescLower)

		// If they share 2+ key terms, consider them similar
		sharedTerms := 0
		for _, term := range newTerms {
			for _, existingTerm := range existingTerms {
				if term == existingTerm && len(term) > 4 {
					sharedTerms++
					break
				}
			}
		}

		// If similar and failed, avoid testing
		if sharedTerms >= 2 {
			log.Printf("🧠 [INTELLIGENCE] Skipping hypothesis similar to failed one: '%s' (similar to failed: '%s')",
				description[:min(60, len(description))], existingDesc[:min(60, len(existingDesc))])
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

	// Enhance concepts with related concepts using MCP for better context
	concepts = ki.enhanceConceptsWithRelated(concepts, domain)

	// If no concepts available, generate basic hypotheses based on facts or return empty
	if len(concepts) == 0 {
		log.Printf("ℹ️ No concepts found in domain %s, generating basic hypotheses from facts", domain)

		// If we have facts, generate basic hypotheses from them
		if len(facts) > 0 {
			var basicHypotheses []Hypothesis
			for i, fact := range facts {
				baseConfidence := fact.Confidence * 0.5
				// Estimate uncertainties
				epistemicUncertainty := EstimateEpistemicUncertainty(1, false, false)
				aleatoricUncertainty := EstimateAleatoricUncertainty(domain, "")
				uncertainty := NewUncertaintyModel(baseConfidence, epistemicUncertainty, aleatoricUncertainty)

				hypothesis := Hypothesis{
					ID:          fmt.Sprintf("hyp_basic_%d_%d", time.Now().UnixNano(), i),
					Description: fmt.Sprintf("Investigate %s to discover new %s opportunities", fact.Content, domain),
					Domain:      domain,
					Confidence:  uncertainty.CalibratedConfidence,
					Status:      "proposed",
					Facts:       []string{fact.ID},
					Constraints: fact.Constraints,
					CreatedAt:   time.Now(),
					Uncertainty: uncertainty,
				}

				// Check for duplicates before adding
				if !ki.isDuplicateHypothesis(hypothesis, existingHypotheses) {
					// Enrich basic hypotheses with causal reasoning too
					enrichedHypothesis := ki.enrichHypothesisWithCausalReasoning(hypothesis, domain)
					basicHypotheses = append(basicHypotheses, enrichedHypothesis)
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
			// 🧠 INTELLIGENCE: Check if similar hypothesis failed before
			if ki.hasSimilarFailedHypothesis(hypothesis.Description, domain) {
				log.Printf("🧠 [INTELLIGENCE] Skipping hypothesis similar to failed one: %s", hypothesis.Description[:min(60, len(hypothesis.Description))])
				continue
			}

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
			// Pre-evaluate hypothesis value before adding
			potentialValue := ki.evaluateFactHypothesisPotential(fact, domain)
			if potentialValue < 0.3 {
				log.Printf("⏭️ Skipping low-value fact-based hypothesis: %s (value: %.2f < 0.3)", factHypothesis.Description, potentialValue)
				continue
			}
			// Scale confidence by potential value
			factHypothesis.Confidence = factHypothesis.Confidence * potentialValue

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
			// Enrich hypothesis with causal reasoning signals
			log.Printf("🔬 [CAUSAL-DEBUG] About to enrich hypothesis (not duplicate): %s", hypothesis.Description[:min(60, len(hypothesis.Description))])
			enrichedHypothesis := ki.enrichHypothesisWithCausalReasoning(hypothesis, domain)
			finalHypotheses = append(finalHypotheses, enrichedHypothesis)
		} else {
			log.Printf("🔬 [CAUSAL-DEBUG] Skipping enrichment for duplicate hypothesis: %s", hypothesis.Description[:min(60, len(hypothesis.Description))])
		}
	}

	log.Printf("✅ Generated %d data-driven hypotheses (after deduplication)", len(finalHypotheses))
	return finalHypotheses, nil
}

// generateConceptBasedHypothesis creates hypotheses based on individual concept analysis
func (ki *KnowledgeIntegration) generateConceptBasedHypothesis(conceptName, conceptDef, domain string, index int) *Hypothesis {
	// First, evaluate potential value before generating hypothesis
	potentialValue := ki.evaluateHypothesisPotential(conceptName, conceptDef, domain)

	// Skip low-value hypotheses (threshold: 0.3)
	if potentialValue < 0.3 {
		log.Printf("⏭️ Skipping low-value hypothesis for concept: %s (value: %.2f < 0.3)", conceptName, potentialValue)
		return nil
	}

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

	// Scale confidence by potential value
	confidence = confidence * potentialValue

	// Estimate uncertainties for hypothesis
	// Hypotheses start with higher epistemic uncertainty (we don't know if they're true yet)
	epistemicUncertainty := EstimateEpistemicUncertainty(1, false, false) // 1 fact, no definition/examples yet
	aleatoricUncertainty := EstimateAleatoricUncertainty(domain, "")

	// Create uncertainty model
	uncertainty := NewUncertaintyModel(confidence, epistemicUncertainty, aleatoricUncertainty)

	return &Hypothesis{
		ID:          fmt.Sprintf("hyp_concept_%d_%d", time.Now().UnixNano(), index),
		Description: hypothesisDesc,
		Domain:      domain,
		Confidence:  uncertainty.CalibratedConfidence, // Use calibrated confidence
		Status:      "proposed",
		Facts:       []string{fmt.Sprintf("concept_%s", conceptName)},
		Constraints: []string{},
		CreatedAt:   time.Now(),
		Uncertainty: uncertainty,
	}
}

// evaluateHypothesisPotential evaluates the potential value of a hypothesis before generating it
func (ki *KnowledgeIntegration) evaluateHypothesisPotential(conceptName, conceptDef, domain string) float64 {
	value := 0.5 // Base value

	// Check if similar hypotheses succeeded
	similarSuccessRate := ki.getSimilarHypothesisSuccessRate(conceptName, domain)
	value += similarSuccessRate * 0.3

	// Check concept depth/completeness
	conceptDepth := ki.assessConceptDepth(conceptName, domain)
	value += conceptDepth * 0.2

	// Check if concept has actionable properties
	if ki.hasActionableProperties(conceptName, domain) {
		value += 0.2
	}

	// Penalize very generic concepts
	lowerDef := strings.ToLower(conceptDef)
	genericTerms := []string{"thing", "stuff", "item", "object", "concept", "idea", "notion"}
	for _, term := range genericTerms {
		if strings.Contains(lowerDef, term) && len(conceptDef) < 50 {
			value -= 0.2
			break
		}
	}

	// Ensure value stays in valid range [0, 1]
	if value > 1.0 {
		value = 1.0
	}
	if value < 0.0 {
		value = 0.0
	}

	return value
}

// getSimilarHypothesisSuccessRate retrieves success rate for similar hypotheses
func (ki *KnowledgeIntegration) getSimilarHypothesisSuccessRate(conceptName, domain string) float64 {
	// Check Redis for hypothesis success rates by concept
	key := fmt.Sprintf("hypothesis_success_rate:%s:%s", domain, conceptName)
	rateStr, err := ki.redis.Get(ki.ctx, key).Result()
	if err != nil {
		// Check for domain-level success rate
		domainKey := fmt.Sprintf("hypothesis_success_rate:%s", domain)
		domainRateStr, err := ki.redis.Get(ki.ctx, domainKey).Result()
		if err != nil {
			return 0.0 // No data available
		}
		if rate, err := strconv.ParseFloat(domainRateStr, 64); err == nil {
			return rate
		}
		return 0.0
	}
	if rate, err := strconv.ParseFloat(rateStr, 64); err == nil {
		return rate
	}
	return 0.0
}

// assessConceptDepth assesses how deep/complete a concept is
func (ki *KnowledgeIntegration) assessConceptDepth(conceptName, domain string) float64 {
	depth := 0.0

	// Get concept details from knowledge base
	concepts, err := ki.getDomainConcepts(domain)
	if err != nil {
		return 0.0
	}

	for _, concept := range concepts {
		name, _ := concept["name"].(string)
		if name == conceptName {
			// Check for definition length (longer = more complete)
			if def, ok := concept["definition"].(string); ok {
				if len(def) > 100 {
					depth += 0.3
				} else if len(def) > 50 {
					depth += 0.2
				} else if len(def) > 20 {
					depth += 0.1
				}
			}

			// Check for properties
			if props, ok := concept["properties"].([]interface{}); ok && len(props) > 0 {
				depth += 0.2
			}

			// Check for constraints
			if constraints, ok := concept["constraints"].([]interface{}); ok && len(constraints) > 0 {
				depth += 0.2
			}

			// Check for examples
			if examples, ok := concept["examples"].([]interface{}); ok && len(examples) > 0 {
				depth += 0.2
			}

			// Check for relationships
			if relations, ok := concept["relations"].([]interface{}); ok && len(relations) > 0 {
				depth += 0.1
			}

			break
		}
	}

	// Ensure depth stays in valid range [0, 1]
	if depth > 1.0 {
		depth = 1.0
	}
	return depth
}

// hasActionableProperties checks if a concept has actionable properties
func (ki *KnowledgeIntegration) hasActionableProperties(conceptName, domain string) bool {
	concepts, err := ki.getDomainConcepts(domain)
	if err != nil {
		return false
	}

	for _, concept := range concepts {
		name, _ := concept["name"].(string)
		if name == conceptName {
			// Check definition for actionable keywords
			if def, ok := concept["definition"].(string); ok {
				lowerDef := strings.ToLower(def)
				actionableTerms := []string{"can", "able", "capable", "enable", "allow", "support", "provide", "implement", "execute", "perform"}
				for _, term := range actionableTerms {
					if strings.Contains(lowerDef, term) {
						return true
					}
				}
			}

			// Check for properties that suggest actionability
			if props, ok := concept["properties"].([]interface{}); ok {
				for _, prop := range props {
					if propStr, ok := prop.(string); ok {
						lowerProp := strings.ToLower(propStr)
						if strings.Contains(lowerProp, "action") || strings.Contains(lowerProp, "function") {
							return true
						}
					}
				}
			}
			break
		}
	}

	return false
}

// enhanceConceptsWithRelated enhances concepts with related concepts using MCP
func (ki *KnowledgeIntegration) enhanceConceptsWithRelated(concepts []map[string]interface{}, domain string) []map[string]interface{} {
	enhanced := make([]map[string]interface{}, len(concepts))

	for i, concept := range concepts {
		enhanced[i] = concept
		name, _ := concept["name"].(string)
		if name == "" {
			continue
		}

		// Use MCP to find related concepts
		result, err := ki.callMCPTool("find_related_concepts", map[string]interface{}{
			"concept_name": name,
			"max_depth":    1, // Just immediate relationships for learning context
		})
		if err != nil {
			// MCP call failed, keep concept as-is
			continue
		}

		// Parse related concepts from result
		resultMap, ok := result.(map[string]interface{})
		if !ok {
			continue
		}

		results, ok := resultMap["results"].([]interface{})
		if !ok {
			continue
		}

		// Extract related concept names
		var relatedNames []string
		for _, r := range results {
			row, ok := r.(map[string]interface{})
			if !ok {
				continue
			}

			var relatedConcept map[string]interface{}
			if c, ok := row["related"].(map[string]interface{}); ok {
				relatedConcept = c
			} else if c, ok := row["c"].(map[string]interface{}); ok {
				relatedConcept = c
			} else {
				relatedConcept = row
			}

			if relName, ok := relatedConcept["name"].(string); ok && relName != "" {
				relatedNames = append(relatedNames, relName)
			}
		}

		// Add related concepts to the concept's properties
		if len(relatedNames) > 0 {
			if props, ok := enhanced[i]["properties"].(map[string]interface{}); ok {
				props["related_concepts"] = relatedNames
			} else {
				enhanced[i]["properties"] = map[string]interface{}{
					"related_concepts": relatedNames,
				}
			}
			log.Printf("🔗 Enhanced concept %s with %d related concepts via MCP", name, len(relatedNames))
		}
	}

	return enhanced
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

				// Create uncertainty model for relationship-based hypothesis
				epistemicUncertainty := EstimateEpistemicUncertainty(2, false, false) // 2 concepts
				aleatoricUncertainty := EstimateAleatoricUncertainty(domain, "")
				uncertainty := NewUncertaintyModel(0.6, epistemicUncertainty, aleatoricUncertainty)

				hypothesis := Hypothesis{
					ID:          fmt.Sprintf("hyp_rel_%d_%d_%d", time.Now().UnixNano(), i, j),
					Description: fmt.Sprintf("If we combine %s and %s, we can create new %s capabilities", name1, name2, domain),
					Domain:      domain,
					Confidence:  uncertainty.CalibratedConfidence,
					Status:      "proposed",
					Facts:       []string{fmt.Sprintf("concept_%s", name1), fmt.Sprintf("concept_%s", name2)},
					Constraints: []string{},
					CreatedAt:   time.Now(),
					Uncertainty: uncertainty,
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

	// Create uncertainty model for fact-based hypothesis
	epistemicUncertainty := EstimateEpistemicUncertainty(1, false, false) // 1 fact, no definition/examples yet
	aleatoricUncertainty := EstimateAleatoricUncertainty(domain, "")
	uncertainty := NewUncertaintyModel(confidence, epistemicUncertainty, aleatoricUncertainty)

	return &Hypothesis{
		ID:          fmt.Sprintf("hyp_fact_%d_%d", time.Now().UnixNano(), index),
		Description: hypothesisDesc,
		Domain:      domain,
		Confidence:  uncertainty.CalibratedConfidence, // Use calibrated confidence
		Status:      "proposed",
		Facts:       []string{fact.ID},
		Constraints: fact.Constraints,
		CreatedAt:   time.Now(),
		Uncertainty: uncertainty,
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

// callMCPTool calls an MCP tool and returns the result
func (ki *KnowledgeIntegration) callMCPTool(toolName string, arguments map[string]interface{}) (interface{}, error) {
	reqBody := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name":      toolName,
			"arguments": arguments,
		},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal MCP request: %w", err)
	}

	// Use async HTTP client (or sync fallback)
	resp, err := Post(ki.ctx, ki.mcpEndpoint, "application/json", jsonData, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to call MCP server: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("MCP server returned status %d", resp.StatusCode)
	}

	var mcpResponse struct {
		JSONRPC string      `json:"jsonrpc"`
		ID      int         `json:"id"`
		Result  interface{} `json:"result,omitempty"`
		Error   *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error,omitempty"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&mcpResponse); err != nil {
		return nil, fmt.Errorf("failed to decode MCP response: %w", err)
	}

	if mcpResponse.Error != nil {
		return nil, fmt.Errorf("MCP error: %s", mcpResponse.Error.Message)
	}

	return mcpResponse.Result, nil
}

// getDomainConcepts gets domain concepts using MCP query_neo4j tool
func (ki *KnowledgeIntegration) getDomainConcepts(domain string) ([]map[string]interface{}, error) {
	// Try MCP first, fallback to direct API
	cypherQuery := fmt.Sprintf("MATCH (c:Concept {domain: '%s'}) RETURN c LIMIT 50", domain)

	result, err := ki.callMCPTool("query_neo4j", map[string]interface{}{
		"query": cypherQuery,
	})
	if err != nil {
		log.Printf("⚠️ MCP query failed, falling back to direct API: %v", err)
		// Fallback to direct API
		return ki.getDomainConceptsDirect(domain)
	}

	// Parse MCP result
	resultMap, ok := result.(map[string]interface{})
	if !ok {
		return ki.getDomainConceptsDirect(domain)
	}

	results, ok := resultMap["results"].([]interface{})
	if !ok {
		return ki.getDomainConceptsDirect(domain)
	}

	var concepts []map[string]interface{}
	for _, r := range results {
		row, ok := r.(map[string]interface{})
		if !ok {
			continue
		}
		// Extract concept from Cypher result format
		if c, ok := row["c"].(map[string]interface{}); ok {
			concepts = append(concepts, c)
		} else if c, ok := row["c"].(map[string]interface{}); ok {
			// Handle different result formats
			concepts = append(concepts, c)
		}
	}

	if len(concepts) > 0 {
		log.Printf("✅ Retrieved %d concepts via MCP", len(concepts))
		return concepts, nil
	}

	// Fallback if no results
	return ki.getDomainConceptsDirect(domain)
}

// getDomainConceptsDirect gets domain concepts via direct API (fallback)
func (ki *KnowledgeIntegration) getDomainConceptsDirect(domain string) ([]map[string]interface{}, error) {
	encodedDomain := url.QueryEscape(domain)
	searchURL := fmt.Sprintf("%s/api/v1/knowledge/search?domain=%s&limit=50", ki.hdnURL, encodedDomain)

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

// assessKnowledgeValue uses LLM to assess if knowledge is novel and worth learning
// Returns: (isNovel, isWorthLearning, error)
func (ki *KnowledgeIntegration) assessKnowledgeValue(knowledge string, domain string) (bool, bool, error) {
	if len(strings.TrimSpace(knowledge)) == 0 {
		return false, false, nil
	}

	// Get existing knowledge context
	existingConcepts, err := ki.getDomainConcepts(domain)
	if err != nil {
		log.Printf("⚠️ Failed to get domain concepts for assessment: %v", err)
		existingConcepts = []map[string]interface{}{}
	}

	// Build context of existing knowledge
	conceptNames := []string{}
	for _, c := range existingConcepts {
		if name, ok := c["name"].(string); ok {
			conceptNames = append(conceptNames, name)
		}
	}
	existingContext := strings.Join(conceptNames, ", ")
	if existingContext == "" {
		existingContext = "No existing concepts in this domain"
	}

	// Create prompt for LLM assessment
	prompt := fmt.Sprintf(`Assess whether the following knowledge is worth learning and storing.

Knowledge to assess: %s
Domain: %s
Existing knowledge in domain: %s

Evaluate:
1. NOVELTY: Is this knowledge new/novel, or is it already obvious/known?
   - Consider if this is common knowledge that everyone knows
   - Consider if this is already covered by existing knowledge
   - Consider if this is just restating something obvious

2. VALUE: Is this knowledge worth storing?
   - Will this help accomplish tasks or solve problems?
   - Is this actionable and useful?
   - Is this specific enough to be valuable?
   - Will this help with future learning or decision-making?

Return JSON:
{
  "is_novel": true/false,
  "is_worth_learning": true/false,
  "reasoning": "Brief explanation of why",
  "novelty_score": 0.0-1.0,
  "value_score": 0.0-1.0
}

Be strict: Only mark as novel and worth learning if:
- The knowledge is genuinely new or adds meaningful detail
- The knowledge is actionable and useful
- The knowledge is not obvious/common knowledge
- The knowledge is not already covered by existing knowledge

If the knowledge is obvious, common knowledge, or already known, mark is_novel=false.
If the knowledge is not actionable or useful, mark is_worth_learning=false.`, knowledge, domain, existingContext)

	// Call HDN interpret endpoint
	hdnURL := strings.TrimSuffix(ki.hdnURL, "/")
	if hdnURL == "" {
		hdnURL = "http://localhost:8081"
	}

	interpretURL := fmt.Sprintf("%s/api/v1/interpret", hdnURL)
	reqData := map[string]interface{}{
		"input": prompt, // API expects "input" not "text"
		"context": map[string]string{
			"origin": "fsm", // Mark as background task for LOW priority
		},
	}

	reqJSON, err := json.Marshal(reqData)
	if err != nil {
		return false, false, fmt.Errorf("failed to marshal assessment request: %w", err)
	}

	// Rate limiting: Add delay between LLM requests to prevent GPU overload
	// Default: 5 seconds, configurable via FSM_LLM_REQUEST_DELAY_MS
	// Note: With async queue, this delay is less critical but still useful for burst control
	delayMs := 5000
	if v := strings.TrimSpace(os.Getenv("FSM_LLM_REQUEST_DELAY_MS")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			delayMs = n
		}
	}
	if delayMs > 0 {
		time.Sleep(time.Duration(delayMs) * time.Millisecond)
	}

	// Use async HTTP client (or sync fallback)
	resp, err := Post(ki.ctx, interpretURL, "application/json", reqJSON, nil)
	if err != nil {
		return false, false, fmt.Errorf("knowledge assessment LLM call failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false, false, fmt.Errorf("knowledge assessment returned status %d", resp.StatusCode)
	}

	var interpretResp map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&interpretResp); err != nil {
		return false, false, fmt.Errorf("failed to decode assessment response: %w", err)
	}

	// Extract assessment from response
	assessmentJSON, ok := interpretResp["result"].(string)
	if !ok {
		if result, ok := interpretResp["output"].(string); ok {
			assessmentJSON = result
		} else {
			return false, false, fmt.Errorf("no result in assessment response")
		}
	}

	// Parse assessment JSON
	var assessment map[string]interface{}
	if err := json.Unmarshal([]byte(assessmentJSON), &assessment); err != nil {
		// Try to extract JSON from text response
		start := strings.Index(assessmentJSON, "{")
		end := strings.LastIndex(assessmentJSON, "}")
		if start >= 0 && end > start {
			assessmentJSON = assessmentJSON[start : end+1]
			if err := json.Unmarshal([]byte(assessmentJSON), &assessment); err != nil {
				return false, false, fmt.Errorf("failed to parse assessment JSON: %w", err)
			}
		} else {
			return false, false, fmt.Errorf("no JSON found in assessment response")
		}
	}

	// Extract values
	isNovel, _ := assessment["is_novel"].(bool)
	isWorthLearning, _ := assessment["is_worth_learning"].(bool)

	// Also check scores as fallback
	if noveltyScore, ok := assessment["novelty_score"].(float64); ok && noveltyScore > 0.5 {
		isNovel = true
	}
	if valueScore, ok := assessment["value_score"].(float64); ok && valueScore > 0.5 {
		isWorthLearning = true
	}

	reasoning, _ := assessment["reasoning"].(string)
	if reasoning != "" {
		log.Printf("🧠 Knowledge assessment: novel=%v, worth_learning=%v, reasoning=%s", isNovel, isWorthLearning, reasoning)
	}

	return isNovel, isWorthLearning, nil
}

// knowledgeAlreadyExists checks if knowledge already exists in the knowledge base using MCP
func (ki *KnowledgeIntegration) knowledgeAlreadyExists(knowledge string, domain string) (bool, error) {
	if len(strings.TrimSpace(knowledge)) == 0 {
		return false, nil
	}

	// Extract key terms from knowledge for search
	keyTerms := ki.extractKeyTerms(knowledge)
	if len(keyTerms) == 0 {
		return false, nil
	}

	// Try using MCP get_concept tool for each key term
	for _, term := range keyTerms {
		if len(term) < 3 {
			continue
		}

		// Use MCP get_concept tool
		result, err := ki.callMCPTool("get_concept", map[string]interface{}{
			"name":   term,
			"domain": domain,
		})
		if err != nil {
			// Tool call failed, continue to next term
			continue
		}

		// Parse result
		resultMap, ok := result.(map[string]interface{})
		if !ok {
			continue
		}

		results, ok := resultMap["results"].([]interface{})
		if !ok {
			continue
		}

		// Check if any concept matches
		knowledgeLower := strings.ToLower(knowledge)
		for _, r := range results {
			row, ok := r.(map[string]interface{})
			if !ok {
				continue
			}

			// Extract concept from result
			var concept map[string]interface{}
			if c, ok := row["c"].(map[string]interface{}); ok {
				concept = c
			} else if c, ok := row["concept"].(map[string]interface{}); ok {
				concept = c
			} else {
				concept = row
			}

			conceptName, _ := concept["name"].(string)
			conceptDef, _ := concept["definition"].(string)

			// Check similarity
			if conceptName != "" && strings.Contains(knowledgeLower, strings.ToLower(conceptName)) {
				log.Printf("🔍 Found existing concept via MCP: %s", conceptName)
				return true, nil
			}
			if conceptDef != "" {
				defLen := len(conceptDef)
				if defLen > 100 {
					defLen = 100
				}
				if strings.Contains(knowledgeLower, strings.ToLower(conceptDef[:defLen])) {
					log.Printf("🔍 Found existing concept definition via MCP: %s", conceptName)
					return true, nil
				}
			}
		}
	}

	// Fallback: Use Cypher query via MCP for broader search
	cypherQuery := fmt.Sprintf(
		"MATCH (c:Concept) WHERE toLower(c.name) CONTAINS toLower('%s') OR toLower(c.definition) CONTAINS toLower('%s') RETURN c LIMIT 10",
		keyTerms[0], keyTerms[0],
	)

	result, err := ki.callMCPTool("query_neo4j", map[string]interface{}{
		"query": cypherQuery,
	})
	if err == nil {
		resultMap, ok := result.(map[string]interface{})
		if ok {
			if results, ok := resultMap["results"].([]interface{}); ok && len(results) > 0 {
				log.Printf("🔍 Found existing knowledge via MCP Cypher query")
				return true, nil
			}
		}
	}

	return false, nil
}

// extractKeyTerms extracts key terms from knowledge for searching
func (ki *KnowledgeIntegration) extractKeyTerms(knowledge string) []string {
	// Simple extraction - remove common words and get meaningful terms
	words := strings.Fields(strings.ToLower(knowledge))
	commonWords := map[string]bool{
		"the": true, "a": true, "an": true, "is": true, "are": true,
		"was": true, "were": true, "be": true, "been": true, "being": true,
		"have": true, "has": true, "had": true, "do": true, "does": true,
		"did": true, "will": true, "would": true, "should": true, "could": true,
		"can": true, "may": true, "might": true, "must": true, "this": true,
		"that": true, "these": true, "those": true, "it": true, "its": true,
		"they": true, "them": true, "their": true, "we": true, "our": true,
		"you": true, "your": true, "i": true, "my": true, "me": true,
		"to": true, "of": true, "in": true, "on": true, "at": true,
		"for": true, "with": true, "by": true, "from": true, "as": true,
		"and": true, "or": true, "but": true, "if": true, "then": true,
	}

	var keyTerms []string
	for _, word := range words {
		// Remove punctuation
		word = strings.Trim(word, ".,!?;:()[]{}")
		if len(word) > 3 && !commonWords[word] {
			keyTerms = append(keyTerms, word)
		}
	}

	// Return up to 5 key terms
	if len(keyTerms) > 5 {
		return keyTerms[:5]
	}
	return keyTerms
}

// evaluateFactHypothesisPotential evaluates the potential value of a fact-based hypothesis
func (ki *KnowledgeIntegration) evaluateFactHypothesisPotential(fact Fact, domain string) float64 {
	value := 0.5 // Base value

	// Higher confidence facts lead to higher value hypotheses
	value += fact.Confidence * 0.3

	// Check if similar fact-based hypotheses succeeded
	similarSuccessRate := ki.getSimilarHypothesisSuccessRate("fact", domain)
	value += similarSuccessRate * 0.2

	// Check fact content for actionable keywords
	lowerContent := strings.ToLower(fact.Content)
	actionableTerms := []string{"working", "functioning", "improve", "enhance", "optimize", "implement"}
	for _, term := range actionableTerms {
		if strings.Contains(lowerContent, term) {
			value += 0.1
			break
		}
	}

	// Ensure value stays in valid range [0, 1]
	if value > 1.0 {
		value = 1.0
	}
	if value < 0.0 {
		value = 0.0
	}

	return value
}

// enrichHypothesisWithCausalReasoning classifies and enriches a hypothesis with causal reasoning signals
func (ki *KnowledgeIntegration) enrichHypothesisWithCausalReasoning(hypothesis Hypothesis, domain string) Hypothesis {
	// Always log when this function is called for debugging
	log.Printf("🔬 [CAUSAL-DEBUG] Enriching hypothesis: %s", hypothesis.Description[:min(80, len(hypothesis.Description))])

	// Classify the hypothesis as causal vs correlation
	causalType := ki.classifyCausalType(hypothesis.Description, domain)
	hypothesis.CausalType = causalType

	// Generate counterfactual reasoning actions
	hypothesis.CounterfactualActions = ki.generateCounterfactualActions(hypothesis, domain)

	// Generate intervention-style goals for experimental testing
	hypothesis.InterventionGoals = ki.generateInterventionGoals(hypothesis, domain)

	log.Printf("🔬 [CAUSAL] Hypothesis '%s' classified as: %s (counterfactuals: %d, interventions: %d)",
		hypothesis.Description[:min(60, len(hypothesis.Description))],
		causalType,
		len(hypothesis.CounterfactualActions),
		len(hypothesis.InterventionGoals))

	return hypothesis
}

// classifyCausalType determines if a hypothesis represents a causal relationship or just correlation
// Returns: "observational_relation", "inferred_causal_candidate", or "experimentally_testable_relation"
func (ki *KnowledgeIntegration) classifyCausalType(description, domain string) string {
	descLower := strings.ToLower(description)

	// Patterns that suggest causal relationships
	causalIndicators := []string{
		"if we", "if we apply", "if we optimize", "if we enhance", "if we build",
		"causes", "leads to", "results in", "produces", "triggers", "enables",
		"affects", "influences", "determines", "controls", "drives",
		"when we", "by doing", "through", "by means of",
	}

	// Patterns that suggest experimental testability
	experimentalIndicators := []string{
		"test", "experiment", "trial", "measure", "verify", "validate",
		"compare", "contrast", "control", "intervention", "manipulate",
	}

	// Patterns that suggest only observational correlation
	observationalIndicators := []string{
		"related to", "associated with", "correlated with", "linked to",
		"co-occurs", "appears with", "found together",
	}

	// Count indicators
	causalCount := 0
	experimentalCount := 0
	observationalCount := 0

	for _, indicator := range causalIndicators {
		if strings.Contains(descLower, indicator) {
			causalCount++
		}
	}

	for _, indicator := range experimentalIndicators {
		if strings.Contains(descLower, indicator) {
			experimentalCount++
		}
	}

	for _, indicator := range observationalIndicators {
		if strings.Contains(descLower, indicator) {
			observationalCount++
		}
	}

	// Classification logic
	// If it has experimental indicators and causal indicators, it's experimentally testable
	if experimentalCount > 0 && causalCount > 0 {
		return "experimentally_testable_relation"
	}

	// If it has causal indicators but no experimental ones, it's an inferred causal candidate
	if causalCount > 0 {
		return "inferred_causal_candidate"
	}

	// If it only has observational indicators, it's observational
	if observationalCount > 0 {
		return "observational_relation"
	}

	// Default: if it has "if" statements suggesting action, treat as causal candidate
	if strings.Contains(descLower, "if ") && (strings.Contains(descLower, "can") || strings.Contains(descLower, "will")) {
		return "inferred_causal_candidate"
	}

	// Otherwise, default to observational (most conservative)
	return "observational_relation"
}

// generateCounterfactualActions creates actions for counterfactual reasoning
// These answer: "what outcome would change my belief?"
func (ki *KnowledgeIntegration) generateCounterfactualActions(hypothesis Hypothesis, domain string) []string {
	var actions []string
	descLower := strings.ToLower(hypothesis.Description)

	// Extract key entities/concepts from the hypothesis
	// Look for patterns like "If we [action] [entity], we can [outcome]"
	// Generate counterfactual questions about what would change the belief

	// Base counterfactual actions
	actions = append(actions, fmt.Sprintf("What outcome would refute the hypothesis: %s?", hypothesis.Description))
	actions = append(actions, fmt.Sprintf("What evidence would change my confidence in: %s?", hypothesis.Description))

	// Domain-specific counterfactuals
	if strings.Contains(descLower, "improve") || strings.Contains(descLower, "enhance") {
		actions = append(actions, fmt.Sprintf("What would happen if we did NOT apply this approach to %s?", domain))
		actions = append(actions, "What alternative explanations could account for the observed pattern?")
	}

	if strings.Contains(descLower, "optimize") || strings.Contains(descLower, "system") {
		actions = append(actions, "What would the outcome be if we changed a different variable?")
		actions = append(actions, "What if the relationship is reversed (reverse causality)?")
	}

	if strings.Contains(descLower, "combine") || strings.Contains(descLower, "integrate") {
		actions = append(actions, "What would happen if we tested each component separately?")
		actions = append(actions, "What if the interaction effect is different than expected?")
	}

	// For causal candidates, add more specific counterfactuals
	if hypothesis.CausalType == "inferred_causal_candidate" || hypothesis.CausalType == "experimentally_testable_relation" {
		actions = append(actions, "What would happen if we removed the proposed cause?")
		actions = append(actions, "What if a confounding variable explains the relationship?")
		actions = append(actions, "What outcome would demonstrate this is correlation, not causation?")
	}

	return actions
}

// generateInterventionGoals creates goals for intervention-style experiments
// These answer: "design an experiment to test this hypothesis"
func (ki *KnowledgeIntegration) generateInterventionGoals(hypothesis Hypothesis, domain string) []string {
	var goals []string
	descLower := strings.ToLower(hypothesis.Description)

	// Base intervention goal
	goals = append(goals, fmt.Sprintf("Design a controlled experiment to test: %s", hypothesis.Description))

	// For experimentally testable relations, create specific intervention goals
	if hypothesis.CausalType == "experimentally_testable_relation" {
		goals = append(goals, fmt.Sprintf("Create intervention: manipulate key variables in %s to test causal relationship", domain))
		goals = append(goals, "Set up control and treatment groups to isolate causal effects")
		goals = append(goals, "Measure outcomes before and after intervention to assess causality")
	}

	// For inferred causal candidates, create goals to make them testable
	if hypothesis.CausalType == "inferred_causal_candidate" {
		goals = append(goals, fmt.Sprintf("Design experiment to test if %s causes the proposed outcome", hypothesis.Description))
		goals = append(goals, "Identify variables to manipulate and outcomes to measure")
		goals = append(goals, "Create testable predictions from the causal hypothesis")
	}

	// Domain-specific intervention goals
	if strings.Contains(descLower, "improve") || strings.Contains(descLower, "enhance") {
		goals = append(goals, fmt.Sprintf("Test intervention: apply proposed improvement to %s and measure results", domain))
		goals = append(goals, "Compare baseline performance with intervention performance")
	}

	if strings.Contains(descLower, "optimize") || strings.Contains(descLower, "system") {
		goals = append(goals, "A/B test: compare optimized system with current system")
		goals = append(goals, "Measure system performance metrics before and after optimization")
	}

	if strings.Contains(descLower, "combine") || strings.Contains(descLower, "integrate") {
		goals = append(goals, "Test integration: measure outcomes when components are combined vs separate")
		goals = append(goals, "Isolate interaction effects through factorial design")
	}

	// For observational relations, create goals to move toward causal testing
	if hypothesis.CausalType == "observational_relation" {
		goals = append(goals, fmt.Sprintf("Design study to test if observed correlation in %s is causal", domain))
		goals = append(goals, "Identify potential confounders and control for them")
		goals = append(goals, "Create experimental design to move from correlation to causation")
	}

	return goals
}
