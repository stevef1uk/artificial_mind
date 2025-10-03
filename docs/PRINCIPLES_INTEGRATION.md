# FSM Hardcoded Principles Integration

## Overview

The Artificial Mind FSM has **hardcoded principles checking** at critical decision points. This ensures that no action can be taken without explicit approval from the Principles Server.

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                    ARTIFICIAL MIND FSM                          │
│                   (Hardcoded Principles)                       │
└─────────────────────────────────────────────────────────────────┘
                                │
                                ▼
┌─────────────────────────────────────────────────────────────────┐
│                    FSM STATES                                  │
│                                                                 │
│  idle → perceive → learn → summarize → hypothesize → plan      │
│                                                      │         │
│                                                      ▼         │
│  archive ← evaluate ← observe ← act ← decide ◄───────────────┘
│     │                                    │
│     └────────────────────────────────────┘
│
└─────────────────────────────────────────────────────────────────┘
                                │
                                ▼
┌─────────────────────────────────────────────────────────────────┐
│                HARDCODED PRINCIPLES CHECKS                     │
│                                                                 │
│  🔒 MANDATORY CHECK (decide state)                             │
│  ├─ Always calls Principles Server                             │
│  ├─ FSM fails if Principles Server unavailable                │
│  ├─ Stores result in context for transition logic             │
│  └─ Required before any action can be taken                   │
│                                                                 │
│  🔒 PRE-EXECUTION CHECK (act state)                            │
│  ├─ Double-check before actual execution                      │
│  ├─ Even if first check passed, check again                   │
│  ├─ Maximum safety before any action                          │
│  └─ Blocks execution if principles violated                   │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
                                │
                                ▼
┌─────────────────────────────────────────────────────────────────┐
│                    PRINCIPLES SERVER                           │
│                                                                 │
│  POST /action                                                   │
│  ├─ Input: action description + context                        │
│  ├─ Output: allowed/blocked + reason + confidence              │
│  ├─ Integration: Domain-aware safety checking                  │
│  └─ Response: Detailed blocking rules if applicable            │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

## Key Features

### 1. **Hardcoded Safety Points**

- **`decide` state**: MANDATORY principles check before any decision
- **`act` state**: PRE-EXECUTION principles check before execution
- **No bypassing**: Principles checks cannot be disabled or skipped
- **Fail-safe**: FSM fails if Principles Server is unavailable

### 2. **Double Safety**

```
User Input → FSM → decide → 🔒 MANDATORY CHECK → act → 🔒 PRE-EXECUTION CHECK → Execute
                    │                                    │
                    ▼                                    ▼
              Principles Server                    Principles Server
              (First Check)                        (Second Check)
```

### 3. **Context Preservation**

All principles check results are stored in FSM context:
- `principles_allowed`: Boolean result
- `principles_reason`: Explanation from Principles Server
- `principles_confidence`: Confidence level
- `principles_error`: Error if check failed
- `blocked_rules`: Specific rules that blocked the action

### 4. **Transition Logic**

```yaml
decide:
  on:
    allowed: act                    # Principles approved
    blocked: archive                # Principles blocked
    principles_error: fail          # Principles Server error
    needs_more_info: learn          # Need more context

act:
  on:
    execution_finished: observe     # Execution completed
    execution_failed: fail          # Execution failed
    principles_blocked: archive     # Pre-execution check blocked
```

## Implementation Details

### Principles Integration Class

```go
type PrinciplesIntegration struct {
    principlesURL string
    httpClient    *http.Client
}

// Mandatory check before any decision
func (pi *PrinciplesIntegration) MandatoryPrinciplesCheck(action string, context map[string]interface{}) (*PrinciplesCheckResponse, error)

// Double-check before execution
func (pi *PrinciplesIntegration) PreExecutionPrinciplesCheck(action string, context map[string]interface{}) (*PrinciplesCheckResponse, error)

// Domain-aware checking
func (pi *PrinciplesIntegration) DomainAwarePrinciplesCheck(action string, domain string, constraints []string, context map[string]interface{}) (*PrinciplesCheckResponse, error)
```

### FSM Engine Integration

```go
type FSMEngine struct {
    // ... other fields
    principles *PrinciplesIntegration  // Hardcoded integration
}

// Hardcoded principles checking in action execution
func (e *FSMEngine) executeMandatoryPrinciplesChecker(action ActionConfig, event map[string]interface{})
func (e *FSMEngine) executePreExecutionPrinciplesChecker(action ActionConfig, event map[string]interface{})
```

## Example Flow

### 1. User Input: "Generate code for matrix multiplication"

```
FSM State: idle
Event: new_input
Action: "Generate code for matrix multiplication"
```

### 2. Domain Classification

```
FSM State: perceive → learn
Domain: "Math"
Concepts: ["Matrix", "Matrix Multiplication", "Prime Number"]
Confidence: 0.85
```

### 3. Hypothesis Generation

```
FSM State: learn → summarize → hypothesize
Hypothesis: "If we generate matrix multiplication code, we can solve mathematical problems"
Confidence: 0.7
Domain: "Math"
```

### 4. Plan Creation

```
FSM State: hypothesize → plan
Plan: "Create Python code for matrix multiplication with validation"
Steps: ["Generate code", "Add validation", "Test with sample data"]
Expected Value: 0.8
Risk: 0.2
```

### 5. **MANDATORY PRINCIPLES CHECK**

```
FSM State: plan → decide
🔒 MANDATORY PRINCIPLES CHECK - Checking action: "Generate code for matrix multiplication"

POST /action
{
  "action": "Generate code for matrix multiplication",
  "context": {
    "check_type": "mandatory",
    "hardcoded": true,
    "critical_safety": true,
    "domain": "Math",
    "constraints": ["Must follow domain safety principles"]
  }
}

Response:
{
  "allowed": true,
  "reason": "Code generation for mathematical operations is permitted",
  "confidence": 0.9,
  "rule_matches": ["educational_content", "mathematical_computation"]
}

✅ MANDATORY PRINCIPLES CHECK PASSED - Action allowed: Code generation for mathematical operations is permitted
```

### 6. **PRE-EXECUTION PRINCIPLES CHECK**

```
FSM State: decide → act
🔒 PRE-EXECUTION PRINCIPLES CHECK - Double-checking before execution: "Execute matrix multiplication code"

POST /action
{
  "action": "Execute matrix multiplication code",
  "context": {
    "check_type": "pre_execution",
    "double_check": true,
    "final_safety": true,
    "domain": "Math",
    "execution_context": "Docker container with resource limits"
  }
}

Response:
{
  "allowed": true,
  "reason": "Mathematical computation is safe and within resource limits",
  "confidence": 0.95
}

✅ PRE-EXECUTION PRINCIPLES CHECK PASSED - Action allowed: Mathematical computation is safe and within resource limits
```

### 7. Execution and Observation

```
FSM State: act → observe → evaluate
Execution: Code generated and executed successfully
Results: Matrix multiplication working correctly
Domain Knowledge: Updated with success metrics
```

### 8. Archive and Return to Idle

```
FSM State: evaluate → archive → idle
Episode: Saved to Weaviate with domain context
Checkpoint: Created with principles check results
Ready: For next user input
```

## Safety Guarantees

1. **No Action Without Principles Approval**: Every action must pass principles checks
2. **Double Safety**: Two separate principles checks before execution
3. **Fail-Safe Design**: FSM fails if Principles Server is unavailable
4. **Complete Audit Trail**: All decisions and principles results are logged
5. **Domain Awareness**: Principles checks consider domain context and constraints
6. **No Bypassing**: Principles checks are hardcoded and cannot be disabled

## Error Handling

### Principles Server Unavailable
```
❌ MANDATORY PRINCIPLES CHECK FAILED - Cannot reach Principles Server: connection refused
FSM State: decide → fail
Result: FSM stops, no actions can be taken
```

### Action Blocked by Principles
```
❌ MANDATORY PRINCIPLES CHECK FAILED - Action blocked: Data deletion violates safety principles
FSM State: decide → archive
Result: Hypothesis saved, no execution attempted
```

### Pre-Execution Block
```
❌ PRE-EXECUTION PRINCIPLES CHECK FAILED - Action blocked: Resource limits exceeded
FSM State: act → archive
Result: Plan archived, execution prevented
```

## Integration with Existing Systems

- **HDN API**: FSM calls HDN for domain knowledge and execution
- **Principles Server**: Hardcoded integration for all safety checks
- **Neo4j**: Domain knowledge informs principles decisions
- **Redis**: FSM state and context persistence
- **NATS**: Event-driven architecture with principles events
- **Monitor UI**: Real-time visibility into principles decisions

This hardcoded integration ensures that the Artificial Mind can never take actions that violate ethical principles, making it a truly safe and responsible AI system.
