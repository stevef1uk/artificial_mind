# How Learning is Triggered

## Overview

Learning in the Artificial Mind system is triggered through the **FSM (Finite State Machine)** state transitions. The system follows a flow: `idle` → `perceive` → `learn` → `hypothesize` → ...

## Learning Flow

```
┌─────────┐
│  idle   │  ← Waits for input or timer events
└────┬────┘
     │ new_input event
     ▼
┌──────────┐
│ perceive │  ← Ingests and validates input
└────┬─────┘
     │ ingest_ok or domain_unknown event
     ▼
┌─────────┐
│  learn  │  ← **LEARNING HAPPENS HERE** (uses MCP!)
└────┬────┘
     │ facts_extracted event
     ▼
┌─────────────┐
│ hypothesize │  ← Generate hypotheses
└─────────────┘
```

## Trigger Mechanisms

### 1. **User Input** (Primary Trigger)

**How it works:**
1. User sends input via HTTP API: `POST /input` or `POST /api/v1/input`
2. FSM receives `new_input` event
3. Transitions from `idle` → `perceive`
4. After perception succeeds, emits `ingest_ok` event
5. Transitions from `perceive` → `learn`

**Example:**
```bash
curl -X POST http://localhost:8083/input \
  -H "Content-Type: application/json" \
  -d '{
    "input": "Machine learning uses neural networks to process data",
    "session_id": "test_session"
  }'
```

**State Machine Configuration:**
```yaml
- name: perceive
  on:
    ingest_ok:
      next: learn  # ← Triggers learning
    domain_unknown:
      next: learn  # ← Also triggers learning (with lower confidence)
```

### 2. **NATS Events** (Event-Driven)

**How it works:**
1. External system publishes event to NATS subject
2. FSM subscribes to NATS subjects (configured in `artificial_mind.yaml`)
3. Event is received and processed as `new_input`
4. Same flow as user input above

**NATS Subjects:**
- `agi.events.input` - General input events
- `agi.events.news_relations` - News relation events
- `agi.events.news_alerts` - News alert events

**Example:**
```bash
# Publish event via NATS
nats pub agi.events.input '{
  "input": "New scientific discovery about quantum computing",
  "session_id": "nats_test"
}'
```

### 3. **Autonomy Cycle** (Automatic)

**How it works:**
1. If `autonomy: true` in config, system runs periodic cycles
2. Every N seconds (configurable), triggers autonomy cycle
3. Autonomy cycle can generate curiosity goals
4. Goals can trigger learning through the normal flow

**Configuration:**
```yaml
autonomy: true
autonomy_every: 60  # seconds
```

**Code:**
```go
// From fsm/server.go
if config.Autonomy {
    go func() {
        ticker := time.NewTicker(time.Duration(config.AutonomyEvery) * time.Second)
        for {
            <-ticker.C
            engine.TriggerAutonomyCycle()  // ← Triggers learning
        }
    }()
}
```

### 4. **Timer Events** (Periodic)

**How it works:**
1. FSM has a timer loop that emits `timer_tick` events
2. If in `idle` state with `has_pending_work` guard, transitions to `perceive`
3. Can trigger learning if there's pending work

**Configuration:**
```yaml
- name: idle
  on:
    timer_tick:
      next: perceive
      guard: "has_pending_work"
```

## Learning State Actions

When the FSM enters the `learn` state, it executes these actions (which use MCP):

### 1. **Extract Facts** (`extract_facts`)
- Uses `knowledge_integration.go`
- Calls `ExtractFacts()` which uses MCP to:
  - Check if knowledge already exists (`knowledgeAlreadyExists()` → MCP `get_concept`)
  - Get domain concepts (`getDomainConcepts()` → MCP `query_neo4j`)

### 2. **Embed Episode** (`embed_episode`)
- Stores episode in Weaviate vector database
- Creates embeddings for semantic search

### 3. **Update Domain Knowledge** (`update_domain_knowledge`)
- Updates existing concepts
- Adds examples to concepts

### 4. **Discover New Concepts** (`discover_new_concepts`)
- Uses `knowledge_growth.go`
- Calls `DiscoverNewConcepts()` which uses MCP to:
  - Check if concept exists (`conceptAlreadyExists()` → MCP `get_concept`)
  - Get domain concepts (`getDomainConcepts()` → MCP `query_neo4j`)

### 5. **Find Knowledge Gaps** (`find_knowledge_gaps`)
- Identifies missing relationships
- Suggests new concepts

## Learning State Configuration

From `fsm/config/artificial_mind.yaml`:

```yaml
- name: learn
  description: "Extract facts and update domain knowledge - GROW KNOWLEDGE BASE"
  actions:
    - type: extract_facts
      module: "self.knowledge_extractor"
      params:
        use_domain_constraints: true
    - type: embed_episode
      module: "memory.embedding"
      params:
        store_in_weaviate: true
    - type: update_domain_knowledge
      module: "knowledge.updater"
      params:
        update_concepts: true
        add_examples: true
    - type: discover_new_concepts
      module: "knowledge.concept_discovery"
      params:
        auto_create_concepts: true
        confidence_threshold: 0.7
    - type: find_knowledge_gaps
      module: "knowledge.gap_analyzer"
      params:
        identify_missing_relations: true
        suggest_new_concepts: true
  on:
    facts_extracted:
      next: hypothesize
    new_concepts_discovered:
      next: hypothesize
    timer_tick:
      next: hypothesize
      guard: "learning_timeout"
```

## How to Trigger Learning

### Method 1: HTTP API (Easiest)

```bash
curl -X POST http://localhost:8083/input \
  -H "Content-Type: application/json" \
  -d '{
    "input": "I learned that quantum computers use qubits instead of bits",
    "session_id": "test_learning"
  }'
```

### Method 2: Via Monitor UI

1. Open http://localhost:8082
2. Navigate to input/chat interface
3. Send a message
4. System will automatically trigger learning

### Method 3: NATS Event

```bash
# Publish to NATS
nats pub agi.events.input '{
  "input": "New knowledge about artificial intelligence",
  "session_id": "nats_test"
}'
```

### Method 4: Autonomy (Automatic)

If autonomy is enabled, the system will automatically:
- Generate curiosity goals
- Trigger learning cycles
- Process pending work

## Monitoring Learning

### Check FSM State

```bash
curl http://localhost:8083/thinking | jq '.current_state'
```

### Watch Logs

```bash
# Watch for learning activity
tail -f /tmp/fsm_server.log | grep -i -E "(learn|extract|concept|mcp)"

# Watch for MCP usage
tail -f /tmp/fsm_server.log | grep -i -E "(via MCP|retrieved.*concepts|enhanced)"
```

### Check Activity

```bash
curl http://localhost:8083/activity?limit=10 | jq '.activities[] | select(.message | contains("learn"))'
```

## Learning Process Details

### Step 1: Input Reception
- Input received via HTTP or NATS
- Stored in FSM context
- `new_input` event emitted

### Step 2: Perception
- Input is parsed and validated
- Domain is classified
- `ingest_ok` or `domain_unknown` event emitted

### Step 3: Learning (MCP Integration)
- **Extract Facts**: Uses MCP `get_concept` to check duplicates
- **Get Domain Concepts**: Uses MCP `query_neo4j` to retrieve concepts
- **Discover Concepts**: Uses MCP `get_concept` to check if concept exists
- **Enhance with Related**: Uses MCP `find_related_concepts` for context

### Step 4: Hypothesis Generation
- Facts are used to generate hypotheses
- Concepts are enhanced with related concepts (via MCP)
- System transitions to `hypothesize` state

## Expected Log Messages

When learning is triggered, you should see:

```
🔄 Processing event: new_input in state: idle
✅ Found transition: idle -> perceive for event new_input
🔄 Processing event: ingest_ok in state: perceive
✅ Found transition: perceive -> learn for event ingest_ok
📚 Extracted X relevant facts from input
✅ Retrieved X concepts via MCP
🔍 Found existing concept via MCP: [concept_name]
🔗 Enhanced concept X with Y related concepts via MCP
📊 Knowledge growth stats: X concepts created, Y skipped
```

## Troubleshooting

### Learning Not Triggering

1. **Check FSM State:**
   ```bash
   curl http://localhost:8083/thinking | jq '.current_state'
   ```
   Should be `idle` or `perceive` to accept new input

2. **Check Input Endpoint:**
   ```bash
   curl -X POST http://localhost:8083/input \
     -H "Content-Type: application/json" \
     -d '{"input": "test", "session_id": "test"}'
   ```

3. **Check Logs:**
   ```bash
   tail -f /tmp/fsm_server.log
   ```

### MCP Not Being Used

1. **Verify MCP Endpoint:**
   ```bash
   curl http://localhost:8081/mcp
   ```

2. **Check Logs for Fallback:**
   ```bash
   tail -f /tmp/fsm_server.log | grep "falling back to direct API"
   ```

3. **Verify HDN URL:**
   ```bash
   echo $HDN_URL
   # Should be: http://localhost:8081
   ```

## Summary

Learning is triggered when:
1. ✅ User sends input via HTTP API
2. ✅ NATS event is received
3. ✅ Autonomy cycle runs (if enabled)
4. ✅ Timer event with pending work

The learning process automatically uses MCP tools for:
- ✅ Checking if knowledge exists
- ✅ Retrieving domain concepts
- ✅ Finding related concepts
- ✅ Querying the knowledge graph

All of this happens automatically when the FSM transitions to the `learn` state!

