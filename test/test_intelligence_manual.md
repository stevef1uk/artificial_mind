# Manual Intelligence Testing Guide

This guide helps you manually test the intelligence improvements to verify they're working.

## Prerequisites

- System is running on Kubernetes
- kubectl is configured and can access the cluster
- Services are deployed in namespace (default: `agi`)
- Port-forwarding set up if accessing from local machine

## Kubernetes Setup

### 1. Check if services are running

```bash
# Check pods
kubectl get pods -n agi

# Check services
kubectl get svc -n agi

# Get HDN pod name
HDN_POD=$(kubectl get pods -n agi -l app=hdn-server -o jsonpath='{.items[0].metadata.name}')
echo $HDN_POD
```

### 2. Set up port-forwarding (if needed)

```bash
# Port-forward HDN server
kubectl port-forward -n agi svc/hdn-server 8081:8081 &

# Port-forward FSM server  
kubectl port-forward -n agi svc/fsm-server 8083:8083 &

# Port-forward Redis (for direct access)
kubectl port-forward -n agi svc/redis 6379:6379 &
```

### 3. Or use NodePort/Ingress

If services are exposed via NodePort or Ingress, use those URLs instead:
```bash
# Get NodePort URL
kubectl get svc -n agi hdn-server -o jsonpath='{.spec.ports[0].nodePort}'
```

## Test 1: Code Generation Intelligence

### Step 1: Generate code that will fail (to create learning data)

```bash
curl -X POST http://localhost:8081/api/v1/intelligent/execute \
  -H "Content-Type: application/json" \
  -d '{
    "task_name": "test_go_unused",
    "description": "Create a Go program that imports fmt and os but only uses fmt",
    "language": "go"
  }'
```

**Expected**: Code may fail initially with "imported and not used" error, then get fixed.

### Step 2: Generate similar code again

```bash
curl -X POST http://localhost:8081/api/v1/intelligent/execute \
  -H "Content-Type: application/json" \
  -d '{
    "task_name": "test_go_unused_2",
    "description": "Create a Go program that imports json and fmt but only uses fmt",
    "language": "go"
  }'
```

**Expected**: 
- Check logs for: `🧠 [INTELLIGENCE] Added X prevention hints from learned experience`
- Code should include prevention hints in the prompt
- Should have fewer retries than first attempt

### Step 3: Check the generated code prompt

Look in the HDN server logs for the code generation prompt. You should see:

```
🧠 LEARNED FROM EXPERIENCE - Common errors to avoid:
- Remove unused imports - they cause compilation errors
```

## Test 2: Verify Learning Data Storage

### Check Redis for learning data (Kubernetes)

```bash
# Get Redis pod name
REDIS_POD=$(kubectl get pods -n agi -l app=redis -o jsonpath='{.items[0].metadata.name}')

# Check learning data via kubectl exec
kubectl exec -n agi $REDIS_POD -- redis-cli KEYS "failure_pattern:*"
kubectl exec -n agi $REDIS_POD -- redis-cli KEYS "codegen_strategy:*"
kubectl exec -n agi $REDIS_POD -- redis-cli KEYS "prevention_hint:*"

# Or if you port-forwarded Redis
redis-cli -h localhost -p 6379 KEYS "failure_pattern:*"
```

**Expected**: Should see keys like:
- `failure_pattern:compilation:import_error:go`
- `codegen_strategy:general:go`
- `prevention_hint:compilation:import_error:go`

## Test 3: Hypothesis Generation Intelligence

### Check FSM activity for hypothesis generation

```bash
curl http://localhost:8083/activity?limit=20 | jq '.activities[] | select(.category == "hypothesis")'
```

**Expected**: Should see activities like:
- "Generated X hypotheses in domain 'Y'"
- If a hypothesis fails, similar ones should be skipped

### Force hypothesis generation

```bash
# Trigger autonomy cycle which generates hypotheses
curl -X POST http://localhost:8083/trigger_autonomy
```

**Expected in logs**: 
- `🧠 [INTELLIGENCE] Skipping hypothesis similar to failed one: '...'`

## Test 4: Planner Capability Selection

### Test hierarchical planning

```bash
curl -X POST http://localhost:8081/api/v1/hierarchical/execute \
  -H "Content-Type: application/json" \
  -d '{
    "input": "Create a Python program to calculate fibonacci numbers",
    "task_name": "fibonacci",
    "description": "Calculate fibonacci sequence"
  }'
```

**Expected in logs**:
- Capabilities should be sorted by success rate
- More successful capabilities should be preferred

## Test 5: Goal Scoring Intelligence

### Check goal scoring

```bash
# Get FSM status
curl http://localhost:8083/status | jq '.goals[] | {id, type, priority, score}'
```

**Expected**: Goals with higher historical success rates should have higher scores.

### Check goal outcomes

```bash
# If redis-cli available
redis-cli LRANGE "goal_outcomes:all" 0 10
```

**Expected**: Should see goal outcomes with success/failure data.

## Monitoring Intelligence in Real-Time (Kubernetes)

### Watch HDN logs for intelligence messages

```bash
# Get HDN pod name
HDN_POD=$(kubectl get pods -n agi -l app=hdn-server -o jsonpath='{.items[0].metadata.name}')

# Watch logs with filtering
kubectl logs -n agi -f $HDN_POD | grep -i "intelligence\|learned\|prevention"

# Or view recent logs
kubectl logs -n agi $HDN_POD --tail=100 | grep -i "intelligence\|learned\|prevention"
```

**Look for**:
- `🧠 [INTELLIGENCE] Added X prevention hints`
- `🧠 [INTELLIGENCE] Retrieved learned prevention hint`
- `🧠 [INTELLIGENCE] Using learned successful strategy`

### Watch FSM logs for intelligence messages

```bash
# Get FSM pod name
FSM_POD=$(kubectl get pods -n agi -l app=fsm-server -o jsonpath='{.items[0].metadata.name}')

# Watch logs
kubectl logs -n agi -f $FSM_POD | grep -i "intelligence\|similar.*failed\|success.*bonus"
```

**Look for**:
- `🧠 [INTELLIGENCE] Skipping hypothesis similar to failed one`
- `📊 Goal X: success rate bonus +Y`
- `💰 Goal X: value bonus +Y`

## Quick Verification Checklist

- [ ] Code generation includes prevention hints in prompts
- [ ] Similar code generation tasks have fewer retries over time
- [ ] Redis contains learning data (failure_pattern, codegen_strategy keys)
- [ ] Hypothesis generation skips similar failed hypotheses
- [ ] Goals are scored with historical success bonuses
- [ ] Planner prefers capabilities with higher success rates
- [ ] Logs show intelligence messages

## Troubleshooting

If intelligence doesn't seem to be working:

1. **Check Redis connection**: Ensure HDN and FSM can connect to Redis
2. **Check learning data**: Verify keys exist in Redis
3. **Check logs**: Look for error messages about Redis or learning
4. **Wait for data**: Intelligence needs some execution history to work
5. **Verify deployment**: Ensure latest code is deployed

## Expected Behavior Over Time

1. **First few executions**: System learns from failures, stores patterns
2. **After 5-10 executions**: System starts using learned hints
3. **After 20+ executions**: System shows clear improvement (fewer retries, better choices)

The system gets smarter as it accumulates more learning data!

