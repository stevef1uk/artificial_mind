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

## Scraper & Smart Scrape Integration

### 1. Test the Scraper Service directly
This confirms the Playwright workers in Kubernetes are handling complex multi-step forms (like the EcoTree calculator).

```bash
# 1. Port forward the scraper service
kubectl port-forward -n agi svc/playwright-scraper 8085:8085 &

# 2. Run the transport suite (Plane, Train, Car)
export PLAYWRIGHT_SCRAPER_URL=http://localhost:8085
./test/test_all_transports.sh
```

### 2. Test Smart Scrape (AI Planning)
This confirms the HDN server can use the LLM to generate a plan and execute it through the scraper service.

```bash
# Example: Find Apple stock price via Kubernetes HDN
./test/test_smart_scrape_k8s.sh agi "https://finance.yahoo.com/quote/AAPL" "Find the current stock price"
```

### 3. Smart Scrape Validator (Go)
Use the Go-based mirror to verify the Agent's planning and synthesis quality against the K3s cluster:

```bash
# Option A: Using NodePort (Direct RPI IP)
export HDN_URL=http://<rpi-ip>:30257
go run test/test_nationwide_k8s.go

# Option B: Using Port Forward (if NodePort is blocked)
kubectl port-forward -n agi svc/hdn-server-rpi58 30257:8080 &
go run test/test_nationwide_k8s.go
```

## What to Look For

### In Logs:
- `🧠 [MCP-SMART-SCRAPE] Starting smart scrape...` (HDN Pod)
- `🔧 Worker X: Processing job...` (Scraper Pod)
- `✅ Completed in Xs!` (Scraper Pod)

### Behavior:
- **Plane Test**: Should take ~25-30s. If it hangs for 2m, check `SelectOption` timeouts.
- **Smart Scrape**: Should produce a clean **Markdown Table** in the final response.
- **NodePort**: If testing from the RPI directly without port-forwarding, use the NodeIP and NodePort (usually http://<rpi-ip>:30081).

