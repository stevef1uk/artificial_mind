# Autonomy Troubleshooting Guide

## Problem: System Running but No New Workflows/Goals/Artifacts

If your system has been running successfully (GPU at 100%) but you're not seeing new workflows, goals, or artifacts, here are the most common causes and fixes.

## Quick Diagnostic

Run the diagnostic script:
```bash
./scripts/diagnose_autonomy.sh
```

Or manually check:

### 1. Check if Autonomy is Paused

```bash
kubectl exec -n agi deployment/redis -- redis-cli GET auto_executor:paused
```

If it returns `1`, autonomy is paused. Fix:
```bash
kubectl exec -n agi deployment/redis -- redis-cli DEL auto_executor:paused
```

### 2. Check Background LLM Status

```bash
kubectl exec -n agi deployment/hdn-server-rpi58 -- env | grep DISABLE_BACKGROUND_LLM
```

If it's set to `1` or `true`, background LLM is disabled. This prevents goal execution. Check the HDN deployment config and ensure `DISABLE_BACKGROUND_LLM=0` or remove it.

### 3. Check FSM Autonomy Configuration

```bash
kubectl exec -n agi deployment/fsm-server-rpi58 -- env | grep AUTONOMY
```

Should be `AUTONOMY=true`. Also check the FSM config file for `autonomy: true` and `autonomy_every: <seconds>`.

### 4. Check Active Goals

```bash
# Count active goals
kubectl exec -n agi deployment/redis -- redis-cli LRANGE reasoning:curiosity_goals:General 0 199 | jq -r 'select(.status == "active")' | wc -l
```

If you have 2+ active goals and `FSM_MAX_ACTIVE_GOALS=2`, new goals won't be selected until one completes.

### 5. Check FSM State

```bash
curl http://localhost:8083/thinking | jq '.current_state'
```

If stuck in a state like `plan` or `act` for a long time, check logs:
```bash
kubectl logs -n agi -l app=fsm-server-rpi58 --tail=100 | grep -i "error\|stuck\|timeout"
```

## Common Issues

### Issue 1: Autonomy Paused

**Symptoms:**
- GPU busy but no new activity
- No goals being generated
- Logs show "Autonomy paused by Redis flag"

**Fix:**
```bash
kubectl exec -n agi deployment/redis -- redis-cli DEL auto_executor:paused
```

**Why it happens:**
- Manual goal execution sets a 2-minute pause
- If a process crashes, the pause flag might persist

### Issue 2: Background LLM Disabled

**Symptoms:**
- Goals are generated but never executed
- GPU not being used for goal execution
- Logs show goals being created but not acted upon

**Fix:**
Check HDN deployment config:
```bash
kubectl get deployment hdn-server-rpi58 -n agi -o yaml | grep DISABLE_BACKGROUND_LLM
```

Ensure it's set to `0` or remove the env var entirely.

### Issue 3: Processing Capacity Full

**Symptoms:**
- Goals exist in Redis but none are active
- Logs show "Processing capacity full, skipping goal selection"
- `FSM_MAX_ACTIVE_GOALS` limit reached

**Fix:**
1. Check active goals:
```bash
kubectl exec -n agi deployment/redis -- redis-cli LRANGE reasoning:curiosity_goals:General 0 199 | jq -r 'select(.status == "active")'
```

2. If goals are stuck, manually mark them as completed or failed:
```bash
# This requires editing the goal JSON in Redis - use the monitor UI or API
```

3. Or increase the limit temporarily:
```bash
kubectl set env deployment/fsm-server-rpi58 -n agi FSM_MAX_ACTIVE_GOALS=3
```

### Issue 4: Goals in Cooldown

**Symptoms:**
- Goals generated but marked as ineligible
- Logs show "Skipping bootstrap for 'X' (cooldown/seen)"

**Fix:**
This is normal behavior - goals have cooldowns to prevent spam. Wait for cooldown to expire or reduce `FSM_BOOTSTRAP_COOLDOWN_HOURS` in FSM config.

### Issue 5: No Goals Being Generated

**Symptoms:**
- No goals in Redis at all
- Logs show "No curiosity goals from reasoning"

**Possible causes:**
1. No knowledge base to generate goals from
2. All goal types have low success rates
3. Domain has no concepts

**Fix:**
1. Check if knowledge exists:
```bash
curl http://localhost:8081/api/v1/knowledge/stats
```

2. Trigger manual learning:
```bash
curl -X POST http://localhost:8083/input \
  -H "Content-Type: application/json" \
  -d '{"input": "Test input to trigger learning", "session_id": "manual_test"}'
```

3. Check reasoning engine logs:
```bash
kubectl logs -n agi -l app=fsm-server-rpi58 --tail=100 | grep -i "reasoning\|curiosity"
```

## Monitoring

### Check Current Activity

```bash
# FSM activity
curl http://localhost:8083/activity?limit=10 | jq

# Current thinking
curl http://localhost:8083/thinking | jq

# Goals
curl http://localhost:8082/api/goals/curiosity | jq
```

### Watch Logs

```bash
# FSM autonomy logs
kubectl logs -n agi -l app=fsm-server-rpi58 -f | grep -i "autonomy\|goal"

# HDN async queue logs
kubectl logs -n agi -l app=hdn-server-rpi58 -f | grep -i "async\|llm\|queue"
```

## Quick Fix Script

Run the automated fix:
```bash
./scripts/fix_autonomy.sh
```

This will:
1. Unpause autonomy
2. Check for stuck goals
3. Show recent logs

## Prevention

1. **Monitor the pause flag**: Set up alerts if `auto_executor:paused` is set for > 5 minutes
2. **Check active goals regularly**: Ensure goals are completing, not getting stuck
3. **Monitor FSM state**: If stuck in one state > 30 minutes, investigate
4. **Review logs**: Check for errors in autonomy cycle execution

## Still Not Working?

1. Check all services are healthy:
```bash
curl http://localhost:8082/api/status | jq '.services'
```

2. Verify FSM config has autonomy enabled:
```bash
kubectl get configmap fsm-config -n agi -o yaml | grep -A 5 autonomy
```

3. Check if FSM is actually running autonomy cycles:
```bash
kubectl logs -n agi -l app=fsm-server-rpi58 --tail=200 | grep "🤖.*Autonomy"
```

4. Verify Redis connectivity:
```bash
kubectl exec -n agi deployment/fsm-server-rpi58 -- redis-cli -h redis.agi.svc.cluster.local PING
```





