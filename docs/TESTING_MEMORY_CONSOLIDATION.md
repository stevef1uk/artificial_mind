# Testing Memory Consolidation Locally

This guide explains how to test the Memory Consolidation & Compression system locally before deploying to Kubernetes.

## Prerequisites

1. **Docker & Docker Compose** - For running infrastructure services
2. **Go 1.21+** - For building and running the HDN server
3. **Redis CLI** (optional) - For inspecting Redis data
4. **curl** (optional) - For checking service health

## Quick Start

### Option 1: Automated Test Script

The easiest way to test is using the automated test script:

```bash
# Make sure you're in the project root
cd /path/to/artificial_mind

# Run the test script
./test/test_memory_consolidation.sh
```

This script will:
1. ✅ Check/start required services (Redis, Weaviate, Neo4j)
2. 📝 Seed test data (episodes, traces)
3. 🚀 Start HDN server with consolidation enabled
4. ⏳ Wait for consolidation cycle
5. 🔍 Verify results (schemas, skills, archived traces)
6. 📊 Show summary and tips

### Option 2: Manual Testing

If you prefer to test manually:

#### Step 1: Start Infrastructure

```bash
# Start all required services
docker-compose up -d redis weaviate neo4j

# Verify services are running
docker-compose ps

# Check health
curl http://localhost:8080/v1/meta  # Weaviate
redis-cli -u redis://localhost:6379 ping  # Redis
curl http://localhost:7474  # Neo4j
```

#### Step 2: Configure Environment

```bash
export REDIS_URL="redis://localhost:6379"
export WEAVIATE_URL="http://localhost:8080"
export NEO4J_URI="bolt://localhost:7687"
export NEO4J_USER="neo4j"
export NEO4J_PASS="test1234"
export LLM_PROVIDER="mock"  # Use mock LLM for testing
```

#### Step 3: Build HDN Server

```bash
cd hdn
go build -tags neo4j -o ../bin/hdn-server .
cd ..
```

#### Step 4: Start HDN Server

```bash
./bin/hdn-server -mode=server -port=8081
```

You should see logs like:
```
🧠 [API] Episodic memory via Weaviate: http://localhost:8080
🧠 [API] Domain knowledge enabled: bolt://localhost:7687
🧠 [CONSOLIDATION] Starting memory consolidation scheduler (interval: 1h0m0s)
```

#### Step 5: Seed Test Data

Create some test episodes to consolidate. You can use the HDN API:

```bash
# Create episodes via intelligent execution
curl -X POST http://localhost:8081/api/v1/intelligent/execute \
  -H "Content-Type: application/json" \
  -d '{
    "task_name": "test_task",
    "description": "Test task for consolidation",
    "context": {"session_id": "test_session_1"},
    "language": "python"
  }'

# Repeat a few times with similar tasks to create redundant episodes
for i in {1..10}; do
  curl -X POST http://localhost:8081/api/v1/intelligent/execute \
    -H "Content-Type: application/json" \
    -d "{
      \"task_name\": \"data_processing\",
      \"description\": \"Process data pipeline\",
      \"context\": {\"session_id\": \"test_session_$i\"},
      \"language\": \"python\"
    }"
  sleep 1
done
```

#### Step 6: Wait for Consolidation

Consolidation runs:
- **Immediately**: 30 seconds after server startup
- **Periodically**: Every hour (default)

To see consolidation in action sooner, you can:
1. Wait 35 seconds after starting the server
2. Or restart the server to trigger another initial run

#### Step 7: Verify Results

**Check Redis for schemas:**
```bash
redis-cli -u redis://localhost:6379 KEYS "consolidation:schema:*"
redis-cli -u redis://localhost:6379 GET "consolidation:schema:<key>"
```

**Check Redis for skills:**
```bash
redis-cli -u redis://localhost:6379 KEYS "consolidation:skill:*"
```

**Check Redis for archived traces:**
```bash
redis-cli -u redis://localhost:6379 KEYS "archive:reasoning_trace:*"
```

**Check Neo4j for promoted concepts:**
```bash
# Using Neo4j Browser (http://localhost:7474)
# Login: neo4j / test1234
# Run query:
MATCH (c:Concept) 
WHERE c.domain = "Skills" OR c.domain = "General"
RETURN c.name, c.definition, c.domain
LIMIT 10
```

**Check server logs:**
```bash
# Look for consolidation logs
tail -f /path/to/hdn-server.log | grep CONSOLIDATION
```

You should see logs like:
```
🔄 [CONSOLIDATION] Starting consolidation cycle
✅ [CONSOLIDATION] Compressed 2 episode groups
✅ [CONSOLIDATION] Promoted 1 structures to semantic memory
✅ [CONSOLIDATION] Archived 0 traces
✅ [CONSOLIDATION] Extracted 1 skill abstractions
✅ [CONSOLIDATION] Consolidation cycle completed in 2.3s
```

## Testing Different Scenarios

### Test Episode Compression

To test compression, you need at least 5 similar episodes:

```bash
# Create similar episodes
for i in {1..6}; do
  curl -X POST http://localhost:8081/api/v1/intelligent/execute \
    -H "Content-Type: application/json" \
    -d "{
      \"task_name\": \"similar_task\",
      \"description\": \"Execute data processing workflow\",
      \"context\": {\"session_id\": \"compression_test_$i\"},
      \"language\": \"python\"
    }"
done

# Wait for consolidation (35s after server start, or restart server)
# Check for schemas
redis-cli KEYS "consolidation:schema:*"
```

### Test Skill Extraction

To test skill extraction, you need at least 3 episodes with the same workflow_id:

```bash
# Create episodes with workflow_id in metadata
# (This would typically come from actual workflow execution)
# For testing, you can manually index episodes via Weaviate API
```

### Test Trace Archiving

To test archiving, create old traces:

```bash
# Create a trace that will expire soon
redis-cli SET "reasoning_trace:old_session" '{"decisions":[],"steps":[]}' EX 3600

# Wait for consolidation cycle
# Check for archived traces
redis-cli KEYS "archive:reasoning_trace:*"
```

## Troubleshooting

### Consolidation Not Running

**Check:**
1. Are vectorDB and domainKnowledge initialized?
   - Look for: `🧠 [API] Episodic memory via Weaviate: ...`
   - Look for: `🧠 [API] Domain knowledge enabled: ...`
2. Check server logs for errors
3. Verify services are accessible:
   ```bash
   curl http://localhost:8080/v1/meta  # Weaviate
   redis-cli ping  # Redis
   ```

### No Schemas Created

**Possible reasons:**
- Not enough similar episodes (need ≥5)
- Episodes not similar enough (similarity threshold: 0.75)
- Episodes too old (only processes last 30 days)

**Solution:**
- Create more similar episodes
- Lower similarity threshold in config
- Use more recent episodes

### No Skills Extracted

**Possible reasons:**
- Not enough workflow repetitions (need ≥3)
- Episodes don't have `workflow_id` in metadata

**Solution:**
- Create more episodes with same workflow_id
- Ensure metadata includes workflow_id

### No Concepts Promoted

**Possible reasons:**
- Stability score too low (need ≥0.7)
- Not enough occurrences (need ≥3)

**Solution:**
- Create more episodes for the same pattern
- Wait longer for patterns to stabilize
- Lower stability threshold in config

## Configuration

You can customize consolidation behavior by modifying the config:

```go
config := &mempkg.ConsolidationConfig{
    Interval:              30 * time.Minute,  // Run every 30 minutes
    MinSimilarEpisodes:    3,                 // Lower threshold for testing
    SimilarityThreshold:   0.7,                // Lower similarity
    MinStabilityScore:     0.6,                // Lower stability
    MinOccurrences:        2,                   // Lower occurrences
    TraceMaxAge:           1 * 24 * time.Hour,  // 1 day
    TraceMinUtility:       0.2,                 // Lower utility
    MinWorkflowRepetitions: 2,                  // Lower repetitions
}
```

Then pass it when creating the consolidator (modify `hdn/server.go`).

## Next Steps

After successful local testing:

1. **Review Results**: Check what was consolidated, promoted, and archived
2. **Adjust Config**: Tune thresholds based on your data patterns
3. **Monitor**: Watch logs during normal operation
4. **Deploy**: Deploy to Kubernetes with confidence

## Additional Resources

- [Memory Consolidation Documentation](MEMORY_CONSOLIDATION.md)
- [Architecture Overview](ARCHITECTURE.md)
- [HDN Server Configuration](../hdn/server.go)

