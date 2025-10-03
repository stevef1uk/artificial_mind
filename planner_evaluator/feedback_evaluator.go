package planner

import (
	"fmt"
	"time"

	selfmodel "agi/self"
)

// FeedbackEvaluator extends the base Evaluator with experience-based scoring
type FeedbackEvaluator struct {
	base           *Evaluator
	selfModel      *selfmodel.Manager
	principlesURL  string
	principlesRisk float64 // Current principles risk threshold
}

// NewFeedbackEvaluator creates a new feedback-aware evaluator
func NewFeedbackEvaluator(base *Evaluator, sm *selfmodel.Manager, principlesURL string) *FeedbackEvaluator {
	return &FeedbackEvaluator{
		base:           base,
		selfModel:      sm,
		principlesURL:  principlesURL,
		principlesRisk: 0.1, // Default risk threshold
	}
}

// ScoreOption evaluates a single plan option using both principles and feedback
func (fe *FeedbackEvaluator) ScoreOption(opt Option) (float64, error) {
	// 1. Start with the base evaluator (principles-based)
	score, err := fe.base.ScoreOption(opt)
	if err != nil {
		return 0, err
	}

	// 2. Apply principles-based scoring
	principlesScore := fe.scorePrinciples(opt)
	score += principlesScore

	// 3. Load self-model for historical feedback
	sm, err := fe.selfModel.Load()
	if err != nil {
		return score, nil // fall back gracefully
	}

	// 4. Apply feedback-based adjustments
	feedbackScore := fe.scoreFeedback(opt, sm)
	score += feedbackScore

	return score, nil
}

// scorePrinciples evaluates principles-based scoring
func (fe *FeedbackEvaluator) scorePrinciples(opt Option) float64 {
	// Check historical principle violations for this task
	sm, err := fe.selfModel.Load()
	if err != nil {
		return 0 // fall back gracefully
	}

	principlesScore := 0.0
	taskName := opt.TaskName

	// Check principle violation history
	violKey := fmt.Sprintf("principles_violations:%s", taskName)
	if violations, ok := sm.Beliefs[violKey].(int); ok {
		// Heavy penalty for principle violations
		principlesScore -= float64(violations) * 10
	} else if violations, ok := sm.Beliefs[violKey].(float64); ok {
		principlesScore -= violations * 10
	}

	// Check principle compliance rate
	complianceKey := fmt.Sprintf("principles_compliance:%s", taskName)
	if compliance, ok := sm.Beliefs[complianceKey].(float64); ok {
		// Reward high compliance, penalize low compliance
		principlesScore += (compliance - 0.5) * 5
	}

	// Language-specific principle risk
	langRiskKey := fmt.Sprintf("principles_risk:%s", opt.Language)
	if risk, ok := sm.Beliefs[langRiskKey].(float64); ok {
		principlesScore -= risk * 3
	}

	return principlesScore
}

// scoreFeedback evaluates feedback-based scoring
func (fe *FeedbackEvaluator) scoreFeedback(opt Option, sm *selfmodel.SelfModel) float64 {
	feedbackScore := 0.0
	taskName := opt.TaskName
	lang := opt.Language

	// Success rate adjustment
	if rate, ok := sm.Beliefs[fmt.Sprintf("success_rate:%s", taskName)].(float64); ok {
		feedbackScore += (rate - 0.5) * 2 // push above/below neutral
	}

	// Execution time performance
	if perf, ok := sm.Beliefs[fmt.Sprintf("avg_exec_time:%s", lang)].(float64); ok {
		// Faster languages get a small boost
		if perf < 1000 { // < 1s avg
			feedbackScore += 0.5
		} else {
			feedbackScore -= 0.5
		}
	}

	// General violation penalty (non-principle violations)
	violKey := fmt.Sprintf("violations:%s", taskName)
	if violations, ok := sm.Beliefs[violKey].(int); ok {
		feedbackScore -= float64(violations) * 2
	} else if violations, ok := sm.Beliefs[violKey].(float64); ok {
		feedbackScore -= violations * 2
	}

	return feedbackScore
}

// SelectBest chooses the best option using feedback-aware scoring
func (fe *FeedbackEvaluator) SelectBest(options []Option) (Option, error) {
	var best Option
	var bestScore float64 = -1e9

	for _, opt := range options {
		score, err := fe.ScoreOption(opt)
		if err != nil {
			continue
		}
		if score > bestScore {
			best = opt
			bestScore = score
		}
	}

	if best.TaskName == "" {
		return Option{}, fmt.Errorf("no valid option found")
	}
	return best, nil
}

// CheckPrinciplesCompliance checks if a plan complies with principles
func (fe *FeedbackEvaluator) CheckPrinciplesCompliance(opt Option) (bool, float64, error) {
	sm, err := fe.selfModel.Load()
	if err != nil {
		return true, 0.0, err // Default to compliant if we can't load
	}

	taskName := opt.TaskName
	lang := opt.Language

	// Check principle violation history
	violKey := fmt.Sprintf("principles_violations:%s", taskName)
	violations := 0
	if v, ok := sm.Beliefs[violKey].(int); ok {
		violations = v
	} else if v, ok := sm.Beliefs[violKey].(float64); ok {
		violations = int(v)
	}

	// Check compliance rate
	complianceKey := fmt.Sprintf("principles_compliance:%s", taskName)
	compliance := 0.5 // Default neutral
	if v, ok := sm.Beliefs[complianceKey].(float64); ok {
		compliance = v
	}

	// Check language risk
	langRiskKey := fmt.Sprintf("principles_risk:%s", lang)
	risk := 0.0
	if v, ok := sm.Beliefs[langRiskKey].(float64); ok {
		risk = v
	}

	// Calculate overall compliance score
	complianceScore := compliance - (float64(violations) * 0.1) - risk

	// Consider compliant if score > 0.3
	isCompliant := complianceScore > 0.3

	return isCompliant, complianceScore, nil
}

// RecordFeedback updates beliefs after an execution result
func (fe *FeedbackEvaluator) RecordFeedback(taskName, lang string, success bool, execTime time.Duration, violatedPrinciples int) error {
	sm, err := fe.selfModel.Load()
	if err != nil {
		return err
	}

	// Success rate (EWMA)
	key := fmt.Sprintf("success_rate:%s", taskName)
	old := 0.5
	if v, ok := sm.Beliefs[key].(float64); ok {
		old = v
	}
	alpha := 0.3
	newRate := (1-alpha)*old + alpha*boolToFloat(success)
	sm.Beliefs[key] = newRate

	// Avg execution time
	timeKey := fmt.Sprintf("avg_exec_time:%s", lang)
	if v, ok := sm.Beliefs[timeKey].(float64); ok {
		sm.Beliefs[timeKey] = (v + float64(execTime.Milliseconds())) / 2
	} else {
		sm.Beliefs[timeKey] = float64(execTime.Milliseconds())
	}

	// General violations (non-principle)
	violKey := fmt.Sprintf("violations:%s", taskName)
	if v, ok := sm.Beliefs[violKey].(int); ok {
		sm.Beliefs[violKey] = v + 1 // Count total violations
	} else {
		sm.Beliefs[violKey] = 1
	}

	// Principles-specific tracking
	fe.recordPrinciplesFeedback(sm, taskName, lang, success, violatedPrinciples)

	return fe.selfModel.Save(sm)
}

// recordPrinciplesFeedback tracks principles-specific metrics
func (fe *FeedbackEvaluator) recordPrinciplesFeedback(sm *selfmodel.SelfModel, taskName, lang string, success bool, violatedPrinciples int) {
	// Track principle violations specifically
	principlesViolKey := fmt.Sprintf("principles_violations:%s", taskName)
	if v, ok := sm.Beliefs[principlesViolKey].(int); ok {
		sm.Beliefs[principlesViolKey] = v + violatedPrinciples
	} else {
		sm.Beliefs[principlesViolKey] = violatedPrinciples
	}

	// Track principle compliance rate (EWMA)
	complianceKey := fmt.Sprintf("principles_compliance:%s", taskName)
	oldCompliance := 0.5
	if v, ok := sm.Beliefs[complianceKey].(float64); ok {
		oldCompliance = v
	}

	// Compliance is 1.0 if no violations, 0.0 if violations occurred
	compliance := 1.0
	if violatedPrinciples > 0 {
		compliance = 0.0
	}

	alpha := 0.3
	newCompliance := (1-alpha)*oldCompliance + alpha*compliance
	sm.Beliefs[complianceKey] = newCompliance

	// Track language-specific principle risk
	langRiskKey := fmt.Sprintf("principles_risk:%s", lang)
	oldRisk := 0.0
	if v, ok := sm.Beliefs[langRiskKey].(float64); ok {
		oldRisk = v
	}

	// Risk increases with violations
	riskIncrease := float64(violatedPrinciples) * 0.1
	newRisk := oldRisk + riskIncrease
	if newRisk > 1.0 {
		newRisk = 1.0 // Cap at 1.0
	}
	sm.Beliefs[langRiskKey] = newRisk
}

func boolToFloat(b bool) float64 {
	if b {
		return 1.0
	}
	return 0.0
}
