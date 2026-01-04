# Fix for Vague Curiosity Goals

## Problem

Curiosity goals were being generated with vague, nested descriptions like:
- `Test hypothesis: How can we better test: Investigate System state: learn to discover new General opportunities`
- `Test hypothesis: What are the specific conditions for: What additional evidence would support: Investigate System state: learn to discover new General opportunities`

These goals were:
1. **Not actionable** - Too vague to execute
2. **Nested** - Multiple prefixes concatenated together
3. **Not filtered** - Generic goal filters weren't catching them

## Root Cause

1. **Follow-up hypothesis generation** (`fsm/engine.go:generateFollowUpHypotheses`):
   - When hypothesis tests were inconclusive, it generated follow-ups with prefixes like "How can we better test:", "What additional evidence would support:", etc.
   - These prefixes were then concatenated with the original vague hypothesis description

2. **Goal description creation** (`fsm/autonomy.go:generateHypothesisTestingGoalsForExisting`):
   - Simply prefixed "Test hypothesis: " to the hypothesis description
   - Didn't check for or handle nested prefixes

3. **Generic goal filtering** (`fsm/autonomy.go:isGenericHypothesisGoal`):
   - Didn't detect nested vague descriptions (multiple colons)
   - Didn't catch patterns like "learn to discover new General opportunities"

## Solution

### 1. Improved Goal Description Generation

**File**: `fsm/autonomy.go:generateHypothesisTestingGoalsForExisting`

- Detects nested prefixes in hypothesis descriptions
- Extracts the core hypothesis from nested descriptions
- Creates more actionable descriptions like "Test and refine: [core hypothesis]" instead of nested prefixes

**File**: `fsm/engine.go:createHypothesisTestingGoals`

- Same improvements applied to the main goal creation function
- Handles follow-up hypotheses with nested prefixes

### 2. Improved Follow-up Hypothesis Generation

**File**: `fsm/engine.go:generateFollowUpHypotheses`

- Detects if description already has nested prefixes
- Extracts core hypothesis before generating follow-ups
- Creates more specific, actionable follow-up descriptions:
  - Old: "How can we better test: [hypothesis]"
  - New: "Design a specific test to validate: [hypothesis]"
  - Old: "What additional evidence would support: [hypothesis]"
  - New: "Identify concrete evidence needed to support: [hypothesis]"

### 3. Enhanced Generic Goal Filtering

**Files**: 
- `fsm/autonomy.go:isGenericHypothesisGoal`
- `fsm/reasoning_engine.go:isGenericGoal`

**Improvements**:
- Detects nested vague descriptions by counting colons (>2 indicates nesting)
- Catches patterns like "learn to discover new", "discover new general opportunities"
- Detects nested prefixes like "test hypothesis: how can we better test:"
- Filters out goals with multiple question prefixes

## Expected Results

After these fixes:
1. **More actionable goals**: Goals will have clear, specific descriptions
2. **No nested prefixes**: Follow-up hypotheses won't create nested vague descriptions
3. **Better filtering**: Generic goal filters will catch and remove vague goals
4. **Cleaner goal list**: Only useful, actionable goals will be created

## Testing

To verify the fixes work:
1. Check goal descriptions in Redis:
   ```bash
   kubectl exec -n agi deployment/redis -- redis-cli LRANGE "reasoning:curiosity_goals:General" 0 10 | jq -r '.description'
   ```
2. Look for:
   - ✅ No nested prefixes (multiple colons)
   - ✅ No vague patterns like "learn to discover new General opportunities"
   - ✅ Actionable descriptions like "Test and refine: [specific hypothesis]"
3. Check logs for filtered goals:
   ```bash
   kubectl logs -n agi deployment/fsm-server-rpi58 --tail=100 | grep "Filtered out generic"
   ```

## Related Issues

- See `docs/SYSTEM_STUCK_ANALYSIS.md` for the goal execution bottleneck issue
- These fixes address the quality of goals, but the conversion rate issue still needs to be addressed separately





