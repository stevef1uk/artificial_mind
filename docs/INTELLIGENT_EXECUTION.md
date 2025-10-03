# HDN Intelligent Execution System

## Overview

The HDN Intelligent Execution System is a revolutionary enhancement that enables the system to automatically generate, test, and cache executable code for any task using Large Language Models (LLMs) and Docker containers. This system learns from each interaction and builds a library of reusable capabilities.

## Key Features

### 🧠 Intelligent Code Generation
- **LLM-Powered**: Uses Ollama or other LLM providers to generate executable code
- **Multi-Language Support**: Supports Python, JavaScript, Go, Java, C++, Rust, and more
- **Context-Aware**: Generates code based on task description and context

### 🐳 Docker-Based Validation
- **Safe Execution**: All code runs in isolated Docker containers
- **Automatic Testing**: Generated code is automatically tested before caching
- **Error Handling**: Comprehensive error reporting and debugging information

### 🔄 Self-Improving System
- **Automatic Fixes**: If code fails validation, the LLM attempts to fix it
- **Retry Logic**: Configurable retry attempts with intelligent feedback
- **Persistent Retry Counts**: Retry counts are stored in Redis to prevent infinite retry loops
- **Learning Loop**: System learns from failures and improves over time

### 💾 Intelligent Caching
- **Code Reuse**: Successfully validated code is cached for future use
- **Smart Lookup**: System searches for similar cached capabilities before generating new code
- **Performance**: Subsequent requests for similar tasks are much faster

### 🎯 Dynamic Action Creation
- **Auto-Learning**: System automatically creates dynamic actions for learned capabilities
- **HTN Integration**: Learned capabilities integrate seamlessly with HTN planning
- **Persistence**: Capabilities are stored in Redis for long-term persistence

## Architecture

```
┌─────────────────┐    ┌──────────────────┐    ┌─────────────────┐
│   User Request  │───▶│  Intelligent     │───▶│   LLM Client    │
│                 │    │  Executor        │    │                 │
└─────────────────┘    └──────────────────┘    └─────────────────┘
                                │                        │
                                ▼                        ▼
                       ┌──────────────────┐    ┌─────────────────┐
                       │  Code Storage    │    │  Code Generator │
                       │  (Redis Cache)   │    │                 │
                       └──────────────────┘    └─────────────────┘
                                │                        │
                                ▼                        ▼
                       ┌──────────────────┐    ┌─────────────────┐
                       │  Docker Executor │    │  Validation     │
                       │  (Safe Testing)  │    │  Engine         │
                       └──────────────────┘    └─────────────────┘
```

## Workflow Orchestration & Retry Management

### 🔄 Workflow Orchestrator
The Workflow Orchestrator manages complex multi-step workflows with intelligent retry logic:

- **Persistent Retry Counts**: Retry counts are stored in Redis with keys like `workflow_step_retry:{workflow_id}:{step_id}`
- **Retry Limits**: Each workflow step has a maximum retry count (default: 3 attempts)
- **Deadlock Detection**: System detects and handles workflow deadlocks gracefully
- **Cleanup**: Retry counts are automatically cleared when steps complete or workflows fail

### 🛡️ Retry Loop Prevention
The system prevents infinite retry loops through:

1. **Redis Persistence**: Retry counts survive server restarts
2. **Step-Level Limits**: Each step has individual retry limits
3. **Workflow Cleanup**: Failed workflows clear all retry counts
4. **Deadlock Detection**: Workflows with unmet dependencies are marked as failed

### 📊 Retry Monitoring
Retry activity is logged and can be monitored:
- Step retry events are emitted to the event system
- Retry counts are visible in workflow status
- Failed workflows are properly cleaned up

## API Endpoints

### Intelligent Execution

#### POST `/api/v1/intelligent/execute`
Execute any task intelligently using LLM-generated code.

**Request Body:**
```json
{
  "task_name": "CalculatePrimes",
  "description": "Calculate the first 10 prime numbers",
  "context": {
    "count": "10",
    "input": "10"
  },
  "language": "python",
  "force_regenerate": false,
  "max_retries": 3,
  "timeout": 30
}
```

**Response:**
```json
{
  "success": true,
  "result": "First 10 prime numbers:\n2, 3, 5, 7, 11, 13, 17, 19, 23, 29\nCalculation time: 0.0001 seconds",
  "generated_code": {
    "id": "code_1234567890",
    "task_name": "CalculatePrimes",
    "language": "python",
    "code": "def is_prime(n):...",
    "created_at": "2024-01-01T12:00:00Z"
  },
  "execution_time_ms": 1250,
  "retry_count": 1,
  "used_cached_code": false,
  "validation_steps": [...],
  "new_action": {...}
}
```

#### POST `/api/v1/intelligent/primes`
Specialized endpoint for prime number calculations.

**Request Body:**
```json
{
  "count": 10
}
```

#### GET `/api/v1/intelligent/capabilities`
List all cached capabilities and execution statistics.

**Response:**
```json
{
  "capabilities": [
    {
      "task_name": "CalculatePrimes",
      "language": "python",
      "created_at": "2024-01-01T12:00:00Z",
      "tags": ["intelligent_execution", "validated"]
    }
  ],
  "stats": {
    "total_cached_capabilities": 5,
    "languages": {
      "python": 3,
      "javascript": 2
    },
    "tags": {
      "intelligent_execution": 5,
      "validated": 5
    }
  }
}
```

## Usage Examples

### Basic Usage

```bash
# Calculate prime numbers
curl -X POST http://localhost:8080/api/v1/intelligent/primes \
  -H "Content-Type: application/json" \
  -d '{"count": 10}'

# Calculate Fibonacci sequence
curl -X POST http://localhost:8080/api/v1/intelligent/execute \
  -H "Content-Type: application/json" \
  -d '{
    "task_name": "CalculateFibonacci",
    "description": "Calculate the first 15 Fibonacci numbers",
    "context": {"count": "15"},
    "language": "python"
  }'
```

### Advanced Usage

```bash
# Force regeneration of code
curl -X POST http://localhost:8080/api/v1/intelligent/execute \
  -H "Content-Type: application/json" \
  -d '{
    "task_name": "CalculatePrimes",
    "description": "Calculate prime numbers with different algorithm",
    "context": {"count": "20"},
    "language": "python",
    "force_regenerate": true,
    "max_retries": 5
  }'

# Use different programming language
curl -X POST http://localhost:8080/api/v1/intelligent/execute \
  -H "Content-Type: application/json" \
  -d '{
    "task_name": "CalculateSquares",
    "description": "Calculate squares of numbers 1 to 10",
    "context": {"max": "10"},
    "language": "javascript"
  }'
```

## Configuration

### Environment Variables

```bash
# Redis configuration
REDIS_URL=localhost:6379

# LLM configuration
LLM_PROVIDER=local  # or "openai", "anthropic"
LLM_API_KEY=your_api_key_here
OLLAMA_ENDPOINT=localhost:11434

# Docker configuration
DOCKER_TIMEOUT=30
DOCKER_MEMORY_LIMIT=512m
```

### Server Configuration

The intelligent execution system is automatically initialized when starting the HDN server:

```bash
# Start the server
go run . -mode=server

# The system will automatically:
# 1. Initialize Redis connections
# 2. Set up LLM clients
# 3. Configure Docker executor
# 4. Enable intelligent execution endpoints
```

## Workflow

### 1. Task Request
When a task is requested, the system first checks if it has cached code for similar tasks.

### 2. Code Generation
If no cached code exists, the LLM generates executable code based on:
- Task name and description
- Context parameters
- Programming language preference
- Best practices and error handling

### 3. Validation
Generated code is tested in a Docker container:
- Code is written to a temporary file
- Docker container is created with appropriate runtime
- Code is executed with timeout protection
- Output and errors are captured

### 4. Error Handling
If validation fails:
- Error details are analyzed
- LLM attempts to fix the code
- Fixed code is re-validated
- Process repeats up to max_retries

### 5. Caching
Successful code is cached with:
- Task metadata
- Execution context
- Performance metrics
- Searchable tags

### 6. Action Creation
A dynamic action is created for the learned capability:
- Integrates with HTN planning
- Can be reused in future plans
- Persists across server restarts

## Benefits

### For Developers
- **No Manual Coding**: System generates code automatically
- **Multi-Language Support**: Works with any programming language
- **Safe Execution**: All code runs in isolated containers
- **Learning System**: Gets better over time

### For Users
- **Natural Language**: Describe tasks in plain English
- **Fast Execution**: Cached code executes quickly
- **Reliable Results**: Validated code is guaranteed to work
- **Extensible**: System learns new capabilities automatically

### For Organizations
- **Cost Effective**: Reduces manual development time
- **Scalable**: Handles increasing complexity automatically
- **Maintainable**: Self-improving system requires less maintenance
- **Secure**: Sandboxed execution prevents system damage

## Monitoring and Debugging

### Execution Statistics
```bash
# View execution statistics
curl -X GET http://localhost:8080/api/v1/intelligent/capabilities

# Check specific capability
curl -X GET http://localhost:8080/api/v1/actions/intelligent/{action_id}
```

### Logging
The system provides detailed logging at multiple levels:
- **INFO**: Normal execution flow
- **DEBUG**: Detailed step-by-step information
- **WARN**: Non-critical issues
- **ERROR**: Execution failures

### Validation Steps
Each execution includes detailed validation steps:
- Code generation success/failure
- Docker execution results
- Error messages and fixes
- Performance metrics

## Future Enhancements

### Planned Features
- **Multi-Model Support**: Support for multiple LLM providers
- **Code Optimization**: Automatic code performance optimization
- **Dependency Management**: Automatic dependency resolution
- **Testing Framework**: Built-in unit testing for generated code
- **Version Control**: Code versioning and rollback capabilities

### Integration Opportunities
- **CI/CD Pipelines**: Integration with continuous integration
- **Monitoring Systems**: Integration with APM tools
- **Documentation**: Automatic documentation generation
- **Code Review**: AI-powered code review and suggestions

## Troubleshooting

### Common Issues

#### Redis Connection Failed
```bash
# Check Redis status
redis-cli ping

# Start Redis if needed
redis-server
```

#### Docker Execution Failed
```bash
# Check Docker status
docker ps

# Check Docker logs
docker logs {container_id}
```

#### LLM Generation Failed
```bash
# Check Ollama status
curl http://localhost:11434/api/tags

# Check LLM configuration
curl -X POST http://localhost:8080/api/v1/intelligent/execute \
  -H "Content-Type: application/json" \
  -d '{"task_name": "TestLLM", "description": "Simple test", "language": "python"}'
```

### Performance Optimization

#### Cache Management
```bash
# Clear all cached capabilities
redis-cli FLUSHDB

# View cache statistics
redis-cli INFO memory
```

#### Docker Optimization
```bash
# Clean up unused containers
docker system prune

# Monitor resource usage
docker stats
```

## Conclusion

The HDN Intelligent Execution System represents a significant advancement in automated task execution. By combining LLM-powered code generation, Docker-based validation, and intelligent caching, it creates a self-improving system that learns from each interaction and builds a comprehensive library of reusable capabilities.

This system enables users to describe tasks in natural language and have them executed automatically, while developers benefit from reduced manual coding and a more maintainable system architecture.
