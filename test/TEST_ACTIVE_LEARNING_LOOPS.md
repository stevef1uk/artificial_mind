# Testing Active Learning Loops

## Quick Start

Run the test script:
```bash
./test/test_active_learning_loops.sh
```

Or check status:
```bash
./test/test_active_learning_loops.sh status
```

## What the Test Does

1. **Checks for high-uncertainty concepts** in beliefs, hypotheses, and goals
2. **Triggers (best-effort) curiosity goal generation** and then polls Redis until active learning goals appear (or times out)
3. **Verifies active learning goals** are created with type `active_learning`
4. **Checks goal properties** (uncertainty models, priority, value)
5. **Reviews FSM logs** for active learning messages

## Manual Testing Steps

### 1. Check Current Status

```bash
# Quick status check
./test/test_active_learning_loops.sh status

# Or manually check Redis
kubectl exec -n agi <redis-pod> -- redis-cli LRANGE reasoning:curiosity_goals:General 0 9 | grep -i "active_learning"
```

### 2. Create High-Uncertainty Concept for Testing

To test active learning, you need concepts with high epistemic uncertainty (≥0.4). Create a test belief:

```bash
REDIS_POD=$(kubectl get pods -n agi -l app=redis -o jsonpath='{.items[0].metadata.name}')

# Create a high-uncertainty belief
kubectl exec -n agi $REDIS_POD -- redis-cli LPUSH reasoning:beliefs:General '{
  "id": "test_high_uncertainty_1",
  "statement": "Quantum Computing will revolutionize cryptography",
  "domain": "General",
  "confidence": 0.3,
  "uncertainty": {
    "epistemic_uncertainty": 0.7,
    "aleatoric_uncertainty": 0.1,
    "calibrated_confidence": 0.3
  },
  "created_at": "'$(date -u +%Y-%m-%dT%H:%M:%SZ)'"
}'
```

### 3. Trigger Curiosity Goal Generation

Active learning goals are generated as part of normal curiosity goal generation. In practice, the system typically generates these via the **FSM autonomy scheduler**. Some environments may also expose a `POST /api/v1/events` endpoint, but this is **best-effort** and not guaranteed.

```bash
kubectl port-forward -n agi svc/fsm-server-rpi58 8083:8083 &
curl -s -X POST http://localhost:8083/api/v1/events \
  -H "Content-Type: application/json" \
  -d '{"event":"generate_curiosity_goals","payload":{}}' || true
```

### 4. Verify Active Learning Goals Were Created

```bash
REDIS_POD=$(kubectl get pods -n agi -l app=redis -o jsonpath='{.items[0].metadata.name}')

# Check for active learning goals
kubectl exec -n agi $REDIS_POD -- redis-cli LRANGE reasoning:curiosity_goals:General 0 19 | \
  python3 -c "
import sys, json
for line in sys.stdin:
    try:
        goal = json.loads(line.strip())
        if goal.get('type') == 'active_learning':
            print(json.dumps(goal, indent=2))
    except:
        pass
"
```

### 5. Check FSM Logs

```bash
FSM_POD=$(kubectl get pods -n agi -l app=fsm-server -o jsonpath='{.items[0].metadata.name}')

# Look for active learning messages
kubectl logs -n agi $FSM_POD --tail=200 | grep -i "ACTIVE-LEARNING\|active learning\|high-uncertainty"
```

Expected log messages:
- `🔍 [ACTIVE-LEARNING] Identifying high-uncertainty concepts in domain: ...`
- `✅ [ACTIVE-LEARNING] Identified N high-uncertainty concepts`
- `📋 [ACTIVE-LEARNING] Generating data acquisition plans for N concepts`
- `⚡ [ACTIVE-LEARNING] Prioritizing N experiments by uncertainty reduction speed`
- `🎯 [ACTIVE-LEARNING] Converting N plans to curiosity goals`
- `✅ [ACTIVE-LEARNING] Generated N active learning goals`

## Expected Results

### Active Learning Goal Properties

Active learning goals should have:
- **Type**: `active_learning`
- **Priority**: 1-10 (based on efficiency ranking)
- **Uncertainty Model**: 
  - Epistemic uncertainty (reducible)
  - Aleatoric uncertainty (inherent)
  - Calibrated confidence
- **Value**: Uncertainty reduction potential (0-1)
- **Description**: Contains `[ACTIVE-LEARNING]` prefix
- **Targets**: Array with concept name(s)

### Example Active Learning Goal

```json
{
  "id": "active_learning_plan_Quantum_Computing_1234567890",
  "type": "active_learning",
  "description": "[ACTIVE-LEARNING] query_knowledge_base: Query Neo4j knowledge base for existing information about 'Quantum Computing'",
  "domain": "General",
  "priority": 9,
  "status": "pending",
  "targets": ["Quantum Computing"],
  "created_at": "2024-12-20T10:00:00Z",
  "uncertainty": {
    "epistemic_uncertainty": 0.585,
    "aleatoric_uncertainty": 0.1,
    "calibrated_confidence": 0.65
  },
  "value": 0.585
}
```

## Troubleshooting

### No Active Learning Goals Generated

**Possible reasons:**
1. **No high-uncertainty concepts**: Threshold is 0.4 epistemic uncertainty
   - **Solution**: Create test beliefs with high epistemic uncertainty (see step 2)

2. **Domain has no concepts**: System needs concepts in the domain
   - **Solution**: Ensure domain has some beliefs, hypotheses, or goals

3. **Uncertainty models missing**: Old data may not have uncertainty models
   - **Solution**: Wait for new data or trigger hypothesis/goal generation

4. **System needs more time**: Processing may take a few seconds
   - **Solution**: Wait 5-10 seconds after triggering goal generation

### Active Learning Goals Have Wrong Properties

**Check:**
- Goal type is `active_learning`
- Uncertainty model is present
- Priority is between 1-10
- Value matches uncertainty reduction potential

**Fix:** Check FSM logs for errors during goal generation

### Goals Not Being Prioritized Correctly

**Check:**
- Efficiency calculation: `uncertainty_reduction_potential / estimated_time`
- Plans with very high uncertainty (>0.7) should get 1.5x boost

**Verify:** Check FSM logs for prioritization messages

## Integration with Other Systems

### Goal Manager Integration

Active learning goals are stored in Redis like other curiosity goals:
- Key: `reasoning:curiosity_goals:<domain>`
- Format: JSON list of `CuriosityGoal` objects

The Monitor Service should automatically convert them to Goal Manager tasks (same as other curiosity goals).

### Monitor UI

Active learning goals should appear in the Monitor UI under:
- Curiosity Goals panel
- Filter by type: `active_learning`

## Advanced Testing

### Test with Multiple High-Uncertainty Concepts

Create multiple test beliefs with varying uncertainty:

```bash
# High uncertainty (should generate goal)
kubectl exec -n agi $REDIS_POD -- redis-cli LPUSH reasoning:beliefs:General '{
  "id": "test_high_1",
  "statement": "AI will achieve AGI by 2030",
  "domain": "General",
  "confidence": 0.3,
  "uncertainty": {"epistemic_uncertainty": 0.8, "aleatoric_uncertainty": 0.1, "calibrated_confidence": 0.3}
}'

# Medium uncertainty (may not generate goal if threshold is 0.4)
kubectl exec -n agi $REDIS_POD -- redis-cli LPUSH reasoning:beliefs:General '{
  "id": "test_medium_1",
  "statement": "Machine learning is useful",
  "domain": "General",
  "confidence": 0.6,
  "uncertainty": {"epistemic_uncertainty": 0.3, "aleatoric_uncertainty": 0.1, "calibrated_confidence": 0.6}
}'
```

### Test Prioritization

Create goals with different uncertainty reduction potentials and verify they're prioritized correctly:

1. Create beliefs with varying epistemic uncertainty
2. Trigger goal generation
3. Check that goals are sorted by priority (highest first)
4. Verify efficiency calculation is correct

## Success Criteria

✅ **Test passes if:**
- Active learning goals are generated when high-uncertainty concepts exist
- Goals have correct type (`active_learning`)
- Goals have uncertainty models
- Goals are prioritized by efficiency
- FSM logs show active learning messages

⚠️ **Test may pass even if:**
- No active learning goals generated (if no high-uncertainty concepts)
- This is expected behavior - system only generates goals when needed

