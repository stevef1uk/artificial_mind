# 🚀 AGI on Kubernetes (k3s)

This directory contains Kubernetes manifests for deploying the AGI project on k3s clusters, including Raspberry Pi deployments.

## 📋 Prerequisites

- **k3s cluster** running (local or remote)
- **kubectl** configured to access your cluster
- **Docker images** built and pushed to registry
- **Storage class** available (default: `local-path`)

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

### 1. Deploy Infrastructure
```bash
# Create namespace
kubectl apply -f namespace.yaml

# Deploy persistent volumes
kubectl apply -f pvc-redis.yaml -f pvc-weaviate.yaml -f pvc-neo4j.yaml

# Deploy infrastructure services
kubectl apply -f redis.yaml -f weaviate.yaml -f neo4j.yaml -f nats.yaml
```

### 2. Deploy AGI Services
```bash
# Deploy all AGI services
kubectl apply -f principles-server.yaml
kubectl apply -f hdn-server.yaml
kubectl apply -f goal-manager.yaml
kubectl apply -f fsm-server.yaml
kubectl apply -f monitor-ui.yaml
```

### 3. Verify Deployment
```bash
# Check all resources
kubectl -n agi get pods,svc,pvc

# Check service health
kubectl -n agi get endpoints
```

## 🌐 Accessing Services

### Port Forwarding
```bash
# Monitor UI
kubectl port-forward -n agi svc/monitor-ui 8082:8082

# Principles API
kubectl port-forward -n agi svc/principles-server 8080:8080

# HDN Server
kubectl port-forward -n agi svc/hdn-server 8081:8081

# FSM Server
kubectl port-forward -n agi svc/fsm-server 8083:8083

# Neo4j Browser
kubectl port-forward -n agi svc/neo4j 7474:7474

# Weaviate
kubectl port-forward -n agi svc/weaviate 8080:8080
```

### Load Balancer (Production)
For production deployments, configure a load balancer or ingress controller to expose services externally.

## 🔧 Configuration

### Environment Variables
Each service can be configured via environment variables in the manifests:

```yaml
env:
  - name: LLM_PROVIDER
    value: "openai"
  - name: OPENAI_API_KEY
    valueFrom:
      secretKeyRef:
        name: agi-secrets
        key: openai-api-key
```

### Secrets Management
Create secrets for sensitive data:

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
kubectl -n agi logs -f deployment/hdn-server
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
kubectl -n agi exec -it deployment/hdn-server -- /bin/sh

# Check service endpoints
kubectl -n agi get endpoints
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

1. **Pod not starting**
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

## 📚 Related Documentation

- [Main README](../README.md) - Project overview and setup
- [Docker Compose](../docker-compose.yml) - Local development deployment
- [Configuration Guide](../docs/CONFIGURATION_GUIDE.md) - Detailed configuration options
- [Setup Guide](../docs/SETUP_GUIDE.md) - Complete setup instructions

## 🎯 Production Considerations

- **Resource Limits**: Set appropriate CPU/memory limits
- **Health Checks**: Configure liveness and readiness probes
- **Security**: Use RBAC, network policies, and pod security standards
- **Monitoring**: Deploy Prometheus/Grafana for observability
- **Backup**: Implement regular backup strategies
- **Updates**: Use GitOps for automated deployments