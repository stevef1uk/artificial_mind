# API Refactoring Plan

## Current Problem
The `api.go` file has grown to **4,614 lines**, making it extremely difficult to:
- Navigate and understand
- Maintain and debug
- Add new features
- Test individual components
- Collaborate on different parts

## Proposed Solution

### 1. Break into Handler Modules
Create separate handler files for different functional areas:

```
hdn/
├── handlers/
│   ├── core.go              # Base handler functionality (~50 lines)
│   ├── health.go            # Health endpoints (~50 lines)
│   ├── task.go              # Task execution (~200 lines)
│   ├── learning.go          # Learning endpoints (~200 lines)
│   ├── domain.go            # Domain management (~200 lines)
│   ├── project.go           # Project management (~300 lines)
│   ├── workflow.go          # Workflow management (~400 lines)
│   ├── memory.go            # Memory management (~300 lines)
│   ├── tools.go             # Tool management (~250 lines)
│   ├── concepts.go          # Knowledge concepts (~200 lines)
│   └── docker.go            # Docker execution (~150 lines)
├── api.go                   # Main API server (~400 lines)
└── conversational/          # Conversational AI layer
    ├── conversational_layer.go
    ├── intent_parser.go
    ├── reasoning_trace.go
    ├── nlg_generator.go
    ├── conversation_memory.go
    ├── api.go
    └── interfaces.go
```

### 2. Benefits

| Aspect | Before | After | Improvement |
|--------|--------|-------|-------------|
| Main file size | 4,614 lines | ~400 lines | 91% reduction |
| Number of files | 1 monolithic | 12 focused | Better organization |
| Maintainability | Very difficult | Easy | Much better |
| Testability | Hard to test | Easy to test | Much better |
| Readability | Overwhelming | Clear | Much better |
| Collaboration | Conflicts | Parallel work | Much better |

### 3. Implementation Strategy

#### Phase 1: Create Handler Structure
- [x] Create `handlers/` directory
- [x] Create base handler with common functionality
- [x] Create individual handler modules
- [x] Define handler interfaces

#### Phase 2: Extract Handlers
- [ ] Extract health endpoints to `handlers/health.go`
- [ ] Extract task endpoints to `handlers/task.go`
- [ ] Extract learning endpoints to `handlers/learning.go`
- [ ] Extract domain endpoints to `handlers/domain.go`
- [ ] Extract project endpoints to `handlers/project.go`
- [ ] Extract workflow endpoints to `handlers/workflow.go`
- [ ] Extract memory endpoints to `handlers/memory.go`
- [ ] Extract tool endpoints to `handlers/tools.go`
- [ ] Extract concept endpoints to `handlers/concepts.go`
- [ ] Extract docker endpoints to `handlers/docker.go`

#### Phase 3: Refactor Main API
- [ ] Create new `api.go` with handler delegation
- [ ] Remove handler functions from main file
- [ ] Update route registration
- [ ] Test all endpoints

#### Phase 4: Cleanup
- [ ] Remove unused code
- [ ] Update documentation
- [ ] Run comprehensive tests

### 4. Handler Module Pattern

Each handler module follows this pattern:

```go
package handlers

import (
    "encoding/json"
    "net/http"
)

type MyHandler struct {
    BaseHandler
}

func NewMyHandler(server *APIServer) *MyHandler {
    return &MyHandler{
        BaseHandler: BaseHandler{Server: server},
    }
}

func (h *MyHandler) RegisterRoutes(router interface{}) {
    // Register specific routes
}

func (h *MyHandler) HandleMyEndpoint(w http.ResponseWriter, r *http.Request) {
    // Handle specific endpoint
    h.writeSuccessResponse(w, data)
}
```

### 5. Main API Pattern

The main API file becomes a coordinator:

```go
type APIServer struct {
    // Core dependencies
    handlers map[string]HandlerGroup
    // ... other fields
}

func (s *APIServer) setupRoutes() {
    // Register all handler routes
    for _, handler := range s.handlers {
        handler.RegisterRoutes(s.router)
    }
}

func (s *APIServer) handleMyEndpoint(w http.ResponseWriter, r *http.Request) {
    if handler, ok := s.handlers["my"].(*MyHandler); ok {
        handler.HandleMyEndpoint(w, r)
    }
}
```

### 6. Migration Steps

1. **Create handler modules** (in progress)
2. **Extract functions** from original `api.go` to appropriate handlers
3. **Update main API** to use handlers
4. **Test thoroughly** to ensure no functionality is lost
5. **Replace original** `api.go` with refactored version

### 7. Testing Strategy

- Unit tests for each handler module
- Integration tests for the main API
- End-to-end tests for critical workflows
- Performance tests to ensure no regression

### 8. Rollback Plan

- Keep original `api.go` as `api_legacy.go`
- Use feature flags to switch between versions
- Gradual migration of endpoints
- Comprehensive testing at each step

## Conclusion

This refactoring will transform a 4,614-line monolithic file into a well-organized, maintainable structure with 12 focused modules. The main API file will be reduced by 91% while improving maintainability, testability, and collaboration.

The conversational AI layer is already implemented and ready to be integrated into this new structure.
