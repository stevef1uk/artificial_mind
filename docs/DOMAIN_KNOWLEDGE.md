# Domain Knowledge System

## Overview

The Domain Knowledge System extends your Artificial Mind architecture with a structured knowledge graph stored in Neo4j. This system allows your AI agent to reason about domain-specific concepts, constraints, relationships, and safety principles.

## Architecture

```
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│   Redis Cache   │    │   Qdrant VDB    │    │   Neo4j Graph   │
│                 │    │                 │    │                 │
│ • Working Memory│    │ • Episodic      │    │ • Domain        │
│ • Beliefs       │    │   Memory        │    │   Knowledge     │
│ • Goals         │    │ • Experiences   │    │ • Concepts      │
│ • Short-term    │    │ • Vector Search │    │ • Relationships │
└─────────────────┘    └─────────────────┘    └─────────────────┘
         │                       │                       │
         └───────────────────────┼───────────────────────┘
                                 │
                    ┌─────────────────┐
                    │   HDN API       │
                    │                 │
                    │ • Planner       │
                    │ • Evaluator     │
                    │ • Executor      │
                    └─────────────────┘
```

## Key Components

### 1. **Concepts**
- **Name**: Unique identifier (e.g., "Matrix Multiplication")
- **Domain**: Category (e.g., "Math", "Programming", "System")
- **Definition**: Human-readable description
- **Properties**: Characteristics (e.g., "Associative", "Commutative")
- **Constraints**: Rules and limitations (e.g., "A.cols == B.rows")
- **Examples**: Worked examples and use cases

### 2. **Relationships**
- **REQUIRES**: One concept requires another
- **RELATED_TO**: General relationship
- **CAN_USE**: One concept can use another
- **BLOCKED_BY**: Safety principle constraints

### 3. **Safety Principles**
- Links domain concepts to ethical/safety rules
- Prevents harmful actions based on domain knowledge
- Integrates with your existing Principles server

## API Endpoints

### Concepts
- `GET /api/v1/knowledge/concepts` - List all concepts
- `POST /api/v1/knowledge/concepts` - Create a concept
- `GET /api/v1/knowledge/concepts/{name}` - Get specific concept
- `PUT /api/v1/knowledge/concepts/{name}` - Update concept
- `DELETE /api/v1/knowledge/concepts/{name}` - Delete concept

### Properties & Constraints
- `POST /api/v1/knowledge/concepts/{name}/properties` - Add property
- `POST /api/v1/knowledge/concepts/{name}/constraints` - Add constraint
- `POST /api/v1/knowledge/concepts/{name}/examples` - Add example

### Relationships
- `POST /api/v1/knowledge/concepts/{name}/relations` - Create relationship
- `GET /api/v1/knowledge/concepts/{name}/related` - Get related concepts

### Search
- `GET /api/v1/knowledge/search` - Search concepts by domain/name

## Usage Examples

### 1. Creating a Concept

```bash
curl -X POST http://localhost:8081/api/v1/knowledge/concepts \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Fibonacci Sequence",
    "domain": "Math",
    "definition": "A sequence where each number is the sum of the two preceding ones"
  }'
```

### 2. Adding Constraints

```bash
curl -X POST http://localhost:8081/api/v1/knowledge/concepts/Fibonacci%20Sequence/constraints \
  -H "Content-Type: application/json" \
  -d '{
    "description": "First two numbers must be 0 and 1",
    "type": "initialization",
    "severity": "error"
  }'
```

### 3. Creating Relationships

```bash
curl -X POST http://localhost:8081/api/v1/knowledge/concepts/Fibonacci%20Sequence/relations \
  -H "Content-Type: application/json" \
  -d '{
    "relation_type": "RELATED_TO",
    "target_concept": "Prime Number",
    "properties": {
      "description": "Fibonacci numbers can be used in prime number generation"
    }
  }'
```

## Integration with Planner/Evaluator

The domain knowledge system integrates with your existing planner/evaluator loop:

### 1. **Input Validation**
```go
// Before planning, validate inputs against domain constraints
constraints, err := domainKnowledge.GetConstraints(ctx, "Matrix Multiplication")
for _, constraint := range constraints {
    if constraint.Type == "dimension" {
        // Check if A.cols == B.rows
        if !validateMatrixDimensions(matrixA, matrixB) {
            return errors.New("Invalid matrix dimensions")
        }
    }
}
```

### 2. **Safety Checking**
```go
// Check if action violates safety principles
concept, err := domainKnowledge.GetConcept(ctx, "File Deletion")
if concept != nil {
    // Check if concept is blocked by any principles
    // This integrates with your existing Principles server
}
```

### 3. **Plan Scoring**
```go
// Use domain knowledge to score plan options
concept, err := domainKnowledge.GetConcept(ctx, "Python")
if concept != nil {
    // Python has high success rate for matrix operations
    score += concept.SuccessRate * 0.3
}
```

## Setup Instructions

### 1. **Start Neo4j**
```bash
# Using docker-compose
docker-compose up -d neo4j

# Or manually
docker run -d --name neo4j \
  -p 7474:7474 -p 7687:7687 \
  -e NEO4J_AUTH=neo4j/test1234 \
  neo4j:5-community
```

### 2. **Build with Neo4j Support**
```bash
# Build HDN with Neo4j support
go build -tags neo4j -o bin/hdn hdn/main.go

# Build domain knowledge tools
make build-domain-knowledge
```

### 3. **Populate Initial Knowledge**
```bash
# Populate with sample domain knowledge
make populate-knowledge
```

### 4. **Test the System**
```bash
# Run API tests
make test-knowledge

# Or manually test
./scripts/test_domain_knowledge.sh
```

## Environment Variables

```bash
# Neo4j Configuration
NEO4J_URI=bolt://localhost:7687
NEO4J_USER=neo4j
NEO4J_PASS=test1234

# Optional: Qdrant for episodic memory
QDRANT_URL=http://localhost:6333
```

## Neo4j Schema

### Nodes
- **Concept**: Domain concepts (Matrix, Prime Number, Docker Container)
- **Property**: Concept properties (Associative, Commutative)
- **Constraint**: Rules and limitations (A.cols == B.rows)
- **Example**: Worked examples and use cases
- **Principle**: Safety principles and ethical rules

### Relationships
- **HAS_PROPERTY**: Concept → Property
- **HAS_CONSTRAINT**: Concept → Constraint
- **HAS_EXAMPLE**: Concept → Example
- **REQUIRES**: Concept → Concept
- **RELATED_TO**: Concept → Concept
- **BLOCKED_BY**: Concept → Principle

## Example Knowledge Graph

```
Matrix Multiplication
├── Properties
│   ├── Associative
│   ├── Distributive
│   └── Not Commutative
├── Constraints
│   ├── A.cols == B.rows
│   └── Result is A.rows × B.cols
├── Examples
│   └── [[1,2],[3,4]] × [[5,6],[7,8]] = [[19,22],[43,50]]
└── Relationships
    ├── REQUIRES → Matrix
    └── USED_IN → Prime Number (for encryption)
```

## Benefits

1. **Structured Reasoning**: AI can reason about domain-specific rules and constraints
2. **Safety Integration**: Domain knowledge integrates with your existing safety principles
3. **Explainable Decisions**: AI can explain why it made certain choices based on domain knowledge
4. **Extensible**: Easy to add new domains and concepts as your system grows
5. **Persistent**: Knowledge persists across sessions and can be shared between agents

## Next Steps

1. **Populate More Domains**: Add knowledge for your specific use cases
2. **Integrate with Planner**: Use domain knowledge in your planning algorithms
3. **Monitor UI Integration**: Add domain knowledge visualization to your monitor
4. **Learning Integration**: Update domain knowledge based on execution results
5. **Multi-Agent Support**: Share domain knowledge between multiple AI agents

## Troubleshooting

### Common Issues

1. **Neo4j Connection Failed**
   - Check if Neo4j is running: `curl http://localhost:7474`
   - Verify credentials in environment variables
   - Check firewall/network connectivity

2. **Build Tags Not Working**
   - Ensure you're building with `-tags neo4j`
   - Check that Neo4j driver is installed: `go mod tidy`

3. **API Returns 503 Service Unavailable**
   - Domain knowledge client failed to initialize
   - Check Neo4j connection and credentials
   - Look at HDN server logs for error details

### Debug Commands

```bash
# Check Neo4j status
curl -s http://localhost:7474

# Test domain knowledge API
curl -s http://localhost:8081/api/v1/knowledge/concepts

# View Neo4j browser
open http://localhost:7474
# Login: neo4j / test1234
```

## Contributing

To add new domain knowledge:

1. Create concepts using the API
2. Add properties, constraints, and examples
3. Create relationships between concepts
4. Link to safety principles as needed
5. Test with the integration examples

The system is designed to be extensible - you can add any domain knowledge that helps your AI agent make better decisions!
