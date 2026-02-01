package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// KnowledgeGrowthEngine handles growing the knowledge base
type KnowledgeGrowthEngine struct {
	hdnURL      string
	mcpEndpoint string // MCP server endpoint
	redis       *redis.Client
	ctx         context.Context
	httpClient  *http.Client
}

// NewKnowledgeGrowthEngine creates a new knowledge growth engine
func NewKnowledgeGrowthEngine(hdnURL string, redis *redis.Client) *KnowledgeGrowthEngine {
	mcpEndpoint := hdnURL + "/mcp"
	if hdnURL == "" {
		mcpEndpoint = "http://localhost:8081/mcp"
	}
	return &KnowledgeGrowthEngine{
		hdnURL:      hdnURL,
		mcpEndpoint: mcpEndpoint,
		redis:       redis,
		ctx:         context.Background(),
		httpClient:  &http.Client{Timeout: 10 * time.Second},
	}
}

// ConceptDiscovery represents a discovered concept
type ConceptDiscovery struct {
	Name        string            `json:"name"`
	Domain      string            `json:"domain"`
	Definition  string            `json:"definition"`
	Confidence  float64           `json:"confidence"`
	Source      string            `json:"source"`
	Properties  []string          `json:"properties"`
	Constraints []string          `json:"constraints"`
	Examples    []Example         `json:"examples"`
	Relations   []ConceptRelation `json:"relations"`
	CreatedAt   time.Time         `json:"created_at"`
}

type Example struct {
	Input  string `json:"input"`
	Output string `json:"output"`
	Type   string `json:"type"`
}

type ConceptRelation struct {
	TargetConcept string                 `json:"target_concept"`
	RelationType  string                 `json:"relation_type"`
	Properties    map[string]interface{} `json:"properties"`
	Confidence    float64                `json:"confidence"`
}

// KnowledgeGap represents a gap in the knowledge base
type KnowledgeGap struct {
	Type        string   `json:"type"` // missing_concept, missing_relation, missing_constraint
	Description string   `json:"description"`
	Domain      string   `json:"domain"`
	Priority    int      `json:"priority"` // 1-10, higher is more important
	Suggestions []string `json:"suggestions"`
}

// DiscoverNewConcepts discovers new concepts from episodes and facts
func (kge *KnowledgeGrowthEngine) DiscoverNewConcepts(episodes []map[string]interface{}, domain string) ([]ConceptDiscovery, error) {
	log.Printf("🔍 Discovering new concepts in domain: %s", domain)

	var discoveries []ConceptDiscovery

	// Analyze episodes for potential new concepts
	for _, episode := range episodes {
		// Extract text content for analysis
		text := kge.extractTextFromEpisode(episode)
		if text == "" {
			continue
		}

		// Look for domain-specific patterns
		concepts := kge.extractConceptsFromText(text, domain)
		discoveries = append(discoveries, concepts...)
	}

	// Remove duplicates and filter by confidence threshold (0.6 - balanced to allow useful concepts)
	discoveries = kge.deduplicateConcepts(discoveries)
	discoveries = kge.filterByConfidence(discoveries, 0.6)

	log.Printf("📚 Discovered %d new concepts", len(discoveries))
	return discoveries, nil
}

// FindKnowledgeGaps identifies gaps in the knowledge base
func (kge *KnowledgeGrowthEngine) FindKnowledgeGaps(domain string) ([]KnowledgeGap, error) {
	log.Printf("🔍 Finding knowledge gaps in domain: %s", domain)

	var gaps []KnowledgeGap

	// Get existing concepts for the domain
	existingConcepts, err := kge.getDomainConcepts(domain)
	if err != nil {
		return nil, fmt.Errorf("failed to get existing concepts: %w", err)
	}

	// Look for missing relationships
	relationGaps := kge.findMissingRelations(existingConcepts, domain)
	gaps = append(gaps, relationGaps...)

	// Look for missing constraints
	constraintGaps := kge.findMissingConstraints(existingConcepts, domain)
	gaps = append(gaps, constraintGaps...)

	// Look for missing examples
	exampleGaps := kge.findMissingExamples(existingConcepts, domain)
	gaps = append(gaps, exampleGaps...)

	log.Printf("🕳️ Found %d knowledge gaps", len(gaps))
	return gaps, nil
}

// GrowKnowledgeBase actively grows the knowledge base
func (kge *KnowledgeGrowthEngine) GrowKnowledgeBase(episodes []map[string]interface{}, domain string) error {
	log.Printf("🌱 Growing knowledge base for domain: %s (with %d episodes)", domain, len(episodes))

	// 1. Discover new concepts with higher confidence threshold
	discoveries, err := kge.DiscoverNewConcepts(episodes, domain)
	if err != nil {
		return fmt.Errorf("failed to discover concepts: %w", err)
	}
	log.Printf("🔍 Initial discoveries: %d concepts before filtering", len(discoveries))

	// 2. Create new concepts in the knowledge base (with enhanced quality gate + novelty checking)
	var createdCount, skippedCount int
	for _, discovery := range discoveries {
		// Additional quality check: require minimum definition length
		if len(strings.TrimSpace(discovery.Definition)) < 20 {
			log.Printf("⚠️ Skipping concept %s: definition too short (%d chars)",
				discovery.Name, len(discovery.Definition))
			skippedCount++
			continue
		}

		// Avoid generic/meaningless concept names
		if kge.isGenericConceptName(discovery.Name) {
			log.Printf("⚠️ Skipping concept %s: name too generic", discovery.Name)
			skippedCount++
			continue
		}

		// Check if concept already exists and if it's a stub (needs definition)
		existingConcept, err := kge.getExistingConcept(discovery.Name, domain)
		if err != nil {
			log.Printf("⚠️ Failed to check if concept exists, defaulting to create: %v", err)
		}

		if existingConcept != nil {
			// If it exists but has a definition, skip it
			existingDef, _ := existingConcept["definition"].(string)
			if len(strings.TrimSpace(existingDef)) > 20 {
				log.Printf("⏭️ Skipping concept %s: already exists with detailed definition", discovery.Name)
				skippedCount++
				continue
			}
			log.Printf("📥 Concept %s exists as a stub; will attempt to update with detailed definition", discovery.Name)
		}

		// Assess if concept is novel and worth learning
		conceptKnowledge := fmt.Sprintf("%s: %s", discovery.Name, discovery.Definition)
		isNovel, isWorthLearning, err := kge.assessConceptValue(conceptKnowledge, domain)
		if err != nil {
			log.Printf("⚠️ Failed to assess concept value, defaulting to create: %v", err)
			// Default to creating if assessment fails
			isNovel = true
			isWorthLearning = true
		}

		if !isNovel || !isWorthLearning {
			if !isNovel {
				log.Printf("⏭️ Skipping concept %s: not novel/obvious", discovery.Name)
			} else {
				log.Printf("⏭️ Skipping concept %s: not worth learning", discovery.Name)
			}
			skippedCount++
			continue
		}

		if err := kge.createConcept(discovery); err != nil {
			log.Printf("Warning: Failed to create concept %s: %v", discovery.Name, err)
			continue
		}
		log.Printf("✅ Created new concept: %s (confidence: %.2f, source: %s)",
			discovery.Name, discovery.Confidence, discovery.Source)
		createdCount++
	}
	log.Printf("📊 Knowledge growth stats: %d concepts created, %d skipped due to quality checks",
		createdCount, skippedCount)

	// 3. Find and fill knowledge gaps
	gaps, err := kge.FindKnowledgeGaps(domain)
	if err != nil {
		return fmt.Errorf("failed to find gaps: %w", err)
	}

	// 4. Fill high-priority gaps
	for _, gap := range gaps {
		if gap.Priority >= 7 { // High priority gaps
			if err := kge.fillKnowledgeGap(gap, domain); err != nil {
				log.Printf("Warning: Failed to fill gap %s: %v", gap.Description, err)
				continue
			}
			log.Printf("✅ Filled knowledge gap: %s", gap.Description)
		}
	}

	// 5. Update existing concepts with new information
	if err := kge.updateExistingConcepts(episodes, domain); err != nil {
		log.Printf("Warning: Failed to update existing concepts: %v", err)
	}

	log.Printf("🌱 Knowledge base growth completed for domain: %s", domain)
	return nil
}

// ValidateKnowledgeConsistency checks for contradictions and conflicts
func (kge *KnowledgeGrowthEngine) ValidateKnowledgeConsistency(domain string) error {
	log.Printf("🔍 Validating knowledge consistency for domain: %s", domain)

	// Get all concepts for the domain
	concepts, err := kge.getDomainConcepts(domain)
	if err != nil {
		return fmt.Errorf("failed to get concepts: %w", err)
	}

	// Check for contradictions
	contradictions := kge.findContradictions(concepts)
	if len(contradictions) > 0 {
		log.Printf("⚠️ Found %d contradictions in knowledge base", len(contradictions))
		// Resolve contradictions
		for _, contradiction := range contradictions {
			if err := kge.resolveContradiction(contradiction); err != nil {
				log.Printf("Warning: Failed to resolve contradiction: %v", err)
			}
		}
	}

	// Check for missing relationships
	missingRelations := kge.findMissingRelations(concepts, domain)
	if len(missingRelations) > 0 {
		log.Printf("🔗 Found %d missing relationships", len(missingRelations))
		// Suggest relationships
		for _, relation := range missingRelations {
			if err := kge.suggestRelationship(relation); err != nil {
				log.Printf("Warning: Failed to suggest relationship: %v", err)
			}
		}
	}

	// Store validation metrics
	kge.storeValidationMetrics(domain, len(contradictions), len(missingRelations))

	log.Printf("✅ Knowledge consistency validation completed")
	return nil
}

// Helper methods
func (kge *KnowledgeGrowthEngine) extractTextFromEpisode(episode map[string]interface{}) string {
	// Extract text from various fields in the episode
	if text, ok := episode["text"].(string); ok {
		return text
	}
	if text, ok := episode["description"].(string); ok {
		return text
	}
	if text, ok := episode["content"].(string); ok {
		return text
	}
	return ""
}

func (kge *KnowledgeGrowthEngine) extractConceptsFromText(text, domain string) []ConceptDiscovery {
	// Use semantic analysis via LLM instead of simple pattern matching
	return kge.extractConceptsWithLLM(text, domain)
}

// extractConceptsWithLLM uses LLM to extract concepts with semantic understanding
func (kge *KnowledgeGrowthEngine) extractConceptsWithLLM(text, domain string) []ConceptDiscovery {
	if len(strings.TrimSpace(text)) == 0 {
		return []ConceptDiscovery{}
	}

	// Use HDN API to extract concepts via LLM
	// First, try to use HDN's interpret endpoint for concept extraction
	hdnURL := strings.TrimSuffix(kge.hdnURL, "/")
	if hdnURL == "" {
		hdnURL = "http://localhost:8081"
	}

	// Get user interests for relevance filtering
	userInterests := kge.getUserInterests()

	// Create prompt for concept extraction with relevance focus
	prompt := fmt.Sprintf(`Analyze the following text from the %s domain and extract USEFUL, RELEVANT concepts.

Text: %s

User Interests: %s

For each concept you identify, provide:
1. A clear, meaningful name (not generic like "concept" or "thing")
2. A definition explaining what it is and why it's useful
3. Confidence level (0.0-1.0) based on how clearly the concept is described
4. Relevance score (0.0-1.0) - how relevant/useful this concept is to the user
5. Properties or characteristics if mentioned
6. Constraints or limitations if mentioned

Return as JSON array with format:
[
  {
    "name": "ConceptName",
    "definition": "Clear definition explaining what it is and why it's useful...",
    "confidence": 0.85,
    "relevance": 0.80,
    "properties": ["property1", "property2"],
    "constraints": ["constraint1"]
  }
]

Only extract concepts that are:
- USEFUL - Will help accomplish tasks or solve problems
- RELEVANT - Related to user interests or domain knowledge
- ACTIONABLE - Can be used in practice, not just theoretical
- Clearly defined or described in the text
- Domain-relevant
- Not too generic or vague
- Have meaningful names (not timestamps or IDs)

Prioritize concepts that:
- Help accomplish specific tasks
- Solve real problems
- Are actionable and practical
- Relate to user interests

Skip concepts that are:
- Too abstract or theoretical without practical use
- Not relevant to user interests
- Too generic or vague
- Just restating obvious information

If no useful, relevant concepts are found, return empty array [].`, domain, text, userInterests)

	// Call HDN interpret endpoint
	interpretURL := fmt.Sprintf("%s/api/v1/interpret", hdnURL)
	reqData := map[string]interface{}{
		"input": prompt, // API expects "input" not "text"
		"context": map[string]string{
			"origin": "fsm", // Mark as background task for LOW priority
		},
	}

	reqJSON, err := json.Marshal(reqData)
	if err != nil {
		log.Printf("⚠️ Failed to marshal concept extraction request: %v", err)
		return kge.extractConceptsFallback(text, domain)
	}

	// Rate limiting: Add delay between LLM requests to prevent GPU overload
	// Default: 5 seconds, configurable via FSM_LLM_REQUEST_DELAY_MS
	delayMs := 5000
	if v := strings.TrimSpace(os.Getenv("FSM_LLM_REQUEST_DELAY_MS")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			delayMs = n
		}
	}
	if delayMs > 0 {
		time.Sleep(time.Duration(delayMs) * time.Millisecond)
	}

	// Create context with longer timeout for concept extraction (LLM calls can be slow)
	// Default: 120 seconds, configurable via FSM_CONCEPT_EXTRACTION_TIMEOUT_SECONDS
	timeoutSeconds := 120
	if v := strings.TrimSpace(os.Getenv("FSM_CONCEPT_EXTRACTION_TIMEOUT_SECONDS")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			timeoutSeconds = n
		}
	}
	ctx, cancel := context.WithTimeout(kge.ctx, time.Duration(timeoutSeconds)*time.Second)
	defer cancel()

	// Create HTTP request with context for timeout control
	req, err := http.NewRequestWithContext(ctx, "POST", interpretURL, bytes.NewReader(reqJSON))
	if err != nil {
		log.Printf("⚠️ Failed to create concept extraction request: %v", err)
		return kge.extractConceptsFallback(text, domain)
	}
	req.Header.Set("Content-Type", "application/json")

	// Use async HTTP client (or sync fallback) - handles longer timeouts better
	// This will use async queue if USE_ASYNC_HTTP_QUEUE=1 is set
	// The async queue processes requests asynchronously and handles timeouts properly
	resp, err := Do(ctx, req)
	if err != nil {
		log.Printf("⚠️ Concept extraction LLM call failed: %v, using fallback", err)
		return kge.extractConceptsFallback(text, domain)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("⚠️ Concept extraction returned status %d, using fallback", resp.StatusCode)
		return kge.extractConceptsFallback(text, domain)
	}

	var interpretResp map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&interpretResp); err != nil {
		log.Printf("⚠️ Failed to decode concept extraction response: %v, using fallback", err)
		return kge.extractConceptsFallback(text, domain)
	}

	// Extract concepts from response
	conceptsJSON, ok := interpretResp["result"].(string)
	if !ok {
		if result, ok := interpretResp["output"].(string); ok {
			conceptsJSON = result
		} else {
			log.Printf("⚠️ No result in concept extraction response, using fallback")
			return kge.extractConceptsFallback(text, domain)
		}
	}

	// Parse concepts from JSON
	var concepts []map[string]interface{}
	if err := json.Unmarshal([]byte(conceptsJSON), &concepts); err != nil {
		// Try to extract JSON array from text response
		start := strings.Index(conceptsJSON, "[")
		end := strings.LastIndex(conceptsJSON, "]")
		if start >= 0 && end > start {
			conceptsJSON = conceptsJSON[start : end+1]
			if err := json.Unmarshal([]byte(conceptsJSON), &concepts); err != nil {
				log.Printf("⚠️ Failed to parse concepts JSON: %v, using fallback", err)
				return kge.extractConceptsFallback(text, domain)
			}
		} else {
			log.Printf("⚠️ No JSON array found in response, using fallback")
			return kge.extractConceptsFallback(text, domain)
		}
	}

	// Convert to ConceptDiscovery
	var discoveries []ConceptDiscovery
	for _, concept := range concepts {
		name, _ := concept["name"].(string)
		def, _ := concept["definition"].(string)
		conf, _ := concept["confidence"].(float64)

		if name == "" || def == "" {
			continue
		}

		// Filter out generic names
		if kge.isGenericConceptName(name) {
			continue
		}

		// Ensure confidence is valid
		if conf < 0.0 {
			conf = 0.0
		}
		if conf > 1.0 {
			conf = 1.0
		}

		// Extract properties
		var properties []string
		if props, ok := concept["properties"].([]interface{}); ok {
			for _, p := range props {
				if propStr, ok := p.(string); ok {
					properties = append(properties, propStr)
				}
			}
		}

		// Extract constraints
		var constraints []string
		if constrs, ok := concept["constraints"].([]interface{}); ok {
			for _, c := range constrs {
				if constrStr, ok := c.(string); ok {
					constraints = append(constraints, constrStr)
				}
			}
		}

		// Get relevance score
		relevance, _ := concept["relevance"].(float64)
		if relevance < 0.0 {
			relevance = 0.0
		}
		if relevance > 1.0 {
			relevance = 1.0
		}

		// Only include concepts with reasonable relevance (>= 0.4)
		if relevance >= 0.4 {
			discovery := ConceptDiscovery{
				Name:        name,
				Domain:      domain,
				Definition:  def,
				Confidence:  conf,
				Source:      "llm_semantic_analysis",
				Properties:  properties,
				Constraints: constraints,
				CreatedAt:   time.Now(),
			}

			// Store relevance in properties for later use
			// (Note: ConceptDiscovery doesn't have a Relevance field, but we can filter here)
			discoveries = append(discoveries, discovery)
			log.Printf("✨ Extracted relevant concept: %s (confidence: %.2f, relevance: %.2f)", name, conf, relevance)
		} else {
			log.Printf("🛑 Filtered out low-relevance concept: %s (relevance: %.2f)", name, relevance)
		}
	}

	if len(discoveries) > 0 {
		log.Printf("📚 Extracted %d concepts via semantic analysis", len(discoveries))
	}

	if len(discoveries) > 0 {
		log.Printf("📚 Extracted %d relevant concepts via semantic analysis", len(discoveries))
	}

	return discoveries
}

// getUserInterests retrieves user interests/goals from Redis for relevance filtering
func (kge *KnowledgeGrowthEngine) getUserInterests() string {
	// Try to get user interests from Redis
	interestsKey := "user:interests"
	interests, err := kge.redis.Get(kge.ctx, interestsKey).Result()
	if err == nil && interests != "" {
		return interests
	}

	// Try to get from recent goals (what user is working on)
	goalsKey := "reasoning:curiosity_goals:all"
	goalsData, err := kge.redis.LRange(kge.ctx, goalsKey, 0, 4).Result()
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

// extractConceptsFallback provides fallback concept extraction when LLM is unavailable
func (kge *KnowledgeGrowthEngine) extractConceptsFallback(text, domain string) []ConceptDiscovery {
	log.Printf("⚠️ Using fallback concept extraction (LLM unavailable)")

	// Fallback: simple keyword extraction for important terms
	var discoveries []ConceptDiscovery

	// Look for capitalized words (potential concept names)
	words := strings.Fields(text)
	seen := make(map[string]bool)

	for _, word := range words {
		// Remove punctuation
		cleanWord := strings.Trim(word, ".,!?;:()[]{}")
		if len(cleanWord) < 3 {
			continue
		}

		// Check if it's capitalized (potential concept name)
		if strings.ToUpper(cleanWord[:1]) == cleanWord[:1] && !seen[cleanWord] {
			seen[cleanWord] = true

			// Skip common words
			commonWords := []string{"The", "This", "That", "These", "Those", "A", "An", "And", "Or", "But", "If", "Then", "When", "Where", "Why", "How"}
			isCommon := false
			for _, common := range commonWords {
				if cleanWord == common {
					isCommon = true
					break
				}
			}
			if isCommon {
				continue
			}

			// Create discovery with low confidence (fallback)
			discovery := ConceptDiscovery{
				Name:       cleanWord,
				Domain:     domain,
				Definition: fmt.Sprintf("A concept mentioned in %s context", domain),
				Confidence: 0.4, // Lower confidence for fallback
				Source:     "fallback_extraction",
				CreatedAt:  time.Now(),
			}

			// Filter generic names
			if !kge.isGenericConceptName(cleanWord) {
				discoveries = append(discoveries, discovery)
			}
		}
	}

	return discoveries
}

func (kge *KnowledgeGrowthEngine) textContainsPattern(text, pattern string) bool {
	// Simple pattern matching - in reality would use more sophisticated NLP
	return len(text) > 0 && len(pattern) > 0
}

func (kge *KnowledgeGrowthEngine) deduplicateConcepts(discoveries []ConceptDiscovery) []ConceptDiscovery {
	seen := make(map[string]bool)
	var unique []ConceptDiscovery

	for _, discovery := range discoveries {
		if !seen[discovery.Name] {
			seen[discovery.Name] = true
			unique = append(unique, discovery)
		}
	}

	return unique
}

func (kge *KnowledgeGrowthEngine) filterByConfidence(discoveries []ConceptDiscovery, threshold float64) []ConceptDiscovery {
	var filtered []ConceptDiscovery
	var lowConfCount, mediumConfCount, highConfCount int

	for _, discovery := range discoveries {
		if discovery.Confidence >= threshold {
			// Categorize by confidence level
			if discovery.Confidence >= 0.85 {
				highConfCount++
			} else if discovery.Confidence >= 0.75 {
				mediumConfCount++
			}
			filtered = append(filtered, discovery)
		} else {
			lowConfCount++
			// Log rejected discoveries for monitoring with more context
			log.Printf("🛑 Concept discovery rejected (confidence %.2f < %.2f): %s [Source: %s]",
				discovery.Confidence, threshold, discovery.Name, discovery.Source)
		}
	}

	// Summary log for quality metrics
	if len(discoveries) > 0 {
		log.Printf("📊 Confidence filtering: %d high (≥0.85), %d medium (≥0.75), %d rejected (<%.2f) out of %d total",
			highConfCount, mediumConfCount, lowConfCount, threshold, len(discoveries))
	}

	return filtered
}

func (kge *KnowledgeGrowthEngine) getDomainConcepts(domain string) ([]map[string]interface{}, error) {
	// Try MCP first, fallback to direct API
	cypherQuery := fmt.Sprintf("MATCH (c:Concept {domain: '%s'}) RETURN c LIMIT 100", domain)

	result, err := kge.callMCPTool("query_neo4j", map[string]interface{}{
		"query": cypherQuery,
	})
	if err != nil {
		log.Printf("⚠️ MCP query failed, falling back to direct API: %v", err)
		return kge.getDomainConceptsDirect(domain)
	}

	// Parse MCP result
	resultMap, ok := result.(map[string]interface{})
	if !ok {
		return kge.getDomainConceptsDirect(domain)
	}

	results, ok := resultMap["results"].([]interface{})
	if !ok {
		return kge.getDomainConceptsDirect(domain)
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
		} else if c, ok := row["concept"].(map[string]interface{}); ok {
			concepts = append(concepts, c)
		}
	}

	if len(concepts) > 0 {
		log.Printf("✅ Retrieved %d concepts via MCP", len(concepts))
		return concepts, nil
	}

	// Fallback if no results
	return kge.getDomainConceptsDirect(domain)
}

// getDomainConceptsDirect gets domain concepts via direct API (fallback)
func (kge *KnowledgeGrowthEngine) getDomainConceptsDirect(domain string) ([]map[string]interface{}, error) {
	encodedDomain := url.QueryEscape(domain)
	searchURL := fmt.Sprintf("%s/api/v1/knowledge/search?domain=%s&limit=100", kge.hdnURL, encodedDomain)

	resp, err := kge.httpClient.Get(searchURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var searchResult struct {
		Concepts []map[string]interface{} `json:"concepts"`
	}

	// Read body once to allow tolerant fallback
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return []map[string]interface{}{}, nil
	}
	if err := json.Unmarshal(bodyBytes, &searchResult); err != nil {
		log.Printf("⚠️ Knowledge search decode fallback: treating as zero concepts: %v", err)
		return []map[string]interface{}{}, nil
	}

	return searchResult.Concepts, nil
}

func (kge *KnowledgeGrowthEngine) createConcept(discovery ConceptDiscovery) error {
	conceptData := map[string]interface{}{
		"name":        discovery.Name,
		"domain":      discovery.Domain,
		"definition":  discovery.Definition,
		"properties":  discovery.Properties,
		"constraints": discovery.Constraints,
		"examples":    discovery.Examples,
	}

	reqData, _ := json.Marshal(conceptData)
	resp, err := kge.httpClient.Post(kge.hdnURL+"/api/v1/knowledge/concepts", "application/json", bytes.NewReader(reqData))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("failed to create concept: status %d", resp.StatusCode)
	}

	return nil
}

func (kge *KnowledgeGrowthEngine) findMissingRelations(concepts []map[string]interface{}, domain string) []KnowledgeGap {
	// Simplified implementation - look for concepts without relationships
	var gaps []KnowledgeGap

	for _, concept := range concepts {
		name, ok := concept["name"].(string)
		if !ok {
			continue
		}

		// Check if concept has relationships
		hasRelations := false
		if relations, exists := concept["relations"]; exists && relations != nil {
			hasRelations = true
		}

		if !hasRelations {
			gap := KnowledgeGap{
				Type:        "missing_relation",
				Description: fmt.Sprintf("Concept '%s' has no relationships", name),
				Domain:      domain,
				Priority:    6,
				Suggestions: []string{"Find related concepts", "Add relationship types"},
			}
			gaps = append(gaps, gap)
		}
	}

	return gaps
}

func (kge *KnowledgeGrowthEngine) findMissingConstraints(concepts []map[string]interface{}, domain string) []KnowledgeGap {
	// Look for concepts without constraints
	var gaps []KnowledgeGap

	for _, concept := range concepts {
		name, ok := concept["name"].(string)
		if !ok {
			continue
		}

		constraints, exists := concept["constraints"]
		var num int
		if exists && constraints != nil {
			if arr, ok := constraints.([]interface{}); ok {
				num = len(arr)
			} else {
				num = 0
			}
		}
		if !exists || constraints == nil || num == 0 {
			gap := KnowledgeGap{
				Type:        "missing_constraint",
				Description: fmt.Sprintf("Concept '%s' has no constraints", name),
				Domain:      domain,
				Priority:    5,
				Suggestions: []string{"Add domain-specific constraints", "Define validation rules"},
			}
			gaps = append(gaps, gap)
		}
	}

	return gaps
}

func (kge *KnowledgeGrowthEngine) findMissingExamples(concepts []map[string]interface{}, domain string) []KnowledgeGap {
	// Look for concepts without examples
	var gaps []KnowledgeGap

	for _, concept := range concepts {
		name, ok := concept["name"].(string)
		if !ok {
			continue
		}

		examples, exists := concept["examples"]
		var num int
		if exists && examples != nil {
			if arr, ok := examples.([]interface{}); ok {
				num = len(arr)
			} else {
				num = 0
			}
		}
		if !exists || examples == nil || num == 0 {
			gap := KnowledgeGap{
				Type:        "missing_example",
				Description: fmt.Sprintf("Concept '%s' has no examples", name),
				Domain:      domain,
				Priority:    4,
				Suggestions: []string{"Add usage examples", "Create test cases"},
			}
			gaps = append(gaps, gap)
		}
	}

	return gaps
}

func (kge *KnowledgeGrowthEngine) fillKnowledgeGap(gap KnowledgeGap, domain string) error {
	// Implement gap filling based on gap type
	switch gap.Type {
	case "missing_relation":
		return kge.fillMissingRelations(gap, domain)
	case "missing_constraint":
		return kge.fillMissingConstraints(gap, domain)
	case "missing_example":
		return kge.fillMissingExamples(gap, domain)
	default:
		return fmt.Errorf("unknown gap type: %s", gap.Type)
	}
}

func (kge *KnowledgeGrowthEngine) fillMissingRelations(gap KnowledgeGap, domain string) error {
	// Implementation would analyze the concept and suggest relationships
	log.Printf("🔗 Filling missing relations for: %s", gap.Description)
	return nil
}

func (kge *KnowledgeGrowthEngine) fillMissingConstraints(gap KnowledgeGap, domain string) error {
	// Implementation would add domain-appropriate constraints
	log.Printf("🔒 Filling missing constraints for: %s", gap.Description)
	return nil
}

func (kge *KnowledgeGrowthEngine) fillMissingExamples(gap KnowledgeGap, domain string) error {
	// Implementation would generate examples based on domain knowledge
	log.Printf("📝 Filling missing examples for: %s", gap.Description)
	return nil
}

func (kge *KnowledgeGrowthEngine) updateExistingConcepts(episodes []map[string]interface{}, domain string) error {
	// Update existing concepts with new information from episodes
	log.Printf("🔄 Updating existing concepts with new information")
	return nil
}

func (kge *KnowledgeGrowthEngine) findContradictions(concepts []map[string]interface{}) []map[string]interface{} {
	// Look for contradictory information in concepts
	var contradictions []map[string]interface{}

	// Simplified implementation - in reality would use more sophisticated logic
	for i, concept1 := range concepts {
		for j, concept2 := range concepts {
			if i >= j {
				continue
			}

			// Check for contradictions (simplified)
			if kge.conceptsContradict(concept1, concept2) {
				contradiction := map[string]interface{}{
					"concept1": concept1["name"],
					"concept2": concept2["name"],
					"type":     "contradiction",
					"severity": "medium",
				}
				contradictions = append(contradictions, contradiction)
			}
		}
	}

	return contradictions
}

func (kge *KnowledgeGrowthEngine) conceptsContradict(concept1, concept2 map[string]interface{}) bool {
	// Simplified contradiction detection
	// In reality, this would use more sophisticated logic
	return false
}

func (kge *KnowledgeGrowthEngine) resolveContradiction(contradiction map[string]interface{}) error {
	// Resolve contradictions by updating concepts
	log.Printf("🔧 Resolving contradiction between %s and %s",
		contradiction["concept1"], contradiction["concept2"])
	return nil
}

func (kge *KnowledgeGrowthEngine) suggestRelationship(relation KnowledgeGap) error {
	// Suggest relationships between concepts
	log.Printf("💡 Suggesting relationship for: %s", relation.Description)
	return nil
}

// isGenericConceptName checks if a concept name is too generic to be useful
func (kge *KnowledgeGrowthEngine) isGenericConceptName(name string) bool {
	genericNames := []string{
		"concept", "thing", "stuff", "item", "object", "entity",
		"element", "component", "part", "piece", "unit",
		"idea", "notion", "thought", "unknown",
	}

	lowerName := strings.ToLower(strings.TrimSpace(name))

	// Check exact matches
	for _, generic := range genericNames {
		if lowerName == generic {
			return true
		}
	}

	// Check if name is just a timestamp or ID pattern
	if strings.Contains(lowerName, "_2006") || strings.Contains(lowerName, "_20") {
		return true
	}

	// Name too short (likely meaningless)
	if len(lowerName) < 3 {
		return true
	}

	return false
}

// storeValidationMetrics stores validation metrics in Redis for monitoring
// assessConceptValue uses LLM to assess if a concept is novel and worth learning
func (kge *KnowledgeGrowthEngine) assessConceptValue(conceptKnowledge string, domain string) (bool, bool, error) {
	if len(strings.TrimSpace(conceptKnowledge)) == 0 {
		return false, false, nil
	}

	// Get existing knowledge context
	existingConcepts, err := kge.getDomainConcepts(domain)
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

	// Create prompt for LLM assessment (similar to knowledge_integration.go)
	prompt := fmt.Sprintf(`Assess whether the following concept is worth learning and storing.

Concept to assess: %s
Domain: %s
Existing concepts in domain: %s

Evaluate:
1. NOVELTY: Is this concept new/novel, or is it already obvious/known?
2. VALUE: Is this concept worth storing? Will it help accomplish tasks?

Return JSON:
{
  "is_novel": true/false,
  "is_worth_learning": true/false,
  "reasoning": "Brief explanation",
  "novelty_score": 0.0-1.0,
  "value_score": 0.0-1.0
}

Be strict: Only mark as novel and worth learning if genuinely new and useful.`, conceptKnowledge, domain, existingContext)

	// Call HDN interpret endpoint
	hdnURL := strings.TrimSuffix(kge.hdnURL, "/")
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
	resp, err := Post(kge.ctx, interpretURL, "application/json", reqJSON, nil)
	if err != nil {
		return false, false, fmt.Errorf("concept assessment LLM call failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false, false, fmt.Errorf("concept assessment returned status %d", resp.StatusCode)
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

	return isNovel, isWorthLearning, nil
}

// callMCPTool calls an MCP tool and returns the result
func (kge *KnowledgeGrowthEngine) callMCPTool(toolName string, arguments map[string]interface{}) (interface{}, error) {
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

	resp, err := kge.httpClient.Post(kge.mcpEndpoint, "application/json", bytes.NewReader(jsonData))
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

// getExistingConcept checks if a concept already exists and returns it
func (kge *KnowledgeGrowthEngine) getExistingConcept(conceptName string, domain string) (map[string]interface{}, error) {
	if len(strings.TrimSpace(conceptName)) == 0 {
		return nil, nil
	}

	// Try using MCP get_concept tool first
	result, err := kge.callMCPTool("get_concept", map[string]interface{}{
		"name":   conceptName,
		"domain": domain,
	})
	if err != nil {
		log.Printf("⚠️ MCP get_concept failed, falling back to direct API: %v", err)
		return kge.getExistingConceptDirect(conceptName, domain)
	}

	// Parse MCP result
	resultMap, ok := result.(map[string]interface{})
	if !ok {
		return kge.getExistingConceptDirect(conceptName, domain)
	}

	results, ok := resultMap["results"].([]interface{})
	if !ok {
		return kge.getExistingConceptDirect(conceptName, domain)
	}

	// Check if any existing concept matches (case-insensitive)
	conceptNameLower := strings.ToLower(conceptName)
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

		// Row in Cypher result usually has Props if it's a node
		props, _ := concept["Props"].(map[string]interface{})
		name := ""
		if props != nil {
			name, _ = props["name"].(string)
		} else {
			name, _ = concept["name"].(string)
		}

		if strings.ToLower(name) == conceptNameLower {
			log.Printf("🔍 Found existing concept via MCP: %s", name)
			if props != nil {
				return props, nil
			}
			return concept, nil
		}
	}

	return nil, nil
}

// getExistingConceptDirect checks if a concept exists via direct API (fallback)
func (kge *KnowledgeGrowthEngine) getExistingConceptDirect(conceptName string, domain string) (map[string]interface{}, error) {
	hdnURL := strings.TrimSuffix(kge.hdnURL, "/")
	if hdnURL == "" {
		hdnURL = "http://localhost:8081"
	}

	encodedConceptName := url.QueryEscape(conceptName)
	searchURL := fmt.Sprintf("%s/api/v1/knowledge/search?name=%s&limit=10", hdnURL, encodedConceptName)
	resp, err := kge.httpClient.Get(searchURL)
	if err != nil {
		return nil, fmt.Errorf("failed to search concepts: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, nil
	}

	var searchResult struct {
		Concepts []map[string]interface{} `json:"concepts"`
		Count    int                      `json:"count"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&searchResult); err != nil {
		return nil, nil
	}

	conceptNameLower := strings.ToLower(conceptName)
	for _, concept := range searchResult.Concepts {
		existingName, _ := concept["name"].(string)
		if strings.ToLower(existingName) == conceptNameLower {
			return concept, nil
		}
	}

	return nil, nil
}

func (kge *KnowledgeGrowthEngine) storeValidationMetrics(domain string, contradictions, missingRelations int) {
	key := fmt.Sprintf("knowledge:validation:metrics:%s", domain)
	metrics := map[string]interface{}{
		"contradictions":    contradictions,
		"missing_relations": missingRelations,
		"timestamp":         time.Now().UTC().Format(time.RFC3339),
		"last_validation":   time.Now().Unix(),
	}

	data, err := json.Marshal(metrics)
	if err != nil {
		log.Printf("Warning: Failed to marshal validation metrics: %v", err)
		return
	}

	if err := kge.redis.Set(kge.ctx, key, data, 7*24*time.Hour).Err(); err != nil {
		log.Printf("Warning: Failed to store validation metrics: %v", err)
	} else {
		log.Printf("📊 Stored validation metrics: %d contradictions, %d missing relations",
			contradictions, missingRelations)
	}
}
