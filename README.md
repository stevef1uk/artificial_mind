# 🧠 Artificial Mind Project V0.2
## *AI Designing and Building AI*

**Project Directed by:** Steven Fisher  
**Designed by:** ChatGPT  
**Implemented using:** Cursor  
**Powered by:** Claude  

---

## 🎯 Project Overview

This is an advanced Artificial Mind (Artificial Mind) system that represents a unique collaboration between human direction and AI capabilities. The project demonstrates AI designing and building AI, showcasing the potential for recursive intelligence development where AI systems can contribute to their own evolution and the creation of more sophisticated AI architectures.

### Live Demo
A short demo video of the project running: [YouTube Demo Video](https://youtu.be/htnKAMptuzw)

### 🌟 Key Philosophy

This project embodies the concept of **"AI Building AI"** - where artificial intelligence systems are not just tools, but active participants in the design and implementation of more advanced AI systems. It represents a step toward recursive self-improvement and collaborative intelligence development.

---

## 🏗️ System Architecture

The Artificial Mind system consists of several interconnected components that work together to create a comprehensive artificial intelligence platform:

### Core Components

- **🧠 FSM Engine** - Finite State Machine for cognitive state management
- **🎯 HDN (Hierarchical Decision Network)** - AI planning and execution system with ethical safeguards
- **⚖️ Principles API** - Ethical decision-making system for AI actions
- **🎪 Conversational Layer** - Natural language interface with thinking mode
- **🔧 Tool System** - Extensible tool framework for AI capabilities
- **📊 Monitor UI** - Real-time visualization and control interface
- **🧠 Thinking Mode** - Real-time AI introspection and transparency

### Advanced Features

- **Real-time Thought Expression** - See inside the AI's reasoning process
- **Ethical Safeguards** - Built-in principles checking for all actions
- **Hierarchical Planning** - Multi-level task decomposition and execution
- **Natural Language Interface** - Conversational AI with full transparency
- **Tool Integration** - Extensible framework for AI capabilities
- **Knowledge Growth** - Continuous learning and adaptation
- **Focused Learning** - System focuses on promising areas and learns from outcomes
- **Meta-Learning** - System learns about its own learning process
- **Semantic Concept Discovery** - LLM-based concept extraction with understanding
- **Intelligent Knowledge Filtering** - LLM-based assessment of novelty and value to prevent storing obvious/duplicate knowledge

---

## 📚 Documentation

### 🏛️ Architecture & Design
- [**System Overview**](docs/SYSTEM_OVERVIEW.md) - High-level system architecture
- [**Architecture Details**](docs/ARCHITECTURE.md) - Detailed technical architecture
- [**Solution Architecture Diagram**](docs/SOLUTION_ARCHITECTURE_DIAGRAM.md) - Visual system design
- [**HDN Architecture**](docs/hdn_architecture.md) - Hierarchical Decision Network design

### 🧠 AI & Reasoning
- [**Thinking Mode**](docs/THINKING_MODE_README.md) - Real-time AI introspection and transparency
- [**Reasoning & Inference**](docs/REASONING_AND_INFERENCE.md) - AI reasoning capabilities
- [**Reasoning Implementation**](docs/REASONING_IMPLEMENTATION_SUMMARY.md) - Technical implementation details
- [**Knowledge Growth**](docs/KNOWLEDGE_GROWTH.md) - Continuous learning system
- [**Domain Knowledge**](docs/DOMAIN_KNOWLEDGE.md) - Knowledge representation and management
- [**LLM-Based Knowledge Filtering**](docs/LLM_BASED_KNOWLEDGE_FILTERING.md) - Intelligent filtering of novel, valuable knowledge

### 💬 Interfaces & Communication
- [**Conversational AI Summary**](docs/CONVERSATIONAL_AI_SUMMARY.md) - Natural language interface
- [**Natural Language Interface**](docs/NATURAL_LANGUAGE_INTERFACE.md) - Language processing capabilities
- [**API Reference**](docs/API_REFERENCE.md) - Complete API documentation

### ⚖️ Ethics & Safety
- [**Principles Integration**](docs/PRINCIPLES_INTEGRATION.md) - Ethical decision-making system
- [**Content Safety**](docs/CONTENT_SAFETY_README.md) - Safety mechanisms and content filtering
- [**Dynamic Integration Guide**](docs/DYNAMIC_INTEGRATION_GUIDE.md) - Dynamic system integration

### 🔧 Implementation & Development
- [**Setup Guide**](docs/SETUP_GUIDE.md) - Complete setup instructions for new users
- [**Configuration Guide**](docs/CONFIGURATION_GUIDE.md) - Docker, LLM, and deployment configuration
- [**Secure Packaging Guide**](docs/SECURE_PACKAGING_GUIDE.md) - Binary encryption and security
- [**Implementation Summary**](docs/IMPLEMENTATION_SUMMARY.md) - Development overview
- [**Integration Guide**](docs/INTEGRATION_GUIDE.md) - System integration instructions
- [**Refactoring Plan**](docs/REFACTORING_PLAN.md) - Code organization and refactoring
- [**Tool Metrics**](docs/TOOL_METRICS_README.md) - Performance monitoring and metrics

### 🐳 Infrastructure & Deployment
- [**Docker Compose**](docker-compose.yml) - Local development deployment
- [**Kubernetes (k3s)**](k3s/README.md) - Production Kubernetes deployment
- [**Docker Resource Config**](docs/DOCKER_RESOURCE_CONFIG.md) - Container configuration
- [**Docker Reuse Strategy**](docs/docker_reuse_strategy.md) - Container optimization

### 📊 Monitoring & Analysis
- [**Tool Metrics**](docs/TOOL_METRICS_README.md) - Performance monitoring
- [**Intelligent Execution**](docs/INTELLIGENT_EXECUTION.md) - Execution monitoring and analysis

---

## 🚀 Quick Start

### 🎯 Super Quick Start (5 minutes)

```bash
# 1. Clone the repository
git clone https://github.com/yourusername/agi-project.git
cd agi-project

# 2. Start infrastructure (Redis, Neo4j, Weaviate, NATS)
docker compose up -d  # or: docker-compose up -d

# 3. Start app services without touching infra (safer on macOS)
./scripts/start_servers.sh --skip-infra

# 4. Open your browser to http://localhost:8082
```

### 📋 Prerequisites

- **Docker & Docker Compose** - [Download here](https://www.docker.com/get-started)
- **Git** - [Download here](https://git-scm.com/downloads)
- **Go 1.21+** - [Download here](https://go.dev/dl/) (required for building services)
- **LLM Provider** - OpenAI, Anthropic, or local LLM (see [Setup Guide](docs/SETUP_GUIDE.md))

**macOS Users:**
- The Monitor UI builds natively on macOS without CGO dependencies
- Use `make build-monitor` or `make build-macos` to build
- Ensure Go 1.21+ is installed: `go version`

### ⚙️ Configuration

1. **Copy environment template**:
   ```bash
   cp env.example .env
   ```

2. **Edit configuration** (see [Configuration Guide](docs/CONFIGURATION_GUIDE.md)):
   ```bash
   nano .env
   ```

3. **Configure your environment**:
   ```bash
   # Copy the example configuration
   cp .env.example .env
   
   # Edit with your settings
   nano .env
   ```

   The `.env` file contains all configuration including:
   - **LLM Provider Settings** (OpenAI, Anthropic, Ollama, Mock)
   - **Service URLs** (Redis, NATS, Neo4j, Weaviate)
   - **Database Configuration** (Neo4j credentials, Qdrant URL)
   - **Docker Resource Limits** (Memory, CPU, PIDs)
   - **Performance Tuning** (Concurrent executions, timeouts)

### 🐳 Docker Setup (Development)

#### For x86_64 Systems (Intel/AMD)
```bash
# Start all services with x86_64 optimized images
docker-compose -f docker-compose.x86.yml up -d

# Check status
docker-compose -f docker-compose.x86.yml ps

# View logs
docker-compose -f docker-compose.x86.yml logs -f
```

#### For ARM64 Systems (Raspberry Pi, Apple Silicon)
```bash
# Start all services with ARM64 images
docker compose up -d   # prefer v2 syntax if available; otherwise use docker-compose up -d

# Check status
docker-compose ps

# View logs
docker-compose logs -f
```

### ▶️ Running App Services Without Managing Infra

If you already started infrastructure with Compose, you can start just the Go services without touching Docker ports using the new flag:

```bash
./scripts/start_servers.sh --skip-infra
```

This avoids killing Docker Desktop proxy processes on macOS and prevents daemon disruptions.

#### Multi-Architecture Build
```bash
# Build for multiple architectures
./scripts/build-multi-arch.sh -r your-registry.com -t latest --push

# Or use Makefile for local builds
make build-x86      # Build for x86_64
make build-arm64    # Build for ARM64
make build-all-archs # Build for both
```

### ☸️ Kubernetes Setup (Production)

```bash
# Deploy to k3s cluster on ARM Raspberry Pi
kubectl apply -f k3s/namespace.yaml
kubectl apply -f k3s/pvc-*.yaml
kubectl apply -f k3s/redis.yaml -f k3s/weaviate.yaml -f k3s/neo4j.yaml -f k3s/nats.yaml
kubectl apply -f k3s/principles-server.yaml -f k3s/hdn-server.yaml -f k3s/goal-manager.yaml -f k3s/fsm-server.yaml -f k3s/monitor-ui.yaml

# Check deployment
kubectl -n agi get pods,svc
```

**Note:** All Kubernetes configurations use `kubernetes.io/arch: arm64` node selectors and are optimized for ARM Raspberry Pi hardware with Drone CI execution methods.

See [k3s/README.md](k3s/README.md) for detailed Kubernetes deployment instructions.

### 🔧 Manual Setup (Development)

```bash
# Build all components
make build

# Start services individually
./bin/principles-server &
./bin/hdn-server -mode=server &
./bin/goal-manager -agent=agent_1 &
./bin/fsm-server &
```

### 🍎 Building Monitor UI on macOS

The Monitor UI can be built natively on macOS:

```bash
# Build monitor UI for macOS
make build-monitor

# Or build specifically for macOS (explicit)
make build-macos

# The binary will be created at: bin/monitor-ui
```

**Note:** The monitor UI uses pure Go (no CGO dependencies), so it builds cleanly on macOS without any special requirements. Just ensure you have Go 1.21+ installed.

**Troubleshooting:**
- If you encounter issues, ensure you're using Go 1.21 or later: `go version`
- The monitor UI doesn't require CGO, so it should build without any C compiler dependencies
- If templates aren't found, ensure you're running from the project root directory

### 🧪 Test Your Setup

```bash
# Test basic functionality
curl http://localhost:8081/health

# Test chat with thinking mode
curl -X POST http://localhost:8081/api/v1/chat \
  -H "Content-Type: application/json" \
  -d '{"message": "Hello! Think out loud about what you can do.", "show_thinking": true}'

# Test specific LLM provider
curl -X POST http://localhost:8081/api/v1/chat \
  -H "Content-Type: application/json" \
  -d '{"message": "What LLM provider are you using?", "show_thinking": true}'
```

---

## 🛠️ Utility Scripts

The project includes several utility scripts for common operations:

### Database Management

**Clean All Databases** (`scripts/clean_databases.sh`):
```bash
# Thoroughly clean all databases (stops services first)
./scripts/clean_databases.sh --confirm

# This script:
# - Stops all services to prevent key recreation
# - Clears Redis (all keys)
# - Clears Neo4j (all nodes and relationships)
# - Clears Weaviate (all collections)
# - Cleans persistent data directories
# - Restarts containers
```

**Clean Reasoning Traces** (`scripts/clean_reasoning_traces.sh`):
```bash
# Clean old reasoning traces from Redis (reduces UI clutter)
./scripts/clean_reasoning_traces.sh

# This script:
# - Trims reasoning traces to 10 per key
# - Trims explanations to 5 per key
# - Helps reduce UI spam after database cleanup
```

**Clean All Data** (`scripts/clean_all.sh`):
```bash
# Complete cleanup including logs
./scripts/clean_all.sh --confirm

# This script:
# - Clears all log files
# - Clears Redis, Neo4j, and Weaviate
# - More comprehensive than clean_databases.sh
```

### System Management

**Restart System** (`restart.sh`):
```bash
# Quick restart of entire system
./restart.sh

# This script:
# - Stops all application services
# - Restarts infrastructure (Docker Compose)
# - Starts all application services
# - Provides status check URLs
```

**Using Make Targets**:
```bash
# Clean databases via Makefile
make clean-databases

# Full reset (stop → clean → restart)
make reset-all

# Clear Redis only
make clear-redis

# Clear all databases (requires confirmation)
make clear-redis CONFIRM=YES
```

See [Database Cleanup Guide](docs/DATABASE_CLEANUP.md) for detailed information.

### 🔐 Secure Packaging (Optional)

Create security files for production:

```bash
# Create secure directory and keypairs
mkdir -p secure/
openssl genrsa -out secure/customer_private.pem 2048
openssl rsa -in secure/customer_private.pem -pubout -out secure/customer_public.pem
openssl genrsa -out secure/vendor_private.pem 2048
openssl rsa -in secure/vendor_private.pem -pubout -out secure/vendor_public.pem
echo "your-token-content-here" > secure/token.txt
```

See [Secure Packaging Guide](docs/SECURE_PACKAGING_GUIDE.md) for details.

---

## 🎯 Key Features

### 🧠 Thinking Mode (NEW!)
Experience real-time AI introspection with our revolutionary thinking mode:

```json
{
  "message": "Please learn about black holes and explain them to me",
  "show_thinking": true
}
```

**Features:**
- **Real-time thought streaming** via WebSockets/SSE
- **Multiple thought styles** (conversational, technical, streaming)
- **Confidence visualization** and decision tracking
- **Tool usage monitoring** and execution transparency
- **Educational interface** for understanding AI reasoning

### 📋 Activity Log (NEW!)
See what the system is doing in plain English with the activity log:

```bash
# View recent activities
curl http://localhost:8083/activity?limit=20
```

**Features:**
- **Human-readable activity log** - See state transitions, hypothesis generation, knowledge growth
- **Real-time monitoring** - Activities logged as they happen
- **Easy debugging** - Understand why the system made certain decisions
- **Learning insights** - Track when and how the knowledge base grows
- **Hypothesis tracking** - Follow hypothesis generation and testing cycles

See [Activity Log Documentation](docs/ACTIVITY_LOG.md) for complete details.

### ⚖️ Ethical AI
- **Pre-execution checking** - All actions validated before execution
- **Dynamic rule loading** - Update ethical rules without restarting
- **Fail-safe design** - Continues operation with safety checks
- **Transparent decision-making** - Clear reasoning for all decisions

### 🎯 Hierarchical Planning
- **Multi-level task decomposition** - Break complex tasks into manageable steps
- **Dynamic task analysis** - Handles LLM-generated tasks intelligently
- **Context-aware execution** - Maintains context across task hierarchies
- **Progress tracking** - Real-time monitoring of task execution

### 💬 Natural Language Interface
- **Conversational AI** - Natural language interaction with full transparency
- **Intent recognition** - Understands user goals and context
- **Multi-modal communication** - Text, structured data, and visual interfaces
- **Session management** - Persistent conversation context

---

## 🔌 API Endpoints

### Core Services

| Service | Port | Description |
|---------|------|-------------|
| **Principles API** | 8080 | Ethical decision-making |
| **HDN Server** | 8081 | AI planning and execution |
| **Monitor UI** | 8082 | Real-time visualization |
| **FSM Server** | 8083 | Cognitive state management |

### Key Endpoints

#### 🧠 Thinking Mode
- `POST /api/v1/chat` - Chat with thinking mode enabled
- `GET /api/v1/chat/sessions/{id}/thoughts` - Get AI thoughts
- `GET /api/v1/chat/sessions/{id}/thoughts/stream` - Stream thoughts in real-time

#### 🎯 Task Execution
- `POST /api/v1/interpret/execute` - Natural language task execution
- `POST /api/v1/hierarchical/execute` - Complex task planning
- `POST /api/v1/docker/execute` - Code execution in containers

#### 🔧 Tools & Capabilities
- `GET /api/v1/tools` - List available tools
- `POST /api/v1/tools/execute` - Execute specific tools
- `GET /api/v1/intelligent/capabilities` - AI capabilities

#### 📊 FSM Monitoring & Activity Log
- `GET /health` - FSM server health check
- `GET /status` - Full FSM status and metrics
- `GET /thinking` - Current thinking process and state
- `GET /activity?limit=50` - **Activity log** - See what the system is doing in plain English
- `GET /timeline?hours=24` - State transition timeline
- `GET /hypotheses` - Active hypotheses
- `GET /episodes?limit=10` - Recent learning episodes

---

## 🎨 Usage Examples

### Basic Chat with Thinking Mode

```bash
curl -X POST http://localhost:8081/api/v1/chat \
  -H "Content-Type: application/json" \
  -d '{
    "message": "Explain quantum computing in simple terms",
    "show_thinking": true,
    "session_id": "demo_session"
  }'
```

### Real-time Thought Monitoring

```bash
# Stream AI thoughts in real-time
curl http://localhost:8081/api/v1/chat/sessions/demo_session/thoughts/stream
```

### Natural Language Task Execution

```bash
curl -X POST http://localhost:8081/api/v1/interpret/execute \
  -H "Content-Type: application/json" \
  -d '{
    "input": "Scrape https://example.com and analyze the content"
  }'
```

### Code Generation and Execution

```bash
curl -X POST http://localhost:8081/api/v1/docker/execute \
  -H "Content-Type: application/json" \
  -d '{
    "code": "def fibonacci(n): return n if n <= 1 else fibonacci(n-1) + fibonacci(n-2)",
    "language": "python"
  }'
```

### View System Activity Log

**See what the system is doing in real-time:**

```bash
# Get recent activity (last 20 activities)
curl http://localhost:8083/activity?limit=20

# Watch activity in real-time (updates every 2 seconds)
watch -n 2 'curl -s http://localhost:8083/activity?limit=5 | jq -r ".activities[] | \"\(.timestamp | split(\".\")[0]) \(.message)\""'

# Get activity for specific agent
curl http://localhost:8083/activity?agent_id=agent_1&limit=50
```

**Example Response:**
```json
{
  "activities": [
    {
      "timestamp": "2024-01-15T10:30:00Z",
      "message": "Moved from 'idle' to 'perceive': Ingesting and validating new data",
      "state": "perceive",
      "category": "state_change",
      "details": "Reason: new_input"
    },
    {
      "timestamp": "2024-01-15T10:30:15Z",
      "message": "🧠 Generating hypotheses from facts and domain knowledge",
      "state": "hypothesize",
      "category": "action",
      "action": "generate_hypotheses"
    },
    {
      "timestamp": "2024-01-15T10:30:30Z",
      "message": "Generated 3 hypotheses in domain 'programming'",
      "state": "hypothesize",
      "category": "hypothesis",
      "details": "Domain: programming, Count: 3"
    }
  ],
  "count": 3,
  "agent_id": "agent_1"
}
```

**Activity Categories:**
- `state_change` - System moved to a new state
- `action` - Important action being executed
- `hypothesis` - Hypothesis generation or testing
- `learning` - Knowledge base growth
- `decision` - Decision-making processes

See [Activity Log Documentation](docs/ACTIVITY_LOG.md) for more details.

---

## 🧪 Testing

### Integration Tests
```bash
make test-integration
```

### Component Tests
```bash
make test-principles    # Test ethical decision-making
make test-hdn          # Test AI planning system
make test-thinking     # Test thinking mode features
```

### Performance Tests
```bash
make test-performance  # Load and stress testing
make test-metrics      # Performance metrics
```

---

## 🔧 Development

### Development Mode
```bash
make dev  # Start all services with auto-reload
```

### Building Components

**Build All:**
```bash
make build  # Build all components
```

**Build Individual Components:**
```bash
make build-principles  # Build Principles Server
make build-hdn         # Build HDN Server
make build-monitor     # Build Monitor UI
make build-fsm         # Build FSM Server
make build-goal        # Build Goal Manager
```

**Cross-Platform Builds:**
```bash
make build-macos       # Build for macOS (darwin/amd64)
make build-linux       # Build for Linux
make build-windows     # Build for Windows
make build-arm64       # Build for ARM64
make build-x86         # Build for x86_64
make build-all-archs   # Build for multiple architectures
```

**Monitor UI on macOS:**
```bash
# The monitor UI builds cleanly on macOS without CGO dependencies
make build-monitor

# Or use the explicit macOS target
make build-macos

# Verify the build
./bin/monitor-ui --help
```

### Code Quality
```bash
make fmt      # Format code
make lint      # Lint code
make coverage  # Generate coverage report
```

### Adding New Features
1. Create feature branch
2. Implement changes
3. Add tests
4. Update documentation
5. Submit pull request

---

## 🌟 Innovation Highlights

### AI Building AI
This project represents a unique approach where AI systems actively participate in their own development and the creation of more advanced AI architectures.

### Real-time Transparency
The thinking mode provides unprecedented insight into AI decision-making processes, enabling trust and understanding.

### Ethical by Design
Built-in ethical safeguards ensure all AI actions are evaluated against moral principles before execution.

### Hierarchical Intelligence
Multi-level planning and execution capabilities that can handle complex, multi-step tasks intelligently.

### Continuous Learning
The system grows and adapts through experience, demonstrating true learning capabilities.

### Focused and Successful Learning (NEW!)
The system now includes six major improvements for more focused and successful learning:

1. **Goal Outcome Learning**: Tracks which goals succeed/fail and learns from outcomes
2. **Enhanced Goal Scoring**: Uses historical success data to prioritize goals
3. **Hypothesis Value Pre-Evaluation**: Filters low-value hypotheses before testing
4. **Focused Learning Strategy**: Focuses on promising areas (70% focused, 30% exploration)
5. **Meta-Learning System**: Learns about its own learning process
6. **Improved Concept Discovery**: Uses LLM-based semantic analysis instead of pattern matching

See `docs/LEARNING_FOCUS_IMPROVEMENTS.md` for detailed information.

---

## 🤝 Contributing

We welcome contributions from the AI and research community! This project represents a collaborative effort between human intelligence and artificial intelligence.

### How to Contribute
1. Fork the repository
2. Create a feature branch
3. Implement your changes
4. Add comprehensive tests
5. Update documentation
6. Submit a pull request

### Areas for Contribution
- **New AI capabilities** - Extend the tool system
- **Ethical frameworks** - Improve the principles system
- **Interface improvements** - Enhance user experience
- **Performance optimization** - Improve system efficiency
- **Documentation** - Help others understand the system

---

## 📄 License

This project is licensed under the **MIT License with Attribution Requirement**.

### Key Points:
- ✅ **Free to use** for personal and commercial projects
- ✅ **Free to modify** and create derivative works
- ✅ **Free to distribute** and sell
- 📝 **Must attribute** Steven Fisher as the original author
- 📝 **Must include** this license file in derivative works

### Attribution Requirements:
When using this software, you must:
1. Include the original copyright notice: "Copyright (c) 2025 Steven Fisher"
2. Display "Steven Fisher" in README files, credits, or documentation
3. Include this LICENSE file in your project
4. Preserve attribution in any derivative works

See the [LICENSE](LICENSE) file for complete terms.

This license ensures Steven Fisher receives proper credit while allowing maximum freedom for others to use and build upon this work.

---

## 🙏 Acknowledgments

- **Steven Fisher** - Project Direction and Vision
- **ChatGPT** - System Design and Architecture
- **Cursor** - Development Environment and Tools
- **Claude** - Implementation and Code Generation
- **Open Source Community** - Foundational technologies and libraries

---

*"The best way to predict the future is to invent it, and the best way to invent the future is to have AI help us build it."*

**This project demonstrates that the future of AI development is not just human-led or AI-led, but a collaborative partnership between human creativity and artificial intelligence capabilities.**
