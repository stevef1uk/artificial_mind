# AGI System API Reference

This document provides comprehensive API reference for the AGI system components.

## Table of Contents

- [Principles API (Port 8080)](#principles-api-port-8080)
- [HDN API (Port 8081)](#hdn-api-port-8081)
- [Monitor UI (Port 8082)](#monitor-ui-port-8082)
- [Available Tools](#available-tools)
- [Response Formats](#response-formats)
- [Error Handling](#error-handling)

## Principles API (Port 8080)

### Check Action Ethics
```
POST /action
```
Check if an action is ethically allowed.

**Request Body:**
```json
{
  "action": "steal",
  "context": {
    "target": "money",
    "amount": 100
  }
}
```

**Response:**
```json
{
  "allowed": false,
  "reason": "Action would harm a human (First Law)"
}
```

## HDN API (Port 8081)

### Health Check
```
GET /health
```
Returns server status and version information.

**Response:**
```json
{
  "status": "healthy",
  "timestamp": "2025-09-27T22:41:32+02:00",
  "version": "1.0.0"
}
```

### Natural Language Processing

#### Interpret and Execute (RECOMMENDED)
```
POST /api/v1/interpret/execute
```
**RECOMMENDED** - Interpret and execute natural language commands. This is the most user-friendly way to interact with the system.

**Request Body:**
```json
{
  "input": "Can you scrape https://example.com and show me the content?"
}
```

**Response:**
```json
{
  "success": true,
  "interpretation": {
    "success": true,
    "tasks": [],
    "message": "Tool executed successfully",
    "session_id": "session_1234567890",
    "interpreted_at": "2025-09-27T22:45:08.682272282+02:00"
  },
  "execution_plan": [
    {
      "task": {
        "task_name": "Tool Execution",
        "description": "",
        "context": null,
        "language": "",
        "force_regenerate": false,
        "max_retries": 0,
        "timeout": 0,
        "is_multi_step": false,
        "original_input": ""
      },
      "success": true,
      "result": "map[body:<!DOCTYPE html>...]",
      "error": "",
      "executed_at": "0001-01-01T00:00:00Z"
    }
  ],
  "message": "Successfully interpreted and executed 1 task(s)"
}
```

#### Chat Interface
```
POST /api/v1/chat/text
```
Simple text-based chat interface.

**Request Body:**
```json
{
  "message": "What tools do you have available?",
  "session_id": "test_session"
}
```

**Response:**
```
I have access to 12 different tools including:
- HTTP GET requests
- HTML scraping
- File operations
- Shell execution
- Docker management
- Code generation
- JSON parsing
- Text search
- And more!

What specific tool would you like me to use?
```

### Docker Code Execution (RECOMMENDED)

#### Execute Code
```
POST /api/v1/docker/execute
```
**RECOMMENDED** - Execute code in Docker containers. Fast and reliable for code execution.

**Request Body:**
```json
{
  "code": "def factorial(n):\n    if n <= 1:\n        return 1\n    return n * factorial(n-1)\n\nprint(\"Factorial of 5:\", factorial(5))",
  "language": "python"
}
```

**Response:**
```json
{
  "success": true,
  "output": "Factorial of 5: 120\nScanning for PDFs...\nFound 0 PDF(s) in working dir\n...",
  "exit_code": 0,
  "execution_time_ms": 459,
  "container_id": "code-executor-1234567890",
  "files": {
    "code.py": "base64_encoded_content",
    "output.txt": "base64_encoded_output"
  }
}
```

#### Calculate Primes
```
POST /api/v1/docker/primes
```
Calculate prime numbers via Docker execution.

**Request Body:**
```json
{
  "limit": 100
}
```

### Task Management

#### Execute Task
```
POST /api/v1/task/execute
```
Execute a task with optional state and context.

**Request Body:**
```json
{
  "task_name": "ScrapeWebsite",
  "context": {
    "url": "https://example.com"
  }
}
```

#### Plan Task
```
POST /api/v1/task/plan
```
Generate a plan for a task without executing it.

#### Learn New Method
```
POST /api/v1/learn
```
Learn a new method using the specified approach.

### Intelligent Execution

#### Intelligent Execute
```
POST /api/v1/intelligent/execute
```
Intelligent execution with file generation (120s timeout).

**Request Body:**
```json
{
  "task_name": "CreateHelloWorld",
  "description": "Create a simple Python script that prints hello world",
  "language": "python"
}
```

#### Calculate Primes (Intelligent)
```
POST /api/v1/intelligent/primes
```
Calculate prime numbers via intelligent execution.

#### List Capabilities
```
GET /api/v1/intelligent/capabilities
```
List cached intelligent capabilities.

### Hierarchical Planning

#### Hierarchical Execute
```
POST /api/v1/hierarchical/execute
```
Asynchronous task execution (returns workflow ID).

**Request Body:**
```json
{
  "task": "CreateHelloWorld",
  "description": "Create a simple Python script that prints hello world"
}
```

**Response:**
```json
{
  "success": true,
  "workflow_id": "intelligent_1759005857295103193",
  "message": "Accepted for asynchronous execution"
}
```

#### Get Workflow Status
```
GET /api/v1/hierarchical/workflow/{id}/status
```
Check workflow execution status.

### Tools & Utilities

#### List Tools
```
GET /api/v1/tools
```
List available tools (12 tools including HTTP GET, HTML scraping, file operations, etc.).

**Response:**
```json
{
  "tools": [
    {
      "id": "tool_http_get",
      "name": "HTTP GET",
      "description": "Fetch URL",
      "input_schema": {
        "url": "string"
      },
      "output_schema": {
        "body": "string",
        "status": "int"
      },
      "permissions": ["net:read"],
      "safety_level": "low",
      "created_by": "system",
      "created_at": "2025-09-27T18:40:47.368963905Z"
    }
  ]
}
```

#### Execute Tool
```
POST /api/v1/tools/execute
```
Execute specific tools.

**Request Body:**
```json
{
  "tool_id": "tool_http_get",
  "input": {
    "url": "https://example.com"
  }
}
```

#### Register Tool
```
POST /api/v1/tools
```
Register new tools.

### State Management

#### Get State
```
GET /api/v1/state
```
Get the current world state.

#### Update State
```
PUT /api/v1/state
```
Update the world state.

#### Get Working Memory
```
GET /api/v1/state/session/{id}/working_memory
```
Get working memory for session.

### Domain Management

#### Get Domain
```
GET /api/v1/domain
```
Retrieve the current domain definition.

#### Update Domain
```
PUT /api/v1/domain
```
Update the domain definition.

#### Save Domain
```
POST /api/v1/domain/save
```
Save the current domain to file.

## Monitor UI (Port 8082)

### Dashboard
```
GET /
```
Main dashboard interface.

### Chat Interface
```
GET /chat
```
Chat interface with AI.

### Chat API
```
POST /api/chat
```
Chat API endpoint.

**Request Body:**
```json
{
  "message": "What tools do you have available?",
  "session_id": "test_session"
}
```

### Natural Language Processing
```
POST /api/interpret
```
Natural language interpretation.

```
POST /api/interpret/execute
```
Natural language interpretation and execution.

## Available Tools

The system includes 12 built-in tools:

1. **HTTP GET** (`tool_http_get`) - Fetch URLs
2. **HTML Scraper** (`tool_html_scraper`) - Parse HTML and extract content
3. **File Reader** (`tool_file_read`) - Read files
4. **File Writer** (`tool_file_write`) - Write files
5. **List Directory** (`tool_ls`) - List directory contents
6. **Shell Exec** (`tool_exec`) - Run shell commands (sandboxed)
7. **Docker List** (`tool_docker_list`) - List Docker entities
8. **Codegen** (`tool_codegen`) - Generate code via LLM
9. **Docker Build** (`tool_docker_build`) - Build Docker images
10. **Register Tool** (`tool_register`) - Register tool metadata
11. **JSON Parse** (`tool_json_parse`) - Parse JSON
12. **Text Search** (`tool_text_search`) - Search text

## Response Formats

### Success Response
```json
{
  "success": true,
  "data": { ... },
  "message": "Operation completed successfully"
}
```

### Error Response
```json
{
  "success": false,
  "error": "Error message",
  "code": "ERROR_CODE"
}
```

### Task Execution Response
```json
{
  "success": true,
  "plan": ["Task1", "Task2", "Task3"],
  "message": "Task executed successfully",
  "new_state": {
    "task1_completed": true,
    "task2_completed": true,
    "task3_completed": true
  }
}
```

## Error Handling

### Common Error Codes

- `400` - Bad Request (invalid JSON, missing required fields)
- `404` - Not Found (endpoint or resource not found)
- `429` - Too Many Requests (rate limiting)
- `500` - Internal Server Error
- `502` - Bad Gateway (service unavailable)

### Error Response Format
```json
{
  "error": "Error message",
  "code": "ERROR_CODE",
  "details": "Additional error details"
}
```

## Rate Limiting

Some endpoints have rate limiting:
- Intelligent execution: Limited concurrent executions
- Tool execution: May have timeouts for long-running operations

## Authentication

Currently, the API does not require authentication. This may change in future versions.

## CORS

The API supports CORS for web applications. All origins are currently allowed.

## Examples

### Web Scraping
```bash
curl -X POST http://localhost:8081/api/v1/interpret/execute \
  -H "Content-Type: application/json" \
  -d '{
    "input": "Can you scrape https://example.com and show me the content?"
  }'
```

### Code Execution
```bash
curl -X POST http://localhost:8081/api/v1/docker/execute \
  -H "Content-Type: application/json" \
  -d '{
    "code": "print(\"Hello, World!\")",
    "language": "python"
  }'
```

### Chat Interface
```bash
curl -X POST http://localhost:8081/api/v1/chat/text \
  -H "Content-Type: application/json" \
  -d '{
    "message": "What can you help me with?",
    "session_id": "test_session"
  }'
```
