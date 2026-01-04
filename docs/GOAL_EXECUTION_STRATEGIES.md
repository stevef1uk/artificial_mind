# Goal Execution Strategies

## Problem
Currently, most goals are being routed to the intelligent executor which tries to generate and execute Python code. This is inefficient and inappropriate for many goal types.

## Current State

### Goal Sources (After Fixes)
1. **Hypothesis Testing** - Generates 1-2 goals per cycle ✅
2. **Behavior Loop Detection** - Max 1 per transition per 24h ✅
3. **Belief Contradictions** - DISABLED (too slow) ❌
4. **Active Learning / Curiosity** - Should generate goals but rate is low ⚠️
5. **Knowledge Growth** - Minimal activity ⚠️

### Execution Paths Available
1. **Intelligent Executor** - Generates Python/Go code (current default)
2. **Tool Calling** - Direct tool invocation (underutilized)
3. **Reasoning Engine** - Inference and explanation (underutilized)
4. **Knowledge Query** - Neo4j/Weaviate search (underutilized)

## Proposed Solutions

### Solution 1: Route Goals by Type

**Goal Type Routing Map**:
```
query_knowledge_base          → Knowledge Query (Neo4j/Weaviate)
tool_http_get                 → Tool Calling (direct HTTP)
tool_html_scraper             → Tool Calling (direct scraper)
analyze_inconsistency         → Reasoning Engine (inference)
test_hypothesis               → Mixed (tools + code if needed)
learn_*                       → Active Learning Loop
```

**Implementation**: Update `goals_poller.go` to route based on goal description/type

### Solution 2: Increase Curiosity Goal Generation

**Enable more active learning sources**:
- Knowledge gaps (what don't we know?)
- Unexplored domains
- Low-confidence beliefs
- Interesting patterns in data

**Location**: `fsm/knowledge_growth.go` and `fsm/active_learning.go`

### Solution 3: Re-enable Belief Checking (with limits)

Instead of disabling entirely:
- Limit to 10 beliefs max per domain
- Skip O(n²) comparison, use LLM to identify contradictions
- Run async or with timeout

**Location**: `fsm/coherence_monitor.go:checkBeliefContradictions()`

### Solution 4: Add Goal Templates

**Pre-defined goal templates that don't require code**:
```go
var GoalTemplates = map[string]GoalTemplate{
    "explore_domain": {
        Type: "knowledge_query",
        Executor: "knowledge_base",
        Template: "Query Neo4j for concepts in domain: {domain}",
    },
    "fact_check": {
        Type: "reasoning",
        Executor: "reasoning_engine",
        Template: "Verify belief: {belief} using available evidence",
    },
    "fetch_news": {
        Type: "tool_call",
        Executor: "tool_http_get",
        Template: "Fetch latest news about {topic}",
    },
}
```

## Recommended Immediate Actions

### 1. Quick Fix: Route Knowledge Queries Directly
**File**: `fsm/goals_poller.go`

```go
// Before sending to HDN intelligent executor, check if it's a knowledge query
if strings.Contains(goalDesc, "query_knowledge_base") || 
   strings.Contains(goalDesc, "Query Neo4j") {
    // Route to knowledge query endpoint instead
    executeKnowledgeQuery(goal)
    continue
}
```

### 2. Increase Curiosity Generation Rate
**File**: `fsm/knowledge_growth.go`

Reduce threshold for curiosity gap detection or increase generation frequency.

### 3. Add Tool-Based Goals to Autonomy
**File**: `fsm/autonomy.go`

Generate goals that call tools directly:
- "Use tool_http_get to fetch Wikipedia page about X"
- "Use tool_html_scraper to extract data from Y"

## Metrics to Track

After implementing:
1. Goals created per hour (by type)
2. Goals executed per hour (by execution path)
3. Success rate by execution path
4. Average execution time by path

Target: 5-10 diverse goals per hour across all types
