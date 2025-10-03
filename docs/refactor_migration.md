# API Refactoring Migration Guide

## Overview

The original `api.go` file has grown to over 4,600 lines, making it difficult to maintain and understand. This refactoring breaks it into logical modules for better organization and maintainability.

## New Structure

```
hdn/
├── api.go                    # Original (4,600+ lines) - TO BE REPLACED
├── api_refactored.go         # New main API file (~400 lines)
├── handlers/                 # Handler modules
│   ├── core.go              # Base handler functionality
│   ├── health.go            # Health check endpoints
│   ├── task.go              # Task execution endpoints
│   ├── learning.go          # Learning endpoints
│   ├── domain.go            # Domain management endpoints
│   ├── project.go           # Project management endpoints
│   ├── workflow.go          # Workflow management endpoints
│   ├── memory.go            # Memory management endpoints
│   ├── tools.go             # Tool management endpoints
│   └── concepts.go          # Knowledge concepts endpoints
└── conversational/          # Conversational AI layer
    ├── conversational_layer.go
    ├── intent_parser.go
    ├── reasoning_trace.go
    ├── nlg_generator.go
    ├── conversation_memory.go
    ├── api.go
    └── interfaces.go
```

## Migration Steps

### Phase 1: Create Handler Modules
- [x] Create `handlers/core.go` with base functionality
- [x] Create `handlers/health.go` for health endpoints
- [x] Create `handlers/task.go` for task execution
- [x] Create `handlers/learning.go` for learning endpoints
- [x] Create `handlers/domain.go` for domain management
- [ ] Create `handlers/project.go` for project management
- [ ] Create `handlers/workflow.go` for workflow management
- [ ] Create `handlers/memory.go` for memory management
- [ ] Create `handlers/tools.go` for tool management
- [ ] Create `handlers/concepts.go` for knowledge concepts

### Phase 2: Create Refactored API
- [x] Create `api_refactored.go` with new structure
- [ ] Move remaining handler functions to appropriate modules
- [ ] Update server initialization logic
- [ ] Test all endpoints

### Phase 3: Replace Original API
- [ ] Backup original `api.go` as `api_legacy.go`
- [ ] Rename `api_refactored.go` to `api.go`
- [ ] Update imports and dependencies
- [ ] Run comprehensive tests

## Benefits of Refactoring

1. **Maintainability**: Each handler module focuses on a specific domain
2. **Readability**: Smaller files are easier to understand and navigate
3. **Testability**: Individual handlers can be tested in isolation
4. **Scalability**: New handlers can be added without modifying core files
5. **Separation of Concerns**: Each module has a single responsibility

## Handler Module Guidelines

Each handler module should:

1. **Implement HandlerGroup interface**:
   ```go
   type HandlerGroup interface {
       RegisterRoutes(router interface{})
   }
   ```

2. **Embed BaseHandler**:
   ```go
   type MyHandler struct {
       BaseHandler
   }
   ```

3. **Provide common methods**:
   - `writeJSONResponse()`
   - `writeErrorResponse()`
   - `writeSuccessResponse()`

4. **Keep handlers focused** on a single domain (e.g., tasks, learning, domains)

## Next Steps

1. Complete remaining handler modules
2. Move all handler functions from original `api.go` to appropriate modules
3. Update the refactored API to use all handlers
4. Test thoroughly
5. Replace original API

## File Size Comparison

| File | Lines | Purpose |
|------|-------|---------|
| `api.go` (original) | 4,614 | Monolithic API server |
| `api_refactored.go` | ~400 | Main API coordination |
| `handlers/core.go` | ~50 | Base handler functionality |
| `handlers/health.go` | ~50 | Health endpoints |
| `handlers/task.go` | ~150 | Task execution |
| `handlers/learning.go` | ~200 | Learning endpoints |
| `handlers/domain.go` | ~200 | Domain management |
| **Total** | **~1,050** | **Refactored structure** |

**Reduction**: ~77% fewer lines in main files, better organization
