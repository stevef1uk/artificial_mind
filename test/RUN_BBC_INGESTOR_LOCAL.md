# Running BBC News Ingestor Locally

This guide shows how to run the BBC news ingestor on your local Linux machine to test the news ingestion and RAG search functionality.

## Prerequisites

You need these services running locally:

1. **NATS** - Message bus for publishing news events
2. **Redis** (optional) - For duplicate detection
3. **FSM Server** (optional) - To receive and store events in Weaviate
4. **Weaviate** (optional) - To store news articles for RAG search

## Quick Start

### 1. Start Required Services

```bash
# Start NATS (if using Docker)
docker run -d --name nats -p 4222:4222 nats:latest

# Start Redis (if using Docker)
docker run -d --name redis -p 6379:6379 redis:latest

# Or use docker-compose if you have it
docker-compose up -d nats redis
```

### 2. Set Environment Variables

```bash
export NATS_URL=nats://127.0.0.1:4222
export REDIS_URL=redis://127.0.0.1:6379

# Optional: For LLM classification
export LLM_MODEL=llama3.1
export OLLAMA_URL=http://localhost:11434/api/chat
```

### 3. Run the Ingestor

**Basic run (heuristic classification, no publishing):**
```bash
./test/run_bbc_ingestor_local.sh --dry --debug
```

**Basic run (heuristic classification, publish to NATS):**
```bash
./test/run_bbc_ingestor_local.sh --debug
```

**With LLM classification:**
```bash
./test/run_bbc_ingestor_local.sh --llm --debug
```

**Dry run with more stories:**
```bash
./test/run_bbc_ingestor_local.sh --dry --max 30 --debug
```

## Command Options

- `--dry` - Dry run mode (don't publish to NATS, just show what would be published)
- `--llm` - Use LLM for classification (requires Ollama running)
- `--max N` - Maximum number of stories to process (default: 15)
- `--debug` - Verbose debug output
- `--batch-size N` - LLM batch size (default: 10)
- `--help` - Show help message

## What It Does

1. **Scrapes BBC News** - Fetches headlines from https://www.bbc.com/news
2. **Checks Duplicates** - Uses Redis to avoid processing the same story twice
3. **Classifies Stories** - Either heuristic rules or LLM-based classification
4. **Publishes Events** - Sends to NATS subjects:
   - `agi.events.news.relations` - For stories that relate to existing knowledge
   - `agi.events.news.alerts` - For important/breaking news

## Verifying It Works

### 1. Check Ingestor Output

The ingestor will show:
- Number of stories discovered
- Number of stories processed (after duplicate filtering)
- Classification decisions (ALERT, REL, or SKIP)

### 2. Check NATS Events (if not dry run)

If you have the `nats` CLI tool:
```bash
nats sub "agi.events.news.>" --count=5
```

### 3. Check FSM Server Logs (if running)

If FSM server is running and subscribed to NATS:
```bash
# Check logs for news events
tail -f /path/to/fsm-server.log | grep -i news
```

Look for:
- `📨 Received NATS event on agi.events.news.relations`
- `📰 Storing news events for curiosity goal generation`
- `✅ Stored news event in Weaviate`

### 4. Check Weaviate (if FSM server stored events)

```bash
# If Weaviate is running locally
./test/check_weaviate_bbc.sh http://localhost:8080

# Or query directly
curl -X POST "http://localhost:8080/v1/graphql" \
  -H "Content-Type: application/json" \
  -d '{"query": "{ Get { WikipediaArticle(limit: 10, where: {path: [\"source\"], operator: Equal, valueString: \"news:fsm\"}) { title source timestamp } } }"}'
```

## Troubleshooting

### "NATS connect error"
- Make sure NATS is running: `docker ps | grep nats`
- Check NATS_URL is correct: `echo $NATS_URL`

### "Failed to initialize Redis"
- This is a warning, not an error
- The ingestor will continue without duplicate detection
- Make sure Redis is running if you want duplicate detection

### "No stories discovered"
- Try increasing `--max` (e.g., `--max 50`)
- Check your internet connection
- BBC website structure may have changed

### "LLM error" or timeouts
- Make sure Ollama is running if using `--llm`
- Check OLLAMA_URL is correct
- Try without `--llm` to use heuristic classification

### Events not appearing in Weaviate
- Make sure FSM server is running
- Check FSM server is subscribed to NATS subjects
- Check FSM server logs for errors
- Verify Weaviate is accessible from FSM server

## Example Output

```
==========================================
BBC News Ingestor - Local Test
==========================================

[INFO] Checking prerequisites...
[OK] NATS is accessible at nats://127.0.0.1:4222
[OK] Redis is accessible at redis://127.0.0.1:6379

[INFO] Configuration:
  NATS_URL: nats://127.0.0.1:4222
  REDIS_URL: redis://127.0.0.1:6379
  LLM_MODEL: llama3.1 (default)

[INFO] Using heuristic classification
[INFO] Debug output enabled

[INFO] Running command:
  ./bin/bbc-news-ingestor -max 15 -debug

==========================================

discovered 15 BBC stories from https://www.bbc.com/news
Processing 12 stories (filtered from 15)
[FALLBACK] REL: Story about technology
[FALLBACK] ALERT: Breaking news about...
...

==========================================
[OK] Ingestor completed successfully!
==========================================
```

## Next Steps

After running the ingestor:

1. **If using dry run**: Remove `--dry` to actually publish events
2. **Start FSM server**: To receive events and store in Weaviate
3. **Test RAG search**: Query the system with a question that should match news items
4. **Check Weaviate**: Verify items were stored with vectors









