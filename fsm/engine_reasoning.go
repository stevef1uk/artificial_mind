package main

// Helper methods for reasoning
func (e *FSMEngine) getCurrentGoal() string {
	if goal, ok := e.context["current_goal"].(string); ok {
		return goal
	}
	return "Unknown goal"
}

func (e *FSMEngine) getCurrentReasoningSteps() []ReasoningStep {
	if steps, ok := e.context["reasoning_steps"].([]ReasoningStep); ok {
		return steps
	}
	return []ReasoningStep{}
}

func (e *FSMEngine) getCurrentEvidence() []string {
	if evidence, ok := e.context["evidence"].([]string); ok {
		return evidence
	}
	return []string{}
}

func (e *FSMEngine) getCurrentConclusion() string {
	if conclusion, ok := e.context["conclusion"].(string); ok {
		return conclusion
	}
	return "No conclusion reached"
}

func (e *FSMEngine) getCurrentConfidence() float64 {
	if confidence, ok := e.context["confidence"].(float64); ok {
		return confidence
	}
	return 0.5
}
