# Artificial Mind System - High-Level Solution Architecture

## Executive Summary

This diagram represents the high-level architecture of an Artificial Mind (Artificial Mind) system that combines ethical decision-making, hierarchical planning, intelligent code generation, and self-aware learning capabilities.

## Architecture Diagram (Updated)

```mermaid
graph TB
    %% External Interface Layer
    subgraph "External Interface Layer"
        User[👤 User / Client Apps]
        Monitor[📊 Monitor UI<br/>Dashboard & Chain-of-Thought]
    end

    %% Cognition & Control Layer
    subgraph "Cognition & Control Layer"
        FSM[🧠 FSM Engine<br/>Reasoning & Autonomy]
        GoalMgr[🎯 Goal / Self-Model Manager<br/>Goals, Outcomes, Meta-Learning]
        Principles[🔒 Principles Server<br/>Ethical Rules (8080)]
    end

    %% Planning & Execution Layer
    subgraph "Planning & Execution Layer"
        HDN[🌐 HDN API Server (8081)<br/>Tasks, Chat, Tools, Memory]
        Planner[📋 Planner Evaluator<br/>Hierarchical Workflows]
        Executor[🤖 Intelligent Executor<br/>Code Gen & Execution]
    end

    %% Knowledge, Reasoning & MCP Layer
    subgraph "Knowledge & Reasoning Layer"
        Reasoning[💭 Reasoning Engine<br/>Curiosity & Hypotheses]
        MCP[🔌 MCP Knowledge Server<br/>Neo4j / Weaviate Tools]
        LLM[🤖 LLM Provider(s)<br/>OpenAI / Anthropic / Local]
    end

    %% Memory & Data Layer
    subgraph "Memory & Data Layer"
        Redis[(💾 Redis<br/>Working Memory, Goals, State)]
        Qdrant[(🔍 Qdrant<br/>Episodic Memory / RAG)]
        Neo4j[(🕸️ Neo4j<br/>Domain Knowledge Graph)]
    end

    %% Infrastructure & Integration Layer
    subgraph "Infrastructure Layer"
        Docker[🐳 Docker<br/>Execution Sandbox]
        NATS[📡 NATS Event Bus<br/>agi.events.*]
        Daily[📅 Daily Summary Pipeline<br/>Nightly FSM → HDN job]
    end

    %% External Flows
    User -->|Chat, Tasks, Tools| HDN
    User -->|Policy Config| Principles
    User -->|Status, Traces, Activity| Monitor

    %% Cognition & Control Flows
    FSM <-->|Delegate Complex Tasks| HDN
    FSM -->|Reasoning State, Curiosity Goals| Reasoning
    FSM -->|Ethical Check Requests| Principles
    FSM -->|Goal Outcomes| GoalMgr
    GoalMgr -->|Goal Scores & Focus Areas| FSM
    GoalMgr -->|Goal Stats & Meta-Learning| Redis

    %% Planning & Execution Flows
    HDN -->|Hierarchical Plans| Planner
    Planner -->|Workflow Orchestration| Executor
    HDN -->|Intelligent Execute / Tools| Executor
    Executor -->|Code & Results| Docker
    Executor -->|Working State & Capabilities| Redis

    %% Knowledge & Reasoning Flows
    Reasoning -->|Knowledge Queries| HDN
    HDN -->|/api/v1/knowledge/*| Neo4j
    HDN -->|MCP JSON-RPC| MCP
    MCP -->|Graph / Vector Queries| Neo4j
    MCP -->|Vector / Hybrid Search| Qdrant
    Executor -->|Code Gen, Fix, Safety| LLM
    Reasoning -->|Hypothesis Screening| LLM

    %% Memory & Data Flows
    HDN -->|Sessions, State, Tools, Projects| Redis
    FSM -->|Beliefs, Goals, Curiosity Data| Redis
    Planner -->|Episodes & Feedback| Qdrant
    HDN -->|Episodic Traces| Qdrant
    Neo4j -->|Domain Constraints & Concepts| Reasoning

    %% Event Bus & Observability
    FSM -->|Perception, Reasoning, Learning Events| NATS
    HDN -->|Task, Tool, Memory Events| NATS
    Planner -->|Workflow Events| NATS
    Monitor -->|Subscribe, Render Dashboards| NATS
    Daily -->|Summary Requests| HDN
    HDN -->|Daily Summary JSON| Redis
    Monitor -->|Daily Summary API| Redis

    %% Styling
    classDef externalClass fill:#e3f2fd,stroke:#1976d2,stroke-width:1.5px
    classDef cognitionClass fill:#f3e5f5,stroke:#7b1fa2,stroke-width:1.5px
    classDef planningClass fill:#e8f5e8,stroke:#388e3c,stroke-width:1.5px
    classDef knowledgeClass fill:#fff3e0,stroke:#f57c00,stroke-width:1.5px
    classDef memoryClass fill:#fce4ec,stroke:#c2185b,stroke-width:1.5px
    classDef infraClass fill:#f1f8e9,stroke:#689f38,stroke-width:1.5px

    class User,Monitor externalClass
    class FSM,GoalMgr,Principles cognitionClass
    class HDN,Planner,Executor planningClass
    class Reasoning,MCP,LLM knowledgeClass
    class Redis,Qdrant,Neo4j memoryClass
    class Docker,NATS,Daily infraClass
```

## Key Architectural Layers

### 1. **External Interface Layer**
- **User Interface**: Natural language interaction for task requests
- **Monitoring Dashboard**: Real-time system observability and control

### 2. **AI Cognition & Control Layer**
- **FSM Engine**: Core state management and reasoning engine
- **Principles Server**: Ethical decision-making and safety validation
- **Goal Manager**: Self-model management and learning coordination

### 3. **Planning & Execution Layer**
- **HDN API**: Hierarchical Decision Network for task orchestration
- **Planner/Evaluator**: Workflow planning and evaluation
- **Intelligent Executor**: Code generation and safe execution

### 4. **AI Services Layer**
- **LLM Services**: Large Language Model integration for code generation
- **Reasoning Engine**: Knowledge inference and logical deduction
- **Knowledge Integration**: Domain knowledge management and learning

### 5. **Data & Storage Layer**
- **Redis**: Working memory, caching, and session state
- **Qdrant**: Episodic memory and vector search
- **Neo4j**: Domain knowledge graph and relationships

### 6. **Infrastructure Layer**
- **Docker**: Secure code execution sandbox
- **NATS**: Event-driven communication backbone

## Key Architectural Principles

### 🛡️ **Safety-First Design**
- Multi-layer ethical validation
- Sandboxed code execution
- Principles-based decision making

### 🧠 **Self-Aware Intelligence**
- Goal-driven behavior
- Learning from experience
- Self-model management

### 🔄 **Event-Driven Architecture**
- Loose coupling via event bus
- Real-time monitoring and observability
- Scalable microservices design

### 📊 **Multi-Modal Memory**
- Working memory for immediate context
- Episodic memory for experience storage
- Semantic knowledge for domain expertise

### 🎯 **Hierarchical Planning**
- Multi-level task decomposition
- Workflow orchestration
- Intelligent execution delegation

## Technology Stack

- **Backend**: Go 1.21+
- **Databases**: Redis, Qdrant, Neo4j
- **AI/ML**: Ollama (Local LLM), Vector Search
- **Infrastructure**: Docker, NATS
- **APIs**: RESTful HTTP, Event-driven messaging

## Scalability & Performance

- **Horizontal Scaling**: Stateless microservices design
- **Caching Strategy**: Multi-layer Redis caching
- **Event Processing**: Asynchronous NATS messaging
- **Resource Management**: Docker containerization with limits

---

*This architecture represents a comprehensive Artificial Mind system designed for safe, ethical, and intelligent task execution with continuous learning capabilities.*
