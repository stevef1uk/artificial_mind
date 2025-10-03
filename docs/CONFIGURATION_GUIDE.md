# ⚙️ Configuration Guide

This guide covers all configuration options for the AGI project, including Docker customization, LLM provider setup, and deployment scenarios.

## 📁 Environment Configuration

The AGI project uses a `.env` file for all configuration. Start by copying the example:

```bash
cp .env.example .env
```

The `.env.example` file contains comprehensive configuration options including:
- **LLM Provider Settings** (OpenAI, Anthropic, Ollama, Mock)
- **Service URLs** (Redis, NATS, Neo4j, Weaviate)
- **Database Configuration** (Neo4j credentials, Qdrant URL)
- **Docker Resource Limits** (Memory, CPU, PIDs)
- **Performance Tuning** (Concurrent executions, timeouts)
- **Security Settings** (JWT secrets, API keys)
- **Development/Production Flags**

## 🐳 Docker Configuration

### Custom Image Names and Registries

#### Environment Variables
```bash
# Docker Registry Configuration
DOCKER_REGISTRY=your-registry.com
DOCKER_NAMESPACE=your-namespace
DOCKER_TAG=latest

# Custom Image Names
FSM_IMAGE_NAME=my-agi-fsm
HDN_IMAGE_NAME=my-agi-hdn
PRINCIPLES_IMAGE_NAME=my-agi-principles
MONITOR_IMAGE_NAME=my-agi-monitor
```

#### Docker Compose Configuration
```yaml
# docker-compose.yml
version: '3.8'

services:
  fsm-server:
    image: ${DOCKER_REGISTRY:-docker.io}/${DOCKER_NAMESPACE:-agi-project}/${FSM_IMAGE_NAME:-fsm}:${DOCKER_TAG:-latest}
    build:
      context: .
      dockerfile: Dockerfile.fsm
    environment:
      - LLM_PROVIDER=${LLM_PROVIDER:-mock}
      - REDIS_HOST=${REDIS_HOST:-redis}
      - NATS_URL=${NATS_URL:-nats://nats:4222}
    ports:
      - "${FSM_PORT:-8083}:8083"
    depends_on:
      - redis
      - nats

  hdn-server:
    image: ${DOCKER_REGISTRY:-docker.io}/${DOCKER_NAMESPACE:-agi-project}/${HDN_IMAGE_NAME:-hdn}:${DOCKER_TAG:-latest}
    build:
      context: .
      dockerfile: Dockerfile.hdn
    environment:
      - LLM_PROVIDER=${LLM_PROVIDER:-mock}
      - OPENAI_API_KEY=${OPENAI_API_KEY}
      - ANTHROPIC_API_KEY=${ANTHROPIC_API_KEY}
      - REDIS_HOST=${REDIS_HOST:-redis}
      - NATS_URL=${NATS_URL:-nats://nats:4222}
    ports:
      - "${HDN_PORT:-8081}:8081"
    depends_on:
      - redis
      - nats
      - principles-server

  principles-server:
    image: ${DOCKER_REGISTRY:-docker.io}/${DOCKER_NAMESPACE:-agi-project}/${PRINCIPLES_IMAGE_NAME:-principles}:${DOCKER_TAG:-latest}
    build:
      context: .
      dockerfile: Dockerfile.principles
    environment:
      - REDIS_HOST=${REDIS_HOST:-redis}
    ports:
      - "${PRINCIPLES_PORT:-8080}:8080"
    depends_on:
      - redis

  monitor-ui:
    image: ${DOCKER_REGISTRY:-docker.io}/${DOCKER_NAMESPACE:-agi-project}/${MONITOR_IMAGE_NAME:-monitor}:${DOCKER_TAG:-latest}
    build:
      context: .
      dockerfile: Dockerfile.monitor
    environment:
      - HDN_URL=http://hdn-server:8081
      - FSM_URL=http://fsm-server:8083
      - PRINCIPLES_URL=http://principles-server:8080
    ports:
      - "${MONITOR_PORT:-8082}:8082"
    depends_on:
      - hdn-server
      - fsm-server
      - principles-server

  redis:
    image: redis:7-alpine
    ports:
      - "${REDIS_PORT:-6379}:6379"
    volumes:
      - redis_data:/data

  nats:
    image: nats:2.9-alpine
    ports:
      - "${NATS_PORT:-4222}:4222"

volumes:
  redis_data:
```

### Building Custom Images

#### Build Script
```bash
#!/bin/bash
# build-images.sh

set -e

# Configuration
REGISTRY=${DOCKER_REGISTRY:-docker.io}
NAMESPACE=${DOCKER_NAMESPACE:-agi-project}
TAG=${DOCKER_TAG:-latest}

# Image names
FSM_IMAGE="${REGISTRY}/${NAMESPACE}/fsm:${TAG}"
HDN_IMAGE="${REGISTRY}/${NAMESPACE}/hdn:${TAG}"
PRINCIPLES_IMAGE="${REGISTRY}/${NAMESPACE}/principles:${TAG}"
MONITOR_IMAGE="${REGISTRY}/${NAMESPACE}/monitor:${TAG}"

echo "Building images with registry: ${REGISTRY}/${NAMESPACE}"

# Build FSM
echo "Building FSM image..."
docker build -t "${FSM_IMAGE}" -f Dockerfile.fsm .

# Build HDN
echo "Building HDN image..."
docker build -t "${HDN_IMAGE}" -f Dockerfile.hdn .

# Build Principles
echo "Building Principles image..."
docker build -t "${PRINCIPLES_IMAGE}" -f Dockerfile.principles .

# Build Monitor
echo "Building Monitor image..."
docker build -t "${MONITOR_IMAGE}" -f Dockerfile.monitor .

echo "All images built successfully!"
echo "FSM: ${FSM_IMAGE}"
echo "HDN: ${HDN_IMAGE}"
echo "Principles: ${PRINCIPLES_IMAGE}"
echo "Monitor: ${MONITOR_IMAGE}"
```

#### Push Script
```bash
#!/bin/bash
# push-images.sh

set -e

# Load configuration
source .env

# Push all images
docker push "${DOCKER_REGISTRY}/${DOCKER_NAMESPACE}/fsm:${DOCKER_TAG}"
docker push "${DOCKER_REGISTRY}/${DOCKER_NAMESPACE}/hdn:${DOCKER_TAG}"
docker push "${DOCKER_REGISTRY}/${DOCKER_NAMESPACE}/principles:${DOCKER_TAG}"
docker push "${DOCKER_REGISTRY}/${DOCKER_NAMESPACE}/monitor:${DOCKER_TAG}"

echo "All images pushed successfully!"
```

## 🤖 LLM Provider Configuration

### OpenAI Configuration

#### Environment Variables
```bash
LLM_PROVIDER=openai
OPENAI_API_KEY=sk-your-key-here
OPENAI_MODEL=gpt-4
OPENAI_BASE_URL=https://api.openai.com/v1
OPENAI_MAX_TOKENS=4000
OPENAI_TEMPERATURE=0.7
```

#### Configuration File
```json
{
  "llm_provider": "openai",
  "openai": {
    "api_key": "sk-your-key-here",
    "model": "gpt-4",
    "base_url": "https://api.openai.com/v1",
    "max_tokens": 4000,
    "temperature": 0.7,
    "timeout": "30s"
  }
}
```

### Anthropic Configuration

#### Environment Variables
```bash
LLM_PROVIDER=anthropic
ANTHROPIC_API_KEY=sk-ant-your-key-here
ANTHROPIC_MODEL=claude-3-sonnet-20240229
ANTHROPIC_MAX_TOKENS=4000
ANTHROPIC_TEMPERATURE=0.7
```

#### Configuration File
```json
{
  "llm_provider": "anthropic",
  "anthropic": {
    "api_key": "sk-ant-your-key-here",
    "model": "claude-3-sonnet-20240229",
    "max_tokens": 4000,
    "temperature": 0.7,
    "timeout": "30s"
  }
}
```

### Ollama Configuration (Local LLM)

#### Environment Variables
```bash
LLM_PROVIDER=ollama
OLLAMA_BASE_URL=http://localhost:11434
OLLAMA_MODEL=llama2
OLLAMA_MAX_TOKENS=4000
OLLAMA_TEMPERATURE=0.7
```

#### Configuration File
```json
{
  "llm_provider": "ollama",
  "ollama": {
    "base_url": "http://localhost:11434",
    "model": "llama2",
    "max_tokens": 4000,
    "temperature": 0.7,
    "timeout": "60s"
  }
}
```

### Custom LLM Provider

#### Environment Variables
```bash
LLM_PROVIDER=custom
CUSTOM_LLM_BASE_URL=https://your-llm-api.com/v1
CUSTOM_LLM_API_KEY=your-api-key
CUSTOM_LLM_MODEL=your-model
```

#### Configuration File
```json
{
  "llm_provider": "custom",
  "custom": {
    "base_url": "https://your-llm-api.com/v1",
    "api_key": "your-api-key",
    "model": "your-model",
    "max_tokens": 4000,
    "temperature": 0.7,
    "timeout": "30s",
    "headers": {
      "Authorization": "Bearer ${CUSTOM_LLM_API_KEY}",
      "Content-Type": "application/json"
    }
  }
}
```

## 🔧 Service Configuration

### FSM Server Configuration

#### Environment Variables
```bash
# FSM Configuration
FSM_PORT=8083
FSM_LOG_LEVEL=info
FSM_REDIS_HOST=localhost
FSM_REDIS_PORT=6379
FSM_NATS_URL=nats://localhost:4222
FSM_AGENT_ID=agent_1
FSM_CONFIG_PATH=./fsm/config/artificial_mind.yaml
```

#### Configuration File
```yaml
# fsm/config/artificial_mind.yaml
agent_id: "agent_1"
log_level: "info"
performance:
  state_transition_delay: 0.1
  max_concurrent_actions: 10

redis:
  host: "localhost"
  port: 6379
  db: 0

nats:
  url: "nats://localhost:4222"
  cluster_id: "agi-cluster"

states:
  - name: idle
    description: "Wait for input or timer events"
    timeout_ms: 1000
    # ... rest of state configuration
```

### HDN Server Configuration

#### Environment Variables
```bash
# HDN Configuration
HDN_PORT=8081
HDN_LOG_LEVEL=info
HDN_REDIS_HOST=localhost
HDN_REDIS_PORT=6379
HDN_NATS_URL=nats://localhost:4222
HDN_PRINCIPLES_URL=http://localhost:8080
HDN_LLM_PROVIDER=openai
HDN_OPENAI_API_KEY=your-key-here
```

#### Configuration File
```json
{
  "server": {
    "port": 8081,
    "host": "0.0.0.0",
    "log_level": "info"
  },
  "redis": {
    "host": "localhost",
    "port": 6379,
    "db": 0
  },
  "nats": {
    "url": "nats://localhost:4222"
  },
  "principles": {
    "url": "http://localhost:8080"
  },
  "llm_provider": "openai",
  "openai": {
    "api_key": "your-key-here",
    "model": "gpt-4"
  }
}
```

### Principles Server Configuration

#### Environment Variables
```bash
# Principles Configuration
PRINCIPLES_PORT=8080
PRINCIPLES_LOG_LEVEL=info
PRINCIPLES_REDIS_HOST=localhost
PRINCIPLES_REDIS_PORT=6379
PRINCIPLES_CONFIG_PATH=./principles/config/principles.json
```

#### Configuration File
```json
[
  {
    "name": "FirstLaw",
    "priority": 1,
    "action": "*",
    "condition": "human_harm==true",
    "deny_message": "Action would harm a human (First Law)"
  },
  {
    "name": "SecondLaw",
    "priority": 2,
    "action": "*",
    "condition": "obedience_conflict==true",
    "deny_message": "Action conflicts with human orders (Second Law)"
  },
  {
    "name": "ThirdLaw",
    "priority": 3,
    "action": "*",
    "condition": "self_preservation==true",
    "deny_message": "Action would harm the AI (Third Law)"
  }
]
```

## 🌐 Deployment Scenarios

### Development Environment

#### docker-compose.dev.yml
```yaml
version: '3.8'

services:
  fsm-server:
    build:
      context: .
      dockerfile: Dockerfile.fsm
    environment:
      - LOG_LEVEL=debug
      - DEBUG_MODE=true
    volumes:
      - ./fsm:/app/fsm
      - ./logs:/app/logs
    ports:
      - "8083:8083"

  hdn-server:
    build:
      context: .
      dockerfile: Dockerfile.hdn
    environment:
      - LOG_LEVEL=debug
      - DEBUG_MODE=true
    volumes:
      - ./hdn:/app/hdn
      - ./logs:/app/logs
    ports:
      - "8081:8081"

  # ... other services
```

### Production Environment

#### docker-compose.prod.yml
```yaml
version: '3.8'

services:
  fsm-server:
    image: your-registry.com/agi-project/fsm:latest
    environment:
      - LOG_LEVEL=info
      - DEBUG_MODE=false
    restart: unless-stopped
    ports:
      - "8083:8083"
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:8083/health"]
      interval: 30s
      timeout: 10s
      retries: 3

  hdn-server:
    image: your-registry.com/agi-project/hdn:latest
    environment:
      - LOG_LEVEL=info
      - DEBUG_MODE=false
    restart: unless-stopped
    ports:
      - "8081:8081"
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:8081/health"]
      interval: 30s
      timeout: 10s
      retries: 3

  # ... other services
```

### Kubernetes Deployment

#### k8s/namespace.yaml
```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: agi-project
```

#### k8s/configmap.yaml
```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: agi-config
  namespace: agi-project
data:
  LLM_PROVIDER: "openai"
  REDIS_HOST: "redis-service"
  NATS_URL: "nats://nats-service:4222"
  LOG_LEVEL: "info"
```

#### k8s/secret.yaml
```yaml
apiVersion: v1
kind: Secret
metadata:
  name: agi-secrets
  namespace: agi-project
type: Opaque
data:
  OPENAI_API_KEY: <base64-encoded-key>
  JWT_SECRET: <base64-encoded-secret>
```

#### k8s/deployment.yaml
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: fsm-server
  namespace: agi-project
spec:
  replicas: 2
  selector:
    matchLabels:
      app: fsm-server
  template:
    metadata:
      labels:
        app: fsm-server
    spec:
      containers:
      - name: fsm-server
        image: your-registry.com/agi-project/fsm:latest
        ports:
        - containerPort: 8083
        envFrom:
        - configMapRef:
            name: agi-config
        - secretRef:
            name: agi-secrets
        resources:
          requests:
            memory: "256Mi"
            cpu: "250m"
          limits:
            memory: "512Mi"
            cpu: "500m"
```

## 🔐 Security Configuration

### API Authentication

#### JWT Configuration
```bash
JWT_SECRET=your-super-secret-jwt-key-here
JWT_EXPIRY=24h
JWT_ISSUER=agi-project
```

#### API Key Configuration
```bash
API_KEY_HEADER=X-API-Key
API_KEY_VALUE=your-api-key-here
ENABLE_API_AUTH=true
```

### Network Security

#### CORS Configuration
```bash
ENABLE_CORS=true
CORS_ORIGINS=http://localhost:3000,https://yourdomain.com
CORS_METHODS=GET,POST,PUT,DELETE,OPTIONS
CORS_HEADERS=Content-Type,Authorization,X-API-Key
```

#### SSL/TLS Configuration
```bash
ENABLE_SSL=true
SSL_CERT_PATH=/certs/cert.pem
SSL_KEY_PATH=/certs/key.pem
SSL_PORT=8443
```

### Docker Security

#### Security Context
```yaml
securityContext:
  runAsNonRoot: true
  runAsUser: 1000
  allowPrivilegeEscalation: false
  readOnlyRootFilesystem: true
  capabilities:
    drop:
    - ALL
```

## 📊 Monitoring Configuration

### Metrics Configuration
```bash
ENABLE_METRICS=true
METRICS_PORT=9090
METRICS_PATH=/metrics
```

### Logging Configuration
```bash
LOG_LEVEL=info
LOG_FORMAT=json
LOG_OUTPUT=stdout
LOG_FILE_PATH=/app/logs/agi.log
```

### Health Check Configuration
```yaml
healthcheck:
  test: ["CMD", "curl", "-f", "http://localhost:8080/health"]
  interval: 30s
  timeout: 10s
  retries: 3
  start_period: 40s
```

## 🚀 Performance Tuning

### Resource Limits
```yaml
resources:
  requests:
    memory: "256Mi"
    cpu: "250m"
  limits:
    memory: "512Mi"
    cpu: "500m"
```

### Scaling Configuration
```yaml
# Horizontal Pod Autoscaler
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: fsm-server-hpa
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: fsm-server
  minReplicas: 2
  maxReplicas: 10
  metrics:
  - type: Resource
    resource:
      name: cpu
      target:
        type: Utilization
        averageUtilization: 70
```

This configuration guide provides comprehensive options for customizing the AGI project to fit your specific needs, whether you're running it locally, in production, or in a Kubernetes cluster.
