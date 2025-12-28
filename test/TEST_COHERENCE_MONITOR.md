# Testing the Coherence Monitor

## Quick Start

Run the test script:
```bash
./test/test_coherence_monitor.sh
```

## Manual Testing Steps

### 1. Verify the Coherence Monitor is Running

The coherence monitor starts automatically with the FSM server. Check the logs:

**Local:**
```bash
# Check FSM server logs for:
grep -i "coherence" <fsm-server-logs>
# Look for: "🔍 [Coherence] Coherence monitoring loop started"
```

**K8s:**
```bash
kubectl logs -n agi -l app=fsm | grep -i coherence
```

### 2. Wait for Automatic Check

The coherence monitor runs **every 5 minutes** automatically. You can:
- Wait 5 minutes and check Redis
- Or create test scenarios and wait

### 3. Create Test Scenarios

#### Scenario A: Conflicting Goals (Policy Conflict)

Create two conflicting goals via Goal Manager API:

```bash
# Goal 1: Increase something
curl -X POST http://localhost:8084/goals \
  -H "Content-Type: application/json" \
  -d '{
    "agent_id": "agent_1",
    "description": "Increase API response time",
    "priority": "high",
    "status": "active"
  }'

# Goal 2: Decrease the same thing (CONFLICT!)
curl -X POST http://localhost:8084/goals \
  -H "Content-Type: application/json" \
  -d '{
    "agent_id": "agent_1",
    "description": "Decrease API response time",
    "priority": "high",
    "status": "active"
  }'
```

**Expected Result:** After 5 minutes, the coherence monitor should detect a `policy_conflict` inconsistency.

#### Scenario B: Goal Drift

Create a goal that's been "active" for 25+ hours:

```bash
# Using Redis directly
redis-cli SET "goal:test_stale_goal" '{
  "id": "test_stale_goal",
  "agent_id": "agent_1",
  "description": "Test stale goal",
  "status": "active",
  "created_at": "2024-01-01T00:00:00Z",
  "updated_at": "2024-01-01T00:00:00Z"
}'

redis-cli SADD "goals:agent_1:active" "test_stale_goal"
```

**Expected Result:** After 5 minutes, the coherence monitor should detect a `goal_drift` inconsistency.

#### Scenario C: Behavior Loop

Create repetitive state transitions in the activity log:

```bash
# Create 6+ identical state transitions
for i in {1..6}; do
  redis-cli LPUSH "fsm:agent_1:activity_log" '{
    "timestamp": "'$(date -u +%Y-%m-%dT%H:%M:%SZ)'",
    "message": "State transition",
    "state": "reason",
    "category": "state_change"
  }'
done
```

**Expected Result:** After 5 minutes, the coherence monitor should detect a `behavior_loop` inconsistency.

#### Scenario D: Belief Contradiction

This requires actual beliefs in the knowledge base. The monitor checks reasoning traces, so you need to:

1. Send input to FSM to trigger learning
2. Wait for beliefs to be created
3. Create contradictory beliefs (this is harder to do manually)

```bash
# Trigger learning
curl -X POST http://localhost:8083/input \
  -H "Content-Type: application/json" \
  -d '{
    "input": "Neural networks always require large datasets",
    "session_id": "test_belief_1"
  }'

# Later, create a contradictory belief
curl -X POST http://localhost:8083/input \
  -H "Content-Type: application/json" \
  -d '{
    "input": "Neural networks can work with small datasets",
    "session_id": "test_belief_2"
  }'
```

### 4. Check for Detected Inconsistencies

After waiting 5 minutes (or triggering manually), check Redis:

```bash
# Count inconsistencies
redis-cli LLEN coherence:inconsistencies:agent_1

# View recent inconsistencies
redis-cli LRANGE coherence:inconsistencies:agent_1 0 4 | jq

# View by type
redis-cli LRANGE coherence:inconsistencies:agent_1:policy_conflict 0 4 | jq
redis-cli LRANGE coherence:inconsistencies:agent_1:goal_drift 0 4 | jq
redis-cli LRANGE coherence:inconsistencies:agent_1:behavior_loop 0 4 | jq
```

### 5. Check for Self-Reflection Tasks

```bash
# View reflection tasks
redis-cli LRANGE coherence:reflection_tasks:agent_1 0 4 | jq
```

### 6. Check for Generated Curiosity Goals

The coherence monitor creates curiosity goals for the reasoning engine:

```bash
# View coherence-related curiosity goals
redis-cli LRANGE reasoning:curiosity_goals:system_coherence 0 4 | jq
```

## Expected Output

### Inconsistency Example

```json
{
  "id": "policy_conflict_1234567890",
  "type": "policy_conflict",
  "severity": "medium",
  "description": "Conflicting goals: 'Increase API response time' vs 'Decrease API response time'",
  "details": {
    "goal1_id": "g_123",
    "goal1": "Increase API response time",
    "goal1_priority": "high",
    "goal2_id": "g_456",
    "goal2": "Decrease API response time",
    "goal2_priority": "high"
  },
  "detected_at": "2024-01-15T10:30:00Z",
  "resolved": false
}
```

### Self-Reflection Task Example

```json
{
  "id": "reflection_1234567890",
  "inconsistency_id": "policy_conflict_1234567890",
  "description": "Resolve inconsistency: policy_conflict - Conflicting goals: ...",
  "priority": 6,
  "status": "pending",
  "created_at": "2024-01-15T10:30:00Z",
  "metadata": {
    "inconsistency_type": "policy_conflict",
    "severity": "medium",
    "details": {...}
  }
}
```

## Troubleshooting

### No Inconsistencies Detected

1. **Check if monitor is running:**
   ```bash
   # Look for this in FSM logs:
   grep "Coherence monitoring loop started" <fsm-logs>
   ```

2. **Check timing:**
   - The monitor runs every 5 minutes
   - Wait at least 5 minutes after creating test scenarios

3. **Check Redis connectivity:**
   - The monitor needs Redis access
   - Verify FSM can connect to Redis

### Monitor Not Running

1. **Check FSM server is running:**
   ```bash
   curl http://localhost:8083/status
   ```

2. **Check FSM logs for errors:**
   ```bash
   # Look for coherence-related errors
   grep -i "coherence.*error" <fsm-logs>
   ```

3. **Verify code is up to date:**
   - Make sure you're running the latest code with coherence monitor
   - Check that `fsm/coherence_monitor.go` exists

## Advanced Testing

### Force Immediate Check (Development)

To test immediately without waiting 5 minutes, you can temporarily modify the interval in `fsm/engine.go`:

```go
// In coherenceMonitoringLoop(), change:
interval := 5 * time.Minute
// To:
interval := 30 * time.Second  // For testing
```

Then restart the FSM server.

### Monitor in Real-Time

Watch FSM logs for coherence activity:

```bash
# Local
tail -f <fsm-log-file> | grep -i coherence

# K8s
kubectl logs -n agi -l app=fsm -f | grep -i coherence
```

## Success Criteria

✅ Coherence monitor runs every 5 minutes  
✅ Inconsistencies are detected and stored in Redis  
✅ Self-reflection tasks are generated  
✅ Curiosity goals are created for the reasoning engine  
✅ FSM logs show [Coherence] messages  

