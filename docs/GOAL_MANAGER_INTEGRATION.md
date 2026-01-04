# Goal Manager Integration

**Version:** Jan 2026  
**Status:** Fully Operational  

## Overview

The **Unified Goal Management System** unifies all FSM autonomy activities (dream mode, hypothesis testing, coherence monitoring, active learning, and general reasoning) through a central Goal Manager service. This enables centralized workflow creation, unified UI visibility, and coordinated goal execution.

## Architecture

### Goal Flow Pipeline

```
FSM Autonomy Components
    ↓
Goal Manager Client
    ↓
Goal Manager Service (Redis-backed)
    ↓
FSM Goals Poller
    ↓
HDN Workflow Engine
    ↓
Workflow Execution & Artifacts
    ↓
Monitor UI Display
```

### Key Components

#### 1. **Goal Manager Client** (`fsm/goal_manager_integration.go`)
- **Purpose:** Utility for posting goals from FSM to Goal Manager
- **Methods:**
  - `PostCuriosityGoal(goal CuriosityGoal, source string)` - POSTs goal to Goal Manager with proper formatting
  - `IsGoalAlreadyInManager(ctx, goalID)` - Checks for duplicates using Redis

#### 2. **FSM Engine Integration** (`fsm/engine.go`)
- **Initialization:** Goal Manager client created and passed to all FSM modules
- **Field:** `goalManager *GoalManagerClient` on FSMEngine struct
- **Initialization Priority:** Goal Manager created BEFORE CoherenceMonitor to enable goal posting

#### 3. **Goal-Generating Modules**

All of the following modules now post goals to Goal Manager:

##### Autonomy Module (`fsm/autonomy.go`)
- **Function:** `TriggerAutonomyCycle()`
- **Goals Posted:** All reasoning engine outputs (gap-filling, contradictions, exploration, active-learning)
- **Source Tag:** `"autonomy_generated"`
- **Logic:** Lines 256-261 - Posts deduplicated new goals when added to Redis

##### Dream Mode (`fsm/dream_mode.go`)
- **Function:** `GenerateDreamGoals()`
- **Goals Posted:** Creative exploration goals
- **Source Tag:** `"dream_mode"`
- **Format:** Goals with domain, targets, confidence scores

##### Hypothesis Testing (`fsm/engine.go`)
- **Function:** `createHypothesisTestingGoals()`
- **Goals Posted:** 
  - Intervention testing goals (lines 3585)
  - Standard hypothesis testing goals (lines 3689)
- **Source Tag:** `"hypothesis_testing"`
- **Priority:** 8-10 (high priority for actionable tests)

##### Coherence Monitor (`fsm/coherence_monitor.go`)
- **Function:** `generateResolutionTaskForInconsistency()`
- **Goals Posted:** System inconsistency resolution goals
- **Source Tag:** `"coherence_monitor"`
- **Domain:** `"system_coherence"`
- **Inconsistency Types:** behavior_loop, belief_contradiction, policy_conflict, goal_drift, strategy_conflict

##### Workflow Execution (`fsm/workflows.go`)
- **Function:** `createWorkflowExecutionGoals()`
- **Goals Posted:** Workflow execution goals
- **Source Tag:** `"workflow_execution"`
- **Priority:** 7 (high priority for discovered workflows)

## Goal API Format

All goals posted to Goal Manager follow this standard format:

```json
{
  "id": "goal_<unique_id>",
  "agent_id": "agent_1",
  "description": "Goal description text",
  "priority": "8",          // String format (converted from int)
  "status": "pending",      // pending, active, completed, failed
  "confidence": 0.75,       // Numerical confidence value
  "context": {
    "domain": "General",
    "source": "autonomy_generated",
    "targets": ["target1", "target2"]
  }
}
```

### Important Notes
- **Priority:** Must be STRING format (converted from int with `fmt.Sprintf("%d", priority)`)
- **Confidence:** Uses `goal.Value` field from CuriosityGoal
- **Context:** Includes domain, source, and targets for routing/tracking

## Goal Manager Service

### Redis Storage
- **Active Goals Set:** `goals:agent_1:active`
- **Goal Storage:** One entry per goal ID in the active set
- **Indexing:** By goal ID, supports set membership queries

### HTTP API Endpoints
- **POST /goal** - Add/update a goal
- **GET /goals** - Fetch all active goals (supports limit)
- **GET /goal/:id** - Get specific goal details
- **PATCH /goal/:id/status** - Update goal status

## Workflow Creation Flow

1. **FSM generates goal** (autonomy, dream, hypothesis testing, coherence, workflows)
2. **Posts to Goal Manager** via `GoalManagerClient.PostCuriosityGoal()`
3. **Goal Manager stores in Redis** set `goals:agent_1:active`
4. **FSM Goals Poller fetches** goals from Goal Manager every 2-4 seconds
5. **Selects goal for execution** (limit: 1 per cycle to prevent overload)
6. **Routes to HDN** via `/api/v1/hierarchical/execute`
7. **HDN creates workflow** in `active_workflows` set
8. **Workflow executes** with timeout enforcement (10 minutes)
9. **Results stored** with artifacts if applicable
10. **Goal marked complete** in Goal Manager

## Goal Routing and Execution

### Goals Poller (`fsm/goals_poller.go`)

**Key Features:**
- Fetches from Goal Manager every 2-4 seconds
- Limits to 1 goal execution per cycle (prevents HDN overload)
- Tracks "already triggered" goals to avoid duplicates
- Implements exponential backoff on HDN 429 errors

**Flow:**
```
Fetch 46 goals from Goal Manager
    ↓
Filter out already-triggered goals
    ↓
Execute first non-triggered goal
    ↓
Mark as triggered (30-min TTL)
    ↓
Watch for completion/timeout
    ↓
Clear triggered flag when done
```

### Concurrency Control

**HDN Workflow Limits:**
- **Max 2 active workflows** (non-UI requests) 
- **Max 4 active workflows** (UI requests)
- Prevents system overload while ensuring responsiveness

**Timeout Enforcement:**
- **Execution Timeout:** 10 minutes per workflow
- **Auto-Fail Logic:** HDN cleanup automatically marks stuck workflows as failed
- **Implementation:** `cleanupStaleActiveWorkflows()` in `hdn/api.go:4059-4092`

## Workflow Status Tracking

### Active Workflows Set (`active_workflows`)
```redis
SMEMBERS active_workflows
→ ["intelligent_1767531977107460423", "intelligent_1767531090108825094"]
```

### Cleanup and Auto-Failure

**Trigger:** Periodic cleanup called during:
- Goal execution acceptance (line 3324)
- Workflow status retrieval (line 2291)
- File retrieval endpoints (line 4102)

**Logic:**
1. Iterate all workflows in `active_workflows` set
2. For each "running" workflow:
   - Check `started_at` timestamp
   - If running > 10 minutes: mark as failed, set error message, remove from active set
3. For completed/failed workflows: remove from active set

**Error Message:** "Workflow timeout: exceeded 10 minute execution limit"

## Data Flow Integration

### Redis Keys Used

**FSM Autonomy:**
- `reasoning:curiosity_goals:{domain}` - Local goal storage (Redis List)
- `fsm:{agent_id}:goals:triggered` - Triggered goal tracking (Redis Set, 30-min TTL)

**Goal Manager:**
- `goals:agent_1:active` - All active goals (Redis Set)
- `goals:agent_1:priorities` - Priority index (Redis ZSet)

**HDN Workflows:**
- `active_workflows` - Currently executing workflows (Redis Set)
- `workflow:{id}` - Workflow details (Redis String, JSON)
- `workflow_project:{id}` - Project mapping (Redis String)

### Error Handling

**Goal Posting Failures:**
- Returns error from HTTP POST
- Logs with `[GOAL-MGR]` tag
- Common error: 400 status for type mismatches
- Current implementation: Silently fails (`_ =` pattern) but logs warnings

**Goal Execution Failures:**
- HDN returns 429 (Too Many Requests) when at concurrency limit
- FSM backs off polling interval to 4 seconds
- Goal remains in Goal Manager for retry
- Eventually times out and marked as failed

## Monitoring and Debugging

### Log Tags
- **Goal Posting:** `[GOAL-MGR]` - Shows successful posts and errors
- **Goal Execution:** `[FSM][Goals]` - Shows goal polling and execution attempts
- **Workflow Cleanup:** `[API]` - Shows cleanup activities and auto-failures

### Key Metrics
- **Goals in Goal Manager:** `SCARD goals:agent_1:active`
- **Active Workflows:** `SCARD active_workflows`
- **Triggered Goals:** `SCARD fsm:agent_1:goals:triggered`

### Example Commands

```bash
# Check active goals
redis-cli SCARD goals:agent_1:active

# List active workflows
redis-cli SMEMBERS active_workflows

# Check specific goal
redis-cli SMEMBERS goals:agent_1:active | grep dream_

# Check workflow status
redis-cli GET "workflow:intelligent_<timestamp>"
```

## Implementation Details

### Source Tag Convention

Each module uses a source tag for workflow tracking:

| Module | Source Tag | Use Case |
|--------|-----------|----------|
| Autonomy | `autonomy_generated` | Reasoning engine outputs |
| Dream Mode | `dream_mode` | Creative exploration |
| Hypothesis Testing | `hypothesis_testing` | Test generation |
| Coherence Monitor | `coherence_monitor` | Inconsistency resolution |
| Workflow Execution | `workflow_execution` | Discovered workflow execution |

### Priority Levels

- **10:** Highest priority (intervention testing for causally testable hypotheses)
- **8-9:** High priority (hypothesis testing, active learning)
- **6:** Medium priority (dream mode)
- **1-5:** Lower priority (general exploration)

### Confidence/Value Scoring

- **0.0-1.0:** Scaled confidence score
- **Active Learning:** Uncertainty reduction potential
- **Dream:** Fixed 0.7 (exploratory confidence)
- **Hypotheses:** Based on evidence strength

## Testing the Integration

### 1. Verify Goals Posted to Goal Manager

```bash
# Check that goals are in Goal Manager set
kubectl exec -n agi redis-6f67f4f5db-8qc94 -- redis-cli SMEMBERS "goals:agent_1:active" | wc -l

# Filter for specific goal types
kubectl exec -n agi redis-6f67f4f5db-8qc94 -- redis-cli SMEMBERS "goals:agent_1:active" | grep -E "dream_|hyp_test|active_learning"
```

### 2. Verify Workflows Created

```bash
# Check workflows
kubectl exec -n agi redis-6f67f4f5db-8qc94 -- redis-cli SCARD "active_workflows"

# List recent workflows
kubectl exec -n agi redis-6f67f4f5db-8qc94 -- redis-cli KEYS "workflow:intelligent_176753*" | sort | tail -5
```

### 3. Check Logs

```bash
# FSM goal posting
kubectl logs fsm-server-<pod> -n agi | grep "GOAL-MGR"

# Goal execution
kubectl logs fsm-server-<pod> -n agi | grep "Executing goal"

# Workflow completion
kubectl logs hdn-server-<pod> -n agi | grep "Intelligent execution completed"
```

## Performance Characteristics

### Throughput
- **Goal Generation:** Depends on autonomy cycle (300s interval for general, varies by module)
- **Goal Execution:** 1 per 2-4 seconds (configurable backoff)
- **Workflow Completion:** 20-40 seconds (for intelligent execution)

### Latency
- **Post to Goal Manager:** <100ms (local HTTP)
- **Goal Fetch:** <100ms (Redis)
- **Workflow Creation:** <1s (HDN planning phase)
- **Execution:** 10-60s (depends on LLM calls)

### Storage
- **Per Goal:** ~200-500 bytes (metadata)
- **Per Workflow:** ~5-50KB (depending on artifacts)
- **Memory Cost:** Negligible for 100-1000 goals

## Future Enhancements

1. **Goal Prioritization:** Intelligent goal ranking based on impact potential
2. **Goal Dependencies:** Support for goal prerequisites and ordering
3. **Goal Batching:** Execute multiple non-conflicting goals in parallel
4. **Adaptive Concurrency:** Dynamically adjust workflow limit based on system load
5. **Goal Versioning:** Track goal evolution and hypothesis refinement
6. **Feedback Loop:** Post-execution learning updates goal generation heuristics

## Related Documentation

- [Autonomy System](docs/AUTONOMY_SYSTEM.md)
- [Active Learning Loops](docs/ACTIVE_LEARNING_LOOPS.md)
- [Coherence Monitor](docs/CROSS_SYSTEM_CONSISTENCY_CHECKING.md)
- [HDN Architecture](docs/hdn_architecture.md)
- [Workflow Orchestration](docs/WORKFLOW_ORCHESTRATION.md)
