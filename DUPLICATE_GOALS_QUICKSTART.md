# Quick Start: Duplicate Goals Cleanup - RESOLVED

## Issues Found & Fixed

### Issue 1: Duplicate Behavior Loop Goals
**Problem**: Multiple goals with same transition but different counts (e.g., `perceive->idle` with counts 28, 26, 16, 27, 34...)

**Root Cause**: Deduplication key included count value, so each count change created a new goal.

**Fix Applied**: ✅ Lines 527-554 in `fsm/coherence_monitor.go`
- Deduplicate by transition type only (ignoring count variations)
- Extended TTL from 5 minutes → 24 hours
- Increased detection threshold from 5 → 10 transitions

### Issue 2: Coherence Check Hanging
**Problem**: FSM coherence monitoring hung during belief contradiction check, preventing completion.

**Root Cause**: `checkBeliefContradictions()` queried 125,485 beliefs and did O(n²) comparisons (100×100 = 10,000 comparisons per domain).

**Fix Applied**: ✅ Lines 83-96 in `fsm/coherence_monitor.go`
- Disabled belief contradiction check by default (too slow)
- Can re-enable with `ENABLE_BELIEF_CHECK=true` environment variable
- Allows coherence check to complete and behavior loop detection to work

## Cleanup Tool
**Location**: `cmd/cleanup-duplicate-goals/main.go`

**Usage**:
```bash
cd /home/stevef/dev/artificial_mind/cmd/cleanup-duplicate-goals

# Dry run to see what would be deleted
./cleanup-duplicate-goals --dry-run

# Actually delete duplicates
./cleanup-duplicate-goals
```

## Deploy Fixed Code

**Rebuild Docker image**:
```bash
cd /home/stevef/dev/artificial_mind
docker build -f Dockerfile.fsm.secure -t stevef1uk/fsm-server:secure .
docker push stevef1uk/fsm-server:secure
```

**Restart FSM in Kubernetes**:
```bash
kubectl rollout restart deployment/fsm-server-rpi58 -n agi
```

## Expected Results

**Before Fixes**:
- Same behavior loop flagged repeatedly with different counts (40+ duplicate goals)
- Coherence check hung on belief contradiction checking
- System never completed coherence monitoring cycle

**After Fixes**:
- Each behavior loop transition flagged **max once per 24 hours**
- Coherence check completes in seconds (skips slow belief check)
- System properly detects and deduplicates behavior loops

---

**Status**: ✅ All fixes committed and pushed to Git (commit `6cee6eb`)

