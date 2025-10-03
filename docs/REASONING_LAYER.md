# Reasoning Layer for FSM

The Reasoning Layer adds intelligent deduction and inference capabilities to the FSM system, enabling it to reason about stored knowledge rather than just storing and recalling it.

## Overview

The Reasoning Layer provides:

1. **Query Engine on Neo4j** - FSM can query knowledge as a "belief system"
2. **Inference Layer** - Forward-chaining rules for deduction
3. **Curiosity-driven Goals** - Auto-generate subgoals for knowledge exploration
4. **Explanation Logging** - Transparent reasoning traces

## Key Components

### 1. ReasoningEngine

The core reasoning engine that provides:

- **Belief Querying**: Query the knowledge base using natural language
- **Inference Rules**: Apply forward-chaining rules to generate new beliefs
- **Curiosity Goals**: Generate intrinsic goals for knowledge exploration
- **Reasoning Traces**: Log comprehensive reasoning steps
- **Explanations**: Generate human-readable explanations

### 2. Belief System

The knowledge base is treated as a belief system where:

- **Beliefs** represent facts with confidence scores
- **Evidence** links beliefs to supporting facts
- **Sources** track where beliefs originated
- **Domains** organize beliefs by subject area

### 3. Dynamic Inference Rules

Data-driven inference rules that adapt to actual concept patterns:

- **Academic Field Classification**: Identifies academic fields based on definition keywords (study, science, field, discipline)
- **Technology Classification**: Identifies technology-related concepts (technology, machine, system, device)
- **Concept Similarity**: Finds similar concepts based on name matching patterns
- **Domain Relationships**: Finds concepts that reference each other in their definitions
- **Practical Application**: Identifies concepts with practical applications (practice, application, use, implement)

**Key Features:**
- Dynamic Cypher query generation with domain parameter substitution
- Confidence scoring based on pattern matching strength
- Proper error handling and detailed logging
- Adapts to actual knowledge base content rather than hardcoded patterns

### 4. Intelligent Exploration System

Smart exploration tracking that prevents redundant exploration:

- **Exploration Memory**: Tracks when each concept was last explored (Redis with 24-hour expiration)
- **Time-Based Avoidance**: Won't re-explore concepts within 6 hours unless new facts available
- **New Facts Override**: Re-explores concepts when new information is available
- **Comprehensive Deduplication**: Prevents duplicate hypothesis generation across all methods

**Key Functions:**
- `hasRecentlyExplored()` - Checks if concept was explored recently
- `recordExploration()` - Records exploration timestamp
- `hasNewFactsForConcept()` - Checks for new facts since last exploration
- `isDuplicateHypothesis()` - Prevents duplicate hypothesis generation
- `extractConceptNamesFromHypothesis()` - Extracts concepts from hypothesis descriptions

### 5. Curiosity Goals

Intrinsic goals for knowledge exploration:

- **Gap Filling**: Find concepts without relationships or definitions
- **Contradiction Resolution**: Identify and resolve conflicting information
- **Concept Exploration**: Discover new concepts and relationships
- **News Analysis**: Generate goals from news events and alerts
- **Hypothesis Testing**: Test generated hypotheses through experimentation

### 5. Hypothesis Generation

The system generates testable hypotheses from facts and domain knowledge:

- **Fact-Based Hypotheses**: Created from extracted facts with domain context
- **Pattern-Based Hypotheses**: Generated from domain-specific patterns and concepts
- **LLM Screening**: Hypotheses are evaluated by LLM for impact and tractability
- **Configurable Threshold**: Screening threshold adjustable via YAML config
- **Goal Creation**: Approved hypotheses become curiosity goals for testing

#### Intelligent Goal Prioritization

The system uses sophisticated scoring to prioritize goals based on multiple factors:

**Priority Scoring Factors:**
- **Base Priority** (1-10): From goal type and importance
- **News Analysis Bonuses**:
  - +2.0 for time-sensitivity
  - +3.0 for high-impact news
  - +1.5 for medium-impact news
  - +2.0 for news < 1 hour old
  - +1.0 for news < 6 hours old
- **Gap Filling Bonuses**:
  - +2.0 for important technical concepts (AI, ML, security, etc.)
  - -1.0 for generic concepts (thing, stuff, etc.)
- **Aging Penalties**:
  - -2.0 for goals > 24 hours old
  - -1.0 for goals > 12 hours old
  - -1.5 for recently tried goals

**Processing Limits:**
- **Max 3 concurrent active goals** per domain
- **2-hour cooldown** for recently processed goals
- **Capacity checking** before starting new goals

**Goal Selection Process:**
1. Check processing capacity
2. Score all available goals
3. Sort by priority score (highest first)
4. Apply eligibility checks (cooldown, duplicates)
5. Select highest-scoring eligible goal
6. Track processing to avoid immediate re-processing

## FSM Integration

The reasoning layer integrates with the FSM through new states and actions:

### New States

- **`reason`**: Apply reasoning and inference to generate new beliefs
- **`reason_continue`**: Continue reasoning process and generate explanations

### New Actions

- **`reasoning.belief_query`**: Query beliefs from the knowledge base
- **`reasoning.inference`**: Apply inference rules to generate new beliefs
- **`reasoning.curiosity_goals`**: Generate curiosity-driven goals
- **`reasoning.explanation`**: Generate human-readable explanations
- **`reasoning.trace_logger`**: Log reasoning traces
- **`reasoning.news_storage`**: Store news events for goal generation
- **`planner.hypothesis_generator`**: Generate hypotheses from facts and domain knowledge

## Usage Examples

### Querying Beliefs

```go
// Query the knowledge base
beliefs, err := reasoning.QueryBeliefs("what is TCP/IP", "Networking")
if err != nil {
    log.Printf("Query failed: %v", err)
} else {
    for _, belief := range beliefs {
        log.Printf("Belief: %s (confidence: %.2f)", belief.Statement, belief.Confidence)
    }
}
```

### Applying Inference

```go
// Apply inference rules to generate new beliefs
newBeliefs, err := reasoning.InferNewBeliefs("Networking")
if err != nil {
    log.Printf("Inference failed: %v", err)
} else {
    for _, belief := range newBeliefs {
        log.Printf("Inferred: %s (confidence: %.2f)", belief.Statement, belief.Confidence)
    }
}
```

### Generating Curiosity Goals

```go
// Generate curiosity-driven goals
goals, err := reasoning.GenerateCuriosityGoals("Networking")
if err != nil {
    log.Printf("Goal generation failed: %v", err)
} else {
    for _, goal := range goals {
        log.Printf("Goal: %s (priority: %d)", goal.Description, goal.Priority)
    }
}
```

### Hypothesis Generation and Testing

The system generates testable hypotheses from facts and domain knowledge:

```go
// Generate hypotheses from facts
facts := []Fact{
    {Content: "User sent a test message", Domain: "communication", Confidence: 0.9},
    {Content: "FSM is processing input", Domain: "system", Confidence: 0.95},
}
hypotheses, err := knowledgeGrowth.GenerateHypotheses(facts, "General")
if err != nil {
    log.Printf("Hypothesis generation failed: %v", err)
} else {
    for _, hypothesis := range hypotheses {
        log.Printf("Hypothesis: %s (confidence: %.2f)", hypothesis.Description, hypothesis.Confidence)
    }
}
```

**LLM Screening Process:**
1. Each hypothesis is sent to HDN interpreter for evaluation
2. LLM rates hypothesis on impact and tractability (0.0-1.0 scale)
3. Only hypotheses above threshold become testing goals
4. Threshold configurable via `agent.hypothesis_screen_threshold` in YAML

**Configuration:**
```yaml
agent:
  hypothesis_screen_threshold: 0.6  # Adjust for more/less selective screening
```

### News-Driven Goal Generation

The system automatically generates curiosity goals from news events:

- **News Relations**: Goals like "Analyze news relation: Actor action target"
- **News Alerts**: Goals like "Investigate news alert: Headline" with priority based on impact
- **Automatic Storage**: News events are stored in Redis for goal generation
- **Status Tracking**: Goals progress from "pending" → "active" → "completed"

### Logging Reasoning Traces

```go
// Create a reasoning trace
trace := ReasoningTrace{
    ID:         "trace_1",
    Goal:       "Understand TCP/IP networking",
    Steps:      []ReasoningStep{...},
    Evidence:   []string{"TCP/IP concept", "Protocol concept"},
    Conclusion: "TCP/IP enables communication",
    Confidence: 0.85,
    Domain:     "Networking",
    CreatedAt:  time.Now(),
}

// Log the trace
err := reasoning.LogReasoningTrace(trace)
if err != nil {
    log.Printf("Trace logging failed: %v", err)
}
```

## Configuration

The reasoning layer is configured in the FSM YAML configuration:

```yaml
states:
  - name: reason
    description: "Apply reasoning and inference to generate new beliefs"
    actions:
      - type: query_beliefs
        module: "reasoning.belief_query"
        params:
          query: "all concepts"
          use_domain_context: true
      - type: apply_inference
        module: "reasoning.inference"
        params:
          use_transitivity_rules: true
          confidence_threshold: 0.7
      - type: generate_curiosity_goals
        module: "reasoning.curiosity_goals"
        params:
          gap_filling: true
          contradiction_resolution: true
          concept_exploration: true
      - type: log_reasoning_trace
        module: "reasoning.trace_logger"
        params:
          include_steps: true
          include_evidence: true
```

## Monitoring

The reasoning layer includes comprehensive monitoring:

### Metrics

- `beliefs_queried`: Number of belief queries executed
- `beliefs_inferred`: Number of new beliefs inferred
- `curiosity_goals_generated`: Number of curiosity goals created
- `reasoning_traces_logged`: Number of reasoning traces logged
- `inference_confidence_avg`: Average confidence of inferences

### UI Panels

- **Reasoning Traces**: Stream of reasoning steps
- **Beliefs and Inferences**: Table of beliefs and inferred facts
- **Curiosity Goals**: List of generated exploration goals (including news-driven goals)
- **Active Hypotheses**: Display of generated and screened hypotheses
- **Reasoning Explanations**: Human-readable explanations

## Testing

Run the reasoning layer tests:

```bash
cd fsm
go run reasoning_test.go
```

Run the comprehensive demo:

```bash
cd examples
go run reasoning_demo.go
```

## Future Enhancements

Planned improvements include:

1. **Probabilistic Reasoning**: Add uncertainty quantification
2. **Rule Learning**: Learn new inference rules from data
3. **Causal Reasoning**: Understand cause-and-effect relationships
4. **Temporal Reasoning**: Reason about time and sequences
5. **Multi-hop Reasoning**: Complex multi-step reasoning chains

## Architecture

```
┌─────────────────┐    ┌──────────────────┐    ┌─────────────────┐
│   FSM Engine    │    │ Reasoning Engine │    │   Knowledge     │
│                 │    │                  │    │     Base        │
│ ┌─────────────┐ │    │ ┌──────────────┐ │    │                 │
│ │   reason    │◄┼────┼─┤ QueryBeliefs │ │    │ ┌─────────────┐ │
│ │   state     │ │    │ └──────────────┘ │    │ │   Neo4j     │ │
│ └─────────────┘ │    │ ┌──────────────┐ │    │ │  Graph DB   │ │
│ ┌─────────────┐ │    │ │ InferNewBeliefs│ │    │ └─────────────┘ │
│ │reason_cont. │◄┼────┼─┤              │ │    │ ┌─────────────┐ │
│ │   state     │ │    │ └──────────────┘ │    │ │   Redis     │ │
│ └─────────────┘ │    │ ┌──────────────┐ │    │ │   Cache     │ │
│                 │    │ │GenerateGoals │ │    │ └─────────────┘ │
│                 │    │ └──────────────┘ │    │                 │
└─────────────────┘    └──────────────────┘    └─────────────────┘
```

The reasoning layer transforms the FSM from a simple state machine into an intelligent agent capable of:

- **Reasoning about knowledge** rather than just storing it
- **Generating its own goals** for exploration and learning
- **Explaining its reasoning** in human-understandable terms
- **Learning from experience** through belief updates

This makes the system more "AI-like" by adding the ability to think, reason, and explain its thought processes.
