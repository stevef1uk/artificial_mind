# Reasoning and Inference Layer

This document describes the reasoning layer that enables querying and inference over the domain knowledge graph (Neo4j) and the associated observability.

## Components

- FSM ReasoningEngine (`fsm/reasoning_engine.go`):
  - Translates simple NL queries into Cypher (toy translator)
  - Sends Cypher to HDN endpoint
  - Applies dynamic inference rules based on actual concept patterns
  - Logs `ReasoningTrace` to Redis for the Monitor UI with proper conclusions
  - Enhanced debug logging for troubleshooting inference execution

- FSM KnowledgeIntegration (`fsm/knowledge_integration.go`):
  - Generates data-driven hypotheses from facts and domain knowledge
  - Implements intelligent exploration tracking to avoid redundant exploration
  - Comprehensive deduplication to prevent duplicate hypothesis generation
  - Smart re-exploration when new facts are available
  - **Causal Reasoning Enrichment**: Automatically enriches all hypotheses with causal reasoning signals
    - Classifies hypotheses by causal type (observational, inferred causal candidate, experimentally testable)
    - Generates counterfactual reasoning actions to challenge beliefs
    - Creates intervention goals for experimental testing

- FSM Hypothesis Screening (`fsm/engine.go`):
  - Screens hypotheses using LLM evaluation via HDN interpreter
  - Configurable threshold for hypothesis approval
  - Creates curiosity goals only for approved hypotheses

- HDN Knowledge Query (`hdn/api.go`):
  - `POST /api/v1/knowledge/query` accepts `{ "query": "MATCH ..." }`
  - Executes Cypher via Neo4j driver when built with `-tags neo4j`
  - Returns `{ results: [...], count: N }`

- Neo4j Helper (`hdn/memory/cypher_query.go`):
  - `ExecuteCypher(ctx, uri, user, pass, query)` reads/writes with driver
  - Stubbed when Neo4j tag is not set (`cypher_query_stub.go`)

- Monitor Reasoning Views (`monitor/main.go`, dashboard):
  - `GET /api/reasoning/traces/:domain` → shows latest reasoning traces
  - `GET /api/reasoning/hypotheses/:domain` → shows generated and screened hypotheses

## Recent Improvements

### Intelligent Exploration System
- **Smart Exploration Tracking**: Prevents redundant exploration of recently analyzed concepts
- **Time-Based Avoidance**: Won't re-explore concepts within 6 hours unless new facts available
- **New Facts Override**: Re-explores concepts when new information is available
- **Comprehensive Deduplication**: Prevents duplicate hypothesis generation across all methods

### Dynamic Inference Rules
- **Data-Driven Rules**: Inference rules now adapt to actual concept patterns in the knowledge base
- **Academic Field Classification**: Identifies academic fields based on definition keywords
- **Technology Classification**: Identifies technology-related concepts
- **Concept Similarity**: Finds similar concepts based on name matching
- **Domain Relationships**: Finds concepts that reference each other in definitions
- **Practical Application**: Identifies concepts with practical applications

### Enhanced Debugging
- **Detailed Logging**: Enhanced logging for troubleshooting inference rule execution
- **Proper Conclusions**: Reasoning traces now show meaningful conclusions instead of "No conclusion reached"
- **Exploration Tracking**: Clear logging of exploration decisions and duplicate prevention

## Build & Run

1. Start infra:
```bash
docker-compose up -d redis neo4j nats qdrant
```

2. Build HDN with Neo4j support:
```bash
go build -tags neo4j ./hdn && bin/hdn-server
```

3. Build and run FSM:
```bash
bin/fsm-server
```

4. Start Monitor UI:
```bash
bin/monitor-ui
```

## Data Keys in Redis

- `reasoning:traces:<domain>`: list of `ReasoningTrace` JSON items
- `reasoning:traces:goal:<goal>`: traces bucketed by goal
- `fsm:agent_1:hypotheses`: hash of generated hypotheses with LLM screening scores
- `reasoning:curiosity_goals:<domain>`: list of `CuriosityGoal` JSON items
- `reasoning:news_relations:recent`: recent news relations for goal generation
- `reasoning:news_alerts:recent`: recent news alerts for goal generation

## Hypothesis Generation and LLM Screening

The system generates testable hypotheses from facts and domain knowledge:

### Hypothesis Generation Process

1. **Fact Extraction**: FSM extracts facts from context during processing
2. **Domain Classification**: Facts are classified by domain (General, Math, Programming, etc.)
3. **Hypothesis Creation**: Two types of hypotheses are generated:
   - **Fact-Based**: Created directly from extracted facts with domain context
   - **Pattern-Based**: Generated from domain-specific patterns and concepts
4. **Causal Reasoning Enrichment**: All hypotheses are automatically enriched with causal reasoning signals:
   - **Causal Classification**: Hypotheses are tagged as `observational_relation`, `inferred_causal_candidate`, or `experimentally_testable_relation`
   - **Counterfactual Actions**: Generates questions like "what outcome would change my belief?" to challenge beliefs
   - **Intervention Goals**: Creates experimental goals to test causal hypotheses
5. **LLM Screening**: Each hypothesis is evaluated by HDN interpreter for impact and tractability (includes causal reasoning fields)
6. **Goal Creation**: Only approved hypotheses become curiosity goals for testing
   - **Intervention Goals**: Causal hypotheses automatically generate intervention goals with higher priority (priority=10)

### Configuration

```yaml
agent:
  hypothesis_screen_threshold: 0.6  # Adjust for more/less selective screening
```

### LLM Screening Details

- **Evaluation Prompt**: "Rate the following hypothesis for impact and tractability in domain 'X' on a 0.0-1.0 scale"
- **Causal Reasoning Context**: Prompt includes causal type, counterfactual actions, and intervention goals for better evaluation
- **Response Format**: JSON with `score` and `reason` fields
- **Fallback**: If LLM evaluation fails, hypothesis is approved by default
- **Threshold**: Only hypotheses scoring above threshold become testing goals
- **Intervention Goals**: Causal hypotheses automatically generate intervention goals with higher priority (priority=10) and boosted value (1.2x multiplier)

## Default Inference Rules (toy)

- IS_A Transitivity: `(a)-[:IS_A]->(b)-[:IS_A]->(c)` ⇒ `(a)-[:IS_A]->(c)`
- PART_OF Transitivity: `(a)-[:PART_OF]->(b)-[:PART_OF]->(c)` ⇒ `(a)-[:PART_OF]->(c)`
- ENABLES Transitivity: `(a)-[:ENABLES]->(b)-[:ENABLES]->(c)` ⇒ `(a)-[:ENABLES]->(c)`

## Curiosity Goals System

The reasoning layer includes an autonomous curiosity goals system that generates exploration goals:

### Goal Types

- **Gap Filling**: Goals to fill knowledge gaps for concepts without relationships
- **Contradiction Resolution**: Goals to resolve conflicting information
- **Concept Exploration**: Goals to explore new concepts and relationships
- **News Analysis**: Goals generated from news events and alerts

### Goal Lifecycle

1. **Generation**: Goals are generated during the `reason` state
2. **Selection**: The autonomy cycle selects goals based on priority and cooldown
3. **Processing**: Selected goals are marked as "active" and processed
4. **Completion**: Goals are marked as "completed" when successfully processed
5. **Cleanup**: Old and completed goals are automatically cleaned up

### News-Driven Goals

News events automatically generate curiosity goals:

- **News Relations**: `"Analyze news relation: Actor action target"`
- **News Alerts**: `"Investigate news alert: Headline"` with priority based on impact
- **Automatic Storage**: News events stored in Redis for goal generation
- **Deduplication**: Prevents duplicate goals from being created

### Intelligent Goal Prioritization

The system uses sophisticated scoring to prioritize goals based on multiple factors:

#### Priority Scoring Factors

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

#### Processing Limits

- **Max 3 concurrent active goals** per domain
- **2-hour cooldown** for recently processed goals
- **Capacity checking** before starting new goals

#### Goal Selection Process

1. Check processing capacity
2. Score all available goals
3. Sort by priority score (highest first)
4. Apply eligibility checks (cooldown, duplicates)
5. Select highest-scoring eligible goal
6. Track processing to avoid immediate re-processing

## Security & Safety Notes

- The `/api/v1/knowledge/query` endpoint runs arbitrary Cypher. Restrict access in production (authn/z, allow-list, or safe translator-only mode).
- Explanations and traces include context; avoid sensitive data.


