# Goal Diversity Quick Fix

## Problem
System is generating only 1 unique goal because all hypotheses have the same description:
`"Test hypothesis: If we apply insights from System state: learn, we can improve our General approach"`

## Root Cause
Limited input diversity - system only has facts in "General" domain about "System state: learn"

## Solutions Implemented

### 1. Disabled Generic Hypothesis Filter
**Files**: `fsm/autonomy.go` and `fsm/engine.go`

Disabled the filter that was blocking "generic" hypotheses. Now all hypotheses will be allowed through, even if they seem vague.

**Limitation**: Won't help if all hypotheses have identical descriptions (which they currently do)

### 2. Recommended: Add Diverse Exploration Goals

Create goals that explore different topics:

```bash
# Example: Add exploration goals via Goal Manager API
curl -X POST http://localhost:8090/goal \
  -H "Content-Type: application/json" \
  -d '{
    "agent_id": "agent_1",
    "description": "Use tool_http_get to fetch latest AI news from ArXiv",
    "priority": "medium",
    "context": {"source": "manual", "domain": "AI Research"}
  }'

curl -X POST http://localhost:8090/goal \
  -H "Content-Type: application/json" \
  -d '{
    "agent_id": "agent_1",
    "description": "[ACTIVE-LEARNING] query_knowledge_base: Explore concepts related to machine learning",
    "priority": "medium",
    "context": {"source": "manual", "domain": "Machine Learning"}
  }'

curl -X POST http://localhost:8090/goal \
  -H "Content-Type: application/json" \
  -d '{
    "agent_id": "agent_1",
    "description": "Use tool_html_scraper to analyze Wikipedia page about neural networks",
    "priority": "medium",
    "context": {"source": "manual", "domain": "Deep Learning"}
  }'
```

### 3. Long-term Solution: Improve Hypothesis Diversity

**Location**: `fsm/engine.go` - hypothesis generation

Need to:
- Generate hypotheses from multiple domains
- Use more varied fact sources
- Add randomness/exploration to hypothesis generation
- Query knowledge base for diverse concepts

## Deploy

```bash
cd /home/stevef/dev/artificial_mind/fsm
go build -o fsm
# Push Docker image and restart pod
```

## Expected Results

**After deploying filter removal**:
- Still only 1 goal (because all hypotheses identical)
- Need to add diverse input data or manual goals

**After adding manual exploration goals**:
- Multiple diverse goals in UI
- Goals will trigger different execution paths (knowledge queries, tool calls, etc.)
- System will start building more diverse knowledge

**After improving hypothesis generation**:
- Hypotheses about different topics
- More domains explored
- Natural goal diversity
