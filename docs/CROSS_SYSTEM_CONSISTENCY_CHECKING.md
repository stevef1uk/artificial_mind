# Cross-System Consistency Checking

## Overview

This feature introduces a **global coherence monitor** that checks for inconsistencies across the three main systems:
- **FSM** (produces goals + learning traces)
- **HDN** (executes workflows + learns from failure patterns)
- **Self-Model** (tracks goals + policies)

The coherence monitor acts as a **cognitive integrity system** that detects contradictions and generates self-reflection tasks to resolve them.

## What Was Implemented

### 1. CoherenceMonitor (`fsm/coherence_monitor.go`)

A comprehensive monitoring system that checks for:

#### **Belief Contradictions**
- Detects contradictory beliefs within the same domain
- Compares beliefs from reasoning traces
- Uses keyword-based contradiction detection (true/false, always/never, increase/decrease, etc.)
- Severity based on confidence levels of conflicting beliefs

#### **Policy Conflicts**
- Checks for conflicting goals in Self-Model
- Detects opposite objectives (e.g., "increase" vs "decrease", "enable" vs "disable")
- Analyzes active goals from Goal Manager

#### **Strategy Conflicts**
- Monitors learned code generation strategies from HDN
- Identifies conflicting approaches for the same task category
- Tracks strategies stored in Redis

#### **Goal Drift**
- Detects goals that have been active too long without progress
- Default threshold: 24 hours without updates
- Flags stale goals that may need attention or cancellation

#### **Behavior Loops**
- Analyzes FSM activity logs for repetitive state transitions
- Flags transitions that occur 5+ times in recent history
- Identifies potential infinite loops or stuck behaviors

### 2. Self-Reflection Task Generation

When inconsistencies are detected:
- **Generates self-reflection tasks** with priority based on severity
- **Creates curiosity goals** for the reasoning engine to process
- **Stores tasks in Redis** for tracking and resolution

### 3. Integration with FSM Engine

- **Periodic monitoring**: Runs coherence checks every 5 minutes
- **Automatic resolution**: Generates resolution tasks for detected inconsistencies
- **Logging**: Comprehensive logging of all detected inconsistencies

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│                    CoherenceMonitor                      │
│                                                           │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐  │
│  │   Belief     │  │   Policy     │  │  Strategy    │  │
│  │ Contradiction│  │   Conflict   │  │   Conflict   │  │
│  └──────────────┘  └──────────────┘  └──────────────┘  │
│                                                           │
│  ┌──────────────┐  ┌──────────────┐                     │
│  │  Goal Drift  │  │ Behavior Loop│                     │
│  └──────────────┘  └──────────────┘                     │
│                                                           │
│         ↓ Detects Inconsistencies ↓                       │
│                                                           │
│  ┌──────────────────────────────────────┐               │
│  │   Generate Self-Reflection Task      │               │
│  │   → Reasoning Engine                 │               │
│  │   → Curiosity Goal                   │               │
│  └──────────────────────────────────────┘               │
└─────────────────────────────────────────────────────────┘
```

## Data Structures

### Inconsistency
```go
type Inconsistency struct {
    ID          string                 // Unique identifier
    Type        string                 // "belief_contradiction", "policy_conflict", etc.
    Severity    string                 // "low", "medium", "high", "critical"
    Description string                 // Human-readable description
    Details     map[string]interface{} // Context and evidence
    DetectedAt  time.Time              // When detected
    Resolved    bool                   // Resolution status
    Resolution  string                 // Resolution details (if resolved)
}
```

### SelfReflectionTask
```go
type SelfReflectionTask struct {
    ID            string                 // Unique identifier
    Inconsistency string                 // Related inconsistency ID
    Description   string                 // Task description
    Priority      int                    // 1-10, higher is more important
    Status        string                 // "pending", "active", "resolved", "failed"
    CreatedAt     time.Time              // Creation timestamp
    Metadata      map[string]interface{} // Additional context
}
```

## Redis Storage

Inconsistencies are stored in Redis with the following keys:
- `coherence:inconsistencies:{agent_id}` - All inconsistencies
- `coherence:inconsistencies:{agent_id}:{type}` - Inconsistencies by type
- `coherence:reflection_tasks:{agent_id}` - Self-reflection tasks

## Usage

The coherence monitor runs automatically every 5 minutes as part of the FSM engine's background tasks. No manual intervention is required.

### Manual Trigger (Future Enhancement)

You could add an API endpoint or FSM action to trigger coherence checks on demand:

```go
// In FSM engine
inconsistencies, err := e.coherenceMonitor.CheckCoherence()
```

## Example Scenarios

### Scenario 1: Belief Contradiction
**Detected**: Two beliefs in the "machine learning" domain:
- Belief 1: "Neural networks always require large datasets" (confidence: 0.85)
- Belief 2: "Neural networks can work with small datasets" (confidence: 0.75)

**Action**: Generates a self-reflection task to resolve the contradiction, creates a curiosity goal for the reasoning engine.

### Scenario 2: Goal Drift
**Detected**: Goal "Reduce API error rate below 1%" has been active for 26 hours without updates.

**Action**: Flags as inconsistency, suggests reviewing or updating the goal.

### Scenario 3: Behavior Loop
**Detected**: State transition "reason → reason_continue → reason" occurred 7 times in recent history.

**Action**: Flags as potential loop, suggests investigating why reasoning is not progressing.

## Future Enhancements

1. **More sophisticated contradiction detection**: Use LLM-based semantic analysis
2. **Automatic resolution**: Have the reasoning engine automatically resolve simple inconsistencies
3. **Metrics and dashboards**: Track inconsistency rates over time
4. **Configurable thresholds**: Allow tuning of detection sensitivity
5. **Integration with Monitor UI**: Display inconsistencies in the monitoring dashboard

## Benefits

✅ **Cognitive Integrity**: Ensures the system maintains consistent beliefs and goals  
✅ **Early Detection**: Catches problems before they cause larger issues  
✅ **Self-Reflection**: Enables the system to identify and fix its own inconsistencies  
✅ **Cross-System Awareness**: Monitors all three systems (FSM, HDN, Self-Model) together  
✅ **Automated Resolution**: Generates tasks for the reasoning engine to resolve issues  

## Files Modified

- `fsm/coherence_monitor.go` - New file with coherence monitoring implementation
- `fsm/engine.go` - Added coherence monitor initialization and monitoring loop
- `fsm/go.mod` - Added dependency on `agi/self` package

## Testing

To test the coherence monitor:

1. Create conflicting beliefs in the knowledge base
2. Create conflicting goals in Goal Manager
3. Let the system run for 24+ hours with a goal
4. Observe the coherence monitor detecting inconsistencies
5. Check Redis for stored inconsistencies and reflection tasks
6. Verify that curiosity goals are generated for resolution

