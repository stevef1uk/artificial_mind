package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// ActiveLearningLoop implements query-driven learning that identifies
// high-uncertainty concepts and generates targeted data acquisition plans
// to reduce uncertainty fastest.
type ActiveLearningLoop struct {
	redis     *redis.Client
	ctx       context.Context
	reasoning *ReasoningEngine
	hdnURL    string
}

// NewActiveLearningLoop creates a new active learning loop system
func NewActiveLearningLoop(redis *redis.Client, ctx context.Context, reasoning *ReasoningEngine, hdnURL string) *ActiveLearningLoop {
	return &ActiveLearningLoop{
		redis:     redis,
		ctx:       ctx,
		reasoning: reasoning,
		hdnURL:    hdnURL,
	}
}

// HighUncertaintyConcept represents a concept with high uncertainty that needs investigation
type HighUncertaintyConcept struct {
	ConceptName                   string    `json:"concept_name"`
	Domain                        string    `json:"domain"`
	EpistemicUncertainty          float64   `json:"epistemic_uncertainty"`
	AleatoricUncertainty          float64   `json:"aleatoric_uncertainty"`
	CalibratedConfidence          float64   `json:"calibrated_confidence"`
	UncertaintyReductionPotential float64   `json:"uncertainty_reduction_potential"` // How much we can reduce uncertainty
	Sources                       []string  `json:"sources"`                         // Where this uncertainty comes from (beliefs, hypotheses, goals)
	EvidenceCount                 int       `json:"evidence_count"`
	LastInvestigated              time.Time `json:"last_investigated"`
}

// DataAcquisitionPlan represents a structured plan to acquire data to reduce uncertainty
type DataAcquisitionPlan struct {
	ID                            string            `json:"id"`
	TargetConcept                 string            `json:"target_concept"`
	Domain                        string            `json:"domain"`
	UncertaintyReductionPotential float64           `json:"uncertainty_reduction_potential"`
	Priority                      int               `json:"priority"` // 1-10, higher is more important
	AcquisitionSteps              []AcquisitionStep `json:"acquisition_steps"`
	ExpectedOutcome               string            `json:"expected_outcome"`
	EstimatedTime                 time.Duration     `json:"estimated_time"`
	CreatedAt                     time.Time         `json:"created_at"`
}

// AcquisitionStep represents a single step in a data acquisition plan
type AcquisitionStep struct {
	StepNumber                   int     `json:"step_number"`
	Action                       string  `json:"action"` // e.g., "query_knowledge_base", "fetch_external_data", "run_experiment"
	Description                  string  `json:"description"`
	Target                       string  `json:"target"`                         // What to query/fetch
	ExpectedUncertaintyReduction float64 `json:"expected_uncertainty_reduction"` // How much this step reduces uncertainty
	Tool                         string  `json:"tool"`                           // Which tool to use
}

// IdentifyHighUncertaintyConcepts identifies concepts with high epistemic uncertainty
// that can be reduced through targeted data acquisition
func (all *ActiveLearningLoop) IdentifyHighUncertaintyConcepts(domain string, threshold float64) ([]HighUncertaintyConcept, error) {
	log.Printf("🔍 [ACTIVE-LEARNING] Identifying high-uncertainty concepts in domain: %s (threshold: %.2f)", domain, threshold)

	var highUncertaintyConcepts []HighUncertaintyConcept
	conceptMap := make(map[string]*HighUncertaintyConcept)

	// 1. Check beliefs for high uncertainty
	beliefs, err := all.getBeliefsWithUncertainty(domain)
	if err == nil {
		for _, belief := range beliefs {
			if belief.Uncertainty != nil && belief.Uncertainty.EpistemicUncertainty >= threshold {
				conceptName := all.extractConceptFromStatement(belief.Statement)
				if conceptName != "" {
					if existing, exists := conceptMap[conceptName]; exists {
						// Update if this belief has higher uncertainty
						if belief.Uncertainty.EpistemicUncertainty > existing.EpistemicUncertainty {
							existing.EpistemicUncertainty = belief.Uncertainty.EpistemicUncertainty
							existing.AleatoricUncertainty = belief.Uncertainty.AleatoricUncertainty
							existing.CalibratedConfidence = belief.Uncertainty.CalibratedConfidence
							existing.Sources = append(existing.Sources, fmt.Sprintf("belief:%s", belief.ID))
						}
					} else {
						conceptMap[conceptName] = &HighUncertaintyConcept{
							ConceptName:          conceptName,
							Domain:               domain,
							EpistemicUncertainty: belief.Uncertainty.EpistemicUncertainty,
							AleatoricUncertainty: belief.Uncertainty.AleatoricUncertainty,
							CalibratedConfidence: belief.Uncertainty.CalibratedConfidence,
							Sources:              []string{fmt.Sprintf("belief:%s", belief.ID)},
							EvidenceCount:        len(belief.Evidence),
						}
					}
				}
			}
		}
	}

	// 2. Check hypotheses for high uncertainty
	hypotheses, err := all.getHypothesesWithUncertainty(domain)
	if err == nil {
		for _, hypothesis := range hypotheses {
			if hypothesis.Uncertainty != nil && hypothesis.Uncertainty.EpistemicUncertainty >= threshold {
				conceptName := all.extractConceptFromDescription(hypothesis.Description)
				if conceptName != "" {
					if existing, exists := conceptMap[conceptName]; exists {
						if hypothesis.Uncertainty.EpistemicUncertainty > existing.EpistemicUncertainty {
							existing.EpistemicUncertainty = hypothesis.Uncertainty.EpistemicUncertainty
							existing.AleatoricUncertainty = hypothesis.Uncertainty.AleatoricUncertainty
							existing.CalibratedConfidence = hypothesis.Uncertainty.CalibratedConfidence
							existing.Sources = append(existing.Sources, fmt.Sprintf("hypothesis:%s", hypothesis.ID))
						}
					} else {
						conceptMap[conceptName] = &HighUncertaintyConcept{
							ConceptName:          conceptName,
							Domain:               domain,
							EpistemicUncertainty: hypothesis.Uncertainty.EpistemicUncertainty,
							AleatoricUncertainty: hypothesis.Uncertainty.AleatoricUncertainty,
							CalibratedConfidence: hypothesis.Uncertainty.CalibratedConfidence,
							Sources:              []string{fmt.Sprintf("hypothesis:%s", hypothesis.ID)},
							EvidenceCount:        len(hypothesis.Facts),
						}
					}
				}
			}
		}
	}

	// 3. Check goals for high uncertainty
	goals, err := all.getGoalsWithUncertainty(domain)
	if err == nil {
		for _, goal := range goals {
			if goal.Uncertainty != nil && goal.Uncertainty.EpistemicUncertainty >= threshold {
				conceptName := ""
				if len(goal.Targets) > 0 {
					conceptName = goal.Targets[0]
				} else {
					conceptName = all.extractConceptFromDescription(goal.Description)
				}
				if conceptName != "" {
					if existing, exists := conceptMap[conceptName]; exists {
						if goal.Uncertainty.EpistemicUncertainty > existing.EpistemicUncertainty {
							existing.EpistemicUncertainty = goal.Uncertainty.EpistemicUncertainty
							existing.AleatoricUncertainty = goal.Uncertainty.AleatoricUncertainty
							existing.CalibratedConfidence = goal.Uncertainty.CalibratedConfidence
							existing.Sources = append(existing.Sources, fmt.Sprintf("goal:%s", goal.ID))
						}
					} else {
						conceptMap[conceptName] = &HighUncertaintyConcept{
							ConceptName:          conceptName,
							Domain:               domain,
							EpistemicUncertainty: goal.Uncertainty.EpistemicUncertainty,
							AleatoricUncertainty: goal.Uncertainty.AleatoricUncertainty,
							CalibratedConfidence: goal.Uncertainty.CalibratedConfidence,
							Sources:              []string{fmt.Sprintf("goal:%s", goal.ID)},
							EvidenceCount:        0,
						}
					}
				}
			}
		}
	}

	// Convert map to slice and calculate uncertainty reduction potential
	for _, concept := range conceptMap {
		// Uncertainty reduction potential = epistemic uncertainty * (1 - aleatoric uncertainty)
		// Higher epistemic + lower aleatoric = more reducible
		concept.UncertaintyReductionPotential = concept.EpistemicUncertainty * (1.0 - concept.AleatoricUncertainty)
		highUncertaintyConcepts = append(highUncertaintyConcepts, *concept)
	}

	// Sort by uncertainty reduction potential (highest first)
	sort.Slice(highUncertaintyConcepts, func(i, j int) bool {
		return highUncertaintyConcepts[i].UncertaintyReductionPotential > highUncertaintyConcepts[j].UncertaintyReductionPotential
	})

	log.Printf("✅ [ACTIVE-LEARNING] Identified %d high-uncertainty concepts", len(highUncertaintyConcepts))
	return highUncertaintyConcepts, nil
}

// GenerateDataAcquisitionPlans creates targeted plans to acquire data for high-uncertainty concepts
func (all *ActiveLearningLoop) GenerateDataAcquisitionPlans(concepts []HighUncertaintyConcept, maxPlans int) ([]DataAcquisitionPlan, error) {
	log.Printf("📋 [ACTIVE-LEARNING] Generating data acquisition plans for %d concepts", len(concepts))

	if maxPlans <= 0 {
		maxPlans = 10
	}

	var plans []DataAcquisitionPlan

	for i, concept := range concepts {
		if i >= maxPlans {
			break
		}

		plan := all.createDataAcquisitionPlan(concept)
		plans = append(plans, plan)
	}

	// Sort by priority (highest first)
	sort.Slice(plans, func(i, j int) bool {
		return plans[i].Priority > plans[j].Priority
	})

	log.Printf("✅ [ACTIVE-LEARNING] Generated %d data acquisition plans", len(plans))
	return plans, nil
}

// createDataAcquisitionPlan creates a structured plan for a single concept
func (all *ActiveLearningLoop) createDataAcquisitionPlan(concept HighUncertaintyConcept) DataAcquisitionPlan {
	plan := DataAcquisitionPlan{
		ID:                            fmt.Sprintf("active_learning_plan_%s_%d", concept.ConceptName, time.Now().UnixNano()),
		TargetConcept:                 concept.ConceptName,
		Domain:                        concept.Domain,
		UncertaintyReductionPotential: concept.UncertaintyReductionPotential,
		Priority:                      all.calculatePlanPriority(concept),
		AcquisitionSteps:              all.generateAcquisitionSteps(concept),
		ExpectedOutcome: fmt.Sprintf("Reduce epistemic uncertainty for '%s' from %.2f to <%.2f",
			concept.ConceptName, concept.EpistemicUncertainty, concept.EpistemicUncertainty*0.5),
		EstimatedTime: all.estimatePlanTime(concept),
		CreatedAt:     time.Now(),
	}

	return plan
}

// calculatePlanPriority calculates priority based on uncertainty reduction potential
func (all *ActiveLearningLoop) calculatePlanPriority(concept HighUncertaintyConcept) int {
	// Base priority from uncertainty reduction potential (0-1 -> 1-10)
	basePriority := int(concept.UncertaintyReductionPotential*9) + 1

	// Boost priority if concept has low evidence count (more learnable)
	if concept.EvidenceCount < 3 {
		basePriority += 2
	}

	// Boost priority if concept hasn't been investigated recently
	if concept.LastInvestigated.IsZero() || time.Since(concept.LastInvestigated) > 24*time.Hour {
		basePriority += 1
	}

	// Clamp to 1-10
	if basePriority > 10 {
		basePriority = 10
	}
	if basePriority < 1 {
		basePriority = 1
	}

	return basePriority
}

// generateAcquisitionSteps creates a sequence of steps to acquire data
func (all *ActiveLearningLoop) generateAcquisitionSteps(concept HighUncertaintyConcept) []AcquisitionStep {
	var steps []AcquisitionStep

	// Step 1: Query knowledge base for existing information
	steps = append(steps, AcquisitionStep{
		StepNumber:                   1,
		Action:                       "query_knowledge_base",
		Description:                  fmt.Sprintf("Query Neo4j knowledge base for existing information about '%s'", concept.ConceptName),
		Target:                       concept.ConceptName,
		ExpectedUncertaintyReduction: concept.EpistemicUncertainty * 0.2, // 20% reduction from existing knowledge
		Tool:                         "tool_mcp_query_neo4j",
	})

	// Step 2: Fetch external data if concept is well-defined
	if concept.EvidenceCount > 0 {
		steps = append(steps, AcquisitionStep{
			StepNumber:                   2,
			Action:                       "fetch_external_data",
			Description:                  fmt.Sprintf("Fetch Wikipedia or external sources about '%s'", concept.ConceptName),
			Target:                       concept.ConceptName,
			ExpectedUncertaintyReduction: concept.EpistemicUncertainty * 0.3, // 30% reduction from external data
			Tool:                         "tool_http_get",
		})
	}

	// Step 3: Generate hypothesis and test if uncertainty is still high
	if concept.EpistemicUncertainty > 0.5 {
		steps = append(steps, AcquisitionStep{
			StepNumber:                   3,
			Action:                       "generate_and_test_hypothesis",
			Description:                  fmt.Sprintf("Generate testable hypothesis about '%s' and design experiment", concept.ConceptName),
			Target:                       concept.ConceptName,
			ExpectedUncertaintyReduction: concept.EpistemicUncertainty * 0.4, // 40% reduction from testing
			Tool:                         "tool_hypothesis_generator",
		})
	}

	return steps
}

// estimatePlanTime estimates how long a plan will take
func (all *ActiveLearningLoop) estimatePlanTime(concept HighUncertaintyConcept) time.Duration {
	// Base time: 5 minutes per step
	baseTime := 5 * time.Minute

	// More steps for higher uncertainty
	stepCount := len(all.generateAcquisitionSteps(concept))
	estimatedTime := time.Duration(stepCount) * baseTime

	// Add buffer for complex concepts
	if concept.EvidenceCount == 0 {
		estimatedTime += 10 * time.Minute
	}

	return estimatedTime
}

// PrioritizeExperiments ranks experiments by their potential to reduce uncertainty fastest
func (all *ActiveLearningLoop) PrioritizeExperiments(plans []DataAcquisitionPlan) []DataAcquisitionPlan {
	log.Printf("⚡ [ACTIVE-LEARNING] Prioritizing %d experiments by uncertainty reduction speed", len(plans))

	// Calculate efficiency score: uncertainty reduction potential / estimated time
	type scoredPlan struct {
		Plan  DataAcquisitionPlan
		Score float64
	}

	var scoredPlans []scoredPlan
	for _, plan := range plans {
		// Efficiency = uncertainty reduction potential / time (in hours)
		timeHours := plan.EstimatedTime.Hours()
		if timeHours < 0.1 {
			timeHours = 0.1 // Minimum 6 minutes
		}
		efficiency := plan.UncertaintyReductionPotential / timeHours

		// Boost score for plans targeting concepts with very high uncertainty
		if plan.UncertaintyReductionPotential > 0.7 {
			efficiency *= 1.5
		}

		scoredPlans = append(scoredPlans, scoredPlan{
			Plan:  plan,
			Score: efficiency,
		})
	}

	// Sort by efficiency (highest first)
	sort.Slice(scoredPlans, func(i, j int) bool {
		return scoredPlans[i].Score > scoredPlans[j].Score
	})

	// Update priorities based on efficiency ranking
	var prioritizedPlans []DataAcquisitionPlan
	for i, sp := range scoredPlans {
		// Recalculate priority based on efficiency rank
		newPriority := 10 - i // Top plan gets priority 10, second gets 9, etc.
		if newPriority < 1 {
			newPriority = 1
		}
		sp.Plan.Priority = newPriority
		prioritizedPlans = append(prioritizedPlans, sp.Plan)

		log.Printf("   📊 Plan %d: %s (efficiency: %.3f, priority: %d)",
			i+1, sp.Plan.TargetConcept, sp.Score, newPriority)
	}

	log.Printf("✅ [ACTIVE-LEARNING] Prioritized %d experiments", len(prioritizedPlans))
	return prioritizedPlans
}

// ConvertPlansToCuriosityGoals converts data acquisition plans into curiosity goals
func (all *ActiveLearningLoop) ConvertPlansToCuriosityGoals(plans []DataAcquisitionPlan) []CuriosityGoal {
	log.Printf("🎯 [ACTIVE-LEARNING] Converting %d plans to curiosity goals", len(plans))

	var goals []CuriosityGoal

	for _, plan := range plans {
		// Create a goal for the first step (most important)
		if len(plan.AcquisitionSteps) > 0 {
			step := plan.AcquisitionSteps[0]

			goal := CuriosityGoal{
				ID:          plan.ID,
				Type:        "active_learning",
				Description: fmt.Sprintf("[ACTIVE-LEARNING] %s: %s", step.Action, step.Description),
				Domain:      plan.Domain,
				Priority:    plan.Priority,
				Status:      "pending",
				Targets:     []string{plan.TargetConcept},
				CreatedAt:   plan.CreatedAt,
				Uncertainty: NewUncertaintyModel(
					plan.UncertaintyReductionPotential,
					plan.UncertaintyReductionPotential, // Epistemic uncertainty = reduction potential
					EstimateAleatoricUncertainty(plan.Domain, "active_learning"),
				),
				Value: plan.UncertaintyReductionPotential,
			}

			goals = append(goals, goal)
		}
	}

	log.Printf("✅ [ACTIVE-LEARNING] Converted %d plans to curiosity goals", len(goals))
	return goals
}

// Helper functions to retrieve data with uncertainty

func (all *ActiveLearningLoop) getBeliefsWithUncertainty(domain string) ([]Belief, error) {
	// Query Redis for beliefs in this domain
	key := fmt.Sprintf("reasoning:beliefs:%s", domain)
	beliefsData, err := all.redis.LRange(all.ctx, key, 0, 100).Result()
	if err != nil {
		return nil, err
	}

	var beliefs []Belief
	for _, data := range beliefsData {
		var belief Belief
		if err := json.Unmarshal([]byte(data), &belief); err == nil {
			beliefs = append(beliefs, belief)
		}
	}

	return beliefs, nil
}

func (all *ActiveLearningLoop) getHypothesesWithUncertainty(domain string) ([]Hypothesis, error) {
	// Query Redis for hypotheses
	key := "fsm:agent_1:hypotheses"
	hypothesesData, err := all.redis.LRange(all.ctx, key, 0, 100).Result()
	if err != nil {
		return nil, err
	}

	var hypotheses []Hypothesis
	for _, data := range hypothesesData {
		var hypothesis Hypothesis
		if err := json.Unmarshal([]byte(data), &hypothesis); err == nil {
			if hypothesis.Domain == domain {
				hypotheses = append(hypotheses, hypothesis)
			}
		}
	}

	return hypotheses, nil
}

func (all *ActiveLearningLoop) getGoalsWithUncertainty(domain string) ([]CuriosityGoal, error) {
	// Query Redis for goals
	key := fmt.Sprintf("reasoning:curiosity_goals:%s", domain)
	goalsData, err := all.redis.LRange(all.ctx, key, 0, 100).Result()
	if err != nil {
		return nil, err
	}

	var goals []CuriosityGoal
	for _, data := range goalsData {
		var goal CuriosityGoal
		if err := json.Unmarshal([]byte(data), &goal); err == nil {
			if goal.Uncertainty != nil {
				goals = append(goals, goal)
			}
		}
	}

	return goals, nil
}

// Helper functions to extract concept names

func (all *ActiveLearningLoop) extractConceptFromStatement(statement string) string {
	// Simple extraction: look for quoted strings or capitalized words
	// This is a heuristic - could be improved with NLP
	words := strings.Fields(statement)
	for _, word := range words {
		word = strings.Trim(word, ".,!?;:")
		if len(word) > 2 && strings.ToUpper(word[:1]) == word[:1] {
			return word
		}
	}
	return ""
}

func (all *ActiveLearningLoop) extractConceptFromDescription(description string) string {
	// Look for patterns like "concept: X" or "about X"
	lower := strings.ToLower(description)

	// Check for "concept:" pattern
	if idx := strings.Index(lower, "concept:"); idx >= 0 {
		rest := description[idx+len("concept:"):]
		words := strings.Fields(rest)
		if len(words) > 0 {
			return strings.Trim(words[0], ".,!?;:")
		}
	}

	// Check for "about" pattern
	if idx := strings.Index(lower, "about"); idx >= 0 {
		rest := description[idx+len("about"):]
		words := strings.Fields(rest)
		if len(words) > 0 {
			return strings.Trim(words[0], ".,!?;:")
		}
	}

	// Fallback: extract first capitalized word
	return all.extractConceptFromStatement(description)
}
