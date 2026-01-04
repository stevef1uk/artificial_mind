# System Architecture - January 2026 Update

## Executive Summary

The Artificial Mind system has been enhanced with a **Unified Goal Management System** that centralizes all FSM autonomy activities (dream mode, hypothesis testing, coherence monitoring, active learning) through a single Goal Manager service. This enables:

- **Unified workflow creation** - All autonomy-generated goals automatically create workflows
- **Centralized UI visibility** - All goal types visible in Monitor UI
- **Coordinated goal execution** - Single goals poller manages all goal types
- **Scalable architecture** - Extensible pattern for adding new goal-generating modules

## Architecture Diagram

```
┌─────────────────────────────────────────────────────────────────┐
│                     FSM AUTONOMY CYCLE (300s)                   │
├─────────────────────────────────────────────────────────────────┤
│                                                                   │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐           │
│  │ Dream Mode   │  │  Hypothesis  │  │  Coherence   │           │
│  │ (5m cycle)   │  │  Testing     │  │  Monitor     │           │
│  │              │  │  (autonomy)  │  │  (5m cycle)  │           │
│  └──────┬───────┘  └──────┬───────┘  └──────┬───────┘           │
│         │                 │                 │                   │
│         ├─────────────────┼─────────────────┤                   │
│         │                 │                 │                   │
│         ▼                 ▼                 ▼                   │
│  ┌────────────────────────────────────────┐                     │
│  │   Reasoning Engine (Autonomy Module)   │                     │
│  │  - Gap Filling Goals                   │                     │
│  │  - Contradiction Resolution            │                     │
│  │  - Exploration Goals                   │                     │
│  │  - Active Learning Plans               │                     │
│  └────────────────┬───────────────────────┘                     │
│                   │                                              │
└───────────────────┼──────────────────────────────────────────────┘
                    │
                    ▼
        ┌──────────────────────────┐
        │ Goal Manager Client      │
        │ (goal_manager_integration│
        │     .go)                 │
        │                          │
        │ - PostCuriosityGoal()    │
        │ - Deduplicate           │
        │ - Format standardization │
        └──────────────┬───────────┘
                       │
                       ▼
        ┌──────────────────────────┐
        │   Goal Manager Service   │
        │   (Redis-backed)         │
        │                          │
        │ - goals:agent_1:active   │
        │ - HTTP API endpoints     │
        │ - Goal persistence       │
        └──────────────┬───────────┘
                       │
                       ▼
        ┌──────────────────────────┐
        │  FSM Goals Poller        │
        │  (2-4s cycle)            │
        │                          │
        │ - Fetch from Goal Manager│
        │ - Select 1 goal/cycle    │
        │ - Track triggered goals  │
        │ - Backoff on HDN errors  │
        └──────────────┬───────────┘
                       │
                       ▼
        ┌──────────────────────────┐
        │   HDN Workflow Engine    │
        │                          │
        │ - Concurrent execution   │
        │ - Max 2 workflows (non-UI│
        │ - Max 4 workflows (UI)   │
        │ - 10-min timeout         │
        │ - Auto-failure on timeout│
        └──────────────┬───────────┘
                       │
        ┌──────────────┴──────────────┐
        │                             │
        ▼                             ▼
    Intelligent            Hierarchical
    Execution              Workflows
    (code/tools)           (multi-step)
        │                             │
        ▼                             ▼
    Artifacts               Hierarchical
    + Results               Results
        │                             │
        └──────────────┬──────────────┘
                       │
                       ▼
        ┌──────────────────────────┐
        │   Monitor UI             │
        │                          │
        │ - Goal visibility        │
        │ - Workflow status        │
        │ - Artifacts display      │
        │ - Goal metrics           │
        └──────────────────────────┘
```

## Component Integration

### 1. Goal Generation (FSM Autonomy)

**Components:**
- Autonomy Module - Main orchestrator (300s cycle)
- Dream Mode - Creative exploration (5m cycle)
- Reasoning Engine - Goal generation engine
- Coherence Monitor - Inconsistency resolution (5m cycle)
- Hypothesis Testing - Test generation (within autonomy)
- Active Learning - Data acquisition planning (within autonomy)

**Output:** CuriosityGoal objects

### 2. Goal Posting (Goal Manager Client)

**Component:** `fsm/goal_manager_integration.go`

**Interface:**
```go
type GoalManagerClient struct {
    baseURL string          // Goal Manager service URL
    client  *http.Client    // HTTP client
    redis   *redis.Client   // For duplicate checking
}

func (gmc *GoalManagerClient) PostCuriosityGoal(goal CuriosityGoal, source string) error
```

**Features:**
- Converts goal priority from int to string format
- Includes source tracking (autonomy_generated, dream_mode, etc.)
- Posts to `/goal` endpoint
- Returns error for 4xx/5xx responses
- Logs success/failure

### 3. Goal Management (Goal Manager Service)

**Storage:** Redis Set `goals:agent_1:active`

**HTTP Endpoints:**
- `POST /goal` - Add/update goal
- `GET /goals` - List all goals
- `GET /goal/:id` - Get specific goal
- `PATCH /goal/:id/status` - Update status

**Goal Lifecycle:**
1. Created as "pending" in Goal Manager
2. Selected by Goals Poller
3. Executed by HDN
4. Marked "completed" or "failed"
5. Eventually removed from active set

### 4. Goal Execution (FSM Goals Poller)

**Component:** `fsm/goals_poller.go`

**Key Features:**
- **Polling Interval:** 2 seconds normal, 4+ seconds backoff
- **Concurrency Control:** Max 1 goal triggered per cycle
- **Triggered Tracking:** 30-minute TTL on `fsm:agent_1:goals:triggered` set
- **Error Handling:** Exponential backoff on HDN 429 (overload)
- **Workflow Watching:** Monitors completion, clears triggered flag when done

**Flow:**
```
Fetch all goals from Goal Manager
    ↓
Filter out already-triggered goals
    ↓
For each goal:
    ├─ POST to HDN /api/v1/hierarchical/execute
    ├─ If success: Mark as triggered, watch workflow
    ├─ If 429 (overload): Backoff, retry next cycle
    └─ If error: Log, continue to next goal
```

### 5. Workflow Execution (HDN)

**Component:** `hdn/api.go` - Hierarchical task execution

**Concurrency Control:**
- Non-UI requests: max 2 concurrent workflows
- UI requests: max 4 concurrent workflows
- Tracked in Redis set `active_workflows`

**Timeout Enforcement:**
- Execution context: 10-minute timeout per workflow
- Auto-failure: Workflows > 10 min marked as failed
- Cleanup: Periodic `cleanupStaleActiveWorkflows()` runs

**Execution Types:**
- **Intelligent Execution:** Code generation + tool execution
- **Hierarchical Workflows:** Multi-step planner-based execution

### 6. Monitoring & Visibility (Monitor UI)

**Data Source:** Redis + Goal Manager API

**Displays:**
- Active goals with metadata
- Workflow status and progress
- Execution artifacts
- Timeline of completions
- Performance metrics

## Data Flow Example

### Scenario: Dream Mode Goal → Workflow → Artifact

```
Time  Event                              System          Storage
────────────────────────────────────────────────────────────────────

0s    Dream cycle triggers              FSM             
      Generates exploration goal         (5m)

5ms   Goal posted to Goal Manager       FSM             goals:agent_1:active
      Format: {id, desc, domain}                        ← dream_1767531143...

2s    Goals Poller fetches              FSM Goals       
      43 goals from Goal Manager        Poller

4s    Dream goal selected for exec      FSM Goals       fsm:agent_1:goals:
                                        Poller          triggered
                                                        ← dream_1767531143...

6s    POST /hierarchical/execute        FSM to HDN      active_workflows
      to HDN with goal details                          ← intelligent_176753...

8s    HDN creates workflow              HDN             workflow:intelligent_...
      Plans execution steps

10s   Step 1: LLM thinking              HDN             (same workflow)
      Step 2: Tool execution            Workflow

40s   Workflow completes                HDN             (remove from active,
      Artifacts generated                               set status:completed)

42s   Goals Poller marks goal done      FSM             (clear from triggered)
      Removes from triggered set

────────────────────────────────────────────────────────────────────
Total latency: ~42 seconds from generation to completion
Result: Artifacts visible in Monitor UI
```

## Module Responsibilities

| Module | Responsibility | Frequency | Output |
|--------|---|---|---|
| **Autonomy** | Goal generation orchestration | 300s | Curiosity goals |
| **Dream Mode** | Creative exploration | 5m | Dream goals |
| **Reasoning Engine** | Knowledge-based goals | 300s | Gap-filling, exploration |
| **Coherence Monitor** | Inconsistency detection | 5m | Resolution goals |
| **Goal Manager Client** | Goal posting utility | On-demand | HTTP posts |
| **Goal Manager Service** | Goal storage & API | Persistent | Redis-backed API |
| **Goals Poller** | Goal selection & routing | 2-4s | HDN requests |
| **HDN** | Workflow creation & execution | On-demand | Workflows + artifacts |
| **Monitor UI** | Visualization | Real-time | Web interface |

## Performance Characteristics

### Throughput
- **Goal Generation:** ~5-10 goals/autonomy cycle (300s)
- **Goal Execution:** 1 goal/2-4 seconds = 15-30 goals/min
- **Workflow Completion:** 20-60 seconds per workflow

### Latency
- **Post to Goal Manager:** <100ms
- **Goal Manager fetch:** <100ms
- **Workflow creation:** <1s
- **Total (generation to completion):** 30-60s

### Storage
- **Per Goal:** 200-500 bytes
- **Per Workflow:** 5-50 KB
- **100 goals:** ~50KB
- **100 workflows:** ~2-5MB

## Scaling Characteristics

| Dimension | Current | Bottleneck | Mitigation |
|-----------|---------|-----------|-----------|
| **Concurrent Goals** | 2-4 workflows | HDN execution slots | Increase workflow limit |
| **Total Goals** | 100+ | Memory | Archive old goals |
| **Polling Rate** | 2-4s | FSM poller capacity | N/A (per-cycle limit sufficient) |
| **Goal Diversity** | 5 types | Routing logic | Extensible with new source tags |

## Reliability Features

### Failure Handling
- **Post Failures:** Logged but non-blocking
- **HDN Overload:** Automatic backoff (2s → 4s)
- **Stuck Workflows:** Auto-fail after 10 minutes
- **Triggered Tracking:** Prevents duplicate execution

### Recovery
- **Workflow Timeout:** Marked failed, slot freed for new goals
- **Goal Retry:** Automatically attempted next polling cycle
- **State Persistence:** All state in Redis (survives pod restarts)

## Configuration Points

### FSM Engine Initialization
```go
// Create Goal Manager client (before CoherenceMonitor)
goalMgrURL := "http://goal-manager:8090"
goalManager := NewGoalManagerClient(goalMgrURL, redis)

// Pass to modules that generate goals
coherenceMonitor := NewCoherenceMonitor(..., goalManager)
```

### Goals Poller Settings
- **Poll Interval:** 2s default, 4s+ on backoff
- **Max Goals/Cycle:** 1 (prevents overload)
- **Triggered TTL:** 30 minutes (prevents stale tracking)
- **Backoff Strategy:** Exponential with cap

### HDN Workflow Limits
- **Non-UI Max:** 2 concurrent workflows
- **UI Max:** 4 concurrent workflows
- **Execution Timeout:** 10 minutes
- **Cleanup Frequency:** Periodic (called on every exec decision)

## Integration Points

### With FSM Autonomy
- **Input:** CuriosityGoal objects from all autonomy modules
- **Interface:** `PostCuriosityGoal(goal, source)`
- **Output:** Workflow execution requests to HDN

### With Goal Manager Service
- **Input:** HTTP requests with goal JSON
- **Storage:** Redis Set `goals:agent_1:active`
- **Output:** Goal listing via HTTP GET

### With HDN
- **Input:** Goal execution requests
- **Processing:** Workflow creation, execution, artifact generation
- **Output:** Workflow status, artifacts, completion status

### With Monitor UI
- **Input:** Redis queries for goals and workflows
- **Display:** Real-time goal/workflow visualization
- **Interaction:** Goal details, artifact viewing

## Future Extensibility

### Adding New Goal-Generating Modules
1. Create module with goal generation logic
2. Create `GoalManagerClient` field in module
3. Call `PostCuriosityGoal(goal, "your_source_tag")`
4. Monitor UI automatically displays new goal types

### Adding New Goal Types
1. Extend `CuriosityGoal` struct if needed
2. Set appropriate source tag
3. Goals system automatically routes and executes
4. No changes to core infrastructure needed

### Scaling Workflow Concurrency
1. Increase `maxActiveWorkflows` in `hdn/api.go:3393`
2. Monitor system resource usage
3. Adjust based on machine capacity

## Related Systems

- **FSM Autonomy:** Generates goals
- **Reasoning Engine:** Provides reasoning-based goals
- **HDN Planner:** Executes workflows
- **Monitor UI:** Visualizes results
- **NATS:** Event distribution
- **Redis:** Persistent storage

## Version History

| Date | Version | Changes |
|------|---------|---------|
| Jan 2026 | 1.0 | Initial unified goal management system |
| Jan 2026 | 1.1 | Added coherence monitor integration |
| Jan 2026 | 1.2 | Added workflow timeout auto-failure |
| Jan 2026 | 1.3 | Added GoalManagerClient abstraction |
