# 🚀 Artificial Mind Project Setup Guide

This guide will help you set up the Artificial Mind project from scratch, configure your own Docker and LLM settings, and get everything running.

## 📋 Prerequisites

### Required Software
- **Go 1.21+** - [Download here](https://golang.org/dl/)
- **Docker & Docker Compose** - [Download here](https://www.docker.com/get-started)
- **Git** - [Download here](https://git-scm.com/downloads)
- **Make** (optional but recommended) - Usually pre-installed on Linux/macOS

### Required Services
- **Redis** - For state management and caching
- **NATS** - For event streaming and messaging
- **LLM Provider** - OpenAI, Anthropic, or local LLM

## 🏗️ Quick Setup (5 minutes)

### 1. Clone the Repository
```bash
git clone https://github.com/yourusername/agi-project.git
cd agi-project
```

### 2. Configure Environment
```bash
# Copy the example environment file
cp .env.example .env

# Edit the configuration
nano .env
```

The `.env.example` file contains a comprehensive configuration template with:

- **LLM Provider Settings** (OpenAI, Anthropic, Ollama, Mock)
- **Service URLs** (Redis, NATS, Neo4j, Weaviate)
- **Database Configuration** (Neo4j credentials, Qdrant URL)
- **Docker Resource Limits** (Memory, CPU, PIDs)
- **Performance Tuning** (Concurrent executions, timeouts)
- **Security Settings** (JWT secrets, API keys)
- **Development/Production Flags**

### 3. Start with Docker Compose
```bash
# Start all services
docker compose up -d   # prefer v2 syntax if available; otherwise use docker-compose up -d

# Check status
docker-compose ps
```

### 4. Test the Setup
```bash
# Test the API
curl http://localhost:8081/health

# Test thinking mode
curl -X POST http://localhost:8081/api/v1/chat \
  -H "Content-Type: application/json" \
  -d '{"message": "Hello, can you think out loud?", "show_thinking": true}'
```

### 5. Start App Services Without Managing Infra (Recommended on macOS)

If your infrastructure is already running via Compose, start only the application servers to avoid interfering with Docker Desktop's port proxies:

```bash
./scripts/start_servers.sh --skip-infra
```

This flag prevents attempts to kill listeners on container-mapped ports (like 8080 for Weaviate) that are managed by Docker Desktop, which can otherwise disrupt the Docker daemon.

## ⚙️ Detailed Configuration

### Environment Variables

Create a `.env` file in the project root:

```bash
# =============================================================================
# Artificial Mind PROJECT CONFIGURATION
# =============================================================================

# =============================================================================
# DOCKER CONFIGURATION
# =============================================================================
# Docker registry and image names
DOCKER_REGISTRY=your-registry.com
DOCKER_NAMESPACE=your-namespace
DOCKER_TAG=latest

# Custom image names (optional - defaults to project names)
FSM_IMAGE_NAME=agi-fsm
HDN_IMAGE_NAME=agi-hdn
PRINCIPLES_IMAGE_NAME=agi-principles
MONITOR_IMAGE_NAME=agi-monitor

# =============================================================================
# LLM CONFIGURATION
# =============================================================================
# Choose your LLM provider: openai, anthropic, ollama, or mock
LLM_PROVIDER=openai

# OpenAI Configuration
OPENAI_API_KEY=your_openai_api_key_here
OPENAI_MODEL=gpt-4
OPENAI_BASE_URL=https://api.openai.com/v1

# Anthropic Configuration
ANTHROPIC_API_KEY=your_anthropic_api_key_here
ANTHROPIC_MODEL=claude-3-sonnet-20240229

# Ollama Configuration (for local LLMs)
OLLAMA_BASE_URL=http://localhost:11434
OLLAMA_MODEL=llama2

# =============================================================================
# REDIS CONFIGURATION
# =============================================================================
REDIS_HOST=localhost
REDIS_PORT=6379
REDIS_PASSWORD=
REDIS_DB=0

# =============================================================================
# NATS CONFIGURATION
# =============================================================================
NATS_URL=nats://localhost:4222
NATS_CLUSTER_ID=agi-cluster

# =============================================================================
# SERVICE PORTS
# =============================================================================
PRINCIPLES_PORT=8080
HDN_PORT=8081
MONITOR_PORT=8082
FSM_PORT=8083

# =============================================================================
# SECURITY CONFIGURATION
# =============================================================================
# JWT Secret for API authentication
JWT_SECRET=your_jwt_secret_here

# API Keys for external services
API_KEY_HEADER=X-API-Key
API_KEY_VALUE=your_api_key_here

# =============================================================================
# MONITORING & LOGGING
# =============================================================================
LOG_LEVEL=info
ENABLE_METRICS=true
METRICS_PORT=9090

# =============================================================================
# DEVELOPMENT SETTINGS
# =============================================================================
DEBUG_MODE=false
ENABLE_CORS=true
CORS_ORIGINS=http://localhost:3000,http://localhost:8082
```

### Docker Configuration

#### Custom Docker Images

If you want to use your own Docker registry or custom image names:

```bash
# Build with custom names
docker build -t your-registry.com/your-namespace/agi-fsm:latest -f Dockerfile.fsm .
docker build -t your-registry.com/your-namespace/agi-hdn:latest -f Dockerfile.hdn .
docker build -t your-registry.com/your-namespace/agi-principles:latest -f Dockerfile.principles .
docker build -t your-registry.com/your-namespace/agi-monitor:latest -f Dockerfile.monitor .

# Push to your registry
docker push your-registry.com/your-namespace/agi-fsm:latest
docker push your-registry.com/your-namespace/agi-hdn:latest
docker push your-registry.com/your-namespace/agi-principles:latest
docker push your-registry.com/your-namespace/agi-monitor:latest
```

#### Docker Compose Override

Create `docker-compose.override.yml` for custom configurations:

```yaml
version: '3.8'

services:
  fsm-server:
    image: your-registry.com/your-namespace/agi-fsm:latest
    environment:
      - LLM_PROVIDER=${LLM_PROVIDER}
      - OPENAI_API_KEY=${OPENAI_API_KEY}
      - REDIS_HOST=${REDIS_HOST}
      - NATS_URL=${NATS_URL}
    ports:
      - "${FSM_PORT}:8083"

  hdn-server:
    image: your-registry.com/your-namespace/agi-hdn:latest
    environment:
      - LLM_PROVIDER=${LLM_PROVIDER}
      - OPENAI_API_KEY=${OPENAI_API_KEY}
      - REDIS_HOST=${REDIS_HOST}
      - NATS_URL=${NATS_URL}
    ports:
      - "${HDN_PORT}:8081"

  principles-server:
    image: your-registry.com/your-namespace/agi-principles:latest
    environment:
      - REDIS_HOST=${REDIS_HOST}
    ports:
      - "${PRINCIPLES_PORT}:8080"

  monitor-ui:
    image: your-registry.com/your-namespace/agi-monitor:latest
    environment:
      - HDN_URL=http://hdn-server:8081
      - FSM_URL=http://fsm-server:8083
    ports:
      - "${MONITOR_PORT}:8082"
```

## 🤖 LLM Provider Setup

The Artificial Mind project supports multiple LLM providers. Choose the one that best fits your needs:

### Option 1: OpenAI (Recommended for Production BUT ensure you underdtand the costs!)

**Best for**: Production use, high-quality responses, reliability

1. **Get API Key**: 
   - Sign up at [OpenAI Platform](https://platform.openai.com/api-keys)
   - Create a new API key
   - Add billing information (pay-per-use)

2. **Configure Environment**:
   ```bash
   # Set in your .env file
   LLM_PROVIDER=openai
   OPENAI_API_KEY=sk-your-key-here
   OPENAI_MODEL=gpt-4
   ```

3. **Test OpenAI Integration**:
   ```bash
   # Test the API
   curl -X POST http://localhost:8081/api/v1/chat \
     -H "Content-Type: application/json" \
     -d '{"message": "Hello! Can you help me test OpenAI integration?", "show_thinking": true}'
   ```

**Models Available**:
- `gpt-4` - Most capable, best for complex tasks
- `gpt-4-turbo` - Faster, good balance of speed and capability
- `gpt-3.5-turbo` - Cost-effective, good for simple tasks

### Option 2: Local LLM with Ollama (Recommended for Development)

**Best for**: Development, privacy, cost control, offline use

1. **Install Ollama**: [Download here](https://ollama.ai/)
2. **Pull a Model**:
   ```bash
   # For general use
   ollama pull llama2
   
   # For coding tasks
   ollama pull codellama
   
   # For latest models
   ollama pull llama3.2
   ```
3. **Configure Environment**:
   ```bash
   # Set in your .env file
   LLM_PROVIDER=ollama
   OLLAMA_BASE_URL=http://localhost:11434
   OLLAMA_MODEL=llama2
   ```

4. **Test Ollama Integration**:
   ```bash
   # Test the API
   curl -X POST http://localhost:8081/api/v1/chat \
     -H "Content-Type: application/json" \
     -d '{"message": "Hello! Can you help me test Ollama integration?", "show_thinking": true}'
   ```

### Option 3: Anthropic Claude

**Best for**: Alternative to OpenAI, high-quality responses

1. **Get API Key**: Sign up at [Anthropic Console](https://console.anthropic.com/)
2. **Configure Environment**:
   ```bash
   LLM_PROVIDER=anthropic
   ANTHROPIC_API_KEY=sk-ant-your-key-here
   ANTHROPIC_MODEL=claude-3-sonnet-20240229
   ```

### Option 4: Mock LLM (Development)

For testing without API costs:
```bash
LLM_PROVIDER=mock
```

## 🐳 Docker Setup Options

### Option 1: Use Pre-built Images (Easiest)

```bash
# Use the default images from Docker Hub
docker-compose up -d
```

### Option 2: Build Your Own Images

```bash
# Build all images
make docker-build

# Or build individually
docker build -t agi-fsm -f Dockerfile.fsm .
docker build -t agi-hdn -f Dockerfile.hdn .
docker build -t agi-principles -f Dockerfile.principles .
docker build -t agi-monitor -f Dockerfile.monitor .
```

### Option 3: Custom Registry

```bash
# Set your registry
export DOCKER_REGISTRY=your-registry.com
export DOCKER_NAMESPACE=your-namespace

# Build and push
make docker-build-push
```

## 🔧 Service Configuration

### Redis Setup

#### Using Docker (Recommended)
```bash
docker run -d --name agi-redis -p 6379:6379 redis:7-alpine
```

#### Using Local Installation
```bash
# Ubuntu/Debian
sudo apt-get install redis-server
sudo systemctl start redis-server

# macOS
brew install redis
brew services start redis

# Windows
# Download from https://github.com/microsoftarchive/redis/releases
```

### NATS Setup

#### Using Docker (Recommended)
```bash
docker run -d --name agi-nats -p 4222:4222 nats:2.9-alpine
```

#### Using Local Installation
```bash
# Download and run
wget https://github.com/nats-io/nats-server/releases/download/v2.9.0/nats-server-v2.9.0-linux-amd64.zip
unzip nats-server-v2.9.0-linux-amd64.zip
./nats-server-v2.9.0-linux-amd64/nats-server
```

## 🚀 Running the Project

### Method 1: Docker Compose (Recommended)

```bash
# Start all services
docker-compose up -d

# View logs
docker-compose logs -f

# Stop services
docker-compose down
```

### Method 2: Manual Build and Run

#### For x86_64 (Intel/AMD) Systems
```bash
# Build all components for x86_64
make build-x86

# Or build for current architecture
make build

# Start services individually
./bin/principles-server &
./bin/hdn-server -mode=server &
./bin/goal-manager -agent=agent_1 &
./bin/fsm-server &
```

#### For ARM64 (Raspberry Pi, Apple Silicon) Systems
```bash
# Build all components for ARM64
make build-arm64

# Or use Docker (recommended for ARM64)
docker-compose up -d
```

#### Cross-Compilation Support
```bash
# Build for different architectures from any system
make build-x86      # Build for x86_64
make build-arm64    # Build for ARM64
make build-windows  # Build for Windows
make build-macos    # Build for macOS

# Build for multiple architectures
make build-all-archs

# Custom target architecture
make TARGET_OS=linux TARGET_ARCH=amd64 build
make TARGET_OS=windows TARGET_ARCH=amd64 build
```

#### Architecture Detection
The Makefile automatically detects your system architecture:
- **x86_64** → `amd64` (Intel/AMD processors)
- **aarch64** → `arm64` (ARM processors like Raspberry Pi, Apple Silicon)

For help with cross-compilation:
```bash
make help-cross
```

### Method 3: Development Mode

```bash
# Start with auto-reload
make dev

# Or with specific services
make dev-fsm
make dev-hdn
make dev-principles
```

## 🏗️ CI/CD and Multi-Architecture Support

### Drone CI Configuration

The project includes separate Drone CI configurations for different architectures:

#### ARM64 Pipeline (`.drone.yml`)
- **Target**: Raspberry Pi and ARM64 servers
- **Images**: `*-secure` (e.g., `fsm-server:secure`)
- **Deployment**: Kubernetes with ARM64-specific manifests

#### x86_64 Pipeline (`.drone.x86.yml`)
- **Target**: Intel/AMD servers and workstations
- **Images**: `*-secure-x86` (e.g., `fsm-server:secure-x86`)
- **Deployment**: Kubernetes with x86_64 manifests

### Docker Multi-Architecture Builds

#### Building for Multiple Architectures
```bash
# Build for both x86_64 and ARM64
docker buildx build --platform linux/amd64,linux/arm64 -t your-registry/agi:latest .

# Build specific architecture
docker buildx build --platform linux/amd64 -t your-registry/agi:x86 .
docker buildx build --platform linux/arm64 -t your-registry/agi:arm64 .
```

#### Using Docker Compose with Architecture Tags
```yaml
# docker-compose.x86.yml
services:
  hdn-server:
    image: your-registry/hdn-server:secure-x86
    # ... other config

# docker-compose.arm64.yml  
services:
  hdn-server:
    image: your-registry/hdn-server:secure
    # ... other config
```

### Platform-Specific Considerations

#### x86_64 (Intel/AMD)
- **Performance**: Higher performance, more memory
- **Dependencies**: Standard x86_64 libraries
- **Docker**: Native support, faster builds
- **CI/CD**: Use `.drone.x86.yml` pipeline

#### ARM64 (Raspberry Pi, Apple Silicon)
- **Performance**: Lower power consumption, good for edge computing
- **Dependencies**: ARM64-specific libraries
- **Docker**: Multi-arch support required
- **CI/CD**: Use `.drone.yml` pipeline (default)

### Deployment Strategies

#### Single Architecture Deployment
```bash
# Deploy to x86_64 server
drone exec .drone.x86.yml

# Deploy to ARM64 server (Raspberry Pi)
drone exec .drone.yml
```

#### Multi-Architecture Deployment
```bash
# Build and push multi-arch images
docker buildx build --platform linux/amd64,linux/arm64 --push -t your-registry/agi:latest .

# Deploy to mixed architecture cluster
kubectl apply -f k3s/  # Uses appropriate manifests
```

## 🧪 Testing Your Setup

### 1. Health Checks

```bash
# Check all services
curl http://localhost:8080/health  # Principles
curl http://localhost:8081/health  # HDN
curl http://localhost:8082/health  # Monitor
curl http://localhost:8083/health  # FSM
```

### 2. Basic Chat Test

```bash
curl -X POST http://localhost:8081/api/v1/chat/text \
  -H "Content-Type: application/json" \
  -d '{
    "message": "Hello, can you help me test this setup?",
    "session_id": "test_session"
  }'
```

### 3. Thinking Mode Test

```bash
curl -X POST http://localhost:8081/api/v1/chat \
  -H "Content-Type: application/json" \
  -d '{
    "message": "Please explain what you can do and think out loud while doing it",
    "show_thinking": true,
    "session_id": "thinking_test"
  }'
```

### 4. Tool Execution Test

```bash
curl -X POST http://localhost:8081/api/v1/interpret/execute \
  -H "Content-Type: application/json" \
  -d '{
    "input": "What tools do you have available? List them for me."
  }'
```

## 🔍 Troubleshooting

### Common Issues

#### 1. Port Conflicts
```bash
# Check what's using the ports
netstat -tulpn | grep :8080
netstat -tulpn | grep :8081
netstat -tulpn | grep :8082
netstat -tulpn | grep :8083

# Kill processes if needed
sudo kill -9 <PID>
```

#### 2. Docker Issues
```bash
# Check Docker status
docker ps
docker-compose ps

# View logs
docker-compose logs fsm-server
docker-compose logs hdn-server
docker-compose logs principles-server
```

##### macOS: Docker Desktop Daemon Unreachable
- Symptoms: `Cannot connect to the Docker daemon at unix:///Users/<you>/.docker/run/docker.sock`
- Fix:
  ```bash
  # Ensure Docker Desktop is running
  open -ga Docker

  # Use Desktop context and ensure DOCKER_HOST is not overriding
  unset DOCKER_HOST
  docker context use desktop-linux
  docker version && docker ps
  ```
- If `DOCKER_HOST` keeps reappearing, remove it from your shell profiles:
  ```bash
  grep -Hn 'DOCKER_HOST' ~/.zshrc ~/.zprofile ~/.bash_profile ~/.profile 2>/dev/null
  # Edit and remove any export lines, then open a new terminal
  ```

##### Safer Server Startup
If Compose is already running infra, prefer:
```bash
./scripts/start_servers.sh --skip-infra
```
This avoids killing Docker Desktop proxy processes on ports 8080/7474/7687.

#### 3. LLM API Issues
```bash
# Test API key
curl -H "Authorization: Bearer $OPENAI_API_KEY" \
  https://api.openai.com/v1/models

# Check environment variables
echo $OPENAI_API_KEY
echo $LLM_PROVIDER
```

#### 4. Redis/NATS Connection Issues
```bash
# Test Redis
redis-cli ping

# Test NATS
nats pub test "Hello World"
nats sub test
```

### Debug Mode

Enable debug logging:
```bash
export LOG_LEVEL=debug
export DEBUG_MODE=true
```

### Reset Everything

```bash
# Stop all services
docker-compose down

# Remove volumes (WARNING: This deletes all data)
docker-compose down -v

# Rebuild and restart
docker-compose up --build -d
```

## 📊 Monitoring

### Access the Monitor UI
Open your browser to: http://localhost:8082

### View Logs
```bash
# All services
docker-compose logs -f

# Specific service
docker-compose logs -f hdn-server

# Follow logs
tail -f logs/agi.log
```

### Metrics
```bash
# If metrics are enabled
curl http://localhost:9090/metrics
```

## 🔐 Security Considerations

### API Keys
- Never commit API keys to version control
- Use environment variables or secure key management
- Rotate keys regularly

### Network Security
- Use HTTPS in production
- Configure firewall rules
- Use VPN for remote access

### Docker Security
- Use non-root users in containers
- Keep base images updated
- Scan images for vulnerabilities

## 📚 Next Steps

1. **Explore the API**: Check out the [API Reference](API_REFERENCE.md)
2. **Learn about Thinking Mode**: Read the [Thinking Mode Guide](THINKING_MODE_README.md)
3. **Understand the Architecture**: Review the [Architecture Documentation](ARCHITECTURE.md)
4. **Contribute**: See the [Contributing Guide](CONTRIBUTING.md)

## 🆘 Getting Help

- **Issues**: Create an issue on GitHub
- **Discussions**: Use GitHub Discussions
- **Documentation**: Check the `/docs` folder
- **Examples**: Look at the `/examples` folder

---

**Welcome to the Artificial Mind Project! 🚀**

You're now ready to explore the world of AI building AI. Start with the basic examples and gradually work your way up to more complex use cases.
