package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// ExplanationLearningFeedback implements explanation-grounded learning feedback
// that closes the loop between reasoning quality → execution outcomes → improved reasoning
type ExplanationLearningFeedback struct {
	redis      *redis.Client
	hdnURL     string
	ctx        context.Context
	httpClient *http.Client
}

// NewExplanationLearningFeedback creates a new explanation learning feedback system
func NewExplanationLearningFeedback(redis *redis.Client, hdnURL string) *ExplanationLearningFeedback {
	return &ExplanationLearningFeedback{
		redis:      redis,
		hdnURL:     hdnURL,
		ctx:        context.Background(),
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// GoalCompletionFeedback represents the feedback collected after a goal is completed
type GoalCompletionFeedback struct {
	GoalID          string    `json:"goal_id"`
	GoalDescription string    `json:"goal_description"`
	Status          string    `json:"status"` // achieved, failed
	CompletedAt     time.Time `json:"completed_at"`
	Domain          string    `json:"domain"`

	// Hypotheses that were associated with this goal
	Hypotheses []HypothesisEvaluation `json:"hypotheses"`

	// Reasoning traces associated with this goal
	ReasoningTraces []ReasoningTraceEvaluation `json:"reasoning_traces"`

	// Outcome metrics
	OutcomeMetrics map[string]interface{} `json:"outcome_metrics"`

	// Overall evaluation
	OverallAccuracy float64 `json:"overall_accuracy"`
	OverallQuality  float64 `json:"overall_quality"`
	AlignmentScore  float64 `json:"alignment_score"`
}

// HypothesisEvaluation evaluates a hypothesis post-hoc
type HypothesisEvaluation struct {
	HypothesisID       string  `json:"hypothesis_id"`
	Description        string  `json:"description"`
	OriginalConfidence float64 `json:"original_confidence"`

	// Post-hoc evaluation
	Accuracy   float64 `json:"accuracy"`    // How accurate was the hypothesis?
	Quality    float64 `json:"quality"`     // Quality of the explanation
	Alignment  float64 `json:"alignment"`   // Alignment with actual outcome
	WasCorrect bool    `json:"was_correct"` // Was the hypothesis correct?

	// Feedback for learning
	ConfidenceError  float64                `json:"confidence_error"` // Difference between predicted and actual
	ImprovementAreas []string               `json:"improvement_areas"`
	Metadata         map[string]interface{} `json:"metadata,omitempty"`
}

// ReasoningTraceEvaluation evaluates reasoning quality
type ReasoningTraceEvaluation struct {
	TraceID string `json:"trace_id"`
	GoalID  string `json:"goal_id"`

	// Quality metrics
	ReasoningQuality      float64 `json:"reasoning_quality"`      // Overall quality of reasoning
	StepCoherence         float64 `json:"step_coherence"`         // How coherent were the steps?
	DecisionQuality       float64 `json:"decision_quality"`       // Quality of decisions made
	ConfidenceCalibration float64 `json:"confidence_calibration"` // How well-calibrated was confidence?

	// Correlation with outcome
	OutcomeCorrelation float64 `json:"outcome_correlation"` // Correlation between reasoning quality and outcome

	// Feedback
	Strengths  []string               `json:"strengths"`
	Weaknesses []string               `json:"weaknesses"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
}

// EvaluateGoalCompletion performs post-hoc evaluation after a goal is completed
func (elf *ExplanationLearningFeedback) EvaluateGoalCompletion(goalID string, goalDescription string, status string, domain string, outcomeMetrics map[string]interface{}) error {
	log.Printf("🧠 [EXPLANATION-LEARNING] Evaluating goal completion: %s (status: %s)", goalID, status)

	// Validate required fields
	if goalID == "" {
		log.Printf("⚠️ [EXPLANATION-LEARNING] Empty goal ID, skipping evaluation")
		return fmt.Errorf("empty goal ID")
	}
	if elf.redis == nil {
		log.Printf("⚠️ [EXPLANATION-LEARNING] Redis client is nil, skipping evaluation")
		return fmt.Errorf("redis client is nil")
	}

	// Collect hypotheses associated with this goal
	hypotheses := elf.collectGoalHypotheses(goalID, domain)

	// Collect reasoning traces for this goal
	reasoningTraces := elf.collectGoalReasoningTraces(goalID, domain)

	// Evaluate hypotheses
	hypothesisEvaluations := make([]HypothesisEvaluation, 0, len(hypotheses))
	for _, hyp := range hypotheses {
		eval := elf.evaluateHypothesis(hyp, status, outcomeMetrics)
		hypothesisEvaluations = append(hypothesisEvaluations, eval)
	}

	// Evaluate reasoning traces
	traceEvaluations := make([]ReasoningTraceEvaluation, 0, len(reasoningTraces))
	for _, trace := range reasoningTraces {
		eval := elf.evaluateReasoningTrace(trace, status, outcomeMetrics)
		traceEvaluations = append(traceEvaluations, eval)
	}

	// Calculate overall metrics
	overallAccuracy := elf.calculateOverallAccuracy(hypothesisEvaluations)
	overallQuality := elf.calculateOverallQuality(traceEvaluations)
	alignmentScore := elf.calculateAlignmentScore(hypothesisEvaluations, traceEvaluations, status, outcomeMetrics)

	// Create feedback record
	feedback := GoalCompletionFeedback{
		GoalID:          goalID,
		GoalDescription: goalDescription,
		Status:          status,
		CompletedAt:     time.Now().UTC(),
		Domain:          domain,
		Hypotheses:      hypothesisEvaluations,
		ReasoningTraces: traceEvaluations,
		OutcomeMetrics:  outcomeMetrics,
		OverallAccuracy: overallAccuracy,
		OverallQuality:  overallQuality,
		AlignmentScore:  alignmentScore,
	}

	// Store feedback
	if err := elf.storeFeedback(feedback); err != nil {
		log.Printf("⚠️ [EXPLANATION-LEARNING] Failed to store feedback: %v", err)
		return err
	}

	// Update learning parameters based on feedback
	elf.updateInferenceWeighting(feedback)
	elf.updateConfidenceScaling(feedback)
	elf.updateExplorationHeuristics(feedback)

	log.Printf("✅ [EXPLANATION-LEARNING] Completed evaluation for goal %s: accuracy=%.2f, quality=%.2f, alignment=%.2f",
		goalID, overallAccuracy, overallQuality, alignmentScore)

	return nil
}

// collectGoalHypotheses retrieves hypotheses associated with a goal
func (elf *ExplanationLearningFeedback) collectGoalHypotheses(goalID string, domain string) []Hypothesis {
	// Look for hypotheses in Redis that reference this goal
	key := fmt.Sprintf("fsm:%s:hypotheses", domain)
	// PERFORMANCE FIX: Limit to 100 hypotheses
	hypothesesData, err := elf.redis.LRange(elf.ctx, key, 0, 99).Result()
	if err != nil {
		log.Printf("⚠️ [EXPLANATION-LEARNING] Failed to retrieve hypotheses: %v", err)
		return []Hypothesis{}
	}

	var hypotheses []Hypothesis
	for _, hypData := range hypothesesData {
		var hyp Hypothesis
		if err := json.Unmarshal([]byte(hypData), &hyp); err == nil {
			// Check if hypothesis is related to this goal (by checking context or goal references)
			// For now, we'll include all hypotheses in the domain that are in "testing" status
			if hyp.Status == "testing" || hyp.Status == "proposed" {
				hypotheses = append(hypotheses, hyp)
			}
		}
	}

	// Also check goal context for hypothesis IDs
	goalKey := fmt.Sprintf("goal:%s", goalID)
	goalData, err := elf.redis.Get(elf.ctx, goalKey).Result()
	if err == nil {
		var goal map[string]interface{}
		if err := json.Unmarshal([]byte(goalData), &goal); err == nil {
			if context, ok := goal["context"].(map[string]interface{}); ok {
				if hypIDs, ok := context["hypothesis_ids"].([]interface{}); ok {
					// Retrieve specific hypotheses by ID
					for _, hypID := range hypIDs {
						if id, ok := hypID.(string); ok {
							hyp := elf.getHypothesisByID(id, domain)
							if hyp != nil {
								hypotheses = append(hypotheses, *hyp)
							}
						}
					}
				}
			}
		}
	}

	return hypotheses
}

// getHypothesisByID retrieves a hypothesis by ID
func (elf *ExplanationLearningFeedback) getHypothesisByID(hypID string, domain string) *Hypothesis {
	key := fmt.Sprintf("fsm:%s:hypotheses", domain)
	// PERFORMANCE FIX: Limit to 100 hypotheses
	hypothesesData, err := elf.redis.LRange(elf.ctx, key, 0, 99).Result()
	if err != nil {
		return nil
	}

	for _, hypData := range hypothesesData {
		var hyp Hypothesis
		if err := json.Unmarshal([]byte(hypData), &hyp); err == nil {
			if hyp.ID == hypID {
				return &hyp
			}
		}
	}
	return nil
}

// collectGoalReasoningTraces retrieves reasoning traces for a goal
func (elf *ExplanationLearningFeedback) collectGoalReasoningTraces(goalID string, domain string) []ReasoningTrace {
	// Look for reasoning traces associated with this goal
	key := fmt.Sprintf("reasoning:traces:goal:%s", goalID)
	// PERFORMANCE FIX: Limit to 20 traces
	tracesData, err := elf.redis.LRange(elf.ctx, key, 0, 19).Result()
	if err != nil {
		// Try alternative key patterns
		key = fmt.Sprintf("reasoning:traces:%s", domain)
		// PERFORMANCE FIX: Limit to 20 traces
		tracesData, err = elf.redis.LRange(elf.ctx, key, 0, 19).Result()
		if err != nil {
			log.Printf("⚠️ [EXPLANATION-LEARNING] Failed to retrieve reasoning traces: %v", err)
			return []ReasoningTrace{}
		}
	}

	var traces []ReasoningTrace
	for _, traceData := range tracesData {
		var trace ReasoningTrace
		if err := json.Unmarshal([]byte(traceData), &trace); err == nil {
			// Filter traces that match the goal
			if trace.Goal == goalID || strings.Contains(strings.ToLower(trace.Goal), strings.ToLower(goalID)) {
				traces = append(traces, trace)
			}
		}
	}

	return traces
}

// evaluateHypothesis evaluates a hypothesis post-hoc
func (elf *ExplanationLearningFeedback) evaluateHypothesis(hyp Hypothesis, goalStatus string, outcomeMetrics map[string]interface{}) HypothesisEvaluation {
	eval := HypothesisEvaluation{
		HypothesisID:       hyp.ID,
		Description:        hyp.Description,
		OriginalConfidence: hyp.Confidence,
		Metadata:           make(map[string]interface{}),
	}

	// Determine if hypothesis was correct based on goal status and outcome
	// For achieved goals, hypotheses that predicted success are correct
	// For failed goals, hypotheses that predicted failure or identified issues are correct
	wasCorrect := false
	if goalStatus == "achieved" {
		// Hypothesis is correct if it predicted success or was supportive
		// This is a simplified heuristic - in practice, would use LLM to evaluate
		wasCorrect = hyp.Confidence > 0.5
	} else if goalStatus == "failed" {
		// Hypothesis is correct if it identified risks or predicted failure
		wasCorrect = hyp.Confidence < 0.5 || strings.Contains(strings.ToLower(hyp.Description), "risk") ||
			strings.Contains(strings.ToLower(hyp.Description), "fail") ||
			strings.Contains(strings.ToLower(hyp.Description), "error")
	}

	eval.WasCorrect = wasCorrect

	// Calculate accuracy (1.0 if correct, 0.0 if incorrect)
	if wasCorrect {
		eval.Accuracy = 1.0
	} else {
		eval.Accuracy = 0.0
	}

	// Calculate quality based on explanation richness
	quality := 0.5 // Base quality
	if len(hyp.Facts) > 0 {
		quality += 0.2 // Has supporting facts
	}
	if hyp.Uncertainty != nil {
		quality += 0.2 // Has uncertainty model
	}
	if len(hyp.CounterfactualActions) > 0 {
		quality += 0.1 // Has counterfactual reasoning
	}
	eval.Quality = math.Min(quality, 1.0)

	// Calculate alignment with outcome
	// Alignment is high if hypothesis confidence matches actual outcome
	if wasCorrect {
		eval.Alignment = hyp.Confidence // High confidence + correct = high alignment
	} else {
		eval.Alignment = 1.0 - hyp.Confidence // Low confidence + incorrect = high alignment
	}

	// Calculate confidence error
	if wasCorrect {
		eval.ConfidenceError = math.Abs(1.0 - hyp.Confidence) // Should have been more confident
	} else {
		eval.ConfidenceError = hyp.Confidence // Should have been less confident
	}

	// Identify improvement areas
	if eval.ConfidenceError > 0.3 {
		eval.ImprovementAreas = append(eval.ImprovementAreas, "confidence_calibration")
	}
	if eval.Quality < 0.6 {
		eval.ImprovementAreas = append(eval.ImprovementAreas, "explanation_depth")
	}
	if !wasCorrect && len(hyp.Facts) == 0 {
		eval.ImprovementAreas = append(eval.ImprovementAreas, "evidence_gathering")
	}

	return eval
}

// evaluateReasoningTrace evaluates reasoning quality
func (elf *ExplanationLearningFeedback) evaluateReasoningTrace(trace ReasoningTrace, goalStatus string, outcomeMetrics map[string]interface{}) ReasoningTraceEvaluation {
	eval := ReasoningTraceEvaluation{
		TraceID:  trace.ID,
		GoalID:   trace.Goal,
		Metadata: make(map[string]interface{}),
	}

	// Calculate reasoning quality based on trace structure
	quality := 0.5 // Base quality

	// More reasoning steps generally indicate better reasoning (up to a point)
	if len(trace.Steps) > 0 {
		stepScore := math.Min(float64(len(trace.Steps))/10.0, 0.3)
		quality += stepScore
	}

	// Evidence indicates active reasoning
	if len(trace.Evidence) > 0 {
		evidenceScore := math.Min(float64(len(trace.Evidence))/5.0, 0.2)
		quality += evidenceScore
	}

	eval.ReasoningQuality = math.Min(quality, 1.0)

	// Calculate step coherence (simplified - would use LLM in practice)
	// Coherence is higher if steps are well-structured
	if len(trace.Steps) > 1 {
		eval.StepCoherence = 0.7 // Simplified - would analyze step relationships
	} else {
		eval.StepCoherence = 0.5
	}

	// Calculate decision quality based on average confidence of steps
	if len(trace.Steps) > 0 {
		totalConfidence := 0.0
		for _, step := range trace.Steps {
			totalConfidence += step.Confidence
		}
		avgConfidence := totalConfidence / float64(len(trace.Steps))
		eval.DecisionQuality = avgConfidence
	} else {
		eval.DecisionQuality = 0.5
	}

	// Calculate confidence calibration
	// Compare trace confidence with actual outcome
	if goalStatus == "achieved" {
		// High confidence + achieved = well calibrated
		eval.ConfidenceCalibration = trace.Confidence
	} else {
		// Low confidence + failed = well calibrated, high confidence + failed = poorly calibrated
		eval.ConfidenceCalibration = 1.0 - trace.Confidence
	}

	// Calculate outcome correlation
	// Positive correlation if high quality reasoning led to success
	if goalStatus == "achieved" {
		eval.OutcomeCorrelation = eval.ReasoningQuality
	} else {
		eval.OutcomeCorrelation = 1.0 - eval.ReasoningQuality
	}

	// Identify strengths and weaknesses
	if eval.ReasoningQuality > 0.7 {
		eval.Strengths = append(eval.Strengths, "comprehensive_reasoning")
	}
	if eval.StepCoherence > 0.7 {
		eval.Strengths = append(eval.Strengths, "coherent_steps")
	}
	if eval.ConfidenceCalibration > 0.7 {
		eval.Strengths = append(eval.Strengths, "well_calibrated")
	}

	if eval.ReasoningQuality < 0.5 {
		eval.Weaknesses = append(eval.Weaknesses, "insufficient_reasoning")
	}
	if eval.StepCoherence < 0.5 {
		eval.Weaknesses = append(eval.Weaknesses, "incoherent_steps")
	}
	if eval.ConfidenceCalibration < 0.5 {
		eval.Weaknesses = append(eval.Weaknesses, "poor_calibration")
	}

	return eval
}

// calculateOverallAccuracy calculates overall hypothesis accuracy
func (elf *ExplanationLearningFeedback) calculateOverallAccuracy(evaluations []HypothesisEvaluation) float64 {
	if len(evaluations) == 0 {
		return 0.5 // Default neutral accuracy
	}

	totalAccuracy := 0.0
	for _, eval := range evaluations {
		totalAccuracy += eval.Accuracy
	}

	return totalAccuracy / float64(len(evaluations))
}

// calculateOverallQuality calculates overall reasoning quality
func (elf *ExplanationLearningFeedback) calculateOverallQuality(evaluations []ReasoningTraceEvaluation) float64 {
	if len(evaluations) == 0 {
		return 0.5 // Default neutral quality
	}

	totalQuality := 0.0
	for _, eval := range evaluations {
		totalQuality += eval.ReasoningQuality
	}

	return totalQuality / float64(len(evaluations))
}

// calculateAlignmentScore calculates how well hypotheses and reasoning aligned with outcomes
func (elf *ExplanationLearningFeedback) calculateAlignmentScore(
	hypEvals []HypothesisEvaluation,
	traceEvals []ReasoningTraceEvaluation,
	goalStatus string,
	outcomeMetrics map[string]interface{},
) float64 {
	// Combine hypothesis alignment and reasoning trace correlation
	hypAlignment := 0.0
	if len(hypEvals) > 0 {
		for _, eval := range hypEvals {
			hypAlignment += eval.Alignment
		}
		hypAlignment /= float64(len(hypEvals))
	} else {
		hypAlignment = 0.5
	}

	traceCorrelation := 0.0
	if len(traceEvals) > 0 {
		for _, eval := range traceEvals {
			traceCorrelation += eval.OutcomeCorrelation
		}
		traceCorrelation /= float64(len(traceEvals))
	} else {
		traceCorrelation = 0.5
	}

	// Weighted average
	return (hypAlignment*0.6 + traceCorrelation*0.4)
}

// storeFeedback stores feedback in Redis
func (elf *ExplanationLearningFeedback) storeFeedback(feedback GoalCompletionFeedback) error {
	// Store individual feedback record
	key := fmt.Sprintf("explanation_learning:feedback:%s", feedback.GoalID)
	data, err := json.Marshal(feedback)
	if err != nil {
		return fmt.Errorf("failed to marshal feedback: %w", err)
	}

	if err := elf.redis.Set(elf.ctx, key, data, 30*24*time.Hour).Err(); err != nil {
		return fmt.Errorf("failed to store feedback: %w", err)
	}

	// Store in domain-specific list
	domainKey := fmt.Sprintf("explanation_learning:feedback:domain:%s", feedback.Domain)
	elf.redis.LPush(elf.ctx, domainKey, data)
	elf.redis.LTrim(elf.ctx, domainKey, 0, 99) // Keep last 100 feedback records per domain

	// Store in global list
	globalKey := "explanation_learning:feedback:all"
	elf.redis.LPush(elf.ctx, globalKey, data)
	elf.redis.LTrim(elf.ctx, globalKey, 0, 199) // Keep last 200 feedback records

	// Update aggregate statistics
	elf.updateAggregateStats(feedback)

	return nil
}

// updateAggregateStats updates aggregate learning statistics
func (elf *ExplanationLearningFeedback) updateAggregateStats(feedback GoalCompletionFeedback) {
	statsKey := fmt.Sprintf("explanation_learning:stats:%s", feedback.Domain)

	// Get current stats
	statsData, err := elf.redis.Get(elf.ctx, statsKey).Result()
	var stats map[string]interface{}
	if err == nil {
		json.Unmarshal([]byte(statsData), &stats)
	} else {
		stats = make(map[string]interface{})
		stats["total_goals"] = 0.0
		stats["achieved_goals"] = 0.0
		stats["failed_goals"] = 0.0
		stats["avg_accuracy"] = 0.0
		stats["avg_quality"] = 0.0
		stats["avg_alignment"] = 0.0
	}

	// Update stats
	totalGoals := stats["total_goals"].(float64) + 1
	stats["total_goals"] = totalGoals

	if feedback.Status == "achieved" {
		stats["achieved_goals"] = stats["achieved_goals"].(float64) + 1
	} else {
		stats["failed_goals"] = stats["failed_goals"].(float64) + 1
	}

	// Update running averages
	currentAvgAccuracy := stats["avg_accuracy"].(float64)
	currentAvgQuality := stats["avg_quality"].(float64)
	currentAvgAlignment := stats["avg_alignment"].(float64)

	stats["avg_accuracy"] = (currentAvgAccuracy*(totalGoals-1) + feedback.OverallAccuracy) / totalGoals
	stats["avg_quality"] = (currentAvgQuality*(totalGoals-1) + feedback.OverallQuality) / totalGoals
	stats["avg_alignment"] = (currentAvgAlignment*(totalGoals-1) + feedback.AlignmentScore) / totalGoals

	// Store updated stats
	statsDataBytes, _ := json.Marshal(stats)
	elf.redis.Set(elf.ctx, statsKey, statsDataBytes, 0)
}

// updateInferenceWeighting updates inference rule weights based on feedback
func (elf *ExplanationLearningFeedback) updateInferenceWeighting(feedback GoalCompletionFeedback) {
	log.Printf("🧠 [EXPLANATION-LEARNING] Updating inference weighting for domain: %s", feedback.Domain)

	// Analyze which inference patterns led to successful vs failed outcomes
	// Update weights for inference rules based on hypothesis accuracy

	// Calculate weight adjustment based on feedback
	// Rules that led to accurate hypotheses get higher weights
	weightAdjustment := 0.0
	if feedback.OverallAccuracy > 0.7 {
		weightAdjustment = 0.1 // Increase weight for good performance
	} else if feedback.OverallAccuracy < 0.3 {
		weightAdjustment = -0.1 // Decrease weight for poor performance
	}

	// Store weight adjustments for later application
	adjustmentKey := fmt.Sprintf("explanation_learning:inference_adjustments:%s", feedback.Domain)
	adjustment := map[string]interface{}{
		"goal_id":           feedback.GoalID,
		"accuracy":          feedback.OverallAccuracy,
		"weight_adjustment": weightAdjustment,
		"timestamp":         time.Now().UTC().Format(time.RFC3339),
	}
	adjustmentData, _ := json.Marshal(adjustment)
	elf.redis.LPush(elf.ctx, adjustmentKey, adjustmentData)
	elf.redis.LTrim(elf.ctx, adjustmentKey, 0, 49) // Keep last 50 adjustments

	log.Printf("✅ [EXPLANATION-LEARNING] Recorded inference weight adjustment: %.2f", weightAdjustment)
}

// updateConfidenceScaling updates confidence scaling rules based on feedback
func (elf *ExplanationLearningFeedback) updateConfidenceScaling(feedback GoalCompletionFeedback) {
	log.Printf("🧠 [EXPLANATION-LEARNING] Updating confidence scaling for domain: %s", feedback.Domain)

	// Analyze confidence calibration errors
	avgConfidenceError := 0.0
	count := 0
	for _, hypEval := range feedback.Hypotheses {
		avgConfidenceError += hypEval.ConfidenceError
		count++
	}
	if count > 0 {
		avgConfidenceError /= float64(count)
	}

	// Determine scaling adjustment
	// If confidence errors are consistently high, we need to adjust scaling
	scalingKey := fmt.Sprintf("explanation_learning:confidence_scaling:%s", feedback.Domain)

	// Get current scaling factors
	scalingData, err := elf.redis.Get(elf.ctx, scalingKey).Result()
	var scaling map[string]interface{}
	if err == nil {
		json.Unmarshal([]byte(scalingData), &scaling)
	} else {
		scaling = make(map[string]interface{})
		scaling["base_scale"] = 1.0
		scaling["calibration_factor"] = 1.0
	}

	// Adjust calibration factor based on errors
	currentCalibration := scaling["calibration_factor"].(float64)
	if avgConfidenceError > 0.3 {
		// Overconfident - reduce calibration factor
		newCalibration := math.Max(0.5, currentCalibration-0.05)
		scaling["calibration_factor"] = newCalibration
		log.Printf("📉 [EXPLANATION-LEARNING] Reducing confidence calibration (overconfident): %.2f -> %.2f", currentCalibration, newCalibration)
	} else if avgConfidenceError < 0.1 {
		// Underconfident - increase calibration factor
		newCalibration := math.Min(1.5, currentCalibration+0.05)
		scaling["calibration_factor"] = newCalibration
		log.Printf("📈 [EXPLANATION-LEARNING] Increasing confidence calibration (underconfident): %.2f -> %.2f", currentCalibration, newCalibration)
	}

	// Store updated scaling
	scalingDataBytes, _ := json.Marshal(scaling)
	elf.redis.Set(elf.ctx, scalingKey, scalingDataBytes, 0)

	log.Printf("✅ [EXPLANATION-LEARNING] Updated confidence scaling: calibration_factor=%.2f", scaling["calibration_factor"])
}

// updateExplorationHeuristics updates exploration heuristics based on feedback
func (elf *ExplanationLearningFeedback) updateExplorationHeuristics(feedback GoalCompletionFeedback) {
	log.Printf("🧠 [EXPLANATION-LEARNING] Updating exploration heuristics for domain: %s", feedback.Domain)

	// Analyze which types of exploration led to better outcomes
	heuristicsKey := fmt.Sprintf("explanation_learning:exploration_heuristics:%s", feedback.Domain)

	// Get current heuristics
	heuristicsData, err := elf.redis.Get(elf.ctx, heuristicsKey).Result()
	var heuristics map[string]interface{}
	if err == nil {
		json.Unmarshal([]byte(heuristicsData), &heuristics)
	} else {
		heuristics = make(map[string]interface{})
		heuristics["exploration_rate"] = 0.1
		heuristics["exploitation_rate"] = 0.9
		heuristics["curiosity_bonus"] = 0.1
		heuristics["success_bonus"] = 0.05
	}

	// Adjust exploration vs exploitation balance
	// If quality is high, we can exploit more; if low, explore more
	currentExplorationRate := heuristics["exploration_rate"].(float64)
	if feedback.OverallQuality < 0.5 {
		// Low quality - increase exploration
		newRate := math.Min(0.3, currentExplorationRate+0.02)
		heuristics["exploration_rate"] = newRate
		heuristics["exploitation_rate"] = 1.0 - newRate
		log.Printf("🔍 [EXPLANATION-LEARNING] Increasing exploration (low quality): %.2f -> %.2f", currentExplorationRate, newRate)
	} else if feedback.OverallQuality > 0.8 {
		// High quality - can exploit more
		newRate := math.Max(0.05, currentExplorationRate-0.01)
		heuristics["exploration_rate"] = newRate
		heuristics["exploitation_rate"] = 1.0 - newRate
		log.Printf("🎯 [EXPLANATION-LEARNING] Decreasing exploration (high quality): %.2f -> %.2f", currentExplorationRate, newRate)
	}

	// Adjust curiosity bonus based on alignment
	// High alignment means our curiosity is well-directed
	currentCuriosityBonus := heuristics["curiosity_bonus"].(float64)
	if feedback.AlignmentScore > 0.7 {
		// Well-aligned - maintain or slightly increase curiosity
		newBonus := math.Min(0.2, currentCuriosityBonus+0.01)
		heuristics["curiosity_bonus"] = newBonus
	} else if feedback.AlignmentScore < 0.4 {
		// Poor alignment - reduce curiosity bonus
		newBonus := math.Max(0.05, currentCuriosityBonus-0.01)
		heuristics["curiosity_bonus"] = newBonus
	}

	// Store updated heuristics
	heuristicsDataBytes, _ := json.Marshal(heuristics)
	elf.redis.Set(elf.ctx, heuristicsKey, heuristicsDataBytes, 0)

	log.Printf("✅ [EXPLANATION-LEARNING] Updated exploration heuristics: exploration_rate=%.2f, curiosity_bonus=%.2f",
		heuristics["exploration_rate"], heuristics["curiosity_bonus"])
}

// GetLearningStats retrieves learning statistics for a domain
func (elf *ExplanationLearningFeedback) GetLearningStats(domain string) (map[string]interface{}, error) {
	statsKey := fmt.Sprintf("explanation_learning:stats:%s", domain)
	statsData, err := elf.redis.Get(elf.ctx, statsKey).Result()
	if err != nil {
		return nil, err
	}

	var stats map[string]interface{}
	if err := json.Unmarshal([]byte(statsData), &stats); err != nil {
		return nil, err
	}

	return stats, nil
}

// GetConfidenceScaling retrieves confidence scaling factors for a domain
func (elf *ExplanationLearningFeedback) GetConfidenceScaling(domain string) (map[string]interface{}, error) {
	scalingKey := fmt.Sprintf("explanation_learning:confidence_scaling:%s", domain)
	scalingData, err := elf.redis.Get(elf.ctx, scalingKey).Result()
	if err != nil {
		// Return defaults
		return map[string]interface{}{
			"base_scale":         1.0,
			"calibration_factor": 1.0,
		}, nil
	}

	var scaling map[string]interface{}
	if err := json.Unmarshal([]byte(scalingData), &scaling); err != nil {
		return nil, err
	}

	return scaling, nil
}

// GetExplorationHeuristics retrieves exploration heuristics for a domain
func (elf *ExplanationLearningFeedback) GetExplorationHeuristics(domain string) (map[string]interface{}, error) {
	heuristicsKey := fmt.Sprintf("explanation_learning:exploration_heuristics:%s", domain)
	heuristicsData, err := elf.redis.Get(elf.ctx, heuristicsKey).Result()
	if err != nil {
		// Return defaults
		return map[string]interface{}{
			"exploration_rate":  0.1,
			"exploitation_rate": 0.9,
			"curiosity_bonus":   0.1,
			"success_bonus":     0.05,
		}, nil
	}

	var heuristics map[string]interface{}
	if err := json.Unmarshal([]byte(heuristicsData), &heuristics); err != nil {
		return nil, err
	}

	return heuristics, nil
}
