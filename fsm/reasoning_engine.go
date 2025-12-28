package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// ReasoningEngine provides deduction and inference capabilities
type ReasoningEngine struct {
	hdnURL     string
	redis      *redis.Client
	ctx        context.Context
	httpClient *http.Client
}

// NewReasoningEngine creates a new reasoning engine
func NewReasoningEngine(hdnURL string, redis *redis.Client) *ReasoningEngine {
	return &ReasoningEngine{
		hdnURL:     hdnURL,
		redis:      redis,
		ctx:        context.Background(),
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// Belief represents a belief in the system
type Belief struct {
	ID          string                 `json:"id"`
	Statement   string                 `json:"statement"`
	Confidence  float64                `json:"confidence"`
	Source      string                 `json:"source"`
	Domain      string                 `json:"domain"`
	Evidence    []string               `json:"evidence"` // IDs of supporting facts
	Properties  map[string]interface{} `json:"properties"`
	CreatedAt   time.Time              `json:"created_at"`
	LastUpdated time.Time              `json:"last_updated"`
}

// InferenceRule represents a rule for making inferences
type InferenceRule struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Pattern     string   `json:"pattern"`    // Cypher pattern to match
	Conclusion  string   `json:"conclusion"` // Cypher pattern to create
	Confidence  float64  `json:"confidence"`
	Domain      string   `json:"domain"`
	Description string   `json:"description"`
	Examples    []string `json:"examples"`
}

// ReasoningTrace represents a trace of reasoning steps
type ReasoningTrace struct {
	ID         string                 `json:"id"`
	Goal       string                 `json:"goal"`
	Steps      []ReasoningStep        `json:"steps"`
	Evidence   []string               `json:"evidence"`
	Conclusion string                 `json:"conclusion"`
	Confidence float64                `json:"confidence"`
	Domain     string                 `json:"domain"`
	CreatedAt  time.Time              `json:"created_at"`
	Properties map[string]interface{} `json:"properties"`
}

type ReasoningStep struct {
	StepNumber int                    `json:"step_number"`
	Action     string                 `json:"action"`    // query, infer, validate, etc.
	Query      string                 `json:"query"`     // Cypher query executed
	Result     map[string]interface{} `json:"result"`    // Query result
	Reasoning  string                 `json:"reasoning"` // Human-readable explanation
	Confidence float64                `json:"confidence"`
	Timestamp  time.Time              `json:"timestamp"`
}

// CuriosityGoal represents an intrinsic goal for knowledge exploration
type CuriosityGoal struct {
	ID          string    `json:"id"`
	Type        string    `json:"type"` // gap_filling, contradiction_resolution, concept_exploration
	Description string    `json:"description"`
	Domain      string    `json:"domain"`
	Priority    int       `json:"priority"` // 1-10, higher is more important
	Status      string    `json:"status"`   // pending, active, completed, failed
	Targets     []string  `json:"targets"`  // Concept names or patterns to explore
	CreatedAt   time.Time `json:"created_at"`
}

// GoalOutcome represents the outcome of a goal execution for learning
type GoalOutcome struct {
	GoalID        string    `json:"goal_id"`
	GoalType      string    `json:"goal_type"`
	Domain        string    `json:"domain"`
	Status        string    `json:"status"` // completed, failed, abandoned
	Success       bool      `json:"success"`
	Value         float64   `json:"value"`          // 0-1, value of outcomes
	ExecutionTime float64   `json:"execution_time"` // seconds
	Outcomes      []string  `json:"outcomes"`       // What was learned/achieved
	CreatedAt     time.Time `json:"created_at"`
}

// QueryBeliefs queries the knowledge base as a belief system
func (re *ReasoningEngine) QueryBeliefs(query string, domain string) ([]Belief, error) {
	log.Printf("🧠 Querying beliefs: %s (domain: %s)", query, domain)

	// Convert natural language query to Cypher
	cypherQuery, err := re.translateToCypher(query, domain)
	if err != nil {
		return nil, fmt.Errorf("failed to translate query: %w", err)
	}

	// Execute query against Neo4j via HDN
	beliefs, err := re.executeCypherQuery(cypherQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}

	// Fallback: if no beliefs found, try a broader match within the domain
	if len(beliefs) == 0 {
		terms := strings.TrimSpace(re.extractConceptFromQuery(query))
		if terms != "" {
			fallback := fmt.Sprintf("MATCH (c:Concept) WHERE c.domain = '%s' AND (toLower(c.name) CONTAINS toLower('%s') OR toLower(c.definition) CONTAINS toLower('%s')) RETURN c LIMIT 25", domain, terms, terms)
			fbBeliefs, fbErr := re.executeCypherQuery(fallback)
			if fbErr == nil && len(fbBeliefs) > 0 {
				// Lower confidence for fallback hits (increased from 0.65 to 0.7)
				for i := range fbBeliefs {
					if fbBeliefs[i].Confidence > 0.7 {
						fbBeliefs[i].Confidence = 0.7
					}
				}
				// Filter fallback beliefs below threshold
				var filteredFallback []Belief
				for _, fb := range fbBeliefs {
					if fb.Confidence >= 0.7 {
						filteredFallback = append(filteredFallback, fb)
					}
				}
				fbBeliefs = filteredFallback
				beliefs = fbBeliefs
			}
		}
	}

	log.Printf("📊 Found %d beliefs", len(beliefs))
	return beliefs, nil
}

// InferNewBeliefs applies inference rules to generate new beliefs
func (re *ReasoningEngine) InferNewBeliefs(domain string) ([]Belief, error) {
	log.Printf("🔍 Applying inference rules for domain: %s", domain)
	log.Printf("🔍 Reasoning engine HDN URL: %s", re.hdnURL)

	// Get inference rules for the domain
	rules, err := re.getInferenceRules(domain)
	if err != nil {
		return nil, fmt.Errorf("failed to get inference rules: %w", err)
	}
	log.Printf("📋 Retrieved %d inference rules for domain %s", len(rules), domain)

	var newBeliefs []Belief

	// Apply each rule
	for _, rule := range rules {
		inferred, err := re.applyInferenceRule(rule)
		if err != nil {
			log.Printf("Warning: Failed to apply rule %s: %v", rule.Name, err)
			continue
		}
		newBeliefs = append(newBeliefs, inferred...)
	}

	// If no beliefs were inferred, check if it's because there's no data
	if len(newBeliefs) == 0 {
		// Check if there are any concepts in the domain
		conceptQuery := fmt.Sprintf("MATCH (c:Concept) WHERE c.domain = '%s' RETURN c LIMIT 1", domain)
		concepts, err := re.executeCypherQuery(conceptQuery)
		if err != nil {
			log.Printf("Warning: Failed to check for concepts: %v", err)
		} else if len(concepts) == 0 {
			log.Printf("ℹ️ No concepts found in domain %s, no beliefs to infer", domain)
		} else {
			log.Printf("ℹ️ Found %d concepts but no new beliefs inferred - rules may not match existing data", len(concepts))
		}
	}

	log.Printf("✨ Inferred %d new beliefs", len(newBeliefs))
	return newBeliefs, nil
}

// GenerateCuriosityGoals creates intrinsic goals for knowledge exploration
func (re *ReasoningEngine) GenerateCuriosityGoals(domain string) ([]CuriosityGoal, error) {
	log.Printf("🎯 Generating curiosity goals for domain: %s", domain)

	// First check if there are any concepts in the domain
	conceptQuery := fmt.Sprintf("MATCH (c:Concept) WHERE c.domain = '%s' RETURN c LIMIT 1", domain)
	concepts, err := re.executeCypherQuery(conceptQuery)
	if err != nil {
		log.Printf("Warning: Failed to check for concepts: %v", err)
	} else if len(concepts) == 0 {
		// Check if basic exploration goals already exist to avoid duplicates
		curiosityGoalsKey := fmt.Sprintf("reasoning:curiosity_goals:%s", domain)
		existingGoalsData, err := re.redis.LRange(re.ctx, curiosityGoalsKey, 0, 199).Result()
		if err == nil {
			// Check if goals with the same descriptions already exist
			expectedDescriptions := []string{
				fmt.Sprintf("Use tool_http_get to fetch Wikipedia category page for '%s' and extract key concepts", domain),
				fmt.Sprintf("Use tool_http_get to fetch recent Wikipedia articles about domain concepts"),
				fmt.Sprintf("Use tool_html_scraper to scrape Wikipedia articles about '%s' and extract structured knowledge", domain),
			}

			existingDescriptions := make(map[string]bool)
			for _, goalData := range existingGoalsData {
				var existingGoal CuriosityGoal
				if err := json.Unmarshal([]byte(goalData), &existingGoal); err == nil {
					// Normalize description for comparison
					desc := strings.ToLower(strings.TrimSpace(existingGoal.Description))
					existingDescriptions[desc] = true
				}
			}

			// Check if any of the expected goals already exist
			allExist := true
			for _, expectedDesc := range expectedDescriptions {
				normalized := strings.ToLower(strings.TrimSpace(expectedDesc))
				if !existingDescriptions[normalized] {
					allExist = false
					break
				}
			}

			if allExist {
				log.Printf("ℹ️ Skipping basic exploration goal generation - similar goals already exist for domain %s", domain)
				return []CuriosityGoal{}, nil
			}
		}

		// Also check if we've recently generated basic exploration goals (avoid spam)
		recentGoalsKey := fmt.Sprintf("reasoning:recent_goals:%s", domain)
		recentCount, _ := re.redis.LLen(re.ctx, recentGoalsKey).Result()

		// If we've generated goals recently (within last 10 minutes), skip to avoid spam
		if recentCount > 0 {
			// Check the timestamp of the most recent goal
			recentGoalData, err := re.redis.LIndex(re.ctx, recentGoalsKey, 0).Result()
			if err == nil {
				var recentGoal map[string]interface{}
				if json.Unmarshal([]byte(recentGoalData), &recentGoal) == nil {
					if createdAtStr, ok := recentGoal["created_at"].(string); ok {
						if createdAt, err := time.Parse(time.RFC3339, createdAtStr); err == nil {
							if time.Since(createdAt) < 10*time.Minute {
								log.Printf("ℹ️ Skipping goal generation - recently generated goals for domain %s (within last 10 minutes)", domain)
								return []CuriosityGoal{}, nil
							}
						}
					}
				}
			}
		}

		log.Printf("ℹ️ No concepts found in domain %s, generating basic exploration goals", domain)
		// Generate basic exploration goals when no data exists
		basicGoals := []CuriosityGoal{
			{
				ID:          fmt.Sprintf("explore_%s_%d", domain, time.Now().UnixNano()),
				Type:        "exploration",
				Description: fmt.Sprintf("Use tool_http_get to fetch Wikipedia category page for '%s' and extract key concepts", domain),
				Targets:     []string{},
				Priority:    8,
				Domain:      domain,
				CreatedAt:   time.Now(),
			},
			{
				ID:          fmt.Sprintf("populate_%s_%d", domain, time.Now().UnixNano()),
				Type:        "knowledge_building",
				Description: fmt.Sprintf("Use tool_html_scraper to scrape Wikipedia articles about '%s' and extract structured knowledge", domain),
				Targets:     []string{},
				Priority:    9,
				Domain:      domain,
				CreatedAt:   time.Now(),
			},
		}

		// Track recent goal generation to prevent spam
		for _, goal := range basicGoals {
			goalData, _ := json.Marshal(goal)
			re.redis.LPush(re.ctx, recentGoalsKey, goalData)
			re.redis.LTrim(re.ctx, recentGoalsKey, 0, 9)            // Keep last 10
			re.redis.Expire(re.ctx, recentGoalsKey, 10*time.Minute) // Increased to 10 minutes
		}

		log.Printf("✅ Generated %d basic exploration goals", len(basicGoals))
		return basicGoals, nil
	}

	var goals []CuriosityGoal

	// 1. Gap filling goals
	gapGoals, err := re.generateGapFillingGoals(domain)
	if err != nil {
		log.Printf("Warning: Failed to generate gap filling goals: %v", err)
	} else {
		goals = append(goals, gapGoals...)
	}

	// 2. Contradiction resolution goals
	contradictionGoals, err := re.generateContradictionGoals(domain)
	if err != nil {
		log.Printf("Warning: Failed to generate contradiction goals: %v", err)
	} else {
		goals = append(goals, contradictionGoals...)
	}

	// 3. Concept exploration goals
	explorationGoals, err := re.generateExplorationGoals(domain)
	if err != nil {
		log.Printf("Warning: Failed to generate exploration goals: %v", err)
	} else {
		goals = append(goals, explorationGoals...)
	}

	// 4. News-driven curiosity goals
	newsGoals, err := re.generateNewsCuriosityGoals(domain)
	if err != nil {
		log.Printf("Warning: Failed to generate news curiosity goals: %v", err)
	} else {
		goals = append(goals, newsGoals...)
	}

	// Get existing goals from Redis for deduplication
	curiosityGoalsKey := fmt.Sprintf("reasoning:curiosity_goals:%s", domain)
	existingGoalsData, err := re.redis.LRange(re.ctx, curiosityGoalsKey, 0, 199).Result()
	existingDescriptions := make(map[string]bool)
	if err == nil {
		for _, goalData := range existingGoalsData {
			var existingGoal CuriosityGoal
			if err := json.Unmarshal([]byte(goalData), &existingGoal); err == nil {
				// Normalize description for comparison
				desc := strings.ToLower(strings.TrimSpace(existingGoal.Description))
				existingDescriptions[desc] = true
			}
		}
	}

	// Filter out generic/useless goals and duplicates before returning
	var filteredGoals []CuriosityGoal
	seenDescriptions := make(map[string]bool)
	for _, goal := range goals {
		// Skip generic goals
		if re.isGenericGoal(goal) {
			log.Printf("🚫 Filtered out generic goal: %s", goal.Description)
			continue
		}
		// Skip duplicate descriptions (normalized) - check both current batch and existing goals
		descKey := strings.ToLower(strings.TrimSpace(goal.Description))
		if seenDescriptions[descKey] {
			log.Printf("🚫 Filtered out duplicate goal (current batch): %s", goal.Description)
			continue
		}
		if existingDescriptions[descKey] {
			log.Printf("🚫 Filtered out duplicate goal (already exists): %s", goal.Description)
			continue
		}
		seenDescriptions[descKey] = true
		filteredGoals = append(filteredGoals, goal)
	}

	// Limit the number of goals returned to prevent overwhelming the system
	maxGoals := 10
	if len(filteredGoals) > maxGoals {
		// Keep the highest priority goals
		sort.Slice(filteredGoals, func(i, j int) bool {
			return filteredGoals[i].Priority > filteredGoals[j].Priority
		})
		filteredGoals = filteredGoals[:maxGoals]
		log.Printf("📊 Limited goals to top %d by priority", maxGoals)
	}

	// Clean up old and completed goals before adding new ones
	re.cleanupOldGoals(domain)

	log.Printf("🎯 Generated %d curiosity goals (filtered from %d raw goals)", len(filteredGoals), len(goals))
	return filteredGoals, nil
}

// LogReasoningTrace creates a trace of reasoning steps
func (re *ReasoningEngine) LogReasoningTrace(trace ReasoningTrace) error {
	log.Printf("📝 Logging reasoning trace: %s", trace.Goal)

	// Store trace in Redis
	traceData, err := json.Marshal(trace)
	if err != nil {
		return fmt.Errorf("failed to marshal trace: %w", err)
	}

	// Store in multiple keys for different access patterns
	keys := []string{
		fmt.Sprintf("reasoning:traces:%s", trace.Domain),
		"reasoning:traces:all",
		fmt.Sprintf("reasoning:traces:goal:%s", trace.Goal),
	}

	for _, key := range keys {
		if err := re.redis.LPush(re.ctx, key, traceData).Err(); err != nil {
			log.Printf("Warning: Failed to store trace in %s: %v", key, err)
		}
		// Keep only last 20 traces per key (reduced from 100 to prevent UI spam)
		re.redis.LTrim(re.ctx, key, 0, 19)
	}

	log.Printf("✅ Reasoning trace logged successfully")
	return nil
}

// ExplainReasoning provides a human-readable explanation of reasoning
func (re *ReasoningEngine) ExplainReasoning(goal string, domain string) (string, error) {
	log.Printf("💭 Explaining reasoning for goal: %s", goal)

	// Get recent traces for this goal
	traces, err := re.getReasoningTraces(goal, domain, 5)
	if err != nil {
		return "", fmt.Errorf("failed to get traces: %w", err)
	}

	if len(traces) == 0 {
		return "No reasoning traces found for this goal.", nil
	}

	// Build explanation from traces
	var explanation strings.Builder
	explanation.WriteString("Reasoning explanation for goal: " + goal + "\n\n")

	for i, trace := range traces {
		explanation.WriteString(fmt.Sprintf("Approach %d:\n", i+1))
		explanation.WriteString(fmt.Sprintf("  Goal: %s\n", trace.Goal))
		explanation.WriteString(fmt.Sprintf("  Conclusion: %s\n", trace.Conclusion))
		explanation.WriteString(fmt.Sprintf("  Confidence: %.2f\n", trace.Confidence))
		explanation.WriteString("  Steps:\n")

		for _, step := range trace.Steps {
			explanation.WriteString(fmt.Sprintf("    %d. %s: %s (confidence: %.2f)\n",
				step.StepNumber, step.Action, step.Reasoning, step.Confidence))
		}
		explanation.WriteString("\n")
	}

	return explanation.String(), nil
}

// Helper methods
func (re *ReasoningEngine) translateToCypher(query, domain string) (string, error) {
	// This is a simplified implementation
	// In a real system, this would use NLP to convert natural language to Cypher

	query = strings.ToLower(query)

	// Simple pattern matching for common queries
	if strings.Contains(query, "what is") {
		concept := re.extractConceptFromQuery(query)
		return fmt.Sprintf("MATCH (c:Concept {name: '%s'}) RETURN c", concept), nil
	}

	if strings.Contains(query, "related to") {
		concept := re.extractConceptFromQuery(query)
		// Broaden edges and direction to find useful neighbors for exploration
		return fmt.Sprintf("MATCH (:Concept {name: '%s'})-[:RELATED_TO|:IS_A|:PART_OF]-(related) RETURN related", concept), nil
	}

	if strings.Contains(query, "all concepts") {
		return fmt.Sprintf("MATCH (c:Concept) WHERE c.domain = '%s' RETURN c", domain), nil
	}

	// Default: search for concepts containing the query terms
	terms := strings.Fields(query)
	whereClause := "WHERE "
	for i, term := range terms {
		if i > 0 {
			whereClause += " AND "
		}
		whereClause += fmt.Sprintf("c.name CONTAINS '%s'", term)
	}

	return "MATCH (c:Concept) " + whereClause + " RETURN c", nil
}

func (re *ReasoningEngine) extractConceptFromQuery(query string) string {
	// Simple extraction - in reality would use more sophisticated NLP
	words := strings.Fields(query)
	if len(words) > 2 {
		return strings.Join(words[2:], " ")
	}
	return query
}

func (re *ReasoningEngine) executeCypherQuery(cypherQuery string) ([]Belief, error) {
	// Execute Cypher query via HDN API
	queryData := map[string]interface{}{
		"query": cypherQuery,
	}

	reqData, err := json.Marshal(queryData)
	if err != nil {
		return nil, err
	}

	resp, err := re.httpClient.Post(re.hdnURL+"/api/v1/knowledge/query", "application/json", bytes.NewReader(reqData))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("query failed with status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Count   int                      `json:"count"`
		Results []map[string]interface{} `json:"results"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	// Convert results to beliefs with quality-based confidence and LLM filtering
	var beliefs []Belief
	var filteredCount int

	// Limit how many beliefs we process to avoid overwhelming the system
	maxBeliefsToProcess := 100
	if len(result.Results) > maxBeliefsToProcess {
		log.Printf("⚠️ Limiting belief processing to %d out of %d results", maxBeliefsToProcess, len(result.Results))
		result.Results = result.Results[:maxBeliefsToProcess]
	}

	for i, res := range result.Results {
		statement := re.extractStatementFromResult(res)

		// Calculate confidence based on data quality
		confidence := re.calculateBeliefConfidence(res, statement)

		// Skip low-confidence beliefs (filter at 0.45 threshold - adjusted to allow existing concepts)
		if confidence < 0.45 {
			filteredCount++
			continue
		}

		// Skip LLM assessment for simple concept names (they're already in KB as concepts)
		// Only assess beliefs that have substantial content beyond just a name
		shouldAssess := confidence >= 0.7 && len(statement) > 20 &&
			!strings.Contains(strings.ToLower(statement), "century") && // Skip time periods
			!strings.Contains(strings.ToLower(statement), "in ") // Skip "X in Y" patterns

		if shouldAssess {
			isNovel, isWorthLearning, err := re.assessBeliefValue(statement, re.extractDomainFromResult(res))
			if err != nil {
				// If assessment fails, skip the belief (don't default to storing)
				// This prevents storing beliefs when LLM assessment isn't working
				log.Printf("⏭️ Skipping belief (assessment failed): %s - %v", statement[:minInt(50, len(statement))], err)
				filteredCount++
				continue
			}

			if !isNovel || !isWorthLearning {
				filteredCount++
				if !isNovel {
					log.Printf("⏭️ Skipping belief (not novel/obvious): %s", statement[:minInt(50, len(statement))])
				} else {
					log.Printf("⏭️ Skipping belief (not worth learning): %s", statement[:minInt(50, len(statement))])
				}
				continue
			}
		} else if confidence >= 0.6 {
			// For medium-confidence beliefs that don't meet assessment criteria,
			// store them but log that they weren't assessed
			log.Printf("ℹ️ Storing belief without LLM assessment (simple concept name): %s", statement[:minInt(50, len(statement))])
		}

		belief := Belief{
			ID:          fmt.Sprintf("belief_%d_%d", time.Now().UnixNano(), i),
			Statement:   statement,
			Confidence:  confidence,
			Source:      "knowledge_query",
			Domain:      re.extractDomainFromResult(res),
			CreatedAt:   time.Now(),
			LastUpdated: time.Now(),
		}
		beliefs = append(beliefs, belief)
	}

	log.Printf("📊 Belief quality: %d beliefs extracted (%d filtered: %d low confidence, %d not worth learning)",
		len(beliefs), filteredCount, len(result.Results)-len(beliefs)-filteredCount, filteredCount-(len(result.Results)-len(beliefs)))

	return beliefs, nil
}

func (re *ReasoningEngine) extractStatementFromResult(result map[string]interface{}) string {
	// Flat fields
	if name, ok := result["name"].(string); ok && name != "" {
		return name
	}
	if definition, ok := result["definition"].(string); ok && definition != "" {
		return definition
	}
	// Nested Neo4j node formats (from HDN /knowledge/query): keys like "n","c","a","b"
	// Expect shape: map[key] -> map{"Props": map{"name":..., "definition":..., "domain":...}}
	for _, k := range []string{"n", "c", "a", "b", "related"} {
		if node, ok := result[k].(map[string]interface{}); ok {
			if props, ok := node["Props"].(map[string]interface{}); ok {
				if name, ok := props["name"].(string); ok && name != "" {
					return name
				}
				if definition, ok := props["definition"].(string); ok && definition != "" {
					return definition
				}
			}
		}
	}
	return "Unknown concept"
}

func (re *ReasoningEngine) extractDomainFromResult(result map[string]interface{}) string {
	if domain, ok := result["domain"].(string); ok && domain != "" {
		return domain
	}
	for _, k := range []string{"n", "c", "a", "b", "related"} {
		if node, ok := result[k].(map[string]interface{}); ok {
			if props, ok := node["Props"].(map[string]interface{}); ok {
				if d, ok := props["domain"].(string); ok && d != "" {
					return d
				}
			}
		}
	}
	return "General"
}

func (re *ReasoningEngine) getInferenceRules(domain string) ([]InferenceRule, error) {
	// Get rules from Redis or default rules
	key := fmt.Sprintf("inference_rules:%s", domain)
	rulesData, err := re.redis.LRange(re.ctx, key, 0, -1).Result()
	if err != nil {
		// Return default rules if none stored
		return re.getDefaultInferenceRules(domain), nil
	}

	var rules []InferenceRule
	for _, ruleData := range rulesData {
		var rule InferenceRule
		if err := json.Unmarshal([]byte(ruleData), &rule); err == nil {
			rules = append(rules, rule)
		}
	}

	// If no rules found in Redis, return default rules
	if len(rules) == 0 {
		return re.getDefaultInferenceRules(domain), nil
	}

	return rules, nil
}

func (re *ReasoningEngine) getDefaultInferenceRules(domain string) []InferenceRule {
	return []InferenceRule{
		{
			ID:          "academic_field_classification",
			Name:        "Academic Field Classification",
			Pattern:     "MATCH (a:Concept) WHERE a.domain = $domain AND (a.definition CONTAINS 'study' OR a.definition CONTAINS 'science' OR a.definition CONTAINS 'field' OR a.definition CONTAINS 'discipline') RETURN a",
			Conclusion:  "ACADEMIC_FIELD",
			Confidence:  0.85, // Increased from 0.8
			Domain:      domain,
			Description: "Identify academic fields based on definition keywords",
			Examples:    []string{"Concepts with 'study', 'science', 'field', or 'discipline' in definition are academic fields"},
		},
		{
			ID:          "technology_classification",
			Name:        "Technology Classification",
			Pattern:     "MATCH (a:Concept) WHERE a.domain = $domain AND (a.definition CONTAINS 'technology' OR a.definition CONTAINS 'machine' OR a.definition CONTAINS 'system' OR a.definition CONTAINS 'device') RETURN a",
			Conclusion:  "TECHNOLOGY",
			Confidence:  0.85, // Increased from 0.8
			Domain:      domain,
			Description: "Identify technology-related concepts",
			Examples:    []string{"Concepts with 'technology', 'machine', 'system', or 'device' in definition are technologies"},
		},
		{
			ID:          "concept_similarity",
			Name:        "Concept Similarity",
			Pattern:     "MATCH (a:Concept), (b:Concept) WHERE a.domain = $domain AND b.domain = $domain AND a.name <> b.name AND (a.name CONTAINS b.name OR b.name CONTAINS a.name OR a.name =~ b.name OR b.name =~ a.name) RETURN a, b",
			Conclusion:  "SIMILAR_TO",
			Confidence:  0.7,
			Domain:      domain,
			Description: "Find similar concepts based on name similarity",
			Examples:    []string{"Computer and Computing are similar concepts"},
		},
		{
			ID:          "domain_relationships",
			Name:        "Domain Relationships",
			Pattern:     "MATCH (a:Concept), (b:Concept) WHERE a.domain = $domain AND b.domain = $domain AND a.name <> b.name AND (a.definition CONTAINS b.name OR b.definition CONTAINS a.name) RETURN a, b",
			Conclusion:  "RELATED_TO",
			Confidence:  0.6,
			Domain:      domain,
			Description: "Find concepts that reference each other in their definitions",
			Examples:    []string{"Concepts that mention each other in their definitions are related"},
		},
		{
			ID:          "practical_application",
			Name:        "Practical Application",
			Pattern:     "MATCH (a:Concept) WHERE a.domain = $domain AND (a.definition CONTAINS 'practice' OR a.definition CONTAINS 'application' OR a.definition CONTAINS 'use' OR a.definition CONTAINS 'implement') RETURN a",
			Conclusion:  "PRACTICAL_APPLICATION",
			Confidence:  0.75, // Increased from 0.7
			Domain:      domain,
			Description: "Identify concepts with practical applications",
			Examples:    []string{"Concepts with 'practice', 'application', 'use', or 'implement' in definition are practical"},
		},
	}
}

func (re *ReasoningEngine) applyInferenceRule(rule InferenceRule) ([]Belief, error) {
	// Replace $domain parameter with actual domain value
	query := strings.ReplaceAll(rule.Pattern, "$domain", fmt.Sprintf("'%s'", rule.Domain))
	log.Printf("🔍 Applying rule %s with query: %s", rule.ID, query)

	// Execute the pattern query
	results, err := re.executeCypherQuery(query)
	if err != nil {
		log.Printf("❌ Rule %s query failed: %v", rule.ID, err)
		return nil, err
	}

	log.Printf("📊 Rule %s returned %d results", rule.ID, len(results))

	var newBeliefs []Belief

	// For each match, create the conclusion
	for i, result := range results {
		// Create new belief based on the conclusion pattern
		belief := Belief{
			ID:          fmt.Sprintf("inferred_%s_%d_%d", rule.ID, time.Now().UnixNano(), i),
			Statement:   re.generateStatementFromConclusion(rule.Conclusion, result),
			Confidence:  rule.Confidence,
			Source:      "inference_rule",
			Domain:      rule.Domain,
			Evidence:    []string{result.ID},
			CreatedAt:   time.Now(),
			LastUpdated: time.Now(),
		}
		log.Printf("✨ Created belief: %s", belief.Statement)
		newBeliefs = append(newBeliefs, belief)
	}

	log.Printf("✅ Rule %s generated %d new beliefs", rule.ID, len(newBeliefs))
	return newBeliefs, nil
}

func (re *ReasoningEngine) generateStatementFromConclusion(conclusion string, evidence Belief) string {
	// Generate a human-readable statement from the conclusion pattern
	switch conclusion {
	case "ACADEMIC_FIELD":
		return fmt.Sprintf("'%s' is an academic field based on definition analysis", evidence.Statement)
	case "TECHNOLOGY":
		return fmt.Sprintf("'%s' is a technology-related concept", evidence.Statement)
	case "SIMILAR_TO":
		return fmt.Sprintf("Concepts are similar based on name matching")
	case "RELATED_TO":
		return fmt.Sprintf("Concepts are related based on definition cross-references")
	case "PRACTICAL_APPLICATION":
		return fmt.Sprintf("'%s' has practical applications", evidence.Statement)
	default:
		return fmt.Sprintf("Inferred relationship: %s", conclusion)
	}
}

func (re *ReasoningEngine) generateGapFillingGoals(domain string) ([]CuriosityGoal, error) {
	// Find concepts without relationships or definitions
	query := fmt.Sprintf("MATCH (c:Concept) WHERE c.domain = '%s' AND (NOT (c)-[:RELATED_TO]->() OR c.definition IS NULL) RETURN c", domain)
	results, err := re.executeCypherQuery(query)
	if err != nil {
		return nil, err
	}

	var goals []CuriosityGoal
	for i, result := range results {
		concept := result.Statement

		goal := CuriosityGoal{
			ID:          fmt.Sprintf("gap_filling_%d_%d", time.Now().UnixNano(), i),
			Type:        "gap_filling",
			Description: fmt.Sprintf("Use tool_http_get to fetch Wikipedia page for '%s' and extract knowledge", concept),
			Domain:      domain,
			Priority:    7,
			Status:      "pending",
			Targets:     []string{concept},
			CreatedAt:   time.Now(),
		}
		goals = append(goals, goal)
	}

	return goals, nil
}

func (re *ReasoningEngine) generateContradictionGoals(domain string) ([]CuriosityGoal, error) {
	// Look for potential contradictions in the knowledge base
	// This is simplified - in reality would use more sophisticated contradiction detection
	return []CuriosityGoal{
		{
			ID:          fmt.Sprintf("contradiction_%d", time.Now().UnixNano()),
			Type:        "contradiction_resolution",
			Description: "Resolve any contradictions in the knowledge base",
			Domain:      domain,
			Priority:    8,
			Status:      "pending",
			Targets:     []string{},
			CreatedAt:   time.Now(),
		},
	}, nil
}

func (re *ReasoningEngine) generateExplorationGoals(domain string) ([]CuriosityGoal, error) {
	// Generate goals to explore new concepts and relationships
	return []CuriosityGoal{
		{
			ID:          fmt.Sprintf("exploration_%d", time.Now().UnixNano()),
			Type:        "concept_exploration",
			Description: "Use tool_http_get to fetch recent Wikipedia articles about domain concepts",
			Domain:      domain,
			Priority:    5,
			Status:      "pending",
			Targets:     []string{},
			CreatedAt:   time.Now(),
		},
	}, nil
}

func (re *ReasoningEngine) generateNewsCuriosityGoals(domain string) ([]CuriosityGoal, error) {
	// Generate goals based on recent news events
	var goals []CuriosityGoal

	// Check for recent news relations
	relationsKey := "reasoning:news_relations:recent"
	processedKey := "reasoning:news_relations:processed"
	relations, err := re.redis.LRange(re.ctx, relationsKey, 0, 9).Result()
	if err == nil && len(relations) > 0 {
		// Get already processed news relations
		processed, _ := re.redis.SMembers(re.ctx, processedKey).Result()
		processedSet := make(map[string]bool)
		for _, p := range processed {
			processedSet[p] = true
		}

		for i, relationData := range relations {
			// Create a hash of the relation for deduplication
			relationHash := fmt.Sprintf("%x", sha256.Sum256([]byte(relationData)))

			// Skip if already processed
			if processedSet[relationHash] {
				continue
			}

			var relation map[string]interface{}
			if err := json.Unmarshal([]byte(relationData), &relation); err == nil {
				head, _ := relation["head"].(string)
				relationType, _ := relation["relation"].(string)
				tail, _ := relation["tail"].(string)

				if head != "" && relationType != "" && tail != "" {
					goal := CuriosityGoal{
						ID:          fmt.Sprintf("news_relation_%d_%d", time.Now().UnixNano(), i),
						Type:        "news_analysis",
						Description: fmt.Sprintf("Use tool_http_get to fetch Wikipedia pages for '%s' and '%s' to verify relation: %s", head, tail, relationType),
						Domain:      domain,
						Priority:    6,
						Status:      "pending",
						Targets:     []string{head, tail},
						CreatedAt:   time.Now(),
					}
					goals = append(goals, goal)

					// Mark this relation as processed
					re.redis.SAdd(re.ctx, processedKey, relationHash)
				}
			}
		}
	}

	// Check for recent news alerts
	alertsKey := "reasoning:news_alerts:recent"
	processedAlertsKey := "reasoning:news_alerts:processed"
	alerts, err := re.redis.LRange(re.ctx, alertsKey, 0, 4).Result()
	if err == nil && len(alerts) > 0 {
		// Get already processed news alerts
		processedAlerts, _ := re.redis.SMembers(re.ctx, processedAlertsKey).Result()
		processedAlertsSet := make(map[string]bool)
		for _, p := range processedAlerts {
			processedAlertsSet[p] = true
		}

		for i, alertData := range alerts {
			// Create a hash of the alert for deduplication
			alertHash := fmt.Sprintf("%x", sha256.Sum256([]byte(alertData)))

			// Skip if already processed
			if processedAlertsSet[alertHash] {
				continue
			}

			var alert map[string]interface{}
			if err := json.Unmarshal([]byte(alertData), &alert); err == nil {
				headline, _ := alert["headline"].(string)
				impact, _ := alert["impact"].(string)

				if headline != "" {
					priority := 5
					if impact == "high" {
						priority = 9
					} else if impact == "medium" {
						priority = 7
					}

					goal := CuriosityGoal{
						ID:          fmt.Sprintf("news_alert_%d_%d", time.Now().UnixNano(), i),
						Type:        "news_analysis",
						Description: fmt.Sprintf("Use tool_http_get to fetch news article about '%s' and analyze impact: %s", headline, impact),
						Domain:      domain,
						Priority:    priority,
						Status:      "pending",
						Targets:     []string{headline},
						CreatedAt:   time.Now(),
					}
					goals = append(goals, goal)

					// Mark this alert as processed
					re.redis.SAdd(re.ctx, processedAlertsKey, alertHash)
				}
			}
		}
	}

	log.Printf("📰 Generated %d news-driven curiosity goals", len(goals))
	return goals, nil
}

func (re *ReasoningEngine) getReasoningTraces(goal, domain string, limit int) ([]ReasoningTrace, error) {
	// Try multiple key patterns to find traces
	keys := []string{
		fmt.Sprintf("reasoning:traces:goal:%s", goal),
		fmt.Sprintf("reasoning:traces:domain:%s", domain),
		"reasoning:traces:all",
	}

	var allTraces []ReasoningTrace

	for _, key := range keys {
		tracesData, err := re.redis.LRange(re.ctx, key, 0, int64(limit-1)).Result()
		if err != nil {
			continue
		}

		for _, traceData := range tracesData {
			var trace ReasoningTrace
			if err := json.Unmarshal([]byte(traceData), &trace); err == nil {
				// Filter by goal and domain if specified
				if goal != "" && trace.Goal != goal && !strings.Contains(strings.ToLower(trace.Goal), strings.ToLower(goal)) {
					continue
				}
				if domain != "" && trace.Domain != domain {
					continue
				}
				allTraces = append(allTraces, trace)
			}
		}

		// If we found traces, return them (limit to requested amount)
		if len(allTraces) > 0 {
			if len(allTraces) > limit {
				allTraces = allTraces[:limit]
			}
			return allTraces, nil
		}
	}

	return allTraces, nil
}

// cleanupOldGoals removes old and completed goals to prevent Redis list from growing indefinitely
func (re *ReasoningEngine) cleanupOldGoals(domain string) {
	key := fmt.Sprintf("reasoning:curiosity_goals:%s", domain)
	goalsData, err := re.redis.LRange(re.ctx, key, 0, 199).Result()
	if err != nil {
		return
	}

	var activeGoals []string
	cutoffTime := time.Now().Add(-24 * time.Hour) // Remove goals older than 24 hours

	for _, goalData := range goalsData {
		var goal CuriosityGoal
		if err := json.Unmarshal([]byte(goalData), &goal); err == nil {
			// Keep goals that are:
			// 1. Not completed or failed
			// 2. Not older than 24 hours
			// 3. Not duplicate generic goals
			shouldKeep := goal.Status != "completed" &&
				goal.Status != "failed" &&
				goal.CreatedAt.After(cutoffTime) &&
				!re.isGenericGoal(goal)

			if shouldKeep {
				activeGoals = append(activeGoals, goalData)
			}
		}
	}

	// Replace the list with only active goals
	if len(activeGoals) < len(goalsData) {
		// Clear the list
		re.redis.Del(re.ctx, key)

		// Add back only active goals
		for _, goalData := range activeGoals {
			re.redis.LPush(re.ctx, key, goalData)
		}

		log.Printf("🧹 Cleaned up %d old/completed goals, kept %d active goals",
			len(goalsData)-len(activeGoals), len(activeGoals))
	}
}

// isGenericGoal checks if a goal is a generic/duplicate goal that should be cleaned up
func (re *ReasoningEngine) isGenericGoal(goal CuriosityGoal) bool {
	// Check for generic exploration goals
	if goal.Type == "concept_exploration" && goal.Description == "Explore new concepts and relationships in the domain" {
		return true
	}

	// Check for generic contradiction goals
	if goal.Type == "contradiction_resolution" && goal.Description == "Resolve any contradictions in the knowledge base" {
		return true
	}

	// Check for generic hypothesis testing goals with vague descriptions
	if goal.Type == "hypothesis_testing" {
		desc := strings.ToLower(goal.Description)

		// Check for nested vague descriptions (multiple colons indicate nesting)
		colonCount := strings.Count(desc, ":")
		if colonCount > 2 {
			// Likely a nested vague description
			return true
		}

		// Generic patterns that indicate useless goals
		genericPatterns := []string{
			"apply insights from system state",
			"improve our general approach",
			"improve general performance",
			"optimize the ai capability control system",
			"if we apply insights",
			"we can improve",
			"learn to discover new",
			"discover new general opportunities",
			"investigate system state: learn",
		}
		for _, pattern := range genericPatterns {
			if strings.Contains(desc, pattern) {
				return true
			}
		}

		// Check for overly vague descriptions with multiple question prefixes
		vaguePrefixes := []string{
			"test hypothesis: how can we better test:",
			"test hypothesis: what additional evidence would support:",
			"test hypothesis: what are the specific conditions for:",
			"test hypothesis: what are the implications of:",
		}
		for _, prefix := range vaguePrefixes {
			if strings.HasPrefix(desc, prefix) {
				return true
			}
		}

		// Check if description is too vague (less than 30 chars or very repetitive)
		if len(goal.Description) < 30 {
			return true
		}
	}

	return false
}

// calculateBeliefConfidence calculates confidence based on data quality
func (re *ReasoningEngine) calculateBeliefConfidence(result map[string]interface{}, statement string) float64 {
	baseConfidence := 0.7 // Lowered from 0.8 to be less harsh

	// Penalty for "Unknown concept" (no real data)
	if statement == "Unknown concept" || strings.TrimSpace(statement) == "" {
		return 0.3
	}

	// Check if this is an existing concept (has a name in knowledge base)
	isExistingConcept := false
	for _, k := range []string{"n", "c", "a", "b", "related"} {
		if node, ok := result[k].(map[string]interface{}); ok {
			if props, ok := node["Props"].(map[string]interface{}); ok {
				// If concept exists in KB, give it baseline confidence
				if name, ok := props["name"].(string); ok && name != "" {
					isExistingConcept = true
					// Existing concepts get baseline boost (they're known entities)
					baseConfidence = 0.6 // Baseline for existing concepts
					break
				}
			}
		}
	}

	// Bonus for having a proper definition
	hasDefinition := false
	for _, k := range []string{"n", "c", "a", "b", "related"} {
		if node, ok := result[k].(map[string]interface{}); ok {
			if props, ok := node["Props"].(map[string]interface{}); ok {
				if def, ok := props["definition"].(string); ok && len(strings.TrimSpace(def)) > 20 {
					hasDefinition = true
					break
				}
			}
		}
	}
	if hasDefinition {
		baseConfidence += 0.15 // Increased bonus for definitions
	} else if !isExistingConcept {
		// Only penalize if it's not an existing concept
		baseConfidence -= 0.1 // Reduced penalty from 0.2
	}

	// Reduced penalty for short statements (many valid concepts have short names)
	if len(statement) < 10 && !isExistingConcept {
		baseConfidence -= 0.1 // Reduced penalty from 0.15
	}

	// Bonus for longer, more detailed statements
	if len(statement) > 50 {
		baseConfidence += 0.05
	}

	// Ensure confidence stays in valid range [0, 1]
	if baseConfidence > 1.0 {
		baseConfidence = 1.0
	}
	if baseConfidence < 0.0 {
		baseConfidence = 0.0
	}

	return baseConfidence
}

// assessBeliefValue uses LLM to assess if a belief from the knowledge base is worth storing
// This prevents storing obvious/common knowledge that's already in the KB
func (re *ReasoningEngine) assessBeliefValue(statement string, domain string) (bool, bool, error) {
	if len(strings.TrimSpace(statement)) == 0 {
		return false, false, nil
	}

	// Create prompt for LLM assessment
	prompt := fmt.Sprintf(`Assess whether this knowledge from the knowledge base is worth storing as a belief.

Knowledge: %s
Domain: %s

This knowledge already exists in the knowledge base. Should we store it as a belief?

Evaluate:
1. NOVELTY: Is this knowledge novel/interesting, or is it obvious/common knowledge?
2. VALUE: Is this knowledge worth storing as a belief? Will it help with tasks or decisions?

Return JSON:
{
  "is_novel": true/false,
  "is_worth_learning": true/false,
  "reasoning": "Brief explanation"
}

Be strict: Only mark as worth learning if:
- The knowledge is actionable and useful
- The knowledge will help accomplish tasks
- The knowledge is not obvious/common knowledge
- The knowledge adds value beyond just existing in the KB

If the knowledge is obvious, common knowledge, or not actionable, mark is_worth_learning=false.`, statement, domain)

	// Call HDN interpret endpoint
	interpretURL := fmt.Sprintf("%s/api/v1/interpret", re.hdnURL)
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

	resp, err := re.httpClient.Post(interpretURL, "application/json", bytes.NewReader(reqJSON))
	if err != nil {
		return false, false, fmt.Errorf("belief assessment LLM call failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false, false, fmt.Errorf("belief assessment returned status %d", resp.StatusCode)
	}

	var interpretResp map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&interpretResp); err != nil {
		return false, false, fmt.Errorf("failed to decode assessment response: %w", err)
	}

	// Extract assessment from response
	// The /api/v1/interpret endpoint returns InterpretationResult with tasks/message
	// The LLM response (including JSON) is in the task description
	var assessmentJSON string

	// Try to get JSON from task descriptions first (most likely location)
	if tasks, ok := interpretResp["tasks"].([]interface{}); ok && len(tasks) > 0 {
		for _, t := range tasks {
			if task, ok := t.(map[string]interface{}); ok {
				// Check description field
				if desc, ok := task["description"].(string); ok && desc != "" {
					assessmentJSON = desc
					break
				}
			}
		}
	}

	// Fallback to message field
	if assessmentJSON == "" {
		if msg, ok := interpretResp["message"].(string); ok && msg != "" {
			assessmentJSON = msg
		}
	}

	// Final fallback: try result/output fields (for other endpoints)
	if assessmentJSON == "" {
		if result, ok := interpretResp["result"].(string); ok {
			assessmentJSON = result
		} else if output, ok := interpretResp["output"].(string); ok {
			assessmentJSON = output
		} else {
			return false, false, fmt.Errorf("no assessment data in response")
		}
	}

	// Extract JSON from the text (the LLM response may contain JSON embedded in text)
	// Look for JSON object in the response
	start := strings.Index(assessmentJSON, "{")
	end := strings.LastIndex(assessmentJSON, "}")
	if start >= 0 && end > start {
		assessmentJSON = assessmentJSON[start : end+1]
	} else {
		return false, false, fmt.Errorf("no JSON found in assessment response: %s", assessmentJSON[:minInt(100, len(assessmentJSON))])
	}

	// Parse assessment JSON (already extracted above)
	var assessment map[string]interface{}
	if err := json.Unmarshal([]byte(assessmentJSON), &assessment); err != nil {
		return false, false, fmt.Errorf("failed to parse assessment JSON: %w (extracted: %s)", err, assessmentJSON[:minInt(200, len(assessmentJSON))])
	}

	// Extract values
	isNovel, _ := assessment["is_novel"].(bool)
	isWorthLearning, _ := assessment["is_worth_learning"].(bool)

	reasoning, _ := assessment["reasoning"].(string)
	if reasoning != "" {
		log.Printf("🧠 Belief assessment: '%s' - novel=%v, worth_learning=%v, reasoning=%s",
			statement[:minInt(50, len(statement))], isNovel, isWorthLearning, reasoning)
	}

	return isNovel, isWorthLearning, nil
}
