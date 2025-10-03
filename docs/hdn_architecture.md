# HDN (Hierarchical Decision Network) System Architecture

## Mermaid Architecture Diagram

```mermaid
graph TB
    %% External Components
    User[👤 User] --> API[🌐 HDN API Server<br/>Port 8081]
    User --> Principles[🔒 Principles Server<br/>Port 8080]
    
    %% HDN Core Components
    API --> IE[🧠 Intelligent Executor]
    API --> CG[⚙️ Code Generator]
    API --> CS[💾 Code Storage<br/>Redis]
    API --> DA[🐳 Docker API]
    
    %% Event Bus (NATS)
    subgraph Bus[📡 Event Bus]
      NATS[(NATS Core<br/>agi.events.*)]
    end
    
    %% Producers
    API --> |Publish Canonical Events| NATS
    IE --> |Publish Exec Events| NATS
    
    %% Consumers
    SM --> |Subscribe (Beliefs)| NATS
    Capabilities --> |Subscribe (Catalog Updates)| NATS
    
    %% Self-Model Integration
    IE --> SM[🧠 Self-Model Manager<br/>Goal Tracking & Learning]
    SM --> Redis[💾 Redis<br/>Self-Model & Goals]
    
    %% LLM Integration - Safety Analysis
    IE --> |"1. Safety Analysis"| LLM1[🤖 LLM Client<br/>Safety Categorization]
    LLM1 --> |"Returns safety context"| IE
    
    %% LLM Integration - Code Generation
    CG --> |"2. Generate Code"| LLM2[🤖 LLM Client<br/>Code Generation]
    LLM2 --> |"Returns generated code"| CG
    
    %% LLM Integration - Code Fixing
    IE --> |"3. Fix Code"| LLM3[🤖 LLM Client<br/>Code Fixing]
    LLM3 --> |"Returns fixed code"| IE
    
    %% Principles Integration
    IE --> PC[🔍 Principles Checker]
    PC --> Principles
    
    %% Code Generation Flow
    CG --> CS
    CS --> DA
    DA --> Docker[🐳 Docker Container<br/>Code Execution]
    
    %% Validation Flow
    Docker --> Validation[✅ Code Validation]
    Validation --> CS
    Validation --> IE
    
    %% Self-Model Learning Flow
    IE --> |"4. Record Episode"| SM
    IE --> |"5. Update Beliefs"| SM
    IE --> |"6. Track Goals"| SM
    
    %% Capability Management
    CS --> Capabilities[📚 Capability Library]
    Capabilities --> API
    
    %% Safety & Security
    Principles --> Rules[📋 Ethical Rules<br/>principles.json]
    Rules --> Block[🚫 Block Harmful Actions]
    
    %% Data Flow
    User --> |"1. Request Task"| API
    API --> |"2. Check Principles"| PC
    PC --> |"3. Generate Code"| CG
    CG --> |"4. Store Code"| CS
    CS --> |"5. Execute in Docker"| DA
    DA --> |"6. Validate Results"| Validation
    Validation --> |"7. Learn & Update"| SM
    SM --> |"8. Return Results"| User
    
    %% LLM Call Labels
    LLM1 -.-> |"Safety Analysis<br/>categorizeRequestForSafety()"| IE
    LLM2 -.-> |"Code Generation<br/>GenerateCode()"| CG
    LLM3 -.-> |"Code Fixing<br/>fixCodeWithLLM()"| IE
    
    %% Self-Model Labels
    SM -.-> |"Goal Tracking<br/>Episode Recording<br/>Belief Updates"| IE
    
    %% Styling
    classDef userClass fill:#e1f5fe
    classDef serverClass fill:#f3e5f5
    classDef aiClass fill:#e8f5e8
    classDef storageClass fill:#fff3e0
    classDef securityClass fill:#ffebee
    classDef llmClass fill:#fff9c4
    classDef selfClass fill:#e8eaf6
    
    class User userClass
    class API,IE,CG,DA serverClass
    class PC aiClass
    class CS,Capabilities,Redis storageClass
    class Principles,Rules,Block securityClass
    class LLM1,LLM2,LLM3 llmClass
    class SM selfClass
```

## System Components

### 🧠 **Intelligent Executor (IE)**
- **Purpose**: Core orchestration engine
- **Functions**:
  - LLM safety analysis and categorization
  - Principles server integration
  - Code validation and execution
  - Capability caching and reuse
  - Self-model integration for learning

### 🧠 **Self-Model Manager (SM)**
- **Purpose**: Self-awareness and learning system
- **Functions**:
  - Goal tracking and status management
  - Episode recording with detailed metadata
  - Belief updates based on execution results
  - Performance metrics and success rate tracking
  - Learning from past experiences

#### Motivation & Goal Manager (Policy Layer)
- **Role**: Generates/prioritizes goals and influences planner/decider via active goals and priorities.
- **NATS Subjects (input)**: `agi.perception.fact`, `agi.evaluation.result`, `agi.user.goal`
- **NATS Subjects (output)**: `agi.goal.created`, `agi.goal.updated`, `agi.goal.progress`, `agi.goal.achieved`, `agi.goal.failed`
- **Redis Keys**:
  - `goals:{agent_id}:active` — set of current goal IDs
  - `goals:{agent_id}:history` — achieved/failed goals
  - `goals:{agent_id}:priorities` — sorted set for top-N selection
  - `goal:{goal_id}` — JSON blob
- **Scoring**: `priority_importance * confidence` baseline; policies can override.
- **Usage**: Planner/decider consult priorities and filter/score plans; evaluator emits progress to update goals.

### ⚙️ **Code Generator (CG)**
- **Purpose**: Generate executable code from natural language
- **Functions**:
  - LLM prompt engineering
  - Code extraction and cleaning
  - Multi-language support (Python, Go, JavaScript)
  - Test case removal

### 💾 **Code Storage (CS)**
- **Purpose**: Persistent storage and retrieval of generated code
- **Technology**: Redis
- **Functions**:
  - Code caching and versioning
  - Capability library management
  - Search and retrieval

### 🐳 **Docker API (DA)**
- **Purpose**: Safe code execution environment
- **Functions**:
  - Isolated code execution
  - Multi-language runtime support
  - Security sandboxing
  - Output capture

### 🔒 **Principles Server**
- **Purpose**: Ethical and safety validation
- **Functions**:
  - Rule-based action blocking
  - Context-aware safety checks
  - Dynamic rule loading
  - Harmful action prevention

### 🤖 **LLM Client (3 Different Calls)**
- **Purpose**: Natural language processing and code generation
- **Functions**:
  - **Safety Categorization** (`categorizeRequestForSafety()`)
    - Analyzes task requests for safety concerns
    - Returns safety context for principles checking
    - Categorizes as safe/unsafe based on task description
  - **Code Generation** (`GenerateCode()`)
    - Generates executable code from natural language
    - Supports multiple languages (Python, Go, JavaScript)
    - Includes code cleaning and test case removal
  - **Code Fixing** (`fixCodeWithLLM()`)
    - Fixes code when validation fails
    - Improves code based on error feedback
    - Retries with corrected implementation

## Key Features

### ✅ **Safety & Security**
- Principles-based ethical checking
- Docker sandboxing for code execution
- LLM safety analysis
- Harmful action blocking

### 🚀 **Intelligence**
- Natural language task understanding
- Multi-language code generation
- Automatic code validation
- Capability learning and reuse
- Self-aware learning from experience
- Cached capability reuse with cold-start vs cached execution surfaced to UI

### 🧠 **Self-Awareness**
- Goal tracking and management
- Episode recording with metadata
- Belief updates and performance tracking
- Learning from past execution results
- Continuous improvement capabilities

### 🔄 **Scalability**
- Redis-based caching
- Docker containerization
- RESTful API design
- Microservice architecture

### 📊 **Monitoring**
- Execution time tracking
- Success/failure validation
- Capability library statistics
- Self-model learning metrics
- Comprehensive logging

## Data Flow

1. **User Request** → HDN API Server
2. **Safety Check** → Principles Server
3. **Code Generation** → LLM Client
4. **Code Storage** → Redis
5. **Code Execution** → Docker Container (or reuse cached result when capability is hot)
6. **Validation** → Results verification
7. **Learning** → Self-Model Manager (goals, episodes, beliefs)
8. **Response** → User with results

## Technology Stack

- **Backend**: Go
- **Database**: Redis
- **Containerization**: Docker
- **LLM**: Ollama (Local)
- **API**: RESTful HTTP
- **Security**: Principles-based rules
 - **Project Scoping**: All intelligent/execute and capability routes accept/require `X-Project-ID` and propagate `project_id` in body context
 - **Timeouts**: Intelligent execution keeps a 120s timeout window to accommodate cold-starts
