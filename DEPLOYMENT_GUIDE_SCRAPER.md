# Quick Deployment Guide: Async Scraper Service

## 🎯 Goal
Deploy the new async Playwright scraper service and lightweight HDN server.

## 📋 Prerequisites
- Docker installed on build machine (RPI or dev machine)
- Access to Kubernetes cluster
- `kubectl` configured for `agi` namespace

---

## 🚀 Deployment Steps

### Step 1: Build & Push Scraper Service

```bash
cd ~/dev/artificial_mind

# Build scraper image
docker build -t stevef1uk/playwright-scraper:latest \
  -f services/playwright_scraper/Dockerfile \
  services/playwright_scraper/

# Push to registry
docker push stevef1uk/playwright-scraper:latest
```

**Expected output:**
```
Successfully built abc123def456
Successfully tagged stevef1uk/playwright-scraper:latest
The push refers to repository [docker.io/stevef1uk/playwright-scraper]
...
latest: digest: sha256:... size: 1234
```

---

### Step 2: Deploy Scraper to Kubernetes

```bash
# Apply deployment
kubectl apply -f k8s/playwright-scraper-deployment.yaml

# Verify deployment
kubectl get pods -n agi | grep playwright-scraper

# Check logs
kubectl logs -n agi deployment/playwright-scraper --tail=50
```

**Expected logs:**
```
🚀 Starting Playwright Scraper Service...
⏰ Timezone: UTC
✅ Chromium found: Chromium 120.0.6099.0
📊 Configuration:
   - Worker Count: 3 (hardcoded in main.go)
   - Job Queue Size: 100
   - Job Retention: 30 minutes
   - Page Timeout: 20 seconds
   - Port: 8080
🎬 Starting scraper service...
✅ Started 3 scraper workers
🚀 Playwright Scraper Service starting on :8080
```

---

### Step 3: Test Scraper Service (Optional but Recommended)

```bash
# Port-forward to test locally
kubectl port-forward -n agi svc/playwright-scraper 8080:8080 &

# Run test script
./test/test_scraper_service.sh

# Stop port-forward when done
pkill -f "port-forward.*playwright-scraper"
```

**Expected output:**
```
🧪 Testing Playwright Scraper Service
======================================
Service URL: http://localhost:8080

1️⃣  Testing health endpoint...
✅ Health check passed
{
  "status": "healthy",
  "service": "playwright-scraper",
  "time": "2026-02-03T..."
}

2️⃣  Starting scrape job (EcoTree Car: Portsmouth -> London)...
✅ Job started: 550e8400-e29b-41d4-a716-446655440000
...

3️⃣  Polling for results (timeout: 90s)...
   [2 s] Status: running
   [4 s] Status: running
   ...
   [18 s] Status: completed

✅ Job completed in 18s!

📊 Results:
{
  "co2_kg": "12.5",
  "distance_km": "104",
  ...
}

🚗 CO2 Emissions: 12.5 kg
📏 Distance: 104 km
```

---

### Step 4: Rebuild Lightweight HDN Server

```bash
# Build new lightweight HDN image (no Chromium!)
docker build --no-cache \
  -f Dockerfile.hdn.secure \
  --build-arg CUSTOMER_PUBLIC_KEY=secure/customer_public.pem \
  --build-arg VENDOR_PUBLIC_KEY=secure/vendor_public.pem \
  -t stevef1uk/hdn-server:secure .

# Push to registry
docker push stevef1uk/hdn-server:secure
```

**Expected size difference:**
```
# OLD (with Chromium):
stevef1uk/hdn-server:secure   900MB

# NEW (without Chromium):
stevef1uk/hdn-server:secure   200MB   ← 700MB smaller! 🎉
```

---

### Step 5: Restart HDN Deployment

```bash
# Restart to pull new lightweight image
kubectl rollout restart deployment/hdn-server-rpi58 -n agi

# Watch rollout status
kubectl rollout status deployment/hdn-server-rpi58 -n agi

# Verify HDN can reach scraper
kubectl logs -n agi deployment/hdn-server-rpi58 --tail=50 | grep MCP-SCRAPE
```

**Expected HDN logs (when scraping):**
```
📝 [MCP-SCRAPE] Received TypeScript config (646 bytes)
🚀 [MCP-SCRAPE] Starting scrape job at http://playwright-scraper.agi.svc.cluster.local:8080/scrape/start
⏳ [MCP-SCRAPE] Job 550e8400-... started, polling for results...
⏳ [MCP-SCRAPE] Job 550e8400-... status: running (elapsed: 2s)
⏳ [MCP-SCRAPE] Job 550e8400-... status: running (elapsed: 4s)
✅ [MCP-SCRAPE] Job 550e8400-... completed in 18s
```

---

### Step 6: Test End-to-End from n8n

1. **Open your n8n workflow** with MCP integration

2. **Update the `scrape_url` tool call** to use the new config:

```json
{
  "url": "https://ecotree.green/en/calculate-car-co2",
  "typescript_config": "import { test } from '@playwright/test';\ntest('test', async ({ page }) => {\n  await page.goto('https://ecotree.green/en/calculate-car-co2');\n  await page.waitForTimeout(200);\n  await page.locator('div.geosuggest:nth-of-type(1) #geosuggest__input').fill('Portsmouth');\n  await page.waitForTimeout(200);\n  await page.getByText('Portsmouth').first().click();\n  await page.waitForTimeout(200);\n  await page.locator('div.geosuggest:nth-of-type(2) #geosuggest__input').fill('London');\n  await page.waitForTimeout(200);\n  await page.getByText('London').first().click();\n  await page.waitForTimeout(200);\n  await page.getByRole('link', { name: ' Calculate my emissions ' }).click();\n});"
}
```

3. **Run the workflow**

4. **Expected result:** ✅ No timeout! Results in ~20-30 seconds

---

## ✅ Verification Checklist

- [ ] Scraper service deployed and healthy
- [ ] Scraper service has 1 pod running
- [ ] HDN server restarted with new lightweight image
- [ ] HDN can reach scraper (check logs)
- [ ] Test script passes
- [ ] n8n workflow completes without timeout

---

## 🐛 Troubleshooting

### Scraper pod not starting
```bash
# Check pod status
kubectl describe pod -n agi -l app=playwright-scraper

# Common issues:
# - Image pull error → check image name/tag
# - CrashLoopBackOff → check logs for chromium errors
# - Pending → check resource limits
```

### HDN can't reach scraper
```bash
# Verify service exists
kubectl get svc -n agi playwright-scraper

# Test DNS from HDN pod
kubectl exec -n agi deployment/hdn-server-rpi58 -- \
  wget -qO- http://playwright-scraper.agi.svc.cluster.local:8080/health
```

### Scraper jobs timing out
```bash
# Check scraper logs for errors
kubectl logs -n agi deployment/playwright-scraper --tail=200

# Increase timeout in main.go if needed (default 20s)
# Then rebuild and redeploy
```

### n8n still timing out
```bash
# Check HDN polling timeout (default 90s)
# Edit hdn/mcp_knowledge_server.go line ~601:
pollTimeout := 120 * time.Second  # Increase to 2 minutes

# Rebuild HDN server
```

---

## 📊 Architecture Summary

**Before:**
```
┌─────────────────────────┐
│   HDN Server (900MB)    │
│  - Go binary            │
│  - Chromium (700MB)     │
│  - Playwright           │
│  - All tools            │
└─────────────────────────┘
```

**After:**
```
┌──────────────────┐         HTTP        ┌────────────────────────┐
│ HDN Server       │  ────────────────▶  │ Scraper Service        │
│ (200MB)          │   async jobs        │ (900MB)                │
│ - Go binary      │  ◀────────────────  │ - Chromium             │
│ - No Chromium!   │   poll results      │ - Playwright           │
│ - HTTP client    │                     │ - 3 workers            │
└──────────────────┘                     │ - Job queue            │
                                          └────────────────────────┘
```

**Benefits:**
- ✅ 700MB smaller HDN image
- ✅ No timeouts (90s scraping + polling)
- ✅ Independent scaling
- ✅ Better resource management

---

## 🔧 Configuration

### Environment Variables

**HDN Server:**
```bash
# Optional: Override scraper URL
export PLAYWRIGHT_SCRAPER_URL=http://custom-scraper:8080
```

**Scraper Service:**
- No environment variables needed (all hardcoded)

### Resource Limits (in deployment.yaml)

**Scraper:**
```yaml
resources:
  requests:
    memory: "512Mi"
    cpu: "250m"
  limits:
    memory: "2Gi"      # Increase for heavy workloads
    cpu: "1000m"       # Increase for more concurrency
```

### Node Placement (optional)

To run scraper on specific node:
```yaml
# In k8s/playwright-scraper-deployment.yaml
spec:
  template:
    spec:
      nodeSelector:
        kubernetes.io/hostname: node-with-more-memory
```

---

## 📚 Related Documentation

- **Architecture:** `ASYNC_SCRAPER_ARCHITECTURE.md`
- **API Reference:** `services/playwright_scraper/README.md`
- **Test Script:** `test/test_scraper_service.sh`

---

## 🎉 Success Criteria

You'll know it's working when:
1. ✅ Scraper pod shows "Ready 1/1"
2. ✅ HDN logs show "Job ... completed in Xs"
3. ✅ n8n workflows complete without timeout
4. ✅ Docker images show HDN at ~200MB
5. ✅ Test script passes end-to-end

**Happy deploying! 🚀**

