# Goal Screening and GPU Optimization

## Problem

1. **Vague Goals**: Curiosity goals were being generated with vague, non-actionable descriptions
2. **GPU Overload**: GPU was at 100% load all the time due to too many LLM calls
3. **Low-Value Goals**: Many goals were being created that weren't useful or actionable

## Solution

### 1. LLM Screening for Curiosity Goals

Added LLM screening for curiosity goals (similar to hypothesis screening) to filter out useless goals before they consume resources.

**Implementation**: `fsm/autonomy.go:screenCuriosityGoalsWithLLM()`

**Features**:
- Screens goals on three criteria: Actionability, Value, and Tractability
- Configurable threshold (default: 0.5)
- Skips hypothesis testing goals (already screened)
- Falls back to allowing goals if screening fails (fail-safe)

### 2. GPU Load Reduction

Implemented batching and rate limiting to reduce GPU load:

**Batching**:
- Processes goals in batches of 3 (configurable via `FSM_GOAL_SCREEN_BATCH_SIZE`)
- 8-second delay between batches (configurable via `FSM_GOAL_SCREEN_BATCH_DELAY_MS`)
- 3-second delay between individual goals (configurable via `FSM_GOAL_SCREEN_DELAY_MS`)

**Rate Limiting**:
- Delays between goals prevent GPU saturation
- Longer delays between batches give GPU time to process
- All screening requests marked as LOW priority background tasks

### 3. Configuration

**Config File** (`fsm/config/artificial_mind.yaml`):
```yaml
agent:
  goal_screen_threshold: 0.5  # Threshold for curiosity goal LLM screening (0.0-1.0)
```

**Environment Variables**:
- `FSM_GOAL_SCREENING_ENABLED` - Enable/disable screening (default: enabled)
- `FSM_GOAL_SCREEN_THRESHOLD` - Override threshold from config (0.0-1.0)
- `FSM_GOAL_SCREEN_BATCH_SIZE` - Goals per batch (default: 3)
- `FSM_GOAL_SCREEN_BATCH_DELAY_MS` - Delay between batches in ms (default: 8000)
- `FSM_GOAL_SCREEN_DELAY_MS` - Delay between goals in ms (default: 3000)

## Expected Results

1. **Better Goal Quality**: Only actionable, valuable goals pass screening
2. **Reduced GPU Load**: Batching and delays prevent GPU saturation
3. **Fewer Low-Value Goals**: Vague or useless goals are filtered out early
4. **Better Resource Usage**: GPU time is spent on valuable goals, not vague ones

## Performance Impact

**Before**:
- All goals generated → All goals stored → All goals executed
- GPU at 100% from too many concurrent LLM calls
- Many vague goals consuming resources

**After**:
- Goals generated → LLM screening (batched, rate-limited) → Only valuable goals stored → Only valuable goals executed
- GPU load reduced through batching and delays
- Vague goals filtered out before execution

## Tuning

To adjust GPU load vs. goal quality:

**More Aggressive Filtering (Less GPU Load)**:
- Increase `goal_screen_threshold` to 0.6-0.7
- Increase `FSM_GOAL_SCREEN_BATCH_DELAY_MS` to 10000-15000
- Increase `FSM_GOAL_SCREEN_DELAY_MS` to 5000

**Less Aggressive Filtering (More Goals)**:
- Decrease `goal_screen_threshold` to 0.3-0.4
- Decrease delays if GPU can handle it

**Disable Screening** (if needed):
- Set `FSM_GOAL_SCREENING_ENABLED=false`

## Related

- See `docs/VAGUE_GOALS_FIX.md` for fixes to vague goal descriptions
- See `docs/SYSTEM_STUCK_ANALYSIS.md` for goal execution bottleneck issues
- See `docs/GPU_OPTIMIZATION.md` for general GPU optimization strategies





