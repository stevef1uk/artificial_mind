# Dynamic HDN-Principles Integration Guide

This guide explains how to integrate HDN (Hierarchical Decision Network) with the Principles API for **dynamic ethical decision making** with LLM-generated tasks.

## Overview

The dynamic integration system handles the real-world scenario where HDN learns new tasks through LLM interaction and needs to check them against ethical principles in real-time.

### Key Components

1. **Task Analyzer** - Extracts ethical context from natural language task descriptions
2. **Dynamic Action Mapper** - Maps LLM-generated tasks to principles API format
3. **LLM Integration** - Provides intelligent task analysis and refinement
4. **Principles API** - Makes final ethical decisions based on rules

## Architecture

```
HDN System → LLM Integration → Task Analyzer → Dynamic Mapper → Principles API
     ↓              ↓              ↓              ↓              ↓
  LLM Task     Task Analysis   Context Extract   Format Convert   Rule Check
  Generation   & Refinement    & Risk Assess     to API Format    & Decision
```

## Quick Start

### 1. Start the Principles API Server

```bash
cd principles
go run main.go
```

### 2. Test the Dynamic Integration

```bash
# Test basic integration
go run examples/main.go basic

# Test dynamic LLM integration
go run examples/main.go dynamic
```

## Dynamic Task Processing

### Step 1: LLM Generates Task

When HDN's LLM generates a new task:

```go
// HDN receives LLM-generated task
llmTask := mapper.LLMTask{
    TaskName:    "retrieve_sample",
    Description: "Go to the laboratory and get the blood sample from the fridge",
    TaskType:    "retrieval",
    Context: map[string]interface{}{
        "target_location": "laboratory",
        "object_type":     "blood_sample",
        "urgency":         "high",
    },
    GeneratedBy: "llm",
}
```

### Step 2: Task Analysis

The system automatically analyzes the task:

```go
// Create dynamic mapper
dynamicMapper := mapper.NewDynamicActionMapper("http://localhost:8080")

// Analyze task
result := dynamicMapper.CheckTask(llmTask)

fmt.Printf("Task: %s\n", result.TaskName)
fmt.Printf("Allowed: %v\n", result.Allowed)
fmt.Printf("Risk Level: %s\n", result.Analysis.RiskLevel)
fmt.Printf("Confidence: %.2f\n", result.Analysis.Confidence)
```

### Step 3: Ethical Context Extraction

The system automatically extracts ethical context:

```json
{
  "action": "retrieve_sample",
  "params": {
    "task_name": "retrieve_sample",
    "description": "Go to the laboratory and get the blood sample from the fridge",
    "location": "laboratory",
    "item": "blood sample"
  },
  "context": {
    "human_harm": false,
    "self_harm": false,
    "human_order": true,
    "stealing": false,
    "damage": false,
    "unauthorized_access": false,
    "risk_level": "low",
    "confidence": 0.8
  }
}
```

## Advanced Features

### 1. Intelligent Task Analysis

The system uses pattern matching and LLM integration for deep analysis:

```go
// Create LLM integration
llmIntegration := llm.NewLLMIntegration("http://localhost:8080")
llmClient := &YourLLMClient{} // Your actual LLM client

// Analyze with LLM enhancement
analysis, err := llmIntegration.AnalyzeTaskWithLLM(
    llmClient, 
    "steal_documents", 
    "Take the classified files from the secure server",
    context,
)
```

### 2. Task Plan Generation

Generate ethical task plans:

```go
goal := "Complete the assigned mission"
context := map[string]interface{}{
    "mission_type": "retrieval",
    "target":       "laboratory",
    "human_order":  true,
}

plan, err := llmIntegration.GenerateEthicalTaskPlan(llmClient, goal, context)
```

### 3. Task Refinement

Automatically refine blocked tasks:

```go
// If task is blocked, try to refine it
if !result.Allowed {
    refinedTask, err := llmIntegration.RefineTaskWithLLM(
        llmClient, 
        blockedTask, 
        result.Reasons,
    )
    
    // Check refined task
    refinedResult := dynamicMapper.CheckTask(refinedTask)
}
```

### 4. Batch Processing

Process multiple tasks efficiently:

```go
tasks := []mapper.LLMTask{
    {TaskName: "scan_area", Description: "..."},
    {TaskName: "move_safely", Description: "..."},
    {TaskName: "access_restricted", Description: "..."},
}

allAllowed, blockedTasks, results := dynamicMapper.ValidateTaskPlan(tasks)
```

## Integration Patterns

### Pattern 1: Pre-execution Validation

```go
func executeHDNTaskWithEthics(task mapper.LLMTask) error {
    // Check task before execution
    result := dynamicMapper.CheckTask(task)
    
    if !result.Allowed {
        return fmt.Errorf("task blocked: %v", result.Reasons)
    }
    
    // Execute the task
    return executeTask(task)
}
```

### Pattern 2: Plan Validation

```go
func validateHDNPlan(plan []mapper.LLMTask) ([]mapper.LLMTask, error) {
    allAllowed, blockedTasks, results := dynamicMapper.ValidateTaskPlan(plan)
    
    if !allAllowed {
        // Try to refine blocked tasks
        refinedPlan := []mapper.LLMTask{}
        for i, result := range results {
            if result.Allowed {
                refinedPlan = append(refinedPlan, plan[i])
            } else {
                // Try refinement or skip
                refinedTask := tryRefineTask(plan[i], result.Reasons)
                if refinedTask != nil {
                    refinedPlan = append(refinedPlan, *refinedTask)
                }
            }
        }
        return refinedPlan, nil
    }
    
    return plan, nil
}
```

### Pattern 3: Real-time Learning Integration

```go
func handleHDNLearning(newTask mapper.LLMTask) {
    // Analyze the new task
    result := dynamicMapper.CheckTask(newTask)
    
    if result.Allowed {
        // Add to HDN's learned tasks
        hdn.AddLearnedTask(newTask)
    } else {
        // Try to refine or find alternative
        refinedTask := llmIntegration.RefineTaskWithLLM(
            hdn.LLMClient, 
            newTask, 
            result.Reasons,
        )
        
        if refinedTask != nil {
            hdn.AddLearnedTask(*refinedTask)
        }
    }
}
```

## Context Mapping

### Automatic Context Extraction

The system automatically extracts context from task descriptions:

| Pattern | Context Field | Example |
|---------|---------------|---------|
| "harm", "hurt", "injure" | `human_harm: true` | "harm the patient" |
| "steal", "take", "grab" | `stealing: true` | "steal the documents" |
| "break", "damage", "destroy" | `damage: true` | "break the equipment" |
| "access", "enter", "hack" | `unauthorized_access: true` | "access the server" |
| "obey", "follow", "execute" | `human_order: true` | "obey the command" |

### Risk Assessment

The system automatically calculates risk levels:

- **Low Risk**: No ethical concerns detected
- **Medium Risk**: Some concerning patterns detected
- **High Risk**: Multiple concerning patterns or high-confidence matches

### Confidence Scoring

- **0.0-0.3**: Low confidence, may need LLM analysis
- **0.4-0.7**: Medium confidence, pattern-based analysis
- **0.8-1.0**: High confidence, clear ethical indicators

## Error Handling

### Network Errors

```go
result := dynamicMapper.CheckTask(task)
if result.Error != "" {
    // Handle network or API errors
    log.Printf("Principles API error: %s", result.Error)
    // Decide whether to proceed or fail
}
```

### Task Refinement Failures

```go
refinedTask, err := llmIntegration.RefineTaskWithLLM(llmClient, task, reasons)
if err != nil {
    // Refinement failed, try alternative approach
    alternativeTask := findAlternativeTask(task)
    if alternativeTask != nil {
        return *alternativeTask, nil
    }
    return task, err
}
```

## Configuration

### Task Analyzer Patterns

You can customize the task analyzer patterns in `internal/analyzer/task_analyzer.go`:

```go
harmPatterns: []*regexp.Regexp{
    regexp.MustCompile(`(?i)(harm|hurt|injure|damage|destroy|kill)`),
    // Add your custom patterns
},
```

### Principles Rules

Configure ethical rules in `config/principles.json`:

```json
[
  {
    "name": "CustomRule",
    "priority": 5,
    "action": "*",
    "condition": "custom_field==true",
    "deny_message": "Custom ethical violation"
  }
]
```

## Performance Considerations

### Caching

- Results are cached in memory or Redis
- Cache key includes task name and parameters
- TTL configurable (default: 1 hour)

### Batch Processing

- Process multiple tasks in parallel
- Use `BatchCheckTasks()` for efficiency
- Consider rate limiting for large batches

### LLM Integration

- LLM calls are expensive, use pattern matching first
- Only call LLM for low-confidence or high-risk tasks
- Cache LLM analysis results

## Testing

### Unit Tests

```bash
go test ./internal/analyzer
go test ./internal/mapper
go test ./internal/llm
```

### Integration Tests

```bash
# Start principles server
go run main.go &

# Run examples
go run examples/main.go dynamic
```

### Load Testing

```go
// Test with many tasks
tasks := generateManyTasks(1000)
results := dynamicMapper.BatchCheckTasks(tasks)
```

## Troubleshooting

### Common Issues

1. **Tasks Always Blocked**: Check context mapping and rule conditions
2. **Low Confidence**: Improve task descriptions or add more patterns
3. **LLM Integration Fails**: Check LLM client implementation
4. **Performance Issues**: Enable caching or reduce batch size

### Debug Mode

Enable detailed logging:

```go
// Add logging to see analysis details
fmt.Printf("Analysis: %+v\n", result.Analysis)
fmt.Printf("Context: %+v\n", result.Analysis.EthicalContext)
```

## Future Enhancements

- **Rule Learning**: Learn new ethical rules from examples
- **Context Learning**: Improve context extraction with ML
- **Multi-language Support**: Support for non-English task descriptions
- **Real-time Updates**: Update rules without restarting
- **Integration with HDN Learning**: Direct integration with HDN's learning system

## Example: Complete HDN Integration

```go
// In your HDN system
type HDNWithEthics struct {
    llmClient       LLMClient
    principlesClient *client.PrinciplesClient
    dynamicMapper   *mapper.DynamicActionMapper
    llmIntegration  *llm.LLMIntegration
}

func (h *HDNWithEthics) ExecuteLearnedTask(taskName, description string) error {
    // Create task from LLM output
    task := mapper.LLMTask{
        TaskName:    taskName,
        Description: description,
        GeneratedBy: "llm",
    }
    
    // Check with principles
    result := h.dynamicMapper.CheckTask(task)
    
    if !result.Allowed {
        // Try to refine
        refinedTask, err := h.llmIntegration.RefineTaskWithLLM(
            h.llmClient, 
            task, 
            result.Reasons,
        )
        if err != nil {
            return fmt.Errorf("task blocked and refinement failed: %v", result.Reasons)
        }
        
        // Check refined task
        refinedResult := h.dynamicMapper.CheckTask(*refinedTask)
        if !refinedResult.Allowed {
            return fmt.Errorf("refined task still blocked: %v", refinedResult.Reasons)
        }
        
        task = *refinedTask
    }
    
    // Execute the task
    return h.executeTask(task)
}
```

This dynamic integration system provides HDN with intelligent, real-time ethical decision making that can handle the complexity of LLM-generated tasks while maintaining safety and ethical compliance.
