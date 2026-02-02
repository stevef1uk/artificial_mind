# Testing Docker Images from Command Line

This guide explains how to test the Docker images locally from the command line.

## Prerequisites

1. **Docker must be running**
2. **Customer private key** must be available at `secure/customer_private.pem`
3. **Vendor token** (optional but recommended): Set `SECURE_VENDOR_TOKEN` environment variable
4. **Service dependencies** (optional for basic testing):
   - NATS server (for news-ingestor)
   - Neo4j (for wiki-bootstrapper)
   - Redis (for wiki-bootstrapper and wiki-summarizer)
   - Weaviate (for wiki-bootstrapper and wiki-summarizer)
   - Ollama/LLM endpoint (for news-ingestor and wiki-summarizer)

## Quick Start

Use the provided test script:

```bash
# Test a specific image
./scripts/test-docker-images.sh news-ingestor
./scripts/test-docker-images.sh wiki-bootstrapper
./scripts/test-docker-images.sh wiki-summarizer

# Test all images
./scripts/test-docker-images.sh all
```

## Manual Testing

### 1. News Ingestor (data-processor)

Tests the BBC news scraping and ingestion:

```bash
docker run --rm \
  -v "$(pwd)/secure/customer_private.pem:/keys/customer_private.pem:ro" \
  -e SECURE_CUSTOMER_PRIVATE_PATH="/keys/customer_private.pem" \
  -e SECURE_VENDOR_TOKEN="${SECURE_VENDOR_TOKEN:-}" \
  -e UNPACK_WORK_DIR="/tmp/unpack" \
  -e NATS_URL="nats://localhost:4222" \
  -e OLLAMA_URL="http://localhost:11434/api/chat" \
  -e OLLAMA_MODEL="Qwen2.5-VL-7B-Instruct:latest" \
  stevef1uk/data-processor:secure \
  -url "https://www.bbc.com/news" \
  -max 5 \
  -debug
```

**Command-line arguments:**
- `-url`: BBC News URL to scrape (default: https://www.bbc.com/news)
- `-max`: Maximum stories to process (default: 15)
- `-dry`: Print decisions without publishing
- `-debug`: Verbose discovery debug output
- `-llm`: Use LLM to classify headlines in batches
- `-batch-size`: LLM batch size (default: 10)
- `-llm-model`: LLM model name
- `-ollama-url`: LLM endpoint / Ollama chat API URL

### 2. Wiki Bootstrapper (knowledge-builder)

Tests Wikipedia knowledge graph building:

```bash
docker run --rm \
  -v "$(pwd)/secure/customer_private.pem:/keys/customer_private.pem:ro" \
  -e SECURE_CUSTOMER_PRIVATE_PATH="/keys/customer_private.pem" \
  -e SECURE_VENDOR_TOKEN="${SECURE_VENDOR_TOKEN:-}" \
  -e UNPACK_WORK_DIR="/tmp/unpack" \
  -e NEO4J_URI="bolt://localhost:7687" \
  -e NEO4J_USER="neo4j" \
  -e NEO4J_PASS="test1234" \
  -e REDIS_ADDR="localhost:6379" \
  -e WEAVIATE_URL="http://localhost:8080" \
  -e NATS_URL="nats://localhost:4222" \
  stevef1uk/knowledge-builder:secure \
  -weaviate \
  -max-nodes 10 \
  -max-depth 2 \
  -rpm 60 \
  -burst 10 \
  -jitter-ms 25 \
  -seeds "Science,Technology"
```

**Command-line arguments:**
- `-seeds`: Comma-separated Wikipedia titles to seed (default: "Science,Technology,History,Mathematics,Biology")
- `-max-depth`: Maximum crawl depth (default: 1)
- `-max-nodes`: Maximum number of concepts to ingest (default: 200)
- `-rpm`: Requests per minute rate limit (default: 30)
- `-burst`: Burst allowance for rate limiter (default: 5)
- `-jitter-ms`: Jitter in milliseconds added to each request (default: 250)
- `-min-confidence`: Minimum confidence to create relation (default: 0.6)
- `-domain`: Domain tag to assign to seeded concepts (default: "General")
- `-weaviate`: Also index summaries into Weaviate episodic memory
- `-job-id`: Job ID for pause/resume (default: timestamp)
- `-resume`: Resume from previous state for this job-id
- `-pause`: Set the job paused flag and exit

### 3. Wiki Summarizer

Tests Wikipedia article summarization:

```bash
docker run --rm \
  -v "$(pwd)/secure/customer_private.pem:/keys/customer_private.pem:ro" \
  -e SECURE_CUSTOMER_PRIVATE_PATH="/keys/customer_private.pem" \
  -e SECURE_VENDOR_TOKEN="${SECURE_VENDOR_TOKEN:-}" \
  -e UNPACK_WORK_DIR="/tmp/unpack" \
  -e WEAVIATE_URL="http://localhost:8080" \
  -e REDIS_ADDR="localhost:6379" \
  -e LLM_PROVIDER="ollama" \
  -e LLM_ENDPOINT="http://localhost:11434/api/generate" \
  -e LLM_MODEL="Qwen2.5-VL-7B-Instruct:latest" \
  -e BATCH_SIZE="5" \
  -e MAX_WORDS="250" \
  -e DOMAIN="General" \
  stevef1uk/wiki-summarizer:secure \
  -weaviate="http://localhost:8080" \
  -redis="localhost:6379" \
  -llm-provider="ollama" \
  -llm-endpoint="http://localhost:11434/api/generate" \
  -llm-model="Qwen2.5-VL-7B-Instruct:latest" \
  -batch-size=5 \
  -max-words=250 \
  -domain="General" \
  -job-id="test_$(date +%s)"
```

**Command-line arguments:**
- `-weaviate`: Vector database URL (Weaviate) (default: http://localhost:8080)
- `-redis`: Redis address (default: localhost:6379)
- `-llm-provider`: LLM provider (ollama, openai, etc.) (default: ollama)
- `-llm-endpoint`: LLM endpoint (default: http://localhost:11434/api/generate)
- `-llm-model`: LLM model name (default: gemma3n:latest)
- `-batch-size`: Number of articles to process per batch (default: 5)
- `-max-words`: Maximum words in summary (default: 250)
- `-domain`: Domain to process (default: General)
- `-job-id`: Job ID for pause/resume (default: timestamp)
- `-resume`: Resume from previous state for this job-id
- `-pause`: Set the job paused flag and exit

## Environment Variables

### Required for all images:
- `SECURE_CUSTOMER_PRIVATE_PATH`: Path to customer private key inside container (default: `/keys/customer_private.pem`)
- `UNPACK_WORK_DIR`: Working directory for unpacking (default: `/tmp/unpack`)

### Optional for all images:
- `SECURE_VENDOR_TOKEN`: Vendor token for license validation (required if image was built with `-license` flag)

### Service-specific:
- **NATS_URL**: NATS server URL (for news-ingestor)
- **NEO4J_URI**: Neo4j connection URI (for wiki-bootstrapper)
- **NEO4J_USER**: Neo4j username (for wiki-bootstrapper)
- **NEO4J_PASS**: Neo4j password (for wiki-bootstrapper)
- **REDIS_ADDR**: Redis address (for wiki-bootstrapper and wiki-summarizer)
- **WEAVIATE_URL**: Weaviate server URL (for wiki-bootstrapper and wiki-summarizer)
- **OLLAMA_URL** / **LLM_ENDPOINT**: LLM endpoint URL (for news-ingestor and wiki-summarizer)
- **OLLAMA_MODEL** / **LLM_MODEL**: LLM model name (for news-ingestor and wiki-summarizer)

## Testing Without Services

You can test the images even if services aren't running, but they may fail when trying to connect. To test just the image unpacking and binary execution:

```bash
# Test image unpacking (will fail on service connection, but shows image works)
docker run --rm \
  -v "$(pwd)/secure/customer_private.pem:/keys/customer_private.pem:ro" \
  -e SECURE_CUSTOMER_PRIVATE_PATH="/keys/customer_private.pem" \
  -e SECURE_VENDOR_TOKEN="${SECURE_VENDOR_TOKEN:-}" \
  -e UNPACK_WORK_DIR="/tmp/unpack" \
  stevef1uk/data-processor:secure \
  -help 2>&1 || echo "Image unpacked successfully (connection errors expected)"
```

## Troubleshooting

### "Customer private key not found"
- Ensure `secure/customer_private.pem` exists
- Check the volume mount path is correct

### "Vendor token required"
- Set `SECURE_VENDOR_TOKEN` environment variable
- Or ensure the image was built without `-license` flag

### "Connection refused" errors
- Start required services (NATS, Neo4j, Redis, Weaviate, Ollama)
- Check service URLs in environment variables
- For local testing, use `localhost` instead of service names

### "Image not found"
- Pull the image first: `docker pull stevef1uk/data-processor:secure`
- Or build it locally: `make docker-build-push`

## Example: Full Test with Services

If you have docker-compose services running:

```bash
# Start services
docker-compose up -d

# Test news ingestor
NATS_URL=nats://localhost:4222 \
  OLLAMA_URL=http://localhost:11434/api/chat \
  ./scripts/test-docker-images.sh news-ingestor

# Test wiki bootstrapper
NEO4J_URI=bolt://localhost:7687 \
  NEO4J_USER=neo4j \
  NEO4J_PASS=test1234 \
  REDIS_ADDR=localhost:6379 \
  WEAVIATE_URL=http://localhost:8080 \
  NATS_URL=nats://localhost:4222 \
  ./scripts/test-docker-images.sh wiki-bootstrapper
```






