# HTN Planner with API and Learning

This is a Hierarchical Task Network (HTN) planner that has been enhanced with:
- REST API for external control
- LLM integration for learning new methods
- MCP (Model Context Protocol) integration for tool calling
- Support for different task types (primitive, LLM, MCP, method)

## Features

- **HTN Planning**: Hierarchical task decomposition and planning
- **Learning**: Automatic learning of new methods when tasks fail
- **LLM Integration**: Use Large Language Models to generate new methods
- **MCP Integration**: Use Model Context Protocol to discover and use tools
- **REST API**: External control via HTTP endpoints
- **Multiple Task Types**: Support for primitive actions, LLM tasks, MCP tools, and composite methods

## Quick Start

### Prerequisites

- Go 1.21 or later
- (Optional) OpenAI API key for LLM integration
- (Optional) MCP server for tool integration

### Installation

```bash
# Clone and navigate to the directory
cd hdn

# Install dependencies
go mod tidy

# Run the API server
go run . -mode=server

# Or run the original CLI version
go run . -mode=cli
```

### Configuration

Create a `config.json` file:

```json
{
  "llm_provider": "openai",
  "llm_api_key": "your-openai-api-key",
  "mcp_endpoint": "http://localhost:3000/mcp",
  "settings": {
    "model": "gpt-3.5-turbo",
    "temperature": "0.7",
    "max_tokens": "1000"
  },
  "server": {
    "port": 8080,
    "host": "localhost"
  }
}
```

## API Endpoints

### Health Check
```
GET /health
```
Returns server status and version information.

### Core Task Management
```
POST /api/v1/task/execute
```
Execute a task with optional state and context.

```
POST /api/v1/task/plan
```
Generate a plan for a task without executing it.

```
POST /api/v1/learn
```
Learn a new method using the specified approach.

### Natural Language Processing (RECOMMENDED)
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

### Docker Code Execution (RECOMMENDED)
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
  "output": "Factorial of 5: 120\n...",
  "exit_code": 0,
  "execution_time_ms": 459,
  "container_id": "code-executor-1234567890",
  "files": {
    "code.py": "base64_encoded_content",
    "output.txt": "base64_encoded_output"
  }
}
```

### Intelligent Execution
```
POST /api/v1/intelligent/execute
```
Intelligent execution with file generation (120s timeout).

```
POST /api/v1/intelligent/primes
```
Calculate prime numbers via intelligent execution.

```
GET /api/v1/intelligent/capabilities
```
List cached intelligent capabilities.

### Hierarchical Planning
```
POST /api/v1/hierarchical/execute
```
Asynchronous task execution (returns workflow ID).

```
GET /api/v1/hierarchical/workflow/{id}/status
```
Check workflow execution status.

### Tools & Utilities
```
GET /api/v1/tools
```
List available tools (12 tools including HTTP GET, HTML scraping, file operations, etc.).

```
POST /api/v1/tools/execute
```
Execute specific tools.

```
POST /api/v1/tools
```
Register new tools.

### State Management
```
GET /api/v1/state
```
Get the current world state.

```
PUT /api/v1/state
```
Update the world state.

```
GET /api/v1/state/session/{id}/working_memory
```
Get working memory for session.

### Domain Management
```
GET /api/v1/domain
```
Retrieve the current domain definition.

```
PUT /api/v1/domain
```
Update the domain definition.

```
POST /api/v1/domain/save
```
Save the current domain to file.

## Task Types

### Primitive Tasks
Basic actions that can be executed directly. Defined in the `actions` section of the domain.

### LLM Tasks
Tasks that are executed by calling a Large Language Model. The LLM receives a prompt and context to perform the task.

### MCP Tasks
Tasks that use Model Context Protocol tools. These can interact with external systems and services.

### Method Tasks
Composite tasks that decompose into subtasks. These can be learned automatically or defined manually.

## Domain Format

The domain is defined in JSON format with methods and actions:

```json
{
  "methods": [
    {
      "task": "DeliverReport",
      "preconditions": ["not report_submitted"],
      "subtasks": ["WriteDraft", "GetReview", "SubmitFinal"],
      "task_type": "method",
      "description": "Complete report delivery process"
    }
  ],
  "actions": [
    {
      "task": "WriteDraft",
      "preconditions": ["not draft_written"],
      "effects": ["draft_written"],
      "task_type": "primitive",
      "description": "Write the initial draft"
    }
  ],
  "config": {
    "llm_provider": "openai",
    "llm_api_key": "your-key",
    "mcp_endpoint": "http://localhost:3000/mcp"
  }
}
```

## Examples

### Web Scraping (Natural Language)
```bash
# Scrape a website using natural language - RECOMMENDED
curl -X POST http://localhost:8081/api/v1/interpret/execute \
  -H "Content-Type: application/json" \
  -d '{
    "input": "Can you scrape https://emotion-service-api.sjfisher.com and show me the content?"
  }'
```

### Code Creation and Execution
```bash
# Execute Python code in Docker - RECOMMENDED
curl -X POST http://localhost:8081/api/v1/docker/execute \
  -H "Content-Type: application/json" \
  -d '{
    "code": "def factorial(n):\n    if n <= 1:\n        return 1\n    return n * factorial(n-1)\n\nprint(\"Factorial of 5:\", factorial(5))",
    "language": "python"
  }'
```

### Chat Interface
```bash
# Use the chat interface
curl -X POST http://localhost:8081/api/v1/chat/text \
  -H "Content-Type: application/json" \
  -d '{
    "message": "What tools do you have available?",
    "session_id": "test_session"
  }'
```

### List Available Tools
```bash
# See all available tools
curl -s http://localhost:8081/api/v1/tools | jq '.tools[] | {id, name, description}'
```

### Basic Task Execution
```bash
curl -X POST http://localhost:8081/api/v1/task/execute \
  -H "Content-Type: application/json" \
  -d '{
    "task_name": "ScrapeWebsite",
    "context": {
      "url": "https://example.com"
    }
  }'
```

### Learning a New Method
```bash
curl -X POST http://localhost:8081/api/v1/learn/llm \
  -H "Content-Type: application/json" \
  -d '{
    "task_name": "SendEmail",
    "description": "Send an email to a recipient with a subject and body",
    "context": {
      "recipient": "user@example.com",
      "subject": "Test Email"
    }
  }'
```

### Using MCP Tools
```bash
curl -X POST http://localhost:8081/api/v1/learn/mcp \
  -H "Content-Type: application/json" \
  -d '{
    "task_name": "AnalyzeData",
    "description": "Analyze data from a file and generate insights",
    "context": {
      "file_path": "/path/to/data.csv",
      "analysis_type": "statistical"
    }
  }'
```

## Development

### Running Tests

```bash
go test ./...
```

### Building

```bash
go build -o hdn .
```

### Running with Custom Config

```bash
go run . -config=my-config.json -domain=my-domain.json -port=9090
```

## Architecture

The system consists of several key components:

1. **API Server**: HTTP server that handles REST endpoints
2. **Execution Engine**: Executes tasks based on their type
3. **LLM Client**: Integrates with Large Language Models
4. **MCP Client**: Integrates with Model Context Protocol
5. **HTN Planner**: Core planning algorithm
6. **Learning Engine**: Learns new methods from failures

## Contributing

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Add tests
5. Submit a pull request

## License

This project is licensed under the MIT License.

## Testing
 Test the integration
cd hdn
go run . -mode=test-llm

# Or run the full test suite
./test_ollama_integration.sh