# Reasoning Explanation Examples

This document shows concrete examples of how the Artificial Mind explains its reasoning process.

## Overview

The system explains its reasoning through:
1. **Reasoning Traces** - Structured logs of reasoning steps
2. **Explanations** - Human-readable summaries generated from traces
3. **Thinking Mode** - Real-time thought expression

## Reasoning Trace Structure

A reasoning trace contains:
- **Goal**: What the system is trying to understand or accomplish
- **Steps**: Individual reasoning steps with actions, queries, and results
- **Evidence**: Supporting facts or concepts used
- **Conclusion**: The final conclusion reached
- **Confidence**: How confident the system is in its conclusion

## Example 1: Understanding TCP/IP Networking

### Reasoning Trace Data

```json
{
  "id": "trace_1704123456789",
  "goal": "Understand TCP/IP networking",
  "domain": "Networking",
  "created_at": "2024-01-15T10:30:00Z",
  "steps": [
    {
      "step_number": 1,
      "action": "query",
      "query": "MATCH (c:Concept {name: 'TCP/IP'}) RETURN c",
      "result": {
        "name": "TCP/IP",
        "domain": "Networking",
        "definition": "Suite of communication protocols"
      },
      "reasoning": "Looking up TCP/IP concept in knowledge base",
      "confidence": 0.9,
      "timestamp": "2024-01-15T10:30:01Z"
    },
    {
      "step_number": 2,
      "action": "infer",
      "query": "MATCH (a)-[:IS_A]->(b)-[:IS_A]->(c) WHERE a.name = 'TCP/IP' RETURN a, b, c",
      "result": {
        "a": "TCP/IP",
        "b": "Protocol",
        "c": "Communication"
      },
      "reasoning": "Applying transitivity rule: TCP/IP is a Protocol, Protocol is a Communication, therefore TCP/IP is a Communication",
      "confidence": 0.8,
      "timestamp": "2024-01-15T10:30:02Z"
    },
    {
      "step_number": 3,
      "action": "query",
      "query": "MATCH (tcp:Concept {name: 'TCP/IP'})-[:ENABLES]->(x) RETURN x",
      "result": {
        "enables": ["Internet Communication", "Data Transfer", "Network Connectivity"]
      },
      "reasoning": "Querying what TCP/IP enables in the network",
      "confidence": 0.85,
      "timestamp": "2024-01-15T10:30:03Z"
    }
  ],
  "evidence": [
    "TCP/IP concept",
    "Protocol concept",
    "Communication concept",
    "Internet Communication relationship"
  ],
  "conclusion": "TCP/IP enables communication through protocol hierarchy, specifically enabling Internet communication, data transfer, and network connectivity",
  "confidence": 0.85
}
```

### Generated Explanation

```
Reasoning explanation for goal: Understand TCP/IP networking

Approach 1:
  Goal: Understand TCP/IP networking
  Conclusion: TCP/IP enables communication through protocol hierarchy, specifically enabling Internet communication, data transfer, and network connectivity
  Confidence: 0.85
  Steps:
    1. query: Looking up TCP/IP concept in knowledge base (confidence: 0.90)
    2. infer: Applying transitivity rule: TCP/IP is a Protocol, Protocol is a Communication, therefore TCP/IP is a Communication (confidence: 0.80)
    3. query: Querying what TCP/IP enables in the network (confidence: 0.85)
```

## Example 2: Hypothesis Generation and Testing

### Reasoning Trace for Hypothesis Testing

```json
{
  "id": "trace_1704123456790",
  "goal": "Test hypothesis: Machine learning models can predict system failures",
  "domain": "Programming",
  "created_at": "2024-01-15T11:00:00Z",
  "steps": [
    {
      "step_number": 1,
      "action": "hypothesis_generation",
      "query": "Generate hypothesis from facts and domain knowledge",
      "result": {
        "hypothesis": "Machine learning models can predict system failures",
        "facts_used": ["ML models analyze patterns", "System failures have patterns", "Pattern analysis enables prediction"]
      },
      "reasoning": "Generated hypothesis from observed facts about ML and system failures",
      "confidence": 0.75,
      "timestamp": "2024-01-15T11:00:01Z"
    },
    {
      "step_number": 2,
      "action": "llm_screening",
      "query": "Evaluate hypothesis for impact and tractability",
      "result": {
        "score": 0.85,
        "reason": "High impact (preventing failures is valuable) and tractable (ML models exist for this purpose)"
      },
      "reasoning": "LLM evaluated hypothesis and approved it for testing",
      "confidence": 0.85,
      "timestamp": "2024-01-15T11:00:02Z"
    },
    {
      "step_number": 3,
      "action": "evidence_gathering",
      "query": "Gather evidence from knowledge base and tools",
      "result": {
        "supporting_evidence": 5,
        "contradicting_evidence": 0,
        "evidence_types": ["knowledge_base", "tool_results"]
      },
      "reasoning": "Gathered 5 pieces of supporting evidence from knowledge base and tool execution",
      "confidence": 0.80,
      "timestamp": "2024-01-15T11:00:05Z"
    },
    {
      "step_number": 4,
      "action": "evaluation",
      "query": "Evaluate hypothesis based on evidence",
      "result": {
        "status": "confirmed",
        "confidence": 0.85,
        "supporting_evidence_count": 5,
        "contradicting_evidence_count": 0
      },
      "reasoning": "Hypothesis confirmed with 0.85 confidence based on 5 pieces of supporting evidence and no contradicting evidence",
      "confidence": 0.85,
      "timestamp": "2024-01-15T11:00:06Z"
    }
  ],
  "evidence": [
    "ML models analyze patterns",
    "System failures have patterns",
    "Pattern analysis enables prediction",
    "Knowledge base: ML prediction examples",
    "Tool result: Successful ML failure prediction case"
  ],
  "conclusion": "Hypothesis confirmed: Machine learning models can predict system failures with 0.85 confidence based on 5 pieces of supporting evidence",
  "confidence": 0.85
}
```

### Generated Explanation

```
Reasoning explanation for goal: Test hypothesis: Machine learning models can predict system failures

Approach 1:
  Goal: Test hypothesis: Machine learning models can predict system failures
  Conclusion: Hypothesis confirmed: Machine learning models can predict system failures with 0.85 confidence based on 5 pieces of supporting evidence
  Confidence: 0.85
  Steps:
    1. hypothesis_generation: Generated hypothesis from observed facts about ML and system failures (confidence: 0.75)
    2. llm_screening: LLM evaluated hypothesis and approved it for testing (confidence: 0.85)
    3. evidence_gathering: Gathered 5 pieces of supporting evidence from knowledge base and tool execution (confidence: 0.80)
    4. evaluation: Hypothesis confirmed with 0.85 confidence based on 5 pieces of supporting evidence and no contradicting evidence (confidence: 0.85)
```

## Example 3: Autonomous Goal Generation

### Reasoning Trace for Curiosity Goal Generation

```json
{
  "id": "trace_1704123456791",
  "goal": "Generate curiosity goals for knowledge exploration",
  "domain": "General",
  "created_at": "2024-01-15T12:00:00Z",
  "steps": [
    {
      "step_number": 1,
      "action": "gap_analysis",
      "query": "MATCH (c:Concept) WHERE c.domain = 'General' AND (NOT (c)-[:RELATED_TO]->() OR c.definition IS NULL) RETURN c",
      "result": {
        "concepts_with_gaps": ["Quantum Computing", "Blockchain", "Neural Networks"]
      },
      "reasoning": "Identified 3 concepts with incomplete knowledge (missing relationships or definitions)",
      "confidence": 0.9,
      "timestamp": "2024-01-15T12:00:01Z"
    },
    {
      "step_number": 2,
      "action": "goal_generation",
      "query": "Generate goals to fill knowledge gaps",
      "result": {
        "goals_generated": 3,
        "goal_types": ["gap_filling", "gap_filling", "gap_filling"]
      },
      "reasoning": "Generated 3 gap-filling goals for concepts with incomplete knowledge",
      "confidence": 0.85,
      "timestamp": "2024-01-15T12:00:02Z"
    },
    {
      "step_number": 3,
      "action": "prioritization",
      "query": "Score and prioritize generated goals",
      "result": {
        "prioritized_goals": [
          {"description": "Fill gaps in knowledge for concept: Quantum Computing", "priority": 9, "score": 11.0},
          {"description": "Fill gaps in knowledge for concept: Neural Networks", "priority": 8, "score": 10.0},
          {"description": "Fill gaps in knowledge for concept: Blockchain", "priority": 7, "score": 9.0}
        ]
      },
      "reasoning": "Prioritized goals based on importance (Quantum Computing scored highest due to technical importance bonus)",
      "confidence": 0.8,
      "timestamp": "2024-01-15T12:00:03Z"
    }
  ],
  "evidence": [
    "Quantum Computing concept (incomplete)",
    "Neural Networks concept (incomplete)",
    "Blockchain concept (incomplete)"
  ],
  "conclusion": "Generated 3 curiosity goals for knowledge exploration, prioritized by importance and technical relevance",
  "confidence": 0.85
}
```

### Generated Explanation

```
Reasoning explanation for goal: Generate curiosity goals for knowledge exploration

Approach 1:
  Goal: Generate curiosity goals for knowledge exploration
  Conclusion: Generated 3 curiosity goals for knowledge exploration, prioritized by importance and technical relevance
  Confidence: 0.85
  Steps:
    1. gap_analysis: Identified 3 concepts with incomplete knowledge (missing relationships or definitions) (confidence: 0.90)
    2. goal_generation: Generated 3 gap-filling goals for concepts with incomplete knowledge (confidence: 0.85)
    3. prioritization: Prioritized goals based on importance (Quantum Computing scored highest due to technical importance bonus) (confidence: 0.80)
```

## Example 4: Inference Rule Application

### Reasoning Trace for Inference

```json
{
  "id": "trace_1704123456792",
  "goal": "Apply inference rules to generate new beliefs",
  "domain": "Technology",
  "created_at": "2024-01-15T13:00:00Z",
  "steps": [
    {
      "step_number": 1,
      "action": "rule_selection",
      "query": "Get inference rules for domain 'Technology'",
      "result": {
        "rules_retrieved": 5,
        "rule_names": [
          "Academic Field Classification",
          "Technology Classification",
          "Concept Similarity",
          "Domain Relationships",
          "Practical Application"
        ]
      },
      "reasoning": "Retrieved 5 inference rules applicable to Technology domain",
      "confidence": 0.9,
      "timestamp": "2024-01-15T13:00:01Z"
    },
    {
      "step_number": 2,
      "action": "rule_application",
      "query": "MATCH (a:Concept) WHERE a.domain = 'Technology' AND (a.definition CONTAINS 'technology' OR a.definition CONTAINS 'machine' OR a.definition CONTAINS 'system' OR a.definition CONTAINS 'device') RETURN a",
      "result": {
        "matches": 12,
        "concepts": ["Artificial Intelligence", "Machine Learning", "Computer System", "IoT Device"]
      },
      "reasoning": "Applied Technology Classification rule: Found 12 technology-related concepts based on definition keywords",
      "confidence": 0.85,
      "timestamp": "2024-01-15T13:00:02Z"
    },
    {
      "step_number": 3,
      "action": "belief_creation",
      "query": "Create beliefs from inference results",
      "result": {
        "beliefs_created": 12,
        "average_confidence": 0.85
      },
      "reasoning": "Created 12 new beliefs with average confidence 0.85 based on Technology Classification rule",
      "confidence": 0.85,
      "timestamp": "2024-01-15T13:00:03Z"
    }
  ],
  "evidence": [
    "Technology Classification inference rule",
    "12 technology-related concepts",
    "Definition keyword matching patterns"
  ],
  "conclusion": "Applied Technology Classification rule and created 12 new beliefs about technology-related concepts with 0.85 average confidence",
  "confidence": 0.85
}
```

### Generated Explanation

```
Reasoning explanation for goal: Apply inference rules to generate new beliefs

Approach 1:
  Goal: Apply inference rules to generate new beliefs
  Conclusion: Applied Technology Classification rule and created 12 new beliefs about technology-related concepts with 0.85 average confidence
  Confidence: 0.85
  Steps:
    1. rule_selection: Retrieved 5 inference rules applicable to Technology domain (confidence: 0.90)
    2. rule_application: Applied Technology Classification rule: Found 12 technology-related concepts based on definition keywords (confidence: 0.85)
    3. belief_creation: Created 12 new beliefs with average confidence 0.85 based on Technology Classification rule (confidence: 0.85)
```

## Accessing Explanations

### Via API

**Note:** All reasoning API endpoints are on the Monitor UI server (port 8082), not the FSM server.

```bash
# Get explanation for a specific goal (URL-encode the goal name)
curl "http://localhost:8082/api/reasoning/explanations/Understand%20TCP%2FIP%20networking"

# Get recent explanations (all goals)
curl "http://localhost:8082/api/reasoning/explanations"

# Get reasoning traces for a domain
curl "http://localhost:8082/api/reasoning/traces/Networking"

# Get reasoning traces for General domain
curl "http://localhost:8082/api/reasoning/traces/General"

# Get beliefs for a domain
curl "http://localhost:8082/api/reasoning/beliefs/Networking"

# Get curiosity goals for a domain
curl "http://localhost:8082/api/reasoning/curiosity-goals/Networking"

# Get hypotheses for a domain
curl "http://localhost:8082/api/reasoning/hypotheses/Networking"

# Get list of all reasoning domains
curl "http://localhost:8082/api/reasoning/domains"
```

**Important Notes:**
- The goal parameter in `/api/reasoning/explanations/:goal` should be URL-encoded
- The traces endpoint doesn't support a `limit` parameter - it returns up to 100 traces
- All endpoints return JSON with the data in a nested structure (e.g., `{"traces": [...]}`)

### Via Code

```go
// Generate explanation
explanation, err := reasoning.ExplainReasoning("Understand TCP/IP networking", "Networking")
if err != nil {
    log.Printf("Error: %v", err)
} else {
    log.Printf("Explanation:\n%s", explanation)
}

// Get reasoning traces
traces, err := reasoning.getReasoningTraces("Understand TCP/IP networking", "Networking", 5)
if err != nil {
    log.Printf("Error: %v", err)
} else {
    for _, trace := range traces {
        log.Printf("Trace: %s", trace.Conclusion)
    }
}
```

### Via Monitor UI

The Monitor UI displays reasoning explanations in the Reasoning panel:
- Navigate to `http://localhost:8082`
- Click on the "Reasoning" tab
- View reasoning traces and explanations in real-time

## Thinking Mode Examples

When `show_thinking: true` is enabled, the system also provides real-time thought expression:

```json
{
  "response": "TCP/IP is a suite of communication protocols...",
  "thoughts": [
    {
      "type": "thinking",
      "content": "I need to understand what TCP/IP is. Let me query the knowledge base.",
      "state": "reason",
      "goal": "Understand TCP/IP networking",
      "confidence": 0.8,
      "timestamp": "2024-01-15T10:30:01Z"
    },
    {
      "type": "decision",
      "content": "I'll query the knowledge base for TCP/IP concept first, then apply inference rules to understand its relationships.",
      "state": "reason",
      "goal": "Understand TCP/IP networking",
      "confidence": 0.9,
      "timestamp": "2024-01-15T10:30:02Z"
    },
    {
      "type": "action",
      "content": "Querying knowledge base for TCP/IP concept...",
      "state": "reason",
      "goal": "Understand TCP/IP networking",
      "confidence": 0.95,
      "action": "belief_query",
      "result": "Found TCP/IP concept with definition",
      "timestamp": "2024-01-15T10:30:03Z"
    }
  ]
}
```

## Key Features of Explanations

1. **Step-by-Step Reasoning**: Shows each step in the reasoning process
2. **Confidence Levels**: Indicates how confident the system is at each step
3. **Evidence Tracking**: Lists supporting evidence used in reasoning
4. **Conclusion**: Provides a clear conclusion reached
5. **Transparency**: Shows queries executed and results obtained
6. **Human-Readable**: Converts technical reasoning into understandable explanations

## Conclusion

The system provides comprehensive explanations of its reasoning through:
- Structured reasoning traces with detailed steps
- Human-readable explanations generated from traces
- Real-time thought expression in thinking mode
- Confidence scoring at each step
- Evidence tracking and conclusion generation

This transparency allows users to understand not just *what* the system concluded, but *how* and *why* it reached that conclusion.

