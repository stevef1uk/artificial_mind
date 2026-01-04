# Dream Mode: Creative Knowledge Exploration

## Overview

Dream Mode is a self-learning mechanism that generates creative exploration goals by randomly connecting concepts from the knowledge base. It operates like a "cognitive dream state" where the system discovers unexpected relationships and patterns.

## How It Works

### Concept Selection
1. Queries Neo4j knowledge base for random concepts
2. Selects diverse concepts across different domains
3. Avoids duplicate pairings

### Goal Generation
Dream Mode creates exploration goals using templates like:
- "Explore the relationship between 'X' and 'Y' - what connections exist?"
- "Compare and contrast 'X' with 'Y' to identify similarities and differences"
- "Investigate how 'X' might influence or interact with 'Y'"
- "Discover unexpected connections linking 'X' to 'Y'"

### Execution Flow
```
Dream Cycle (every 15 min) 
  → Query random concepts from Neo4j
  → Randomly pair concepts  
  → Generate exploration goals
  → Store in Redis curiosity_goals
  → Monitor picks up → Goal Manager → FSM Goals Poller → HDN execution
```

## Configuration

**Environment Variable**: `DREAM_INTERVAL_MINUTES`
- Default: 15 minutes
- Example: `DREAM_INTERVAL_MINUTES=30` for 30-minute intervals

**Goals per cycle**: 3 dream goals

## Benefits

### 1. Serendipitous Discovery
- Connects seemingly unrelated concepts
- Finds unexpected patterns and relationships
- Breaks out of domain silos

### 2. Knowledge Graph Enrichment
- Discovers new edges between nodes
- Identifies missing relationships
- Validates or challenges existing connections

### 3. Creative Thinking
- Generates novel hypotheses through cross-domain pollination
- Encourages lateral thinking
- Explores unconventional combinations

### 4. Continuous Learning
- Never runs out of exploration topics
- Automatically adapts as knowledge base grows
- Self-directed curiosity

## Examples

**Dream Goal 1**:
```
Explore the relationship between 'neural networks' and 'evolutionary algorithms'
Domain: Machine Learning + Optimization
Priority: 6 (medium-high)
```

**Dream Goal 2**:
```
Investigate how 'cognitive architectures' might influence or interact with 'reinforcement learning'
Domain: Cognitive Science + Machine Learning
Priority: 6
```

**Dream Goal 3**:
```
Discover unexpected connections linking 'memory consolidation' to 'attention mechanisms'
Domain: Neuroscience + Deep Learning
Priority: 6
```

## Integration with Existing Systems

### Coherence Monitor
- Dream goals are treated as curiosity goals
- Subject to same quality screening
- Deduplicated with other goals

### Goals Poller
- Dreams routed based on type
- Exploration goals → reasoning engine
- Knowledge queries → direct Neo4j access

### Autonomy System
- Complements hypothesis generation
- Provides alternative goal source
- Reduces reliance on structured input

## Monitoring

**Logs to watch**:
```bash
kubectl logs fsm-pod | grep "\[Dream\]"
```

**Expected messages**:
```
💭 [Dream] Dream mode enabled with interval: 15m0s
💭 [Dream] Entering dream mode - generating creative exploration goals...
💭 [Dream] Retrieved 20 concepts from knowledge base
💭 [Dream] Created dream goal: neural networks ↔ symbolic reasoning
💭 [Dream] Generated 3 dream exploration goals
💭 [Dream] Dream cycle complete
```

## Future Enhancements

### Phase 1 (Current)
- ✅ Random concept pairing
- ✅ Template-based goal generation
- ✅ Periodic dream cycles

### Phase 2
- Weight concepts by unexplored status
- Prefer distant concepts (maximize novelty)
- Track which pairs have been explored

### Phase 3
- Learn which dream patterns produce valuable insights
- Adjust dream interval based on discovery rate
- Generate meta-dreams (dream about dreaming patterns)

### Phase 4
- Multi-concept dreams (3+ concepts)
- Temporal dreams (compare concepts across time)
- Analogical reasoning dreams

## Disabling Dream Mode

Set `DREAM_INTERVAL_MINUTES=0` or modify `dream_mode.go:14` to set `enabled: false`

## Performance Impact

- **CPU**: Minimal (1 Neo4j query every 15 minutes)
- **Memory**: ~1KB per dream goal
- **Network**: 1 HTTP request to HDN/Neo4j per cycle
- **Knowledge Base**: Read-only queries, no writes

## Success Metrics

Track in Monitor UI:
1. Dream goals created per hour
2. Dream goal success rate
3. Novel relationships discovered
4. Knowledge graph growth from dreams
