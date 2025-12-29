# Testing Workflows After Connection Fix

This guide explains how to test that workflows are working correctly after fixing the Kubernetes connection issues.

## Quick Test (Recommended First Step)

Run the quick connectivity test:
```bash
./k3s/quick-test-workflow.sh
```

This verifies:
- HDN server is healthy
- HDN_URL is correctly configured
- Workflows endpoint is accessible
- FSM can connect to HDN

**Expected output**: All tests should pass ✅

## Comprehensive Test

For a full workflow test suite:
```bash
./k3s/test-workflows.sh
```

This comprehensive test includes:
1. ✅ HDN Server Health Check
2. ✅ HDN_URL Configuration Verification
3. ✅ Workflows Endpoint Accessibility
4. ✅ Workflow Creation (creates a real test workflow)
5. ✅ Workflow Status Checking
6. ✅ FSM to HDN Connection
7. ✅ Redis Workflow Storage
8. ✅ Workflow Listing from Multiple Services
9. ✅ Tool Invocation Testing

**Expected output**: All tests should pass ✅

## Manual Testing Options

### Option 1: Using kubectl exec (from host)

#### Test HDN Health
```bash
kubectl exec -n agi deployment/hdn-server-rpi58 -- \
  wget -qO- --timeout=5 http://hdn-server-rpi58.agi.svc.cluster.local:8080/health
```

#### Create a Simple Workflow
```bash
kubectl exec -n agi deployment/monitor-ui -- sh -c "
  echo '{
    \"task_name\": \"test_connection\",
    \"description\": \"Create a Python function that returns hello world\",
    \"context\": {\"test\": true},
    \"language\": \"python\"
  }' | wget -qO- --timeout=30 --post-data='@-' \
    --header='Content-Type: application/json' \
    http://hdn-server-rpi58.agi.svc.cluster.local:8080/api/v1/intelligent/execute
"
```

#### List Active Workflows
```bash
kubectl exec -n agi deployment/monitor-ui -- \
  wget -qO- --timeout=10 \
  http://hdn-server-rpi58.agi.svc.cluster.local:8080/api/v1/hierarchical/workflows | jq '.'
```

#### Check Workflow Status
```bash
WORKFLOW_ID="your_workflow_id_here"
kubectl exec -n agi deployment/monitor-ui -- \
  wget -qO- --timeout=10 \
  "http://hdn-server-rpi58.agi.svc.cluster.local:8080/api/v1/hierarchical/workflow/$WORKFLOW_ID/status" | jq '.'
```

### Option 2: Using Monitor UI (Web Interface)

1. **Port-forward the Monitor UI**:
   ```bash
   kubectl port-forward -n agi svc/monitor-ui 8082:8082
   ```

2. **Open in browser**: http://localhost:8082

3. **Test workflows**:
   - Go to the "Execute" tab
   - Enter a simple task like: "Create a Python function that adds two numbers"
   - Click "Execute"
   - Check the "Workflows" tab to see the workflow status
   - Monitor progress in real-time

### Option 3: Using curl with port-forward

1. **Port-forward HDN server**:
   ```bash
   kubectl port-forward -n agi svc/hdn-server-rpi58 8080:8080
   ```

2. **Test from your local machine**:
   ```bash
   # Health check
   curl http://localhost:8080/health
   
   # Create workflow
   curl -X POST http://localhost:8080/api/v1/intelligent/execute \
     -H "Content-Type: application/json" \
     -d '{
       "task_name": "test_connection",
       "description": "Create a Python function that returns hello world",
       "context": {"test": true},
       "language": "python"
     }'
   
   # List workflows
   curl http://localhost:8080/api/v1/hierarchical/workflows | jq '.'
   ```

## Diagnostic Tools

### Check Service Connectivity
```bash
./k3s/diagnose-workflow-connections.sh
```

This comprehensive diagnostic checks:
- DNS resolution for all services
- Service endpoint connectivity
- Environment variable configuration
- Connection errors in logs
- Workflow status in Redis

### Check HDN Workflows Specifically
```bash
./k3s/check-hdn-workflows.sh
```

This checks:
- HDN health
- Workflows endpoint
- Redis active workflows
- Workflow records
- Recent errors and activity

## What to Look For

### ✅ Success Indicators

- All connectivity tests pass
- Workflows can be created successfully
- Workflow status can be retrieved
- Services can communicate with each other
- No connection refused or timeout errors in logs

### ❌ Failure Indicators

- Connection refused errors
- Timeout errors
- DNS resolution failures
- HDN_URL still set to localhost
- Services cannot reach each other

## Troubleshooting

If tests fail:

1. **Check HDN_URL is correct**:
   ```bash
   kubectl exec -n agi deployment/hdn-server-rpi58 -- env | grep HDN_URL
   ```
   Should show: `HDN_URL=http://hdn-server-rpi58.agi.svc.cluster.local:8080`

2. **Check service logs**:
   ```bash
   kubectl -n agi logs deployment/hdn-server-rpi58 --tail=50 | grep -i error
   kubectl -n agi logs deployment/fsm-server-rpi58 --tail=50 | grep -i error
   ```

3. **Verify services are running**:
   ```bash
   kubectl -n agi get pods -l 'app in (hdn-server-rpi58,fsm-server-rpi58,monitor-ui)'
   ```

4. **Check service endpoints**:
   ```bash
   kubectl -n agi get endpoints
   ```

5. **Run full diagnostics**:
   ```bash
   ./k3s/diagnose-workflow-connections.sh
   ```

## Next Steps

After confirming workflows are working:

1. Monitor workflow execution in the Monitor UI
2. Check workflow logs for any issues
3. Test more complex workflows
4. Verify tool invocations work correctly
5. Test hierarchical planning workflows

## Related Documentation

- `k3s/WORKFLOW_CONNECTION_FIX.md` - Details about the connection fix
- `k3s/diagnose-workflow-connections.sh` - Comprehensive connectivity diagnostic
- `k3s/check-hdn-workflows.sh` - HDN-specific workflow diagnostic

