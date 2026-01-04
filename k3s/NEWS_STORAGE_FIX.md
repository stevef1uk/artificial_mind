# News Event Storage Issue - Diagnosis & Fix

## Problem
News events are being received by FSM (visible in logs) but not appearing in Monitor UI Overview pane.

## Root Cause
The `store_news_events` action is configured in the `perceive` state, but news events may not be triggering the state transition properly due to:

1. **Guard Condition**: News events have `input_validation` guard that might be failing
2. **State Dependency**: Actions only execute when entering a state, not when events arrive
3. **Silent Failures**: Storage might be failing but errors aren't visible

## Diagnosis Steps

Run the diagnostic script:
```bash
cd k3s
./diagnose-news-storage.sh
```

Check FSM logs for:
```bash
# Check if storage is being attempted
kubectl logs -n agi -l app=fsm-server-rpi58 --tail=500 | grep -E "Storing news events|Stored news event|storeNewsEventInWeaviate"

# Check for state transitions
kubectl logs -n agi -l app=fsm-server-rpi58 --tail=500 | grep -E "Processing event.*news|transition.*perceive"

# Check for guard failures
kubectl logs -n agi -l app=fsm-server-rpi58 --tail=500 | grep -E "Guard.*failed|input_validation"
```

## Quick Fix Options

### Option 1: Remove Guard (Recommended)
Edit `fsm/config/artificial_mind.yaml` and remove the guard from news event transitions:

```yaml
# In the idle state
news_relations:
  next: perceive
  # Remove: guard: "input_validation"
news_alerts:
  next: perceive
  # Remove: guard: "input_validation"

# In the perceive state (if present)
news_relations:
  next: perceive
  # Remove: guard: "input_validation"
news_alerts:
  next: perceive
  # Remove: guard: "input_validation"
```

### Option 2: Make Storage Action State-Independent
Modify `executeNewsStorage` to be called directly when news events arrive, not just when entering `perceive` state.

### Option 3: Check Guard Implementation
Verify that `input_validation` guard allows news events through. The guard might be too restrictive.

## Verification

After applying fix, verify:

1. **FSM logs show storage attempts**:
   ```bash
   kubectl logs -n agi -l app=fsm-server-rpi58 -f | grep "Storing news events"
   ```

2. **Weaviate contains events**:
   ```bash
   kubectl port-forward -n agi svc/weaviate 8080:8080
   curl -X POST http://localhost:8080/v1/graphql \
     -H "Content-Type: application/json" \
     -d '{"query": "{ Get { WikipediaArticle(limit: 10, where: { path: [\"source\"], operator: Equal, valueString: \"news:fsm\" }) { title source timestamp } } }"}'
   ```

3. **Monitor UI shows events**:
   ```bash
   kubectl port-forward -n agi svc/monitor-ui 8082:8082
   curl http://localhost:8082/api/news/events
   ```

## Expected Behavior

When news events arrive:
1. FSM receives NATS event: `📨 Received NATS event on agi.events.news.relations`
2. Event triggers transition: `✅ Found transition: idle -> perceive for event news_relations`
3. Actions execute: `📰 Storing news events for curiosity goal generation`
4. Storage succeeds: `✅ Stored news event in Weaviate: news_20251225_xxx (type: relations)`
5. Monitor UI queries Weaviate and displays events

## Current Status

Based on your logs:
- ✅ Events are being received
- ❌ Storage is not happening (no "Storing news events" or "Stored news event" logs)
- ❓ State transitions may not be occurring

## Next Steps

1. Run `./diagnose-news-storage.sh` to get detailed status
2. Check FSM logs for state transitions and guard evaluations
3. Apply one of the fix options above
4. Restart FSM pod to apply config changes:
   ```bash
   kubectl rollout restart deployment/fsm-server-rpi58 -n agi
   ```





