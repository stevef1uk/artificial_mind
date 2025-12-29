# Active Learning Loops

## Overview

Active Learning Loops introduce **query-driven learning** to the curiosity system, transforming it from reactive scanning to structured inquiry. Instead of only reacting to news and gaps, the system now proactively identifies high-uncertainty concepts and generates targeted data acquisition plans to reduce uncertainty fastest.

## Problem Statement

**Before Active Learning Loops:**
- Curiosity goals arose from news + gaps (reactive)
- Exploration was cooldown-bounded
- Meta-learning influenced scoring
- System appeared mostly reactive

**After Active Learning Loops:**
- ✅ Structured inquiry into high-uncertainty concepts
- ✅ Targeted data acquisition plans
- ✅ Prioritized experiments that reduce uncertainty fastest
- ✅ Not just opportunistic scanning

## Key Features

### 1. High-Uncertainty Concept Identification

The system identifies concepts with high epistemic uncertainty (reducible through gathering more information) from:
- **Beliefs**: Concepts mentioned in beliefs with high uncertainty
- **Hypotheses**: Concepts in hypotheses with high epistemic uncertainty
- **Goals**: Concepts targeted by goals with high uncertainty

**Threshold**: Default 0.4 epistemic uncertainty (configurable)

### 2. Data Acquisition Plan Generation

For each high-uncertainty concept, the system generates a structured plan with:
- **Target Concept**: The concept to investigate
- **Uncertainty Reduction Potential**: How much uncertainty can be reduced
- **Priority**: Calculated from uncertainty reduction potential and evidence count
- **Acquisition Steps**: Sequence of actions to acquire data
  - Step 1: Query knowledge base (20% uncertainty reduction)
  - Step 2: Fetch external data (30% uncertainty reduction)
  - Step 3: Generate and test hypothesis (40% uncertainty reduction)
- **Expected Outcome**: Description of what will be learned
- **Estimated Time**: How long the plan will take

### 3. Experiment Prioritization

Experiments are prioritized by **efficiency**: uncertainty reduction potential per unit time.

**Efficiency Score** = `Uncertainty Reduction Potential / Estimated Time (hours)`

Plans with:
- Higher uncertainty reduction potential
- Shorter estimated time
- Very high uncertainty (>0.7) get 1.5x efficiency boost

### 4. Integration with Curiosity Goals

Data acquisition plans are automatically converted to curiosity goals with:
- **Type**: `active_learning`
- **Priority**: Based on efficiency ranking (top plan = 10, second = 9, etc.)
- **Uncertainty Model**: Includes epistemic uncertainty and reduction potential
- **Value**: Set to uncertainty reduction potential

## Implementation

### Files Added

- `fsm/active_learning.go`: Core active learning loop implementation (516 lines)
  - `ActiveLearningLoop` struct
  - `HighUncertaintyConcept` struct
  - `DataAcquisitionPlan` struct
  - `AcquisitionStep` struct

### Files Modified

- `fsm/reasoning_engine.go`: Added `generateActiveLearningGoals()` function
  - Integrated into `GenerateCuriosityGoals()` as step 5
  - Generates active learning goals alongside gap filling, contradiction resolution, exploration, and news goals

### Key Functions

#### `IdentifyHighUncertaintyConcepts(domain, threshold)`
- Scans beliefs, hypotheses, and goals for high epistemic uncertainty
- Calculates uncertainty reduction potential: `epistemic * (1 - aleatoric)`
- Returns sorted list (highest potential first)

#### `GenerateDataAcquisitionPlans(concepts, maxPlans)`
- Creates structured plans for each high-uncertainty concept
- Generates acquisition steps (query, fetch, test)
- Calculates priority and estimated time

#### `PrioritizeExperiments(plans)`
- Ranks plans by efficiency (uncertainty reduction / time)
- Recalculates priorities based on efficiency ranking
- Returns prioritized list

#### `ConvertPlansToCuriosityGoals(plans)`
- Converts data acquisition plans to curiosity goals
- Sets appropriate type, priority, uncertainty model, and value

## Usage

Active learning loops are automatically integrated into the curiosity goal generation system. When `GenerateCuriosityGoals()` is called:

1. System identifies high-uncertainty concepts (threshold: 0.4)
2. Generates data acquisition plans (top 5 concepts)
3. Prioritizes experiments by efficiency
4. Converts plans to curiosity goals
5. Returns goals alongside other curiosity goal types

## Example

**High-Uncertainty Concept Identified:**
```
Concept: "Quantum Entanglement"
Epistemic Uncertainty: 0.65
Aleatoric Uncertainty: 0.1
Uncertainty Reduction Potential: 0.585
Sources: [belief:bel_123, hypothesis:hyp_456]
Evidence Count: 2
```

**Data Acquisition Plan Generated:**
```
Plan ID: active_learning_plan_Quantum_Entanglement_1234567890
Target: Quantum Entanglement
Priority: 9
Steps:
  1. Query knowledge base (20% reduction)
  2. Fetch external data (30% reduction)
  3. Generate and test hypothesis (40% reduction)
Expected Outcome: Reduce epistemic uncertainty from 0.65 to <0.33
Estimated Time: 25 minutes
```

**Curiosity Goal Created:**
```
Type: active_learning
Description: [ACTIVE-LEARNING] query_knowledge_base: Query Neo4j knowledge base for existing information about 'Quantum Entanglement'
Priority: 9
Uncertainty: {epistemic: 0.585, aleatoric: 0.1, calibrated: 0.65}
Value: 0.585
```

## Benefits

1. **Structured Inquiry**: System actively seeks to reduce uncertainty rather than just reacting
2. **Efficient Learning**: Prioritizes experiments that reduce uncertainty fastest
3. **Targeted Data Acquisition**: Generates specific plans for each high-uncertainty concept
4. **Integration**: Seamlessly integrates with existing curiosity goal system
5. **Uncertainty-Aware**: Uses formal uncertainty models for principled decision-making

## Configuration

- **Uncertainty Threshold**: Default 0.4 (configurable in `generateActiveLearningGoals()`)
- **Max Plans**: Default 5 (configurable in `GenerateDataAcquisitionPlans()`)
- **Efficiency Boost**: 1.5x for plans with uncertainty reduction potential > 0.7

## Future Enhancements

- Dynamic threshold adjustment based on domain characteristics
- Cross-domain uncertainty transfer
- Learning from plan execution outcomes
- Adaptive step generation based on concept type
- Integration with explanation-grounded learning feedback

