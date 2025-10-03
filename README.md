# 🧠 Artificial Mind Project V0.2
## *AI Designing and Building AI*

**Project Directed by:** Steven Fisher  
**Designed by:** ChatGPT  
**Implemented using:** Cursor  
**Powered by:** Claude  

---

## 🎯 Project Overview

This is an advanced Artificial Mind (Artificial Mind) system that represents a unique collaboration between human direction and AI capabilities. The project demonstrates AI designing and building AI, showcasing the potential for recursive intelligence development where AI systems can contribute to their own evolution and the creation of more sophisticated AI architectures.

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
- **LLM Provider** - OpenAI, Anthropic, or local LLM (see [Setup Guide](docs/SETUP_GUIDE.md))

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