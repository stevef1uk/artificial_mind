# Quick Test: Tool Creation Agent on Kubernetes

## 1. Build and Deploy

```bash
# Build the image
docker build -f Dockerfile.hdn.secure -t stevef1uk/hdn-server:secure .

# Push to registry
docker push stevef1uk/hdn-server:secure

# Restart deployment to pull new image
kubectl rollout restart deployment/hdn-server-rpi58 -n agi
kubectl rollout status deployment/hdn-server-rpi58 -n agi
```

## 2. Run Test Script

```bash
./k3s/test_tool_creation_agent_k8s.sh
```

## 3. Manual Test

```bash
# Port-forward
kubectl port-forward -n agi deployment/hdn-server-rpi58 8080:8080 &

# Execute task
curl -X POST http://localhost:8080/api/v1/intelligent/execute \
  -H "Content-Type: application/json" \
  -d '{
    "task_name": "JSONParser",
    "description": "Create a Python function that parses JSON data",
    "context": {"input": "{\"test\":123}"},
    "language": "python",
    "force_regenerate": true
  }'

# Check logs
kubectl logs -n agi -l app=hdn-server-rpi58 --tail=200 | grep TOOL-CREATOR

# List tools
curl http://localhost:8080/api/v1/tools | jq '.tools[] | select(.created_by == "agent")'
```

## 4. Monitor Logs

```bash
kubectl logs -n agi -l app=hdn-server-rpi58 -f | grep TOOL-CREATOR
```
