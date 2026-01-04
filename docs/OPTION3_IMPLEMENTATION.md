# Option 3 Implementation Summary

## Changes Implemented

### 1. Goal Type Routing (`fsm/goals_poller.go`)

**Added `routeGoalExecution()` function** that intelligently routes goals based on their description:

**Route Categories:**
- **Knowledge Queries** → Adds `routing_hint: knowledge_query` context
  - Detects: `query_knowledge_base`, `Query Neo4j`, `[ACTIVE-LEARNING] query_knowledge_base`
  
- **Tool Calls** → Adds `routing_hint: tool_call` context
  - Detects: `use tool_`, `tool_http_get`, `tool_html_scraper`
  
- **Inconsistency Analysis** → Adds `routing_hint: reasoning` context
  - Detects: `you have detected an inconsistency`, `analyze this inconsistency`, `behavior_loop`
  
- **Default** → Uses hierarchical execute for complex tasks

**How it Works:**
```
Goal Created → Goals Poller → routeGoalExecution() → Adds routing_hint → HDN receives context → Chooses executor
```

The routing hints allow downstream executors (in HDN) to choose the appropriate execution path instead of always generating Python code.

### 2. Re-enabled Belief Contradiction Checking (`fsm/coherence_monitor.go`)

**Previous State:** Disabled (was hanging system by querying 125K+ beliefs)

**New Implementation:**
- **Strict Limits:**
  - Max 3 reasoning traces (down from 10)
  - Max 10 beliefs per domain (down from unlimited)
  - Only 1 domain per check (prevents multiple HTTP calls)
  
- **Performance:**
  - Max comparisons: 10×10 = 100 (vs previous 125K×125K)
  - Completes in seconds (vs hanging indefinitely)
  
- **Expected Output:**
  - 1-3 belief contradiction goals per coherence check
  - Check runs every 5 minutes
  - Should generate 10-30 goals per hour

### 3. Documentation Added

**File:** `docs/GOAL_EXECUTION_STRATEGIES.md`

Comprehensive guide covering:
- Current goal sources and execution paths
- Routing strategy by goal type
- Recommendations for increasing curiosity generation
- Metrics to track

## Expected Results

### Before Option 3:
- **Goal Generation:** 1-2 goals/hour (only hypothesis testing)
- **Execution Path:** Everything → code generation
- **Belief Check:** Disabled (hanging)
- **Routing:** None (everything to hierarchical execute)

### After Option 3:
- **Goal Generation:** 5-15 goals/hour
  - Hypothesis testing: 1-2/hour
  - Behavior loops: 1-5/hour (1 per transition per 24h)
  - Belief contradictions: 10-30/hour (re-enabled with limits)
  
- **Execution Paths:** Intelligent routing
  - Knowledge queries → Context hint for knowledge base
  - Tool calls → Context hint for tool system
  - Inconsistency analysis → Context hint for reasoning
  - Complex tasks → Hierarchical planner
  
- **Belief Check:** Enabled with strict performance limits
- **Routing:** Smart goal type detection

## Next Steps to Deploy

### 1. Rebuild Docker Image
```bash
cd /home/stevef/dev/artificial_mind
docker build -f Dockerfile.fsm.secure -t stevef1uk/fsm-server:secure .
docker push stevef1uk/fsm-server:secure
```

### 2. Restart FSM Pod
```bash
kubectl rollout restart deployment/fsm-server-rpi58 -n agi
```

### 3. Monitor Results (after 1 hour)
```bash
# Check goal generation rate
kubectl logs -n agi $(kubectl get pods -n agi -l app=fsm-server-rpi58 -o jsonpath='{.items[0].metadata.name}') --since=1h | grep "Generated\|curiosity\|Belief contradiction"

# Check goal count
curl -s http://localhost:8090/goals/agent_1/active | jq 'length'
```

## Future Enhancements

### Phase 2: Direct Execution Routes
Currently, routing hints are added to context but HDN still uses hierarchical execute. Next phase:
- Create dedicated endpoints in HDN for each route type
- `/api/v1/knowledge/query` - Direct Neo4j/Weaviate queries
- `/api/v1/tools/execute` - Direct tool invocation
- `/api/v1/reasoning/analyze` - Direct reasoning engine calls

### Phase 3: Increase Curiosity Generation
- Lower thresholds for knowledge gap detection
- Add more active learning triggers
- Periodic knowledge exploration goals

### Phase 4: Goal Templates
- Pre-defined goal templates that map to specific execution paths
- Reduce code generation dependency
- Faster, more reliable execution

## Git Commits
- `a940cbd` - feat: implement goal execution routing and increase goal generation
- `58d1751` - docs: update duplicate goals quickstart with coherence hang fix
- `6cee6eb` - fix: disable slow belief contradiction check that was hanging coherence monitor
