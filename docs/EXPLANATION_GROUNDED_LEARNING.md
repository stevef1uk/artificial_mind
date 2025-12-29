# Explanation-Grounded Learning Feedback

## Overview

The Explanation-Grounded Learning Feedback system closes the loop between reasoning quality → execution outcomes → improved reasoning. This system evaluates hypotheses, explanations, and reasoning traces after each goal completion to continuously improve the AI's reasoning capabilities.

## Problem Statement

Previously, the system had:
- Hypothesis generation
- LLM hypothesis screening
- Reasoning traces

But nothing indicated whether:
- Explanations improve over time
- Reasoning quality correlates with outcomes
- Confidence calibration is accurate

## Solution

After each completed goal (achieved or failed), the system now:

1. **Evaluates post-hoc:**
   - Accuracy of hypotheses (were they correct?)
   - Quality of explanations (how well-structured were they?)
   - Alignment with outcome (did reasoning match results?)

2. **Updates learning parameters:**
   - Inference weighting (adjusts which inference patterns to trust)
   - Confidence scaling rules (calibrates confidence predictions)
   - Exploration heuristics (balances exploration vs exploitation)

## Architecture

### Components

1. **ExplanationLearningFeedback** (`fsm/explanation_learning.go`)
   - Main learning feedback system
   - Evaluates goal completions
   - Updates learning parameters

2. **Goal Completion Integration** (`fsm/engine.go`)
   - Subscribes to `agi.goal.achieved` and `agi.goal.failed` NATS events
   - Triggers learning feedback evaluation

3. **Data Storage** (Redis)
   - Feedback records: `explanation_learning:feedback:{goal_id}`
   - Domain statistics: `explanation_learning:stats:{domain}`
   - Confidence scaling: `explanation_learning:confidence_scaling:{domain}`
   - Exploration heuristics: `explanation_learning:exploration_heuristics:{domain}`

## Evaluation Metrics

### Hypothesis Evaluation

- **Accuracy**: Binary (1.0 if correct, 0.0 if incorrect)
- **Quality**: Based on explanation richness (facts, uncertainty model, counterfactuals)
- **Alignment**: How well hypothesis confidence matched actual outcome
- **Confidence Error**: Difference between predicted and actual confidence

### Reasoning Trace Evaluation

- **Reasoning Quality**: Based on number and structure of reasoning steps
- **Step Coherence**: How well-structured the reasoning steps are
- **Decision Quality**: Average confidence of decisions made
- **Confidence Calibration**: How well-calibrated confidence was with outcome
- **Outcome Correlation**: Correlation between reasoning quality and actual outcome

### Overall Metrics

- **Overall Accuracy**: Average hypothesis accuracy
- **Overall Quality**: Average reasoning quality
- **Alignment Score**: Weighted combination of hypothesis alignment and trace correlation

## Learning Updates

### Inference Weighting

- Rules that led to accurate hypotheses get higher weights
- Rules that led to inaccurate hypotheses get lower weights
- Adjustments stored in: `explanation_learning:inference_adjustments:{domain}`

### Confidence Scaling

- Analyzes confidence calibration errors
- Adjusts `calibration_factor` based on overconfidence/underconfidence
- Overconfident (high errors) → reduce calibration factor
- Underconfident (low errors) → increase calibration factor

### Exploration Heuristics

- **Exploration Rate**: Fraction of time spent exploring vs exploiting
- **Curiosity Bonus**: Bonus for curiosity-driven goals
- Adjustments based on:
  - Low quality → increase exploration
  - High quality → decrease exploration (exploit more)
  - High alignment → maintain/increase curiosity
  - Low alignment → decrease curiosity

## Usage

### Automatic Operation

The system operates automatically:
1. Goal completes (achieved/failed)
2. GoalManager publishes NATS event
3. FSM engine receives event
4. Learning feedback evaluation triggered
5. Parameters updated in Redis

### Querying Learning Statistics

```go
// Get learning statistics for a domain
stats, err := explanationLearning.GetLearningStats("General")

// Get confidence scaling factors
scaling, err := explanationLearning.GetConfidenceScaling("General")

// Get exploration heuristics
heuristics, err := explanationLearning.GetExplorationHeuristics("General")
```

## Redis Keys

### Feedback Records
- `explanation_learning:feedback:{goal_id}` - Individual feedback record
- `explanation_learning:feedback:domain:{domain}` - List of feedback for domain
- `explanation_learning:feedback:all` - Global feedback list

### Statistics
- `explanation_learning:stats:{domain}` - Aggregate statistics per domain
  - `total_goals`: Total number of goals evaluated
  - `achieved_goals`: Number of achieved goals
  - `failed_goals`: Number of failed goals
  - `avg_accuracy`: Average hypothesis accuracy
  - `avg_quality`: Average reasoning quality
  - `avg_alignment`: Average alignment score

### Learning Parameters
- `explanation_learning:confidence_scaling:{domain}` - Confidence calibration factors
- `explanation_learning:exploration_heuristics:{domain}` - Exploration/exploitation balance
- `explanation_learning:inference_adjustments:{domain}` - Inference weight adjustments

## Future Enhancements

1. **LLM-Based Evaluation**: Use LLM to evaluate explanation quality more accurately
2. **Temporal Analysis**: Track improvements over time
3. **Cross-Domain Learning**: Transfer learning between domains
4. **Active Learning**: Proactively seek feedback on uncertain predictions
5. **Explanation Generation**: Generate better explanations based on learned patterns

## Integration Points

The system integrates with:
- **Goal Manager**: Receives goal completion events
- **FSM Engine**: Subscribes to goal events and triggers evaluation
- **Reasoning Engine**: Uses updated inference weights
- **Knowledge Integration**: Uses updated confidence scaling
- **Autonomy System**: Uses updated exploration heuristics

## Monitoring

Log messages are prefixed with `[EXPLANATION-LEARNING]` for easy filtering:
- `🧠 [EXPLANATION-LEARNING] Evaluating goal completion`
- `✅ [EXPLANATION-LEARNING] Completed evaluation`
- `📉 [EXPLANATION-LEARNING] Reducing confidence calibration`
- `📈 [EXPLANATION-LEARNING] Increasing confidence calibration`
- `🔍 [EXPLANATION-LEARNING] Increasing exploration`
- `🎯 [EXPLANATION-LEARNING] Decreasing exploration`

## Example Flow

1. Goal "Reduce API error rate" is achieved
2. System collects:
   - Hypotheses: "Rate limiting will reduce errors" (confidence: 0.8)
   - Reasoning traces: 5 steps, average confidence: 0.75
3. Evaluation:
   - Hypothesis accuracy: 1.0 (correct)
   - Reasoning quality: 0.8
   - Alignment: 0.85
4. Updates:
   - Inference weighting: +0.1 (rule performed well)
   - Confidence scaling: calibration_factor = 1.0 (well-calibrated)
   - Exploration: exploration_rate = 0.08 (slightly reduced, high quality)

## Benefits

1. **Continuous Improvement**: System learns from every goal completion
2. **Better Calibration**: Confidence values become more accurate over time
3. **Smarter Exploration**: Balances exploration vs exploitation based on outcomes
4. **Quality Tracking**: Monitors whether explanations and reasoning improve
5. **Closed Loop**: Connects reasoning quality to execution outcomes

