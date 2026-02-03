# Playwright Scraper Service

Standalone async web scraping service using Playwright with job queue architecture.

## License

**⚠️ IMPORTANT: This service has a different license than the main project.**

This service is licensed for **non-commercial use only**. Commercial use requires a separate commercial license.

- ✅ **FREE** for personal, educational, and research use
- ❌ **PAID LICENSE REQUIRED** for commercial use, SaaS, or production in for-profit organizations

See the [LICENSE](LICENSE) file for full terms and conditions.

For commercial licensing inquiries, please contact the copyright holder.

## Features

- ✅ **Async Job Queue** - Submit scraping jobs and poll for results
- ✅ **Multiple Workers** - Concurrent scraping (3 workers by default)
- ✅ **Auto Cleanup** - Old jobs cleaned up after 30 minutes
- ✅ **TypeScript Config** - Same Playwright config format as MCP tool
- ✅ **REST API** - Simple HTTP endpoints

## API Endpoints

### `POST /scrape/start`
Start a new scraping job.

**Request:**
```json
{
  "url": "https://example.com",
  "typescript_config": "import { test } from '@playwright/test';\ntest('test', async ({ page }) => {\n  await page.goto('https://example.com');\n});"
}
```

**Response:**
```json
{
  "job_id": "550e8400-e29b-41d4-a716-446655440000",
  "status": "pending",
  "created_at": "2026-02-03T12:00:00Z"
}
```

### `GET /scrape/job?job_id=<id>`
Get job status and results.

**Response (Pending):**
```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "status": "pending",
  "created_at": "2026-02-03T12:00:00Z"
}
```

**Response (Running):**
```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "status": "running",
  "created_at": "2026-02-03T12:00:00Z",
  "started_at": "2026-02-03T12:00:05Z"
}
```

**Response (Completed):**
```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "status": "completed",
  "created_at": "2026-02-03T12:00:00Z",
  "started_at": "2026-02-03T12:00:05Z",
  "completed_at": "2026-02-03T12:00:25Z",
  "result": {
    "page_url": "https://example.com",
    "page_title": "Example Domain",
    "co2_kg": "12.5",
    "distance_km": "450",
    "raw_text": "..."
  }
}
```

**Response (Failed):**
```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "status": "failed",
  "created_at": "2026-02-03T12:00:00Z",
  "started_at": "2026-02-03T12:00:05Z",
  "completed_at": "2026-02-03T12:00:15Z",
  "error": "failed to navigate: timeout exceeded"
}
```

### `GET /health`
Health check endpoint.

**Response:**
```json
{
  "status": "healthy",
  "service": "playwright-scraper",
  "time": "2026-02-03T12:00:00Z"
}
```

## Building

### Secure Build (Production)

The scraper service uses the same secure packaging as the HDN server:

```bash
# Build with secure packaging
cd services/playwright_scraper
docker build \
  --build-arg CUSTOMER_PUBLIC_KEY=../../secure/customer_public.pem \
  --build-arg VENDOR_PUBLIC_KEY=../../secure/vendor_public.pem \
  -t stevef1uk/playwright-scraper:latest .

# Push to registry
docker push stevef1uk/playwright-scraper:latest
```

### Local Testing

```bash
# Build test image
make build-scraper-test

# Start with required secrets
docker run -p 8080:8080 \
  -v $(pwd)/secure/customer_private.pem:/keys/customer_private.pem:ro \
  -e SECURE_CUSTOMER_PRIVATE_PATH=/keys/customer_private.pem \
  -e SECURE_VENDOR_TOKEN=$(cat secure/vendor_token.txt) \
  -e UNPACK_WORK_DIR=/tmp/unpack \
  playwright-scraper:test

# Or use make targets
make start-scraper
```

## Testing

```bash
# Start a scrape job
curl -X POST http://localhost:8080/scrape/start \
  -H 'Content-Type: application/json' \
  -d '{
    "url": "https://ecotree.green/en/calculate-flight-co2",
    "typescript_config": "import { test } from '\''@playwright/test'\'';\ntest('\''test'\'', async ({ page }) => {\n  await page.goto('\''https://ecotree.green/en/calculate-flight-co2'\'');\n  await page.getByRole('\''link'\'', { name: '\''Plane'\'' }).click();\n});"
  }'

# Get job status (replace JOB_ID with actual ID from above)
curl http://localhost:8080/scrape/job?job_id=JOB_ID

# Health check
curl http://localhost:8080/health
```

## Configuration

- **Worker Count**: Set in `main.go` (default: 3)
- **Job Queue Size**: 100 jobs max
- **Job Retention**: 30 minutes after completion
- **Page Timeout**: 20 seconds
- **Port**: 8080

## Kubernetes Deployment

See `k8s/playwright-scraper-deployment.yaml` for Kubernetes configuration.

