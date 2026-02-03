# Local Testing Guide: Async Scraper Service

## 🎯 Goal
Test the new async scraper service and HDN integration locally before deploying to Kubernetes.

---

## 📋 Prerequisites

- Docker installed
- `jq` installed (for parsing JSON)
- Ports 8080 (scraper) and 3001 (HDN) available

---

## 🧪 Test Plan

We'll test in 3 stages:
1. **Stage 1:** Test scraper service standalone
2. **Stage 2:** Test HDN server calling scraper
3. **Stage 3:** Test complete MCP flow

---

## Stage 1: Test Scraper Service Standalone

### Step 1.1: Build Scraper Service

```bash
cd /home/stevef/dev/artificial_mind

# Build the scraper image
docker build -t playwright-scraper:test \
  -f services/playwright_scraper/Dockerfile \
  services/playwright_scraper/
```

**Expected output:**
```
Successfully built abc123def456
Successfully tagged playwright-scraper:test
```

### Step 1.2: Run Scraper Service

```bash
# Run scraper on port 8080
docker run -d \
  --name scraper-test \
  -p 8080:8080 \
  playwright-scraper:test

# Check it's running
docker ps | grep scraper-test

# View logs
docker logs scraper-test
```

**Expected logs:**
```
🚀 Starting Playwright Scraper Service...
⏰ Timezone: UTC
✅ Chromium found: Chromium 120.0.6099.0
📊 Configuration:
   - Worker Count: 3
   - Job Queue Size: 100
   - Job Retention: 30 minutes
   - Page Timeout: 20 seconds
   - Port: 8080
🎬 Starting scraper service...
🚀 Worker 0 started
🚀 Worker 1 started
🚀 Worker 2 started
✅ Started 3 scraper workers
🚀 Playwright Scraper Service starting on :8080
```

### Step 1.3: Test Scraper API

```bash
# Run the test script
./test/test_scraper_service.sh
```

**Expected result:** Test completes successfully with CO2/distance data

### Step 1.4: Cleanup Stage 1

```bash
# Stop and remove scraper container
docker stop scraper-test
docker rm scraper-test
```

---

## Stage 2: Test HDN Server Calling Scraper

### Step 2.1: Start Scraper Service (Background)

```bash
# Start scraper in background
docker run -d \
  --name scraper-test \
  -p 8080:8080 \
  playwright-scraper:test
```

### Step 2.2: Build HDN Server (Local Test Version)

We'll use a simplified local build without secure packaging:

```bash
cd /home/stevef/dev/artificial_mind

# Build HDN binary locally
cd hdn
go build -o ../bin/hdn-server-test .
cd ..
```

### Step 2.3: Run HDN Server

```bash
# Set environment variable to point to local scraper
export PLAYWRIGHT_SCRAPER_URL=http://localhost:8080
export NEO4J_URI=neo4j://localhost:7687
export NEO4J_USERNAME=neo4j
export NEO4J_PASSWORD=your_password
export WEAVIATE_URL=http://localhost:8081
export REDIS_ADDR=localhost:6379

# Run HDN server
./bin/hdn-server-test
```

**Note:** If you don't have Neo4j/Weaviate/Redis running locally, the server will log warnings but should still start.

### Step 2.4: Test MCP scrape_url Tool

Open another terminal:

```bash
# Test the scrape_url tool via MCP
curl -X POST http://localhost:3001/mcp \
  -H 'Content-Type: application/json' \
  -d '{
    "jsonrpc": "2.0",
    "id": 1,
    "method": "tools/call",
    "params": {
      "name": "scrape_url",
      "arguments": {
        "url": "https://ecotree.green/en/calculate-car-co2",
        "typescript_config": "import { test } from '\''@playwright/test'\'';\ntest('\''test'\'', async ({ page }) => {\n  await page.goto('\''https://ecotree.green/en/calculate-car-co2'\'');\n  await page.waitForTimeout(200);\n  await page.locator('\''div.geosuggest:nth-of-type(1) #geosuggest__input'\'').fill('\''Portsmouth'\'');\n  await page.waitForTimeout(200);\n  await page.getByText('\''Portsmouth'\'').first().click();\n  await page.waitForTimeout(200);\n  await page.locator('\''div.geosuggest:nth-of-type(2) #geosuggest__input'\'').fill('\''London'\'');\n  await page.waitForTimeout(200);\n  await page.getByText('\''London'\'').first().click();\n  await page.waitForTimeout(200);\n  await page.getByRole('\''link'\'', { name: '\'' Calculate my emissions '\'' }).click();\n});"
      }
    }
  }' | jq '.'
```

**Expected response:**
```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "content": [
      {
        "type": "text",
        "text": "Scrape Results:\n{...}"
      }
    ],
    "result": {
      "co2_kg": "12.5",
      "distance_km": "104",
      "page_url": "https://ecotree.green/en/calculate-car-co2",
      "page_title": "Calculate your car CO2 emissions"
    }
  }
}
```

### Step 2.5: Monitor Logs

In separate terminals:

```bash
# Watch scraper logs
docker logs -f scraper-test

# Watch HDN logs
# (HDN logs to stdout in the terminal where you ran it)
```

**Expected HDN logs:**
```
📝 [MCP-SCRAPE] Received TypeScript config (646 bytes)
🚀 [MCP-SCRAPE] Starting scrape job at http://localhost:8080/scrape/start
⏳ [MCP-SCRAPE] Job 550e8400-... started, polling for results...
⏳ [MCP-SCRAPE] Job 550e8400-... status: running (elapsed: 2s)
✅ [MCP-SCRAPE] Job 550e8400-... completed in 18s
```

**Expected scraper logs:**
```
📥 Created job 550e8400-e29b-41d4-a716-446655440000 for https://ecotree.green/en/calculate-car-co2
🔧 Worker 0: Processing job 550e8400-...
🔧 Installing Playwright driver (one-time setup)...
✅ Playwright driver installed
📍 Navigating to https://ecotree.green/en/calculate-car-co2
  [1/6] goto
  [2/6] wait
  [3/6] locatorFill
  ...
📊 Extracting results...
✅ Worker 0: Job 550e8400-... completed
```

### Step 2.6: Cleanup Stage 2

```bash
# Stop HDN server (Ctrl+C in its terminal)

# Stop and remove scraper
docker stop scraper-test
docker rm scraper-test
```

---

## Stage 3: Docker Compose Test (Optional - Full Stack)

For a more realistic test, we can use Docker Compose:

### Step 3.1: Create Docker Compose File

```bash
cat > /home/stevef/dev/artificial_mind/docker-compose.test.yml << 'EOF'
version: '3.8'

services:
  scraper:
    image: playwright-scraper:test
    container_name: scraper-test
    ports:
      - "8080:8080"
    healthcheck:
      test: ["CMD", "/app/entrypoint.sh", "-health-check"]
      interval: 10s
      timeout: 5s
      retries: 3

  # Uncomment if you want to test full HDN integration
  # hdn:
  #   image: hdn-server:test
  #   container_name: hdn-test
  #   ports:
  #     - "3001:3001"
  #   environment:
  #     - PLAYWRIGHT_SCRAPER_URL=http://scraper:8080
  #   depends_on:
  #     - scraper
EOF
```

### Step 3.2: Run with Docker Compose

```bash
cd /home/stevef/dev/artificial_mind

# Start services
docker-compose -f docker-compose.test.yml up -d

# View logs
docker-compose -f docker-compose.test.yml logs -f

# Test scraper
./test/test_scraper_service.sh

# Cleanup
docker-compose -f docker-compose.test.yml down
```

---

## 🐛 Troubleshooting

### Scraper container won't start

```bash
# Check build logs
docker build -t playwright-scraper:test \
  -f services/playwright_scraper/Dockerfile \
  services/playwright_scraper/ 2>&1 | tee build.log

# Check container logs
docker logs scraper-test

# Run interactively to debug
docker run -it --rm \
  -p 8080:8080 \
  playwright-scraper:test \
  /bin/bash
```

### Port 8080 already in use

```bash
# Find what's using it
sudo lsof -i :8080

# Use different port
docker run -d \
  --name scraper-test \
  -p 8081:8080 \
  playwright-scraper:test

# Update PLAYWRIGHT_SCRAPER_URL
export PLAYWRIGHT_SCRAPER_URL=http://localhost:8081
```

### Chromium errors in scraper

```bash
# Check if Chromium installed correctly
docker run -it --rm playwright-scraper:test \
  chromium --version

# Check entrypoint script
docker run -it --rm playwright-scraper:test \
  cat /app/entrypoint.sh
```

### HDN can't connect to scraper

```bash
# Verify scraper is reachable
curl http://localhost:8080/health

# Check firewall
sudo iptables -L -n | grep 8080

# Test from HDN container (if using Docker)
docker exec hdn-test \
  wget -qO- http://scraper:8080/health
```

### Job timeouts

```bash
# Check scraper logs for errors
docker logs scraper-test | grep -E "(Failed|Error|⚠️)"

# Increase timeout in HDN (if needed)
# Edit hdn/mcp_knowledge_server.go:
#   pollTimeout := 120 * time.Second

# Rebuild HDN
cd hdn && go build -o ../bin/hdn-server-test .
```

---

## ✅ Success Checklist

Stage 1 (Scraper Standalone):
- [ ] Scraper container starts
- [ ] Health check returns "healthy"
- [ ] Test script completes
- [ ] CO2 data extracted correctly

Stage 2 (HDN Integration):
- [ ] HDN server starts
- [ ] HDN connects to scraper
- [ ] MCP tool call succeeds
- [ ] Results returned in <90s

Stage 3 (Docker Compose):
- [ ] Both services start
- [ ] Health checks pass
- [ ] Services can communicate
- [ ] End-to-end test works

---

## 📊 Performance Benchmarks

Expected timings (Portsmouth → London):
- Job creation: <100ms
- Scraping: 15-25 seconds
- Total (HDN → result): 18-30 seconds

If significantly slower:
- Check network connectivity
- Check system resources (CPU/memory)
- Review scraper logs for retries

---

## 🎯 Next Steps

Once local testing passes:
1. ✅ Commit changes
2. ✅ Deploy to Kubernetes (see `DEPLOYMENT_GUIDE_SCRAPER.md`)
3. ✅ Test in production environment
4. ✅ Update n8n workflows

---

## 🔧 Helper Commands

```bash
# Quick cleanup all test containers
docker stop scraper-test hdn-test 2>/dev/null
docker rm scraper-test hdn-test 2>/dev/null

# View all logs
docker logs scraper-test 2>&1 | less

# Test health endpoint
watch -n 1 'curl -s http://localhost:8080/health | jq'

# Monitor resource usage
docker stats scraper-test

# Shell into running container
docker exec -it scraper-test /bin/bash
```

---

**Happy testing! 🧪**

