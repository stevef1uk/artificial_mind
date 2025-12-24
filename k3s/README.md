# 🚀 AGI on Kubernetes (k3s)

This directory contains Kubernetes manifests for deploying the AGI project on k3s clusters, including Raspberry Pi deployments.

## 📋 Prerequisites

- **k3s cluster** running (local or remote)
- **kubectl** configured to access your cluster
- **Docker images** built and pushed to registry
- **Storage class** available (default: `local-path`)
- **Security keys** generated (see [Security Setup](#-security-setup) below)

## 🏗️ Deployment Architecture

### Infrastructure Services
- **Redis** - Working memory and caching
- **Weaviate** - Vector database for semantic search
- **Neo4j** - Graph database for knowledge representation
- **NATS** - Event streaming and messaging

### AGI Services
- **Principles Server** - Ethical decision-making (Port 8080)
- **HDN Server** - AI planning and execution (Port 8081)
- **FSM Server** - Cognitive state management (Port 8083)
- **Goal Manager** - Goal tracking and prioritization
- **Monitor UI** - Real-time visualization (Port 8082)

## 🚀 Quick Deployment

### 0. Security Setup (First Time Only)

**Before deploying**, set up security keys and tokens:

```bash
# On your build machine
cd ~/dev/artificial_mind
./scripts/create-secure-files.sh

# Copy secure directory to Raspberry Pi (if building remotely)
# Then on Raspberry Pi:
cd ~/dev/artificial_mind/k3s
./generate-vendor-token.sh ~/dev/artificial_mind/secure
./update-secrets.sh ~/dev/artificial_mind/secure
```

See [Security Setup](#-security-setup) section for detailed instructions.

### 1. Deploy Infrastructure
```bash
# Create namespace
kubectl apply -f namespace.yaml

# Deploy persistent volumes
kubectl apply -f pvc-redis.yaml -f pvc-weaviate.yaml -f pvc-neo4j.yaml

# Deploy infrastructure services
kubectl apply -f redis.yaml -f weaviate.yaml -f neo4j.yaml -f nats.yaml
```

### 2. Configure LLM (Required)
```bash
# Create LLM configuration secret
kubectl apply -f llm-config-secret.yaml

# If using llama.cpp, create the service (update IP address first!)
kubectl apply -f llama-server-service.yaml
```

### 3. Deploy AGI Services
```bash
# Deploy all AGI services
kubectl apply -f principles-server.yaml
kubectl apply -f hdn-server-rpi58.yaml  # Use -rpi58 variant for ARM64
kubectl apply -f goal-manager.yaml
kubectl apply -f fsm-server-rpi58.yaml  # Use -rpi58 variant for ARM64
kubectl apply -f monitor-ui.yaml

# Apply RBAC for monitor-ui
kubectl apply -f monitor-ui-rbac.yaml
```

### 4. Bootstrap Tools (Required)

Tools need to be registered in HDN. They should bootstrap automatically, but if not:

```bash
# Option 1: Trigger tool discovery (registers 3 tools)
cd ~/dev/artificial_mind/k3s
./bootstrap-tools.sh

# Option 2: Register all default tools (recommended)
./register-all-tools.sh
```

### 5. Verify Deployment
```bash
# Check all resources
kubectl -n agi get pods,svc,pvc

# Check service health
kubectl -n agi get endpoints

# Check tools are registered
kubectl exec -n agi deployment/redis -- redis-cli SMEMBERS tools:registry

# Check NATS connectivity
./check-nats-connectivity.sh
```

## 🌐 Accessing Services

### Port Forwarding

**From your local machine (via SSH):**
```bash
# SSH with port forwarding
ssh -L 8082:localhost:8082 pi@192.168.1.63 "kubectl port-forward -n agi svc/monitor-ui 8082:8082"

# Or use NodePort (monitor-ui is configured as NodePort on port 30082)
# Access directly: http://192.168.1.63:30082
```

**Direct port forwarding (on Raspberry Pi):**
```bash
# Monitor UI
kubectl port-forward -n agi svc/monitor-ui 8082:8082

# HDN Server
kubectl port-forward -n agi svc/hdn-server-rpi58 8080:8080

# FSM Server
kubectl port-forward -n agi svc/fsm-server-rpi58 8083:8083

# Principles API
kubectl port-forward -n agi svc/principles-server 8080:8080

# Neo4j Browser
kubectl port-forward -n agi svc/neo4j 7474:7474

# Weaviate
kubectl port-forward -n agi svc/weaviate 8080:8080

# NATS Monitoring
kubectl port-forward -n agi svc/nats 8223:8223
```

### Service URLs

Services are accessible within the cluster using these DNS names:
- HDN: `http://hdn-server-rpi58.agi.svc.cluster.local:8080`
- FSM: `http://fsm-server-rpi58.agi.svc.cluster.local:8083`
- Principles: `http://principles-server.agi.svc.cluster.local:8080`
- Goal Manager: `http://goal-manager.agi.svc.cluster.local:8090`
- Monitor UI: `http://monitor-ui.agi.svc.cluster.local:8082`
- Redis: `redis://redis.agi.svc.cluster.local:6379`
- Neo4j: `bolt://neo4j.agi.svc.cluster.local:7687` (Bolt) or `http://neo4j.agi.svc.cluster.local:7474` (HTTP)
- Weaviate: `http://weaviate.agi.svc.cluster.local:8080`
- NATS: `nats://nats.agi.svc.cluster.local:4222` (client) or `http://nats.agi.svc.cluster.local:8223` (monitoring)

### Load Balancer (Production)
For production deployments, configure a load balancer or ingress controller to expose services externally.

## 🔧 Configuration

### LLM Configuration (Using Secrets)

**LLM settings are now managed via Kubernetes secrets**, allowing you to change the LLM provider and model without editing deployment YAML files.

#### Initial Setup

1. **Create the LLM configuration secret**:
   ```bash
   kubectl apply -f k3s/llm-config-secret.yaml
   ```

2. **Create the llama-server service** (if using llama.cpp):
   ```bash
   # Edit llama-server-service.yaml to set the correct IP address
   kubectl apply -f k3s/llama-server-service.yaml
   ```

3. **Deploy services** (they will automatically use the secret):
   ```bash
   kubectl apply -f k3s/hdn-server-rpi58.yaml
   kubectl apply -f k3s/fsm-server-rpi58.yaml
   ```

#### Changing LLM Settings

To switch LLM providers or models, simply update the secret:

```bash
# Edit the secret directly
kubectl edit secret llm-config -n agi

# Or use patch commands (see LLM_CONFIG_SECRET.md for examples)
```

Then restart the pods to pick up new values:

```bash
kubectl rollout restart deployment/hdn-server-rpi58 -n agi
kubectl rollout restart deployment/fsm-server-rpi58 -n agi
```

#### Supported LLM Providers

- **OpenAI/OpenAI-compatible** (e.g., llama.cpp):
  - Set `LLM_PROVIDER=openai`
  - Set `OPENAI_BASE_URL` to your server URL
  - Example: `http://llama-server.agi.svc.cluster.local:8085`

- **Ollama**:
  - Set `LLM_PROVIDER=local` or `ollama`
  - Set `OLLAMA_BASE_URL` to your Ollama server
  - Example: `http://ollama.agi.svc.cluster.local:11434`

- **OpenAI API**:
  - Set `LLM_PROVIDER=openai`
  - Set `OPENAI_BASE_URL=https://api.openai.com`
  - Set `LLM_API_KEY` in the secret (if needed)

For detailed instructions, see [`k3s/LLM_CONFIG_SECRET.md`](LLM_CONFIG_SECRET.md).

### Environment Variables
Each service can be configured via environment variables in the manifests. LLM-related variables are now sourced from the `llm-config` secret (see above).

```yaml
env:
  - name: LLM_PROVIDER
    valueFrom:
      secretKeyRef:
        name: llm-config
        key: LLM_PROVIDER
  - name: LLM_MODEL
    valueFrom:
      secretKeyRef:
        name: llm-config
        key: LLM_MODEL
```

### Secrets Management

#### Security Keys and Tokens (Required for Secure Containers)

The secure container images require cryptographic keys and tokens to unpack and run. Follow these steps:

**1. Generate Security Keys**

On your build machine, generate the required keys:

```bash
# Navigate to project root
cd ~/dev/artificial_mind

# Run the secure files creation script
./scripts/create-secure-files.sh
```

This creates:
- `secure/customer_private.pem` - Customer private key (keep secret!)
- `secure/customer_public.pem` - Customer public key (used during build)
- `secure/vendor_private.pem` - Vendor private key (keep secret!)
- `secure/vendor_public.pem` - Vendor public key (used during build)

**2. Generate Vendor Token**

On your Raspberry Pi (or wherever you'll deploy), generate the vendor token:

```bash
cd ~/dev/artificial_mind/k3s

# Generate vendor token (requires vendor_private.pem)
./generate-vendor-token.sh ~/dev/artificial_mind/secure
```

This creates `secure/token.txt` with a properly signed token.

**3. Update Kubernetes Secrets**

Copy the keys to your Raspberry Pi (if built elsewhere), then update the secrets:

```bash
cd ~/dev/artificial_mind/k3s

# Update secrets with new keys and token
./update-secrets.sh ~/dev/artificial_mind/secure
```

This updates:
- `secure-customer-private` - Customer private key (mounted in pods)
- `secure-vendor` - Vendor token (used for license validation)

**4. Verify Secrets**

```bash
# Check secrets exist
kubectl get secrets -n agi | grep secure

# Verify secret contents (base64 encoded)
kubectl get secret secure-customer-private -n agi -o yaml
kubectl get secret secure-vendor -n agi -o yaml
```

**Important Notes:**
- The `customer_private.pem` must match the `customer_public.pem` used during Docker image build
- The `vendor_private.pem` must match the `vendor_public.pem` used during Docker image build
- The vendor token must be generated with the same `vendor_private.pem` that matches the build
- Keep private keys secure and never commit them to git

#### Other Secrets

Create additional secrets for sensitive data:

```bash
# Create secrets
kubectl create secret generic agi-secrets \
  --from-literal=openai-api-key=your-key-here \
  --from-literal=anthropic-api-key=your-key-here \
  -n agi
```

### Storage Configuration
- **Default StorageClass**: `local-path` (adjust for your environment)
- **PVC Sizes**: Configured for development (adjust for production)
- **Data Persistence**: All data stored in persistent volumes

## 🏷️ Specialized Deployments

### Raspberry Pi (ARM64)
Use the `-rpi58.yaml` variants for Raspberry Pi deployments. These manifests are optimized for ARM and assume Drone/SSH execution.

```bash
# Deploy ARM64 versions
kubectl apply -f hdn-server-rpi58.yaml
kubectl apply -f fsm-server-rpi58.yaml
```

### Node placement and workload distribution
These manifests intentionally pin workloads to specific nodes to balance load across your cluster. This is done using `nodeSelector` (and optional tolerations) in each Deployment.

Example (from `k3s/hdn-server-rpi58.yaml`):

```yaml
spec:
  template:
    spec:
      nodeSelector:
        kubernetes.io/hostname: rpi58
      tolerations:
        - key: "node-role.kubernetes.io/control-plane"
          operator: "Exists"
          effect: "NoSchedule"
```

To adapt for your cluster:
- Label your nodes with stable labels (Kubernetes provides `kubernetes.io/hostname` by default):
  - `kubectl get nodes --show-labels`
  - `kubectl label nodes rpi58 kubernetes.io/hostname=rpi58` (if needed)
- Edit each manifest’s `nodeSelector` to the node you want that service to run on (e.g., pin FSM to `rpi5b`, HDN to `rpi58`).
- Optionally use Affinity/Anti-Affinity or TopologySpreadConstraints for softer placement instead of hard pinning.

Service → default node mapping (example):
- HDN Server: `kubernetes.io/hostname: rpi58`
- FSM Server: `kubernetes.io/hostname: rpi5b`
- Databases (Redis/Neo4j/Weaviate/NATS): distributed across nodes per your storage and performance needs.

### Cron Jobs
Deploy background processing jobs:

```bash
# News processing
kubectl apply -f news-ingestor-cronjob.yaml

# Knowledge processing
kubectl apply -f wiki-bootstrapper-cronjob.yaml
kubectl apply -f wiki-summarizer-cronjob.yaml
```

## 🔍 Monitoring & Debugging

### Check Pod Status
```bash
# All pods
kubectl -n agi get pods

# Pod logs
kubectl -n agi logs -f deployment/hdn-server-rpi58
kubectl -n agi logs -f deployment/principles-server
```

### Resource Usage
```bash
# Resource consumption
kubectl -n agi top pods
kubectl -n agi top nodes
```

### Debugging
```bash
# Execute commands in pods
kubectl -n agi exec -it deployment/hdn-server-rpi58 -- /bin/sh

# Check service endpoints
kubectl -n agi get endpoints
```

## 🛠️ Helper Scripts

This directory includes several helper scripts for managing the deployment:

### Security Scripts

**`generate-vendor-token.sh`** - Generate vendor token for secure containers
```bash
# Generate vendor token (requires vendor_private.pem)
./generate-vendor-token.sh ~/dev/artificial_mind/secure

# Options:
#   -expiry 2025-12-31    # Token expiry date
#   -company SJFisher     # Company name
#   -email stevef@sjfisher.com  # Contact email
```

**`update-secrets.sh`** - Update Kubernetes secrets with new keys
```bash
# Update secrets from secure directory
./update-secrets.sh ~/dev/artificial_mind/secure

# This updates:
#   - secure-customer-private (customer private key)
#   - secure-vendor (vendor token)
```

### Tool Management Scripts

**`bootstrap-tools.sh`** - Bootstrap tools in HDN server
```bash
# Register tools via tool discovery endpoint
./bootstrap-tools.sh

# This triggers /api/v1/tools/discover which registers:
#   - tool_http_get
#   - tool_wiki_bootstrapper
#   - tool_ssh_executor (if on ARM64)
```

**`register-all-tools.sh`** - Register all default tools
```bash
# Register all 11+ default tools that should be available
./register-all-tools.sh

# This registers tools like:
#   - tool_html_scraper
#   - tool_file_read, tool_file_write
#   - tool_ls, tool_exec
#   - tool_docker_list, tool_codegen
#   - And more...
```

**Note:** Tools should bootstrap automatically on HDN startup, but if they don't appear, run these scripts.

### Diagnostic Scripts

**`check-nats-connectivity.sh`** - Check NATS connectivity and connections
```bash
# Comprehensive NATS connectivity check
./check-nats-connectivity.sh

# Checks:
#   - NATS pod status
#   - Service configuration
#   - DNS resolution
#   - Network connectivity from service pods
#   - Active connections
#   - Service logs for NATS messages
```

**`check-nats-usage.sh`** - Check NATS usage and subscriptions
```bash
# Detailed NATS usage analysis
./check-nats-usage.sh

# Shows:
#   - Active connections with message counts
#   - Subscriptions and subjects
#   - Connection details (IP, CID, etc.)
#   - Service activity logs
```

### Configuration Scripts

**`setup-fsm-config.sh`** - Setup FSM configuration and secrets
```bash
# Creates FSM ConfigMap and required secrets
./setup-fsm-config.sh
```

**`setup-fsm-only.sh`** - Setup only FSM configuration
```bash
# Creates FSM ConfigMap only
./setup-fsm-only.sh
```

## 🔄 Updates & Maintenance

### Rolling Updates
```bash
# Update a service
kubectl -n agi set image deployment/hdn-server hdn-server=your-registry/agi-hdn:latest

# Check rollout status
kubectl -n agi rollout status deployment/hdn-server
```

### Scaling
```bash
# Scale services
kubectl -n agi scale deployment/hdn-server --replicas=3
```

### Backup
```bash
# Backup persistent volumes
kubectl -n agi get pvc
# Use your preferred backup solution for the underlying storage
```

## 🚨 Troubleshooting

### Common Issues

1. **Pod not starting / Container won't unpack**
   - **Cause:** Missing or incorrect security keys/tokens
   - **Fix:**
     ```bash
     # Check secrets exist
     kubectl get secrets -n agi | grep secure
     
     # Regenerate and update secrets
     ./generate-vendor-token.sh ~/dev/artificial_mind/secure
     ./update-secrets.sh ~/dev/artificial_mind/secure
     
     # Restart pods
     kubectl rollout restart deployment/<service-name> -n agi
     ```
   - **Check logs:**
     ```bash
     kubectl -n agi describe pod <pod-name>
     kubectl -n agi logs <pod-name>
     ```

2. **Service not accessible**
   ```bash
   kubectl -n agi get svc
   kubectl -n agi get endpoints
   ```

3. **Storage issues**
   ```bash
   kubectl -n agi get pvc
   kubectl -n agi describe pvc <pvc-name>
   ```

4. **Resource constraints**
   ```bash
   kubectl -n agi top pods
   kubectl -n agi describe nodes
   ```

5. **No tools showing in Monitor UI**
   - **Cause:** Tools not bootstrapped on HDN startup
   - **Fix:**
     ```bash
     cd ~/dev/artificial_mind/k3s
     ./register-all-tools.sh
     ```
   - **Verify:**
     ```bash
     kubectl exec -n agi deployment/redis -- redis-cli SMEMBERS tools:registry
     ```

6. **NATS showing as unhealthy in Monitor UI**
   - **Cause:** Monitor UI using wrong URL format
   - **Fix:**
     ```bash
     # Update environment variable
     kubectl set env deployment/monitor-ui -n agi NATS_URL="http://nats.agi.svc.cluster.local:8223"
     kubectl rollout restart deployment/monitor-ui -n agi
     ```
   - **Check connectivity:**
     ```bash
     ./check-nats-connectivity.sh
     ```

7. **Neo4j showing as unhealthy in Monitor UI**
   - **Cause:** Monitor UI using Bolt URL instead of HTTP
   - **Fix:**
     ```bash
     # Update environment variable
     kubectl set env deployment/monitor-ui -n agi NEO4J_URL="http://neo4j.agi.svc.cluster.local:7474"
     kubectl rollout restart deployment/monitor-ui -n agi
     ```

## 🔐 Security Setup

### Initial Setup

**Step 1: Generate Security Keys (Build Machine)**

```bash
cd ~/dev/artificial_mind
./scripts/create-secure-files.sh
```

This creates:
- `secure/customer_private.pem` - **Keep secret!** Required for containers to unpack
- `secure/customer_public.pem` - Used during Docker image build
- `secure/vendor_private.pem` - **Keep secret!** Used to sign tokens
- `secure/vendor_public.pem` - Used during Docker image build

**Step 2: Build Docker Images**

```bash
# Build images with public keys (on build machine)
make docker-build-push
```

The build process uses:
- `secure/customer_public.pem` - Encrypts payload for customer
- `secure/vendor_public.pem` - Validates license tokens

**Step 3: Generate Vendor Token (Deployment Machine)**

On your Raspberry Pi (or deployment machine):

```bash
cd ~/dev/artificial_mind/k3s

# Generate vendor token (requires vendor_private.pem)
./generate-vendor-token.sh ~/dev/artificial_mind/secure

# With custom options:
docker run --rm \
  -v ~/dev/artificial_mind/secure:/keys:ro \
  stevef1uk/secure-packager:latest \
  issue-token \
    -priv /keys/vendor_private.pem \
    -expiry 2025-12-31 \
    -company SJFisher \
    -email stevef@sjfisher.com \
    -out /keys/token.txt
```

**Step 4: Update Kubernetes Secrets**

```bash
cd ~/dev/artificial_mind/k3s

# Update secrets with keys and token
./update-secrets.sh ~/dev/artificial_mind/secure
```

This creates/updates:
- `secure-customer-private` - Contains `customer_private.pem` (mounted at `/keys/customer_private.pem`)
- `secure-vendor` - Contains `token.txt` (used as `SECURE_VENDOR_TOKEN` env var)

**Step 5: Verify Secrets**

```bash
# List secrets
kubectl get secrets -n agi | grep secure

# Verify customer private key
kubectl get secret secure-customer-private -n agi -o jsonpath='{.data.customer_private\.pem}' | base64 -d | head -5

# Verify vendor token
kubectl get secret secure-vendor -n agi -o jsonpath='{.data.token}' | base64 -d | head -c 50
```

### Updating Keys

If you need to regenerate keys (e.g., after rebuilding images):

1. **Generate new keys** (on build machine):
   ```bash
   ./scripts/create-secure-files.sh  # This overwrites existing keys
   ```

2. **Rebuild images** with new public keys:
   ```bash
   make docker-build-push
   ```

3. **Generate new token** (on deployment machine):
   ```bash
   ./generate-vendor-token.sh ~/dev/artificial_mind/secure
   ```

4. **Update secrets**:
   ```bash
   ./update-secrets.sh ~/dev/artificial_mind/secure
   ```

5. **Restart pods** to pick up new keys:
   ```bash
   kubectl rollout restart deployment/hdn-server-rpi58 -n agi
   kubectl rollout restart deployment/fsm-server-rpi58 -n agi
   kubectl rollout restart deployment/principles-server -n agi
   kubectl rollout restart deployment/goal-manager -n agi
   kubectl rollout restart deployment/monitor-ui -n agi
   ```

### Security Best Practices

- **Never commit private keys to git** - The `secure/` directory is excluded
- **Use separate keys for production** - Don't reuse development keys
- **Rotate keys periodically** - Regenerate keys and tokens on a schedule
- **Secure key storage** - Use Kubernetes secrets (encrypted at rest if configured)
- **Limit access** - Only authorized personnel should have access to private keys

## 📚 Related Documentation

- [Main README](../README.md) - Project overview and setup
- [Docker Compose](../docker-compose.yml) - Local development deployment
- [Configuration Guide](../docs/CONFIGURATION_GUIDE.md) - Detailed configuration options
- [Setup Guide](../docs/SETUP_GUIDE.md) - Complete setup instructions
- [Secure Packaging Guide](../docs/SECURE_PACKAGING_GUIDE.md) - Secure container details
- [LLM Configuration Secret](LLM_CONFIG_SECRET.md) - How to configure LLM settings using Kubernetes secrets

## 🎯 Production Considerations

- **Resource Limits**: Set appropriate CPU/memory limits
- **Health Checks**: Configure liveness and readiness probes
- **Security**: Use RBAC, network policies, and pod security standards
- **Monitoring**: Deploy Prometheus/Grafana for observability
- **Backup**: Implement regular backup strategies
- **Updates**: Use GitOps for automated deployments