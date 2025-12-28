package main

import (
	"math"
	"strings"
	"time"
)

// UncertaintyModel represents a formal uncertainty tracking system
// distinguishing between epistemic (lack of knowledge) and aleatoric (inherent randomness) uncertainty
type UncertaintyModel struct {
	// Epistemic uncertainty: reducible through gathering more information
	// Represents lack of knowledge about the true state
	EpistemicUncertainty float64 `json:"epistemic_uncertainty"` // 0-1, higher = less knowledge

	// Aleatoric uncertainty: irreducible, inherent randomness/variability
	// Represents inherent unpredictability in the system
	AleatoricUncertainty float64 `json:"aleatoric_uncertainty"` // 0-1, higher = more inherent randomness

	// Calibrated confidence: overall confidence accounting for both uncertainty types
	// confidence = 1 - sqrt(epistemic^2 + aleatoric^2) / sqrt(2)
	// This ensures both types of uncertainty reduce confidence
	CalibratedConfidence float64 `json:"calibrated_confidence"` // 0-1

	// Belief stability: how stable the belief has been over time
	// Higher stability = less volatility in confidence updates
	Stability float64 `json:"stability"` // 0-1, higher = more stable

	// Belief volatility: measure of how much confidence has changed over time
	// Higher volatility = more frequent/large changes
	Volatility float64 `json:"volatility"` // 0-1, higher = more volatile

	// History of confidence values for tracking stability/volatility
	ConfidenceHistory []ConfidenceSnapshot `json:"confidence_history"`

	// Last update timestamp for decay calculations
	LastUpdated time.Time `json:"last_updated"`

	// Decay rate per hour (0-1, fraction of uncertainty to add per hour)
	DecayRatePerHour float64 `json:"decay_rate_per_hour"`
}

// ConfidenceSnapshot records a point-in-time confidence value
type ConfidenceSnapshot struct {
	Confidence float64   `json:"confidence"`
	Timestamp  time.Time `json:"timestamp"`
	Source     string    `json:"source"` // e.g., "inference", "reinforcement", "decay"
}

// NewUncertaintyModel creates a new uncertainty model with initial values
func NewUncertaintyModel(initialConfidence float64, epistemicUncertainty, aleatoricUncertainty float64) *UncertaintyModel {
	um := &UncertaintyModel{
		EpistemicUncertainty: clamp(epistemicUncertainty, 0.0, 1.0),
		AleatoricUncertainty: clamp(aleatoricUncertainty, 0.0, 1.0),
		Stability:            0.5, // Start with neutral stability
		Volatility:           0.0, // Start with no volatility
		ConfidenceHistory: []ConfidenceSnapshot{
			{
				Confidence: clamp(initialConfidence, 0.0, 1.0),
				Timestamp:  time.Now(),
				Source:     "initial",
			},
		},
		LastUpdated:      time.Now(),
		DecayRatePerHour: 0.01, // Default: 1% epistemic uncertainty increase per hour
	}
	um.updateCalibratedConfidence()
	return um
}

// updateCalibratedConfidence recalculates calibrated confidence from uncertainty components
// Uses geometric mean to combine uncertainties: confidence = 1 - sqrt(ep^2 + al^2) / sqrt(2)
func (um *UncertaintyModel) updateCalibratedConfidence() {
	// Combine uncertainties using Euclidean distance normalized to [0,1]
	combinedUncertainty := math.Sqrt(um.EpistemicUncertainty*um.EpistemicUncertainty +
		um.AleatoricUncertainty*um.AleatoricUncertainty) / math.Sqrt(2.0)
	um.CalibratedConfidence = clamp(1.0-combinedUncertainty, 0.0, 1.0)
}

// UpdateConfidence updates the confidence and records it in history
func (um *UncertaintyModel) UpdateConfidence(newConfidence float64, source string) {
	newConfidence = clamp(newConfidence, 0.0, 1.0)
	
	// Record snapshot
	snapshot := ConfidenceSnapshot{
		Confidence: newConfidence,
		Timestamp:  time.Now(),
		Source:     source,
	}
	um.ConfidenceHistory = append(um.ConfidenceHistory, snapshot)
	
	// Keep only last 100 snapshots to prevent unbounded growth
	if len(um.ConfidenceHistory) > 100 {
		um.ConfidenceHistory = um.ConfidenceHistory[len(um.ConfidenceHistory)-100:]
	}
	
	// Update stability and volatility based on history
	um.updateStabilityAndVolatility()
	um.LastUpdated = time.Now()
}

// updateStabilityAndVolatility calculates stability and volatility from confidence history
func (um *UncertaintyModel) updateStabilityAndVolatility() {
	if len(um.ConfidenceHistory) < 2 {
		um.Stability = 0.5
		um.Volatility = 0.0
		return
	}
	
	// Calculate variance in confidence values
	var sum, sumSq float64
	for _, snap := range um.ConfidenceHistory {
		sum += snap.Confidence
		sumSq += snap.Confidence * snap.Confidence
	}
	n := float64(len(um.ConfidenceHistory))
	mean := sum / n
	variance := (sumSq / n) - (mean * mean)
	
	// Volatility is normalized variance (0-1)
	um.Volatility = clamp(variance*4.0, 0.0, 1.0) // Scale variance to [0,1]
	
	// Stability is inverse of volatility, but also considers recency
	// More recent changes have more weight
	recentVolatility := um.calculateRecentVolatility()
	um.Stability = clamp(1.0-(um.Volatility*0.7+recentVolatility*0.3), 0.0, 1.0)
}

// calculateRecentVolatility calculates volatility from recent snapshots (last 10)
func (um *UncertaintyModel) calculateRecentVolatility() float64 {
	if len(um.ConfidenceHistory) < 2 {
		return 0.0
	}
	
	// Use last 10 snapshots or all if fewer
	start := 0
	if len(um.ConfidenceHistory) > 10 {
		start = len(um.ConfidenceHistory) - 10
	}
	recent := um.ConfidenceHistory[start:]
	
	var sum, sumSq float64
	for _, snap := range recent {
		sum += snap.Confidence
		sumSq += snap.Confidence * snap.Confidence
	}
	n := float64(len(recent))
	mean := sum / n
	variance := (sumSq / n) - (mean * mean)
	
	return clamp(variance*4.0, 0.0, 1.0)
}

// ApplyDecay applies epistemic uncertainty decay over time without reinforcement
// Epistemic uncertainty increases over time if not reinforced
func (um *UncertaintyModel) ApplyDecay() {
	now := time.Now()
	hoursElapsed := now.Sub(um.LastUpdated).Hours()
	
	if hoursElapsed <= 0 {
		return
	}
	
	// Increase epistemic uncertainty by decay rate
	// More uncertainty = less confidence
	epistemicIncrease := um.DecayRatePerHour * hoursElapsed
	um.EpistemicUncertainty = clamp(um.EpistemicUncertainty+epistemicIncrease, 0.0, 1.0)
	
	// Update calibrated confidence
	um.updateCalibratedConfidence()
	
	// Record decay in history
	um.UpdateConfidence(um.CalibratedConfidence, "decay")
}

// Reinforce reduces epistemic uncertainty through new evidence
func (um *UncertaintyModel) Reinforce(evidenceStrength float64) {
	// Stronger evidence reduces epistemic uncertainty more
	reduction := clamp(evidenceStrength, 0.0, 1.0) * 0.2 // Max 20% reduction per reinforcement
	um.EpistemicUncertainty = clamp(um.EpistemicUncertainty-reduction, 0.0, 1.0)
	um.updateCalibratedConfidence()
	um.UpdateConfidence(um.CalibratedConfidence, "reinforcement")
}

// PropagateConfidence propagates confidence through an inference chain
// Returns new confidence accounting for chain length and input confidences
func PropagateConfidenceThroughChain(inputConfidences []float64, chainLength int) float64 {
	if len(inputConfidences) == 0 {
		return 0.5 // Default if no inputs
	}
	
	// Calculate geometric mean of input confidences
	// This is more conservative than arithmetic mean
	var product float64 = 1.0
	for _, conf := range inputConfidences {
		product *= clamp(conf, 0.0, 1.0)
	}
	geometricMean := math.Pow(product, 1.0/float64(len(inputConfidences)))
	
	// Apply chain length penalty: longer chains reduce confidence
	// Each additional step reduces confidence by 5%
	chainPenalty := math.Pow(0.95, float64(chainLength-1))
	
	propagatedConfidence := geometricMean * chainPenalty
	return clamp(propagatedConfidence, 0.0, 1.0)
}

// CombineUncertainties combines multiple uncertainty models
// Used when combining beliefs from different sources
func CombineUncertainties(models []*UncertaintyModel) *UncertaintyModel {
	if len(models) == 0 {
		return NewUncertaintyModel(0.5, 0.5, 0.5)
	}
	
	// Average epistemic and aleatoric uncertainties
	var sumEpistemic, sumAleatoric, sumConfidence float64
	for _, m := range models {
		sumEpistemic += m.EpistemicUncertainty
		sumAleatoric += m.AleatoricUncertainty
		sumConfidence += m.CalibratedConfidence
	}
	
	n := float64(len(models))
	combined := NewUncertaintyModel(
		sumConfidence/n,
		sumEpistemic/n,
		sumAleatoric/n,
	)
	
	// Average stability and volatility
	var sumStability, sumVolatility float64
	for _, m := range models {
		sumStability += m.Stability
		sumVolatility += m.Volatility
	}
	combined.Stability = sumStability / n
	combined.Volatility = sumVolatility / n
	
	return combined
}

// EstimateAleatoricUncertainty estimates aleatoric uncertainty based on domain characteristics
// Some domains have inherent randomness (e.g., news events) vs deterministic (e.g., math)
func EstimateAleatoricUncertainty(domain string, goalType string) float64 {
	// Domains with high inherent randomness
	highRandomnessDomains := []string{"news", "events", "politics", "markets", "weather"}
	for _, rd := range highRandomnessDomains {
		if contains(domain, rd) {
			return 0.3 // Higher aleatoric uncertainty
		}
	}
	
	// Goal types that involve exploration have more randomness
	exploratoryTypes := []string{"concept_exploration", "gap_filling"}
	for _, et := range exploratoryTypes {
		if goalType == et {
			return 0.2
		}
	}
	
	// Default: moderate aleatoric uncertainty
	return 0.1
}

// EstimateEpistemicUncertainty estimates epistemic uncertainty based on available evidence
func EstimateEpistemicUncertainty(evidenceCount int, hasDefinition bool, hasExamples bool) float64 {
	// Start with high epistemic uncertainty
	epistemic := 0.6
	
	// Reduce based on evidence
	if evidenceCount > 0 {
		epistemic -= float64(evidenceCount) * 0.1
	}
	if hasDefinition {
		epistemic -= 0.2
	}
	if hasExamples {
		epistemic -= 0.1
	}
	
	return clamp(epistemic, 0.0, 1.0)
}

// clamp ensures a value is within [min, max]
func clamp(value, min, max float64) float64 {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

// contains checks if a string contains a substring (case-insensitive)
func contains(s, substr string) bool {
	return len(s) >= len(substr) && 
		(len(substr) == 0 || 
			strings.Contains(strings.ToLower(s), strings.ToLower(substr)))
}

// ApplyDecayToBelief applies decay to a belief's uncertainty model
// Returns true if the belief should be considered for removal (very low confidence)
func ApplyDecayToBelief(belief *Belief) bool {
	if belief.Uncertainty == nil {
		// Initialize uncertainty model if missing
		epistemicUncertainty := EstimateEpistemicUncertainty(len(belief.Evidence), false, false)
		aleatoricUncertainty := EstimateAleatoricUncertainty(belief.Domain, "")
		belief.Uncertainty = NewUncertaintyModel(belief.Confidence, epistemicUncertainty, aleatoricUncertainty)
	}
	
	belief.Uncertainty.ApplyDecay()
	belief.Confidence = belief.Uncertainty.CalibratedConfidence
	belief.LastUpdated = time.Now()
	
	// Return true if confidence is very low (below 0.2) - might want to remove
	return belief.Confidence < 0.2
}

// ApplyDecayToHypothesis applies decay to a hypothesis's uncertainty model
// Returns true if the hypothesis should be considered for removal
func ApplyDecayToHypothesis(hypothesis *Hypothesis) bool {
	if hypothesis.Uncertainty == nil {
		// Initialize uncertainty model if missing
		epistemicUncertainty := EstimateEpistemicUncertainty(len(hypothesis.Facts), false, false)
		aleatoricUncertainty := EstimateAleatoricUncertainty(hypothesis.Domain, "")
		hypothesis.Uncertainty = NewUncertaintyModel(hypothesis.Confidence, epistemicUncertainty, aleatoricUncertainty)
	}
	
	hypothesis.Uncertainty.ApplyDecay()
	hypothesis.Confidence = hypothesis.Uncertainty.CalibratedConfidence
	
	// Return true if confidence is very low (below 0.2) and status is still "proposed"
	return hypothesis.Confidence < 0.2 && hypothesis.Status == "proposed"
}

// ApplyDecayToGoal applies decay to a goal's uncertainty model
func ApplyDecayToGoal(goal *CuriosityGoal) {
	if goal.Uncertainty == nil {
		// Initialize uncertainty model if missing
		epistemicUncertainty := EstimateEpistemicUncertainty(0, false, false)
		aleatoricUncertainty := EstimateAleatoricUncertainty(goal.Domain, goal.Type)
		goal.Uncertainty = NewUncertaintyModel(0.5, epistemicUncertainty, aleatoricUncertainty)
	}
	
	goal.Uncertainty.ApplyDecay()
	// Update value based on uncertainty if not explicitly set
	if goal.Value == 0 {
		goal.Value = goal.Uncertainty.CalibratedConfidence
	}
}

