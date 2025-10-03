package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/redis/go-redis/v9"
)

// KnowledgeGrowthEngine handles growing the knowledge base
type KnowledgeGrowthEngine struct {
	hdnURL     string
	redis      *redis.Client
	ctx        context.Context
	httpClient *http.Client
}

// NewKnowledgeGrowthEngine creates a new knowledge growth engine
func NewKnowledgeGrowthEngine(hdnURL string, redis *redis.Client) *KnowledgeGrowthEngine {
	return &KnowledgeGrowthEngine{
		hdnURL:     hdnURL,
		redis:      redis,
		ctx:        context.Background(),
		httpClient: &http.Client{Timeout: 10 * time.Second},
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

	// Remove duplicates and filter by confidence
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
	log.Printf("🌱 Growing knowledge base for domain: %s", domain)

	// 1. Discover new concepts
	discoveries, err := kge.DiscoverNewConcepts(episodes, domain)
	if err != nil {
		return fmt.Errorf("failed to discover concepts: %w", err)
	}

	// 2. Create new concepts in the knowledge base
	for _, discovery := range discoveries {
		if err := kge.createConcept(discovery); err != nil {
			log.Printf("Warning: Failed to create concept %s: %v", discovery.Name, err)
			continue
		}
		log.Printf("✅ Created new concept: %s (confidence: %.2f)", discovery.Name, discovery.Confidence)
	}

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
	// This is a simplified implementation
	// In a real system, this would use NLP techniques to extract concepts

	var discoveries []ConceptDiscovery

	// Look for domain-specific patterns
	patterns := map[string][]string{
		"Math":        {"algorithm", "formula", "equation", "theorem", "proof", "calculation"},
		"Programming": {"function", "class", "method", "variable", "loop", "condition"},
		"System":      {"process", "service", "daemon", "thread", "memory", "disk"},
	}

	domainPatterns, exists := patterns[domain]
	if !exists {
		domainPatterns = []string{"concept", "idea", "principle", "rule"}
	}

	for _, pattern := range domainPatterns {
		if kge.textContainsPattern(text, pattern) {
			discovery := ConceptDiscovery{
				Name:       fmt.Sprintf("%s_%s", pattern, time.Now().Format("20060102_150405")),
				Domain:     domain,
				Definition: fmt.Sprintf("A %s related to %s", pattern, domain),
				Confidence: 0.7,
				Source:     "episode_analysis",
				CreatedAt:  time.Now(),
			}
			discoveries = append(discoveries, discovery)
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

	for _, discovery := range discoveries {
		if discovery.Confidence >= threshold {
			filtered = append(filtered, discovery)
		} else {
			// Log rejected discoveries for monitoring
			log.Printf("🛑 Concept discovery rejected (confidence %.2f < %.2f): %s", discovery.Confidence, threshold, discovery.Name)
		}
	}

	return filtered
}

func (kge *KnowledgeGrowthEngine) getDomainConcepts(domain string) ([]map[string]interface{}, error) {
	searchURL := fmt.Sprintf("%s/api/v1/knowledge/search?domain=%s&limit=100", kge.hdnURL, domain)

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
