# Kubernetes Troubleshooting Guide

## Current Status Summary

Your AGI system is running on Kubernetes, but goals are stuck and not being processed. Here's what's happening:

### Issues Found

1. **Goals Are Stuck** ⚠️
   - 14 active goals exist in Goal Manager
   - 71 goals are marked as "triggered" in Redis
   - All active goals are already marked as triggered, so FSM skips them
   - FSM goal poller runs every 2 seconds but silently skips already-triggered goals

2. **Neo4j Write Errors** ⚠️
   - HDN is getting "Writing in read access mode not allowed" errors
   - This happens when trying to write to Neo4j using a read-only session
   - May be preventing some knowledge storage operations

3. **No Goal Processing Activity** ⚠️
   - FSM goal poller is running but not logging activity (because it's skipping goals)
   - Goals are being created but not executed

### System Health

✅ **Working:**
- All pods are running and healthy
- Services are accessible
- NATS connectivity is good (5 active connections)
- Tools are registered (22 tools)
- Goal Manager is returning active goals
- FSM can reach Goal Manager

⚠️ **Needs Attention:**
- Goals stuck in "triggered" state
- Neo4j write operations failing
- No goal execution happening

## Solutions

### Quick Fix: Clear Stuck Goals

Run the fix script to clear triggered flags for active goals:

```bash
cd k3s
./fix-stuck-goals.sh
```

This will:
1. Fetch all active goals from Goal Manager
2. Check which ones are marked as triggered
3. Clear the triggered flag so FSM can retry them

### Manual Fix: Clear Triggered Goals

If you prefer to do it manually:

```bash
# Get list of active goal IDs
kubectl exec -n agi deployment/fsm-server-rpi58 -- \
  wget -qO- http://goal-manager.agi.svc.cluster.local:8090/goals/agent_1/active | \
  jq -r '.[].id' > /tmp/active_goals.txt

# Clear triggered flag for each active goal
while read goal_id; do
  kubectl exec -n agi deployment/redis -- \
    redis-cli SREM "fsm:agent_1:goals:triggered" "$goal_id"
done < /tmp/active_goals.txt
```

### Check Goal Poller Activity

Monitor FSM logs to see if goals are being processed:

```bash
# Watch FSM logs for goal activity
kubectl -n agi logs -f deployment/fsm-server-rpi58 | grep -i "\[FSM\]\[Goals\]"
```

You should see:
- `[FSM][Goals] triggered goal <id>` when goals are successfully triggered
- `[FSM][Goals] fetch active goals error` if there are connectivity issues
- `[FSM][Goals] execute failed` if HDN execution fails

### Check Neo4j Issues

Investigate Neo4j write errors:

```bash
# Check Neo4j logs
kubectl -n agi logs deployment/neo4j --tail=100

# Test Neo4j connectivity from HDN
kubectl -n agi exec deployment/hdn-server-rpi58 -- \
  wget -qO- http://neo4j.agi.svc.cluster.local:7474

# Check Neo4j configuration
kubectl -n agi exec deployment/neo4j -- \
  cypher-shell -u neo4j -p test1234 "CALL dbms.listConfig() YIELD name, value WHERE name CONTAINS 'read' RETURN name, value;"
```

### Restart Services (If Needed)

If clearing goals doesn't help, restart FSM:

```bash
kubectl rollout restart deployment/fsm-server-rpi58 -n agi
```

## Monitoring

### Run Diagnostics

Use the diagnostic script to check system health:

```bash
cd k3s
./diagnose-system.sh
```

### Check Active Goals

```bash
# Count active goals
kubectl exec -n agi deployment/fsm-server-rpi58 -- \
  wget -qO- http://goal-manager.agi.svc.cluster.local:8090/goals/agent_1/active | \
  jq '. | length'

# List active goals
kubectl exec -n agi deployment/fsm-server-rpi58 -- \
  wget -qO- http://goal-manager.agi.svc.cluster.local:8090/goals/agent_1/active | \
  jq -r '.[] | "\(.id): \(.description)"'
```

### Check Goal Execution Status

```bash
# Check how many goals are marked as triggered
kubectl exec -n agi deployment/redis -- \
  redis-cli SCARD "fsm:agent_1:goals:triggered"

# List triggered goals
kubectl exec -n agi deployment/redis -- \
  redis-cli SMEMBERS "fsm:agent_1:goals:triggered" | head -20
```

## Root Cause Analysis

### Why Goals Get Stuck

1. **Goal is triggered but execution fails silently**
   - FSM marks goal as triggered before HDN execution completes
   - If HDN fails, goal remains marked as triggered but never completes
   - FSM won't retry because it thinks it already triggered it

2. **Goal execution times out**
   - HDN execution takes too long or hangs
   - Goal is marked as triggered but execution never finishes
   - Goal remains active but stuck

3. **FSM restart clears in-memory state but not Redis**
   - When FSM restarts, it loses track of which goals it was processing
   - Redis still has goals marked as triggered
   - FSM skips them on restart

### Prevention

Consider adding:
1. **Goal execution tracking**: Track which goals are currently executing
2. **Timeout handling**: Automatically clear triggered flag after timeout
3. **Execution status**: Update goal status based on HDN execution result
4. **Retry logic**: Retry failed goals after a delay

## Next Steps

1. ✅ Run `./fix-stuck-goals.sh` to clear stuck goals
2. ✅ Monitor FSM logs to see if goals start processing
3. ✅ Check HDN logs for execution errors
4. ✅ Investigate Neo4j write errors if they persist
5. ✅ Consider implementing better goal tracking/retry logic

