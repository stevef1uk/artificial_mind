# Deploying Tool Creation Agent to Kubernetes

## Issue: Endpoints Not Found

If you see `404` errors for `/api/v1/intelligent/execute`, the Kubernetes deployment is running an **older version** of the code that doesn't include the tool creation agent.

## Solution: Rebuild and Redeploy

### Step 1: Build Updated Docker Image

```bash
cd ~/dev/artificial_mind

# Build the HDN server image with tool creation agent code
docker build -f Dockerfile.hdn.secure -t stevef1uk/hdn-server:secure .

# Tag with a version if desired
docker tag stevef1uk/hdn-server:secure stevef1uk/hdn-server:secure-tool-creator

# Push to registry
docker push stevef1uk/hdn-server:secure
# Or if using version tag:
docker push stevef1uk/hdn-server:secure-tool-creator
```

### Step 2: Update Kubernetes Deployment

**Option A: Restart to pull latest image (if using `:secure` tag)**

```bash
kubectl rollout restart deployment/hdn-server-rpi58 -n agi
kubectl rollout status deployment/hdn-server-rpi58 -n agi
```

**Option B: Update image tag in deployment**

```bash
# Edit the deployment to use new image tag
kubectl set image deployment/hdn-server-rpi58 \
  hdn-server=stevef1uk/hdn-server:secure-tool-creator \
  -n agi

# Or edit manually
kubectl edit deployment hdn-server-rpi58 -n agi
# Change: image: stevef1uk/hdn-server:secure
# To:     image: stevef1uk/hdn-server:secure-tool-creator
```

### Step 3: Verify Deployment

```bash
# Check pod is running
kubectl get pods -n agi -l app=hdn-server-rpi58

# Check logs for startup
kubectl logs -n agi -l app=hdn-server-rpi58 --tail=50 | grep -E "(Starting|API|routes)"

# Verify endpoints are available
kubectl port-forward -n agi deployment/hdn-server-rpi58 8080:8080 &
sleep 3
curl http://localhost:8080/api/v1/intelligent/execute -X POST -H "Content-Type: application/json" -d '{"test":true}' 2>&1 | head -5
kill %1
```

### Step 4: Run Test

```bash
./k3s/test_tool_creation_agent_k8s.sh
```

## Quick Check: Is Code Updated?

To verify the deployment has the tool creation agent code:

```bash
# Check if the code exists in the running pod
kubectl exec -n agi deployment/hdn-server-rpi58 -- \
  sh -c 'strings /app/hdn-server 2>/dev/null | grep -q "considerToolCreationFromExecution" && echo "✅ Tool creation code found" || echo "❌ Tool creation code NOT found"'
```

If it says "NOT found", you need to rebuild and redeploy.

## Alternative: Check Image Build Date

```bash
# Check when the image was built
docker inspect stevef1uk/hdn-server:secure | jq '.[0].Created'

# Compare with when you made the code changes
# If image is older than your code changes, rebuild is needed
```

## Troubleshooting

### Image Not Updating

If the pod keeps using the old image:

1. **Check image pull policy:**
   ```bash
   kubectl get deployment hdn-server-rpi58 -n agi -o jsonpath='{.spec.template.spec.containers[0].imagePullPolicy}'
   ```
   Should be `Always` to pull latest.

2. **Force delete pod:**
   ```bash
   kubectl delete pod -n agi -l app=hdn-server-rpi58
   ```

3. **Check image registry:**
   ```bash
   # Verify image exists in registry
   docker pull stevef1uk/hdn-server:secure
   ```

### Endpoints Still 404 After Update

1. **Check server logs for route registration:**
   ```bash
   kubectl logs -n agi -l app=hdn-server-rpi58 --tail=200 | grep -E "(intelligent/execute|HandleFunc)"
   ```

2. **Verify code was compiled:**
   ```bash
   kubectl exec -n agi deployment/hdn-server-rpi58 -- \
     sh -c 'strings /app/hdn-server 2>/dev/null | grep "handleIntelligentExecute" | head -1'
   ```

3. **Check if server started correctly:**
   ```bash
   kubectl logs -n agi -l app=hdn-server-rpi58 --tail=100 | grep -E "(error|Error|panic|failed)"
   ```

