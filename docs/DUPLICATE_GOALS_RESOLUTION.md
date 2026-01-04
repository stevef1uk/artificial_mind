# Duplicate Goals Resolution Plan

## Problem Analysis

You're seeing multiple duplicate Goal entries with identical or very similar descriptions (behavior_loop patterns like `hypothesize->hypothesize`, `learn->learn`, `perceive->idle`). These are being repeatedly created even though the UI shows deduplication.

### Root Cause

1. **Insufficient Deduplication in Coherence Monitor** (`fsm/coherence_monitor.go:435-447`)
   - The `checkBehaviorLoops()` function detects state transitions and flags them as behavior loops
   - It tries to prevent duplicates using a Redis key with a **10-minute TTL**
   - Multiple coherence checks within that window create duplicate inconsistency objects
   - Each inconsistency becomes a separate Goal in the Goal Manager

2. **Threshold Too Low**
   - Flagging threshold was 5 occurrences (now raised to 10)
   - Normal FSM activity can easily trigger this, creating false positives

3. **Persistent Storage**
   - Goals are stored permanently in Goal Manager
   - While the Monitor UI deduplicates on display, the underlying data contains duplicates
   - This causes the Goals list to grow indefinitely

## Solution Implemented

### 1. Fixed Coherence Monitor Deduplication
**File**: `fsm/coherence_monitor.go`

**Changes**:
- Increased transition count threshold from **5 to 10** to reduce false positives
- Extended Redis deduplication TTL from **10 minutes to 24 hours**
- This prevents the same behavior loop from being flagged multiple times per day

**Effect**: 
- New behavior loop detections will be deduplicated more aggressively
- Existing duplicates in storage are not affected

### 2. Cleanup Tools Created

#### Option A: Go Tool (Recommended)
**Location**: `cmd/cleanup-duplicate-goals/main.go`

**Build**:
```bash
cd /home/stevef/dev/artificial_mind/cmd/cleanup-duplicate-goals
go build -o cleanup-duplicate-goals
```

**Usage**:
```bash
# Dry run (shows what would be deleted)
./cleanup-duplicate-goals --url http://localhost:8090 --agent agent_1 --dry-run

# Actually delete duplicates
./cleanup-duplicate-goals --url http://localhost:8090 --agent agent_1
```

#### Option B: Bash Script
**Location**: `scripts/cleanup-duplicate-goals.sh`

**Usage**:
```bash
chmod +x scripts/cleanup-duplicate-goals.sh
./scripts/cleanup-duplicate-goals.sh http://localhost:8090 agent_1
```

### 3. How Deduplication Works

Both cleanup tools:
1. Fetch all active goals from Goal Manager
2. Normalize descriptions (lowercase, trim whitespace)
3. Group goals by normalized description
4. For each group, keep the most recently updated goal
5. Delete older duplicates

## How to Use

### Step 1: Rebuild FSM Service
```bash
cd /home/stevef/dev/artificial_mind/fsm
go build -o fsm

# Then restart the FSM service (in Docker/K8s or locally)
```

### Step 2: Clean Up Existing Duplicates

**Using the Go tool** (recommended):
```bash
cd /home/stevef/dev/artificial_mind/cmd/cleanup-duplicate-goals
go build -o cleanup-duplicate-goals

# Dry run first to see what would be deleted
./cleanup-duplicate-goals --dry-run

# Then delete
./cleanup-duplicate-goals
```

**Using the bash script**:
```bash
./scripts/cleanup-duplicate-goals.sh
```

### Step 3: Verify

Check the monitor UI at `http://localhost:3000` and confirm:
- Duplicate goals are gone
- No new duplicates are being created every minute
- Behavior loops are only flagged once per category per day

## Conflicting Elements Reconciled

**Before**: 
- Coherence monitor created multiple inconsistencies for the same transition within 10 minutes
- Each became a separate goal, causing duplicates to accumulate

**After**:
- Single flagging per 24-hour period per transition type
- Duplicates can be cleaned with provided tools
- System is self-correcting going forward

## Expected Behavior

After these changes:

1. **Immediately**: Fewer duplicate goals being created (threshold raised, TTL extended)
2. **On cleanup run**: All current duplicates removed
3. **Going forward**: 
   - Same behavior loop type flagged max once per day
   - If system truly enters a pathological loop, it gets flagged once and stays visible
   - Operator must then investigate and fix the underlying cause

## Monitoring

Check the logs from coherence monitor:
```bash
# Look for deduplication messages
grep "already flagged recently" fsm.log

# See which loops are being detected
grep "Potential behavior loop" fsm.log
```

## Additional Notes

- The deduplication is per-transition-type (e.g., `perceive->idle` is different from `idle->perceive`)
- Each agent ID has separate goal tracking
- The Redis TTL key `coherence:flagged_loop:TRANSITION` tracks recent detections
- You can manually clear dedup cache if needed: `redis-cli DEL "coherence:flagged_loop:*"`
