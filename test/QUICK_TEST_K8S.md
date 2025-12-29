# Quick Intelligence Test for Kubernetes

## From Your Mac (Remote Access)

### Option 1: Use the remote test script

```bash
# Make sure kubectl is configured to access your k3s cluster
kubectl get nodes

# Run the test
./test/test_intelligence_remote_k8s.sh
```

This script will:
- Automatically set up port-forwarding
- Check logs for intelligence messages
- Check Redis for learning data
- Test code generation

### Option 2: Manual testing

```bash
# 1. Set up port-forwarding
kubectl port-forward -n agi <hdn-pod-name> 8081:8080 &

# 2. Test code generation
curl -X POST http://localhost:8081/api/v1/intelligent/execute \
  -H "Content-Type: application/json" \
  -d '{
    "task_name": "test",
    "description": "Print hello world",
    "language": "python"
  }'

# 3. Check logs for intelligence
kubectl logs -n agi <hdn-pod-name> --tail=100 | grep -i intelligence

# 4. Check Redis
REDIS_POD=$(kubectl get pods -n agi | grep redis | awk '{print $1}')
kubectl exec -n agi $REDIS_POD -- redis-cli KEYS "failure_pattern:*"
```

## From Raspberry Pi (Direct Access)

```bash
cd ~/dev/artificial_mind/k3s
./test_intelligence.sh
```

## What to Look For

### In Logs:
- `🧠 [INTELLIGENCE] Added X prevention hints from learned experience`
- `🧠 [INTELLIGENCE] Retrieved learned prevention hint`
- `🧠 [INTELLIGENCE] Using learned successful strategy`

### In Redis:
After running code generation tasks, you should see:
- `failure_pattern:*` keys
- `codegen_strategy:*` keys  
- `prevention_hint:*` keys

### Behavior:
- First code generation: May have retries
- Similar code generation: Should have fewer retries (shows learning)

