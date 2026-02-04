# Async Playwright Scraper Service Architecture

## Overview

The web scraping functionality has been refactored into a standalone microservice with async job queue capabilities to solve timeout issues and reduce Docker image sizes.

## Architecture

```
┌─────────────────────┐         HTTP POST          ┌──────────────────────────┐
│   HDN Server        │  ──────────────────────▶   │  Playwright Scraper      │
│   (Lightweight)     │   /scrape/start            │  Service                 │
│   ~200MB            │  ◀──────────────────────   │  (Chromium + Playwright) │
│                     │   { job_id }               │  ~900MB                  │
│                     │                             │                          │
│                     │         HTTP GET            │  Features:               │
│                     │  ──────────────────────▶   │  • Async job queue       │
│                     │   /scrape/job?job_id=...   │  • 3 worker threads      │
│                     │  ◀──────────────────────   │  • Auto cleanup          │
│                     │   { status, result }       │  • 90s timeout           │
└─────────────────────┘                             └──────────────────────────┘
```

## Components

### 1. Playwright Scraper Service (`services/playwright_scraper/`)

**Location:** `/home/stevef/dev/artificial_mind/services/playwright_scraper/`

**Files:**
- `main.go` - Async scraper service with job queue
- `go.mod` - Dependencies (playwright-go, uuid)
- `Dockerfile` - Debian-based image with Chromium
- `README.md` - API documentation

**Features:**
- ✅ Async job queue with immediate job ID return
- ✅ Polling-based result retrieval
- ✅ 3 concurrent worker threads
- ✅ Auto cleanup of old jobs (30min retention)
- ✅ 90-second timeout for scraping operations
- ✅ Same TypeScript config format as MCP tool

**API Endpoints:**
```
POST /scrape/start
  Request: { "url": "...", "typescript_config": "..." }
  Response: { "job_id": "uuid", "status": "pending", "created_at": "..." }

GET /scrape/job?job_id=<uuid>
  Response: { "id": "...", "status": "completed|pending|running|failed", "result": {...} }

GET /health
  Response: { "status": "healthy", "service": "playwright-scraper", "time": "..." }
```

### 2. HDN Server Updates

**Changes:**
- ✅ Removed `playwright-go` dependency
- ✅ Removed direct Playwright execution code
- ✅ Added HTTP client to call scraper service
- ✅ Implemented polling loop with 90s timeout
- ✅ Returns results in same MCP format

**Configuration:**
- Environment variable: `PLAYWRIGHT_SCRAPER_URL` (optional)
- Default: `http://playwright-scraper.agi.svc.cluster.local:8080`

### 3. Dockerfile Optimizations

**HDN Server (`Dockerfile.hdn.secure`):**
- ✅ Base image: `alpine:latest` (back to lightweight)
- ✅ No Chromium dependencies
- ✅ Reduced size: ~200MB (down from ~900MB)
- ✅ Faster builds and deployments

**Scraper Service (`services/playwright_scraper/Dockerfile`):**
- ✅ Base image: `debian:bookworm-slim`
- ✅ Includes Chromium and browser dependencies
- ✅ Size: ~900MB (acceptable for dedicated service)

### 4. Kubernetes Deployment

**File:** `k8s/playwright-scraper-deployment.yaml`

**Features:**
- ✅ Separate deployment for scraper
- ✅ ClusterIP service for internal access
- ✅ Health checks (liveness + readiness)
- ✅ Resource limits (2Gi mem, 1 CPU)
- ✅ Optional node selector for placement

**Deployment:**
```bash
kubectl apply -f k8s/playwright-scraper-deployment.yaml
```

## Benefits

### 1. **No More Timeouts**
- Async job queue prevents HTTP timeout issues
- 90-second scrape timeout (configurable)
- Client polls for results at own pace

### 2. **Smaller HDN Image**
- ~700MB reduction in image size
- Faster builds (no Chromium compilation)
- Quicker deployments for code changes

### 3. **Better Resource Management**
- Scale scraper independently based on load
- Run scraper on different node with more resources
- HDN server remains lightweight

### 4. **Improved Reliability**
- Scraper crashes don't affect HDN server
- Can restart/update scraper without HDN downtime
- Better separation of concerns

### 5. **Concurrent Scraping**
- 3 worker threads handle multiple requests
- Job queue prevents overload
- Automatic cleanup of old jobs

## Usage Example

### From n8n Workflow:

**Step 1: Start Scrape Job (MCP Tool Call)**
```json
{
  "tool": "scrape_url",
  "arguments": {
    "url": "https://ecotree.green/en/calculate-car-co2",
    "typescript_config": "import { test } from '@playwright/test';\ntest('test', async ({ page }) => {\n  await page.goto('https://ecotree.green/en/calculate-car-co2');\n  await page.waitForTimeout(200);\n  await page.locator('div.geosuggest:nth-of-type(1) #geosuggest__input').fill('Portsmouth');\n  await page.waitForTimeout(200);\n  await page.getByText('Portsmouth').first().click();\n  await page.waitForTimeout(200);\n  await page.locator('div.geosuggest:nth-of-type(2) #geosuggest__input').fill('London');\n  await page.waitForTimeout(200);\n  await page.getByText('London').first().click();\n  await page.waitForTimeout(200);\n  await page.getByRole('link', { name: ' Calculate my emissions ' }).click();\n});"
  }
}
```

**Response (after polling):**
```json
{
  "content": [{
    "type": "text",
    "text": "Scrape Results:\n{\"co2_kg\": \"12.5\", \"distance_km\": \"104\", ...}"
  }],
  "result": {
    "co2_kg": "12.5",
    "distance_km": "104",
    "page_url": "https://ecotree.green/en/calculate-car-co2",
    "page_title": "Calculate your car CO2 emissions"
  }
}
```

## Testing

### 1. Test Scraper Service Locally
```bash
# Build
docker build -t stevef1uk/playwright-scraper:latest services/playwright_scraper/

# Run
docker run -p 8080:8080 stevef1uk/playwright-scraper:latest

# Test
curl -X POST http://localhost:8080/scrape/start \
  -H 'Content-Type: application/json' \
  -d '{"url": "https://example.com", "typescript_config": "..."}'
```

### 2. Deploy to Kubernetes
```bash
# Build and push (on RPI or build machine)
docker build -t stevef1uk/playwright-scraper:latest services/playwright_scraper/
docker push stevef1uk/playwright-scraper:latest

# Deploy
kubectl apply -f k8s/playwright-scraper-deployment.yaml

# Check status
kubectl get pods -n agi | grep playwright-scraper
kubectl logs -n agi deployment/playwright-scraper
```

### 3. Test End-to-End from n8n
See `TESTING_GUIDE.md` for complete n8n integration tests.

## Migration Notes

### Before (Monolithic):
- HDN server: ~900MB with Chromium
- Scraping blocked HTTP server (health check issues)
- Timeout failures common (60s limit)
- Slow deployments

### After (Microservices):
- HDN server: ~200MB (no Chromium)
- Scraping doesn't block HTTP server
- No timeout issues (90s + polling)
- Fast HDN deployments
- Independent scraper scaling

## Future Enhancements

### Possible Improvements:
1. **Redis-backed job store** - For persistence across restarts
2. **WebSocket notifications** - Push results instead of polling
3. **Priority queue** - Prioritize certain scrape requests
4. **Rate limiting** - Per-domain rate limits
5. **Screenshot capture** - Return screenshots with results
6. **Multi-node scraping** - Horizontal scaling with load balancer

## Configuration

### Environment Variables:

**HDN Server:**
- `PLAYWRIGHT_SCRAPER_URL` - Scraper service URL (default: `http://playwright-scraper.agi.svc.cluster.local:8080`)

**Scraper Service:**
- None currently (all hardcoded for simplicity)

### Performance Tuning:

**Scraper Service (`main.go`):**
```go
// Worker count (line ~13)
service := NewScraperService(3)  // Change to 5 for more concurrency

// Job queue size (line ~136)
jobQueue:    make(chan string, 100),  // Change to 200 for larger queue

// Job retention (line ~177)
s.store.CleanupOld(30 * time.Minute)  // Change to 1 hour

// Page timeout (line ~319)
page.SetDefaultTimeout(20000)  // Change to 30000 for slower sites
```

**HDN Server (`mcp_knowledge_server.go`):**
```go
// Poll timeout (line ~601)
pollTimeout := 90 * time.Second  // Increase for very slow sites

// Poll interval (line ~602)
pollInterval := 2 * time.Second  // Decrease for faster feedback
```

## Troubleshooting

### Scraper Service Not Starting:
```bash
# Check logs
kubectl logs -n agi deployment/playwright-scraper

# Common issues:
# 1. Chromium missing - check Dockerfile apt install
# 2. Port conflict - ensure port 8080 is free
# 3. Resource limits - increase memory limit in deployment.yaml
```

### Jobs Timing Out:
```bash
# Increase timeouts in scraper service
# Edit main.go, line 319:
page.SetDefaultTimeout(30000)  // 30 seconds

# Rebuild and redeploy
```

### HDN Can't Reach Scraper:
```bash
# Check service exists
kubectl get svc -n agi playwright-scraper

# Test DNS resolution from HDN pod
kubectl exec -n agi deployment/hdn-server-rpi58 -- \
  ping playwright-scraper.agi.svc.cluster.local

# Check firewall/network policies
```

## Related Files

- `/home/stevef/dev/artificial_mind/services/playwright_scraper/` - Scraper service code
- `/home/stevef/dev/artificial_mind/k8s/playwright-scraper-deployment.yaml` - K8s config
- `/home/stevef/dev/artificial_mind/hdn/mcp_knowledge_server.go` - HDN integration
- `/home/stevef/dev/artificial_mind/Dockerfile.hdn.secure` - Optimized HDN Dockerfile

