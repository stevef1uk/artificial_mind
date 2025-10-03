# HDN-Principles Integration Guide

This guide explains how to integrate the HDN (Hierarchical Decision Network) system with the Principles API for ethical decision making.

## Overview

The integration allows HDN to check actions against ethical principles before execution. The system consists of:

1. **Principles API Server** - Runs ethical rules and makes allow/deny decisions
2. **Action Mapper** - Converts HDN actions to principles API format
3. **Principles Client** - Provides easy interface for HDN to call principles API

## Architecture

```
HDN System → Principles Client → Principles API → Ethical Rules Engine
     ↓              ↓                    ↓              ↓
  Action      HTTP Request         JSON Request    Rule Evaluation
  Decision    to API               Processing      (JSON Rules)
```

## Quick Start

### 1. Start the Principles API Server

```bash
cd principles
go run main.go
```

The server will start on `http://localhost:8080`

### 2. Use the Principles Client in HDN

```go
package main

import (
    "principles/internal/client"
)

func main() {
    // Create principles client
    principlesClient := client.NewPrinciplesClient("http://localhost:8080")
    
    // Define your action
    action := "move_robot"
    params := map[string]interface{}{
        "location": "lab",
        "speed": "fast",
    }
    context := map[string]interface{}{
        "human_harm": false,
        "human_order": true,
        "self_harm": false,
    }
    
    // Check if action is allowed
    allowed, reasons, err := principlesClient.IsActionAllowed(action, params, context)
    if err != nil {
        log.Fatalf("Error: %v", err)
    }
    
    if !allowed {
        fmt.Printf("Action blocked: %v\n", reasons)
        return
    }
    
    // Execute your action
    fmt.Println("Action allowed, proceeding...")
}
```

## Integration Patterns

### Pattern 1: Pre-execution Check

Check if an action is allowed before executing it:

```go
func executeActionWithEthicsCheck(action string, params, context map[string]interface{}) error {
    allowed, reasons, err := principlesClient.IsActionAllowed(action, params, context)
    if err != nil {
        return err
    }
    
    if !allowed {
        return fmt.Errorf("action blocked: %v", reasons)
    }
    
    // Execute the actual action
    return executeAction(action, params)
}
```

### Pattern 2: Integrated Execution

Use the built-in execution with ethics checking:

```go
func executeActionSafely(action string, params, context map[string]interface{}) (string, error) {
    executor := func(params map[string]interface{}) string {
        // Your actual action implementation
        return "Action completed successfully"
    }
    
    result, reasons, err := principlesClient.ExecuteActionIfAllowed(action, params, context, executor)
    if err != nil {
        return "", err
    }
    
    if len(reasons) > 0 {
        return "", fmt.Errorf("action blocked: %v", reasons)
    }
    
    return result, nil
}
```

### Pattern 3: HDN Action Mapping

Convert HDN's action structure to principles format:

```go
func convertHDNAction(hdnAction HDNAction) (string, map[string]interface{}, map[string]interface{}) {
    action := hdnAction.TaskName
    
    // Convert context
    context := make(map[string]interface{})
    for k, v := range hdnAction.Context {
        context[k] = v
    }
    
    // Add state information
    for k, v := range hdnAction.State {
        context["state_"+k] = v
    }
    
    // Create parameters
    params := make(map[string]interface{})
    params["task_name"] = hdnAction.TaskName
    params["task_type"] = hdnAction.TaskType
    params["description"] = hdnAction.Description
    
    return action, params, context
}
```

## Context Mapping

The principles system expects specific context fields for ethical evaluation:

### Required Context Fields

- `human_harm` (bool) - Will this action harm a human?
- `human_order` (bool) - Is this action in response to a human order?
- `self_harm` (bool) - Will this action harm the agent itself?

### Optional Context Fields

- `urgency` (string) - How urgent is this action?
- `task_type` (string) - Type of task being performed
- `description` (string) - Human-readable description
- `state_*` (bool) - Any state variables from HDN

## Example Ethical Rules

The principles system uses JSON-based rules. Example rules in `config/principles.json`:

```json
[
  {
    "name": "FirstLaw",
    "priority": 1,
    "action": "*",
    "condition": "human_harm==true",
    "deny_message": "Action would harm a human (First Law)"
  },
  {
    "name": "SecondLaw",
    "priority": 2,
    "action": "*",
    "condition": "human_order==true && human_harm==true",
    "deny_message": "Cannot obey order that harms a human (Second Law)"
  },
  {
    "name": "NoStealing",
    "priority": 4,
    "action": "steal",
    "condition": "",
    "deny_message": "Stealing violates societal norms"
  }
]
```

## Error Handling

The integration provides comprehensive error handling:

```go
allowed, reasons, err := principlesClient.IsActionAllowed(action, params, context)
if err != nil {
    // Network or API error
    log.Printf("Principles API error: %v", err)
    // Decide whether to proceed or fail
}

if !allowed {
    // Action is ethically blocked
    log.Printf("Action blocked: %v", reasons)
    // Handle blocked action (e.g., find alternative)
}
```

## Testing

Test the integration:

```bash
# Start principles server
cd principles
go run main.go &

# Test with example
go run examples/hdn_integration.go
```

## Configuration

### Principles API Configuration

- **Port**: 8080 (configurable in main.go)
- **Redis**: Optional (falls back to in-memory storage)
- **Rules**: Loaded from `config/principles.json`

### HDN Integration Configuration

- **Principles API URL**: `http://localhost:8080`
- **Timeout**: 10 seconds
- **Retry Logic**: Not implemented (can be added)

## Advanced Usage

### Custom Rule Conditions

The principles system supports complex conditions:

```json
{
  "condition": "human_harm==false && urgency==high && task_type==emergency"
}
```

### Dynamic Rule Loading

Rules can be reloaded without restarting the server by modifying `config/principles.json`.

### Caching

The principles system caches results for performance (Redis or in-memory).

## Troubleshooting

### Common Issues

1. **Connection Refused**: Ensure principles server is running
2. **Action Always Blocked**: Check context mapping and rule conditions
3. **Timeout Errors**: Increase client timeout or check network connectivity

### Debug Mode

Enable debug logging by modifying the principles client timeout or adding logging to see the actual requests/responses.

## Future Enhancements

- Rule versioning and rollback
- Real-time rule updates via API
- Integration with HDN's learning system
- Performance metrics and monitoring
- Rule conflict resolution
