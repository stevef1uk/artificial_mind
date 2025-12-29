# Building Explanation Learning Feature on Raspberry Pi

## Quick Start

The branch `add-explanation-grounded-learning-feedback` has been pushed to GitHub. To build and deploy on your Raspberry Pi:

### Option 1: Use the Rebuild Script (Recommended)

```bash
# On your Raspberry Pi
cd ~/dev/artificial_mind

# Run the rebuild script
./test/rebuild_fsm_explanation_learning.sh
```

This script will:
1. Switch to the branch
2. Pull latest changes
3. Build the FSM binary
4. Build the Docker image
5. Restart the FSM pod

### Option 2: Manual Steps

If you prefer to do it manually:

```bash
# 1. Switch to the branch
cd ~/dev/artificial_mind
git fetch origin
git checkout add-explanation-grounded-learning-feedback

# 2. Pull latest changes
git pull origin add-explanation-grounded-learning-feedback

# 3. Build the FSM binary
cd fsm
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -tags neo4j -ldflags="-s -w" -o ../bin/fsm-server .
cd ..

# 4. Build Docker image (choose one based on your setup)

# If you have secure keys:
docker build -f Dockerfile.fsm.secure \
    --build-arg CUSTOMER_PUBLIC_KEY=secure/customer_public.pem \
    --build-arg VENDOR_PUBLIC_KEY=secure/vendor_public.pem \
    -t fsm-server:explanation-learning .

# Or if using release Dockerfile:
docker build -f Dockerfile.fsm.release \
    -t fsm-server:explanation-learning .

# Or if building from fsm directory:
cd fsm
docker build -t fsm-server:explanation-learning .
cd ..

# 5. Restart the FSM pod
kubectl delete pod -n agi -l app=fsm-server-rpi58
# Wait for pod to restart
kubectl wait --for=condition=ready pod -n agi -l app=fsm-server-rpi58 --timeout=120s
```

## Verify It's Working

After the pod restarts, check the logs:

```bash
# Get the FSM pod name
FSM_POD=$(kubectl get pods -n agi -l app=fsm-server-rpi58 -o jsonpath='{.items[0].metadata.name}')

# Watch for explanation learning messages
kubectl logs -f -n agi $FSM_POD | grep EXPLANATION-LEARNING
```

You should see messages like:
- `🧠 [EXPLANATION-LEARNING] Evaluating goal completion`
- `✅ [EXPLANATION-LEARNING] Completed evaluation`
- `📉 [EXPLANATION-LEARNING] Reducing confidence calibration`
- `📈 [EXPLANATION-LEARNING] Increasing confidence calibration`

## Testing the Feature

1. **Trigger a goal completion** (create and complete a goal)
2. **Watch the logs** for explanation learning evaluation
3. **Check Redis** for stored feedback:
   ```bash
   kubectl exec -it -n agi redis-0 -- redis-cli
   > KEYS explanation_learning:*
   > GET explanation_learning:stats:General
   ```

## Troubleshooting

### Branch Not Found
If you get "branch not found" error:
```bash
git fetch origin
git checkout -b add-explanation-grounded-learning-feedback origin/add-explanation-grounded-learning-feedback
```

### Build Fails
- Make sure you have Go 1.21+ installed
- Check that you're in the project root directory
- Verify dependencies: `cd fsm && go mod tidy`

### Pod Won't Start
- Check pod logs: `kubectl logs -n agi <pod-name>`
- Check pod status: `kubectl describe pod -n agi <pod-name>`
- Verify image was built: `docker images | grep fsm-server`

## What Changed

The FSM server now includes:
- **New module**: `fsm/explanation_learning.go` - Learning feedback system
- **Updated**: `fsm/engine.go` - Subscribes to goal completion events
- **Documentation**: `docs/EXPLANATION_GROUNDED_LEARNING.md`

No new servers or ports - it's all integrated into the existing FSM server.

## Next Steps

After deployment:
1. Monitor logs for learning feedback messages
2. Check Redis for accumulated learning statistics
3. Observe how confidence calibration improves over time
4. See exploration heuristics adjust based on outcomes

For more details, see `docs/EXPLANATION_GROUNDED_LEARNING.md`

