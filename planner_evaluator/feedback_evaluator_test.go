package planner

import (
	"testing"
	"time"

	selfmodel "agi/self"

	"github.com/stretchr/testify/assert"
)

func setupFE(t *testing.T) *FeedbackEvaluator {
	sm := selfmodel.NewManager("localhost:6379", "selfmodel:feedbacktest")

	// Clear any existing data by loading and saving empty state
	emptyModel, _ := sm.Load()
	if emptyModel != nil {
		emptyModel.Beliefs = make(map[string]interface{})
		sm.Save(emptyModel)
	}

	base := NewEvaluator() // your existing symbolic evaluator
	return NewFeedbackEvaluator(base, sm, "http://localhost:8080/principles")
}

func TestFeedbackScoring(t *testing.T) {
	fe := setupFE(t)

	opt := Option{TaskName: "PrimeNumberGenerator", Language: "python"}

	// Record some successful runs
	err := fe.RecordFeedback("PrimeNumberGenerator", "python", true, 500*time.Millisecond, 0)
	assert.NoError(t, err)
	err = fe.RecordFeedback("PrimeNumberGenerator", "python", true, 400*time.Millisecond, 0)
	assert.NoError(t, err)

	// Now score should be higher than baseline
	score, err := fe.ScoreOption(opt)
	assert.NoError(t, err)
	assert.True(t, score > 0)
}

func TestViolationPenalty(t *testing.T) {
	fe := setupFE(t)

	opt := Option{TaskName: "DangerousTask", Language: "python"}

	// Record violations with failure
	_ = fe.RecordFeedback("DangerousTask", "python", false, 200*time.Millisecond, 3)

	score, _ := fe.ScoreOption(opt)

	// Get base score for comparison
	baseScore, _ := fe.base.ScoreOption(opt)

	// Debug output
	t.Logf("Base score: %f, Feedback score: %f", baseScore, score)

	// The score should be lower than base due to violations
	assert.True(t, score < baseScore, "violations should reduce score from base")
	assert.True(t, score < 0, "violations should result in negative score")
}

func TestPrinciplesIntegration(t *testing.T) {
	fe := setupFE(t)

	opt := Option{TaskName: "SecureTask", Language: "python"}

	// Record some principle violations
	err := fe.RecordFeedback("SecureTask", "python", true, 300*time.Millisecond, 2)
	assert.NoError(t, err)
	err = fe.RecordFeedback("SecureTask", "python", false, 400*time.Millisecond, 1)
	assert.NoError(t, err)

	// Score should be heavily penalized due to principle violations
	score, err := fe.ScoreOption(opt)
	assert.NoError(t, err)

	baseScore, _ := fe.base.ScoreOption(opt)
	t.Logf("Base score: %f, Principles-aware score: %f", baseScore, score)

	// Should be much lower due to principle violations
	assert.True(t, score < baseScore, "principle violations should heavily penalize score")
	assert.True(t, score < -10, "principle violations should result in very negative score")
}
