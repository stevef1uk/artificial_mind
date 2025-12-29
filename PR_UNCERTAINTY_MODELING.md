# Uncertainty Modeling & Confidence Calibration

## Overview

This PR introduces a formal uncertainty modeling system that distinguishes between epistemic (lack of knowledge) and aleatoric (inherent randomness) uncertainty. This enables more principled decision-making, better exploration vs exploitation trade-offs, and fewer spurious curiosity loops.

## Problem Statement

Previously, hypotheses & goals had:
- Value scores
- Confidence scaling
- Heuristic thresholds

But there was no formal uncertainty model. This becomes important as:
- Beliefs compound across inference chains
- Hypotheses influence goal selection
- Learning modifies strategy over time

## Solution

### Core Features

1. **Uncertainty Model Types**
   - Epistemic uncertainty (reducible through more information)
   - Aleatoric uncertainty (inherent randomness)
   - Calibrated confidence (combines both uncertainties)
   - Belief stability vs volatility tracking

2. **Confidence Propagation**
   - Propagates confidence through inference chains
   - Applies chain length penalties to epistemic uncertainty
   - Uses geometric mean for conservative confidence combination

3. **Confidence Decay**
   - Epistemic uncertainty increases over time without reinforcement
   - Configurable decay rate per hour
   - Automatic decay application functions

4. **Stability & Volatility Tracking**
   - Tracks confidence history over time
   - Calculates stability (inverse of volatility)
   - Identifies volatile vs stable beliefs

### Implementation Details

**New Files:**
- `fsm/uncertainty_model.go` - Core uncertainty modeling system (352 lines)

**Updated Files:**
- `fsm/autonomy.go` - Goal scoring with uncertainty models
- `fsm/engine.go` - Hypothesis/goal storage with uncertainty
- `fsm/knowledge_integration.go` - Hypothesis creation with uncertainty
- `fsm/reasoning_engine.go` - Inference and goal creation with uncertainty

**Test Files:**
- `test/test_uncertainty_modeling.sh` - Comprehensive test suite (k3s compatible)
- `test/test_uncertainty_quick.sh` - Quick verification script
- `test/trigger_uncertainty_test.sh` - Data generation helper
- `test/verify_uncertainty_fresh_data.sh` - Fresh data verification

### Data Structure Changes

**Hypothesis:**
```go
type Hypothesis struct {
    // ... existing fields ...
    Uncertainty *UncertaintyModel `json:"uncertainty,omitempty"`
}
```

**CuriosityGoal:**
```go
type CuriosityGoal struct {
    // ... existing fields ...
    Uncertainty *UncertaintyModel `json:"uncertainty,omitempty"`
    Value       float64          `json:"value,omitempty"`
}
```

**Belief:**
```go
type Belief struct {
    // ... existing fields ...
    Uncertainty *UncertaintyModel `json:"uncertainty,omitempty"`
}
```

## Benefits

✅ **More cautious decision-making** - Uncertainty-aware confidence prevents overconfidence  
✅ **Principled exploration vs exploitation** - Epistemic uncertainty guides exploration  
✅ **Fewer spurious curiosity loops** - Volatility tracking identifies unstable beliefs  
✅ **Better inference chain handling** - Confidence degrades appropriately through chains  
✅ **Time-aware beliefs** - Decay mechanism reduces confidence without reinforcement

## Testing

### Verified Working:
- ✅ New hypotheses include uncertainty models
- ✅ Uncertainty values in valid ranges [0,1]
- ✅ JSON structure correct and complete
- ✅ Works in both local and k3s environments
- ✅ Goal scoring incorporates uncertainty models

### Test Results:
```
✅ Hypotheses: Uncertainty models present
   - Epistemic: 0.5
   - Aleatoric: 0.1
   - Calibrated Confidence: 0.639
   - Stability: 0.5
   - Volatility: 0
```

## Backward Compatibility

- Legacy `Confidence` fields maintained for compatibility
- New `Uncertainty` models are optional (omitempty)
- Old data without uncertainty models continues to work
- New data automatically includes uncertainty models

## Usage

### For New Data:
All new hypotheses, goals, and beliefs automatically include uncertainty models.

### For Existing Data:
Old data created before this PR won't have uncertainty models. This is expected and doesn't affect functionality. New data will include uncertainty models.

### Testing:
```bash
# Run comprehensive test
./test/test_uncertainty_modeling.sh

# Quick verification
./test/test_uncertainty_quick.sh

# Verify with fresh data
./test/verify_uncertainty_fresh_data.sh
```

## Files Changed

- `fsm/uncertainty_model.go` (new, 352 lines)
- `fsm/autonomy.go` (+57 lines)
- `fsm/engine.go` (+55 lines)
- `fsm/knowledge_integration.go` (+37 lines)
- `fsm/reasoning_engine.go` (+150 lines)
- Test scripts (4 new files, 499 lines)

**Total:** 9 files changed, +1,121 insertions, -29 deletions

## Migration Notes

No migration required. The system is backward compatible:
- Old data continues to work
- New data automatically includes uncertainty models
- No configuration changes needed

## Future Enhancements (Out of Scope)

- Uncertainty-based goal prioritization algorithms
- Adaptive decay rates based on domain
- Uncertainty visualization in Monitor UI
- Uncertainty-based exploration strategies

---

**Status:** ✅ Ready to merge  
**Tested:** ✅ Local and k3s environments  
**Documentation:** ✅ Code comments and test scripts included

