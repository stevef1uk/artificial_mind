# Testing Tool Creation Agent on Kubernetes

## Prerequisites

1. **Kubernetes cluster** running (k3s)
2. **kubectl** configured to access your cluster
3. **HDN server deployed** in the `agi` namespace
4. **Updated code** built and pushed to Docker registry

## Step 1: Build and Push Updated Docker Image

The tool creation agent code needs to be in the Docker image:

```bash
# Build the HDN server image with the new code
cd ~/dev/artificial_mind
docker build -f Dockerfile.hdn.secure -t stevef1uk/hdn-server:secure .

# Push to registry
docker push stevef1uk/hdn-server:secure
```

## Step 2: Deploy/Update HDN Server

If the deployment already exists, restart it to pick up the new image:

```bash
# Restart the deployment to pull the new image
kubectl rollout restart deployment/hdn-server-rpi58 -n agi

# Wait for rollout to complete
kubectl rollout status deployment/hdn-server-rpi58 -n agi

# Verify pod is running
kubectl get pods -n agi -l app=hdn-server-rpi58
```

If deploying for the first time:

```bash
# Apply the deployment
kubectl apply -f k3s/hdn-server-rpi58.yaml

# Wait for pod to be ready
kubectl wait --for=condition=ready pod -l app=hdn-server-rpi58 -n agi --timeout=300s
```

## Step 3: Run the Test Script

Use the provided Kubernetes test script:

```bash
cd ~/dev/artificial_mind
./k3s/test_tool_creation_agent_k8s.sh
```

This script will:
1. Set up port-forward to the HDN server pod
2. Execute tasks that should generate reusable code
3. Check logs for tool creation activity
4. List agent-created tools
5. Clean up port-forward automatically

## Step 4: Monitor Logs

### Real-time Monitoring

```bash
# Watch tool creation logs in real-time
kubectl logs -n agi -l app=hdn-server-rpi58 -f | grep TOOL-CREATOR
```

### Check Recent Activity

```bash
# View recent tool creation logs
kubectl logs -n agi -l app=hdn-server-rpi58 --tail=500 | grep -E "(TOOL-CREATOR|considerToolCreation|LLM.*recommends)"
```

### View All Logs

```bash
# View all recent logs
kubectl logs -n agi -l app=hdn-server-rpi58 --tail=200
```

## Step 5: Verify Tools Were Created

### Via Port-Forward

```bash
# Set up port-forward
kubectl port-forward -n agi deployment/hdn-server-rpi58 8080:8080

# In another terminal, list tools
curl http://localhost:8080/api/v1/tools | jq '.tools[] | select(.created_by == "agent")'
```

### Via kubectl exec

```bash
# Get pod name
POD=$(kubectl get pods -n agi -l app=hdn-server-rpi58 -o jsonpath='{.items[0].metadata.name}')

# Execute curl inside the pod
kubectl exec -n agi $POD -- curl -s http://localhost:8080/api/v1/tools | jq '.tools[] | select(.created_by == "agent")'
```

## Step 6: Test Created Tools

### Test a Created Tool

```bash
# Set up port-forward
kubectl port-forward -n agi deployment/hdn-server-rpi58 8080:8080

# Invoke a tool (replace tool_id with actual tool ID)
curl -X POST http://localhost:8080/api/v1/tools/tool_parsejsondata/invoke \
  -H "Content-Type: application/json" \
  -d '{"input":"{\"test\":123}"}'
```

## Troubleshooting

### Tool Creation Not Happening

1. **Check if LLM client is available:**
   ```bash
   kubectl logs -n agi -l app=hdn-server-rpi58 --tail=100 | grep "LLM client not available"
   ```

2. **Check if code executions are successful:**
   ```bash
   kubectl logs -n agi -l app=hdn-server-rpi58 --tail=200 | grep "✅.*execution.*success"
   ```

3. **Check LLM evaluation:**
   ```bash
   kubectl logs -n agi -l app=hdn-server-rpi58 --tail=500 | grep -E "(buildToolEvaluationPrompt|LLM.*recommends)"
   ```

### Tool Registration Fails

1. **Check HDN base URL:**
   ```bash
   kubectl logs -n agi -l app=hdn-server-rpi58 --tail=100 | grep "HDN base URL"
   ```

2. **Check API endpoint:**
   ```bash
   kubectl exec -n agi $POD -- curl -s http://localhost:8080/api/v1/tools
   ```

3. **Check for 404 errors:**
   ```bash
   kubectl logs -n agi -l app=hdn-server-rpi58 --tail=200 | grep "tool registration failed"
   ```

### Background LLM Disabled

If `DISABLE_BACKGROUND_LLM=1` is set, tool creation won't work (it uses low priority):

```bash
# Check environment variables
kubectl get deployment hdn-server-rpi58 -n agi -o jsonpath='{.spec.template.spec.containers[0].env}' | jq '.[] | select(.name == "DISABLE_BACKGROUND_LLM")'
```

## Manual Testing

### Execute a Task Directly

```bash
# Port-forward
kubectl port-forward -n agi deployment/hdn-server-rpi58 8080:8080

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
```

### Check Logs After Execution

```bash
# Wait a few seconds, then check logs
sleep 5
kubectl logs -n agi -l app=hdn-server-rpi58 --tail=100 | grep -E "(TOOL-CREATOR|recordSuccessfulExecution)"
```

## Expected Results

✅ **Success indicators:**
- Tasks execute successfully
- Logs show: `✅ [TOOL-CREATOR] LLM recommends tool creation`
- Logs show: `✅ [TOOL-CREATOR] Successfully created and registered tool`
- Tools appear in `/api/v1/tools` with `created_by: "agent"`
- Tools have `exec.type: "code"` for dynamic execution
- Tools can be invoked successfully

❌ **Failure indicators:**
- `⚠️ [TOOL-CREATOR] LLM client not available`
- `⚠️ [TOOL-CREATOR] LLM evaluation failed`
- `⚠️ [TOOL-CREATOR] Failed to register tool`
- No tools with `created_by: "agent"` in tools list

## Quick Test Command

One-liner to test everything:

```bash
kubectl port-forward -n agi deployment/hdn-server-rpi58 8080:8080 & \
sleep 3 && \
curl -X POST http://localhost:8080/api/v1/intelligent/execute \
  -H "Content-Type: application/json" \
  -d '{"task_name":"TestTool","description":"Create a Python function that processes data","context":{},"language":"python","force_regenerate":true}' && \
sleep 5 && \
kubectl logs -n agi -l app=hdn-server-rpi58 --tail=200 | grep TOOL-CREATOR && \
curl http://localhost:8080/api/v1/tools | jq '.tools[] | select(.created_by == "agent")' && \
kill %1
```

