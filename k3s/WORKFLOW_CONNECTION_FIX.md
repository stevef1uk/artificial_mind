# Workflow Connection Issues - Fixed

## Problem
On Kubernetes, many components were unable to connect to each other, causing workflow execution errors.

## Root Cause
The HDN server was configured with `HDN_URL=http://localhost:8080` instead of using the Kubernetes service DNS name. This caused issues when:
1. Other services tried to reference HDN
2. Workflows tried to make tool calls via HDN
3. Generated code tried to call HDN APIs
4. The intelligent executor tried to make internal API calls

Additionally, the HDN server code had a hardcoded `http://localhost:8080` in the intelligent executor initialization, ignoring the environment variable.

## Fixes Applied

### 1. Kubernetes Deployment Configuration
**File**: `k3s/hdn-server-rpi58.yaml`

Changed:
```yaml
- name: HDN_URL
  value: "http://localhost:8080"
```

To:
```yaml
- name: HDN_URL
  value: "http://hdn-server-rpi58.agi.svc.cluster.local:8080"
```

### 2. Server Code
**File**: `hdn/server.go`

Changed the hardcoded localhost URL to use the `HDN_URL` environment variable:

```go
// Get HDN base URL from environment variable, fallback to localhost for local development
hdnBaseURL := getenvTrim("HDN_URL")
if hdnBaseURL == "" {
    hdnBaseURL = "http://localhost:8080" // Default for local development
}
log.Printf("✅ [HDN] Using HDN base URL: %s", hdnBaseURL)

// Create intelligent executor for planner
intelligentExecutor := NewIntelligentExecutor(
    // ... other params ...
    hdnBaseURL, // HDN base URL for tool calling (from HDN_URL env var)
    redisAddr,
)
```

## Verification

Run the diagnostic script to verify all connections:
```bash
./k3s/diagnose-workflow-connections.sh
```

Expected results:
- ✅ All DNS resolution should work
- ✅ All service endpoints should be reachable
- ✅ HDN_URL should be set to the Kubernetes service DNS name

## Deployment

After making these changes:

1. **Rebuild the HDN server image** (if code changes were made):
   ```bash
   # Build and push the updated image
   docker build -f Dockerfile.hdn.secure -t stevef1uk/hdn-server:secure .
   docker push stevef1uk/hdn-server:secure
   ```

2. **Apply the updated Kubernetes deployment**:
   ```bash
   kubectl apply -f k3s/hdn-server-rpi58.yaml
   ```

3. **Restart the HDN server pod**:
   ```bash
   kubectl rollout restart deployment/hdn-server-rpi58 -n agi
   ```

4. **Verify the fix**:
   ```bash
   # Check that HDN_URL is now set correctly
   kubectl exec -n agi deployment/hdn-server-rpi58 -- env | grep HDN_URL
   # Should show: HDN_URL=http://hdn-server-rpi58.agi.svc.cluster.local:8080
   
   # Run diagnostics
   ./k3s/diagnose-workflow-connections.sh
   ```

## Related Services

Other services are correctly configured:
- ✅ FSM Server: `HDN_URL=http://hdn-server-rpi58.agi.svc.cluster.local:8080`
- ✅ Monitor UI: `HDN_URL=http://hdn-server-rpi58.agi.svc.cluster.local:8080`

## Impact

This fix should resolve:
- Workflow execution errors due to connection failures
- Tool invocation failures in generated code
- Internal API call failures within HDN
- Service-to-service communication issues

## Testing

After applying the fix, test that workflows are working:

### Quick Test
Run the comprehensive test script:
```bash
./k3s/test-workflows.sh
```

This script tests:
1. HDN server health
2. HDN_URL environment variable configuration
3. Workflows endpoint accessibility
4. Workflow creation
5. Workflow status checking
6. FSM to HDN connection
7. Redis workflow storage
8. Workflow listing from multiple services
9. Tool invocation

### Manual Testing

#### Test 1: Check HDN Health
```bash
kubectl exec -n agi deployment/hdn-server-rpi58 -- wget -qO- --timeout=5 http://hdn-server-rpi58.agi.svc.cluster.local:8080/health
```

#### Test 2: Create a Simple Workflow
```bash
kubectl exec -n agi deployment/monitor-ui -- sh -c "
  echo '{
    \"task_name\": \"test_connection\",
    \"description\": \"Create a Python function that returns hello world\",
    \"context\": {\"test\": true},
    \"language\": \"python\"
  }' | wget -qO- --timeout=30 --post-data='@-' --header='Content-Type: application/json' \
    http://hdn-server-rpi58.agi.svc.cluster.local:8080/api/v1/intelligent/execute
"
```

#### Test 3: List Active Workflows
```bash
kubectl exec -n agi deployment/monitor-ui -- wget -qO- --timeout=10 \
  http://hdn-server-rpi58.agi.svc.cluster.local:8080/api/v1/hierarchical/workflows | jq '.'
```

#### Test 4: Check Workflow Status (replace WORKFLOW_ID)
```bash
WORKFLOW_ID="your_workflow_id_here"
kubectl exec -n agi deployment/monitor-ui -- wget -qO- --timeout=10 \
  "http://hdn-server-rpi58.agi.svc.cluster.local:8080/api/v1/hierarchical/workflow/$WORKFLOW_ID/status" | jq '.'
```

### Using Monitor UI

You can also test workflows through the Monitor UI:
1. Port-forward the Monitor UI:
   ```bash
   kubectl port-forward -n agi svc/monitor-ui 8082:8082
   ```
2. Open http://localhost:8082 in your browser
3. Use the "Execute" tab to create and test workflows
4. Check the "Workflows" tab to see active workflows

## Notes

- The fix maintains backward compatibility: if `HDN_URL` is not set, it defaults to `http://localhost:8080` for local development
- All service DNS names follow the pattern: `<service-name>.agi.svc.cluster.local:<port>`
- The diagnostic script (`k3s/diagnose-workflow-connections.sh`) can be used to verify connectivity at any time
- The test script (`k3s/test-workflows.sh`) provides comprehensive workflow testing

